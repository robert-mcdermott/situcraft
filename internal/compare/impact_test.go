package compare

import (
	"math"
	"slices"
	"testing"

	"situcraft/internal/segment"
)

// cellCounts makes counts of cells over genes from each cell's {gene: count}.
func cellCounts(genes []string, cells []map[string]uint32) *segment.Counts {
	c := &segment.Counts{Genes: genes, Offsets: []uint32{0}}
	for _, m := range cells {
		for g, name := range genes {
			if n := m[name]; n > 0 {
				c.Gene = append(c.Gene, uint32(g))
				c.Count = append(c.Count, n)
			}
		}
		c.Offsets = append(c.Offsets, uint32(len(c.Gene)))
	}
	return c
}

// Two label sets of ten cells each, through ten differences, by hand. A's labels:
//
//	0 T, 1 Tumor, 2 T, 3 Mac, 4 T (not covered), 5 Tumor, 6 T, 7 T, 8 Mac, 9 none
//
// B's (its names in another order): 0 T, 1 Tumor, 2 Fib, 3 none, 4 Tumor, 5 Tumor, 6 T, 7 T,
// 8 Fib, 9 T (not covered). The differences: 0–1, 1–1 (expanded), 2–2, 3–3 (contracted), 4–4
// (A's not covered), A5 split into B5 and B6, A6 and A7 merged into B7, A8 only in A, B8 only in
// B, 9–9 assignment-only (B's not covered).
func TestLabelsThroughAComparisonByHand(t *testing.T) {
	a := &SideLabels{Names: []string{"T", "Tumor", "Mac"}, Label: []int32{0, 1, 0, 2, 0, 1, 0, 0, 2, -1},
		Covered: []bool{true, true, true, true, false, true, true, true, true, true}}
	b := &SideLabels{Names: []string{"Tumor", "T", "Fib"}, Label: []int32{1, 0, 2, -1, 0, 0, 1, 1, 2, 1},
		Covered: []bool{true, true, true, true, true, true, true, true, true, false}}
	groups := []Group{
		{Class: OneToOne, A: []int32{0}, B: []int32{0}},
		{Class: Expanded, A: []int32{1}, B: []int32{1}},
		{Class: OneToOne, A: []int32{2}, B: []int32{2}, Changed: 30},
		{Class: Contracted, A: []int32{3}, B: []int32{3}, Changed: 5},
		{Class: OneToOne, A: []int32{4}, B: []int32{4}},
		{Class: Split, A: []int32{5}, B: []int32{5, 6}, Changed: 50},
		{Class: Merged, A: []int32{6, 7}, B: []int32{7}},
		{Class: OnlyA, A: []int32{8}},
		{Class: OnlyB, B: []int32{8}},
		{Class: AssignmentOnly, A: []int32{9}, B: []int32{9}},
	}
	genes := []string{"EPCAM", "CD3E", "CD68"}
	ca := cellCounts(genes, []map[string]uint32{
		{"CD3E": 2}, {"EPCAM": 3}, {"EPCAM": 1, "CD3E": 1}, {"CD68": 2}, {"EPCAM": 1, "CD3E": 1},
		{"EPCAM": 2, "CD68": 1}, {"CD3E": 1, "CD68": 1}, {}, {"CD68": 1, "EPCAM": 1}, {"EPCAM": 1, "CD3E": 1}})
	cb := cellCounts(genes, []map[string]uint32{{"EPCAM": 1, "CD3E": 1}, {}, {}, {}, {}, {}, {}, {}, {}, {}})
	pairs := [][2]string{{"EPCAM", "CD3E"}, {"EPCAM", "CD68"}, {"CD3E", "XYZ"}}
	im, err := CompareLabels(groups, a, b, ca, cb, pairs)
	if err != nil {
		t.Fatal(err)
	}
	if im.CellsA != (SideCells{10, 9, 8}) || im.CellsB != (SideCells{10, 9, 8}) {
		t.Errorf("cells %+v %+v", im.CellsA, im.CellsB)
	}
	// T: A0, A2, A6, A7 against B0, B6, B7; Tumor: A1, A5 against B1, B4, B5; Mac: A3, A8; Fib: B2,
	// B8; no label: A9, B3.
	want := []LabelCount{{"T", 4, 3}, {"Tumor", 2, 3}, {"Mac", 2, 0}, {"Fib", 0, 2}, {"", 1, 1}}
	if !slices.Equal(im.Composition, want) {
		t.Errorf("composition %+v", im.Composition)
	}
	// Matched: 0, 1, 2 and 3 (4 and 9 have a cell not covered); alike 0 and 1 of the three labeled
	// in both; 3 labeled under A only.
	m := im.Matched
	if m.Cells != 4 || m.Labeled != 3 || m.Same != 2 || m.Agreement != Float(0.666667) || m.LabeledOne != 1 ||
		m.ByClass[OneToOne] != [2]int{2, 1} || m.ByClass[Expanded] != [2]int{1, 1} || len(m.ByClass) != 2 {
		t.Errorf("matched %+v", m)
	}
	if !slices.Equal(m.Transitions, []Transition{{"Mac", "", 1}, {"T", "Fib", 1}, {"T", "T", 1}, {"Tumor", "Tumor", 1}}) {
		t.Errorf("transitions %+v", m.Transitions)
	}
	// The Tumor cell split into a Tumor cell and a T cell; two T cells merged into a T cell.
	if im.Split != (Fates{Groups: 1, Partly: 1, Mixed: 1}) || im.Merged != (Fates{Groups: 1, Kept: 1}) || im.Rearranged != (Fates{}) {
		t.Errorf("fates %+v %+v %+v", im.Split, im.Merged, im.Rearranged)
	}
	if !slices.Equal(im.OnlyA, []LabelCount{{"Mac", 1, 0}}) || !slices.Equal(im.OnlyB, []LabelCount{{"Fib", 1, 0}}) {
		t.Errorf("unmatched %+v %+v", im.OnlyA, im.OnlyB)
	}
	// The differences whose labels change, the most transcripts reassigned first.
	if !slices.Equal(im.Changed, []int{5, 2, 3}) {
		t.Errorf("changed %v", im.Changed)
	}
	text := func(side []int32, cell int) string {
		if side[cell] < 0 {
			return ""
		}
		return im.Others[side[cell]]
	}
	for _, c := range []struct {
		side []int32
		cell int
		want string
	}{
		{im.SensitiveA, 0, ""}, {im.SensitiveA, 2, "Fib"}, {im.SensitiveA, 3, "no label"}, {im.SensitiveA, 5, "split into 2 cells: T, Tumor"},
		{im.SensitiveA, 6, ""}, {im.SensitiveA, 8, ""}, {im.SensitiveB, 2, "T"}, {im.SensitiveB, 3, "Mac"}, {im.SensitiveB, 5, ""},
		{im.SensitiveB, 6, "one of 2 cells split from a cell labeled Tumor"}, {im.SensitiveB, 7, ""},
	} {
		if got := text(c.side, c.cell); got != c.want {
			t.Errorf("cell %d: %q, by hand %q", c.cell, got, c.want)
		}
	}
	if im.Sensitive != [2]int{3, 2} {
		t.Errorf("sensitive %v", im.Sensitive)
	}
	// Marker mixing (the pairs EPCAM–CD3E and EPCAM–CD68; CD3E–XYZ not in the panel). A's T cells
	// (0, 2, 6, 7): 2 expresses a pair; EPCAM–CD3E in 1 of the 3 expressing either, EPCAM–CD68 in 0
	// of 2: MECR 1/6. All of A's labeled cells: 2, 5 and 8 mixed; 1 of 6 and 2 of 6: MECR 1/4.
	mx := im.Mixing
	if mx == nil || mx.Pairs != 2 {
		t.Fatalf("mixing %+v", mx)
	}
	close := func(got Float, want float64) bool { return math.Abs(float64(got)-want) < 1e-6 }
	if mx.A[0].Name != "T" || mx.A[0].Cells != 4 || mx.A[0].Mixed != 1 || !close(mx.A[0].Share, 0.25) || !close(mx.A[0].MECR, 1.0/6) ||
		mx.A[1].Name != "Tumor" || !close(mx.A[1].MECR, 0.25) || mx.A[2].Name != "Mac" || !close(mx.A[2].Share, 0.5) {
		t.Errorf("A's mixing %+v", mx.A)
	}
	if mx.AllA.Cells != 8 || mx.AllA.Mixed != 3 || !close(mx.AllA.MECR, 0.25) {
		t.Errorf("A's mixing over all %+v", mx.AllA)
	}
	if mx.AllB.Cells != 8 || mx.AllB.Mixed != 1 || !close(mx.AllB.MECR, 0.5) || mx.B[1].Name != "T" || !close(mx.B[1].Share, 1.0/3) || !math.IsNaN(float64(mx.B[0].MECR)) {
		t.Errorf("B's mixing %+v %+v", mx.AllB, mx.B)
	}
	// Labels naming a cell the comparison does not have are refused.
	if _, err := CompareLabels([]Group{{Class: OneToOne, A: []int32{12}, B: []int32{0}}}, a, b, nil, nil, nil); err == nil {
		t.Error("a difference naming cell 12 of 10 passed")
	}
}
