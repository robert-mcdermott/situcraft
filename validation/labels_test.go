package validation

// SituCraft's labels from markers (internal/label) against scanpy's score_genes, and on the
// simulated tissue against its true types (wave 4, slice 3; research.md §5.3): the instrument's
// segmentation of each reference data set, clustered with the defaults, labeled from the marker
// set SituCraft offers it (the Breast and Lung panels' starter sets, the simulated tissue's own).
//
// The maintainer makes the numbers in two steps:
//
//	SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceLabels -v
//	    writes SituCraft's labels into <data>/clustering/<set>/labels/ (not committed)
//	validation/reference/labels/compare.sh
//	    compares their scores with scanpy's and writes validation/reference/labels/<set>.json
//
// Otherwise (weekly, SITUCRAFT_REQUIRE_DATA) this test labels each data set again, requires the
// files to be, byte for byte, those compared, and checks the committed numbers against the
// tolerances below.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"situcraft/internal/cluster"
	"situcraft/internal/label"
	"situcraft/internal/segment"
	"situcraft/internal/segment/imports"
	"situcraft/internal/simulate"
	"situcraft/internal/xenium"
)

// labelNumbers are a data set's (validation/reference/labels/<set>.json), written by compare.py.
type labelNumbers struct {
	Set       string            `json:"dataSet"`
	Tools     map[string]string `json:"tools"`
	SituCraft struct {
		Digest   string `json:"digest"`
		Markers  string `json:"markers"`
		Checksum string `json:"checksum"`
		Cells    int    `json:"cells"`
		Labeled  []int  `json:"labeled"`
	} `json:"situcraft"`
	Scores struct {
		Lists            int     `json:"lists"`
		ControlsSame     int     `json:"controlsSame"`
		MaxAbs           float64 `json:"maxAbs"`
		ScanpyMaxAbsDiff float64 `json:"scanpyMaxAbsDiff"`
		CombinedMaxAbs   float64 `json:"combinedMaxAbs"`
	} `json:"scores"`
	Truth *struct {
		Cells     int     `json:"cells"`
		Right     float64 `json:"right"`
		Wrong     float64 `json:"wrong"`
		Unlabeled float64 `json:"unlabeled"`
	} `json:"truth"`
}

// The tolerances.
const (
	// Every list's control genes are scanpy's, and each cell's score scanpy's within float32's
	// rounding of the scores (scanpy scores the float32 matrix it reads).
	tolScoreScanpy = 1e-5
	// A label's score is its positive markers' less its negative markers' where above 0: recomputed
	// from the parts, within float32 rounding.
	tolScoreCombined = 1e-6
	// On the simulated tissue, of the cells whose true type is known: labeled with it, at least;
	// with another, at most.
	tolTruthRight = 0.96
	tolTruthWrong = 0.02
)

