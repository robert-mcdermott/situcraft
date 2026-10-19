package cohort

// Spatial statistics compared between groups (wave 7, slice 7): each sample's numbers from its
// spatial analyses (a pair of labels' neighbor enrichment, a label's median distance to the nearest
// cell of another, a gene's Moran's I), a replicate unit's value the mean of its samples', in a
// linear model of the design (its pairing and covariates). Ratios are compared on the log2 scale.
// A family's statistics are fitted together, their variances moderated across the family
// (limma.go: Smyth 2004), each contrast's estimate with its interval and moderated t; p-values
// adjusted by Benjamini and Hochberg over the family's tests, the universe recorded. A niche's share
// is a count of cells, compared by abundance's quasi-binomial model (abundance.go): moderated with
// the other niches', six unlike variances made one, its tests were too often found (slice 8's
// study of the error rates: 6.0% at 5%).
//
// Files (an artifact of kind spatial-comparison; docs/FORMATS.md): spatial-tests.json
// (SpatialComparison), results.csv (a row a test).

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"situcraft/internal/artifacts"
	"situcraft/internal/dmath"
	"situcraft/internal/stats"
)

// The comparison's files and method version.
const (
	FileSpatialTests          = "spatial-tests.json"
	FileSpatialResults        = "results.csv"
	SpatialTestsMethodVersion = "2"
)

// SpatialTestsRoles are the files' roles.
var SpatialTestsRoles = map[string]string{FileSpatialTests: "comparison", FileSpatialResults: "results"}

// The families of spatial statistics.
const (
	FamilyEnrichment = "enrichment"
	FamilyNearest    = "nearest"
	FamilyMoran      = "moran"
)

// SpatialFamilies are the families a comparison can test: their names, whether they are compared
// as log2 ratios, and their effects' names.
var SpatialFamilies = []struct {
	ID, Name, Effect string
	Log              bool
}{
	{FamilyEnrichment, "Neighbor enrichment (found over expected)", "ratio of enrichments", true},
	{FamilyNearest, "Median distance to the nearest cell (µm)", "ratio of distances", true},
	{FamilyMoran, "Moran's I of a gene's counts", "difference in Moran's I", false},
}

// SpatialTest is one statistic's test of one contrast: the estimate (a log2 ratio, or a
// difference) with its interval, the moderated t, p and adjusted p; the effect as a ratio (2 to
// the estimate) for a log family; or, untested, why.
type SpatialTest struct {
	Key      string  `json:"key"`
	Contrast string  `json:"contrast"`
	Tested   bool    `json:"tested"`
	Note     string  `json:"note,omitempty"`
	Estimate float64 `json:"estimate"`
	Lo       float64 `json:"lo"`
	Hi       float64 `json:"hi"`
	T        float64 `json:"t"`
	P        float64 `json:"p"`
	PAdj     float64 `json:"padj"`
	Ratio    float64 `json:"ratio,omitempty"`
	RatioLo  float64 `json:"ratioLo,omitempty"`
	RatioHi  float64 `json:"ratioHi,omitempty"`
}

// SpatialFamily is a family's comparison: its statistics (keys), each row's value of each (null
// when none of its samples has it; log2 for a log family), the prior of their variances (its
// degrees of freedom null when infinite) and the tests' degrees of freedom, its tests and their
// universe.
type SpatialFamily struct {
	Family   string        `json:"family"`
	Name     string        `json:"name"`
	Effect   string        `json:"effect"`
	Log      bool          `json:"log"`
	Keys     []string      `json:"keys"`
	Values   [][]*float64  `json:"values"`
	S2Prior  float64       `json:"s2Prior"`
	DFPrior  *float64      `json:"dfPrior"`
	DFTotal  float64       `json:"dfTotal"`
	Tests    []SpatialTest `json:"tests"`
	Universe string        `json:"universe"`
	Untested int           `json:"untested"`
}

// SpatialComparison is spatial-tests.json.
type SpatialComparison struct {
	Format        string           `json:"format"`
	Version       int              `json:"version"`
	Method        string           `json:"method"`
	MethodVersion string           `json:"methodVersion"`
	Summary       SummaryRef       `json:"summary"`
	Labels        LabelSet         `json:"labels"`
	Design        ComparisonDesign `json:"design"`
	Level         float64          `json:"level"`
	Families      []SpatialFamily  `json:"families"`
}

// SpatialFamiliesOf are the families the summary's samples have numbers of, in SpatialFamilies'
// order.
func SpatialFamiliesOf(d *Document) []string {
	has := map[string]bool{}
	for _, s := range d.Samples {
		for _, st := range s.Statistics {
			has[st.Family] = true
		}
	}
	var out []string
	for _, f := range SpatialFamilies {
		if has[f.ID] {
			out = append(out, f.ID)
		}
	}
	return out
}

