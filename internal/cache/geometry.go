package cache

// Geometry tiles: cell and nucleus boundaries, one file per set and level ("cells-<level>.bin",
// "nuclei-<level>.bin", magic SCGT). Each polygon belongs to the tile of the level holding the
// centre of its bounding box, so it is stored once per level; a viewport query widens itself by
// the summary's largest polygon extent to catch polygons centred just outside. Level 0 holds the
// bundle's vertices exactly. Level L above it holds them simplified (Douglas–Peucker; Douglas &
// Peucker 1973) to within half a pixel of that level, keeping at least three vertices, so that
// a view of many cells draws a fraction of the vertices.
//
// Fields: level, tiles across, tiles down, polygons, vertices. Table entries: A is the tile's
// number of polygons, B its number of vertices. Block (deflated):
//
//	cell u32[A]      row of the polygon's cell in cells.parquet (0xFFFFFFFF: not in it)
//	starts u32[A+1]  vertices of polygon i are starts[i] to starts[i+1]
//	xy f32[2B]       vertices, x and y interleaved (µm)

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"

	"situcraft/internal/xenium"
)

const geometryMagic = "SCGT"

// GeometrySets are the polygon sets, in file-name order.
var GeometrySets = []string{"cells", "nuclei"}

// GeometryInput is what geometry tiles are built from (a set may be nil).
type GeometryInput struct {
	Cells, Nuclei *xenium.Polygons
	CellIndex     map[string]int32
}

// GeometryParams are the geometry's parameters (part of its key).
type GeometryParams struct {
	Grid Grid `json:"grid"`
	// Tolerance is the simplification tolerance at level 1 in pixels of that level; it doubles in
	// µm with each level.
	Tolerance float64 `json:"tolerance"`
}

// NewGeometryParams is half a pixel of each level.
func NewGeometryParams(grid Grid) GeometryParams { return GeometryParams{Grid: grid, Tolerance: 0.5} }

// epsilon is the simplification tolerance at a level in µm (0 at level 0).
func (p GeometryParams) epsilon(level int) float64 {
	if level == 0 {
		return 0
	}
	return p.Tolerance * p.Grid.PixelSize * float64(int(1)<<level)
}

// GeometrySummary is a geometry entry's summary.
type GeometrySummary struct {
	Sets []SetSummary `json:"sets"`
}

// SetSummary describes one polygon set.
type SetSummary struct {
	Name     string `json:"name"` // "cells" or "nuclei"
	Polygons int    `json:"polygons"`
	// UnknownCells counts polygons whose cell cells.parquet does not have.
	UnknownCells int `json:"unknownCells"`
	// MaxExtent is the largest side of a polygon's bounding box (µm).
	MaxExtent float64         `json:"maxExtent"`
	Levels    []GeometryLevel `json:"levels"`
}

// GeometryLevel describes one level of a set.
type GeometryLevel struct {
	Vertices int     `json:"vertices"`
	Epsilon  float64 `json:"epsilon"` // µm
}

func geometryFile(set string, level int) string { return set + "-" + strconv.Itoa(level) + ".bin" }

