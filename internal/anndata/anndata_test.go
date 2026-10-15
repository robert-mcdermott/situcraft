package anndata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"situcraft/internal/zarr"
)

// AnnData stores written by the anndata package (testdata/make_fixtures.py), in Zarr format 3
// (sharded) and 2, with X as sparse rows, sparse columns and a dense array, read as written.
func TestStoresFromAnndata(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Index, CellID, Var []string
		Region, Name       []*string
		Label              []*int64
		Kept               []bool
		Area, RowSums      []float64
	}
	want.CellID = nil
	var doc map[string]json.RawMessage
	json.Unmarshal(raw, &doc)
	json.Unmarshal(doc["index"], &want.Index)
	json.Unmarshal(doc["cell_id"], &want.CellID)
	json.Unmarshal(doc["var"], &want.Var)
	json.Unmarshal(doc["region"], &want.Region)
	json.Unmarshal(doc["name"], &want.Name)
	json.Unmarshal(doc["label"], &want.Label)
	json.Unmarshal(doc["kept"], &want.Kept)
	json.Unmarshal(doc["area"], &want.Area)
	json.Unmarshal(doc["row_sums"], &want.RowSums)
	text := func(c *Column, i int, w *string) bool {
		if w == nil {
			return c.Missing != nil && c.Missing[i]
		}
		return (c.Missing == nil || !c.Missing[i]) && c.String(i) == *w
	}
	for _, store := range []string{"v3_csr", "v3_csc", "v3_dense", "v2_csr", "v2_csc", "v2_dense"} {
		s := zarr.OpenDir(filepath.Join("testdata", store+".zarr"))
		obs, err := ReadFrame(s, "obs")
		if err != nil {
			t.Fatalf("%s: %v", store, err)
		}
		if !reflect.DeepEqual(obs.Index.Text, want.Index) || !reflect.DeepEqual(obs.Column("cell_id").Text, want.CellID) {
			t.Errorf("%s: index %q, cell_id %q", store, obs.Index.Text, obs.Column("cell_id").Text)
		}
		for i := range want.Index {
			if !text(obs.Column("region"), i, want.Region[i]) || !text(obs.Column("name"), i, want.Name[i]) {
				t.Errorf("%s: row %d: region %q, name %q", store, i, obs.Column("region").String(i), obs.Column("name").String(i))
			}
			label := obs.Column("label")
			if w := want.Label[i]; w == nil && (label.Missing == nil || !label.Missing[i]) || w != nil && label.Numbers[i] != float64(*w) {
				t.Errorf("%s: row %d: label %v", store, i, label.String(i))
			}
			if kept := obs.Column("kept").Numbers[i] == 1; kept != want.Kept[i] {
				t.Errorf("%s: row %d: kept %v", store, i, kept)
			}
			if obs.Column("area").Numbers[i] != want.Area[i] {
				t.Errorf("%s: row %d: area %v", store, i, obs.Column("area").Numbers[i])
			}
		}
		v, err := ReadFrame(s, "var")
		if err != nil || !reflect.DeepEqual(v.Index.Text, want.Var) {
			t.Errorf("%s: var %v %v", store, v, err)
		}
		m, err := ReadMatrix(s, "X")
		if err != nil {
			t.Fatalf("%s: X: %v", store, err)
		}
		if m.Rows != 4 || m.Cols != 3 || !reflect.DeepEqual(m.RowSums(), want.RowSums) {
			t.Errorf("%s: X %d × %d, row sums %v", store, m.Rows, m.Cols, m.RowSums())
		}
	}
}

func TestDamagedMatricesAreRefused(t *testing.T) {
	for name, m := range map[string]struct {
		indptr, indices []int64
		data            []float64
	}{
		"indptr too short":     {[]int64{0, 1}, []int64{0}, []float64{1}},
		"rows out of order":    {[]int64{0, 2, 1, 2}, []int64{0, 1}, []float64{1, 2}},
		"index outside":        {[]int64{0, 1, 1, 1}, []int64{7}, []float64{1}},
		"data and indices":     {[]int64{0, 1, 1, 1}, []int64{0}, []float64{1, 2}},
		"indptr not from zero": {[]int64{1, 1, 1, 1}, []int64{}, []float64{}},
	} {
		if err := checkSparse(name, 3, 3, m.indptr, m.indices, m.data); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}
