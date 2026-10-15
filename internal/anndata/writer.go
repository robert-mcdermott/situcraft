package anndata

// Writing AnnData objects into Zarr (internal/zarr's writer, format 2 or 3), following anndata's
// documented on-disk specification as Read does: obs and var as data frames (text, categorical and
// numeric columns), X in compressed sparse rows, obsm's dense arrays, and uns as nested
// dictionaries of text, numbers and lists of text. Empty layers, obsp, varm and varp are written
// as anndata writes them.

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"situcraft/internal/zarr"
)

// WriteColumn is one column of a data frame to write: one of its slices set.
type WriteColumn struct {
	Name string
	Text []string
	// Categorical writes Text as categories and codes (for repeated values, such as a region).
	Categorical bool
	Float64     []float64
	Float32     []float32
	Int64       []int64
	Int32       []int32
	Uint32      []uint32
}

func (c *WriteColumn) length() int {
	switch {
	case c.Text != nil:
		return len(c.Text)
	case c.Float64 != nil:
		return len(c.Float64)
	case c.Float32 != nil:
		return len(c.Float32)
	case c.Int64 != nil:
		return len(c.Int64)
	case c.Int32 != nil:
		return len(c.Int32)
	default:
		return len(c.Uint32)
	}
}

// WriteFrame is a data frame to write: its index (text) and its columns.
type WriteFrame struct {
	Index   []string
	Columns []WriteColumn
}

// Dense is a two-dimensional array of numbers, row by row.
type Dense struct {
	Rows, Cols int
	Data       []float64
}

// Object is an AnnData object to write.
type Object struct {
	Obs, Var WriteFrame
	// X holds len(Obs.Index) rows and len(Var.Index) columns in compressed sparse rows.
	X struct {
		Indptr  []int64
		Indices []int32
		Data    []float32
	}
	Obsm map[string]Dense
	// Uns holds text, numbers (float64, int), lists of text and nested map[string]any.
	Uns map[string]any
	// Attrs are added to the object's own attributes (SpatialData's table attributes).
	Attrs map[string]any
}

// chunkLen is the elements of a chunk of a long array.
const chunkLen = 1 << 20

func enc(kind, version string) map[string]any {
	return map[string]any{"encoding-type": kind, "encoding-version": version}
}

