package validation

// The cohort tests' error rates (wave 7, slice 8; research.md §6.14). Many simulated cohorts, each
// sample's true cells made by the simulator (simulate.Cells: their true types the labels), each
// sample summarized as the summary job summarizes it (cohort.Summarize, its statistics from its
// Delaunay graph's enrichment, its nearest distances and Moran's I, with the cohort steps' graph and
// the analyses' own parameters where they shape the numbers), niches found across the samples
// together (cohort.Builder), and the subjects compared by condition as the page compares them:
// abundance, expression and spatial statistics.
//
//   - On null cohorts (the null preset's settings, a new seed each): each family's false-positive
//     rate at 5% and 1%, and how often a family finds anything below an adjusted p of 0.05 (which,
//     when nothing differs, is its false discovery rate).
//   - The same null cohorts tested cell by cell, as if each cell were a replicate: a label's share
//     by a two-proportion z-test of the groups' pooled cells, and a gene in a label by the Wilcoxon
//     rank-sum test of the groups' cells (each cell's counts normalized as the clustering
//     normalizes them). Their false positives are those pseudoreplication makes.
//   - On cohorts with the standard cohort's effects (a new seed each): how often each effect is
//     found (power); how often the expression effect's interval covers its true log2 fold (2.5,
//     the simulator's); and the false discovery proportion among the genes found.
//
// A few cohorts on every commit, to keep the study working; thousands weekly (the data workflow):
//
//	SITUCRAFT_COHORT_ERRORS=2000 go test ./validation/ -run CohortErrorRates -v -timeout 3h
//
// With SITUCRAFT_COHORT_ERRORS_REPORT set to a file, the results are written there as JSON.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"situcraft/internal/cohort"
	"situcraft/internal/dmath"
	"situcraft/internal/segment"
	"situcraft/internal/simulate"
	"situcraft/internal/spatial"
	"situcraft/internal/stats"
)

// cohortStudy is what a cohort's comparisons found: each family's tests (raw and adjusted p), the
// effects' tests, and the cell-by-cell tests' p-values.
type cohortStudy struct {
	families map[string][]studyTest
	effects  map[string]studyTest
	cells    map[string][]float64
}

type studyTest struct {
	key           string
	p, padj       float64
	estimate      float64
	lo, hi        float64
	found, higher bool
}

// The families compared: abundance's, expression's and the spatial statistics'.
var studyFamilies = []string{"share", "density", "niche share", "expression", "enrichment", "nearest", "moran"}

// The cell-by-cell tests.
var cellFamilies = []string{"share, cells as replicates", "expression, cells as replicates"}

