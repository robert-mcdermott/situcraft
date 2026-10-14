package examples

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A small "bundle" zipped (in a folder, as some publishers do), served by a test server, and its
// sources record.
func fixture(t *testing.T, mutate func(name string, data []byte) (string, []byte)) (Source, *httptest.Server) {
	t.Helper()
	files := map[string][]byte{
		"experiment.xenium":      []byte(`{"run_name":"run"}`),
		"cells.parquet":          bytes.Repeat([]byte("cells"), 200),
		"morphology_focus/a.tif": bytes.Repeat([]byte{1, 2, 3}, 300),
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	s := Source{Name: "tiny", Title: "A tiny bundle", License: "CC BY 4.0", Source: "a test"}
	for _, name := range []string{"experiment.xenium", "cells.parquet", "morphology_focus/a.tif"} {
		data := files[name]
		sum := sha256.Sum256(data)
		s.Files = append(s.Files, File{Path: name, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])})
		zipName, zipData := "outs/"+name, data
		if mutate != nil {
			zipName, zipData = mutate(zipName, data)
		}
		w, _ := zw.Create(zipName)
		w.Write(zipData)
	}
	w, _ := zw.Create("outs/notes.txt") // not part of the bundle: skipped
	w.Write([]byte("hello"))
	zw.Close()
	raw := buf.Bytes()
	md := md5.Sum(raw)
	sh := sha256.Sum256(raw)
	s.Archive.Size, s.Archive.MD5, s.Archive.SHA256 = int64(len(raw)), hex.EncodeToString(md[:]), hex.EncodeToString(sh[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "SituCraft-test" {
			t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
		}
		w.Write(raw)
	}))
	t.Cleanup(server.Close)
	s.Archive.URL = server.URL + "/tiny.zip"
	return s, server
}

func TestAnExampleDownloadsChecksAndExtracts(t *testing.T) {
	s, _ := fixture(t, nil)
	dir := t.TempDir()
	lib := &Library{Dir: dir, Sources: []Source{s}, UserAgent: "SituCraft-test"}
	if st := lib.List(); len(st) != 1 || st[0].State != "absent" || st[0].Download != s.Archive.Size || st[0].ManifestSHA256 != s.Files[0].SHA256 {
		t.Fatalf("before: %+v", st)
	}
	var last float64
	got, err := lib.Download(context.Background(), "tiny", func(f float64, _ string) {
		if f < last {
			t.Errorf("progress went back from %v to %v", last, f)
		}
		last = f
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "tiny") || last != 1 {
		t.Errorf("at %s, progress %v", got, last)
	}
	if st := lib.List(); st[0].State != "ready" || st[0].Path != got {
		t.Errorf("after: %+v", st)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the examples folder holds %d entries: the archive was not deleted", len(entries))
	}
	if _, err := os.Stat(filepath.Join(got, "notes.txt")); err == nil {
		t.Error("a file not of the bundle was extracted")
	}
	if _, err := os.Stat(filepath.Join(got, ".situcraft-example.json")); err != nil {
		t.Error("no record of the check")
	}
	if err := lib.Remove("tiny"); err != nil || lib.List()[0].State != "absent" {
		t.Errorf("removing: %v", err)
	}
}

func TestDamagedOrHostileArchivesAreRefused(t *testing.T) {
	cases := map[string]func(Source, *httptest.Server) Source{
		"wrong MD5": func(s Source, _ *httptest.Server) Source {
			s.Archive.MD5 = strings.Repeat("0", 32)
			return s
		},
		"wrong size": func(s Source, _ *httptest.Server) Source {
			s.Archive.Size--
			return s
		},
		"a file's SHA-256": func(s Source, _ *httptest.Server) Source {
			s.Files[1].SHA256 = strings.Repeat("0", 64)
			return s
		},
		"a missing file": func(s Source, _ *httptest.Server) Source {
			s.Files = append(s.Files, File{Path: "transcripts.parquet", Size: 1, SHA256: "00"})
			return s
		},
		"not found": func(s Source, server *httptest.Server) Source {
			s.Archive.URL = server.URL + "/missing"
			return s
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s, server := fixture(t, nil)
			if name == "not found" {
				server.Config.Handler = http.NotFoundHandler()
			}
			s = change(s, server)
			dir := t.TempDir()
			lib := &Library{Dir: dir, Sources: []Source{s}, UserAgent: "SituCraft-test"}
			if _, err := lib.Download(context.Background(), "tiny", nil); err == nil {
				t.Fatal("accepted")
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("left %d entries behind", len(entries))
			}
		})
	}
	// A member whose name leaves the folder.
	s, _ := fixture(t, func(name string, data []byte) (string, []byte) {
		if strings.HasSuffix(name, "cells.parquet") {
			return "../cells.parquet", data
		}
		return name, data
	})
	// The publisher's checksums are of the hostile archive itself; its member names still refuse it.
	lib := &Library{Dir: t.TempDir(), Sources: []Source{s}, UserAgent: "SituCraft-test"}
	if _, err := lib.Download(context.Background(), "tiny", nil); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("a member leaving the folder: %v", err)
	}
	// Too little free space.
	s, _ = fixture(t, nil)
	lib = &Library{Dir: t.TempDir(), Sources: []Source{s}, UserAgent: "SituCraft-test", Free: func(string) (int64, error) { return 10, nil }}
	if _, err := lib.Download(context.Background(), "tiny", nil); err == nil || !strings.Contains(err.Error(), "free") {
		t.Errorf("without room: %v", err)
	}
	// Cancelled.
	s, _ = fixture(t, nil)
	dir := t.TempDir()
	lib = &Library{Dir: dir, Sources: []Source{s}, UserAgent: "SituCraft-test"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lib.Download(ctx, "tiny", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a cancelled download left %d entries", len(entries))
	}
}

// The program's examples are the validation suite's sources.
func TestTheSourcesParse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "validation", "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	sources, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].Name != "xenium-lung-2fov" || sources[1].Name != "xenium-breast-2fov" || !strings.HasPrefix(sources[0].Archive.URL, "https://") || sources[0].License == "" || sources[0].Summary == "" || sources[0].LicenseURL == "" || sources[0].Publisher == "" || !strings.HasPrefix(sources[0].Page, "https://") {
		t.Errorf("sources %+v", sources)
	}
}
