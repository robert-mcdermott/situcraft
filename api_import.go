package main

// Segmentations made by other tools, imported as artifacts (internal/segment/imports): a job reads
// the files named, checks them against the dataset, writes SituCraft's segmentation files into a
// staging folder, and promotes them, with what the reading and the checks found. The artifact is
// named by what it is made from: the dataset (its instrument segmentation's files), the files read
// (by their SHA-256, wherever they are), the format and the options; the same import of the same
// files is the same artifact.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/segment/imports"
	"situcraft/internal/segment/refine"
	"situcraft/internal/xenium"
)

type importSpec struct {
	Bundle  string          `json:"bundle"`
	Options imports.Options `json:"options"`
}

func (a *app) registerImports(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/import", a.startImport)
}

// startImport starts the job importing a segmentation into a bundle's dataset: the body is the
// import's options (its format, paths and how to read them). Only this computer may ask: it names
// files to read.
func (a *app) startImport(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may import files.")
		return
	}
	if !a.needCache(w) {
		return
	}
	var opt imports.Options
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&opt); err != nil {
		writeError(w, http.StatusBadRequest, "The request is not an import's options: "+err.Error())
		return
	}
	if err := checkImport(&opt); err != nil {
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
		writeError(w, http.StatusConflict, "The instrument's segmentation of "+name+" is not registered yet: an import is checked against the dataset it names.")
		return
	}
	title := fmt.Sprintf("Import of %s into %s", importTitle(opt), name)
	key := "import\x00" + path + "\x00" + strings.Join(opt.Paths, "\x00")
	job, _, err := a.jobs.Submit("import", title, key, importSpec{path, opt}, a.importJob(path, opt))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"job": job})
}