// studyCohort simulates a cohort's sections, summarizes them and compares them by condition. A
// panel seed (not 0) keeps the cohort's panel that seed's: the standard cohort's, whose effect gene
// is among its type's most expressed, while its sections and their variation are the seed's.
func studyCohort(ctx context.Context, c simulate.Cohort, panelSeed uint64, cellTests bool) (*cohortStudy, error) {
	samples, params, err := simulate.CohortSamples(c)
	if err != nil {
		return nil, err
	}
	if panelSeed != 0 {
		for k := range params {
			params[k].PanelSeed = panelSeed
		}
	}
	types := make([]string, len(c.Section.Types))
	for k, t := range c.Section.Types {
		types[k] = t.Name
	}
	K := len(types)
	np := spatial.DefaultNiches()
	np.Seeds = 0
	b := cohort.NewBuilder(cohort.LabelSet{Name: "true types", Names: types}, &np)
	// For the cell-by-cell tests: each label's cells' normalized values by group (all samples'
	// cells pooled), and each group's labeled cells by label.
	type pooled struct {
		values [][][]float64 // label × gene × cells
		byType []int
		cells  int
	}
	var groups [2]pooled
	for g := range groups {
		groups[g].byType = make([]int, K)
	}
	var genes []string
	for k, s := range samples {
		tc, err := simulate.Cells(ctx, params[k], 1, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.Name, err)
		}
		n := len(tc.X)
		genes = tc.Counts.Genes
		cells := make([]segment.Cell, n)
		keep := make([]bool, n)
		rows := make([]uint32, n)
		for i := range cells {
			cells[i] = segment.Cell{ID: strconv.Itoa(i + 1), X: tc.X[i], Y: tc.Y[i]}
			for j := tc.Counts.Offsets[i]; j < tc.Counts.Offsets[i+1]; j++ {
				cells[i].Transcripts += tc.Counts.Count[j]
				cells[i].Genes++
			}
			keep[i], rows[i] = true, uint32(i)
		}
		area := mulf(params[k].WidthUm, params[k].HeightUm)
		sum, err := cohort.Summarize(cohort.Input{Cells: cells, Counts: tc.Counts, Keep: keep, Label: tc.Type, Tissue: area}, K)
		if err != nil {
			return nil, err
		}
		stat, err := sampleStatistics(ctx, tc, types)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.Name, err)
		}
		sd := cohort.SampleDoc{Sample: s.Name, Name: s.Name, Dataset: s.Name, PanelGenes: len(genes), Summary: *sum, Statistics: stat}
		panel := cohort.Panel{Name: "SituCraft simulated panel"}
		for _, gname := range genes {
			panel.Genes = append(panel.Genes, cohort.PanelGene{Name: gname})
		}
		b.Add(sd, sum.Pseudobulk, genes, panel, spatial.NicheCells{X: tc.X, Y: tc.Y, Label: tc.Type}, rows)
		if cellTests {
			g := &groups[groupIndex(c, s.Group)]
			if g.values == nil {
				g.values = make([][][]float64, K)
				for t := range g.values {
					g.values[t] = make([][]float64, len(genes))
				}
			}
			normalized(tc, func(cell int, value []float64) {
				t := tc.Type[cell]
				g.byType[t]++
				g.cells++
				for gi, v := range value {
					g.values[t][gi] = append(g.values[t][gi], v)
				}
			})
		}
	}
	built, err := b.Finish(ctx, 1, nil)
	if err != nil {
		return nil, err
	}
	doc := built.Doc
	design := cohort.ComparisonDesign{Names: []string{"Intercept", "condition[T." + c.Groups[1] + "]"}, Contrasts: []cohort.Contrast{{Name: c.Groups[1] + " − " + c.Groups[0], Vector: []float64{0, 1}}}}
	for _, s := range samples {
		x := []float64{1, 0}
		if s.Group == c.Groups[1] {
			x[1] = 1
		}
		design.Rows = append(design.Rows, cohort.DesignRow{Unit: s.Subject, Group: s.Group, Samples: []string{s.Name}, X: x})
	}
	out := &cohortStudy{families: map[string][]studyTest{}, effects: map[string]studyTest{}, cells: map[string][]float64{}}
	ab, err := cohort.Abundance(doc, design, []string{cohort.FamilyShare, cohort.FamilyDensity, cohort.FamilyNiche}, 0.95)
	if err != nil {
		return nil, fmt.Errorf("abundance: %w", err)
	}
	for _, f := range ab.Families {
		name := map[string]string{cohort.FamilyShare: "share", cohort.FamilyDensity: "density", cohort.FamilyNiche: "niche share"}[f.Family]
		for _, t := range f.Tests {
			if t.Tested {
				st := studyTest{key: t.Label, p: t.P, padj: t.PAdj, estimate: t.Estimate, lo: dmath.Log(t.Lo), hi: dmath.Log(t.Hi), found: t.PAdj < 0.05, higher: t.Ratio > 1}
				out.families[name] = append(out.families[name], st)
				if f.Family == cohort.FamilyShare && t.Label == "endothelial-like" {
					out.effects["endothelial-like share"] = st
				}
			}
		}
	}
	ex, err := cohort.CompareExpression(doc, built.Pseudobulk, design, 0.95, cohort.MinLabelCells)
	if err != nil {
		return nil, fmt.Errorf("expression: %w", err)
	}
	for _, r := range ex.Results {
		if !r.Tested {
			continue
		}
		c0 := r.Contrasts[0]
		for i, g := range r.Genes {
			st := studyTest{key: r.Label + " " + ex.Genes[g], p: c0.P[i], padj: c0.PAdj[i], estimate: c0.LogFC[i], lo: c0.Lo[i], hi: c0.Hi[i], found: c0.PAdj[i] < 0.05, higher: c0.LogFC[i] > 0}
			out.families["expression"] = append(out.families["expression"], st)
			if r.Label == "epithelial-like" && ex.Genes[g] == simulate.CohortGene {
				out.effects["SIM033 in epithelial-like"] = st
			}
		}
	}
	sp, err := cohort.CompareSpatial(doc, design, []string{cohort.FamilyEnrichment, cohort.FamilyNearest, cohort.FamilyMoran}, 0.95)
	if err != nil {
		return nil, fmt.Errorf("spatial statistics: %w", err)
	}
	for _, f := range sp.Families {
		name := f.Family
		for _, t := range f.Tests {
			if t.Tested {
				st := studyTest{key: t.Key, p: t.P, padj: t.PAdj, estimate: t.Estimate, lo: t.Lo, hi: t.Hi, found: t.PAdj < 0.05, higher: t.Estimate > 0}
				out.families[name] = append(out.families[name], st)
				if f.Family == cohort.FamilyEnrichment && t.Key == "epithelial-like|immune-like" {
					out.effects["epithelial–immune enrichment"] = st
				}
			}
		}
	}
	if cellTests {
		// A label's share: the groups' pooled labeled cells, each cell a replicate.
		for t := 0; t < K; t++ {
			x0, n0 := float64(groups[0].byType[t]), float64(groups[0].cells)
			x1, n1 := float64(groups[1].byType[t]), float64(groups[1].cells)
			pp := (x0 + x1) / (n0 + n1)
			se := math.Sqrt(mulf(mulf(pp, 1-pp), 1/n0+1/n1))
			if se > 0 {
				z := (x1/n1 - x0/n0) / se
				out.cells[cellFamilies[0]] = append(out.cells[cellFamilies[0]], 2*stats.NormalCDF(-math.Abs(z)))
			}
		}
		// A gene in a label: the Wilcoxon rank-sum test of the groups' cells of the label.
		for t := 0; t < K; t++ {
			for gi := range genes {
				a, bv := groups[0].values[t][gi], groups[1].values[t][gi]
				if p, ok := rankSum(a, bv); ok {
					out.cells[cellFamilies[1]] = append(out.cells[cellFamilies[1]], p)
				}
			}
		}
	}
	return out, nil
}

