package cohort

// A sample's numbers from its other artifacts: its graph's and its spatial analyses', each keyed by
// what it is of (labels and genes by name, so that the same analysis of every sample lines up) and
// traced to the artifact it came from.

import (
	"math"
	"sort"

	"situcraft/internal/spatial"
)

// Statistic is one number of a sample from another artifact: its family (graph, enrichment,
// nearest, moran), its key, its value and its source (the artifact's id).
type Statistic struct {
	Family string  `json:"family"`
	Key    string  `json:"key"`
	Value  float64 `json:"value"`
	Source string  `json:"source"`
}

// GraphStatistics are a graph's numbers: its cells' mean neighbors, its edges' median length (µm),
// its cells without a neighbor (a share) and its pieces.
func GraphStatistics(id string, s spatial.Summary) []Statistic {
	out := []Statistic{
		{Family: "graph", Key: "mean neighbors", Value: s.MeanDegree, Source: id},
		{Family: "graph", Key: "median edge µm", Value: s.MedianLength, Source: id},
		{Family: "graph", Key: "pieces", Value: float64(s.Components), Source: id},
	}
	if s.Cells > 0 {
		out = append(out, Statistic{Family: "graph", Key: "without a neighbor", Value: float64(s.Isolated) / float64(s.Cells), Source: id})
	}
	return out
}

// AnalysisStatistics are an analysis's numbers: an enrichment's ratio (neighbors found over
// expected) for each pair of labels ("A|B", the names in order), a nearest distance's median (µm)
// from each label to each ("A→B"), a gene's Moran's I (by the gene's name). Other questions give
// none. A number that is not one (no expected neighbors) is left out.
func AnalysisStatistics(id string, a *spatial.Analysis) []Statistic {
	var out []Statistic
	names := a.Labels.Names
	K := len(names)
	add := func(family, key string, v float64) {
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			out = append(out, Statistic{Family: family, Key: key, Value: v, Source: id})
		}
	}
	switch {
	case a.Enrichment != nil:
		e := a.Enrichment
		for x := 0; x < K; x++ {
			for y := x; y < K; y++ {
				q := x*K + y
				if e.Expected[q] > 0 {
					pair := []string{names[x], names[y]}
					sort.Strings(pair)
					add("enrichment", pair[0]+"|"+pair[1], e.Count[q]/e.Expected[q])
				}
			}
		}
	case a.Nearest != nil:
		for x := 0; x < K; x++ {
			for y := 0; y < K; y++ {
				add("nearest", names[x]+"→"+names[y], a.Nearest.Median[x*K+y])
			}
		}
	case a.Moran != nil:
		for g, gene := range a.Moran.Genes {
			add("moran", gene, a.Moran.I[g])
		}
	}
	return out
}
