package validation

// SituCraft's clustering (internal/cluster) against scanpy, leidenalg, umap-learn and
// scikit-learn on the reference data sets (wave 4, slice 1; research.md §5): the instrument's
// segmentation of each, clustered with the defaults.
//
// The maintainer makes the numbers in two steps:
//
//	SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceClustering -v
//	    writes SituCraft's clusterings into <data>/clustering/<set>/situcraft/ (not committed)
//	validation/reference/clustering/compare.sh
//	    compares them with the libraries and writes validation/reference/clustering/<set>.json
//
// Otherwise (weekly, SITUCRAFT_REQUIRE_DATA) this test clusters each data set again, requires the
// files to be, byte for byte, those compared, and checks the committed numbers against the
// tolerances below.

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"situcraft/internal/cluster"
	"situcraft/internal/embed"
	"situcraft/internal/segment"
	"situcraft/internal/segment/imports"
	"situcraft/internal/xenium"
)

// clusteringNumbers are a data set's (validation/reference/clustering/<set>.json), written by
// compare.py.
type clusteringNumbers struct {
	Set       string `json:"dataSet"`
	SituCraft struct {
		Version    string  `json:"version"`
		Digest     string  `json:"digest"`
		MapDigest  string  `json:"mapDigest"`
		Cells      int     `json:"cells"`
		Clusters   int     `json:"clusters"`
		Modularity float64 `json:"modularity"`
	} `json:"situcraft"`
	Map struct {
		A                   float64   `json:"a"`
		B                   float64   `json:"b"`
		UMAPA               float64   `json:"umapA"`
		UMAPB               float64   `json:"umapB"`
		Trustworthiness     float64   `json:"trustworthiness"`
		Recomputed          float64   `json:"trustworthinessRecomputed"`
		UMAPTrustworthiness []float64 `json:"umapTrustworthiness"`
		Precision           float64   `json:"precision"`
		Recall              float64   `json:"recall"`
		UMAPRecall          []float64 `json:"umapRecall"`
	} `json:"map"`
	Tools map[string]string `json:"tools"`
	Cells struct {
		Same     bool `json:"same"`
		Analyzed int  `json:"analyzed"`
	} `json:"cells"`
	PCA struct {
		VarianceRelDiff float64 `json:"varianceRelDiff"`
		RatioRelDiff    float64 `json:"ratioRelDiff"`
		CosMin10        float64 `json:"cosMin10"`
		SubspaceCos50   float64 `json:"subspaceCos50"`
		ScoreMaxDiff    float64 `json:"scoreMaxDiff"`
	} `json:"pca"`
	Neighbors struct {
		SameSets    float64 `json:"sameSets"`
		MeanJaccard float64 `json:"meanJaccard"`
	} `json:"neighbors"`
	Graph struct {
		ModularityOnUMAPGraph float64 `json:"modularityOnUmapGraph"`
		WeightMaxDiff         float64 `json:"weightMaxDiff"`
	} `json:"graph"`
	Leiden struct {
		Leidenalg      []float64 `json:"leidenalg"`
		Clusters       []int     `json:"clusters"`
		ARIWith        []float64 `json:"ariWithLeidenalg"`
		ARIAmong       []float64 `json:"ariAmongLeidenalg"`
		ModularityDiff float64   `json:"modularityOursOnTheirs"`
	} `json:"leiden"`
	Markers struct {
		ScoreMaxDiff     float64 `json:"scoreMaxDiff"`
		ExactScore       float64 `json:"exactScoreMaxDiff"`
		ExactLog10PAdj   float64 `json:"exactLog10PadjMaxDiff"`
		LogFCMaxDiff     float64 `json:"logfcMaxDiff"`
		PctMaxDiff       float64 `json:"pctMaxDiff"`
		Log10PAdjMaxDiff float64 `json:"log10PadjMaxDiff"`
		TopAgreement     float64 `json:"top10Agreement"`
	} `json:"markers"`
	EndToEnd struct {
		ARI      float64 `json:"ari"`
		Clusters int     `json:"clustersScanpy"`
	} `json:"endToEnd"`
	Truth *struct {
		Types          int     `json:"types"`
		ARISituCraft   float64 `json:"ariSituCraft"`
		ARIScanpy      float64 `json:"ariScanpy"`
		ARIInstrument  float64 `json:"ariInstrumentGraphclust"`
		ClustersScanpy int     `json:"clustersScanpy"`
	} `json:"truth"`
	Instrument *struct {
		ARI      float64 `json:"ariGraphclust"`
		Clusters int     `json:"clustersGraphclust"`
	} `json:"instrument"`
}

