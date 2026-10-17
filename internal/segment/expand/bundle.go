package expand

// Nucleus expansion of a Xenium bundle's nuclei, written as a segmentation's files (internal/
// segment, format.go). The nuclei are the bundle's nucleus mask (cells.zarr.zip masks/0), or its
// nucleus polygons drawn into one when it has no mask; each nucleus grows by the distance asked
// into a cell of its own; each transcript goes to the cell holding the pixel it lies in, as the
// bundle's own transcripts were assigned; the counts are of gene transcripts of quality 20 or more.
//
// Each nucleus belongs to one of the instrument's cells: the one holding most of its pixels in the
// instrument's cell mask (cells.zarr.zip masks/1), or the cell its polygon names. A cell the
// instrument drew from a cell stain may hold no nucleus, or several, so nuclei and the instrument's
// cells need not pair one to one, nor share labels. The cell grown from a nucleus is compared with
// the instrument's cell it belongs to: pixel by pixel, transcript by transcript, and by area where
// they pair one to one; what differs is described.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"

	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// Params are nucleus expansion's parameters.
type Params struct {
	// DistanceUm is how far each nucleus grows, in µm (Xenium Onboard Analysis's default is 5).
	DistanceUm float64 `json:"distance_um"`
}

// Recorded are the parameters as the artifact records them: every one, after defaults.
type Recorded struct {
	DistanceUm         float64 `json:"distance_um"`
	DistancePx         float64 `json:"distance_px"`
	Nuclei             string  `json:"nuclei"`
	NucleusCells       string  `json:"nucleus_cells"`
	Distance           string  `json:"distance"`
	Assignment         string  `json:"assignment"`
	CountsMinQuality   int     `json:"counts_min_quality"`
	OutlineTolerancePx float64 `json:"outline_tolerance_px"`
	Ties               string  `json:"ties"`
}

// Method names the method and its version, as its artifacts record them.
const (
	Method        = "nucleus expansion"
	MethodVersion = "1"
	Citation      = "Okabe A, Boots B, Sugihara K, Chiu SN. Spatial Tessellations: Concepts and Applications of Voronoi Diagrams, 2nd ed. Wiley, 2000; distances: Felzenszwalb PF, Huttenlocher DP. Distance Transforms of Sampled Functions. Theory Comput. 2012;8:415–428. doi:10.4086/toc.2012.v008a019"
	outlineTol    = 0.5
)

// Agreement is how a segmentation compares with the instrument's.
type Agreement struct {
	Reference string `json:"reference"`
	Cells     struct {
		// SituCraft's cells, one for each nucleus, and the instrument's.
		SituCraft  int `json:"situcraft"`
		Instrument int `json:"instrument"`
		// Paired are the instrument's cells holding exactly one nucleus; the others hold none or
		// several. NucleiOutside are nuclei in none of the instrument's cells.
		Paired                   int `json:"paired"`
		InstrumentWithoutNucleus int `json:"instrumentWithoutNucleus"`
		InstrumentWithSeveral    int `json:"instrumentWithSeveralNuclei"`
		NucleiOutside            int `json:"nucleiOutsideCells"`
	} `json:"cells"`
	// Pixels and transcripts are in the same cell when SituCraft's cell grew from a nucleus of the
	// instrument's.
	Pixels      Tally `json:"pixels"`
	Transcripts Tally `json:"transcripts"`
	// Area compares the paired cells.
	Area struct {
		MedianRelativeDifference float64 `json:"medianRelativeDifference"`
		Within5                  float64 `json:"within5Percent"`
		Within10                 float64 `json:"within10Percent"`
	} `json:"area"`
	AssignedFraction struct {
		SituCraft  float64 `json:"situcraft"`
		Instrument float64 `json:"instrument"`
	} `json:"assignedFraction"`
	// Differences describe the pixels and transcripts the two segmentations treat differently
	// (when the bundle has the instrument's cell mask).
	Differences *Differences `json:"differences,omitempty"`
}

