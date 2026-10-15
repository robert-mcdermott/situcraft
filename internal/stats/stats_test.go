package stats

import (
	"math"
	"testing"
)

func near(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// Known values: chi-square critical values, P(1, x) = 1 − e^−x, P(1/2, x) = erf(√x), the
// Kolmogorov distribution's 5% point, and the pmfs summing to one with their means.
func TestKnownValues(t *testing.T) {
	near(t, "χ²(3.841459, 1)", ChiSquareP(3.841459, 1), 0.05, 1e-6)
	near(t, "χ²(18.307038, 10)", ChiSquareP(18.307038, 10), 0.05, 1e-6)
	near(t, "χ²(124.342113, 100)", ChiSquareP(124.342113, 100), 0.05, 1e-6)
	for _, x := range []float64{0.01, 0.5, 2, 9, 40} {
		near(t, "P(1, x)", GammaP(1, x), 1-math.Exp(-x), 1e-14)
		near(t, "P(1/2, x)", GammaP(0.5, x), math.Erf(math.Sqrt(x)), 1e-13)
		near(t, "P + Q", GammaP(3.3, x)+GammaQ(3.3, x), 1, 1e-14)
	}
	near(t, "Q_KS(1.3581)", KolmogorovQ(1.3581), 0.05, 1e-4)
	for _, c := range [][2]float64{{0.7, 0}, {12, 0}, {12, 0.8}, {40, 15}} {
		var sum, mean float64
		for k := 0; k < 5000; k++ {
			var p float64
			if c[1] == 0 {
				p = PoissonPMF(k, c[0])
			} else {
				p = NegativeBinomialPMF(k, c[0], c[1])
			}
			sum += p
			mean += float64(k) * p
		}
		near(t, "pmf sum", sum, 1, 1e-10)
		near(t, "pmf mean", mean, c[0], 1e-8)
	}
}

// A sample from the distribution passes; one from another does not.
func TestKSAndChiSquare(t *testing.T) {
	sample := make([]float64, 2000)
	for i := range sample {
		sample[i] = (float64(i) + 0.5) / 2000
	}
	if _, p := KS(sample, func(x float64) float64 { return x }); p < 0.99 {
		t.Fatalf("an even uniform sample: p %v", p)
	}
	if _, p := KS(sample, func(x float64) float64 { return x * x }); p > 1e-6 {
		t.Fatalf("against another distribution: p %v", p)
	}
	stat, df, p := ChiSquare([]float64{30, 22, 25, 23}, []float64{25, 25, 25, 25}, 0)
	near(t, "χ² statistic", stat, 1.52, 1e-12)
	if df != 3 || p < 0.6 {
		t.Fatalf("df %d, p %v", df, p)
	}
	// Bins expecting fewer than five merge: (2 + 2 + 45), then 45 with the 3.5 left after it.
	if _, df, _ := ChiSquare([]float64{1, 2, 40, 50, 3, 1}, []float64{2, 2, 45, 45, 3, 0.5}, 0); df != 1 {
		t.Fatalf("merged df %d", df)
	}
}

// The beta distribution function against the binomial identity I_x(a, b) = P(Binomial(a+b−1, x) ≥ a)
// for whole shapes, and known closed forms.
func TestBetaCDF(t *testing.T) {
	binom := func(n, k int) float64 {
		v := 1.0
		for i := 1; i <= k; i++ {
			v = v * float64(n-k+i) / float64(i)
		}
		return v
	}
	for a := 1; a <= 9; a += 2 {
		for b := 1; b <= 12; b += 3 {
			for _, x := range []float64{0.01, 0.2, 0.5, 0.77, 0.99} {
				n := a + b - 1
				want := 0.0
				for j := a; j <= n; j++ {
					want += binom(n, j) * math.Pow(x, float64(j)) * math.Pow(1-x, float64(n-j))
				}
				if got := BetaCDF(x, float64(a), float64(b)); math.Abs(got-want) > 1e-12 {
					t.Errorf("I_%g(%d, %d) = %.15g, want %.15g", x, a, b, got, want)
				}
			}
		}
	}
	for _, c := range []struct{ x, a, b, want float64 }{
		{0.5, 3.3, 3.3, 0.5},
		{0.3, 0.5, 1, math.Sqrt(0.3)},
		{0.3, 1, 2.5, 1 - math.Pow(0.7, 2.5)},
		{0, 2, 2, 0}, {1, 2, 2, 1},
	} {
		if got := BetaCDF(c.x, c.a, c.b); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("I_%g(%g, %g) = %.15g, want %.15g", c.x, c.a, c.b, got, c.want)
		}
	}
}

// Two samples of one distribution pass; of two that differ, fail.
func TestKS2(t *testing.T) {
	r := func(seed uint64, shift float64) []float64 {
		x := make([]float64, 3000)
		state := seed
		for i := range x {
			// A sum of twelve uniforms (Irwin–Hall), from a linear congruential generator.
			var u float64
			for k := 0; k < 12; k++ {
				state = state*6364136223846793005 + 1442695040888963407
				u += float64(state>>11) * 0x1p-53
			}
			x[i] = u - 6 + shift
		}
		return x
	}
	if _, p := KS2(r(1, 0), r(2, 0)); p < 1e-3 {
		t.Errorf("one distribution: p %.2g", p)
	}
	if _, p := KS2(r(1, 0), r(2, 0.2)); p > 1e-3 {
		t.Errorf("shifted by 0.2 σ: p %.2g", p)
	}
	if d, _ := KS2([]float64{1, 2, 3}, []float64{4, 5}); d != 1 {
		t.Errorf("disjoint samples: D %g", d)
	}
}
