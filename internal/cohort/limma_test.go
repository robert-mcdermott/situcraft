package cohort

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// rnum is a number as R's jsonlite writes it: Inf as a string.
type rnum float64

func (r *rnum) UnmarshalJSON(b []byte) error {
	if s, err := strconv.Unquote(string(b)); err == nil {
		v, err := strconv.ParseFloat(s, 64)
		*r = rnum(v)
		return err
	}
	var v float64
	err := json.Unmarshal(b, &v)
	*r = rnum(v)
	return err
}

type limmaCases struct {
	Tests []struct {
		Name      string      `json:"name"`
		Counts    [][]float64 `json:"counts"`
		X         [][]float64 `json:"x"`
		Contrasts [][]float64 `json:"contrasts"`
	} `json:"tests"`
	Plain []struct {
		Name      string      `json:"name"`
		Keys      []string    `json:"keys"`
		Y         [][]float64 `json:"y"`
		X         [][]float64 `json:"x"`
		Contrasts [][]float64 `json:"contrasts"`
	} `json:"plain"`
	Lowess []struct {
		Name  string    `json:"name"`
		X     []float64 `json:"x"`
		Y     []float64 `json:"y"`
		F     float64   `json:"f"`
		Iter  int       `json:"iter"`
		Delta float64   `json:"delta"`
	} `json:"lowess"`
}

type limmaReference struct {
	Software map[string]string `json:"software"`
	Tests    []struct {
		Name    string      `json:"name"`
		Keep    []bool      `json:"keep"`
		Lib     []float64   `json:"lib"`
		Factors []float64   `json:"factors"`
		Span    float64     `json:"span"`
		Weights [][]float64 `json:"weights"`
		Sigma   []float64   `json:"sigma"`
		Tests   []struct {
			LogFC   []float64 `json:"logFC"`
			Lo      []float64 `json:"lo"`
			Hi      []float64 `json:"hi"`
			T       []float64 `json:"t"`
			P       []float64 `json:"p"`
			S2Prior rnum      `json:"s2Prior"`
			DFPrior rnum      `json:"dfPrior"`
			DFTotal rnum      `json:"dfTotal"`
		} `json:"tests"`
	} `json:"tests"`
	Plain []struct {
		Name  string `json:"name"`
		Tests []struct {
			Estimate []float64 `json:"estimate"`
			Lo       []float64 `json:"lo"`
			Hi       []float64 `json:"hi"`
			T        []float64 `json:"t"`
			P        []float64 `json:"p"`
			S2Prior  rnum      `json:"s2Prior"`
			DFPrior  rnum      `json:"dfPrior"`
			DFTotal  rnum      `json:"dfTotal"`
		} `json:"tests"`
	} `json:"plain"`
	Lowess []struct {
		Name string    `json:"name"`
		Y    []float64 `json:"y"`
	} `json:"lowess"`
	Special struct {
		X               []float64 `json:"x"`
		Digamma         []float64 `json:"digamma"`
		Trigamma        []float64 `json:"trigamma"`
		Tetragamma      []float64 `json:"tetragamma"`
		InverseOf       []float64 `json:"inverseOf"`
		TrigammaInverse []float64 `json:"trigammaInverse"`
	} `json:"special"`
}

func readLimma(t *testing.T) (limmaCases, limmaReference) {
	t.Helper()
	dir := filepath.Join("..", "..", "validation", "reference", "limma")
	var cases limmaCases
	var ref limmaReference
	for file, v := range map[string]any{"cases.json": &cases, "limma.json": &ref} {
		raw, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, v); err != nil {
			t.Fatal(err)
		}
	}
	if len(cases.Tests) != len(ref.Tests) || len(cases.Plain) != len(ref.Plain) || len(cases.Lowess) != len(ref.Lowess) || len(cases.Tests) < 5 || len(cases.Plain) < 5 {
		t.Fatalf("%d, %d and %d cases, %d, %d and %d answers", len(cases.Tests), len(cases.Plain), len(cases.Lowess), len(ref.Tests), len(ref.Plain), len(ref.Lowess))
	}
	return cases, ref
}