// Differences describe where two segmentations differ.
type Differences struct {
	// SituCraftOnlyUm is the range of distances, µm, from each pixel only SituCraft gives a cell
	// to that cell's nucleus; InstrumentOnlyUm the same for the instrument's cells that hold a
	// nucleus (to the nearest of their nuclei). InstrumentOnlyNoNucleus counts the pixels only
	// the instrument gives a cell, a cell holding no nucleus.
	SituCraftOnlyUm         [2]float64 `json:"situcraftOnlyDistanceUm"`
	InstrumentOnlyUm        [2]float64 `json:"instrumentOnlyDistanceUm"`
	InstrumentOnlyNoNucleus int64      `json:"instrumentOnlyNoNucleus"`
	// OtherCellGapUm is, of the pixels the two give different cells, how much nearer to SituCraft's
	// cell's nucleus than to the instrument's cell's nuclei a pixel is, at most, µm: how far from
	// equally near both the differing boundaries lie. OtherCellNoNucleus counts those whose
	// instrument cell holds no nucleus.
	OtherCellGapUm     float64 `json:"otherCellGapUm"`
	OtherCellNoNucleus int64   `json:"otherCellNoNucleus"`
	// TranscriptsOffMask are transcripts given differently whose cell the instrument records other
	// than its own mask holds at their pixel (on a pixel's edge, or outside the mask).
	TranscriptsOffMask int64 `json:"transcriptsOffMask"`
}

// Tally sorts pixels or transcripts by what each segmentation does with them.
type Tally struct {
	Same           int64   `json:"same"`
	OtherCell      int64   `json:"otherCell"`
	SituCraftOnly  int64   `json:"situcraftOnly"`
	InstrumentOnly int64   `json:"instrumentOnly"`
	Neither        int64   `json:"neither,omitempty"`
	Agreement      float64 `json:"agreement"`
}

