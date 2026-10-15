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
