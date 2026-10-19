package cohort

// Linear models of many features at once, with their variances moderated by empirical Bayes
// (wave 7, slice 7), and the steps that make pseudobulk counts ready for them. Each is written
// here from its published description:
//
//   - filtering by expression: Chen Y, Lun ATL, Smyth GK. From reads to genes to pathways.
//     F1000Research 2016;5:1438 (a gene kept with a count-per-million above min.count over the
//     median library size in the smallest group's number of samples, and a total count);
//   - TMM normalization: Robinson MD, Oshlack A. A scaling normalization method for differential
//     expression analysis of RNA-seq data. Genome Biol 2010;11:R25;
//   - voom: Law CW, Chen Y, Shi W, Smyth GK. voom: precision weights unlock linear model analysis
//     tools for RNA-seq read counts. Genome Biol 2014;15:R29, its trend by lowess (Cleveland WS.
//     Robust locally weighted regression and smoothing scatterplots. JASA 1979;74:829–836);
//   - the moderated t: Smyth GK. Linear models and empirical Bayes methods for assessing
//     differential expression in microarray experiments. Stat Appl Genet Mol Biol 2004;3:3 (the
//     prior's degrees of freedom and scale by moments of the log variances, the trigamma function
//     inverted by Newton's method as its appendix gives).

import (
	"errors"
	"math"
	"sort"

	"situcraft/internal/dmath"
	"situcraft/internal/stats"
)

// --- Special functions ------------------------------------------------------------------------------

// digamma is ψ(x) for x > 0: the recurrence up to 20, then its asymptotic series.
func digamma(x float64) float64 {
	var s float64
	for x < 20 {
		s -= 1 / x
		x++
	}
	r := 1 / mul(x, x)
	return s + dmath.Log(x) - 0.5/x - mul(r, 1.0/12-mul(r, 1.0/120-mul(r, 1.0/252-mul(r, 1.0/240-mul(r, 1.0/132)))))
}

// trigamma is ψ′(x) for x > 0.
func trigamma(x float64) float64 {
	var s float64
	for x < 20 {
		s += 1 / mul(x, x)
		x++
	}
	r := 1 / mul(x, x)
	return s + 1/x + mul(r, 0.5) + mul(r/x, 1.0/6-mul(r, 1.0/30-mul(r, 1.0/42-mul(r, 1.0/30-mul(r, 5.0/66)))))
}

// tetragamma is ψ″(x) for x > 0.
func tetragamma(x float64) float64 {
	var s float64
	for x < 20 {
		s -= 2 / mul(mul(x, x), x)
		x++
	}
	r := 1 / mul(x, x)
	return s - r - r/x - mul(mul(r, r), 0.5-mul(r, 1.0/6-mul(r, 1.0/6-mul(r, 3.0/10-mul(r, 5.0/6)))))
}

// trigammaInverse solves ψ′(y) = x by Newton's method (Smyth 2004, appendix).
func trigammaInverse(x float64) float64 {
	switch {
	case x > 1e7:
		return 1 / math.Sqrt(x)
	case x < 1e-6:
		return 1 / x
	}
	y := 0.5 + 1/x
	for it := 0; it < 50; it++ {
		tri := trigamma(y)
		dif := mul(tri, 1-tri/x) / tetragamma(y)
		y += dif
		if -dif/y < 1e-8 {
			break
		}
	}
	return y
}

// --- Lowess ----------------------------------------------------------------------------------------

