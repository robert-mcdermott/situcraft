package label

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"situcraft/internal/anndata"
	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/random"
	"situcraft/internal/segment"
	"situcraft/internal/zarr"
)

// handRef is three labels over four genes:
//
//	A  5 1 0 2
//	B  1 4 0 2
//	C  0 0 3 2.5
func handRef() *Ref {
	return &Ref{Labels: []string{"A", "B", "C"}, Cells: []int{0, 0, 0}, Genes: []int{0, 1, 2, 3},
		Profiles: [][]float64{{5, 1, 0, 2}, {1, 4, 0, 2}, {0, 0, 3, 2.5}}}
}

func TestRanksAndSpearmanByHand(t *testing.T) {
	if r := rankAvg([]float64{3, 1, 3, 0, 1}); !slices.Equal(r, []float64{4.5, 2.5, 4.5, 1, 2.5}) {
		t.Errorf("ranks %v", r)
	}
	// Over genes 0–3, A's profile ranks 4 2 1 3; a cell of 5 0 0 1 ranks 4 1.5 1.5 3: centred,
	// 1.5 −0.5 −1.5 0.5 and 1.5 −1 −1 0.5, whose products sum to 4.5, over √(5 × 4.5): √0.9.
	c := handRef().compare([]int{0}, [][][]int{{{0, 1, 2, 3}}})
	out := make([]float64, 1)
	c.correlate([]int{0, 3}, []float64{5, 1}, out)
	if math.Abs(out[0]-math.Sqrt(0.9)) > 1e-15 {
		t.Errorf("Spearman %v, by hand %v", out[0], math.Sqrt(0.9))
	}
	// A cell expressing none of the genes, or all of them equally, has no ranks to correlate.
	c.correlate(nil, nil, out)
	if !math.IsNaN(out[0]) {
		t.Errorf("a cell of zeros correlates %v", out[0])
	}
}

// Each ordered pair's genes: those most higher in the first label's profile, two of them, only
// those higher at all.
func TestPairMarkersByHand(t *testing.T) {
	p := handRef().pairMarkers(2)
	want := [][][]int{
		{nil, {0}, {0, 1}},    // A over B: 4, −3, 0, 0; over C: 5, 1, −3, −0.5
		{{1}, nil, {1, 0}},    // B over A: g1 alone; over C: 1, 4, −3, −0.5
		{{2, 3}, {2, 3}, nil}, // C over A: −5, −1, 3, 0.5; over B: −1, −4, 3, 0.5
	}
	for a := range want {
		for b := range want[a] {
			if !slices.Equal(p[a][b], want[a][b]) {
				t.Errorf("%d over %d: %v, by hand %v", a, b, p[a][b], want[a][b])
			}
		}
	}
	if n := (RefParams{}).MarkersPer(37); n != 60 {
		t.Errorf("genes per pair for 37 labels %d: 500 × (2/3)^log₂37 is 60.5", n)
	}
}

// A cell of 1 2 0 3 ranks 2 3 1 4: against A (4 2 1 3) 0.4, B (2 4 1 3) 0.8, C (1.5 1.5 4 3)
// −1.5/√22.5. Within 0.05 of the best, B alone: one round. Within 0.5, A and B are scored again on
// their own genes (0 and 1), where the cell (1 2) ranks as B (1 4) does and against A (5 1).
func TestFineTuningByHand(t *testing.T) {
	ref := handRef()
	mp, err := NewMapper(ref, 4, RefParams{Tune: 0.05, Markers: 2})
	if err != nil {
		t.Fatal(err)
	}
	m := mp.Map([]int{0, 1, 3}, []float64{1, 2, 3})
	want := []float64{0.4, 0.8, -1.5 / math.Sqrt(22.5)}
	if len(m.Rounds) != 1 || m.Label != 1 || math.Abs(m.Lead-0.4) > 1e-15 {
		t.Fatalf("%+v", m)
	}
	for k, s := range m.Rounds[0].Scores {
		if math.Abs(s-want[k]) > 1e-15 {
			t.Errorf("label %d: %v, by hand %v", k, s, want[k])
		}
	}
	mp, _ = NewMapper(ref, 4, RefParams{Tune: 0.5, Markers: 2})
	m = mp.Map([]int{0, 1, 3}, []float64{1, 2, 3})
	if len(m.Rounds) != 2 || !slices.Equal(m.Rounds[1].Labels, []int{0, 1}) || !slices.Equal(m.Rounds[1].Genes, []int{0, 1}) ||
		math.Abs(m.Rounds[1].Scores[0]+1) > 1e-15 || math.Abs(m.Rounds[1].Scores[1]-1) > 1e-15 || m.Label != 1 || math.Abs(m.Lead-2) > 1e-15 {
		t.Fatalf("fine-tuned: %+v", m)
	}
	// A margin above the lead leaves the cell without a label.
	mp, _ = NewMapper(ref, 4, RefParams{Tune: 0.05, Margin: 0.5, Markers: 2})
	if m := mp.Map([]int{0, 1, 3}, []float64{1, 2, 3}); m.Label != -1 || math.Abs(m.Lead-0.4) > 1e-15 {
		t.Errorf("below the margin: %+v", m)
	}
}

