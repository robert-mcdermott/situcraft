package xenium

// The segmentation masks of cells.zarr.zip: masks/0 labels each pixel with its nucleus, masks/1
// with its cell (label k is cells.parquet's row k-1), at the morphology image's resolution;
// masks/homogeneous_transform maps µm to mask pixels. A mask is read a band of rows at a time, so
// that a whole slide's need not fit in memory; the chunk row last read is kept for the next band.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"situcraft/internal/zarr"
)

// Mask is one of cells.zarr.zip's label masks.
type Mask struct {
	f      ReaderAt
	a      *zarr.Array
	width  int
	height int
	// PixelSize is the mask's pixel in µm (from the homogeneous transform).
	PixelSize float64
	cached    int      // the chunk row held in rows, or -1
	rows      []uint32 // that chunk row, decoded: Chunks[0] rows × width
}

// Mask indexes: nuclei and cells.
const (
	NucleusMask = 0
	CellMask    = 1
)

// OpenMask opens masks/<index> of cells.zarr.zip.
func (b *Bundle) OpenMask(index int) (*Mask, error) {
	f, err := b.OpenFile("cells.zarr.zip")
	if err != nil {
		return nil, err
	}
	return openMask(f, index, b.Experiment.PixelSize)
}

// openMask opens masks/<index> of a cells.zarr.zip; pixelSize is the manifest's (0 if none).
func openMask(f ReaderAt, index int, pixelSize float64) (*Mask, error) {
	fail := func(err error) (*Mask, error) {
		f.Close()
		return nil, fmt.Errorf("cells.zarr.zip: %w", err)
	}
	store, err := zarr.OpenZip(f, f.Size())
	if err != nil {
		return fail(err)
	}
	a, err := zarr.OpenArray(store, fmt.Sprintf("masks/%d", index))
	if err != nil {
		return fail(err)
	}
	if len(a.Shape) != 2 || len(a.Chunks) != 2 || a.Chunks[0] <= 0 || a.Chunks[1] <= 0 || a.DType.Size != 4 || a.DType.Kind != 'u' && a.DType.Kind != 'i' {
		return fail(fmt.Errorf("masks/%d is not a 2-D array of 32-bit labels (%v, %s)", index, a.Shape, a.DType))
	}
	// The manifest's pixel size; the transform's when the manifest gives none (the transform holds
	// its inverse as a float32, 4.705882 for 0.2125 µm).
	m := &Mask{f: f, a: a, width: a.Shape[1], height: a.Shape[0], cached: -1, PixelSize: pixelSize}
	if !(m.PixelSize > 0) {
		if t, err := zarr.OpenArray(store, "masks/homogeneous_transform"); err == nil {
			if raw, err := t.ReadAll(); err == nil && len(raw) >= 4 && t.DType.Size == 4 && t.DType.Kind == 'f' {
				if s := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw))); s > 0 {
					m.PixelSize = 1 / s
				}
			}
		}
	}
	if !(m.PixelSize > 0) {
		return fail(errors.New("neither the masks' transform nor the manifest gives the pixel size"))
	}
	return m, nil
}

// Size is the mask's width and height in pixels.
func (m *Mask) Size() (int, int) { return m.width, m.height }

// Close releases the file.
func (m *Mask) Close() error { return m.f.Close() }

// Rows fills dst with rows y0..y1-1 of the mask, row-major.
func (m *Mask) Rows(y0, y1 int, dst []uint32) error {
	if y0 < 0 || y1 > m.height || y0 > y1 || len(dst) < (y1-y0)*m.width {
		return fmt.Errorf("rows %d to %d of a mask of %d", y0, y1, m.height)
	}
	ch := m.a.Chunks[0]
	for y := y0; y < y1; {
		r := y / ch
		if err := m.load(r); err != nil {
			return err
		}
		end := min(y1, (r+1)*ch)
		copy(dst[(y-y0)*m.width:(end-y0)*m.width], m.rows[(y-r*ch)*m.width:(end-r*ch)*m.width])
		y = end
	}
	return nil
}

// load decodes chunk row r into m.rows.
func (m *Mask) load(r int) error {
	if m.cached == r {
		return nil
	}
	ch, cw := m.a.Chunks[0], m.a.Chunks[1]
	if len(m.rows) != ch*m.width {
		m.rows = make([]uint32, ch*m.width)
	}
	for c := 0; c*cw < m.width; c++ {
		chunk, err := m.a.ReadChunk([]int{r, c})
		if err != nil {
			return err
		}
		if len(chunk) < 4*ch*cw {
			return fmt.Errorf("a chunk of masks is %d bytes, not %d", len(chunk), 4*ch*cw)
		}
		for y := 0; y < ch && r*ch+y < m.height; y++ {
			for x := 0; x < cw && c*cw+x < m.width; x++ {
				k := y*cw + x
				if m.a.Order == 'F' {
					k = x*ch + y
				}
				m.rows[y*m.width+c*cw+x] = binary.LittleEndian.Uint32(chunk[4*k:])
			}
		}
	}
	m.cached = r
	return nil
}
