// SituCraft's readers on the development bundles, against independent readers: every Parquet
// column as DuckDB reads it, every Zarr array as zarr-python and numcodecs read it, and every
// TIFF's structure, and the pixels of each image plane's smallest (lossless) pyramid level, as
// tifffile and imagecodecs read them (validation/reference/xenium-bundles.json, made by
// generate_bundles.py). Every image tile is decoded, and the time taken reported; internal/jp2k's
// tests compare each tile's pixels with OpenJPEG.
//
// The bundles are fetched by node validation/fetch.mjs into validation/cache/ (or $SITUCRAFT_DATA);
// a missing one is skipped, unless SITUCRAFT_REQUIRE_DATA names it (a comma-separated list of data
// sets, or 1 for all): CI requires the Lung bundle for each commit, the weekly run both.
package validation

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"situcraft/internal/parquet"
	"situcraft/internal/tiff"
	"situcraft/internal/zarr"
)

type reference struct {
	Bundles map[string]struct {
		Parquet map[string]struct {
			Rows    int64 `json:"rows"`
			Columns []struct {
				Name, Physical, SHA256 string
				Nulls                  int
			} `json:"columns"`
		} `json:"parquet"`
		Zarr map[string][]struct {
			Path, DType, SHA256 string
			Shape               []int
		} `json:"zarr"`
		TIFF map[string]struct {
			Pages [][]struct {
				Width, Height, Tiles, Compression, Bits int
			} `json:"pages"`
			OME      bool     `json:"ome"`
			Smallest []string `json:"smallest_level_sha256"`
		} `json:"tiff"`
	} `json:"bundles"`
}

func dataRoot() string {
	if root := os.Getenv("SITUCRAFT_DATA"); root != "" {
		return root
	}
	return "cache"
}

