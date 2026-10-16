package imports

// Polygons as other tools write them, read into µm: GeoJSON (RFC 7946: Polygon and MultiPolygon
// geometries, in a FeatureCollection, a Feature, or a GeometryCollection whose geometries carry
// their cell, as Baysor writes) and well-known binary (WKB, as GeoParquet stores geometries:
// Polygon and MultiPolygon, either byte order, with Z or M dimensions ignored). Each is drawn into
// the dataset's pixel grid: a pixel belongs to a cell's polygon when its centre lies inside it
// (even–odd, so holes are left out).

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"

	"situcraft/internal/segment"
)

type point struct{ X, Y float64 }

// polygon is an exterior ring and its holes (rings closed implicitly or explicitly).
type polygon [][]point

// area of a ring (shoelace), unsigned.
func ringArea(r []point) float64 {
	var s float64
	for i, j := 0, len(r)-1; i < len(r); j, i = i, i+1 {
		s += mul(r[j].X, r[i].Y) - mul(r[i].X, r[j].Y)
	}
	return math.Abs(s) / 2
}

// polygonArea is the exterior's area less the holes'.
func polygonArea(p polygon) float64 {
	if len(p) == 0 {
		return 0
	}
	a := ringArea(p[0])
	for _, h := range p[1:] {
		a -= ringArea(h)
	}
	return a
}

// --- GeoJSON ------------------------------------------------------------------------------------

type geoGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
	Geometries  []geoGeometry   `json:"geometries"`
	// Baysor writes each polygon's cell beside its coordinates.
	Cell json.RawMessage `json:"cell"`
}

type geoFeature struct {
	Type       string                     `json:"type"`
	ID         json.RawMessage            `json:"id"`
	Geometry   *geoGeometry               `json:"geometry"`
	Properties map[string]json.RawMessage `json:"properties"`
}

type geoDocument struct {
	Type        string                     `json:"type"`
	Features    []geoFeature               `json:"features"`
	Geometries  []geoGeometry              `json:"geometries"`
	Geometry    *geoGeometry               `json:"geometry"`
	Coordinates json.RawMessage            `json:"coordinates"`
	ID          json.RawMessage            `json:"id"`
	Properties  map[string]json.RawMessage `json:"properties"`
	Cell        json.RawMessage            `json:"cell"`
}

// shape is one cell's polygons, as a file gives them.
type shape struct {
	id       string
	polygons []polygon
}

// idProperties are the properties tried, in order, for a feature's cell when none is named.
var idProperties = []string{"cell_id", "cell", "id", "cell_ID", "CellID", "name", "label"}

// readGeoJSON reads the cells' polygons; property names the cell (tried in idProperties' order, then
// the feature's id, when empty). It answers the property used.
func readGeoJSON(raw []byte, property string) ([]shape, string, error) {
	var doc geoDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, "", fmt.Errorf("not GeoJSON: %w", err)
	}
	var features []geoFeature
	switch doc.Type {
	case "FeatureCollection":
		features = doc.Features
	case "Feature":
		features = []geoFeature{{Type: "Feature", ID: doc.ID, Geometry: doc.Geometry, Properties: doc.Properties}}
	case "GeometryCollection":
		// Baysor: {"type": "GeometryCollection", "geometries": [{"type": "Polygon", "coordinates": …, "cell": 7}, …]}.
		for i := range doc.Geometries {
			g := doc.Geometries[i]
			features = append(features, geoFeature{Type: "Feature", Geometry: &g, Properties: map[string]json.RawMessage{"cell": g.Cell}})
		}
	case "Polygon", "MultiPolygon":
		g := geoGeometry{Type: doc.Type, Coordinates: doc.Coordinates}
		features = []geoFeature{{Type: "Feature", Geometry: &g, Properties: map[string]json.RawMessage{"cell": doc.Cell}}}
	default:
		return nil, "", fmt.Errorf("GeoJSON of type %q holds no cells", doc.Type)
	}
	if property == "" {
		property = chooseProperty(features)
	}
	var out []shape
	for i, f := range features {
		if f.Geometry == nil {
			continue
		}
		var id string
		if property == "(feature id)" {
			id = jsonText(f.ID)
		} else {
			id = jsonText(f.Properties[property])
		}
		if id == "" {
			return nil, "", fmt.Errorf("feature %d has no %s", i, property)
		}
		polys, err := geoPolygons(*f.Geometry)
		if err != nil {
			return nil, "", fmt.Errorf("feature %d (%s): %w", i, id, err)
		}
		out = append(out, shape{id: id, polygons: polys})
	}
	return out, property, nil
}

