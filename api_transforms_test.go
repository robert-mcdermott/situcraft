package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/register"
)

// An image's alignments made here: landmarks fitted as a similarity, then a version of it as an
// affine transform (its parent recorded); each landmark's residual and leave-one-out error; the same
// landmarks again the same version; what cannot be fitted refused, saying why. Any version written
// as Xenium Explorer's matrix and keypoints, which read back as the same transform; Xenium
// Explorer's keypoints file read as a version, from this computer only.
func TestTransformsFromLandmarks(t *testing.T) {
	f := newRefineFixtureOf(t, false, 80, 60)
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("internal", "slide", "testdata", "he-deflate.ome.tif"))
	if err != nil {
		t.Fatal(err)
	}
	he := filepath.Join(dir, "S2_he_image.ome.tif")
	os.WriteFile(he, data, 0o644)
	local := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Host, req.RemoteAddr = "127.0.0.1:8840", "127.0.0.1:50000"
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		return rec
	}
	var started struct{ Job struct{ ID string } }
	json.NewDecoder(local(http.MethodPost, "/api/local/0/images", fmt.Sprintf(`{"path":%q}`, he)).Body).Decode(&started)
	j := f.wait(started.Job.ID)
	var img string
	for _, e := range j.Events {
		if m := regexp.MustCompile(`artifact (img-[0-9a-f]+)`).FindStringSubmatch(e.Message); m != nil {
			img = m[1]
		}
	}
	if img == "" {
		t.Fatalf("the image was not added: %s", j.Error)
	}

	// The image's pixels to µm: 0.36 µm each, mirrored and turned, as an H&E often is.
	truth := register.Affine{A: 0.36 * math.Cos(0.3), B: 0.36 * math.Sin(0.3), C: 12, D: 0.36 * math.Sin(0.3), E: -0.36 * math.Cos(0.3), F: 70}
	var landmarks []landmark
	for _, p := range [][2]float64{{3, 4}, {40, 7}, {9, 37}, {33, 30}, {20, 20}} {
		x, y := truth.Apply(p[0], p[1])
		landmarks = append(landmarks, landmark{Fixed: [2]float64{x, y}, Moving: p})
	}
	post := func(body any, wantCode int) (artifacts.Manifest, transformDescription, string) {
		t.Helper()
		raw, _ := json.Marshal(body)
		rec := local(http.MethodPost, "/api/images/"+img+"/transforms", string(raw))
		if rec.Code != wantCode {
			t.Fatalf("%s: %d %s", raw, rec.Code, rec.Body)
		}
		var answer struct {
			Manifest  artifacts.Manifest
			Transform transformDescription
		}
		json.Unmarshal(rec.Body.Bytes(), &answer)
		return answer.Manifest, answer.Transform, rec.Body.String()
	}
	m, d, _ := post(map[string]any{"landmarks": landmarks, "model": "similarity", "by": "RM", "note": "five nuclei"}, http.StatusOK)
	if m.Kind != "transform" || m.State != artifacts.Complete || d.Method != "landmarks" || d.By != "RM" || d.Note != "five nuclei" || d.Fit == nil || len(d.Fit.LeaveOneOut) != 5 || d.Fit.RMS > 1e-9 || d.Fit.LooMax > 1e-9 {
		t.Fatalf("landmarks: %+v %+v", m, d)
	}
	for _, pair := range [][2]float64{{d.Transform.A, truth.A}, {d.Transform.B, truth.B}, {d.Transform.C, truth.C}, {d.Transform.D, truth.D}, {d.Transform.E, truth.E}, {d.Transform.F, truth.F}} {
		if math.Abs(pair[0]-pair[1]) > 1e-9 {
			t.Fatalf("fitted %+v, made with %+v", d.Transform, truth)
		}
	}
	if again, _, _ := post(map[string]any{"landmarks": landmarks, "model": "similarity", "by": "RM", "note": "five nuclei"}, http.StatusOK); again.ID != m.ID {
		t.Errorf("the same landmarks again: %s, then %s", m.ID, again.ID)
	}
	// A version of it: the landmarks moved, fitted as an affine transform.
	moved := append([]landmark(nil), landmarks...)
	moved[4].Fixed[0] += 2
	child, cd, _ := post(map[string]any{"landmarks": moved, "model": "affine", "parent": m.ID, "by": "RM"}, http.StatusOK)
	if cd.Parent != m.ID || cd.Model != register.ModelAffine || child.Lineage.Parents[1].ID != m.ID || cd.Fit.LooMax < 1 {
		t.Errorf("the version: %+v, its fit %+v", child.Lineage.Parents, cd.Fit)
	}
	for _, c := range []struct {
		body any
		want string
	}{
		{map[string]any{"landmarks": landmarks[:2]}, "three or more"},
		{map[string]any{"landmarks": []landmark{{Moving: [2]float64{0, 0}}, {Moving: [2]float64{1, 1}, Fixed: [2]float64{1, 1}}, {Moving: [2]float64{2, 2}, Fixed: [2]float64{2, 2}}}, "model": "affine"}, "one line"},
		{map[string]any{"landmarks": landmarks, "model": "projective"}, "no model"},
		{map[string]any{"landmarks": landmarks, "parent": "tf-0000000000"}, "not an alignment of this image"},
	} {
		if _, _, body := post(c.body, http.StatusUnprocessableEntity); !strings.Contains(body, c.want) {
			t.Errorf("%v: %s", c.body, body)
		}
	}

	// Written for Xenium Explorer: the matrix in the Xenium image's pixels, the keypoints with them.
	export := func(id, which string) (*register.Alignment, string) {
		t.Helper()
		rec := request(t, f.handler, http.MethodGet, "/api/transforms/"+id+"/xenium-explorer/"+which, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", id, which, rec.Code, rec.Body)
		}
		al, err := register.ReadAlignment(rec.Body.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		return al, rec.Header().Get("Content-Disposition")
	}
	ps := d.XeniumPixelSize
	matrix, name := export(m.ID, "matrix")
	if !strings.Contains(name, "S2_he_imagealignment.csv") || math.Abs(matrix.Matrix.A*ps-d.Transform.A) > 1e-12 || math.Abs(matrix.Matrix.F*ps-d.Transform.F) > 1e-9 {
		t.Errorf("the matrix %+v (%s), of %+v", matrix.Matrix, name, d.Transform)
	}
	kps, name := export(m.ID, "keypoints")
	if !strings.Contains(name, "S2_he_keypoints.csv") || len(kps.Keypoints) != 5 || math.Abs(kps.Keypoints[1].Fixed.X*ps-landmarks[1].Fixed[0]) > 1e-9 || math.Abs(kps.Matrix.C*ps-d.Transform.C) > 1e-9 {
		t.Errorf("the keypoints %+v (%s)", kps, name)
	}

	// Xenium Explorer's keypoints, read as a version (their µm fitted), from this computer only.
	path := filepath.Join(dir, "S2_he_keypoints.csv")
	rec := request(t, f.handler, http.MethodGet, "/api/transforms/"+m.ID+"/xenium-explorer/keypoints", nil, nil)
	os.WriteFile(path, rec.Body.Bytes(), 0o644)
	if rec := f.post("/api/images/"+img+"/transforms", fmt.Sprintf(`{"alignment":%q}`, path)); rec.Code != http.StatusForbidden {
		t.Errorf("a file named from another computer: %d", rec.Code)
	}
	km, kd, _ := post(map[string]any{"alignment": path}, http.StatusOK)
	if kd.Method != "keypoints" || len(kd.Landmarks) != 5 || kd.File.Path != path || len(km.Files) != 2 || math.Abs(kd.Transform.A-d.Transform.A) > 1e-12 || kd.Fit.RMS > 1e-9 {
		t.Errorf("the keypoints file: %+v", kd)
	}
	if rec := request(t, f.handler, http.MethodGet, "/api/transforms/nothing/xenium-explorer/matrix", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("no such alignment: %d", rec.Code)
	}
}
