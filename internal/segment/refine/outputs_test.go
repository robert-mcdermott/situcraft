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
		in, idx, err := BundleInputs(b, 20)
		if err != nil {
			t.Fatal(err)
		}
		opt := DefaultOptions()
		opt.Stages, opt.Samples = []Stage{{1, layers, 60}}, 40
		res, err := Run(context.Background(), in, Region{}, opt, nil)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		d, diag, err := WriteBundleOutputs(b, in, idx, []*Result{res}, opt, Recorded{}, dir)
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
		if summary.Cells != in.Cells || summary.AssignedFraction < 0.8 || len(summary.Diagnostics) == 0 {
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
				want := uint32(0)
				if f := res.Assigned[i]; f > 0 {
					want = uint32(res.Kept[f-1] + 1) // the instrument's cell it started from
				}
				if assignments[r] != want || prob[r] != res.Probability[i] {
					t.Fatalf("row %d: cell %d (%g), the run's %d (%g)", r, assignments[r], prob[r], want, res.Probability[i])
				}
			} else if !math.IsNaN(float64(prob[r])) {
				t.Fatalf("row %d, not modelled, has a probability %g", r, prob[r])
			}
		}
		if diag.Summary.Samples != 40 || diag.Summary.Sweeps != 60 || diag.Summary.SplitHalf < 0.8 || diag.Summary.MeanProbability < 0.5 {
			t.Errorf("%d layers: diagnostics %+v", layers, diag.Summary)
		}
		// Each refined cell's boundary certainty, below the threshold exactly for those flagged
		// uncertain.
		certainty, err := segment.ReadCertainty(dir)
		if err != nil || len(certainty) != in.Cells {
			t.Fatalf("%d layers: %d boundary certainties for %d cells (%v)", layers, len(certainty), in.Cells, err)
		}
		uncertain := map[string]bool{}
		for _, id := range diag.Limitations.Uncertain {
			uncertain[id] = true
		}
		cellsOut, _ := segment.ReadCells(dir)
		for c, x := range certainty {
			if flagged := !(x >= uncertainShare); flagged != uncertain[cellsOut[c].ID] {
				t.Fatalf("%d layers: cell %s's boundary certainty %g, flagged uncertain %v", layers, cellsOut[c].ID, x, uncertain[cellsOut[c].ID])
			}
		}
		// The simulated tissue's background is about 2.5% of its transcripts, half of it false
		// decodes, which its negative controls measure.
		if s := diag.Summary; s.NegativeControls == 0 || !(s.FalseDecodeShare > 0.005 && s.FalseDecodeShare < 0.02) || !(s.AmbientShare > 0) || !(s.InBackground > 0) {
			t.Errorf("%d layers: background %+v", layers, s)
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

// The cells' limitations are flagged as their thresholds say, for the refined cells only.
func TestLimitationsAreFlagged(t *testing.T) {
	res := &Result{Kept: []int{0, 2, 1}, Frozen: []bool{false, false, true}, Labels: []uint32{1, 1, 2, 0, 3}, LabelShare: []float32{0.9, 0.8, 0.5, 1, 0.2},
		Probability: []float32{1}, Assigned: []uint32{1}, Trace: []SweepTrace{{}}, LeftOut: []int{3}}
	cells := []segment.Cell{{ID: "c1", Transcripts: 25}, {ID: "c2", Transcripts: 1}, {ID: "c3", Transcripts: 3}, {ID: "c4"}}
	in := &Inputs{NucleusUm2: []float64{30, 0, 0, 10}}
	idx := &BundleIndex{CellIDs: []string{"a-1", "b-1", "c-1", "d-1"}}
	d := diagnostics([]*Result{res}, cells, in, idx, nil)
	l := d.Limitations
	// c2 (input cell 1) is held: never flagged; c3 is refined with 3 transcripts, no nucleus and
	// an uncertain boundary; d-1 was left out.
	if len(l.LowTranscripts) != 1 || l.LowTranscripts[0] != "c3" || len(l.NoNucleus) != 1 || l.NoNucleus[0] != "c3" ||
		len(l.Uncertain) != 1 || l.Uncertain[0] != "c3" || len(l.LeftOut) != 1 || l.LeftOut[0] != "d-1" {
		t.Errorf("limitations %+v", l)
	}
	if s := d.Summary; s.Refined != 2 || s.Held != 1 || s.Regions != 1 || s.LeftOut != 1 {
		t.Errorf("summary %+v", s)
	}
}

// The background's shares are each region's, weighted by its transcripts over several, and the
// negative controls are summed, from results released as a refinement releases them before
// writing.
func TestBackgroundIsSummarized(t *testing.T) {
	region := func(transcripts, inBackground, controls int, falseDecodes, ambient float64) *Result {
		res := &Result{Probability: make([]float32, transcripts), Assigned: make([]uint32, transcripts), Trace: []SweepTrace{{}},
			FalseDecodes: falseDecodes, Ambient: ambient, Controls: controls, Model: &Model{Controls: uint32(controls)}}
		for i := inBackground; i < transcripts; i++ {
			res.Assigned[i] = 1
		}
		res.Release()
		return res
	}
	d := diagnostics([]*Result{region(100, 2, 7, 3, 1), region(300, 0, 5, 3, 9)}, nil, &Inputs{}, &BundleIndex{}, nil)
	s := d.Summary
	if s.FalseDecodeShare != 6.0/400 || s.AmbientShare != 10.0/400 || s.InBackground != 2.0/400 || s.NegativeControls != 12 {
		t.Errorf("background %g, %g; in no cell %g; controls %d", s.FalseDecodeShare, s.AmbientShare, s.InBackground, s.NegativeControls)
	}
	if r := d.Regions[0].Summary; r.FalseDecodeShare != 0.03 || r.AmbientShare != 0.01 || r.InBackground != 0.02 || r.NegativeControls != 7 {
		t.Errorf("the first region's background %+v", r)
	}
}

// Geweke's diagnostic: noise about a level is within ±2, a trend far beyond, a constant 0; a
// trace too short is not judged.
func TestGewekeDiagnostic(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	noise, trend, flat := make([]float64, 500), make([]float64, 500), make([]float64, 500)
	for i := range noise {
		noise[i] = r.NormFloat64()
		trend[i] = float64(i)*0.01 + r.NormFloat64()
	}
	if z, ok := gewekeZ(noise); math.Abs(z) > 2 || !ok {
		t.Errorf("noise: z %g (%v)", z, ok)
	}
	if z, ok := gewekeZ(trend); z < 5 || !ok {
		t.Errorf("a trend: z %g (%v)", z, ok)
	}
	if z, ok := gewekeZ(flat); z != 0 || !ok {
		t.Errorf("a constant: z %g (%v)", z, ok)
	}
	// Too short to judge: under minGeweke samples.
	if z, ok := gewekeZ(noise[:minGeweke-1]); z != 0 || ok {
		t.Errorf("too short: z %g (%v)", z, ok)
	}
	if _, ok := gewekeZ(noise[:minGeweke]); !ok {
		t.Errorf("%d samples not judged", minGeweke)
	}
}
