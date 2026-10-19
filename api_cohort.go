package main

// The cohort's summaries (wave 7, slice 4; internal/cohort): every sample of a cohort summarized
// the same way in one job (its cells by label, their densities in its tissue, its quality, its
// pseudobulk, its graph's and analyses' numbers), and niches found across the samples together; an
// artifact of kind cohort-summary, named by what it is made of. The page sends each sample's
// bundle (an item open on this computer), segmentation, cells chosen, labels (one vocabulary of
// names for every sample), region, graph and analyses.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/cohort"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/spatial"
	"situcraft/internal/xenium"
)

// cohortCitation cites the pseudobulk's approach, and nichesCitation the niches across samples'.
const (
	cohortCitation = "Crowell HL et al. (2020) On the discovery of subpopulation-specific state transitions from multi-sample multi-condition single-cell RNA sequencing data. Nat Commun 11:6077."
	nichesCitation = "Schürch CM, Bhate SS, Barlow GL, et al. Coordinated cellular neighborhoods orchestrate antitumoral immunity at the colorectal cancer invasive front. Cell 2020;182:1341–1359."
)

type cohortSampleRequest struct {
	Sample       string         `json:"sample"`
	Name         string         `json:"name"`
	Dataset      string         `json:"dataset"`
	Item         string         `json:"item"`
	Segmentation string         `json:"segmentation"`
	Cells        graphCells     `json:"cells"`
	Labels       *spatialLabels `json:"labels"`
	Region       *spatialWindow `json:"region,omitempty"`
	Graph        string         `json:"graph,omitempty"`
	Analyses     []string       `json:"analyses,omitempty"`
}

type cohortSummaryRequest struct {
	Labels  cohort.LabelSet       `json:"labels"`
	Samples []cohortSampleRequest `json:"samples"`
	Niches  *spatial.NicheParams  `json:"niches,omitempty"`
	Closing float64               `json:"closing,omitempty"`
}

type cohortSummarySpec struct {
	Bundles []string `json:"bundles"`
	cohortSummaryRequest
}

func (a *app) registerCohort(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/cohort/summary", a.startCohortSummary)
	mux.HandleFunc("POST /api/cohort/panels", a.comparePanels)
}

