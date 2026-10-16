package label

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
)

type importFixtures struct {
	Cells      []string   `json:"cells"`
	Label      []*string  `json:"label"`
	Confidence []*float64 `json:"confidence"`
}

func readFixtures(t *testing.T) importFixtures {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f importFixtures
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// Tables written by pandas, pyarrow and anndata (testdata/make_fixtures.py) read exactly: each cell's
// label and confidence as written, the columns' roles found, cells the table does not name counted.
func TestImportedTablesReadExactly(t *testing.T) {
	want := readFixtures(t)
	cases := []struct {
		file, format, id, label, path string
		confidence                    bool
	}{
		{"labels.csv", FormatCSV, "cell_id", "cell_type", "", true},
		{"labels.tsv.gz", FormatCSV, "cell_id", "cell_type", "", true},
		{"labels.parquet", FormatParquet, "cell_id", "cell_type", "", true},
		{"labels_v3.zarr", FormatAnnData, "cell", "cell_type", "obs", true},
		{"labels_v2.zarr", FormatAnnData, "cell", "cell_type", "obs", true},
		{"spatialdata.zarr", FormatAnnData, "cell_id", "cell_type", "tables/table/obs", true},
		{"explorer.csv", FormatExplorer, "cell_id", "group", "", false},
	}
	for _, c := range cases {
		tab, err := ReadTable(filepath.Join("testdata", c.file))
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		info, roles := Describe(tab)
		if tab.Format != c.format || tab.Path != c.path || roles.ID != c.id || roles.Label != c.label || (roles.Confidence != "") != c.confidence || len(tab.SHA256) != 64 {
			t.Errorf("%s: format %s, path %q, roles %+v", c.file, tab.Format, tab.Path, roles)
			continue
		}
		if c.file == "labels.csv" && (info[0].Kind != "text" || !info[0].Unique || info[2].Kind != "number" || info[2].Missing != 1 || info[1].Distinct != 4) {
			t.Errorf("%s: columns %+v", c.file, info)
		}
		imp, err := Import(tab, ImportSpec{IDColumn: roles.ID, Column: roles.Label, ConfidenceColumn: roles.Confidence, ColorColumn: roles.Color}, want.Cells)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		// Xenium Explorer's groups leave out the cell without a label.
		rows := map[bool]int{true: 7, false: 8}[c.format == FormatExplorer]
		if imp.Match.Matched != rows || imp.Match.CellsNotNamed != 10-rows || imp.Match.Rows != rows {
			t.Errorf("%s: match %+v", c.file, imp.Match)
		}
		for i := range want.Cells {
			got := ""
			if imp.Label[i] >= 0 {
				got = imp.Labels[imp.Label[i]]
			}
			w := ""
			if want.Label[i] != nil {
				w = *want.Label[i]
			}
			if got != w {
				t.Errorf("%s: cell %s labeled %q, written %q", c.file, want.Cells[i], got, w)
			}
			wc := float32(math.NaN())
			if c.confidence && want.Confidence[i] != nil && want.Label[i] != nil {
				wc = float32(*want.Confidence[i])
			}
			if gc := imp.Confidence[i]; !(gc == wc || math.IsNaN(float64(gc)) && math.IsNaN(float64(wc))) {
				t.Errorf("%s: cell %s confidence %v, written %v", c.file, want.Cells[i], gc, wc)
			}
		}
		// The labels by decreasing cells: three Tumour, two T cells, then one each by name.
		if strings.Join(imp.Labels, "|") != "Tumour|T cell|Fibroblast, myo|Macrophage (M2)" {
			t.Errorf("%s: labels %v", c.file, imp.Labels)
		}
		if c.format == FormatExplorer && strings.Join(imp.Colors, " ") != "#1f77b4 #ff0055 #9467bd #2ca02c" {
			t.Errorf("%s: colors %v", c.file, imp.Colors)
		}
	}
}

// A table naming a cell the segmentation does not have is refused, with how many; so is one naming
// a cell twice, a label column of numbers as confidence, and a missing column.
func TestMismatchedTablesRefused(t *testing.T) {
	want := readFixtures(t)
	read := func(name string) *Table {
		tab, err := ReadTable(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return tab
	}
	spec := ImportSpec{IDColumn: "cell_id", Column: "cell_type"}
	if _, err := Import(read("other.csv"), spec, want.Cells); err == nil || !strings.Contains(err.Error(), "1 of other.csv's 5 rows name cells this segmentation does not have (zzzzzzzz-1)") {
		t.Errorf("another segmentation's cell: %v", err)
	}
	if _, err := Import(read("twice.csv"), spec, want.Cells); err == nil || !strings.Contains(err.Error(), "1 of twice.csv's rows name a cell another row names (aaaaeeee-1)") {
		t.Errorf("a cell named twice: %v", err)
	}
	labels := read("labels.csv")
	if _, err := Import(labels, ImportSpec{IDColumn: "cell_id", Column: "cell_type", ConfidenceColumn: "sample"}, want.Cells); err == nil || !strings.Contains(err.Error(), "holds text") {
		t.Errorf("a confidence column of text: %v", err)
	}
	if _, err := Import(labels, ImportSpec{IDColumn: "cell_id", Column: "celltype"}, want.Cells); err == nil || !strings.Contains(err.Error(), `no column "celltype"`) {
		t.Errorf("a missing column: %v", err)
	}
	if _, err := ReadTable(filepath.Join("testdata", "expected.json")); err == nil {
		t.Error("a JSON file read as a table")
	}
}

// Imported labels' files: written, checked, the same digest for the same table; a label moved is
// caught.
func TestImportedLabelFiles(t *testing.T) {
	want := readFixtures(t)
	tab, err := ReadTable(filepath.Join("testdata", "explorer.csv"))
	if err != nil {
		t.Fatal(err)
	}
	imp, err := Import(tab, ImportSpec{IDColumn: "cell_id", Column: "group", ColorColumn: "color"}, want.Cells)
	if err != nil {
		t.Fatal(err)
	}
	seg := cluster.Side{ID: "seg-1", Name: "Instrument segmentation"}
	var digests []string
	for k := 0; k < 2; k++ {
		dir := t.TempDir()
		if err := WriteImported(dir, seg, imp); err != nil {
			t.Fatal(err)
		}
		var m artifacts.Manifest
		if findings := Check(dir, &m); len(findings) != 0 {
			t.Fatalf("check: %+v", findings)
		}
		var s Summary
		json.Unmarshal(m.Summary, &s)
		if s.Cells != 10 || s.Labeled[0] != 7 || s.Unlabeled != 3 || s.Labels != 4 || s.Source == nil || s.Source.Format != FormatExplorer {
			t.Errorf("summary %+v", s)
		}
		d, set, lab, err := Read(dir)
		if err != nil || set != nil || d.Sets[1].Color != "#ff0055" || d.Levels[0] != "group" || len(FilesOf(d)) != 2 {
			t.Fatalf("read: %v %v %+v", err, set, d.Sets)
		}
		if lab.Label[0][0] != 0 || lab.Label[0][4] != -1 {
			t.Errorf("cells' labels %v", lab.Label[0])
		}
		digest, err := Digest(dir)
		if err != nil {
			t.Fatal(err)
		}
		digests = append(digests, digest)
		if k == 1 {
			imp.Label[0] = 1
			WriteImported(dir, seg, imp)
			if findings := Check(dir, &m); len(findings) == 0 {
				t.Error("a cell moved to another label passed the check")
			}
		}
	}
	if digests[0] != digests[1] {
		t.Errorf("digests %s and %s", digests[0], digests[1])
	}
}
