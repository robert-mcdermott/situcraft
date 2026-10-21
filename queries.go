package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/cache"
	"situcraft/internal/cluster"
	"situcraft/internal/label"
	"situcraft/internal/query"
	"situcraft/internal/segment"
	"situcraft/internal/spatial"
	"situcraft/internal/view"
	"situcraft/internal/xenium"
)

// Cells and transcripts asked for by a typed filter tree (wave 9, slice 3; requirement A3;
// internal/query): the tree's conditions answered from a dataset's columns (its cells' QC
// attributes, their genes' counts, a label set's, clustering's or niches analysis's cells, the
// project's regions and saved filters) and from its transcript index. The program evaluates a tree
// for agents and scripts (query_cells, query_transcripts and summarize_region, answered without
// the window) and for the page (a filter saved as a tree, POST /api/local/{index}/query).

// queryContext is what a tree's names refer to beyond the dataset: the project's regions, saved
// filters and clusters' names. The program reads them from the library's project; the page sends
// its own, its unsaved changes counting.
type queryContext struct {
	Regions      []queryRegion  `json:"regions"`
	Filters      []queryFilter  `json:"filters"`
	ClusterNames []clusterNames `json:"clusterNames"`
}

// queryRegion is a region as the project keeps it: its versions, the latest the region.
type queryRegion struct {
	ID       string `json:"id"`
	Dataset  string `json:"dataset"`
	Versions []struct {
		Name     string           `json:"name"`
		Cls      string           `json:"cls"`
		Geometry [][][][2]float64 `json:"geometry"`
		Removed  bool             `json:"removed"`
	} `json:"versions"`
}

// queryFilter is a saved filter: its conditions (the QC view's) or its tree.
type queryFilter struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Dataset    string            `json:"dataset"`
	Version    int               `json:"version"`
	Conditions []json.RawMessage `json:"conditions"`
	Tree       json.RawMessage   `json:"tree"`
}

// clusterNames is a version of a clustering's clusters' names (null for a cluster not named).
type clusterNames struct {
	Clustering string    `json:"clustering"`
	Version    int       `json:"version"`
	Names      []*string `json:"names"`
}

// contextOf reads a project's regions, filters and cluster names.
func contextOf(p *libraryProject) *queryContext {
	qc := &queryContext{Regions: p.Regions}
	for _, raw := range p.Artifacts {
		var head struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(raw, &head) != nil {
			continue
		}
		switch head.Kind {
		case "filter":
			var f queryFilter
			if json.Unmarshal(raw, &f) == nil {
				qc.Filters = append(qc.Filters, f)
			}
		case "cluster-names":
			var n clusterNames
			if json.Unmarshal(raw, &n) == nil {
				qc.ClusterNames = append(qc.ClusterNames, n)
			}
		}
	}
	return qc
}

// region finds a dataset's region (its latest version, not removed) by id or name.
func (qc *queryContext) region(dataset, ref string) (string, [][][][2]float64, error) {
	var names []string
	for _, r := range qc.Regions {
		if r.Dataset != dataset || len(r.Versions) == 0 {
			continue
		}
		last := r.Versions[len(r.Versions)-1]
		if last.Removed {
			continue
		}
		if r.ID == ref || strings.EqualFold(last.Name, ref) {
			return last.Name, last.Geometry, nil
		}
		names = append(names, last.Name)
	}
	if len(names) == 0 {
		return "", nil, fmt.Errorf("the dataset has no region (%q asked for): draw_region draws one", ref)
	}
	return "", nil, fmt.Errorf("no region %q of the dataset; its regions are %s", ref, strings.Join(names, ", "))
}

// filter finds a dataset's saved filter by id, or by name (its latest version).
func (qc *queryContext) filter(dataset, ref string) (*queryFilter, error) {
	var found *queryFilter
	names := map[string]bool{}
	for i := range qc.Filters {
		f := &qc.Filters[i]
		if f.Dataset != dataset {
			continue
		}
		names[f.Name] = true
		if f.ID == ref {
			return f, nil
		}
		if strings.EqualFold(f.Name, ref) && (found == nil || f.Version > found.Version) {
			found = f
		}
	}
	if found != nil {
		return found, nil
	}
	var list []string
	for n := range names {
		list = append(list, n)
	}
	sort.Strings(list)
	if len(list) == 0 {
		return nil, fmt.Errorf("the dataset has no saved filter (%q asked for)", ref)
	}
	return nil, fmt.Errorf("no filter %q of the dataset; its filters are %s", ref, strings.Join(list, ", "))
}

// clusterNamesOf are a clustering's clusters' names, as the page names them: the latest version's,
// "Cluster k" for one not named.
func (qc *queryContext) clusterNamesOf(clustering string, count int) []string {
	var latest *clusterNames
	for i := range qc.ClusterNames {
		n := &qc.ClusterNames[i]
		if n.Clustering == clustering && (latest == nil || n.Version > latest.Version) {
			latest = n
		}
	}
	out := make([]string, count)
	for k := range out {
		out[k] = fmt.Sprintf("Cluster %d", k+1)
		if latest != nil && k < len(latest.Names) && latest.Names[k] != nil && strings.TrimSpace(*latest.Names[k]) != "" {
			out[k] = strings.TrimSpace(*latest.Names[k])
		}
	}
	return out
}

