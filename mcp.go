package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// "situcraft mcp" is a Model Context Protocol server for AI agents (wave 9, slice 1; requirement
// A2). It answers the agent's JSON-RPC requests on stdin and stdout, one message per line; its
// tools are the remote-control actions (actions.go), each performed by the SituCraft page
// (web/ui/remote.js) as a script's are, under the agent's name. The banner and logs go to stderr,
// since stdout carries the protocol.
//
// Read-only unless allowed: the tools offered are those that read and show (actionHints); --allow
// edits, compute and files add those that change the project, start jobs, and read or write this
// computer's files by path (or download an example), and a call that needs more leave than was
// given is refused with what it needs.
//
// Where it works: a SituCraft already serving this library with remote control on (its
// remote.json) is attached to, and the agent works in its window; otherwise SituCraft is started
// here, with its window opened when a tool first needs it and no script routes while the agent
// drives it, and it stops when the agent closes stdin. Adapted from MaveScape 0.8.0's mcp.go (itself
// from CytoWeave's and Proteoscope's); the gates, the attaching and the instructions are
// SituCraft's.

var mcpProtocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const (
	mcpMaxLine      = 64 << 20
	mcpTextLimit    = 200_000
	mcpImageLimit   = 8 << 20
	mcpPageWait     = 40 * time.Second
	mcpReopenAfter  = 20 * time.Second
	mcpProgressTick = 5 * time.Second
	mcpEndGrace     = time.Second
	mcpInstructions = "SituCraft analyzes imaging spatial transcriptomics (10x Genomics Xenium output bundles) in a window on this computer, which the user watches: the tissue and its transcripts, segmentations and their comparison, quality control, clusters and cell types, post-Xenium H&E and immunofluorescence images, regions, spatial statistics, cohorts of samples compared between groups, figures, and a record anyone can check. Start with get_state (the open project) and jobs; list_projects, describe_project and get_result read the library's projects and any result's record and numbers (as tables, a page at a time) without opening anything; query_cells, query_transcripts and summarize_region ask which cells and transcripts meet a tree of conditions (all, any, not; never a query string) and what a region holds, and save_filter with the same tree saves the cells as a filter analyses use. show_tissue, show_map and figure with picture: true also give you a picture of what they show. Open data with open_example (10x Genomics' example bundles and simulated tissues whose truth is known) or open_paths (absolute paths of Xenium output bundles). Every result is an artifact with its lineage; record says what made one and methods writes its methods. Results are research findings, never a diagnosis. You may have been given read-only tools only: the user starts this server with --allow edits (change the project: regions, views, filters, names, decisions, figures), --allow compute (start jobs: clustering, labels, graphs, spatial analyses, segmentation, cohort comparisons) or --allow files (read and write files by absolute path, download examples). A tool refused for its leave says which it needs; do not try to work around it. Every change appears in the window, is named in the project's history with your name, and can be undone there. Files are written only at absolute paths in folders that exist, never over a file unless overwrite is given."
)

type mcpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpServer struct {
	// hub is SituCraft's own, started here; attached is another SituCraft's, reached over HTTP
	// (one of the two).
	hub      *remoteHub
	attached *attachedSituCraft
	url      string
	// allowed is the leave given (--allow).
	allowed  uint8
	out      io.Writer
	outMu    sync.Mutex
	pageWait time.Duration
	// progressTick: how often a long call reports progress (mcpProgressTick when zero).
	progressTick time.Duration
	calls        sync.WaitGroup
	openPage     func()
	cancelMu     sync.Mutex
	cancels      map[string]context.CancelFunc
	clientMu     sync.Mutex
	client       string
}

// mcpOptions are the flags of situcraft mcp beyond SituCraft's own.
type mcpOptions struct {
	allowed uint8
}

