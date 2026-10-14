package cache

// The transcript spatial index. Every transcript is stored once, in the full-resolution (level 0)
// tile of the grid holding it; coarser levels have per-gene density grids instead, so a view of
// the whole tissue never touches points. The levels nest (a tile is four tiles of the level
// below), which makes the index a complete quadtree of tiles aligned with the image's.
//
// Points ("points-<group>.bin", magic SCTP) are written in groups of tile rows. Fields: group,
// first tile row, rows, tiles across, number of genes. Table entries: A is the tile's number of
// points, B the number of genes present. A tile's block (deflated, as every block of the cache's
// files is) holds its points sorted by gene, then by their row in transcripts.parquet:
//
//	starts u32[B+1]   points of the i-th gene present are starts[i] to starts[i+1]
//	x, y, z, qv f32[A]  µm, and the Phred-scaled quality
//	cell u32[A]       row of the cell in cells.parquet; 0xFFFFFFFF for none
//	row u32[A]        row of the transcript in transcripts.parquet
//	genes u16[B]      the genes present (indexes into the summary's genes)
//	flags u8[A]       bit 0: the transcript overlaps its cell's nucleus
//
// Density ("density-<level>.bin", magic SCTD) exists for levels 1 and up. Fields: level, tiles
// across, tiles down, bins per tile side, number of genes. Each tile is divided into bins × bins
// bins; table entries: A is the tile's number of non-empty (gene, bin) pairs, B the number of
// genes present. Indexes past the genes are pseudo-genes, groups of transcripts counted together
// (Pseudo*: every transcript, gene transcripts, gene transcripts in a cell, negative controls),
// which the QC maps draw. Block:
//
//	starts u32[B+1]   entries of the i-th gene present
//	n u32[A]          transcripts in the bin
//	q20 u32[A]        of which with quality 20 or more
//	genes u16[B]
//	bins u16[A]       by*bins + bx, bx and by counted from the tile's top left
//
// Level 1's grids count the points; each coarser level's add up the level below's, 2×2 bins
// into one, so every level's grid of a gene sums to the gene's count exactly.
//
// The build runs in phases that each leave finished files, so that an interrupted build resumes:
// a scan of transcripts.parquet into one spill file per group (scan.json is written when all
// are), each group's points file, then each level's density file.

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"

	"situcraft/internal/xenium"
)

const (
	pointsMagic  = "SCTP"
	densityMagic = "SCTD"
	// DensityBins is the number of density bins along a tile's side.
	DensityBins = 128
	// NoCell is the cell of a transcript assigned to none.
	NoCell = math.MaxUint32
	// maxGroups bounds the spill files open at once while scanning.
	maxGroups   = 64
	spillRecord = 28
	// maxGenes leaves room for the pseudo-genes' indexes in 16 bits.
	maxGenes = math.MaxUint16 + 1 - PseudoCount
)

// The pseudo-genes: density grids of groups of transcripts, at the indexes following the genes
// (the number of genes plus these offsets).
const (
	PseudoAll      = iota // every transcript
	PseudoGenes           // transcripts of genes (not controls or codewords)
	PseudoInCells         // transcripts of genes assigned to a cell
	PseudoControls        // negative control probes and negative control codewords
	PseudoCount
)

// PseudoNames name the pseudo-genes, in order.
var PseudoNames = [PseudoCount]string{"All transcripts", "Gene transcripts", "Gene transcripts in a cell", "Negative controls"}

// pseudoOf is the pseudo-genes a transcript of a kind, assigned to a cell or not, counts in.
func pseudoOf(kind string, inCell bool, out []int) []int {
	out = append(out[:0], PseudoAll)
	switch kind {
	case xenium.KindGene:
		out = append(out, PseudoGenes)
		if inCell {
			out = append(out, PseudoInCells)
		}
	case xenium.KindNegControlProbe, xenium.KindNegControlCode:
		out = append(out, PseudoControls)
	}
	return out
}

