package cache

// The derived data of the development bundles (validation/README.md), checked against the
// bundles themselves: every tile, transcript, density bin and polygon. Each bundle that is
// present is checked; SITUCRAFT_REQUIRE_DATA makes a missing one fail rather than skip.

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"situcraft/internal/tiff"
	"situcraft/internal/xenium"
)

func bundleDir(t *testing.T, name string) string {
	t.Helper()
	root := os.Getenv("SITUCRAFT_DATA")
	if root == "" {
		root = filepath.Join("..", "..", "validation", "cache")
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(filepath.Join(dir, "experiment.xenium")); err != nil {
		want := os.Getenv("SITUCRAFT_REQUIRE_DATA")
		if want == "1" || strings.Contains(","+want+",", ","+name+",") {
			t.Fatalf("%s is missing (node validation/fetch.mjs %s)", dir, name)
		}
		t.Skipf("%s is missing (node validation/fetch.mjs %s)", dir, name)
	}
	return dir
}

// fileStates records every file's size and time, to show the bundle is not touched.
func fileStates(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			out[p] = fmt.Sprint(info.ModTime().UnixNano(), " ", info.Size())
		}
		return nil
	})
	return out
}

// Budgets: the two bundles' derived data together stay under 500 MB (roadmap, wave 1, slice 4),
// and each builds well within two minutes even on a small CI runner (about 2 and 4 seconds on an
// Apple M4 with 10 cores; research.md §1.3c).
const (
	cacheBudget = 500e6
	buildBudget = 2 * time.Minute
)

