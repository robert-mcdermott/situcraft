package cluster

import (
	"context"
	"errors"
	"math"
	"runtime"
	"sort"
	"sync"

	"situcraft/internal/dmath"
	"situcraft/internal/linalg"
	"situcraft/internal/segment"
)

func mul(a, b float64) float64 { return float64(a * b) }

// matrix is the cells analyzed: their counts in compressed sparse rows (genes in increasing order
// within a row), each row's total, and then each count's log-normalized value.
type matrix struct {
	cells   []uint32 // indices in the segmentation
	genes   int
	offsets []int
	gene    []uint32
	count   []uint32
	total   []uint32
	value   []float64 // log1p(count / (total / target)), beside each count
}

// selectCells takes the cells keep says from a segmentation's counts. Cells without a gene
// transcript are left out whatever keep says, and counted.
func selectCells(c *segment.Counts, keep func(i int, total uint32) bool) (*matrix, int, error) {
	cells := len(c.Offsets) - 1
	if cells < 0 || int(c.Offsets[cells]) != len(c.Gene) || len(c.Gene) != len(c.Count) {
		return nil, 0, errors.New("the counts are not a cell-by-gene matrix")
	}
	m := &matrix{genes: len(c.Genes), offsets: []int{0}}
	empty := 0
	type pair struct{ g, n uint32 }
	var row []pair
	for i := 0; i < cells; i++ {
		total := uint32(0)
		row = row[:0]
		for e := c.Offsets[i]; e < c.Offsets[i+1]; e++ {
			if int(c.Gene[e]) >= m.genes {
				return nil, 0, errors.New("the counts name a gene the panel does not have")
			}
			if c.Count[e] > 0 {
				total += c.Count[e]
				row = append(row, pair{c.Gene[e], c.Count[e]})
			}
		}
		if !keep(i, total) {
			continue
		}
		if total == 0 {
			empty++
			continue
		}
		sort.Slice(row, func(a, b int) bool { return row[a].g < row[b].g })
		for _, p := range row {
			m.gene = append(m.gene, p.g)
			m.count = append(m.count, p.n)
		}
		m.cells = append(m.cells, uint32(i))
		m.total = append(m.total, total)
		m.offsets = append(m.offsets, len(m.gene))
	}
	return m, empty, nil
}

// median is the median of the totals (the mean of the middle two of an even count), as numpy's.
func median(totals []uint32) float64 {
	s := append([]uint32(nil), totals...)
	sort.Slice(s, func(a, b int) bool { return s[a] < s[b] })
	n := len(s)
	if n%2 == 1 {
		return float64(s[n/2])
	}
	return (float64(s[n/2-1]) + float64(s[n/2])) / 2
}

// normalize sets each count's value: the count scaled to the target total and log-transformed,
// log1p(x / (t/target)), as scanpy's normalize_total and log1p compute it (Wolf FA, Angerer P,
// Theis FJ. SCANPY: large-scale single-cell gene expression data analysis. Genome Biol 2018;19:15).
func (m *matrix) normalize(target float64) {
	m.value = make([]float64, len(m.count))
	for i := range m.cells {
		scale := float64(m.total[i]) / target
		for e := m.offsets[i]; e < m.offsets[i+1]; e++ {
			m.value[e] = dmath.Log(1 + float64(m.count[e])/scale)
		}
	}
}

// scaling is each gene's mean and standard deviation (n − 1 in the denominator, as scanpy's
// scale; 1 for a gene without variance) of the log-normalized values, zeros included.
func (m *matrix) scaling() (mean, sd []float64, detected []int) {
	n := float64(len(m.cells))
	mean = make([]float64, m.genes)
	sd = make([]float64, m.genes)
	detected = make([]int, m.genes)
	for e, g := range m.gene {
		mean[g] += m.value[e]
		detected[g]++
	}
	for g := range mean {
		mean[g] /= n
	}
	ss := make([]float64, m.genes)
	for e, g := range m.gene {
		d := m.value[e] - mean[g]
		ss[g] += mul(d, d)
	}
	for g := range ss {
		zeros := n - float64(detected[g])
		v := (ss[g] + mul(zeros, mul(mean[g], mean[g]))) / (n - 1)
		sd[g] = math.Sqrt(v)
		if sd[g] == 0 {
			sd[g] = 1
		}
	}
	return mean, sd, detected
}

func clip(x, at float64) float64 { return math.Max(-at, math.Min(at, x)) }

