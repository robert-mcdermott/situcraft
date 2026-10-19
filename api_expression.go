package main

// Differential expression and spatial statistics between groups (wave 7, slice 7; internal/cohort
// CompareExpression and CompareSpatial): POST /api/cohort/expression {summary, design, level,
// minCells} starts a job comparing each label's pseudobulk by the design, recorded as an artifact of
// kind expression; GET /api/cohort/expression/values?id=&label=&gene= answers one gene's values in a
// label's rows for its plot; POST /api/cohort/spatial-tests {summary, design, families, level}
// starts a job comparing the samples' spatial statistics, recorded as an artifact of kind
// spatial-comparison. Both are made from the summary.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/cohort"
	"situcraft/internal/jobs"
)

// expressionCitation cites the expression comparison's methods.
const expressionCitation = "Crowell HL, Soneson C, Germain P-L, et al. muscat detects subpopulation-specific state transitions from multi-sample multi-condition single-cell transcriptomics data. Nat Commun 2020;11:6077; " +
	"Chen Y, Lun ATL, Smyth GK. From reads to genes to pathways: differential expression analysis of RNA-Seq experiments using Rsubread and the edgeR quasi-likelihood pipeline. F1000Research 2016;5:1438; " +
	"Robinson MD, Oshlack A. A scaling normalization method for differential expression analysis of RNA-seq data. Genome Biol 2010;11:R25; " +
	"Law CW, Chen Y, Shi W, Smyth GK. voom: precision weights unlock linear model analysis tools for RNA-seq read counts. Genome Biol 2014;15:R29; " +
	"Smyth GK. Linear models and empirical Bayes methods for assessing differential expression in microarray experiments. Stat Appl Genet Mol Biol 2004;3:3; " +
	"Benjamini Y, Hochberg Y. Controlling the false discovery rate: a practical and powerful approach to multiple testing. J R Stat Soc B 1995;57:289–300."

// spatialTestsCitation cites the spatial comparison's methods.
const spatialTestsCitation = "Smyth GK. Linear models and empirical Bayes methods for assessing differential expression in microarray experiments. Stat Appl Genet Mol Biol 2004;3:3; " +
	"Benjamini Y, Hochberg Y. Controlling the false discovery rate: a practical and powerful approach to multiple testing. J R Stat Soc B 1995;57:289–300."

type expressionRequest struct {
	Summary  string                  `json:"summary"`
	Design   cohort.ComparisonDesign `json:"design"`
	Level    float64                 `json:"level"`
	MinCells int                     `json:"minCells"`
}

type spatialTestsRequest struct {
	Summary  string                  `json:"summary"`
	Design   cohort.ComparisonDesign `json:"design"`
	Families []string                `json:"families"`
	Level    float64                 `json:"level"`
}

// testID names a comparison by its kind's prefix and what it is of.
func testID(prefix, methodVersion string, body any) string {
	raw, _ := json.Marshal(body)
	h := sha256.Sum256(append(raw, []byte("\n"+methodVersion)...))
	return prefix + hex.EncodeToString(h[:10])
}

func expressionID(body expressionRequest) string {
	return testID("exp-", cohort.ExpressionMethodVersion, body)
}

func spatialTestsID(body spatialTestsRequest) string {
	return testID("spt-", cohort.SpatialTestsMethodVersion, body)
}

// completeSummary reads a complete cohort summary's document and pseudobulk.
func (a *app) completeSummary(id string) (*cohort.Document, [][][]uint32, artifacts.Manifest, error) {
	var none artifacts.Manifest
	m, err := a.artifacts.Get(id)
	if err != nil || m.Kind != "cohort-summary" {
		return nil, nil, none, fmt.Errorf("no cohort summary %s", id)
	}
	if m.State != artifacts.Complete {
		return nil, nil, none, fmt.Errorf("the summary %s is %s", id, m.State)
	}
	p, err := a.artifacts.Path(m.ID, cohort.FileSummary)
	if err != nil {
		return nil, nil, none, err
	}
	doc, pb, err := cohort.Read(filepath.Dir(p))
	if err != nil {
		return nil, nil, none, err
	}
	return doc, pb, m, nil
}

// checkLevel says what is wrong with an intervals' level (0 the default, 0.95).
func checkLevel(level *float64) error {
	if *level == 0 {
		*level = 0.95
	}
	if !(*level > 0.5 && *level < 1) {
		return errors.New("the intervals' level is above 0.5 and below 1")
	}
	return nil
}

// checkExpression fills a request's defaults and says what is wrong with it.
func (a *app) checkExpression(body *expressionRequest) (*cohort.Document, [][][]uint32, artifacts.Manifest, error) {
	var none artifacts.Manifest
	if err := checkLevel(&body.Level); err != nil {
		return nil, nil, none, err
	}
	if body.MinCells == 0 {
		body.MinCells = cohort.MinLabelCells
	}
	if body.MinCells < 1 || body.MinCells > 10000 {
		return nil, nil, none, errors.New("the fewest cells of a label a row is tested with is 1 to 10,000")
	}
	doc, pb, m, err := a.completeSummary(body.Summary)
	if err != nil {
		return nil, nil, none, err
	}
	if err := cohort.CheckDesign(doc, body.Design); err != nil {
		return nil, nil, none, err
	}
	return doc, pb, m, nil
}

