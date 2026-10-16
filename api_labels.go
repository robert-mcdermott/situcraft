package main

// Labels from markers (internal/label): the marker sets SituCraft ships, checked against a
// clustering's panel; a marker set checked; a clustering's cells labeled by a job and recorded as
// an artifact of kind "labels"; and a label set's cells, for the map, the tissue and the Viewer.

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/jobs"
	"situcraft/internal/label"
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

// labelSet answers a label set's description and marker set: {description, markers}.
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
	set, err := label.ReadMarkers(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
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

// segmentationCells is the number of the segmentation's cells a label set's clustering records.
func (a *app) segmentationCells(d *label.Description) (int, error) {
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
