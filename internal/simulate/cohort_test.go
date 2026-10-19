package simulate

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"situcraft/internal/dmath"
)

// tinyCohort is the standard cohort with one subject a group and sections of 120 µm: quick to
// write.
func tinyCohort() Cohort {
	c := standardCohort()
	c.Subjects = 1
	c.Section.WidthUm, c.Section.HeightUm = 120, 120
	return c
}

// pinnedCohort is tinyCohort's folder (written by version "test"), the same on every processor: a
// change here is a change to what the cohort simulator makes, which must be deliberate.
const pinnedCohort = "2f4d542a90d06a99"

// The same cohort gives the same folder, byte for byte, with any number of workers.
func TestCohortIsReproducible(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if _, err := RunCohort(context.Background(), tinyCohort(), a, "test", nil); err != nil {
		t.Fatal(err)
	}
	was := runtime.GOMAXPROCS(1)
	_, err := RunCohort(context.Background(), tinyCohort(), b, "test", nil)
	runtime.GOMAXPROCS(was)
	if err != nil {
		t.Fatal(err)
	}
	da, fa := digest(t, a)
	db, fb := digest(t, b)
	if da != db {
		for name, sum := range fa {
			if fb[name] != sum {
				t.Errorf("%s differs", name)
			}
		}
	}
	if da != pinnedCohort {
		t.Errorf("the cohort's digest is %s, not %s", da, pinnedCohort)
	}
}

// Unpaired: subjects numbered across the groups, the slides dealt within each group; paired: a
// subject's samples together, on one slide.
func TestCohortDesign(t *testing.T) {
	c := standardCohort()
	var got []string
	for _, s := range c.design() {
		got = append(got, s.Name+" "+s.Subject+" "+s.Group+" "+s.Slide)
	}
	want := []string{
		"S01 S01 control simulated slide 1", "S02 S02 control simulated slide 2", "S03 S03 control simulated slide 1", "S04 S04 control simulated slide 2",
		"S05 S05 treated simulated slide 1", "S06 S06 treated simulated slide 2", "S07 S07 treated simulated slide 1", "S08 S08 treated simulated slide 2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unpaired:\n%s", strings.Join(got, "\n"))
	}
	p := pairedCohort()
	got = nil
	for _, s := range p.design() {
		got = append(got, s.Name+" "+s.Subject+" "+s.Group+" "+s.Slide)
	}
	want = []string{
		"S01-before S01 before simulated slide 1", "S01-after S01 after simulated slide 1", "S02-before S02 before simulated slide 2", "S02-after S02 after simulated slide 2",
		"S03-before S03 before simulated slide 1", "S03-after S03 after simulated slide 1", "S04-before S04 before simulated slide 2", "S04-after S04 after simulated slide 2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("paired:\n%s", strings.Join(got, "\n"))
	}
	c.Slides = 0
	if s := c.design(); s[0].Slide != "" || c.slideEfficiency(s[0].Slide) != 1 {
		t.Errorf("without slides: %+v", s[0])
	}
}

