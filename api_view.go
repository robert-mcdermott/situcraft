package main

// The tissue viewer's data (internal/view): a dataset's description, its cells and their details,
// and its derived data by tile. Tiles are addressed by cache entry, whose name hashes its sources:
// an entry never changes, so the browser may keep its tiles for good.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"situcraft/internal/cache"
	"situcraft/internal/view"
)

func (a *app) registerView(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/{index}/view", a.viewDescriptor)
	mux.HandleFunc("GET /api/local/{index}/cells", a.viewCells)
	mux.HandleFunc("GET /api/local/{index}/cells/{row}", a.viewCell)
	mux.HandleFunc("GET /api/local/{index}/points", a.viewRegionPoints)
	mux.HandleFunc("GET /api/cache/{entry}/image/{channel}/{level}/{tx}/{ty}", a.imageTile)
	mux.HandleFunc("GET /api/cache/{entry}/points/{tx}/{ty}", a.pointsTile)
	mux.HandleFunc("GET /api/cache/{entry}/density/{level}/{tx}/{ty}", a.densityTile)
	mux.HandleFunc("GET /api/cache/{entry}/polygons/{set}/{level}/{tx}/{ty}", a.polygonsTile)
}

// views holds the datasets open for viewing: by the bundle's place, and by the names of their
// entries (which the tile requests carry).
type views struct {
	mu      sync.Mutex
	byPath  map[string]*view.Dataset
	byEntry map[string]*view.Dataset
	order   []string // places, least recently opened first
}

// maxViews is how many datasets stay open at once.
const maxViews = 4

func newViews() *views {
	return &views{byPath: map[string]*view.Dataset{}, byEntry: map[string]*view.Dataset{}}
}

func (v *views) put(d *view.Dataset) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if old, ok := v.byPath[d.Path]; ok {
		v.drop(old)
	}
	v.byPath[d.Path] = d
	for _, name := range d.Entries {
		v.byEntry[name] = d
	}
	v.order = append(v.order, d.Path)
	for len(v.order) > maxViews {
		if old, ok := v.byPath[v.order[0]]; ok {
			v.drop(old)
		}
	}
}

// drop closes a dataset and forgets it. v.mu is held.
func (v *views) drop(d *view.Dataset) {
	delete(v.byPath, d.Path)
	for _, name := range d.Entries {
		if v.byEntry[name] == d {
			delete(v.byEntry, name)
		}
	}
	kept := v.order[:0]
	for _, p := range v.order {
		if p != d.Path {
			kept = append(kept, p)
		}
	}
	v.order = kept
	d.Close()
}

func (v *views) entry(name string) (*view.Dataset, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	d, ok := v.byEntry[name]
	return d, ok
}

func (v *views) path(p string) (*view.Dataset, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	d, ok := v.byPath[p]
	return d, ok
}

func (v *views) closeAll() {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, d := range v.byPath {
		v.drop(d)
	}
}

// dataset opens (or finds open) the dataset of the local item in the request. A dataset whose
// derived data are not all built answers 409.
func (a *app) dataset(w http.ResponseWriter, r *http.Request) (*view.Dataset, bool) {
	if !a.needCache(w) {
		return nil, false
	}
	file, ok := a.files.get(r.PathValue("index"))
	if !ok {
		writeError(w, http.StatusNotFound, "No such local item.")
		return nil, false
	}
	if d, ok := a.views.path(file.path); ok {
		// Still the same entries? A bundle that changed has new ones.
		if b, ok := a.openBundle(w, r); ok {
			status, err := a.cache.Status(b)
			b.Close()
			if err == nil && sameEntries(status, d.Entries) {
				return d, true
			}
		} else {
			return nil, false
		}
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return nil, false
	}
	defer b.Close()
	d, err := view.Open(a.cache, b)
	if errors.Is(err, view.ErrNotReady) {
		writeError(w, http.StatusConflict, "Its derived data are not built yet; build them in the Data view.")
		return nil, false
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return nil, false
	}
	a.views.put(d)
	return d, true
}

func sameEntries(s *cache.Status, entries map[string]string) bool {
	if !s.Ready {
		return false
	}
	for _, e := range s.Entries {
		if e.State == "ready" && entries[e.Kind] != e.Name {
			return false
		}
	}
	return true
}

func (a *app) viewDescriptor(w http.ResponseWriter, r *http.Request) {
	d, ok := a.dataset(w, r)
	if !ok {
		return
	}
	desc, err := d.Describe()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, desc)
}

func (a *app) viewCells(w http.ResponseWriter, r *http.Request) {
	d, ok := a.dataset(w, r)
	if !ok {
		return
	}
	table, err := d.CellTable()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeBinary(w, table, false)
}

func (a *app) viewCell(w http.ResponseWriter, r *http.Request) {
	d, ok := a.dataset(w, r)
	if !ok {
		return
	}
	row, err := strconv.Atoi(r.PathValue("row"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "A cell is named by its row.")
		return
	}
	top := 12
	if s := r.URL.Query().Get("top"); s != "" {
		if top, err = strconv.Atoi(s); err != nil || top < 0 || top > 10000 {
			writeError(w, http.StatusBadRequest, "top must be a number of genes.")
			return
		}
	}
	cell, err := d.Cell(row, top)
	if errors.Is(err, view.ErrNoCell) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, cell)
}

