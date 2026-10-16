// Package graph has the graphs SituCraft's analyses build on cells, the same on every processor
// for a seed (every product rounded on its own, sums in a fixed order, logarithms and
// exponentials from internal/dmath, one random stream per purpose):
//
//   - each point's nearest neighbors, found exactly (KNN);
//   - UMAP's fuzzy simplicial set of a neighbor table (McInnes L, Healy J, Melville J. UMAP:
//     Uniform Manifold Approximation and Projection for dimension reduction. arXiv:1802.03426,
//     2018), the weighted graph scanpy clusters on (Fuzzy);
//   - Leiden community detection (Traag VA, Waltman L, van Eck NJ. From Louvain to Leiden:
//     guaranteeing well-connected communities. Sci Rep 2019;9:5233.
//     doi:10.1038/s41598-019-41695-z) of modularity with a resolution (Reichardt J, Bornholdt S.
//     Statistical mechanics of community detection. Phys Rev E 2006;74:016110).
//
// Adapted from CytoWeave 0.8.1 web/lib/knn.js and web/lib/graph-cluster.js.
package graph

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
)

func mul(a, b float64) float64 { return float64(a * b) }

// Neighbors is a neighbor table: row i lists point i's K nearest other points, nearest first.
type Neighbors struct {
	N, K      int
	Index     []int32   // N × K
	Distances []float64 // N × K, Euclidean
}

// KNN finds each of the n points' (rows of x, n × dim, row-major) k nearest other points by
// Euclidean distance, exactly: every pair's squared distance is computed, summed over dimensions
// in order. Equal distances are ordered by the points' indices, so the table is the same for any
// number of workers. progress, if not nil, is told the share done.
func KNN(ctx context.Context, x []float64, n, dim, k, workers int, progress func(float64)) (*Neighbors, error) {
	if n < 2 || dim < 1 || len(x) < n*dim {
		return nil, errors.New("graph: the points do not fill an n × dim matrix of two or more points")
	}
	if k < 1 || k > n-1 {
		return nil, fmt.Errorf("graph: %d neighbors cannot be found among %d points", k, n)
	}
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	nb := &Neighbors{N: n, K: k, Index: make([]int32, n*k), Distances: make([]float64, n*k)}
	const block = 64
	blocks := (n + block - 1) / block
	var next, done int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	var failed error
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			heap := make([]entry, 0, k)
			for {
				mu.Lock()
				b := next
				next++
				stop := failed != nil
				mu.Unlock()
				if stop || int(b) >= blocks {
					return
				}
				if err := ctx.Err(); err != nil {
					mu.Lock()
					failed = err
					mu.Unlock()
					return
				}
				for i := int(b) * block; i < min(n, int(b+1)*block); i++ {
					heap = heap[:0]
					xi := x[i*dim : i*dim+dim]
					for j := 0; j < n; j++ {
						if j == i {
							continue
						}
						xj := x[j*dim : j*dim+dim]
						// A partial sum past the k-th nearest so far cannot come back below it.
						bound := -1.0
						if len(heap) == k {
							bound = heap[0].d
						}
						d := 0.0
						for t, v := range xi {
							diff := v - xj[t]
							d += mul(diff, diff)
							if bound >= 0 && d > bound {
								break
							}
						}
						if bound < 0 || d <= bound {
							heap = push(heap, k, entry{d, int32(j)})
						}
					}
					sortEntries(heap)
					for t, e := range heap {
						nb.Index[i*k+t] = e.j
						nb.Distances[i*k+t] = sqrt(e.d)
					}
				}
				mu.Lock()
				done++
				if progress != nil {
					progress(float64(done) / float64(blocks))
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if failed != nil {
		return nil, failed
	}
	return nb, nil
}

// entry is a candidate neighbor; one is nearer than another by distance, then by index.
type entry struct {
	d float64
	j int32
}

func (a entry) before(b entry) bool { return a.d < b.d || a.d == b.d && a.j < b.j }

// push keeps the k nearest entries in a max-heap (the farthest at the root).
func push(h []entry, k int, e entry) []entry {
	if len(h) < k {
		h = append(h, e)
		for c := len(h) - 1; c > 0; {
			p := (c - 1) / 2
			if !h[p].before(h[c]) {
				break
			}
			h[p], h[c] = h[c], h[p]
			c = p
		}
		return h
	}
	if !e.before(h[0]) {
		return h
	}
	h[0] = e
	for p := 0; ; {
		l, r, m := 2*p+1, 2*p+2, p
		if l < len(h) && h[m].before(h[l]) {
			m = l
		}
		if r < len(h) && h[m].before(h[r]) {
			m = r
		}
		if m == p {
			break
		}
		h[p], h[m] = h[m], h[p]
		p = m
	}
	return h
}

// sortEntries orders a small slice nearest first (insertion sort: k is a few dozen).
func sortEntries(h []entry) {
	for i := 1; i < len(h); i++ {
		for j := i; j > 0 && h[j].before(h[j-1]); j-- {
			h[j], h[j-1] = h[j-1], h[j]
		}
	}
}