func relClose(a, b, tol float64) bool {
	if math.IsInf(b, 1) {
		return math.IsInf(a, 1)
	}
	return math.Abs(a-b) <= tol*math.Max(1, math.Abs(b))
}

// The special functions and lowess against R's.
func TestSpecialFunctionsAndLowessAgainstR(t *testing.T) {
	cases, ref := readLimma(t)
	s := ref.Special
	for i, x := range s.X {
		for _, c := range []struct {
			name      string
			got, want float64
		}{{"digamma", digamma(x), s.Digamma[i]}, {"trigamma", trigamma(x), s.Trigamma[i]}, {"tetragamma", tetragamma(x), s.Tetragamma[i]}} {
			if !relClose(c.got, c.want, 1e-12) {
				t.Errorf("%s(%g) = %.17g, R %.17g", c.name, x, c.got, c.want)
			}
		}
	}
	for i, x := range s.InverseOf {
		if got := trigammaInverse(x); !relClose(got, s.TrigammaInverse[i], 1e-7) {
			t.Errorf("trigammaInverse(%g) = %.17g, limma %.17g", x, got, s.TrigammaInverse[i])
		}
	}
	for c, cs := range cases.Lowess {
		got := lowess(cs.X, cs.Y, cs.F, cs.Iter, cs.Delta)
		worst := 0.0
		for i, v := range got {
			worst = math.Max(worst, math.Abs(v-ref.Lowess[c].Y[i]))
		}
		if worst > 1e-10 {
			t.Errorf("%s: differs from R's by up to %.3g", cs.Name, worst)
		}
	}
}

// Each case of validation/reference/limma/cases.json filtered, normalized, weighed and tested as
// limma and edgeR do it (validation/reference/limma/compare.sh): the genes kept, the TMM factors,
// voom's span and weights, the residual deviations, the prior, and each contrast's estimate,
// interval, moderated t and p.
func TestCountsAgainstLimma(t *testing.T) {
	cases, ref := readLimma(t)
	t.Logf("against %v", ref.Software)
	worst := map[string]float64{}
	note := worstOf(worst)
	for c, cs := range cases.Tests {
		want := ref.Tests[c]
		f, err := FitCounts(cs.Counts, cs.X)
		if err != nil {
			t.Fatalf("%s: %v", cs.Name, err)
		}
		for g := range want.Keep {
			if f.Keep[g] != want.Keep[g] {
				t.Fatalf("%s: gene %d kept %v, edgeR %v", cs.Name, g, f.Keep[g], want.Keep[g])
			}
		}
		for i := range want.Lib {
			note("library sizes", f.Lib[i], want.Lib[i])
			note("TMM factors", f.Factors[i], want.Factors[i])
		}
		note("span", f.Voom.Span, want.Span)
		for g := range want.Weights {
			note("sigma", f.Fit.Sigma[g], want.Sigma[g])
			for i := range want.Weights[g] {
				note("voom weights", f.Voom.Weights[g][i], want.Weights[g][i])
			}
		}
		for k, ct := range cs.Contrasts {
			w := want.Tests[k]
			note("prior variance", f.Moderate.S2Prior, float64(w.S2Prior))
			note("prior df", f.Moderate.DFPrior, float64(w.DFPrior))
			note("total df", f.Moderate.DFTotal, float64(w.DFTotal))
			for g, r := range TestContrast(f.Fit, f.Moderate, ct, 0.95) {
				note("log fold", r.Estimate, w.LogFC[g])
				note("interval", r.Lo, w.Lo[g])
				note("interval", r.Hi, w.Hi[g])
				note("t", r.T, w.T[g])
				note("p", r.P, w.P[g])
			}
		}
	}
	reportWorst(t, worst, "limma's")
}

