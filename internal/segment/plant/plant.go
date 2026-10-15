// Package plant makes a segmentation with known errors planted in another: neighbouring cells of
// unlike expression merged into one, large cells split in two, cells missed, and cells drawn too
// wide, taking their neighbours' pixels. It is simulated, for practice: comparing it with the
// segmentation it came from, a reviewer finds the errors, and the answer key (planted.json) says
// what was planted where. The same segmentation and seed give the same errors.
//
// Each error's cells, and the cells around them, are kept apart from every other error's, so that
// each is one difference of a comparison. Every other cell keeps its pixels, outline and
// transcripts; the transcripts of the cells an error changed go to the cell holding their pixel
// afterwards, and every cell's counts are made again (gene transcripts of quality 20 or more).
package plant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"

	"situcraft/internal/compare"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// Method names the planted segmentation's method, MethodVersion its version.
const (
	Method        = "SituCraft planted errors"
	MethodVersion = "1"
	FileTruth     = "planted.json"
)

// Params are how many errors of each kind are planted, how far a grown cell reaches, and the seed.
type Params struct {
	Merges int     `json:"merges"`
	Splits int     `json:"splits"`
	Missed int     `json:"missed"`
	Grown  int     `json:"grown"`
	GrowUm float64 `json:"grow_um"`
	Seed   uint64  `json:"seed"`
}

// DefaultParams are the example's.
func DefaultParams() Params {
	return Params{Merges: 20, Splits: 20, Missed: 10, Grown: 10, GrowUm: 1, Seed: 20261014}
}

// The kinds of errors.
const (
	Merge  = "merge"
	Split  = "split"
	Missed = "missed"
	Grown  = "grown"
)

// Error is one planted error: its kind, the source's cells it changed, the planted segmentation's
// cells they became, and where it lies (µm).
type Error struct {
	Kind  string   `json:"kind"`
	Cells []string `json:"cells"`
	Into  []string `json:"into"`
	X     float32  `json:"x"`
	Y     float32  `json:"y"`
	// Note says what makes it an error ("unlike expression: 0.91", "neighbours: 4").
	Note string `json:"note"`
}

// Truth is planted.json: the answer key.
type Truth struct {
	Format string  `json:"format"`
	Source string  `json:"source"` // the segmentation planted in
	Params Params  `json:"params"`
	Errors []Error `json:"errors"`
}

// Transcripts gives the dataset's transcripts in their row order.
type Transcripts interface {
	Transcripts(fn func(*xenium.TranscriptBatch) error) error
}