// lowess smooths y on x (Cleveland 1979): at each x a line fitted to its nearest fraction f of the
// points, weighted by the tricube of their distance, and again (iter times) with each point's
// residual's bisquare robustness weight; points within delta of a fitted one are interpolated.
// x must be sorted. Answers the smoothed values at x.
func lowess(x, y []float64, f float64, iter int, delta float64) []float64 {
	n := len(x)
	ys := make([]float64, n)
	if n < 2 {
		copy(ys, y)
		return ys
	}
	ns := int(mul(f, float64(n)) + 1e-7)
	if ns > n {
		ns = n
	}
	if ns < 2 {
		ns = 2
	}
	rw := make([]float64, n)
	res := make([]float64, n)
	w := make([]float64, n)
	span := x[n-1] - x[0]
	for it := 1; it <= iter+1; it++ {
		nleft, nright, last, i := 0, ns-1, -1, 0
		for {
			if nright < n-1 {
				if d1, d2 := x[i]-x[nleft], x[nright+1]-x[i]; d1 > d2 {
					nleft++
					nright++
					continue
				}
			}
			v, ok := lowest(x, y, x[i], nleft, nright, w, it > 1, rw, span)
			if ok {
				ys[i] = v
			} else {
				ys[i] = y[i]
			}
			if last < i-1 {
				denom := x[i] - x[last]
				for j := last + 1; j < i; j++ {
					alpha := (x[j] - x[last]) / denom
					ys[j] = mul(alpha, ys[i]) + mul(1-alpha, ys[last])
				}
			}
			last = i
			cut := x[last] + delta
			for i = last + 1; i < n; i++ {
				if x[i] > cut {
					break
				}
				if x[i] == x[last] {
					ys[i] = ys[last]
					last = i
				}
			}
			if i-1 > last+1 {
				i = i - 1
			} else {
				i = last + 1
			}
			if last >= n-1 {
				break
			}
		}
		for k := range res {
			res[k] = y[k] - ys[k]
		}
		if it > iter {
			break
		}
		var sc float64
		for k := range res {
			rw[k] = math.Abs(res[k])
			sc += rw[k]
		}
		sc /= float64(n)
		sorted := append([]float64(nil), rw...)
		sort.Float64s(sorted)
		m1, m2 := n/2, n/2
		if n%2 == 0 {
			m1 = n/2 - 1
		}
		cmad := mul(3, sorted[m1]+sorted[m2])
		if cmad < mul(1e-7, sc) {
			break
		}
		c9, c1 := mul(0.999, cmad), mul(0.001, cmad)
		for k := range rw {
			r := math.Abs(res[k])
			switch {
			case r <= c1:
				rw[k] = 1
			case r > c9:
				rw[k] = 0
			default:
				u := r / cmad
				rw[k] = mul(1-mul(u, u), 1-mul(u, u))
			}
		}
	}
	return ys
}

// lowest is lowess's local fit at xs over the window [nleft, nright] (and the points beyond it as
// near), with the robustness weights when robust.
func lowest(x, y []float64, xs float64, nleft, nright int, w []float64, robust bool, rw []float64, span float64) (float64, bool) {
	n := len(x)
	h := math.Max(xs-x[nleft], x[nright]-xs)
	h9, h1 := mul(0.999, h), mul(0.001, h)
	var a float64
	j := nleft
	for j < n {
		w[j] = 0
		r := math.Abs(x[j] - xs)
		if r <= h9 {
			if r <= h1 {
				w[j] = 1
			} else {
				u := r / h
				q := 1 - mul(mul(u, u), u)
				w[j] = mul(mul(q, q), q)
			}
			if robust {
				w[j] = mul(w[j], rw[j])
			}
			a += w[j]
		} else if x[j] > xs {
			break
		}
		j++
	}
	nrt := j - 1
	if a <= 0 {
		return 0, false
	}
	for k := nleft; k <= nrt; k++ {
		w[k] /= a
	}
	if h > 0 {
		var mean float64
		for k := nleft; k <= nrt; k++ {
			mean += mul(w[k], x[k])
		}
		b := xs - mean
		var c float64
		for k := nleft; k <= nrt; k++ {
			d := x[k] - mean
			c += mul(mul(w[k], d), d)
		}
		if math.Sqrt(c) > mul(0.001, span) {
			b /= c
			for k := nleft; k <= nrt; k++ {
				w[k] = mul(w[k], mul(b, x[k]-mean)+1)
			}
		}
	}
	var v float64
	for k := nleft; k <= nrt; k++ {
		v += mul(w[k], y[k])
	}
	return v, true
}

// interpolate answers a piecewise-linear function through (xs, ys) (xs sorted; ties averaged),
// constant beyond its ends.
func interpolate(xs, ys []float64) func(float64) float64 {
	var ux, uy []float64
	for i := 0; i < len(xs); {
		j := i
		var s float64
		for j < len(xs) && xs[j] == xs[i] {
			s += ys[j]
			j++
		}
		ux = append(ux, xs[i])
		uy = append(uy, s/float64(j-i))
		i = j
	}
	return func(v float64) float64 {
		switch {
		case len(ux) == 0:
			return math.NaN()
		case v <= ux[0]:
			return uy[0]
		case v >= ux[len(ux)-1]:
			return uy[len(uy)-1]
		}
		k := sort.SearchFloat64s(ux, v)
		if ux[k] == v {
			return uy[k]
		}
		t := (v - ux[k-1]) / (ux[k] - ux[k-1])
		return uy[k-1] + mul(t, uy[k]-uy[k-1])
	}
}

