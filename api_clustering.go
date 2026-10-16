package main

// Clusterings of a segmentation's cells (internal/cluster), made by a job and recorded as artifacts
// of kind "clustering". The segmentation's counts are read from its files (the instrument's
// written once from the bundle into the cache). A clustering is named by the segmentation, the
// cells analyzed (the rule, or a QC filter's cells by their checksum), the method's version and the
// parameters: asking again answers it without a job.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strconv"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// clusteringCitation cites the methods a clustering uses.
const clusteringCitation = "Wolf FA, Angerer P, Theis FJ. SCANPY: large-scale single-cell gene expression data analysis. Genome Biol 2018;19:15. doi:10.1186/s13059-017-1382-0; " +
	"Traag VA, Waltman L, van Eck NJ. From Louvain to Leiden: guaranteeing well-connected communities. Sci Rep 2019;9:5233. doi:10.1038/s41598-019-41695-z; " +
	"McInnes L, Healy J, Melville J. UMAP: Uniform Manifold Approximation and Projection for dimension reduction. arXiv:1802.03426, 2018 (the neighbor graph); " +
	"Mann HB, Whitney DR. On a test of whether one of two random variables is stochastically larger than the other. Ann Math Stat 1947;18:50–60 (the markers)"

// clusterFilter is a QC filter's cells, as the page sends them: its id, name and checksum, and the
// bytes the checksum is of (lib/filters.js keepChecksum: the count of cells, then a bit per cell).
type clusterFilter struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
	Keep     string `json:"keep"`
}

// keep decodes the filter's cells, checking them against its checksum and the segmentation.
func (f *clusterFilter) keep(cells int) ([]bool, error) {
	raw, err := base64.StdEncoding.DecodeString(f.Keep)
	if err != nil || len(raw) < 4 {
		return nil, errors.New("the filter's cells are not base64 of its count and bits")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != f.Checksum {
		return nil, errors.New("the filter's cells do not have its checksum")
	}
	n := int(binary.LittleEndian.Uint32(raw))
	if n != cells || len(raw) != 4+(n+7)/8 {
		return nil, fmt.Errorf("the filter keeps cells of a table of %d; the segmentation has %d cells", n, cells)
	}
	out := make([]bool, n)
	for i := range out {
		out[i] = raw[4+i/8]>>(i%8)&1 == 1
	}
	return out, nil
}

type clusterRequest struct {
	Segmentation string          `json:"segmentation"`
	Filter       *clusterFilter  `json:"filter,omitempty"`
	Parameters   *cluster.Params `json:"parameters,omitempty"`
}

type clusterSpec struct {
	Bundle       string         `json:"bundle"`
	Segmentation string         `json:"segmentation"`
	Filter       *clusterFilter `json:"filter,omitempty"`
	Parameters   cluster.Params `json:"parameters"`
}

// clusteringID names a clustering by what it is made from.
func clusteringID(segmentation string, filter *clusterFilter, p cluster.Params) string {
	params, _ := json.Marshal(p)
	cells := "rule"
	if filter != nil {
		cells = "filter " + filter.Checksum
	}
	h := sha256.Sum256([]byte(segmentation + "\n" + cells + "\n" + cluster.MethodVersion + "\n" + string(params)))
	return "clu-" + hex.EncodeToString(h[:10])
}

func (a *app) registerClusterings(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/clustering/defaults", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"parameters": cluster.DefaultParams(), "method": cluster.MethodVersion, "clip": cluster.ClipAt, "randomness": cluster.Randomness, "citation": clusteringCitation})
	})
	mux.HandleFunc("POST /api/local/{index}/cluster", a.startClustering)
	mux.HandleFunc("GET /api/clusterings/{id}/cells", a.clusteringCells)
	mux.HandleFunc("GET /api/clusterings/{id}/markers", a.clusteringMarkers)
}

