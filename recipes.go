package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/xenium"
)

// The recipe of a result (wave 9, slice 5; requirement E5): its steps from the bundles, each the
// job that made it and its spec as readable JSON, taken from the artifacts' lineage (each manifest
// records its job's kind and spec, and what it was made from). Nothing of this computer is kept:
// a bundle is named by its role (situcraft:bundle/<role>[/<path in it>]), another file it read by
// its name (situcraft:file/<name>), and an artifact an earlier step made by that step
// (situcraft:step/<n>). situcraft run makes it again on a bundle (run.go).
//
// A step whose spec carries cells the window chose (a filter's, each cell's label, the cells of
// regions, as base64 of a number for each cell) is made again only on the bundle the recipe was
// made from, where those cells are the same.

const recipeKindResult = "result"

// resultRecipe is a result's recipe.
type resultRecipe struct {
	Format   string `json:"format"`
	Version  int    `json:"version"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Software struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Commit  string `json:"commit,omitempty"`
	} `json:"software"`
	// Result is the step that makes the result (the last).
	Result  int            `json:"result"`
	Bundles []recipeBundle `json:"bundles"`
	Files   []recipeFile   `json:"files,omitempty"`
	Steps   []recipeStep   `json:"steps"`
	Run     string         `json:"run"`
}

// recipeBundle is a bundle the recipe was made from, by its role: its run and the files its steps
// read, by SHA-256, so that the same bundle is known again.
type recipeBundle struct {
	Role     string      `json:"role"`
	Name     string      `json:"name"`
	Analysis string      `json:"analysis,omitempty"`
	Files    []certNamed `json:"files"`
}

// recipeFile is another file a step read (a table of labels, a reference, a segmentation made
// elsewhere), by its name and SHA-256.
type recipeFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256,omitempty"`
}

