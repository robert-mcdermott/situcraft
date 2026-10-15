// Package view answers what the tissue viewer asks of a dataset: a description of what can be
// shown, transcripts by tile or by region (some genes, a quality floor), density grids by gene,
// the cell table, and one cell in full. It reads the derived-data cache (internal/cache) and the
// bundle's cells table, and encodes answers as compact little-endian columns that the page views
// as typed arrays in place (no parsing): every response starts with a four-letter magic and three
// 32-bit numbers, and every column starts on a multiple of its width.
package view

import (
	"container/list"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"sync"

	"situcraft/internal/cache"
	"situcraft/internal/xenium"
)

// Dataset is a dataset open for viewing.
type Dataset struct {
	Name, Path string
	Grid       cache.Grid
	// Entries maps entry kinds to the names of the dataset's entries.
	Entries     map[string]string
	Image       *cache.ImageEntry  // nil without an image entry
	Transcripts *cache.Transcripts // nil without a transcript index
	Geometry    *cache.Geometry    // nil without geometry
	Cells       *xenium.Cells
	Methods     *xenium.CellMethods // nil when the bundle does not record them

	tiles *pointTiles
	once  sync.Once
	desc  *Descriptor
	dErr  error

	countsOnce sync.Once
	counts     *cellCounts
	countsErr  error

	rowsOnce sync.Once
	rows     map[string]int
}

// CellRow is the row of the cell with this ID (cells.parquet's cell_id).
func (d *Dataset) CellRow(id string) (int, bool) {
	d.rowsOnce.Do(func() {
		d.rows = make(map[string]int, len(d.Cells.IDs))
		for row, cell := range d.Cells.IDs {
			d.rows[cell] = row
		}
	})
	row, ok := d.rows[id]
	return row, ok
}

// Open opens a bundle's dataset for viewing; its entries must be built (status Ready).
func Open(c *cache.Cache, b *xenium.Bundle) (*Dataset, error) {
	status, err := c.Status(b)
	if err != nil {
		return nil, err
	}
	if !status.Ready {
		return nil, ErrNotReady
	}
	d := &Dataset{Name: b.Name, Path: b.Path, Grid: *status.Grid, Entries: map[string]string{}, tiles: newPointTiles(64)}
	fail := func(err error) (*Dataset, error) {
		d.Close()
		return nil, err
	}
	for _, e := range status.Entries {
		if e.State != "ready" {
			continue
		}
		d.Entries[e.Kind] = e.Name
		dir := c.EntryDir(e.Name)
		switch e.Kind {
		case cache.KindImage:
			if d.Image, err = cache.OpenImageEntry(dir); err != nil {
				return fail(err)
			}
		case cache.KindTranscripts:
			if d.Transcripts, err = cache.OpenTranscripts(dir); err != nil {
				return fail(err)
			}
		case cache.KindGeometry:
			if d.Geometry, err = cache.OpenGeometry(dir); err != nil {
				return fail(err)
			}
		}
	}
	if d.Cells, err = b.ReadCells(); err != nil {
		return fail(err)
	}
	// The methods are a nicety: a bundle whose cells.zarr.zip cannot be read is still viewed.
	d.Methods, _ = b.ReadCellMethods(d.Cells)
	return d, nil
}

// ErrNotReady is returned for a dataset whose derived data are not all built.
var ErrNotReady = errors.New("the dataset's derived data are not built yet")

// ErrNoCell is returned for a cell row the table does not have.
var ErrNoCell = errors.New("no such cell")

// Close releases the dataset's files.
func (d *Dataset) Close() {
	if d.Image != nil {
		d.Image.Close()
	}
	if d.Transcripts != nil {
		d.Transcripts.Close()
	}
	if d.Geometry != nil {
		d.Geometry.Close()
	}
}

// --- Decoded point tiles -------------------------------------------------------------------------

// pointTiles keeps the most recently used decoded tiles of a transcript index, so that asking for
// other genes of a tile does not inflate it again.
type pointTiles struct {
	mu    sync.Mutex
	max   int
	order *list.List // of [2]int, most recent first
	tiles map[[2]int]*list.Element
}

type pointTileEntry struct {
	key  [2]int
	tile *cache.PointTile
}

func newPointTiles(max int) *pointTiles {
	return &pointTiles{max: max, order: list.New(), tiles: map[[2]int]*list.Element{}}
}