// The tolerances (research.md §5), set before the comparison was made, each with its reason.
const (
	// The components are exact in both: their variances agree to rounding, and the leading
	// components (well separated) point the same way.
	tolVariance     = 1e-6
	tolCosLeading   = 0.9999
	tolSubspace     = 0.999
	tolNeighborSets = 0.999
	tolGraphQ       = 1e-5
	// The rank-sum test with exact ties computes the same sums: rounding alone.
	tolMarkerExact = 1e-9
	// scanpy ranks rounded values, which can part cells expressing a gene equally (research.md §5):
	// its scores differ from the exact test's by a few hundredths at most.
	tolMarkerScanpy  = 0.05
	tolMarkerLogFC   = 1e-4
	tolMarkerPct     = 1e-9
	tolMarkerTop     = 0.99
	tolLeidenQ       = 0.003
	tolLeidenARI     = 0.10
	tolTruthARI      = 0.02
	tolTruthARIFloor = 0.85
	// The map: the curve fitted as umap-learn fits it; its trustworthiness recomputed independently
	// from its places; no less trustworthy than umap-learn's least of five seeds on the same graph,
	// less a hundredth (UMAP is random, and maps of different seeds differ by about that).
	tolCurve      = 1e-5
	tolRecomputed = 1e-4
	tolTrustUMAP  = 0.01
)

// clusterCheck evaluates the tolerances on a data set's numbers.
func clusterChecks(n *clusteringNumbers) []check {
	var out []check
	add := func(name, tolerance string, value, limit float64, pass bool) {
		out = append(out, check{Name: n.Set + ": " + name, Tolerance: tolerance, Value: value, Limit: limit, Pass: pass})
	}
	add("the same cells analyzed", "all", b2f(n.Cells.Same), 1, n.Cells.Same)
	add("components' variances, relative difference", "at most", n.PCA.VarianceRelDiff, tolVariance, n.PCA.VarianceRelDiff <= tolVariance)
	add("the first ten components' directions, |cos|", "at least", n.PCA.CosMin10, tolCosLeading, n.PCA.CosMin10 >= tolCosLeading)
	add("the fifty components' subspace, cos of the largest angle", "at least", n.PCA.SubspaceCos50, tolSubspace, n.PCA.SubspaceCos50 >= tolSubspace)
	add("cells with the same neighbors as exact search", "at least", n.Neighbors.SameSets, tolNeighborSets, n.Neighbors.SameSets >= tolNeighborSets)
	q := n.SituCraft.Modularity
	add("modularity on umap-learn's graph of the same neighbors, difference", "at most", math.Abs(n.Graph.ModularityOnUMAPGraph-q), tolGraphQ, math.Abs(n.Graph.ModularityOnUMAPGraph-q) <= tolGraphQ)
	theirs := append([]float64(nil), n.Leiden.Leidenalg...)
	sort.Float64s(theirs)
	if len(theirs) > 0 {
		low := theirs[0] - tolLeidenQ
		add("modularity against leidenalg's lowest over ten seeds", "at least", q, low, q >= low)
		with := median(n.Leiden.ARIWith)
		among := median(n.Leiden.ARIAmong)
		add("agreement with leidenalg (median ARI) against leidenalg's with itself", "at least", with, among-tolLeidenARI, with >= among-tolLeidenARI)
	}
	add("markers' scores against scipy's rank-sum test with exact ties, largest difference", "at most", n.Markers.ExactScore, tolMarkerExact, n.Markers.ExactScore <= tolMarkerExact)
	add("markers' adjusted p-values against it, largest difference of log₁₀", "at most", n.Markers.ExactLog10PAdj, tolMarkerExact, n.Markers.ExactLog10PAdj <= tolMarkerExact)
	add("markers' scores against scanpy's, largest difference", "at most", n.Markers.ScoreMaxDiff, tolMarkerScanpy, n.Markers.ScoreMaxDiff <= tolMarkerScanpy)
	add("markers' log₂ fold changes, largest difference", "at most", n.Markers.LogFCMaxDiff, tolMarkerLogFC, n.Markers.LogFCMaxDiff <= tolMarkerLogFC)
	add("markers' shares expressing, largest difference", "at most", n.Markers.PctMaxDiff, tolMarkerPct, n.Markers.PctMaxDiff <= tolMarkerPct)
	add("each cluster's first ten markers, the same genes", "at least", n.Markers.TopAgreement, tolMarkerTop, n.Markers.TopAgreement >= tolMarkerTop)
	add("the map's curve (a, b) against umap-learn's, largest difference", "at most", math.Max(math.Abs(n.Map.A-n.Map.UMAPA), math.Abs(n.Map.B-n.Map.UMAPB)), tolCurve, math.Max(math.Abs(n.Map.A-n.Map.UMAPA), math.Abs(n.Map.B-n.Map.UMAPB)) <= tolCurve)
	add("the map's trustworthiness, recomputed from its places", "within", math.Abs(n.Map.Recomputed-n.Map.Trustworthiness), tolRecomputed, math.Abs(n.Map.Recomputed-n.Map.Trustworthiness) <= tolRecomputed)
	if len(n.Map.UMAPTrustworthiness) > 0 {
		low := slicesMin(n.Map.UMAPTrustworthiness) - tolTrustUMAP
		add("the map's trustworthiness against umap-learn's lowest of five seeds on the same graph", "at least", n.Map.Trustworthiness, low, n.Map.Trustworthiness >= low)
	}
	if t := n.Truth; t != nil {
		low := math.Max(tolTruthARIFloor, t.ARIScanpy-tolTruthARI)
		add("agreement with the true cell types (ARI)", "at least", t.ARISituCraft, low, t.ARISituCraft >= low)
	}
	return out
}

