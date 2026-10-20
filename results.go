package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/label"
	"situcraft/internal/spatial"
)

// Answers the program gives itself, without the window (wave 9, slice 2; requirement A2): the
// library's projects, what a project holds, and a result's record and its numbers as tables. They
// are remote actions (actions.go, marked program) that agents and scripts call as any other, read
// from the library's project documents and the artifacts' folders, so an agent can look before
// anything is opened and while the window shows something else. Every answer names the artifacts
// it read by their ids.

// programAction performs an action the program answers itself; ok is false for one the page
// performs.
func (a *app) programAction(ctx context.Context, name string, raw json.RawMessage) (remoteResult, bool) {
	var args map[string]any
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return remoteResult{Message: "The arguments are not a JSON object."}, true
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	var result remoteResult
	var err error
	switch name {
	case "list_projects":
		result, err = a.listProjectsAction()
	case "describe_project":
		result, err = a.describeProjectAction(args)
	case "get_result":
		result, err = a.getResultAction(args)
	default:
		return remoteResult{}, false
	}
	if err != nil {
		return remoteResult{OK: false, Message: err.Error()}, true
	}
	result.OK = true
	return result, true
}

func answer(message string, data any) (remoteResult, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return remoteResult{}, err
	}
	return remoteResult{Message: message, Data: raw}, nil
}

// --- The library's projects --------------------------------------------------------------------

// libraryProject is what these answers read of a project document (docs/FORMATS.md).
type libraryProject struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Created  string `json:"created"`
	Modified string `json:"modified"`
	Datasets []struct {
		ID          string          `json:"id"`
		Name        string          `json:"name"`
		Path        string          `json:"path"`
		Run         string          `json:"run"`
		Panel       json.RawMessage `json:"panel"`
		Tissue      string          `json:"tissue"`
		Cells       json.RawMessage `json:"cells"`
		Transcripts json.RawMessage `json:"transcripts"`
	} `json:"datasets"`
	Artifacts []json.RawMessage `json:"artifacts"`
	Views     []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Dataset string `json:"dataset"`
	} `json:"views"`
	Regions []struct {
		ID       string `json:"id"`
		Dataset  string `json:"dataset"`
		Versions []struct {
			Name    string `json:"name"`
			Cls     string `json:"cls"`
			Removed bool   `json:"removed"`
		} `json:"versions"`
	} `json:"regions"`
	Figures []struct {
		ID     string            `json:"id"`
		Name   string            `json:"name"`
		Panels []json.RawMessage `json:"panels"`
	} `json:"figures"`
	Decisions []struct {
		Kind      string `json:"kind"`
		Dataset   string `json:"dataset"`
		Artifact  string `json:"artifact"`
		Rationale string `json:"rationale"`
	} `json:"decisions"`
	Samples json.RawMessage `json:"samples"`
}

