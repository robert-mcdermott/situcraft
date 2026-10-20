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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
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
	// Regions compared, two overlapping, and a profile across one's edge, in the tissue.
	twoRegions := `[{"region":{"id":"reg-2","name":"Box","version":1},"geometry":[[[[0,0],[60,0],[60,50],[0,50]]]]},{"region":{"id":"reg-3","name":"Strip","version":2},"geometry":[[[[40,0],[90,0],[90,30],[40,30]]]]}]`
	compared, _ := run(fmt.Sprintf(`{"question":"regions","segmentation":%q,"cells":%s,"labels":%s,"window":{"kind":"tissue","closing":5},"regions":%s}`, seg.ID, chosenCells(keep, ""), labelsJSON, twoRegions))
	pm, _ := f.a.artifacts.Get(compared)
	s = spatial.AnalysisSummary{}
	json.Unmarshal(pm.Summary, &s)
	if s.Question != "regions" || len(s.Rows) != 2 || s.Rows[1].Name != "Strip" || s.Window == nil || s.Window.Kind != "tissue" {
		t.Errorf("regions compared: %+v", s)
	}
	described, _ := f.a.artifacts.Path(compared, spatial.FileAnalysis)
	an, err := spatial.ReadAnalysis(filepath.Dir(described))
	if err != nil {
		t.Fatal(err)
	}
	if rr := an.Regions; rr == nil || len(rr.Area) != 3 || !(rr.Area[0] > 0 && rr.Area[0] <= 3000) || rr.Labeled[0]+rr.Labeled[2] > n {
		t.Errorf("regions compared: %+v", an.Regions)
	}
	raw = request(t, f.handler, http.MethodGet, "/api/analyses/"+compared+"/cells", nil, nil).Body.Bytes()
	if len(raw) != 4+12*n {
		t.Fatalf("the regions' cells: %d bytes", len(raw))
	}
	masks := map[int32]int{}
	for i := 0; i < n; i++ {
		masks[int32(binary.LittleEndian.Uint32(raw[4+8*n+4*i:]))]++
	}
	if masks[3] == 0 || masks[1] == 0 || masks[0] == 0 {
		t.Errorf("the cells' regions: %v (both, only the box, neither)", masks)
	}
	edge, _ := run(fmt.Sprintf(`{"question":"boundary","segmentation":%q,"cells":%s,"labels":%s,"regions":[{"region":{"id":"reg-2","name":"Box","version":1},"geometry":[[[[0,0],[60,0],[60,50],[0,50]]]]}],"parameters":{"reach":20,"step":5,"margin":5}}`, seg.ID, chosenCells(keep, ""), labelsJSON))
	em, _ := f.a.artifacts.Get(edge)
	s = spatial.AnalysisSummary{}
	json.Unmarshal(em.Summary, &s)
	if s.Question != "boundary" || s.Edge == nil || s.Edge.Name != "Box" || s.Window == nil || s.Window.Closing != 10 {
		t.Errorf("across the edge: %+v", s)
	}
	// Niches: each cell's 5 nearest labeled cells, two niches, numbered by size; or a radius.
	nicheID, _ := run(fmt.Sprintf(`{"question":"niches","segmentation":%q,"cells":%s,"labels":%s,"parameters":{"neighbors":5,"niches":2,"starts":3,"seeds":2}}`, seg.ID, chosenCells(keep, ""), labelsJSON))
	nm, _ := f.a.artifacts.Get(nicheID)
	s = spatial.AnalysisSummary{}
	json.Unmarshal(nm.Summary, &s)
	if s.Question != "niches" || len(s.Niches) != 2 || s.AgreementMin == nil || s.Niches[0].Cells < s.Niches[1].Cells || len(s.Niches[0].Defining) == 0 {
		t.Errorf("niches: %+v", s)
	}
	byRadius, _ := run(fmt.Sprintf(`{"question":"niches","segmentation":%q,"cells":%s,"labels":%s,"parameters":{"radius":15,"niches":2,"starts":2,"seeds":0}}`, seg.ID, chosenCells(keep, ""), labelsJSON))
	rd, _ := f.a.artifacts.Get(byRadius)
	var np spatial.NicheParams
	json.Unmarshal(rd.Lineage.Parameters, &np)
	if np.Radius != 15 || np.Neighbors != 0 {
		t.Errorf("niches by a radius: %s", rd.Lineage.Parameters)
	}
	// Moran's I of the genes over the graph, without labels, its local values for two genes.
	moranID, _ := run(fmt.Sprintf(`{"question":"moran","segmentation":%q,"graph":%q,"parameters":{"permutations":19,"local":2,"localPermutations":19}}`, seg.ID, g.ID))
	mm, _ := f.a.artifacts.Get(moranID)
	s = spatial.AnalysisSummary{}
	json.Unmarshal(mm.Summary, &s)
	if s.Question != "moran" || s.Genes == 0 || len(mm.Files) != 3 {
		t.Errorf("Moran's I: %+v, %d files", s, len(mm.Files))
	}
	loc := request(t, f.handler, http.MethodGet, "/api/analyses/"+moranID+"/local", nil, nil).Body.Bytes()
	if len(loc) < 8 || binary.LittleEndian.Uint32(loc[4:]) != 2 || len(loc) != 8+4*2+2*9*int(binary.LittleEndian.Uint32(loc)) {
		t.Errorf("the local values: %d bytes", len(loc))
	}
	bad := func(sumOf string) string {
		return fmt.Sprintf(`{"id":"lab-x","kind":"labels","name":"By turns","level":0,"names":["A","B","C"],"codes":%q,"checksum":%q}`, codes, sumOf)
	}
	short, shortSum := codesOf(labels[:10])
	for _, c := range []struct{ body, want string }{
		{fmt.Sprintf(`{"question":"hotspots","segmentation":%q,"graph":%q,"labels":%s}`, seg.ID, g.ID, labelsJSON), "no question"},
		{fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"labels":%s}`, seg.ID, labelsJSON), "are of a graph"},
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
		{fmt.Sprintf(`{"question":"niches","segmentation":%q,"cells":%s,"labels":%s,"parameters":{"neighbors":5,"radius":10}}`, seg.ID, chosenCells(keep, ""), labelsJSON), "one of them"},
		{fmt.Sprintf(`{"question":"niches","segmentation":%q,"cells":%s,"labels":%s,"parameters":{"niches":1}}`, seg.ID, chosenCells(keep, ""), labelsJSON), "2 to 30 niches"},
		{fmt.Sprintf(`{"question":"moran","segmentation":%q,"graph":%q,"labels":%s}`, seg.ID, g.ID, labelsJSON), "takes no labels"},
		{fmt.Sprintf(`{"question":"moran","segmentation":%q,"cells":%s}`, seg.ID, chosenCells(keep, "")), "of a graph"},
		{fmt.Sprintf(`{"question":"moran","segmentation":%q,"graph":%q,"parameters":{"weights":"distance"}}`, seg.ID, g.ID), "row (standardized) or binary"},
		{fmt.Sprintf(`{"question":"regions","segmentation":%q,"cells":%s,"labels":%s}`, seg.ID, chosenCells(keep, ""), labelsJSON), "1 to 20 regions"},
		{fmt.Sprintf(`{"question":"boundary","segmentation":%q,"cells":%s,"labels":%s,"regions":%s}`, seg.ID, chosenCells(keep, ""), labelsJSON, twoRegions), "of one region"},
		{fmt.Sprintf(`{"question":"distances","segmentation":%q,"cells":%s,"labels":%s,"regions":%s}`, seg.ID, chosenCells(keep, ""), labelsJSON, twoRegions), "Only regions compared"},
		{fmt.Sprintf(`{"question":"regions","segmentation":%q,"cells":%s,"labels":%s,"regions":%s,"window":{"kind":"region","region":{"id":"r","name":"r","version":1},"geometry":[[[[0,0],[9,0],[9,9]]]]}}`, seg.ID, chosenCells(keep, ""), labelsJSON, twoRegions), "within the tissue"},
		{fmt.Sprintf(`{"question":"regions","segmentation":%q,"cells":%s,"labels":%s,"regions":[{"region":{"id":"r","name":"r","version":1},"geometry":[[[[0,0],[9,0],[9,9]]]]},{"region":{"id":"r","name":"r","version":1},"geometry":[[[[0,0],[9,0],[9,9]]]]}]}`, seg.ID, chosenCells(keep, ""), labelsJSON), "once"},
		{fmt.Sprintf(`{"question":"boundary","segmentation":%q,"cells":%s,"labels":%s,"regions":[{"region":{"id":"r","name":"r","version":1},"geometry":[[[[0,0],[9,0],[9,9]]]]}],"parameters":{"margin":100}}`, seg.ID, chosenCells(keep, ""), labelsJSON), "margin"},
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

// A graph and a spatial analysis interrupted by SituCraft stopping are orphaned when it starts
// again, and resume to the artifacts the same requests name: asked again, they are answered at once.
func TestInterruptedGraphAndSpatialJobsResume(t *testing.T) {
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
	dir, err := f.a.segmentationFolder(context.Background(), b, seg)
	b.Close()
	if err != nil {
		t.Fatal(err)
	}
	cells, err := segment.ReadCells(dir)
	if err != nil {
		t.Fatal(err)
	}
	labels, keep := make([]int32, len(cells)), make([]bool, len(cells))
	for i := range labels {
		labels[i], keep[i] = int32(i%3), true
	}
	codes, sum := codesOf(labels)
	chosen := chosenCells(keep, "")
	graphBody := fmt.Sprintf(`{"segmentation":%q,"parameters":{"kind":"delaunay"},"cells":%s}`, seg.ID, chosen)
	spatialBody := fmt.Sprintf(`{"question":"distances","segmentation":%q,"cells":%s,"labels":{"id":"lab-x","kind":"labels","name":"By turns","level":0,"names":["A","B","C"],"codes":%q,"checksum":%q},"parameters":{"permutations":50,"seed":3}}`, seg.ID, chosen, codes, sum)

	// Each job's spec as its start handler writes it, SituCraft stopped while the first runs and the
	// second waits its turn.
	var gr graphRequest
	if err := json.Unmarshal([]byte(graphBody), &gr); err != nil {
		t.Fatal(err)
	}
	gs := graphSpec{Bundle: f.folder, Segmentation: seg.ID, Parameters: *gr.Parameters, Cells: *gr.Cells}
	gid := graphID(seg.ID, gr.Cells.Checksum, gs.Parameters)
	var sr spatialRequest
	if err := json.Unmarshal([]byte(spatialBody), &sr); err != nil {
		t.Fatal(err)
	}
	params, err := checkedParameters(sr.Question, sr.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	sid := spatialID(sr, params)
	sr.Parameters = params
	blocked := func(ctx context.Context, r *jobs.Run) error {
		<-ctx.Done()
		return ctx.Err()
	}
	var ids []string
	for _, j := range []struct {
		kind, key string
		spec      any
	}{{"graph", "graph\x00" + gid, gs}, {"spatial", "spatial\x00" + sid, spatialSpec{Bundle: f.folder, spatialRequest: sr}}} {
		job, _, err := f.a.jobs.Submit(j.kind, j.kind, j.key, j.spec, blocked)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, job.ID)
	}
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(10 * time.Millisecond) {
		j, _ := f.a.jobs.Get(ids[0])
		if j.State == jobs.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the graph's job did not start: %s", j.State)
		}
	}
	f.a.jobs.Close()

	// SituCraft again, on the same folders: both orphaned and resumable, and resumed.
	root := filepath.Dir(f.folder)
	a, err := newApp(config{dataDir: root, cacheDir: filepath.Join(root, "cache"), cacheLimit: 10e9, files: []string{f.folder}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.jobs.Close() })
	h, _ := a.handler()
	g := &refineFixture{t: t, a: a, handler: protect(h, "127.0.0.1", defaultPort), folder: f.folder}
	for _, id := range ids {
		j, ok := a.jobs.Get(id)
		if !ok || j.State != jobs.Orphaned || !j.Resumable {
			t.Fatalf("after starting again: %+v", j)
		}
		if rec := g.post("/api/jobs/"+id+"/resume", ""); rec.Code != http.StatusOK {
			t.Fatalf("resume: %d %s", rec.Code, rec.Body)
		}
		if done := g.wait(id); done.Runs != j.Runs+1 {
			t.Errorf("%s resumed: runs %d, before %d", j.Kind, done.Runs, j.Runs)
		}
	}
	for _, c := range []struct{ target, body, id string }{{"/api/local/0/graph", graphBody, gid}, {"/api/local/0/spatial", spatialBody, sid}} {
		var answer struct {
			ID       string
			Artifact *artifacts.Manifest
		}
		json.NewDecoder(g.post(c.target, c.body).Body).Decode(&answer)
		if answer.ID != c.id || answer.Artifact == nil || answer.Artifact.State != artifacts.Complete {
			t.Errorf("%s asked again: %s (resumed %s), %+v", c.target, answer.ID, c.id, answer.Artifact)
		}
	}
}
