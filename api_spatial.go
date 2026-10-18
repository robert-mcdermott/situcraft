package main

// Spatial analyses of a segmentation's labeled cells (internal/spatial), made by a job and recorded
// as artifacts of kind "spatial": neighborhood enrichment over a graph; and, over the cells chosen,
// co-occurrence across distances, nearest distances by label, and Ripley's K in a window (the tissue,
// from the segmentation's cells, or a region). The page sends each cell's label as the label set it shows has
// it (any kind: from markers, imported, curated, consensus), and, to deal the labels within regions,
// each cell's region; an analysis is named by what it is made from, so asking again answers it.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/spatial"
	"situcraft/internal/xenium"
)

// spatialCitation cites the analyses' methods.
const spatialCitation = "Schapiro D, Jackson HW, Raghuraman S, et al. histoCAT: analysis of cell phenotypes and interactions in multiplex image cytometry data. Nat Methods 2017;14:873–876. doi:10.1038/nmeth.4391; " +
	"Palla G, Spitzer H, Klein M, et al. Squidpy: a scalable framework for spatial omics analysis. Nat Methods 2022;19:171–178. doi:10.1038/s41592-021-01358-2; " +
	"Benjamini Y, Hochberg Y. Controlling the false discovery rate: a practical and powerful approach to multiple testing. J R Stat Soc B 1995;57:289–300"

// cellCodes are a number for each of a segmentation's cells, as the page sends them: base64 of the
// count (u32) then a signed 32-bit number a cell, little-endian, and the SHA-256 of those bytes.
type cellCodes struct {
	Codes    string `json:"codes"`
	Checksum string `json:"checksum"`
}

// decode checks the codes against their checksum and the segmentation's cells.
func (c cellCodes) decode(cells int) ([]int32, error) {
	raw, err := base64.StdEncoding.DecodeString(c.Codes)
	if err != nil || len(raw) < 4 {
		return nil, errors.New("not base64 of a count and a number a cell")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != c.Checksum {
		return nil, errors.New("not the bytes of their checksum")
	}
	n := int(binary.LittleEndian.Uint32(raw))
	if n != cells || len(raw) != 4+4*n {
		return nil, fmt.Errorf("a number for each of %d cells; the segmentation has %d", n, cells)
	}
	out := make([]int32, n)
	for i := range out {
		out[i] = int32(binary.LittleEndian.Uint32(raw[4+4*i:]))
	}
	return out, nil
}

// spatialLabels are each cell's label (−1 none) and the set they are of.
type spatialLabels struct {
	cellCodes
	ID     string   `json:"id"`
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	Level  int      `json:"level"`
	Names  []string `json:"names"`
	Colors []string `json:"colors,omitempty"`
}

// spatialWindow is Ripley's K's window: the tissue, closed by a radius (µm), or a region, its
// polygons as the page holds them (µm).
type spatialWindow struct {
	Kind     string           `json:"kind"`
	Closing  *float64         `json:"closing,omitempty"`
	Region   *spatial.Region  `json:"region,omitempty"`
	Geometry [][][][2]float64 `json:"geometry,omitempty"`
}

// tissueGrid is the tissue window's grid, about a µm.
const tissueGrid = 1.0

// check fills its defaults and says what is wrong.
func (w *spatialWindow) check() error {
	switch w.Kind {
	case "", "tissue":
		w.Kind = "tissue"
		if w.Closing == nil {
			ten := 10.0
			w.Closing = &ten
		}
		if *w.Closing < 0 || *w.Closing > 100 {
			return errors.New("the tissue window's closing radius is 0 to 100 µm")
		}
		w.Region, w.Geometry = nil, nil
	case "region":
		if w.Region == nil || len(w.Geometry) == 0 {
			return errors.New("a region window needs the region and its polygons")
		}
		w.Closing = nil
	default:
		return fmt.Errorf("no window %q (tissue or region)", w.Kind)
	}
	return nil
}

// spatialStrata are each cell's stratum: 0 in none of the regions, k in the kth.
type spatialStrata struct {
	cellCodes
	Regions []spatial.Region `json:"regions"`
}

type spatialRequest struct {
	Question     string          `json:"question"`
	Segmentation string          `json:"segmentation"`
	Graph        string          `json:"graph,omitempty"`
	Cells        *graphCells     `json:"cells,omitempty"`
	Labels       *spatialLabels  `json:"labels"`
	Strata       *spatialStrata  `json:"strata,omitempty"`
	Window       *spatialWindow  `json:"window,omitempty"`
	Parameters   json.RawMessage `json:"parameters,omitempty"`
}

type spatialSpec struct {
	Bundle string `json:"bundle"`
	spatialRequest
}

// checkedParameters are a request's parameters after defaults and checks, as written.
func checkedParameters(question string, raw json.RawMessage) (json.RawMessage, error) {
	var v any
	switch question {
	case spatial.QuestionEnrichment:
		p := spatial.DefaultEnrichment()
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
		}
		if err := p.Check(); err != nil {
			return nil, err
		}
		v = p
	case spatial.QuestionCooccurrence:
		p := spatial.DefaultCooccurrence()
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
		}
		if err := p.Check(); err != nil {
			return nil, err
		}
		v = p
	case spatial.QuestionDistances:
		p := spatial.DefaultNearest()
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
		}
		if err := p.Check(); err != nil {
			return nil, err
		}
		v = p
	case spatial.QuestionRipley:
		p := spatial.DefaultRipley()
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
		}
		if err := p.Check(); err != nil {
			return nil, err
		}
		v = p
	default:
		return nil, fmt.Errorf("no question %q (enrichment, cooccurrence, distances or ripley)", question)
	}
	return json.Marshal(v)
}

