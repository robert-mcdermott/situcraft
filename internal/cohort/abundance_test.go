package cohort

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/dmath"
	"situcraft/internal/random"
)

// handCohort is six samples of two labels (the second in none of them: untested), three in each of
// two groups, the last two the one subject's technical replicates; with niches.
func handCohort() (*Document, ComparisonDesign) {
	d := &Document{Labels: LabelSet{Name: "Types", Names: []string{"T cell", "Neuron"}}, Niches: &NichesDoc{Niches: 2}}
	counts := []int{20, 30, 25, 40, 22, 21}
	labeled := []int{200, 210, 190, 205, 100, 110}
	for i := range counts {
		d.Samples = append(d.Samples, SampleDoc{Sample: string(rune('a' + i)), Name: string(rune('A' + i)),
			Summary: Summary{Labeled: labeled[i], Counts: []int{counts[i], 0}, Tissue: float64(1e5 + 1e4*i)},
			Niches:  []int{counts[i] * 2, labeled[i] - counts[i]*2}, NicheCells: labeled[i]})
	}
	design := ComparisonDesign{Names: []string{"Intercept", "group[T.b]"},
		Rows: []DesignRow{
			{Unit: "1", Group: "a", Samples: []string{"a"}, X: []float64{1, 0}},
			{Unit: "2", Group: "a", Samples: []string{"b"}, X: []float64{1, 0}},
			{Unit: "3", Group: "a", Samples: []string{"c"}, X: []float64{1, 0}},
			{Unit: "4", Group: "b", Samples: []string{"d"}, X: []float64{1, 1}},
			{Unit: "5", Group: "b", Samples: []string{"e", "f"}, X: []float64{1, 1}},
		},
		Contrasts: []Contrast{{Name: "b − a", Vector: []float64{0, 1}}}}
	return d, design
}

// Worked by hand: each row's counts its samples' summed (the subject's two replicates one row), the
// share test the model's own, the density test's offset the tissue in mm², the untested label said
// so and left out of the adjustment, the files written and checked.
func TestAbundanceByHand(t *testing.T) {
	d, design := handCohort()
	c, err := Abundance(d, design, []string{FamilyShare, FamilyDensity, FamilyNiche}, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	share, density, niche := c.Families[0], c.Families[1], c.Families[2]
	if len(share.Counts) != 2 || share.Counts[0][4] != 43 || share.Totals[4] != 210 || math.Abs(share.Values[0][4]-43.0/210) > 1e-15 {
		t.Errorf("rows: %v %v", share.Counts, share.Totals)
	}
	X := [][]float64{{1, 0}, {1, 0}, {1, 0}, {1, 1}, {1, 1}}
	fit, err := FitQuasi(Binomial, X, []float64{20, 30, 25, 40, 43}, []float64{200, 210, 190, 205, 210}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := fit.Test([]float64{0, 1}, 0.95)
	got := share.Tests[0]
	if !got.Tested || got.Estimate != want.Estimate || got.P != want.P || got.PAdj != want.P || math.Abs(got.Ratio-math.Exp(want.Estimate)) > 1e-12 || got.DF != 3 {
		t.Errorf("the share's test %+v, the model's %+v", got, want)
	}
	// The second label: no cells anywhere, untested, outside the adjustment.
	if share.Tests[1].Tested || !strings.Contains(share.Tests[1].Note, "no sample") || share.Untested != 1 || !strings.Contains(share.Universe, "1 test (2 labels × 1 contrast, 1 untested)") {
		t.Errorf("untested: %+v; %s", share.Tests[1], share.Universe)
	}
	// Densities: per mm², the tissue summed in the subject's row.
	if math.Abs(density.Totals[4]-(1.4e5+1.5e5)/1e6) > 1e-12 || math.Abs(density.Values[0][0]-20/0.1) > 1e-9 {
		t.Errorf("densities: %v %v", density.Totals, density.Values[0])
	}
	pfit, _ := FitQuasi(Poisson, X, []float64{20, 30, 25, 40, 43}, nil, logs(density.Totals))
	if math.Abs(density.Tests[0].Estimate-pfit.Test([]float64{0, 1}, 0.95).Estimate) > 1e-12 {
		t.Errorf("the density's test %+v", density.Tests[0])
	}
	if len(niche.Labels) != 2 || niche.Labels[0] != "niche 1" || niche.Counts[0][0] != 40 || niche.Totals[0] != 200 {
		t.Errorf("niches: %v %v %v", niche.Labels, niche.Counts, niche.Totals)
	}
	// Written, read back and checked; the results table the document's.
	dir := t.TempDir()
	c.Summary = SummaryRef{ID: "coh-x", Name: "a summary"}
	if err := WriteComparison(dir, c); err != nil {
		t.Fatal(err)
	}
	m := &artifacts.Manifest{}
	if f := CheckComparison(dir, m); len(f) != 0 {
		t.Errorf("findings %+v", f)
	}
	if !bytes.Contains(m.Summary, []byte(`"summary":"coh-x"`)) || !bytes.Contains(ResultsCSV(c), []byte("share,Neuron,b − a,false")) {
		t.Errorf("summary %s", m.Summary)
	}
}

func logs(v []float64) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = dmath.Log(x)
	}
	return out
}

