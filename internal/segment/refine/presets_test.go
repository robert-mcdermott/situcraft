package refine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"situcraft/internal/segment"
	"situcraft/internal/simulate"
)

// Every preset's settings are usable; Balanced is the defaults; each says what it is for, changes
// and risks; every setting's help names a setting the options or the spec have.
func TestPresetsAreWellFormed(t *testing.T) {
	ids := map[string]bool{}
	for _, p := range Presets() {
		if ids[p.ID] {
			t.Errorf("preset %s twice", p.ID)
		}
		ids[p.ID] = true
		if err := p.Options.Check(); err != nil {
			t.Errorf("%s: %v", p.ID, err)
		}
		if p.Name == "" || p.About == "" || p.Changes == "" || p.Warning == "" {
			t.Errorf("%s says too little: %+v", p.ID, p)
		}
	}
	if b, _ := PresetByID(PresetBalanced); !sameOptions(b.Options, DefaultOptions()) {
		t.Error("Balanced is not the defaults")
	}
	if p, _ := PresetByID(PresetPilot); p.Area != AreaRegions || !p.SeedCheck {
		t.Error("the ROI pilot needs a region and checks a second seed")
	}
	raw, _ := json.Marshal(DefaultOptions())
	var opts map[string]any
	json.Unmarshal(raw, &opts)
	for _, s := range SettingsHelp() {
		if s.Help == "" {
			t.Errorf("%s: no help", s.Key)
		}
		switch s.Key {
		case "min_qv", "seed_check", "gc_percent", "seed", "samples", "block_um":
			continue
		}
		parts := strings.Split(s.Key, ".")
		var at any = opts
		for _, part := range parts {
			switch v := at.(type) {
			case map[string]any:
				at = v[part]
			case []any:
				at = v[0].(map[string]any)[part]
			}
		}
		if at == nil {
			t.Errorf("help for %s, which the options do not have", s.Key)
		}
	}
}

