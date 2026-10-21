package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/label"
	"situcraft/internal/query"
	"situcraft/internal/segment"
	"situcraft/internal/spatial"
	"situcraft/internal/xenium"
)

// The window's choices in a recipe (wave 9, slice 6): a step whose spec carries cells the window
// chose (recipes.go) is made again on another bundle when the program can choose them as the
// window did: every cell of the segmentation, a saved filter's cells (its conditions or tree,
// internal/query, as the window's filters are evaluated), and each cell's label of a label set an
// earlier step makes, at a level (lib/spatial.js labelsRequest). A recipe says so only when the
// program, choosing them on the bundle the recipe was written from, gives the very cells the window
// gave (their checksums); a region's cells, a review's, curated or consensus labels and niches are
// the window's alone, and their steps are made only on their own bundle.

// recipeChoice says how a field of a step's spec is chosen again.
type recipeChoice struct {
	// Field is the spec's: filter (a clustering's cells), cells (a graph's or spatial analysis's),
	// labels (a spatial analysis's).
	Field  string        `json:"field"`
	All    bool          `json:"all,omitempty"`
	Filter *recipeFilter `json:"filter,omitempty"`
	Labels *recipeLabels `json:"labels,omitempty"`
}

// recipeFilter is a saved filter as the project records it: its conditions, or its tree (whose
// label sets, clusterings and niches are named by the steps that make them).
type recipeFilter struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Conditions []json.RawMessage `json:"conditions,omitempty"`
	Tree       json.RawMessage   `json:"tree,omitempty"`
}

// recipeLabels are each cell's label of the label set a step makes, at a level, as the window
// gave them: the source's name, and the colors it gave its labels (a label it did not use, the
// set's color or the palette's).
type recipeLabels struct {
	Step   int               `json:"step"`
	Level  int               `json:"level"`
	Name   string            `json:"name"`
	Colors map[string]string `json:"colors,omitempty"`
}

// filterDefinition is a saved filter's record, from the library project holding it.
func (a *app) filterDefinition(id string) (*queryFilter, error) {
	projects, err := a.libraryProjects()
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		for _, raw := range p.Artifacts {
			var f queryFilter
			var head struct{ Kind string }
			if json.Unmarshal(raw, &head) != nil || head.Kind != "filter" || json.Unmarshal(raw, &f) != nil || f.ID != id {
				continue
			}
			return &f, nil
		}
	}
	return nil, fmt.Errorf("the filter %s is in no project of the library", id)
}

// stepChoices are how a step's carried fields are chosen again (spec: as its manifest records it,
// with this computer's ids; stepOf: the recipe's steps by the ids they made), and whether every
// field it carries is among them.
func (a *app) stepChoices(job string, spec json.RawMessage, stepOf map[string]int) ([]recipeChoice, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(spec, &fields) != nil {
		return nil, false
	}
	var out []recipeChoice
	// A field's cells: every cell, or a saved filter's (whose tree names only earlier steps).
	cellsChoice := func(field string, raw json.RawMessage) (recipeChoice, bool) {
		var c graphCells
		if json.Unmarshal(raw, &c) != nil || len(c.Regions) > 0 || c.Excluded > 0 {
			return recipeChoice{}, false
		}
		if c.Filter == nil {
			return recipeChoice{Field: field, All: true}, true
		}
		f, ok := a.portableFilter(c.Filter.ID, c.Filter.Name, stepOf)
		return recipeChoice{Field: field, Filter: f}, ok
	}
	switch job {
	case "clustering":
		if raw, ok := fields["filter"]; ok && string(raw) != "null" {
			var f clusterFilter
			if json.Unmarshal(raw, &f) != nil {
				return nil, false
			}
			def, ok := a.portableFilter(f.ID, f.Name, stepOf)
			if !ok {
				return nil, false
			}
			out = append(out, recipeChoice{Field: "filter", Filter: def})
		}
	case "graph":
		c, ok := cellsChoice("cells", fields["cells"])
		if !ok {
			return nil, false
		}
		out = append(out, c)
	case "spatial":
		for _, field := range []string{"strata", "window", "regions"} {
			if raw, ok := fields[field]; ok && string(raw) != "null" && !(field == "window" && tissueWindow(raw)) {
				return nil, false
			}
		}
		if raw, ok := fields["cells"]; ok && string(raw) != "null" {
			c, ok := cellsChoice("cells", raw)
			if !ok {
				return nil, false
			}
			out = append(out, c)
		}
		if raw, ok := fields["labels"]; ok && string(raw) != "null" {
			var l spatialLabels
			if json.Unmarshal(raw, &l) != nil || l.Kind != "labels" {
				return nil, false
			}
			k, ok := stepOf[l.ID]
			if !ok {
				return nil, false
			}
			colors := map[string]string{}
			for i, name := range l.Names {
				if i < len(l.Colors) {
					colors[name] = l.Colors[i]
				}
			}
			out = append(out, recipeChoice{Field: "labels", Labels: &recipeLabels{Step: k, Level: l.Level, Name: l.Name, Colors: colors}})
		}
	default:
		return nil, false
	}
	return out, true
}

