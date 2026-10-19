package validation

// A cohort at scale (wave 7, slice 8; research.md §6.14): 24 simulated sections, the standard
// cohort's two groups of 12 subjects each, each section a mosaic of 3 by 3 fields (2.6 mm a side,
// about 84,000 cells): about 2 million cells in all, their true cells alone (simulate.Cells: no
// images or transcripts; their true types the labels), each section kept in its folder as a
// segmentation keeps its cells and counts. Then, in a process of its own (this test binary again,
// TestCohortScaleStep), the cohort summarized and compared as SituCraft's jobs do it, one section
// at a time: each section read from its folder; its Delaunay graph built, its labels' enrichment,
// their nearest distances and its genes' Moran's I asked with the defaults the page offers; it
// summarized and let go, cohort.Builder keeping only what the summary needs; the niches found
// across the sections together; the summary written and checked; and the groups' abundance,
// expression and spatial statistics compared and written. Its time and peak memory against their
// budgets; and the same of a quarter of the sections, its peak to show that one section is held
// at a time (the peak does not grow with the sections). About 1.5 GB of disk; the weekly workflow
// runs it:
//
//	SITUCRAFT_SCALE=/a/folder go test ./validation/ -run CohortScale -v -timeout 2h

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"situcraft/internal/artifacts"
	"situcraft/internal/cohort"
	"situcraft/internal/segment"
	"situcraft/internal/simulate"
	"situcraft/internal/spatial"
)

// The budgets of a cohort's summary and comparisons on four cores: the 24 sections' time and peak
// resident memory, and the peak's growth from 6 sections to 24 (measured on four cores of a 10-core
// laptop: 251 s and 0.65 GB, 6 sections' peak 0.54 GB; each section's Moran's I most of the time,
// 139 s in all, and its nearest distances 77 s).
const (
	cohortScaleSeconds = 1200
	cohortScalePeak    = 1.5 * gb
	cohortScaleGrowth  = 1.5
)

// scaleCohort is the cohort made: the standard cohort's settings and effects, 12 subjects a group,
// each section 3 by 3 fields of the large preset's.
func scaleCohort() simulate.Cohort {
	c := simulate.CohortPresets["standard"]
	c.Subjects = 12
	field := simulate.Presets["large"]
	p := c.Section
	p.WidthUm, p.HeightUm = field.WidthUm, field.HeightUm
	p.TilesAcross, p.TilesDown = 3, 3
	c.Section = p
	return c
}

type scaleSample struct {
	Name, Subject, Group string
	Area                 float64
}

