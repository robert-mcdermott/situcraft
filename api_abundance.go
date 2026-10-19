package main

// Differential abundance (wave 7, slice 6; internal/cohort Abundance): POST /api/cohort/abundance
// {summary, design, families, level} starts a job comparing the summary's samples by the design
// (web/lib/design.js makes it: its columns, its rows of units in groups with their samples, its
// contrasts), recorded as an artifact of kind abundance made from the summary.

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

// abundanceCitation cites the comparison's methods.
const abundanceCitation = "Wedderburn RWM. Quasi-likelihood functions, generalized linear models, and the Gauss–Newton method. Biometrika 1974;61:439–447; " +
	"McCullagh P, Nelder JA. Generalized Linear Models, 2nd ed. Chapman and Hall, 1989; " +
	"Benjamini Y, Hochberg Y. Controlling the false discovery rate: a practical and powerful approach to multiple testing. J R Stat Soc B 1995;57:289–300."

type abundanceRequest struct {
	Summary  string                  `json:"summary"`
	Design   cohort.ComparisonDesign `json:"design"`
	Families []string                `json:"families"`
	Level    float64                 `json:"level"`
}

// abundanceID names a comparison by what it is of.
func abundanceID(body abundanceRequest) string {
	raw, _ := json.Marshal(body)
	h := sha256.Sum256(append(raw, []byte("\n"+cohort.AbundanceMethodVersion)...))
	return "abu-" + hex.EncodeToString(h[:10])
}

// checkAbundance fills a request's defaults and says what is wrong with it: its summary complete,
// its design the summary's samples', its families known.
func (a *app) checkAbundance(body *abundanceRequest) (*cohort.Document, artifacts.Manifest, error) {
	var none artifacts.Manifest
	if len(body.Families) == 0 {
		body.Families = []string{cohort.FamilyShare, cohort.FamilyDensity}
	}
	if body.Level == 0 {
		body.Level = 0.95
	}
	seen := map[string]bool{}
	for _, f := range body.Families {
		if f != cohort.FamilyShare && f != cohort.FamilyDensity && f != cohort.FamilyNiche {
			return nil, none, fmt.Errorf("no family %q to compare: share, density or niche", f)
		}
		if seen[f] {
			return nil, none, fmt.Errorf("the family %s twice", f)
		}
		seen[f] = true
	}
	m, err := a.artifacts.Get(body.Summary)
	if err != nil || m.Kind != "cohort-summary" {
		return nil, none, fmt.Errorf("no cohort summary %s", body.Summary)
	}
	if m.State != artifacts.Complete {
		return nil, none, fmt.Errorf("the summary %s is %s", body.Summary, m.State)
	}
	p, err := a.artifacts.Path(m.ID, cohort.FileSummary)
	if err != nil {
		return nil, none, err
	}
	doc, _, err := cohort.Read(filepath.Dir(p))
	if err != nil {
		return nil, none, err
	}
	if err := cohort.CheckDesign(doc, body.Design); err != nil {
		return nil, none, err
	}
	if seen[cohort.FamilyNiche] && doc.Niches == nil {
		return nil, none, errors.New("the summary found no niches to compare")
	}
	if !(body.Level > 0.5 && body.Level < 1) {
		return nil, none, errors.New("the intervals' level is above 0.5 and below 1")
	}
	return doc, m, nil
}

func (a *app) startAbundance(w http.ResponseWriter, r *http.Request) {
	var body abundanceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Send {\"summary\": id, \"design\": {names, rows, contrasts}, \"families\": [share, density, niche], \"level\"}.")
		return
	}
	if _, _, err := a.checkAbundance(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := abundanceID(body)
	if m, err := a.artifacts.Get(id); err == nil {
		writeJSON(w, map[string]any{"id": id, "artifact": m})
		return
	}
	job, _, err := a.jobs.Submit("abundance", fmt.Sprintf("Abundance compared over %d rows", len(body.Design.Rows)), "abundance\x00"+id, body, a.abundanceJob(body, id))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"id": id, "artifact": nil, "job": job})
}

// abundanceJob compares the summary's samples and records the comparison.
func (a *app) abundanceJob(body abundanceRequest, id string) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		doc, sm, err := a.checkAbundance(&body)
		if err != nil {
			return err
		}
		r.Progress(0.2, "Fitting each label's model")
		c, err := cohort.Abundance(doc, body.Design, body.Families, body.Level)
		if err != nil {
			return err
		}
		c.Summary = cohort.SummaryRef{ID: sm.ID, Name: sm.Name}
		for _, f := range c.Families {
			r.Log("info", "%s: %s.", f.Name, f.Universe)
		}
		stage, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		if err := cohort.WriteComparison(stage, c); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Validating()
		var names []string
		for _, ct := range body.Design.Contrasts {
			names = append(names, ct.Name)
		}
		jobSpec, _ := json.Marshal(body)
		params, _ := json.Marshal(map[string]any{"families": body.Families, "level": body.Level, "design": map[string]any{"name": body.Design.Name, "version": body.Design.Version, "names": body.Design.Names, "contrasts": body.Design.Contrasts}})
		var files []artifacts.File
		for _, f := range []string{cohort.FileAbundance, cohort.FileAbundanceResults} {
			files = append(files, artifacts.File{Path: f, Role: cohort.AbundanceRoles[f]})
		}
		m := artifacts.Manifest{
			ID:   id,
			Kind: "abundance",
			Name: "Abundance compared: " + strings.Join(names, ", "),
			Lineage: artifacts.Lineage{
				Parents:    []artifacts.Parent{{Kind: "cohort-summary", ID: sm.ID, Name: sm.Name}},
				Job:        &artifacts.Job{ID: r.ID(), Kind: "abundance", Spec: jobSpec},
				Method:     artifacts.Method{Name: "SituCraft differential abundance", Version: cohort.AbundanceMethodVersion, Citation: abundanceCitation},
				Parameters: params,
				Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: files,
		}
		recorded, err := a.artifacts.Promote(m, cohort.CheckComparison)
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
}
