package refine

// Each conditional update against the distribution it should draw from (research.md §3.2): the
// conjugate ones against their analytic posteriors (Kolmogorov–Smirnov), the components against
// probabilities computed independently (χ²), the shapes' augmented chain against the posterior
// computed by numerical integration. Fixed seeds; each test at the 0.001 level, so it always
// passes or always fails.

import (
	"fmt"
	"math"
	"testing"

	"situcraft/internal/random"
	"situcraft/internal/stats"
)

const (
	alpha = 1e-3
	draws = 6000
)

// ksGamma tests draws against Gamma(shape, rate).
func ksGamma(t *testing.T, name string, x []float64, shape, rate float64) {
	t.Helper()
	if d, p := stats.KS(x, func(v float64) float64 { return stats.GammaP(shape, v*rate) }); p < alpha {
		t.Errorf("%s: Kolmogorov–Smirnov D %.4f, p %.2g against Gamma(%g, %g)", name, d, p, shape, rate)
	}
}

// fixture is a small model with fixed statistics and parameters: 2 components, 3 genes, 5 cells.
func fixture(t *testing.T) *Model {
	t.Helper()
	h := DefaultHyper()
	h.Components = 2
	m, err := NewModel(h, []float64{40, 75, 22, 130, 60}, 3, 5000, 7)
	if err != nil {
		t.Fatal(err)
	}
	m.Workers = 3
	copy(m.Counts, []uint32{3, 0, 9, 1, 1, 2, 0, 4, 0, 12, 0, 5, 2, 2, 2})
	copy(m.BgCounts, []uint32{4, 11, 0})
	copy(m.Comp, []int{0, 1, 0, 1, 1})
	copy(m.Shape, []float64{1.5, 3, 0.7, 2.2, 5, 1.1})
	copy(m.RateParam, []float64{30, 80, 12, 45, 25, 200})
	copy(m.Scale, []float64{0.03, 0.02, 0.01})
	copy(m.Pi, []float64{0.35, 0.65})
	copy(m.VolMean, []float64{3.4, 4.3})
	copy(m.VolVar, []float64{0.3, 0.15})
	for c := 0; c < m.Cells; c++ {
		var sum float64
		for g := 0; g < m.Genes; g++ {
			m.Rate[c*m.Genes+g] = 0.01 * float64(1+c+g)
			sum += m.Rate[c*m.Genes+g]
		}
		m.RateSum[c] = sum
	}
	copy(m.Bg, []float64{0.002, 0.004, 0.001})
	return m
}

// repeat runs an update once per sweep, collecting a value after each.
func repeat(m *Model, update func(), value func() float64) []float64 {
	out := make([]float64, draws)
	for i := range out {
		m.Sweep = i
		update()
		out[i] = value()
	}
	return out
}

func TestBackgroundIsItsGammaPosterior(t *testing.T) {
	m := fixture(t)
	for g := 0; g < m.Genes; g++ {
		x := repeat(m, m.UpdateBackground, func() float64 { return m.Bg[g] })
		ksGamma(t, fmt.Sprintf("background rate of gene %d", g), x, m.H.BackgroundA+float64(m.BgCounts[g]), m.H.BackgroundBUm+m.Region)
	}
}

func TestRatesAreTheirGammaPosteriors(t *testing.T) {
	m := fixture(t)
	for _, cg := range [][2]int{{0, 0}, {1, 1}, {3, 0}, {4, 2}} {
		c, g := cg[0], cg[1]
		k := m.Comp[c]
		x := repeat(m, m.UpdateRates, func() float64 { return m.Rate[c*m.Genes+g] })
		ksGamma(t, fmt.Sprintf("rate of cell %d, gene %d", c, g), x, m.Shape[k*m.Genes+g]+float64(m.Counts[c*m.Genes+g]), m.RateParam[k*m.Genes+g]+m.Volume[c])
	}
	// The sums follow the rates.
	for c := 0; c < m.Cells; c++ {
		var sum float64
		for g := 0; g < m.Genes; g++ {
			sum += m.Rate[c*m.Genes+g]
		}
		if sum != m.RateSum[c] {
			t.Errorf("cell %d: rate sum %g, rates %g", c, m.RateSum[c], sum)
		}
	}
}

