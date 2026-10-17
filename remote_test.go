package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakePage answers every action the hub sends, like the browser page would.
func fakePage(t *testing.T, hub *remoteHub, answer func(remoteEvent) remoteResult) func() {
	t.Helper()
	client := hub.connect()
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case event := <-client.events:
				result, ok := hub.take(event.ID)
				if ok {
					result <- answer(event)
				}
			}
		}
	}()
	return func() {
		close(stop)
		hub.disconnect(client)
	}
}

func hubMux(hub *remoteHub) *http.ServeMux {
	mux := http.NewServeMux()
	hub.register(mux)
	return mux
}

// post sends a request as a program on this computer does.
func post(mux http.Handler, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Host = "127.0.0.1:8840"
	req.RemoteAddr = "127.0.0.1:50000"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestActionsReachThePageAndItsAnswerReturns(t *testing.T) {
	hub := newRemoteHub()
	mux := hubMux(hub)
	var seen remoteEvent
	stop := fakePage(t, hub, func(event remoteEvent) remoteResult {
		seen = event
		return remoteResult{OK: true, Message: "Shown.", Data: json.RawMessage(`{"mode":"map"}`)}
	})
	defer stop()
	rec := post(mux, "/api/remote/action", `{"action":"set_mode","args":{"mode":"map"},"client":"capture\u0007 script"}`, nil)
	var result remoteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || rec.Code != http.StatusOK || !result.OK || string(result.Data) != `{"mode":"map"}` {
		t.Fatalf("action: %d %s", rec.Code, rec.Body)
	}
	if seen.Action != "set_mode" || string(seen.Args) != `{"mode":"map"}` || seen.Client != "capture script" || seen.Output != "" || len(seen.Files) != 0 {
		t.Fatalf("the page got %+v", seen)
	}
	// An action the hub does not list still reaches the page, which answers with the ones it knows.
	if rec := post(mux, "/api/remote/action", `{"action":"no_such_action"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("unknown action: %d %s", rec.Code, rec.Body)
	}
	if rec := post(mux, "/api/remote/action", `not json`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad request: %d", rec.Code)
	}
}

func TestWithoutAPageActionsAreRefused(t *testing.T) {
	mux := hubMux(newRemoteHub())
	rec := post(mux, "/api/remote/action", `{"action":"get_state"}`, nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "No SituCraft page") {
		t.Fatalf("no page: %d %s", rec.Code, rec.Body)
	}
}

func TestActionsRunOneAtATime(t *testing.T) {
	hub := newRemoteHub()
	mux := hubMux(hub)
	var running, most atomic.Int32
	stop := fakePage(t, hub, func(event remoteEvent) remoteResult {
		n := running.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return remoteResult{OK: true, Message: event.Action}
	})
	defer stop()
	var wg sync.WaitGroup
	codes := make([]int, 6)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = post(mux, "/api/remote/action", `{"action":"get_state"}`, nil).Code
		}()
	}
	wg.Wait()
	for i, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("action %d: %d", i, code)
		}
	}
	if most.Load() != 1 {
		t.Fatalf("%d actions ran at once", most.Load())
	}
}

func TestAPageThatDoesNotAnswerOrLeaves(t *testing.T) {
	hub := newRemoteHub()
	hub.timeout = 50 * time.Millisecond
	mux := hubMux(hub)
	silent := hub.connect()
	if rec := post(mux, "/api/remote/action", `{"action":"get_state"}`, nil); rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("a silent page: %d %s", rec.Code, rec.Body)
	}
	hub.disconnect(silent)
	hub.timeout = time.Minute
	leaving := hub.connect()
	go func() {
		<-leaving.events
		hub.disconnect(leaving)
	}()
	if rec := post(mux, "/api/remote/action", `{"action":"get_state"}`, nil); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "closed or reloaded") {
		t.Fatalf("a page that left: %d %s", rec.Code, rec.Body)
	}
	// Long actions get an hour, others the hub's timeout.
	if a, _ := findAction("add_dataset"); !a.long {
		t.Fatal("add_dataset should be a long action")
	}
}

// Only a page opened from this run of the program takes its actions: a tab left open while
// SituCraft was started again (another version, another library) is refused when it reconnects.
func TestOnlyThisRunsPageIsTheWindow(t *testing.T) {
	hub := newRemoteHub()
	hub.session = "run-1"
	mux := hubMux(hub)
	events := func(query string) (int, string) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/remote/events"+query, nil).WithContext(ctx)
		req.Host = "127.0.0.1:8840"
		req.RemoteAddr = "127.0.0.1:50000"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	for _, query := range []string{"", "?session=run-0"} {
		if code, body := events(query); code != http.StatusConflict || !strings.Contains(body, "another run of SituCraft") {
			t.Fatalf("a page of another run (%q): %d %s", query, code, body)
		}
	}
	if code, body := events("?session=run-1"); code != http.StatusOK || !strings.Contains(body, ": connected") {
		t.Fatalf("this run's page: %d %s", code, body)
	}
}

func TestOnlyThisComputerMayControl(t *testing.T) {
	hub := newRemoteHub()
	mux := hubMux(hub)
	for _, c := range []struct{ method, path, host, remote string }{
		{http.MethodPost, "/api/remote/action", "127.0.0.1:8840", "10.0.0.2:50000"},
		{http.MethodPost, "/api/remote/action", "evil.example:8840", "127.0.0.1:50000"},
		{http.MethodGet, "/api/remote/events", "evil.example:8840", "127.0.0.1:50000"},
		{http.MethodGet, "/api/remote/tools", "127.0.0.1:8840", "10.0.0.2:50000"},
		{http.MethodPost, "/api/remote/result/1", "127.0.0.1:8840", "10.0.0.2:50000"},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{"action":"get_state"}`))
		req.Host = c.host
		req.RemoteAddr = c.remote
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s from %s as %s: %d", c.method, c.path, c.remote, c.host, rec.Code)
		}
	}
}

