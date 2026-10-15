package simulate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// digest is a SHA-256 of a folder's every file, by name.
func digest(t *testing.T, dir string) (string, map[string]string) {
	t.Helper()
	files := map[string]string{}
	var names []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		sum := sha256.Sum256(raw)
		files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n + " " + files[n] + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))[:16], files
}

// pinnedSmall is the small preset's bundle (written by version "test"), the same on every
// processor: a change here is a change to what the simulator makes, which must be deliberate.
const pinnedSmall = "6afe2a7c5786ea5e"

// The same parameters give the same bundle, byte for byte, with any number of workers and on any
// processor.
func TestBundlesAreReproducible(t *testing.T) {
	folder, _ := smallBundle(t)
	want, wantFiles := digest(t, folder)
	again := filepath.Join(t.TempDir(), "again")
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	if _, err := Run(context.Background(), Presets["small"], again, "test", nil); err != nil {
		t.Fatal(err)
	}
	got, gotFiles := digest(t, again)
	if got != want {
		for name, sum := range wantFiles {
			if gotFiles[name] != sum {
				t.Errorf("%s differs on one worker", name)
			}
		}
	}
	if want != pinnedSmall {
		t.Errorf("the small bundle's digest is %s, not %s (on %s)", want, pinnedSmall, runtime.GOARCH)
	}
}