// filterTree is a saved filter as a tree: its own, or its conditions under all.
func filterTree(f *queryFilter) (*query.Node, error) {
	if len(f.Tree) > 0 && string(f.Tree) != "null" {
		return query.Parse(f.Tree, query.Cells)
	}
	if len(f.Conditions) == 0 {
		return nil, fmt.Errorf("the filter %s has no conditions", f.Name)
	}
	var nodes []json.RawMessage
	for _, raw := range f.Conditions {
		var c struct {
			Attribute string   `json:"attribute"`
			Min       *float64 `json:"min"`
			Max       *float64 `json:"max"`
			Values    []int    `json:"values"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("a condition of the filter %s cannot be read", f.Name)
		}
		node := map[string]any{"attribute": c.Attribute}
		if c.Attribute == "method" {
			node["in"] = c.Values
		} else {
			if c.Min != nil {
				node["min"] = *c.Min
			}
			if c.Max != nil {
				node["max"] = *c.Max
			}
		}
		encoded, _ := json.Marshal(node)
		nodes = append(nodes, encoded)
	}
	encoded, _ := json.Marshal(map[string]any{"all": nodes})
	return query.Parse(encoded, query.Cells)
}

// --- A dataset's cells ---------------------------------------------------------------------------

// cellSource answers a tree's conditions over a segmentation's cells (query.Source), reading each
// column when first asked.
type cellSource struct {
	a          *app
	ctx        context.Context
	b          *xenium.Bundle
	path       string
	dataset    string // the project's dataset id
	seg        artifacts.Manifest
	instrument bool
	qc         *queryContext
	depth      int
	n          int // cells

	dir     string
	cells   []segment.Cell
	counts  *segment.Counts
	table   *cellTable
	windows map[string]*spatial.Window
}

// cellTable is the Viewer's cells table (view.Dataset.CellTable), read as the page reads it
// (lib/viewer-data.js readCells), so that a tree tests the very numbers its filters do.
type cellTable struct {
	n                                                                   int
	methods                                                             []string
	x, y, area, nucleusArea                                             []float32
	transcripts, total, controlProbes, controlCodewords, genes, nuclear []uint32
	method                                                              []uint8
	ids                                                                 []string
}

func readCellTable(raw []byte, methods []string, ids []string) (*cellTable, error) {
	if len(raw) < 16 || string(raw[:4]) != "SCVC" {
		return nil, errors.New("not a cells table")
	}
	n := int(binary.LittleEndian.Uint32(raw[4:]))
	at := 16
	f32 := func() []float32 {
		out := make([]float32, n)
		for i := range out {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[at+4*i:]))
		}
		at += 4 * n
		return out
	}
	u32 := func() []uint32 {
		out := make([]uint32, n)
		for i := range out {
			out[i] = binary.LittleEndian.Uint32(raw[at+4*i:])
		}
		at += 4 * n
		return out
	}
	if len(raw) < 16+4*n*10+n {
		return nil, errors.New("a cells table of the wrong length")
	}
	t := &cellTable{n: n, methods: methods, ids: ids}
	t.x, t.y, t.area, t.nucleusArea = f32(), f32(), f32(), f32()
	t.transcripts, t.total, t.controlProbes, t.controlCodewords, t.genes, t.nuclear = u32(), u32(), u32(), u32(), u32(), u32()
	t.method = append([]uint8(nil), raw[at:at+n]...)
	return t, nil
}

func (s *cellSource) folder() (string, error) {
	if s.dir == "" {
		dir, err := s.a.segmentationFolder(s.ctx, s.b, s.seg)
		if err != nil {
			return "", err
		}
		s.dir = dir
	}
	return s.dir, nil
}

func (s *cellSource) segCells() ([]segment.Cell, error) {
	if s.cells == nil {
		dir, err := s.folder()
		if err != nil {
			return nil, err
		}
		if s.cells, err = segment.ReadCells(dir); err != nil {
			return nil, err
		}
	}
	return s.cells, nil
}

func (s *cellSource) cellTable() (*cellTable, error) {
	if s.table == nil {
		d, err := s.a.openView(s.b, s.path)
		if errors.Is(err, view.ErrNotReady) {
			return nil, errors.New("the dataset's derived data are not built yet (open it in SituCraft, or wait for them)")
		}
		if err != nil {
			return nil, err
		}
		raw, err := d.CellTable()
		if err != nil {
			return nil, err
		}
		var methods []string
		if d.Methods != nil {
			methods = d.Methods.Methods
		}
		if s.table, err = readCellTable(raw, methods, d.Cells.IDs); err != nil {
			return nil, err
		}
	}
	return s.table, nil
}

// Len is the segmentation's cells (the instrument's: the cells table's), read when the source is
// opened (opened).
func (s *cellSource) Len() int { return s.n }

// opened reads the segmentation's cells, so that a source that cannot is refused when opened.
func (s *cellSource) opened() (*cellSource, error) {
	if s.instrument {
		t, err := s.cellTable()
		if err != nil {
			return nil, err
		}
		s.n = t.n
		return s, nil
	}
	cells, err := s.segCells()
	if err != nil {
		return nil, err
	}
	s.n = len(cells)
	return s, nil
}

// centroids are the cells' centroids (the instrument's as the cells table holds them).
func (s *cellSource) centroids() ([]float32, []float32, error) {
	if s.instrument {
		t, err := s.cellTable()
		if err != nil {
			return nil, nil, err
		}
		return t.x, t.y, nil
	}
	cells, err := s.segCells()
	if err != nil {
		return nil, nil, err
	}
	x, y := make([]float32, len(cells)), make([]float32, len(cells))
	for i, c := range cells {
		x[i], y[i] = c.X, c.Y
	}
	return x, y, nil
}

// cellIDs are the cells' ids.
func (s *cellSource) cellIDs() ([]string, error) {
	if s.instrument {
		t, err := s.cellTable()
		if err != nil {
			return nil, err
		}
		return t.ids, nil
	}
	cells, err := s.segCells()
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(cells))
	for i, c := range cells {
		ids[i] = c.ID
	}
	return ids, nil
}

// attribute is a QC attribute's values (NaN where a cell has none), computed as
// lib/cell-attributes.js computes it.
func (s *cellSource) attribute(id string) ([]float64, error) {
	ratio := func(a, b float64) float64 {
		if b > 0 && !math.IsNaN(a) && !math.IsInf(a, 0) {
			return a / b
		}
		return math.NaN()
	}
	if s.instrument {
		t, err := s.cellTable()
		if err != nil {
			return nil, err
		}
		out := make([]float64, t.n)
		for i := range out {
			switch id {
			case "transcripts":
				out[i] = float64(t.transcripts[i])
			case "genes":
				out[i] = float64(t.genes[i])
			case "total":
				out[i] = float64(t.total[i])
			case "controls":
				out[i] = float64(t.controlProbes[i]) + float64(t.controlCodewords[i])
			case "area":
				out[i] = float64(t.area[i])
			case "nucleusArea":
				out[i] = float64(t.nucleusArea[i])
			case "nucleusShare":
				out[i] = ratio(float64(t.nucleusArea[i]), float64(t.area[i]))
			case "density":
				out[i] = ratio(100*float64(t.transcripts[i]), float64(t.area[i]))
			case "nuclearShare":
				out[i] = ratio(float64(t.nuclear[i]), float64(t.transcripts[i]))
			case "method":
				out[i] = math.NaN()
				if t.method[i] != xenium.NoMethod {
					out[i] = float64(t.method[i])
				}
			}
		}
		return out, nil
	}
	cells, err := s.segCells()
	if err != nil {
		return nil, err
	}
	out := make([]float64, len(cells))
	for i, c := range cells {
		switch id {
		case "transcripts":
			out[i] = float64(c.Transcripts)
		case "genes":
			out[i] = float64(c.Genes)
		case "total":
			out[i] = float64(c.Assigned)
		case "area":
			out[i] = float64(c.Area)
		case "nucleusArea":
			out[i] = float64(c.NucleusArea)
		case "nucleusShare":
			out[i] = ratio(float64(c.NucleusArea), float64(c.Area))
		case "density":
			out[i] = ratio(100*float64(c.Transcripts), float64(c.Area))
		default:
			return nil, fmt.Errorf("%s is the instrument's segmentation's alone (%s is %s's)", id, s.seg.Name, s.seg.ID)
		}
	}
	return out, nil
}

// geneCounts are each cell's Q20 transcripts of a gene, from the segmentation's counts.
func (s *cellSource) geneCounts(name string) ([]float64, string, error) {
	if s.counts == nil {
		dir, err := s.folder()
		if err != nil {
			return nil, "", err
		}
		if s.counts, err = segment.ReadCounts(dir); err != nil {
			return nil, "", err
		}
	}
	c := s.counts
	g := -1
	for i, gene := range c.Genes {
		if gene == name {
			g = i
			break
		}
	}
	if g < 0 {
		for i, gene := range c.Genes {
			if strings.EqualFold(gene, name) {
				g = i
				break
			}
		}
	}
	if g < 0 {
		return nil, "", fmt.Errorf("the panel has no gene %q", name)
	}
	n := len(c.Offsets) - 1
	out := make([]float64, s.Len())
	for i := 0; i < n && i < len(out); i++ {
		for k := c.Offsets[i]; k < c.Offsets[i+1]; k++ {
			if int(c.Gene[k]) == g {
				out[i] += float64(c.Count[k])
			}
		}
	}
	return out, c.Genes[g], nil
}

func (s *cellSource) artifactDir(id, kind string) (artifacts.Manifest, string, error) {
	m, err := s.a.artifacts.Get(id)
	if err != nil {
		return m, "", fmt.Errorf("no %s %s in the library", kind, id)
	}
	return m, filepath.Join(s.a.artifacts.Dir(), id), nil
}

// labelCodes are a label set's label at a level for each cell (−1 for none), as the page reads them
// (lib/labels.js labelsPerCell), and the labels' names.
func (s *cellSource) labelCodes(id string, level int, levelName string) ([]int32, []string, error) {
	m, dir, err := s.artifactDir(id, "label set")
	if err != nil {
		return nil, nil, err
	}
	if m.Kind != "labels" {
		return nil, nil, fmt.Errorf("%s is a %s, not a label set", id, m.Kind)
	}
	d, _, lab, err := label.Read(dir)
	if err != nil {
		return nil, nil, err
	}
	if d.Segmentation.ID != s.seg.ID {
		return nil, nil, fmt.Errorf("%s labels the cells of another segmentation (%s), not %s", id, d.Segmentation.ID, s.seg.ID)
	}
	if levelName != "" {
		level = -1
		for l, name := range d.Levels {
			if strings.EqualFold(name, levelName) {
				level = l
			}
		}
		if level < 0 {
			return nil, nil, fmt.Errorf("%s has no level %q; its levels are %s", id, levelName, strings.Join(d.Levels, ", "))
		}
	}
	if level < 0 || level >= len(lab.Label) {
		return nil, nil, fmt.Errorf("%s has %d levels (0 to %d)", id, len(lab.Label), len(lab.Label)-1)
	}
	out := make([]int32, s.Len())
	for i := range out {
		out[i] = -1
	}
	for i, row := range lab.Cells {
		if int(row) < len(out) {
			out[row] = lab.Label[level][i]
		}
	}
	names := make([]string, len(d.Sets))
	for k, set := range d.Sets {
		names[k] = set.Name
	}
	return out, names, nil
}

// clusterCodes are a clustering's cluster for each cell (−1 for one not clustered), and the
// clusters' names.
func (s *cellSource) clusterCodes(id string) ([]int32, []string, error) {
	m, dir, err := s.artifactDir(id, "clustering")
	if err != nil {
		return nil, nil, err
	}
	if m.Kind != "clustering" && m.Kind != "analysis" {
		return nil, nil, fmt.Errorf("%s is a %s, not a clustering", id, m.Kind)
	}
	d, err := cluster.ReadDescription(dir)
	if err != nil {
		return nil, nil, err
	}
	if d.Segmentation.ID != s.seg.ID {
		return nil, nil, fmt.Errorf("%s clusters the cells of another segmentation (%s), not %s", id, d.Segmentation.ID, s.seg.ID)
	}
	rows, clusters, err := cluster.ReadCells(dir)
	if err != nil {
		return nil, nil, err
	}
	out := make([]int32, s.Len())
	for i := range out {
		out[i] = -1
	}
	for i, row := range rows {
		if int(row) < len(out) {
			out[row] = int32(clusters[i])
		}
	}
	return out, s.qc.clusterNamesOf(id, d.Clusters.Count), nil
}

// nicheCodes are a niches analysis's niche for each cell (−1 for none), and the niches' names.
func (s *cellSource) nicheCodes(id string) ([]int32, []string, error) {
	m, dir, err := s.artifactDir(id, "niches analysis")
	if err != nil {
		return nil, nil, err
	}
	a, err := spatial.ReadAnalysis(dir)
	if m.Kind != "spatial" || err != nil || a.Niches == nil {
		return nil, nil, fmt.Errorf("%s is not a niches analysis", id)
	}
	if a.Segmentation.ID != s.seg.ID {
		return nil, nil, fmt.Errorf("%s found the niches of another segmentation (%s), not %s", id, a.Segmentation.ID, s.seg.ID)
	}
	cells, err := spatial.ReadAnalyzed(dir)
	if err != nil {
		return nil, nil, err
	}
	out := make([]int32, s.Len())
	for i := range out {
		out[i] = -1
	}
	for i, row := range cells.Rows {
		if int(row) < len(out) && cells.Stratum[i] > 0 {
			out[row] = cells.Stratum[i] - 1
		}
	}
	names := make([]string, a.Niches.Niches)
	for k := range names {
		names[k] = fmt.Sprintf("Niche %d", k+1)
	}
	return out, names, nil
}

// window is a region's window (its latest version).
func (s *cellSource) window(ref string) (*spatial.Window, error) {
	if w, ok := s.windows[ref]; ok {
		return w, nil
	}
	_, geometry, err := s.qc.region(s.dataset, ref)
	if err != nil {
		return nil, err
	}
	w, err := spatial.PolygonWindow(geometry)
	if err != nil {
		return nil, fmt.Errorf("the region %s: %v", ref, err)
	}
	if s.windows == nil {
		s.windows = map[string]*spatial.Window{}
	}
	s.windows[ref] = w
	return w, nil
}

// codesMatch keeps the cells whose code the condition names.
func codesMatch(c *query.Condition, codes []int32, names []string, base int, what string) ([]bool, error) {
	keepCode, err := c.Codes(names, base, what)
	if err != nil {
		return nil, err
	}
	out := make([]bool, len(codes))
	for i, k := range codes {
		out[i] = k >= 0 && int(k) < len(keepCode) && keepCode[k]
	}
	return out, nil
}

// placeMatch answers a region, distance or box condition for points.
func placeMatch(c *query.Condition, x, y []float32, window func(string) (*spatial.Window, error)) ([]bool, error) {
	out := make([]bool, len(x))
	switch c.Kind {
	case "box":
		for i := range x {
			px, py := float64(x[i]), float64(y[i])
			out[i] = px >= c.Box[0] && px < c.Box[2] && py >= c.Box[1] && py < c.Box[3]
		}
		return out, nil
	case "region":
		w, err := window(c.Name)
		if err != nil {
			return nil, err
		}
		for i := range x {
			out[i] = w.Contains(float64(x[i]), float64(y[i]))
		}
		return out, nil
	}
	w, err := window(c.Name)
	if err != nil {
		return nil, err
	}
	limit := 1.0
	for _, b := range []*float64{c.Min, c.Max} {
		if b != nil {
			limit = math.Max(limit, math.Abs(*b)+1)
		}
	}
	values := make([]float64, len(x))
	for i := range x {
		px, py := float64(x[i]), float64(y[i])
		d := w.DistanceWithin(px, py, limit)
		if w.Contains(px, py) {
			d = -d
		}
		values[i] = d
	}
	return c.InRange(values), nil
}

// Match answers one condition for every cell.
func (s *cellSource) Match(c *query.Condition) ([]bool, error) {
	switch c.Kind {
	case "attribute":
		values, err := s.attribute(c.Name)
		if err != nil {
			return nil, err
		}
		if c.Name == "method" {
			t, err := s.cellTable()
			if err != nil {
				return nil, err
			}
			codes := make([]int32, len(values))
			for i, v := range values {
				codes[i] = -1
				if !math.IsNaN(v) {
					codes[i] = int32(v)
				}
			}
			return codesMatch(c, codes, t.methods, 0, "the instrument's segmentation methods")
		}
		for i, v := range values {
			if math.IsInf(v, 0) {
				values[i] = math.NaN() // a filter's test: a finite value (lib/filters.js passes)
			}
		}
		return c.InRange(values), nil
	case "gene":
		values, _, err := s.geneCounts(c.Name)
		if err != nil {
			return nil, err
		}
		return c.InRange(values), nil
	case "labels":
		codes, names, err := s.labelCodes(c.Name, c.Level, c.LevelName)
		if err != nil {
			return nil, err
		}
		return codesMatch(c, codes, names, 0, "the label set "+c.Name)
	case "clusters":
		codes, names, err := s.clusterCodes(c.Name)
		if err != nil {
			return nil, err
		}
		return codesMatch(c, codes, names, 1, "the clustering "+c.Name)
	case "niches":
		codes, names, err := s.nicheCodes(c.Name)
		if err != nil {
			return nil, err
		}
		return codesMatch(c, codes, names, 1, "the niches "+c.Name)
	case "region", "distance", "box":
		x, y, err := s.centroids()
		if err != nil {
			return nil, err
		}
		return placeMatch(c, x, y, s.window)
	case "filter":
		if !s.instrument {
			return nil, fmt.Errorf("a saved filter keeps cells of the instrument's segmentation, not of %s", s.seg.Name)
		}
		if s.depth >= 4 {
			return nil, errors.New("filters name filters too deeply (four at most)")
		}
		f, err := s.qc.filter(s.dataset, c.Name)
		if err != nil {
			return nil, err
		}
		tree, err := filterTree(f)
		if err != nil {
			return nil, err
		}
		s.depth++
		defer func() { s.depth-- }()
		return tree.Eval(s)
	}
	return nil, fmt.Errorf("a %s condition is not one of cells", c.Kind)
}

// --- Where a question is asked -------------------------------------------------------------------

// queryTarget is a dataset of a project, open, and the segmentation asked about.
type queryTarget struct {
	project *libraryProject
	dataset string // its id in the project
	name    string
	path    string
	b       *xenium.Bundle
	source  *cellSource
}

func (t *queryTarget) Close() {
	if t.b != nil {
		t.b.Close()
	}
}

// queryTargetOf finds a library project's dataset (by name or id; its only one if not given), opens
// its bundle and the segmentation asked about (by id or name, "instrument", or the dataset's
// default: the one a decision promoted, else the instrument's).
func (a *app) queryTargetOf(ctx context.Context, args map[string]any) (*queryTarget, error) {
	if a.cache == nil {
		return nil, errors.New("SituCraft runs without a cache folder")
	}
	projects, err := a.libraryProjects()
	if err != nil {
		return nil, err
	}
	ref, _ := args["project"].(string)
	p, err := findProject(projects, ref)
	if err != nil {
		return nil, err
	}
	want, _ := args["dataset"].(string)
	want = strings.TrimSpace(want)
	var names []string
	idx := -1
	for i, d := range p.Datasets {
		names = append(names, d.Name)
		if d.ID == want || strings.EqualFold(d.Name, want) {
			idx = i
		}
	}
	if want == "" && len(p.Datasets) == 1 {
		idx = 0
	}
	if idx < 0 {
		if len(p.Datasets) == 0 {
			return nil, fmt.Errorf("%s has no dataset", p.Name)
		}
		if want == "" {
			return nil, fmt.Errorf("%s has %d datasets: name one (%s)", p.Name, len(p.Datasets), strings.Join(names, ", "))
		}
		return nil, fmt.Errorf("%s has no dataset %q; its datasets are %s", p.Name, want, strings.Join(names, ", "))
	}
	d := p.Datasets[idx]
	b, err := xenium.Open(d.Path, xenium.Options{CacheDir: a.cacheDir})
	if err != nil {
		return nil, fmt.Errorf("%s's bundle (%s): %v", d.Name, d.Path, err)
	}
	t := &queryTarget{project: p, dataset: d.ID, name: d.Name, path: d.Path, b: b}
	segRef, _ := args["segmentation"].(string)
	src, err := a.cellSourceOf(ctx, b, d.Path, d.ID, segRef, p, contextOf(p))
	if err != nil {
		t.Close()
		return nil, err
	}
	t.source = src
	return t, nil
}

// cellSourceOf opens the segmentation asked about of a dataset's bundle.
func (a *app) cellSourceOf(ctx context.Context, b *xenium.Bundle, path, dataset, segRef string, p *libraryProject, qc *queryContext) (*cellSource, error) {
	segRef = strings.TrimSpace(segRef)
	if segRef == "" || strings.EqualFold(segRef, "default") {
		segRef = "instrument"
		if p != nil {
			for _, d := range p.Decisions {
				if d.Kind == "promote-segmentation" && d.Dataset == dataset && d.Artifact != "" {
					segRef = d.Artifact
				}
			}
		}
	}
	instrument := a.knownSegmentation(b)
	src := &cellSource{a: a, ctx: ctx, b: b, path: path, dataset: dataset, qc: qc}
	if instrument != nil && (strings.EqualFold(segRef, "instrument") || segRef == instrument.ID) {
		src.seg, src.instrument = *instrument, true
		return src.opened()
	}
	if strings.EqualFold(segRef, "instrument") {
		return nil, errors.New("the instrument's segmentation of the dataset is not registered yet: open the dataset in SituCraft once")
	}
	m, err := a.artifacts.Get(segRef)
	if err != nil && p != nil {
		var names []string
		for _, raw := range p.Artifacts {
			var r projectRecord
			if json.Unmarshal(raw, &r) == nil && r.Kind == "segmentation" && r.Dataset == dataset {
				if strings.EqualFold(r.Name, segRef) {
					m, err = a.artifacts.Get(r.ID)
				}
				names = append(names, r.Name)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("no segmentation %q of the dataset; its segmentations are instrument, %s", segRef, strings.Join(names, ", "))
		}
	}
	if err != nil || m.Kind != "segmentation" {
		return nil, fmt.Errorf("no segmentation %q", segRef)
	}
	src.seg = m
	return src.opened()
}

// --- The actions ----------------------------------------------------------------------------------

// treeArg reads a tree argument (an object, or JSON text of one).
func treeArg(args map[string]any, key string, of query.Of) (*query.Node, error) {
	value, ok := args[key]
	if !ok || value == nil {
		return nil, fmt.Errorf("%s is required: a tree of conditions (all, any, not)", key)
	}
	var raw []byte
	if text, isText := value.(string); isText {
		raw = []byte(text)
	} else {
		raw, _ = json.Marshal(value)
	}
	return query.Parse(raw, of)
}

func intArg(args map[string]any, key string, fallback, most int) int {
	v, ok := args[key].(float64)
	if !ok {
		return fallback
	}
	if int(v) <= 0 {
		return most
	}
	return min(int(v), most)
}

// composition is how the cells a tree keeps divide among a label set's labels, a clustering's
// clusters or a niches analysis's niches, against all the segmentation's cells.
func (s *cellSource) composition(by string, level int, keep []bool) (map[string]any, error) {
	m, err := s.a.artifacts.Get(by)
	if err != nil {
		return nil, fmt.Errorf("no result %s to divide the cells by", by)
	}
	var codes []int32
	var names []string
	switch m.Kind {
	case "labels":
		codes, names, err = s.labelCodes(by, level, "")
	case "clustering", "analysis":
		codes, names, err = s.clusterCodes(by)
	case "spatial":
		codes, names, err = s.nicheCodes(by)
	default:
		return nil, fmt.Errorf("the cells are divided by a label set, a clustering or a niches analysis, not a %s", m.Kind)
	}
	if err != nil {
		return nil, err
	}
	kept, all := make([]int, len(names)+1), make([]int, len(names)+1)
	nKept := 0
	for i, k := range codes {
		slot := len(names)
		if k >= 0 && int(k) < len(names) {
			slot = int(k)
		}
		all[slot]++
		if keep[i] {
			kept[slot]++
			nKept++
		}
	}
	rows := [][]any{}
	for k := 0; k <= len(names); k++ {
		if all[k] == 0 {
			continue
		}
		name := "none"
		if k < len(names) {
			name = names[k]
		}
		shareKept, shareAll := ratioOrNil(float64(kept[k]), float64(nKept)), ratioOrNil(float64(all[k]), float64(len(codes)))
		var enrichment any
		if sk, ok := shareKept.(float64); ok {
			if sa, ok := shareAll.(float64); ok && sa > 0 {
				enrichment = sk / sa
			}
		}
		rows = append(rows, []any{name, kept[k], shareKept, all[k], shareAll, enrichment})
	}
	return map[string]any{"by": map[string]string{"id": m.ID, "kind": m.Kind, "name": m.Name}, "columns": []string{"name", "kept", "share", "all", "shareAll", "ratio"}, "rows": rows}, nil
}

func ratioOrNil(a, b float64) any {
	if b > 0 {
		return a / b
	}
	return nil
}

func (a *app) queryCellsAction(ctx context.Context, args map[string]any) (remoteResult, error) {
	tree, err := treeArg(args, "where", query.Cells)
	if err != nil {
		return remoteResult{}, err
	}
	t, err := a.queryTargetOf(ctx, args)
	if err != nil {
		return remoteResult{}, err
	}
	defer t.Close()
	s := t.source
	keep, err := tree.Eval(s)
	if err != nil {
		return remoteResult{}, err
	}
	kept := query.Count(keep)
	data := map[string]any{
		"project":      map[string]string{"id": t.project.ID, "name": t.project.Name},
		"dataset":      map[string]string{"id": t.dataset, "name": t.name},
		"segmentation": map[string]string{"id": s.seg.ID, "name": s.seg.Name},
		"where":        tree.Text(),
		"cells":        len(keep),
		"kept":         kept,
		"checksum":     query.Checksum(keep),
	}
	message := fmt.Sprintf("%d of %d cells of %s (%s) kept, those %s", kept, len(keep), t.name, s.seg.Name, tree.Text())
	if by, _ := args["by"].(string); strings.TrimSpace(by) != "" {
		level := 0
		if v, ok := args["level"].(float64); ok {
			level = int(v)
		}
		comp, err := s.composition(strings.TrimSpace(by), level, keep)
		if err != nil {
			return remoteResult{}, err
		}
		data["composition"] = comp
	}
	// A page of the cells kept: each one's row, id and centroid, and the columns asked for.
	limit := intArg(args, "limit", 20, resultRowsMost)
	// An offset of 0 is the first cell (intArg's 0 is the most, a limit's).
	offset := 0
	if v, ok := args["offset"].(float64); ok {
		offset = int(min(max(0, v), math.MaxInt32))
	}
	columns := []string{"row", "id", "x", "y"}
	var extra [][]float64
	if list, ok := args["columns"].([]any); ok {
		for _, item := range list {
			name, _ := item.(string)
			var values []float64
			known := false
			for _, attr := range query.Attributes {
				known = known || attr == name
			}
			if known {
				values, err = s.attribute(name)
			} else {
				values, name, err = s.geneCounts(name)
			}
			if err != nil {
				return remoteResult{}, err
			}
			columns = append(columns, name)
			extra = append(extra, values)
		}
	}
	ids, err := s.cellIDs()
	if err != nil {
		return remoteResult{}, err
	}
	x, y, err := s.centroids()
	if err != nil {
		return remoteResult{}, err
	}
	rows := [][]any{}
	seen := 0
	for i, k := range keep {
		if !k {
			continue
		}
		if seen >= offset && len(rows) < limit {
			row := []any{i, ids[i], float64(x[i]), float64(y[i])}
			for _, values := range extra {
				row = append(row, number(values[i]))
			}
			rows = append(rows, row)
		}
		seen++
	}
	data["rows"] = map[string]any{"columns": columns, "offset": offset, "rows": rows, "total": kept}
	message += fmt.Sprintf("; the cells' checksum %s…", query.Checksum(keep)[:12])
	if s.instrument {
		message += ". save_filter with this tree (where) saves them as a filter that analyses can use"
	}
	return answer(message+".", data)
}

func (a *app) summarizeRegionAction(ctx context.Context, args map[string]any) (remoteResult, error) {
	t, err := a.queryTargetOf(ctx, args)
	if err != nil {
		return remoteResult{}, err
	}
	defer t.Close()
	s := t.source
	// The place: a region by name or id, or a box.
	var where *query.Condition
	var area float64
	var placeName string
	if ref, _ := args["region"].(string); strings.TrimSpace(ref) != "" {
		w, err := s.window(strings.TrimSpace(ref))
		if err != nil {
			return remoteResult{}, err
		}
		name, _, _ := s.qc.region(s.dataset, strings.TrimSpace(ref))
		where, area, placeName = &query.Condition{Kind: "region", Name: strings.TrimSpace(ref)}, w.Area, name
	} else if list, ok := args["box"].([]any); ok && len(list) == 4 {
		var box [4]float64
		for i, v := range list {
			f, ok := v.(float64)
			if !ok {
				return remoteResult{}, errors.New("box is [x0, y0, x1, y1] in µm")
			}
			box[i] = f
		}
		if !(box[2] > box[0] && box[3] > box[1]) {
			return remoteResult{}, errors.New("a box's x1 and y1 are above its x0 and y0")
		}
		where, area = &query.Condition{Kind: "box", Box: box}, (box[2]-box[0])*(box[3]-box[1])
		placeName = fmt.Sprintf("[%g, %g]–[%g, %g] µm", box[0], box[1], box[2], box[3])
	} else {
		return remoteResult{}, errors.New("name a region (region), or give a box [x0, y0, x1, y1] in µm")
	}
	inside, err := s.Match(where)
	if err != nil {
		return remoteResult{}, err
	}
	n := query.Count(inside)
	data := map[string]any{
		"project":      map[string]string{"id": t.project.ID, "name": t.project.Name},
		"dataset":      map[string]string{"id": t.dataset, "name": t.name},
		"segmentation": map[string]string{"id": s.seg.ID, "name": s.seg.Name},
		"place":        placeName,
		"area":         area,
		"cells":        n,
		"density":      ratioOrNil(float64(n), area/1e6),
		"allCells":     len(inside),
	}
	message := fmt.Sprintf("%s of %s (%s): %.0f µm², %d cells by centroid", placeName, t.name, s.seg.Name, area, n)
	if area > 0 {
		message += fmt.Sprintf(" (%.0f per mm²)", float64(n)/(area/1e6))
	}
	// Its labels: the label set asked for, else the latest of the segmentation in the project.
	labels, _ := args["labels"].(string)
	if labels == "" {
		for _, raw := range t.project.Artifacts {
			var r projectRecord
			if json.Unmarshal(raw, &r) != nil || r.Kind != "labels" || r.State != "complete" {
				continue
			}
			if m, err := a.artifacts.Get(r.ID); err == nil {
				if d, err := label.ReadDescription(filepath.Join(a.artifacts.Dir(), r.ID)); err == nil && d.Segmentation.ID == s.seg.ID {
					labels = m.ID
				}
			}
		}
	}
	if labels != "" {
		level := 0
		if v, ok := args["level"].(float64); ok {
			level = int(v)
		}
		comp, err := s.composition(labels, level, inside)
		if err != nil {
			return remoteResult{}, err
		}
		// Densities: a label's cells inside per mm² of the region.
		if area > 0 {
			comp["columns"] = append(comp["columns"].([]string), "density")
			for i, row := range comp["rows"].([][]any) {
				comp["rows"].([][]any)[i] = append(row, float64(row[1].(int))/(area/1e6))
			}
		}
		data["labels"] = comp
		message += fmt.Sprintf("; its labels by %s", comp["by"].(map[string]string)["name"])
	}
	// Its genes: the region's cells' transcripts of each gene, against the cells outside.
	top := intArg(args, "genes", 10, 200)
	if s.counts == nil {
		dir, err := s.folder()
		if err != nil {
			return remoteResult{}, err
		}
		if s.counts, err = segment.ReadCounts(dir); err != nil {
			return remoteResult{}, err
		}
	}
	c := s.counts
	in, out := make([]float64, len(c.Genes)), make([]float64, len(c.Genes))
	nIn, nOut := 0, 0
	for i := 0; i+1 < len(c.Offsets) && i < len(inside); i++ {
		sum := out
		if inside[i] {
			sum = in
			nIn++
		} else {
			nOut++
		}
		for k := c.Offsets[i]; k < c.Offsets[i+1]; k++ {
			sum[c.Gene[k]] += float64(c.Count[k])
		}
	}
	type geneRow struct {
		gene                   string
		count, meanIn, meanOut float64
	}
	var genes []geneRow
	for g, name := range c.Genes {
		r := geneRow{gene: name, count: in[g]}
		if nIn > 0 {
			r.meanIn = in[g] / float64(nIn)
		}
		if nOut > 0 {
			r.meanOut = out[g] / float64(nOut)
		}
		genes = append(genes, r)
	}
	table := func(rows []geneRow) [][]any {
		out := [][]any{}
		for _, r := range rows {
			var ratio any
			if r.meanOut > 0 {
				ratio = r.meanIn / r.meanOut
			}
			out = append(out, []any{r.gene, r.count, r.meanIn, r.meanOut, ratio})
		}
		return out
	}
	sort.SliceStable(genes, func(i, j int) bool { return genes[i].count > genes[j].count })
	abundant := table(genes[:min(top, len(genes))])
	// Enriched: the most above the rest, among genes with ten or more transcripts inside.
	var candidates []geneRow
	for _, r := range genes {
		if r.count >= 10 && r.meanOut > 0 {
			candidates = append(candidates, r)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].meanIn/candidates[i].meanOut > candidates[j].meanIn/candidates[j].meanOut
	})
	enriched := table(candidates[:min(top, len(candidates))])
	columns := []string{"gene", "transcripts", "meanInside", "meanOutside", "ratio"}
	data["genes"] = map[string]any{"columns": columns, "abundant": abundant, "enriched": enriched, "cellsInside": nIn, "cellsOutside": nOut}
	if len(enriched) > 0 {
		var names []string
		for _, r := range enriched[:min(3, len(enriched))] {
			names = append(names, r[0].(string))
		}
		message += "; most above the rest of the tissue: " + strings.Join(names, ", ")
	}
	return answer(message+".", data)
}

// --- The page's filters ---------------------------------------------------------------------------

func (a *app) registerQueries(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/query", a.queryCellsForPage)
}

// queryRequest is a tree the page asks about: of a dataset's instrument cells (a filter's), with
// the project's regions, filters and cluster names as the page holds them.
type queryRequest struct {
	Dataset string          `json:"dataset"`
	Where   json.RawMessage `json:"where"`
	Context queryContext    `json:"context"`
}

// queryCellsForPage answers which of the instrument's cells a tree keeps: their count, the SHA-256
// of which (lib/filters.js keepChecksum), the tree described, and the bytes themselves (keepBytes,
// base64) for the page to use as a filter's.
func (a *app) queryCellsForPage(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var ask queryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&ask); err != nil {
		writeError(w, http.StatusBadRequest, "Send {\"dataset\", \"where\": a tree of conditions, \"context\": {regions, filters, clusterNames}}.")
		return
	}
	tree, err := query.Parse(ask.Where, query.Cells)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "query.refused", err.Error())
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	s, err := a.cellSourceOf(r.Context(), b, b.Path, ask.Dataset, "instrument", nil, &ask.Context)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "query.refused", err.Error())
		return
	}
	keep, err := tree.Eval(s)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "query.refused", err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"cells":  len(keep),
		"kept":   query.Count(keep),
		"sha256": query.Checksum(keep),
		"text":   tree.Text(),
		"keep":   base64.StdEncoding.EncodeToString(query.KeepBytes(keep)),
	})
}

// --- Transcripts -----------------------------------------------------------------------------------

// transcriptSource answers a tree of transcripts over those of a rectangle (view.RegionPoints).
type transcriptSource struct {
	cells   *cellSource
	gene    []uint16
	names   []string // the index's genes
	x, y    []float32
	cell    []uint32
	inCells map[*query.Node][]bool
}

func (t *transcriptSource) Len() int { return len(t.x) }

func (t *transcriptSource) Match(c *query.Condition) ([]bool, error) {
	out := make([]bool, len(t.x))
	switch c.Kind {
	case "gene":
		want := map[int]bool{}
		for _, name := range c.Genes {
			found := false
			for g, n := range t.names {
				if strings.EqualFold(n, name) {
					want[g], found = true, true
				}
			}
			if !found {
				return nil, fmt.Errorf("the panel has no gene %q", name)
			}
		}
		for i, g := range t.gene {
			out[i] = want[int(g)]
		}
		return out, nil
	case "assigned":
		for i, cell := range t.cell {
			out[i] = (cell != cache.NoCell) == c.Assigned
		}
		return out, nil
	case "cells":
		keep, ok := t.inCells[c.Cells]
		if !ok {
			var err error
			if keep, err = c.Cells.Eval(t.cells); err != nil {
				return nil, err
			}
			t.inCells[c.Cells] = keep
		}
		for i, cell := range t.cell {
			out[i] = cell != cache.NoCell && int(cell) < len(keep) && keep[cell]
		}
		return out, nil
	case "region", "distance", "box":
		return placeMatch(c, t.x, t.y, t.cells.window)
	}
	return nil, fmt.Errorf("a %s condition is not one of transcripts", c.Kind)
}

// treeBounds is a rectangle a tree's transcripts lie within, when its conditions bound them (a
// region, a distance's maximum from one, a box; all of them, or any when each is bounded).
func treeBounds(n *query.Node, window func(string) (*spatial.Window, error)) (view.Rect, bool) {
	switch {
	case n.Cond != nil:
		c := n.Cond
		switch c.Kind {
		case "box":
			return view.Rect{X0: c.Box[0], Y0: c.Box[1], X1: c.Box[2], Y1: c.Box[3]}, true
		case "region", "distance":
			w, err := window(c.Name)
			if err != nil {
				return view.Rect{}, false
			}
			x0, y0, x1, y1 := w.Bounds()
			grow := 0.0
			if c.Kind == "distance" {
				if c.Max == nil {
					return view.Rect{}, false
				}
				grow = math.Max(0, *c.Max)
			}
			return view.Rect{X0: x0 - grow, Y0: y0 - grow, X1: x1 + grow, Y1: y1 + grow}, true
		}
		return view.Rect{}, false
	case len(n.All) > 0:
		var r view.Rect
		found := false
		for _, child := range n.All {
			if b, ok := treeBounds(child, window); ok {
				if !found {
					r, found = b, true
				} else {
					r = view.Rect{X0: math.Max(r.X0, b.X0), Y0: math.Max(r.Y0, b.Y0), X1: math.Min(r.X1, b.X1), Y1: math.Min(r.Y1, b.Y1)}
				}
			}
		}
		if found && (r.X1 < r.X0 || r.Y1 < r.Y0) {
			r = view.Rect{X0: r.X0, Y0: r.Y0, X1: r.X0, Y1: r.Y0}
		}
		return r, found
	case len(n.Any) > 0:
		var r view.Rect
		for i, child := range n.Any {
			b, ok := treeBounds(child, window)
			if !ok {
				return view.Rect{}, false
			}
			if i == 0 {
				r = b
			} else {
				r = view.Rect{X0: math.Min(r.X0, b.X0), Y0: math.Min(r.Y0, b.Y0), X1: math.Max(r.X1, b.X1), Y1: math.Max(r.Y1, b.Y1)}
			}
		}
		return r, true
	}
	return view.Rect{}, false
}

func (a *app) queryTranscriptsAction(ctx context.Context, args map[string]any) (remoteResult, error) {
	tree, err := treeArg(args, "where", query.Transcripts)
	if err != nil {
		return remoteResult{}, err
	}
	minQ := 20.0
	if v, ok := args["min_quality"].(float64); ok {
		minQ = v
	}
	args = cloneArgs(args)
	args["segmentation"] = "instrument" // transcripts are assigned to the instrument's cells
	t, err := a.queryTargetOf(ctx, args)
	if err != nil {
		return remoteResult{}, err
	}
	defer t.Close()
	d, err := a.openView(t.b, t.path)
	if errors.Is(err, view.ErrNotReady) {
		return remoteResult{}, errors.New("the dataset's derived data are not built yet (open it in SituCraft, or wait for them)")
	}
	if err != nil {
		return remoteResult{}, err
	}
	rect, bounded := treeBounds(tree, t.source.window)
	if !bounded {
		rect = view.Rect{X0: 0, Y0: 0, X1: float64(d.Grid.Width) * d.Grid.PixelSize, Y1: float64(d.Grid.Height) * d.Grid.PixelSize}
	}
	pts, err := d.RegionPoints(rect, nil, float32(minQ))
	if err != nil {
		return remoteResult{}, fmt.Errorf("the transcripts there: %v (bound the tree with a region or a box)", err)
	}
	src := &transcriptSource{cells: t.source, x: pts.X, y: pts.Y, cell: pts.Cell, inCells: map[*query.Node][]bool{}}
	src.gene = make([]uint16, len(pts.X))
	for k, g := range pts.Genes {
		for i := pts.Starts[k]; i < pts.Starts[k+1]; i++ {
			src.gene[i] = g
		}
	}
	for _, g := range d.Transcripts.Summary.Genes {
		src.names = append(src.names, g.Name)
	}
	keep, err := tree.Eval(src)
	if err != nil {
		return remoteResult{}, err
	}
	kept := query.Count(keep)
	byGene := map[uint16]int{}
	for i, k := range keep {
		if k {
			byGene[src.gene[i]]++
		}
	}
	type geneCount struct {
		gene  string
		count int
	}
	var genes []geneCount
	for g, n := range byGene {
		name := fmt.Sprintf("gene %d", g)
		if int(g) < len(src.names) {
			name = src.names[g]
		}
		genes = append(genes, geneCount{name, n})
	}
	sort.Slice(genes, func(i, j int) bool {
		if genes[i].count != genes[j].count {
			return genes[i].count > genes[j].count
		}
		return genes[i].gene < genes[j].gene
	})
	limit := intArg(args, "limit", 20, 10000)
	rows := [][]any{}
	for _, g := range genes[:min(limit, len(genes))] {
		rows = append(rows, []any{g.gene, g.count, ratioOrNil(float64(g.count), float64(kept))})
	}
	data := map[string]any{
		"project":  map[string]string{"id": t.project.ID, "name": t.project.Name},
		"dataset":  map[string]string{"id": t.dataset, "name": t.name},
		"where":    tree.Text(),
		"quality":  minQ,
		"kept":     kept,
		"searched": len(keep),
		"bounds":   []float64{rect.X0, rect.Y0, rect.X1, rect.Y1},
		"genes":    map[string]any{"columns": []string{"gene", "transcripts", "share"}, "rows": rows, "total": len(genes)},
	}
	var lead []string
	for _, g := range genes[:min(3, len(genes))] {
		lead = append(lead, fmt.Sprintf("%s %d", g.gene, g.count))
	}
	message := fmt.Sprintf("%d transcripts of quality %g or more of %s kept, those %s, of %s", kept, minQ, t.name, tree.Text(), plural(len(genes), "gene"))
	if len(lead) > 0 {
		message += " (" + strings.Join(lead, ", ") + ")"
	}
	return answer(message+".", data)
}

func cloneArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	return out
}