func loadReference(t *testing.T) reference {
	raw, err := os.ReadFile(filepath.Join("reference", "xenium-bundles.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ref reference
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	return ref
}

// bundle is the folder of a development bundle, or "" (the test skipped) when it is absent.
func bundle(t *testing.T, name string) string {
	dir := filepath.Join(dataRoot(), name)
	if _, err := os.Stat(filepath.Join(dir, "experiment.xenium")); err != nil {
		if required(name) {
			t.Fatalf("%s is missing (node validation/fetch.mjs %s)", dir, name)
		}
		t.Skipf("%s is missing (node validation/fetch.mjs %s)", dir, name)
	}
	return dir
}

// required reports whether SITUCRAFT_REQUIRE_DATA names this data set (or is 1).
func required(name string) bool {
	want := os.Getenv("SITUCRAFT_REQUIRE_DATA")
	if want == "1" {
		return true
	}
	for _, n := range strings.Split(want, ",") {
		if strings.TrimSpace(n) == name {
			return true
		}
	}
	return false
}

// columnDigest is generate_bundles.py's digest of a column, from SituCraft's reading of it.
func columnDigest(col parquet.Column, v *parquet.Values) (string, int) {
	h := sha256.New()
	nulls := 0
	var buf [8]byte
	for i := 0; i < v.Len; i++ {
		if v.Valid != nil && !v.Valid[i] {
			h.Write([]byte{0})
			nulls++
			continue
		}
		h.Write([]byte{1})
		switch col.Type {
		case parquet.Boolean:
			if v.Bool[i] {
				h.Write([]byte{1})
			} else {
				h.Write([]byte{0})
			}
		case parquet.Int32:
			binary.LittleEndian.PutUint32(buf[:], uint32(v.Int32[i]))
			h.Write(buf[:4])
		case parquet.Int64:
			binary.LittleEndian.PutUint64(buf[:], uint64(v.Int64[i]))
			h.Write(buf[:8])
		case parquet.Float:
			binary.LittleEndian.PutUint32(buf[:], math.Float32bits(v.Float32[i]))
			h.Write(buf[:4])
		case parquet.Double:
			binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v.Float64[i]))
			h.Write(buf[:8])
		default:
			binary.LittleEndian.PutUint32(buf[:], uint32(len(v.Bytes[i])))
			h.Write(buf[:4])
			h.Write(v.Bytes[i])
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nulls
}

func TestReadersMatchIndependentReaders(t *testing.T) {
	ref := loadReference(t)
	for _, name := range []string{"xenium-lung-2fov", "xenium-breast-2fov"} {
		want := ref.Bundles[name]
		t.Run(name, func(t *testing.T) {
			dir := bundle(t, name)
			var columns, rows, arrays, levels, tiles int64
			var decodeTime time.Duration
			for file, pq := range want.Parquet {
				f, err := os.Open(filepath.Join(dir, file))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				info, _ := f.Stat()
				p, err := parquet.Open(f, info.Size())
				if err != nil {
					t.Fatalf("%s: %v", file, err)
				}
				if p.NumRows != pq.Rows || len(p.Columns) != len(pq.Columns) {
					t.Fatalf("%s: %d rows and %d columns, DuckDB %d and %d", file, p.NumRows, len(p.Columns), pq.Rows, len(pq.Columns))
				}
				for c, wantCol := range pq.Columns {
					col := p.Columns[c]
					v, err := p.ReadAll(c)
					if err != nil {
						t.Fatalf("%s %s: %v", file, col.Name, err)
					}
					digest, nulls := columnDigest(col, v)
					if col.Name != wantCol.Name || col.Type.String() != wantCol.Physical || digest != wantCol.SHA256 || nulls != wantCol.Nulls {
						t.Errorf("%s %s (%v): %d nulls, digest %s; DuckDB %s %s: %d nulls, %s", file, col.Name, col.Type, nulls, digest, wantCol.Name, wantCol.Physical, wantCol.Nulls, wantCol.SHA256)
					}
					columns++
				}
				rows += p.NumRows
			}
			for file, wantArrays := range want.Zarr {
				f, err := os.Open(filepath.Join(dir, file))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				info, _ := f.Stat()
				store, err := zarr.OpenZip(f, info.Size())
				if err != nil {
					t.Fatalf("%s: %v", file, err)
				}
				for _, wantArray := range wantArrays {
					a, err := zarr.OpenArray(store, wantArray.Path)
					if err != nil {
						t.Fatalf("%s %s: %v", file, wantArray.Path, err)
					}
					data, err := a.ReadAll()
					if err != nil {
						t.Fatalf("%s %s: %v", file, wantArray.Path, err)
					}
					sum := sha256.Sum256(data)
					if hex.EncodeToString(sum[:]) != wantArray.SHA256 || strings.Trim(a.DType.String(), "<>|") != strings.Trim(wantArray.DType, "<>|") {
						t.Errorf("%s %s (%s %v, %s): values differ from zarr-python's", file, wantArray.Path, a.DType, a.Shape, a.Codec)
					}
					arrays++
				}
			}
			for file, wantTIFF := range want.TIFF {
				f, err := os.Open(filepath.Join(dir, file))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				info, _ := f.Stat()
				tf, err := tiff.Open(f, info.Size())
				if err != nil {
					t.Fatalf("%s: %v", file, err)
				}
				if len(tf.IFDs) != len(wantTIFF.Pages) {
					t.Fatalf("%s: %d images, tifffile %d", file, len(tf.IFDs), len(wantTIFF.Pages))
				}
				for p, ifd := range tf.IFDs {
					got := append([]*tiff.IFD{ifd}, ifd.SubIFDs...)
					if len(got) != len(wantTIFF.Pages[p]) {
						t.Fatalf("%s image %d: %d levels, tifffile %d", file, p, len(got), len(wantTIFF.Pages[p]))
					}
					for l, level := range got {
						w := wantTIFF.Pages[p][l]
						if level.Width != w.Width || level.Height != w.Height || len(level.Offsets) != w.Tiles || level.Compression != w.Compression || level.BitsPerSample[0] != w.Bits {
							t.Errorf("%s image %d level %d: %d×%d, %d tiles, compression %d; tifffile %d×%d, %d, %d", file, p, l, level.Width, level.Height, len(level.Offsets), level.Compression, w.Width, w.Height, w.Tiles, w.Compression)
						}
						levels++
					}
				}
				if wantTIFF.OME {
					if _, err := tiff.ParseOME(tf.IFDs[0].Description); err != nil {
						t.Errorf("%s: %v", file, err)
					}
				}
				for p, ifd := range tf.IFDs {
					smallest := ifd
					if n := len(ifd.SubIFDs); n > 0 {
						smallest = ifd.SubIFDs[n-1]
					}
					pixels, err := smallest.ReadImage()
					if err != nil {
						t.Fatalf("%s image %d, smallest level: %v", file, p, err)
					}
					sum := sha256.Sum256(pixels)
					if hex.EncodeToString(sum[:]) != wantTIFF.Smallest[p] {
						t.Errorf("%s image %d: the smallest level's pixels differ from tifffile's", file, p)
					}
				}
				n, elapsed := decodeEveryTile(t, file, tf)
				tiles += n
				decodeTime += elapsed
			}
			t.Logf("%s: %d Parquet columns over %d rows, %d Zarr arrays and %d image levels, all as the independent readers read them", name, columns, rows, arrays, levels)
			if tiles > 0 {
				t.Logf("%s: %d image tiles decoded in %v on %d cores (%.1f ms per tile per core)", name, tiles, decodeTime.Round(time.Millisecond), runtime.GOMAXPROCS(0), float64(decodeTime.Milliseconds())*float64(runtime.GOMAXPROCS(0))/float64(tiles))
			}
		})
	}
}

// decodeEveryTile decodes every tile of every image and pyramid level of a TIFF, a tile per core
// at a time, and returns how many and how long it took (none in -short mode: under the race
// detector it takes minutes).
func decodeEveryTile(t *testing.T, file string, tf *tiff.File) (int64, time.Duration) {
	if testing.Short() {
		return 0, 0
	}
	type job struct {
		ifd   *tiff.IFD
		index int
	}
	var jobs []job
	for _, ifd := range tf.IFDs {
		for _, level := range append([]*tiff.IFD{ifd}, ifd.SubIFDs...) {
			for i := range level.Offsets {
				jobs = append(jobs, job{level, i})
			}
		}
	}
	start := time.Now()
	work := make(chan job)
	var wg sync.WaitGroup
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range work {
				block, err := j.ifd.DecodeBlock(j.index)
				if err != nil {
					t.Errorf("%s: %v", file, err)
					continue
				}
				width, height := j.ifd.BlockSize(j.index)
				if len(block) != j.ifd.BlockBytes(width, height) {
					t.Errorf("%s: tile %d decodes to %d bytes", file, j.index, len(block))
				}
			}
		}()
	}
	for _, j := range jobs {
		work <- j
	}
	close(work)
	wg.Wait()
	return int64(len(jobs)), time.Since(start)
}
