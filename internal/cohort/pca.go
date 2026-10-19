package cohort

// The samples' pseudobulk in principal components (wave 7, slice 5): how the samples differ in
// expression, shown before any comparison so that a batch, a group or an outlying sample can be
// seen. Each sample's transcripts of the genes every panel has, summed over its labels, become
// log2(1 + counts per million); the most variable genes are kept (as DESeq2's plotPCA keeps its
// ntop; Love, Huber and Anders 2014), centered, and decomposed. Nothing is corrected for.

import (
	"errors"
	"math"
	"sort"

	"situcraft/internal/dmath"
)

// PCA defaults: the most variable genes kept, and the components answered at most.
const (
	PCATop        = 500
	PCAComponents = 10
)

// PCA is the samples' principal components: the genes used (the most variable, in the document's
// order), each component's share of the variance, and each sample's scores.
type PCA struct {
	Normalization string      `json:"normalization"`
	Top           int         `json:"top"`
	Genes         []string    `json:"genes"`
	Variance      []float64   `json:"variance"`
	Scores        [][]float64 `json:"scores"`
}

// mul rounds a product, so that no compiler fuses it into an addition (the same answer on every
// processor).
func mul(a, b float64) float64 { return float64(a * b) }

// PseudobulkPCA finds the principal components of samples' counts (counts[s][g], transcripts of
// genes[g]): log2(1 + counts per million of the sample's total), the top most variable genes
// (ties to the first), centered; at most components components, and fewer than the samples.
func PseudobulkPCA(counts [][]float64, genes []string, top, components int) (*PCA, error) {
	S := len(counts)
	if S < 2 {
		return nil, errors.New("cohort: principal components need two samples")
	}
	G := len(genes)
	if G == 0 {
		return nil, errors.New("cohort: no gene every sample's panel has")
	}
	y := make([][]float64, S)
	for s := range counts {
		if len(counts[s]) != G {
			return nil, errors.New("cohort: a sample's counts are not of the genes")
		}
		var total float64
		for _, v := range counts[s] {
			total += v
		}
		if !(total > 0) {
			return nil, errors.New("cohort: a sample without transcripts of the genes every panel has")
		}
		y[s] = make([]float64, G)
		for g, v := range counts[s] {
			y[s][g] = dmath.Log(1+mul(v/total, 1e6)) / math.Ln2
		}
	}
	// Each gene's variance across the samples; the most variable kept, in the genes' order.
	mean := make([]float64, G)
	variance := make([]float64, G)
	for g := 0; g < G; g++ {
		var sum float64
		for s := 0; s < S; s++ {
			sum += y[s][g]
		}
		mean[g] = sum / float64(S)
		var ss float64
		for s := 0; s < S; s++ {
			d := y[s][g] - mean[g]
			ss += mul(d, d)
		}
		variance[g] = ss / float64(S)
	}
	rank := make([]int, G)
	for g := range rank {
		rank[g] = g
	}
	sort.SliceStable(rank, func(i, j int) bool { return variance[rank[i]] > variance[rank[j]] })
	if top > G {
		top = G
	}
	keep := append([]int(nil), rank[:top]...)
	sort.Ints(keep)
	// The centered matrix's Gram matrix, S × S.
	c := make([][]float64, S)
	for s := range c {
		c[s] = make([]float64, len(keep))
		for j, g := range keep {
			c[s][j] = y[s][g] - mean[g]
		}
	}
	gram := make([][]float64, S)
	var trace float64
	for a := 0; a < S; a++ {
		gram[a] = make([]float64, S)
		for b := 0; b <= a; b++ {
			var dot float64
			for j := range keep {
				dot += mul(c[a][j], c[b][j])
			}
			gram[a][b] = dot
			gram[b][a] = dot
		}
		trace += gram[a][a]
	}
	out := &PCA{Normalization: "log2(1 + counts per million), the genes every panel has", Top: top, Genes: make([]string, len(keep)), Variance: []float64{}, Scores: make([][]float64, S)}
	for j, g := range keep {
		out.Genes[j] = genes[g]
	}
	for s := range out.Scores {
		out.Scores[s] = []float64{}
	}
	if !(trace > 0) {
		return out, nil
	}
	values, vectors := jacobi(gram)
	K := components
	if K > S-1 {
		K = S - 1
	}
	for k := 0; k < K && k < len(values); k++ {
		if !(values[k] > 1e-12*trace) {
			break
		}
		root := math.Sqrt(values[k])
		// The sign that makes the sample farthest along the component positive (the first of equals).
		far := 0
		for s := 1; s < S; s++ {
			if math.Abs(vectors[s][k]) > math.Abs(vectors[far][k]) {
				far = s
			}
		}
		sign := 1.0
		if vectors[far][k] < 0 {
			sign = -1
		}
		for s := 0; s < S; s++ {
			out.Scores[s] = append(out.Scores[s], mul(mul(sign, vectors[s][k]), root))
		}
		out.Variance = append(out.Variance, values[k]/trace)
	}
	return out, nil
}

