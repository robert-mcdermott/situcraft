package zarr

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stores(t testing.TB) map[string]Store {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "v2.zarr.zip"))
	if err != nil {
		t.Fatal(err)
	}
	zipped, err := OpenZip(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]Store{
		"v2.zarr":     OpenDir(filepath.Join("testdata", "v2.zarr")),
		"v2.zarr.zip": zipped,
		"v3.zarr":     OpenDir(filepath.Join("testdata", "v3.zarr")),
	}
}

// Stores written by zarr-python 3 with numcodecs (testdata/make_fixtures.py): format 2 in a
// directory and a zip, format 3; Blosc (LZ4, Zstd, bit shuffle), Zstd, zlib, gzip, numcodecs' LZ4,
// uncompressed; F order, big-endian types, three dimensions, "/" chunk keys, booleans, text and
// bytes, chunks never written, CRC-32C and transposition; every value compared by SHA-256.
func TestStoresFromZarrPython(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Arrays []struct {
			Store, Path, DType, SHA256 string
			Shape                      []int
		} `json:"arrays"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	all := stores(t)
	for _, want := range doc.Arrays {
		a, err := OpenArray(all[want.Store], want.Path)
		if err != nil {
			t.Fatalf("%s %s: %v", want.Store, want.Path, err)
		}
		if strings.Trim(a.DType.String(), "<>|") != strings.Trim(want.DType, "<>|") {
			t.Errorf("%s %s: dtype %s, want %s", want.Store, want.Path, a.DType, want.DType)
		}
		data, err := a.ReadAll()
		if err != nil {
			t.Fatalf("%s %s: %v", want.Store, want.Path, err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want.SHA256 {
			t.Errorf("%s %s (%s, %v): values differ", want.Store, want.Path, a.Codec, want.Shape)
		}
	}
}

// Text, of varying length (format 2's VLenUTF8 filter; format 3's string type, plain and sharded)
// and of numpy's fixed types, read as zarr-python wrote it.
func TestTextFromZarrPython(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Texts []struct {
			Store, Path string
			Values      []string
		} `json:"texts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Texts) != 4 {
		t.Fatalf("%d texts in expected.json", len(doc.Texts))
	}
	all := stores(t)
	for _, want := range doc.Texts {
		a, err := OpenArray(all[want.Store], want.Path)
		if err != nil {
			t.Fatalf("%s %s: %v", want.Store, want.Path, err)
		}
		got, err := a.ReadStrings()
		if err != nil {
			t.Fatalf("%s %s: %v", want.Store, want.Path, err)
		}
		if strings.Join(got, "|") != strings.Join(want.Values, "|") {
			t.Errorf("%s %s: %q, want %q", want.Store, want.Path, got, want.Values)
		}
		if _, err := a.ReadAll(); err == nil {
			t.Errorf("%s %s: text read as bytes", want.Store, want.Path)
		}
	}
	for path, want := range map[string]string{"text": "DAPI|Boundary|Interior RNA|18S", "bytes": "aiaafdpo-1|bbbbcccc-1"} {
		a, err := OpenArray(all["v2.zarr"], path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.ReadStrings()
		if err != nil || strings.Join(got, "|") != want {
			t.Errorf("%s: %q %v", path, got, err)
		}
	}
}

