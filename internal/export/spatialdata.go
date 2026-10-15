package export

// SpatialData Zarr (Marconato et al. 2025) and AnnData Zarr. The SpatialData store is laid out as
// the spatialdata package lays out its own (Zarr format 3):
//
//	labels/cell_labels       the label raster (label k is cell k), a single-scale OME-NGFF label
//	                         image, scaled to µm in the global coordinate system
//	shapes/cell_boundaries   the cells' outlines: GeoParquet polygons (WKB), indexed by cell_id
//	points/transcripts       every transcript (x, y, z in µm, feature_name, cell_id, qv, and
//	                         transcript_id when the dataset has them), one Parquet part; points
//	                         in three dimensions
//	tables/table             the counts as AnnData, annotating cell_labels: obs holds each cell's
//	                         id, label and measures, obsm['spatial'] its centroid
//
// The global coordinate system is in µm. The AnnData store holds the same table on its own, in
// Zarr format 2 (anndata's default).

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"path/filepath"

	"situcraft/internal/anndata"
	"situcraft/internal/parquet"
	"situcraft/internal/zarr"
)

// The names of the SpatialData store's elements.
const (
	LabelsElement = "cell_labels"
	ShapesElement = "cell_boundaries"
	PointsElement = "transcripts"
	TableElement  = "table"
)

// labelChunk is the label image's chunk side.
const labelChunk = 1024

// object is the segmentation's counts as AnnData; spatial adds what a SpatialData table carries.
func (d *data) object(spatial bool) *anndata.Object {
	n := len(d.cells)
	ids := make([]string, n)
	cols := struct {
		x, y, area, nucleusArea []float64
		transcripts, assigned   []uint32
		genes                   []uint32
		nucleus                 []string
		labels                  []int64
	}{}
	for i, c := range d.cells {
		ids[i] = c.ID
		cols.x = append(cols.x, float64(c.X))
		cols.y = append(cols.y, float64(c.Y))
		cols.area = append(cols.area, float64(c.Area))
		cols.nucleusArea = append(cols.nucleusArea, float64(c.NucleusArea))
		cols.transcripts = append(cols.transcripts, c.Transcripts)
		cols.assigned = append(cols.assigned, c.Assigned)
		cols.genes = append(cols.genes, c.Genes)
		cols.nucleus = append(cols.nucleus, c.Nucleus)
		cols.labels = append(cols.labels, int64(i+1))
	}
	var obs []anndata.WriteColumn
	if spatial {
		region := make([]string, n)
		for i := range region {
			region[i] = LabelsElement
		}
		obs = append(obs,
			anndata.WriteColumn{Name: "cell_id", Text: ids},
			anndata.WriteColumn{Name: "region", Text: region, Categorical: true},
			anndata.WriteColumn{Name: LabelsElement, Int64: cols.labels})
	}
	obs = append(obs,
		anndata.WriteColumn{Name: "x_centroid", Float64: cols.x},
		anndata.WriteColumn{Name: "y_centroid", Float64: cols.y},
		anndata.WriteColumn{Name: "transcript_counts", Uint32: cols.transcripts},
		anndata.WriteColumn{Name: "assigned_transcripts", Uint32: cols.assigned},
		anndata.WriteColumn{Name: "detected_genes", Uint32: cols.genes},
		anndata.WriteColumn{Name: "cell_area", Float64: cols.area},
		anndata.WriteColumn{Name: "nucleus_area", Float64: cols.nucleusArea},
		anndata.WriteColumn{Name: "nucleus_id", Text: cols.nucleus})
	geneIDs := make([]string, len(d.counts.Genes))
	kinds := make([]string, len(d.counts.Genes))
	for i, g := range d.counts.Genes {
		geneIDs[i], kinds[i] = g, "Gene Expression"
		if id := d.geneIDs[g]; id != "" {
			geneIDs[i] = id
		}
	}
	o := &anndata.Object{
		Obs: anndata.WriteFrame{Index: ids, Columns: obs},
		Var: anndata.WriteFrame{Index: d.counts.Genes, Columns: []anndata.WriteColumn{
			{Name: "gene_ids", Text: geneIDs}, {Name: "feature_types", Text: kinds, Categorical: true}}},
		Obsm: map[string]anndata.Dense{"spatial": {Rows: n, Cols: 2, Data: interleave(cols.x, cols.y)}},
		Uns: map[string]any{"situcraft": map[string]any{
			"software":     d.software,
			"segmentation": map[string]any{"id": d.seg.ID, "name": d.seg.Name, "method": d.seg.Method, "version": d.seg.Version, "citation": d.seg.Citation},
			"dataset":      d.ds.Name(),
			"counts":       "gene transcripts of quality 20 or more",
			"units":        "µm",
		}},
	}
	o.X.Indptr = make([]int64, n+1)
	for i, v := range d.counts.Offsets {
		o.X.Indptr[i] = int64(v)
	}
	o.X.Indices = make([]int32, len(d.counts.Gene))
	o.X.Data = make([]float32, len(d.counts.Count))
	for k := range d.counts.Gene {
		o.X.Indices[k] = int32(d.counts.Gene[k])
		o.X.Data[k] = float32(d.counts.Count[k])
	}
	if spatial {
		// The table's keys, in its attributes as spatialdata reads them, and in uns as older
		// versions kept them.
		o.Uns["spatialdata_attrs"] = map[string]any{"region": LabelsElement, "region_key": "region", "instance_key": LabelsElement}
		o.Attrs = map[string]any{"spatialdata-encoding-type": "ngff:regions_table", "version": "0.2",
			"region": LabelsElement, "region_key": "region", "instance_key": LabelsElement}
	}
	return o
}

