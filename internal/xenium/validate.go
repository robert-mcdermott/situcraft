package xenium

// Validation: whether a bundle is complete, internally consistent and usable, as findings that keep
// apart what stops its use (fatal), what may bias an analysis (warning) and what is only worth
// knowing (info), each naming the files and tissue region it concerns and what to do. The checks
// compare the bundle with itself: every table that records the same thing (cells, counts, the
// matrix's copies, polygons, the run's metrics) must agree, coordinates must lie on the images,
// and the images must agree with the manifest. Nothing is written to the bundle.

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"situcraft/internal/zarr"
)

// Severity of a finding.
type Severity string

const (
	Fatal   Severity = "fatal"
	Warning Severity = "warning"
	Info    Severity = "info"
)

// Finding is one result of validation.
type Finding struct {
	Severity Severity `json:"severity"`
	// Code is stable across versions ("transcripts.unknown-cell"), for scripts and documentation.
	Code   string   `json:"code"`
	Title  string   `json:"title"`
	Detail string   `json:"detail,omitempty"`
	Files  []string `json:"files,omitempty"`
	Count  int64    `json:"count,omitempty"`
	// Region is where in the tissue (µm) the finding's items lie, when they lie somewhere.
	Region *Region `json:"region,omitempty"`
	Remedy string  `json:"remedy,omitempty"`
}

// Summary is what the bundle holds, as validation counted it.
type Summary struct {
	Run                 string    `json:"run,omitempty"`
	Region              string    `json:"region,omitempty"`
	RunStart            string    `json:"runStart,omitempty"`
	Software            string    `json:"software,omitempty"`
	Panel               string    `json:"panel,omitempty"`
	Organism            string    `json:"organism,omitempty"`
	Tissue              string    `json:"tissue,omitempty"`
	Preservation        string    `json:"preservation,omitempty"`
	PixelSize           float64   `json:"pixelSizeUm,omitempty"`
	ExtentUm            []float64 `json:"extentUm,omitempty"`
	ImageSize           []int     `json:"imageSizePx,omitempty"`
	Channels            []string  `json:"channels,omitempty"`
	ZPlanes             int       `json:"zPlanes,omitempty"`
	Cells               int       `json:"cells"`
	Transcripts         int64     `json:"transcripts"`
	Q20GeneTranscripts  int64     `json:"q20GeneTranscripts"`
	AssignedFraction    float64   `json:"assignedFraction"`
	MedianPerCell       float64   `json:"medianTranscriptsPerCell"`
	PanelGenes          int       `json:"panelGenes"`
	ObservedGenes       int       `json:"observedGenes"`
	ControlTranscripts  int64     `json:"controlTranscripts"`
	MetricsCompared     int       `json:"metricsCompared"`
	MetricsAgreeing     int       `json:"metricsAgreeing"`
	FreeCacheBytes      int64     `json:"freeCacheBytes,omitempty"`
	EstimatedCacheBytes int64     `json:"estimatedCacheBytes,omitempty"`
	BundleBytes         int64     `json:"bundleBytes"`
	Zipped              bool      `json:"zipped,omitempty"`
	SegmentationMethods []string  `json:"segmentationMethods,omitempty"`
	// SegmentationCounts are the cells each method segmented (from cells.zarr.zip), in order.
	SegmentationCounts   []int64 `json:"segmentationCounts,omitempty"`
	TranscriptRowGroups  int     `json:"transcriptRowGroups,omitempty"`
	CheckDurationSeconds float64 `json:"checkDurationSeconds"`
}

// Report is a bundle's validation.
type Report struct {
	Bundle         string `json:"bundle"`
	Version        string `json:"version"`
	Support        string `json:"support"`
	SupportMessage string `json:"supportMessage"`
	Inventory      []Item `json:"inventory"`
	// ExperimentUUID and AnalysisUUID name the run and its analysis (from experiment.xenium).
	ExperimentUUID string `json:"experimentUuid,omitempty"`
	AnalysisUUID   string `json:"analysisUuid,omitempty"`
	// Fingerprint identifies the bundle's files as they were checked.
	Fingerprint []FileFingerprint `json:"fingerprint"`
	Findings    []Finding         `json:"findings"`
	Summary     Summary           `json:"summary"`
	Checked     string            `json:"checked"`
}

// Count is the number of findings of a severity.
func (r *Report) Count(s Severity) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == s {
			n++
		}
	}
	return n
}

// Usable reports whether the bundle can be analyzed (no fatal finding).
func (r *Report) Usable() bool { return r.Count(Fatal) == 0 }

// ValidateOptions tune validation.
type ValidateOptions struct {
	// CacheDir is where derived data will go: its free space is reported.
	CacheDir string
	// Progress, when set, is told what is being checked.
	Progress func(fraction float64, message string)
}

// Tolerances: coordinates may stray this far (µm) past the image without a warning (the bundles
// in hand reach 0.35 µm past it); recorded fractions and areas are compared this closely.
const (
	edgeToleranceUm   = 5.0
	relativeTolerance = 1e-6
)

type validator struct {
	b       *Bundle
	ctx     context.Context
	opts    ValidateOptions
	report  *Report
	cells   *Cells
	extentX float64 // µm; 0 when unknown
	extentY float64
	// stats are the transcripts' totals, for the metrics.
	stats transcriptStats
}

func (v *validator) add(f Finding) { v.report.Findings = append(v.report.Findings, f) }

func (v *validator) progress(fraction float64, message string) {
	if v.opts.Progress != nil {
		v.opts.Progress(fraction, message)
	}
}