// worstOf notes the largest relative difference of each kind of number.
func worstOf(worst map[string]float64) func(what string, got, want float64) {
	return func(what string, got, want float64) {
		if math.IsInf(want, 1) && math.IsInf(got, 1) {
			return
		}
		d := math.Abs(got-want) / math.Max(1, math.Abs(want))
		if math.IsNaN(d) {
			d = math.Inf(1)
		}
		worst[what] = math.Max(worst[what], d)
	}
}

func reportWorst(t *testing.T, worst map[string]float64, whose string) {
	t.Helper()
	whats := make([]string, 0, len(worst))
	for what := range worst {
		whats = append(whats, what)
	}
	sort.Strings(whats)
	for _, what := range whats {
		d := worst[what]
		t.Logf("%-15s within %.2g", what, d)
		if d > 1e-8 {
			t.Errorf("%s differ from %s by up to %.3g", what, whose, d)
		}
	}
}

// Each plain case of cases.json (spatial statistics by the design's rows) by FitLinear, Moderate
// and TestContrast, against limma's lmFit and eBayes; and each statistic's unmoderated contrast
// (its own residual variance, the residual degrees of freedom) against statsmodels' OLS.
func TestPlainAgainstLimmaAndStatsmodels(t *testing.T) {
	cases, ref := readLimma(t)
	var ols struct {
		Statsmodels string `json:"statsmodels"`
		Cases       []struct {
			Name     string `json:"name"`
			Features []struct {
				Params []float64      `json:"params"`
				Sigma  float64        `json:"sigma"`
				DF     float64        `json:"df"`
				Tests  []ContrastTest `json:"tests"`
			} `json:"features"`
		} `json:"cases"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "validation", "reference", "limma", "statsmodels.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &ols); err != nil {
		t.Fatal(err)
	}
	if len(ols.Cases) != len(cases.Plain) {
		t.Fatalf("%d cases, %d answers from statsmodels", len(cases.Plain), len(ols.Cases))
	}
	worstL, worstS := map[string]float64{}, map[string]float64{}
	noteL, noteS := worstOf(worstL), worstOf(worstS)
	for c, cs := range cases.Plain {
		f, err := FitLinear(cs.Y, nil, cs.X)
		if err != nil {
			t.Fatalf("%s: %v", cs.Name, err)
		}
		m := Moderate(f)
		for k, ct := range cs.Contrasts {
			w := ref.Plain[c].Tests[k]
			noteL("prior variance", m.S2Prior, float64(w.S2Prior))
			noteL("prior df", m.DFPrior, float64(w.DFPrior))
			noteL("total df", m.DFTotal, float64(w.DFTotal))
			for g, r := range TestContrast(f, m, ct, 0.95) {
				noteL("estimate", r.Estimate, w.Estimate[g])
				noteL("interval", r.Lo, w.Lo[g])
				noteL("interval", r.Hi, w.Hi[g])
				noteL("t", r.T, w.T[g])
				noteL("p", r.P, w.P[g])
			}
			// Unmoderated: each feature's own variance on the residual degrees of freedom.
			own := Moderation{DFPrior: 0, DFTotal: float64(f.DF), S2Post: make([]float64, len(f.Sigma))}
			for g, s := range f.Sigma {
				own.S2Post[g] = mul(s, s)
			}
			for g, r := range TestContrast(f, own, ct, 0.95) {
				o := ols.Cases[c].Features[g]
				noteS("coefficients", f.Coef[g][0], o.Params[0])
				noteS("sigma", f.Sigma[g], o.Sigma)
				noteS("df", float64(f.DF), o.DF)
				ot := o.Tests[k]
				noteS("estimate", r.Estimate, ot.Estimate)
				noteS("interval", r.Lo, ot.Lo)
				noteS("interval", r.Hi, ot.Hi)
				noteS("t", r.T, ot.T)
				noteS("p", r.P, ot.P)
			}
		}
	}
	reportWorst(t, worstL, "limma's")
	t.Logf("against statsmodels %s", ols.Statsmodels)
	reportWorst(t, worstS, "statsmodels'")
}
