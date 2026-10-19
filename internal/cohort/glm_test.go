package cohort

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"situcraft/internal/dmath"
)

// The quasi-likelihood models of validation/reference/abundance/cases.json against statsmodels'
// (validation/reference/abundance/compare.sh): every coefficient, standard error, dispersion and
// contrast (estimate, standard error, t, p and 95% interval) within 1e-8 of statsmodels'.
func TestGLMAgainstStatsmodels(t *testing.T) {
	dir := filepath.Join("..", "..", "validation", "reference", "abundance")
	var cases []struct {
		Name      string      `json:"name"`
		Family    string      `json:"family"`
		X         [][]float64 `json:"x"`
		Y         []float64   `json:"y"`
		N         []float64   `json:"n"`
		Area      []float64   `json:"area"`
		Contrasts [][]float64 `json:"contrasts"`
	}
	var ref struct {
		Statsmodels string `json:"statsmodels"`
		Cases       []struct {
			Name   string         `json:"name"`
			Params []float64      `json:"params"`
			BSE    []float64      `json:"bse"`
			Scale  float64        `json:"scale"`
			DF     float64        `json:"df"`
			Tests  []ContrastTest `json:"tests"`
		} `json:"cases"`
	}
	for file, v := range map[string]any{"cases.json": &cases, "statsmodels.json": &ref} {
		raw, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, v); err != nil {
			t.Fatal(err)
		}
	}
	if len(cases) != len(ref.Cases) || len(cases) < 5 {
		t.Fatalf("%d cases, %d answers", len(cases), len(ref.Cases))
	}
	close := func(a, b float64) bool { return math.Abs(a-b) <= 1e-8*math.Max(1, math.Abs(b)) }
	worst := 0.0
	for c, cs := range cases {
		want := ref.Cases[c]
		fam := Binomial
		var offset []float64
		if cs.Family == "poisson" {
			fam = Poisson
			for _, a := range cs.Area {
				offset = append(offset, dmath.Log(a))
			}
		}
		fit, err := FitQuasi(fam, cs.X, cs.Y, cs.N, offset)
		if err != nil || !fit.Converged {
			t.Fatalf("%s: %v (converged %v)", cs.Name, err, fit != nil && fit.Converged)
		}
		if !close(fit.Dispersion, want.Scale) || float64(fit.DF) != want.DF {
			t.Errorf("%s: dispersion %g on %d df, statsmodels %g on %g", cs.Name, fit.Dispersion, fit.DF, want.Scale, want.DF)
		}
		for a := range want.Params {
			se := math.Sqrt(fit.Dispersion * fit.Unscaled[a][a])
			if !close(fit.Beta[a], want.Params[a]) || !close(se, want.BSE[a]) {
				t.Errorf("%s: coefficient %d %g (se %g), statsmodels %g (%g)", cs.Name, a, fit.Beta[a], se, want.Params[a], want.BSE[a])
			}
			worst = math.Max(worst, math.Abs(fit.Beta[a]-want.Params[a]))
		}
		for k, cv := range cs.Contrasts {
			got, w := fit.Test(cv, 0.95), want.Tests[k]
			if !close(got.Estimate, w.Estimate) || !close(got.SE, w.SE) || !close(got.T, w.T) || !close(got.P, w.P) || !close(got.Lo, w.Lo) || !close(got.Hi, w.Hi) {
				t.Errorf("%s: contrast %d %+v,\nstatsmodels %+v", cs.Name, k, got, w)
			}
		}
	}
	t.Logf("%d cases against statsmodels %s: coefficients off by %.2g at most", len(cases), ref.Statsmodels, worst)
}

// Refused: no residual degrees of freedom, a count above its total, columns not independent.
func TestGLMRefuses(t *testing.T) {
	x := [][]float64{{1, 0}, {1, 1}}
	if _, err := FitQuasi(Binomial, x, []float64{1, 2}, []float64{10, 10}, nil); err == nil {
		t.Error("no residual degrees of freedom accepted")
	}
	x = [][]float64{{1, 0}, {1, 1}, {1, 1}}
	if _, err := FitQuasi(Binomial, x, []float64{1, 20, 3}, []float64{10, 10, 10}, nil); err == nil {
		t.Error("a count above its total accepted")
	}
	if _, err := FitQuasi(Poisson, [][]float64{{1, 2}, {1, 2}, {1, 2}}, []float64{1, 2, 3}, nil, nil); err == nil {
		t.Error("dependent columns accepted")
	}
	// A group with none of the cells: its coefficient runs off, said not to converge.
	f, err := FitQuasi(Binomial, x, []float64{5, 0, 0}, []float64{10, 10, 10}, nil)
	if err != nil || f.Converged {
		t.Errorf("separation: %v converged %v", err, f != nil && f.Converged)
	}
}
