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
// read once and what is per cell kept while it is drawn (the last few). What is per transcript is
// streamed, never held whole: it is written once into the cache by the dataset's point tiles
// (segpoints), each tile's read when it is asked for. A drawn segmentation holds a few dozen
// bytes a cell, none a transcript.

import (
	"bufio"
	"cmp"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
	points           *segPoints // its transcripts by tile
	hasProb          bool       // its transcripts have probabilities
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
	if old, ok := s.byID[v.id]; ok {
		old.points.close()
	} else {
		s.order = append(s.order, v.id)
	}
	s.byID[v.id] = v
	for len(s.order) > maxSegViews {
		s.byID[s.order[0]].points.close()
		delete(s.byID, s.order[0])
		s.order = s.order[1:]
	}
}

// segPoints is a segmentation's transcripts by the dataset's point tiles (a file in the cache,
// "SCSP"): a header (magic, version, tiles), each tile's offset (u64, tiles + 1), then each tile's
// points in its order, every one's cell (u32: its index in the cells table, NoCell for none) and
// probability of it (f32, NaN when none).
type segPoints struct {
	f       *os.File
	offsets []uint64
	across  int
}

func (p *segPoints) close() {
	if p != nil {
		p.f.Close()
	}
}

// tile reads a tile's points' cells and probabilities.
func (p *segPoints) tile(tx, ty int) ([]byte, error) {
	k := ty*p.across + tx
	if tx < 0 || tx >= p.across || k+1 >= len(p.offsets) {
		return nil, errors.New("no such tile")
	}
	buf := make([]byte, p.offsets[k+1]-p.offsets[k])
	_, err := p.f.ReadAt(buf, int64(p.offsets[k]))
	return buf, err
}

// segPointsBatch is the most transcripts writeSegPoints places in one pass over a
// segmentation's columns (16 bytes each while it does), so that memory is bounded whatever the
// tissue's size.
var segPointsBatch = 1 << 22

// writeSegPoints writes the points file of the segmentation in dir (its assignments and, when it
// has them, probabilities) by the dataset's point tiles. The tiles are taken in batches: a batch's
// transcripts are sorted by row and picked out of the columns streamed from the start.
func writeSegPoints(path string, d *view.Dataset, dir string, hasProb bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	across, down := d.Grid.Across(0), d.Grid.Down(0)
	tiles := across * down
	offsets := make([]uint64, tiles+1)
	header := 16 + 8*(tiles+1)
	w := bufio.NewWriterSize(f, 1<<20)
	if _, err := w.Write(make([]byte, header)); err != nil {
		return err
	}
	at := uint64(header)
	var keys []uint64 // each transcript's row << 32 | its place in the batch
	var picked []byte
	for k0 := 0; k0 < tiles; {
		keys = keys[:0]
		k := k0
		for ; k < tiles && (k == k0 || len(keys) < segPointsBatch); k++ {
			offsets[k] = at + 8*uint64(len(keys))
			pts, err := d.TilePoints(k%across, k/across, nil, 0)
			if err != nil {
				return err
			}
			for _, row := range pts.Row {
				keys = append(keys, uint64(row)<<32|uint64(len(keys)))
			}
		}
		slices.Sort(keys)
		if cap(picked) < 8*len(keys) {
			picked = make([]byte, 8*len(keys))
		}
		picked = picked[:8*len(keys)]
		if err := pickSegPoints(dir, hasProb, keys, picked); err != nil {
			return err
		}
		if _, err := w.Write(picked); err != nil {
			return err
		}
		at += uint64(len(picked))
		k0 = k
	}
	offsets[tiles] = at
	if err := w.Flush(); err != nil {
		return err
	}
	head := []byte("SCSP")
	head = binary.LittleEndian.AppendUint32(head, 1)
	head = binary.LittleEndian.AppendUint32(head, uint32(tiles))
	head = binary.LittleEndian.AppendUint32(head, 0)
	for _, o := range offsets {
		head = binary.LittleEndian.AppendUint64(head, o)
	}
	if _, err := f.WriteAt(head, 0); err != nil {
		return err
	}
	return f.Close()
}

// pickSegPoints puts each of a batch's transcripts (keys sorted by row) in its place in out: its
// cell (NoCell for none) and its probability (NaN for none), read from the columns streamed up to
// the batch's last row.
func pickSegPoints(dir string, hasProb bool, keys []uint64, out []byte) error {
	le := binary.LittleEndian
	nan := math.Float32bits(float32(math.NaN()))
	for i := range len(keys) {
		le.PutUint32(out[8*i:], cache.NoCell)
		le.PutUint32(out[8*i+4:], nan)
	}
	j := 0
	return streamColumns(dir, hasProb, func(start int, assign, prob []uint32) bool {
		end := uint64(start + len(assign))
		for ; j < len(keys) && keys[j]>>32 < end; j++ {
			i, slot := int(keys[j]>>32)-start, keys[j]&0xffffffff
			if a := assign[i]; a > 0 {
				le.PutUint32(out[8*slot:], a-1)
			}
			if prob != nil {
				le.PutUint32(out[8*slot+4:], prob[i])
			}
		}
		return j < len(keys)
	})
}

