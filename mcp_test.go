package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The MCP server (mcp.go): JSON-RPC on stdio, its tools the remote actions it was given leave
// for, each call performed by the page under the agent's name. After MaveScape 0.8.0's
// mcp_test.go.

const allLeave = leaveEdits | leaveCompute | leaveFiles

func runLines(t *testing.T, server *mcpServer, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	server.out = &out
	if err := server.serveStream(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n")); err != nil {
		t.Fatal(err)
	}
	var replies []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var reply map[string]any
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatalf("bad reply %q: %v", line, err)
		}
		replies = append(replies, reply)
	}
	return replies
}

func byID(replies []map[string]any) map[float64]map[string]any {
	out := map[float64]map[string]any{}
	for _, reply := range replies {
		if id, ok := reply["id"].(float64); ok {
			out[id] = reply
		}
	}
	return out
}

func textOf(t *testing.T, result map[string]any) string {
	t.Helper()
	return result["content"].([]any)[0].(map[string]any)["text"].(string)
}

func jsonString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

func listedTools(t *testing.T, server *mcpServer) []string {
	t.Helper()
	replies := runLines(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	var names []string
	for _, raw := range replies[0]["result"].(map[string]any)["tools"].([]any) {
		tool := raw.(map[string]any)
		if tool["title"] == "" || tool["inputSchema"] == nil || tool["description"] == "" {
			t.Fatalf("tool %v", tool)
		}
		if _, ok := tool["annotations"].(map[string]any)["readOnlyHint"].(bool); !ok {
			t.Fatalf("tool %s has no readOnlyHint", tool["name"])
		}
		names = append(names, tool["name"].(string))
	}
	return names
}

func TestMCPInitializeAndListTools(t *testing.T) {
	server := &mcpServer{hub: newRemoteHub(), pageWait: 50 * time.Millisecond}
	replies := runLines(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":3,"method":"nope"}`,
		`not json`,
		`{"jsonrpc":"2.0","id":4,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"ping"}`,
		`[{"jsonrpc":"2.0","id":6,"method":"ping"}]`,
	)
	if len(replies) != 6 {
		t.Fatalf("got %d replies: %v", len(replies), replies)
	}
	init := replies[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" || init["serverInfo"].(map[string]any)["name"] != "situcraft" || !strings.Contains(init["instructions"].(string), "get_state") {
		t.Fatalf("initialize: %v", init)
	}
	if replies[1]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatalf("unknown method: %v", replies[1])
	}
	if replies[2]["error"].(map[string]any)["code"].(float64) != -32700 {
		t.Fatalf("parse error: %v", replies[2])
	}
	// A protocol version not offered: the latest offered.
	if replies[3]["result"].(map[string]any)["protocolVersion"] != mcpProtocolVersions[0] {
		t.Fatalf("version: %v", replies[3])
	}
	if len(replies[4]["result"].(map[string]any)) != 0 {
		t.Fatalf("ping: %v", replies[4])
	}
	if replies[5]["error"].(map[string]any)["code"].(float64) != -32600 {
		t.Fatalf("a batch: %v", replies[5])
	}

	// Read-only by default: the tools that read and show, in the actions' order; each leave adds
	// its tools, and all of them every action.
	read := listedTools(t, server)
	for _, name := range read {
		if a, _ := findAction(name); a.hint.leave != 0 {
			t.Errorf("read-only server offers %s (%s)", name, leaveText(a.hint.leave))
		}
	}
	if len(read) < 20 || read[0] != "get_state" {
		t.Fatalf("read-only tools: %v", read)
	}
	server.allowed = leaveCompute
	compute := listedTools(t, server)
	if !contains(compute, "cluster_cells") || contains(compute, "draw_region") || contains(compute, "export") || contains(compute, "reference_labels") {
		t.Fatalf("with compute: %v", compute)
	}
	server.allowed = allLeave
	if all := listedTools(t, server); len(all) != len(remoteActions) {
		t.Fatalf("with all leave: %d tools, %d actions", len(all), len(remoteActions))
	}
}

func contains(list []string, name string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
}

