package cohort

// Quasi-likelihood generalized linear models (wave 7, slice 6), for counts by sample: a label's
// cells out of the sample's labeled cells (quasi-binomial, logit link) or out of its tissue's area
// (quasi-Poisson, log link, the area an offset). Fitted by iteratively reweighted least squares as
// R's glm and statsmodels' GLM fit them (McCullagh and Nelder, Generalized Linear Models, 1989),
// starting where R starts; the dispersion is Pearson's χ² over the residual degrees of freedom, and
// a coefficient's or contrast's test is a t test on those degrees of freedom.

import (
	"errors"
	"math"

	"situcraft/internal/dmath"
	"situcraft/internal/stats"
)

// Family is a model's family: binomial (y of n) or Poisson (y counts).
type Family int

// The families.
const (
	Binomial Family = iota
	Poisson
)

// GLMFit is a fitted model: its coefficients, the unscaled covariance (XᵀWX)⁻¹, the dispersion and
// residual degrees of freedom, the deviance, and whether the iterations converged.
type GLMFit struct {
	Beta       []float64
	Unscaled   [][]float64
	Dispersion float64
	DF         int
	Deviance   float64
	Iterations int
	Converged  bool
	// Fitted are the means: each row's share (binomial) or count (Poisson).
	Fitted []float64
}

// glmTolerance is the relative change in deviance the iterations stop at (R's is 1e-8).
const glmTolerance = 1e-12

func logistic(eta float64) float64 {
	if eta >= 0 {
		return 1 / (1 + dmath.Exp(-eta))
	}
	e := dmath.Exp(eta)
	return e / (1 + e)
}

// xlogy is x·log(x/y), 0 where x is 0.
func xlogy(x, y float64) float64 {
	if x == 0 {
		return 0
	}
	return mul(x, dmath.Log(x/y))
}

// FitQuasi fits a model of y on the rows of X: binomial (y of n trials) with the logit link, or
// Poisson with the log link and an offset (each row's, added to its linear predictor). X has a row
// for each count; it must have full column rank, and more rows than columns.
func FitQuasi(fam Family, X [][]float64, y, n, offset []float64) (*GLMFit, error) {
	rows := len(X)
	if rows == 0 || len(y) != rows {
		return nil, errors.New("cohort: a count for every row of the design")
	}
	p := len(X[0])
	if rows <= p {
		return nil, errors.New("cohort: no residual degrees of freedom")
	}
	if fam == Binomial && len(n) != rows {
		return nil, errors.New("cohort: a total for every count")
	}
	off := func(i int) float64 {
		if offset == nil {
			return 0
		}
		return offset[i]
	}
	// R's starting means: (n·share + 0.5)/(n + 1) for a binomial, y + 0.1 for a Poisson.
	eta := make([]float64, rows)
	mu := make([]float64, rows)
	for i := range eta {
		switch fam {
		case Binomial:
			if !(n[i] > 0) || y[i] < 0 || y[i] > n[i] {
				return nil, errors.New("cohort: a count out of its total")
			}
			mu[i] = (y[i] + 0.5) / (n[i] + 1)
			eta[i] = dmath.Log(mu[i] / (1 - mu[i]))
		case Poisson:
			if y[i] < 0 {
				return nil, errors.New("cohort: a negative count")
			}
			mu[i] = y[i] + 0.1
			eta[i] = dmath.Log(mu[i]) - off(i)
		}
	}
	deviance := func() float64 {
		var d float64
		for i := range mu {
			switch fam {
			case Binomial:
				d += xlogy(y[i], mul(n[i], mu[i])) + xlogy(n[i]-y[i], mul(n[i], 1-mu[i]))
			case Poisson:
				d += xlogy(y[i], mu[i]) - (y[i] - mu[i])
			}
		}
		return 2 * d
	}
	fit := &GLMFit{Beta: make([]float64, p), DF: rows - p}
	old := math.Inf(1)
	w := make([]float64, rows)
	z := make([]float64, rows)
	var xtwx [][]float64
	for it := 1; it <= 100; it++ {
		for i := range w {
			switch fam {
			case Binomial:
				v := mul(mu[i], 1-mu[i])
				w[i] = mul(n[i], v)
				z[i] = eta[i] + (y[i]/n[i]-mu[i])/v
			case Poisson:
				w[i] = mu[i]
				z[i] = eta[i] + (y[i]-mu[i])/mu[i]
			}
		}
		// Weighted least squares: (XᵀWX) β = XᵀWz.
		xtwx = make([][]float64, p)
		xtwz := make([]float64, p)
		for a := 0; a < p; a++ {
			xtwx[a] = make([]float64, p)
			for b := 0; b <= a; b++ {
				var s float64
				for i := 0; i < rows; i++ {
					s += mul(mul(X[i][a], w[i]), X[i][b])
				}
				xtwx[a][b] = s
				xtwx[b][a] = s
			}
			var s float64
			for i := 0; i < rows; i++ {
				s += mul(mul(X[i][a], w[i]), z[i])
			}
			xtwz[a] = s
		}
		beta, err := solveSPD(xtwx, xtwz)
		if err != nil {
			return nil, err
		}
		fit.Beta = beta
		for i := 0; i < rows; i++ {
			var e float64
			for a := 0; a < p; a++ {
				e += mul(X[i][a], beta[a])
			}
			eta[i] = e
			switch fam {
			case Binomial:
				mu[i] = logistic(e)
			case Poisson:
				mu[i] = dmath.Exp(e + off(i))
			}
		}
		dev := deviance()
		fit.Iterations = it
		if math.Abs(dev-old)/(math.Abs(dev)+0.1) < glmTolerance {
			fit.Converged = true
			fit.Deviance = dev
			break
		}
		old = dev
		fit.Deviance = dev
	}
	// The covariance at the solution's weights, and the dispersion.
	for i := range w {
		switch fam {
		case Binomial:
			w[i] = mul(n[i], mul(mu[i], 1-mu[i]))
		case Poisson:
			w[i] = mu[i]
		}
	}
	for a := 0; a < p; a++ {
		for b := 0; b <= a; b++ {
			var s float64
			for i := 0; i < rows; i++ {
				s += mul(mul(X[i][a], w[i]), X[i][b])
			}
			xtwx[a][b] = s
			xtwx[b][a] = s
		}
	}
	inv, err := invertSPD(xtwx)
	if err != nil {
		return nil, err
	}
	fit.Unscaled = inv
	var chi2 float64
	for i := range mu {
		switch fam {
		case Binomial:
			d := y[i] - mul(n[i], mu[i])
			chi2 += mul(d, d) / mul(n[i], mul(mu[i], 1-mu[i]))
		case Poisson:
			d := y[i] - mu[i]
			chi2 += mul(d, d) / mu[i]
		}
	}
	fit.Dispersion = chi2 / float64(fit.DF)
	fit.Fitted = mu
	for _, b := range fit.Beta {
		if math.IsNaN(b) || math.Abs(b) > 30 {
			fit.Converged = false
		}
	}
	return fit, nil
}