func interleave(x, y []float64) []float64 {
	out := make([]float64, 0, 2*len(x))
	for i := range x {
		out = append(out, x[i], y[i])
	}
	return out
}

func writeAnnData(d *data, root string) error {
	w, err := zarr.NewWriter(root, 2, nil)
	if err != nil {
		return err
	}
	// The object is the store's root.
	return anndata.Write(w, "", d.object(false))
}

// coordinateSystem is an NGFF coordinate system of these axes ("x", "y") in unit.
func coordinateSystem(name string, axes []string, unit string) map[string]any {
	list := make([]any, len(axes))
	for i, a := range axes {
		list[i] = map[string]any{"name": a, "type": "space", "unit": unit}
	}
	return map[string]any{"name": name, "axes": list}
}

func writeSpatialData(ctx context.Context, d *data, root string, step Progress) error {
	w, err := zarr.NewWriter(root, 3, map[string]any{"spatialdata_attrs": map[string]any{"version": "0.2"}})
	if err != nil {
		return err
	}
	identity := func(axes []string, input string) []any {
		return []any{map[string]any{"type": "identity", "input": coordinateSystem(input, axes, "micrometer"), "output": coordinateSystem("global", axes, "micrometer")}}
	}
	// The label image.
	r, err := d.loadRaster()
	if err != nil {
		return err
	}
	if err := w.Group("labels", map[string]any{"ome": map[string]any{"labels": []string{LabelsElement}}}); err != nil {
		return err
	}
	yx := []any{map[string]any{"name": "y", "type": "space"}, map[string]any{"name": "x", "type": "space"}}
	labelAttrs := map[string]any{
		"ome": map[string]any{
			"version": "0.5-dev-spatialdata",
			"multiscales": []any{map[string]any{
				"name": LabelsElement, "axes": yx,
				"datasets": []any{map[string]any{"path": "s0", "coordinateTransformations": []any{map[string]any{"type": "scale", "scale": []float64{1, 1}}}}},
				"coordinateTransformations": []any{map[string]any{"type": "scale", "scale": []float64{r.PixelSize, r.PixelSize},
					"input":  coordinateSystem("yx", []string{"y", "x"}, "pixel"),
					"output": coordinateSystem("global", []string{"y", "x"}, "micrometer")}},
			}},
			"image-label": map[string]any{"version": "0.5"},
		},
		"spatialdata_attrs": map[string]any{"version": "0.3"},
	}
	base := "labels/" + LabelsElement
	if err := w.Group(base, labelAttrs); err != nil {
		return err
	}
	cy, cx := min(labelChunk, r.Height), min(labelChunk, r.Width)
	a, err := w.Array(base+"/s0", zarr.ArraySpec{Shape: []int{r.Height, r.Width}, Chunks: []int{cy, cx}, DType: "<u4", Dimensions: []string{"y", "x"}})
	if err != nil {
		return err
	}
	chunk := make([]byte, 4*cy*cx)
	row := make([]uint32, cx)
	for gy := 0; gy*cy < r.Height; gy++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		step(0.3*float64(gy*cy)/float64(r.Height), "Writing the label image")
		for gx := 0; gx*cx < r.Width; gx++ {
			clear(chunk)
			x0, x1 := gx*cx, min((gx+1)*cx, r.Width)
			for y := gy * cy; y < min((gy+1)*cy, r.Height); y++ {
				r.RowRange(y, x0, x1, row[:x1-x0])
				at := 4 * (y - gy*cy) * cx
				for i, v := range row[:x1-x0] {
					binary.LittleEndian.PutUint32(chunk[at+4*i:], v)
				}
			}
			if err := a.WriteChunk([]int{gy, gx}, chunk); err != nil {
				return err
			}
		}
	}

	// The outlines.
	step(0.3, "Writing the outlines")
	if err := w.Group("shapes", nil); err != nil {
		return err
	}
	shapes := "shapes/" + ShapesElement
	if err := w.Group(shapes, map[string]any{"encoding-type": "ngff:shapes", "axes": []string{"x", "y"},
		"coordinateTransformations": identity([]string{"x", "y"}, "xy"), "spatialdata_attrs": map[string]any{"version": "0.2"}}); err != nil {
		return err
	}
	if err := writeShapes(d, filepath.Join(root, filepath.FromSlash(shapes), "shapes.parquet")); err != nil {
		return err
	}

	// The transcripts.
	step(0.4, "Writing the transcripts")
	if err := w.Group("points", nil); err != nil {
		return err
	}
	points := "points/" + PointsElement
	// Three-dimensional, as spatialdata writes points with z.
	if err := w.Group(points, map[string]any{"encoding-type": "ngff:points", "axes": []string{"x", "y", "z"},
		"coordinateTransformations": identity([]string{"x", "y", "z"}, "xyz"),
		"spatialdata_attrs":         map[string]any{"feature_key": "feature_name", "instance_key": "cell_id", "version": "0.2"}}); err != nil {
		return err
	}
	if err := writePoints(ctx, d, filepath.Join(root, filepath.FromSlash(points), "points.parquet", "part.0.parquet")); err != nil {
		return err
	}

	// The table.
	step(0.9, "Writing the table")
	if err := w.Group("tables", nil); err != nil {
		return err
	}
	return anndata.Write(w, "tables/"+TableElement, d.object(true))
}