// startCohortSummary starts the job summarizing a cohort's samples, answering {id, artifact, job}:
// the artifact at once when it was made already.
func (a *app) startCohortSummary(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var body cohortSummaryRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<30)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Send {\"labels\": {name, level, names}, \"samples\": [{sample, name, dataset, item, segmentation, cells, labels, region, graph, analyses}], \"niches\"}.")
		return
	}
	spec, err := a.checkCohortSummary(&body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := cohortSummaryID(body)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	job, _, err := a.jobs.Submit("cohort", fmt.Sprintf("A summary of %d samples", len(body.Samples)), "cohort\x00"+id, spec, a.cohortSummaryJob(spec, id))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

// checkCohortSummary checks a request, filling its defaults, and answers the job's spec: each
// sample's bundle found, its segmentation of that bundle's run, its labels' names the cohort's,
// its cells and labels its segmentation's, its region a polygon, its graph and analyses its
// segmentation's.
func (a *app) checkCohortSummary(body *cohortSummaryRequest) (cohortSummarySpec, error) {
	spec := cohortSummarySpec{}
	K := len(body.Labels.Names)
	if K < 1 || K > 1000 {
		return spec, errors.New("the cohort's labels are 1 to 1,000 names")
	}
	vocabulary := map[string]bool{}
	for _, n := range body.Labels.Names {
		if n == "" || vocabulary[n] {
			return spec, errors.New("the cohort's labels need names, each once")
		}
		vocabulary[n] = true
	}
	if len(body.Samples) < 1 || len(body.Samples) > 500 {
		return spec, errors.New("a summary is of 1 to 500 samples")
	}
	if body.Closing == 0 {
		body.Closing = 10
	}
	if body.Closing < 0 || body.Closing > 100 {
		return spec, errors.New("the tissue window's closing radius is 0 to 100 µm")
	}
	if body.Niches != nil {
		if err := body.Niches.Check(); err != nil {
			return spec, fmt.Errorf("the niches: %w", err)
		}
	}
	seen := map[string]bool{}
	for k := range body.Samples {
		s := &body.Samples[k]
		what := s.Name
		if s.Sample == "" || s.Name == "" || seen[s.Sample] {
			return spec, errors.New("each sample needs its id and name, once")
		}
		seen[s.Sample] = true
		file, ok := a.files.get(s.Item)
		if !ok || (file.Kind != "bundle" && file.Kind != "archive") {
			return spec, fmt.Errorf("%s: its bundle is not open on this computer", what)
		}
		b, err := xenium.Open(file.path, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return spec, fmt.Errorf("%s: %v", what, err)
		}
		uuid := b.Experiment.AnalysisUUID
		b.Close()
		sm, err := a.segmentationOf(s.Segmentation, uuid)
		if err != nil {
			return spec, fmt.Errorf("%s: %v", what, err)
		}
		var segSummary struct{ Cells int }
		json.Unmarshal(sm.Summary, &segSummary)
		if s.Labels == nil || len(s.Labels.Names) == 0 || len(s.Labels.Checksum) != 64 {
			return spec, fmt.Errorf("%s: its labels need their names, each cell's label and their checksum", what)
		}
		for _, n := range s.Labels.Names {
			if !vocabulary[n] {
				return spec, fmt.Errorf("%s: its label %q is not one of the cohort's (%s)", what, n, strings.Join(body.Labels.Names, ", "))
			}
		}
		if _, err := s.Labels.decode(segSummary.Cells); err != nil && segSummary.Cells > 0 {
			return spec, fmt.Errorf("%s: its labels: %v", what, err)
		}
		f := clusterFilter{Keep: s.Cells.Keep, Checksum: s.Cells.Checksum}
		if _, err := f.keep(segSummary.Cells); err != nil && segSummary.Cells > 0 {
			return spec, fmt.Errorf("%s: its cells: %v", what, err)
		}
		if s.Region != nil {
			s.Region.Kind = "region"
			if err := s.Region.check(); err != nil {
				return spec, fmt.Errorf("%s: its region: %v", what, err)
			}
			if _, err := spatial.PolygonWindow(s.Region.Geometry); err != nil {
				return spec, fmt.Errorf("%s: its region: %v", what, err)
			}
		}
		ofSegmentation := func(id, kind string) error {
			m, err := a.artifacts.Get(id)
			if err != nil || m.Kind != kind || m.State != artifacts.Complete {
				return fmt.Errorf("%s: no %s %s", what, kind, id)
			}
			for _, p := range m.Lineage.Parents {
				if p.Kind == "segmentation" && p.ID != sm.ID {
					return fmt.Errorf("%s: its %s %s is of another segmentation", what, kind, id)
				}
			}
			return nil
		}
		if s.Graph != "" {
			if err := ofSegmentation(s.Graph, "graph"); err != nil {
				return spec, err
			}
		}
		for _, id := range s.Analyses {
			if err := ofSegmentation(id, "spatial"); err != nil {
				return spec, err
			}
		}
		spec.Bundles = append(spec.Bundles, file.path)
	}
	spec.cohortSummaryRequest = *body
	return spec, nil
}

// cohortSummaryID names a summary by what it is made of: the labels, each sample's segmentation,
// cells, labels, region, graph and analyses, the niches' parameters and the method's version.
func cohortSummaryID(body cohortSummaryRequest) string {
	key := body
	key.Samples = append([]cohortSampleRequest(nil), body.Samples...)
	for k := range key.Samples {
		// The cells and labels by their checksums; the item by nothing (where it is open).
		key.Samples[k].Item = ""
		key.Samples[k].Cells.Keep = ""
		labels := *key.Samples[k].Labels
		labels.Codes = ""
		key.Samples[k].Labels = &labels
	}
	raw, _ := json.Marshal(key)
	h := sha256.Sum256(append(raw, []byte("\n"+cohort.MethodVersion)...))
	return "coh-" + hex.EncodeToString(h[:10])
}

// cohortSummaryJob summarizes each sample in turn, finds the niches across them, and records the
// summary.
func (a *app) cohortSummaryJob(spec cohortSummarySpec, id string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		K := len(spec.Labels.Names)
		index := map[string]int32{}
		for k, n := range spec.Labels.Names {
			index[n] = int32(k)
		}
		doc := &cohort.Document{Format: "situcraft-cohort-summary", Version: 1, Method: "SituCraft cohort summary", MethodVersion: cohort.MethodVersion, Labels: spec.Labels, Genes: []string{}}
		geneAt := map[string]int{}
		var bySample [][][]uint64 // each sample's pseudobulk, over its own genes
		var genesOf [][]string
		var nicheCells []spatial.NicheCells
		var nicheRows [][]uint32
		var panels []cohort.Panel
		parents := []artifacts.Parent{}
		S := len(spec.Samples)
		for s, sr := range spec.Samples {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.Progress(0.8*float64(s)/float64(S), fmt.Sprintf("Summarizing %s (%d of %d)", sr.Name, s+1, S))
			sd, pb, genes, cells, err := a.summarizeSample(ctx, spec.Bundles[s], sr, index, K, spec.Closing, r)
			if err != nil {
				return fmt.Errorf("%s: %w", sr.Name, err)
			}
			for _, g := range genes {
				if _, ok := geneAt[g]; !ok {
					geneAt[g] = len(doc.Genes)
					doc.Genes = append(doc.Genes, g)
				}
			}
			bySample, genesOf = append(bySample, pb), append(genesOf, genes)
			doc.Samples = append(doc.Samples, *sd)
			panels = append(panels, cells.panel)
			parents = append(parents, artifacts.Parent{Kind: "segmentation", ID: sd.Segmentation.ID, Name: sd.Segmentation.Name})
			if spec.Niches != nil {
				nicheCells = append(nicheCells, cells.niche)
				nicheRows = append(nicheRows, cells.rows)
			}
			r.Log("info", "%s: %d cells, %d labeled; %s µm² of tissue; %d genes.", sr.Name, sd.Cells, sd.Labeled, formatArea(sd.Tissue), len(genes))
		}
		// The pseudobulk over every sample's genes, Missing where a sample's panel has not one.
		G := len(doc.Genes)
		pseudobulk := make([][][]uint32, S)
		for s := range pseudobulk {
			pseudobulk[s] = make([][]uint32, K)
			has := make([]bool, G)
			for _, g := range genesOf[s] {
				has[geneAt[g]] = true
			}
			for k := range pseudobulk[s] {
				row := make([]uint32, G)
				for g := range row {
					if !has[g] {
						row[g] = cohort.Missing
					}
				}
				for j, g := range genesOf[s] {
					v := bySample[s][k][j]
					if v >= cohort.Missing {
						return fmt.Errorf("%s: %d transcripts of %s in %s, more than the pseudobulk holds", spec.Samples[s].Name, v, g, spec.Labels.Names[k])
					}
					row[geneAt[g]] = uint32(v)
				}
				pseudobulk[s][k] = row
			}
		}
		// The niches across the samples together.
		var rows []uint32
		var niches []int32
		if spec.Niches != nil {
			p := *spec.Niches
			r.Log("info", "Niches across the %d samples: windows of each cell's %d nearest labeled cells in its own sample, %d niches, the best of %d k-means starts (seed %d), %d further seeds.", S, p.Neighbors, p.Niches, p.Starts, p.Seed, p.Seeds)
			nc, offsets, err := spatial.FindNichesAcross(ctx, nicheCells, K, p, runtime.NumCPU(), func(f float64) { r.Progress(0.8+0.15*f, "Finding the niches across the samples") })
			if err != nil {
				return fmt.Errorf("the niches: %w", err)
			}
			nd := &cohort.NichesDoc{Parameters: p, Niches: nc.Count, Composition: nc.Composition, Cells: nc.Cells, Tissue: nc.Tissue, Windows: nc.Windows, Inertia: nc.Inertia, Agreement: nc.Agreement}
			if nd.Agreement == nil {
				nd.Agreement = []float64{}
			}
			for v := 0; v < nc.Count; v++ {
				nd.Defining = append(nd.Defining, spatial.DefiningLabels(nc.Composition[v*K:(v+1)*K], nc.Tissue))
			}
			doc.Niches = nd
			for s := range doc.Samples {
				counts := make([]int, nc.Count)
				windowed := 0
				for _, v := range nc.Assign[offsets[s]:offsets[s+1]] {
					if v >= 0 {
						counts[v]++
						windowed++
					}
				}
				shares := make([]float64, nc.Count)
				for v := range shares {
					if windowed > 0 {
						shares[v] = float64(counts[v]) / float64(windowed)
					}
				}
				doc.Samples[s].Niches, doc.Samples[s].NicheShares, doc.Samples[s].NicheCells = counts, shares, windowed
				rows = append(rows, nicheRows[s]...)
			}
			niches = nc.Assign
		}
		// The samples' panels compared; their quality side by side; their pseudobulk's principal
		// components over the genes every panel has.
		ids := make([]string, S)
		for k := range doc.Samples {
			ids[k] = doc.Samples[k].Sample
		}
		doc.Panels = cohort.ComparePanels(ids, panels)
		r.Log("info", "The samples' panels: %s.", doc.Panels.PanelWords())
		doc.QCMetrics, doc.OutlierRule = cohort.QCMetrics, cohort.DefaultOutlierRule
		doc.Outliers, doc.Batches = cohort.Outliers(doc.Samples, doc.OutlierRule), cohort.Batches(doc.Samples)
		for _, o := range doc.Outliers {
			r.Log("info", "%s is far out: %s.", o.Name, o.Reason)
		}
		if S >= 2 {
			pca, err := cohort.PseudobulkPCA(cohort.SharedCounts(doc, pseudobulk, doc.Panels.Shared), doc.Panels.Shared, cohort.PCATop, cohort.PCAComponents)
			if err != nil {
				r.Log("warn", "No principal components: %v.", err)
			} else {
				doc.PCA = pca
			}
		}
		stage, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := cohort.Write(stage, doc, pseudobulk, rows, niches); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Validating()
		r.Progress(0.97, "Checking the summary")
		jobSpec, _ := json.Marshal(spec)
		params, _ := json.Marshal(map[string]any{"labels": spec.Labels, "niches": spec.Niches, "closing": spec.Closing})
		var files []artifacts.File
		for _, f := range []string{cohort.FileSummary, cohort.FilePseudobulk, cohort.FileTable, cohort.FilePseudobulkCSV} {
			files = append(files, artifacts.File{Path: f, Role: cohort.Roles[f]})
		}
		if spec.Niches != nil {
			files = append(files, artifacts.File{Path: cohort.FileNiches, Role: cohort.Roles[cohort.FileNiches]})
		}
		citation := cohortCitation
		if spec.Niches != nil {
			citation += " " + nichesCitation
		}
		noun := "samples"
		if S == 1 {
			noun = "sample"
		}
		m := artifacts.Manifest{
			ID:   id,
			Kind: "cohort-summary",
			Name: fmt.Sprintf("Summary of %d %s by %s", S, noun, spec.Labels.Name),
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "cohort", Spec: jobSpec},
				Method:     artifacts.Method{Name: "SituCraft cohort summary", Version: cohort.MethodVersion, Citation: citation},
				Parameters: params,
				Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: files,
		}
		recorded, err := a.artifacts.Promote(m, cohort.Check)
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		if recorded.State == artifacts.Invalid {
			return fmt.Errorf("%w: the summary's check found fatal problems (artifact %s, kept for review)", jobs.ErrInvalid, recorded.ID)
		}
		r.Log("info", "Recorded the summary as artifact %s.", recorded.ID)
		return nil
	}
}