// Validate checks a bundle. Problems become findings; the error is only for a cancelled context.
func Validate(ctx context.Context, b *Bundle, opts ValidateOptions) (*Report, error) {
	start := time.Now()
	level, message := b.Support()
	r := &Report{Bundle: b.Name, Version: b.Version.String(), Support: level, SupportMessage: message, Inventory: b.Inventory(), Findings: []Finding{},
		ExperimentUUID: b.Experiment.ExperimentUUID, AnalysisUUID: b.Experiment.AnalysisUUID, Fingerprint: b.Fingerprint()}
	v := &validator{b: b, ctx: ctx, opts: opts, report: r}
	v.summary()
	steps := []struct {
		name string
		fn   func() error
	}{
		{"files", v.checkInventory},
		{"manifest", v.checkManifest},
		{"images", v.checkImages},
		{"cells", v.checkCells},
		{"transcripts", v.checkTranscripts},
		{"matrix", v.checkMatrix},
		{"cells.zarr", v.checkCellsZarr},
		{"boundaries", v.checkBoundaries},
		{"metrics", v.checkMetrics},
		{"disk", v.checkDisk},
	}
	for i, step := range steps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		v.progress(float64(i)/float64(len(steps)), "Checking "+step.name)
		if err := step.fn(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
	}
	sortFindings(r.Findings)
	r.Summary.CheckDurationSeconds = math.Round(time.Since(start).Seconds()*100) / 100
	r.Checked = time.Now().UTC().Format(time.RFC3339)
	v.progress(1, "Checked")
	return r, nil
}

func sortFindings(f []Finding) {
	rank := map[Severity]int{Fatal: 0, Warning: 1, Info: 2}
	sort.SliceStable(f, func(i, j int) bool { return rank[f[i].Severity] < rank[f[j].Severity] })
}

func (v *validator) summary() {
	e, s := v.b.Experiment, &v.report.Summary
	s.Run, s.Region, s.RunStart, s.Software = e.RunName, e.RegionName, e.RunStartTime, e.AnalysisSoftware
	s.Panel, s.Organism, s.Tissue, s.Preservation = e.PanelName, e.PanelOrganism, e.PanelTissueType, e.PreservationMethod
	s.PixelSize, s.Zipped = e.PixelSize, v.b.Zipped
	for _, f := range v.b.Files {
		s.BundleBytes += f.Size
	}
	if v.b.Panel != nil {
		s.PanelGenes = len(v.b.Panel.Genes())
	}
}

func (v *validator) checkInventory() error {
	for _, item := range v.report.Inventory {
		switch {
		case item.Required && len(item.Files) == 0:
			v.add(Finding{Severity: Fatal, Code: "files.missing", Title: fmt.Sprintf("%s missing", item.Description),
				Detail: fmt.Sprintf("The bundle has no %s, which SituCraft needs.", strings.ToLower(item.Description)),
				Remedy: "Use the complete output folder (or zip) as the instrument or Xenium Ranger wrote it; files may have been left behind when it was copied."})
		case item.Note != "":
			v.add(Finding{Severity: Fatal, Code: "files.unread-form", Title: fmt.Sprintf("%s: only a form SituCraft does not read", item.Description), Detail: item.Note, Files: item.Files,
				Remedy: "Bundles from Xenium Onboard Analysis 2 and later include the Parquet and Zarr forms; re-export with a current version, or convert the file."})
		}
	}
	for _, p := range v.b.Problems {
		v.add(Finding{Severity: Warning, Code: "files.unreadable", Title: "A description file could not be read", Detail: p,
			Remedy: "The checks that use it are skipped; the rest of the bundle is unaffected."})
	}
	if v.b.Metrics == nil && !v.b.Has("metrics_summary.csv") {
		v.add(Finding{Severity: Info, Code: "files.no-metrics", Title: "No run metrics (metrics_summary.csv)", Detail: "The run's own metrics cannot be compared with its data."})
	}
	if v.b.Zipped {
		var external int64
		for _, f := range v.b.Files {
			if f.External {
				external += f.Size
			}
		}
		if external > 0 {
			v.add(Finding{Severity: Info, Code: "files.zipped", Title: "Opened from a zip",
				Detail: fmt.Sprintf("Compressed members (%s in all) are unpacked into SituCraft's cache as they are read; the zip itself is never changed.", humanBytes(external)),
				Remedy: "Unzipping the bundle once and opening the folder avoids the copy."})
		}
	}
	if level := v.report.Support; level != "supported" {
		v.add(Finding{Severity: Warning, Code: "version.untested", Title: "A version SituCraft has not been checked against", Detail: v.report.SupportMessage,
			Remedy: "Read the rest of this report with care, and report anything that looks wrong."})
	}
	return nil
}

func (v *validator) checkManifest() error {
	e := v.b.Experiment
	switch {
	case e.PixelSize <= 0 || math.IsNaN(e.PixelSize):
		v.add(Finding{Severity: Fatal, Code: "manifest.pixel-size", Title: "No pixel size", Files: []string{manifestName},
			Detail: "experiment.xenium gives no pixel size, so images cannot be placed with the transcripts.", Remedy: "Use the experiment.xenium the instrument wrote."})
	case e.PixelSize < 0.05 || e.PixelSize > 5:
		v.add(Finding{Severity: Warning, Code: "manifest.pixel-size", Title: "An implausible pixel size", Files: []string{manifestName},
			Detail: fmt.Sprintf("experiment.xenium gives %g µm per pixel; Xenium images are about 0.2125 µm per pixel.", e.PixelSize)})
	}
	return nil
}

