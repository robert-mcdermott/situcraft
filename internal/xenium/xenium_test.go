package xenium

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestCellIDs(t *testing.T) {
	// The first cell of the Lung bundle: cells.zarr.zip's cell_id [362568, 1] is cells.parquet's
	// "aaafiiei-1".
	if got := CellIDString(362568, 1); got != "aaafiiei-1" {
		t.Fatalf("CellIDString = %q", got)
	}
	random := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 1000; i++ {
		prefix, suffix := random.Uint32(), uint32(random.IntN(100))
		p, s, ok := ParseCellID(CellIDString(prefix, suffix))
		if !ok || p != prefix || s != suffix {
			t.Fatalf("round trip of %d-%d", prefix, suffix)
		}
	}
	for _, bad := range []string{"", "aaafiiei", "aaafiiq1-1", "aaafiiei-x", "aaa-1", "12345"} {
		if _, _, ok := ParseCellID(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestFeatureKindsAndVersions(t *testing.T) {
	kinds := map[string]string{
		"EPCAM": KindGene, "NegControlProbe_00042": KindNegControlProbe, "NegControlCodeword_0500": KindNegControlCode,
		"UnassignedCodeword_0499": KindUnassignedCode, "BLANK_0006": KindUnassignedCode, "DeprecatedCodeword_0010": KindDeprecatedCode,
	}
	for name, want := range kinds {
		if got := FeatureKind(name); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	versions := map[string]string{"xenium-2.0.0.10": "xenium 2.0.0", "xenium-ranger-3.1.0.4": "xenium-ranger 3.1.0", "xenium-1.9": "xenium 1.9.0"}
	for raw, want := range versions {
		v, ok := ParseVersion(raw)
		if !ok || v.Software+" "+v.String() != want {
			t.Errorf("%s: %+v", raw, v)
		}
	}
	if v, ok := ParseVersion("R&D"); ok || v.String() != "unknown" && v.Raw != "R&D" {
		t.Errorf("R&D: %+v", v)
	}
}

func TestPolygonProblems(t *testing.T) {
	cases := []struct {
		name string
		x, y []float32
		want string
	}{
		{"closed square", []float32{0, 1, 1, 0, 0}, []float32{0, 0, 1, 1, 0}, ""},
		{"open square", []float32{0, 1, 1, 0}, []float32{0, 0, 1, 1}, ""},
		{"repeated vertices", []float32{0, 1, 1, 1, 0, 0}, []float32{0, 0, 0, 1, 1, 0}, ""},
		{"bow tie", []float32{0, 2, 2, 0, 0}, []float32{0, 2, 0, 1, 0}, "self-intersecting"},
		{"pinched", []float32{0, 2, 2, 1, 0, 1, 0}, []float32{0, 0, 2, 0, 2, 0, 0}, "self-intersecting"},
		{"two points", []float32{0, 1, 0}, []float32{0, 1, 0}, "degenerate"},
		{"collinear", []float32{0, 1, 2, 0}, []float32{0, 1, 2, 0}, "zero-area"},
		{"not a number", []float32{0, 1, float32(math.NaN()), 0}, []float32{0, 0, 1, 0}, "non-finite"},
	}
	for _, c := range cases {
		if got := polygonProblem(c.x, c.y); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMetricsAndMatrixMarket(t *testing.T) {
	m, err := parseMetrics([]byte("run_name,num_cells_detected,fraction_transcripts_assigned,stain_definition\nrun,11898,0.979724287837387,\n"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := m.Number("num_cells_detected"); v != 11898 {
		t.Fatalf("num_cells_detected %v", v)
	}
	if _, ok := m.Number("stain_definition"); ok {
		t.Fatal("an empty metric read as a number")
	}
	gz := func(s string) []byte {
		var b bytes.Buffer
		w := gzip.NewWriter(&b)
		w.Write([]byte(s))
		w.Close()
		return b.Bytes()
	}
	files := map[string][]byte{
		"features.tsv.gz": gz("ENSG1\tACE\tGene Expression\nNegControlProbe_00001\tNegControlProbe_00001\tNegative Control Probe\n"),
		"barcodes.tsv.gz": gz("aaaaaaab-1\naaaaaaac-1\naaaaaaad-1\n"),
		"matrix.mtx.gz":   gz("%%MatrixMarket matrix coordinate integer general\n%comment\n2 3 3\n1 1 5\n2 3 1\n1 3 2\n"),
	}
	mx, err := readMEX(files, "test/")
	if err != nil {
		t.Fatal(err)
	}
	if mx.Features[1].Kind != KindNegControlProbe || len(mx.CellIDs) != 3 || mx.Indptr[1] != 2 || mx.Data[0]+mx.Data[1] != 7 {
		t.Fatalf("matrix %+v", mx)
	}
	files["matrix.mtx.gz"] = gz("%%MatrixMarket matrix coordinate integer general\n2 3 1\n3 1 5\n")
	if _, err := readMEX(files, "test/"); err == nil {
		t.Fatal("an entry outside the matrix was accepted")
	}
}

// --- The development bundles -------------------------------------------------------------------

func bundleDir(t *testing.T, name string) string {
	t.Helper()
	root := os.Getenv("SITUCRAFT_DATA")
	if root == "" {
		root = filepath.Join("..", "..", "validation", "cache")
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(filepath.Join(dir, manifestName)); err != nil {
		want := os.Getenv("SITUCRAFT_REQUIRE_DATA")
		if want == "1" || strings.Contains(","+want+",", ","+name+",") {
			t.Fatalf("%s is missing (node validation/fetch.mjs %s)", dir, name)
		}
		t.Skipf("%s is missing (node validation/fetch.mjs %s)", dir, name)
	}
	return dir
}

func validate(t *testing.T, path string) *Report {
	t.Helper()
	b, err := Open(path, Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	r, err := Validate(context.Background(), b, ValidateOptions{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// codes is the findings' codes with their counts, sorted, e.g. "info boundaries.missing 255".
func codes(r *Report) []string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, string(f.Severity)+" "+f.Code+" "+itoa(f.Count))
	}
	sort.Strings(out)
	return out
}

func itoa(n int64) string {
	if n == 0 {
		return "-"
	}
	return strings.TrimSpace(strings.Repeat(" ", 0) + formatNumber(float64(n)))
}

// fingerprint hashes every file of a folder, to show that nothing was written to it.
func fingerprint(t *testing.T, dir string) map[string]string {
	t.Helper()
	sums := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		io.Copy(h, f)
		info, _ := f.Stat()
		sums[p] = hex.EncodeToString(h.Sum(nil)) + " " + info.ModTime().String()
		return nil
	})
	return sums
}

// Both development bundles validate with no fatal finding and no warning; every metric the run
// records that SituCraft recomputes (twelve in each) is reproduced; what remains is information,
// all of it expected. The self-intersecting polygons are the ones Shapely 2 (GEOS) finds invalid
// in the same files (44 and 54 in the Lung bundle, 10 and 308 in the Breast one).
func TestDevelopmentBundles(t *testing.T) {
	cases := []struct {
		name             string
		cells            int
		transcripts, q20 int64
		channels         int
		medianPerCell    float64
		want             []string
		segmentation     []int64
	}{
		{"xenium-lung-2fov", 11898, 825885, 746213, 1, 56,
			[]string{"info boundaries.self-intersecting 44", "info boundaries.self-intersecting 54", "info transcripts.at-edge 7"}, []int64{0, 0, 11898, 0}},
		{"xenium-breast-2fov", 7275, 1113950, 1035543, 4, 109,
			[]string{"info boundaries.missing 255", "info boundaries.self-intersecting 10", "info boundaries.self-intersecting 308"}, []int64{3300, 3913, 62, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := bundleDir(t, c.name)
			before := fingerprint(t, dir)
			r := validate(t, dir)
			s := r.Summary
			if got := codes(r); strings.Join(got, "; ") != strings.Join(c.want, "; ") {
				t.Errorf("findings:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(c.want, "\n  "))
			}
			if s.Cells != c.cells || s.Transcripts != c.transcripts || s.Q20GeneTranscripts != c.q20 || len(s.Channels) != c.channels || s.MedianPerCell != c.medianPerCell {
				t.Errorf("summary %+v", s)
			}
			// The run's metrics (and two values of the manifest) recomputed from the data: research.md
			// §1.3b lists them with their definitions.
			if s.MetricsCompared != 22 || s.MetricsAgreeing != 22 || len(s.Metrics) != 22 {
				t.Errorf("%d of %d metrics agree", s.MetricsAgreeing, s.MetricsCompared)
			}
			names := map[string]bool{}
			for _, m := range s.Metrics {
				names[m.Name] = true
				if !m.Agrees {
					t.Errorf("%s: recorded %v, computed %v", m.Name, m.Recorded, m.Computed)
				}
			}
			for _, name := range []string{"fraction_transcripts_decoded_q20", "nuclear_transcripts_per_100um2", "median_genes_per_cell", "fraction_empty_cells",
				"negative_control_probe_counts_per_control_per_cell", "estimated_number_of_false_positive_transcripts_per_cell", "segmented_cell_stain_frac"} {
				if !names[name] {
					t.Errorf("%s was not recomputed", name)
				}
			}
			if len(s.RunMetrics) != 48 || s.RunMetrics[0].Name != "run_name" {
				t.Errorf("%d recorded metrics, the first %+v", len(s.RunMetrics), s.RunMetrics[0])
			}
			if s.ObservedGenes != s.PanelGenes || s.ZPlanes != 11 || r.Support != "supported" || r.Version != "2.0.0" {
				t.Errorf("genes %d of %d, z planes %d, support %s %s", s.ObservedGenes, s.PanelGenes, s.ZPlanes, r.Support, r.Version)
			}
			if strings.Join(int64s(s.SegmentationCounts), ",") != strings.Join(int64s(c.segmentation), ",") {
				t.Errorf("segmentation counts %v", s.SegmentationCounts)
			}
			after := fingerprint(t, dir)
			if len(after) != len(before) {
				t.Fatalf("the bundle's files changed: %d before, %d after", len(before), len(after))
			}
			for p, sum := range before {
				if after[p] != sum {
					t.Errorf("%s changed", p)
				}
			}
		})
	}
}

func int64s(v []int64) []string {
	out := make([]string, len(v))
	for i, x := range v {
		out[i] = formatNumber(float64(x))
	}
	return out
}

// The Lung bundle zipped (images stored, everything else deflated, inside one folder, as 10x
// publishes bundles) validates as the folder does, with one more piece of information.
func TestZippedBundle(t *testing.T) {
	dir, err := filepath.EvalSymlinks(bundleDir(t, "xenium-lung-2fov"))
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "Xenium_V1_human_Lung_2fov_outs.zip")
	out, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(out)
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		method := zip.Deflate
		if strings.HasSuffix(p, ".tif") || strings.HasSuffix(p, ".zip") || strings.HasSuffix(p, ".gz") {
			method = zip.Store
		}
		dst, err := w.CreateHeader(&zip.FileHeader{Name: "outs/" + filepath.ToSlash(rel), Method: method})
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(dst, src)
		return err
	})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out.Close()
	folder, zipped := validate(t, dir), validate(t, archive)
	want := append(codes(folder), "info files.zipped -")
	sort.Strings(want)
	if got := codes(zipped); strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("zipped findings %v, want %v", got, want)
	}
	if zipped.Summary.Cells != folder.Summary.Cells || zipped.Summary.Q20GeneTranscripts != folder.Summary.Q20GeneTranscripts || zipped.Summary.MetricsAgreeing != 22 || !zipped.Summary.Zipped {
		t.Errorf("zipped summary %+v", zipped.Summary)
	}
}

// Broken bundles, each the Lung bundle with one thing changed (in a temporary folder of links to
// its files), raise exactly the findings they should.
func TestBrokenBundles(t *testing.T) {
	lung := bundleDir(t, "xenium-lung-2fov")
	// The cases that borrow a file of the Breast bundle need it; the others run with Lung alone
	// (each commit's CI fetches only the Lung bundle).
	breast := filepath.Join(filepath.Dir(lung), "xenium-breast-2fov")
	if _, err := os.Stat(filepath.Join(breast, manifestName)); err != nil {
		breast = ""
	}
	replaceText := func(file, old, new string) func(t *testing.T, dst string) {
		return func(t *testing.T, dst string) {
			raw, err := os.ReadFile(filepath.Join(lung, file))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), old) {
				t.Fatalf("%s does not contain %q", file, old)
			}
			replaceFile(t, dst, file, []byte(strings.Replace(string(raw), old, new, 1)))
		}
	}
	fromBreast := func(files ...string) func(t *testing.T, dst string) {
		return func(t *testing.T, dst string) {
			if breast == "" {
				t.Skip("the Breast bundle is not present (node validation/fetch.mjs xenium-breast-2fov)")
			}
			for _, f := range files {
				os.Remove(filepath.Join(dst, f))
				if err := os.Symlink(filepath.Join(absolute(t, breast), f), filepath.Join(dst, f)); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	cases := []struct {
		name   string
		change func(t *testing.T, dst string)
		// want are findings that must be raised (severity and code); every fatal one must be listed.
		want []string
	}{
		{"cells table missing", func(t *testing.T, dst string) {
			os.Remove(filepath.Join(dst, "cells.parquet"))
			os.Remove(filepath.Join(dst, "cells.csv.gz"))
		}, []string{"fatal files.missing"}},
		{"only the CSV cells table", func(t *testing.T, dst string) { os.Remove(filepath.Join(dst, "cells.parquet")) }, []string{"fatal files.unread-form"}},
		{"morphology images missing", func(t *testing.T, dst string) { os.RemoveAll(filepath.Join(dst, "morphology_focus")) }, []string{"fatal files.missing"}},
		{"only an HDF5 matrix", func(t *testing.T, dst string) {
			for _, f := range []string{"cell_feature_matrix.zarr.zip", "cell_feature_matrix.tar.gz"} {
				os.Remove(filepath.Join(dst, f))
			}
		}, []string{"fatal files.unread-form"}},
		{"transcripts cut short", func(t *testing.T, dst string) {
			raw, _ := os.ReadFile(filepath.Join(lung, "transcripts.parquet"))
			replaceFile(t, dst, "transcripts.parquet", raw[:len(raw)/2])
		}, []string{"fatal transcripts.unreadable"}},
		{"transcripts of another run", fromBreast("transcripts.parquet"), []string{"fatal transcripts.unknown-cell", "warning cells.counts-differ", "warning transcripts.not-in-panel"}},
		{"matrix of another run", fromBreast("cell_feature_matrix.zarr.zip"), []string{"fatal matrix.cells-differ"}},
		{"boundaries of another run", fromBreast("cell_boundaries.parquet"), []string{"fatal boundaries.unknown-cell", "warning boundaries.missing"}},
		{"panel of another run", fromBreast("gene_panel.json"), []string{"warning transcripts.not-in-panel"}},
		{"no pixel size", replaceText(manifestName, `"pixel_size": 0.2125`, `"pixel_size": 0`), []string{"fatal manifest.pixel-size"}},
		{"wrong pixel size", replaceText(manifestName, `"pixel_size": 0.2125`, `"pixel_size": 0.1`), []string{"warning images.pixel-size", "warning transcripts.outside-image", "warning cells.outside-image"}},
		{"a metric that disagrees", replaceText("metrics_summary.csv", ",11898,", ",11899,"), []string{"warning metrics.num_cells_detected"}},
		{"a newer version", replaceText(manifestName, `"xenium-2.0.0.10"`, `"xenium-4.1.0.2"`), []string{"warning version.untested"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst := linkedCopy(t, lung)
			c.change(t, dst)
			r := validate(t, dst)
			have := map[string]bool{}
			for _, f := range r.Findings {
				have[string(f.Severity)+" "+f.Code] = true
			}
			for _, w := range c.want {
				if !have[w] {
					t.Errorf("missing %q; findings: %v", w, codes(r))
				}
			}
			for _, f := range r.Findings {
				if f.Severity == Fatal && !contains(c.want, "fatal "+f.Code) {
					t.Errorf("unexpected fatal finding %s: %s", f.Code, f.Detail)
				}
			}
			if r.Usable() != (len(c.want) > 0 && !strings.HasPrefix(c.want[0], "fatal")) {
				t.Errorf("usable %v with findings %v", r.Usable(), codes(r))
			}
		})
	}
	// Not a bundle at all.
	if _, err := Open(t.TempDir(), Options{}); err == nil || !strings.Contains(err.Error(), "not a Xenium output bundle") {
		t.Errorf("an empty folder opened: %v", err)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func absolute(t *testing.T, p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// linkedCopy makes a folder of links to a bundle's top-level files and folders.
func linkedCopy(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.Symlink(filepath.Join(absolute(t, src), e.Name()), filepath.Join(dst, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// replaceFile puts new contents in place of a link.
func replaceFile(t *testing.T, dst, name string, data []byte) {
	t.Helper()
	os.Remove(filepath.Join(dst, name))
	if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func FuzzReadMEX(f *testing.F) {
	f.Add([]byte("ENSG1\tACE\tGene Expression\n"), []byte("aaaaaaab-1\naaaaaaac-1\n"), []byte("%%MatrixMarket matrix coordinate integer general\n1 2 2\n1 1 5\n1 2 3\n"))
	f.Fuzz(func(t *testing.T, features, barcodes, matrix []byte) {
		gz := func(b []byte) []byte {
			var out bytes.Buffer
			w := gzip.NewWriter(&out)
			w.Write(b)
			w.Close()
			return out.Bytes()
		}
		readMEX(map[string][]byte{"features.tsv.gz": gz(features), "barcodes.tsv.gz": gz(barcodes), "matrix.mtx.gz": gz(matrix)}, "fuzz/")
	})
}

func FuzzPolygonProblem(f *testing.F) {
	f.Add([]byte{0, 0, 10, 0, 10, 10, 0, 10, 0, 0})
	f.Fuzz(func(t *testing.T, coords []byte) {
		n := len(coords) / 2
		if n > 200 {
			return
		}
		x, y := make([]float32, n), make([]float32, n)
		for i := 0; i < n; i++ {
			x[i], y[i] = float32(int8(coords[2*i])), float32(int8(coords[2*i+1]))
		}
		polygonProblem(x, y)
	})
}