// The folder: each sample a bundle naming its region, run and slide, the panel the same in every
// one; samples.csv; situcraft-cohort.json with the parameters as given; the truth's counts the
// bundles' own.
func TestCohortFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cohort")
	c := tinyCohort()
	sum, err := RunCohort(context.Background(), c, dir, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Samples) != 2 {
		t.Fatalf("%d samples", len(sum.Samples))
	}
	table, _ := os.ReadFile(filepath.Join(dir, CohortSamplesFile))
	if string(table) != "sample,subject,condition\nS01,S01,control\nS02,S02,treated\n" {
		t.Errorf("samples.csv:\n%s", table)
	}
	var desc struct {
		Format     string   `json:"format"`
		Simulated  bool     `json:"simulated"`
		Samples    []Sample `json:"samples"`
		Parameters Cohort   `json:"parameters"`
	}
	raw, _ := os.ReadFile(filepath.Join(dir, CohortFile))
	if err := json.Unmarshal(raw, &desc); err != nil {
		t.Fatal(err)
	}
	asGiven, _ := json.Marshal(c)
	asRead, _ := json.Marshal(desc.Parameters)
	if desc.Format != "situcraft-cohort" || !desc.Simulated || len(desc.Samples) != 2 || !bytes.Equal(asGiven, asRead) {
		t.Errorf("situcraft-cohort.json: %s", raw[:200])
	}
	var panel []byte
	for k, s := range sum.Samples {
		raw, err := os.ReadFile(filepath.Join(dir, s.Name, "experiment.xenium"))
		if err != nil {
			t.Fatal(err)
		}
		var run map[string]any
		json.Unmarshal(raw, &run)
		if run["region_name"] != s.Name || run["slide_id"] != s.Slide || run["run_name"] != strings.Replace(s.Slide, "slide", "run", 1) {
			t.Errorf("%s: %v %v %v", s.Name, run["region_name"], run["slide_id"], run["run_name"])
		}
		gp, _ := os.ReadFile(filepath.Join(dir, s.Name, "gene_panel.json"))
		if k == 0 {
			panel = gp
		} else if !bytes.Equal(gp, panel) {
			t.Errorf("%s's panel differs from %s's", s.Name, sum.Samples[0].Name)
		}
		var made struct {
			Parameters Params `json:"parameters"`
			Made       struct {
				Cells       int            `json:"cells"`
				CellsByType map[string]int `json:"cells_by_type"`
			} `json:"made"`
		}
		raw, _ = os.ReadFile(filepath.Join(dir, s.Name, SimulationFile))
		json.Unmarshal(raw, &made)
		truth := sum.Truth.Samples[k]
		if made.Parameters.PanelSeed != c.Seed || made.Made.Cells != truth.Cells || !reflect.DeepEqual(made.Made.CellsByType, truth.ByType) {
			t.Errorf("%s: the bundle says %d cells %v, the truth %d %v", s.Name, made.Made.Cells, made.Made.CellsByType, truth.Cells, truth.ByType)
		}
	}
	var written CohortTruth
	raw, _ = os.ReadFile(filepath.Join(dir, CohortTruthFile))
	if err := json.Unmarshal(raw, &written); err != nil || len(written.Samples) != 2 || len(written.Effects) != 3 || written.Design.Slides[0] != "simulated slide 1" {
		t.Errorf("truth/cohort.json: %v", err)
	}
}

// Over a cohort of 12 subjects a group, every effect as the true cells show it is the one asked,
// within three standard errors; and the null cohort's groups differ in no type's share.
func TestCohortEffectsAreAsAsked(t *testing.T) {
	if testing.Short() {
		t.Skip("simulates 48 sections")
	}
	c := standardCohort()
	c.Subjects = 12
	c.Section.WidthUm, c.Section.HeightUm = 200, 200
	truth, err := c.trueCohort()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range truth.Effects {
		z := (e.Measured - e.LogFold) / e.StdError
		t.Logf("%s of %s%s%s ×%g: ×%.2f (log %.3f ± %.3f, z %.2f)", e.Kind, e.Type, map[bool]string{true: " within " + e.Within}[e.Within != ""], map[bool]string{true: " " + e.Gene}[e.Gene != ""], e.Fold, dmath.Exp(e.Measured), e.Measured, e.StdError, z)
		if math.Abs(z) > 3 || e.StdError <= 0 {
			t.Errorf("%s of %s: measured log fold %.3f ± %.3f, asked %.3f", e.Kind, e.Type, e.Measured, e.StdError, e.LogFold)
		}
	}
	null := nullCohort()
	null.Subjects = 12
	null.Section.WidthUm, null.Section.HeightUm = 200, 200
	nt, err := null.trueCohort()
	if err != nil {
		t.Fatal(err)
	}
	for _, ty := range null.Section.Types {
		e := null.measure(Effect{Kind: EffectShare, Group: "treated", Type: ty.Name, Fold: 1}, nt.Samples)
		if math.Abs(e.Measured/e.StdError) > 3 {
			t.Errorf("null: %s's log odds differ by %.3f ± %.3f", ty.Name, e.Measured, e.StdError)
		}
	}
}

// A sample's level is its asked fold exactly: the gene's log odds among its type's transcripts,
// less the log of the fold asked of the sample, is the same in every sample but for counting.
func TestCohortLevelIsExact(t *testing.T) {
	c := standardCohort()
	truth, err := c.trueCohort()
	if err != nil {
		t.Fatal(err)
	}
	var rest []float64
	for _, s := range truth.Samples {
		l := s.Levels[0]
		v := dmath.Log((float64(l.Transcripts) + 0.5) / (float64(l.TypeTotal-l.Transcripts) + 0.5))
		rest = append(rest, v-dmath.Log(l.Fold))
		// Counting's own spread: about one over the square root of the count.
		if l.Transcripts < 300 {
			t.Errorf("%s: only %d transcripts of %s", s.Name, l.Transcripts, l.Gene)
		}
	}
	if _, sd := meanSD(rest); sd > 0.1 {
		t.Errorf("the gene's log odds less the asked fold vary by %.3f across samples: %v", sd, rest)
	}
}

