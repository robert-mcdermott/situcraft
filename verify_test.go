package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The certificate the page writes for its fixture (web/lib/certificate.test.mjs keeps
// testdata/certificate.zip its own): read here as intact, its fingerprint the page's over the
// canonical JSON the program writes the same way.
func TestCertificateReadAsThePageWroteIt(t *testing.T) {
	c, files, problems, err := readCertificateZip(filepath.Join("testdata", "certificate.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) > 0 {
		t.Fatalf("the page's certificate read with problems: %v", problems)
	}
	if c.Subject.ID != "exp-1" || len(c.Bundles) != 2 || len(c.Steps) < 10 {
		t.Fatalf("the certificate read: %+v, %d bundles, %d steps", c.Subject, len(c.Bundles), len(c.Steps))
	}
	if files["README.txt"] == nil || !bytes.Contains(files["README.txt"], []byte(c.Fingerprint)) {
		t.Fatal("the README does not name the fingerprint")
	}
	// Each of SituCraft's steps: its job's spec, as recorded, its paths stood in for.
	clu := stepByID(c, "clu-1")
	if clu == nil || clu.By != "program" || clu.Job == nil || clu.Manifest == nil || len(clu.Files) != 1 {
		t.Fatalf("the clustering's step: %+v", clu)
	}
	var m struct {
		Lineage struct {
			Job struct {
				Spec json.RawMessage `json:"spec"`
			} `json:"job"`
		} `json:"lineage"`
	}
	if err := json.Unmarshal(files[clu.Manifest.Path], &m); err != nil {
		t.Fatal(err)
	}
	local, missing := localize(m.Lineage.Job.Spec, map[string]string{"d-1": filepath.Join("data", "S01")}, func(string) string { return "" })
	var spec struct {
		Bundle string `json:"bundle"`
	}
	if err := json.Unmarshal(local, &spec); err != nil || len(missing) > 0 || spec.Bundle != filepath.Join("data", "S01") {
		t.Fatalf("the spec with this computer's paths: %s (missing %v)", local, missing)
	}
}

// A certificate edited after it was made: its fingerprint does not match, or a file it names is not
// as certified.
func TestCertificateEditsFound(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "certificate.zip"))
	if err != nil {
		t.Fatal(err)
	}
	edit := func(name string, change func([]byte) []byte) string {
		r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		w := zip.NewWriter(&out)
		for _, f := range r.File {
			rc, _ := f.Open()
			var b bytes.Buffer
			b.ReadFrom(rc)
			rc.Close()
			data := b.Bytes()
			if f.Name == name {
				data = change(data)
			}
			fw, _ := w.Create(f.Name)
			fw.Write(data)
		}
		w.Close()
		path := filepath.Join(t.TempDir(), "edited.zip")
		os.WriteFile(path, out.Bytes(), 0o644)
		return path
	}
	_, _, problems, err := readCertificateZip(edit("certificate.json", func(b []byte) []byte {
		return bytes.Replace(b, []byte(`"resolution": 1`), []byte(`"resolution": 2`), 1)
	}))
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "fingerprint") {
		t.Fatalf("a parameter edited: %v %v", problems, err)
	}
	_, _, problems, err = readCertificateZip(edit("artifacts/gra-1/manifest.json", func(b []byte) []byte {
		return bytes.Replace(b, []byte(`"longest": 50`), []byte(`"longest": 40`), 1)
	}))
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "artifacts/gra-1/manifest.json is not as certified") {
		t.Fatalf("a manifest edited: %v %v", problems, err)
	}
	if _, _, _, err := readCertificateZip(filepath.Join("testdata", "missing.zip")); err == nil {
		t.Fatal("a missing file read as a certificate")
	}
}

// Canonical JSON written as the page's JSON.stringify writes it.
func TestCanonicalJSONAsThePageWritesIt(t *testing.T) {
	v, err := decodeGeneric([]byte(`{"b": [1, 2.5, -0.001, 1e-7, 12345678901234567890], "a": "quote \" back \\ tab \t nl \n bell \u0007 µm – ✓", "c": {"z": null, "y": true}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":"quote \" back \\ tab \t nl \n bell \u0007 µm – ✓","b":[1,2.5,-0.001,1e-7,12345678901234567890],"c":{"y":true,"z":null}}`
	if got := string(canonicalJSON(v)); got != want {
		t.Fatalf("canonical JSON\n got %s\nwant %s", got, want)
	}
}

// Paths put back in a spec's text: nothing else of it changes, byte for byte.
func TestLocalizeKeepsTheSpecAsRecorded(t *testing.T) {
	spec := []byte(`{"bundle":"situcraft:bundle/d-1","design":{"unit":"subject","group":"condition"},"keep":"/////w==","table":"situcraft:file/labels.csv","other":"situcraft:bundle/d-2/outs"}`)
	out, missing := localize(spec, map[string]string{"d-1": "/data/S01"}, func(name string) string {
		if name == "labels.csv" {
			return "/data/labels.csv"
		}
		return ""
	})
	want := `{"bundle":"/data/S01","design":{"unit":"subject","group":"condition"},"keep":"/////w==","table":"/data/labels.csv","other":"situcraft:bundle/d-2/outs"}`
	if filepath.Separator == '/' && string(out) != want {
		t.Fatalf("localized\n got %s\nwant %s", out, want)
	}
	if len(missing) != 1 || missing[0] != "the bundle of d-2" {
		t.Fatalf("missing: %v", missing)
	}
}

// The verdict: differs when anything differs, not checkable when anything could not be checked.
func TestVerdict(t *testing.T) {
	r := &verifyReport{Bundles: []foundBundle{{Status: "found"}}, Steps: []stepFound{{Verdict: stepVerified}, {Verdict: stepAsRecorded}, {Verdict: stepNotRemade}}}
	if verdictOf(r) != stepVerified {
		t.Fatal(verdictOf(r))
	}
	r.Steps = append(r.Steps, stepFound{Verdict: stepUnchecked})
	if verdictOf(r) != stepUnchecked {
		t.Fatal(verdictOf(r))
	}
	r.Steps = append(r.Steps, stepFound{Verdict: stepDiffers})
	if verdictOf(r) != stepDiffers {
		t.Fatal(verdictOf(r))
	}
	if verdictOf(&verifyReport{Problems: []string{"x"}}) != stepDiffers {
		t.Fatal("an archive edited is a difference")
	}
}

func stepByID(c *certificate, id string) *certStep {
	for i := range c.Steps {
		if c.Steps[i].ID == id {
			return &c.Steps[i]
		}
	}
	return nil
}
