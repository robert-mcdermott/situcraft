package simulate

// A tissue too large to simulate whole (wave 3, slice 9): a mosaic of TilesAcross × TilesDown
// tiles, each the parameters' field and a tissue of its own (its own layout, cells, molecules and
// stain; the panel and every other parameter shared), simulated one at a time and joined into one
// bundle as each is made, so that only one tile is ever in memory. Each tile's files are written
// as a simulated bundle's are, in a folder of their own, then joined:
//   - the tables (transcripts, cells, boundaries, the truth's) row group by row group, every place
//     shifted to the tile's, the cells renumbered across the mosaic, the transcripts' fields of view
//     numbered across it;
//   - the masks a tile to a chunk, their labels renumbered; the cells' summaries and polygons;
//   - the stain a tile at a time, its reduced levels from each tile's (coarse ones assembled whole);
//   - the counts, the metrics and the descriptions over the whole.
//
// The tiles meet without overlap: a cell at a tile's edge is cut there, as at a field's edge, and
// the instrument's expansion and a transcript's distance to the nearest nucleus stop there. The
// truth is exact.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"situcraft/internal/parquet"
	"situcraft/internal/tiff"
	"situcraft/internal/zarr"
)

// mosaic joins the tiles' files into one bundle.
type mosaic struct {
	p                Params
	across, down     int
	tw, th           int     // a tile's pixels
	ox, oy           float64 // the tile being joined's offset, µm
	dir, software    string
	tables           map[string]*table
	fovs             map[int]uint64 // transcripts so far in each field of view
	fovCols          int
	instBase         int // cells (instrument, true) and true nuclei before the tile being joined
	trueBase         int
	nucBase          int
	instIDs          []string // every instrument cell's id
	masks            map[string]*mosaicMasks
	image            *mosaicImage
	indptr, indices  []int64 // the counts, compressed sparse columns
	data             []int32
	w                written
	pn               *panel
	instMap, trueMap map[string][]byte // the tile's cell ids, renumbered
}