// Plant reads the segmentation in src, plants errors and writes the result into dir, with its
// answer key. source names the segmentation planted in (for the key).
func Plant(ctx context.Context, src, source string, ds Transcripts, dir string, p Params, progress func(float64, string)) (*segment.Description, *Truth, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	if p.Merges < 0 || p.Splits < 0 || p.Missed < 0 || p.Grown < 0 || !(p.GrowUm >= 0 && p.GrowUm <= 20) {
		return nil, nil, errors.New("the numbers of errors are 0 or more, and a grown cell reaches 0 to 20 µm")
	}
	progress(0, "Reading the segmentation")
	desc, err := segment.ReadDescription(src)
	if err != nil {
		return nil, nil, err
	}
	cells, err := segment.ReadCells(src)
	if err != nil {
		return nil, nil, err
	}
	r, err := segment.ReadMask(src)
	if err != nil {
		return nil, nil, err
	}
	assign, err := segment.ReadAssignments(src)
	if err != nil {
		return nil, nil, err
	}
	counts, err := segment.ReadCounts(src)
	if err != nil {
		return nil, nil, err
	}
	outlines, err := segment.ReadOutlines(src)
	if err != nil {
		return nil, nil, err
	}
	n := len(cells)
	if len(counts.Offsets) != n+1 || len(outlines) != n {
		return nil, nil, errors.New("the segmentation's files disagree")
	}
	stats := r.Stats(n)
	pl := &planter{r: r, n: n, cells: cells, counts: counts, stats: stats, rng: rand.New(rand.NewPCG(p.Seed, 0x5175)),
		used: make([]bool, n+1), touched: make([]bool, n+1), edits: map[int]map[int]uint32{}}
	progress(0.05, "Finding neighbouring cells")
	pl.neighbours()
	truth := &Truth{Format: "situcraft-planted-errors", Source: source, Params: p}
	progress(0.15, "Planting the errors")
	truth.Errors = append(truth.Errors, pl.merges(p.Merges)...)
	truth.Errors = append(truth.Errors, pl.splits(p.Splits)...)
	truth.Errors = append(truth.Errors, pl.missed(p.Missed)...)
	truth.Errors = append(truth.Errors, pl.grow(p.Grown, p.GrowUm/r.PixelSize)...)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	// The planted raster: each source label (and each split's new part) to its planted label.
	progress(0.3, "Drawing the planted segmentation")
	total := n + len(pl.parts)
	relabel := make([]uint32, total+1)
	var out []segment.Cell
	from := make([]int, 0, total) // each planted cell's source cell (-1 for a split's new part)
	for i := 1; i <= n; i++ {
		switch {
		case pl.removed[i]:
		case pl.mergedInto[i] != 0:
		default:
			out = append(out, cells[i-1])
			from = append(from, i-1)
			relabel[i] = uint32(len(out))
		}
	}
	for i := 1; i <= n; i++ {
		if a := pl.mergedInto[i]; a != 0 {
			relabel[i] = relabel[a]
		}
	}
	for k, part := range pl.parts {
		c := segment.Cell{ID: cells[part.of-1].ID + "-split"}
		out = append(out, c)
		from = append(from, -1)
		relabel[n+1+k] = uint32(len(out))
	}
	planted := &segment.Raster{Width: r.Width, Height: r.Height, PixelSize: r.PixelSize, Rows: make([][]segment.Run, r.Height)}
	row := make([]uint32, r.Width)
	for y := 0; y < r.Height; y++ {
		r.Row(y, row)
		for x, l := range pl.edits[y] {
			row[x] = l
		}
		for x, l := range row {
			row[x] = relabel[l]
		}
		planted.Rows[y] = segment.EncodeRow(row)
	}
	// The planted cells' names in the answer key.
	name := func(sourceLabel uint32) string {
		if l := relabel[sourceLabel]; l > 0 {
			return out[l-1].ID
		}
		return ""
	}
	for i := range truth.Errors {
		e := &truth.Errors[i]
		seen := map[string]bool{}
		for _, l := range pl.errorLabels[i] {
			if id := name(l); id != "" && !seen[id] {
				seen[id] = true
				e.Into = append(e.Into, id)
			}
		}
		if e.Into == nil {
			e.Into = []string{}
		}
	}

	// Each planted cell's place and area.
	m := len(out)
	pstats := planted.Stats(m)
	px2 := r.PixelSize * r.PixelSize
	changed := make([]bool, m+1) // planted labels whose pixels changed
	for i := 1; i <= total; i++ {
		if i <= n && !pl.touched[i] {
			continue
		}
		if l := relabel[i]; l > 0 {
			changed[l] = true
		}
	}
	for l := 1; l <= m; l++ {
		if !changed[l] {
			continue
		}
		c := &out[l-1]
		s := pstats[l-1]
		c.Area, c.X, c.Y = 0, 0, 0
		if s.Pixels > 0 {
			c.Area = float32(float64(s.Pixels) * px2)
			c.X, c.Y = float32(s.SumX/float64(s.Pixels)*r.PixelSize), float32(s.SumY/float64(s.Pixels)*r.PixelSize)
		}
		if from[l-1] < 0 {
			c.NucleusArea = 0
		}
	}

	// Transcripts: a changed cell's to the cell holding their pixel now; every other's stay.
	progress(0.5, "Assigning the transcripts")
	genes := map[string]uint32{}
	for i, g := range counts.Genes {
		genes[g] = uint32(i)
	}
	newAssign := make([]uint32, len(assign))
	pairs := map[uint64]uint32{}
	for i := range out {
		out[i].Transcripts, out[i].Assigned, out[i].Genes = 0, 0, 0
	}
	var q20, q20Assigned int64
	at := 0
	err = ds.Transcripts(func(b *xenium.TranscriptBatch) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i := 0; i < b.Rows; i++ {
			if at+i >= len(assign) {
				return fmt.Errorf("the dataset has more transcripts than the segmentation assigns (%d)", len(assign))
			}
			old := assign[at+i]
			var l uint32
			switch {
			case old != 0 && !pl.touched[old]:
				l = relabel[old]
			default:
				// In a changed cell, or in none: the cell holding its pixel, if that changed.
				if here := planted.AtMicrons(float64(b.X[i]), float64(b.Y[i])); here > 0 && (changed[here] || old != 0) {
					l = here
				}
			}
			newAssign[at+i] = l
			if l > 0 {
				out[l-1].Assigned++
			}
			gene := string(b.Feature[i])
			if b.QV[i] < 20 || xenium.FeatureKind(gene) != xenium.KindGene {
				continue
			}
			q20++
			g, ok := genes[gene]
			if !ok || l == 0 {
				continue
			}
			pairs[uint64(l-1)<<32|uint64(g)]++
			out[l-1].Transcripts++
			q20Assigned++
		}
		at += b.Rows
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if at != len(assign) {
		return nil, nil, fmt.Errorf("the dataset has %d transcripts; the segmentation assigns %d", at, len(assign))
	}
	newCounts := countsOf(pairs, counts.Genes, m)
	for i := range out {
		out[i].Genes = newCounts.Offsets[i+1] - newCounts.Offsets[i]
	}

	// Outlines: a changed cell's traced again, every other's kept.
	progress(0.75, "Tracing the changed cells' outlines")
	newOutlines := make([]segment.Outline, m)
	for l := 1; l <= m; l++ {
		if changed[l] || from[l-1] < 0 {
			newOutlines[l-1] = planted.TraceOutline(uint32(l), pstats[l-1], 0.5)
		} else {
			newOutlines[l-1] = outlines[from[l-1]]
		}
	}

	progress(0.9, "Writing the planted segmentation")
	params, _ := json.Marshal(p)
	d := segment.Description{Method: Method + " " + MethodVersion, Parameters: params, Width: r.Width, Height: r.Height, PixelSize: r.PixelSize,
		Cells: m, Transcripts: len(newAssign), Genes: len(newCounts.Genes), Q20GeneTranscripts: q20, Q20GeneAssigned: q20Assigned}
	d.Nuclei = desc.Nuclei
	for _, write := range []func() error{
		func() error { return segment.WriteCells(dir, out) },
		func() error { return segment.WriteMask(dir, planted) },
		func() error { return segment.WriteAssignments(dir, newAssign) },
		func() error { return segment.WriteCounts(dir, newCounts) },
		func() error { return segment.WriteOutlines(dir, newOutlines) },
		func() error { return segment.WriteDescription(dir, d) },
		func() error { return WriteTruth(dir, truth) },
	} {
		if err := write(); err != nil {
			return nil, nil, err
		}
	}
	progress(1, "Written")
	return &d, truth, nil
}

// WriteTruth writes planted.json.
func WriteTruth(dir string, t *Truth) error {
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileTruth), append(raw, '\n'), 0o644)
}

