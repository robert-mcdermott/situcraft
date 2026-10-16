package main

// What the comparison workspace draws (ui/mode-compare.js): each segmentation's outlines as
// geometry tiles on the dataset's grid, and a comparison's differences.
//
// The instrument's outlines are the dataset's own geometry entry (its cells are cells.parquet's
// rows, as the instrument's segmentation files number them). Any other segmentation's are tiled
// from its outlines into a cache entry of their own (kind "outlines"), built the first time they
// are asked for and served from /api/outlines/<entry>/….

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"situcraft/internal/artifacts"
	"situcraft/internal/cache"
	"situcraft/internal/compare"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// outlineEntries are the outline entries opened for serving.
type outlineEntries struct {
	mu   sync.Mutex
	open map[string]*cache.Geometry
	// building is held while an entry is built: a second request waits for the first's.
	building sync.Mutex
}

func (o *outlineEntries) get(name string) (*cache.Geometry, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	g, ok := o.open[name]
	return g, ok
}

func (o *outlineEntries) put(name string, g *cache.Geometry) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.open == nil {
		o.open = map[string]*cache.Geometry{}
	}
	if old, ok := o.open[name]; ok && old != g {
		old.Close()
	}
	o.open[name] = g
}

func (o *outlineEntries) closeAll() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, g := range o.open {
		g.Close()
	}
	o.open = nil
}

func (a *app) registerWorkspace(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/{index}/segmentations/{id}/outlines", a.segmentationOutlines)
	mux.HandleFunc("GET /api/outlines/{entry}/{set}/{level}/{tx}/{ty}", a.outlineTile)
	mux.HandleFunc("GET /api/local/{index}/comparisons/{id}/groups", a.comparisonGroups)
}

