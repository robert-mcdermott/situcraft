package view

import (
	"math"
	"sort"

	"situcraft/internal/cache"
	"situcraft/internal/xenium"
)

// Descriptor is what the viewer needs to know of a dataset before drawing it.
type Descriptor struct {
	Name    string            `json:"name"`
	Path    string            `json:"path"`
	Grid    cache.Grid        `json:"grid"`
	Entries map[string]string `json:"entries"`
	// Channels are the morphology image's channels (none without an image entry).
	Channels    []ChannelInfo      `json:"channels"`
	Transcripts *TranscriptInfo    `json:"transcripts,omitempty"`
	Geometry    []cache.SetSummary `json:"geometry"`
	Cells       CellsInfo          `json:"cells"`
}

// ChannelInfo describes one channel and how to show it at first.
type ChannelInfo struct {
	Name    string               `json:"name"`
	Purpose string               `json:"purpose,omitempty"`
	Bits    int                  `json:"bits"`
	Levels  []cache.LevelSummary `json:"levels"`
	// Range is the default display window: the 1st and 99.5th percentiles of the coarsest
	// level's non-zero pixels. Max is the largest value there.
	Range [2]float64 `json:"range"`
	Max   float64    `json:"max"`
}

// TranscriptInfo describes the transcript index.
type TranscriptInfo struct {
	Count int64        `json:"count"`
	Q20   int64        `json:"q20"`
	Genes []cache.Gene `json:"genes"`
	// Pseudo are the groups of transcripts with density grids at indexes after the genes'.
	Pseudo        []cache.Gene `json:"pseudo"`
	DensityLevels []int        `json:"densityLevels"`
	DensityBins   int          `json:"densityBins"`
	// Scale is, for each gene and then each pseudo-gene, the 99th percentile of its non-empty
	// Q20 bins at the coarsest density level: a level L finer holds about a 4^L-th of that in a
	// bin, which sets the density's color scale.
	Scale  []float64  `json:"scale"`
	Bounds [4]float32 `json:"bounds"`
}

// CellsInfo describes the cells table.
type CellsInfo struct {
	Count   int      `json:"count"`
	Methods []string `json:"methods,omitempty"`
	// MethodCounts are the cells of each method.
	MethodCounts []int `json:"methodCounts,omitempty"`
}

// Describe describes the dataset (computed once).
func (d *Dataset) Describe() (*Descriptor, error) {
	d.once.Do(func() { d.desc, d.dErr = d.describe() })
	return d.desc, d.dErr
}

func (d *Dataset) describe() (*Descriptor, error) {
	desc := &Descriptor{Name: d.Name, Path: d.Path, Grid: d.Grid, Entries: d.Entries, Channels: []ChannelInfo{}, Geometry: []cache.SetSummary{}}
	if d.Image != nil {
		for ci, ch := range d.Image.Summary.Channels {
			info := ChannelInfo{Name: ch.Name, Purpose: ch.Purpose, Bits: d.Image.Summary.Bits, Levels: ch.Levels}
			top := len(ch.Levels) - 1
			hist := make([]int64, 1<<16)
			for ty := 0; ty < ch.Levels[top].Down; ty++ {
				for tx := 0; tx < ch.Levels[top].Across; tx++ {
					pix, _, _, err := d.Image.Tile(ci, top, tx, ty)
					if err != nil {
						return nil, err
					}
					for _, v := range pix {
						hist[v]++
					}
				}
			}
			info.Range, info.Max = displayRange(hist)
			desc.Channels = append(desc.Channels, info)
		}
	}
	if t := d.Transcripts; t != nil {
		info := &TranscriptInfo{Count: t.Summary.Transcripts, Q20: t.Summary.Q20, Genes: t.Summary.Genes, Pseudo: t.Summary.Pseudo, DensityLevels: t.Summary.DensityLevels, DensityBins: t.Params.DensityBins, Bounds: t.Summary.Bounds}
		info.Scale = make([]float64, len(t.Summary.Genes)+cache.PseudoCount)
		for i := range info.Scale {
			info.Scale[i] = 1
		}
		if n := len(t.Summary.DensityLevels); n > 0 {
			top := t.Summary.DensityLevels[n-1]
			bins := make([][]uint32, len(info.Scale))
			for ty := 0; ty < d.Grid.Down(top); ty++ {
				for tx := 0; tx < d.Grid.Across(top); tx++ {
					tile, err := t.Density(top, tx, ty)
					if err != nil {
						return nil, err
					}
					for k, gene := range tile.Genes {
						for i := tile.Starts[k]; i < tile.Starts[k+1]; i++ {
							if tile.Q20[i] > 0 {
								bins[gene] = append(bins[gene], tile.Q20[i])
							}
						}
					}
				}
			}
			for g, b := range bins {
				if len(b) == 0 {
					continue
				}
				sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })
				info.Scale[g] = math.Max(1, float64(b[min(len(b)-1, int(0.99*float64(len(b))))]))
			}
		}
		desc.Transcripts = info
	}
	if d.Geometry != nil {
		desc.Geometry = d.Geometry.Summary.Sets
	}
	desc.Cells.Count = len(d.Cells.IDs)
	if d.Methods != nil {
		desc.Cells.Methods = d.Methods.Methods
		desc.Cells.MethodCounts = make([]int, len(d.Methods.Methods))
		for _, m := range d.Methods.Cell {
			if int(m) < len(desc.Cells.MethodCounts) {
				desc.Cells.MethodCounts[m]++
			}
		}
	}
	return desc, nil
}

