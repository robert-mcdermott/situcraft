// Package embed lays a clustering's cells out on a map, by UMAP (McInnes L, Healy J, Melville J.
// UMAP: Uniform Manifold Approximation and Projection for dimension reduction. arXiv:1802.03426,
// 2018), as umap-learn's simplicial_set_embedding lays out a fuzzy graph and scanpy's tl.umap calls
// it, and judges the map: its trustworthiness (Venna J, Kaski S. Neighborhood preservation in
// nonlinear projection methods: an experimental study. ICANN 2001:485–491) and each cell's
// neighborhood precision and recall (Venna J, Peltonen J, Nybo K, Aidos H, Kaski S. Information
// retrieval perspective to nonlinear dimensionality reduction for data visualization. J Mach Learn
// Res 2010;11:451–490). A map is an artifact of kind "embedding" (files.go).
//
// Every product is rounded on its own, sums run in a fixed order, logarithms and exponentials come
// from internal/dmath, and the optimization draws from one seeded stream on one thread (above
// graph.ExactLimit cells, in batches over every core, its negative samples from a hash of the seed):
// the map is the same on every processor, and for any number of workers, for a seed. Adapted from
// CytoWeave 0.8.1 web/lib/dimred.js.
package embed

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"

	"situcraft/internal/dmath"
	"situcraft/internal/graph"
	"situcraft/internal/random"
)

func mul(a, b float64) float64 { return float64(a * b) }

// Params are UMAP's parameters.
type Params struct {
	// MinDist: how tightly cells may pack on the map (scanpy's default 0.5; umap-learn's 0.1).
	MinDist float64 `json:"minDist"`
	// Spread: the scale of the map's distances.
	Spread float64 `json:"spread"`
	// Epochs of optimization; 0 for umap-learn's default (500 up to 10,000 cells, else 200).
	Epochs int `json:"epochs"`
	// NegativeRate: random cells pushed away per pull.
	NegativeRate int    `json:"negativeRate"`
	Seed         uint64 `json:"seed"`
}

// The fixed choices: umap-learn's learning rate and repulsion strength.
const (
	LearningRate = 1.0
	Repulsion    = 1.0
)

// DefaultParams are scanpy's tl.umap defaults, with SituCraft's seed.
func DefaultParams() Params {
	return Params{MinDist: 0.5, Spread: 1, Epochs: 0, NegativeRate: 5, Seed: 20250}
}

// Check refuses parameters out of range.
func (p Params) Check() error {
	switch {
	case !(p.Spread > 0) || p.Spread > 10:
		return errors.New("the spread is above 0 and at most 10")
	case !(p.MinDist >= 0) || p.MinDist > p.Spread:
		return errors.New("the minimum distance is from 0 to the spread")
	case p.Epochs < 0 || p.Epochs > 5000:
		return errors.New("the epochs are from 0 (the default) to 5,000")
	case p.NegativeRate < 1 || p.NegativeRate > 50:
		return errors.New("the negative samples per edge are from 1 to 50")
	}
	return nil
}

// FindAB fits umap-learn's curve 1/(1 + a·x^{2b}) to the membership it stands for (1 below the
// minimum distance, exp(−(x − minDist)/spread) above) on 300 points of [0, 3·spread], by least
// squares from a = b = 1 (umap-learn's find_ab_params, which calls scipy's curve_fit), here by
// Levenberg–Marquardt with the analytic Jacobian.
func FindAB(spread, minDist float64) (a, b float64) {
	const m = 300
	xs := make([]float64, m)
	ys := make([]float64, m)
	for i := range xs {
		xs[i] = mul(3, spread) * float64(i) / float64(m-1)
		if xs[i] < minDist {
			ys[i] = 1
		} else {
			ys[i] = dmath.Exp(-(xs[i] - minDist) / spread)
		}
	}
	pow2b := func(x, b float64) float64 {
		if x <= 0 {
			return 0
		}
		return dmath.Exp(mul(mul(2, b), dmath.Log(x)))
	}
	cost := func(a, b float64) float64 {
		s := 0.0
		for i, x := range xs {
			r := 1/(1+mul(a, pow2b(x, b))) - ys[i]
			s += mul(r, r)
		}
		return s
	}
	a, b = 1, 1
	lambda := 1e-3
	current := cost(a, b)
	for it := 0; it < 500; it++ {
		var jaa, jab, jbb, ga, gb float64
		for i, x := range xs {
			x2b := pow2b(x, b)
			u := 1 + mul(a, x2b)
			r := 1/u - ys[i]
			uu := mul(u, u)
			da := -x2b / uu
			db := 0.0
			if x > 0 {
				db = mul(mul(mul(-a, x2b), 2), dmath.Log(x)) / uu
			}
			jaa += mul(da, da)
			jab += mul(da, db)
			jbb += mul(db, db)
			ga += mul(da, r)
			gb += mul(db, r)
		}
		improved := false
		for tries := 0; tries < 30 && !improved; tries++ {
			A := mul(jaa, 1+lambda)
			B := mul(jbb, 1+lambda)
			det := mul(A, B) - mul(jab, jab)
			sa := (mul(-ga, B) + mul(gb, jab)) / det
			sb := (mul(-gb, A) + mul(ga, jab)) / det
			next := cost(a+sa, b+sb)
			if !math.IsNaN(next) && !math.IsInf(next, 0) && next < current {
				relative := (current - next) / current
				a += sa
				b += sb
				current = next
				lambda = math.Max(lambda/10, 1e-12)
				improved = true
				if relative < 1e-14 {
					return a, b
				}
			} else {
				lambda *= 10
			}
		}
		if !improved {
			break
		}
	}
	return a, b
}

