package validation

// SituCraft's labels from a reference (internal/label, reference.go) against SingleR's on the
// reference data sets, and on the simulated tissue against its true types (wave 4, slice 6;
// research.md §5.6). The bundles are mapped to the Human Primary Cell Atlas's main labels (each
// label's mean log-expression, as celldex packages the atlas); the simulated tissue to a
// single-cell reference of its own types (internal/simulate, reference.go).
//
// The maintainer makes the numbers in steps:
//
//	validation/reference/references/compare.sh reference
//	    writes the atlas's profiles into <data>/references/ (R, celldex)
//	SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceMapping -v
//	    maps each data set's cells, into <data>/clustering/<set>/reference/, and writes the cells'
//	    counts (and the simulated reference's) for SingleR into <data>/references/<set>/
//	validation/reference/references/compare.sh
//	    runs SingleR on the same cells and writes validation/reference/references/<set>.json
//
// Otherwise (weekly, SITUCRAFT_REQUIRE_DATA) this test maps each data set again, requires the
// files to be, byte for byte, those compared, and checks the committed numbers against the
// tolerances below. The atlas is not fetched weekly: its sets are skipped without its profiles.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"context"

	"situcraft/internal/cluster"
	"situcraft/internal/label"
	"situcraft/internal/segment"
	"situcraft/internal/simulate"
	"situcraft/internal/xenium"
)

// refNumbers are a data set's (validation/reference/references/<set>.json), written by compare.py.
type refNumbers struct {
	Set       string            `json:"dataSet"`
	Tools     map[string]string `json:"tools"`
	SituCraft struct {
		Digest    string `json:"digest"`
		Reference string `json:"reference"`
		SHA256    string `json:"sha256"`
		Cells     int    `json:"cells"`
		Labeled   int    `json:"labeled"`
		Shared    int    `json:"shared"`
		Compared  int    `json:"compared"`
	} `json:"situcraft"`
	// SingleR given the same profiles: its first-round scores against SituCraft's, and the share of
	// cells labeled alike; SingleR at its defaults on the reference's own cells or samples: the
	// share labeled alike.
	SingleR struct {
		Aggregated struct {
			ScoreMaxAbsDiff float64 `json:"scoreMaxAbsDiff"`
			Agreement       float64 `json:"agreement"`
			FirstAgreement  float64 `json:"firstAgreement"`
		} `json:"aggregated"`
		Default struct {
			Agreement float64 `json:"agreement"`
		} `json:"default"`
	} `json:"singler"`
	Truth *struct {
		Cells            int     `json:"cells"`
		SituCraft        float64 `json:"situcraft"`
		SingleRDefault   float64 `json:"singlerDefault"`
		SingleRAggregate float64 `json:"singlerAggregated"`
	} `json:"truth"`
}

// The tolerances.
const (
	// SingleR given the same profiles scores each cell as SituCraft does: the same Spearman
	// correlations, within float32's rounding of the scores SituCraft records.
	tolRefScore = 1e-5
	// And labels the cells alike, but for cells whose labels' scores tie within rounding in
	// fine-tuning.
	tolRefAgreement = 0.995
	// On the simulated tissue, of the cells whose true type is known: labeled with it, at least.
	tolRefTruth = 0.95
)

// atlasProfiles is the Human Primary Cell Atlas's main labels' profiles (compare.sh reference).
func atlasProfiles() string { return filepath.Join(dataRoot(), "references", "hpca-main.csv") }

