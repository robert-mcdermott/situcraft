package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
	"situcraft/internal/spatial"
	"situcraft/internal/xenium"
)

// chosenCells writes cells as the page sends them: the bits lib/filters.js's keepChecksum hashes,
// and their checksum.
func chosenCells(keep []bool, extra string) string {
	raw := make([]byte, 4+(len(keep)+7)/8)
	binary.LittleEndian.PutUint32(raw, uint32(len(keep)))
	for i, k := range keep {
		if k {
			raw[4+i/8] |= 1 << (i % 8)
		}
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf(`{"keep":%q,"checksum":%q%s}`, base64.StdEncoding.EncodeToString(raw), hex.EncodeToString(sum[:]), extra)
}

// A graph of the instrument's cells of a small simulated tissue, by each kind: made by a job and
// recorded with its parameters and the cells chosen; asked again, answered at once; its edges as the
// page reads them, the graph's own; what cannot be built refused.
func TestGraphsOfASegmentation(t *testing.T) {
	f := newRefineFixture(t, true)
	all, _ := f.a.artifacts.List()
	var seg artifacts.Manifest
	for _, a := range all {
		if a.Kind == "segmentation" && isBundleOwn(a) {
			seg = a
		}
	}
	b, err := xenium.Open(f.folder, xenium.Options{CacheDir: f.a.cacheDir})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	segDir, err := f.a.segmentationFolder(context.Background(), b, seg)
	if err != nil {
		t.Fatal(err)
	}
	cells, err := segment.ReadCells(segDir)
	if err != nil || len(cells) < 50 {
		t.Fatalf("the segmentation's cells: %d, %v", len(cells), err)
	}
	keep := make([]bool, len(cells))
	for i := range keep {
		keep[i] = i%10 != 3
	}
	chosen := chosenCells(keep, `,"regions":[{"id":"reg-1","name":"Tumor nest","version":2}],"excluded":4`)
	start := func(params string) (string, *artifacts.Manifest) {
		t.Helper()
		rec := f.post("/api/local/0/graph", fmt.Sprintf(`{"segmentation":%q,"parameters":%s,"cells":%s}`, seg.ID, params, chosen))
		var answer struct {
			ID       string
			Artifact *artifacts.Manifest
			Job      *struct{ ID string }
		}
		if err := json.NewDecoder(rec.Body).Decode(&answer); err != nil || answer.ID == "" {
			t.Fatalf("%s: %d", params, rec.Code)
		}
		if answer.Job != nil {
			f.wait(answer.Job.ID)
		}
		m, err := f.a.artifacts.Get(answer.ID)
		if err != nil || m.State != artifacts.Complete {
			t.Fatalf("%s: the graph %s: %v %+v", params, answer.ID, err, m.Findings)
		}
		return answer.ID, answer.Artifact
	}
	for _, params := range []string{`{"kind":"delaunay","longest":40}`, `{"kind":"knn","k":5}`, `{"kind":"radius","radius":20}`, `{"kind":"contact","gap":1}`} {
		id, _ := start(params)
		m, _ := f.a.artifacts.Get(id)
		var s spatial.Summary
		json.Unmarshal(m.Summary, &s)
		inGraph := 0
		for _, k := range keep {
			if k {
				inGraph++
			}
		}
		if m.Kind != "graph" || s.Cells != inGraph || s.Edges == 0 || len(s.Choice.Regions) != 1 || s.Choice.Excluded != 4 || m.Lineage.Parents[len(m.Lineage.Parents)-1].ID != seg.ID {
			t.Errorf("%s: %s, %+v", params, m.Name, s)
		}
		if _, again := start(params); again == nil || again.ID != id {
			t.Errorf("%s asked again: not answered at once", params)
		}
		// The edges as the page reads them.
		rec := request(t, f.handler, http.MethodGet, "/api/graphs/"+id+"/edges", nil, nil)
		raw := rec.Body.Bytes()
		n, ends := int(binary.LittleEndian.Uint32(raw)), int(binary.LittleEndian.Uint32(raw[4:]))
		shared := raw[8] == 1
		size := 12 + 12*n + 4*(n+1) + 8*ends
		if shared {
			size += 4 * ends
		}
		if rec.Code != http.StatusOK || n != inGraph || ends != 2*s.Edges || len(raw) != size || shared != strings.Contains(params, "contact") {
			t.Fatalf("%s: the edges: %d, %d cells, %d ends, %d bytes", params, rec.Code, n, ends, len(raw))
		}
		rows := raw[12:]
		first := binary.LittleEndian.Uint32(rows)
		x0 := math.Float32frombits(binary.LittleEndian.Uint32(rows[4*n:]))
		offsets := rows[12*n:]
		neighbors := offsets[4*(n+1):]
		lengths := neighbors[4*ends:]
		j := binary.LittleEndian.Uint32(neighbors)
		row := binary.LittleEndian.Uint32(rows[4*j:])
		l := math.Float32frombits(binary.LittleEndian.Uint32(lengths))
		dx, dy := float64(cells[row].X-cells[first].X), float64(cells[row].Y-cells[first].Y)
		if binary.LittleEndian.Uint32(offsets) != 0 || math.Abs(float64(l)-math.Hypot(dx, dy)) > 1e-3 || first != 0 || x0 != cells[0].X {
			t.Errorf("%s: the first edge, %d to %d, %g µm", params, first, row, l)
		}
	}
	for _, c := range []struct{ body, want string }{
		{fmt.Sprintf(`{"segmentation":%q,"parameters":{"kind":"voronoi"},"cells":%s}`, seg.ID, chosen), "no graph of kind"},
		{fmt.Sprintf(`{"segmentation":%q,"parameters":{"kind":"knn","k":0},"cells":%s}`, seg.ID, chosen), "k is 1 to 100"},
		{fmt.Sprintf(`{"segmentation":%q,"cells":%s}`, seg.ID, chosenCells(keep[:10], "")), "the segmentation has"},
		{fmt.Sprintf(`{"segmentation":%q}`, seg.ID), "Send"},
	} {
		if rec := f.post("/api/local/0/graph", c.body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: %d %s", c.body, rec.Code, rec.Body)
		}
	}
	if rec := request(t, f.handler, http.MethodGet, "/api/graphs/gra-0000000000/edges", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("no such graph: %d", rec.Code)
	}
}
