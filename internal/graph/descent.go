package graph

// Each point's nearest neighbors found approximately, for tables too large to search exactly, by
// NN-Descent (Dong W, Moses C, Li K. Efficient k-nearest neighbor graph construction for generic
// similarity measures. Proceedings of the 20th International Conference on World Wide Web
// 2011:577–586. doi:10.1145/1963405.1963487): a neighbor of a neighbor is likely a neighbor. From
// random neighbors, each round compares each point's sampled new and old neighbors, and its reverse
// neighbors, with one another, and keeps for every point the nearest it has met; the rounds stop
// when one changes fewer than Delta of the table's entries.
//
// SituCraft's arrangement, the same table for a seed on any number of workers and processors: each
// round's samples are drawn from each point's own stream; the comparisons are made a block of points
// at a time against the table as it stands, and the block's improvements are then applied in the
// points' order (a point keeps the k nearest by distance, then index, of all it has met, whatever
// the order it met them in). Distances are summed over dimensions in order, every product rounded.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"sync"

	"situcraft/internal/random"
)

// DescentParams are NN-Descent's: the sample rate Rho (of k), the share of entries a round must
// change to go on (Delta), the most rounds, the seed, and an optional table to start from (its
// first k neighbors, completed at random).
type DescentParams struct {
	Rho       float64
	Delta     float64
	MaxRounds int
	Seed      uint64
	Start     *Neighbors
}

// DefaultDescent are the paper's sample rate and stop (ρ 1, δ 0.001), at most 30 rounds.
func DefaultDescent(seed uint64) DescentParams {
	return DescentParams{Rho: 1, Delta: 0.001, MaxRounds: 30, Seed: seed}
}

// DescentStats say how the search went: its rounds and each round's entries changed.
type DescentStats struct {
	Rounds  int
	Changed []int
}

// descentBlock is the points compared before their improvements are applied: fixed, so that the
// table does not depend on the workers.
const descentBlock = 2048

// table is the neighbor table under construction: each point's k entries as a max-heap (the
// farthest, by squared distance then index, at the root), with whether each is new.
type table struct {
	n, k  int
	d     []float64
	j     []int32
	fresh []bool
}

func (t *table) before(i, a, b int) bool {
	da, db := t.d[i*t.k+a], t.d[i*t.k+b]
	return da < db || da == db && t.j[i*t.k+a] < t.j[i*t.k+b]
}

// worst is point i's farthest kept (its root).
func (t *table) worst(i int) (float64, int32) { return t.d[i*t.k], t.j[i*t.k] }

// offer keeps point j at squared distance d among point i's k nearest when it is nearer than the
// farthest and not there already; answers whether it was kept.
func (t *table) offer(i int, d float64, j int32) bool {
	base := i * t.k
	wd, wj := t.d[base], t.j[base]
	if !(d < wd || d == wd && j < wj) {
		return false
	}
	for s := base; s < base+t.k; s++ {
		if t.j[s] == j {
			return false
		}
	}
	t.d[base], t.j[base], t.fresh[base] = d, j, true
	for p := 0; ; {
		l, r, m := 2*p+1, 2*p+2, p
		if l < t.k && t.before(i, m, l) {
			m = l
		}
		if r < t.k && t.before(i, m, r) {
			m = r
		}
		if m == p {
			break
		}
		a, b := base+p, base+m
		t.d[a], t.d[b] = t.d[b], t.d[a]
		t.j[a], t.j[b] = t.j[b], t.j[a]
		t.fresh[a], t.fresh[b] = t.fresh[b], t.fresh[a]
		p = m
	}
	return true
}

// heapify orders point i's entries as a max-heap.
func (t *table) heapify(i int) {
	for c := t.k/2 - 1; c >= 0; c-- {
		for p := c; ; {
			l, r, m := 2*p+1, 2*p+2, p
			if l < t.k && t.before(i, m, l) {
				m = l
			}
			if r < t.k && t.before(i, m, r) {
				m = r
			}
			if m == p {
				break
			}
			a, b := i*t.k+p, i*t.k+m
			t.d[a], t.d[b] = t.d[b], t.d[a]
			t.j[a], t.j[b] = t.j[b], t.j[a]
			t.fresh[a], t.fresh[b] = t.fresh[b], t.fresh[a]
			p = m
		}
	}
}

func sqDist(x []float64, dim, a, b int) float64 {
	xa, xb := x[a*dim:a*dim+dim], x[b*dim:b*dim+dim]
	d := 0.0
	for t, v := range xa {
		diff := v - xb[t]
		d += mul(diff, diff)
	}
	return d
}