// dense ranks two vectors in full and correlates them.
func dense(x, y []float64) float64 {
	rx, ry := rankAvg(x), rankAvg(y)
	m := float64(len(x)+1) / 2
	var dot, sx, sy float64
	for i := range rx {
		dot += (rx[i] - m) * (ry[i] - m)
		sx += (rx[i] - m) * (rx[i] - m)
		sy += (ry[i] - m) * (ry[i] - m)
	}
	return dot / math.Sqrt(sx*sy)
}

// The mapping against a plain one: every label's genes compared by sorting all pairs afresh, each
// correlation from the cell's every value; random references and sparse cells.
func TestMappingAgainstDense(t *testing.T) {
	s := random.New(7, 0)
	switched := 0
	for trial := 0; trial < 20; trial++ {
		L, G := 3+s.IntN(6), 20+s.IntN(40)
		ref := &Ref{Cells: make([]int, L)}
		for k := 0; k < L; k++ {
			ref.Labels = append(ref.Labels, string(rune('A'+k)))
			p := make([]float64, G)
			for g := range p {
				if s.Float64() < 0.6 {
					p[g] = math.Round(s.Gamma(1, 1)*4) / 4 // ties among the profile's values
				}
			}
			ref.Profiles = append(ref.Profiles, p)
		}
		for g := 0; g < G; g++ {
			ref.Genes = append(ref.Genes, g)
		}
		p := RefParams{Tune: 0.1, Markers: 3 + s.IntN(8)}
		mp, err := NewMapper(ref, G, p)
		if err != nil {
			t.Fatal(err)
		}
		for cell := 0; cell < 50; cell++ {
			var genes []int
			var values []float64
			full := make([]float64, G)
			for g := 0; g < G; g++ {
				if s.Float64() < 0.3 {
					genes = append(genes, g)
					full[g] = math.Log1p(float64(1 + s.IntN(4)))
					values = append(values, full[g])
				}
			}
			m := mp.Map(genes, values)
			// Plain: from all labels, keep those within Tune until one is left or none drops.
			labels := make([]int, L)
			for k := range labels {
				labels[k] = k
			}
			for r := 0; ; r++ {
				in := map[int]bool{}
				for _, a := range labels {
					for _, b := range labels {
						if a == b {
							continue
						}
						order := make([]int, G)
						for g := range order {
							order[g] = g
						}
						slices.SortStableFunc(order, func(x, y int) int {
							dx, dy := ref.Profiles[a][x]-ref.Profiles[b][x], ref.Profiles[a][y]-ref.Profiles[b][y]
							switch {
							case dx > dy:
								return -1
							case dx < dy:
								return 1
							}
							return 0
						})
						for _, g := range order[:p.Markers] {
							if ref.Profiles[a][g] > ref.Profiles[b][g] {
								in[g] = true
							}
						}
					}
				}
				var gs []int
				for g := 0; g < G; g++ {
					if in[g] {
						gs = append(gs, g)
					}
				}
				var top float64 = math.Inf(-1)
				scores := make([]float64, len(labels))
				for j, l := range labels {
					x, y := make([]float64, len(gs)), make([]float64, len(gs))
					for k, g := range gs {
						x[k], y[k] = full[g], ref.Profiles[l][g]
					}
					scores[j] = dense(x, y)
					if !math.IsNaN(scores[j]) {
						top = math.Max(top, scores[j])
					}
				}
				if r >= len(m.Rounds) {
					if !math.IsInf(top, -1) && !(r == 0) {
						t.Fatalf("trial %d, cell %d: %d rounds; the plain mapping goes on", trial, cell, len(m.Rounds))
					}
					break
				}
				round := m.Rounds[r]
				if !slices.Equal(round.Labels, labels) || !slices.Equal(round.Genes, gs) {
					t.Fatalf("trial %d, cell %d, round %d: labels %v genes %v; plain %v %v", trial, cell, r, round.Labels, round.Genes, labels, gs)
				}
				for j := range scores {
					if d := math.Abs(round.Scores[j] - scores[j]); d > 1e-12 || math.IsNaN(round.Scores[j]) != math.IsNaN(scores[j]) {
						t.Fatalf("trial %d, cell %d, round %d, label %d: %v; plain %v", trial, cell, r, labels[j], round.Scores[j], scores[j])
					}
				}
				var keep []int
				for j, sc := range scores {
					if sc >= top-p.Tune {
						keep = append(keep, labels[j])
					}
				}
				if len(keep) <= 1 || len(keep) == len(labels) {
					if r != len(m.Rounds)-1 {
						t.Fatalf("trial %d, cell %d: %d rounds; the plain mapping stops at %d", trial, cell, len(m.Rounds), r+1)
					}
					break
				}
				labels = keep
			}
			if len(m.Rounds) > 1 && m.Label >= 0 {
				first := m.Rounds[0]
				best := 0
				for j := range first.Scores {
					if first.Scores[j] > first.Scores[best] {
						best = j
					}
				}
				if first.Labels[best] != m.Label {
					switched++
				}
			}
		}
	}
	if switched == 0 {
		t.Error("fine-tuning never changed a cell's label from its first round's best")
	}
	t.Logf("%d cells changed label in fine-tuning", switched)
}

