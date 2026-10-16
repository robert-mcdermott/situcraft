package imports

import (
	"context"
	"math"
	"slices"
	"testing"

	"situcraft/internal/random"
	"situcraft/internal/segment"
)

// Drawn a band of rows at a time, the cells' polygons give the raster drawn whole: cells across the
// bands' edges, overlapping (the later's), with holes and in pieces, and the same overlap counted.
func TestDrawCellsInBands(t *testing.T) {
	w, h, size := 300, 2*drawBand+150, 0.5
	s := random.New(4, 0)
	var cells [][]polygon
	square := func(x, y, r float64) []point {
		return []point{{x - r, y - r}, {x + r, y - r}, {x + r, y + r}, {x - r, y + r}}
	}
	for k := 0; k < 400; k++ {
		x, y, r := s.Float64()*float64(w)*size, s.Float64()*float64(h)*size, 2+s.Float64()*20
		cell := []polygon{{square(x, y, r)}}
		switch k % 5 {
		case 1: // a hole
			cell[0] = append(cell[0], square(x, y, r/3))
		case 2: // a second piece, far below
			cell = append(cell, polygon{square(x, math.Mod(y+float64(drawBand)*size, float64(h)*size), r/2)})
		case 3: // partly outside the grid
			cell[0][0] = []point{{x, -10}, {x + r, -10}, {x + r, y}, {x, y}}
		}
		cells = append(cells, cell)
	}
	cells = append(cells, nil, []polygon{{nil}})
	whole := make([]uint32, w*h)
	overlap := 0
	for k, polys := range cells {
		overlap += draw(whole, w, 0, h, size, polys, uint32(k+1))
	}
	r := &segment.Raster{Width: w, Height: h, PixelSize: size, Rows: make([][]segment.Run, h)}
	got, err := drawCells(context.Background(), r, cells)
	if err != nil {
		t.Fatal(err)
	}
	if got != overlap || overlap == 0 {
		t.Errorf("overlap %d in bands, %d whole", got, overlap)
	}
	for y := 0; y < h; y++ {
		if !slices.Equal(r.Rows[y], segment.EncodeRow(whole[y*w:(y+1)*w])) {
			t.Fatalf("row %d differs", y)
		}
	}
}

// A label image read a row at a time resamples as the same image held whole; its
// labelled pixels outside the grid are counted (none when its pixels' centres all fall inside).
func TestResampleByRows(t *testing.T) {
	iw, ih := 230, 170
	s := random.New(9, 0)
	values := make([]uint32, iw*ih)
	for j := 0; j < ih; j++ {
		for i := 0; i < iw; i++ {
			values[j*iw+i] = uint32((i/9 + 31*(j/7)) % 60)
			if s.Float64() < 0.05 {
				values[j*iw+i] = 0
			}
		}
	}
	for _, tc := range []struct {
		name      string
		transform affine
	}{
		{"the same grid", affine{a: 0.5, e: 0.5}},
		{"scaled and shifted, partly outside", affine{a: 0.7, e: 0.6, c: -9, f: 4}},
		{"rotated", affine{a: 0.4, b: 0.2, c: 10, d: -0.2, e: 0.4, f: 30}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, h, size := iw, ih, 0.5
			held := &labelImage{width: iw, height: ih, values: values, transform: tc.transform}
			// Each row a copy, overwritten by the next read, as a mask reader's.
			last := make([]uint32, iw)
			byRows := &labelImage{width: iw, height: ih, transform: tc.transform, rowOf: func(j int) ([]uint32, error) {
				copy(last, values[j*iw:(j+1)*iw])
				return last, nil
			}}
			a := &segment.Raster{Width: w, Height: h, PixelSize: size, Rows: make([][]segment.Run, h)}
			b := &segment.Raster{Width: w, Height: h, PixelSize: size, Rows: make([][]segment.Run, h)}
			outA, err := resample(context.Background(), held, a)
			if err != nil {
				t.Fatal(err)
			}
			outB, err := resample(context.Background(), byRows, b)
			if err != nil {
				t.Fatal(err)
			}
			for y := 0; y < h; y++ {
				if !slices.Equal(a.Rows[y], b.Rows[y]) {
					t.Fatalf("row %d differs", y)
				}
			}
			want := 0
			for j := 0; j < ih; j++ {
				for i := 0; i < iw; i++ {
					x, y := tc.transform.apply(float64(i)+0.5, float64(j)+0.5)
					if values[j*iw+i] != 0 && (x < 0 || y < 0 || x >= float64(w)*size || y >= float64(h)*size) {
						want++
					}
				}
			}
			if outA != want || outB != want || tc.transform.c == 0 && want != 0 || tc.transform.c < 0 && want == 0 {
				t.Errorf("outside: %d held, %d by rows, %d counted here", outA, outB, want)
			}
		})
	}
}
