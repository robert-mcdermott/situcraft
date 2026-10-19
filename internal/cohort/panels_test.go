package cohort

import (
	"reflect"
	"testing"
)

// Three samples on two panels, worked by hand: the second panel lacks a gene and adds one, reads a
// shared gene by another codeword, gives one an older Ensembl ID, and names a gene by another
// symbol under the same ID.
func TestComparePanels(t *testing.T) {
	a := Panel{Name: "Lung", Design: "hLung_v1.1", Version: "1.1.0", Genes: []PanelGene{
		{Name: "ACTA2", ID: "ENSG1", Codewords: []int{5}},
		{Name: "CD3E", ID: "ENSG2", Codewords: []int{9, 3}},
		{Name: "EPCAM", ID: "ENSG3", Codewords: []int{11}},
		{Name: "KRT5", ID: "ENSG4", Codewords: []int{12}},
		{Name: "C1orf43", ID: "ENSG7", Codewords: []int{20}},
	}}
	b := Panel{Name: "Breast", Design: "hBreast_v1", Version: "1.0.0", Genes: []PanelGene{
		{Name: "CD3E", ID: "ENSG2", Codewords: []int{3, 9}},
		{Name: "ACTA2", ID: "ENSG1", Codewords: []int{6}},
		{Name: "EPCAM", ID: "ENSG3old", Codewords: []int{11}},
		{Name: "ERBB2", ID: "ENSG5", Codewords: []int{14}},
		{Name: "CHMP1AOS", ID: "ENSG7", Codewords: []int{20}},
	}}
	d := ComparePanels([]string{"s-1", "s-2", "s-3"}, []Panel{a, b, a})
	if len(d.Panels) != 2 || d.Panels[0].Design != "hLung_v1.1" || d.Panels[0].Genes != 5 || !reflect.DeepEqual(d.Panels[0].Samples, []string{"s-1", "s-3"}) || !reflect.DeepEqual(d.Panels[1].Samples, []string{"s-2"}) || d.Panels[0].Checksum == d.Panels[1].Checksum {
		t.Fatalf("panels %+v", d.Panels)
	}
	// Shared: in the first panel's order.
	if !reflect.DeepEqual(d.Shared, []string{"ACTA2", "CD3E", "EPCAM"}) {
		t.Errorf("shared %v", d.Shared)
	}
	if !reflect.DeepEqual(d.Partial, []PartialGene{{Gene: "KRT5", ID: "ENSG4", Panels: []int{0}}, {Gene: "C1orf43", ID: "ENSG7", Panels: []int{0}}, {Gene: "ERBB2", ID: "ENSG5", Panels: []int{1}}, {Gene: "CHMP1AOS", ID: "ENSG7", Panels: []int{1}}}) {
		t.Errorf("partial %+v", d.Partial)
	}
	// CD3E's codewords are the same set in another order: alike.
	if !reflect.DeepEqual(d.Codewords, []CodewordGene{{Gene: "ACTA2", Codewords: [][]int{{5}, {6}}}}) {
		t.Errorf("codewords %+v", d.Codewords)
	}
	if !reflect.DeepEqual(d.IDs, []IDGene{{Gene: "EPCAM", IDs: []string{"ENSG3", "ENSG3old"}}, {ID: "ENSG7", Names: []string{"C1orf43", "CHMP1AOS"}}}) {
		t.Errorf("ids %+v", d.IDs)
	}
	if d.PanelWords() != "2 panels: 3 genes in every one, 4 in some" {
		t.Errorf("words %q", d.PanelWords())
	}
	// One panel: every gene shared, nothing else said.
	one := ComparePanels([]string{"s-1", "s-2"}, []Panel{a, a})
	if len(one.Panels) != 1 || len(one.Shared) != 5 || len(one.Partial)+len(one.Codewords)+len(one.IDs) != 0 || one.PanelWords() != "one panel" {
		t.Errorf("one panel: %+v", one)
	}
}