// writeReference writes an AnnData reference: its cells' counts (rows), labels and genes.
func writeReference(t *testing.T, path string, rows [][]float64, labels, genes []string) {
	t.Helper()
	w, err := zarr.NewWriter(path, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	o := &anndata.Object{}
	for i := range rows {
		o.Obs.Index = append(o.Obs.Index, "cell"+string(rune('a'+i%26))+strings.Repeat("x", i/26))
	}
	o.Obs.Columns = []anndata.WriteColumn{{Name: "cell_type", Text: labels, Categorical: true}, {Name: "donor", Text: slices.Repeat([]string{"d1"}, len(rows)), Categorical: true}}
	o.Var.Index = genes
	o.X.Indptr = []int64{0}
	for _, row := range rows {
		for g, v := range row {
			if v != 0 {
				o.X.Indices = append(o.X.Indices, int32(g))
				o.X.Data = append(o.X.Data, float32(v))
			}
		}
		o.X.Indptr = append(o.X.Indptr, int64(len(o.X.Data)))
	}
	if err := anndata.Write(w, "", o); err != nil {
		t.Fatal(err)
	}
}

// An AnnData reference of counts: each label's profile its cells' mean of log1p(counts scaled to
// the median total), over the panel's genes it has (matched ignoring case); the panel's others
// listed; cells without a label left out.
func TestReferenceFromAnnData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ref.zarr")
	// Totals 4, 2, 6, 2: median 3.
	rows := [][]float64{{2, 0, 2, 0}, {0, 1, 0, 1}, {3, 3, 0, 0}, {1, 1, 0, 0}}
	writeReference(t, path, rows, []string{"T cell", "B cell", "T cell", "nan"}, []string{"cd3e", "MS4A1", "Other", "CD19"})
	panel := []string{"CD3E", "MS4A1", "EPCAM", "CD19"}
	d, err := DescribeReference(path, panel)
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != RefAnnData || d.Rows != 4 || len(d.Matrices) != 1 || d.Matrices[0].Values != ValuesCounts || d.Suggested.LabelColumn != "cell_type" || d.Suggested.GeneColumn != "" || d.Genes[0].Shared != 3 {
		t.Errorf("described %+v", d)
	}
	ref, err := ReadReference(RefSpec{Path: path, Matrix: "X", LabelColumn: "cell_type"}, panel)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ref.Labels, []string{"B cell", "T cell"}) || !slices.Equal(ref.Cells, []int{1, 2}) || !slices.Equal(ref.Genes, []int{0, 1, 3}) ||
		!slices.Equal(ref.Missing, []string{"EPCAM"}) || ref.Values != ValuesCounts || ref.RefGenes != 4 || ref.RefCells != 4 || len(ref.SHA256) != 64 {
		t.Fatalf("read %+v", ref)
	}
	l := func(c, total float64) float64 { return math.Log(1 + c/(total/3)) }
	want := [][]float64{
		{0, l(1, 2), l(1, 2)},                     // B cell: the second cell
		{(l(2, 4) + l(3, 6)) / 2, l(3, 6) / 2, 0}, // T cell: the first and third
	}
	for k := range want {
		for g := range want[k] {
			if math.Abs(ref.Profiles[k][g]-want[k][g]) > 1e-15 {
				t.Errorf("%s, gene %d: %v, by hand %v", ref.Labels[k], g, ref.Profiles[k][g], want[k][g])
			}
		}
	}
	if _, err := ReadReference(RefSpec{Path: path, Matrix: "X", LabelColumn: "donor"}, panel); err == nil || !strings.Contains(err.Error(), "1 labels") {
		t.Errorf("one label: %v", err)
	}
	if _, err := ReadReference(RefSpec{Path: path, Matrix: "obsm/x", LabelColumn: "cell_type"}, panel); err == nil {
		t.Error("read a matrix that is not one")
	}
}

