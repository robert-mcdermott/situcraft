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
	if n := len(spatialRecipe.Steps); n != 3 || !spatialRecipe.Steps[1].Carries || !spatialRecipe.Steps[2].Carries || spatialRecipe.Steps[2].Job != "spatial" {
		t.Fatalf("the enrichment's recipe: %+v", spatialRecipe.Steps)
	}
	// As an action, in words.
	if outcome, ok := f.a.programAction(context.Background(), "recipe", json.RawMessage(`{"id":"`+enrichment+`"}`)); !ok || !outcome.OK || !strings.Contains(outcome.Message, "3 steps from 1 bundle") || !strings.Contains(outcome.Message, "2 steps carry cells the window chose") {
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
	if code, _, _, said := makeIt(spatialPath, other.folder); code != exitRunNotRun || !strings.Contains(said, "2 steps carry cells the window chose") {
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