func slicesMin(v []float64) float64 {
	m := math.Inf(1)
	for _, x := range v {
		m = math.Min(m, x)
	}
	return m
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// clusterSet clusters a data set's instrument segmentation with the defaults into out.
func clusterSet(t *testing.T, bundleDir, out string) *cluster.Result {
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
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cluster.Write(out, cluster.Side{ID: "instrument", Name: "Instrument segmentation"}, nil, r); err != nil {
		t.Fatal(err)
	}
	return r
}

// mapSet lays out a clustering's cells (its folder) with the defaults into out.
func mapSet(t *testing.T, clustering, out string) *embed.Result {
	t.Helper()
	m, err := embed.Run(context.Background(), clustering, cluster.Side{ID: "clustering", Name: "Clusters"}, embed.DefaultParams(), runtime.NumCPU(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := embed.Write(out, m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReferenceClustering(t *testing.T) {
	if testing.Short() || !writeReference() && os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 (or SITUCRAFT_WRITE_REFERENCE=1) to cluster the reference data sets")
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
				out := filepath.Join(dataRoot(), "clustering", set, "situcraft")
				os.RemoveAll(out)
				r := clusterSet(t, dir, out)
				digest, err := cluster.Digest(out)
				if err != nil {
					t.Fatal(err)
				}
				mapOut := filepath.Join(dataRoot(), "clustering", set, "map")
				os.RemoveAll(mapOut)
				mres := mapSet(t, out, mapOut)
				mapDigest, err := embed.Digest(mapOut)
				if err != nil {
					t.Fatal(err)
				}
				abs, _ := filepath.Abs(dir)
				info, _ := json.MarshalIndent(map[string]any{"digest": digest, "mapDigest": mapDigest, "cells": len(r.Cells), "clusters": len(r.Partition.Sizes), "modularity": r.Partition.Modularity, "bundle": abs, "map": mres.Description}, "", " ")
				if err := os.WriteFile(filepath.Join(dataRoot(), "clustering", set, "situcraft.json"), append(info, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: %d clusters of %d cells, modularity %.4f, digest %s; now run validation/reference/clustering/compare.sh", set, len(r.Partition.Sizes), len(r.Cells), r.Partition.Modularity, digest[:16])
				return
			}
			raw, err := os.ReadFile(filepath.Join("reference", "clustering", set+".json"))
			if err != nil {
				t.Fatalf("%v (made by SITUCRAFT_WRITE_REFERENCE=1 and compare.sh)", err)
			}
			var n clusteringNumbers
			if err := json.Unmarshal(raw, &n); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "situcraft")
			clusterSet(t, dir, out)
			digest, err := cluster.Digest(out)
			if err != nil {
				t.Fatal(err)
			}
			if digest != n.SituCraft.Digest {
				t.Errorf("SituCraft's clustering of %s is not the one its reference numbers were made from (digest %s, recorded %s): make them again", set, digest[:16], n.SituCraft.Digest[:min(16, len(n.SituCraft.Digest))])
			}
			mapOut := filepath.Join(t.TempDir(), "map")
			mapSet(t, out, mapOut)
			if mapDigest, err := embed.Digest(mapOut); err != nil || mapDigest != n.SituCraft.MapDigest {
				t.Errorf("SituCraft's map of %s is not the one its reference numbers were made from (digest %.16s, recorded %.16s): make them again", set, mapDigest, n.SituCraft.MapDigest)
			}
			for _, c := range clusterChecks(&n) {
				if c.Pass {
					t.Logf("met: %s: %.6g (%s %.6g)", c.Name, c.Value, c.Tolerance, c.Limit)
				} else {
					t.Errorf("not met: %s: %.6g (%s %.6g)", c.Name, c.Value, c.Tolerance, c.Limit)
				}
			}
			t.Logf("%s: SituCraft %d clusters; scanpy's own pipeline %d (ARI %.3f)", set, n.SituCraft.Clusters, n.EndToEnd.Clusters, n.EndToEnd.ARI)
			if n.Instrument != nil {
				t.Logf("%s: the instrument's graph-based clusters: %d (ARI with SituCraft's %.3f)", set, n.Instrument.Clusters, n.Instrument.ARI)
			}
		})
	}
}
