package spatial

import (
	"context"
	"math"
	"testing"

	"situcraft/internal/random"
	"situcraft/internal/stats"
)

func enrich(t *testing.T, g *Graph, label []int32, K int, stratum []int32, p EnrichmentParams, workers int) *Enrichment {
	t.Helper()
	e, err := Enrich(context.Background(), g, label, K, stratum, p, workers, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// A checkerboard on a square lattice, its 4 nearest joined: every edge joins the two labels, so
// they are as enriched as they can be, and each with itself as depleted.
func TestEnrichmentOfACheckerboard(t *testing.T) {
	const side = 20
	x, y := lattice(side, 10, false)
	g := build(t, Inputs{X: x, Y: y}, Params{Kind: Radius, Radius: 10}, 2)
	label := make([]int32, side*side)
	for r := 0; r < side; r++ {
		for c := 0; c < side; c++ {
			label[r*side+c] = int32((r + c) % 2)
		}
	}
	e := enrich(t, g, label, 2, nil, EnrichmentParams{Permutations: 500, Seed: 1}, 4)
	edges := float64(2 * side * (side - 1))
	if e.Count[0*2+1] != edges || e.Count[1*2+0] != edges || e.Count[0] != 0 || e.Count[3] != 0 {
		t.Fatalf("counts %v, not %g between and none within", e.Count, edges)
	}
	// Expected: each edge's ends differ with chance 2·200·200 / (400·399).
	want := edges * 2 * 200 * 200 / (400 * 399)
	if math.Abs(e.Expected[1]-want) > 1e-9 {
		t.Errorf("expected %g, not %g", e.Expected[1], want)
	}
	if !(e.Z[1] > 20) || !(e.Z[0] < -20) || e.P[1] != 1.0/501 || e.P[0] != 1.0/501 || e.PAdj[1] > 0.01 {
		t.Errorf("z %v, p %v, adjusted %v", e.Z, e.P, e.PAdj)
	}
}

// randomCase is a graph of random cells and random labels.
func randomCase(t *testing.T, n, K int, seed uint64) (*Graph, []int32) {
	s := random.New(seed, 9)
	x, y := make([]float32, n), make([]float32, n)
	for i := range x {
		x[i], y[i] = float32(300*s.Float64()), float32(300*s.Float64())
	}
	g := build(t, Inputs{X: x, Y: y}, Params{Kind: Delaunay}, 2)
	label := make([]int32, n)
	for i := range label {
		label[i] = int32(s.IntN(K+1)) - 1 // some cells unlabeled
	}
	return g, label
}

// The permutations' mean is the exact expectation, within its standard error, with and without
// strata.
func TestPermutationsMeanTheExpectation(t *testing.T) {
	g, label := randomCase(t, 600, 4, 3)
	stratum := make([]int32, len(label))
	for i := range stratum {
		if g.X[i] > 150 {
			stratum[i] = 1
		}
		if g.Y[i] > 200 {
			stratum[i] = 2
		}
	}
	for _, st := range [][]int32{nil, stratum} {
		e := enrich(t, g, label, 4, st, EnrichmentParams{Permutations: 20000, Seed: 7}, 8)
		for c := range e.Count {
			se := e.SD[c] / math.Sqrt(20000)
			if math.Abs(e.Mean[c]-e.Expected[c]) > 4.5*se+1e-12 {
				t.Errorf("strata %v, pair %d: the permutations' mean %g, the expectation %g (standard error %g)", st != nil, c, e.Mean[c], e.Expected[c], se)
			}
		}
	}
}

// Labels dealt at random: their p-values are uniform.
func TestPValuesOfRandomLabelsAreUniform(t *testing.T) {
	g, _ := randomCase(t, 400, 3, 5)
	s := random.New(11, 1)
	var ps []float64
	for rep := 0; rep < 300; rep++ {
		label := make([]int32, len(g.Rows))
		for i := range label {
			label[i] = int32(s.IntN(3))
		}
		e := enrich(t, g, label, 3, nil, EnrichmentParams{Permutations: 199, Seed: uint64(rep)}, 4)
		ps = append(ps, e.P[0*3+1]) // one pair a replicate: independent draws
	}
	// Permutation p-values take the values k/200: compared with their own discrete distribution.
	d, p := stats.KS(ps, func(v float64) float64 { return math.Min(1, math.Floor(v*200+1e-9)/200) })
	t.Logf("p-values of random labels: KS D = %.3f, p = %.3f", d, p)
	if p < 0.001 {
		t.Errorf("the p-values of random labels are not uniform (KS p = %g)", p)
	}
}

// Strata that each hold one label: dealing within them changes nothing.
func TestStrataKeepTheirLabels(t *testing.T) {
	g, _ := randomCase(t, 300, 2, 8)
	label := make([]int32, len(g.Rows))
	stratum := make([]int32, len(g.Rows))
	for i := range label {
		if g.X[i] > 150 {
			label[i], stratum[i] = 1, 1
		}
	}
	e := enrich(t, g, label, 2, stratum, EnrichmentParams{Permutations: 200, Seed: 2}, 3)
	for c := range e.Count {
		if e.SD[c] != 0 || !math.IsNaN(e.Z[c]) || e.P[c] != 1 || math.Abs(e.Mean[c]-e.Count[c]) > 1e-9 || math.Abs(e.Expected[c]-e.Count[c]) > 1e-9 {
			t.Errorf("pair %d: count %g, expected %g, mean %g, sd %g, z %g, p %g", c, e.Count[c], e.Expected[c], e.Mean[c], e.SD[c], e.Z[c], e.P[c])
		}
	}
}

// The same results on one worker and on eight, bit for bit; weighed by shared boundaries in a
// contact graph, the counts are the boundaries' sums.
func TestEnrichmentIsTheSameOnAnyWorkers(t *testing.T) {
	g, label := randomCase(t, 800, 5, 4)
	one := enrich(t, g, label, 5, nil, DefaultEnrichment(), 1)
	eight := enrich(t, g, label, 5, nil, DefaultEnrichment(), 8)
	for c := range one.Count {
		same := func(a, b float64) bool { return a == b || math.IsNaN(a) && math.IsNaN(b) }
		if !same(one.Mean[c], eight.Mean[c]) || !same(one.SD[c], eight.SD[c]) || !same(one.Z[c], eight.Z[c]) || one.P[c] != eight.P[c] || one.PAdj[c] != eight.PAdj[c] {
			t.Fatalf("pair %d differs on eight workers", c)
		}
	}
	mask := raster([][]uint32{
		{1, 1, 2, 2, 3},
		{1, 1, 2, 2, 3},
		{4, 4, 4, 3, 3},
	}, 0.5)
	cx := []float32{0.5, 1.5, 2.25, 1}
	cy := []float32{0.5, 0.5, 0.75, 1.25}
	cg := build(t, Inputs{X: cx, Y: cy, Mask: mask}, Params{Kind: Contact}, 1)
	e := enrich(t, cg, []int32{0, 1, 1, 0}, 2, nil, EnrichmentParams{Permutations: 100, Seed: 1, Weight: "boundary"}, 2)
	// Shared (pixel edges of 0.5 µm): 1–2 two edges (1 µm), 2–3 three (1.5), 1–4 two (1), 2–4 one
	// (0.5), 3–4 one (0.5). Labels: cells 1 and 4 the first, 2 and 3 the second; within a label
	// an edge counts from both ends.
	want := []float64{2 * 1, 1 + 0.5 + 0.5, 1 + 0.5 + 0.5, 2 * 1.5}
	for c := range want {
		if math.Abs(e.Count[c]-want[c]) > 1e-6 {
			t.Errorf("boundary counts %v, not %v", e.Count, want)
			break
		}
	}
	if _, err := Enrich(context.Background(), g, label, 5, nil, EnrichmentParams{Permutations: 100, Weight: "boundary"}, 1, nil); err == nil {
		t.Error("a graph without boundaries weighed by them")
	}
}

// Co-occurrence by hand: cells on a line, A at 0 and 12, B at 5 and 30; pairs A–B at 5, 7, 18 and
// 30 µm, A–A at 12, B–B at 25. Within 10, 20 and 40 µm (Squidpy 1.8.3 gives the same, run on these
// cells), and between them.
func TestCooccurrenceByHand(t *testing.T) {
	x := []float32{0, 5, 12, 30}
	y := []float32{0, 0, 0, 0}
	for _, c := range []struct {
		pairs string
		count [][]float64
		ratio [][]float64
	}{
		// Within 10: two A–B; within 20: three A–B, one A–A (rows A 5, B 3, total 8); within 40:
		// four A–B, one A–A, one B–B (rows 6 and 6, total 12).
		{PairsWithin, [][]float64{{0, 2, 2, 0}, {2, 3, 3, 0}, {2, 4, 4, 2}}, [][]float64{{0, 2, 2, 0}, {0.64, 1.6, 1.6, 0}, {2.0 / 3, 4.0 / 3, 4.0 / 3, 2.0 / 3}}},
		// Between: (10, 20]: one A–A, one A–B (rows A 3, B 1, total 4); (20, 40]: one A–B, one B–B.
		{PairsBetween, [][]float64{{0, 2, 2, 0}, {2, 1, 1, 0}, {0, 1, 1, 2}}, [][]float64{{0, 2, 2, 0}, {8.0 / 9, 4.0 / 3, 4.0 / 3, 0}, {0, 4.0 / 3, 4.0 / 3, 8.0 / 9}}},
	} {
		co, err := Cooccur(context.Background(), x, y, []int32{0, 1, 0, 1}, 2, CooccurrenceParams{Distances: []float64{10, 20, 40}, Pairs: c.pairs}, 2)
		if err != nil {
			t.Fatal(err)
		}
		for band := range c.count {
			for q := range c.count[band] {
				if co.Count[band][q] != c.count[band][q] || math.Abs(co.Ratio[band][q]-c.ratio[band][q]) > 1e-12 {
					t.Errorf("%s, distance %d: counts %v ratios %v, not %v %v", c.pairs, band, co.Count[band], co.Ratio[band], c.count[band], c.ratio[band])
					break
				}
			}
		}
	}
}

// Co-occurrence's counts against an exhaustive count of every pair, within and between.
func TestCooccurrenceAgainstEveryPair(t *testing.T) {
	s := random.New(13, 1)
	n, K := 900, 3
	x, y, label := make([]float32, n), make([]float32, n), make([]int32, n)
	for i := 0; i < n; i++ {
		x[i], y[i], label[i] = float32(400*s.Float64()), float32(400*s.Float64()), int32(s.IntN(K+1))-1
	}
	for _, pairs := range []string{PairsWithin, PairsBetween} {
		p := DefaultCooccurrence()
		p.Pairs = pairs
		c, err := Cooccur(context.Background(), x, y, label, K, p, 4)
		if err != nil {
			t.Fatal(err)
		}
		nb := len(p.Distances)
		want := make([]float64, nb*K*K)
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				if label[i] < 0 || label[j] < 0 {
					continue
				}
				d := distance(x[i], y[i], x[j], y[j])
				for band := 0; band < nb; band++ {
					lo := 0.0
					if band > 0 && pairs == PairsBetween {
						lo = p.Distances[band-1]
					}
					if d <= p.Distances[band] && (d > lo || band == 0 || pairs == PairsWithin) {
						want[band*K*K+int(label[i])*K+int(label[j])]++
						want[band*K*K+int(label[j])*K+int(label[i])]++
					}
				}
			}
		}
		for band := 0; band < nb; band++ {
			for q := 0; q < K*K; q++ {
				if c.Count[band][q] != want[band*K*K+q] {
					t.Fatalf("%s, distance %d, pair %d: %g pairs, the exhaustive count %g", pairs, band, q, c.Count[band][q], want[band*K*K+q])
				}
			}
		}
	}
}
