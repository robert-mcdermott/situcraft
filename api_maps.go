package main

// Maps of a clustering's cells (internal/embed), made by a job and recorded as artifacts of kind
// "embedding"; the instrument's own analysis (its clusters and map) registered from the bundle as
// a clustering and a map of the instrument's segmentation; and a clustering's cells' expression,
// for coloring the map and the tissue by a gene, the dot plot and the markers of a selection.

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
	"runtime"
	"sort"
	"strings"
	"sync"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/embed"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

const embeddingCitation = "McInnes L, Healy J, Melville J. UMAP: Uniform Manifold Approximation and Projection for dimension reduction. arXiv:1802.03426, 2018; " +
	"Venna J, Kaski S. Neighborhood preservation in nonlinear projection methods: an experimental study. ICANN 2001:485–491 (trustworthiness); " +
	"Venna J, Peltonen J, Nybo K, Aidos H, Kaski S. Information retrieval perspective to nonlinear dimensionality reduction for data visualization. J Mach Learn Res 2010;11:451–490 (each cell's neighborhoods)"

const instrumentAnalysisCitation = "10x Genomics. Xenium Onboard Analysis: secondary analysis outputs (graph-based and k-means clustering, principal components, UMAP)"

type embedRequest struct {
	Clustering string        `json:"clustering"`
	Parameters *embed.Params `json:"parameters,omitempty"`
}

type embedSpec struct {
	Bundle     string       `json:"bundle"`
	Clustering string       `json:"clustering"`
	Parameters embed.Params `json:"parameters"`
}

func embeddingID(clustering string, p embed.Params) string {
	params, _ := json.Marshal(p)
	h := sha256.Sum256([]byte(clustering + "\n" + embed.MethodVersion + "\n" + string(params)))
	return "emb-" + hex.EncodeToString(h[:10])
}

func instrumentClusteringID(segmentation, name string) string {
	h := sha256.Sum256([]byte(segmentation + "\ninstrument " + name + "\n" + cluster.MethodVersion))
	return "clu-" + hex.EncodeToString(h[:10])
}

func instrumentMapID(segmentation string) string {
	h := sha256.Sum256([]byte(segmentation + "\ninstrument map\n" + embed.MethodVersion))
	return "emb-" + hex.EncodeToString(h[:10])
}

// expressions are clusterings' cells' expression, loaded for the Cell types view (the last few).
type expressions struct {
	mu    sync.Mutex
	order []string
	open  map[string]*cluster.Expression
	load  sync.Mutex
}

func (x *expressions) get(id string) *cluster.Expression {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.open[id]
}

func (x *expressions) put(id string, e *cluster.Expression) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.open == nil {
		x.open = map[string]*cluster.Expression{}
	}
	if _, ok := x.open[id]; !ok {
		x.order = append(x.order, id)
	}
	x.open[id] = e
	for len(x.order) > 4 {
		delete(x.open, x.order[0])
		x.order = x.order[1:]
	}
}

func (a *app) registerMaps(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/embedding/defaults", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"parameters": embed.DefaultParams(), "method": embed.MethodVersion, "citation": embeddingCitation})
	})
	mux.HandleFunc("POST /api/local/{index}/embed", a.startEmbedding)
	mux.HandleFunc("GET /api/embeddings/{id}/map", a.embeddingMap)
	mux.HandleFunc("GET /api/local/{index}/analysis", a.instrumentAnalysis)
	mux.HandleFunc("POST /api/local/{index}/analysis", a.registerInstrumentAnalysis)
	mux.HandleFunc("GET /api/local/{index}/clusterings/{id}/expression", a.clusteringExpression)
	mux.HandleFunc("GET /api/local/{index}/clusterings/{id}/dotplot", a.clusteringDotPlot)
	mux.HandleFunc("POST /api/local/{index}/clusterings/{id}/contrast", a.clusteringContrast)
}

