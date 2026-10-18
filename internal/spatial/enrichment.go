package spatial

// Neighborhood enrichment: for each pair of labels, how often their cells are neighbors in a graph,
// against how often they would be if the labels were dealt to the same cells at random (Schapiro D,
// Jackson HW, Raghuraman S, et al. histoCAT: analysis of cell phenotypes and interactions in
// multiplex image cytometry data. Nat Methods 2017;14:873–876. doi:10.1038/nmeth.4391; Palla G,
// Spitzer H, Klein M, et al. Squidpy: a scalable framework for spatial omics analysis. Nat Methods
// 2022;19:171–178. doi:10.1038/s41592-021-01358-2).
//
// The counts are directed, as Squidpy's: count[A][B] is the sum, over the cells of label A, of their
// neighbors of label B (an edge between two cells of A counts twice), each weighted by 1 or, in a
// contact graph, by the boundary the two cells share. The cells without a label keep their places
// and count for no pair. Under the null, the labels are dealt again to the labeled cells (within
// each stratum, a region, when strata are given, so that each keeps its composition): the expected
// count is computed exactly (each edge's chance of joining the two labels, summed), and the
// permutations, each from its own seeded stream, give the count's spread: z = (count − the
// permutations' mean) / their standard deviation, and a two-sided p-value, the share of permutations
// at least as far from the expectation as the count ((1 + k) / (1 + n)), so never below
// 1 / (n + 1). A pair with a label no cell has cannot be tested: its z and p are NaN, and it is left
// out of the adjustment. The p-values of the other unordered pairs are adjusted by Benjamini and
// Hochberg's procedure. The ratio count / expected is the effect's size; z and p say how unusual it
// is in this section, whose cells are not replicates.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"

	"situcraft/internal/random"
	"situcraft/internal/stats"
)

// EnrichmentParams are an enrichment's parameters.
type EnrichmentParams struct {
	Permutations int    `json:"permutations"`
	Seed         uint64 `json:"seed"`
	// Weight: "edges" (each neighbor counts 1) or "boundary" (its shared boundary, µm; contact
	// graphs only).
	Weight string `json:"weight"`
}

// DefaultEnrichment are the defaults: 1,000 permutations (Squidpy's), seed 20250, edges.
func DefaultEnrichment() EnrichmentParams {
	return EnrichmentParams{Permutations: 1000, Seed: 20250, Weight: "edges"}
}

// Check says what is wrong with the parameters.
func (p *EnrichmentParams) Check() error {
	if p.Permutations < 100 || p.Permutations > 100000 {
		return errors.New("an enrichment's permutations are 100 to 100,000")
	}
	if p.Weight == "" {
		p.Weight = "edges"
	}
	if p.Weight != "edges" && p.Weight != "boundary" {
		return fmt.Errorf("an enrichment weighs each neighbor by edges or boundary, not %q", p.Weight)
	}
	return nil
}

// Enrichment is an enrichment's result: K × K matrices, row-major, [A*K+B].
type Enrichment struct {
	K int
	// Cells of each label in the graph; Edges counted (between labeled cells); Strata used.
	Cells  []int
	Edges  int
	Strata int
	// Count: the directed counts (or boundaries); Expected under the null, exactly; Mean and SD
	// of the permutations' counts.
	Count, Expected, Mean, SD []float64
	// Z: (count − mean) / SD (NaN when SD is 0); P: two-sided, from the permutations; PAdj: P
	// adjusted over the unordered pairs.
	Z, P, PAdj []float64
}

