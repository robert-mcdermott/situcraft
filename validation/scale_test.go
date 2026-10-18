// The scale test (wave 3, slice 9; requirements S12 and P6): a generated tissue of about 55
// million transcripts (the simulator's large preset, made in tiles) is refined in patches and read
// by the Viewer, its cells typed (wave 4, slice 8: the instrument's segmentation clustered, mapped
// and labeled, and the map drawn in headless Chrome) and their spatial analyses asked (wave 6,
// slice 8: graphs, enrichment, niches and Moran's I), each step in a process of its own,
// timed and its peak memory measured against its budget; then everything is deleted. It needs
// about 8 GB of disk and, on four cores, about an hour and a half; the weekly workflow runs it:
//
//	SITUCRAFT_SCALE=/a/folder/with/room go test ./validation/ -run Scale -v -timeout 3h
//
// The budgets are what keeps memory bounded whatever the tissue's size: the simulator holds a tile
// at a time, the refinement a patch at a time (and keeps within its own estimate), and the Viewer
// reads a segmentation's transcripts by tile.
package validation

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"situcraft/internal/spatial"
)

const gb = 1 << 30

// The budgets (peak resident memory). The simulator's tissue is 28,672 by 24,576 pixels: held whole,
// its image alone would be several times its budget.
const (
	simulateBudget = 4.5 * gb
	refineBudget   = 8 * gb   // and within a tenth over the preflight's estimate
	deriveBudget   = 3 * gb   // the derived data, the instrument's segmentation and the import
	viewerBudget   = 1.5 * gb // the Viewer: reading a refinement whole took 1.8 GB at this scale
	typingBudget   = 5 * gb   // the instrument's segmentation made, and about 400,000 cells typed
	mapBudget      = 2 * gb   // SituCraft serving the page that draws the map
	spatialBudget  = 2 * gb   // the typed cells' graphs, enrichment, niches and Moran's I (1.2 GB)
	// typingTime bounds the typing on four cores; spatialTime the spatial analyses; mapTime the
	// map's showing in the page.
	typingTime  = 30 * time.Minute
	spatialTime = 10 * time.Minute
	mapTime     = 60 * time.Second
	// tileBudget is the slowest a transcript tile with its cells and probabilities may answer (the
	// Viewer suite's query budget).
	tileBudget = 500 * time.Millisecond
)

// scaleStep is a step's measures.
type scaleStep struct {
	Name    string
	Seconds float64
	Peak    int64
}

func (s scaleStep) String() string {
	return fmt.Sprintf("%-34s %8.0f s %8.2f GB", s.Name, s.Seconds, float64(s.Peak)/gb)
}

