package main

// Graphs of a segmentation's cells (internal/spatial), made by a job and recorded as artifacts of
// kind "graph". A graph is named by the segmentation, the cells chosen (by their checksum), the
// method's version and its definition: asking again answers it without a job. The page chooses the
// cells (all, a QC filter's, those in regions, never those a review excluded) and sends them with
// how they were chosen, which the graph records.

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
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

// graphCitation cites a Delaunay graph's methods (a nearest-neighbor, radius or contact graph cites
// none: each is its definition).
const graphCitation = "Lee DT, Schachter BJ. Two algorithms for constructing a Delaunay triangulation. Int J Comput Inf Sci 1980;9:219–242. doi:10.1007/BF00977785; " +
	"Bowyer A. Computing Dirichlet tessellations. Comput J 1981;24:162–166; Watson DF. Computing the n-dimensional Delaunay tessellation with application to Voronoi polytopes. Comput J 1981;24:167–172; " +
	"Shewchuk JR. Adaptive precision floating-point arithmetic and fast robust geometric predicates. Discrete Comput Geom 1997;18:305–363. doi:10.1007/PL00009321"

// graphCitationOf cites a graph's kind.
func graphCitationOf(kind string) string {
	if kind == spatial.Delaunay {
		return graphCitation
	}
	return ""
}

// graphCells are the cells a graph is built over, as the page sends them: the bits of
// lib/filters.js's keepChecksum (base64), their checksum, and how they were chosen.
type graphCells struct {
	Keep     string           `json:"keep"`
	Checksum string           `json:"checksum"`
	Filter   *spatial.Filter  `json:"filter,omitempty"`
	Regions  []spatial.Region `json:"regions,omitempty"`
	Excluded int              `json:"excluded,omitempty"`
}

func (c *graphCells) choice() spatial.Choice {
	return spatial.Choice{Checksum: c.Checksum, Filter: c.Filter, Regions: c.Regions, Excluded: c.Excluded}
}

type graphRequest struct {
	Segmentation string          `json:"segmentation"`
	Parameters   *spatial.Params `json:"parameters,omitempty"`
	Cells        *graphCells     `json:"cells"`
}

type graphSpec struct {
	Bundle       string         `json:"bundle"`
	Segmentation string         `json:"segmentation"`
	Parameters   spatial.Params `json:"parameters"`
	Cells        graphCells     `json:"cells"`
}

// graphID names a graph by what it is made from.
func graphID(segmentation, cells string, p spatial.Params) string {
	params, _ := json.Marshal(p)
	h := sha256.Sum256([]byte(segmentation + "\n" + cells + "\n" + spatial.MethodVersion + "\n" + string(params)))
	return "gra-" + hex.EncodeToString(h[:10])
}

// graphName names a graph for people.
func graphName(seg string, p spatial.Params, c graphCells) string {
	num := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	var name string
	switch p.Kind {
	case spatial.Delaunay:
		name = "Delaunay graph of " + seg
		if p.Longest > 0 {
			name += ", edges to " + num(p.Longest) + " µm"
		}
	case spatial.KNN:
		name = fmt.Sprintf("%d nearest neighbors in %s", p.K, seg)
	case spatial.Radius:
		name = "Neighbors within " + num(p.Radius) + " µm in " + seg
	case spatial.Contact:
		name = "Contact graph of " + seg
		if p.Gap > 0 {
			name += ", gaps to " + num(p.Gap) + " µm"
		}
	}
	if c.Filter != nil {
		name += ", filter " + c.Filter.Name
	}
	if len(c.Regions) > 0 {
		names := make([]string, len(c.Regions))
		for i, r := range c.Regions {
			names[i] = r.Name
		}
		name += ", in " + strings.Join(names, ", ")
	}
	return name
}

