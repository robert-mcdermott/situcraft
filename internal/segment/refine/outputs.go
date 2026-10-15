package refine

// A run's segmentation as SituCraft's segmentation files (internal/segment), so that it is
// checked, compared, viewed and exported as every segmentation is:
//   - the label raster at the morphology image's resolution: each pixel the 2D footprint's cell of
//     the lattice column holding its centre (each column the label holding most of its layers);
//   - the cells (named c1, c2, … and by the instrument's cell each started from), their outlines
//     traced from the raster, and in more than one layer each layer's outlines;
//   - each transcript's cell: for the gene transcripts of quality 20 or more the run modelled, the
//     cell it lay in most often over the summarized sweeps, with that share as its probability;
//     for the others (controls, low quality, beyond the image), the cell holding the pixel it was
//     observed in, as the instrument assigns them;
//   - the counts of gene transcripts of quality 20 or more;
//   - the diagnostics: every sweep's trace, the summary's convergence, and the cells flagged for
//     what limits them (few transcripts, no nucleus, uncertain boundaries), and those left out.

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// The method as artifacts record it.
const (
	Method        = "SituCraft transcript-informed segmentation"
	MethodVersion = "1"
	Citation      = "Jones DC, Elz AE, Hadadianpour A, Ryu H, Glass DR, Newell EW. Cell simulation as cell segmentation. Nat Methods 2025;22(6):1331–1342. doi:10.1038/s41592-025-02697-0"
)

// Thresholds of the cells' limitations (SituCraft's choices, research.md §3.2).
const (
	// A cell with fewer gene transcripts than this has boundaries its prior draws more than its
	// transcripts.
	lowTranscripts = 10
	// A cell whose voxels' labels are, on average, its own in fewer of the summarized sweeps than
	// this has uncertain boundaries.
	uncertainShare = 0.75
	// outlineTolerance simplifies outlines to within half a voxel (in pixels of the lattice); at
	// the image's resolution, its pixels are finer than the voxels, and the outline is simplified to
	// half a voxel too, so that the voxels' steps are not drawn.
	outlineTolerance = 0.5
)

// Recorded are a run's parameters as its artifact records them: every option after defaults
// (the volume prior's mean resolved), and what the run started from.
type Recorded struct {
	Model   Hyper    `json:"model"`
	Sampler Settings `json:"sampler"`
	Stages  []Stage  `json:"stages"`
	BlockUm float64  `json:"block_um"`
	Seed    uint64   `json:"seed"`
	Samples int      `json:"samples"`
	// Start says what the cells started from; StartStage the stage the run started at (from 1).
	Start      string `json:"start"`
	StartStage int    `json:"start_stage"`
	// CountsMinQuality is the quality the transcripts modelled and counted reach.
	CountsMinQuality int `json:"counts_min_quality"`
}

// Limitations are the cells a run flags, by id (the instrument's ids for those left out).
type Limitations struct {
	LowTranscripts []string `json:"lowTranscripts"`
	NoNucleus      []string `json:"noNucleus"`
	Uncertain      []string `json:"uncertainBoundaries"`
	LeftOut        []string `json:"leftOut"`
	Thresholds     struct {
		LowTranscripts int     `json:"lowTranscripts"`
		UncertainShare float64 `json:"uncertainShare"`
	} `json:"thresholds"`
}

// DiagnosticsSummary is what the artifact's summary carries of the diagnostics.
type DiagnosticsSummary struct {
	Sweeps  int `json:"sweeps"`
	Samples int `json:"samples"`
	// SplitHalf is the share of modelled transcripts whose cell is the same over both halves of
	// the samples; MeanProbability their assignments' mean probability; LowProbability the share
	// below 0.5.
	SplitHalf       float64 `json:"splitHalfAgreement"`
	MeanProbability float64 `json:"meanProbability"`
	LowProbability  float64 `json:"lowProbabilityShare"`
	// LogLikelihoodDrift is the log-likelihood's mean change from the first half of the samples to
	// the second, per modelled transcript (near 0 when it has settled).
	LogLikelihoodDrift float64 `json:"logLikelihoodDriftPerTranscript"`
	// GewekeZ is Geweke's convergence diagnostic of the log-likelihood over the summarized sweeps:
	// the difference between its mean over their first tenth and their last half, in standard
	// errors (batch means). Within ±2 it has settled.
	GewekeZ float64 `json:"gewekeZ"`
	// MovedLast is the voxels moved per sweep over the last tenth of the sweeps.
	MovedLast float64 `json:"voxelsMovedPerSweep"`
	// The cells flagged, by limitation, and left out.
	LowTranscripts int `json:"cellsLowTranscripts"`
	NoNucleus      int `json:"cellsNoNucleus"`
	Uncertain      int `json:"cellsUncertainBoundaries"`
	LeftOut        int `json:"cellsLeftOut"`
}