// Info is how a map was made.
type Info struct {
	A, B      float64
	Epochs    int
	Edges     int // the undirected edges optimized (after pruning)
	Init      string
	Optimizer string // how, when not on one thread
}

// Batch is the edges the batched optimization moves at once, from the places as they stood.
const Batch = 16384

// Layout lays a graph out in two dimensions: the edges too weak to be sampled once pruned, the
// spectral layout of the graph (or init when the graph is in pieces: n × 2, as the caller chooses)
// scaled to [0, 10] with a little noise, then umap-learn's optimize_layout_euclidean: each epoch,
// every edge due pulls its two cells together by the gradient of log(1/(1 + a·d^{2b})) and pushes
// the first away from NegativeRate random cells, gradients clipped at ±4, the learning rate falling
// linearly to 0. progress, if not nil, is told the share of epochs done.
// Above graph.ExactLimit cells the epochs run in batches over workers (optimizeBatched).
func Layout(ctx context.Context, g *graph.CSR, fallback []float64, p Params, workers int, progress func(float64)) ([]float64, Info, error) {
	if err := p.Check(); err != nil {
		return nil, Info{}, err
	}
	n := g.N
	if n < 4 {
		return nil, Info{}, errors.New("a map needs four cells or more")
	}
	info := Info{Epochs: p.Epochs}
	info.A, info.B = FindAB(p.Spread, p.MinDist)
	defaultEpochs := 200
	if n <= 10000 {
		defaultEpochs = 500
	}
	if info.Epochs == 0 {
		info.Epochs = defaultEpochs
	}
	// Prune: weights below the largest over the epochs are never sampled.
	wmax := 0.0
	for _, w := range g.Weights {
		wmax = math.Max(wmax, w)
	}
	cut := wmax / float64(info.Epochs)
	if info.Epochs <= 10 {
		cut = wmax / float64(defaultEpochs)
	}
	pruned := &graph.CSR{N: n, Offsets: make([]int, n+1)}
	for i := 0; i < n; i++ {
		for e := g.Offsets[i]; e < g.Offsets[i+1]; e++ {
			if g.Weights[e] >= cut {
				pruned.Targets = append(pruned.Targets, g.Targets[e])
				pruned.Weights = append(pruned.Weights, g.Weights[e])
			}
		}
		pruned.Offsets[i+1] = len(pruned.Targets)
	}
	info.Edges = pruned.Edges()
	rnd := random.New(p.Seed, 0)
	start, ok := Spectral(ctx, pruned, 2, p.Seed)
	if err := ctx.Err(); err != nil {
		return nil, info, err
	}
	info.Init = "spectral"
	if !ok {
		if len(fallback) != 2*n {
			return nil, info, errors.New("the graph is in pieces and no other start was given")
		}
		start = append([]float64(nil), fallback...)
		info.Init = "principal components (the graph is in pieces)"
	}
	y := scaleStart(start, n, rnd)
	if n > graph.ExactLimit {
		info.Optimizer = fmt.Sprintf("each epoch's edges in batches of %d over every core, each batch moving the cells from their places before it; negative samples from a hash of the seed", Batch)
		if err := optimizeBatched(ctx, pruned, y, info, p, workers, progress); err != nil {
			return nil, info, err
		}
		return y, info, nil
	}
	if err := optimize(ctx, pruned, y, info, p, rnd, progress); err != nil {
		return nil, info, err
	}
	return y, info, nil
}

