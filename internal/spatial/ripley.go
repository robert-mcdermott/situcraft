package spatial

// Ripley's K and Besag's L (Ripley BD. Modelling spatial patterns. J R Stat Soc B 1977;39:172–212;
// Besag JE. Contribution to the discussion of Dr Ripley's paper. J R Stat Soc B 1977;39:193–195),
// of each label's cells and across each pair of labels, in a window W of area |W|:
//
//	K_AB(r) = |W| / (n_A·(n_B − [A = B])) · Σ_{i ∈ A} Σ_{j ∈ B, j ≠ i} w_ij·1[d_ij ≤ r],   L = √(K/π),
//
// w_ij Ripley's isotropic edge correction: 1 over the share of the circle about cell i through
// cell j that lies inside W (1 when the circle is wholly inside; "none" sets every weight to 1). A
// pattern of complete spatial randomness has K = πr², L = r. Cells cannot overlap and tissue is not
// uniform, so that line is a reference only; the comparison is with the labels dealt again to the
// same cells (random labeling): each permutation's L, the pointwise 2.5% and 97.5% of them
// (an envelope), and a global test over the radii, the maximum absolute deviation from the
// permutations' mean (Diggle PJ. Statistical Analysis of Spatial Point Patterns, 2nd ed. Arnold,
// 2003; Baddeley A, Diggle PJ, Hardegen A, et al. On tests of spatial pattern based on simulation
// envelopes. Ecol Monogr 2014;84:477–489), its p-value (1 + k) / (1 + n), adjusted over the pairs
// by Benjamini and Hochberg's procedure. The cells' positions are fixed, so the pairs within the
// farthest radius, their distances and their weights are found once.

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

// RipleyParams are Ripley's K's radii (µm, increasing), edge correction ("isotropic" or "none"),
// permutations and seed.
type RipleyParams struct {
	Radii        []float64 `json:"radii"`
	Correction   string    `json:"correction"`
	Permutations int       `json:"permutations"`
	Seed         uint64    `json:"seed"`
}

// DefaultRipley are radii of 5 to 100 µm, the isotropic correction, 199 permutations.
func DefaultRipley() RipleyParams {
	r := make([]float64, 20)
	for i := range r {
		r[i] = float64(5 * (i + 1))
	}
	return RipleyParams{Radii: r, Correction: "isotropic", Permutations: 199, Seed: 20250}
}

// Check says what is wrong with the parameters.
func (p *RipleyParams) Check() error {
	if len(p.Radii) < 1 || len(p.Radii) > 200 {
		return errors.New("Ripley's K is at 1 to 200 radii")
	}
	for i, r := range p.Radii {
		if math.IsNaN(r) || r <= 0 || i > 0 && r <= p.Radii[i-1] {
			return errors.New("Ripley's K's radii increase from more than 0 µm")
		}
	}
	if p.Radii[len(p.Radii)-1] > 500 {
		return errors.New("Ripley's K reaches at most 500 µm")
	}
	if p.Correction == "" {
		p.Correction = "isotropic"
	}
	if p.Correction != "isotropic" && p.Correction != "none" {
		return errors.New("Ripley's K's edge correction is isotropic or none")
	}
	if p.Permutations < 19 || p.Permutations > 9999 {
		return errors.New("Ripley's K's permutations are 19 to 9,999")
	}
	return nil
}

// Ripley is Ripley's K's result. Curves are indexed [(A·K + B)·R + r] over the R radii.
type Ripley struct {
	K       int
	Radii   []float64
	Area    float64
	Cells   []int // of each label, inside the window
	Pairs   int   // pairs of labeled cells within the farthest radius
	Outside int   // labeled cells outside the window, left out
	KObs    []float64
	L       []float64
	Mean    []float64 // the permutations' mean L
	Lo, Hi  []float64 // their 2.5% and 97.5%
	// P: the global test's p-value of each pair (NaN for a label without cells); PAdj adjusted.
	P, PAdj []float64
}

type ripleyPair struct {
	i, j     int32
	bin      int32
	wij, wji float32
}