func TestScale(t *testing.T) {
	root := os.Getenv("SITUCRAFT_SCALE")
	if root == "" {
		t.Skip("set SITUCRAFT_SCALE to a folder with 8 GB free to run the scale test")
	}
	if runtime.GOOS == "windows" {
		t.Skip("peak memory is measured on Linux and macOS")
	}
	work := filepath.Join(root, "situcraft-scale")
	os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
	bin := buildSituCraft(t)
	bundle, out := filepath.Join(work, "large"), filepath.Join(work, "refined")
	var steps []scaleStep
	t.Cleanup(func() {
		var b strings.Builder
		for _, s := range steps {
			fmt.Fprintln(&b, s)
		}
		t.Logf("on %d cores:\n%s", runtime.NumCPU(), b.String())
	})
	within := func(s scaleStep, budget int64) {
		t.Helper()
		steps = append(steps, s)
		if s.Peak > budget {
			t.Errorf("%s: a peak of %.2f GB, over its budget of %.2f GB", s.Name, float64(s.Peak)/gb, float64(budget)/gb)
		}
	}

	// The tissue, made a tile at a time.
	text, s := command(t, "Simulating the large tissue", bin, "simulate", "--preset", "large", "--quiet", bundle)
	within(s, simulateBudget)
	transcripts := number(t, text, `(\d+) transcripts:`)
	cells := number(t, text, `Simulated (\d+) cells`)
	if transcripts < 45e6 {
		t.Fatalf("a large tissue of %d transcripts", transcripts)
	}

	// The refinement's preflight: patches, and the memory it expects.
	sched := []string{"--sweeps", "40", "--samples", "20"}
	text, _ = command(t, "", bin, append(append([]string{"refine", "--preflight"}, sched...), bundle)...)
	patches := number(t, text, `refined in (\d+) patches`)
	estimate := memoryOf(t, text, `Needs about ([\d.]+ [KMG]B) of memory`)
	if patches < 16 {
		t.Errorf("%d patches", patches)
	}

	// The refinement, a patch at a time.
	text, s = command(t, "Refining in patches (40 sweeps)", bin, append(append([]string{"refine", "--quiet"}, sched...), bundle, out)...)
	within(s, refineBudget)
	// The collector's timing moves the peak by a sixth from run to run (5.1 to 6.2 GB here, against
	// an estimate of 6.6): within a tenth over the estimate is within it.
	if s.Peak > estimate+estimate/10 {
		t.Errorf("the refinement's peak of %.2f GB is more than a tenth over its estimate of %.2f GB", float64(s.Peak)/gb, float64(estimate)/gb)
	}
	refined := number(t, text, `Refined (\d+) of`)
	if !strings.Contains(text, fmt.Sprintf("in %d patches of one region", patches)) || refined < cells*9/10 {
		t.Errorf("the refinement: %s", text)
	}

	// The derived data, the instrument's segmentation and the refinement imported, in SituCraft.
	library, cache := filepath.Join(work, "library"), filepath.Join(work, "cache")
	p := startSituCraft(t, bin, library, cache, bundle)
	start := time.Now()
	p.job(p.post("/api/local/0/cache", ""))
	p.job(p.post("/api/local/0/segmentation", ""))
	p.job(p.post("/api/local/0/import", fmt.Sprintf(`{"format":"situcraft","paths":[%q]}`, out)))
	within(p.stop("Derived data, registration and import", start), deriveBudget)

	// The Viewer with the dataset alone, in a new SituCraft: its descriptor, its cells and a
	// region's transcripts.
	p = startSituCraft(t, bin, library, cache, bundle)
	start = time.Now()
	p.get("/api/local/0/view")
	p.get("/api/local/0/cells")
	p.get("/api/local/0/points?x0=1000&y0=1000&x1=1200&y1=1200&minq=20")
	within(p.stop("The Viewer, the dataset alone", start), viewerBudget)

	// The Viewer drawing the refinement, in another: its cells (the first read writes its
	// transcripts' cells and probabilities by tile), and every transcript tile with them.
	p = startSituCraft(t, bin, library, cache, bundle)
	start = time.Now()
	var desc struct {
		Grid struct {
			Width, Height, Tile int
		}
		Transcripts struct{ Count int64 }
	}
	json.Unmarshal(p.get("/api/local/0/view"), &desc)
	var list struct {
		Artifacts []struct {
			ID, Kind string
			Lineage  struct{ Parents []struct{ Kind string } }
		}
	}
	json.Unmarshal(p.get("/api/artifacts"), &list)
	id := ""
	for _, a := range list.Artifacts {
		if a.Kind == "segmentation" && len(a.Lineage.Parents) > 0 && slices.ContainsFunc(a.Lineage.Parents, func(p struct{ Kind string }) bool { return p.Kind == "import" }) {
			id = a.ID
		}
	}
	if id == "" {
		t.Fatal("the imported refinement is not listed")
	}
	at := time.Now()
	table := p.get("/api/local/0/segmentations/" + id + "/cells")
	t.Logf("the refinement read for the Viewer in %.1f s", time.Since(at).Seconds())
	n := int(binary.LittleEndian.Uint32(table[4:]))
	if string(table[:4]) != "SCVS" || n < refined {
		t.Errorf("a cells table of %d cells (%q)", n, table[:4])
	}
	p.get("/api/local/0/segmentations/" + id + "/cells/0")
	across, down := (desc.Grid.Width+desc.Grid.Tile-1)/desc.Grid.Tile, (desc.Grid.Height+desc.Grid.Tile-1)/desc.Grid.Tile
	var points, inCells int64
	var slowest time.Duration
	first := time.Now()
	for ty := 0; ty < down; ty++ {
		for tx := 0; tx < across; tx++ {
			at := time.Now()
			tile := p.get(fmt.Sprintf("/api/local/0/segmentations/%s/points/%d/%d?minq=0", id, tx, ty))
			slowest = max(slowest, time.Since(at))
			c, ok := readCells(tile, n)
			if !ok {
				t.Fatalf("tile %d, %d: a malformed answer", tx, ty)
			}
			points += int64(len(c))
			for _, k := range c {
				if k != math.MaxUint32 {
					inCells++
				}
			}
		}
	}
	t.Logf("%d transcript tiles in %.1f s, the slowest in %v", across*down, time.Since(first).Seconds(), slowest.Round(time.Millisecond))
	within(p.stop("The Viewer drawing the refinement", start), viewerBudget)
	if points != desc.Transcripts.Count || points != int64(transcripts) || inCells < points/2 {
		t.Errorf("%d transcripts in the tiles, %d in cells; the dataset has %d", points, inCells, desc.Transcripts.Count)
	}
	if slowest > tileBudget {
		t.Errorf("a transcript tile took %v, over the budget of %v", slowest, tileBudget)
	}

	// The cells typed, in another: the instrument's segmentation (its files made on first use, the
	// mask read a row of chunks at a time) clustered (its neighbors by NN-Descent above 50,000 cells,
	// their recall measured), mapped (the epochs in batches over every core) and labeled from the
	// simulated types' markers.
	p = startSituCraft(t, bin, library, cache, bundle)
	start = time.Now()
	json.Unmarshal(p.get("/api/artifacts"), &list)
	seg := ""
	for _, a := range list.Artifacts {
		if a.Kind == "segmentation" && !slices.ContainsFunc(a.Lineage.Parents, func(p struct{ Kind string }) bool { return p.Kind == "import" }) {
			seg = a.ID
		}
	}
	var made struct{ ID string }
	answer := p.post("/api/local/0/cluster", fmt.Sprintf(`{"segmentation":%q}`, seg))
	json.Unmarshal(answer, &made)
	p.job(answer)
	clustering := made.ID
	answer = p.post("/api/local/0/embed", fmt.Sprintf(`{"clustering":%q}`, clustering))
	json.Unmarshal(answer, &made)
	p.job(answer)
	embedding := made.ID
	var offer struct {
		Starters []struct {
			ID  string
			Set json.RawMessage
		}
	}
	json.Unmarshal(p.get("/api/local/0/clusterings/"+clustering+"/markers"), &offer)
	var markers json.RawMessage
	for _, st := range offer.Starters {
		if st.ID == "simulated" {
			markers = st.Set
		}
	}
	if markers == nil {
		t.Fatal("no marker set offered for the simulated tissue")
	}
	answer = p.post("/api/local/0/label", fmt.Sprintf(`{"clustering":%q,"markers":%s}`, clustering, markers))
	json.Unmarshal(answer, &made)
	p.job(answer)
	labels := made.ID
	var cd struct {
		Cells struct{ Analyzed int }
		Graph struct {
			Search *struct {
				Recall float64
				Rounds int
			}
		}
		Clusters struct{ Count int }
		Seconds  map[string]float64
	}
	json.Unmarshal(p.get("/api/artifacts/"+clustering+"/files/clustering.json"), &cd)
	var ed struct {
		Fitted  struct{ Optimizer string }
		Quality struct {
			Trustworthiness float64
			Search          *struct{ Recall float64 }
		}
		Seconds map[string]float64
	}
	json.Unmarshal(p.get("/api/artifacts/"+embedding+"/files/embedding.json"), &ed)
	var ld struct {
		Description struct {
			Sets []struct {
				Name  string
				Cells int
			}
			Cells struct {
				Analyzed int
				Labeled  []int
			}
		}
	}
	json.Unmarshal(p.get("/api/labels/"+labels), &ld)
	typing := p.stop("Typing the cells", start)
	within(typing, typingBudget)
	t.Logf("typed: %d cells, %d clusters (%v); neighbors' recall %+v; map %v, trustworthiness %.3f, its neighborhoods' recall %+v; %v of %d labeled",
		cd.Cells.Analyzed, cd.Clusters.Count, cd.Seconds, cd.Graph.Search, ed.Seconds, ed.Quality.Trustworthiness, ed.Quality.Search, ld.Description.Cells.Labeled, ld.Description.Cells.Analyzed)
	if time.Duration(typing.Seconds*float64(time.Second)) > typingTime {
		t.Errorf("typing took %.0f s, over its budget of %v", typing.Seconds, typingTime)
	}
	if cd.Cells.Analyzed < cells*3/4 || cd.Graph.Search == nil || cd.Graph.Search.Recall < 0.9 || ed.Fitted.Optimizer == "" || ed.Quality.Search == nil || ed.Quality.Trustworthiness < 0.75 {
		t.Errorf("the clustering or the map: %d cells analyzed of %d; neighbors %+v; map optimizer %q, trustworthiness %.3f, %+v", cd.Cells.Analyzed, cells, cd.Graph.Search, ed.Fitted.Optimizer, ed.Quality.Trustworthiness, ed.Quality.Search)
	}
	// Each type's share of the labeled cells against its share of the true cells.
	var sim struct {
		Made struct {
			CellsByType map[string]int `json:"cells_by_type"`
		}
	}
	raw, err := os.ReadFile(filepath.Join(bundle, "situcraft-simulation.json"))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(raw, &sim)
	trueTotal, labeledTotal := 0, 0
	for _, n := range sim.Made.CellsByType {
		trueTotal += n
	}
	for _, s := range ld.Description.Sets {
		labeledTotal += s.Cells
	}
	// Half the sum over every type, labeled or true, of the difference of its shares.
	share := map[string]float64{}
	for _, s := range ld.Description.Sets {
		share[s.Name] += float64(s.Cells) / float64(labeledTotal)
	}
	for name, n := range sim.Made.CellsByType {
		share[name] -= float64(n) / float64(trueTotal)
	}
	distance := 0.0
	for _, d := range share {
		distance += math.Abs(d) / 2
	}
	t.Logf("the types' shares against the truth's: total variation distance %.4f", distance)
	if labeledTotal < cd.Cells.Analyzed*9/10 || distance > 0.05 {
		t.Errorf("%d of %d cells labeled; the types' shares %.4f from the truth's", labeledTotal, cd.Cells.Analyzed, distance)
	}

	// The typed cells' spatial analyses (wave 6, slice 8), in another, asked as the Spatial view
	// asks them, with its defaults: the Delaunay graph of every cell of the instrument's
	// segmentation and its contact graph (its mask read a row of chunks at a time); the labels'
	// enrichment over the Delaunay graph; niches, with their genes; and Moran's I of the panel's
	// genes over the Delaunay graph, with the leading genes' local Moran's I.
	p = startSituCraft(t, bin, library, cache, bundle)
	start = time.Now()
	lc := p.get("/api/labels/" + labels + "/cells")
	le := binary.LittleEndian
	segCells, labeledN, levels := int(le.Uint32(lc)), int(le.Uint32(lc[4:])), int(le.Uint32(lc[8:]))
	if levels < 1 || len(lc) < 16+12*labeledN {
		t.Fatalf("the label set's cells: %d of %d cells, %d levels", labeledN, segCells, levels)
	}
	codes := make([]byte, 4+4*segCells)
	le.PutUint32(codes, uint32(segCells))
	for i := 0; i < segCells; i++ {
		le.PutUint32(codes[4+4*i:], math.MaxUint32) // −1: no label
	}
	for k := 0; k < labeledN; k++ {
		cell := le.Uint32(lc[16+4*k:])
		le.PutUint32(codes[4+4*cell:], le.Uint32(lc[16+8*labeledN+4*k:]))
	}
	keep := make([]byte, 4+(segCells+7)/8)
	le.PutUint32(keep, uint32(segCells))
	for i := 0; i < segCells; i++ {
		keep[4+i/8] |= 1 << (i % 8)
	}
	var names []string
	for _, st := range ld.Description.Sets {
		names = append(names, st.Name)
	}
	encoded := func(raw []byte) (string, string) {
		sum := sha256.Sum256(raw)
		return base64.StdEncoding.EncodeToString(raw), hex.EncodeToString(sum[:])
	}
	keepCodes, keepSum := encoded(keep)
	labelCodes, labelSum := encoded(codes)
	cellsOf := map[string]string{"keep": keepCodes, "checksum": keepSum}
	labelsOf := map[string]any{"codes": labelCodes, "checksum": labelSum, "id": labels, "kind": "labels", "name": "Labels from markers", "level": 0, "names": names}
	ask := func(what, path string, body map[string]any) string {
		raw, _ := json.Marshal(body)
		answer := p.post(path, string(raw))
		var made struct{ ID string }
		json.Unmarshal(answer, &made)
		at := time.Now()
		p.job(answer)
		t.Logf("%s: %.1f s", what, time.Since(at).Seconds())
		return made.ID
	}
	delaunay := ask("the Delaunay graph", "/api/local/0/graph", map[string]any{"segmentation": seg, "parameters": spatial.DefaultParams(spatial.Delaunay), "cells": cellsOf})
	ask("the contact graph", "/api/local/0/graph", map[string]any{"segmentation": seg, "parameters": spatial.DefaultParams(spatial.Contact), "cells": cellsOf})
	enrichment := ask("the enrichment", "/api/local/0/spatial", map[string]any{"question": spatial.QuestionEnrichment, "segmentation": seg, "graph": delaunay, "labels": labelsOf, "parameters": spatial.DefaultEnrichment()})
	niches := ask("niches", "/api/local/0/spatial", map[string]any{"question": spatial.QuestionNiches, "segmentation": seg, "cells": cellsOf, "labels": labelsOf, "parameters": spatial.DefaultNiches()})
	moran := ask("Moran's I", "/api/local/0/spatial", map[string]any{"question": spatial.QuestionMoran, "segmentation": seg, "graph": delaunay, "parameters": spatial.DefaultMoran()})
	analyses := p.stop("Analyzing the typed cells", start)
	within(analyses, spatialBudget)
	if time.Duration(analyses.Seconds*float64(time.Second)) > spatialTime {
		t.Errorf("the spatial analyses took %.0f s, over their budget of %v", analyses.Seconds, spatialTime)
	}
	// Their answers, from their files.
	p = startSituCraft(t, bin, library, cache, bundle)
	var en struct {
		Enrichment struct{ Cells []int }
		Niches     struct{ Windows int }
		Moran      struct {
			Genes []string
			Local []int
		}
	}
	for _, id := range []string{enrichment, niches, moran} {
		json.Unmarshal(p.get("/api/artifacts/"+id+"/files/analysis.json"), &en)
	}
	p.stop("", time.Now())
	t.Logf("analyzed: the enrichment of %d labels, niches of %d windows, Moran's I of %d genes (%d local)", len(en.Enrichment.Cells), en.Niches.Windows, len(en.Moran.Genes), len(en.Moran.Local))
	if len(en.Enrichment.Cells) == 0 || en.Niches.Windows < labeledN*9/10 || len(en.Moran.Genes) == 0 || len(en.Moran.Local) != spatial.DefaultMoran().Local {
		t.Errorf("the spatial analyses' answers are not whole")
	}

	// The map drawn in headless Chrome, by a SituCraft serving the page.
	if _, err := exec.LookPath("node"); err != nil {
		t.Log("Node is not installed: the map is not drawn")
		return
	}
	p = startSituCraft(t, bin, library, cache, bundle)
	start = time.Now()
	var conn struct{ Token string }
	raw, _ = os.ReadFile(filepath.Join(library, "remote.json"))
	json.Unmarshal(raw, &conn)
	// go test runs in the package's folder, validation/.
	printed, err := exec.Command("node", "scale-map.mjs", p.url, conn.Token, bundle).CombinedOutput()
	if err != nil {
		p.stop("The map drawn", start)
		t.Fatalf("the map in Chrome: %v\n%s", err, printed)
	}
	var drawn struct {
		Cells     int
		Seconds   float64
		RecolorMs float64
	}
	json.Unmarshal(printed[bytes.LastIndexByte(bytes.TrimSpace(printed), '\n')+1:], &drawn)
	within(p.stop("The map drawn", start), mapBudget)
	t.Logf("the map: %d cells shown in %.1f s, colored again in %.0f ms", drawn.Cells, drawn.Seconds, drawn.RecolorMs)
	if drawn.Cells != cd.Cells.Analyzed || time.Duration(drawn.Seconds*float64(time.Second)) > mapTime {
		t.Errorf("the map drew %d of %d cells in %.1f s (budget %v)", drawn.Cells, cd.Cells.Analyzed, drawn.Seconds, mapTime)
	}
}