// Every action has a leave (actionHints), files when it reads or writes this computer's files;
// none reads only and destroys.
func TestEveryActionHasALeave(t *testing.T) {
	seen := map[string]bool{}
	for _, action := range remoteActions {
		hint, ok := actionHints[action.Name]
		if !ok {
			t.Errorf("action %s has no leave in actionHints", action.Name)
			continue
		}
		if (action.output || action.reads || action.readsAt || action.writes) && hint.leave&leaveFiles == 0 {
			t.Errorf("action %s reads or writes files but does not need the files leave", action.Name)
		}
		if hint.leave == 0 && hint.destructive {
			t.Errorf("action %s reads only but is marked destructive", action.Name)
		}
		for _, name := range hint.files {
			if _, ok := action.InputSchema["properties"].(map[string]any)[name]; !ok {
				t.Errorf("action %s: its file argument %s is not one of its arguments", action.Name, name)
			}
		}
		for name := range hint.flags {
			if _, ok := action.InputSchema["properties"].(map[string]any)[name]; !ok {
				t.Errorf("action %s: its flag %s is not one of its arguments", action.Name, name)
			}
		}
		if action.Annotations["readOnlyHint"] != (hint.leave == 0 && len(hint.flags) == 0) {
			t.Errorf("action %s: annotations %v", action.Name, action.Annotations)
		}
		seen[action.Name] = true
	}
	var stale []string
	for name := range actionHints {
		if !seen[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("actionHints names actions that do not exist: %v", stale)
	}
}

// A tool needing leave the server was not given is refused before the page sees it, with the leave
// it needs; arguments can ask for more (a path to read, a download).
func TestMCPRefusesWhatItWasNotGivenLeaveFor(t *testing.T) {
	hub := newRemoteHub()
	var seen []string
	stop := fakePage(t, hub, func(event remoteEvent) remoteResult {
		seen = append(seen, event.Action)
		return remoteResult{OK: true, Message: "done"}
	})
	defer stop()
	server := &mcpServer{hub: hub, pageWait: time.Second}
	call := func(name, args string) map[string]any {
		t.Helper()
		replies := runLines(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`)
		return replies[0]["result"].(map[string]any)
	}
	refused := func(name, args, leave string) {
		t.Helper()
		r := call(name, args)
		if r["isError"] != true || !strings.Contains(textOf(t, r), "--allow "+leave) {
			t.Fatalf("%s %s: %v", name, args, r)
		}
	}
	refused("cluster_cells", `{}`, "compute")
	refused("jobs", `{"job":"j1","do":"discard"}`, "compute")
	refused("draw_region", `{"polygon":[[0,0],[1,0],[1,1]]}`, "edits")
	refused("export", `{"what":"project","path":"/tmp/x.json"}`, "files")
	server.allowed = leaveCompute
	refused("reference_labels", `{"path":"/tmp/ref.zarr"}`, "files")
	refused("import_labels", `{"path":"/tmp/labels.csv"}`, "files")
	refused("cohort_steps", `{"reference":"/tmp/ref.zarr"}`, "files")
	refused("open_example", `{"example":"lung","download":true}`, "files")
	refused("open_paths", `{"paths":["/tmp"]}`, "files")
	server.allowed = leaveFiles
	refused("open_paths", `{"paths":["/tmp"],"add":true}`, "compute")
	if len(seen) != 0 {
		t.Fatalf("the page saw refused calls: %v", seen)
	}
	// What the leave given covers reaches the page.
	server.allowed = leaveCompute
	for _, c := range [][2]string{{"jobs", `{"job":"j1","do":"cancel"}`}, {"cluster_cells", `{}`}, {"import_labels", `{"instrument":"graph-based"}`}, {"open_example", `{"example":"lung"}`}, {"summarize_samples", `{"reference":"` + strings.Repeat("ab", 32) + `"}`}, {"get_state", `{}`}} {
		if r := call(c[0], c[1]); r["isError"] == true {
			t.Fatalf("%s %s refused: %v", c[0], c[1], r)
		}
	}
	if strings.Join(seen, ",") != "jobs,cluster_cells,import_labels,open_example,summarize_samples,get_state" {
		t.Fatalf("the page saw %v", seen)
	}
}

// A script names a file on this computer only with the token, as it opens or writes one.
func TestScriptsNeedTheTokenForFilesTheirArgumentsName(t *testing.T) {
	hub := newRemoteHub()
	stop := fakePage(t, hub, func(event remoteEvent) remoteResult { return remoteResult{OK: true, Message: event.Action} })
	defer stop()
	mux := hubMux(hub)
	for _, c := range []struct {
		body  string
		token bool
	}{
		{`{"action":"import_labels","args":{"path":"/tmp/labels.csv"}}`, true},
		{`{"action":"reference_labels","args":{"path":"/tmp/ref.zarr"}}`, true},
		{`{"action":"summarize_samples","args":{"reference":"/tmp/ref.zarr"}}`, true},
		{`{"action":"import_labels","args":{"instrument":"graph-based"}}`, false},
		{`{"action":"summarize_samples","args":{"reference":"` + strings.Repeat("0f", 32) + `"}}`, false},
		{`{"action":"cluster_cells","args":{}}`, false},
	} {
		rec := post(mux, "/api/remote/action", c.body, nil)
		if needs := rec.Code == http.StatusUnauthorized; needs != c.token {
			t.Errorf("%s without the token: %d %s", c.body, rec.Code, rec.Body)
		}
		if rec := post(mux, "/api/remote/action", c.body, map[string]string{remoteTokenHeader: hub.token}); rec.Code != http.StatusOK {
			t.Errorf("%s with the token: %d %s", c.body, rec.Code, rec.Body)
		}
	}
}

func TestMCPToolCallsReachThePageUnderTheAgentsName(t *testing.T) {
	hub := newRemoteHub()
	var seen remoteEvent
	stop := fakePage(t, hub, func(event remoteEvent) remoteResult {
		seen = event
		return remoteResult{OK: true, Message: "Lung: 1 dataset", Data: json.RawMessage(`{"datasets":[{"name":"xenium-lung-2fov"}]}`)}
	})
	defer stop()
	server := &mcpServer{hub: hub, pageWait: time.Second}
	replies := runLines(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"segmentations","arguments":{"dataset":"xenium-lung-2fov"}}}`)
	result := replies[0]["result"].(map[string]any)
	// Restricted to its leave, none given: the page computes nothing on the way.
	if seen.Action != "segmentations" || seen.Client != "an AI agent" || !seen.Restricted || len(seen.Leave) != 0 {
		t.Fatalf("page saw %+v", seen)
	}
	var args map[string]any
	json.Unmarshal(seen.Args, &args)
	if args["dataset"] != "xenium-lung-2fov" {
		t.Fatalf("arguments not passed: %s", seen.Args)
	}
	text := textOf(t, result)
	if !strings.Contains(text, "1 dataset") || !strings.Contains(text, `"name":"xenium-lung-2fov"`) {
		t.Fatalf("text %q", text)
	}
	if result["structuredContent"].(map[string]any)["message"] != "Lung: 1 dataset" {
		t.Fatalf("structured %v", result["structuredContent"])
	}
	// The agent's name, as its client gives it (printable, its title before its name).
	runLines(t, server,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"claude-code","title":"Claude Code\u0007"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_state","arguments":{}}}`,
	)
	if seen.Client != "Claude Code" {
		t.Fatalf("client %q", seen.Client)
	}
	runLines(t, server,
		`{"jsonrpc":"2.0","id":4,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"codex"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_state"}}`,
	)
	if seen.Client != "codex" {
		t.Fatalf("client %q", seen.Client)
	}
}

func TestMCPRequiredArgumentsMissingPageAndUnknownTools(t *testing.T) {
	server := &mcpServer{hub: newRemoteHub(), pageWait: 20 * time.Millisecond, url: "http://127.0.0.1:8840", allowed: allLeave}
	server.hub.log = newCallLog(t.TempDir(), "")
	replies := byID(runLines(t, server,
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"claude-code","title":"Claude Code"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"open_project","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_state","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"unknown_tool"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_state","arguments":[1,2]}}`,
	))
	first := replies[1]["result"].(map[string]any)
	if first["isError"] != true || textOf(t, first) != "project is required." {
		t.Fatalf("missing argument: %v", first)
	}
	second := replies[2]["result"].(map[string]any)
	if second["isError"] != true || !strings.Contains(textOf(t, second), "No SituCraft page is connected. Open http://127.0.0.1:8840") {
		t.Fatalf("no page: %v", second)
	}
	if replies[3]["error"] == nil || !strings.Contains(replies[3]["error"].(map[string]any)["message"].(string), "unknown_tool") {
		t.Fatalf("unknown tool: %v", replies[3])
	}
	if fourth := replies[4]["result"].(map[string]any); fourth["isError"] != true || !strings.Contains(textOf(t, fourth), "not a JSON object") {
		t.Fatalf("arguments not an object: %v", replies[4])
	}
	// Each call logged, under the agent's name with its leave: refused, or failed for want of a page.
	var got []string
	for _, c := range readCalls(t, server.hub.log) {
		if c.By != "Claude Code" || c.Via != viaAgent || fmt.Sprint(c.Leave) != "[edits compute files]" {
			t.Errorf("logged as %s, %s, %v", c.By, c.Via, c.Leave)
		}
		got = append(got, c.Tool+" "+c.Outcome)
	}
	sort.Strings(got) // the calls run at once
	if strings.Join(got, ", ") != "get_state failed, get_state refused, open_project refused, unknown_tool refused" {
		t.Errorf("the log: %s", strings.Join(got, ", "))
	}
}

// What an action changed in the project reaches the agent, as a script's answer carries it.
func TestMCPResultsSayWhatTheyChanged(t *testing.T) {
	hub := newRemoteHub()
	stop := fakePage(t, hub, func(remoteEvent) remoteResult {
		return remoteResult{OK: true, Message: "Saved Good cells.", Changed: []callChange{{ID: "f-1", Kind: "filter", Name: "Good cells", Change: "added"}}}
	})
	defer stop()
	server := &mcpServer{hub: hub, pageWait: time.Second, allowed: leaveEdits}
	replies := runLines(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"save_filter","arguments":{"name":"Good cells","where":{"attribute":"transcripts","min":20}}}}`)
	result := replies[0]["result"].(map[string]any)
	raw, _ := json.Marshal(result["structuredContent"].(map[string]any)["changed"])
	var changed []callChange
	json.Unmarshal(raw, &changed)
	if len(changed) != 1 || changed[0] != (callChange{ID: "f-1", Kind: "filter", Name: "Good cells", Change: "added"}) || !strings.Contains(textOf(t, result), `Changed in the project: [{"id":"f-1"`) {
		t.Fatalf("%v", result)
	}
}

func TestMCPPageRefusalsBecomeToolErrors(t *testing.T) {
	hub := newRemoteHub()
	stop := fakePage(t, hub, func(remoteEvent) remoteResult {
		return remoteResult{OK: false, Message: `No dataset "liver" in the project.`}
	})
	defer stop()
	server := &mcpServer{hub: hub, pageWait: time.Second}
	replies := runLines(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"segmentations","arguments":{"dataset":"liver"}}}`)
	result := replies[0]["result"].(map[string]any)
	if result["isError"] != true || textOf(t, result) != `No dataset "liver" in the project.` {
		t.Fatalf("%v", result)
	}
}

// Pictures: each data URL in a result's data becomes an image after the text, captioned, the data
// naming it by its number with its fields in the page's order; past the limit, null and said.
func TestMCPPicturesBecomeImageContent(t *testing.T) {
	hub := newRemoteHub()
	png := "iVBORw0KGgo="
	stop := fakePage(t, hub, func(remoteEvent) remoteResult {
		return remoteResult{OK: true, Message: "The tissue.", Data: json.RawMessage(`{"dataset":"xenium-lung-2fov","caption":"The Lung's tissue","image":"data:image/png;base64,` + png + `","pages":[{"name":"Figure 1","image":"data:image/png;base64,AAAA"}]}`)}
	})
	defer stop()
	server := &mcpServer{hub: hub, pageWait: time.Second}
	replies := runLines(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"show_tissue","arguments":{"image":true}}}`)
	result := replies[0]["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) != 5 {
		t.Fatalf("content: %v", content)
	}
	if text := content[0].(map[string]any)["text"].(string); text != `The tissue.`+"\n\n"+`{"dataset":"xenium-lung-2fov","caption":"The Lung's tissue","image":1,"pages":[{"name":"Figure 1","image":2}]}` {
		t.Fatalf("text: %s", text)
	}
	first, second := content[2].(map[string]any), content[4].(map[string]any)
	if content[1].(map[string]any)["text"] != "Image 1: The Lung's tissue" || first["type"] != "image" || first["data"] != png || first["mimeType"] != "image/png" || content[3].(map[string]any)["text"] != "Image 2: Figure 1" || second["data"] != "AAAA" {
		t.Fatalf("images: %v", content[1:])
	}
	big := strings.Repeat("A", mcpImageLimit/2+4)
	out, images, dropped := extractImages(json.RawMessage(`{"a":"data:image/png;base64,` + big + `","b":"data:image/png;base64,` + big + `"}`))
	if len(images) != 1 || dropped != 1 || string(out) != `{"a":1,"b":null}` {
		t.Fatalf("limit: %d images, %d dropped, %.40s", len(images), dropped, out)
	}
}