// parseMCPArgs takes --allow (repeated, or a comma-separated list of edits, compute, files or all)
// from the arguments, leaving SituCraft's flags.
func parseMCPArgs(args []string, stderr io.Writer) (mcpOptions, []string, error) {
	var opts mcpOptions
	var rest []string
	add := func(value string) error {
		for _, part := range strings.Split(value, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			switch part {
			case "":
			case "all":
				opts.allowed = leaveEdits | leaveCompute | leaveFiles
			case "none", "read":
			default:
				found := false
				for _, l := range leaveNames {
					if part == l.name {
						opts.allowed |= l.bit
						found = true
					}
				}
				if !found {
					fmt.Fprintf(stderr, "--allow takes edits, compute, files or all (comma-separated or repeated), not %q\n", part)
					return errors.New("invalid --allow")
				}
			}
		}
		return nil
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") || name != "allow" {
			rest = append(rest, arg)
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "--allow needs edits, compute, files or all")
				return opts, nil, errors.New("invalid --allow")
			}
			i++
			value = args[i]
		}
		if err := add(value); err != nil {
			return opts, nil, err
		}
	}
	return opts, rest, nil
}

func runMCP(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	log.SetOutput(stderr)
	opts, rest, err := parseMCPArgs(args, stderr)
	if err != nil {
		return 2
	}
	cfg, err := parseConfig(rest)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(stderr, "situcraft mcp [--allow edits,compute,files|all] [situcraft's flags]: a Model Context Protocol server on stdin and stdout; read-only tools unless --allow gives more (docs/MCP.md).")
		return 0
	}
	if err != nil {
		return 2
	}
	if cfg.showVersion {
		fmt.Fprintf(stdout, "situcraft %s\n", version)
		return 0
	}
	if !isLoopbackName(normalizeHost(cfg.host)) {
		fmt.Fprintln(stderr, "situcraft mcp serves only this computer; leave out --host or use --host 127.0.0.1.")
		return 2
	}
	window := cfg.window
	if window == "" {
		window = "app"
	}
	if opts.allowed == 0 {
		fmt.Fprintln(stderr, "situcraft mcp: offering the tools that read and show (--allow edits, compute or files offers more).")
	} else {
		fmt.Fprintf(stderr, "situcraft mcp: offering the tools that read and show, and those that need %s.\n", leaveText(opts.allowed))
	}

	// A SituCraft already serving this library, with remote control: the agent works in its window.
	if attached := findAttachable(cfg.dataDir); attached != nil {
		fmt.Fprintf(stderr, "SituCraft %s is already running at %s with this library (%s): the agent works in its window.\n", attached.version, attached.url, cfg.dataDir)
		server := &mcpServer{attached: attached, url: attached.url, allowed: opts.allowed, out: stdout, pageWait: mcpPageWait}
		server.openPage = server.pageOpener(attached.url, window, cfg.dataDir)
		if err := server.serveStream(context.Background(), stdin); err != nil {
			fmt.Fprintln(stderr, "MCP:", err)
			return 1
		}
		return 0
	}
	if running := findRunning(cfg.host, cfg.port); running != "" && sameLibrary(running, cfg.dataDir) {
		fmt.Fprintf(stderr, "SituCraft is already running at %s with this library, without remote control: the agent works in a SituCraft of its own. Start that one with --remote-control for the agent to work in its window.\n", running)
	}

	cfg.remote, cfg.mcp = true, true
	cfg.window = "none"
	running, err := start(cfg, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	served := make(chan error, 1)
	go func() { served <- running.serve() }()
	server := &mcpServer{hub: running.app.control, url: running.url, allowed: opts.allowed, out: stdout, pageWait: mcpPageWait}
	if window != "none" {
		server.openPage = server.pageOpener(running.url, window, cfg.dataDir)
	}
	err = server.serveStream(context.Background(), stdin)
	running.stop(2 * time.Second)
	if err != nil {
		fmt.Fprintln(stderr, "MCP:", err)
		return 1
	}
	if err := <-served; err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// pageOpener opens the window when a tool needs a page, at most once in mcpReopenAfter.
func (s *mcpServer) pageOpener(url, window, dataDir string) func() {
	if window == "none" {
		return nil
	}
	var mu sync.Mutex
	var last time.Time
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(last) < mcpReopenAfter {
			return
		}
		last = time.Now()
		if _, err := openWindow(url, window, dataDir); err != nil {
			log.Printf("open window: %v", err)
		}
	}
}

