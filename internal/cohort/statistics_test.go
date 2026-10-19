package cohort

import (
	"math"
	"reflect"
	"testing"

	"situcraft/internal/spatial"
)

func TestStatistics(t *testing.T) {
	g := GraphStatistics("gra-1", spatial.Summary{Cells: 10, MeanDegree: 5.5, MedianLength: 11.25, Isolated: 2, Components: 1})
	if len(g) != 4 || g[0].Value != 5.5 || g[1].Value != 11.25 || g[3].Value != 0.2 || g[3].Source != "gra-1" {
		t.Errorf("graph: %+v", g)
	}
	// Labels named out of order: the pair's key names them in order.
	e := AnalysisStatistics("spa-e", &spatial.Analysis{Labels: spatial.LabelSet{Names: []string{"Tumor", "Immune"}},
		Enrichment: &spatial.EnrichmentResult{Count: spatial.Nums{10, 6, 6, 4}, Expected: spatial.Nums{5, 3, 3, 0}}})
	want := []Statistic{{"enrichment", "Tumor|Tumor", 2, "spa-e"}, {"enrichment", "Immune|Tumor", 2, "spa-e"}}
	if !reflect.DeepEqual(e, want) {
		t.Errorf("enrichment: %+v", e)
	}
	n := AnalysisStatistics("spa-n", &spatial.Analysis{Labels: spatial.LabelSet{Names: []string{"A", "B"}}, Nearest: &spatial.NearestResult{Median: spatial.Nums{4, 7.5, math.NaN(), 3}}})
	if len(n) != 3 || n[1].Key != "A→B" || n[1].Value != 7.5 || n[2].Key != "B→B" {
		t.Errorf("nearest: %+v", n)
	}
	m := AnalysisStatistics("spa-m", &spatial.Analysis{Moran: &spatial.MoranResult{Genes: []string{"G1", "G2"}, I: spatial.Nums{0.4, -0.02}}})
	if len(m) != 2 || m[0].Key != "G1" || m[1].Value != -0.02 || m[0].Family != "moran" {
		t.Errorf("moran: %+v", m)
	}
	if AnalysisStatistics("x", &spatial.Analysis{}) != nil {
		t.Error("another question gave numbers")
	}
}