// An agent given the files leave writes and reads files by path, as a script with the token does:
// an export gets an upload slot for its path, checked first; bundles to open are registered for the
// page.
func TestMCPFilesByPath(t *testing.T) {
	hub := newRemoteHub()
	dir := t.TempDir()
	bundle := filepath.Join(dir, "bundle")
	os.Mkdir(bundle, 0o755)
	hub.open = func(paths []string) ([]localFile, []string) {
		var files []localFile
		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				files = append(files, localFile{Name: filepath.Base(path)})
			}
		}
		if len(files) == 0 {
			return nil, []string{"no such bundle"}
		}
		return files, nil
	}
	var seen []remoteEvent
	stop := fakePage(t, hub, func(event remoteEvent) remoteResult {
		seen = append(seen, event)
		return remoteResult{OK: true, Message: "done"}
	})
	defer stop()
	server := &mcpServer{hub: hub, pageWait: time.Second, allowed: leaveFiles}
	target := filepath.Join(dir, "project.json")
	replies := byID(runLines(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"export","arguments":{"what":"project","path":`+jsonString(target)+`}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"export","arguments":{"what":"project","path":"relative/project.json"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"open_paths","arguments":{"paths":[`+jsonString(bundle)+`]}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"open_paths","arguments":{"paths":[`+jsonString(filepath.Join(dir, "missing"))+`]}}}`,
	))
	// The calls run at once; the page sees them in either order.
	events := map[string]remoteEvent{}
	for _, event := range seen {
		events[event.Action] = event
	}
	if len(seen) != 2 || events["export"].Output == "" || len(events["open_paths"].Files) != 1 || events["open_paths"].Files[0].Name != "bundle" {
		t.Fatalf("the page saw %+v", seen)
	}
	if r := replies[2]["result"].(map[string]any); r["isError"] != true || !strings.Contains(textOf(t, r), "absolute") {
		t.Fatalf("relative path: %v", r)
	}
	if r := replies[4]["result"].(map[string]any); r["isError"] != true || !strings.Contains(textOf(t, r), "no such bundle") {
		t.Fatalf("missing bundle: %v", r)
	}
}

// A call the agent cancels gets no reply; one with a progress token hears from the server while it
// runs, and never after its reply.
func TestMCPCancellationAndProgress(t *testing.T) {
	hub := newRemoteHub()
	release := make(chan struct{})
	stop := fakePage(t, hub, func(event remoteEvent) remoteResult {
		if event.Action == "jobs" {
			<-release
		} else {
			time.Sleep(120 * time.Millisecond)
		}
		return remoteResult{OK: true, Message: event.Action}
	})
	defer stop()
	server := &mcpServer{hub: hub, pageWait: time.Second, progressTick: 20 * time.Millisecond}
	reader, writer := io.Pipe()
	var out bytes.Buffer
	server.out = &out
	done := make(chan error, 1)
	go func() { done <- server.serveStream(context.Background(), reader) }()
	writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"jobs","arguments":{}}}` + "\n"))
	time.Sleep(50 * time.Millisecond)
	writer.Write([]byte(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1,"reason":"no longer needed"}}` + "\n"))
	time.Sleep(50 * time.Millisecond)
	// The page finishes what it was doing, as the window does; its answer goes nowhere.
	close(release)
	writer.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_state","arguments":{},"_meta":{"progressToken":"p2"}}}` + "\n"))
	time.Sleep(400 * time.Millisecond)
	writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, `"id":1`) {
		t.Fatalf("a cancelled call was answered: %s", text)
	}
	if !strings.Contains(text, `"id":2`) || !strings.Contains(text, `"method":"notifications/progress"`) || !strings.Contains(text, `"progressToken":"p2"`) {
		t.Fatalf("no answer or no progress: %s", text)
	}
	if strings.LastIndex(text, `"progressToken":"p2"`) > strings.Index(text, `"id":2`) {
		t.Fatalf("progress after the reply: %s", text)
	}
}