func TestAbundanceRefuses(t *testing.T) {
	d, design := handCohort()
	bad := func(change func(*ComparisonDesign), want string) {
		g := design
		g.Rows = append([]DesignRow(nil), design.Rows...)
		change(&g)
		if _, err := Abundance(d, g, []string{FamilyShare}, 0.95); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
	bad(func(g *ComparisonDesign) { g.Rows = g.Rows[:2] }, "no degrees of freedom")
	bad(func(g *ComparisonDesign) { g.Rows[0] = DesignRow{Samples: []string{"z"}, X: []float64{1, 0}} }, "not in the summary")
	bad(func(g *ComparisonDesign) { g.Rows[1] = DesignRow{Samples: []string{"a"}, X: []float64{1, 0}} }, "in two rows")
	bad(func(g *ComparisonDesign) { g.Contrasts = []Contrast{{Name: "x", Vector: []float64{1}}} }, "has 1 values")
	// Tests of cells, not replicates: a group of one unit, a unit twice in a group, one group.
	bad(func(g *ComparisonDesign) {
		g.Rows = append(g.Rows[:3], DesignRow{Unit: "4", Group: "b", Samples: []string{"d", "e", "f"}, X: []float64{1, 1}})
	}, "b has 1 replicate unit: a group needs at least two")
	bad(func(g *ComparisonDesign) { g.Rows[4].Unit = "4" }, "the unit 4 is in two rows of b")
	bad(func(g *ComparisonDesign) {
		for k := range g.Rows {
			g.Rows[k].Group = "a"
		}
	}, "every row is in one group")
	if _, err := Abundance(d, design, []string{"mass"}, 0.95); err == nil {
		t.Error("an unknown family accepted")
	}
	// Regions of one section as different units: refused, unless the design says they are
	// pseudo-samples (a teaching example's).
	d.Samples[0].Dataset, d.Samples[3].Dataset = "d-1", "d-1"
	if _, err := Abundance(d, design, []string{FamilyShare}, 0.95); err == nil || !strings.Contains(err.Error(), "A and D are regions of one section given different units (1, 4)") {
		t.Errorf("regions of one section: %v", err)
	}
	design.PseudoSamples = true
	if _, err := Abundance(d, design, []string{FamilyShare}, 0.95); err != nil {
		t.Errorf("pseudo-samples: %v", err)
	}
	design.PseudoSamples = false
	d.Samples[3].Dataset = "d-2"
	if _, err := Abundance(d, design, []string{FamilyShare}, 0.95); err != nil {
		t.Errorf("two sections: %v", err)
	}
	if _, err := Abundance(d, design, []string{FamilyShare}, 1.5); err == nil {
		t.Error("a level of 1.5 accepted")
	}
}

// Calibration on null tables: four samples against four, every sample's share drawn about the same
// value with more variation than binomial (beta-binomial), and every density about the same with
// more than Poisson (negative binomial, over areas that vary threefold). Of 4,000 null tables each,
// the tests reject at 5% and 1% about as often as they should: within 0.015 and 0.006 (over four
// standard errors).
func TestAbundanceCalibration(t *testing.T) {
	s := random.New(20261019, 7)
	X := [][]float64{{1, 0}, {1, 0}, {1, 0}, {1, 0}, {1, 1}, {1, 1}, {1, 1}, {1, 1}}
	c := []float64{0, 1}
	const reps = 4000
	var share5, share1, dens5, dens1, tests int
	for r := 0; r < reps; r++ {
		y, n := make([]float64, 8), make([]float64, 8)
		yd, off := make([]float64, 8), make([]float64, 8)
		for i := range y {
			n[i] = float64(500 + s.IntN(400))
			a, b := s.Gamma(0.2*150, 1), s.Gamma(0.8*150, 1)
			y[i] = float64(s.Binomial(int(n[i]), a/(a+b)))
			area := 0.05 + 0.1*s.Float64()
			yd[i] = float64(s.NegativeBinomial(area*600, 20))
			off[i] = dmath.Log(area)
		}
		if f, err := FitQuasi(Binomial, X, y, n, nil); err == nil && f.Converged {
			p := f.Test(c, 0.95).P
			share5 += btoi(p < 0.05)
			share1 += btoi(p < 0.01)
			tests++
		}
		if f, err := FitQuasi(Poisson, X, yd, nil, off); err == nil && f.Converged {
			p := f.Test(c, 0.95).P
			dens5 += btoi(p < 0.05)
			dens1 += btoi(p < 0.01)
		}
	}
	rate := func(k int) float64 { return float64(k) / reps }
	t.Logf("null rejections: shares %.4f at 5%%, %.4f at 1%%; densities %.4f, %.4f (%d tables)", rate(share5), rate(share1), rate(dens5), rate(dens1), tests)
	if tests != reps || math.Abs(rate(share5)-0.05) > 0.015 || math.Abs(rate(share1)-0.01) > 0.006 || math.Abs(rate(dens5)-0.05) > 0.015 || math.Abs(rate(dens1)-0.01) > 0.006 {
		t.Errorf("rejections off their levels")
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