func (v *validator) checkImages() error {
	img, err := v.b.FocusImage()
	if err != nil {
		v.add(Finding{Severity: Fatal, Code: "images.unreadable", Title: "The morphology images cannot be read", Detail: err.Error(), Files: v.b.focusFiles(),
			Remedy: "Check that every morphology_focus file was copied whole."})
		return nil
	}
	if img == nil {
		return nil // reported missing by the inventory
	}
	s := &v.report.Summary
	first := img.Channels[0]
	s.ImageSize = []int{first.Levels[0].Width, first.Levels[0].Height}
	px := v.b.Experiment.PixelSize
	if first.PixelSize > 0 && px > 0 && math.Abs(first.PixelSize-px) > px*1e-4 {
		v.add(Finding{Severity: Warning, Code: "images.pixel-size", Title: "The images and the manifest disagree on the pixel size",
			Detail: fmt.Sprintf("The OME-XML says %g µm, experiment.xenium %g µm; SituCraft uses the manifest's.", first.PixelSize, px), Files: []string{first.File, manifestName}})
	}
	if px > 0 {
		v.extentX, v.extentY = float64(s.ImageSize[0])*px, float64(s.ImageSize[1])*px
		s.ExtentUm = []float64{round3(v.extentX), round3(v.extentY)}
	}
	for _, ch := range img.Channels {
		label := ch.Name
		if ch.Purpose != "" {
			label += " (" + ch.Purpose + ")"
		}
		s.Channels = append(s.Channels, label)
		if ch.Levels[0].Width != first.Levels[0].Width || ch.Levels[0].Height != first.Levels[0].Height {
			v.add(Finding{Severity: Warning, Code: "images.size-mismatch", Title: "Morphology channels of different sizes", Files: []string{ch.File},
				Detail: fmt.Sprintf("%s is %d×%d pixels, %s %d×%d.", ch.Name, ch.Levels[0].Width, ch.Levels[0].Height, first.Name, first.Levels[0].Width, first.Levels[0].Height)})
		}
		for _, l := range ch.Levels {
			if !readableCompression(l.Compression) {
				v.add(Finding{Severity: Fatal, Code: "images.compression", Title: "Image compression SituCraft does not read", Files: []string{ch.File},
					Detail: fmt.Sprintf("%s uses TIFF compression %d.", ch.Name, l.Compression)})
				break
			}
		}
		if ch.Bits != 8 && ch.Bits != 16 {
			v.add(Finding{Severity: Fatal, Code: "images.bits", Title: "Image sample size SituCraft does not read", Files: []string{ch.File},
				Detail: fmt.Sprintf("%s has %d bits per sample.", ch.Name, ch.Bits)})
		}
	}
	stack, err := v.b.ZStack()
	if err != nil {
		v.add(Finding{Severity: Warning, Code: "images.zstack-unreadable", Title: "The morphology z-stack cannot be read", Detail: err.Error(),
			Remedy: "The in-focus images are enough for viewing and analysis; the z-stack is optional."})
	} else if stack != nil {
		s.ZPlanes = stack.SizeZ
		if l := stack.Channels[0].Levels[0]; l.Width != first.Levels[0].Width || l.Height != first.Levels[0].Height {
			v.add(Finding{Severity: Warning, Code: "images.zstack-size", Title: "The z-stack and the in-focus images differ in size", Files: stack.Files,
				Detail: fmt.Sprintf("The z-stack is %d×%d pixels, the in-focus images %d×%d.", l.Width, l.Height, first.Levels[0].Width, first.Levels[0].Height)})
		}
	}
	return nil
}

func readableCompression(code int) bool {
	switch code {
	case 1, 5, 8, 32773, 32946, 34712, 33003, 33005:
		return true
	}
	return false
}

// outside reports whether a point lies past the image by more than the tolerance, and whether it
// lies past it at all.
func (v *validator) outside(x, y float64) (far, near bool) {
	if v.extentX == 0 {
		return false, false
	}
	dx := math.Max(-x, x-v.extentX)
	dy := math.Max(-y, y-v.extentY)
	d := math.Max(dx, dy)
	return d > edgeToleranceUm, d > 0
}