// startClustering starts the job clustering a segmentation of a bundle's dataset, answering {id,
// artifact, job}: the artifact at once when it was made already.
func (a *app) startClustering(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var body clusterRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&body); err != nil || body.Segmentation == "" {
		writeError(w, http.StatusBadRequest, "Send {\"segmentation\": artifact id, \"filter\": optional QC filter, \"parameters\": optional}.")
		return
	}
	p := cluster.DefaultParams()
	if body.Parameters != nil {
		p = *body.Parameters
	}
	if err := p.Check(); err != nil {
		writeError(w, http.StatusBadRequest, "The clustering's parameters: "+err.Error()+".")
		return
	}
	if f := body.Filter; f != nil && (f.ID == "" || len(f.Checksum) != 64) {
		writeError(w, http.StatusBadRequest, "A filter needs its id, name, checksum and cells.")
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	path, name := b.Path, b.Name
	uuid := b.Experiment.AnalysisUUID
	b.Close()
	sm, err := a.segmentationOf(body.Segmentation, uuid)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Filter != nil {
		cells := 0
		var summary struct{ Cells int }
		if json.Unmarshal(sm.Summary, &summary) == nil {
			cells = summary.Cells
		}
		if _, err := body.Filter.keep(cells); err != nil && cells > 0 {
			writeError(w, http.StatusBadRequest, "The filter "+body.Filter.Name+": "+err.Error()+".")
			return
		}
	}
	id := clusteringID(sm.ID, body.Filter, p)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	spec := clusterSpec{Bundle: path, Segmentation: sm.ID, Filter: body.Filter, Parameters: p}
	job, _, err := a.jobs.Submit("clustering", "Clusters of "+sm.Name+" of "+name, "clustering\x00"+id, spec, a.clusteringJob(spec, id))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

// clusteringJob clusters a segmentation's cells and records the clustering.
func (a *app) clusteringJob(spec clusterSpec, id string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		b, err := xenium.Open(spec.Bundle, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		sm, err := a.segmentationOf(spec.Segmentation, b.Experiment.AnalysisUUID)
		if err != nil {
			return err
		}
		r.Log("info", "Clustering the cells of %s.", sm.Name)
		r.Progress(0, "Reading "+sm.Name)
		dir, err := a.segmentationFolder(ctx, b, sm)
		if err != nil {
			return err
		}
		counts, err := segment.ReadCounts(dir)
		if err != nil {
			return err
		}
		in := cluster.Inputs{Counts: counts}
		var filter *cluster.Filter
		if spec.Filter != nil {
			if in.Keep, err = spec.Filter.keep(len(counts.Offsets) - 1); err != nil {
				return fmt.Errorf("the filter %s: %w", spec.Filter.Name, err)
			}
			filter = &cluster.Filter{ID: spec.Filter.ID, Name: spec.Filter.Name, Checksum: spec.Filter.Checksum}
			r.Log("info", "The cells the filter %s keeps.", spec.Filter.Name)
		}
		res, err := cluster.Run(ctx, in, spec.Parameters, runtime.NumCPU(), func(f float64, message string) { r.Progress(0.05+0.85*f, message) })
		if err != nil {
			return err
		}
		r.Log("info", "%d cells analyzed (%d left out, %d without a gene transcript); %d genes; the median total %g.", len(res.Cells), res.Excluded, res.NoTranscript, len(res.Genes), res.Target)
		stage, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := cluster.Write(stage, cluster.Side{ID: sm.ID, Name: sm.Name}, filter, res); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Validating()
		r.Progress(0.95, "Checking the clustering")
		var parents []artifacts.Parent
		for _, p := range sm.Lineage.Parents {
			if p.Kind == "bundle" {
				parents = append(parents, p)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "segmentation", ID: sm.ID, Name: sm.Name})
		params, _ := json.Marshal(spec.Parameters)
		jobSpec, _ := json.Marshal(spec)
		var files []artifacts.File
		for _, f := range cluster.Files {
			files = append(files, artifacts.File{Path: f, Role: cluster.Roles[f]})
		}
		name := fmt.Sprintf("Clusters of %s, resolution %s", sm.Name, strconv.FormatFloat(spec.Parameters.Resolution, 'g', -1, 64))
		if filter != nil {
			name += ", filter " + filter.Name
		}
		m := artifacts.Manifest{
			ID:   id,
			Kind: "clustering",
			Name: name,
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "clustering", Spec: jobSpec},
				Method:     artifacts.Method{Name: "SituCraft clustering", Version: cluster.MethodVersion, Citation: clusteringCitation},
				Parameters: params,
				Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: files,
		}
		recorded, err := a.artifacts.Promote(m, cluster.Check)
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		if recorded.State == artifacts.Invalid {
			first := ""
			for _, f := range recorded.Findings {
				if f.Severity == "fatal" {
					first = ": " + f.Message
					break
				}
			}
			return fmt.Errorf("%w: the clustering's check found fatal problems%s (artifact %s, kept for review)", jobs.ErrInvalid, first, recorded.ID)
		}
		r.Log("info", "Recorded the clustering as artifact %s: %d clusters, modularity %.4f.", recorded.ID, len(res.Partition.Sizes), res.Partition.Modularity)
		return nil
	}
}

