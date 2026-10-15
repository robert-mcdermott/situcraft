package main

// SituCraft's transcript-informed segmentation of a bundle (internal/segment/refine), made by a
// job and recorded as a segmentation artifact, as nucleus expansion's is (api_expansion.go): its
// files written to a staging folder, checked (internal/segment, Check) and promoted. The artifact
// is named by what it starts from (the instrument's segmentation), the method's version and every
// option: the same bundle and options give the same artifact. The page then compares it with the
// instrument's segmentation.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/segment/refine"
	"situcraft/internal/xenium"
)

// refineSpec is a refinement job's spec: the bundle and the options (every one, after defaults).
type refineSpec struct {
	Bundle  string         `json:"bundle"`
	Options refine.Options `json:"options"`
}

// refineRequest are the options a request may set; the rest are the defaults.
type refineRequest struct {
	Sweeps  *int    `json:"sweeps"`
	Samples *int    `json:"samples"`
	Seed    *uint64 `json:"seed"`
	Layers  *int    `json:"layers"`
}

// refineOptions are the defaults with a request's options, checked.
func refineOptions(req refineRequest) (refine.Options, error) {
	opt := refine.DefaultOptions()
	last := &opt.Stages[len(opt.Stages)-1]
	if req.Sweeps != nil {
		if *req.Sweeps < 10 || *req.Sweeps > 100000 {
			return opt, errors.New("sweeps are from 10 to 100,000")
		}
		last.Sweeps = *req.Sweeps
		opt.Samples = min(opt.Samples, *req.Sweeps*5/6)
	}
	if req.Samples != nil {
		opt.Samples = *req.Samples
	}
	if req.Seed != nil {
		opt.Seed = *req.Seed
	}
	if req.Layers != nil {
		if *req.Layers < 1 || *req.Layers > 16 {
			return opt, errors.New("layers are from 1 to 16")
		}
		last.Layers = *req.Layers
	}
	if err := opt.Check(); err != nil {
		return opt, errors.New(strings.TrimPrefix(err.Error(), "refine: "))
	}
	return opt, nil
}

// refineID names the refinement of a segmentation with options.
func refineID(instrument string, opt refine.Options) string {
	raw, _ := json.Marshal(opt)
	h := sha256.Sum256([]byte(instrument + "\n" + refine.Method + " " + refine.MethodVersion + "\n" + string(raw)))
	return "ref-" + hex.EncodeToString(h[:10])
}

// refineName is the artifact's name: the method, with what differs from the defaults.
func refineName(opt refine.Options) string {
	def := refine.DefaultOptions()
	var parts []string
	last, dl := opt.Stages[len(opt.Stages)-1], def.Stages[len(def.Stages)-1]
	if last.Sweeps != dl.Sweeps {
		parts = append(parts, fmt.Sprintf("%d sweeps", last.Sweeps))
	}
	if last.Layers != dl.Layers {
		parts = append(parts, fmt.Sprintf("%d layers", last.Layers))
	}
	if opt.Seed != def.Seed {
		parts = append(parts, fmt.Sprintf("seed %d", opt.Seed))
	}
	if len(parts) == 0 {
		return "Transcript-informed segmentation"
	}
	return "Transcript-informed segmentation (" + strings.Join(parts, ", ") + ")"
}

func (a *app) registerRefine(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/refinement", a.startRefinement)
}

// startRefinement starts the job refining a bundle's segmentation with {sweeps, samples, seed,
// layers} (each optional), answering {id, artifact, job}: the artifact at once when it was made
// already.
func (a *app) startRefinement(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var req refineRequest
	if r.ContentLength != 0 {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "The request is not the refinement's options: "+err.Error())
			return
		}
	}
	opt, err := refineOptions(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "The options: "+err.Error()+".")
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	instrument := a.knownSegmentation(b)
	path, name := b.Path, b.Name
	b.Close()
	if instrument == nil {
		writeError(w, http.StatusConflict, "The instrument's segmentation of "+name+" is not registered yet: the refinement starts from its cells.")
		return
	}
	id := refineID(instrument.ID, opt)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	title := "Transcript-informed segmentation of " + name
	job, _, err := a.jobs.Submit("refinement", title, "refinement\x00"+path+"\x00"+id, refineSpec{path, opt}, a.refineJob(path, opt))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