func (v *validator) checkCells() error {
	if !v.b.Has("cells.parquet") {
		return nil
	}
	cells, err := v.b.ReadCells()
	if err != nil {
		v.add(Finding{Severity: Fatal, Code: "cells.unreadable", Title: "The cells table cannot be read", Detail: err.Error(), Files: []string{"cells.parquet"},
			Remedy: "Check that cells.parquet was copied whole."})
		return nil
	}
	v.cells = cells
	s := &v.report.Summary
	s.Cells = len(cells.IDs)
	if s.Cells == 0 {
		v.add(Finding{Severity: Fatal, Code: "cells.none", Title: "No cells", Files: []string{"cells.parquet"}, Detail: "The bundle's segmentation found no cells."})
		return nil
	}
	var far, near, badArea, nucleusLarger, totalMismatch, badID int64
	var farBox, areaBox bounds
	for i := range cells.IDs {
		x, y := cells.X[i], cells.Y[i]
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			far++
			continue
		}
		if f, n := v.outside(x, y); f {
			far++
			farBox.add(x, y)
		} else if n {
			near++
		}
		if cells.CellArea != nil && !(cells.CellArea[i] > 0) {
			badArea++
			areaBox.add(x, y)
		}
		if cells.CellArea != nil && cells.NucleusArea != nil && cells.NucleusArea[i] > cells.CellArea[i]*1.0001 {
			nucleusLarger++
		}
		if cells.TotalCounts != nil {
			var sum int64
			for _, c := range [][]int64{cells.TranscriptCounts, cells.ControlProbeCounts, cells.ControlCodewordCounts, cells.UnassignedCodewordCounts, cells.DeprecatedCodewordCounts} {
				if c != nil {
					sum += c[i]
				}
			}
			if sum != cells.TotalCounts[i] {
				totalMismatch++
			}
		}
		if _, _, ok := ParseCellID(cells.IDs[i]); !ok {
			badID++
		}
	}
	if far > 0 {
		v.add(Finding{Severity: Warning, Code: "cells.outside-image", Title: "Cell centroids outside the images", Count: far, Region: farBox.region(), Files: []string{"cells.parquet"},
			Detail: fmt.Sprintf("%d cell centroids lie more than %g µm outside the %g × %g µm images (or are not numbers).", far, edgeToleranceUm, round3(v.extentX), round3(v.extentY)),
			Remedy: "Check that the images and tables come from the same run and region."})
	}
	if near > 0 {
		v.add(Finding{Severity: Info, Code: "cells.at-edge", Title: "Cell centroids just past the images' edge", Count: near, Files: []string{"cells.parquet"},
			Detail: fmt.Sprintf("%d cell centroids lie up to %g µm past the images' edge.", near, edgeToleranceUm)})
	}
	if badArea > 0 {
		v.add(Finding{Severity: Warning, Code: "cells.area", Title: "Cells without a positive area", Count: badArea, Region: areaBox.region(), Files: []string{"cells.parquet"}})
	}
	if nucleusLarger > 0 {
		v.add(Finding{Severity: Info, Code: "cells.nucleus-larger", Title: "Nuclei larger than their cells", Count: nucleusLarger, Files: []string{"cells.parquet"},
			Detail: "A nucleus's area exceeds its cell's; a cell boundary may cut through its nucleus."})
	}
	if totalMismatch > 0 {
		v.add(Finding{Severity: Warning, Code: "cells.total-counts", Title: "Cell totals that are not the sum of their parts", Count: totalMismatch, Files: []string{"cells.parquet"},
			Detail: "total_counts differs from the sum of the gene, control and codeword counts."})
	}
	if badID > 0 {
		v.add(Finding{Severity: Info, Code: "cells.id-form", Title: "Cell IDs in an older form", Count: badID, Files: []string{"cells.parquet"},
			Detail: "These cell IDs are not in the form XOA 1.3 and later write (eight letters a–p, a hyphen and a number)."})
	}
	return nil
}

// Indexes of per-cell counts by kind.
const (
	countGene = iota
	countProbe
	countCodeword
	countUnassigned
	countDeprecated
	countGenomic
	countKinds
)

var kindIndex = map[string]int{KindGene: countGene, KindNegControlProbe: countProbe, KindNegControlCode: countCodeword, KindUnassignedCode: countUnassigned, KindDeprecatedCode: countDeprecated, KindGenomicControl: countGenomic}

