package imports

// SpatialData Zarr (Marconato et al. 2025), as the spatialdata Python package writes it, in format
// 2 or 3, a directory or a zip:
//
//   - shapes/<name>: GeoParquet (shapes.parquet; polygons as WKB, the cells' ids in its index);
//   - labels/<name>: a multiscale label image (its finest level is read);
//   - points/<name>: Parquet (points.parquet, one file or a folder of parts) with x, y, the feature
//     (the element's feature_key) and, from a segmentation, each transcript's cell;
//   - tables/<name>: an AnnData table annotating an element (region), whose instance_key column
//     holds that element's ids (a label image's label values), with the counts in X.
//
// Each element's coordinateTransformations map it to coordinate systems; the one named (global
// unless the options say otherwise) is used, in the units the options give (µm unless pixels).
// The cells come from a shapes or labels element (the options name it; else the one the table
// annotates, else one named for cells). With a label image, each label's cell id is the table's
// row for it (its obs index), else the label itself; the cells' outlines are those of a shapes
// element with the same ids, when there is one, else traced.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"situcraft/internal/anndata"
	"situcraft/internal/parquet"
	"situcraft/internal/zarr"
)

// Citation of the SpatialData format.
const spatialDataCitation = "Marconato L, Palla G, Yamauchi KA, et al. SpatialData: an open and universal data framework for spatial omics. Nat Methods. 2025;22:58–62. doi:10.1038/s41592-024-02212-x"

type spatialStore struct {
	store zarr.Store
	dir   string // the store's folder (empty for a zip)
	elems map[string][]string
	keys  []string
}

