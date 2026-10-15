package main

// A segmentation with errors planted in the instrument's (internal/segment/plant), for the example
// that teaches reviewing a comparison: a job writes it with its answer key (planted.json), checks
// and records it as a segmentation artifact, simulated. It is named by the instrument's
// segmentation, the method's version and the parameters: asking again answers the same artifact.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/segment/plant"
	"situcraft/internal/xenium"
)

type plantSpec struct {
	Bundle string       `json:"bundle"`
	Params plant.Params `json:"params"`
}

func plantID(instrument string, p plant.Params) string {
	params, _ := json.Marshal(p)
	h := sha256.Sum256([]byte(instrument + "\n" + plant.Method + " " + plant.MethodVersion + "\n" + string(params)))
	return "pla-" + hex.EncodeToString(h[:10])
}

func (a *app) registerPlant(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/plant", a.startPlant)
}

// startPlant starts the job planting errors in a bundle's instrument segmentation, answering {id,
// artifact, job}: the artifact at once when it was made already. The body may change the
// example's parameters ({merges, splits, missed, grown, grow_um, seed}).
func (a *app) startPlant(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	p := plant.DefaultParams()
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&p); err != nil {
			writeError(w, http.StatusBadRequest, "The request is not JSON: "+err.Error())
			return
		}
	}
	if err := checkPlant(p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
		writeError(w, http.StatusConflict, "The instrument's segmentation of "+name+" is not registered yet: the errors are planted in it.")
		return
	}
	id := plantID(instrument.ID, p)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	job, _, err := a.jobs.Submit("plant", "Planted errors in "+name+"'s segmentation", "plant\x00"+path+"\x00"+id, plantSpec{path, p}, a.plantJob(path, p))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

func checkPlant(p plant.Params) error {
	for _, n := range []int{p.Merges, p.Splits, p.Missed, p.Grown} {
		if n < 0 || n > 1000 {
			return errors.New("each kind of error is planted 0 to 1000 times")
		}
	}
	if !(p.GrowUm >= 0 && p.GrowUm <= 20) {
		return errors.New("a grown cell reaches 0 to 20 µm")
	}
	return nil
}

// plantJob plants errors in a bundle's instrument segmentation and records the result.
func (a *app) plantJob(path string, p plant.Params) jobs.Func {
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
		id := plantID(instrument.ID, p)
		r.Progress(0.02, "Reading the instrument's segmentation")
		src, err := a.segmentationFolder(ctx, b, *instrument)
		if err != nil {
			return err
		}
		dir, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		r.Log("info", "Planting %d merges, %d splits, %d missed and %d grown cells in %s (seed %d).", p.Merges, p.Splits, p.Missed, p.Grown, instrument.Name, p.Seed)
		d, truth, err := plant.Plant(ctx, src, instrument.Name, bundleTranscripts{b}, dir, p, func(f float64, message string) {
			r.Progress(0.05+0.85*f, message)
		})
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		counts := map[string]int{}
		for _, e := range truth.Errors {
			counts[e.Kind]++
		}
		r.Log("info", "Planted %d merges, %d splits, %d missed and %d grown cells: %d cells.", counts[plant.Merge], counts[plant.Split], counts[plant.Missed], counts[plant.Grown], d.Cells)
		r.Validating()
		r.Progress(0.92, "Checking the segmentation")
		var files []artifacts.File
		for _, name := range []string{segment.FileDescription, segment.FileCells, segment.FileMask, segment.FileAssignments, segment.FileCounts, segment.FileOutlines} {
			files = append(files, artifacts.File{Path: name, Role: segment.Roles[name]})
		}
		files = append(files, artifacts.File{Path: plant.FileTruth, Role: "answer key"})
		var parents []artifacts.Parent
		for _, pr := range instrument.Lineage.Parents {
			if pr.Kind == "bundle" {
				parents = append(parents, pr)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "segmentation", ID: instrument.ID, Name: instrument.Name})
		spec, _ := json.Marshal(plantSpec{path, p})
		m := artifacts.Manifest{
			ID:   id,
			Kind: "segmentation",
			Name: "Planted errors (simulated)",
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "plant", Spec: spec},
				Method:     artifacts.Method{Name: plant.Method, Version: plant.MethodVersion},
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
		r.Log("info", "Recorded it as artifact %s, with its answer key (planted.json).", recorded.ID)
		return nil
	}
}

// bundleTranscripts reads a bundle's transcripts in their row order.
type bundleTranscripts struct{ b *xenium.Bundle }

func (d bundleTranscripts) Transcripts(fn func(*xenium.TranscriptBatch) error) error {
	t, err := d.b.OpenTranscripts()
	if err != nil {
		return err
	}
	defer t.Close()
	return t.Each(fn)
}