// checkImport checks an import's options and makes its paths absolute.
func checkImport(opt *imports.Options) error {
	switch opt.Format {
	case imports.SpatialData, imports.Baysor, imports.GeoJSON, imports.Xenium, imports.SituCraft:
	default:
		return fmt.Errorf("the format is %s, %s, %s, %s or %s", imports.SpatialData, imports.Baysor, imports.GeoJSON, imports.Xenium, imports.SituCraft)
	}
	if len(opt.Paths) == 0 || len(opt.Paths) > 2 {
		return errors.New("name one or two files to import")
	}
	for i, p := range opt.Paths {
		p = strings.TrimSpace(p)
		if p == "" {
			return errors.New("a path is empty")
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		opt.Paths[i] = abs
	}
	if opt.Units != "" && opt.Units != "um" && opt.Units != "pixels" {
		return errors.New("units are um or pixels")
	}
	return nil
}

func importTitle(opt imports.Options) string {
	if opt.Tool != "" {
		return opt.Tool
	}
	return map[string]string{imports.SpatialData: "a SpatialData store", imports.Baysor: "a Baysor segmentation", imports.GeoJSON: "GeoJSON polygons", imports.Xenium: "an output bundle",
		imports.SituCraft: "a SituCraft segmentation"}[opt.Format]
}

// importJob imports a segmentation into the bundle's dataset and records it.
func (a *app) importJob(path string, opt imports.Options) jobs.Func {
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
		ds, err := imports.BundleDataset(b)
		if err != nil {
			return err
		}
		pending := "imp-job-" + strings.ToLower(r.ID())
		dir, err := a.artifacts.Stage(pending)
		if err != nil {
			return err
		}
		r.Log("info", "Reading %s (%s).", strings.Join(opt.Paths, ", "), opt.Format)
		d, findings, files, err := imports.Run(ctx, ds, opt, dir, runtime.NumCPU(), func(f float64, message string) {
			r.Progress(0.9*f, message)
		})
		if err != nil {
			a.artifacts.Discard(pending)
			return err
		}
		var rec imports.Recorded
		json.Unmarshal(d.Parameters, &rec)
		// The artifact's name: the dataset, the files read, the reading and its options.
		canonical, _ := json.Marshal(struct {
			Format, Units, CoordinateSystem, Cells, Points, Table, CellColumn, CellProperty string
		}{opt.Format, opt.Units, opt.CoordinateSystem, opt.Cells, opt.Points, opt.Table, opt.CellColumn, opt.CellProperty})
		roots := map[string]bool{}
		for _, p := range opt.Paths {
			roots[p] = true
		}
		digest := imports.Digest(files, func(p string) string {
			for root := range roots {
				if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
					if rel == "." {
						return filepath.Base(p)
					}
					return filepath.ToSlash(rel)
				}
			}
			return filepath.Base(p)
		})
		h := sha256.Sum256([]byte(instrument.ID + "\n" + d.Method + "\n" + digest + "\n" + string(canonical)))
		id := "imp-" + hex.EncodeToString(h[:10])
		if err := a.artifacts.Restage(pending, id); err != nil {
			a.artifacts.Discard(pending)
			return err
		}
		r.Validating()
		r.Progress(0.92, "Checking the segmentation")
		var parents []artifacts.Parent
		for _, p := range instrument.Lineage.Parents {
			if p.Kind == "bundle" {
				parents = append(parents, p)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "import", ID: digest[:16], Name: filepath.Base(opt.Paths[0]), SHA256: digest})
		// Each path imported, with the files read under it (all of them are in files).
		type source struct {
			Path   string `json:"path"`
			Files  int    `json:"files"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		}
		var sources []source
		for _, p := range opt.Paths {
			var mine []imports.SourceFile
			for _, f := range files {
				if rel, err := filepath.Rel(p, f.Path); f.Path == p || err == nil && !strings.HasPrefix(rel, "..") {
					mine = append(mine, f)
				}
			}
			src := source{Path: p, Files: len(mine)}
			for _, f := range mine {
				src.Size += f.Size
			}
			src.SHA256 = imports.Digest(mine, func(f string) string {
				if rel, err := filepath.Rel(p, f); err == nil && rel != "." {
					return filepath.ToSlash(rel)
				}
				return filepath.Base(f)
			})
			sources = append(sources, src)
		}
		detail, _ := json.Marshal(map[string]any{"format": opt.Format, "sources": sources, "files": files})
		name := rec.Tool
		if name == "" {
			name = map[string]string{imports.SpatialData: "SpatialData", imports.Baysor: "Baysor", imports.GeoJSON: "GeoJSON", imports.Xenium: "Xenium output", imports.SituCraft: "SituCraft"}[opt.Format]
		}
		method := artifacts.Method{Name: name, Version: rec.Version, Citation: rec.Citation, Detail: detail}
		if opt.Format == imports.SituCraft && d.Method == refine.Method+" "+refine.MethodVersion {
			// SituCraft's own refinement, made headless: recorded as its method, as the app's are.
			name = refineName(refineSpecOfRecorded(d.Parameters))
			method = artifacts.Method{Name: refine.Method, Version: refine.MethodVersion, Citation: refine.Citation, Detail: detail}
		}
		software := []artifacts.Software{}
		if rec.Tool != "" {
			software = append(software, artifacts.Software{Name: rec.Tool, Version: rec.Version})
		}
		software = append(software, artifacts.Software{Name: "situcraft", Version: version, Commit: buildCommit()})
		spec, _ := json.Marshal(importSpec{path, opt})
		var own []artifacts.File
		for _, f := range []string{segment.FileDescription, segment.FileCells, segment.FileMask, segment.FileAssignments, segment.FileCounts, segment.FileOutlines,
			segment.FileProbabilities, segment.FileCertainty, segment.FileDiagnostics, segment.FileLayers} {
			if _, err := os.Stat(filepath.Join(a.artifacts.Dir(), "staging", id, f)); err == nil || f == segment.FileDescription {
				own = append(own, artifacts.File{Path: f, Role: segment.Roles[f]})
			}
		}
		m := artifacts.Manifest{
			ID:   id,
			Kind: "segmentation",
			Name: name + " (imported)",
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "import", Spec: spec},
				Method:     method,
				Parameters: d.Parameters,
				Software:   software,
			},
			Files: own,
		}
		recorded, err := a.artifacts.Promote(m, func(dir string, m *artifacts.Manifest) []artifacts.Finding {
			return append(findings, segment.Check(dir, m)...)
		})
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		if recorded.State == artifacts.Invalid {
			for _, f := range recorded.Findings {
				if f.Severity == "fatal" {
					r.Log("error", "%s", f.Message)
				}
			}
			return fmt.Errorf("%w: the import's check found fatal problems (artifact %s, kept for review)", jobs.ErrInvalid, recorded.ID)
		}
		r.Log("info", "Recorded the import as artifact %s: %d cells, %d of %d gene transcripts assigned, %d findings.", recorded.ID, d.Cells, d.Q20GeneAssigned, d.Q20GeneTranscripts, len(recorded.Findings))
		return nil
	}
}
