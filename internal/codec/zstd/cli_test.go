// Round trips through the zstd command-line tool, when it is installed: a few hundred inputs of
// random kinds and sizes, compressed with random levels and options, must decode to exactly the
// input. The committed fixtures cover the same ground without the tool.

package zstd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

var (
	zstdOnce sync.Once
	zstdPath string
)

// zstdCLI returns the path of the zstd tool, or skips the test.
func zstdCLI(t testing.TB) string {
	zstdOnce.Do(func() {
		zstdPath, _ = exec.LookPath("zstd")
	})
	if zstdPath == "" {
		t.Skip("the zstd command-line tool is not installed")
	}
	return zstdPath
}

// cliCompress compresses input with the zstd tool. From a pipe the frame has no content size;
// from a file it has one.
func cliCompress(t testing.TB, input []byte, args []string, stdin bool) []byte {
	t.Helper()
	exe := zstdCLI(t)
	full := append([]string{"-q", "-c"}, args...)
	var cmd *exec.Cmd
	if stdin {
		cmd = exec.Command(exe, full...)
		cmd.Stdin = bytes.NewReader(input)
	} else {
		dir := t.TempDir()
		path := filepath.Join(dir, "input")
		if err := os.WriteFile(path, input, 0o600); err != nil {
			t.Fatal(err)
		}
		cmd = exec.Command(exe, append(full, path)...)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("zstd %v: %v: %s", args, err, stderr.String())
	}
	return out
}

type cliCase struct {
	gen   string
	seed  uint64
	size  int
	args  []string
	stdin bool
}

var generators = map[string]func(uint64, int) []byte{
	"text":    genText,
	"floats":  genFloats,
	"random":  genRandom,
	"runs":    genRuns,
	"repeats": genRepeats,
	"mixed":   genMixed,
}

func cliCases(n int) []cliCase {
	r := rng{20261013}
	names := []string{"text", "floats", "random", "runs", "repeats", "mixed"}
	cases := make([]cliCase, n)
	for i := range cases {
		c := cliCase{gen: names[r.intn(len(names))], seed: r.next()}
		switch r.intn(4) {
		case 0:
			c.size = r.intn(300)
		case 1:
			c.size = r.intn(20 << 10)
		default:
			c.size = r.intn(600 << 10)
		}
		switch k := r.intn(20); {
		case k == 0:
			c.args = []string{"--fast=" + strconv.Itoa(1+r.intn(20))}
		case k == 1:
			c.args = []string{"--ultra", "-" + strconv.Itoa(20+r.intn(3))}
		default:
			c.args = []string{"-" + strconv.Itoa(1+r.intn(19))}
		}
		switch r.intn(8) {
		case 0:
			c.args = append(c.args, "--no-check")
		case 1:
			c.args = append(c.args, "--zstd=wlog="+strconv.Itoa(10+r.intn(8)))
		case 2:
			c.args = append(c.args, "--long=24")
		case 3:
			c.args = append(c.args, "--zstd=strategy="+strconv.Itoa(1+r.intn(9)))
		}
		c.stdin = r.intn(3) == 0
		cases[i] = c
	}
	return cases
}

func TestCLIRoundTrip(t *testing.T) {
	zstdCLI(t)
	n := 300
	if testing.Short() {
		n = 40
	}
	var mu sync.Mutex
	var total stats
	t.Run("cases", func(t *testing.T) {
		for i, c := range cliCases(n) {
			t.Run(fmt.Sprintf("%03d-%s-%d%v", i, c.gen, c.size, c.args), func(t *testing.T) {
				t.Parallel()
				input := generators[c.gen](c.seed, c.size)
				comp := cliCompress(t, input, c.args, c.stdin)
				got, st, err := decodeWithStats(comp, 0)
				if err != nil {
					t.Fatalf("Decode: %v", err)
				}
				mu.Lock()
				total.add(st)
				mu.Unlock()
				if !bytes.Equal(got, input) {
					t.Fatalf("output differs from the input (%d bytes, want %d) at byte %d", len(got), len(input), firstDiff(got, input))
				}
				size, ok := FrameContentSize(comp)
				if c.stdin && ok {
					t.Errorf("FrameContentSize from a pipe = %d, true; want unknown", size)
				}
				if !c.stdin && (!ok || size != uint64(len(input))) {
					t.Errorf("FrameContentSize = %d, %v; want %d", size, ok, len(input))
				}
			})
		}
	})
	t.Logf("features met: %+v", total)
}

func firstDiff(a, b []byte) int {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}
