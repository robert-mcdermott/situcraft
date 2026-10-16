package main

// Labels from markers (internal/label): the marker sets SituCraft ships, checked against a
// clustering's panel; a marker set checked; a clustering's cells labeled by a job and recorded as
// an artifact of kind "labels"; and a label set's cells, for the map, the tissue and the Viewer.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"situcraft/internal/anndata"
	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/jobs"
	"situcraft/internal/label"
	"situcraft/internal/segment"
	"situcraft/internal/simulate"
	"situcraft/internal/xenium"
)

const labelsCitation = "Tirosh I, et al. Dissecting the multicellular ecosystem of metastatic melanoma by single-cell RNA-seq. Science 2016;352:189–196. doi:10.1126/science.aad0501 (scores against control genes binned by expression); " +
	"Wolf FA, Angerer P, Theis FJ. SCANPY: large-scale single-cell gene expression data analysis. Genome Biol 2018;19:15. doi:10.1186/s13059-017-1382-0 (score_genes)"

type labelRequest struct {
	Clustering string          `json:"clustering"`
	Markers    json.RawMessage `json:"markers"`
	Parameters *label.Params   `json:"parameters,omitempty"`
}

type labelSpec struct {
	Bundle     string          `json:"bundle"`
	Clustering string          `json:"clustering"`
	Markers    json.RawMessage `json:"markers"`
	Parameters label.Params    `json:"parameters"`
}

// labelParams are what a label set's lineage records: the parameters and the marker set.
type labelParams struct {
	label.Params
	Markers struct {
		Name     string `json:"name"`
		Checksum string `json:"checksum"`
	} `json:"markers"`
}

func labelsID(clustering, checksum string, p label.Params) string {
	params, _ := json.Marshal(p)
	h := sha256.Sum256([]byte(clustering + "\n" + label.MethodVersion + "\n" + checksum + "\n" + string(params)))
	return "lab-" + hex.EncodeToString(h[:10])
}

func (a *app) registerLabels(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/labels/defaults", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"parameters": label.DefaultParams(), "method": label.MethodVersion, "citation": labelsCitation})
	})
	mux.HandleFunc("GET /api/local/{index}/clusterings/{id}/markers", a.markerStarters)
	mux.HandleFunc("POST /api/markers/check", a.checkMarkers)
	mux.HandleFunc("POST /api/local/{index}/label", a.startLabels)
	mux.HandleFunc("GET /api/labels/{id}", a.labelSet)
	mux.HandleFunc("GET /api/labels/{id}/cells", a.labelCells)
	mux.HandleFunc("GET /api/labels/{id}/scores", a.labelScores)
	a.registerLabelImports(mux)
	a.registerLabelReview(mux)
}

// markerStarter is a marker set offered for a clustering: SituCraft's, or the simulated tissue's,
// with the markers its panel lacks.
type markerStarter struct {
	ID       string           `json:"id"`
	Set      *label.MarkerSet `json:"set"`
	Checksum string           `json:"checksum"`
	Missing  []string         `json:"missing"`
}