// command runs the program, measured (named) or not, and returns what it printed.
func command(t *testing.T, name, bin string, args ...string) (string, scaleStep) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("situcraft %s: %v\n%s%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), scaleStep{name, time.Since(start).Seconds(), peakResident(cmd.ProcessState)}
}

func number(t *testing.T, text, pattern string) int {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("no %q in %s", pattern, text)
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// memoryOf reads a size the program printed ("6.6 GB").
func memoryOf(t *testing.T, text, pattern string) int64 {
	t.Helper()
	m := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("no %q in %s", pattern, text)
	}
	var v float64
	var unit string
	fmt.Sscanf(m[1], "%g %s", &v, &unit)
	return int64(v * map[string]float64{"KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30}[unit])
}

// readCells reads a transcript tile's cells (internal/view Points.Encode: a header, the genes'
// starts, x, y, then each transcript's cell), checking its probabilities are there.
func readCells(tile []byte, cells int) ([]uint32, bool) {
	if len(tile) < 16 || string(tile[:4]) != "SCVP" {
		return nil, false
	}
	le := binary.LittleEndian
	n, genes, extra := int(le.Uint32(tile[4:])), int(le.Uint32(tile[8:])), le.Uint32(tile[12:])
	at := 16 + 4*(genes+1) + 8*n
	end := (at + 4*n + 2*genes + 3) &^ 3
	if extra != 1 || len(tile) != end+4*n {
		return nil, false
	}
	out := make([]uint32, n)
	for i := range out {
		out[i] = le.Uint32(tile[at+4*i:])
		if out[i] != math.MaxUint32 && int(out[i]) >= cells {
			return nil, false
		}
		if p := math.Float32frombits(le.Uint32(tile[end+4*i:])); p < 0 || p > 1 {
			return nil, false
		}
	}
	return out, true
}

// situCraft is the program serving, for the steps that use it as people do.
type situCraft struct {
	t   *testing.T
	cmd *exec.Cmd
	url string
}

// startSituCraft starts the program on a bundle, with its own library and cache, and no window.
func startSituCraft(t *testing.T, bin, library, cache, bundle string) *situCraft {
	t.Helper()
	cmd := exec.Command(bin, "--remote-control", "--window", "none", "--port", "8856", "--cache-limit", "0", "--data-dir", library, "--cache-dir", cache, bundle)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	found := make(chan string, 1)
	go func() {
		var seen bytes.Buffer
		buf := make([]byte, 4096)
		sent := false
		for {
			k, err := stdout.Read(buf)
			seen.Write(buf[:k])
			if m := regexp.MustCompile(`is running at (http://\S+)`).FindSubmatch(seen.Bytes()); m != nil && !sent {
				found <- strings.TrimRight(string(m[1]), "/")
				sent = true
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case url := <-found:
		return &situCraft{t, cmd, url}
	case <-time.After(time.Minute):
		cmd.Process.Kill()
		t.Fatal("SituCraft did not start")
	}
	return nil
}

func (p *situCraft) do(method, path, body string) []byte {
	p.t.Helper()
	req, _ := http.NewRequest(method, p.url+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		p.t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, raw)
	}
	return raw
}

func (p *situCraft) get(path string) []byte        { return p.do(http.MethodGet, path, "") }
func (p *situCraft) post(path, body string) []byte { return p.do(http.MethodPost, path, body) }

// job waits for the job an answer started, if it started one.
func (p *situCraft) job(answer []byte) {
	p.t.Helper()
	var started struct {
		Job *struct{ ID string }
	}
	json.Unmarshal(answer, &started)
	if started.Job == nil {
		return
	}
	for {
		var j struct{ State, Title, Error string }
		json.Unmarshal(p.get("/api/jobs/"+started.Job.ID), &j)
		switch j.State {
		case "complete":
			return
		case "failed", "cancelled", "invalid", "orphaned":
			p.t.Fatalf("%s: %s %s", j.Title, j.State, j.Error)
		}
		time.Sleep(time.Second)
	}
}

// stop ends the program and measures it from start.
func (p *situCraft) stop(name string, start time.Time) scaleStep {
	p.t.Helper()
	seconds := time.Since(start).Seconds()
	p.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { p.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Minute):
		p.cmd.Process.Kill()
		<-done
	}
	return scaleStep{name, seconds, peakResident(p.cmd.ProcessState)}
}
