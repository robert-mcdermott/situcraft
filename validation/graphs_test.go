package validation

// Cell graphs (wave 6, slice 1; research.md §6.1) against SciPy's and Squidpy's, on the instrument's
// segmentation of both 2-FOV bundles and the standard simulated tissue. SITUCRAFT_WRITE_REFERENCE=1
// writes SituCraft's graphs (each kind with its defaults) and the cells' centroids into
// <data>/graphs/<set>/; validation/reference/graphs/compare.sh builds the same graphs with SciPy
// (Delaunay by Qhull, k nearest and radius by a k-d tree) and Squidpy's spatial_neighbors, and
// records how they agree; this test, weekly, builds SituCraft's again, checks they are the graphs the
// numbers were made from, and checks the numbers.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"situcraft/internal/segment"
	"situcraft/internal/segment/imports"
	"situcraft/internal/spatial"
	"situcraft/internal/xenium"
)

// graphKinds are the graphs compared, with SituCraft's defaults (contact has no reference).
var graphKinds = []string{spatial.Delaunay, spatial.KNN, spatial.Radius}

// graphNumbers are reference/graphs/<set>.json.
type graphNumbers struct {
	SituCraft struct {
		Cells  int               `json:"cells"`
		Digest map[string]string `json:"digest"`
	} `json:"situcraft"`
	Versions map[string]string `json:"versions"`
	Kinds    map[string]struct {
		Params        spatial.Params `json:"params"`
		Edges         int            `json:"edges"`
		Reference     int            `json:"reference"`
		OnlySituCraft int            `json:"onlySituCraft"`
		OnlyReference int            `json:"onlyReference"`
		// Explained: the differences that are the other diagonal of four cells on one circle (the
		// triangulation is not unique there), each checked exactly.
		Explained int `json:"explained"`
		// MaxLength: the largest difference between an edge's length and the reference's (µm).
		MaxLength float64 `json:"maxLength"`
		// Squidpy: the edges Squidpy's spatial_neighbors gives, and how many differ from SciPy's.
		Squidpy          int `json:"squidpy"`
		SquidpyDiffering int `json:"squidpyDiffering"`
	} `json:"kinds"`
}

// graphsOfSet builds each kind's graph of a set's instrument cells, with its defaults.
func graphsOfSet(t *testing.T, bundleDir string) (map[string]*spatial.Graph, []segment.Cell) {
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
	in := spatial.Inputs{X: make([]float32, len(cells)), Y: make([]float32, len(cells))}
	for i, c := range cells {
		in.X[i], in.Y[i] = c.X, c.Y
	}
	out := map[string]*spatial.Graph{}
	for _, kind := range graphKinds {
		g, err := spatial.Build(ctx, in, spatial.DefaultParams(kind), runtime.NumCPU(), nil)
		if err != nil {
			t.Fatal(err)
		}
		out[kind] = g
	}
	return out, cells
}

// graphDigest is the SHA-256 of a graph's files, written.
func graphDigest(t *testing.T, g *spatial.Graph, kind string) string {
	t.Helper()
	dir := t.TempDir()
	p := spatial.DefaultParams(kind)
	if err := spatial.Write(dir, g, spatial.Describe(g, spatial.Side{ID: "instrument", Name: "Instrument segmentation"}, len(g.Rows), p, spatial.Choice{})); err != nil {
		t.Fatal(err)
	}
	h, err := spatial.Digest(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestReferenceGraphs(t *testing.T) {
	if testing.Short() || !writeReference() && os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 (or SITUCRAFT_WRITE_REFERENCE=1) to build the reference data sets' graphs")
	}
	for _, set := range referenceSets {
		t.Run(set, func(t *testing.T) {
			dir := filepath.Join(dataRoot(), set)
			if set == "simulated-standard" {
				dir = simulatedStandard(t)
			} else if bundle(t, set) == "" {
				return
			}
			graphs, cells := graphsOfSet(t, dir)
			digests := map[string]string{}
			for kind, g := range graphs {
				digests[kind] = graphDigest(t, g, kind)
			}
			if writeReference() {
				out := filepath.Join(dataRoot(), "graphs", set)
				os.RemoveAll(out)
				if err := os.MkdirAll(out, 0o755); err != nil {
					t.Fatal(err)
				}
				// The centroids as SituCraft holds them (float32, written to read back the same).
				var b strings.Builder
				b.WriteString("row,x,y\n")
				for i, c := range cells {
					fmt.Fprintf(&b, "%d,%s,%s\n", i, strconv.FormatFloat(float64(c.X), 'g', -1, 32), strconv.FormatFloat(float64(c.Y), 'g', -1, 32))
				}
				if err := os.WriteFile(filepath.Join(out, "cells.csv"), []byte(b.String()), 0o644); err != nil {
					t.Fatal(err)
				}
				for kind, g := range graphs {
					var e strings.Builder
					e.WriteString("a,b,length\n")
					for i := range g.Rows {
						for k := g.Offsets[i]; k < g.Offsets[i+1]; k++ {
							if j := g.Neighbors[k]; j > uint32(i) {
								fmt.Fprintf(&e, "%d,%d,%s\n", g.Rows[i], g.Rows[j], strconv.FormatFloat(float64(g.Length[k]), 'g', -1, 32))
							}
						}
					}
					if err := os.WriteFile(filepath.Join(out, kind+".csv"), []byte(e.String()), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				params := map[string]spatial.Params{}
				for _, kind := range graphKinds {
					params[kind] = spatial.DefaultParams(kind)
				}
				info, _ := json.MarshalIndent(map[string]any{"cells": len(cells), "digest": digests, "params": params}, "", " ")
				if err := os.WriteFile(filepath.Join(out, "situcraft.json"), append(info, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: %d cells; now run validation/reference/graphs/compare.sh", set, len(cells))
				return
			}
			raw, err := os.ReadFile(filepath.Join("reference", "graphs", set+".json"))
			if err != nil {
				t.Fatalf("%v (made by SITUCRAFT_WRITE_REFERENCE=1 and compare.sh)", err)
			}
			var n graphNumbers
			if err := json.Unmarshal(raw, &n); err != nil {
				t.Fatal(err)
			}
			if n.SituCraft.Cells != len(cells) {
				t.Errorf("%d cells, the numbers were made from %d", len(cells), n.SituCraft.Cells)
			}
			for _, kind := range graphKinds {
				if digests[kind] != n.SituCraft.Digest[kind] {
					t.Errorf("SituCraft's %s graph of %s is not the one its reference numbers were made from (digest %.16s, recorded %.16s): make them again", kind, set, digests[kind], n.SituCraft.Digest[kind])
				}
				k, ok := n.Kinds[kind]
				if !ok {
					t.Errorf("no numbers for the %s graph", kind)
					continue
				}
				// Every edge the reference's, but where four cells lie on one circle; lengths alike
				// to float32's rounding; Squidpy's graph SciPy's.
				unexplained := k.OnlySituCraft + k.OnlyReference - k.Explained
				t.Logf("%s: %d edges, SciPy %d; %d only SituCraft's, %d only SciPy's (%d explained); lengths within %.2g µm; Squidpy %d edges, %d differing", kind, k.Edges, k.Reference, k.OnlySituCraft, k.OnlyReference, k.Explained, k.MaxLength, k.Squidpy, k.SquidpyDiffering)
				if k.Edges != graphs[kind].Edges() || unexplained != 0 || k.MaxLength > 1e-4 || k.SquidpyDiffering != 0 {
					t.Errorf("%s: the graphs differ beyond what is explained", kind)
				}
			}
		})
	}
}