func (p *pointTiles) get(t *cache.Transcripts, tx, ty int) (*cache.PointTile, error) {
	key := [2]int{tx, ty}
	p.mu.Lock()
	if el, ok := p.tiles[key]; ok {
		p.order.MoveToFront(el)
		tile := el.Value.(pointTileEntry).tile
		p.mu.Unlock()
		return tile, nil
	}
	p.mu.Unlock()
	tile, err := t.Points(tx, ty)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.tiles[key]; !ok {
		p.tiles[key] = p.order.PushFront(pointTileEntry{key, tile})
		for p.order.Len() > p.max {
			last := p.order.Back()
			p.order.Remove(last)
			delete(p.tiles, last.Value.(pointTileEntry).key)
		}
	}
	return tile, nil
}

// --- Gene selections -----------------------------------------------------------------------------

// Genes is a selection of genes by index (nil: every gene). Indexes from the number of genes on
// are pseudo-genes (cache.PseudoAll…), which only density grids have.
type Genes []int

func (g Genes) mask(n int) ([]bool, error) {
	if g == nil {
		return nil, nil
	}
	m := make([]bool, n+cache.PseudoCount)
	for _, i := range g {
		if i < 0 || i >= len(m) {
			return nil, fmt.Errorf("no gene %d", i)
		}
		m[i] = true
	}
	return m, nil
}

// --- Points --------------------------------------------------------------------------------------

// Points are transcripts grouped by gene.
type Points struct {
	Genes  []uint16
	Starts []uint32 // the points of Genes[i] are Starts[i] to Starts[i+1]
	X, Y   []float32
	Cell   []uint32
	Row    []uint32 // the transcript's row in transcripts.parquet (not encoded)
	Raw    []uint32 // its place among its tile's points, all genes and qualities (not encoded)
	// Probability is each point's probability of its cell, when the segmentation gives one (NaN
	// for a transcript it assigned by place); encoded only when set.
	Probability []float32
}

// Len is the number of points.
func (p *Points) Len() int { return len(p.X) }

// TilePoints are the transcripts of a full-resolution tile, of the selected genes and with a
// quality of at least minQ.
func (d *Dataset) TilePoints(tx, ty int, genes Genes, minQ float32) (*Points, error) {
	if d.Transcripts == nil {
		return nil, ErrNotReady
	}
	n := len(d.Transcripts.Summary.Genes)
	mask, err := genes.mask(n)
	if err != nil {
		return nil, err
	}
	for _, g := range genes {
		if g >= n {
			return nil, fmt.Errorf("gene %d is a group of transcripts, which only density grids have", g)
		}
	}
	tile, err := d.tiles.get(d.Transcripts, tx, ty)
	if err != nil {
		return nil, err
	}
	out := &Points{Starts: []uint32{0}}
	for k, gene := range tile.Genes {
		if mask != nil && !mask[gene] {
			continue
		}
		before := len(out.X)
		for i := tile.Starts[k]; i < tile.Starts[k+1]; i++ {
			if tile.QV[i] >= minQ {
				out.X, out.Y = append(out.X, tile.X[i]), append(out.Y, tile.Y[i])
				out.Cell, out.Row, out.Raw = append(out.Cell, tile.Cell[i]), append(out.Row, tile.Row[i]), append(out.Raw, i)
			}
		}
		if len(out.X) > before {
			out.Genes = append(out.Genes, gene)
			out.Starts = append(out.Starts, uint32(len(out.X)))
		}
	}
	return out, nil
}

// Rect is a region in µm: x0 ≤ x < x1, y0 ≤ y < y1.
type Rect struct{ X0, Y0, X1, Y1 float64 }

func (r Rect) valid() bool {
	return !math.IsNaN(r.X0+r.Y0+r.X1+r.Y1) && r.X1 >= r.X0 && r.Y1 >= r.Y0
}

// tilesOf is the range of a level's tiles holding the region's points (edge tiles hold points
// just past the edges too).
func (d *Dataset) tilesOf(level int, r Rect) (tx0, ty0, tx1, ty1 int) {
	tx0, ty0 = d.Grid.TileOf(level, r.X0, r.Y0)
	tx1, ty1 = d.Grid.TileOf(level, r.X1, r.Y1)
	return
}

// maxQueryTiles bounds a region query (a whole slide's level 0 is thousands of tiles).
const maxQueryTiles = 4096

