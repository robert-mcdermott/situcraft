// SituCraft's transcript-informed segmentation against an independent reference (wave 3, slice 6;
// research.md §3.2): on both 2-FOV bundles and the standard simulated tissue, SituCraft's
// refinement (Balanced, two seeds) and Proseg 3.2.0's (its Xenium preset, two runs each: it has no
// seed) are compared with each other, with the instrument's segmentation and, on the simulated
// tissue, with the truth, using the comparison workspace's own measures. Proseg runs in one voxel
// layer, as SituCraft's default (proseg-1, proseg-2: the like-for-like reference the tolerances
// apply to), and with its own defaults, four layers (proseg-defaults-1, -2: reported).
//
// The maintainer makes Proseg's segmentations (validation/reference/segmentation/run_proseg.sh,
// into validation/cache/proseg/) and reduces everything to numbers with
//
//	SITUCRAFT_WRITE_REFERENCE=1 go test ./validation/ -run ReferenceSegmentation -v -timeout 2h
//
// which writes validation/reference/segmentation/<data set>.json. With SITUCRAFT_REQUIRE_DATA set
// (the weekly workflow) the test refines each data set again, as `situcraft refine` does, and
// checks that the segmentation is still the one the numbers were made from, byte for byte, and
// that the numbers keep within their tolerances or are explained.
package validation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"situcraft/internal/compare"
	"situcraft/internal/label"
	"situcraft/internal/segment/expand"
	"situcraft/internal/segment/imports"
	"situcraft/internal/simulate"
	"situcraft/internal/xenium"
)

// The data sets compared.
var referenceSets = []string{"xenium-lung-2fov", "xenium-breast-2fov", "simulated-standard"}

// segNumbers are one segmentation's measures (compare.SegMetrics), and its cost when measured.
type segNumbers struct {
	Cells             int           `json:"cells"`
	CellsWithNucleus  int           `json:"cellsWithNucleus"`
	AssignedFraction  float64       `json:"assignedFraction"`
	MedianTranscripts compare.Float `json:"medianTranscriptsPerCell"`
	MedianAreaUm2     compare.Float `json:"medianAreaUm2"`
	Coexpression      compare.Float `json:"spuriousCoexpression"`
	MECR              compare.Float `json:"mecr"` // null where not measured
	Seconds           float64       `json:"seconds,omitempty"`
	PeakBytes         int64         `json:"peakResidentBytes,omitempty"`
	// Digest is SituCraft's files' SHA-256 (its segmentation, without the recipe).
	Digest string `json:"digest,omitempty"`
}

// pairNumbers compare two segmentations.
type pairNumbers struct {
	Classes map[string]int `json:"classes"`
	// Matched are the cells paired one to one (expanded and contracted included); IoU their IoUs'
	// quantiles; Counts the correlation of their gene transcripts (quality 20 or more): Pearson's
	// of log(1 + n), and Spearman's.
	Matched int                `json:"matchedCells"`
	IoU     map[string]float64 `json:"iou"`
	Counts  struct {
		PearsonLog float64 `json:"pearsonLog"`
		Spearman   float64 `json:"spearman"`
	} `json:"countCorrelation"`
	// ChangedQ20 is the share of gene transcripts (quality 20 or more) whose cell changed;
	// ChangedQ20OfA the share of those A assigns; with the truth as A, 1 − it is the share in
	// their true cell.
	ChangedQ20     float64 `json:"changedQ20"`
	ChangedQ20OfA  float64 `json:"changedQ20OfAssignedA"`
	PixelAgreement float64 `json:"pixelAgreement"`
	// LostQ20 is the share of gene transcripts (quality 20 or more) A puts in a cell and B in none;
	// GainedQ20 the share A puts in none and B in a cell. With the truth as A, LostQ20 is B's
	// background that is cells' transcripts.
	LostQ20   float64 `json:"lostQ20"`
	GainedQ20 float64 `json:"gainedQ20"`
}

// prosegRun is how a configuration of Proseg was run.
type prosegRun struct {
	Version string `json:"version"`
	Command string `json:"command"`
	Machine string `json:"machine"`
}

