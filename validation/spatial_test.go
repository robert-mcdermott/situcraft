package validation

// Neighborhood enrichment and co-occurrence (wave 6, slice 2; research.md §6.2) against Squidpy's,
// on the instrument's segmentation of both 2-FOV bundles and the standard simulated tissue, its
// cells labeled by SituCraft's clustering (the defaults) and every one of them analyzed.
// SITUCRAFT_WRITE_REFERENCE=1 writes the cells, the Delaunay and 20 µm graphs and SituCraft's
// results into <data>/spatial/<set>/; validation/reference/spatial/compare.sh runs Squidpy's
// nhood_enrichment on the same graphs (1,000 permutations) and co_occurrence at the same distances,
// and records how they agree; this test, weekly, makes SituCraft's results again, checks they are
// the ones the numbers were made from, and checks the numbers.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"situcraft/internal/cluster"
	"situcraft/internal/segment"
	"situcraft/internal/segment/imports"
	"situcraft/internal/spatial"
	"situcraft/internal/xenium"
)

// spatialNumbers are reference/spatial/<set>.json.
type spatialNumbers struct {
	SituCraft struct {
		Cells  int    `json:"cells"`
		Labels int    `json:"labels"`
		Digest string `json:"digest"`
	} `json:"situcraft"`
	Versions   map[string]string `json:"versions"`
	Enrichment map[string]struct {
		Edges           int     `json:"edges"`
		CountsDiffering int     `json:"countsDiffering"`
		Pairs           int     `json:"pairs"`
		ZBeyond         int     `json:"zBeyond"`
		ZCorrelation    float64 `json:"zCorrelation"`
		ZMaxDifference  float64 `json:"zMaxDifference"`
	} `json:"enrichment"`
	// Co-occurrence: against Squidpy's routine on float64 coordinates, and its float32 API, with the
	// pairs float32's rounding places on the other side of a distance.
	Cooccurrence struct {
		Distances            int     `json:"distances"`
		Compared             int     `json:"compared"`
		MaxDifference        float64 `json:"maxDifference"`
		MaxDifferenceFloat32 float64 `json:"maxDifferenceFloat32"`
		PlacedOtherwise      int     `json:"pairsPlacedOtherwiseInFloat32"`
	} `json:"cooccurrence"`
}

