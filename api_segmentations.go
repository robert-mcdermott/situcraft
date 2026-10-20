package main

// Segmentations as artifacts (internal/artifacts): the instrument's segmentation of a bundle is
// registered by a job, which checksums the files that make it (the cells, their boundaries and
// the nuclei's, the transcripts' assignments, the cell-by-gene counts and the segmentation masks),
// checks them as the bundle's validation does, and records the artifact: its lineage (the bundle,
// the software and the methods that segmented its cells) and the SHA-256 of each file, which stays
// in the bundle. The same bundle files give the same artifact, in any project.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/xenium"
)

func segmentationJobKey(path string) string { return "segmentation\x00" + path }

func (a *app) registerSegmentations(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/{index}/segmentation", a.instrumentSegmentation)
	mux.HandleFunc("POST /api/local/{index}/segmentation", a.registerInstrumentSegmentation)
	mux.HandleFunc("GET /api/artifacts", func(w http.ResponseWriter, r *http.Request) {
		list, err := a.artifacts.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"artifacts": list})
	})
	// An artifact's own file (one of its manifest's, in its folder): a comparison's description, say.
	mux.HandleFunc("GET /api/artifacts/{id}/files/{file}", func(w http.ResponseWriter, r *http.Request) {
		m, err := a.artifacts.Get(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "No such artifact.")
			return
		}
		name := r.PathValue("file")
		for _, f := range m.Files {
			if f.Path != name || f.Location == artifacts.InBundle {
				continue
			}
			p, err := a.artifacts.Path(m.ID, f.Path)
			if err != nil {
				break
			}
			if strings.HasSuffix(name, ".json") {
				w.Header().Set("Content-Type", "application/json")
			} else {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			http.ServeFile(w, r, p)
			return
		}
		writeError(w, http.StatusNotFound, "The artifact has no file "+name+" of its own.")
	})
	// An artifact's content digest (a clustering's, a map's, labels': its content but for how long
	// its run took), as a certificate records it; null for the kinds compared byte for byte.
	mux.HandleFunc("GET /api/artifacts/{id}/digest", func(w http.ResponseWriter, r *http.Request) {
		m, err := a.artifacts.Get(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "No such artifact.")
			return
		}
		content, err := contentDigest(m.Kind, filepath.Join(a.artifacts.Dir(), m.ID))
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if content == "" {
			writeJSON(w, map[string]any{"content": nil})
			return
		}
		writeJSON(w, map[string]any{"content": content})
	})
	mux.HandleFunc("GET /api/artifacts/{id}", func(w http.ResponseWriter, r *http.Request) {
		m, err := a.artifacts.Get(r.PathValue("id"))
		if errors.Is(err, artifacts.ErrNotFound) {
			writeError(w, http.StatusNotFound, "No such artifact.")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, m)
	})
}

// The files that make the instrument's segmentation, by their role.
func segmentationFiles(b *xenium.Bundle) []artifacts.File {
	var out []artifacts.File
	add := func(path, role string) {
		if info, ok := b.Stat(path); ok {
			out = append(out, artifacts.File{Path: path, Role: role, Size: info.Size, Location: artifacts.InBundle})
		}
	}
	add("cells.parquet", "cells")
	add("cell_boundaries.parquet", "cell boundaries")
	add("nucleus_boundaries.parquet", "nucleus boundaries")
	add("transcripts.parquet", "transcript assignments")
	for _, item := range b.Inventory() {
		if item.Role == "matrix" {
			for _, f := range item.Files {
				add(f, "counts")
			}
		}
	}
	add("cells.zarr.zip", "segmentation masks and methods")
	return out
}

// segmentationID names the artifact by its files' checksums: the same files, the same artifact.
func segmentationID(files []artifacts.File) string {
	sums := make([]string, 0, len(files))
	for _, f := range files {
		sums = append(sums, f.Path+"="+f.SHA256)
	}
	sort.Strings(sums)
	h := sha256.Sum256([]byte(strings.Join(sums, "\n")))
	return "seg-" + hex.EncodeToString(h[:10])
}

// knownSegmentation is the instrument's segmentation artifact of a bundle, when every file's
// checksum is already known (nothing is read); nil when it was not registered yet.
func (a *app) knownSegmentation(b *xenium.Bundle) *artifacts.Manifest {
	if a.cache == nil {
		return nil
	}
	files := segmentationFiles(b)
	for i := range files {
		id, err := b.FileIdentity(files[i].Path)
		if err != nil {
			return nil
		}
		sum, ok := a.cache.KnownChecksum(id)
		if !ok {
			return nil
		}
		files[i].SHA256 = sum
	}
	m, err := a.artifacts.Get(segmentationID(files))
	if err != nil {
		return nil
	}
	return &m
}

// instrumentSegmentation answers {artifact, job}: the registered artifact (or null) and the job
// registering it, if one is under way.
func (a *app) instrumentSegmentation(w http.ResponseWriter, r *http.Request) {
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	writeJSON(w, map[string]any{"artifact": a.knownSegmentation(b), "job": a.jobFor(segmentationJobKey(b.Path))})
}

// registerInstrumentSegmentation starts the job registering it (answering at once when it is
// registered already).
func (a *app) registerInstrumentSegmentation(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	known := a.knownSegmentation(b)
	path, name := b.Path, b.Name
	b.Close()
	if known != nil {
		writeJSON(w, map[string]any{"artifact": known})
		return
	}
	job, _, err := a.jobs.Submit("segmentation", "The instrument's segmentation of "+name, segmentationJobKey(path), map[string]string{"bundle": path}, a.segmentationJob(path))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"artifact": nil, "job": job})
}

