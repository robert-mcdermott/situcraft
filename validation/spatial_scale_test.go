package validation

// The spatial analyses at a million cells (wave 6, slice 8; research.md §6.8): a generated tissue
// of about a million cells, the large preset's fields 11 by 10 (9.6 by 8.7 mm), its true cells
// alone (simulate.Cells: no images or transcripts), their true types the labels. Each analysis runs
// in a process of its own (this test binary again, TestSpatialScaleStep), as SituCraft's job runs
// it from the files its inputs are kept in, timed and its peak memory measured against its budget:
// the Delaunay, 6-nearest and 20 µm graphs; the types' enrichment over the Delaunay graph; their
// co-occurrence; nearest distances; Ripley's K in the tissue's rectangle; niches with their genes;
// and Moran's I of the panel's genes with the leading genes' local Moran's I, each with the
// defaults the page offers. Each answer is checked as far as the tissue's truth says. Then all is
// deleted. It needs about 2 GB of disk; the weekly workflow runs it with the scale test:
//
//	SITUCRAFT_SCALE=/a/folder go test ./validation/ -run SpatialScale -v -timeout 1h

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"situcraft/internal/cluster"
	"situcraft/internal/segment"
	"situcraft/internal/simulate"
	"situcraft/internal/spatial"
)

// The budgets of the analyses at a million cells: the peak resident memory of the process running
// one, and the time it may take on four cores (measured on a 4-core run of a 10-core laptop, in
// GB and seconds: Delaunay 0.77 and 4, 6-nearest 0.33 and 1, 20 µm 0.67 and 2, enrichment 0.23
// and 4, co-occurrence 0.07 and 5, nearest distances 3.4 and 37, Ripley's K 4.0 and 36, niches
// 1.8 and 12, Moran's I 0.94 and 88). Nearest distances and Ripley's K keep every pair of cells
// within their reach (100 µm: about 380 a cell here), so their memory grows with the cells and
// their density.
var spatialBudgets = map[string]struct {
	peak    int64
	seconds float64
}{
	"delaunay":     {1.25 * gb, 60},
	"knn":          {1 * gb, 60},
	"radius":       {1 * gb, 60},
	"enrichment":   {0.5 * gb, 60},
	"cooccurrence": {0.5 * gb, 60},
	"nearest":      {4.5 * gb, 180},
	"ripley":       {5 * gb, 180},
	"niches":       {2.5 * gb, 120},
	"moran":        {1.5 * gb, 300},
}

var spatialSteps = []string{"delaunay", "knn", "radius", "enrichment", "cooccurrence", "nearest", "ripley", "niches", "moran"}

