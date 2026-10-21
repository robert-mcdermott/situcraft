package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// A result's recipe (recipes.go) made again by situcraft run (run.go): on the bundle it was made
// from, the very artifacts, by their ids, and a certificate situcraft verify verifies; again, the
// same files, on one thread as on many, and with a fixed clock the same certificate byte for byte;
// on another bundle, the steps that carry no cells the window chose, and those that do refused.
func TestARecipeIsMadeAgainWithoutAWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	f := newRefineFixture(t, true)
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
	dir, err := f.a.segmentationFolder(context.Background(), b, seg)
	b.Close()
	if err != nil {
		t.Fatal(err)
	}
	cells, err := segment.ReadCells(dir)
	if err != nil {
		t.Fatal(err)
	}
	run := func(target, body string) string {
		t.Helper()
		var started struct {
			ID  string
			Job *struct{ ID string }
		}
		rec := f.post(target, body)
		json.Unmarshal(rec.Body.Bytes(), &started)
		if started.Job != nil {
			f.wait(started.Job.ID)
		}
		if started.ID == "" {
			t.Fatalf("%s: no id (%d %s)", target, rec.Code, rec.Body)
		}
		return started.ID
	}
	// A clustering and its map (the program's alone), and a graph and its enrichment by labels the
	// window gave (each cell's label: cells it chose).
	clustering := run("/api/local/0/cluster", fmt.Sprintf(`{"segmentation":%q}`, seg.ID))
	embedding := run("/api/local/0/embed", fmt.Sprintf(`{"clustering":%q}`, clustering))
	keep, labels := make([]bool, len(cells)), make([]int32, len(cells))
	for i := range keep {
		keep[i], labels[i] = true, int32(i%3)
	}
	codes, sum := codesOf(labels)
	graph := run("/api/local/0/graph", fmt.Sprintf(`{"segmentation":%q,"parameters":{"kind":"delaunay"},"cells":%s}`, seg.ID, chosenCells(keep, "")))
	enrichment := run("/api/local/0/spatial", fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"graph":%q,"labels":{"id":"lab-x","kind":"labels","name":"By turns","level":0,"names":["A","B","C"],"codes":%q,"checksum":%q},"parameters":{"permutations":100,"seed":3}}`, seg.ID, graph, codes, sum))

	recipeOf := func(id string) (*resultRecipe, string) {
		t.Helper()
		rec := request(t, f.handler, http.MethodGet, "/api/artifacts/"+id+"/recipe?download=1", nil, nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Disposition"), id+".recipe.json") {
			t.Fatalf("the recipe of %s: %d %s", id, rec.Code, rec.Body)
		}
		r, err := readResultRecipe(rec.Body.Bytes())
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		path := filepath.Join(t.TempDir(), id+".recipe.json")
		os.WriteFile(path, rec.Body.Bytes(), 0o644)
		if bytes.Contains(rec.Body.Bytes(), []byte(f.folder)) || bytes.Contains(rec.Body.Bytes(), []byte(f.a.store.dir)) {
			t.Errorf("the recipe of %s keeps this computer's paths", id)
		}
		return r, path
	}
	mapRecipe, mapPath := recipeOf(embedding)
	var kinds []string
	for _, s := range mapRecipe.Steps {
		kinds = append(kinds, fmt.Sprintf("%d %s%v %v", s.N, s.Job, s.Inputs, s.Carries))
	}
	if strings.Join(kinds, "; ") != "1 segmentation[] false; 2 clustering[1] false; 3 embedding[2] false" || mapRecipe.Result != 3 || len(mapRecipe.Bundles) != 1 || mapRecipe.Steps[2].Made != embedding {
		t.Fatalf("the map's recipe: %s; result %d; bundles %+v", strings.Join(kinds, "; "), mapRecipe.Result, mapRecipe.Bundles)
	}
	var spec bytes.Buffer
	json.Compact(&spec, mapRecipe.Steps[1].Spec)
	if !strings.Contains(spec.String(), `"segmentation":"situcraft:step/1"`) || !strings.Contains(spec.String(), `"bundle":"situcraft:bundle/simulated"`) {
		t.Errorf("stand-ins: %s", spec.String())
	}
	spatialRecipe, spatialPath := recipeOf(enrichment)
	// The graph's cells (every cell) are chosen again anywhere; the enrichment's labels, given by
	// the window from no step of the recipe, only on this bundle.
	if n := len(spatialRecipe.Steps); n != 3 || !spatialRecipe.Steps[1].Carries || !spatialRecipe.Steps[1].Portable || !spatialRecipe.Steps[2].Carries || spatialRecipe.Steps[2].Portable || spatialRecipe.Steps[2].Job != "spatial" {
		t.Fatalf("the enrichment's recipe: %+v", spatialRecipe.Steps)
	}
	// As an action, in words.
	if outcome, ok := f.a.programAction(context.Background(), "recipe", json.RawMessage(`{"id":"`+enrichment+`"}`)); !ok || !outcome.OK || !strings.Contains(outcome.Message, "3 steps from 1 bundle") || !strings.Contains(outcome.Message, "1 step carries cells the window chose that only its own bundle has") || !strings.Contains(outcome.Message, "for 1 step are chosen there again") {
		t.Errorf("the recipe action: %+v", outcome)
	}

	// Made again on the same bundle: the same artifacts, and a certificate verify verifies.
	clock := "2026-01-01T00:00:00Z"
	makeIt := func(recipe, bundle string, extra ...string) (int, *runReport, string, string) {
		t.Helper()
		out := filepath.Join(t.TempDir(), "run")
		var stdout, stderr bytes.Buffer
		code := runRun(append([]string{"--recipe", recipe, "--bundle", bundle, "--out", out}, extra...), &stdout, &stderr)
		raw, _ := os.ReadFile(filepath.Join(out, "run.json"))
		var report runReport
		json.Unmarshal(raw, &report)
		return code, &report, out, stdout.String() + stderr.String()
	}
	code, first, out1, said := makeIt(spatialPath, f.folder, "--clock", clock)
	if code != exitRunMade || len(first.Steps) != 3 || first.Exit.Code != 0 {
		t.Fatalf("made again: exit %d: %s", code, said)
	}
	for i, s := range first.Steps {
		// (The instrument's segmentation's files are its bundle's.)
		if !s.Same || s.ID != spatialRecipe.Steps[i].Made || s.State != "made" || len(s.Files) == 0 && s.Job != "segmentation" {
			t.Errorf("step %d: %+v", s.N, s)
		}
	}
	if !first.Bundles[0].Same || first.Result.ID != enrichment || first.Clock != clock {
		t.Errorf("run.json: bundles %+v, result %+v, clock %s", first.Bundles, first.Result, first.Clock)
	}
	if raw, _ := os.ReadFile(filepath.Join(out1, "run.json")); bytes.Contains(raw, []byte(f.folder)) {
		t.Error("run.json keeps the bundle's path")
	}
	var verified bytes.Buffer
	if code := runVerify([]string{"--data", filepath.Dir(f.folder), "--no-page", filepath.Join(out1, "certificate.zip")}, &verified, io.Discard); code != exitVerified {
		t.Fatalf("verify: exit %d\n%s", code, verified.String())
	}
	// Again, on one thread: the same files; with the same clock, the same certificate.
	code, second, out2, said := makeIt(spatialPath, f.folder, "--clock", clock, "--threads", "1")
	if code != exitRunMade || !sameRunFiles(first, second) {
		t.Fatalf("again: exit %d, the same files %v: %s", code, sameRunFiles(first, second), said)
	}
	one, _ := os.ReadFile(filepath.Join(out1, "certificate.zip"))
	two, _ := os.ReadFile(filepath.Join(out2, "certificate.zip"))
	if !bytes.Equal(one, two) || first.Cert.Fingerprint != second.Cert.Fingerprint {
		t.Errorf("the same recipe, bundle and clock, another certificate: %s, %s", first.Cert.Fingerprint, second.Cert.Fingerprint)
	}
	// The map's recipe, whose clustering's and map's files record how long they took: the same
	// content again.
	code, mapFirst, _, said := makeIt(mapPath, f.folder)
	_, mapSecond, _, _ := makeIt(mapPath, f.folder)
	if code != exitRunMade || !sameRunFiles(mapFirst, mapSecond) || mapFirst.Steps[1].Content == "" || mapFirst.Result.ID != embedding {
		t.Fatalf("the map again: exit %d: %s", code, said)
	}

	// The same files on every processor: those made here pinned, so that CI's other processor makes
	// them again (a step's files by SHA-256, a clustering's and a map's content).
	pins := map[string]string{"the enrichment's": runDigest(first), "the map's": runDigest(mapFirst)}
	for what, want := range map[string]string{"the enrichment's": pinnedEnrichmentRun, "the map's": pinnedMapRun} {
		if pins[what] != want {
			t.Errorf("%s run made other files than pinned: %s (pinned %s)", what, pins[what], want)
		}
	}

	// Another bundle: the map is made (other cells, other ids); the enrichment, which carries the
	// window's cells, is refused before anything is made.
	other := newRefineFixtureOf(t, false, 70, 60)
	code, elsewhere, _, said := makeIt(mapPath, other.folder)
	if code != exitRunMade || elsewhere.Bundles[0].Same || elsewhere.Steps[0].Same || elsewhere.Result.ID == embedding {
		t.Fatalf("on another bundle: exit %d: %s", code, said)
	}
	if code, _, _, said := makeIt(spatialPath, other.folder); code != exitRunNotRun || !strings.Contains(said, "1 step carries cells the window chose") || !strings.Contains(said, "step 3, Neighborhood enrichment") {
		t.Fatalf("carried cells on another bundle: exit %d: %s", code, said)
	}
	if code, _, _, said := makeIt(mapPath, other.folder, "--dry-run"); code != exitRunMade || !strings.Contains(said, "3. Map of") || !strings.Contains(said, "nothing made") {
		t.Errorf("--dry-run: exit %d: %s", code, said)
	}
}

// What situcraft run refuses, with why: a wrong command line, a recipe it does not make, a bundle
// it does not have, a folder not empty.
func TestRunRefusesWithWhy(t *testing.T) {
	dir := t.TempDir()
	recipe := filepath.Join(dir, "r.json")
	os.WriteFile(recipe, []byte(`{"format":"situcraft-recipe","version":1,"kind":"result","name":"x","software":{"name":"SituCraft","version":"0.9.0"},"result":1,"bundles":[{"role":"lung","name":"lung","files":[]}],"steps":[{"n":1,"kind":"segmentation","name":"Instrument segmentation","made":"seg-1","job":"segmentation","spec":{"bundle":"situcraft:bundle/lung"}}],"run":""}`), 0o644)
	refinement := filepath.Join(dir, "refine.json")
	os.WriteFile(refinement, []byte(`{"format":"situcraft-recipe","version":1,"kind":"refinement"}`), 0o644)
	newer := filepath.Join(dir, "newer.json")
	os.WriteFile(newer, []byte(`{"format":"situcraft-recipe","version":9,"kind":"result"}`), 0o644)
	badStep := filepath.Join(dir, "bad.json")
	os.WriteFile(badStep, []byte(`{"format":"situcraft-recipe","version":1,"kind":"result","name":"x","software":{"name":"SituCraft","version":"0.9.0"},"result":1,"bundles":[{"role":"lung","name":"lung","files":[]}],"steps":[{"n":1,"kind":"clustering","name":"C","made":"clu-1","job":"clustering","spec":{"bundle":"situcraft:bundle/lung","segmentation":"situcraft:step/1"}}],"run":""}`), 0o644)
	full := filepath.Join(dir, "full")
	os.MkdirAll(full, 0o755)
	os.WriteFile(filepath.Join(full, "x"), nil, 0o644)
	for _, c := range []struct {
		args []string
		says string
	}{
		{[]string{"--bundle", dir, "--out", filepath.Join(dir, "o")}, "name the recipe with --recipe"},
		{[]string{"--recipe", recipe, "--out", filepath.Join(dir, "o")}, "name the bundle's folder with --bundle"},
		{[]string{"--recipe", recipe, "--bundle", dir}, "with --out"},
		{[]string{"--recipe", recipe, "--bundle", dir, "--out", filepath.Join(dir, "o"), "--clock", "noon"}, "is not a time"},
		{[]string{"--recipe", refinement, "--bundle", dir, "--out", filepath.Join(dir, "o")}, "situcraft refine --recipe makes it"},
		{[]string{"--recipe", newer, "--bundle", dir, "--out", filepath.Join(dir, "o")}, "a recipe of version 9"},
		{[]string{"--recipe", badStep, "--bundle", dir, "--out", filepath.Join(dir, "o")}, "not one before it"},
		{[]string{"--recipe", recipe, "--bundle", dir, "--out", filepath.Join(dir, "o")}, "is not a Xenium output bundle"},
		{[]string{"--recipe", recipe, "--bundle", "kidney=" + dir, "--out", filepath.Join(dir, "o")}, "needs the bundle lung"},
		{[]string{"--recipe", recipe, "--bundle", "a=" + dir, "--bundle", "b=" + dir, "--out", filepath.Join(dir, "o")}, "needs the bundle lung"},
	} {
		var stderr bytes.Buffer
		if code := runRun(c.args, io.Discard, &stderr); code != exitRunNotRun || !strings.Contains(stderr.String(), c.says) {
			t.Errorf("%v: exit %d, %q; want it to say %q", c.args, code, stderr.String(), c.says)
		}
	}
	_ = full
}

// The digests of the runs above on the simulated tissue, as made on arm64 (and on amd64 in CI).
const (
	pinnedEnrichmentRun = "e61cc8e41b5a310fd1d14117f61fa70f941ec05449650048f63d9f7f71a6eb14"
	pinnedMapRun        = "e652a1355b9c53cbd6355242fb42068e2018b03a030b547db8cc125d9fac6767"
)

// runDigest is the SHA-256 of what a run made: each step's id and its files by SHA-256, or a
// clustering's, a map's and labels' content.
func runDigest(r *runReport) string {
	var b strings.Builder
	for _, s := range r.Steps {
		fmt.Fprintf(&b, "%d %s %s\n", s.N, s.ID, s.Content)
		if s.Content != "" {
			continue
		}
		for _, f := range s.Files {
			fmt.Fprintf(&b, "  %s %s\n", f.Path, f.SHA256)
		}
	}
	return sha256Hex([]byte(b.String()))
}

// sameRunFiles says whether two runs made the same files (each step's files, or for a clustering,
// a map and labels, its content: their files also record how long the run took).
func sameRunFiles(a, b *runReport) bool {
	if len(a.Steps) != len(b.Steps) {
		return false
	}
	for i := range a.Steps {
		x, y := a.Steps[i], b.Steps[i]
		if x.ID != y.ID || x.Content != y.Content {
			return false
		}
		if x.Content != "" {
			continue
		}
		left, _ := json.Marshal(x.Files)
		right, _ := json.Marshal(y.Files)
		if !bytes.Equal(left, right) {
			return false
		}
	}
	return true
}

// The window's choices made again on another bundle (recipe_choices.go): a clustering of a saved
// filter's cells, a label set from the simulated tissue's markers and an enrichment by its labels
// over a graph of every cell; each recipe says so only when the program, choosing them on this
// bundle, gives the very cells it was given; on another bundle they are chosen there, and the run
// is verified.
func TestTheWindowsChoicesAreMadeAgainOnAnotherBundle(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
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
	run := func(target, body string) string {
		t.Helper()
		var started struct {
			ID  string
			Job *struct{ ID string }
		}
		rec := f.post(target, body)
		json.Unmarshal(rec.Body.Bytes(), &started)
		if started.Job != nil {
			f.wait(started.Job.ID)
		}
		if started.ID == "" {
			t.Fatalf("%s: no id (%d %s)", target, rec.Code, rec.Body)
		}
		return started.ID
	}
	// A saved filter, as the window keeps it in a project, and its cells as the window sends them.
	project := `{"format":"situcraft-project","version":1,"id":"p-choices","name":"Choices","created":"2026-10-20T10:00:00Z","modified":"2026-10-20T10:00:00Z","datasets":[{"id":"d-1","name":"simulated","path":` + jsonQuote(f.folder) + `}],"artifacts":[{"kind":"filter","id":"f-0123456789abcdef","name":"Enough signal","version":1,"dataset":"d-1","conditions":[{"attribute":"transcripts","min":60,"max":null}],"result":{}}]}`
	os.MkdirAll(filepath.Join(f.a.store.dir, "projects"), 0o755)
	os.WriteFile(filepath.Join(f.a.store.dir, "projects", "p-choices.json"), []byte(project), 0o644)
	c := choiceContext{a: f.a, ctx: context.Background(), bundle: f.folder, made: map[int]string{}}
	keep, err := c.filterKeep(&recipeFilter{ID: "f-0123456789abcdef", Name: "Enough signal v1", Conditions: []json.RawMessage{json.RawMessage(`{"attribute":"transcripts","min":60,"max":null}`)}})
	if err != nil {
		t.Fatal(err)
	}
	b64, sum := keepPayload(keep)
	filtered := run("/api/local/0/cluster", fmt.Sprintf(`{"segmentation":%q,"filter":{"id":"f-0123456789abcdef","name":"Enough signal v1","checksum":%q,"keep":%q}}`, seg.ID, sum, b64))
	// Labels from the simulated tissue's markers, and an enrichment by them over every cell's graph.
	clustering := run("/api/local/0/cluster", fmt.Sprintf(`{"segmentation":%q}`, seg.ID))
	var starters struct {
		Starters []struct {
			ID  string
			Set json.RawMessage
		}
	}
	json.Unmarshal(request(t, f.handler, http.MethodGet, "/api/local/0/clusterings/"+clustering+"/markers", nil, nil).Body.Bytes(), &starters)
	var markers json.RawMessage
	for _, s := range starters.Starters {
		if s.ID == "simulated" {
			markers = s.Set
		}
	}
	if markers == nil {
		t.Fatal("no simulated marker set")
	}
	labelSet := run("/api/local/0/label", fmt.Sprintf(`{"clustering":%q,"markers":%s}`, clustering, markers))
	n, _ := c.segmentationCells(seg.ID)
	every := make([]bool, n)
	for i := range every {
		every[i] = true
	}
	graph := run("/api/local/0/graph", fmt.Sprintf(`{"segmentation":%q,"parameters":{"kind":"delaunay"},"cells":%s}`, seg.ID, chosenCells(every, "")))
	c.made = map[int]string{1: labelSet}
	labels, err := c.labelsOf(&recipeLabels{Step: 1, Level: 0, Name: "Simulated tissue's types"}, seg.ID)
	if err != nil {
		t.Fatal(err)
	}
	labelsJSON, _ := json.Marshal(labels)
	enrichment := run("/api/local/0/spatial", fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"graph":%q,"labels":%s,"parameters":{"permutations":100,"seed":3}}`, seg.ID, graph, labelsJSON))

	write := func(id string) (*resultRecipe, string) {
		t.Helper()
		rec := request(t, f.handler, http.MethodGet, "/api/artifacts/"+id+"/recipe", nil, nil)
		r, err := readResultRecipe(rec.Body.Bytes())
		if err != nil {
			t.Fatalf("%s: %v (%s)", id, err, rec.Body)
		}
		path := filepath.Join(t.TempDir(), id+".recipe.json")
		os.WriteFile(path, rec.Body.Bytes(), 0o644)
		return r, path
	}
	clusterRecipe, clusterPath := write(filtered)
	if s := clusterRecipe.Steps[1]; !s.Carries || !s.Portable || len(s.Choices) != 1 || s.Choices[0].Filter == nil || s.Choices[0].Filter.Name != "Enough signal v1" {
		t.Fatalf("the filtered clustering's recipe: %+v", s)
	}
	enrichRecipe, enrichPath := write(enrichment)
	var steps []string
	for _, s := range enrichRecipe.Steps {
		steps = append(steps, fmt.Sprintf("%s portable=%v", s.Job, s.Portable || !s.Carries))
	}
	if strings.Join(steps, "; ") != "segmentation portable=true; graph portable=true; clustering portable=true; labels portable=true; spatial portable=true" {
		t.Fatalf("the enrichment's recipe: %s", strings.Join(steps, "; "))
	}

	other := newRefineFixtureOf(t, false, 70, 60)
	for _, path := range []string{clusterPath, enrichPath} {
		out := filepath.Join(t.TempDir(), "run")
		var said bytes.Buffer
		code := runRun([]string{"--recipe", path, "--bundle", other.folder, "--out", out}, &said, &said)
		raw, _ := os.ReadFile(filepath.Join(out, "run.json"))
		var report runReport
		json.Unmarshal(raw, &report)
		rechosen := 0
		for _, s := range report.Steps {
			if s.Rechosen {
				rechosen++
			}
		}
		if code != exitRunMade || rechosen == 0 || report.Result.Same {
			t.Fatalf("%s on another bundle: exit %d, %d rechosen: %s", filepath.Base(path), code, rechosen, said.String())
		}
		var verified bytes.Buffer
		if code := runVerify([]string{"--data", filepath.Dir(other.folder), "--no-page", filepath.Join(out, "certificate.zip")}, &verified, io.Discard); code != exitVerified {
			t.Fatalf("verify %s: exit %d\n%s", filepath.Base(path), code, verified.String())
		}
	}
}