func TestRealBundleCaches(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: under the race detector, building and checking both bundles takes minutes")
	}
	var total int64
	for _, name := range []string{"xenium-lung-2fov", "xenium-breast-2fov"} {
		t.Run(name, func(t *testing.T) {
			dir := bundleDir(t, name)
			before := fileStates(t, dir)
			b, err := xenium.Open(dir, xenium.Options{CacheDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			c := openCache(t, 0)
			start := time.Now()
			status, err := c.Build(context.Background(), b, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			took := time.Since(start)
			// The budget is a claim about the program's speed, which the race detector's
			// instrumentation does not keep (the Breast bundle about 130 s with every package's
			// tests running beside it); its time is only logged then.
			if took > buildBudget && !raceDetector {
				t.Errorf("building took %v, over the %v budget", took, buildBudget)
			}
			if !status.Ready {
				t.Fatalf("not ready after building: %+v", status.Entries)
			}
			var size int64
			sizes := map[string]int64{}
			for _, e := range status.Entries {
				size += e.Size
				sizes[e.Kind] = e.Size
			}
			total += size
			t.Logf("%s: built in %.1f s; image %.1f MB, transcripts %.1f MB, geometry %.1f MB, %.1f MB in all",
				name, took.Seconds(), float64(sizes[KindImage])/1e6, float64(sizes[KindTranscripts])/1e6, float64(sizes[KindGeometry])/1e6, float64(size)/1e6)

			image, _ := status.Entry(KindImage)
			transcripts, _ := status.Entry(KindTranscripts)
			geometry, _ := status.Entry(KindGeometry)
			t.Run("image", func(t *testing.T) { checkRealImage(t, b, c.EntryDir(image.Name)) })
			t.Run("transcripts", func(t *testing.T) { checkRealTranscripts(t, b, c.EntryDir(transcripts.Name)) })
			t.Run("geometry", func(t *testing.T) { checkRealGeometry(t, b, c.EntryDir(geometry.Name)) })

			// Reopened unchanged, nothing is read or built again.
			again, err := xenium.Open(dir, xenium.Options{CacheDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer again.Close()
			start = time.Now()
			st, err := c.Status(again)
			if err != nil || !st.Ready {
				t.Fatalf("reopened: %+v, %v", st, err)
			}
			if _, err := c.Build(context.Background(), again, func(f float64, m string) {
				t.Errorf("reopened, the build reported %q", m)
			}, nil); err != nil {
				t.Fatal(err)
			}
			if d := time.Since(start); d > 2*time.Second {
				t.Errorf("finding the built entries took %v", d)
			}

			// An interrupted build, stopped in each entry and resumed, gives the same bytes.
			resumed := openCache(t, 0)
			for _, stopAt := range []float64{0.15, 0.7, 0.92} {
				ctx, cancel := context.WithCancel(context.Background())
				_, err := resumed.Build(ctx, b, func(f float64, m string) {
					if f >= stopAt {
						cancel()
					}
				}, nil)
				cancel()
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("stopping at %.0f%%: %v", 100*stopAt, err)
				}
			}
			final, err := resumed.Build(context.Background(), b, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range final.Entries {
				sameEntry(t, c.EntryDir(e.Name), resumed.EntryDir(e.Name))
			}
			if after := fileStates(t, dir); len(after) != len(before) {
				t.Error("the bundle's files changed")
			} else {
				for p, s := range before {
					if after[p] != s {
						t.Errorf("%s changed", p)
					}
				}
			}
		})
	}
	if total > cacheBudget {
		t.Errorf("the derived data take %.0f MB, over the %.0f MB budget", float64(total)/1e6, cacheBudget/1e6)
	}
}

// checkRealImage compares every tile of every level with the bundle's own levels, decoded
// directly.
func checkRealImage(t *testing.T, b *xenium.Bundle, dir string) {
	img, err := b.FocusImage()
	if err != nil {
		t.Fatal(err)
	}
	m, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	var sum ImageSummary
	if err := json.Unmarshal(m.Summary, &sum); err != nil {
		t.Fatal(err)
	}
	if len(sum.Channels) != len(img.Channels) {
		t.Fatalf("%d channels cached, the image has %d", len(sum.Channels), len(img.Channels))
	}
	for ci, ch := range img.Channels {
		if sum.Channels[ci].Name != ch.Name {
			t.Errorf("channel %d is %q, not %q", ci, sum.Channels[ci].Name, ch.Name)
		}
		f, err := b.OpenFile(ch.File)
		if err != nil {
			t.Fatal(err)
		}
		tf, err := tiff.Open(f, f.Size())
		if err != nil {
			t.Fatal(err)
		}
		levels := append([]*tiff.IFD{tf.IFDs[ch.IFD]}, tf.IFDs[ch.IFD].SubIFDs...)
		for l, lv := range sum.Channels[ci].Levels {
			if lv.Source != "decoded" {
				t.Errorf("channel %s level %d was computed; the bundle has it", ch.Name, l)
				continue
			}
			ifd := levels[l]
			if ifd.Width != lv.Width || ifd.Height != lv.Height {
				t.Fatalf("level %d is %d×%d, the bundle's %d×%d", l, lv.Width, lv.Height, ifd.Width, ifd.Height)
			}
			cached, err := OpenImage(dir, ci, l)
			if err != nil {
				t.Fatal(err)
			}
			across, _ := ifd.Blocks()
			blocks := map[int][]uint16{}
			for ty := 0; ty < lv.Down; ty++ {
				for tx := 0; tx < lv.Across; tx++ {
					pix, w, h, err := cached.Tile(tx, ty)
					if err != nil {
						t.Fatal(err)
					}
					for y := 0; y < h; y++ {
						for x := 0; x < w; x++ {
							px, py := tx*TileSize+x, ty*TileSize+y
							bi := (py/ifd.BlockHeight)*across + px/ifd.BlockWidth
							block, ok := blocks[bi]
							if !ok {
								raw, err := ifd.DecodeBlock(bi)
								if err != nil {
									t.Fatal(err)
								}
								block = samples16(raw, 16)
								blocks[bi] = block
							}
							want := block[(py%ifd.BlockHeight)*ifd.BlockWidth+px%ifd.BlockWidth]
							if pix[y*w+x] != want {
								t.Fatalf("channel %s level %d pixel (%d, %d) is %d, the bundle's %d", ch.Name, l, px, py, pix[y*w+x], want)
							}
						}
					}
				}
			}
			cached.Close()
		}
		f.Close()
	}
}

// checkRealTranscripts checks that every transcript of transcripts.parquet is in the index once,
// in its tile, with its values, and that the density grids add up.
func checkRealTranscripts(t *testing.T, b *xenium.Bundle, dir string) {
	idx, err := OpenTranscripts(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	cells, err := b.ReadCells()
	if err != nil {
		t.Fatal(err)
	}
	g := idx.Params.Grid
	type stored struct {
		gene         uint16
		x, y, z, qv  float32
		cell         uint32
		flags        uint8
		tileX, tileY int
	}
	byRow := make([]stored, idx.Summary.Transcripts)
	seen := make([]bool, idx.Summary.Transcripts)
	for ty := 0; ty < g.Down(0); ty++ {
		for tx := 0; tx < g.Across(0); tx++ {
			pt, err := idx.Points(tx, ty)
			if err != nil {
				t.Fatal(err)
			}
			for k, gene := range pt.Genes {
				for i := pt.Starts[k]; i < pt.Starts[k+1]; i++ {
					row := pt.Row[i]
					if int64(row) >= idx.Summary.Transcripts || seen[row] {
						t.Fatalf("transcript %d is stored twice or does not exist", row)
					}
					seen[row] = true
					byRow[row] = stored{gene, pt.X[i], pt.Y[i], pt.Z[i], pt.QV[i], pt.Cell[i], pt.Flags[i], tx, ty}
				}
			}
		}
	}
	tr, err := b.OpenTranscripts()
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	row := 0
	err = tr.Each(func(batch *xenium.TranscriptBatch) error {
		for i := 0; i < batch.Rows; i++ {
			if !seen[row] {
				t.Fatalf("transcript %d is not stored", row)
			}
			s := byRow[row]
			if idx.Summary.Genes[s.gene].Name != string(batch.Feature[i]) || s.x != batch.X[i] || s.y != batch.Y[i] || s.z != batch.Z[i] || s.qv != batch.QV[i] {
				t.Fatalf("transcript %d differs from the bundle's", row)
			}
			if tx, ty := g.TileOf(0, float64(batch.X[i]), float64(batch.Y[i])); tx != s.tileX || ty != s.tileY {
				t.Fatalf("transcript %d is in tile (%d, %d), not (%d, %d)", row, s.tileX, s.tileY, tx, ty)
			}
			wantCell := uint32(NoCell)
			if id := string(batch.CellID[i]); id != "UNASSIGNED" {
				c, ok := cells.Index[id]
				if !ok {
					t.Fatalf("transcript %d names cell %s, which cells.parquet does not have", row, id)
				}
				wantCell = uint32(c)
			}
			if s.cell != wantCell || (batch.OverlapsNucleus != nil && int32(s.flags&1) != batch.OverlapsNucleus[i]) {
				t.Fatalf("transcript %d: cell %d (want %d), flags %d", row, s.cell, wantCell, s.flags)
			}
			row++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if int64(row) != idx.Summary.Transcripts {
		t.Fatalf("the bundle has %d transcripts, the index %d", row, idx.Summary.Transcripts)
	}
	if idx.Summary.UnknownCells != 0 {
		t.Errorf("%d transcripts name unknown cells", idx.Summary.UnknownCells)
	}
	checkDensity(t, idx)
	t.Logf("%d transcripts of %d features in %d tiles; Q20 %d; assigned %d", idx.Summary.Transcripts, len(idx.Summary.Genes), g.Across(0)*g.Down(0), idx.Summary.Q20, idx.Summary.Assigned)
}

// checkRealGeometry checks that level 0 holds the bundle's polygons exactly, and every level each
// polygon once.
func checkRealGeometry(t *testing.T, b *xenium.Bundle, dir string) {
	geo, err := OpenGeometry(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer geo.Close()
	cells, err := b.ReadCells()
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range geo.Summary.Sets {
		file := map[string]string{"cells": "cell_boundaries.parquet", "nuclei": "nucleus_boundaries.parquet"}[set.Name]
		polys, err := b.ReadBoundaries(file)
		if err != nil {
			t.Fatal(err)
		}
		// Each cell has at most one polygon in these bundles: find a polygon by its cell.
		byCell := map[uint32]int{}
		for i, id := range polys.CellIDs {
			c, ok := cells.Index[id]
			if !ok {
				t.Fatalf("%s: polygon %d names cell %s, which cells.parquet does not have", file, i, id)
			}
			byCell[uint32(c)] = i
		}
		if len(byCell) != len(polys.CellIDs) || set.Polygons != len(polys.CellIDs) || set.UnknownCells != 0 {
			t.Fatalf("%s: %d polygons for %d cells", file, len(polys.CellIDs), len(byCell))
		}
		g := geo.Params.Grid
		for l := 0; l < g.Levels; l++ {
			seen := map[uint32]bool{}
			for ty := 0; ty < g.Down(l); ty++ {
				for tx := 0; tx < g.Across(l); tx++ {
					tile, err := geo.Tile(set.Name, l, tx, ty)
					if err != nil {
						t.Fatal(err)
					}
					for k, cell := range tile.Cells {
						if seen[cell] {
							t.Fatalf("%s level %d: cell %d twice", set.Name, l, cell)
						}
						seen[cell] = true
						i := byCell[cell]
						xs, ys := polys.X[polys.Offsets[i]:polys.Offsets[i+1]], polys.Y[polys.Offsets[i]:polys.Offsets[i+1]]
						got := tile.XY[2*tile.Starts[k] : 2*tile.Starts[k+1]]
						if l > 0 {
							checkSimplified(t, xs, ys, got, geo.Params.epsilon(l))
							continue
						}
						if len(got) != 2*len(xs) {
							t.Fatalf("%s: cell %d has %d vertices, the bundle %d", set.Name, cell, len(got)/2, len(xs))
						}
						for v := range xs {
							if got[2*v] != xs[v] || got[2*v+1] != ys[v] {
								t.Fatalf("%s: cell %d vertex %d differs", set.Name, cell, v)
							}
						}
					}
				}
			}
			if len(seen) != len(polys.CellIDs) {
				t.Errorf("%s level %d holds %d polygons of %d", set.Name, l, len(seen), len(polys.CellIDs))
			}
		}
		t.Logf("%s: %d polygons; vertices by level %v", set.Name, set.Polygons, vertexCounts(set))
	}
}

func vertexCounts(s SetSummary) []int {
	out := make([]int, len(s.Levels))
	for i, l := range s.Levels {
		out[i] = l.Vertices
	}
	return out
}

// A bundle zipped (or moved) keeps its derived data: entries are named for the files' contents,
// so once its files are read for their checksums nothing is built again.
func TestRealZippedBundleKeepsItsEntries(t *testing.T) {
	dir := bundleDir(t, "xenium-lung-2fov")
	folder, err := xenium.Open(dir, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer folder.Close()
	c := openCache(t, 0)
	built, err := c.Build(context.Background(), folder, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A zip of what the cache reads: the Parquet files compressed (unpacked into the cache to be
	// read), the images stored.
	archive := filepath.Join(t.TempDir(), "lung.zip")
	out, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(out)
	names := []string{"experiment.xenium", "transcripts.parquet", "cells.parquet", "cell_boundaries.parquet", "nucleus_boundaries.parquet"}
	for _, f := range folder.Files {
		if strings.HasPrefix(f.Path, "morphology_focus/") {
			names = append(names, f.Path)
		}
	}
	for _, name := range names {
		method := zip.Deflate
		if strings.HasSuffix(name, ".tif") {
			method = zip.Store
		}
		dst, err := w.CreateHeader(&zip.FileHeader{Name: "outs/" + name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		dst.Write(raw)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out.Close()
	zipped, err := xenium.Open(archive, xenium.Options{CacheDir: c.Root()})
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	st, err := c.Status(zipped)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range st.Entries {
		if e.State != "unchecked" {
			t.Errorf("before its files were read, the zipped bundle's %s is %s", e.Kind, e.State)
		}
	}
	after, err := c.Build(context.Background(), zipped, func(f float64, message string) {
		if message != "Checking the bundle's files" {
			t.Errorf("the zipped bundle's build did more than read its files: %q", message)
		}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range after.Entries {
		if e.State != "ready" || e.Name != built.Entries[i].Name {
			t.Errorf("zipped, %s is %s as %s; the folder's is %s", e.Kind, e.State, e.Name, built.Entries[i].Name)
		}
	}
	u, err := c.Usage()
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, it := range u.Items {
		kinds[it.Kind]++
	}
	if kinds["unzipped"] != 1 || kinds[KindImage] != 1 || kinds[KindTranscripts] != 1 || kinds[KindGeometry] != 1 {
		t.Errorf("the cache holds %v", kinds)
	}
}