// RegionPoints are the transcripts inside a region, of the selected genes and with a quality of
// at least minQ, grouped by gene and in transcripts.parquet's order within a gene.
func (d *Dataset) RegionPoints(r Rect, genes Genes, minQ float32) (*Points, error) {
	if d.Transcripts == nil {
		return nil, ErrNotReady
	}
	if !r.valid() {
		return nil, errors.New("invalid region")
	}
	tx0, ty0, tx1, ty1 := d.tilesOf(0, r)
	if (tx1-tx0+1)*(ty1-ty0+1) > maxQueryTiles {
		return nil, fmt.Errorf("the region spans more than %d tiles", maxQueryTiles)
	}
	type pt struct {
		row  uint32
		x, y float32
		cell uint32
	}
	byGene := map[uint16][]pt{}
	for ty := ty0; ty <= ty1; ty++ {
		for tx := tx0; tx <= tx1; tx++ {
			tile, err := d.TilePoints(tx, ty, genes, minQ)
			if err != nil {
				return nil, err
			}
			for k, gene := range tile.Genes {
				for i := tile.Starts[k]; i < tile.Starts[k+1]; i++ {
					x, y := float64(tile.X[i]), float64(tile.Y[i])
					if x >= r.X0 && x < r.X1 && y >= r.Y0 && y < r.Y1 {
						byGene[gene] = append(byGene[gene], pt{tile.Row[i], tile.X[i], tile.Y[i], tile.Cell[i]})
					}
				}
			}
		}
	}
	order := make([]uint16, 0, len(byGene))
	for g := range byGene {
		order = append(order, g)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := &Points{Starts: []uint32{0}}
	for _, g := range order {
		pts := byGene[g]
		sort.Slice(pts, func(i, j int) bool { return pts[i].row < pts[j].row })
		for _, p := range pts {
			out.X, out.Y, out.Cell, out.Row = append(out.X, p.x), append(out.Y, p.y), append(out.Cell, p.cell), append(out.Row, p.row)
		}
		out.Genes = append(out.Genes, g)
		out.Starts = append(out.Starts, uint32(len(out.X)))
	}
	return out, nil
}

// Encode writes the points ("SCVP"; points, genes present, 1 with probabilities else 0):
//
//	starts u32[genes+1] | x f32[n] | y f32[n] | cell u32[n] | genes u16[genes] | padding
//	| probability f32[n] (with probabilities)
func (p *Points) Encode() []byte {
	var w writer
	extra := uint32(0)
	if p.Probability != nil {
		extra = 1
	}
	w.header("SCVP", uint32(len(p.X)), uint32(len(p.Genes)), extra)
	w.u32(p.Starts)
	w.f32(p.X)
	w.f32(p.Y)
	w.u32(p.Cell)
	w.u16(p.Genes)
	w.pad()
	if p.Probability != nil {
		w.f32(p.Probability)
	}
	return w.buf
}

// --- Density -------------------------------------------------------------------------------------

// Density is a tile's density grids for some genes.
type Density struct {
	Bins   int
	Genes  []uint16
	Starts []uint32 // the bins of Genes[i] are entries Starts[i] to Starts[i+1]
	Bin    []uint16 // by*Bins + bx
	Count  []uint32
}

// TileDensity is a tile's density grids for the selected genes and pseudo-genes (nil: every
// transcript together only), counting all transcripts or (q20) those of quality 20 or more.
func (d *Dataset) TileDensity(level, tx, ty int, genes Genes, q20 bool) (*Density, error) {
	if d.Transcripts == nil {
		return nil, ErrNotReady
	}
	n := len(d.Transcripts.Summary.Genes)
	if genes == nil {
		genes = Genes{n}
	}
	mask, err := genes.mask(n)
	if err != nil {
		return nil, err
	}
	tile, err := d.Transcripts.Density(level, tx, ty)
	if err != nil {
		return nil, err
	}
	out := &Density{Bins: d.Transcripts.Params.DensityBins, Starts: []uint32{0}}
	for k, gene := range tile.Genes {
		if !mask[gene] {
			continue
		}
		before := len(out.Bin)
		for i := tile.Starts[k]; i < tile.Starts[k+1]; i++ {
			c := tile.N[i]
			if q20 {
				c = tile.Q20[i]
			}
			if c > 0 {
				out.Bin, out.Count = append(out.Bin, tile.Bins[i]), append(out.Count, c)
			}
		}
		if len(out.Bin) > before {
			out.Genes = append(out.Genes, gene)
			out.Starts = append(out.Starts, uint32(len(out.Bin)))
		}
	}
	return out, nil
}

// Encode writes the grids ("SCVD"; bins per side, genes present, entries):
//
//	starts u32[genes+1] | count u32[entries] | genes u16[genes] | padding | bin u16[entries] | padding
func (g *Density) Encode() []byte {
	var w writer
	w.header("SCVD", uint32(g.Bins), uint32(len(g.Genes)), uint32(len(g.Bin)))
	w.u32(g.Starts)
	w.u32(g.Count)
	w.u16(g.Genes)
	w.pad()
	w.u16(g.Bin)
	w.pad()
	return w.buf
}

// --- Cells ---------------------------------------------------------------------------------------

// CellColumns are what the cells table holds of each cell, in order, beyond its centroid (QC
// attributes for maps, plots and filters).
var CellColumns = []string{"area", "nucleusArea", "transcripts", "total", "controlProbes", "controlCodewords", "genes", "nuclear"}

// cellCounts are per-cell counts from the transcript index: genes detected (with a Q20
// transcript) and Q20 gene transcripts overlapping the nucleus.
type cellCounts struct {
	genes, nuclear []uint32
}

// countCells counts, for every cell, its genes detected and its nuclear transcripts, from the
// transcript index (computed once), holding no more than a row of tiles' transcripts whatever the
// tissue's size. A first pass finds the last row of tiles each cell has a transcript in; the
// second takes the rows in turn, each cell's (cell, gene) pairs made unique with those carried from
// the rows before, and counts a cell's genes at its last row.
func (d *Dataset) countCells() (*cellCounts, error) {
	d.countsOnce.Do(func() {
		n := len(d.Cells.IDs)
		out := &cellCounts{genes: make([]uint32, n), nuclear: make([]uint32, n)}
		t := d.Transcripts
		if t == nil {
			d.counts = out
			return
		}
		// each calls f with every Q20 gene transcript of a row of tiles assigned to a cell.
		each := func(ty int, f func(c uint32, gene uint16, nuclear bool)) error {
			for tx := 0; tx < d.Grid.Across(0); tx++ {
				tile, err := t.Points(tx, ty)
				if err != nil {
					return err
				}
				for k, gene := range tile.Genes {
					if t.Summary.Genes[gene].Kind != xenium.KindGene {
						continue
					}
					for i := tile.Starts[k]; i < tile.Starts[k+1]; i++ {
						if c := tile.Cell[i]; c != cache.NoCell && int(c) < n && tile.QV[i] >= 20 {
							f(c, gene, tile.Flags[i]&1 != 0)
						}
					}
				}
			}
			return nil
		}
		down := d.Grid.Down(0)
		last := make([]int32, n)
		for ty := 0; ty < down; ty++ {
			if err := each(ty, func(c uint32, _ uint16, _ bool) { last[c] = int32(ty) }); err != nil {
				d.countsErr = err
				return
			}
		}
		var pairs, carried []uint64
		for ty := 0; ty < down; ty++ {
			pairs = append(pairs[:0], carried...)
			err := each(ty, func(c uint32, gene uint16, nuclear bool) {
				pairs = append(pairs, uint64(c)<<16|uint64(gene))
				if nuclear {
					out.nuclear[c]++
				}
			})
			if err != nil {
				d.countsErr = err
				return
			}
			slices.Sort(pairs)
			carried = carried[:0]
			for i, p := range pairs {
				if i > 0 && p == pairs[i-1] {
					continue
				}
				if c := p >> 16; last[c] > int32(ty) {
					carried = append(carried, p)
				} else {
					out.genes[c]++
				}
			}
		}
		d.counts = out
	})
	return d.counts, d.countsErr
}

// SegmentationTable is any segmentation's cells, for the Viewer's fills when it draws one other
// than the instrument's (the dataset's own are CellTable's).
type SegmentationTable struct {
	X, Y, Area, NucleusArea                      []float32 // µm, µm²
	Transcripts, Assigned, Genes                 []uint32  // gene transcripts of Q20, every one, genes detected
	Instrument                                   []uint32  // the instrument's row of its nucleus's cell (NoCell none)
	Certainty, Probability                       []float32 // boundary certainty, transcripts' mean probability (NaN unknown)
	Limitations                                  []uint8   // 1 few transcripts, 2 uncertain boundary, 4 no nucleus
	HasLimitations, HasCertainty, HasProbability bool
}

// Encode writes the table ("SCVS"; cells, what it holds, 11 columns), "what it holds" being 1 with
// limitations, 2 with boundary certainties, 4 with probabilities:
//
//	x f32[n] | y f32[n] | area f32[n] | nucleus area f32[n] | transcripts u32[n] |
//	assigned u32[n] | genes u32[n] | instrument row u32[n] | certainty f32[n] |
//	probability f32[n] | limitations u8[n] | padding
func (t *SegmentationTable) Encode() []byte {
	holds := uint32(0)
	for bit, on := range []bool{t.HasLimitations, t.HasCertainty, t.HasProbability} {
		if on {
			holds |= 1 << bit
		}
	}
	var w writer
	w.header("SCVS", uint32(len(t.X)), holds, 11)
	for _, c := range [][]float32{t.X, t.Y, t.Area, t.NucleusArea} {
		w.f32(c)
	}
	for _, c := range [][]uint32{t.Transcripts, t.Assigned, t.Genes, t.Instrument} {
		w.u32(c)
	}
	w.f32(t.Certainty)
	w.f32(t.Probability)
	w.u8(t.Limitations)
	w.pad()
	return w.buf
}

// CellTable encodes the cells ("SCVC"; cells, segmentation methods, columns), for coloring cells
// by an attribute, plotting and filtering them, and finding them:
//
//	x f32[n] | y f32[n] | area f32[n] | nucleus area f32[n] (µm²) |
//	transcripts u32[n] (Q20 gene transcripts, cells.parquet's transcript_counts) |
//	total u32[n] (all its Q20 transcripts, total_counts) | control probes u32[n] |
//	control codewords u32[n] | genes u32[n] (genes detected) |
//	nuclear u32[n] (Q20 gene transcripts overlapping the nucleus) |
//	method u8[n] (255: unknown) | padding
//
// Areas a bundle does not record are NaN; counts it does not record are 0.
func (d *Dataset) CellTable() ([]byte, error) {
	counts, err := d.countCells()
	if err != nil {
		return nil, err
	}
	c := d.Cells
	n := len(c.IDs)
	methods := 0
	if d.Methods != nil {
		methods = len(d.Methods.Methods)
	}
	var w writer
	w.header("SCVC", uint32(n), uint32(methods), uint32(len(CellColumns)))
	floats := func(v []float64) []float32 {
		out := make([]float32, n)
		for i := range out {
			out[i] = float32(math.NaN())
			if i < len(v) {
				out[i] = float32(v[i])
			}
		}
		return out
	}
	ints := func(v []int64) []uint32 {
		out := make([]uint32, n)
		for i := range out {
			if i < len(v) && v[i] > 0 {
				out[i] = uint32(min(v[i], math.MaxUint32))
			}
		}
		return out
	}
	w.f32(floats(c.X))
	w.f32(floats(c.Y))
	w.f32(floats(c.CellArea))
	w.f32(floats(c.NucleusArea))
	w.u32(ints(c.TranscriptCounts))
	w.u32(ints(c.TotalCounts))
	w.u32(ints(c.ControlProbeCounts))
	w.u32(ints(c.ControlCodewordCounts))
	w.u32(counts.genes)
	w.u32(counts.nuclear)
	method := make([]uint8, n)
	for i := range method {
		method[i] = xenium.NoMethod
		if d.Methods != nil {
			method[i] = d.Methods.Cell[i]
		}
	}
	w.u8(method)
	w.pad()
	return w.buf, nil
}

// --- Encoding ------------------------------------------------------------------------------------

type writer struct{ buf []byte }

func (w *writer) header(magic string, a, b, c uint32) {
	w.buf = append(w.buf, magic...)
	w.u32([]uint32{a, b, c})
}

func (w *writer) u32(v []uint32) {
	for _, x := range v {
		w.buf = append(w.buf, byte(x), byte(x>>8), byte(x>>16), byte(x>>24))
	}
}

func (w *writer) f32(v []float32) {
	for _, f := range v {
		x := math.Float32bits(f)
		w.buf = append(w.buf, byte(x), byte(x>>8), byte(x>>16), byte(x>>24))
	}
}

func (w *writer) u16(v []uint16) {
	for _, x := range v {
		w.buf = append(w.buf, byte(x), byte(x>>8))
	}
}

func (w *writer) u8(v []uint8) { w.buf = append(w.buf, v...) }

func (w *writer) pad() {
	for len(w.buf)%4 != 0 {
		w.buf = append(w.buf, 0)
	}
}
