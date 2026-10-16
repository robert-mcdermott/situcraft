package main

// Labels from a reference (internal/label, reference.go): a labeled single-cell reference or a
// table of label profiles read and shown against a clustering's panel, and the clustering's cells
// mapped to it by a job, recorded as an artifact of kind "labels".

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/jobs"
	"situcraft/internal/label"
	"situcraft/internal/xenium"
)

const referenceCitation = "Aran D, et al. Reference-based analysis of lung single-cell sequencing reveals a transitional profibrotic macrophage. Nat Immunol 2019;20:163–172. doi:10.1038/s41590-018-0276-y (correlation with each label's profile, fine-tuned on the leading labels' genes)"

// referenceRequest names a reference (its path: only this computer may name one), what to read of
// it, the clustering whose cells to map, and the mapping's parameters.
type referenceRequest struct {
	label.RefSpec
	Clustering string           `json:"clustering"`
	Parameters *label.RefParams `json:"parameters,omitempty"`
}

type referenceSpec struct {
	Bundle     string          `json:"bundle"`
	Clustering string          `json:"clustering"`
	Reference  label.RefSpec   `json:"reference"`
	SHA256     string          `json:"sha256"`
	Parameters label.RefParams `json:"parameters"`
}

func referenceLabelsID(clustering, sha string, spec label.RefSpec, p label.RefParams) string {
	params, _ := json.Marshal(p)
	what, _ := json.Marshal(label.RefSpec{Matrix: spec.Matrix, LabelColumn: spec.LabelColumn, GeneColumn: spec.GeneColumn})
	h := sha256.Sum256([]byte("reference\n" + clustering + "\n" + sha + "\n" + string(what) + "\n" + string(params) + "\n" + label.MethodRef))
	return "lab-" + hex.EncodeToString(h[:10])
}

func (a *app) registerReferences(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/references/defaults", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"parameters": label.DefaultRefParams(), "method": label.MethodRef, "citation": referenceCitation, "maxLabels": label.MaxReferenceLabels})
	})
	mux.HandleFunc("POST /api/local/{index}/references/read", a.readReference)
	mux.HandleFunc("POST /api/local/{index}/references/map", a.mapReference)
}

// referenceRequestOf decodes a request and the clustering's panel genes, refusing a path from
// another computer.
func (a *app) referenceRequestOf(w http.ResponseWriter, r *http.Request) (referenceRequest, artifacts.Manifest, []string, string, bool) {
	var body referenceRequest
	var cm artifacts.Manifest
	if !a.needCache(w) {
		return body, cm, nil, "", false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.Path == "" || body.Clustering == "" {
		writeError(w, http.StatusBadRequest, "Send {\"path\", \"clustering\", and optionally \"matrix\", \"labelColumn\", \"geneColumn\", \"parameters\"}.")
		return body, cm, nil, "", false
	}
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may name files to read.")
		return body, cm, nil, "", false
	}
	abs, err := filepath.Abs(strings.TrimSpace(body.Path))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return body, cm, nil, "", false
	}
	body.Path = abs
	b, ok := a.openBundle(w, r)
	if !ok {
		return body, cm, nil, "", false
	}
	bundle, uuid := b.Path, b.Experiment.AnalysisUUID
	b.Close()
	if cm, err = a.clusteringFor(body.Clustering, uuid); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return body, cm, nil, "", false
	}
	dir, err := a.artifactFolder(cm.ID, cluster.FileDescription)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return body, cm, nil, "", false
	}
	pc, err := cluster.ReadPCA(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return body, cm, nil, "", false
	}
	return body, cm, pc.Genes, bundle, true
}

// referencePreview is a reference as it would be compared: its labels and their reference cells,
// the panel genes it shares and lacks, what its values are, and the genes per pair and compared.
type referencePreview struct {
	label.Ref
	Spec     label.RefSpec `json:"spec"`
	Shared   int           `json:"shared"`
	PerPair  int           `json:"perPair"`
	Compared int           `json:"compared"`
}

// readReference says what a reference holds and, read as asked (else as suggested), how it meets
// the clustering's panel: {description, preview, error (why it could not be read as asked)}.
func (a *app) readReference(w http.ResponseWriter, r *http.Request) {
	body, _, panel, _, ok := a.referenceRequestOf(w, r)
	if !ok {
		return
	}
	d, err := label.DescribeReference(body.Path, panel)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "reference.unreadable", err.Error())
		return
	}
	spec := d.Suggested
	if body.LabelColumn != "" || body.GeneColumn != "" || body.Matrix != "" {
		spec = body.RefSpec
	}
	out := map[string]any{"description": d, "preview": nil}
	ref, err := label.ReadReference(spec, panel)
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, out)
		return
	}
	p := label.DefaultRefParams()
	if body.Parameters != nil {
		p = *body.Parameters
	}
	pv := referencePreview{Ref: *ref, Spec: spec, Shared: len(ref.Genes), PerPair: p.MarkersPer(len(ref.Labels))}
	if mp, err := label.NewMapper(ref, len(panel), p); err == nil {
		pv.Compared = len(mp.Compared())
	} else {
		out["error"] = err.Error()
	}
	out["preview"] = pv
	writeJSON(w, out)
}