// chooseProperty is the first of idProperties every feature has, else the features' ids.
func chooseProperty(features []geoFeature) string {
	for _, p := range idProperties {
		all := len(features) > 0
		for _, f := range features {
			if jsonText(f.Properties[p]) == "" {
				all = false
				break
			}
		}
		if all {
			return p
		}
	}
	return "(feature id)"
}

// jsonText is a JSON string or number as text ("" for null or absent).
func jsonText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		if f, err := n.Float64(); err == nil && f == math.Trunc(f) && math.Abs(f) < 1<<53 {
			return strconv.FormatInt(int64(f), 10)
		}
		return n.String()
	}
	return ""
}

func geoPolygons(g geoGeometry) ([]polygon, error) {
	switch g.Type {
	case "Polygon":
		var rings [][][]float64
		if err := json.Unmarshal(g.Coordinates, &rings); err != nil {
			return nil, fmt.Errorf("polygon: %w", err)
		}
		p, err := toPolygon(rings)
		if err != nil {
			return nil, err
		}
		return []polygon{p}, nil
	case "MultiPolygon":
		var polys [][][][]float64
		if err := json.Unmarshal(g.Coordinates, &polys); err != nil {
			return nil, fmt.Errorf("multipolygon: %w", err)
		}
		var out []polygon
		for _, rings := range polys {
			p, err := toPolygon(rings)
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, nil
	case "GeometryCollection":
		var out []polygon
		for _, inner := range g.Geometries {
			ps, err := geoPolygons(inner)
			if err != nil {
				return nil, err
			}
			out = append(out, ps...)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("a %s is not a cell's outline (polygons are)", g.Type)
	}
}

func toPolygon(rings [][][]float64) (polygon, error) {
	var p polygon
	for _, r := range rings {
		ring := make([]point, 0, len(r))
		for _, c := range r {
			if len(c) < 2 || math.IsNaN(c[0]) || math.IsNaN(c[1]) || math.IsInf(c[0], 0) || math.IsInf(c[1], 0) {
				return nil, errors.New("a vertex is not a pair of finite numbers")
			}
			ring = append(ring, point{c[0], c[1]})
		}
		if len(ring) < 3 {
			return nil, errors.New("a ring has fewer than three vertices")
		}
		p = append(p, ring)
	}
	if len(p) == 0 {
		return nil, errors.New("a polygon without rings")
	}
	return p, nil
}

// --- WKB ----------------------------------------------------------------------------------------

// readWKB reads a Polygon or MultiPolygon (ISO or EWKB-style dimension flags; Z and M skipped).
func readWKB(b []byte) ([]polygon, error) {
	r := &wkbReader{b: b}
	ps := r.geometry(0)
	if r.err != nil {
		return nil, r.err
	}
	return ps, nil
}

type wkbReader struct {
	b   []byte
	err error
}

func (r *wkbReader) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf("WKB: "+format, args...)
	}
}

