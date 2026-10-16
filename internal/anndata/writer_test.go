package anndata

import (
	"fmt"
	"path/filepath"
	"testing"

	"situcraft/internal/zarr"
)

func sampleObject() *Object {
	o := &Object{
		Obs: WriteFrame{Index: []string{"c1", "c2", "c3"}, Columns: []WriteColumn{
			{Name: "cell_id", Text: []string{"c1", "c2", "c3"}},
			{Name: "region", Text: []string{"cells", "cells", "cells"}, Categorical: true},
			{Name: "area", Float64: []float64{10.5, 20, 0}},
			{Name: "transcripts", Uint32: []uint32{3, 0, 7}},
			{Name: "x", Float32: []float32{1.5, 2.5, 3.5}},
			{Name: "label", Int64: []int64{1, 2, 3}},
		}},
		Var:   WriteFrame{Index: []string{"EPCAM", "VIM"}, Columns: []WriteColumn{{Name: "gene_ids", Text: []string{"ENSG1", "ENSG2"}}}},
		Obsm:  map[string]Dense{"spatial": {Rows: 3, Cols: 2, Data: []float64{1.5, 4, 2.5, 5, 3.5, 6}}},
		Uns:   map[string]any{"situcraft": map[string]any{"version": "0.2.0", "cells": 3}, "genes": []string{"EPCAM", "VIM"}},
		Attrs: map[string]any{"region": "cells"},
	}
	o.X.Indptr = []int64{0, 2, 2, 3}
	o.X.Indices = []int32{0, 1, 1}
	o.X.Data = []float32{1, 2, 7}
	return o
}

// An object written in each format reads back as written.
func TestWriteRoundTrip(t *testing.T) {
	for _, format := range []int{2, 3} {
		t.Run(fmt.Sprint("format ", format), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "t.zarr")
			w, err := zarr.NewWriter(root, format, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := Write(w, "table", sampleObject()); err != nil {
				t.Fatal(err)
			}
			s := zarr.OpenDir(root)
			obs, err := ReadFrame(s, "table/obs")
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(obs.Index.Text) != "[c1 c2 c3]" || len(obs.Columns) != 6 {
				t.Fatalf("obs: %v, %d columns", obs.Index.Text, len(obs.Columns))
			}
			for name, want := range map[string]string{"cell_id": "c2", "region": "cells", "area": "20", "transcripts": "0", "x": "2.5", "label": "2"} {
				if c := obs.Column(name); c == nil || c.String(1) != want {
					t.Fatalf("obs %s: %v", name, c)
				}
			}
			m, err := ReadMatrix(s, "table/X")
			if err != nil {
				t.Fatal(err)
			}
			if m.Rows != 3 || m.Cols != 2 || fmt.Sprint(m.RowSums()) != "[3 0 7]" || fmt.Sprint(m.Indices) != "[0 1 1]" {
				t.Fatalf("X: %+v", m)
			}
			if attrs, _ := zarr.Attributes(s, "table"); attrs["region"] != "cells" || attrs["encoding-type"] != "anndata" {
				t.Fatalf("attributes %v", attrs)
			}
			a, err := zarr.OpenArray(s, "table/uns/situcraft/version")
			if err != nil {
				t.Fatal(err)
			}
			if v, err := a.ReadStrings(); err != nil || v[0] != "0.2.0" {
				t.Fatalf("uns version %v: %v", v, err)
			}
		})
	}
}

func TestWriteRefusals(t *testing.T) {
	w, _ := zarr.NewWriter(filepath.Join(t.TempDir(), "t.zarr"), 3, nil)
	o := sampleObject()
	o.X.Indices[0] = 5
	if err := Write(w, "a", o); err == nil {
		t.Fatal("a column outside the matrix was written")
	}
	o = sampleObject()
	o.Obs.Columns[2].Float64 = []float64{1}
	if err := Write(w, "b", o); err == nil {
		t.Fatal("a short column was written")
	}
	o = sampleObject()
	o.Uns["bad"] = []int{1}
	if err := Write(w, "c", o); err == nil {
		t.Fatal("an unwritable uns value was written")
	}
}

// A matrix in sparse rows of small chunks is walked row by row as ReadMatrix reads it whole.
func TestEachRowReadsChunkByChunk(t *testing.T) {
	root := filepath.Join(t.TempDir(), "m.zarr")
	w, err := zarr.NewWriter(root, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Four rows over five columns: 0 2 | (empty) | 1 3 4 | 0 1 2 3 4; chunks of three elements.
	indptr := []int64{0, 2, 2, 5, 10}
	indices := []int32{0, 2, 1, 3, 4, 0, 1, 2, 3, 4}
	data := []float32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	x := enc("csr_matrix", "0.1.0")
	x["shape"] = []int{4, 5}
	if err := w.Group("X", x); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteArray("X/indptr", "<i8", []int{len(indptr)}, int64s(indptr), 2, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteArray("X/indices", "<i4", []int{len(indices)}, int32s(indices), 3, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteArray("X/data", "<f4", []int{len(data)}, float32s(data), 3, nil); err != nil {
		t.Fatal(err)
	}
	s := zarr.OpenDir(root)
	m, err := ReadMatrix(s, "X")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	rows, cols, err := EachRow(s, "X", func(row int, c []int64, v []float64) error {
		lo, hi := m.Indptr[row], m.Indptr[row+1]
		if fmt.Sprint(c) != fmt.Sprint(m.Indices[lo:hi]) || fmt.Sprint(v) != fmt.Sprint(m.Data[lo:hi]) {
			t.Errorf("row %d: %v %v; whole %v %v", row, c, v, m.Indices[lo:hi], m.Data[lo:hi])
		}
		got = append(got, fmt.Sprint(c, v))
		return nil
	})
	if err != nil || rows != 4 || cols != 5 || len(got) != 4 || got[1] != "[] []" || got[3] != "[0 1 2 3 4] [6 7 8 9 10]" {
		t.Errorf("%d × %d: %v (%v)", rows, cols, got, err)
	}
}
