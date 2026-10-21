package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The window's part of a run (situcraft run --report; wave 9, slice 6): in a headless browser opened
// on the run's SituCraft, the page makes a project of the run (the bundles, and the results the
// program made, added as the views that make them add them), and writes the result's report and
// methods, as the window writes them, through the same actions scripts use. A browser whose page
// stops answering is started again, the project opened again, and the action asked again once.

// runPageRecord is what the page did: the browser, how many times it was started, the project, each
// action, and the files written by SHA-256.
type runPageRecord struct {
	Browser string        `json:"browser"`
	Starts  int           `json:"starts"`
	Project string        `json:"project,omitempty"`
	Steps   []runPageStep `json:"steps"`
	Outputs []certNamed   `json:"outputs,omitempty"`
}

type runPageStep struct {
	Action  string  `json:"action"`
	OK      bool    `json:"ok"`
	Message string  `json:"message"`
	Seconds float64 `json:"seconds"`
}

const runPageClient = "situcraft run"

// runPage does the window's part.
func runPage(ctx context.Context, opts runOptions, running *runningServer, recipe *resultRecipe, report *runReport) (*runPageRecord, error) {
	browser := opts.chrome
	if browser == "" {
		browser = os.Getenv("CHROME")
	}
	if browser == "" {
		browser = findChromium()
	}
	record := &runPageRecord{Browser: filepath.Base(browser)}
	if browser == "" {
		return record, errors.New("it is written in Chrome, Chromium, Edge or Brave, and none was found (give --chrome PATH or set CHROME)")
	}
	stop, err := headlessPage(ctx, browser, running)
	if err != nil {
		return record, err
	}
	record.Starts = 1
	defer func() { stop() }()
	hub := running.app.control

	// perform asks the page for an action; a page that stopped answering is started again (and the
	// project opened again) and asked once more.
	var perform func(action string, args map[string]any, again bool) (remoteResult, error)
	perform = func(action string, args map[string]any, again bool) (remoteResult, error) {
		began := time.Now()
		encoded, _ := json.Marshal(args)
		event := remoteEvent{Action: action, Args: encoded, Client: runPageClient}
		outcome, err := func() (remoteResult, error) {
			spec, _ := findAction(action)
			prepared, release, err := hub.prepare(event, spec)
			if err != nil {
				return remoteResult{}, err
			}
			defer release()
			return hub.dispatch(ctx, prepared)
		}()
		stalled := errors.Is(err, errPageGone) || errors.Is(err, errPageTimeout) || errors.Is(err, errNoPage) || errors.Is(err, errPageBusy)
		if stalled && again && ctx.Err() == nil && record.Starts < 4 {
			stop()
			if stop, err = headlessPage(ctx, browser, running); err != nil {
				return remoteResult{}, err
			}
			record.Starts++
			if record.Project != "" {
				if _, err := perform("open_project", map[string]any{"project": record.Project}, false); err != nil {
					return remoteResult{}, err
				}
			}
			return perform(action, args, false)
		}
		step := runPageStep{Action: action, Seconds: time.Since(began).Seconds()}
		switch {
		case err != nil:
			step.Message = err.Error()
		case !outcome.OK:
			step.Message = outcome.Message
			err = errors.New(outcome.Message)
		default:
			step.OK, step.Message = true, outcome.Message
		}
		// No path of this computer: the output folder's files by their names, the bundles by role.
		step.Message = strings.ReplaceAll(step.Message, opts.out+string(filepath.Separator), "")
		for _, b := range report.Bundles {
			step.Message = strings.ReplaceAll(step.Message, b.folder, b.Role)
		}
		record.Steps = append(record.Steps, step)
		return outcome, err
	}

	name := "Run of " + recipe.Name
	outcome, err := perform("new_project", map[string]any{"name": name}, true)
	if err != nil {
		return record, err
	}
	var created struct {
		ID string `json:"id"`
	}
	json.Unmarshal(outcome.Data, &created)
	record.Project = created.ID
	var paths []string
	for _, b := range report.Bundles {
		paths = append(paths, b.folder)
	}
	if _, err := perform("open_paths", map[string]any{"paths": paths, "add": true}, true); err != nil {
		return record, err
	}
	var ids []string
	for _, s := range report.Steps {
		ids = append(ids, s.ID)
	}
	if _, err := perform("add_results", map[string]any{"ids": ids}, true); err != nil {
		return record, err
	}
	result := report.Steps[recipe.Result-1]
	for _, o := range []struct{ what, file string }{{"report", "report.html"}, {"methods", "methods.md"}} {
		path := filepath.Join(opts.out, o.file)
		args := map[string]any{"what": o.what, "path": path}
		if o.what == "report" {
			args["results"], args["title"] = []string{result.ID}, name
		} else {
			args["result"] = result.ID
		}
		if _, err := perform("export", args, true); err != nil {
			return record, err
		}
		sum, err := fileSHA256(path)
		if err != nil {
			return record, err
		}
		record.Outputs = append(record.Outputs, certNamed{Path: o.file, SHA256: sum})
	}
	// The project saved in the run's library before the browser stops.
	file := filepath.Join(running.app.store.dir, "projects", record.Project+".json")
	for wait := 0; wait < 100; wait++ {
		if raw, err := os.ReadFile(file); err == nil && strings.Contains(string(raw), result.ID) {
			return record, nil
		}
		select {
		case <-ctx.Done():
			return record, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return record, fmt.Errorf("the run's project was not saved in its library")
}
