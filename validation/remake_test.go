package validation

// Analyses across segmentations (wave 6, slice 7; research.md §6.7): the standard simulated tissue
// segmented three ways, each labeled from its types' markers (three_test.go), and the same analyses
// made under each, as the Spatial view makes a saved analysis again on another segmentation: the
// enrichment of neighbors over the Delaunay graph, spatially variable genes (Moran's I over the same
// graph), and niches of each cell's 30 nearest labeled cells as many as the kinds of domain. The
// instrument's and the refinement's numbers are read against the truth's: how alike their order
// (Spearman's rank correlation), the largest change, the pairs the truth finds enriched or avoided
// found so, and the niches against the kinds of domain the simulator drew the cells from.

import (
	"context"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"

	"situcraft/internal/cluster"
	"situcraft/internal/dmath"
	"situcraft/internal/segment"
	"situcraft/internal/simulate"
	"situcraft/internal/spatial"
)

// analysesUnder are one segmentation's analyses, by the names the Spatial view keys them by.
type analysesUnder struct {
	ratio       map[string]float64 // label pair → neighbors found over expected
	significant map[string]bool    // label pair → adjusted p below 0.05
	moran       map[string]float64 // gene → Moran's I
	markerLow   float64            // the markers' least I
	markerMean  float64            // the markers' mean I
	otherHigh   float64            // the unmarked genes' greatest I
	nicheARI    float64            // the niches against the kinds of domain
	agreement   float64            // the further seeds' least agreement
	cells       int
	edges       int
}