// forEach runs f on 0…n−1 over workers in contiguous ranges (f writes only what index i owns).
func forEach(n, workers int, f func(lo, hi int)) {
	chunk := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for lo := 0; lo < n; lo += chunk {
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			f(lo, hi)
		}(lo, min(n, lo+chunk))
	}
	wg.Wait()
}

// sample draws up to m of the items without replacement (a partial Fisher–Yates shuffle of a copy).
func sample(s *random.Stream, items []int32, m int, out []int32) []int32 {
	out = append(out[:0], items...)
	if len(out) <= m {
		return out
	}
	for a := 0; a < m; a++ {
		b := a + s.IntN(len(out)-a)
		out[a], out[b] = out[b], out[a]
	}
	return out[:m]
}

// update is an improvement found: point to may keep point from at squared distance d.
type update struct {
	to, from int32
	d        float64
}

// NNDescent finds each of the n points' (rows of x, n × dim, row-major) k nearest other points
// approximately. progress, if not nil, is told the share of the most rounds done.
func NNDescent(ctx context.Context, x []float64, n, dim, k int, p DescentParams, workers int, progress func(float64)) (*Neighbors, *DescentStats, error) {
	if n < 2 || dim < 1 || len(x) < n*dim {
		return nil, nil, errors.New("graph: the points do not fill an n × dim matrix of two or more points")
	}
	if k < 1 || k > n-1 {
		return nil, nil, fmt.Errorf("graph: %d neighbors cannot be found among %d points", k, n)
	}
	if !(p.Rho > 0 && p.Rho <= 1) || !(p.Delta >= 0) || p.MaxRounds < 1 {
		return nil, nil, errors.New("graph: NN-Descent's sample rate is in (0, 1], its stop at least 0, its rounds at least 1")
	}
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	t := &table{n: n, k: k, d: make([]float64, n*k), j: make([]int32, n*k), fresh: make([]bool, n*k)}
	// The start: the given table's first k neighbors, completed with random others.
	forEach(n, workers, func(lo, hi int) {
		for i := lo; i < hi; i++ {
			s := random.New(p.Seed, uint64(i))
			row := t.j[i*k : i*k+k]
			c := 0
			taken := func(j int32) bool { return j == int32(i) || slices.Contains(row[:c], j) }
			if p.Start != nil && i < p.Start.N {
				for _, j := range p.Start.Index[i*p.Start.K : i*p.Start.K+min(k, p.Start.K)] {
					if !taken(j) {
						row[c] = j
						c++
					}
				}
			}
			for c < k {
				if j := int32(s.IntN(n)); !taken(j) {
					row[c] = j
					c++
				}
			}
			for s := i * k; s < i*k+k; s++ {
				t.d[s] = sqDist(x, dim, i, int(t.j[s]))
				t.fresh[s] = true
			}
			t.heapify(i)
		}
	})
	m := max(1, int(math.Round(p.Rho*float64(k))))
	stats := &DescentStats{}
	newOf, oldOf := make([][]int32, n), make([][]int32, n)
	// Buffers kept across blocks and rounds: each worker's improvements, and the grouping's.
	found := make([][]update, workers)
	count, first := make([]int32, n), make([]int32, n)
	var touched []int32
	var grouped []update
	for round := 0; round < p.MaxRounds; round++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		// Each point's new neighbors sampled (and marked old), and its old ones.
		forEach(n, workers, func(lo, hi int) {
			var fresh []int32
			for i := lo; i < hi; i++ {
				s := random.New(p.Seed, uint64(round+1)<<40|uint64(i))
				fresh = fresh[:0]
				oldOf[i] = oldOf[i][:0]
				for e := i * k; e < i*k+k; e++ {
					if t.fresh[e] {
						fresh = append(fresh, t.j[e])
					} else {
						oldOf[i] = append(oldOf[i], t.j[e])
					}
				}
				newOf[i] = sample(s, fresh, m, newOf[i])
				for _, j := range newOf[i] {
					for e := i * k; e < i*k+k; e++ {
						if t.j[e] == j {
							t.fresh[e] = false
						}
					}
				}
			}
		})
		// The reverse lists, in the points' order, sampled and joined to each point's own.
		rnew, rnewAt := reverse(newOf, n)
		rold, roldAt := reverse(oldOf, n)
		forEach(n, workers, func(lo, hi int) {
			var buf []int32
			for i := lo; i < hi; i++ {
				s := random.New(p.Seed, uint64(round+1)<<40|1<<39|uint64(i))
				newOf[i] = union(newOf[i], sample(s, rnew[rnewAt[i]:rnewAt[i+1]], m, buf))
				oldOf[i] = union(oldOf[i], sample(s, rold[roldAt[i]:roldAt[i+1]], m, buf))
			}
		})
		// The local joins, a block of points at a time.
		changed := 0
		for lo := 0; lo < n; lo += descentBlock {
			hi := min(n, lo+descentBlock)
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			// Each worker's improvements, its points in order; the workers' ranges in order.
			ranges := (hi - lo + workers - 1) / workers
			parts := make([][]update, 0, workers)
			for a := 0; a < hi-lo; a += ranges {
				parts = append(parts, nil)
			}
			var wg sync.WaitGroup
			for w := range parts {
				wg.Add(1)
				go func(w int) {
					defer wg.Done()
					out := found[w][:0]
					for v := lo + w*ranges; v < min(hi, lo+(w+1)*ranges); v++ {
						nv, ov := newOf[v], oldOf[v]
						for a, u := range nv {
							for _, w := range nv[a+1:] {
								out = join(t, x, dim, u, w, out)
							}
							for _, w := range ov {
								out = join(t, x, dim, u, w, out)
							}
						}
					}
					parts[w] = out
				}(w)
			}
			wg.Wait()
			// Grouped by target point (a counting sort, keeping the order found), then applied.
			touched = touched[:0]
			total := 0
			for _, part := range parts {
				for _, u := range part {
					if count[u.to] == 0 {
						touched = append(touched, u.to)
					}
					count[u.to]++
				}
				total += len(part)
			}
			at := 0
			for _, v := range touched {
				first[v] = int32(at)
				at += int(count[v])
				count[v] = 0
			}
			if cap(grouped) < total {
				grouped = make([]update, total)
			}
			grouped = grouped[:total]
			for _, part := range parts {
				for _, u := range part {
					grouped[int(first[u.to])+int(count[u.to])] = u
					count[u.to]++
				}
			}
			counts := make([]int, len(touched))
			forEach(len(touched), workers, func(a, b int) {
				for q := a; q < b; q++ {
					v := touched[q]
					for _, u := range grouped[first[v] : int(first[v])+int(count[v])] {
						if t.offer(int(v), u.d, u.from) {
							counts[q]++
						}
					}
				}
			})
			for q, v := range touched {
				changed += counts[q]
				count[v] = 0
			}
			for w, part := range parts {
				found[w] = part
			}
		}
		stats.Rounds++
		stats.Changed = append(stats.Changed, changed)
		if progress != nil {
			progress(float64(round+1) / float64(p.MaxRounds))
		}
		if float64(changed) < p.Delta*float64(n*k) {
			break
		}
	}
	// The table, nearest first.
	nb := &Neighbors{N: n, K: k, Index: make([]int32, n*k), Distances: make([]float64, n*k)}
	forEach(n, workers, func(lo, hi int) {
		row := make([]entry, k)
		for i := lo; i < hi; i++ {
			for s := 0; s < k; s++ {
				row[s] = entry{t.d[i*k+s], t.j[i*k+s]}
			}
			sortEntries(row)
			for s, e := range row {
				nb.Index[i*k+s] = e.j
				nb.Distances[i*k+s] = sqrt(e.d)
			}
		}
	})
	return nb, stats, nil
}