// runMosaic simulates the mosaic and writes it into folder (written beside it, then moved).
func runMosaic(p Params, folder, software string, stop func() bool, progress func(float64, string)) (*Summary, error) {
	parent := filepath.Dir(folder)
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(folder)+".writing-")
	if err != nil {
		return nil, err
	}
	done := false
	defer func() {
		if !done {
			os.RemoveAll(staging)
		}
	}()
	across, down := p.tiles()
	m := &mosaic{p: p, across: across, down: down, tw: int(math.Ceil(p.WidthUm / p.PixelUm)), th: int(math.Ceil(p.HeightUm / p.PixelUm)),
		dir: staging, software: software, tables: map[string]*table{}, fovs: map[int]uint64{}, masks: map[string]*mosaicMasks{},
		indptr: []int64{0}}
	m.fovCols = int(math.Ceil(float64(across*m.tw) * p.PixelUm / fovUm))
	m.w.moleculesByOrigin = map[string]int{}
	m.w.trueCellsByType = map[string]int{}
	if err := os.MkdirAll(filepath.Join(staging, "truth"), 0o755); err != nil {
		return nil, err
	}
	tiles := across * down
	for k := 0; k < tiles; k++ {
		i, j := k%across, k/across
		at := func(f float64) float64 { return (float64(k) + f) / float64(tiles) }
		progress(at(0), fmt.Sprintf("Tile %d of %d: laying out the tissue", k+1, tiles))
		pk := p
		pk.TilesAcross, pk.TilesDown, pk.part = 0, 0, uint64(k)
		t, pn, mols, in, err := simulateTissue(pk, stop, func(float64, string) error {
			if stop() {
				return errStopped
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("tile %d: %w", k+1, err)
		}
		progress(at(0.5), fmt.Sprintf("Tile %d of %d: writing", k+1, tiles))
		tileDir := filepath.Join(staging, ".tile")
		if err := os.MkdirAll(filepath.Join(tileDir, "truth"), 0o755); err != nil {
			return nil, err
		}
		wr := &writer{t: t, in: in, pn: pn, mols: mols, dir: tileDir, software: software}
		wr.count()
		wr.prepare()
		for _, step := range []func() error{wr.writeTranscripts, wr.writeInstrument, func() error {
			return wr.writeCellsZarr(filepath.Join(tileDir, "cells.zarr.zip"), wr.inst)
		}, wr.writeTruth} {
			if err := step(); err != nil {
				return nil, fmt.Errorf("tile %d: %w", k+1, err)
			}
		}
		if err := m.add(wr, i, j); err != nil {
			return nil, fmt.Errorf("tile %d: %w", k+1, err)
		}
		if err := os.RemoveAll(tileDir); err != nil {
			return nil, err
		}
		if stop() {
			return nil, errStopped
		}
	}
	progress(0.99, "Writing the whole")
	if err := m.finish(); err != nil {
		return nil, err
	}
	if err := os.Chmod(staging, 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(staging, folder); err != nil {
		return nil, err
	}
	done = true
	progress(1, "Done")
	return m.writer().summary(folder), nil
}

// writer is the mosaic's whole as the bundle's writer describes it.
func (m *mosaic) writer() *writer {
	t := &tissue{p: m.p, w: m.across * m.tw, h: m.down * m.th}
	return &writer{t: t, pn: m.pn, dir: m.dir, software: m.software, run: runOf(m.p, m.software), instID: m.instIDs, w: m.w}
}

// add joins a tile, the tile (i, j) of the mosaic.
func (m *mosaic) add(wr *writer, i, j int) error {
	ps := m.p.PixelUm
	m.ox, m.oy = mul(float64(i*m.tw), ps), mul(float64(j*m.th), ps)
	m.pn = wr.pn
	// The tile's cells, renumbered across the mosaic.
	m.instMap, m.trueMap = map[string][]byte{}, map[string][]byte{}
	for k, id := range wr.instID {
		_, g := cellID(m.instBase+k, 0)
		m.instMap[id] = []byte(g)
		m.instIDs = append(m.instIDs, g)
	}
	for k, id := range wr.trueID {
		_, g := cellID(m.trueBase+k, trueSalt)
		m.trueMap[id] = []byte(g)
	}
	src := func(name string) string { return filepath.Join(wr.dir, filepath.FromSlash(name)) }
	transcripts := func(ids map[string][]byte) func(c map[string]*parquet.Values) {
		return func(c map[string]*parquet.Values) {
			m.renumber(c["cell_id"], ids)
			m.shift(c, "x_location", "y_location")
			m.shift(c, "true_x_location", "true_y_location")
			// Fields of view across the mosaic, and the transcripts' ids in them.
			x, y := c["x_location"].Float32, c["y_location"].Float32
			names := map[int][]byte{}
			for r := range x {
				row, col := int(float64(y[r])/fovUm), int(float64(x[r])/fovUm)
				fov := row*m.fovCols + col
				c["transcript_id"].Int64[r] = int64(uint64(fov+1)<<48 | m.fovs[fov])
				m.fovs[fov]++
				if names[fov] == nil {
					names[fov] = []byte(fovName(row, col))
				}
				c["fov_name"].Bytes[r] = names[fov]
			}
		}
	}
	// Both files of transcripts number their fields of view alike: the truth's from the same counts.
	saved := map[int]uint64{}
	for k, v := range m.fovs {
		saved[k] = v
	}
	if err := m.copyTable(src("transcripts.parquet"), "transcripts.parquet", transcripts(m.instMap)); err != nil {
		return err
	}
	m.fovs = saved
	if err := m.copyTable(src("truth/transcripts.parquet"), "truth/transcripts.parquet", transcripts(m.trueMap)); err != nil {
		return err
	}
	cells := func(ids map[string][]byte) func(c map[string]*parquet.Values) {
		return func(c map[string]*parquet.Values) {
			m.renumber(c["cell_id"], ids)
			m.shift(c, "x_centroid", "y_centroid")
			// The truth's domains, each tile's its own: numbered across the mosaic.
			if d := c["domain"]; d != nil {
				for r := range d.Int32 {
					d.Int32[r] += int32((j*m.across + i) * m.p.Domains)
				}
			}
		}
	}
	boundaries := func(ids map[string][]byte, base int) func(c map[string]*parquet.Values) {
		return func(c map[string]*parquet.Values) {
			m.renumber(c["cell_id"], ids)
			m.shift(c, "vertex_x", "vertex_y")
			for r := range c["label_id"].Int32 {
				c["label_id"].Int32[r] += int32(base)
			}
		}
	}
	trueNuclei := 0
	for _, c := range wr.t.cells {
		if c.nucleus {
			trueNuclei++
		}
	}
	for _, f := range []struct {
		name string
		edit func(c map[string]*parquet.Values)
	}{
		{"cells.parquet", cells(m.instMap)},
		{"cell_boundaries.parquet", boundaries(m.instMap, m.instBase)},
		{"nucleus_boundaries.parquet", boundaries(m.instMap, m.instBase)},
		{"truth/cells.parquet", cells(m.trueMap)},
		{"truth/cell_boundaries.parquet", boundaries(m.trueMap, m.trueBase)},
		{"truth/nucleus_boundaries.parquet", boundaries(m.trueMap, m.nucBase)},
	} {
		if err := m.copyTable(src(f.name), f.name, f.edit); err != nil {
			return err
		}
	}
	// The masks: the instrument's nuclei are labelled as their cells; the truth's apart.
	if err := m.addMasks("cells.zarr.zip", src("cells.zarr.zip"), i, j, m.instBase, m.instBase, 0); err != nil {
		return err
	}
	if err := m.addMasks("truth/cells.zarr.zip", src("truth/cells.zarr.zip"), i, j, m.trueBase, m.nucBase, trueSalt); err != nil {
		return err
	}
	// The stain.
	if m.image == nil {
		var err error
		if m.image, err = m.newImage(); err != nil {
			return err
		}
	}
	if err := m.image.add(wr.t.stain(), i, j); err != nil {
		return err
	}
	// The counts, a column for each of the tile's cells.
	indptr, indices, data := wr.cellCounts()
	base := m.indptr[len(m.indptr)-1]
	for _, at := range indptr[1:] {
		m.indptr = append(m.indptr, base+at)
	}
	m.indices, m.data = append(m.indices, indices...), append(m.data, data...)
	// What the tile holds.
	tw := &wr.w
	m.w.trueCells += tw.trueCells
	m.w.instrumentCells += tw.instrumentCells
	m.w.transcripts += tw.transcripts
	m.w.q20Genes += tw.q20Genes
	m.w.q20GenesAssigned += tw.q20GenesAssigned
	m.w.q20GenesNuclear += tw.q20GenesNuclear
	m.w.genesAnyQuality += tw.genesAnyQuality
	m.w.nucleusPixels += tw.nucleusPixels
	m.w.cellArea += tw.cellArea
	m.w.transcriptCounts = append(m.w.transcriptCounts, tw.transcriptCounts...)
	m.w.controlCounts = append(m.w.controlCounts, tw.controlCounts...)
	m.w.genesPerCell = append(m.w.genesPerCell, tw.genesPerCell...)
	for k, v := range tw.trueCellsByType {
		m.w.trueCellsByType[k] += v
	}
	m.w.visibleNuclei += tw.visibleNuclei
	for k, v := range tw.moleculesByOrigin {
		m.w.moleculesByOrigin[k] += v
	}
	m.w.wide += tw.wide
	m.w.lost += tw.lost
	m.instBase += len(wr.in.cells)
	m.trueBase += len(wr.t.cells)
	m.nucBase += trueNuclei
	return nil
}

// shift moves a pair of coordinate columns (µm) to the tile's place.
func (m *mosaic) shift(c map[string]*parquet.Values, xName, yName string) {
	x, y := c[xName], c[yName]
	if x == nil || y == nil {
		return
	}
	for r := range x.Float32 {
		x.Float32[r] += float32(m.ox)
		y.Float32[r] += float32(m.oy)
	}
	for r := range x.Float64 {
		x.Float64[r] += m.ox
		y.Float64[r] += m.oy
	}
}

// renumber gives a column of cell ids the mosaic's (UNASSIGNED stays).
func (m *mosaic) renumber(v *parquet.Values, ids map[string][]byte) {
	for r, id := range v.Bytes {
		if g, ok := ids[string(id)]; ok {
			v.Bytes[r] = g
		}
	}
}

// table is a Parquet file of the mosaic, written a row group at a time.
type table struct {
	f   *os.File
	buf *bufio.Writer
	w   *parquet.Writer
}

// fieldsOf are a file's columns as a Writer writes them.
func fieldsOf(f *parquet.File) []parquet.Field {
	out := make([]parquet.Field, len(f.Columns))
	for i, c := range f.Columns {
		ann := ""
		switch {
		case c.Logical == "STRING" || c.ConvertedType == 0:
			ann = "string"
		case c.IntegerBits == 8 && !c.Signed:
			ann = "uint8"
		case c.IntegerBits > 0 && !c.Signed:
			ann = "uint"
		}
		out[i] = parquet.Field{Name: c.Name, Type: c.Type, Annotation: ann}
	}
	return out
}

// copyTable copies a tile's table into the mosaic's, each row group through edit (its columns by
// name).
func (m *mosaic) copyTable(path, name string, edit func(c map[string]*parquet.Values)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	pf, err := parquet.Open(f, st.Size())
	if err != nil {
		return err
	}
	t := m.tables[name]
	if t == nil {
		out, err := os.Create(filepath.Join(m.dir, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		buf := bufio.NewWriterSize(out, 1<<20)
		w, err := parquet.NewWriter(buf, fieldsOf(pf), m.software)
		if err != nil {
			out.Close()
			return err
		}
		t = &table{out, buf, w}
		m.tables[name] = t
	}
	for g := range pf.RowGroups {
		cols := make([]*parquet.Values, len(pf.Columns))
		byName := map[string]*parquet.Values{}
		for i, c := range pf.Columns {
			if cols[i], err = pf.ReadColumn(g, i); err != nil {
				return err
			}
			byName[c.Name] = cols[i]
		}
		edit(byName)
		if err := t.w.WriteRowGroup(cols); err != nil {
			return err
		}
	}
	return nil
}

// mosaicMasks is a cells.zarr.zip being joined: its masks written a tile to a chunk, the rest
// gathered.
type mosaicMasks struct {
	dir           string
	z             *zarr.Writer
	nuclei, cells *zarr.ArrayWriter
	salt          uint32
	n             int
	ids           []uint32
	summary       []byte
	summaryAttrs  map[string]any
	transform     []byte
	methods       []string
	sets          [2]polygons
}

type polygons struct {
	vertices          [][]float32
	count, cell, kind []uint32
}

// addMasks joins a tile's cells.zarr.zip: its masks a chunk each (labels renumbered: cells from
// cellBase, nuclei from nucleusBase), its cells' ids, summaries and polygons shifted and renumbered.
func (m *mosaic) addMasks(name, path string, i, j, cellBase, nucleusBase int, salt uint32) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	store, err := zarr.OpenZip(f, st.Size())
	if err != nil {
		return err
	}
	mm := m.masks[name]
	if mm == nil {
		dir, err := os.MkdirTemp(m.dir, ".zarr-")
		if err != nil {
			return err
		}
		root, err := zarr.Attributes(store, "")
		if err != nil {
			return err
		}
		var methods []string
		if list, ok := root["segmentation_methods"].([]any); ok {
			for _, x := range list {
				methods = append(methods, fmt.Sprint(x))
			}
		}
		if len(methods) != 2 {
			return fmt.Errorf("%s: no segmentation methods", name)
		}
		z, err := zarr.NewWriter(dir, 2, cellsZarrAttrs(0, methods))
		if err != nil {
			return err
		}
		if err := z.Group("masks", nil); err != nil {
			return err
		}
		mm = &mosaicMasks{dir: dir, z: z, salt: salt, methods: methods}
		h, w := m.down*m.th, m.across*m.tw
		for _, a := range []struct {
			path string
			dst  **zarr.ArrayWriter
		}{{"masks/0", &mm.nuclei}, {"masks/1", &mm.cells}} {
			if *a.dst, err = z.Array(a.path, zarr.ArraySpec{Shape: []int{h, w}, Chunks: []int{m.th, m.tw}, DType: "<u4"}); err != nil {
				return err
			}
		}
		m.masks[name] = mm
	}
	for _, a := range []struct {
		path string
		dst  *zarr.ArrayWriter
		base int
	}{{"masks/0", mm.nuclei, nucleusBase}, {"masks/1", mm.cells, cellBase}} {
		arr, err := zarr.OpenArray(store, a.path)
		if err != nil {
			return err
		}
		if len(arr.Shape) != 2 || arr.Shape[0] != m.th || arr.Shape[1] != m.tw {
			return fmt.Errorf("%s: a tile's %s is %v, not %d × %d", name, a.path, arr.Shape, m.th, m.tw)
		}
		raw, err := arr.ReadAll()
		if err != nil {
			return err
		}
		for k := 0; k+4 <= len(raw); k += 4 {
			if l := binary.LittleEndian.Uint32(raw[k:]); l != 0 {
				binary.LittleEndian.PutUint32(raw[k:], l+uint32(a.base))
			}
		}
		if err := a.dst.WriteChunk([]int{j, i}, raw); err != nil {
			return err
		}
	}
	read := func(p string) (*zarr.Array, []byte, error) {
		arr, err := zarr.OpenArray(store, p)
		if err != nil {
			return nil, nil, err
		}
		raw, err := arr.ReadAll()
		return arr, raw, err
	}
	if mm.transform == nil {
		if _, mm.transform, err = read("masks/homogeneous_transform"); err != nil {
			return err
		}
	}
	// Cells: their ids, and summaries (centroids shifted where there is one).
	sa, summary, err := read("cell_summary")
	if err != nil {
		return err
	}
	mm.summaryAttrs = sa.Attrs
	n := len(summary) / 64
	for k := 0; k < n; k++ {
		row := summary[64*k : 64*(k+1)]
		get := func(c int) float64 { return math.Float64frombits(binary.LittleEndian.Uint64(row[8*c:])) }
		set := func(c int, v float64) { binary.LittleEndian.PutUint64(row[8*c:], math.Float64bits(v)) }
		if get(2) > 0 {
			set(0, get(0)+m.ox)
			set(1, get(1)+m.oy)
		}
		if get(7) > 0 {
			set(3, get(3)+m.ox)
			set(4, get(4)+m.oy)
		}
		prefix, _ := cellID(cellBase+k, salt)
		mm.ids = append(mm.ids, prefix, 1)
	}
	mm.summary = append(mm.summary, summary...)
	mm.n += n
	// Polygons: set 0 the nuclei, 1 the cells.
	for set := 0; set < 2; set++ {
		dir := fmt.Sprintf("polygon_sets/%d/", set)
		va, vertices, err := read(dir + "vertices")
		if err != nil {
			return err
		}
		_, counts, err := read(dir + "num_vertices")
		if err != nil {
			return err
		}
		_, cells, err := read(dir + "cell_index")
		if err != nil {
			return err
		}
		_, kinds, err := read(dir + "method")
		if err != nil {
			return err
		}
		width := va.Shape[1]
		ps := &mm.sets[set]
		for p := 0; p < len(counts)/4; p++ {
			c := int(binary.LittleEndian.Uint32(counts[4*p:]))
			v := make([]float32, 2*c)
			for q := 0; q < c; q++ {
				v[2*q] = math.Float32frombits(binary.LittleEndian.Uint32(vertices[4*(p*width+2*q):])) + float32(m.ox)
				v[2*q+1] = math.Float32frombits(binary.LittleEndian.Uint32(vertices[4*(p*width+2*q+1):])) + float32(m.oy)
			}
			ps.vertices = append(ps.vertices, v)
			ps.count = append(ps.count, uint32(c))
			ps.cell = append(ps.cell, binary.LittleEndian.Uint32(cells[4*p:])+uint32(cellBase))
			ps.kind = append(ps.kind, binary.LittleEndian.Uint32(kinds[4*p:]))
		}
	}
	return nil
}

// finishMasks writes a cells.zarr.zip's gathered arrays, then zips it.
func (m *mosaic) finishMasks(name string, mm *mosaicMasks) error {
	z := mm.z
	if err := z.Group("", cellsZarrAttrs(mm.n, mm.methods)); err != nil {
		return err
	}
	if err := z.WriteArray("masks/homogeneous_transform", "<f4", []int{4, 4}, mm.transform, 0, nil); err != nil {
		return err
	}
	ids := make([]byte, 0, 4*len(mm.ids))
	for _, x := range mm.ids {
		ids = binary.LittleEndian.AppendUint32(ids, x)
	}
	if err := z.WriteArray("cell_id", "<u4", []int{mm.n, 2}, ids, 0, nil); err != nil {
		return err
	}
	if err := z.WriteArray("cell_summary", "<f8", []int{mm.n, 8}, mm.summary, 0, mm.summaryAttrs); err != nil {
		return err
	}
	if err := z.Group("polygon_sets", nil); err != nil {
		return err
	}
	u32s := func(v []uint32) []byte {
		b := make([]byte, 0, 4*len(v))
		for _, x := range v {
			b = binary.LittleEndian.AppendUint32(b, x)
		}
		return b
	}
	for set, ps := range mm.sets {
		dir := fmt.Sprintf("polygon_sets/%d", set)
		if err := z.Group(dir, nil); err != nil {
			return err
		}
		width := 1
		for _, c := range ps.count {
			width = max(width, int(c))
		}
		vertices := make([]byte, 8*width*len(ps.vertices))
		for p, v := range ps.vertices {
			for q, x := range v {
				binary.LittleEndian.PutUint32(vertices[4*(p*2*width+q):], math.Float32bits(x))
			}
		}
		for _, a := range []struct {
			name, dtype string
			shape       []int
			data        []byte
		}{
			{"vertices", "<f4", []int{len(ps.vertices), 2 * width}, vertices},
			{"num_vertices", "<i4", []int{len(ps.vertices)}, u32s(ps.count)},
			{"cell_index", "<u4", []int{len(ps.vertices)}, u32s(ps.cell)},
			{"method", "<u4", []int{len(ps.vertices)}, u32s(ps.kind)},
		} {
			if err := z.WriteArray(dir+"/"+a.name, a.dtype, a.shape, a.data, 0, nil); err != nil {
				return err
			}
		}
	}
	defer os.RemoveAll(mm.dir)
	return zipFolder(mm.dir, filepath.Join(m.dir, filepath.FromSlash(name)))
}

// mosaicImage is the stain being written: each level a tile at a time where a tile's own share of
// it is whole tiles of the file's, the coarser levels gathered whole.
type mosaicImage struct {
	w      *tiff.TiledWriter
	levels int
	coarse map[int]*tiff.Plane16
	tw, th int
}

func (m *mosaic) newImage() (*mosaicImage, error) {
	w, h := m.across*m.tw, m.down*m.th
	if err := os.MkdirAll(filepath.Join(m.dir, "morphology_focus"), 0o755); err != nil {
		return nil, err
	}
	levels := stainLevels(w, h)
	tw, err := tiff.NewTiledWriter(filepath.Join(m.dir, "morphology_focus", "morphology_focus_0000.ome.tif"), w, h, tiff.WriteOptions{
		Description: stainXML(w, h, m.p.PixelUm, m.software), Software: "SituCraft simulate " + m.software, Tile: mosaicBlock, Levels: levels})
	if err != nil {
		return nil, err
	}
	return &mosaicImage{w: tw, levels: levels, coarse: map[int]*tiff.Plane16{}, tw: m.tw, th: m.th}, nil
}

// add writes tile (i, j)'s stain into every level.
func (im *mosaicImage) add(plane *tiff.Plane16, i, j int) error {
	for level := 0; level <= im.levels; level++ {
		if level > 0 {
			plane = plane.Halve()
		}
		lw, lh := im.tw>>level, im.th>>level
		if lw<<level != im.tw || lh<<level != im.th {
			return fmt.Errorf("a tile of %d × %d pixels does not halve %d times", im.tw, im.th, level)
		}
		if lw%mosaicBlock == 0 && lh%mosaicBlock == 0 {
			for ty := 0; ty < lh/mosaicBlock; ty++ {
				for tx := 0; tx < lw/mosaicBlock; tx++ {
					if err := im.w.WriteTileAt(level, i*lw/mosaicBlock+tx, j*lh/mosaicBlock+ty, plane, i*lw, j*lh); err != nil {
						return err
					}
				}
			}
			continue
		}
		c := im.coarse[level]
		if c == nil {
			w, h := im.w.Size(level)
			c = &tiff.Plane16{Width: w, Height: h, Pix: make([]uint16, w*h)}
			im.coarse[level] = c
		}
		for y := 0; y < lh; y++ {
			copy(c.Pix[(j*lh+y)*c.Width+i*lw:(j*lh+y)*c.Width+i*lw+lw], plane.Pix[y*lw:(y+1)*lw])
		}
	}
	return nil
}

func (im *mosaicImage) finish() error {
	for level, c := range im.coarse {
		for ty := 0; ty < (c.Height+mosaicBlock-1)/mosaicBlock; ty++ {
			for tx := 0; tx < (c.Width+mosaicBlock-1)/mosaicBlock; tx++ {
				if err := im.w.WriteTile(level, tx, ty, c); err != nil {
					return err
				}
			}
		}
	}
	return im.w.Close()
}

// finish closes the tables and writes what is of the whole.
func (m *mosaic) finish() error {
	for _, t := range m.tables {
		if err := t.w.Close(); err != nil {
			return err
		}
		if err := t.buf.Flush(); err != nil {
			return err
		}
		if err := t.f.Close(); err != nil {
			return err
		}
	}
	for name, mm := range m.masks {
		if err := m.finishMasks(name, mm); err != nil {
			return err
		}
	}
	if err := m.image.finish(); err != nil {
		return err
	}
	wr := m.writer()
	if err := wr.writeMatrixFiles(m.indptr, m.indices, m.data); err != nil {
		return err
	}
	run := map[string]any{}
	for k, v := range wr.run {
		run[k] = v
	}
	if err := writeTruthRun(filepath.Join(m.dir, "truth"), run, m.p, m.software); err != nil {
		return err
	}
	return wr.writeDescription()
}
