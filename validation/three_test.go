package validation

// The standard simulated tissue segmented three ways — its truth, the instrument's nucleus
// expansion and SituCraft's refinement (Balanced) — each clustered with the defaults and labeled
// from its types' markers: made once in a run and shared by the tests that read them (labels across
// segmentations, analyses across segmentations), in a folder removed when the run ends.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"situcraft/internal/label"
	"situcraft/internal/segment/imports"
	"situcraft/internal/segment/refine"
	"situcraft/internal/xenium"
)

type threeSegs struct {
	dir     string            // the bundle
	folders map[string]string // truth, instrument, refinement
	segs    map[string]labeledSeg
	set     *label.MarkerSet
}

var (
	threeMu     sync.Mutex
	threeShared *threeSegs
	sharedDirs  []string
)

func TestMain(m *testing.M) {
	code := m.Run()
	for _, d := range sharedDirs {
		os.RemoveAll(d)
	}
	os.Exit(code)
}

// threeSegmentations makes them, or answers those made already in this run.
func threeSegmentations(t *testing.T) *threeSegs {
	t.Helper()
	threeMu.Lock()
	defer threeMu.Unlock()
	if threeShared != nil {
		return threeShared
	}
	ctx := context.Background()
	dir := simulatedStandard(t)
	work, err := os.MkdirTemp("", "situcraft-three-")
	if err != nil {
		t.Fatal(err)
	}
	sharedDirs = append(sharedDirs, work)
	b, err := xenium.Open(dir, xenium.Options{CacheDir: filepath.Join(work, "cache")})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ds, err := imports.BundleDataset(b)
	if err != nil {
		t.Fatal(err)
	}
	workers := runtime.NumCPU()
	folders := map[string]string{"truth": filepath.Join(work, "truth"), "instrument": filepath.Join(work, "instrument"), "refinement": filepath.Join(work, "refinement")}
	for _, f := range folders {
		os.MkdirAll(f, 0o755)
	}
	if _, _, _, err := imports.Run(ctx, ds, imports.Options{Format: imports.Xenium, Paths: []string{filepath.Join(dir, "truth")}}, folders["truth"], workers, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := imports.Instrument(ctx, b, folders["instrument"], workers); err != nil {
		t.Fatal(err)
	}
	if _, _, err := refine.Refine(ctx, b, refine.DefaultSpec(), folders["refinement"], refine.Job{Workers: workers}); err != nil {
		t.Fatal(err)
	}
	set := markersFor(t, "simulated-standard", b)
	segs := map[string]labeledSeg{}
	for name, f := range folders {
		segs[name] = labelFromMarkers(t, f, set)
	}
	threeShared = &threeSegs{dir: dir, folders: folders, segs: segs, set: set}
	return threeShared
}