// join compares points u and w, adding the improvement each would be to the other as the table
// stands.
func join(t *table, x []float64, dim int, u, w int32, out []update) []update {
	if u == w {
		return out
	}
	d := sqDist(x, dim, int(u), int(w))
	du, ju := t.worst(int(u))
	dw, jw := t.worst(int(w))
	if d < du || d == du && w < ju {
		out = append(out, update{u, w, d})
	}
	if d < dw || d == dw && u < jw {
		out = append(out, update{w, u, d})
	}
	return out
}

// reverse lists, for each point, the points listing it, in the points' order: all of them, and
// where each point's begin.
func reverse(lists [][]int32, n int) ([]int32, []int) {
	at := make([]int, n+1)
	for _, l := range lists {
		for _, j := range l {
			at[j+1]++
		}
	}
	for i := 0; i < n; i++ {
		at[i+1] += at[i]
	}
	out := make([]int32, at[n])
	fill := append([]int(nil), at[:n]...)
	for i, l := range lists {
		for _, j := range l {
			out[fill[j]] = int32(i)
			fill[j]++
		}
	}
	return out, at
}

// union appends to a the items of b it lacks.
func union(a, b []int32) []int32 {
	for _, x := range b {
		found := false
		for _, y := range a {
			if x == y {
				found = true
				break
			}
		}
		if !found {
			a = append(a, x)
		}
	}
	return a
}

