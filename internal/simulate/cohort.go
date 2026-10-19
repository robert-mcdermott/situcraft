package simulate

// Simulated cohorts (situcraft simulate --cohort; wave 7): sections of subjects in groups, each a
// simulated bundle of its own layout, cells and molecules, the panel shared; differing from sample
// to sample by a stated amount, and between groups by known effects; with the truth of the design,
// the effects and each sample's true values.
//
// The design: groups, the first the reference; subjects in each (each subject one sample, in one
// group), or, paired, each subject a sample in every group. Slides as batches: unpaired samples
// dealt to the slides in turn within each group, so that every slide holds samples of every group;
// a paired subject's samples on one slide. Each slide's detection efficiency is log-normal (σ
// slide_sd), its cells' densities and the ambient background multiplied by it.
//
// From sample to sample, on the log scale (normal, from the cohort's own streams):
//   - each type's weight in the domains' mixes (σ share_sd): its odds against each other type, in
//     every domain;
//   - each gene's expected count in each type's cells (σ level_sd).
// In a paired design a subject's samples share a part of each draw: √subject_share times the
// subject's draw plus √(1 − subject_share) times the sample's own.
//
// Effects hold in one group against the reference, multiplying:
//   - share: a type's weight in every domain's mix (its odds against each other type, in every
//     domain), or, with within, in the domains another type leads (how much it mixes into them);
//   - level: a gene's expected count in a type's cells.
//
// The cohort's folder holds situcraft-cohort.json (what it is, its parameters and samples),
// samples.csv (each sample's subject and condition, as SituCraft reads a table of samples),
// truth/cohort.json (the design, the effects and each sample's true values) and a bundle for each
// sample, named by it.

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"situcraft/internal/dmath"
	"situcraft/internal/random"
)

// CohortFile is the file at a simulated cohort's root that says what it is.
const CohortFile = "situcraft-cohort.json"

// CohortTruthFile is the cohort's truth, within it.
const CohortTruthFile = "truth/cohort.json"

// CohortReferenceFile is a cohort's single-cell reference, in its folder, when one is made.
const CohortReferenceFile = "reference.zarr"

// CohortReference writes a single-cell reference of a cohort's types over the panel every one of
// its samples shares (WriteReference: perType cells of each type), to label its samples' cells
// from.
func CohortReference(c Cohort, path string, perType int) error {
	return WriteReference(c.referenceParams(), path, perType, ReferenceDepth, c.Seed)
}

// referenceParams are the parameters a cohort's reference is drawn from: its section's, with the
// panel's seed every sample's.
func (c *Cohort) referenceParams() Params {
	p := c.Section
	p.Seed, p.PanelSeed = c.Seed, c.panelSeed()
	return p
}

// CohortSamplesFile is the samples' table, within it.
const CohortSamplesFile = "samples.csv"

// Cohort is a simulated cohort's every setting, recorded in situcraft-cohort.json.
type Cohort struct {
	Seed uint64 `json:"seed"`
	// PanelSeed is the seed of the panel every section shares (the seed if 0): the presets share
	// the standard cohort's, on which its effect gene is among its type's most expressed.
	PanelSeed uint64   `json:"panel_seed,omitempty"`
	Groups    []string `json:"groups"`
	Subjects  int      `json:"subjects"`
	Paired    bool     `json:"paired"`
	Slides    int      `json:"slides"`
	SlideSD   float64  `json:"slide_sd"`
	ShareSD   float64  `json:"share_sd"`
	LevelSD   float64  `json:"level_sd"`
	// SubjectShare is, in a paired design, the share of each draw's variance a subject's samples
	// have in common.
	SubjectShare float64  `json:"subject_share"`
	Effects      []Effect `json:"effects"`
	// Outliers are samples made far out on purpose: each one's detection multiplied, as a failed
	// run's would be (for checking that cohort QC finds them).
	Outliers []Outlier `json:"outliers,omitempty"`
	// Section is every sample's parameters but those its sample sets (its seed, odds, levels,
	// names and densities).
	Section Params `json:"section"`
}