// ReadTruth reads planted.json.
func ReadTruth(dir string) (*Truth, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileTruth))
	if err != nil {
		return nil, err
	}
	var t Truth
	if err := json.Unmarshal(raw, &t); err != nil || t.Format != "situcraft-planted-errors" {
		return nil, fmt.Errorf("%s is not an answer key of planted errors", FileTruth)
	}
	return &t, nil
}

func countsOf(pairs map[uint64]uint32, genes []string, n int) *segment.Counts {
	keys := make([]uint64, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool { return keys[a] < keys[b] })
	c := &segment.Counts{Genes: genes, Offsets: make([]uint32, n+1)}
	for _, k := range keys {
		cell := int(k >> 32)
		c.Offsets[cell+1]++
		c.Gene = append(c.Gene, uint32(k))
		c.Count = append(c.Count, pairs[k])
	}
	for i := 1; i <= n; i++ {
		c.Offsets[i] += c.Offsets[i-1]
	}
	return c
}

// planter keeps what the planting has done so far, in the source's labels (1..n; a split's new
// part n+1+k).
type planter struct {
	r       *segment.Raster
	n       int
	cells   []segment.Cell
	counts  *segment.Counts
	stats   []segment.LabelStats
	rng     *rand.Rand
	contact map[[2]uint32]int // pixel edges shared by two cells (lower label first)
	around  [][]uint32        // each label's neighbours
	used    []bool            // in an error, or next to one
	touched []bool            // pixels changed
	// edits are pixels given a new label (source labels, or n+1+k for split part k), by row.
	edits       map[int]map[int]uint32
	removed     map[int]bool
	mergedInto  map[int]int
	parts       []part
	errorLabels [][]uint32 // each error's source labels, for the key
}