// scaleStart is umap-learn's: the layout scaled so its largest coordinate is 10, normal noise of
// standard deviation 10⁻⁴ added, then each axis rescaled to [0, 10].
func scaleStart(start []float64, n int, rnd *random.Stream) []float64 {
	big := 0.0
	for _, v := range start {
		big = math.Max(big, math.Abs(v))
	}
	expansion := 1.0
	if big > 0 {
		expansion = 10 / big
	}
	y := make([]float64, 2*n)
	for i := range y {
		y[i] = mul(start[i], expansion) + mul(rnd.Normal(), 1e-4)
	}
	for d := 0; d < 2; d++ {
		lo, hi := math.Inf(1), math.Inf(-1)
		for i := 0; i < n; i++ {
			lo = math.Min(lo, y[2*i+d])
			hi = math.Max(hi, y[2*i+d])
		}
		span := hi - lo
		if span == 0 {
			span = 1
		}
		for i := 0; i < n; i++ {
			y[2*i+d] = mul(10, y[2*i+d]-lo) / span
		}
	}
	return y
}

func clip4(v float64) float64 {
	if v > 4 {
		return 4
	}
	if v < -4 {
		return -4
	}
	return v
}

// optimize is umap-learn's optimize_layout_euclidean on one thread, with the head moved and the
// tail too (the same layout on both ends), negative samples applied one after another.
func optimize(ctx context.Context, g *graph.CSR, y []float64, info Info, p Params, rnd *random.Stream, progress func(float64)) error {
	n := g.N
	edges := len(g.Targets)
	head := make([]int32, edges)
	for i := 0; i < n; i++ {
		for e := g.Offsets[i]; e < g.Offsets[i+1]; e++ {
			head[e] = int32(i)
		}
	}
	tail := g.Targets
	wmax := 0.0
	for _, w := range g.Weights {
		wmax = math.Max(wmax, w)
	}
	epochs := float64(info.Epochs)
	eps := make([]float64, edges)
	for e, w := range g.Weights {
		if s := mul(epochs, w/wmax); s > 0 {
			eps[e] = epochs / s
		} else {
			eps[e] = -1
		}
	}
	epsNeg := make([]float64, edges)
	for e := range eps {
		epsNeg[e] = eps[e] / float64(p.NegativeRate)
	}
	next := append([]float64(nil), eps...)
	nextNeg := append([]float64(nil), epsNeg...)
	a, b := info.A, info.B
	twoAB := mul(mul(2, a), b)
	twoGB := mul(mul(2, Repulsion), b)
	for epoch := 0; epoch < info.Epochs; epoch++ {
		if epoch%10 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			if progress != nil {
				progress(float64(epoch) / epochs)
			}
		}
		alpha := mul(LearningRate, 1-float64(epoch)/epochs)
		fe := float64(epoch)
		for e := 0; e < edges; e++ {
			if eps[e] <= 0 || next[e] > fe {
				continue
			}
			j, k := int(head[e]), int(tail[e])
			hx, hy := y[2*j], y[2*j+1]
			dx, dy := hx-y[2*k], hy-y[2*k+1]
			d2 := mul(dx, dx) + mul(dy, dy)
			coeff := 0.0
			if d2 > 0 {
				ld := dmath.Log(d2)
				pb := dmath.Exp(mul(b, ld))
				coeff = mul(-twoAB, pb/d2) / (mul(a, pb) + 1)
			}
			gx := clip4(mul(coeff, dx))
			gy := clip4(mul(coeff, dy))
			y[2*j] = hx + mul(gx, alpha)
			y[2*j+1] = hy + mul(gy, alpha)
			y[2*k] -= mul(gx, alpha)
			y[2*k+1] -= mul(gy, alpha)
			next[e] += eps[e]
			negatives := int((fe - nextNeg[e]) / epsNeg[e])
			for s := 0; s < negatives; s++ {
				o := rnd.IntN(n)
				nx, ny := y[2*j]-y[2*o], y[2*j+1]-y[2*o+1]
				nd2 := mul(nx, nx) + mul(ny, ny)
				if nd2 > 0 {
					c := twoGB / mul(0.001+nd2, mul(a, dmath.Exp(mul(b, dmath.Log(nd2))))+1)
					y[2*j] += mul(clip4(mul(c, nx)), alpha)
					y[2*j+1] += mul(clip4(mul(c, ny)), alpha)
				}
			}
			nextNeg[e] += mul(float64(negatives), epsNeg[e])
		}
	}
	if progress != nil {
		progress(1)
	}
	return nil
}

// sampleOf is negative sample s of edge e in an epoch: a cell from a hash of the seed and the three
// (SplitMix64's finalizer; Steele, Lea and Flood 2014), the same whatever the order edges are met.
func sampleOf(seed uint64, epoch, e, s, n int) int {
	z := seed ^ uint64(epoch)*0x9e3779b97f4a7c15 ^ uint64(e)*0xbf58476d1ce4e5b9 ^ uint64(s)*0x94d049bb133111eb
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	z ^= z >> 31
	return int(z % uint64(n))
}

