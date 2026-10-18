package spatial

// Spatially variable genes: each gene's Moran's I over a graph of the cells (Moran PAP. Notes on
// continuous stochastic phenomena. Biometrika 1950;37:17–23), I = (n / S0) Σᵢⱼ wᵢⱼ zᵢ zⱼ / Σᵢ zᵢ², z
// the cells' values less their mean, the weights the graph's edges (binary, or each cell's
// neighbors weighing 1 / its neighbors' number, row-standardized; a cell without a neighbor
// weighing nothing). Under the null of no spatial pattern its expectation is −1 / (n − 1), and its
// variance, with S1 = ½ Σᵢⱼ (wᵢⱼ + wⱼᵢ)² and S2 = Σᵢ (wᵢ. + w.ᵢ)², is under normality
// (n² S1 − n S2 + 3 S0²) / (S0² (n² − 1)) − E², and under randomization (the values dealt again
// to the cells: their kurtosis b2 = n Σ z⁴ / (Σ z²)²) [n ((n² − 3n + 3) S1 − n S2 + 3 S0²) −
// b2 ((n² − n) S1 − 2n S2 + 6 S0²)] / ((n − 1)(n − 2)(n − 3) S0²) − E² (Cliff AD, Ord JK. Spatial
// Processes: Models and Applications. Pion, 1981). The test is randomization's z, two-sided; with
// permutations, the values dealt again (each permutation one shuffle of the cells, every gene's
// values moved alike), the share at least as far from the expectation ((1 + k) / (1 + n)); its
// p-values adjusted over the genes by Benjamini and Hochberg's procedure. I is the effect's size:
// 1 for values alike across every edge, about 0 for none, negative where neighbors differ.
//
// Local Moran's I for the leading genes (Anselin L. Local indicators of spatial association — LISA.
// Geogr Anal 1995;27:93–115): each cell's Iᵢ = zᵢ Σⱼ wᵢⱼ zⱼ / m2, m2 = Σ z² / n, with a
// conditional permutation test (its neighbors' values drawn again from the other cells, its own
// kept; the share of draws at least as extreme in its direction), the cell then classed by its
// value and its neighbors' (high among high, low among low, or an outlier either way) where the
// p-value is below 0.05, unadjusted: these show where a pattern lies, they do not test it.

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"

	"situcraft/internal/dmath"
	"situcraft/internal/random"
	"situcraft/internal/segment"
	"situcraft/internal/stats"
)

// MoranParams are the weights ("row" or "binary"), the permutations (0 for the analytic test
// alone) and seed, and the leading genes whose local Moran's I is found, with their permutations.
type MoranParams struct {
	Weights           string `json:"weights"`
	Permutations      int    `json:"permutations"`
	Seed              uint64 `json:"seed"`
	Local             int    `json:"local"`
	LocalPermutations int    `json:"localPermutations"`
}

// DefaultMoran are row-standardized weights, 199 permutations, and the 10 leading genes' local
// Moran's I from 199 permutations each.
func DefaultMoran() MoranParams {
	return MoranParams{Weights: "row", Permutations: 199, Seed: 20250, Local: 10, LocalPermutations: 199}
}

// Check says what is wrong with the parameters.
func (p *MoranParams) Check() error {
	switch {
	case p.Weights != "row" && p.Weights != "binary":
		return errors.New("Moran's I's weights are row (standardized) or binary")
	case p.Permutations != 0 && (p.Permutations < 19 || p.Permutations > 9999):
		return errors.New("Moran's I's permutations are 0 (the analytic test) or 19 to 9,999")
	case p.Local < 0 || p.Local > 50:
		return errors.New("local Moran's I is of 0 to 50 genes")
	case p.Local > 0 && (p.LocalPermutations < 19 || p.LocalPermutations > 9999):
		return errors.New("local Moran's I's permutations are 19 to 9,999")
	}
	return nil
}

// Local cell classes.
const (
	LocalNone int8 = iota
	LocalHighHigh
	LocalLowLow
	LocalHighLow
	LocalLowHigh
)

// LocalMoran is one gene's local Moran's I: each cell's value, its p-value and its class.
type LocalMoran struct {
	Gene  int
	I, P  []float32
	Class []int8
}

// Moran is spatially variable genes: by gene, I, its variances, randomization's z and p, the
// permutations' mean and deviation and p, the adjusted p (of the permutations when run), the mean
// value and the cells expressing it; and the weights' sums, and the leading genes' local I.
type Moran struct {
	Cells, Genes, Islands  int
	S0, S1, S2, Expected   float64
	I, VarNorm, VarRand    []float64
	Z, P                   []float64
	PermMean, PermSD, Perm []float64
	PAdj                   []float64
	Mean                   []float64
	Detected               []int
	Local                  []LocalMoran
}

