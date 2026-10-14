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

// Genes is a selection of genes by index (nil: every gene). The index equal to the number of
// genes is every gene together, which only density grids have.
type Genes []int

func (g Genes) mask(n int) ([]bool, error) {
	if g == nil {
		return nil, nil
	}
	m := make([]bool, n+1)
	for _, i := range g {
		if i < 0 || i > n {
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
}

// Len is the number of points.
func (p *Points) Len() int { return len(p.X) }

// TilePoints are the transcripts of a full-resolution tile, of the selected genes and with a
// quality of at least minQ.
func (d *Dataset) TilePoints(tx, ty int, genes Genes, minQ float32) (*Points, error) {
	if d.Transcripts == nil {
		return nil, ErrNotReady
	}
	mask, err := genes.mask(len(d.Transcripts.Summary.Genes))
	if err != nil {
		return nil, err
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
				out.Cell, out.Row = append(out.Cell, tile.Cell[i]), append(out.Row, tile.Row[i])
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

// Encode writes the points ("SCVP"; points, genes present, 0):
//
//	starts u32[genes+1] | x f32[n] | y f32[n] | cell u32[n] | genes u16[genes] | padding
func (p *Points) Encode() []byte {
	var w writer
	w.header("SCVP", uint32(len(p.X)), uint32(len(p.Genes)), 0)
	w.u32(p.Starts)
	w.f32(p.X)
	w.f32(p.Y)
	w.u32(p.Cell)
	w.u16(p.Genes)
	w.pad()
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

// TileDensity is a tile's density grids for the selected genes (nil: every gene together only),
// counting all transcripts or (q20) those of quality 20 or more.
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

// CellTable encodes the cells ("SCVC"; cells, segmentation methods, 0), for coloring cells by an
// attribute and finding them:
//
//	x f32[n] | y f32[n] | area f32[n] | nucleus area f32[n] | gene transcripts u32[n] |
//	all transcripts u32[n] | method u8[n] (255: unknown) | padding
//
// Areas a bundle does not record are NaN; counts it does not record are 0.
func (d *Dataset) CellTable() []byte {
	c := d.Cells
	n := len(c.IDs)
	methods := 0
	if d.Methods != nil {
		methods = len(d.Methods.Methods)
	}
	var w writer
	w.header("SCVC", uint32(n), uint32(methods), 0)
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
	counts := func(v []int64) []uint32 {
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
	w.u32(counts(c.TranscriptCounts))
	w.u32(counts(c.TotalCounts))
	method := make([]uint8, n)
	for i := range method {
		method[i] = xenium.NoMethod
		if d.Methods != nil {
			method[i] = d.Methods.Cell[i]
		}
	}
	w.u8(method)
	w.pad()
	return w.buf
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
