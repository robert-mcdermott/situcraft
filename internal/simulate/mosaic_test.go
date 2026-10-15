package simulate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"situcraft/internal/parquet"
	"situcraft/internal/xenium"
	"situcraft/internal/zarr"
)

// mosaicParams are 2 × 2 tiles of the small preset's tissue, each 512 pixels a side.
func mosaicParams() Params {
	p := Presets["small"]
	p.WidthUm, p.HeightUm = 512*p.PixelUm, 512*p.PixelUm
	p.TilesAcross, p.TilesDown = 2, 2
	return p
}

// A mosaic is one bundle: it validates, its metrics agree, its first tile is the field a single
// simulation makes, its transcripts' ids are unique, its truth describes every transcript, its
// masks are whole, and it is made the same every time.
func TestAMosaicIsOneBundle(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	p := mosaicParams()
	dir := t.TempDir()
	folder := filepath.Join(dir, "mosaic")
	sum, err := Run(context.Background(), p, folder, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.ImageWidth != 1024 || sum.ImageHeight != 1024 {
		t.Errorf("an image of %d × %d", sum.ImageWidth, sum.ImageHeight)
	}
	b, err := xenium.Open(folder, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	r, err := xenium.Validate(context.Background(), b, xenium.ValidateOptions{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Findings {
		if f.Severity == xenium.Fatal || f.Severity == xenium.Warning {
			t.Errorf("%s %s: %s", f.Severity, f.Code, f.Detail)
		}
	}
	if r.Summary.MetricsCompared < 12 || r.Summary.MetricsAgreeing != r.Summary.MetricsCompared {
		for _, m := range r.Summary.Metrics {
			t.Logf("metric %s: recorded %g, computed %g, agrees %v", m.Name, m.Recorded, m.Computed, m.Agrees)
		}
		t.Errorf("%d of %d metrics agree", r.Summary.MetricsAgreeing, r.Summary.MetricsCompared)
	}
	if r.Summary.Cells != sum.InstrumentCells {
		t.Errorf("%d cells read, %d written", r.Summary.Cells, sum.InstrumentCells)
	}

	// The first tile is the field one simulation of its parameters makes.
	single := p
	single.TilesAcross, single.TilesDown = 0, 0
	one := filepath.Join(dir, "one")
	if _, err := Run(context.Background(), single, one, "test", nil); err != nil {
		t.Fatal(err)
	}
	read := func(folder, name string, cols ...string) []*parquet.Values {
		f, err := os.Open(filepath.Join(folder, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		st, _ := f.Stat()
		pf, err := parquet.Open(f, st.Size())
		if err != nil {
			t.Fatal(err)
		}
		var out []*parquet.Values
		for _, c := range cols {
			v, err := pf.ReadAll(pf.Index(c))
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, v)
		}
		return out
	}
	m := read(folder, "transcripts.parquet", "x_location", "y_location", "feature_name", "cell_id", "transcript_id")
	s := read(one, "transcripts.parquet", "x_location", "y_location", "feature_name", "cell_id")
	for i := 0; i < s[0].Len; i++ {
		if m[0].Float32[i] != s[0].Float32[i] || m[1].Float32[i] != s[1].Float32[i] || string(m[2].Bytes[i]) != string(s[2].Bytes[i]) || string(m[3].Bytes[i]) != string(s[3].Bytes[i]) {
			t.Fatalf("transcript %d of the first tile differs from the single field's", i)
		}
	}
	if m[0].Len <= 3*s[0].Len || m[0].Len != sum.Transcripts {
		t.Errorf("%d transcripts in the mosaic, %d in a field", m[0].Len, s[0].Len)
	}
	ids := map[int64]bool{}
	outside := 0
	for i := 0; i < m[0].Len; i++ {
		ids[m[4].Int64[i]] = true
		if x, y := float64(m[0].Float32[i]), float64(m[1].Float32[i]); x < -10 || y < -10 || x > 2*p.WidthUm+10 || y > 2*p.HeightUm+10 {
			outside++
		}
	}
	if len(ids) != m[0].Len || outside > 0 {
		t.Errorf("%d transcript ids for %d transcripts; %d outside the mosaic", len(ids), m[0].Len, outside)
	}
	// The truth describes the same transcripts.
	tr := read(folder, filepath.Join("truth", "transcripts.parquet"), "transcript_id", "x_location", "cell_id", "origin")
	if tr[0].Len != m[0].Len {
		t.Fatalf("%d truth rows of %d", tr[0].Len, m[0].Len)
	}
	for i := 0; i < tr[0].Len; i++ {
		if tr[0].Int64[i] != m[4].Int64[i] || tr[1].Float32[i] != m[0].Float32[i] || (string(tr[3].Bytes[i]) == "cell") != (string(tr[2].Bytes[i]) != "UNASSIGNED") {
			t.Fatalf("truth row %d differs", i)
		}
	}
	// The masks: whole, every cell's label in them.
	f, _ := os.Open(filepath.Join(folder, "cells.zarr.zip"))
	defer f.Close()
	st, _ := f.Stat()
	store, err := zarr.OpenZip(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	cells, err := zarr.OpenArray(store, "masks/1")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := cells.ReadAll()
	labels := map[uint32]bool{}
	for k := 0; k+4 <= len(raw); k += 4 {
		labels[uint32(raw[k])|uint32(raw[k+1])<<8|uint32(raw[k+2])<<16|uint32(raw[k+3])<<24] = true
	}
	delete(labels, 0)
	if cells.Shape[0] != 1024 || cells.Shape[1] != 1024 || len(labels) != sum.InstrumentCells {
		t.Errorf("a mask of %v holding %d cells, of %d", cells.Shape, len(labels), sum.InstrumentCells)
	}
	// The same every time.
	again := filepath.Join(dir, "again")
	if _, err := Run(context.Background(), p, again, "test", nil); err != nil {
		t.Fatal(err)
	}
	if a, _ := digest(t, folder); a != func() string { d, _ := digest(t, again); return d }() {
		t.Error("two runs made different mosaics")
	}
}
