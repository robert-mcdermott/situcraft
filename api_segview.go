package main

// Any segmentation on the tissue (ui/mode-viewer.js; wave 3, slice 8). Its outlines are tiled as
// the comparison workspace tiles them (api_workspace.go); here are the rest of what the Viewer
// draws of it:
//
//	GET …/segmentations/{id}/cells             its cells table ("SCVS", below), for fills
//	GET …/segmentations/{id}/cells/{cell}?top= a cell's detail (JSON), for the inspector
//	GET …/segmentations/{id}/points/{tx}/{ty}  a transcript tile ("SCVP") with each transcript's
//	                                           cell in the segmentation, and its probability of
//	                                           it when the segmentation gives one
//
// The instrument's segmentation is the dataset's own (api_view.go). A segmentation's files are
// read once and kept while it is drawn (the last few).

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"situcraft/internal/artifacts"
	"situcraft/internal/cache"
	"situcraft/internal/segment"
	"situcraft/internal/segment/refine"
	"situcraft/internal/view"
)

// Limitation flags, by bit.
const (
	flagFewTranscripts = 1 << iota
	flagUncertain
	flagNoNucleus
)

// segView is what the Viewer draws of a segmentation, read from its files.
type segView struct {
	id, name, method string
	cells            []segment.Cell
	outlines         []segment.Outline
	assign           []uint32  // each transcript's cell (k: cells[k−1]; 0 none)
	prob             []float32 // each transcript's probability of its cell; nil when not given
	counts           *segment.Counts
	certainty        []float32 // each cell's boundary certainty; nil when not given
	flags            []uint8   // each cell's limitations; nil without diagnostics
	meanProb         []float32 // each cell's transcripts' mean probability; nil when not given
	instrument       []int32   // the instrument's row of the cell its nucleus lies in (−1 none)
	geneIndex        []int     // each counted gene's index among the dataset's (−1 none)
	table            []byte
}

// segViews keeps the last few segmentations read.
type segViews struct {
	mu    sync.Mutex
	load  sync.Mutex // held while one is read
	byID  map[string]*segView
	order []string
}

const maxSegViews = 3

func (s *segViews) get(id string) *segView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id]
}

func (s *segViews) put(v *segView) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID == nil {
		s.byID = map[string]*segView{}
	}
	if _, ok := s.byID[v.id]; !ok {
		s.order = append(s.order, v.id)
	}
	s.byID[v.id] = v
	for len(s.order) > maxSegViews {
		delete(s.byID, s.order[0])
		s.order = s.order[1:]
	}
}

func (a *app) registerSegView(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/{index}/segmentations/{id}/cells", a.segViewCells)
	mux.HandleFunc("GET /api/local/{index}/segmentations/{id}/cells/{cell}", a.segViewCell)
	mux.HandleFunc("GET /api/local/{index}/segmentations/{id}/points/{tx}/{ty}", a.segViewPoints)
}

// segViewOf reads (or finds read) the request's segmentation of the request's dataset; the
// instrument's is the dataset's own, answered by api_view.go.
func (a *app) segViewOf(w http.ResponseWriter, r *http.Request) (*view.Dataset, *segView, bool) {
	d, ok := a.dataset(w, r)
	if !ok {
		return nil, nil, false
	}
	id := r.PathValue("id")
	if v := a.segViews.get(id); v != nil {
		return d, v, true
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return nil, nil, false
	}
	uuid := b.Experiment.AnalysisUUID
	b.Close()
	m, err := a.segmentationOf(id, uuid)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return nil, nil, false
	}
	if isBundleOwn(m) {
		writeError(w, http.StatusBadRequest, "The instrument's segmentation is the dataset's own: its cells are answered with the dataset's.")
		return nil, nil, false
	}
	a.segViews.load.Lock()
	defer a.segViews.load.Unlock()
	if v := a.segViews.get(id); v != nil {
		return d, v, true
	}
	v, err := a.readSegView(d, m)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return nil, nil, false
	}
	a.segViews.put(v)
	return d, v, true
}