// sampleCells are a sample's chosen cells for the niches (their rows and their centroids and
// labels), and its bundle's panel.
type sampleCells struct {
	rows  []uint32
	niche spatial.NicheCells
	panel cohort.Panel
}

// summarizeSample summarizes one sample: its segmentation's cells and counts, its cells chosen
// labeled in the cohort's vocabulary, its tissue (in its region), its graph's and analyses'
// numbers. Answers its document, its pseudobulk over its genes, the genes, and its chosen cells.
func (a *app) summarizeSample(ctx context.Context, bundle string, sr cohortSampleRequest, index map[string]int32, K int, closing float64, r *jobs.Run) (*cohort.SampleDoc, [][]uint64, []string, sampleCells, error) {
	var none sampleCells
	b, err := xenium.Open(bundle, xenium.Options{CacheDir: a.cacheDir})
	if err != nil {
		return nil, nil, nil, none, err
	}
	defer b.Close()
	sm, err := a.segmentationOf(sr.Segmentation, b.Experiment.AnalysisUUID)
	if err != nil {
		return nil, nil, nil, none, err
	}
	dir, err := a.segmentationFolder(ctx, b, sm)
	if err != nil {
		return nil, nil, nil, none, err
	}
	cells, err := segment.ReadCells(dir)
	if err != nil {
		return nil, nil, nil, none, err
	}
	counts, err := segment.ReadCounts(dir)
	if err != nil {
		return nil, nil, nil, none, err
	}
	// The instrument's segmentation records its nuclei in the bundle's cells table, not its own.
	if known := a.knownSegmentation(b); known != nil && known.ID == sm.ID {
		if err := instrumentNuclei(b, cells); err != nil {
			r.Log("warn", "%s: the nuclei of the bundle's cells could not be read (%v): its nuclei's numbers are left out.", sr.Name, err)
		}
	}
	local, err := sr.Labels.decode(len(cells))
	if err != nil {
		return nil, nil, nil, none, fmt.Errorf("its labels: %w", err)
	}
	label := make([]int32, len(cells))
	for i, l := range local {
		label[i] = -1
		if l >= 0 {
			if int(l) >= len(sr.Labels.Names) {
				return nil, nil, nil, none, fmt.Errorf("a cell's label %d is not one of the %d", l, len(sr.Labels.Names))
			}
			label[i] = index[sr.Labels.Names[l]]
		}
	}
	f := clusterFilter{Keep: sr.Cells.Keep, Checksum: sr.Cells.Checksum}
	keep, err := f.keep(len(cells))
	if err != nil {
		return nil, nil, nil, none, fmt.Errorf("its cells: %w", err)
	}
	// Its tissue: the tissue window's, or the tissue within its region.
	c := closing
	tissue, ref, err := a.spatialWindowOf(ctx, dir, &spatialWindow{Kind: "tissue", Closing: &c}, r)
	if err != nil {
		return nil, nil, nil, none, err
	}
	area := ref.Area
	if sr.Region != nil {
		rw, err := spatial.PolygonWindow(sr.Region.Geometry)
		if err != nil {
			return nil, nil, nil, none, err
		}
		areas, err := spatial.RegionAreas(ctx, tissue, []*spatial.Window{rw}, runtime.NumCPU())
		if err != nil {
			return nil, nil, nil, none, err
		}
		area = areas[0]
		ref = &spatial.WindowRef{Kind: "region", Region: sr.Region.Region, Area: area, Segments: rw.Segments()}
	}
	sum, err := cohort.Summarize(cohort.Input{Cells: cells, Counts: counts, Keep: keep, Label: label, Tissue: area}, K)
	if err != nil {
		return nil, nil, nil, none, err
	}
	sd := &cohort.SampleDoc{Sample: sr.Sample, Name: sr.Name, Dataset: sr.Dataset, Segmentation: spatial.Side{ID: sm.ID, Name: sm.Name},
		Labels: cohort.SampleLabels{ID: sr.Labels.ID, Kind: sr.Labels.Kind, Name: sr.Labels.Name, Level: sr.Labels.Level, Checksum: sr.Labels.Checksum},
		Choice: sr.Cells.choice(), Window: ref, PanelGenes: len(counts.Genes), Summary: *sum, Statistics: []cohort.Statistic{}}
	sd.Bundle = cohort.BundleFacts{Run: b.Experiment.RunName, Slide: b.Experiment.SlideID, Region: b.Experiment.RegionName, Software: b.Experiment.AnalysisSoftware}
	var panel cohort.Panel
	if p := b.Panel; p != nil {
		panel = cohort.Panel{Name: p.Name, Design: p.DesignID, Version: p.Version}
		for _, g := range p.Genes() {
			panel.Genes = append(panel.Genes, cohort.PanelGene{Name: g.Name, ID: g.ID, Codewords: g.Codewords})
		}
		sd.Bundle.Panel = cohort.PanelRef{Name: p.Name, Design: p.DesignID, Version: p.Version, Genes: len(panel.Genes)}
	}
	if sr.Region != nil {
		reg := *sr.Region.Region
		sd.Region = &reg
	}
	if sr.Graph != "" {
		gm, err := a.artifacts.Get(sr.Graph)
		if err != nil {
			return nil, nil, nil, none, err
		}
		var gs spatial.Summary
		json.Unmarshal(gm.Summary, &gs)
		sd.Statistics = append(sd.Statistics, cohort.GraphStatistics(gm.ID, gs)...)
	}
	for _, id := range sr.Analyses {
		p, err := a.artifacts.Path(id, "analysis.json")
		if err != nil {
			return nil, nil, nil, none, err
		}
		an, err := spatial.ReadAnalysis(p[:len(p)-len("analysis.json")])
		if err != nil {
			return nil, nil, nil, none, err
		}
		sd.Statistics = append(sd.Statistics, cohort.AnalysisStatistics(id, an)...)
	}
	sc := sampleCells{panel: panel}
	for i, cl := range cells {
		if keep[i] {
			sc.rows = append(sc.rows, uint32(i))
			sc.niche.X, sc.niche.Y, sc.niche.Label = append(sc.niche.X, cl.X), append(sc.niche.Y, cl.Y), append(sc.niche.Label, label[i])
		}
	}
	return sd, sum.Pseudobulk, counts.Genes, sc, nil
}

