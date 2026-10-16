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
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"situcraft/internal/cluster"
	"situcraft/internal/label"
	"situcraft/internal/random"
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

// clustered clusters a data set's instrument segmentation with the defaults: its cells'
// expression, its counts, and the clusters' number and target total.
func clustered(t *testing.T, bundleDir string) (*cluster.Expression, *segment.Counts, int, float64) {
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
	return e, counts, len(r.Partition.Sizes), r.Target
}

// labelSet clusters a data set's instrument segmentation with the defaults and labels the cells
// from the marker set offered for it, into out.
func labelSet(t *testing.T, set, bundleDir, out string) (*label.Result, string) {
	t.Helper()
	e, _, clusters, target := clustered(t, bundleDir)
	b, err := xenium.Open(bundleDir, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	markers := markersFor(t, set, b)
	b.Close()
	res, err := label.Run(e, markers, clusters, target, label.DefaultParams())
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

// Labels imported for a bundle's own cells (wave 4, slice 4): each development bundle's graph-based
// clusters, written as a table (Barcode, Cluster), read and matched to the instrument segmentation's
// cells by id, every cell its cluster and the cells the instrument did not cluster without a label;
// the Lung bundle's table refused for the Breast bundle's cells, every row counted.
func TestImportedLabelsMatchTheBundles(t *testing.T) {
	tables := map[string]string{}
	cells := map[string][]string{}
	for _, set := range []string{"xenium-lung-2fov", "xenium-breast-2fov"} {
		dir := bundle(t, set)
		b, err := xenium.Open(dir, xenium.Options{CacheDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		seg := filepath.Join(t.TempDir(), "instrument")
		if err := os.MkdirAll(seg, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := imports.Instrument(context.Background(), b, seg, runtime.NumCPU()); err != nil {
			t.Fatal(err)
		}
		segCells, err := segment.ReadCells(seg)
		if err != nil {
			t.Fatal(err)
		}
		an, err := b.ReadAnalysis()
		b.Close()
		if err != nil {
			t.Fatal(err)
		}
		var graph *xenium.AnalysisClustering
		for i := range an.Clusterings {
			if an.Clusterings[i].Name == "gene_expression_graphclust" {
				graph = &an.Clusterings[i]
			}
		}
		if graph == nil {
			t.Fatalf("%s: no graph-based clusters", set)
		}
		var sb strings.Builder
		sb.WriteString("Barcode,Cluster\n")
		want := map[string]string{}
		for k, id := range graph.Barcodes {
			fmt.Fprintf(&sb, "%s,Cluster %d\n", id, graph.Cluster[k])
			want[id] = fmt.Sprintf("Cluster %d", graph.Cluster[k])
		}
		path := filepath.Join(t.TempDir(), set+".csv")
		if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		tables[set] = path
		ids := make([]string, len(segCells))
		for i, c := range segCells {
			ids[i] = c.ID
		}
		cells[set] = ids
		tab, err := label.ReadTable(path)
		if err != nil {
			t.Fatal(err)
		}
		info, roles := label.Describe(tab)
		if roles.ID != "Barcode" || roles.Label != "Cluster" || !info[0].Unique {
			t.Errorf("%s: roles %+v", set, roles)
		}
		imp, err := label.Import(tab, label.ImportSpec{IDColumn: roles.ID, Column: roles.Label}, ids)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		wrong, unlabeled := 0, 0
		for i, id := range ids {
			got := ""
			if k := imp.Label[i]; k >= 0 {
				got = imp.Labels[k]
			} else {
				unlabeled++
			}
			if got != want[id] {
				wrong++
			}
		}
		if wrong != 0 || unlabeled != len(ids)-len(graph.Barcodes) || imp.Match.Matched != len(graph.Barcodes) {
			t.Errorf("%s: %d cells with another label, %d without one (the instrument clusters %d of %d)", set, wrong, unlabeled, len(graph.Barcodes), len(ids))
		}
		t.Logf("%s: %d of %d cells labeled from %d rows, %d labels", set, imp.Match.Matched, len(ids), tab.Rows, len(imp.Labels))
	}
	tab, err := label.ReadTable(tables["xenium-lung-2fov"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := label.Import(tab, label.ImportSpec{IDColumn: "Barcode", Column: "Cluster"}, cells["xenium-breast-2fov"]); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d of xenium-lung-2fov.csv's %d rows name cells this segmentation does not have", tab.Rows, tab.Rows)) {
		t.Errorf("the Lung table for the Breast cells: %v", err)
	}
}

// The review queue's recall (wave 4, slice 5): 3% of a tissue's labeled cells given another class
// at random (seeded), each cell's agreement with its 10 nearest labeled cells, and the queue of the
// implausible (at most 20% of them sharing its label, most sharing another): of the planted
// mislabels, the share in the queue and in its first five times as many cells as were planted. On
// the simulated tissue, whose domains mix their types, a mislabel the neighbors share cannot be
// found by place; the Breast tissue's nests and stroma are more alike within.
func TestReviewQueueFindsPlantedMislabels(t *testing.T) {
	cases := []struct {
		set               string
		recall, recallTop float64
	}{
		{"simulated-standard", 0.72, 0.68},
		{"xenium-breast-2fov", 0.92, 0.92},
	}
	for _, c := range cases {
		t.Run(c.set, func(t *testing.T) {
			dir := filepath.Join(dataRoot(), c.set)
			if c.set == "simulated-standard" {
				dir = simulatedStandard(t)
			} else if bundle(t, c.set) == "" {
				return
			}
			res, _ := labelSet(t, c.set, dir, t.TempDir())
			b, err := xenium.Open(dir, xenium.Options{CacheDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			seg := filepath.Join(t.TempDir(), "instrument")
			if err := os.MkdirAll(seg, 0o755); err != nil {
				t.Fatal(err)
			}
			_, err = imports.Instrument(context.Background(), b, seg, runtime.NumCPU())
			b.Close()
			if err != nil {
				t.Fatal(err)
			}
			cells, err := segment.ReadCells(seg)
			if err != nil {
				t.Fatal(err)
			}
			xy := make([]float64, 2*len(cells))
			for i, cell := range cells {
				xy[2*i], xy[2*i+1] = float64(cell.X), float64(cell.Y)
			}
			var classes []int32
			for k, s := range res.Sets {
				if s.Level == 0 && !s.State {
					classes = append(classes, int32(k))
				}
			}
			lab := make([]int32, len(cells))
			for i := range lab {
				lab[i] = -1
			}
			s := random.New(20251016, 0)
			planted := map[int]bool{}
			for j, cell := range res.Cells {
				l := res.Assignment.At(0, j)
				if l >= 0 && s.Float64() < 0.03 {
					others := slices.DeleteFunc(slices.Clone(classes), func(x int32) bool { return x == l })
					l = others[s.IntN(len(others))]
					planted[int(cell)] = true
				}
				lab[cell] = l
			}
			share, dominant, err := label.Agreement(context.Background(), xy, lab, 10, runtime.NumCPU())
			if err != nil {
				t.Fatal(err)
			}
			items, counts := label.Queue(lab, share, dominant, nil, label.QueueOptions{Implausible: 0.2, LowConfidence: math.NaN()})
			found, top := 0, 0
			for r, it := range items {
				if planted[it.Cell] {
					found++
					if r < 5*len(planted) {
						top++
					}
				}
			}
			recall, recallTop := float64(found)/float64(len(planted)), float64(top)/float64(len(planted))
			t.Logf("%s: %d of %d labeled cells planted; the queue %d cells (%.1f%%); recall %.3f, in its first %d %.3f", c.set, len(planted), counts.Labeled, len(items), 100*float64(len(items))/float64(counts.Labeled), recall, 5*len(planted), recallTop)
			if recall < c.recall || recallTop < c.recallTop {
				t.Errorf("recall %.3f (at least %.2f), in the queue's first %d %.3f (at least %.2f)", recall, c.recall, 5*len(planted), recallTop, c.recallTop)
			}
		})
	}
}
