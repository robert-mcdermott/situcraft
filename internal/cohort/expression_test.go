package cohort

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/random"
)

// exprCohort is handCohort's six samples (five rows, the last two samples one subject's) with a
// pseudobulk of 60 genes: the T cells' counts negative binomial, the first gene's three times as
// high in group b; the second label in only two rows with 10 cells or more.
func exprCohort() (*Document, [][][]uint32, ComparisonDesign) {
	d, design := handCohort()
	s := random.New(20261019, 11)
	G := 60
	for g := 0; g < G; g++ {
		d.Genes = append(d.Genes, fmt.Sprintf("G%02d", g+1))
	}
	base := make([]float64, G)
	for g := range base {
		base[g] = s.Gamma(1.5, 40)
	}
	pb := make([][][]uint32, len(d.Samples))
	neurons := []int{12, 3, 5, 30, 4, 5}
	for i := range d.Samples {
		d.Samples[i].Counts = []int{d.Samples[i].Counts[0], neurons[i]}
		lib := 0.6 + 0.2*float64(i)
		pb[i] = make([][]uint32, 2)
		for k := range pb[i] {
			pb[i][k] = make([]uint32, G)
			for g := range pb[i][k] {
				m := base[g] * lib
				if k == 0 && g == 0 && i >= 3 {
					m *= 3
				}
				if k == 1 {
					m *= float64(neurons[i]) / 20
				}
				pb[i][k][g] = uint32(s.NegativeBinomial(m, 20))
			}
		}
	}
	return d, pb, design
}

// Worked by hand: each row's counts its samples' summed; the T cells' tests FitCounts' own on the
// rows' counts; the neurons, in two rows with 10 of them, untested and said so; the adjustment
// over the tested label's genes; the files written and checked; a gene's values by row.
func TestExpressionByHand(t *testing.T) {
	d, pb, design := exprCohort()
	e, err := CompareExpression(d, pb, design, 0.95, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Genes) != 60 || e.MinCells != MinLabelCells {
		t.Fatalf("genes %d, min cells %d", len(e.Genes), e.MinCells)
	}
	counts, cells := RowCounts(d, pb, design, 0, SharedGenes(d, pb))
	if counts[0][4] != float64(pb[4][0][0]+pb[5][0][0]) || cells[4] != 43 {
		t.Errorf("the subject's row: %v cells %v", counts[0], cells)
	}
	X := [][]float64{{1, 0}, {1, 0}, {1, 0}, {1, 1}, {1, 1}}
	fit, err := FitCounts(counts, X)
	if err != nil {
		t.Fatal(err)
	}
	want := TestContrast(fit.Fit, fit.Moderate, []float64{0, 1}, 0.95)
	tc := e.Results[0]
	if !tc.Tested || len(tc.Rows) != 5 || len(tc.Genes) != len(want) || tc.Contrasts[0].LogFC[0] != want[0].Estimate || tc.Contrasts[0].P[3] != want[3].P {
		t.Fatalf("the T cells' tests: %+v", tc)
	}
	if tc.Contrasts[0].LogFC[0] < 1 || tc.Contrasts[0].Lo[0] > math.Log2(3) || tc.Contrasts[0].Hi[0] < math.Log2(3) {
		t.Errorf("the first gene's log fold %.3f (%.3f to %.3f), its truth %.3f", tc.Contrasts[0].LogFC[0], tc.Contrasts[0].Lo[0], tc.Contrasts[0].Hi[0], math.Log2(3))
	}
	n := e.Results[1]
	if n.Tested || !strings.Contains(n.Note, "2 rows have 10 of its cells or more: too few for the design's 2 columns") || fmt.Sprint(n.Rows) != "[0 3]" || n.DFPrior != nil {
		t.Errorf("the neurons: %+v", n)
	}
	if e.Tests != len(tc.Genes) || !strings.Contains(e.Universe, "(the genes tested in 1 label × 1 contrast; 1 label untested)") {
		t.Errorf("tests %d: %s", e.Tests, e.Universe)
	}
	// The adjustment: the lowest p's adjusted p is it times the tests.
	low := 0
	for g, p := range tc.Contrasts[0].P {
		if p < tc.Contrasts[0].P[low] {
			low = g
		}
	}
	if q := tc.Contrasts[0].PAdj[low]; math.Abs(q-math.Min(1, tc.Contrasts[0].P[low]*float64(e.Tests))) > 1e-12*q && q > tc.Contrasts[0].PAdj[0] {
		t.Errorf("the lowest p's adjusted %g", q)
	}
	dir := t.TempDir()
	e.Summary = SummaryRef{ID: "coh-x", Name: "a summary"}
	if err := WriteExpression(dir, e); err != nil {
		t.Fatal(err)
	}
	m := &artifacts.Manifest{}
	if f := CheckExpression(dir, m); len(f) != 0 {
		t.Errorf("findings %+v", f)
	}
	if !bytes.Contains(m.Summary, []byte(`"untested":["Neuron"]`)) || !bytes.Contains(ExpressionResultsCSV(e), []byte("Neuron,,,false")) {
		t.Errorf("summary %s", m.Summary)
	}
	back, err := ReadExpression(dir)
	if err != nil || back.Results[0].Contrasts[0].LogFC[0] != tc.Contrasts[0].LogFC[0] {
		t.Fatalf("read back: %v", err)
	}
	v, err := back.ValuesOf(d, pb, "T cell", "G01")
	if err != nil {
		t.Fatal(err)
	}
	for i := range v.Rows {
		if math.Abs(v.LogCPM[i]-fit.Voom.LogCPM[0][i]) > 1e-12 || v.Counts[i] != counts[0][i] {
			t.Errorf("row %d: %v, voom's %v", i, v.LogCPM[i], fit.Voom.LogCPM[0][i])
		}
	}
	if _, err := back.ValuesOf(d, pb, "Neuron", "G01"); err == nil {
		t.Error("an untested label's values")
	}
}

