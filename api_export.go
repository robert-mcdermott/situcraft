package main

// Exports of a segmentation (internal/export): a job writes the formats asked for into a folder
// the user names, with situcraft-export.json listing every file by its SHA-256. A segmentation
// with errors planted for practice is simulated, and never exported. Only this computer
// may ask (it writes files); the remote control's export_segmentation needs the token as well.
// An export is not an artifact: it leaves SituCraft's data folder, and the project records it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/export"
	"situcraft/internal/jobs"
	"situcraft/internal/segment/plant"
	"situcraft/internal/xenium"
)

type exportSpec struct {
	Bundle       string   `json:"bundle"`
	Segmentation string   `json:"segmentation"`
	Formats      []string `json:"formats"`
	Folder       string   `json:"folder"`
	Overwrite    bool     `json:"overwrite,omitempty"`
}

func (a *app) registerExports(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/export", a.startExport)
	mux.HandleFunc("GET /api/exports", a.readExport)
}

// startExport starts the job exporting a segmentation of a bundle's dataset: the body is
// {segmentation (artifact id), formats, folder (absolute), overwrite}.
func (a *app) startExport(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may export files.")
		return
	}
	if !a.needCache(w) {
		return
	}
	var spec exportSpec
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "Send {\"segmentation\", \"formats\", \"folder\", \"overwrite\"}: "+err.Error())
		return
	}
	if err := checkExport(&spec); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	spec.Bundle = b.Path
	name, uuid := b.Name, b.Experiment.AnalysisUUID
	b.Close()
	m, err := a.segmentationOf(spec.Segmentation, uuid)
	if err == nil {
		err = exportable(m)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	title := fmt.Sprintf("Export of %s of %s", m.Name, name)
	job, _, err := a.jobs.Submit("export", title, "export\x00"+spec.Folder, spec, a.exportJob(spec))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"job": job, "folder": spec.Folder})
}

// checkExport checks an export's request: known formats, an absolute folder whose parent exists,
// and no folder in the way (a SituCraft export there only when overwrite).
func checkExport(spec *exportSpec) error {
	if spec.Segmentation == "" {
		return errors.New("name the segmentation to export")
	}
	if len(spec.Formats) == 0 {
		return fmt.Errorf("choose a format: %s", strings.Join(export.Formats, ", "))
	}
	for _, f := range spec.Formats {
		if _, ok := export.About[f]; !ok {
			return fmt.Errorf("the format %q is not one of %s", f, strings.Join(export.Formats, ", "))
		}
	}
	spec.Folder = strings.TrimSpace(spec.Folder)
	if spec.Folder == "" {
		return errors.New("name the folder to export into")
	}
	if !filepath.IsAbs(spec.Folder) {
		return fmt.Errorf("%s is not an absolute path", spec.Folder)
	}
	spec.Folder = filepath.Clean(spec.Folder)
	return export.CheckFolder(spec.Folder, spec.Overwrite)
}

func (a *app) exportJob(spec exportSpec) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		b, err := xenium.Open(spec.Bundle, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		m, err := a.segmentationOf(spec.Segmentation, b.Experiment.AnalysisUUID)
		if err == nil {
			err = exportable(m)
		}
		if err != nil {
			return err
		}
		r.Log("info", "Exporting %s of %s as %s into %s.", m.Name, b.Name, strings.Join(spec.Formats, ", "), spec.Folder)
		r.Progress(0.01, "Reading the segmentation")
		folder, err := a.segmentationFolder(ctx, b, m)
		if err != nil {
			return err
		}
		seg := export.Segmentation{ID: m.ID, Name: m.Name, Method: m.Lineage.Method.Name, Version: m.Lineage.Method.Version, Citation: m.Lineage.Method.Citation}
		out, err := export.Run(ctx, export.FromBundle(b), folder, seg, export.Options{
			Formats: spec.Formats, Folder: spec.Folder, Overwrite: spec.Overwrite, Software: "situcraft " + version, Commit: buildCommit(),
		}, func(f float64, message string) { r.Progress(0.02+0.97*f, message) })
		if err != nil {
			return err
		}
		var size int64
		for _, f := range out.Files {
			size += f.Size
		}
		for _, f := range out.Findings {
			level := map[string]string{"fatal": "error", "warning": "warning"}[f.Severity]
			if level == "" {
				level = "info"
			}
			r.Log(level, "%s: %s", f.Format, f.Message)
		}
		r.Log("info", "Wrote %d files (%.1f MB) into %s, each listed by its SHA-256 in %s.", len(out.Files), float64(size)/1e6, spec.Folder, export.ManifestName)
		if out.XeniumRanger != nil {
			r.Log("info", "Xenium Ranger: %s", out.XeniumRanger.Command)
		}
		return nil
	}
}

// exportable refuses a segmentation with errors planted for practice: it is simulated, and its
// cells must not leave SituCraft as if they were a segmentation of the data.
func exportable(m artifacts.Manifest) error {
	if m.Lineage.Method.Name == plant.Method {
		return fmt.Errorf("%s is simulated, its errors planted for practice: it is not exported", m.Name)
	}
	return nil
}

// readExport answers an export's manifest (?folder=, absolute), as the page shows it once the job
// ends. Only this computer may ask.
func (a *app) readExport(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may read exports.")
		return
	}
	folder := r.URL.Query().Get("folder")
	if !filepath.IsAbs(folder) {
		writeError(w, http.StatusBadRequest, "Give the export's folder, an absolute path.")
		return
	}
	m, err := export.ReadManifest(filepath.Clean(folder))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "No SituCraft export in "+folder+".")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, m)
}

// exportOf reads an export job's spec.
func exportOf(spec json.RawMessage) (exportSpec, error) {
	var p exportSpec
	if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || p.Segmentation == "" || p.Folder == "" || len(p.Formats) == 0 {
		return p, errors.New("its spec names no bundle, segmentation, formats and folder")
	}
	return p, nil
}

// removePartialExports removes what an interrupted export left beside its folder.
func removePartialExports(folder string) {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(folder), "."+filepath.Base(folder)+".partial-*"))
	for _, m := range matches {
		os.RemoveAll(m)
	}
}
