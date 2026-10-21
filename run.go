package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/xenium"
)

// "situcraft run" (wave 9, slice 5; requirements E5 and A5) makes a result's recipe (recipes.go)
// on a bundle without a window: in a SituCraft of its own, its library in the output folder, each
// step's job made from its spec with the bundle's folder, the files and the earlier steps' ids put
// back, as situcraft verify makes a certificate's steps again; so that it runs on a cluster's
// nodes. Its outputs are the artifacts (in <out>/library), run.json (the inputs and outputs by
// SHA-256, each step, why it stopped when it did not finish) and certificate.zip (run_certificate.go),
// which situcraft verify checks. The same recipe on the same bundle gives the same files on any
// computer and any number of threads; --clock records them as made at a fixed time, so that their
// manifests are the same too.
//
// Exit status: 0 made; 1 a step failed (said in run.json); 2 it could not run (a wrong command
// line, a recipe or bundle that cannot be read, a file missing, a step that cannot be made on this
// bundle).

const (
	exitRunMade   = 0
	exitRunFailed = 1
	exitRunNotRun = 2
	runFormat     = "situcraft-run"
	runVersion    = 1
)

type runOptions struct {
	recipe    string
	bundles   map[string]string // role ("" for the recipe's only bundle) → folder
	files     map[string]string // name → path
	data      []string
	out       string
	cache     string
	threads   int
	clock     time.Time
	dryRun    bool
	timeout   time.Duration
	keepCache bool
	report    bool
	chrome    string
	// A batch: bundles' folders, each made in a folder of its own (--index: only the nth).
	batch []string
	index int
}

