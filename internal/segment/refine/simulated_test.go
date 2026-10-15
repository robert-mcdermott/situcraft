package refine

// The segmentation recovers simulated cells: on a simulated tissue (internal/simulate), started
// from the simulated instrument's nucleus expansion, it assigns more transcripts to their true
// cells, and draws cells that overlap the true ones more, than the expansion does.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"situcraft/internal/parquet"
	"situcraft/internal/segment"
	"situcraft/internal/simulate"
	"situcraft/internal/xenium"
)

// score is a segmentation measured against the truth.
type score struct {
	accuracy  float64 // share of the cells' gene transcripts (Q20) in their true cell
	medianIoU float64 // median, over true cells with a nucleus in the section, of the IoU
}

func readColumn(t *testing.T, path, column string) [][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, _ := f.Stat()
	p, err := parquet.Open(f, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	v, err := p.ReadAll(p.Index(column))
	if err != nil {
		t.Fatal(err)
	}
	return v.Bytes
}

func openBundle(t *testing.T, path string) *xenium.Bundle {
	t.Helper()
	b, err := xenium.Open(path, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

// truthOf scores segmentations of a simulated bundle: each segmentation's cells are named by the
// instrument's cell ids (cells.parquet), matched to true cells through their nuclei.
type truthOf struct {
	trueCell  [][]byte          // each transcript's true cell id (UNASSIGNED: none)
	trueMask  *segment.Raster   // the true cells
	trueIDs   []string          // true cell ids by label − 1
	toTrue    map[string]string // instrument cell id → its true cell's id
	withNuc   map[string]bool   // true cells with a nucleus in the section
	instMask  *segment.Raster
	instIDs   []string
	instCells [][]byte // each transcript's instrument cell
}

func loadTruth(t *testing.T, folder string) *truthOf {
	t.Helper()
	b, tb := openBundle(t, folder), openBundle(t, filepath.Join(folder, "truth"))
	tr := &truthOf{toTrue: map[string]string{}, withNuc: map[string]bool{}}
	tr.trueCell = readColumn(t, filepath.Join(folder, "truth", "transcripts.parquet"), "cell_id")
	tr.instCells = readColumn(t, filepath.Join(folder, "transcripts.parquet"), "cell_id")
	var err error
	if tr.trueMask, err = readMask(tb, xenium.CellMask); err != nil {
		t.Fatal(err)
	}
	if tr.instMask, err = readMask(b, xenium.CellMask); err != nil {
		t.Fatal(err)
	}
	nuc, err := readMask(b, xenium.NucleusMask)
	if err != nil {
		t.Fatal(err)
	}
	tc, _ := tb.ReadCells()
	ic, _ := b.ReadCells()
	tr.trueIDs, tr.instIDs = tc.IDs, ic.IDs
	// Each instrument nucleus's true cell: the one holding most of its pixels.
	votes := map[uint64]int{}
	a, c := make([]uint32, nuc.Width), make([]uint32, nuc.Width)
	for y := 0; y < nuc.Height; y++ {
		nuc.Row(y, a)
		tr.trueMask.Row(y, c)
		for x, k := range a {
			if k > 0 && c[x] > 0 {
				votes[uint64(k)<<32|uint64(c[x])]++
			}
		}
	}
	best := map[uint32]int{}
	for key, v := range votes {
		k, tcell := uint32(key>>32), uint32(key)
		if v > best[k] {
			best[k] = v
			tr.toTrue[ic.IDs[k-1]] = tc.IDs[tcell-1]
		}
	}
	for _, id := range tr.toTrue {
		tr.withNuc[id] = true
	}
	return tr
}

// score measures a segmentation given each transcript's cell id (by row; only the rows in
// counted, the gene transcripts of quality 20 or more, which every segmentation counts) and a
// function naming the cell at an image pixel.
func (tr *truthOf) score(counted map[int]bool, cellOfRow func(row int) string, cellAt func(x, y int) string) score {
	var right, all float64
	for row, truth := range tr.trueCell {
		if string(truth) == "UNASSIGNED" || !counted[row] {
			continue
		}
		id := cellOfRow(row)
		if id == "" {
			all++
			continue
		}
		all++
		if tr.toTrue[id] == string(truth) {
			right++
		}
	}
	inter, union := map[string]int{}, map[string]int{}
	row := make([]uint32, tr.trueMask.Width)
	for y := 0; y < tr.trueMask.Height; y++ {
		tr.trueMask.Row(y, row)
		for x, l := range row {
			truth := ""
			if l > 0 {
				truth = tr.trueIDs[l-1]
			}
			got := ""
			if id := cellAt(x, y); id != "" {
				got = tr.toTrue[id]
			}
			if truth != "" && tr.withNuc[truth] {
				union[truth]++
				if got == truth {
					inter[truth]++
				}
			}
			if got != "" && got != truth {
				union[got]++
			}
		}
	}
	var ious []float64
	for id := range tr.withNuc {
		if union[id] > 0 {
			ious = append(ious, float64(inter[id])/float64(union[id]))
		}
	}
	sort.Float64s(ious)
	return score{accuracy: right / all, medianIoU: ious[len(ious)/2]}
}

func TestRecoversSimulatedCells(t *testing.T) {
	preset := "small"
	if p := os.Getenv("REFINE_PRESET"); p != "" {
		preset = p
	}
	folder := filepath.Join(t.TempDir(), preset)
	if _, err := simulate.Run(context.Background(), simulate.Presets[preset], folder, "test", nil); err != nil {
		t.Fatal(err)
	}
	b := openBundle(t, folder)
	in, idx, err := BundleInputs(b)
	if err != nil {
		t.Fatal(err)
	}
	// The default schedule, shortened: one stage of 1 µm voxels in one layer.
	opt := DefaultOptions()
	opt.Stages, opt.Samples = []Stage{{1, 1, 300}}, 250
	if v := os.Getenv("REFINE_STAGES"); v != "" {
		opt.Stages = nil
		for _, f := range strings.Split(v, ";") {
			var st Stage
			fmt.Sscanf(f, "%g,%d,%d", &st.VoxelUm, &st.Layers, &st.Sweeps)
			opt.Stages = append(opt.Stages, st)
		}
		opt.Samples = max(1, opt.Stages[len(opt.Stages)-1].Sweeps/2)
	}
	if v := os.Getenv("REFINE_SAMPLES"); v != "" {
		opt.Samples, _ = strconv.Atoi(v)
	}
	if os.Getenv("REFINE_NOREPOSITION") != "" {
		opt.Sampler.Reposition = false
	}
	start := time.Now()
	res, err := Run(context.Background(), in, opt, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d transcripts, %d cells (%d kept), started at stage %d; %v", len(idx.Rows), in.Cells, len(res.Kept), res.StartStage+1, time.Since(start))
	tr := loadTruth(t, folder)
	counted := map[int]bool{}
	for _, r := range idx.Rows {
		counted[r] = true
	}
	instrument := tr.score(counted, func(row int) string {
		if id := string(tr.instCells[row]); id != "UNASSIGNED" {
			return id
		}
		return ""
	}, func(x, y int) string {
		if l := tr.instMask.At(x, y); l > 0 {
			return tr.instIDs[l-1]
		}
		return ""
	})
	// The segmentation's cells by row: rows it did not segment (other features, low quality)
	// count against it only if they are cells' gene transcripts of quality 20, which it has.
	assigned := res.Assigned
	if os.Getenv("REFINE_LAST") != "" {
		assigned = res.State.Assignments()
	}
	byRow := map[int]string{}
	for i, r := range idx.Rows {
		if c := assigned[i]; c > 0 {
			byRow[r] = idx.CellIDs[res.Kept[c-1]]
		}
	}
	consensus := res.State.Lat.Consensus(res.Labels)
	if os.Getenv("REFINE_LAST") != "" {
		consensus = res.State.Consensus()
	}
	l := res.State.Lat
	ps := tr.trueMask.PixelSize
	refined := tr.score(counted, func(row int) string { return byRow[row] }, func(x, y int) string {
		cx := int((float64(x) + 0.5) * ps / l.VoxelUm)
		cy := int((float64(y) + 0.5) * ps / l.VoxelUm)
		if cx >= l.W || cy >= l.H {
			return ""
		}
		if c := consensus[cy*l.W+cx]; c > 0 {
			return idx.CellIDs[res.Kept[c-1]]
		}
		return ""
	})
	if os.Getenv("REFINE_DIAG") != "" {
		// Areas: the true cells', the instrument's, the refined (consensus) cells', µm².
		var trueArea, instArea float64
		row := make([]uint32, tr.trueMask.Width)
		for y := 0; y < tr.trueMask.Height; y++ {
			tr.trueMask.Row(y, row)
			for _, v := range row {
				if v > 0 {
					trueArea++
				}
			}
			tr.instMask.Row(y, row)
			for _, v := range row {
				if v > 0 {
					instArea++
				}
			}
		}
		var refArea float64
		for _, c := range consensus {
			if c > 0 {
				refArea++
			}
		}
		px2 := ps * ps
		t.Logf("areas (µm²): true %.0f, instrument %.0f, refined %.0f (of %.0f)", trueArea*px2, instArea*px2, refArea*l.VoxelUm*l.VoxelUm, in.WidthUm*in.HeightUm)
		// By observed place.
		obs := map[int]string{}
		for i, r := range idx.Rows {
			v := l.Voxel(float64(in.Transcripts.X[i]), float64(in.Transcripts.Y[i]), float64(in.Transcripts.Z[i]))
			if c := res.State.Labels[v]; c > 0 {
				obs[r] = idx.CellIDs[res.Kept[c-1]]
			}
		}
		o := tr.score(counted, func(row int) string { return obs[row] }, func(x, y int) string { return "" })
		// The ceiling: the true cells, transcripts assigned by where they were observed.
		trueByPlace := map[int]string{}
		for i, r := range idx.Rows {
			if c := tr.trueMask.AtMicrons(float64(in.Transcripts.X[i]), float64(in.Transcripts.Y[i])); c > 0 {
				trueByPlace[r] = tr.trueIDs[c-1]
			}
		}
		var right, all float64
		for _, r := range idx.Rows {
			if truth := string(tr.trueCell[r]); truth != "UNASSIGNED" {
				all++
				if trueByPlace[r] == truth {
					right++
				}
			}
		}
		t.Logf("the true cells, by observed place: %.1f%%", 100*right/all)
		bg := 0
		for _, own := range res.State.fromCell {
			if !own {
				bg++
			}
		}
		t.Logf("by observed place: %.1f%%; attributed to the background: %.1f%%", 100*o.accuracy, 100*float64(bg)/float64(len(res.State.fromCell)))
		m := res.Model
		var vol float64
		for _, v := range m.Volume {
			vol += v
		}
		t.Logf("mean cell volume %.0f µm³; volume prior means %v, variances %v; background rates sum %.4g", vol/float64(len(m.Volume)), m.VolMean, m.VolVar, func() float64 {
			var s float64
			for _, x := range m.Bg {
				s += x
			}
			return s
		}())
	}
	t.Logf("instrument: %.1f%% of transcripts in their true cell, median IoU %.3f", 100*instrument.accuracy, instrument.medianIoU)
	t.Logf("refined:    %.1f%% of transcripts in their true cell, median IoU %.3f", 100*refined.accuracy, refined.medianIoU)
	for i, tr := range res.Trace {
		if i%20 == 0 || i == len(res.Trace)-1 {
			t.Logf("sweep %d (stage %d): %d voxels moved, %d transcripts repositioned, log-likelihood %s", i+1, tr.Stage+1, tr.Moved, tr.Repositioned, strconv.FormatFloat(tr.LogLikelihood, 'f', 1, 64))
		}
	}
	if refined.accuracy <= instrument.accuracy {
		t.Errorf("the refined segmentation assigns %.1f%% of transcripts to their true cells, the expansion %.1f%%", 100*refined.accuracy, 100*instrument.accuracy)
	}
	if refined.medianIoU <= instrument.medianIoU {
		t.Errorf("the refined segmentation's median IoU %.3f, the expansion's %.3f", refined.medianIoU, instrument.medianIoU)
	}
}