// recipeStep is a step: the artifact it makes (its kind, its name and id where the recipe was
// made), the job that makes it, the steps it is made from, and the job's spec.
type recipeStep struct {
	N      int    `json:"n"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Made   string `json:"made"`
	Job    string `json:"job"`
	Method string `json:"method,omitempty"`
	Inputs []int  `json:"inputs,omitempty"`
	// Carries: the spec holds cells the window chose; made again only on the same bundle.
	Carries bool            `json:"carries,omitempty"`
	Spec    json.RawMessage `json:"spec"`
}

// jsonString is a JSON string literal, escapes and all.
var jsonStringLiteral = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

// rewriteStrings answers raw JSON with each string literal f changes replaced, every other byte as
// it was (a job writes some of its spec through as it was given).
func rewriteStrings(raw []byte, f func(string) (string, bool)) []byte {
	return jsonStringLiteral.ReplaceAllFunc(raw, func(m []byte) []byte {
		var text string
		if json.Unmarshal(m, &text) != nil {
			return m
		}
		out, ok := f(text)
		if !ok {
			return m
		}
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		e.Encode(out)
		return bytes.TrimRight(b.Bytes(), "\n")
	})
}

// carriesCells says whether a spec holds cells the window chose: a filter's or regions' cells, or
// a number for each cell (keep, codes).
func carriesCells(spec json.RawMessage) bool {
	var v any
	if json.Unmarshal(spec, &v) != nil {
		return false
	}
	var walk func(v any) bool
	walk = func(v any) bool {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				if (k == "keep" || k == "codes") && x != nil {
					return true
				}
				if walk(x) {
					return true
				}
			}
		case []any:
			for _, x := range v {
				if walk(x) {
					return true
				}
			}
		}
		return false
	}
	return walk(v)
}

// specBundles are the bundles a spec names (bundle, or a cohort's bundles).
func specBundles(spec json.RawMessage) []string {
	var s struct {
		Bundle  string   `json:"bundle"`
		Bundles []string `json:"bundles"`
	}
	json.Unmarshal(spec, &s)
	if s.Bundle != "" {
		return append([]string{s.Bundle}, s.Bundles...)
	}
	return s.Bundles
}

// roleOf is a bundle's role in a recipe: its folder's name, in letters, digits, '.', '_' and '-'.
func roleOf(path string, taken map[string]bool) string {
	role := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, filepath.Base(path))
	if strings.Trim(role, "-.") == "" {
		role = "bundle"
	}
	base := role
	for k := 2; taken[role]; k++ {
		role = base + "-" + strconv.Itoa(k)
	}
	taken[role] = true
	return role
}

// resultRecipe writes the recipe of an artifact: every artifact it is made from, those its
// lineage names and those its spec names (a label set whose labels it was given), inputs first.
func (a *app) resultRecipe(id string) (*resultRecipe, error) {
	result, err := a.artifacts.Get(id)
	if err != nil {
		return nil, fmt.Errorf("no result %s in the library", id)
	}
	var order []artifacts.Manifest
	seen := map[string]bool{}
	var visit func(m artifacts.Manifest) error
	visit = func(m artifacts.Manifest) error {
		if seen[m.ID] {
			return nil
		}
		seen[m.ID] = true
		if m.State != artifacts.Complete {
			return fmt.Errorf("%s (%s) is %s: a recipe is written of complete results", m.Name, m.ID, m.State)
		}
		job := m.Lineage.Job
		if job == nil || len(job.Spec) == 0 || string(job.Spec) == "null" || !remadeKinds[job.Kind] {
			return fmt.Errorf("%s (%s) was not made by one of SituCraft's jobs from a spec (an image, an alignment): a recipe holds only what they make", m.Name, m.ID)
		}
		var inputs []string
		for _, p := range m.Lineage.Parents {
			inputs = append(inputs, p.ID)
		}
		rewriteStrings(job.Spec, func(s string) (string, bool) {
			inputs = append(inputs, s)
			return "", false
		})
		for _, in := range inputs {
			if in == m.ID || seen[in] || !callIDPattern.MatchString(in) {
				continue
			}
			parent, err := a.artifacts.Get(in)
			if err != nil {
				continue // a bundle's run, or a record of the window's
			}
			if err := visit(parent); err != nil {
				return err
			}
		}
		order = append(order, m)
		return nil
	}
	if err := visit(result); err != nil {
		return nil, err
	}

	r := &resultRecipe{Format: recipeFormat, Version: recipeVersion, Kind: recipeKindResult, Name: result.Name}
	r.Software.Name, r.Software.Version, r.Software.Commit = "SituCraft", version, buildCommit()
	r.Result = len(order)
	r.Run = "situcraft run --recipe <this file> --bundle <the bundle's folder> --out <a new folder>"
	roles := map[string]string{} // bundle folder → role
	taken := map[string]bool{}
	byRole := map[string]*recipeBundle{}
	files := map[string]string{} // name → SHA-256
	stepOf := map[string]int{}
	for i, m := range order {
		n := i + 1
		stepOf[m.ID] = n
		for _, path := range specBundles(m.Lineage.Job.Spec) {
			if _, ok := roles[path]; ok {
				continue
			}
			role := roleOf(path, taken)
			roles[path] = role
			r.Bundles = append(r.Bundles, recipeBundle{Role: role, Name: filepath.Base(path)})
			byRole[role] = &r.Bundles[len(r.Bundles)-1]
		}
	}
	// A bundle known again by its experiment.xenium and the files its steps read.
	for path, role := range roles {
		b := byRole[role]
		listed := map[string]string{}
		if sum, err := fileSHA256(filepath.Join(path, "experiment.xenium")); err == nil {
			listed["experiment.xenium"] = sum
		}
		if bundle, err := xenium.Open(path, xenium.Options{CacheDir: a.cacheDir}); err == nil {
			if bundle.Experiment != nil {
				b.Analysis = bundle.Experiment.AnalysisUUID
			}
			bundle.Close()
		}
		for _, m := range order {
			if !slicesContain(specBundles(m.Lineage.Job.Spec), path) {
				continue
			}
			for _, f := range m.Files {
				if f.Location == artifacts.InBundle && f.SHA256 != "" {
					listed[filepath.ToSlash(f.Path)] = f.SHA256
				}
			}
		}
		for p, sum := range listed {
			b.Files = append(b.Files, certNamed{Path: p, SHA256: sum})
		}
		sort.Slice(b.Files, func(i, j int) bool { return b.Files[i].Path < b.Files[j].Path })
	}
	for i, m := range order {
		n := i + 1
		inputs := map[int]bool{}
		spec := rewriteStrings(m.Lineage.Job.Spec, func(s string) (string, bool) {
			for path, role := range roles {
				if s == path {
					return "situcraft:bundle/" + role, true
				}
				if rest, ok := strings.CutPrefix(s, path+string(filepath.Separator)); ok {
					return "situcraft:bundle/" + role + "/" + filepath.ToSlash(rest), true
				}
			}
			if k, ok := stepOf[s]; ok && k < n {
				inputs[k] = true
				return "situcraft:step/" + strconv.Itoa(k), true
			}
			if filepath.IsAbs(s) {
				name := filepath.Base(s)
				if _, ok := files[name]; !ok {
					files[name], _ = fileSHA256(s)
				}
				return "situcraft:file/" + name, true
			}
			return "", false
		})
		var compact bytes.Buffer
		if err := json.Compact(&compact, spec); err != nil {
			return nil, fmt.Errorf("%s's spec: %v", m.Name, err)
		}
		step := recipeStep{N: n, Kind: m.Kind, Name: m.Name, Made: m.ID, Job: m.Lineage.Job.Kind, Spec: compact.Bytes(), Carries: carriesCells(spec)}
		if m.Lineage.Method.Name != "" {
			step.Method = strings.TrimSpace(m.Lineage.Method.Name + " " + m.Lineage.Method.Version)
		}
		for k := range inputs {
			step.Inputs = append(step.Inputs, k)
		}
		sort.Ints(step.Inputs)
		r.Steps = append(r.Steps, step)
	}
	for name, sum := range files {
		r.Files = append(r.Files, recipeFile{Name: name, SHA256: sum})
	}
	sort.Slice(r.Files, func(i, j int) bool { return r.Files[i].Name < r.Files[j].Name })
	return r, nil
}

func slicesContain(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// recipeStandIn is a stand-in a recipe's spec holds.
var recipeStandIn = regexp.MustCompile(`^situcraft:(bundle|file|step)/(.*)$`)

// readResultRecipe reads a result's recipe and checks it: its format and version, its steps in
// order, each made by a job SituCraft makes again from its spec, its stand-ins naming its bundles,
// its files and earlier steps.
func readResultRecipe(raw []byte) (*resultRecipe, error) {
	var head struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
		Kind    string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("not a recipe: %v", err)
	}
	if head.Format != recipeFormat {
		return nil, fmt.Errorf("not a SituCraft recipe (its format is %q)", head.Format)
	}
	if head.Version > recipeVersion || head.Version < 1 {
		return nil, fmt.Errorf("a recipe of version %d, which this SituCraft (%s) does not read: a newer one made it", head.Version, version)
	}
	if head.Kind == "refinement" {
		return nil, errors.New("a refinement's recipe: situcraft refine --recipe makes it")
	}
	if head.Kind != recipeKindResult {
		return nil, fmt.Errorf("a recipe of a %q, which this SituCraft does not make", head.Kind)
	}
	var r resultRecipe
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("the recipe: %v", err)
	}
	if len(r.Steps) == 0 {
		return nil, errors.New("the recipe has no steps")
	}
	if r.Result < 1 || r.Result > len(r.Steps) {
		return nil, fmt.Errorf("the recipe's result is step %d, of %d", r.Result, len(r.Steps))
	}
	roles := map[string]bool{}
	for _, b := range r.Bundles {
		if b.Role == "" || roles[b.Role] {
			return nil, fmt.Errorf("the recipe names the bundle role %q twice, or none", b.Role)
		}
		roles[b.Role] = true
	}
	names := map[string]bool{}
	for _, f := range r.Files {
		names[f.Name] = true
	}
	for i, s := range r.Steps {
		if s.N != i+1 {
			return nil, fmt.Errorf("the recipe's steps are not numbered in order (step %d is %d)", i+1, s.N)
		}
		if !remadeKinds[s.Job] {
			return nil, fmt.Errorf("step %d, %s, is made by a %q job, which SituCraft does not make from a spec", s.N, s.Name, s.Job)
		}
		if !json.Valid(s.Spec) {
			return nil, fmt.Errorf("step %d's spec is not JSON", s.N)
		}
		var problem error
		rewriteStrings(s.Spec, func(text string) (string, bool) {
			m := recipeStandIn.FindStringSubmatch(text)
			if m == nil || problem != nil {
				return "", false
			}
			switch m[1] {
			case "bundle":
				role, _, _ := strings.Cut(m[2], "/")
				if !roles[role] {
					problem = fmt.Errorf("step %d names the bundle %q, which the recipe does not", s.N, role)
				}
			case "file":
				if !names[m[2]] {
					problem = fmt.Errorf("step %d names the file %q, which the recipe does not", s.N, m[2])
				}
			case "step":
				k, err := strconv.Atoi(m[2])
				if err != nil || k < 1 || k >= s.N {
					problem = fmt.Errorf("step %d is made from step %q, not one before it", s.N, m[2])
				}
			}
			return "", false
		})
		if problem != nil {
			return nil, problem
		}
	}
	return &r, nil
}

// localizeRecipe puts a step's stand-ins back: the bundles' folders, the files', and the ids the
// earlier steps made. It answers what it could not find.
func localizeRecipe(spec json.RawMessage, bundles map[string]string, files map[string]string, made map[int]string) ([]byte, []string) {
	var missing []string
	out := rewriteStrings(spec, func(text string) (string, bool) {
		m := recipeStandIn.FindStringSubmatch(text)
		if m == nil {
			return "", false
		}
		switch m[1] {
		case "bundle":
			role, sub, _ := strings.Cut(m[2], "/")
			root, ok := bundles[role]
			if !ok {
				missing = append(missing, "the bundle "+role)
				return "", false
			}
			if sub == "" {
				return root, true
			}
			return filepath.Join(root, filepath.FromSlash(sub)), true
		case "file":
			path, ok := files[m[2]]
			if !ok {
				missing = append(missing, m[2])
				return "", false
			}
			return path, true
		}
		k, _ := strconv.Atoi(m[2])
		id, ok := made[k]
		if !ok {
			missing = append(missing, "step "+m[2]+"'s result")
			return "", false
		}
		return id, true
	})
	return out, missing
}

// recipeText is a recipe as its file holds it: indented, each spec on one line or few.
func recipeText(r *resultRecipe) []byte {
	text, _ := json.MarshalIndent(r, "", " ")
	return append(text, '\n')
}

func (a *app) registerRecipes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/artifacts/{id}/recipe", a.serveRecipe)
}

// serveRecipe answers GET /api/artifacts/{id}/recipe: the result's recipe (a file to save with
// ?download=1).
func (a *app) serveRecipe(w http.ResponseWriter, r *http.Request) {
	if _, err := a.artifacts.Get(r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "No result "+r.PathValue("id")+" in the library.")
		return
	}
	recipe, err := a.resultRecipe(r.PathValue("id"))
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, "recipe.refused", err.Error())
		return
	}
	if r.URL.Query().Get("download") != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+r.PathValue("id")+`.recipe.json"`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(recipeText(recipe))
}

// recipeAction answers the recipe action (a program's): a result's recipe, its steps in words.
func (a *app) recipeAction(args map[string]any) (remoteResult, error) {
	id, _ := args["id"].(string)
	if strings.TrimSpace(id) == "" {
		return remoteResult{}, errors.New("id is required: a result's id (describe_project lists them)")
	}
	recipe, err := a.resultRecipe(strings.TrimSpace(id))
	if err != nil {
		return remoteResult{}, err
	}
	var steps []string
	carried := 0
	for _, s := range recipe.Steps {
		steps = append(steps, fmt.Sprintf("%d. %s (%s)", s.N, s.Name, s.Job))
		if s.Carries {
			carried++
		}
	}
	message := fmt.Sprintf("The recipe of %s: %s from %s. situcraft run makes it again without a window: %s.", recipe.Name, plural(len(recipe.Steps), "step"), plural(len(recipe.Bundles), "bundle"), strings.Join(steps, "; "))
	if carried > 0 {
		message += fmt.Sprintf(" %s carry cells the window chose (a filter's, labels', regions'): those are made again only on the bundle the recipe was made from.", plural(carried, "step"))
	}
	return answer(message, recipe)
}
