package validation

// Neighborhood enrichment and co-occurrence (wave 6, slice 2; research.md §6.2) against Squidpy's,
// nearest distances and Ripley's K (slice 3; §6.3) against SciPy's k-d tree and astropy's
// estimator, regions compared and a profile across a region's edge (slice 4; §6.4) against
// Shapely's buffers and distances, niches (slice 5; §6.5) against scikit-learn's k-means, and each
// gene's Moran's I (slice 6; §6.6) against Squidpy's spatial_autocorr, on the instrument's
// segmentation of both 2-FOV bundles and the
// standard simulated tissue, its cells labeled by SituCraft's clustering (the defaults) and every
// one of them analyzed; Ripley's K, and the regions, in the rectangle of the middle half of the
// cells' extent as the window.
// SITUCRAFT_WRITE_REFERENCE=1 writes the cells, the Delaunay and 20 µm graphs and SituCraft's
// results into <data>/spatial/<set>/; validation/reference/spatial/compare.sh runs Squidpy's
// nhood_enrichment on the same graphs (1,000 permutations) and co_occurrence at the same distances,
// and records how they agree; this test, weekly, makes SituCraft's results again, checks they are
// the ones the numbers were made from, and checks the numbers.

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
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
	"situcraft/internal/label"
	"situcraft/internal/segment"
	"situcraft/internal/segment/imports"
	"situcraft/internal/simulate"
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
	// Nearest distances: each pair's median and mean against SciPy's k-d tree (µm).
	Nearest struct {
		Pairs         int     `json:"pairs"`
		MaxDifference float64 `json:"maxDifference"`
	} `json:"nearest"`
	// Ripley's K of each label in the rectangle: without a correction against astropy's "none"
	// (relative difference), with the isotropic correction against its "ripley" (which weighs a
	// pair by its first cell's circle alone, both ways).
	Ripley struct {
		Labels          int     `json:"labels"`
		MaxNone         float64 `json:"maxRelativeNone"`
		MaxExact        float64 `json:"maxRelativeExact"`
		MaxIsotropic    float64 `json:"maxRelativeIsotropic"`
		MedianIsotropic float64 `json:"medianRelativeIsotropic"`
	} `json:"ripley"`
	// Regions compared and a profile across an edge in the rectangle: the areas against Shapely's
	// (relative difference), the cells counted in each row and band against its.
	Regions struct {
		Rows            int     `json:"rows"`
		Bands           int     `json:"bands"`
		MaxArea         float64 `json:"maxRelativeArea"`
		MaxBandArea     float64 `json:"maxRelativeBandArea"`
		MaxBandAreaDiff float64 `json:"maxBandAreaDifference"`
		CountsDiffering int     `json:"countsDiffering"`
		BandsDiffering  int     `json:"bandCountsDiffering"`
	} `json:"regions"`
	// Niches: SituCraft's k-means of the cells' neighborhoods against scikit-learn's, from as many
	// starts, on the same compositions: the inertias, and the niches' adjusted Rand index.
	Niches struct {
		Niches          int     `json:"niches"`
		Cells           int     `json:"cells"`
		Inertia         float64 `json:"inertia"`
		SklearnInertia  float64 `json:"sklearnInertia"`
		RelativeInertia float64 `json:"relativeInertia"`
		AdjustedRand    float64 `json:"adjustedRand"`
	} `json:"niches"`
	// Moran's I of each gene over the Delaunay graph, row-standardized: I and the variance under
	// normality against Squidpy's, and both variances against an independent computation.
	Moran struct {
		Genes         int     `json:"genes"`
		MaxI          float64 `json:"maxDifferenceI"`
		MaxVarNorm    float64 `json:"maxRelativeVarNorm"`
		MaxExactI     float64 `json:"maxDifferenceExactI"`
		MaxExactVarRd float64 `json:"maxRelativeExactVarRand"`
	} `json:"moran"`
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
// spatialExtras are a set's nearest distances and Ripley's K in the middle rectangle.
type spatialExtras struct {
	nearest     *spatial.Nearest
	rectangle   [4]float64
	isotropic   *spatial.Ripley
	none        *spatial.Ripley
	polygons    [2][][2]float64
	composition *spatial.Composition
	boundary    *spatial.Boundary
	niches      *spatial.Niches
	moran       *spatial.Moran
	moranValues []float32
	moranGenes  []string
}