// buildGeometry writes every level of each polygon set the input has.
func buildGeometry(ctx context.Context, b *building, p GeometryParams, in GeometryInput, progress func(float64)) (*GeometrySummary, error) {
	if !p.Grid.valid() || !(p.Tolerance >= 0) {
		return nil, fmt.Errorf("invalid geometry parameters %+v", p)
	}
	sum := &GeometrySummary{}
	sets := []*xenium.Polygons{in.Cells, in.Nuclei}
	steps, step := 0, 0
	for _, polys := range sets {
		if polys != nil {
			steps += p.Grid.Levels
		}
	}
	for s, polys := range sets {
		if polys == nil {
			continue
		}
		if len(polys.Offsets) != len(polys.CellIDs)+1 || len(polys.X) != len(polys.Y) {
			return nil, fmt.Errorf("%s: malformed polygons", GeometrySets[s])
		}
		set := SetSummary{Name: GeometrySets[s], Polygons: len(polys.CellIDs)}
		cells := make([]uint32, len(polys.CellIDs))
		cx, cy := make([]float64, len(cells)), make([]float64, len(cells))
		for i, id := range polys.CellIDs {
			cells[i] = NoCell
			if c, ok := in.CellIndex[id]; ok {
				cells[i] = uint32(c)
			} else {
				set.UnknownCells++
			}
			x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
			for v := polys.Offsets[i]; v < polys.Offsets[i+1]; v++ {
				x, y := float64(polys.X[v]), float64(polys.Y[v])
				x0, y0, x1, y1 = math.Min(x0, x), math.Min(y0, y), math.Max(x1, x), math.Max(y1, y)
			}
			if x1 >= x0 && y1 >= y0 {
				cx[i], cy[i] = (x0+x1)/2, (y0+y1)/2
				if e := math.Max(x1-x0, y1-y0); e > set.MaxExtent {
					set.MaxExtent = e
				}
			}
		}
		for l := 0; l < p.Grid.Levels; l++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			name := geometryFile(set.Name, l)
			eps := p.epsilon(l)
			// The level is simplified even when its file is finished (by a build that was then
			// interrupted): its vertex count is part of the summary, and simplifying is fast.
			finished := b.done(name)
			tiles := make([][]int, p.Grid.Across(l)*p.Grid.Down(l))
			for i := range cells {
				tx, ty := p.Grid.TileOf(l, cx[i], cy[i])
				tiles[ty*p.Grid.Across(l)+tx] = append(tiles[ty*p.Grid.Across(l)+tx], i)
			}
			blocks := make([][]byte, len(tiles))
			counts := make([][2]uint32, len(tiles))
			vertices := 0
			for t, members := range tiles {
				if len(members) == 0 {
					continue
				}
				var c columns
				starts := []uint32{0}
				var xy []float32
				ids := make([]uint32, len(members))
				for k, i := range members {
					ids[k] = cells[i]
					xs, ys := polys.X[polys.Offsets[i]:polys.Offsets[i+1]], polys.Y[polys.Offsets[i]:polys.Offsets[i+1]]
					if eps > 0 {
						xs, ys = simplifyRing(xs, ys, eps)
					}
					for v := range xs {
						xy = append(xy, xs[v], ys[v])
					}
					starts = append(starts, uint32(len(xy)/2))
				}
				vertices += len(xy) / 2
				counts[t] = [2]uint32{uint32(len(members)), uint32(len(xy) / 2)}
				if finished {
					continue
				}
				c.u32(ids)
				c.u32(starts)
				c.f32(xy)
				block, err := deflate(c.buf)
				if err != nil {
					return nil, err
				}
				blocks[t] = block
			}
			set.Levels = append(set.Levels, GeometryLevel{Vertices: vertices, Epsilon: eps})
			if !finished {
				if err := writeGeometryLevel(b, name, l, p.Grid, len(cells), vertices, blocks, counts); err != nil {
					return nil, fmt.Errorf("%s, level %d: %w", set.Name, l, err)
				}
			}
			step++
			progress(float64(step) / float64(steps))
		}
		sum.Sets = append(sum.Sets, set)
	}
	return sum, nil
}

func writeGeometryLevel(b *building, name string, level int, grid Grid, polygons, vertices int, blocks [][]byte, counts [][2]uint32) error {
	f, err := b.create(name)
	if err != nil {
		return err
	}
	w, err := newTableWriter(f, geometryMagic, [6]uint32{uint32(level), uint32(grid.Across(level)), uint32(grid.Down(level)), uint32(polygons), uint32(vertices)}, len(blocks))
	if err != nil {
		f.Close()
		return err
	}
	for t, block := range blocks {
		if err := w.block(t, block, counts[t][0], counts[t][1]); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.finish(); err != nil {
		f.Close()
		return err
	}
	return b.commitFile(f, name)
}

// simplifyRing simplifies a closed ring (its last vertex repeating its first, or not) with the
// Douglas–Peucker algorithm: every vertex removed lies within eps of the chord that replaced
// it. The ring is split at its first vertex and the vertex farthest from it, and at least three
// distinct vertices are kept. A closing vertex is kept if the ring had one.
func simplifyRing(xs, ys []float32, eps float64) ([]float32, []float32) {
	n := len(xs)
	closed := n >= 2 && xs[0] == xs[n-1] && ys[0] == ys[n-1]
	m := n
	if closed {
		m--
	}
	if m <= 3 || eps <= 0 {
		return xs, ys
	}
	keep := make([]bool, m)
	far, best := 0, -1.0
	for i := 1; i < m; i++ {
		if d := math.Hypot(float64(xs[i]-xs[0]), float64(ys[i]-ys[0])); d > best {
			far, best = i, d
		}
	}
	keep[0], keep[far] = true, true
	at := func(i int) (float64, float64) { i %= m; return float64(xs[i]), float64(ys[i]) }
	// Spans of the ring as index ranges, the end wrapping to vertex 0.
	type span struct{ a, b int }
	refine := func(stack []span) {
		for len(stack) > 0 {
			s := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if s.b-s.a < 2 {
				continue
			}
			ax, ay := at(s.a)
			bx, by := at(s.b)
			worst, dmax := -1, eps
			for i := s.a + 1; i < s.b; i++ {
				px, py := at(i)
				if d := segmentDistance(px, py, ax, ay, bx, by); d > dmax {
					worst, dmax = i, d
				}
			}
			if worst >= 0 {
				keep[worst] = true
				stack = append(stack, span{s.a, worst}, span{worst, s.b})
			}
		}
	}
	refine([]span{{0, far}, {far, m}})
	kept := 0
	for _, k := range keep {
		if k {
			kept++
		}
	}
	if kept < 3 {
		// The ring lies within eps of the chord: keep the vertex farthest from it as well, and
		// refine its half of the ring against the two new edges.
		ax, ay := at(0)
		bx, by := at(far)
		third, dmax := -1, -1.0
		for i := 1; i < m; i++ {
			if i == far {
				continue
			}
			px, py := at(i)
			if d := segmentDistance(px, py, ax, ay, bx, by); d > dmax {
				third, dmax = i, d
			}
		}
		keep[third] = true
		if third < far {
			refine([]span{{0, third}, {third, far}})
		} else {
			refine([]span{{far, third}, {third, m}})
		}
	}
	var ox, oy []float32
	for i, k := range keep {
		if k {
			ox, oy = append(ox, xs[i]), append(oy, ys[i])
		}
	}
	if closed {
		ox, oy = append(ox, xs[0]), append(oy, ys[0])
	}
	return ox, oy
}

// segmentDistance is the distance from (px, py) to the segment from (ax, ay) to (bx, by).
func segmentDistance(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/l))
	}
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