// prosegConfigs are Proseg's configurations: their folders under validation/cache/proseg/<set>/,
// and their segmentations' names here.
var prosegConfigs = []struct{ folder, name string }{{"one-layer", "proseg"}, {"defaults", "proseg-defaults"}}

// check is a tolerance and how a data set met it.
type check struct {
	Name      string  `json:"name"`
	Tolerance string  `json:"tolerance"`
	Value     float64 `json:"value"`
	Limit     float64 `json:"limit"`
	Pass      bool    `json:"pass"`
	// Explained says why a difference beyond the tolerance is what it should be.
	Explained string `json:"explained,omitempty"`
}

// referenceNumbers are a data set's (validation/reference/segmentation/<set>.json).
type referenceNumbers struct {
	Set       string `json:"dataSet"`
	SituCraft struct {
		Version string `json:"version"`
		Command string `json:"command"`
		Cores   int    `json:"cores"`
	} `json:"situcraft"`
	// Proseg is each configuration's run: its version, command and the computer it ran on.
	Proseg        map[string]prosegRun   `json:"proseg"`
	Segmentations map[string]segNumbers  `json:"segmentations"`
	Pairs         map[string]pairNumbers `json:"pairs"`
	Checks        []check                `json:"checks"`
}

// The tolerances (research.md §3.2, the reference comparison), set before the comparison was
// made. Each says why.
const (
	// SituCraft keeps every cell of the instrument's; Proseg makes one for each nucleus.
	tolCells = 0.05
	// Both assign the transcripts they model by the same model.
	tolAssigned = 0.03
	// Spurious co-expression is what the method is for: SituCraft's may be no more than a tenth
	// above Proseg's.
	tolCoexpression = 1.10
	// The two methods should differ little beyond how each differs from itself between runs.
	tolCountCorrelation = 0.05
	tolIoU              = 0.10
	// On the simulated truth, no worse than the reference.
	tolTrueCell = 0.01
	tolTrueIoU  = 0.02
	// Budgets on a laptop (the Breast bundle).
	budgetSeconds = 120
	budgetBytes   = 4 << 30
)

// explained are differences beyond a tolerance, explained (research.md §3.2, against an
// independent reference), by data set and check.
var explained = map[string]string{
	"xenium-lung-2fov: assigned fraction":      "Proseg leaves more transcripts in no cell, but on the simulated tissue only 18% of those it leaves there are background: 1.42% of all transcripts are cells' it leaves out, against SituCraft's 0.13% (61% of the 0.34% it leaves out are background). A larger background share is not a more accurate one. On Lung (SituCraft 99.63%, Proseg 95.68%, the instrument 97.97%) the negative controls put the false decodes at 0.04% of the transcripts; the rest of any background is ambient RNA, which inside cells cannot be told from their own (research.md §3.2, the background).",
	"xenium-lung-2fov: spurious co-expression": "Spurious co-expression does not rank methods by accuracy: on the simulated tissue Proseg's (1.87) is below the truth's own (1.89) while it puts 75.2% of transcripts in their true cell, against SituCraft's 84.7% (2.02). On Lung SituCraft's is 10% below the instrument's; part of the rest is ambient RNA counted in cells (research.md §3.2, the background).",
	"xenium-lung-2fov: median IoU":             "Lung's cells are the smallest (median 37 µm²), and Proseg's outlines are more compact there (1.16 times the least perimeter, SituCraft's 1.32): with Conservative's bound (1.3) SituCraft's agreement with Proseg rises from 0.557 to 0.576. Starting SituCraft from the nuclei, as Proseg starts, leaves it at 0.557.",
}

func writeReference() bool { return os.Getenv("SITUCRAFT_WRITE_REFERENCE") != "" }

