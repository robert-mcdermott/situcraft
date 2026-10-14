package main

// SituCraft's nucleus expansion of a bundle (internal/segment/expand), made by a job and recorded
// as a segmentation artifact: its files are written to a staging folder, checked
// (internal/segment, Check) and promoted. The artifact is named by what it is made from, the
// instrument's segmentation (its files' checksums), the method's version and the distance: the
// same bundle and distance give the same artifact.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"runtime"
	"strconv"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/segment/expand"
	"situcraft/internal/xenium"
)

// The expansion distance SituCraft offers first (that of Xenium Onboard Analysis), and the most.
const (
	defaultExpansionUm = 5.0
	maxExpansionUm     = 100.0
)

type expansionSpec struct {
	Bundle     string  `json:"bundle"`
	DistanceUm float64 `json:"distance_um"`
}

func expansionJobKey(path string, distance float64) string {
	return "expansion\x00" + path + "\x00" + formatUm(distance)
}

func formatUm(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// expansionID names the expansion of a segmentation's nuclei by a distance.
func expansionID(instrument string, distance float64) string {
	h := sha256.Sum256([]byte(instrument + "\n" + expand.Method + " " + expand.MethodVersion + "\n" + formatUm(distance)))
	return "exp-" + hex.EncodeToString(h[:10])
}

func (a *app) registerExpansion(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/expansion", a.startExpansion)
}

// startExpansion starts the job expanding a bundle's nuclei by {distance_um} (5 if not given),
// answering {id, artifact, job}: the artifact at once when it was made already.
func (a *app) startExpansion(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var body struct {
		DistanceUm *float64 `json:"distance_um"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "The request is not JSON: "+err.Error())
			return
		}
	}
	distance := defaultExpansionUm
	if body.DistanceUm != nil {
		distance = *body.DistanceUm
	}
	if !(distance >= 0) || distance > maxExpansionUm {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("The distance is from 0 to %g µm.", maxExpansionUm))
		return
	}
	// A tenth of a nanometre is finer than any image: the distance is kept to it, so that 5 and
	// 5.00000000001 are one artifact.
	distance = math.Round(distance*1e4) / 1e4
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	instrument := a.knownSegmentation(b)
	path, name := b.Path, b.Name
	b.Close()
	if instrument == nil {
		writeError(w, http.StatusConflict, "The instrument's segmentation of "+name+" is not registered yet: nucleus expansion starts from its nuclei.")
		return
	}
	id := expansionID(instrument.ID, distance)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	title := fmt.Sprintf("Nucleus expansion of %s by %s µm", name, formatUm(distance))
	job, _, err := a.jobs.Submit("expansion", title, expansionJobKey(path, distance), expansionSpec{path, distance}, a.expansionJob(path, distance))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

// expansionJob grows a bundle's nuclei by distance µm and records the segmentation.
func (a *app) expansionJob(path string, distance float64) jobs.Func {
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
		id := expansionID(instrument.ID, distance)
		dir, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		r.Log("info", "Growing %s's nuclei by %s µm.", b.Name, formatUm(distance))
		d, err := expand.FromBundle(ctx, b, dir, expand.Params{DistanceUm: distance}, runtime.NumCPU(), func(f float64, message string) {
			r.Progress(0.9*f, message)
		})
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Log("info", "Nuclei from %s: %d cells.", d.Nuclei.Source, d.Cells)
		r.Validating()
		r.Progress(0.92, "Checking the segmentation")
		var files []artifacts.File
		for _, name := range []string{segment.FileDescription, segment.FileCells, segment.FileMask, segment.FileAssignments, segment.FileCounts, segment.FileOutlines} {
			files = append(files, artifacts.File{Path: name, Role: segment.Roles[name]})
		}
		var parents []artifacts.Parent
		for _, p := range instrument.Lineage.Parents {
			if p.Kind == "bundle" {
				parents = append(parents, p)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "segmentation", ID: instrument.ID, Name: instrument.Name})
		spec, _ := json.Marshal(expansionSpec{path, distance})
		m := artifacts.Manifest{
			ID:   id,
			Kind: "segmentation",
			Name: "Nucleus expansion, " + formatUm(distance) + " µm",
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "expansion", Spec: spec},
				Method:     artifacts.Method{Name: "SituCraft nucleus expansion", Version: expand.MethodVersion, Citation: expand.Citation},
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
		var agreement expand.Agreement
		if json.Unmarshal(d.Agreement, &agreement) == nil && agreement.Cells.Instrument > 0 {
			r.Log("info", "%.2f%% of transcripts are in the instrument's cell or, as there, in none; %.2f%% of gene transcripts assigned (the instrument: %.2f%%).",
				100*agreement.Transcripts.Agreement, 100*agreement.AssignedFraction.SituCraft, 100*agreement.AssignedFraction.Instrument)
		}
		r.Log("info", "Recorded the expansion as artifact %s: %d cells, %d files by SHA-256, %d findings.", recorded.ID, d.Cells, len(recorded.Files), len(recorded.Findings))
		return nil
	}
}

// expansionOf reads an expansion job's spec.
func expansionOf(spec json.RawMessage) (expansionSpec, error) {
	var p expansionSpec
	if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || !(p.DistanceUm >= 0) || p.DistanceUm > maxExpansionUm {
		return p, errors.New("its spec names no bundle and distance")
	}
	return p, nil
}