// TranscriptInput is what the index is built from.
type TranscriptInput struct {
	// Each calls fn with each batch of transcripts in turn (transcripts.parquet's row groups).
	Each func(fn func(*xenium.TranscriptBatch) error) error
	// CellIndex maps cell IDs to rows of cells.parquet.
	CellIndex map[string]int32
	// Rows is the number of transcripts (for progress).
	Rows int64
}

// TranscriptParams are the index's parameters (part of its key).
type TranscriptParams struct {
	Grid        Grid `json:"grid"`
	DensityBins int  `json:"densityBins"`
	GroupRows   int  `json:"groupRows"`
}

// NewTranscriptParams chooses the groups of tile rows: pairs of rows (one row of level 1), or
// more when that would make more than maxGroups groups.
func NewTranscriptParams(grid Grid) TranscriptParams {
	rows := 2 * max(1, ceilDiv(grid.Down(0), 2*maxGroups))
	return TranscriptParams{Grid: grid, DensityBins: DensityBins, GroupRows: rows}
}

func (p TranscriptParams) groups() int { return ceilDiv(p.Grid.Down(0), p.GroupRows) }

// Gene is one feature of the index.
type Gene struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
	Q20   int64  `json:"q20"`
}

// TranscriptSummary is a transcript index's summary.
type TranscriptSummary struct {
	Transcripts int64 `json:"transcripts"`
	Q20         int64 `json:"q20"`
	Assigned    int64 `json:"assigned"`
	// UnknownCells counts transcripts naming a cell cells.parquet does not have (stored as
	// assigned to none).
	UnknownCells int64 `json:"unknownCells"`
	// Genes are in index order: genes, then the controls and codewords by kind, each by name.
	Genes []Gene `json:"genes"`
	// Pseudo are the pseudo-genes' counts (PseudoAll…), whose density grids follow the genes'.
	Pseudo []Gene `json:"pseudo"`
	// Bounds are the points' extent: min x, min y, max x, max y (µm).
	Bounds     [4]float32 `json:"bounds"`
	HasNucleus bool       `json:"hasNucleus"`
	Groups     int        `json:"groups"`
	// DensityLevels are the levels with density grids.
	DensityLevels []int `json:"densityLevels"`
}

// kindOrder orders genes before controls and codewords.
var kindOrder = map[string]int{
	xenium.KindGene: 0, xenium.KindNegControlProbe: 1, xenium.KindNegControlCode: 2, xenium.KindGenomicControl: 3,
	xenium.KindUnassignedCode: 4, xenium.KindDeprecatedCode: 5,
}

// scanState is scan.json: what the scan found, with genes in the order first met.
type scanState struct {
	Features     []string   `json:"features"`
	Counts       []int64    `json:"counts"`
	Q20          []int64    `json:"q20"`
	PseudoCounts []int64    `json:"pseudoCounts"`
	PseudoQ20    []int64    `json:"pseudoQ20"`
	Rows         int64      `json:"rows"`
	Assigned     int64      `json:"assigned"`
	UnknownCells int64      `json:"unknownCells"`
	Bounds       [4]float32 `json:"bounds"`
	HasNucleus   bool       `json:"hasNucleus"`
}

func pointsFile(group int) string  { return "points-" + strconv.Itoa(group) + ".bin" }
func densityFile(level int) string { return "density-" + strconv.Itoa(level) + ".bin" }
func spillFile(group int) string   { return "spill-" + strconv.Itoa(group) }