// Write writes the object as the group at path of the hierarchy.
func Write(w *zarr.Writer, path string, o *Object) error {
	rows, cols := len(o.Obs.Index), len(o.Var.Index)
	if len(o.X.Indptr) != rows+1 {
		return fmt.Errorf("anndata: X has %d row pointers for %d rows", len(o.X.Indptr), rows)
	}
	if len(o.X.Indices) != len(o.X.Data) || int64(len(o.X.Data)) != o.X.Indptr[rows] {
		return fmt.Errorf("anndata: X's %d indices and %d values do not end at %d", len(o.X.Indices), len(o.X.Data), o.X.Indptr[rows])
	}
	for _, c := range o.X.Indices {
		if c < 0 || int(c) >= cols {
			return fmt.Errorf("anndata: X names column %d of %d", c, cols)
		}
	}
	attrs := enc("anndata", "0.1.0")
	for k, v := range o.Attrs {
		attrs[k] = v
	}
	if err := w.Group(path, attrs); err != nil {
		return err
	}
	at := func(name string) string { return join(path, name) }
	if err := writeFrame(w, at("obs"), &o.Obs); err != nil {
		return err
	}
	if err := writeFrame(w, at("var"), &o.Var); err != nil {
		return err
	}
	x := enc("csr_matrix", "0.1.0")
	x["shape"] = []int{rows, cols}
	if err := w.Group(at("X"), x); err != nil {
		return err
	}
	if err := w.WriteArray(at("X/indptr"), "<i8", []int{len(o.X.Indptr)}, int64s(o.X.Indptr), chunkLen, nil); err != nil {
		return err
	}
	if err := w.WriteArray(at("X/indices"), "<i4", []int{len(o.X.Indices)}, int32s(o.X.Indices), chunkLen, nil); err != nil {
		return err
	}
	if err := w.WriteArray(at("X/data"), "<f4", []int{len(o.X.Data)}, float32s(o.X.Data), chunkLen, nil); err != nil {
		return err
	}
	if err := w.Group(at("obsm"), enc("dict", "0.1.0")); err != nil {
		return err
	}
	for _, name := range sortedKeys(o.Obsm) {
		d := o.Obsm[name]
		if d.Rows != rows || len(d.Data) != d.Rows*d.Cols {
			return fmt.Errorf("anndata: obsm %s is %d × %d with %d values, for %d rows", name, d.Rows, d.Cols, len(d.Data), rows)
		}
		if err := w.WriteArray(at("obsm/"+name), "<f8", []int{d.Rows, d.Cols}, float64s(d.Data), 0, enc("array", "0.2.0")); err != nil {
			return err
		}
	}
	for _, name := range []string{"layers", "obsp", "varm", "varp"} {
		if err := w.Group(at(name), enc("dict", "0.1.0")); err != nil {
			return err
		}
	}
	return writeDict(w, at("uns"), o.Uns)
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "/" + name
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeFrame(w *zarr.Writer, path string, f *WriteFrame) error {
	order := make([]string, 0, len(f.Columns))
	seen := map[string]bool{"_index": true}
	for _, c := range f.Columns {
		if seen[c.Name] || c.Name == "" {
			return fmt.Errorf("anndata: %s: column name %q is empty or repeated", path, c.Name)
		}
		seen[c.Name] = true
		if c.length() != len(f.Index) {
			return fmt.Errorf("anndata: %s: column %s has %d values for %d rows", path, c.Name, c.length(), len(f.Index))
		}
		order = append(order, c.Name)
	}
	attrs := enc("dataframe", "0.2.0")
	attrs["_index"] = "_index"
	attrs["column-order"] = order
	if err := w.Group(path, attrs); err != nil {
		return err
	}
	if err := w.WriteTextArray(path+"/_index", f.Index, 1<<16, enc("string-array", "0.2.0"), false); err != nil {
		return err
	}
	for i := range f.Columns {
		if err := writeColumn(w, path+"/"+f.Columns[i].Name, &f.Columns[i]); err != nil {
			return err
		}
	}
	return nil
}

func writeColumn(w *zarr.Writer, path string, c *WriteColumn) error {
	array := enc("array", "0.2.0")
	n := []int{c.length()}
	switch {
	case c.Text != nil && c.Categorical:
		var cats []string
		index := map[string]int{}
		codes := make([]int, len(c.Text))
		for i, v := range c.Text {
			k, ok := index[v]
			if !ok {
				k = len(cats)
				index[v] = k
				cats = append(cats, v)
			}
			codes[i] = k
		}
		attrs := enc("categorical", "0.2.0")
		attrs["ordered"] = false
		if err := w.Group(path, attrs); err != nil {
			return err
		}
		if err := w.WriteTextArray(path+"/categories", cats, 1<<16, enc("string-array", "0.2.0"), false); err != nil {
			return err
		}
		// The narrowest signed type holding the codes, as pandas chooses.
		switch {
		case len(cats) <= math.MaxInt8:
			raw := make([]byte, len(codes))
			for i, k := range codes {
				raw[i] = byte(int8(k))
			}
			return w.WriteArray(path+"/codes", "|i1", n, raw, chunkLen, array)
		case len(cats) <= math.MaxInt16:
			raw := make([]byte, 2*len(codes))
			for i, k := range codes {
				binary.LittleEndian.PutUint16(raw[2*i:], uint16(int16(k)))
			}
			return w.WriteArray(path+"/codes", "<i2", n, raw, chunkLen, array)
		default:
			k32 := make([]int32, len(codes))
			for i, k := range codes {
				k32[i] = int32(k)
			}
			return w.WriteArray(path+"/codes", "<i4", n, int32s(k32), chunkLen, array)
		}
	case c.Text != nil:
		return w.WriteTextArray(path, c.Text, 1<<16, enc("string-array", "0.2.0"), false)
	case c.Float64 != nil:
		return w.WriteArray(path, "<f8", n, float64s(c.Float64), chunkLen, array)
	case c.Float32 != nil:
		return w.WriteArray(path, "<f4", n, float32s(c.Float32), chunkLen, array)
	case c.Int64 != nil:
		return w.WriteArray(path, "<i8", n, int64s(c.Int64), chunkLen, array)
	case c.Int32 != nil:
		return w.WriteArray(path, "<i4", n, int32s(c.Int32), chunkLen, array)
	default:
		u := c.Uint32
		if u == nil {
			u = []uint32{}
		}
		raw := make([]byte, 4*len(u))
		for i, v := range u {
			binary.LittleEndian.PutUint32(raw[4*i:], v)
		}
		return w.WriteArray(path, "<u4", n, raw, chunkLen, array)
	}
}

// writeDict writes uns (or a dictionary within it).
func writeDict(w *zarr.Writer, path string, d map[string]any) error {
	if err := w.Group(path, enc("dict", "0.1.0")); err != nil {
		return err
	}
	for _, k := range sortedKeys(d) {
		p := path + "/" + k
		var err error
		switch v := d[k].(type) {
		case string:
			err = w.WriteTextArray(p, []string{v}, 0, enc("string", "0.2.0"), true)
		case []string:
			err = w.WriteTextArray(p, v, 0, enc("string-array", "0.2.0"), false)
		case float64:
			err = w.WriteArray(p, "<f8", []int{}, float64s([]float64{v}), 0, enc("numeric-scalar", "0.2.0"))
		case int:
			err = w.WriteArray(p, "<i8", []int{}, int64s([]int64{int64(v)}), 0, enc("numeric-scalar", "0.2.0"))
		case int64:
			err = w.WriteArray(p, "<i8", []int{}, int64s([]int64{v}), 0, enc("numeric-scalar", "0.2.0"))
		case map[string]any:
			err = writeDict(w, p, v)
		default:
			err = fmt.Errorf("anndata: uns %s is a %T, which is not written", p, v)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func int64s(v []int64) []byte {
	out := make([]byte, 8*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint64(out[8*i:], uint64(x))
	}
	return out
}

func int32s(v []int32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[4*i:], uint32(x))
	}
	return out
}

func float32s(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

func float64s(v []float64) []byte {
	out := make([]byte, 8*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint64(out[8*i:], math.Float64bits(x))
	}
	return out
}