// pairChunks holds pairs in chunks of a fixed size, in the order added: it grows without copying
// what it holds, as a slice grown by doubling would (twice the pairs, at a million cells several
// GB).
type pairChunks [][]ripleyPair

const pairChunk = 1 << 16

func (c *pairChunks) add(p ripleyPair) {
	if n := len(*c); n == 0 || len((*c)[n-1]) == pairChunk {
		*c = append(*c, make([]ripleyPair, 0, pairChunk))
	}
	last := &(*c)[len(*c)-1]
	*last = append(*last, p)
}

// Ripleys computes K and L of the labeled cells (x, y: centroids, µm; label: 0 to K−1, or −1 for none)
// in a window.
func Ripleys(ctx context.Context, x, y []float32, label []int32, K int, win *Window, p RipleyParams, workers int, progress func(float64)) (*Ripley, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	if len(x) != len(y) || len(label) != len(x) {
		return nil, errors.New("spatial: the cells' centroids and labels differ in length")
	}
	if K < 1 || K > 60 {
		return nil, errors.New("spatial: Ripley's K of 1 to 60 labels")
	}
	if workers < 1 {
		workers = runtime.NumCPU()
	}
	if progress == nil {
		progress = func(float64) {}
	}
	R := len(p.Radii)
	reach := p.Radii[R-1]
	out := &Ripley{K: K, Radii: append([]float64(nil), p.Radii...), Area: win.Area, Cells: make([]int, K)}
	var cells []int32
	for i := range label {
		if label[i] < -1 || label[i] >= int32(K) {
			return nil, errors.New("spatial: a label beyond the label set")
		}
		if label[i] < 0 {
			continue
		}
		if !win.Contains(float64(x[i]), float64(y[i])) {
			out.Outside++
			continue
		}
		cells = append(cells, int32(i))
		out.Cells[label[i]]++
	}
	n := len(cells)
	if n < 2 {
		return nil, errors.New("spatial: fewer than two labeled cells inside the window")
	}
	// The pairs within reach, through a grid of squares of the reach; each cell's pairs with the
	// cells after it, found by the worker of its range, kept in its chunks, read in order.
	x0, y0 := math.Inf(1), math.Inf(1)
	for _, i := range cells {
		x0, y0 = math.Min(x0, float64(x[i])), math.Min(y0, float64(y[i]))
	}
	type key struct{ x, y int64 }
	sq := func(c int) key {
		i := cells[c]
		return key{int64(math.Floor((float64(x[i]) - x0) / reach)), int64(math.Floor((float64(y[i]) - y0) / reach))}
	}
	grid := map[key][]int32{}
	for c := range cells {
		k := sq(c)
		grid[k] = append(grid[k], int32(c))
	}
	found := make([]pairChunks, workers)
	errs := make([]error, workers)
	parallelFor(n, workers, func(w, a, b int) {
		win := win.Copy()
		for c := a; c < b; c++ {
			if (c-a)%512 == 0 && ctx.Err() != nil {
				errs[w] = ctx.Err()
				return
			}
			i := cells[c]
			xi, yi := float64(x[i]), float64(y[i])
			bi := math.Inf(1)
			if p.Correction == "isotropic" {
				bi = win.Distance(xi, yi)
			}
			k := sq(c)
			for dy := int64(-1); dy <= 1; dy++ {
				for dx := int64(-1); dx <= 1; dx++ {
					for _, e := range grid[key{k.x + dx, k.y + dy}] {
						if int(e) <= c {
							continue
						}
						j := cells[e]
						d := distance(x[i], y[i], x[j], y[j])
						if d > reach {
							continue
						}
						pr := ripleyPair{i: int32(c), j: e, bin: int32(sort.SearchFloat64s(p.Radii, d)), wij: 1, wji: 1}
						if p.Correction == "isotropic" && d > 0 {
							if d > bi {
								pr.wij = float32(1 / math.Max(win.ArcInside(xi, yi, d), 1e-6))
							}
							if d > win.Distance(float64(x[j]), float64(y[j])) {
								pr.wji = float32(1 / math.Max(win.ArcInside(float64(x[j]), float64(y[j]), d), 1e-6))
							}
						}
						found[w].add(pr)
					}
				}
			}
		}
		progress(0.3 * float64(b) / float64(n))
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	var chunks [][]ripleyPair
	for _, f := range found {
		chunks = append(chunks, f...)
	}
	for _, ch := range chunks {
		out.Pairs += len(ch)
	}
	lab := make([]int32, n)
	for c, i := range cells {
		lab[c] = label[i]
	}
	// L of each pair of labels at each radius, from the pairs' weights summed by radius and label.
	curves := func(lab []int32, sums []float64, kout, lout []float64) {
		clear(sums)
		for _, ch := range chunks {
			for _, pr := range ch {
				a, b := int(lab[pr.i]), int(lab[pr.j])
				sums[(a*K+b)*R+int(pr.bin)] += float64(pr.wij)
				sums[(b*K+a)*R+int(pr.bin)] += float64(pr.wji)
			}
		}
		for a := 0; a < K; a++ {
			for b := 0; b < K; b++ {
				den := float64(out.Cells[a]) * float64(out.Cells[b]-btoi(a == b))
				cum := 0.0
				for r := 0; r < R; r++ {
					q := (a*K+b)*R + r
					cum += sums[q]
					k := math.NaN()
					if den > 0 {
						k = mul(win.Area, cum) / den
					}
					if kout != nil {
						kout[q] = k
					}
					lout[q] = math.Sqrt(k / math.Pi)
				}
			}
		}
	}
	out.KObs, out.L = make([]float64, K*K*R), make([]float64, K*K*R)
	curves(lab, make([]float64, K*K*R), out.KObs, out.L)

	// The permutations' curves, each from its own stream, kept as float32 for the envelope.
	P := p.Permutations
	perm := make([]float32, P*K*K*R)
	var mu sync.Mutex
	finished := 0
	parallelFor(P, workers, func(w, a, b int) {
		mine := make([]int32, n)
		sums := make([]float64, K*K*R)
		l := make([]float64, K*K*R)
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
			curves(mine, sums, nil, l)
			for q, v := range l {
				perm[s*K*K*R+q] = float32(v)
			}
			mu.Lock()
			finished++
			progress(0.3 + 0.65*float64(finished)/float64(P))
			mu.Unlock()
		}
	})
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	out.Mean, out.Lo, out.Hi = make([]float64, K*K*R), make([]float64, K*K*R), make([]float64, K*K*R)
	vals := make([]float64, P)
	for q := 0; q < K*K*R; q++ {
		sum := 0.0
		for s := 0; s < P; s++ {
			vals[s] = float64(perm[s*K*K*R+q])
			sum += vals[s]
		}
		out.Mean[q] = sum / float64(P)
		sort.Float64s(vals)
		out.Lo[q], out.Hi[q] = quantile(vals, 0.025), quantile(vals, 0.975)
	}
	// The global test: the largest deviation from the permutations' mean over the radii.
	out.P, out.PAdj = make([]float64, K*K), make([]float64, K*K)
	deviation := func(curve func(r int) float64, q int) float64 {
		dmax := 0.0
		for r := 0; r < R; r++ {
			if v := curve(r); !math.IsNaN(v) {
				dmax = math.Max(dmax, math.Abs(v-out.Mean[q*R+r]))
			}
		}
		return dmax
	}
	var ps []float64
	var tested []int
	for q := 0; q < K*K; q++ {
		a, b := q/K, q%K
		if out.Cells[a] == 0 || out.Cells[b]-btoi(a == b) <= 0 {
			out.P[q], out.PAdj[q] = math.NaN(), math.NaN()
			continue
		}
		obs := deviation(func(r int) float64 { return out.L[q*R+r] }, q)
		far := 0
		for s := 0; s < P; s++ {
			// Allowing for float32's rounding of the permutations' curves.
			if deviation(func(r int) float64 { return float64(perm[s*K*K*R+q*R+r]) }, q) >= obs-mul(1e-6, math.Max(1, obs)) {
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
	return out, nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