func mulf(a, b float64) float64 { return float64(a * b) }

func groupIndex(c simulate.Cohort, group string) int {
	if group == c.Groups[1] {
		return 1
	}
	return 0
}

// sampleStatistics are a sample's spatial numbers as the cohort steps' analyses give them: its
// Delaunay graph's enrichment of its labels, their nearest distances, and its genes' Moran's I.
// The parameters that shape these numbers are the analyses' defaults; the permutations, which
// give only their p-values, are the fewest each allows.
func sampleStatistics(ctx context.Context, tc *simulate.TrueCells, types []string) ([]cohort.Statistic, error) {
	K := len(types)
	g, err := spatial.Build(ctx, spatial.Inputs{X: tc.X, Y: tc.Y}, spatial.DefaultParams("delaunay"), 1, nil)
	if err != nil {
		return nil, err
	}
	label := make([]int32, len(g.Rows))
	for i, row := range g.Rows {
		label[i] = tc.Type[row]
	}
	ep := spatial.DefaultEnrichment()
	ep.Permutations = 100
	e, err := spatial.Enrich(ctx, g, label, K, nil, ep, 1, nil)
	if err != nil {
		return nil, err
	}
	out := cohort.EnrichmentStatistics("enrichment", types, e.Count, e.Expected)
	npar := spatial.DefaultNearest()
	npar.Permutations = 19
	n, err := spatial.NearestDistances(ctx, tc.X, tc.Y, tc.Type, K, npar, 1, nil)
	if err != nil {
		return nil, err
	}
	out = append(out, cohort.NearestStatistics("nearest", types, n.Median)...)
	expr, _, _, err := spatial.NormalizedExpression(tc.Counts, g.Rows)
	if err != nil {
		return nil, err
	}
	mp := spatial.DefaultMoran()
	mp.Permutations, mp.Local = 0, 0
	m, err := spatial.MoransI(ctx, g, expr, mp, 1, nil)
	if err != nil {
		return nil, err
	}
	out = append(out, cohort.MoranStatistics("moran", tc.Counts.Genes, m.I)...)
	return out, nil
}

