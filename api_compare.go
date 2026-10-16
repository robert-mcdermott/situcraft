package main

// Comparisons of two segmentations of a dataset (internal/compare), made by a job and recorded as
// artifacts of kind "comparison". Each segmentation is read as SituCraft's segmentation files: its
// artifact's own (an expansion, an import), or, for the instrument's, written once from the bundle
// into the cache. So is the co-expression reference: the dataset's nuclei, and a coarse 10 µm
// expansion of them. A comparison is named by the two segmentations, the method's version and its
// parameters: asking again answers it without a job.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"situcraft/internal/artifacts"
	"situcraft/internal/compare"
	"situcraft/internal/jobs"
	"situcraft/internal/segment/expand"
	"situcraft/internal/segment/imports"
	"situcraft/internal/xenium"
)

// coarseExpansionUm is the coarse expansion of the nuclei that finds the spurious gene pairs
// (internal/compare, coexpression.go).
const coarseExpansionUm = 10

type compareSpec struct {
	Bundle string `json:"bundle"`
	A      string `json:"a"`
	B      string `json:"b"`
}

func comparisonID(a, b string, p compare.Params) string {
	params, _ := json.Marshal(p)
	h := sha256.Sum256([]byte(a + "\n" + b + "\n" + compare.MethodVersion + "\n" + string(params)))
	return "cmp-" + hex.EncodeToString(h[:10])
}

func (a *app) registerComparisons(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/compare", a.startComparison)
	mux.HandleFunc("POST /api/artifacts/{id}/trash", a.trashComparison)
}

// trashComparison moves a comparison's files into the library's trash (trash/artifacts) once no
// saved project names it: the page asks after removing it from its project and saving. While a
// project records it, or a decision cites it, it stays. A comparison can always be made again.
func (a *app) trashComparison(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may remove files.")
		return
	}
	m, err := a.artifacts.Get(r.PathValue("id"))
	if errors.Is(err, artifacts.ErrNotFound) {
		writeJSON(w, map[string]any{"trashed": false, "missing": true})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if m.Kind != "comparison" {
		writeError(w, http.StatusBadRequest, "Only a comparison's files are moved to the trash this way.")
		return
	}
	users, err := a.store.projectsNaming(m.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(users) > 0 {
		writeJSON(w, map[string]any{"trashed": false, "usedBy": users})
		return
	}
	to, err := a.artifacts.Trash(m.ID, filepath.Join(a.store.dir, "trash", "artifacts"), time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"trashed": true, "trash": to})
}

