package refine

// A run's inputs from a Xenium output bundle: its gene transcripts of a quality or more, and its
// negative controls (what they say of false decodes, research.md §3.2, the background); the
// instrument's cells (cells.zarr.zip masks/1, label k cells.parquet's row k−1) to start from, each
// seeded at its nucleus's centre; and each transcript over a nucleus (masks/0) given that
// nucleus's cell, the instrument's cell holding most of its pixels, as its prior.

import (
	"math"
	"sort"

	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// BundleIndex relates a run's inputs to its bundle: the genes' names, each transcript's row in
// transcripts.parquet, and each cell's id.
type BundleIndex struct {
	Genes   []string
	Rows    []int
	CellIDs []string
	// Outside counts the gene transcripts beyond the image, left out; Controls the rows of other
	// features (negative controls, unassigned codewords); LowQuality the gene transcripts below the
	// quality; WithPrior the modelled transcripts over a nucleus, which have its cell as their
	// prior.
	Outside    int
	Controls   int
	LowQuality int
	WithPrior  int
	// Total is transcripts.parquet's rows; RowGroup the most in one of its row groups (read at
	// once); MaskBlock the pixels the masks' readers decode at once (their chunks' rows across).
	Total     int
	RowGroup  int
	MaskBlock int
}

// readMask reads a mask whole, as runs.
func readMask(b *xenium.Bundle, which int) (*segment.Raster, error) {
	r, _, err := readMaskBlock(b, which)
	return r, err
}

// readMaskBlock reads a mask whole, as runs, answering the pixels its reader decodes at once.
func readMaskBlock(b *xenium.Bundle, which int) (*segment.Raster, int, error) {
	m, err := b.OpenMask(which)
	if err != nil {
		return nil, 0, err
	}
	defer m.Close()
	w, h := m.Size()
	block := min(h, m.ChunkRows()) * w
	r := &segment.Raster{Width: w, Height: h, PixelSize: m.PixelSize, Rows: make([][]segment.Run, h)}
	row := make([]uint32, w)
	for y := 0; y < h; y++ {
		if err := m.Rows(y, y+1, row); err != nil {
			return nil, 0, err
		}
		r.Rows[y] = segment.EncodeRow(row)
	}
	return r, block, nil
}

// BundleInputs builds a run's inputs from a bundle: its gene transcripts of quality minQV or more.
func BundleInputs(b *xenium.Bundle, minQV float64) (*Inputs, *BundleIndex, error) {
	if !b.Has("cells.zarr.zip") {
		return nil, nil, fail(ClassPrior, remedyPrior, "the bundle has no cells.zarr.zip: the instrument's cells and nuclei the refinement starts from")
	}
	cells, err := b.ReadCells()
	if err != nil {
		return nil, nil, fail(ClassInput, remedyInput, "cells.parquet: %w", err)
	}
	cellMask, block, err := readMaskBlock(b, xenium.CellMask)
	if err != nil {
		return nil, nil, fail(ClassPrior, remedyPrior, "the cell mask (cells.zarr.zip masks/1): %w", err)
	}
	nucMask, err := readMask(b, xenium.NucleusMask)
	if err != nil {
		return nil, nil, fail(ClassPrior, remedyPrior, "the nucleus mask (cells.zarr.zip masks/0): %w", err)
	}
	if nucMask.Width != cellMask.Width || nucMask.Height != cellMask.Height {
		return nil, nil, fail(ClassInput, remedyInput, "the nucleus and cell masks differ in size")
	}
	n := len(cells.IDs)
	ps := cellMask.PixelSize
	// Each cell's pixels' sums (its centre without a nucleus), each nucleus's sums and votes for
	// the cell holding it.
	type sums struct {
		n      int64
		sx, sy float64
	}
	cellSum := make([]sums, n+1)
	nucSum := map[uint32]*sums{}
	votes := map[uint64]int64{}
	crow, nrow := make([]uint32, cellMask.Width), make([]uint32, cellMask.Width)
	boxes := make([][4]int, n+1)
	for c := range boxes {
		boxes[c] = [4]int{cellMask.Width, cellMask.Height, -1, -1}
	}
	for y := 0; y < cellMask.Height; y++ {
		cellMask.Row(y, crow)
		nucMask.Row(y, nrow)
		for x, c := range crow {
			if int(c) > n {
				return nil, nil, fail(ClassInput, remedyInput, "the cell mask has a cell %d, but cells.parquet has %d cells", c, n)
			}
			if c > 0 {
				s := &cellSum[c]
				s.n++
				s.sx += float64(x)
				s.sy += float64(y)
				box := &boxes[c]
				box[0], box[1] = min(box[0], x), min(box[1], y)
				box[2], box[3] = max(box[2], x), max(box[3], y)
			}
			if k := nrow[x]; k > 0 {
				s := nucSum[k]
				if s == nil {
					s = &sums{}
					nucSum[k] = s
				}
				s.n++
				s.sx += float64(x)
				s.sy += float64(y)
				votes[uint64(k)<<32|uint64(c)]++
			}
		}
	}
	owner := map[uint32]uint32{}
	best := map[uint32]int64{}
	for key, v := range votes {
		k, c := uint32(key>>32), uint32(key)
		if c == 0 {
			continue
		}
		if v > best[k] || v == best[k] && c < owner[k] {
			owner[k], best[k] = c, v
		}
	}
	in := &Inputs{WidthUm: mul(float64(cellMask.Width), ps), HeightUm: mul(float64(cellMask.Height), ps), Cells: n, Start: cellMask,
		SeedX: make([]float64, n), SeedY: make([]float64, n), NucleusUm2: make([]float64, n), Box: make([]Region, n), MinQV: minQV}
	for c := 1; c <= n; c++ {
		if b := boxes[c]; b[2] >= 0 {
			in.Box[c-1] = Region{mul(float64(b[0]), ps), mul(float64(b[1]), ps), mul(float64(b[2]+1), ps), mul(float64(b[3]+1), ps)}
		}
	}
	// Seeds: the centre of the cell's largest nucleus, else of the cell.
	seedPixels := make([]int64, n+1)
	for c := 1; c <= n; c++ {
		if s := cellSum[c]; s.n > 0 {
			in.SeedX[c-1], in.SeedY[c-1] = mul(s.sx/float64(s.n)+0.5, ps), mul(s.sy/float64(s.n)+0.5, ps)
		}
	}
	nuclei := make([]uint32, 0, len(nucSum))
	for k := range nucSum {
		nuclei = append(nuclei, k)
	}
	sort.Slice(nuclei, func(i, j int) bool { return nuclei[i] < nuclei[j] })
	for _, k := range nuclei {
		if c := owner[k]; c > 0 {
			in.NucleusUm2[c-1] += mul(float64(nucSum[k].n), mul(ps, ps))
		}
	}
	for _, k := range nuclei {
		s := nucSum[k]
		c := owner[k]
		if c == 0 || s.n < seedPixels[c] || s.n == seedPixels[c] && seedPixels[c] > 0 {
			continue
		}
		seedPixels[c] = s.n
		in.SeedX[c-1], in.SeedY[c-1] = mul(s.sx/float64(s.n)+0.5, ps), mul(s.sy/float64(s.n)+0.5, ps)
	}
	// Transcripts: gene transcripts of the quality or more, in the file's order.
	tr, err := b.OpenTranscripts()
	if err != nil {
		return nil, nil, err
	}
	defer tr.Close()
	tr.Only("feature_name", "qv", "x_location", "y_location", "z_location")
	idx := &BundleIndex{CellIDs: cells.IDs, MaskBlock: block}
	for _, g := range tr.File.RowGroups {
		idx.RowGroup = max(idx.RowGroup, int(g.NumRows))
	}
	genes := map[string]uint32{}
	controlKinds := map[string]bool{}
	zMin, zMax := math.Inf(1), math.Inf(-1)
	row := 0
	err = tr.Each(func(batch *xenium.TranscriptBatch) error {
		for i := 0; i < batch.Rows; i++ {
			r := row
			row++
			name := string(batch.Feature[i])
			if xenium.FeatureKind(name) != xenium.KindGene {
				// A negative control (probe or codeword): a false decode, whatever its quality.
				idx.Controls++
				controlKinds[name] = true
				x, y := float64(batch.X[i]), float64(batch.Y[i])
				if float64(batch.QV[i]) >= minQV && x >= 0 && y >= 0 && x <= in.WidthUm && y <= in.HeightUm {
					in.ControlX, in.ControlY = append(in.ControlX, batch.X[i]), append(in.ControlY, batch.Y[i])
				}
				continue
			}
			if float64(batch.QV[i]) < minQV {
				idx.LowQuality++
				continue
			}
			x, y := float64(batch.X[i]), float64(batch.Y[i])
			if !(x >= 0 && y >= 0 && x <= in.WidthUm && y <= in.HeightUm) {
				idx.Outside++
				continue
			}
			g, ok := genes[name]
			if !ok {
				g = uint32(len(idx.Genes))
				genes[name] = g
				idx.Genes = append(idx.Genes, name)
			}
			prior := uint32(0)
			if k := nucMask.AtMicrons(x, y); k > 0 {
				prior = owner[k]
			}
			if prior > 0 {
				idx.WithPrior++
			}
			z := batch.Z[i]
			zMin, zMax = math.Min(zMin, float64(z)), math.Max(zMax, float64(z))
			t := &in.Transcripts
			t.X, t.Y, t.Z = append(t.X, batch.X[i]), append(t.Y, batch.Y[i]), append(t.Z, z)
			t.Gene, t.Prior = append(t.Gene, g), append(t.Prior, prior)
			idx.Rows = append(idx.Rows, r)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	idx.Total = row
	if len(idx.Rows) == 0 {
		return nil, nil, fail(ClassInput, "Lower the quality threshold, or check the bundle's transcripts in the Data view.", "the bundle has no gene transcripts of quality %g or more", minQV)
	}
	in.Genes = len(idx.Genes)
	in.ControlKinds = len(controlKinds)
	in.Z0, in.DepthUm = zMin, zMax-zMin
	return in, idx, nil
}