// sampleValues are a sample's numbers of a family by key, on the scale compared: a ratio's log2
// (a ratio of 0 is none), Moran's I as it is.
func sampleValues(s *SampleDoc, family string, log bool) map[string]float64 {
	out := map[string]float64{}
	for _, st := range s.Statistics {
		if st.Family != family {
			continue
		}
		v := st.Value
		if log {
			if !(v > 0) {
				continue
			}
			v = log2(v)
		}
		out[st.Key] = v
	}
	return out
}

// CompareSpatial compares the summary's samples' spatial statistics by the design: the families
// asked (enrichment, nearest, moran), each statistic's test of each contrast at the level.
func CompareSpatial(d *Document, design ComparisonDesign, families []string, level float64) (*SpatialComparison, error) {
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
	X := make([][]float64, len(design.Rows))
	for r, row := range design.Rows {
		X[r] = row.X
	}
	c := &SpatialComparison{Format: "situcraft-spatial-tests", Version: 1, Method: "SituCraft spatial statistics compared", MethodVersion: SpatialTestsMethodVersion, Summary: SummaryRef{}, Labels: d.Labels, Design: design, Level: level}
	for _, fam := range families {
		k := -1
		for i, f := range SpatialFamilies {
			if f.ID == fam {
				k = i
			}
		}
		if k < 0 {
			return nil, fmt.Errorf("no family %q to compare: enrichment, nearest or moran (a niche's share is compared with the abundances)", fam)
		}
		def := SpatialFamilies[k]
		f := SpatialFamily{Family: fam, Name: def.Name, Effect: def.Effect, Log: def.Log, Keys: []string{}, Values: [][]*float64{}, Tests: []SpatialTest{}}
		// Each row's mean of its samples' values, on the scale compared.
		rows := make([]map[string]float64, len(design.Rows))
		seen := map[string]bool{}
		for r, row := range design.Rows {
			sums, ns := map[string]float64{}, map[string]int{}
			for _, s := range row.Samples {
				for key, v := range sampleValues(&d.Samples[at[s]], fam, def.Log) {
					sums[key] += v
					ns[key]++
					seen[key] = true
				}
			}
			rows[r] = map[string]float64{}
			for key, v := range sums {
				rows[r][key] = v / float64(ns[key])
			}
		}
		for key := range seen {
			f.Keys = append(f.Keys, key)
		}
		sort.Strings(f.Keys)
		if len(f.Keys) == 0 {
			return nil, fmt.Errorf("the summary's samples have no %s to compare", def.Name)
		}
		var y [][]float64
		var tested []int
		for i, key := range f.Keys {
			vals := make([]*float64, len(rows))
			every := true
			for r := range rows {
				if v, ok := rows[r][key]; ok {
					vv := v
					vals[r] = &vv
				} else {
					every = false
				}
			}
			f.Values = append(f.Values, vals)
			if every {
				row := make([]float64, len(rows))
				for r := range rows {
					row[r] = *vals[r]
				}
				y = append(y, row)
				tested = append(tested, i)
			}
		}
		var results [][]ModeratedTest
		var m Moderation
		if len(y) > 0 {
			fit, err := FitLinear(y, nil, X)
			if err != nil {
				return nil, err
			}
			m = Moderate(fit)
			f.S2Prior, f.DFTotal = m.S2Prior, m.DFTotal
			if !math.IsInf(m.DFPrior, 1) {
				v := m.DFPrior
				f.DFPrior = &v
			}
			for _, ct := range design.Contrasts {
				results = append(results, TestContrast(fit, m, ct.Vector, level))
			}
		}
		index := map[int]int{}
		for j, i := range tested {
			index[i] = j
		}
		for i, key := range f.Keys {
			j, ok := index[i]
			for ci, ct := range design.Contrasts {
				t := SpatialTest{Key: key, Contrast: ct.Name}
				switch {
				case !ok:
					t.Note = "not in every row: a row's samples have none of it"
				case !(m.S2Post[j] > 0) || math.IsNaN(results[ci][j].T):
					t.Note = "no variation left to test"
				default:
					r := results[ci][j]
					t.Tested = true
					t.Estimate, t.Lo, t.Hi, t.T, t.P = r.Estimate, r.Lo, r.Hi, r.T, r.P
					if def.Log {
						t.Ratio, t.RatioLo, t.RatioHi = exp2(r.Estimate), exp2(r.Lo), exp2(r.Hi)
					}
				}
				if !t.Tested {
					f.Untested++
				}
				f.Tests = append(f.Tests, t)
			}
		}
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
		f.Universe = fmt.Sprintf("%s (%s × %s%s), adjusted together by Benjamini and Hochberg", plural(len(ps), "test"), plural(len(f.Keys), "statistic"), plural(len(design.Contrasts), "contrast"), left)
		c.Families = append(c.Families, f)
	}
	if len(c.Families) == 0 {
		return nil, errors.New("no family to compare: enrichment, nearest or moran")
	}
	return c, nil
}

