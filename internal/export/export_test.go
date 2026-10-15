package export

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"situcraft/internal/anndata"
	"situcraft/internal/parquet"
	"situcraft/internal/segment"
	"situcraft/internal/segment/imports"
	"situcraft/internal/xenium"
	"situcraft/internal/zarr"
)

// fixture is the hand-made dataset the imports' fixtures segment
// (internal/segment/imports/testdata/make_fixtures.py): 80 × 60 pixels of 0.5 µm, 240 transcripts.
type fixture struct {
	Width, Height int
	PixelSize     float64 `json:"pixel_size"`
	RunName       string  `json:"run"`
	Region        string
	List          []struct {
		ID   uint64
		X, Y float32
		Gene string
		QV   float32
	} `json:"transcripts"`
	nuclei []uint32 // the nucleus mask (nil for none)
	zipped bool
}

func (f *fixture) Name() string { return "the fixture" }
func (f *fixture) Transcripts(fn func(*xenium.TranscriptBatch) error) error {
	// Two batches, as a bundle's row groups.
	for _, part := range [][2]int{{0, 100}, {100, len(f.List)}} {
		b := &xenium.TranscriptBatch{Rows: part[1] - part[0]}
		for _, t := range f.List[part[0]:part[1]] {
			b.TranscriptID = append(b.TranscriptID, t.ID)
			b.X, b.Y, b.Z, b.QV = append(b.X, t.X), append(b.Y, t.Y), append(b.Z, 1.5), append(b.QV, t.QV)
			b.Feature = append(b.Feature, []byte(t.Gene))
			b.CellID = append(b.CellID, []byte("UNASSIGNED"))
		}
		if err := fn(b); err != nil {
			return err
		}
	}
	return nil
}
func (f *fixture) GeneIDs() map[string]string { return map[string]string{"EPCAM": "ENSG00000119888"} }
func (f *fixture) ImageSize() (int, int)      { return f.Width, f.Height }
func (f *fixture) BundleFolder() string {
	if f.zipped {
		return ""
	}
	return "/data/fixture bundle"
}
func (f *fixture) Run() map[string]string { return map[string]string{"run": f.RunName} }
func (f *fixture) NucleusMask() (Mask, error) {
	if f.nuclei == nil {
		return nil, nil
	}
	return &arrayMask{f.Width, f.Height, f.nuclei}, nil
}

type arrayMask struct {
	w, h int
	v    []uint32
}

func (m *arrayMask) Size() (int, int) { return m.w, m.h }
func (m *arrayMask) Close() error     { return nil }
func (m *arrayMask) Rows(y0, y1 int, dst []uint32) error {
	copy(dst, m.v[y0*m.w:y1*m.w])
	return nil
}

// importing is the fixture as the dataset an import checks.
type importing struct{ *fixture }

func (f importing) Grid() (int, int, float64) { return f.Width, f.Height, f.PixelSize }
func (f importing) Run() imports.RunIdentity {
	return imports.RunIdentity{Name: f.RunName, Region: f.Region}
}
func (f importing) Name() string { return f.fixture.Name() }

const fixtures = "../segment/imports/testdata"

func loadFixture(t *testing.T) *fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtures, "dataset.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	// Nuclei: a 4 × 4 pixel square in each of A, B and C.
	f.nuclei = make([]uint32, f.Width*f.Height)
	for k, at := range [][2]int{{10, 10}, {30, 8}, {8, 30}} {
		for y := at[1]; y < at[1]+4; y++ {
			for x := at[0]; x < at[0]+4; x++ {
				f.nuclei[y*f.Width+x] = uint32(k + 1)
			}
		}
	}
	return &f
}

// segmentation imports a fixture store as SituCraft's segmentation files.
func segmentation(t *testing.T, ds *fixture, opt imports.Options) string {
	t.Helper()
	dir := t.TempDir()
	if _, _, _, err := imports.Run(context.Background(), importing{ds}, opt, dir, 2, nil); err != nil {
		t.Fatal(err)
	}
	return dir
}

type loaded struct {
	cells  []segment.Cell
	raster *segment.Raster
	assign []uint32
	counts *segment.Counts
}

func load(t *testing.T, dir string) *loaded {
	t.Helper()
	l := &loaded{}
	var err error
	if l.cells, err = segment.ReadCells(dir); err != nil {
		t.Fatal(err)
	}
	if l.raster, err = segment.ReadMask(dir); err != nil {
		t.Fatal(err)
	}
	if l.assign, err = segment.ReadAssignments(dir); err != nil {
		t.Fatal(err)
	}
	if l.counts, err = segment.ReadCounts(dir); err != nil {
		t.Fatal(err)
	}
	return l
}

