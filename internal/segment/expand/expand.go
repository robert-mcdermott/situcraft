// Package expand is SituCraft's nucleus expansion: each nucleus is grown by up to a distance, and
// a pixel belongs to the nearest nucleus within that distance (a Voronoi partition of the plane by
// the nuclei, clipped at the distance: Okabe A, Boots B, Sugihara K, Chiu SN. Spatial
// Tessellations: Concepts and Applications of Voronoi Diagrams, 2nd ed. Wiley, 2000). Distances are
// Euclidean, between pixel centres, exact (edt.go); a nucleus's own pixels stay its own, and a
// pixel equally near two nuclei goes to the one met first (edt.go says which).
//
// The image is processed in bands of rows, each with a margin of the distance above and below, so
// that a whole slide fits in memory: within the margin lies every nucleus near enough to claim a
// pixel of the band, so each band's result is exact. Bands, and the columns and rows within them,
// are split among workers; the result is the same for any number of workers and any band height.
package expand

import (
	"context"
	"errors"
	"math"

	"situcraft/internal/segment"
)

// Source gives the rows of a nucleus label image (0: no nucleus; k: nucleus k).
type Source interface {
	Size() (width, height int)
	// Rows fills dst with rows y0..y1-1, row-major.
	Rows(y0, y1 int, dst []uint32) error
}

// Options say how far nuclei grow and how the work is done.
type Options struct {
	// Distance is how far a nucleus grows, in pixels.
	Distance float64
	// Workers share the work (1 if not set).
	Workers int
	// Band is the rows processed at once (chosen from the image's width if not set).
	Band int
	// Progress receives the fraction done.
	Progress func(float64)
}

// memoryPerBand bounds a band's working memory (about 20 bytes a pixel).
const memoryPerBand = 256 << 20

// Expand grows the nuclei of src and answers the label raster of the cells (label k is the cell
// grown from nucleus k) and the nuclei's own, as src gives them.
func Expand(ctx context.Context, src Source, opt Options) (cells, nuclei *segment.Raster, err error) {
	w, h := src.Size()
	if w <= 0 || h <= 0 {
		return nil, nil, errors.New("the nucleus image is empty")
	}
	if !(opt.Distance >= 0) || math.IsInf(opt.Distance, 0) {
		return nil, nil, errors.New("the expansion distance must be a finite number of pixels, at least 0")
	}
	margin := int(math.Ceil(opt.Distance)) + 1
	band := opt.Band
	if band <= 0 {
		band = max(64, min(1024, memoryPerBand/(20*w)-2*margin))
	}
	limit := opt.Distance * opt.Distance
	out := &segment.Raster{Width: w, Height: h, Rows: make([][]segment.Run, h)}
	nuc := &segment.Raster{Width: w, Height: h, Rows: make([][]segment.Run, h)}
	labels := make([]uint32, 0)
	for y0 := 0; y0 < h; y0 += band {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		y1 := min(h, y0+band)
		top, bottom := max(0, y0-margin), min(h, y1+margin)
		rows := bottom - top
		if cap(labels) < rows*w {
			labels = make([]uint32, rows*w)
		}
		labels = labels[:rows*w]
		if err := src.Rows(top, bottom, labels); err != nil {
			return nil, nil, err
		}
		site, dist2 := nearest(labels, w, rows, opt.Workers)
		parallel(y1-y0, opt.Workers, func(a, b int) {
			row := make([]uint32, w)
			for y := y0 + a; y < y0+b; y++ {
				local := (y - top) * w
				for x := 0; x < w; x++ {
					i := local + x
					row[x] = 0
					if site[i] >= 0 && float64(dist2[i]) <= limit {
						row[x] = labels[site[i]]
					}
				}
				out.Rows[y] = segment.EncodeRow(row)
				nuc.Rows[y] = segment.EncodeRow(labels[local : local+w])
			}
		})
		if opt.Progress != nil {
			opt.Progress(float64(y1) / float64(h))
		}
	}
	return out, nuc, nil
}

// Image is a nucleus label image held in memory.
type Image struct {
	Width, Height int
	Labels        []uint32
}

func (m *Image) Size() (int, int) { return m.Width, m.Height }

func (m *Image) Rows(y0, y1 int, dst []uint32) error {
	copy(dst, m.Labels[y0*m.Width:y1*m.Width])
	return nil
}
