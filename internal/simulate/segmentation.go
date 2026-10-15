package simulate

// A segmentation as a bundle describes it, the simulated instrument's or the truth: its cell mask
// (label k+1 is cell k), its nucleus mask (label p+1 is nucleus p, of cell nucleusCell[p]), the
// cells' ids, and every label's outline and shape. The boundary tables and cells.zarr.zip are
// written from it, so they agree.

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"

	"situcraft/internal/parquet"
	"situcraft/internal/segment"
	"situcraft/internal/zarr"
)

type segmentation struct {
	cells, nuclei   []uint32
	nucleusCell     []uint32 // nucleus p's cell
	nucleusOf       []int    // cell k's nucleus (−1: none)
	ids             []string
	idSalt          uint32
	trueCell        []int // cell k's true cell
	cellOutlines    []segment.Outline
	nucleusOutlines []segment.Outline
	cellShapes      []shape
	nucleusShapes   []shape
	// methods name how the cells (methods[0]) and nuclei (methods[1]) were segmented.
	methods []string
}

// instrumentSegmentation is the simulated instrument's: a nucleus for each of its cells, labelled
// as the cell.
func (wr *writer) instrumentSegmentation() *segmentation {
	n := len(wr.in.cells)
	s := &segmentation{cells: wr.in.labels, nuclei: wr.in.nuclei, ids: wr.instID, idSalt: 0, trueCell: wr.in.cells,
		methods: []string{wr.t.p.methodName(), "Segmented by nuclear stain (DAPI)"}}
	for k := 0; k < n; k++ {
		s.nucleusCell = append(s.nucleusCell, uint32(k))
		s.nucleusOf = append(s.nucleusOf, k)
	}
	wr.trace(s)
	return s
}

// trueSegmentation is the truth: every true cell, and the nuclei in the section numbered in their
// cells' order.
func (wr *writer) trueSegmentation() *segmentation {
	n := len(wr.t.cells)
	s := &segmentation{cells: wr.t.labels, ids: wr.trueID, idSalt: trueSalt, nucleusOf: make([]int, n),
		methods: []string{"Simulated: the true cells", "Simulated: the true nuclei in the section"}}
	relabel := make([]uint32, n+1)
	for k := range wr.t.cells {
		s.trueCell = append(s.trueCell, k)
		s.nucleusOf[k] = -1
		if wr.t.cells[k].nucleus {
			s.nucleusOf[k] = len(s.nucleusCell)
			s.nucleusCell = append(s.nucleusCell, uint32(k))
			relabel[k+1] = uint32(len(s.nucleusCell))
		}
	}
	s.nuclei = make([]uint32, len(wr.t.nuclei))
	for i, l := range wr.t.nuclei {
		s.nuclei[i] = relabel[l]
	}
	wr.trace(s)
	return s
}

// trace finds every label's outline and shape.
func (wr *writer) trace(s *segmentation) {
	outlines := func(labels []uint32, n int) ([]segment.Outline, []shape) {
		r := wr.raster(labels)
		stats := r.Stats(n)
		out := make([]segment.Outline, n)
		for k := 0; k < n; k++ {
			if stats[k].Pixels > 0 {
				out[k] = r.TraceOutline(uint32(k+1), stats[k], 1)
			}
		}
		return out, wr.shapes(labels, n)
	}
	s.cellOutlines, s.cellShapes = outlines(s.cells, len(s.ids))
	s.nucleusOutlines, s.nucleusShapes = outlines(s.nuclei, len(s.nucleusCell))
}

// nucleusShape is cell k's nucleus's shape (none: zero).
func (s *segmentation) nucleusShape(k int) shape {
	if p := s.nucleusOf[k]; p >= 0 {
		return s.nucleusShapes[p]
	}
	return shape{}
}

// ring is an outline's vertices with the first repeated last, as Xenium writes them (none for an
// outline of fewer than three).
func ring(o segment.Outline) (xs, ys []float32) {
	if len(o.X) < 3 {
		return nil, nil
	}
	return append(append([]float32(nil), o.X...), o.X[0]), append(append([]float32(nil), o.Y...), o.Y[0])
}

// writeBoundaries writes the cells' (or the nuclei's) outlines as Xenium's boundary tables: each
// vertex a row, outlines in label order, with the cell's id and the label.
func (wr *writer) writeBoundaries(path string, s *segmentation, nuclei bool) error {
	outlines, owner := s.cellOutlines, func(p int) int { return p }
	if nuclei {
		outlines, owner = s.nucleusOutlines, func(p int) int { return int(s.nucleusCell[p]) }
	}
	var cell [][]byte
	var xs, ys []float32
	var label []int32
	for p, o := range outlines {
		rx, ry := ring(o)
		id := []byte(s.ids[owner(p)])
		for range rx {
			cell = append(cell, id)
			label = append(label, int32(p+1))
		}
		xs, ys = append(xs, rx...), append(ys, ry...)
	}
	return parquetFile(path, []parquet.Field{text("cell_id"), f32("vertex_x"), f32("vertex_y"), u32("label_id")}, wr.software, len(cell), func(from, to int) []*parquet.Values {
		return []*parquet.Values{{Bytes: cell[from:to]}, {Float32: xs[from:to]}, {Float32: ys[from:to]}, {Int32: label[from:to]}}
	})
}

// zLevel is the z plane (of the run's z steps) through the middle of a true cell's nucleus.
func (wr *writer) zLevel(k int) float64 {
	c := &wr.t.cells[k]
	return math.Floor((c.nzLo + c.nzHi) / 2 / zStepUm)
}

// zStepUm is the run's z step (experiment.xenium's z_step_size).
const zStepUm = 0.75