// segmentationOutlines answers where a segmentation's outline tiles are: {polygonsBase, geometry
// (the sets' summaries), cells (how many)}.
func (a *app) segmentationOutlines(w http.ResponseWriter, r *http.Request) {
	d, ok := a.dataset(w, r)
	if !ok {
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	uuid := b.Experiment.AnalysisUUID
	b.Close()
	m, err := a.segmentationOf(r.PathValue("id"), uuid)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if isBundleOwn(m) {
		desc, err := d.Describe()
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(w, map[string]any{"polygonsBase": "/api/cache/" + d.Entries[cache.KindGeometry] + "/polygons", "geometry": desc.Geometry, "cells": len(d.Cells.IDs)})
		return
	}
	dir, err := a.artifacts.Path(m.ID, segment.FileCells)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	folder := dir[:len(dir)-len(segment.FileCells)]
	cells, err := segment.ReadCells(folder)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	outlines, err := segment.ReadOutlines(folder)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	p := &xenium.Polygons{Offsets: []int32{0}}
	index := map[string]int32{}
	for i, c := range cells {
		index[c.ID] = int32(i)
		if i >= len(outlines) || len(outlines[i].X) < 3 {
			continue
		}
		p.CellIDs = append(p.CellIDs, c.ID)
		p.X = append(p.X, outlines[i].X...)
		p.Y = append(p.Y, outlines[i].Y...)
		p.Offsets = append(p.Offsets, int32(len(p.X)))
	}
	a.outlines.building.Lock()
	defer a.outlines.building.Unlock()
	var name string
	var summary *cache.GeometrySummary
	// An entry that does not open (one an interrupted build left) is built again, once.
	for attempt := 0; ; attempt++ {
		name, summary, err = a.cache.Outlines(r.Context(), m.ID, d.Grid, p, index)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if _, ok := a.outlines.get(name); ok {
			break
		}
		g, err := cache.OpenGeometry(a.cache.EntryDir(name))
		if err == nil {
			a.outlines.put(name, g)
			break
		}
		if attempt > 0 {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.cache.Remove(name)
	}
	writeJSON(w, map[string]any{"polygonsBase": "/api/outlines/" + name, "geometry": summary.Sets, "cells": len(cells)})
}

// outlineTile answers a tile of an outline entry, as the dataset's geometry tiles are answered.
func (a *app) outlineTile(w http.ResponseWriter, r *http.Request) {
	g, ok := a.outlines.get(r.PathValue("entry"))
	if !ok {
		writeError(w, http.StatusNotFound, "No outlines open by this name; ask for the segmentation's outlines first.")
		return
	}
	v, ok := ints(w, r, "level", "tx", "ty")
	if !ok {
		return
	}
	block, polygons, vertices, err := g.RawTile(r.PathValue("set"), v[0], v[1], v[2])
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

// earlierMethod says why a comparison made by another version of the method is not shown. Its
// headline numbers still read; its differences are laid out as that version wrote them.
func earlierMethod(v string) string {
	if n, err := strconv.Atoi(v); err == nil {
		if m, _ := strconv.Atoi(compare.MethodVersion); n > m {
			return fmt.Sprintf("This comparison was made by a newer SituCraft (comparison method %s; this one reads method %s). Open it with that version, or compare the two segmentations again here.", v, compare.MethodVersion)
		}
	}
	return fmt.Sprintf("This comparison was made by an earlier SituCraft (comparison method %s), whose list of differences this version (method %s) does not read. Compare the two segmentations again to see them; the comparison made then replaces this one in the workspace.", v, compare.MethodVersion)
}

// comparisonGroups answers a comparison's differences, column by column, with the ids of the two
// segmentations' cells: {classes, class, a, b, iou, jaccard, displacement, changed, x, y,
// transcriptsA, transcriptsB, mixedA, mixedB, idsA, idsB}.
// comparisonOf finds a comparison of the bundle's dataset and its groups, answering its manifest,
// the two segmentations it compares (A, B) and its differences; ok is false when it wrote why not.
func (a *app) comparisonOf(w http.ResponseWriter, id string) (artifacts.Manifest, [2]string, []compare.Group, string, bool) {
	var sides [2]string
	m, err := a.artifacts.Get(id)
	if errors.Is(err, artifacts.ErrNotFound) {
		writeErrorCode(w, http.StatusNotFound, "comparison.missing", "The comparison's files are not among SituCraft's artifacts (it was removed, or made on another computer). Compare the two segmentations again to make it here.")
		return m, sides, nil, "", false
	}
	if err != nil || m.Kind != "comparison" {
		writeError(w, http.StatusNotFound, "No such comparison.")
		return m, sides, nil, "", false
	}
	if v := m.Lineage.Method.Version; v != compare.MethodVersion {
		writeErrorCode(w, http.StatusUnprocessableEntity, "comparison.earlier-method", earlierMethod(v))
		return m, sides, nil, "", false
	}
	p, err := a.artifacts.Path(m.ID, compare.FileGroups)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return m, sides, nil, "", false
	}
	dir := p[:len(p)-len(compare.FileGroups)]
	groups, err := compare.ReadGroups(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return m, sides, nil, "", false
	}
	n := 0
	for _, parent := range m.Lineage.Parents {
		if parent.Kind == "segmentation" && n < 2 {
			sides[n] = parent.ID
			n++
		}
	}
	if n != 2 {
		writeError(w, http.StatusUnprocessableEntity, "The comparison does not name its two segmentations.")
		return m, sides, nil, "", false
	}
	return m, sides, groups, dir, true
}

func (a *app) comparisonGroups(w http.ResponseWriter, r *http.Request) {
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	_, sides, groups, _, ok := a.comparisonOf(w, r.PathValue("id"))
	if !ok {
		return
	}
	ids := func(id string) ([]string, error) {
		sm, err := a.artifacts.Get(id)
		if err != nil {
			return nil, err
		}
		dir, err := a.segmentationFolder(context.Background(), b, sm)
		if err != nil {
			return nil, err
		}
		cells, err := segment.ReadCells(dir)
		if err != nil {
			return nil, err
		}
		out := make([]string, len(cells))
		for i, c := range cells {
			out[i] = c.ID
		}
		return out, nil
	}
	idsA, err := ids(sides[0])
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	idsB, err := ids(sides[1])
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	n := len(groups)
	cols := map[string]any{"classes": compare.Classes}
	class := make([]int, n)
	as, bs := make([][]int32, n), make([][]int32, n)
	iou, jac, disp, xs, ys := make([]float32, n), make([]float32, n), make([]float32, n), make([]float32, n), make([]float32, n)
	changed, ta, tb, ma, mb := make([]uint32, n), make([]uint32, n), make([]uint32, n), make([]uint32, n), make([]uint32, n)
	index := map[string]int{}
	for i, c := range compare.Classes {
		index[c] = i
	}
	for i, g := range groups {
		class[i] = index[g.Class]
		as[i], bs[i] = append([]int32{}, g.A...), append([]int32{}, g.B...)
		iou[i], jac[i], disp[i], xs[i], ys[i] = g.IoU, g.Jaccard, g.Displacement, g.X, g.Y
		changed[i], ta[i], tb[i], ma[i], mb[i] = g.Changed, g.TranscriptsA, g.TranscriptsB, g.MixedA, g.MixedB
	}
	cols["class"], cols["a"], cols["b"] = class, as, bs
	cols["iou"], cols["jaccard"], cols["displacement"], cols["x"], cols["y"] = iou, jac, disp, xs, ys
	cols["changed"], cols["transcriptsA"], cols["transcriptsB"], cols["mixedA"], cols["mixedB"] = changed, ta, tb, ma, mb
	cols["idsA"], cols["idsB"] = idsA, idsB
	writeJSON(w, cols)
}