// labelSet clusters a data set's instrument segmentation with the defaults and labels the cells
// from the marker set offered for it, into out.
func labelSet(t *testing.T, set, bundleDir, out string) (*label.Result, string) {
	t.Helper()
	ctx := context.Background()
	b, err := xenium.Open(bundleDir, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	seg := filepath.Join(t.TempDir(), "instrument")
	if err := os.MkdirAll(seg, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := imports.Instrument(ctx, b, seg, runtime.NumCPU()); err != nil {
		t.Fatal(err)
	}
	counts, err := segment.ReadCounts(seg)
	if err != nil {
		t.Fatal(err)
	}
	r, err := cluster.Run(ctx, cluster.Inputs{Counts: counts}, cluster.DefaultParams(), runtime.NumCPU(), nil)
	if err != nil {
		t.Fatal(err)
	}
	clusters := make([]uint32, len(r.Cells))
	for i, c := range r.Partition.Community {
		clusters[i] = uint32(c)
	}
	e, err := cluster.LoadExpression(counts, r.Cells, clusters, r.Target)
	if err != nil {
		t.Fatal(err)
	}
	markers := markersFor(t, set, b)
	res, err := label.Run(e, markers, len(r.Partition.Sizes), r.Target, label.DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := label.Write(out, cluster.Side{ID: "instrument", Name: "Instrument segmentation"}, cluster.Side{ID: "clustering", Name: "Clusters"}, res); err != nil {
		t.Fatal(err)
	}
	digest, err := label.Digest(out)
	if err != nil {
		t.Fatal(err)
	}
	return res, digest
}

// markersFor is the marker set SituCraft offers a reference data set.
func markersFor(t *testing.T, set string, b *xenium.Bundle) *label.MarkerSet {
	t.Helper()
	if b.Simulation != nil {
		raw, err := b.ReadSmall(xenium.SimulationFile)
		if err != nil {
			t.Fatal(err)
		}
		var f struct {
			Parameters simulate.Params `json:"parameters"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		var types []string
		for _, ty := range f.Parameters.Types {
			types = append(types, ty.Name)
		}
		return label.Simulated(types, simulate.MarkerNames(f.Parameters))
	}
	for _, s := range label.Starters() {
		if strings.Contains(set, s.ID) {
			return s.Set
		}
	}
	t.Fatalf("no marker set for %s", set)
	return nil
}

func TestReferenceLabels(t *testing.T) {
	if testing.Short() || !writeReference() && os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 (or SITUCRAFT_WRITE_REFERENCE=1) to label the reference data sets")
	}
	for _, set := range referenceSets {
		t.Run(set, func(t *testing.T) {
			dir := filepath.Join(dataRoot(), set)
			if set == "simulated-standard" {
				dir = simulatedStandard(t)
			} else if bundle(t, set) == "" {
				return
			}
			if writeReference() {
				out := filepath.Join(dataRoot(), "clustering", set, "labels")
				os.RemoveAll(out)
				res, digest := labelSet(t, set, dir, out)
				abs, _ := filepath.Abs(dir)
				labeled := make([]int, res.Assignment.Levels)
				for l := range labeled {
					for _, k := range res.Assignment.Label[l] {
						if k >= 0 {
							labeled[l]++
						}
					}
				}
				info, _ := json.MarshalIndent(map[string]any{"digest": digest, "bundle": abs, "markers": res.Set.Name, "checksum": res.Set.Checksum(), "cells": len(res.Cells), "labeled": labeled}, "", " ")
				if err := os.WriteFile(filepath.Join(dataRoot(), "clustering", set, "labels.json"), append(info, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: %d cells labeled by %s %v, digest %.16s; now run validation/reference/labels/compare.sh", set, len(res.Cells), res.Set.Name, labeled, digest)
				return
			}
			raw, err := os.ReadFile(filepath.Join("reference", "labels", set+".json"))
			if err != nil {
				t.Fatalf("%v (made by SITUCRAFT_WRITE_REFERENCE=1 and compare.sh)", err)
			}
			var n labelNumbers
			if err := json.Unmarshal(raw, &n); err != nil {
				t.Fatal(err)
			}
			_, digest := labelSet(t, set, dir, filepath.Join(t.TempDir(), "labels"))
			if digest != n.SituCraft.Digest {
				t.Errorf("SituCraft's labels of %s are not the ones their reference numbers were made from (digest %.16s, recorded %.16s): make them again", set, digest, n.SituCraft.Digest)
			}
			for _, c := range labelChecks(&n) {
				if c.Pass {
					t.Logf("met: %s: %.6g (%s %.6g)", c.Name, c.Value, c.Tolerance, c.Limit)
				} else {
					t.Errorf("not met: %s: %.6g (%s %.6g)", c.Name, c.Value, c.Tolerance, c.Limit)
				}
			}
		})
	}
}

func labelChecks(n *labelNumbers) []check {
	s := n.Scores
	out := []check{
		{Name: "lists whose control genes are not scanpy's", Tolerance: "≤", Value: float64(s.Lists - s.ControlsSame), Limit: 0, Pass: s.ControlsSame == s.Lists && s.Lists > 0},
		{Name: "each cell's scores against scanpy's score_genes (max |difference|)", Tolerance: "≤", Value: s.ScanpyMaxAbsDiff, Limit: tolScoreScanpy, Pass: s.ScanpyMaxAbsDiff <= tolScoreScanpy},
		{Name: "each label's score from its parts (max |difference|)", Tolerance: "≤", Value: s.CombinedMaxAbs, Limit: tolScoreCombined, Pass: s.CombinedMaxAbs <= tolScoreCombined},
	}
	if t := n.Truth; t != nil {
		out = append(out,
			check{Name: "cells labeled with their true type", Tolerance: "≥", Value: t.Right, Limit: tolTruthRight, Pass: t.Right >= tolTruthRight},
			check{Name: "cells labeled with another type", Tolerance: "≤", Value: t.Wrong, Limit: tolTruthWrong, Pass: t.Wrong <= tolTruthWrong})
	}
	return out
}

// Every marker of the sets SituCraft ships is in its panel (the development bundles' panels).
func TestStarterSetsFitTheirPanels(t *testing.T) {
	for _, s := range label.Starters() {
		set := "xenium-" + s.ID + "-2fov"
		dir := bundle(t, set)
		raw, err := os.ReadFile(filepath.Join(dir, "gene_panel.json"))
		if err != nil {
			t.Fatal(err)
		}
		var panel struct {
			Payload struct {
				Panel struct {
					Identity struct {
						DesignID string `json:"design_id"`
					} `json:"identity"`
				} `json:"panel"`
				Targets []struct {
					Type struct {
						Data struct {
							Name string `json:"name"`
						} `json:"data"`
						Descriptor string `json:"descriptor"`
					} `json:"type"`
				} `json:"targets"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &panel); err != nil {
			t.Fatal(err)
		}
		var genes []string
		for _, target := range panel.Payload.Targets {
			if target.Type.Descriptor == "gene" {
				genes = append(genes, target.Type.Data.Name)
			}
		}
		if s.Set.Panel != panel.Payload.Panel.Identity.DesignID {
			t.Errorf("%s names the panel %s; %s's is %s", s.ID, s.Set.Panel, set, panel.Payload.Panel.Identity.DesignID)
		}
		if _, err := s.Set.Resolve(genes); err != nil {
			t.Errorf("%s: %v", s.ID, err)
		}
	}
}