func TestReferenceSegmentation(t *testing.T) {
	// Minutes of refinement: the weekly real-data workflow runs it (SITUCRAFT_REQUIRE_DATA), and
	// the maintainer writing the numbers.
	if testing.Short() || !writeReference() && os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 (or SITUCRAFT_WRITE_REFERENCE=1) to refine the reference data sets")
	}
	bin := buildSituCraft(t)
	for _, set := range referenceSets {
		t.Run(set, func(t *testing.T) {
			dir := filepath.Join(dataRoot(), set)
			if set == "simulated-standard" {
				dir = simulatedStandard(t)
			} else if bundle(t, set) == "" {
				return
			}
			path := filepath.Join("reference", "segmentation", set+".json")
			if writeReference() {
				ref := reduce(t, bin, set, dir)
				var raw bytes.Buffer
				enc := json.NewEncoder(&raw)
				enc.SetEscapeHTML(false)
				enc.SetIndent("", " ")
				if err := enc.Encode(ref); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, raw.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				report(t, ref)
				return
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (made by SITUCRAFT_WRITE_REFERENCE=1 with Proseg's outputs)", err)
			}
			var ref referenceNumbers
			if err := json.Unmarshal(raw, &ref); err != nil {
				t.Fatal(err)
			}
			// The tolerances, on the numbers recorded, with the explanations here.
			ref.Checks = checks(set, &ref)
			// SituCraft's segmentation is still the one compared.
			out := filepath.Join(t.TempDir(), "situcraft-1")
			got := refineWith(t, bin, dir, out, 20250)
			if want := ref.Segmentations["situcraft-1"].Digest; got.Digest != want {
				t.Errorf("SituCraft's segmentation of %s is not the one its reference numbers were made from (digest %s, recorded %s): make them again (SITUCRAFT_WRITE_REFERENCE=1)", set, got.Digest, want)
			}
			report(t, &ref)
		})
	}
}

// report logs a data set's numbers and fails on a check not met nor explained.
func report(t *testing.T, ref *referenceNumbers) {
	t.Helper()
	names := make([]string, 0, len(ref.Segmentations))
	for name := range ref.Segmentations {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := ref.Segmentations[name]
		t.Logf("%s: %d cells, %.2f%% assigned, %.0f transcripts and %.1f µm² per cell, co-expression %.3f, MECR %.4f; %.0f s, %d MB",
			name, s.Cells, 100*s.AssignedFraction, float64(s.MedianTranscripts), float64(s.MedianAreaUm2), float64(s.Coexpression), float64(s.MECR), s.Seconds, s.PeakBytes>>20)
	}
	pairs := make([]string, 0, len(ref.Pairs))
	for name := range ref.Pairs {
		pairs = append(pairs, name)
	}
	sort.Strings(pairs)
	for _, name := range pairs {
		p := ref.Pairs[name]
		t.Logf("%s: %d matched; IoU median %.3f (10%% %.3f, 90%% %.3f); counts r %.3f (Spearman %.3f); %.2f%% changed (%.2f%% of A's); pixels %.2f%%",
			name, p.Matched, p.IoU["p50"], p.IoU["p10"], p.IoU["p90"], p.Counts.PearsonLog, p.Counts.Spearman, 100*p.ChangedQ20, 100*p.ChangedQ20OfA, 100*p.PixelAgreement)
	}
	// On the truth, each one's background: how much of what it leaves in no cell truly is
	// background (precision), and how much of the background it leaves there (recall).
	if truth, ok := ref.Segmentations["truth"]; ok {
		background := 1 - truth.AssignedFraction
		for _, name := range names {
			p, ok := ref.Pairs["truth / "+name]
			if !ok {
				continue
			}
			none := 1 - ref.Segmentations[name].AssignedFraction
			right := none - p.LostQ20
			t.Logf("%s's background: %.2f%% of transcripts in no cell (the truth's %.2f%%), %.2f%% of them cells'; precision %.1f%%, recall %.1f%%",
				name, 100*none, 100*background, 100*p.LostQ20, 100*right/math.Max(none, 1e-12), 100*right/math.Max(background, 1e-12))
		}
	}
	for _, c := range ref.Checks {
		switch {
		case c.Pass:
			t.Logf("met: %s: %.4f (%s, %.4f)", c.Name, c.Value, c.Tolerance, c.Limit)
		case c.Explained != "":
			t.Logf("explained: %s: %.4f (%s, %.4f): %s", c.Name, c.Value, c.Tolerance, c.Limit, c.Explained)
		default:
			t.Errorf("not met: %s: %.4f (%s, %.4f)", c.Name, c.Value, c.Tolerance, c.Limit)
		}
	}
}