func TestRateParamsAreTheirGammaPosteriors(t *testing.T) {
	m := fixture(t)
	for k := 0; k < 2; k++ {
		for g := 0; g < m.Genes; g++ {
			var n, sum float64
			for c, ck := range m.Comp {
				if ck == k {
					n++
					sum += m.Rate[c*m.Genes+g]
				}
			}
			x := repeat(m, m.UpdateRateParams, func() float64 { return m.RateParam[k*m.Genes+g] })
			ksGamma(t, fmt.Sprintf("rate parameter of component %d, gene %d", k, g), x, m.H.RateA+n*m.Shape[k*m.Genes+g], m.Scale[g]+sum)
		}
	}
}

func TestScalesAreTheirGammaPosteriors(t *testing.T) {
	m := fixture(t)
	for g := 0; g < m.Genes; g++ {
		x := repeat(m, m.UpdateScales, func() float64 { return m.Scale[g] })
		ksGamma(t, fmt.Sprintf("scale of gene %d", g), x, m.H.ScaleE+2*m.H.RateA, m.H.ScaleFUm+m.RateParam[g]+m.RateParam[m.Genes+g])
	}
}

func TestProportionsAreTheirDirichletPosterior(t *testing.T) {
	m := fixture(t)
	// Component 0 has 2 cells, component 1 has 3: π₀ ~ Beta(α + 2, α + 3).
	x := repeat(m, m.UpdateProportions, func() float64 { return m.Pi[0] })
	if d, p := stats.KS(x, func(v float64) float64 { return stats.BetaCDF(v, m.H.Alpha+2, m.H.Alpha+3) }); p < alpha {
		t.Errorf("proportion: D %.4f, p %.2g", d, p)
	}
}

func TestVolumesAreTheirNormalInverseGammaPosterior(t *testing.T) {
	m := fixture(t)
	h := m.H
	for k := 0; k < 2; k++ {
		var y []float64
		for c, ck := range m.Comp {
			if ck == k {
				y = append(y, math.Log(m.Volume[c]))
			}
		}
		n := float64(len(y))
		var mean, ss float64
		for _, v := range y {
			mean += v / n
		}
		for _, v := range y {
			ss += (v - mean) * (v - mean)
		}
		kappa := h.VolumeKappa + n
		mn := (h.VolumeKappa*h.VolumeMean + n*mean) / kappa
		a := h.VolumeA + n/2
		b := h.VolumeB + ss/2 + h.VolumeKappa*n*(mean-h.VolumeMean)*(mean-h.VolumeMean)/(2*kappa)
		variance := make([]float64, draws)
		standard := make([]float64, draws)
		for i := range variance {
			m.Sweep = i
			m.UpdateVolumes()
			variance[i] = m.VolVar[k]
			standard[i] = (m.VolMean[k] - mn) / math.Sqrt(m.VolVar[k]/kappa)
		}
		// s² is inverse gamma: P(s² ≤ x) = Q(a, b/x).
		if d, p := stats.KS(variance, func(x float64) float64 { return stats.GammaQ(a, b/x) }); p < alpha {
			t.Errorf("component %d's volume variance: D %.4f, p %.2g", k, d, p)
		}
		if d, p := stats.KS(standard, stats.NormalCDF); p < alpha {
			t.Errorf("component %d's volume mean given its variance: D %.4f, p %.2g", k, d, p)
		}
	}
	// Exact mode keeps the prior's means.
	m.H.Exact = true
	m.UpdateVolumes()
	if m.VolMean[1] != h.VolumeMean || m.VolVar[1] != h.VolumeB/(h.VolumeA-1) {
		t.Errorf("exact mode: %g, %g", m.VolMean[1], m.VolVar[1])
	}
}

// The components' probabilities, computed here independently: π_k LogNormal(V) Π NB(n; mean
// rV/β, dispersion r).
func TestComponentsAreTheirCollapsedPosterior(t *testing.T) {
	m := fixture(t)
	m.H.Components = 3
	m.Shape = []float64{1.5, 3, 0.7, 2.2, 5, 1.1, 0.9, 0.9, 0.9}
	m.RateParam = []float64{30, 80, 12, 45, 25, 200, 20, 20, 20}
	m.Pi = []float64{0.3, 0.5, 0.2}
	m.VolMean = []float64{3.4, 4.3, 3.9}
	m.VolVar = []float64{0.3, 0.15, 0.5}
	for _, c := range []int{0, 3} {
		v := m.Volume[c]
		p := make([]float64, 3)
		var total float64
		for k := range p {
			y := math.Log(v)
			w := m.Pi[k] * math.Exp(-(y-m.VolMean[k])*(y-m.VolMean[k])/(2*m.VolVar[k])) / (v * math.Sqrt(2*math.Pi*m.VolVar[k]))
			for g := 0; g < m.Genes; g++ {
				r, beta := m.Shape[k*m.Genes+g], m.RateParam[k*m.Genes+g]
				w *= stats.NegativeBinomialPMF(int(m.Counts[c*m.Genes+g]), r*v/beta, r)
			}
			p[k] = w
			total += w
		}
		counts := make([]float64, 3)
		for i := 0; i < draws; i++ {
			m.Sweep = i
			m.UpdateComponents()
			counts[m.Comp[c]]++
		}
		expected := make([]float64, 3)
		for k := range p {
			expected[k] = p[k] / total * draws
		}
		if stat, df, pv := stats.ChiSquare(counts, expected, 0); pv < alpha {
			t.Errorf("cell %d's component: χ² %.1f on %d degrees of freedom, p %.2g (drawn %v, expected %v)", c, stat, df, pv, counts, expected)
		}
	}
}

