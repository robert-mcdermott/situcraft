package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
	"situcraft/internal/spatial"
	"situcraft/internal/xenium"
)

// codesOf writes numbers a cell as the page sends them: base64 of the count and the numbers, and
// their checksum.
func codesOf(v []int32) (string, string) {
	raw := make([]byte, 4+4*len(v))
	binary.LittleEndian.PutUint32(raw, uint32(len(v)))
	for i, x := range v {
		binary.LittleEndian.PutUint32(raw[4+4*i:], uint32(x))
	}
	sum := sha256.Sum256(raw)
	return base64.StdEncoding.EncodeToString(raw), hex.EncodeToString(sum[:])
}

// Spatial analyses of a small simulated tissue's instrument cells, labeled by turns: enrichment over
// a graph (within two strata too) and co-occurrence over the cells chosen, each made by a job and
// recorded with its inputs; asked again, answered at once; the cells read back as the page reads
// them; what cannot be asked refused.
func TestSpatialAnalyses(t *testing.T) {
	f := newRefineFixture(t, true)
	all, _ := f.a.artifacts.List()
	var seg artifacts.Manifest
	for _, a := range all {
		if a.Kind == "segmentation" && isBundleOwn(a) {
			seg = a
		}
	}
	b, err := xenium.Open(f.folder, xenium.Options{CacheDir: f.a.cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	dir, err := f.a.segmentationFolder(context.Background(), b, seg)
	if err != nil {
		t.Fatal(err)
	}
	cells, err := segment.ReadCells(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := len(cells)
	labels := make([]int32, n)
	strata := make([]int32, n)
	keep := make([]bool, n)
	for i := range labels {
		labels[i] = int32(i%4) - 1 // a quarter without a label
		if cells[i].X > 40 {
			strata[i] = 1
		}
		keep[i] = true
	}
	codes, sum := codesOf(labels)
	labelsJSON := fmt.Sprintf(`{"id":"lab-x","kind":"labels","name":"By turns","level":0,"names":["A","B","C"],"codes":%q,"checksum":%q}`, codes, sum)
	stCodes, stSum := codesOf(strata)
	strataJSON := fmt.Sprintf(`{"regions":[{"id":"reg-1","name":"Right","version":1}],"codes":%q,"checksum":%q}`, stCodes, stSum)
	var g struct{ ID string }
	rec := f.post("/api/local/0/graph", fmt.Sprintf(`{"segmentation":%q,"parameters":{"kind":"delaunay"},"cells":%s}`, seg.ID, chosenCells(keep, "")))
	var started struct {
		ID  string
		Job *struct{ ID string }
	}
	json.NewDecoder(rec.Body).Decode(&started)
	if started.Job != nil {
		f.wait(started.Job.ID)
	}
	g.ID = started.ID
	run := func(body string) (string, *artifacts.Manifest) {
		t.Helper()
		rec := f.post("/api/local/0/spatial", body)
		var answer struct {
			ID       string
			Artifact *artifacts.Manifest
			Job      *struct{ ID string }
		}
		if err := json.NewDecoder(rec.Body).Decode(&answer); err != nil || answer.ID == "" {
			t.Fatalf("%d: %s", rec.Code, body[:min(200, len(body))])
		}
		if answer.Job != nil {
			f.wait(answer.Job.ID)
		}
		m, err := f.a.artifacts.Get(answer.ID)
		if err != nil || m.State != artifacts.Complete {
			t.Fatalf("the analysis %s: %v %+v", answer.ID, err, m.Findings)
		}
		return answer.ID, answer.Artifact
	}
	enrichment := fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"graph":%q,"labels":%s,"parameters":{"permutations":200,"seed":3}}`, seg.ID, g.ID, labelsJSON)
	id, _ := run(enrichment)
	m, _ := f.a.artifacts.Get(id)
	var s spatial.AnalysisSummary
	json.Unmarshal(m.Summary, &s)
	if m.Kind != "spatial" || s.Question != "enrichment" || s.Graph == nil || s.Graph.ID != g.ID || s.Labels.Count != 3 || s.Cells != n || s.Labeled != n-n/4-btoi(n%4 > 0) {
		t.Errorf("the enrichment: %s, %+v", m.Name, s)
	}
	if _, again := run(enrichment); again == nil || again.ID != id {
		t.Error("the enrichment asked again: not answered at once")
	}
	p, _ := f.a.artifacts.Path(id, spatial.FileAnalysis)
	a, err := spatial.ReadAnalysis(p[:len(p)-len(spatial.FileAnalysis)])
	if err != nil || len(a.Enrichment.Z) != 9 || a.Enrichment.Strata != 1 {
		t.Fatalf("analysis.json: %v %+v", err, a)
	}
	// Within the two strata: another analysis, its strata recorded.
	within, _ := run(fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"graph":%q,"labels":%s,"strata":%s,"parameters":{"permutations":200,"seed":3}}`, seg.ID, g.ID, labelsJSON, strataJSON))
	wm, _ := f.a.artifacts.Get(within)
	json.Unmarshal(wm.Summary, &s)
	if within == id || len(s.Strata) != 1 || s.Strata[0].Name != "Right" {
		t.Errorf("within regions: %s %+v", within, s.Strata)
	}
	// The cells as the page reads them.
	raw := request(t, f.handler, http.MethodGet, "/api/analyses/"+id+"/cells", nil, nil).Body.Bytes()
	if len(raw) != 4+12*n || int(binary.LittleEndian.Uint32(raw)) != n || int32(binary.LittleEndian.Uint32(raw[4+4*n:])) != labels[0] {
		t.Errorf("the cells: %d bytes", len(raw))
	}
	co, _ := run(fmt.Sprintf(`{"question":"cooccurrence","segmentation":%q,"cells":%s,"labels":%s,"parameters":{"distances":[10,20,40]}}`, seg.ID, chosenCells(keep, ""), labelsJSON))
	cm, _ := f.a.artifacts.Get(co)
	json.Unmarshal(cm.Summary, &s)
	if s.Question != "cooccurrence" || s.Distances != 3 {
		t.Errorf("the co-occurrence: %+v", s)
	}
	// Nearest distances, and Ripley's K in the tissue and in a region.
	dist, _ := run(fmt.Sprintf(`{"question":"distances","segmentation":%q,"cells":%s,"labels":%s,"parameters":{"permutations":49,"reach":40,"step":5}}`, seg.ID, chosenCells(keep, ""), labelsJSON))
	dm, _ := f.a.artifacts.Get(dist)
	json.Unmarshal(dm.Summary, &s)
	if s.Question != "distances" {
		t.Errorf("nearest distances: %+v", s)
	}
	tissue, _ := run(fmt.Sprintf(`{"question":"ripley","segmentation":%q,"cells":%s,"labels":%s,"window":{"kind":"tissue","closing":5},"parameters":{"radii":[5,10,15],"permutations":19}}`, seg.ID, chosenCells(keep, ""), labelsJSON))
	tm, _ := f.a.artifacts.Get(tissue)
	json.Unmarshal(tm.Summary, &s)
	if s.Question != "ripley" || s.Window == nil || s.Window.Kind != "tissue" || !(s.Window.Area > 1000) || s.Window.Closing != 5 {
		t.Errorf("Ripley's K in the tissue: %+v %+v", s, s.Window)
	}
	region, _ := run(fmt.Sprintf(`{"question":"ripley","segmentation":%q,"cells":%s,"labels":%s,"window":{"kind":"region","region":{"id":"reg-2","name":"Box","version":1},"geometry":[[[[0,0],[60,0],[60,50],[0,50]]]]},"parameters":{"radii":[5,10],"permutations":19}}`, seg.ID, chosenCells(keep, ""), labelsJSON))
	rm, _ := f.a.artifacts.Get(region)
	json.Unmarshal(rm.Summary, &s)
	if s.Window == nil || s.Window.Kind != "region" || s.Window.Area != 3000 || s.Window.Region.Name != "Box" {
		t.Errorf("Ripley's K in a region: %+v", s.Window)
	}
	bad := func(sumOf string) string {
		return fmt.Sprintf(`{"id":"lab-x","kind":"labels","name":"By turns","level":0,"names":["A","B","C"],"codes":%q,"checksum":%q}`, codes, sumOf)
	}
	short, shortSum := codesOf(labels[:10])
	for _, c := range []struct{ body, want string }{
		{fmt.Sprintf(`{"question":"niches","segmentation":%q,"graph":%q,"labels":%s}`, seg.ID, g.ID, labelsJSON), "no question"},
		{fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"labels":%s}`, seg.ID, labelsJSON), "is of a graph"},
		{fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"graph":%q,"labels":%s,"parameters":{"weight":"boundary"}}`, seg.ID, g.ID, labelsJSON), "contact graph"},
		{fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"graph":%q,"labels":%s,"parameters":{"permutations":5}}`, seg.ID, g.ID, labelsJSON), "100 to 100,000"},
		{fmt.Sprintf(`{"question":"cooccurrence","segmentation":%q,"cells":%s,"labels":%s,"strata":%s}`, seg.ID, chosenCells(keep, ""), labelsJSON, strataJSON), "within regions"},
		{fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"graph":%q,"labels":%s}`, seg.ID, g.ID, bad(strings.Repeat("0", 64))), "checksum"},
		{fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"graph":%q,"labels":{"names":["A"],"codes":%q,"checksum":%q}}`, seg.ID, g.ID, short, shortSum), "the segmentation has"},
		{fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"graph":"gra-0000000000","labels":%s}`, seg.ID, labelsJSON), "No such graph"},
		{fmt.Sprintf(`{"question":"ripley","segmentation":%q,"cells":%s,"labels":%s,"window":{"kind":"hull"}}`, seg.ID, chosenCells(keep, ""), labelsJSON), "no window"},
		{fmt.Sprintf(`{"question":"ripley","segmentation":%q,"cells":%s,"labels":%s,"window":{"kind":"region","region":{"id":"r","name":"r","version":1},"geometry":[[[[0,0],[1,1]]]]}}`, seg.ID, chosenCells(keep, ""), labelsJSON), "fewer than three"},
		{fmt.Sprintf(`{"question":"ripley","segmentation":%q,"cells":%s,"labels":%s,"parameters":{"correction":"translation"}}`, seg.ID, chosenCells(keep, ""), labelsJSON), "isotropic or none"},
		{fmt.Sprintf(`{"question":"distances","segmentation":%q,"cells":%s,"labels":%s,"window":{"kind":"tissue"}}`, seg.ID, chosenCells(keep, ""), labelsJSON), "Only Ripley"},
		{fmt.Sprintf(`{"question":"distances","segmentation":%q,"cells":%s,"labels":%s,"parameters":{"reach":0}}`, seg.ID, chosenCells(keep, ""), labelsJSON), "reach"},
	} {
		if rec := f.post("/api/local/0/spatial", c.body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: %d %s", c.want, rec.Code, rec.Body)
		}
	}
	if rec := request(t, f.handler, http.MethodGet, "/api/analyses/spa-0000000000/cells", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("no such analysis: %d", rec.Code)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
