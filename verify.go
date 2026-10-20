package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"situcraft/internal/cluster"
	"situcraft/internal/embed"
	"situcraft/internal/jobs"
	"situcraft/internal/label"
)

// "situcraft verify" (wave 8, slice 6; requirement E7; after MaveScape 0.8.0's verify.go, the same
// author's) checks a certificate (web/lib/certificate.js) by making its result again:
//   - the certificate as certified: its fingerprint (the SHA-256 of its canonical JSON, written here
//     as the page writes it) and every file it holds, by SHA-256;
//   - its bundles found in the folders given (--data) by their files' SHA-256, every file it names
//     checked; the other files its steps read (an image, a table of labels, a reference) found by
//     name and checked;
//   - each step SituCraft's jobs made, made again in a SituCraft of its own (its own store of
//     results, its own derived data, in a temporary folder) from the spec its manifest recorded,
//     with this computer's paths put back, and every file it wrote compared bit for bit (SituCraft
//     gives the same bits on every computer; no tolerance);
//   - the page's own records: each as certified (its digest), and those the page computes (a
//     filter's cells, a design, a figure's drawing) computed again in a headless page
//     (verifypage.go).
//
// Exit status: 0 verified; 1 something differs (listed, with where it first differs); 2 it could
// not be checked (a bundle or file missing: give its folder with --data; no browser for the page's
// records; a wrong command line).

const (
	exitVerified  = 0
	exitDiffers   = 1
	exitUnchecked = 2
	verifyClient  = "situcraft verify"
	// A step's verdicts.
	stepVerified   = "verified"
	stepDiffers    = "differs"
	stepUnchecked  = "not checkable"
	stepNotRemade  = "not made again"
	stepAsRecorded = "as certified"
)

type verifyOptions struct {
	certificate string
	data        []string
	report      string
	overwrite   bool
	chrome      string
	noPage      bool
	keep        bool
	dev         bool
	timeout     time.Duration
}

type dataFlag []string

func (d *dataFlag) String() string { return strings.Join(*d, ", ") }
func (d *dataFlag) Set(value string) error {
	*d = append(*d, value)
	return nil
}

func parseVerifyArgs(args []string, stderr io.Writer) (verifyOptions, error) {
	var opts verifyOptions
	var data dataFlag
	flags := flag.NewFlagSet("situcraft verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Var(&data, "data", "a folder holding the bundles (and any other file) the certificate names, found by their SHA-256; may be repeated")
	flags.StringVar(&opts.report, "report", "", "also write what was found as JSON to this file")
	flags.BoolVar(&opts.overwrite, "overwrite", false, "replace the report if it exists")
	flags.StringVar(&opts.chrome, "chrome", "", "the Chrome, Chromium, Edge or Brave the page's records are computed again in (default: CHROME, else one installed)")
	flags.BoolVar(&opts.noPage, "no-page", false, "do not compute the page's records again (each is checked against its digest only; the verdict says so)")
	flags.BoolVar(&opts.keep, "keep", false, "keep the SituCraft the steps were made again in (its folder is printed)")
	flags.BoolVar(&opts.dev, "dev", false, "serve web/ from the working directory instead of the embedded copy")
	flags.DurationVar(&opts.timeout, "timeout", 6*time.Hour, "stop a verification that takes longer")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintln(out, "Usage: situcraft verify [--data DIR]... [--report verification.json] certificate.zip|report.html")
		fmt.Fprintln(out, "Makes a SituCraft certificate's result again from its bundles, found in the folders")
		fmt.Fprintln(out, "given by their files' SHA-256, and compares every file bit for bit. A report is checked")
		fmt.Fprintln(out, "by the certificate it carries, and its numbers, figures and methods against it.")
		fmt.Fprintln(out, "Exit status: 0 verified, 1 something differs (listed), 2 it could not be checked (a bundle")
		fmt.Fprintln(out, "or file missing: give its folder with --data; no browser; a wrong command line).")
		flags.PrintDefaults()
	}
	positional, err := parseArgs(flags, args)
	if err != nil {
		return opts, err
	}
	if len(positional) != 1 {
		return opts, errors.New("name one certificate (.zip) or report (.html) to verify")
	}
	if opts.certificate, err = filepath.Abs(positional[0]); err != nil {
		return opts, err
	}
	if info, err := os.Stat(opts.certificate); err != nil || info.IsDir() {
		return opts, fmt.Errorf("%s is not a file", positional[0])
	}
	for _, d := range data {
		path, err := filepath.Abs(d)
		if err != nil {
			return opts, err
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return opts, fmt.Errorf("--data %s is not a folder", d)
		}
		opts.data = append(opts.data, path)
	}
	if opts.report != "" {
		if opts.report, err = filepath.Abs(opts.report); err != nil {
			return opts, err
		}
		if _, err := os.Stat(opts.report); err == nil && !opts.overwrite {
			return opts, fmt.Errorf("%s exists; give --overwrite to replace it", opts.report)
		}
	}
	if opts.timeout <= 0 {
		return opts, errors.New("--timeout must be positive")
	}
	return opts, nil
}