func openSpatialData(p string) (*spatialStore, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	s := &spatialStore{elems: map[string][]string{}}
	if info.IsDir() {
		s.store, s.dir = zarr.OpenDir(p), p
	} else {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		// The archive stays open while the import reads it; it is small next to its contents.
		if s.store, err = zarr.OpenZip(f, info.Size()); err != nil {
			f.Close()
			return nil, err
		}
	}
	if s.keys, err = s.store.Keys(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, k := range s.keys {
		parts := strings.SplitN(k, "/", 3)
		if len(parts) < 3 {
			continue
		}
		switch parts[0] {
		case "shapes", "labels", "points", "tables", "images":
			if !seen[parts[0]+"/"+parts[1]] {
				seen[parts[0]+"/"+parts[1]] = true
				s.elems[parts[0]] = append(s.elems[parts[0]], parts[1])
			}
		}
	}
	if len(s.elems["shapes"])+len(s.elems["labels"]) == 0 {
		return nil, fmt.Errorf("%s is not a SpatialData store with shapes or labels", filepath.Base(p))
	}
	for _, list := range s.elems {
		sort.Strings(list)
	}
	return s, nil
}

// file opens one of the store's files (a Parquet file) for reading at offsets.
func (s *spatialStore) file(key string) (io.ReaderAt, int64, func(), error) {
	if s.dir != "" {
		f, err := os.Open(filepath.Join(s.dir, filepath.FromSlash(path.Clean("/" + key)[1:])))
		if err != nil {
			return nil, 0, nil, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, 0, nil, err
		}
		return f, info.Size(), func() { f.Close() }, nil
	}
	raw, err := s.store.Get(key)
	if err != nil {
		return nil, 0, nil, err
	}
	return bytes.NewReader(raw), int64(len(raw)), func() {}, nil
}

func (s *spatialStore) has(kind, name string) bool {
	for _, n := range s.elems[kind] {
		if n == name {
			return true
		}
	}
	return false
}

// tableInfo is what a table says of the element it annotates.
type tableInfo struct {
	name        string
	region      []string
	regionKey   string
	instanceKey string
}

func (s *spatialStore) tables() []tableInfo {
	var out []tableInfo
	for _, name := range s.elems["tables"] {
		attrs, _ := zarr.Attributes(s.store, "tables/"+name)
		t := tableInfo{name: name}
		t.regionKey, _ = attrs["region_key"].(string)
		t.instanceKey, _ = attrs["instance_key"].(string)
		switch r := attrs["region"].(type) {
		case string:
			t.region = []string{r}
		case []any:
			for _, v := range r {
				if sv, ok := v.(string); ok {
					t.region = append(t.region, sv)
				}
			}
		}
		if t.instanceKey == "" {
			// Older stores keep them in uns/spatialdata_attrs.
			for key, dst := range map[string]*string{"instance_key": &t.instanceKey, "region_key": &t.regionKey} {
				if v := scalarText(s.store, "tables/"+name+"/uns/spatialdata_attrs/"+key); v != "" {
					*dst = v
				}
			}
			if v := scalarText(s.store, "tables/"+name+"/uns/spatialdata_attrs/region"); v != "" {
				t.region = []string{v}
			} else if a, err := zarr.OpenArray(s.store, "tables/"+name+"/uns/spatialdata_attrs/region"); err == nil {
				t.region, _ = a.ReadStrings()
			}
		}
		out = append(out, t)
	}
	return out
}

// scalarText reads a 0- or 1-dimensional text array of one value ("" when there is none).
func scalarText(s zarr.Store, p string) string {
	a, err := zarr.OpenArray(s, p)
	if err != nil {
		return ""
	}
	if len(a.Shape) == 0 && a.DType.Kind != 'O' {
		// numpy's fixed-width text, one element (format 2's key "0" either way).
		a.Shape, a.Chunks = []int{1}, []int{1}
	}
	v, err := a.ReadStrings()
	if err != nil || len(v) != 1 {
		return ""
	}
	return v[0]
}

func readSpatialData(ctx context.Context, opt Options, scale float64) (*source, error) {
	if len(opt.Paths) != 1 {
		return nil, errors.New("name one SpatialData store (a .zarr folder or its zip)")
	}
	root := opt.Paths[0]
	s, err := openSpatialData(root)
	if err != nil {
		return nil, err
	}
	system := opt.CoordinateSystem
	if system == "" {
		system = "global"
	}
	src := newSource()
	src.toolName, src.toolVer, src.citation = opt.Tool, opt.ToolVersion, opt.Citation
	if src.citation == "" {
		src.citation = spatialDataCitation
	}
	src.read["coordinate_system"] = system
	if attrs, err := zarr.Attributes(s.store, ""); err == nil {
		if sd, ok := attrs["spatialdata_attrs"].(map[string]any); ok {
			if v, ok := sd["spatialdata_software_version"].(string); ok {
				src.read["written_by"] = "spatialdata " + v
			}
		}
	}

	// The cells' element.
	tables := s.tables()
	cells := opt.Cells
	if cells == "" {
		cells = chooseCells(s, tables)
	}
	kind := ""
	switch {
	case s.has("labels", cells):
		kind = "labels"
	case s.has("shapes", cells):
		kind = "shapes"
	default:
		return nil, fmt.Errorf("the store has no shapes or labels named %q (shapes: %v; labels: %v)", cells, s.elems["shapes"], s.elems["labels"])
	}
	src.read["cells"] = kind + "/" + cells
	var table *tableInfo
	for i := range tables {
		if opt.Table != "" && tables[i].name != opt.Table {
			continue
		}
		for _, r := range tables[i].region {
			if r == cells {
				table = &tables[i]
			}
		}
	}
	var frame *anndata.Frame
	if table != nil {
		if frame, err = anndata.ReadFrame(s.store, "tables/"+table.name+"/obs"); err != nil {
			return nil, fmt.Errorf("table %s: %w", table.name, err)
		}
		src.read["table"] = table.name
	}

	switch kind {
	case "shapes":
		shapes, err := readShapes(s, cells, system, scale)
		if err != nil {
			return nil, fmt.Errorf("shapes %s: %w", cells, err)
		}
		src.polygons = [][]polygon{}
		for _, sh := range shapes {
			k := src.cell(sh.id)
			src.polygons[k] = append(src.polygons[k], sh.polygons...)
		}
	case "labels":
		img, err := readLabels(s, cells, system, scale)
		if err != nil {
			return nil, fmt.Errorf("labels %s: %w", cells, err)
		}
		// Each label's cell: the table's row naming it, else the label itself.
		img.cellOf = map[uint32]int32{}
		if frame != nil && frame.Column(table.instanceKey) != nil {
			inst := frame.Column(table.instanceKey)
			for i := 0; i < inst.Len(); i++ {
				v, err := strconv.ParseUint(inst.String(i), 10, 32)
				if err != nil || v == 0 {
					continue
				}
				img.cellOf[uint32(v)] = src.cell(frame.Index.String(i))
			}
			src.read["label_ids"] = "the table's index (" + table.instanceKey + " holds each label)"
		}
		present := map[uint32]bool{}
		for _, v := range img.values {
			if v != 0 {
				present[v] = true
			}
		}
		values := make([]uint32, 0, len(present))
		for v := range present {
			values = append(values, v)
		}
		sort.Slice(values, func(a, b int) bool { return values[a] < values[b] })
		unnamed := 0
		for _, v := range values {
			if _, ok := img.cellOf[v]; !ok {
				img.cellOf[v] = src.cell(strconv.FormatUint(uint64(v), 10))
				unnamed++
			}
		}
		if frame != nil && unnamed > 0 {
			src.find("warning", "labels.unnamed", "Labels the table has no row for (named by their value)", unnamed)
		}
		src.labels = img
		// Outlines from a shapes element with the same ids, when there is one.
		for _, name := range s.elems["shapes"] {
			shapes, err := readShapes(s, name, system, scale)
			if err != nil {
				continue
			}
			same := 0
			for _, sh := range shapes {
				if _, ok := src.index[sh.id]; ok {
					same++
				}
			}
			if same*10 >= len(src.ids)*9 && same > 0 {
				src.polygons = make([][]polygon, len(src.ids))
				for _, sh := range shapes {
					if k, ok := src.index[sh.id]; ok {
						src.polygons[k] = append(src.polygons[k], sh.polygons...)
					}
				}
				src.read["outlines"] = "shapes/" + name
				break
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// The table's counts, by cell.
	if frame != nil {
		if m, err := anndata.ReadMatrix(s.store, "tables/"+table.name+"/X"); err == nil && m.Rows == frame.Index.Len() {
			sums := m.RowSums()
			src.table = make([]float64, len(src.ids))
			for i := range src.table {
				src.table[i] = math.NaN()
			}
			key := frame.Column(table.instanceKey)
			for r, total := range sums {
				id := frame.Index.String(r)
				if kind == "shapes" && key != nil {
					id = key.String(r)
				}
				if k, ok := src.index[id]; ok {
					src.table[k] = total
				}
			}
			src.tableKind = "genes"
		}
	}

	// The points with the assignments.
	pts := opt.Points
	if pts == "" {
		if s.has("points", "transcripts") {
			pts = "transcripts"
		} else if len(s.elems["points"]) == 1 {
			pts = s.elems["points"][0]
		}
	}
	if pts != "" {
		if !s.has("points", pts) {
			return nil, fmt.Errorf("the store has no points named %q (points: %v)", pts, s.elems["points"])
		}
		if err := readPoints(ctx, s, src, pts, system, scale, opt); err != nil {
			return nil, fmt.Errorf("points %s: %w", pts, err)
		}
	}

	// The files read: the elements used and the store's own metadata.
	if s.dir != "" {
		for _, k := range []string{"zarr.json", ".zgroup", ".zattrs", ".zmetadata"} {
			if _, err := os.Stat(filepath.Join(s.dir, k)); err == nil {
				src.hashFile(filepath.Join(s.dir, k))
			}
		}
		used := []string{kind + "/" + cells}
		if table != nil {
			used = append(used, "tables/"+table.name)
		}
		if pts != "" {
			used = append(used, "points/"+pts)
		}
		if o, ok := src.read["outlines"].(string); ok {
			used = append(used, o)
		}
		for _, u := range used {
			if err := src.hashTree(filepath.Join(s.dir, filepath.FromSlash(u))); err != nil {
				return nil, err
			}
		}
	} else if err := src.hashFile(root); err != nil {
		return nil, err
	}
	return src, nil
}

// chooseCells is the element the table annotates (shapes or labels), else labels or shapes named
// for cells (not nuclei), else the only one.
func chooseCells(s *spatialStore, tables []tableInfo) string {
	for _, t := range tables {
		for _, r := range t.region {
			if s.has("labels", r) || s.has("shapes", r) {
				return r
			}
		}
	}
	for _, kind := range []string{"labels", "shapes"} {
		for _, n := range s.elems[kind] {
			l := strings.ToLower(n)
			if strings.Contains(l, "cell") && !strings.Contains(l, "nucle") {
				return n
			}
		}
	}
	if len(s.elems["shapes"]) == 1 {
		return s.elems["shapes"][0]
	}
	if len(s.elems["labels"]) == 1 {
		return s.elems["labels"][0]
	}
	return ""
}

// elementTransform reads an element's transformation to the coordinate system, scaled to µm.
func elementTransform(attrs map[string]any, system string, axes []string, scale float64) (affine, error) {
	list, _ := attrs["coordinateTransformations"].([]any)
	t, err := transformTo(list, system, axes)
	if err != nil {
		return affine{}, err
	}
	return t.scaled(scale), nil
}

func readShapes(s *spatialStore, name, system string, scale float64) ([]shape, error) {
	base := "shapes/" + name
	attrs, err := zarr.Attributes(s.store, base)
	if err != nil {
		return nil, err
	}
	axes := []string{"x", "y"}
	if list, ok := attrs["axes"].([]any); ok && len(list) > 0 {
		axes = axes[:0]
		for _, a := range list {
			if v, ok := a.(string); ok {
				axes = append(axes, v)
			}
		}
	}
	t, err := elementTransform(attrs, system, axes, scale)
	if err != nil {
		return nil, err
	}
	r, size, done, err := s.file(base + "/shapes.parquet")
	if err != nil {
		return nil, err
	}
	defer done()
	p, err := parquet.Open(r, size)
	if err != nil {
		return nil, err
	}
	geometry := "geometry"
	if geo := p.KeyValue["geo"]; geo != "" {
		var g struct {
			PrimaryColumn string `json:"primary_column"`
		}
		if json.Unmarshal([]byte(geo), &g) == nil && g.PrimaryColumn != "" {
			geometry = g.PrimaryColumn
		}
	}
	gi := p.Index(geometry)
	if gi < 0 {
		return nil, fmt.Errorf("no geometry column %q", geometry)
	}
	if p.Index("radius") >= 0 {
		return nil, errors.New("its shapes are circles, not cell outlines")
	}
	// The index: pandas' metadata names it; a range index is the rows' numbers.
	ii := -1
	var start, step int64 = 0, 1
	if meta := p.KeyValue["pandas"]; meta != "" {
		var m struct {
			IndexColumns []any `json:"index_columns"`
		}
		if json.Unmarshal([]byte(meta), &m) == nil && len(m.IndexColumns) > 0 {
			switch v := m.IndexColumns[0].(type) {
			case string:
				ii = p.Index(v)
			case map[string]any:
				if f, ok := v["start"].(float64); ok {
					start = int64(f)
				}
				if f, ok := v["step"].(float64); ok && f != 0 {
					step = int64(f)
				}
			}
		}
	}
	if ii < 0 {
		ii = p.Index("__index_level_0__")
	}
	var out []shape
	row := int64(0)
	err = eachParquetRow(context.Background(), p, []int{gi, ii}, func(get func(int) string) error {
		id := strconv.FormatInt(start+row*step, 10)
		if ii >= 0 {
			id = get(ii)
		}
		row++
		wkb := get(gi)
		if wkb == "" {
			return nil
		}
		polys, err := readWKB([]byte(wkb))
		if err != nil {
			return fmt.Errorf("shape %s: %w", id, err)
		}
		for i := range polys {
			for r := range polys[i] {
				for k, v := range polys[i][r] {
					x, y := t.apply(v.X, v.Y)
					polys[i][r][k] = point{x, y}
				}
			}
		}
		out = append(out, shape{id: id, polygons: polys})
		return nil
	})
	return out, err
}

func readLabels(s *spatialStore, name, system string, scale float64) (*labelImage, error) {
	base := "labels/" + name
	attrs, err := zarr.Attributes(s.store, base)
	if err != nil {
		return nil, err
	}
	// OME-NGFF 0.5 nests the multiscales under "ome"; 0.4 does not.
	ms := attrs
	if ome, ok := attrs["ome"].(map[string]any); ok {
		ms = ome
	}
	list, _ := ms["multiscales"].([]any)
	if len(list) == 0 {
		return nil, errors.New("no multiscales")
	}
	m, _ := list[0].(map[string]any)
	var axes []string
	for _, a := range func() []any { v, _ := m["axes"].([]any); return v }() {
		switch v := a.(type) {
		case map[string]any:
			n, _ := v["name"].(string)
			axes = append(axes, n)
		case string:
			axes = append(axes, v)
		}
	}
	if len(axes) == 0 {
		axes = []string{"y", "x"}
	}
	datasets, _ := m["datasets"].([]any)
	if len(datasets) == 0 {
		return nil, errors.New("no levels")
	}
	first, _ := datasets[0].(map[string]any)
	level, _ := first["path"].(string)
	levelT := identity()
	if steps, ok := first["coordinateTransformations"].([]any); ok {
		for _, item := range steps {
			step, err := readTransform(item.(map[string]any), axes)
			if err != nil {
				return nil, err
			}
			levelT = levelT.then(step)
		}
	}
	elemT, err := transformTo(func() []any { v, _ := m["coordinateTransformations"].([]any); return v }(), system, axes)
	if err != nil {
		// SpatialData 0.1 kept them beside the multiscales.
		if elemT, err = transformTo(func() []any { v, _ := attrs["coordinateTransformations"].([]any); return v }(), system, axes); err != nil {
			return nil, err
		}
	}
	a, err := zarr.OpenArray(s.store, base+"/"+level)
	if err != nil {
		return nil, err
	}
	if len(a.Shape) != 2 || a.DType.Kind != 'u' && a.DType.Kind != 'i' {
		return nil, fmt.Errorf("level %s is %v of %s, not a 2-D label image", level, a.Shape, a.DType)
	}
	xi, yi := 1, 0
	if len(axes) == 2 && axes[0] == "x" {
		xi, yi = 0, 1
	}
	raw, err := a.ReadAll()
	if err != nil {
		return nil, err
	}
	img := &labelImage{width: a.Shape[xi], height: a.Shape[yi], values: make([]uint32, a.Shape[0]*a.Shape[1]), transform: levelT.then(elemT).scaled(scale)}
	size := a.DType.Size
	for k := range img.values {
		var v uint64
		b := raw[k*size : (k+1)*size]
		switch size {
		case 1:
			v = uint64(b[0])
		case 2:
			v = uint64(binary.LittleEndian.Uint16(b))
		case 4:
			v = uint64(binary.LittleEndian.Uint32(b))
		case 8:
			v = binary.LittleEndian.Uint64(b)
		}
		if a.DType.Kind == 'i' && int64(v)<<(64-8*size)>>(64-8*size) < 0 || v > math.MaxUint32 {
			return nil, fmt.Errorf("label %d is not a label", int64(v))
		}
		// In C order, rows run along the last axis.
		j, i := k/a.Shape[1], k%a.Shape[1]
		if xi == 0 {
			i, j = j, i
		}
		img.values[j*img.width+i] = uint32(v)
	}
	return img, nil
}

func readPoints(ctx context.Context, s *spatialStore, src *source, name, system string, scale float64, opt Options) error {
	base := "points/" + name
	attrs, err := zarr.Attributes(s.store, base)
	if err != nil {
		return err
	}
	t, err := elementTransform(attrs, system, []string{"x", "y"}, scale)
	if err != nil {
		return err
	}
	feature := "feature_name"
	if sd, ok := attrs["spatialdata_attrs"].(map[string]any); ok {
		if f, ok := sd["feature_key"].(string); ok && f != "" {
			feature = f
		}
	}
	var parts []string
	for _, k := range s.keys {
		if strings.HasPrefix(k, base+"/points.parquet") && strings.HasSuffix(k, ".parquet") {
			parts = append(parts, k)
		}
	}
	if len(parts) == 0 {
		return errors.New("no points.parquet")
	}
	r := &records{}
	src.read["points"] = name
	for pi, part := range parts {
		ra, size, done, err := s.file(part)
		if err != nil {
			return err
		}
		p, err := parquet.Open(ra, size)
		if err != nil {
			done()
			return fmt.Errorf("%s: %w", path.Base(part), err)
		}
		header := make([]string, len(p.Columns))
		for i, c := range p.Columns {
			header[i] = c.Name
		}
		at := func(n string) int { return p.Index(n) }
		cols := tableColumns{id: at("transcript_id"), x: at("x"), y: at("y"), gene: at(feature), noise: -1}
		names := cellColumns
		if opt.CellColumn != "" {
			names = []string{opt.CellColumn}
		}
		cols.cell = -1
		for _, n := range names {
			if cols.cell = at(n); cols.cell >= 0 {
				cols.cellName = n
				break
			}
		}
		if cols.cell < 0 {
			done()
			if pi == 0 {
				// Points without assignments: the transcripts go by the cells' pixels.
				src.find("info", "points.no-assignment", "The points have no cell column ("+strings.Join(names, ", ")+"): transcripts are assigned by the cells' pixels", 0)
				src.read["points"] = name + " (no cell column)"
				return nil
			}
			return fmt.Errorf("%s has no cell column", path.Base(part))
		}
		if cols.id < 0 && (cols.x < 0 || cols.y < 0 || cols.gene < 0) {
			done()
			return fmt.Errorf("the points name their transcripts neither by transcript_id nor by x, y and %s", feature)
		}
		r.byID = cols.id >= 0
		r.hasXY = r.byID && cols.x >= 0 && cols.y >= 0
		src.read["cell_column"] = cols.cellName
		err = eachParquetRow(ctx, p, []int{cols.id, cols.x, cols.y, cols.gene, cols.cell}, func(get func(int) string) error {
			if !r.byID {
				x, _ := strconv.ParseFloat(get(cols.x), 64)
				y, _ := strconv.ParseFloat(get(cols.y), 64)
				ux, uy := t.apply(x, y)
				r.x, r.y, r.gene = append(r.x, ux), append(r.y, uy), append(r.gene, get(cols.gene))
				k := int32(-1)
				if c := get(cols.cell); !unassignedValues[c] {
					k = src.cell(c)
				}
				r.cell = append(r.cell, k)
				return nil
			}
			id, err := strconv.ParseUint(get(cols.id), 10, 64)
			if err != nil {
				return fmt.Errorf("transcript_id %q", get(cols.id))
			}
			r.id = append(r.id, id)
			if r.hasXY {
				x, err1 := strconv.ParseFloat(get(cols.x), 64)
				y, err2 := strconv.ParseFloat(get(cols.y), 64)
				if err1 != nil || err2 != nil {
					return fmt.Errorf("the position of transcript %d", id)
				}
				ux, uy := t.apply(x, y)
				r.x, r.y = append(r.x, ux), append(r.y, uy)
			}
			k := int32(-1)
			if c := get(cols.cell); !unassignedValues[c] {
				k = src.cell(c)
			}
			r.cell = append(r.cell, k)
			return nil
		})
		done()
		if err != nil {
			return err
		}
	}
	src.records = r
	src.read["transcripts_by"] = map[bool]string{true: "transcript_id", false: "position and gene"}[r.byID]
	return nil
}
