package embed

import (
	"context"
	"errors"
	"runtime"
	"sync"

	"situcraft/internal/graph"
)

// QualitySeed seeds the search for the wider neighborhoods when it is not exact.
const QualitySeed = 20250

// Quality is how faithfully a map keeps the neighborhoods of the space it came from.
type Quality struct {
	// K: the neighborhood judged; KWide: the wider one a neighbor may fall in and still count.
	K, KWide int
	// Trustworthiness T(K) (Venna & Kaski 2001), exact over Queries cells evenly spaced through
	// the cells (every n/Queries-th), against all cells.
	Trustworthiness float64
	Queries         int
	// Precision and Recall, the means over the cells: the share of a cell's K nearest on the map
	// among its KWide nearest in the space, and of its K nearest in the space among its KWide
	// nearest on the map (Venna et al. 2010).
	Precision, Recall float64
	// Score: each cell's (precision + recall)/2, averaged over it and its K nearest on the map: how
	// faithfully the map keeps that region's neighborhoods, to color the map by.
	Score []float32
	// Search says how the space's wider neighborhoods were found when not exactly (more than
	// graph.ExactLimit cells).
	Search *graph.Approximate
}

// Assess judges the map low (n × 2) against the space high (n × dimHigh), with K 15 and KWide 75.
// Above graph.ExactLimit cells the space's wider neighborhoods are found by NN-Descent (sample
// rate 0.5), starting from start (the clustering's neighbors) when given.
func Assess(ctx context.Context, high []float64, dimHigh int, low []float64, n, workers int, start ...*graph.Neighbors) (*Quality, error) {
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	k := min(15, (n-1)/2)
	if k < 1 {
		return nil, errors.New("too few cells to judge a map")
	}
	wide := min(5*k, n-1)
	var from *graph.Neighbors
	if len(start) > 0 {
		from = start[0]
	}
	hi, search, err := graph.Nearest(ctx, high, n, dimHigh, wide, QualitySeed, 0.5, from, workers, nil)
	if err != nil {
		return nil, err
	}
	lo, err := graph.KNN2D(ctx, low, n, wide, workers)
	if err != nil {
		return nil, err
	}
	q := &Quality{K: k, KWide: wide, Score: make([]float32, n), Search: search}
	raw := make([]float64, n)
	markH := make([]int32, n)
	markL := make([]int32, n)
	for i := range markH {
		markH[i], markL[i] = -1, -1
	}
	var ps, rs float64
	for i := 0; i < n; i++ {
		for t := 0; t < wide; t++ {
			markH[hi.Index[i*wide+t]] = int32(i)
			markL[lo.Index[i*wide+t]] = int32(i)
		}
		p, r := 0, 0
		for t := 0; t < k; t++ {
			if markH[lo.Index[i*wide+t]] == int32(i) {
				p++
			}
			if markL[hi.Index[i*wide+t]] == int32(i) {
				r++
			}
		}
		pf, rf := float64(p)/float64(k), float64(r)/float64(k)
		ps += pf
		rs += rf
		raw[i] = (pf + rf) / 2
	}
	q.Precision, q.Recall = ps/float64(n), rs/float64(n)
	for i := 0; i < n; i++ {
		s := raw[i]
		for t := 0; t < k; t++ {
			s += raw[lo.Index[i*wide+t]]
		}
		q.Score[i] = float32(s / float64(k+1))
	}
	q.Trustworthiness, q.Queries, err = trustworthiness(ctx, high, dimHigh, lo, n, k, workers)
	return q, err
}

// trustworthiness is T(k) = 1 − 2/(Q·k·(2n − 3k − 1)) Σ_i Σ_{j ∈ U_k(i)} (r(i, j) − k) over Q query
// cells, U_k(i) the map's k nearest that are not among the space's k nearest and r their rank in the
// space (from 1; equal distances ranked by the cells' order). Over every cell it is
// sklearn.manifold.trustworthiness's value; over evenly spaced queries, an estimate of it.
func trustworthiness(ctx context.Context, high []float64, dim int, lo *graph.Neighbors, n, k, workers int) (float64, int, error) {
	queries := min(n, 2000)
	penalty := make([]int64, queries)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed error
	chunk := (queries + workers - 1) / workers
	for start := 0; start < queries; start += chunk {
		wg.Add(1)
		go func(a, b int) {
			defer wg.Done()
			dist := make([]float64, n)
			for qi := a; qi < b; qi++ {
				if qi%64 == 0 && ctx.Err() != nil {
					mu.Lock()
					failed = ctx.Err()
					mu.Unlock()
					return
				}
				i := qi * n / queries
				xi := high[i*dim : i*dim+dim]
				for j := 0; j < n; j++ {
					d := 0.0
					xj := high[j*dim : j*dim+dim]
					for t, v := range xi {
						diff := v - xj[t]
						d += mul(diff, diff)
					}
					dist[j] = d
				}
				rank := func(j int) int64 {
					r := int64(1)
					dj := dist[j]
					for l := 0; l < n; l++ {
						if l == i || l == j {
							continue
						}
						if dist[l] < dj || dist[l] == dj && l < j {
							r++
						}
					}
					return r
				}
				var sum int64
				for t := 0; t < k; t++ {
					j := int(lo.Index[i*lo.K+t])
					if r := rank(j); r > int64(k) {
						sum += r - int64(k)
					}
				}
				penalty[qi] = sum
			}
		}(start, min(queries, start+chunk))
	}
	wg.Wait()
	if failed != nil {
		return 0, 0, failed
	}
	var total int64
	for _, p := range penalty {
		total += p
	}
	scale := 2 / (float64(queries) * float64(k) * float64(2*n-3*k-1))
	return 1 - mul(scale, float64(total)), queries, nil
}