// specFilters are the saved filters a spec's cells were chosen by (a clustering's filter, a graph's
// or spatial analysis's cells' filter), by id.
func specFilters(spec json.RawMessage) []string {
	var s struct {
		Filter *struct{ ID string } `json:"filter"`
		Cells  *struct {
			Filter *struct{ ID string } `json:"filter"`
		} `json:"cells"`
	}
	json.Unmarshal(spec, &s)
	var out []string
	if s.Filter != nil && s.Filter.ID != "" {
		out = append(out, s.Filter.ID)
	}
	if s.Cells != nil && s.Cells.Filter != nil && s.Cells.Filter.ID != "" {
		out = append(out, s.Cells.Filter.ID)
	}
	return out
}

// tissueWindow says whether a spatial window is the tissue's (not a region's).
func tissueWindow(raw json.RawMessage) bool {
	var w spatialWindow
	return json.Unmarshal(raw, &w) == nil && w.Kind != "region"
}

// portableFilter is a saved filter's definition when its tree names nothing of its bundle (a
// region, another filter) and only label sets, clusterings and niches the recipe's steps make,
// named by them.
func (a *app) portableFilter(id, shownName string, stepOf map[string]int) (*recipeFilter, bool) {
	f, err := a.filterDefinition(id)
	if err != nil {
		return nil, false
	}
	def := &recipeFilter{ID: f.ID, Name: shownName, Conditions: f.Conditions}
	if len(f.Tree) > 0 && string(f.Tree) != "null" {
		tree, err := query.Parse(f.Tree, query.Cells)
		if err != nil {
			return nil, false
		}
		portable := true
		tree.Walk(func(c *query.Condition) {
			switch c.Kind {
			case "region", "distance", "filter":
				portable = false
			case "labels", "clusters", "niches":
				if _, ok := stepOf[c.Name]; !ok {
					portable = false
				}
			}
		})
		if !portable {
			return nil, false
		}
		def.Tree = rewriteStrings(f.Tree, func(s string) (string, bool) {
			if k, ok := stepOf[s]; ok {
				return fmt.Sprintf("situcraft:step/%d", k), true
			}
			return "", false
		})
	}
	return def, true
}

// portableChoices are a carried step's choices, when the program, choosing them on the bundle the
// recipe is written from, gives the cells the window gave.
func (a *app) portableChoices(m artifacts.Manifest, stepOf map[string]int, n int) ([]recipeChoice, bool) {
	earlier := map[string]int{}
	made := map[int]string{}
	for id, k := range stepOf {
		if k < n {
			earlier[id], made[k] = k, id
		}
	}
	choices, ok := a.stepChoices(m.Lineage.Job.Kind, m.Lineage.Job.Spec, earlier)
	if !ok {
		return nil, false
	}
	bundles := specBundles(m.Lineage.Job.Spec)
	if len(bundles) != 1 {
		return nil, false
	}
	c := choiceContext{a: a, ctx: context.Background(), bundle: bundles[0], made: made}
	remade, err := c.remakeChoices(m.Lineage.Job.Spec, choices)
	if err != nil || !sameChoices(m.Lineage.Job.Spec, remade) {
		return nil, false
	}
	return choices, true
}

// --- Choosing them again ----------------------------------------------------------------------------

