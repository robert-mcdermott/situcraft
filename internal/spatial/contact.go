package spatial

// Contact graphs: two cells are in contact where their pixels touch in the segmentation's label
// raster, side by side or one above the other (not corner to corner). The boundary they share is the
// number of pixel edges between them times the pixel's side, in µm: a slanted boundary is counted
// as the staircase of pixel edges along it, longer than the line by up to √2 (4/π on average for
// boundaries of every direction), the same measure for every cell. With a gap, each cell is first
// grown into the pixels of no cell by up to half the gap (each such pixel to the nearest cell,
// internal/segment/expand), so that cells at most the gap apart touch; a cell between two others
// still keeps them apart. Every cell of the segmentation is in the raster, so a cell left out of
// the graph still stands between those on either side of it.

import (
	"context"
	"fmt"

	"situcraft/internal/segment"
	"situcraft/internal/segment/expand"
)

// rasterSource gives a label raster's rows, as the expansion reads them.
type rasterSource struct{ r *segment.Raster }

func (s rasterSource) Size() (int, int) { return s.r.Width, s.r.Height }

func (s rasterSource) Rows(y0, y1 int, dst []uint32) error {
	for y := y0; y < y1; y++ {
		row := dst[(y-y0)*s.r.Width : (y-y0+1)*s.r.Width]
		x := 0
		for _, run := range s.r.Rows[y] {
			for k := uint32(0); k < run.Length; k++ {
				row[x] = run.Label
				x++
			}
		}
	}
	return nil
}

func contactPairs(ctx context.Context, in Inputs, g *Graph, gap float64, workers int) ([]pair, error) {
	mask := in.Mask
	if !(mask.PixelSize > 0) || len(mask.Rows) != mask.Height {
		return nil, fmt.Errorf("spatial: the label raster has no pixel size or lacks rows")
	}
	if gap > 0 {
		grown, _, err := expand.Expand(ctx, rasterSource{mask}, expand.Options{Distance: gap / 2 / mask.PixelSize, Workers: workers})
		if err != nil {
			return nil, err
		}
		grown.PixelSize = mask.PixelSize
		mask = grown
	}
	// Each label's cell among the graph's (-1: not in it).
	index := make([]int32, len(in.X)+1)
	for i := range index {
		index[i] = -1
	}
	for k, row := range g.Rows {
		index[row+1] = int32(k)
	}
	type key struct{ i, j uint32 }
	counts := make([]map[key]uint64, workers)
	bad := make([]uint32, workers)
	parallelFor(mask.Height, workers, func(w, a, b int) {
		c := map[key]uint64{}
		counts[w] = c
		touch := func(p, q uint32, n uint32) {
			if p == q || p == 0 || q == 0 {
				return
			}
			if int(p) >= len(index) || int(q) >= len(index) {
				bad[w] = max(p, q)
				return
			}
			i, j := index[p], index[q]
			if i < 0 || j < 0 {
				return
			}
			c[key{uint32(min(i, j)), uint32(max(i, j))}] += uint64(n)
		}
		for y := a; y < b; y++ {
			if (y-a)%256 == 0 && ctx.Err() != nil {
				return
			}
			row := mask.Rows[y]
			for k := 1; k < len(row); k++ {
				touch(row[k-1].Label, row[k].Label, 1)
			}
			if y+1 >= mask.Height {
				continue
			}
			// The runs of rows y and y+1 walked together: each stretch where both keep their labels.
			below := mask.Rows[y+1]
			var x, xa, xb uint32
			ka, kb := 0, 0
			for ka < len(row) && kb < len(below) {
				endA, endB := xa+row[ka].Length, xb+below[kb].Length
				end := min(endA, endB)
				touch(row[ka].Label, below[kb].Label, end-x)
				x = end
				if endA == end {
					xa = endA
					ka++
				}
				if endB == end {
					xb = endB
					kb++
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, l := range bad {
		if l != 0 {
			return nil, fmt.Errorf("spatial: the label raster holds label %d, beyond the segmentation's %d cells", l, len(in.X))
		}
	}
	total := map[key]uint64{}
	for _, c := range counts {
		for k, n := range c {
			total[k] += n
		}
	}
	pairs := make([]pair, 0, len(total))
	for k, n := range total {
		pairs = append(pairs, pair{i: k.i, j: k.j, shared: float64(n) * mask.PixelSize})
	}
	return pairs, nil
}
