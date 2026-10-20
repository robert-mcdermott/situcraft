package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

// The report the page writes for its fixture (web/lib/report.test.mjs keeps testdata/report.html
// its own): the certificate it carries read as intact, its numbers, figure and methods the
// certificate's; each edited, found.
func TestReportReadAsThePageWroteIt(t *testing.T) {
	path := filepath.Join("testdata", "report.html")
	page, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c, files, problems, err := readCertificateZip(path)
	if err != nil || len(problems) > 0 {
		t.Fatalf("the report's certificate: %v %v", problems, err)
	}
	if c.Subject.Kind != "report" || c.Subject.Name != "Results of the fixture" {
		t.Fatalf("its subject: %+v", c.Subject)
	}
	checked, problems := checkReport(page, c, files)
	if len(problems) > 0 || !strings.Contains(checked, "1 figure and its methods") || strings.HasPrefix(checked, "0 ") {
		t.Fatalf("the report checked: %q %v", checked, problems)
	}
	// The first match of re replaced.
	edit := func(re, with string) []string {
		pattern := regexp.MustCompile(re)
		at := pattern.FindSubmatchIndex(page)
		if at == nil {
			t.Fatalf("%s: nothing edited", re)
		}
		edited := append(append(append([]byte{}, page[:at[0]]...), pattern.Expand(nil, []byte(with), page, at)...), page[at[1]:]...)
		_, problems := checkReport(edited, c, files)
		return problems
	}
	if p := edit(`data-value="2"`, `data-value="3"`); len(p) != 1 || !strings.Contains(p[0], "numbers is not the certificate's") {
		t.Fatalf("a number edited: %v", p)
	}
	if p := edit(`(data-value="2"[^>]*>)2<`, `${1}3<`); len(p) != 1 || !strings.Contains(p[0], "numbers is not the certificate's") {
		t.Fatalf("a number's text edited: %v", p)
	}
	if p := edit(`(<p class="methods-paragraph">)`, `${1}Also, `); len(p) != 1 || !strings.Contains(p[0], "methods") {
		t.Fatalf("the methods edited: %v", p)
	}
	if p := edit(`(<img data-figure="fig-1"[^>]*base64,)[A-Za-z0-9]`, `${1}A`); len(p) != 1 || !strings.Contains(p[0], "figures") {
		t.Fatalf("a figure edited: %v", p)
	}
}

// A number written as the report writes it, in JavaScript (web/lib/report-check.js's numberText;
// these its own answers): its digits rounded from its exact value, a tie away from zero.
func TestNumberTextAsThePageWritesIt(t *testing.T) {
	for _, c := range []struct{ number, text string }{
		{"0", "0"},
		{"7", "7"},
		{"-12", "-12"},
		{"1234567", "1,234,567"},
		{"123456789012", "123,456,789,012"},
		{"1.0625", "1.063"},
		{"2.5625", "2.563"},
		{"-1.0625", "-1.063"},
		{"0.15625", "0.1563"},
		{"999.95", "1000"},
		{"999.94999", "999.9"},
		{"999999.7", "1000000"},
		{"12345.6", "12350"},
		{"1234.5", "1235"},
		{"0.0016650016650016", "0.001665"},
		{"0.001", "0.001"},
		{"0.00099995", "1.00e-3"},
		{"0.0009999", "1.00e-3"},
		{"0.00012345", "1.23e-4"},
		{"2.5e-7", "2.50e-7"},
		{"0.00001125", "1.13e-5"},
		{"3500000", "3,500,000"},
		{"0.30000000000000004", "0.3"},
		{"0.3333333333333333", "0.3333"},
		{"-7.592", "-7.592"},
		{"2.0660598", "2.066"},
		{"0.4362947245656103", "0.4363"},
		{"5e-324", "4.94e-324"},
		{"100.5", "100.5"},
		{"4.0005", "4"},
		{"1152921504606847000", "1,152,921,504,606,847,000"},
		{"9007199254740994", "9,007,199,254,740,994"},
		{"1e+21", "1,000,000,000,000,000,000,000"},
		{"-1.1805916207174113e+21", "-1,180,591,620,717,411,300,000"},
	} {
		if got := numberText(json.Number(c.number)); got != c.text {
			t.Errorf("%s: %q, the page writes %q", c.number, got, c.text)
		}
	}
}