func analyzeUnder(t *testing.T, seg labeledSeg, ds []simulate.Domain, kinds int, marker map[string]bool) analysesUnder {
	t.Helper()
	ctx := context.Background()
	workers := runtime.NumCPU()
	cells, err := segment.ReadCells(seg.dir)
	if err != nil {
		t.Fatal(err)
	}
	in := spatial.Inputs{X: make([]float32, len(cells)), Y: make([]float32, len(cells))}
	for i, c := range cells {
		in.X[i], in.Y[i] = c.X, c.Y
	}
	g, err := spatial.Build(ctx, in, spatial.DefaultParams(spatial.Delaunay), workers, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := seg.labels.Names
	K := len(names)
	lab := make([]int32, len(g.Rows))
	for i, r := range g.Rows {
		lab[i] = seg.labels.Label[r]
	}
	e, err := spatial.Enrich(ctx, g, lab, K, nil, spatial.DefaultEnrichment(), workers, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := analysesUnder{ratio: map[string]float64{}, significant: map[string]bool{}, moran: map[string]float64{}, cells: len(g.Rows), edges: g.Edges()}
	for a := 0; a < K; a++ {
		for b := a; b < K; b++ {
			q := a*K + b
			if !(e.Expected[q] > 0) {
				continue
			}
			pair := []string{names[a], names[b]}
			sort.Strings(pair)
			key := strings.Join(pair, "|")
			out.ratio[key] = e.Count[q] / e.Expected[q]
			out.significant[key] = e.PAdj[q] < 0.05
		}
	}
	values, _, _, err := spatial.NormalizedExpression(seg.counts, g.Rows)
	if err != nil {
		t.Fatal(err)
	}
	m, err := spatial.MoransI(ctx, g, values, len(seg.counts.Genes), spatial.MoranParams{Weights: "row"}, workers, nil)
	if err != nil {
		t.Fatal(err)
	}
	out.markerLow, out.otherHigh = math.Inf(1), math.Inf(-1)
	markers := 0
	for gene, name := range seg.counts.Genes {
		if math.IsNaN(m.I[gene]) {
			continue
		}
		out.moran[name] = m.I[gene]
		if marker[name] {
			out.markerLow = math.Min(out.markerLow, m.I[gene])
			out.markerMean += m.I[gene]
			markers++
		} else {
			out.otherHigh = math.Max(out.otherHigh, m.I[gene])
		}
	}
	out.markerMean /= float64(markers)
	// Niches of every cell, against the kind of domain at its centroid (the domain whose center is
	// nearest, as the simulator draws them).
	label := make([]int32, len(cells))
	copy(label, seg.labels.Label)
	p := spatial.DefaultNiches()
	p.Neighbors, p.Niches = 30, kinds
	nc, err := spatial.FindNiches(ctx, in.X, in.Y, label, K, p, workers, nil)
	if err != nil {
		t.Fatal(err)
	}
	var assigned, kind []int32
	for i, c := range cells {
		if nc.Assign[i] < 0 {
			continue
		}
		best, bestD := 0, math.Inf(1)
		for d, dm := range ds {
			dx, dy := float64(c.X)-dm.X, float64(c.Y)-dm.Y
			if dd := dx*dx + dy*dy; dd < bestD {
				best, bestD = d, dd
			}
		}
		assigned = append(assigned, nc.Assign[i])
		kind = append(kind, int32(ds[best].Dominant))
	}
	out.nicheARI = cluster.AdjustedRand(assigned, kind)
	out.agreement = 1
	for _, a := range nc.Agreement {
		out.agreement = math.Min(out.agreement, a)
	}
	return out
}

// compared reads two segmentations' numbers together, over those of both: Spearman's rank
// correlation, and the largest change (log₂ of the ratio for a ratio; the difference otherwise).
func compared(a, b map[string]float64, ratio bool) (rho, largest float64, at string, n int) {
	var keys []string
	for k := range a {
		if _, ok := b[k]; ok && (!ratio || (a[k] > 0 && b[k] > 0)) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	x, y := make([]float64, len(keys)), make([]float64, len(keys))
	for i, k := range keys {
		x[i], y[i] = a[k], b[k]
		change := b[k] - a[k]
		if ratio {
			change = dmath.Log(b[k]/a[k]) / math.Ln2
		}
		if math.Abs(change) > math.Abs(largest) {
			largest, at = change, k
		}
	}
	return pearson(ranks(x), ranks(y)), largest, at, len(keys)
}

func TestAnalysesAcrossSegmentations(t *testing.T) {
	if testing.Short() || os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 to analyze the simulated tissue under three segmentations")
	}
	th := threeSegmentations(t)
	ds, err := simulate.Domains(simulate.Presets["standard"])
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[int]bool{}
	for _, d := range ds {
		kinds[d.Dominant] = true
	}
	marker := map[string]bool{}
	for _, names := range simulate.MarkerNames(simulate.Presets["standard"]) {
		for _, name := range names {
			marker[name] = true
		}
	}
	under := map[string]analysesUnder{}
	for _, name := range []string{"truth", "instrument", "refinement"} {
		r := analyzeUnder(t, th.segs[name], ds, len(kinds), marker)
		under[name] = r
		t.Logf("%s: %d cells, %d edges; markers' Moran's I %.3f or more, unmarked genes' %.3f at most; %d niches against the kinds of domain, adjusted Rand index %.3f (seeds agreeing %.3f or more)",
			name, r.cells, r.edges, r.markerLow, r.otherHigh, len(kinds), r.nicheARI, r.agreement)
	}
	truth := under["truth"]
	only := func(v map[string]float64) map[string]float64 {
		out := map[string]float64{}
		for k, x := range v {
			if marker[k] {
				out[k] = x
			}
		}
		return out
	}
	largest := map[string]float64{}
	for _, name := range []string{"instrument", "refinement"} {
		r := under[name]
		rho, change, at, pairs := compared(truth.ratio, r.ratio, true)
		kept, found := 0, 0
		for k, sig := range truth.significant {
			if !sig {
				continue
			}
			found++
			if v, ok := r.ratio[k]; ok && r.significant[k] && (v > 1) == (truth.ratio[k] > 1) {
				kept++
			}
		}
		mrho, mchange, mat, genes := compared(truth.moran, r.moran, false)
		krho, _, _, markers := compared(only(truth.moran), only(r.moran), false)
		largest[name] = math.Abs(mchange)
		t.Logf("%s against the truth: enrichment of %d label pairs, Spearman %.3f, the largest change %+.3f (log₂, %s: %.3f → %.3f); of the truth's %d pairs enriched or avoided, %d found so", name, pairs, rho, change, at, truth.ratio[at], r.ratio[at], found, kept)
		t.Logf("  Moran's I of %d genes, Spearman %.3f (of the %d markers %.3f), the largest change %+.4f (%s: %.4f → %.4f), the markers' mean %.4f against the truth's %.4f; niches %.3f against the truth's %.3f",
			genes, mrho, markers, krho, mchange, mat, truth.moran[mat], r.moran[mat], r.markerMean, truth.markerMean, r.nicheARI, truth.nicheARI)
		if !(rho > 0.95) || math.Abs(change) > 0.5 || kept != found || found == 0 {
			t.Errorf("%s: the enrichment's numbers are not the truth's, near enough", name)
		}
		if !(r.markerLow > r.otherHigh) || !(krho > 0.8) {
			t.Errorf("%s: the markers' Moran's I does not stand above the unmarked genes', in the truth's order", name)
		}
		if !(r.nicheARI > 0.85) || math.Abs(r.nicheARI-truth.nicheARI) > 0.05 {
			t.Errorf("%s: the niches do not find the kinds of domain as the truth's do", name)
		}
	}
	// The instrument's expansion takes transcripts from its neighbors, so neighbors are alike in
	// more than their type: its markers' Moran's I is above the truth's; the refinement's near it.
	if !(under["instrument"].markerMean > truth.markerMean*1.2) || !(largest["refinement"] < 0.06) || !(largest["refinement"] < largest["instrument"]/2) {
		t.Errorf("the instrument's Moran's I is not inflated by what its cells take from their neighbors, or the refinement's is not near the truth's")
	}
}
