package parquet

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureRows = 2500

func open(t testing.TB, name string) *File {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".parquet"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return f
}

// The fixtures' rows (testdata/make_fixtures.py), computed again: column → row → value or nil.
func expected(column string, i int) any {
	switch column {
	case "id":
		return int64(i)
	case "small":
		return int32(i*7%1000 - 500)
	case "u8":
		return int32(i % 256)
	case "f":
		return float32(float64(i) / 3.0)
	case "d":
		return float64(i)*0.25 - 100
	case "s":
		return fmt.Sprintf("gene_%d", i%97)
	case "b":
		return i%3 == 0
	case "maybe":
		if i%5 == 0 {
			return nil
		}
		return int32(i)
	case "name":
		if i%7 == 0 {
			return nil
		}
		return fmt.Sprintf("cell-%d", i)
	}
	return nil
}

func value(v *Values, i int) any {
	if v.Valid != nil && !v.Valid[i] {
		return nil
	}
	switch {
	case v.Bool != nil:
		return v.Bool[i]
	case v.Int32 != nil:
		return v.Int32[i]
	case v.Int64 != nil:
		return v.Int64[i]
	case v.Float32 != nil:
		return v.Float32[i]
	case v.Float64 != nil:
		return v.Float64[i]
	default:
		return string(v.Bytes[i])
	}
}

// Files written by DuckDB 1.5 and pyarrow 26 (testdata/make_fixtures.py): every codec SituCraft
// reads, dictionary and plain pages, data page versions 1 and 2, the delta encodings and
// byte-stream-split, nulls, and several row groups; every value compared.
func TestFilesFromDuckDBAndPyarrow(t *testing.T) {
	names := []string{
		"duckdb-zstd", "duckdb-snappy", "duckdb-gzip", "duckdb-uncompressed", "duckdb-lz4_raw", "duckdb-v2",
		"pyarrow-plain", "pyarrow-dictionary", "pyarrow-v2pages", "pyarrow-delta", "pyarrow-bytestreamsplit", "pyarrow-lz4",
	}
	for _, name := range names {
		f := open(t, name)
		// DuckDB rounds row groups up to its 2,048-row vectors: one group here; pyarrow writes three.
		groups := 3
		if strings.HasPrefix(name, "duckdb") {
			groups = 1
		}
		if f.NumRows != fixtureRows || len(f.RowGroups) != groups || len(f.Columns) != 9 {
			t.Fatalf("%s: %d rows, %d row groups, %d columns", name, f.NumRows, len(f.RowGroups), len(f.Columns))
		}
		for c, col := range f.Columns {
			v, err := f.ReadAll(c)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if v.Len != fixtureRows {
				t.Fatalf("%s %s: %d values", name, col.Name, v.Len)
			}
			for i := 0; i < fixtureRows; i++ {
				if got, want := value(v, i), expected(col.Name, i); got != want {
					t.Fatalf("%s %s row %d: %v, want %v", name, col.Name, i, got, want)
				}
			}
		}
	}
}

func TestSchema(t *testing.T) {
	f := open(t, "pyarrow-dictionary")
	byName := map[string]Column{}
	for _, c := range f.Columns {
		byName[c.Name] = c
	}
	if c := byName["u8"]; c.Type != Int32 || c.Logical != "INTEGER" || c.IntegerBits != 8 || c.Signed {
		t.Fatalf("u8: %+v", c)
	}
	if c := byName["s"]; c.Type != ByteArray || c.Logical != "STRING" {
		t.Fatalf("s: %+v", c)
	}
	if c := byName["maybe"]; !c.Optional || c.Type != Int32 {
		t.Fatalf("maybe: %+v", c)
	}
	if f.Index("d") != 4 || f.Index("missing") != -1 {
		t.Fatal("Index")
	}
	if !strings.Contains(f.CreatedBy, "parquet-cpp") {
		t.Fatalf("created by %q", f.CreatedBy)
	}
	chunk := f.RowGroups[0].Chunks[f.Index("id")]
	if chunk.Codec != Zstd || !chunk.Stats.HasNullCount || chunk.Stats.NullCount != 0 || len(chunk.Stats.Min) != 8 {
		t.Fatalf("id chunk: %+v", chunk)
	}
}

func TestUnreadCodecsAndHostileFiles(t *testing.T) {
	f := open(t, "pyarrow-brotli")
	if _, err := f.ReadAll(0); err == nil || !strings.Contains(err.Error(), "BROTLI") {
		t.Fatalf("brotli: %v", err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "duckdb-zstd.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{nil, []byte("PAR1PAR1"), []byte("notparquetatall!")} {
		if _, err := Open(bytes.NewReader(bad), int64(len(bad))); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	// Corrupt every hundredth byte, one at a time: errors, never panics.
	for i := 4; i < len(data)-8; i += 97 {
		broken := append([]byte(nil), data...)
		broken[i] ^= 0x5a
		if f, err := Open(bytes.NewReader(broken), int64(len(broken))); err == nil {
			for c := range f.Columns {
				f.ReadAll(c)
			}
		}
	}
}

func FuzzOpen(f *testing.F) {
	for _, name := range []string{"duckdb-zstd", "pyarrow-delta", "pyarrow-v2pages", "pyarrow-bytestreamsplit"} {
		data, err := os.ReadFile(filepath.Join("testdata", name+".parquet"))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := Open(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		for c := range file.Columns {
			file.ReadAll(c)
		}
	})
}