// wkbPolygon is a polygon of one ring, closed, in WKB (little-endian).
func wkbPolygon(xs, ys []float32) []byte {
	n := len(xs) + 1
	out := make([]byte, 0, 13+16*n)
	out = append(out, 1)
	out = binary.LittleEndian.AppendUint32(out, 3)
	out = binary.LittleEndian.AppendUint32(out, 1)
	out = binary.LittleEndian.AppendUint32(out, uint32(n))
	for k := 0; k < n; k++ {
		j := k % len(xs)
		out = binary.LittleEndian.AppendUint64(out, math.Float64bits(float64(xs[j])))
		out = binary.LittleEndian.AppendUint64(out, math.Float64bits(float64(ys[j])))
	}
	return out
}

func writeShapes(d *data, path string) error {
	fields := []parquet.Field{{Name: "geometry", Type: parquet.ByteArray}, str("cell_id")}
	bbox := []float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	c := columns(fields, 0)
	for i, o := range d.outlines {
		if len(o.X) < 3 {
			continue
		}
		c[0].Bytes = append(c[0].Bytes, wkbPolygon(o.X, o.Y))
		c[1].Bytes = append(c[1].Bytes, []byte(d.cells[i].ID))
		for k := range o.X {
			x, y := float64(o.X[k]), float64(o.Y[k])
			bbox[0], bbox[1], bbox[2], bbox[3] = math.Min(bbox[0], x), math.Min(bbox[1], y), math.Max(bbox[2], x), math.Max(bbox[3], y)
		}
	}
	geo := map[string]any{"version": "1.0.0", "primary_column": "geometry", "columns": map[string]any{
		"geometry": map[string]any{"encoding": "WKB", "geometry_types": []string{"Polygon"}, "crs": nil}}}
	if len(c[0].Bytes) > 0 {
		geo["columns"].(map[string]any)["geometry"].(map[string]any)["bbox"] = bbox
	}
	pandas := map[string]any{
		"index_columns":  []string{"cell_id"},
		"column_indexes": []any{map[string]any{"name": nil, "field_name": nil, "pandas_type": "unicode", "numpy_type": "object", "metadata": map[string]any{"encoding": "UTF-8"}}},
		"columns": []any{
			map[string]any{"name": "geometry", "field_name": "geometry", "pandas_type": "bytes", "numpy_type": "object", "metadata": nil},
			map[string]any{"name": "cell_id", "field_name": "cell_id", "pandas_type": "unicode", "numpy_type": "object", "metadata": nil},
		},
		"creator": map[string]any{"library": "situcraft", "version": d.software},
	}
	g, _ := json.Marshal(geo)
	p, _ := json.Marshal(pandas)
	s, err := newParquet(path, fields, d.software, map[string]string{"geo": string(g), "pandas": string(p)})
	if err != nil {
		return err
	}
	if err := s.write(c); err != nil {
		s.close()
		return err
	}
	return s.close()
}

// PointFields are the points' columns (transcript_id only when the dataset has them).
var PointFields = []parquet.Field{f32("x"), f32("y"), f32("z"), str("feature_name"), str("cell_id"), f32("qv"), u64("transcript_id")}

func writePoints(ctx context.Context, d *data, path string) error {
	var s *parquetSink
	err := d.transcriptBatches(ctx, func(ids bool) error {
		fields := PointFields
		if !ids {
			fields = fields[:len(fields)-1]
		}
		var err error
		s, err = newParquet(path, fields, d.software, nil)
		return err
	}, func(c []*parquet.Values) error {
		// From the transcripts table's columns (transcript_id first when present).
		off := 0
		var id *parquet.Values
		if len(c) == len(TranscriptFields) {
			id, off = c[0], 1
		}
		cell, gene, x, y, z, qv := c[off], c[off+1], c[off+2], c[off+3], c[off+4], c[off+5]
		cols := []*parquet.Values{x, y, z, gene, cell, qv}
		if id != nil {
			cols = append(cols, id)
		}
		return s.write(cols)
	})
	if s != nil {
		if e := s.close(); err == nil {
			err = e
		}
	}
	return err
}
