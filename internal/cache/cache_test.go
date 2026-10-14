package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"situcraft/internal/xenium"
)

func openCache(t *testing.T, limit int64) *Cache {
	t.Helper()
	c, err := Open(t.TempDir(), limit)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTheGridIsTheImagePyramid(t *testing.T) {
	g := NewGrid(0.2125, 5791, 3553)
	if g.Levels != 5 {
		t.Errorf("%d levels, want 5 (the Lung bundle's image has five)", g.Levels)
	}
	for l, want := range [][2]int{{12, 7}, {6, 4}, {3, 2}, {2, 1}, {1, 1}} {
		if g.Across(l) != want[0] || g.Down(l) != want[1] {
			t.Errorf("level %d: %d×%d tiles, want %d×%d", l, g.Across(l), g.Down(l), want[0], want[1])
		}
	}
	if got := g.TileUm(1); math.Abs(got-217.6) > 1e-9 {
		t.Errorf("a level-1 tile is %v µm", got)
	}
	for _, c := range []struct {
		x, y   float64
		tx, ty int
	}{{0, 0, 0, 0}, {-3, -0.5, 0, 0}, {108.79, 108.81, 0, 1}, {1e9, 1e9, 11, 6}, {math.NaN(), 50, 0, 0}} {
		if tx, ty := g.TileOf(0, c.x, c.y); tx != c.tx || ty != c.ty {
			t.Errorf("TileOf(%v, %v) = %d, %d; want %d, %d", c.x, c.y, tx, ty, c.tx, c.ty)
		}
	}
	// A level's tiles are its children's parents, at the edges too.
	for l := 1; l < g.Levels; l++ {
		for _, x := range []float64{-1, 0, 500, 1229.9, 1300} {
			tx0, _ := g.TileOf(0, x, 0)
			tx, _ := g.TileOf(l, x, 0)
			if tx0>>l != tx {
				t.Errorf("level %d: x=%v is in tile %d, its level-0 tile's parent is %d", l, x, tx, tx0>>l)
			}
		}
	}
	if one := NewGrid(1, 100, 100); one.Levels != 1 {
		t.Errorf("a one-tile image has %d levels", one.Levels)
	}
}

func TestEntryNamesFollowTheirSources(t *testing.T) {
	base := Spec{Kind: "image", Params: map[string]int{"tile": 512}, Sources: []SourceFile{{Path: "a.tif", Size: 10, SHA256: "aa"}}}
	name, err := base.Name()
	if err != nil || !strings.HasPrefix(name, "image-") || len(name) != len("image-")+24 {
		t.Fatalf("name %q, %v", name, err)
	}
	again, _ := base.Name()
	if again != name {
		t.Error("the same spec named twice differently")
	}
	for _, changed := range []Spec{
		{Kind: "image", Params: map[string]int{"tile": 256}, Sources: base.Sources},
		{Kind: "image", Params: base.Params, Sources: []SourceFile{{Path: "a.tif", Size: 10, SHA256: "ab"}}},
		{Kind: "image", Params: base.Params, Sources: []SourceFile{{Path: "b.tif", Size: 10, SHA256: "aa"}}},
		{Kind: "geometry", Params: base.Params, Sources: base.Sources},
	} {
		if n, _ := changed.Name(); n == name {
			t.Errorf("%+v has the same name as %+v", changed, base)
		}
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{"10GB": 10e9, "500 mb": 500e6, "2.5G": 2.5e9, "0": 0, "1024": 1024, "3kB": 3000, "1TB": 1e12} {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ten", "-1GB", "5XB", "1e30"} {
		if _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) succeeded", bad)
		}
	}
}

