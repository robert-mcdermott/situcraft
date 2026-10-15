package refine

// Patches on a simulated tissue (REFINE_PATCHES=small or standard; research.md §3.2, patches): the
// tissue refined whole and in four patches, each scored against the truth, the transcripts near
// the patches' seams apart. The patches must recover the cells as the whole does.

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"situcraft/internal/segment"
	"situcraft/internal/simulate"
)

func TestPatchesOnSimulatedTissue(t *testing.T) {
	tissue := os.Getenv("REFINE_PATCHES")
	if tissue == "" {
		t.Skip("set REFINE_PATCHES=small or standard to measure patches against the whole")
	}
	folder := filepath.Join(t.TempDir(), tissue)
	if _, err := simulate.Run(context.Background(), simulate.Presets[tissue], folder, "test", nil); err != nil {
		t.Fatal(err)
	}
	b := openBundle(t, folder)
	tr := loadTruth(t, folder)
	in, idx, err := BundleInputs(b, 20)
	if err != nil {
		t.Fatal(err)
	}
	opt := DefaultOptions()
	type outcome struct {
		all, seams score
		seconds    float64
		patches    []Unit
	}
	run := func(area Area) outcome {
		start := time.Now()
		results, err := RunArea(context.Background(), in, area, opt, nil)
		if err != nil {
			t.Fatal(err)
		}
		seconds := time.Since(start).Seconds()
		dir := t.TempDir()
		if _, _, err := WriteBundleOutputs(b, in, idx, results, opt, Recorded{Area: area}, dir); err != nil {
			t.Fatal(err)
		}
		mask, _ := segment.ReadMask(dir)
		labels, _ := segment.ReadAssignments(dir)
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
		var patches []Unit
		for _, res := range results {
			patches = append(patches, Unit{Free: res.Region, Own: res.Own})
		}
		// The transcripts within 20 µm of a seam between two patches' squares.
		counted, nearSeams := map[int]bool{}, map[int]bool{}
		for i, r := range idx.Rows {
			counted[r] = true
			x, y := float64(in.Transcripts.X[i]), float64(in.Transcripts.Y[i])
			for _, p := range patches {
				o := p.Own
				if p.Own == p.Free || !o.Contains(x, y) {
					continue
				}
				inner := in.whole()
				d := math.Inf(1)
				if o.X0 > inner.X0 {
					d = math.Min(d, x-o.X0)
				}
				if o.X1 < inner.X1 {
					d = math.Min(d, o.X1-x)
				}
				if o.Y0 > inner.Y0 {
					d = math.Min(d, y-o.Y0)
				}
				if o.Y1 < inner.Y1 {
					d = math.Min(d, o.Y1-y)
				}
				if d < 20 {
					nearSeams[r] = true
				}
			}
		}
		out := outcome{all: tr.score(counted, cellOfRow, cellAt), seconds: seconds, patches: patches}
		if len(nearSeams) > 0 {
			out.seams = tr.score(nearSeams, cellOfRow, cellAt)
		}
		return out
	}
	whole := run(Area{Mode: AreaWhole})
	patched := run(Area{Mode: AreaWhole, PatchTranscripts: len(in.Transcripts.Gene)/4 + 1})
	t.Logf("whole: %.1f%% of transcripts in their true cell, median IoU %.3f, %.0f s", 100*whole.all.accuracy, whole.all.medianIoU, whole.seconds)
	t.Logf("in %d patches: %.1f%% in their true cell, median IoU %.3f, %.0f s; within 20 µm of a seam %.1f%%", len(patched.patches), 100*patched.all.accuracy, patched.all.medianIoU, patched.seconds, 100*patched.seams.accuracy)
	for _, p := range patched.patches {
		t.Logf("  patch: square (%.0f, %.0f)–(%.0f, %.0f) µm, refining (%.0f, %.0f)–(%.0f, %.0f)", p.Own.X0, p.Own.Y0, p.Own.X1, p.Own.Y1, p.Free.X0, p.Free.Y0, p.Free.X1, p.Free.Y1)
	}
	if len(patched.patches) != 4 || patched.all.accuracy < whole.all.accuracy-0.005 || patched.all.medianIoU < whole.all.medianIoU-0.01 || patched.seams.accuracy < whole.all.accuracy-0.02 {
		t.Errorf("the patches recover the cells less well than the whole")
	}
}
