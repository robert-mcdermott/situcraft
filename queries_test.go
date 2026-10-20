package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/query"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// Trees of conditions answered by the program (queries.go): each kind of condition keeps the cells
// it says, against numbers counted here from the bundle's own columns; a saved filter by name the
// same cells as its conditions; the transcripts of a box by gene; a region summarized.
func TestQueriesKeepWhatTheySay(t *testing.T) {
	f := newRefineFixture(t, true)
	t.Cleanup(f.a.views.closeAll)
	var building struct{ Job *jobs.Job }
	json.NewDecoder(f.post("/api/local/0/cache", "").Body).Decode(&building)
	if building.Job != nil {
		f.wait(building.Job.ID)
	}
	all, _ := f.a.artifacts.List()
	var seg artifacts.Manifest
	for _, m := range all {
		if m.Kind == "segmentation" && isBundleOwn(m) {
			seg = m
		}
	}
	b, err := xenium.Open(f.folder, xenium.Options{CacheDir: f.a.cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	cells, err := b.ReadCells()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := f.a.segmentationFolder(context.Background(), b, seg)
	b.Close()
	if err != nil {
		t.Fatal(err)
	}
	counts, err := segment.ReadCounts(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := len(cells.IDs)
	// The instrument's segmentation's rows are the bundle's cells, in order.
	own, err := segment.ReadCells(dir)
	if err != nil || len(own) != n || len(counts.Offsets) != n+1 {
		t.Fatalf("the instrument's segmentation: %d cells, %d rows of counts, of %d: %v", len(own), len(counts.Offsets)-1, n, err)
	}
	for i, c := range own {
		if c.ID != cells.IDs[i] {
			t.Fatalf("row %d is %s, the bundle's %s", i, c.ID, cells.IDs[i])
		}
	}
	// Bounds that divide the cells: the median transcripts and density.
	density := func(i int) float64 {
		return 100 * float64(cells.TranscriptCounts[i]) / float64(float32(cells.CellArea[i]))
	}
	var many, dense []float64
	for i := range n {
		many, dense = append(many, float64(cells.TranscriptCounts[i])), append(dense, density(i))
	}
	slices.Sort(many)
	slices.Sort(dense)
	least, densest := many[n/2], dense[n/2]

	// A label set: the cells left of x = 40 µm Left, the others Right.
	table := filepath.Join(t.TempDir(), "sides.csv")
	var csv strings.Builder
	csv.WriteString("cell_id,side\n")
	for i, id := range cells.IDs {
		side := "Right"
		if cells.X[i] < 40 {
			side = "Left"
		}
		fmt.Fprintf(&csv, "%s,%s\n", id, side)
	}
	os.WriteFile(table, []byte(csv.String()), 0o644)
	req := httptest.NewRequest(http.MethodPost, "/api/local/0/labels/import", strings.NewReader(fmt.Sprintf(`{"path":%q,"segmentation":%q,"columns":{"idColumn":"cell_id","column":"side"}}`, table, seg.ID)))
	req.Host, req.RemoteAddr = "127.0.0.1:8840", "127.0.0.1:50000"
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	var imported struct {
		ID  string
		Job *jobs.Job
	}
	json.NewDecoder(rec.Body).Decode(&imported)
	if imported.Job == nil {
		t.Fatalf("no label import: %d %s", rec.Code, rec.Body)
	}
	f.wait(imported.Job.ID)
	labels := imported.ID

	// The project, as the page saves it: the dataset, the label set, a region (a rectangle), a
	// filter saved with conditions.
	project := map[string]any{
		"format": "situcraft-project", "version": 1, "id": "p-queries", "name": "Queries", "created": "2026-10-20T10:00:00Z", "modified": "2026-10-20T11:00:00Z",
		"datasets": []any{map[string]any{"id": "d-1", "name": "simulated", "path": f.folder}},
		"artifacts": []any{
			map[string]any{"id": seg.ID, "kind": "segmentation", "name": seg.Name, "dataset": "d-1", "state": "complete"},
			map[string]any{"id": labels, "kind": "labels", "name": "Sides", "dataset": "d-1", "state": "complete"},
			map[string]any{"kind": "filter", "id": "flt-1", "name": "Good cells", "dataset": "d-1", "version": 1, "conditions": []any{map[string]any{"attribute": "transcripts", "min": least, "max": nil}}},
		},
		"regions": []any{map[string]any{"id": "r-1", "dataset": "d-1", "versions": []any{map[string]any{"name": "Nest", "cls": "Tumor", "geometry": [][][][2]float64{{{{20, 20}, {50, 20}, {50, 40}, {20, 40}, {20, 20}}}}}}}},
	}
	raw, _ := json.Marshal(project)
	os.MkdirAll(filepath.Join(f.a.store.dir, "projects"), 0o755)
	if err := os.WriteFile(filepath.Join(f.a.store.dir, "projects", "p-queries.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	call := func(name, args string) (remoteResult, map[string]any) {
		t.Helper()
		outcome, ok := f.a.programAction(context.Background(), name, json.RawMessage(args))
		if !ok {
			t.Fatalf("%s is not answered by the program", name)
		}
		var data map[string]any
		json.Unmarshal(outcome.Data, &data)
		return outcome, data
	}
	expect := func(where string, want func(i int) bool) map[string]any {
		t.Helper()
		outcome, data := call("query_cells", `{"project":"Queries","where":`+where+`,"limit":0}`)
		if !outcome.OK {
			t.Fatalf("%s: %s", where, outcome.Message)
		}
		keep := make([]bool, n)
		for i := range keep {
			keep[i] = want(i)
		}
		if data["cells"].(float64) != float64(n) || data["kept"].(float64) != float64(query.Count(keep)) || data["checksum"] != query.Checksum(keep) {
			t.Fatalf("%s: %v of %v kept (%s), want %d of %d", where, data["kept"], data["cells"], outcome.Message, query.Count(keep), n)
		}
		return data
	}
	inNest := func(i int) bool { return cells.X[i] > 20 && cells.X[i] < 50 && cells.Y[i] > 20 && cells.Y[i] < 40 }
	left := func(i int) bool { return cells.X[i] < 40 }
	geneCount := func(i, g int) float64 {
		sum := 0.0
		for k := counts.Offsets[i]; k < counts.Offsets[i+1]; k++ {
			if int(counts.Gene[k]) == g {
				sum += float64(counts.Count[k])
			}
		}
		return sum
	}

	enough := func(i int) bool { return float64(cells.TranscriptCounts[i]) >= least }
	good := expect(fmt.Sprintf(`{"attribute":"transcripts","min":%g}`, least), enough)
	if expect(`{"filter":"good CELLS"}`, enough)["checksum"] != good["checksum"] {
		t.Fatal("a saved filter by name keeps other cells than its conditions")
	}
	expect(fmt.Sprintf(`{"attribute":"density","max":%v}`, densest), func(i int) bool { return cells.CellArea[i] > 0 && density(i) <= densest })
	expect(`{"region":"Nest"}`, inNest)
	expect(`{"region":"r-1"}`, inNest)
	expect(`{"box":[20,20,50,40]}`, inNest)
	expect(`{"distance":"Nest","max":0}`, inNest)
	expect(`{"distance":"Nest","min":0,"max":10}`, func(i int) bool {
		x, y := float64(float32(cells.X[i])), float64(float32(cells.Y[i]))
		dx, dy := math.Max(math.Max(20-x, x-50), 0), math.Max(math.Max(20-y, y-40), 0)
		return !inNest(i) && math.Hypot(dx, dy) <= 10
	})
	expect(`{"labels":"`+labels+`","in":["left"]}`, left)
	expect(`{"all":[{"region":"Nest"},{"not":{"labels":"`+labels+`","in":["Left"]}}]}`, func(i int) bool { return inNest(i) && !left(i) })
	gene := counts.Genes[0]
	expect(`{"gene":"`+strings.ToLower(gene)+`","min":2}`, func(i int) bool { return geneCount(i, 0) >= 2 })
	expect(`{"any":[{"gene":"`+gene+`"},{"region":"Nest"}]}`, func(i int) bool { return geneCount(i, 0) >= 1 || inNest(i) })

	// Divided by the label set, a page of the cells with columns.
	outcome, data := call("query_cells", `{"project":"Queries","where":{"region":"Nest"},"by":"`+labels+`","columns":["area","`+gene+`"],"limit":3,"offset":1}`)
	comp := data["composition"].(map[string]any)
	kept, inside := 0.0, 0
	for _, row := range comp["rows"].([]any) {
		kept += row.([]any)[1].(float64)
	}
	for i := range n {
		if inNest(i) {
			inside++
		}
	}
	if !outcome.OK || kept != float64(inside) || len(comp["rows"].([]any)) != 2 {
		t.Fatalf("by labels: %s %v", outcome.Message, comp)
	}
	page := data["rows"].(map[string]any)
	rows := page["rows"].([]any)
	if len(rows) != 3 || fmt.Sprint(page["columns"]) != fmt.Sprintf("[row id x y area %s]", gene) {
		t.Fatalf("the page: %v", page)
	}
	second := int(rows[0].([]any)[0].(float64))
	if !inNest(second) || rows[0].([]any)[1] != cells.IDs[second] || !sameNumber(rows[0].([]any)[4], float64(float32(cells.CellArea[second]))) || !sameNumber(rows[0].([]any)[5], geneCount(second, 0)) {
		t.Fatalf("the page's first row: %v", rows[0])
	}

	// Refused, saying why: a region, filter, label or gene the dataset has not; a malformed tree.
	for where, says := range map[string]string{
		`{"region":"Moat"}`:                       "its regions are Nest",
		`{"filter":"Bad cells"}`:                  "its filters are Good cells",
		`{"labels":"` + labels + `","in":["Up"]}`: "has no \"Up\"",
		`{"gene":"NOTAGENE"}`:                     "no gene \"NOTAGENE\"",
		`{"attribute":"transcripts","in":[1]}`:    "tested with min and max",
		`{"clusters":"` + labels + `","in":[1]}`:  "not a clustering",
	} {
		if outcome, _ := call("query_cells", `{"project":"Queries","where":`+where+`}`); outcome.OK || !strings.Contains(outcome.Message, says) {
			t.Errorf("%s: %s, want it to say %q", where, outcome.Message, says)
		}
	}
	if outcome, _ := call("query_cells", `{"project":"Queries"}`); outcome.OK || !strings.Contains(outcome.Message, "where is required") {
		t.Errorf("no tree: %s", outcome.Message)
	}

	// Transcripts: those of the box by gene, the box's of one gene, and assigned to the box's cells.
	outcome, data = call("query_transcripts", `{"project":"Queries","where":{"box":[20,20,50,40]},"limit":0}`)
	if !outcome.OK || data["kept"].(float64) == 0 {
		t.Fatalf("query_transcripts: %s", outcome.Message)
	}
	byGene := map[string]float64{}
	sum := 0.0
	for _, row := range data["genes"].(map[string]any)["rows"].([]any) {
		byGene[row.([]any)[0].(string)] = row.([]any)[1].(float64)
		sum += row.([]any)[1].(float64)
	}
	if sum != data["kept"].(float64) {
		t.Fatalf("the genes' transcripts %v, of %v", sum, data["kept"])
	}
	lead := data["genes"].(map[string]any)["rows"].([]any)[0].([]any)[0].(string)
	_, one := call("query_transcripts", `{"project":"Queries","where":{"all":[{"box":[20,20,50,40]},{"gene":["`+lead+`"]}]}}`)
	if one["kept"].(float64) != byGene[lead] {
		t.Fatalf("%s: %v, of %v", lead, one["kept"], byGene[lead])
	}
	_, assigned := call("query_transcripts", `{"project":"Queries","where":{"all":[{"box":[20,20,50,40]},{"cells":{"region":"Nest"}}]}}`)
	_, free := call("query_transcripts", `{"project":"Queries","where":{"all":[{"box":[20,20,50,40]},{"assigned":false}]}}`)
	if a := assigned["kept"].(float64); a == 0 || a+free["kept"].(float64) > data["kept"].(float64) {
		t.Fatalf("assigned to the region's cells %v, to none %v, of %v", a, free["kept"], data["kept"])
	}

	// A region summarized: its area, its cells and their labels (the project's label set), its genes.
	outcome, data = call("summarize_region", `{"project":"Queries","region":"Nest"}`)
	if !outcome.OK || data["area"].(float64) != 600 || data["cells"].(float64) != float64(inside) || data["labels"] == nil {
		t.Fatalf("summarize_region: %s %v", outcome.Message, data)
	}
	genes := data["genes"].(map[string]any)
	if len(genes["abundant"].([]any)) == 0 || genes["cellsInside"].(float64) != float64(inside) {
		t.Fatalf("its genes: %v", genes)
	}
	if outcome, _ := call("summarize_region", `{"project":"Queries"}`); outcome.OK || !strings.Contains(outcome.Message, "name a region") {
		t.Fatalf("no place: %s", outcome.Message)
	}
}

// The filters the page saved on the Lung bundle (the validation projects, 0.1.0 to 0.6.0), read by
// the program as trees and applied to the same cells through the page's route: each keeps the very
// cells the page recorded, by their SHA-256.
func TestThePagesFiltersKeepTheSameCellsInGo(t *testing.T) {
	root := os.Getenv("SITUCRAFT_DATA")
	if root == "" {
		root = filepath.Join("validation", "cache")
	}
	bundle := filepath.Join(root, "xenium-lung-2fov")
	if _, err := os.Stat(filepath.Join(bundle, "experiment.xenium")); err != nil {
		t.Skip("the Lung bundle is not present (node validation/fetch.mjs xenium-lung-2fov)")
	}
	if testing.Short() {
		t.Skip("short mode")
	}
	dir := t.TempDir()
	a, err := newApp(config{dataDir: dir, cacheDir: filepath.Join(dir, "cache"), cacheLimit: 10e9, files: []string{bundle}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.jobs.Close() })
	t.Cleanup(a.views.closeAll)
	h, _ := a.handler()
	f := &refineFixture{t: t, a: a, handler: protect(h, "127.0.0.1", defaultPort), folder: bundle}
	for _, target := range []string{"/api/local/0/segmentation", "/api/local/0/cache"} {
		var started struct{ Job *jobs.Job }
		json.NewDecoder(f.post(target, "").Body).Decode(&started)
		if started.Job != nil {
			f.wait(started.Job.ID)
		}
	}
	files, _ := filepath.Glob(filepath.Join("validation", "projects", "*.situcraft.json"))
	checked := 0
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Datasets []struct {
				ID   string `json:"id"`
				Path string `json:"path"`
			} `json:"datasets"`
			Artifacts []json.RawMessage `json:"artifacts"`
			Regions   json.RawMessage   `json:"regions"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		var filters []json.RawMessage
		for _, a := range p.Artifacts {
			var head struct{ Kind string }
			if json.Unmarshal(a, &head) == nil && head.Kind == "filter" {
				filters = append(filters, a)
			}
		}
		for _, d := range p.Datasets {
			if filepath.Base(d.Path) != "xenium-lung-2fov" {
				continue
			}
			for _, raw := range filters {
				var filter struct {
					ID, Name, Dataset string
					Version           int
					Result            struct {
						Cells, Kept int
						SHA256      string
					}
				}
				json.Unmarshal(raw, &filter)
				if filter.Dataset != d.ID {
					continue
				}
				body, _ := json.Marshal(map[string]any{"dataset": d.ID, "where": map[string]string{"filter": filter.ID}, "context": map[string]any{"filters": filters, "regions": p.Regions}})
				rec := f.post("/api/local/0/query", string(body))
				var answer struct {
					Cells, Kept  int
					SHA256, Text string
					Keep         []byte
				}
				json.NewDecoder(rec.Body).Decode(&answer)
				if rec.Code != http.StatusOK || answer.Cells != filter.Result.Cells || answer.Kept != filter.Result.Kept || answer.SHA256 != filter.Result.SHA256 {
					t.Errorf("%s: %s v%d kept %d of %d (%.12s…), the page %d of %d (%.12s…): %d %s", filepath.Base(file), filter.Name, filter.Version, answer.Kept, answer.Cells, answer.SHA256, filter.Result.Kept, filter.Result.Cells, filter.Result.SHA256, rec.Code, rec.Body)
				}
				if sum := sha256.Sum256(answer.Keep); hex.EncodeToString(sum[:]) != answer.SHA256 {
					t.Errorf("%s: the bytes of which cells are not those summed", filter.Name)
				}
				checked++
			}
		}
	}
	if checked < 10 {
		t.Fatalf("%d filters checked", checked)
	}
	// A tree refused says why.
	rec := f.post("/api/local/0/query", `{"dataset":"d","where":{"attribute":"size","min":1}}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `no attribute \"size\"`) {
		t.Errorf("a malformed tree: %d %s", rec.Code, rec.Body)
	}
}