func parseRunArgs(args []string, stderr io.Writer) (runOptions, error) {
	opts := runOptions{bundles: map[string]string{}, files: map[string]string{}}
	var bundles, files, data dataFlag
	var clock, list, folder string
	flags := flag.NewFlagSet("situcraft run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&opts.recipe, "recipe", "", "the recipe to make (a result's, saved from SituCraft)")
	flags.Var(&bundles, "bundle", "the bundle's folder; with several, role=folder for each (the recipe names the roles); may be repeated")
	flags.Var(&files, "file", "name=path: a file the recipe names (a table of labels, a reference); may be repeated")
	flags.Var(&data, "data", "a folder holding the files the recipe names, found by their name and SHA-256; may be repeated")
	flags.StringVar(&list, "bundles", "", "a batch: a file listing bundles' folders, one a line, each made in a folder of its own under --out")
	flags.StringVar(&folder, "bundle-dir", "", "a batch: every bundle in this folder (each folder holding experiment.xenium), each made in a folder of its own under --out")
	flags.IntVar(&opts.index, "index", 0, "with --bundles or --bundle-dir, make only the nth bundle (from 1; a cluster's array job passes its task's number)")
	flags.StringVar(&opts.out, "out", "", "a new (or empty) folder for its library, run.json and certificate.zip")
	flags.StringVar(&opts.cache, "cache", "", "the folder for the bundles' derived data, kept to be used again (default: a temporary folder, removed)")
	flags.IntVar(&opts.threads, "threads", 0, "the threads it may use (default: every core); the result is the same on any number")
	flags.StringVar(&clock, "clock", "", "record what it makes as made at this time (RFC 3339, such as 2026-01-01T00:00:00Z), so that the same recipe gives the same files and manifests")
	flags.BoolVar(&opts.dryRun, "dry-run", false, "check the recipe, its bundles and files, and say what it would make, without making it")
	flags.DurationVar(&opts.timeout, "timeout", 72*time.Hour, "stop a run that takes longer")
	flags.BoolVar(&opts.report, "report", false, "also write, in a headless Chrome, Chromium, Edge or Brave, the result's report (report.html) and methods (methods.md), and the run's project in its library")
	flags.StringVar(&opts.chrome, "chrome", "", "the browser --report writes in (default: CHROME, else one installed)")
	flags.Usage = func() {
		out := flags.Output()
		fmt.Fprintln(out, "Usage: situcraft run --recipe recipe.json --bundle DIR --out DIR [--file NAME=PATH]... [--threads N] [--clock TIME]")
		fmt.Fprintln(out, "Makes a result's recipe on a bundle without a window, in a SituCraft of its own: its artifacts")
		fmt.Fprintln(out, "(in DIR/library), run.json and certificate.zip, which situcraft verify checks.")
		fmt.Fprintln(out, "Exit status: 0 made, 1 a step failed (said in run.json), 2 it could not run (a wrong command")
		fmt.Fprintln(out, "line, a recipe or bundle that cannot be read, a file missing, a step it cannot make on the bundle).")
		flags.PrintDefaults()
	}
	positional, err := parseArgs(flags, args)
	if err != nil {
		return opts, err
	}
	if len(positional) > 0 {
		return opts, fmt.Errorf("unexpected %q: name the recipe with --recipe and the bundle with --bundle", positional[0])
	}
	if opts.recipe == "" {
		return opts, errors.New("name the recipe with --recipe")
	}
	if list != "" || folder != "" {
		if len(bundles) > 0 || list != "" && folder != "" {
			return opts, errors.New("name the bundles with one of --bundle, --bundles and --bundle-dir")
		}
		if opts.batch, err = batchBundles(list, folder); err != nil {
			return opts, err
		}
		if opts.index < 0 || opts.index > len(opts.batch) {
			return opts, fmt.Errorf("--index %d: the batch has %s (1 to %d)", opts.index, plural(len(opts.batch), "bundle"), len(opts.batch))
		}
	} else if opts.index != 0 {
		return opts, errors.New("--index chooses a bundle of --bundles or --bundle-dir")
	}
	if len(bundles) == 0 && len(opts.batch) == 0 {
		return opts, errors.New("name the bundle's folder with --bundle (or a batch's with --bundles or --bundle-dir)")
	}
	for _, b := range bundles {
		role, folder, named := strings.Cut(b, "=")
		if !named {
			role, folder = "", b
		}
		abs, err := filepath.Abs(folder)
		if err != nil {
			return opts, err
		}
		if _, ok := opts.bundles[role]; ok {
			return opts, fmt.Errorf("--bundle names %s twice", map[bool]string{true: "the bundle", false: "the role " + role}[role == ""])
		}
		opts.bundles[role] = abs
	}
	if _, ok := opts.bundles[""]; ok && len(opts.bundles) > 1 {
		return opts, errors.New("with several bundles, name each one's role: --bundle role=folder")
	}
	for _, f := range files {
		name, path, ok := strings.Cut(f, "=")
		if !ok || name == "" || path == "" {
			return opts, fmt.Errorf("--file %s: give name=path", f)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return opts, err
		}
		opts.files[name] = abs
	}
	for _, d := range data {
		abs, err := filepath.Abs(d)
		if err != nil {
			return opts, err
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return opts, fmt.Errorf("--data %s is not a folder", d)
		}
		opts.data = append(opts.data, abs)
	}
	if opts.out == "" && !opts.dryRun {
		return opts, errors.New("name a new folder for what it makes with --out")
	}
	if opts.out != "" {
		if opts.out, err = filepath.Abs(opts.out); err != nil {
			return opts, err
		}
	}
	if opts.cache != "" {
		if opts.cache, err = filepath.Abs(opts.cache); err != nil {
			return opts, err
		}
		opts.keepCache = true
	}
	if clock != "" {
		if opts.clock, err = time.Parse(time.RFC3339, clock); err != nil {
			return opts, fmt.Errorf("--clock %s is not a time such as 2026-01-01T00:00:00Z", clock)
		}
	}
	if opts.threads < 0 {
		return opts, errors.New("--threads is 0 (every core) or more")
	}
	if opts.timeout <= 0 {
		return opts, errors.New("--timeout must be positive")
	}
	return opts, nil
}

// runReport is run.json: what was made from what, and how it ended.
type runReport struct {
	Format   string `json:"format"`
	Version  int    `json:"version"`
	Software struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Commit  string `json:"commit,omitempty"`
	} `json:"software"`
	Recipe struct {
		Name   string `json:"name"`
		SHA256 string `json:"sha256"`
		By     string `json:"madeBy"`
	} `json:"recipe"`
	Clock   string          `json:"clock,omitempty"`
	Threads int             `json:"threads"`
	Bundles []runBundle     `json:"bundles"`
	Files   []recipeFile    `json:"files,omitempty"`
	Steps   []runStep       `json:"steps"`
	Result  *runStep        `json:"result,omitempty"`
	Cert    *runCertificate `json:"certificate,omitempty"`
	Page    *runPageRecord  `json:"page,omitempty"`
	Exit    struct {
		Code   int    `json:"code"`
		Reason string `json:"reason"`
	} `json:"exit"`
	Seconds float64 `json:"seconds"`
}

// runBundle is a bundle a run read: its role, whether it is the bundle the recipe was made from,
// and its files by SHA-256 (those the recipe names).
type runBundle struct {
	Role     string      `json:"role"`
	Analysis string      `json:"analysis,omitempty"`
	Same     bool        `json:"sameAsRecipe"`
	Differ   []string    `json:"differ,omitempty"`
	Files    []certNamed `json:"files"`
	folder   string
}

type runStep struct {
	N     int    `json:"n"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Job   string `json:"job"`
	State string `json:"state"`
	ID    string `json:"id,omitempty"`
	Made  string `json:"recipeMade"`
	Same  bool   `json:"sameAsRecipe"`
	// Rechosen: the cells the window chose were chosen again on this bundle (recipe_choices.go).
	Rechosen bool       `json:"rechosen,omitempty"`
	Files    []certFile `json:"files,omitempty"`
	Content  string     `json:"content,omitempty"`
	Error    string     `json:"error,omitempty"`
	Seconds  float64    `json:"seconds"`
}

func runRun(args []string, stdout, stderr io.Writer) int {
	opts, err := parseRunArgs(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return exitRunMade
	}
	if err != nil {
		fmt.Fprintln(stderr, "situcraft run:", err)
		return exitRunNotRun
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	// Stopped (Ctrl-C, or its scheduler's SIGTERM): the step under way ends, run.json says so, and
	// the browser ends with it.
	defer cancelOnSignal(cancel, stderr)()
	if len(opts.batch) > 0 {
		return makeBatch(ctx, opts, stdout, stderr)
	}
	return makeRecipe(ctx, opts, stdout, stderr)
}

// cancelOnSignal cancels the run on an interrupt or SIGTERM; the function returned stops listening.
// After MaveScape 0.8.0's run.go.
func cancelOnSignal(cancel context.CancelFunc, stderr io.Writer) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-signals:
			fmt.Fprintln(stderr, "situcraft run: stopped")
			cancel()
		case <-done:
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}

// batchBundles are a batch's bundles: the folders a file lists (one a line; blank lines and those
// starting with # passed over), or every folder of a folder holding experiment.xenium, by name.
func batchBundles(list, folder string) ([]string, error) {
	var out []string
	if list != "" {
		raw, err := os.ReadFile(list)
		if err != nil {
			return nil, err
		}
		base := filepath.Dir(list)
		for n, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if !filepath.IsAbs(line) {
				line = filepath.Join(base, line)
			}
			if _, err := os.Stat(filepath.Join(line, "experiment.xenium")); err != nil {
				return nil, fmt.Errorf("%s, line %d: %s is not a Xenium output bundle", filepath.Base(list), n+1, line)
			}
			out = append(out, line)
		}
	} else {
		abs, err := filepath.Abs(folder)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			path := filepath.Join(abs, e.Name())
			if _, err := os.Stat(filepath.Join(path, "experiment.xenium")); err == nil {
				out = append(out, path)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the batch has no bundle")
	}
	return out, nil
}

// batchEntry is a bundle of a batch, as batch.json says how it went.
type batchEntry struct {
	Bundle      string `json:"bundle"`
	Folder      string `json:"folder"`
	Exit        int    `json:"exit"`
	Reason      string `json:"reason"`
	Result      string `json:"result,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// makeBatch makes the recipe on each bundle of a batch (or, with --index, on one), each in a folder
// of its own under --out named by the bundle's; a whole batch also writes batch.json. Its exit
// status: 0 every bundle made; 2 none could run; 1 otherwise.
func makeBatch(ctx context.Context, opts runOptions, out, stderr io.Writer) int {
	taken := map[string]bool{}
	names := make([]string, len(opts.batch))
	for i, folder := range opts.batch {
		names[i] = roleOf(folder, taken)
	}
	chosen := make([]int, 0, len(opts.batch))
	for i := range opts.batch {
		if opts.index == 0 || opts.index == i+1 {
			chosen = append(chosen, i)
		}
	}
	var entries []batchEntry
	made, notRun := 0, 0
	for k, i := range chosen {
		if ctx.Err() != nil {
			break
		}
		one := opts
		one.batch, one.index = nil, 0
		one.bundles = map[string]string{"": opts.batch[i]}
		one.out = filepath.Join(opts.out, names[i])
		fmt.Fprintf(out, "== %s (%d of %d)\n", names[i], k+1, len(chosen))
		code := makeRecipe(ctx, one, out, stderr)
		entry := batchEntry{Bundle: names[i], Folder: names[i], Exit: code}
		if raw, err := os.ReadFile(filepath.Join(one.out, "run.json")); err == nil {
			var r runReport
			if json.Unmarshal(raw, &r) == nil {
				entry.Reason = r.Exit.Reason
				if r.Result != nil {
					entry.Result = r.Result.ID
				}
				if r.Cert != nil {
					entry.Fingerprint = r.Cert.Fingerprint
				}
			}
		}
		switch code {
		case exitRunMade:
			made++
		case exitRunNotRun:
			notRun++
			if entry.Reason == "" {
				entry.Reason = "not run (said above)"
			}
		}
		entries = append(entries, entry)
	}
	if opts.index == 0 {
		raw, _ := os.ReadFile(opts.recipe)
		summary := map[string]any{"format": "situcraft-batch", "version": 1, "recipe": map[string]string{"sha256": sha256Hex(raw)}, "made": made, "bundles": entries}
		text, _ := json.MarshalIndent(summary, "", " ")
		os.MkdirAll(opts.out, 0o755)
		if err := os.WriteFile(filepath.Join(opts.out, "batch.json"), append(text, '\n'), 0o644); err != nil {
			fmt.Fprintln(stderr, "situcraft run: batch.json:", err)
		}
		fmt.Fprintf(out, "The batch: %d of %s made.\n", made, plural(len(chosen), "bundle"))
	}
	switch {
	case made == len(chosen):
		return exitRunMade
	case notRun == len(chosen):
		return exitRunNotRun
	}
	return exitRunFailed
}

// makeRecipe does the work, saying what it does as it goes, and answers the exit status.
func makeRecipe(ctx context.Context, opts runOptions, out, stderr io.Writer) int {
	began := time.Now()
	fail := func(err error) int {
		fmt.Fprintln(stderr, "situcraft run:", err)
		return exitRunNotRun
	}
	raw, err := os.ReadFile(opts.recipe)
	if err != nil {
		return fail(err)
	}
	recipe, err := readResultRecipe(raw)
	if err != nil {
		return fail(fmt.Errorf("%s: %v", filepath.Base(opts.recipe), err))
	}
	report := &runReport{Format: runFormat, Version: runVersion, Threads: opts.threads}
	report.Software.Name, report.Software.Version, report.Software.Commit = "SituCraft", version, buildCommit()
	report.Recipe.Name, report.Recipe.SHA256, report.Recipe.By = recipe.Name, sha256Hex(raw), recipe.Software.Name+" "+recipe.Software.Version
	if !opts.clock.IsZero() {
		report.Clock = opts.clock.UTC().Format(time.RFC3339)
	}
	fmt.Fprintf(out, "SituCraft %s: making the recipe of %s (%s from %s)\n", version, recipe.Name, plural(len(recipe.Steps), "step"), plural(len(recipe.Bundles), "bundle"))

	// The bundles by their roles, each known again (or not) by its files.
	folders := map[string]string{}
	for _, b := range recipe.Bundles {
		folder, ok := opts.bundles[b.Role]
		if !ok && len(recipe.Bundles) == 1 {
			folder, ok = opts.bundles[""]
		}
		if !ok {
			return fail(fmt.Errorf("the recipe needs the bundle %s: give --bundle %s=<its folder>", b.Role, b.Role))
		}
		if _, err := os.Stat(filepath.Join(folder, "experiment.xenium")); err != nil {
			return fail(fmt.Errorf("%s is not a Xenium output bundle (it has no experiment.xenium)", folder))
		}
		folders[b.Role] = folder
		rb := runBundle{Role: b.Role, folder: folder, Same: true}
		if bundle, err := xenium.Open(folder, xenium.Options{}); err == nil {
			if bundle.Experiment != nil {
				rb.Analysis = bundle.Experiment.AnalysisUUID
			}
			bundle.Close()
		}
		for _, f := range b.Files {
			sum, err := fileSHA256(filepath.Join(folder, filepath.FromSlash(f.Path)))
			if err != nil || sum != f.SHA256 {
				rb.Same = false
				rb.Differ = append(rb.Differ, f.Path)
			}
			if err == nil {
				rb.Files = append(rb.Files, certNamed{Path: f.Path, SHA256: sum})
			}
			if !rb.Same && f.Path == "experiment.xenium" {
				break // another bundle: its other files need not be read
			}
		}
		if len(b.Files) == 0 {
			rb.Same = false
		}
		report.Bundles = append(report.Bundles, rb)
		if rb.Same {
			fmt.Fprintf(out, "Bundle %s: %s, the bundle the recipe was made from (%s as recorded).\n", b.Role, folder, plural(len(rb.Files), "file"))
		} else {
			fmt.Fprintf(out, "Bundle %s: %s, another bundle than the recipe was made from.\n", b.Role, folder)
		}
	}
	for role := range opts.bundles {
		if role != "" && folders[role] == "" {
			return fail(fmt.Errorf("the recipe names no bundle %s (its bundles: %s)", role, rolesOf(recipe)))
		}
	}
	same := map[string]bool{}
	for _, b := range report.Bundles {
		same[b.Role] = b.Same
	}

	// The files, given or found by their name and SHA-256.
	files := map[string]string{}
	for _, f := range recipe.Files {
		path := opts.files[f.Name]
		if path == "" {
			path = findFile(opts.data, f.Name, f.SHA256)
		}
		if path == "" {
			return fail(fmt.Errorf("the recipe reads %s: give it with --file %s=<path>, or its folder with --data", f.Name, f.Name))
		}
		sum, err := fileSHA256(path)
		if err != nil {
			return fail(err)
		}
		if f.SHA256 != "" && sum != f.SHA256 {
			return fail(fmt.Errorf("%s is not the %s the recipe read (its SHA-256 differs)", path, f.Name))
		}
		files[f.Name] = path
		report.Files = append(report.Files, recipeFile{Name: f.Name, SHA256: sum})
	}

	// A step carrying cells the window chose is made on another bundle only when the program
	// chooses them again as the window did; its cells are chosen there before it is made.
	var refused []string
	rechoose := map[int]bool{}
	for _, s := range recipe.Steps {
		if !s.Carries {
			continue
		}
		for _, role := range stepRoles(s.Spec) {
			if same[role] {
				continue
			}
			if !s.Portable {
				refused = append(refused, fmt.Sprintf("step %d, %s", s.N, s.Name))
			}
			rechoose[s.N] = true
			break
		}
	}
	if len(refused) > 0 {
		return fail(fmt.Errorf("%s carr%s cells the window chose on the bundle the recipe was made from (a region's, a review's, curated labels', niches'), and %s made only on that bundle: %s", plural(len(refused), "step"), map[bool]string{true: "ies", false: "y"}[len(refused) == 1], map[bool]string{true: "is", false: "are"}[len(refused) == 1], strings.Join(refused, "; ")))
	}
	if opts.dryRun {
		for _, s := range recipe.Steps {
			fmt.Fprintf(out, "  %d. %s (a %s job)%s\n", s.N, s.Name, s.Job, inputsText(s.Inputs))
		}
		fmt.Fprintln(out, "The recipe can be made (--dry-run: nothing made).")
		return exitRunMade
	}

	// The output folder: new, or empty.
	if entries, err := os.ReadDir(opts.out); err == nil && len(entries) > 0 {
		return fail(fmt.Errorf("%s is not empty: name a new folder with --out", opts.out))
	}
	if err := os.MkdirAll(opts.out, 0o755); err != nil {
		return fail(err)
	}
	cacheDir := opts.cache
	if cacheDir == "" {
		if cacheDir, err = os.MkdirTemp("", "situcraft-run-cache-"); err != nil {
			return fail(err)
		}
		defer os.RemoveAll(cacheDir)
	}
	if opts.threads > 0 {
		previous := runtime.GOMAXPROCS(opts.threads)
		defer runtime.GOMAXPROCS(previous)
	}
	examples, err := os.MkdirTemp("", "situcraft-run-examples-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(examples)
	// A SituCraft of its own, serving this computer alone (its page, for --report), taking no
	// script's actions.
	running, err := start(config{host: "127.0.0.1", window: "none", remote: true, mcp: true, dataDir: filepath.Join(opts.out, "library"), cacheDir: cacheDir, examplesDir: examples}, io.Discard)
	if err != nil {
		return fail(err)
	}
	go running.serve()
	defer running.stop(5 * time.Second)
	a := running.app
	if !opts.clock.IsZero() {
		at := opts.clock.UTC()
		a.artifacts.Clock = func() time.Time { return at }
		a.jobs.IDClock = func() time.Time { return at }
	}

	finish := func(code int, reason string) int {
		report.Exit.Code, report.Exit.Reason = code, reason
		report.Seconds = time.Since(began).Seconds()
		text, _ := json.MarshalIndent(report, "", " ")
		if err := os.WriteFile(filepath.Join(opts.out, "run.json"), append(text, '\n'), 0o644); err != nil {
			fmt.Fprintln(stderr, "situcraft run: run.json:", err)
		}
		switch code {
		case exitRunMade:
			fmt.Fprintf(out, "Made: %s (%.0f s). %s\n", reason, report.Seconds, filepath.Join(opts.out, "run.json"))
		default:
			fmt.Fprintf(out, "Not made: %s (%.0f s). %s\n", reason, report.Seconds, filepath.Join(opts.out, "run.json"))
		}
		return code
	}

	// The bundles' derived data, built as opening them builds it.
	for _, b := range report.Bundles {
		fmt.Fprintf(out, "Building the derived data of %s…\n", b.Role)
		job, _, err := a.jobs.Submit("cache", "Derived data of "+b.Role, cacheJobKey(b.folder), map[string]string{"bundle": b.folder}, a.cacheJob(b.folder))
		if err == nil {
			job, err = a.jobs.Wait(ctx, job.ID)
		}
		if err != nil || job.State != jobs.Complete {
			why := job.Error
			if err != nil {
				why = err.Error()
			}
			return finish(exitRunFailed, fmt.Sprintf("the derived data of %s could not be built: %s", b.Role, why))
		}
	}

	// Each step, in order.
	made := map[int]string{}
	for _, s := range recipe.Steps {
		step := runStep{N: s.N, Kind: s.Kind, Name: s.Name, Job: s.Job, Made: s.Made, State: "not made"}
		spec, missing := localizeRecipe(s.Spec, folders, files, made)
		if len(missing) > 0 {
			step.Error = "not found: " + strings.Join(missing, ", ")
			report.Steps = append(report.Steps, step)
			return finish(exitRunNotRun, fmt.Sprintf("step %d, %s: %s", s.N, s.Name, step.Error))
		}
		if s.Job == "refinement" {
			spec = withThreads(spec, opts.threads)
		}
		if rechoose[s.N] {
			c := choiceContext{a: a, ctx: ctx, bundle: folders[stepRolesFirst(s.Spec)], made: made}
			chosen, err := c.remakeChoices(spec, s.Choices)
			if err != nil {
				step.State, step.Error = "failed", "its cells could not be chosen again: "+err.Error()
				report.Steps = append(report.Steps, step)
				return finish(exitRunFailed, fmt.Sprintf("step %d, %s: %s", s.N, s.Name, step.Error))
			}
			spec, step.Rechosen = chosen, true
		}
		t0 := time.Now()
		fmt.Fprintf(out, "  %d. %s…", s.N, s.Name)
		fn, err := a.jobs.Remake(s.Job, spec)
		var job jobs.Job
		if err == nil {
			job, _, err = a.jobs.Submit(s.Job, s.Name, "run\x00"+fmt.Sprint(s.N), json.RawMessage(spec), fn)
		}
		if err == nil {
			job, err = a.jobs.Wait(ctx, job.ID)
		}
		step.Seconds = time.Since(t0).Seconds()
		switch {
		case ctx.Err() != nil:
			step.State, step.Error = "failed", "stopped before it finished"
		case err != nil:
			step.State, step.Error = "failed", err.Error()
		case job.State != jobs.Complete:
			step.State, step.Error = "failed", fmt.Sprintf("its job ended %s: %s", job.State, job.Error)
		}
		if step.State == "failed" {
			fmt.Fprintf(out, " failed: %s\n", step.Error)
			report.Steps = append(report.Steps, step)
			return finish(exitRunFailed, fmt.Sprintf("step %d, %s, failed: %s", s.N, s.Name, step.Error))
		}
		m, found := madeBy(a, job.ID, s.Kind)
		if !found {
			step.State, step.Error = "failed", "its job made no "+s.Kind
			fmt.Fprintf(out, " failed: %s\n", step.Error)
			report.Steps = append(report.Steps, step)
			return finish(exitRunFailed, fmt.Sprintf("step %d, %s: %s", s.N, s.Name, step.Error))
		}
		made[s.N] = m.ID
		step.State, step.ID, step.Same = "made", m.ID, m.ID == s.Made
		step.Files = madeFiles(a, m)
		step.Content, _ = contentDigest(m.Kind, filepath.Join(a.artifacts.Dir(), m.ID))
		report.Steps = append(report.Steps, step)
		fmt.Fprintf(out, " %s (%.1f s)%s\n", m.ID, step.Seconds, map[bool]string{true: ", as the recipe's", false: ""}[step.Same])
	}
	result := report.Steps[recipe.Result-1]
	report.Result = &result

	// The certificate of the result, which situcraft verify checks.
	cert, err := writeRunCertificate(a, recipe, report, filepath.Join(opts.out, "certificate.zip"))
	if err != nil {
		return finish(exitRunFailed, "the certificate could not be written: "+err.Error())
	}
	report.Cert = cert

	// The window's records and outputs, in a headless page: the run's project, its report and methods.
	if opts.report {
		fmt.Fprintln(out, "Writing the report in a headless page…")
		page, err := runPage(ctx, opts, running, recipe, report)
		report.Page = page
		if err != nil {
			return finish(exitRunFailed, "the report could not be written: "+err.Error())
		}
	}
	return finish(exitRunMade, fmt.Sprintf("%s, %s, certificate %s", result.Name, result.ID, shortFingerprint(cert.Fingerprint)))
}

func rolesOf(r *resultRecipe) string {
	var roles []string
	for _, b := range r.Bundles {
		roles = append(roles, b.Role)
	}
	return strings.Join(roles, ", ")
}

func inputsText(inputs []int) string {
	if len(inputs) == 0 {
		return ""
	}
	var parts []string
	for _, n := range inputs {
		parts = append(parts, fmt.Sprint(n))
	}
	return ", from step " + strings.Join(parts, ", ")
}

// stepRoles are the bundles a step's spec names, by role.
func stepRoles(spec json.RawMessage) []string {
	var roles []string
	rewriteStrings(spec, func(text string) (string, bool) {
		if m := recipeStandIn.FindStringSubmatch(text); m != nil && m[1] == "bundle" {
			role, _, _ := strings.Cut(m[2], "/")
			if !slicesContain(roles, role) {
				roles = append(roles, role)
			}
		}
		return "", false
	})
	return roles
}

// withThreads is a refinement's spec run on the threads given (0: every core); they never change
// what it makes.
func withThreads(spec []byte, threads int) []byte {
	var p refineSpec
	if json.Unmarshal(spec, &p) != nil {
		return spec
	}
	p.Threads = threads
	out, err := json.Marshal(p)
	if err != nil {
		return spec
	}
	return out
}

// madeBy is the artifact of a kind a job made (its lineage names the job).
func madeBy(a *app, jobID, kind string) (artifacts.Manifest, bool) {
	all, err := a.artifacts.List()
	if err != nil {
		return artifacts.Manifest{}, false
	}
	for _, m := range all {
		if m.Kind == kind && m.Lineage.Job != nil && m.Lineage.Job.ID == jobID && m.State == artifacts.Complete {
			return m, true
		}
	}
	return artifacts.Manifest{}, false
}

// madeFiles are the files an artifact's folder holds, by SHA-256 (not those it names in its
// bundle or where they lie).
func madeFiles(a *app, m artifacts.Manifest) []certFile {
	var out []certFile
	for _, f := range m.Files {
		if f.Location == artifacts.InBundle || f.Location == artifacts.InPlace {
			continue
		}
		path, err := a.artifacts.Path(m.ID, f.Path)
		if err != nil {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		sum, err := fileSHA256(path)
		if err != nil {
			continue
		}
		size := info.Size()
		out = append(out, certFile{Path: filepath.ToSlash(f.Path), Role: f.Role, Size: &size, SHA256: sum})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
