package anndata

// Reading a large matrix a chunk at a time: a reference of hundreds of thousands of cells holds
// more values than fit in memory as float64, but each row is needed only once, in order.

import (
	"fmt"

	"situcraft/internal/zarr"
)

// chunked reads a one-dimensional array's elements in order, a chunk at a time.
type chunked struct {
	a     *zarr.Array
	at    int // the first element of the chunk held
	chunk []float64
}

func (c *chunked) get(e int) (float64, error) {
	if e < c.at || e >= c.at+len(c.chunk) {
		size := c.a.Chunks[0]
		k := e / size
		raw, err := c.a.ReadChunk([]int{k})
		if err != nil {
			return 0, err
		}
		v, err := decode(c.a, raw)
		if err != nil {
			return 0, err
		}
		c.at = k * size
		c.chunk = v[:min(len(v), c.a.Shape[0]-c.at)]
	}
	return c.chunk[e-c.at], nil
}

// EachRow walks the matrix at path row by row, calling fn with each row's column indices and
// values as stored (the slices are reused between calls). A matrix in compressed sparse rows is
// read a chunk at a time; one in sparse columns, or dense, is read whole first. Answers its shape.
func EachRow(s zarr.Store, path string, fn func(row int, cols []int64, vals []float64) error) (rows, cols int, err error) {
	attrs, err := zarr.Attributes(s, path)
	if err != nil {
		return 0, 0, err
	}
	if encoding(attrs) != "csr_matrix" {
		m, err := ReadMatrix(s, path)
		if err != nil {
			return 0, 0, err
		}
		for i := 0; i < m.Rows; i++ {
			lo, hi := m.Indptr[i], m.Indptr[i+1]
			if err := fn(i, m.Indices[lo:hi], m.Data[lo:hi]); err != nil {
				return m.Rows, m.Cols, err
			}
		}
		return m.Rows, m.Cols, nil
	}
	shape, _ := attrs["shape"].([]any)
	if len(shape) != 2 {
		return 0, 0, fmt.Errorf("anndata: %s has no shape", path)
	}
	r, _ := shape[0].(float64)
	c, _ := shape[1].(float64)
	rows, cols = int(r), int(c)
	indptr, err := readInts(s, path+"/indptr")
	if err != nil {
		return 0, 0, err
	}
	ia, err := zarr.OpenArray(s, path+"/indices")
	if err != nil {
		return 0, 0, err
	}
	da, err := zarr.OpenArray(s, path+"/data")
	if err != nil {
		return 0, 0, err
	}
	if len(ia.Shape) != 1 || len(da.Shape) != 1 || ia.Shape[0] != da.Shape[0] || len(indptr) != rows+1 || indptr[0] != 0 || indptr[rows] != int64(da.Shape[0]) {
		return 0, 0, fmt.Errorf("anndata: %s is not a sparse matrix of shape %d × %d", path, rows, cols)
	}
	indices, data := &chunked{a: ia}, &chunked{a: da}
	var rc []int64
	var rv []float64
	for i := 0; i < rows; i++ {
		lo, hi := indptr[i], indptr[i+1]
		if hi < lo {
			return rows, cols, fmt.Errorf("anndata: %s: its rows are out of order", path)
		}
		rc, rv = rc[:0], rv[:0]
		for e := lo; e < hi; e++ {
			j, err := indices.get(int(e))
			if err != nil {
				return rows, cols, err
			}
			if j < 0 || j >= float64(cols) {
				return rows, cols, fmt.Errorf("anndata: %s: index %v outside %d", path, j, cols)
			}
			v, err := data.get(int(e))
			if err != nil {
				return rows, cols, err
			}
			rc, rv = append(rc, int64(j)), append(rv, v)
		}
		if err := fn(i, rc, rv); err != nil {
			return rows, cols, err
		}
	}
	return rows, cols, nil
}
