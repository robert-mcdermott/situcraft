package graph

import (
	"context"
	"errors"
	"math"
	"runtime"
	"sync"
)

// KNN2D finds each of n points' (xy, n × 2) k nearest other points, exactly, as KNN does (equal
// distances ordered by index) but by a grid: the points are bucketed in squares holding about
// four each, and each point's search widens ring by ring until the next ring lies farther than its
// k-th nearest so far.
func KNN2D(ctx context.Context, xy []float64, n, k, workers int) (*Neighbors, error) {
	if n < 2 || len(xy) < 2*n {
		return nil, errors.New("graph: the points do not fill an n × 2 matrix of two or more points")
	}
	if k < 1 || k > n-1 {
		return nil, errors.New("graph: too many neighbors for the points")
	}
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i := 0; i < n; i++ {
		x, y := xy[2*i], xy[2*i+1]
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			return nil, errors.New("graph: a point is not finite")
		}
		x0, y0, x1, y1 = math.Min(x0, x), math.Min(y0, y), math.Max(x1, x), math.Max(y1, y)
	}
	side := int(math.Max(1, math.Floor(math.Sqrt(float64(n)/4))))
	w := math.Max(x1-x0, y1-y0) / float64(side)
	if !(w > 0) {
		w = 1
	}
	cellOf := func(v, origin float64) int {
		c := int((v - origin) / w)
		if c >= side {
			c = side - 1
		}
		if c < 0 {
			c = 0
		}
		return c
	}
	start := make([]int, side*side+1)
	at := make([]int, n)
	for i := 0; i < n; i++ {
		at[i] = cellOf(xy[2*i+1], y0)*side + cellOf(xy[2*i], x0)
		start[at[i]+1]++
	}
	for c := 0; c < side*side; c++ {
		start[c+1] += start[c]
	}
	members := make([]int32, n)
	fill := append([]int(nil), start[:side*side]...)
	for i := 0; i < n; i++ {
		members[fill[at[i]]] = int32(i)
		fill[at[i]]++
	}
	nb := &Neighbors{N: n, K: k, Index: make([]int32, n*k), Distances: make([]float64, n*k)}
	var wg sync.WaitGroup
	chunk := (n + workers - 1) / workers
	var failed error
	var mu sync.Mutex
	for lo := 0; lo < n; lo += chunk {
		wg.Add(1)
		go func(a, b int) {
			defer wg.Done()
			heap := make([]entry, 0, k)
			for i := a; i < b; i++ {
				if i%4096 == 0 && ctx.Err() != nil {
					mu.Lock()
					failed = ctx.Err()
					mu.Unlock()
					return
				}
				heap = heap[:0]
				px, py := xy[2*i], xy[2*i+1]
				cx, cy := cellOf(px, x0), cellOf(py, y0)
				for ring := 0; ring < side; ring++ {
					// The ring's squares are at least (ring − 1)·w away; with the heap full and its
					// farthest nearer than that, no farther ring can hold a nearer point.
					if len(heap) == k && ring > 0 {
						near := float64(ring-1) * w
						if near > 0 && mul(near, near) > heap[0].d {
							break
						}
					}
					for gy := cy - ring; gy <= cy+ring; gy++ {
						if gy < 0 || gy >= side {
							continue
						}
						for gx := cx - ring; gx <= cx+ring; gx++ {
							if gx < 0 || gx >= side || (gy != cy-ring && gy != cy+ring && gx != cx-ring && gx != cx+ring) {
								continue
							}
							c := gy*side + gx
							for _, j := range members[start[c]:start[c+1]] {
								if int(j) == i {
									continue
								}
								dx, dy := px-xy[2*j], py-xy[2*j+1]
								heap = push(heap, k, entry{mul(dx, dx) + mul(dy, dy), j})
							}
						}
					}
				}
				sortEntries(heap)
				for t, e := range heap {
					nb.Index[i*k+t] = e.j
					nb.Distances[i*k+t] = math.Sqrt(e.d)
				}
			}
		}(lo, min(n, lo+chunk))
	}
	wg.Wait()
	if failed != nil {
		return nil, failed
	}
	return nb, nil
}
