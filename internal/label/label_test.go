package label

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/random"
	"situcraft/internal/segment"
	"situcraft/internal/simulate"
)

// labelsDigest is the digest of the planted tissue's labels: the same on every processor.
const labelsDigest = "0a86d101db6e6e156d4aa6dae1ebfe2d44e3d859c9ddabaf8977992584dee632"

func set(raw string) *MarkerSet {
	s, err := Parse([]byte(raw))
	if err != nil {
		panic(err)
	}
	return s
}

const two = `{"format": "situcraft-markers", "version": 1, "name": "Two levels", "levels": ["Class", "Type"],
 "labels": [
  {"name": "A", "positive": ["G00", "G01"]},
  {"name": "A1", "parent": "A", "positive": ["G04"], "negative": ["G05"]},
  {"name": "A2", "parent": "A", "positive": ["G05"]},
  {"name": "B", "positive": ["G02", "G03"]},
  {"name": "B1", "parent": "B", "positive": ["G06"]}
 ],
 "states": [{"name": "Cycling", "positive": ["G07"], "parent": "A"}]}`

func TestMarkerSetsAreChecked(t *testing.T) {
	base := set(two)
	if base.Checksum() != set(strings.ReplaceAll(two, `"name": "A1"`, `"name": "  A1 "`)).Checksum() {
		t.Error("names trimmed should give the same checksum")
	}
	if base.Checksum() == set(strings.Replace(two, `"G06"`, `"G07"`, 1)).Checksum() {
		t.Error("another marker gave the same checksum")
	}
	bad := map[string]string{
		`"name": "A2", "parent": "A"`:    `"name": "a1", "parent": "A"`, // a name twice, regardless of case
		`"name": "B1", "parent": "B"`:    `"name": "B1", "parent": "C"`, // a parent not listed before
		`"levels": ["Class", "Type"]`:    `"levels": ["Class"]`,         // a label deeper than the levels
		`"positive": ["G00", "G01"]`:     `"positive": ["G00", "g00"]`,  // a gene twice
		`"positive": ["G04"]`:            `"positive": ["G05"]`,         // positive and negative
		`"version": 1`:                   `"version": 2`,                // a later format
		`"positive": ["G02", "G03"]}`:    `"positive": []}`,             // no positive marker
		`{"name": "Cycling", "positive"`: `{"name": "Cycling", "extra": 1, "positive"`,
	}
	for from, to := range bad {
		raw := strings.Replace(two, from, to, 1)
		if raw == two {
			t.Fatalf("%q is not in the set", from)
		}
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("%s passed", to)
		}
	}
	cited := `{"format": "situcraft-markers", "version": 1, "name": "Cited", "levels": ["Class"], "labels": [{"name": "A", "positive": ["X"], "references": ["10.1/a"]}], "references": [{"doi": "10.1/b", "citation": "B"}]}`
	if _, err := Parse([]byte(cited)); err == nil || !strings.Contains(err.Error(), "not among the set's references") {
		t.Errorf("a label citing what the set does not: %v", err)
	}
	// Genes the panel lacks are listed by label; a gene matches regardless of case when one does.
	_, err := base.Resolve([]string{"G00", "g01", "G02", "G03", "G04", "G05"})
	var missing *MissingError
	if !errors.As(err, &missing) || !slices.Equal(missing.Missing["B1"], []string{"G06"}) || !slices.Equal(missing.Missing["Cycling"], []string{"G07"}) || len(missing.Missing) != 2 {
		t.Errorf("missing genes: %v", err)
	}
	sets, err := base.Resolve([]string{"G00", "g01", "G02", "G03", "G04", "G05", "G06", "G07"})
	if err != nil {
		t.Fatal(err)
	}
	if s := sets[1]; s.Level != 1 || s.Parent != 0 || !slices.Equal(s.Positive, []int{4}) || !slices.Equal(s.Negative, []int{5}) || !slices.Equal(sets[0].Positive, []int{0, 1}) {
		t.Errorf("A1 resolved to %+v", s)
	}
	if s := sets[5]; !s.State || s.Level != -1 || s.Parent != 0 {
		t.Errorf("the state resolved to %+v", s)
	}
}