// referenceRegions are two regions in the rectangle (x0, y0, x1, y1): an irregular heptagon about
// its middle, and a box over the heptagon's right side reaching past the rectangle's edge.
func referenceRegions(r [4]float64) [2][][2]float64 {
	cx, cy := (r[0]+r[2])/2, (r[1]+r[3])/2
	s := math.Min(r[2]-r[0], r[3]-r[1]) / 2
	var hept [][2]float64
	for k := 0; k < 7; k++ {
		a := 2 * math.Pi * float64(k) / 7
		rad := s * (0.35 + 0.1*float64(k%3))
		hept = append(hept, [2]float64{cx + rad*math.Cos(a), cy + rad*math.Sin(a)})
	}
	box := [][2]float64{{cx, cy - s/2}, {r[2] + 20, cy - s/2}, {r[2] + 20, cy + s/3}, {cx, cy + s/3}}
	return [2][][2]float64{hept, box}
}

func spatialOfSet(t *testing.T, bundleDir string) (graphs map[string]*spatial.Graph, label []int32, results map[string]*spatial.Enrichment, co *spatial.Cooccurrence, extra spatialExtras) {
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
	np := spatial.DefaultNearest()
	np.Permutations = 19
	if extra.nearest, err = spatial.NearestDistances(ctx, g.X, g.Y, label, K, np, runtime.NumCPU(), nil); err != nil {
		t.Fatal(err)
	}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i := range g.X {
		x0, x1 = math.Min(x0, float64(g.X[i])), math.Max(x1, float64(g.X[i]))
		y0, y1 = math.Min(y0, float64(g.Y[i])), math.Max(y1, float64(g.Y[i]))
	}
	w, h := x1-x0, y1-y0
	extra.rectangle = [4]float64{x0 + w/4, y0 + h/4, x1 - w/4, y1 - h/4}
	rx0, ry0, rx1, ry1 := extra.rectangle[0], extra.rectangle[1], extra.rectangle[2], extra.rectangle[3]
	win, err := spatial.PolygonWindow([][][][2]float64{{{{rx0, ry0}, {rx1, ry0}, {rx1, ry1}, {rx0, ry1}}}})
	if err != nil {
		t.Fatal(err)
	}
	rp := spatial.RipleyParams{Radii: []float64{10, 20, 30, 40, 50}, Correction: "isotropic", Permutations: 19, Seed: 1}
	if extra.isotropic, err = spatial.Ripleys(ctx, g.X, g.Y, label, K, win, rp, runtime.NumCPU(), nil); err != nil {
		t.Fatal(err)
	}
	rp.Correction = "none"
	if extra.none, err = spatial.Ripleys(ctx, g.X, g.Y, label, K, win, rp, runtime.NumCPU(), nil); err != nil {
		t.Fatal(err)
	}
	extra.polygons = referenceRegions(extra.rectangle)
	var regions []*spatial.Window
	for _, poly := range extra.polygons {
		w, err := spatial.PolygonWindow([][][][2]float64{{poly}})
		if err != nil {
			t.Fatal(err)
		}
		regions = append(regions, w)
	}
	if extra.composition, err = spatial.Compose(ctx, g.X, g.Y, label, K, win, regions, spatial.DefaultRegions(), runtime.NumCPU()); err != nil {
		t.Fatal(err)
	}
	if extra.boundary, err = spatial.AcrossEdge(ctx, g.X, g.Y, label, K, win, regions[0], spatial.DefaultBoundary(), runtime.NumCPU()); err != nil {
		t.Fatal(err)
	}
	expr, _, _, err := spatial.NormalizedExpression(counts, g.Rows)
	if err != nil {
		t.Fatal(err)
	}
	extra.moranGenes = counts.Genes
	extra.moranValues = expr.Dense()
	if extra.moran, err = spatial.MoransI(ctx, g, expr, spatial.MoranParams{Weights: "row"}, runtime.NumCPU(), nil); err != nil {
		t.Fatal(err)
	}
	nicheParams := spatial.DefaultNiches()
	nicheParams.Seeds = 0
	if extra.niches, err = spatial.FindNiches(ctx, g.X, g.Y, label, K, nicheParams, runtime.NumCPU(), nil); err != nil {
		t.Fatal(err)
	}
	return graphs, label, results, co, extra
}

