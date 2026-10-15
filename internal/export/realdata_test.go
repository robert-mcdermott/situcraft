package export

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"situcraft/internal/segment"
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

// The Lung bundle's own segmentation exported in every format: the SpatialData store imports as
// that segmentation exactly (every pixel, every transcript's cell); Xenium Ranger's masks are the
// morphology image's size, with the bundle's nuclei, and the instrument's assignments hold at
// their pixels but for the few it records off its own mask; and what the maintainer's check read
// with spatialdata, anndata, pandas, scipy, geopandas and numpy (validation/reference/exports.json)
// is what SituCraft writes now. With SITUCRAFT_EXPORT_DIR, the export is kept there for that check.
func TestRealExports(t *testing.T) {
	const name = "xenium-lung-2fov"
	b, err := xenium.Open(bundleDir(t, name), xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx := context.Background()
	from := t.TempDir()
	if _, err := imports.Instrument(ctx, b, from, runtime.NumCPU()); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(t.TempDir(), name+"-instrument")
	if dir := os.Getenv("SITUCRAFT_EXPORT_DIR"); dir != "" {
		if !filepath.IsAbs(dir) {
			// Relative to the repository, where the maintainer's check runs.
			dir = filepath.Join("..", "..", dir)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		abs, _ := filepath.Abs(dir)
		folder = filepath.Join(abs, name+"-instrument")
	}
	seg := Segmentation{ID: "instrument", Name: "Instrument segmentation", Method: "Xenium cell segmentation"}
	m, err := Run(ctx, FromBundle(b), from, seg, Options{Formats: Formats, Folder: folder, Overwrite: true, Software: "situcraft test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", m.Counts)
	if m.Counts.Cells != 11898 || m.Counts.CellsWithOutline != 11898 || m.Counts.CellsWithPixels != 11898 {
		t.Errorf("totals %+v", m.Counts)
	}
	for _, f := range m.Findings {
		t.Logf("%s %s: %s", f.Severity, f.Code, f.Message)
		if f.Severity == "fatal" || f.Code == "ranger.no-nuclei" || f.Code == "ranger.image-size" {
			t.Errorf("finding %+v", f)
		}
	}

	// Imported back from SpatialData: the same segmentation.
	back := t.TempDir()
	ds, err := imports.BundleDataset(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := imports.Run(ctx, ds, imports.Options{Format: imports.SpatialData, Paths: []string{filepath.Join(folder, "spatialdata.zarr")}}, back, runtime.NumCPU(), nil); err != nil {
		t.Fatal(err)
	}
	again, err := Run(ctx, FromBundle(b), back, seg, Options{Formats: []string{GeoJSON}, Folder: filepath.Join(t.TempDir(), "again"), Software: "situcraft test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Content != m.Content || again.Counts != m.Counts {
		t.Errorf("imported back from SpatialData, the segmentation differs: %+v %+v, exported %+v %+v", again.Counts, again.Content, m.Counts, m.Content)
	}
	orig, _ := segment.ReadCells(from)
	cells, _ := segment.ReadCells(back)
	for i := range orig {
		if i >= len(cells) || cells[i].ID != orig[i].ID {
			t.Fatalf("imported back: cell %d is not %s", i, orig[i].ID)
		}
	}

	// What the maintainer's check read.
	raw, err := os.ReadFile(filepath.Join("..", "..", "validation", "reference", "exports.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		GeneratedBy string `json:"generated_by"`
		Exports     map[string]struct {
			Counts  Totals
			Content Content
			Formats map[string]struct {
				Agrees bool
			}
		}
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	r, ok := ref.Exports[name+"-instrument"]
	if !ok {
		t.Fatalf("validation/reference/exports.json has no %s-instrument: run validation/reference/read_exports.py", name)
	}
	if r.Content != m.Content || r.Counts != m.Counts {
		t.Errorf("the export differs from what %s read (validation/reference/exports.json): run read_exports.py again", ref.GeneratedBy)
	}
	for _, f := range Formats {
		if !r.Formats[f].Agrees {
			t.Errorf("%s: the maintainer's check did not agree with the manifest", f)
		}
	}
}
