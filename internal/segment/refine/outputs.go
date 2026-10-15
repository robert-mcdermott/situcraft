package refine

// A refinement's segmentation as SituCraft's segmentation files (internal/segment), so that it is
// checked, compared, viewed and exported as every segmentation is. Its cells are the instrument's,
// one for one (cell k, named ck, started from the instrument's cell k, whose id it records):
//   - the label raster at the morphology image's resolution: in each refined region, a refined
//     cell's pixels are those its 2D footprint holds (each lattice column's dominant label, holes
//     filled), and every other pixel keeps the instrument's segmentation, as do the pixels beyond
//     the regions;
//   - the outlines traced from the raster, and with more than one layer each layer's;
//   - each transcript's cell: for a modelled transcript in a refined cell, the cell it lay in most
//     often over the summarized sweeps, with that share as its probability; for every other (a
//     cell held where it was, controls, low quality, beyond the regions), the cell holding the
//     pixel it was observed in, as the instrument assigns them;
//   - the counts of gene transcripts of quality 20 or more, by those assignments;
//   - the diagnostics: each region's traces and convergence, the cells flagged for what limits
//     them (few transcripts, no nucleus, uncertain boundaries), and those left out.

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

// Recorded are a refinement's parameters as its artifact records them: every option after
// defaults (the volume prior's mean resolved), what it covered and started from.
type Recorded struct {
	Model   Hyper    `json:"model"`
	Sampler Settings `json:"sampler"`
	Stages  []Stage  `json:"stages"`
	BlockUm float64  `json:"block_um"`
	Seed    uint64   `json:"seed"`
	Samples int      `json:"samples"`
	// Area is what was asked for; Regions the rectangles refined. The caller gives the area and
	// the preset; the rest is filled from the options, the inputs and the results.
	Area    Area     `json:"area"`
	Regions []Region `json:"regions"`
	// MinQV is the quality the modelled transcripts reach; the counts are of quality 20 or more,
	// as every segmentation's.
	MinQV            float64 `json:"min_qv"`
	CountsMinQuality int     `json:"counts_min_quality"`
	// Start says what the cells started from; StartStage the stage each region started at (from 1).
	Start       string `json:"start"`
	StartStages []int  `json:"start_stages"`
	// Preset is the preset the options came from ("" for none), and the presets' version.
	Preset         string `json:"preset,omitempty"`
	PresetsVersion string `json:"presets_version,omitempty"`
	// SecondSeed is the seed of a second run checking the first (0: none), Second its results.
	SecondSeed uint64    `json:"second_seed,omitempty"`
	Second     []*Result `json:"-"`
}

// Limitations are the cells a refinement flags, by id (the instrument's ids for those left out).
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

// DiagnosticsSummary is a region's, or a refinement's, convergence and flagged cells.
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
	// errors (batch means). Within ±2 it has settled. Over several regions, the one farthest from 0.
	// GewekeJudged says whether the sweeps summarized were enough to judge (100 or more; in every
	// region).
	GewekeZ      float64 `json:"gewekeZ"`
	GewekeJudged bool    `json:"gewekeJudged"`
	// MovedLast is the voxels moved per sweep over the last tenth of the sweeps.
	MovedLast float64 `json:"voxelsMovedPerSweep"`
	// The cells refined and held, the cells flagged, by limitation, and left out.
	Refined        int `json:"cellsRefined"`
	Held           int `json:"cellsHeld"`
	LowTranscripts int `json:"cellsLowTranscripts"`
	NoNucleus      int `json:"cellsNoNucleus"`
	Uncertain      int `json:"cellsUncertainBoundaries"`
	LeftOut        int `json:"cellsLeftOut"`
	// Regions are the regions refined.
	Regions int `json:"regions"`
	// Transcripts are the modelled transcripts (in the regions' lattices).
	Transcripts int `json:"transcripts"`
	// SeedAgreement is the share of the transcripts a run with a second seed puts in the same cell,
	// of those either run puts in a refined cell (absent without a second run).
	SeedAgreement *float64 `json:"seedAgreement,omitempty"`
}

