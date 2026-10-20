package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"situcraft/internal/simulate"
)

func TestSimulateWritesABundleLabelledSimulated(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "tissue")
	var out, errs bytes.Buffer
	if code := runSimulate([]string{"--preset", "small", "--width", "60", "--height", "50", "--quiet", folder}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "Simulated ") || !strings.Contains(out.String(), "in 60 × 50 µm") {
		t.Errorf("summary: %s", out.String())
	}
	raw, err := os.ReadFile(filepath.Join(folder, simulate.SimulationFile))
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Simulated  bool
		Parameters simulate.Params
	}
	if err := json.Unmarshal(raw, &d); err != nil || !d.Simulated || d.Parameters.WidthUm != 60 {
		t.Errorf("%s: %+v, %v", simulate.SimulationFile, d, err)
	}
	// The folder is new: a second run refuses to write into it.
	if code := runSimulate([]string{"--preset", "small", "--quiet", folder}, &out, &errs); code != 1 || !strings.Contains(errs.String(), "already exists") {
		t.Errorf("a second run into the folder: exit %d, %s", code, errs.String())
	}
}

func TestSimulateChecksItsParameters(t *testing.T) {
	var out, errs bytes.Buffer
	if code := runSimulate([]string{"--preset", "huge", "x"}, &out, &errs); code != 2 || !strings.Contains(errs.String(), `no preset "huge"`) {
		t.Errorf("an unknown preset: exit %d, %s", code, errs.String())
	}
	errs.Reset()
	if code := runSimulate([]string{"--width", "5", "x"}, &out, &errs); code != 2 || !strings.Contains(errs.String(), "20 µm to 20 mm") {
		t.Errorf("a field too small: exit %d, %s", code, errs.String())
	}
	params := filepath.Join(t.TempDir(), "p.json")
	os.WriteFile(params, []byte(`{"seed": 7, "fil": 0.5}`), 0o644)
	errs.Reset()
	if code := runSimulate([]string{"--params", params, "x"}, &out, &errs); code != 2 || !strings.Contains(errs.String(), `unknown field "fil"`) {
		t.Errorf("a misspelled parameter: exit %d, %s", code, errs.String())
	}
	out.Reset()
	if code := runSimulate([]string{"--preset", "small", "--seed", "9", "--print-params"}, &out, &errs); code != 0 || !strings.Contains(out.String(), `"seed": 9`) {
		t.Errorf("--print-params: exit %d, %s", code, out.String())
	}
}

// --cohort: the parameters printed and read back, a cohort written with its truth and table,
// and what a cohort cannot have refused.
func TestSimulateWritesACohort(t *testing.T) {
	dir := t.TempDir()
	var out, errs bytes.Buffer
	if code := runSimulate([]string{"--cohort", "standard", "--print-params"}, &out, &errs); code != 0 {
		t.Fatalf("print: exit %d: %s", code, errs.String())
	}
	var c simulate.Cohort
	if err := json.Unmarshal(out.Bytes(), &c); err != nil || len(c.Effects) != 3 || c.Subjects != 4 {
		t.Fatalf("the printed parameters: %v", err)
	}
	// Smaller, read from a file.
	c.Subjects, c.Slides = 2, 0
	params := filepath.Join(dir, "cohort.json")
	raw, _ := json.Marshal(c)
	os.WriteFile(params, raw, 0o644)
	folder := filepath.Join(dir, "cohort")
	out.Reset()
	if code := runSimulate([]string{"--cohort", "standard", "--params", params, "--width", "100", "--height", "100", "--quiet", folder}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if !strings.Contains(out.String(), "Simulated a cohort of 4 sections (2 subjects in each of control and treated)") || !strings.Contains(out.String(), "Effect: level of SIM033 in epithelial-like, ×2.5 in treated") {
		t.Errorf("summary: %s", out.String())
	}
	for _, name := range []string{simulate.CohortFile, simulate.CohortTruthFile, simulate.CohortSamplesFile, "S01/experiment.xenium", "S04/experiment.xenium"} {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	errs.Reset()
	if code := runSimulate([]string{"--cohort", "standard", "--post-xenium", filepath.Join(dir, "x")}, &out, &errs); code != 2 || !strings.Contains(errs.String(), "no post-Xenium images") {
		t.Errorf("--post-xenium: exit %d, %s", code, errs.String())
	}
	errs.Reset()
	if code := runSimulate([]string{"--cohort", "huge", filepath.Join(dir, "y")}, &out, &errs); code != 2 || !strings.Contains(errs.String(), "null, outlier, paired, standard") {
		t.Errorf("an unknown cohort: exit %d, %s", code, errs.String())
	}
}

// A simulated bundle records this version as what wrote it, unless the validation suite asks for
// an earlier release's (SITUCRAFT_SIMULATE_AS), to make its bundles again exactly.
func TestSimulatedByThisVersionOrTheOneAskedFor(t *testing.T) {
	t.Setenv("SITUCRAFT_SIMULATE_AS", "")
	if simulatedBy() != version {
		t.Fatalf("by %q", simulatedBy())
	}
	t.Setenv("SITUCRAFT_SIMULATE_AS", "0.7.0")
	if simulatedBy() != "0.7.0" {
		t.Fatalf("by %q", simulatedBy())
	}
}
