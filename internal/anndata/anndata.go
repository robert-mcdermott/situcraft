// Package anndata reads AnnData objects stored in Zarr (the on-disk format of the anndata Python
// package, as SpatialData tables use it): data frames (obs, var) and the matrix X. It follows
// anndata's documented on-disk specification: each element carries an encoding-type attribute.
//
//   - dataframe: a group whose attributes name its index column (_index) and its columns
//     (column-order); each column is an array, a categorical (codes, categories; code −1 is
//     missing), or a nullable array (values, mask; true in the mask is missing);
//   - array and string-array: a Zarr array (text of fixed or varying length);
//   - csr_matrix and csc_matrix: a group with data, indices and indptr, and its shape.
package anndata

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"situcraft/internal/zarr"
)

// Column is one column of a data frame: text, or numbers. Missing marks absent values.
type Column struct {
	Name    string
	Text    []string  // for text and categorical columns
	Numbers []float64 // for numeric columns
	Missing []bool    // nil when none is missing
}

// Len is the number of values.
func (c *Column) Len() int {
	if c.Text != nil {
		return len(c.Text)
	}
	return len(c.Numbers)
}

// String is value i as text (numbers written as integers when they are).
func (c *Column) String(i int) string {
	if c.Missing != nil && c.Missing[i] {
		return ""
	}
	if c.Text != nil {
		return c.Text[i]
	}
	v := c.Numbers[i]
	if v == math.Trunc(v) && math.Abs(v) < 1<<53 {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprint(v)
}

// Frame is a data frame: its index and its columns.
type Frame struct {
	Index   *Column
	Columns []*Column
}

// Column is the column with this name, or nil.
func (f *Frame) Column(name string) *Column {
	for _, c := range f.Columns {
		if c.Name == name {
			return c
		}
	}
	if f.Index != nil && f.Index.Name == name {
		return f.Index
	}
	return nil
}

func encoding(attrs map[string]any) string {
	s, _ := attrs["encoding-type"].(string)
	return s
}

// ReadFrame reads the data frame at path.
func ReadFrame(s zarr.Store, path string) (*Frame, error) {
	attrs, err := zarr.Attributes(s, path)
	if err != nil {
		return nil, err
	}
	if e := encoding(attrs); e != "dataframe" {
		return nil, fmt.Errorf("anndata: %s is %q, not a data frame", path, e)
	}
	index, _ := attrs["_index"].(string)
	if index == "" {
		return nil, fmt.Errorf("anndata: %s names no index", path)
	}
	f := &Frame{}
	if f.Index, err = ReadColumn(s, path+"/"+index); err != nil {
		return nil, err
	}
	order, _ := attrs["column-order"].([]any)
	for _, name := range order {
		n, _ := name.(string)
		if n == "" {
			continue
		}
		c, err := ReadColumn(s, path+"/"+n)
		if err != nil {
			return nil, err
		}
		if c.Len() != f.Index.Len() {
			return nil, fmt.Errorf("anndata: %s has %d values for %d rows", c.Name, c.Len(), f.Index.Len())
		}
		f.Columns = append(f.Columns, c)
	}
	return f, nil
}

// ReadColumn reads one column (or index) of a data frame.
func ReadColumn(s zarr.Store, path string) (*Column, error) {
	name := path[strings.LastIndex(path, "/")+1:]
	attrs, err := zarr.Attributes(s, path)
	if err != nil {
		return nil, err
	}
	c := &Column{Name: name}
	switch e := encoding(attrs); e {
	case "categorical":
		codes, err := readInts(s, path+"/codes")
		if err != nil {
			return nil, err
		}
		cats, err := ReadColumn(s, path+"/categories")
		if err != nil {
			return nil, err
		}
		c.Text = make([]string, len(codes))
		for i, k := range codes {
			if k < 0 || int(k) >= cats.Len() {
				if c.Missing == nil {
					c.Missing = make([]bool, len(codes))
				}
				c.Missing[i] = true
				continue
			}
			c.Text[i] = cats.String(int(k))
		}
		if cats.Text == nil {
			// Numeric categories, kept as numbers.
			c.Numbers = make([]float64, len(codes))
			for i, k := range codes {
				if k >= 0 && int(k) < cats.Len() {
					c.Numbers[i] = cats.Numbers[k]
				}
			}
			c.Text = nil
		}
		return c, nil
	case "nullable-string-array", "nullable-integer", "nullable-boolean":
		values, err := ReadColumn(s, path+"/values")
		if err != nil {
			return nil, err
		}
		mask, err := readInts(s, path+"/mask")
		if err != nil {
			return nil, err
		}
		if len(mask) != values.Len() {
			return nil, fmt.Errorf("anndata: %s has %d values and a mask of %d", path, values.Len(), len(mask))
		}
		values.Name = name
		values.Missing = make([]bool, len(mask))
		for i, m := range mask {
			values.Missing[i] = m != 0
		}
		return values, nil
	case "array", "string-array", "":
		a, err := zarr.OpenArray(s, path)
		if err != nil {
			return nil, err
		}
		if len(a.Shape) != 1 {
			return nil, fmt.Errorf("anndata: %s has %d dimensions", path, len(a.Shape))
		}
		switch a.DType.Kind {
		case 'O', 'U', 'S':
			c.Text, err = a.ReadStrings()
		default:
			c.Numbers, err = Numbers(a)
		}
		return c, err
	default:
		return nil, fmt.Errorf("anndata: %s is %q, which is not read", path, e)
	}
}

func readInts(s zarr.Store, path string) ([]int64, error) {
	a, err := zarr.OpenArray(s, path)
	if err != nil {
		return nil, err
	}
	v, err := Numbers(a)
	if err != nil {
		return nil, err
	}
	out := make([]int64, len(v))
	for i, x := range v {
		out[i] = int64(x)
	}
	return out, nil
}

// Numbers reads a numeric or boolean array whole, as float64.
func Numbers(a *zarr.Array) ([]float64, error) {
	raw, err := a.ReadAll()
	if err != nil {
		return nil, err
	}
	size := a.DType.Size
	n := len(raw) / max(size, 1)
	out := make([]float64, n)
	for i := range out {
		b := raw[i*size : (i+1)*size]
		switch a.DType.Kind {
		case 'b':
			if b[0] != 0 {
				out[i] = 1
			}
		case 'i':
			switch size {
			case 1:
				out[i] = float64(int8(b[0]))
			case 2:
				out[i] = float64(int16(binary.LittleEndian.Uint16(b)))
			case 4:
				out[i] = float64(int32(binary.LittleEndian.Uint32(b)))
			case 8:
				out[i] = float64(int64(binary.LittleEndian.Uint64(b)))
			}
		case 'u':
			switch size {
			case 1:
				out[i] = float64(b[0])
			case 2:
				out[i] = float64(binary.LittleEndian.Uint16(b))
			case 4:
				out[i] = float64(binary.LittleEndian.Uint32(b))
			case 8:
				out[i] = float64(binary.LittleEndian.Uint64(b))
			}
		case 'f':
			switch size {
			case 4:
				out[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
			case 8:
				out[i] = math.Float64frombits(binary.LittleEndian.Uint64(b))
			default:
				return nil, fmt.Errorf("anndata: %s: %s is not read", a.Path, a.DType)
			}
		default:
			return nil, fmt.Errorf("anndata: %s holds %s, not numbers", a.Path, a.DType)
		}
	}
	return out, nil
}

// Matrix is a matrix in compressed sparse rows.
type Matrix struct {
	Rows, Cols int
	Indptr     []int64 // len Rows+1
	Indices    []int64
	Data       []float64
}

// RowSums are each row's total.
func (m *Matrix) RowSums() []float64 {
	out := make([]float64, m.Rows)
	for r := 0; r < m.Rows; r++ {
		for k := m.Indptr[r]; k < m.Indptr[r+1]; k++ {
			out[r] += m.Data[k]
		}
	}
	return out
}

// ReadMatrix reads the matrix at path (csr_matrix, csc_matrix or a dense array) as sparse rows.
func ReadMatrix(s zarr.Store, path string) (*Matrix, error) {
	attrs, err := zarr.Attributes(s, path)
	if err != nil {
		return nil, err
	}
	switch e := encoding(attrs); e {
	case "csr_matrix", "csc_matrix":
		shape, _ := attrs["shape"].([]any)
		if len(shape) != 2 {
			return nil, fmt.Errorf("anndata: %s has no shape", path)
		}
		rows, _ := shape[0].(float64)
		cols, _ := shape[1].(float64)
		indptr, err := readInts(s, path+"/indptr")
		if err != nil {
			return nil, err
		}
		indices, err := readInts(s, path+"/indices")
		if err != nil {
			return nil, err
		}
		dataArray, err := zarr.OpenArray(s, path+"/data")
		if err != nil {
			return nil, err
		}
		data, err := Numbers(dataArray)
		if err != nil {
			return nil, err
		}
		major, minor := int(rows), int(cols)
		if e == "csc_matrix" {
			major, minor = minor, major
		}
		if err := checkSparse(path, major, minor, indptr, indices, data); err != nil {
			return nil, err
		}
		m := &Matrix{Rows: major, Cols: minor, Indptr: indptr, Indices: indices, Data: data}
		if e == "csc_matrix" {
			m = m.transpose()
		}
		return m, nil
	case "array", "":
		a, err := zarr.OpenArray(s, path)
		if err != nil {
			return nil, err
		}
		if len(a.Shape) != 2 {
			return nil, fmt.Errorf("anndata: %s has %d dimensions", path, len(a.Shape))
		}
		v, err := Numbers(a)
		if err != nil {
			return nil, err
		}
		m := &Matrix{Rows: a.Shape[0], Cols: a.Shape[1], Indptr: make([]int64, a.Shape[0]+1)}
		for r := 0; r < m.Rows; r++ {
			for c := 0; c < m.Cols; c++ {
				if x := v[r*m.Cols+c]; x != 0 {
					m.Indices = append(m.Indices, int64(c))
					m.Data = append(m.Data, x)
				}
			}
			m.Indptr[r+1] = int64(len(m.Data))
		}
		return m, nil
	default:
		return nil, fmt.Errorf("anndata: %s is %q, which is not read", path, e)
	}
}

func checkSparse(path string, major, minor int, indptr, indices []int64, data []float64) error {
	if major < 0 || minor < 0 || len(indptr) != major+1 || len(indices) != len(data) || indptr[0] != 0 || indptr[major] != int64(len(data)) {
		return fmt.Errorf("anndata: %s is not a sparse matrix of shape %d × %d", path, major, minor)
	}
	for i := 0; i < major; i++ {
		if indptr[i+1] < indptr[i] {
			return fmt.Errorf("anndata: %s: its rows are out of order", path)
		}
	}
	for _, j := range indices {
		if j < 0 || j >= int64(minor) {
			return fmt.Errorf("anndata: %s: index %d outside %d", path, j, minor)
		}
	}
	return nil
}

// transpose turns sparse columns into sparse rows.
func (m *Matrix) transpose() *Matrix {
	t := &Matrix{Rows: m.Cols, Cols: m.Rows, Indptr: make([]int64, m.Cols+1), Indices: make([]int64, len(m.Data)), Data: make([]float64, len(m.Data))}
	for _, j := range m.Indices {
		t.Indptr[j+1]++
	}
	for r := 0; r < t.Rows; r++ {
		t.Indptr[r+1] += t.Indptr[r]
	}
	next := append([]int64(nil), t.Indptr[:t.Rows]...)
	for r := 0; r < m.Rows; r++ {
		for k := m.Indptr[r]; k < m.Indptr[r+1]; k++ {
			j := m.Indices[k]
			t.Indices[next[j]] = int64(r)
			t.Data[next[j]] = m.Data[k]
			next[j]++
		}
	}
	return t
}
