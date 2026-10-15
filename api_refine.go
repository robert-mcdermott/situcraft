package main

// SituCraft's transcript-informed segmentation of a bundle (internal/segment/refine), made by a
// job and recorded as a segmentation artifact, as nucleus expansion's is (api_expansion.go): its
// files written to a staging folder, checked (internal/segment, Check) and promoted. The artifact
// is named by what it starts from (the instrument's segmentation), the method's version and every
// setting that changes the result: the same bundle and settings give the same artifact. The page
// then compares it with the instrument's segmentation.
//
// The segmentation studio (requirements S3, S4, S5, S7, E5) asks for the presets and the
// settings' help, then for a spec's preflight (what it models, covers and costs, its problems and
// risks, and its recipe) before it starts the job.

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
	"sync"
	"time"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/segment/refine"
	"situcraft/internal/sysinfo"
	"situcraft/internal/xenium"
)

// refineSpec is a refinement job's spec: the bundle, the refinement, and how it runs.
type refineSpec struct {
	Bundle string `json:"bundle"`
	refine.Spec
	// Threads are the threads it runs on (0: every core); Force runs it past the computer's memory.
	Threads int  `json:"threads,omitempty"`
	Force   bool `json:"force,omitempty"`
}

// refineRequest is a request for a refinement or its preflight: a whole spec, or a preset (default
// Balanced) with what changes it (slice 4's shorthands among them), and how it runs.
type refineRequest struct {
	Spec      *refine.Spec    `json:"spec"`
	Preset    string          `json:"preset"`
	Options   json.RawMessage `json:"options"`
	Sweeps    *int            `json:"sweeps"`
	Samples   *int            `json:"samples"`
	Seed      *uint64         `json:"seed"`
	Layers    *int            `json:"layers"`
	Area      *refine.Area    `json:"area"`
	MinQV     *float64        `json:"min_qv"`
	SeedCheck *bool           `json:"seed_check"`
	GCPercent *int            `json:"gc_percent"`
	Threads   int             `json:"threads"`
	Force     bool            `json:"force"`
}

// refineSpecOf resolves a request to a checked spec.
func refineSpecOf(req refineRequest) (refine.Spec, error) {
	var spec refine.Spec
	if req.Spec != nil {
		spec = *req.Spec
	} else {
		preset := req.Preset
		if preset == "" {
			preset = refine.PresetBalanced
		}
		var err error
		if spec, err = refine.SpecOf(preset); err != nil {
			return spec, err
		}
		if len(req.Options) > 0 {
			dec := json.NewDecoder(strings.NewReader(string(req.Options)))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&spec.Options); err != nil {
				return spec, fmt.Errorf("the options: %v", err)
			}
		}
		last := &spec.Options.Stages[len(spec.Options.Stages)-1]
		if req.Sweeps != nil {
			if *req.Sweeps < 10 || *req.Sweeps > 100000 {
				return spec, errors.New("sweeps are from 10 to 100,000")
			}
			last.Sweeps = *req.Sweeps
			spec.Options.Samples = min(spec.Options.Samples, *req.Sweeps*5/6)
		}
		if req.Samples != nil {
			spec.Options.Samples = *req.Samples
		}
		if req.Seed != nil {
			spec.Options.Seed = *req.Seed
		}
		if req.Layers != nil {
			if *req.Layers < 1 || *req.Layers > 16 {
				return spec, errors.New("layers are from 1 to 16")
			}
			last.Layers = *req.Layers
		}
		if req.Area != nil {
			spec.Area = *req.Area
		}
		if req.MinQV != nil {
			spec.MinQV = *req.MinQV
		}
		if req.SeedCheck != nil {
			spec.SeedCheck = *req.SeedCheck
		}
		if req.GCPercent != nil {
			spec.GCPercent = *req.GCPercent
		}
	}
	if spec.Area.Mode == "" {
		spec.Area.Mode = refine.AreaWhole
	}
	spec.Resolve()
	if err := spec.Check(); err != nil {
		return spec, errors.New(strings.TrimPrefix(err.Error(), "refine: "))
	}
	if req.Threads < 0 || req.Threads > 4096 {
		return spec, errors.New("threads are from 1 to 4,096 (0: every core)")
	}
	return spec, nil
}

