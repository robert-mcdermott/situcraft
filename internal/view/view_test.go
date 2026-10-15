package view

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"situcraft/internal/cache"
	"situcraft/internal/xenium"
)

func bundleDir(t *testing.T, name string) string {
	t.Helper()
	root := os.Getenv("SITUCRAFT_DATA")
	if root == "" {
		root = filepath.Join("..", "..", "validation", "cache")
	}
	dir := filepath.Join(root, name)
	if _, err := os.Stat(filepath.Join(dir, "experiment.xenium")); err != nil {
		want := os.Getenv("SITUCRAFT_REQUIRE_DATA")
		if want == "1" || strings.Contains(","+want+",", ","+name+",") {
			t.Fatalf("%s is missing (node validation/fetch.mjs %s)", dir, name)
		}
		t.Skipf("%s is missing (node validation/fetch.mjs %s)", dir, name)
	}
	return dir
}

// openReal builds a bundle's derived data in a temporary cache and opens it for viewing.
func openReal(t *testing.T, name string) (*Dataset, *xenium.Bundle) {
	t.Helper()
	dir := bundleDir(t, name)
	if testing.Short() {
		t.Skip("short mode: under the race detector, building the derived data takes minutes")
	}
	b, err := xenium.Open(dir, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	c, err := cache.Open(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Build(context.Background(), b, nil, nil); err != nil {
		t.Fatal(err)
	}
	d, err := Open(c, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d, b
}

type transcript struct {
	gene     uint16
	x, y, qv float32
	cell     uint32
	row      uint32
}

// readAll reads transcripts.parquet directly, naming genes by the index's numbering.
func readAll(t *testing.T, d *Dataset, b *xenium.Bundle) []transcript {
	t.Helper()
	geneOf := map[string]uint16{}
	for i, g := range d.Transcripts.Summary.Genes {
		geneOf[g.Name] = uint16(i)
	}
	tr, err := b.OpenTranscripts()
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	var out []transcript
	row := uint32(0)
	err = tr.Each(func(batch *xenium.TranscriptBatch) error {
		for i := 0; i < batch.Rows; i++ {
			cell := uint32(cache.NoCell)
			if c, ok := d.Cells.Index[string(batch.CellID[i])]; ok {
				cell = uint32(c)
			}
			out = append(out, transcript{geneOf[string(batch.Feature[i])], batch.X[i], batch.Y[i], batch.QV[i], cell, row})
			row++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRealRegionQueries(t *testing.T) {
	for _, name := range []string{"xenium-lung-2fov", "xenium-breast-2fov"} {
		t.Run(name, func(t *testing.T) {
			d, b := openReal(t, name)
			all := readAll(t, d, b)
			genes := len(d.Transcripts.Summary.Genes)
			cases := []struct {
				r     Rect
				genes Genes
				minQ  float32
			}{
				{Rect{100, 100, 400, 300}, nil, 0},
				{Rect{100, 100, 400, 300}, Genes{0, 3, genes - 1}, 20},
				// Across tile edges (a level-0 tile is 108.8 µm) and past the image's edges.
				{Rect{-10, -10, 120, 230}, nil, 20},
				{Rect{1100, 600, 1400, 900}, Genes{1, 2}, 0},
				{Rect{0, 0, 2000, 2000}, nil, 0},
				{Rect{500, 500, 500, 600}, nil, 0},
			}
			for _, c := range cases {
				got, err := d.RegionPoints(c.r, c.genes, c.minQ)
				if err != nil {
					t.Fatal(err)
				}
				var want []transcript
				wanted := map[uint16]bool{}
				for _, g := range c.genes {
					wanted[uint16(g)] = true
				}
				for _, tr := range all {
					x, y := float64(tr.x), float64(tr.y)
					if x >= c.r.X0 && x < c.r.X1 && y >= c.r.Y0 && y < c.r.Y1 && tr.qv >= c.minQ && (c.genes == nil || wanted[tr.gene]) {
						want = append(want, tr)
					}
				}
				sort.Slice(want, func(i, j int) bool {
					if want[i].gene != want[j].gene {
						return want[i].gene < want[j].gene
					}
					return want[i].row < want[j].row
				})
				if got.Len() != len(want) {
					t.Fatalf("%+v: %d points, transcripts.parquet has %d", c, got.Len(), len(want))
				}
				i := 0
				for k, gene := range got.Genes {
					for at := got.Starts[k]; at < got.Starts[k+1]; at++ {
						w := want[i]
						if w.gene != gene || w.row != got.Row[at] || w.x != got.X[at] || w.y != got.Y[at] || w.cell != got.Cell[at] {
							t.Fatalf("%+v: point %d differs from transcripts.parquet's", c, i)
						}
						i++
					}
				}
			}
			if _, err := d.RegionPoints(Rect{0, 0, 1, math.NaN()}, nil, 0); err == nil {
				t.Error("a region with NaN was accepted")
			}
			if _, err := d.TilePoints(0, 0, Genes{genes + 5}, 0); err == nil {
				t.Error("an unknown gene was accepted")
			}
		})
	}
}

func TestRealDensityAndCells(t *testing.T) {
	for _, name := range []string{"xenium-lung-2fov", "xenium-breast-2fov"} {
		t.Run(name, func(t *testing.T) {
			d, _ := openReal(t, name)
			tr := d.Transcripts
			n := len(tr.Summary.Genes)
			// Each level's grids of a few genes, and of every gene together, sum to their counts.
			for _, l := range tr.Summary.DensityLevels {
				sums := map[uint16]int64{}
				for ty := 0; ty < d.Grid.Down(l); ty++ {
					for tx := 0; tx < d.Grid.Across(l); tx++ {
						g, err := d.TileDensity(l, tx, ty, Genes{0, 7, n}, true)
						if err != nil {
							t.Fatal(err)
						}
						for k, gene := range g.Genes {
							for i := g.Starts[k]; i < g.Starts[k+1]; i++ {
								sums[gene] += int64(g.Count[i])
							}
						}
					}
				}
				if sums[0] != tr.Summary.Genes[0].Q20 || sums[7] != tr.Summary.Genes[7].Q20 || sums[uint16(n)] != tr.Summary.Q20 {
					t.Errorf("level %d: Q20 sums %v, counts %d, %d, %d", l, sums, tr.Summary.Genes[0].Q20, tr.Summary.Genes[7].Q20, tr.Summary.Q20)
				}
			}
			// Every cell's Q20 gene transcripts, counted from the index, equal cells.parquet's.
			perCell := make([]int64, len(d.Cells.IDs))
			for ty := 0; ty < d.Grid.Down(0); ty++ {
				for tx := 0; tx < d.Grid.Across(0); tx++ {
					tile, err := tr.Points(tx, ty)
					if err != nil {
						t.Fatal(err)
					}
					for k, gene := range tile.Genes {
						if tr.Summary.Genes[gene].Kind != xenium.KindGene {
							continue
						}
						for i := tile.Starts[k]; i < tile.Starts[k+1]; i++ {
							if tile.Cell[i] != cache.NoCell && tile.QV[i] >= 20 {
								perCell[tile.Cell[i]]++
							}
						}
					}
				}
			}
			for row, n := range perCell {
				if n != d.Cells.TranscriptCounts[row] {
					t.Fatalf("cell %s: %d Q20 gene transcripts in the index, cells.parquet says %d", d.Cells.IDs[row], n, d.Cells.TranscriptCounts[row])
				}
			}
			// A cell in full: its counts, top genes and polygons, for every 37th cell.
			cells, _ := xenium.Open(d.Path, xenium.Options{CacheDir: t.TempDir()})
			defer cells.Close()
			boundaries, err := cells.ReadBoundaries("cell_boundaries.parquet")
			if err != nil {
				t.Fatal(err)
			}
			polygonOf := map[string][]float32{}
			for i, id := range boundaries.CellIDs {
				var xy []float32
				for v := boundaries.Offsets[i]; v < boundaries.Offsets[i+1]; v++ {
					xy = append(xy, boundaries.X[v], boundaries.Y[v])
				}
				polygonOf[id] = xy
			}
			for row := 0; row < len(d.Cells.IDs); row += 37 {
				c, err := d.Cell(row, 10)
				if err != nil {
					t.Fatal(err)
				}
				if int64(c.Counted.Q20Genes) != d.Cells.TranscriptCounts[row] || c.ID != d.Cells.IDs[row] {
					t.Fatalf("cell %d: %d Q20 gene transcripts counted, %d recorded", row, c.Counted.Q20Genes, d.Cells.TranscriptCounts[row])
				}
				sum := 0
				for i, g := range c.Genes {
					sum += g.Count
					if i > 0 && g.Count > c.Genes[i-1].Count {
						t.Fatal("top genes out of order")
					}
				}
				if len(c.Genes) > 10 || (c.GenesDetected <= 10 && sum != c.Counted.Q20Genes) {
					t.Fatalf("cell %d: top genes %+v", row, c.Genes)
				}
				if len(c.Boundaries) != 1 || !equalFloats(c.Boundaries[0], polygonOf[c.ID]) {
					t.Fatalf("cell %d: its boundary differs from cell_boundaries.parquet's", row)
				}
			}
			if _, err := d.Cell(len(d.Cells.IDs), 5); err != ErrNoCell {
				t.Errorf("a cell past the table: %v", err)
			}
		})
	}
}

func equalFloats(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRealDescriptor(t *testing.T) {
	d, _ := openReal(t, "xenium-breast-2fov")
	desc, err := d.Describe()
	if err != nil {
		t.Fatal(err)
	}
	if len(desc.Channels) != 4 || desc.Channels[0].Name != "DAPI" || desc.Channels[1].Purpose == "" {
		t.Fatalf("channels %+v", desc.Channels)
	}
	for _, ch := range desc.Channels {
		if !(ch.Range[0] >= 1 && ch.Range[1] > ch.Range[0] && ch.Max >= ch.Range[1]) {
			t.Errorf("%s: range %v, max %v", ch.Name, ch.Range, ch.Max)
		}
	}
	if desc.Transcripts == nil || desc.Transcripts.Count != 1113950 || len(desc.Transcripts.Scale) != len(desc.Transcripts.Genes)+cache.PseudoCount {
		t.Fatalf("transcripts %+v", desc.Transcripts)
	}
	if strings.Join(desc.Cells.Methods, "|") == "" || desc.Cells.MethodCounts[0] != 3300 || desc.Cells.MethodCounts[1] != 3913 || desc.Cells.MethodCounts[2] != 62 {
		t.Errorf("methods %v %v", desc.Cells.Methods, desc.Cells.MethodCounts)
	}
	if len(desc.Geometry) != 2 || desc.Cells.Count != 7275 {
		t.Errorf("geometry %+v, cells %d", desc.Geometry, desc.Cells.Count)
	}
}

func TestEncodings(t *testing.T) {
	p := &Points{Genes: []uint16{2, 9}, Starts: []uint32{0, 1, 3}, X: []float32{1, 2, 3}, Y: []float32{4, 5, 6}, Cell: []uint32{7, cache.NoCell, 8}}
	raw := p.Encode()
	if string(raw[:4]) != "SCVP" || len(raw)%4 != 0 || len(raw) != 16+4*3+4*3*3+2*2 {
		t.Fatalf("points encode to %d bytes", len(raw))
	}
	le := binary.LittleEndian
	if le.Uint32(raw[4:]) != 3 || le.Uint32(raw[8:]) != 2 || math.Float32frombits(le.Uint32(raw[16+12+8:])) != 3 || le.Uint16(raw[16+12+36+2:]) != 9 {
		t.Error("points columns misplaced")
	}
	g := &Density{Bins: 128, Genes: []uint16{4}, Starts: []uint32{0, 3}, Bin: []uint16{1, 2, 3}, Count: []uint32{5, 6, 7}}
	raw = g.Encode()
	if string(raw[:4]) != "SCVD" || len(raw)%4 != 0 || len(raw) != 16+8+12+4+8 {
		t.Fatalf("density encodes to %d bytes", len(raw))
	}
	if le.Uint32(raw[16+8+8:]) != 7 || le.Uint16(raw[16+8+12:]) != 4 || le.Uint16(raw[16+8+12+4+4:]) != 3 {
		t.Error("density columns misplaced")
	}
	hist := make([]int64, 1<<16)
	hist[0] = 1000
	for v := 1; v <= 200; v++ {
		hist[v] = 1
	}
	r, m := displayRange(hist)
	if r != [2]float64{2, 199} || m != 200 {
		t.Errorf("display range %v, max %v", r, m)
	}
}

// The QC maps' groups of transcripts, summed over the tissue, give the run's metrics: the share
// of gene transcripts in cells (the unassigned-fraction map's complement) and the share of
// quality 20 or more (the Q-score map). The cells table's genes per cell give the median.
func TestRealQCMapsAndColumns(t *testing.T) {
	for _, name := range []string{"xenium-lung-2fov", "xenium-breast-2fov"} {
		t.Run(name, func(t *testing.T) {
			d, b := openReal(t, name)
			tr := d.Transcripts
			n := len(tr.Summary.Genes)
			levels := tr.Summary.DensityLevels
			top := levels[len(levels)-1]
			sum := func(gene int, q20 bool) float64 {
				var total float64
				for ty := 0; ty < d.Grid.Down(top); ty++ {
					for tx := 0; tx < d.Grid.Across(top); tx++ {
						g, err := d.TileDensity(top, tx, ty, Genes{gene}, q20)
						if err != nil {
							t.Fatal(err)
						}
						for _, c := range g.Count {
							total += float64(c)
						}
					}
				}
				return total
			}
			genesQ := sum(n+cache.PseudoGenes, true)
			assigned := sum(n+cache.PseudoInCells, true)
			genesAll := sum(n+cache.PseudoGenes, false)
			metric := func(name string) float64 {
				v, ok := b.Metrics.Number(name)
				if !ok {
					t.Fatalf("no %s", name)
				}
				return v
			}
			near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b)) }
			if !near(assigned/genesQ, metric("fraction_transcripts_assigned")) || !near(genesQ/genesAll, metric("fraction_transcripts_decoded_q20")) || genesQ != metric("total_high_quality_decoded_transcripts") {
				t.Errorf("assigned %v of %v (Q20 of %v): %v and %v; recorded %v and %v", assigned, genesQ, genesAll, assigned/genesQ, genesQ/genesAll, metric("fraction_transcripts_assigned"), metric("fraction_transcripts_decoded_q20"))
			}
			if _, err := d.TilePoints(0, 0, Genes{n + cache.PseudoGenes}, 0); err == nil {
				t.Error("points of a pseudo-gene were answered")
			}
			// The cells table's columns.
			raw, err := d.CellTable()
			if err != nil {
				t.Fatal(err)
			}
			cells := len(d.Cells.IDs)
			le := binary.LittleEndian
			if string(raw[:4]) != "SCVC" || int(le.Uint32(raw[4:])) != cells || int(le.Uint32(raw[12:])) != len(CellColumns) || len(raw) != 16+4*10*cells+(cells+3)/4*4 {
				t.Fatalf("the cells table has %d bytes", len(raw))
			}
			column := func(k int) []uint32 {
				out := make([]uint32, cells)
				for i := range out {
					out[i] = le.Uint32(raw[16+4*k*cells+4*i:])
				}
				return out
			}
			transcripts, genes, nuclear := column(4), column(8), column(9)
			sorted := make([]int, cells)
			var nuclearSum, transcriptSum int64
			for i := range sorted {
				sorted[i] = int(genes[i])
				nuclearSum += int64(nuclear[i])
				transcriptSum += int64(transcripts[i])
				if genes[i] > transcripts[i] || nuclear[i] > transcripts[i] || int64(transcripts[i]) != d.Cells.TranscriptCounts[i] {
					t.Fatalf("cell %d: %d genes, %d nuclear, %d transcripts", i, genes[i], nuclear[i], transcripts[i])
				}
			}
			sort.Ints(sorted)
			median := float64(sorted[cells/2])
			if cells%2 == 0 {
				median = float64(sorted[cells/2-1]+sorted[cells/2]) / 2
			}
			if median != metric("median_genes_per_cell") {
				t.Errorf("median genes per cell %v, recorded %v", median, metric("median_genes_per_cell"))
			}
			// Every cell's genes, against transcripts.parquet read directly: the table counts a row of
			// tiles at a time, carrying the cells that span rows.
			detected := make([]map[uint16]bool, cells)
			for _, x := range readAll(t, d, b) {
				if x.cell != cache.NoCell && x.qv >= 20 && tr.Summary.Genes[x.gene].Kind == xenium.KindGene {
					if detected[x.cell] == nil {
						detected[x.cell] = map[uint16]bool{}
					}
					detected[x.cell][x.gene] = true
				}
			}
			for i := range detected {
				if int(genes[i]) != len(detected[i]) {
					t.Fatalf("cell %d: %d genes in the table, %d in transcripts.parquet", i, genes[i], len(detected[i]))
				}
			}
			if nuclearSum == 0 || nuclearSum >= transcriptSum {
				t.Errorf("%d nuclear of %d transcripts", nuclearSum, transcriptSum)
			}
			desc, err := d.Describe()
			if err != nil {
				t.Fatal(err)
			}
			if len(desc.Transcripts.Pseudo) != cache.PseudoCount || len(desc.Transcripts.Scale) != n+cache.PseudoCount {
				t.Errorf("descriptor pseudo-genes %+v", desc.Transcripts.Pseudo)
			}
		})
	}
}
