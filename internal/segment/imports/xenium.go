package imports

// Xenium Ranger output bundles (import-segmentation, resegment), and Xenium Onboard Analysis
// bundles of the same run: a bundle like the dataset's, its cells segmented anew. It must be of the
// same run and region. Its cells are cells.parquet's; its raster is its cell mask
// (cells.zarr.zip masks/1, label k cells.parquet's row k−1); each transcript's cell is its
// transcripts.parquet's cell_id, matched to the dataset's transcripts by transcript_id; its
// outlines are cell_boundaries.parquet's; its own count of each cell's gene transcripts
// (cells.parquet's transcript_counts) is compared with the assignment's.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

func readXenium(ctx context.Context, ds Dataset, opt Options) (*source, error) {
	if len(opt.Paths) != 1 {
		return nil, errors.New("name one output bundle (its folder, its zip or its experiment.xenium)")
	}
	b, err := xenium.Open(opt.Paths[0], xenium.Options{})
	if err != nil {
		return nil, err
	}
	if mine, theirs := runOf(b), ds.Run(); !mine.Same(theirs) {
		b.Close()
		return nil, fmt.Errorf("%s is of %s; %s is of %s: an output bundle imports into the run it segments, of another run it does not", b.Name, mine, ds.Name(), theirs)
	}
	src, err := readBundle(ctx, b, false)
	if err != nil {
		b.Close()
		return nil, err
	}
	// Its mask is read as the segmentation is built: the bundle stays open until then.
	src.closers = append([]func() error{b.Close}, src.closers...)
	if opt.Tool != "" {
		src.toolName, src.toolVer = opt.Tool, opt.ToolVersion
	}
	src.citation = opt.Citation
	return src, nil
}

// Instrument writes a bundle's own segmentation, the instrument's, as SituCraft's segmentation
// files (so that it compares with any other on the same terms): each transcript's cell is its
// transcripts.parquet's, row by row.
func Instrument(ctx context.Context, b *xenium.Bundle, dir string, workers int) (*segment.Description, error) {
	ds, err := BundleDataset(b)
	if err != nil {
		return nil, err
	}
	src, err := readBundle(ctx, b, true)
	if err != nil {
		return nil, err
	}
	defer src.close()
	return build(ctx, ds, Options{Format: Xenium, Paths: []string{b.Path}, Units: "um"}, src, dir, workers, func(float64, string) {})
}

// readBundle reads a bundle's segmentation; own: its transcripts are the dataset's, row by row.
func readBundle(ctx context.Context, b *xenium.Bundle, own bool) (_ *source, err error) {
	src := newSource()
	defer func() {
		if err != nil {
			src.close()
		}
	}()
	e := b.Experiment
	software := e.AnalysisSoftware
	src.toolName = "Xenium Onboard Analysis"
	if strings.Contains(strings.ToLower(software), "ranger") {
		src.toolName = "Xenium Ranger"
	}
	src.toolVer = software
	src.software = append(src.software, artifacts.Software{Name: src.toolName, Version: software})
	src.read["bundle"] = b.Name
	src.read["analysis_uuid"] = e.AnalysisUUID

	cells, err := b.ReadCells()
	if err != nil {
		return nil, err
	}
	for _, id := range cells.IDs {
		src.cell(id)
	}
	// The raster: its cell mask, read a row of its chunks at a time as the segmentation is built
	// (a whole slide's is gigabytes).
	if b.Has("cells.zarr.zip") {
		m, err := b.OpenMask(xenium.CellMask)
		if err == nil {
			w, h := m.Size()
			ps := m.PixelSize
			src.labels = &labelImage{width: w, height: h, rowOf: m.Row, transform: affine{a: ps, e: ps}}
			src.closers = append(src.closers, m.Close)
			src.read["raster"] = "cells.zarr.zip masks/1"
		}
	}
	// The outlines (and the raster, when there is no mask).
	if b.Has("cell_boundaries.parquet") {
		p, err := b.ReadBoundaries("cell_boundaries.parquet")
		if err != nil {
			return nil, err
		}
		src.polygons = make([][]polygon, len(src.ids))
		unknown := 0
		for k := 0; k+1 < len(p.Offsets) && k < len(p.CellIDs); k++ {
			i, ok := src.index[p.CellIDs[k]]
			if !ok {
				unknown++
				continue
			}
			ring := make([]point, 0, p.Offsets[k+1]-p.Offsets[k])
			for v := p.Offsets[k]; v < p.Offsets[k+1]; v++ {
				ring = append(ring, point{float64(p.X[v]), float64(p.Y[v])})
			}
			if len(ring) >= 3 {
				src.polygons[i] = append(src.polygons[i], polygon{ring})
			}
		}
		src.find("warning", "boundaries.unknown-cell", "Boundaries of cells cells.parquet does not have", unknown)
		src.read["outlines"] = "cell_boundaries.parquet"
	}
	if src.labels == nil && src.polygons == nil {
		return nil, fmt.Errorf("%s has neither a cell mask (cells.zarr.zip) nor cell boundaries", b.Name)
	}
	// The assignments.
	tr, err := b.OpenTranscripts()
	if err != nil {
		return nil, err
	}
	defer tr.Close()
	r := &records{byID: !own, byRow: own, hasXY: !own}
	unknown := 0
	err = tr.Each(func(batch *xenium.TranscriptBatch) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if batch.TranscriptID == nil && !own {
			return errors.New("its transcripts.parquet has no transcript_id to match the dataset's")
		}
		for i := 0; i < batch.Rows; i++ {
			if !own {
				r.id = append(r.id, batch.TranscriptID[i])
				r.x, r.y = append(r.x, float64(batch.X[i])), append(r.y, float64(batch.Y[i]))
			}
			k := int32(-1)
			if id := string(batch.CellID[i]); !unassignedValues[id] {
				if c, ok := src.index[id]; ok {
					k = c
				} else {
					unknown++
				}
			}
			r.cell = append(r.cell, k)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	src.find("warning", "transcripts.unknown-cell", "Transcripts assigned to cells that cells.parquet does not have", unknown)
	src.records = r
	src.read["transcripts_by"] = "transcript_id"
	if cells.TranscriptCounts != nil {
		src.table = make([]float64, len(cells.IDs))
		for i, v := range cells.TranscriptCounts {
			src.table[i] = float64(v)
		}
		src.tableKind = "genes"
	}
	// The files read, each by its SHA-256 (a zipped bundle's members, as they are inflated).
	for _, name := range []string{"experiment.xenium", "cells.parquet", "cells.zarr.zip", "cell_boundaries.parquet", "transcripts.parquet"} {
		if !b.Has(name) {
			continue
		}
		f, err := b.OpenFile(name)
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		n, err := io.Copy(h, io.NewSectionReader(f, 0, f.Size()))
		f.Close()
		if err != nil {
			return nil, err
		}
		src.files = append(src.files, SourceFile{Path: filepath.Join(b.Path, name), Size: n, SHA256: hex.EncodeToString(h.Sum(nil))})
	}
	return src, nil
}
