package main

// Spatial analyses of a segmentation's labeled cells (internal/spatial), made by a job and recorded
// as artifacts of kind "spatial": neighborhood enrichment over a graph; and, over the cells chosen,
// co-occurrence across distances, nearest distances by label, Ripley's K in a window (the tissue,
// from the segmentation's cells, or a region), regions compared, and a profile across a region's
// edge (both within the tissue window, the regions' polygons sent with them), cellular
// neighborhoods (niches, each with its leading genes from the segmentation's counts), and, of no
// labels, each gene's Moran's I over a graph (spatially variable genes). The page sends each
// cell's label as the label set it shows has it (any kind: from markers, imported, curated,
// consensus), and, to deal the labels within regions, each cell's region; an analysis is named by
// what it is made from, so asking again answers it.

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
	"math"
	"net/http"
	"runtime"
	"strconv"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/spatial"
	"situcraft/internal/xenium"
)

// spatialCitation cites the analyses' methods.
const spatialCitation = "Schapiro D, Jackson HW, Raghuraman S, et al. histoCAT: analysis of cell phenotypes and interactions in multiplex image cytometry data. Nat Methods 2017;14:873–876. doi:10.1038/nmeth.4391; " +
	"Palla G, Spitzer H, Klein M, et al. Squidpy: a scalable framework for spatial omics analysis. Nat Methods 2022;19:171–178. doi:10.1038/s41592-021-01358-2; " +
	"Benjamini Y, Hochberg Y. Controlling the false discovery rate: a practical and powerful approach to multiple testing. J R Stat Soc B 1995;57:289–300; " +
	"Moran PAP. Notes on continuous stochastic phenomena. Biometrika 1950;37:17–23; Cliff AD, Ord JK. Spatial Processes: Models and Applications. Pion, 1981; " +
	"Anselin L. Local indicators of spatial association — LISA. Geogr Anal 1995;27:93–115"

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

// spatialRegion is a region compared, or whose edge is crossed: which version of which region, and
// its polygons as the page holds them (µm).
type spatialRegion struct {
	Region   spatial.Region   `json:"region"`
	Geometry [][][][2]float64 `json:"geometry"`
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
	Regions      []spatialRegion `json:"regions,omitempty"`
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
	case spatial.QuestionRegions:
		p := spatial.DefaultRegions()
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
		}
		if err := p.Check(); err != nil {
			return nil, err
		}
		v = p
	case spatial.QuestionBoundary:
		p := spatial.DefaultBoundary()
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
		}
		if err := p.Check(); err != nil {
			return nil, err
		}
		v = p
	case spatial.QuestionNiches:
		p := spatial.DefaultNiches()
		if len(raw) > 0 {
			// A request's window replaces the default's: neighbors or a radius.
			var given map[string]json.RawMessage
			if err := json.Unmarshal(raw, &given); err != nil {
				return nil, err
			}
			if _, ok := given["radius"]; ok {
				p.Neighbors = 0
			}
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, err
			}
		}
		if err := p.Check(); err != nil {
			return nil, err
		}
		v = p
	case spatial.QuestionMoran:
		p := spatial.DefaultMoran()
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
		return nil, fmt.Errorf("no question %q (enrichment, cooccurrence, distances, ripley, regions, boundary, niches or moran)", question)
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
	regions := ""
	if len(r.Regions) > 0 {
		raw, _ := json.Marshal(r.Regions)
		regions = string(raw)
	}
	labels, names := "", ""
	if r.Labels != nil {
		labels, names = r.Labels.Checksum, strings.Join(r.Labels.Names, "\x1f")
	}
	h := sha256.Sum256([]byte(strings.Join([]string{r.Question, r.Segmentation, of, labels, names, strata, window, regions, spatial.AnalysisVersion, string(params)}, "\n")))
	return "spa-" + hex.EncodeToString(h[:10])
}

func (a *app) registerSpatial(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/spatial/defaults", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"enrichment": spatial.DefaultEnrichment(), "cooccurrence": spatial.DefaultCooccurrence(), "distances": spatial.DefaultNearest(), "ripley": spatial.DefaultRipley(), "regions": spatial.DefaultRegions(), "boundary": spatial.DefaultBoundary(), "niches": spatial.DefaultNiches(), "moran": spatial.DefaultMoran(), "method": spatial.AnalysisVersion, "citation": spatialCitation})
	})
	mux.HandleFunc("POST /api/local/{index}/spatial", a.startSpatial)
	mux.HandleFunc("GET /api/analyses/{id}/cells", a.spatialCells)
	mux.HandleFunc("GET /api/analyses/{id}/local", a.spatialLocal)
}