// projectRecord is a result or record a project keeps (its artifacts list).
type projectRecord struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Dataset string `json:"dataset"`
	Created string `json:"created"`
	State   string `json:"state"`
	Version int    `json:"version"`
	Method  struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"method"`
	Parents []struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	} `json:"parents"`
}

// libraryProjects reads the library's projects, the most recently changed first.
func (a *app) libraryProjects() ([]libraryProject, error) {
	dir := filepath.Join(a.store.dir, "projects")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var out []libraryProject
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") || !projectIDPattern.MatchString(strings.TrimSuffix(name, ".json")) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var p libraryProject
		if json.Unmarshal(raw, &p) != nil {
			continue
		}
		if p.ID == "" {
			p.ID = strings.TrimSuffix(name, ".json")
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Modified > out[j].Modified })
	return out, nil
}

func plural(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	if strings.HasSuffix(one, "is") {
		return fmt.Sprintf("%d %ses", n, one[:len(one)-2])
	}
	if strings.HasSuffix(one, "y") {
		return fmt.Sprintf("%d %sies", n, one[:len(one)-1])
	}
	return fmt.Sprintf("%d %ss", n, one)
}

func (a *app) listProjectsAction() (remoteResult, error) {
	projects, err := a.libraryProjects()
	if err != nil {
		return remoteResult{}, err
	}
	type item struct {
		ID       string   `json:"id"`
		Name     string   `json:"name"`
		Created  string   `json:"created"`
		Modified string   `json:"modified"`
		Datasets []string `json:"datasets"`
		Results  int      `json:"results"`
		Figures  int      `json:"figures"`
		Views    int      `json:"views"`
	}
	list := []item{}
	var names []string
	for _, p := range projects {
		it := item{ID: p.ID, Name: p.Name, Created: p.Created, Modified: p.Modified, Datasets: []string{}, Figures: len(p.Figures), Views: len(p.Views)}
		for _, d := range p.Datasets {
			it.Datasets = append(it.Datasets, d.Name)
		}
		it.Results = len(p.Artifacts)
		list = append(list, it)
		names = append(names, p.Name)
	}
	message := "The library holds no project."
	if len(list) > 0 {
		message = fmt.Sprintf("%s in the library, the most recently changed first: %s.", plural(len(list), "project"), strings.Join(names, ", "))
	}
	return answer(message, map[string]any{"projects": list})
}

// findProject finds a project by its id, its name, or a part of its name naming one alone; the
// most recently changed when none is named.
func findProject(projects []libraryProject, ref string) (*libraryProject, error) {
	if len(projects) == 0 {
		return nil, errors.New("The library holds no project.")
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return &projects[0], nil
	}
	for i := range projects {
		if projects[i].ID == ref {
			return &projects[i], nil
		}
	}
	var exact, partial []int
	for i := range projects {
		name := strings.ToLower(projects[i].Name)
		if name == strings.ToLower(ref) {
			exact = append(exact, i)
		} else if strings.Contains(name, strings.ToLower(ref)) {
			partial = append(partial, i)
		}
	}
	names := func(list []int) string {
		var out []string
		for _, i := range list {
			out = append(out, fmt.Sprintf("%s (%s)", projects[i].Name, projects[i].ID))
		}
		return strings.Join(out, ", ")
	}
	switch {
	case len(exact) >= 1:
		// Opening an example names its project after it: the most recently changed of those named so.
		return &projects[exact[0]], nil
	case len(partial) == 1:
		return &projects[partial[0]], nil
	case len(partial) > 1:
		return nil, fmt.Errorf("Several projects' names hold %q: %s.", ref, names(partial))
	}
	var all []string
	for _, p := range projects {
		all = append(all, p.Name)
	}
	return nil, fmt.Errorf("No project %q in the library; it holds %s.", ref, strings.Join(all, ", "))
}

// pageRecordKinds are what the page decides and records itself: they have no state, being no
// job's (a result the program made has one, complete or invalid).
var pageRecordKinds = map[string]bool{"filter": true, "design": true, "marker-set": true, "cluster-names": true, "curated-labels": true, "consensus-labels": true}

func (r projectRecord) isResult() bool {
	return !pageRecordKinds[r.Kind] && r.State != ""
}

func (a *app) describeProjectAction(args map[string]any) (remoteResult, error) {
	projects, err := a.libraryProjects()
	if err != nil {
		return remoteResult{}, err
	}
	ref, _ := args["project"].(string)
	p, err := findProject(projects, ref)
	if err != nil {
		return remoteResult{}, err
	}
	sameName := 0
	for _, other := range projects {
		if other.Name == p.Name {
			sameName++
		}
	}
	datasetName := map[string]string{}
	type datasetItem struct {
		ID          string          `json:"id"`
		Name        string          `json:"name"`
		Path        string          `json:"path"`
		Run         string          `json:"run,omitempty"`
		Panel       json.RawMessage `json:"panel,omitempty"`
		Tissue      string          `json:"tissue,omitempty"`
		Cells       json.RawMessage `json:"cells,omitempty"`
		Transcripts json.RawMessage `json:"transcripts,omitempty"`
	}
	datasets := []datasetItem{}
	for _, d := range p.Datasets {
		datasetName[d.ID] = d.Name
		datasets = append(datasets, datasetItem{d.ID, d.Name, d.Path, d.Run, d.Panel, d.Tissue, d.Cells, d.Transcripts})
	}
	type resultItem struct {
		ID      string   `json:"id"`
		Kind    string   `json:"kind"`
		Name    string   `json:"name"`
		Dataset string   `json:"dataset,omitempty"`
		Created string   `json:"created,omitempty"`
		State   string   `json:"state,omitempty"`
		Method  string   `json:"method,omitempty"`
		Parents []string `json:"parents"`
	}
	type recordItem struct {
		ID      string `json:"id"`
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Version int    `json:"version,omitempty"`
	}
	results, records := []resultItem{}, []recordItem{}
	kinds := map[string]int{}
	var kindOrder []string
	for _, raw := range p.Artifacts {
		var r projectRecord
		if json.Unmarshal(raw, &r) != nil || r.ID == "" {
			continue
		}
		if !r.isResult() {
			records = append(records, recordItem{r.ID, r.Kind, r.Name, r.Version})
			continue
		}
		method := strings.TrimSpace(r.Method.Name + " " + r.Method.Version)
		item := resultItem{ID: r.ID, Kind: r.Kind, Name: r.Name, Dataset: datasetName[r.Dataset], Created: r.Created, State: r.State, Method: method, Parents: []string{}}
		if item.Dataset == "" {
			item.Dataset = r.Dataset
		}
		for _, parent := range r.Parents {
			if parent.ID != "" {
				item.Parents = append(item.Parents, parent.ID)
			}
		}
		results = append(results, item)
		if kinds[r.Kind] == 0 {
			kindOrder = append(kindOrder, r.Kind)
		}
		kinds[r.Kind]++
	}
	views := []map[string]string{}
	for _, v := range p.Views {
		views = append(views, map[string]string{"id": v.ID, "name": v.Name, "dataset": datasetName[v.Dataset]})
	}
	regions := []map[string]any{}
	for _, r := range p.Regions {
		if len(r.Versions) == 0 || r.Versions[len(r.Versions)-1].Removed {
			continue
		}
		last := r.Versions[len(r.Versions)-1]
		regions = append(regions, map[string]any{"id": r.ID, "dataset": datasetName[r.Dataset], "name": last.Name, "class": last.Cls, "version": len(r.Versions)})
	}
	figures := []map[string]any{}
	for _, f := range p.Figures {
		figures = append(figures, map[string]any{"id": f.ID, "name": f.Name, "panels": len(f.Panels)})
	}
	decisions := []map[string]string{}
	for _, d := range p.Decisions {
		decisions = append(decisions, map[string]string{"kind": d.Kind, "dataset": datasetName[d.Dataset], "artifact": d.Artifact, "rationale": d.Rationale})
	}
	var counts []string
	for _, k := range kindOrder {
		counts = append(counts, plural(kinds[k], kindWords(k)))
	}
	var dsNames []string
	for _, d := range p.Datasets {
		dsNames = append(dsNames, d.Name)
	}
	message := fmt.Sprintf("%s: %s", p.Name, plural(len(p.Datasets), "dataset"))
	if len(dsNames) > 0 {
		message += " (" + strings.Join(dsNames, ", ") + ")"
	}
	message += "; " + plural(len(results), "result")
	if len(counts) > 0 {
		message += " (" + strings.Join(counts, ", ") + ")"
	}
	message += fmt.Sprintf("; %s, %s, %s, %s. get_result gives a result's record and numbers by its id.", plural(len(records), "record"), plural(len(regions), "region"), plural(len(views), "saved view"), plural(len(figures), "figure"))
	if sameName > 1 {
		message += fmt.Sprintf(" %d projects are named %s: this is the most recently changed (%s); name another by its id (list_projects lists them).", sameName, p.Name, p.ID)
	}
	data := struct {
		Project   map[string]string   `json:"project"`
		Datasets  []datasetItem       `json:"datasets"`
		Results   []resultItem        `json:"results"`
		Records   []recordItem        `json:"records"`
		Regions   []map[string]any    `json:"regions"`
		Views     []map[string]string `json:"views"`
		Figures   []map[string]any    `json:"figures"`
		Decisions []map[string]string `json:"decisions"`
		Samples   int                 `json:"samples,omitempty"`
	}{map[string]string{"id": p.ID, "name": p.Name, "created": p.Created, "modified": p.Modified}, datasets, results, records, regions, views, figures, decisions, countJSONList(p.Samples)}
	return answer(message, data)
}

func countJSONList(raw json.RawMessage) int {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return 0
	}
	return len(list)
}

// kindWords names a kind of result in a sentence.
func kindWords(kind string) string {
	switch kind {
	case "spatial":
		return "spatial analysis"
	case "spatial-comparison":
		return "spatial comparison"
	case "cohort-summary":
		return "cohort summary"
	case "labels":
		return "label set"
	case "abundance":
		return "abundance comparison"
	case "expression":
		return "expression comparison"
	}
	return kind
}

// --- A result ----------------------------------------------------------------------------------

// resultTable is a table of a result's numbers: its columns, and rows of numbers (null where there
// is none) and words.
type resultTable struct {
	Name    string   `json:"name"`
	Caption string   `json:"caption"`
	Columns []string `json:"columns"`
	rows    [][]any
}

const (
	resultRowsDefault = 100
	resultRowsMost    = 10000
)

func (a *app) getResultAction(args map[string]any) (remoteResult, error) {
	id, _ := args["id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return remoteResult{}, errors.New("id is required: a result's id, as describe_project lists them.")
	}
	offset, limit := 0, resultRowsDefault
	if v, ok := args["offset"].(float64); ok {
		offset = max(0, int(v))
	}
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
		if limit <= 0 || limit > resultRowsMost {
			limit = resultRowsMost
		}
	}
	projects, _ := a.libraryProjects()
	var holders []map[string]string
	var pageRecord json.RawMessage
	for _, p := range projects {
		for _, raw := range p.Artifacts {
			var r projectRecord
			if json.Unmarshal(raw, &r) == nil && r.ID == id {
				holders = append(holders, map[string]string{"id": p.ID, "name": p.Name})
				if !r.isResult() && pageRecord == nil {
					pageRecord = raw
				}
			}
		}
	}
	data := map[string]any{"id": id, "projects": holders}
	var tables []resultTable
	var name, kind string
	if m, err := a.artifacts.Get(id); err == nil {
		name, kind = m.Name, m.Kind
		parents := []map[string]string{}
		for _, parent := range m.Lineage.Parents {
			entry := map[string]string{"kind": parent.Kind, "id": parent.ID, "name": parent.Name}
			if pm, err := a.artifacts.Get(parent.ID); err == nil {
				entry["kind"], entry["name"] = pm.Kind, pm.Name
			}
			parents = append(parents, entry)
		}
		data["kind"], data["name"], data["dataset"], data["state"], data["created"] = m.Kind, m.Name, m.Dataset, m.State, m.Created
		data["method"] = m.Lineage.Method
		if len(m.Lineage.Parameters) > 0 {
			data["parameters"] = m.Lineage.Parameters
		}
		data["parents"], data["software"], data["findings"] = parents, m.Lineage.Software, m.Findings
		if m.Lineage.Job != nil {
			data["job"] = map[string]any{"id": m.Lineage.Job.ID, "kind": m.Lineage.Job.Kind}
		}
		if len(m.Summary) > 0 {
			data["summary"] = m.Summary
		}
		tables = append(kindTables(filepath.Join(a.artifacts.Dir(), id), m), summaryTables(m.Summary)...)
	} else if pageRecord != nil {
		var r projectRecord
		json.Unmarshal(pageRecord, &r)
		name, kind = r.Name, r.Kind
		data["kind"], data["name"], data["record"] = r.Kind, r.Name, pageRecord
		var fields map[string]json.RawMessage
		if json.Unmarshal(pageRecord, &fields) == nil && len(fields["result"]) > 0 {
			tables = summaryTables(fields["result"])
		}
	} else {
		return remoteResult{}, fmt.Errorf("No result %s in the library: describe_project lists a project's results by id.", id)
	}
	list := []map[string]any{}
	for _, t := range tables {
		list = append(list, map[string]any{"name": t.Name, "caption": t.Caption, "columns": t.Columns, "rows": len(t.rows)})
	}
	data["tables"] = list
	message := fmt.Sprintf("%s (%s, %s)", name, kindWords(kind), id)
	if len(tables) > 0 {
		want, _ := args["table"].(string)
		chosen := -1
		if want == "" {
			chosen = 0
		}
		for i, t := range tables {
			if t.Name == want {
				chosen = i
			}
		}
		if chosen < 0 {
			var names []string
			for _, t := range tables {
				names = append(names, t.Name)
			}
			return remoteResult{}, fmt.Errorf("%s has no table %q; its tables are %s.", id, want, strings.Join(names, ", "))
		}
		t := tables[chosen]
		end := min(len(t.rows), offset+limit)
		rows := [][]any{}
		if offset < len(t.rows) {
			rows = t.rows[offset:end]
		}
		data["table"] = map[string]any{"name": t.Name, "caption": t.Caption, "columns": t.Columns, "total": len(t.rows), "offset": offset, "rows": rows}
		message += fmt.Sprintf(": %s; %s, rows %d to %d of %d", plural(len(tables), "table"), t.Caption, min(offset+1, len(t.rows)), max(offset, end), len(t.rows))
		if end < len(t.rows) {
			message += fmt.Sprintf(" (offset %d for the next)", end)
		}
	}
	return answer(message+".", data)
}

// number is a value of a table: null for NaN and infinities.
func number(v float64) any {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return v
}

func at(list []float64, i int) any {
	if i < 0 || i >= len(list) {
		return nil
	}
	return number(list[i])
}

func atInt(list []int, i int) any {
	if i < 0 || i >= len(list) {
		return nil
	}
	return list[i]
}

// kindTables are a result's full numbers by its kind: a spatial analysis's by its question, a
// clustering's clusters, a label set's labels, and any table it keeps as CSV.
func kindTables(dir string, m artifacts.Manifest) []resultTable {
	var tables []resultTable
	switch m.Kind {
	case "spatial":
		if a, err := spatial.ReadAnalysis(dir); err == nil {
			tables = append(tables, spatialTables(a)...)
		}
	case "clustering":
		if d, err := cluster.ReadDescription(dir); err == nil {
			t := resultTable{Name: "clusters", Caption: "Clusters", Columns: []string{"cluster", "cells", "share", "markers"}}
			total := 0
			for _, n := range d.Clusters.Sizes {
				total += n
			}
			for c, n := range d.Clusters.Sizes {
				var genes []string
				if c < len(d.Top) {
					for _, mk := range d.Top[c] {
						genes = append(genes, mk.Gene)
					}
				}
				share := any(nil)
				if total > 0 {
					share = float64(n) / float64(total)
				}
				t.rows = append(t.rows, []any{c + 1, n, share, strings.Join(genes, ", ")})
			}
			tables = append(tables, t)
		}
	case "labels":
		if d, err := label.ReadDescription(dir); err == nil {
			t := resultTable{Name: "labels", Caption: "Labels", Columns: []string{"level", "label", "cells", "share"}}
			for _, s := range d.Sets {
				levelName := ""
				if s.Level >= 0 && s.Level < len(d.Levels) {
					levelName = d.Levels[s.Level]
				}
				// Its share of the cells labeled at its level.
				share := any(nil)
				if s.Level >= 0 && s.Level < len(d.Cells.Labeled) && d.Cells.Labeled[s.Level] > 0 {
					share = float64(s.Cells) / float64(d.Cells.Labeled[s.Level])
				}
				t.rows = append(t.rows, []any{levelName, s.Name, s.Cells, share})
			}
			tables = append(tables, t)
		}
	}
	// Its CSV files, a cohort summary's pseudobulk (a column a gene) after its table.
	var files []string
	for _, f := range m.Files {
		if f.Location == artifacts.InArtifact && strings.HasSuffix(f.Path, ".csv") {
			files = append(files, f.Path)
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[j] == "pseudobulk.csv" && files[i] != "pseudobulk.csv" })
	for _, file := range files {
		if t, err := csvTable(filepath.Join(dir, filepath.FromSlash(file))); err == nil {
			tables = append(tables, t)
		}
	}
	return tables
}

// spatialTables are an analysis's numbers by its question (spatial.Analysis's arrays, as their
// comments say they are laid out).
func spatialTables(a *spatial.Analysis) []resultTable {
	names := a.Labels.Names
	K := len(names)
	name := func(k int) string {
		if k >= 0 && k < K {
			return names[k]
		}
		return fmt.Sprintf("label %d", k+1)
	}
	var out []resultTable
	switch {
	case a.Enrichment != nil:
		e := a.Enrichment
		t := resultTable{Name: "pairs", Caption: "Neighborhood enrichment, by pair", Columns: []string{"a", "b", "count", "expected", "mean", "sd", "z", "p", "padj"}}
		for i := 0; i < K; i++ {
			for j := 0; j < K; j++ {
				q := i*K + j
				t.rows = append(t.rows, []any{name(i), name(j), at(e.Count, q), at(e.Expected, q), at(e.Mean, q), at(e.SD, q), at(e.Z, q), at(e.P, q), at(e.PAdj, q)})
			}
		}
		out = append(out, t)
	case a.Cooccurrence != nil:
		c := a.Cooccurrence
		t := resultTable{Name: "ratios", Caption: "Co-occurrence, by distance and pair", Columns: []string{"distance", "a", "b", "count", "ratio"}}
		for d, distance := range c.Distances {
			for i := 0; i < K; i++ {
				for j := 0; j < K; j++ {
					q := i*K + j
					var count, ratio any
					if d < len(c.Count) {
						count = at(c.Count[d], q)
					}
					if d < len(c.Ratio) {
						ratio = at(c.Ratio[d], q)
					}
					t.rows = append(t.rows, []any{distance, name(i), name(j), count, ratio})
				}
			}
		}
		out = append(out, t)
	case a.Nearest != nil:
		n := a.Nearest
		t := resultTable{Name: "pairs", Caption: "Nearest distances, by pair (µm)", Columns: []string{"a", "b", "beyond", "median", "mean", "permMean", "permSd", "z", "p", "padj"}}
		for i := 0; i < K; i++ {
			for j := 0; j < K; j++ {
				q := i*K + j
				t.rows = append(t.rows, []any{name(i), name(j), atInt(n.Beyond, q), at(n.Median, q), at(n.Mean, q), at(n.PermMean, q), at(n.PermSD, q), at(n.Z, q), at(n.P, q), at(n.PAdj, q)})
			}
		}
		out = append(out, t)
	case a.Ripley != nil:
		r := a.Ripley
		tests := resultTable{Name: "pairs", Caption: "Ripley's K, by pair", Columns: []string{"a", "b", "p", "padj"}}
		curves := resultTable{Name: "curves", Caption: "Ripley's K and L, by pair and radius (µm)", Columns: []string{"a", "b", "radius", "k", "l", "mean", "lo", "hi"}}
		R := len(r.Radii)
		for i := 0; i < K; i++ {
			for j := 0; j < K; j++ {
				q := i*K + j
				tests.rows = append(tests.rows, []any{name(i), name(j), at(r.P, q), at(r.PAdj, q)})
				for k, radius := range r.Radii {
					c := q*R + k
					curves.rows = append(curves.rows, []any{name(i), name(j), radius, at(r.K, c), at(r.L, c), at(r.Mean, c), at(r.Lo, c), at(r.Hi, c)})
				}
			}
		}
		out = append(out, tests, curves)
	case a.Regions != nil:
		g := a.Regions
		rowName := func(r int) string {
			if r < len(g.Regions) {
				return g.Regions[r].Name
			}
			return "the tissue in none"
		}
		R := len(g.Area)
		rows := resultTable{Name: "rows", Caption: "Regions compared, by row and label", Columns: []string{"row", "label", "area", "count", "density", "densityLo", "densityHi", "share", "shareLo", "shareHi"}}
		for r := 0; r < R; r++ {
			for k := 0; k < K; k++ {
				q := r*K + k
				rows.rows = append(rows.rows, []any{rowName(r), name(k), at(g.Area, r), atInt(g.Count, q), at(g.Density, q), at(g.DensityLo, q), at(g.DensityHi, q), at(g.Share, q), at(g.ShareLo, q), at(g.ShareHi, q)})
			}
		}
		ratios := resultTable{Name: "ratios", Caption: "Regions compared, one row over another, by label", Columns: []string{"row", "over", "label", "densityRatio", "densityRatioLo", "densityRatioHi", "shareRatio", "shareRatioLo", "shareRatioHi"}}
		for r := 0; r < R; r++ {
			for s := 0; s < R; s++ {
				if r == s {
					continue
				}
				for k := 0; k < K; k++ {
					q := (r*R+s)*K + k
					ratios.rows = append(ratios.rows, []any{rowName(r), rowName(s), name(k), at(g.DensityRatio, q), at(g.DensityRatioLo, q), at(g.DensityRatioHi, q), at(g.ShareRatio, q), at(g.ShareRatioLo, q), at(g.ShareRatioHi, q)})
				}
			}
		}
		out = append(out, rows, ratios)
	case a.Boundary != nil:
		b := a.Boundary
		bandName := func(i int) string {
			switch {
			case len(b.Edges) == 0:
				return "all"
			case i == 0:
				return fmt.Sprintf("below %g", b.Edges[0])
			case i >= len(b.Edges):
				return fmt.Sprintf("%g and beyond", b.Edges[len(b.Edges)-1])
			}
			return fmt.Sprintf("%g to %g", b.Edges[i-1], b.Edges[i])
		}
		bands := resultTable{Name: "bands", Caption: "Across the edge, by band (µm from the edge, inside negative) and label", Columns: []string{"band", "label", "area", "count", "density", "densityLo", "densityHi", "share", "shareLo", "shareHi"}}
		for i := 0; i <= len(b.Edges); i++ {
			for k := 0; k < K; k++ {
				q := i*K + k
				bands.rows = append(bands.rows, []any{bandName(i), name(k), at(b.Area, i), atInt(b.Count, q), at(b.Density, q), at(b.DensityLo, q), at(b.DensityHi, q), at(b.Share, q), at(b.ShareLo, q), at(b.ShareHi, q)})
			}
		}
		zoneNames := []string{"core", "margin", "around"}
		zones := resultTable{Name: "zones", Caption: "Across the edge, by zone and label", Columns: []string{"zone", "label", "area", "count", "density", "lo", "hi"}}
		for z, zn := range zoneNames {
			for k := 0; k < K; k++ {
				q := z*K + k
				zones.rows = append(zones.rows, []any{zn, name(k), at(b.ZoneArea, z), atInt(b.ZoneCount, q), at(b.ZoneDensity, q), at(b.ZoneLo, q), at(b.ZoneHi, q)})
			}
		}
		comparisons := []string{"core over around", "margin over core", "margin over around"}
		ratios := resultTable{Name: "ratios", Caption: "The zones compared, by label", Columns: []string{"label", "comparison", "ratio", "lo", "hi", "inside", "atMargin"}}
		for k := 0; k < K; k++ {
			for c, cn := range comparisons {
				q := k*len(comparisons) + c
				var atMargin any
				if k < len(b.AtMargin) {
					atMargin = b.AtMargin[k]
				}
				ratios.rows = append(ratios.rows, []any{name(k), cn, at(b.Ratio, q), at(b.RatioLo, q), at(b.RatioHi, q), atInt(b.Inside, k), atMargin})
			}
		}
		out = append(out, bands, zones, ratios)
	case a.Niches != nil:
		n := a.Niches
		niches := resultTable{Name: "niches", Caption: "Niches", Columns: []string{"niche", "cells", "defining", "genes"}}
		composition := resultTable{Name: "composition", Caption: "Niches' composition, by niche and label", Columns: []string{"niche", "label", "share", "count"}}
		for i := 0; i < n.Niches; i++ {
			var defining, genes []string
			if i < len(n.Defining) {
				for _, k := range n.Defining[i] {
					defining = append(defining, name(k))
				}
			}
			if i < len(n.Markers) {
				for j, m := range n.Markers[i] {
					if j >= 10 {
						break
					}
					genes = append(genes, m.Gene)
				}
			}
			niches.rows = append(niches.rows, []any{i + 1, atInt(n.Cells, i), strings.Join(defining, ", "), strings.Join(genes, ", ")})
			for k := 0; k < K; k++ {
				q := i*K + k
				composition.rows = append(composition.rows, []any{i + 1, name(k), at(n.Composition, q), atInt(n.Counts, q)})
			}
		}
		out = append(out, niches, composition)
	case a.Moran != nil:
		m := a.Moran
		t := resultTable{Name: "genes", Caption: "Moran's I, by gene", Columns: []string{"gene", "i", "z", "p", "padj", "permMean", "permSd", "permP", "mean", "detected"}}
		for g, gene := range m.Genes {
			t.rows = append(t.rows, []any{gene, at(m.I, g), at(m.Z, g), at(m.P, g), at(m.PAdj, g), at(m.PermMean, g), at(m.PermSD, g), at(m.PermP, g), at(m.Mean, g), atInt(m.Detected, g)})
		}
		out = append(out, t)
	}
	return out
}

// csvTable reads a table a result keeps as CSV: its first row the columns, numbers as numbers and
// NA or nothing as null.
func csvTable(path string) (resultTable, error) {
	file, err := os.Open(path)
	if err != nil {
		return resultTable{}, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return resultTable{}, err
	}
	base := strings.TrimSuffix(filepath.Base(path), ".csv")
	t := resultTable{Name: base, Caption: base + ".csv", Columns: header}
	for len(t.rows) < resultRowsMost*10 {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return resultTable{}, err
		}
		row := make([]any, len(header))
		for i := range row {
			if i >= len(record) {
				continue
			}
			v := record[i]
			switch {
			case v == "" || v == "NA" || v == "NaN":
				row[i] = nil
			default:
				if f, err := strconv.ParseFloat(v, 64); err == nil && !math.IsInf(f, 0) {
					row[i] = json.Number(v)
				} else {
					row[i] = v
				}
			}
		}
		t.rows = append(t.rows, row)
	}
	return t, nil
}

// --- A summary's tables, as the report shows them ----------------------------------------------

// summaryHidden are the keys of a summary not shown: where it came from (its record says it) and
// colors (web/lib/report.js's HIDDEN).
var summaryHidden = map[string]bool{"segmentation": true, "graph": true, "clustering": true, "summary": true, "colors": true, "id": true, "question": true, "parameters": true, "choice": true, "checksum": true, "markers": true, "methods": true}

// orderedValue is a JSON value read with its objects' keys in order.
type orderedValue struct {
	keys   []string
	fields map[string]*orderedValue
	list   []*orderedValue
	scalar any // json.Number, string, bool or nil
	kind   byte
}

func readOrdered(raw []byte) (*orderedValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return readOrderedValue(decoder)
}

func readOrderedValue(d *json.Decoder) (*orderedValue, error) {
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t := token.(type) {
	case json.Delim:
		if t == '{' {
			v := &orderedValue{kind: 'o', fields: map[string]*orderedValue{}}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				child, err := readOrderedValue(d)
				if err != nil {
					return nil, err
				}
				k := key.(string)
				if _, seen := v.fields[k]; !seen {
					v.keys = append(v.keys, k)
				}
				v.fields[k] = child
			}
			_, err := d.Token()
			return v, err
		}
		v := &orderedValue{kind: 'a'}
		for d.More() {
			child, err := readOrderedValue(d)
			if err != nil {
				return nil, err
			}
			v.list = append(v.list, child)
		}
		_, err := d.Token()
		return v, err
	case json.Number:
		return &orderedValue{kind: 'n', scalar: t}, nil
	case string:
		return &orderedValue{kind: 's', scalar: t}, nil
	case bool:
		return &orderedValue{kind: 'b', scalar: t}, nil
	}
	return &orderedValue{kind: '0'}, nil
}

// summaryTables are a summary's numbers as the report tabulates them: its numbers and words at the
// top ("summary"), a table for each list of rows and a list of numbers as a row ("summary.<key>").
func summaryTables(raw json.RawMessage) []resultTable {
	if len(raw) == 0 {
		return nil
	}
	root, err := readOrdered(raw)
	if err != nil || root.kind != 'o' {
		return nil
	}
	var tables []resultTable
	top := resultTable{Name: "summary", Caption: "Its numbers", Columns: []string{"key", "value"}}
	var walk func(v *orderedValue, prefix string)
	walk = func(v *orderedValue, prefix string) {
		for _, k := range v.keys {
			child := v.fields[k]
			if summaryHidden[k] || child.kind == '0' || (child.kind == 's' && child.scalar == "") {
				continue
			}
			if child.kind == 'o' {
				if id, ok := child.fields["id"]; ok && id.kind == 's' && id.scalar == "" {
					continue
				}
			}
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			switch child.kind {
			case 'a':
				if len(child.list) == 0 {
					continue
				}
				switch {
				case allKind(child.list, 'o'):
					var columns []string
					seen := map[string]bool{}
					for _, row := range child.list {
						for _, c := range row.keys {
							if !summaryHidden[c] && !seen[c] && row.fields[c].kind != 'o' && row.fields[c].kind != 'a' {
								seen[c] = true
								columns = append(columns, c)
							}
						}
					}
					t := resultTable{Name: "summary." + key, Caption: key, Columns: columns}
					for _, row := range child.list {
						values := make([]any, len(columns))
						for i, c := range columns {
							if f, ok := row.fields[c]; ok {
								values[i] = f.scalar
							}
						}
						t.rows = append(t.rows, values)
					}
					tables = append(tables, t)
				case allKind(child.list, 'n'):
					t := resultTable{Name: "summary." + key, Caption: key}
					values := make([]any, len(child.list))
					for i, n := range child.list {
						t.Columns = append(t.Columns, strconv.Itoa(i+1))
						values[i] = n.scalar
					}
					t.rows = [][]any{values}
					tables = append(tables, t)
				case noneKind(child.list, 'o', 'a'):
					var words []string
					for _, s := range child.list {
						words = append(words, fmt.Sprint(s.scalar))
					}
					top.rows = append(top.rows, []any{key, strings.Join(words, ", ")})
				}
			case 'o':
				walk(child, key)
			default:
				top.rows = append(top.rows, []any{key, child.scalar})
			}
		}
	}
	walk(root, "")
	if len(top.rows) > 0 {
		tables = append([]resultTable{top}, tables...)
	}
	return tables
}

func allKind(list []*orderedValue, kind byte) bool {
	for _, v := range list {
		if v == nil || v.kind != kind {
			return false
		}
	}
	return true
}

func noneKind(list []*orderedValue, kinds ...byte) bool {
	for _, v := range list {
		for _, k := range kinds {
			if v.kind == k {
				return false
			}
		}
	}
	return true
}