// Tables of profiles in either orientation read the same; scaled values and a label given twice
// are refused.
func TestReferenceFromProfiles(t *testing.T) {
	dir := t.TempDir()
	panel := []string{"CD3E", "MS4A1", "EPCAM"}
	byLabels := filepath.Join(dir, "labels.csv")
	os.WriteFile(byLabels, []byte("cell_type,CD3E,MS4A1,KRT8\nT cell,2.5,0.1,0\nB cell,0.2,3,0\nEpithelial,0,0,4\n"), 0o644)
	byGenes := filepath.Join(dir, "genes.csv")
	os.WriteFile(byGenes, []byte("gene,T cell,B cell,Epithelial\nKRT8,0,0,4\nMS4A1,0.1,3,0\nCD3E,2.5,0.2,\n"), 0o644)
	d1, err := DescribeReference(byLabels, panel)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := DescribeReference(byGenes, panel)
	if err != nil {
		t.Fatal(err)
	}
	if d1.Suggested.LabelColumn != "cell_type" || d1.Suggested.GeneColumn != "" || d1.ColumnGenes != 2 || d2.Suggested.GeneColumn != "gene" || d2.Suggested.LabelColumn != "" {
		t.Errorf("suggested %+v, %+v", d1.Suggested, d2.Suggested)
	}
	a, err := ReadReference(d1.Suggested, panel)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ReadReference(d2.Suggested, panel)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []*Ref{a, b} {
		if r.Format != RefProfiles || !slices.Equal(r.Labels, []string{"B cell", "Epithelial", "T cell"}) || !slices.Equal(r.Genes, []int{0, 1}) || !slices.Equal(r.Missing, []string{"EPCAM"}) ||
			!slices.Equal(r.Profiles[2], []float64{2.5, 0.1}) || !slices.Equal(r.Profiles[1], []float64{0, 0}) {
			t.Errorf("%s: %+v %v", r.File, r, r.Profiles)
		}
	}
	scaled := filepath.Join(dir, "scaled.csv")
	os.WriteFile(scaled, []byte("cell_type,CD3E,MS4A1\nT,1.2,-0.4\nB,-0.8,1.1\n"), 0o644)
	if _, err := ReadReference(RefSpec{Path: scaled, LabelColumn: "cell_type"}, panel); err == nil || !strings.Contains(err.Error(), "scaled") {
		t.Errorf("scaled: %v", err)
	}
	twice := filepath.Join(dir, "twice.csv")
	os.WriteFile(twice, []byte("cell_type,CD3E,MS4A1\nT,1,0\nT,0,1\n"), 0o644)
	if _, err := ReadReference(RefSpec{Path: twice, LabelColumn: "cell_type"}, panel); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("twice: %v", err)
	}
}