// RegionDiagnostics are one region's.
type RegionDiagnostics struct {
	Region  Region             `json:"region"`
	Summary DiagnosticsSummary `json:"summary"`
	Trace   []SweepTrace       `json:"trace"`
}

// Diagnostics is diagnostics.json.
type Diagnostics struct {
	Summary     DiagnosticsSummary  `json:"summary"`
	Limitations Limitations         `json:"limitations"`
	Regions     []RegionDiagnostics `json:"regions"`
}

// cellName is output cell k's id (k from 1).
func cellName(k int) string { return "c" + strconv.Itoa(k) }

// WriteBundleOutputs writes a refinement (its regions' results) of a bundle's inputs as a
// segmentation in dir.
func WriteBundleOutputs(b *xenium.Bundle, in *Inputs, idx *BundleIndex, results []*Result, opt Options, rec Recorded, dir string) (*segment.Description, *Diagnostics, error) {
	n := in.Cells
	img := in.Start
	ps := img.PixelSize
	// Which result refines each input cell (−1: none), and each result's footprint.
	refinedBy := make([]int, n)
	for c := range refinedBy {
		refinedBy[c] = -1
	}
	footprints := make([][]uint32, len(results))
	for r, res := range results {
		footprints[r] = Footprint(res)
		for k, c := range res.Kept {
			if !res.Frozen[k] {
				refinedBy[c] = r
			}
		}
		for _, c := range res.LeftOut {
			refinedBy[c] = r
		}
	}
	// The raster: the instrument's, each region's lattice redrawn.
	raster := &segment.Raster{Width: img.Width, Height: img.Height, PixelSize: ps, Rows: make([][]segment.Run, img.Height)}
	row := make([]uint32, img.Width)
	for y := 0; y < img.Height; y++ {
		img.Row(y, row)
		cyUm := in.Y0 + mul(float64(y), ps) + ps/2
		for r, res := range results {
			l := res.State.Lat
			cy := int(math.Floor((cyUm - l.Y0) / l.VoxelUm))
			if cy < 0 || cy >= l.H {
				continue
			}
			fp := footprints[r]
			for x := range row {
				cx := int(math.Floor((in.X0 + mul(float64(x), ps) + ps/2 - l.X0) / l.VoxelUm))
				if cx < 0 || cx >= l.W {
					continue
				}
				if f := fp[cy*l.W+cx]; f > 0 && !res.Frozen[f-1] {
					row[x] = uint32(res.Kept[f-1] + 1)
				} else if c := row[x]; c > 0 && refinedBy[c-1] == r {
					row[x] = 0 // a refined cell's pixel its footprint gave up
				}
			}
		}
		raster.Rows[y] = segment.EncodeRow(row)
	}
	stats := raster.Stats(n)

	// Where each modelled transcript was refined: its result and index there.
	type place struct{ result, index int32 }
	modelledAt := make([]place, len(in.Transcripts.Gene))
	for i := range modelledAt {
		modelledAt[i] = place{-1, -1}
	}
	for r, res := range results {
		for i, j := range res.Index {
			modelledAt[j] = place{int32(r), int32(i)}
		}
	}
	rowOf := make([]int32, idx.Total)
	for r := range rowOf {
		rowOf[r] = -1
	}
	for i, r := range idx.Rows {
		rowOf[r] = int32(i)
	}
	// Assignments, every row of transcripts.parquet; counts of gene transcripts of quality 20 or
	// more by gene.
	assignments := make([]uint32, idx.Total)
	probability := make([]float32, idx.Total)
	nan := float32(math.NaN())
	tr, err := b.OpenTranscripts()
	if err != nil {
		return nil, nil, err
	}
	tr.Only("feature_name", "qv", "x_location", "y_location")
	genes := append([]string(nil), idx.Genes...)
	geneIndex := make(map[string]uint32, len(genes))
	for g, name := range genes {
		geneIndex[name] = uint32(g)
	}
	var q20Total, q20Assigned int64
	pairs := map[uint64]uint32{}
	row0 := 0
	err = tr.Each(func(batch *xenium.TranscriptBatch) error {
		for i := 0; i < batch.Rows; i++ {
			r := row0 + i
			c := uint32(0)
			p := nan
			assigned := false
			if j := rowOf[r]; j >= 0 {
				if at := modelledAt[j]; at.result >= 0 {
					res := results[at.result]
					f := res.Assigned[at.index]
					switch {
					case f > 0 && !res.Frozen[f-1]:
						c, p, assigned = uint32(res.Kept[f-1]+1), res.Probability[at.index], true
					case f == 0:
						// The background, most often: so it stays, unless it lies on a held
						// cell's pixel (assigned by place, below).
						if pix := raster.AtMicrons(float64(batch.X[i])-in.X0, float64(batch.Y[i])-in.Y0); pix == 0 || refinedBy[pix-1] == int(at.result) {
							c, p, assigned = 0, res.Probability[at.index], true
						}
					}
				}
			}
			if !assigned {
				c = raster.AtMicrons(float64(batch.X[i])-in.X0, float64(batch.Y[i])-in.Y0)
			}
			assignments[r], probability[r] = c, p
			name := string(batch.Feature[i])
			if batch.QV[i] >= 20 && xenium.FeatureKind(name) == xenium.KindGene {
				q20Total++
				if c > 0 {
					g, ok := geneIndex[name]
					if !ok {
						g = uint32(len(genes))
						geneIndex[name] = g
						genes = append(genes, name)
					}
					q20Assigned++
					pairs[uint64(c-1)<<32|uint64(g)]++
				}
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
	cells := make([]segment.Cell, n)
	px2 := mul(ps, ps)
	for c := range cells {
		cell := &cells[c]
		cell.ID = cellName(c + 1)
		cell.Nucleus = idx.CellIDs[c]
		cell.NucleusArea = float32(in.NucleusUm2[c])
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
	counts := &segment.Counts{Genes: genes, Offsets: make([]uint32, n+1)}
	for _, key := range keys {
		c, g := int(key>>32), uint32(key)
		counts.Gene = append(counts.Gene, g)
		counts.Count = append(counts.Count, pairs[key])
		counts.Offsets[c+1]++
		cells[c].Transcripts += pairs[key]
		cells[c].Genes++
	}
	for c := 1; c <= n; c++ {
		counts.Offsets[c] += counts.Offsets[c-1]
	}

	// Outlines: the raster's, simplified to half a voxel; and each layer's in the refined regions.
	finest := opt.Stages[len(opt.Stages)-1]
	outlines := make([]segment.Outline, n)
	tolerance := math.Max(outlineTolerance, outlineTolerance*finest.VoxelUm/ps)
	for c := range outlines {
		outlines[c] = raster.TraceOutline(uint32(c+1), stats[c], tolerance)
		shift(&outlines[c], in.X0, in.Y0)
	}
	if finest.Layers > 1 {
		layers := make([]segment.Layer, finest.Layers)
		for z := range layers {
			layers[z].Outlines = make([]segment.Outline, n)
		}
		for _, res := range results {
			l := res.State.Lat
			for z := 0; z < l.L; z++ {
				layers[z].Z0, layers[z].Z1 = float32(l.Z0+mul(float64(z), l.LayerUm)), float32(l.Z0+mul(float64(z+1), l.LayerUm))
				lr := &segment.Raster{Width: l.W, Height: l.H, PixelSize: l.VoxelUm, Rows: make([][]segment.Run, l.H)}
				plane := res.Labels[z*l.W*l.H : (z+1)*l.W*l.H]
				for y := 0; y < l.H; y++ {
					lr.Rows[y] = segment.EncodeRow(plane[y*l.W : (y+1)*l.W])
				}
				ls := lr.Stats(len(res.Kept))
				for k, c := range res.Kept {
					if res.Frozen[k] {
						continue
					}
					o := lr.TraceOutline(uint32(k+1), ls[k], outlineTolerance)
					shift(&o, l.X0, l.Y0)
					layers[z].Outlines[c] = o
				}
			}
		}
		if err := segment.WriteLayers(dir, layers); err != nil {
			return nil, nil, err
		}
	}

	// Diagnostics, and the description.
	diag := diagnostics(results, cells, in, idx)
	if len(rec.Second) == len(results) && len(results) > 0 {
		var same, all int
		for r, res := range results {
			sr, ar := agreement(res, rec.Second[r])
			x := float64(sr) / float64(max(ar, 1))
			diag.Regions[r].Summary.SeedAgreement = &x
			same += sr
			all += ar
		}
		x := float64(same) / float64(max(all, 1))
		diag.Summary.SeedAgreement = &x
	} else {
		rec.SecondSeed = 0
	}
	rec.Regions = nil
	rec.StartStages = nil
	for _, res := range results {
		rec.Regions = append(rec.Regions, res.Region)
		rec.StartStages = append(rec.StartStages, res.StartStage+1)
	}
	if len(results) > 0 {
		rec.Model = results[0].Model.H
	}
	rec.Sampler, rec.Stages, rec.BlockUm, rec.Seed, rec.Samples = opt.Sampler, opt.Stages, opt.BlockUm, opt.Seed, opt.Samples
	rec.MinQV, rec.CountsMinQuality = in.MinQV, 20
	rec.Start = "the instrument's cells (cells.zarr.zip masks/1), seeded at their nuclei (masks/0)"
	if rec.Area.Mode == "" {
		rec.Area.Mode = AreaWhole
	}
	params, _ := json.Marshal(rec)
	nuclei := 0
	for c := 0; c < n; c++ {
		if in.NucleusUm2[c] > 0 {
			nuclei++
		}
	}
	d := segment.Description{Method: Method + " " + MethodVersion, Parameters: params, Width: raster.Width, Height: raster.Height, PixelSize: ps,
		Cells: n, Transcripts: len(assignments), Genes: len(counts.Genes), Q20GeneTranscripts: q20Total, Q20GeneAssigned: q20Assigned}
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
			if f := Classify(err); f != nil {
				return nil, nil, f
			}
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

// regionSummary summarizes one region's convergence and flags its refined cells.
func regionSummary(res *Result, cells []segment.Cell, in *Inputs, lim *Limitations) DiagnosticsSummary {
	var s DiagnosticsSummary
	s.Sweeps, s.SplitHalf, s.Samples, s.Regions, s.Transcripts = len(res.Trace), res.SplitHalf, res.Samples, 1, len(res.Assigned)
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
		ll := make([]float64, samples)
		for i, t := range window {
			ll[i] = t.LogLikelihood
			if i < h {
				first += t.LogLikelihood / float64(h)
			} else {
				second += t.LogLikelihood / float64(samples-h)
			}
		}
		s.LogLikelihoodDrift = (second - first) / n
		s.GewekeZ, s.GewekeJudged = gewekeZ(ll)
	}
	tenth := max(1, len(res.Trace)/10)
	for _, t := range res.Trace[len(res.Trace)-tenth:] {
		s.MovedLast += float64(t.Moved) / float64(tenth)
	}
	// The refined cells' limitations.
	shares := make([]float64, len(res.Kept))
	voxels := make([]int, len(res.Kept))
	for v, c := range res.Labels {
		if c > 0 {
			shares[c-1] += float64(res.LabelShare[v])
			voxels[c-1]++
		}
	}
	for k, c := range res.Kept {
		if res.Frozen[k] {
			s.Held++
			continue
		}
		s.Refined++
		id := cells[c].ID
		if cells[c].Transcripts < lowTranscripts {
			lim.LowTranscripts = append(lim.LowTranscripts, id)
			s.LowTranscripts++
		}
		if in.NucleusUm2[c] == 0 {
			lim.NoNucleus = append(lim.NoNucleus, id)
			s.NoNucleus++
		}
		if voxels[k] == 0 || shares[k]/float64(voxels[k]) < uncertainShare {
			lim.Uncertain = append(lim.Uncertain, id)
			s.Uncertain++
		}
	}
	s.LeftOut = len(res.LeftOut)
	return s
}

// diagnostics summarizes a refinement: each region's convergence, the whole (weighted by each
// region's modelled transcripts; Geweke's z the one farthest from 0), and the cells flagged.
func diagnostics(results []*Result, cells []segment.Cell, in *Inputs, idx *BundleIndex) *Diagnostics {
	d := &Diagnostics{}
	lim := &d.Limitations
	lim.Thresholds.LowTranscripts, lim.Thresholds.UncertainShare = lowTranscripts, uncertainShare
	lim.LowTranscripts, lim.NoNucleus, lim.Uncertain, lim.LeftOut = []string{}, []string{}, []string{}, []string{}
	all := &d.Summary
	all.GewekeJudged = len(results) > 0
	var weight float64
	for _, res := range results {
		s := regionSummary(res, cells, in, lim)
		for _, c := range res.LeftOut {
			lim.LeftOut = append(lim.LeftOut, idx.CellIDs[c])
		}
		d.Regions = append(d.Regions, RegionDiagnostics{Region: res.Region, Summary: s, Trace: res.Trace})
		w := float64(s.Transcripts)
		weight += w
		all.Sweeps, all.Samples = s.Sweeps, s.Samples
		all.SplitHalf += w * s.SplitHalf
		all.MeanProbability += w * s.MeanProbability
		all.LowProbability += w * s.LowProbability
		all.LogLikelihoodDrift += w * s.LogLikelihoodDrift
		if math.Abs(s.GewekeZ) > math.Abs(all.GewekeZ) {
			all.GewekeZ = s.GewekeZ
		}
		all.GewekeJudged = all.GewekeJudged && s.GewekeJudged
		all.MovedLast += s.MovedLast
		all.Refined += s.Refined
		all.Held += s.Held
		all.LowTranscripts += s.LowTranscripts
		all.NoNucleus += s.NoNucleus
		all.Uncertain += s.Uncertain
		all.LeftOut += s.LeftOut
		all.Regions++
		all.Transcripts += s.Transcripts
	}
	if weight > 0 {
		all.SplitHalf /= weight
		all.MeanProbability /= weight
		all.LowProbability /= weight
		all.LogLikelihoodDrift /= weight
	}
	return d
}

// agreement counts the modelled transcripts two runs of a region put in the same cell (or both
// in the background), of those either puts in a refined cell.
func agreement(a, b *Result) (same, all int) {
	cellOf := func(res *Result, i int) (int, bool) {
		k := res.Assigned[i]
		if k == 0 {
			return -1, false
		}
		return res.Kept[k-1], !res.Frozen[k-1]
	}
	for i := range a.Assigned {
		ca, fa := cellOf(a, i)
		cb, fb := cellOf(b, i)
		if !fa && !fb {
			continue
		}
		all++
		if ca == cb {
			same++
		}
	}
	return same, all
}

// gewekeZ is Geweke's convergence diagnostic of a trace (Geweke J. Evaluating the accuracy of
// sampling-based approaches to the calculation of posterior moments. In: Bayesian Statistics 4,
// Oxford University Press 1992:169–193): the means of its first tenth and its last half compared,
// each mean's standard error from batch means (batches of a fifth of the segment, at least 2), so
// that the samples' autocorrelation is allowed for. It is not judged (0, false) for a trace too
// short, under minGeweke samples; a constant trace has settled (0, true).
func gewekeZ(x []float64) (float64, bool) {
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
	if !okA || !okB {
		return 0, false
	}
	if sa+sb == 0 {
		return 0, true
	}
	return (mb - ma) / math.Sqrt(sa*sa+sb*sb), true
}

// minGeweke is the fewest samples Geweke's diagnostic judges: a first tenth of five batches of
// two.
const minGeweke = 100

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
