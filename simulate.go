package main

// situcraft simulate: a simulated tissue, written as a Xenium-shaped output bundle labelled
// simulated, with its truth (internal/simulate). It runs in the terminal and exits; nothing is
// served.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"situcraft/internal/simulate"
)

// runSimulate runs `situcraft simulate` with its arguments, answering the exit code.
func runSimulate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("situcraft simulate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var names []string
	for name := range simulate.Presets {
		names = append(names, name)
	}
	sort.Strings(names)
	preset := flags.String("preset", "standard", "the starting parameters: "+strings.Join(names[:len(names)-1], ", ")+" or "+names[len(names)-1]+" (large: a mosaic of 55 million transcripts, for measuring at scale)")
	seed := flags.Uint64("seed", 0, "the random seed (default: the preset's); the same parameters and seed give the same bundle, byte for byte")
	width := flags.Float64("width", 0, "the field's width in µm (default: the preset's)")
	height := flags.Float64("height", 0, "the field's height in µm (default: the preset's)")
	paramsFile := flags.String("params", "", "a JSON file of parameters, each overriding the preset's (as --print-params writes them)")
	printParams := flags.Bool("print-params", false, "print the parameters as JSON and exit")
	quiet := flags.Bool("quiet", false, "print only the summary")
	postXenium := flags.Bool("post-xenium", false, "also make post-Xenium images of the section, an H&E and an immunofluorescence image (DAPI and a membrane stain), each placed by a known transform and deformation, in its post_xenium/ folder; where each lies is in truth/post_xenium/")
	reference := flags.Int("reference", 0, "also write a single-cell reference of the tissue's types, this many cells of each, as an AnnData object in Zarr beside the bundle (<folder>-reference.zarr), to label its cells from")
	var cohortNames []string
	for name := range simulate.CohortPresets {
		cohortNames = append(cohortNames, name)
	}
	sort.Strings(cohortNames)
	cohort := flags.String("cohort", "", "a simulated cohort instead of one tissue: "+strings.Join(cohortNames, ", ")+" (sections of subjects in groups, each a bundle, with known effects between the groups; --params then reads the cohort's parameters)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: situcraft simulate [flags] <new output folder>")
		fmt.Fprintln(stderr, "Writes a simulated tissue as a Xenium-shaped output bundle, labeled simulated, with the truth in its truth/ folder;")
		fmt.Fprintln(stderr, "with --cohort, a simulated cohort: a bundle for each sample, samples.csv and truth/cohort.json.")
		flags.PrintDefaults()
	}
	folders, err := parseArgs(flags, args)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return 2
	}
	if *cohort != "" {
		return runCohort(*cohort, cohortNames, *paramsFile, *seed, *width, *height, *postXenium || *reference > 0, *printParams, *quiet, folders, stdout, stderr, flags.Usage)
	}
	p, ok := simulate.Presets[*preset]
	if !ok {
		fmt.Fprintf(stderr, "situcraft simulate: no preset %q (%s)\n", *preset, strings.Join(names, ", "))
		return 2
	}
	if *paramsFile != "" {
		raw, err := os.ReadFile(*paramsFile)
		if err != nil {
			fmt.Fprintf(stderr, "situcraft simulate: %v\n", err)
			return 2
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&p); err != nil {
			fmt.Fprintf(stderr, "situcraft simulate: %s: %v\n", *paramsFile, err)
			return 2
		}
	}
	if *seed != 0 {
		p.Seed = *seed
	}
	if *width != 0 {
		p.WidthUm = *width
	}
	if *height != 0 {
		p.HeightUm = *height
	}
	if *postXenium && p.PostXenium == nil {
		p.PostXenium = simulate.DefaultPostXenium()
	}
	if err := p.Check(); err != nil {
		fmt.Fprintf(stderr, "situcraft simulate: %v\n", err)
		return 2
	}
	if *reference < 0 || *reference > 100000 {
		fmt.Fprintln(stderr, "situcraft simulate: --reference is a number of cells per type from 0 to 100000")
		return 2
	}
	if *printParams {
		raw, _ := json.MarshalIndent(p, "", "  ")
		fmt.Fprintln(stdout, string(raw))
		return 0
	}
	if len(folders) != 1 {
		flags.Usage()
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	last := ""
	progress := func(at float64, what string) {
		if !*quiet && what != last {
			fmt.Fprintf(stderr, "%3.0f%%  %s\n", 100*at, what)
			last = what
		}
	}
	sum, err := simulate.Run(ctx, p, folders[0], version, progress)
	if err != nil {
		fmt.Fprintf(stderr, "situcraft simulate: %v\n", err)
		return 1
	}
	var types []string
	for _, ty := range p.Types {
		if n := sum.CellsByType[ty.Name]; n > 0 {
			types = append(types, fmt.Sprintf("%d %s", n, ty.Name))
		}
	}
	area := fmt.Sprintf("%g × %g µm", p.WidthUm, p.HeightUm)
	if p.TilesAcross > 1 || p.TilesDown > 1 {
		area = fmt.Sprintf("%g × %g µm (a mosaic of %d × %d tiles of %g × %g µm)", float64(sum.ImageWidth)*p.PixelUm, float64(sum.ImageHeight)*p.PixelUm,
			max(1, p.TilesAcross), max(1, p.TilesDown), p.WidthUm, p.HeightUm)
	}
	fmt.Fprintf(stdout, "Simulated %d cells (%s) in %s, seed %d.\n", sum.Cells, strings.Join(types, ", "), area, p.Seed)
	fmt.Fprintf(stdout, "%d nuclei are in the section; the simulated instrument segmented %d cells by nucleus expansion.\n", sum.VisibleNuclei, sum.InstrumentCells)
	fmt.Fprintf(stdout, "%d transcripts: %d from cells, %d background, %d negative controls.\n", sum.Transcripts, sum.MoleculesByOrigin["cell"], sum.MoleculesByOrigin["background"],
		sum.MoleculesByOrigin["control probe"]+sum.MoleculesByOrigin["control codeword"])
	if len(sum.PostXeniumImages) > 0 {
		fmt.Fprintf(stdout, "Post-Xenium images: %s (where each lies: %s).\n", strings.Join(sum.PostXeniumImages, ", "), simulate.RegistrationFile)
	}
	fmt.Fprintf(stdout, "Written to %s (the truth in its truth/ folder). Open it with: situcraft %s\n", sum.Folder, sum.Folder)
	if *reference > 0 {
		path := strings.TrimRight(sum.Folder, "/") + "-reference.zarr"
		if err := simulate.WriteReference(p, path, *reference, simulate.ReferenceDepth, p.Seed); err != nil {
			fmt.Fprintf(stderr, "situcraft simulate: the reference: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "A single-cell reference of its types (%d cells of each) is in %s.\n", *reference, path)
	}
	return 0
}

// runCohort runs `situcraft simulate --cohort`: a preset, its parameters read from a file, its seed
// and sections' size replaced if asked; printed, or made.
func runCohort(name string, names []string, paramsFile string, seed uint64, width, height float64, extras, printParams, quiet bool, folders []string, stdout, stderr io.Writer, usage func()) int {
	c, ok := simulate.CohortPresets[name]
	if !ok {
		fmt.Fprintf(stderr, "situcraft simulate: no cohort %q (%s)\n", name, strings.Join(names, ", "))
		return 2
	}
	if extras {
		fmt.Fprintln(stderr, "situcraft simulate: a cohort's sections have no post-Xenium images or reference")
		return 2
	}
	if paramsFile != "" {
		raw, err := os.ReadFile(paramsFile)
		if err != nil {
			fmt.Fprintf(stderr, "situcraft simulate: %v\n", err)
			return 2
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			fmt.Fprintf(stderr, "situcraft simulate: %s: %v\n", paramsFile, err)
			return 2
		}
	}
	if seed != 0 {
		c.Seed = seed
	}
	if width != 0 {
		c.Section.WidthUm = width
	}
	if height != 0 {
		c.Section.HeightUm = height
	}
	if err := c.Check(); err != nil {
		fmt.Fprintf(stderr, "situcraft simulate: %v\n", err)
		return 2
	}
	if printParams {
		raw, _ := json.MarshalIndent(c, "", "  ")
		fmt.Fprintln(stdout, string(raw))
		return 0
	}
	if len(folders) != 1 {
		usage()
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	last := ""
	progress := func(at float64, what string) {
		if !quiet && what != last {
			fmt.Fprintf(stderr, "%3.0f%%  %s\n", 100*at, what)
			last = what
		}
	}
	sum, err := simulate.RunCohort(ctx, c, folders[0], version, progress)
	if err != nil {
		fmt.Fprintf(stderr, "situcraft simulate: %v\n", err)
		return 1
	}
	design := fmt.Sprintf("%d subjects in each of %s", c.Subjects, strings.Join(c.Groups, " and "))
	if c.Paired {
		design = fmt.Sprintf("%d subjects, each in %s", c.Subjects, strings.Join(c.Groups, " and "))
	}
	cells := 0
	for _, s := range sum.Truth.Samples {
		cells += s.Cells
	}
	fmt.Fprintf(stdout, "Simulated a cohort of %d sections (%s), %d true cells in all, seed %d.\n", len(sum.Samples), design, cells, c.Seed)
	if len(sum.Truth.Design.Slides) > 0 {
		fmt.Fprintf(stdout, "On %d slides, each holding samples of every group.\n", len(sum.Truth.Design.Slides))
	}
	for _, e := range sum.Truth.Effects {
		of := e.Type
		if e.Kind == simulate.EffectLevel {
			of = e.Gene + " in " + e.Type
		} else if e.Within != "" {
			of += " in the domains " + e.Within + " leads"
		}
		fmt.Fprintf(stdout, "Effect: %s of %s, ×%g in %s; the true cells show ×%.2f (±%.2f, a standard error on the log scale).\n", e.Kind, of, e.Fold, e.Group, math.Exp(e.Measured), e.StdError)
	}
	if len(sum.Truth.Effects) == 0 {
		fmt.Fprintln(stdout, "No effect: the groups differ only by chance.")
	}
	fmt.Fprintf(stdout, "Written to %s (each sample a bundle; samples.csv; the truth in truth/cohort.json). Open it with: situcraft %s\n", sum.Folder, sum.Folder)
	return 0
}