func (r *wkbReader) take(n int) []byte {
	if r.err != nil || n < 0 || len(r.b) < n {
		r.fail("ends early")
		return nil
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}

func (r *wkbReader) geometry(depth int) []polygon {
	if depth > 2 {
		r.fail("nested too deeply")
		return nil
	}
	head := r.take(5)
	if head == nil {
		return nil
	}
	var order binary.ByteOrder = binary.LittleEndian
	if head[0] == 0 {
		order = binary.BigEndian
	} else if head[0] != 1 {
		r.fail("byte order %d", head[0])
		return nil
	}
	code := order.Uint32(head[1:])
	dims := 2
	// EWKB flags (Z 0x80000000, M 0x40000000, SRID 0x20000000) and ISO codes (1000s Z, 2000s M, 3000s ZM).
	if code&0x80000000 != 0 {
		dims++
	}
	if code&0x40000000 != 0 {
		dims++
	}
	if code&0x20000000 != 0 {
		r.take(4) // the SRID
	}
	code &= 0x0fffffff
	switch code / 1000 {
	case 1, 2:
		dims++
	case 3:
		dims += 2
	}
	code %= 1000
	u32 := func() int {
		b := r.take(4)
		if b == nil {
			return 0
		}
		return int(order.Uint32(b))
	}
	switch code {
	case 3: // Polygon
		rings := u32()
		if rings > len(r.b)/4 {
			r.fail("%d rings in %d bytes", rings, len(r.b))
			return nil
		}
		var p polygon
		for i := 0; i < rings && r.err == nil; i++ {
			n := u32()
			if n > len(r.b)/(8*dims) {
				r.fail("%d points in %d bytes", n, len(r.b))
				return nil
			}
			ring := make([]point, n)
			for k := range ring {
				c := r.take(8 * dims)
				if c == nil {
					return nil
				}
				ring[k] = point{math.Float64frombits(order.Uint64(c)), math.Float64frombits(order.Uint64(c[8:]))}
				if math.IsNaN(ring[k].X) || math.IsNaN(ring[k].Y) || math.IsInf(ring[k].X, 0) || math.IsInf(ring[k].Y, 0) {
					r.fail("a vertex is not finite")
					return nil
				}
			}
			if n < 3 {
				r.fail("a ring has %d vertices", n)
				return nil
			}
			p = append(p, ring)
		}
		if len(p) == 0 {
			// An empty polygon: no outline.
			return nil
		}
		return []polygon{p}
	case 6, 7: // MultiPolygon, GeometryCollection
		n := u32()
		if n > len(r.b)/5 {
			r.fail("%d parts in %d bytes", n, len(r.b))
			return nil
		}
		var out []polygon
		for i := 0; i < n && r.err == nil; i++ {
			out = append(out, r.geometry(depth+1)...)
		}
		return out
	default:
		r.fail("geometry type %d is not a cell's outline (polygons are)", code)
		return nil
	}
}

// --- Drawing ------------------------------------------------------------------------------------

// drawBand is how many of the grid's rows the cells are drawn into at a time.
const drawBand = 256

// drawCells draws each cell's polygons (µm) into the raster's rows as its label (its index + 1), a
// band of rows at a time, each row encoded as its band is done; in a band, the cells whose polygons
// reach it in their order, so that a pixel inside two cells' is the later's. It answers how many
// pixels another cell's label held already.
func drawCells(ctx context.Context, r *segment.Raster, cells [][]polygon) (int, error) {
	w, h, size := r.Width, r.Height, r.PixelSize
	bands := (h + drawBand - 1) / drawBand
	reach := make([][]int32, bands)
	for k, polys := range cells {
		lo, hi := h, -1
		for _, p := range polys {
			a, b := rowsOf(p, size, h)
			lo, hi = min(lo, a), max(hi, b)
		}
		for band := max(0, lo/drawBand); hi >= lo && band <= hi/drawBand; band++ {
			reach[band] = append(reach[band], int32(k))
		}
	}
	labels := make([]uint32, drawBand*w)
	overwritten := 0
	for band := 0; band < bands; band++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		y0, y1 := band*drawBand, min(h, (band+1)*drawBand)
		clear(labels)
		for _, k := range reach[band] {
			overwritten += draw(labels, w, y0, y1, size, cells[k], uint32(k+1))
		}
		reach[band] = nil
		for y := y0; y < y1; y++ {
			r.Rows[y] = segment.EncodeRow(labels[(y-y0)*w : (y-y0+1)*w])
		}
	}
	return overwritten, nil
}

// rowsOf is the rows of a grid of h rows (of pixels of size µm) a polygon may reach: first, last.
func rowsOf(p polygon, size float64, h int) (int, int) {
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, r := range p {
		for _, v := range r {
			minY, maxY = math.Min(minY, v.Y/size), math.Max(maxY, v.Y/size)
		}
	}
	if minY > maxY {
		return h, -1
	}
	return max(0, int(math.Floor(minY))), min(h-1, int(math.Ceil(maxY)))
}

// draw draws a cell's polygons (µm) into labels, the rows y0…y1−1 of a grid w pixels wide (pixels
// of size µm), as label: a pixel whose centre lies inside (even–odd). It answers how many of its
// pixels another label held already.
func draw(labels []uint32, w, y0, y1 int, size float64, polys []polygon, label uint32) int {
	overwritten := 0
	var cross []float64
	for _, p := range polys {
		first, last := rowsOf(p, size, y1)
		for py := max(y0, first); py <= last; py++ {
			cy := float64(py) + 0.5
			cross = cross[:0]
			for _, r := range p {
				for i, j := 0, len(r)-1; i < len(r); j, i = i, i+1 {
					yi, yj := r[i].Y/size, r[j].Y/size
					if (yi > cy) != (yj > cy) {
						xi, xj := r[i].X/size, r[j].X/size
						cross = append(cross, xi+(cy-yi)*(xj-xi)/(yj-yi))
					}
				}
			}
			sort.Float64s(cross)
			for c := 0; c+1 < len(cross); c += 2 {
				for px := max(0, int(math.Ceil(cross[c]-0.5))); px <= min(w-1, int(math.Ceil(cross[c+1]-0.5))-1); px++ {
					k := (py-y0)*w + px
					if labels[k] != 0 && labels[k] != label {
						overwritten++
					}
					labels[k] = label
				}
			}
		}
	}
	return overwritten
}