// A gene on one panel only is left out: the genes compared are those every panel has.
func TestExpressionSharedGenes(t *testing.T) {
	d, pb, design := exprCohort()
	pb[2][0][5], pb[2][1][5] = Missing, Missing
	e, err := CompareExpression(d, pb, design, 0.95, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Genes) != 59 || strings.Contains(strings.Join(e.Genes, ","), "G06") {
		t.Errorf("genes %v", e.Genes)
	}
	d.Panels = &PanelsDoc{Shared: []string{"G01", "G02", "G03", "G04", "G05", "G07", "G08"}}
	if e, err = CompareExpression(d, pb, design, 0.95, 0); err != nil || len(e.Genes) != 7 {
		t.Errorf("by the panels: %v %v", err, e)
	}
}

// Calibration on null tables: four rows against four, 200 genes of negative binomial counts (each
// gene its own mean and dispersion, each row its own library size), no difference. Of 40 tables,
// the moderated tests reject at 5% and 1% about as often as they should.
func TestExpressionCalibration(t *testing.T) {
	s := random.New(20261019, 13)
	X := make([][]float64, 8)
	for i := range X {
		X[i] = []float64{1, float64(i / 4)}
	}
	var n, at5, at1 int
	for table := 0; table < 40; table++ {
		lib := make([]float64, 8)
		for i := range lib {
			lib[i] = s.LogNormal(0, 0.3)
		}
		counts := make([][]float64, 200)
		for g := range counts {
			mean, size := s.Gamma(0.8, 100), 1/s.Gamma(4, 0.03)
			counts[g] = make([]float64, 8)
			for i := range counts[g] {
				counts[g][i] = float64(s.NegativeBinomial(mean*lib[i], size))
			}
		}
		f, err := FitCounts(counts, X)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range TestContrast(f.Fit, f.Moderate, []float64{0, 1}, 0.95) {
			n++
			if r.P < 0.05 {
				at5++
			}
			if r.P < 0.01 {
				at1++
			}
		}
	}
	r5, r1 := float64(at5)/float64(n), float64(at1)/float64(n)
	t.Logf("%d tests: %.4f below 0.05, %.4f below 0.01", n, r5, r1)
	if math.Abs(r5-0.05) > 0.015 || math.Abs(r1-0.01) > 0.006 {
		t.Errorf("rejections %.4f at 5%%, %.4f at 1%%", r5, r1)
	}
}