// Diagnostics is diagnostics.json.
type Diagnostics struct {
	Summary     DiagnosticsSummary `json:"summary"`
	Limitations Limitations        `json:"limitations"`
	Trace       []SweepTrace       `json:"trace"`
}

// WriteBundleOutputs writes a run on a bundle's inputs as a segmentation in dir.
func WriteBundleOutputs(b *xenium.Bundle, in *Inputs, idx *BundleIndex, res *Result, opt Options, dir string) (*segment.Description, *Diagnostics, error) {
	l := res.State.Lat
	k := res.State.Cells
	footprint := Footprint(res)
	// The raster at the image's grid.
	img := in.Start
	ps := img.PixelSize
	raster := &segment.Raster{Width: img.Width, Height: img.Height, PixelSize: ps, Rows: make([][]segment.Run, img.Height)}
	row := make([]uint32, img.Width)
	for y := 0; y < img.Height; y++ {
		cy := int((mul(float64(y), ps) + ps/2 - (l.Y0 - in.Y0)) / l.VoxelUm)
		for x := range row {
			row[x] = 0
			cx := int((mul(float64(x), ps) + ps/2 - (l.X0 - in.X0)) / l.VoxelUm)
			if cx >= 0 && cy >= 0 && cx < l.W && cy < l.H {
				row[x] = footprint[cy*l.W+cx]
			}
		}
		raster.Rows[y] = segment.EncodeRow(row)
	}
	stats := raster.Stats(k)

	// Assignments, every row of transcripts.parquet; counts of the modelled ones by gene.
	assignments := make([]uint32, idx.Total)
	probability := make([]float32, idx.Total)
	nan := float32(math.NaN())
	modelled := make([]int32, idx.Total)
	for i := range modelled {
		modelled[i] = -1
		probability[i] = nan
	}
	for i, r := range idx.Rows {
		modelled[r] = int32(i)
	}
	tr, err := b.OpenTranscripts()
	if err != nil {
		return nil, nil, err
	}
	var q20Total, q20Assigned int64
	pairs := map[uint64]uint32{}
	row0 := 0
	err = tr.Each(func(batch *xenium.TranscriptBatch) error {
		for i := 0; i < batch.Rows; i++ {
			r := row0 + i
			if j := modelled[r]; j >= 0 {
				c := res.Assigned[j]
				assignments[r], probability[r] = c, res.Probability[j]
				q20Total++
				if c > 0 {
					q20Assigned++
					pairs[uint64(c-1)<<32|uint64(in.Transcripts.Gene[j])]++
				}
				continue
			}
			assignments[r] = raster.AtMicrons(float64(batch.X[i])-in.X0, float64(batch.Y[i])-in.Y0)
			if batch.QV[i] >= 20 && xenium.FeatureKind(string(batch.Feature[i])) == xenium.KindGene {
				q20Total++ // a gene transcript beyond the image: in no cell
			}
		}
		row0 += batch.Rows
		return nil
	})
	tr.Close()
	if err != nil {
		return nil, nil, err
	}

	// Cells and counts.
	cells := make([]segment.Cell, k)
	px2 := mul(ps, ps)
	for c := range cells {
		cell := &cells[c]
		cell.ID = "c" + strconv.Itoa(c+1)
		cell.Nucleus = idx.CellIDs[res.Kept[c]]
		cell.NucleusArea = float32(in.NucleusUm2[res.Kept[c]])
		if s := stats[c]; s.Pixels > 0 {
			cell.Area = float32(mul(float64(s.Pixels), px2))
			cell.X, cell.Y = float32(mul(s.SumX/float64(s.Pixels), ps)+in.X0), float32(mul(s.SumY/float64(s.Pixels), ps)+in.Y0)
		}
	}
	for _, c := range assignments {
		if c > 0 {
			cells[c-1].Assigned++
		}
	}
	keys := make([]uint64, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	counts := &segment.Counts{Genes: idx.Genes, Offsets: make([]uint32, k+1)}
	for _, key := range keys {
		c, g := int(key>>32), uint32(key)
		counts.Gene = append(counts.Gene, g)
		counts.Count = append(counts.Count, pairs[key])
		counts.Offsets[c+1]++
		cells[c].Transcripts += pairs[key]
		cells[c].Genes++
	}
	for c := 1; c <= k; c++ {
		counts.Offsets[c] += counts.Offsets[c-1]
	}

	// Outlines: the footprint's, and each layer's when there are several.
	outlines := make([]segment.Outline, k)
	tolerance := math.Max(outlineTolerance, outlineTolerance*l.VoxelUm/ps)
	for c := range outlines {
		outlines[c] = raster.TraceOutline(uint32(c+1), stats[c], tolerance)
		shift(&outlines[c], in.X0, in.Y0)
	}
	if l.L > 1 {
		layers := make([]segment.Layer, l.L)
		for z := range layers {
			lr := &segment.Raster{Width: l.W, Height: l.H, PixelSize: l.VoxelUm, Rows: make([][]segment.Run, l.H)}
			plane := res.Labels[z*l.W*l.H : (z+1)*l.W*l.H]
			for y := 0; y < l.H; y++ {
				lr.Rows[y] = segment.EncodeRow(plane[y*l.W : (y+1)*l.W])
			}
			ls := lr.Stats(k)
			layers[z] = segment.Layer{Z0: float32(l.Z0 + mul(float64(z), l.LayerUm)), Z1: float32(l.Z0 + mul(float64(z+1), l.LayerUm)), Outlines: make([]segment.Outline, k)}
			for c := 0; c < k; c++ {
				layers[z].Outlines[c] = lr.TraceOutline(uint32(c+1), ls[c], outlineTolerance)
				shift(&layers[z].Outlines[c], l.X0, l.Y0)
			}
		}
		if err := segment.WriteLayers(dir, layers); err != nil {
			return nil, nil, err
		}
	}

	// Diagnostics.
	diag := diagnostics(res, cells, in, idx, k)
	rec := Recorded{Model: res.Model.H, Sampler: opt.Sampler, Stages: opt.Stages, BlockUm: opt.BlockUm, Seed: opt.Seed, Samples: opt.Samples,
		Start: "the instrument's cells (cells.zarr.zip masks/1), seeded at their nuclei (masks/0)", StartStage: res.StartStage + 1, CountsMinQuality: 20}
	params, _ := json.Marshal(rec)
	nuclei := 0
	for c := 0; c < k; c++ {
		if in.NucleusUm2[res.Kept[c]] > 0 {
			nuclei++
		}
	}
	d := segment.Description{Method: Method + " " + MethodVersion, Parameters: params, Width: raster.Width, Height: raster.Height, PixelSize: ps,
		Cells: k, Transcripts: len(assignments), Genes: len(counts.Genes), Q20GeneTranscripts: q20Total, Q20GeneAssigned: q20Assigned}
	d.Nuclei.Source, d.Nuclei.Count = "the instrument's nucleus mask (cells.zarr.zip masks/0), as each cell's start and prior", nuclei
	raw, _ := json.MarshalIndent(diag, "", " ")
	for _, write := range []func() error{
		func() error { return segment.WriteCells(dir, cells) },
		func() error { return segment.WriteMask(dir, raster) },
		func() error { return segment.WriteAssignments(dir, assignments) },
		func() error { return segment.WriteCounts(dir, counts) },
		func() error { return segment.WriteOutlines(dir, outlines) },
		func() error { return segment.WriteProbabilities(dir, probability) },
		func() error {
			return os.WriteFile(filepath.Join(dir, segment.FileDiagnostics), append(raw, '\n'), 0o644)
		},
		func() error { return segment.WriteDescription(dir, d) },
	} {
		if err := write(); err != nil {
			return nil, nil, err
		}
	}
	return &d, diag, nil
}

// shift moves an outline by an origin (µm).
func shift(o *segment.Outline, x0, y0 float64) {
	if x0 == 0 && y0 == 0 {
		return
	}
	for i := range o.X {
		o.X[i] += float32(x0)
		o.Y[i] += float32(y0)
	}
}

// diagnostics summarizes a run's convergence and flags its cells' limitations.
func diagnostics(res *Result, cells []segment.Cell, in *Inputs, idx *BundleIndex, k int) *Diagnostics {
	d := &Diagnostics{Trace: res.Trace}
	s := &d.Summary
	s.Sweeps, s.SplitHalf, s.Samples = len(res.Trace), res.SplitHalf, res.Samples
	samples := min(res.Samples, len(res.Trace))
	var sum float64
	low := 0
	for _, p := range res.Probability {
		sum += float64(p)
		if p < 0.5 {
			low++
		}
	}
	n := float64(max(1, len(res.Probability)))
	s.MeanProbability, s.LowProbability = sum/n, float64(low)/n
	if samples >= 2 {
		window := res.Trace[len(res.Trace)-samples:]
		var first, second float64
		h := samples / 2
		for i, t := range window {
			if i < h {
				first += t.LogLikelihood / float64(h)
			} else {
				second += t.LogLikelihood / float64(samples-h)
			}
		}
		s.LogLikelihoodDrift = (second - first) / n
		ll := make([]float64, samples)
		for i, t := range window {
			ll[i] = t.LogLikelihood
		}
		s.GewekeZ = gewekeZ(ll)
	}
	tenth := max(1, len(res.Trace)/10)
	for _, t := range res.Trace[len(res.Trace)-tenth:] {
		s.MovedLast += float64(t.Moved) / float64(tenth)
	}
	// Limitations.
	lim := &d.Limitations
	lim.Thresholds.LowTranscripts, lim.Thresholds.UncertainShare = lowTranscripts, uncertainShare
	shares := make([]float64, k)
	voxels := make([]int, k)
	for v, c := range res.Labels {
		if c > 0 {
			shares[c-1] += float64(res.LabelShare[v])
			voxels[c-1]++
		}
	}
	lim.LowTranscripts, lim.NoNucleus, lim.Uncertain, lim.LeftOut = []string{}, []string{}, []string{}, []string{}
	for c := 0; c < k; c++ {
		if cells[c].Transcripts < lowTranscripts {
			lim.LowTranscripts = append(lim.LowTranscripts, cells[c].ID)
		}
		if in.NucleusUm2[res.Kept[c]] == 0 {
			lim.NoNucleus = append(lim.NoNucleus, cells[c].ID)
		}
		if voxels[c] == 0 || shares[c]/float64(voxels[c]) < uncertainShare {
			lim.Uncertain = append(lim.Uncertain, cells[c].ID)
		}
	}
	kept := map[int]bool{}
	for _, c := range res.Kept {
		kept[c] = true
	}
	for c, id := range idx.CellIDs {
		if !kept[c] {
			lim.LeftOut = append(lim.LeftOut, id)
		}
	}
	s.LowTranscripts, s.NoNucleus, s.Uncertain, s.LeftOut = len(lim.LowTranscripts), len(lim.NoNucleus), len(lim.Uncertain), len(lim.LeftOut)
	return d
}

// gewekeZ is Geweke's convergence diagnostic of a trace (Geweke J. Evaluating the accuracy of
// sampling-based approaches to the calculation of posterior moments. In: Bayesian Statistics 4,
// Oxford University Press 1992:169–193): the means of its first tenth and its last half compared,
// each mean's standard error from batch means (batches of a fifth of the segment, at least 2), so
// that the samples' autocorrelation is allowed for. 0 for a trace too short.
func gewekeZ(x []float64) float64 {
	n := len(x)
	a, b := x[:n/10], x[n/2:]
	meanSE := func(v []float64) (float64, float64, bool) {
		batches := 5
		size := len(v) / batches
		if size < 2 {
			return 0, 0, false
		}
		var mean float64
		means := make([]float64, batches)
		for k := range means {
			for _, y := range v[k*size : (k+1)*size] {
				means[k] += y / float64(size)
			}
			mean += means[k] / float64(batches)
		}
		var ss float64
		for _, m := range means {
			ss += (m - mean) * (m - mean)
		}
		return mean, math.Sqrt(ss / float64(batches-1) / float64(batches)), true
	}
	ma, sa, okA := meanSE(a)
	mb, sb, okB := meanSE(b)
	if !okA || !okB || sa+sb == 0 {
		return 0
	}
	return (mb - ma) / math.Sqrt(sa*sa+sb*sb)
}

// Footprint is a run's 2D segmentation at its lattice, x then y: each column's dominant label
// (Lattice.Consensus) of the summarized labels, with every hole filled. A region of the
// background enclosed by one cell (not reaching the region's edge, every neighbour that cell's)
// is the cell's: a posterior summary leaves holes where a cell's voxels happened to hold no
// transcript, but cells have none (SituCraft's choice, research.md §3.2).
func Footprint(res *Result) []uint32 {
	l := res.State.Lat
	cols := l.Consensus(res.Labels)
	seen := make([]bool, len(cols))
	var region, stack []int
	for start, c := range cols {
		if c != 0 || seen[start] {
			continue
		}
		region, stack = region[:0], append(stack[:0], start)
		seen[start] = true
		edge := false
		var around uint32
		mixed := false
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			region = append(region, p)
			x, y := p%l.W, p/l.W
			if x == 0 || y == 0 || x == l.W-1 || y == l.H-1 {
				edge = true
			}
			for _, q := range [4][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
				if q[0] < 0 || q[1] < 0 || q[0] >= l.W || q[1] >= l.H {
					continue
				}
				n := q[1]*l.W + q[0]
				switch {
				case cols[n] == 0:
					if !seen[n] {
						seen[n] = true
						stack = append(stack, n)
					}
				case around == 0:
					around = cols[n]
				case cols[n] != around:
					mixed = true
				}
			}
		}
		if !edge && !mixed && around != 0 {
			for _, p := range region {
				cols[p] = around
			}
		}
	}
	return cols
}