// displayRange is a default window over a histogram of 16-bit values: the 1st and 99.5th
// percentiles of the non-zero values (zero is the empty border of many images), and the largest.
func displayRange(hist []int64) ([2]float64, float64) {
	var total int64
	maxV := 0
	for v := 1; v < len(hist); v++ {
		total += hist[v]
		if hist[v] > 0 {
			maxV = v
		}
	}
	if total == 0 {
		return [2]float64{0, 1}, 0
	}
	quantile := func(q float64) int {
		target := int64(math.Ceil(q * float64(total)))
		var seen int64
		for v := 1; v < len(hist); v++ {
			seen += hist[v]
			if seen >= target {
				return v
			}
		}
		return maxV
	}
	lo, hi := quantile(0.01), quantile(0.995)
	if hi <= lo {
		hi = lo + 1
	}
	return [2]float64{float64(lo), float64(hi)}, float64(maxV)
}

// --- One cell ------------------------------------------------------------------------------------

// CellDetail is everything known of one cell.
type CellDetail struct {
	Row         int      `json:"row"`
	ID          string   `json:"id"`
	X           float64  `json:"x"`
	Y           float64  `json:"y"`
	Area        *float64 `json:"area,omitempty"`
	NucleusArea *float64 `json:"nucleusArea,omitempty"`
	// Recorded are cells.parquet's counts of the cell's transcripts (quality 20 or more) by kind.
	Recorded []Count `json:"recorded"`
	Method   string  `json:"method,omitempty"`
	// Counted are the cell's transcripts as the index holds them: every one assigned to it, those
	// of quality 20 or more, and those of genes (not controls) of quality 20 or more, which
	// cells.parquet's transcript_counts counts.
	Counted struct {
		All      int `json:"all"`
		Q20      int `json:"q20"`
		Q20Genes int `json:"q20Genes"`
	} `json:"counted"`
	// Genes are its genes by their Q20 transcripts, most first (at most the number asked for);
	// GenesDetected counts all with at least one.
	Genes         []GeneCount `json:"genes"`
	GenesDetected int         `json:"genesDetected"`
	// Boundaries are the cell's polygons at full resolution (x, y interleaved, µm), and its
	// nucleus's.
	Boundaries [][]float32 `json:"boundaries"`
	Nuclei     [][]float32 `json:"nuclei"`
}

// Count is a labeled number.
type Count struct {
	Label string `json:"label"`
	Value int64  `json:"value"`
}

