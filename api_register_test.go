package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"situcraft/internal/simulate"
)

// A simulated H&E added without its alignment and registered automatically, as a job: recorded as
// a version (its matches, the fit to those kept, its deformation and what the search found), within
// its truth's deformation; its deformation's inverse field undoes it; a registration started from a
// version names it as its parent and does not search; what cannot be registered is refused.
func TestRegisterImage(t *testing.T) {
	p := simulate.Presets["small"]
	p.WidthUm, p.HeightUm = 220, 180
	p.PostXenium = simulate.DefaultPostXenium()
	p.PostXenium.IF = nil
	f := newRefineFixtureWith(t, false, p)
	local := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Host, req.RemoteAddr = "127.0.0.1:8840", "127.0.0.1:50000"
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		return rec
	}
	logged := func(rec *httptest.ResponseRecorder, pattern string) string {
		t.Helper()
		var started struct{ Job struct{ ID string } }
		if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil || started.Job.ID == "" {
			t.Fatalf("no job: %d %s", rec.Code, rec.Body)
		}
		j := f.wait(started.Job.ID)
		for _, e := range j.Events {
			if m := regexp.MustCompile(pattern).FindStringSubmatch(e.Message); m != nil {
				return m[1]
			}
		}
		t.Fatalf("the job logged no %s", pattern)
		return ""
	}
	he := filepath.Join(f.folder, simulate.PostXeniumFolder, "simulated_he_image.ome.tif")
	img := logged(local(http.MethodPost, "/api/local/0/images", fmt.Sprintf(`{"path":%q}`, he)), `artifact (img-[0-9a-f]+)`)
	tf := logged(local(http.MethodPost, "/api/local/0/images/"+img+"/register", `{"model":"affine","nonrigid":true,"by":"RM"}`), `artifact (tf-[0-9a-f]+)`)
	var d transformDescription
	json.NewDecoder(request(t, f.handler, http.MethodGet, "/api/transforms/"+tf, nil, nil).Body).Decode(&d)
	kept := len(keptMatches(d.Matches))
	if d.Method != "automatic" || d.Model != "affine" || d.By != "RM" || kept < 12 || d.Fit == nil || len(d.Fit.Residuals) != kept || d.Registration == nil || d.Registration.Best == nil || !strings.Contains(d.Registration.Channel, "haematoxylin") {
		t.Fatalf("the version: %s, %s, by %s, %d kept, %+v", d.Method, d.Model, d.By, kept, d.Registration)
	}
	regs, err := simulate.ReadRegistrations(f.folder)
	if err != nil {
		t.Fatal(err)
	}
	r := regs[0]
	worst := 0.0
	for _, q := range [][2]float64{{300, 300}, {400, 250}, {250, 420}} {
		tx, ty := r.Map(q[0], q[1])
		x, y := d.Transform.Apply(q[0], q[1])
		if d.Deformation != nil {
			dx, dy := d.Deformation.At(x, y)
			x, y = x+dx, y+dy
		}
		worst = math.Max(worst, math.Hypot(x-tx, y-ty))
	}
	t.Logf("%s: %d of %d matches kept; deformed %v; at most %.2f µm from the truth at three points", d.Name, kept, len(d.Matches), d.Deformation != nil, worst)
	if worst > 1.5 || d.Deformation == nil {
		t.Errorf("%.2f µm from the truth; deformed %v", worst, d.Deformation != nil)
	}
	// The inverse field: each point, moved back by it, is deformed to the point again.
	if d.Deformation != nil {
		inv := request(t, f.handler, http.MethodGet, "/api/transforms/"+tf+"/inverse", nil, nil).Body.Bytes()
		nx, ny := int(binary.LittleEndian.Uint32(inv[4:])), int(binary.LittleEndian.Uint32(inv[8:]))
		x0 := float64(math.Float32frombits(binary.LittleEndian.Uint32(inv[12:])))
		y0 := float64(math.Float32frombits(binary.LittleEndian.Uint32(inv[16:])))
		step := float64(math.Float32frombits(binary.LittleEndian.Uint32(inv[20:])))
		if string(inv[:4]) != "SCIV" || len(inv) != 24+8*nx*ny {
			t.Fatalf("the inverse field: %q, %d × %d, %d bytes", inv[:4], nx, ny, len(inv))
		}
		for _, k := range [][2]int{{nx / 2, ny / 2}, {nx / 3, 2 * ny / 3}} {
			o := 24 + 8*(k[1]*nx+k[0])
			ex := float64(math.Float32frombits(binary.LittleEndian.Uint32(inv[o:])))
			ey := float64(math.Float32frombits(binary.LittleEndian.Uint32(inv[o+4:])))
			y := [2]float64{x0 + float64(k[0])*step, y0 + float64(k[1])*step}
			dx, dy := d.Deformation.At(y[0]+ex, y[1]+ey)
			if math.Hypot(y[0]+ex+dx-y[0], y[1]+ey+dy-y[1]) > 1e-3 {
				t.Errorf("the inverse at %v does not undo the deformation", y)
			}
		}
	} else if rec := request(t, f.handler, http.MethodGet, "/api/transforms/"+tf+"/inverse", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("an inverse field of no deformation: %d", rec.Code)
	}
	// Started from the version: its child, no search.
	child := logged(local(http.MethodPost, "/api/local/0/images/"+img+"/register", fmt.Sprintf(`{"model":"similarity","start":%q}`, tf)), `artifact (tf-[0-9a-f]+)`)
	var cd transformDescription
	json.NewDecoder(request(t, f.handler, http.MethodGet, "/api/transforms/"+child, nil, nil).Body).Decode(&cd)
	if cd.Parent != tf || cd.Model != "similarity" || cd.Registration.Best != nil || cd.By != "SituCraft" {
		t.Errorf("started from %s: parent %s, %s, search %+v, by %s", tf, cd.Parent, cd.Model, cd.Registration.Best, cd.By)
	}
	for _, c := range []struct{ body, want string }{
		{`{"model":"projective"}`, "no model"},
		{`{"model":"affine","start":"tf-0000000000"}`, "not an alignment of this image"},
	} {
		if rec := local(http.MethodPost, "/api/local/0/images/"+img+"/register", c.body); rec.Code < 400 || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: %d %s", c.body, rec.Code, rec.Body)
		}
	}
	if rec := local(http.MethodPost, "/api/local/0/images/img-0000000000/register", `{"model":"affine"}`); rec.Code != http.StatusNotFound {
		t.Errorf("no such image: %d", rec.Code)
	}

	// The version checked over the tissue (api_checks.go): within its truth's deformation, as the
	// check of its parent's version; checked again, the same check.
	chk := logged(local(http.MethodPost, "/api/local/0/images/"+img+"/check", fmt.Sprintf(`{"transform":%q}`, tf)), `artifact (chk-[0-9a-f]+)`)
	var c checkDescription
	json.NewDecoder(request(t, f.handler, http.MethodGet, "/api/checks/"+chk, nil, nil).Body).Decode(&c)
	cm, _ := f.a.artifacts.Get(chk)
	t.Logf("%s: %d of %d patches matched; off by %.3f µm (median), %.3f (90th percentile), at most %.3f; the matches' own error at most %.3f", c.Name, c.Matched, c.Checked, c.Median, c.P90, c.Max, c.Noise)
	if c.Format != "situcraft-alignment-check" || c.Transform != tf || c.Matched < 10 || c.Median > 1 || len(cm.Lineage.Parents) != 2 || cm.Lineage.Parents[1].ID != tf {
		t.Errorf("the check: %+v; its lineage %+v", c, cm.Lineage.Parents)
	}
	if again := logged(local(http.MethodPost, "/api/local/0/images/"+img+"/check", fmt.Sprintf(`{"transform":%q}`, tf)), `artifact (chk-[0-9a-f]+)`); again != chk {
		t.Errorf("checked again: %s, then %s", chk, again)
	}
	if rec := local(http.MethodPost, "/api/local/0/images/"+img+"/check", `{"transform":"tf-0000000000"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a check of no version: %d", rec.Code)
	}
	if rec := request(t, f.handler, http.MethodGet, "/api/checks/chk-0000000000", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("no such check: %d", rec.Code)
	}
}