// typed is a tissue of two classes of two types each over 60 genes: each class's mean of each gene
// its own, each type's a third of its class's genes twice or half as high (drawn once from a fixed
// stream); cells of different depths drawn from their type. Answers their counts and true types.
func typed(cells int, seed uint64) (*segment.Counts, []int) {
	ps := random.New(1, 0)
	means := make([][]float64, 4)
	for c := 0; c < 2; c++ {
		class := make([]float64, 60)
		for g := range class {
			class[g] = 3 * ps.Gamma(0.7, 1)
		}
		for k := 2 * c; k < 2*c+2; k++ {
			means[k] = append([]float64(nil), class...)
			for g := range means[k] {
				switch u := ps.Float64(); {
				case u < 1.0/6:
					means[k][g] *= 2
				case u < 1.0/3:
					means[k][g] /= 2
				}
			}
		}
	}
	s := random.New(seed, 0)
	var genes []string
	for g := 0; g < 60; g++ {
		genes = append(genes, fmt.Sprintf("G%02d", g))
	}
	truth := make([]int, cells)
	var rows [][]uint32
	for i := 0; i < cells; i++ {
		truth[i] = i % 4
		depth := 0.2 + 0.8*s.Float64()
		row := make([]uint32, len(genes))
		for g := range genes {
			row[g] = uint32(s.NegativeBinomial(means[truth[i]][g]*depth, 2))
		}
		rows = append(rows, row)
	}
	return counts(genes, rows), truth
}

// A tissue mapped to a reference of another draw of its types: most cells take their type (86%);
// the same labels whatever the workers.
func TestTissueMapsToItsReference(t *testing.T) {
	names := []string{"Alpha", "Beta", "Delta", "Gamma"}
	rc, rtruth := typed(400, 99)
	x := &anndata.Matrix{Rows: 400, Cols: len(rc.Genes), Indptr: []int64{0}}
	var labels []string
	for i := 0; i < 400; i++ {
		for k := rc.Offsets[i]; k < rc.Offsets[i+1]; k++ {
			x.Indices = append(x.Indices, int64(rc.Gene[k]))
			x.Data = append(x.Data, float64(rc.Count[k]))
		}
		x.Indptr = append(x.Indptr, int64(len(x.Data)))
		labels = append(labels, names[rtruth[i]])
	}
	c, truth := typed(800, 20250)
	ref, err := FromMatrix(x, labels, rc.Genes, c.Genes)
	if err != nil {
		t.Fatal(err)
	}
	var cells, clusters []uint32
	for i := range truth {
		cells = append(cells, uint32(i))
		clusters = append(clusters, uint32(truth[i]/2))
	}
	e, err := cluster.LoadExpression(c, cells, clusters, 100)
	if err != nil {
		t.Fatal(err)
	}
	var first []int32
	for _, workers := range []int{1, 4} {
		r, err := MapReference(context.Background(), e, ref, DefaultRefParams(), 2, 100, workers)
		if err != nil {
			t.Fatal(err)
		}
		right, none := 0, 0
		for i, tt := range truth {
			switch k := r.Label[i]; {
			case k < 0:
				none++
			case ref.Labels[k] == names[tt]:
				right++
			}
		}
		if float64(right) < 0.85*800 || none > 8 {
			t.Errorf("%d workers: %d of 800 right, %d without a label", workers, right, none)
		}
		if first == nil {
			first = r.Label
			t.Logf("%d of 800 right, %d without a label", right, none)
		} else if !slices.Equal(first, r.Label) {
			t.Error("the labels differ with the workers")
		}
		if workers > 1 {
			checkReferenceFiles(t, r)
		}
	}
}

// refDigest is the typed tissue's labels from its reference: the same on every processor.
const refDigest = "227e04c58fc20a11efb3e34bf881038b7ad53b47877916480a46bb0f1921d080"

