package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The library's log of the calls programs make (wave 9, slice 4; requirement A4): each call an
// agent (situcraft mcp) or a script (remote control) makes of SituCraft is a line of
// calls/YYYY-MM.jsonl in the library, refusals and failures too: when, by whom and how, the tool
// and its arguments, whether it was done, refused or failed and the sentence it answered, what it
// added, changed or removed in the project, the ids it answered with, and how long it took. The
// app shows and searches it (the drawer's Calls tab) and exports it; it never leaves this computer.

const (
	callsFolder      = "calls"
	callStringLimit  = 500  // an argument's text kept, in characters
	callListLimit    = 50   // an argument's list items kept
	callMessageLimit = 1000 // the sentence kept, in characters
	callIDLimit      = 100  // ids kept
	callFilesRead    = 24   // months read when searching
)

// callEntry is a line of the log.
type callEntry struct {
	Time    string          `json:"time"` // when it was asked (UTC, RFC 3339, milliseconds)
	By      string          `json:"by"`   // the caller's name
	Via     string          `json:"via"`  // agent or script
	Leave   []string        `json:"leave,omitempty"`
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args,omitempty"`
	Outcome string          `json:"outcome"` // done, refused or failed
	Message string          `json:"message"`
	Changed []callChange    `json:"changed,omitempty"`
	IDs     []string        `json:"ids,omitempty"`
	Ms      int64           `json:"ms"`
	Session string          `json:"session,omitempty"` // the run of SituCraft that took it
}

// callChange is a record of the project a call added, changed or removed, as the page saw it.
type callChange struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
	Change string `json:"change"` // added, changed or removed
}

const (
	callDone    = "done"
	callRefused = "refused"
	callFailed  = "failed"
	viaAgent    = "agent"
	viaScript   = "script"
)

// callLog appends to and reads the library's log. A nil log records nothing.
type callLog struct {
	dir     string
	session string
	mu      sync.Mutex
	now     func() time.Time
	// logged is told of each line written (the hub tells the pages, which show it at once).
	logged func(callEntry)
}

func newCallLog(dataDir, session string) *callLog {
	return &callLog{dir: filepath.Join(dataDir, callsFolder), session: session, now: time.Now}
}

// callStart is a call being made: when, by whom, the tool and its arguments.
type callStart struct {
	at    time.Time
	by    string
	via   string
	leave []string
	tool  string
	args  json.RawMessage
}

func (l *callLog) start(by, via string, leave []string, tool string, args json.RawMessage) *callStart {
	if l == nil {
		return nil
	}
	if tool == "" {
		tool = "(none)"
	}
	return &callStart{at: l.now(), by: by, via: via, leave: leave, tool: tool, args: args}
}

// end writes the call's line: its outcome, the page's or program's answer (or why there was none).
func (l *callLog) end(c *callStart, outcome string, result remoteResult, err error) {
	if l == nil || c == nil {
		return
	}
	message := result.Message
	if err != nil {
		message = err.Error()
	}
	if len([]rune(message)) > callMessageLimit {
		message = string([]rune(message)[:callMessageLimit]) + "…"
	}
	e := callEntry{
		Time:    c.at.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		By:      c.by,
		Via:     c.via,
		Leave:   c.leave,
		Tool:    c.tool,
		Args:    boundedArgs(c.args),
		Outcome: outcome,
		Message: message,
		Changed: result.Changed,
		IDs:     resultIDs(result.Data),
		Ms:      l.now().Sub(c.at).Milliseconds(),
		Session: l.session,
	}
	if len(e.Changed) > callIDLimit {
		e.Changed = e.Changed[:callIDLimit]
	}
	if err := l.append(e); err != nil {
		fmt.Fprintf(os.Stderr, "the calls' log: %v\n", err)
		return
	}
	if l.logged != nil {
		l.logged(e)
	}
}

// append writes one line, whole (O_APPEND: an attached situcraft mcp writes the same file).
func (l *callLog) append(e callEntry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(l.dir, e.Time[:7]+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// boundedArgs are a call's arguments as logged: texts cut at callStringLimit characters and lists
// at callListLimit items (a region's outline, a picture), so that a line stays small.
func boundedArgs(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		text := string(raw)
		if !utf8.ValidString(text) {
			text = strings.ToValidUTF8(text, "?")
		}
		value = text
	}
	var bound func(v any, depth int) any
	bound = func(v any, depth int) any {
		switch v := v.(type) {
		case string:
			if runes := []rune(v); len(runes) > callStringLimit {
				return string(runes[:callStringLimit]) + fmt.Sprintf("… (%d characters)", len(runes))
			}
			return v
		case []any:
			if depth > 8 {
				return fmt.Sprintf("(a list of %d)", len(v))
			}
			out := make([]any, 0, min(len(v), callListLimit+1))
			for i, item := range v {
				if i == callListLimit {
					out = append(out, fmt.Sprintf("… (%d items)", len(v)))
					break
				}
				out = append(out, bound(item, depth+1))
			}
			return out
		case map[string]any:
			if depth > 8 {
				return "(an object)"
			}
			out := make(map[string]any, len(v))
			for k, item := range v {
				out[k] = bound(item, depth+1)
			}
			return out
		}
		return v
	}
	encoded, err := json.Marshal(bound(value, 0))
	if err != nil {
		return nil
	}
	return encoded
}

// callIDPattern is an id SituCraft gives a record: a short prefix, a hyphen and a hexadecimal or
// base-36 tail with a digit in it (seg-…, lab-…, f-…, r-…, p-…), a version after it (cn-…-2).
var callIDPattern = regexp.MustCompile(`^[a-z]{1,4}-[0-9a-z]{6,40}(-[0-9]+)?$`)

// resultIDs are the ids an answer's data holds, in the order they come, each once.
func resultIDs(data json.RawMessage) []string {
	if len(data) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		if len(out) >= callIDLimit {
			return
		}
		switch v := v.(type) {
		case string:
			if !seen[v] && len(v) <= 48 && callIDPattern.MatchString(v) && strings.ContainsAny(v[strings.IndexByte(v, '-'):], "0123456789") {
				seen[v] = true
				out = append(out, v)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		case map[string]any:
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(v[k])
			}
		}
	}
	walk(value)
	return out
}