// clusteringOf reads a complete clustering's folder.
func (a *app) clusteringOf(w http.ResponseWriter, id string) (string, *cluster.Description, bool) {
	m, err := a.artifacts.Get(id)
	if err != nil || m.Kind != "clustering" || m.State != artifacts.Complete {
		writeError(w, http.StatusNotFound, "No such clustering.")
		return "", nil, false
	}
	p, err := a.artifacts.Path(m.ID, cluster.FileDescription)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", nil, false
	}
	dir := p[:len(p)-len(cluster.FileDescription)]
	d, err := cluster.ReadDescription(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return "", nil, false
	}
	return dir, d, true
}

// clusteringCells answers each of the segmentation's cells' cluster (−1: not analyzed), with the
// clusters' sizes: {cells, clusters, sizes, cluster}.
func (a *app) clusteringCells(w http.ResponseWriter, r *http.Request) {
	dir, d, ok := a.clusteringOf(w, r.PathValue("id"))
	if !ok {
		return
	}
	cells, clusters, err := cluster.ReadCells(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	out := make([]int32, d.Cells.Segmentation)
	for i := range out {
		out[i] = -1
	}
	for i, c := range cells {
		if int(c) < len(out) {
			out[c] = int32(clusters[i])
		}
	}
	writeJSON(w, map[string]any{"cells": d.Cells.Segmentation, "clusters": d.Clusters.Count, "sizes": d.Clusters.Sizes, "cluster": out})
}

// clusteringMarkers answers each cluster's leading markers (?top=, 10 by default, at most every
// gene): {genes, clusters: [{cluster, size, markers: [{gene, score, logfc, padj, pctIn, pctOut}]}]}.
func (a *app) clusteringMarkers(w http.ResponseWriter, r *http.Request) {
	dir, d, ok := a.clusteringOf(w, r.PathValue("id"))
	if !ok {
		return
	}
	mk, err := cluster.ReadMarkers(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pc, err := cluster.ReadPCA(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	top := 10
	if v, err := strconv.Atoi(r.URL.Query().Get("top")); err == nil && v > 0 {
		top = min(v, mk.Genes)
	}
	type group struct {
		Cluster int              `json:"cluster"`
		Size    int              `json:"size"`
		Markers []cluster.Marker `json:"markers"`
	}
	var groups []group
	for c := 0; c < mk.Clusters; c++ {
		groups = append(groups, group{Cluster: c, Size: d.Clusters.Sizes[c], Markers: cluster.TopMarkers(mk, pc.Genes, c, top)})
	}
	writeJSON(w, map[string]any{"genes": mk.Genes, "clusters": groups})
}