// lowessSpan is the span chosen for n points: 1 up to 50, else 0.3 + 0.7 (50/n)^(1/3) (a span that
// shrinks as the cube root of the points, as limma chooses one for voom's trend).
func lowessSpan(n int) float64 {
	if n <= 50 {
		return 1
	}
	return 0.3 + mul(0.7, dmath.Pow(50/float64(n), 1.0/3))
}

// --- Linear models of many features ----------------------------------------------------------------

// LinearFits are features' fits of one design: each feature's coefficients, its unscaled covariance
// (XᵀWX)⁻¹, residual standard deviation and degrees of freedom, and its mean value.
type LinearFits struct {
	Coef     [][]float64
	Unscaled [][][]float64
	Sigma    []float64
	DF       int
	Amean    []float64
	Fitted   [][]float64
}

// FitLinear fits each feature's values (y[g], one per row of X) by least squares, weighted by
// w[g] when w is given.
func FitLinear(y [][]float64, w [][]float64, X [][]float64) (*LinearFits, error) {
	n := len(X)
	if n == 0 {
		return nil, errors.New("cohort: a design without rows")
	}
	p := len(X[0])
	if n <= p {
		return nil, errors.New("cohort: no residual degrees of freedom")
	}
	G := len(y)
	out := &LinearFits{Coef: make([][]float64, G), Unscaled: make([][][]float64, G), Sigma: make([]float64, G), DF: n - p, Amean: make([]float64, G), Fitted: make([][]float64, G)}
	for g := range y {
		wt := func(i int) float64 {
			if w == nil {
				return 1
			}
			return w[g][i]
		}
		xtwx := make([][]float64, p)
		xtwy := make([]float64, p)
		for a := 0; a < p; a++ {
			xtwx[a] = make([]float64, p)
			for b := 0; b <= a; b++ {
				var s float64
				for i := 0; i < n; i++ {
					s += mul(mul(X[i][a], wt(i)), X[i][b])
				}
				xtwx[a][b], xtwx[b][a] = s, s
			}
			var s float64
			for i := 0; i < n; i++ {
				s += mul(mul(X[i][a], wt(i)), y[g][i])
			}
			xtwy[a] = s
		}
		beta, err := solveSPD(xtwx, xtwy)
		if err != nil {
			return nil, err
		}
		inv, err := invertSPD(xtwx)
		if err != nil {
			return nil, err
		}
		var rss, sum float64
		fitted := make([]float64, n)
		for i := 0; i < n; i++ {
			var e float64
			for a := 0; a < p; a++ {
				e += mul(X[i][a], beta[a])
			}
			fitted[i] = e
			r := y[g][i] - e
			rss += mul(mul(wt(i), r), r)
			sum += y[g][i]
		}
		out.Coef[g], out.Unscaled[g], out.Fitted[g] = beta, inv, fitted
		out.Sigma[g] = math.Sqrt(rss / float64(n-p))
		out.Amean[g] = sum / float64(n)
	}
	return out, nil
}

// Moderation is the empirical Bayes prior of the features' variances (its scale s0² and degrees of
// freedom d0, infinite when the variances agree beyond chance) and each feature's posterior
// variance and total degrees of freedom.
type Moderation struct {
	S2Prior float64   `json:"s2Prior"`
	DFPrior float64   `json:"dfPrior"`
	S2Post  []float64 `json:"-"`
	DFTotal float64   `json:"dfTotal"`
}

