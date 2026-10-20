package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Remote control, enabled with --remote-control: a program on this computer POSTs an action to
// /api/remote/action. The open SituCraft page receives it over server-sent events from
// /api/remote/events, performs it where the user sees it, and POSTs the outcome to
// /api/remote/result/{id}, which the server returns to the waiting caller. The actions are listed
// in actions.go and performed by web/ui/remote.js. Adapted from MaveScape 0.8.0 remote.go.
//
// Only requests from this computer that name it in their Host header are accepted, and browsers
// on other origins are refused (security.go). Reading files by path and writing files also need
// the token printed at startup and written to remote.json (connection.go): another user of a
// shared computer can reach the loopback port but not the owner's terminal or files.

const (
	maxRemoteActionBytes = 8 << 20
	maxRemoteResultBytes = 64 << 20
	remoteHeartbeat      = 15 * time.Second
	longActionTimeout    = 60 * time.Minute
	remoteTokenHeader    = "X-SituCraft-Token"
)

type remoteHub struct {
	mu        sync.Mutex
	counter   uint64
	clients   []*remoteClient
	pending   map[string]chan remoteResult
	timeout   time.Duration
	turn      chan struct{}
	connected chan struct{}
	// open registers local items for the page to open (local.go), answering those the paths
	// name and what was skipped.
	open    func(paths []string) ([]localFile, []string)
	token   string
	outputs outputSlots
	// session names this run of the program: only a page opened from it takes its actions, so
	// that a tab left open while SituCraft was started again (another version, another library) is
	// refused when it reconnects. Empty: any page (the tests' hubs).
	session string
	// scripts: programs may send actions (POST /api/remote/action). Off while an agent drives the
	// window (situcraft mcp), whose actions come only from its stdin (after CytoWeave 0.8.0).
	scripts bool
}

type remoteClient struct {
	id     uint64
	events chan remoteEvent
	done   chan struct{}
}

// An event carries an action for the page: { id, action, args }, the local items it opens, where
// it uploads a file it writes (output.go), and who sent it (what a script calls itself).
type remoteEvent struct {
	ID     string          `json:"id"`
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args,omitempty"`
	Files  []localFile     `json:"files,omitempty"`
	Client string          `json:"client,omitempty"`
	Output string          `json:"output,omitempty"`
}

var (
	errNoPage      = errors.New("No SituCraft page is connected. Open SituCraft in a browser first.")
	errPageBusy    = errors.New("The SituCraft page is busy; try again.")
	errPageTimeout = errors.New("The SituCraft page did not answer in time.")
	errPageGone    = errors.New("The SituCraft page was closed or reloaded before it answered.")
)

type remoteResult struct {
	OK      bool            `json:"ok"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func newRemoteHub() *remoteHub {
	return &remoteHub{
		pending:   map[string]chan remoteResult{},
		timeout:   5 * time.Minute,
		turn:      make(chan struct{}, 1),
		connected: make(chan struct{}),
		token:     randomToken(),
		scripts:   true,
	}
}

func randomToken() string {
	buffer := make([]byte, 18)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func (h *remoteHub) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/remote/events", localOnly(h.serveEvents))
	mux.HandleFunc("POST /api/remote/result/{id}", localOnly(h.serveResult))
	mux.HandleFunc("POST /api/remote/output/{token}", localOnly(h.serveOutput))
	if h.scripts {
		mux.HandleFunc("POST /api/remote/action", localOnly(h.serveAction))
	}
	mux.HandleFunc("GET /api/remote/tools", localOnly(h.serveTools))
}

// clientName is a sender's name as the page shows it: printable, at most 60 characters.
func clientName(name, fallback string) string {
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		return fallback
	}
	if runes := []rune(name); len(runes) > 60 {
		name = string(runes[:60])
	}
	return name
}

// localOnly refuses requests from other machines and from web pages that reach the loopback
// port under another name (DNS rebinding).
func localOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRequest(r) {
			writeError(w, http.StatusForbidden, "Forbidden: remote control only accepts requests from this computer, addressed to localhost or 127.0.0.1.")
			return
		}
		next(w, r)
	}
}

func (h *remoteHub) connect() *remoteClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.counter++
	client := &remoteClient{id: h.counter, events: make(chan remoteEvent, 16), done: make(chan struct{})}
	h.clients = append(h.clients, client)
	close(h.connected)
	h.connected = make(chan struct{})
	return client
}

func (h *remoteHub) disconnect(client *remoteClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for index, item := range h.clients {
		if item == client {
			h.clients = append(h.clients[:index], h.clients[index+1:]...)
			close(client.done)
			return
		}
	}
}

// Actions go to the most recently opened page.
func (h *remoteHub) latest() *remoteClient {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) == 0 {
		return nil
	}
	return h.clients[len(h.clients)-1]
}

func (h *remoteHub) expect() (string, chan remoteResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.counter++
	id := strconv.FormatUint(h.counter, 10)
	result := make(chan remoteResult, 1)
	h.pending[id] = result
	return id, result
}

func (h *remoteHub) take(id string) (chan remoteResult, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	result, ok := h.pending[id]
	delete(h.pending, id)
	return result, ok
}

func (h *remoteHub) serveEvents(w http.ResponseWriter, r *http.Request) {
	if h.session != "" && r.URL.Query().Get("session") != h.session {
		writeError(w, http.StatusConflict, "This page was opened from another run of SituCraft: reload it to drive this one.")
		return
	}
	stream := http.NewResponseController(w)
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("Connection", "keep-alive")
	fmt.Fprint(w, ": connected\n\n")
	if err := stream.Flush(); err != nil {
		return
	}
	client := h.connect()
	defer h.disconnect(client)
	ticker := time.NewTicker(remoteHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-client.events:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: action\ndata: %s\n\n", data)
			if err := stream.Flush(); err != nil {
				return
			}
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			if err := stream.Flush(); err != nil {
				return
			}
		}
	}
}

func (h *remoteHub) hasToken(r *http.Request) bool {
	return subtle.ConstantTimeCompare([]byte(r.Header.Get(remoteTokenHeader)), []byte(h.token)) == 1
}

// serveAction runs {"action": "...", "args": {...}, "client": "..."} in the page. Actions that
// read or write files need the token.
func (h *remoteHub) serveAction(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Action string          `json:"action"`
		Args   json.RawMessage `json:"args"`
		Client string          `json:"client"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRemoteActionBytes))
	if err == nil {
		err = json.Unmarshal(body, &request)
	}
	if err != nil || request.Action == "" {
		writeError(w, http.StatusBadRequest, `Send JSON such as {"action": "get_state"}.`)
		return
	}
	event := remoteEvent{Action: request.Action, Args: request.Args, Client: clientName(request.Client, "a program on this computer")}
	action, known := findAction(request.Action)
	if !known {
		// The page answers with the actions it knows; the hub need not know them all first.
		h.respond(w, r, event)
		return
	}
	var args map[string]any
	json.Unmarshal(request.Args, &args)
	if action.touchesFiles(args) && !h.hasToken(r) {
		what := "Writing files"
		if action.reads || action.readsAt || action.namesFile(args) {
			what = "Reading files"
		}
		writeError(w, http.StatusUnauthorized, what+" needs the "+remoteTokenHeader+" header with the token SituCraft printed when it started (also in remote.json).")
		return
	}
	event, release, err := h.prepare(event, action)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer release()
	h.respond(w, r, event)
}

