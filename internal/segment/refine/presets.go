package refine

// Presets (requirements S3): named starting points for a refinement's settings, each with what it
// is for, what it changes and what it risks, and every setting it makes shown. Changing any
// setting but the seed makes a run Expert. The settings' help says, in plain language, what each
// does and what it costs. Each preset was measured on the standard simulated tissue (research.md
// §3.2, the segmentation studio; TestPresetsOnSimulatedTissue).

// PresetsVersion is the presets' version, recorded with each refinement made from one: a preset
// whose settings change gets a new version.
const PresetsVersion = "1"

// Preset ids.
const (
	PresetBalanced     = "balanced"
	PresetConservative = "conservative"
	PresetFlexible     = "flexible"
	PresetLowMemory    = "low-memory"
	PresetPilot        = "roi-pilot"
	PresetExpert       = "expert"
)

// Preset is a named starting point.
type Preset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// About is what it is for; Changes what it changes from Balanced; Warning what it risks.
	About   string `json:"about"`
	Changes string `json:"changes"`
	Warning string `json:"warning"`
	// Measured is how it did on the standard simulated tissue.
	Measured string  `json:"measured"`
	Options  Options `json:"options"`
	// Area is the area it needs ("" any).
	Area string `json:"area,omitempty"`
	// SeedCheck runs a second seed and reports the two runs' agreement.
	SeedCheck bool `json:"seedCheck"`
	// Expert opens every setting.
	Expert bool `json:"expert"`
	// GCPercent is the garbage collector's target while it runs (0: 50; ConserveMemory): it
	// changes the memory and time a run takes, never its result.
	GCPercent int `json:"gcPercent,omitempty"`
}

// Presets are the presets, Balanced first.
func Presets() []Preset {
	balanced := DefaultOptions()

	conservative := DefaultOptions()
	conservative.Sampler.PerimeterFactor = 1.3
	conservative.Sampler.Bubble = 0.1
	conservative.Model.NuclearPrior = 0.9

	flexible := DefaultOptions()
	flexible.Sampler.PerimeterFactor = 2.5
	flexible.Model.NuclearPrior = 0.7

	low := DefaultOptions()
	low.Stages = []Stage{{2, 1, 600}}

	return []Preset{
		{ID: PresetBalanced, Name: "Balanced", Options: balanced,
			About:    "SituCraft's defaults: compact outlines that still admit elongated cells. Start here.",
			Changes:  "Nothing: 1 µm voxels in one layer, 600 sweeps with the last 500 summarized, molecules allowed to have drifted, outlines within 1.6 times the most compact shape's, a nuclear prior of 0.8.",
			Warning:  "The result is a summary of samples from a model, not a measurement: review it against the instrument's cells before relying on it.",
			Measured: "On the standard simulated tissue: 86.3% of transcripts in their true cell and a median IoU of 0.712, against the instrument's 82.5% and 0.622, in about 20 seconds on ten cores."},
		{ID: PresetConservative, Name: "Conservative boundary", Options: conservative,
			About:    "For cells that should stay compact and near their nuclei: tissues of rounded cells, or where the instrument's boundaries are trusted and only refined a little.",
			Changes:  "Outlines within 1.3 times the most compact shape's (Balanced 1.6), gaps proposed twice as often (0.1), and transcripts over a nucleus held to its cell more strongly (nuclear prior 0.9).",
			Warning:  "Elongated and irregular cells are held to compact outlines: where real cells have processes, their transcripts may go to a neighbour or to the background.",
			Measured: "On the standard simulated tissue, whose cells are mostly compact: 86.2% of transcripts in their true cell and a median IoU of 0.727, outlines 1.21 times the most compact (Balanced 1.32)."},
		{ID: PresetFlexible, Name: "Flexible morphology", Options: flexible,
			About:    "For elongated or irregular cells (fibroblasts, neurons' processes, muscle) that compact outlines cut short.",
			Changes:  "Outlines within 2.5 times the most compact shape's (Balanced 1.6), and a weaker nuclear prior (0.7).",
			Warning:  "Boundaries follow expression more freely: where neighbouring cells differ in expression, a cell may grow along the transcripts it fits and distort its shape.",
			Measured: "On the standard simulated tissue: 85.0% of transcripts in their true cell and a median IoU of 0.638, outlines 1.63 times the most compact: its mostly compact cells are drawn less well than by Balanced."},
		{ID: PresetLowMemory, Name: "Low memory", Options: low, GCPercent: lowMemoryGC,
			About:    "For a computer short of memory: a coarser grid, and memory collected more often.",
			Changes:  "2 µm voxels (Balanced 1 µm), a quarter as many, and the program's memory kept within a fifth more than it holds (Balanced: half more).",
			Warning:  "Lower detail and accuracy: boundaries are drawn on a coarser grid. Most memory goes with the transcripts, so for a tissue far too large, refine its sections or regions one at a time instead.",
			Measured: "On the standard simulated tissue: about a fifth less memory than Balanced (73 MB against 89), in about the same time; 84.9% of transcripts in their true cell and a median IoU of 0.650."},
		{ID: PresetPilot, Name: "ROI pilot", Options: balanced, Area: AreaRegions, SeedCheck: true,
			About:    "Try the model on a region before the whole tissue: quick, and run twice with different seeds to show how much of the result is chance.",
			Changes:  "Balanced's settings on a region you choose; a second run with the next seed, reported as the share of transcripts both runs put in the same cell.",
			Warning:  "A region may not represent the whole tissue: choose one with the cell types and densities that matter.",
			Measured: "On the standard simulated tissue, a 200 µm square in its middle: 86.9% of its transcripts in their true cell and a median IoU of 0.719, as the whole tissue's run gave there (87.1%, 0.716); the two seeds agreed on 93.4% of transcripts."},
		{ID: PresetExpert, Name: "Expert", Options: balanced, Expert: true,
			About:   "Every setting open, starting from Balanced's.",
			Changes: "Whatever you change.",
			Warning: "Settings away from the validated presets have not been checked against simulated truth: you take responsibility for them, and the run records every one."},
	}
}

