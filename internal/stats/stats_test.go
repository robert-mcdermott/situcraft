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