// NormalizedExpression is cells' log-normalized expression as SituCraft's clustering normalizes it
// (each cell's counts scaled to a target total, the median of the cells with a gene transcript,
// then log1p), gene-major (genes × cells), zeros for a cell without a transcript; with the target
// and the cells without one.
func NormalizedExpression(c *segment.Counts, rows []uint32) (values []float32, target float64, empty int, err error) {
	n, G := len(rows), len(c.Genes)
	if G == 0 {
		return nil, 0, 0, errors.New("spatial: the segmentation's counts have no gene")
	}
	totals := make([]uint32, n)
	var some []uint32
	for i, r := range rows {
		if int(r)+1 >= len(c.Offsets) {
			return nil, 0, 0, errors.New("spatial: a cell beyond the segmentation's counts")
		}
		for k := c.Offsets[r]; k < c.Offsets[r+1]; k++ {
			totals[i] += c.Count[k]
		}
		if totals[i] > 0 {
			some = append(some, totals[i])
		} else {
			empty++
		}
	}
	if len(some) == 0 {
		return nil, 0, 0, errors.New("spatial: no cell has a gene transcript")
	}
	sort.Slice(some, func(a, b int) bool { return some[a] < some[b] })
	if m := len(some); m%2 == 1 {
		target = float64(some[m/2])
	} else {
		target = (float64(some[m/2-1]) + float64(some[m/2])) / 2
	}
	values = make([]float32, G*n)
	for i, r := range rows {
		if totals[i] == 0 {
			continue
		}
		scale := float64(totals[i]) / target
		for k := c.Offsets[r]; k < c.Offsets[r+1]; k++ {
			values[int(c.Gene[k])*n+i] = float32(dmath.Log(1 + float64(c.Count[k])/scale))
		}
	}
	return values, target, empty, nil
}