type part struct{ of int }

func (pl *planter) neighbours() {
	pl.contact = map[[2]uint32]int{}
	pl.removed, pl.mergedInto = map[int]bool{}, map[int]int{}
	prev := make([]uint32, pl.r.Width)
	row := make([]uint32, pl.r.Width)
	add := func(a, b uint32) {
		if a == b || a == 0 || b == 0 {
			return
		}
		if a > b {
			a, b = b, a
		}
		pl.contact[[2]uint32{a, b}]++
	}
	for y := 0; y < pl.r.Height; y++ {
		pl.r.Row(y, row)
		for x := 1; x < len(row); x++ {
			add(row[x-1], row[x])
		}
		if y > 0 {
			for x := range row {
				add(prev[x], row[x])
			}
		}
		prev, row = row, prev
	}
	pl.around = make([][]uint32, pl.n+1)
	keys := make([][2]uint32, 0, len(pl.contact))
	for k := range pl.contact {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i][0] < keys[j][0] || keys[i][0] == keys[j][0] && keys[i][1] < keys[j][1]
	})
	for _, k := range keys {
		pl.around[k[0]] = append(pl.around[k[0]], k[1])
		pl.around[k[1]] = append(pl.around[k[1]], k[0])
	}
}

// free: a cell and its neighbours are in no error yet.
func (pl *planter) free(labels ...uint32) bool {
	for _, l := range labels {
		if pl.used[l] {
			return false
		}
		for _, o := range pl.around[l] {
			if pl.used[o] {
				return false
			}
		}
	}
	return true
}

func (pl *planter) take(labels ...uint32) {
	for _, l := range labels {
		pl.used[l] = true
		for _, o := range pl.around[l] {
			pl.used[o] = true
		}
	}
}

// entry is one gene's share of a cell's unit-length counts.
type entry struct {
	gene uint32
	v    float64
}

// profile is a cell's counts as a unit vector, by gene in ascending order. Its sums, like every
// floating-point sum here, add products each rounded on its own (never fused) in a fixed order,
// so the same cells are chosen on every processor.
func (pl *planter) profile(l uint32) []entry {
	c := pl.counts
	var out []entry
	var norm float64
	for k := c.Offsets[l-1]; k < c.Offsets[l]; k++ {
		v := float64(c.Count[k])
		out = append(out, entry{c.Gene[k], v})
		norm += float64(v * v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].gene < out[j].gene })
	norm = math.Sqrt(norm)
	for i := range out {
		out[i].v /= norm
	}
	return out
}

// dot is two profiles' inner product.
func dot(a, b []entry) float64 {
	var sum float64
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i].gene < b[j].gene:
			i++
		case a[i].gene > b[j].gene:
			j++
		default:
			sum += float64(a[i].v * b[j].v)
			i, j = i+1, j+1
		}
	}
	return sum
}