func sameOptions(a, b Options) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// watchHeap samples the heap in use every 10 ms until stopped, answering its peak; each sample
// also goes to the functions given.
func watchHeap(also ...func(int64)) (stop func() int64) {
	runtime.GC()
	var peak atomic.Uint64
	done, sampled := make(chan bool), make(chan bool)
	go func() {
		defer close(sampled)
		var ms runtime.MemStats
		for {
			runtime.ReadMemStats(&ms)
			if ms.HeapInuse > peak.Load() {
				peak.Store(ms.HeapInuse)
			}
			for _, f := range also {
				f(int64(ms.HeapInuse))
			}
			select {
			case <-done:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	return func() int64 {
		close(done)
		<-sampled
		return int64(peak.Load())
	}
}

// The presets on a simulated tissue (REFINE_PRESETS=small or standard; research.md §3.2, the
// segmentation studio): each scored against the truth, with its time and peak heap. Each puts more
// transcripts in their true cells than the instrument; Conservative draws more compact outlines
// than Balanced, Flexible less compact ones; Low memory needs less memory; the ROI pilot's two
// seeds mostly agree.
func TestPresetsOnSimulatedTissue(t *testing.T) {
	tissue := os.Getenv("REFINE_PRESETS")
	if tissue == "" {
		t.Skip("set REFINE_PRESETS=small or standard to measure the presets")
	}
	folder := filepath.Join(t.TempDir(), tissue)
	p := simulate.Presets[tissue]
	if _, err := simulate.Run(context.Background(), p, folder, "test", nil); err != nil {
		t.Fatal(err)
	}
	b := openBundle(t, folder)
	tr := loadTruth(t, folder)
	in, idx, err := BundleInputs(b, 20)
	if err != nil {
		t.Fatal(err)
	}
	counted := map[int]bool{}
	for _, r := range idx.Rows {
		counted[r] = true
	}
	// The pilot's region: the middle 40% of the tissue each way.
	w, h := in.WidthUm, in.HeightUm
	pilot := Region{in.X0 + 0.3*w, in.Y0 + 0.3*h, in.X0 + 0.7*w, in.Y0 + 0.7*h}
	type outcome struct {
		whole, region     score
		unassigned, area  float64
		compact           float64
		seconds           float64
		peak, estimated   int64
		agreement, geweke float64
	}
	// compactness is the cells' median ratio of their outline (pixel edges) to the least a shape
	// of their area can have (minPerimeter).
	compactness := func(mask *segment.Raster) float64 {
		edges, area := map[uint32]int{}, map[uint32]int{}
		above, row := make([]uint32, mask.Width), make([]uint32, mask.Width)
		for y := 0; y < mask.Height; y++ {
			mask.Row(y, row)
			for x, l := range row {
				if l == 0 {
					continue
				}
				area[l]++
				if x == 0 || row[x-1] != l {
					edges[l]++
				}
				if x == mask.Width-1 || row[x+1] != l {
					edges[l]++
				}
				if y == 0 || above[x] != l {
					edges[l]++
				}
			}
			for x, l := range above {
				if y > 0 && l != 0 && row[x] != l {
					edges[l]++
				}
			}
			above, row = row, above
		}
		for _, l := range above {
			if l != 0 {
				edges[l]++
			}
		}
		var ratios []float64
		for l, n := range area {
			ratios = append(ratios, float64(edges[l])/float64(minPerimeter(n)))
		}
		sort.Float64s(ratios)
		return ratios[len(ratios)/2]
	}
	scoreDir := func(dir string, region Region) (score, score, float64, float64) {
		mask, err := segment.ReadMask(dir)
		if err != nil {
			t.Fatal(err)
		}
		labels, err := segment.ReadAssignments(dir)
		if err != nil {
			t.Fatal(err)
		}
		cellAt := func(x, y int) string {
			if l := mask.At(x, y); l > 0 {
				return idx.CellIDs[l-1]
			}
			return ""
		}
		cellOfRow := func(row int) string {
			if l := labels[row]; l > 0 {
				return idx.CellIDs[l-1]
			}
			return ""
		}
		var unassigned, all float64
		for r := range counted {
			if string(tr.trueCell[r]) == "UNASSIGNED" {
				continue
			}
			all++
			if labels[r] == 0 {
				unassigned++
			}
		}
		var area float64
		row := make([]uint32, mask.Width)
		for y := 0; y < mask.Height; y++ {
			mask.Row(y, row)
			for _, l := range row {
				if l > 0 {
					area++
				}
			}
		}
		whole := tr.score(counted, cellOfRow, cellAt)
		// In the pilot's region: its transcripts, and the true cells centred in it.
		inRegion := map[int]bool{}
		for i, r := range idx.Rows {
			if pilot.Contains(float64(in.Transcripts.X[i]), float64(in.Transcripts.Y[i])) {
				inRegion[r] = true
			}
		}
		sub := *tr
		sub.withNuc = map[string]bool{}
		sx, sy, n := map[string]float64{}, map[string]float64{}, map[string]float64{}
		ps := tr.trueMask.PixelSize
		for y := 0; y < tr.trueMask.Height; y++ {
			tr.trueMask.Row(y, row)
			for x, l := range row {
				if l > 0 {
					id := tr.trueIDs[l-1]
					sx[id] += (float64(x) + 0.5) * ps
					sy[id] += (float64(y) + 0.5) * ps
					n[id]++
				}
			}
		}
		for id := range tr.withNuc {
			if n[id] > 0 && region.Contains(in.X0+sx[id]/n[id], in.Y0+sy[id]/n[id]) {
				sub.withNuc[id] = true
			}
		}
		return whole, sub.score(inRegion, cellOfRow, cellAt), unassigned / all, area * ps * ps
	}
	run := func(p Preset, area Area) outcome {
		spec := Spec{Preset: p.ID, Options: p.Options, Area: area, MinQV: 20, SeedCheck: p.SeedCheck, GCPercent: p.GCPercent}
		regions, err := in.Regions(area)
		if err != nil {
			t.Fatal(err)
		}
		est := EstimateCost(in, idx, regions, p.Options, runtime.GOMAXPROCS(0), p.GCPercent)
		dir := filepath.Join(t.TempDir(), p.ID)
		os.MkdirAll(dir, 0o755)
		stop := watchHeap()
		start := time.Now()
		_, diag, err := Refine(context.Background(), b, spec, dir, Job{})
		if err != nil {
			t.Fatal(err)
		}
		o := outcome{seconds: time.Since(start).Seconds(), peak: stop(), estimated: est.PeakBytes, geweke: diag.Summary.GewekeZ}
		if diag.Summary.SeedAgreement != nil {
			o.agreement = *diag.Summary.SeedAgreement
		}
		o.whole, o.region, o.unassigned, o.area = scoreDir(dir, pilot)
		mask, err := segment.ReadMask(dir)
		if err != nil {
			t.Fatal(err)
		}
		o.compact = compactness(mask)
		return o
	}
	// The instrument's segmentation, scored the same way.
	var instrument score
	{
		dir := filepath.Join(t.TempDir(), "instrument")
		os.MkdirAll(dir, 0o755)
		if _, _, err := WriteBundleOutputs(b, in, idx, nil, DefaultOptions(), Recorded{}, dir); err != nil {
			t.Fatal(err)
		}
		whole, region, unassigned, area := scoreDir(dir, pilot)
		instrument = whole
		mask, _ := segment.ReadMask(dir)
		t.Logf("instrument: %.1f%% in their true cell, median IoU %.3f; in the pilot's region %.1f%%, %.3f; %.2f%% unassigned; cells cover %.0f µm², outlines %.2f times the least",
			100*whole.accuracy, whole.medianIoU, 100*region.accuracy, region.medianIoU, 100*unassigned, area, compactness(mask))
	}
	got := map[string]outcome{}
	for _, p := range Presets() {
		if p.ID == PresetExpert {
			continue
		}
		area := Area{Mode: AreaWhole}
		if p.Area == AreaRegions {
			area = Area{Mode: AreaRegions, Regions: []Region{pilot}}
		}
		o := run(p, area)
		got[p.ID] = o
		t.Logf("%s: %.1f%% in their true cell, median IoU %.3f; in the pilot's region %.1f%%, %.3f; %.2f%% unassigned; cells cover %.0f µm², outlines %.2f times the least; %.1f s, heap %d MB (estimated %d); Geweke z %.1f; seeds agree %.1f%%",
			p.Name, 100*o.whole.accuracy, o.whole.medianIoU, 100*o.region.accuracy, o.region.medianIoU, 100*o.unassigned, o.area, o.compact, o.seconds, o.peak>>20, o.estimated>>20, o.geweke, 100*o.agreement)
	}
	bal, con, flex, low, pil := got[PresetBalanced], got[PresetConservative], got[PresetFlexible], got[PresetLowMemory], got[PresetPilot]
	// Conservative draws more compact outlines than Balanced, Flexible less compact ones.
	if !(con.compact < bal.compact && bal.compact < flex.compact) {
		t.Errorf("outlines: Conservative %.3f, Balanced %.3f, Flexible %.3f times the least", con.compact, bal.compact, flex.compact)
	}
	// Low memory: its estimate lower (the measured peak varies with the collector's timing), and
	// still above the instrument.
	if !(low.estimated < bal.estimated) {
		t.Errorf("Low memory: %d MB (Balanced %d MB)", low.estimated>>20, bal.estimated>>20)
	}
	for name, o := range got {
		if !(o.whole.accuracy > instrument.accuracy) {
			t.Errorf("%s: %.3f of transcripts in their true cell, the instrument's %.3f", name, o.whole.accuracy, instrument.accuracy)
		}
	}
	if !(pil.agreement > 0.8) {
		t.Errorf("the pilot's seeds agree on %.3f", pil.agreement)
	}
}
