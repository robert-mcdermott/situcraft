package cluster

import (
	"math"
	"sort"

	"situcraft/internal/dmath"
	"situcraft/internal/stats"
)

// Markers are each cluster's statistics for every gene against the rest of the cells analyzed,
// from the log-normalized values: the Wilcoxon rank-sum test's z-score with ties corrected (Mann
// HB, Whitney DR. On a test of whether one of two random variables is stochastically larger than
// the other. Ann Math Stat 1947;18:50–60), its two-sided p-value from the normal approximation, the
// p-values adjusted over the genes by Benjamini and Hochberg's procedure (Benjamini Y, Hochberg Y.
// Controlling the false discovery rate. J R Stat Soc B 1995;57:289–300), the log₂ fold change of
// the mean expression, and the shares of cells expressing the gene in and out, as scanpy's
// rank_genes_groups (method "wilcoxon", tie_correct) computes them. Cells are not replicates: the
// p-values rank genes and are labeled exploratory.
type Markers struct {
	Clusters, Genes int
	// Each [cluster*Genes + gene].
	Score, LogFC, P, PAdj []float64
	PctIn, PctOut         []float64
}

// markers computes them for clusters numbered 0…count−1 of the cells of m.
func (m *matrix) markers(cluster []int32, count, workers int) *Markers {
	n, G := len(m.cells), m.genes
	mk := &Markers{Clusters: count, Genes: G}
	size := count * G
	mk.Score, mk.LogFC, mk.P, mk.PAdj = make([]float64, size), make([]float64, size), make([]float64, size), make([]float64, size)
	mk.PctIn, mk.PctOut = make([]float64, size), make([]float64, size)
	sizes := make([]int, count)
	for _, c := range cluster {
		sizes[c]++
	}
	// The values by gene: each gene's cells (in order) and values.
	start := make([]int, G+1)
	for _, g := range m.gene {
		start[g+1]++
	}
	for g := 0; g < G; g++ {
		start[g+1] += start[g]
	}
	cell := make([]int32, len(m.gene))
	value := make([]float64, len(m.gene))
	tally := make([]uint32, len(m.gene))
	cursor := append([]int(nil), start[:G]...)
	for i := 0; i < n; i++ {
		for e := m.offsets[i]; e < m.offsets[i+1]; e++ {
			g := m.gene[e]
			cell[cursor[g]] = int32(i)
			value[cursor[g]] = m.value[e]
			tally[cursor[g]] = m.count[e]
			cursor[g]++
		}
	}
	nf := float64(n)
	parallel(G, workers, func(g int) {
		cells, values, counts := cell[start[g]:start[g+1]], value[start[g]:start[g+1]], tally[start[g]:start[g+1]]
		nz := len(cells)
		zeros := n - nz
		// Ranks: the zeros share the first ranks; the values after them, ties averaged. A value is
		// increasing in the cell's count over its total, so the cells are ordered by that ratio,
		// exactly (c₁t₂ against c₂t₁ in integers): cells expressing a gene equally tie, which their
		// values, rounded, need not show.
		order := make([]int, nz)
		for i := range order {
			order[i] = i
		}
		ratio := func(a, b int) (uint64, uint64) {
			return uint64(counts[a]) * uint64(m.total[cells[b]]), uint64(counts[b]) * uint64(m.total[cells[a]])
		}
		sort.SliceStable(order, func(a, b int) bool {
			x, y := ratio(order[a], order[b])
			return x < y
		})
		same := func(a, b int) bool {
			x, y := ratio(a, b)
			return x == y
		}
		rank := make([]float64, nz)
		ties := 0.0
		if zeros > 1 {
			z := float64(zeros)
			ties += mul(mul(z, z), z) - z
		}
		for a := 0; a < nz; {
			b := a
			for b+1 < nz && same(order[b+1], order[a]) {
				b++
			}
			r := float64(zeros) + float64(a+b+2)/2
			for t := a; t <= b; t++ {
				rank[order[t]] = r
			}
			if t := float64(b - a + 1); t > 1 {
				ties += mul(mul(t, t), t) - t
			}
			a = b + 1
		}
		tie := 1 - ties/(mul(mul(nf, nf), nf)-nf)
		zeroRank := float64(zeros+1) / 2
		rankSum := make([]float64, count)
		sum := make([]float64, count)
		expressing := make([]int, count)
		for t, i := range cells {
			c := cluster[i]
			rankSum[c] += rank[t]
			sum[c] += values[t]
			expressing[c]++
		}
		for c := 0; c < count; c++ {
			at := c*G + g
			n1 := float64(sizes[c])
			n2 := nf - n1
			if sizes[c] == 0 || n2 == 0 {
				// A cluster another tool left without a cell here, or holding every cell.
				mk.P[at] = 1
				continue
			}
			rs := rankSum[c] + mul(float64(sizes[c]-expressing[c]), zeroRank)
			sd := math.Sqrt(mul(mul(mul(tie, n1), n2), nf+1) / 12)
			z := 0.0
			if sd > 0 {
				z = (rs - mul(n1, nf+1)/2) / sd
			}
			mk.Score[at] = z
			mk.P[at] = dmath.Erfc(math.Abs(z) / math.Sqrt2)
			rest := 0.0
			for o := 0; o < count; o++ {
				if o != c {
					rest += sum[o]
				}
			}
			in, out := sum[c]/n1, rest/n2
			mk.LogFC[at] = dmath.Log((expm1(in)+1e-9)/(expm1(out)+1e-9)) / math.Ln2
			mk.PctIn[at] = float64(expressing[c]) / n1
			mk.PctOut[at] = float64(nz-expressing[c]) / n2
		}
	})
	for c := 0; c < count; c++ {
		bh(mk.P[c*G:(c+1)*G], mk.PAdj[c*G:(c+1)*G])
	}
	return mk
}

func expm1(x float64) float64 { return dmath.Exp(x) - 1 }

// bh adjusts p-values by Benjamini and Hochberg's procedure (internal/stats).
func bh(p, adj []float64) { stats.BenjaminiHochberg(p, adj) }