// MoransI finds each gene's Moran's I over a graph, values gene-major (genes × the graph's cells).
func MoransI(ctx context.Context, g *Graph, values []float32, genes int, p MoranParams, workers int, progress func(float64)) (*Moran, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	n := len(g.Rows)
	if n < 4 || genes < 1 || len(values) != genes*n {
		return nil, errors.New("spatial: Moran's I needs four cells or more and each gene's value in each")
	}
	if workers < 1 {
		workers = 1
	}
	if progress == nil {
		progress = func(float64) {}
	}
	// The weights: each cell's neighbors' weight (row-standardized: 1 / its neighbors' number).
	w := make([]float64, n)
	m := &Moran{Cells: n, Genes: genes}
	for i := 0; i < n; i++ {
		d := g.Offsets[i+1] - g.Offsets[i]
		switch {
		case d == 0:
			m.Islands++
		case p.Weights == "row":
			w[i] = 1 / float64(d)
		default:
			w[i] = 1
		}
	}
	// S0, S1 and S2 (the graph is symmetric: wⱼᵢ is j's weight where i is j's neighbor).
	rowSum := make([]float64, n)
	colSum := make([]float64, n)
	for i := 0; i < n; i++ {
		for k := g.Offsets[i]; k < g.Offsets[i+1]; k++ {
			j := g.Neighbors[k]
			m.S0 += w[i]
			rowSum[i] += w[i]
			colSum[j] += w[i]
			s := w[i] + w[j]
			m.S1 += mul(s, s)
		}
	}
	m.S1 /= 2
	for i := 0; i < n; i++ {
		s := rowSum[i] + colSum[i]
		m.S2 += mul(s, s)
	}
	nf := float64(n)
	m.Expected = -1 / (nf - 1)
	E2 := mul(m.Expected, m.Expected)
	S02 := mul(m.S0, m.S0)
	varNorm := (mul(mul(nf, nf), m.S1)-mul(nf, m.S2)+3*S02)/(mul(S02, mul(nf, nf)-1)) - E2
	// lag answers Σⱼ wᵢⱼ zⱼ for each cell, into out.
	lag := func(z, out []float64) {
		for i := 0; i < n; i++ {
			s := 0.0
			for k := g.Offsets[i]; k < g.Offsets[i+1]; k++ {
				s += z[g.Neighbors[k]]
			}
			out[i] = mul(w[i], s)
		}
	}
	nan := math.NaN()
	G := genes
	m.I, m.VarNorm, m.VarRand, m.Z, m.P = make([]float64, G), make([]float64, G), make([]float64, G), make([]float64, G), make([]float64, G)
	m.PermMean, m.PermSD, m.Perm, m.PAdj = make([]float64, G), make([]float64, G), make([]float64, G), make([]float64, G)
	m.Mean, m.Detected = make([]float64, G), make([]int, G)
	// Each gene's centered values, sum of squares and observed I.
	z := make([]float64, G*n)
	m2 := make([]float64, G)
	errs := make([]error, workers)
	parallelFor(G, workers, func(wk, a, b int) {
		out := make([]float64, n)
		for gene := a; gene < b; gene++ {
			if ctx.Err() != nil {
				errs[wk] = ctx.Err()
				return
			}
			v := values[gene*n : (gene+1)*n]
			zg := z[gene*n : (gene+1)*n]
			sum := 0.0
			for i, x := range v {
				sum += float64(x)
				if x > 0 {
					m.Detected[gene]++
				}
				zg[i] = float64(x)
			}
			mean := sum / nf
			m.Mean[gene] = mean
			s2, s4 := 0.0, 0.0
			for i := range zg {
				zg[i] -= mean
				q := mul(zg[i], zg[i])
				s2 += q
				s4 += mul(q, q)
			}
			m2[gene] = s2
			if !(s2 > 0) {
				m.I[gene], m.VarNorm[gene], m.VarRand[gene], m.Z[gene], m.P[gene] = nan, nan, nan, nan, nan
				continue
			}
			lag(zg, out)
			cross := 0.0
			for i := range zg {
				cross += mul(zg[i], out[i])
			}
			m.I[gene] = nf / m.S0 * cross / s2
			b2 := nf * s4 / mul(s2, s2)
			num := nf*((mul(nf, nf)-3*nf+3)*m.S1-mul(nf, m.S2)+3*S02) - b2*((mul(nf, nf)-nf)*m.S1-2*mul(nf, m.S2)+6*S02)
			m.VarNorm[gene] = varNorm
			m.VarRand[gene] = num/((nf-1)*(nf-2)*(nf-3)*S02) - E2
			if m.VarRand[gene] > 0 {
				m.Z[gene] = (m.I[gene] - m.Expected) / math.Sqrt(m.VarRand[gene])
				m.P[gene] = 2 * stats.NormalCDF(-math.Abs(m.Z[gene]))
			} else {
				m.Z[gene], m.P[gene] = nan, nan
			}
		}
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	progress(0.1)
	// Permutations: each one deals the cells' values again, every gene's alike.
	P := p.Permutations
	if P > 0 {
		var mu sync.Mutex
		done := 0
		// Tallies by block of permutations, summed in the blocks' order after: the same on any
		// number of workers.
		const block = 50
		blocks := (P + block - 1) / block
		bSum := make([][]float64, blocks)
		bSq := make([][]float64, blocks)
		bFar := make([][]int, blocks)
		parallelFor(blocks, workers, func(wk, a, b int) {
			perm := make([]int32, n)
			zp := make([]float64, n)
			out := make([]float64, n)
			for blk := a; blk < b; blk++ {
				s, q, f := make([]float64, G), make([]float64, G), make([]int, G)
				for s0 := blk * block; s0 < min(P, (blk+1)*block); s0++ {
					if ctx.Err() != nil {
						errs[wk] = ctx.Err()
						return
					}
					st := random.New(p.Seed, uint64(s0))
					for i := range perm {
						perm[i] = int32(i)
					}
					for k := n - 1; k > 0; k-- {
						j := st.IntN(k + 1)
						perm[k], perm[j] = perm[j], perm[k]
					}
					for gene := 0; gene < G; gene++ {
						if !(m2[gene] > 0) {
							continue
						}
						zg := z[gene*n : (gene+1)*n]
						for i := range zp {
							zp[i] = zg[perm[i]]
						}
						lag(zp, out)
						cross := 0.0
						for i := range zp {
							cross += mul(zp[i], out[i])
						}
						I := nf / m.S0 * cross / m2[gene]
						s[gene] += I
						q[gene] += mul(I, I)
						dev := math.Abs(m.I[gene] - m.Expected)
						if math.Abs(I-m.Expected) >= dev-1e-12*math.Max(1, dev) {
							f[gene]++
						}
					}
					mu.Lock()
					done++
					progress(0.1 + 0.6*float64(done)/float64(P))
					mu.Unlock()
				}
				bSum[blk], bSq[blk], bFar[blk] = s, q, f
			}
		})
		for _, err := range errs {
			if err != nil {
				return nil, err
			}
		}
		for gene := 0; gene < G; gene++ {
			if !(m2[gene] > 0) {
				m.PermMean[gene], m.PermSD[gene], m.Perm[gene] = nan, nan, nan
				continue
			}
			s, q, f := 0.0, 0.0, 0
			for blk := 0; blk < blocks; blk++ {
				s += bSum[blk][gene]
				q += bSq[blk][gene]
				f += bFar[blk][gene]
			}
			mean := s / float64(P)
			m.PermMean[gene] = mean
			m.PermSD[gene] = math.Sqrt(math.Max(0, (q-float64(P)*mul(mean, mean))/float64(P-1)))
			m.Perm[gene] = float64(1+f) / float64(1+P)
		}
	} else {
		for gene := 0; gene < G; gene++ {
			m.PermMean[gene], m.PermSD[gene], m.Perm[gene] = nan, nan, nan
		}
	}
	// Benjamini–Hochberg over the genes tested: of the permutations' p-values when run.
	var ps []float64
	var tested []int
	for gene := 0; gene < G; gene++ {
		pv := m.P[gene]
		if P > 0 {
			pv = m.Perm[gene]
		}
		m.PAdj[gene] = nan
		if !math.IsNaN(pv) {
			ps, tested = append(ps, pv), append(tested, gene)
		}
	}
	adj := make([]float64, len(ps))
	stats.BenjaminiHochberg(ps, adj)
	for k, gene := range tested {
		m.PAdj[gene] = adj[k]
	}
	progress(0.75)
	// Local Moran's I of the leading genes: the significant with the greatest I, then the rest by I.
	if p.Local > 0 {
		order := make([]int, 0, G)
		for gene := 0; gene < G; gene++ {
			if !math.IsNaN(m.I[gene]) {
				order = append(order, gene)
			}
		}
		sort.SliceStable(order, func(a, b int) bool {
			sa, sb := m.PAdj[order[a]] < 0.05, m.PAdj[order[b]] < 0.05
			if sa != sb {
				return sa
			}
			return m.I[order[a]] > m.I[order[b]]
		})
		for k, gene := range order[:min(p.Local, len(order))] {
			lm, err := localMoran(ctx, g, w, z[gene*n:(gene+1)*n], m2[gene], p.LocalPermutations, p.Seed, workers)
			if err != nil {
				return nil, err
			}
			lm.Gene = gene
			m.Local = append(m.Local, *lm)
			progress(0.75 + 0.25*float64(k+1)/float64(p.Local))
		}
	}
	return m, nil
}

// localMoran is one gene's local Moran's I with its conditional permutations: each cell's
// neighbors' values drawn again from the other cells, without replacement, from the cell's own
// seeded stream (the same draws for every gene).
func localMoran(ctx context.Context, g *Graph, w []float64, z []float64, m2 float64, P int, seed uint64, workers int) (*LocalMoran, error) {
	n := len(z)
	scale := float64(n) / m2
	lm := &LocalMoran{I: make([]float32, n), P: make([]float32, n), Class: make([]int8, n)}
	errs := make([]error, workers)
	parallelFor(n, workers, func(wk, a, b int) {
		var picked []int32
		for i := a; i < b; i++ {
			if (i-a)%512 == 0 && ctx.Err() != nil {
				errs[wk] = ctx.Err()
				return
			}
			d := int(g.Offsets[i+1] - g.Offsets[i])
			s := 0.0
			for k := g.Offsets[i]; k < g.Offsets[i+1]; k++ {
				s += z[g.Neighbors[k]]
			}
			lagged := mul(w[i], s)
			obs := mul(mul(z[i], lagged), scale)
			lm.I[i] = float32(obs)
			if d == 0 {
				lm.P[i] = float32(math.NaN())
				continue
			}
			// The draws: d distinct other cells, P times.
			st := random.New(seed^0x9e3779b97f4a7c15, uint64(i))
			draws := make([]float64, P)
			for r := 0; r < P; r++ {
				picked = picked[:0]
				sum := 0.0
				for len(picked) < d {
					j := int32(st.IntN(n))
					if int(j) == i {
						continue
					}
					seen := false
					for _, q := range picked {
						if q == j {
							seen = true
							break
						}
					}
					if seen {
						continue
					}
					picked = append(picked, j)
					sum += z[j]
				}
				draws[r] = mul(mul(z[i], mul(w[i], sum)), scale)
			}
			// At least as extreme in the observed value's direction (from the draws' median).
			sorted := append([]float64(nil), draws...)
			sort.Float64s(sorted)
			median := sorted[P/2]
			k := 0
			for _, v := range draws {
				if obs >= median && v >= obs || obs < median && v <= obs {
					k++
				}
			}
			lm.P[i] = float32(float64(k+1) / float64(P+1))
			if lm.P[i] < 0.05 {
				switch {
				case z[i] > 0 && lagged > 0:
					lm.Class[i] = LocalHighHigh
				case z[i] < 0 && lagged < 0:
					lm.Class[i] = LocalLowLow
				case z[i] > 0:
					lm.Class[i] = LocalHighLow
				case z[i] < 0:
					lm.Class[i] = LocalLowHigh
				}
			}
		}
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return lm, nil
}
