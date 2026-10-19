package cohort

// Differential abundance (wave 7, slice 6): each label's cells compared between groups with the
// sample (or the replicate unit, its samples combined) as the unit. A label's share of the labeled
// cells by a quasi-binomial model of the design (its pairing and covariates); its density by a
// quasi-Poisson model with the tissue's area as offset; a niche's share of the cells with a window
// as a share is. Each contrast's effect is the model's (an odds ratio, a density ratio) with its
// interval; p-values adjusted by Benjamini and Hochberg over the family's tests, the universe
// recorded.
//
// Files (an artifact of kind abundance; docs/FORMATS.md): abundance.json (Comparison), results.csv
// (a row a test).

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"situcraft/internal/artifacts"
	"situcraft/internal/dmath"
	"situcraft/internal/stats"
)

// The comparison's files and method version.
const (
	FileAbundance          = "abundance.json"
	FileAbundanceResults   = "results.csv"
	AbundanceMethodVersion = "1"
)

// AbundanceRoles are the files' roles.
var AbundanceRoles = map[string]string{FileAbundance: "comparison", FileAbundanceResults: "results"}

// The families of tests.
const (
	FamilyShare   = "share"
	FamilyDensity = "density"
	FamilyNiche   = "niche"
)

// DesignRow is a row of the design: a replicate unit in a group, its samples (by id, their counts
// combined), and its columns' values.
type DesignRow struct {
	Unit    string    `json:"unit"`
	Group   string    `json:"group"`
	Samples []string  `json:"samples"`
	X       []float64 `json:"x"`
}

// Contrast is a contrast of the design's coefficients: its name and its vector over the columns.
type Contrast struct {
	Name   string    `json:"name"`
	Vector []float64 `json:"vector"`
}

