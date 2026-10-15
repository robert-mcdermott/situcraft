package refine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"situcraft/internal/simulate"
)

// measure refines a bundle as the app's job does (Refine), answering the preflight's estimate,
// the peak heap in use (sampled), the time and the files' size.
func measure(t *testing.T, folder string, spec Spec) (Estimate, int64, float64, int64) {
	t.Helper()
	b := openBundle(t, folder)
	in, idx, err := BundleInputs(b, spec.MinQV)
	if err != nil {
		t.Fatal(err)
	}
	pre := Check(in, idx, spec, runtime.GOMAXPROCS(0), 0, -1)
	if len(pre.Problems) > 0 {
		t.Fatalf("%+v", pre.Problems)
	}
	t.Logf("rows %d, row group %d, mask block %d, modelled %d of %d, voxels %d, cell-genes %d",
		idx.Total, idx.RowGroup, idx.MaskBlock, pre.Estimate.Transcripts, len(idx.Rows), pre.Estimate.Voxels, (pre.Estimate.Cells+pre.Estimate.Held)*in.Genes)
	in, idx = nil, nil
	dir := t.TempDir()
	var phase atomic.Int32
	var phases [3]atomic.Int64
	stop := watchHeap(func(heap int64) {
		p := &phases[phase.Load()]
		if heap > p.Load() {
			p.Store(heap)
		}
	})
	start := time.Now()
	job := Job{Workers: runtime.GOMAXPROCS(0), Progress: func(f float64, _ string) {
		switch {
		case f >= 0.9:
			phase.Store(2)
		case f >= 0.05:
			phase.Store(1)
		}
	}}
	if _, _, err := Refine(context.Background(), b, spec, dir, job); err != nil {
		t.Fatal(err)
	}
	seconds := time.Since(start).Seconds()
	peak := stop()
	t.Logf("peaks: reading %d MB, sampling %d MB, writing %d MB", phases[0].Load()>>20, phases[1].Load()>>20, phases[2].Load()>>20)
	var disk int64
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		info, _ := e.Info()
		disk += info.Size()
	}
	return pre.Estimate, peak, seconds, disk
}

// The preflight's estimates against measured runs: memory and disk within 30% (time is logged:
// it depends on the computer). Not under the race detector, whose shadow memory the estimates do
// not count (the weekly real-data workflow runs it).
func TestEstimatesMatchMeasuredRuns(t *testing.T) {
	if testing.Short() || raceDetector {
		t.Skip("short mode, or the race detector")
	}
	opt := DefaultOptions()
	opt.Stages, opt.Samples = []Stage{{1, 1, 100}}, 80
	low := opt
	low.Stages = []Stage{{2, 1, 100}}
	for _, c := range []struct {
		name          string
		width, height float64
		genes         int
		area          Area
		gc            int
		opt           Options
	}{
		{"small, 120 genes", 160, 120, 120, Area{}, 0, opt},
		{"300 µm, 1,000 genes", 300, 300, 1000, Area{}, 0, opt},
		{"300 µm, a region", 300, 300, 120, Area{Mode: AreaRegions, Regions: []Region{{60, 60, 200, 220}}}, 0, opt},
		{"500 µm, a small region", 500, 500, 120, Area{Mode: AreaRegions, Regions: []Region{{100, 100, 180, 180}}}, 0, opt},
		{"500 µm, 120 genes", 500, 500, 120, Area{}, 0, opt},
		{"500 µm, 1,000 genes, Low memory", 500, 500, 1000, Area{}, lowMemoryGC, low},
	} {
		folder := filepath.Join(t.TempDir(), "sim")
		p := simulate.Presets["standard"]
		p.WidthUm, p.HeightUm, p.Genes = c.width, c.height, c.genes
		if _, err := simulate.Run(context.Background(), p, folder, "test", nil); err != nil {
			t.Fatal(err)
		}
		est, peak, seconds, disk := measure(t, folder, Spec{Options: c.opt, Area: c.area, MinQV: 20, GCPercent: c.gc})
		t.Logf("%s: memory %d MB (estimated %d), disk %d KB (%d), %.1f s (%.1f); modelled %d, voxels %d, cells %d + %d held",
			c.name, peak>>20, est.PeakBytes>>20, disk>>10, est.DiskBytes>>10, seconds, est.Seconds, est.Transcripts, est.Voxels, est.Cells, est.Held)
		if r := float64(est.PeakBytes) / float64(peak); r < 1/1.3 || r > 1.3 {
			t.Errorf("%s: memory estimated %d MB, measured %d MB", c.name, est.PeakBytes>>20, peak>>20)
		}
		if r := float64(est.DiskBytes) / float64(disk); r < 1/1.3 || r > 1.3 {
			t.Errorf("%s: disk estimated %d KB, measured %d KB", c.name, est.DiskBytes>>10, disk>>10)
		}
	}
}
