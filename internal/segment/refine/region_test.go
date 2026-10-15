package refine

import (
	"context"
	"path/filepath"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
	"situcraft/internal/simulate"
)

// Sections: cells far apart are separate sections, near ones one.
func TestSectionsAreTheTissuesPieces(t *testing.T) {
	box := func(x, y float64) Region { return Region{x, y, x + 10, y + 10} }
	in := &Inputs{WidthUm: 2000, HeightUm: 1000, Cells: 5, Box: []Region{box(100, 100), box(150, 120), box(1500, 600), box(1550, 640), {}}}
	got, err := in.Regions(Area{Mode: AreaSections})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d sections: %+v", len(got), got)
	}
	// Each section is its cells' box grown by the margin.
	if got[0] != (Region{80, 80, 180, 150}) || got[1] != (Region{1480, 580, 1580, 670}) {
		t.Errorf("sections %+v", got)
	}
	// A gap wide enough joins them.
	if one, _ := in.Regions(Area{Mode: AreaSections, GapUm: 3000}); len(one) != 1 {
		t.Errorf("a wide gap: %d sections", len(one))
	}
	// Chosen regions are clipped to the tissue, and near ones merged.
	rs, err := in.Regions(Area{Mode: AreaRegions, Regions: []Region{{-50, -50, 100, 100}, {120, 0, 200, 50}, {900, 500, 1000, 600}}})
	if err != nil || len(rs) != 2 || rs[0] != (Region{0, 0, 200, 100}) {
		t.Errorf("chosen regions %+v (%v)", rs, err)
	}
	if _, err := in.Regions(Area{Mode: AreaRegions, Regions: []Region{{3000, 0, 3100, 10}}}); Classify(err) == nil || Classify(err).Class != ClassRegion {
		t.Errorf("a region outside the tissue: %v", err)
	}
}

// A region's run moves only its own cells; the output keeps the instrument's segmentation beyond
// the region and for the cells held about it.
func TestARegionKeepsTheRest(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "small")
	p := simulate.Presets["small"]
	p.WidthUm, p.HeightUm = 120, 90
	if _, err := simulate.Run(context.Background(), p, folder, "test", nil); err != nil {
		t.Fatal(err)
	}
	b := openBundle(t, folder)
	in, idx, err := BundleInputs(b, 20)
	if err != nil {
		t.Fatal(err)
	}
	opt := DefaultOptions()
	opt.Stages, opt.Samples = []Stage{{1, 1, 60}}, 40
	area := Area{Mode: AreaRegions, Regions: []Region{{30, 25, 80, 65}}}
	results, err := RunArea(context.Background(), in, area, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := results[0]
	// Held cells: their voxels are as they started.
	start, _ := startLabels(in, res.State.Lat, func(c int) bool { return res.Region.Contains(in.SeedX[c], in.SeedY[c]) })
	held, free := 0, 0
	for k, f := range res.Frozen {
		if f {
			held++
		} else {
			free++
		}
		_ = k
	}
	if held == 0 || free == 0 {
		t.Fatalf("%d cells held, %d free", held, free)
	}
	keep := map[uint32]uint32{}
	for k, c := range res.Kept {
		keep[uint32(c+1)] = uint32(k + 1)
	}
	for v, c := range res.State.Labels {
		was := keep[start[v]]
		if was > 0 && res.Frozen[was-1] && c != was || c > 0 && res.Frozen[c-1] && was != c {
			t.Fatalf("voxel %d: a held cell's voxel changed (%d, was %d)", v, c, was)
		}
	}
	dir := t.TempDir()
	if _, _, err := WriteBundleOutputs(b, in, idx, results, opt, Recorded{Area: area}, dir); err != nil {
		t.Fatal(err)
	}
	m := artifacts.Manifest{}
	for _, f := range segment.Check(dir, &m) {
		if f.Severity == "fatal" {
			t.Errorf("%s: %s", f.Code, f.Message)
		}
	}
	mask, _ := segment.ReadMask(dir)
	ext := res.State.Lat.extent()
	ps := mask.PixelSize
	a, inst := make([]uint32, mask.Width), make([]uint32, mask.Width)
	isHeld := map[uint32]bool{}
	for k, c := range res.Kept {
		if res.Frozen[k] {
			isHeld[uint32(c+1)] = true
		}
	}
	changedInside, changedOutside, heldChanged := 0, 0, 0
	for y := 0; y < mask.Height; y++ {
		mask.Row(y, a)
		in.Start.Row(y, inst)
		for x := range a {
			if a[x] == inst[x] {
				continue
			}
			if ext.Contains((float64(x)+0.5)*ps, (float64(y)+0.5)*ps) {
				changedInside++
				if isHeld[inst[x]] && a[x] != 0 && !isHeld[a[x]] {
					continue // a refined cell grew into a held cell's pixel the lattice gave it
				}
				if isHeld[inst[x]] {
					heldChanged++
				}
			} else {
				changedOutside++
			}
		}
	}
	if changedOutside != 0 || changedInside == 0 || heldChanged != 0 {
		t.Errorf("pixels changed: %d inside the region's lattice, %d outside it, %d of held cells to another", changedInside, changedOutside, heldChanged)
	}
}
