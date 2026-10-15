package simulate

// The simulated instrument: it sees the nuclei in the section and segments by growing each up to
// the expansion distance, every pixel going to the nucleus nearest it. Distances are a chamfer
// transform with the 5-7-11 mask, which approximates Euclidean distance to within 2% (Borgefors
// G. Distance transformations in digital images. Comput Vis Graph Image Process 1986;34:344–371):
// an instrument's own approximation, deliberately not SituCraft's exact expansion. Ties go to
// the nucleus reached first in the passes' order. Its cells are those of the visible nuclei, in
// the true cells' order; transcripts go to the cell holding the pixel they are observed in.

import "math"

// chamfer offsets: the forward pass's neighbours (above and to the left) and their weights; the
// backward pass mirrors them.
var chamfer = [...]struct{ dx, dy, w int32 }{
	{-1, 0, 5}, {0, -1, 5}, {-1, -1, 7}, {1, -1, 7},
	{-2, -1, 11}, {-1, -2, 11}, {1, -2, 11}, {2, -1, 11},
}

// instrument is the simulated instrument's segmentation.
type instrument struct {
	cells    []int    // the true cells (indices) it has, in order
	labels   []uint32 // each pixel's instrument cell (k for cells[k-1])
	nuclei   []uint32 // each pixel's nucleus, by instrument cell
	fromTrue []uint32 // a true cell's label → its instrument label (0: none)
}

func (t *tissue) segmentLikeTheInstrument() *instrument {
	in := &instrument{fromTrue: make([]uint32, len(t.cells)+1)}
	for k, c := range t.cells {
		if c.nucleus {
			in.cells = append(in.cells, k)
			in.fromTrue[k+1] = uint32(len(in.cells))
		}
	}
	n := t.w * t.h
	const far = math.MaxInt32 / 2
	dist := make([]int32, n)
	in.labels = make([]uint32, n)
	in.nuclei = make([]uint32, n)
	for i, l := range t.nuclei {
		if l != 0 {
			in.nuclei[i] = in.fromTrue[l]
			in.labels[i] = in.nuclei[i]
		} else {
			dist[i] = far
		}
	}
	w, h := int32(t.w), int32(t.h)
	relax := func(x, y int32, sign int32) {
		i := y*w + x
		for _, o := range chamfer {
			nx, ny := x+sign*o.dx, y+sign*o.dy
			if nx < 0 || ny < 0 || nx >= w || ny >= h {
				continue
			}
			j := ny*w + nx
			if d := dist[j] + o.w; d < dist[i] {
				dist[i], in.labels[i] = d, in.labels[j]
			}
		}
	}
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			relax(x, y, 1)
		}
	}
	for y := h - 1; y >= 0; y-- {
		for x := w - 1; x >= 0; x-- {
			relax(x, y, -1)
		}
	}
	limit := int32(math.Round(5 * t.p.ExpansionUm / t.p.PixelUm))
	for i := range dist {
		if dist[i] > limit {
			in.labels[i] = 0
		}
	}
	return in
}