// checkSpatialTests fills a request's defaults (every family the summary has) and says what is
// wrong with it.
func (a *app) checkSpatialTests(body *spatialTestsRequest) (*cohort.Document, artifacts.Manifest, error) {
	var none artifacts.Manifest
	if err := checkLevel(&body.Level); err != nil {
		return nil, none, err
	}
	doc, _, m, err := a.completeSummary(body.Summary)
	if err != nil {
		return nil, none, err
	}
	if err := cohort.CheckDesign(doc, body.Design); err != nil {
		return nil, none, err
	}
	has := cohort.SpatialFamiliesOf(doc)
	if len(body.Families) == 0 {
		body.Families = has
	}
	if len(body.Families) == 0 {
		return nil, none, errors.New("the summary's samples have no spatial statistics: answer a spatial question in the steps on every sample (neighbor enrichment, nearest distances, Moran's I)")
	}
	seen := map[string]bool{}
	for _, f := range body.Families {
		known := false
		for _, g := range cohort.SpatialFamilies {
			known = known || g.ID == f
		}
		if !known {
			return nil, none, fmt.Errorf("no family %q to compare: enrichment, nearest or moran (a niche's share is compared with the abundances)", f)
		}
		if seen[f] {
			return nil, none, fmt.Errorf("the family %s twice", f)
		}
		seen[f] = true
		if !strings.Contains(" "+strings.Join(has, " ")+" ", " "+f+" ") {
			return nil, none, fmt.Errorf("the summary's samples have no %s statistics", f)
		}
	}
	return doc, m, nil
}