// normalized gives each cell's genes' values as the clustering normalizes them: its counts scaled
// to the median total of the cells with a transcript, then log1p.
func normalized(tc *simulate.TrueCells, each func(cell int, value []float64)) {
	c := tc.Counts
	n := len(c.Offsets) - 1
	totals := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		var t float64
		for j := c.Offsets[i]; j < c.Offsets[i+1]; j++ {
			t += float64(c.Count[j])
		}
		if t > 0 {
			totals = append(totals, t)
		}
	}
	sort.Float64s(totals)
	target := 1.0
	if len(totals) > 0 {
		target = totals[len(totals)/2]
	}
	value := make([]float64, len(c.Genes))
	for i := 0; i < n; i++ {
		for g := range value {
			value[g] = 0
		}
		var t float64
		for j := c.Offsets[i]; j < c.Offsets[i+1]; j++ {
			t += float64(c.Count[j])
		}
		for j := c.Offsets[i]; j < c.Offsets[i+1]; j++ {
			value[c.Gene[j]] = dmath.Log(1 + float64(c.Count[j])/t*target)
		}
		each(i, value)
	}
}

// rankSum is the Wilcoxon rank-sum test's two-sided p of two samples (the normal approximation,
// its variance corrected for ties), as single-cell tools test genes; false when it cannot be made.
func rankSum(a, b []float64) (float64, bool) {
	n1, n2 := len(a), len(b)
	if n1 < 2 || n2 < 2 {
		return 0, false
	}
	type v struct {
		x float64
		g int
	}
	all := make([]v, 0, n1+n2)
	for _, x := range a {
		all = append(all, v{x, 0})
	}
	for _, x := range b {
		all = append(all, v{x, 1})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].x < all[j].x })
	N := float64(n1 + n2)
	var r1, ties float64
	for i := 0; i < len(all); {
		j := i
		for j < len(all) && all[j].x == all[i].x {
			j++
		}
		rank := float64(i+j+1) / 2
		t := float64(j - i)
		ties += t*t*t - t
		for k := i; k < j; k++ {
			if all[k].g == 0 {
				r1 += rank
			}
		}
		i = j
	}
	u := r1 - float64(n1)*float64(n1+1)/2
	mean := float64(n1) * float64(n2) / 2
	variance := float64(n1) * float64(n2) / 12 * (N + 1 - ties/(N*(N-1)))
	if !(variance > 0) {
		return 0, false
	}
	z := (u - mean) / math.Sqrt(variance)
	return 2 * stats.NormalCDF(-math.Abs(z)), true
}

// studyRate is a family's rejections over many cohorts: at 5% and 1%, their standard errors
// (the cohorts the clusters: a cohort's tests are not independent), and the cohorts finding
// anything below an adjusted p of 0.05.
type studyRate struct {
	Family   string  `json:"family"`
	Cohorts  int     `json:"cohorts"`
	Tests    int     `json:"tests"`
	At5      float64 `json:"at5"`
	SE5      float64 `json:"se5"`
	At1      float64 `json:"at1"`
	SE1      float64 `json:"se1"`
	AnyFound float64 `json:"anyFound"`
}

func rateOf(family string, ps [][]float64, padjs [][]float64) studyRate {
	r := studyRate{Family: family, Cohorts: len(ps)}
	ratio := func(level float64) (float64, float64) {
		var rej, n float64
		hits := make([]float64, len(ps))
		for i, p := range ps {
			for _, v := range p {
				if v < level {
					hits[i]++
				}
			}
			rej += hits[i]
			n += float64(len(p))
		}
		if n == 0 {
			return math.NaN(), math.NaN()
		}
		rate := rej / n
		var ss float64
		for i, p := range ps {
			d := hits[i] - rate*float64(len(p))
			ss += d * d
		}
		N := float64(len(ps))
		return rate, math.Sqrt(ss*N/(N-1)) / n
	}
	r.At5, r.SE5 = ratio(0.05)
	r.At1, r.SE1 = ratio(0.01)
	for _, p := range ps {
		r.Tests += len(p)
	}
	if padjs != nil {
		var any int
		for _, pa := range padjs {
			for _, v := range pa {
				if v < 0.05 {
					any++
					break
				}
			}
		}
		r.AnyFound = float64(any) / float64(len(padjs))
	}
	return r
}

// studyEffect is an effect over cohorts with it: how often found (adjusted p below 0.05, in its
// direction), and, for the expression effect, how often its interval covers its truth.
type studyEffect struct {
	Effect   string  `json:"effect"`
	Cohorts  int     `json:"cohorts"`
	Tested   int     `json:"tested"`
	Power    float64 `json:"power"`
	Mean     float64 `json:"meanEstimate"`
	Coverage float64 `json:"coverage,omitempty"`
	Truth    float64 `json:"truth,omitempty"`
}