// move is an edge's moves in a batch: its head's (the pull and the pushes), and its tail's.
type move struct{ hx, hy, tx, ty float64 }

// optimizeBatched is optimize over every core, the same for any number of workers: each epoch's
// edges due, in order, in batches of Batch; a batch's moves are computed in parallel from the places
// as they stood before it, then applied in the edges' order.
func optimizeBatched(ctx context.Context, g *graph.CSR, y []float64, info Info, p Params, workers int, progress func(float64)) error {
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	n := g.N
	edges := len(g.Targets)
	head := make([]int32, edges)
	for i := 0; i < n; i++ {
		for e := g.Offsets[i]; e < g.Offsets[i+1]; e++ {
			head[e] = int32(i)
		}
	}
	tail := g.Targets
	wmax := 0.0
	for _, w := range g.Weights {
		wmax = math.Max(wmax, w)
	}
	epochs := float64(info.Epochs)
	eps := make([]float64, edges)
	for e, w := range g.Weights {
		if s := mul(epochs, w/wmax); s > 0 {
			eps[e] = epochs / s
		} else {
			eps[e] = -1
		}
	}
	epsNeg := make([]float64, edges)
	for e := range eps {
		epsNeg[e] = eps[e] / float64(p.NegativeRate)
	}
	next := append([]float64(nil), eps...)
	nextNeg := append([]float64(nil), epsNeg...)
	a, b := info.A, info.B
	twoAB := mul(mul(2, a), b)
	twoGB := mul(mul(2, Repulsion), b)
	due := make([]int32, 0, edges)
	moves := make([]move, Batch)
	for epoch := 0; epoch < info.Epochs; epoch++ {
		if epoch%10 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			if progress != nil {
				progress(float64(epoch) / epochs)
			}
		}
		alpha := mul(LearningRate, 1-float64(epoch)/epochs)
		fe := float64(epoch)
		due = due[:0]
		for e := 0; e < edges; e++ {
			if eps[e] > 0 && next[e] <= fe {
				due = append(due, int32(e))
			}
		}
		for lo := 0; lo < len(due); lo += Batch {
			batch := due[lo:min(len(due), lo+Batch)]
			chunk := (len(batch) + workers - 1) / workers
			var wg sync.WaitGroup
			for w0 := 0; w0 < len(batch); w0 += chunk {
				wg.Add(1)
				go func(a0, a1 int) {
					defer wg.Done()
					for q := a0; q < a1; q++ {
						e := int(batch[q])
						j, k := int(head[e]), int(tail[e])
						hx, hy := y[2*j], y[2*j+1]
						dx, dy := hx-y[2*k], hy-y[2*k+1]
						d2 := mul(dx, dx) + mul(dy, dy)
						coeff := 0.0
						if d2 > 0 {
							pb := dmath.Exp(mul(b, dmath.Log(d2)))
							coeff = mul(-twoAB, pb/d2) / (mul(a, pb) + 1)
						}
						gx := mul(clip4(mul(coeff, dx)), alpha)
						gy := mul(clip4(mul(coeff, dy)), alpha)
						m := move{hx: gx, hy: gy, tx: -gx, ty: -gy}
						negatives := int((fe - nextNeg[e]) / epsNeg[e])
						for s := 0; s < negatives; s++ {
							o := sampleOf(p.Seed, epoch, e, s, n)
							nx, ny := hx-y[2*o], hy-y[2*o+1]
							nd2 := mul(nx, nx) + mul(ny, ny)
							if nd2 > 0 {
								c := twoGB / mul(0.001+nd2, mul(a, dmath.Exp(mul(b, dmath.Log(nd2))))+1)
								m.hx += mul(clip4(mul(c, nx)), alpha)
								m.hy += mul(clip4(mul(c, ny)), alpha)
							}
						}
						moves[q] = m
					}
				}(w0, min(len(batch), w0+chunk))
			}
			wg.Wait()
			for q, e := range batch {
				m := moves[q]
				j, k := int(head[e]), int(tail[e])
				y[2*j] += m.hx
				y[2*j+1] += m.hy
				y[2*k] += m.tx
				y[2*k+1] += m.ty
				negatives := int((fe - nextNeg[e]) / epsNeg[e])
				next[e] += eps[e]
				nextNeg[e] += mul(float64(negatives), epsNeg[e])
			}
		}
	}
	if progress != nil {
		progress(1)
	}
	return nil
}