// --- The certificate ------------------------------------------------------------------------------

type certFile struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Size     *int64 `json:"size"`
	SHA256   string `json:"sha256"`
	Location string `json:"location,omitempty"`
}

type certNamed struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type certBundle struct {
	Dataset string     `json:"dataset"`
	Name    string     `json:"name"`
	Run     string     `json:"run"`
	Files   []certFile `json:"files"`
}

type certStep struct {
	N        int                          `json:"n"`
	ID       string                       `json:"id"`
	Kind     string                       `json:"kind"`
	Name     string                       `json:"name"`
	By       string                       `json:"by"`
	Inputs   []int                        `json:"inputs"`
	Bundle   string                       `json:"bundle,omitempty"`
	Job      *struct{ Kind, Spec string } `json:"job,omitempty"`
	Manifest *certNamed                   `json:"manifest,omitempty"`
	Files    []certFile                   `json:"files,omitempty"`
	Record   string                       `json:"record,omitempty"`
	Drawing  json.RawMessage              `json:"drawing,omitempty"`
	// Content: the SHA-256 of a clustering's, a map's or labels' content (contentDigest).
	Content  string `json:"content,omitempty"`
	Software []struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"software,omitempty"`
}

type certificate struct {
	Format   string `json:"format"`
	Version  int    `json:"version"`
	Software struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
	} `json:"software"`
	Subject struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"subject"`
	Project struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Modified string `json:"modified"`
		Path     string `json:"path"`
		SHA256   string `json:"sha256"`
	} `json:"project"`
	Record       certNamed    `json:"record"`
	Methods      certNamed    `json:"methods"`
	Bundles      []certBundle `json:"bundles"`
	Steps        []certStep   `json:"steps"`
	NotMadeAgain []struct {
		Step int    `json:"step"`
		What string `json:"what"`
		Why  string `json:"why"`
	} `json:"notMadeAgain"`
	Fingerprint string `json:"fingerprint"`
}

const certificateFormat = "situcraft-certificate"
const certificateVersion = 1