// studyReport is the study's results.
type studyReport struct {
	NullCohorts   int           `json:"nullCohorts"`
	EffectCohorts int           `json:"effectCohorts"`
	Seconds       float64       `json:"seconds"`
	Null          []studyRate   `json:"null"`
	Cells         []studyRate   `json:"cellsAsReplicates"`
	Effects       []studyEffect `json:"effects"`
	// The expression tests' false discovery proportion with the effects, its mean over cohorts.
	EffectsFDR float64 `json:"effectsFdr"`
}

// runStudy studies n null cohorts and m cohorts with the standard cohort's effects, in parallel.
func runStudy(t *testing.T, n, m int) studyReport {
	ctx := context.Background()
	type job struct {
		k    int
		null bool
	}
	nullOut, effOut := make([]*cohortStudy, n), make([]*cohortStudy, m)
	errs := make(chan error, n+m)
	jobs := make(chan job)
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				var c simulate.Cohort
				var panel uint64
				if j.null {
					c = simulate.CohortPresets["null"]
					c.Seed = 20261100 + uint64(j.k)
				} else {
					c = simulate.CohortPresets["standard"]
					panel = c.Seed
					c.Seed = 20262100 + uint64(j.k)
				}
				r, err := studyCohort(ctx, c, panel, j.null)
				if err != nil {
					errs <- fmt.Errorf("cohort seed %d: %w", c.Seed, err)
					continue
				}
				if j.null {
					nullOut[j.k] = r
				} else {
					effOut[j.k] = r
				}
			}
		}()
	}
	for k := 0; k < n; k++ {
		jobs <- job{k, true}
	}
	for k := 0; k < m; k++ {
		jobs <- job{k, false}
	}
	close(jobs)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	rep := studyReport{NullCohorts: n, EffectCohorts: m, Seconds: time.Since(start).Seconds()}
	for _, fam := range studyFamilies {
		var ps, padjs [][]float64
		for _, r := range nullOut {
			var p, pa []float64
			for _, s := range r.families[fam] {
				p, pa = append(p, s.p), append(pa, s.padj)
			}
			ps, padjs = append(ps, p), append(padjs, pa)
		}
		rep.Null = append(rep.Null, rateOf(fam, ps, padjs))
	}
	for _, fam := range cellFamilies {
		var ps, padjs [][]float64
		for _, r := range nullOut {
			p := r.cells[fam]
			pa := make([]float64, len(p))
			stats.BenjaminiHochberg(p, pa)
			ps, padjs = append(ps, p), append(padjs, pa)
		}
		rep.Cells = append(rep.Cells, rateOf(fam, ps, padjs))
	}
	truth := math.Log2(2.5)
	for _, eff := range []string{"endothelial-like share", "SIM033 in epithelial-like", "epithelial–immune enrichment"} {
		e := studyEffect{Effect: eff, Cohorts: m}
		var found, covered int
		for _, r := range effOut {
			s, ok := r.effects[eff]
			if !ok {
				continue
			}
			e.Tested++
			if s.found && s.higher {
				found++
			}
			e.Mean += s.estimate
			if eff == "SIM033 in epithelial-like" && s.lo <= truth && truth <= s.hi {
				covered++
			}
		}
		// Found among the cohorts (an effect untested is not found); its estimate's mean and its
		// interval's coverage among those testing it.
		e.Power = float64(found) / float64(max(1, m))
		e.Mean /= float64(max(1, e.Tested))
		if eff == "SIM033 in epithelial-like" {
			e.Coverage, e.Truth = float64(covered)/float64(max(1, e.Tested)), truth
		}
		rep.Effects = append(rep.Effects, e)
	}
	for _, r := range effOut {
		var found, falses int
		for _, s := range r.families["expression"] {
			if s.found {
				found++
				if s.key != "epithelial-like "+simulate.CohortGene {
					falses++
				}
			}
		}
		if found > 0 {
			rep.EffectsFDR += float64(falses) / float64(found) / float64(max(1, m))
		}
	}
	return rep
}

