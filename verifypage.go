package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The page's own records in "situcraft verify" (wave 8, slice 6): each as certified (the digest of
// its record in the project certified), and those the page computes made again in a headless page
// opened on the SituCraft the steps were made again in (a design from its samples, a filter's cells
// from their attributes, a figure's drawing), as MaveScape's verify computes its numbers in one.

// The page records computed again.
var pageComputed = map[string]bool{"design": true, "filter": true, "figure": true}

// projectItems reads the project certified: the document, and its records by ID.
func projectItems(raw []byte) (map[string]any, map[string]any) {
	project, err := decodeGeneric(raw)
	doc, _ := project.(map[string]any)
	items := map[string]any{}
	if err != nil || doc == nil {
		return nil, items
	}
	for _, field := range []string{"datasets", "artifacts", "figures", "views", "regions", "exports"} {
		list, _ := doc[field].([]any)
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				if id, ok := m["id"].(string); ok {
					items[id] = m
				}
			}
		}
	}
	return doc, items
}

// checkPageRecord checks one of the page's records against its digest.
func checkPageRecord(s certStep, items map[string]any, found *stepFound) {
	item, ok := items[s.ID]
	switch {
	case !ok:
		found.Verdict, found.Detail = stepDiffers, "it is not in the project certified"
	case sha256Hex(canonicalJSON(item)) != s.Record:
		found.Verdict, found.Detail = stepDiffers, "its record is not as certified (its digest differs)"
	default:
		found.Verdict, found.Detail = stepAsRecorded, "the page's own record, checked by its digest"
	}
}