// prepare completes an event for its action: where the page uploads the file it writes (an upload
// slot, released by the returned function once the action is done) and the local items it opens.
func (h *remoteHub) prepare(event remoteEvent, action remoteAction) (remoteEvent, func(), error) {
	release := func() {}
	if action.output {
		output, free, err := h.prepareOutput(event.Args)
		if err != nil {
			return event, release, err
		}
		event.Output, release = output, free
	}
	if action.reads {
		files, err := h.openFiles(event.Args)
		if err != nil {
			release()
			return event, func() {}, err
		}
		event.Files = files
	}
	return event, release, nil
}

// openFiles registers the items of {"paths": [...]} for the page to open.
func (h *remoteHub) openFiles(args json.RawMessage) ([]localFile, error) {
	var params struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(args, &params); err != nil || len(params.Paths) == 0 {
		return nil, errors.New(`open_paths needs {"paths": ["/path/to/Xenium_output_bundle"]}`)
	}
	if h.open == nil {
		return nil, errors.New("Opening files by path is not available.")
	}
	files, problems := h.open(params.Paths)
	if len(files) == 0 {
		message := "Nothing SituCraft opens was found there."
		if len(problems) > 0 {
			message = problems[0]
		}
		return nil, errors.New(message)
	}
	return files, nil
}

func (h *remoteHub) respond(w http.ResponseWriter, r *http.Request, event remoteEvent) {
	outcome, err := h.dispatch(r.Context(), event)
	switch {
	case err == nil:
		writeJSON(w, outcome)
	case errors.Is(err, errPageTimeout):
		writeError(w, http.StatusGatewayTimeout, err.Error())
	case errors.Is(err, errNoPage), errors.Is(err, errPageBusy), errors.Is(err, errPageGone):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

// dispatch sends an event to the most recently opened page and waits for its result, one at a
// time; a request waiting its turn can still be canceled.
func (h *remoteHub) dispatch(ctx context.Context, event remoteEvent) (remoteResult, error) {
	select {
	case h.turn <- struct{}{}:
	case <-ctx.Done():
		return remoteResult{}, ctx.Err()
	}
	defer func() { <-h.turn }()
	client := h.latest()
	if client == nil {
		return remoteResult{}, errNoPage
	}
	id, result := h.expect()
	event.ID = id
	select {
	case client.events <- event:
	default:
		h.take(id)
		return remoteResult{}, errPageBusy
	}
	limit := h.timeout
	if action, ok := findAction(event.Action); ok && action.long && limit < longActionTimeout {
		limit = longActionTimeout
	}
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case outcome := <-result:
		return outcome, nil
	case <-client.done:
		h.take(id)
		return remoteResult{}, errPageGone
	case <-timer.C:
		h.take(id)
		return remoteResult{}, errPageTimeout
	case <-ctx.Done():
		h.take(id)
		return remoteResult{}, ctx.Err()
	}
}

func (h *remoteHub) serveResult(w http.ResponseWriter, r *http.Request) {
	result, ok := h.take(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "No action is waiting for that result.")
		return
	}
	var outcome remoteResult
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRemoteResultBytes))
	if err == nil {
		err = json.Unmarshal(body, &outcome)
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		message := "The result could not be read."
		if errors.As(err, &tooLarge) {
			message = "The result is too large."
		}
		outcome = remoteResult{OK: false, Message: message}
	}
	result <- outcome
	w.WriteHeader(http.StatusNoContent)
}

func (h *remoteHub) serveTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"version": version, "api": apiVersion, "tools": remoteActions})
}
