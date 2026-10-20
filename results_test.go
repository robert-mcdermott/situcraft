package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
	"situcraft/internal/spatial"
	"situcraft/internal/xenium"
)

// The answers the program gives itself (results.go): the library's projects, a project's datasets,
// results and records, and a result's record and numbers as tables, read without a page, the same
// numbers as its files hold.
func TestResultsWithoutTheWindow(t *testing.T) {
	f := newRefineFixture(t, true)
	all, _ := f.a.artifacts.List()
	var seg artifacts.Manifest
	for _, m := range all {
		if m.Kind == "segmentation" && isBundleOwn(m) {
			seg = m
		}
	}
	b, err := xenium.Open(f.folder, xenium.Options{CacheDir: f.a.cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := f.a.segmentationFolder(context.Background(), b, seg)
	b.Close()
	if err != nil {
		t.Fatal(err)
	}
	cells, err := segment.ReadCells(dir)
	if err != nil {
		t.Fatal(err)
	}
	labels, keep := make([]int32, len(cells)), make([]bool, len(cells))
	for i := range labels {
		labels[i], keep[i] = int32(i%3), true
	}
	codes, sum := codesOf(labels)
	labelsJSON := fmt.Sprintf(`{"id":"lab-x","kind":"labels","name":"By turns","level":0,"names":["A","B","C"],"codes":%q,"checksum":%q}`, codes, sum)
	run := func(target, body string) string {
		t.Helper()
		var started struct {
			ID  string
			Job *struct{ ID string }
		}
		json.NewDecoder(f.post(target, body).Body).Decode(&started)
		if started.Job != nil {
			f.wait(started.Job.ID)
		}
		if started.ID == "" {
			t.Fatalf("%s: no id", body[:min(80, len(body))])
		}
		return started.ID
	}
	graph := run("/api/local/0/graph", fmt.Sprintf(`{"segmentation":%q,"parameters":{"kind":"delaunay"},"cells":%s}`, seg.ID, chosenCells(keep, "")))
	enrichment := run("/api/local/0/spatial", fmt.Sprintf(`{"question":"enrichment","segmentation":%q,"graph":%q,"labels":%s,"parameters":{"permutations":100,"seed":3}}`, seg.ID, graph, labelsJSON))
	distances := run("/api/local/0/spatial", fmt.Sprintf(`{"question":"distances","segmentation":%q,"cells":%s,"labels":%s,"parameters":{"permutations":50,"seed":3}}`, seg.ID, chosenCells(keep, ""), labelsJSON))

	call := func(name, args string) (remoteResult, map[string]any) {
		t.Helper()
		outcome, ok := f.a.programAction(context.Background(), name, json.RawMessage(args))
		if !ok {
			t.Fatalf("%s is not answered by the program", name)
		}
		var data map[string]any
		json.Unmarshal(outcome.Data, &data)
		return outcome, data
	}

	// A result's record and its numbers: the enrichment's pairs, the same numbers analysis.json holds.
	outcome, data := call("get_result", `{"id":"`+enrichment+`","limit":4}`)
	if !outcome.OK || data["kind"] != "spatial" || !strings.Contains(outcome.Message, "rows 1 to 4 of 9") {
		t.Fatalf("get_result: %v %s", outcome.OK, outcome.Message)
	}
	parents := data["parents"].([]any)
	if len(parents) == 0 || !strings.Contains(fmt.Sprint(parents), graph) {
		t.Fatalf("parents: %v", parents)
	}
	table := data["table"].(map[string]any)
	if table["name"] != "pairs" || table["total"].(float64) != 9 || len(table["rows"].([]any)) != 4 {
		t.Fatalf("table: %v", table)
	}
	path, _ := f.a.artifacts.Path(enrichment, spatial.FileAnalysis)
	a, err := spatial.ReadAnalysis(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	row := table["rows"].([]any)[1].([]any) // A with B
	if row[0] != "A" || row[1] != "B" || !sameNumber(row[6], a.Enrichment.Z[1]) || !sameNumber(row[8], a.Enrichment.PAdj[1]) {
		t.Fatalf("A–B: %v; z %v, padj %v", row, a.Enrichment.Z[1], a.Enrichment.PAdj[1])
	}
	names := []string{}
	for _, t := range data["tables"].([]any) {
		names = append(names, t.(map[string]any)["name"].(string))
	}
	if !strings.Contains(strings.Join(names, ","), "pairs") || !strings.Contains(strings.Join(names, ","), "summary") {
		t.Fatalf("tables: %v", names)
	}
	// The next page, a table by name, a table it has not.
	_, data = call("get_result", `{"id":"`+enrichment+`","offset":8,"limit":4}`)
	if rows := data["table"].(map[string]any)["rows"].([]any); len(rows) != 1 || rows[0].([]any)[0] != "C" {
		t.Fatalf("the last page: %v", rows)
	}
	_, data = call("get_result", `{"id":"`+enrichment+`","table":"summary"}`)
	if data["table"].(map[string]any)["name"] != "summary" {
		t.Fatalf("by name: %v", data["table"])
	}
	if outcome, _ := call("get_result", `{"id":"`+enrichment+`","table":"nope"}`); outcome.OK || !strings.Contains(outcome.Message, "pairs") {
		t.Fatalf("a table it has not: %+v", outcome)
	}
	if outcome, _ := call("get_result", `{"id":"spa-0000"}`); outcome.OK || !strings.Contains(outcome.Message, "No result spa-0000") {
		t.Fatalf("no such result: %+v", outcome)
	}
	if outcome, _ := call("get_result", `{}`); outcome.OK || !strings.Contains(outcome.Message, "id is required") {
		t.Fatalf("no id: %+v", outcome)
	}
	_, data = call("get_result", `{"id":"`+distances+`","limit":0}`)
	if table := data["table"].(map[string]any); table["name"] != "pairs" || len(table["rows"].([]any)) != 9 || len(table["columns"].([]any)) != 10 {
		t.Fatalf("nearest distances: %v", table)
	}

	// A project of the library, as the page saves it: its datasets, results (with what they were
	// made from), the page's records, a region, a view and a figure.
	record := func(id string) json.RawMessage {
		m, err := f.a.artifacts.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"id": m.ID, "kind": m.Kind, "name": m.Name, "dataset": "d-1", "state": m.State, "created": m.Created, "method": m.Lineage.Method, "parents": m.Lineage.Parents, "summary": m.Summary})
		return raw
	}
	project := map[string]any{
		"format": "situcraft-project", "version": 1, "id": "p-results", "name": "Results test", "created": "2026-10-20T10:00:00Z", "modified": "2026-10-20T11:00:00Z",
		"datasets":  []any{map[string]any{"id": "d-1", "name": "simulated", "path": f.folder, "run": "run 1", "tissue": "simulated", "cells": len(cells)}},
		"artifacts": []json.RawMessage{record(seg.ID), record(graph), record(enrichment), json.RawMessage(`{"kind":"filter","id":"flt-1","name":"Good cells","version":2,"result":{"cells":100,"kept":90}}`)},
		"regions":   []any{map[string]any{"id": "r-1", "dataset": "d-1", "versions": []any{map[string]any{"name": "Nest", "cls": "Tumor"}}}},
		"views":     []any{map[string]any{"id": "v-1", "name": "Overview", "dataset": "d-1"}},
		"figures":   []any{map[string]any{"id": "fig-1", "name": "Figure 1", "panels": []any{map[string]any{}}}},
	}
	raw, _ := json.Marshal(project)
	os.MkdirAll(filepath.Join(f.a.store.dir, "projects"), 0o755)
	if err := os.WriteFile(filepath.Join(f.a.store.dir, "projects", "p-results.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	outcome, data = call("list_projects", `{}`)
	if !outcome.OK || !strings.Contains(outcome.Message, "Results test") {
		t.Fatalf("list_projects: %s", outcome.Message)
	}
	outcome, data = call("describe_project", `{"project":"results"}`)
	if !outcome.OK || !strings.Contains(outcome.Message, "Results test: 1 dataset (simulated); 3 results (1 segmentation, 1 graph, 1 spatial analysis); 1 record, 1 region, 1 saved view, 1 figure") {
		t.Fatalf("describe_project: %s", outcome.Message)
	}
	results := data["results"].([]any)
	last := results[2].(map[string]any)
	if last["id"] != enrichment || last["dataset"] != "simulated" || !strings.Contains(fmt.Sprint(last["parents"]), graph) {
		t.Fatalf("results: %v", results)
	}
	if outcome, _ := call("describe_project", `{"project":"nothing like it"}`); outcome.OK || !strings.Contains(outcome.Message, "Results test") {
		t.Fatalf("no such project: %+v", outcome)
	}
	// A record of the page's: the record, and its result's numbers.
	outcome, data = call("get_result", `{"id":"flt-1"}`)
	if !outcome.OK || data["kind"] != "filter" || data["table"].(map[string]any)["total"].(float64) != 2 || !strings.Contains(fmt.Sprint(data["projects"]), "Results test") {
		t.Fatalf("a filter: %s %v", outcome.Message, data)
	}

	// Asked by a script, with no page open: the program answers.
	hub := newRemoteHub()
	hub.program = f.a.programAction
	rec := post(hubMux(hub), "/api/remote/action", `{"action":"get_result","args":{"id":"`+enrichment+`","limit":1}}`, nil)
	var hubAnswer remoteResult
	json.NewDecoder(rec.Body).Decode(&hubAnswer)
	if rec.Code != http.StatusOK || !hubAnswer.OK || !strings.Contains(hubAnswer.Message, "rows 1 to 1 of 9") {
		t.Fatalf("a script: %d %+v", rec.Code, hubAnswer)
	}
	// And by an agent, read-only, with no page.
	server := &mcpServer{hub: hub, pageWait: 10 * time.Millisecond}
	replies := runLines(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"describe_project","arguments":{"project":"p-results"}}}`)
	if r := replies[0]["result"].(map[string]any); r["isError"] == true || !strings.Contains(textOf(t, r), enrichment) {
		t.Fatalf("an agent: %v", replies[0])
	}
}

func sameNumber(v any, want float64) bool {
	got, ok := v.(float64)
	if !ok {
		return math.IsNaN(want) && v == nil
	}
	return got == want
}

// A summary's tables, as the report tabulates them: its numbers and words, a table for each list of
// rows, a list of numbers as a row; where it came from left out, its keys in order.
func TestSummaryTablesAsTheReportShowsThem(t *testing.T) {
	tables := summaryTables(json.RawMessage(`{"question":"distances","segmentation":{"id":"seg-1","name":"S"},"labels":{"id":"lab-1","name":"L","count":4},"cells":691,"empty":"","significant":2,"genes":["A","B"],"sizes":[3,1.5],"nearer":[{"a":"x","b":"y","ratio":0.79,"z":-3.8},{"a":"y","b":"x","ratio":1.2,"z":2}],"window":{"kind":"tissue","area":687211.5}}`))
	var names []string
	for _, tb := range tables {
		names = append(names, tb.Name)
	}
	if strings.Join(names, ",") != "summary,summary.sizes,summary.nearer" {
		t.Fatalf("tables %v", names)
	}
	top := fmt.Sprint(tables[0].rows)
	if top != "[[labels.name L] [labels.count 4] [cells 691] [significant 2] [genes A, B] [window.kind tissue] [window.area 687211.5]]" {
		t.Fatalf("numbers %s", top)
	}
	if fmt.Sprint(tables[2].Columns) != "[a b ratio z]" || fmt.Sprint(tables[2].rows[0]) != "[x y 0.79 -3.8]" || fmt.Sprint(tables[1].rows) != "[[3 1.5]]" {
		t.Fatalf("rows %v %v %v", tables[2].Columns, tables[2].rows, tables[1].rows)
	}
}