// The real command: the protocol alone on stdout, the banner on stderr, no script route and no
// remote.json; it stops when stdin ends. --allow is checked.
func TestMCPCommandKeepsStdoutForTheProtocol(t *testing.T) {
	dir := t.TempDir()
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var stdout, stderr bytes.Buffer
	code := runMCP([]string{"--window", "none", "--port", "0", "--data-dir", dir, "--cache-dir", filepath.Join(dir, "cache"), "--allow", "compute"}, stdin, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("stdout has %d lines: %q", len(lines), stdout.String())
	}
	for _, line := range lines {
		var message map[string]any
		if err := json.Unmarshal([]byte(line), &message); err != nil || message["jsonrpc"] != "2.0" {
			t.Fatalf("not JSON-RPC on stdout: %q", line)
		}
	}
	if !strings.Contains(lines[1], `"cluster_cells"`) || strings.Contains(lines[1], `"draw_region"`) {
		t.Fatalf("--allow compute: %s", lines[1][:200])
	}
	if banner := stderr.String(); !strings.Contains(banner, "SituCraft "+version+" is running at") || !strings.Contains(banner, "MCP: answering on stdin and stdout") || !strings.Contains(banner, "those that need compute") || strings.Contains(banner, "Remote control: POST") {
		t.Fatalf("banner: %s", banner)
	}
	if _, err := os.Stat(filepath.Join(dir, connectionFileName)); !os.IsNotExist(err) {
		t.Fatalf("remote.json written in MCP mode: %v", err)
	}
	if code := runMCP([]string{"--host", "0.0.0.0"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("a host other than this computer: exit %d", code)
	}
	if code := runMCP([]string{"--allow", "everything"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("--allow everything: exit %d", code)
	}
	opts, rest, err := parseMCPArgs([]string{"--allow=edits,files", "--port", "9000", "-allow", "compute"}, io.Discard)
	if err != nil || opts.allowed != allLeave || strings.Join(rest, " ") != "--port 9000" {
		t.Fatalf("parse: %v %v %v", opts, rest, err)
	}
}

// With a SituCraft already serving the library with remote control, the agent works in its
// window: the call goes there under the agent's name, with the token, and the leave is still
// checked here.
func TestMCPAttachesToTheSituCraftServingTheLibrary(t *testing.T) {
	dir := t.TempDir()
	cfg := config{host: "127.0.0.1", port: 0, window: "none", dataDir: dir, cacheDir: filepath.Join(dir, "cache"), remote: true}
	running, err := start(cfg, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	go running.serve()
	defer running.stop(time.Second)
	var seen []remoteEvent
	stop := fakePage(t, running.app.control, func(event remoteEvent) remoteResult {
		seen = append(seen, event)
		return remoteResult{OK: true, Message: "The project.", Data: json.RawMessage(`{"name":"Lung"}`)}
	})
	defer stop()
	stdin := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"claude-code","title":"Claude Code"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_state","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"cluster_cells","arguments":{}}}`,
	}, "\n") + "\n")
	var stdout, stderr bytes.Buffer
	if code := runMCP([]string{"--window", "none", "--data-dir", dir}, stdin, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "already running at "+running.url) {
		t.Fatalf("stderr: %s", stderr.String())
	}
	replies := map[float64]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		var reply map[string]any
		json.Unmarshal([]byte(line), &reply)
		replies[reply["id"].(float64)] = reply
	}
	if r := replies[2]["result"].(map[string]any); r["isError"] == true || !strings.Contains(textOf(t, r), `"name":"Lung"`) {
		t.Fatalf("get_state: %v", r)
	}
	if r := replies[3]["result"].(map[string]any); r["isError"] != true || !strings.Contains(textOf(t, r), "--allow compute") {
		t.Fatalf("cluster_cells: %v", r)
	}
	if len(seen) != 1 || seen[0].Action != "get_state" || seen[0].Client != "Claude Code" || !seen[0].Restricted {
		t.Fatalf("the page saw %+v", seen)
	}
	// The library's log holds each call once: the one sent, as the SituCraft attached to logged it,
	// and the one refused here.
	var got []string
	for _, c := range readCalls(t, newCallLog(dir, "")) {
		got = append(got, fmt.Sprintf("%s %s %s %s %v", c.Tool, c.Outcome, c.Via, c.By, c.Leave))
	}
	sort.Strings(got)
	if strings.Join(got, "; ") != "cluster_cells refused agent Claude Code []; get_state done agent Claude Code []" {
		t.Errorf("the log: %s", strings.Join(got, "; "))
	}
}

// docs/MCP.md names every tool, so its list stays the list.
func TestTheMCPGuideNamesEveryTool(t *testing.T) {
	guide, err := os.ReadFile("docs/MCP.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range remoteActions {
		if !strings.Contains(string(guide), "`"+action.Name+"`") {
			t.Errorf("docs/MCP.md does not name the tool %s", action.Name)
		}
	}
}
