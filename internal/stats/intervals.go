package stats

// Confidence intervals for counts, proportions and their ratios, at a level such as 0.95:
//
//   - a count of events (a Poisson count): Garwood's exact interval, the gamma quantiles
//     Γ⁻¹(α/2; k) and Γ⁻¹(1 − α/2; k + 1) (Garwood F. Fiducial limits for the Poisson distribution.
//     Biometrika 1936;28:437–442);
//   - a proportion: Wilson's score interval (Wilson EB. Probable inference, the law of succession,
//     and statistical inference. J Am Stat Assoc 1927;22:209–212), and Clopper and Pearson's exact
//     one, the beta quantiles (Clopper CJ, Pearson ES. The use of confidence or fiducial limits
//     illustrated in the case of the binomial. Biometrika 1934;26:404–413);
//   - the ratio of two rates, counts over their exposures (cells over areas): exact, conditional on
//     the counts' sum, the first count binomial among both (Clopper–Pearson), its odds scaled by
//     the exposures (Ederer F, Mantel N. Confidence limits on the ratio of two Poisson variables. Am
//     J Epidemiol 1974;100:165–167);
//   - the ratio of two proportions: Koopman's score interval, the ratios whose Pearson chi-square,
//     at the proportions most likely under that ratio, is within the level's critical value
//     (Koopman PAR. Confidence intervals for the ratio of two binomial proportions. Biometrics
//     1984;40:513–517).
//
// Each answers its lower and upper bound; a bound beyond any finite ratio is +Inf.

import (
	"math"

	"situcraft/internal/dmath"
)

// NormalQuantile is the standard normal distribution's p-quantile.
func NormalQuantile(p float64) float64 { return mul(math.Sqrt2, dmath.Erfinv(2*p-1)) }