// spatialDigest is the SHA-256 of the results' numbers, as JSON.
func spatialDigest(results map[string]*spatial.Enrichment, co *spatial.Cooccurrence, extra spatialExtras) string {
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
	for _, v := range []spatial.Nums{extra.nearest.Median, extra.nearest.Mean, extra.isotropic.KObs, extra.none.KObs, extra.composition.Area, extra.boundary.Area, extra.boundary.Density} {
		raw, _ := json.Marshal(v)
		h.Write(raw)
	}
	for _, v := range [][]int{extra.composition.Count, extra.boundary.Count, extra.niches.Cells} {
		raw, _ := json.Marshal(v)
		h.Write(raw)
	}
	raw, _ := json.Marshal(map[string]any{"assign": extra.niches.Assign, "inertia": extra.niches.Inertia, "composition": spatial.Nums(extra.niches.Composition)})
	h.Write(raw)
	raw, _ = json.Marshal(map[string]any{"i": spatial.Nums(extra.moran.I), "varNorm": spatial.Nums(extra.moran.VarNorm), "varRand": spatial.Nums(extra.moran.VarRand)})
	h.Write(raw)
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
			graphs, label, results, co, extra := spatialOfSet(t, dir)
			digest := spatialDigest(results, co, extra)
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
				// Ripley's K of each label alone (its diagonal), at each radius.
				R := len(extra.isotropic.Radii)
				iso, none := make([]spatial.Nums, K), make([]spatial.Nums, K)
				for a := 0; a < K; a++ {
					q := (a*K + a) * R
					iso[a], none[a] = extra.isotropic.KObs[q:q+R], extra.none.KObs[q:q+R]
				}
				info, _ := json.MarshalIndent(map[string]any{"cells": n, "labels": K, "digest": digest, "permutations": spatial.DefaultEnrichment().Permutations, "enrichment": enrichment, "cooccurrence": map[string]any{"distances": co.Distances, "pairs": co.Pairs, "ratio": ratios},
					"nearest": map[string]any{"reach": extra.nearest.Reach, "median": spatial.Nums(extra.nearest.Median), "mean": spatial.Nums(extra.nearest.Mean)},
					"ripley":  map[string]any{"rectangle": extra.rectangle, "radii": extra.isotropic.Radii, "area": extra.isotropic.Area, "isotropic": iso, "none": none},
					"regions": map[string]any{"rectangle": extra.rectangle, "polygons": extra.polygons, "area": extra.composition.Area, "count": extra.composition.Count, "outside": extra.composition.Outside,
						"edges": extra.boundary.Edges, "bandArea": extra.boundary.Area, "bandCount": extra.boundary.Count},
					"niches": map[string]any{"niches": extra.niches.Count, "starts": spatial.DefaultNiches().Starts, "inertia": extra.niches.Inertia, "assign": extra.niches.Assign},
					"moran":  map[string]any{"genes": extra.moranGenes, "i": spatial.Nums(extra.moran.I), "varNorm": spatial.Nums(extra.moran.VarNorm), "varRand": spatial.Nums(extra.moran.VarRand), "expected": extra.moran.Expected}}, "", " ")
				// The niches' windows: each cell's composition, a row a cell, in the cells' order.
				var nb strings.Builder
				for k := 0; k < K; k++ {
					if k > 0 {
						nb.WriteByte(',')
					}
					fmt.Fprintf(&nb, "label%d", k)
				}
				nb.WriteByte('\n')
				for i := 0; i < n; i++ {
					for k := 0; k < K; k++ {
						if k > 0 {
							nb.WriteByte(',')
						}
						nb.WriteString(strconv.FormatFloat(extra.niches.Composed[i*K+k], 'g', -1, 64))
					}
					nb.WriteByte('\n')
				}
				if err := os.WriteFile(filepath.Join(out, "niches.csv"), []byte(nb.String()), 0o644); err != nil {
					t.Fatal(err)
				}
				// Moran's I's values: genes × cells, float32, little-endian.
				mv := make([]byte, 4*len(extra.moranValues))
				for i, v := range extra.moranValues {
					binary.LittleEndian.PutUint32(mv[4*i:], math.Float32bits(v))
				}
				if err := os.WriteFile(filepath.Join(out, "moran.f32"), mv, 0o644); err != nil {
					t.Fatal(err)
				}
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
			nd, rk := num.Nearest, num.Ripley
			t.Logf("nearest distances: %d pairs, the largest difference from SciPy's %.2g µm; Ripley's K of %d labels: without a correction within %.2g of astropy's; isotropic within %.2g of the same estimator by Ripley's formula, and %.3f of astropy's (median %.4f)", nd.Pairs, nd.MaxDifference, rk.Labels, rk.MaxNone, rk.MaxExact, rk.MaxIsotropic, rk.MedianIsotropic)
			if nd.Pairs != K*K || !(nd.MaxDifference < 1e-4) || rk.Labels == 0 || !(rk.MaxNone < 1e-9) || !(rk.MaxExact < 1e-6) || !(rk.MedianIsotropic < 0.02) {
				t.Errorf("nearest distances or Ripley's K differ from the references beyond their conventions")
			}
			rg := num.Regions
			t.Logf("regions compared: %d rows, the tissue's areas within %.2g of Shapely's, %d counts differing; across an edge: %d bands, their areas within %.2g (at most %.2g µm² apart), %d counts differing", rg.Rows, rg.MaxArea, rg.CountsDiffering, rg.Bands, rg.MaxBandArea, rg.MaxBandAreaDiff, rg.BandsDiffering)
			if rg.Rows != 3 || rg.Bands != len(extra.boundary.Area) || !(rg.MaxArea < 1e-6) || !(rg.MaxBandArea < 2e-5) || !(rg.MaxBandAreaDiff < 0.05) || rg.CountsDiffering != 0 || rg.BandsDiffering != 0 {
				t.Errorf("regions compared or the profile across an edge differ from Shapely's areas and counts")
			}
			mo := num.Moran
			t.Logf("Moran's I of %d genes: Squidpy's within %.2g, its variance under normality within %.2g; an independent computation's I within %.2g and variance under randomization within %.2g", mo.Genes, mo.MaxI, mo.MaxVarNorm, mo.MaxExactI, mo.MaxExactVarRd)
			if mo.Genes != len(extra.moranGenes) || !(mo.MaxI < 1e-9) || !(mo.MaxVarNorm < 1e-9) || !(mo.MaxExactI < 1e-9) || !(mo.MaxExactVarRd < 1e-9) {
				t.Errorf("Moran's I differs from Squidpy's or from the independent computation")
			}
			nn := num.Niches
			t.Logf("niches: %d of %d cells; SituCraft's inertia %.4f, scikit-learn's %.4f (%+.2g), their niches' adjusted Rand index %.3f", nn.Niches, nn.Cells, nn.Inertia, nn.SklearnInertia, nn.RelativeInertia, nn.AdjustedRand)
			if nn.Niches != extra.niches.Count || nn.Cells != n || math.Abs(nn.Inertia-extra.niches.Inertia) > 1e-9*nn.Inertia || !(nn.RelativeInertia < 0.01) || !(nn.AdjustedRand > 0.8) {
				t.Errorf("the niches' k-means is worse than scikit-learn's, or finds other niches")
			}
			c := num.Cooccurrence
			t.Logf("co-occurrence: %d distances, %d ratios compared; Squidpy's routine in float64, the largest difference %.3g; its float32 API %.3g, %d pairs placed otherwise by float32's rounding", c.Distances, c.Compared, c.MaxDifference, c.MaxDifferenceFloat32, c.PlacedOtherwise)
			if c.Distances != len(co.Distances) || c.Compared == 0 || !(c.MaxDifference < 1e-9) || math.IsNaN(c.MaxDifference) || c.MaxDifferenceFloat32 > 1e-9 && c.PlacedOtherwise == 0 {
				t.Errorf("the co-occurrence differs from Squidpy's beyond float32's rounding")
			}
		})
	}
}

