package simulate

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"situcraft/internal/parquet"
	"situcraft/internal/xenium"
)

// The small preset's bundle, made once for the tests that read it.
var (
	smallOnce   sync.Once
	smallFolder string
	smallSum    *Summary
	smallErr    error
)

func smallBundle(t *testing.T) (string, *Summary) {
	t.Helper()
	smallOnce.Do(func() {
		dir, err := testTempDir()
		if err != nil {
			smallErr = err
			return
		}
		smallFolder = filepath.Join(dir, "small")
		smallSum, smallErr = Run(context.Background(), Presets["small"], smallFolder, "test", nil)
	})
	if smallErr != nil {
		t.Fatal(smallErr)
	}
	return smallFolder, smallSum
}

func TestSmallBundleValidates(t *testing.T) {
	folder, sum := smallBundle(t)
	t.Logf("%+v", *sum)
	b, err := xenium.Open(folder, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Simulation == nil {
		t.Fatal("the bundle is not recognized as simulated")
	}
	r, err := xenium.Validate(context.Background(), b, xenium.ValidateOptions{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if r.Support != "simulated" {
		t.Errorf("support %q", r.Support)
	}
	simulated := false
	for _, f := range r.Findings {
		t.Logf("%s %s: %s — %s", f.Severity, f.Code, f.Title, f.Detail)
		if f.Severity == xenium.Fatal || f.Severity == xenium.Warning {
			t.Errorf("%s %s: %s", f.Severity, f.Code, f.Detail)
		}
		simulated = simulated || f.Code == "bundle.simulated"
	}
	if !simulated {
		t.Error("no bundle.simulated finding")
	}
	for _, m := range r.Summary.Metrics {
		t.Logf("metric %s (%s): recorded %g, computed %g, agrees %v", m.Name, m.Source, m.Recorded, m.Computed, m.Agrees)
	}
	// Every metric the validator recomputes agrees with what the bundle records.
	if r.Summary.MetricsCompared < 12 || r.Summary.MetricsAgreeing != r.Summary.MetricsCompared {
		t.Errorf("%d of %d metrics agree", r.Summary.MetricsAgreeing, r.Summary.MetricsCompared)
	}
	if r.Summary.Cells != sum.InstrumentCells {
		t.Errorf("%d cells read, %d written", r.Summary.Cells, sum.InstrumentCells)
	}
}

// The truth's transcripts are the bundle's, row for row, with each one's true cell, place, origin
// and displacement.
func TestTheTruthDescribesEveryTranscript(t *testing.T) {
	folder, sum := smallBundle(t)
	read := func(name string) (*parquet.File, func(string) *parquet.Values) {
		f, err := os.Open(filepath.Join(folder, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		info, _ := f.Stat()
		pf, err := parquet.Open(f, info.Size())
		if err != nil {
			t.Fatal(err)
		}
		return pf, func(col string) *parquet.Values {
			i := pf.Index(col)
			if i < 0 {
				t.Fatalf("%s has no column %s", name, col)
			}
			v, err := pf.ReadAll(i)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
	}
	observed, obs := read("transcripts.parquet")
	truth, tru := read(filepath.Join("truth", "transcripts.parquet"))
	if observed.NumRows != int64(sum.Transcripts) || truth.NumRows != observed.NumRows {
		t.Fatalf("%d and %d rows, of %d transcripts", observed.NumRows, truth.NumRows, sum.Transcripts)
	}
	for _, col := range []string{"transcript_id", "x_location", "y_location", "z_location", "qv", "feature_name", "fov_name"} {
		a, b := obs(col), tru(col)
		for i := 0; i < a.Len; i++ {
			same := false
			switch {
			case a.Int64 != nil:
				same = a.Int64[i] == b.Int64[i]
			case a.Float32 != nil:
				same = a.Float32[i] == b.Float32[i]
			default:
				same = string(a.Bytes[i]) == string(b.Bytes[i])
			}
			if !same {
				t.Fatalf("%s differs at row %d", col, i)
			}
		}
	}
	origins := map[string]int{}
	origin, cell, wide := tru("origin"), tru("cell_id"), tru("wide_displacement")
	widened := 0
	for i := 0; i < origin.Len; i++ {
		o := string(origin.Bytes[i])
		origins[o]++
		if (o == "cell") != (string(cell.Bytes[i]) != "UNASSIGNED") {
			t.Fatalf("row %d: origin %s, true cell %s", i, o, cell.Bytes[i])
		}
		widened += int(wide.Int32[i])
	}
	for o, n := range sum.MoleculesByOrigin {
		if origins[o] != n {
			t.Errorf("%d molecules of origin %s, of %d", origins[o], o, n)
		}
	}
	if widened != sum.WideDisplacements {
		t.Errorf("%d wide displacements, of %d", widened, sum.WideDisplacements)
	}
}
