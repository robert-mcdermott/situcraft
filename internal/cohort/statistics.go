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
	switch {
	case a.Enrichment != nil:
		return EnrichmentStatistics(id, a.Labels.Names, a.Enrichment.Count, a.Enrichment.Expected)
	case a.Nearest != nil:
		return NearestStatistics(id, a.Labels.Names, a.Nearest.Median)
	case a.Moran != nil:
		return MoranStatistics(id, a.Moran.Genes, a.Moran.I)
	}
	return nil
}

// statistic adds a number to out when it is one.
func statistic(out []Statistic, family, key string, v float64, id string) []Statistic {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return out
	}
	return append(out, Statistic{Family: family, Key: key, Value: v, Source: id})
}

// EnrichmentStatistics are an enrichment's ratios of neighbors found over expected, by unordered
// pair of labels (count and expected K × K, row-major), those expected at all.
func EnrichmentStatistics(id string, names []string, count, expected []float64) []Statistic {
	var out []Statistic
	K := len(names)
	for x := 0; x < K; x++ {
		for y := x; y < K; y++ {
			q := x*K + y
			if expected[q] > 0 {
				pair := []string{names[x], names[y]}
				sort.Strings(pair)
				out = statistic(out, "enrichment", pair[0]+"|"+pair[1], count[q]/expected[q], id)
			}
		}
	}
	return out
}

// NearestStatistics are the median distances from each label's cells to the nearest of each
// label's (K × K, row-major).
func NearestStatistics(id string, names []string, median []float64) []Statistic {
	var out []Statistic
	K := len(names)
	for x := 0; x < K; x++ {
		for y := 0; y < K; y++ {
			out = statistic(out, "nearest", names[x]+"→"+names[y], median[x*K+y], id)
		}
	}
	return out
}

// MoranStatistics are the genes' Moran's I.
func MoranStatistics(id string, genes []string, I []float64) []Statistic {
	var out []Statistic
	for g, gene := range genes {
		out = statistic(out, "moran", gene, I[g], id)
	}
	return out
}
