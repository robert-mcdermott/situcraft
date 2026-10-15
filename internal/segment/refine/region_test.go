package refine

import (
	"context"
	"math"
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

// Patches: a region of more than twice a patch's transcripts is cut into equal squares that tile
// it, each refining the cells within the overlap of its square; one at or under that is itself.
func TestPatchesTileTheRegion(t *testing.T) {
	r := Region{0, 0, 1000, 600}
	if u := patches(r, 4_000_000, patchTarget); len(u) != 1 || u[0].Own != r || u[0].Free != r {
		t.Errorf("at twice a patch: %+v", u)
	}
	u := patches(r, 6_000_000, 1_000_000)
	var area float64
	for _, p := range u {
		area += p.Own.Area()
		if want := p.Own.grown(patchOverlapUm).clipped(r); p.Free != want {
			t.Errorf("patch %+v: refines %+v", p.Own, p.Free)
		}
		for _, q := range u {
			if p != q && p.Own.overlaps(q.Own) {
				t.Errorf("squares %+v and %+v overlap", p.Own, q.Own)
			}
		}
	}
	if len(u) != 6 || math.Abs(area-r.Area()) > 1e-6 || u[len(u)-1].Own.X1 != r.X1 || u[len(u)-1].Own.Y1 != r.Y1 {
		t.Errorf("%d patches covering %g µm² of %g", len(u), area, r.Area())
	}
}

// A tissue refined in patches is stitched into one segmentation: each pixel and transcript from
// the patch whose square holds it, each cell counted once; it agrees with a run of the whole
// about as two seeds do.
func TestPatchesAreStitched(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	folder := filepath.Join(t.TempDir(), "small")
	p := simulate.Presets["small"]
	p.WidthUm, p.HeightUm = 200, 160
	if _, err := simulate.Run(context.Background(), p, folder, "test", nil); err != nil {
		t.Fatal(err)
	}
	b := openBundle(t, folder)
	in, idx, err := BundleInputs(b, 20)
	if err != nil {
		t.Fatal(err)
	}
	opt := DefaultOptions()
	opt.Stages, opt.Samples = []Stage{{1, 1, 100}}, 60
	area := Area{Mode: AreaWhole, PatchTranscripts: len(in.Transcripts.Gene)/4 + 1}
	results, err := RunArea(context.Background(), in, area, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("%d patches", len(results))
	}
	footprints := make([][]uint32, len(results))
	for r, res := range results {
		if !res.patched() || res.Model.Rate != nil || res.State.voxel != nil {
			t.Fatalf("patch %+v: patched %v, its sampler kept", res.Own, res.patched())
		}
		footprints[r] = Footprint(res)
	}
	dir := t.TempDir()
	_, diag, err := WriteBundleOutputs(b, in, idx, results, opt, Recorded{Area: area}, dir)
	if err != nil {
		t.Fatal(err)
	}
	m := artifacts.Manifest{}
	for _, f := range segment.Check(dir, &m) {
		if f.Severity == "fatal" {
			t.Errorf("%s: %s", f.Code, f.Message)
		}
	}
	// Every cell decided once: the whole tissue refined, none held.
	if s := diag.Summary; s.Refined+s.LeftOut != in.Cells || s.Held != 0 || s.Transcripts != len(in.Transcripts.Gene) || s.Regions != 1 || s.Patches != 4 {
		t.Errorf("refined %d, left out %d, held %d of %d cells; %d of %d transcripts; %d regions, %d patches", s.Refined, s.LeftOut, s.Held, in.Cells, s.Transcripts, len(in.Transcripts.Gene), s.Regions, s.Patches)
	}
	// Each pixel is its square's patch's: a cell that patch refines where its footprint has one.
	mask, _ := segment.ReadMask(dir)
	row := make([]uint32, mask.Width)
	wrong, checked := 0, 0
	for y := 0; y < mask.Height; y++ {
		mask.Row(y, row)
		yUm := in.Y0 + (float64(y)+0.5)*mask.PixelSize
		for x := range row {
			xUm := in.X0 + (float64(x)+0.5)*mask.PixelSize
			for r, res := range results {
				if !res.Own.Contains(xUm, yUm) {
					continue
				}
				l := res.State.Lat
				f := footprints[r][l.Voxel(xUm, yUm, l.Z0+l.LayerUm/2)%(l.W*l.H)]
				if f > 0 && !res.Frozen[f-1] {
					checked++
					if row[x] != uint32(res.Kept[f-1]+1) {
						wrong++
					}
				}
			}
		}
	}
	if checked == 0 || wrong != 0 {
		t.Errorf("%d of %d pixels not their patch's", wrong, checked)
	}
	// Against runs of the whole tissue: the patches agree with one about as well as two seeds do.
	whole, err := Run(context.Background(), in, Region{}, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	opt2 := opt
	opt2.Seed++
	other, err := Run(context.Background(), in, Region{}, opt2, nil)
	if err != nil {
		t.Fatal(err)
	}
	cellOf := func(res *Result, i int) uint32 {
		if k := res.Assigned[i]; k > 0 {
			return uint32(res.Kept[k-1] + 1)
		}
		return 0
	}
	assignments, _ := segment.ReadAssignments(dir)
	patched, seeds := 0, 0
	for i, r := range idx.Rows {
		if assignments[r] == cellOf(whole, i) {
			patched++
		}
		if cellOf(other, i) == cellOf(whole, i) {
			seeds++
		}
	}
	n := float64(len(idx.Rows))
	t.Logf("the patches and the whole agree on %.3f of transcripts, two seeds of the whole on %.3f", float64(patched)/n, float64(seeds)/n)
	if float64(patched)/n < float64(seeds)/n-0.03 {
		t.Errorf("the patches agree with the whole on %.3f of transcripts, two seeds on %.3f", float64(patched)/n, float64(seeds)/n)
	}
}