// spatialCohort is handCohort with each sample's statistics: two enrichment pairs (the second
// missing from the third sample: its row has none), a distance, a gene's Moran's I; niches' shares.
func spatialCohort() (*Document, ComparisonDesign) {
	d, design := handCohort()
	s := random.New(20261019, 17)
	for i := range d.Samples {
		b := 1.0
		if i >= 3 {
			b = 2
		}
		st := []Statistic{
			{"enrichment", "A|B", b * s.LogNormal(0, 0.1), "spa-1"},
			{"nearest", "A→B", 20 * s.LogNormal(0, 0.2), "spa-2"},
			{"moran", "G1", 0.3 + 0.05*s.Normal(), "spa-3"},
			{"graph", "pieces", 3, "gra-1"},
		}
		if i != 2 {
			st = append(st, Statistic{"enrichment", "A|A", s.LogNormal(0, 0.2), "spa-1"})
		}
		d.Samples[i].Statistics = st
		d.Samples[i].NicheShares = []float64{0.4 + 0.02*float64(i), 0.6 - 0.02*float64(i)}
	}
	return d, design
}

// Worked by hand: a row's value its samples' mean on the log2 scale; a statistic some row has none
// of untested; the family's tests FitLinear's and Moderate's own; ratios 2 to the estimates; the
// files written and checked.
func TestSpatialByHand(t *testing.T) {
	d, design := spatialCohort()
	if got := fmt.Sprint(SpatialFamiliesOf(d)); got != "[enrichment nearest niche moran]" {
		t.Errorf("families %s", got)
	}
	c, err := CompareSpatial(d, design, []string{FamilyEnrichment, FamilyNiche, FamilyMoran}, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	en := c.Families[0]
	if fmt.Sprint(en.Keys) != "[A|A A|B]" || en.Values[0][2] != nil || !en.Log {
		t.Fatalf("keys %v values %v", en.Keys, en.Values)
	}
	ab := func(i int) float64 { return d.Samples[i].Statistics[0].Value }
	if got, want := *en.Values[1][4], (log2(ab(4))+log2(ab(5)))/2; math.Abs(got-want) > 1e-15 {
		t.Errorf("the subject's row %g, its samples' mean %g", got, want)
	}
	if en.Tests[0].Tested || !strings.Contains(en.Tests[0].Note, "not in every row") || en.Untested != 1 {
		t.Errorf("A|A: %+v", en.Tests[0])
	}
	y := make([]float64, 5)
	for r := range y {
		y[r] = *en.Values[1][r]
	}
	X := [][]float64{{1, 0}, {1, 0}, {1, 0}, {1, 1}, {1, 1}}
	fit, _ := FitLinear([][]float64{y}, nil, X)
	want := TestContrast(fit, Moderate(fit), []float64{0, 1}, 0.95)[0]
	got := en.Tests[1]
	if !got.Tested || got.Estimate != want.Estimate || got.P != want.P || math.Abs(got.Ratio-math.Exp2(got.Estimate)) > 1e-12 || got.Ratio < 1.5 || got.RatioLo > 2 || got.RatioHi < 2 {
		t.Errorf("A|B: %+v, by hand %+v", got, want)
	}
	if *en.DFPrior != 0 || en.DFTotal != 3 {
		t.Errorf("one statistic tested: prior df %v, total %v", *en.DFPrior, en.DFTotal)
	}
	ni := c.Families[1]
	if ni.Log || fmt.Sprint(ni.Keys) != "[niche 1 niche 2]" || math.Abs(*ni.Values[0][4]-(0.48+0.50)/2) > 1e-15 || ni.Tests[0].Ratio != 0 {
		t.Errorf("niches: %+v", ni)
	}
	dir := t.TempDir()
	if err := WriteSpatialComparison(dir, c); err != nil {
		t.Fatal(err)
	}
	m := &artifacts.Manifest{}
	if f := CheckSpatialComparison(dir, m); len(f) != 0 {
		t.Errorf("findings %+v", f)
	}
	if !bytes.Contains(SpatialResultsCSV(c), []byte("enrichment,A|A,b − a,false")) {
		t.Errorf("results %s", SpatialResultsCSV(c))
	}
	if _, err := CompareSpatial(d, design, []string{"graph"}, 0.95); err == nil {
		t.Error("an unknown family accepted")
	}
	d.Samples[0].Statistics = nil
	if c, err := CompareSpatial(d, design, []string{FamilyMoran}, 0.95); err != nil || c.Families[0].Tests[0].Tested {
		t.Errorf("a statistic the first row lacks: %v %+v", err, c)
	}
}