// FromBundle expands the bundle's nuclei and writes the segmentation's files into dir.
func FromBundle(ctx context.Context, b *xenium.Bundle, dir string, p Params, workers int, progress func(float64, string)) (*segment.Description, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	if !(p.DistanceUm >= 0) || p.DistanceUm > 100 {
		return nil, errors.New("the distance is from 0 to 100 µm")
	}
	cells, err := b.ReadCells()
	if err != nil {
		return nil, err
	}
	progress(0, "Reading the nuclei")
	nuc, err := nucleiOf(b, cells)
	if err != nil {
		return nil, err
	}
	defer nuc.close()
	ref, err := instrumentCells(b, nuc.labels())
	if err != nil {
		return nil, err
	}
	if ref != nil {
		if top := maxLabel(ref); int(top) > len(cells.IDs) {
			return nil, fmt.Errorf("the instrument's cell mask (cells.zarr.zip masks/1) has a cell %d, but cells.parquet has %d cells: the mask is not of these cells", top, len(cells.IDs))
		}
	}
	rec := Recorded{
		DistanceUm: p.DistanceUm, DistancePx: p.DistanceUm / nuc.pixelSize, Nuclei: nuc.source,
		// Written into the artifact as earlier releases wrote it, so that it is made again byte for byte.
		Distance:         "Euclidean, between pixel centres, from each nucleus's pixels",
		Assignment:       "a transcript belongs to the cell holding the pixel it lies in",
		CountsMinQuality: 20, OutlineTolerancePx: outlineTol,
		Ties: "a pixel equally near two nuclei goes to the upper one in its column, else the left one in its row",
	}
	cellR, nucR, err := Expand(ctx, nuc.labels(), Options{Distance: rec.DistancePx, Workers: workers, Progress: func(f float64) {
		progress(0.4*f, "Growing the nuclei")
	}})
	if err != nil {
		return nil, err
	}
	cellR.PixelSize, nucR.PixelSize = nuc.pixelSize, nuc.pixelSize
	n := int(maxLabel(nucR)) // a cell for each nucleus label
	owners := nuc.owners
	switch {
	case owners != nil:
		rec.NucleusCells = "each nucleus polygon belongs to the cell it names (nucleus_boundaries.parquet's cell_id)"
	case ref != nil:
		owners = ownersOf(nucR, ref, n)
		rec.NucleusCells = "each nucleus belongs to the instrument's cell holding most of its pixels (cells.zarr.zip masks/1)"
	default:
		owners = make([]uint32, n+1)
		for k := 1; k <= n && k <= len(cells.IDs); k++ {
			owners[k] = uint32(k)
		}
		rec.NucleusCells = "nucleus k belongs to cells.parquet's row k (the bundle has no cell mask)"
	}
	stats, nstats := cellR.Stats(n), nucR.Stats(n)
	nuclei := 0
	for _, s := range nstats {
		if s.Pixels > 0 {
			nuclei++
		}
	}
	px2 := nuc.pixelSize * nuc.pixelSize
	out := make([]segment.Cell, n)
	for i := range out {
		c := &out[i]
		c.ID = "c" + strconv.Itoa(i+1)
		if o := owners[i+1]; o > 0 {
			c.Nucleus = cells.IDs[o-1]
		}
		if s := stats[i]; s.Pixels > 0 {
			c.Area = float32(float64(s.Pixels) * px2)
			c.X, c.Y = float32(s.SumX/float64(s.Pixels)*nuc.pixelSize), float32(s.SumY/float64(s.Pixels)*nuc.pixelSize)
		}
		c.NucleusArea = float32(float64(nstats[i].Pixels) * px2)
	}

	// Transcripts: each to the cell holding its pixel; counts of gene transcripts of quality 20+.
	progress(0.4, "Assigning the transcripts")
	tr, err := b.OpenTranscripts()
	if err != nil {
		return nil, err
	}
	defer tr.Close()
	assignments := make([]uint32, 0, tr.File.NumRows)
	cmp := &comparison{cellR: cellR, nucR: nucR, ref: ref, owners: owners, cells: cells}
	geneIndex := map[string]uint32{}
	var geneNames []string
	pairs := map[uint64]uint32{}
	var q20, q20Assigned, q20Instrument int64
	err = tr.Each(func(batch *xenium.TranscriptBatch) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i := 0; i < batch.Rows; i++ {
			label := cellR.AtMicrons(float64(batch.X[i]), float64(batch.Y[i]))
			assignments = append(assignments, label)
			theirs := uint32(0)
			if row, ok := cells.Index[string(batch.CellID[i])]; ok {
				theirs = uint32(row) + 1
			}
			cmp.transcript(label, theirs, float64(batch.X[i]), float64(batch.Y[i]))
			if label > 0 {
				out[label-1].Assigned++
			}
			name := string(batch.Feature[i])
			if batch.QV[i] < 20 || xenium.FeatureKind(name) != xenium.KindGene {
				continue
			}
			q20++
			if theirs > 0 {
				q20Instrument++
			}
			if label == 0 {
				continue
			}
			g, ok := geneIndex[name]
			if !ok {
				g = uint32(len(geneNames))
				geneIndex[name] = g
				geneNames = append(geneNames, name)
			}
			pairs[uint64(label-1)<<32|uint64(g)]++
			out[label-1].Transcripts++
			q20Assigned++
		}
		progress(0.4+0.25*float64(len(assignments))/float64(max(tr.File.NumRows, 1)), "Assigning the transcripts")
		return nil
	})
	if err != nil {
		return nil, err
	}
	counts := countsOf(pairs, geneNames, n)
	for i := range out {
		out[i].Genes = counts.Offsets[i+1] - counts.Offsets[i]
	}

	// Outlines, a cell at a time on every worker.
	progress(0.65, "Tracing the cells' outlines")
	outlines := make([]segment.Outline, n)
	cellR.RowRange(0, 0, 1, make([]uint32, 1)) // index the raster before the workers share it
	parallel(n, workers, func(a, z int) {
		for i := a; i < z; i++ {
			outlines[i] = cellR.TraceOutline(uint32(i+1), stats[i], outlineTol)
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// The agreement with the instrument's segmentation.
	progress(0.85, "Comparing with the instrument's segmentation")
	agreement, err := cmp.finish(out, q20, q20Assigned, q20Instrument)
	if err != nil {
		return nil, err
	}
	progress(0.95, "Writing the segmentation")
	params, _ := json.Marshal(rec)
	d := segment.Description{Method: Method + " " + MethodVersion, Parameters: params, Width: cellR.Width, Height: cellR.Height, PixelSize: nuc.pixelSize,
		Cells: n, Transcripts: len(assignments), Genes: len(counts.Genes), Q20GeneTranscripts: q20, Q20GeneAssigned: q20Assigned, Agreement: agreement}
	d.Nuclei.Source, d.Nuclei.Count = nuc.source, nuclei
	for _, write := range []func() error{
		func() error { return segment.WriteCells(dir, out) },
		func() error { return segment.WriteMask(dir, cellR) },
		func() error { return segment.WriteAssignments(dir, assignments) },
		func() error { return segment.WriteCounts(dir, counts) },
		func() error { return segment.WriteOutlines(dir, outlines) },
		func() error { return segment.WriteDescription(dir, d) },
	} {
		if err := write(); err != nil {
			return nil, err
		}
	}
	progress(1, "Written")
	return &d, nil
}

// nuclei are the bundle's nucleus label image, where it comes from, and (for drawn polygons) each
// label's cell.
type nuclei struct {
	mask      *xenium.Mask // the nucleus mask, or
	image     *Image       // the polygons drawn
	source    string
	pixelSize float64
	owners    []uint32 // label → cells.parquet's row + 1 (0: none); nil when the mask's
}

func (n *nuclei) labels() Source {
	if n.mask != nil {
		return n.mask
	}
	return n.image
}

func (n *nuclei) close() {
	if n.mask != nil {
		n.mask.Close()
	}
}

func nucleiOf(b *xenium.Bundle, cells *xenium.Cells) (*nuclei, error) {
	if b.Has("cells.zarr.zip") {
		if m, err := b.OpenMask(xenium.NucleusMask); err == nil {
			return &nuclei{mask: m, source: "the instrument's nucleus mask (cells.zarr.zip masks/0)", pixelSize: m.PixelSize}, nil
		}
	}
	img, owners, size, err := rasterizeNuclei(b, cells)
	if err != nil {
		return nil, fmt.Errorf("the bundle has neither a nucleus mask nor nucleus polygons SituCraft can draw: %w", err)
	}
	return &nuclei{image: img, owners: owners, source: "the instrument's nucleus polygons (nucleus_boundaries.parquet), drawn at the image's resolution", pixelSize: size}, nil
}

// instrumentCells is the instrument's cell mask (cells.zarr.zip masks/1) as a raster, when the
// bundle has one the size of the nuclei's; nil otherwise.
func instrumentCells(b *xenium.Bundle, nuclei Source) (*segment.Raster, error) {
	if !b.Has("cells.zarr.zip") {
		return nil, nil
	}
	m, err := b.OpenMask(xenium.CellMask)
	if err != nil {
		return nil, nil
	}
	defer m.Close()
	w, h := m.Size()
	if nw, nh := nuclei.Size(); w != nw || h != nh {
		return nil, nil
	}
	r := &segment.Raster{Width: w, Height: h, PixelSize: m.PixelSize, Rows: make([][]segment.Run, h)}
	row := make([]uint32, w)
	for y := range r.Rows {
		if err := m.Rows(y, y+1, row); err != nil {
			return nil, err
		}
		r.Rows[y] = segment.EncodeRow(row)
	}
	return r, nil
}

func maxLabel(r *segment.Raster) uint32 {
	var top uint32
	for _, row := range r.Rows {
		for _, run := range row {
			top = max(top, run.Label)
		}
	}
	return top
}

// ownersOf is, for each nucleus label 1..n, the instrument's cell holding most of its pixels (0:
// none; the lower label of a tie).
func ownersOf(nucR, ref *segment.Raster, n int) []uint32 {
	votes := map[uint64]int64{}
	a, c := make([]uint32, nucR.Width), make([]uint32, nucR.Width)
	for y := 0; y < nucR.Height; y++ {
		nucR.Row(y, a)
		ref.Row(y, c)
		for x, k := range a {
			if k != 0 {
				votes[uint64(k)<<32|uint64(c[x])]++
			}
		}
	}
	owners := make([]uint32, n+1)
	best := make([]int64, n+1)
	keys := make([]uint64, 0, len(votes))
	for key := range votes {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, key := range keys {
		k, cell := key>>32, uint32(key)
		if v := votes[key]; v > best[k] {
			best[k], owners[k] = v, cell
		}
	}
	return owners
}

// rasterizeNuclei draws nucleus_boundaries.parquet's polygons into a label image of the morphology
// image's size, polygon k labelled k+1; it answers each label's cell.
func rasterizeNuclei(b *xenium.Bundle, cells *xenium.Cells) (*Image, []uint32, float64, error) {
	size := b.Experiment.PixelSize
	if !(size > 0) {
		return nil, nil, 0, errors.New("the manifest gives no pixel size")
	}
	img, err := b.FocusImage()
	if err != nil || img == nil || len(img.Channels) == 0 || len(img.Channels[0].Levels) == 0 {
		return nil, nil, 0, errors.New("the morphology image's size is unknown")
	}
	w, h := img.Channels[0].Levels[0].Width, img.Channels[0].Levels[0].Height
	p, err := b.ReadBoundaries("nucleus_boundaries.parquet")
	if err != nil {
		return nil, nil, 0, err
	}
	drawn, owners := drawNuclei(p, cells.Index, w, h, size)
	return drawn, owners, size, nil
}

// drawNuclei draws polygons (µm) into a label image of w × h pixels of size µm, polygon k labelled
// k+1, and answers each label's cell (its row in index + 1; 0 when index lacks it). The polygons'
// vertices lie on their edge pixels' own coordinates (pixel (i, j) at (i, j) × size, the corner
// the pixel's area starts from), so a pixel belongs to a polygon when that point is inside it or
// on its edge (within a thousandth of a pixel).
func drawNuclei(p *xenium.Polygons, index map[string]int32, w, h int, size float64) (*Image, []uint32) {
	const eps = 1e-3
	out := &Image{Width: w, Height: h, Labels: make([]uint32, w*h)}
	owners := make([]uint32, len(p.CellIDs)+1)
	var cross []float64
	for k := 0; k+1 < len(p.Offsets) && k < len(p.CellIDs); k++ {
		if row, ok := index[p.CellIDs[k]]; ok {
			owners[k+1] = uint32(row) + 1
		}
		xs, ys := p.X[p.Offsets[k]:p.Offsets[k+1]], p.Y[p.Offsets[k]:p.Offsets[k+1]]
		minY, maxY := math.Inf(1), math.Inf(-1)
		for _, y := range ys {
			minY, maxY = math.Min(minY, float64(y)/size), math.Max(maxY, float64(y)/size)
		}
		for py := max(0, int(math.Ceil(minY-eps))); py <= min(h-1, int(math.Floor(maxY+eps))); py++ {
			// Scan just above and just below the pixel's row, so that edges along it count.
			for _, cy := range [2]float64{float64(py) - eps, float64(py) + eps} {
				cross = cross[:0]
				for i, j := 0, len(xs)-1; i < len(xs); j, i = i, i+1 {
					yi, yj := float64(ys[i])/size, float64(ys[j])/size
					if (yi > cy) != (yj > cy) {
						xi, xj := float64(xs[i])/size, float64(xs[j])/size
						cross = append(cross, xi+(cy-yi)*(xj-xi)/(yj-yi))
					}
				}
				sort.Float64s(cross)
				for c := 0; c+1 < len(cross); c += 2 {
					for px := max(0, int(math.Ceil(cross[c]-eps))); px <= min(w-1, int(math.Floor(cross[c+1]+eps))); px++ {
						out.Labels[py*w+px] = uint32(k + 1)
					}
				}
			}
		}
	}
	return out, owners
}

// countsOf turns (cell, gene) counts into a matrix in compressed sparse rows, genes in name order.
func countsOf(pairs map[uint64]uint32, genes []string, n int) *segment.Counts {
	order := make([]int, len(genes))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return genes[order[a]] < genes[order[b]] })
	rank := make([]uint32, len(genes))
	sorted := make([]string, len(genes))
	for r, g := range order {
		rank[g] = uint32(r)
		sorted[r] = genes[g]
	}
	keys := make([]uint64, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k>>32<<32|uint64(rank[uint32(k)]))
	}
	sort.Slice(keys, func(a, b int) bool { return keys[a] < keys[b] })
	c := &segment.Counts{Genes: sorted, Offsets: make([]uint32, n+1)}
	for _, k := range keys {
		cell := int(k >> 32)
		gene := uint32(k)
		original := uint32(order[gene])
		c.Gene = append(c.Gene, gene)
		c.Count = append(c.Count, pairs[uint64(cell)<<32|uint64(original)])
		c.Offsets[cell+1]++
	}
	for i := 1; i <= n; i++ {
		c.Offsets[i] += c.Offsets[i-1]
	}
	return c
}

