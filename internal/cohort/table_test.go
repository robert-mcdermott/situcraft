package cohort

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"strings"
	"testing"

	"situcraft/internal/spatial"
)

// A document of two samples written as the table and the pseudobulk's, read back: every number
// the document's (exactly), each with its source, and a gene a panel has not left empty.
func TestTable(t *testing.T) {
	tenth, fifth := 0.1, 0.2 // a sum whose shortest text is long
	d := &Document{Labels: LabelSet{Name: "Types", Names: []string{"T cell", "B, plasma"}}, Genes: []string{"CD3E", "MS4A1"}}
	one := SampleDoc{Sample: "s-1", Name: "First", Segmentation: spatial.Side{ID: "seg-1"}, Labels: SampleLabels{ID: "lab-1"},
		Summary:     Summary{Cells: 3, Labeled: 3, Tissue: 3e5, Counts: []int{2, 1}, Shares: []float64{2.0 / 3, 1.0 / 3}, Density: []float64{2 / 0.3, 1 / 0.3}, QC: QC{Cells: 3, Transcripts: 30, NucleusShare: ptr(1), MedianNucleusArea: ptr(20)}},
		NicheShares: []float64{0.25, 0.75}, Statistics: []Statistic{{"enrichment", "B, plasma|T cell", 1.5, "spa-1"}, {"moran", "CD3E", tenth + fifth, "spa-2"}}}
	two := SampleDoc{Sample: "s-2", Name: "Second \"core\"", Segmentation: spatial.Side{ID: "seg-2"}, Labels: SampleLabels{ID: "lab-2"},
		Summary:     Summary{Cells: 1, Labeled: 1, Tissue: 1e5, Counts: []int{0, 1}, Shares: []float64{0, 1}, Density: []float64{0, 10}},
		NicheShares: []float64{1, 0}, Statistics: []Statistic{}}
	d.Samples = []SampleDoc{one, two}
	rows, err := csv.NewReader(bytes.NewReader(TableCSV(d))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rows[0], ",") != tableHeader {
		t.Fatalf("header %v", rows[0])
	}
	want := Table(d)
	// The second sample's segmentation records no nucleus: its two nuclei's numbers left out.
	per := 3*2 + 2 + len(QCKeys)
	if len(rows)-1 != len(want) || len(want) != 2*per+2-2 {
		t.Fatalf("%d rows, %d numbers; want %d", len(rows)-1, len(want), 2*per)
	}
	for i, r := range want {
		got := rows[i+1]
		v, err := strconv.ParseFloat(got[4], 64)
		if err != nil || v != r.Value || got[0] != r.Sample || got[1] != r.Name || got[2] != r.Family || got[3] != r.Key || got[5] != r.Source {
			t.Errorf("row %d: %v, want %+v", i+1, got, r)
		}
	}
	find := func(sample, family, key string) []string {
		for _, r := range rows[1:] {
			if r[0] == sample && r[2] == family && r[3] == key {
				return r
			}
		}
		return nil
	}
	if r := find("s-2", "qc", "nucleus share"); r != nil {
		t.Errorf("a nucleus share not recorded, written: %v", r)
	}
	if r := find("s-1", "cells", "B, plasma"); r == nil || r[4] != "1" || r[5] != "lab-1" {
		t.Errorf("First's B cells: %v", r)
	}
	if r := find("s-1", "qc", "tissue area µm²"); r == nil || r[4] != "300000" || r[5] != "seg-1" {
		t.Errorf("First's tissue: %v", r)
	}
	if r := find("s-2", "niche", "niche 1"); r == nil || r[4] != "1" || r[5] != "" {
		t.Errorf("Second's niche 1: %v", r)
	}
	if r := find("s-1", "moran", "CD3E"); r == nil || r[4] != "0.30000000000000004" {
		t.Errorf("First's Moran's I: %v", r)
	}
	// The pseudobulk's: the second sample's panel has not MS4A1.
	pb := [][][]uint32{{{20, 0}, {1, 9}}, {{0, Missing}, {3, Missing}}}
	raw, err := PseudobulkCSV(d, pb)
	if err != nil {
		t.Fatal(err)
	}
	prows, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	wantP := [][]string{
		{"sample", "name", "label", "cells", "CD3E", "MS4A1"},
		{"s-1", "First", "T cell", "2", "20", "0"},
		{"s-1", "First", "B, plasma", "1", "1", "9"},
		{"s-2", "Second \"core\"", "T cell", "0", "0", ""},
		{"s-2", "Second \"core\"", "B, plasma", "1", "3", ""},
	}
	for i := range wantP {
		if strings.Join(prows[i], "|") != strings.Join(wantP[i], "|") {
			t.Errorf("pseudobulk row %d: %v, want %v", i, prows[i], wantP[i])
		}
	}
	if _, err := PseudobulkCSV(d, pb[:1]); err == nil {
		t.Error("a pseudobulk short a sample was written")
	}
}