// solve finds where an increasing function f crosses target on [lo, hi], by bisection.
func solve(f func(float64) float64, target, lo, hi float64) float64 {
	for i := 0; i < 200; i++ {
		mid := lo + (hi-lo)/2
		if mid <= lo || mid >= hi {
			break
		}
		if f(mid) < target {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo + (hi-lo)/2
}

// GammaQuantile is the p-quantile of the gamma distribution of shape a (scale 1).
func GammaQuantile(p, a float64) float64 {
	switch {
	case !(a > 0) || !(p >= 0 && p <= 1):
		return math.NaN()
	case p == 0:
		return 0
	case p == 1:
		return math.Inf(1)
	}
	hi := a + mul(10, math.Sqrt(a)) + 10
	for GammaP(a, hi) < p {
		hi *= 2
	}
	return solve(func(x float64) float64 { return GammaP(a, x) }, p, 0, hi)
}

// BetaQuantile is the p-quantile of the beta distribution of shapes a and b.
func BetaQuantile(p, a, b float64) float64 {
	switch {
	case !(a > 0) || !(b > 0) || !(p >= 0 && p <= 1):
		return math.NaN()
	case p == 0:
		return 0
	case p == 1:
		return 1
	}
	return solve(func(x float64) float64 { return BetaCDF(x, a, b) }, p, 0, 1)
}

// PoissonInterval is Garwood's interval for the mean of a Poisson count k.
func PoissonInterval(k int, level float64) (lo, hi float64) {
	alpha := 1 - level
	if k < 0 {
		return math.NaN(), math.NaN()
	}
	if k > 0 {
		lo = GammaQuantile(alpha/2, float64(k))
	}
	return lo, GammaQuantile(1-alpha/2, float64(k+1))
}

// WilsonInterval is Wilson's score interval for a proportion of x in n.
func WilsonInterval(x, n int, level float64) (lo, hi float64) {
	if n <= 0 || x < 0 || x > n {
		return math.NaN(), math.NaN()
	}
	z := NormalQuantile(1 - (1-level)/2)
	nf, p := float64(n), float64(x)/float64(n)
	z2 := mul(z, z)
	center := (p + z2/mul(2, nf)) / (1 + z2/nf)
	half := mul(z/(1+z2/nf), math.Sqrt(mul(p, 1-p)/nf+z2/mul(mul(4, nf), nf)))
	return math.Max(0, center-half), math.Min(1, center+half)
}

// ClopperPearsonInterval is Clopper and Pearson's exact interval for a proportion of x in n.
func ClopperPearsonInterval(x, n int, level float64) (lo, hi float64) {
	if n <= 0 || x < 0 || x > n {
		return math.NaN(), math.NaN()
	}
	alpha := 1 - level
	lo, hi = 0, 1
	if x > 0 {
		lo = BetaQuantile(alpha/2, float64(x), float64(n-x+1))
	}
	if x < n {
		hi = BetaQuantile(1-alpha/2, float64(x+1), float64(n-x))
	}
	return lo, hi
}

// RateRatio is the ratio of two rates, a over exposure ea and b over eb, with its exact
// conditional interval. With no events in either, the ratio is NaN and the interval 0 to +Inf.
func RateRatio(a int, ea float64, b int, eb float64, level float64) (ratio, lo, hi float64) {
	if a < 0 || b < 0 || !(ea > 0) || !(eb > 0) {
		return math.NaN(), math.NaN(), math.NaN()
	}
	if a+b == 0 {
		return math.NaN(), 0, math.Inf(1)
	}
	ratio = (float64(a) / ea) / (float64(b) / eb)
	if b == 0 {
		ratio = math.Inf(1)
	}
	pl, pu := ClopperPearsonInterval(a, a+b, level)
	odds := func(p float64) float64 {
		if p >= 1 {
			return math.Inf(1)
		}
		return p / (1 - p) * (eb / ea)
	}
	return ratio, odds(pl), odds(pu)
}

// ProportionRatio is the ratio of two proportions, x1 of n1 over x2 of n2, with Koopman's score
// interval. With neither proportion above 0, the ratio is NaN and the interval 0 to +Inf.
func ProportionRatio(x1, n1, x2, n2 int, level float64) (ratio, lo, hi float64) {
	if n1 <= 0 || n2 <= 0 || x1 < 0 || x2 < 0 || x1 > n1 || x2 > n2 {
		return math.NaN(), math.NaN(), math.NaN()
	}
	if x1 == 0 && x2 == 0 {
		return math.NaN(), 0, math.Inf(1)
	}
	X1, N1, X2, N2 := float64(x1), float64(n1), float64(x2), float64(n2)
	ratio = (X1 / N1) / (X2 / N2)
	if x2 == 0 {
		ratio = math.Inf(1)
	}
	z := NormalQuantile(1 - (1-level)/2)
	critical := mul(z, z)
	// The Pearson chi-square at the proportions most likely when p1 = θ p2: p2 the smaller root of
	// θ(n1 + n2) p² − (θ(n1 + x2) + x1 + n2) p + (x1 + x2) = 0.
	chi2 := func(theta float64) float64 {
		a := mul(theta, N1+N2)
		b := -(mul(theta, N1+X2) + X1 + N2)
		c := X1 + X2
		disc := math.Max(0, mul(b, b)-mul(mul(4, a), c))
		// The smaller root, written to avoid cancellation.
		p2 := mul(2, c) / (-b + math.Sqrt(disc))
		// At p2 = 1/θ (all of the first sample counted) rounding may put p1 a little above 1.
		p1 := math.Min(1, mul(theta, p2))
		term := func(x, n, p float64) float64 {
			switch {
			case p >= 1 && x == n, p <= 0 && x == 0:
				return 0
			case p >= 1, p <= 0:
				return math.Inf(1)
			}
			d := x - mul(n, p)
			return mul(d, d) / mul(mul(n, p), 1-p)
		}
		return term(X1, N1, p1) + term(X2, N2, p2)
	}
	// In log θ the statistic falls to 0 at the estimate and rises on either side of it (with x1 = 0
	// the estimate is θ = 0 and it only rises; with x2 = 0, θ = +Inf and it only falls). Each bound
	// is where it crosses the critical value, found within θ of 10^±26 of the estimate, else 0 or
	// +Inf.
	g := func(t float64) float64 { return chi2(dmath.Exp(t)) }
	const far = 60.0
	lo, hi = 0, math.Inf(1)
	if x1 > 0 {
		from, to := -far, far
		if x2 > 0 {
			from, to = dmath.Log(ratio)-far, dmath.Log(ratio)
		}
		if g(from) > critical {
			lo = dmath.Exp(solve(func(t float64) float64 { return -g(t) }, -critical, from, to))
		}
	}
	if x2 > 0 {
		from, to := -far, far
		if x1 > 0 {
			from, to = dmath.Log(ratio), dmath.Log(ratio)+far
		}
		if g(to) > critical {
			hi = dmath.Exp(solve(g, critical, from, to))
		}
	}
	return ratio, lo, hi
}

// StudentTTwoSided is the two-sided p-value of a t statistic with df degrees of freedom: the
// chance of a |T| at least |t|, I_{df/(df+t²)}(df/2, 1/2).
func StudentTTwoSided(t, df float64) float64 {
	switch {
	case math.IsNaN(t) || !(df > 0):
		return math.NaN()
	case math.IsInf(t, 0):
		return 0
	}
	return BetaCDF(df/(df+mul(t, t)), df/2, 0.5)
}

// StudentTQuantile is the p-quantile of Student's t distribution with df degrees of freedom.
func StudentTQuantile(p, df float64) float64 {
	switch {
	case !(df > 0) || !(p >= 0 && p <= 1):
		return math.NaN()
	case p == 0.5:
		return 0
	case p < 0.5:
		return -StudentTQuantile(1-p, df)
	case p == 1:
		return math.Inf(1)
	}
	// |T| beyond t has chance 2(1 − p): x = df/(df + t²) is that beta quantile.
	x := BetaQuantile(2*(1-p), df/2, 0.5)
	return math.Sqrt(df * (1 - x) / x)
}