// exp2 is 2 to the x, the same on every computer.
func exp2(x float64) float64 { return dmath.Exp(mul(x, math.Ln2)) }

// SpatialResultsCSV is results.csv: a row each test.
func SpatialResultsCSV(c *SpatialComparison) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write([]string{"family", "key", "contrast", "tested", "estimate", "lo", "hi", "t", "df", "p", "padj", "ratio", "ratio_lo", "ratio_hi", "note"})
	for _, f := range c.Families {
		for _, t := range f.Tests {
			if !t.Tested {
				w.Write([]string{f.Family, t.Key, t.Contrast, "false", "", "", "", "", "", "", "", "", "", "", t.Note})
				continue
			}
			ratio := []string{"", "", ""}
			if f.Log {
				ratio = []string{number(t.Ratio), number(t.RatioLo), number(t.RatioHi)}
			}
			w.Write(append(append([]string{f.Family, t.Key, t.Contrast, "true", number(t.Estimate), number(t.Lo), number(t.Hi), number(t.T), number(f.DFTotal), number(t.P), number(t.PAdj)}, ratio...), t.Note))
		}
	}
	w.Flush()
	return buf.Bytes()
}

// WriteSpatialComparison writes a comparison's files into dir.
func WriteSpatialComparison(dir string, c *SpatialComparison) error {
	raw, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileSpatialTests), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileSpatialResults), SpatialResultsCSV(c), 0o644)
}

// ReadSpatialComparison reads spatial-tests.json.
func ReadSpatialComparison(dir string) (*SpatialComparison, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileSpatialTests))
	if err != nil {
		return nil, err
	}
	c := &SpatialComparison{}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, fmt.Errorf("%s: %w", FileSpatialTests, err)
	}
	return c, nil
}

// CheckSpatialComparison checks a comparison's files: each family a value per statistic and row,
// each tested test's interval around its estimate, its ratios its estimate's, its adjusted p
// between its p and 1, the results the document's. Its summary: the tests, and those below an
// adjusted p of 0.05.
func CheckSpatialComparison(dir string, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: "cohort.spatial-tests", Message: fmt.Sprintf(format, args...)})
	}
	c, err := ReadSpatialComparison(dir)
	if err != nil {
		fatal("%v", err)
		return out
	}
	type found struct {
		Family   string  `json:"family"`
		Key      string  `json:"key"`
		Contrast string  `json:"contrast"`
		Estimate float64 `json:"estimate"`
		Ratio    float64 `json:"ratio,omitempty"`
		PAdj     float64 `json:"padj"`
	}
	sum := struct {
		Summary  string  `json:"summary"`
		Tests    int     `json:"tests"`
		Untested int     `json:"untested"`
		Found    []found `json:"found"`
	}{Summary: c.Summary.ID, Found: []found{}}
	for _, f := range c.Families {
		if len(f.Values) != len(f.Keys) || len(f.Tests) != len(f.Keys)*len(c.Design.Contrasts) {
			fatal("%s: not a value per statistic, and a test per statistic and contrast", f.Family)
			continue
		}
		for k, v := range f.Values {
			if len(v) != len(c.Design.Rows) {
				fatal("%s: %s has not a value per row", f.Family, f.Keys[k])
			}
		}
		for _, t := range f.Tests {
			if !t.Tested {
				sum.Untested++
				continue
			}
			sum.Tests++
			bad := !(t.Lo <= t.Estimate && t.Estimate <= t.Hi) || !(t.PAdj >= t.P*(1-1e-12) && t.PAdj <= 1)
			if f.Log && math.Abs(t.Ratio-math.Exp2(t.Estimate)) > 1e-9*t.Ratio {
				bad = true
			}
			if bad {
				fatal("%s: the test of %s, %s, is not consistent", f.Family, t.Key, t.Contrast)
			}
			if t.PAdj < 0.05 {
				sum.Found = append(sum.Found, found{f.Family, t.Key, t.Contrast, t.Estimate, t.Ratio, t.PAdj})
			}
		}
	}
	if raw, err := os.ReadFile(filepath.Join(dir, FileSpatialResults)); err != nil || !bytes.Equal(raw, SpatialResultsCSV(c)) {
		fatal("%s: not the comparison's tests", FileSpatialResults)
	}
	m.Summary, _ = json.Marshal(sum)
	return out
}