// refineJob refines a bundle's segmentation and records it.
func (a *app) refineJob(path string, opt refine.Options) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		b, err := xenium.Open(path, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		instrument := a.knownSegmentation(b)
		if instrument == nil {
			return errors.New("the instrument's segmentation of " + b.Name + " is not registered (its files changed, or it was never registered)")
		}
		id := refineID(instrument.ID, opt)
		r.Progress(0, "Reading the transcripts, cells and nuclei")
		in, idx, err := refine.BundleInputs(b)
		if err != nil {
			return err
		}
		r.Log("info", "%d gene transcripts of quality 20 or more, %d cells to start from, %d genes.", len(idx.Rows), in.Cells, in.Genes)
		opt.Workers = runtime.NumCPU()
		res, err := refine.Run(ctx, in, opt, func(f float64, message string) {
			r.Progress(0.05+0.8*f, message)
		})
		if err != nil {
			return err
		}
		if left := in.Cells - len(res.Kept); left > 0 {
			r.Log("warning", "%d cells could not be kept: their nuclei share a voxel with another's at the finest stage.", left)
		}
		dir, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		r.Progress(0.86, "Writing the segmentation")
		d, diag, err := refine.WriteBundleOutputs(b, in, idx, res, opt, dir)
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		s := diag.Summary
		r.Log("info", "%d cells; %.1f%% of modelled transcripts in the same cell over both halves of the last %d sweeps; mean assignment probability %.3f.", d.Cells, 100*s.SplitHalf, s.Samples, s.MeanProbability)
		r.Validating()
		r.Progress(0.92, "Checking the segmentation")
		files := []artifacts.File{}
		for _, name := range []string{segment.FileDescription, segment.FileCells, segment.FileMask, segment.FileAssignments, segment.FileCounts, segment.FileOutlines,
			segment.FileProbabilities, segment.FileDiagnostics, segment.FileLayers} {
			if name == segment.FileLayers && opt.Stages[len(opt.Stages)-1].Layers == 1 {
				continue
			}
			files = append(files, artifacts.File{Path: name, Role: segment.Roles[name]})
		}
		var parents []artifacts.Parent
		for _, p := range instrument.Lineage.Parents {
			if p.Kind == "bundle" {
				parents = append(parents, p)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "segmentation", ID: instrument.ID, Name: instrument.Name})
		spec, _ := json.Marshal(refineSpec{path, opt})
		m := artifacts.Manifest{
			ID:   id,
			Kind: "segmentation",
			Name: refineName(opt),
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "refinement", Spec: spec},
				Method:     artifacts.Method{Name: refine.Method, Version: refine.MethodVersion, Citation: refine.Citation},
				Parameters: d.Parameters,
				Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: files,
		}
		recorded, err := a.artifacts.Promote(m, segment.Check)
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		if recorded.State == artifacts.Invalid {
			return fmt.Errorf("%w: the check found fatal problems in the segmentation (artifact %s, kept for review)", jobs.ErrInvalid, recorded.ID)
		}
		r.Log("info", "Recorded the segmentation as artifact %s: %d cells, %d files by SHA-256, %d findings.", recorded.ID, d.Cells, len(recorded.Files), len(recorded.Findings))
		return nil
	}
}

// refineOf reads a refinement job's spec.
func refineOf(spec json.RawMessage) (refineSpec, error) {
	var p refineSpec
	if json.Unmarshal(spec, &p) != nil || p.Bundle == "" {
		return p, errors.New("its spec names no bundle")
	}
	if err := p.Options.Check(); err != nil {
		return p, err
	}
	return p, nil
}
