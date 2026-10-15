package segment

// The check a segmentation passes before it becomes an artifact (internal/artifacts): its files
// read, and agree with one another. A fatal finding makes the artifact invalid.
//
//   - metadata: the description names the method and its parameters, and its sizes are the files';
//   - geometry: the raster's rows cover it exactly and its labels are cells; each cell's area and
//     centroid are its pixels'; each cell with pixels has an outline of at least three finite
//     vertices inside the raster, whose area is near its traced pixels';
//   - assignments: one per transcript, each to a cell or none, and each cell's count of them is the
//     one it records;
//   - counts: the matrix's rows sum to each cell's gene transcripts, its gene indices are genes, and
//     each row's genes are the cell's genes detected;
//   - the optional files, when written: a probability per transcript, in [0, 1]; an outline for
//     every cell in every layer; diagnostics with a summary.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"situcraft/internal/artifacts"
)

// Summary is what the check reports of a segmentation (the artifact's summary).
type Summary struct {
	Cells              int             `json:"cells"`
	EmptyCells         int             `json:"emptyCells"`
	Q20GeneTranscripts int64           `json:"q20GeneTranscripts"`
	AssignedFraction   float64         `json:"assignedFraction"`
	MedianTranscripts  float64         `json:"medianTranscriptsPerCell"`
	MedianArea         float64         `json:"medianAreaUm2"`
	MultipieceCells    int             `json:"multipieceCells"`
	Method             string          `json:"method"`
	Parameters         json.RawMessage `json:"parameters"`
	Agreement          json.RawMessage `json:"agreement,omitempty"`
	// Diagnostics is diagnostics.json's summary, for methods that write it.
	Diagnostics json.RawMessage `json:"diagnostics,omitempty"`
}