// startEmbedding starts the job laying out a clustering's cells, answering {id, artifact, job}.
func (a *app) startEmbedding(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var body embedRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.Clustering == "" {
		writeError(w, http.StatusBadRequest, "Send {\"clustering\": artifact id, \"parameters\": optional}.")
		return
	}
	p := embed.DefaultParams()
	if body.Parameters != nil {
		p = *body.Parameters
	}
	if err := p.Check(); err != nil {
		writeError(w, http.StatusBadRequest, "The map's parameters: "+err.Error()+".")
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
	var s cluster.Summary
	json.Unmarshal(cm.Summary, &s)
	if s.Source != nil {
		writeError(w, http.StatusBadRequest, "A clustering from "+s.Source.Tool+" has no neighbors of SituCraft's to lay out; its map is the instrument's.")
		return
	}
	id := embeddingID(cm.ID, p)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	spec := embedSpec{Bundle: path, Clustering: cm.ID, Parameters: p}
	job, _, err := a.jobs.Submit("embedding", "Map of "+cm.Name, "embedding\x00"+id, spec, a.embeddingJob(spec, id))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

// clusteringFor is a complete clustering of the dataset whose bundle's analysis is uuid.
func (a *app) clusteringFor(id, uuid string) (artifacts.Manifest, error) {
	m, err := a.artifacts.Get(id)
	if err != nil || m.Kind != "clustering" || m.State != artifacts.Complete {
		return m, fmt.Errorf("no complete clustering %s", id)
	}
	for _, p := range m.Lineage.Parents {
		if p.Kind == "bundle" && p.ID == uuid {
			return m, nil
		}
	}
	return m, fmt.Errorf("%s is a clustering of another dataset", m.Name)
}

func (a *app) artifactFolder(id, file string) (string, error) {
	p, err := a.artifacts.Path(id, file)
	if err != nil {
		return "", err
	}
	return p[:len(p)-len(file)], nil
}

func (a *app) embeddingJob(spec embedSpec, id string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		cm, err := a.artifacts.Get(spec.Clustering)
		if err != nil {
			return err
		}
		dir, err := a.artifactFolder(cm.ID, cluster.FileDescription)
		if err != nil {
			return err
		}
		r.Log("info", "Laying out the cells of %s.", cm.Name)
		res, err := embed.Run(ctx, dir, cluster.Side{ID: cm.ID, Name: cm.Name}, spec.Parameters, runtime.NumCPU(), func(f float64, m string) { r.Progress(0.9*f, m) })
		if err != nil {
			return err
		}
		q := res.Description.Quality
		r.Log("info", "%d cells laid out (%s start, %d epochs); trustworthiness %.3f, the cells' neighborhoods kept with precision %.3f and recall %.3f.", res.Description.Cells, res.Description.Fitted.Init, res.Description.Fitted.Epochs, q.Trustworthiness, q.Precision, q.Recall)
		stage, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := embed.Write(stage, res); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Validating()
		var parents []artifacts.Parent
		for _, p := range cm.Lineage.Parents {
			if p.Kind == "bundle" || p.Kind == "segmentation" {
				parents = append(parents, p)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "clustering", ID: cm.ID, Name: cm.Name})
		params, _ := json.Marshal(spec.Parameters)
		jobSpec, _ := json.Marshal(spec)
		m := artifacts.Manifest{
			ID: id, Kind: "embedding", Name: "Map of " + cm.Name,
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "embedding", Spec: jobSpec},
				Method:     artifacts.Method{Name: "SituCraft UMAP", Version: embed.MethodVersion, Citation: embeddingCitation},
				Parameters: params,
				Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: filesOf(embed.Files, embed.Roles),
		}
		return a.promoteChecked(m, embed.Check, "map")
	}
}

func filesOf(names []string, roles map[string]string) []artifacts.File {
	var out []artifacts.File
	for _, f := range names {
		out = append(out, artifacts.File{Path: f, Role: roles[f]})
	}
	return out
}

// promoteChecked records an artifact, failing the job with the check's first fatal finding.
func (a *app) promoteChecked(m artifacts.Manifest, check artifacts.Check, what string) error {
	recorded, err := a.artifacts.Promote(m, check)
	if err != nil {
		a.artifacts.Discard(m.ID)
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
		return fmt.Errorf("%w: the %s's check found fatal problems%s (artifact %s, kept for review)", jobs.ErrInvalid, what, first, recorded.ID)
	}
	return nil
}