// lowMemoryGC is Low memory's target for the garbage collector: the heap within a fifth more than
// its live size.
const lowMemoryGC = 20

// PresetByID finds a preset.
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// Setting is one setting's plain-language help.
type Setting struct {
	// Key is its path in the options (or the spec, for quality, area and seed check).
	Key  string `json:"key"`
	Name string `json:"name"`
	Unit string `json:"unit,omitempty"`
	// Help is what it does; Cost what it costs; Expert whether only Expert opens it.
	Help   string `json:"help"`
	Cost   string `json:"cost,omitempty"`
	Expert bool   `json:"expert"`
	// Min and Max bound it (both 0: no bound given).
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// SettingsHelp is every setting's help, in the order the studio shows them.
func SettingsHelp() []Setting {
	return []Setting{
		{Key: "min_qv", Name: "Transcript quality", Unit: "Q", Min: 0, Max: 40,
			Help: "Only transcripts whose quality score reaches this are modelled; the rest keep the cell of the pixel they were observed in. Q20 means a 1 in 100 chance the gene was misread. The cells' counts are always of Q20 or more, as every segmentation's.",
			Cost: "Memory and time grow with the transcripts modelled."},
		{Key: "seed", Name: "Seed", Min: 0, Max: 1 << 53,
			Help: "Starts the random number streams. The same seed and settings give the same segmentation, byte for byte, on any computer and any number of threads; another seed shows how much of the result is chance.",
			Cost: "None."},
		{Key: "stages.voxel_um", Name: "Voxel size", Unit: "µm", Min: 0.25, Max: 16,
			Help: "The grid cells are drawn on. Finer voxels draw finer boundaries, but each holds fewer transcripts, so less evidence decides it. Stages go from coarse to fine, each voxel a whole number of the next stage's.",
			Cost: "Memory and time per sweep grow with the voxels: halving the size makes four times as many."},
		{Key: "stages.layers", Name: "Layers", Min: 1, Max: 16,
			Help: "Slices the section's depth is cut into. More than one lets cells lie over one another; a section of 5 to 10 µm holds about one layer of cells. Each stage's layers are a whole multiple of the previous stage's.",
			Cost: "Voxels, and so memory and time, grow with the layers."},
		{Key: "stages.sweeps", Name: "Sweeps", Min: 10, Max: 100000,
			Help: "How many times the sampler revisits every voxel, transcript and parameter. More let the boundaries settle further; the convergence diagnostic says whether they have.",
			Cost: "Time grows in proportion; memory does not."},
		{Key: "samples", Name: "Sweeps summarized", Min: 1, Max: 65535,
			Help: "The last sweeps counted for the result: each voxel's most frequent cell, and each transcript's with its probability. More give a steadier summary; the sweeps before them let the run settle from its start.",
			Cost: "None beyond the sweeps. At most the last stage's sweeps."},
		{Key: "sampler.perimeter_factor", Name: "Perimeter bound", Min: 1, Max: 10,
			Help: "How far a cell's outline may stray from the most compact shape of its area. 1.6 admits an ellipse four times as long as it is wide, at any angle; lower keeps outlines compact, higher lets them grow ragged along transcripts.",
			Cost: "None."},
		{Key: "model.nuclear_prior", Name: "Nuclear prior", Min: 0.01, Max: 0.99,
			Help: "The prior probability that a transcript over a nucleus belongs to that nucleus's cell. Higher holds cells to their nuclei; lower lets expression decide.",
			Cost: "None."},
		{Key: "sampler.reposition", Name: "Molecule drift",
			Help: "Lets each transcript's true place differ from where it was observed, as molecules drift a little before they are fixed. Off, every transcript counts where it was observed.",
			Cost: "A little time and memory per transcript."},
		{Key: "sampler.bubble", Name: "Gap proposals", Min: 0, Max: 0.5, Expert: true,
			Help: "How often a move proposes the background instead of a neighbouring cell, which lets gaps open between cells.",
			Cost: "None."},
		{Key: "sampler.diffusion_wide_share", Name: "Far drift share", Min: 0, Max: 1, Expert: true,
			Help: "The share of molecules that drift far (the rest drift a little).",
			Cost: "None."},
		{Key: "sampler.diffusion_wide_um", Name: "Far drift", Unit: "µm", Min: 0.1, Max: 20, Expert: true,
			Help: "The standard deviation of a far drift.",
			Cost: "None."},
		{Key: "sampler.diffusion_narrow_um", Name: "Near drift", Unit: "µm", Min: 0.05, Max: 5, Expert: true,
			Help: "The standard deviation of a near drift.",
			Cost: "None."},
		{Key: "model.components", Name: "Expression states", Min: 1, Max: 1000, Expert: true,
			Help: "How many expression patterns, roughly cell types, the model uses to judge whether a transcript fits a cell. Too few blur types together; unused ones cost time only.",
			Cost: "Time per sweep grows with the states × cells × genes."},
		{Key: "block_um", Name: "Parallel block", Unit: "µm", Min: 2, Max: 1000, Expert: true,
			Help: "The side of the squares updated in parallel. The result does not depend on the threads, but does on this.",
			Cost: "Small blocks keep more threads busy on a small region."},
		{Key: "model.exact", Name: "Exact volume prior", Expert: true,
			Help: "Holds one volume prior for every expression state, which makes the sampler exact (research.md §3.2, the one approximation). Off, each state has its own.",
			Cost: "None."},
		{Key: "gc_percent", Name: "Memory target", Unit: "%", Min: 1, Max: 400, Expert: true,
			Help: "How far the program's memory may grow past what it holds before it collects what it no longer needs (50 by default, 20 for Low memory). It never changes the result.",
			Cost: "Lower takes less memory and a little more time."},
		{Key: "seed_check", Name: "Second seed",
			Help: "Runs again with the next seed and reports the share of transcripts both runs put in the same cell: how much of the result is chance.",
			Cost: "Twice the time."},
	}
}