func TestSpatialScale(t *testing.T) {
	root := os.Getenv("SITUCRAFT_SCALE")
	if root == "" {
		t.Skip("set SITUCRAFT_SCALE to a folder with 2 GB free to run the analyses at a million cells")
	}
	if runtime.GOOS == "windows" {
		t.Skip("peak memory is measured on Linux and macOS")
	}
	work := filepath.Join(root, "situcraft-spatial-scale")
	os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
	p := simulate.Presets["large"]
	p.TilesAcross, p.TilesDown = 11, 10
	start := time.Now()
	tc, err := simulate.Cells(context.Background(), p, runtime.NumCPU(), nil)
	if err != nil {
		t.Fatal(err)
	}
	n := len(tc.X)
	t.Logf("a tissue of %d cells, %d types, %d genes, %d counts, made in %.1f s", n, len(tc.Types), len(tc.Counts.Genes), len(tc.Counts.Gene), time.Since(start).Seconds())
	if n < 1e6 {
		t.Fatalf("a tissue of %d cells, not a million", n)
	}
	if err := writeScaleCells(work, tc); err != nil {
		t.Fatal(err)
	}
	if err := segment.WriteCounts(work, tc.Counts); err != nil {
		t.Fatal(err)
	}
	tc = nil
	var steps []scaleStep
	t.Cleanup(func() {
		var b strings.Builder
		for _, s := range steps {
			fmt.Fprintln(&b, s)
		}
		t.Logf("a million cells on %d cores:\n%s", runtime.GOMAXPROCS(0), b.String())
	})
	results := map[string]map[string]float64{}
	for _, step := range spatialSteps {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSpatialScaleStep$", "-test.count=1", "-test.v", "-test.timeout=1h")
		cmd.Env = append(os.Environ(), "SITUCRAFT_SCALE_STEP="+step, "SITUCRAFT_SCALE_INPUT="+work)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		at := time.Now()
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s: %v\n%s", step, err, out.String())
		}
		s := scaleStep{step, time.Since(at).Seconds(), peakResident(cmd.ProcessState)}
		steps = append(steps, s)
		var r map[string]float64
		line := out.String()
		if i := strings.Index(line, "RESULT "); i >= 0 {
			json.Unmarshal([]byte(strings.TrimSpace(strings.SplitN(line[i+7:], "\n", 2)[0])), &r)
		}
		results[step] = r
		t.Logf("%s: %.1f s, %.2f GB; %v", step, s.Seconds, float64(s.Peak)/gb, r)
		b := spatialBudgets[step]
		if s.Peak > b.peak {
			t.Errorf("%s: a peak of %.2f GB, over its budget of %.2f GB", step, float64(s.Peak)/gb, float64(b.peak)/gb)
		}
		// The time budgets are four cores' (GOMAXPROCS=4 measures them on more); more cores are
		// allowed no more.
		if s.Seconds > b.seconds {
			t.Errorf("%s: %.0f s, over its budget of %.0f s", step, s.Seconds, b.seconds)
		}
	}
	// What the tissue's truth says of the answers: about three edges a cell in a triangulation;
	// each type's cells nearer one another than chance (they gather in the domains they lead);
	// every cell in a niche, with its genes (the default 6 niches are more than the tissue's 4
	// kinds of domain, so the seeds' agreement is reported, not required); the marker genes in a
	// pattern above every unmarked gene.
	if d := results["delaunay"]; d["edges"] < 2.8*float64(n) || d["edges"] > 3*float64(n) {
		t.Errorf("the Delaunay graph: %v edges of %d cells", d["edges"], n)
	}
	if e := results["enrichment"]; e["selfLeastZ"] < 10 {
		t.Errorf("the enrichment: a type's cells with one another at z %v", e["selfLeastZ"])
	}
	if nc := results["niches"]; nc["markerCells"] < 0.9*float64(n) {
		t.Errorf("the niches: %v", nc)
	}
	if m := results["moran"]; !(m["markerLow"] > m["otherHigh"]) || m["local"] != 10 {
		t.Errorf("Moran's I: %v", m)
	}
}

// writeScaleCells writes the cells' centroids, types and domains (n, then x, y as float32, type and
// domain as int32, little-endian) and the types' names.
func writeScaleCells(dir string, tc *simulate.TrueCells) error {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(len(tc.X)))
	for _, v := range [][]float32{tc.X, tc.Y} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	for _, v := range [][]int32{tc.Type, tc.Domain} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	if err := os.WriteFile(filepath.Join(dir, "cells.scale"), b.Bytes(), 0o644); err != nil {
		return err
	}
	raw, _ := json.Marshal(tc.Types)
	return os.WriteFile(filepath.Join(dir, "types.json"), raw, 0o644)
}