// embeddingMap answers a map as little-endian binary: the cells n (u32) and the segmentation's
// cells (u32), each cell's index in the segmentation (u32 × n), its place (f32 × 2n) and its
// reliability score (f32 × n).
func (a *app) embeddingMap(w http.ResponseWriter, r *http.Request) {
	m, err := a.artifacts.Get(r.PathValue("id"))
	if err != nil || m.Kind != "embedding" || m.State != artifacts.Complete {
		writeError(w, http.StatusNotFound, "No such map.")
		return
	}
	dir, err := a.artifactFolder(m.ID, embed.FileDescription)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mp, err := embed.ReadMap(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	segCells := 0
	for _, p := range m.Lineage.Parents {
		if p.Kind == "segmentation" {
			if sm, err := a.artifacts.Get(p.ID); err == nil {
				var s struct{ Cells int }
				json.Unmarshal(sm.Summary, &s)
				segCells = s.Cells
			}
		}
	}
	n := len(mp.Cells)
	out := make([]byte, 0, 8+16*n)
	out = binary.LittleEndian.AppendUint32(out, uint32(n))
	out = binary.LittleEndian.AppendUint32(out, uint32(segCells))
	for _, c := range mp.Cells {
		out = binary.LittleEndian.AppendUint32(out, c)
	}
	for _, v := range mp.XY {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
	}
	for _, v := range mp.Score {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
	}
	writeBinary(w, out, false)
}

// --- The instrument's analysis ---

// instrumentAnalysis lists the instrument's clusterings in the bundle, each with the artifact it
// is registered as, if it is: {source, clusterings: [{name, title, clusters, cells, artifact}],
// map: {artifact}}.
func (a *app) instrumentAnalysis(w http.ResponseWriter, r *http.Request) {
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	if !b.HasAnalysis() {
		writeJSON(w, map[string]any{"source": nil, "clusterings": []any{}})
		return
	}
	an, err := b.ReadAnalysis()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	seg := a.knownSegmentation(b)
	type item struct {
		Name     string `json:"name"`
		Title    string `json:"title"`
		Clusters int    `json:"clusters"`
		Cells    int    `json:"cells"`
		Artifact string `json:"artifact,omitempty"`
	}
	var list []item
	for _, c := range an.Clusterings {
		k := int32(0)
		for _, v := range c.Cluster {
			k = max(k, v)
		}
		it := item{Name: c.Name, Title: c.Title, Clusters: int(k), Cells: len(c.Barcodes)}
		if seg != nil {
			if m, err := a.artifacts.Get(instrumentClusteringID(seg.ID, c.Name)); err == nil && m.State == artifacts.Complete {
				it.Artifact = m.ID
			}
		}
		list = append(list, it)
	}
	out := map[string]any{"source": an.Source, "clusterings": list, "map": nil}
	if an.UMAP != nil && seg != nil {
		mapInfo := map[string]any{"cells": len(an.UMAP.Barcodes)}
		if m, err := a.artifacts.Get(instrumentMapID(seg.ID)); err == nil && m.State == artifacts.Complete {
			mapInfo["artifact"] = m.ID
		}
		out["map"] = mapInfo
	}
	writeJSON(w, out)
}

type analysisSpec struct {
	Bundle     string `json:"bundle"`
	Clustering string `json:"clustering"`
}

// registerInstrumentAnalysis starts the job registering one of the instrument's clusterings (and
// the instrument's map, the first time): {id, artifact, job}.
func (a *app) registerInstrumentAnalysis(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var body struct{ Clustering string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.Clustering == "" {
		writeError(w, http.StatusBadRequest, "Send {\"clustering\": the instrument's clustering's name, gene_expression_graphclust for one}.")
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	path, name := b.Path, b.Name
	seg := a.knownSegmentation(b)
	has := b.HasAnalysis()
	b.Close()
	if seg == nil {
		writeError(w, http.StatusConflict, "The instrument's segmentation of "+name+" is not registered yet.")
		return
	}
	if !has {
		writeError(w, http.StatusNotFound, name+" holds no analysis of the instrument's (analysis.tar.gz).")
		return
	}
	id := instrumentClusteringID(seg.ID, body.Clustering)
	if m, err := a.artifacts.Get(id); err == nil && m.State == artifacts.Complete {
		if _, err := a.artifacts.Get(instrumentMapID(seg.ID)); err == nil {
			writeJSON(w, map[string]any{"id": id, "artifact": m})
			return
		}
	}
	spec := analysisSpec{Bundle: path, Clustering: body.Clustering}
	job, _, err := a.jobs.Submit("analysis", "The instrument's clusters of "+name, "analysis\x00"+id, spec, a.analysisJob(spec))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

func (a *app) analysisJob(spec analysisSpec) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		b, err := xenium.Open(spec.Bundle, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		seg := a.knownSegmentation(b)
		if seg == nil {
			return errors.New("the instrument's segmentation of " + b.Name + " is not registered")
		}
		r.Progress(0.05, "Reading the instrument's analysis")
		an, err := b.ReadAnalysis()
		if err != nil {
			return err
		}
		var chosen *xenium.AnalysisClustering
		for i := range an.Clusterings {
			if an.Clusterings[i].Name == spec.Clustering {
				chosen = &an.Clusterings[i]
			}
		}
		if chosen == nil {
			names := []string{}
			for _, c := range an.Clusterings {
				names = append(names, c.Name)
			}
			return fmt.Errorf("the instrument's analysis has no clustering %s (it has %s)", spec.Clustering, strings.Join(names, ", "))
		}
		dir, err := a.segmentationFolder(ctx, b, *seg)
		if err != nil {
			return err
		}
		counts, err := segment.ReadCounts(dir)
		if err != nil {
			return err
		}
		segCells, err := segment.ReadCells(dir)
		if err != nil {
			return err
		}
		index := make(map[string]uint32, len(segCells))
		for i, c := range segCells {
			index[c.ID] = uint32(i)
		}
		place := func(barcodes []string) ([]uint32, []int, int) {
			var cells []uint32
			var rows []int
			unknown := 0
			for row, bc := range barcodes {
				if i, ok := index[bc]; ok {
					cells = append(cells, i)
					rows = append(rows, row)
				} else {
					unknown++
				}
			}
			return cells, rows, unknown
		}
		software := b.Experiment.AnalysisSoftware
		file := an.Source + "/analysis/clustering/" + chosen.Name + "/clusters.csv"
		cells, rows, unknown := place(chosen.Barcodes)
		if unknown > 0 {
			r.Log("warning", "%d of the instrument's clustered cells are not in its segmentation's cells and are left out.", unknown)
		}
		labels := make([]int32, len(rows))
		for i, row := range rows {
			labels[i] = chosen.Cluster[row] - 1
		}
		r.Progress(0.2, "Each of the instrument's clusters' markers")
		res, err := cluster.FromLabels(counts, cells, labels, cluster.Source{Tool: "Xenium Onboard Analysis", Version: software, File: file, Name: chosen.Name, Title: chosen.Title}, runtime.NumCPU())
		if err != nil {
			return err
		}
		segSide := cluster.Side{ID: seg.ID, Name: seg.Name}
		var bundleParents []artifacts.Parent
		for _, p := range seg.Lineage.Parents {
			if p.Kind == "bundle" {
				bundleParents = append(bundleParents, p)
			}
		}
		parents := append(append([]artifacts.Parent{}, bundleParents...), artifacts.Parent{Kind: "segmentation", ID: seg.ID, Name: seg.Name})
		id := instrumentClusteringID(seg.ID, chosen.Name)
		if _, err := a.artifacts.Get(id); err != nil {
			stage, err := a.artifacts.Stage(id)
			if err != nil {
				return err
			}
			if err := cluster.Write(stage, segSide, nil, res); err != nil {
				a.artifacts.Discard(id)
				return err
			}
			d, _ := cluster.ReadDescription(stage)
			params, _ := json.Marshal(map[string]string{"clustering": chosen.Name})
			jobSpec, _ := json.Marshal(spec)
			m := artifacts.Manifest{
				ID: id, Kind: "clustering", Name: "The instrument's " + strings.ToLower(chosen.Title[:1]) + chosen.Title[1:] + " clusters",
				Lineage: artifacts.Lineage{
					Parents:    parents,
					Job:        &artifacts.Job{ID: r.ID(), Kind: "analysis", Spec: jobSpec},
					Method:     artifacts.Method{Name: "Xenium Onboard Analysis " + chosen.Title + " clustering", Version: software, Citation: instrumentAnalysisCitation},
					Parameters: params,
					Software:   []artifacts.Software{{Name: "Xenium Onboard Analysis", Version: software}, {Name: "situcraft", Version: version, Commit: buildCommit()}},
				},
				Files: filesOf(cluster.FilesOf(d), cluster.Roles),
			}
			if err := a.promoteChecked(m, cluster.Check, "clustering"); err != nil {
				return err
			}
			r.Log("info", "Registered the instrument's %s clusters (%d of %d cells; markers by SituCraft) as artifact %s.", strings.ToLower(chosen.Title), len(res.Cells), len(segCells), id)
		}
		// The instrument's map, judged against its principal components.
		mapID := instrumentMapID(seg.ID)
		if _, err := a.artifacts.Get(mapID); err == nil || an.UMAP == nil || an.PCA == nil || an.UMAP.Dims != 2 {
			return nil
		}
		r.Progress(0.6, "Judging the instrument's map")
		pcaRow := make(map[string]int, len(an.PCA.Barcodes))
		for i, bc := range an.PCA.Barcodes {
			pcaRow[bc] = i
		}
		type placed struct {
			cell     uint32
			umap, pc int
		}
		var all []placed
		for row, bc := range an.UMAP.Barcodes {
			i, ok := index[bc]
			p, okp := pcaRow[bc]
			if ok && okp {
				all = append(all, placed{i, row, p})
			}
		}
		sort.Slice(all, func(x, y int) bool { return all[x].cell < all[y].cell })
		dims := an.PCA.Dims
		mapCells := make([]uint32, len(all))
		xy := make([]float64, 2*len(all))
		high := make([]float64, dims*len(all))
		for i, p := range all {
			mapCells[i] = p.cell
			xy[2*i], xy[2*i+1] = an.UMAP.Values[2*p.umap], an.UMAP.Values[2*p.umap+1]
			copy(high[i*dims:(i+1)*dims], an.PCA.Values[p.pc*dims:(p.pc+1)*dims])
		}
		src := embed.Source{Tool: "Xenium Onboard Analysis", Version: software, File: an.Source + "/analysis/umap/" + an.UMAP.Name + "/projection.csv", Space: fmt.Sprintf("the instrument's %d principal components (%s)", dims, an.PCA.Name)}
		mres, err := embed.FromSource(ctx, segSide, cluster.Side{}, src, mapCells, xy, high, dims, runtime.NumCPU())
		if err != nil {
			return err
		}
		stage, err := a.artifacts.Stage(mapID)
		if err != nil {
			return err
		}
		if err := embed.Write(stage, mres); err != nil {
			a.artifacts.Discard(mapID)
			return err
		}
		jobSpec, _ := json.Marshal(spec)
		mm := artifacts.Manifest{
			ID: mapID, Kind: "embedding", Name: "The instrument's map",
			Lineage: artifacts.Lineage{
				Parents:  parents,
				Job:      &artifacts.Job{ID: r.ID(), Kind: "analysis", Spec: jobSpec},
				Method:   artifacts.Method{Name: "Xenium Onboard Analysis UMAP", Version: software, Citation: instrumentAnalysisCitation},
				Software: []artifacts.Software{{Name: "Xenium Onboard Analysis", Version: software}, {Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: filesOf(embed.Files, embed.Roles),
		}
		if err := a.promoteChecked(mm, embed.Check, "map"); err != nil {
			return err
		}
		r.Log("info", "Registered the instrument's map of %d cells (trustworthiness %.3f against its principal components) as artifact %s.", len(mapCells), mres.Description.Quality.Trustworthiness, mapID)
		return nil
	}
}

// --- A clustering's cells' expression ---

// expressionOf loads (or answers the loaded) expression of a clustering of the request's bundle.
func (a *app) expressionOf(w http.ResponseWriter, r *http.Request) (*cluster.Expression, *cluster.Description, bool) {
	id := r.PathValue("id")
	cm, err := a.artifacts.Get(id)
	if err != nil || cm.Kind != "clustering" || cm.State != artifacts.Complete {
		writeError(w, http.StatusNotFound, "No such clustering.")
		return nil, nil, false
	}
	dir, err := a.artifactFolder(cm.ID, cluster.FileDescription)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, nil, false
	}
	d, err := cluster.ReadDescription(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return nil, nil, false
	}
	if e := a.exprs.get(cm.ID); e != nil {
		return e, d, true
	}
	a.exprs.load.Lock()
	defer a.exprs.load.Unlock()
	if e := a.exprs.get(cm.ID); e != nil {
		return e, d, true
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return nil, nil, false
	}
	defer b.Close()
	var segID string
	for _, p := range cm.Lineage.Parents {
		if p.Kind == "segmentation" {
			segID = p.ID
		}
	}
	sm, err := a.segmentationOf(segID, b.Experiment.AnalysisUUID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, nil, false
	}
	sdir, err := a.segmentationFolder(r.Context(), b, sm)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return nil, nil, false
	}
	counts, err := segment.ReadCounts(sdir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return nil, nil, false
	}
	cells, clusters, err := cluster.ReadCells(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return nil, nil, false
	}
	e, err := cluster.LoadExpression(counts, cells, clusters, d.Fixed.TargetSum)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return nil, nil, false
	}
	a.exprs.put(cm.ID, e)
	return e, d, true
}

func geneIndex(e *cluster.Expression, name string) int {
	for i, g := range e.Genes {
		if g == name {
			return i
		}
	}
	for i, g := range e.Genes {
		if strings.EqualFold(g, name) {
			return i
		}
	}
	return -1
}

// clusteringExpression answers a gene's log-normalized value in each of the segmentation's cells
// (f32, little-endian; NaN for a cell not analyzed).
func (a *app) clusteringExpression(w http.ResponseWriter, r *http.Request) {
	e, d, ok := a.expressionOf(w, r)
	if !ok {
		return
	}
	g := geneIndex(e, r.URL.Query().Get("gene"))
	if g < 0 {
		writeError(w, http.StatusNotFound, "No gene "+r.URL.Query().Get("gene")+" in the panel.")
		return
	}
	values := e.Values(g)
	all := make([]float32, d.Cells.Segmentation)
	for i := range all {
		all[i] = float32(math.NaN())
	}
	for i, c := range e.Cells {
		if int(c) < len(all) {
			all[c] = values[i]
		}
	}
	out := make([]byte, 0, 4*len(all))
	for _, v := range all {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
	}
	w.Header().Set("X-Gene", e.Genes[g])
	writeBinary(w, out, false)
}

// clusteringDotPlot answers each cluster's mean log-normalized value and share expressing for the
// genes asked (?genes=A,B,…): {genes, mean: [cluster][gene], share: [cluster][gene]}.
func (a *app) clusteringDotPlot(w http.ResponseWriter, r *http.Request) {
	e, d, ok := a.expressionOf(w, r)
	if !ok {
		return
	}
	var genes []int
	var names []string
	for _, name := range strings.Split(r.URL.Query().Get("genes"), ",") {
		if name = strings.TrimSpace(name); name == "" {
			continue
		}
		if g := geneIndex(e, name); g >= 0 {
			genes = append(genes, g)
			names = append(names, e.Genes[g])
		}
	}
	if len(genes) == 0 || len(genes) > 200 {
		writeError(w, http.StatusBadRequest, "Ask for 1 to 200 of the panel's genes: ?genes=EPCAM,PTPRC.")
		return
	}
	mean, share := e.Dot(genes, d.Clusters.Count)
	writeJSON(w, map[string]any{"genes": names, "mean": mean, "share": share})
}

// clusteringContrast answers the markers of a selection of a clustering's cells against the rest
// of its cells: {cells, selected, up: [markers higher in the selection], down: [lower]}, each the
// first 25 by score and said from the selection's side (exploratory).
func (a *app) clusteringContrast(w http.ResponseWriter, r *http.Request) {
	var body struct{ Cells string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Send {\"cells\": base64 of the cell count (u32) and a bit per cell of the segmentation}.")
		return
	}
	e, d, ok := a.expressionOf(w, r)
	if !ok {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(body.Cells)
	n := d.Cells.Segmentation
	if err != nil || len(raw) != 4+(n+7)/8 || int(binary.LittleEndian.Uint32(raw)) != n {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("The cells are not a bit for each of the segmentation's %d cells.", n))
		return
	}
	selected := make([]bool, len(e.Cells))
	count := 0
	for i, c := range e.Cells {
		if raw[4+c/8]>>(c%8)&1 == 1 {
			selected[i] = true
			count++
		}
	}
	mk, err := e.Contrast(selected, runtime.NumCPU())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	up := cluster.TopMarkers(mk, e.Genes, 0, 25)
	// The genes higher in the rest, said from the selection's side: lower in it (the score and
	// fold change negated, the shares swapped, all exactly).
	down := cluster.TopMarkers(mk, e.Genes, 1, 25)
	for i := range down {
		m := &down[i]
		m.Score, m.LogFC = -m.Score, -m.LogFC
		m.PctIn, m.PctOut = m.PctOut, m.PctIn
	}
	writeJSON(w, map[string]any{"cells": len(e.Cells), "selected": count, "up": up, "down": down})
}