// buildTranscripts builds the index. progress is called with the fraction done and the phase.
func buildTranscripts(ctx context.Context, b *building, p TranscriptParams, in TranscriptInput, progress func(float64, string)) (*TranscriptSummary, error) {
	if !p.Grid.valid() || p.DensityBins < 2 || p.DensityBins%2 != 0 || p.DensityBins*p.DensityBins > math.MaxUint16+1 || p.GroupRows < 2 || p.GroupRows%2 != 0 {
		return nil, fmt.Errorf("invalid transcript index parameters %+v", p)
	}
	groups := p.groups()
	if p.GroupRows*p.Grid.Across(0) > math.MaxUint16 {
		return nil, fmt.Errorf("the image is too wide to index (%d tiles across)", p.Grid.Across(0))
	}
	// Phase 1: the scan.
	var scan scanState
	raw, err := os.ReadFile(b.path("scan.json"))
	if err == nil && json.Unmarshal(raw, &scan) == nil {
		for g := 0; g < groups; g++ {
			if !b.done(pointsFile(g)) && !b.done(spillFile(g)) {
				err = fmt.Errorf("spill file %d is missing", g)
			}
		}
	}
	if err != nil {
		for g := 0; g < groups; g++ {
			b.remove(spillFile(g), pointsFile(g))
		}
		if scan, err = scanTranscripts(ctx, b, p, in, func(f float64) { progress(0.5*f, "Reading the transcripts") }); err != nil {
			return nil, err
		}
	}
	if len(scan.Features) > maxGenes {
		return nil, fmt.Errorf("%d features: the index holds at most %d", len(scan.Features), maxGenes)
	}
	// The genes' final order, and each provisional index's place in it.
	order := make([]int, len(scan.Features))
	for i := range order {
		order[i] = i
	}
	kindOf := func(i int) int {
		if k, ok := kindOrder[xenium.FeatureKind(scan.Features[i])]; ok {
			return k
		}
		return len(kindOrder)
	}
	sort.Slice(order, func(i, j int) bool {
		a, c := order[i], order[j]
		if ka, kc := kindOf(a), kindOf(c); ka != kc {
			return ka < kc
		}
		return scan.Features[a] < scan.Features[c]
	})
	final := make([]uint16, len(order))
	sum := &TranscriptSummary{Transcripts: scan.Rows, Assigned: scan.Assigned, UnknownCells: scan.UnknownCells, Bounds: scan.Bounds, HasNucleus: scan.HasNucleus, Groups: groups}
	for place, i := range order {
		final[i] = uint16(place)
		sum.Genes = append(sum.Genes, Gene{Name: scan.Features[i], Kind: xenium.FeatureKind(scan.Features[i]), Count: scan.Counts[i], Q20: scan.Q20[i]})
		sum.Q20 += scan.Q20[i]
	}
	if len(scan.PseudoCounts) != PseudoCount || len(scan.PseudoQ20) != PseudoCount {
		return nil, errDamaged
	}
	for k, name := range PseudoNames {
		sum.Pseudo = append(sum.Pseudo, Gene{Name: name, Kind: "pseudo", Count: scan.PseudoCounts[k], Q20: scan.PseudoQ20[k]})
	}
	// Phase 2: each group's points.
	for g := 0; g < groups; g++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !b.done(pointsFile(g)) {
			if err := writePointsGroup(b, p, g, final, len(sum.Genes)); err != nil {
				return nil, fmt.Errorf("points of group %d: %w", g, err)
			}
		}
		progress(0.5+0.3*float64(g+1)/float64(groups), "Sorting the transcripts into tiles")
	}
	for g := 0; g < groups; g++ {
		b.remove(spillFile(g))
	}
	// Phase 3: density, level by level.
	for l := 1; l < p.Grid.Levels; l++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !b.done(densityFile(l)) {
			if err := writeDensityLevel(ctx, b, p, l, sum.Genes); err != nil {
				return nil, fmt.Errorf("density of level %d: %w", l, err)
			}
		}
		sum.DensityLevels = append(sum.DensityLevels, l)
		progress(0.8+0.2*float64(l)/float64(p.Grid.Levels-1), "Counting the transcripts of each gene")
	}
	b.remove("scan.json")
	return sum, nil
}