// segmentationFinding reports whether a validation finding bears on the segmentation: the cells,
// their boundaries, the masks, the transcripts' cells and the counts' cells.
func segmentationFinding(code string) bool {
	for _, prefix := range []string{"cells.", "boundaries.", "cells-zarr.", "matrix.cells-differ", "matrix.counts-differ", "matrix.cell-order", "transcripts.unknown-cell"} {
		if strings.HasPrefix(code, prefix) {
			return true
		}
	}
	return false
}

// segmentationJob registers a bundle's instrument segmentation.
func (a *app) segmentationJob(path string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		b, err := xenium.Open(path, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		files := segmentationFiles(b)
		if len(files) == 0 || files[0].Path != "cells.parquet" {
			return fmt.Errorf("%w: %s has no cells.parquet", jobs.ErrInvalid, b.Name)
		}
		var total, done int64
		for _, f := range files {
			total += f.Size
		}
		r.Log("info", "Recording the checksums of the %d files of %s's segmentation.", len(files), b.Name)
		for i := range files {
			f := &files[i]
			id, err := b.FileIdentity(f.Path)
			if err != nil {
				return err
			}
			name := f.Path
			f.SHA256, err = a.cache.Checksum(id, f.Size, func() (io.ReadCloser, error) {
				rf, err := b.OpenFile(name)
				if err != nil {
					return nil, err
				}
				return struct {
					io.Reader
					io.Closer
				}{io.NewSectionReader(rf, 0, rf.Size()), rf}, nil
			}, func(n int64) {
				done += n
				r.Progress(0.4*float64(done)/float64(max(total, 1)), "Checksumming "+name)
			})
			if err != nil {
				return fmt.Errorf("%s: %w", f.Path, err)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		r.Validating()
		r.Progress(0.5, "Checking the segmentation as the bundle's validation does")
		report, err := xenium.Validate(ctx, b, xenium.ValidateOptions{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		var findings []artifacts.Finding
		for _, f := range report.Findings {
			if segmentationFinding(f.Code) {
				findings = append(findings, artifacts.Finding{Severity: string(f.Severity), Code: f.Code, Message: f.Title, Count: int(f.Count)})
			}
		}
		s := report.Summary
		type methodCount struct {
			Method string `json:"method"`
			Cells  int64  `json:"cells"`
		}
		methods := []methodCount{}
		for i, m := range s.SegmentationMethods {
			// The methods that segmented cells (a bundle lists every method its software knows).
			if i < len(s.SegmentationCounts) && s.SegmentationCounts[i] > 0 {
				methods = append(methods, methodCount{m, s.SegmentationCounts[i]})
			}
		}
		detail, _ := json.Marshal(map[string]any{"methods": methods})
		fields := map[string]any{"cells": s.Cells, "q20GeneTranscripts": s.Q20GeneTranscripts, "assignedFraction": s.AssignedFraction, "medianTranscriptsPerCell": s.MedianPerCell, "methods": methods}
		if cells, err := b.ReadCells(); err == nil && len(cells.CellArea) > 0 {
			areas := append([]float64(nil), cells.CellArea...)
			sort.Float64s(areas)
			n := len(areas)
			fields["medianAreaUm2"] = (areas[(n-1)/2] + areas[n/2]) / 2
		}
		summary, _ := json.Marshal(fields)
		manifestSum := ""
		if id, err := b.FileIdentity("experiment.xenium"); err == nil {
			manifestSum, _ = a.cache.KnownChecksum(id)
		}
		if manifestSum == "" {
			if raw, err := b.ReadSmall("experiment.xenium"); err == nil {
				h := sha256.Sum256(raw)
				manifestSum = hex.EncodeToString(h[:])
			}
		}
		software := report.Summary.Software
		spec, _ := json.Marshal(map[string]string{"bundle": path})
		m := artifacts.Manifest{
			ID:   segmentationID(files),
			Kind: "segmentation",
			Name: "Instrument segmentation",
			Lineage: artifacts.Lineage{
				Parents: []artifacts.Parent{{Kind: "bundle", ID: report.AnalysisUUID, Name: b.Name, SHA256: manifestSum}},
				Job:     &artifacts.Job{ID: r.ID(), Kind: "segmentation", Spec: spec},
				Method: artifacts.Method{
					Name:     "Xenium cell segmentation (" + softwareTitle(software) + ")",
					Version:  software,
					Citation: "Janesick A, Shelansky R, Gottscho AD, et al. High resolution mapping of the tumor microenvironment using integrated single-cell, spatial and in situ analysis. Nat Commun. 2023;14:8353. doi:10.1038/s41467-023-43458-x",
					Detail:   detail,
				},
				Software: []artifacts.Software{{Name: softwareTitle(software), Version: software}, {Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files:   files,
			Summary: summary,
		}
		r.Progress(0.95, "Recording the artifact")
		recorded, err := a.artifacts.Promote(m, func(string, *artifacts.Manifest) []artifacts.Finding { return findings })
		if err != nil {
			return err
		}
		if recorded.State == artifacts.Invalid {
			return fmt.Errorf("%w: the check found fatal problems in the segmentation (artifact %s, kept for review)", jobs.ErrInvalid, recorded.ID)
		}
		r.Log("info", "Recorded %s's segmentation as artifact %s: %d cells, %d files by SHA-256, %d findings.", b.Name, recorded.ID, s.Cells, len(files), len(recorded.Findings))
		return nil
	}
}

func softwareTitle(software string) string {
	if strings.Contains(strings.ToLower(software), "ranger") {
		return "Xenium Ranger"
	}
	return "Xenium Onboard Analysis"
}