// markerStarters answers a clustering's panel genes and the marker sets SituCraft offers for it:
// {genes, starters: [{id, set, checksum, missing}]}, those whose every marker the panel has first,
// the simulated tissue's set for a simulated bundle.
func (a *app) markerStarters(w http.ResponseWriter, r *http.Request) {
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	cm, err := a.clusteringFor(r.PathValue("id"), b.Experiment.AnalysisUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	dir, err := a.artifactFolder(cm.ID, cluster.FileDescription)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pc, err := cluster.ReadPCA(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var out []markerStarter
	if sim := simulatedSet(b); sim != nil {
		out = append(out, markerStarter{ID: "simulated", Set: sim})
	}
	for _, s := range label.Starters() {
		out = append(out, markerStarter{ID: s.ID, Set: s.Set})
	}
	for i := range out {
		out[i].Checksum = out[i].Set.Checksum()
		out[i].Missing = []string{}
		if _, err := out[i].Set.Resolve(pc.Genes); err != nil {
			var missing *label.MissingError
			if errors.As(err, &missing) {
				seen := map[string]bool{}
				for _, genes := range missing.Missing {
					for _, g := range genes {
						if !seen[g] {
							seen[g] = true
							out[i].Missing = append(out[i].Missing, g)
						}
					}
				}
				sort.Strings(out[i].Missing)
			}
		}
	}
	sort.SliceStable(out, func(x, y int) bool { return len(out[x].Missing) == 0 && len(out[y].Missing) > 0 })
	writeJSON(w, map[string]any{"genes": pc.Genes, "starters": out})
}

// simulatedSet is a simulated bundle's types' marker set, nil for a measured bundle.
func simulatedSet(b *xenium.Bundle) *label.MarkerSet {
	if b.Simulation == nil {
		return nil
	}
	raw, err := b.ReadSmall(xenium.SimulationFile)
	if err != nil {
		return nil
	}
	var f struct {
		Parameters simulate.Params `json:"parameters"`
	}
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	var types []string
	for _, t := range f.Parameters.Types {
		types = append(types, t.Name)
	}
	s := label.Simulated(types, simulate.MarkerNames(f.Parameters))
	if s.Check() != nil {
		return nil
	}
	return s
}

// checkMarkers checks a marker set ({set, genes: optional panel}): {set (as SituCraft reads it),
// checksum, missing: {label: [genes]}} or a 422 saying what is wrong.
func (a *app) checkMarkers(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Set   json.RawMessage `json:"set"`
		Genes []string        `json:"genes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body); err != nil || len(body.Set) == 0 {
		writeError(w, http.StatusBadRequest, "Send {\"set\": a marker set, \"genes\": optional panel}.")
		return
	}
	s, err := label.Parse(body.Set)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "markers.invalid", err.Error())
		return
	}
	missing := map[string][]string{}
	if len(body.Genes) > 0 {
		if _, err := s.Resolve(body.Genes); err != nil {
			var m *label.MissingError
			if errors.As(err, &m) {
				missing = m.Missing
			}
		}
	}
	writeJSON(w, map[string]any{"set": s, "checksum": s.Checksum(), "missing": missing})
}

// startLabels starts the job labeling a clustering's cells from a marker set, answering {id,
// artifact, job}: the artifact at once when it was made already.
func (a *app) startLabels(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var body labelRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body); err != nil || body.Clustering == "" || len(body.Markers) == 0 {
		writeError(w, http.StatusBadRequest, "Send {\"clustering\": artifact id, \"markers\": a marker set, \"parameters\": optional}.")
		return
	}
	p := label.DefaultParams()
	if body.Parameters != nil {
		p = *body.Parameters
	}
	if err := p.Check(); err != nil {
		writeError(w, http.StatusBadRequest, "The labels' parameters: "+err.Error()+".")
		return
	}
	set, err := label.Parse(body.Markers)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "markers.invalid", err.Error())
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	path, uuid := b.Path, b.Experiment.AnalysisUUID
	b.Close()
	cm, err := a.clusteringFor(body.Clustering, uuid)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir, err := a.artifactFolder(cm.ID, cluster.FileDescription)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pc, err := cluster.ReadPCA(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if _, err := set.Resolve(pc.Genes); err != nil {
		var m *label.MissingError
		if errors.As(err, &m) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]any{"error": "The panel lacks some of the set's " + err.Error() + ".", "code": "markers.missing", "missing": m.Missing})
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	checksum := set.Checksum()
	id := labelsID(cm.ID, checksum, p)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	spec := labelSpec{Bundle: path, Clustering: cm.ID, Markers: set.Canonical(), Parameters: p}
	job, _, err := a.jobs.Submit("labels", "Labels of "+cm.Name+" by "+set.Name, "labels\x00"+id, spec, a.labelsJob(spec, id))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

func (a *app) labelsJob(spec labelSpec, id string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		set, err := label.Parse(spec.Markers)
		if err != nil {
			return err
		}
		b, err := xenium.Open(spec.Bundle, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		cm, err := a.clusteringFor(spec.Clustering, b.Experiment.AnalysisUUID)
		if err != nil {
			return err
		}
		dir, err := a.artifactFolder(cm.ID, cluster.FileDescription)
		if err != nil {
			return err
		}
		d, err := cluster.ReadDescription(dir)
		if err != nil {
			return err
		}
		r.Log("info", "Labeling the cells of %s by %s (%d labels, %d states).", cm.Name, set.Name, len(set.Labels), len(set.States))
		r.Progress(0.05, "Reading the cells' expression")
		e, err := a.loadExpression(ctx, b, cm, dir, d)
		if err != nil {
			return err
		}
		r.Progress(0.4, "Scoring the cells")
		res, err := label.Run(e, set, d.Clusters.Count, d.Fixed.TargetSum, spec.Parameters)
		if err != nil {
			return err
		}
		labeled := 0
		for _, k := range res.Assignment.Label[0] {
			if k >= 0 {
				labeled++
			}
		}
		r.Log("info", "%d of %d cells labeled at the %s level.", labeled, len(res.Cells), set.Levels[0])
		stage, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := label.Write(stage, d.Segmentation, cluster.Side{ID: cm.ID, Name: cm.Name}, res); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Validating()
		r.Progress(0.9, "Checking the labels")
		var parents []artifacts.Parent
		for _, p := range cm.Lineage.Parents {
			if p.Kind == "bundle" || p.Kind == "segmentation" {
				parents = append(parents, p)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "clustering", ID: cm.ID, Name: cm.Name})
		lp := labelParams{Params: spec.Parameters}
		lp.Markers.Name, lp.Markers.Checksum = set.Name, set.Checksum()
		params, _ := json.Marshal(lp)
		jobSpec, _ := json.Marshal(spec)
		m := artifacts.Manifest{
			ID: id, Kind: "labels", Name: "Labels of " + cm.Name + " by " + set.Name,
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "labels", Spec: jobSpec},
				Method:     artifacts.Method{Name: "SituCraft labels from markers", Version: label.MethodVersion, Citation: labelsCitation},
				Parameters: params,
				Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: filesOf(label.Files, label.Roles),
		}
		return a.promoteChecked(m, label.Check, "label set")
	}
}

// labelsOf reads a complete label set's folder.
func (a *app) labelsOf(w http.ResponseWriter, id string) (string, bool) {
	m, err := a.artifacts.Get(id)
	if err != nil || m.Kind != "labels" || m.State != artifacts.Complete {
		writeError(w, http.StatusNotFound, "No such label set.")
		return "", false
	}
	dir, err := a.artifactFolder(m.ID, label.FileDescription)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return dir, true
}

// labelSet answers a label set's description and marker set (null for imported labels):
// {description, markers}.
func (a *app) labelSet(w http.ResponseWriter, r *http.Request) {
	dir, ok := a.labelsOf(w, r.PathValue("id"))
	if !ok {
		return
	}
	d, err := label.ReadDescription(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var set *label.MarkerSet
	if d.Source == nil {
		if set, err = label.ReadMarkers(dir); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	writeJSON(w, map[string]any{"description": d, "markers": set})
}

// labelCells answers a label set's cells as little-endian binary: the segmentation's cells, the
// cells labeled n, the levels L and the states S (u32 each); each cell's index in the segmentation
// and cluster (u32 × n each); at each level the label each cell carries there (its own, or the one
// above where it stopped; i32 × n, −1 for none); at each level its lead (f32 × n, NaN where not
// reached); its confidence (f32 × n); and for each state whether it carries it (u8 × n).
func (a *app) labelCells(w http.ResponseWriter, r *http.Request) {
	dir, ok := a.labelsOf(w, r.PathValue("id"))
	if !ok {
		return
	}
	d, _, lab, err := label.Read(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	segCells, err := a.segmentationCells(d)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	n := len(lab.Cells)
	out := make([]byte, 0, 16+n*(8+8*len(lab.Label)+4)+n*len(lab.State))
	for _, v := range []int{segCells, n, len(lab.Label), len(lab.State)} {
		out = binary.LittleEndian.AppendUint32(out, uint32(v))
	}
	for _, c := range lab.Cells {
		out = binary.LittleEndian.AppendUint32(out, c)
	}
	for _, c := range lab.Cluster {
		out = binary.LittleEndian.AppendUint32(out, c)
	}
	carried := make([]int32, n)
	for i := range carried {
		carried[i] = -1
	}
	for l := range lab.Label {
		for i, k := range lab.Label[l] {
			if k >= 0 {
				carried[i] = k
			}
			out = binary.LittleEndian.AppendUint32(out, uint32(carried[i]))
		}
	}
	f32 := func(v []float32) {
		for _, x := range v {
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(x))
		}
	}
	for l := range lab.Lead {
		f32(lab.Lead[l])
	}
	f32(lab.Confidence)
	for _, has := range lab.State {
		for _, v := range has {
			if v {
				out = append(out, 1)
			} else {
				out = append(out, 0)
			}
		}
	}
	writeBinary(w, out, false)
}

// segmentationCells is the number of the segmentation's cells a label set's clustering records
// (imported labels are of every cell).
func (a *app) segmentationCells(d *label.Description) (int, error) {
	if d.Clustering == nil {
		return d.Cells.Analyzed, nil
	}
	dir, err := a.artifactFolder(d.Clustering.ID, cluster.FileDescription)
	if err != nil {
		return 0, err
	}
	cd, err := cluster.ReadDescription(dir)
	if err != nil {
		return 0, err
	}
	return cd.Cells.Segmentation, nil
}

// labelScores answers each of the segmentation's cells' score for one label or state (?set=, its
// index; f32, little-endian, NaN for a cell not labeled); ?part=positive or negative answers that
// part of it.
func (a *app) labelScores(w http.ResponseWriter, r *http.Request) {
	dir, ok := a.labelsOf(w, r.PathValue("id"))
	if !ok {
		return
	}
	d, _, lab, err := label.Read(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if d.Source != nil {
		writeError(w, http.StatusBadRequest, "Imported labels have no scores.")
		return
	}
	k, err := strconv.Atoi(r.URL.Query().Get("set"))
	if err != nil || k < 0 || k >= len(d.Sets) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Ask for ?set= a label's index, 0 to %d.", len(d.Sets)-1))
		return
	}
	values := lab.Score[k]
	switch r.URL.Query().Get("part") {
	case "positive":
		values = lab.Positive[k]
	case "negative":
		if values = lab.Negative[k]; values == nil {
			writeError(w, http.StatusBadRequest, d.Sets[k].Name+" has no negative markers.")
			return
		}
	}
	segCells, err := a.segmentationCells(d)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	all := make([]float32, segCells)
	for i := range all {
		all[i] = float32(math.NaN())
	}
	for i, c := range lab.Cells {
		if int(c) < len(all) {
			all[c] = values[i]
		}
	}
	out := make([]byte, 0, 4*len(all))
	for _, v := range all {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
	}
	w.Header().Set("X-Label", d.Sets[k].Name)
	writeBinary(w, out, false)
}

// --- Labels imported from a table, or the instrument's clusters ---

// importLabelsRequest names what to read: a table's path (only this computer may name one), or
// one of the instrument's clusterings by name; the segmentation whose cells it labels; and, to
// import, its columns.
type importLabelsRequest struct {
	Path         string           `json:"path,omitempty"`
	Instrument   string           `json:"instrument,omitempty"`
	Segmentation string           `json:"segmentation"`
	Columns      label.ImportSpec `json:"columns"`
}

type importLabelsSpec struct {
	Bundle string              `json:"bundle"`
	Ask    importLabelsRequest `json:"ask"`
	SHA256 string              `json:"sha256"`
}

func importedLabelsID(segmentation, sha string, spec label.ImportSpec) string {
	cols, _ := json.Marshal(spec)
	h := sha256.Sum256([]byte("import\n" + segmentation + "\n" + sha + "\n" + string(cols) + "\n" + label.MethodVersion))
	return "lab-" + hex.EncodeToString(h[:10])
}

func (a *app) registerLabelImports(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/labels/read", a.readLabelsTable)
	mux.HandleFunc("POST /api/local/{index}/labels/import", a.importLabels)
}

// instrumentTable is one of the instrument's clusterings as a table of labels: each cell's barcode
// and "Instrument cluster N", N the instrument's number (apart from SituCraft's clusters' names);
// its SHA-256 of the barcodes and labels read.
func instrumentTable(b *xenium.Bundle, name string) (*label.Table, *xenium.AnalysisClustering, error) {
	an, err := b.ReadAnalysis()
	if err != nil {
		return nil, nil, err
	}
	for i := range an.Clusterings {
		c := &an.Clusterings[i]
		if c.Name != name {
			continue
		}
		ids := &anndata.Column{Name: "Barcode", Text: c.Barcodes}
		clusters := &anndata.Column{Name: "Cluster", Text: make([]string, len(c.Cluster))}
		h := sha256.New()
		for k, v := range c.Cluster {
			clusters.Text[k] = fmt.Sprintf("Instrument cluster %d", v)
			fmt.Fprintf(h, "%s\t%s\n", c.Barcodes[k], clusters.Text[k])
		}
		return &label.Table{Format: label.FormatInstrument, File: an.Source, SHA256: hex.EncodeToString(h.Sum(nil)), Rows: len(c.Barcodes), Columns: []*anndata.Column{ids, clusters}}, c, nil
	}
	return nil, nil, fmt.Errorf("the instrument's analysis has no clustering %q", name)
}

// labelsTable reads what a request names, refusing a path from another computer.
func (a *app) labelsTable(r *http.Request, b *xenium.Bundle, ask *importLabelsRequest) (*label.Table, error) {
	if ask.Instrument != "" {
		t, _, err := instrumentTable(b, ask.Instrument)
		return t, err
	}
	if ask.Path == "" {
		return nil, errors.New("name a table (its path) or one of the instrument's clusterings")
	}
	if r != nil && !isLoopbackRequest(r) {
		return nil, errForbidden
	}
	abs, err := filepath.Abs(strings.TrimSpace(ask.Path))
	if err != nil {
		return nil, err
	}
	ask.Path = abs
	return label.ReadTable(abs)
}

var errForbidden = errors.New("only this computer may name files to read")

// segmentationCellIDs reads a segmentation of the bundle's dataset and its cells' ids.
func (a *app) segmentationCellIDs(ctx context.Context, b *xenium.Bundle, id string) (artifacts.Manifest, []string, error) {
	sm, err := a.segmentationOf(id, b.Experiment.AnalysisUUID)
	if err != nil {
		return sm, nil, err
	}
	dir, err := a.segmentationFolder(ctx, b, sm)
	if err != nil {
		return sm, nil, err
	}
	cells, err := segment.ReadCells(dir)
	if err != nil {
		return sm, nil, err
	}
	ids := make([]string, len(cells))
	for i, c := range cells {
		ids[i] = c.ID
	}
	return sm, ids, nil
}

// readLabelsTable reads a table of labels and says what it holds and how it matches the
// segmentation's cells: {format, file, sha256, rows, path, columns, roles, match, others}, others
// the dataset's other segmentations its ids match better.
func (a *app) readLabelsTable(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var ask importLabelsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&ask); err != nil || ask.Segmentation == "" {
		writeError(w, http.StatusBadRequest, "Send {\"path\" or \"instrument\", \"segmentation\", \"columns\": optional}.")
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	t, err := a.labelsTable(r, b, &ask)
	if errors.Is(err, errForbidden) {
		writeError(w, http.StatusForbidden, "Only this computer may name files to read.")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	columns, roles := label.Describe(t)
	if ask.Columns.IDColumn != "" {
		roles.ID = ask.Columns.IDColumn
	}
	sm, ids, err := a.segmentationCellIDs(r.Context(), b, ask.Segmentation)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	out := map[string]any{"format": t.Format, "file": t.File, "sha256": t.SHA256, "rows": t.Rows, "path": t.Path, "columns": columns, "roles": roles, "segmentation": map[string]any{"id": sm.ID, "name": sm.Name, "cells": len(ids)}}
	if c := t.Column(roles.ID); c != nil {
		_, m := label.MatchIDs(c, ids)
		out["match"] = m
		// Another of the dataset's segmentations whose cells the ids name, when these do not.
		others := []map[string]any{}
		if m.NotInSegmentation > 0 {
			list, _ := a.artifacts.List()
			for _, om := range list {
				if om.Kind != "segmentation" || om.ID == sm.ID || om.State != artifacts.Complete {
					continue
				}
				if _, err := a.segmentationOf(om.ID, b.Experiment.AnalysisUUID); err != nil {
					continue
				}
				_, oids, err := a.segmentationCellIDs(r.Context(), b, om.ID)
				if err != nil {
					continue
				}
				if _, o := label.MatchIDs(c, oids); o.NotInSegmentation < m.NotInSegmentation {
					others = append(others, map[string]any{"id": om.ID, "name": om.Name, "notInSegmentation": o.NotInSegmentation, "matched": o.Matched})
				}
			}
		}
		out["others"] = others
	}
	writeJSON(w, out)
}

// importLabels starts the job importing a table's labels for a segmentation's cells, answering
// {id, artifact, job}: the artifact at once when it was made already. The table is refused here,
// with why, when its ids are not all the segmentation's.
func (a *app) importLabels(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var ask importLabelsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&ask); err != nil || ask.Segmentation == "" {
		writeError(w, http.StatusBadRequest, "Send {\"path\" or \"instrument\", \"segmentation\", \"columns\": {idColumn, column, confidenceColumn, colorColumn}}.")
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	path := b.Path
	defer b.Close()
	t, err := a.labelsTable(r, b, &ask)
	if errors.Is(err, errForbidden) {
		writeError(w, http.StatusForbidden, "Only this computer may name files to read.")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if ask.Instrument != "" {
		ask.Columns = label.ImportSpec{IDColumn: "Barcode", Column: "Cluster"}
	}
	sm, ids, err := a.segmentationCellIDs(r.Context(), b, ask.Segmentation)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if _, err := label.Import(t, ask.Columns, ids); err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "labels.refused", err.Error())
		return
	}
	id := importedLabelsID(sm.ID, t.SHA256, ask.Columns)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	spec := importLabelsSpec{Bundle: path, Ask: ask, SHA256: t.SHA256}
	job, _, err := a.jobs.Submit("label-import", "Labels from "+t.File+" ("+ask.Columns.Column+") for "+sm.Name, "label-import\x00"+id, spec, a.labelImportJob(spec, id))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

func (a *app) labelImportJob(spec importLabelsSpec, id string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		b, err := xenium.Open(spec.Bundle, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		ask := spec.Ask
		r.Progress(0.05, "Reading the table")
		t, err := a.labelsTable(nil, b, &ask)
		if err != nil {
			return err
		}
		if t.SHA256 != spec.SHA256 {
			return fmt.Errorf("%s changed after it was read: read it again", t.File)
		}
		r.Progress(0.4, "Matching the cells")
		sm, ids, err := a.segmentationCellIDs(ctx, b, ask.Segmentation)
		if err != nil {
			return err
		}
		imp, err := label.Import(t, ask.Columns, ids)
		if err != nil {
			return err
		}
		title, citation := ask.Columns.Column+" from "+t.File, ""
		if ask.Instrument != "" {
			_, c, err := instrumentTable(b, ask.Instrument)
			if err != nil {
				return err
			}
			imp.Source.Tool, imp.Source.Version, imp.Source.Title = "Xenium Onboard Analysis", b.Experiment.AnalysisSoftware, c.Title
			title, citation = "the instrument's "+strings.ToLower(c.Title)+" clusters", instrumentAnalysisCitation
		}
		labeled := 0
		for _, k := range imp.Label {
			if k >= 0 {
				labeled++
			}
		}
		r.Log("info", "%d of %s's %d rows name cells of %s (%d of them without a label); %d of its %d cells have no label; %d labels.", imp.Match.Matched, t.File, imp.Match.Rows, sm.Name, imp.Match.Matched-labeled, len(ids)-labeled, len(ids), len(imp.Labels))
		stage, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := label.WriteImported(stage, cluster.Side{ID: sm.ID, Name: sm.Name}, imp); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Validating()
		var parents []artifacts.Parent
		for _, p := range sm.Lineage.Parents {
			if p.Kind == "bundle" {
				parents = append(parents, p)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "segmentation", ID: sm.ID, Name: sm.Name})
		params, _ := json.Marshal(imp.Source)
		jobSpec, _ := json.Marshal(spec)
		files := filesOf(label.FilesOf(&label.Description{Source: &imp.Source}), label.Roles)
		m := artifacts.Manifest{
			ID: id, Kind: "labels", Name: "Labels: " + title,
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "label-import", Spec: jobSpec},
				Method:     artifacts.Method{Name: "Imported labels", Version: label.MethodVersion, Citation: citation},
				Parameters: params,
				Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: files,
		}
		return a.promoteChecked(m, label.Check, "imported label set")
	}
}

// --- Reviewing labels ---

func (a *app) registerLabelReview(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/segmentations/{seg}/review", a.reviewQueue)
	mux.HandleFunc("GET /api/local/{index}/clusterings/{id}/cells/{cell}/evidence", a.cellEvidence)
}

// reviewRequest is a segmentation's cells' labels (int32 per cell, little-endian, base64; −1 for
// none), their confidence (float32 per cell, NaN for none; optional), the neighbors counted, the
// thresholds, the queue's length and the cells reviewed already.
type reviewRequest struct {
	Labels        string   `json:"labels"`
	Confidence    string   `json:"confidence,omitempty"`
	Neighbors     int      `json:"neighbors"`
	Implausible   float64  `json:"implausible"`
	LowConfidence *float64 `json:"lowConfidence"`
	Limit         int      `json:"limit"`
	Reviewed      []int    `json:"reviewed"`
}

func decodeLE[T int32 | float32](b64 string, n int) ([]T, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	if len(raw) != 4*n {
		return nil, fmt.Errorf("%d bytes for %d cells", len(raw), n)
	}
	out := make([]T, n)
	for i := range out {
		v := binary.LittleEndian.Uint32(raw[4*i:])
		switch p := any(&out[i]).(type) {
		case *int32:
			*p = int32(v)
		case *float32:
			*p = math.Float32frombits(v)
		}
	}
	return out, nil
}

// reviewQueue answers the cells to review among a segmentation's labeled cells: {neighbors,
// counts, queue: [{cell, reasons, share, dominant, confidence}], share (float32 per cell, base64),
// dominant (int32 per cell, base64)}.
func (a *app) reviewQueue(w http.ResponseWriter, r *http.Request) {
	var body reviewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<20)).Decode(&body); err != nil || body.Labels == "" {
		writeError(w, http.StatusBadRequest, "Send {\"labels\": int32 per cell in base64, \"confidence\": optional, \"neighbors\", \"implausible\", \"lowConfidence\", \"limit\", \"reviewed\"}.")
		return
	}
	if body.Neighbors < 1 || body.Neighbors > 100 || !(body.Implausible >= 0 && body.Implausible <= 1) || body.Limit < 0 || body.Limit > 100000 {
		writeError(w, http.StatusBadRequest, "Neighbors is 1 to 100, implausible a share from 0 to 1, limit at most 100,000.")
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	sm, err := a.segmentationOf(r.PathValue("seg"), b.Experiment.AnalysisUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	dir, err := a.segmentationFolder(r.Context(), b, sm)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	cells, err := segment.ReadCells(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	n := len(cells)
	labels, err := decodeLE[int32](body.Labels, n)
	if err != nil {
		writeError(w, http.StatusBadRequest, "The labels: "+err.Error()+".")
		return
	}
	var conf []float32
	if body.Confidence != "" {
		if conf, err = decodeLE[float32](body.Confidence, n); err != nil {
			writeError(w, http.StatusBadRequest, "The confidence: "+err.Error()+".")
			return
		}
	}
	xy := make([]float64, 2*n)
	for i, c := range cells {
		xy[2*i], xy[2*i+1] = float64(c.X), float64(c.Y)
	}
	share, dominant, err := label.Agreement(r.Context(), xy, labels, body.Neighbors, runtime.NumCPU())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	o := label.QueueOptions{Implausible: body.Implausible, LowConfidence: math.NaN(), Limit: body.Limit, Reviewed: map[int]bool{}}
	if body.LowConfidence != nil {
		o.LowConfidence = *body.LowConfidence
	}
	for _, c := range body.Reviewed {
		o.Reviewed[c] = true
	}
	items, counts := label.Queue(labels, share, dominant, conf, o)
	if items == nil {
		items = []label.QueueItem{}
	}
	sb := make([]byte, 0, 4*n)
	db := make([]byte, 0, 4*n)
	for i := range share {
		sb = binary.LittleEndian.AppendUint32(sb, math.Float32bits(share[i]))
		db = binary.LittleEndian.AppendUint32(db, uint32(dominant[i]))
	}
	writeJSON(w, map[string]any{"neighbors": body.Neighbors, "counts": counts, "queue": items, "share": base64.StdEncoding.EncodeToString(sb), "dominant": base64.StdEncoding.EncodeToString(db)})
}

// cellEvidence answers a segmentation cell's evidence in a clustering: {cell, index (among the
// cells analyzed, −1 for none), cluster, neighbors (its nearest cells in expression, as
// segmentation cells), labels (with ?labels=, a label set from markers made from the clustering:
// its levels, candidates, markers and controls in the cell; with ?also=, another label's too)}.
func (a *app) cellEvidence(w http.ResponseWriter, r *http.Request) {
	e, d, ok := a.expressionOf(w, r)
	if !ok {
		return
	}
	cell, err := strconv.Atoi(r.PathValue("cell"))
	if err != nil || cell < 0 || cell >= d.Cells.Segmentation {
		writeError(w, http.StatusNotFound, "No such cell in the segmentation.")
		return
	}
	i := e.Index(uint32(cell))
	out := map[string]any{"cell": cell, "index": i, "cluster": -1, "neighbors": []int{}}
	if i < 0 {
		writeJSON(w, out)
		return
	}
	out["cluster"] = e.Cluster[i]
	dir, err := a.artifactFolder(r.PathValue("id"), cluster.FileDescription)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Source == nil {
		nb, err := cluster.ReadNeighbors(dir)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		var near []int
		for _, j := range nb.Index[i*nb.K : (i+1)*nb.K] {
			near = append(near, int(e.Cells[j]))
		}
		out["neighbors"] = near
	}
	if id := r.URL.Query().Get("labels"); id != "" {
		ldir, ok := a.labelsOf(w, id)
		if !ok {
			return
		}
		ld, _, lab, err := label.Read(ldir)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if ld.Source != nil || ld.Clustering == nil || ld.Clustering.ID != r.PathValue("id") || len(lab.Cells) != len(e.Cells) {
			writeError(w, http.StatusBadRequest, "The label set is not from markers of this clustering.")
			return
		}
		also := -1
		if v := r.URL.Query().Get("also"); v != "" {
			if also, err = strconv.Atoi(v); err != nil {
				writeError(w, http.StatusBadRequest, "also is a label's index.")
				return
			}
		}
		out["labels"] = label.CellEvidence(e, ld, lab, i, also)
	}
	writeJSON(w, out)
}
