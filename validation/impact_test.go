package validation

// Labels across segmentations (internal/compare, impact.go; wave 4, slice 7; research.md §5.7): the
// standard simulated tissue segmented three ways — its truth, the instrument's nucleus expansion
// and SituCraft's refinement (Balanced) — each clustered with the defaults and labeled from its
// types' markers; the truth's cells also carry their true types. Each segmentation is compared with
// the truth, and its labels read through the comparison against the true types: the cells matched
// to their true cell labeled with its type, each type's share of the cells, what split and merged
// cells carry, and marker mixing by label (the simulated types' markers as exclusive pairs). The
// instrument's and the refinement's labels are read against each other too, as a user would.

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"situcraft/internal/cluster"
	"situcraft/internal/compare"
	"situcraft/internal/label"
	"situcraft/internal/segment"
	"situcraft/internal/segment/expand"
	"situcraft/internal/segment/imports"
	"situcraft/internal/segment/refine"
	"situcraft/internal/xenium"
)

// labeledSeg is a segmentation of the tissue, its counts, and its cells' labels from markers.
type labeledSeg struct {
	dir    string
	counts *segment.Counts
	labels *compare.SideLabels
}

// labelFromMarkers clusters a segmentation's cells with the defaults and labels them from the
// marker set: each cell's label at the first level, the cells not analyzed left out.
func labelFromMarkers(t *testing.T, dir string, set *label.MarkerSet) labeledSeg {
	t.Helper()
	counts, err := segment.ReadCounts(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := cluster.Run(context.Background(), cluster.Inputs{Counts: counts}, cluster.DefaultParams(), runtime.NumCPU(), nil)
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
	res, err := label.Run(e, set, len(r.Partition.Sizes), r.Target, label.DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	n := len(counts.Offsets) - 1
	s := &compare.SideLabels{Label: make([]int32, n), Covered: make([]bool, n)}
	for _, x := range res.Sets {
		s.Names = append(s.Names, x.Name)
	}
	for i := range s.Label {
		s.Label[i] = -1
	}
	for j, c := range res.Cells {
		s.Label[c] = res.Assignment.At(0, j)
		s.Covered[c] = true
	}
	return labeledSeg{dir: dir, counts: counts, labels: s}
}

// trueTypes are the truth segmentation's cells' types (truth/cells.parquet), as labels.
func trueTypes(t *testing.T, bundleDir, segDir string) *compare.SideLabels {
	t.Helper()
	tab, err := label.ReadTable(filepath.Join(bundleDir, "truth", "cells.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	ids, types := tab.Column("cell_id"), tab.Column("cell_type")
	if ids == nil || types == nil {
		t.Fatal("the truth's cells name no cell_id and cell_type")
	}
	cells, err := segment.ReadCells(segDir)
	if err != nil {
		t.Fatal(err)
	}
	typeOf := map[string]string{}
	for i := 0; i < ids.Len(); i++ {
		typeOf[ids.String(i)] = types.String(i)
	}
	s := &compare.SideLabels{Label: make([]int32, len(cells)), Covered: make([]bool, len(cells))}
	index := map[string]int32{}
	for i, c := range cells {
		ty, ok := typeOf[c.ID]
		s.Label[i] = -1
		if !ok {
			continue
		}
		k, seen := index[ty]
		if !seen {
			k = int32(len(s.Names))
			index[ty] = k
			s.Names = append(s.Names, ty)
		}
		s.Label[i], s.Covered[i] = k, true
	}
	return s
}

// composition distance is the total variation distance between two label sets' shares of the
// cells they label (half the sum of the shares' absolute differences): 0 alike, 1 apart.
func compositionDistance(im *compare.Impact) float64 {
	var na, nb float64
	for _, c := range im.Composition {
		if c.Name != "" {
			na += float64(c.A)
			nb += float64(c.B)
		}
	}
	var d float64
	for _, c := range im.Composition {
		if c.Name != "" {
			d += math.Abs(float64(c.A)/na - float64(c.B)/nb)
		}
	}
	return d / 2
}

func TestLabelsAcrossSegmentations(t *testing.T) {
	if testing.Short() || os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 to label the simulated tissue under three segmentations")
	}
	ctx := context.Background()
	dir := simulatedStandard(t)
	work := t.TempDir()
	b, err := xenium.Open(dir, xenium.Options{CacheDir: work})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ds, err := imports.BundleDataset(b)
	if err != nil {
		t.Fatal(err)
	}
	workers := runtime.NumCPU()
	folders := map[string]string{"truth": filepath.Join(work, "truth"), "instrument": filepath.Join(work, "instrument"), "refinement": filepath.Join(work, "refinement")}
	for _, f := range folders {
		os.MkdirAll(f, 0o755)
	}
	if _, _, _, err := imports.Run(ctx, ds, imports.Options{Format: imports.Xenium, Paths: []string{filepath.Join(dir, "truth")}}, folders["truth"], workers, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := imports.Instrument(ctx, b, folders["instrument"], workers); err != nil {
		t.Fatal(err)
	}
	if _, _, err := refine.Refine(ctx, b, refine.DefaultSpec(), folders["refinement"], refine.Job{Workers: workers}); err != nil {
		t.Fatal(err)
	}
	set := markersFor(t, "simulated-standard", b)
	segs := map[string]labeledSeg{}
	for name, f := range folders {
		segs[name] = labelFromMarkers(t, f, set)
	}
	truthTypes := trueTypes(t, dir, folders["truth"])
	var pairs [][2]string
	for i, l1 := range set.Labels {
		for _, l2 := range set.Labels[i+1:] {
			for _, g1 := range l1.Positive {
				for _, g2 := range l2.Positive {
					pairs = append(pairs, [2]string{g1, g2})
				}
			}
		}
	}
	nuclei, err := expand.NucleusRaster(b)
	if err != nil {
		t.Fatal(err)
	}
	transcripts, err := compare.BundleTranscripts(b)
	if err != nil {
		t.Fatal(err)
	}
	loaded := map[string]*compare.Seg{}
	for name, f := range folders {
		if loaded[name], err = compare.Load(f); err != nil {
			t.Fatal(err)
		}
	}
	groupsOf := func(a, b string) []compare.Group {
		res, err := compare.Compare(ctx, compare.Inputs{A: loaded[a], B: loaded[b], Nuclei: nuclei, Transcripts: transcripts, Params: compare.DefaultParams()})
		if err != nil {
			t.Fatal(err)
		}
		return res.Groups
	}
	// The labeling alone: the truth's cells labeled from markers against their true types.
	self := groupsOf("truth", "truth")
	im, err := compare.CompareLabels(self, truthTypes, segs["truth"].labels, segs["truth"].counts, segs["truth"].counts, pairs)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("truth: labeled from markers, %.4f of %d cells their true type; composition distance %.4f; MECR of the true types %.4f", float64(im.Matched.Agreement), im.Matched.Labeled, compositionDistance(im), float64(im.Mixing.AllA.MECR))
	results := map[string]*compare.Impact{"truth": im}
	for _, name := range []string{"instrument", "refinement"} {
		im, err := compare.CompareLabels(groupsOf("truth", name), truthTypes, segs[name].labels, segs["truth"].counts, segs[name].counts, pairs)
		if err != nil {
			t.Fatal(err)
		}
		results[name] = im
		t.Logf("%s: of %d cells matched to their true cell, %.4f labeled with its type; composition distance %.4f; split %+v, merged %+v; MECR %.4f (the truth's %.4f), cells mixing %.4f",
			name, im.Matched.Labeled, float64(im.Matched.Agreement), compositionDistance(im), im.Split, im.Merged, float64(im.Mixing.AllB.MECR), float64(im.Mixing.AllA.MECR), float64(im.Mixing.AllB.Share))
		for _, m := range im.Mixing.B {
			t.Logf("  %s: MECR %.4f over %d cells", m.Name, float64(m.MECR), m.Cells)
		}
	}
	between, err := compare.CompareLabels(groupsOf("instrument", "refinement"), segs["instrument"].labels, segs["refinement"].labels, segs["instrument"].counts, segs["refinement"].counts, pairs)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("instrument against refinement: %.4f of %d matched cells alike; %d and %d labeled cells change label; split %+v, merged %+v", float64(between.Matched.Agreement), between.Matched.Labeled, between.Sensitive[0], between.Sensitive[1], between.Split, between.Merged)
	for _, c := range refChecksAcross(results, between) {
		if c.Pass {
			t.Logf("met: %s: %.6g (%s %.6g)", c.Name, c.Value, c.Tolerance, c.Limit)
		} else {
			t.Errorf("not met: %s: %.6g (%s %.6g)", c.Name, c.Value, c.Tolerance, c.Limit)
		}
	}
}

// The tolerances (research.md §5.7): the labeling alone takes every true type; each segmentation's
// matched cells take their true cell's type, and its labels its types' shares, within a margin of
// what was measured; the refinement's labels at least as true as the instrument's; marker mixing
// least under the truth, then the refinement, then the instrument; and the two segmentations'
// labels read against each other alike on most matched cells.
const (
	tolAcrossTruth      = 0.995
	tolAcrossInstrument = 0.97
	tolAcrossRefinement = 0.99
	tolAcrossCompInst   = 0.04
	tolAcrossCompRefine = 0.015
	tolAcrossBetween    = 0.96
)

func refChecksAcross(r map[string]*compare.Impact, between *compare.Impact) []check {
	agreement := func(name string) float64 { return float64(r[name].Matched.Agreement) }
	mecr := func(name string) float64 { return float64(r[name].Mixing.AllB.MECR) }
	truthMECR := float64(r["truth"].Mixing.AllA.MECR)
	return []check{
		{Name: "the truth's cells labeled from markers with their true type", Tolerance: "≥", Value: agreement("truth"), Limit: tolAcrossTruth, Pass: agreement("truth") >= tolAcrossTruth},
		{Name: "the instrument's cells matched to their true cell labeled with its type", Tolerance: "≥", Value: agreement("instrument"), Limit: tolAcrossInstrument, Pass: agreement("instrument") >= tolAcrossInstrument},
		{Name: "the refinement's cells matched to their true cell labeled with its type", Tolerance: "≥", Value: agreement("refinement"), Limit: tolAcrossRefinement, Pass: agreement("refinement") >= tolAcrossRefinement},
		{Name: "the refinement's labels as true as the instrument's, or more (difference)", Tolerance: "≥", Value: agreement("refinement") - agreement("instrument"), Limit: 0, Pass: agreement("refinement") >= agreement("instrument")},
		{Name: "the instrument's types' shares against the truth's (total variation distance)", Tolerance: "≤", Value: compositionDistance(r["instrument"]), Limit: tolAcrossCompInst, Pass: compositionDistance(r["instrument"]) <= tolAcrossCompInst},
		{Name: "the refinement's types' shares against the truth's (total variation distance)", Tolerance: "≤", Value: compositionDistance(r["refinement"]), Limit: tolAcrossCompRefine, Pass: compositionDistance(r["refinement"]) <= tolAcrossCompRefine},
		{Name: "marker mixing (MECR) under the refinement, above the truth's and below the instrument's", Tolerance: "<", Value: mecr("refinement"), Limit: mecr("instrument"), Pass: truthMECR < mecr("refinement") && mecr("refinement") < mecr("instrument")},
		{Name: "the instrument's and the refinement's matched cells labeled alike", Tolerance: "≥", Value: float64(between.Matched.Agreement), Limit: tolAcrossBetween, Pass: float64(between.Matched.Agreement) >= tolAcrossBetween},
	}
}