// writeCellsZarr writes cells.zarr.zip as Xenium Onboard Analysis 2 lays it out (Zarr format 2,
// zipped): masks/0 (nuclei) and masks/1 (cells), the µm-to-pixel transform, the cells' ids,
// cell_summary, and for each polygon set (0 nuclei, 1 cells) its outlines' vertices, each
// polygon's cell and segmentation method; the root's attributes describe them.
func (wr *writer) writeCellsZarr(path string, s *segmentation) error {
	tmp, err := os.MkdirTemp(filepath.Dir(path), ".cells-zarr-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	n := len(s.ids)
	z, err := zarr.NewWriter(tmp, 2, map[string]any{
		"major_version": 6, "minor_version": 2, "name": "CellSegmentationDataset", "number_cells": n,
		"polygon_set_names":         []string{"nucleus", "cell"},
		"polygon_set_display_names": []string{"Nucleus boundaries", "Cell boundaries"},
		"polygon_set_descriptions":  []string{s.methods[1], s.methods[0]},
		"segmentation_methods":      s.methods,
		"spatial_units":             "microns",
	})
	if err != nil {
		return err
	}
	f32s := func(v ...float32) []byte {
		b := make([]byte, 0, 4*len(v))
		for _, x := range v {
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(x))
		}
		return b
	}
	u32s := func(v []uint32) []byte {
		b := make([]byte, 0, 4*len(v))
		for _, x := range v {
			b = binary.LittleEndian.AppendUint32(b, x)
		}
		return b
	}
	if err := z.Group("masks", nil); err != nil {
		return err
	}
	if err := writeMask(z, "masks/0", s.nuclei, wr.t.w, wr.t.h); err != nil {
		return err
	}
	if err := writeMask(z, "masks/1", s.cells, wr.t.w, wr.t.h); err != nil {
		return err
	}
	scale := float32(1 / wr.t.p.PixelUm)
	if err := z.WriteArray("masks/homogeneous_transform", "<f4", []int{4, 4}, f32s(scale, 0, 0, 0, 0, scale, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1), 0, nil); err != nil {
		return err
	}
	ids := make([]uint32, 0, 2*n)
	for k := 0; k < n; k++ {
		prefix, _ := cellID(k, s.idSalt)
		ids = append(ids, prefix, 1)
	}
	if err := z.WriteArray("cell_id", "<u4", []int{n, 2}, u32s(ids), 0, nil); err != nil {
		return err
	}
	// cell_summary: each cell's centroid and area, its nucleus's, its z plane and nuclei.
	ps := wr.t.p.PixelUm
	summary := make([]byte, 0, 64*n)
	for k := 0; k < n; k++ {
		c, nu := s.cellShapes[k], s.nucleusShape(k)
		row := []float64{0, 0, mul(float64(c.pixels), mul(ps, ps)), 0, 0, mul(float64(nu.pixels), mul(ps, ps)), wr.zLevel(s.trueCell[k]), 0}
		if c.pixels > 0 {
			row[0], row[1] = mul(c.sx/float64(c.pixels), ps), mul(c.sy/float64(c.pixels), ps)
		}
		if nu.pixels > 0 {
			row[3], row[4], row[7] = mul(nu.sx/float64(nu.pixels), ps), mul(nu.sy/float64(nu.pixels), ps), 1
		}
		for _, v := range row {
			summary = binary.LittleEndian.AppendUint64(summary, math.Float64bits(v))
		}
	}
	if err := z.WriteArray("cell_summary", "<f8", []int{n, 8}, summary, 0, map[string]any{
		"column_names": []string{"cell_centroid_x", "cell_centroid_y", "cell_area", "nucleus_centroid_x", "nucleus_centroid_y", "nucleus_area", "z_level", "nucleus_count"},
		"column_descriptions": []string{"Cell centroid in X", "Cell centroid in Y", "Cell area", "Nucleus centroid in X", "Nucleus centroid in Y", "Nucleus area",
			"z plane through the middle of the nucleus", "Nucleus count"},
	}); err != nil {
		return err
	}
	if err := z.Group("polygon_sets", nil); err != nil {
		return err
	}
	for set, polys := range [][]segment.Outline{s.nucleusOutlines, s.cellOutlines} {
		dir := "polygon_sets/" + string(rune('0'+set))
		if err := z.Group(dir, nil); err != nil {
			return err
		}
		width := 1
		for _, o := range polys {
			rx, _ := ring(o)
			width = max(width, len(rx))
		}
		vertices := make([]float32, 2*width*len(polys))
		counts := make([]byte, 0, 4*len(polys))
		cellIndex, method := make([]uint32, len(polys)), make([]uint32, len(polys))
		for p, o := range polys {
			rx, ry := ring(o)
			for i := range rx {
				vertices[2*(p*width+i)], vertices[2*(p*width+i)+1] = rx[i], ry[i]
			}
			counts = binary.LittleEndian.AppendUint32(counts, uint32(len(rx)))
			cellIndex[p] = uint32(p)
			if set == 0 {
				cellIndex[p], method[p] = s.nucleusCell[p], 1
			}
		}
		for _, a := range []struct {
			name, dtype string
			shape       []int
			data        []byte
		}{
			{"vertices", "<f4", []int{len(polys), 2 * width}, f32s(vertices...)},
			{"num_vertices", "<i4", []int{len(polys)}, counts},
			{"cell_index", "<u4", []int{len(polys)}, u32s(cellIndex)},
			{"method", "<u4", []int{len(polys)}, u32s(method)},
		} {
			if err := z.WriteArray(dir+"/"+a.name, a.dtype, a.shape, a.data, 0, nil); err != nil {
				return err
			}
		}
	}
	return zipFolder(tmp, path)
}
