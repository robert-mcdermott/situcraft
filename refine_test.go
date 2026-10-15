package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/simulate"
)

// The transcript-informed segmentation of a (simulated) bundle: refused before the instrument's
// segmentation is registered and with bad options; then made by a job, checked and recorded as an
// artifact with its probabilities and diagnostics, named by its options; asked again, the same
// artifact.
func TestRefinementIsASegmentationArtifact(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
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
	defer a.jobs.Close()
	h, _ := a.handler()
	handler := protect(h, "127.0.0.1", defaultPort)
	post := func(target, body string) *httptest.ResponseRecorder {
		return request(t, handler, http.MethodPost, target, strings.NewReader(body), nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if rec := post("/api/local/0/refinement", `{"sweeps":40,"samples":20}`); rec.Code != http.StatusConflict {
		t.Errorf("before the instrument's segmentation: %d %s", rec.Code, rec.Body)
	}
	var registering struct{ Job *jobs.Job }
	json.NewDecoder(post("/api/local/0/segmentation", "").Body).Decode(&registering)
	if registering.Job == nil {
		t.Fatal("no registration job")
	}
	if j, _ := a.jobs.Wait(ctx, registering.Job.ID); j.State != jobs.Complete {
		t.Fatalf("registration ended %s: %s", j.State, j.Error)
	}
	for _, bad := range []string{`{"sweeps":5}`, `{"layers":0}`, `{"samples":0}`, `{"sweeps":40,"samples":41}`, `{"burnin":3}`, `[1]`} {
		if rec := post("/api/local/0/refinement", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("options %s: %d %s", bad, rec.Code, rec.Body)
		}
	}
	type answer struct {
		ID       string
		Artifact *artifacts.Manifest
		Job      *jobs.Job
	}
	var started answer
	json.NewDecoder(post("/api/local/0/refinement", `{"sweeps":40,"samples":20,"seed":7}`).Body).Decode(&started)
	if started.Job == nil || !strings.HasPrefix(started.ID, "ref-") {
		t.Fatalf("no refinement job: %+v", started)
	}
	if j, _ := a.jobs.Wait(ctx, started.Job.ID); j.State != jobs.Complete {
		t.Fatalf("refinement ended %s: %s", j.State, j.Error)
	}
	var again answer
	json.NewDecoder(post("/api/local/0/refinement", `{"seed":7,"sweeps":40,"samples":20}`).Body).Decode(&again)
	m := again.Artifact
	if again.Job != nil || m == nil || m.ID != started.ID || m.State != artifacts.Complete {
		t.Fatalf("asking again: %+v", again)
	}
	if m.Name != "Transcript-informed segmentation (40 sweeps, seed 7)" || m.Lineage.Method.Name != "SituCraft transcript-informed segmentation" || len(m.Lineage.Parents) != 2 {
		t.Errorf("name %q, lineage %+v", m.Name, m.Lineage)
	}
	roles := map[string]bool{}
	for _, f := range m.Files {
		roles[f.Path] = true
	}
	for _, name := range []string{segment.FileMask, segment.FileAssignments, segment.FileProbabilities, segment.FileDiagnostics} {
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
		Seed       uint64
		Samples    int
		StartStage int `json:"start_stage"`
		Stages     []struct{ Sweeps int }
		Model      struct {
			VolumeMean float64 `json:"volume_mean_log_um3"`
		}
	}
	json.Unmarshal(m.Lineage.Parameters, &params)
	if params.Seed != 7 || params.Samples != 20 || len(params.Stages) != 1 || params.Stages[0].Sweeps != 40 || params.Model.VolumeMean == 0 || params.StartStage != 1 {
		t.Errorf("parameters %s", m.Lineage.Parameters)
	}
	// Another seed is another artifact.
	var other answer
	json.NewDecoder(post("/api/local/0/refinement", `{"sweeps":40,"samples":20,"seed":8}`).Body).Decode(&other)
	if other.ID == started.ID {
		t.Error("another seed, the same artifact")
	}
	if other.Job != nil {
		a.jobs.Wait(ctx, other.Job.ID)
	}
}