// comparison compares the cells grown from the nuclei with the instrument's cells the nuclei
// belong to.
type comparison struct {
	a      Agreement
	cellR  *segment.Raster
	nucR   *segment.Raster
	ref    *segment.Raster // the instrument's cell mask, or nil
	owners []uint32        // nucleus label → the instrument's cell (0: none)
	cells  *xenium.Cells
	offRef int64
}

// tally counts a pixel or transcript that SituCraft gives cell mine (grown from a nucleus of the
// instrument's cell mapped) and the instrument cell theirs (0: none).
func tally(t *Tally, mine, mapped, theirs uint32) {
	switch {
	case mine == 0 && theirs == 0:
		t.Neither++
	case mine == 0:
		t.InstrumentOnly++
	case theirs == 0:
		t.SituCraftOnly++
	case mapped == theirs:
		t.Same++
	default:
		t.OtherCell++
	}
}

// transcript tallies a transcript at (x, y) µm.
func (c *comparison) transcript(mine, theirs uint32, x, y float64) {
	mapped := c.owners[mine]
	tally(&c.a.Transcripts, mine, mapped, theirs)
	same := mine == 0 && theirs == 0 || mine != 0 && mapped == theirs
	if !same && c.ref != nil && c.ref.AtMicrons(x, y) != theirs {
		c.offRef++
	}
}