func (v *validator) checkTranscripts() error {
	if !v.b.Has("transcripts.parquet") {
		return nil
	}
	t, err := v.b.OpenTranscripts()
	if err != nil {
		v.add(Finding{Severity: Fatal, Code: "transcripts.unreadable", Title: "The transcripts cannot be read", Detail: err.Error(), Files: []string{"transcripts.parquet"},
			Remedy: "Check that transcripts.parquet was copied whole."})
		return nil
	}
	defer t.Close()
	s := &v.report.Summary
	s.TranscriptRowGroups = len(t.File.RowGroups)
	var perCell [][countKinds]int64
	if v.cells != nil {
		perCell = make([][countKinds]int64, len(v.cells.IDs))
	}
	panel := map[string]bool{}
	if v.b.Panel != nil {
		for _, target := range v.b.Panel.Targets {
			panel[target.Name] = true
		}
	}
	observed := map[string]bool{}
	var total, q20Genes, q20GenesAssigned, q20Decoded, controls, far, near, unknownCell, notInPanel, badQV int64
	var farBox, unknownBox bounds
	notInPanelNames := map[string]bool{}
	groups := len(t.File.RowGroups)
	g := 0
	err = t.Each(func(batch *TranscriptBatch) error {
		if err := v.ctx.Err(); err != nil {
			return err
		}
		g++
		v.progress(0.4+0.2*float64(g)/float64(groups+1), fmt.Sprintf("Checking transcripts (row group %d of %d)", g, groups))
		for i := 0; i < batch.Rows; i++ {
			total++
			feature := batch.Feature[i]
			kind := FeatureKind(string(feature))
			if kind == KindGene {
				if !observed[string(feature)] {
					observed[string(feature)] = true
				}
				if len(panel) > 0 && !panel[string(feature)] {
					notInPanel++
					notInPanelNames[string(feature)] = true
				}
			} else {
				controls++
			}
			x, y := float64(batch.X[i]), float64(batch.Y[i])
			if f, n := v.outside(x, y); f || math.IsNaN(x) || math.IsNaN(y) {
				far++
				farBox.add(x, y)
			} else if n {
				near++
			}
			qv := batch.QV[i]
			if qv != qv || qv < 0 {
				badQV++
			}
			q20 := qv >= 20
			if q20 {
				q20Decoded++
			}
			cell := batch.CellID[i]
			assigned := string(cell) != unassignedCellMarker && len(cell) > 0
			if q20 && kind == KindGene {
				q20Genes++
				if assigned {
					q20GenesAssigned++
				}
			}
			if !assigned || perCell == nil {
				continue
			}
			index, ok := v.cells.Index[string(cell)]
			if !ok {
				unknownCell++
				unknownBox.add(x, y)
				continue
			}
			if q20 {
				perCell[index][kindIndex[kind]]++
			}
		}
		return nil
	})
	if err != nil {
		if v.ctx.Err() != nil {
			return v.ctx.Err()
		}
		v.add(Finding{Severity: Fatal, Code: "transcripts.unreadable", Title: "The transcripts cannot be read", Detail: err.Error(), Files: []string{"transcripts.parquet"},
			Remedy: "Check that transcripts.parquet was copied whole."})
		return nil
	}
	s.Transcripts, s.Q20GeneTranscripts, s.ControlTranscripts = total, q20Genes, controls
	if q20Genes > 0 {
		s.AssignedFraction = round6(float64(q20GenesAssigned) / float64(q20Genes))
	}
	s.ObservedGenes = len(observed)
	v.stats = transcriptStats{total: total, q20Genes: q20Genes, q20GenesAssigned: q20GenesAssigned, q20Decoded: q20Decoded}
	if total == 0 {
		v.add(Finding{Severity: Fatal, Code: "transcripts.none", Title: "No transcripts", Files: []string{"transcripts.parquet"}})
		return nil
	}
	if far > 0 {
		v.add(Finding{Severity: Warning, Code: "transcripts.outside-image", Title: "Transcripts outside the images", Count: far, Region: farBox.region(), Files: []string{"transcripts.parquet"},
			Detail: fmt.Sprintf("%d transcripts lie more than %g µm outside the %g × %g µm images (or have no position).", far, edgeToleranceUm, round3(v.extentX), round3(v.extentY)),
			Remedy: "Check that the images and tables come from the same run and region."})
	}
	if near > 0 {
		v.add(Finding{Severity: Info, Code: "transcripts.at-edge", Title: "Transcripts just past the images' edge", Count: near, Files: []string{"transcripts.parquet"},
			Detail: fmt.Sprintf("%d transcripts lie up to %g µm past the images' edge; they are kept.", near, edgeToleranceUm)})
	}
	if badQV > 0 {
		v.add(Finding{Severity: Warning, Code: "transcripts.qv", Title: "Transcripts without a valid quality score", Count: badQV, Files: []string{"transcripts.parquet"}})
	}
	if unknownCell > 0 {
		v.add(Finding{Severity: Fatal, Code: "transcripts.unknown-cell", Title: "Transcripts assigned to cells the cells table does not have", Count: unknownCell, Region: unknownBox.region(),
			Files: []string{"transcripts.parquet", "cells.parquet"}, Detail: fmt.Sprintf("%d transcripts name a cell missing from cells.parquet: the tables are not from the same segmentation.", unknownCell),
			Remedy: "Use transcripts.parquet and cells.parquet from the same output folder (after Xenium Ranger, both from its output)."})
	}
	if notInPanel > 0 {
		names := sortedKeys(notInPanelNames, 8)
		v.add(Finding{Severity: Warning, Code: "transcripts.not-in-panel", Title: "Genes not in the gene panel", Count: notInPanel, Files: []string{"transcripts.parquet", "gene_panel.json"},
			Detail: fmt.Sprintf("%d transcripts of %d genes absent from gene_panel.json (%s).", notInPanel, len(notInPanelNames), strings.Join(names, ", ")),
			Remedy: "Check that gene_panel.json is this run's."})
	}
	if v.b.Panel != nil {
		var missing []string
		for _, gene := range v.b.Panel.Genes() {
			if !observed[gene.Name] {
				missing = append(missing, gene.Name)
			}
		}
		if len(missing) > 0 {
			v.add(Finding{Severity: Info, Code: "transcripts.panel-gene-absent", Title: "Panel genes with no transcripts", Count: int64(len(missing)), Files: []string{"gene_panel.json"},
				Detail: fmt.Sprintf("%d of the panel's %d genes were not detected in this region (%s).", len(missing), len(v.b.Panel.Genes()), strings.Join(limit(missing, 8), ", "))})
		}
	}
	if perCell != nil && v.cells.TranscriptCounts != nil {
		var mismatched int64
		var box bounds
		columns := [][]int64{v.cells.TranscriptCounts, v.cells.ControlProbeCounts, v.cells.ControlCodewordCounts, v.cells.UnassignedCodewordCounts, v.cells.DeprecatedCodewordCounts}
		for i := range perCell {
			for k, col := range columns {
				if col != nil && col[i] != perCell[i][k] {
					mismatched++
					box.add(v.cells.X[i], v.cells.Y[i])
					break
				}
			}
		}
		if mismatched > 0 {
			v.add(Finding{Severity: Warning, Code: "cells.counts-differ", Title: "Cell counts that the transcripts do not reproduce", Count: mismatched, Region: box.region(),
				Files:  []string{"cells.parquet", "transcripts.parquet"},
				Detail: fmt.Sprintf("For %d cells, counting the transcripts assigned to them (quality score at least 20) gives different numbers from cells.parquet.", mismatched),
				Remedy: "Use the two tables from the same output; if they are, report it."})
		}
	}
	return nil
}