// ContrastTest is a contrast's estimate (on the link's scale), its standard error, t statistic,
// two-sided p-value and confidence interval (at the level asked).
type ContrastTest struct {
	Estimate float64 `json:"estimate"`
	SE       float64 `json:"se"`
	T        float64 `json:"t"`
	P        float64 `json:"p"`
	Lo       float64 `json:"lo"`
	Hi       float64 `json:"hi"`
}

// Test tests a contrast c of the coefficients: c·β over √(φ cᵀ(XᵀWX)⁻¹c), on the residual degrees
// of freedom.
func (f *GLMFit) Test(c []float64, level float64) ContrastTest {
	var est, v float64
	for a := range c {
		est += mul(c[a], f.Beta[a])
		for b := range c {
			v += mul(mul(c[a], f.Unscaled[a][b]), c[b])
		}
	}
	se := math.Sqrt(mul(f.Dispersion, v))
	t := est / se
	q := stats.StudentTQuantile(1-(1-level)/2, float64(f.DF))
	out := ContrastTest{Estimate: est, SE: se, T: t, P: stats.StudentTTwoSided(t, float64(f.DF)), Lo: est - mul(q, se), Hi: est + mul(q, se)}
	if se == 0 {
		out.T, out.P = math.NaN(), math.NaN()
	}
	return out
}

// solveSPD solves A x = b for a symmetric positive definite A, by Cholesky's factorization.
func solveSPD(a [][]float64, b []float64) ([]float64, error) {
	l, err := cholesky(a)
	if err != nil {
		return nil, err
	}
	n := len(b)
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		s := b[i]
		for k := 0; k < i; k++ {
			s -= mul(l[i][k], y[k])
		}
		y[i] = s / l[i][i]
	}
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		s := y[i]
		for k := i + 1; k < n; k++ {
			s -= mul(l[k][i], x[k])
		}
		x[i] = s / l[i][i]
	}
	return x, nil
}

// invertSPD inverts a symmetric positive definite matrix, column by column.
func invertSPD(a [][]float64) ([][]float64, error) {
	n := len(a)
	inv := make([][]float64, n)
	for i := range inv {
		inv[i] = make([]float64, n)
	}
	for j := 0; j < n; j++ {
		e := make([]float64, n)
		e[j] = 1
		col, err := solveSPD(a, e)
		if err != nil {
			return nil, err
		}
		for i := range col {
			inv[i][j] = col[i]
		}
	}
	return inv, nil
}

func cholesky(a [][]float64) ([][]float64, error) {
	n := len(a)
	l := make([][]float64, n)
	for i := range l {
		l[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			s := a[i][j]
			for k := 0; k < j; k++ {
				s -= mul(l[i][k], l[j][k])
			}
			if i == j {
				if !(s > 0) {
					return nil, errors.New("cohort: the design's columns are not independent at these weights")
				}
				l[i][i] = math.Sqrt(s)
			} else {
				l[i][j] = s / l[j][j]
			}
		}
	}
	return l, nil
}