// The shapes' augmented chain leaves the shape's posterior invariant: independent chains, each
// started from the prior and run long, end distributed as the posterior
// p(r | n) ∝ Gamma(r; a_r, b_r) Π_c NB(n_c; r, V_c/(V_c + β)), computed by numerical integration.
func TestShapesReachTheirPosterior(t *testing.T) {
	h := DefaultHyper()
	h.Components = 1
	volumes := []float64{40, 75, 22, 130, 60, 18}
	counts := []uint32{0, 3, 1, 7, 2, 0}
	const beta = 50.0
	m, err := NewModel(h, volumes, 1, 1000, 11)
	if err != nil {
		t.Fatal(err)
	}
	copy(m.Counts, counts)
	m.RateParam[0] = beta
	// The posterior on a grid.
	const top, steps = 80.0, 80000
	logp := func(r float64) float64 {
		v := (h.ShapeA-1)*math.Log(r) - h.ShapeB*r
		for c, n := range counts {
			lg1, _ := math.Lgamma(float64(n) + r)
			lg2, _ := math.Lgamma(r)
			v += lg1 - lg2 + r*math.Log(beta/(volumes[c]+beta))
		}
		return v
	}
	grid := make([]float64, steps+1)
	cdf := make([]float64, steps+1)
	peak := math.Inf(-1)
	dens := make([]float64, steps+1)
	for i := 1; i <= steps; i++ {
		grid[i] = top * float64(i) / steps
		dens[i] = logp(grid[i])
		peak = math.Max(peak, dens[i])
	}
	for i := 1; i <= steps; i++ {
		dens[i] = math.Exp(dens[i] - peak)
		cdf[i] = cdf[i-1] + (dens[i]+dens[i-1])/2*(grid[i]-grid[i-1])
	}
	for i := range cdf {
		cdf[i] /= cdf[steps]
	}
	posterior := func(r float64) float64 {
		i := int(r / top * steps)
		if i >= steps {
			return 1
		}
		f := (r - grid[i]) / (grid[i+1] - grid[i])
		return cdf[i] + f*(cdf[i+1]-cdf[i])
	}
	chains := 3000
	x := make([]float64, chains)
	for j := range x {
		m.Seed = uint64(1000 + j)
		m.Shape[0] = random.New(5, uint64(j)).Gamma(h.ShapeA, 1/h.ShapeB)
		for it := 0; it < 60; it++ {
			m.Sweep = it
			m.UpdateShapes()
		}
		x[j] = m.Shape[0]
	}
	if d, p := stats.KS(x, posterior); p < alpha {
		t.Errorf("shape: D %.4f, p %.2g against its numerical posterior", d, p)
	}
}

// The same results on any number of workers.
func TestSweepsAreTheSameOnAnyWorkers(t *testing.T) {
	run := func(workers int) *Model {
		m := fixture(t)
		m.Workers = workers
		for i := 0; i < 20; i++ {
			m.SweepParameters()
		}
		return m
	}
	a, b := run(1), run(8)
	for name, pair := range map[string][2][]float64{
		"rates": {a.Rate, b.Rate}, "shapes": {a.Shape, b.Shape}, "rate parameters": {a.RateParam, b.RateParam},
		"scales": {a.Scale, b.Scale}, "proportions": {a.Pi, b.Pi}, "volume means": {a.VolMean, b.VolMean}, "background": {a.Bg, b.Bg},
	} {
		for i := range pair[0] {
			if pair[0][i] != pair[1][i] {
				t.Fatalf("%s differ at %d: %g and %g", name, i, pair[0][i], pair[1][i])
			}
		}
	}
	for c := range a.Comp {
		if a.Comp[c] != b.Comp[c] {
			t.Fatalf("cell %d's component differs", c)
		}
	}
}