// spatialID names an analysis by what it is made from.
func spatialID(r spatialRequest, params json.RawMessage) string {
	of := r.Graph
	if r.Cells != nil {
		of = "cells " + r.Cells.Checksum
	}
	strata := ""
	if r.Strata != nil {
		strata = r.Strata.Checksum
	}
	window := ""
	if r.Window != nil {
		raw, _ := json.Marshal(r.Window)
		window = string(raw)
	}
	h := sha256.Sum256([]byte(strings.Join([]string{r.Question, r.Segmentation, of, r.Labels.Checksum, strings.Join(r.Labels.Names, "\x1f"), strata, window, spatial.AnalysisVersion, string(params)}, "\n")))
	return "spa-" + hex.EncodeToString(h[:10])
}

func (a *app) registerSpatial(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/spatial/defaults", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"enrichment": spatial.DefaultEnrichment(), "cooccurrence": spatial.DefaultCooccurrence(), "method": spatial.AnalysisVersion, "citation": spatialCitation})
	})
	mux.HandleFunc("POST /api/local/{index}/spatial", a.startSpatial)
	mux.HandleFunc("GET /api/analyses/{id}/cells", a.spatialCells)
}

// startSpatial starts the job making a spatial analysis, answering {id, artifact, job}: the
// artifact at once when it was made already.
func (a *app) startSpatial(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var body spatialRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<20)).Decode(&body); err != nil || body.Segmentation == "" || body.Labels == nil {
		writeError(w, http.StatusBadRequest, "Send {\"question\", \"segmentation\": artifact id, \"graph\" (enrichment) or \"cells\" (co-occurrence), \"labels\": {…}, \"parameters\"}.")
		return
	}
	params, err := checkedParameters(body.Question, body.Parameters)
	if err != nil {
		writeError(w, http.StatusBadRequest, "The analysis: "+err.Error()+".")
		return
	}
	if len(body.Labels.Names) == 0 || len(body.Labels.Names) > 1000 || len(body.Labels.Checksum) != 64 {
		writeError(w, http.StatusBadRequest, "The labels need their names (1 to 1,000), each cell's label and their checksum.")
		return
	}
	if body.Question == spatial.QuestionEnrichment && body.Graph == "" || body.Question != spatial.QuestionEnrichment && (body.Cells == nil || body.Graph != "") {
		writeError(w, http.StatusBadRequest, "An enrichment is of a graph; the other questions, of the cells chosen.")
		return
	}
	if body.Question == spatial.QuestionRipley {
		if body.Window == nil {
			body.Window = &spatialWindow{}
		}
		if err := body.Window.check(); err != nil {
			writeError(w, http.StatusBadRequest, "Ripley's K's window: "+err.Error()+".")
			return
		}
		if body.Window.Kind == "region" {
			if _, err := spatial.PolygonWindow(body.Window.Geometry); err != nil {
				writeError(w, http.StatusBadRequest, "Ripley's K's window: "+err.Error()+".")
				return
			}
		}
	} else if body.Window != nil {
		writeError(w, http.StatusBadRequest, "Only Ripley's K has a window.")
		return
	}
	if body.Strata != nil && body.Question != spatial.QuestionEnrichment {
		writeError(w, http.StatusBadRequest, "Only an enrichment deals its labels within regions.")
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	path, name := b.Path, b.Name
	uuid := b.Experiment.AnalysisUUID
	b.Close()
	sm, err := a.segmentationOf(body.Segmentation, uuid)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var summary struct{ Cells int }
	json.Unmarshal(sm.Summary, &summary)
	if _, err := body.Labels.decode(summary.Cells); err != nil && summary.Cells > 0 {
		writeError(w, http.StatusBadRequest, "The labels: "+err.Error()+".")
		return
	}
	if body.Strata != nil {
		if _, err := body.Strata.decode(summary.Cells); err != nil && summary.Cells > 0 {
			writeError(w, http.StatusBadRequest, "The regions: "+err.Error()+".")
			return
		}
	}
	if body.Graph != "" {
		gm, err := a.artifacts.Get(body.Graph)
		if err != nil || gm.Kind != "graph" || gm.State != artifacts.Complete {
			writeError(w, http.StatusBadRequest, "No such graph.")
			return
		}
		if last := gm.Lineage.Parents[len(gm.Lineage.Parents)-1]; last.ID != sm.ID {
			writeError(w, http.StatusBadRequest, "The graph is of another segmentation.")
			return
		}
		var p spatial.EnrichmentParams
		json.Unmarshal(params, &p)
		var gs spatial.Summary
		json.Unmarshal(gm.Summary, &gs)
		if p.Weight == "boundary" && gs.Kind != spatial.Contact {
			writeError(w, http.StatusBadRequest, "Only a contact graph's neighbors are weighed by the boundary they share.")
			return
		}
	}
	if body.Cells != nil {
		f := clusterFilter{Keep: body.Cells.Keep, Checksum: body.Cells.Checksum}
		if _, err := f.keep(summary.Cells); err != nil && summary.Cells > 0 {
			writeError(w, http.StatusBadRequest, "The cells: "+err.Error()+".")
			return
		}
	}
	id := spatialID(body, params)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	body.Parameters = params
	spec := spatialSpec{Bundle: path, spatialRequest: body}
	what := map[string]string{spatial.QuestionEnrichment: "Neighborhood enrichment", spatial.QuestionCooccurrence: "Co-occurrence", spatial.QuestionDistances: "Nearest distances", spatial.QuestionRipley: "Ripley's K"}[body.Question]
	job, _, err := a.jobs.Submit("spatial", what+" of "+sm.Name+" of "+name, "spatial\x00"+id, spec, a.spatialJob(spec, id))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

// spatialJob makes an analysis and records it.
func (a *app) spatialJob(spec spatialSpec, id string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		b, err := xenium.Open(spec.Bundle, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		defer b.Close()
		sm, err := a.segmentationOf(spec.Segmentation, b.Experiment.AnalysisUUID)
		if err != nil {
			return err
		}
		r.Progress(0, "Reading "+sm.Name)
		dir, err := a.segmentationFolder(ctx, b, sm)
		if err != nil {
			return err
		}
		segCells, err := segment.ReadCells(dir)
		if err != nil {
			return err
		}
		labels, err := spec.Labels.decode(len(segCells))
		if err != nil {
			return fmt.Errorf("the labels: %w", err)
		}
		K := len(spec.Labels.Names)
		for _, l := range labels {
			if l < -1 || l >= int32(K) {
				return fmt.Errorf("the labels: a cell's label %d is not one of the %d", l, K)
			}
		}
		out := &spatial.Analysis{Format: "situcraft-spatial-analysis", Version: 1, Method: "SituCraft spatial analysis", MethodVersion: spatial.AnalysisVersion, Question: spec.Question,
			Segmentation: spatial.Side{ID: sm.ID, Name: sm.Name}, Parameters: spec.Parameters,
			Labels: spatial.LabelSet{ID: spec.Labels.ID, Kind: spec.Labels.Kind, Name: spec.Labels.Name, Level: spec.Labels.Level, Names: spec.Labels.Names, Colors: spec.Labels.Colors, Checksum: spec.Labels.Checksum}}
		out.Cells.Segmentation = len(segCells)
		cells := &spatial.Analyzed{}
		parents := []artifacts.Parent{}
		for _, p := range sm.Lineage.Parents {
			if p.Kind == "bundle" {
				parents = append(parents, p)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "segmentation", ID: sm.ID, Name: sm.Name})
		var name string
		switch spec.Question {
		case spatial.QuestionEnrichment:
			gm, err := a.artifacts.Get(spec.Graph)
			if err != nil {
				return err
			}
			gp, err := a.artifacts.Path(gm.ID, spatial.FileDescription)
			if err != nil {
				return err
			}
			gdir := gp[:len(gp)-len(spatial.FileDescription)]
			g, err := spatial.Read(gdir)
			if err != nil {
				return err
			}
			gd, err := spatial.ReadDescription(gdir)
			if err != nil {
				return err
			}
			out.Graph = &spatial.GraphRef{ID: gm.ID, Name: gm.Name, Parameters: gd.Parameters}
			out.Cells.Choice = gd.Cells.Choice
			parents = append(parents, artifacts.Parent{Kind: "graph", ID: gm.ID, Name: gm.Name})
			var strata []int32
			if spec.Strata != nil {
				if strata, err = spec.Strata.decode(len(segCells)); err != nil {
					return fmt.Errorf("the regions: %w", err)
				}
				out.Strata = &spatial.Strata{Regions: spec.Strata.Regions, Checksum: spec.Strata.Checksum}
			}
			cells.Rows = g.Rows
			cells.Label = make([]int32, len(g.Rows))
			cells.Stratum = make([]int32, len(g.Rows))
			for i, row := range g.Rows {
				cells.Label[i] = labels[row]
				if strata != nil {
					if strata[row] < 0 || int(strata[row]) > len(spec.Strata.Regions) {
						return fmt.Errorf("the regions: cell %d is in region %d of %d", row, strata[row], len(spec.Strata.Regions))
					}
					cells.Stratum[i] = strata[row]
				}
			}
			var p spatial.EnrichmentParams
			if err := json.Unmarshal(spec.Parameters, &p); err != nil {
				return err
			}
			r.Log("info", "Neighborhood enrichment of %s in %s: %d permutations, seed %d.", spec.Labels.Name, gm.Name, p.Permutations, p.Seed)
			var st []int32
			if strata != nil {
				st = cells.Stratum
			}
			e, err := spatial.Enrich(ctx, g, cells.Label, K, st, p, runtime.NumCPU(), func(f float64) { r.Progress(0.05+0.85*f, "Dealing the labels again") })
			if err != nil {
				return err
			}
			out.Enrichment = &spatial.EnrichmentResult{Cells: e.Cells, Edges: e.Edges, Strata: e.Strata, Count: e.Count, Expected: e.Expected, Mean: e.Mean, SD: e.SD, Z: e.Z, P: e.P, PAdj: e.PAdj}
			name = fmt.Sprintf("Neighborhood enrichment of %s, %s", spec.Labels.Name, gm.Name)
		default:
			f := clusterFilter{Keep: spec.Cells.Keep, Checksum: spec.Cells.Checksum}
			keep, err := f.keep(len(segCells))
			if err != nil {
				return fmt.Errorf("the cells chosen: %w", err)
			}
			out.Cells.Choice = spec.Cells.choice()
			var x, y []float32
			for i, c := range segCells {
				if keep[i] {
					cells.Rows = append(cells.Rows, uint32(i))
					cells.Label = append(cells.Label, labels[i])
					cells.Stratum = append(cells.Stratum, 0)
					x, y = append(x, c.X), append(y, c.Y)
				}
			}
			switch spec.Question {
			case spatial.QuestionCooccurrence:
				var p spatial.CooccurrenceParams
				if err := json.Unmarshal(spec.Parameters, &p); err != nil {
					return err
				}
				r.Log("info", "Co-occurrence of %s in %s: %d distances to %g µm, the pairs %s each.", spec.Labels.Name, sm.Name, len(p.Distances), p.Distances[len(p.Distances)-1], map[string]string{spatial.PairsWithin: "within", spatial.PairsBetween: "up to"}[p.Pairs])
				r.Progress(0.1, "Counting the pairs at each distance")
				co, err := spatial.Cooccur(ctx, x, y, cells.Label, K, p, runtime.NumCPU())
				if err != nil {
					return err
				}
				res := &spatial.CooccurrenceResult{Distances: co.Distances, Pairs: co.Pairs, Cells: co.Cells}
				for band := range co.Count {
					res.Count = append(res.Count, co.Count[band])
					res.Ratio = append(res.Ratio, co.Ratio[band])
				}
				out.Cooccurrence = res
				name = fmt.Sprintf("Co-occurrence of %s in %s", spec.Labels.Name, sm.Name)
			case spatial.QuestionDistances:
				var p spatial.NearestParams
				if err := json.Unmarshal(spec.Parameters, &p); err != nil {
					return err
				}
				r.Log("info", "Nearest distances of %s in %s: to %g µm, %d permutations, seed %d.", spec.Labels.Name, sm.Name, p.Reach, p.Permutations, p.Seed)
				nd, err := spatial.NearestDistances(ctx, x, y, cells.Label, K, p, runtime.NumCPU(), func(f float64) { r.Progress(0.05+0.85*f, "Finding each cell's nearest of each label") })
				if err != nil {
					return err
				}
				out.Nearest = &spatial.NearestResult{Reach: nd.Reach, Distances: nd.Distances, Cells: nd.Cells, Beyond: nd.Beyond, Median: nd.Median, Mean: nd.Mean, PermMean: nd.PermMean, PermSD: nd.PermSD, Z: nd.Z, P: nd.P, PAdj: nd.PAdj, CDF: nd.CDF, MeanCDF: nd.MeanCDF, Lo: nd.Lo, Hi: nd.Hi}
				name = fmt.Sprintf("Nearest distances of %s in %s", spec.Labels.Name, sm.Name)
			case spatial.QuestionRipley:
				var p spatial.RipleyParams
				if err := json.Unmarshal(spec.Parameters, &p); err != nil {
					return err
				}
				win, ref, err := a.spatialWindowOf(ctx, dir, spec.Window, r)
				if err != nil {
					return err
				}
				out.Window = ref
				r.Log("info", "Ripley's K of %s in %s: to %g µm, the %s correction, %d permutations, seed %d; the window %s µm².", spec.Labels.Name, sm.Name, p.Radii[len(p.Radii)-1], p.Correction, p.Permutations, p.Seed, strconv.FormatFloat(ref.Area, 'f', 0, 64))
				rk, err := spatial.Ripleys(ctx, x, y, cells.Label, K, win, p, runtime.NumCPU(), func(f float64) { r.Progress(0.1+0.8*f, "Counting the pairs within each radius") })
				if err != nil {
					return err
				}
				if rk.Outside > 0 {
					r.Log("warn", "%d labeled cells lie outside the window and are left out.", rk.Outside)
				}
				out.Ripley = &spatial.RipleyResult{Radii: rk.Radii, Correction: p.Correction, Area: rk.Area, Cells: rk.Cells, Pairs: rk.Pairs, Outside: rk.Outside, K: rk.KObs, L: rk.L, Mean: rk.Mean, Lo: rk.Lo, Hi: rk.Hi, P: rk.P, PAdj: rk.PAdj}
				name = fmt.Sprintf("Ripley's K of %s in %s", spec.Labels.Name, sm.Name)
			}
		}
		out.Cells.Analyzed = len(cells.Rows)
		for _, l := range cells.Label {
			if l >= 0 {
				out.Cells.Labeled++
			}
		}
		if out.Cells.Labeled < 2 {
			return errors.New("fewer than two of the cells analyzed have a label")
		}
		r.Log("info", "%d cells analyzed, %d of them labeled with %d labels.", out.Cells.Analyzed, out.Cells.Labeled, K)
		stage, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := spatial.WriteAnalysis(stage, out, cells); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Validating()
		r.Progress(0.95, "Checking the analysis")
		jobSpec, _ := json.Marshal(spec)
		var files []artifacts.File
		for _, f := range spatial.AnalysisFiles {
			files = append(files, artifacts.File{Path: f, Role: spatial.AnalysisRoles[f]})
		}
		m := artifacts.Manifest{
			ID:   id,
			Kind: "spatial",
			Name: name,
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "spatial", Spec: jobSpec},
				Method:     artifacts.Method{Name: "SituCraft spatial analysis", Version: spatial.AnalysisVersion, Citation: spatialCitation},
				Parameters: spec.Parameters,
				Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: files,
		}
		recorded, err := a.artifacts.Promote(m, spatial.CheckAnalysis)
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		if recorded.State == artifacts.Invalid {
			first := ""
			for _, f := range recorded.Findings {
				if f.Severity == "fatal" {
					first = ": " + f.Message
					break
				}
			}
			return fmt.Errorf("%w: the analysis's check found fatal problems%s (artifact %s, kept for review)", jobs.ErrInvalid, first, recorded.ID)
		}
		r.Log("info", "Recorded the analysis as artifact %s.", recorded.ID)
		return nil
	}
}