// A result package (wave 8, slice 8) given to verify: its folder, or a zip of it, read by its
// report (index.html), the certificate it carries read as intact.
func TestPackageReadByItsReport(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("testdata", "report.html"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "Results-12345678")
	os.MkdirAll(filepath.Join(dir, "views"), 0o755)
	os.WriteFile(filepath.Join(dir, "index.html"), page, 0o644)
	os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"format":"situcraft-package"}`), 0o644)
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, data := range map[string][]byte{"Results-12345678/index.html": page, "Results-12345678/manifest.json": []byte("{}")} {
		f, _ := w.Create(name)
		f.Write(data)
	}
	w.Close()
	zipped := filepath.Join(t.TempDir(), "Results-12345678.zip")
	os.WriteFile(zipped, b.Bytes(), 0o644)
	for _, path := range []string{dir, zipped} {
		c, files, problems, err := readCertificateZip(path)
		if err != nil || len(problems) > 0 || c.Subject.Kind != "report" {
			t.Fatalf("%s: %v %v", filepath.Base(path), problems, err)
		}
		got, _, err := submitted(path)
		if err != nil || !bytes.Equal(got, page) {
			t.Fatalf("%s: its report not read: %v", filepath.Base(path), err)
		}
		if _, problems := checkReport(got, c, files); len(problems) > 0 {
			t.Fatalf("%s: %v", filepath.Base(path), problems)
		}
	}
	if _, _, err := submitted(t.TempDir()); err == nil {
		t.Fatal("a folder without a report read as a package")
	}
}

// A step whose job was given names the certificate leaves out: not checkable, before anything is
// made again.
func TestARedactedStepIsNotMadeAgain(t *testing.T) {
	found := stepFound{}
	remakeStep(nil, nil, certStep{N: 3, ID: "coh-1", By: "program", Redacted: true}, nil, nil, nil, map[int]*stepFound{}, map[string]bool{}, &found)
	if found.Verdict != stepUnchecked || !strings.Contains(found.Detail, "names this certificate leaves out") {
		t.Fatalf("%+v", found)
	}
}

// Verifying in SituCraft itself (wave 8, slice 9): what is not a certificate refused at once; the
// bundles it looks among, each project's datasets' folders and the examples'.
func TestVerifyInSituCraft(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(config{dataDir: filepath.Join(dir, "library"), cacheDir: filepath.Join(dir, "cache"), examplesDir: filepath.Join(dir, "examples")})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := a.handler()
	if err != nil {
		t.Fatal(err)
	}
	h := protect(handler, "127.0.0.1", defaultPort)
	if rec := request(t, h, http.MethodPost, "/api/verify?name=notes.zip", strings.NewReader("not a zip"), nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not a SituCraft certificate") {
		t.Fatalf("not a certificate: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, h, http.MethodPost, "/api/verify", strings.NewReader(`{"path":"certificate.zip"}`), map[string]string{"Content-Type": "application/json"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a relative path: %d %s", rec.Code, rec.Body)
	}
	if rec := request(t, h, http.MethodGet, "/api/verify/job-unknown", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown verification: %d", rec.Code)
	}
	// A bundle a project names, one among the examples (and a generated cohort's section); a
	// missing folder and a zipped bundle are not looked in.
	bundle := filepath.Join(dir, "data", "S01")
	example := filepath.Join(a.examples.Dir, "xenium-lung-2fov")
	section := filepath.Join(a.examples.Dir, "simulated-cohort", "S02")
	for _, d := range []string{bundle, example, section} {
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "experiment.xenium"), []byte("{}"), 0o644)
	}
	project := `{"id":"p-1","datasets":[{"id":"d-1","path":` + strconv.Quote(bundle) + `},{"id":"d-2","path":` + strconv.Quote(filepath.Join(dir, "gone")) + `},{"id":"d-3","path":` + strconv.Quote(filepath.Join(dir, "S03.zip")) + `}]}`
	os.MkdirAll(filepath.Join(a.store.dir, "projects"), 0o755)
	os.WriteFile(filepath.Join(a.store.dir, "projects", "p-1.json"), []byte(project), 0o644)
	got := a.knownBundleFolders()
	sort.Strings(got)
	want := []string{bundle, example, section}
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("bundles known: %v", got)
	}
}