// verifyPageRecords computes again, in a headless page, the page's records named (report.Steps
// indexes): designs, filters, figures.
func verifyPageRecords(ctx context.Context, opts verifyOptions, c *certificate, doc map[string]any, projectRaw []byte, compute []int, bundles map[string]string, running *runningServer, report *verifyReport, out io.Writer) {
	if len(compute) == 0 || doc == nil {
		return
	}
	unchecked := func(why string) {
		for _, i := range compute {
			report.Steps[i].Verdict, report.Steps[i].Detail = stepUnchecked, why
			fmt.Fprintf(out, "  %d. %s (%s): %s (%s)\n", report.Steps[i].N, report.Steps[i].Name, report.Steps[i].Kind, stepUnchecked, why)
		}
	}
	if opts.noPage {
		unchecked("not computed again (--no-page)")
		return
	}
	browser := opts.chrome
	if browser == "" {
		browser = os.Getenv("CHROME")
	}
	if browser == "" {
		browser = findChromium()
	}
	if browser == "" {
		unchecked("no Chrome, Chromium, Edge or Brave to compute it again in (give --chrome PATH, or --no-page)")
		return
	}
	// The project certified, with this computer's paths, in the library of the SituCraft the steps
	// were made again in.
	// (A file of the page's records not found, an image's, is left as written.)
	raw, _ := localize(projectRaw, bundles, func(string) string { return "" })
	id := c.Project.ID
	if err := os.WriteFile(filepath.Join(running.app.store.dir, "projects", id+".json"), raw, 0o644); err != nil {
		unchecked("the project could not be placed: " + err.Error())
		return
	}
	stop, err := headlessPage(ctx, browser, running)
	if err != nil {
		unchecked(err.Error())
		return
	}
	defer stop()
	hub := running.app.control
	args, _ := json.Marshal(map[string]any{"project": id})
	if outcome, err := hub.dispatch(ctx, remoteEvent{Action: "open_project", Args: args, Client: verifyClient}); err != nil || !outcome.OK {
		why := "the page could not open the project"
		if err != nil {
			why += ": " + err.Error()
		} else {
			why += ": " + outcome.Message
		}
		unchecked(why)
		return
	}
	var steps []map[string]any
	for _, i := range compute {
		s := c.Steps[i]
		steps = append(steps, map[string]any{"id": s.ID, "kind": s.Kind, "drawing": s.Drawing})
	}
	args, _ = json.Marshal(map[string]any{"steps": steps})
	outcome, err := hub.dispatch(ctx, remoteEvent{Action: "verify_records", Args: args, Client: verifyClient})
	if err == nil && !outcome.OK {
		err = errors.New(outcome.Message)
	}
	if err != nil {
		unchecked("the page could not compute it again: " + err.Error())
		return
	}
	var answer struct {
		Steps []struct {
			ID      string   `json:"id"`
			Same    *bool    `json:"same"`
			Detail  string   `json:"detail"`
			Differ  []string `json:"differ"`
			Checked bool     `json:"checked"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(outcome.Data, &answer); err != nil {
		unchecked("the page's answer could not be read")
		return
	}
	for _, i := range compute {
		found := &report.Steps[i]
		found.Verdict, found.Detail = stepUnchecked, "the page did not compute it again"
		for _, r := range answer.Steps {
			if r.ID != found.ID {
				continue
			}
			switch {
			case !r.Checked || r.Same == nil:
				found.Verdict, found.Detail = stepUnchecked, r.Detail
			case *r.Same:
				found.Verdict, found.Detail = stepVerified, r.Detail
			default:
				found.Verdict, found.Detail, found.Differ = stepDiffers, r.Detail, r.Differ
			}
		}
		fmt.Fprintf(out, "  %d. %s (%s): %s%s\n", found.N, found.Name, found.Kind, found.Verdict, detailText(*found))
	}
}

// headlessPage opens SituCraft's page in a headless browser with a profile of its own and waits
// for it to connect; the function returned closes it. A browser whose page has not connected in
// 20 s is replaced, up to three times (a fresh profile's first start is sometimes slow). After
// MaveScape 0.8.0's startHeadlessPage.
func headlessPage(ctx context.Context, browser string, running *runningServer) (func(), error) {
	profiles, err := os.MkdirTemp("", "situcraft-verify-page-")
	if err != nil {
		return nil, err
	}
	var chrome *exec.Cmd
	var exited chan struct{}
	kill := func() {
		if chrome != nil && chrome.Process != nil {
			chrome.Process.Kill()
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
			}
		}
	}
	// More flags, as the validation suite's (SITUCRAFT_CHROME_ARGS: in CI, WebGL in software).
	extra := strings.Fields(os.Getenv("SITUCRAFT_CHROME_ARGS"))
	for attempt := 1; ; attempt++ {
		args := append([]string{
			"--headless=new",
			"--user-data-dir=" + filepath.Join(profiles, fmt.Sprintf("profile-%d", attempt)),
			"--no-first-run",
			"--no-default-browser-check",
			"--disable-extensions",
			"--disable-sync",
			"--disable-background-networking",
			"--disable-component-update",
			"--disable-features=Translate,MediaRouter,OptimizationHints",
			"--use-mock-keychain",
			"--password-store=basic",
			"--window-size=1600,1000",
		}, extra...)
		chrome = exec.CommandContext(ctx, browser, append(args, running.url+"/")...)
		if err := chrome.Start(); err != nil {
			os.RemoveAll(profiles)
			return nil, fmt.Errorf("could not start %s: %v", browser, err)
		}
		exited = make(chan struct{})
		go func(cmd *exec.Cmd, done chan struct{}) {
			cmd.Wait()
			close(done)
		}(chrome, exited)
		if running.app.control.waitForPage(ctx, 20*time.Second) {
			return func() {
				kill()
				os.RemoveAll(profiles)
			}, nil
		}
		kill()
		if ctx.Err() != nil || attempt == 3 {
			os.RemoveAll(profiles)
			return nil, fmt.Errorf("the page did not start in %s (%d attempts)", filepath.Base(browser), attempt)
		}
	}
}

// waitForPage waits until a page has connected to the hub.
func (h *remoteHub) waitForPage(ctx context.Context, limit time.Duration) bool {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	for {
		h.mu.Lock()
		has := len(h.clients) > 0
		connected := h.connected
		h.mu.Unlock()
		if has {
			return true
		}
		select {
		case <-connected:
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		}
	}
}