// mapReference starts the job mapping a clustering's cells to a reference, answering {id,
// artifact, job}: the artifact at once when it was made already.
func (a *app) mapReference(w http.ResponseWriter, r *http.Request) {
	body, cm, panel, bundle, ok := a.referenceRequestOf(w, r)
	if !ok {
		return
	}
	p := label.DefaultRefParams()
	if body.Parameters != nil {
		p = *body.Parameters
	}
	if err := p.Check(); err != nil {
		writeError(w, http.StatusBadRequest, "The mapping's parameters: "+err.Error()+".")
		return
	}
	ref, err := label.ReadReference(body.RefSpec, panel)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "reference.refused", err.Error())
		return
	}
	if _, err := label.NewMapper(ref, len(panel), p); err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "reference.refused", err.Error())
		return
	}
	id := referenceLabelsID(cm.ID, ref.SHA256, body.RefSpec, p)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	spec := referenceSpec{Bundle: bundle, Clustering: cm.ID, Reference: body.RefSpec, SHA256: ref.SHA256, Parameters: p}
	job, _, err := a.jobs.Submit("label-reference", "Labels of "+cm.Name+" from "+ref.File, "label-reference\x00"+id, spec, a.referenceJob(spec, id))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

func (a *app) referenceJob(spec referenceSpec, id string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
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
		r.Progress(0.05, "Reading the cells' expression")
		e, err := a.loadExpression(ctx, b, cm, dir, d)
		if err != nil {
			return err
		}
		r.Progress(0.2, "Reading the reference")
		start := time.Now()
		ref, err := label.ReadReference(spec.Reference, e.Genes)
		if err != nil {
			return err
		}
		if ref.SHA256 != spec.SHA256 {
			return fmt.Errorf("%s changed after it was read: read it again", ref.File)
		}
		read := time.Since(start).Seconds()
		r.Log("info", "%s: %d labels; %d of the panel's %d genes shared (%s).", ref.File, len(ref.Labels), len(ref.Genes), len(e.Genes), ref.Values)
		r.Progress(0.35, "Correlating the cells with the reference's labels")
		start = time.Now()
		res, err := label.MapReference(ctx, e, ref, spec.Parameters, d.Clusters.Count, d.Fixed.TargetSum, runtime.NumCPU())
		if err != nil {
			return err
		}
		res.Seconds = map[string]float64{"reference": math.Round(read*100) / 100, "mapping": math.Round(time.Since(start).Seconds()*100) / 100}
		labeled := 0
		for _, k := range res.Label {
			if k >= 0 {
				labeled++
			}
		}
		r.Log("info", "%d of %d cells labeled from %s.", labeled, len(res.Cells), ref.File)
		stage, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := label.WriteReference(stage, d.Segmentation, cluster.Side{ID: cm.ID, Name: cm.Name}, res); err != nil {
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
		params, _ := json.Marshal(map[string]any{"parameters": spec.Parameters, "reference": map[string]any{"file": ref.File, "sha256": ref.SHA256, "format": ref.Format, "matrix": ref.Matrix, "labelColumn": ref.LabelColumn, "geneColumn": ref.GeneColumn}})
		jobSpec, _ := json.Marshal(spec)
		m := artifacts.Manifest{
			ID: id, Kind: "labels", Name: "Labels of " + cm.Name + " from " + ref.File,
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "label-reference", Spec: jobSpec},
				Method:     artifacts.Method{Name: "SituCraft labels from a reference", Version: label.MethodRef, Citation: referenceCitation},
				Parameters: params,
				Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: filesOf(label.FilesOf(&label.Description{Reference: &label.RefInfo{}}), label.Roles),
		}
		return a.promoteChecked(m, label.Check, "label set")
	}
}

// referenceJobOf restores a mapping job from its spec.
func (a *app) referenceJobOf(spec json.RawMessage) (referenceSpec, string, error) {
	var p referenceSpec
	if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || p.Clustering == "" || p.SHA256 == "" || p.Parameters.Check() != nil {
		return p, "", errors.New("its spec names no bundle, clustering, reference and parameters")
	}
	return p, referenceLabelsID(p.Clustering, p.SHA256, p.Reference, p.Parameters), nil
}
