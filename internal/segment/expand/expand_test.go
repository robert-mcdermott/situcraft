package expand

import (
	"context"
	"math/rand/v2"
	"reflect"
	"testing"

	"situcraft/internal/xenium"
)

// The distance transform against brute force: every pixel's squared distance is the least to any
// labelled pixel, and its site is one at that distance.
func TestTheDistanceTransformIsExact(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 200; trial++ {
		w, h := 1+rng.IntN(30), 1+rng.IntN(30)
		labels := make([]uint32, w*h)
		for k := rng.IntN(6); k >= 0; k-- {
			labels[rng.IntN(w*h)] = uint32(1 + rng.IntN(5))
		}
		site, dist2 := nearest(labels, w, h, 1+rng.IntN(4))
		for i := range labels {
			best := int64(-1)
			for j := range labels {
				if labels[j] == 0 {
					continue
				}
				dx, dy := int64(i%w-j%w), int64(i/w-j/w)
				if d := dx*dx + dy*dy; best < 0 || d < best {
					best = d
				}
			}
			if best < 0 {
				if site[i] != -1 {
					t.Fatalf("trial %d pixel %d: a site with no labelled pixel", trial, i)
				}
				continue
			}
			s := int(site[i])
			dx, dy := int64(i%w-s%w), int64(i/w-s/w)
			if dist2[i] != best || labels[s] == 0 || dx*dx+dy*dy != best {
				t.Fatalf("trial %d pixel %d: distance %d (site %d), brute force %d", trial, i, dist2[i], s, best)
			}
		}
	}
}

func TestExpansionGeometry(t *testing.T) {
	ctx := context.Background()
	// One nucleus pixel grown by 2 pixels: the 13 pixels within distance 2.
	img := &Image{Width: 9, Height: 9, Labels: make([]uint32, 81)}
	img.Labels[4*9+4] = 1
	r, _, err := Expand(ctx, img, Options{Distance: 2})
	if err != nil {
		t.Fatal(err)
	}
	if s := r.Stats(1); s[0].Pixels != 13 {
		t.Errorf("%d pixels within distance 2", s[0].Pixels)
	}
	// Two nuclei three pixels apart in a row: each takes its side, the middle pixel the left one.
	img = &Image{Width: 9, Height: 1, Labels: make([]uint32, 9)}
	img.Labels[3], img.Labels[6] = 1, 2
	r, _, _ = Expand(ctx, img, Options{Distance: 10})
	row := make([]uint32, 9)
	r.Row(0, row)
	if want := []uint32{1, 1, 1, 1, 1, 2, 2, 2, 2}; !reflect.DeepEqual(row, want) {
		t.Errorf("row %v, want %v", row, want)
	}
	// Distance 0 keeps the nuclei as they are.
	r, _, _ = Expand(ctx, img, Options{Distance: 0})
	r.Row(0, row)
	if want := []uint32{0, 0, 0, 1, 0, 0, 2, 0, 0}; !reflect.DeepEqual(row, want) {
		t.Errorf("distance 0: %v", row)
	}
	if _, _, err := Expand(ctx, img, Options{Distance: -1}); err == nil {
		t.Error("a negative distance was accepted")
	}
}

// Bands and workers change nothing: the raster is the same for any band height and any number
// of workers.
func TestBandsAndWorkersGiveTheSameCells(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	w, h := 97, 83
	img := &Image{Width: w, Height: h, Labels: make([]uint32, w*h)}
	for k := 1; k <= 40; k++ {
		cx, cy := rng.IntN(w), rng.IntN(h)
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				if x, y := cx+dx, cy+dy; x >= 0 && y >= 0 && x < w && y < h && rng.IntN(4) > 0 {
					img.Labels[y*w+x] = uint32(k)
				}
			}
		}
	}
	whole, nuclei, err := Expand(context.Background(), img, Options{Distance: 6.5, Band: h, Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, band := range []int{1, 7, 20, 64} {
		for _, workers := range []int{1, 3, 8} {
			got, gotNuclei, _ := Expand(context.Background(), img, Options{Distance: 6.5, Band: band, Workers: workers})
			if !reflect.DeepEqual(got.Rows, whole.Rows) || !reflect.DeepEqual(gotNuclei.Rows, nuclei.Rows) {
				t.Fatalf("band %d, %d workers: a different raster", band, workers)
			}
		}
	}
}

// A polygon covers the pixels whose own points ((i, j) × the pixel size) are inside it or on its
// edge; each polygon is a nucleus, labelled in order, and belongs to the cell it names.
func TestNucleusPolygonsAreDrawnByPixelPoints(t *testing.T) {
	p := &xenium.Polygons{
		CellIDs: []string{"a", "b", "unknown"},
		Offsets: []int32{0, 4, 7, 10},
		// a: the square 1–3 µm; b: a triangle; unknown: a nucleus of no known cell.
		X: []float32{1, 3, 3, 1, 4, 7, 4, 0, 1, 0},
		Y: []float32{1, 1, 3, 3, 0, 0, 3, 0, 0, 1},
	}
	img, owners := drawNuclei(p, map[string]int32{"a": 4, "b": 0}, 8, 5, 1)
	want := []string{
		"33..2222",
		"3111222.",
		".11122..",
		".1112...",
		"........",
	}
	for y, row := range want {
		for x, c := range row {
			label := uint32(0)
			if c != '.' {
				label = uint32(c - '0')
			}
			if got := img.Labels[y*8+x]; got != label {
				t.Errorf("pixel (%d, %d) is %d, want %d", x, y, got, label)
			}
		}
	}
	if !reflect.DeepEqual(owners, []uint32{0, 5, 1, 0}) {
		t.Errorf("owners %v", owners)
	}
}

// Counts are rows of cells, genes in name order.
func TestCountsAreSparseRowsInGeneOrder(t *testing.T) {
	genes := []string{"VIM", "ACTA2", "EPCAM"} // as first met
	pairs := map[uint64]uint32{0<<32 | 0: 2, 0<<32 | 1: 1, 2<<32 | 2: 5, 2<<32 | 0: 1}
	c := countsOf(pairs, genes, 3)
	if !reflect.DeepEqual(c.Genes, []string{"ACTA2", "EPCAM", "VIM"}) ||
		!reflect.DeepEqual(c.Offsets, []uint32{0, 2, 2, 4}) ||
		!reflect.DeepEqual(c.Gene, []uint32{0, 2, 1, 2}) ||
		!reflect.DeepEqual(c.Count, []uint32{1, 2, 5, 1}) {
		t.Errorf("counts %+v", c)
	}
}
