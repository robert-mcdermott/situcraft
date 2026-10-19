package cohort

import (
	"math"
	"reflect"
	"testing"

	"situcraft/internal/segment"
)

// Five cells, worked by hand: four chosen, three of them labeled; their counts over three genes.
func TestSummarizeByHand(t *testing.T) {
	cells := []segment.Cell{
		{ID: "a", Nucleus: "n1", Area: 100, NucleusArea: 30, Transcripts: 10, Genes: 3},
		{ID: "b", Nucleus: "n2", Area: 80, NucleusArea: 20, Transcripts: 4, Genes: 2},
		{ID: "c", Area: 60, Transcripts: 0, Genes: 0},
		{ID: "d", Nucleus: "n4", Area: 120, NucleusArea: 40, Transcripts: 6, Genes: 2},
		{ID: "e", Nucleus: "n5", Area: 999, NucleusArea: 99, Transcripts: 99, Genes: 3},
	}
	// Rows: a: g0×5, g1×3, g2×2; b: g0×4; c: none; d: g1×1, g2×5; e: g0×99.
	counts := &segment.Counts{Genes: []string{"G0", "G1", "G2"}, Offsets: []uint32{0, 3, 4, 4, 6, 7}, Gene: []uint32{0, 1, 2, 0, 1, 2, 0}, Count: []uint32{5, 3, 2, 4, 1, 5, 99}}
	in := Input{Cells: cells, Counts: counts, Keep: []bool{true, true, true, true, false}, Label: []int32{0, 1, -1, 0, 1}, Tissue: 2e6}
	s, err := Summarize(in, 2)
	if err != nil {
		t.Fatal(err)
	}
	if s.Cells != 4 || s.Labeled != 3 || !reflect.DeepEqual(s.Counts, []int{2, 1}) {
		t.Fatalf("cells %d, labeled %d, counts %v", s.Cells, s.Labeled, s.Counts)
	}
	if !reflect.DeepEqual(s.Shares, []float64{2.0 / 3, 1.0 / 3}) || !reflect.DeepEqual(s.Density, []float64{1, 0.5}) {
		t.Errorf("shares %v, densities %v", s.Shares, s.Density)
	}
	if !reflect.DeepEqual(s.Pseudobulk, [][]uint64{{5, 4, 7}, {4, 0, 0}}) {
		t.Errorf("pseudobulk %v", s.Pseudobulk)
	}
	q := s.QC
	// Transcripts 10, 4, 0, 6: median 5; genes 3, 2, 0, 2: median 2; areas 100, 80, 60, 120:
	// median 90; nuclei 30, 20, 40: median 30, in 3 of 4 cells; 20 transcripts over 360 µm².
	want := QC{Cells: 4, CellsPerMm2: 2, MedianTranscripts: 5, MedianGenes: 2, MedianArea: 90, MedianNucleusArea: ptr(30), NucleusShare: ptr(0.75), TranscriptsPer100: 20.0 / 360 * 100, Transcripts: 20, NoTranscriptShare: 0.25}
	if !reflect.DeepEqual(q, want) {
		t.Errorf("QC %+v,\nwant %+v", q, want)
	}
	// A segmentation that records no nucleus: its nuclei's numbers absent, not zero.
	bare := append([]segment.Cell(nil), cells...)
	for i := range bare {
		bare[i].Nucleus, bare[i].NucleusArea = "", 0
	}
	in.Cells = bare
	if s2, err := Summarize(in, 2); err != nil || s2.QC.NucleusShare != nil || s2.QC.MedianNucleusArea != nil || s2.QC.MedianTranscripts != 5 {
		t.Errorf("no nuclei recorded: %+v %v", s2.QC, err)
	}
	// The pseudobulk's total is the labeled cells' transcripts.
	var total uint64
	for _, row := range s.Pseudobulk {
		for _, v := range row {
			total += v
		}
	}
	if total != 10+4+6 {
		t.Errorf("the pseudobulk holds %d transcripts", total)
	}
}

func ptr(v float64) *float64 { return &v }

func TestSummarizeRefuses(t *testing.T) {
	cells := []segment.Cell{{ID: "a", Area: 1}}
	counts := &segment.Counts{Genes: []string{"G"}, Offsets: []uint32{0, 0}}
	for name, in := range map[string]Input{
		"lengths":   {Cells: cells, Counts: counts, Keep: []bool{true, true}, Label: []int32{0}, Tissue: 1},
		"counts":    {Cells: cells, Counts: &segment.Counts{Offsets: []uint32{0}}, Keep: []bool{true}, Label: []int32{0}, Tissue: 1},
		"no tissue": {Cells: cells, Counts: counts, Keep: []bool{true}, Label: []int32{0}, Tissue: 0},
		"label":     {Cells: cells, Counts: counts, Keep: []bool{true}, Label: []int32{3}, Tissue: 1},
	} {
		if _, err := Summarize(in, 2); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if m := median([]float64{3, 1, 2}); m != 2 || median(nil) != 0 || math.Abs(median([]float64{1, 2})-1.5) > 0 {
		t.Error("median")
	}
}