// readCertificateZip reads a certificate's zip (or the one a report carries): the certificate, its
// entries (name → bytes), and what is not as certified (its fingerprint, a file it names, an entry
// its list names).
func readCertificateZip(path string) (*certificate, map[string][]byte, []string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, err
	}
	if isReport(raw) {
		if raw, err = carriedCertificate(raw); err != nil {
			return nil, nil, nil, fmt.Errorf("%s is not a SituCraft report: %v", filepath.Base(path), err)
		}
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%s is not a SituCraft certificate: %v", filepath.Base(path), err)
	}
	files := map[string][]byte{}
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > 1<<30 {
			return nil, nil, nil, fmt.Errorf("%s holds %s of %d bytes: not a certificate", filepath.Base(path), f.Name, f.UncompressedSize64)
		}
		r, err := f.Open()
		if err != nil {
			return nil, nil, nil, err
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %v", f.Name, err)
		}
		files[f.Name] = data
	}
	raw, ok := files["certificate.json"]
	if !ok {
		return nil, nil, nil, fmt.Errorf("%s is not a SituCraft certificate: it holds no certificate.json", filepath.Base(path))
	}
	var c certificate
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, nil, nil, fmt.Errorf("certificate.json cannot be read: %v", err)
	}
	if c.Format != certificateFormat {
		return nil, nil, nil, errors.New("certificate.json is not a SituCraft certificate")
	}
	if c.Version < 1 || c.Version > certificateVersion {
		return nil, nil, nil, fmt.Errorf("the certificate was written by a newer SituCraft (format %d); check it with SituCraft %s or later", c.Version, c.Software.Version)
	}
	var problems []string
	generic, err := decodeGeneric(raw)
	if err != nil {
		return nil, nil, nil, err
	}
	object, _ := generic.(map[string]any)
	delete(object, "fingerprint")
	if sha256Hex(canonicalJSON(object)) != c.Fingerprint {
		problems = append(problems, "certificate.json is not as certified: its fingerprint does not match its content")
	}
	named := []certNamed{{c.Project.Path, c.Project.SHA256}, c.Record, c.Methods}
	for _, s := range c.Steps {
		if s.Manifest != nil {
			named = append(named, *s.Manifest)
		}
	}
	isNamed := map[string]bool{"certificate.json": true}
	for _, n := range named {
		isNamed[n.Path] = true
		data, ok := files[n.Path]
		switch {
		case !ok:
			problems = append(problems, n.Path+" is missing")
		case sha256Hex(data) != n.SHA256:
			problems = append(problems, n.Path+" is not as certified (its SHA-256 differs)")
		}
	}
	if listed, ok := files["manifest.json"]; ok {
		var list struct {
			Files []certNamed `json:"files"`
		}
		if err := json.Unmarshal(listed, &list); err != nil {
			problems = append(problems, "manifest.json cannot be read")
		}
		for _, f := range list.Files {
			if data, ok := files[f.Path]; ok && !isNamed[f.Path] && sha256Hex(data) != f.SHA256 {
				problems = append(problems, f.Path+" is not as written (its SHA-256 differs from manifest.json's)")
			}
		}
	}
	return &c, files, problems, nil
}

func decodeGeneric(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// canonicalJSON writes a value decoded with UseNumber as the page's canonicalJSON
// (web/lib/figure-manifest.js) does: every object's keys sorted, numbers as written (the page
// writes them with JSON.stringify), strings escaped as JSON.stringify escapes them.
func canonicalJSON(v any) []byte {
	var b bytes.Buffer
	writeCanonical(&b, v)
	return b.Bytes()
}

func writeCanonical(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSString(b, k)
			b.WriteByte(':')
			writeCanonical(b, x[k])
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCanonical(b, e)
		}
		b.WriteByte(']')
	case string:
		writeJSString(b, x)
	case json.Number:
		b.WriteString(string(x))
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	default:
		b.WriteString("null")
	}
}

// writeJSString writes a string as JSON.stringify does: quotes and backslashes escaped, the short
// escapes for \b \f \n \r \t, \u00xx (lowercase) for the other control characters, and every
// other character as itself.
func writeJSString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// --- Finding the bundles and files ----------------------------------------------------------------

// foundBundle is a certificate's bundle as found in the folders given.
type foundBundle struct {
	Dataset string   `json:"dataset"`
	Name    string   `json:"name"`
	Path    string   `json:"path,omitempty"`
	Status  string   `json:"status"` // "found", "differs" or "missing"
	Checked int      `json:"checked"`
	Differ  []string `json:"differ,omitempty"`
	Missing []string `json:"missing,omitempty"`
}

// bundleFolders answers the folders under dirs (to a few levels) that hold experiment.xenium.
func bundleFolders(dirs []string) []string {
	var out []string
	for _, root := range dirs {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || strings.Count(rel, string(filepath.Separator)) >= 5) {
				return filepath.SkipDir
			}
			if info, err := os.Stat(filepath.Join(path, "experiment.xenium")); err == nil && !info.IsDir() {
				out = append(out, path)
				return filepath.SkipDir
			}
			return nil
		})
	}
	return out
}

// locateBundles finds each certified bundle among the folders by the SHA-256 of its
// experiment.xenium, then checks every file the certificate names by SHA-256.
func locateBundles(c *certificate, dirs []string) []foundBundle {
	folders := bundleFolders(dirs)
	sums := map[string]string{}
	for _, f := range folders {
		if sum, err := fileSHA256(filepath.Join(f, "experiment.xenium")); err == nil {
			sums[f] = sum
		}
	}
	var out []foundBundle
	for _, b := range c.Bundles {
		found := foundBundle{Dataset: b.Dataset, Name: b.Name, Status: "missing"}
		want := ""
		for _, f := range b.Files {
			if f.Path == "experiment.xenium" {
				want = f.SHA256
			}
		}
		for _, folder := range folders {
			if want != "" && sums[folder] == want {
				found.Path, found.Status = folder, "found"
				break
			}
		}
		if found.Path != "" {
			for _, f := range b.Files {
				if f.SHA256 == "" {
					continue
				}
				sum, err := fileSHA256(filepath.Join(found.Path, filepath.FromSlash(f.Path)))
				switch {
				case err != nil:
					found.Missing = append(found.Missing, f.Path)
				case sum != f.SHA256:
					found.Differ = append(found.Differ, f.Path)
				default:
					found.Checked++
				}
			}
			if len(found.Differ) > 0 {
				found.Status = "differs"
			} else if len(found.Missing) > 0 {
				found.Status = "missing"
			}
		}
		out = append(out, found)
	}
	return out
}