// --- Reading -----------------------------------------------------------------------------------------

// callQuery chooses calls: words anywhere in the line, a tool, a caller, an outcome.
type callQuery struct {
	Text, Tool, By, Outcome string
}

func (q callQuery) matches(e callEntry, line []byte) bool {
	if q.Tool != "" && e.Tool != q.Tool || q.By != "" && e.By != q.By || q.Outcome != "" && e.Outcome != q.Outcome {
		return false
	}
	if q.Text == "" {
		return true
	}
	lower := bytes.ToLower(line)
	for _, word := range strings.Fields(strings.ToLower(q.Text)) {
		if !bytes.Contains(lower, []byte(word)) {
			return false
		}
	}
	return true
}

// callsRead are the calls chosen, newest first, and the tools and callers of all the calls read.
type callsRead struct {
	Calls   []callEntry `json:"calls"`
	Total   int         `json:"total"`
	Tools   []string    `json:"tools"`
	Callers []string    `json:"callers"`
}

// read answers the calls q chooses, newest first, from offset (at most limit of them), reading the
// latest callFilesRead months.
func (l *callLog) read(q callQuery, offset, limit int) (callsRead, error) {
	out := callsRead{Calls: []callEntry{}, Tools: []string{}, Callers: []string{}}
	if l == nil {
		return out, nil
	}
	files, err := filepath.Glob(filepath.Join(l.dir, "*.jsonl"))
	if err != nil {
		return out, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	if len(files) > callFilesRead {
		files = files[:callFilesRead]
	}
	tools, callers := map[string]bool{}, map[string]bool{}
	for _, file := range files {
		entries, lines, err := readCallFile(file)
		if err != nil {
			return out, err
		}
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			tools[e.Tool], callers[e.By] = true, true
			if !q.matches(e, lines[i]) {
				continue
			}
			if out.Total >= offset && len(out.Calls) < limit {
				out.Calls = append(out.Calls, e)
			}
			out.Total++
		}
	}
	for t := range tools {
		out.Tools = append(out.Tools, t)
	}
	for c := range callers {
		out.Callers = append(out.Callers, c)
	}
	slices.Sort(out.Tools)
	slices.Sort(out.Callers)
	return out, nil
}

// readCallFile reads a month's lines in the order written (one not read whole, as a line being
// written, is passed over).
func readCallFile(path string) ([]callEntry, [][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var entries []callEntry
	var lines [][]byte
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	for scanner.Scan() {
		var e callEntry
		if json.Unmarshal(scanner.Bytes(), &e) != nil {
			continue
		}
		entries = append(entries, e)
		lines = append(lines, bytes.Clone(scanner.Bytes()))
	}
	return entries, lines, scanner.Err()
}

func (a *app) registerCalls(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/calls", a.serveCalls)
	mux.HandleFunc("GET /api/calls/export", a.exportCalls)
}

func callQueryOf(r *http.Request) callQuery {
	v := r.URL.Query()
	return callQuery{Text: v.Get("q"), Tool: v.Get("tool"), By: v.Get("by"), Outcome: v.Get("outcome")}
}

// serveCalls answers GET /api/calls?q=&tool=&by=&outcome=&offset=&limit= (100; at most 1,000).
func (a *app) serveCalls(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		limit = 100
	}
	read, err := a.calls.read(callQueryOf(r), max(0, offset), min(limit, 1000))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, read)
}

// exportCalls answers the calls chosen as a file, the oldest first: format=jsonl (the lines as
// written) or csv.
func (a *app) exportCalls(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format != "csv" && format != "jsonl" {
		writeError(w, http.StatusBadRequest, "format is csv or jsonl.")
		return
	}
	read, err := a.calls.read(callQueryOf(r), 0, 1<<30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	slices.Reverse(read.Calls)
	name := "situcraft-calls-" + time.Now().Format("2006-01-02") + "." + format
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if format == "jsonl" {
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, e := range read.Calls {
			line, _ := json.Marshal(e)
			w.Write(append(line, '\n'))
		}
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	out := csv.NewWriter(w)
	out.Write([]string{"time", "by", "via", "leave", "tool", "outcome", "ms", "message", "args", "changed", "ids"})
	for _, e := range read.Calls {
		var changed []string
		for _, c := range e.Changed {
			changed = append(changed, c.Change+" "+c.Kind+" "+c.ID)
		}
		out.Write([]string{e.Time, e.By, e.Via, strings.Join(e.Leave, " "), e.Tool, e.Outcome, strconv.FormatInt(e.Ms, 10), csvSafe(e.Message), csvSafe(string(e.Args)), strings.Join(changed, "; "), strings.Join(e.IDs, " ")})
	}
	out.Flush()
}

// csvSafe keeps a spreadsheet from reading a cell as a formula.
func csvSafe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