// A mapping's files pass the check and read back; a cell moved to a label that does not lead, and
// a reference renamed, are caught.
func checkReferenceFiles(t *testing.T, r *RefResult) {
	t.Helper()
	dir := t.TempDir()
	seg, clu := cluster.Side{ID: "seg-1", Name: "Typed"}, cluster.Side{ID: "clu-1", Name: "Clusters"}
	if err := WriteReference(dir, seg, clu, r); err != nil {
		t.Fatal(err)
	}
	var m artifacts.Manifest
	if findings := Check(dir, &m); len(findings) != 0 {
		t.Fatalf("check: %+v", findings)
	}
	var s Summary
	json.Unmarshal(m.Summary, &s)
	if s.Cells != 800 || s.Labels != 4 || s.Reference == nil || s.Reference.Shared != 60 || s.Labeled[0]+s.Unlabeled != 800 {
		t.Errorf("summary %+v", s)
	}
	d, set, lab, err := Read(dir)
	if err != nil || set != nil || !slices.Equal(FilesOf(d), []string{FileDescription, FileReference, FileLabels}) || d.FromMarkers() {
		t.Fatalf("read: %v", err)
	}
	if len(lab.Score) != 4 || lab.Score[0][0] != float32(r.First[0][0]) || lab.Label[0][5] != r.Label[5] || d.Reference.PerPair != 222 || len(d.Reference.Compared) == 0 {
		t.Errorf("read back: %+v", d.Reference)
	}
	t.Logf("%d cells fine-tuned, %d changed by it", d.Reference.FineTuned, d.Reference.Changed)
	checkRefEvidence(t, r, dir, d, lab)
	digest, err := Digest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if digest != refDigest {
		t.Errorf("digest %s, recorded %s", digest, refDigest)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, FileReference))
	os.WriteFile(filepath.Join(dir, FileReference), []byte(strings.Replace(string(raw), `"Beta"`, `"Bets"`, 1)), 0o644)
	if findings := Check(dir, &m); len(findings) == 0 || findings[0].Code != "labels.reference" {
		t.Errorf("a renamed reference label passed the check: %+v", findings)
	}
	os.WriteFile(filepath.Join(dir, FileReference), raw, 0o644)
	keep := r.Label[0]
	r.Label[0] = (keep + 1) % 4
	defer func() { r.Label[0] = keep }()
	if err := WriteReference(dir, seg, clu, r); err != nil {
		t.Fatal(err)
	}
	if findings := Check(dir, &m); len(findings) == 0 || findings[0].Code != "labels.assignment" {
		t.Errorf("a cell given a label that is not its best passed the check: %+v", findings)
	}
}

// A cell's evidence maps it again from reference.json as it was labeled: its first round the
// recorded scores, its label the recorded one; the genes for the best are higher in its profile.
func checkRefEvidence(t *testing.T, r *RefResult, dir string, d *Description, lab *Labels) {
	t.Helper()
	rp, err := ReadReferenceProfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := typed(800, 20250)
	var cells []uint32
	for i := 0; i < 800; i++ {
		cells = append(cells, uint32(i))
	}
	e, err := cluster.LoadExpression(c, cells, r.Cluster, 100)
	if err != nil {
		t.Fatal(err)
	}
	tuned := 0
	for i := 0; i < 800; i += 7 {
		ev, err := CellRefEvidence(e, d, rp, lab, i, 2)
		if err != nil {
			t.Fatal(err)
		}
		if int32(ev.Label) != lab.Label[0][i] || len(ev.Rounds) != int(r.Rounds[i]) || ev.Also == nil || ev.Also.Label != 2 {
			t.Fatalf("cell %d: %+v", i, ev)
		}
		for _, s := range ev.Rounds[0].Scores {
			if float32(s.Score) != lab.Score[s.Label][i] {
				t.Errorf("cell %d, %s: %v, recorded %v", i, s.Name, s.Score, lab.Score[s.Label][i])
			}
		}
		for _, g := range ev.ForBest {
			if g.Best <= g.Next {
				t.Errorf("cell %d: %s is for the best, %v against %v", i, g.Gene, g.Best, g.Next)
			}
		}
		if len(ev.Rounds) > 1 {
			tuned++
		}
	}
	if tuned == 0 {
		t.Error("no cell's evidence shows fine-tuning")
	}
}