// Moderate fits a scaled inverse chi-square prior to the features' residual variances by the
// moments of their logs (Smyth 2004), and squeezes each toward it; when their logs vary no more
// than chance alone would vary them, every feature has the variances' mean.
func Moderate(f *LinearFits) Moderation {
	G := len(f.Sigma)
	d := float64(f.DF)
	s2 := make([]float64, G)
	for g, s := range f.Sigma {
		s2[g] = mul(s, s)
	}
	// Variances of 0 would have no logarithm: floored at a small fraction of the median.
	med := median(s2)
	if !(med > 0) {
		med = 1
	}
	var emean float64
	e := make([]float64, G)
	for g, v := range s2 {
		e[g] = dmath.Log(math.Max(v, mul(1e-5, med))) - digamma(d/2) + dmath.Log(d/2)
		emean += e[g]
	}
	emean /= float64(G)
	var evar float64
	for _, v := range e {
		evar += mul(v-emean, v-emean)
	}
	m := Moderation{S2Post: make([]float64, G)}
	if G == 1 {
		// One feature: nothing to learn its prior from.
		m.S2Prior, m.DFPrior, m.DFTotal = s2[0], 0, d
		m.S2Post[0] = s2[0]
		return m
	}
	if G > 1 {
		evar = evar/float64(G-1) - trigamma(d/2)
	} else {
		evar = 0
	}
	if evar > 0 {
		m.DFPrior = mul(2, trigammaInverse(evar))
		m.S2Prior = dmath.Exp(emean + digamma(m.DFPrior/2) - dmath.Log(m.DFPrior/2))
	} else {
		// The variances agree beyond chance: one variance of every feature, their mean.
		m.DFPrior = math.Inf(1)
		var sum float64
		for _, v := range s2 {
			sum += v
		}
		m.S2Prior = sum / float64(G)
	}
	m.DFTotal = math.Min(d+m.DFPrior, mul(d, float64(G)))
	for g, v := range s2 {
		if math.IsInf(m.DFPrior, 1) {
			m.S2Post[g] = m.S2Prior
		} else {
			m.S2Post[g] = (mul(m.DFPrior, m.S2Prior) + mul(d, v)) / (m.DFPrior + d)
		}
	}
	return m
}

// ModeratedTest is a feature's moderated t test of a contrast: its estimate, interval, t, p.
type ModeratedTest struct {
	Estimate, Lo, Hi, T, P float64
}

// TestContrast tests a contrast c of each feature's coefficients by its moderated t (each
// feature's own covariance: exact under its weights), with the interval at level.
func TestContrast(f *LinearFits, m Moderation, c []float64, level float64) []ModeratedTest {
	q := stats.StudentTQuantile(1-(1-level)/2, m.DFTotal)
	out := make([]ModeratedTest, len(f.Coef))
	for g := range f.Coef {
		var est, v float64
		for a := range c {
			est += mul(c[a], f.Coef[g][a])
			for b := range c {
				v += mul(mul(c[a], f.Unscaled[g][a][b]), c[b])
			}
		}
		se := math.Sqrt(mul(m.S2Post[g], v))
		t := est / se
		out[g] = ModeratedTest{Estimate: est, Lo: est - mul(q, se), Hi: est + mul(q, se), T: t, P: stats.StudentTTwoSided(t, m.DFTotal)}
	}
	return out
}

// --- Counts --------------------------------------------------------------------------------------

// FilterByExpression keeps the genes (rows of counts, a column a sample) expressed enough to test:
// a count-per-million at least minCount over the median library size (×10⁶) in the smallest
// group's number of samples (1 over the design's largest leverage; for groups above 10, 10 plus
// 70% of the rest), and a total count of at least minTotal.
func FilterByExpression(counts [][]float64, X [][]float64, minCount, minTotal float64) []bool {
	n := len(X)
	lib := make([]float64, n)
	for _, row := range counts {
		for i, v := range row {
			lib[i] += v
		}
	}
	cutoff := minCount / median(lib) * 1e6
	// The leverages: the diagonal of X (XᵀX)⁻¹ Xᵀ.
	p := len(X[0])
	xtx := make([][]float64, p)
	for a := 0; a < p; a++ {
		xtx[a] = make([]float64, p)
		for b := 0; b < p; b++ {
			for i := 0; i < n; i++ {
				xtx[a][b] += mul(X[i][a], X[i][b])
			}
		}
	}
	inv, err := invertSPD(xtx)
	minSize := 1.0
	if err == nil {
		var maxH float64
		for i := 0; i < n; i++ {
			var h float64
			for a := 0; a < p; a++ {
				for b := 0; b < p; b++ {
					h += mul(mul(X[i][a], inv[a][b]), X[i][b])
				}
			}
			maxH = math.Max(maxH, h)
		}
		minSize = 1 / maxH
	}
	if minSize > 10 {
		minSize = 10 + mul(minSize-10, 0.7)
	}
	keep := make([]bool, len(counts))
	for g, row := range counts {
		var total float64
		above := 0
		for i, v := range row {
			total += v
			if lib[i] > 0 && v/lib[i]*1e6 >= cutoff {
				above++
			}
		}
		keep[g] = float64(above) >= minSize-1e-14 && total >= minTotal-1e-14
	}
	return keep
}