func TestOpeningPathsNeedsTheToken(t *testing.T) {
	hub := newRemoteHub()
	files := newLocalFiles()
	hub.open = func(paths []string) ([]localFile, []string) {
		problems := files.add(paths)
		return files.within(paths), problems
	}
	mux := hubMux(hub)
	var seen remoteEvent
	stop := fakePage(t, hub, func(event remoteEvent) remoteResult {
		seen = event
		return remoteResult{OK: true}
	})
	defer stop()
	// A folder holding a bundle and an archive; a bundle named by its manifest is the bundle.
	dir := t.TempDir()
	bundle := filepath.Join(dir, "Region_1")
	os.MkdirAll(bundle, 0o755)
	os.WriteFile(filepath.Join(bundle, "experiment.xenium"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "lung.scz"), []byte("PK"), 0o644)
	body, _ := json.Marshal(map[string]any{"action": "open_paths", "args": map[string]any{"paths": []string{filepath.Join(bundle, "experiment.xenium")}}})
	if rec := post(mux, "/api/remote/action", string(body), nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("open_paths without the token: %d", rec.Code)
	}
	if rec := post(mux, "/api/remote/action", string(body), map[string]string{remoteTokenHeader: "wrong"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("open_paths with a wrong token: %d", rec.Code)
	}
	if rec := post(mux, "/api/remote/action", string(body), map[string]string{remoteTokenHeader: hub.token}); rec.Code != http.StatusOK {
		t.Fatalf("open_paths: %d %s", rec.Code, rec.Body)
	}
	if len(seen.Files) != 1 || seen.Files[0].Name != "Region_1" || seen.Files[0].Kind != "bundle" || seen.Files[0].URL == "" {
		t.Fatalf("the page got items %+v", seen.Files)
	}
	folder, _ := json.Marshal(map[string]any{"action": "open_paths", "args": map[string]any{"paths": []string{dir}}})
	if rec := post(mux, "/api/remote/action", string(folder), map[string]string{remoteTokenHeader: hub.token}); rec.Code != http.StatusOK || len(seen.Files) != 2 {
		t.Fatalf("a folder: %d %+v", rec.Code, seen.Files)
	}
	missing, _ := json.Marshal(map[string]any{"action": "open_paths", "args": map[string]any{"paths": []string{filepath.Join(dir, "none")}}})
	if rec := post(mux, "/api/remote/action", string(missing), map[string]string{remoteTokenHeader: hub.token}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a missing path: %d", rec.Code)
	}
	if rec := post(mux, "/api/remote/action", `{"action":"open_paths","args":{}}`, map[string]string{remoteTokenHeader: hub.token}); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "paths") {
		t.Fatalf("no paths: %d %s", rec.Code, rec.Body)
	}
	// A post-Xenium image is read where it lies: the token, and the page given its path, nothing opened.
	image, _ := json.Marshal(map[string]any{"action": "add_image", "args": map[string]any{"path": filepath.Join(dir, "S1_he_image.ome.tif")}})
	if rec := post(mux, "/api/remote/action", string(image), nil); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "Reading files") {
		t.Fatalf("add_image without the token: %d %s", rec.Code, rec.Body)
	}
	seen = remoteEvent{}
	if rec := post(mux, "/api/remote/action", string(image), map[string]string{remoteTokenHeader: hub.token}); rec.Code != http.StatusOK || seen.Action != "add_image" || len(seen.Files) != 0 {
		t.Fatalf("add_image: %d %s, the page got %+v", rec.Code, rec.Body, seen)
	}
}