// keepPayload is cells as a request sends them: base64 of keepBytes, and their SHA-256.
func keepPayload(keep []bool) (string, string) {
	raw := query.KeepBytes(keep)
	sum := sha256.Sum256(raw)
	return base64.StdEncoding.EncodeToString(raw), hex.EncodeToString(sum[:])
}

// codesPayload is a number for each cell as a request sends it (lib/spatial.js codesRequest).
func codesPayload(values []int32) cellCodes {
	raw := make([]byte, 4+4*len(values))
	binary.LittleEndian.PutUint32(raw, uint32(len(values)))
	for i, v := range values {
		binary.LittleEndian.PutUint32(raw[4+4*i:], uint32(v))
	}
	sum := sha256.Sum256(raw)
	return cellCodes{Codes: base64.StdEncoding.EncodeToString(raw), Checksum: hex.EncodeToString(sum[:])}
}

// choiceContext is what choosing a step's cells again reads: the app, the bundle (its folder), and
// the ids the recipe's earlier steps made.
type choiceContext struct {
	a      *app
	ctx    context.Context
	bundle string
	made   map[int]string
}

// segmentationCells is how many cells a segmentation has.
func (c choiceContext) segmentationCells(id string) (int, error) {
	m, err := c.a.artifacts.Get(id)
	if err != nil {
		return 0, err
	}
	b, err := xenium.Open(c.bundle, xenium.Options{CacheDir: c.a.cacheDir})
	if err != nil {
		return 0, err
	}
	defer b.Close()
	dir, err := c.a.segmentationFolder(c.ctx, b, m)
	if err != nil {
		return 0, err
	}
	cells, err := segment.ReadCells(dir)
	return len(cells), err
}

// filterKeep evaluates a filter on the bundle's instrument cells.
func (c choiceContext) filterKeep(f *recipeFilter) ([]bool, error) {
	def := queryFilter{ID: f.ID, Name: f.Name, Conditions: f.Conditions}
	if len(f.Tree) > 0 {
		tree, missing := localizeRecipe(f.Tree, nil, nil, c.made)
		if len(missing) > 0 {
			return nil, fmt.Errorf("the filter %s names %s, not made", f.Name, strings.Join(missing, ", "))
		}
		def.Tree = tree
	}
	tree, err := filterTree(&def)
	if err != nil {
		return nil, err
	}
	b, err := xenium.Open(c.bundle, xenium.Options{CacheDir: c.a.cacheDir})
	if err != nil {
		return nil, err
	}
	defer b.Close()
	src, err := c.a.cellSourceOf(c.ctx, b, c.bundle, "", "instrument", nil, &queryContext{})
	if err != nil {
		return nil, err
	}
	return tree.Eval(src)
}

// labelsOf are each cell's label of a label set at a level, as the window gives them for a spatial
// analysis (lib/spatial.js labelsRequest): only the labels some cell has, numbered again in their
// order, with their colors.
func (c choiceContext) labelsOf(l *recipeLabels, segmentation string) (spatialLabels, error) {
	id, ok := c.made[l.Step]
	if !ok {
		return spatialLabels{}, fmt.Errorf("step %d's label set is not made", l.Step)
	}
	n, err := c.segmentationCells(segmentation)
	if err != nil {
		return spatialLabels{}, err
	}
	d, _, lab, err := label.Read(filepath.Join(c.a.artifacts.Dir(), id))
	if err != nil {
		return spatialLabels{}, err
	}
	level := min(l.Level, len(lab.Label)-1)
	per := make([]int32, n)
	for i := range per {
		per[i] = -1
	}
	for i, row := range lab.Cells {
		if int(row) < n {
			per[row] = lab.Label[level][i]
		}
	}
	used := make([]bool, len(d.Sets))
	for _, k := range per {
		if k >= 0 && int(k) < len(used) {
			used[k] = true
		}
	}
	index := make([]int32, len(d.Sets))
	out := spatialLabels{ID: id, Kind: "labels", Name: l.Name, Level: l.Level, Names: []string{}, Colors: []string{}}
	for k, s := range d.Sets {
		index[k] = -1
		if !used[k] {
			continue
		}
		index[k] = int32(len(out.Names))
		out.Names = append(out.Names, s.Name)
		color, ok := l.Colors[s.Name]
		if !ok {
			color = s.Color
			if color == "" {
				color = categoricalColor(k)
			}
		}
		out.Colors = append(out.Colors, color)
	}
	codes := make([]int32, n)
	for i, k := range per {
		codes[i] = -1
		if k >= 0 && int(k) < len(index) {
			codes[i] = index[k]
		}
	}
	out.cellCodes = codesPayload(codes)
	return out, nil
}