// ExcludedSample is a sample the design leaves out, with the reason it records.
type ExcludedSample struct {
	Sample string `json:"sample"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// ComparisonDesign is the design a comparison is of (web/lib/design.js makes it): its name and
// version when saved, its specification, its columns, rows, contrasts and samples left out.
// PseudoSamples marks a teaching example's design whose units are regions of one section: not
// independent replicates, compared only so that the comparison says so.
type ComparisonDesign struct {
	Name          string           `json:"name,omitempty"`
	Version       int              `json:"version,omitempty"`
	Spec          json.RawMessage  `json:"spec,omitempty"`
	Names         []string         `json:"names"`
	Rows          []DesignRow      `json:"rows"`
	Contrasts     []Contrast       `json:"contrasts"`
	Excluded      []ExcludedSample `json:"excluded,omitempty"`
	PseudoSamples bool             `json:"pseudoSamples,omitempty"`
}

// AbundanceTest is one label's test of one contrast: the model's estimate (on the link's scale)
// with its standard error, t, p and interval; the adjusted p; the effect as a ratio with its
// interval; the dispersion and residual degrees of freedom; or, untested, why.
type AbundanceTest struct {
	Label      string  `json:"label"`
	Contrast   string  `json:"contrast"`
	Tested     bool    `json:"tested"`
	Note       string  `json:"note,omitempty"`
	Estimate   float64 `json:"estimate"`
	SE         float64 `json:"se"`
	T          float64 `json:"t"`
	P          float64 `json:"p"`
	PAdj       float64 `json:"padj"`
	Ratio      float64 `json:"ratio"`
	Lo         float64 `json:"lo"`
	Hi         float64 `json:"hi"`
	Dispersion float64 `json:"dispersion"`
	DF         int     `json:"df"`
}

// AbundanceFamily is a family of tests: what is compared and by what model, its labels, each row's
// counts of each label and its total (labeled cells, tissue in mm², cells with a window), the
// values compared (shares or densities), its tests and their universe.
type AbundanceFamily struct {
	Family   string          `json:"family"`
	Name     string          `json:"name"`
	Model    string          `json:"model"`
	Effect   string          `json:"effect"`
	Labels   []string        `json:"labels"`
	Counts   [][]float64     `json:"counts"`
	Totals   []float64       `json:"totals"`
	Values   [][]float64     `json:"values"`
	Tests    []AbundanceTest `json:"tests"`
	Universe string          `json:"universe"`
	Untested int             `json:"untested"`
}

// SummaryRef names the summary a comparison is of.
type SummaryRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Comparison is abundance.json.
type Comparison struct {
	Format        string            `json:"format"`
	Version       int               `json:"version"`
	Method        string            `json:"method"`
	MethodVersion string            `json:"methodVersion"`
	Summary       SummaryRef        `json:"summary"`
	Labels        LabelSet          `json:"labels"`
	Design        ComparisonDesign  `json:"design"`
	Level         float64           `json:"level"`
	Families      []AbundanceFamily `json:"families"`
}

// CheckDesign says what is wrong with a design for the summary's samples: rows of the columns'
// width naming the summary's samples, each once; contrasts of that width; more rows than columns.
// And what would make its tests of cells rather than of replicates: a group of one unit, a unit in
// two rows of one group, and regions of one section given different units (unless the design says
// they are pseudo-samples, for teaching).
func CheckDesign(d *Document, design ComparisonDesign) error {
	p := len(design.Names)
	if p == 0 || len(design.Rows) <= p {
		return fmt.Errorf("the design has %d rows and %d columns: no degrees of freedom are left", len(design.Rows), p)
	}
	known := map[string]bool{}
	for _, s := range d.Samples {
		known[s.Sample] = true
	}
	used := map[string]bool{}
	for k, r := range design.Rows {
		if len(r.X) != p {
			return fmt.Errorf("the design's row %d has %d values, not %d", k+1, len(r.X), p)
		}
		if len(r.Samples) == 0 {
			return fmt.Errorf("the design's row %d has no sample", k+1)
		}
		for _, s := range r.Samples {
			if !known[s] {
				return fmt.Errorf("the design's sample %s is not in the summary", s)
			}
			if used[s] {
				return fmt.Errorf("the design's sample %s is in two rows", s)
			}
			used[s] = true
		}
		for _, v := range r.X {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("the design's row %d is not of numbers", k+1)
			}
		}
	}
	if err := checkReplicates(d, design); err != nil {
		return err
	}
	if len(design.Contrasts) == 0 || len(design.Contrasts) > 50 {
		return errors.New("a comparison is of 1 to 50 contrasts")
	}
	for _, c := range design.Contrasts {
		if len(c.Vector) != p {
			return fmt.Errorf("the contrast %s has %d values, not %d", c.Name, len(c.Vector), p)
		}
	}
	return nil
}

// checkReplicates refuses a design whose groups are not compared by their replicate units' variation.
func checkReplicates(d *Document, design ComparisonDesign) error {
	units := map[string]map[string]bool{}
	var groups []string
	for _, r := range design.Rows {
		if units[r.Group] == nil {
			units[r.Group] = map[string]bool{}
			groups = append(groups, r.Group)
		}
		if units[r.Group][r.Unit] {
			return fmt.Errorf("the unit %s is in two rows of %s: a unit's samples in a group are one row (technical replicates, combined)", r.Unit, r.Group)
		}
		units[r.Group][r.Unit] = true
	}
	if len(groups) < 2 {
		return errors.New("every row is in one group: a comparison needs two")
	}
	for _, g := range groups {
		if n := len(units[g]); n < 2 {
			return fmt.Errorf("%s has 1 replicate unit: a group needs at least two, so that the variation between units can be measured (with one, a test would be of its cells, which are not replicates)", g)
		}
	}
	if design.PseudoSamples {
		return nil
	}
	// Regions of one section: one dataset's samples in rows of different units.
	at := map[string]int{}
	for i, s := range d.Samples {
		at[s.Sample] = i
	}
	type seen struct{ unit, name string }
	byDataset := map[string]seen{}
	for _, r := range design.Rows {
		for _, id := range r.Samples {
			sd := d.Samples[at[id]]
			if sd.Dataset == "" {
				continue
			}
			if o, ok := byDataset[sd.Dataset]; ok && o.unit != r.Unit {
				return fmt.Errorf("%s and %s are regions of one section given different units (%s, %s): regions of one section of one tissue are not independent replicates. Give them one unit, or compare sections", o.name, sd.Name, o.unit, r.Unit)
			}
			byDataset[sd.Dataset] = seen{r.Unit, sd.Name}
		}
	}
	return nil
}

// Abundance compares the summary's samples by the design: the families asked (share, density,
// niche), each label's model and each contrast's test at the level (0.95 for 95% intervals).
func Abundance(d *Document, design ComparisonDesign, families []string, level float64) (*Comparison, error) {
	if err := CheckDesign(d, design); err != nil {
		return nil, err
	}
	if !(level > 0.5 && level < 1) {
		return nil, errors.New("the intervals' level is above 0.5 and below 1")
	}
	at := map[string]int{}
	for i, s := range d.Samples {
		at[s.Sample] = i
	}
	c := &Comparison{Format: "situcraft-abundance", Version: 1, Method: "SituCraft differential abundance", MethodVersion: AbundanceMethodVersion, Labels: d.Labels, Design: design, Level: level}
	X := make([][]float64, len(design.Rows))
	for k, r := range design.Rows {
		X[k] = r.X
	}
	// Each row's sums over its samples.
	sum := func(f func(sd *SampleDoc) float64) []float64 {
		out := make([]float64, len(design.Rows))
		for k, r := range design.Rows {
			for _, s := range r.Samples {
				out[k] += f(&d.Samples[at[s]])
			}
		}
		return out
	}
	for _, fam := range families {
		var f AbundanceFamily
		var labels []string
		var count func(sd *SampleDoc, k int) float64
		var total func(sd *SampleDoc) float64
		model := Binomial
		switch fam {
		case FamilyShare:
			f = AbundanceFamily{Family: fam, Name: "Shares of the labeled cells", Model: "quasi-binomial generalized linear model (logit link), the dispersion Pearson's", Effect: "odds ratio"}
			labels = d.Labels.Names
			count = func(sd *SampleDoc, k int) float64 { return float64(sd.Counts[k]) }
			total = func(sd *SampleDoc) float64 { return float64(sd.Labeled) }
		case FamilyDensity:
			f = AbundanceFamily{Family: fam, Name: "Cells per mm² of tissue", Model: "quasi-Poisson generalized linear model (log link), the log of the tissue's area an offset, the dispersion Pearson's", Effect: "density ratio"}
			labels = d.Labels.Names
			count = func(sd *SampleDoc, k int) float64 { return float64(sd.Counts[k]) }
			total = func(sd *SampleDoc) float64 { return sd.Tissue / 1e6 }
			model = Poisson
		case FamilyNiche:
			if d.Niches == nil {
				return nil, errors.New("the summary found no niches to compare")
			}
			f = AbundanceFamily{Family: fam, Name: "Niches' shares of the cells with a window", Model: "quasi-binomial generalized linear model (logit link), the dispersion Pearson's", Effect: "odds ratio"}
			for v := 0; v < d.Niches.Niches; v++ {
				labels = append(labels, NicheName(v))
			}
			count = func(sd *SampleDoc, k int) float64 {
				if k < len(sd.Niches) {
					return float64(sd.Niches[k])
				}
				return 0
			}
			total = func(sd *SampleDoc) float64 { return float64(sd.NicheCells) }
		default:
			return nil, fmt.Errorf("no family %q to compare: share, density or niche", fam)
		}
		f.Labels = labels
		f.Totals = sum(total)
		for k := range labels {
			kk := k
			y := sum(func(sd *SampleDoc) float64 { return count(sd, kk) })
			values := make([]float64, len(y))
			for r := range y {
				if f.Totals[r] > 0 {
					values[r] = y[r] / f.Totals[r]
				}
			}
			f.Counts = append(f.Counts, y)
			f.Values = append(f.Values, values)
			var ytot, rest float64
			for r := range y {
				ytot += y[r]
				rest += f.Totals[r] - y[r]
			}
			untested := func(note string) {
				for _, ct := range design.Contrasts {
					f.Tests = append(f.Tests, AbundanceTest{Label: labels[k], Contrast: ct.Name, Note: note})
				}
				f.Untested += len(design.Contrasts)
			}
			if ytot == 0 {
				untested("no sample has a cell of it")
				continue
			}
			if model == Binomial && rest == 0 {
				untested("every cell is of it")
				continue
			}
			var fit *GLMFit
			var err error
			if model == Binomial {
				fit, err = FitQuasi(Binomial, X, y, f.Totals, nil)
			} else {
				offset := make([]float64, len(f.Totals))
				for r, a := range f.Totals {
					if !(a > 0) {
						err = errors.New("a row without tissue")
						break
					}
					offset[r] = dmath.Log(a)
				}
				if err == nil {
					fit, err = FitQuasi(Poisson, X, y, nil, offset)
				}
			}
			if err != nil {
				untested("not estimable: " + err.Error())
				continue
			}
			if !fit.Converged {
				untested("not estimable: a group has none of it (or nothing else)")
				continue
			}
			for _, ct := range design.Contrasts {
				t := fit.Test(ct.Vector, level)
				test := AbundanceTest{Label: labels[k], Contrast: ct.Name, Dispersion: fit.Dispersion, DF: fit.DF}
				if math.IsNaN(t.P) || math.IsNaN(t.T) {
					test.Note = "no variation left to test"
					f.Tests = append(f.Tests, test)
					f.Untested++
					continue
				}
				ratio, lo, hi := dmath.Exp(t.Estimate), dmath.Exp(t.Lo), dmath.Exp(t.Hi)
				if !finite(ratio, lo, hi, t.SE, fit.Dispersion) || !(lo > 0) {
					// A group with almost none of it: the effect, or its interval, beyond numbers.
					test.Note = "not estimable: a group has almost none of it, so its effect's interval reaches beyond any ratio"
					f.Tests = append(f.Tests, test)
					f.Untested++
					continue
				}
				test.Tested = true
				test.Estimate, test.SE, test.T, test.P = t.Estimate, t.SE, t.T, t.P
				test.Ratio, test.Lo, test.Hi = ratio, lo, hi
				f.Tests = append(f.Tests, test)
			}
		}
		// Benjamini and Hochberg over the family's tests.
		var ps []float64
		var idx []int
		for i, t := range f.Tests {
			if t.Tested {
				ps = append(ps, t.P)
				idx = append(idx, i)
			}
		}
		adj := make([]float64, len(ps))
		stats.BenjaminiHochberg(ps, adj)
		for j, i := range idx {
			f.Tests[i].PAdj = adj[j]
		}
		left := ""
		if f.Untested > 0 {
			left = fmt.Sprintf(", %d untested", f.Untested)
		}
		f.Universe = fmt.Sprintf("%s (%s × %s%s), adjusted together by Benjamini and Hochberg", plural(len(ps), "test"), plural(len(labels), "label"), plural(len(design.Contrasts), "contrast"), left)
		c.Families = append(c.Families, f)
	}
	if len(c.Families) == 0 {
		return nil, errors.New("no family to compare: share, density or niche")
	}
	return c, nil
}

// finite says whether every value is a number, not infinite.
func finite(v ...float64) bool {
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

// ResultsCSV is results.csv: a row each test.
func ResultsCSV(c *Comparison) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write([]string{"family", "label", "contrast", "tested", "estimate", "se", "t", "df", "p", "padj", "ratio", "lo", "hi", "dispersion", "note"})
	for _, f := range c.Families {
		for _, t := range f.Tests {
			if !t.Tested {
				w.Write([]string{f.Family, t.Label, t.Contrast, "false", "", "", "", "", "", "", "", "", "", "", t.Note})
				continue
			}
			w.Write([]string{f.Family, t.Label, t.Contrast, "true", number(t.Estimate), number(t.SE), number(t.T), strconv.Itoa(t.DF), number(t.P), number(t.PAdj), number(t.Ratio), number(t.Lo), number(t.Hi), number(t.Dispersion), t.Note})
		}
	}
	w.Flush()
	return buf.Bytes()
}

// WriteComparison writes a comparison's files into dir.
func WriteComparison(dir string, c *Comparison) error {
	raw, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileAbundance), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileAbundanceResults), ResultsCSV(c), 0o644)
}

// ReadComparison reads abundance.json.
func ReadComparison(dir string) (*Comparison, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileAbundance))
	if err != nil {
		return nil, err
	}
	c := &Comparison{}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, fmt.Errorf("%s: %w", FileAbundance, err)
	}
	return c, nil
}

// CheckComparison checks a comparison's files: each value its count over its total, each tested
// test's ratio and interval its estimate's, its adjusted p between its p and 1, the results the
// document's. Its summary: the tests, and those whose adjusted p is below 0.05.
func CheckComparison(dir string, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: "cohort.abundance", Message: fmt.Sprintf(format, args...)})
	}
	c, err := ReadComparison(dir)
	if err != nil {
		fatal("%v", err)
		return out
	}
	type found struct {
		Family   string  `json:"family"`
		Label    string  `json:"label"`
		Contrast string  `json:"contrast"`
		Ratio    float64 `json:"ratio"`
		PAdj     float64 `json:"padj"`
	}
	sum := struct {
		Summary  string  `json:"summary"`
		Tests    int     `json:"tests"`
		Untested int     `json:"untested"`
		Found    []found `json:"found"`
	}{Summary: c.Summary.ID, Found: []found{}}
	for _, f := range c.Families {
		for k := range f.Labels {
			for r := range f.Totals {
				if f.Totals[r] > 0 && math.Abs(f.Values[k][r]-f.Counts[k][r]/f.Totals[r]) > 1e-12*math.Max(1, f.Values[k][r]) {
					fatal("%s: %s's value in row %d is not its count over its total", f.Family, f.Labels[k], r+1)
				}
			}
		}
		for _, t := range f.Tests {
			if !t.Tested {
				sum.Untested++
				continue
			}
			sum.Tests++
			if !(t.PAdj >= t.P*(1-1e-12) && t.PAdj <= 1) || math.Abs(t.Ratio-math.Exp(t.Estimate)) > 1e-9*t.Ratio || !(t.Lo <= t.Ratio && t.Ratio <= t.Hi) {
				fatal("%s: the test of %s, %s, is not consistent", f.Family, t.Label, t.Contrast)
			}
			if t.PAdj < 0.05 {
				sum.Found = append(sum.Found, found{f.Family, t.Label, t.Contrast, t.Ratio, t.PAdj})
			}
		}
	}
	if raw, err := os.ReadFile(filepath.Join(dir, FileAbundanceResults)); err != nil || !bytes.Equal(raw, ResultsCSV(c)) {
		fatal("%s: not the comparison's tests", FileAbundanceResults)
	}
	m.Summary, _ = json.Marshal(sum)
	return out
}
