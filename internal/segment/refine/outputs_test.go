package refine

import (
	"context"
	"encoding/json"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/compare"
	"situcraft/internal/segment"
	"situcraft/internal/segment/imports"
	"situcraft/internal/simulate"
)

// A run's outputs are a segmentation like any other: they pass the check, load, and compare with
// the instrument's segmentation; their assignments, probabilities, counts and diagnostics agree
// with the run.
func TestOutputsAreASegmentation(t *testing.T) {
	for _, layers := range []int{1, 2} {
		folder := filepath.Join(t.TempDir(), "small")
		p := simulate.Presets["small"]
		p.WidthUm, p.HeightUm = 80, 60
		if _, err := simulate.Run(context.Background(), p, folder, "test", nil); err != nil {
			t.Fatal(err)
		}
		b := openBundle(t, folder)
		in, idx, err := BundleInputs(b)
		if err != nil {
			t.Fatal(err)
		}
		opt := DefaultOptions()
		opt.Stages, opt.Samples = []Stage{{1, layers, 60}}, 40
		res, err := Run(context.Background(), in, opt, nil)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		d, diag, err := WriteBundleOutputs(b, in, idx, res, opt, dir)
		if err != nil {
			t.Fatal(err)
		}
		m := artifacts.Manifest{}
		for _, f := range segment.Check(dir, &m) {
			if f.Severity == "fatal" {
				t.Errorf("%d layers: %s: %s", layers, f.Code, f.Message)
			}
		}
		var summary segment.Summary
		json.Unmarshal(m.Summary, &summary)
		if summary.Cells != len(res.Kept) || summary.AssignedFraction < 0.8 || len(summary.Diagnostics) == 0 {
			t.Errorf("%d layers: summary %s", layers, m.Summary)
		}
		_, hasLayers := os.Stat(filepath.Join(dir, segment.FileLayers))
		if (hasLayers == nil) != (layers > 1) {
			t.Errorf("%d layers: layers.bin present: %v", layers, hasLayers == nil)
		}
		// Assignments: the modelled transcripts' cells and probabilities are the run's; every
		// other row has no probability.
		assignments, _ := segment.ReadAssignments(dir)
		prob, _ := segment.ReadProbabilities(dir)
		if len(assignments) != idx.Total || d.Transcripts != idx.Total {
			t.Fatalf("%d assignments of %d rows", len(assignments), idx.Total)
		}
		isModelled := map[int]int{}
		for i, r := range idx.Rows {
			isModelled[r] = i
		}
		for r := range assignments {
			if i, ok := isModelled[r]; ok {
				if assignments[r] != res.Assigned[i] || prob[r] != res.Probability[i] {
					t.Fatalf("row %d: cell %d (%g), the run's %d (%g)", r, assignments[r], prob[r], res.Assigned[i], res.Probability[i])
				}
			} else if !math.IsNaN(float64(prob[r])) {
				t.Fatalf("row %d, not modelled, has a probability %g", r, prob[r])
			}
		}
		if diag.Summary.Samples != 40 || diag.Summary.Sweeps != 60 || diag.Summary.SplitHalf < 0.8 || diag.Summary.MeanProbability < 0.5 {
			t.Errorf("%d layers: diagnostics %+v", layers, diag.Summary)
		}
		// It loads and compares with the instrument's segmentation.
		seg, err := compare.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		instDir := t.TempDir()
		if _, err := imports.Instrument(context.Background(), b, instDir, 2); err != nil {
			t.Fatal(err)
		}
		inst, err := compare.Load(instDir)
		if err != nil {
			t.Fatal(err)
		}
		transcripts, err := compare.BundleTranscripts(b)
		if err != nil {
			t.Fatal(err)
		}
		r, err := compare.Compare(context.Background(), compare.Inputs{A: inst, B: seg, Transcripts: transcripts, Params: compare.DefaultParams()})
		if err != nil {
			t.Fatal(err)
		}
		matched := r.Classes[compare.OneToOne] + r.Classes[compare.Expanded] + r.Classes[compare.Contracted]
		if matched < len(res.Kept)*3/4 {
			t.Errorf("%d layers: only %d of %d cells correspond to the instrument's: %v", layers, matched, len(res.Kept), r.Classes)
		}
	}
}

// The cells' limitations are flagged as their thresholds say.
func TestLimitationsAreFlagged(t *testing.T) {
	res := &Result{Kept: []int{0, 2}, Labels: []uint32{1, 1, 2, 0}, LabelShare: []float32{0.9, 0.8, 0.5, 1}, Probability: []float32{1}, Trace: []SweepTrace{{}}}
	cells := []segment.Cell{{ID: "c1", Transcripts: 25}, {ID: "c2", Transcripts: 3}}
	in := &Inputs{NucleusUm2: []float64{30, 20, 0}}
	idx := &BundleIndex{CellIDs: []string{"a-1", "b-1", "c-1"}}
	d := diagnostics(res, cells, in, idx, 2)
	l := d.Limitations
	if len(l.LowTranscripts) != 1 || l.LowTranscripts[0] != "c2" || len(l.NoNucleus) != 1 || l.NoNucleus[0] != "c2" ||
		len(l.Uncertain) != 1 || l.Uncertain[0] != "c2" || len(l.LeftOut) != 1 || l.LeftOut[0] != "b-1" {
		t.Errorf("limitations %+v", l)
	}
}

// Geweke's diagnostic: noise about a level is within ±2, a trend far beyond, a constant 0.
func TestGewekeDiagnostic(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	noise, trend, flat := make([]float64, 500), make([]float64, 500), make([]float64, 500)
	for i := range noise {
		noise[i] = r.NormFloat64()
		trend[i] = float64(i)*0.01 + r.NormFloat64()
	}
	if z := gewekeZ(noise); math.Abs(z) > 2 {
		t.Errorf("noise: z %g", z)
	}
	if z := gewekeZ(trend); z < 5 {
		t.Errorf("a trend: z %g", z)
	}
	if z := gewekeZ(flat); z != 0 {
		t.Errorf("a constant: z %g", z)
	}
	if z := gewekeZ(noise[:20]); z != 0 {
		t.Errorf("too short: z %g", z)
	}
}
