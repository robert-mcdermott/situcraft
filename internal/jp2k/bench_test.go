// Benchmarks: decoding a real 1024×1024 Xenium tile (9/7 irreversible, 16-bit) on one core and on
// all cores. The budget is 150 ms per tile on one core (roadmap, wave 1 slice 2).

package jp2k

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// benchTile returns the largest (most detailed) full-resolution tile of the Lung morphology_focus
// image, a 1024×1024 9/7 irreversible codestream.
func benchTile(b *testing.B) []byte {
	tiles := realTiles(b, realFiles[0])
	if len(tiles) < 24 {
		b.Skip("the Lung bundle is not present (node validation/fetch.mjs xenium-lung-2fov)")
	}
	var best []byte
	for _, tl := range tiles[:24] {
		if len(tl.data) > len(best) {
			best = tl.data
		}
	}
	return best
}

func benchmarkTile(b *testing.B, workers int) {
	data := benchTile(b)
	b.SetBytes(1024 * 1024 * 2)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, _, err := DecodeGray16(data, Options{Workers: workers}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkXeniumTile1Worker(b *testing.B) { benchmarkTile(b, 1) }

func BenchmarkXeniumTileAllWorkers(b *testing.B) { benchmarkTile(b, runtime.GOMAXPROCS(0)) }

// BenchmarkXeniumProfileFixture decodes the committed 256×256 synthetic fixture with the Xenium
// coding parameters, for machines without the development data.
func BenchmarkXeniumProfileFixture(b *testing.B) {
	data, err := os.ReadFile(filepath.Join(fixtureDir, "xenium-profile.jp2"))
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(256 * 256 * 2)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, _, err := DecodeGray16(data, Options{Workers: 1}); err != nil {
			b.Fatal(err)
		}
	}
}