func (v *validator) checkMatrix() error {
	if !v.b.Has("cell_feature_matrix.zarr.zip") && !v.b.Has("cell_feature_matrix/matrix.mtx.gz") && !v.b.Has("cell_feature_matrix.tar.gz") {
		return nil
	}
	m, err := v.b.ReadMatrix()
	if err != nil {
		v.add(Finding{Severity: Fatal, Code: "matrix.unreadable", Title: "The cell-by-feature matrix cannot be read", Detail: err.Error(), Remedy: "Check that the matrix files were copied whole."})
		return nil
	}
	if v.cells == nil {
		return nil
	}
	files := []string{m.Source, "cells.parquet"}
	var unknown int64
	inMatrix := make([]bool, len(v.cells.IDs))
	order := true
	for i, id := range m.CellIDs {
		index, ok := v.cells.Index[id]
		if !ok {
			unknown++
			continue
		}
		inMatrix[index] = true
		if int(index) != i {
			order = false
		}
	}
	var absent int64
	for _, ok := range inMatrix {
		if !ok {
			absent++
		}
	}
	if unknown > 0 || absent > 0 {
		v.add(Finding{Severity: Fatal, Code: "matrix.cells-differ", Title: "The matrix and the cells table list different cells", Count: unknown + absent, Files: files,
			Detail: fmt.Sprintf("%d of the matrix's cells are not in cells.parquet; %d cells are not in the matrix.", unknown, absent),
			Remedy: "Use the matrix and the cells table from the same output."})
		return nil
	}
	if !order {
		v.add(Finding{Severity: Info, Code: "matrix.cell-order", Title: "The matrix lists the cells in another order", Files: files})
	}
	// Per-cell sums by kind against cells.parquet's counts; the Zarr form's aggregate feature
	// ("Total transcripts") against the gene sum.
	sums := make([][countKinds]float64, len(v.cells.IDs))
	aggregate := make([]float64, len(v.cells.IDs))
	hasAggregate := false
	for f, feature := range m.Features {
		for k := m.Indptr[f]; k < m.Indptr[f+1]; k++ {
			cell := v.cells.Index[m.CellIDs[m.Indices[k]]]
			if feature.Kind == KindAggregate {
				aggregate[cell] += m.Data[k]
				hasAggregate = true
				continue
			}
			if idx, ok := kindIndex[feature.Kind]; ok {
				sums[cell][idx] += m.Data[k]
			}
		}
	}
	columns := [][]int64{v.cells.TranscriptCounts, v.cells.ControlProbeCounts, v.cells.ControlCodewordCounts, v.cells.UnassignedCodewordCounts, v.cells.DeprecatedCodewordCounts}
	var mismatched, aggregateOff int64
	var box bounds
	for i := range sums {
		for k, col := range columns {
			if col != nil && float64(col[i]) != sums[i][k] {
				mismatched++
				box.add(v.cells.X[i], v.cells.Y[i])
				break
			}
		}
		if hasAggregate && aggregate[i] != sums[i][countGene] {
			aggregateOff++
		}
	}
	if mismatched > 0 {
		v.add(Finding{Severity: Warning, Code: "matrix.counts-differ", Title: "Matrix counts that differ from the cells table", Count: mismatched, Region: box.region(), Files: files,
			Detail: fmt.Sprintf("For %d cells the matrix's counts, summed by kind, differ from cells.parquet's.", mismatched)})
	}
	if aggregateOff > 0 {
		v.add(Finding{Severity: Warning, Code: "matrix.aggregate", Title: "The matrix's total differs from its genes' sum", Count: aggregateOff, Files: []string{m.Source}})
	}
	if v.b.Panel != nil {
		genes := map[string]bool{}
		for _, g := range v.b.Panel.Genes() {
			genes[g.Name] = true
		}
		var extra []string
		for _, f := range m.Features {
			if f.Kind == KindGene && !genes[f.Name] {
				extra = append(extra, f.Name)
			}
		}
		if len(extra) > 0 {
			v.add(Finding{Severity: Warning, Code: "matrix.not-in-panel", Title: "Matrix genes not in the gene panel", Count: int64(len(extra)), Files: []string{m.Source, "gene_panel.json"},
				Detail: strings.Join(limit(extra, 8), ", ")})
		}
	}
	return nil
}

func (v *validator) checkCellsZarr() error {
	if !v.b.Has("cells.zarr.zip") || v.cells == nil {
		return nil
	}
	f, err := v.b.OpenFile("cells.zarr.zip")
	if err != nil {
		return nil
	}
	defer f.Close()
	fail := func(err error) error {
		v.add(Finding{Severity: Warning, Code: "cells-zarr.unreadable", Title: "cells.zarr.zip cannot be read", Detail: err.Error(), Files: []string{"cells.zarr.zip"},
			Remedy: "It is Xenium Explorer's copy of the segmentation; the Parquet tables are enough."})
		return nil
	}
	store, err := zarr.OpenZip(f, f.Size())
	if err != nil {
		return fail(err)
	}
	if attrs, err := zarr.Attributes(store, ""); err == nil {
		if methods, ok := attrs["segmentation_methods"].([]any); ok {
			for _, m := range methods {
				if s, ok := m.(string); ok {
					v.report.Summary.SegmentationMethods = append(v.report.Summary.SegmentationMethods, s)
				}
			}
		}
	}
	v.segmentationCounts(store)
	a, err := zarr.OpenArray(store, "cell_id")
	if err != nil {
		return fail(err)
	}
	data, err := a.ReadAll()
	if err != nil {
		return fail(err)
	}
	if len(a.Shape) != 2 || a.Shape[1] != 2 || a.DType.Size != 4 {
		return fail(fmt.Errorf("cell_id has shape %v and type %s", a.Shape, a.DType))
	}
	differ := a.Shape[0] != len(v.cells.IDs)
	for i := 0; !differ && i < a.Shape[0]; i++ {
		differ = CellIDString(le32(data[8*i:]), le32(data[8*i+4:])) != v.cells.IDs[i]
	}
	if differ {
		v.add(Finding{Severity: Warning, Code: "cells-zarr.cells-differ", Title: "cells.zarr.zip and cells.parquet list different cells", Files: []string{"cells.zarr.zip", "cells.parquet"},
			Detail: "Xenium Explorer's copy of the segmentation does not match the cells table, as it does when Xenium Ranger replaced the segmentation without rewriting it."})
	}
	if size := v.report.Summary.ImageSize; size != nil {
		for _, mask := range []string{"masks/0", "masks/1"} {
			m, err := zarr.OpenArray(store, mask)
			if err != nil {
				continue
			}
			if len(m.Shape) != 2 || m.Shape[0] != size[1] || m.Shape[1] != size[0] {
				v.add(Finding{Severity: Warning, Code: "cells-zarr.mask-size", Title: "A segmentation mask differs in size from the images", Files: []string{"cells.zarr.zip"},
					Detail: fmt.Sprintf("%s is %v pixels; the images are %d×%d.", mask, m.Shape, size[0], size[1])})
			}
		}
	}
	return nil
}