// findFile finds a file by its name under the folders (the first; with sum, the first of that
// SHA-256).
func findFile(dirs []string, name, sum string) string {
	found := ""
	for _, root := range dirs {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || found != "" {
				return filepath.SkipDir
			}
			if d.IsDir() {
				if rel, _ := filepath.Rel(root, path); rel != "." && (strings.HasPrefix(d.Name(), ".") || strings.Count(rel, string(filepath.Separator)) >= 8) {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() == name {
				if sum == "" {
					found = path
				} else if s, err := fileSHA256(path); err == nil && s == sum {
					found = path
				}
			}
			return nil
		})
		if found != "" {
			break
		}
	}
	return found
}

// A stand-in for a path, as a JSON string: "situcraft:bundle/…" or "situcraft:file/…".
var standIn = regexp.MustCompile(`"situcraft:(?:bundle|file)/(?:[^"\\]|\\.)*"`)

// localize puts this computer's paths back into JSON the certificate wrote without them
// (situcraft:bundle/<dataset>[/<path>] and situcraft:file/<name>), in its text: everything else
// stays as recorded, byte for byte (a job writes some of its spec through as it was given). It
// answers what it could not find.
func localize(raw []byte, bundles map[string]string, file func(name string) string) ([]byte, []string) {
	var missing []string
	out := standIn.ReplaceAllFunc(raw, func(m []byte) []byte {
		var text string
		if json.Unmarshal(m, &text) != nil {
			return m
		}
		local := ""
		if rest, ok := strings.CutPrefix(text, "situcraft:bundle/"); ok {
			id, sub, _ := strings.Cut(rest, "/")
			root, ok := bundles[id]
			if !ok {
				missing = append(missing, "the bundle of "+id)
				return m
			}
			local = root
			if sub != "" {
				local = filepath.Join(root, filepath.FromSlash(sub))
			}
		} else if name, ok := strings.CutPrefix(text, "situcraft:file/"); ok {
			if local = file(name); local == "" {
				missing = append(missing, name)
				return m
			}
		}
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		e.Encode(local)
		return bytes.TrimRight(b.Bytes(), "\n")
	})
	return out, missing
}

// --- Verifying ---------------------------------------------------------------------------------

// stepFound is what verify found of one step.
type stepFound struct {
	N       int      `json:"n"`
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Name    string   `json:"name"`
	By      string   `json:"by"`
	Verdict string   `json:"verdict"`
	Detail  string   `json:"detail,omitempty"`
	Files   int      `json:"files,omitempty"`
	Differ  []string `json:"differ,omitempty"`
}