func (a *app) registerGraphs(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/graph/defaults", func(w http.ResponseWriter, r *http.Request) {
		defaults := map[string]spatial.Params{}
		for _, k := range spatial.Kinds {
			defaults[k] = spatial.DefaultParams(k)
		}
		writeJSON(w, map[string]any{"kinds": spatial.Kinds, "defaults": defaults, "method": spatial.MethodVersion, "citation": graphCitation})
	})
	mux.HandleFunc("POST /api/local/{index}/graph", a.startGraph)
	mux.HandleFunc("GET /api/graphs/{id}/edges", a.graphEdges)
}

// startGraph starts the job building a graph of a segmentation's cells, answering {id, artifact,
// job}: the artifact at once when it was made already.
func (a *app) startGraph(w http.ResponseWriter, r *http.Request) {
	if !a.needCache(w) {
		return
	}
	var body graphRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&body); err != nil || body.Segmentation == "" || body.Cells == nil {
		writeError(w, http.StatusBadRequest, "Send {\"segmentation\": artifact id, \"parameters\": {\"kind\", …}, \"cells\": {\"keep\", \"checksum\", …}}.")
		return
	}
	p := spatial.DefaultParams(spatial.Delaunay)
	if body.Parameters != nil {
		p = *body.Parameters
	}
	if err := p.Check(); err != nil {
		writeError(w, http.StatusBadRequest, "The graph: "+err.Error()+".")
		return
	}
	if len(body.Cells.Checksum) != 64 {
		writeError(w, http.StatusBadRequest, "The cells need their bits and checksum.")
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
	if json.Unmarshal(sm.Summary, &summary) == nil && summary.Cells > 0 {
		f := clusterFilter{Keep: body.Cells.Keep, Checksum: body.Cells.Checksum}
		if _, err := f.keep(summary.Cells); err != nil {
			writeError(w, http.StatusBadRequest, "The cells: "+err.Error()+".")
			return
		}
	}
	id := graphID(sm.ID, body.Cells.Checksum, p)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	spec := graphSpec{Bundle: path, Segmentation: sm.ID, Parameters: p, Cells: *body.Cells}
	job, _, err := a.jobs.Submit("graph", "Graph of "+sm.Name+" of "+name, "graph\x00"+id, spec, a.graphJob(spec, id))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

// graphJob builds a graph of a segmentation's cells and records it.
func (a *app) graphJob(spec graphSpec, id string) jobs.Func {
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
		r.Log("info", "Building a graph of the cells of %s.", sm.Name)
		r.Progress(0, "Reading "+sm.Name)
		dir, err := a.segmentationFolder(ctx, b, sm)
		if err != nil {
			return err
		}
		cells, err := segment.ReadCells(dir)
		if err != nil {
			return err
		}
		f := clusterFilter{Keep: spec.Cells.Keep, Checksum: spec.Cells.Checksum}
		keep, err := f.keep(len(cells))
		if err != nil {
			return fmt.Errorf("the cells chosen: %w", err)
		}
		in := spatial.Inputs{X: make([]float32, len(cells)), Y: make([]float32, len(cells)), Keep: keep}
		for i, c := range cells {
			in.X[i], in.Y[i] = c.X, c.Y
		}
		if spec.Parameters.Kind == spatial.Contact {
			r.Progress(0.05, "Reading the label raster")
			if in.Mask, err = segment.ReadMask(dir); err != nil {
				return err
			}
		}
		g, err := spatial.Build(ctx, in, spec.Parameters, runtime.NumCPU(), func(f float64, message string) { r.Progress(0.05+0.8*f, message) })
		if err != nil {
			return err
		}
		choice := spec.Cells.choice()
		d := spatial.Describe(g, spatial.Side{ID: sm.ID, Name: sm.Name}, len(cells), spec.Parameters, choice)
		r.Log("info", "%d cells of %d in the graph; %d edges, %.2f neighbors a cell on average, %d cells without one.", len(g.Rows), len(cells), d.Edges, d.Degree.Mean, d.Isolated)
		if g.Duplicates > 0 {
			r.Log("warn", "%d cells lie at the same place as another: each is joined to it and its neighbors.", g.Duplicates)
		}
		stage, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := spatial.Write(stage, g, d); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Validating()
		r.Progress(0.95, "Checking the graph")
		var parents []artifacts.Parent
		for _, p := range sm.Lineage.Parents {
			if p.Kind == "bundle" {
				parents = append(parents, p)
			}
		}
		parents = append(parents, artifacts.Parent{Kind: "segmentation", ID: sm.ID, Name: sm.Name})
		params, _ := json.Marshal(spec.Parameters)
		jobSpec, _ := json.Marshal(spec)
		var files []artifacts.File
		for _, f := range spatial.Files {
			files = append(files, artifacts.File{Path: f, Role: spatial.Roles[f]})
		}
		m := artifacts.Manifest{
			ID:   id,
			Kind: "graph",
			Name: graphName(sm.Name, spec.Parameters, spec.Cells),
			Lineage: artifacts.Lineage{
				Parents:    parents,
				Job:        &artifacts.Job{ID: r.ID(), Kind: "graph", Spec: jobSpec},
				Method:     artifacts.Method{Name: "SituCraft cell graph", Version: spatial.MethodVersion, Citation: graphCitationOf(spec.Parameters.Kind)},
				Parameters: params,
				Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: files,
		}
		recorded, err := a.artifacts.Promote(m, spatial.Check)
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
			return fmt.Errorf("%w: the graph's check found fatal problems%s (artifact %s, kept for review)", jobs.ErrInvalid, first, recorded.ID)
		}
		r.Log("info", "Recorded the graph as artifact %s.", recorded.ID)
		return nil
	}
}

