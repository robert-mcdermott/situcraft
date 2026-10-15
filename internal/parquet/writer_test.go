package parquet

import (
	"bytes"
	"fmt"
	"math"
	"testing"
)

// Written files read back as written: every type, annotations, pages split, several row groups,
// key-value metadata; the same values give the same bytes.
func TestWriterRoundTrip(t *testing.T) {
	fields := []Field{
		{Name: "id", Type: Int64, Annotation: "uint"},
		{Name: "n", Type: Int32},
		{Name: "u", Type: Int32, Annotation: "uint"},
		{Name: "x", Type: Float},
		{Name: "d", Type: Double},
		{Name: "gene", Type: ByteArray, Annotation: "string"},
		{Name: "wkb", Type: ByteArray},
		{Name: "flag", Type: Boolean},
	}
	group := func(start, n int) []*Values {
		v := make([]*Values, len(fields))
		for i := range v {
			v[i] = &Values{Len: n}
		}
		for i := start; i < start+n; i++ {
			v[0].Int64 = append(v[0].Int64, int64(i)*1_000_003)
			v[1].Int32 = append(v[1].Int32, int32(i%1000-500))
			v[2].Int32 = append(v[2].Int32, int32(uint32(i)*2654435761))
			v[3].Float32 = append(v[3].Float32, float32(i)/7)
			v[4].Float64 = append(v[4].Float64, math.Sqrt(float64(i)))
			v[5].Bytes = append(v[5].Bytes, []byte(fmt.Sprintf("GENE_%d", i%37)))
			v[6].Bytes = append(v[6].Bytes, bytes.Repeat([]byte{byte(i)}, i%5))
			v[7].Bool = append(v[7].Bool, i%3 == 0)
		}
		return v
	}
	write := func() []byte {
		var buf bytes.Buffer
		w, err := NewWriter(&buf, fields, "situcraft test")
		if err != nil {
			t.Fatal(err)
		}
		w.PageSize = 1000 // several pages per column
		w.SetKeyValue("geo", `{"primary_column": "wkb"}`)
		for _, g := range [][2]int{{0, 3000}, {3000, 1}, {3001, 0}, {3001, 999}} {
			if err := w.WriteRowGroup(group(g[0], g[1])); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	data := write()
	if !bytes.Equal(data, write()) {
		t.Fatal("the same values gave different bytes")
	}
	f, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if f.NumRows != 4000 || len(f.RowGroups) != 4 || f.CreatedBy != "situcraft test" || f.KeyValue["geo"] != `{"primary_column": "wkb"}` {
		t.Fatalf("metadata: %d rows, %d groups, created by %q, %v", f.NumRows, len(f.RowGroups), f.CreatedBy, f.KeyValue)
	}
	for i, c := range f.Columns {
		if c.Name != fields[i].Name || c.Type != fields[i].Type || c.Optional {
			t.Fatalf("column %d: %+v", i, c)
		}
	}
	if c := f.Columns[0]; c.Logical != "INTEGER" || c.IntegerBits != 64 || c.Signed {
		t.Fatalf("id is not an unsigned 64-bit integer: %+v", c)
	}
	if c := f.Columns[5]; c.Logical != "STRING" || c.ConvertedType != 0 {
		t.Fatalf("gene is not text: %+v", c)
	}
	want := group(0, 4000)
	for i := range fields {
		got, err := f.ReadAll(i)
		if err != nil {
			t.Fatalf("%s: %v", fields[i].Name, err)
		}
		if got.Len != 4000 || got.Valid != nil {
			t.Fatalf("%s: %d values", fields[i].Name, got.Len)
		}
		for r := 0; r < 4000; r++ {
			if a, b := fmt.Sprint(value(got, r)), fmt.Sprint(value(want[i], r)); a != b {
				t.Fatalf("%s row %d: %s, wrote %s", fields[i].Name, r, a, b)
			}
		}
	}
}

func TestWriterRefusals(t *testing.T) {
	var buf bytes.Buffer
	for _, fields := range [][]Field{
		nil,
		{{Name: "a", Type: Int32}, {Name: "a", Type: Int32}},
		{{Name: "s", Type: Int32, Annotation: "string"}},
		{{Name: "f", Type: FixedLenByteArray}},
	} {
		if _, err := NewWriter(&buf, fields, ""); err == nil {
			t.Fatalf("%v was accepted", fields)
		}
	}
	w, _ := NewWriter(&buf, []Field{{Name: "a", Type: Int32}, {Name: "b", Type: Double}}, "")
	if err := w.WriteRowGroup([]*Values{{Int32: []int32{1, 2}}, {Float64: []float64{1}}}); err == nil {
		t.Fatal("columns of different lengths were written")
	}
}
