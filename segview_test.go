package main

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"situcraft/internal/cache"
	"situcraft/internal/jobs"
	"situcraft/internal/segment"
	"situcraft/internal/segment/refine"
)

// A refinement on the tissue (api_segview.go): its cells table, a cell's detail and its transcript
// tiles answer what its files hold; the instrument's segmentation is the dataset's own.
func TestASegmentationOnTheTissue(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	f := newRefineFixture(t, true)
	t.Cleanup(f.a.views.closeAll)
	var building struct{ Job *jobs.Job }
	json.NewDecoder(f.post("/api/local/0/cache", "").Body).Decode(&building)
	if building.Job != nil {
		f.wait(building.Job.ID)
	}
	m := f.refine(`{"sweeps":40,"samples":20,"seed":7}`)
	get := func(target string) []byte {
		t.Helper()
		rec := request(t, f.handler, http.MethodGet, target, nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, rec.Code, rec.Body)
		}
		return rec.Body.Bytes()
	}
	p, _ := f.a.artifacts.Path(m.ID, segment.FileCells)
	dir := filepath.Dir(p)
	cells, _ := segment.ReadCells(dir)
	assign, _ := segment.ReadAssignments(dir)
	prob, _ := segment.ReadProbabilities(dir)
	certainty, _ := segment.ReadCertainty(dir)
	counts, _ := segment.ReadCounts(dir)
	outlines, _ := segment.ReadOutlines(dir)
	// Limitations for the table to carry, whatever this short run flagged.
	var diag refine.Diagnostics
	raw, _ := os.ReadFile(filepath.Join(dir, segment.FileDiagnostics))
	json.Unmarshal(raw, &diag)
	diag.Limitations.Uncertain = append(diag.Limitations.Uncertain, cells[1].ID)
	diag.Limitations.LowTranscripts = append(diag.Limitations.LowTranscripts, cells[2].ID)
	diag.Limitations.NoNucleus = append(diag.Limitations.NoNucleus, cells[2].ID)
	raw, _ = json.Marshal(diag)
	os.WriteFile(filepath.Join(dir, segment.FileDiagnostics), raw, 0o644)
	d, ok := f.a.views.path(f.folder)
	if !ok {
		get("/api/local/0/view")
		d, _ = f.a.views.path(f.folder)
	}

	// The cells table.
	table := get("/api/local/0/segmentations/" + m.ID + "/cells")
	le := binary.LittleEndian
	n := int(le.Uint32(table[4:]))
	if string(table[:4]) != "SCVS" || n != len(cells) || le.Uint32(table[8:]) != 7 || le.Uint32(table[12:]) != 11 {
		t.Fatalf("table header %q %d %d %d", table[:4], n, le.Uint32(table[8:]), le.Uint32(table[12:]))
	}
	column := func(k int) []byte { return table[16+4*n*k : 16+4*n*(k+1)] }
	f32 := func(k, i int) float32 { return math.Float32frombits(le.Uint32(column(k)[4*i:])) }
	u32 := func(k, i int) uint32 { return le.Uint32(column(k)[4*i:]) }
	flags := table[16+4*n*10 : 16+4*n*10+n]
	flagged := map[string]uint8{}
	for _, id := range diag.Limitations.LowTranscripts {
		flagged[id] |= 1
	}
	for _, id := range diag.Limitations.Uncertain {
		flagged[id] |= 2
	}
	for _, id := range diag.Limitations.NoNucleus {
		flagged[id] |= 4
	}
	sum, count := make([]float64, n), make([]int, n)
	for row, c := range assign {
		if c > 0 && !math.IsNaN(float64(prob[row])) {
			sum[c-1] += float64(prob[row])
			count[c-1]++
		}
	}
	same := func(a, b float32) bool { return a == b || math.IsNaN(float64(a)) && math.IsNaN(float64(b)) }
	for i, c := range cells {
		row, ok := d.CellRow(c.Nucleus)
		instrument := uint32(cache.NoCell)
		if ok {
			instrument = uint32(row)
		}
		mean := float32(sum[i] / float64(count[i]))
		if f32(0, i) != c.X || f32(1, i) != c.Y || f32(2, i) != c.Area || f32(3, i) != c.NucleusArea || u32(4, i) != c.Transcripts || u32(5, i) != c.Assigned ||
			u32(6, i) != c.Genes || u32(7, i) != instrument || !same(f32(8, i), certainty[i]) || !same(f32(9, i), mean) || flags[i] != flagged[c.ID] {
			t.Fatalf("cell %d (%s) in the table differs from the files", i, c.ID)
		}
	}

	// A cell's detail, by its id: one flagged with two limitations.
	id := cells[2].ID
	var detail segCellDetail
	json.Unmarshal(get("/api/local/0/segmentations/"+m.ID+"/cells/id:"+id+"?top=3"), &detail)
	c := cells[detail.Row]
	total := 0
	for k := counts.Offsets[detail.Row]; k < counts.Offsets[detail.Row+1]; k++ {
		total += int(counts.Count[k])
	}
	top := 0
	for i, g := range detail.Genes {
		top += g.Count
		if i > 0 && g.Count > detail.Genes[i-1].Count || g.Gene < 0 || d.Transcripts.Summary.Genes[g.Gene].Name != g.Name {
			t.Errorf("top genes %+v", detail.Genes)
		}
	}
	if detail.ID != id || detail.Transcripts != int(c.Transcripts) || total != int(c.Transcripts) || top > total || len(detail.Genes) > 3 ||
		detail.Segmentation.ID != m.ID || detail.Instrument == nil || detail.Instrument.ID != c.Nucleus || len(detail.Boundaries) != 1 ||
		len(detail.Boundaries[0]) != 2*len(outlines[detail.Row].X) || len(detail.Nuclei) == 0 {
		t.Errorf("detail %+v", detail)
	}
	if len(detail.Limitations) != 2 || detail.Limitations[0] != "few transcripts" || detail.Limitations[1] != "no nucleus" || detail.BoundaryCertainty == nil || detail.MeanProbability == nil {
		t.Errorf("a flagged cell: limitations %v, certainty %v, mean probability %v", detail.Limitations, detail.BoundaryCertainty, detail.MeanProbability)
	}

	// A transcript tile: each transcript's cell and probability in the segmentation, in the
	// dataset's tile's order.
	tile := get("/api/local/0/segmentations/" + m.ID + "/points/0/0?minq=0")
	pts, err := d.TilePoints(0, 0, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	np := int(le.Uint32(tile[4:]))
	genes := int(le.Uint32(tile[8:]))
	if string(tile[:4]) != "SCVP" || np != len(pts.Row) || le.Uint32(tile[12:]) != 1 || np == 0 {
		t.Fatalf("tile header %q %d %d", tile[:4], np, le.Uint32(tile[12:]))
	}
	at := 16 + 4*(genes+1) + 8*np
	end := at + 4*np + 2*genes
	end = (end + 3) &^ 3
	for i, row := range pts.Row {
		want := uint32(cache.NoCell)
		if assign[row] > 0 {
			want = assign[row] - 1
		}
		if le.Uint32(tile[at+4*i:]) != want || !same(math.Float32frombits(le.Uint32(tile[end+4*i:])), prob[row]) {
			t.Fatalf("point %d (row %d): cell %d, probability %g", i, row, le.Uint32(tile[at+4*i:]), math.Float32frombits(le.Uint32(tile[end+4*i:])))
		}
	}

	// The instrument's segmentation is the dataset's own; an unknown one is not found.
	all, _ := f.a.artifacts.List()
	for _, a := range all {
		if a.Kind == "segmentation" && isBundleOwn(a) {
			if rec := request(t, f.handler, http.MethodGet, "/api/local/0/segmentations/"+a.ID+"/cells", nil, nil); rec.Code != http.StatusBadRequest {
				t.Errorf("the instrument's: %d", rec.Code)
			}
		}
	}
	if rec := request(t, f.handler, http.MethodGet, "/api/local/0/segmentations/nonesuch/cells", nil, nil); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown segmentation: %d", rec.Code)
	}
}