// scanTranscripts reads every transcript once, writing each to its group's spill file.
func scanTranscripts(ctx context.Context, b *building, p TranscriptParams, in TranscriptInput, progress func(float64)) (scanState, error) {
	groups := p.groups()
	s := scanState{Bounds: [4]float32{float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.Inf(-1))},
		PseudoCounts: make([]int64, PseudoCount), PseudoQ20: make([]int64, PseudoCount)}
	var kinds []string // by provisional gene
	var pseudo []int
	files := make([]*os.File, groups)
	writers := make([]*bufio.Writer, groups)
	closeAll := func() {
		for _, f := range files {
			if f != nil {
				f.Close()
			}
		}
	}
	for g := range files {
		f, err := b.create(spillFile(g))
		if err != nil {
			closeAll()
			return s, err
		}
		files[g], writers[g] = f, bufio.NewWriterSize(f, 1<<16)
	}
	features := map[string]uint16{}
	var rec [spillRecord]byte
	err := in.Each(func(batch *xenium.TranscriptBatch) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if batch.OverlapsNucleus != nil {
			s.HasNucleus = true
		}
		for i := 0; i < batch.Rows; i++ {
			name := batch.Feature[i]
			gene, ok := features[string(name)]
			if !ok {
				if len(s.Features) >= maxGenes {
					return fmt.Errorf("more than %d features", maxGenes)
				}
				gene = uint16(len(s.Features))
				features[string(name)] = gene
				s.Features = append(s.Features, string(name))
				s.Counts, s.Q20 = append(s.Counts, 0), append(s.Q20, 0)
				kinds = append(kinds, xenium.FeatureKind(string(name)))
			}
			x, y, z, qv := batch.X[i], batch.Y[i], batch.Z[i], batch.QV[i]
			cell := uint32(NoCell)
			if id := batch.CellID[i]; string(id) != "UNASSIGNED" {
				if c, ok := in.CellIndex[string(id)]; ok {
					cell = uint32(c)
					s.Assigned++
				} else {
					s.UnknownCells++
				}
			}
			var flags byte
			if batch.OverlapsNucleus != nil && batch.OverlapsNucleus[i] != 0 {
				flags |= 1
			}
			s.Counts[gene]++
			if qv >= 20 {
				s.Q20[gene]++
			}
			pseudo = pseudoOf(kinds[gene], cell != NoCell, pseudo)
			for _, k := range pseudo {
				s.PseudoCounts[k]++
				if qv >= 20 {
					s.PseudoQ20[k]++
				}
			}
			s.Bounds[0], s.Bounds[1] = min(s.Bounds[0], x), min(s.Bounds[1], y)
			s.Bounds[2], s.Bounds[3] = max(s.Bounds[2], x), max(s.Bounds[3], y)
			if s.Rows >= math.MaxUint32 {
				return fmt.Errorf("more than %d transcripts", uint32(math.MaxUint32))
			}
			le.PutUint32(rec[0:], uint32(s.Rows))
			le.PutUint32(rec[4:], math.Float32bits(x))
			le.PutUint32(rec[8:], math.Float32bits(y))
			le.PutUint32(rec[12:], math.Float32bits(z))
			le.PutUint32(rec[16:], math.Float32bits(qv))
			le.PutUint32(rec[20:], cell)
			le.PutUint16(rec[24:], gene)
			rec[26], rec[27] = flags, 0
			_, ty := p.Grid.TileOf(0, float64(x), float64(y))
			if _, err := writers[ty/p.GroupRows].Write(rec[:]); err != nil {
				return err
			}
			s.Rows++
		}
		if in.Rows > 0 {
			progress(float64(s.Rows) / float64(in.Rows))
		}
		return nil
	})
	if err != nil {
		closeAll()
		return s, err
	}
	if s.Rows == 0 {
		s.Bounds = [4]float32{}
	}
	for g := range files {
		if err := writers[g].Flush(); err != nil {
			closeAll()
			return s, err
		}
		if err := b.commitFile(files[g], spillFile(g)); err != nil {
			files[g] = nil
			closeAll()
			return s, err
		}
		files[g] = nil
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return s, err
	}
	return s, b.writeFile("scan.json", raw)
}

// point is one transcript while its group is sorted.
type point struct {
	row         uint32
	x, y, z, qv float32
	cell        uint32
	gene, tile  uint16
	flags       uint8
}

