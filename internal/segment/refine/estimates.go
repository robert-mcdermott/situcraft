package refine

// A refinement's cost before it runs (requirements S4): its peak memory, the disk its artifact
// takes, and its time, from what it covers. Each is linear in what drives it, its coefficients
// measured on simulated tissues of 0.02 to 0.5 mm², 120 to 3,000 genes, whole and in regions
// (research.md §3.2): memory by the bundle's transcripts and masks as they are read, the
// transcripts modelled, the voxels and the cells × genes the model holds; time by the transcripts
// and the cells × genes × components each sweep visits; disk by the bundle's transcripts
// (assignments), those modelled (probabilities), the cells (outlines, counts) and the sweeps
// (the diagnostics' trace). On the Lung 2-FOV bundle they were within 5% of the resident memory,
// 10% of the disk and 10% of the time a whole run took.

import "math"

// Coefficients (bytes, and seconds on one core), measured with the collector's target that
// refinements run with (ConserveMemory, 50): what is held through a phase counts half again for
// the garbage the collector lets accumulate, and scales with another target.
const (
	memBase         = 2 << 20 // the program's own
	memPerBatchRow  = 75.0    // a row of transcripts.parquet's row group, the columns read decoded at once
	memPerMaskPixel = 13.0    // a pixel of a mask's block, decoded while it is read
	memPerInput     = 28.0    // a modelled transcript in the inputs (place, gene, prior, row)
	memPerPixel     = 1.8     // the instrument's cells, as runs, by image pixel
	memPerModelled  = 132.0   // a modelled transcript in a run: places, attribution, tallies
	memPerVoxel     = 50.0    // a voxel in a run: its label, tally and summary
	memPerCellGene  = 17.0    // a cell's rate and count of a gene in a run
	memPerRow       = 41.0    // a row's assignment and probability, written
	memPerResult    = 27.0    // a modelled transcript's result, kept while the segmentation is written
	memPerResultVox = 10.0    // a voxel's result, likewise
	memPerCount     = 4.0     // a cell's count of a gene, written
	secPerModelled  = 490e-9  // a modelled transcript in a sweep (attributions, moves, repositioning)
	secPerCellGeneK = 23e-9   // a cell's negative binomial for a gene and a component in a sweep
	secPerRow       = 1.5e-6  // reading and writing a row of transcripts.parquet
	diskPerRow      = 1.0     // a row's assignment, compressed (0.5 to 1.6 bytes: real files' order compresses less)
	diskPerModelled = 1.6     // a modelled transcript's probability, compressed (the others' are empty)
	diskPerCount    = 0.65    // a cell's non-zero count of a gene, compressed (at most a cell × gene, or a transcript)
	diskPerCell     = 120.0   // a cell's record, outline and runs of the label image
	diskPerSweep    = 150.0   // a sweep's trace in the diagnostics, in each region
	diskBase        = 4 << 10
	parallelism     = 0.8 // the share of the cores a run keeps busy
)

// Estimate is a refinement's predicted cost.
type Estimate struct {
	Regions     int     `json:"regions"`
	Cells       int     `json:"cells"`
	Held        int     `json:"cellsHeld"`
	Transcripts int     `json:"transcripts"`
	Voxels      int64   `json:"voxels"`
	PeakBytes   int64   `json:"peakMemoryBytes"`
	DiskBytes   int64   `json:"diskBytes"`
	Seconds     float64 `json:"seconds"`
	Cores       int     `json:"cores"`
}

// EstimateCost predicts what refining the regions of a bundle's inputs with the options takes on
// a number of cores, with the collector's target. Its peak memory is the largest of three phases', the collector run between
// them (Refine): reading the bundle (a block of each mask, then a row group of its transcripts,
// decoded at once), sampling each region in turn (only one region's run held at a time), and
// writing the segmentation (a row group again, every row's assignment, the regions' results).
func EstimateCost(in *Inputs, idx *BundleIndex, regions []Region, opt Options, cores, gcPercent int) Estimate {
	e := Estimate{Regions: len(regions), Cores: max(1, cores)}
	finest := opt.Stages[len(opt.Stages)-1]
	sweeps := 0
	for _, st := range opt.Stages {
		sweeps += st.Sweeps
	}
	k := float64(opt.Model.Components)
	g := float64(in.Genes)
	var worstRegion, results, seconds, refinedGenes, modelled float64
	for _, r := range regions {
		ext := r.grown(marginUm).clipped(in.whole())
		l := in.latticeOf(ext, finest, finest)
		voxels := int64(l.Voxels())
		transcripts := 0
		t := in.Transcripts
		for i := range t.Gene {
			if ext.Contains(float64(t.X[i]), float64(t.Y[i])) {
				transcripts++
			}
		}
		cells, held := 0, 0
		for c := 0; c < in.Cells; c++ {
			switch {
			case r.Contains(in.SeedX[c], in.SeedY[c]):
				cells++
			case !in.Box[c].Empty() && in.Box[c].overlaps(ext):
				held++
			}
		}
		e.Voxels += voxels
		e.Transcripts += transcripts
		e.Cells += cells
		e.Held += held
		cg := float64(cells+held) * g
		refinedGenes += float64(cells) * g
		worstRegion = math.Max(worstRegion, memPerModelled*float64(transcripts)+memPerVoxel*float64(voxels)+memPerCellGene*cg)
		results += memPerResult*float64(transcripts) + memPerResultVox*float64(voxels)
		modelled += float64(transcripts)
		seconds += float64(sweeps) * (secPerModelled*float64(transcripts) + secPerCellGeneK*cg*k)
	}
	rows := float64(idx.Total)
	batch := memPerBatchRow * math.Min(rows, float64(max(idx.RowGroup, 1)))
	pixels := 0.0
	if in.Start != nil {
		pixels = float64(in.Start.Width) * float64(in.Start.Height)
	}
	if gcPercent <= 0 {
		gcPercent = defaultGC
	}
	slack := (1 + float64(gcPercent)/100) / (1 + defaultGC/100.0)
	held := memPerInput*float64(len(in.Transcripts.Gene)) + memPerPixel*pixels
	reading := math.Max(memPerMaskPixel*float64(idx.MaskBlock), batch+held)
	sampling := slack * (held + worstRegion)
	writing := batch + slack*(held+memPerRow*rows+results+memPerCount*refinedGenes)
	e.PeakBytes = int64(memBase + math.Max(reading, math.Max(sampling, writing)))
	counts := math.Min(float64(in.Cells)*g, float64(len(in.Transcripts.Gene)))
	e.DiskBytes = int64(diskBase + diskPerRow*rows + diskPerModelled*modelled + diskPerCount*counts + diskPerCell*float64(in.Cells) +
		diskPerSweep*float64(sweeps*len(regions)))
	e.Seconds = seconds/(parallelism*float64(e.Cores)) + secPerRow*rows
	return e
}
