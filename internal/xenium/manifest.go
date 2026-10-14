package xenium

// The bundle's own descriptions: experiment.xenium (the run manifest), metrics_summary.csv (the
// run's metrics) and gene_panel.json (the panel's targets), as 10x Genomics' Xenium Onboard
// Analysis documentation describes them and the example bundles hold them.

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Experiment is experiment.xenium.
type Experiment struct {
	MajorVersion        int     `json:"major_version"`
	MinorVersion        int     `json:"minor_version"`
	PatchVersion        int     `json:"patch_version"`
	RunName             string  `json:"run_name"`
	RunStartTime        string  `json:"run_start_time"`
	RegionName          string  `json:"region_name"`
	PreservationMethod  string  `json:"preservation_method"`
	NumCells            *int64  `json:"num_cells"`
	TranscriptsPerCell  *int64  `json:"transcripts_per_cell"`
	TranscriptsPer100um float64 `json:"transcripts_per_100um"`
	CassetteName        string  `json:"cassette_name"`
	SlideID             string  `json:"slide_id"`
	PanelDesignID       string  `json:"panel_design_id"`
	PanelName           string  `json:"panel_name"`
	PanelOrganism       string  `json:"panel_organism"`
	PanelTissueType     string  `json:"panel_tissue_type"`
	PanelTargetsPre     int     `json:"panel_num_targets_predesigned"`
	PanelTargetsCustom  int     `json:"panel_num_targets_custom"`
	PixelSize           float64 `json:"pixel_size"`
	InstrumentSN        string  `json:"instrument_sn"`
	InstrumentSoftware  string  `json:"instrument_sw_version"`
	AnalysisSoftware    string  `json:"analysis_sw_version"`
	AnalysisUUID        string  `json:"analysis_uuid"`
	ExperimentUUID      string  `json:"experiment_uuid"`
	ZStepSize           float64 `json:"z_step_size"`
	SegmentationStain   string  `json:"segmentation_stain"`
	Images              struct {
		Morphology      string `json:"morphology_filepath"`
		MorphologyFocus string `json:"morphology_focus_filepath"`
		MorphologyMIP   string `json:"morphology_mip_filepath"`
	} `json:"images"`
	ExplorerFiles map[string]string `json:"xenium_explorer_files"`
}

// Version is a Xenium Onboard Analysis (or Xenium Ranger) version, from analysis_sw_version.
type Version struct {
	Software            string // "xenium", "xenium-ranger" or as written
	Major, Minor, Patch int
	Raw                 string
}

func (v Version) String() string {
	if v.Raw == "" {
		return "unknown"
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

var versionPattern = regexp.MustCompile(`^(.*?)[-_ ]?v?(\d+)\.(\d+)(?:\.(\d+))?`)

// ParseVersion reads analysis_sw_version ("xenium-2.0.0.10", "xenium-ranger-3.1.0.4", …).
func ParseVersion(s string) (Version, bool) {
	m := versionPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Version{Raw: s}, false
	}
	v := Version{Software: strings.Trim(m[1], "-_ "), Raw: s}
	v.Major, _ = strconv.Atoi(m[2])
	v.Minor, _ = strconv.Atoi(m[3])
	v.Patch, _ = strconv.Atoi(m[4])
	return v, true
}

func parseExperiment(raw []byte) (*Experiment, error) {
	var e Experiment
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", manifestName, err)
	}
	return &e, nil
}

// Metrics is metrics_summary.csv: one row of named values, numbers where they parse.
type Metrics struct {
	Values  map[string]float64
	Strings map[string]string
	Order   []string
}

// Number is a metric's value, if it is a number.
func (m *Metrics) Number(name string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m.Values[name]
	return v, ok
}

func parseMetrics(raw []byte) (*Metrics, error) {
	r := csv.NewReader(strings.NewReader(string(raw)))
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("metrics_summary.csv: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("metrics_summary.csv has no row of values")
	}
	m := &Metrics{Values: map[string]float64{}, Strings: map[string]string{}}
	for i, name := range rows[0] {
		if i >= len(rows[1]) {
			break
		}
		value := strings.TrimSpace(rows[1][i])
		m.Order = append(m.Order, name)
		m.Strings[name] = value
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			m.Values[name] = f
		}
	}
	return m, nil
}

// Target is one target of the gene panel.
type Target struct {
	Name      string // the feature name transcripts carry (gene symbol, NegControlProbe_…)
	ID        string // Ensembl ID for genes
	Kind      string // "gene", "negative_control", "deprecated_codeword", …
	Codewords []int
}

// Panel is gene_panel.json.
type Panel struct {
	Name, DesignID, Version string
	Species, Tissue         string
	Targets                 []Target
}

// Genes are the panel's gene targets.
func (p *Panel) Genes() []Target {
	var out []Target
	for _, t := range p.Targets {
		if t.Kind == "gene" {
			out = append(out, t)
		}
	}
	return out
}

func parsePanel(raw []byte) (*Panel, error) {
	var doc struct {
		Payload struct {
			Panel struct {
				Identity struct {
					DesignID string `json:"design_id"`
					Name     string `json:"name"`
					Version  string `json:"version"`
				} `json:"identity"`
				Species string `json:"species"`
				Tissue  string `json:"tissue"`
			} `json:"panel"`
			Targets []struct {
				Codewords []int `json:"codewords"`
				Type      struct {
					Descriptor string `json:"descriptor"`
					Data       struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"data"`
				} `json:"type"`
			} `json:"targets"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("gene_panel.json is not valid JSON: %w", err)
	}
	p := doc.Payload
	panel := &Panel{Name: p.Panel.Identity.Name, DesignID: p.Panel.Identity.DesignID, Version: p.Panel.Identity.Version, Species: p.Panel.Species, Tissue: p.Panel.Tissue}
	for _, t := range p.Targets {
		panel.Targets = append(panel.Targets, Target{Name: t.Type.Data.Name, ID: t.Type.Data.ID, Kind: t.Type.Descriptor, Codewords: t.Codewords})
	}
	return panel, nil
}