// pca is principal component analysis of the scaled values (each gene centered, divided by its
// standard deviation and clipped at ±clipAt), exact: the eigenvectors of the genes' covariance,
// computed from the sparse counts without forming the dense matrix. A cell's scaled values are a
// gene's value at zero, z0, plus a sparse difference d over the genes it expresses; with a = z0
// minus the scaled values' mean, the covariance is (Σᵢ dᵢdᵢᵀ − n·aaᵀ)/(n − 1), each entry of the
// sum accumulated over the cells in order (by the worker owning its row, so that the sum is the
// same for any number of workers). Components are signed so that each one's largest loading in
// magnitude is positive (scikit-learn's convention, which scanpy's PCA follows).
type pcaResult struct {
	components    int
	values        []float64 // the components' variances
	totalVariance float64
	loadings      []float64 // genes × components
	scores        []float64 // cells × components
}

func (m *matrix) pca(ctx context.Context, mean, sd []float64, clipAt float64, components, workers int, progress func(float64)) (*pcaResult, error) {
	n, G := len(m.cells), m.genes
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	z0 := make([]float64, G)
	for g := range z0 {
		z0[g] = clip(-mean[g]/sd[g], clipAt)
	}
	d := make([]float64, len(m.value))
	sumD := make([]float64, G)
	for e, g := range m.gene {
		d[e] = clip((m.value[e]-mean[g])/sd[g], clipAt) - z0[g]
		sumD[g] += d[e]
	}
	a := make([]float64, G)
	for g := range a {
		a[g] = -sumD[g] / float64(n)
	}
	// Σᵢ dᵢdᵢᵀ, lower triangle. Rows are dealt to workers in stripes; every entry is summed over
	// the cells in order whoever owns it.
	s := make([]float64, G*G)
	var wg sync.WaitGroup
	var cancelled error
	var mu sync.Mutex
	var rowsDone int
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < n; i++ {
				if i%4096 == 0 && ctx.Err() != nil {
					mu.Lock()
					cancelled = ctx.Err()
					mu.Unlock()
					return
				}
				lo, hi := m.offsets[i], m.offsets[i+1]
				for e := lo; e < hi; e++ {
					g := int(m.gene[e])
					if g%workers != w {
						continue
					}
					dg := d[e]
					row := s[g*G:]
					for f := lo; f <= e; f++ {
						row[m.gene[f]] += mul(dg, d[f])
					}
				}
				if w == 0 && progress != nil && i%8192 == 0 {
					mu.Lock()
					rowsDone = i
					mu.Unlock()
					progress(0.8 * float64(rowsDone) / float64(n))
				}
			}
		}(w)
	}
	wg.Wait()
	if cancelled != nil {
		return nil, cancelled
	}
	cov := make([]float64, G*G)
	nn := float64(n)
	total := 0.0
	for g := 0; g < G; g++ {
		for h := 0; h <= g; h++ {
			v := (s[g*G+h] - mul(nn, mul(a[g], a[h]))) / (nn - 1)
			cov[g*G+h], cov[h*G+g] = v, v
		}
		total += cov[g*G+g]
	}
	values, vectors, err := linalg.SymEigen(cov, G)
	if err != nil {
		return nil, err
	}
	if progress != nil {
		progress(0.9)
	}
	k := min(components, G-1, n-1)
	if k < 1 {
		return nil, errors.New("too few cells or genes for principal components")
	}
	r := &pcaResult{components: k, values: values[:k], totalVariance: total, loadings: make([]float64, G*k)}
	for c := 0; c < k; c++ {
		at, big := 0, -1.0
		for g := 0; g < G; g++ {
			if v := math.Abs(vectors[g*G+c]); v > big {
				at, big = g, v
			}
		}
		sign := 1.0
		if vectors[at*G+c] < 0 {
			sign = -1
		}
		for g := 0; g < G; g++ {
			r.loadings[g*k+c] = sign * vectors[g*G+c]
		}
	}
	// Scores: (a + dᵢ)·V.
	base := make([]float64, k)
	for g := 0; g < G; g++ {
		for c := 0; c < k; c++ {
			base[c] += mul(a[g], r.loadings[g*k+c])
		}
	}
	r.scores = make([]float64, n*k)
	parallel(n, workers, func(i int) {
		out := r.scores[i*k : i*k+k]
		copy(out, base)
		for e := m.offsets[i]; e < m.offsets[i+1]; e++ {
			l := r.loadings[int(m.gene[e])*k:]
			for c := 0; c < k; c++ {
				out[c] += mul(d[e], l[c])
			}
		}
	})
	return r, nil
}

// parallel runs f on 0…n−1 over workers, each index once (f writes only its own outputs).
func parallel(n, workers int, f func(i int)) {
	var wg sync.WaitGroup
	chunk := (n + workers - 1) / max(workers, 1)
	for lo := 0; lo < n; lo += max(chunk, 1) {
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			for i := lo; i < hi; i++ {
				f(i)
			}
		}(lo, min(n, lo+max(chunk, 1)))
	}
	wg.Wait()
}