// Enrich computes the enrichment of a graph's labeled cells. label: each graph cell's label (0 to
// K−1, or −1 for none); stratum: each graph cell's stratum (0 to S−1), or nil for one.
func Enrich(ctx context.Context, g *Graph, label []int32, K int, stratum []int32, p EnrichmentParams, workers int, progress func(float64)) (*Enrichment, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	n := len(g.Rows)
	if len(label) != n || stratum != nil && len(stratum) != n {
		return nil, errors.New("spatial: the labels or strata are not one a cell of the graph")
	}
	if K < 1 || K > 1000 {
		return nil, errors.New("spatial: an enrichment of 1 to 1,000 labels")
	}
	if p.Weight == "boundary" && g.Shared == nil {
		return nil, errors.New("spatial: only a contact graph's neighbors are weighed by the boundary they share")
	}
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	if progress == nil {
		progress = func(float64) {}
	}
	S := 1
	for i := 0; i < n; i++ {
		if label[i] < -1 || label[i] >= int32(K) {
			return nil, fmt.Errorf("spatial: cell %d has label %d of %d", i, label[i], K)
		}
		if stratum != nil {
			if stratum[i] < 0 {
				return nil, fmt.Errorf("spatial: cell %d has no stratum", i)
			}
			S = max(S, int(stratum[i])+1)
		}
	}
	strat := func(i int) int {
		if stratum == nil {
			return 0
		}
		return int(stratum[i])
	}
	// The edges between labeled cells, each once, with its weight.
	var ea, eb []int32
	var ew []float64
	for i := 0; i < n; i++ {
		if label[i] < 0 {
			continue
		}
		for k := g.Offsets[i]; k < g.Offsets[i+1]; k++ {
			j := g.Neighbors[k]
			if j > uint32(i) && label[j] >= 0 {
				w := 1.0
				if p.Weight == "boundary" {
					w = float64(g.Shared[k])
				}
				ea, eb, ew = append(ea, int32(i)), append(eb, int32(j)), append(ew, w)
			}
		}
	}
	e := &Enrichment{K: K, Cells: make([]int, K), Edges: len(ea), Strata: S}
	for i := 0; i < n; i++ {
		if label[i] >= 0 {
			e.Cells[label[i]]++
		}
	}
	count := func(lab []int32, out []float64) {
		for i := range out {
			out[i] = 0
		}
		for k := range ea {
			a, b := lab[ea[k]], lab[eb[k]]
			out[int(a)*K+int(b)] += ew[k]
			out[int(b)*K+int(a)] += ew[k]
		}
	}
	e.Count = make([]float64, K*K)
	count(label, e.Count)
	e.Expected = expectedCounts(label, strat, n, K, S, ea, eb, ew)

	// The labeled cells of each stratum, whose labels the permutations deal again.
	positions := make([][]int32, S)
	for i := 0; i < n; i++ {
		if label[i] >= 0 {
			positions[strat(i)] = append(positions[strat(i)], int32(i))
		}
	}
	// The permutations in fixed blocks, each summed in order about the exact expectation (so the
	// variance loses nothing to cancellation), then the blocks in order: the same sums on any
	// number of workers.
	P := p.Permutations
	const block = 50
	blocks := (P + block - 1) / block
	type sums struct {
		s1, s2 []float64
		far    []int
	}
	partial := make([]sums, blocks)
	dev := make([]float64, K*K)
	for c := range dev {
		dev[c] = math.Abs(e.Count[c] - e.Expected[c])
	}
	var finished int64
	var mu sync.Mutex
	errs := make([]error, workers)
	parallelFor(blocks, workers, func(w, a, b int) {
		lab := append([]int32(nil), label...)
		vals := make([]int32, 0, n)
		counts := make([]float64, K*K)
		for blk := a; blk < b; blk++ {
			if ctx.Err() != nil {
				errs[w] = ctx.Err()
				return
			}
			out := sums{s1: make([]float64, K*K), s2: make([]float64, K*K), far: make([]int, K*K)}
			for r := blk * block; r < min(P, (blk+1)*block); r++ {
				// Permutation r's own stream: the same deal for any number of workers.
				s := random.New(p.Seed, uint64(r))
				copy(lab, label)
				for _, pos := range positions {
					vals = vals[:0]
					for _, i := range pos {
						vals = append(vals, label[i])
					}
					for k := len(vals) - 1; k > 0; k-- {
						j := s.IntN(k + 1)
						vals[k], vals[j] = vals[j], vals[k]
					}
					for k, i := range pos {
						lab[i] = vals[k]
					}
				}
				count(lab, counts)
				for c, v := range counts {
					d := v - e.Expected[c]
					out.s1[c] += d
					out.s2[c] += mul(d, d)
					// At least as far from the expectation, allowing for the sums' rounding.
					if math.Abs(d) >= dev[c]-1e-9*math.Max(1, dev[c]) {
						out.far[c]++
					}
				}
			}
			partial[blk] = out
			mu.Lock()
			finished++
			progress(float64(finished) / float64(blocks))
			mu.Unlock()
		}
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	e.Mean, e.SD, e.Z, e.P = make([]float64, K*K), make([]float64, K*K), make([]float64, K*K), make([]float64, K*K)
	for c := 0; c < K*K; c++ {
		var s1, s2 float64
		far := 0
		for _, blk := range partial {
			s1 += blk.s1[c]
			s2 += blk.s2[c]
			far += blk.far[c]
		}
		e.Mean[c] = e.Expected[c] + s1/float64(P)
		e.SD[c] = math.Sqrt(math.Max(0, s2-mul(s1, s1)/float64(P)) / float64(P-1))
		e.Z[c] = math.NaN()
		if e.SD[c] > 0 {
			e.Z[c] = (e.Count[c] - e.Mean[c]) / e.SD[c]
		}
		e.P[c] = float64(1+far) / float64(1+P)
	}
	// Adjusted over the unordered pairs (A ≤ B) that can be tested, each written to both cells of
	// the matrix; a pair with a label no cell has is not a test.
	var ps []float64
	var tested [][2]int
	e.PAdj = make([]float64, K*K)
	for a := 0; a < K; a++ {
		for b := a; b < K; b++ {
			if e.Cells[a] == 0 || e.Cells[b] == 0 {
				for _, q := range []int{a*K + b, b*K + a} {
					e.Z[q], e.P[q], e.PAdj[q] = math.NaN(), math.NaN(), math.NaN()
				}
				continue
			}
			ps = append(ps, e.P[a*K+b])
			tested = append(tested, [2]int{a, b})
		}
	}
	adj := make([]float64, len(ps))
	stats.BenjaminiHochberg(ps, adj)
	for k, ab := range tested {
		a, b := ab[0], ab[1]
		e.PAdj[a*K+b], e.PAdj[b*K+a] = adj[k], adj[k]
	}
	return e, nil
}

// expectedCounts are the directed counts' exact expectations when the labeled cells' labels are
// dealt again within their strata: each edge between strata s and t adds its weight times the
// chance that its ends take labels A and B, n_As·n_Bt / (N_s·N_t) when s ≠ t, and within a stratum
// n_As·(n_Bs − [A = B]) / (N_s·(N_s − 1)) (dealing without replacement).
func expectedCounts(label []int32, strat func(int) int, n, K, S int, ea, eb []int32, ew []float64) []float64 {
	cells := make([]float64, S*K) // n_As
	total := make([]float64, S)   // N_s
	for i := 0; i < n; i++ {
		if label[i] >= 0 {
			cells[strat(i)*K+int(label[i])]++
			total[strat(i)]++
		}
	}
	// The edges' weights by the strata of their ends (ordered as the edge is, s then t).
	weight := make([]float64, S*S)
	for k := range ea {
		weight[strat(int(ea[k]))*S+strat(int(eb[k]))] += ew[k]
	}
	out := make([]float64, K*K)
	for s := 0; s < S; s++ {
		for t := 0; t < S; t++ {
			w := weight[s*S+t]
			if w == 0 {
				continue
			}
			for a := 0; a < K; a++ {
				for b := 0; b < K; b++ {
					var q float64
					if s == t {
						nb := cells[t*K+b]
						if a == b {
							nb--
						}
						q = mul(cells[s*K+a], nb) / mul(total[s], total[s]-1)
					} else {
						q = mul(cells[s*K+a], cells[t*K+b]) / mul(total[s], total[t])
					}
					// The edge's ends in either order: A at s and B at t, or B at s and A at t,
					// each a directed count from A to B.
					out[a*K+b] += mul(w, q)
					out[b*K+a] += mul(w, q)
				}
			}
		}
	}
	return out
}
