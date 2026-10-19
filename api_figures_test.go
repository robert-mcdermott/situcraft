package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestReadsAFigureBack(t *testing.T) {
	_, handler := testApp(t)
	dir := t.TempDir()
	svg := filepath.Join(dir, "Figure_1.svg")
	os.WriteFile(svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0o644)
	other := filepath.Join(dir, "notes.txt")
	os.WriteFile(other, []byte("x"), 0o644)
	ask := func(path, remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/read-figure?path="+url.QueryEscape(path), nil)
		req.Host, req.RemoteAddr = "127.0.0.1:8840", remote
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := ask(svg, "127.0.0.1:5000"); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || rec.Body.String() != `<svg xmlns="http://www.w3.org/2000/svg"/>` {
		t.Fatalf("an SVG: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if rec := ask(other, "127.0.0.1:5000"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a text file: %d", rec.Code)
	}
	if rec := ask(filepath.Join(dir, "none.pdf"), "127.0.0.1:5000"); rec.Code != http.StatusNotFound {
		t.Fatalf("no file: %d", rec.Code)
	}
	if rec := ask(svg, "10.0.0.2:5000"); rec.Code != http.StatusForbidden {
		t.Fatalf("from elsewhere: %d", rec.Code)
	}
}