// The standard simulated tissue's domains as regions (wave 6, slice 4; research.md §6.4): its true
// cells, labeled by their true types, compared across the domains the simulator laid out (each the
// Voronoi cell of its seeded center), and profiled across each domain's edge, in the whole field.
// Each domain's dominant type has the largest share of its cells and is denser there than in the
// other domains, its interval clear of 1; across the domain's edge it fills the domain, unless a
// domain of the same type lies within the bands' reach (then what is around is much the same).
// Each domain's cells are counted again by every edge of its polygon.
func TestDomainsAsRegions(t *testing.T) {
	if testing.Short() || os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 to analyze the standard simulated tissue")
	}
	dir := simulatedStandard(t)
	b, err := xenium.Open(dir, xenium.Options{CacheDir: t.TempDir()})
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
	p := f.Parameters
	domains, err := simulate.Domains(p)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := label.ReadTable(filepath.Join(dir, "truth", "cells.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	types, xs, ys := tab.Column("cell_type"), tab.Column("x_centroid"), tab.Column("y_centroid")
	if types == nil || xs == nil || ys == nil {
		t.Fatal("the truth's cells name no type or centroid")
	}
	names := make([]string, len(p.Types))
	index := map[string]int32{}
	for k, ty := range p.Types {
		names[k] = ty.Name
		index[ty.Name] = int32(k)
	}
	n := types.Len()
	x, y, lab := make([]float32, n), make([]float32, n), make([]int32, n)
	for i := 0; i < n; i++ {
		k, ok := index[types.String(i)]
		if !ok {
			t.Fatalf("a true cell of type %q, not one of the simulation's", types.String(i))
		}
		x[i], y[i], lab[i] = float32(xs.Numbers[i]), float32(ys.Numbers[i]), k
	}
	field, err := spatial.PolygonWindow([][][][2]float64{{{{0, 0}, {p.WidthUm, 0}, {p.WidthUm, p.HeightUm}, {0, p.HeightUm}}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	K := len(names)
	for d, dm := range domains {
		region, err := spatial.PolygonWindow([][][][2]float64{{dm.Polygon}})
		if err != nil {
			t.Fatal(err)
		}
		c, err := spatial.Compose(ctx, x, y, lab, K, field, []*spatial.Window{region}, spatial.DefaultRegions(), runtime.NumCPU())
		if err != nil {
			t.Fatal(err)
		}
		// The domain's cells by every edge of its polygon.
		count := make([]int, K)
		for i := range x {
			in := false
			for j, q := range dm.Polygon {
				r := dm.Polygon[(j+1)%len(dm.Polygon)]
				if (q[1] <= float64(y[i])) != (r[1] <= float64(y[i])) && q[0]+(float64(y[i])-q[1])/(r[1]-q[1])*(r[0]-q[0]) > float64(x[i]) {
					in = !in
				}
			}
			if in {
				count[lab[i]]++
			}
		}
		for k := range count {
			if count[k] != c.Count[k] {
				t.Errorf("domain %d, %s: %d cells, by its polygon's edges %d", d, names[k], c.Count[k], count[k])
			}
		}
		k := dm.Dominant
		q := (0*c.R+1)*K + k
		largest := true
		for j := 0; j < K; j++ {
			if c.Share[j] > c.Share[k] {
				largest = false
			}
		}
		e, err := spatial.AcrossEdge(ctx, x, y, lab, K, field, region, spatial.DefaultBoundary(), runtime.NumCPU())
		if err != nil {
			t.Fatal(err)
		}
		alike := ""
		for o, other := range domains {
			if o != d && other.Dominant == k && polygonGap(dm.Polygon, other.Polygon) <= spatial.DefaultBoundary().Reach {
				alike = fmt.Sprintf("; domain %d, also %s, lies within the reach", o, names[k])
			}
		}
		t.Logf("domain %d (%s dominant, %.0f%% of its mix): %.0f%% of its %d cells; %.2f× (%.2f to %.2f) as dense as in the other domains; the core over around %.2f×%s", d, names[k], 100*dm.Mix[k], 100*c.Share[k], c.Labeled[0], c.DensityRatio[q], c.DensityRatioLo[q], c.DensityRatioHi[q], e.Ratio[k*spatial.Comparisons+spatial.CoreOverAround], alike)
		if !largest || !(c.DensityRatioLo[q] > 1) || alike == "" && e.Inside[k] != 1 {
			t.Errorf("domain %d: its dominant type %s is not found filling it", d, names[k])
		}
	}
}

// polygonGap is the least distance between two polygons' outlines (0 where they touch).
func polygonGap(a, b [][2]float64) float64 {
	best := math.Inf(1)
	for _, pair := range [][2][][2]float64{{a, b}, {b, a}} {
		for _, p := range pair[0] {
			for j, q := range pair[1] {
				r := pair[1][(j+1)%len(pair[1])]
				dx, dy := r[0]-q[0], r[1]-q[1]
				u := math.Max(0, math.Min(1, ((p[0]-q[0])*dx+(p[1]-q[1])*dy)/(dx*dx+dy*dy)))
				best = math.Min(best, math.Hypot(p[0]-q[0]-u*dx, p[1]-q[1]-u*dy))
			}
		}
	}
	return best
}

// Niches against the standard simulated tissue's true domains (wave 6, slice 5; research.md §6.5):
// its true cells, labeled by their true types, each with the domain the simulator drew its type
// from (truth/cells.parquet). Domains led by the same type differ only in their minor types, so
// what composition can tell apart are the kinds of domain, by their dominant type: niches as many
// as the kinds, from each cell's 30 nearest cells, find them (adjusted Rand index above 0.85), every
// seed agreeing; niches as many as the domains, from the default 10 nearest, split kinds of domain
// by chance, and the seeds say so, agreeing less.
func TestNichesFindDomains(t *testing.T) {
	if testing.Short() || os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 to analyze the standard simulated tissue")
	}
	dir := simulatedStandard(t)
	tab, err := label.ReadTable(filepath.Join(dir, "truth", "cells.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	types, xs, ys, domains := tab.Column("cell_type"), tab.Column("x_centroid"), tab.Column("y_centroid"), tab.Column("domain")
	if types == nil || xs == nil || ys == nil || domains == nil {
		t.Fatal("the truth's cells name no type, centroid or domain")
	}
	ds, err := simulate.Domains(simulate.Presets["standard"])
	if err != nil {
		t.Fatal(err)
	}
	index := map[string]int32{}
	n := types.Len()
	x, y, lab, domain, kind := make([]float32, n), make([]float32, n), make([]int32, n), make([]int32, n), make([]int32, n)
	kinds := map[int]bool{}
	for i := 0; i < n; i++ {
		k, ok := index[types.String(i)]
		if !ok {
			k = int32(len(index))
			index[types.String(i)] = k
		}
		x[i], y[i], lab[i], domain[i] = float32(xs.Numbers[i]), float32(ys.Numbers[i]), k, int32(domains.Numbers[i])
		kind[i] = int32(ds[domain[i]].Dominant)
		kinds[ds[domain[i]].Dominant] = true
	}
	find := func(neighbors, niches int) *spatial.Niches {
		p := spatial.DefaultNiches()
		p.Neighbors, p.Niches = neighbors, niches
		nc, err := spatial.FindNiches(context.Background(), x, y, lab, len(index), p, runtime.NumCPU(), nil)
		if err != nil {
			t.Fatal(err)
		}
		return nc
	}
	least := func(v []float64) float64 {
		lo := 1.0
		for _, a := range v {
			lo = math.Min(lo, a)
		}
		return lo
	}
	byKind := find(30, len(kinds))
	byDomain := find(10, len(ds))
	t.Logf("%d true cells, %d domains of %d kinds: %d niches of each cell's 30 nearest find the kinds at an adjusted Rand index of %.3f (the domains %.3f), the seeds agreeing at %.3f or more; %d niches of the 10 nearest find the domains at %.3f, the seeds agreeing at %.3f or more",
		n, len(ds), len(kinds), len(kinds), cluster.AdjustedRand(byKind.Assign, kind), cluster.AdjustedRand(byKind.Assign, domain), least(byKind.Agreement), len(ds), cluster.AdjustedRand(byDomain.Assign, domain), least(byDomain.Agreement))
	if !(cluster.AdjustedRand(byKind.Assign, kind) > 0.85) || !(least(byKind.Agreement) > 0.95) || !(least(byDomain.Agreement) < least(byKind.Agreement)) {
		t.Errorf("the niches do not find the kinds of domain, or the seeds' agreement does not show too many niches")
	}
}

// Spatially variable genes in the standard simulated tissue (wave 6, slice 6; research.md §6.6):
// over the Delaunay graph of the instrument's cells, the genes marking a cell type, whose cells
// gather in the domains that type leads, against the genes no type marks (each type's share of
// them alike): every marker's Moran's I above every unmarked gene's, and every marker in a pattern
// (adjusted p below 0.05).
func TestMoranFindsDomainGenes(t *testing.T) {
	if testing.Short() || os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 to analyze the standard simulated tissue")
	}
	ctx := context.Background()
	dir := simulatedStandard(t)
	b, err := xenium.Open(dir, xenium.Options{CacheDir: t.TempDir()})
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
	in := spatial.Inputs{X: make([]float32, len(cells)), Y: make([]float32, len(cells))}
	for i, c := range cells {
		in.X[i], in.Y[i] = c.X, c.Y
	}
	g, err := spatial.Build(ctx, in, spatial.DefaultParams(spatial.Delaunay), runtime.NumCPU(), nil)
	if err != nil {
		t.Fatal(err)
	}
	expr, _, _, err := spatial.NormalizedExpression(counts, g.Rows)
	if err != nil {
		t.Fatal(err)
	}
	m, err := spatial.MoransI(ctx, g, expr, spatial.MoranParams{Weights: "row"}, runtime.NumCPU(), nil)
	if err != nil {
		t.Fatal(err)
	}
	marker := map[string]bool{}
	for _, names := range simulate.MarkerNames(simulate.Presets["standard"]) {
		for _, name := range names {
			marker[name] = true
		}
	}
	lowMarker, highOther := math.Inf(1), math.Inf(-1)
	markers, others, patterned := 0, 0, 0
	for gene, name := range counts.Genes {
		if marker[name] {
			markers++
			lowMarker = math.Min(lowMarker, m.I[gene])
			if m.PAdj[gene] < 0.05 && m.I[gene] > m.Expected {
				patterned++
			}
		} else {
			others++
			highOther = math.Max(highOther, m.I[gene])
		}
	}
	t.Logf("%d marker genes, Moran's I %.3f or more, %d of them in a pattern; %d genes no type marks, I %.3f at most", markers, lowMarker, patterned, others, highOther)
	if markers == 0 || others == 0 || !(lowMarker > highOther) || patterned != markers {
		t.Errorf("the markers' Moran's I does not stand above the unmarked genes'")
	}
}