// A page that answers the export action by uploading a file, as remote.js does.
func uploadingPage(t *testing.T, hub *remoteHub, mux http.Handler, body string) func() {
	return fakePage(t, hub, func(event remoteEvent) remoteResult {
		if event.Output == "" {
			return remoteResult{OK: false, Message: "no output slot"}
		}
		rec := post(mux, "/"+event.Output, body, nil)
		if rec.Code != http.StatusOK {
			return remoteResult{OK: false, Message: rec.Body.String()}
		}
		return remoteResult{OK: true, Message: "Wrote " + rec.Body.String()}
	})
}

func TestExportsWriteWhereAskedAndNeverReplaceUnlessTold(t *testing.T) {
	hub := newRemoteHub()
	mux := hubMux(hub)
	stop := uploadingPage(t, hub, mux, "A human lung section was profiled.\n")
	defer stop()
	dir := t.TempDir()
	target := filepath.Join(dir, "methods.md")
	call := func(args map[string]any) (int, remoteResult) {
		body, _ := json.Marshal(map[string]any{"action": "export", "args": args})
		rec := post(mux, "/api/remote/action", string(body), map[string]string{remoteTokenHeader: hub.token})
		var result remoteResult
		json.Unmarshal(rec.Body.Bytes(), &result)
		return rec.Code, result
	}
	if code, result := call(map[string]any{"what": "methods", "path": target}); code != http.StatusOK || !result.OK {
		t.Fatalf("export: %d %+v", code, result)
	}
	if got, _ := os.ReadFile(target); string(got) != "A human lung section was profiled.\n" {
		t.Fatalf("file holds %q", got)
	}
	// Refused before the page is asked: an existing file, a relative path, a missing folder, a folder.
	for _, args := range []map[string]any{
		{"what": "methods", "path": target},
		{"what": "methods", "path": "methods.md"},
		{"what": "methods", "path": filepath.Join(dir, "missing", "methods.md")},
		{"what": "methods", "path": dir},
	} {
		if code, _ := call(args); code != http.StatusBadRequest {
			t.Fatalf("%v should be refused, got %d", args, code)
		}
	}
	if code, result := call(map[string]any{"what": "methods", "path": target, "overwrite": true}); code != http.StatusOK || !result.OK {
		t.Fatalf("overwrite refused: %d %+v", code, result)
	}
	// No temporary file is left behind.
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("the folder holds %d entries", len(entries))
	}
	body, _ := json.Marshal(map[string]any{"action": "export", "args": map[string]any{"what": "methods", "path": filepath.Join(dir, "other.md")}})
	if rec := post(mux, "/api/remote/action", string(body), nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("an export without the token: %d", rec.Code)
	}
}

func TestAnUploadSlotTakesOneUpload(t *testing.T) {
	hub := newRemoteHub()
	mux := hubMux(hub)
	args, _ := json.Marshal(map[string]any{"path": filepath.Join(t.TempDir(), "project.json")})
	output, release, err := hub.prepareOutput(args)
	if err != nil {
		t.Fatal(err)
	}
	if rec := post(mux, "/"+output, "{}", nil); rec.Code != http.StatusOK {
		t.Fatalf("first upload: %d %s", rec.Code, rec.Body)
	}
	if rec := post(mux, "/"+output, "again", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("a second upload should be refused, got %d", rec.Code)
	}
	release()
	if rec := post(mux, "/api/remote/output/unknown", "x", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown slot: %d", rec.Code)
	}
}

func TestTheConnectionFileIsPrivateAndRemovedOnStop(t *testing.T) {
	dir := t.TempDir()
	var banner bytes.Buffer
	running, err := start(config{host: "127.0.0.1", port: 0, dataDir: dir, remote: true, window: "none"}, &banner)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, connectionFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("no connection file: %v\n%s", err, banner.String())
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("connection file mode %v, want 0600", info.Mode().Perm())
	}
	var c connectionInfo
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &c); err != nil || c.URL != running.url || c.Token != running.app.control.token || c.Version != version || c.PID != os.Getpid() {
		t.Fatalf("connection file %s does not describe the server at %s", data, running.url)
	}
	for _, want := range []string{path, running.app.control.token, "/api/remote/action"} {
		if !strings.Contains(banner.String(), want) {
			t.Fatalf("the banner does not give %s:\n%s", want, banner.String())
		}
	}
	running.stop(time.Second)
	running.listener.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the connection file is still there after stopping")
	}
}

func TestAConnectionFileOfAnotherServerIsKept(t *testing.T) {
	dir := t.TempDir()
	path, err := writeConnectionFile(dir, "http://127.0.0.1:1", "theirs")
	if err != nil {
		t.Fatal(err)
	}
	removeConnectionFile(path, "ours")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("another server's file was removed")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("the folder holds %d entries, want only remote.json", len(entries))
	}
}

