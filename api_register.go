package main

// Automatic registration of a post-Xenium image (internal/register, internal/nuclei), as a job: the
// image's nuclear signal (an H&E's haematoxylin, an immunofluorescence image's nuclear channel)
// registered to its dataset's DAPI, recorded as a version of the image's alignment (a transform
// artifact, api_transforms.go) with its local matches, its deformation when one was fitted and
// kept, and what the search and the fits found. The same images give the same version.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/nuclei"
	"situcraft/internal/register"
	"situcraft/internal/slide"
	"situcraft/internal/xenium"
)

// registrationReport is what an automatic registration found.
type registrationReport struct {
	// Channel is what of the image was registered, Fixed the Xenium image's channel.
	Channel string `json:"channel"`
	Fixed   string `json:"fixed"`
	// Best is the search's candidate (none when the registration started from a version), and
	// Candidates its best few.
	Best       *register.Candidate  `json:"best,omitempty"`
	Candidates []register.Candidate `json:"candidates,omitempty"`
	// NMI is the images' normalized mutual information at the finest level refined (Resolution µm
	// a pixel); PatchUm the local matches' patches' side, Fine their µm a pixel.
	NMI        float64 `json:"nmi"`
	Resolution float64 `json:"resolution"`
	PatchUm    float64 `json:"patchUm"`
	Fine       float64 `json:"fine"`
	// A deformation's: the matches' errors with it, each fifth predicted from the rest with it and
	// with the model alone, its smoothness and its largest displacement over the Xenium image.
	DeformedRMS     float64  `json:"deformedRms,omitempty"`
	HeldOut         float64  `json:"heldOut,omitempty"`
	ModelHeldOut    float64  `json:"modelHeldOut,omitempty"`
	Smoothness      float64  `json:"smoothness,omitempty"`
	MaxDisplacement float64  `json:"maxDisplacement,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

// registerSpec is what a registration job is asked.
type registerSpec struct {
	Bundle   string `json:"bundle"`
	Image    string `json:"image"`
	Model    string `json:"model"`
	NonRigid bool   `json:"nonrigid"`
	// Channel is an immunofluorescence image's nuclear channel (its index; found by its name if
	// not given).
	Channel *int `json:"channel,omitempty"`
	// Start is a version to start from instead of searching.
	Start string `json:"start,omitempty"`
	By    string `json:"by,omitempty"`
	Note  string `json:"note,omitempty"`
}

func (a *app) registerRegistration(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/images/{id}/register", a.startRegistration)
	mux.HandleFunc("GET /api/transforms/{id}/inverse", a.inverseField)
}

// startRegistration starts the job registering an image of a bundle's dataset.
func (a *app) startRegistration(w http.ResponseWriter, r *http.Request) {
	var spec registerSpec
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "The request is not a registration's: "+err.Error())
		return
	}
	if _, err := register.ParseModel(spec.Model); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	spec.Image = r.PathValue("id")
	im, idesc, err := a.imageOf(spec.Image)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
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
	if spec.Start != "" {
		if _, sd, err := a.transformOf(spec.Start); err != nil || sd.Source.Image != spec.Image {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("%s is not an alignment of this image.", spec.Start))
			return
		}
	}
	title := fmt.Sprintf("Register %s to %s", idesc.Name, name)
	raw, _ := json.Marshal(spec)
	job, _, err := a.jobs.Submit("register", title, "register\x00"+string(raw), spec, a.registerJob(spec))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"job": job})
}

func bundleRun(parents []artifacts.Parent) string {
	for _, p := range parents {
		if p.Kind == "bundle" {
			return p.ID
		}
	}
	return ""
}

// registerJob registers the image and records the version.
func (a *app) registerJob(spec registerSpec) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		im, idesc, err := a.imageOf(spec.Image)
		if err != nil {
			return err
		}
		model, _ := register.ParseModel(spec.Model)
		b, err := xenium.Open(spec.Bundle, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		r.Progress(0, "Reading the images")
		fixed, pixel, fixedName, closeFixed, err := nuclei.FromBundle(b)
		if err != nil {
			return err
		}
		defer closeFixed()
		img, err := slide.Open(idesc.Source.Path)
		if err != nil {
			return fmt.Errorf("the image is not where it was recorded (%s): %w", idesc.Source.Path, err)
		}
		defer img.Close()
		if img.Width != idesc.Image.Width || img.Height != idesc.Image.Height {
			return fmt.Errorf("%s is not the image recorded: it changed", idesc.Source.Path)
		}
		channel := -1
		if img.Kind == slide.Channels {
			if spec.Channel != nil {
				channel = *spec.Channel
			} else if channel = nuclei.NuclearChannel(img.Description); channel < 0 {
				return fmt.Errorf("none of the image's channels (%s) is named as a nuclear stain: name the one to register by (channel)", kindText(img.Description))
			}
		}
		moving, what, err := nuclei.FromImage(img, channel, register.HAndE)
		if err != nil {
			return err
		}
		var start *register.Affine
		if spec.Start != "" {
			_, sd, err := a.transformOf(spec.Start)
			if err != nil {
				return err
			}
			start = &sd.Transform
		}
		r.Log("info", "Registering %s (%s) to the Xenium image's %s, as a %s%s.", idesc.Name, what, fixedName, model, map[bool]string{true: ", with a deformation if one helps", false: ""}[spec.NonRigid])
		res, err := register.Register(ctx, fixed, moving, register.Options{
			Model: model, NonRigid: spec.NonRigid, FixedPixel: pixel, MovingPixel: idesc.Image.PixelSize, Start: start,
			Progress: func(at float64, what string) { r.Progress(math.Min(0.98, at), what) },
			Log:      func(f string, args ...any) { r.Log("info", f, args...) },
		})
		if err != nil {
			return err
		}
		if res.Fit == nil {
			return errors.New("too few local matches to fit the alignment to: the images may not be of one section, or the image's nuclear stain is too faint; place landmarks instead")
		}
		ext := [2]float64{float64(fixed.Levels()[0][0]) * pixel, float64(fixed.Levels()[0][1]) * pixel}
		report := &registrationReport{Channel: what, Fixed: fixedName, Candidates: res.Candidates, NMI: res.NMI, Resolution: res.Resolution, PatchUm: res.PatchUm, Fine: res.Fine,
			DeformedRMS: res.DeformedRMS, HeldOut: res.HeldOut, ModelHeldOut: res.AffineHeld, Smoothness: res.Smoothness, Warnings: res.Warnings}
		if start == nil {
			best := res.Best
			report.Best = &best
		}
		if res.Deformation != nil {
			report.MaxDisplacement = res.Deformation.MaxDisplacement(0, 0, ext[0], ext[1])
		}
		d := transformDescription{
			Format: "situcraft-transform", Version: 1,
			Source:          coordinateSystem{Name: "image:" + spec.Image, Units: "pixels", Image: spec.Image},
			Destination:     coordinateSystem{Name: "global", Units: "µm", Dataset: bundleRun(im.Lineage.Parents)},
			Method:          "automatic",
			Model:           model,
			Transform:       res.Transform,
			Deformation:     res.Deformation,
			Registration:    report,
			XeniumPixelSize: pixel,
			Parent:          spec.Start,
			By:              spec.By,
			Note:            spec.Note,
			Fit:             &fitSummary{Residuals: res.Fit.Residuals, LeaveOneOut: res.Fit.LeaveOneOut, RMS: res.Fit.RMS, Max: res.Fit.Max, LooRMS: res.Fit.LooRMS, LooMax: res.Fit.LooMax},
		}
		if d.By == "" {
			d.By = "SituCraft"
		}
		for _, m := range res.Matches {
			d.Matches = append(d.Matches, match{Fixed: [2]float64{m.Fixed.X, m.Fixed.Y}, Moving: [2]float64{m.Moving.X, m.Moving.Y}, Quality: m.Quality, Kept: m.Kept})
		}
		d.Name = transformName(d, nil)
		r.Validating()
		m, err := a.recordTransform(im, idesc, d, nil)
		if err != nil {
			return err
		}
		r.Log("info", "Recorded the alignment as artifact %s: %s, %.4f µm per pixel; the kept matches %.3f µm from the fit, root-mean-square.", m.ID, d.Name, d.Transform.Scale(), d.Fit.RMS)
		return nil
	}
}

// inverseField answers where each point of a grid over a deformed version's Xenium image comes
// from: the displacement e(y) with y + e(y) deformed to y, so that the page draws the image by
// mapping each point back. Little-endian: "SCIV", the grid's points across and down (uint32), its
// first point's x and y and its step (µm, float32), then each point's displacement (x then y,
// float32), row by row.
func (a *app) inverseField(w http.ResponseWriter, r *http.Request) {
	_, d, err := a.transformOf(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	b := d.Deformation
	if b == nil {
		writeError(w, http.StatusNotFound, "This alignment has no deformation.")
		return
	}
	step := b.Spacing / 8
	nx, ny := 8*(b.NX-1)+1, 8*(b.NY-1)+1
	out := make([]byte, 24+8*nx*ny)
	copy(out, "SCIV")
	binary.LittleEndian.PutUint32(out[4:], uint32(nx))
	binary.LittleEndian.PutUint32(out[8:], uint32(ny))
	binary.LittleEndian.PutUint32(out[12:], math.Float32bits(float32(b.Origin[0])))
	binary.LittleEndian.PutUint32(out[16:], math.Float32bits(float32(b.Origin[1])))
	binary.LittleEndian.PutUint32(out[20:], math.Float32bits(float32(step)))
	for j := 0; j < ny; j++ {
		for i := 0; i < nx; i++ {
			y0, y1 := b.Origin[0]+float64(i)*step, b.Origin[1]+float64(j)*step
			x0, x1 := b.Inverse(y0, y1)
			k := 24 + 8*(j*nx+i)
			binary.LittleEndian.PutUint32(out[k:], math.Float32bits(float32(x0-y0)))
			binary.LittleEndian.PutUint32(out[k+4:], math.Float32bits(float32(x1-y1)))
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(out)
}