// serveStream reads one JSON-RPC message per line until the input ends. Tool calls run
// concurrently; the page still performs them one at a time.
func (s *mcpServer) serveStream(ctx context.Context, in io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader := bufio.NewReaderSize(in, 1<<20)
	var readErr error
	for {
		line, err := readLine(reader, mcpMaxLine)
		if errors.Is(err, errLineTooLong) {
			s.send(json.RawMessage("null"), nil, &mcpError{Code: -32700, Message: "Parse error: the message is longer than 64 MiB"})
			continue
		}
		if len(strings.TrimSpace(string(line))) > 0 {
			s.receive(ctx, line)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				readErr = err
			}
			break
		}
	}
	grace := time.AfterFunc(mcpEndGrace, cancel)
	s.calls.Wait()
	grace.Stop()
	return readErr
}

var errLineTooLong = errors.New("line too long")

func readLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	tooLong := false
	for {
		chunk, err := reader.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(chunk) > limit {
				tooLong, line = true, nil
			} else {
				line = append(line, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if tooLong && err == nil {
			return nil, errLineTooLong
		}
		return []byte(strings.TrimRight(string(line), "\r\n")), err
	}
}

func (s *mcpServer) receive(ctx context.Context, line []byte) {
	trimmed := strings.TrimSpace(string(line))
	if !strings.HasPrefix(trimmed, "{") {
		code, message := -32600, "Invalid Request: send one JSON-RPC object per line (batches are not supported)"
		if !json.Valid(line) {
			code, message = -32700, "Parse error"
		}
		s.send(json.RawMessage("null"), nil, &mcpError{Code: code, Message: message})
		return
	}
	var message mcpMessage
	if err := json.Unmarshal(line, &message); err != nil {
		s.send(json.RawMessage("null"), nil, &mcpError{Code: -32700, Message: "Parse error"})
		return
	}
	hasID := len(message.ID) > 0 && string(message.ID) != "null"
	if message.Method == "" {
		if hasID && (len(message.Result) > 0 || len(message.Error) > 0) {
			return
		}
		s.send(idOrNull(message.ID), nil, &mcpError{Code: -32600, Message: "Invalid Request"})
		return
	}
	if message.JSONRPC != "2.0" {
		if hasID {
			s.send(message.ID, nil, &mcpError{Code: -32600, Message: "Invalid Request: jsonrpc must be \"2.0\""})
		}
		return
	}
	if message.Method == "tools/call" && hasID {
		callCtx, cancel := context.WithCancel(ctx)
		key := string(message.ID)
		s.cancelMu.Lock()
		if s.cancels == nil {
			s.cancels = map[string]context.CancelFunc{}
		}
		s.cancels[key] = cancel
		s.cancelMu.Unlock()
		s.calls.Add(1)
		go func() {
			defer s.calls.Done()
			defer func() {
				s.cancelMu.Lock()
				delete(s.cancels, key)
				s.cancelMu.Unlock()
				cancel()
			}()
			s.handle(callCtx, message)
		}()
		return
	}
	s.handle(ctx, message)
}

func idOrNull(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

// tools are the actions the agent was given leave for, in their order.
func (s *mcpServer) tools() []remoteAction {
	var out []remoteAction
	for _, action := range remoteActions {
		if action.hint.leave&^s.allowed == 0 {
			out = append(out, action)
		}
	}
	return out
}

func (s *mcpServer) handle(ctx context.Context, request mcpMessage) {
	notification := len(request.ID) == 0 || string(request.ID) == "null"
	var result any
	var failure *mcpError
	switch request.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      struct {
				Name  string `json:"name"`
				Title string `json:"title"`
			} `json:"clientInfo"`
		}
		json.Unmarshal(request.Params, &params)
		s.clientMu.Lock()
		s.client = clientName(params.ClientInfo.Title, clientName(params.ClientInfo.Name, ""))
		s.clientMu.Unlock()
		result = map[string]any{
			"protocolVersion": negotiateVersion(params.ProtocolVersion),
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "situcraft", "title": "SituCraft", "version": version},
			"instructions":    mcpInstructions,
		}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": s.tools()}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Meta      struct {
				ProgressToken json.RawMessage `json:"progressToken"`
			} `json:"_meta"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			failure = &mcpError{Code: -32602, Message: "Invalid params"}
			break
		}
		action, ok := findAction(params.Name)
		if !ok {
			failure = &mcpError{Code: -32602, Message: fmt.Sprintf("Unknown tool: %s", params.Name)}
			break
		}
		stop := s.reportProgress(params.Meta.ProgressToken)
		result = s.callTool(ctx, action, params.Arguments)
		stop()
		if ctx.Err() != nil {
			return
		}
	case "notifications/cancelled":
		var params struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		json.Unmarshal(request.Params, &params)
		s.cancelMu.Lock()
		cancel := s.cancels[string(params.RequestID)]
		s.cancelMu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	default:
		if strings.HasPrefix(request.Method, "notifications/") {
			return
		}
		failure = &mcpError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", request.Method)}
	}
	if notification {
		return
	}
	s.send(request.ID, result, failure)
}

// reportProgress sends a progress notification every few seconds while a call with a progress
// token runs (a job such as a refinement can take minutes). The function it returns stops them and
// waits until the last one is written, so that none follows the call's reply (MCP: progress ends
// with the request).
func (s *mcpServer) reportProgress(token json.RawMessage) func() {
	if len(token) == 0 || string(token) == "null" {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		tick := s.progressTick
		if tick <= 0 {
			tick = mcpProgressTick
		}
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for step := 1; ; step++ {
			select {
			case <-done:
				return
			case <-ticker.C:
				// A tick and the end can come together: the end wins.
				select {
				case <-done:
					return
				default:
				}
				s.notify("notifications/progress", map[string]any{"progressToken": token, "progress": step, "message": "SituCraft is working in the window"})
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

func negotiateVersion(requested string) string {
	for _, supported := range mcpProtocolVersions {
		if requested == supported {
			return requested
		}
	}
	return mcpProtocolVersions[0]
}

func (s *mcpServer) send(id json.RawMessage, result any, failure *mcpError) {
	message := map[string]any{"jsonrpc": "2.0", "id": id}
	if failure != nil {
		message["error"] = failure
	} else {
		message["result"] = result
	}
	s.write(message, id)
}

func (s *mcpServer) notify(method string, params any) {
	s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params}, nil)
}

func (s *mcpServer) write(message map[string]any, id json.RawMessage) {
	data, err := json.Marshal(message)
	if err != nil {
		data, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": idOrNull(id), "error": mcpError{Code: -32603, Message: err.Error()}})
	}
	s.outMu.Lock()
	defer s.outMu.Unlock()
	s.out.Write(append(data, '\n'))
}

// callTool checks the arguments and the leave they need, waits for a page (opening the window),
// prepares the files the action reads or writes (the agent on stdio is the person's, as a script
// with the token is), sends it to the page under the agent's name and shapes the reply.
func (s *mcpServer) callTool(ctx context.Context, action remoteAction, raw json.RawMessage) map[string]any {
	var args map[string]any
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return toolError("The arguments are not a JSON object.")
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	if missing := action.leave(args) &^ s.allowed; missing != 0 {
		return toolError(fmt.Sprintf("%s needs leave to use %s, which this server was not given: the user starts situcraft mcp with --allow %s to give it.", action.Name, leaveText(missing), strings.ReplaceAll(leaveText(missing), " and ", ",")))
	}
	required, _ := action.InputSchema["required"].([]string)
	for _, name := range required {
		if value, ok := args[name]; !ok || value == nil || value == "" {
			return toolError(fmt.Sprintf("%s is required.", name))
		}
	}
	encoded, _ := json.Marshal(args)
	s.clientMu.Lock()
	client := clientName(s.client, "an AI agent")
	s.clientMu.Unlock()
	if s.attached != nil {
		outcome, err := s.callAttached(ctx, action, encoded, client)
		if err != nil {
			return toolError(err.Error())
		}
		return commandResult(outcome)
	}
	if action.program && s.hub.program != nil {
		if outcome, ok := s.hub.program(ctx, action.Name, encoded); ok {
			return commandResult(outcome)
		}
	}
	if s.hub.latest() == nil && s.openPage != nil {
		s.openPage()
	}
	if !s.hub.waitForPage(ctx, s.pageWait) {
		return toolError(fmt.Sprintf("No SituCraft page is connected. Open %s in a browser, then try again.", s.url))
	}
	event, release, err := s.hub.prepare(remoteEvent{Action: action.Name, Args: encoded, Client: client, Restricted: true, Leave: leaveList(s.allowed)}, action)
	if err != nil {
		return toolError(err.Error())
	}
	defer release()
	outcome, err := s.hub.dispatch(ctx, event)
	if err != nil {
		return toolError(err.Error())
	}
	return commandResult(outcome)
}

// callAttached sends the action to the SituCraft attached to; when it has no page, its window is
// opened and the call tried again until one connects.
func (s *mcpServer) callAttached(ctx context.Context, action remoteAction, args json.RawMessage, client string) (remoteResult, error) {
	deadline := time.Now().Add(s.pageWait)
	opened := false
	for {
		outcome, status, err := s.attached.call(ctx, action.Name, args, client, leaveList(s.allowed))
		if status != http.StatusServiceUnavailable || !strings.Contains(err.Error(), "No SituCraft page") {
			return outcome, err
		}
		if !opened && s.openPage != nil {
			s.openPage()
			opened = true
		}
		if time.Now().After(deadline) {
			return remoteResult{}, fmt.Errorf("No SituCraft page is connected. Open %s in a browser, then try again.", s.url)
		}
		select {
		case <-ctx.Done():
			return remoteResult{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// leaveList names the leave given, for the page (never null: an agent given none is still
// restricted).
func leaveList(allowed uint8) []string {
	out := []string{}
	for _, l := range leaveNames {
		if allowed&l.bit != 0 {
			out = append(out, l.name)
		}
	}
	return out
}

func toolError(message string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": message}}, "isError": true}
}

// commandResult: the page's sentence and its data, as text (the data's JSON after the sentence,
// cut at mcpTextLimit) and as structured content (all of it, as the page wrote it: its fields in
// their order, its numbers exactly), and its pictures as images (extractImages).
func commandResult(outcome remoteResult) map[string]any {
	if !outcome.OK {
		return toolError(outcome.Message)
	}
	text := outcome.Message
	structured := map[string]any{"message": outcome.Message}
	var images []mcpImage
	if len(outcome.Data) > 0 && string(outcome.Data) != "null" {
		if json.Valid(outcome.Data) {
			data, found, dropped := extractImages(outcome.Data)
			images = found
			structured["data"] = data
			serialized := string(data)
			if len(serialized) > mcpTextLimit {
				serialized = strings.ToValidUTF8(serialized[:mcpTextLimit], "") + " … (truncated; the structured content has everything)"
			}
			if text != "" {
				text += "\n\n"
			}
			text += serialized
			if dropped > 0 {
				text += fmt.Sprintf("\n\n%d more image%s left out (null above): the images of one result are limited to %d MB in all.", dropped, map[bool]string{true: "", false: "s"}[dropped == 1], mcpImageLimit>>20)
			}
		}
	}
	if text == "" {
		text = "Done."
	}
	content := []any{map[string]any{"type": "text", "text": text}}
	for index, image := range images {
		content = append(content,
			map[string]any{"type": "text", "text": fmt.Sprintf("Image %d: %s", index+1, image.caption)},
			map[string]any{"type": "image", "data": image.data, "mimeType": image.mimeType})
	}
	return map[string]any{"content": content, "structuredContent": structured}
}

// Pictures in a result (after MaveScape's): the page gives each as a data URL in its data (the
// tissue shown, a figure), with a caption beside it. An agent gets each as MCP image content, after
// the text, and the data names it by its number (1, 2, …), its fields kept in the page's order;
// past mcpImageLimit of them in all, null. A script reads the data URLs as they are.
type mcpImage struct {
	data, mimeType, caption string
}

var dataURLImage = regexp.MustCompile(`"data:(image/(?:png|jpeg|webp|gif));base64,([A-Za-z0-9+/=]+)"`)

func extractImages(raw json.RawMessage) (json.RawMessage, []mcpImage, int) {
	if !bytes.Contains(raw, []byte(`"data:image/`)) {
		return raw, nil, 0
	}
	captions := map[string]string{}
	var decoded any
	if json.Unmarshal(raw, &decoded) == nil {
		collectCaptions(decoded, captions)
	}
	var images []mcpImage
	dropped, total := 0, 0
	out := dataURLImage.ReplaceAllFunc(raw, func(match []byte) []byte {
		parts := dataURLImage.FindSubmatch(match)
		data := string(parts[2])
		if total+len(data) > mcpImageLimit {
			dropped++
			return []byte("null")
		}
		total += len(data)
		caption := captions[data]
		if caption == "" {
			caption = "image"
		}
		images = append(images, mcpImage{data: data, mimeType: string(parts[1]), caption: caption})
		return []byte(strconv.Itoa(len(images)))
	})
	return out, images, dropped
}