// writePointsGroup sorts one group's spilled transcripts into its tiles.
func writePointsGroup(b *building, p TranscriptParams, g int, final []uint16, genes int) error {
	raw, err := os.ReadFile(b.path(spillFile(g)))
	if err != nil {
		return err
	}
	if len(raw)%spillRecord != 0 {
		return errDamaged
	}
	row0 := g * p.GroupRows
	rows := min(p.GroupRows, p.Grid.Down(0)-row0)
	across := p.Grid.Across(0)
	pts := make([]point, len(raw)/spillRecord)
	for i := range pts {
		r := raw[i*spillRecord:]
		pt := point{
			row: le.Uint32(r), x: math.Float32frombits(le.Uint32(r[4:])), y: math.Float32frombits(le.Uint32(r[8:])),
			z: math.Float32frombits(le.Uint32(r[12:])), qv: math.Float32frombits(le.Uint32(r[16:])), cell: le.Uint32(r[20:]),
			flags: r[26],
		}
		gene := int(le.Uint16(r[24:]))
		if gene >= len(final) {
			return errDamaged
		}
		pt.gene = final[gene]
		tx, ty := p.Grid.TileOf(0, float64(pt.x), float64(pt.y))
		if ty < row0 || ty >= row0+rows {
			return errDamaged
		}
		pt.tile = uint16((ty-row0)*across + tx)
		pts[i] = pt
	}
	raw = nil
	slices.SortFunc(pts, func(a, c point) int {
		if a.tile != c.tile {
			return cmp.Compare(a.tile, c.tile)
		}
		if a.gene != c.gene {
			return cmp.Compare(a.gene, c.gene)
		}
		return cmp.Compare(a.row, c.row)
	})
	f, err := b.create(pointsFile(g))
	if err != nil {
		return err
	}
	w, err := newTableWriter(f, pointsMagic, [6]uint32{uint32(g), uint32(row0), uint32(rows), uint32(across), uint32(genes)}, rows*across)
	if err != nil {
		f.Close()
		return err
	}
	for start := 0; start < len(pts); {
		end := start
		for end < len(pts) && pts[end].tile == pts[start].tile {
			end++
		}
		block, present := encodePoints(pts[start:end])
		if block, err = deflate(block); err != nil {
			f.Close()
			return err
		}
		if err := w.block(int(pts[start].tile), block, uint32(end-start), uint32(present)); err != nil {
			f.Close()
			return err
		}
		start = end
	}
	if err := w.finish(); err != nil {
		f.Close()
		return err
	}
	return b.commitFile(f, pointsFile(g))
}

// encodePoints writes one tile's points (sorted by gene) as a block.
func encodePoints(pts []point) ([]byte, int) {
	var genes []uint16
	var starts []uint32
	for i, pt := range pts {
		if i == 0 || pt.gene != pts[i-1].gene {
			genes = append(genes, pt.gene)
			starts = append(starts, uint32(i))
		}
	}
	starts = append(starts, uint32(len(pts)))
	n := len(pts)
	x, y, z, qv := make([]float32, n), make([]float32, n), make([]float32, n), make([]float32, n)
	cell, row := make([]uint32, n), make([]uint32, n)
	flags := make([]uint8, n)
	for i, pt := range pts {
		x[i], y[i], z[i], qv[i], cell[i], row[i], flags[i] = pt.x, pt.y, pt.z, pt.qv, pt.cell, pt.row, pt.flags
	}
	var c columns
	c.u32(starts)
	c.f32(x)
	c.f32(y)
	c.f32(z)
	c.f32(qv)
	c.u32(cell)
	c.u32(row)
	c.u16(genes)
	c.u8(flags)
	c.pad()
	return c.buf, len(genes)
}

// PointTile is one tile's transcripts.
type PointTile struct {
	// Genes are the genes present; the points of Genes[i] are Starts[i] to Starts[i+1].
	Genes       []uint16
	Starts      []uint32
	X, Y, Z, QV []float32
	Cell, Row   []uint32
	Flags       []uint8
}

// Len is the number of points.
func (t *PointTile) Len() int { return len(t.X) }