// graphOf reads a complete graph's folder.
func (a *app) graphOf(w http.ResponseWriter, id string) (string, *spatial.Description, bool) {
	m, err := a.artifacts.Get(id)
	if err != nil || m.Kind != "graph" || m.State != artifacts.Complete {
		writeError(w, http.StatusNotFound, "No such graph.")
		return "", nil, false
	}
	p, err := a.artifacts.Path(m.ID, spatial.FileDescription)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", nil, false
	}
	dir := p[:len(p)-len(spatial.FileDescription)]
	d, err := spatial.ReadDescription(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return "", nil, false
	}
	return dir, d, true
}

// graphEdges answers a graph for the page, little-endian: the cells in it (n), the edge ends (m),
// whether it holds shared boundaries (a byte, then three of padding); the cells' rows of the
// segmentation (n × u32) and centroids (µm; x, then y, n × f32 each); the compressed rows' offsets
// ((n + 1) × u32); each edge end's neighbor (an index among the graph's cells; m × u32) and length
// (µm; m × f32); and, in a contact graph, each one's shared boundary (µm; m × f32).
func (a *app) graphEdges(w http.ResponseWriter, r *http.Request) {
	dir, _, ok := a.graphOf(w, r.PathValue("id"))
	if !ok {
		return
	}
	g, err := spatial.Read(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	n, m := len(g.Rows), len(g.Neighbors)
	size := 12 + 12*n + 4*(n+1) + 8*m
	if g.Shared != nil {
		size += 4 * m
	}
	out := make([]byte, 0, size)
	out = binary.LittleEndian.AppendUint32(out, uint32(n))
	out = binary.LittleEndian.AppendUint32(out, uint32(m))
	shared := byte(0)
	if g.Shared != nil {
		shared = 1
	}
	out = append(out, shared, 0, 0, 0)
	for _, v := range g.Rows {
		out = binary.LittleEndian.AppendUint32(out, v)
	}
	for _, v := range append(append([]float32(nil), g.X...), g.Y...) {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
	}
	for _, v := range g.Offsets {
		out = binary.LittleEndian.AppendUint32(out, v)
	}
	for _, v := range g.Neighbors {
		out = binary.LittleEndian.AppendUint32(out, v)
	}
	for _, v := range g.Length {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
	}
	for _, v := range g.Shared {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(out)
}