// verifyReport is what verify found, as --report writes it.
type verifyReport struct {
	Verdict     string        `json:"verdict"`
	Fingerprint string        `json:"fingerprint"`
	Subject     any           `json:"subject"`
	Certified   any           `json:"certifiedBy"`
	Verifier    any           `json:"verifiedBy"`
	Problems    []string      `json:"problems,omitempty"`
	Bundles     []foundBundle `json:"bundles"`
	Steps       []stepFound   `json:"steps"`
	FirstDiffer *stepFound    `json:"firstDiffers,omitempty"`
	Notes       []string      `json:"notes,omitempty"`
	Seconds     float64       `json:"seconds"`
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	opts, err := parseVerifyArgs(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return exitVerified
	}
	if err != nil {
		fmt.Fprintln(stderr, "situcraft verify:", err)
		return exitUnchecked
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	report, err := verifyCertificate(ctx, opts, stdout)
	if err != nil {
		fmt.Fprintln(stderr, "situcraft verify:", err)
		return exitUnchecked
	}
	if opts.report != "" {
		text, _ := json.MarshalIndent(report, "", "  ")
		if _, err := writeOutput(opts.report, opts.overwrite, bytes.NewReader(append(text, '\n'))); err != nil {
			fmt.Fprintln(stderr, "situcraft verify: could not write the report:", err)
			return exitUnchecked
		}
	}
	switch report.Verdict {
	case stepVerified:
		return exitVerified
	case stepDiffers:
		return exitDiffers
	}
	return exitUnchecked
}

// verifyCertificate does the work and prints what it finds as it goes.
func verifyCertificate(ctx context.Context, opts verifyOptions, out io.Writer) (*verifyReport, error) {
	began := time.Now()
	c, files, problems, err := readCertificateZip(opts.certificate)
	if err != nil {
		return nil, err
	}
	report := &verifyReport{Fingerprint: c.Fingerprint, Subject: c.Subject, Certified: c.Software, Verifier: map[string]string{"name": "SituCraft", "version": version, "commit": buildCommit()}, Problems: problems}
	fmt.Fprintf(out, "SituCraft %s: verifying %s\n", version, filepath.Base(opts.certificate))
	fmt.Fprintf(out, "Of %s, certified by SituCraft %s; fingerprint %s.\n", c.Subject.Name, c.Software.Version, c.Fingerprint)
	if c.Software.Version != version {
		report.Notes = append(report.Notes, fmt.Sprintf("Certified by SituCraft %s and verified by %s: a step may differ where the version changed how it is made.", c.Software.Version, version))
	}
	for _, p := range problems {
		fmt.Fprintln(out, "  The archive:", p)
	}
	// A report: its numbers, figures and methods, the certificate's.
	if page, err := os.ReadFile(opts.certificate); err == nil && isReport(page) {
		checked, more := checkReport(page, c, files)
		if len(more) == 0 {
			fmt.Fprintf(out, "The report carries it: its %s are the certificate's.\n", checked)
		} else {
			fmt.Fprintf(out, "The report carries it, but %s.\n", strings.Join(more, "; "))
		}
		report.Problems = append(report.Problems, more...)
	}

	// The bundles, by their files' SHA-256.
	report.Bundles = locateBundles(c, opts.data)
	bundles := map[string]string{}
	for _, b := range report.Bundles {
		switch b.Status {
		case "found":
			bundles[b.Dataset] = b.Path
			fmt.Fprintf(out, "Bundle %s: found at %s, %d files as certified.\n", b.Name, b.Path, b.Checked)
		case "differs":
			fmt.Fprintf(out, "Bundle %s: at %s, but %s not as certified.\n", b.Name, b.Path, strings.Join(b.Differ, ", "))
		default:
			fmt.Fprintf(out, "Bundle %s: not found%s (give the folder holding it with --data).\n", b.Name, missingText(b.Missing))
		}
	}
	fileCache := map[string]string{}
	file := func(name string) string {
		if p, ok := fileCache[name]; ok {
			return p
		}
		p := findFile(opts.data, name, "")
		fileCache[name] = p
		return p
	}

	// A SituCraft of its own, in a temporary folder.
	scratch, err := os.MkdirTemp("", "situcraft-verify-")
	if err != nil {
		return nil, err
	}
	if opts.keep {
		fmt.Fprintf(out, "The steps are made again in %s (kept).\n", scratch)
	} else {
		defer os.RemoveAll(scratch)
	}
	cfg := config{host: "127.0.0.1", port: 0, window: "none", remote: true, dev: opts.dev, dataDir: filepath.Join(scratch, "library"), cacheDir: filepath.Join(scratch, "cache"), cacheLimit: 0, examplesDir: filepath.Join(scratch, "examples")}
	running, err := start(cfg, io.Discard)
	if err != nil {
		return nil, err
	}
	go running.serve()
	defer running.stop(5 * time.Second)
	a := running.app

	// The bundles' derived data, built as opening them builds it.
	for _, b := range report.Bundles {
		if b.Status != "found" {
			continue
		}
		job, _, err := a.jobs.Submit("cache", "Derived data of "+b.Name, cacheJobKey(b.Path), map[string]string{"bundle": b.Path}, a.cacheJob(b.Path))
		if err == nil {
			job, err = a.jobs.Wait(ctx, job.ID)
		}
		if err != nil || job.State != jobs.Complete {
			return nil, fmt.Errorf("the derived data of %s could not be built: %v%s", b.Name, err, job.Error)
		}
	}

	// The project certified, its records by ID.
	doc, items := projectItems(files[c.Project.Path])
	byNumber := map[int]*stepFound{}
	ran := map[string]bool{}
	var compute []int
	for _, s := range c.Steps {
		found := stepFound{N: s.N, ID: s.ID, Kind: s.Kind, Name: s.Name, By: s.By}
		switch s.By {
		case "instrument":
			b := bundleOf(report.Bundles, s.Bundle)
			switch {
			case b == nil || b.Status == "missing":
				found.Verdict, found.Detail = stepUnchecked, "its bundle was not found"
			case b.Status == "differs":
				found.Verdict, found.Detail, found.Differ = stepDiffers, "its files are not as certified", b.Differ
			default:
				found.Verdict, found.Detail, found.Files = stepVerified, "its files as certified", b.Checked
			}
		case "program":
			remakeStep(ctx, a, s, files, bundles, file, byNumber, ran, &found)
		default:
			checkPageRecord(s, items, &found)
		}
		byNumber[s.N] = &found
		report.Steps = append(report.Steps, found)
		if s.By == "page" && pageComputed[s.Kind] && found.Verdict == stepAsRecorded {
			compute = append(compute, len(report.Steps)-1)
			continue
		}
		fmt.Fprintf(out, "  %d. %s (%s): %s%s\n", s.N, s.Name, s.Kind, found.Verdict, detailText(found))
	}

	// The page's records it computes, computed again.
	verifyPageRecords(ctx, opts, c, doc, files[c.Project.Path], compute, bundles, running, report, out)

	report.Verdict = verdictOf(report)
	for i := range report.Steps {
		if report.Steps[i].Verdict == stepDiffers {
			first := report.Steps[i]
			report.FirstDiffer = &first
			break
		}
	}
	report.Seconds = time.Since(began).Seconds()
	switch report.Verdict {
	case stepVerified:
		fmt.Fprintf(out, "Verified: every step made again gives the files certified (%.0f s).\n", report.Seconds)
	case stepDiffers:
		where := ""
		if f := report.FirstDiffer; f != nil {
			where = fmt.Sprintf(": it first differs at step %d, %s%s", f.N, f.Name, detailText(*f))
		}
		fmt.Fprintf(out, "Differs%s (%.0f s).\n", where, report.Seconds)
	default:
		fmt.Fprintf(out, "Not checkable: %s (%.0f s).\n", uncheckedText(report), report.Seconds)
	}
	for _, n := range report.Notes {
		fmt.Fprintln(out, "Note:", n)
	}
	return report, nil
}

func bundleOf(list []foundBundle, dataset string) *foundBundle {
	for i := range list {
		if list[i].Dataset == dataset {
			return &list[i]
		}
	}
	return nil
}

// remakeStep makes one of SituCraft's steps again from its recorded spec and compares its files.
func remakeStep(ctx context.Context, a *app, s certStep, files map[string][]byte, bundles map[string]string, file func(string) string, byNumber map[int]*stepFound, ran map[string]bool, found *stepFound) {
	// Its inputs: made again (or found) first.
	for _, n := range s.Inputs {
		in := byNumber[n]
		if in == nil {
			continue
		}
		switch {
		case in.Verdict == stepUnchecked && in.By == "instrument":
			found.Verdict, found.Detail = stepUnchecked, fmt.Sprintf("its bundle, %s, was not found", in.Name)
			return
		case in.Verdict == stepUnchecked || in.Verdict == stepNotRemade && in.By == "program":
			found.Verdict, found.Detail = stepUnchecked, fmt.Sprintf("step %d, %s, could not be made again", in.N, in.Name)
			return
		}
	}
	var manifest struct {
		Lineage struct {
			Job *struct {
				Kind string          `json:"kind"`
				Spec json.RawMessage `json:"spec"`
			} `json:"job"`
		} `json:"lineage"`
	}
	if s.Manifest == nil || json.Unmarshal(files[s.Manifest.Path], &manifest) != nil {
		found.Verdict, found.Detail = stepUnchecked, "its manifest cannot be read"
		return
	}
	job := manifest.Lineage.Job
	if job == nil || len(job.Spec) == 0 || string(job.Spec) == "null" || !remadeKinds[job.Kind] {
		why := "an alignment, made by hand or found automatically: its file is certified by SHA-256"
		switch {
		case s.Kind == "image":
			why = "an image read where it lies (its own file records where): the image is certified by SHA-256"
		case s.Kind == "alignment-check":
			why = "an alignment's check: certified by SHA-256"
		}
		found.Verdict, found.Detail = stepNotRemade, why
		return
	}
	spec, missing := localize(job.Spec, bundles, file)
	if len(missing) > 0 {
		found.Verdict, found.Detail = stepUnchecked, "not found: "+strings.Join(missing, ", ")+" (give its folder with --data)"
		return
	}
	key := job.Kind + "\x00" + string(spec)
	if !ran[key] {
		fn, err := a.rerun(job.Kind, spec, s.ID)
		if err != nil {
			found.Verdict, found.Detail = stepUnchecked, err.Error()
			return
		}
		submitted, _, err := a.jobs.Submit(job.Kind, "Made again: "+s.Name, "verify\x00"+s.ID, json.RawMessage(spec), fn)
		if err == nil {
			submitted, err = a.jobs.Wait(ctx, submitted.ID)
		}
		if err != nil {
			found.Verdict, found.Detail = stepUnchecked, err.Error()
			return
		}
		ran[key] = true
		if submitted.State != jobs.Complete {
			found.Verdict, found.Detail = stepDiffers, fmt.Sprintf("its job ended %s: %s", submitted.State, submitted.Error)
			return
		}
	}
	m, err := a.artifacts.Get(s.ID)
	if err != nil {
		found.Verdict, found.Detail = stepDiffers, "made again, it is not "+s.ID+" (its inputs or parameters give another result)"
		return
	}
	made := map[string]string{}
	for _, f := range m.Files {
		made[f.Path] = f.SHA256
		if f.Location != "bundle" && f.Location != "place" {
			if p, err := a.artifacts.Path(s.ID, f.Path); err == nil {
				if sum, err := fileSHA256(p); err == nil {
					made[f.Path] = sum
				}
			}
		}
	}
	for _, f := range s.Files {
		if f.Location == "place" {
			continue
		}
		found.Files++
		got, ok := made[f.Path]
		switch {
		case !ok:
			found.Differ = append(found.Differ, f.Path+" (not made)")
		case got != f.SHA256:
			found.Differ = append(found.Differ, f.Path)
		}
		delete(made, f.Path)
	}
	for path := range made {
		found.Differ = append(found.Differ, path+" (made, not certified)")
	}
	sort.Strings(found.Differ)
	// A clustering's, a map's or labels' content, where the certificate holds it: its files and its
	// description but for how long the run took.
	if s.Content != "" {
		content, err := contentDigest(m.Kind, filepath.Join(a.artifacts.Dir(), s.ID))
		switch {
		case err != nil:
			found.Verdict, found.Detail = stepDiffers, "its content cannot be read: "+err.Error()
		case content != s.Content:
			found.Verdict, found.Detail = stepDiffers, "its content differs"+madeByText(s)
		case len(found.Differ) > 0:
			found.Verdict, found.Detail, found.Differ = stepVerified, "its content the same; only how long its run took differs", nil
		default:
			found.Verdict, found.Detail = stepVerified, "every file the same"
		}
		return
	}
	if len(found.Differ) > 0 {
		found.Verdict, found.Detail = stepDiffers, "its files differ"+madeByText(s)
		return
	}
	found.Verdict, found.Detail = stepVerified, "every file the same"
}

// madeByText says which SituCraft made a step, when not this one: a version may make it otherwise.
func madeByText(s certStep) string {
	for _, sw := range s.Software {
		if sw.Name == "situcraft" && sw.Version != version {
			return fmt.Sprintf("; made by SituCraft %s, made again by %s", sw.Version, version)
		}
	}
	return ""
}

// contentDigest is the SHA-256 of an artifact's content where its kind's files also record how
// long its run took (a clustering, a map, labels): its files and its description without the run's
// timings, the same on any computer (each package's Digest). "" for the kinds whose files are
// compared byte for byte.
func contentDigest(kind, dir string) (string, error) {
	switch kind {
	case "clustering":
		return cluster.Digest(dir)
	case "embedding":
		return embed.Digest(dir)
	case "labels":
		return label.Digest(dir)
	}
	return "", nil
}

// The jobs made again from the spec their manifests record.
var remadeKinds = map[string]bool{
	"segmentation": true, "expansion": true, "refinement": true, "import": true, "plant": true, "comparison": true,
	"clustering": true, "embedding": true, "analysis": true, "labels": true, "label-import": true, "label-reference": true,
	"graph": true, "spatial": true, "cohort": true, "abundance": true, "expression": true, "spatial-tests": true,
}

// rerun answers the job that makes an artifact again from the spec its manifest recorded: the
// function the API submits, given the spec as recorded (with this computer's paths).
func (a *app) rerun(kind string, spec json.RawMessage, id string) (jobs.Func, error) {
	decode := func(v any) error {
		if err := json.Unmarshal(spec, v); err != nil {
			return fmt.Errorf("its %s job's spec cannot be read: %v", kind, err)
		}
		return nil
	}
	switch kind {
	case "segmentation":
		var s struct {
			Bundle string `json:"bundle"`
		}
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.segmentationJob(s.Bundle), nil
	case "expansion":
		var s expansionSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.expansionJob(s.Bundle, s.DistanceUm), nil
	case "refinement":
		var s refineSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.refineJob(s), nil
	case "import":
		var s importSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.importJob(s.Bundle, s.Options), nil
	case "plant":
		var s plantSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.plantJob(s.Bundle, s.Params), nil
	case "comparison":
		var s compareSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.comparisonJob(s.Bundle, s.A, s.B), nil
	case "clustering":
		var s clusterSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.clusteringJob(s, id), nil
	case "embedding":
		var s embedSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.embeddingJob(s, id), nil
	case "analysis":
		var s analysisSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.analysisJob(s), nil
	case "labels":
		var s labelSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.labelsJob(s, id), nil
	case "label-import":
		var s importLabelsSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.labelImportJob(s, id), nil
	case "label-reference":
		var s referenceSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.referenceJob(s, id), nil
	case "graph":
		var s graphSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.graphJob(s, id), nil
	case "spatial":
		var s spatialSpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.spatialJob(s, id), nil
	case "cohort":
		var s cohortSummarySpec
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.cohortSummaryJob(s, id), nil
	case "abundance":
		var s abundanceRequest
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.abundanceJob(s, id), nil
	case "expression":
		var s expressionRequest
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.expressionJob(s, id), nil
	case "spatial-tests":
		var s spatialTestsRequest
		if err := decode(&s); err != nil {
			return nil, err
		}
		return a.spatialTestsJob(s, id), nil
	}
	return nil, fmt.Errorf("a %s job is not made again", kind)
}

// verdictOf: differs when anything differs (the archive, a bundle, a step); not checkable when
// something could not be checked; else verified.
func verdictOf(r *verifyReport) string {
	if len(r.Problems) > 0 {
		return stepDiffers
	}
	unchecked := false
	for _, b := range r.Bundles {
		if b.Status == "differs" {
			return stepDiffers
		}
		if b.Status != "found" {
			unchecked = true
		}
	}
	for _, s := range r.Steps {
		if s.Verdict == stepDiffers {
			return stepDiffers
		}
		if s.Verdict == stepUnchecked {
			unchecked = true
		}
	}
	if unchecked {
		return stepUnchecked
	}
	return stepVerified
}

func uncheckedText(r *verifyReport) string {
	var parts []string
	for _, b := range r.Bundles {
		if b.Status != "found" {
			parts = append(parts, "the bundle "+b.Name+" was not found")
		}
	}
	n := 0
	for _, s := range r.Steps {
		if s.Verdict == stepUnchecked {
			n++
		}
	}
	if n > 0 {
		parts = append(parts, fmt.Sprintf("%d steps could not be made again", n))
	}
	if len(parts) == 0 {
		return "see the steps"
	}
	return strings.Join(parts, "; ")
}

func detailText(s stepFound) string {
	text := ""
	if s.Detail != "" {
		text = " (" + s.Detail
		if len(s.Differ) > 0 {
			shown := s.Differ
			if len(shown) > 4 {
				shown = append(append([]string(nil), shown[:4]...), fmt.Sprintf("%d more", len(s.Differ)-4))
			}
			text += ": " + strings.Join(shown, ", ")
		}
		text += ")"
	}
	return text
}

func missingText(files []string) string {
	if len(files) == 0 {
		return ""
	}
	return ", or missing " + strings.Join(files, ", ")
}