// segmentationCounts counts the cells of each segmentation method (cells.zarr.zip's
// polygon_sets/1/method indexes its segmentation_methods) and compares them with the run's
// metrics, which count the boundary, interior and nucleus-expansion methods.
func (v *validator) segmentationCounts(store zarr.Store) {
	s := &v.report.Summary
	a, err := zarr.OpenArray(store, "polygon_sets/1/method")
	if err != nil || len(s.SegmentationMethods) == 0 || a.DType.Kind != 'u' && a.DType.Kind != 'i' {
		return
	}
	data, err := a.ReadAll()
	if err != nil {
		return
	}
	counts := make([]int64, len(s.SegmentationMethods))
	for _, m := range unsignedInts(data, a.DType.Size) {
		if m >= 0 && int(m) < len(counts) {
			counts[m]++
		}
	}
	s.SegmentationCounts = counts
	for i, method := range s.SegmentationMethods {
		lower := strings.ToLower(method)
		switch {
		case strings.Contains(lower, "boundary stain"):
			v.compareMetric("segmented_cell_boundary_count", float64(counts[i]), true)
		case strings.Contains(lower, "interior stain"):
			v.compareMetric("segmented_cell_interior_count", float64(counts[i]), true)
		case strings.Contains(lower, "nucleus expansion"):
			v.compareMetric("segmented_cell_nuc_expansion_count", float64(counts[i]), true)
		}
	}
}

func (v *validator) checkBoundaries() error {
	for _, name := range []string{"cell_boundaries.parquet", "nucleus_boundaries.parquet"} {
		if !v.b.Has(name) {
			continue
		}
		poly, err := v.b.ReadBoundaries(name)
		if err != nil {
			v.add(Finding{Severity: Fatal, Code: "boundaries.unreadable", Title: "Boundaries cannot be read", Detail: err.Error(), Files: []string{name}})
			continue
		}
		problems := map[string]int64{}
		var box bounds
		var unknown int64
		seen := map[string]int{}
		for p := range poly.CellIDs {
			x, y := poly.X[poly.Offsets[p]:poly.Offsets[p+1]], poly.Y[poly.Offsets[p]:poly.Offsets[p+1]]
			if problem := polygonProblem(x, y); problem != "" {
				problems[problem]++
				if len(x) > 0 {
					box.add(float64(x[0]), float64(y[0]))
				}
			}
			seen[poly.CellIDs[p]]++
			if v.cells != nil {
				if _, ok := v.cells.Index[poly.CellIDs[p]]; !ok {
					unknown++
				}
			}
		}
		kind := "Cell"
		if strings.HasPrefix(name, "nucleus") {
			kind = "Nucleus"
		}
		if unknown > 0 {
			v.add(Finding{Severity: Fatal, Code: "boundaries.unknown-cell", Title: kind + " boundaries of cells the cells table does not have", Count: unknown, Files: []string{name, "cells.parquet"},
				Remedy: "Use the boundaries and the cells table from the same output."})
		}
		if v.cells != nil {
			var without int64
			for _, id := range v.cells.IDs {
				if seen[id] == 0 {
					without++
				}
			}
			if without > 0 {
				severity := Warning
				if kind == "Nucleus" {
					severity = Info // segmentation by boundary stain can find cells without a nucleus
				}
				v.add(Finding{Severity: severity, Code: "boundaries.missing", Title: fmt.Sprintf("Cells without a %s boundary", strings.ToLower(kind)), Count: without, Files: []string{name}})
			}
		}
		var several int64
		for _, n := range seen {
			if n > 1 {
				several++
			}
		}
		if several > 0 {
			v.add(Finding{Severity: Info, Code: "boundaries.multipart", Title: kind + " boundaries in several parts", Count: several, Files: []string{name}})
		}
		for _, problem := range []string{"non-finite", "degenerate", "zero-area", "self-intersecting"} {
			n := problems[problem]
			if n == 0 {
				continue
			}
			severity := Warning
			detail := map[string]string{
				"non-finite":        "vertices that are not numbers",
				"degenerate":        "fewer than three distinct vertices",
				"zero-area":         "no area",
				"self-intersecting": "edges that cross (not a simple polygon)",
			}[problem]
			if problem == "self-intersecting" {
				severity = Info
			}
			v.add(Finding{Severity: severity, Code: "boundaries." + problem, Title: fmt.Sprintf("%s boundaries with %s", kind, detail), Count: n, Region: box.region(), Files: []string{name}})
		}
		v.compareMetric(fmt.Sprintf("number_of_%s_non_simple_polygons", strings.ToLower(kind)), float64(problems["self-intersecting"]), true)
	}
	return nil
}