// formatArea writes an area in µm² with thousands separated.
func formatArea(v float64) string {
	s := fmt.Sprintf("%.0f", math.Round(v))
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// instrumentNuclei gives the instrument's segmentation's cells their nuclei's areas from the
// bundle's cells table (by cell id), where the segmentation records none.
func instrumentNuclei(b *xenium.Bundle, cells []segment.Cell) error {
	for _, c := range cells {
		if c.NucleusArea > 0 {
			return nil
		}
	}
	table, err := b.ReadCells()
	if err != nil {
		return err
	}
	if table.NucleusArea == nil {
		return errors.New("cells.parquet has no nucleus_area column")
	}
	for i := range cells {
		if k, ok := table.Index[cells[i].ID]; ok && int(k) < len(table.NucleusArea) {
			if v := table.NucleusArea[k]; v > 0 && !math.IsNaN(v) {
				cells[i].NucleusArea = float32(v)
			}
		}
	}
	return nil
}

// comparePanels answers the panels of samples' bundles compared (POST /api/cohort/panels):
// {samples: [{sample, name, item}]}, before any summary.
func (a *app) comparePanels(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Samples []struct {
			Sample string `json:"sample"`
			Name   string `json:"name"`
			Item   string `json:"item"`
		} `json:"samples"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || len(body.Samples) == 0 || len(body.Samples) > 500 {
		writeError(w, http.StatusBadRequest, "Send {\"samples\": [{sample, name, item}]}, 1 to 500 samples.")
		return
	}
	ids := make([]string, len(body.Samples))
	panels := make([]cohort.Panel, len(body.Samples))
	for k, s := range body.Samples {
		file, ok := a.files.get(s.Item)
		if !ok || (file.Kind != "bundle" && file.Kind != "archive") {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s: its bundle is not open on this computer", s.Name))
			return
		}
		b, err := xenium.Open(file.path, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s: %v", s.Name, err))
			return
		}
		ids[k] = s.Sample
		if p := b.Panel; p != nil {
			panels[k] = cohort.Panel{Name: p.Name, Design: p.DesignID, Version: p.Version}
			for _, g := range p.Genes() {
				panels[k].Genes = append(panels[k].Genes, cohort.PanelGene{Name: g.Name, ID: g.ID, Codewords: g.Codewords})
			}
		}
		b.Close()
	}
	writeJSON(w, cohort.ComparePanels(ids, panels))
}
