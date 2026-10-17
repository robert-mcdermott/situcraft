package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/slide"
)

// A post-Xenium image added to a dataset: checked first (what it is, the alignment named after it
// beside it), added by a job that records it where it lies (by SHA-256, never copied) and its
// supplied alignment as its first transform (the file where it lies), placing it in µm; its tiles
// served as the page draws them (an RGB tile's pixels, a channel's two byte planes); added again,
// the same artifacts; added without an alignment, not aligned; moved, it says where it was.
func TestPostXeniumImage(t *testing.T) {
	f := newRefineFixtureOf(t, false, 80, 60)
	dir := t.TempDir()
	copyFile := func(src, dst string) {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(dst, data, 0o644)
	}
	he := filepath.Join(dir, "S1_he_image.ome.tif")
	copyFile(filepath.Join("internal", "slide", "testdata", "he-deflate.ome.tif"), he)
	heAlign := filepath.Join(dir, "S1_he_imagealignment.csv")
	os.WriteFile(heAlign, []byte("2,0,10\n0,-2,140\n0,0,1\n"), 0o644)
	fl := filepath.Join(dir, "S1_if_image.ome.tif")
	copyFile(filepath.Join("internal", "slide", "testdata", "if-u16-deflate.ome.tif"), fl)

	// From this computer, as the page asks.
	local := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Host, req.RemoteAddr = "127.0.0.1:8840", "127.0.0.1:50000"
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		return rec
	}
	get := func(target string) *http.Response {
		return local(http.MethodGet, target, "").Result()
	}
	var checked struct {
		Image    slide.Description
		Siblings []string
	}
	json.NewDecoder(get("/api/local/0/images/check?path=" + url.QueryEscape(he)).Body).Decode(&checked)
	if checked.Image.Kind != slide.RGB || len(checked.Siblings) != 1 || checked.Siblings[0] != heAlign {
		t.Fatalf("checked: %+v", checked)
	}
	if rec := f.post("/api/local/0/images", fmt.Sprintf(`{"path":%q,"alignment":%q}`, he, heAlign)); rec.Code != http.StatusForbidden {
		t.Errorf("from another computer: %d", rec.Code)
	}
	// add answers the image's artifact and its alignment's ("" when none was supplied).
	add := func(image, alignment string) (artifacts.Manifest, string) {
		t.Helper()
		var started struct{ Job struct{ ID string } }
		json.NewDecoder(local(http.MethodPost, "/api/local/0/images", fmt.Sprintf(`{"path":%q,"alignment":%q}`, image, alignment)).Body).Decode(&started)
		j := f.wait(started.Job.ID)
		var id, tf string
		for _, e := range j.Events {
			if m := regexp.MustCompile(`artifact (img-[0-9a-f]+)`).FindStringSubmatch(e.Message); m != nil {
				id = m[1]
			}
			if m := regexp.MustCompile(`artifact (tf-[0-9a-f]+)`).FindStringSubmatch(e.Message); m != nil {
				tf = m[1]
			}
		}
		m, err := f.a.artifacts.Get(id)
		if err != nil {
			t.Fatalf("no image recorded: %v (%s)", err, j.Error)
		}
		return m, tf
	}
	m, tf := add(he, heAlign)
	byRole := map[string]artifacts.File{}
	for _, file := range m.Files {
		byRole[file.Role] = file
	}
	if m.Kind != "image" || m.State != artifacts.Complete || len(m.Files) != 2 || byRole["image"].Location != artifacts.InPlace || byRole["image"].Path != he || byRole["description"].Location != artifacts.InArtifact {
		t.Fatalf("the artifact: %+v", m)
	}
	if _, err := os.Stat(filepath.Join(f.a.artifacts.Dir(), m.ID, filepath.Base(he))); err == nil {
		t.Error("the image was copied")
	}
	// Its supplied alignment, its first transform: the matrix's pixels to µm, the file where it lies.
	tm, err := f.a.artifacts.Get(tf)
	if err != nil || tm.Kind != "transform" || tm.State != artifacts.Complete || len(tm.Files) != 2 || tm.Lineage.Parents[0].ID != m.ID {
		t.Fatalf("the alignment's artifact %s: %+v, %v", tf, tm, err)
	}
	var d imageDescription
	json.NewDecoder(get("/api/images/" + m.ID).Body).Decode(&d)
	var td transformDescription
	json.NewDecoder(get("/api/transforms/" + tf).Body).Decode(&td)
	ps := d.XeniumPixelSize
	want := [6]float64{2 * ps, 0, 10 * ps, 0, -2 * ps, 140 * ps}
	got := [6]float64{td.Transform.A, td.Transform.B, td.Transform.C, td.Transform.D, td.Transform.E, td.Transform.F}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Fatalf("the transform %v, not %v", got, want)
		}
	}
	if td.Method != "matrix" || td.File == nil || td.File.Path != heAlign || td.Source.Image != m.ID || td.Destination.Units != "µm" {
		t.Errorf("the alignment: %+v", td)
	}
	im, err := slide.Open(he)
	if err != nil {
		t.Fatal(err)
	}
	defer im.Close()
	direct, _ := im.Tile(1, 0, 1, 0)
	tile := request(t, f.handler, http.MethodGet, "/api/images/"+m.ID+"/tiles/1/0/1/0", nil, nil)
	if tile.Code != http.StatusOK || tile.Body.String() != string(direct) || tile.Header().Get("X-Tile-Width") != "16" || tile.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("an RGB tile: %d, %d bytes", tile.Code, tile.Body.Len())
	}
	if again, againTF := add(he, heAlign); again.ID != m.ID || againTF != tf {
		t.Errorf("added again: %s and %s, then %s and %s", m.ID, tf, again.ID, againTF)
	}

	// The immunofluorescence image, added without an alignment: not aligned. Three channels of 16
	// bits, each tile two byte planes, and the window each is first shown with.
	fm, ftf := add(fl, "")
	if ftf != "" {
		t.Errorf("an image added without an alignment has one: %s", ftf)
	}
	var fd imageDescription
	json.NewDecoder(get("/api/images/" + fm.ID).Body).Decode(&fd)
	if fd.Image.Kind != slide.Channels || len(fd.Ranges) != 3 || fd.Ranges[1][1] <= fd.Ranges[1][0] {
		t.Errorf("the IF image: %+v, ranges %v", fd.Image, fd.Ranges)
	}
	fim, _ := slide.Open(fl)
	defer fim.Close()
	samples, _ := fim.Tile(0, 2, 0, 0)
	planes := request(t, f.handler, http.MethodGet, "/api/images/"+fm.ID+"/tiles/0/2/0/0", nil, nil).Body.Bytes()
	n := len(samples) / 2
	if len(planes) != len(samples) || planes[5] != samples[10] || planes[n+5] != samples[11] {
		t.Errorf("a channel's tile: %d bytes for %d", len(planes), len(samples))
	}

	// Moved, the image is not where it was recorded.
	f.a.images = openImages{}
	os.Rename(he, he+".moved")
	if rec := request(t, f.handler, http.MethodGet, "/api/images/"+m.ID, nil, nil); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "not where it was recorded") {
		t.Errorf("moved: %d %s", rec.Code, rec.Body)
	}
}