// spatialOfSet labels a set's instrument cells by SituCraft's clustering and computes, over the
// cells analyzed, the enrichment on the Delaunay and 20 µm graphs and the co-occurrence within the
// default distances.
func spatialOfSet(t *testing.T, bundleDir string) (graphs map[string]*spatial.Graph, label []int32, results map[string]*spatial.Enrichment, co *spatial.Cooccurrence) {
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
	cells, err := segment.ReadCells(seg)
	if err != nil {
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
	byRow := make([]int32, len(cells))
	for i := range byRow {
		byRow[i] = -1
	}
	keep := make([]bool, len(cells))
	for k, row := range r.Cells {
		byRow[row] = r.Partition.Community[k]
		keep[row] = true
	}
	in := spatial.Inputs{X: make([]float32, len(cells)), Y: make([]float32, len(cells)), Keep: keep}
	for i, c := range cells {
		in.X[i], in.Y[i] = c.X, c.Y
	}
	K := len(r.Partition.Sizes)
	graphs = map[string]*spatial.Graph{}
	results = map[string]*spatial.Enrichment{}
	for _, p := range []spatial.Params{spatial.DefaultParams(spatial.Delaunay), spatial.DefaultParams(spatial.Radius)} {
		g, err := spatial.Build(ctx, in, p, runtime.NumCPU(), nil)
		if err != nil {
			t.Fatal(err)
		}
		label = make([]int32, len(g.Rows))
		for i, row := range g.Rows {
			label[i] = byRow[row]
		}
		e, err := spatial.Enrich(ctx, g, label, K, nil, spatial.DefaultEnrichment(), runtime.NumCPU(), nil)
		if err != nil {
			t.Fatal(err)
		}
		graphs[p.Kind], results[p.Kind] = g, e
	}
	g := graphs[spatial.Delaunay]
	co, err = spatial.Cooccur(ctx, g.X, g.Y, label, K, spatial.DefaultCooccurrence(), runtime.NumCPU())
	if err != nil {
		t.Fatal(err)
	}
	return graphs, label, results, co
}

// spatialDigest is the SHA-256 of the results' numbers, as JSON.
func spatialDigest(results map[string]*spatial.Enrichment, co *spatial.Cooccurrence) string {
	h := sha256.New()
	for _, kind := range []string{spatial.Delaunay, spatial.Radius} {
		e := results[kind]
		for _, v := range []spatial.Nums{e.Count, e.Expected, e.Mean, e.SD, e.Z, e.P, e.PAdj} {
			raw, _ := json.Marshal(v)
			h.Write(raw)
		}
	}
	for _, r := range co.Ratio {
		raw, _ := json.Marshal(spatial.Nums(r))
		h.Write(raw)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestReferenceSpatial(t *testing.T) {
	if testing.Short() || !writeReference() && os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 (or SITUCRAFT_WRITE_REFERENCE=1) to analyze the reference data sets")
	}
	for _, set := range referenceSets {
		t.Run(set, func(t *testing.T) {
			dir := filepath.Join(dataRoot(), set)
			if set == "simulated-standard" {
				dir = simulatedStandard(t)
			} else if bundle(t, set) == "" {
				return
			}
			graphs, label, results, co := spatialOfSet(t, dir)
			digest := spatialDigest(results, co)
			K := results[spatial.Delaunay].K
			n := len(label)
			if writeReference() {
				out := filepath.Join(dataRoot(), "spatial", set)
				os.RemoveAll(out)
				if err := os.MkdirAll(out, 0o755); err != nil {
					t.Fatal(err)
				}
				g := graphs[spatial.Delaunay]
				var b strings.Builder
				b.WriteString("index,row,x,y,label\n")
				for i := range g.Rows {
					fmt.Fprintf(&b, "%d,%d,%s,%s,%d\n", i, g.Rows[i], strconv.FormatFloat(float64(g.X[i]), 'g', -1, 32), strconv.FormatFloat(float64(g.Y[i]), 'g', -1, 32), label[i])
				}
				if err := os.WriteFile(filepath.Join(out, "cells.csv"), []byte(b.String()), 0o644); err != nil {
					t.Fatal(err)
				}
				enrichment := map[string]any{}
				for kind, g := range graphs {
					var e strings.Builder
					e.WriteString("a,b\n")
					for i := range g.Rows {
						for k := g.Offsets[i]; k < g.Offsets[i+1]; k++ {
							if j := g.Neighbors[k]; j > uint32(i) {
								fmt.Fprintf(&e, "%d,%d\n", i, j)
							}
						}
					}
					if err := os.WriteFile(filepath.Join(out, kind+".csv"), []byte(e.String()), 0o644); err != nil {
						t.Fatal(err)
					}
					r := results[kind]
					enrichment[kind] = map[string]any{"edges": g.Edges(), "count": spatial.Nums(r.Count), "z": spatial.Nums(r.Z), "expected": spatial.Nums(r.Expected), "mean": spatial.Nums(r.Mean)}
				}
				ratios := make([]spatial.Nums, len(co.Ratio))
				for i, r := range co.Ratio {
					ratios[i] = r
				}
				info, _ := json.MarshalIndent(map[string]any{"cells": n, "labels": K, "digest": digest, "permutations": spatial.DefaultEnrichment().Permutations, "enrichment": enrichment, "cooccurrence": map[string]any{"distances": co.Distances, "pairs": co.Pairs, "ratio": ratios}}, "", " ")
				if err := os.WriteFile(filepath.Join(out, "situcraft.json"), append(info, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: %d cells, %d labels; now run validation/reference/spatial/compare.sh", set, n, K)
				return
			}
			raw, err := os.ReadFile(filepath.Join("reference", "spatial", set+".json"))
			if err != nil {
				t.Fatalf("%v (made by SITUCRAFT_WRITE_REFERENCE=1 and compare.sh)", err)
			}
			var num spatialNumbers
			if err := json.Unmarshal(raw, &num); err != nil {
				t.Fatal(err)
			}
			if digest != num.SituCraft.Digest || n != num.SituCraft.Cells || K != num.SituCraft.Labels {
				t.Errorf("SituCraft's results on %s are not those its reference numbers were made from (digest %.16s, recorded %.16s): make them again", set, digest, num.SituCraft.Digest)
			}
			for _, kind := range []string{spatial.Delaunay, spatial.Radius} {
				e := num.Enrichment[kind]
				t.Logf("%s: %d edges; counts differing from Squidpy's %d of %d; z within its permutation error but %d, correlation %.5f, largest difference %.3f", kind, e.Edges, e.CountsDiffering, e.Pairs, e.ZBeyond, e.ZCorrelation, e.ZMaxDifference)
				if e.Edges != graphs[kind].Edges() || e.Pairs != K*K || e.CountsDiffering != 0 || e.ZBeyond > 0 || !(e.ZCorrelation > 0.999) {
					t.Errorf("%s: the enrichment differs from Squidpy's beyond its permutations' error", kind)
				}
			}
			c := num.Cooccurrence
			t.Logf("co-occurrence: %d distances, %d ratios compared; Squidpy's routine in float64, the largest difference %.3g; its float32 API %.3g, %d pairs placed otherwise by float32's rounding", c.Distances, c.Compared, c.MaxDifference, c.MaxDifferenceFloat32, c.PlacedOtherwise)
			if c.Distances != len(co.Distances) || c.Compared == 0 || !(c.MaxDifference < 1e-9) || math.IsNaN(c.MaxDifference) || c.MaxDifferenceFloat32 > 1e-9 && c.PlacedOtherwise == 0 {
				t.Errorf("the co-occurrence differs from Squidpy's beyond float32's rounding")
			}
		})
	}
}