// buildSituCraft builds the program, to run `situcraft refine` as people do.
func buildSituCraft(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "situcraft")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = ".."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// simulatedStandard is the standard simulated tissue, made once in the data folder (the same, byte
// for byte, wherever it is made); made again when an earlier simulator's (its truth without each
// cell's domain) is found there.
func simulatedStandard(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(dataRoot(), "simulated-standard")
	if _, err := os.Stat(filepath.Join(dir, "experiment.xenium")); err == nil {
		if tab, err := label.ReadTable(filepath.Join(dir, "truth", "cells.parquet")); err == nil && tab.Column("domain") != nil {
			return dir
		}
		t.Logf("remaking %s: its truth has no domains", dir)
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := simulate.Run(context.Background(), simulate.Presets["standard"], dir, "validation", nil); err != nil {
		t.Fatal(err)
	}
	return dir
}

// refineWith runs `situcraft refine` (Balanced) with a seed, measuring its time and peak memory.
func refineWith(t *testing.T, bin, bundle, out string, seed uint64) segNumbers {
	t.Helper()
	cmd := exec.Command(bin, "refine", "--quiet", "--seed", strconv.FormatUint(seed, 10), bundle, out)
	start := time.Now()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("situcraft refine: %v\n%s", err, b)
	}
	s := segNumbers{Seconds: math.Round(time.Since(start).Seconds()*10) / 10, PeakBytes: peakResident(cmd.ProcessState)}
	s.Digest = digestOf(t, out)
	return s
}

// digestOf hashes a segmentation's files, in name order (the recipe left out: it names the
// software's version).
func digestOf(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for _, e := range entries {
		if e.IsDir() || e.Name() == "recipe.json" {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h, "%s\n", e.Name())
		io.Copy(h, f)
		f.Close()
	}
	return hex.EncodeToString(h.Sum(nil))
}

