package plant

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"situcraft/internal/compare"
	"situcraft/internal/segment/expand"
	"situcraft/internal/segment/imports"
	"situcraft/internal/xenium"
)

func bundleDir(t *testing.T, name string) string {
	t.Helper()
	root := os.Getenv("SITUCRAFT_DATA")
	if root == "" {
		root = filepath.Join("..", "..", "..", "validation", "cache")
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(filepath.Join(dir, "experiment.xenium")); err != nil {
		want := os.Getenv("SITUCRAFT_REQUIRE_DATA")
		if want == "1" || strings.Contains(","+want+",", ","+name+",") {
			t.Fatalf("%s is missing (node validation/fetch.mjs %s)", dir, name)
		}
		t.Skipf("%s is missing (node validation/fetch.mjs %s)", dir, name)
	}
	return dir
}

type bundleTranscripts struct{ b *xenium.Bundle }

func (d bundleTranscripts) Transcripts(fn func(*xenium.TranscriptBatch) error) error {
	t, err := d.b.OpenTranscripts()
	if err != nil {
		return err
	}
	defer t.Close()
	return t.Each(fn)
}

// The example's errors planted in the Lung bundle's own segmentation: the comparison finds each
// as what it is (every merge a merged difference of its two cells, its merged cell mixing more
// mutually exclusive markers than either; every split a split; every missed cell only in the
// instrument's; every grown cell expanded), and every cell no error touched is the same cell in
// both, with the same transcripts.
func TestRealPlantedErrors(t *testing.T) {
	b, err := xenium.Open(bundleDir(t, "xenium-lung-2fov"), xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx := context.Background()
	src := t.TempDir()
	if _, err := imports.Instrument(ctx, b, src, runtime.NumCPU()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	_, truth, err := Plant(ctx, src, "the instrument's segmentation", bundleTranscripts{b}, dir, DefaultParams(), nil)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, e := range truth.Errors {
		kinds[e.Kind]++
	}
	t.Logf("planted %v", kinds)
	if kinds[Merge] != 20 || kinds[Split] != 20 || kinds[Missed] != 10 || kinds[Grown] != 10 {
		t.Fatalf("planted %v", kinds)
	}
	a, err := compare.Load(src)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := compare.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	nuclei, err := expand.NucleusRaster(b)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := compare.BundleTranscripts(b)
	if err != nil {
		t.Fatal(err)
	}
	res, err := compare.Compare(ctx, compare.Inputs{A: a, B: pb, Nuclei: nuclei, Transcripts: tr, Params: compare.DefaultParams()})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("classes %v; %.4f%% of gene transcripts changed cell", res.Classes, 100*res.ChangesQ20.Fraction)
	index := map[string]int32{}
	for i, c := range a.Cells {
		index[c.ID] = int32(i)
	}
	groupOf := map[int32]compare.Group{}
	for _, g := range res.Groups {
		for _, c := range g.A {
			groupOf[c] = g
		}
	}
	planted := map[int32]bool{}
	mixed := 0
	for _, e := range truth.Errors {
		for _, c := range e.Cells {
			planted[index[c]] = true
		}
		g := groupOf[index[e.Cells[0]]]
		want := map[string]string{Merge: compare.Merged, Split: compare.Split, Missed: compare.OnlyA, Grown: compare.Expanded}[e.Kind]
		if g.Class != want {
			t.Errorf("%s of %v (%s) is a %s difference, not %s", e.Kind, e.Cells, e.Note, g.Class, want)
		}
		if e.Kind == Merge && g.MixedB > g.MixedA {
			mixed++
		}
	}
	if mixed != kinds[Merge] {
		t.Errorf("%d of %d merges mix mutually exclusive markers", mixed, kinds[Merge])
	}
	for _, g := range res.Groups {
		touched := false
		for _, c := range g.A {
			touched = touched || planted[c]
		}
		if !touched && g.Class != compare.OneToOne {
			t.Errorf("an unplanted difference: %s of %d and %d cells", g.Class, len(g.A), len(g.B))
		}
		if !touched && g.Changed > 0 {
			t.Errorf("an unplanted cell's transcripts changed: %d", g.Changed)
		}
	}
}
