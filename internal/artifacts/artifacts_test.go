package artifacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func stage(t *testing.T, s *Store, id string, files map[string]string) {
	t.Helper()
	dir, err := s.Stage(id)
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func manifest(id string) Manifest {
	return Manifest{ID: id, Kind: "segmentation", Name: "Nucleus expansion 5 µm", Dataset: "d1",
		Lineage: Lineage{Parents: []Parent{{Kind: "dataset", ID: "d1"}}, Method: Method{Name: "nucleus expansion", Version: "1"}, Software: []Software{{Name: "situcraft", Version: "0.2.0"}}},
		Files:   []File{{Path: "cells.bin", Role: "cells"}, {Path: "parts/polygons.bin", Role: "polygons"}}}
}

func TestAnArtifactIsCheckedThenRecorded(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stage(t, s, "s-1", map[string]string{"cells.bin": "cells", "parts/polygons.bin": "polygons"})
	checked := false
	m, err := s.Promote(manifest("s-1"), func(dir string, m *Manifest) []Finding {
		checked = true
		if _, err := os.Stat(filepath.Join(dir, "cells.bin")); err != nil {
			t.Errorf("the check does not see the staged files: %v", err)
		}
		m.Summary = []byte(`{"cells":2}`)
		return []Finding{{Severity: "warning", Code: "cells.small", Message: "two cells", Count: 2}}
	})
	if err != nil || !checked {
		t.Fatal(err)
	}
	if m.State != Complete || m.Files[0].SHA256 != sum([]byte("cells")) || m.Files[0].Size != 5 || len(m.Findings) != 1 || string(m.Summary) != `{"cells":2}` {
		t.Errorf("manifest %+v", m)
	}
	got, err := s.Get("s-1")
	if err != nil || got.State != Complete || got.Format != "situcraft-artifact" || got.Lineage.Method.Name != "nucleus expansion" {
		t.Fatalf("read back: %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), "staging", "s-1")); !os.IsNotExist(err) {
		t.Error("the staging folder is still there")
	}
	if findings, err := s.Verify("s-1", nil); err != nil || len(findings) != 0 {
		t.Errorf("verify: %v %v", findings, err)
	}
	// A changed file is found.
	path, _ := s.Path("s-1", "parts/polygons.bin")
	os.WriteFile(path, []byte("POLYGONS"), 0o644)
	if findings, _ := s.Verify("s-1", nil); len(findings) != 1 || findings[0].Code != "artifact.file-changed" {
		t.Errorf("a changed file: %v", findings)
	}
	list, _ := s.List()
	if len(list) != 1 || list[0].ID != "s-1" {
		t.Errorf("list %+v", list)
	}
}

func TestAFatalFindingMakesItInvalid(t *testing.T) {
	s, _ := Open(t.TempDir())
	stage(t, s, "s-2", map[string]string{"cells.bin": "x", "parts/polygons.bin": "y"})
	m, err := s.Promote(manifest("s-2"), func(string, *Manifest) []Finding {
		return []Finding{{Severity: "fatal", Code: "polygons.open", Message: "a polygon is not closed"}}
	})
	if err != nil || m.State != Invalid {
		t.Fatalf("%+v %v", m, err)
	}
	if got, _ := s.Get("s-2"); got.State != Invalid {
		t.Error("not recorded as invalid")
	}
}

func TestOutputsMustBeListedAndStayInside(t *testing.T) {
	s, _ := Open(t.TempDir())
	stage(t, s, "s-3", map[string]string{"cells.bin": "x", "parts/polygons.bin": "y", "stray.tmp": "z"})
	if _, err := s.Promote(manifest("s-3"), nil); err == nil || !strings.Contains(err.Error(), "stray.tmp") {
		t.Errorf("an unlisted output: %v", err)
	}
	stage(t, s, "s-4", map[string]string{"cells.bin": "x"})
	if _, err := s.Promote(manifest("s-4"), nil); err == nil {
		t.Error("a listed output that is missing was accepted")
	}
	for _, bad := range []string{"../escape", "/abs", "a/../../b", "manifest.json", `a\b`, ""} {
		m := manifest("s-5")
		m.Files = []File{{Path: bad}}
		if _, err := s.Promote(m, nil); err == nil {
			t.Errorf("file path %q accepted", bad)
		}
	}
	for _, bad := range []string{"../x", "staging", "Upper", "", "a/b"} {
		if _, err := s.Stage(bad); err == nil {
			t.Errorf("id %q accepted", bad)
		}
	}
}

// The instrument's segmentation: files of the bundle, recorded by SHA-256, never copied.
func TestBundleFilesAreReferencedNotCopied(t *testing.T) {
	s, _ := Open(t.TempDir())
	bundle := map[string][]byte{"cells.parquet": []byte("PAR1 cells"), "cell_boundaries.parquet": []byte("PAR1 boundaries")}
	m := Manifest{ID: "s-instrument", Kind: "segmentation", Name: "Instrument", Lineage: Lineage{Method: Method{Name: "Xenium Onboard Analysis"}}}
	for _, name := range []string{"cells.parquet", "cell_boundaries.parquet"} {
		m.Files = append(m.Files, File{Path: name, Role: name, Size: int64(len(bundle[name])), SHA256: sum(bundle[name]), Location: InBundle})
	}
	got, err := s.Promote(m, nil)
	if err != nil || got.State != Complete {
		t.Fatalf("%+v %v", got, err)
	}
	entries, _ := os.ReadDir(filepath.Join(s.Dir(), "s-instrument"))
	if len(entries) != 1 || entries[0].Name() != "manifest.json" {
		t.Errorf("the artifact's folder holds %v", entries)
	}
	open := func(path string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(bundle[path])), nil }
	if findings, _ := s.Verify("s-instrument", open); len(findings) != 0 {
		t.Errorf("verify: %v", findings)
	}
	bundle["cells.parquet"] = []byte("PAR1 changed")
	if findings, _ := s.Verify("s-instrument", open); len(findings) != 1 {
		t.Errorf("a changed bundle file: %v", findings)
	}
	// Registering it again is the same artifact; with other files, it is refused.
	if again, err := s.Promote(m, nil); err != nil || !again.Created.Equal(got.Created) {
		t.Errorf("registering again: %v", err)
	}
	m.Files[0].SHA256 = sum([]byte("other"))
	if _, err := s.Promote(m, nil); err == nil {
		t.Error("an artifact was replaced")
	}
	m.Files[0].SHA256 = ""
	if _, err := s.Promote(Manifest{ID: "s-x", Kind: "segmentation", Name: "x", Files: []File{{Path: "a", Location: InBundle}}}, nil); err == nil {
		t.Error("a bundle file without its checksum was accepted")
	}
}

func TestDiscardedOutputsLeaveNothing(t *testing.T) {
	s, _ := Open(t.TempDir())
	stage(t, s, "s-6", map[string]string{"cells.bin": "x"})
	if err := s.Discard("s-6"); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(s.Dir(), "staging")); len(entries) != 0 {
		t.Errorf("staging holds %v", entries)
	}
	if _, err := s.Get("s-6"); err != ErrNotFound {
		t.Errorf("get: %v", err)
	}
}
