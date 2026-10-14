package xenium

// Each cell's segmentation method. Bundles that write it in cells.parquet (a segmentation_method
// column) are read from there; XOA 2.0 keeps it only in cells.zarr.zip, where
// polygon_sets/1/method indexes the store's segmentation_methods for each cell of its cell_id
// array (research.md §1.3b).

import (
	"errors"
	"fmt"

	"situcraft/internal/parquet"
	"situcraft/internal/zarr"
)

// NoMethod is the method of a cell whose method is not recorded.
const NoMethod = 255

// CellMethods names the segmentation methods and gives each cell's.
type CellMethods struct {
	Methods []string
	// Cell[i] indexes Methods for row i of cells.parquet (NoMethod when unknown).
	Cell []uint8
	// Source is the file the methods were read from.
	Source string
}

// ReadCellMethods reads each cell's segmentation method, in the order of cells (nil, nil when the
// bundle does not record them).
func (b *Bundle) ReadCellMethods(cells *Cells) (*CellMethods, error) {
	if m, err := b.cellMethodsFromParquet(cells); m != nil || err != nil {
		return m, err
	}
	if !b.Has("cells.zarr.zip") {
		return nil, nil
	}
	f, err := b.OpenFile("cells.zarr.zip")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	store, err := zarr.OpenZip(f, f.Size())
	if err != nil {
		return nil, fmt.Errorf("cells.zarr.zip: %w", err)
	}
	out := &CellMethods{Source: "cells.zarr.zip", Cell: make([]uint8, len(cells.IDs))}
	attrs, err := zarr.Attributes(store, "")
	if err != nil {
		return nil, fmt.Errorf("cells.zarr.zip: %w", err)
	}
	names, _ := attrs["segmentation_methods"].([]any)
	for _, n := range names {
		s, _ := n.(string)
		out.Methods = append(out.Methods, s)
	}
	if len(out.Methods) == 0 {
		return nil, nil
	}
	if len(out.Methods) >= NoMethod {
		return nil, errors.New("cells.zarr.zip names too many segmentation methods")
	}
	method, err := zarr.OpenArray(store, "polygon_sets/1/method")
	if err != nil {
		return nil, fmt.Errorf("cells.zarr.zip: %w", err)
	}
	if method.DType.Kind != 'u' && method.DType.Kind != 'i' {
		return nil, fmt.Errorf("cells.zarr.zip: polygon_sets/1/method is %s", method.DType)
	}
	raw, err := method.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("cells.zarr.zip: %w", err)
	}
	methods := unsignedInts(raw, method.DType.Size)
	ids, err := zarr.OpenArray(store, "cell_id")
	if err != nil {
		return nil, fmt.Errorf("cells.zarr.zip: %w", err)
	}
	idRaw, err := ids.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("cells.zarr.zip: %w", err)
	}
	if len(ids.Shape) != 2 || ids.Shape[1] != 2 || ids.DType.Size != 4 || ids.Shape[0] != len(methods) {
		return nil, fmt.Errorf("cells.zarr.zip: cell_id has shape %v for %d methods", ids.Shape, len(methods))
	}
	for i := range out.Cell {
		out.Cell[i] = NoMethod
	}
	// The store usually lists cells in cells.parquet's order; it is matched by ID either way.
	for i, m := range methods {
		id := CellIDString(le32(idRaw[8*i:]), le32(idRaw[8*i+4:]))
		row := i
		if i >= len(cells.IDs) || cells.IDs[i] != id {
			r, ok := cells.Index[id]
			if !ok {
				continue
			}
			row = int(r)
		}
		if m >= 0 && m < int64(len(out.Methods)) {
			out.Cell[row] = uint8(m)
		}
	}
	return out, nil
}

// cellMethodsFromParquet reads cells.parquet's segmentation_method column, when it has one.
func (b *Bundle) cellMethodsFromParquet(cells *Cells) (*CellMethods, error) {
	has := false
	for _, c := range cells.Columns {
		has = has || c == "segmentation_method"
	}
	if !has {
		return nil, nil
	}
	const name = "cells.parquet"
	p, f, err := b.openParquet(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	v, _, err := column(p, name, "segmentation_method", parquet.ByteArray)
	if err != nil {
		return nil, err
	}
	out := &CellMethods{Source: name, Cell: make([]uint8, v.Len)}
	index := map[string]uint8{}
	for i, raw := range v.Bytes {
		s := string(raw)
		m, ok := index[s]
		if !ok {
			if len(out.Methods) >= NoMethod {
				return nil, errors.New("cells.parquet names too many segmentation methods")
			}
			m = uint8(len(out.Methods))
			index[s] = m
			out.Methods = append(out.Methods, s)
		}
		out.Cell[i] = m
	}
	return out, nil
}
