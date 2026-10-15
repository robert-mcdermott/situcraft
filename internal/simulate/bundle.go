package simulate

// Writing a simulated tissue as a Xenium-shaped output bundle, and its truth as an output bundle
// of the same run:
//
//	experiment.xenium, gene_panel.json, metrics_summary.csv
//	transcripts.parquet       every molecule observed: its id, the instrument's cell, whether it
//	                          overlaps a nucleus, its gene, its place (µm) and its quality
//	cells.parquet             the instrument's cells; cell_ and nucleus_boundaries.parquet
//	cell_feature_matrix/      their counts of molecules of quality 20 or more, Matrix Market
//	cells.zarr.zip            the nucleus and cell masks, the µm-to-pixel transform, the cells'
//	                          ids and segmentation methods
//	morphology_focus/morphology_focus_0000.ome.tif   the DAPI stain (deflate, with its pyramid)
//	situcraft-simulation.json the simulation: its parameters, seed and what it made
//	truth/                    the true segmentation, shaped as a Xenium Ranger output bundle of
//	                          the same run (SituCraft imports it to score any segmentation): every
//	                          true cell (with its type), its outline and nucleus, the masks, and
//	                          every molecule's true cell, true place, origin and displacement
//
// Every file is written the same for the same simulation: no times, fixed orders.

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"situcraft/internal/hdf5"
	"situcraft/internal/parquet"
	"situcraft/internal/segment"
	"situcraft/internal/segment/expand"
	"situcraft/internal/xenium"
	"situcraft/internal/zarr"
)

// MethodName is the simulated instrument's segmentation method, as Xenium names its methods.
func (p Params) methodName() string {
	return fmt.Sprintf("Segmented by nucleus expansion of %.1fµm", p.ExpansionUm)
}

// trueSalt makes the true cells' ids differ from the instrument's.
const trueSalt = 0x5eed7a11

// analysisSoftware is experiment.xenium's analysis_sw_version: the layout the files follow, as
// Xenium Onboard Analysis names its versions (readers choose how to read a bundle by it), then
// what wrote them.
func analysisSoftware(software string) string {
	return "xenium-2.0.0 layout, situcraft-simulate " + software
}

// cellID is a Xenium-style cell id: a distinct 32-bit prefix for each index (multiplying by an
// odd constant permutes them), salted by what the ids belong to.
func cellID(i int, salt uint32) (uint32, string) {
	prefix := uint32(i+1)*0x9e3779b1 ^ salt
	return prefix, xenium.CellIDString(prefix, 1)
}

