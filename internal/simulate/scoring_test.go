package simulate

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"situcraft/internal/compare"
	"situcraft/internal/segment/expand"
	"situcraft/internal/segment/imports"
	"situcraft/internal/xenium"
)

// A simulated bundle is used as a measured one is: its instrument's cells, SituCraft's nucleus
// expansion and the truth (imported as a Xenium Ranger bundle of the same run) are segmentations
// of it, and each compares with the truth.
func TestScoringAgainstTheTruth(t *testing.T) {
	folder, sum := smallBundle(t)
	b, err := xenium.Open(folder, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx, workers := context.Background(), runtime.NumCPU()
	load := func(make func(dir string) error) *compare.Seg {
		dir := t.TempDir()
		if err := make(dir); err != nil {
			t.Fatal(err)
		}
		s, err := compare.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	instrument := load(func(dir string) error { _, err := imports.Instrument(ctx, b, dir, workers); return err })
	expansion := load(func(dir string) error {
		_, err := expand.FromBundle(ctx, b, dir, expand.Params{DistanceUm: 5}, workers, nil)
		return err
	})
	truth := load(func(dir string) error {
		ds, err := imports.BundleDataset(b)
		if err != nil {
			return err
		}
		d, findings, _, err := imports.Run(ctx, ds, imports.Options{Format: imports.Xenium, Paths: []string{filepath.Join(folder, "truth")}}, dir, workers, nil)
		for _, f := range findings {
			t.Logf("truth import: %s %s: %s", f.Severity, f.Code, f.Message)
		}
		// Named as what it is, not as the instrument's software.
		if err == nil && !strings.Contains(string(d.Parameters), `"tool":"Simulated truth"`) {
			t.Errorf("the truth imported as %s", d.Parameters)
		}
		return err
	})
	if len(truth.Cells) != sum.Cells {
		t.Errorf("the truth imports %d cells, of %d", len(truth.Cells), sum.Cells)
	}
	if len(instrument.Cells) != sum.InstrumentCells {
		t.Errorf("the instrument's segmentation imports %d cells, of %d", len(instrument.Cells), sum.InstrumentCells)
	}
	nuclei, err := expand.NucleusRaster(b)
	if err != nil {
		t.Fatal(err)
	}
	transcripts, err := compare.BundleTranscripts(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		seg  *compare.Seg
	}{{"instrument", instrument}, {"expansion", expansion}} {
		r, err := compare.Compare(ctx, compare.Inputs{A: c.seg, B: truth, Nuclei: nuclei, Transcripts: transcripts, Params: compare.DefaultParams()})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s against the truth: classes %v, median IoU %.3f, pixel agreement %.3f, q20 transcripts changing cell %.3f",
			c.name, r.Classes, float64(r.IoU.Median), r.PixelAgreement, r.ChangesQ20.Fraction)
		// Nucleus expansion finds most true cells, imperfectly: the truth is not an expansion.
		if r.Classes[compare.OneToOne]+r.Classes[compare.Expanded]+r.Classes[compare.Contracted] < sum.InstrumentCells*3/4 {
			t.Errorf("%s: too few cells correspond to true ones", c.name)
		}
		if iou := float64(r.IoU.Median); iou < 0.5 || iou > 0.97 {
			t.Errorf("%s: a median IoU of %.3f with the truth", c.name, iou)
		}
	}
}

// A cell mask that is not of the bundle's cells (here the truth's, with more cells than the
// instrument's) is refused by the expansion, with a reason, instead of read past cells.parquet.
func TestExpansionRefusesAMaskOfOtherCells(t *testing.T) {
	folder, _ := smallBundle(t)
	mixed := filepath.Join(t.TempDir(), "mixed")
	if err := os.CopyFS(mixed, os.DirFS(folder)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(folder, "truth", "cells.zarr.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mixed, "cells.zarr.zip"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := xenium.Open(mixed, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	_, err = expand.FromBundle(context.Background(), b, t.TempDir(), expand.Params{DistanceUm: 5}, 2, nil)
	if err == nil || !strings.Contains(err.Error(), "not of these cells") {
		t.Fatalf("expansion with another segmentation's mask: %v", err)
	}
}