func (a *app) readSegView(d *view.Dataset, m artifacts.Manifest) (*segView, error) {
	p, err := a.artifacts.Path(m.ID, segment.FileCells)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(p)
	v := &segView{id: m.ID, name: m.Name, method: m.Lineage.Method.Name}
	if v.cells, err = segment.ReadCells(dir); err != nil {
		return nil, err
	}
	if v.outlines, err = segment.ReadOutlines(dir); err != nil {
		return nil, err
	}
	if v.assign, err = segment.ReadAssignments(dir); err != nil {
		return nil, err
	}
	if v.counts, err = segment.ReadCounts(dir); err != nil {
		return nil, err
	}
	optional := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	if optional(segment.FileProbabilities) {
		if v.prob, err = segment.ReadProbabilities(dir); err != nil {
			return nil, err
		}
		if len(v.prob) != len(v.assign) {
			return nil, errors.New("its probabilities do not match its assignments")
		}
	}
	if optional(segment.FileCertainty) {
		if v.certainty, err = segment.ReadCertainty(dir); err != nil {
			return nil, err
		}
	}
	if optional(segment.FileDiagnostics) {
		raw, err := os.ReadFile(filepath.Join(dir, segment.FileDiagnostics))
		if err != nil {
			return nil, err
		}
		var diag refine.Diagnostics
		if err := json.Unmarshal(raw, &diag); err != nil {
			return nil, err
		}
		index := make(map[string]int, len(v.cells))
		for i, c := range v.cells {
			index[c.ID] = i
		}
		v.flags = make([]uint8, len(v.cells))
		for _, l := range []struct {
			ids  []string
			flag uint8
		}{{diag.Limitations.LowTranscripts, flagFewTranscripts}, {diag.Limitations.Uncertain, flagUncertain}, {diag.Limitations.NoNucleus, flagNoNucleus}} {
			for _, id := range l.ids {
				if i, ok := index[id]; ok {
					v.flags[i] |= l.flag
				}
			}
		}
	}
	n := len(v.cells)
	if v.prob != nil {
		sum, count := make([]float64, n), make([]int, n)
		for row, c := range v.assign {
			if c > 0 && int(c) <= n && !math.IsNaN(float64(v.prob[row])) {
				sum[c-1] += float64(v.prob[row])
				count[c-1]++
			}
		}
		v.meanProb = make([]float32, n)
		for i := range v.meanProb {
			v.meanProb[i] = float32(sum[i] / float64(count[i])) // 0/0: NaN
		}
	}
	v.instrument = make([]int32, n)
	for i, c := range v.cells {
		v.instrument[i] = -1
		if row, ok := d.CellRow(c.Nucleus); c.Nucleus != "" && ok {
			v.instrument[i] = int32(row)
		}
	}
	genes := map[string]int{}
	if d.Transcripts != nil {
		for i, g := range d.Transcripts.Summary.Genes {
			genes[g.Name] = i
		}
	}
	v.geneIndex = make([]int, len(v.counts.Genes))
	for i, name := range v.counts.Genes {
		v.geneIndex[i] = -1
		if g, ok := genes[name]; ok {
			v.geneIndex[i] = g
		}
	}
	v.table = v.encodeTable()
	return v, nil
}

// encodeTable writes the cells table (internal/view SegmentationTable).
func (v *segView) encodeTable() []byte {
	n := len(v.cells)
	nan := float32(math.NaN())
	t := view.SegmentationTable{HasLimitations: v.flags != nil, HasCertainty: v.certainty != nil, HasProbability: v.prob != nil,
		X: make([]float32, n), Y: make([]float32, n), Area: make([]float32, n), NucleusArea: make([]float32, n),
		Transcripts: make([]uint32, n), Assigned: make([]uint32, n), Genes: make([]uint32, n), Instrument: make([]uint32, n),
		Certainty: make([]float32, n), Probability: make([]float32, n), Limitations: make([]uint8, n)}
	for i, c := range v.cells {
		t.X[i], t.Y[i], t.Area[i], t.NucleusArea[i] = c.X, c.Y, c.Area, c.NucleusArea
		t.Transcripts[i], t.Assigned[i], t.Genes[i] = c.Transcripts, c.Assigned, c.Genes
		t.Instrument[i] = cache.NoCell
		if v.instrument[i] >= 0 {
			t.Instrument[i] = uint32(v.instrument[i])
		}
		t.Certainty[i], t.Probability[i] = nan, nan
		if v.certainty != nil && i < len(v.certainty) {
			t.Certainty[i] = v.certainty[i]
		}
		if v.meanProb != nil {
			t.Probability[i] = v.meanProb[i]
		}
	}
	copy(t.Limitations, v.flags)
	return t.Encode()
}

func (a *app) segViewCells(w http.ResponseWriter, r *http.Request) {
	_, v, ok := a.segViewOf(w, r)
	if !ok {
		return
	}
	writeBinary(w, v.table, false)
}