func (pl *planter) q20(l uint32) uint32 { return pl.cells[l-1].Transcripts }

func (pl *planter) shuffle(v []uint32) {
	pl.rng.Shuffle(len(v), func(i, j int) { v[i], v[j] = v[j], v[i] })
}

func (pl *planter) centre(labels ...uint32) (float32, float32) {
	var sx, sy, n float64
	for _, l := range labels {
		s := pl.stats[l-1]
		sx, sy, n = sx+s.SumX, sy+s.SumY, n+float64(s.Pixels)
	}
	if n == 0 {
		return 0, 0
	}
	return float32(sx / n * pl.r.PixelSize), float32(sy / n * pl.r.PixelSize)
}

// merges joins neighbours of unlike expression, of the pairs sharing an edge of 8 pixels or more,
// each with 30 gene transcripts or more: first those whose merged cell would express markers of
// two lineages that neither does alone (compare.LineagePairs, as the comparison's contamination
// counts them), then the quarter least alike in their expression; each in a seeded order.
func (pl *planter) merges(k int) []Error {
	type pair struct {
		a, b   uint32
		unlike float64
		mixed  [][2]string
	}
	keys := make([][2]uint32, 0, len(pl.contact))
	for key, edge := range pl.contact {
		if edge >= 8 && pl.q20(key[0]) >= 30 && pl.q20(key[1]) >= 30 {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i][0] < keys[j][0] || keys[i][0] == keys[j][0] && keys[i][1] < keys[j][1]
	})
	markers := compare.LineagePairs(pl.counts.Genes)
	expresses := func(l uint32) map[string]bool {
		out := map[string]bool{}
		c := pl.counts
		for i := c.Offsets[l-1]; i < c.Offsets[l]; i++ {
			if c.Count[i] > 0 {
				out[c.Genes[c.Gene[i]]] = true
			}
		}
		return out
	}
	var pairs []pair
	for _, key := range keys {
		p := pair{a: key[0], b: key[1], unlike: 1 - dot(pl.profile(key[0]), pl.profile(key[1]))}
		ea, eb := expresses(key[0]), expresses(key[1])
		for _, m := range markers {
			alone := ea[m[0]] && ea[m[1]] || eb[m[0]] && eb[m[1]]
			if !alone && (ea[m[0]] || eb[m[0]]) && (ea[m[1]] || eb[m[1]]) {
				p.mixed = append(p.mixed, m)
			}
		}
		pairs = append(pairs, p)
	}
	if len(pairs) == 0 {
		return nil
	}
	sorted := append([]pair{}, pairs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].unlike < sorted[j].unlike })
	quartile := sorted[len(sorted)*3/4].unlike
	var mixing, unlike []pair
	for _, p := range pairs {
		switch {
		case len(p.mixed) > 0:
			mixing = append(mixing, p)
		case p.unlike >= quartile:
			unlike = append(unlike, p)
		}
	}
	pl.rng.Shuffle(len(mixing), func(i, j int) { mixing[i], mixing[j] = mixing[j], mixing[i] })
	pl.rng.Shuffle(len(unlike), func(i, j int) { unlike[i], unlike[j] = unlike[j], unlike[i] })
	var out []Error
	for _, p := range append(mixing, unlike...) {
		if len(out) == k {
			break
		}
		if !pl.free(p.a, p.b) {
			continue
		}
		pl.take(p.a, p.b)
		pl.mergedInto[int(p.b)] = int(p.a)
		pl.touched[p.a], pl.touched[p.b] = true, true
		x, y := pl.centre(p.a, p.b)
		note := fmt.Sprintf("unlike expression (%.2f)", p.unlike)
		if len(p.mixed) > 0 {
			note = fmt.Sprintf("markers of two lineages together (%s and %s)", p.mixed[0][0], p.mixed[0][1])
			if len(p.mixed) > 1 {
				note = fmt.Sprintf("markers of different lineages together (%s and %s, and %d more pairs)", p.mixed[0][0], p.mixed[0][1], len(p.mixed)-1)
			}
		}
		out = append(out, Error{Kind: Merge, Cells: []string{pl.cells[p.a-1].ID, pl.cells[p.b-1].ID}, X: x, Y: y, Note: note})
		pl.errorLabels = append(pl.errorLabels, []uint32{p.a, p.b})
	}
	return out
}

