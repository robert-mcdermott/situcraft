package compare

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"situcraft/internal/segment/expand"
	"situcraft/internal/segment/imports"
	"situcraft/internal/xenium"
)

func bundleDir(t *testing.T, name string) string {
	t.Helper()
	root := os.Getenv("SITUCRAFT_DATA")
	if root == "" {
		root = filepath.Join("..", "..", "validation", "cache")
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(filepath.Join(dir, "experiment.xenium")); err != nil {
		want := os.Getenv("SITUCRAFT_REQUIRE_DATA")
		if want == "1" || strings.Contains(","+want+",", ","+name+",") {
			t.Fatalf("%s is missing (node validation/fetch.mjs %s)", dir, name)
		}
		t.Skipf("%s is missing (node validation/fetch.mjs %s)", dir, name)
	}
	return dir
}

// compareInstrumentAndExpansion compares a bundle's instrument segmentation with its nuclei grown
// by 5 µm, with the co-expression reference of its nuclei and a 10 µm expansion.
func compareInstrumentAndExpansion(t *testing.T, name string) *Result {
	t.Helper()
	b, err := xenium.Open(bundleDir(t, name), xenium.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, workers := context.Background(), runtime.NumCPU()
	load := func(make func(dir string) error) *Seg {
		dir := t.TempDir()
		if err := make(dir); err != nil {
			t.Fatal(err)
		}
		s, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	grow := func(um float64) func(string) error {
		return func(dir string) error {
			_, err := expand.FromBundle(ctx, b, dir, expand.Params{DistanceUm: um}, workers, nil)
			return err
		}
	}
	instrument := load(func(dir string) error { _, err := imports.Instrument(ctx, b, dir, workers); return err })
	expansion := load(grow(5))
	nuclei, coarse := load(grow(0)), load(grow(10))
	nucleusRaster, err := expand.NucleusRaster(b)
	if err != nil {
		t.Fatal(err)
	}
	transcripts, err := BundleTranscripts(b)
	if err != nil {
		t.Fatal(err)
	}
	p := DefaultParams()
	ref := NewReference(nuclei.Counts, coarse.Counts, p)
	r, err := Compare(ctx, Inputs{A: instrument, B: expansion, Nuclei: nucleusRaster, Transcripts: transcripts, Reference: ref, Params: p})
	if err != nil {
		t.Fatal(err)
	}
	pretty, _ := json.MarshalIndent(map[string]any{"result": r, "A": r.A, "B": r.B}, "", " ")
	t.Logf("%s: %s", name, strings.ReplaceAll(string(pretty[:min(len(pretty), 4000)]), "\n", " "))
	if ns, _ := ref.Score(nuclei.Counts, p); ns != 1 {
		t.Errorf("the nuclei's own score is %v, not 1", ns)
	}
	return r
}

// The Lung bundle's instrument segmentation and SituCraft's 5 µm expansion of the same nuclei:
// every cell pairs one to one, and the pixels and transcripts that agree are those the expansion's
// own comparison found (research.md §3.1), counted here independently.
func TestRealLungInstrumentAndExpansion(t *testing.T) {
	r := compareInstrumentAndExpansion(t, "xenium-lung-2fov")
	if r.Classes[OneToOne] != 11898 || len(r.Groups) != 11898 {
		t.Errorf("classes %v of %d groups; want 11,898 one-to-one", r.Classes, len(r.Groups))
	}
	if r.PixelAgreement != 0.980629 {
		t.Errorf("pixels agree %v, want 0.980629 (the expansion's own count)", r.PixelAgreement)
	}
	if c := r.Changes; c.Changed != 8724 || c.Transcripts != 825885 || c.Gained != 2534 || c.Moved != 6190 || c.Lost != 0 {
		t.Errorf("changes %+v; want 8,724 of 825,885 (2,534 gained, 6,190 moved)", c)
	}
	if m := float64(r.IoU.Median); !(m > 0.9) {
		t.Errorf("median IoU %v", m)
	}
	if r.A.CellsWithNucleus != 11898 || r.B.CellsWithNucleus != 11898 || r.Coexpression.Pairs == 0 {
		t.Errorf("cells with a nucleus %d, %d; %d spurious pairs", r.A.CellsWithNucleus, r.B.CellsWithNucleus, r.Coexpression.Pairs)
	}
	if math.IsNaN(float64(r.A.Coexpression)) || math.IsNaN(float64(r.A.MECR)) {
		t.Errorf("co-expression %v, MECR %v", r.A.Coexpression, r.A.MECR)
	}
}

// The Breast bundle's instrument segmentation drew most cells from stains; its 5 µm expansion
// grows each nucleus: the instrument's cells of several nuclei are split by it, those of none are
// only the instrument's.
func TestRealBreastInstrumentAndExpansion(t *testing.T) {
	r := compareInstrumentAndExpansion(t, "xenium-breast-2fov")
	if r.Classes[Split] == 0 || r.CellsA[OnlyA] == 0 || r.A.Cells != 7275 || r.B.Cells != 7312 {
		t.Errorf("classes %v; cells %d and %d", r.Classes, r.A.Cells, r.B.Cells)
	}
	cellsA, cellsB := 0, 0
	for _, c := range Classes {
		cellsA += r.CellsA[c]
		cellsB += r.CellsB[c]
	}
	if cellsA != 7275 || cellsB != 7312 {
		t.Errorf("the classes hold %d and %d cells", cellsA, cellsB)
	}
}