// compareMetric records a metric the data reproduce, or a finding when they do not.
func (v *validator) compareMetric(name string, computed float64, exact bool) {
	recorded, ok := v.b.Metrics.Number(name)
	if !ok {
		return
	}
	s := &v.report.Summary
	s.MetricsCompared++
	agree := recorded == computed
	if !exact {
		agree = math.Abs(recorded-computed) <= relativeTolerance*math.Max(1, math.Abs(recorded))
	}
	if agree {
		s.MetricsAgreeing++
		return
	}
	v.add(Finding{Severity: Warning, Code: "metrics." + name, Title: "A run metric the data do not reproduce", Files: []string{"metrics_summary.csv"},
		Detail: fmt.Sprintf("%s: the bundle records %s; its data give %s.", name, formatNumber(recorded), formatNumber(computed)),
		Remedy: "The metrics may come from before the data were changed (by Xenium Ranger, say); SituCraft uses the data."})
}

type transcriptStats struct {
	total, q20Genes, q20GenesAssigned, q20Decoded int64
}

func (v *validator) checkMetrics() error {
	s := &v.report.Summary
	if v.cells != nil && v.cells.TranscriptCounts != nil {
		counts := append([]int64(nil), v.cells.TranscriptCounts...)
		sort.Slice(counts, func(i, j int) bool { return counts[i] < counts[j] })
		n := len(counts)
		if n%2 == 1 {
			s.MedianPerCell = float64(counts[n/2])
		} else if n > 0 {
			s.MedianPerCell = float64(counts[n/2-1]+counts[n/2]) / 2
		}
	}
	e := v.b.Experiment
	if v.cells != nil && e.NumCells != nil {
		v.compareManifest("num_cells", float64(*e.NumCells), float64(len(v.cells.IDs)))
	}
	if v.cells != nil && e.TranscriptsPerCell != nil && v.cells.TranscriptCounts != nil {
		v.compareManifest("transcripts_per_cell", float64(*e.TranscriptsPerCell), math.Floor(s.MedianPerCell))
	}
	if v.b.Metrics == nil || v.cells == nil {
		return nil
	}
	st := v.stats
	v.compareMetric("num_cells_detected", float64(len(v.cells.IDs)), true)
	if st.total > 0 {
		v.compareMetric("total_high_quality_decoded_transcripts", float64(st.q20Genes), true)
		geneTotal := float64(0)
		for _, c := range v.cells.TranscriptCounts {
			geneTotal += float64(c)
		}
		if st.q20Genes > 0 {
			v.compareMetric("fraction_transcripts_assigned", float64(st.q20GenesAssigned)/float64(st.q20Genes), false)
		}
		var area float64
		for _, a := range v.cells.CellArea {
			area += a
		}
		if area > 0 {
			v.compareMetric("total_cell_area", area, false)
			v.compareMetric("decoded_transcripts_per_100um2", float64(st.q20Genes)/area*100, false)
		}
		if v.cells.TranscriptCounts != nil {
			v.compareMetric("median_transcripts_per_cell", s.MedianPerCell, false)
		}
	}
	if region, ok := v.b.Metrics.Number("region_area"); ok && region > 0 {
		v.compareMetric("cells_per_100um2", float64(len(v.cells.IDs))/region*100, false)
	}
	return nil
}

// compareManifest checks a value experiment.xenium records.
func (v *validator) compareManifest(name string, recorded, computed float64) {
	s := &v.report.Summary
	s.MetricsCompared++
	if recorded == computed {
		s.MetricsAgreeing++
		return
	}
	v.add(Finding{Severity: Warning, Code: "manifest." + name, Title: "A value of the run manifest the data do not reproduce", Files: []string{manifestName},
		Detail: fmt.Sprintf("%s: experiment.xenium records %s; the data give %s.", name, formatNumber(recorded), formatNumber(computed))})
}

func (v *validator) checkDisk() error {
	s := &v.report.Summary
	if size := s.ImageSize; size != nil {
		channels := int64(len(s.Channels))
		if channels == 0 {
			channels = 1
		}
		// The tile cache holds each channel's pyramid (a third more than the full level) as
		// 16-bit samples, deflated to about half.
		s.EstimatedCacheBytes = int64(size[0]) * int64(size[1]) * 2 * channels * 4 / 3 / 2
	}
	if v.opts.CacheDir == "" {
		return nil
	}
	free, err := freeBytes(v.opts.CacheDir)
	if err != nil {
		return nil
	}
	s.FreeCacheBytes = free
	if s.EstimatedCacheBytes > 0 && free < 2*s.EstimatedCacheBytes {
		v.add(Finding{Severity: Warning, Code: "disk.low", Title: "Little free space for SituCraft's cache",
			Detail: fmt.Sprintf("%s free where SituCraft keeps derived data (%s); this bundle's images need about %s there.", humanBytes(free), v.opts.CacheDir, humanBytes(s.EstimatedCacheBytes)),
			Remedy: "Free some space, or start SituCraft with its cache elsewhere."})
	}
	return nil
}

func sortedKeys(m map[string]bool, n int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return limit(out, n)
}

func limit(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return append(append([]string(nil), list[:n]...), fmt.Sprintf("and %d more", len(list)-n))
}

func round3(x float64) float64 { return math.Round(x*1000) / 1000 }
func round6(x float64) float64 { return math.Round(x*1e6) / 1e6 }

func formatNumber(x float64) string {
	if x == math.Trunc(x) && math.Abs(x) < 1e15 {
		return fmt.Sprintf("%.0f", x)
	}
	return fmt.Sprintf("%.6g", x)
}

func humanBytes(n int64) string {
	units := []string{"bytes", "KB", "MB", "GB", "TB"}
	v := float64(n)
	u := 0
	for v >= 1000 && u < len(units)-1 {
		v /= 1000
		u++
	}
	if u == 0 {
		return fmt.Sprintf("%d bytes", n)
	}
	return fmt.Sprintf("%.1f %s", v, units[u])
}