func TestCohortScale(t *testing.T) {
	root := os.Getenv("SITUCRAFT_SCALE")
	if root == "" {
		t.Skip("set SITUCRAFT_SCALE to a folder with 1.5 GB free to summarize and compare a cohort of 2 million cells")
	}
	if runtime.GOOS == "windows" {
		t.Skip("peak memory is measured on Linux and macOS")
	}
	work := filepath.Join(root, "situcraft-cohort-scale")
	os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
	c := scaleCohort()
	samples, params, err := simulate.CohortSamples(c)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	var list []scaleSample
	cells := 0
	for k, s := range samples {
		tc, err := simulate.Cells(context.Background(), params[k], runtime.NumCPU(), nil)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(work, s.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeScaleCells(dir, tc); err != nil {
			t.Fatal(err)
		}
		if err := segment.WriteCounts(dir, tc.Counts); err != nil {
			t.Fatal(err)
		}
		cells += len(tc.X)
		area := float64(c.Section.TilesAcross*c.Section.TilesDown) * c.Section.WidthUm * c.Section.HeightUm
		list = append(list, scaleSample{s.Name, s.Subject, s.Group, area})
	}
	raw, _ := json.Marshal(list)
	if err := os.WriteFile(filepath.Join(work, "samples.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d sections, %d cells, made in %.0f s", len(list), cells, time.Since(start).Seconds())
	if len(list) != 24 || cells < 1.5e6 {
		t.Fatalf("%d sections of %d cells, not 24 of about 2 million", len(list), cells)
	}
	run := func(sections int) (scaleStep, map[string]float64) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCohortScaleStep$", "-test.count=1", "-test.v", "-test.timeout=2h")
		cmd.Env = append(os.Environ(), "GOMAXPROCS=4", "SITUCRAFT_COHORT_SCALE_INPUT="+work, "SITUCRAFT_COHORT_SCALE_SECTIONS="+strconv.Itoa(sections))
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		at := time.Now()
		if err := cmd.Run(); err != nil {
			t.Fatalf("%d sections: %v\n%s", sections, err, out.String())
		}
		s := scaleStep{fmt.Sprintf("%d sections", sections), time.Since(at).Seconds(), peakResident(cmd.ProcessState)}
		var r map[string]float64
		if i := strings.Index(out.String(), "RESULT "); i >= 0 {
			json.Unmarshal([]byte(strings.TrimSpace(strings.SplitN(out.String()[i+7:], "\n", 2)[0])), &r)
		}
		t.Logf("%s: %.0f s, a peak of %.2f GB; %v", s.Name, s.Seconds, float64(s.Peak)/gb, r)
		return s, r
	}
	quarter, _ := run(6)
	all, r := run(24)
	if all.Seconds > cohortScaleSeconds {
		t.Errorf("24 sections: %.0f s, over the budget of %d s", all.Seconds, cohortScaleSeconds)
	}
	if float64(all.Peak) > cohortScalePeak {
		t.Errorf("24 sections: a peak of %.2f GB, over the budget of %.2f GB", float64(all.Peak)/gb, cohortScalePeak/gb)
	}
	if quarter.Peak > 0 && float64(all.Peak) > cohortScaleGrowth*float64(quarter.Peak) {
		t.Errorf("a peak of %.2f GB for 24 sections and %.2f GB for 6: it grows with the sections", float64(all.Peak)/gb, float64(quarter.Peak)/gb)
	}
	// What the truth says, at 12 subjects a group: the cells all summarized, every label and gene
	// compared, and the effect gene found in its type.
	if r["cells"] != float64(cells) || r["findings"] != 0 || r["sim033"] != 1 || r["labels"] != 4 || r["genes"] != 120 {
		t.Errorf("the summary and comparisons: %v", r)
	}
}

// TestCohortScaleStep is TestCohortScale's summary and comparisons of its first sections, in a
// process of its own; it prints what the parent checks.
func TestCohortScaleStep(t *testing.T) {
	dir := os.Getenv("SITUCRAFT_COHORT_SCALE_INPUT")
	if dir == "" {
		t.Skip("one run of TestCohortScale")
	}
	sections, _ := strconv.Atoi(os.Getenv("SITUCRAFT_COHORT_SCALE_SECTIONS"))
	ctx := context.Background()
	workers := runtime.GOMAXPROCS(0)
	var list []scaleSample
	raw, err := os.ReadFile(filepath.Join(dir, "samples.json"))
	if err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(raw, &list)
	if sections < 4 || sections > len(list) {
		t.Fatalf("%d sections of %d", sections, len(list))
	}
	// The first sections of each group, as many of one as of the other.
	var chosen []scaleSample
	for _, g := range []string{"control", "treated"} {
		n := 0
		for _, s := range list {
			if s.Group == g && n < sections/2 {
				chosen = append(chosen, s)
				n++
			}
		}
	}
	var b *cohort.Builder
	np := spatial.DefaultNiches()
	cells := 0
	timing := map[string]float64{}
	clock := func(what string, at time.Time) { timing[what] += time.Since(at).Seconds() }
	for _, s := range chosen {
		sdir := filepath.Join(dir, s.Name)
		at := time.Now()
		x, y, typ, _, types := readScaleCells(t, sdir)
		counts, err := segment.ReadCounts(sdir)
		if err != nil {
			t.Fatal(err)
		}
		clock("read", at)
		if b == nil {
			b = cohort.NewBuilder(cohort.LabelSet{Name: "true types", Names: types}, &np)
		}
		K := len(types)
		n := len(x)
		cells += n
		// The steps: a Delaunay graph, and the questions with the page's defaults.
		at = time.Now()
		g, err := spatial.Build(ctx, spatial.Inputs{X: x, Y: y}, spatial.DefaultParams("delaunay"), workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		clock("graph", at)
		label := make([]int32, len(g.Rows))
		for i, row := range g.Rows {
			label[i] = typ[row]
		}
		at = time.Now()
		e, err := spatial.Enrich(ctx, g, label, K, nil, spatial.DefaultEnrichment(), workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		clock("enrichment", at)
		at = time.Now()
		near, err := spatial.NearestDistances(ctx, x, y, typ, K, spatial.DefaultNearest(), workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		clock("nearest", at)
		at = time.Now()
		expr, _, _, err := spatial.NormalizedExpression(counts, g.Rows)
		if err != nil {
			t.Fatal(err)
		}
		m, err := spatial.MoransI(ctx, g, expr, spatial.DefaultMoran(), workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		clock("moran", at)
		// Summarized, and let go.
		at = time.Now()
		segCells := make([]segment.Cell, n)
		keep := make([]bool, n)
		rows := make([]uint32, n)
		for i := range segCells {
			segCells[i] = segment.Cell{ID: strconv.Itoa(i + 1), X: x[i], Y: y[i]}
			for j := counts.Offsets[i]; j < counts.Offsets[i+1]; j++ {
				segCells[i].Transcripts += counts.Count[j]
				segCells[i].Genes++
			}
			keep[i], rows[i] = true, uint32(i)
		}
		sum, err := cohort.Summarize(cohort.Input{Cells: segCells, Counts: counts, Keep: keep, Label: typ, Tissue: s.Area}, K)
		if err != nil {
			t.Fatal(err)
		}
		stat := cohort.EnrichmentStatistics("enrichment", types, e.Count, e.Expected)
		stat = append(stat, cohort.NearestStatistics("nearest", types, near.Median)...)
		stat = append(stat, cohort.MoranStatistics("moran", counts.Genes, m.I)...)
		panel := cohort.Panel{Name: "SituCraft simulated panel"}
		for _, gn := range counts.Genes {
			panel.Genes = append(panel.Genes, cohort.PanelGene{Name: gn})
		}
		b.Add(cohort.SampleDoc{Sample: s.Name, Name: s.Name, Dataset: s.Name, PanelGenes: len(counts.Genes), Summary: *sum, Statistics: stat}, sum.Pseudobulk, counts.Genes, panel,
			spatial.NicheCells{X: x, Y: y, Label: typ}, rows)
		clock("summary", at)
	}
	at := time.Now()
	built, err := b.Finish(ctx, workers, nil)
	if err != nil {
		t.Fatal(err)
	}
	clock("niches", at)
	at = time.Now()
	out := filepath.Join(dir, fmt.Sprintf("summary-%d", sections))
	os.RemoveAll(out)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cohort.Write(out, built.Doc, built.Pseudobulk, built.Rows, built.Niches); err != nil {
		t.Fatal(err)
	}
	findings := cohort.Check(out, &artifacts.Manifest{})
	for _, f := range findings {
		t.Logf("finding: %s", f.Message)
	}
	clock("written", at)
	// The comparisons, as their jobs make them.
	at = time.Now()
	doc := built.Doc
	design := cohort.ComparisonDesign{Names: []string{"Intercept", "condition[T.treated]"}, Contrasts: []cohort.Contrast{{Name: "treated − control", Vector: []float64{0, 1}}}}
	for _, s := range chosen {
		x := []float64{1, 0}
		if s.Group == "treated" {
			x[1] = 1
		}
		design.Rows = append(design.Rows, cohort.DesignRow{Unit: s.Subject, Group: s.Group, Samples: []string{s.Name}, X: x})
	}
	ab, err := cohort.Abundance(doc, design, []string{cohort.FamilyShare, cohort.FamilyDensity, cohort.FamilyNiche}, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := cohort.CompareExpression(doc, built.Pseudobulk, design, 0.95, cohort.MinLabelCells)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := cohort.CompareSpatial(doc, design, cohort.SpatialFamiliesOf(doc), 0.95)
	if err != nil {
		t.Fatal(err)
	}
	for k, w := range []func(string) error{
		func(d string) error { return cohort.WriteComparison(d, ab) },
		func(d string) error { return cohort.WriteExpression(d, ex) },
		func(d string) error { return cohort.WriteSpatialComparison(d, sp) },
	} {
		d := filepath.Join(out, strconv.Itoa(k))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := w(d); err != nil {
			t.Fatal(err)
		}
	}
	clock("compared", at)
	sim033 := 0.0
	for _, r := range ex.Results {
		if r.Label != "epithelial-like" || !r.Tested {
			continue
		}
		for i, g := range r.Genes {
			if ex.Genes[g] == simulate.CohortGene && r.Contrasts[0].PAdj[i] < 0.05 && r.Contrasts[0].LogFC[i] > 0 {
				sim033 = 1
			}
		}
	}
	result := map[string]float64{"cells": float64(cells), "sections": float64(len(chosen)), "findings": float64(len(findings)), "labels": float64(len(doc.Labels.Names)), "genes": float64(len(ex.Genes)), "tests": float64(ex.Tests), "sim033": sim033}
	for k, v := range timing {
		result[k] = math.Round(v*10) / 10
	}
	raw, _ = json.Marshal(result)
	fmt.Printf("RESULT %s\n", raw)
}