func decodePoints(block []byte, e tableEntry, genes int) (*PointTile, error) {
	n, g := int(e.A), int(e.B)
	if n == 0 {
		if g != 0 || block != nil {
			return nil, errDamaged
		}
		return &PointTile{Starts: []uint32{0}}, nil
	}
	raw, err := inflate(block, pad4(6*int64(g)+25*int64(n)+4))
	if err != nil {
		return nil, err
	}
	r := &reader{buf: raw}
	t := &PointTile{Starts: r.u32(g + 1)}
	t.X, t.Y, t.Z, t.QV = r.f32(n), r.f32(n), r.f32(n), r.f32(n)
	t.Cell, t.Row = r.u32(n), r.u32(n)
	t.Genes = r.u16(g)
	t.Flags = r.u8(n)
	r.pad()
	if err := r.end(); err != nil {
		return nil, err
	}
	if g == 0 || offsets(t.Starts, n) != nil {
		return nil, errDamaged
	}
	for i, gene := range t.Genes {
		if int(gene) >= genes || (i > 0 && gene <= t.Genes[i-1]) || t.Starts[i] == t.Starts[i+1] {
			return nil, errDamaged
		}
	}
	return t, nil
}

// densityKey packs a gene and a bin.
func densityKey(gene uint16, bin uint16) uint32 { return uint32(gene)<<16 | uint32(bin) }

type counts struct{ n, q20 uint32 }

// writeDensityLevel writes one level's density grids: level 1 from the points, coarser levels
// from the level below.
func writeDensityLevel(ctx context.Context, b *building, p TranscriptParams, level int, geneList []Gene) error {
	genes := len(geneList)
	across, down, bins := p.Grid.Across(level), p.Grid.Down(level), p.DensityBins
	var points []*tableFile
	var below *tableFile
	var err error
	if level == 1 {
		points = make([]*tableFile, p.groups())
		for g := range points {
			if points[g], err = openPointsFile(b.path(pointsFile(g))); err != nil {
				break
			}
		}
		defer func() {
			for _, t := range points {
				if t != nil {
					t.Close()
				}
			}
		}()
	} else {
		if below, err = openDensityFile(b.path(densityFile(level - 1))); err == nil {
			defer below.Close()
		}
	}
	if err != nil {
		return err
	}
	f, err := b.create(densityFile(level))
	if err != nil {
		return err
	}
	w, err := newTableWriter(f, densityMagic, [6]uint32{uint32(level), uint32(across), uint32(down), uint32(bins), uint32(genes)}, across*down)
	if err != nil {
		f.Close()
		return err
	}
	kinds := make([]string, genes)
	for i, g := range geneList {
		kinds[i] = g.Kind
	}
	var pseudo []int
	side := p.Grid.TileUm(level)
	for ty := 0; ty < down; ty++ {
		if err := ctx.Err(); err != nil {
			f.Close()
			return err
		}
		for tx := 0; tx < across; tx++ {
			acc := map[uint32]counts{}
			for j := 0; j < 2; j++ {
				for i := 0; i < 2; i++ {
					cx, cy := 2*tx+i, 2*ty+j
					if level == 1 {
						if cx >= p.Grid.Across(0) || cy >= p.Grid.Down(0) {
							continue
						}
						t := points[cy/p.GroupRows]
						block, e, err := t.block((cy-int(t.Fields[1]))*int(t.Fields[3]) + cx)
						if err != nil {
							f.Close()
							return err
						}
						pts, err := decodePoints(block, e, genes)
						if err != nil {
							f.Close()
							return err
						}
						x0, y0 := float64(tx)*side, float64(ty)*side
						for k, gene := range pts.Genes {
							for at := pts.Starts[k]; at < pts.Starts[k+1]; at++ {
								bx := clampInt(int(math.Floor((float64(pts.X[at])-x0)*float64(bins)/side)), 0, bins-1)
								by := clampInt(int(math.Floor((float64(pts.Y[at])-y0)*float64(bins)/side)), 0, bins-1)
								bin := uint16(by*bins + bx)
								var q uint32
								if pts.QV[at] >= 20 {
									q = 1
								}
								add := func(key uint32) {
									c := acc[key]
									c.n++
									c.q20 += q
									acc[key] = c
								}
								add(densityKey(gene, bin))
								pseudo = pseudoOf(kinds[gene], pts.Cell[at] != NoCell, pseudo)
								for _, k := range pseudo {
									add(densityKey(uint16(genes+k), bin))
								}
							}
						}
						continue
					}
					if cx >= int(below.Fields[1]) || cy >= int(below.Fields[2]) {
						continue
					}
					block, e, err := below.block(cy*int(below.Fields[1]) + cx)
					if err != nil {
						f.Close()
						return err
					}
					d, err := decodeDensity(block, e, genes, bins)
					if err != nil {
						f.Close()
						return err
					}
					half := bins / 2
					for k, gene := range d.Genes {
						for at := d.Starts[k]; at < d.Starts[k+1]; at++ {
							cbx, cby := int(d.Bins[at])%bins, int(d.Bins[at])/bins
							bin := uint16((cby/2+j*half)*bins + cbx/2 + i*half)
							key := densityKey(gene, bin)
							c := acc[key]
							c.n += d.N[at]
							c.q20 += d.Q20[at]
							acc[key] = c
						}
					}
				}
			}
			block, present := encodeDensity(acc)
			if block != nil {
				if block, err = deflate(block); err != nil {
					f.Close()
					return err
				}
			}
			if err := w.block(ty*across+tx, block, uint32(len(acc)), uint32(present)); err != nil {
				f.Close()
				return err
			}
		}
	}
	if err := w.finish(); err != nil {
		f.Close()
		return err
	}
	return b.commitFile(f, densityFile(level))
}