// startComparison starts the job comparing segmentations {a, b} (artifact ids) of a bundle's
// dataset, answering {id, artifact, job}: the artifact at once when it was made already.
func (a *app) startComparison(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var body struct{ A, B string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.A == "" || body.B == "" {
		writeError(w, http.StatusBadRequest, "Send {\"a\": artifact id, \"b\": artifact id}.")
		return
	}
	if body.A == body.B {
		writeError(w, http.StatusBadRequest, "Compare two different segmentations.")
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	path, name := b.Path, b.Name
	instrument := a.knownSegmentation(b)
	uuid := b.Experiment.AnalysisUUID
	b.Close()
	if instrument == nil {
		writeError(w, http.StatusConflict, "The instrument's segmentation of "+name+" is not registered yet.")
		return
	}
	for _, id := range []string{body.A, body.B} {
		if _, err := a.segmentationOf(id, uuid); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	id := comparisonID(body.A, body.B, compare.DefaultParams())
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	job, _, err := a.jobs.Submit("comparison", "Comparison of two segmentations of "+name, "comparison\x00"+path+"\x00"+id, compareSpec{path, body.A, body.B}, a.comparisonJob(path, body.A, body.B))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

// segmentationOf is a complete segmentation artifact of the dataset whose bundle's analysis is uuid.
func (a *app) segmentationOf(id, uuid string) (artifacts.Manifest, error) {
	m, err := a.artifacts.Get(id)
	if err != nil {
		return m, fmt.Errorf("no segmentation %s", id)
	}
	if m.Kind != "segmentation" || m.State != artifacts.Complete {
		return m, fmt.Errorf("%s is not a complete segmentation", id)
	}
	for _, p := range m.Lineage.Parents {
		if p.Kind == "bundle" && p.ID == uuid {
			return m, nil
		}
	}
	return m, fmt.Errorf("%s is a segmentation of another dataset", m.Name)
}

// isBundleOwn: a segmentation whose files are all its bundle's (the instrument's).
func isBundleOwn(m artifacts.Manifest) bool {
	for _, f := range m.Files {
		if f.Location != artifacts.InBundle {
			return false
		}
	}
	return len(m.Files) > 0
}

// segmentationFolder is the folder of a segmentation's files: its artifact's, or for the
// instrument's, written into the cache from the bundle.
func (a *app) segmentationFolder(ctx context.Context, b *xenium.Bundle, m artifacts.Manifest) (string, error) {
	if !isBundleOwn(m) {
		p, err := a.artifacts.Path(m.ID, compare.FileDescription)
		return filepath.Dir(p), err
	}
	return a.cache.Derived("segmentation", m.ID+"/instrument", func(dir string) error {
		_, err := imports.Instrument(ctx, b, dir, runtime.NumCPU())
		return err
	})
}

// comparisonJob compares two segmentations and records the comparison.
func (a *app) comparisonJob(path, idA, idB string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		b, err := xenium.Open(path, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		instrument := a.knownSegmentation(b)
		if instrument == nil {
			return errors.New("the instrument's segmentation of " + b.Name + " is not registered")
		}
		uuid := b.Experiment.AnalysisUUID
		ma, err := a.segmentationOf(idA, uuid)
		if err != nil {
			return err
		}
		mb, err := a.segmentationOf(idB, uuid)
		if err != nil {
			return err
		}
		p := compare.DefaultParams()
		id := comparisonID(idA, idB, p)
		r.Log("info", "Comparing %s (A) with %s (B).", ma.Name, mb.Name)

		load := func(step float64, message string, folder func() (string, error)) (*compare.Seg, error) {
			r.Progress(step, message)
			dir, err := folder()
			if err != nil {
				return nil, err
			}
			return compare.Load(dir)
		}
		segA, err := load(0.05, "Reading "+ma.Name, func() (string, error) { return a.segmentationFolder(ctx, b, ma) })
		if err != nil {
			return err
		}
		segB, err := load(0.15, "Reading "+mb.Name, func() (string, error) { return a.segmentationFolder(ctx, b, mb) })
		if err != nil {
			return err
		}
		grown := func(um float64) func() (string, error) {
			return func() (string, error) {
				return a.cache.Derived("segmentation", fmt.Sprintf("%s/expansion/%g", instrument.ID, um), func(dir string) error {
					_, err := expand.FromBundle(ctx, b, dir, expand.Params{DistanceUm: um}, runtime.NumCPU(), nil)
					return err
				})
			}
		}
		nuclear, err := load(0.25, "Reading the nuclei", grown(0))
		if err != nil {
			return err
		}
		coarse, err := load(0.35, fmt.Sprintf("Growing the nuclei by %d µm for the co-expression reference", coarseExpansionUm), grown(coarseExpansionUm))
		if err != nil {
			return err
		}
		r.Progress(0.45, "Reading the transcripts and nuclei")
		nuclei, err := expand.NucleusRaster(b)
		if err != nil {
			return err
		}
		transcripts, err := compare.BundleTranscripts(b)
		if err != nil {
			return err
		}
		r.Progress(0.55, "Finding the spurious gene pairs")
		ref := compare.NewReference(nuclear.Counts, coarse.Counts, p)
		r.Log("info", "%d spurious gene pairs, from %d nuclei and %d cells of the coarse expansion with %d transcripts or more.", len(ref.Pairs), ref.NuclearCells, ref.CoarseCells, p.Depth)
		r.Progress(0.65, "Matching the cells")
		in := compare.Inputs{A: segA, B: segB, Nuclei: nuclei, Transcripts: transcripts, Reference: ref, Params: p}
		in.RuntimeA, in.SizeA = a.costOf(ma)
		in.RuntimeB, in.SizeB = a.costOf(mb)
		res, err := compare.Compare(ctx, in)
		if err != nil {
			return err
		}
		dir, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := compare.Write(dir, compare.Side{ID: ma.ID, Name: ma.Name}, compare.Side{ID: mb.ID, Name: mb.Name}, res); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Validating()
		r.Progress(0.95, "Checking the comparison")
		var parents []artifacts.Parent
		for _, p := range instrument.Lineage.Parents {
			if p.Kind == "bundle" {
				parents = append(parents, p)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "segmentation", ID: ma.ID, Name: ma.Name}, artifacts.Parent{Kind: "segmentation", ID: mb.ID, Name: mb.Name})
		params, _ := json.Marshal(p)
		spec, _ := json.Marshal(compareSpec{path, idA, idB})
		m := artifacts.Manifest{
			ID:   id,
			Kind: "comparison",
			Name: ma.Name + " and " + mb.Name,
			Lineage: artifacts.Lineage{
				Parents: parents,
				Job:     &artifacts.Job{ID: r.ID(), Kind: "comparison", Spec: spec},
				Method: artifacts.Method{Name: "SituCraft segmentation comparison", Version: compare.MethodVersion,
					Citation: "Spurious co-expression: Jones DC, Elz AE, Hadadianpour A, et al. Cell simulation as cell segmentation. Nat Methods. 2025;22:1331–1342. doi:10.1038/s41592-025-02697-0; mutually exclusive co-expression rate: Hartman A, Satija R. Comparative analysis of multiplexed in situ gene expression profiling technologies. bioRxiv 2024. doi:10.1101/2024.01.11.575135"},
				Parameters: params,
				Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: []artifacts.File{{Path: compare.FileDescription, Role: compare.Roles[compare.FileDescription]}, {Path: compare.FileGroups, Role: compare.Roles[compare.FileGroups]}},
		}
		recorded, err := a.artifacts.Promote(m, compare.Check)
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		if recorded.State == artifacts.Invalid {
			return fmt.Errorf("%w: the comparison's check found fatal problems (artifact %s, kept for review)", jobs.ErrInvalid, recorded.ID)
		}
		r.Log("info", "Recorded the comparison as artifact %s: %d one-to-one, %d split, %d merged; %.2f%% of gene transcripts changed cell.", recorded.ID, res.Classes[compare.OneToOne], res.Classes[compare.Split], res.Classes[compare.Merged], 100*res.ChangesQ20.Fraction)
		return nil
	}
}

// costOf is what a segmentation cost SituCraft, when known: the time of the job that made (or
// imported) it, and its own files' size. The instrument's own segmentation was made by the
// instrument: its registration's time is not its cost.
func (a *app) costOf(m artifacts.Manifest) (float64, int64) {
	if isBundleOwn(m) {
		return 0, 0
	}
	var size int64
	for _, f := range m.Files {
		if f.Location != artifacts.InBundle {
			size += f.Size
		}
	}
	var seconds float64
	if m.Lineage.Job != nil {
		if j, ok := a.jobs.Get(m.Lineage.Job.ID); ok && !j.Started.IsZero() && !j.Ended.IsZero() {
			seconds = j.Ended.Sub(j.Started).Seconds()
		}
	}
	return seconds, size
}