// segCellDetail is a segmentation's cell, for the inspector.
type segCellDetail struct {
	Row          int    `json:"row"`
	ID           string `json:"id"`
	Segmentation struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Method string `json:"method"`
	} `json:"segmentation"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Area        float64 `json:"area"`
	NucleusArea float64 `json:"nucleusArea"`
	// Instrument is the instrument's cell its nucleus lies in (null when none).
	Instrument *struct {
		ID  string `json:"id"`
		Row int    `json:"row"`
	} `json:"instrument"`
	// Its gene transcripts of quality 20 or more, every transcript assigned to it, and its genes
	// detected; its top genes (gene: the dataset's index, −1 for one the dataset does not have).
	Transcripts   int              `json:"transcripts"`
	Assigned      int              `json:"assigned"`
	GenesDetected int              `json:"genesDetected"`
	Genes         []view.GeneCount `json:"genes"`
	// Its limitations (few transcripts, uncertain boundary, no nucleus) when the method flags
	// them; its boundary certainty and its transcripts' mean probability when it gives them.
	Limitations       []string `json:"limitations"`
	BoundaryCertainty *float64 `json:"boundaryCertainty"`
	MeanProbability   *float64 `json:"meanProbability"`
	// Its outline (x, y interleaved, µm), and its nucleus's (the instrument's).
	Boundaries [][]float32 `json:"boundaries"`
	Nuclei     [][]float32 `json:"nuclei"`
}

func (a *app) segViewCell(w http.ResponseWriter, r *http.Request) {
	d, v, ok := a.segViewOf(w, r)
	if !ok {
		return
	}
	// A cell by its index, or by its id as "id:<cell id>".
	name := r.PathValue("cell")
	row, err := strconv.Atoi(name)
	if id, byID := strings.CutPrefix(name, "id:"); byID {
		row, err = -1, nil
		for i, c := range v.cells {
			if c.ID == id {
				row = i
				break
			}
		}
	}
	if err != nil || row < 0 || row >= len(v.cells) {
		writeError(w, http.StatusNotFound, "No such cell in this segmentation.")
		return
	}
	top := 12
	if s := r.URL.Query().Get("top"); s != "" {
		if top, err = strconv.Atoi(s); err != nil || top < 0 || top > 10000 {
			writeError(w, http.StatusBadRequest, "top must be a number of genes.")
			return
		}
	}
	c := v.cells[row]
	out := segCellDetail{Row: row, ID: c.ID, X: float64(c.X), Y: float64(c.Y), Area: float64(c.Area), NucleusArea: float64(c.NucleusArea),
		Transcripts: int(c.Transcripts), Assigned: int(c.Assigned), GenesDetected: int(c.Genes),
		Genes: []view.GeneCount{}, Limitations: []string{}, Boundaries: [][]float32{}, Nuclei: [][]float32{}}
	out.Segmentation.ID, out.Segmentation.Name, out.Segmentation.Method = v.id, v.name, v.method
	if ir := v.instrument[row]; ir >= 0 {
		out.Instrument = &struct {
			ID  string `json:"id"`
			Row int    `json:"row"`
		}{d.Cells.IDs[ir], int(ir)}
	}
	cs := v.counts
	if row+1 < len(cs.Offsets) {
		var genes []view.GeneCount
		for k := cs.Offsets[row]; k < cs.Offsets[row+1]; k++ {
			g := cs.Gene[k]
			genes = append(genes, view.GeneCount{Gene: v.geneIndex[g], Name: cs.Genes[g], Count: int(cs.Count[k])})
		}
		sort.SliceStable(genes, func(i, j int) bool { return genes[i].Count > genes[j].Count })
		out.Genes = append(out.Genes, genes[:min(top, len(genes))]...)
	}
	if v.flags != nil {
		for _, l := range []struct {
			flag uint8
			text string
		}{{flagFewTranscripts, "few transcripts"}, {flagUncertain, "uncertain boundary"}, {flagNoNucleus, "no nucleus"}} {
			if v.flags[row]&l.flag != 0 {
				out.Limitations = append(out.Limitations, l.text)
			}
		}
	}
	finite := func(x float32) *float64 {
		if math.IsNaN(float64(x)) {
			return nil
		}
		f := float64(x)
		return &f
	}
	if v.certainty != nil {
		out.BoundaryCertainty = finite(v.certainty[row])
	}
	if v.meanProb != nil {
		out.MeanProbability = finite(v.meanProb[row])
	}
	if o := v.outlines[row]; len(o.X) >= 3 {
		ring := make([]float32, 0, 2*len(o.X))
		for i := range o.X {
			ring = append(ring, o.X[i], o.Y[i])
		}
		out.Boundaries = append(out.Boundaries, ring)
	}
	if ir := v.instrument[row]; ir >= 0 {
		if cell, err := d.Cell(int(ir), 0); err == nil {
			out.Nuclei = cell.Nuclei
		}
	}
	writeJSON(w, out)
}

// segViewPoints answers a transcript tile as the dataset's (api_view.go), each transcript's cell
// the segmentation's (its index in the cells table) and, when the segmentation gives them, its
// probability of it.
func (a *app) segViewPoints(w http.ResponseWriter, r *http.Request) {
	d, v, ok := a.segViewOf(w, r)
	if !ok {
		return
	}
	t, ok := ints(w, r, "tx", "ty")
	if !ok {
		return
	}
	genes, minQ, ok := pointParams(w, r)
	if !ok {
		return
	}
	pts, err := d.TilePoints(t[0], t[1], genes, minQ)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if v.prob != nil {
		pts.Probability = make([]float32, len(pts.Row))
	}
	for i, row := range pts.Row {
		pts.Cell[i] = cache.NoCell
		if int(row) < len(v.assign) {
			if c := v.assign[row]; c > 0 {
				pts.Cell[i] = c - 1
			}
			if v.prob != nil {
				pts.Probability[i] = v.prob[row]
			}
		}
	}
	writeBinary(w, pts.Encode(), false)
}