// The variation is as asked: over 100 samples, each type's log weight (less the effects) spreads by
// share_sd and each gene's log level by level_sd; in a paired design a subject's samples correlate
// by the subject share, unpaired samples not at all.
func TestCohortVariationIsAsAsked(t *testing.T) {
	c := standardCohort()
	c.Subjects = 50
	var shares, levels []float64
	for k, s := range c.design() {
		p := c.sampleParams(k, s)
		for i, v := range p.TypeOdds {
			if s.Group == "treated" && p.Types[i].Name == "endothelial-like" {
				v /= 2
			}
			shares = append(shares, dmath.Log(v))
		}
		for i := range p.LevelFolds {
			for g, v := range p.LevelFolds[i] {
				if s.Group == "treated" && p.Types[i].Name == "epithelial-like" && GeneName(g) == CohortGene {
					continue
				}
				levels = append(levels, dmath.Log(v))
			}
		}
	}
	if m, sd := meanSD(shares); math.Abs(sd-c.ShareSD) > 0.15*c.ShareSD || math.Abs(m) > 0.1 {
		t.Errorf("the types' log weights: mean %.3f, spread %.3f (asked %g)", m, sd, c.ShareSD)
	}
	if m, sd := meanSD(levels); math.Abs(sd-c.LevelSD) > 0.02*c.LevelSD || math.Abs(m) > 0.01 {
		t.Errorf("the genes' log levels: mean %.4f, spread %.4f (asked %g)", m, sd, c.LevelSD)
	}
	correlation := func(c Cohort) float64 {
		samples := c.design()
		var x, y []float64
		for k := 0; k+1 < len(samples); k += 2 {
			a, b := c.sampleParams(k, samples[k]), c.sampleParams(k+1, samples[k+1])
			for i := range a.LevelFolds {
				for g := range a.LevelFolds[i] {
					if GeneName(g) == CohortGene {
						continue
					}
					x = append(x, dmath.Log(a.LevelFolds[i][g]))
					y = append(y, dmath.Log(b.LevelFolds[i][g]))
				}
			}
		}
		mx, sx := meanSD(x)
		my, sy := meanSD(y)
		var s float64
		for i := range x {
			s += mul(x[i]-mx, y[i]-my)
		}
		return s / float64(len(x)-1) / (sx * sy)
	}
	p := pairedCohort()
	p.Subjects = 50
	if r := correlation(p); math.Abs(r-p.SubjectShare) > 0.03 {
		t.Errorf("a subject's samples correlate by %.3f, asked %g", r, p.SubjectShare)
	}
	if r := correlation(c); math.Abs(r) > 0.03 {
		t.Errorf("unpaired samples correlate by %.3f", r)
	}
}

func TestCohortIsChecked(t *testing.T) {
	cases := map[string]func(c *Cohort){
		"2 to 10 groups":            func(c *Cohort) { c.Groups = []string{"one"} },
		"groups of different names": func(c *Cohort) { c.Groups = []string{"a", "A"}; c.Effects = nil },
		"a group's name":            func(c *Cohort) { c.Groups = []string{"control", "treated/x"}; c.Effects = nil },
		"1 to 200 subjects":         func(c *Cohort) { c.Subjects = 0 },
		"at most 500 samples":       func(c *Cohort) { c.Subjects = 200; c.Groups = []string{"a", "b", "c"}; c.Effects = nil },
		"spreads of 0 to 3":         func(c *Cohort) { c.ShareSD = -1 },
		"subject share":             func(c *Cohort) { c.SubjectShare = 2 },
		"other than the first":      func(c *Cohort) { c.Effects[0].Group = "control" },
		"of a cell type":            func(c *Cohort) { c.Effects[0].Type = "neuron-like" },
		"within the domains":        func(c *Cohort) { c.Effects[1].Within = "immune-like" },
		"of a gene of the panel":    func(c *Cohort) { c.Effects[2].Gene = "SIM999" },
		"a fold above 0":            func(c *Cohort) { c.Effects[2].Fold = 0 },
		"of kind share or level":    func(c *Cohort) { c.Effects[0].Kind = "spread" },
		"a sample's own settings":   func(c *Cohort) { c.Section.SlideID = "x" },
		"without post-Xenium":       func(c *Cohort) { c.Section.PostXenium = DefaultPostXenium() },
	}
	for want, change := range cases {
		c := standardCohort()
		c.Effects = append([]Effect(nil), c.Effects...)
		change(&c)
		if err := c.Check(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
	for name, c := range CohortPresets {
		if err := c.Check(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := RunCohort(context.Background(), standardCohort(), t.TempDir(), "test", nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("an existing folder: %v", err)
	}
}