// TMM computes the samples' normalization factors by trimmed means of M-values (Robinson and
// Oshlack 2010): each sample against the reference whose upper quartile of counts per million is
// nearest the samples' mean, the genes with a count in both, their log-ratios trimmed 30% from each
// end and their mean log-expressions 5%, weighted by their inverse binomial variances; the factors
// scaled to multiply to 1.
func TMM(counts [][]float64, lib []float64) []float64 {
	n := len(lib)
	var rows [][]float64
	for _, row := range counts {
		for _, v := range row {
			if v > 0 {
				rows = append(rows, row)
				break
			}
		}
	}
	factors := make([]float64, n)
	for i := range factors {
		factors[i] = 1
	}
	if len(rows) == 0 {
		return factors
	}
	// The reference: the upper quartile (R's type 7) nearest the mean.
	f75 := make([]float64, n)
	for i := 0; i < n; i++ {
		col := make([]float64, len(rows))
		for g, row := range rows {
			col[g] = row[i] / lib[i]
		}
		sort.Float64s(col)
		h := mul(0.75, float64(len(col)-1))
		lo := int(math.Floor(h))
		v := col[lo]
		if lo+1 < len(col) {
			v += mul(h-float64(lo), col[lo+1]-col[lo])
		}
		f75[i] = v
	}
	var mean float64
	for _, v := range f75 {
		mean += v
	}
	mean /= float64(n)
	ref := 0
	for i := 1; i < n; i++ {
		if math.Abs(f75[i]-mean) < math.Abs(f75[ref]-mean) {
			ref = i
		}
	}
	for i := 0; i < n; i++ {
		factors[i] = tmmPair(rows, i, ref, lib[i], lib[ref])
	}
	var logs float64
	for _, f := range factors {
		logs += dmath.Log(f)
	}
	g := dmath.Exp(logs / float64(n))
	for i := range factors {
		factors[i] /= g
	}
	return factors
}

// tmmPair is sample k's factor against sample r.
func tmmPair(rows [][]float64, k, r int, nk, nr float64) float64 {
	var logR, absE, v []float64
	for _, row := range rows {
		obs, ref := row[k], row[r]
		if obs <= 0 || ref <= 0 {
			continue
		}
		// M as the log of the ratio of the proportions (so that genes of the same ratio tie), A
		// the mean of their logs.
		logR = append(logR, log2((obs/nk)/(ref/nr)))
		absE = append(absE, (log2(obs/nk)+log2(ref/nr))/2)
		v = append(v, (nk-obs)/nk/obs+(nr-ref)/nr/ref)
	}
	n := len(logR)
	if n == 0 {
		return 1
	}
	var big float64
	for _, x := range logR {
		big = math.Max(big, math.Abs(x))
	}
	if big < 1e-6 {
		return 1
	}
	loL := math.Floor(mul(float64(n), 0.3)) + 1
	hiL := float64(n) + 1 - loL
	loS := math.Floor(mul(float64(n), 0.05)) + 1
	hiS := float64(n) + 1 - loS
	rR, rE := ranks(logR), ranks(absE)
	var num, den float64
	for g := range logR {
		if rR[g] >= loL && rR[g] <= hiL && rE[g] >= loS && rE[g] <= hiS {
			num += logR[g] / v[g]
			den += 1 / v[g]
		}
	}
	if den == 0 {
		return 1
	}
	return dmath.Exp(mul(num/den, math.Ln2))
}

// log2 is the base-2 logarithm, the same on every computer: the exponent exact, the logarithm of
// the fraction in [½, 1).
func log2(x float64) float64 {
	frac, exp := math.Frexp(x)
	if frac == 0.5 {
		return float64(exp - 1)
	}
	return mul(dmath.Log(frac), 1/math.Ln2) + float64(exp)
}