// Damaged text and shards are refused, not misread.
func TestDamagedTextAndShards(t *testing.T) {
	for _, bad := range [][]byte{nil, {1, 0, 0}, {9, 0, 0, 0, 1, 0, 0, 0}, {1, 0, 0, 0, 5, 0, 0, 0, 'a', 'b'}, {1, 0, 0, 0, 1, 0, 0, 0, 0xff}} {
		if _, err := vlenUTF8(bad); err == nil {
			t.Errorf("% x read as text", bad)
		}
	}
	got, err := vlenUTF8([]byte{2, 0, 0, 0, 1, 0, 0, 0, 'a', 0, 0, 0, 0})
	if err != nil || len(got) != 2 || got[0] != "a" || got[1] != "" {
		t.Errorf("%q %v", got, err)
	}
	a, err := OpenArray(stores(t)["v3.zarr"], "sharded")
	if err != nil {
		t.Fatal(err)
	}
	shard, err := a.store.Get(a.chunkKey([]int{0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	for name, damaged := range map[string][]byte{
		"truncated":       shard[:10],
		"index corrupted": append(append([]byte(nil), shard[:len(shard)-5]...), shard[len(shard)-5]^1, 0, 0, 0, 0),
	} {
		if _, err := a.decodeShard(damaged, 16*16*a.DType.Size); err == nil {
			t.Errorf("a %s shard read", name)
		}
	}
}

func TestMetadataAndListing(t *testing.T) {
	s := stores(t)["v2.zarr.zip"]
	arrays, err := Arrays(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(arrays) != 16 {
		t.Fatalf("%d arrays: %v", len(arrays), arrays)
	}
	attrs, err := Attributes(s, "")
	if err != nil || attrs["note"] != "SituCraft test fixture" {
		t.Fatalf("attributes %v %v", attrs, err)
	}
	a, err := OpenArray(s, "order_f")
	if err != nil {
		t.Fatal(err)
	}
	if a.Order != 'F' || a.Codec != "blosc" || a.Shape[0] != 37 || a.Chunks[1] != 8 {
		t.Fatalf("order_f: %+v", a)
	}
	if sh, err := OpenArray(stores(t)["v3.zarr"], "sharded"); err != nil || sh.Codec != "sharded zstd" || sh.Chunks[0] != 16 {
		t.Fatalf("sharded array: %+v %v", sh, err)
	}
	if _, err := OpenArray(s, "missing"); err == nil {
		t.Fatal("a missing array opened")
	}
}

func TestHostileStoresAreRefused(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
	}
	write("huge/.zarray", `{"zarr_format":2,"shape":[1000000000,1000000000],"chunks":[1000000,1000000],"dtype":"<f8","compressor":null,"fill_value":0,"order":"C","filters":null}`)
	write("filtered/.zarray", `{"zarr_format":2,"shape":[4],"chunks":[4],"dtype":"<f8","compressor":null,"fill_value":0,"order":"C","filters":[{"id":"delta","dtype":"<f8"}]}`)
	write("objects/.zarray", `{"zarr_format":2,"shape":[4],"chunks":[4],"dtype":"|O","compressor":null,"fill_value":0,"order":"C","filters":null}`)
	write("short/.zarray", `{"zarr_format":2,"shape":[4],"chunks":[4],"dtype":"<f8","compressor":null,"fill_value":0,"order":"C","filters":null}`)
	write("short/0", "abc")
	s := OpenDir(dir)
	for _, name := range []string{"huge", "filtered", "objects"} {
		if _, err := OpenArray(s, name); err == nil {
			t.Errorf("%s opened", name)
		}
	}
	a, err := OpenArray(s, "short")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReadAll(); err == nil {
		t.Error("a chunk of the wrong size was accepted")
	}
	if _, err := s.Get("../outside"); err == nil {
		t.Error("a key outside the store was read")
	}
}

type memStore map[string][]byte

func (m memStore) Get(key string) ([]byte, error) {
	if v, ok := m[key]; ok {
		return v, nil
	}
	return nil, ErrNotFound
}

func (m memStore) Keys() ([]string, error) {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys, nil
}

func FuzzArray(f *testing.F) {
	chunk, _ := os.ReadFile(filepath.Join("testdata", "v2.zarr", "blosc_lz4_shuffle", "0.0"))
	meta, _ := os.ReadFile(filepath.Join("testdata", "v2.zarr", "blosc_lz4_shuffle", ".zarray"))
	f.Add(meta, chunk)
	f.Add([]byte(`{"zarr_format":2,"shape":[5,7],"chunks":[2,3],"dtype":">u2","compressor":{"id":"zlib"},"fill_value":3,"order":"F","filters":null}`), []byte{0x78, 0x9c, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01})
	f.Fuzz(func(t *testing.T, meta, chunk []byte) {
		s := memStore{"a/.zarray": meta, "a/0.0": chunk, "a/0": chunk, "a/0.0.0": chunk}
		a, err := OpenArray(s, "a")
		if err != nil {
			return
		}
		n := int64(a.DType.Size)
		for _, d := range a.Shape {
			n *= int64(d)
		}
		if n <= 1<<20 {
			a.ReadAll()
		}
	})
}