// finish pairs the cells, compares the pixels and the areas, and answers the agreement.
func (c *comparison) finish(out []segment.Cell, q20, q20Ours, q20Theirs int64) (json.RawMessage, error) {
	a := &c.a
	a.Reference = "the instrument's segmentation: transcripts.parquet's cell_id and cells.parquet's cell_area"
	n := len(c.cells.IDs)
	nucleiOf := make([]int, n+1) // the instrument's cell → its nuclei
	nucleus := make([]int, n+1)  // … the last of them
	for k := 1; k < len(c.owners); k++ {
		if out[k-1].NucleusArea == 0 {
			continue
		}
		if o := c.owners[k]; o == 0 {
			a.Cells.NucleiOutside++
		} else {
			nucleiOf[o]++
			nucleus[o] = k
		}
	}
	a.Cells.SituCraft, a.Cells.Instrument = len(out), n
	for t := 1; t <= n; t++ {
		switch nucleiOf[t] {
		case 0:
			a.Cells.InstrumentWithoutNucleus++
		case 1:
			a.Cells.Paired++
		default:
			a.Cells.InstrumentWithSeveral++
		}
	}
	if total := a.Transcripts.Same + a.Transcripts.OtherCell + a.Transcripts.SituCraftOnly + a.Transcripts.InstrumentOnly + a.Transcripts.Neither; total > 0 {
		a.Transcripts.Agreement = round6(float64(a.Transcripts.Same+a.Transcripts.Neither) / float64(total))
	}
	if c.ref != nil {
		a.Reference = "the instrument's segmentation: cells.zarr.zip masks/1, transcripts.parquet's cell_id and cells.parquet's cell_area"
		d := &Differences{TranscriptsOffMask: c.offRef}
		own := spansOf(c.nucR, len(out), nil)  // each nucleus's pixels
		theirs := spansOf(c.nucR, n, c.owners) // each instrument cell's nuclei's pixels
		ranges := [2][2]float64{{math.Inf(1), math.Inf(-1)}, {math.Inf(1), math.Inf(-1)}}
		widen := func(r *[2]float64, v float64) { r[0], r[1] = math.Min(r[0], v), math.Max(r[1], v) }
		w := c.cellR.Width
		mine, ref := make([]uint32, w), make([]uint32, w)
		ps := c.cellR.PixelSize
		for y := 0; y < c.cellR.Height; y++ {
			c.cellR.Row(y, mine)
			c.ref.Row(y, ref)
			for x := 0; x < w; x++ {
				m, t := mine[x], ref[x]
				if m == 0 && t == 0 {
					continue
				}
				tally(&a.Pixels, m, c.owners[m], t)
				switch {
				case m != 0 && t != 0 && c.owners[m] == t:
				case t == 0:
					widen(&ranges[0], distance(own[m], x, y)*ps)
				case len(theirs[t]) == 0 && m == 0:
					d.InstrumentOnlyNoNucleus++
				case len(theirs[t]) == 0:
					d.OtherCellNoNucleus++
				case m == 0:
					widen(&ranges[1], distance(theirs[t], x, y)*ps)
				default:
					d.OtherCellGapUm = max(d.OtherCellGapUm, (distance(theirs[t], x, y)-distance(own[m], x, y))*ps)
				}
			}
		}
		a.Pixels.Neither = 0
		if union := a.Pixels.Same + a.Pixels.OtherCell + a.Pixels.SituCraftOnly + a.Pixels.InstrumentOnly; union > 0 {
			a.Pixels.Agreement = round6(float64(a.Pixels.Same) / float64(union))
		}
		for i, r := range ranges {
			if r[0] <= r[1] {
				target := &d.SituCraftOnlyUm
				if i == 1 {
					target = &d.InstrumentOnlyUm
				}
				*target = [2]float64{round6(r[0]), round6(r[1])}
			}
		}
		d.OtherCellGapUm = round6(d.OtherCellGapUm)
		a.Differences = d
	}
	if c.cells.CellArea != nil {
		var diffs []float64
		within5, within10 := 0, 0
		for t := 1; t <= n; t++ {
			ref := c.cells.CellArea[t-1]
			if nucleiOf[t] != 1 || !(ref > 0) {
				continue
			}
			d := (float64(out[nucleus[t]-1].Area) - ref) / ref
			diffs = append(diffs, d)
			if math.Abs(d) <= 0.05 {
				within5++
			}
			if math.Abs(d) <= 0.10 {
				within10++
			}
		}
		if len(diffs) > 0 {
			sort.Float64s(diffs)
			a.Area.MedianRelativeDifference = round6(diffs[len(diffs)/2])
			a.Area.Within5 = round6(float64(within5) / float64(len(diffs)))
			a.Area.Within10 = round6(float64(within10) / float64(len(diffs)))
		}
	}
	if q20 > 0 {
		a.AssignedFraction.SituCraft = round6(float64(q20Ours) / float64(q20))
		a.AssignedFraction.Instrument = round6(float64(q20Theirs) / float64(q20))
	}
	return json.Marshal(a)
}