// encodeDensity writes one tile's (gene, bin) counts as a block, sorted by gene and bin.
func encodeDensity(acc map[uint32]counts) ([]byte, int) {
	if len(acc) == 0 {
		return nil, 0
	}
	keys := make([]uint32, 0, len(acc))
	for k := range acc {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var genes []uint16
	var starts []uint32
	n, q20 := make([]uint32, len(keys)), make([]uint32, len(keys))
	bins := make([]uint16, len(keys))
	for i, k := range keys {
		gene := uint16(k >> 16)
		if i == 0 || gene != genes[len(genes)-1] {
			genes = append(genes, gene)
			starts = append(starts, uint32(i))
		}
		n[i], q20[i], bins[i] = acc[k].n, acc[k].q20, uint16(k)
	}
	starts = append(starts, uint32(len(keys)))
	var c columns
	c.u32(starts)
	c.u32(n)
	c.u32(q20)
	c.u16(genes)
	c.u16(bins)
	c.pad()
	return c.buf, len(genes)
}

// DensityTile is one tile's density grids.
type DensityTile struct {
	// Genes are the genes present (indexes from the number of genes on are pseudo-genes); the bins
	// of Genes[i] are entries Starts[i] to Starts[i+1].
	Genes  []uint16
	Starts []uint32
	Bins   []uint16 // by*bins + bx
	N, Q20 []uint32
}

func decodeDensity(block []byte, e tableEntry, genes, bins int) (*DensityTile, error) {
	n, g := int(e.A), int(e.B)
	if n == 0 {
		if g != 0 || block != nil {
			return nil, errDamaged
		}
		return &DensityTile{Starts: []uint32{0}}, nil
	}
	raw, err := inflate(block, pad4(6*int64(g)+10*int64(n)+4))
	if err != nil {
		return nil, err
	}
	r := &reader{buf: raw}
	d := &DensityTile{Starts: r.u32(g + 1)}
	d.N, d.Q20 = r.u32(n), r.u32(n)
	d.Genes, d.Bins = r.u16(g), r.u16(n)
	r.pad()
	if err := r.end(); err != nil {
		return nil, err
	}
	if g == 0 || offsets(d.Starts, n) != nil {
		return nil, errDamaged
	}
	for i, gene := range d.Genes {
		if int(gene) >= genes+PseudoCount || (i > 0 && gene <= d.Genes[i-1]) || d.Starts[i] == d.Starts[i+1] {
			return nil, errDamaged
		}
	}
	for i, bin := range d.Bins {
		if int(bin) >= bins*bins || d.Q20[i] > d.N[i] || d.N[i] == 0 {
			return nil, errDamaged
		}
	}
	return d, nil
}

func openPointsFile(path string) (*tableFile, error) {
	return openTableFile(path, pointsMagic, func(f [6]uint32) (int, error) {
		if f[2] == 0 || f[3] == 0 || uint64(f[2])*uint64(f[3]) > math.MaxUint16+1 {
			return 0, errDamaged
		}
		return int(f[2]) * int(f[3]), nil
	})
}

func openDensityFile(path string) (*tableFile, error) {
	return openTableFile(path, densityMagic, func(f [6]uint32) (int, error) {
		if f[1] == 0 || f[2] == 0 || uint64(f[1])*uint64(f[2]) > 1<<24 || f[3] < 2 || f[3]*f[3] > math.MaxUint16+1 {
			return 0, errDamaged
		}
		return int(f[1]) * int(f[2]), nil
	})
}

// Transcripts is an open transcript index.
type Transcripts struct {
	Summary TranscriptSummary
	Params  TranscriptParams
	dir     string
	points  []*tableFile
	density map[int]*tableFile
}

// OpenTranscripts opens a complete transcript index.
func OpenTranscripts(dir string) (*Transcripts, error) {
	m, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	t := &Transcripts{dir: dir, density: map[int]*tableFile{}}
	if err := json.Unmarshal(m.Summary, &t.Summary); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(m.Params, &t.Params); err != nil {
		return nil, err
	}
	if !t.Params.Grid.valid() || t.Params.GroupRows < 2 || t.Summary.Groups != t.Params.groups() {
		return nil, errDamaged
	}
	t.points = make([]*tableFile, t.Summary.Groups)
	for g := range t.points {
		f, err := openPointsFile(filepath.Join(dir, pointsFile(g)))
		if err != nil {
			t.Close()
			return nil, err
		}
		t.points[g] = f
		if int(f.Fields[0]) != g || int(f.Fields[1]) != g*t.Params.GroupRows || int(f.Fields[3]) != t.Params.Grid.Across(0) || int(f.Fields[4]) != len(t.Summary.Genes) {
			t.Close()
			return nil, errDamaged
		}
	}
	for _, l := range t.Summary.DensityLevels {
		f, err := openDensityFile(filepath.Join(dir, densityFile(l)))
		if err != nil {
			t.Close()
			return nil, err
		}
		t.density[l] = f
		if int(f.Fields[0]) != l || int(f.Fields[1]) != t.Params.Grid.Across(l) || int(f.Fields[2]) != t.Params.Grid.Down(l) || int(f.Fields[3]) != t.Params.DensityBins || int(f.Fields[4]) != len(t.Summary.Genes) {
			t.Close()
			return nil, errDamaged
		}
	}
	return t, nil
}

// Points reads level-0 tile (tx, ty)'s transcripts.
func (t *Transcripts) Points(tx, ty int) (*PointTile, error) {
	g := t.Params.Grid
	if tx < 0 || ty < 0 || tx >= g.Across(0) || ty >= g.Down(0) {
		return nil, fmt.Errorf("no tile (%d, %d)", tx, ty)
	}
	f := t.points[ty/t.Params.GroupRows]
	block, e, err := f.block((ty-int(f.Fields[1]))*int(f.Fields[3]) + tx)
	if err != nil {
		return nil, err
	}
	return decodePoints(block, e, len(t.Summary.Genes))
}

// Density reads tile (tx, ty)'s density grids at a level of DensityLevels.
func (t *Transcripts) Density(level, tx, ty int) (*DensityTile, error) {
	f, ok := t.density[level]
	if !ok {
		return nil, fmt.Errorf("no density grids at level %d", level)
	}
	if tx < 0 || ty < 0 || tx >= int(f.Fields[1]) || ty >= int(f.Fields[2]) {
		return nil, fmt.Errorf("no tile (%d, %d) at level %d", tx, ty, level)
	}
	block, e, err := f.block(ty*int(f.Fields[1]) + tx)
	if err != nil {
		return nil, err
	}
	return decodeDensity(block, e, len(t.Summary.Genes), t.Params.DensityBins)
}

// Close releases the index's files.
func (t *Transcripts) Close() error {
	for _, f := range t.points {
		if f != nil {
			f.Close()
		}
	}
	for _, f := range t.density {
		f.Close()
	}
	return nil
}
