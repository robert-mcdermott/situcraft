// Package stats has the distribution functions and goodness-of-fit tests that check SituCraft's
// simulations and samplers against the distributions they claim to draw from:
//
//   - the regularized incomplete gamma function, by its series below a + 1 and its continued
//     fraction above (Abramowitz M, Stegun IA. Handbook of Mathematical Functions, 1964, 6.5.29
//     and 6.5.31; the fraction evaluated by Lentz's method), for the chi-square, gamma and Poisson
//     distribution functions;
//   - the one-sample Kolmogorov–Smirnov test, its p-value from the limiting distribution with
//     Stephens' correction for the sample size (Stephens MA. Use of the Kolmogorov–Smirnov,
//     Cramér–von Mises and related statistics without extensive tables. J R Stat Soc B
//     1970;32:115–122);
//   - Pearson's chi-square test of counts against expected ones, neighbouring bins merged in
//     order until each expects at least five (what is left at the end joins the last);
//   - the regularized incomplete beta function, by its continued fraction (Abramowitz and Stegun
//     26.5.8, evaluated by Lentz's method), for the beta distribution function;
//   - the two-sample Kolmogorov–Smirnov test, its p-value from the limiting distribution at the
//     samples' effective size n₁n₂/(n₁ + n₂), with Stephens' correction.
package stats

import (
	"math"
	"sort"
)

// GammaP is the regularized lower incomplete gamma function P(a, x).
func GammaP(a, x float64) float64 {
	switch {
	case !(a > 0) || x < 0 || math.IsNaN(x):
		return math.NaN()
	case x == 0:
		return 0
	case math.IsInf(x, 1):
		return 1
	case x < a+1:
		return gammaSeries(a, x)
	}
	return 1 - gammaFraction(a, x)
}

// GammaQ is 1 − P(a, x), without cancellation in the upper tail.
func GammaQ(a, x float64) float64 {
	switch {
	case !(a > 0) || x < 0 || math.IsNaN(x):
		return math.NaN()
	case x == 0:
		return 1
	case math.IsInf(x, 1):
		return 0
	case x < a+1:
		return 1 - gammaSeries(a, x)
	}
	return gammaFraction(a, x)
}

// prefix is x^a e^-x / Γ(a).
func prefix(a, x float64) float64 {
	lg, _ := math.Lgamma(a)
	return math.Exp(a*math.Log(x) - x - lg)
}

func gammaSeries(a, x float64) float64 {
	sum, term := 1/a, 1/a
	for n := 1; n < 10000; n++ {
		term *= x / (a + float64(n))
		sum += term
		if math.Abs(term) < math.Abs(sum)*1e-16 {
			break
		}
	}
	return sum * prefix(a, x)
}

func gammaFraction(a, x float64) float64 {
	const tiny = 1e-300
	b := x + 1 - a
	c := 1 / tiny
	d := 1 / b
	h := d
	for i := 1; i < 10000; i++ {
		an := -float64(i) * (float64(i) - a)
		b += 2
		d = an*d + b
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = b + an/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		delta := d * c
		h *= delta
		if math.Abs(delta-1) < 1e-16 {
			break
		}
	}
	return h * prefix(a, x)
}

// ChiSquareP is the upper-tail probability of a chi-square statistic with df degrees of freedom.
func ChiSquareP(stat float64, df int) float64 {
	if df <= 0 {
		return math.NaN()
	}
	return GammaQ(float64(df)/2, stat/2)
}

// NormalCDF is the standard normal distribution function.
func NormalCDF(z float64) float64 { return 0.5 * math.Erfc(-z/math.Sqrt2) }

// PoissonPMF is P(N = k) for a Poisson mean m.
func PoissonPMF(k int, m float64) float64 {
	if k < 0 {
		return 0
	}
	lg, _ := math.Lgamma(float64(k) + 1)
	return math.Exp(float64(k)*math.Log(m) - m - lg)
}

// NegativeBinomialPMF is P(N = k) for mean m and dispersion r (variance m + m²/r).
func NegativeBinomialPMF(k int, m, r float64) float64 {
	if k < 0 {
		return 0
	}
	a, _ := math.Lgamma(float64(k) + r)
	b, _ := math.Lgamma(r)
	c, _ := math.Lgamma(float64(k) + 1)
	p := r / (r + m)
	return math.Exp(a - b - c + r*math.Log(p) + float64(k)*math.Log1p(-p))
}

