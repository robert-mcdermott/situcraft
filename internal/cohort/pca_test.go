package cohort

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// The principal components of validation/reference/pca/cases.json's pseudobulk against numpy's
// (validation/reference/pca/compare.sh): the same genes kept, every score and every share of the
// variance within 1e-9 of numpy's, each component's sign the same.
func TestPCAAgainstNumpy(t *testing.T) {
	dir := filepath.Join("..", "..", "validation", "reference", "pca")
	var cases []struct {
		Name   string      `json:"name"`
		Genes  []string    `json:"genes"`
		Counts [][]float64 `json:"counts"`
	}
	var ref struct {
		Numpy string `json:"numpy"`
		Cases []struct {
			Name     string      `json:"name"`
			Genes    []int       `json:"genes"`
			Scores   [][]float64 `json:"scores"`
			Variance []float64   `json:"variance"`
		} `json:"cases"`
	}
	for file, v := range map[string]any{"cases.json": &cases, "numpy.json": &ref} {
		raw, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, v); err != nil {
			t.Fatal(err)
		}
	}
	if len(cases) != len(ref.Cases) || len(cases) < 3 {
		t.Fatalf("%d cases, %d answers", len(cases), len(ref.Cases))
	}
	for c, cs := range cases {
		want := ref.Cases[c]
		p, err := PseudobulkPCA(cs.Counts, cs.Genes, PCATop, PCAComponents)
		if err != nil {
			t.Fatalf("%s: %v", cs.Name, err)
		}
		if len(p.Genes) != len(want.Genes) {
			t.Errorf("%s: %d genes kept, numpy %d", cs.Name, len(p.Genes), len(want.Genes))
			continue
		}
		for j, g := range want.Genes {
			if p.Genes[j] != cs.Genes[g] {
				t.Errorf("%s: gene %d kept is %s, numpy's %s", cs.Name, j, p.Genes[j], cs.Genes[g])
				break
			}
		}
		if len(p.Variance) != len(want.Variance) {
			t.Fatalf("%s: %d components, numpy %d", cs.Name, len(p.Variance), len(want.Variance))
		}
		worst := 0.0
		for k := range want.Variance {
			worst = math.Max(worst, math.Abs(p.Variance[k]-want.Variance[k]))
			for s := range want.Scores {
				worst = math.Max(worst, math.Abs(p.Scores[s][k]-want.Scores[s][k])/math.Max(1, math.Abs(want.Scores[s][k])))
			}
		}
		if worst > 1e-9 {
			t.Errorf("%s: off numpy %s by %.3g", cs.Name, ref.Numpy, worst)
		}
		t.Logf("%s: %d samples, %d genes kept, %d components, off numpy by %.2g", cs.Name, len(cs.Counts), len(p.Genes), len(p.Variance), worst)
	}
}

func TestPCARefuses(t *testing.T) {
	if _, err := PseudobulkPCA([][]float64{{1, 2}}, []string{"a", "b"}, 500, 10); err == nil {
		t.Error("one sample accepted")
	}
	if _, err := PseudobulkPCA([][]float64{{1, 2}, {0, 0}}, []string{"a", "b"}, 500, 10); err == nil {
		t.Error("a sample without transcripts accepted")
	}
	if _, err := PseudobulkPCA([][]float64{{}, {}}, nil, 500, 10); err == nil {
		t.Error("no genes accepted")
	}
	// Samples alike: no component.
	p, err := PseudobulkPCA([][]float64{{1, 2}, {2, 4}}, []string{"a", "b"}, 500, 10)
	if err != nil || len(p.Variance) != 0 || len(p.Scores) != 2 {
		t.Errorf("samples alike: %+v %v", p, err)
	}
}