func TestWithoutTheFlagThereIsNoRemoteControl(t *testing.T) {
	_, handler := testApp(t)
	if rec := request(t, handler, http.MethodGet, "/api/remote/tools", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("tools without --remote-control: %d", rec.Code)
	}
	rec := request(t, handler, http.MethodGet, "/api/info", nil, nil)
	if strings.Contains(rec.Body.String(), `"remoteControl":true`) {
		t.Fatalf("info: %s", rec.Body)
	}
}

func TestScriptsListTheActions(t *testing.T) {
	hub := newRemoteHub()
	req := httptest.NewRequest(http.MethodGet, "/api/remote/tools", nil)
	req.Host = "127.0.0.1:8840"
	req.RemoteAddr = "127.0.0.1:50000"
	rec := httptest.NewRecorder()
	hubMux(hub).ServeHTTP(rec, req)
	var body struct {
		Version string         `json:"version"`
		API     int            `json:"api"`
		Tools   []remoteAction `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK || body.Version != version || body.API != apiVersion || len(body.Tools) != len(remoteActions) {
		t.Fatalf("tools: %d %s", rec.Code, rec.Body)
	}
	for _, tool := range body.Tools {
		if tool.Description == "" || tool.InputSchema["type"] != "object" {
			t.Fatalf("%s: no description or argument schema", tool.Name)
		}
	}
}

// The page's action table (web/ui/remote.js) and the hub's list name the same actions.
func TestEveryActionHasAPageHandler(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("web", "ui", "remote.js"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "const actions = {")
	end := strings.Index(text[start:], "\n  };\n")
	if start < 0 || end < 0 {
		t.Fatal("web/ui/remote.js has no \"const actions = {\" block ending in \"  };\"")
	}
	var page []string
	for _, m := range regexp.MustCompile(`(?m)^    async (\w+)\(`).FindAllStringSubmatch(text[start:start+end], -1) {
		page = append(page, m[1])
	}
	hub := actionNames()
	sort.Strings(page)
	sort.Strings(hub)
	if strings.Join(page, ",") != strings.Join(hub, ",") {
		t.Fatalf("the page performs %v; the hub lists %v", page, hub)
	}
}

// A web page on another site cannot post actions, even from a browser on this computer.
func TestOtherWebPagesCannotSendActions(t *testing.T) {
	a, err := newApp(config{dataDir: t.TempDir(), remote: true})
	if err != nil {
		t.Fatal(err)
	}
	inner, err := a.handler()
	if err != nil {
		t.Fatal(err)
	}
	handler := protect(inner, "127.0.0.1", defaultPort)
	stop := fakePage(t, a.control, func(remoteEvent) remoteResult { return remoteResult{OK: true} })
	defer stop()
	for _, headers := range []map[string]string{
		{"Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "cross-site"},
		{"Origin": "http://127.0.0.1:9999"},
	} {
		if rec := post(handler, "/api/remote/action", `{"action":"get_state"}`, headers); rec.Code != http.StatusForbidden {
			t.Fatalf("an action with %v: %d", headers, rec.Code)
		}
	}
	// The page itself (same origin), and scripts (no Origin), may.
	if rec := post(handler, "/api/remote/action", `{"action":"get_state"}`, map[string]string{"Origin": "http://127.0.0.1:8840", "Sec-Fetch-Site": "same-origin"}); rec.Code != http.StatusOK {
		t.Fatalf("same origin: %d %s", rec.Code, rec.Body)
	}
	if rec := post(handler, "/api/remote/action", `{"action":"get_state"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("a script: %d %s", rec.Code, rec.Body)
	}
	rec := request(t, handler, http.MethodGet, "/api/info", nil, nil)
	if !strings.Contains(rec.Body.String(), `"remoteControl":true`) || !strings.Contains(rec.Body.String(), `"api":1`) {
		t.Fatalf("info: %s", rec.Body)
	}
}

// docs/REMOTE-CONTROL.md describes every action, and no action that does not exist.
func TestEveryActionIsDocumented(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("docs", "REMOTE-CONTROL.md"))
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for _, m := range regexp.MustCompile("(?m)^\\| `(\\w+)` \\|").FindAllStringSubmatch(string(doc), -1) {
		documented[m[1]] = true
	}
	for _, name := range actionNames() {
		if !documented[name] {
			t.Errorf("docs/REMOTE-CONTROL.md does not describe %s", name)
		}
		delete(documented, name)
	}
	for name := range documented {
		t.Errorf("docs/REMOTE-CONTROL.md describes %s, which is not an action", name)
	}
}