// Check examines a staged segmentation in dir; it fills the manifest's summary.
func Check(dir string, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(code, format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: code, Message: fmt.Sprintf(format, args...)})
	}
	desc, err := ReadDescription(dir)
	if err != nil {
		fatal("segmentation.unreadable", "%v", err)
		return out
	}
	if desc.Method == "" || len(desc.Parameters) == 0 || !(desc.PixelSize > 0) {
		fatal("segmentation.metadata", "The description lacks the method, its parameters or the pixel size.")
	}
	cells, err := ReadCells(dir)
	if err != nil {
		fatal("segmentation.unreadable", "%v", err)
		return out
	}
	mask, err := ReadMask(dir)
	if err != nil {
		fatal("segmentation.unreadable", "%v", err)
		return out
	}
	assignments, err := ReadAssignments(dir)
	if err != nil {
		fatal("segmentation.unreadable", "%v", err)
		return out
	}
	counts, err := ReadCounts(dir)
	if err != nil {
		fatal("segmentation.unreadable", "%v", err)
		return out
	}
	outlines, err := ReadOutlines(dir)
	if err != nil {
		fatal("segmentation.unreadable", "%v", err)
		return out
	}
	n := len(cells)
	if desc.Cells != n || desc.Width != mask.Width || desc.Height != mask.Height || desc.PixelSize != mask.PixelSize || desc.Transcripts != len(assignments) || desc.Genes != len(counts.Genes) {
		fatal("segmentation.metadata", "The description's sizes differ from the files' (%d cells, %d × %d pixels, %d transcripts, %d genes).", n, mask.Width, mask.Height, len(assignments), len(counts.Genes))
	}

	// Geometry: the raster's labels are cells; each cell's area and centroid are its pixels'.
	badLabel := 0
	for _, row := range mask.Rows {
		for _, run := range row {
			if int(run.Label) > n {
				badLabel++
			}
		}
	}
	if badLabel > 0 {
		fatal("geometry.unknown-cell", "%d runs of the raster name cells that do not exist.", badLabel)
	}
	stats := mask.Stats(n)
	px2 := mask.PixelSize * mask.PixelSize
	areaOff, centroidOff, empty := 0, 0, 0
	for i, c := range cells {
		s := stats[i]
		if s.Pixels == 0 {
			empty++
			if c.Area != 0 {
				areaOff++
			}
			continue
		}
		if math.Abs(float64(c.Area)-float64(s.Pixels)*px2) > 1e-4*float64(s.Pixels)*px2+1e-6 {
			areaOff++
		}
		cx, cy := s.SumX/float64(s.Pixels)*mask.PixelSize, s.SumY/float64(s.Pixels)*mask.PixelSize
		if math.Abs(float64(c.X)-cx) > 1e-3 || math.Abs(float64(c.Y)-cy) > 1e-3 {
			centroidOff++
		}
	}
	if areaOff > 0 {
		fatal("geometry.area", "%d cells record an area other than their pixels'.", areaOff)
	}
	if centroidOff > 0 {
		fatal("geometry.centroid", "%d cells record a centroid other than their pixels'.", centroidOff)
	}
	if len(outlines) != n {
		fatal("geometry.outlines", "%d outlines for %d cells.", len(outlines), n)
	} else {
		bad, off, multi := 0, 0, 0
		extentX, extentY := float32(float64(mask.Width)*mask.PixelSize), float32(float64(mask.Height)*mask.PixelSize)
		for i, o := range outlines {
			if stats[i].Pixels == 0 {
				continue
			}
			if o.Pieces > 1 {
				multi++
			}
			valid := len(o.X) >= 3
			for k := range o.X {
				x, y := o.X[k], o.Y[k]
				if math.IsNaN(float64(x)) || math.IsNaN(float64(y)) || x < 0 || y < 0 || x > extentX || y > extentY {
					valid = false
				}
			}
			if !valid {
				bad++
				continue
			}
			// The simplified outline stays near the traced pixels' area (holes included).
			want := float64(o.Pixels+0) * px2
			if got := RingArea(o.X, o.Y); want >= 20*px2 && math.Abs(got-want) > 0.25*want {
				off++
			}
		}
		if bad > 0 {
			fatal("geometry.outline", "%d cells' outlines have fewer than three vertices, or vertices outside the raster.", bad)
		}
		if off > 0 {
			out = append(out, artifacts.Finding{Severity: "warning", Code: "geometry.outline-area", Message: "Outlines whose area differs by more than 25% from their pixels'", Count: off})
		}
		if multi > 0 {
			out = append(out, artifacts.Finding{Severity: "info", Code: "geometry.multipiece", Message: "Cells in more than one piece (the largest is outlined)", Count: multi})
		}
	}
	if empty > 0 {
		out = append(out, artifacts.Finding{Severity: "info", Code: "cells.empty", Message: "Cells with no pixels", Count: empty})
	}

	// Assignments: to cells or none; each cell's count is the one it records.
	perCell := make([]uint32, n)
	badAssign := 0
	var assigned int64
	for _, a := range assignments {
		if a == 0 {
			continue
		}
		if int(a) > n {
			badAssign++
			continue
		}
		perCell[a-1]++
		assigned++
	}
	if badAssign > 0 {
		fatal("assignments.unknown-cell", "%d transcripts are assigned to cells that do not exist.", badAssign)
	}
	countOff := 0
	for i, c := range cells {
		if perCell[i] != c.Assigned {
			countOff++
		}
	}
	if countOff > 0 {
		fatal("assignments.counts", "%d cells record another number of assigned transcripts than are assigned to them.", countOff)
	}

	// Counts: CSR in order, gene indices genes, rows summing to the cells' gene transcripts.
	if len(counts.Offsets) != n+1 || counts.Offsets[0] != 0 || int(counts.Offsets[n]) != len(counts.Gene) || len(counts.Count) != len(counts.Gene) {
		fatal("counts.shape", "The counts are not a matrix of %d cells.", n)
		return out
	}
	rowOff, geneOff, detectedOff := 0, 0, 0
	var q20Assigned int64
	totals := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		a, b := counts.Offsets[i], counts.Offsets[i+1]
		if b < a {
			fatal("counts.shape", "The counts' rows are out of order.")
			return out
		}
		var sum uint32
		for k := a; k < b; k++ {
			if int(counts.Gene[k]) >= len(counts.Genes) {
				geneOff++
			}
			sum += counts.Count[k]
		}
		q20Assigned += int64(sum)
		if sum != cells[i].Transcripts {
			rowOff++
		}
		if b-a != cells[i].Genes {
			detectedOff++
		}
		totals = append(totals, float64(sum))
	}
	if rowOff > 0 || geneOff > 0 || detectedOff > 0 {
		fatal("counts.cells", "The counts disagree with the cells: %d rows sum to another number of gene transcripts, %d entries name no gene, %d rows another number of genes.", rowOff, geneOff, detectedOff)
	}
	if q20Assigned != desc.Q20GeneAssigned {
		fatal("counts.total", "The counts hold %d gene transcripts; the description says %d are assigned.", q20Assigned, desc.Q20GeneAssigned)
	}
	// The optional files, when written: one probability per transcript, in [0, 1] (NaN for one
	// assigned by place); one boundary certainty per cell, in [0, 1] (NaN for one not drawn); an
	// outline for every cell in every layer; diagnostics with a summary.
	var diagnostics json.RawMessage
	if exists(dir, FileProbabilities) {
		p, err := ReadProbabilities(dir)
		switch {
		case err != nil:
			fatal("segmentation.unreadable", "%v", err)
		case len(p) != len(assignments):
			fatal("probabilities.count", "%d assignment probabilities for %d transcripts.", len(p), len(assignments))
		default:
			bad := 0
			for i, v := range p {
				if !math.IsNaN(float64(v)) && (v < 0 || v > 1 || assignments[i] != 0 && v == 0) {
					bad++
				}
			}
			if bad > 0 {
				fatal("probabilities.range", "%d assignment probabilities are outside [0, 1], or zero for an assigned transcript.", bad)
			}
		}
	}
	if exists(dir, FileCertainty) {
		c, err := ReadCertainty(dir)
		switch {
		case err != nil:
			fatal("segmentation.unreadable", "%v", err)
		case len(c) != n:
			fatal("certainty.count", "%d boundary certainties for %d cells.", len(c), n)
		default:
			bad := 0
			for _, v := range c {
				if !math.IsNaN(float64(v)) && (v < 0 || v > 1) {
					bad++
				}
			}
			if bad > 0 {
				fatal("certainty.range", "%d boundary certainties are outside [0, 1].", bad)
			}
		}
	}
	if exists(dir, FileLayers) {
		layers, err := ReadLayers(dir)
		switch {
		case err != nil:
			fatal("segmentation.unreadable", "%v", err)
		default:
			for i, l := range layers {
				if len(l.Outlines) != n || !(l.Z1 > l.Z0) {
					fatal("layers.shape", "Layer %d has %d outlines for %d cells, or no depth.", i+1, len(l.Outlines), n)
				}
			}
		}
	}
	if exists(dir, FileDiagnostics) {
		raw, err := os.ReadFile(filepath.Join(dir, FileDiagnostics))
		var d struct {
			Summary json.RawMessage `json:"summary"`
		}
		if err != nil || json.Unmarshal(raw, &d) != nil || len(d.Summary) == 0 {
			fatal("diagnostics.unreadable", "%s is not diagnostics with a summary.", FileDiagnostics)
		} else {
			diagnostics = d.Summary
		}
	}
	areas := make([]float64, 0, n)
	for i := range cells {
		if stats[i].Pixels > 0 {
			areas = append(areas, float64(cells[i].Area))
		}
	}
	s := Summary{Cells: n, EmptyCells: empty, Q20GeneTranscripts: desc.Q20GeneTranscripts, MedianTranscripts: median(totals), MedianArea: median(areas), Method: desc.Method, Parameters: desc.Parameters, Agreement: desc.Agreement, Diagnostics: diagnostics}
	if desc.Q20GeneTranscripts > 0 {
		s.AssignedFraction = math.Round(float64(q20Assigned)/float64(desc.Q20GeneTranscripts)*1e6) / 1e6
	}
	for _, o := range outlines {
		if o.Pieces > 1 {
			s.MultipieceCells++
		}
	}
	m.Summary, _ = json.Marshal(s)
	return out
}

// median of a slice (sorted in place); 0 when empty.
func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sortFloats(v)
	if len(v)%2 == 1 {
		return v[len(v)/2]
	}
	return (v[len(v)/2-1] + v[len(v)/2]) / 2
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}