// collectCaptions finds each data URL's caption: its object's caption, title or name.
func collectCaptions(value any, captions map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		caption := ""
		for _, key := range []string{"caption", "title", "name"} {
			if text, ok := typed[key].(string); ok && text != "" {
				caption = text
				break
			}
		}
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.HasPrefix(text, "data:image/") {
				if _, data, found := strings.Cut(text, ";base64,"); found {
					captions[data] = caption
				}
				continue
			}
			collectCaptions(item, captions)
		}
	case []any:
		for _, item := range typed {
			collectCaptions(item, captions)
		}
	}
}

// --- Attaching to a SituCraft already running --------------------------------------------------

// attachedSituCraft is a SituCraft serving the same library with remote control on: its address
// and the token from its remote.json (readable by this user only).
type attachedSituCraft struct {
	url, token, version string
	client              *http.Client
}

// findAttachable answers the SituCraft serving dataDir's library with remote control, if one does:
// its remote.json names it, and its /api/info says it is SituCraft, with remote control, on this
// library.
func findAttachable(dataDir string) *attachedSituCraft {
	raw, err := os.ReadFile(filepath.Join(dataDir, connectionFileName))
	if err != nil {
		return nil
	}
	var conn connectionInfo
	if json.Unmarshal(raw, &conn) != nil || conn.URL == "" || conn.Token == "" {
		return nil
	}
	body, ok := runningInfo(conn.URL)
	if !ok || !body.RemoteControl || !sameFolder(body.DataDir, dataDir) {
		return nil
	}
	return &attachedSituCraft{url: conn.URL, token: conn.Token, version: body.Version, client: &http.Client{}}
}