// candidates are the free cells with at least min gene transcripts and pixels, in a seeded order.
func (pl *planter) candidates(minQ20 uint32, minPixels int64) []uint32 {
	var out []uint32
	for l := uint32(1); int(l) <= pl.n; l++ {
		if pl.q20(l) >= minQ20 && pl.stats[l-1].Pixels >= minPixels && pl.free(l) {
			out = append(out, l)
		}
	}
	pl.shuffle(out)
	return out
}

// splits cuts large cells in two across their long axis: of the cells larger than the median,
// with 40 gene transcripts or more.
func (pl *planter) splits(k int) []Error {
	areas := make([]int64, 0, pl.n)
	for _, s := range pl.stats {
		if s.Pixels > 0 {
			areas = append(areas, s.Pixels)
		}
	}
	if len(areas) == 0 {
		return nil
	}
	sort.Slice(areas, func(i, j int) bool { return areas[i] < areas[j] })
	var out []Error
	for _, l := range pl.candidates(40, areas[len(areas)/2]) {
		if len(out) == k {
			break
		}
		if !pl.free(l) {
			continue
		}
		s := pl.stats[l-1]
		cx, cy := s.SumX/float64(s.Pixels)-0.5, s.SumY/float64(s.Pixels)-0.5
		// The long axis: the second moments' leading eigenvector, from products, sums, a
		// quotient and a square root alone (each rounded the same on every processor).
		var sxx, syy, sxy float64
		pixels := pl.pixels(l)
		for _, p := range pixels {
			dx, dy := float64(p[0])-cx, float64(p[1])-cy
			sxx, syy, sxy = sxx+float64(dx*dx), syy+float64(dy*dy), sxy+float64(dx*dy)
		}
		ux, uy := longAxis(sxx, syy, sxy)
		var half [][2]int
		for _, p := range pixels {
			if float64((float64(p[0])-cx)*ux)+float64((float64(p[1])-cy)*uy) > 0 {
				half = append(half, p)
			}
		}
		if len(half) == 0 || len(half) == len(pixels) {
			continue
		}
		pl.take(l)
		pl.touched[l] = true
		newLabel := uint32(pl.n + 1 + len(pl.parts))
		pl.parts = append(pl.parts, part{of: int(l)})
		for _, p := range half {
			pl.edit(p[0], p[1], newLabel)
		}
		x, y := pl.centre(l)
		out = append(out, Error{Kind: Split, Cells: []string{pl.cells[l-1].ID}, X: x, Y: y, Note: fmt.Sprintf("%d of %d pixels cut away", len(half), len(pixels))})
		pl.errorLabels = append(pl.errorLabels, []uint32{l, newLabel})
	}
	return out
}

// missed drops cells with 20 gene transcripts or more.
func (pl *planter) missed(k int) []Error {
	var out []Error
	for _, l := range pl.candidates(20, 1) {
		if len(out) == k {
			break
		}
		if !pl.free(l) {
			continue
		}
		pl.take(l)
		pl.touched[l] = true
		pl.removed[int(l)] = true
		x, y := pl.centre(l)
		out = append(out, Error{Kind: Missed, Cells: []string{pl.cells[l-1].ID}, X: x, Y: y, Note: fmt.Sprintf("%d gene transcripts left in no cell", pl.q20(l))})
		pl.errorLabels = append(pl.errorLabels, []uint32{l})
	}
	return out
}

