package main

// situcraft refine: the transcript-informed segmentation of a bundle in the terminal, from a
// preset or a recipe (requirements E5), written to a new folder as SituCraft's segmentation files
// with the recipe beside them. The same recipe on the same bundle gives the same files as the
// app's refinement, byte for byte; the folder imports into the app as a SituCraft segmentation.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
	"situcraft/internal/segment/refine"
	"situcraft/internal/sysinfo"
	"situcraft/internal/xenium"
)

// regionFlags collects --region x0,y0,x1,y1 (µm).
type regionFlags []refine.Region

func (r *regionFlags) String() string { return fmt.Sprint(*r) }

func (r *regionFlags) Set(v string) error {
	parts := strings.Split(v, ",")
	if len(parts) != 4 {
		return errors.New("a region is x0,y0,x1,y1 in µm")
	}
	var c [4]float64
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("%q is not a number", p)
		}
		c[i] = f
	}
	*r = append(*r, refine.Region{X0: c[0], Y0: c[1], X1: c[2], Y1: c[3]})
	return nil
}

// runRefine runs `situcraft refine` with its arguments, answering the exit code.
func runRefine(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("situcraft refine", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var ids []string
	for _, p := range refine.Presets() {
		ids = append(ids, p.ID)
	}
	recipeFile := flags.String("recipe", "", "a recipe (JSON) to run: its preset, settings, area and quality; the flags below change it")
	preset := flags.String("preset", "", "the settings' preset: "+strings.Join(ids, ", ")+" (default balanced)")
	seed := flags.Uint64("seed", 0, "the random seed (default: the preset's); the same settings and seed give the same segmentation, byte for byte")
	sweeps := flags.Int("sweeps", 0, "the sweeps of the last stage (default: the preset's)")
	samples := flags.Int("samples", 0, "the last sweeps summarized (default: the preset's)")
	minQV := flags.Float64("min-qv", -1, "the quality the modelled transcripts reach (default 20)")
	var regions regionFlags
	flags.Var(&regions, "region", "refine a region, x0,y0,x1,y1 in µm (repeat for more); the rest keeps the instrument's cells")
	sections := flags.Bool("sections", false, "refine the tissue's separate sections apart, each with a model of its own")
	seedCheck := flags.Bool("seed-check", false, "run again with the next seed and report the two runs' agreement")
	threads := flags.Int("threads", 0, "the threads to run on (default: every core); the result does not depend on them")
	force := flags.Bool("force", false, "run even when the estimate exceeds the computer's memory")
	preflight := flags.Bool("preflight", false, "print what the refinement would model, cover and cost, and exit")
	printRecipe := flags.Bool("print-recipe", false, "print the recipe and exit")
	quiet := flags.Bool("quiet", false, "print only the summary")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: situcraft refine [flags] <Xenium output bundle> <new output folder>")
		fmt.Fprintln(stderr, "Refines the instrument's segmentation of a bundle with SituCraft's transcript-informed segmentation, writing")
		fmt.Fprintln(stderr, "SituCraft's segmentation files and the recipe (recipe.json) to the new folder.")
		flags.PrintDefaults()
	}
	positional, err := parseArgs(flags, args)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return 2
	}
	set := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { set[f.Name] = true })
	fail := func(code int, format string, args ...any) int {
		fmt.Fprintf(stderr, "situcraft refine: "+format+"\n", args...)
		return code
	}

	// The spec: the recipe's or the preset's, then the flags.
	spec := refine.DefaultSpec()
	var made *recipe
	if *recipeFile != "" {
		raw, err := os.ReadFile(*recipeFile)
		if err != nil {
			return fail(2, "%v", err)
		}
		r, err := readRecipe(raw)
		if err != nil {
			return fail(2, "%s: %v", *recipeFile, err)
		}
		made, spec = &r, r.Refinement
	}
	if set["preset"] {
		// A preset's settings; the seed, the area and the quality stay as they were.
		from := spec
		if spec, err = refine.SpecOf(*preset); err != nil {
			return fail(2, "%v (%s)", err, strings.Join(ids, ", "))
		}
		spec.Options.Seed, spec.MinQV = from.Options.Seed, from.MinQV
		if spec.Area.Mode != refine.AreaRegions || from.Area.Mode == refine.AreaRegions {
			spec.Area = from.Area
		}
	}
	if set["seed"] {
		spec.Options.Seed = *seed
	}
	if set["sweeps"] {
		spec.Options.Stages[len(spec.Options.Stages)-1].Sweeps = *sweeps
		if !set["samples"] {
			spec.Options.Samples = min(spec.Options.Samples, *sweeps*5/6)
		}
	}
	if set["samples"] {
		spec.Options.Samples = *samples
	}
	if set["min-qv"] {
		spec.MinQV = *minQV
	}
	if len(regions) > 0 && *sections {
		return fail(2, "refine regions or sections, not both")
	}
	if len(regions) > 0 {
		spec.Area = refine.Area{Mode: refine.AreaRegions, Regions: regions}
	}
	if *sections {
		spec.Area = refine.Area{Mode: refine.AreaSections}
	}
	if set["seed-check"] {
		spec.SeedCheck = *seedCheck
	}
	spec.Resolve()
	if err := spec.Check(); err != nil {
		return fail(2, "%s", strings.TrimPrefix(err.Error(), "refine: "))
	}

	// The bundle: the one named, else the recipe's.
	want := 2
	if *preflight || *printRecipe {
		want = 1
	}
	if made != nil && len(positional) == want-1 && made.Dataset.Path != "" {
		positional = append([]string{made.Dataset.Path}, positional...)
	}
	if len(positional) != want {
		flags.Usage()
		return 2
	}
	b, err := xenium.Open(positional[0], xenium.Options{})
	if err != nil {
		return fail(1, "%v", err)
	}
	defer b.Close()
	if made != nil && !made.Dataset.sameDataset(b) && !*quiet {
		fmt.Fprintf(stderr, "Note: the recipe was made for %s; refining %s.\n", made.Dataset.Name, b.Name)
	}
	if *printRecipe {
		raw, _ := json.MarshalIndent(newRecipe(b, spec), "", "  ")
		fmt.Fprintln(stdout, string(raw))
		return 0
	}
	cores := *threads
	if cores <= 0 {
		cores = runtime.NumCPU()
	}
	if *preflight {
		in, idx, err := refine.BundleInputs(b, spec.MinQV)
		if err != nil {
			return fail(1, "%v", err)
		}
		memory, _ := sysinfo.Memory()
		dir, _ := os.Getwd()
		free, err := xenium.FreeBytes(dir)
		if err != nil {
			free = -1
		}
		printPreflight(stdout, refine.Check(in, idx, spec, cores, memory, free))
		return 0
	}

	// The run, into a hidden folder beside the output, renamed into place when it is complete.
	out, err := filepath.Abs(positional[1])
	if err != nil {
		return fail(2, "%v", err)
	}
	if _, err := os.Stat(out); err == nil {
		return fail(2, "%s exists: name a new folder", out)
	}
	work := filepath.Join(filepath.Dir(out), "."+filepath.Base(out)+".partial")
	os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return fail(1, "%v", err)
	}
	defer os.RemoveAll(work)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	last := ""
	job := refine.Job{Workers: cores, Force: *force,
		Progress: func(at float64, what string) {
			if !*quiet && what != last {
				fmt.Fprintf(stderr, "%3.0f%%  %s\n", 100*at, what)
				last = what
			}
		},
		Log: func(level, format string, args ...any) {
			if !*quiet || level == "warning" {
				fmt.Fprintf(stderr, "%s: %s\n", level, fmt.Sprintf(format, args...))
			}
		}}
	d, diag, err := refine.Refine(ctx, b, spec, work, job)
	if err != nil {
		if f := refine.Classify(err); f != nil {
			return fail(1, "%v\n%s", err, f.Remedy)
		}
		return fail(1, "%v", err)
	}
	m := artifacts.Manifest{}
	fatal := 0
	for _, f := range segment.Check(work, &m) {
		if f.Severity == "fatal" {
			fatal++
		}
		if f.Severity != "info" || !*quiet {
			fmt.Fprintf(stderr, "%s %s: %s\n", f.Severity, f.Code, f.Message)
		}
	}
	if fatal > 0 {
		return fail(1, "the segmentation check found %d fatal problems; nothing was written", fatal)
	}
	raw, _ := json.MarshalIndent(newRecipe(b, spec), "", "  ")
	if err := os.WriteFile(filepath.Join(work, "recipe.json"), append(raw, '\n'), 0o644); err != nil {
		return fail(1, "%v", err)
	}
	if err := os.Rename(work, out); err != nil {
		return fail(1, "%v", err)
	}
	s := diag.Summary
	fmt.Fprintf(stdout, "Refined %d of %d cells (%d held about %d region(s)) of %s with the %s preset, seed %d.\n", s.Refined, d.Cells, s.Held, s.Regions, b.Name, presetName(spec.Preset), spec.Options.Seed)
	verdict := "settled"
	if math.Abs(s.GewekeZ) > 2 {
		verdict = "still drifting: more sweeps would make sure"
	}
	fmt.Fprintf(stdout, "%d transcripts modelled; Geweke's z %.1f (%s); %.1f%% the same cell over both halves of the last %d sweeps.\n", s.Transcripts, s.GewekeZ, verdict, 100*s.SplitHalf, s.Samples)
	if s.SeedAgreement != nil {
		fmt.Fprintf(stdout, "A second seed put %.1f%% of the transcripts in refined cells in the same cell.\n", 100*(*s.SeedAgreement))
	}
	fmt.Fprintf(stdout, "Written to %s with its recipe. Import it into SituCraft as a SituCraft segmentation of %s.\n", out, b.Name)
	return 0
}

