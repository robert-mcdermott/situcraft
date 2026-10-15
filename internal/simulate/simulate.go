package simulate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"situcraft/internal/random"
	"situcraft/internal/tiff"
	"situcraft/internal/xenium"
)

// SimulationFile is the file at a simulated bundle's root that says it is simulated and how.
const SimulationFile = xenium.SimulationFile

// Summary is what a simulation made.
type Summary struct {
	Folder            string         `json:"folder"`
	Cells             int            `json:"cells"`
	CellsByType       map[string]int `json:"cells_by_type"`
	VisibleNuclei     int            `json:"visible_nuclei"`
	InstrumentCells   int            `json:"instrument_cells"`
	Transcripts       int            `json:"transcripts"`
	MoleculesByOrigin map[string]int `json:"molecules_by_origin"`
	WideDisplacements int            `json:"wide_displacements"`
	ImageWidth        int            `json:"image_width_px"`
	ImageHeight       int            `json:"image_height_px"`
}

// Run makes the tissue p describes and writes it, as a simulated Xenium-shaped bundle with its
// truth, into folder (which must not exist yet: it is written beside it and moved into place
// when complete). Software names SituCraft's version; progress hears how far it is (0 to 1).
func Run(ctx context.Context, p Params, folder, software string, progress func(float64, string)) (*Summary, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	if err := p.Check(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(folder); err == nil {
		return nil, fmt.Errorf("%s already exists: the simulation writes a new folder", folder)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	parent := filepath.Dir(folder)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	stop := func() bool { return ctx.Err() != nil }
	report := func(at float64, what string) error {
		if stop() {
			return errStopped
		}
		progress(at, what)
		return nil
	}

	t, pn, mols, in, err := simulateTissue(p, stop, report)
	if err != nil {
		return nil, err
	}

	staging, err := os.MkdirTemp(parent, "."+filepath.Base(folder)+".writing-")
	if err != nil {
		return nil, err
	}
	done := false
	defer func() {
		if !done {
			os.RemoveAll(staging)
		}
	}()
	wr := &writer{t: t, in: in, pn: pn, mols: mols, dir: staging, software: software}
	wr.count()
	err = wr.writeAll(func(at float64, what string) {
		progress(0.4+0.6*at, what)
	})
	if err != nil {
		return nil, err
	}
	if stop() {
		return nil, errStopped
	}
	if err := os.Chmod(staging, 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(staging, folder); err != nil {
		return nil, err
	}
	done = true
	progress(1, "Done")
	return wr.summary(folder), nil
}

// simulateTissue lays out the tissue, draws its molecules and segments it as the instrument does.
func simulateTissue(p Params, stop func() bool, report func(float64, string) error) (*tissue, *panel, []molecule, *instrument, error) {
	if err := report(0, "Laying out the tissue"); err != nil {
		return nil, nil, nil, nil, err
	}
	t := newTissue(p)
	layout := random.New(p.Seed, streamLayout)
	t.makeDomains(layout)
	t.placeCells(layout)
	if err := t.rasterize(stop); err != nil {
		return nil, nil, nil, nil, err
	}
	t.placeNuclei(layout)
	if len(t.cells) == 0 {
		return nil, nil, nil, nil, errors.New("the simulation made no cells: raise the fill or the cells' areas")
	}
	if err := report(0.15, "Drawing the molecules"); err != nil {
		return nil, nil, nil, nil, err
	}
	pn := makePanel(p, random.New(p.Seed, streamPanel), t.typeShares())
	mols, err := t.molecules(pn, stop)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := report(0.35, "Segmenting as the instrument does"); err != nil {
		return nil, nil, nil, nil, err
	}
	in := t.segmentLikeTheInstrument()
	if len(in.cells) == 0 {
		return nil, nil, nil, nil, errors.New("no nucleus of the simulated tissue is in the section: lower the hidden-nucleus share")
	}
	return t, pn, mols, in, nil
}

// typeShares are the types' shares of the cells' expected transcripts, which weigh the
// background's genes.
func (t *tissue) typeShares() []float64 {
	p := t.p
	share := make([]float64, len(p.Types))
	var total float64
	for k := range t.cells {
		c := &t.cells[k]
		e := mul(p.Types[c.typ].Density, float64(len(c.pixels)))
		share[c.typ] += e
		total += e
	}
	for k := range share {
		if total > 0 {
			share[k] /= total
		} else {
			share[k] = 1 / float64(len(share))
		}
	}
	return share
}

// molecules draws every cell's molecules and every background tile's, in parallel, each from its
// own stream, merged in order: the same for any number of processors.
func (t *tissue) molecules(pn *panel, stop func() bool) ([]molecule, error) {
	cells, tiles := len(t.cells), t.tiles()
	parts := make([][]molecule, cells+tiles)
	jobs := make(chan int, 256)
	var wg sync.WaitGroup
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if j < cells {
					parts[j] = t.cellMolecules(j, pn, nil)
				} else {
					parts[j] = t.background(j-cells, pn, nil)
				}
			}
		}()
	}
	stopped := false
	for j := range parts {
		if j%256 == 0 && stop() {
			stopped = true
			break
		}
		jobs <- j
	}
	close(jobs)
	wg.Wait()
	if stopped {
		return nil, errStopped
	}
	n := 0
	for _, part := range parts {
		n += len(part)
	}
	out := make([]molecule, 0, n)
	for _, part := range parts {
		out = append(out, part...)
	}
	return out, nil
}