// grow draws cells with three neighbours or more too wide: every pixel within reach (pixels) of
// the cell, its neighbours' and the background's, becomes the cell's.
func (pl *planter) grow(k int, reach float64) []Error {
	var out []Error
	for _, l := range pl.candidates(20, 1) {
		if len(out) == k {
			break
		}
		if len(pl.around[l]) < 3 || !pl.free(l) {
			continue
		}
		s := pl.stats[l-1]
		own := pl.pixels(l)
		edge := pl.boundary(l, own)
		margin := int(math.Ceil(reach))
		taken := map[uint32]bool{}
		gained := 0
		buf := make([]uint32, s.MaxX-s.MinX+1+2*margin)
		for y := max(s.MinY-margin, 0); y <= min(s.MaxY+margin, pl.r.Height-1); y++ {
			x0, x1 := max(s.MinX-margin, 0), min(s.MaxX+margin, pl.r.Width-1)
			buf = buf[:x1-x0+1]
			pl.r.RowRange(y, x0, x1+1, buf)
			for x := x0; x <= x1; x++ {
				v := buf[x-x0]
				if e, ok := pl.edits[y][x]; ok {
					v = e
				}
				// The background's pixels and the neighbours', never another cell's.
				if v == l || v != 0 && !pl.isAround(l, v) {
					continue
				}
				if near(edge, x, y, reach) {
					pl.edit(x, y, l)
					gained++
					if v != 0 {
						taken[v] = true
					}
				}
			}
		}
		if gained == 0 {
			continue
		}
		pl.take(l)
		pl.touched[l] = true
		labels := []uint32{l}
		names := []string{pl.cells[l-1].ID}
		for _, o := range pl.around[l] {
			if taken[o] {
				pl.touched[o] = true
				labels = append(labels, o)
				names = append(names, pl.cells[o-1].ID)
			}
		}
		x, y := pl.centre(l)
		out = append(out, Error{Kind: Grown, Cells: names, X: x, Y: y, Note: fmt.Sprintf("%d pixels taken, from %d neighbours and the background", gained, len(taken))})
		pl.errorLabels = append(pl.errorLabels, labels)
	}
	return out
}

// longAxis is the leading eigenvector (not normalized) of the symmetric matrix [[a, b], [b, c]].
func longAxis(a, c, b float64) (float64, float64) {
	if b == 0 {
		if a >= c {
			return 1, 0
		}
		return 0, 1
	}
	h := (a - c) / 2
	lambda := (a+c)/2 + math.Sqrt(float64(h*h)+float64(b*b))
	return b, lambda - a
}

func (pl *planter) isAround(l, o uint32) bool {
	for _, v := range pl.around[l] {
		if v == o {
			return true
		}
	}
	return false
}

func (pl *planter) edit(x, y int, label uint32) {
	row, ok := pl.edits[y]
	if !ok {
		row = map[int]uint32{}
		pl.edits[y] = row
	}
	row[x] = label
}

// pixels lists a cell's pixels (as the source holds them).
func (pl *planter) pixels(l uint32) [][2]int {
	s := pl.stats[l-1]
	var out [][2]int
	buf := make([]uint32, s.MaxX-s.MinX+1)
	for y := s.MinY; y <= s.MaxY; y++ {
		pl.r.RowRange(y, s.MinX, s.MaxX+1, buf)
		for i, v := range buf {
			if v == l {
				out = append(out, [2]int{s.MinX + i, y})
			}
		}
	}
	return out
}

// boundary lists a cell's pixels with a 4-neighbour outside it.
func (pl *planter) boundary(l uint32, own [][2]int) [][2]int {
	in := map[[2]int]bool{}
	for _, p := range own {
		in[p] = true
	}
	var out [][2]int
	for _, p := range own {
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if !in[[2]int{p[0] + d[0], p[1] + d[1]}] {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

func near(edge [][2]int, x, y int, reach float64) bool {
	r2 := reach * reach
	for _, p := range edge {
		dx, dy := float64(p[0]-x), float64(p[1]-y)
		if float64(dx*dx)+float64(dy*dy) <= r2 {
			return true
		}
	}
	return false
}