// prosegCost reads a Proseg run's time and peak memory from /usr/bin/time's report.
func prosegCost(dir string) (float64, int64) {
	raw, err := os.ReadFile(filepath.Join(dir, "proseg.err"))
	if err != nil {
		return 0, 0
	}
	text := string(raw)
	var seconds float64
	var peak int64
	if m := regexp.MustCompile(`([0-9.]+) real`).FindStringSubmatch(text); m != nil { // macOS
		seconds, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := regexp.MustCompile(`(\d+)\s+maximum resident set size`).FindStringSubmatch(text); m != nil {
		peak, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if m := regexp.MustCompile(`Elapsed \(wall clock\) time \(h:mm:ss or m:ss\): ([0-9:.]+)`).FindStringSubmatch(text); m != nil { // GNU time
		for _, part := range strings.Split(m[1], ":") {
			v, _ := strconv.ParseFloat(part, 64)
			seconds = seconds*60 + v
		}
	}
	if m := regexp.MustCompile(`Maximum resident set size \(kbytes\): (\d+)`).FindStringSubmatch(text); m != nil {
		kb, _ := strconv.ParseInt(m[1], 10, 64)
		peak = kb * 1024
	}
	return seconds, peak
}

// reduce makes a data set's numbers: every segmentation made or read, compared pair by pair.
func reduce(t *testing.T, bin, set, dir string) *referenceNumbers {
	t.Helper()
	ctx := context.Background()
	work := t.TempDir()
	ref := &referenceNumbers{Set: set, Segmentations: map[string]segNumbers{}, Pairs: map[string]pairNumbers{}, Proseg: map[string]prosegRun{}}
	ref.SituCraft.Command = "situcraft refine --seed <seed> <bundle> <folder> (Balanced)"
	ref.SituCraft.Cores = runtime.NumCPU()
	if v, err := exec.Command(bin, "--version").Output(); err == nil {
		ref.SituCraft.Version = strings.TrimSpace(strings.TrimPrefix(string(v), "situcraft "))
	}
	b, err := xenium.Open(dir, xenium.Options{CacheDir: work})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ds, err := imports.BundleDataset(b)
	if err != nil {
		t.Fatal(err)
	}
	workers := runtime.NumCPU()
	folders := map[string]string{}
	costs := map[string]segNumbers{}
	// SituCraft, two seeds.
	for i, seed := range []uint64{20250, 20251} {
		name := fmt.Sprintf("situcraft-%d", i+1)
		folders[name] = filepath.Join(work, name)
		costs[name] = refineWith(t, bin, dir, folders[name], seed)
	}
	// Proseg, each configuration twice.
	for _, cfg := range prosegConfigs {
		for i := 1; i <= 2; i++ {
			name := fmt.Sprintf("%s-%d", cfg.name, i)
			run := filepath.Join(dataRoot(), "proseg", set, fmt.Sprintf("%s-%d", cfg.folder, i))
			var meta struct{ Proseg, Command, Machine string }
			raw, err := os.ReadFile(filepath.Join(run, "run.json"))
			if err != nil {
				t.Fatalf("%v (validation/reference/segmentation/run_proseg.sh %s)", err, set)
			}
			json.Unmarshal(raw, &meta)
			ref.Proseg[cfg.folder] = prosegRun{meta.Proseg, meta.Command, meta.Machine}
			folders[name] = importProseg(ctx, t, ds, run, meta.Proseg, filepath.Join(work, name), workers)
			s := segNumbers{}
			s.Seconds, s.PeakBytes = prosegCost(run)
			costs[name] = s
		}
	}
	// The instrument's, and the truth (simulated).
	folders["instrument"] = filepath.Join(work, "instrument")
	os.MkdirAll(folders["instrument"], 0o755)
	if _, err := imports.Instrument(ctx, b, folders["instrument"], workers); err != nil {
		t.Fatal(err)
	}
	truth := filepath.Join(dir, "truth")
	if _, err := os.Stat(truth); err == nil {
		folders["truth"] = filepath.Join(work, "truth")
		os.MkdirAll(folders["truth"], 0o755)
		if _, _, _, err := imports.Run(ctx, ds, imports.Options{Format: imports.Xenium, Paths: []string{truth}}, folders["truth"], workers, nil); err != nil {
			t.Fatal(err)
		}
	}
	// The co-expression reference, as the comparison workspace makes it: the nuclei, and the
	// nuclei grown by 10 µm.
	grown := func(um float64) *compare.Seg {
		d := filepath.Join(work, fmt.Sprintf("grown-%g", um))
		os.MkdirAll(d, 0o755)
		if _, err := expand.FromBundle(ctx, b, d, expand.Params{DistanceUm: um}, workers, nil); err != nil {
			t.Fatal(err)
		}
		s, err := compare.Load(d)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	p := compare.DefaultParams()
	coexpression := compare.NewReference(grown(0).Counts, grown(10).Counts, p)
	nuclei, err := expand.NucleusRaster(b)
	if err != nil {
		t.Fatal(err)
	}
	transcripts, err := compare.BundleTranscripts(b)
	if err != nil {
		t.Fatal(err)
	}
	segs := map[string]*compare.Seg{}
	for name, d := range folders {
		if segs[name], err = compare.Load(d); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	comparePair := func(a, b string) (*compare.Result, pairNumbers) {
		res, err := compare.Compare(ctx, compare.Inputs{A: segs[a], B: segs[b], Nuclei: nuclei, Transcripts: transcripts, Reference: coexpression, Params: p})
		if err != nil {
			t.Fatalf("%s / %s: %v", a, b, err)
		}
		return res, pairOf(res)
	}
	// Each segmentation's measures, from its comparison with the instrument's.
	for name := range folders {
		res, pair := comparePair("instrument", name)
		m := res.B
		s := costs[name]
		s.Cells, s.CellsWithNucleus, s.AssignedFraction = m.Cells, m.CellsWithNucleus, m.AssignedFraction
		s.MedianTranscripts, s.MedianAreaUm2, s.Coexpression, s.MECR = m.MedianTranscripts, m.MedianArea, m.Coexpression, m.MECR
		ref.Segmentations[name] = s
		if name != "instrument" {
			ref.Pairs["instrument / "+name] = pair
		}
	}
	for _, pr := range [][2]string{{"situcraft-1", "situcraft-2"}, {"proseg-1", "proseg-2"}, {"proseg-defaults-1", "proseg-defaults-2"},
		{"situcraft-1", "proseg-1"}, {"situcraft-2", "proseg-2"}, {"situcraft-1", "proseg-defaults-1"}, {"proseg-1", "proseg-defaults-1"}} {
		_, ref.Pairs[pr[0]+" / "+pr[1]] = comparePair(pr[0], pr[1])
	}
	if _, ok := folders["truth"]; ok {
		for _, name := range []string{"instrument", "situcraft-1", "situcraft-2", "proseg-1", "proseg-2", "proseg-defaults-1", "proseg-defaults-2"} {
			_, ref.Pairs["truth / "+name] = comparePair("truth", name)
		}
	}
	ref.Checks = checks(set, ref)
	return ref
}

// importProseg imports a Proseg run's SpatialData store as a segmentation of the data set, as the
// app's Import a segmentation does.
func importProseg(ctx context.Context, t *testing.T, ds imports.Dataset, run, version, dir string, workers int) string {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	opt := imports.Options{Format: imports.SpatialData, Paths: []string{filepath.Join(run, "proseg-output.zarr")}, Units: "um", Tool: "Proseg", ToolVersion: version}
	_, findings, _, err := imports.Run(ctx, ds, opt, dir, workers, nil)
	if err != nil {
		t.Fatalf("importing %s: %v", run, err)
	}
	for _, f := range findings {
		if f.Severity == "fatal" {
			t.Fatalf("importing %s: %s: %s", run, f.Code, f.Message)
		}
		if f.Severity != "info" {
			t.Logf("%s: %s %s: %s", run, f.Severity, f.Code, f.Message)
		}
	}
	return dir
}

// pairOf reduces a comparison.
func pairOf(r *compare.Result) pairNumbers {
	p := pairNumbers{Classes: r.Classes, IoU: map[string]float64{}, ChangedQ20: r.ChangesQ20.Fraction, ChangedQ20OfA: r.ChangesQ20OfA.Fraction, PixelAgreement: r.PixelAgreement}
	if n := float64(r.ChangesQ20.Transcripts); n > 0 {
		p.LostQ20, p.GainedQ20 = round4(float64(r.ChangesQ20.Lost)/n), round4(float64(r.ChangesQ20.Gained)/n)
	}
	var ious, a, b []float64
	for _, g := range r.Groups {
		if g.Class != compare.OneToOne && g.Class != compare.Expanded && g.Class != compare.Contracted {
			continue
		}
		ious = append(ious, float64(g.IoU))
		a = append(a, float64(g.TranscriptsA))
		b = append(b, float64(g.TranscriptsB))
	}
	p.Matched = len(ious)
	sort.Float64s(ious)
	for _, q := range []int{10, 25, 50, 75, 90} {
		p.IoU[fmt.Sprintf("p%d", q)] = round4(quantile(ious, float64(q)/100))
	}
	la, lb := make([]float64, len(a)), make([]float64, len(b))
	for i := range a {
		la[i], lb[i] = math.Log1p(a[i]), math.Log1p(b[i])
	}
	p.Counts.PearsonLog = round4(pearson(la, lb))
	p.Counts.Spearman = round4(pearson(ranks(a), ranks(b)))
	return p
}

// checks evaluates the tolerances on a data set's numbers.
func checks(set string, ref *referenceNumbers) []check {
	s1, p1 := ref.Segmentations["situcraft-1"], ref.Segmentations["proseg-1"]
	within := func(a, b string) pairNumbers { return ref.Pairs[a+" / "+b] }
	sc, pr, between := within("situcraft-1", "situcraft-2"), within("proseg-1", "proseg-2"), within("situcraft-1", "proseg-1")
	var out []check
	add := func(name, tolerance string, value, limit float64, pass bool) {
		c := check{Name: name, Tolerance: tolerance, Value: round4(value), Limit: round4(limit), Pass: pass}
		if !pass {
			c.Explained = explained[set+": "+name]
		}
		out = append(out, c)
	}
	cells := math.Abs(float64(s1.Cells-p1.Cells)) / float64(p1.Cells)
	add("cells", "SituCraft's within 5% of Proseg's", cells, tolCells, cells <= tolCells)
	assigned := math.Abs(s1.AssignedFraction - p1.AssignedFraction)
	add("assigned fraction", "within 3 points of Proseg's", assigned, tolAssigned, assigned <= tolAssigned)
	sc1, pc1 := float64(s1.Coexpression), float64(p1.Coexpression)
	add("spurious co-expression", "SituCraft's at most 10% above Proseg's", sc1, tolCoexpression*pc1, sc1 <= tolCoexpression*pc1)
	r := math.Min(sc.Counts.PearsonLog, pr.Counts.PearsonLog) - tolCountCorrelation
	add("matched cells' counts", "SituCraft and Proseg correlated within 0.05 of the lower of their own runs' correlations", between.Counts.PearsonLog, r, between.Counts.PearsonLog >= r)
	iou := math.Min(sc.IoU["p50"], pr.IoU["p50"]) - tolIoU
	add("median IoU", "SituCraft and Proseg within 0.10 of the lower of their own runs' median IoUs", between.IoU["p50"], iou, between.IoU["p50"] >= iou)
	if t1, ok := ref.Pairs["truth / situcraft-1"]; ok {
		tp := ref.Pairs["truth / proseg-1"]
		add("transcripts in their true cell", "SituCraft's no more than 1 point below Proseg's", 1-t1.ChangedQ20OfA, 1-tp.ChangedQ20OfA-tolTrueCell, 1-t1.ChangedQ20OfA >= 1-tp.ChangedQ20OfA-tolTrueCell)
		add("median IoU with the truth", "SituCraft's no more than 0.02 below Proseg's", t1.IoU["p50"], tp.IoU["p50"]-tolTrueIoU, t1.IoU["p50"] >= tp.IoU["p50"]-tolTrueIoU)
		add("cells' transcripts in the background", "SituCraft's no more than Proseg's", t1.LostQ20, tp.LostQ20, t1.LostQ20 <= tp.LostQ20)
	}
	if set == "xenium-breast-2fov" {
		add("time (Breast)", "under 2 minutes on a laptop", s1.Seconds, budgetSeconds, s1.Seconds <= budgetSeconds)
		add("memory (Breast)", "under 4 GB on a laptop", float64(s1.PeakBytes), budgetBytes, s1.PeakBytes <= budgetBytes)
	}
	return out
}

func round4(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	return math.Round(x*1e4) / 1e4
}

// quantile of sorted values (linear between order statistics).
func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	pos := q * float64(len(sorted)-1)
	i := int(pos)
	if i+1 >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	return sorted[i] + (pos-float64(i))*(sorted[i+1]-sorted[i])
}

func pearson(a, b []float64) float64 {
	n := float64(len(a))
	if n < 2 {
		return math.NaN()
	}
	var ma, mb float64
	for i := range a {
		ma += a[i] / n
		mb += b[i] / n
	}
	var sab, saa, sbb float64
	for i := range a {
		sab += (a[i] - ma) * (b[i] - mb)
		saa += (a[i] - ma) * (a[i] - ma)
		sbb += (b[i] - mb) * (b[i] - mb)
	}
	return sab / math.Sqrt(saa*sbb)
}

// ranks are values' ranks, ties given their mean rank.
func ranks(v []float64) []float64 {
	idx := make([]int, len(v))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(i, j int) bool { return v[idx[i]] < v[idx[j]] })
	out := make([]float64, len(v))
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && v[idx[j+1]] == v[idx[i]] {
			j++
		}
		for k := i; k <= j; k++ {
			out[idx[k]] = float64(i+j)/2 + 1
		}
		i = j + 1
	}
	return out
}