func (a *app) startExpression(w http.ResponseWriter, r *http.Request) {
	var body expressionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Send {\"summary\": id, \"design\": {names, rows, contrasts}, \"level\", \"minCells\"}.")
		return
	}
	if _, _, _, err := a.checkExpression(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := expressionID(body)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	job, _, err := a.jobs.Submit("expression", fmt.Sprintf("Expression compared over %d rows", len(body.Design.Rows)), "expression\x00"+id, body, a.expressionJob(body, id))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

func (a *app) startSpatialTests(w http.ResponseWriter, r *http.Request) {
	var body spatialTestsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Send {\"summary\": id, \"design\": {names, rows, contrasts}, \"families\": [enrichment, nearest, moran], \"level\"}.")
		return
	}
	if _, _, err := a.checkSpatialTests(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := spatialTestsID(body)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	job, _, err := a.jobs.Submit("spatial-tests", fmt.Sprintf("Spatial statistics compared over %d rows", len(body.Design.Rows)), "spatial-tests\x00"+id, body, a.spatialTestsJob(body, id))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

// comparisonRecord is a comparison to record: its artifact's id, kind and name, its job's kind and
// specification, its parameters and method, its files (in order, with their roles), how they are
// written and checked.
type comparisonRecord struct {
	id, kind, name, jobKind string
	spec, params            any
	method                  artifacts.Method
	files                   []string
	roles                   map[string]string
	write                   func(dir string) error
	check                   func(string, *artifacts.Manifest) []artifacts.Finding
}

// recordComparison stages a comparison's files, promotes them as an artifact made from the
// summary, checked, and logs it.
func (a *app) recordComparison(r *jobs.Run, sm artifacts.Manifest, c comparisonRecord) error {
	id := c.id
	stage, err := a.artifacts.Stage(id)
	if err != nil {
		return err
	}
	if err := c.write(stage); err != nil {
		a.artifacts.Discard(id)
		return err
	}
	r.Validating()
	jobSpec, _ := json.Marshal(c.spec)
	rawParams, _ := json.Marshal(c.params)
	var files []artifacts.File
	for _, f := range c.files {
		files = append(files, artifacts.File{Path: f, Role: c.roles[f]})
	}
	m := artifacts.Manifest{
		ID:   id,
		Kind: c.kind,
		Name: c.name,
		Lineage: artifacts.Lineage{
			Parents:    []artifacts.Parent{{Kind: "cohort-summary", ID: sm.ID, Name: sm.Name}},
			Job:        &artifacts.Job{ID: r.ID(), Kind: c.jobKind, Spec: jobSpec},
			Method:     c.method,
			Parameters: rawParams,
			Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
		},
		Files: files,
	}
	recorded, err := a.artifacts.Promote(m, c.check)
	if err != nil {
		a.artifacts.Discard(id)
		return err
	}
	if recorded.State == artifacts.Invalid {
		return fmt.Errorf("%w: the comparison's check found fatal problems (artifact %s, kept for review)", jobs.ErrInvalid, recorded.ID)
	}
	r.Log("info", "Recorded the comparison as artifact %s.", recorded.ID)
	return nil
}

func contrastNames(d cohort.ComparisonDesign) string {
	var names []string
	for _, ct := range d.Contrasts {
		names = append(names, ct.Name)
	}
	return strings.Join(names, ", ")
}

func designParams(d cohort.ComparisonDesign) map[string]any {
	p := map[string]any{"name": d.Name, "version": d.Version, "names": d.Names, "contrasts": d.Contrasts}
	if d.PseudoSamples {
		p["pseudoSamples"] = true
	}
	return p
}

// expressionJob compares the summary's labels' pseudobulk and records the comparison.
func (a *app) expressionJob(body expressionRequest, id string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		doc, pb, sm, err := a.checkExpression(&body)
		if err != nil {
			return err
		}
		r.Progress(0.2, "Fitting each label's genes")
		e, err := cohort.CompareExpression(doc, pb, body.Design, body.Level, body.MinCells)
		if err != nil {
			return err
		}
		e.Summary = cohort.SummaryRef{ID: sm.ID, Name: sm.Name}
		r.Log("info", "%s.", e.Universe)
		for _, res := range e.Results {
			if !res.Tested {
				r.Log("info", "%s: untested (%s).", res.Label, res.Note)
			}
		}
		params := map[string]any{"level": body.Level, "minCells": body.MinCells, "design": designParams(body.Design)}
		method := artifacts.Method{Name: "SituCraft differential expression", Version: cohort.ExpressionMethodVersion, Citation: expressionCitation}
		return a.recordComparison(r, sm, comparisonRecord{id: id, kind: "expression", name: "Expression compared: " + contrastNames(body.Design), jobKind: "expression",
			spec: body, params: params, method: method, files: []string{cohort.FileExpression, cohort.FileExpressionResults}, roles: cohort.ExpressionRoles,
			write: func(dir string) error { return cohort.WriteExpression(dir, e) }, check: cohort.CheckExpression})
	}
}

// spatialTestsJob compares the summary's samples' spatial statistics and records the comparison.
func (a *app) spatialTestsJob(body spatialTestsRequest, id string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		doc, sm, err := a.checkSpatialTests(&body)
		if err != nil {
			return err
		}
		r.Progress(0.2, "Fitting each family's statistics")
		c, err := cohort.CompareSpatial(doc, body.Design, body.Families, body.Level)
		if err != nil {
			return err
		}
		c.Summary = cohort.SummaryRef{ID: sm.ID, Name: sm.Name}
		for _, f := range c.Families {
			r.Log("info", "%s: %s.", f.Name, f.Universe)
		}
		params := map[string]any{"families": body.Families, "level": body.Level, "design": designParams(body.Design)}
		method := artifacts.Method{Name: "SituCraft spatial statistics compared", Version: cohort.SpatialTestsMethodVersion, Citation: spatialTestsCitation}
		return a.recordComparison(r, sm, comparisonRecord{id: id, kind: "spatial-comparison", name: "Spatial statistics compared: " + contrastNames(body.Design), jobKind: "spatial-tests",
			spec: body, params: params, method: method, files: []string{cohort.FileSpatialTests, cohort.FileSpatialResults}, roles: cohort.SpatialTestsRoles,
			write: func(dir string) error { return cohort.WriteSpatialComparison(dir, c) }, check: cohort.CheckSpatialComparison})
	}
}

// expressionValues answers one gene's values in a label's rows (GET /api/cohort/expression/values
// ?id=&label=&gene=): each used row's unit, group, samples, count, library size and log2 count per
// million.
func (a *app) expressionValues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	m, err := a.artifacts.Get(q.Get("id"))
	if err != nil || m.Kind != "expression" || m.State != artifacts.Complete {
		writeError(w, http.StatusNotFound, "no complete expression comparison "+q.Get("id"))
		return
	}
	p, err := a.artifacts.Path(m.ID, cohort.FileExpression)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	e, err := cohort.ReadExpression(filepath.Dir(p))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	doc, pb, _, err := a.completeSummary(e.Summary.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	v, err := e.ValuesOf(doc, pb, q.Get("label"), q.Get("gene"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	type row struct {
		Row     int      `json:"row"`
		Unit    string   `json:"unit"`
		Group   string   `json:"group"`
		Samples []string `json:"samples"`
		Count   float64  `json:"count"`
		Library float64  `json:"library"`
		LogCPM  float64  `json:"logCPM"`
	}
	rows := []row{}
	for i, k := range v.Rows {
		d := e.Design.Rows[k]
		rows = append(rows, row{k, d.Unit, d.Group, d.Samples, v.Counts[i], v.Lib[i], v.LogCPM[i]})
	}
	writeJSON(w, map[string]any{"label": v.Label, "gene": v.Gene, "rows": rows})
}