// KS is the one-sample Kolmogorov–Smirnov test of a sample against a continuous distribution
// function: the statistic D and its p-value.
func KS(sample []float64, cdf func(float64) float64) (d, p float64) {
	x := append([]float64(nil), sample...)
	sort.Float64s(x)
	n := float64(len(x))
	for i, v := range x {
		f := cdf(v)
		d = math.Max(d, math.Max(f-float64(i)/n, float64(i+1)/n-f))
	}
	return d, KolmogorovQ((math.Sqrt(n) + 0.12 + 0.11/math.Sqrt(n)) * d)
}

// KolmogorovQ is the limiting upper tail of the Kolmogorov distribution: 2 Σ (−1)^(k−1) e^(−2k²λ²).
func KolmogorovQ(lambda float64) float64 {
	if lambda < 0.2 {
		return 1
	}
	sum, sign := 0.0, 1.0
	for k := 1; k <= 100; k++ {
		term := sign * math.Exp(-2*float64(k*k)*lambda*lambda)
		sum += term
		if math.Abs(term) < 1e-12 {
			break
		}
		sign = -sign
	}
	return math.Min(1, math.Max(0, 2*sum))
}

// ChiSquare tests observed counts against expected ones (same total, the same bins): neighbouring
// bins are merged in order until each expects at least five, what is left at the end joining the
// last; the degrees of freedom are the merged bins less one, less fitted (the parameters
// estimated from the data).
func ChiSquare(observed, expected []float64, fitted int) (stat float64, df int, p float64) {
	type bin struct{ o, e float64 }
	var bins []bin
	var acc bin
	for i := range observed {
		acc.o += observed[i]
		acc.e += expected[i]
		if acc.e >= 5 {
			bins = append(bins, acc)
			acc = bin{}
		}
	}
	if acc.e > 0 || acc.o > 0 {
		if len(bins) == 0 {
			bins = append(bins, acc)
		} else {
			bins[len(bins)-1].o += acc.o
			bins[len(bins)-1].e += acc.e
		}
	}
	for _, b := range bins {
		if b.e > 0 {
			stat += (b.o - b.e) * (b.o - b.e) / b.e
		}
	}
	df = len(bins) - 1 - fitted
	return stat, df, ChiSquareP(stat, df)
}

// BetaCDF is the beta distribution function of shapes a and b at x: the regularized incomplete
// beta function I_x(a, b).
func BetaCDF(x, a, b float64) float64 {
	switch {
	case x <= 0:
		return 0
	case x >= 1:
		return 1
	}
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	lab, _ := math.Lgamma(a + b)
	front := math.Exp(a*math.Log(x) + b*math.Log1p(-x) - (la + lb - lab))
	// The fraction converges fast below the mean; above it, by symmetry.
	if x < (a+1)/(a+b+2) {
		return front * betaFraction(x, a, b) / a
	}
	return 1 - front*betaFraction(1-x, b, a)/b
}

// betaFraction is A&S 26.5.8's continued fraction, by Lentz's method.
func betaFraction(x, a, b float64) float64 {
	const tiny = 1e-300
	c, d := 1.0, 1-(a+b)*x/(a+1)
	if math.Abs(d) < tiny {
		d = tiny
	}
	d = 1 / d
	h := d
	for m := 1; m <= 10000; m++ {
		fm := float64(m)
		// The even step, then the odd.
		for _, num := range []float64{
			fm * (b - fm) * x / ((a + 2*fm - 1) * (a + 2*fm)),
			-(a + fm) * (a + b + fm) * x / ((a + 2*fm) * (a + 2*fm + 1)),
		} {
			d = 1 + num*d
			if math.Abs(d) < tiny {
				d = tiny
			}
			c = 1 + num/c
			if math.Abs(c) < tiny {
				c = tiny
			}
			d = 1 / d
			h *= d * c
		}
		if math.Abs(d*c-1) < 1e-15 {
			break
		}
	}
	return h
}

// KS2 is the two-sample Kolmogorov–Smirnov test: the largest difference between the samples'
// distribution functions, and its p-value.
func KS2(a, b []float64) (d, p float64) {
	x := append([]float64(nil), a...)
	y := append([]float64(nil), b...)
	sort.Float64s(x)
	sort.Float64s(y)
	i, j := 0, 0
	for i < len(x) && j < len(y) {
		v := math.Min(x[i], y[j])
		for i < len(x) && x[i] == v {
			i++
		}
		for j < len(y) && y[j] == v {
			j++
		}
		d = math.Max(d, math.Abs(float64(i)/float64(len(x))-float64(j)/float64(len(y))))
	}
	n := float64(len(x)) * float64(len(y)) / float64(len(x)+len(y))
	en := math.Sqrt(n)
	return d, KolmogorovQ((en + 0.12 + 0.11/en) * d)
}