// GeneCount is a gene's transcripts in a cell.
type GeneCount struct {
	Gene  int    `json:"gene"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Cell describes cell row of cells.parquet, with its top genes.
func (d *Dataset) Cell(row, top int) (*CellDetail, error) {
	c := d.Cells
	if row < 0 || row >= len(c.IDs) {
		return nil, ErrNoCell
	}
	out := &CellDetail{Row: row, ID: c.IDs[row], X: c.X[row], Y: c.Y[row], Genes: []GeneCount{}, Boundaries: [][]float32{}, Nuclei: [][]float32{}}
	if row < len(c.CellArea) {
		out.Area = &c.CellArea[row]
	}
	if row < len(c.NucleusArea) {
		out.NucleusArea = &c.NucleusArea[row]
	}
	for _, k := range []struct {
		label  string
		values []int64
	}{
		{"Gene transcripts", c.TranscriptCounts}, {"Negative control probes", c.ControlProbeCounts},
		{"Negative control codewords", c.ControlCodewordCounts}, {"Unassigned codewords", c.UnassignedCodewordCounts},
		{"Deprecated codewords", c.DeprecatedCodewordCounts}, {"All", c.TotalCounts},
	} {
		if row < len(k.values) {
			out.Recorded = append(out.Recorded, Count{k.label, k.values[row]})
		}
	}
	if d.Methods != nil && int(d.Methods.Cell[row]) < len(d.Methods.Methods) {
		out.Method = d.Methods.Methods[d.Methods.Cell[row]]
	}
	// The cell's polygons and transcripts lie within the largest polygon extent of its centroid.
	reach := 0.0
	if d.Geometry != nil {
		for _, s := range d.Geometry.Summary.Sets {
			reach = math.Max(reach, s.MaxExtent)
		}
	}
	reach = math.Max(reach, 50) + 2
	r := Rect{out.X - reach, out.Y - reach, out.X + reach, out.Y + reach}
	if d.Geometry != nil {
		tx0, ty0, tx1, ty1 := d.tilesOf(0, r)
		for _, set := range d.Geometry.Summary.Sets {
			for ty := ty0; ty <= ty1; ty++ {
				for tx := tx0; tx <= tx1; tx++ {
					tile, err := d.Geometry.Tile(set.Name, 0, tx, ty)
					if err != nil {
						return nil, err
					}
					for k, cell := range tile.Cells {
						if int(cell) != row {
							continue
						}
						ring := append([]float32(nil), tile.XY[2*tile.Starts[k]:2*tile.Starts[k+1]]...)
						if set.Name == "nuclei" {
							out.Nuclei = append(out.Nuclei, ring)
						} else {
							out.Boundaries = append(out.Boundaries, ring)
						}
					}
				}
			}
		}
	}
	if t := d.Transcripts; t != nil {
		counts := map[uint16]int{}
		tx0, ty0, tx1, ty1 := d.tilesOf(0, r)
		for ty := ty0; ty <= ty1; ty++ {
			for tx := tx0; tx <= tx1; tx++ {
				tile, err := d.tiles.get(t, tx, ty)
				if err != nil {
					return nil, err
				}
				for k, gene := range tile.Genes {
					isGene := t.Summary.Genes[gene].Kind == xenium.KindGene
					for i := tile.Starts[k]; i < tile.Starts[k+1]; i++ {
						if int(tile.Cell[i]) != row || tile.Cell[i] == cache.NoCell {
							continue
						}
						out.Counted.All++
						if tile.QV[i] >= 20 {
							out.Counted.Q20++
							if isGene {
								out.Counted.Q20Genes++
								counts[gene]++
							}
						}
					}
				}
			}
		}
		for gene, n := range counts {
			out.Genes = append(out.Genes, GeneCount{Gene: int(gene), Name: t.Summary.Genes[gene].Name, Count: n})
		}
		sort.Slice(out.Genes, func(i, j int) bool {
			if out.Genes[i].Count != out.Genes[j].Count {
				return out.Genes[i].Count > out.Genes[j].Count
			}
			return out.Genes[i].Name < out.Genes[j].Name
		})
		out.GenesDetected = len(out.Genes)
		if top >= 0 && len(out.Genes) > top {
			out.Genes = out.Genes[:top]
		}
	}
	return out, nil
}