// simulatedReference writes the simulated tissue's reference of its types (300 cells of each),
// answering its path.
func simulatedReference(t *testing.T, bundleDir string) string {
	t.Helper()
	b, err := xenium.Open(bundleDir, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := b.ReadSmall(xenium.SimulationFile)
	b.Close()
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Parameters simulate.Params `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataRoot(), "references", "simulated-standard-reference.zarr")
	os.RemoveAll(path)
	if err := simulate.WriteReference(f.Parameters, path, 300, simulate.ReferenceDepth, f.Parameters.Seed); err != nil {
		t.Fatal(err)
	}
	return path
}

// mapToReference maps a data set's clustered cells to its reference, into out: its expression, the
// segmentation's counts, the result and the digest.
func mapToReference(t *testing.T, bundleDir string, spec label.RefSpec, out string) (*cluster.Expression, *segment.Counts, *label.RefResult, string) {
	t.Helper()
	e, counts, clusters, target := clustered(t, bundleDir)
	ref, err := label.ReadReference(spec, e.Genes)
	if err != nil {
		t.Fatal(err)
	}
	res, err := label.MapReference(context.Background(), e, ref, label.DefaultRefParams(), clusters, target, runtime.NumCPU())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := label.WriteReference(out, cluster.Side{ID: "instrument", Name: "Instrument segmentation"}, cluster.Side{ID: "clustering", Name: "Clusters"}, res); err != nil {
		t.Fatal(err)
	}
	digest, err := label.Digest(out)
	if err != nil {
		t.Fatal(err)
	}
	return e, counts, res, digest
}

// writeCounts writes cells' counts as Matrix Market (genes by cells, as SingleR takes them), with
// the genes' names and the cells' indices in the segmentation.
func writeCounts(t *testing.T, dir string, genes []string, cells []uint32, row func(i int) ([]int, []uint32)) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	type entry struct {
		g, c int
		n    uint32
	}
	var entries []entry
	for i := range cells {
		gs, ns := row(i)
		for k, g := range gs {
			entries = append(entries, entry{g, i, ns[k]})
		}
	}
	f, err := os.Create(filepath.Join(dir, "counts.mtx"))
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "%%%%MatrixMarket matrix coordinate integer general\n%d %d %d\n", len(genes), len(cells), len(entries))
	for _, x := range entries {
		fmt.Fprintf(w, "%d %d %d\n", x.g+1, x.c+1, x.n)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	lines := func(name string, v []string) {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range v {
			fmt.Fprintln(f, s)
		}
		f.Close()
	}
	lines("genes.txt", genes)
	ids := make([]string, len(cells))
	for i, c := range cells {
		ids[i] = fmt.Sprint(c)
	}
	lines("cells.txt", ids)
}

func TestReferenceMapping(t *testing.T) {
	if testing.Short() || !writeReference() && os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 (or SITUCRAFT_WRITE_REFERENCE=1) to map the reference data sets")
	}
	for _, set := range referenceSets {
		t.Run(set, func(t *testing.T) {
			dir := filepath.Join(dataRoot(), set)
			var spec label.RefSpec
			if set == "simulated-standard" {
				dir = simulatedStandard(t)
				spec = label.RefSpec{Path: simulatedReference(t, dir), Matrix: "X", LabelColumn: "cell_type"}
			} else {
				if bundle(t, set) == "" {
					return
				}
				if _, err := os.Stat(atlasProfiles()); err != nil {
					t.Skipf("%s is missing (validation/reference/references/compare.sh reference)", atlasProfiles())
				}
				spec = label.RefSpec{Path: atlasProfiles(), GeneColumn: "gene"}
			}
			if writeReference() {
				out := filepath.Join(dataRoot(), "clustering", set, "reference")
				os.RemoveAll(out)
				e, counts, res, digest := mapToReference(t, dir, spec, out)
				query := filepath.Join(dataRoot(), "references", set)
				os.RemoveAll(query)
				writeCounts(t, query, counts.Genes, e.Cells, func(i int) ([]int, []uint32) {
					c := e.Cells[i]
					var gs []int
					var ns []uint32
					for k := counts.Offsets[c]; k < counts.Offsets[c+1]; k++ {
						gs = append(gs, int(counts.Gene[k]))
						ns = append(ns, counts.Count[k])
					}
					return gs, ns
				})
				if set == "simulated-standard" {
					writeSimulatedReference(t, dir, filepath.Join(query, "reference"))
				}
				labeled := 0
				for _, k := range res.Label {
					if k >= 0 {
						labeled++
					}
				}
				abs, _ := filepath.Abs(dir)
				refAbs, _ := filepath.Abs(spec.Path)
				info, _ := json.MarshalIndent(map[string]any{"digest": digest, "bundle": abs, "reference": refAbs, "sha256": res.Ref.SHA256, "cells": len(res.Cells), "labeled": labeled, "shared": len(res.Ref.Genes), "compared": len(res.Compared)}, "", " ")
				if err := os.WriteFile(filepath.Join(dataRoot(), "clustering", set, "reference.json"), append(info, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: %d of %d cells labeled from %s (%d genes shared, %d compared), digest %.16s; now run validation/reference/references/compare.sh", set, labeled, len(res.Cells), res.Ref.File, len(res.Ref.Genes), len(res.Compared), digest)
				return
			}
			raw, err := os.ReadFile(filepath.Join("reference", "references", set+".json"))
			if err != nil {
				t.Fatalf("%v (made by SITUCRAFT_WRITE_REFERENCE=1 and compare.sh)", err)
			}
			var n refNumbers
			if err := json.Unmarshal(raw, &n); err != nil {
				t.Fatal(err)
			}
			_, _, _, digest := mapToReference(t, dir, spec, filepath.Join(t.TempDir(), "reference"))
			if digest != n.SituCraft.Digest {
				t.Errorf("SituCraft's labels of %s are not the ones their reference numbers were made from (digest %.16s, recorded %.16s): make them again", set, digest, n.SituCraft.Digest)
			}
			for _, c := range refChecks(&n) {
				if c.Pass {
					t.Logf("met: %s: %.6g (%s %.6g)", c.Name, c.Value, c.Tolerance, c.Limit)
				} else {
					t.Errorf("not met: %s: %.6g (%s %.6g)", c.Name, c.Value, c.Tolerance, c.Limit)
				}
			}
		})
	}
}

// writeSimulatedReference writes the simulated reference's cells for SingleR: their counts, genes
// and types.
func writeSimulatedReference(t *testing.T, bundleDir, dir string) {
	t.Helper()
	b, err := xenium.Open(bundleDir, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := b.ReadSmall(xenium.SimulationFile)
	b.Close()
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Parameters simulate.Params `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	rows, labels, genes := simulate.ReferenceCells(f.Parameters, 300, simulate.ReferenceDepth, f.Parameters.Seed)
	cells := make([]uint32, len(rows))
	for i := range cells {
		cells[i] = uint32(i)
	}
	writeCounts(t, dir, genes, cells, func(i int) ([]int, []uint32) {
		var gs []int
		var ns []uint32
		for g, n := range rows[i] {
			if n > 0 {
				gs = append(gs, g)
				ns = append(ns, n)
			}
		}
		return gs, ns
	})
	out, err := os.Create(filepath.Join(dir, "labels.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range labels {
		fmt.Fprintln(out, l)
	}
	out.Close()
}

func refChecks(n *refNumbers) []check {
	a := n.SingleR.Aggregated
	out := []check{
		{Name: "each cell's first-round correlations against SingleR's, given the same profiles (max |difference|)", Tolerance: "≤", Value: a.ScoreMaxAbsDiff, Limit: tolRefScore, Pass: a.ScoreMaxAbsDiff <= tolRefScore},
		{Name: "cells labeled as SingleR labels them, given the same profiles", Tolerance: "≥", Value: a.Agreement, Limit: tolRefAgreement, Pass: a.Agreement >= tolRefAgreement},
	}
	if t := n.Truth; t != nil {
		out = append(out, check{Name: "cells labeled with their true type", Tolerance: "≥", Value: t.SituCraft, Limit: tolRefTruth, Pass: t.SituCraft >= tolRefTruth && !math.IsNaN(t.SituCraft)})
	}
	return out
}
