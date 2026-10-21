package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"situcraft/internal/artifacts"
)

// The certificate of a run (situcraft run): in the format the window writes (web/lib/certificate.js),
// of the result the recipe makes: its bundles by their files' SHA-256, and each step SituCraft's
// jobs made, its manifest (this computer's paths stood in for) and the files it wrote by SHA-256,
// so that situcraft verify makes each step again from the bundles and compares. A run has no
// project of the window's: its project.json names the run's datasets and results, its record.json
// its steps, and its methods.md each step's method, from the manifests.

// certEntry is a file of a certificate's zip.
type certEntry struct {
	name string
	data []byte
}

// runCertificate is where a run's certificate is, and its fingerprint.
type runCertificate struct {
	Path        string `json:"path"`
	Fingerprint string `json:"fingerprint"`
}

// The zip's entries' time: a certificate has no time of its own (as the window's).
var certificateZipTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Where a computer keeps its files (web/lib/certificate.js ROOTS): such a path within a text is
// stood in for, as the window's certificate does.
var withinText = regexp.MustCompile(`(^|[^A-Za-z0-9+/])((?:/(?:Users|home|private|tmp|var|Volumes|mnt|media|opt|srv|data|root|Applications)/|[A-Za-z]:\\|\\\\)[^\s)"'` + "`" + `,;]*)`)