// refineID names the refinement of a segmentation with a spec: what changes the result (the
// settings, area, quality and seed check), not the preset's name, the threads or the memory
// target.
func refineID(instrument string, spec refine.Spec) string {
	what := struct {
		Options   refine.Options `json:"options"`
		Area      refine.Area    `json:"area"`
		MinQV     float64        `json:"min_qv"`
		SeedCheck bool           `json:"seed_check"`
	}{spec.Options, spec.Area, spec.MinQV, spec.SeedCheck}
	if what.Area.Mode == "" {
		what.Area.Mode = refine.AreaWhole
	}
	raw, _ := json.Marshal(what)
	h := sha256.Sum256([]byte(instrument + "\n" + refine.Method + " " + refine.MethodVersion + "\n" + string(raw)))
	return "ref-" + hex.EncodeToString(h[:10])
}

// refineSpecOfRecorded is the spec a refinement's recorded parameters (refine.Recorded) say it was
// made with.
func refineSpecOfRecorded(params json.RawMessage) refine.Spec {
	var rec refine.Recorded
	json.Unmarshal(params, &rec)
	return refine.Spec{Preset: rec.Preset, Area: rec.Area, MinQV: rec.MinQV, SeedCheck: rec.SecondSeed != 0,
		Options: refine.Options{Model: rec.Model, Sampler: rec.Sampler, Stages: rec.Stages, BlockUm: rec.BlockUm, Samples: rec.Samples, Seed: rec.Seed}}
}

// refineName is the artifact's name: the method, with its preset (other than Balanced) or what
// differs from the defaults, and what it covers.
func refineName(spec refine.Spec) string {
	def := refine.DefaultOptions()
	opt := spec.Options
	if len(opt.Stages) == 0 {
		opt.Stages = def.Stages
	}
	var parts []string
	if p, ok := refine.PresetByID(spec.Preset); ok && spec.Preset != refine.PresetBalanced && spec.Preset != refine.PresetExpert {
		parts = append(parts, p.Name)
	} else {
		last, dl := opt.Stages[len(opt.Stages)-1], def.Stages[len(def.Stages)-1]
		if len(opt.Stages) > 1 {
			parts = append(parts, fmt.Sprintf("%d stages", len(opt.Stages)))
		}
		if last.VoxelUm != dl.VoxelUm {
			parts = append(parts, fmt.Sprintf("%g µm voxels", last.VoxelUm))
		}
		if last.Sweeps != dl.Sweeps {
			parts = append(parts, fmt.Sprintf("%d sweeps", last.Sweeps))
		}
		if last.Layers != dl.Layers {
			parts = append(parts, fmt.Sprintf("%d layers", last.Layers))
		}
		if opt.Sampler.PerimeterFactor != def.Sampler.PerimeterFactor {
			parts = append(parts, fmt.Sprintf("perimeter bound %g", opt.Sampler.PerimeterFactor))
		}
		if opt.Model.NuclearPrior != def.Model.NuclearPrior {
			parts = append(parts, fmt.Sprintf("nuclear prior %g", opt.Model.NuclearPrior))
		}
		if !opt.Sampler.Reposition {
			parts = append(parts, "no drift")
		}
		if spec.Preset == refine.PresetExpert && len(parts) == 0 {
			parts = append(parts, "expert settings")
		}
	}
	switch spec.Area.Mode {
	case refine.AreaSections:
		parts = append(parts, "sections apart")
	case refine.AreaRegions:
		if n := len(spec.Area.Regions); n == 1 {
			r := spec.Area.Regions[0]
			parts = append(parts, fmt.Sprintf("region (%.0f, %.0f)–(%.0f, %.0f) µm", r.X0, r.Y0, r.X1, r.Y1))
		} else {
			parts = append(parts, fmt.Sprintf("%d regions", n))
		}
	}
	if spec.MinQV != 20 {
		parts = append(parts, fmt.Sprintf("Q%g or more", spec.MinQV))
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
	mux.HandleFunc("GET /api/refinement/presets", a.refinePresets)
	mux.HandleFunc("POST /api/local/{index}/refinement", a.startRefinement)
	mux.HandleFunc("POST /api/local/{index}/refinement/preflight", a.refinePreflight)
}

// refinePresets answers the presets, the settings' help and the default spec.
func (a *app) refinePresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"version": refine.PresetsVersion, "presets": refine.Presets(), "settings": refine.SettingsHelp(),
		"defaults": refine.DefaultSpec(), "method": refine.Method, "methodVersion": refine.MethodVersion, "citation": refine.Citation})
}

