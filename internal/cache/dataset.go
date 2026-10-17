package cache

// A Xenium dataset's entries: its morphology-focus image pyramid, its transcript index and its
// cell and nucleus geometry, all on the grid of its image. Status says which are built without
// reading the bundle's large files (it uses the checksums recorded for files seen before);
// Build checks the files it has not seen and builds what is missing.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"situcraft/internal/tiff"
	"situcraft/internal/xenium"
)

// The kinds of entry.
const (
	KindImage       = "image"
	KindTranscripts = "transcripts"
	KindGeometry    = "geometry"
)

// EntryStatus is the state of one of a dataset's entries.
type EntryStatus struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// State is "ready", "missing", "partial" (a build was started and can resume), "unchecked"
	// (a source file has not been seen before, so its checksum, and the entry's name, are not yet
	// known) or "unavailable" (the bundle lacks what it is built from; see Reason).
	State  string `json:"state"`
	Name   string `json:"name,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Status is the state of a dataset's entries.
type Status struct {
	Grid    *Grid         `json:"grid,omitempty"`
	Entries []EntryStatus `json:"entries"`
	// Ready is true when every entry that can be built is.
	Ready bool `json:"ready"`
}

// Entry returns the status of the dataset's entry of a kind.
func (s *Status) Entry(kind string) (EntryStatus, bool) {
	for _, e := range s.Entries {
		if e.Kind == kind {
			return e, true
		}
	}
	return EntryStatus{}, false
}

type planItem struct {
	kind, title string
	params      any
	files       []string
	reason      string
	sources     []SourceFile
	known       bool // every source's checksum is known
	name        string
}

type plan struct {
	grid  Grid
	image *xenium.Image
	cells *xenium.Cells
	items []*planItem
}

const defaultPixelSize = 0.2125

// plan decides a dataset's entries and their sources.
func (c *Cache) plan(b *xenium.Bundle) (*plan, error) {
	p := &plan{}
	img, imgErr := b.FocusImage()
	if imgErr == nil && img != nil && len(img.Channels) > 0 && len(img.Channels[0].Levels) > 0 {
		ch := img.Channels[0]
		size := ch.PixelSize
		if !(size > 0) {
			size = b.Experiment.PixelSize
		}
		if !(size > 0) {
			size = defaultPixelSize
		}
		p.grid, p.image = NewGrid(size, ch.Levels[0].Width, ch.Levels[0].Height), img
	} else {
		// Without an image the grid covers the cells (and a margin for transcripts near them).
		cells, err := b.ReadCells()
		if err != nil {
			return nil, fmt.Errorf("neither the morphology image nor cells.parquet can be read to place the tiles: %w", err)
		}
		size := b.Experiment.PixelSize
		if !(size > 0) {
			size = defaultPixelSize
		}
		maxX, maxY := 0.0, 0.0
		for i := range cells.X {
			maxX, maxY = math.Max(maxX, cells.X[i]), math.Max(maxY, cells.Y[i])
		}
		p.grid = NewGrid(size, int(math.Ceil((maxX+50)/size)), int(math.Ceil((maxY+50)/size)))
		p.cells = cells
	}
	image := &planItem{kind: KindImage, title: "Image pyramid"}
	switch {
	case p.image == nil && imgErr != nil:
		image.reason = "The morphology image cannot be read: " + imgErr.Error()
	case p.image == nil:
		image.reason = "The bundle has no morphology-focus image."
	default:
		image.params = struct {
			Grid     Grid   `json:"grid"`
			Encoding string `json:"encoding"`
		}{p.grid, tileEncoding}
		image.files = p.image.Files
	}
	transcripts := &planItem{kind: KindTranscripts, title: "Transcript index", params: NewTranscriptParams(p.grid), files: []string{"transcripts.parquet", "cells.parquet"}}
	geometry := &planItem{kind: KindGeometry, title: "Cell outlines", params: NewGeometryParams(p.grid), files: []string{"cells.parquet"}}
	for _, f := range []string{"cell_boundaries.parquet", "nucleus_boundaries.parquet"} {
		if b.Has(f) {
			geometry.files = append(geometry.files, f)
		}
	}
	if len(geometry.files) == 1 {
		geometry.reason = "The bundle has no cell or nucleus boundaries in Parquet."
	}
	p.items = []*planItem{image, transcripts, geometry}
	for _, it := range p.items {
		if it.reason != "" {
			continue
		}
		it.known = true
		for _, f := range it.files {
			info, ok := b.Stat(f)
			if !ok {
				it.reason = "The bundle has no " + f + "."
				break
			}
			src := SourceFile{Path: f, Size: info.Size}
			if id, err := b.FileIdentity(f); err == nil {
				src.SHA256, _ = c.KnownChecksum(id)
			}
			if src.SHA256 == "" {
				it.known = false
			}
			it.sources = append(it.sources, src)
		}
		if it.reason == "" && it.known {
			it.name, _ = Spec{Kind: it.kind, Params: it.params, Sources: it.sources}.Name()
		}
	}
	return p, nil
}

func (c *Cache) statusOf(p *plan) *Status {
	grid := p.grid
	s := &Status{Grid: &grid, Ready: true}
	for _, it := range p.items {
		e := EntryStatus{Kind: it.kind, Title: it.title, Name: it.name}
		switch {
		case it.reason != "":
			e.State, e.Reason = "unavailable", it.reason
		case !it.known:
			e.State = "unchecked"
		default:
			if m, ok := c.Lookup(it.name); ok {
				e.State, e.Size = "ready", m.Size()
			} else if _, err := os.Stat(c.buildingDirOf(it.name)); err == nil {
				e.State = "partial"
			} else {
				e.State = "missing"
			}
		}
		if e.State != "ready" && e.State != "unavailable" {
			s.Ready = false
		}
		s.Entries = append(s.Entries, e)
	}
	return s
}

func (c *Cache) buildingDirOf(name string) string {
	return c.buildingDir() + string(os.PathSeparator) + name
}

// Status says which of a bundle's entries are built. It reads no large file.
func (c *Cache) Status(b *xenium.Bundle) (*Status, error) {
	p, err := c.plan(b)
	if err != nil {
		return nil, err
	}
	return c.statusOf(p), nil
}

// Progress receives a build's fraction done and what it is doing.
type Progress func(fraction float64, message string)

// Build builds a bundle's missing entries (it returns at once when none is). Cancelling ctx stops
// it soon after; what was finished is kept and the next Build resumes from there.
func (c *Cache) Build(ctx context.Context, b *xenium.Bundle, progress Progress, logf func(format string, args ...any)) (*Status, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	p, err := c.plan(b)
	if err != nil {
		return nil, err
	}
	// The files not seen before are read for their checksums.
	var unknown int64
	for _, it := range p.items {
		if it.reason == "" && !it.known {
			for _, s := range it.sources {
				if s.SHA256 == "" {
					unknown += s.Size
				}
			}
		}
	}
	hashed := int64(0)
	weightHash := 0.0
	if unknown > 0 {
		weightHash = 0.1
	}
	for _, it := range p.items {
		if it.reason != "" || it.known {
			continue
		}
		for i := range it.sources {
			s := &it.sources[i]
			if s.SHA256 != "" {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			id, err := b.FileIdentity(s.Path)
			if err != nil {
				return nil, err
			}
			path := s.Path
			s.SHA256, err = c.Checksum(id, s.Size, func() (io.ReadCloser, error) {
				f, err := b.OpenFile(path)
				if err != nil {
					return nil, err
				}
				return readCloser{io.NewSectionReader(f, 0, f.Size()), f}, nil
			}, func(n int64) {
				hashed += n
				progress(weightHash*float64(hashed)/float64(unknown), "Checking the bundle's files")
			})
			if err != nil {
				return nil, fmt.Errorf("%s: %w", s.Path, err)
			}
			// A file shared by two entries is read once.
			for _, other := range p.items {
				for k := range other.sources {
					if other.sources[k].Path == path && other.sources[k].SHA256 == "" {
						other.sources[k].SHA256 = s.SHA256
					}
				}
			}
		}
		it.known = true
		it.name, err = Spec{Kind: it.kind, Params: it.params, Sources: it.sources}.Name()
		if err != nil {
			return nil, err
		}
	}
	status := c.statusOf(p)
	// What is left to build, weighted by its usual share of the time.
	weights := map[string]float64{KindImage: 6, KindTranscripts: 3, KindGeometry: 1}
	var todo []*planItem
	total := 0.0
	for i, it := range p.items {
		if st := status.Entries[i].State; st == "missing" || st == "partial" {
			todo = append(todo, it)
			total += weights[it.kind]
		}
	}
	if len(todo) == 0 {
		return status, nil
	}
	if err := c.ensureRoom(p, todo); err != nil {
		return nil, err
	}
	doneWeight := 0.0
	for _, it := range todo {
		share := weights[it.kind] / total
		report := func(f float64, message string) {
			progress(weightHash+(1-weightHash)*(doneWeight+share*min(max(f, 0), 1)), message)
		}
		m, err := c.buildItem(ctx, b, p, it, report, logf)
		if err != nil {
			return nil, err
		}
		logf("%s built: %s in %s.", it.title, humanBytes(m.Size()), it.name)
		doneWeight += share
	}
	if removed, err := c.Evict(); err != nil {
		logf("The cache could not be trimmed to its limit: %v", err)
	} else {
		for _, r := range removed {
			logf("Removed %s (%s) from the cache to stay within its limit.", r.Name, humanBytes(r.Size))
		}
	}
	return c.statusOf(p), nil
}

type readCloser struct {
	io.Reader
	io.Closer
}

// ensureRoom checks there is room for what is to be built, removing old entries if that helps.
func (c *Cache) ensureRoom(p *plan, todo []*planItem) error {
	var need int64
	for _, it := range todo {
		switch it.kind {
		case KindImage:
			for range p.image.Channels {
				// A pyramid is a third more than its full level; tiles deflate to about half.
				need += int64(p.grid.Width) * int64(p.grid.Height) * 2 * 4 / 3 / 2
			}
		default:
			for _, s := range it.sources {
				// Spill files and the index take a few times the Parquet files' size.
				need += 4 * s.Size
			}
		}
	}
	const reserve = 512 << 20
	free, err := xenium.FreeBytes(c.root)
	if err != nil || free >= need+reserve {
		return nil
	}
	if _, err := c.Evict(); err == nil {
		if free, err = xenium.FreeBytes(c.root); err != nil || free >= need+reserve {
			return nil
		}
	}
	return fmt.Errorf("not enough free space for the derived data: about %s is needed in %s and %s is free", humanBytes(need+reserve), c.root, humanBytes(free))
}

func (c *Cache) buildItem(ctx context.Context, b *xenium.Bundle, p *plan, it *planItem, progress Progress, logf func(string, ...any)) (*Manifest, error) {
	spec := Spec{Kind: it.kind, Params: it.params, Sources: it.sources}
	bd, err := c.begin(spec)
	if errors.Is(err, ErrBusy) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("SituCraft cannot write in its cache folder (%s): %w", c.root, err)
	}
	defer bd.release()
	if bd.resumed {
		logf("Resuming the %s where an earlier build stopped.", lower(it.title))
	}
	var summary any
	switch it.kind {
	case KindImage:
		chans, bits, closeAll, err := ImageSources(b, p.image)
		if err != nil {
			return nil, err
		}
		defer closeAll()
		summary, err = buildImage(ctx, bd, p.grid, bits, chans, func(done, total int) {
			progress(float64(done)/float64(max(total, 1)), "Decoding the image into tiles")
		})
		if err != nil {
			return nil, err
		}
	case KindTranscripts:
		cells, err := cellsOf(b, p)
		if err != nil {
			return nil, err
		}
		tr, err := b.OpenTranscripts()
		if err != nil {
			return nil, err
		}
		defer tr.Close()
		summary, err = buildTranscripts(ctx, bd, it.params.(TranscriptParams), TranscriptInput{Each: tr.Each, CellIndex: cells.Index, Rows: tr.File.NumRows}, progress)
		if err != nil {
			return nil, err
		}
	case KindGeometry:
		cells, err := cellsOf(b, p)
		if err != nil {
			return nil, err
		}
		in := GeometryInput{CellIndex: cells.Index}
		if b.Has("cell_boundaries.parquet") {
			if in.Cells, err = b.ReadBoundaries("cell_boundaries.parquet"); err != nil {
				return nil, err
			}
		}
		if b.Has("nucleus_boundaries.parquet") {
			if in.Nuclei, err = b.ReadBoundaries("nucleus_boundaries.parquet"); err != nil {
				return nil, err
			}
		}
		summary, err = buildGeometry(ctx, bd, it.params.(GeometryParams), in, func(f float64) { progress(f, "Tiling the cell outlines") })
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown entry kind %s", it.kind)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return bd.commit(summary)
}

func cellsOf(b *xenium.Bundle, p *plan) (*xenium.Cells, error) {
	if p.cells == nil {
		cells, err := b.ReadCells()
		if err != nil {
			return nil, err
		}
		p.cells = cells
	}
	return p.cells, nil
}

// ImageSources opens each channel's levels for decoding a block at a time (safe to decode from
// several goroutines at once); the function closes the files.
func ImageSources(b *xenium.Bundle, img *xenium.Image) ([]SourceChannel, int, func(), error) {
	var files []io.Closer
	closeAll := func() {
		for _, f := range files {
			f.Close()
		}
	}
	opened := map[string]*tiff.File{}
	bits := 0
	var chans []SourceChannel
	for _, ch := range img.Channels {
		if ch.Z != 0 {
			continue
		}
		t, ok := opened[ch.File]
		if !ok {
			f, err := b.OpenFile(ch.File)
			if err != nil {
				closeAll()
				return nil, 0, nil, err
			}
			files = append(files, f)
			if t, err = tiff.Open(f, f.Size()); err != nil {
				closeAll()
				return nil, 0, nil, fmt.Errorf("%s: %w", ch.File, err)
			}
			opened[ch.File] = t
		}
		if ch.IFD >= len(t.IFDs) {
			closeAll()
			return nil, 0, nil, fmt.Errorf("%s has no image directory %d", ch.File, ch.IFD)
		}
		top := t.IFDs[ch.IFD]
		sc := SourceChannel{Name: ch.Name, Purpose: ch.Purpose}
		for _, ifd := range append([]*tiff.IFD{top}, top.SubIFDs...) {
			if ifd.SamplesPerPixel != 1 || len(ifd.BitsPerSample) == 0 || (ifd.BitsPerSample[0] != 8 && ifd.BitsPerSample[0] != 16) || ifd.SampleFormat == 3 {
				if ifd == top {
					closeAll()
					return nil, 0, nil, fmt.Errorf("%s: channel %s is not 8- or 16-bit grey", ch.File, ch.Name)
				}
				break
			}
			bits = max(bits, ifd.BitsPerSample[0])
			sc.Levels = append(sc.Levels, sourceLevel(ifd))
		}
		chans = append(chans, sc)
	}
	return chans, bits, closeAll, nil
}

func sourceLevel(ifd *tiff.IFD) SourceLevel {
	across, _ := ifd.Blocks()
	return SourceLevel{
		Width: ifd.Width, Height: ifd.Height, BlockWidth: ifd.BlockWidth, BlockHeight: ifd.BlockHeight,
		Decode: func(bx, by int) ([]uint16, error) {
			raw, err := ifd.DecodeBlock(by*across + bx)
			if err != nil {
				return nil, err
			}
			bw, bh := ifd.BlockSize(by*across + bx)
			if bw != ifd.BlockWidth || bh != ifd.BlockHeight {
				// A strip at the bottom is shorter: pad it to a whole block.
				full := make([]uint16, ifd.BlockWidth*ifd.BlockHeight)
				copy(full, samples16(raw, ifd.BitsPerSample[0]))
				return full, nil
			}
			return samples16(raw, ifd.BitsPerSample[0]), nil
		},
	}
}

func samples16(raw []byte, bits int) []uint16 {
	if bits == 8 {
		out := make([]uint16, len(raw))
		for i, v := range raw {
			out[i] = uint16(v)
		}
		return out
	}
	out := make([]uint16, len(raw)/2)
	for i := range out {
		out[i] = binary.LittleEndian.Uint16(raw[2*i:])
	}
	return out
}

// OpenImage opens a complete image entry's channel level.
func OpenImage(dir string, channel, level int) (*imageFile, error) {
	return openImageFile(dir + string(os.PathSeparator) + imageFileName(channel, level))
}

func humanBytes(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.0f MB", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.0f kB", float64(n)/1e3)
	}
	return fmt.Sprintf("%d bytes", n)
}

func lower(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]|0x20) + s[1:]
}