func TestChecksumsAreReadOnceAndRemembered(t *testing.T) {
	c := openCache(t, 0)
	data := []byte("transcripts")
	opened := 0
	open := func() (io.ReadCloser, error) {
		opened++
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	sum, err := c.Checksum("file\x00/x\x0011\x001", int64(len(data)), open, nil)
	if want := sha256.Sum256(data); err != nil || sum != hex.EncodeToString(want[:]) {
		t.Fatalf("%s, %v", sum, err)
	}
	if again, _ := c.Checksum("file\x00/x\x0011\x001", int64(len(data)), open, nil); again != sum || opened != 1 {
		t.Errorf("read %d times", opened)
	}
	reopened, err := Open(c.Root(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if known, ok := reopened.KnownChecksum("file\x00/x\x0011\x001"); !ok || known != sum {
		t.Error("the checksum was not remembered")
	}
	if _, err := c.Checksum("other", 99, open, nil); err == nil {
		t.Error("a file shorter than its size was accepted")
	}
}

func TestOneBuildAtATimePerEntry(t *testing.T) {
	c := openCache(t, 0)
	spec := Spec{Kind: "test", Params: 1}
	first, err := c.begin(spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.begin(spec); !errors.Is(err, ErrBusy) {
		t.Errorf("a second build of the entry started: %v", err)
	}
	first.release()
	second, err := c.begin(spec)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	// A build that stopped touching its lock (its program died) is taken over.
	old := time.Now().Add(-2 * c.staleLock)
	os.Chtimes(second.path(lockName), old, old)
	close(second.stop)
	second.stopped.Wait()
	third, err := c.begin(spec)
	if err != nil {
		t.Fatalf("a stale lock was not taken over: %v", err)
	}
	if err := third.writeFile("data", []byte("x")); err != nil {
		t.Fatal(err)
	}
	m, err := third.commit(map[string]int{"n": 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 1 || m.Files[0].Name != "data" || m.Size() != 1 {
		t.Errorf("manifest files %+v", m.Files)
	}
	if got, ok := c.Lookup(m.Name); !ok || got.Name != m.Name {
		t.Error("the committed entry is not found")
	}
	if err := c.Verify(m.Name); err != nil {
		t.Error(err)
	}
	os.WriteFile(filepath.Join(c.EntryDir(m.Name), "data"), []byte("y"), 0o644)
	if err := c.Verify(m.Name); err == nil {
		t.Error("a changed file passed verification")
	}
	os.WriteFile(filepath.Join(c.EntryDir(m.Name), "data"), []byte("yy"), 0o644)
	if _, ok := c.Lookup(m.Name); ok {
		t.Error("an entry whose file changed size was found")
	}
}

// makeItem makes a complete entry of a given size last used at a given time.
func makeItem(t *testing.T, c *Cache, name string, size int, used time.Time) {
	t.Helper()
	dir := c.EntryDir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data"), make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, used, used); err != nil {
		t.Fatal(err)
	}
}

func TestEvictionRemovesTheLeastRecentlyUsed(t *testing.T) {
	c := openCache(t, 2500)
	long := time.Now().Add(-72 * time.Hour)
	makeItem(t, c, "image-old", 1000, long)
	makeItem(t, c, "image-older", 1000, long.Add(-time.Hour))
	makeItem(t, c, "transcripts-recent", 1000, long.Add(time.Hour))
	makeItem(t, c, "geometry-now", 1000, time.Now().Add(time.Second))
	// The unpacked members of a zipped bundle count too.
	unzipped := filepath.Join(c.Root(), "unzipped", "abc")
	os.MkdirAll(unzipped, 0o755)
	os.WriteFile(filepath.Join(unzipped, "cells.parquet"), make([]byte, 400), 0o644)
	os.Chtimes(unzipped, long.Add(-2*time.Hour), long.Add(-2*time.Hour))
	u, err := c.Usage()
	if err != nil || u.Size != 4400 || len(u.Items) != 5 {
		t.Fatalf("usage %d in %d items, %v", u.Size, len(u.Items), err)
	}
	removed, err := c.Evict()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range removed {
		names = append(names, r.Name)
	}
	if strings.Join(names, " ") != "abc image-older image-old" {
		t.Errorf("removed %v, want the least recently used first until within the limit", names)
	}
	// What is in use stays even when it alone exceeds the limit.
	c.limit = 10
	removed, _ = c.Evict()
	for _, r := range removed {
		if r.Name == "geometry-now" {
			t.Error("an entry used since the program started was removed")
		}
	}
	if _, err := os.Stat(c.EntryDir("geometry-now")); err != nil {
		t.Error("the entry in use was removed")
	}
	if err := c.Remove("../x"); err == nil {
		t.Error("Remove accepted a path")
	}
	if err := c.Remove("geometry-now"); err != nil {
		t.Error(err)
	}
}

// --- Image pyramids ----------------------------------------------------------------------------

// synthImage is a source image whose pixel (x, y) at level 0 is a function of x and y, with its
// first levels given as blocks (as a TIFF holds them) and the rest left for the cache to compute.
func synthImage(width, height, block, sourceLevels int) SourceChannel {
	ch := SourceChannel{Name: "DAPI", Purpose: "Nuclear"}
	w, h := width, height
	for l := 0; l < sourceLevels; l++ {
		lw, lh, level := w, h, l
		ch.Levels = append(ch.Levels, SourceLevel{Width: lw, Height: lh, BlockWidth: block, BlockHeight: block, Decode: func(bx, by int) ([]uint16, error) {
			out := make([]uint16, block*block)
			for y := 0; y < block; y++ {
				for x := 0; x < block; x++ {
					px, py := bx*block+x, by*block+y
					if px < lw && py < lh {
						out[y*block+x] = synthPixel(level, px, py)
					}
				}
			}
			return out, nil
		}})
		w, h = w/2, h/2
	}
	return ch
}

func synthPixel(level, x, y int) uint16 {
	return uint16((x*7919 + y*104729 + level*31) % 65521)
}

func TestImagePyramidsHoldTheSourceExactly(t *testing.T) {
	c := openCache(t, 0)
	grid := NewGrid(0.5, 1300, 900)
	if grid.Levels != 3 {
		t.Fatalf("%d levels", grid.Levels)
	}
	chans := []SourceChannel{synthImage(1300, 900, 256, 2), synthImage(1300, 900, 300, 1)}
	spec := Spec{Kind: KindImage, Params: grid}
	build := func(ctx context.Context) (*Manifest, error) {
		b, err := c.begin(spec)
		if err != nil {
			return nil, err
		}
		defer b.release()
		sum, err := buildImage(ctx, b, grid, 16, chans, func(int, int) {})
		if err != nil {
			return nil, err
		}
		return b.commit(sum)
	}
	m, err := build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var sum ImageSummary
	if err := json.Unmarshal(m.Summary, &sum); err != nil {
		t.Fatal(err)
	}
	wantSources := [][]string{{"decoded", "decoded", "computed"}, {"decoded", "computed", "computed"}}
	for c2, ch := range sum.Channels {
		for l, lv := range ch.Levels {
			if lv.Source != wantSources[c2][l] {
				t.Errorf("channel %d level %d is %s", c2, l, lv.Source)
			}
		}
	}
	dir := c.EntryDir(m.Name)
	// Level 0 and decoded levels hold the source's pixels; computed levels the 2×2 means of the
	// level above.
	for ci := range chans {
		var above []uint16
		aw, ah := 0, 0
		for l, lv := range sum.Channels[ci].Levels {
			f, err := OpenImage(dir, ci, l)
			if err != nil {
				t.Fatal(err)
			}
			full := make([]uint16, lv.Width*lv.Height)
			for ty := 0; ty < lv.Down; ty++ {
				for tx := 0; tx < lv.Across; tx++ {
					pix, w, h, err := f.Tile(tx, ty)
					if err != nil {
						t.Fatal(err)
					}
					for y := 0; y < h; y++ {
						copy(full[(ty*TileSize+y)*lv.Width+tx*TileSize:], pix[y*w:(y+1)*w])
					}
				}
			}
			f.Close()
			for y := 0; y < lv.Height; y++ {
				for x := 0; x < lv.Width; x++ {
					var want uint16
					if lv.Source == "decoded" {
						want = synthPixel(l, x, y)
					} else {
						var s, n uint32
						for _, d := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
							if ax, ay := 2*x+d[0], 2*y+d[1]; ax < aw && ay < ah {
								s += uint32(above[ay*aw+ax])
								n++
							}
						}
						want = uint16((s + n/2) / n)
					}
					if full[y*lv.Width+x] != want {
						t.Fatalf("channel %d level %d pixel (%d, %d) = %d, want %d", ci, l, x, y, full[y*lv.Width+x], want)
					}
				}
			}
			above, aw, ah = full, lv.Width, lv.Height
		}
	}
	// An interrupted build resumes and makes the same bytes.
	c2 := openCache(t, 0)
	c, c2 = c2, c
	for _, stopAfter := range []int{1, 3} {
		ctx, cancel := context.WithCancel(context.Background())
		b, err := c.begin(spec)
		if err != nil {
			t.Fatal(err)
		}
		files := 0
		_, err = buildImage(ctx, b, grid, 16, chans, func(done, total int) {
			if entries, _ := os.ReadDir(b.dir); len(entries)-1 > files {
				files = len(entries) - 1
			}
			if files >= stopAfter {
				cancel()
			}
		})
		b.release()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("stopping after %d files: %v", stopAfter, err)
		}
	}
	resumed, err := build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sameEntry(t, c2.EntryDir(m.Name), c.EntryDir(resumed.Name))
}

// sameEntry checks two entries hold the same files, byte for byte (build.json aside).
func sameEntry(t *testing.T, a, b string) {
	t.Helper()
	list := func(dir string) map[string][]byte {
		out := map[string][]byte{}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() == buildName {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out[e.Name()] = raw
		}
		return out
	}
	fa, fb := list(a), list(b)
	if len(fa) != len(fb) {
		t.Errorf("%d files and %d files", len(fa), len(fb))
	}
	for name, raw := range fa {
		if !bytes.Equal(raw, fb[name]) {
			t.Errorf("%s differs", name)
		}
	}
}

// --- Transcripts -------------------------------------------------------------------------------

type synthTranscript struct {
	cell        string
	feature     string
	x, y, z, qv float32
	nucleus     int32
}

// synthTranscripts makes seeded transcripts over a grid, some past its edges, some in no cell
// and some in a cell cells.parquet would not have, in batches like row groups.
func synthTranscripts(seed uint64, grid Grid, n int) ([]synthTranscript, map[string]int32) {
	rng := rand.New(rand.NewPCG(seed, 1))
	cells := map[string]int32{}
	for i := 0; i < 50; i++ {
		cells[xenium.CellIDString(uint32(i+1), 1)] = int32(i)
	}
	features := []string{"EPCAM", "ACTA2", "CD3E", "NegControlProbe_00042", "UnassignedCodeword_0007", "BLANK_0001", "KRT8", "DeprecatedCodeword_0003"}
	w, h := float64(grid.Width)*grid.PixelSize, float64(grid.Height)*grid.PixelSize
	out := make([]synthTranscript, n)
	for i := range out {
		tr := synthTranscript{
			feature: features[rng.IntN(len(features))],
			x:       float32(rng.Float64()*(w+10) - 5), y: float32(rng.Float64()*(h+10) - 5),
			z: float32(rng.Float64() * 20), qv: float32(rng.Float64() * 40), nucleus: int32(rng.IntN(2)),
		}
		// Clusters, so that bins hold several transcripts.
		if i%3 == 0 {
			tr.x, tr.y = float32(w/3)+float32(rng.NormFloat64()), float32(h/2)+float32(rng.NormFloat64())
		}
		switch r := rng.IntN(10); {
		case r < 3:
			tr.cell = "UNASSIGNED"
		case r == 3:
			tr.cell = "zzzzzzzz-1"
		default:
			tr.cell = xenium.CellIDString(uint32(rng.IntN(50)+1), 1)
		}
		out[i] = tr
	}
	return out, cells
}

func transcriptInput(trs []synthTranscript, cells map[string]int32, batch int) TranscriptInput {
	return TranscriptInput{CellIndex: cells, Rows: int64(len(trs)), Each: func(fn func(*xenium.TranscriptBatch) error) error {
		for start := 0; start < len(trs); start += batch {
			part := trs[start:min(len(trs), start+batch)]
			b := &xenium.TranscriptBatch{Rows: len(part)}
			for _, tr := range part {
				b.CellID = append(b.CellID, []byte(tr.cell))
				b.Feature = append(b.Feature, []byte(tr.feature))
				b.X, b.Y, b.Z, b.QV = append(b.X, tr.x), append(b.Y, tr.y), append(b.Z, tr.z), append(b.QV, tr.qv)
				b.OverlapsNucleus = append(b.OverlapsNucleus, tr.nucleus)
			}
			if err := fn(b); err != nil {
				return err
			}
		}
		return nil
	}}
}

func buildSynthTranscripts(t *testing.T, c *Cache, p TranscriptParams, in TranscriptInput, ctx context.Context, progress func(float64, string)) (*Manifest, error) {
	t.Helper()
	b, err := c.begin(Spec{Kind: KindTranscripts, Params: p})
	if err != nil {
		return nil, err
	}
	defer b.release()
	if progress == nil {
		progress = func(float64, string) {}
	}
	sum, err := buildTranscripts(ctx, b, p, in, progress)
	if err != nil {
		return nil, err
	}
	return b.commit(sum)
}

func TestTheTranscriptIndexHoldsEveryTranscriptOnce(t *testing.T) {
	// Small tiles make many tiles and several groups of rows.
	grid := Grid{PixelSize: 0.5, Width: 1000, Height: 1300, Tile: 64}
	grid.Levels = 1
	for grid.Across(grid.Levels-1) > 1 || grid.Down(grid.Levels-1) > 1 {
		grid.Levels++
	}
	p := NewTranscriptParams(grid)
	p.GroupRows = 4 // 21 rows of tiles: six groups, the last of one row
	p.DensityBins = 16
	trs, cells := synthTranscripts(7, grid, 20000)
	c := openCache(t, 0)
	m, err := buildSynthTranscripts(t, c, p, transcriptInput(trs, cells, 3000), context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := OpenTranscripts(c.EntryDir(m.Name))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	s := idx.Summary
	if s.Transcripts != int64(len(trs)) || s.Groups != 6 || len(s.DensityLevels) != grid.Levels-1 || !s.HasNucleus {
		t.Fatalf("summary %+v", s)
	}
	// Genes come before the controls and codewords.
	var names []string
	for _, g := range s.Genes {
		names = append(names, g.Name)
	}
	if got := strings.Join(names, " "); got != "ACTA2 CD3E EPCAM KRT8 NegControlProbe_00042 BLANK_0001 UnassignedCodeword_0007 DeprecatedCodeword_0003" {
		t.Errorf("gene order %s", got)
	}
	// Every transcript once, in the tile holding it, with its values.
	seen := make([]bool, len(trs))
	var unknown, assigned int64
	for ty := 0; ty < grid.Down(0); ty++ {
		for tx := 0; tx < grid.Across(0); tx++ {
			pt, err := idx.Points(tx, ty)
			if err != nil {
				t.Fatal(err)
			}
			for k, gene := range pt.Genes {
				for i := pt.Starts[k]; i < pt.Starts[k+1]; i++ {
					row := pt.Row[i]
					if seen[row] {
						t.Fatalf("transcript %d appears twice", row)
					}
					seen[row] = true
					tr := trs[row]
					if wx, wy := grid.TileOf(0, float64(tr.x), float64(tr.y)); wx != tx || wy != ty {
						t.Fatalf("transcript %d is in tile (%d, %d), not (%d, %d)", row, tx, ty, wx, wy)
					}
					if s.Genes[gene].Name != tr.feature || pt.X[i] != tr.x || pt.Y[i] != tr.y || pt.Z[i] != tr.z || pt.QV[i] != tr.qv || int32(pt.Flags[i]&1) != tr.nucleus {
						t.Fatalf("transcript %d differs", row)
					}
					want := uint32(NoCell)
					if id, ok := cells[tr.cell]; ok {
						want = uint32(id)
						assigned++
					} else if tr.cell != "UNASSIGNED" {
						unknown++
					}
					if pt.Cell[i] != want {
						t.Fatalf("transcript %d is in cell %d, not %d", row, pt.Cell[i], want)
					}
					if i > pt.Starts[k] && pt.Row[i] <= pt.Row[i-1] {
						t.Fatal("a gene's points are not in row order")
					}
				}
			}
		}
	}
	for row, ok := range seen {
		if !ok {
			t.Fatalf("transcript %d is missing", row)
		}
	}
	if s.Assigned != assigned || s.UnknownCells != unknown {
		t.Errorf("assigned %d (want %d), unknown %d (want %d)", s.Assigned, assigned, s.UnknownCells, unknown)
	}
	checkDensity(t, idx)
	// Interrupted in each phase, then resumed: the same bytes.
	for _, stop := range []string{"Reading the transcripts", "Sorting the transcripts into tiles", "Counting the transcripts of each gene"} {
		c2 := openCache(t, 0)
		calls := 0
		ctx, cancel := context.WithCancel(context.Background())
		_, err := buildSynthTranscripts(t, c2, p, transcriptInput(trs, cells, 3000), ctx, func(f float64, phase string) {
			if phase == stop {
				if calls++; calls == 2 {
					cancel()
				}
			}
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("stopping in %q: %v", stop, err)
		}
		resumed, err := buildSynthTranscripts(t, c2, p, transcriptInput(trs, cells, 3000), context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		sameEntry(t, c.EntryDir(m.Name), c2.EntryDir(resumed.Name))
		// Stopped while reading, nothing was finished, so the build started again; later, it
		// resumed.
		info, _ := os.ReadFile(filepath.Join(c2.EntryDir(resumed.Name), buildName))
		if want := stop != "Reading the transcripts"; strings.Contains(string(info), `"resumed": true`) != want {
			t.Errorf("stopping in %q: build.json says %s", stop, info)
		}
	}
}

// checkDensity checks every level's grids against the points: each gene's grid sums to its
// count, and level 1's bins hold exactly the points in them.
func checkDensity(t *testing.T, idx *Transcripts) {
	t.Helper()
	g, bins := idx.Params.Grid, idx.Params.DensityBins
	genes := len(idx.Summary.Genes)
	for _, l := range idx.Summary.DensityLevels {
		n, q := make([]int64, genes+1), make([]int64, genes+1)
		for ty := 0; ty < g.Down(l); ty++ {
			for tx := 0; tx < g.Across(l); tx++ {
				d, err := idx.Density(l, tx, ty)
				if err != nil {
					t.Fatal(err)
				}
				for k, gene := range d.Genes {
					for i := d.Starts[k]; i < d.Starts[k+1]; i++ {
						n[gene] += int64(d.N[i])
						q[gene] += int64(d.Q20[i])
					}
				}
				if l != 1 {
					continue
				}
				want := map[[2]int][2]uint32{}
				side := g.TileUm(1)
				for j := 0; j < 2; j++ {
					for i := 0; i < 2; i++ {
						if 2*tx+i >= g.Across(0) || 2*ty+j >= g.Down(0) {
							continue
						}
						pt, err := idx.Points(2*tx+i, 2*ty+j)
						if err != nil {
							t.Fatal(err)
						}
						for k, gene := range pt.Genes {
							for at := pt.Starts[k]; at < pt.Starts[k+1]; at++ {
								bx := clampInt(int(math.Floor((float64(pt.X[at])-float64(tx)*side)*float64(bins)/side)), 0, bins-1)
								by := clampInt(int(math.Floor((float64(pt.Y[at])-float64(ty)*side)*float64(bins)/side)), 0, bins-1)
								for _, gg := range []int{int(gene), genes} {
									c := want[[2]int{gg, by*bins + bx}]
									c[0]++
									if pt.QV[at] >= 20 {
										c[1]++
									}
									want[[2]int{gg, by*bins + bx}] = c
								}
							}
						}
					}
				}
				got := 0
				for k, gene := range d.Genes {
					for i := d.Starts[k]; i < d.Starts[k+1]; i++ {
						got++
						if w := want[[2]int{int(gene), int(d.Bins[i])}]; w[0] != d.N[i] || w[1] != d.Q20[i] {
							t.Fatalf("level 1 tile (%d, %d) gene %d bin %d holds %d/%d, the points %d/%d", tx, ty, gene, d.Bins[i], d.N[i], d.Q20[i], w[0], w[1])
						}
					}
				}
				if got != len(want) {
					t.Fatalf("level 1 tile (%d, %d): %d bins, the points fill %d", tx, ty, got, len(want))
				}
			}
		}
		var total, totalQ int64
		for i, gene := range idx.Summary.Genes {
			if n[i] != gene.Count || q[i] != gene.Q20 {
				t.Errorf("level %d: %s sums to %d (Q20 %d), its count is %d (%d)", l, gene.Name, n[i], q[i], gene.Count, gene.Q20)
			}
			total += gene.Count
			totalQ += gene.Q20
		}
		if n[genes] != total || q[genes] != totalQ || total != idx.Summary.Transcripts || totalQ != idx.Summary.Q20 {
			t.Errorf("level %d: every gene together sums to %d (Q20 %d), the transcripts are %d (%d)", l, n[genes], q[genes], idx.Summary.Transcripts, idx.Summary.Q20)
		}
	}
}

func TestAnEmptyTranscriptIndex(t *testing.T) {
	grid := NewGrid(0.2125, 2000, 1000)
	c := openCache(t, 0)
	m, err := buildSynthTranscripts(t, c, NewTranscriptParams(grid), transcriptInput(nil, nil, 10), context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := OpenTranscripts(c.EntryDir(m.Name))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if pt, err := idx.Points(0, 0); err != nil || pt.Len() != 0 {
		t.Errorf("%v %v", pt, err)
	}
	if d, err := idx.Density(1, 0, 0); err != nil || len(d.Genes) != 0 {
		t.Errorf("%v %v", d, err)
	}
}

// --- Geometry ----------------------------------------------------------------------------------

func synthPolygons(seed uint64, grid Grid, n int) (*xenium.Polygons, map[string]int32) {
	rng := rand.New(rand.NewPCG(seed, 2))
	p := &xenium.Polygons{}
	cells := map[string]int32{}
	w, h := float64(grid.Width)*grid.PixelSize, float64(grid.Height)*grid.PixelSize
	for i := 0; i < n; i++ {
		id := xenium.CellIDString(uint32(i+1), 1)
		if i%17 != 5 {
			cells[id] = int32(i)
		}
		p.CellIDs = append(p.CellIDs, id)
		p.Offsets = append(p.Offsets, int32(len(p.X)))
		cx, cy, r := rng.Float64()*w, rng.Float64()*h, 3+rng.Float64()*6
		k := 6 + rng.IntN(30)
		for v := 0; v < k; v++ {
			a := 2 * math.Pi * float64(v) / float64(k)
			rr := r * (1 + 0.15*rng.NormFloat64())
			p.X, p.Y = append(p.X, float32(cx+rr*math.Cos(a))), append(p.Y, float32(cy+rr*math.Sin(a)))
		}
		if i%4 != 0 { // most rings repeat their first vertex
			p.X, p.Y = append(p.X, p.X[p.Offsets[i]]), append(p.Y, p.Y[p.Offsets[i]])
		}
	}
	p.Offsets = append(p.Offsets, int32(len(p.X)))
	return p, cells
}

func TestGeometryTilesHoldEveryPolygonOncePerLevel(t *testing.T) {
	grid := NewGrid(0.2125, 3000, 2000)
	cellsP, cells := synthPolygons(3, grid, 800)
	nuclei, _ := synthPolygons(4, grid, 700)
	p := NewGeometryParams(grid)
	c := openCache(t, 0)
	b, err := c.begin(Spec{Kind: KindGeometry, Params: p})
	if err != nil {
		t.Fatal(err)
	}
	sum, err := buildGeometry(context.Background(), b, p, GeometryInput{Cells: cellsP, Nuclei: nuclei, CellIndex: cells}, func(float64) {})
	if err != nil {
		t.Fatal(err)
	}
	m, err := b.commit(sum)
	if err != nil {
		t.Fatal(err)
	}
	geo, err := OpenGeometry(c.EntryDir(m.Name))
	if err != nil {
		t.Fatal(err)
	}
	defer geo.Close()
	for si, polys := range []*xenium.Polygons{cellsP, nuclei} {
		set := geo.Summary.Sets[si]
		if set.Polygons != len(polys.CellIDs) || set.UnknownCells != (len(polys.CellIDs)+11)/17 {
			t.Errorf("%s: %d polygons, %d unknown", set.Name, set.Polygons, set.UnknownCells)
		}
		for l := 0; l < grid.Levels; l++ {
			eps := p.epsilon(l)
			seen := map[uint32]int{}
			vertices := 0
			for ty := 0; ty < grid.Down(l); ty++ {
				for tx := 0; tx < grid.Across(l); tx++ {
					tile, err := geo.Tile(set.Name, l, tx, ty)
					if err != nil {
						t.Fatal(err)
					}
					for k, cell := range tile.Cells {
						// The cell's polygon: unknown cells are found by their vertices.
						i := int(cell)
						if cell == NoCell {
							i = findPolygon(polys, tile.XY[2*tile.Starts[k]], tile.XY[2*tile.Starts[k]+1])
						}
						seen[uint32(i)]++
						xs, ys := polys.X[polys.Offsets[i]:polys.Offsets[i+1]], polys.Y[polys.Offsets[i]:polys.Offsets[i+1]]
						got := tile.XY[2*tile.Starts[k] : 2*tile.Starts[k+1]]
						vertices += len(got) / 2
						if l == 0 {
							if len(got) != 2*len(xs) {
								t.Fatalf("level 0 polygon %d has %d vertices, the source %d", i, len(got)/2, len(xs))
							}
							for v := range xs {
								if got[2*v] != xs[v] || got[2*v+1] != ys[v] {
									t.Fatalf("level 0 polygon %d vertex %d differs", i, v)
								}
							}
							continue
						}
						checkSimplified(t, xs, ys, got, eps)
					}
				}
			}
			if len(seen) != len(polys.CellIDs) {
				t.Errorf("%s level %d: %d polygons, want %d", set.Name, l, len(seen), len(polys.CellIDs))
			}
			for i, n := range seen {
				if n != 1 {
					t.Errorf("%s level %d: polygon %d appears %d times", set.Name, l, i, n)
				}
			}
			if vertices != set.Levels[l].Vertices {
				t.Errorf("%s level %d: %d vertices, the summary says %d", set.Name, l, vertices, set.Levels[l].Vertices)
			}
		}
		if set.Levels[grid.Levels-1].Vertices >= set.Levels[0].Vertices {
			t.Errorf("%s: simplification removed no vertices", set.Name)
		}
	}
}

func findPolygon(p *xenium.Polygons, x, y float32) int {
	for i := range p.CellIDs {
		if p.X[p.Offsets[i]] == x && p.Y[p.Offsets[i]] == y {
			return i
		}
	}
	return -1
}

// checkSimplified checks a simplified ring: its vertices are the source's, in order, with the
// first kept and the closing vertex kept if the source had one, at least three distinct, and every
// source vertex within eps of the simplified ring.
func checkSimplified(t *testing.T, xs, ys []float32, got []float32, eps float64) {
	t.Helper()
	n := len(got) / 2
	closed := xs[0] == xs[len(xs)-1] && ys[0] == ys[len(ys)-1]
	if closed && (got[0] != got[2*n-2] || got[1] != got[2*n-1]) {
		t.Fatal("the ring's closing vertex was dropped")
	}
	distinct := n
	if closed {
		distinct--
	}
	srcDistinct := len(xs)
	if closed {
		srcDistinct--
	}
	if distinct < min(3, srcDistinct) {
		t.Fatalf("%d distinct vertices left", distinct)
	}
	at := 0
	for v := 0; v < n; v++ {
		for at < len(xs) && (xs[at] != got[2*v] || ys[at] != got[2*v+1]) {
			at++
		}
		if at == len(xs) {
			t.Fatal("a simplified vertex is not a source vertex in order")
		}
	}
	for v := range xs {
		best := math.Inf(1)
		for k := 0; k < distinct; k++ {
			a, b := k, (k+1)%distinct
			best = math.Min(best, segmentDistance(float64(xs[v]), float64(ys[v]), float64(got[2*a]), float64(got[2*a+1]), float64(got[2*b]), float64(got[2*b+1])))
		}
		if best > eps*(1+1e-9) {
			t.Fatalf("a source vertex is %v from the simplified ring (tolerance %v)", best, eps)
		}
	}
}

func FuzzSimplifyRing(f *testing.F) {
	f.Add([]byte{0, 0, 10, 0, 10, 10, 0, 10, 0, 0}, 1.5)
	f.Add([]byte{0, 0, 1, 1, 2, 2, 3, 3}, 0.1)
	f.Fuzz(func(t *testing.T, raw []byte, eps float64) {
		if len(raw) < 2 || len(raw) > 400 || !(eps > 0) || eps > 1e6 {
			return
		}
		var xs, ys []float32
		for i := 0; i+1 < len(raw); i += 2 {
			xs, ys = append(xs, float32(raw[i])), append(ys, float32(raw[i+1]))
		}
		sx, sy := simplifyRing(xs, ys, eps)
		if len(sx) != len(sy) || len(sx) > len(xs) {
			t.Fatalf("%d and %d vertices from %d", len(sx), len(sy), len(xs))
		}
		got := make([]float32, 0, 2*len(sx))
		for i := range sx {
			got = append(got, sx[i], sy[i])
		}
		if len(xs) > 3 && !(xs[0] == xs[len(xs)-1] && ys[0] == ys[len(ys)-1] && len(xs) <= 4) {
			checkSimplified(t, xs, ys, got, eps)
		}
	})
}

// --- Damaged files -----------------------------------------------------------------------------

// FuzzCacheFiles feeds every reader damaged files: they must fail cleanly, never panic or
// allocate without bound.
func FuzzCacheFiles(f *testing.F) {
	dir := f.TempDir()
	c, err := Open(dir, 0)
	if err != nil {
		f.Fatal(err)
	}
	// Small files (seeds of a kilobyte or so) keep the fuzzer's minimizing fast.
	grid := Grid{PixelSize: 1, Width: 130, Height: 70, Tile: 64, Levels: 3}
	trs, cells := synthTranscripts(1, grid, 40)
	p := NewTranscriptParams(grid)
	p.DensityBins = 4
	b, _ := c.begin(Spec{Kind: KindTranscripts, Params: p})
	if _, err := buildTranscripts(context.Background(), b, p, transcriptInput(trs, cells, 100), func(float64, string) {}); err != nil {
		f.Fatal(err)
	}
	small := Grid{PixelSize: 1, Width: 40, Height: 30, Tile: 16, Levels: 3}
	if _, err := buildImage(context.Background(), b, small, 16, []SourceChannel{synthImage(40, 30, 16, 1)}, func(int, int) {}); err != nil {
		f.Fatal(err)
	}
	polys, pc := synthPolygons(1, grid, 4)
	if _, err := buildGeometry(context.Background(), b, NewGeometryParams(grid), GeometryInput{Cells: polys, CellIndex: pc}, func(float64) {}); err != nil {
		f.Fatal(err)
	}
	for _, name := range []string{pointsFile(0), densityFile(1), imageFileName(0, 1), geometryFile("cells", 1)} {
		raw, err := os.ReadFile(b.path(name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw, uint16(0), byte(0))
	}
	b.release()
	path := filepath.Join(f.TempDir(), "file")
	f.Fuzz(func(t *testing.T, raw []byte, at uint16, flip byte) {
		if len(raw) > 0 {
			raw = append([]byte(nil), raw...)
			raw[int(at)%len(raw)] ^= flip
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		if f, err := openPointsFile(path); err == nil {
			for i := range f.Table {
				if block, e, err := f.block(i); err == nil {
					decodePoints(block, e, 8)
				}
			}
			f.Close()
		}
		if f, err := openDensityFile(path); err == nil {
			for i := range f.Table {
				if block, e, err := f.block(i); err == nil {
					decodeDensity(block, e, 8, int(f.Fields[3]))
				}
			}
			f.Close()
		}
		if f, err := openImageFile(path); err == nil {
			for ty := 0; ty < f.Down; ty++ {
				for tx := 0; tx < f.Across; tx++ {
					f.Tile(tx, ty)
				}
			}
			f.Close()
		}
		if f, err := openGeometryFile(path); err == nil {
			for i := range f.Table {
				if block, e, err := f.block(i); err == nil {
					decodePolygons(block, e)
				}
			}
			f.Close()
		}
	})
}

func TestDamagedFilesAreRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.bin")
	for name, raw := range map[string][]byte{
		"empty":       {},
		"wrong magic": append([]byte("XXXX"), make([]byte, 60)...),
		"wrong version": func() []byte {
			b := append([]byte("SCIT"), make([]byte, 60)...)
			le.PutUint32(b[4:], FormatVersion+1)
			return b
		}(),
		"table past the end": func() []byte {
			b := append([]byte("SCGT"), make([]byte, 28)...)
			le.PutUint32(b[4:], FormatVersion)
			le.PutUint32(b[12:], 100)
			le.PutUint32(b[16:], 100)
			return b
		}(),
	} {
		os.WriteFile(path, raw, 0o644)
		for _, open := range []func(string) (*tableFile, error){openPointsFile, openDensityFile, openGeometryFile} {
			if f, err := open(path); err == nil {
				f.Close()
				t.Errorf("%s: opened", name)
			}
		}
		if f, err := openImageFile(path); err == nil {
			f.Close()
			t.Errorf("%s: opened as an image", name)
		}
	}
	if _, err := inflate([]byte{0x78, 0x01, 0, 0}, 1<<31); err == nil {
		t.Error("a block declaring 2 GB was inflated")
	}
}
