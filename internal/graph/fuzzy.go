package graph

import (
	"errors"
	"math"
	"sort"

	"situcraft/internal/dmath"
)

var sqrt = math.Sqrt

// CSR is an undirected weighted graph in compressed sparse rows: each edge in the rows of both its
// ends, targets in increasing order, no self-loops.
type CSR struct {
	N       int
	Offsets []int // N+1
	Targets []int32
	Weights []float64
}

// Edges is the number of undirected edges.
func (g *CSR) Edges() int { return len(g.Targets) / 2 }

// umap-learn's constants (umap/umap_.py, smooth_knn_dist).
const (
	smoothKTolerance = 1e-5
	minKDistScale    = 1e-3
)

// Fuzzy is UMAP's fuzzy simplicial set of a neighbor table, as umap-learn's fuzzy_simplicial_set
// builds it with its defaults (one nearest neighbor of local connectivity, bandwidth 1, the fuzzy
// union of the two directions) and scanpy clusters on: each point counts itself as its nearest
// neighbor (umap-learn's neighbor 0), so a table of K others gives umap-learn's K+1 neighbors.
// For point i with distances d_j to its neighbors, ρ_i is the nearest nonzero distance and σ_i is
// found by bisection so that Σ_j exp(−max(0, d_j − ρ_i)/σ_i) = log₂(K+1); the weight of i → j is
// exp(−(d_j − ρ_i)/σ_i), 1 within ρ_i, and the undirected weight of {i, j} is a + b − ab of the
// two directions' weights.
func Fuzzy(nb *Neighbors) (*CSR, error) {
	n, k := nb.N, nb.K
	if n < 2 || k < 1 || len(nb.Index) != n*k || len(nb.Distances) != n*k {
		return nil, errors.New("graph: the neighbor table is not N × K")
	}
	kk := k + 1
	target := dmath.Log(float64(kk)) / math.Ln2
	// The mean of every distance in umap-learn's table (its zeros included).
	meanAll := 0.0
	for _, d := range nb.Distances {
		meanAll += d
	}
	meanAll /= float64(n * kk)
	rho := make([]float64, n)
	sigma := make([]float64, n)
	for i := 0; i < n; i++ {
		row := nb.Distances[i*k : i*k+k]
		nonzero, sum := 0, 0.0
		first := -1.0
		for _, d := range row {
			if d > 0 {
				if first < 0 {
					first = d
				}
				nonzero++
			}
			sum += d
		}
		if nonzero >= 1 {
			rho[i] = first
		}
		lo, hi, mid := 0.0, math.Inf(1), 1.0
		for it := 0; it < 64; it++ {
			psum := 0.0
			for _, d := range row {
				if r := d - rho[i]; r > 0 {
					psum += dmath.Exp(-(r / mid))
				} else {
					psum++
				}
			}
			if math.Abs(psum-target) < smoothKTolerance {
				break
			}
			if psum > target {
				hi = mid
				mid = (lo + hi) / 2
			} else {
				lo = mid
				if math.IsInf(hi, 1) {
					mid *= 2
				} else {
					mid = (lo + hi) / 2
				}
			}
		}
		sigma[i] = mid
		if rho[i] > 0 {
			if m := mul(minKDistScale, sum/float64(kk)); sigma[i] < m {
				sigma[i] = m
			}
		} else if m := mul(minKDistScale, meanAll); sigma[i] < m {
			sigma[i] = m
		}
	}
	// The directed weights, then their fuzzy union.
	type arc struct {
		to int32
		w  float64
	}
	out := make([][]arc, n)
	in := make([][]arc, n)
	for i := 0; i < n; i++ {
		for t := 0; t < k; t++ {
			j := nb.Index[i*k+t]
			if j < 0 || int(j) == i {
				continue
			}
			w := 1.0
			if r := nb.Distances[i*k+t] - rho[i]; r > 0 && sigma[i] != 0 {
				w = dmath.Exp(-(r / sigma[i]))
			}
			out[i] = append(out[i], arc{j, w})
			in[j] = append(in[j], arc{int32(i), w})
		}
	}
	g := &CSR{N: n, Offsets: make([]int, n+1)}
	weights := map[int32]float64{}
	var keys []int32
	for i := 0; i < n; i++ {
		clear(weights)
		keys = keys[:0]
		forward := map[int32]float64{}
		for _, a := range out[i] {
			forward[a.to] = a.w
			if _, ok := weights[a.to]; !ok {
				keys = append(keys, a.to)
			}
			weights[a.to] = 0
		}
		for _, a := range in[i] {
			if _, ok := weights[a.to]; !ok {
				keys = append(keys, a.to)
				weights[a.to] = 0
			}
		}
		backward := map[int32]float64{}
		for _, a := range in[i] {
			backward[a.to] = a.w
		}
		sort.Slice(keys, func(x, y int) bool { return keys[x] < keys[y] })
		for _, j := range keys {
			a, b := forward[j], backward[j]
			if w := a + b - mul(a, b); w != 0 {
				g.Targets = append(g.Targets, j)
				g.Weights = append(g.Weights, w)
			}
		}
		g.Offsets[i+1] = len(g.Targets)
	}
	return g, nil
}

// Components labels each node with its connected component (numbered in order of their lowest
// node) and answers the count.
func Components(g *CSR) ([]int32, int) {
	label := make([]int32, g.N)
	for i := range label {
		label[i] = -1
	}
	count := int32(0)
	stack := []int32{}
	for s := 0; s < g.N; s++ {
		if label[s] >= 0 {
			continue
		}
		label[s] = count
		stack = append(stack[:0], int32(s))
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for e := g.Offsets[v]; e < g.Offsets[v+1]; e++ {
				if u := g.Targets[e]; label[u] < 0 {
					label[u] = count
					stack = append(stack, u)
				}
			}
		}
		count++
	}
	return label, int(count)
}