func (r studyReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d null cohorts and %d with effects, in %.0f s\n", r.NullCohorts, r.EffectCohorts, r.Seconds)
	fmt.Fprintf(&b, "%-34s %8s %16s %16s %10s\n", "null: family", "tests", "at 5% (SE)", "at 1% (SE)", "any found")
	for _, x := range append(append([]studyRate(nil), r.Null...), r.Cells...) {
		fmt.Fprintf(&b, "%-34s %8d %8.4f (%.4f) %8.4f (%.4f) %10.4f\n", x.Family, x.Tests, x.At5, x.SE5, x.At1, x.SE1, x.AnyFound)
	}
	for _, e := range r.Effects {
		fmt.Fprintf(&b, "effect %-28s tested in %d of %d, power %.3f, mean estimate %.3f", e.Effect, e.Tested, e.Cohorts, e.Power, e.Mean)
		if e.Truth != 0 {
			fmt.Fprintf(&b, ", its interval covering %.3f in %.3f", e.Truth, e.Coverage)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "expression's false discovery proportion with the effects, mean %.4f\n", r.EffectsFDR)
	return b.String()
}

// The study: on every commit a few cohorts (fewer under the race detector), weekly thousands. Each family's false-positive rates
// at 5% and 1% within four standard errors (the larger of the cohorts' and the binomial's) and a
// little of their levels; the cells-as-replicates tests' far above. Weekly too: each family's
// chance of finding anything in a null cohort at most 0.05 and a binomial margin (three standard
// errors); the expression effect's interval covering its truth 95% of the time within that margin
// and the effect found in 80% of the cohorts or more; and the false discovery proportion among
// the genes found with the effects at most 0.05 and the margin.
func TestCohortErrorRates(t *testing.T) {
	n, m := 24, 6
	weekly := false
	if v := os.Getenv("SITUCRAFT_COHORT_ERRORS"); v != "" {
		k, err := strconv.Atoi(v)
		if err != nil || k < 10 {
			t.Fatalf("SITUCRAFT_COHORT_ERRORS=%q: a number of null cohorts, 10 or more", v)
		}
		n, m, weekly = k, k/4, true
	} else if raceDetector {
		n, m = 8, 2
	}
	rep := runStudy(t, n, m)
	t.Logf("\n%s", rep)
	if path := os.Getenv("SITUCRAFT_COHORT_ERRORS_REPORT"); path != "" {
		raw, _ := json.MarshalIndent(rep, "", " ")
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	margin := func(level float64, n int) float64 { return 3 * math.Sqrt(level*(1-level)/float64(n)) }
	for _, x := range rep.Null {
		if x.Tests == 0 {
			t.Errorf("%s: no test", x.Family)
			continue
		}
		for _, lv := range []struct{ rate, se, level float64 }{{x.At5, x.SE5, 0.05}, {x.At1, x.SE1, 0.01}} {
			se := math.Max(lv.se, math.Sqrt(lv.level*(1-lv.level)/float64(x.Tests)))
			if math.Abs(lv.rate-lv.level) > 4*se+lv.level/5 {
				t.Errorf("%s: %.4f below %g (standard error %.4f) of %d null tests", x.Family, lv.rate, lv.level, se, x.Tests)
			}
		}
		if weekly && x.AnyFound > 0.05+margin(0.05, x.Cohorts) {
			t.Errorf("%s: %.4f of the null cohorts find something below an adjusted p of 0.05", x.Family, x.AnyFound)
		}
	}
	for _, x := range rep.Cells {
		if x.At5 < 0.15 {
			t.Errorf("%s: %.4f below 0.05: cells as replicates should find far more than the samples do", x.Family, x.At5)
		}
	}
	if !weekly {
		return
	}
	for _, e := range rep.Effects {
		if e.Effect != "SIM033 in epithelial-like" {
			continue
		}
		if math.Abs(e.Coverage-0.95) > margin(0.05, e.Tested) {
			t.Errorf("%s: its interval covers %.3f in %.3f of the cohorts", e.Effect, e.Truth, e.Coverage)
		}
		if e.Power < 0.8 {
			t.Errorf("%s: found in %.3f of the cohorts", e.Effect, e.Power)
		}
	}
	if rep.EffectsFDR > 0.05+margin(0.05, rep.EffectCohorts) {
		t.Errorf("expression's false discovery proportion with the effects %.4f", rep.EffectsFDR)
	}
}