// spatialCells answers an analysis's cells, little-endian: their count (u32), rows of the
// segmentation (u32), labels (i32, −1 none) and strata (i32).
func (a *app) spatialCells(w http.ResponseWriter, r *http.Request) {
	m, err := a.artifacts.Get(r.PathValue("id"))
	if err != nil || m.Kind != "spatial" || m.State != artifacts.Complete {
		writeError(w, http.StatusNotFound, "No such analysis.")
		return
	}
	p, err := a.artifacts.Path(m.ID, spatial.FileAnalyzed)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := spatial.ReadAnalyzed(p[:len(p)-len(spatial.FileAnalyzed)])
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(len(c.Rows)))
	binary.Write(&buf, binary.LittleEndian, c.Rows)
	binary.Write(&buf, binary.LittleEndian, c.Label)
	binary.Write(&buf, binary.LittleEndian, c.Stratum)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(buf.Bytes())
}

// spatialWindowOf makes Ripley's K's window: the tissue from the segmentation's label raster, or a
// region's polygons; with what the artifact records of it.
func (a *app) spatialWindowOf(ctx context.Context, segDir string, w *spatialWindow, r *jobs.Run) (*spatial.Window, *spatial.WindowRef, error) {
	if w.Kind == "region" {
		win, err := spatial.PolygonWindow(w.Geometry)
		if err != nil {
			return nil, nil, err
		}
		return win, &spatial.WindowRef{Kind: "region", Region: w.Region, Area: win.Area, Segments: win.Segments()}, nil
	}
	r.Progress(0.03, "Finding the tissue")
	mask, err := segment.ReadMask(segDir)
	if err != nil {
		return nil, nil, err
	}
	win, grid, err := spatial.TissueWindow(ctx, mask, tissueGrid, *w.Closing, runtime.NumCPU())
	if err != nil {
		return nil, nil, err
	}
	return win, &spatial.WindowRef{Kind: "tissue", Closing: *w.Closing, Grid: grid, Area: win.Area, Segments: win.Segments()}, nil
}