// runningInfo is what a SituCraft at url says of itself.
func runningInfo(url string) (info, bool) {
	var body info
	client := http.Client{Timeout: time.Second}
	response, err := client.Get(url + "/api/info")
	if err != nil {
		return body, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body) != nil || body.Name != "SituCraft" {
		return body, false
	}
	return body, true
}

// sameLibrary says whether the SituCraft at url keeps its library in dataDir.
func sameLibrary(url, dataDir string) bool {
	body, ok := runningInfo(url)
	return ok && sameFolder(body.DataDir, dataDir)
}

func sameFolder(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	clean := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		return filepath.Clean(p)
	}
	return clean(a) == clean(b)
}

// call sends an action as a script with the token does: its outcome, or the HTTP status and why
// it could not be performed.
func (a *attachedSituCraft) call(ctx context.Context, action string, args json.RawMessage, client string, leave []string) (remoteResult, int, error) {
	payload, _ := json.Marshal(map[string]any{"action": action, "args": args, "client": client, "leave": leave})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url+"/api/remote/action", bytes.NewReader(payload))
	if err != nil {
		return remoteResult{}, 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(remoteTokenHeader, a.token)
	response, err := a.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return remoteResult{}, 0, ctx.Err()
		}
		return remoteResult{}, 0, fmt.Errorf("SituCraft at %s did not answer: %v", a.url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRemoteResultBytes+1<<20))
	if err != nil {
		return remoteResult{}, response.StatusCode, err
	}
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &failure) != nil || failure.Error == "" {
			failure.Error = fmt.Sprintf("SituCraft answered HTTP %d", response.StatusCode)
		}
		return remoteResult{}, response.StatusCode, errors.New(failure.Error)
	}
	var outcome remoteResult
	if err := json.Unmarshal(body, &outcome); err != nil {
		return remoteResult{}, response.StatusCode, fmt.Errorf("SituCraft's answer could not be read: %v", err)
	}
	return outcome, response.StatusCode, nil
}
