package random

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"situcraft/internal/stats"
)

const draws = 20000

// alpha is the level below which a goodness-of-fit test fails. The seeds are fixed, so a test
// either always passes or always fails: no flakiness, and a failure is a wrong distribution.
const alpha = 1e-3

func continuous(t *testing.T, name string, draw func() float64, cdf func(float64) float64) {
	t.Helper()
	x := make([]float64, draws)
	for i := range x {
		x[i] = draw()
	}
	if d, p := stats.KS(x, cdf); p < alpha {
		t.Errorf("%s: Kolmogorov–Smirnov D %.4f, p %.2g", name, d, p)
	}
}

func discrete(t *testing.T, name string, draw func() int, pmf func(int) float64) {
	t.Helper()
	counts := map[int]float64{}
	top := 0
	for i := 0; i < draws; i++ {
		k := draw()
		counts[k]++
		top = max(top, k)
	}
	// The bins up to well past the largest draw, the last holding the rest of the tail.
	n := top + 10
	observed, expected := make([]float64, n+1), make([]float64, n+1)
	rest := 1.0
	for k := 0; k < n; k++ {
		observed[k] = counts[k]
		expected[k] = pmf(k) * draws
		rest -= pmf(k)
	}
	expected[n] = math.Max(rest, 0) * draws
	if stat, df, p := stats.ChiSquare(observed, expected, 0); p < alpha {
		t.Errorf("%s: χ² %.1f on %d degrees of freedom, p %.2g", name, stat, df, p)
	}
}

func TestDistributions(t *testing.T) {
	s := New(20261014, 1)
	continuous(t, "normal", s.Normal, stats.NormalCDF)
	continuous(t, "uniform", s.Float64, func(x float64) float64 { return x })
	continuous(t, "exponential", s.Exponential, func(x float64) float64 { return 1 - math.Exp(-x) })
	continuous(t, "log-normal(1, 0.5)", func() float64 { return s.LogNormal(1, 0.5) }, func(x float64) float64 { return stats.NormalCDF((math.Log(x) - 1) / 0.5) })
	for _, shape := range []float64{0.3, 1, 3.7, 25} {
		continuous(t, fmt.Sprintf("gamma(%g, 2)", shape), func() float64 { return s.Gamma(shape, 2) }, func(x float64) float64 { return stats.GammaP(shape, x/2) })
	}
	for _, m := range []float64{0.3, 4, 29.9, 75, 240} {
		discrete(t, fmt.Sprintf("Poisson(%g)", m), func() int { return s.Poisson(m) }, func(k int) float64 { return stats.PoissonPMF(k, m) })
	}
	for _, c := range [][2]float64{{10, 2}, {3, 0.4}, {60, 8}} {
		discrete(t, fmt.Sprintf("negative binomial(%g, %g)", c[0], c[1]), func() int { return s.NegativeBinomial(c[0], c[1]) }, func(k int) float64 { return stats.NegativeBinomialPMF(k, c[0], c[1]) })
	}
	discrete(t, "IntN(7)", func() int { return s.IntN(7) }, func(k int) float64 {
		if k < 7 {
			return 1.0 / 7
		}
		return 0
	})
}

// The Chinese restaurant table distribution: P(l tables | n, r) = |s(n, l)| r^l / (r(r+1)⋯(r+n−1)),
// |s(n, l)| the unsigned Stirling numbers of the first kind, by their recurrence.
func TestCRT(t *testing.T) {
	s := New(20261015, 2)
	for _, c := range []struct {
		n int
		r float64
	}{{1, 0.5}, {5, 0.7}, {20, 3.2}, {30, 12}} {
		stirling := []float64{1} // |s(0, 0)|
		for m := 0; m < c.n; m++ {
			next := make([]float64, len(stirling)+1)
			for l, v := range stirling {
				next[l] += float64(m) * v
				next[l+1] += v
			}
			stirling = next
		}
		rising := 1.0
		for j := 0; j < c.n; j++ {
			rising *= c.r + float64(j)
		}
		discrete(t, fmt.Sprintf("CRT(%d, %g)", c.n, c.r), func() int { return s.CRT(c.n, c.r) }, func(l int) float64 {
			if l >= len(stirling) {
				return 0
			}
			return stirling[l] * math.Pow(c.r, float64(l)) / rising
		})
	}
	if New(1, 1).CRT(0, 3) != 0 {
		t.Error("no customers, no tables")
	}
}

// A Dirichlet draw's components are beta: component k of Dirichlet(α) is Beta(α_k, Σα − α_k).
func TestDirichlet(t *testing.T) {
	s := New(20261015, 3)
	shapes := []float64{0.6, 2, 7.5}
	components := make([][]float64, len(shapes))
	out := make([]float64, len(shapes))
	for i := 0; i < draws; i++ {
		s.Dirichlet(shapes, out)
		var sum float64
		for k, v := range out {
			components[k] = append(components[k], v)
			sum += v
		}
		if math.Abs(sum-1) > 1e-12 {
			t.Fatalf("a draw sums to %g", sum)
		}
	}
	for k, a := range shapes {
		b := 0.6 + 2 + 7.5 - a
		if d, p := stats.KS(components[k], func(x float64) float64 { return stats.BetaCDF(x, a, b) }); p < alpha {
			t.Errorf("Dirichlet component %d: D %.4f, p %.2g", k, d, p)
		}
	}
}

// The same seed and stream give the same draws, other streams others; and the draws are the same
// bits on every processor (this checksum is the same on amd64, where CI runs, and arm64).
func TestReproducible(t *testing.T) {
	sum := func(seed, stream uint64) string {
		s := New(seed, stream)
		h := sha256.New()
		var b [8]byte
		for i := 0; i < 2000; i++ {
			for _, v := range []float64{s.Float64(), s.Normal(), s.Gamma(0.7, 3), s.Gamma(4.2, 1), s.LogNormal(0, 1), float64(s.Poisson(55.5)), float64(s.NegativeBinomial(12, 1.5)), s.Exponential()} {
				binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
				h.Write(b[:])
			}
		}
		return fmt.Sprintf("%x", h.Sum(nil))[:16]
	}
	a := sum(7, 3)
	if a != sum(7, 3) || a == sum(7, 4) || a == sum(8, 3) {
		t.Fatal("streams are not reproducible, or not distinct")
	}
	if a != pinned {
		t.Fatalf("the draws' checksum is %s, not %s: a draw changed (or differs on this processor)", a, pinned)
	}
}

const pinned = "eb112cf009d10083"