// count records what the molecules are, for the summary.
func (wr *writer) count() {
	wr.w.moleculesByOrigin = map[string]int{}
	names := []string{"cell", "background", "control probe", "control codeword"}
	for _, m := range wr.mols {
		wr.w.moleculesByOrigin[names[m.origin]]++
		if m.wide {
			wr.w.wide++
		}
	}
}

func (wr *writer) summary(folder string) *Summary {
	return &Summary{
		Folder: folder, Cells: len(wr.t.cells), CellsByType: wr.w.trueCellsByType, VisibleNuclei: wr.w.visibleNuclei,
		InstrumentCells: wr.w.instrumentCells, Transcripts: wr.w.transcripts, MoleculesByOrigin: wr.w.moleculesByOrigin,
		WideDisplacements: wr.w.wide, ImageWidth: wr.t.w, ImageHeight: wr.t.h,
	}
}

// description is situcraft-simulation.json: that the bundle is simulated, by what, with what
// settings, where its truth is and what it holds.
func (wr *writer) description() any {
	s := wr.summary("")
	return map[string]any{
		"format":    "situcraft-simulation",
		"version":   1,
		"simulated": true,
		"software":  "SituCraft " + wr.software,
		"about": "Every cell, nucleus and molecule in this bundle is simulated. The files are laid out as a Xenium output bundle " +
			"so that any tool reading Xenium data can read them; no instrument made them. The instrument's segmentation is a " +
			"simulated nucleus expansion; the true segmentation, every molecule's true cell and place, and each cell's type are in truth/.",
		"truth":      "truth",
		"parameters": wr.t.p,
		"made": map[string]any{
			"cells": s.Cells, "cells_by_type": s.CellsByType, "visible_nuclei": s.VisibleNuclei,
			"instrument_cells": s.InstrumentCells, "transcripts": s.Transcripts, "molecules_by_origin": s.MoleculesByOrigin,
			"wide_displacements": s.WideDisplacements, "image_width_px": s.ImageWidth, "image_height_px": s.ImageHeight,
		},
	}
}

func tiffWrite(path string, plane *tiff.Plane16, description, software string, levels int) error {
	return tiff.WriteOMETIFF(path, plane, tiff.WriteOptions{Description: description, Software: software, Tile: 512, Levels: levels})
}