func presetName(id string) string {
	if p, ok := refine.PresetByID(id); ok {
		return p.Name
	}
	return "custom"
}

// printPreflight prints a preflight for people.
func printPreflight(w io.Writer, p refine.Preflight) {
	fmt.Fprintf(w, "Preset: %s.\n", presetName(p.Spec.Preset))
	fmt.Fprintf(w, "Transcripts: %d rows; %d gene transcripts of quality %g or more modelled (%d over a nucleus), %d below it, %d of other features, %d beyond the image.\n",
		p.Rows, p.Transcripts, p.Spec.MinQV, p.OverNuclei, p.LowQuality, p.Controls, p.Outside)
	fmt.Fprintf(w, "Cells to start from: %d (%d with a nucleus); %d genes.\n", p.Cells, p.Nuclei, p.Genes)
	for i, r := range p.Regions {
		fmt.Fprintf(w, "Region %d: (%.0f, %.0f)–(%.0f, %.0f) µm.\n", i+1, r.X0, r.Y0, r.X1, r.Y1)
	}
	e := p.Estimate
	if e.Regions > 0 {
		fmt.Fprintf(w, "Refines %d cells (%d held about them), %d transcripts in %d voxels.\n", e.Cells, e.Held, e.Transcripts, e.Voxels)
		mem := "unknown"
		if p.MemoryBytes > 0 {
			mem = refine.Memory(p.MemoryBytes)
		}
		disk := "unknown"
		if p.FreeBytes >= 0 {
			disk = refine.Bytes(p.FreeBytes)
		}
		fmt.Fprintf(w, "Needs about %s of memory (the computer has %s), %s of disk (%s free) and %s on %d threads.\n",
			refine.Memory(e.PeakBytes), mem, refine.Bytes(e.DiskBytes), disk, refine.Duration(e.Seconds), e.Cores)
	}
	for _, f := range p.Problems {
		fmt.Fprintf(w, "Problem: %s %s\n", f.Message, f.Remedy)
	}
	for _, f := range p.Risks {
		fmt.Fprintf(w, "Risk: %s\n", f.Message)
	}
}
