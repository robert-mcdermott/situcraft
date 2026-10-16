package embed

import (
	"context"
	"math"
	"runtime"
	"sync"

	"situcraft/internal/graph"
	"situcraft/internal/linalg"
	"situcraft/internal/random"
)

// Spectral answers the dims eigenvectors of a connected graph's normalized Laplacian
// L = I − D^{−1/2} W D^{−1/2} with the smallest nonzero eigenvalues (Belkin M, Niyogi P. Laplacian
// eigenmaps for dimensionality reduction and data representation. Neural Comput 2003;15:1373–1396),
// as umap-learn's spectral_layout starts its map: n × dims, row-major. They are found by
// Chebyshev-filtered subspace iteration on M = I − L (Zhou Y, Saad Y. A Chebyshev–Davidson
// algorithm for large symmetric eigenproblems. SIAM J Matrix Anal Appl 2007;29:954–971), a few
// guard vectors beside them, the trivial eigenvector D^{1/2}1 deflated, until their residuals fall
// below 10⁻⁴ (at most 40 rounds). ok is false for a graph in pieces (umap-learn then lays the pieces
// out apart; SituCraft starts from the principal components instead) or one too small.
func Spectral(ctx context.Context, g *graph.CSR, dims int, seed uint64) ([]float64, bool) {
	n := g.N
	if n < dims+2 {
		return nil, false
	}
	if _, count := graph.Components(g); count > 1 {
		return nil, false
	}
	degree := make([]float64, n)
	for i := 0; i < n; i++ {
		for e := g.Offsets[i]; e < g.Offsets[i+1]; e++ {
			degree[i] += g.Weights[e]
		}
	}
	s := make([]float64, n)
	v0 := make([]float64, n)
	norm := 0.0
	for i, d := range degree {
		if !(d > 0) {
			return nil, false
		}
		s[i] = 1 / math.Sqrt(d)
		v0[i] = math.Sqrt(d)
		norm += d
	}
	norm = math.Sqrt(norm)
	for i := range v0 {
		v0[i] /= norm
	}
	wn := make([]float64, len(g.Targets))
	for i := 0; i < n; i++ {
		for e := g.Offsets[i]; e < g.Offsets[i+1]; e++ {
			wn[e] = mul(mul(s[i], g.Weights[e]), s[g.Targets[e]])
		}
	}
	p := min(dims+4, n-1)
	rnd := random.New(seed, 1)
	x := make([]float64, n*p)
	for i := range x {
		x[i] = rnd.Normal()
	}
	if !orthonormalize(x, n, p, v0) {
		return nil, false
	}
	mx := make([]float64, n*p)
	t0 := make([]float64, n*p)
	t1 := make([]float64, n*p)
	multiply(g, wn, x, mx, n, p)
	ritz := rayleighRitz(x, mx, n, p)
	residual := func() float64 {
		worst := 0.0
		for c := 0; c < dims; c++ {
			r := 0.0
			for i := 0; i < n; i++ {
				d := mx[i*p+c] - mul(ritz[c], x[i*p+c])
				r += mul(d, d)
			}
			worst = math.Max(worst, math.Sqrt(r))
		}
		return worst
	}
	const degreeOfFilter = 10
	for it := 0; it < 40 && residual() >= 1e-4; it++ {
		if ctx.Err() != nil {
			return nil, false
		}
		lo := -1.0
		hi := math.Max(ritz[p-1], -0.9)
		e := (hi - lo) / 2
		c := (hi + lo) / 2
		copy(t0, x)
		for i := range t1 {
			t1[i] = (mx[i] - mul(c, x[i])) / e
		}
		for d := 2; d <= degreeOfFilter; d++ {
			multiply(g, wn, t1, mx, n, p)
			for i := range t1 {
				t2 := mul(2, mx[i]-mul(c, t1[i]))/e - t0[i]
				t0[i] = t1[i]
				t1[i] = t2
			}
		}
		x, t1 = t1, x
		if !orthonormalize(x, n, p, v0) {
			return nil, false
		}
		multiply(g, wn, x, mx, n, p)
		ritz = rayleighRitz(x, mx, n, p)
	}
	out := make([]float64, n*dims)
	for i := 0; i < n; i++ {
		for c := 0; c < dims; c++ {
			v := x[i*p+c]
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, false
			}
			out[i*dims+c] = v
		}
	}
	return out, true
}

// orthonormalize makes the p columns of the n × p block x orthonormal and orthogonal to v0, by
// Gram–Schmidt twice; false when a column vanishes.
func orthonormalize(x []float64, n, p int, v0 []float64) bool {
	for pass := 0; pass < 2; pass++ {
		for a := 0; a < p; a++ {
			dot := 0.0
			for i := 0; i < n; i++ {
				dot += mul(x[i*p+a], v0[i])
			}
			for i := 0; i < n; i++ {
				x[i*p+a] -= mul(dot, v0[i])
			}
			for b := 0; b < a; b++ {
				dot := 0.0
				for i := 0; i < n; i++ {
					dot += mul(x[i*p+a], x[i*p+b])
				}
				for i := 0; i < n; i++ {
					x[i*p+a] -= mul(dot, x[i*p+b])
				}
			}
			norm := 0.0
			for i := 0; i < n; i++ {
				norm += mul(x[i*p+a], x[i*p+a])
			}
			norm = math.Sqrt(norm)
			if !(norm > 1e-300) {
				return false
			}
			for i := 0; i < n; i++ {
				x[i*p+a] /= norm
			}
		}
	}
	return true
}

// multiply is Y = M X for the block X, each row by one worker (its own output).
func multiply(g *graph.CSR, wn, x, y []float64, n, p int) {
	workers := runtime.NumCPU()
	chunk := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for lo := 0; lo < n; lo += chunk {
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			for i := lo; i < hi; i++ {
				row := y[i*p : i*p+p]
				clear(row)
				for e := g.Offsets[i]; e < g.Offsets[i+1]; e++ {
					w := wn[e]
					src := x[int(g.Targets[e])*p:]
					for a := 0; a < p; a++ {
						row[a] += mul(w, src[a])
					}
				}
			}
		}(lo, min(n, lo+chunk))
	}
	wg.Wait()
}

// rayleighRitz rotates x and mx to the Ritz vectors of span(x), answering the Ritz values in
// decreasing order.
func rayleighRitz(x, mx []float64, n, p int) []float64 {
	h := make([]float64, p*p)
	for a := 0; a < p; a++ {
		for b := a; b < p; b++ {
			s := 0.0
			for i := 0; i < n; i++ {
				s += mul(x[i*p+a], mx[i*p+b])
			}
			h[a*p+b], h[b*p+a] = s, s
		}
	}
	values, vectors, err := linalg.SymEigen(h, p)
	if err != nil {
		return make([]float64, p)
	}
	rx := make([]float64, p)
	rm := make([]float64, p)
	for i := 0; i < n; i++ {
		for c := 0; c < p; c++ {
			sx, sm := 0.0, 0.0
			for a := 0; a < p; a++ {
				sx += mul(x[i*p+a], vectors[a*p+c])
				sm += mul(mx[i*p+a], vectors[a*p+c])
			}
			rx[c], rm[c] = sx, sm
		}
		copy(x[i*p:i*p+p], rx)
		copy(mx[i*p:i*p+p], rm)
	}
	return values
}
