package stats

import (
	"math"
	"testing"

	"situcraft/internal/random"
)

// The intervals against SciPy 1.18.1's gamma and beta quantiles and exact binomial interval, and
// statsmodels 0.15.0's Wilson and Koopman intervals (confint_proportions_2indep, method "score",
// compare "ratio", without correction), computed once and written here.
func TestIntervalsAgainstReferences(t *testing.T) {
	rel := func(what string, got, want float64) {
		t.Helper()
		if math.IsInf(want, 1) && math.IsInf(got, 1) || want == 0 && got == 0 {
			return
		}
		if math.IsNaN(got) || math.Abs(got-want) > 1e-9*math.Max(1, math.Abs(want)) {
			t.Errorf("%s = %.15g, want %.15g", what, got, want)
		}
	}
	for _, c := range []struct {
		k      int
		lo, hi float64
	}{{0, 0, 3.688879454113936}, {1, 0.025317807984289876, 5.571643390938898}, {5, 1.6234863901184204, 11.66833207932267}, {10, 4.7953886961324335, 18.39035604201778}, {100, 81.36399125092314, 121.62679379242638}} {
		lo, hi := PoissonInterval(c.k, 0.95)
		rel("Garwood lower", lo, c.lo)
		rel("Garwood upper", hi, c.hi)
	}
	for _, c := range []struct {
		x, n                 int
		wlo, whi, cplo, cphi float64
	}{
		{0, 20, 0, 0.1611251580528194, 0, 0.16843347098308534},
		{5, 20, 0.11186170140766563, 0.4687008776187441, 0.08657146910143461, 0.49104587170795744},
		{20, 20, 0.8388748419471804, 0.9999999999999999, 0.8315665290169146, 1},
		{37, 412, 0.06585591872396812, 0.12133431247964654, 0.06402013233394667, 0.12165995381489203},
	} {
		lo, hi := WilsonInterval(c.x, c.n, 0.95)
		rel("Wilson lower", lo, c.wlo)
		rel("Wilson upper", hi, c.whi)
		lo, hi = ClopperPearsonInterval(c.x, c.n, 0.95)
		rel("Clopper–Pearson lower", lo, c.cplo)
		rel("Clopper–Pearson upper", hi, c.cphi)
	}
	for _, c := range []struct {
		a      int
		ea     float64
		b      int
		eb     float64
		lo, hi float64
	}{
		{30, 0.2, 12, 0.35, 2.1751854863587656, 9.38291984613858},
		{5, 1, 0, 2, 1.83271171463092, math.Inf(1)},
		{0, 1, 7, 0.5, 0, 0.34690699004822617},
		{120, 0.687, 410, 1.5, 0.5169941663047765, 0.7850425595860241},
	} {
		_, lo, hi := RateRatio(c.a, c.ea, c.b, c.eb, 0.95)
		rel("rate ratio lower", lo, c.lo)
		rel("rate ratio upper", hi, c.hi)
	}
	for _, c := range []struct {
		x1, n1, x2, n2 int
		lo, hi         float64
	}{
		{30, 100, 12, 120, 1.649483584895655, 5.531232977485758},
		{5, 40, 0, 60, 2.025388678143418, math.Inf(1)},
		{0, 40, 7, 50, 0, 0.6446329848541456},
		{37, 412, 120, 1530, 0.8042516747337246, 1.6199023961186179},
		{10, 10, 3, 9, 1.5484721382575848, 8.292986682436553},
	} {
		_, lo, hi := ProportionRatio(c.x1, c.n1, c.x2, c.n2, 0.95)
		rel("Koopman lower", lo, c.lo)
		rel("Koopman upper", hi, c.hi)
	}
}

// Coverage: Garwood's interval holds a Poisson mean at least 95% of the time; Koopman's interval
// holds the true ratio of proportions about 95% of the time.
func TestIntervalCoverage(t *testing.T) {
	s := random.New(17, 1)
	const reps = 4000
	for _, m := range []float64{0.7, 4, 30} {
		held := 0
		for i := 0; i < reps; i++ {
			lo, hi := PoissonInterval(s.Poisson(m), 0.95)
			if lo <= m && m <= hi {
				held++
			}
		}
		if f := float64(held) / reps; f < 0.95-0.01 {
			t.Errorf("Garwood's interval held a mean of %g %.3f of the time", m, f)
		}
	}
	held := 0
	const p1, p2, n1, n2 = 0.12, 0.08, 150, 200
	for i := 0; i < reps; i++ {
		x1, x2 := 0, 0
		for k := 0; k < n1; k++ {
			if s.Float64() < p1 {
				x1++
			}
		}
		for k := 0; k < n2; k++ {
			if s.Float64() < p2 {
				x2++
			}
		}
		_, lo, hi := ProportionRatio(x1, n1, x2, n2, 0.95)
		if lo <= p1/p2 && p1/p2 <= hi {
			held++
		}
	}
	if f := float64(held) / reps; math.Abs(f-0.95) > 0.015 {
		t.Errorf("Koopman's interval held the ratio %.3f of the time", f)
	}
}