// Bins as scanpy's score_genes cuts them: ranks by mean, ties given the lowest; rank // the genes
// over bins − 1, rounded half to even.
func TestBinsAndControlsAsScanpy(t *testing.T) {
	if roundEven(2.5) != 2 || roundEven(3.5) != 4 || roundEven(11.67) != 12 {
		t.Error("rounding is not half to even")
	}
	// Ranks: 0.1, 0.1 → 1, 1; 0.3 → 3; 0.5 → 4; 0.9 → 5. Five genes over 2 → 2.5 → 2 per bin.
	if got := Bins([]float64{0.5, 0.1, 0.1, 0.9, 0.3}, every(5), 3); !slices.Equal(got, []int{2, 0, 0, 2, 1}) {
		t.Errorf("bins %v", got)
	}
	// Twelve genes over 3 bins − 1 → 6 per bin: ranks 1–5 bin 0, 6–11 bin 1, 12 bin 2.
	means := []float64{12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	bin := Bins(means, every(12), 3)
	if !slices.Equal(bin, []int{2, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0}) {
		t.Fatalf("bins %v", bin)
	}
	// Without a draw, a list's controls are its bins' genes less its own, in the panel's order.
	if got := Controls(bin, []int{1, 8}, 50, random.New(1, 0)); !slices.Equal(got, []int{2, 3, 4, 5, 6, 7, 9, 10, 11}) {
		t.Errorf("controls %v", got)
	}
	// With one, Controls of each bin, the same for the same stream, from the bins alone.
	a := Controls(bin, []int{1, 8}, 2, random.New(1, 0))
	b := Controls(bin, []int{1, 8}, 2, random.New(1, 0))
	if !slices.Equal(a, b) || len(a) < 2 || len(a) > 4 {
		t.Errorf("drawn controls %v and %v", a, b)
	}
	for _, g := range a {
		if g == 1 || g == 8 || bin[g] == 2 {
			t.Errorf("control %d is a marker or of another bin", g)
		}
	}
}

func every(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// counts makes a segmentation's counts from a dense matrix (cells × genes).
func counts(genes []string, rows [][]uint32) *segment.Counts {
	c := &segment.Counts{Genes: genes, Offsets: []uint32{0}}
	for _, row := range rows {
		for g, n := range row {
			if n > 0 {
				c.Gene = append(c.Gene, uint32(g))
				c.Count = append(c.Count, n)
			}
		}
		c.Offsets = append(c.Offsets, uint32(len(c.Gene)))
	}
	return c
}

// A score by hand: the markers' mean log-normalized value less their controls'.
func TestScoresByHand(t *testing.T) {
	genes := []string{"M", "C1", "C2", "N"}
	// Totals 4 and 2; target 2: values log1p(count·2/total).
	c := counts(genes, [][]uint32{{2, 1, 1, 0}, {0, 0, 1, 1}})
	e, err := cluster.LoadExpression(c, []uint32{0, 1}, []uint32{0, 0}, 2)
	if err != nil {
		t.Fatal(err)
	}
	v := func(n, total float64) float64 { return math.Log(1 + n*2/total) }
	sets := []Set{{Name: "L", Parent: -1, Positive: []int{0}, Negative: []int{3}}}
	// Means: M log 2 / 2, C1 log 1.5 / 2, C2 (log 1.5 + log 2)/2, N log 2 / 2: with 3 bins (4/2 → 2
	// per bin) the ranks 1 (C1), 2 (M and N tie), 4 (C2) cut to 0, 1, 1, 2: M's control N, N's M.
	L, err := MakeLists(sets, e.GeneMeans(), Params{Bins: 3, Controls: 50})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(L.PositiveControls[0], []int{3}) || !slices.Equal(L.NegativeControls[0], []int{0}) {
		t.Fatalf("controls %v and %v", L.PositiveControls[0], L.NegativeControls[0])
	}
	sc := Combine(L, e.ListMeans(L.All()))
	pos := []float64{v(2, 4) - 0, 0 - v(1, 2)}
	neg := []float64{0 - v(2, 4), v(1, 2) - 0}
	for i := 0; i < 2; i++ {
		score := pos[i]
		if neg[i] > 0 {
			score -= neg[i]
		}
		if math.Abs(sc.Positive[0][i]-pos[i]) > 1e-15 || math.Abs(sc.Negative[0][i]-neg[i]) > 1e-15 || math.Abs(sc.Score[0][i]-score) > 1e-15 {
			t.Errorf("cell %d: %v %v %v, by hand %v %v %v", i, sc.Positive[0][i], sc.Negative[0][i], sc.Score[0][i], pos[i], neg[i], score)
		}
	}
}

// Labels from the top down: the best child when it leads the next by the margin and scores the
// minimum; ties and narrow leads stop at the level above; a label without siblings against the
// minimum; a state only where its label is.
func TestAssignmentFromTheTopDown(t *testing.T) {
	sets, _ := set(two).Resolve([]string{"G00", "G01", "G02", "G03", "G04", "G05", "G06", "G07"})
	p := Params{Margin: 0.1, Minimum: 0, State: 0.3}
	// Scores of A, A1, A2, B, B1, Cycling for each cell.
	cells := [][]float64{
		{0.5, 0.4, 0.1, 0.2, 0.0, 0.5},  // A (leads B by 0.3), A1 (leads A2 by 0.3); cycling
		{0.5, 0.3, 0.25, 0.2, 0.9, 0.2}, // A, but A1 leads A2 by 0.05: stays A; not cycling
		{0.3, 0.9, 0.0, 0.3, 0.9, 0.9},  // A and B tie: no label; cycling, but not of A
		{0.1, 0.0, 0.0, 0.5, 0.05, 0.0}, // B; B1 alone, 0.05 above the minimum: stays B
		{-0.2, 0, 0, -0.1, 0.5, 0},      // B leads A by 0.1 but is below the minimum: no label
		{0.0, 0.0, 0.0, 0.4, 0.2, 0.0},  // B, then B1 alone, 0.2 above the minimum
	}
	sc := &Scores{Cells: len(cells)}
	for k := range sets {
		row := make([]float64, len(cells))
		for i := range cells {
			row[i] = cells[i][k]
		}
		sc.Score = append(sc.Score, row)
	}
	a := Assign(sets, 2, sc, p)
	want := [][2]int32{{0, 1}, {0, -1}, {-1, -1}, {3, -1}, {-1, -1}, {3, 4}}
	conf := []float64{0.3, 0.3, 0, 0.4, 0.1, 0.2}
	for i, w := range want {
		if a.Label[0][i] != w[0] || a.Label[1][i] != w[1] || math.Abs(a.Confidence[i]-conf[i]) > 1e-12 {
			t.Errorf("cell %d: labels %d %d, confidence %v; want %v, %v", i, a.Label[0][i], a.Label[1][i], a.Confidence[i], w, conf[i])
		}
	}
	if !math.IsNaN(a.Lead[1][2]) || math.Abs(a.Lead[1][1]-0.05) > 1e-12 || math.Abs(a.Lead[1][3]-0.05) > 1e-12 {
		t.Errorf("leads at the second level: %v", a.Lead[1])
	}
	if !slices.Equal(a.State[0], []bool{true, false, false, false, false, false}) {
		t.Errorf("cycling %v", a.State[0])
	}
	if a.Deepest(0) != 1 || a.Deepest(1) != 0 || a.Deepest(2) != -1 || a.At(0, 0) != 0 || a.At(1, 1) != 0 {
		t.Error("the label carried")
	}
}

// tissue is a planted tissue of two classes of two types each, on a panel of 200 genes: each
// class's genes expressed by both its types, each type's own genes, background elsewhere; cells of
// different depths.
func tissue(cells int, seed uint64) (*segment.Counts, []int) {
	s := random.New(seed, 0)
	var genes []string
	for g := 0; g < 200; g++ {
		genes = append(genes, fmt.Sprintf("G%02d", g))
	}
	truth := make([]int, cells)
	var rows [][]uint32
	for i := 0; i < cells; i++ {
		t := i % 4
		truth[i] = t
		depth := 0.5 + s.Float64()
		row := make([]uint32, len(genes))
		for g := range genes {
			mean := 0.3
			switch {
			case g < 8 && g/4 == t/2: // the class's genes: 0–3, 4–7
				mean = 3
			case g >= 8 && g < 16 && (g-8)/2 == t: // the type's: 8–9, 10–11, 12–13, 14–15
				mean = 3
			}
			row[g] = uint32(s.NegativeBinomial(mean*depth, 2))
		}
		rows = append(rows, row)
	}
	return counts(genes, rows), truth
}

const planted = `{"format": "situcraft-markers", "version": 1, "name": "Planted", "levels": ["Class", "Type"],
 "labels": [
  {"name": "One", "positive": ["G00", "G01", "G02", "G03"], "negative": ["G04", "G05"]},
  {"name": "One a", "parent": "One", "positive": ["G08", "G09"]},
  {"name": "One b", "parent": "One", "positive": ["G10", "G11"]},
  {"name": "Two", "positive": ["G04", "G05", "G06", "G07"]},
  {"name": "Two a", "parent": "Two", "positive": ["G12", "G13"]},
  {"name": "Two b", "parent": "Two", "positive": ["G14", "G15"]}
 ],
 "states": [{"name": "High G16", "positive": ["G16"]}]}`

func TestPlantedTypesAreLabeled(t *testing.T) {
	c, truth := tissue(800, 20250)
	var cells, clusters []uint32
	for i := range truth {
		cells = append(cells, uint32(i))
		clusters = append(clusters, uint32(truth[i]/2))
	}
	e, err := cluster.LoadExpression(c, cells, clusters, 100)
	if err != nil {
		t.Fatal(err)
	}
	// Two of a type's genes, of negative-binomial counts (r = 2) of mean 1.5 to 4.5, are both 0 in
	// a cell often enough that it stays at its class: the classes are near certain, the types
	// mostly, the wrong few; with the controls from every gene as with the lineage's left out.
	var r *Result
	for _, lineage := range []bool{false, true} {
		p := DefaultParams()
		p.Lineage = lineage
		var err error
		if r, err = Run(e, set(planted), 2, 100, p); err != nil {
			t.Fatal(err)
		}
		names := []string{"One a", "One b", "Two a", "Two b"}
		class, right, wrong := 0, 0, 0
		for i, tt := range truth {
			if k := r.Assignment.Label[0][i]; k >= 0 && r.Sets[k].Name == []string{"One", "Two"}[tt/2] {
				class++
			}
			switch k := r.Assignment.Label[1][i]; {
			case k < 0:
			case r.Sets[k].Name == names[tt]:
				right++
			default:
				wrong++
			}
		}
		n := float64(len(truth))
		if float64(class)/n < 0.97 || float64(right)/n < 0.88 || float64(wrong)/n > 0.05 {
			t.Errorf("lineage %v: of the cells %.3f labeled with their class, %.3f with their type, %.3f with another type", lineage, float64(class)/n, float64(right)/n, float64(wrong)/n)
		}
	}
	dir := t.TempDir()
	seg, clu := cluster.Side{ID: "seg-1", Name: "Planted"}, cluster.Side{ID: "clu-1", Name: "Clusters"}
	if err := Write(dir, seg, clu, r); err != nil {
		t.Fatal(err)
	}
	var m artifacts.Manifest
	if findings := Check(dir, &m); len(findings) != 0 {
		t.Fatalf("check: %+v", findings)
	}
	var s Summary
	json.Unmarshal(m.Summary, &s)
	if s.Cells != 800 || s.Labels != 6 || s.States != 1 || s.Labeled[0]+s.Unlabeled != 800 {
		t.Errorf("summary %+v", s)
	}
	d, _ := ReadDescription(dir)
	for c := 0; c < 2; c++ {
		total := 0
		for _, n := range d.Composition[1][c] {
			total += n
		}
		if total != 400 {
			t.Errorf("cluster %d's composition counts %d cells", c, total)
		}
	}
	if d.Sets[0].PositiveControls == nil || d.Markers.Checksum != set(planted).Checksum() {
		t.Error("the description lacks the controls or the marker set's checksum")
	}
	digest, err := Digest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if digest != labelsDigest {
		t.Errorf("digest %s, recorded %s", digest, labelsDigest)
	}
	// A label changed in the description, and a cell moved to a label that does not lead, are caught.
	raw, _ := os.ReadFile(filepath.Join(dir, FileDescription))
	os.WriteFile(filepath.Join(dir, FileDescription), []byte(strings.Replace(string(raw), `"name": "One b"`, `"name": "One c"`, 1)), 0o644)
	if findings := Check(dir, &m); len(findings) == 0 {
		t.Error("a renamed label passed the check")
	}
	os.WriteFile(filepath.Join(dir, FileDescription), raw, 0o644)
	r.Assignment.Label[1][0] = 2 // cell 0 is of type One a
	if err := Write(dir, seg, clu, r); err != nil {
		t.Fatal(err)
	}
	if findings := Check(dir, &m); len(findings) == 0 || findings[0].Code != "labels.assignment" {
		t.Errorf("a cell given a label that is not its best passed the check: %+v", findings)
	}
}

// The simulated tissue's set: each type's markers as the simulation made them.
func TestTheSimulatedSet(t *testing.T) {
	p := simulate.Presets["standard"]
	var types []string
	for _, ty := range p.Types {
		types = append(types, ty.Name)
	}
	s := Simulated(types, simulate.MarkerNames(p))
	if err := s.Check(); err != nil {
		t.Fatal(err)
	}
	if len(s.Labels) != 4 || s.Labels[0].Positive[0] != "SIM001" || s.Labels[1].Positive[0] != "SIM009" || len(s.Labels[3].Positive) != 6 || s.Labels[3].Positive[5] != "SIM030" {
		t.Errorf("simulated set %+v", s.Labels)
	}
}

// The sets SituCraft ships parse, cite what they list, and name only their panel's genes (the
// panels are checked against the development bundles in validation/).
func TestStarters(t *testing.T) {
	st := Starters()
	if len(st) != 2 || st[0].ID != "breast" || st[1].ID != "lung" {
		t.Fatalf("starters %v", st)
	}
	for _, s := range st {
		if len(s.Set.References) == 0 || s.Set.Panel == "" {
			t.Errorf("%s: no references or panel", s.ID)
		}
		for _, l := range append(append([]Label(nil), s.Set.Labels...), s.Set.States...) {
			if len(l.References) == 0 {
				t.Errorf("%s: %s cites nothing", s.ID, l.Name)
			}
		}
	}
}
