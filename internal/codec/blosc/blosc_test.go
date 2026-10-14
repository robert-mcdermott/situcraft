package blosc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type fixture struct {
	Name   string         `json:"name"`
	NBytes int            `json:"nbytes"`
	SHA256 string         `json:"sha256"`
	Blosc  map[string]any `json:"blosc"`
}

func fixtures(t testing.TB) []fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Cases []fixture `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Cases
}

// Chunks written by c-blosc 1.21 (testdata/make_fixtures.py): every codec SituCraft reads, with
// and without byte and bit shuffling, splits and a short last block, types past the split limit,
// stored streams and stored chunks.
func TestChunksFromCBlosc(t *testing.T) {
	cases := fixtures(t)
	if len(cases) < 50 {
		t.Fatalf("only %d fixtures", len(cases))
	}
	for _, c := range cases {
		chunk, err := os.ReadFile(filepath.Join("testdata", c.Name+".blosc"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(chunk, 0)
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		sum := sha256.Sum256(got)
		if len(got) != c.NBytes || hex.EncodeToString(sum[:]) != c.SHA256 {
			t.Errorf("%s: decoded %d bytes with SHA-256 %x, want %d bytes, %s", c.Name, len(got), sum, c.NBytes, c.SHA256)
		}
	}
}

func TestHeader(t *testing.T) {
	chunk, err := os.ReadFile(filepath.Join("testdata", "zstd-shuffle-f4.blosc"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := ReadHeader(chunk)
	if err != nil {
		t.Fatal(err)
	}
	if h.Codec() != "zstd" || h.TypeSize != 4 || h.Flags&flagShuffle == 0 || h.NBytes != 24000 {
		t.Fatalf("header %+v (codec %s)", h, h.Codec())
	}
}

func TestHostileChunksAreRefused(t *testing.T) {
	chunk, err := os.ReadFile(filepath.Join("testdata", "lz4-shuffle-f4.blosc"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(chunk, 1000); err == nil {
		t.Error("a chunk declaring more than the limit was accepted")
	}
	for cut := 1; cut < len(chunk); cut += len(chunk) / 13 {
		if _, err := Decode(chunk[:cut], 0); err == nil {
			t.Errorf("a chunk cut to %d bytes was accepted", cut)
		}
	}
	blosclz := append([]byte(nil), chunk...)
	blosclz[2] &^= 0xe0 // codec 0, blosclz
	if _, err := Decode(blosclz, 0); err == nil {
		t.Error("a blosclz chunk was accepted")
	}
	bad := append([]byte(nil), chunk...)
	bad[16] = 0xff // the first block's offset, past the chunk
	if _, err := Decode(bad, 0); err == nil {
		t.Error("a block offset past the chunk was accepted")
	}
}

func FuzzDecode(f *testing.F) {
	for _, name := range []string{"lz4-shuffle-f4", "zstd-bitshuffle-u2-blocks", "zlib-noshuffle-u8", "lz4-clevel0"} {
		chunk, err := os.ReadFile(filepath.Join("testdata", name+".blosc"))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(chunk)
	}
	f.Fuzz(func(t *testing.T, chunk []byte) {
		Decode(chunk, 1<<22)
	})
}
