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
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: situcraft simulate [flags] <new output folder>")
		fmt.Fprintln(stderr, "Writes a simulated tissue as a Xenium-shaped output bundle, labelled simulated, with the truth in its truth/ folder.")
		flags.PrintDefaults()
	}
	folders, err := parseArgs(flags, args)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		return 2
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
	if err := p.Check(); err != nil {
		fmt.Fprintf(stderr, "situcraft simulate: %v\n", err)
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
	fmt.Fprintf(stdout, "Written to %s (the truth in its truth/ folder). Open it with: situcraft %s\n", sum.Folder, sum.Folder)
	return 0
}
