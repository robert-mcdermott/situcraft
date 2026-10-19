package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// browseRequest asks for a folder's listing as the page does, from this computer or another.
func browseRequest(t *testing.T, handler http.Handler, path string, hidden bool, peer string) (*httptest.ResponseRecorder, browseAnswer) {
	t.Helper()
	q := url.Values{}
	if path != "" {
		q.Set("path", path)
	}
	if hidden {
		q.Set("hidden", "1")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/browse?"+q.Encode(), nil)
	req.Host = "127.0.0.1:8840"
	req.RemoteAddr = peer
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var answer browseAnswer
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Fatal(err)
		}
	}
	return rec, answer
}

// A folder of what a user keeps beside their data: a bundle (its run and region read from its
// manifest), a Zarr store, a plain folder, a link to the bundle, a table, an image, a project, a
// file SituCraft does not open, and a hidden file; listed folders first, each kind as SituCraft
// reads it; a file's path lists its folder with it chosen; the home folder by default; what is not
// there, and another computer, refused.
func TestBrowse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	data := filepath.Join(home, "Xenium data")
	bundle := filepath.Join(data, "run 10")
	for _, dir := range []string{bundle, filepath.Join(data, "run 9"), filepath.Join(data, "store.zarr"), filepath.Join(data, "notes"), filepath.Join(home, "Documents")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, text string) {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(bundle, bundleManifest), `{"run_name": "Lung run", "region_name": "Region 1", "panel_name": "Lung panel", "analysis_sw_version": "xenium-2.0.0.10", "num_cells": 11898}`)
	write(filepath.Join(bundle, "cells.parquet"), "")
	write(filepath.Join(data, "run 9", bundleManifest), `not JSON`)
	write(filepath.Join(data, "labels.csv"), "cell_id,cell_type\n")
	write(filepath.Join(data, "he.ome.tif"), "tiff")
	write(filepath.Join(data, "study.scz"), "zip")
	write(filepath.Join(data, "README.txt"), "notes")
	write(filepath.Join(data, ".DS_Store"), "")
	// A link to the bundle is the bundle (where this computer lets a test make links).
	linked := os.Symlink(bundle, filepath.Join(data, "a link")) == nil
	a, handler := testApp(t)
	rec, answer := browseRequest(t, handler, data, false, "127.0.0.1:5555")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var got []string
	for _, e := range answer.Entries {
		got = append(got, e.Name+":"+e.Kind)
	}
	want := "notes:folder run 9:bundle run 10:bundle store.zarr:zarr he.ome.tif:image labels.csv:table README.txt:file study.scz:project"
	if linked {
		want = "a link:bundle " + want
	}
	if strings.Join(got, " ") != want {
		t.Errorf("entries %s,\nwant %s", strings.Join(got, " "), want)
	}
	if answer.Path != data || answer.Parent != home || answer.Home != home || answer.Separator != string(filepath.Separator) || answer.Kind != "" {
		t.Errorf("the folder: %+v", answer)
	}
	for _, e := range answer.Entries {
		switch e.Name {
		case "run 10":
			if b := e.Bundle; b == nil || b.Run != "Lung run" || b.Region != "Region 1" || b.Panel != "Lung panel" || b.Software != "xenium-2.0.0.10" || b.Cells == nil || *b.Cells != 11898 || e.Path != bundle {
				t.Errorf("the bundle: %+v %+v", e, b)
			}
		case "run 9":
			if e.Bundle != nil {
				t.Errorf("a manifest that is not JSON: %+v", e.Bundle)
			}
		case "labels.csv":
			if e.Size != int64(len("cell_id,cell_type\n")) || e.Modified == "" {
				t.Errorf("the table: %+v", e)
			}
		}
	}
	// Places: the home folder, its Documents (its Desktop is not there), the examples, the disks.
	names := map[string]string{}
	for _, p := range answer.Places {
		names[p.Name] = p.Path
	}
	if names["Home"] != home || names["Documents"] != filepath.Join(home, "Documents") || names["Desktop"] != "" || names["SituCraft's examples"] != "" && names["SituCraft's examples"] != a.examples.Dir {
		t.Errorf("places %+v", answer.Places)
	}
	// Hidden entries when asked for.
	if _, hidden := browseRequest(t, handler, data, true, "127.0.0.1:5555"); len(hidden.Entries) != len(answer.Entries)+1 || !anyHidden(hidden.Entries) {
		t.Errorf("hidden: %d entries", len(hidden.Entries))
	}
	// A bundle's own folder says so; a file's path lists its folder with it chosen.
	if _, in := browseRequest(t, handler, bundle, false, "127.0.0.1:5555"); in.Kind != "bundle" || in.Bundle == nil || in.Bundle.Region != "Region 1" {
		t.Errorf("inside the bundle: %+v", in)
	}
	if _, file := browseRequest(t, handler, filepath.Join(data, "labels.csv"), false, "127.0.0.1:5555"); file.Path != data || file.Select != "labels.csv" {
		t.Errorf("a file's path: %s, chosen %q", file.Path, file.Select)
	}
	// The home folder by default, and ~ for it.
	if _, def := browseRequest(t, handler, "", false, "127.0.0.1:5555"); def.Path != home {
		t.Errorf("by default: %s", def.Path)
	}
	if _, tilde := browseRequest(t, handler, "~/Xenium data", false, "127.0.0.1:5555"); tilde.Path != data {
		t.Errorf("~: %s", tilde.Path)
	}
	// Refused: what is not there, and another computer.
	if rec, _ := browseRequest(t, handler, filepath.Join(home, "gone"), false, "127.0.0.1:5555"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "nothing is at") {
		t.Errorf("not there: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := browseRequest(t, handler, data, false, "10.0.0.5:5555"); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Only this computer") {
		t.Errorf("another computer: %d %s", rec.Code, rec.Body)
	}
}

func anyHidden(entries []browseEntry) bool {
	for _, e := range entries {
		if e.Hidden && e.Name == ".DS_Store" {
			return true
		}
	}
	return false
}
