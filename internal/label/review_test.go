package label

import (
	"context"
	"math"
	"slices"
	"testing"

	"situcraft/internal/cluster"
)

// Agreement by hand: a 3 × 3 grid of cells 1 µm apart, the middle one labeled 1 and the others 0,
// one corner without a label.
func TestAgreementByHand(t *testing.T) {
	var xy []float64
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			xy = append(xy, float64(x), float64(y))
		}
	}
	label := []int32{-1, 0, 0, 0, 1, 0, 0, 0, 0}
	share, dominant, err := Agreement(context.Background(), xy, label, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	// The middle's four nearest labeled cells (at 1 µm) are all 0.
	if share[4] != 0 || dominant[4] != 0 {
		t.Errorf("the middle: %v of its neighbors share its label, most carry %d", share[4], dominant[4])
	}
	// Cell 1 (top middle): its nearest labeled are 4 (1 µm, label 1), 2 (1 µm), then 3 and 5 (√2):
	// by distance then index, 2, 4, 3, 5: three of four share its label.
	if share[1] != 0.75 || dominant[1] != 0 {
		t.Errorf("cell 1: %v share its label, most carry %d", share[1], dominant[1])
	}
	if !math.IsNaN(float64(share[0])) || dominant[0] != -1 {
		t.Error("a cell without a label has an agreement")
	}
}

// The queue: both reasons first, then the implausible by agreement, then the least confident; the
// reviewed left out and counted.
func TestQueueOrder(t *testing.T) {
	label := []int32{0, 0, 1, 1, 0, -1, 1}
	share := []float32{0.1, 0.9, 0.0, 0.3, 0.2, float32(math.NaN()), 0.8}
	dominant := []int32{1, 0, 0, 0, 1, -1, 1}
	nan := float32(math.NaN())
	conf := []float32{0.05, 0.02, 0.5, 0.01, 0.9, nan, nan}
	items, counts := Queue(label, share, dominant, conf, QueueOptions{Implausible: 0.2, LowConfidence: 0.1, Reviewed: map[int]bool{3: true}})
	var cells []int
	for _, it := range items {
		cells = append(cells, it.Cell)
	}
	// 0: both (0.1, 0.05); 2: implausible 0.0; 4: implausible 0.2; 1: low confidence 0.02. Cell 3
	// is reviewed; 6 agrees with its neighbors and has no confidence.
	if !slices.Equal(cells, []int{0, 2, 4, 1}) {
		t.Errorf("queue %v", cells)
	}
	if counts != (QueueCounts{Labeled: 6, Flagged: 4, Implausible: 3, LowConfidence: 2, Both: 1, Reviewed: 1}) {
		t.Errorf("counts %+v", counts)
	}
	if items[0].Reasons[0] != ReasonImplausible || items[0].Reasons[1] != ReasonLowConfidence || *items[0].Confidence != 0.05 {
		t.Errorf("the first: %+v", items[0])
	}
	if limited, _ := Queue(label, share, dominant, conf, QueueOptions{Implausible: 0.2, LowConfidence: math.NaN(), Limit: 1}); len(limited) != 1 || limited[0].Cell != 2 {
		t.Errorf("without a confidence threshold, limited to one: %+v", limited)
	}
	// Segmentation-sensitive cells: 1 (whose counterpart is a Macrophage) and 6 (split). Two reasons
	// first, the implausible before the rest; then one: the implausible by agreement, the
	// sensitive, the least confident.
	other := []int32{-1, 0, -1, -1, -1, -1, 1}
	items, counts = Queue(label, share, dominant, conf, QueueOptions{Implausible: 0.2, LowConfidence: 0.1, Other: other, Others: []string{"Macrophage", "split into 2 cells: T, Tumor"}})
	cells = cells[:0]
	for _, it := range items {
		cells = append(cells, it.Cell)
	}
	// 0: implausible and low confidence; 1: sensitive and low confidence; 2, 4: implausible; 6:
	// sensitive; 3 has no reason left but low confidence 0.01 (and is not reviewed here).
	if !slices.Equal(cells, []int{0, 1, 2, 4, 6, 3}) || counts.Sensitive != 2 || counts.Both != 2 || counts.Flagged != 6 || items[1].Other != "Macrophage" || items[1].Reasons[0] != ReasonSensitive {
		t.Errorf("with sensitive cells: %v, %+v, %+v", cells, counts, items[1])
	}
}

// A cell's evidence follows its recorded scores: each list's score from its genes and controls, the
// label taken the best of its candidates, the next the runner-up.
func TestCellEvidence(t *testing.T) {
	c, truth := tissue(400, 7)
	var cells, clusters []uint32
	for i := range truth {
		cells = append(cells, uint32(i))
		clusters = append(clusters, uint32(truth[i]/2))
	}
	e, err := cluster.LoadExpression(c, cells, clusters, 100)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Run(e, set(planted), 2, 100, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Write(dir, cluster.Side{ID: "s"}, cluster.Side{ID: "c"}, r); err != nil {
		t.Fatal(err)
	}
	d, _, lab, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, 1, 2, 3, 57, 399} {
		ev := CellEvidence(e, d, lab, i, 1)
		if ev.Cell != i || len(ev.Levels) == 0 || ev.Also == nil || ev.Also.Name != "One a" {
			t.Fatalf("cell %d: %+v", i, ev)
		}
		for _, lv := range ev.Levels {
			for _, le := range lv.Evidence {
				if math.Abs(le.Positive.Score-float64(lab.Positive[le.Set][i])) > 1e-6 {
					t.Errorf("cell %d, %s: positive score %v from its genes, %v recorded", i, le.Name, le.Positive.Score, lab.Positive[le.Set][i])
				}
				want := le.Positive.Score
				if le.Negative != nil && le.Negative.Score > 0 {
					want -= le.Negative.Score
				}
				if math.Abs(want-float64(le.Score)) > 1e-6 {
					t.Errorf("cell %d, %s: score %v from its lists, %v recorded", i, le.Name, want, le.Score)
				}
			}
			if lv.Label >= 0 && int(lv.Label) != lv.Best {
				t.Errorf("cell %d at %s took %d, not its best %d", i, lv.Level, lv.Label, lv.Best)
			}
			if lv.Next >= 0 && lab.Score[lv.Next][i] > lab.Score[lv.Best][i] {
				t.Errorf("cell %d at %s: the next outscores the best", i, lv.Level)
			}
		}
	}
}
