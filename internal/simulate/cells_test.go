package simulate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"situcraft/internal/parquet"
)

// The true cells alone are the truth's: each cell's centroid, type, domain and gene transcripts
// those its bundle's truth writes, for a field and for a mosaic (its cells shifted to their tile
// and their domains numbered across it); the same on one worker and on four.
func TestCellsAreTheTruths(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	small, _ := smallBundle(t)
	mosaic := filepath.Join(t.TempDir(), "mosaic")
	if _, err := Run(context.Background(), mosaicParams(), mosaic, "test", nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		p      Params
		folder string
	}{{"a field", Presets["small"], small}, {"a mosaic", mosaicParams(), mosaic}} {
		f, err := os.Open(filepath.Join(c.folder, "truth", "cells.parquet"))
		if err != nil {
			t.Fatal(err)
		}
		info, _ := f.Stat()
		pf, err := parquet.Open(f, info.Size())
		if err != nil {
			t.Fatal(err)
		}
		col := func(name string) *parquet.Values {
			v, err := pf.ReadAll(pf.Index(name))
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
		xs, ys, types, domains, totals := col("x_centroid"), col("y_centroid"), col("cell_type"), col("domain"), col("transcript_counts")
		f.Close()
		one, err := Cells(context.Background(), c.p, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		four, err := Cells(context.Background(), c.p, 4, nil)
		if err != nil {
			t.Fatal(err)
		}
		n := len(one.X)
		if n != int(pf.NumRows) || len(four.X) != n || len(one.Counts.Offsets) != n+1 {
			t.Fatalf("%s: %d cells, the truth %d", c.name, n, pf.NumRows)
		}
		for i := 0; i < n; i++ {
			total := uint64(0)
			for k := one.Counts.Offsets[i]; k < one.Counts.Offsets[i+1]; k++ {
				total += uint64(one.Counts.Count[k])
			}
			if one.X[i] != float32(xs.Float64[i]) || one.Y[i] != float32(ys.Float64[i]) || one.Types[one.Type[i]] != string(types.Bytes[i]) || one.Domain[i] != domains.Int32[i] || int64(total) != totals.Int64[i] {
				t.Fatalf("%s: cell %d is (%g, %g) %s in %d with %d transcripts; the truth's (%g, %g) %s in %d with %d", c.name, i, one.X[i], one.Y[i], one.Types[one.Type[i]], one.Domain[i], total, xs.Float64[i], ys.Float64[i], types.Bytes[i], domains.Int32[i], totals.Int64[i])
			}
			if one.X[i] != four.X[i] || one.Type[i] != four.Type[i] || one.Counts.Offsets[i+1] != four.Counts.Offsets[i+1] {
				t.Fatalf("%s: cell %d differs on four workers", c.name, i)
			}
		}
		for k := range one.Counts.Gene {
			if one.Counts.Gene[k] != four.Counts.Gene[k] || one.Counts.Count[k] != four.Counts.Count[k] {
				t.Fatalf("%s: the counts differ on four workers", c.name)
			}
		}
		t.Logf("%s: %d cells, %d genes, %d counts, as the truth has them", c.name, n, len(one.Counts.Genes), len(one.Counts.Gene))
	}
}