// columnBlock is the values streamColumns reads at a time.
var columnBlock = 1 << 20

// streamColumns streams a segmentation's assignments and, when asked, probabilities together, a
// block at a time, while each returns true.
func streamColumns(dir string, withProb bool, each func(start int, assign, prob []uint32) bool) error {
	a, err := segment.OpenColumn(dir, segment.FileAssignments)
	if err != nil {
		return err
	}
	defer a.Close()
	var p *segment.Column
	if withProb {
		if p, err = segment.OpenColumn(dir, segment.FileProbabilities); err != nil {
			return err
		}
		defer p.Close()
		if p.Len() != a.Len() {
			return errors.New("its probabilities do not match its assignments")
		}
	}
	assign := make([]uint32, columnBlock)
	var prob []uint32
	if p != nil {
		prob = make([]uint32, columnBlock)
	}
	for start := 0; ; {
		k, err := a.Next(assign)
		if err != nil {
			return err
		}
		if p != nil {
			if j, err := p.Next(prob); err != nil || j != k {
				return cmp.Or(err, errors.New("its probabilities do not match its assignments"))
			}
		}
		if k == 0 {
			return nil
		}
		var pk []uint32
		if p != nil {
			pk = prob[:k]
		}
		if !each(start, assign[:k], pk) {
			return nil
		}
		start += k
	}
}

// openSegPoints opens a points file for a dataset's tiles.
func openSegPoints(path string, d *view.Dataset) (*segPoints, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 16)
	if _, err := io.ReadFull(f, head); err != nil || string(head[:4]) != "SCSP" || binary.LittleEndian.Uint32(head[4:]) != 1 {
		f.Close()
		return nil, errors.New("not a segmentation's points file")
	}
	tiles := int(binary.LittleEndian.Uint32(head[8:]))
	if tiles != d.Grid.Across(0)*d.Grid.Down(0) {
		f.Close()
		return nil, fmt.Errorf("a points file of %d tiles for %d", tiles, d.Grid.Across(0)*d.Grid.Down(0))
	}
	raw := make([]byte, 8*(tiles+1))
	if _, err := io.ReadFull(f, raw); err != nil {
		f.Close()
		return nil, err
	}
	p := &segPoints{f: f, across: d.Grid.Across(0), offsets: make([]uint64, tiles+1)}
	for k := range p.offsets {
		p.offsets[k] = binary.LittleEndian.Uint64(raw[8*k:])
	}
	return p, nil
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
	if v.counts, err = segment.ReadCounts(dir); err != nil {
		return nil, err
	}
	optional := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	v.hasProb = optional(segment.FileProbabilities)
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
	if v.hasProb {
		// Streamed, as the points file is written: a drawn segmentation holds nothing a transcript.
		sum, count := make([]float64, n), make([]int, n)
		err := streamColumns(dir, true, func(_ int, assign, prob []uint32) bool {
			for i, c := range assign {
				if p := math.Float32frombits(prob[i]); c > 0 && int(c) <= n && !math.IsNaN(float64(p)) {
					sum[c-1] += float64(p)
					count[c-1]++
				}
			}
			return true
		})
		if err != nil {
			return nil, err
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
	// Its transcripts by tile, written once (the dataset's transcript index decides the tiles).
	pdir, err := a.cache.Derived("segpoints", m.ID+"\n"+d.Entries[cache.KindTranscripts], func(into string) error {
		return writeSegPoints(filepath.Join(into, "points.bin"), d, dir, v.hasProb)
	})
	if err != nil {
		return nil, err
	}
	if v.points, err = openSegPoints(filepath.Join(pdir, "points.bin"), d); err != nil {
		return nil, err
	}
	return v, nil
}

// encodeTable writes the cells table (internal/view SegmentationTable).
func (v *segView) encodeTable() []byte {
	n := len(v.cells)
	nan := float32(math.NaN())
	t := view.SegmentationTable{HasLimitations: v.flags != nil, HasCertainty: v.certainty != nil, HasProbability: v.hasProb,
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
	block, err := v.points.tile(t[0], t[1])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if v.hasProb {
		pts.Probability = make([]float32, len(pts.Raw))
	}
	for i, raw := range pts.Raw {
		if int(raw)*8+8 > len(block) {
			writeError(w, http.StatusInternalServerError, "the segmentation's points file does not match the dataset's tiles")
			return
		}
		pts.Cell[i] = binary.LittleEndian.Uint32(block[8*raw:])
		if v.hasProb {
			pts.Probability[i] = math.Float32frombits(binary.LittleEndian.Uint32(block[8*raw+4:]))
		}
	}
	writeBinary(w, pts.Encode(), false)
}