// remakeChoices chooses a step's carried fields again on the bundle, answering its spec with them.
func (c choiceContext) remakeChoices(spec []byte, choices []recipeChoice) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(spec, &fields); err != nil {
		return nil, err
	}
	var segmentation string
	json.Unmarshal(fields["segmentation"], &segmentation)
	set := func(field string, v any) error {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		fields[field] = raw
		return nil
	}
	for _, ch := range choices {
		switch {
		case ch.Field == "filter" && ch.Filter != nil:
			keep, err := c.filterKeep(ch.Filter)
			if err != nil {
				return nil, err
			}
			b64, sum := keepPayload(keep)
			if err := set("filter", clusterFilter{ID: ch.Filter.ID, Name: ch.Filter.Name, Checksum: sum, Keep: b64}); err != nil {
				return nil, err
			}
		case ch.Field == "cells":
			var keep []bool
			cells := graphCells{}
			if ch.All {
				n, err := c.segmentationCells(segmentation)
				if err != nil {
					return nil, err
				}
				keep = make([]bool, n)
				for i := range keep {
					keep[i] = true
				}
			} else if ch.Filter != nil {
				var err error
				if keep, err = c.filterKeep(ch.Filter); err != nil {
					return nil, err
				}
			} else {
				return nil, errors.New("a choice of cells without a filter or every cell")
			}
			cells.Keep, cells.Checksum = keepPayload(keep)
			if ch.Filter != nil {
				cells.Filter = &spatial.Filter{ID: ch.Filter.ID, Name: ch.Filter.Name, Checksum: cells.Checksum}
			}
			if err := set("cells", cells); err != nil {
				return nil, err
			}
		case ch.Field == "labels" && ch.Labels != nil:
			labels, err := c.labelsOf(ch.Labels, segmentation)
			if err != nil {
				return nil, err
			}
			if err := set("labels", labels); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("a choice of %s this SituCraft does not make", ch.Field)
		}
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(fields); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// sameChoices says whether choices made again give the payloads a spec carries (their checksums).
func sameChoices(recorded, remade []byte) bool {
	sums := func(raw []byte) string {
		var fields map[string]json.RawMessage
		json.Unmarshal(raw, &fields)
		var parts []string
		for _, f := range []string{"filter", "cells", "labels"} {
			var c struct {
				Checksum string `json:"checksum"`
			}
			if json.Unmarshal(fields[f], &c) == nil && c.Checksum != "" {
				parts = append(parts, f+"="+c.Checksum)
			}
		}
		return strings.Join(parts, " ")
	}
	return sums(recorded) == sums(remade)
}

// --- The window's palette ------------------------------------------------------------------------------

// categoricalPalette is lib/colormaps.js CATEGORICAL, the colors a label without its own takes.
var categoricalPalette = []string{
	"#4c78e0", "#f0803c", "#3fb27f", "#e45563", "#9a6fd8", "#c99a2e", "#2bb3c0", "#e86fb3",
	"#7c8a2b", "#8f5b3a", "#5c6bc0", "#26a69a", "#ef6c00", "#ad1457", "#6d4c41", "#00897b",
	"#7e57c2", "#c0ca33", "#d84315", "#546e7a",
}

// categoricalColor is lib/colormaps.js categoricalColor (the default palette): its colors, then
// golden-angle hues.
func categoricalColor(index int) string {
	if index < len(categoricalPalette) {
		return categoricalPalette[index]
	}
	h := math.Mod(float64(index)*137.508, 360)
	sat, light := 0.62, 0.52
	a := sat * math.Min(light, 1-light)
	f := func(n float64) float64 {
		k := math.Mod(n+h/30, 12)
		return light - a*math.Max(-1, math.Min(k-3, math.Min(9-k, 1)))
	}
	hex := func(v float64) string {
		return fmt.Sprintf("%02x", int(math.Round(math.Max(0, math.Min(255, v)))))
	}
	return "#" + hex(f(0)*255) + hex(f(8)*255) + hex(f(4)*255)
}