// span is a run of a nucleus's pixels: row y, columns x0..x1.
type span struct{ y, x0, x1 int }

// spansOf lists the runs of each label 1..n, or (with owners) of each owner 1..n's labels.
func spansOf(r *segment.Raster, n int, owners []uint32) [][]span {
	out := make([][]span, n+1)
	for y, row := range r.Rows {
		x := 0
		for _, run := range row {
			k := run.Label
			if owners != nil && int(k) < len(owners) {
				k = owners[k]
			}
			if k != 0 && int(k) <= n {
				out[k] = append(out[k], span{y, x, x + int(run.Length) - 1})
			}
			x += int(run.Length)
		}
	}
	return out
}

// distance from pixel (x, y)'s centre to the nearest pixel centre of spans, in pixels (+Inf when
// there are none).
func distance(spans []span, x, y int) float64 {
	best := math.Inf(1)
	for _, s := range spans {
		dx := 0
		if x < s.x0 {
			dx = s.x0 - x
		} else if x > s.x1 {
			dx = x - s.x1
		}
		dy := y - s.y
		best = min(best, float64(dx*dx+dy*dy))
	}
	return math.Sqrt(best)
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// NucleusRaster is the bundle's nuclei as a label raster at the morphology image's resolution: its
// nucleus mask, or its nucleus polygons drawn (labels are the mask's, or the polygons' order).
func NucleusRaster(b *xenium.Bundle) (*segment.Raster, error) {
	cells, err := b.ReadCells()
	if err != nil {
		return nil, err
	}
	nuc, err := nucleiOf(b, cells)
	if err != nil {
		return nil, err
	}
	defer nuc.close()
	src := nuc.labels()
	w, h := src.Size()
	r := &segment.Raster{Width: w, Height: h, PixelSize: nuc.pixelSize, Rows: make([][]segment.Run, h)}
	band := max(1, (64<<20)/(4*w))
	buf := make([]uint32, band*w)
	for y0 := 0; y0 < h; y0 += band {
		y1 := min(h, y0+band)
		if err := src.Rows(y0, y1, buf[:(y1-y0)*w]); err != nil {
			return nil, err
		}
		for y := y0; y < y1; y++ {
			r.Rows[y] = segment.EncodeRow(buf[(y-y0)*w : (y-y0+1)*w])
		}
	}
	return r, nil
}
