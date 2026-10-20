package spatial

// Nearest distances by label: for each cell of label A, the distance from its centroid to the nearest
// cell of label B (another cell of A, for B = A), searched up to a reach; a cell with none within the
// reach counts at the reach. For each pair, the median and mean of these distances, and the share of
// A's cells within each distance (the distribution, cross-type G; Diggle 2003); against the labels
// dealt again to the same cells (random labeling): the permutations' mean and standard deviation of
// the median, z, a two-sided p-value ((1 + k) / (1 + n), the permutations at least as far from
// their mean), adjusted over the ordered pairs by Benjamini and Hochberg's procedure, and the
// distribution's pointwise 2.5% and 97.5% (its envelope). A median shorter than the permutations'
// says B lies nearer A's cells than chance would put it; longer, that it keeps away.

import (
	"context"
	"errors"
	"math"
	"runtime"
	"sort"
	"sync"

	"situcraft/internal/random"
	"situcraft/internal/stats"
)

// NearestParams are the permutations and seed, the reach (µm) and the distribution's step (µm).
type NearestParams struct {
	Permutations int     `json:"permutations"`
	Seed         uint64  `json:"seed"`
	Reach        float64 `json:"reach"`
	Step         float64 `json:"step"`
}

// DefaultNearest are 199 permutations, a reach of 100 µm and a step of 2.5 µm.
func DefaultNearest() NearestParams {
	return NearestParams{Permutations: 199, Seed: 20250, Reach: 100, Step: 2.5}
}

// Check says what is wrong with the parameters.
func (p *NearestParams) Check() error {
	if p.Permutations < 19 || p.Permutations > 9999 {
		return errors.New("nearest distances' permutations are 19 to 9,999")
	}
	if math.IsNaN(p.Reach) || p.Reach <= 0 || p.Reach > 1000 {
		return errors.New("nearest distances reach more than 0 and at most 1,000 µm")
	}
	if math.IsNaN(p.Step) || p.Step <= 0 || p.Reach/p.Step > 200 {
		return errors.New("nearest distances' step is more than 0 µm and at least the reach over 200")
	}
	return nil
}

// Nearest is nearest distances' result: by pair [A·K + B], and distributions [(A·K + B)·D + d] at
// the distances Distances.
type Nearest struct {
	K         int
	Reach     float64
	Distances []float64
	Cells     []int
	// Beyond: A's cells with no B within the reach.
	Beyond               []int
	Median, Mean         []float64
	PermMean, PermSD     []float64 // of the median
	Z, P, PAdj           []float64
	CDF, Lo, Hi, MeanCDF []float64
}