// readRefineRequest reads a request's body (empty: the defaults) and resolves its spec.
func readRefineRequest(w http.ResponseWriter, r *http.Request) (refineRequest, refine.Spec, bool) {
	var req refineRequest
	if r.ContentLength != 0 {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "The request is not a refinement's settings: "+err.Error())
			return req, refine.Spec{}, false
		}
	}
	spec, err := refineSpecOf(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "The settings: "+err.Error()+".")
		return req, spec, false
	}
	return req, spec, true
}

// startRefinement starts the job refining a bundle's segmentation (refineRequest), answering {id,
// artifact, job}: the artifact at once when it was made already.
func (a *app) startRefinement(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	req, spec, ok := readRefineRequest(w, r)
	if !ok {
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
	id := refineID(instrument.ID, spec)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	a.refineInputs.drop() // the job reads its own
	title := "Transcript-informed segmentation of " + name
	p := refineSpec{Bundle: path, Spec: spec, Threads: req.Threads, Force: req.Force}
	job, _, err := a.jobs.Submit("refinement", title, "refinement\x00"+path+"\x00"+id, p, a.refineJob(p))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

// refineInputs keeps the last bundle's inputs read for a preflight, so that changing a setting
// does not read the bundle again; they go after ten minutes unused, or when a refinement starts.
type refineInputs struct {
	mu    sync.Mutex
	key   string
	in    *refine.Inputs
	idx   *refine.BundleIndex
	err   error
	timer *time.Timer
}

const refineInputsKept = 10 * time.Minute

func (c *refineInputs) get(b *xenium.Bundle, key string, minQV float64) (*refine.Inputs, *refine.BundleIndex, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key = fmt.Sprintf("%s\x00%g", key, minQV)
	if c.key != key {
		c.key, c.in, c.idx, c.err = "", nil, nil, nil
		in, idx, err := refine.BundleInputs(b, minQV)
		c.key, c.in, c.idx, c.err = key, in, idx, err
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(refineInputsKept, c.drop)
	return c.in, c.idx, c.err
}

func (c *refineInputs) drop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.key, c.in, c.idx, c.err = "", nil, nil, nil
}

// refinePreflight answers a spec's preflight on a bundle (refine.Preflight) with the artifact it
// would make (id, and the artifact when it exists), its name, the threads, and its recipe.
func (a *app) refinePreflight(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	req, spec, ok := readRefineRequest(w, r)
	if !ok {
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	cores := req.Threads
	if cores <= 0 {
		cores = runtime.NumCPU()
	}
	memory, _ := sysinfo.Memory()
	free, err := xenium.FreeBytes(a.artifacts.Dir())
	if err != nil {
		free = -1
	}
	out := map[string]any{"threads": cores, "cores": runtime.NumCPU(), "name": refineName(spec)}
	instrument := a.knownSegmentation(b)
	var pre refine.Preflight
	if instrument == nil {
		pre = refine.Preflight{Spec: spec, MemoryBytes: memory, FreeBytes: free, Risks: []refine.Finding{}, Problems: []refine.Finding{{Class: refine.ClassPrior,
			Message: "The instrument's segmentation of " + b.Name + " is not registered yet: the refinement starts from its cells.",
			Remedy:  "Register it: the Segmentation view does so when it opens the bundle's segmentation."}}}
	} else {
		in, idx, err := a.refineInputs.get(b, instrument.ID, spec.MinQV)
		if err != nil {
			f := refine.Classify(err)
			if f == nil {
				f = &refine.Failed{Class: refine.ClassInput, Remedy: "Check the bundle in the Data view.", Err: err}
			}
			pre = refine.Preflight{Spec: spec, MemoryBytes: memory, FreeBytes: free, Risks: []refine.Finding{},
				Problems: []refine.Finding{{Class: f.Class, Message: "The bundle cannot be refined: " + f.Error() + ".", Remedy: f.Remedy}}}
		} else {
			pre = refine.Check(in, idx, spec, cores, memory, free)
		}
		id := refineID(instrument.ID, spec)
		out["id"] = id
		if m, err := a.artifacts.Get(id); err == nil {
			out["artifact"] = m
		}
	}
	out["preflight"] = pre
	out["recipe"] = newRecipe(b, pre.Spec)
	writeJSON(w, out)
}

// refineJob refines a bundle's segmentation and records it.
func (a *app) refineJob(p refineSpec) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		b, err := xenium.Open(p.Bundle, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		instrument := a.knownSegmentation(b)
		if instrument == nil {
			return &refine.Failed{Class: refine.ClassPrior, Remedy: "Open the bundle's segmentation in the Segmentation view, which registers it, and start the refinement again.",
				Err: errors.New("the instrument's segmentation of " + b.Name + " is not registered (its files changed, or it was never registered)")}
		}
		id := refineID(instrument.ID, p.Spec)
		dir, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		d, _, err := refine.Refine(ctx, b, p.Spec, dir, refine.Job{Workers: p.Threads, Force: p.Force,
			Progress: func(f float64, message string) { r.Progress(f, message) },
			Log:      func(level, format string, args ...any) { r.Log(level, format, args...) }})
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Validating()
		r.Progress(0.92, "Checking the segmentation")
		files := []artifacts.File{}
		for _, name := range []string{segment.FileDescription, segment.FileCells, segment.FileMask, segment.FileAssignments, segment.FileCounts, segment.FileOutlines,
			segment.FileProbabilities, segment.FileDiagnostics, segment.FileLayers} {
			if name == segment.FileLayers && p.Options.Stages[len(p.Options.Stages)-1].Layers == 1 {
				continue
			}
			files = append(files, artifacts.File{Path: name, Role: segment.Roles[name]})
		}
		var parents []artifacts.Parent
		for _, pa := range instrument.Lineage.Parents {
			if pa.Kind == "bundle" {
				parents = append(parents, pa)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "segmentation", ID: instrument.ID, Name: instrument.Name})
		spec, _ := json.Marshal(p)
		m := artifacts.Manifest{
			ID:   id,
			Kind: "segmentation",
			Name: refineName(p.Spec),
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
			if f := refine.Classify(err); f != nil {
				return f
			}
			return err
		}
		if recorded.State == artifacts.Invalid {
			return fmt.Errorf("%w: the check found fatal problems in the segmentation (artifact %s, kept for review)", jobs.ErrInvalid, recorded.ID)
		}
		r.Log("info", "Recorded the segmentation as artifact %s: %d cells, %d files by SHA-256, %d findings.", recorded.ID, d.Cells, len(recorded.Files), len(recorded.Findings))
		return nil
	}
}

// refineOf reads a refinement job's spec: slice 4's ({bundle, options}) as the whole tissue's,
// its transcripts of quality 20 or more.
func refineOf(spec json.RawMessage) (refineSpec, error) {
	p := refineSpec{Spec: refine.Spec{MinQV: 20, Area: refine.Area{Mode: refine.AreaWhole}}}
	if json.Unmarshal(spec, &p) != nil || p.Bundle == "" {
		return p, errors.New("its spec names no bundle")
	}
	if err := p.Spec.Check(); err != nil {
		return p, err
	}
	return p, nil
}