// Effect is a known difference between a group and the reference.
type Effect struct {
	Kind   string  `json:"kind"`
	Group  string  `json:"group"`
	Type   string  `json:"type"`
	Within string  `json:"within,omitempty"`
	Gene   string  `json:"gene,omitempty"`
	Fold   float64 `json:"fold"`
}

// Outlier is a sample (S01 is 1) whose detection efficiency is multiplied: its cells' expression
// and the ambient background, as the slide's efficiency multiplies them.
type Outlier struct {
	Sample    int     `json:"sample"`
	Detection float64 `json:"detection"`
}

// Effect kinds.
const (
	EffectShare = "share"
	EffectLevel = "level"
)

// CohortPresets are named cohorts: standard (two groups of four subjects, with effects), null (the
// same without them), paired (six subjects before and after, with the effects after) and outlier
// (the standard cohort with its sixth sample detecting 40% of the transcripts it would).
var CohortPresets = map[string]Cohort{"standard": standardCohort(), "null": nullCohort(), "paired": pairedCohort(), "outlier": outlierCohort()}

// cohortSection is a cohort's section: the standard tissue on 250 µm a side, twelve domains (three
// led by each type), without post-Xenium images.
func cohortSection() Params {
	p := standard()
	p.WidthUm, p.HeightUm, p.Domains = 250, 250, 12
	return p
}

// cohortRetries is how many other places a cohort's sample's candidate cell refused for crowding
// tries in its domain: enough that the types placed are the types drawn.
const cohortRetries = 200

// CohortGene is the gene the standard cohort's level effect is of: not a marker, among the
// epithelial-like type's most expressed.
const CohortGene = "SIM033"

func standardCohort() Cohort {
	return Cohort{
		Seed: 20261019, Groups: []string{"control", "treated"}, Subjects: 4, Slides: 2, SlideSD: 0.1,
		ShareSD: 0.25, LevelSD: 0.2, SubjectShare: 0.6,
		Effects: []Effect{
			{Kind: EffectShare, Group: "treated", Type: "endothelial-like", Fold: 2},
			{Kind: EffectShare, Group: "treated", Type: "immune-like", Within: "epithelial-like", Fold: 4},
			{Kind: EffectLevel, Group: "treated", Type: "epithelial-like", Gene: CohortGene, Fold: 2.5},
		},
		Section: cohortSection(),
	}
}

func outlierCohort() Cohort {
	c := standardCohort()
	c.Outliers = []Outlier{{Sample: 6, Detection: 0.4}}
	return c
}

func nullCohort() Cohort {
	c := standardCohort()
	c.Seed, c.PanelSeed, c.Effects = 20261020, 20261019, []Effect{}
	return c
}

// pairedCohort is six subjects each sampled before and after, the standard cohort's effects after
// but the endothelial-like type's odds multiplied by 4: a shift six pairs find nearly always
// (wave 7, slice 9's worked examples).
func pairedCohort() Cohort {
	c := standardCohort()
	c.Seed, c.PanelSeed, c.Paired, c.Groups, c.Subjects = 20261021, 20261019, true, []string{"before", "after"}, 6
	for k := range c.Effects {
		c.Effects[k].Group = "after"
		if c.Effects[k].Kind == EffectShare && c.Effects[k].Within == "" {
			c.Effects[k].Fold = 4
		}
	}
	return c
}

// panelSeed is the seed of the cohort's panel.
func (c *Cohort) panelSeed() uint64 {
	if c.PanelSeed != 0 {
		return c.PanelSeed
	}
	return c.Seed
}