// NearestDistances computes them for labeled cells (x, y: centroids, µm; label: 0 to K−1, or −1).
func NearestDistances(ctx context.Context, x, y []float32, label []int32, K int, p NearestParams, workers int, progress func(float64)) (*Nearest, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	if len(x) != len(y) || len(label) != len(x) {
		return nil, errors.New("spatial: the cells' centroids and labels differ in length")
	}
	if K < 1 || K > 100 {
		return nil, errors.New("spatial: nearest distances of 1 to 100 labels")
	}
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	if progress == nil {
		progress = func(float64) {}
	}
	var cells []int32
	out := &Nearest{K: K, Reach: p.Reach, Cells: make([]int, K)}
	for i := range label {
		if label[i] < -1 || label[i] >= int32(K) {
			return nil, errors.New("spatial: a label beyond the label set")
		}
		if label[i] >= 0 {
			cells = append(cells, int32(i))
			out.Cells[label[i]]++
		}
	}
	n := len(cells)
	if n < 2 {
		return nil, errors.New("spatial: fewer than two labeled cells")
	}
	for d := p.Step; d <= p.Reach+1e-9; d += p.Step {
		out.Distances = append(out.Distances, math.Round(d*1e9)/1e9)
	}
	D := len(out.Distances)
	// Each cell's neighbors within the reach, nearest first: found once (the places do not move),
	// then each deal's nearest of each label is the first neighbor of it.
	side := math.Max(p.Reach/4, 1e-6)
	x0, y0 := math.Inf(1), math.Inf(1)
	for _, i := range cells {
		x0, y0 = math.Min(x0, float64(x[i])), math.Min(y0, float64(y[i]))
	}
	type key struct{ x, y int64 }
	sq := func(c int) key {
		i := cells[c]
		return key{int64(math.Floor((float64(x[i]) - x0) / side)), int64(math.Floor((float64(y[i]) - y0) / side))}
	}
	grid := map[key][]int32{}
	for c := range cells {
		k := sq(c)
		grid[k] = append(grid[k], int32(c))
	}
	type nb struct {
		c int32
		d float32
	}
	near := make([][]nb, n)
	errs := make([]error, workers)
	reachSquares := int64(math.Ceil(p.Reach / side))
	// Each cell's list is kept in its worker's store, chunks of a fixed size (a longer list one of
	// its own): what the lists hold, not as much again in slices grown for each cell.
	const chunk = 1 << 16
	parallelFor(n, workers, func(w, a, b int) {
		var list, store []nb
		for c := a; c < b; c++ {
			if (c-a)%256 == 0 && ctx.Err() != nil {
				errs[w] = ctx.Err()
				return
			}
			i := cells[c]
			k := sq(c)
			list = list[:0]
			for dy := -reachSquares; dy <= reachSquares; dy++ {
				for dx := -reachSquares; dx <= reachSquares; dx++ {
					for _, e := range grid[key{k.x + dx, k.y + dy}] {
						if int(e) == c {
							continue
						}
						d := distance(x[i], y[i], x[cells[e]], y[cells[e]])
						if d <= p.Reach {
							list = append(list, nb{e, float32(d)})
						}
					}
				}
			}
			sort.Slice(list, func(u, v int) bool { return list[u].d < list[v].d || list[u].d == list[v].d && list[u].c < list[v].c })
			if len(list) > cap(store)-len(store) {
				store = make([]nb, 0, max(chunk, len(list)))
			}
			at := len(store)
			store = append(store, list...)
			near[c] = store[at:len(store):len(store)]
		}
		progress(0.2 * float64(b) / float64(n))
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	lab := make([]int32, n)
	for c, i := range cells {
		lab[c] = label[i]
	}
	// One deal's numbers: each pair's median and mean distance, and its distribution. dist, vals and
	// count are a worker's own, kept from deal to deal (a million cells' deals would otherwise
	// leave as much garbage as the neighbors hold).
	type scratch struct {
		dist, vals []float64
		count      []int
	}
	newScratch := func() *scratch {
		return &scratch{dist: make([]float64, n*K), vals: make([]float64, 0, n), count: make([]int, D)}
	}
	measure := func(lab []int32, sc *scratch, median, mean, cdf []float64, beyond []int) {
		dist, count, vals := sc.dist, sc.count, sc.vals
		// dist[c*K + B]: cell c's distance to its nearest B, at most the reach.
		for c := 0; c < n; c++ {
			row := dist[c*K : (c+1)*K]
			for b := range row {
				row[b] = math.Inf(1)
			}
			left := K
			for _, e := range near[c] {
				l := lab[e.c]
				if math.IsInf(row[l], 1) {
					row[l] = float64(e.d)
					left--
					if left == 0 {
						break
					}
				}
			}
		}
		for a := 0; a < K; a++ {
			for b := 0; b < K; b++ {
				q := a*K + b
				vals = vals[:0]
				clear(count)
				if beyond != nil {
					beyond[q] = 0
				}
				for c := 0; c < n; c++ {
					if int(lab[c]) != a {
						continue
					}
					d := dist[c*K+b]
					if math.IsInf(d, 1) {
						d = p.Reach
						if beyond != nil {
							beyond[q]++
						}
					}
					vals = append(vals, d)
					// The distributions' counts, each cell in the first distance at least its own.
					if k := sort.SearchFloat64s(out.Distances, d); k < D && !math.IsInf(dist[c*K+b], 1) {
						count[k]++
					}
				}
				if len(vals) == 0 {
					median[q], mean[q] = math.NaN(), math.NaN()
					for k := 0; k < D; k++ {
						cdf[q*D+k] = math.NaN()
					}
					continue
				}
				sort.Float64s(vals)
				median[q] = quantile(vals, 0.5)
				sum := 0.0
				for _, v := range vals {
					sum += v
				}
				mean[q] = sum / float64(len(vals))
				cum := 0
				for k := 0; k < D; k++ {
					cum += count[k]
					cdf[q*D+k] = float64(cum) / float64(len(vals))
				}
			}
		}
	}
	out.Median, out.Mean, out.CDF, out.Beyond = make([]float64, K*K), make([]float64, K*K), make([]float64, K*K*D), make([]int, K*K)
	measure(lab, newScratch(), out.Median, out.Mean, out.CDF, out.Beyond)

	P := p.Permutations
	permMedian := make([]float64, P*K*K)
	permCDF := make([]float32, P*K*K*D)
	var mu sync.Mutex
	finished := 0
	parallelFor(P, workers, func(w, a, b int) {
		mine := make([]int32, n)
		sc := newScratch()
		med, mean, cdf := make([]float64, K*K), make([]float64, K*K), make([]float64, K*K*D)
		for s := a; s < b; s++ {
			if ctx.Err() != nil {
				errs[w] = ctx.Err()
				return
			}
			st := random.New(p.Seed, uint64(s))
			copy(mine, lab)
			for k := n - 1; k > 0; k-- {
				j := st.IntN(k + 1)
				mine[k], mine[j] = mine[j], mine[k]
			}
			measure(mine, sc, med, mean, cdf, nil)
			copy(permMedian[s*K*K:(s+1)*K*K], med)
			for q, v := range cdf {
				permCDF[s*K*K*D+q] = float32(v)
			}
			mu.Lock()
			finished++
			progress(0.2 + 0.75*float64(finished)/float64(P))
			mu.Unlock()
		}
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	out.PermMean, out.PermSD, out.Z, out.P, out.PAdj = make([]float64, K*K), make([]float64, K*K), make([]float64, K*K), make([]float64, K*K), make([]float64, K*K)
	var ps []float64
	var tested []int
	for q := 0; q < K*K; q++ {
		a, b := q/K, q%K
		if out.Cells[a] == 0 || out.Cells[b]-btoi(a == b) <= 0 {
			out.PermMean[q], out.PermSD[q], out.Z[q], out.P[q], out.PAdj[q] = math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN()
			continue
		}
		sum := 0.0
		for s := 0; s < P; s++ {
			sum += permMedian[s*K*K+q]
		}
		m := sum / float64(P)
		ss := 0.0
		for s := 0; s < P; s++ {
			d := permMedian[s*K*K+q] - m
			ss += mul(d, d)
		}
		out.PermMean[q], out.PermSD[q] = m, math.Sqrt(ss/float64(P-1))
		out.Z[q] = math.NaN()
		if out.PermSD[q] > 0 {
			out.Z[q] = (out.Median[q] - m) / out.PermSD[q]
		}
		dev := math.Abs(out.Median[q] - m)
		far := 0
		for s := 0; s < P; s++ {
			if math.Abs(permMedian[s*K*K+q]-m) >= dev-mul(1e-9, math.Max(1, dev)) {
				far++
			}
		}
		out.P[q] = float64(1+far) / float64(1+P)
		ps = append(ps, out.P[q])
		tested = append(tested, q)
	}
	adj := make([]float64, len(ps))
	stats.BenjaminiHochberg(ps, adj)
	for k, q := range tested {
		out.PAdj[q] = adj[k]
	}
	out.Lo, out.Hi, out.MeanCDF = make([]float64, K*K*D), make([]float64, K*K*D), make([]float64, K*K*D)
	vals := make([]float64, P)
	for q := 0; q < K*K*D; q++ {
		sum := 0.0
		for s := 0; s < P; s++ {
			vals[s] = float64(permCDF[s*K*K*D+q])
			sum += vals[s]
		}
		out.MeanCDF[q] = sum / float64(P)
		sort.Float64s(vals)
		out.Lo[q], out.Hi[q] = quantile(vals, 0.025), quantile(vals, 0.975)
	}
	return out, nil
}