// ranks are values' ranks from 1, ties given their average rank.
func ranks(v []float64) []float64 {
	idx := make([]int, len(v))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return v[idx[a]] < v[idx[b]] })
	out := make([]float64, len(v))
	for i := 0; i < len(idx); {
		j := i
		for j < len(idx) && v[idx[j]] == v[idx[i]] {
			j++
		}
		r := float64(i+j+1) / 2
		for k := i; k < j; k++ {
			out[idx[k]] = r
		}
		i = j
	}
	return out
}

// VoomResult is voom's log counts per million and precision weights (genes × samples), with the
// effective library sizes and the trend's span.
type VoomResult struct {
	LogCPM  [][]float64
	Weights [][]float64
	Lib     []float64
	Span    float64
}

// Voom makes counts ready for linear models (Law et al. 2014): log2 counts per million of the
// effective library sizes (each count plus 0.5, each library plus 1); a lowess trend of the genes'
// square-root residual standard deviations against their mean log counts; each observation weighted
// by the inverse of the trend's variance at its fitted log count.
func Voom(counts [][]float64, lib []float64, X [][]float64) (*VoomResult, error) {
	n := len(lib)
	G := len(counts)
	if G < 2 {
		return nil, errors.New("cohort: voom needs two genes")
	}
	y := make([][]float64, G)
	for g, row := range counts {
		y[g] = make([]float64, n)
		for i, v := range row {
			y[g][i] = log2((v + 0.5) / (lib[i] + 1) * 1e6)
		}
	}
	fit, err := FitLinear(y, nil, X)
	if err != nil {
		return nil, err
	}
	var meanLog float64
	for _, l := range lib {
		meanLog += log2(l + 1)
	}
	meanLog /= float64(n)
	type pt struct{ x, y float64 }
	pts := make([]pt, G)
	for g := range pts {
		pts[g] = pt{fit.Amean[g] + meanLog - log2(1e6), math.Sqrt(fit.Sigma[g])}
	}
	sort.SliceStable(pts, func(a, b int) bool { return pts[a].x < pts[b].x })
	sx, sy := make([]float64, G), make([]float64, G)
	for g, p := range pts {
		sx[g], sy[g] = p.x, p.y
	}
	span := lowessSpan(G)
	trend := interpolate(sx, lowess(sx, sy, span, 3, mul(0.01, sx[G-1]-sx[0])))
	w := make([][]float64, G)
	for g := range w {
		w[g] = make([]float64, n)
		for i := range w[g] {
			fitted := fit.Fitted[g][i] + log2(lib[i]+1) - log2(1e6)
			s := trend(fitted)
			w[g][i] = 1 / mul(mul(s, s), mul(s, s))
		}
	}
	return &VoomResult{LogCPM: y, Weights: w, Lib: lib, Span: span}, nil
}

// CountsFit is a table of counts (genes × samples) fitted for testing: the genes kept by
// expression, the kept genes' library sizes and TMM factors, voom's values and weights, each gene's
// weighted fit and the variances' moderation.
type CountsFit struct {
	Keep     []bool
	Lib      []float64
	Factors  []float64
	Voom     *VoomResult
	Fit      *LinearFits
	Moderate Moderation
}

// FitCounts filters, normalizes, weighs and fits counts (genes × samples) by the design X: genes
// expressed in enough samples (FilterByExpression, a count of 10 and a total of 15), TMM's
// effective library sizes, voom's weights, each gene's weighted least squares, the moderation.
func FitCounts(counts [][]float64, X [][]float64) (*CountsFit, error) {
	keep := FilterByExpression(counts, X, 10, 15)
	var kept [][]float64
	for g, k := range keep {
		if k {
			kept = append(kept, counts[g])
		}
	}
	if len(kept) < 2 {
		return nil, errors.New("cohort: fewer than two genes expressed enough to test")
	}
	n := len(X)
	lib := make([]float64, n)
	for _, row := range kept {
		for i, v := range row {
			lib[i] += v
		}
	}
	factors := TMM(kept, lib)
	eff := make([]float64, n)
	for i := range eff {
		eff[i] = mul(lib[i], factors[i])
	}
	v, err := Voom(kept, eff, X)
	if err != nil {
		return nil, err
	}
	fit, err := FitLinear(v.LogCPM, v.Weights, X)
	if err != nil {
		return nil, err
	}
	return &CountsFit{Keep: keep, Lib: lib, Factors: factors, Voom: v, Fit: fit, Moderate: Moderate(fit)}, nil
}