func readScaleCells(t *testing.T, dir string) (x, y []float32, typ, domain []int32, types []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "cells.scale"))
	if err != nil {
		t.Fatal(err)
	}
	n := int(binary.LittleEndian.Uint32(raw))
	r := bytes.NewReader(raw[4:])
	x, y, typ, domain = make([]float32, n), make([]float32, n), make([]int32, n), make([]int32, n)
	for _, v := range []any{x, y, typ, domain} {
		if err := binary.Read(r, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := os.ReadFile(filepath.Join(dir, "types.json"))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(tr, &types)
	return x, y, typ, domain, types
}

// TestSpatialScaleStep is one analysis of TestSpatialScale, in its own process: it reads the
// inputs as the analysis's job does, answers, and prints what the parent checks.
func TestSpatialScaleStep(t *testing.T) {
	step, dir := os.Getenv("SITUCRAFT_SCALE_STEP"), os.Getenv("SITUCRAFT_SCALE_INPUT")
	if step == "" {
		t.Skip("one step of TestSpatialScale")
	}
	ctx := context.Background()
	workers := runtime.GOMAXPROCS(0) // the computer's cores, or fewer, set by GOMAXPROCS
	x, y, typ, _, types := readScaleCells(t, dir)
	K := len(types)
	result := map[string]float64{}
	graphDir := filepath.Join(dir, "delaunay")
	readGraph := func() *spatial.Graph {
		g, err := spatial.Read(graphDir)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	build := func(kind string) *spatial.Graph {
		g, err := spatial.Build(ctx, spatial.Inputs{X: x, Y: y}, spatial.DefaultParams(kind), workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		result["edges"] = float64(g.Edges())
		return g
	}
	switch step {
	case "delaunay":
		g := build(spatial.Delaunay)
		if err := os.MkdirAll(graphDir, 0o755); err != nil {
			t.Fatal(err)
		}
		d := spatial.Describe(g, spatial.Side{ID: "generated", Name: "A million generated cells"}, len(x), spatial.DefaultParams(spatial.Delaunay), spatial.Choice{})
		if err := spatial.Write(graphDir, g, d); err != nil {
			t.Fatal(err)
		}
	case "knn":
		build(spatial.KNN)
	case "radius":
		build(spatial.Radius)
	case "enrichment":
		g := readGraph()
		lab := make([]int32, len(g.Rows))
		for i, r := range g.Rows {
			lab[i] = typ[r]
		}
		e, err := spatial.Enrich(ctx, g, lab, K, nil, spatial.DefaultEnrichment(), workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		least := math.Inf(1)
		for a := 0; a < K; a++ {
			if e.Cells[a] > 0 {
				least = math.Min(least, e.Z[a*K+a])
			}
		}
		result["selfLeastZ"] = least
	case "cooccurrence":
		co, err := spatial.Cooccur(ctx, x, y, typ, K, spatial.DefaultCooccurrence(), workers)
		if err != nil {
			t.Fatal(err)
		}
		result["distances"] = float64(len(co.Ratio))
	case "nearest":
		nd, err := spatial.NearestDistances(ctx, x, y, typ, K, spatial.DefaultNearest(), workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		result["pairs"] = float64(len(nd.Median))
	case "ripley":
		x1, y1 := 0.0, 0.0
		for i := range x {
			x1, y1 = math.Max(x1, float64(x[i])), math.Max(y1, float64(y[i]))
		}
		win, err := spatial.PolygonWindow([][][][2]float64{{{{0, 0}, {x1, 0}, {x1, y1}, {0, y1}}}})
		if err != nil {
			t.Fatal(err)
		}
		rk, err := spatial.Ripleys(ctx, x, y, typ, K, win, spatial.DefaultRipley(), workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		result["pairs"] = float64(rk.Pairs)
	case "niches":
		nc, err := spatial.FindNiches(ctx, x, y, typ, K, spatial.DefaultNiches(), workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		lo := 1.0
		for _, a := range nc.Agreement {
			lo = math.Min(lo, a)
		}
		result["leastAgreement"] = lo
		// Each niche's genes against the other niches' cells, as the niches' job finds them.
		counts, err := segment.ReadCounts(dir)
		if err != nil {
			t.Fatal(err)
		}
		var rows []uint32
		var groups []int32
		for i, v := range nc.Assign {
			if v >= 0 {
				rows, groups = append(rows, uint32(i)), append(groups, v)
			}
		}
		mr, err := cluster.FromLabels(counts, rows, groups, cluster.Source{Tool: "SituCraft", Name: "niches"}, workers)
		if err != nil {
			t.Fatal(err)
		}
		result["markerCells"] = float64(len(mr.Cells))
	case "moran":
		g := readGraph()
		counts, err := segment.ReadCounts(dir)
		if err != nil {
			t.Fatal(err)
		}
		expr, _, _, err := spatial.NormalizedExpression(counts, g.Rows)
		if err != nil {
			t.Fatal(err)
		}
		genes := counts.Genes
		counts = nil
		m, err := spatial.MoransI(ctx, g, expr, spatial.DefaultMoran(), workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		marker := map[string]bool{}
		for _, names := range simulate.MarkerNames(simulate.Presets["large"]) {
			for _, name := range names {
				marker[name] = true
			}
		}
		lo, hi := math.Inf(1), math.Inf(-1)
		for gi, name := range genes {
			if math.IsNaN(m.I[gi]) {
				continue
			}
			if marker[name] {
				lo = math.Min(lo, m.I[gi])
			} else {
				hi = math.Max(hi, m.I[gi])
			}
		}
		result["markerLow"], result["otherHigh"], result["local"] = lo, hi, float64(len(m.Local))
	default:
		t.Fatalf("no step %q", step)
	}
	raw, _ := json.Marshal(result)
	fmt.Printf("RESULT %s\n", raw)
}