// A batch: a recipe made on every bundle of a folder, each in a folder of its own, batch.json
// saying how each went; one of them alone, as a cluster's array job makes it (--index); a list of
// bundles; a recipe one bundle cannot make, the others made.
func TestARecipeIsMadeOnABatch(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	f := newRefineFixture(t, true)
	other := newRefineFixtureOf(t, false, 70, 60)
	clustering := ""
	{
		var started struct {
			ID  string
			Job *struct{ ID string }
		}
		all, _ := f.a.artifacts.List()
		var seg artifacts.Manifest
		for _, m := range all {
			if m.Kind == "segmentation" && isBundleOwn(m) {
				seg = m
			}
		}
		json.Unmarshal(f.post("/api/local/0/cluster", fmt.Sprintf(`{"segmentation":%q}`, seg.ID)).Body.Bytes(), &started)
		if started.Job != nil {
			f.wait(started.Job.ID)
		}
		clustering = started.ID
	}
	raw := request(t, f.handler, http.MethodGet, "/api/artifacts/"+clustering+"/recipe", nil, nil).Body.Bytes()
	dir := t.TempDir()
	recipe := filepath.Join(dir, "clusters.recipe.json")
	os.WriteFile(recipe, raw, 0o644)
	batch := filepath.Join(dir, "bundles")
	os.MkdirAll(batch, 0o755)
	os.Symlink(f.folder, filepath.Join(batch, "first"))
	os.Symlink(other.folder, filepath.Join(batch, "second"))
	os.MkdirAll(filepath.Join(batch, "notes"), 0o755)

	out := filepath.Join(dir, "all")
	var said bytes.Buffer
	if code := runRun([]string{"--recipe", recipe, "--bundle-dir", batch, "--out", out}, &said, &said); code != exitRunMade {
		t.Fatalf("the batch: exit %d: %s", code, said.String())
	}
	var summary struct {
		Made    int
		Bundles []batchEntry
	}
	text, _ := os.ReadFile(filepath.Join(out, "batch.json"))
	json.Unmarshal(text, &summary)
	if summary.Made != 2 || len(summary.Bundles) != 2 || summary.Bundles[0].Bundle != "first" || summary.Bundles[0].Result != clustering || summary.Bundles[1].Result == clustering || summary.Bundles[1].Fingerprint == "" {
		t.Fatalf("batch.json: %s", text)
	}
	// The second alone, as an array job's task 2 makes it.
	one := filepath.Join(dir, "array")
	if code := runRun([]string{"--recipe", recipe, "--bundle-dir", batch, "--index", "2", "--out", one}, io.Discard, io.Discard); code != exitRunMade {
		t.Fatalf("--index 2: exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(one, "second", "run.json")); err != nil {
		t.Fatalf("--index 2 made no second/run.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(one, "first")); err == nil {
		t.Error("--index 2 made the first bundle too")
	}
	// A list of bundles, its comments passed over; an index past its end refused.
	list := filepath.Join(dir, "bundles.txt")
	os.WriteFile(list, []byte("# the batch\n"+f.folder+"\n\n"), 0o644)
	if code := runRun([]string{"--recipe", recipe, "--bundles", list, "--out", filepath.Join(dir, "listed")}, io.Discard, io.Discard); code != exitRunMade {
		t.Fatalf("--bundles: exit %d", code)
	}
	var stderr bytes.Buffer
	if code := runRun([]string{"--recipe", recipe, "--bundles", list, "--index", "3", "--out", filepath.Join(dir, "past")}, io.Discard, &stderr); code != exitRunNotRun || !strings.Contains(stderr.String(), "the batch has 1 bundle (1 to 1)") {
		t.Errorf("--index past the end: exit %d, %s", code, stderr.String())
	}
}

func jsonQuote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}