func (l *loaded) id(k uint32) string {
	if k == 0 {
		return ""
	}
	return l.cells[k-1].ID
}

// counts is each cell's counts by gene, as text.
func (l *loaded) countsOf(id string) string {
	for i, c := range l.cells {
		if c.ID == id {
			var parts []string
			for k := l.counts.Offsets[i]; k < l.counts.Offsets[i+1]; k++ {
				parts = append(parts, fmt.Sprintf("%s=%d", l.counts.Genes[l.counts.Gene[k]], l.counts.Count[k]))
			}
			return strings.Join(parts, " ")
		}
	}
	return "missing"
}

var seg = Segmentation{ID: "seg-fixture", Name: "Fixture segmentation", Method: "SpatialData import"}

func exportAll(t *testing.T, ds *fixture, from, folder string) *Manifest {
	t.Helper()
	m, err := Run(context.Background(), ds, from, seg, Options{Formats: Formats, Folder: folder, Software: "situcraft test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Every format written, and read back by SituCraft's own readers: the SpatialData store imports as
// the same segmentation (every pixel, every transcript's cell, every count); the GeoJSON polygons
// with the transcripts table import with every transcript's cell; the tables, the AnnData object,
// the Matrix Market files and the masks hold what the segmentation does.
func TestExportsReadBack(t *testing.T) {
	ds := loadFixture(t)
	from := segmentation(t, ds, imports.Options{Format: imports.SpatialData, Paths: []string{filepath.Join(fixtures, "labels.zarr")}, Units: "pixels"})
	orig := load(t, from)
	out := filepath.Join(t.TempDir(), "export")
	m := exportAll(t, ds, from, out)
	if m.Counts.Cells != 7 || m.Counts.CellsWithOutline != 6 || m.Counts.Transcripts != 240 || m.Counts.Genes != 4 {
		t.Fatalf("totals %+v", m.Counts)
	}
	if len(m.Formats) != len(Formats) {
		t.Fatalf("formats %v", m.Formats)
	}
	if bad, err := Verify(out); err != nil || len(bad) > 0 {
		t.Fatalf("verify: %v %v", bad, err)
	}

	// SpatialData, imported back: the same segmentation.
	back := load(t, segmentation(t, ds, imports.Options{Format: imports.SpatialData, Paths: []string{filepath.Join(out, "spatialdata.zarr")}}))
	if len(back.cells) != len(orig.cells) {
		t.Fatalf("%d cells back of %d", len(back.cells), len(orig.cells))
	}
	row, rowBack := make([]uint32, orig.raster.Width), make([]uint32, orig.raster.Width)
	for y := 0; y < orig.raster.Height; y++ {
		orig.raster.Row(y, row)
		back.raster.Row(y, rowBack)
		for x := range row {
			if orig.id(row[x]) != back.id(rowBack[x]) {
				t.Fatalf("pixel %d, %d: %q back as %q", x, y, orig.id(row[x]), back.id(rowBack[x]))
			}
		}
	}
	for i := range orig.assign {
		if orig.id(orig.assign[i]) != back.id(back.assign[i]) {
			t.Fatalf("transcript %d: %q back as %q", i, orig.id(orig.assign[i]), back.id(back.assign[i]))
		}
	}
	for _, c := range orig.cells {
		if a, b := orig.countsOf(c.ID), back.countsOf(c.ID); a != b {
			t.Fatalf("cell %s counts %s back as %s", c.ID, a, b)
		}
	}

	// GeoJSON with the transcripts table: every transcript's cell.
	geo := load(t, segmentation(t, ds, imports.Options{Format: imports.GeoJSON, Paths: []string{filepath.Join(out, "cell_boundaries.geojson"), filepath.Join(out, "parquet", "transcripts.parquet")}}))
	for i := range orig.assign {
		if orig.id(orig.assign[i]) != geo.id(geo.assign[i]) {
			t.Fatalf("GeoJSON: transcript %d: %q back as %q", i, orig.id(orig.assign[i]), geo.id(geo.assign[i]))
		}
	}
	// And the CSV table.
	geoCSV := load(t, segmentation(t, ds, imports.Options{Format: imports.GeoJSON, Paths: []string{filepath.Join(out, "cell_boundaries.geojson"), filepath.Join(out, "csv", "transcripts.csv.gz")}}))
	for i := range orig.assign {
		if orig.id(orig.assign[i]) != geoCSV.id(geoCSV.assign[i]) {
			t.Fatalf("GeoJSON with CSV: transcript %d: %q back as %q", i, orig.id(orig.assign[i]), geoCSV.id(geoCSV.assign[i]))
		}
	}

	// The Parquet and CSV tables.
	for _, format := range []string{Parquet, CSV} {
		tables := readTables(t, filepath.Join(out, format), format)
		if n := len(tables["cells"]); n != len(orig.cells) {
			t.Fatalf("%s: %d cells", format, n)
		}
		for i, c := range orig.cells {
			r := tables["cells"][i]
			if r["cell_id"] != c.ID || r["transcript_counts"] != fmt.Sprint(c.Transcripts) || r["cell_area"] != strconv.FormatFloat(float64(c.Area), 'g', -1, 64) {
				t.Fatalf("%s: cell %d: %v, want %+v", format, i, r, c)
			}
		}
		if n := len(tables["transcripts"]); n != 240 {
			t.Fatalf("%s: %d transcripts", format, n)
		}
		for i, r := range tables["transcripts"] {
			want := orig.id(orig.assign[i])
			if want == "" {
				want = "UNASSIGNED"
			}
			if r["cell_id"] != want || r["transcript_id"] != fmt.Sprint(ds.List[i].ID) || r["feature_name"] != ds.List[i].Gene || r["z_location"] != "1.5" {
				t.Fatalf("%s: transcript %d: %v", format, i, r)
			}
		}
		var sum uint64
		for _, r := range tables["counts"] {
			v, _ := strconv.ParseUint(r["count"], 10, 32)
			sum += v
		}
		if sum != m.Counts.Sum || len(tables["counts"]) != m.Counts.Entries {
			t.Fatalf("%s: %d counts summing to %d", format, len(tables["counts"]), sum)
		}
		// Each outline closed, as Xenium writes them.
		first := map[string][2]string{}
		last := map[string][2]string{}
		for _, r := range tables["cell_boundaries"] {
			if _, ok := first[r["cell_id"]]; !ok {
				first[r["cell_id"]] = [2]string{r["vertex_x"], r["vertex_y"]}
			}
			last[r["cell_id"]] = [2]string{r["vertex_x"], r["vertex_y"]}
		}
		if len(first) != 6 {
			t.Fatalf("%s: %d outlines", format, len(first))
		}
		for id := range first {
			if first[id] != last[id] {
				t.Fatalf("%s: %s's outline is not closed", format, id)
			}
		}
	}

	// AnnData, and the SpatialData store's table.
	for _, where := range []struct{ root, table string }{{"anndata.zarr", ""}, {"spatialdata.zarr", "tables/table"}} {
		s := zarr.OpenDir(filepath.Join(out, where.root))
		at := func(p string) string {
			if where.table == "" {
				return p
			}
			return where.table + "/" + p
		}
		obs, err := anndata.ReadFrame(s, at("obs"))
		if err != nil {
			t.Fatal(err)
		}
		x, err := anndata.ReadMatrix(s, at("X"))
		if err != nil {
			t.Fatal(err)
		}
		sums := x.RowSums()
		for i, c := range orig.cells {
			if obs.Index.Text[i] != c.ID || sums[i] != float64(c.Transcripts) {
				t.Fatalf("%s: cell %d is %s with %g, want %s with %d", where.root, i, obs.Index.Text[i], sums[i], c.ID, c.Transcripts)
			}
		}
		v, err := anndata.ReadFrame(s, at("var"))
		if err != nil || fmt.Sprint(v.Index.Text) != fmt.Sprint(orig.counts.Genes) || v.Column("gene_ids").Text[1] != "ENSG00000119888" {
			t.Fatalf("%s: var %v: %v", where.root, v, err)
		}
	}

	// The Matrix Market files.
	mex := readMEX(t, filepath.Join(out, "cell_feature_matrix"))
	for i, c := range orig.cells {
		if mex[i] != c.Transcripts {
			t.Fatalf("MEX: cell %s has %d, want %d", c.ID, mex[i], c.Transcripts)
		}
	}

	// Xenium Ranger's inputs.
	w, h, cells, err := ReadNPY(filepath.Join(out, "xenium-ranger", "cells.npy"))
	if err != nil || w != orig.raster.Width || h != orig.raster.Height {
		t.Fatalf("cells.npy %d × %d: %v", w, h, err)
	}
	for y := 0; y < h; y++ {
		orig.raster.Row(y, row)
		for x := 0; x < w; x++ {
			if cells[y*w+x] != row[x] {
				t.Fatalf("cells.npy pixel %d, %d: %d, want %d", x, y, cells[y*w+x], row[x])
			}
		}
	}
	_, _, nuclei, err := ReadNPY(filepath.Join(out, "xenium-ranger", "nuclei.npy"))
	if err != nil || fmt.Sprint(nuclei) != fmt.Sprint(ds.nuclei) {
		t.Fatalf("nuclei.npy differs: %v", err)
	}
	if m.XeniumRanger == nil || !strings.Contains(m.XeniumRanger.Command, "--xenium-bundle='/data/fixture bundle'") ||
		!strings.Contains(m.XeniumRanger.Command, "--cells="+filepath.Join(out, "xenium-ranger", "cells.npy")) || m.XeniumRanger.ID != "the-fixture-Fixture-segmentation" {
		t.Fatalf("command %+v", m.XeniumRanger)
	}
	script, _ := os.ReadFile(filepath.Join(out, "xenium-ranger", "command.sh"))
	if !strings.HasPrefix(string(script), "#!/bin/sh\n") || !strings.Contains(string(script), "xeniumranger import-segmentation \\\n  --id=the-fixture-Fixture-segmentation") {
		t.Fatalf("command.sh:\n%s", script)
	}
	codes := map[string]int{}
	for _, f := range m.Findings {
		codes[f.Code] = f.Count
	}
	// G has transcripts and no pixel; the tool's assignment differs from the mask for G's, the
	// three outside every polygon given to B and the five inside A left as noise.
	if codes["ranger.cells-without-pixels"] != 1 || codes["ranger.assigned-otherwise"] == 0 {
		t.Fatalf("findings %+v", m.Findings)
	}
	if _, ok := codes["ranger.image-size"]; ok {
		t.Fatalf("the mask is the image's size: %+v", m.Findings)
	}
}

// The same segmentation gives the same files; an existing folder is replaced only when asked and
// only when it is an export; a changed file is found.
func TestExportsAreReproducibleAndSafe(t *testing.T) {
	ds := loadFixture(t)
	from := segmentation(t, ds, imports.Options{Format: imports.SpatialData, Paths: []string{filepath.Join(fixtures, "labels.zarr")}, Units: "pixels"})
	dir := t.TempDir()
	a := exportAll(t, ds, from, filepath.Join(dir, "a"))
	b := exportAll(t, ds, from, filepath.Join(dir, "b"))
	if len(a.Files) != len(b.Files) {
		t.Fatalf("%d and %d files", len(a.Files), len(b.Files))
	}
	for i := range a.Files {
		// The command names its own folder; every other file is the same.
		if a.Files[i].SHA256 != b.Files[i].SHA256 && a.Files[i].Path != "xenium-ranger/command.sh" {
			t.Fatalf("%s differs between two exports", a.Files[i].Path)
		}
	}
	if _, err := Run(context.Background(), ds, from, seg, Options{Formats: []string{GeoJSON}, Folder: filepath.Join(dir, "a"), Software: "t"}, nil); err == nil {
		t.Fatal("an existing export was replaced unasked")
	}
	m, err := Run(context.Background(), ds, from, seg, Options{Formats: []string{GeoJSON}, Folder: filepath.Join(dir, "a"), Overwrite: true, Software: "t"}, nil)
	if err != nil || len(m.Files) != 1 {
		t.Fatalf("replacing an export: %v, %v", m, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "parquet")); err == nil {
		t.Fatal("the replaced export's files remain")
	}
	plain := filepath.Join(dir, "plain")
	os.Mkdir(plain, 0o755)
	if _, err := Run(context.Background(), ds, from, seg, Options{Formats: []string{GeoJSON}, Folder: plain, Overwrite: true, Software: "t"}, nil); err == nil {
		t.Fatal("a folder that is not an export was replaced")
	}
	if _, err := Run(context.Background(), ds, from, seg, Options{Formats: []string{"hdf5"}, Folder: filepath.Join(dir, "c"), Software: "t"}, nil); err == nil {
		t.Fatal("an unknown format was written")
	}
	os.WriteFile(filepath.Join(dir, "b", "csv", "cells.csv.gz"), []byte("changed"), 0o644)
	if bad, err := Verify(filepath.Join(dir, "b")); err != nil || len(bad) != 1 || bad[0] != "csv/cells.csv.gz" {
		t.Fatalf("verify found %v: %v", bad, err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".partial-") {
			t.Fatalf("%s was left behind", e.Name())
		}
	}
}

// Without nuclei, with a zipped bundle and a mask of another size, the command says so.
func TestRangerFindings(t *testing.T) {
	ds := loadFixture(t)
	from := segmentation(t, ds, imports.Options{Format: imports.SpatialData, Paths: []string{filepath.Join(fixtures, "labels.zarr")}, Units: "pixels"})
	ds.nuclei, ds.zipped = nil, true
	ds.Width++ // the image is a pixel wider than the mask
	m, err := Run(context.Background(), ds, from, seg, Options{Formats: []string{XeniumRanger}, Folder: filepath.Join(t.TempDir(), "x"), Software: "t"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]string{}
	for _, f := range m.Findings {
		codes[f.Code] = f.Severity
	}
	if codes["ranger.no-nuclei"] != "warning" || codes["ranger.bundle-zipped"] != "warning" || codes["ranger.image-size"] != "fatal" {
		t.Fatalf("findings %+v", m.Findings)
	}
	if strings.Contains(m.XeniumRanger.Command, "--nuclei") {
		t.Fatalf("command %s", m.XeniumRanger.Command)
	}
}

func TestNPYHeader(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {60, 80}, {123456, 98765}} {
		h := npyHeader(size[0], size[1])
		if len(h)%64 != 0 || h[len(h)-1] != '\n' || !strings.Contains(string(h), fmt.Sprintf("'shape': (%d, %d)", size[0], size[1])) {
			t.Fatalf("header %q", h)
		}
	}
	if got := rangerID("Lung 2-FOV", "Nucleus expansion (5 µm)"); got != "Lung-2-FOV-Nucleus-expansion-5-m" {
		t.Fatalf("id %q", got)
	}
	if got := shellQuote("it's here"); got != `'it'\''s here'` {
		t.Fatalf("quoted %s", got)
	}
}

// readTables reads the four tables, as rows of text by column name.
func readTables(t *testing.T, dir, format string) map[string][]map[string]string {
	t.Helper()
	out := map[string][]map[string]string{}
	for _, name := range []string{"cells", "transcripts", "cell_boundaries", "counts"} {
		var rows []map[string]string
		if format == Parquet {
			raw, err := os.ReadFile(filepath.Join(dir, name+".parquet"))
			if err != nil {
				t.Fatal(err)
			}
			p, err := parquet.Open(strings.NewReader(string(raw)), int64(len(raw)))
			if err != nil {
				t.Fatal(err)
			}
			rows = make([]map[string]string, p.NumRows)
			for i := range rows {
				rows[i] = map[string]string{}
			}
			for ci, c := range p.Columns {
				v, err := p.ReadAll(ci)
				if err != nil {
					t.Fatal(err)
				}
				f := parquet.Field{Name: c.Name, Type: c.Type}
				if c.Logical == "INTEGER" && !c.Signed {
					f.Annotation = "uint"
				}
				for i := range rows {
					rows[i][c.Name] = text(f, v, i)
				}
			}
		} else {
			f, err := os.Open(filepath.Join(dir, name+".csv.gz"))
			if err != nil {
				t.Fatal(err)
			}
			gz, err := gzip.NewReader(f)
			if err != nil {
				t.Fatal(err)
			}
			all, err := csv.NewReader(gz).ReadAll()
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range all[1:] {
				m := map[string]string{}
				for i, h := range all[0] {
					m[h] = r[i]
				}
				rows = append(rows, m)
			}
		}
		out[name] = rows
	}
	return out
}

// readMEX reads the Matrix Market files: each cell's total.
func readMEX(t *testing.T, dir string) []uint32 {
	t.Helper()
	lines := func(name string) []string {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(gz)
		return strings.Split(strings.TrimSpace(string(raw)), "\n")
	}
	features, barcodes := lines("features.tsv.gz"), lines("barcodes.tsv.gz")
	matrix := lines("matrix.mtx.gz")
	if matrix[0] != "%%MatrixMarket matrix coordinate integer general" || !strings.HasPrefix(matrix[1], "%metadata_json: ") {
		t.Fatalf("matrix header %q", matrix[:2])
	}
	var genes, cells, nnz int
	fmt.Sscan(matrix[2], &genes, &cells, &nnz)
	if genes != len(features) || cells != len(barcodes) || nnz != len(matrix)-3 || !strings.HasSuffix(features[0], "\tGene Expression") {
		t.Fatalf("MEX: %d × %d with %d, %d features, %d barcodes, %d entries", genes, cells, nnz, len(features), len(barcodes), len(matrix)-3)
	}
	totals := make([]uint32, cells)
	s := bufio.NewScanner(strings.NewReader(strings.Join(matrix[3:], "\n")))
	for s.Scan() {
		var g, c int
		var v uint32
		fmt.Sscan(s.Text(), &g, &c, &v)
		totals[c-1] += v
	}
	return totals
}