// PolygonTile is one tile's polygons.
type PolygonTile struct {
	Cells  []uint32
	Starts []uint32 // vertices of polygon i are Starts[i] to Starts[i+1]
	XY     []float32
}

func decodePolygons(block []byte, e tableEntry) (*PolygonTile, error) {
	p, v := int(e.A), int(e.B)
	if p == 0 {
		if v != 0 || block != nil {
			return nil, errDamaged
		}
		return &PolygonTile{Starts: []uint32{0}}, nil
	}
	raw, err := inflate(block, 8*int64(p)+4+8*int64(v))
	if err != nil {
		return nil, err
	}
	r := &reader{buf: raw}
	t := &PolygonTile{Cells: r.u32(p), Starts: r.u32(p + 1), XY: r.f32(2 * v)}
	if err := r.end(); err != nil {
		return nil, err
	}
	if offsets(t.Starts, v) != nil {
		return nil, errDamaged
	}
	return t, nil
}

func openGeometryFile(path string) (*tableFile, error) {
	return openTableFile(path, geometryMagic, func(f [6]uint32) (int, error) {
		if f[1] == 0 || f[2] == 0 || uint64(f[1])*uint64(f[2]) > 1<<24 {
			return 0, errDamaged
		}
		return int(f[1]) * int(f[2]), nil
	})
}

// Geometry is an open geometry entry.
type Geometry struct {
	Summary GeometrySummary
	Params  GeometryParams
	files   map[string]*tableFile
}

// OpenGeometry opens a complete geometry entry.
func OpenGeometry(dir string) (*Geometry, error) {
	m, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	g := &Geometry{files: map[string]*tableFile{}}
	if err := json.Unmarshal(m.Summary, &g.Summary); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(m.Params, &g.Params); err != nil {
		return nil, err
	}
	if !g.Params.Grid.valid() {
		return nil, errDamaged
	}
	for _, set := range g.Summary.Sets {
		if len(set.Levels) != g.Params.Grid.Levels {
			g.Close()
			return nil, errDamaged
		}
		for l := range set.Levels {
			f, err := openGeometryFile(filepath.Join(dir, geometryFile(set.Name, l)))
			if err != nil {
				g.Close()
				return nil, err
			}
			g.files[geometryFile(set.Name, l)] = f
			if int(f.Fields[0]) != l || int(f.Fields[1]) != g.Params.Grid.Across(l) || int(f.Fields[2]) != g.Params.Grid.Down(l) || int(f.Fields[3]) != set.Polygons {
				g.Close()
				return nil, errDamaged
			}
		}
	}
	return g, nil
}

// Tile reads a set's polygons in tile (tx, ty) of a level.
func (g *Geometry) Tile(set string, level, tx, ty int) (*PolygonTile, error) {
	f, ok := g.files[geometryFile(set, level)]
	if !ok {
		return nil, fmt.Errorf("no %s at level %d", set, level)
	}
	if tx < 0 || ty < 0 || tx >= int(f.Fields[1]) || ty >= int(f.Fields[2]) {
		return nil, fmt.Errorf("no tile (%d, %d) at level %d", tx, ty, level)
	}
	block, e, err := f.block(ty*int(f.Fields[1]) + tx)
	if err != nil {
		return nil, err
	}
	return decodePolygons(block, e)
}

// Close releases the entry's files.
func (g *Geometry) Close() error {
	for _, f := range g.files {
		f.Close()
	}
	return nil
}