var groupName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,39}$`)

// Check refuses a cohort that cannot be made, saying why.
func (c *Cohort) Check() error { return c.check(false) }

// check is Check, sections of several fields (a mosaic) allowed when mosaic: CohortSamples's
// sections, whose true cells Cells makes a field at a time (RunCohort writes sections of one).
func (c *Cohort) check(mosaic bool) error {
	var bad []string
	need := func(ok bool, what string) {
		if !ok {
			bad = append(bad, what)
		}
	}
	need(len(c.Groups) >= 2 && len(c.Groups) <= 10, "2 to 10 groups")
	seen := map[string]bool{}
	for _, g := range c.Groups {
		need(groupName.MatchString(g), fmt.Sprintf("a group's name of letters, digits, spaces, _ . and - (%q)", g))
		need(!seen[strings.ToLower(g)], fmt.Sprintf("groups of different names (%q twice)", g))
		seen[strings.ToLower(g)] = true
	}
	need(c.Subjects >= 1 && c.Subjects <= 200, "1 to 200 subjects")
	need(c.Slides >= 0 && c.Slides <= 100, "0 to 100 slides")
	need(c.SlideSD >= 0 && c.SlideSD <= 3 && c.ShareSD >= 0 && c.ShareSD <= 3 && c.LevelSD >= 0 && c.LevelSD <= 3, "spreads of 0 to 3")
	need(c.SubjectShare >= 0 && c.SubjectShare <= 1, "a subject share of 0 to 1")
	planted := map[int]bool{}
	for _, o := range c.Outliers {
		need(o.Sample >= 1 && o.Sample <= c.samples() && !planted[o.Sample], fmt.Sprintf("an outlier of one of the samples, once (sample %d)", o.Sample))
		need(o.Detection > 0 && o.Detection <= 10, fmt.Sprintf("an outlier's detection above 0, at most 10 (%g)", o.Detection))
		planted[o.Sample] = true
	}
	if n := c.samples(); n > 500 {
		bad = append(bad, fmt.Sprintf("at most 500 samples (%d asked)", n))
	}
	p := c.Section
	if err := p.Check(); err != nil {
		bad = append(bad, err.Error())
	}
	need((mosaic || p.TilesAcross <= 1 && p.TilesDown <= 1) && p.PostXenium == nil, "sections of one field without post-Xenium images")
	need(p.TypeOdds == nil && len(p.DomainOdds) == 0 && p.LevelFolds == nil && p.PanelSeed == 0 && p.MixSeed == 0 && p.Retries == 0 && p.RunName == "" && p.RegionName == "" && p.SlideID == "", "the section without a sample's own settings (its odds, levels, panel seed and names: the cohort sets them)")
	for _, e := range c.Effects {
		what := fmt.Sprintf("the effect %s of %s in %s", e.Kind, e.Type, e.Group)
		need(e.Kind == EffectShare || e.Kind == EffectLevel, what+": of kind share or level")
		g := indexOf(c.Groups, e.Group)
		need(g > 0, what+": in a group other than the first (the reference)")
		need(typeIndex(p, e.Type) >= 0, what+": of a cell type of the section")
		need(e.Fold > 0 && !math.IsInf(e.Fold, 0), what+": a fold above 0")
		if e.Kind == EffectShare {
			need(e.Gene == "", what+": without a gene")
			if e.Within != "" {
				need(typeIndex(p, e.Within) >= 0 && e.Within != e.Type, what+": within the domains of another cell type")
			}
		} else {
			need(e.Within == "", what+": in every domain")
			need(geneIndex(p, e.Gene) >= 0, what+": of a gene of the panel")
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("the cohort's parameters: %s", joinAnd(bad))
	}
	return nil
}

func indexOf(list []string, s string) int {
	for k, v := range list {
		if v == s {
			return k
		}
	}
	return -1
}

func typeIndex(p Params, name string) int {
	for k, t := range p.Types {
		if t.Name == name {
			return k
		}
	}
	return -1
}

func geneIndex(p Params, name string) int {
	for g := 0; g < p.Genes; g++ {
		if GeneName(g) == name {
			return g
		}
	}
	return -1
}

// samples is how many sections the cohort has: each subject's, in one group or (paired) in each.
func (c *Cohort) samples() int { return c.Subjects * len(c.Groups) }

// Sample is one of a cohort's sections: its name (its bundle's folder), subject, group and slide.
type Sample struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Group   string `json:"group"`
	Slide   string `json:"slide,omitempty"`
	// subject's and group's numbers, from 0.
	subject, group int
}

// design lays out the samples: unpaired, by group then subject (S01, S02, …, numbered across the
// groups); paired, by subject then group (S01-before, S01-after, …).
func (c *Cohort) design() []Sample {
	var out []Sample
	if c.Paired {
		for i := 0; i < c.Subjects; i++ {
			for g, group := range c.Groups {
				out = append(out, Sample{Name: fmt.Sprintf("S%02d-%s", i+1, strings.ReplaceAll(group, " ", "_")), Subject: fmt.Sprintf("S%02d", i+1), Group: group, subject: i, group: g})
			}
		}
	} else {
		for g, group := range c.Groups {
			for i := 0; i < c.Subjects; i++ {
				n := g*c.Subjects + i
				out = append(out, Sample{Name: fmt.Sprintf("S%02d", n+1), Subject: fmt.Sprintf("S%02d", n+1), Group: group, subject: n, group: g})
			}
		}
	}
	if c.Slides > 0 {
		within := map[int]int{}
		for k := range out {
			s := &out[k]
			slide := 0
			if c.Paired {
				slide = s.subject % c.Slides
			} else {
				slide = within[s.group] % c.Slides
				within[s.group]++
			}
			s.Slide = fmt.Sprintf("simulated slide %d", slide+1)
		}
	}
	return out
}

// CohortSamples are a cohort's samples and each one's parameters, as RunCohort makes them: for a
// program that simulates many cohorts' sections without writing them (each section's true cells by
// Cells), such as a study of the cohort tests' error rates. Its sections may be mosaics of fields.
func CohortSamples(c Cohort) ([]Sample, []Params, error) {
	if err := c.check(true); err != nil {
		return nil, nil, err
	}
	samples := c.design()
	params := make([]Params, len(samples))
	for k, s := range samples {
		params[k] = c.sampleParams(k, s)
	}
	return samples, params, nil
}

// The cohort's streams: a subject's draws, a sample's own, a slide's efficiency, and a sample's
// seed.
const (
	streamSubject      = 1 << 40
	streamSampleDraws  = 1 << 41
	streamSlide        = 1 << 42
	streamSampleSeed   = 1 << 43
	sampleSeedFallback = 0x9e3779b97f4a7c15
)

// draws is a sample's variation: its types' log weights and its types' genes' log levels, each a
// standard normal (scaled later).
type draws struct {
	share []float64
	level [][]float64
}

func drawsOf(s *random.Stream, types, genes int) draws {
	d := draws{share: make([]float64, types), level: make([][]float64, types)}
	for k := range d.share {
		d.share[k] = s.Normal()
	}
	for k := range d.level {
		d.level[k] = make([]float64, genes)
		for g := range d.level[k] {
			d.level[k][g] = s.Normal()
		}
	}
	return d
}

// sampleParams are a sample's parameters: the section's with its seed, the cohort's panel seed,
// its odds and levels (its variation and its group's effects), names and slide's efficiency.
func (c *Cohort) sampleParams(k int, s Sample) Params {
	p := c.Section
	types, genes := len(p.Types), p.Genes
	seed := random.New(c.Seed, streamSampleSeed+uint64(k)).Uint64()
	if seed == 0 {
		seed = sampleSeedFallback
	}
	p.Seed, p.PanelSeed, p.MixSeed = seed, c.panelSeed(), c.Seed
	p.Retries = cohortRetries
	own := drawsOf(random.New(c.Seed, streamSampleDraws+uint64(k)), types, genes)
	var subject draws
	a, b := 1.0, 0.0
	if c.Paired {
		subject = drawsOf(random.New(c.Seed, streamSubject+uint64(s.subject)), types, genes)
		a, b = math.Sqrt(1-c.SubjectShare), math.Sqrt(c.SubjectShare)
	}
	mix := func(o, sub float64) float64 {
		if c.Paired {
			return mul(a, o) + mul(b, sub)
		}
		return o
	}
	p.TypeOdds = make([]float64, types)
	p.LevelFolds = make([][]float64, types)
	for t := 0; t < types; t++ {
		v := mul(c.ShareSD, mix(own.share[t], drawOf(subject.share, t)))
		p.TypeOdds[t] = dmath.Exp(v)
		p.LevelFolds[t] = make([]float64, genes)
		for g := 0; g < genes; g++ {
			var sub float64
			if c.Paired {
				sub = subject.level[t][g]
			}
			p.LevelFolds[t][g] = dmath.Exp(mul(c.LevelSD, mix(own.level[t][g], sub)))
		}
	}
	for _, e := range c.Effects {
		if e.Group != s.Group {
			continue
		}
		t := typeIndex(p, e.Type)
		switch {
		case e.Kind == EffectShare && e.Within == "":
			p.TypeOdds[t] = mul(p.TypeOdds[t], e.Fold)
		case e.Kind == EffectShare:
			p.DomainOdds = append(p.DomainOdds, DomainOdds{Leader: typeIndex(p, e.Within), Type: t, Odds: e.Fold})
		case e.Kind == EffectLevel:
			g := geneIndex(p, e.Gene)
			p.LevelFolds[t][g] = mul(p.LevelFolds[t][g], e.Fold)
		}
	}
	// The slide's efficiency (and a planted outlier's) multiplies its cells' densities and the
	// ambient background.
	if eff := mul(c.slideEfficiency(s.Slide), c.planted(k)); eff != 1 {
		p.Types = append([]CellType(nil), p.Types...)
		for t := range p.Types {
			p.Types[t].Density = mul(p.Types[t].Density, eff)
		}
		p.Ambient = mul(p.Ambient, eff)
	}
	// A run a slide: "simulated run 1" for "simulated slide 1".
	p.RunName = "SituCraft simulated cohort"
	if s.Slide != "" {
		p.RunName = strings.Replace(s.Slide, "slide", "run", 1)
		p.SlideID = s.Slide
	}
	p.RegionName = s.Name
	return p
}

func drawOf(v []float64, k int) float64 {
	if v == nil {
		return 0
	}
	return v[k]
}

// planted is the kth sample's planted detection (1 when it is not an outlier).
func (c *Cohort) planted(k int) float64 {
	for _, o := range c.Outliers {
		if o.Sample == k+1 {
			return o.Detection
		}
	}
	return 1
}

// slideEfficiency is a slide's detection efficiency: 1 without slides.
func (c *Cohort) slideEfficiency(slide string) float64 {
	var n int
	if _, err := fmt.Sscanf(slide, "simulated slide %d", &n); err != nil || n < 1 {
		return 1
	}
	return dmath.Exp(mul(c.SlideSD, random.New(c.Seed, streamSlide+uint64(n)).Normal()))
}

// CohortSummary is what a cohort's simulation made.
type CohortSummary struct {
	Folder  string      `json:"folder"`
	Samples []Sample    `json:"samples"`
	Truth   CohortTruth `json:"truth"`
}

// CohortTruth is truth/cohort.json: the design, the effects with what the true cells show of them,
// and each sample's true values.
type CohortTruth struct {
	Format  string       `json:"format"`
	Version int          `json:"version"`
	Design  CohortDesign `json:"design"`
	Effects []TrueEffect `json:"effects"`
	Samples []TrueSample `json:"samples"`
}

// CohortDesign is the design: the groups (the first the reference), the subjects, pairing, the
// slides, and the variation.
type CohortDesign struct {
	Groups       []string `json:"groups"`
	Subjects     int      `json:"subjects"`
	Paired       bool     `json:"paired"`
	Slides       []string `json:"slides"`
	SlideSD      float64  `json:"slide_sd"`
	ShareSD      float64  `json:"share_sd"`
	LevelSD      float64  `json:"level_sd"`
	SubjectShare float64  `json:"subject_share,omitempty"`
}

// TrueSample is a sample's truth: who and where it is, what was asked of it, and what its true
// cells and their transcripts are.
type TrueSample struct {
	Sample
	Seed      uint64  `json:"seed"`
	Detection float64 `json:"detection"`
	// Planted: a planted outlier's detection, within Detection (absent for the others).
	Planted  float64            `json:"planted,omitempty"`
	TypeOdds map[string]float64 `json:"type_odds"`
	Cells    int                `json:"cells"`
	ByType   map[string]int     `json:"cells_by_type"`
	// Domains: each domain's leading type and its cells by type (the domains in the same order in
	// every sample, each with the same mix).
	Domains []TrueDomain `json:"domains"`
	// Levels: for each gene a level effect is of, in its type: the fold asked of this sample (its
	// variation and its group's effect), the type's cells, their transcripts of the gene and of
	// every gene.
	Levels []TrueLevel `json:"levels,omitempty"`
}

// TrueDomain is a domain of a sample: the type that leads it, and its true cells by type.
type TrueDomain struct {
	Leader string         `json:"leader"`
	ByType map[string]int `json:"cells_by_type"`
}

// TrueLevel is a gene's truth in a type of a sample.
type TrueLevel struct {
	Type        string  `json:"type"`
	Gene        string  `json:"gene"`
	Fold        float64 `json:"fold"`
	Cells       int     `json:"cells"`
	Transcripts int64   `json:"transcripts"`
	TypeTotal   int64   `json:"type_transcripts"`
}

// TrueEffect is an effect as asked and as the true cells show it: the mean over each group's
// samples of the type's log odds against the cells of the types no share effect changes (in each
// domain the effect holds in, averaged over them), or of the gene's log odds among the type's
// transcripts, both of which the fold multiplies; the difference between the group and the
// reference, and its standard error from the samples' spread (paired designs: of the subjects'
// differences).
type TrueEffect struct {
	Effect
	Reference float64 `json:"reference_mean"`
	InGroup   float64 `json:"group_mean"`
	LogFold   float64 `json:"log_fold"`
	Measured  float64 `json:"measured_log_fold"`
	StdError  float64 `json:"std_error"`
}

// RunCohort makes the cohort c describes and writes it into folder (which must not exist yet: it
// is written beside it and moved into place when complete). Software names SituCraft's version;
// progress hears how far it is (0 to 1).
func RunCohort(ctx context.Context, c Cohort, folder, software string, progress func(float64, string)) (*CohortSummary, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	if err := c.Check(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(folder); err == nil {
		return nil, fmt.Errorf("%s already exists: the simulation writes a new folder", folder)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	parent := filepath.Dir(folder)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(folder)+".writing-")
	if err != nil {
		return nil, err
	}
	done := false
	defer func() {
		if !done {
			os.RemoveAll(staging)
		}
	}()
	samples := c.design()
	sums := make([]*Summary, len(samples))
	params := make([]Params, len(samples))
	errs := make([]error, len(samples))
	// Samples in parallel, each its own folder and streams: the same cohort on any computer.
	var mu sync.Mutex
	finished := 0
	progress(0, fmt.Sprintf("Simulating %d sections", len(samples)))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < max(1, min(len(samples), runtime.GOMAXPROCS(0)/2)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range jobs {
				params[k] = c.sampleParams(k, samples[k])
				sums[k], errs[k] = Run(ctx, params[k], filepath.Join(staging, samples[k].Name), software, nil)
				mu.Lock()
				finished++
				progress(0.95*float64(finished)/float64(len(samples)), fmt.Sprintf("Simulated %d of %d sections", finished, len(samples)))
				mu.Unlock()
			}
		}()
	}
	for k := range samples {
		if ctx.Err() != nil {
			break
		}
		jobs <- k
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, errStopped
	}
	for k, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("%s: %w", samples[k].Name, err)
		}
	}
	truth := c.truth(samples, params, sums)
	progress(0.97, "Writing the truth")
	if err := os.MkdirAll(filepath.Join(staging, "truth"), 0o755); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(staging, CohortTruthFile), truth); err != nil {
		return nil, err
	}
	if err := writeSamplesTable(filepath.Join(staging, CohortSamplesFile), samples); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(staging, CohortFile), c.description(software, samples)); err != nil {
		return nil, err
	}
	if err := os.Chmod(staging, 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(staging, folder); err != nil {
		return nil, err
	}
	done = true
	progress(1, "Done")
	return &CohortSummary{Folder: folder, Samples: samples, Truth: truth}, nil
}

// writeSamplesTable writes samples.csv: each sample's subject and condition (its group), as
// SituCraft reads a table of samples.
func writeSamplesTable(path string, samples []Sample) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.Write([]string{"sample", "subject", "condition"})
	for _, s := range samples {
		w.Write([]string{s.Name, s.Subject, s.Group})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// description is situcraft-cohort.json: that the cohort is simulated, by what, with what settings,
// its samples and where its truth is.
func (c *Cohort) description(software string, samples []Sample) any {
	return map[string]any{
		"format":    "situcraft-cohort",
		"version":   1,
		"simulated": true,
		"software":  "SituCraft " + software,
		"about": "Every section of this cohort is simulated: a folder per sample, each a simulated Xenium-shaped bundle with its own truth. " +
			"The samples differ from one another by stated amounts and between groups by known effects; samples.csv gives each " +
			"sample's subject and condition, and truth/cohort.json the design, the effects and each sample's true values.",
		"truth":      "truth/cohort.json",
		"samples":    samples,
		"parameters": c,
	}
}

// truth gathers the cohort's truth from its samples' true values.
func (c *Cohort) truth(samples []Sample, params []Params, sums []*Summary) CohortTruth {
	p := c.Section
	t := CohortTruth{Format: "situcraft-cohort-truth", Version: 1}
	slides := []string{}
	for _, s := range samples {
		if s.Slide != "" && indexOf(slides, s.Slide) < 0 {
			slides = append(slides, s.Slide)
		}
	}
	t.Design = CohortDesign{Groups: c.Groups, Subjects: c.Subjects, Paired: c.Paired, Slides: slides, SlideSD: c.SlideSD, ShareSD: c.ShareSD, LevelSD: c.LevelSD}
	if c.Paired {
		t.Design.SubjectShare = c.SubjectShare
	}
	for k, s := range samples {
		sum, sp := sums[k], params[k]
		ts := TrueSample{Sample: s, Seed: sp.Seed, Detection: mul(c.slideEfficiency(s.Slide), c.planted(k)), TypeOdds: map[string]float64{}, Cells: sum.Cells, ByType: map[string]int{}}
		if f := c.planted(k); f != 1 {
			ts.Planted = f
		}
		for i, ty := range p.Types {
			ts.TypeOdds[ty.Name] = sp.TypeOdds[i]
			ts.ByType[ty.Name] = sum.CellsByType[ty.Name]
		}
		for d, row := range sum.CellsByDomainType {
			dom := TrueDomain{Leader: p.Types[d%len(p.Types)].Name, ByType: map[string]int{}}
			for i, n := range row {
				dom.ByType[p.Types[i].Name] = n
			}
			ts.Domains = append(ts.Domains, dom)
		}
		for _, e := range c.Effects {
			if e.Kind != EffectLevel {
				continue
			}
			i, g := typeIndex(p, e.Type), geneIndex(p, e.Gene)
			var total int64
			for _, v := range sum.GeneCountsByType[i] {
				total += v
			}
			ts.Levels = append(ts.Levels, TrueLevel{Type: e.Type, Gene: e.Gene, Fold: sp.LevelFolds[i][g], Cells: sum.CellsByType[e.Type], Transcripts: sum.GeneCountsByType[i][g], TypeTotal: total})
		}
		t.Samples = append(t.Samples, ts)
	}
	for _, e := range c.Effects {
		t.Effects = append(t.Effects, c.measure(e, t.Samples))
	}
	if t.Effects == nil {
		t.Effects = []TrueEffect{}
	}
	return t
}

// measure is an effect as the true cells show it: each sample's value (a type's log odds against
// the cells of the types no share effect changes, in each domain the effect holds in, averaged over
// them; a gene's log odds among its type's transcripts), the groups' means, and the difference's
// standard error.
func (c *Cohort) measure(e Effect, samples []TrueSample) TrueEffect {
	value := func(s TrueSample) float64 {
		if e.Kind == EffectLevel {
			for _, l := range s.Levels {
				if l.Type == e.Type && l.Gene == e.Gene {
					return dmath.Log((float64(l.Transcripts) + 0.5) / (float64(l.TypeTotal-l.Transcripts) + 0.5))
				}
			}
			return math.NaN()
		}
		var sum float64
		var n int
		for _, dom := range s.Domains {
			if e.Within != "" && dom.Leader != e.Within {
				continue
			}
			// Against the cells of the types no share effect changes in this domain: a fold
			// multiplies a type's odds against each other type, and an effect on another type
			// changes that type's share of the rest.
			var rest int
			for ty, v := range dom.ByType {
				if ty != e.Type && !c.changed(ty, dom.Leader) {
					rest += v
				}
			}
			sum += dmath.Log((float64(dom.ByType[e.Type]) + 0.5) / (float64(rest) + 0.5))
			n++
		}
		return sum / float64(n)
	}
	out := TrueEffect{Effect: e, LogFold: dmath.Log(e.Fold)}
	ref := c.Groups[0]
	if c.Paired {
		// The subjects' differences.
		bySubject := map[string]map[string]float64{}
		var order []string
		for _, s := range samples {
			if bySubject[s.Subject] == nil {
				bySubject[s.Subject] = map[string]float64{}
				order = append(order, s.Subject)
			}
			bySubject[s.Subject][s.Group] = value(s)
		}
		var diffs, refs, ins []float64
		for _, sub := range order {
			v := bySubject[sub]
			diffs = append(diffs, v[e.Group]-v[ref])
			refs = append(refs, v[ref])
			ins = append(ins, v[e.Group])
		}
		m, sd := meanSD(diffs)
		out.Reference, _ = meanSD(refs)
		out.InGroup, _ = meanSD(ins)
		out.Measured, out.StdError = m, sd/math.Sqrt(float64(len(diffs)))
		return out
	}
	var refs, ins []float64
	for _, s := range samples {
		switch s.Group {
		case ref:
			refs = append(refs, value(s))
		case e.Group:
			ins = append(ins, value(s))
		}
	}
	mr, sr := meanSD(refs)
	mi, si := meanSD(ins)
	out.Reference, out.InGroup = mr, mi
	out.Measured = mi - mr
	out.StdError = math.Sqrt(mul(sr, sr)/float64(len(refs)) + mul(si, si)/float64(len(ins)))
	return out
}

// changed says whether a share effect (of any group) changes a type's weight in the domains a type
// leads.
func (c *Cohort) changed(typ, leader string) bool {
	for _, e := range c.Effects {
		if e.Kind == EffectShare && e.Type == typ && (e.Within == "" || e.Within == leader) {
			return true
		}
	}
	return false
}

// meanSD is the mean and the sample standard deviation (0 for fewer than two values).
func meanSD(v []float64) (float64, float64) {
	if len(v) == 0 {
		return math.NaN(), 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	m := sum / float64(len(v))
	if len(v) < 2 {
		return m, 0
	}
	var ss float64
	for _, x := range v {
		ss += mul(x-m, x-m)
	}
	return m, math.Sqrt(ss / float64(len(v)-1))
}

// trueValues simulates a sample's tissue and molecules without writing them, answering its true
// values as Run's summary has them: for checking a cohort's statistics on many sections quickly.
func trueValues(p Params) (*Summary, error) {
	t, pn, mols, in, err := simulateTissue(p, func() bool { return false }, func(float64, string) error { return nil })
	if err != nil {
		return nil, err
	}
	wr := &writer{t: t, in: in, pn: pn, mols: mols}
	wr.count()
	byType := map[string]int{}
	for k := range t.cells {
		byType[p.Types[t.cells[k].typ].Name]++
	}
	return &Summary{Cells: len(t.cells), CellsByType: byType, CellsByDomainType: wr.w.cellsByDomainType, GeneCountsByType: wr.w.geneCountsByType}, nil
}

// trueCohort is the cohort's truth from its samples simulated without writing them (trueValues), in
// parallel.
func (c *Cohort) trueCohort() (CohortTruth, error) {
	if err := c.Check(); err != nil {
		return CohortTruth{}, err
	}
	samples := c.design()
	params := make([]Params, len(samples))
	sums := make([]*Summary, len(samples))
	errs := make([]error, len(samples))
	var wg sync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < max(1, runtime.GOMAXPROCS(0)/2); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range jobs {
				params[k] = c.sampleParams(k, samples[k])
				sums[k], errs[k] = trueValues(params[k])
			}
		}()
	}
	for k := range samples {
		jobs <- k
	}
	close(jobs)
	wg.Wait()
	for k, err := range errs {
		if err != nil {
			return CohortTruth{}, fmt.Errorf("%s: %w", samples[k].Name, err)
		}
	}
	return c.truth(samples, params, sums), nil
}