// viewRegionPoints answers the transcripts in a region: ?x0=&y0=&x1=&y1= (µm), genes=1,5 and
// minq=20.
func (a *app) viewRegionPoints(w http.ResponseWriter, r *http.Request) {
	d, ok := a.dataset(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var rect view.Rect
	for _, f := range []struct {
		name string
		dst  *float64
	}{{"x0", &rect.X0}, {"y0", &rect.Y0}, {"x1", &rect.X1}, {"y1", &rect.Y1}} {
		v, err := strconv.ParseFloat(q.Get(f.name), 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Give the region as x0, y0, x1 and y1 in µm.")
			return
		}
		*f.dst = v
	}
	genes, minQ, ok := pointParams(w, r)
	if !ok {
		return
	}
	pts, err := d.RegionPoints(rect, genes, minQ)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeBinary(w, pts.Encode(), false)
}

// pointParams reads genes= (indexes, comma-separated; absent for every gene) and minq=.
func pointParams(w http.ResponseWriter, r *http.Request) (view.Genes, float32, bool) {
	q := r.URL.Query()
	genes, err := parseGenes(q.Get("genes"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, 0, false
	}
	minQ := 0.0
	if s := q.Get("minq"); s != "" {
		if minQ, err = strconv.ParseFloat(s, 64); err != nil {
			writeError(w, http.StatusBadRequest, "minq must be a number.")
			return nil, 0, false
		}
	}
	return genes, float32(minQ), true
}

func parseGenes(s string) (view.Genes, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) > 1<<16 {
		return nil, errors.New("too many genes")
	}
	genes := make(view.Genes, 0, len(parts))
	for _, p := range parts {
		g, err := strconv.Atoi(p)
		if err != nil {
			return nil, errors.New("genes are given by their indexes, separated by commas")
		}
		genes = append(genes, g)
	}
	return genes, nil
}

// entryDataset finds the open dataset an entry belongs to.
func (a *app) entryDataset(w http.ResponseWriter, r *http.Request) (*view.Dataset, bool) {
	d, ok := a.views.entry(r.PathValue("entry"))
	if !ok {
		writeError(w, http.StatusNotFound, "No dataset open for viewing has this entry; open its view first.")
		return nil, false
	}
	return d, true
}

// ints reads path values as non-negative integers.
func ints(w http.ResponseWriter, r *http.Request, names ...string) ([]int, bool) {
	out := make([]int, len(names))
	for i, n := range names {
		v, err := strconv.Atoi(r.PathValue(n))
		if err != nil || v < 0 {
			writeError(w, http.StatusBadRequest, n+" must be a whole number.")
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// immutable marks a response as never changing (it is addressed by a content-named entry).
func immutable(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
}

func writeBinary(w http.ResponseWriter, body []byte, forever bool) {
	if forever {
		immutable(w)
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Write(body)
}

// imageTile answers a tile as stored: a zlib stream (sent as Content-Encoding: deflate, which the
// browser inflates) of the tile's 16-bit samples, every low byte, then every high byte.
func (a *app) imageTile(w http.ResponseWriter, r *http.Request) {
	d, ok := a.entryDataset(w, r)
	if !ok {
		return
	}
	v, ok := ints(w, r, "channel", "level", "tx", "ty")
	if !ok {
		return
	}
	if d.Image == nil {
		writeError(w, http.StatusNotFound, "The dataset has no image.")
		return
	}
	block, width, height, err := d.Image.RawTile(v[0], v[1], v[2], v[3])
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	h := w.Header()
	h.Set("Content-Encoding", "deflate")
	h.Set("X-Tile-Width", strconv.Itoa(width))
	h.Set("X-Tile-Height", strconv.Itoa(height))
	writeBinary(w, block, true)
}

func (a *app) pointsTile(w http.ResponseWriter, r *http.Request) {
	d, ok := a.entryDataset(w, r)
	if !ok {
		return
	}
	v, ok := ints(w, r, "tx", "ty")
	if !ok {
		return
	}
	genes, minQ, ok := pointParams(w, r)
	if !ok {
		return
	}
	pts, err := d.TilePoints(v[0], v[1], genes, minQ)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeBinary(w, pts.Encode(), true)
}

// densityTile answers a tile's density grids: ?genes= (absent: every gene together) and q20=1
// to count only transcripts of quality 20 or more.
func (a *app) densityTile(w http.ResponseWriter, r *http.Request) {
	d, ok := a.entryDataset(w, r)
	if !ok {
		return
	}
	v, ok := ints(w, r, "level", "tx", "ty")
	if !ok {
		return
	}
	genes, err := parseGenes(r.URL.Query().Get("genes"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	grids, err := d.TileDensity(v[0], v[1], v[2], genes, r.URL.Query().Get("q20") == "1")
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeBinary(w, grids.Encode(), true)
}

// polygonsTile answers a set's tile as stored (a zlib stream, sent as Content-Encoding: deflate):
// cell u32[polygons] | starts u32[polygons+1] | xy f32[2 × vertices], with the two counts in
// X-Polygons and X-Vertices.
func (a *app) polygonsTile(w http.ResponseWriter, r *http.Request) {
	d, ok := a.entryDataset(w, r)
	if !ok {
		return
	}
	v, ok := ints(w, r, "level", "tx", "ty")
	if !ok {
		return
	}
	if d.Geometry == nil {
		writeError(w, http.StatusNotFound, "The dataset has no cell outlines.")
		return
	}
	block, polygons, vertices, err := d.Geometry.RawTile(r.PathValue("set"), v[0], v[1], v[2])
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	h := w.Header()
	h.Set("X-Polygons", strconv.Itoa(polygons))
	h.Set("X-Vertices", strconv.Itoa(vertices))
	if block == nil {
		writeBinary(w, nil, true)
		return
	}
	h.Set("Content-Encoding", "deflate")
	writeBinary(w, block, true)
}
