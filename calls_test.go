package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// readCalls are the library's calls, newest first.
func readCalls(t *testing.T, l *callLog) []callEntry {
	t.Helper()
	read, err := l.read(callQuery{}, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return read.Calls
}

// Every script's call is a line of the log: done, failed (no page; the page or the program saying
// why) or refused (no token, a request not read, an action no one knows), with its caller, what it
// changed and the ids it answered with; an agent's call through the hub with its leave; each line
// told to the pages at once.
func TestScriptsCallsAreLogged(t *testing.T) {
	hub := newRemoteHub()
	hub.log = newCallLog(t.TempDir(), "s-1")
	hub.log.logged = hub.logged
	hub.program = func(ctx context.Context, action string, args json.RawMessage) (remoteResult, bool) {
		return remoteResult{OK: true, Message: "A result.", Data: json.RawMessage(`{"id":"lab-1a2b3c4d","name":"non-negative","parents":[{"id":"seg-9f8e7d6c"},{"id":"lab-1a2b3c4d"}],"gene":"HLA-DRA","cell":"bomepkkn-1"}`)}, true
	}
	mux := hubMux(hub)
	token := map[string]string{remoteTokenHeader: hub.token}

	post(mux, "/api/remote/action", `{"action":"get_state"}`, nil) // no page
	// A page watching the log, and the window (opened last, it takes the actions).
	watcher := hub.connect()
	defer hub.disconnect(watcher)
	stop := fakePage(t, hub, func(e remoteEvent) remoteResult {
		switch e.Action {
		case "save_filter":
			return remoteResult{OK: true, Message: "Saved.", Data: json.RawMessage(`{"id":"f-0a1b2c3d4e5f6a7b"}`), Changed: []callChange{{ID: "f-0a1b2c3d4e5f6a7b", Kind: "filter", Name: "Nest", Change: "added"}}}
		case "focus":
			return remoteResult{OK: false, Message: `No dataset "liver" in the project.`}
		}
		return remoteResult{OK: false, Message: `Unknown action "` + e.Action + `".`}
	})
	defer stop()
	long := strings.Repeat("x", 2000)
	outline := make([]string, 300)
	for i := range outline {
		outline[i] = fmt.Sprintf("[%d,%d]", i, i)
	}
	post(mux, "/api/remote/action", `{"action":"save_filter","client":"notebook","args":{"name":"Nest","note":"`+long+`","where":{"region":"Nest"},"outline":[`+strings.Join(outline, ",")+`]}}`, nil)
	post(mux, "/api/remote/action", `{"action":"focus","client":"notebook","args":{"dataset":"liver"}}`, nil)
	post(mux, "/api/remote/action", `{"action":"get_result","client":"Claude Code","leave":["edits"],"args":{"id":"lab-1a2b3c4d"}}`, nil)
	post(mux, "/api/remote/action", `{"action":"export","client":"notebook","args":{"path":"/tmp/x.csv"}}`, nil)
	post(mux, "/api/remote/action", `{"action":"export","client":"notebook","args":{"path":"relative.csv"}}`, token)
	post(mux, "/api/remote/action", `{"action":"fly"}`, nil)
	post(mux, "/api/remote/action", `not json`, nil)

	calls := readCalls(t, hub.log)
	var got []string
	for i := len(calls) - 1; i >= 0; i-- {
		c := calls[i]
		got = append(got, c.Tool+" "+c.Outcome+" "+c.Via+" "+c.By)
		if c.Session != "s-1" || c.Time == "" || c.Ms < 0 {
			t.Errorf("%s: %+v", c.Tool, c)
		}
	}
	want := []string{
		"get_state failed script a program on this computer",
		"save_filter done script notebook",
		"focus failed script notebook",
		"get_result done agent Claude Code",
		"export refused script notebook",
		"export refused script notebook",
		"fly refused script a program on this computer",
		"(none) refused script a program on this computer",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the log:\n%s", strings.Join(got, "\n"))
	}
	byTool := func(tool string, outcome string) callEntry {
		for _, c := range calls {
			if c.Tool == tool && c.Outcome == outcome {
				return c
			}
		}
		t.Fatalf("no %s %s", tool, outcome)
		return callEntry{}
	}
	if c := byTool("get_state", "failed"); !strings.Contains(c.Message, "No SituCraft page") {
		t.Errorf("no page: %+v", c)
	}
	saved := byTool("save_filter", "done")
	var args map[string]any
	json.Unmarshal(saved.Args, &args)
	if note := args["note"].(string); !strings.HasSuffix(note, "… (2000 characters)") || len([]rune(note)) > callStringLimit+30 {
		t.Errorf("a long argument: %d characters", len(note))
	}
	if list := args["outline"].([]any); len(list) != callListLimit+1 || list[callListLimit] != "… (300 items)" {
		t.Errorf("a long list: %d items, the last %v", len(list), list[len(list)-1])
	}
	if fmt.Sprint(args["where"]) != "map[region:Nest]" || len(saved.Changed) != 1 || saved.Changed[0].ID != "f-0a1b2c3d4e5f6a7b" || saved.Changed[0].Change != "added" || fmt.Sprint(saved.IDs) != "[f-0a1b2c3d4e5f6a7b]" {
		t.Errorf("save_filter: %+v", saved)
	}
	if c := byTool("get_result", "done"); fmt.Sprint(c.IDs) != "[lab-1a2b3c4d seg-9f8e7d6c]" || fmt.Sprint(c.Leave) != "[edits]" {
		t.Errorf("the ids answered (not a word, a gene or a cell): %v, leave %v", c.IDs, c.Leave)
	}
	if c := byTool("fly", "refused"); !strings.Contains(c.Message, `Unknown action "fly"`) {
		t.Errorf("an unknown action: %+v", c)
	}
	// The page watching was told of each call made while it was connected.
	told := 0
	for len(watcher.notices) > 0 {
		var e callEntry
		json.Unmarshal(<-watcher.notices, &e)
		told++
	}
	if told != len(calls)-1 {
		t.Errorf("the page was told of %d calls, of %d", told, len(calls)-1)
	}
}

// The log read back: chosen by words, caller, tool and outcome, newest first and a page at a time
// across months; exported oldest first as JSON lines or CSV (no cell read as a formula).
func TestTheLogIsSearchedAndExported(t *testing.T) {
	l := newCallLog(t.TempDir(), "")
	clock := time.Date(2026, 9, 30, 23, 59, 0, 0, time.UTC)
	l.now = func() time.Time { return clock }
	add := func(by, tool, outcome, message string, args string) {
		c := l.start(by, viaScript, nil, tool, json.RawMessage(args))
		clock = clock.Add(90 * time.Second)
		result := remoteResult{OK: outcome == callDone, Message: message}
		l.end(c, outcome, result, nil)
	}
	add("notebook", "regions", callDone, "2 regions.", `{}`)
	add("Claude Code", "query_cells", callDone, "581 of 11898 cells kept, those in Tumor nest.", `{"where":{"region":"Tumor nest"}}`)
	add("Claude Code", "cluster_cells", callRefused, "=cmd|' needs compute", `{}`)
	add("notebook", "query_cells", callFailed, "no region Moat", `{"where":{"region":"Moat"}}`)
	read, err := l.read(callQuery{}, 0, 10)
	if err != nil || read.Total != 4 || read.Calls[0].Message != "no region Moat" || read.Calls[3].Tool != "regions" {
		t.Fatalf("all: %+v %v", read, err)
	}
	if fmt.Sprint(read.Tools) != "[cluster_cells query_cells regions]" || fmt.Sprint(read.Callers) != "[Claude Code notebook]" {
		t.Errorf("tools %v, callers %v", read.Tools, read.Callers)
	}
	if read.Calls[3].Time[:7] != "2026-09" || read.Calls[0].Time[:7] != "2026-10" {
		t.Errorf("two months: %s, %s", read.Calls[3].Time, read.Calls[0].Time)
	}
	for q, want := range map[callQuery]int{
		{Text: "tumor NEST"}:                           1,
		{Text: "query_cells"}:                          2,
		{Tool: "query_cells", By: "notebook"}:          1,
		{Outcome: callRefused}:                         1,
		{Text: "moat", Outcome: callDone}:              0,
		{By: "Claude Code", Text: "compute"}:           1,
		{Tool: "regions", By: "Claude Code", Text: ""}: 0,
	} {
		if read, _ := l.read(q, 0, 10); read.Total != want {
			t.Errorf("%+v: %d, want %d", q, read.Total, want)
		}
	}
	if page, _ := l.read(callQuery{}, 1, 2); page.Total != 4 || len(page.Calls) != 2 || page.Calls[0].Tool != "cluster_cells" {
		t.Errorf("a page: %+v", page)
	}

	a := &app{calls: l}
	mux := http.NewServeMux()
	a.registerCalls(mux)
	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}
	var served callsRead
	json.NewDecoder(get("/api/calls?by=Claude+Code&limit=1").Body).Decode(&served)
	if served.Total != 2 || len(served.Calls) != 1 || served.Calls[0].Tool != "cluster_cells" {
		t.Errorf("GET /api/calls: %+v", served)
	}
	rec := get("/api/calls/export?format=jsonl&tool=query_cells")
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if !strings.Contains(rec.Header().Get("Content-Disposition"), ".jsonl") || len(lines) != 2 || !strings.Contains(lines[0], "Tumor nest") {
		t.Errorf("JSON lines: %s", rec.Body)
	}
	rows, err := csv.NewReader(get("/api/calls/export?format=csv").Body).ReadAll()
	if err != nil || len(rows) != 5 || strings.Join(rows[0], ",") != "time,by,via,leave,tool,outcome,ms,message,args,changed,ids" || rows[1][4] != "regions" || rows[3][7] != "'=cmd|' needs compute" || rows[2][6] != "90000" {
		t.Errorf("CSV: %v %v", rows, err)
	}
	if rec := get("/api/calls/export?format=xml"); rec.Code != http.StatusBadRequest {
		t.Errorf("format xml: %d", rec.Code)
	}
}
