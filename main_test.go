// Adapted from MaveScape 0.8.0 main_test.go.

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"situcraft/internal/jobs"
)

func testApp(t *testing.T, files ...string) (*app, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	a, err := newApp(config{dataDir: dir, files: files})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := a.handler()
	if err != nil {
		t.Fatal(err)
	}
	return a, protect(handler, "127.0.0.1", defaultPort)
}

func request(t *testing.T, handler http.Handler, method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	req.Host = "127.0.0.1:8840"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestServesTheAppWithSecurityHeaders(t *testing.T) {
	_, handler := testApp(t)
	rec := request(t, handler, http.MethodGet, "/", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SituCraft") {
		t.Fatal("index.html not served")
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "connect-src 'self'") {
		t.Fatalf("missing CSP: %q", csp)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
	if rec.Header().Get("Cross-Origin-Embedder-Policy") != "require-corp" || rec.Header().Get("Cross-Origin-Opener-Policy") != "same-origin" {
		t.Fatal("the page is not cross-origin isolated (shared memory for workers)")
	}
	js := request(t, handler, http.MethodGet, "/app.js", nil, nil)
	if !strings.HasPrefix(js.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("app.js content type %q", js.Header().Get("Content-Type"))
	}
	lib := request(t, handler, http.MethodGet, "/lib/random.js", nil, nil)
	if lib.Code != http.StatusOK {
		t.Fatalf("lib/random.js status %d", lib.Code)
	}
}

// Everything the page loads must be embedded: a module missing from the go:embed list works with
// --dev and fails in the released program.
func TestEveryWebModuleIsEmbedded(t *testing.T) {
	err := filepath.WalkDir("web", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name := filepath.ToSlash(path)
		if strings.HasSuffix(name, ".test.mjs") || strings.HasSuffix(name, "package.json") {
			if _, err := fs.Stat(content, name); err == nil {
				t.Errorf("%s is embedded but is not part of the app", name)
			}
			return nil
		}
		if _, err := fs.Stat(content, name); err != nil {
			t.Errorf("%s is not embedded (add it to the go:embed list in main.go)", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInfoDescribesTheDesktopProgram(t *testing.T) {
	_, handler := testApp(t)
	rec := request(t, handler, http.MethodGet, "/api/info", nil, nil)
	var body info
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "SituCraft" || body.Version != version || body.DataDir == "" || body.Session == "" || body.Mode != "desktop" {
		t.Fatalf("unexpected info %+v", body)
	}
}

func TestTheBuildCommitComesFromTheLinkerFirst(t *testing.T) {
	saved := commit
	defer func() { commit = saved }()
	commit = "0123456789abcdef"
	if got := buildCommit(); got != "0123456789abcdef" {
		t.Fatalf("buildCommit() = %q", got)
	}
	_, handler := testApp(t)
	var body info
	json.NewDecoder(request(t, handler, http.MethodGet, "/api/info", nil, nil).Body).Decode(&body)
	if body.Commit != "0123456789abcdef" {
		t.Fatalf("info commit %q", body.Commit)
	}
}

func TestRejectsForeignHostsAndCrossOriginAPI(t *testing.T) {
	_, handler := testApp(t)
	req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	req.Host = "evil.example:8840"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("DNS-rebinding host allowed: %d", rec.Code)
	}
	cross := request(t, handler, http.MethodPut, "/api/library/projects/abc", strings.NewReader("{}"), map[string]string{"Origin": "https://evil.example"})
	if cross.Code != http.StatusForbidden {
		t.Fatalf("cross-origin write allowed: %d", cross.Code)
	}
	site := request(t, handler, http.MethodGet, "/api/info", nil, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if site.Code != http.StatusForbidden {
		t.Fatalf("cross-site fetch allowed: %d", site.Code)
	}
	otherPort := request(t, handler, http.MethodGet, "/api/info", nil, map[string]string{"Origin": "http://127.0.0.1:8765"})
	if otherPort.Code != http.StatusForbidden {
		t.Fatalf("another local program's page (another port) allowed: %d", otherPort.Code)
	}
	same := request(t, handler, http.MethodGet, "/api/info", nil, map[string]string{"Origin": "http://127.0.0.1:8840", "Sec-Fetch-Site": "same-origin"})
	if same.Code != http.StatusOK {
		t.Fatalf("same-origin request refused: %d", same.Code)
	}
}

func TestProjectLibraryRoundTrip(t *testing.T) {
	_, handler := testApp(t)
	doc := `{"format":"situcraft-project","version":1,"name":"Lung 2-FOV","modified":"2026-10-14T12:00:00Z","datasets":[{},{}]}`
	put := request(t, handler, http.MethodPut, "/api/library/projects/p1", strings.NewReader(doc), nil)
	if put.Code != http.StatusOK {
		t.Fatalf("put %d %s", put.Code, put.Body)
	}
	list := request(t, handler, http.MethodGet, "/api/library/projects", nil, nil)
	var listed struct {
		Projects []projectSummary `json:"projects"`
	}
	json.NewDecoder(list.Body).Decode(&listed)
	if len(listed.Projects) != 1 || listed.Projects[0].Name != "Lung 2-FOV" || listed.Projects[0].Datasets != 2 {
		t.Fatalf("unexpected list %+v", listed)
	}
	get := request(t, handler, http.MethodGet, "/api/library/projects/p1", nil, nil)
	if get.Body.String() != doc {
		t.Fatalf("round trip changed the document: %s", get.Body)
	}
	if bad := request(t, handler, http.MethodPut, "/api/library/projects/p2", strings.NewReader("{not json"), nil); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON accepted: %d", bad.Code)
	}
	if traversal := request(t, handler, http.MethodGet, "/api/library/projects/..%2Fsecret", nil, nil); traversal.Code == http.StatusOK {
		t.Fatal("path traversal allowed")
	}
	del := request(t, handler, http.MethodDelete, "/api/library/projects/p1", nil, nil)
	if del.Code != http.StatusOK {
		t.Fatalf("delete %d", del.Code)
	}
	if gone := request(t, handler, http.MethodGet, "/api/library/projects/p1", nil, nil); gone.Code != http.StatusNotFound {
		t.Fatalf("deleted project still served: %d", gone.Code)
	}
}

func TestRecordsRoundTripByKind(t *testing.T) {
	_, handler := testApp(t)
	if empty := request(t, handler, http.MethodGet, "/api/library/records/marker-set", nil, nil); !strings.Contains(empty.Body.String(), `"records":[]`) {
		t.Fatalf("empty list: %s", empty.Body)
	}
	doc := `{"name":"Immune lineages","modified":"2026-10-08T08:00:00Z","columns":[]}`
	if put := request(t, handler, http.MethodPut, "/api/library/records/marker-set/t1", strings.NewReader(doc), nil); put.Code != http.StatusOK {
		t.Fatalf("put %d %s", put.Code, put.Body)
	}
	list := request(t, handler, http.MethodGet, "/api/library/records/marker-set", nil, nil)
	var listed struct {
		Records []recordSummary `json:"records"`
	}
	json.NewDecoder(list.Body).Decode(&listed)
	if len(listed.Records) != 1 || listed.Records[0].ID != "t1" || listed.Records[0].Name != "Immune lineages" {
		t.Fatalf("unexpected list %+v", listed)
	}
	if get := request(t, handler, http.MethodGet, "/api/library/records/marker-set/t1", nil, nil); get.Body.String() != doc {
		t.Fatalf("round trip changed the record: %s", get.Body)
	}
	if other := request(t, handler, http.MethodGet, "/api/library/records/segmentation-preset/t1", nil, nil); other.Code != http.StatusNotFound {
		t.Fatalf("records leak across kinds: %d", other.Code)
	}
	if bad := request(t, handler, http.MethodPut, "/api/library/records/Bad_Kind/t1", strings.NewReader("{}"), nil); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid kind accepted: %d", bad.Code)
	}
	if bad := request(t, handler, http.MethodPut, "/api/library/records/segmentation-preset/x", strings.NewReader("{oops"), nil); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON accepted: %d", bad.Code)
	}
	if del := request(t, handler, http.MethodDelete, "/api/library/records/marker-set/t1", nil, nil); del.Code != http.StatusOK {
		t.Fatalf("delete %d", del.Code)
	}
	if gone := request(t, handler, http.MethodGet, "/api/library/records/marker-set/t1", nil, nil); gone.Code != http.StatusNotFound {
		t.Fatalf("deleted record still served: %d", gone.Code)
	}
}

func TestFilesAreStoredUnderTheirHash(t *testing.T) {
	a, handler := testApp(t)
	content := []byte("cell_id,label\naaaabbbb-1,T cell\n")
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	wrong := strings.Repeat("0", 64)
	if rec := request(t, handler, http.MethodPut, "/api/library/files/"+wrong, bytes.NewReader(content), nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatched hash accepted: %d", rec.Code)
	}
	if rec := request(t, handler, http.MethodPut, "/api/library/files/"+sha, bytes.NewReader(content), nil); rec.Code != http.StatusOK {
		t.Fatalf("put %d %s", rec.Code, rec.Body)
	}
	has := request(t, handler, http.MethodGet, "/api/library/has/"+sha, nil, nil)
	if !strings.Contains(has.Body.String(), `"exists":true`) {
		t.Fatalf("has: %s", has.Body)
	}
	missing := request(t, handler, http.MethodGet, "/api/library/has/"+wrong, nil, nil)
	if !strings.Contains(missing.Body.String(), `"exists":false`) {
		t.Fatalf("has missing: %s", missing.Body)
	}
	get := request(t, handler, http.MethodGet, "/api/library/files/"+sha, nil, nil)
	if !bytes.Equal(get.Body.Bytes(), content) {
		t.Fatal("stored file differs")
	}
	if _, err := os.Stat(filepath.Join(a.store.dir, "files", sha[:2], sha)); err != nil {
		t.Fatalf("file not at its content address: %v", err)
	}
}

func TestFilesAreAddedUnderTheHashTheProgramComputes(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "labels.csv")
	content := []byte("cell_id,label\naaaabbbb-1,B cell\naaaacccc-1,Macrophage\n")
	os.WriteFile(local, content, 0o644)
	a, handler := testApp(t, local)
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	same := map[string]string{"Sec-Fetch-Site": "same-origin"}
	var body struct {
		SHA256   string `json:"sha256"`
		Size     int64  `json:"size"`
		Existing bool   `json:"existing"`
	}
	rec := request(t, handler, http.MethodPost, "/api/library/local/0", nil, same)
	if rec.Code != http.StatusOK {
		t.Fatalf("local copy %d %s", rec.Code, rec.Body)
	}
	json.NewDecoder(rec.Body).Decode(&body)
	if body.SHA256 != sha || body.Size != int64(len(content)) || body.Existing {
		t.Fatalf("local copy answered %+v", body)
	}
	if _, err := os.Stat(filepath.Join(a.store.dir, "files", sha[:2], sha)); err != nil {
		t.Fatalf("local file not at its content address: %v", err)
	}
	rec = request(t, handler, http.MethodPost, "/api/library/files", bytes.NewReader(content), same)
	json.NewDecoder(rec.Body).Decode(&body)
	if rec.Code != http.StatusOK || body.SHA256 != sha || !body.Existing {
		t.Fatalf("upload of a stored file: %d %+v", rec.Code, body)
	}
	other := []byte(`{"type":"FeatureCollection","features":[]}`)
	rec = request(t, handler, http.MethodPost, "/api/library/files", bytes.NewReader(other), same)
	json.NewDecoder(rec.Body).Decode(&body)
	otherSum := sha256.Sum256(other)
	if rec.Code != http.StatusOK || body.SHA256 != hex.EncodeToString(otherSum[:]) || body.Existing {
		t.Fatalf("upload: %d %+v", rec.Code, body)
	}
	// Range requests read part of a stored file.
	part := request(t, handler, http.MethodGet, "/api/library/files/"+sha, nil, map[string]string{"Range": "bytes=10-14"})
	if part.Code != http.StatusPartialContent || part.Body.String() != string(content[10:15]) {
		t.Fatalf("range: %d %q", part.Code, part.Body.String())
	}
	if rec := request(t, handler, http.MethodPost, "/api/library/local/7", nil, same); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown local file: %d", rec.Code)
	}
	if rec := request(t, handler, http.MethodPost, "/api/library/files", bytes.NewReader(content), map[string]string{"Origin": "https://example.com"}); rec.Code == http.StatusOK {
		t.Fatal("cross-origin upload accepted")
	}
	entries, _ := os.ReadDir(filepath.Join(a.store.dir, "files"))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".upload-") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestBundlesAndFilesFromFoldersAreListed(t *testing.T) {
	dir := t.TempDir()
	study := filepath.Join(dir, "study")
	write := func(rel, data string) {
		path := filepath.Join(study, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(data), 0o644)
	}
	// Two bundles (read as one item each: nothing inside them is listed), a SpatialData store, a
	// loose image and table, and what is not opened (hidden, unknown).
	write("region10/experiment.xenium", "{}")
	write("region10/transcripts.parquet", "x")
	write("region2/experiment.xenium", "{}")
	write("region2/morphology_focus/morphology_focus_0000.ome.tif", "x")
	write("refined.zarr/.zgroup", "{}")
	write("he.ome.tif", "image")
	write("labels.csv", "cell_id,label")
	write("notes.docx", "x")
	write(".hidden/x.csv", "x")
	_, handler := testApp(t, study)
	var body info
	json.NewDecoder(request(t, handler, http.MethodGet, "/api/info", nil, nil).Body).Decode(&body)
	var names []string
	for _, f := range body.Files {
		names = append(names, fmt.Sprintf("%s:%s:%v", f.Name, f.Kind, f.Dir))
		if f.Folder != "study" {
			t.Fatalf("unexpected item %+v", f)
		}
	}
	want := "he.ome.tif:image:false,labels.csv:table:false,refined.zarr:zarr:true,region2:bundle:true,region10:bundle:true"
	if strings.Join(names, ",") != want {
		t.Fatalf("items %v, want %s", names, want)
	}
	if served := request(t, handler, http.MethodGet, body.Files[1].URL, nil, nil); served.Body.String() != "cell_id,label" {
		t.Fatalf("served %q", served.Body.String())
	}
	// A bundle is read where it is: never served whole nor copied into the library.
	if folder := request(t, handler, http.MethodGet, body.Files[3].URL, nil, nil); folder.Code != http.StatusBadRequest {
		t.Fatalf("a bundle folder was served: %d", folder.Code)
	}
	if copied := request(t, handler, http.MethodPost, "/api/library/local/3", nil, map[string]string{"Sec-Fetch-Site": "same-origin"}); copied.Code != http.StatusBadRequest {
		t.Fatalf("a bundle folder was copied into the library: %d", copied.Code)
	}
	if rec := request(t, handler, http.MethodGet, "/api/local/99", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown local item: %d", rec.Code)
	}
}

func TestABundleIsNamedByItsFolderOrItsManifest(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Xenium_V1_human_Lung_2fov_outs")
	os.MkdirAll(bundle, 0o755)
	os.WriteFile(filepath.Join(bundle, "experiment.xenium"), []byte("{}"), 0o644)
	files := newLocalFiles()
	if problems := files.add([]string{bundle, filepath.Join(bundle, "experiment.xenium")}); len(problems) > 0 {
		t.Fatal(problems)
	}
	list := files.list()
	if len(list) != 1 || list[0].Kind != "bundle" || !list[0].Dir || list[0].Name != "Xenium_V1_human_Lung_2fov_outs" {
		t.Fatalf("the folder and its manifest should be one bundle: %+v", list)
	}
	empty := filepath.Join(dir, "empty")
	os.MkdirAll(empty, 0o755)
	if problems := files.add([]string{empty}); len(problems) != 1 || !strings.Contains(problems[0], "no bundles or files") {
		t.Fatalf("an empty folder: %v", problems)
	}
}

func TestOpenRequiresThisComputer(t *testing.T) {
	_, handler := testApp(t)
	req := httptest.NewRequest(http.MethodPost, "/api/open", strings.NewReader(`{"paths":["/tmp"]}`))
	req.Host = "127.0.0.1:8840"
	req.RemoteAddr = "10.0.0.5:5555"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("remote peer allowed to open files: %d", rec.Code)
	}
}