// startSpatial starts the job making a spatial analysis, answering {id, artifact, job}: the
// artifact at once when it was made already.
func (a *app) startSpatial(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var body spatialRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<20)).Decode(&body); err != nil || body.Segmentation == "" || body.Labels == nil && body.Question != spatial.QuestionMoran {
		writeError(w, http.StatusBadRequest, "Send {\"question\", \"segmentation\": artifact id, \"graph\" (enrichment, moran) or \"cells\" (the others), \"labels\": {…} (all but moran), \"parameters\"}.")
		return
	}
	if body.Question == spatial.QuestionMoran && body.Labels != nil {
		writeError(w, http.StatusBadRequest, "Moran's I is of genes: it takes no labels.")
		return
	}
	params, err := checkedParameters(body.Question, body.Parameters)
	if err != nil {
		writeError(w, http.StatusBadRequest, "The analysis: "+err.Error()+".")
		return
	}
	if body.Labels != nil && (len(body.Labels.Names) == 0 || len(body.Labels.Names) > 1000 || len(body.Labels.Checksum) != 64) {
		writeError(w, http.StatusBadRequest, "The labels need their names (1 to 1,000), each cell's label and their checksum.")
		return
	}
	ofGraph := body.Question == spatial.QuestionEnrichment || body.Question == spatial.QuestionMoran
	if ofGraph && (body.Graph == "" || body.Cells != nil) || !ofGraph && (body.Cells == nil || body.Graph != "") {
		writeError(w, http.StatusBadRequest, "An enrichment and Moran's I are of a graph; the other questions, of the cells chosen.")
		return
	}
	switch body.Question {
	case spatial.QuestionRipley, spatial.QuestionRegions, spatial.QuestionBoundary:
		if body.Window == nil {
			body.Window = &spatialWindow{}
		}
		if err := body.Window.check(); err != nil {
			writeError(w, http.StatusBadRequest, "The window: "+err.Error()+".")
			return
		}
		if body.Window.Kind == "region" {
			if body.Question != spatial.QuestionRipley {
				writeError(w, http.StatusBadRequest, "Regions are compared, and an edge crossed, within the tissue: their window is the tissue.")
				return
			}
			if _, err := spatial.PolygonWindow(body.Window.Geometry); err != nil {
				writeError(w, http.StatusBadRequest, "Ripley's K's window: "+err.Error()+".")
				return
			}
		}
	default:
		if body.Window != nil {
			writeError(w, http.StatusBadRequest, "Only Ripley's K, regions compared and a profile across an edge have a window.")
			return
		}
	}
	switch {
	case body.Question == spatial.QuestionRegions && (len(body.Regions) < 1 || len(body.Regions) > spatial.MaxRegions):
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Regions compared are 1 to %d regions.", spatial.MaxRegions))
		return
	case body.Question == spatial.QuestionBoundary && len(body.Regions) != 1:
		writeError(w, http.StatusBadRequest, "A profile across an edge is of one region.")
		return
	case body.Question != spatial.QuestionRegions && body.Question != spatial.QuestionBoundary && len(body.Regions) > 0:
		writeError(w, http.StatusBadRequest, "Only regions compared and a profile across an edge take regions.")
		return
	}
	seen := map[string]bool{}
	for _, sr := range body.Regions {
		if sr.Region.ID == "" || sr.Region.Name == "" || seen[sr.Region.ID] {
			writeError(w, http.StatusBadRequest, "Each region needs its id and name, once.")
			return
		}
		seen[sr.Region.ID] = true
		if _, err := spatial.PolygonWindow(sr.Geometry); err != nil {
			writeError(w, http.StatusBadRequest, "The region "+sr.Region.Name+": "+err.Error()+".")
			return
		}
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
	if body.Labels == nil {
		// Moran's I: no labels.
	} else if _, err := body.Labels.decode(summary.Cells); err != nil && summary.Cells > 0 {
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
	what := map[string]string{spatial.QuestionEnrichment: "Neighborhood enrichment", spatial.QuestionCooccurrence: "Co-occurrence", spatial.QuestionDistances: "Nearest distances", spatial.QuestionRipley: "Ripley's K", spatial.QuestionRegions: "Regions compared", spatial.QuestionBoundary: "A profile across an edge", spatial.QuestionNiches: "Niches", spatial.QuestionMoran: "Moran's I"}[body.Question]
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
		// Each cell's label (none for Moran's I, which is of genes).
		labels := make([]int32, len(segCells))
		for i := range labels {
			labels[i] = -1
		}
		K := 0
		var labelSet spatial.LabelSet
		if spec.Labels != nil {
			if labels, err = spec.Labels.decode(len(segCells)); err != nil {
				return fmt.Errorf("the labels: %w", err)
			}
			K = len(spec.Labels.Names)
			for _, l := range labels {
				if l < -1 || l >= int32(K) {
					return fmt.Errorf("the labels: a cell's label %d is not one of the %d", l, K)
				}
			}
			labelSet = spatial.LabelSet{ID: spec.Labels.ID, Kind: spec.Labels.Kind, Name: spec.Labels.Name, Level: spec.Labels.Level, Names: spec.Labels.Names, Colors: spec.Labels.Colors, Checksum: spec.Labels.Checksum}
		}
		out := &spatial.Analysis{Format: "situcraft-spatial-analysis", Version: 1, Method: "SituCraft spatial analysis", MethodVersion: spatial.AnalysisVersion, Question: spec.Question,
			Segmentation: spatial.Side{ID: sm.ID, Name: sm.Name}, Parameters: spec.Parameters, Labels: labelSet}
		var local []spatial.LocalMoran
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
		case spatial.QuestionMoran:
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
			cells.Rows = g.Rows
			cells.Label = make([]int32, len(g.Rows))
			cells.Stratum = make([]int32, len(g.Rows))
			for i := range cells.Label {
				cells.Label[i] = -1
			}
			var p spatial.MoranParams
			if err := json.Unmarshal(spec.Parameters, &p); err != nil {
				return err
			}
			r.Progress(0.03, "Normalizing the cells' expression")
			counts, err := segment.ReadCounts(dir)
			if err != nil {
				return err
			}
			values, target, empty, err := spatial.NormalizedExpression(counts, g.Rows)
			if err != nil {
				return err
			}
			r.Log("info", "Moran's I of %d genes over %s (%d cells; %s weights), %d permutations (seed %d); local Moran's I of the %d leading genes.", len(counts.Genes), gm.Name, len(g.Rows), p.Weights, p.Permutations, p.Seed, p.Local)
			if empty > 0 {
				r.Log("info", "%d cells have no gene transcript: their values are zeros.", empty)
			}
			mi, err := spatial.MoransI(ctx, g, values, len(counts.Genes), p, runtime.NumCPU(), func(f float64) { r.Progress(0.05+0.85*f, "Each gene's Moran's I") })
			if err != nil {
				return err
			}
			if mi.Islands > 0 {
				r.Log("warn", "%d cells have no neighbor in the graph: they weigh nothing.", mi.Islands)
			}
			res := &spatial.MoranResult{Genes: counts.Genes, Weights: p.Weights, Cells: mi.Cells, Edges: g.Edges(), Islands: mi.Islands, NoTranscript: empty, Target: target,
				S0: mi.S0, S1: mi.S1, S2: mi.S2, Expected: mi.Expected, I: mi.I, VarNorm: mi.VarNorm, VarRand: mi.VarRand, Z: mi.Z, P: mi.P,
				PermMean: mi.PermMean, PermSD: mi.PermSD, PermP: mi.Perm, PAdj: mi.PAdj, Mean: mi.Mean, Detected: mi.Detected, Local: []int{}}
			for _, l := range mi.Local {
				res.Local = append(res.Local, l.Gene)
			}
			local = mi.Local
			out.Moran = res
			name = fmt.Sprintf("Moran's I of %s's genes, %s", sm.Name, gm.Name)
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
			case spatial.QuestionNiches:
				var p spatial.NicheParams
				if err := json.Unmarshal(spec.Parameters, &p); err != nil {
					return err
				}
				window := fmt.Sprintf("each cell's %d nearest labeled cells", p.Neighbors)
				if p.Radius > 0 {
					window = fmt.Sprintf("the labeled cells within %g µm of each cell", p.Radius)
				}
				r.Log("info", "Niches of %s in %s: windows of %s, %d niches, the best of %d k-means starts (seed %d), %d further seeds.", spec.Labels.Name, sm.Name, window, p.Niches, p.Starts, p.Seed, p.Seeds)
				nc, err := spatial.FindNiches(ctx, x, y, cells.Label, K, p, runtime.NumCPU(), func(f float64) { r.Progress(0.05+0.7*f, "Finding the niches") })
				if err != nil {
					return err
				}
				var rows []uint32
				var groups []int32
				for i := range cells.Rows {
					cells.Stratum[i] = nc.Assign[i] + 1
					if nc.Assign[i] >= 0 {
						rows, groups = append(rows, cells.Rows[i]), append(groups, nc.Assign[i])
					}
				}
				res := &spatial.NichesResult{Niches: nc.Count, Windows: nc.Windows, Cells: nc.Cells, Composition: nc.Composition, Counts: nc.Counts, Tissue: nc.Tissue, Inertia: nc.Inertia, Inertias: nc.Inertias, Agreement: nc.Agreement, Markers: make([][]cluster.Marker, nc.Count)}
				for v := 0; v < nc.Count; v++ {
					res.Defining = append(res.Defining, spatial.DefiningLabels(nc.Composition[v*K:(v+1)*K], nc.Tissue))
				}
				// Each niche's leading genes against the other niches' cells.
				r.Progress(0.8, "Each niche's genes")
				counts, err := segment.ReadCounts(dir)
				if err != nil {
					return err
				}
				if mr, err := cluster.FromLabels(counts, rows, groups, cluster.Source{Tool: "SituCraft", Name: "niches"}, runtime.NumCPU()); err != nil {
					r.Log("warn", "The niches' genes were not found: %v.", err)
				} else {
					res.MarkerCells = len(mr.Cells)
					for v := 0; v < nc.Count; v++ {
						res.Markers[v] = cluster.TopMarkers(mr.Markers, mr.Genes, v, 10)
					}
				}
				if len(nc.Agreement) > 0 {
					lo := 1.0
					for _, v := range nc.Agreement {
						lo = math.Min(lo, v)
					}
					r.Log("info", "The niches from %d further seeds agree with these at an adjusted Rand index of %.3f at least.", len(nc.Agreement), lo)
				}
				out.Niches = res
				name = fmt.Sprintf("Niches of %s in %s", spec.Labels.Name, sm.Name)
			case spatial.QuestionRegions, spatial.QuestionBoundary:
				win, ref, err := a.spatialWindowOf(ctx, dir, spec.Window, r)
				if err != nil {
					return err
				}
				out.Window = ref
				var regions []*spatial.Window
				var refs []spatial.Region
				var names []string
				for _, sr := range spec.Regions {
					rw, err := spatial.PolygonWindow(sr.Geometry)
					if err != nil {
						return fmt.Errorf("the region %s: %w", sr.Region.Name, err)
					}
					regions, refs, names = append(regions, rw), append(refs, sr.Region), append(names, sr.Region.Name)
				}
				outside := 0
				if spec.Question == spatial.QuestionRegions {
					var p spatial.RegionsParams
					if err := json.Unmarshal(spec.Parameters, &p); err != nil {
						return err
					}
					r.Log("info", "Regions compared (%s) by %s in %s; the tissue %s µm².", strings.Join(names, ", "), spec.Labels.Name, sm.Name, strconv.FormatFloat(ref.Area, 'f', 0, 64))
					r.Progress(0.1, "Measuring the tissue within each region")
					c, err := spatial.Compose(ctx, x, y, cells.Label, K, win, regions, p, runtime.NumCPU())
					if err != nil {
						return err
					}
					copy(cells.Stratum, c.Membership)
					outside = c.Outside
					out.Regions = &spatial.RegionsResult{Regions: refs, Area: c.Area, Labeled: c.Labeled, All: c.All, Count: c.Count, Density: c.Density, DensityLo: c.DensityLo, DensityHi: c.DensityHi, Share: c.Share, ShareLo: c.ShareLo, ShareHi: c.ShareHi,
						DensityRatio: c.DensityRatio, DensityRatioLo: c.DensityRatioLo, DensityRatioHi: c.DensityRatioHi, ShareRatio: c.ShareRatio, ShareRatioLo: c.ShareRatioLo, ShareRatioHi: c.ShareRatioHi, Outside: c.Outside}
					name = fmt.Sprintf("Regions compared (%s) by %s in %s", strings.Join(names, ", "), spec.Labels.Name, sm.Name)
				} else {
					var p spatial.BoundaryParams
					if err := json.Unmarshal(spec.Parameters, &p); err != nil {
						return err
					}
					r.Log("info", "Across the edge of %s by %s in %s: bands of %g µm to %g µm either side, a margin of %g µm; the tissue %s µm².", names[0], spec.Labels.Name, sm.Name, p.Step, p.Reach, p.Margin, strconv.FormatFloat(ref.Area, 'f', 0, 64))
					r.Progress(0.1, "Measuring the tissue in each band")
					b, err := spatial.AcrossEdge(ctx, x, y, cells.Label, K, win, regions[0], p, runtime.NumCPU())
					if err != nil {
						return err
					}
					copy(cells.Stratum, b.Band)
					outside = b.Outside
					out.Boundary = &spatial.BoundaryResult{Region: refs[0], Edges: b.Edges, Area: b.Area, Labeled: b.Labeled, All: b.All, Count: b.Count, Density: b.Density, DensityLo: b.DensityLo, DensityHi: b.DensityHi, Share: b.Share, ShareLo: b.ShareLo, ShareHi: b.ShareHi,
						ZoneArea: b.ZoneArea, ZoneLabeled: b.ZoneLabeled, ZoneCount: b.ZoneCount, ZoneDensity: b.ZoneDensity, ZoneLo: b.ZoneLo, ZoneHi: b.ZoneHi, Ratio: b.Ratio, RatioLo: b.RatioLo, RatioHi: b.RatioHi, Inside: b.Inside, AtMargin: b.AtMargin, Outside: b.Outside}
					name = fmt.Sprintf("Across the edge of %s, %s in %s", names[0], spec.Labels.Name, sm.Name)
				}
				if outside > 0 {
					r.Log("warn", "%d labeled cells lie outside the tissue window and are left out.", outside)
				}
			}
		}
		out.Cells.Analyzed = len(cells.Rows)
		for _, l := range cells.Label {
			if l >= 0 {
				out.Cells.Labeled++
			}
		}
		if spec.Question != spatial.QuestionMoran {
			if out.Cells.Labeled < 2 {
				return errors.New("fewer than two of the cells analyzed have a label")
			}
			r.Log("info", "%d cells analyzed, %d of them labeled with %d labels.", out.Cells.Analyzed, out.Cells.Labeled, K)
		}
		stage, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := spatial.WriteAnalysis(stage, out, cells); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		if spec.Question == spatial.QuestionMoran {
			if err := spatial.WriteLocal(stage, len(cells.Rows), local); err != nil {
				a.artifacts.Discard(id)
				return err
			}
		}
		r.Validating()
		r.Progress(0.95, "Checking the analysis")
		jobSpec, _ := json.Marshal(spec)
		var files []artifacts.File
		for _, f := range spatial.AnalysisFilesOf(spec.Question) {
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

// spatialWindowOf makes an analysis's window: the tissue from the segmentation's label raster, or
// (Ripley's K) a region's polygons; with what the artifact records of it.
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

// spatialLocal answers a Moran's I analysis's local values, little-endian: the cells' count (u32),
// the genes' (u32), the genes (u32 indices), each gene's local I for every cell (f32), their
// p-values (f32), and their classes (a byte each: 0 none, 1 high among high, 2 low among low, 3
// high among low, 4 low among high).
func (a *app) spatialLocal(w http.ResponseWriter, r *http.Request) {
	m, err := a.artifacts.Get(r.PathValue("id"))
	if err != nil || m.Kind != "spatial" || m.State != artifacts.Complete {
		writeError(w, http.StatusNotFound, "No such analysis.")
		return
	}
	p, err := a.artifacts.Path(m.ID, spatial.FileLocal)
	if err != nil {
		writeError(w, http.StatusNotFound, "The analysis has no local values.")
		return
	}
	local, err := spatial.ReadLocal(p[:len(p)-len(spatial.FileLocal)])
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	n := 0
	if len(local) > 0 {
		n = len(local[0].I)
	}
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint32(n))
	binary.Write(&buf, binary.LittleEndian, uint32(len(local)))
	for _, l := range local {
		binary.Write(&buf, binary.LittleEndian, uint32(l.Gene))
	}
	for _, l := range local {
		binary.Write(&buf, binary.LittleEndian, l.I)
	}
	for _, l := range local {
		binary.Write(&buf, binary.LittleEndian, l.P)
	}
	for _, l := range local {
		binary.Write(&buf, binary.LittleEndian, l.Class)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(buf.Bytes())
}
