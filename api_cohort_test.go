package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/cohort"
	"situcraft/internal/segment"
	"situcraft/internal/spatial"
	"situcraft/internal/xenium"
)

// A cohort of two samples of a small simulated tissue, the whole section and its left part as a
// region, labeled by turns: summarized by a job (each sample's cells by label, its tissue, its
// pseudobulk, its graph's and enrichment's numbers, niches across both), every number checked
// against the cells, the counts and the other artifacts; asked again, answered at once; what
// cannot be summarized refused.
func TestCohortSummary(t *testing.T) {
	f := newRefineFixture(t, true)
	all, _ := f.a.artifacts.List()
	var seg artifacts.Manifest
	for _, m := range all {
		if m.Kind == "segmentation" && isBundleOwn(m) {
			seg = m
		}
	}
	b, err := xenium.Open(f.folder, xenium.Options{CacheDir: f.a.cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := f.a.segmentationFolder(context.Background(), b, seg)
	b.Close()
	if err != nil {
		t.Fatal(err)
	}
	cells, _ := segment.ReadCells(dir)
	counts, _ := segment.ReadCounts(dir)
	n := len(cells)
	labels := make([]int32, n)
	all1, left := make([]bool, n), make([]bool, n)
	for i := range labels {
		labels[i] = int32(i%3) - 1 // a third without a label
		all1[i] = true
		left[i] = cells[i].X < 40
	}
	codes, sum := codesOf(labels)
	// The sample's labels name B, C (codes 0, 1); the cohort's vocabulary is A, B, C.
	labelsJSON := fmt.Sprintf(`{"id":"lab-x","kind":"labels","name":"By turns","level":0,"names":["B","C"],"codes":%q,"checksum":%q}`, codes, sum)
	var g struct {
		ID  string
		Job *struct{ ID string }
	}
	json.NewDecoder(f.post("/api/local/0/graph", fmt.Sprintf(`{"segmentation":%q,"parameters":{"kind":"delaunay"},"cells":%s}`, seg.ID, chosenCells(all1, ""))).Body).Decode(&g)
	if g.Job != nil {
		f.wait(g.Job.ID)
	}
	var e struct {
		ID  string
		Job *struct{ ID string }
	}
	erec := f.post("/api/local/0/spatial", fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"graph":%q,"labels":%s,"parameters":{"permutations":100,"seed":3}}`, seg.ID, g.ID, labelsJSON))
	etext := erec.Body.String()
	if err := json.Unmarshal([]byte(etext), &e); err != nil || e.ID == "" {
		t.Fatalf("the enrichment: %d %s", erec.Code, etext)
	}
	if e.Job != nil {
		f.wait(e.Job.ID)
	}
	region := `{"kind":"region","region":{"id":"r-1","name":"Left","version":1},"geometry":[[[[0,0],[40,0],[40,60],[0,60]]]]}`
	body := fmt.Sprintf(`{"labels":{"name":"By turns","level":0,"names":["A","B","C"]},"niches":{"neighbors":5,"niches":2,"starts":2,"seeds":1,"seed":4},"samples":[
		{"sample":"s-1","name":"Whole","dataset":"d-1","item":"0","segmentation":%q,"cells":%s,"labels":%s,"graph":%q,"analyses":[%q]},
		{"sample":"s-2","name":"Left","dataset":"d-1","item":"0","segmentation":%q,"cells":%s,"labels":%s,"region":%s}]}`,
		seg.ID, chosenCells(all1, ""), labelsJSON, g.ID, e.ID, seg.ID, chosenCells(left, ""), labelsJSON, region)
	rec := f.post("/api/cohort/summary", body)
	var answer struct {
		ID       string
		Artifact *artifacts.Manifest
		Job      *struct{ ID string }
	}
	text := rec.Body.String()
	if err := json.Unmarshal([]byte(text), &answer); err != nil || answer.ID == "" || answer.Job == nil {
		t.Fatalf("%d: %v %s", rec.Code, err, text)
	}
	f.wait(answer.Job.ID)
	m, err := f.a.artifacts.Get(answer.ID)
	if err != nil || m.State != artifacts.Complete || m.Kind != "cohort-summary" || !strings.HasPrefix(m.ID, "coh-") {
		t.Fatalf("the summary: %v %+v", err, m.Findings)
	}
	p, _ := f.a.artifacts.Path(m.ID, cohort.FileSummary)
	sdir := filepath.Dir(p)
	d, pb, err := cohort.Read(sdir)
	if err != nil {
		t.Fatal(err)
	}
	// Each sample's cells by label, the suite's own; its pseudobulk its labeled cells' transcripts.
	for s, keep := range [][]bool{all1, left} {
		sd := d.Samples[s]
		want := make([]int, 3)
		chosen, labeled := 0, 0
		var transcripts uint64
		for i := range cells {
			if !keep[i] {
				continue
			}
			chosen++
			if labels[i] >= 0 {
				labeled++
				want[labels[i]+1]++
				transcripts += uint64(cells[i].Transcripts)
			}
		}
		if sd.Cells != chosen || sd.Labeled != labeled || fmt.Sprint(sd.Counts) != fmt.Sprint(want) || want[0] != 0 {
			t.Errorf("%s: %d cells, %d labeled, counts %v; want %d, %d, %v", sd.Name, sd.Cells, sd.Labeled, sd.Counts, chosen, labeled, want)
		}
		var total uint64
		for k := range pb[s] {
			for _, v := range pb[s][k] {
				total += uint64(v)
			}
		}
		if total != transcripts {
			t.Errorf("%s: the pseudobulk holds %d transcripts; its labeled cells %d", sd.Name, total, transcripts)
		}
		if sd.PanelGenes != len(counts.Genes) || len(d.Genes) != len(counts.Genes) {
			t.Errorf("%s: %d panel genes", sd.Name, sd.PanelGenes)
		}
	}
	whole, part := d.Samples[0], d.Samples[1]
	if !(part.Tissue > 0 && part.Tissue < whole.Tissue && part.Tissue <= 40*60+1e-6) || whole.Window.Kind != "tissue" || part.Window.Kind != "region" || part.Region.Name != "Left" {
		t.Errorf("tissue: %g of %g µm², windows %s and %s", part.Tissue, whole.Tissue, whole.Window.Kind, part.Window.Kind)
	}
	// The graph's and the enrichment's numbers, their own.
	gm, _ := f.a.artifacts.Get(g.ID)
	var gs spatial.Summary
	json.Unmarshal(gm.Summary, &gs)
	ap, _ := f.a.artifacts.Path(e.ID, spatial.FileAnalysis)
	an, _ := spatial.ReadAnalysis(filepath.Dir(ap))
	stats := map[string]cohort.Statistic{}
	for _, st := range whole.Statistics {
		stats[st.Family+":"+st.Key] = st
	}
	if st := stats["graph:mean neighbors"]; st.Value != gs.MeanDegree || st.Source != g.ID {
		t.Errorf("the graph's mean neighbors: %+v", st)
	}
	if st := stats["enrichment:B|C"]; st.Source != e.ID || math.Abs(st.Value-an.Enrichment.Count[1]/an.Enrichment.Expected[1]) > 1e-12 {
		t.Errorf("the enrichment's B–C ratio: %+v", st)
	}
	if len(part.Statistics) != 0 {
		t.Errorf("the region asked nothing: %+v", part.Statistics)
	}
	// Niches across the two: each sample's cells in them, and every cell's niche in niches.bin.
	if d.Niches == nil || d.Niches.Niches != 2 || len(d.Niches.Composition) != 6 {
		t.Fatalf("niches: %+v", d.Niches)
	}
	for _, sd := range d.Samples {
		if sd.Niches[0]+sd.Niches[1] != sd.NicheCells || sd.NicheCells == 0 || math.Abs(sd.NicheShares[0]+sd.NicheShares[1]-1) > 1e-12 {
			t.Errorf("%s's niches: %v of %d", sd.Name, sd.Niches, sd.NicheCells)
		}
	}
	// The table: each sample's cells by label as its counts, each from its labels.
	table, _ := os.ReadFile(filepath.Join(sdir, cohort.FileTable))
	trows, err := csv.NewReader(bytes.NewReader(table)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range trows[1:] {
		if r[2] != "cells" {
			continue
		}
		sd := d.Samples[map[string]int{"s-1": 0, "s-2": 1}[r[0]]]
		k := map[string]int{"A": 0, "B": 1, "C": 2}[r[3]]
		if r[4] != fmt.Sprint(sd.Counts[k]) || r[5] != "lab-x" {
			t.Errorf("the table's %s %s: %v", sd.Name, r[3], r)
		}
	}
	nrows, nniches, err := cohort.ReadNiches(sdir)
	if err != nil || len(nrows) != whole.Cells+part.Cells || len(nniches) != len(nrows) {
		t.Errorf("niches.bin holds %d cells: %v", len(nrows), err)
	}
	// Asked again: answered at once.
	var again struct {
		ID       string
		Artifact *artifacts.Manifest
	}
	json.NewDecoder(f.post("/api/cohort/summary", body).Body).Decode(&again)
	if again.ID != answer.ID || again.Artifact == nil {
		t.Error("asked again: not answered at once")
	}
	// Refused, saying why.
	for want, bad := range map[string]string{
		"not open on this computer":  strings.Replace(body, `"item":"0"`, `"item":"9"`, 1),
		"is not one of the cohort's": strings.Replace(body, `"names":["A","B","C"]`, `"names":["A","B"]`, 1),
		"the niches":                 strings.Replace(body, `"niches":2`, `"niches":1`, 1),
		"no spatial":                 strings.Replace(body, e.ID, "spa-nothing", 1),
		"1 to 500 samples":           `{"labels":{"name":"x","names":["A"]},"samples":[]}`,
		"each once":                  strings.Replace(body, `"names":["A","B","C"]`, `"names":["B","B","C"]`, 1),
	} {
		rec := f.post("/api/cohort/summary", bad)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %d %s", want, rec.Code, rec.Body.String())
		}
	}
}