// ExactLimit is the most points whose neighbors SituCraft finds exactly; above it, by NN-Descent
// (Nearest). Exact search costs the square of the points: 6 s for 50,000 on ten cores.
const ExactLimit = 50000

// SearchWidth is the neighbors NN-Descent keeps while searching for k: three times k, at most 45,
// and never fewer than k. A wider search meets more candidates: on the development bundles' 50
// components, searching 42 for 14 finds 99.9% of the exact neighbors, searching 14 about 92%.
func SearchWidth(k int) int { return max(k, min(3*k, 45)) }

// Approximate says how a neighbor table was found when not exactly: NN-Descent's parameters, its
// rounds, and its recall against exact search over evenly spaced query points.
type Approximate struct {
	Method  string  `json:"method"`
	Width   int     `json:"width"`
	Rho     float64 `json:"rho"`
	Delta   float64 `json:"delta"`
	Rounds  int     `json:"rounds"`
	Queries int     `json:"queries"`
	Recall  float64 `json:"recall"`
}

// Nearest finds each point's k nearest other points: exactly up to ExactLimit points, by NN-Descent
// above (searching SearchWidth(k), keeping the k nearest), with its recall measured over 1,000
// evenly spaced points. start, if not nil, seeds NN-Descent; rho is its sample rate.
func Nearest(ctx context.Context, x []float64, n, dim, k int, seed uint64, rho float64, start *Neighbors, workers int, progress func(float64)) (*Neighbors, *Approximate, error) {
	if n <= ExactLimit {
		nb, err := KNN(ctx, x, n, dim, k, workers, progress)
		return nb, nil, err
	}
	p := DefaultDescent(seed)
	p.Rho, p.Start = rho, start
	width := min(SearchWidth(k), n-1)
	wide, stats, err := NNDescent(ctx, x, n, dim, width, p, workers, func(f float64) {
		if progress != nil {
			progress(0.9 * f)
		}
	})
	if err != nil {
		return nil, nil, err
	}
	nb := wide
	if width > k {
		nb = &Neighbors{N: n, K: k, Index: make([]int32, n*k), Distances: make([]float64, n*k)}
		for i := 0; i < n; i++ {
			copy(nb.Index[i*k:(i+1)*k], wide.Index[i*width:i*width+k])
			copy(nb.Distances[i*k:(i+1)*k], wide.Distances[i*width:i*width+k])
		}
	}
	queries := min(n, 1000)
	recall, err := SampledRecall(ctx, x, n, dim, nb, queries, workers)
	if err != nil {
		return nil, nil, err
	}
	if progress != nil {
		progress(1)
	}
	return nb, &Approximate{Method: "NN-Descent", Width: width, Rho: p.Rho, Delta: p.Delta, Rounds: stats.Rounds, Queries: queries, Recall: math.Round(recall*1e6) / 1e6}, nil
}

// SampledRecall is the share of the exact k nearest neighbors a table holds, over queries points
// evenly spaced through the points (every n/queries-th), each searched exactly against all.
func SampledRecall(ctx context.Context, x []float64, n, dim int, nb *Neighbors, queries, workers int) (float64, error) {
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	k := nb.K
	hits := make([]int, queries)
	var failed error
	var mu sync.Mutex
	forEach(queries, workers, func(a, b int) {
		heap := make([]entry, 0, k)
		for q := a; q < b; q++ {
			if q%32 == 0 && ctx.Err() != nil {
				mu.Lock()
				failed = ctx.Err()
				mu.Unlock()
				return
			}
			i := q * n / queries
			heap = heap[:0]
			for j := 0; j < n; j++ {
				if j != i {
					heap = push(heap, k, entry{sqDist(x, dim, i, j), int32(j)})
				}
			}
			have := map[int32]bool{}
			for _, j := range nb.Index[i*k : (i+1)*k] {
				have[j] = true
			}
			for _, e := range heap {
				if have[e.j] {
					hits[q]++
				}
			}
		}
	})
	if failed != nil {
		return 0, failed
	}
	total := 0
	for _, h := range hits {
		total += h
	}
	return float64(total) / float64(queries*k), nil
}
