package main

// Checking an alignment of a post-Xenium image in µm (internal/register/check.go), as a job: where
// a version places the image off, measured by local matches over the tissue, recorded as an
// artifact of kind "alignment-check" of the image and the version. The same images and version
// give the same check.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/register"
)

// The check artifact's file in its folder.
const checkFile = "check.json"

// checkDescription is check.json: the version checked, what of each image was compared, and the
// check.
type checkDescription struct {
	Format    string `json:"format"`
	Version   int    `json:"version"`
	Name      string `json:"name"`
	Image     string `json:"image"`
	Transform string `json:"transform"`
	Channel   string `json:"channel"`
	Fixed     string `json:"fixed"`
	register.CheckResult
}

type checkSpec struct {
	Bundle    string `json:"bundle"`
	Image     string `json:"image"`
	Transform string `json:"transform"`
	Channel   *int   `json:"channel,omitempty"`
}

func (a *app) registerChecks(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/images/{id}/check", a.startCheck)
	mux.HandleFunc("GET /api/checks/{id}", a.getCheck)
}

// startCheck starts the job checking a version of an image's alignment.
func (a *app) startCheck(w http.ResponseWriter, r *http.Request) {
	var spec checkSpec
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "The request is not a check's: "+err.Error())
		return
	}
	spec.Image = r.PathValue("id")
	im, idesc, err := a.imageOf(spec.Image)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if _, td, err := a.transformOf(spec.Transform); err != nil || td.Source.Image != spec.Image {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("%s is not an alignment of this image.", spec.Transform))
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	spec.Bundle = b.Path
	run, name := b.Experiment.AnalysisUUID, b.Name
	b.Close()
	if bundleRun(im.Lineage.Parents) != run {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("%s is an image of another dataset than %s.", im.Name, name))
		return
	}
	raw, _ := json.Marshal(spec)
	job, _, err := a.jobs.Submit("check", fmt.Sprintf("Check the alignment of %s", idesc.Name), "check\x00"+string(raw), spec, a.checkJob(spec))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"job": job})
}

// checkJob checks the version and records the check.
func (a *app) checkJob(spec checkSpec) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		im, idesc, err := a.imageOf(spec.Image)
		if err != nil {
			return err
		}
		tm, td, err := a.transformOf(spec.Transform)
		if err != nil {
			return err
		}
		r.Progress(0, "Reading the images")
		ims, err := a.openNuclei(spec.Bundle, idesc, spec.Channel)
		if err != nil {
			return err
		}
		defer ims.close()
		r.Log("info", "Checking %s's placement by %s: patches of the Xenium image's %s against its %s, drawn as the version places it.", idesc.Name, tm.Name, ims.fixedName, ims.what)
		res, err := register.CheckLocal(ctx, ims.fixed, ims.moving, register.Placement{T: td.Transform, D: td.Deformation}, register.CheckOptions{FixedPixel: ims.pixel, Progress: r.Progress})
		if err != nil {
			return err
		}
		d := checkDescription{Format: "situcraft-alignment-check", Version: 1, Name: "Check of " + tm.Name, Image: spec.Image, Transform: spec.Transform, Channel: ims.what, Fixed: ims.fixedName, CheckResult: *res}
		raw, _ := json.MarshalIndent(d, "", " ")
		raw = append(raw, '\n')
		h := sha256.Sum256(raw)
		id := "chk-" + hex.EncodeToString(h[:10])
		if existing, err := a.artifacts.Get(id); err == nil {
			r.Log("info", "Recorded the check as artifact %s (as before).", existing.ID)
			return nil
		}
		dir, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, checkFile), raw, 0o644); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		summary, _ := json.Marshal(map[string]any{"image": spec.Image, "transform": spec.Transform, "checked": res.Checked, "matched": res.Matched,
			"median": res.Median, "p90": res.P90, "max": res.Max, "rms": res.RMS, "noise": res.Noise, "worst": res.Worst, "patchUm": res.PatchUm})
		m := artifacts.Manifest{
			ID: id, Kind: "alignment-check", Name: d.Name,
			Lineage: artifacts.Lineage{
				Parents: []artifacts.Parent{{Kind: "image", ID: im.ID, Name: im.Name, SHA256: idesc.Source.SHA256}, {Kind: "transform", ID: tm.ID, Name: tm.Name}},
				Job:     &artifacts.Job{ID: r.ID(), Kind: "check"},
				Method: artifacts.Method{Name: "Alignment checked by local matches: patches of the Xenium image's nuclear stain and the image's, drawn as the version places it, by phase correlation", Version: "1",
					Citation: "Kuglin CD, Hines DC. The phase correlation image alignment method. Proc IEEE Conf Cybernetics and Society 1975; Foroosh H, Zerubia J, Berthod M. Extension of phase correlation to subpixel registration. IEEE Trans Image Process 2002;11(3):188–200"},
				Software: []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files:   []artifacts.File{{Path: checkFile, Role: "check"}},
			Summary: summary,
		}
		r.Validating()
		recorded, err := a.artifacts.Promote(m, nil)
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Log("info", "Recorded the check as artifact %s: %d of %d patches matched; the alignment off by %.2f µm (median), %.2f (90th percentile), at most %.2f; the matches' own error at most about %.2f µm.", recorded.ID, res.Matched, res.Checked, res.Median, res.P90, res.Max, res.Noise)
		return nil
	}
}

func (a *app) getCheck(w http.ResponseWriter, r *http.Request) {
	m, err := a.artifacts.Get(r.PathValue("id"))
	if err != nil || m.Kind != "alignment-check" {
		writeError(w, http.StatusNotFound, "no such check")
		return
	}
	p, err := a.artifacts.Path(m.ID, checkFile)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}
