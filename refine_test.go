package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/segment/refine"
	"situcraft/internal/simulate"
)

// refineFixture is an app with a small simulated bundle, its instrument's segmentation registered.
type refineFixture struct {
	t       *testing.T
	a       *app
	handler http.Handler
	folder  string
	ctx     context.Context
}

func newRefineFixture(t *testing.T, register bool) *refineFixture {
	t.Helper()
	dir := t.TempDir()
	folder := filepath.Join(dir, "simulated")
	p := simulate.Presets["small"]
	p.WidthUm, p.HeightUm = 80, 60
	if _, err := simulate.Run(context.Background(), p, folder, "test", nil); err != nil {
		t.Fatal(err)
	}
	a, err := newApp(config{dataDir: dir, cacheDir: filepath.Join(dir, "cache"), cacheLimit: 10e9, files: []string{folder}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.jobs.Close() })
	h, _ := a.handler()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	f := &refineFixture{t: t, a: a, handler: protect(h, "127.0.0.1", defaultPort), folder: folder, ctx: ctx}
	if register {
		var registering struct{ Job *jobs.Job }
		json.NewDecoder(f.post("/api/local/0/segmentation", "").Body).Decode(&registering)
		if registering.Job == nil {
			t.Fatal("no registration job")
		}
		f.wait(registering.Job.ID)
	}
	return f
}

func (f *refineFixture) post(target, body string) *httptest.ResponseRecorder {
	return request(f.t, f.handler, http.MethodPost, target, strings.NewReader(body), nil)
}

func (f *refineFixture) wait(id string) jobs.Job {
	f.t.Helper()
	j, _ := f.a.jobs.Wait(f.ctx, id)
	if j.State != jobs.Complete {
		f.t.Fatalf("job %s ended %s: %s", j.Kind, j.State, j.Error)
	}
	return j
}

type refineAnswer struct {
	ID       string
	Artifact *artifacts.Manifest
	Job      *jobs.Job
}

