// Real data: every JPEG 2000 tile of the development bundles' morphology images, compared with
// OpenJPEG. A bundle that is absent is left out, unless SITUCRAFT_REQUIRE_DATA names it (a
// comma-separated list of data sets, or 1 for all), as validation/readers_test.go does; with no
// bundle, or no opj_decompress, the tests are skipped.

package jp2k

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// cacheDir is where validation/fetch.mjs puts the development bundles.
var cacheDir = func() string {
	if dir := os.Getenv("SITUCRAFT_DATA"); dir != "" {
		return dir
	}
	return filepath.Join("..", "..", "validation", "cache")
}()

var realFiles = []string{
	"xenium-lung-2fov/morphology_focus/morphology_focus_0000.ome.tif",
	"xenium-breast-2fov/morphology_focus/morphology_focus_0000.ome.tif",
	"xenium-breast-2fov/morphology_focus/morphology_focus_0001.ome.tif",
	"xenium-breast-2fov/morphology_focus/morphology_focus_0002.ome.tif",
	"xenium-breast-2fov/morphology_focus/morphology_focus_0003.ome.tif",
}

// zstackFiles hold every z-plane; a sample of their tiles is compared.
var zstackFiles = []string{
	"xenium-lung-2fov/morphology.ome.tif",
	"xenium-breast-2fov/morphology.ome.tif",
}

// requireData reports whether any data set is required.
func requireData() bool { return os.Getenv("SITUCRAFT_REQUIRE_DATA") != "" }

// required reports whether SITUCRAFT_REQUIRE_DATA names the data set holding rel (or is 1).
func required(rel string) bool {
	want := os.Getenv("SITUCRAFT_REQUIRE_DATA")
	if want == "1" {
		return true
	}
	set := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
	for _, name := range strings.Split(want, ",") {
		if strings.TrimSpace(name) == set {
			return true
		}
	}
	return false
}

// realTiles loads the tiles of a development file: none when it is absent (a failure when its
// data set is required).
func realTiles(t testing.TB, rel string) []tiffTile {
	t.Helper()
	path := filepath.Join(cacheDir, rel)
	if _, err := os.Stat(path); err != nil {
		if required(rel) {
			t.Fatalf("development file %s is missing: %v", rel, err)
		}
		return nil
	}
	tiles, err := tiffTiles(path)
	if err != nil {
		t.Fatal(err)
	}
	return tiles
}

func needOPJ(t testing.TB) {
	t.Helper()
	if opjDecompress == "" {
		if requireData() {
			t.Fatal("opj_decompress is not on PATH")
		}
		t.Skip("opj_decompress is not on PATH")
	}
}

// TestRealTiles decodes every tile of every IFD and SubIFD of the morphology_focus files (and every
// 25th tile of the z-stacks, or all with JP2K_ZSTACK_ALL=1) and compares each with opj_decompress: within ±1 with the standard's
// 9/7 constants, and bit for bit with OpenJPEG's approximate high-pass constant (see invK97OPJ),
// which shows that the ±1 differences come from that constant alone.
func TestRealTiles(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	needOPJ(t)
	type job struct {
		file string
		tile tiffTile
	}
	var jobs []job
	for _, f := range realFiles {
		for _, tl := range realTiles(t, f) {
			jobs = append(jobs, job{f, tl})
		}
	}
	step := 25 // JP2K_ZSTACK_ALL=1 compares every z-stack tile
	if os.Getenv("JP2K_ZSTACK_ALL") == "1" {
		step = 1
	}
	for _, f := range zstackFiles {
		tiles := realTiles(t, f)
		for i := 0; i < len(tiles); i += step {
			jobs = append(jobs, job{f, tiles[i]})
		}
	}
	if len(jobs) == 0 {
		t.Skip("no development bundle is present (node validation/fetch.mjs)")
	}
	var mu sync.Mutex
	var total, totalEmu diffStats
	reversible := 0
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			ref, err := opjDecode(t, j.tile.data, 0)
			if err != nil {
				t.Errorf("%s %s: %v", j.file, j.tile.label, err)
				return
			}
			img, err := Decode(j.tile.data, Options{Workers: 1})
			if err != nil {
				t.Errorf("%s %s: %v", j.file, j.tile.label, err)
				return
			}
			s, err := compareRef(img, ref)
			if err != nil {
				t.Errorf("%s %s: %v", j.file, j.tile.label, err)
				return
			}
			if s.maxAbs > 1 {
				t.Errorf("%s %s: %v", j.file, j.tile.label, s)
			}
			emu, err := Decode(j.tile.data, Options{Workers: 1, opjHighpass: true})
			if err != nil {
				t.Errorf("%s %s: %v", j.file, j.tile.label, err)
				return
			}
			se, _ := compareRef(emu, ref)
			if se.differing > 0 {
				t.Errorf("%s %s: with OpenJPEG's high-pass constant: %v", j.file, j.tile.label, se)
			}
			d, _ := newDecoder(mustCS(j.tile.data), Options{})
			pix, w, h, err := DecodeGray16(j.tile.data, Options{})
			if err != nil {
				t.Errorf("%s %s: DecodeGray16: %v", j.file, j.tile.label, err)
				return
			}
			if w != img.Components[0].Width || h != img.Components[0].Height {
				t.Errorf("%s %s: DecodeGray16 size %d×%d", j.file, j.tile.label, w, h)
				return
			}
			for i, v := range pix {
				if int32(v) != img.Components[0].Data[i] {
					t.Errorf("%s %s: DecodeGray16 differs from Decode at sample %d", j.file, j.tile.label, i)
					break
				}
			}
			mu.Lock()
			total.merge(s)
			totalEmu.merge(se)
			if d.main.cod.comp.reversible {
				reversible++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	t.Logf("%d tiles (%d reversible 5/3, %d irreversible 9/7)", len(jobs), reversible, len(jobs)-reversible)
	t.Logf("standard decode vs OpenJPEG: %v", total)
	t.Logf("with OpenJPEG's high-pass constant: %v", totalEmu)
}

// TestRealTilesReduced checks resolution reduction on a few real tiles.
func TestRealTilesReduced(t *testing.T) {
	needOPJ(t)
	tiles := realTiles(t, realFiles[0])
	if len(tiles) == 0 {
		t.Skip("the Lung bundle is not present (node validation/fetch.mjs xenium-lung-2fov)")
	}
	for _, r := range []int{1, 3, 5} {
		for _, i := range []int{0, 7, len(tiles) - 1} {
			tl := tiles[i]
			ref, err := opjDecode(t, tl.data, r)
			if err != nil {
				t.Fatal(err)
			}
			img, err := Decode(tl.data, Options{Reduce: r})
			if err != nil {
				t.Fatalf("%s reduce %d: %v", tl.label, r, err)
			}
			s, err := compareRef(img, ref)
			if err != nil {
				t.Fatalf("%s reduce %d: %v", tl.label, r, err)
			}
			if s.maxAbs > 1 {
				t.Errorf("%s reduce %d: %v", tl.label, r, s)
			}
			t.Logf("%s reduce %s: %v", tl.label, strconv.Itoa(r), s)
		}
	}
}
