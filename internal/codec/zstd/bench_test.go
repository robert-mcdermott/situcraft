// Decoding speed on 64 MiB of content like SituCraft's (float32 columns, with text), compressed
// by the zstd tool at levels 1, 3 and 19. Needs the tool; reports output MB/s.

package zstd

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	benchOnce  sync.Once
	benchInput []byte
)

func benchContent() []byte {
	benchOnce.Do(func() { benchInput = realistic(1, 64<<20) })
	return benchInput
}

// benchCompress compresses with all cores; the frame format is the same as single-threaded.
func benchCompress(b *testing.B, input []byte, level string) []byte {
	exe := zstdCLI(b)
	dir := b.TempDir()
	path := filepath.Join(dir, "input")
	if err := os.WriteFile(path, input, 0o600); err != nil {
		b.Fatal(err)
	}
	out, err := exec.Command(exe, "-q", "-c", "-T0", "-"+level, path).Output()
	if err != nil {
		b.Fatalf("zstd -%s: %v", level, err)
	}
	return out
}

func BenchmarkDecode(b *testing.B) {
	zstdCLI(b)
	input := benchContent()
	for _, level := range []string{"1", "3", "19"} {
		b.Run("level"+level, func(b *testing.B) {
			comp := benchCompress(b, input, level)
			dst := make([]byte, 0, len(input))
			b.SetBytes(int64(len(input)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				out, err := Decode(dst[:0], comp, 0)
				if err != nil || len(out) != len(input) {
					b.Fatalf("Decode: %v", err)
				}
			}
			b.ReportMetric(float64(len(input))/float64(len(comp)), "ratio")
		})
	}
	// A fresh output buffer each time, as a caller that does not reuse one sees it.
	b.Run("level3-fresh", func(b *testing.B) {
		comp := benchCompress(b, input, "3")
		b.SetBytes(int64(len(input)))
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := Decode(nil, comp, 0); err != nil {
				b.Fatal(err)
			}
		}
	})
}