// refine starts a refinement and waits for its artifact.
func (f *refineFixture) refine(body string) *artifacts.Manifest {
	f.t.Helper()
	var started refineAnswer
	rec := f.post("/api/local/0/refinement", body)
	json.NewDecoder(rec.Body).Decode(&started)
	if started.Job == nil && started.Artifact == nil {
		f.t.Fatalf("no refinement for %s: %d", body, rec.Code)
	}
	if started.Job != nil {
		f.wait(started.Job.ID)
	}
	m, err := f.a.artifacts.Get(started.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return &m
}

// The transcript-informed segmentation of a (simulated) bundle: refused before the instrument's
// segmentation is registered and with bad options; then made by a job, checked and recorded as an
// artifact with its probabilities and diagnostics, named by its options; asked again, the same
// artifact.
func TestRefinementIsASegmentationArtifact(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	f := newRefineFixture(t, false)
	if rec := f.post("/api/local/0/refinement", `{"sweeps":40,"samples":20}`); rec.Code != http.StatusConflict {
		t.Errorf("before the instrument's segmentation: %d %s", rec.Code, rec.Body)
	}
	var registering struct{ Job *jobs.Job }
	json.NewDecoder(f.post("/api/local/0/segmentation", "").Body).Decode(&registering)
	f.wait(registering.Job.ID)
	for _, bad := range []string{`{"sweeps":5}`, `{"layers":0}`, `{"samples":0}`, `{"sweeps":40,"samples":41}`, `{"burnin":3}`, `[1]`,
		`{"preset":"roi-pilot"}`, `{"preset":"nonesuch"}`, `{"min_qv":50}`, `{"area":{"mode":"regions"}}`, `{"options":{"sampler":{"perimeter_factor":0.5}}}`} {
		if rec := f.post("/api/local/0/refinement", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("options %s: %d %s", bad, rec.Code, rec.Body)
		}
	}
	m := f.refine(`{"sweeps":40,"samples":20,"seed":7}`)
	var again refineAnswer
	json.NewDecoder(f.post("/api/local/0/refinement", `{"seed":7,"sweeps":40,"samples":20,"threads":2}`).Body).Decode(&again)
	if again.Job != nil || again.Artifact == nil || again.Artifact.ID != m.ID || m.State != artifacts.Complete {
		t.Fatalf("asking again: %+v", again)
	}
	if m.Name != "Transcript-informed segmentation (40 sweeps, seed 7)" || m.Lineage.Method.Name != "SituCraft transcript-informed segmentation" || len(m.Lineage.Parents) != 2 {
		t.Errorf("name %q, lineage %+v", m.Name, m.Lineage)
	}
	roles := map[string]bool{}
	for _, file := range m.Files {
		roles[file.Path] = true
	}
	for _, name := range []string{segment.FileMask, segment.FileAssignments, segment.FileProbabilities, segment.FileCertainty, segment.FileDiagnostics} {
		if !roles[name] {
			t.Errorf("no %s among %v", name, m.Files)
		}
	}
	if roles[segment.FileLayers] {
		t.Error("layers.bin for a run in one layer")
	}
	var summary struct {
		Cells            int
		AssignedFraction float64
		Diagnostics      struct {
			Sweeps    int
			Samples   int
			SplitHalf float64 `json:"splitHalfAgreement"`
		}
	}
	json.Unmarshal(m.Summary, &summary)
	if summary.Cells == 0 || summary.AssignedFraction < 0.8 || summary.Diagnostics.Sweeps != 40 || summary.Diagnostics.Samples != 20 {
		t.Errorf("summary %s", m.Summary)
	}
	var params struct {
		Seed        uint64
		Samples     int
		StartStages []int `json:"start_stages"`
		Stages      []struct{ Sweeps int }
		Preset      string
		MinQV       float64 `json:"min_qv"`
		Area        struct{ Mode string }
		Model       struct {
			VolumeMean float64 `json:"volume_mean_log_um3"`
		}
	}
	json.Unmarshal(m.Lineage.Parameters, &params)
	if params.Seed != 7 || params.Samples != 20 || len(params.Stages) != 1 || params.Stages[0].Sweeps != 40 || params.Model.VolumeMean == 0 ||
		len(params.StartStages) != 1 || params.StartStages[0] != 1 || params.Preset != refine.PresetExpert || params.MinQV != 20 || params.Area.Mode != "whole" {
		t.Errorf("parameters %s", m.Lineage.Parameters)
	}
	// Another seed is another artifact.
	var other refineAnswer
	json.NewDecoder(f.post("/api/local/0/refinement", `{"sweeps":40,"samples":20,"seed":8}`).Body).Decode(&other)
	if other.ID == m.ID {
		t.Error("another seed, the same artifact")
	}
	if other.Job != nil {
		f.wait(other.Job.ID)
	}
}

// The studio: the presets and the settings' help; a spec's preflight (refused before the
// instrument's segmentation, then what it models, covers and costs, with its recipe); the ROI
// pilot refused without a region, and with one refined twice, its seeds' agreement recorded.
func TestRefinementStudio(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	f := newRefineFixture(t, false)
	var presets struct {
		Version  string
		Presets  []refine.Preset
		Settings []refine.Setting
		Defaults refine.Spec
	}
	json.NewDecoder(request(t, f.handler, http.MethodGet, "/api/refinement/presets", nil, nil).Body).Decode(&presets)
	if presets.Version != refine.PresetsVersion || len(presets.Presets) != 6 || presets.Presets[0].ID != refine.PresetBalanced || len(presets.Settings) < 10 || presets.Defaults.MinQV != 20 {
		t.Fatalf("presets %+v", presets)
	}
	type preflightAnswer struct {
		ID        string
		Name      string
		Threads   int
		Artifact  *artifacts.Manifest
		Preflight refine.Preflight
		Recipe    json.RawMessage
	}
	preflight := func(body string) preflightAnswer {
		t.Helper()
		rec := f.post("/api/local/0/refinement/preflight", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("preflight %s: %d %s", body, rec.Code, rec.Body)
		}
		var p preflightAnswer
		json.NewDecoder(rec.Body).Decode(&p)
		return p
	}
	if p := preflight(""); len(p.Preflight.Problems) != 1 || p.Preflight.Problems[0].Class != refine.ClassPrior {
		t.Errorf("before the instrument's segmentation: %+v", p.Preflight.Problems)
	}
	var registering struct{ Job *jobs.Job }
	json.NewDecoder(f.post("/api/local/0/segmentation", "").Body).Decode(&registering)
	f.wait(registering.Job.ID)

	p := preflight(`{"preset":"balanced","threads":2}`)
	pre := p.Preflight
	if len(pre.Problems) != 0 || pre.Transcripts == 0 || pre.Cells == 0 || pre.Nuclei == 0 || pre.Genes == 0 || pre.Rows < pre.Transcripts ||
		len(pre.Regions) != 1 || pre.Estimate.PeakBytes <= 0 || pre.Estimate.DiskBytes <= 0 || pre.Estimate.Seconds <= 0 || p.Threads != 2 || pre.Estimate.Cores != 2 ||
		!strings.HasPrefix(p.ID, "ref-") || p.Artifact != nil || p.Name != "Transcript-informed segmentation" {
		t.Errorf("preflight %+v", p)
	}
	r, err := readRecipe(p.Recipe)
	if err != nil || r.Refinement.Preset != refine.PresetBalanced || r.Dataset.Name != "simulated" || !r.Dataset.Simulated {
		t.Errorf("recipe %s (%v)", p.Recipe, err)
	}
	if q := preflight(`{"min_qv":10}`); len(q.Preflight.Risks) == 0 || q.Preflight.Transcripts <= pre.Transcripts {
		t.Errorf("quality 10: %d transcripts (Q20 %d), risks %+v", q.Preflight.Transcripts, pre.Transcripts, q.Preflight.Risks)
	}
	if q := preflight(`{"area":{"mode":"regions","regions":[{"x0_um":200,"y0_um":200,"x1_um":300,"y1_um":300}]}}`); len(q.Preflight.Problems) != 1 || q.Preflight.Problems[0].Class != refine.ClassRegion {
		t.Errorf("a region beyond the tissue: %+v", q.Preflight.Problems)
	}
	// The ROI pilot: a region, refined twice.
	pilot := `{"preset":"roi-pilot","sweeps":40,"samples":20,"area":{"mode":"regions","regions":[{"x0_um":20,"y0_um":15,"x1_um":60,"y1_um":45}]}}`
	if q := preflight(pilot); len(q.Preflight.Problems) != 0 || len(q.Preflight.Regions) != 1 || q.Preflight.Estimate.Held == 0 {
		t.Errorf("the pilot's preflight %+v", q.Preflight)
	}
	m := f.refine(pilot)
	var diag refine.Diagnostics
	path, _ := f.a.artifacts.Path(m.ID, segment.FileDiagnostics)
	raw, _ := os.ReadFile(path)
	json.Unmarshal(raw, &diag)
	if diag.Summary.SeedAgreement == nil || *diag.Summary.SeedAgreement < 0.5 || diag.Summary.Held == 0 || diag.Summary.Regions != 1 {
		t.Errorf("the pilot's diagnostics %+v", diag.Summary)
	}
	if !strings.Contains(m.Name, "region (20, 15)–(60, 45) µm") {
		t.Errorf("the pilot's name %q", m.Name)
	}
	var params refine.Recorded
	json.Unmarshal(m.Lineage.Parameters, &params)
	if params.SecondSeed != params.Seed+1 || params.Area.Mode != refine.AreaRegions {
		t.Errorf("the pilot's parameters %s", m.Lineage.Parameters)
	}
}

func digestFile(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}

// situcraft refine: a recipe from the app's preflight, run headless, gives the app's files byte for
// byte; the folder imports into the app as a refinement of the same dataset, and not into another.
func TestRefineCommandMatchesTheApp(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	f := newRefineFixture(t, true)
	body := `{"sweeps":40,"samples":20,"seed":11}`
	var p struct{ Recipe json.RawMessage }
	json.NewDecoder(f.post("/api/local/0/refinement/preflight", body).Body).Decode(&p)
	recipeFile := filepath.Join(t.TempDir(), "recipe.json")
	os.WriteFile(recipeFile, p.Recipe, 0o644)
	m := f.refine(body)

	out := filepath.Join(t.TempDir(), "refined")
	var stdout, stderr bytes.Buffer
	if code := runRefine([]string{"--recipe", recipeFile, "--quiet", "--threads", "3", f.folder, out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Written to") {
		t.Errorf("summary %q", stdout.String())
	}
	for _, file := range m.Files {
		app, _ := f.a.artifacts.Path(m.ID, file.Path)
		if got, want := digestFile(t, filepath.Join(out, file.Path)), digestFile(t, app); got != want {
			t.Errorf("%s: the command's differs from the app's", file.Path)
		}
	}
	if _, err := readRecipe(must(os.ReadFile(filepath.Join(out, "recipe.json")))); err != nil {
		t.Errorf("the folder's recipe: %v", err)
	}
	// Refused: an existing folder, an unknown preset, regions with sections, a recipe of another
	// kind.
	for _, args := range [][]string{{f.folder, out}, {"--preset", "nonesuch", f.folder, out + "2"}, {"--sections", "--region", "0,0,10,10", f.folder, out + "3"}} {
		stderr.Reset()
		if code := runRefine(args, &stdout, &stderr); code != 2 {
			t.Errorf("%v: exit %d (%s)", args, code, stderr.String())
		}
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{"format":"situcraft-recipe","version":1,"kind":"export"}`), 0o644)
	if code := runRefine([]string{"--recipe", bad, f.folder, out + "4"}, &stdout, &stderr); code != 2 {
		t.Errorf("a recipe of another kind: exit %d", code)
	}
	// The preflight in the terminal.
	stdout.Reset()
	if code := runRefine([]string{"--preflight", "--preset", "low-memory", f.folder}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Needs about") || !strings.Contains(stdout.String(), "Low memory") {
		t.Errorf("preflight: exit %d, %q", code, stdout.String())
	}

	// The folder imports as a refinement of the dataset.
	importFolder := func(folder string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/local/0/import", strings.NewReader(`{"format":"situcraft","paths":[`+strconv.Quote(folder)+`]}`))
		req.Host, req.RemoteAddr = "127.0.0.1:8840", "127.0.0.1:50000"
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		return rec
	}
	var started struct{ Job *jobs.Job }
	rec := importFolder(out)
	json.NewDecoder(rec.Body).Decode(&started)
	if started.Job == nil {
		t.Fatalf("no import job: %d %s", rec.Code, rec.Body)
	}
	j := f.wait(started.Job.ID)
	var imported *artifacts.Manifest
	list, _ := f.a.artifacts.List()
	for i, a := range list {
		if strings.HasPrefix(a.ID, "imp-") && a.Lineage.Job != nil && a.Lineage.Job.ID == j.ID {
			imported = &list[i]
		}
	}
	if imported == nil || imported.State != artifacts.Complete || imported.Lineage.Method.Name != refine.Method || imported.Name != "Transcript-informed segmentation (40 sweeps, seed 11) (imported)" {
		t.Fatalf("imported %+v", imported)
	}
	if len(imported.Files) != len(m.Files) {
		t.Errorf("imported %d files, the refinement %d", len(imported.Files), len(m.Files))
	}
	for i := range m.Files {
		if i < len(imported.Files) && imported.Files[i].SHA256 != m.Files[i].SHA256 {
			t.Errorf("%s differs from the refinement's", imported.Files[i].Path)
		}
	}
	// Another dataset's segmentation (here, one of another image's size) is refused.
	other := filepath.Join(t.TempDir(), "other")
	os.MkdirAll(other, 0o755)
	d, _ := segment.ReadDescription(out)
	d.Width++
	segment.WriteDescription(other, *d)
	json.NewDecoder(importFolder(other).Body).Decode(&started)
	if j, _ := f.a.jobs.Wait(f.ctx, started.Job.ID); j.State != jobs.Failed || !strings.Contains(j.Error, "morphology image") {
		t.Errorf("another dataset's segmentation: %s, %s", j.State, j.Error)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