// jacobi finds a symmetric matrix's eigenvalues, greatest first, and its eigenvectors (column k of
// the answer is the kth's), by Jacobi's cyclic rotations (Golub and Van Loan, Matrix Computations,
// section 8.5): each off-diagonal entry rotated to zero in turn, sweep after sweep, until the
// off-diagonal part is negligible.
func jacobi(m [][]float64) ([]float64, [][]float64) {
	n := len(m)
	a := make([][]float64, n)
	v := make([][]float64, n)
	var norm float64
	for i := range a {
		a[i] = append([]float64(nil), m[i]...)
		v[i] = make([]float64, n)
		v[i][i] = 1
		for j := range a[i] {
			norm += mul(a[i][j], a[i][j])
		}
	}
	for sweep := 0; sweep < 100; sweep++ {
		var off float64
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				off += mul(a[i][j], a[i][j])
			}
		}
		if !(off > mul(1e-30, norm)) {
			break
		}
		for p := 0; p < n; p++ {
			for q := p + 1; q < n; q++ {
				if a[p][q] == 0 {
					continue
				}
				theta := (a[q][q] - a[p][p]) / mul(2, a[p][q])
				t := 1 / (math.Abs(theta) + math.Sqrt(mul(theta, theta)+1))
				if theta < 0 {
					t = -t
				}
				cs := 1 / math.Sqrt(mul(t, t)+1)
				sn := mul(t, cs)
				for k := 0; k < n; k++ {
					akp, akq := a[k][p], a[k][q]
					a[k][p] = mul(cs, akp) - mul(sn, akq)
					a[k][q] = mul(sn, akp) + mul(cs, akq)
				}
				for k := 0; k < n; k++ {
					apk, aqk := a[p][k], a[q][k]
					a[p][k] = mul(cs, apk) - mul(sn, aqk)
					a[q][k] = mul(sn, apk) + mul(cs, aqk)
				}
				for k := 0; k < n; k++ {
					vkp, vkq := v[k][p], v[k][q]
					v[k][p] = mul(cs, vkp) - mul(sn, vkq)
					v[k][q] = mul(sn, vkp) + mul(cs, vkq)
				}
			}
		}
	}
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return a[order[i]][order[i]] > a[order[j]][order[j]] })
	values := make([]float64, n)
	vectors := make([][]float64, n)
	for i := range vectors {
		vectors[i] = make([]float64, n)
	}
	for k, i := range order {
		values[k] = a[i][i]
		for r := 0; r < n; r++ {
			vectors[r][k] = v[r][i]
		}
	}
	return values, vectors
}

// SharedCounts are the samples' transcripts of the genes every panel has, summed over their labels
// (from the pseudobulk, over the document's genes).
func SharedCounts(d *Document, pseudobulk [][][]uint32, shared []string) [][]float64 {
	at := map[string]int{}
	for g, name := range d.Genes {
		at[name] = g
	}
	out := make([][]float64, len(pseudobulk))
	for s := range pseudobulk {
		out[s] = make([]float64, len(shared))
		for j, name := range shared {
			g, ok := at[name]
			if !ok {
				continue
			}
			for k := range pseudobulk[s] {
				if v := pseudobulk[s][k][g]; v != Missing {
					out[s][j] += float64(v)
				}
			}
		}
	}
	return out
}