func TestUnknownAPIAndStaticPaths(t *testing.T) {
	_, handler := testApp(t)
	if rec := request(t, handler, http.MethodGet, "/api/nothing", nil, nil); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Unknown API endpoint") {
		t.Fatalf("unknown API: %d %s", rec.Code, rec.Body)
	}
	// A path without an extension is a view of the app (index.html); a missing file is not.
	if rec := request(t, handler, http.MethodGet, "/viewer", nil, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SituCraft") {
		t.Fatalf("app path: %d", rec.Code)
	}
	if rec := request(t, handler, http.MethodGet, "/lib/missing.js", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing file: %d", rec.Code)
	}
}

func TestNaturalOrder(t *testing.T) {
	names := []string{"region10", "region2", "Region1", "slide B3", "slide A11", "slide A3"}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })
	want := "Region1,region2,region10,slide A3,slide A11,slide B3"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("got %s", got)
	}
}

func TestParseConfig(t *testing.T) {
	cfg, err := parseConfig([]string{"bundle/", "--port", "9000", "--window", "none", "he.ome.tif"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.port != 9000 || cfg.window != "none" || len(cfg.files) != 2 || cfg.dataDir == "" {
		t.Fatalf("unexpected config %+v", cfg)
	}
	defaults, _ := parseConfig(nil)
	if defaults.port != 8840 || defaults.host != "127.0.0.1" || defaults.window != "app" {
		t.Fatalf("unexpected defaults %+v", defaults)
	}
	if _, err := parseConfig([]string{"--window", "fullscreen"}); err == nil {
		t.Fatal("invalid --window accepted")
	}
	noOpen, _ := parseConfig([]string{"--no-open"})
	if noOpen.window != "none" {
		t.Fatal("--no-open should mean --window none")
	}
	if defaults.cacheLimit != 10e9 || defaults.cacheDir == "" {
		t.Fatalf("the default cache limit is %d", defaults.cacheLimit)
	}
	if limited, err := parseConfig([]string{"--cache-limit", "500MB"}); err != nil || limited.cacheLimit != 500e6 {
		t.Fatalf("--cache-limit 500MB gave %d, %v", limited.cacheLimit, err)
	}
	if _, err := parseConfig([]string{"--cache-limit", "lots"}); err == nil {
		t.Fatal("an invalid --cache-limit was accepted")
	}
}

func TestFileKinds(t *testing.T) {
	cases := map[string]string{
		"experiment.xenium": "bundle", "study.scz": "project",
		"cells.zarr.zip": "zarr", "Xenium_V1_human_Lung_2fov_outs.zip": "archive",
		"morphology.ome.tif": "image", "he.OME.TIFF": "image", "stain.tif": "image", "big.ome.btf": "image",
		"cells.geojson": "geometry", "rois.geojson.gz": "geometry",
		"transcripts.parquet": "table", "labels.csv": "table", "transcripts.csv.gz": "table", "meta.tsv": "table",
		"matrix.mtx.gz": "matrix", "counts.mtx": "matrix",
		"gene_panel.json": "", "notes.docx": "", "reads.fastq": "", "cell_feature_matrix.h5": "",
	}
	for name, want := range cases {
		if got := fileKind(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

// SituCraft's ports (the default and the ones it tries after it) must not be any sibling
// program's, so that each finds its own running copy and none answers for another.
func TestPortsStayClearOfTheSiblings(t *testing.T) {
	siblings := []struct {
		name        string
		first, last int
	}{
		{"Proteoscope", 8765, 8814},
		{"CytoWeave", 8770, 8789},
		{"MaveScape", 8820, 8839},
	}
	first, last := defaultPort, defaultPort+portTries-1
	if first != 8840 || last != 8859 {
		t.Fatalf("SituCraft's ports are %d–%d; the documentation says 8840–8859", first, last)
	}
	for _, s := range siblings {
		if first <= s.last && s.first <= last {
			t.Errorf("SituCraft's ports %d–%d overlap %s's %d–%d", first, last, s.name, s.first, s.last)
		}
	}
}

// A bundle named on the command line is described and validated through the API; other items are
// refused.
func TestBundleEndpoints(t *testing.T) {
	root := os.Getenv("SITUCRAFT_DATA")
	if root == "" {
		root = filepath.Join("validation", "cache")
	}
	bundle := filepath.Join(root, "xenium-lung-2fov")
	if _, err := os.Stat(filepath.Join(bundle, "experiment.xenium")); err != nil {
		t.Skip("the Lung bundle is not present (node validation/fetch.mjs xenium-lung-2fov)")
	}
	image := filepath.Join(t.TempDir(), "he.ome.tif")
	os.WriteFile(image, []byte("not really"), 0o644)
	dir := t.TempDir()
	a, err := newApp(config{dataDir: dir, cacheDir: filepath.Join(dir, "cache"), files: []string{bundle, image}})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := a.handler()
	handler := protect(h, "127.0.0.1", defaultPort)
	var described struct {
		Name, Version, Support string
		Inventory              []struct{ Role string }
		Fingerprint            []struct{ Path, SHA256 string }
	}
	rec := request(t, handler, http.MethodGet, "/api/local/0/bundle", nil, nil)
	if err := json.NewDecoder(rec.Body).Decode(&described); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("describe: %d %v", rec.Code, err)
	}
	if described.Name != "xenium-lung-2fov" || described.Version != "2.0.0" || described.Support != "supported" || len(described.Inventory) < 10 || len(described.Fingerprint) != 22 {
		t.Fatalf("described %+v", described)
	}
	var validated struct {
		Report struct {
			Findings []struct{ Severity string }
			Summary  struct{ Cells int }
		}
	}
	rec = request(t, handler, http.MethodGet, "/api/local/0/validation", nil, nil)
	if err := json.NewDecoder(rec.Body).Decode(&validated); err != nil || rec.Code != http.StatusOK || validated.Report.Summary.Cells != 11898 {
		t.Fatalf("validate: %d %v %+v", rec.Code, err, validated)
	}
	for _, f := range validated.Report.Findings {
		if f.Severity == "fatal" {
			t.Fatal("the Lung bundle has a fatal finding")
		}
	}
	if rec := request(t, handler, http.MethodGet, "/api/local/1/validation", nil, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("an image validated as a bundle: %d", rec.Code)
	}
	if rec := request(t, handler, http.MethodGet, "/api/local/9/bundle", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown item: %d", rec.Code)
	}
}

func TestAnEmptyPathIsNotTheCurrentFolder(t *testing.T) {
	files := newLocalFiles()
	problems := files.add([]string{"", "   "})
	if len(files.list()) != 0 || len(problems) != 2 {
		t.Fatalf("empty paths registered %v (problems %v)", files.list(), problems)
	}
}

func TestBundleCacheEndpoints(t *testing.T) {
	root := os.Getenv("SITUCRAFT_DATA")
	if root == "" {
		root = filepath.Join("validation", "cache")
	}
	bundle := filepath.Join(root, "xenium-lung-2fov")
	if _, err := os.Stat(filepath.Join(bundle, "experiment.xenium")); err != nil {
		t.Skip("the Lung bundle is not present (node validation/fetch.mjs xenium-lung-2fov)")
	}
	dir := t.TempDir()
	a, err := newApp(config{dataDir: dir, cacheDir: filepath.Join(dir, "cache"), cacheLimit: 10e9, files: []string{bundle}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.jobs.Close()
	h, _ := a.handler()
	handler := protect(h, "127.0.0.1", defaultPort)
	type answer struct {
		Status struct {
			Ready   bool
			Entries []struct{ Kind, State, Name string }
		}
		Job *struct{ ID, State string }
	}
	get := func(method string) answer {
		t.Helper()
		rec := request(t, handler, method, "/api/local/0/cache", nil, nil)
		var out answer
		if err := json.NewDecoder(rec.Body).Decode(&out); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s cache: %d %v", method, rec.Code, err)
		}
		return out
	}
	first := get(http.MethodGet)
	if first.Status.Ready || len(first.Status.Entries) != 3 || first.Job != nil {
		t.Fatalf("before building: %+v", first)
	}
	for _, e := range first.Status.Entries {
		if e.State != "unchecked" {
			t.Errorf("%s is %s before the bundle's files were read", e.Kind, e.State)
		}
	}
	started := get(http.MethodPost)
	if started.Job == nil {
		t.Fatal("no job was started")
	}
	if again := get(http.MethodPost); again.Job == nil || again.Job.ID != started.Job.ID {
		t.Errorf("a second request started another job: %+v", again.Job)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	done, err := a.jobs.Wait(ctx, started.Job.ID)
	if err != nil || done.State != jobs.Complete {
		t.Fatalf("the job ended %s: %s (%v)", done.State, done.Error, err)
	}
	ready := get(http.MethodGet)
	if !ready.Status.Ready {
		t.Fatalf("after the job: %+v", ready.Status)
	}
	if after := get(http.MethodPost); after.Job != nil {
		t.Error("a job was started with everything built")
	}
	var usage struct {
		Size  int64
		Items []struct{ Name, Kind, State string }
	}
	rec := request(t, handler, http.MethodGet, "/api/cache", nil, nil)
	if err := json.NewDecoder(rec.Body).Decode(&usage); err != nil || len(usage.Items) != 3 || usage.Size < 50e6 {
		t.Fatalf("usage %+v %v", usage, err)
	}
	var list struct{ Jobs []struct{ ID, State string } }
	rec = request(t, handler, http.MethodGet, "/api/jobs", nil, nil)
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil || len(list.Jobs) != 1 || list.Jobs[0].State != "complete" {
		t.Fatalf("jobs %+v %v", list, err)
	}
	rec = request(t, handler, http.MethodGet, "/api/jobs/"+started.Job.ID, nil, nil)
	var full struct{ Events []struct{ Message string } }
	if err := json.NewDecoder(rec.Body).Decode(&full); err != nil || len(full.Events) < 4 {
		t.Fatalf("the job's log %+v %v", full, err)
	}
}

func TestJobsOverHTTP(t *testing.T) {
	a, handler := testApp(t)
	defer a.jobs.Close()
	if rec := request(t, handler, http.MethodGet, "/api/cache", nil, nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("without a cache folder: %d", rec.Code)
	}
	if rec := request(t, handler, http.MethodPost, "/api/jobs/j99/cancel", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("cancelling an unknown job: %d", rec.Code)
	}
	if rec := request(t, handler, http.MethodPost, "/api/jobs/j1/cancel", nil, map[string]string{"Origin": "https://example.com"}); rec.Code != http.StatusForbidden {
		t.Errorf("a cross-origin cancel: %d", rec.Code)
	}
	release := make(chan struct{})
	job, _, err := a.jobs.Submit("test", "Waits", "", nil, func(ctx context.Context, r *jobs.Run) error {
		r.Progress(0.5, "halfway")
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The event stream sends the list at once and again when a job changes.
	server := httptest.NewUnstartedServer(nil)
	port := server.Listener.Addr().(*net.TCPAddr).Port
	h, _ := a.handler()
	server.Config.Handler = protect(h, "127.0.0.1", port)
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/jobs/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	events := bufio.NewScanner(resp.Body)
	next := func() []struct{ ID, State string } {
		t.Helper()
		for events.Scan() {
			if line := events.Text(); strings.HasPrefix(line, "data: ") {
				var list []struct{ ID, State string }
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &list); err != nil {
					t.Fatal(err)
				}
				return list
			}
		}
		t.Fatal("the stream ended")
		return nil
	}
	if list := next(); len(list) != 1 || list[0].ID != job.ID {
		t.Fatalf("first event %+v", list)
	}
	rec := request(t, handler, http.MethodPost, "/api/jobs/"+job.ID+"/cancel", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "true") {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if list := next(); len(list) == 1 && list[0].State == "cancelled" {
			return
		}
	}
	t.Fatal("the stream never said the job was cancelled")
}