// uuidOf is a UUID (version 8, RFC 9562's custom form) made from a text, so the same simulation
// names the same run.
func uuidOf(text string) string {
	h := sha256.Sum256([]byte(text))
	h[6] = h[6]&0x0f | 0x80
	h[8] = h[8]&0x3f | 0x80
	x := hex.EncodeToString(h[:16])
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

// written is what the bundle holds, for the summary and the metrics.
type written struct {
	instrumentCells   int
	transcripts       int
	q20Genes          int64
	q20GenesAssigned  int64
	q20GenesNuclear   int64
	genesAnyQuality   int64
	nucleusPixels     int64
	cellArea          float64
	transcriptCounts  []int64
	controlCounts     []int64
	genesPerCell      []int64
	trueCellsByType   map[string]int
	visibleNuclei     int
	moleculesByOrigin map[string]int
	wide              int
	lost              int
}

type writer struct {
	t        *tissue
	in       *instrument
	pn       *panel
	mols     []molecule
	dir      string
	software string
	run      map[string]any
	instID   []string // instrument cell ids by instrument label − 1
	trueID   []string // true cell ids by true label − 1
	inst     *segmentation
	w        written
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// pixelOf is the pixel holding a point (clamped to the image).
func (wr *writer) pixelOf(x, y float32) int {
	ps := wr.t.p.PixelUm
	px := min(wr.t.w-1, max(0, int(float64(x)/ps)))
	py := min(wr.t.h-1, max(0, int(float64(y)/ps)))
	return py*wr.t.w + px
}

func (wr *writer) writeAll(progress func(float64, string)) error {
	p := wr.t.p
	for i := range wr.in.cells {
		_, id := cellID(i, 0)
		wr.instID = append(wr.instID, id)
	}
	for i := range wr.t.cells {
		_, id := cellID(i, trueSalt)
		wr.trueID = append(wr.trueID, id)
	}
	params, _ := json.Marshal(p)
	experiment := uuidOf("situcraft simulation experiment\n" + string(params))
	wr.run = map[string]any{
		"major_version": 5, "minor_version": 0, "patch_version": 0,
		"run_name": "SituCraft simulation", "region_name": fmt.Sprintf("simulated tissue, seed %d", p.Seed),
		"preservation_method": "", "panel_name": "SituCraft simulated panel", "panel_design_id": "situcraft-simulated",
		"panel_organism": "Simulated", "panel_tissue_type": "", "pixel_size": p.PixelUm,
		"instrument_sn": "simulated", "instrument_sw_version": "simulated", "analysis_sw_version": analysisSoftware(wr.software),
		"experiment_uuid": experiment, "z_step_size": zStepUm, "segmentation_stain": "DAPI",
		"images": map[string]any{"morphology_focus_filepath": "morphology_focus/morphology_focus_0000.ome.tif"},
	}
	steps := []struct {
		what string
		do   func() error
	}{
		{"Writing the transcripts", wr.writeTranscripts},
		{"Writing the instrument's cells", wr.writeInstrument},
		{"Writing the counts", wr.writeMatrix},
		{"Writing the masks", func() error {
			return wr.writeCellsZarr(filepath.Join(wr.dir, "cells.zarr.zip"), wr.inst)
		}},
		{"Writing the stain", wr.writeImage},
		{"Writing the truth", wr.writeTruth},
		{"Writing the description", wr.writeDescription},
	}
	for i, s := range steps {
		progress(float64(i)/float64(len(steps)), s.what)
		if err := s.do(); err != nil {
			return fmt.Errorf("%s: %w", strings.ToLower(s.what), err)
		}
	}
	return nil
}

// parquetFile writes rows in row groups of a million.
func parquetFile(path string, fields []parquet.Field, software string, rows int, column func(from, to int) []*parquet.Values) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	buf := bufio.NewWriterSize(f, 1<<20)
	w, err := parquet.NewWriter(buf, fields, software)
	if err != nil {
		f.Close()
		return err
	}
	for from := 0; from < rows || from == 0; from += 1 << 20 {
		to := min(rows, from+1<<20)
		if err := w.WriteRowGroup(column(from, to)); err != nil {
			f.Close()
			return err
		}
		if to == rows {
			break
		}
	}
	if err := w.Close(); err != nil {
		f.Close()
		return err
	}
	if err := buf.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func text(s string) parquet.Field {
	return parquet.Field{Name: s, Type: parquet.ByteArray, Annotation: "string"}
}
func f32(s string) parquet.Field { return parquet.Field{Name: s, Type: parquet.Float} }
func f64(s string) parquet.Field { return parquet.Field{Name: s, Type: parquet.Double} }
func i64(s string) parquet.Field { return parquet.Field{Name: s, Type: parquet.Int64} }
func u32(s string) parquet.Field {
	return parquet.Field{Name: s, Type: parquet.Int32, Annotation: "uint"}
}
func u8(s string) parquet.Field {
	return parquet.Field{Name: s, Type: parquet.Int32, Annotation: "uint8"}
}
func i32(s string) parquet.Field { return parquet.Field{Name: s, Type: parquet.Int32} }
func u64id(s string) parquet.Field {
	return parquet.Field{Name: s, Type: parquet.Int64, Annotation: "uint"}
}

// fovs order molecules as an instrument's fields of view do: by 250 µm squares, row by row, each
// square's molecules in the order they were drawn.
const fovUm = 250.0

func (wr *writer) order() {
	cols := int(math.Ceil(wr.t.p.WidthUm / fovUm))
	key := func(m molecule) int { return int(float64(m.y)/fovUm)*cols + int(float64(m.x)/fovUm) }
	sort.SliceStable(wr.mols, func(i, j int) bool { return key(wr.mols[i]) < key(wr.mols[j]) })
}

// ids are the molecules' ids, each its field of view (from 1) in the top 16 bits and its place
// in it below, and their fields of view's names (a letter for the row, a number for the column).
func (wr *writer) ids() ([]uint64, [][]byte) {
	cols := int(math.Ceil(wr.t.p.WidthUm / fovUm))
	out := make([]uint64, len(wr.mols))
	names := make([][]byte, len(wr.mols))
	count := map[int]uint64{}
	named := map[int][]byte{}
	for i, m := range wr.mols {
		row, col := int(float64(m.y)/fovUm), int(float64(m.x)/fovUm)
		fov := row*cols + col
		out[i] = uint64(fov+1)<<48 | count[fov]
		count[fov]++
		if named[fov] == nil {
			named[fov] = []byte(fovName(row, col))
		}
		names[i] = named[fov]
	}
	return out, names
}

// fovName names a field of view: rows A–Z, then AA, AB, …; columns from 1.
func fovName(row, col int) string {
	letters := ""
	for r := row + 1; r > 0; r = (r - 1) / 26 {
		letters = string(rune('A'+(r-1)%26)) + letters
	}
	return letters + strconv.Itoa(col+1)
}

func (wr *writer) writeTranscripts() error {
	wr.order()
	ids, fovs := wr.ids()
	genes := wr.pn.genes
	instCell := make([]uint32, len(wr.mols))
	nuclear := make([]bool, len(wr.mols))
	for i, m := range wr.mols {
		px := wr.pixelOf(m.x, m.y)
		instCell[i] = wr.in.labels[px]
		nuclear[i] = wr.in.nuclei[px] != 0
		if int(m.feature) < genes {
			wr.w.genesAnyQuality++
			if m.qv >= 20 {
				wr.w.q20Genes++
				if instCell[i] != 0 {
					wr.w.q20GenesAssigned++
				}
				if nuclear[i] {
					wr.w.q20GenesNuclear++
				}
			}
		}
	}
	wr.w.transcripts = len(wr.mols)
	names := make([][]byte, len(wr.pn.names))
	for i, n := range wr.pn.names {
		names[i] = []byte(n)
	}
	unassigned := []byte("UNASSIGNED")
	inst := make([][]byte, len(wr.instID))
	for i, id := range wr.instID {
		inst[i] = []byte(id)
	}
	tru := make([][]byte, len(wr.trueID))
	for i, id := range wr.trueID {
		tru[i] = []byte(id)
	}
	// Each molecule's distance to the nearest nucleus in the section (µm, from its pixel's centre
	// to the nearest nuclear pixel's; 0 over a nucleus), as Xenium records it.
	near := expand.SquaredDistances(wr.in.nuclei, wr.t.w, wr.t.h, runtime.NumCPU())
	// The columns of Xenium Onboard Analysis 2, in its order; cell_id is each file's own.
	common := func(c []*parquet.Values, from, to int) {
		for i := from; i < to; i++ {
			m := wr.mols[i]
			c[0].Int64 = append(c[0].Int64, int64(ids[i]))
			n := int32(0)
			if nuclear[i] {
				n = 1
			}
			c[2].Int32 = append(c[2].Int32, n)
			c[3].Bytes = append(c[3].Bytes, names[m.feature])
			c[4].Float32 = append(c[4].Float32, m.x)
			c[5].Float32 = append(c[5].Float32, m.y)
			c[6].Float32 = append(c[6].Float32, m.z)
			c[7].Float32 = append(c[7].Float32, m.qv)
			c[8].Bytes = append(c[8].Bytes, fovs[i])
			d := float32(math.NaN())
			if d2 := near[wr.pixelOf(m.x, m.y)]; d2 != math.MaxInt64 {
				d = float32(math.Sqrt(float64(d2)) * wr.t.p.PixelUm)
			}
			c[9].Float32 = append(c[9].Float32, d)
			c[10].Int32 = append(c[10].Int32, int32(m.feature)+1) // gene_panel.json's codeword
		}
	}
	fields := []parquet.Field{u64id("transcript_id"), text("cell_id"), u8("overlaps_nucleus"), text("feature_name"), f32("x_location"), f32("y_location"), f32("z_location"), f32("qv"), text("fov_name"),
		f32("nucleus_distance"), i32("codeword_index")}
	err := parquetFile(filepath.Join(wr.dir, "transcripts.parquet"), fields, wr.software, len(wr.mols), func(from, to int) []*parquet.Values {
		c := make([]*parquet.Values, len(fields))
		for i := range c {
			c[i] = &parquet.Values{}
		}
		common(c, from, to)
		for i := from; i < to; i++ {
			if l := instCell[i]; l != 0 {
				c[1].Bytes = append(c[1].Bytes, inst[l-1])
			} else {
				c[1].Bytes = append(c[1].Bytes, unassigned)
			}
		}
		return c
	})
	if err != nil {
		return err
	}
	// The truth's transcripts: each molecule's true cell, true place, origin and displacement.
	if err := os.MkdirAll(filepath.Join(wr.dir, "truth"), 0o755); err != nil {
		return err
	}
	origins := [][]byte{[]byte("cell"), []byte("background"), []byte("control probe"), []byte("control codeword")}
	truthFields := append(fields[:len(fields):len(fields)], f32("true_x_location"), f32("true_y_location"), f32("true_z_location"), text("origin"), u32("wide_displacement"))
	return parquetFile(filepath.Join(wr.dir, "truth", "transcripts.parquet"), truthFields, wr.software, len(wr.mols), func(from, to int) []*parquet.Values {
		c := make([]*parquet.Values, len(truthFields))
		for i := range c {
			c[i] = &parquet.Values{}
		}
		common(c, from, to)
		for i := from; i < to; i++ {
			m := wr.mols[i]
			if m.cell != 0 {
				c[1].Bytes = append(c[1].Bytes, tru[m.cell-1])
			} else {
				c[1].Bytes = append(c[1].Bytes, unassigned)
			}
			c[11].Float32 = append(c[11].Float32, m.tx)
			c[12].Float32 = append(c[12].Float32, m.ty)
			c[13].Float32 = append(c[13].Float32, m.tz)
			c[14].Bytes = append(c[14].Bytes, origins[m.origin])
			wide := int32(0)
			if m.wide {
				wide = 1
			}
			c[15].Int32 = append(c[15].Int32, wide)
		}
		return c
	})
}

// raster is a label grid as SituCraft's segmentation raster, for tracing outlines.
func (wr *writer) raster(labels []uint32) *segment.Raster {
	r := &segment.Raster{Width: wr.t.w, Height: wr.t.h, PixelSize: wr.t.p.PixelUm, Rows: make([][]segment.Run, wr.t.h)}
	for y := 0; y < wr.t.h; y++ {
		r.Rows[y] = segment.EncodeRow(labels[y*wr.t.w : (y+1)*wr.t.w])
	}
	return r
}

// shapes are each label's pixels, area (µm²) and centroid (µm).
type shape struct {
	pixels int64
	sx, sy float64
}

func (wr *writer) shapes(labels []uint32, n int) []shape {
	out := make([]shape, n)
	for i, l := range labels {
		if l == 0 {
			continue
		}
		s := &out[l-1]
		s.pixels++
		s.sx += float64(i%wr.t.w) + 0.5
		s.sy += float64(i/wr.t.w) + 0.5
	}
	return out
}

func (wr *writer) writeInstrument() error {
	p := wr.t.p
	ps := p.PixelUm
	n := len(wr.in.cells)
	wr.w.instrumentCells = n
	wr.inst = wr.instrumentSegmentation()
	seg := wr.inst
	// Each cell's counts of molecules of quality 20 or more, by feature kind.
	genes := make([]int64, n)
	probes, codewords := make([]int64, n), make([]int64, n)
	perGene := make([]map[int32]bool, n)
	for _, m := range wr.mols {
		l := wr.in.labels[wr.pixelOf(m.x, m.y)]
		if l == 0 || m.qv < 20 {
			continue
		}
		switch f := int(m.feature); {
		case f < wr.pn.genes:
			genes[l-1]++
			if perGene[l-1] == nil {
				perGene[l-1] = map[int32]bool{}
			}
			perGene[l-1][m.feature] = true
		case f < wr.pn.genes+p.Controls:
			probes[l-1]++
		default:
			codewords[l-1]++
		}
	}
	wr.w.transcriptCounts, wr.w.controlCounts = genes, probes
	for k := 0; k < n; k++ {
		wr.w.genesPerCell = append(wr.w.genesPerCell, int64(len(perGene[k])))
		wr.w.nucleusPixels += seg.nucleusShape(k).pixels
	}
	px2 := ps * ps
	fields := []parquet.Field{text("cell_id"), f64("x_centroid"), f64("y_centroid"), i64("transcript_counts"), i64("control_probe_counts"), i64("control_codeword_counts"),
		i64("unassigned_codeword_counts"), i64("deprecated_codeword_counts"), i64("total_counts"), f64("cell_area"), f64("nucleus_area"), i64("nucleus_count"), text("segmentation_method")}
	method := []byte(p.methodName())
	err := parquetFile(filepath.Join(wr.dir, "cells.parquet"), fields, wr.software, n, func(from, to int) []*parquet.Values {
		c := make([]*parquet.Values, len(fields))
		for i := range c {
			c[i] = &parquet.Values{}
		}
		for k := from; k < to; k++ {
			s, ns := seg.cellShapes[k], seg.nucleusShape(k)
			area := mul(float64(s.pixels), px2)
			wr.w.cellArea += area
			c[0].Bytes = append(c[0].Bytes, []byte(wr.instID[k]))
			c[1].Float64 = append(c[1].Float64, s.sx/float64(s.pixels)*ps)
			c[2].Float64 = append(c[2].Float64, s.sy/float64(s.pixels)*ps)
			c[3].Int64 = append(c[3].Int64, genes[k])
			c[4].Int64 = append(c[4].Int64, probes[k])
			c[5].Int64 = append(c[5].Int64, codewords[k])
			c[6].Int64 = append(c[6].Int64, 0)
			c[7].Int64 = append(c[7].Int64, 0)
			c[8].Int64 = append(c[8].Int64, genes[k]+probes[k]+codewords[k])
			c[9].Float64 = append(c[9].Float64, area)
			c[10].Float64 = append(c[10].Float64, float64(ns.pixels)*px2)
			c[11].Int64 = append(c[11].Int64, 1)
			c[12].Bytes = append(c[12].Bytes, method)
		}
		return c
	})
	if err != nil {
		return err
	}
	if err := wr.writeBoundaries(filepath.Join(wr.dir, "cell_boundaries.parquet"), seg, false); err != nil {
		return err
	}
	return wr.writeBoundaries(filepath.Join(wr.dir, "nucleus_boundaries.parquet"), seg, true)
}

// writeMatrix writes the instrument cells' counts of molecules of quality 20 or more, every
// feature, as Matrix Market.
func (wr *writer) writeMatrix() error {
	dir := filepath.Join(wr.dir, "cell_feature_matrix")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	n := len(wr.in.cells)
	counts := make([]map[int32]int64, n)
	for _, m := range wr.mols {
		l := wr.in.labels[wr.pixelOf(m.x, m.y)]
		if l == 0 || m.qv < 20 {
			continue
		}
		if counts[l-1] == nil {
			counts[l-1] = map[int32]int64{}
		}
		counts[l-1][m.feature]++
	}
	gz := func(name string, fn func(w *bufio.Writer)) error {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		z := gzip.NewWriter(f)
		w := bufio.NewWriter(z)
		fn(w)
		err = w.Flush()
		if e := z.Close(); err == nil {
			err = e
		}
		if e := f.Close(); err == nil {
			err = e
		}
		return err
	}
	if err := gz("features.tsv.gz", func(w *bufio.Writer) {
		for i := range wr.pn.names {
			fmt.Fprintf(w, "%s\t%s\t%s\n", wr.pn.ids[i], wr.pn.names[i], wr.pn.kinds[i])
		}
	}); err != nil {
		return err
	}
	if err := gz("barcodes.tsv.gz", func(w *bufio.Writer) {
		for _, id := range wr.instID {
			fmt.Fprintln(w, id)
		}
	}); err != nil {
		return err
	}
	// The counts by cell (compressed sparse columns: a column per cell, its features in order).
	indptr := []int64{0}
	var indices []int64
	var data []int32
	for _, c := range counts {
		features := make([]int, 0, len(c))
		for f := range c {
			features = append(features, int(f))
		}
		sort.Ints(features)
		for _, f := range features {
			indices = append(indices, int64(f))
			data = append(data, int32(c[int32(f)]))
		}
		indptr = append(indptr, int64(len(indices)))
	}
	err := gz("matrix.mtx.gz", func(w *bufio.Writer) {
		fmt.Fprintf(w, "%%%%MatrixMarket matrix coordinate integer general\n%%metadata_json: {\"software_version\": %q}\n%d %d %d\n", "situcraft-simulate "+wr.software, len(wr.pn.names), n, len(data))
		for k := 0; k < n; k++ {
			for i := indptr[k]; i < indptr[k+1]; i++ {
				fmt.Fprintf(w, "%d %d %d\n", indices[i]+1, k+1, data[i])
			}
		}
	})
	if err != nil {
		return err
	}
	return wr.writeMatrixH5(indptr, indices, data)
}

// writeMatrixH5 writes the counts as a 10x Genomics feature-barcode matrix in HDF5
// (cell_feature_matrix.h5), as Xenium bundles have it: /matrix holds the cells' ids (barcodes),
// the counts as compressed sparse columns (data, indices, indptr) and the shape (features,
// cells); /matrix/features each feature's id, name, type and genome.
func (wr *writer) writeMatrixH5(indptr, indices []int64, data []int32) error {
	f := hdf5.NewFile()
	f.Root.AttrString("filetype", "matrix")
	f.Root.AttrStrings("library_ids", []string{"situcraft_simulated"})
	f.Root.AttrInt64s("original_gem_groups", []int64{1})
	f.Root.AttrInt64("version", 2)
	m, err := f.Root.Group("matrix")
	if err != nil {
		return err
	}
	features, err := m.Group("features")
	if err != nil {
		return err
	}
	genome := make([]string, len(wr.pn.names))
	for i := range genome {
		genome[i] = "Unknown"
	}
	for _, add := range []func() error{
		func() error { return m.Strings("barcodes", wr.instID) },
		func() error { return m.Int32s("data", data) },
		func() error { return m.Int64s("indices", indices) },
		func() error { return m.Int64s("indptr", indptr) },
		func() error { return m.Int32s("shape", []int32{int32(len(wr.pn.names)), int32(len(wr.instID))}) },
		func() error { return features.Strings("_all_tag_keys", []string{"genome"}) },
		func() error { return features.Strings("feature_type", wr.pn.kinds) },
		func() error { return features.Strings("genome", genome) },
		func() error { return features.Strings("id", wr.pn.ids) },
		func() error { return features.Strings("name", wr.pn.names) },
	} {
		if err := add(); err != nil {
			return err
		}
	}
	return f.Write(filepath.Join(wr.dir, "cell_feature_matrix.h5"))
}

// writeMask writes a label image in chunks of up to 4,096 pixels a side.
func writeMask(z *zarr.Writer, name string, labels []uint32, w, h int) error {
	cy, cx := min(4096, h), min(4096, w)
	a, err := z.Array(name, zarr.ArraySpec{Shape: []int{h, w}, Chunks: []int{cy, cx}, DType: "<u4"})
	if err != nil {
		return err
	}
	chunk := make([]byte, 4*cy*cx)
	for gy := 0; gy*cy < h; gy++ {
		for gx := 0; gx*cx < w; gx++ {
			clear(chunk)
			for y := gy * cy; y < min(h, (gy+1)*cy); y++ {
				for x := gx * cx; x < min(w, (gx+1)*cx); x++ {
					binary.LittleEndian.PutUint32(chunk[4*((y-gy*cy)*cx+x-gx*cx):], labels[y*w+x])
				}
			}
			if err := a.WriteChunk([]int{gy, gx}, chunk); err != nil {
				return err
			}
		}
	}
	return nil
}

// zipFolder writes a folder's files into a zip, stored (as Xenium stores its Zarr members), in
// name order, with a fixed time.
func zipFolder(dir, path string) error {
	var names []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		names = append(names, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		return err
	}
	sort.Strings(names)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, name := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			f.Close()
			return err
		}
		src, err := os.Open(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			f.Close()
			return err
		}
		_, err = io.Copy(w, src)
		src.Close()
		if err != nil {
			f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (wr *writer) writeImage() error {
	p := wr.t.p
	if err := os.MkdirAll(filepath.Join(wr.dir, "morphology_focus"), 0o755); err != nil {
		return err
	}
	plane := wr.t.stain()
	levels := 0
	for s := max(plane.Width, plane.Height); s > 1024; s = (s + 1) / 2 {
		levels++
	}
	ps := strconv.FormatFloat(p.PixelUm, 'f', -1, 64)
	xml := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06" Creator="SituCraft simulate ` + wr.software + `">` +
		`<Image ID="Image:0" Name="morphology_focus"><Pixels ID="Pixels:0" DimensionOrder="XYZCT" Type="uint16" BigEndian="false"` +
		fmt.Sprintf(` SizeX="%d" SizeY="%d" SizeZ="1" SizeC="1" SizeT="1"`, plane.Width, plane.Height) +
		` PhysicalSizeX="` + ps + `" PhysicalSizeXUnit="µm" PhysicalSizeY="` + ps + `" PhysicalSizeYUnit="µm">` +
		`<Channel ID="Channel:0" Name="DAPI" SamplesPerPixel="1"><AnnotationRef ID="Annotation:0"/></Channel>` +
		`<TiffData IFD="0" PlaneCount="1" FirstZ="0" FirstC="0" FirstT="0"/></Pixels></Image>` +
		`<StructuredAnnotations><MapAnnotation ID="Annotation:0"><Value><M K="Purpose">Nuclear</M><M K="Simulated">true</M></Value></MapAnnotation></StructuredAnnotations></OME>`
	return tiffWrite(filepath.Join(wr.dir, "morphology_focus", "morphology_focus_0000.ome.tif"), plane, xml, "SituCraft simulate "+wr.software, levels)
}

func (wr *writer) writeTruth() error {
	p := wr.t.p
	dir := filepath.Join(wr.dir, "truth")
	n := len(wr.t.cells)
	run := map[string]any{}
	for k, v := range wr.run {
		run[k] = v
	}
	run["analysis_sw_version"] = analysisSoftware(wr.software) + " truth"
	params, _ := json.Marshal(p)
	run["analysis_uuid"] = uuidOf("situcraft simulation truth\n" + string(params))
	delete(run, "images")
	if err := writeJSON(filepath.Join(dir, "experiment.xenium"), run); err != nil {
		return err
	}
	ps := p.PixelUm
	seg := wr.trueSegmentation()
	genes := make([]int64, n)
	for _, m := range wr.mols {
		if m.cell != 0 && m.qv >= 20 && int(m.feature) < wr.pn.genes {
			genes[m.cell-1]++
		}
	}
	wr.w.trueCellsByType = map[string]int{}
	fields := []parquet.Field{text("cell_id"), f64("x_centroid"), f64("y_centroid"), i64("transcript_counts"), f64("cell_area"), f64("nucleus_area"), text("cell_type"), u32("nucleus_in_section")}
	err := parquetFile(filepath.Join(dir, "cells.parquet"), fields, wr.software, n, func(from, to int) []*parquet.Values {
		c := make([]*parquet.Values, len(fields))
		for i := range c {
			c[i] = &parquet.Values{}
		}
		for k := from; k < to; k++ {
			s, ns := seg.cellShapes[k], seg.nucleusShape(k)
			cl := wr.t.cells[k]
			name := p.Types[cl.typ].Name
			wr.w.trueCellsByType[name]++
			c[0].Bytes = append(c[0].Bytes, []byte(wr.trueID[k]))
			c[1].Float64 = append(c[1].Float64, s.sx/float64(s.pixels)*ps)
			c[2].Float64 = append(c[2].Float64, s.sy/float64(s.pixels)*ps)
			c[3].Int64 = append(c[3].Int64, genes[k])
			c[4].Float64 = append(c[4].Float64, float64(s.pixels)*ps*ps)
			c[5].Float64 = append(c[5].Float64, float64(ns.pixels)*ps*ps)
			c[6].Bytes = append(c[6].Bytes, []byte(name))
			seen := int32(0)
			if cl.nucleus {
				seen = 1
				wr.w.visibleNuclei++
			}
			c[7].Int32 = append(c[7].Int32, seen)
		}
		return c
	})
	if err != nil {
		return err
	}
	if err := wr.writeBoundaries(filepath.Join(dir, "cell_boundaries.parquet"), seg, false); err != nil {
		return err
	}
	if err := wr.writeBoundaries(filepath.Join(dir, "nucleus_boundaries.parquet"), seg, true); err != nil {
		return err
	}
	return wr.writeCellsZarr(filepath.Join(dir, "cells.zarr.zip"), seg)
}

func median(v []int64) float64 {
	s := append([]int64(nil), v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	switch n := len(s); {
	case n == 0:
		return 0
	case n%2 == 1:
		return float64(s[n/2])
	default:
		return float64(s[n/2-1]+s[n/2]) / 2
	}
}

func (wr *writer) writeDescription() error {
	p := wr.t.p
	w := &wr.w
	cells := float64(w.instrumentCells)
	region := p.WidthUm * p.HeightUm
	empty := 0
	var probes int64
	for k, c := range w.transcriptCounts {
		if c == 0 {
			empty++
		}
		probes += w.controlCounts[k]
	}
	metrics := []struct {
		name  string
		value any
	}{
		{"run_name", "SituCraft simulation"},
		{"region_name", fmt.Sprintf("simulated tissue, seed %d", p.Seed)},
		{"region_area", region},
		{"total_cell_area", w.cellArea},
		{"num_cells_detected", w.instrumentCells},
		{"fraction_empty_cells", float64(empty) / cells},
		{"total_high_quality_decoded_transcripts", w.q20Genes},
		{"fraction_transcripts_decoded_q20", float64(w.q20Genes) / float64(w.genesAnyQuality)},
		{"decoded_transcripts_per_100um2", float64(w.q20Genes) / w.cellArea * 100},
		{"fraction_transcripts_assigned", float64(w.q20GenesAssigned) / float64(w.q20Genes)},
		{"median_transcripts_per_cell", median(w.transcriptCounts)},
		{"cells_per_100um2", cells / region * 100},
		{"median_genes_per_cell", median(w.genesPerCell)},
		{"nuclear_transcripts_per_100um2", float64(w.q20GenesNuclear) / (float64(w.nucleusPixels) * p.PixelUm * p.PixelUm) * 100},
		{"segmented_cell_nuc_expansion_frac", 1.0},
	}
	if p.Controls > 0 {
		perControl := float64(probes) / float64(p.Controls) / cells
		metrics = append(metrics, struct {
			name  string
			value any
		}{"negative_control_probe_counts_per_control_per_cell", perControl}, struct {
			name  string
			value any
		}{"estimated_number_of_false_positive_transcripts_per_cell", perControl * float64(p.Genes)})
	}
	var head, row []string
	for _, m := range metrics {
		head = append(head, m.name)
		switch v := m.value.(type) {
		case float64:
			row = append(row, strconv.FormatFloat(v, 'g', -1, 64))
		default:
			row = append(row, fmt.Sprint(v))
		}
	}
	var table bytes.Buffer
	cw := csv.NewWriter(&table)
	cw.Write(head)
	cw.Write(row)
	cw.Flush()
	if err := os.WriteFile(filepath.Join(wr.dir, "metrics_summary.csv"), table.Bytes(), 0o644); err != nil {
		return err
	}
	run := map[string]any{}
	for k, v := range wr.run {
		run[k] = v
	}
	params, _ := json.Marshal(p)
	run["analysis_uuid"] = uuidOf("situcraft simulation instrument\n" + string(params))
	run["num_cells"] = w.instrumentCells
	run["transcripts_per_cell"] = int64(math.Floor(median(w.transcriptCounts)))
	if err := writeJSON(filepath.Join(wr.dir, "experiment.xenium"), run); err != nil {
		return err
	}
	if err := wr.writePanel(); err != nil {
		return err
	}
	return writeJSON(filepath.Join(wr.dir, SimulationFile), wr.description())
}

// writePanel writes gene_panel.json, as Xenium describes its panels.
func (wr *writer) writePanel() error {
	var targets []any
	for i := range wr.pn.names {
		kind := "gene"
		switch wr.pn.kinds[i] {
		case "Negative Control Probe":
			kind = "negative_control_probe"
		case "Negative Control Codeword":
			kind = "negative_control_codeword"
		}
		targets = append(targets, map[string]any{
			"codewords": []int{i + 1},
			"type":      map[string]any{"descriptor": kind, "data": map[string]any{"id": wr.pn.ids[i], "name": wr.pn.names[i]}},
		})
	}
	return writeJSON(filepath.Join(wr.dir, "gene_panel.json"), map[string]any{
		"metadata": map[string]any{"generated_by": "SituCraft simulate " + wr.software},
		"payload": map[string]any{
			"panel":   map[string]any{"identity": map[string]any{"design_id": "situcraft-simulated", "name": "SituCraft simulated panel", "version": "1"}, "species": "Simulated", "tissue": "Simulated"},
			"targets": targets,
		},
	})
}