// redactRun stands in for this computer's paths in JSON: a bundle's folder (or a file in it) by
// situcraft:bundle/<its dataset's id>[/<path in it>], any other path by situcraft:file/<its name>,
// a whole text or within one.
func redactRun(raw []byte, bundles map[string]string) []byte {
	type root struct{ folder, id string }
	var roots []root
	for folder, id := range bundles {
		roots = append(roots, root{strings.TrimRight(folder, `/\`), id})
	}
	sort.Slice(roots, func(i, j int) bool { return len(roots[i].folder) > len(roots[j].folder) })
	path := func(text string) string {
		for _, r := range roots {
			if text == r.folder {
				return "situcraft:bundle/" + r.id
			}
			if rest, ok := strings.CutPrefix(text, r.folder); ok && (rest[0] == '/' || rest[0] == '\\') {
				return "situcraft:bundle/" + r.id + strings.ReplaceAll(rest, `\`, "/")
			}
		}
		parts := strings.FieldsFunc(text, func(r rune) bool { return r == '/' || r == '\\' })
		name := ""
		if len(parts) > 0 {
			name = parts[len(parts)-1]
		}
		return "situcraft:file/" + name
	}
	return rewriteStrings(raw, func(text string) (string, bool) {
		if filepath.IsAbs(text) || strings.HasPrefix(text, "/") {
			return path(text), true
		}
		out := text
		for _, r := range roots {
			out = strings.ReplaceAll(out, r.folder, "situcraft:bundle/"+r.id)
		}
		out = withinText.ReplaceAllStringFunc(out, func(m string) string {
			sub := withinText.FindStringSubmatch(m)
			return sub[1] + path(sub[2])
		})
		return out, out != text
	})
}

// digestOfJSON is the SHA-256 of a value's canonical JSON (the window's digestOf).
func digestOfJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	v, err := decodeGeneric(raw)
	if err != nil {
		return ""
	}
	return sha256Hex(canonicalJSON(v))
}

// shortFingerprint is a fingerprint's short form: its first 16 characters in fours.
func shortFingerprint(fingerprint string) string {
	if len(fingerprint) < 16 {
		return fingerprint
	}
	f := fingerprint[:16]
	return f[:4] + "-" + f[4:8] + "-" + f[8:12] + "-" + f[12:]
}

// writeRunCertificate writes the certificate of a run's result to path.
func writeRunCertificate(a *app, recipe *resultRecipe, report *runReport, path string) (*runCertificate, error) {
	// Each bundle a dataset of the run, named by its role and experiment.xenium.
	datasets := map[string]string{} // role → dataset id
	folders := map[string]string{}  // folder → dataset id
	var roles []string
	for _, b := range report.Bundles {
		sum, err := fileSHA256(filepath.Join(b.folder, "experiment.xenium"))
		if err != nil {
			return nil, err
		}
		h := sha256.Sum256([]byte("situcraft run\n" + b.Role + "\n" + sum))
		datasets[b.Role] = "d-" + hex.EncodeToString(h[:8])
		folders[b.folder] = datasets[b.Role]
		roles = append(roles, b.Role)
	}
	var entries []certEntry
	add := func(name string, data []byte) certNamed {
		entries = append(entries, certEntry{name, data})
		return certNamed{Path: name, SHA256: sha256Hex(data)}
	}

	// The steps: each bundle's, then each the run made, numbered after them.
	type stepOut struct {
		N        int              `json:"n"`
		ID       string           `json:"id"`
		Kind     string           `json:"kind"`
		Name     string           `json:"name"`
		Inputs   []int            `json:"inputs"`
		Method   json.RawMessage  `json:"method"`
		By       string           `json:"by"`
		Bundle   string           `json:"bundle,omitempty"`
		Job      map[string]any   `json:"job,omitempty"`
		Manifest *certNamed       `json:"manifest,omitempty"`
		Files    []map[string]any `json:"files,omitempty"`
		Content  string           `json:"content,omitempty"`
		Summary  json.RawMessage  `json:"summary"`
	}
	var steps []stepOut
	type bundleOut struct {
		Dataset  string           `json:"dataset"`
		Name     string           `json:"name"`
		Run      string           `json:"run"`
		Analysis string           `json:"analysisUuid"`
		Files    []map[string]any `json:"files"`
	}
	var bundles []bundleOut
	bundleStep := map[string]int{}
	inBundle := map[string]map[string]string{} // dataset id → path → SHA-256
	for i, b := range report.Bundles {
		id := datasets[b.Role]
		bundleStep[b.Role] = i + 1
		steps = append(steps, stepOut{N: i + 1, ID: id, Kind: "dataset", Name: b.Role, Inputs: []int{}, Method: json.RawMessage("null"), By: "instrument", Bundle: id, Summary: json.RawMessage("null")})
		sum, _ := fileSHA256(filepath.Join(b.folder, "experiment.xenium"))
		inBundle[id] = map[string]string{"experiment.xenium": sum}
	}
	offset := len(report.Bundles)
	var methods []string
	type recordStep struct {
		N      int    `json:"n"`
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		Inputs []int  `json:"inputs"`
		Method string `json:"method,omitempty"`
	}
	var recorded []recordStep
	var made []map[string]any
	for i, s := range report.Steps {
		r := recipe.Steps[i]
		m, err := a.artifacts.Get(s.ID)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(filepath.Join(a.artifacts.Dir(), s.ID, "manifest.json"))
		if err != nil {
			return nil, err
		}
		red := redactRun(raw, folders)
		var indented bytes.Buffer
		if err := json.Indent(&indented, red, "", " "); err != nil {
			return nil, err
		}
		indented.WriteByte('\n')
		var shown artifacts.Manifest
		if err := json.Unmarshal(red, &shown); err != nil {
			return nil, err
		}
		inputs := []int{}
		for _, role := range stepRoles(r.Spec) {
			inputs = append(inputs, bundleStep[role])
		}
		for _, k := range r.Inputs {
			inputs = append(inputs, offset+k)
		}
		var files []map[string]any
		for _, f := range shown.Files {
			var size any
			if f.Size > 0 {
				size = f.Size
			}
			files = append(files, map[string]any{"path": f.Path, "role": f.Role, "size": size, "sha256": f.SHA256, "location": string(f.Location)})
			if f.Location == artifacts.InBundle {
				for _, role := range stepRoles(r.Spec) {
					inBundle[datasets[role]][filepath.ToSlash(f.Path)] = f.SHA256
				}
			}
		}
		method, _ := json.Marshal(map[string]string{"name": m.Lineage.Method.Name, "version": m.Lineage.Method.Version})
		summary := m.Summary
		if len(summary) == 0 {
			summary = json.RawMessage("null")
		}
		var job map[string]any
		if shown.Lineage.Job != nil {
			job = map[string]any{"kind": shown.Lineage.Job.Kind, "spec": digestOfJSON(shown.Lineage.Job.Spec)}
		}
		named := add("artifacts/"+s.ID+"/manifest.json", indented.Bytes())
		steps = append(steps, stepOut{N: offset + s.N, ID: s.ID, Kind: s.Kind, Name: s.Name, Inputs: inputs, Method: method, By: "program", Job: job, Manifest: &named, Files: files, Content: s.Content, Summary: summary})
		recorded = append(recorded, recordStep{N: offset + s.N, ID: s.ID, Kind: s.Kind, Name: s.Name, Inputs: inputs, Method: r.Method})
		line := fmt.Sprintf("%d. %s: %s", offset+s.N, s.Name, strings.TrimSpace(m.Lineage.Method.Name+" "+m.Lineage.Method.Version))
		if m.Lineage.Method.Citation != "" {
			line += " (" + m.Lineage.Method.Citation + ")"
		}
		methods = append(methods, line)
		made = append(made, map[string]any{"id": s.ID, "kind": s.Kind, "name": s.Name, "dataset": datasets[stepRolesFirst(r.Spec)], "state": "complete"})
	}
	for _, role := range roles {
		id := datasets[role]
		b := bundleOut{Dataset: id, Name: role, Files: []map[string]any{}}
		for _, rb := range report.Bundles {
			if rb.Role == role {
				b.Run, b.Analysis = rb.Analysis, rb.Analysis
			}
		}
		var paths []string
		for p := range inBundle[id] {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			b.Files = append(b.Files, map[string]any{"path": p, "size": nil, "sha256": inBundle[id][p]})
		}
		bundles = append(bundles, b)
	}

	result := report.Steps[recipe.Result-1]
	when := time.Now().UTC().Format(time.RFC3339)
	if report.Clock != "" {
		when = report.Clock
	}
	var datasetList []map[string]any
	for _, role := range roles {
		datasetList = append(datasetList, map[string]any{"id": datasets[role], "name": role, "path": "situcraft:bundle/" + datasets[role]})
	}
	projectID := "p-run-" + digestOfJSON(json.RawMessage(fmt.Sprintf("%q", report.Recipe.SHA256)))[:16]
	project, _ := json.MarshalIndent(map[string]any{
		"format": "situcraft-project", "version": 1, "id": projectID, "name": "Run of " + recipe.Name, "created": when, "modified": when,
		"datasets": datasetList, "artifacts": made, "decisions": []any{}, "views": []any{}, "history": []any{},
	}, "", " ")
	record, _ := json.MarshalIndent(map[string]any{"format": "situcraft-record", "version": 1, "result": map[string]string{"id": result.ID, "kind": result.Kind, "name": result.Name}, "steps": recorded}, "", " ")
	methodsText := "# Methods\n\n" + fmt.Sprintf("Made without a window by situcraft run (SituCraft %s) from the recipe of %s, step by step from the bundles (%s):\n\n", version, recipe.Name, strings.Join(roles, ", ")) + strings.Join(methods, "\n") + "\n"
	cert := map[string]any{
		"format":   certificateFormat,
		"version":  certificateVersion,
		"software": map[string]any{"name": "SituCraft", "version": version, "commit": nilIfEmpty(buildCommit())},
		"subject":  map[string]string{"id": result.ID, "kind": result.Kind, "name": result.Name},
		"project":  merge(map[string]any{"id": projectID, "name": "Run of " + recipe.Name, "modified": when}, add("project.json", append(project, '\n'))),
		"record":   add("record.json", append(record, '\n')),
		"methods":  add("methods.md", []byte(methodsText)),
		"bundles":  bundles,
		"steps":    steps,
		"notMadeAgain": func() []map[string]any {
			var out []map[string]any
			for i, role := range roles {
				out = append(out, map[string]any{"step": i + 1, "what": role, "why": "the instrument's output: its files are checked by their SHA-256"})
			}
			return out
		}(),
	}
	encoded, err := json.Marshal(cert)
	if err != nil {
		return nil, err
	}
	generic, err := decodeGeneric(encoded)
	if err != nil {
		return nil, err
	}
	fingerprint := sha256Hex(canonicalJSON(generic))
	cert["fingerprint"] = fingerprint
	text, err := json.MarshalIndent(cert, "", " ")
	if err != nil {
		return nil, err
	}
	readme := runReadme(recipe, result, fingerprint, len(roles), len(report.Steps))
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	all := append([]certEntry{{"README.txt", []byte(readme)}, {"certificate.json", append(text, '\n')}}, entries...)
	var listed []map[string]any
	for _, e := range all {
		listed = append(listed, map[string]any{"path": e.name, "size": len(e.data), "sha256": sha256Hex(e.data)})
	}
	list, _ := json.MarshalIndent(map[string]any{"format": "situcraft-certificate-files", "version": 1, "files": listed}, "", " ")
	all = append(all, certEntry{"manifest.json", append(list, '\n')})

	var zipped bytes.Buffer
	w := zip.NewWriter(&zipped)
	for _, e := range all {
		f, err := w.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: certificateZipTime})
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(e.data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, zipped.Bytes(), 0o644); err != nil {
		return nil, err
	}
	return &runCertificate{Path: filepath.Base(path), Fingerprint: fingerprint}, nil
}

func stepRolesFirst(spec json.RawMessage) string {
	if roles := stepRoles(spec); len(roles) > 0 {
		return roles[0]
	}
	return ""
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func merge(a map[string]any, n certNamed) map[string]any {
	a["path"], a["sha256"] = n.Path, n.SHA256
	return a
}

// runReadme is the certificate's README: what it is of, what it proves, how to check it.
func runReadme(recipe *resultRecipe, result runStep, fingerprint string, bundles, steps int) string {
	lines := []string{
		"SituCraft certificate",
		"=====================",
		"",
		fmt.Sprintf("Of:           %s (%s, %s)", result.Name, result.Kind, result.ID),
		fmt.Sprintf("Made by:      situcraft run, from the recipe of %s", recipe.Name),
		fmt.Sprintf("Certified by: SituCraft %s", version),
		"Fingerprint:  " + fingerprint,
		fmt.Sprintf("              (short form %s; cite it with the result)", shortFingerprint(fingerprint)),
		"",
		fmt.Sprintf("This certificate holds what the result rests on: %s (their files by SHA-256) and", plural(bundles, "bundle")),
		fmt.Sprintf("the %s SituCraft's jobs made from them, each job's kind, its spec by digest and the", plural(steps, "step")),
		"files it wrote, by SHA-256.",
		"",
		"What it proves",
		"--------------",
		"That these bundles, analyzed as these steps record, give these files: anyone with the same",
		"bundles can make each step again and compare its files bit for bit (SituCraft gives the same",
		"bits on every computer). It does not prove that the result is right: that rests on the",
		"tissue, the panel and the methods' assumptions.",
		"",
		"To check it",
		"-----------",
		fmt.Sprintf("Install SituCraft %s or later (https://github.com/robert-mcdermott/situcraft/releases), then run:", version),
		"  situcraft verify <this file> --data <a folder holding the bundles>",
		"The bundles are found in the folder by their files' SHA-256, whatever their folders are named.",
		"Exit status: 0 verified, 1 something differs (listed), 2 it could not be checked.",
		"",
		"Paths",
		"-----",
		"Where files lay on the computer that made it is not kept: a bundle's folder is written",
		"situcraft:bundle/<its dataset's ID>, any other file situcraft:file/<its name>.",
	}
	return strings.Join(lines, "\n") + "\n"
}
