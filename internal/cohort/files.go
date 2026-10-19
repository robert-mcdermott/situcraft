package cohort

// A cohort summary's files (an artifact of kind cohort-summary; docs/FORMATS.md):
//
//	summary.json     the cohort's label set and genes, every sample's numbers (Document), the
//	                 niches found across the samples
//	pseudobulk.bin   "SCCP", the samples' pseudobulk: the samples', labels' and genes' counts (u32
//	                 each), then for each sample, each label, each gene its transcripts (u32;
//	                 0xFFFFFFFF for a gene its panel has not)
//	niches.bin       "SCCN", with niches: the cells' count (u32), each cell's row of its
//	                 segmentation (u32) and niche (i32, −1 without a window), the samples' cells
//	                 in order
//
// The binary files have the envelope of SituCraft's binary files (internal/segment WriteBinary:
// magic, version, length, zlib); their arrays are little-endian.
//	table.csv        the cohort table: every sample's numbers, one a row, with the artifact each
//	                 came from (table.go)
//	pseudobulk.csv   the pseudobulk as a table: a row a sample's label, a column a gene

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
	"situcraft/internal/spatial"
)

// The files, and what each is.
const (
	FileSummary    = "summary.json"
	FilePseudobulk = "pseudobulk.bin"
	FileNiches     = "niches.bin"
)

// MethodVersion names how summaries are made; a change makes them again.
const MethodVersion = "2"

// Missing marks a gene a sample's panel has not, in the pseudobulk.
const Missing = math.MaxUint32

// The binary files' magics.
const (
	magicPseudobulk = "SCCP"
	magicNiches     = "SCCN"
)

// Roles are the files' roles in the manifest.
var Roles = map[string]string{FileSummary: "summary", FilePseudobulk: "pseudobulk", FileNiches: "niches", FileTable: "table", FilePseudobulkCSV: "pseudobulk-table"}

// LabelSet is the cohort's label set: its names (one vocabulary for every sample), and what they
// are (the marker set or source's name, the level).
type LabelSet struct {
	Name   string   `json:"name"`
	Level  int      `json:"level"`
	Names  []string `json:"names"`
	Colors []string `json:"colors,omitempty"`
}

// SampleLabels are the label set a sample's cells were labeled from.
type SampleLabels struct {
	ID       string `json:"id"`
	Kind     string `json:"kind,omitempty"`
	Name     string `json:"name"`
	Level    int    `json:"level"`
	Checksum string `json:"checksum"`
}

// SampleDoc is a sample in the summary: who it is, what it was made from, its numbers, and its
// niches' cells and shares (with niches).
type SampleDoc struct {
	Sample       string             `json:"sample"`
	Name         string             `json:"name"`
	Dataset      string             `json:"dataset"`
	Segmentation spatial.Side       `json:"segmentation"`
	Region       *spatial.Region    `json:"region,omitempty"`
	Labels       SampleLabels       `json:"labels"`
	Choice       spatial.Choice     `json:"choice"`
	Window       *spatial.WindowRef `json:"window"`
	PanelGenes   int                `json:"panelGenes"`
	Bundle       BundleFacts        `json:"bundle"`
	Summary
	Niches      []int       `json:"niches,omitempty"`
	NicheShares []float64   `json:"nicheShares,omitempty"`
	NicheCells  int         `json:"nicheCells,omitempty"`
	Statistics  []Statistic `json:"statistics"`
}

// NichesDoc are the niches found across the samples: their parameters, each niche's composition
// (n × K, its windows' mean) and cells, the labels' shares of all the samples' labeled cells, the
// windows, k-means's inertia and the further seeds' agreement.
type NichesDoc struct {
	Parameters  spatial.NicheParams `json:"parameters"`
	Niches      int                 `json:"niches"`
	Composition []float64           `json:"composition"`
	Cells       []int               `json:"cells"`
	Tissue      []float64           `json:"tissue"`
	Windows     int                 `json:"windows"`
	Inertia     float64             `json:"inertia"`
	Agreement   []float64           `json:"agreement"`
	Defining    [][]int             `json:"defining"`
}

// Document is summary.json.
type Document struct {
	Format        string      `json:"format"`
	Version       int         `json:"version"`
	Method        string      `json:"method"`
	MethodVersion string      `json:"methodVersion"`
	Labels        LabelSet    `json:"labels"`
	Genes         []string    `json:"genes"`
	Samples       []SampleDoc `json:"samples"`
	Niches        *NichesDoc  `json:"niches,omitempty"`
	// The samples' quality compared: the numbers, the rule, the samples far out, the batches.
	QCMetrics   []QCMetric  `json:"qcMetrics"`
	OutlierRule OutlierRule `json:"outlierRule"`
	Outliers    []Outlier   `json:"outliers"`
	Batches     []Batch     `json:"batches"`
	// The samples' panels compared, and their pseudobulk's principal components over the genes
	// every panel has.
	Panels *PanelsDoc `json:"panels"`
	PCA    *PCA       `json:"pca,omitempty"`
}

// Write writes a summary's files into dir: the document, the pseudobulk (by sample, the label by
// gene sums over the document's genes, Missing where a sample's panel has not the gene), the
// table and the pseudobulk's, and, with niches, each sample's cells' rows and niches in order.
func Write(dir string, d *Document, pseudobulk [][][]uint32, rows []uint32, niches []int32) error {
	raw, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileSummary), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	S, K, G := len(d.Samples), len(d.Labels.Names), len(d.Genes)
	if len(pseudobulk) != S {
		return errors.New("cohort: a pseudobulk for every sample")
	}
	var b segment.Buffer
	b.U32(uint32(S))
	b.U32(uint32(K))
	b.U32(uint32(G))
	for s := range pseudobulk {
		if len(pseudobulk[s]) != K {
			return fmt.Errorf("cohort: sample %d's pseudobulk has %d labels, not %d", s+1, len(pseudobulk[s]), K)
		}
		for k := range pseudobulk[s] {
			if len(pseudobulk[s][k]) != G {
				return fmt.Errorf("cohort: sample %d's pseudobulk has %d genes, not %d", s+1, len(pseudobulk[s][k]), G)
			}
			b.U32s(pseudobulk[s][k])
		}
	}
	if err := segment.WriteBinary(filepath.Join(dir, FilePseudobulk), magicPseudobulk, &b); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileTable), TableCSV(d), 0o644); err != nil {
		return err
	}
	pcsv, err := PseudobulkCSV(d, pseudobulk)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FilePseudobulkCSV), pcsv, 0o644); err != nil {
		return err
	}
	if d.Niches == nil {
		return nil
	}
	if len(rows) != len(niches) {
		return errors.New("cohort: a niche for every cell")
	}
	var nb segment.Buffer
	nb.U32(uint32(len(rows)))
	nb.U32s(rows)
	as := make([]uint32, len(niches))
	for i, v := range niches {
		as[i] = uint32(v)
	}
	nb.U32s(as)
	return segment.WriteBinary(filepath.Join(dir, FileNiches), magicNiches, &nb)
}

// ReadNiches reads niches.bin: each cell's row of its segmentation and niche, the samples' cells in
// order.
func ReadNiches(dir string) ([]uint32, []int32, error) {
	r, err := segment.ReadBinary(filepath.Join(dir, FileNiches), magicNiches)
	if err != nil {
		return nil, nil, err
	}
	n := r.Count(8)
	rows := r.U32s(n)
	raw := r.U32s(n)
	if err := r.Done(FileNiches); err != nil {
		return nil, nil, err
	}
	niches := make([]int32, n)
	for i, v := range raw {
		niches[i] = int32(v)
	}
	return rows, niches, nil
}

// Read reads a summary's document and pseudobulk.
func Read(dir string) (*Document, [][][]uint32, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileSummary))
	if err != nil {
		return nil, nil, err
	}
	d := &Document{}
	if err := json.Unmarshal(raw, d); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", FileSummary, err)
	}
	r, err := segment.ReadBinary(filepath.Join(dir, FilePseudobulk), magicPseudobulk)
	if err != nil {
		return nil, nil, err
	}
	S, K, G := int(r.U32()), int(r.U32()), int(r.U32())
	if S != len(d.Samples) || K != len(d.Labels.Names) || G != len(d.Genes) {
		return nil, nil, fmt.Errorf("%s: %d samples, %d labels and %d genes, not the summary's %d, %d and %d", FilePseudobulk, S, K, G, len(d.Samples), len(d.Labels.Names), len(d.Genes))
	}
	pb := make([][][]uint32, S)
	for s := range pb {
		pb[s] = make([][]uint32, K)
		for k := range pb[s] {
			pb[s][k] = r.U32s(G)
		}
	}
	if err := r.Done(FilePseudobulk); err != nil {
		return nil, nil, err
	}
	return d, pb, nil
}

// Check checks a summary's files: each sample's counts its labeled cells, its shares summing to one,
// its densities its counts over its tissue, its pseudobulk's labels' totals within its cells'
// transcripts, the niches' cells the samples', and the tables the document's and pseudobulk's.
func Check(dir string, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: "cohort.summary", Message: fmt.Sprintf(format, args...)})
	}
	d, pb, err := Read(dir)
	if err != nil {
		fatal("%v", err)
		return out
	}
	for s, sd := range d.Samples {
		sum, share := 0, 0.0
		for k, c := range sd.Counts {
			sum += c
			share += sd.Shares[k]
			if want := float64(c) / (sd.Tissue / 1e6); math.Abs(sd.Density[k]-want) > 1e-9*math.Max(1, want) {
				fatal("%s: %s's density %g is not its %d cells over %g µm²", sd.Name, d.Labels.Names[k], sd.Density[k], c, sd.Tissue)
			}
		}
		if sum != sd.Labeled || sd.Labeled > sd.Cells {
			fatal("%s: its labels' cells sum to %d, not its %d labeled cells (of %d)", sd.Name, sum, sd.Labeled, sd.Cells)
		}
		if sd.Labeled > 0 && math.Abs(share-1) > 1e-9 {
			fatal("%s: its shares sum to %g", sd.Name, share)
		}
		var total uint64
		for k := range pb[s] {
			for _, v := range pb[s][k] {
				if v != Missing {
					total += uint64(v)
				}
			}
		}
		if total > uint64(sd.QC.Transcripts) {
			fatal("%s: its pseudobulk holds %d transcripts, more than its cells' %d", sd.Name, total, sd.QC.Transcripts)
		}
	}
	for name, want := range map[string]func() ([]byte, error){
		FileTable:         func() ([]byte, error) { return TableCSV(d), nil },
		FilePseudobulkCSV: func() ([]byte, error) { return PseudobulkCSV(d, pb) },
	} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			fatal("%v", err)
			continue
		}
		if w, err := want(); err != nil || !bytes.Equal(raw, w) {
			fatal("%s: not the summary's numbers", name)
		}
	}
	if d.Niches != nil {
		rows, niches, err := ReadNiches(dir)
		if err != nil {
			fatal("%v", err)
			return out
		}
		n := 0
		for _, sd := range d.Samples {
			n += sd.Cells
		}
		if len(rows) != n {
			fatal("%s: %d cells, not the samples' %d", FileNiches, len(rows), n)
		}
		// Each sample's cells in each niche, its own.
		at := 0
		for _, sd := range d.Samples {
			counts := make([]int, d.Niches.Niches)
			for _, v := range niches[at:min(at+sd.Cells, len(niches))] {
				if v >= int32(len(counts)) {
					fatal("%s: a niche %d of %d", FileNiches, v+1, len(counts))
					return out
				}
				if v >= 0 {
					counts[v]++
				}
			}
			at += sd.Cells
			if fmt.Sprint(counts) != fmt.Sprint(sd.Niches) {
				fatal("%s: its niches' cells are %v, not niches.bin's %v", sd.Name, sd.Niches, counts)
			}
		}
	}
	// What the project records of it: each sample's cells, the labels, the genes and the niches.
	type sampleOf struct {
		Sample  string `json:"sample"`
		Name    string `json:"name"`
		Cells   int    `json:"cells"`
		Labeled int    `json:"labeled"`
	}
	sum := struct {
		Samples []sampleOf `json:"samples"`
		Labels  LabelSet   `json:"labels"`
		Genes   int        `json:"genes"`
		Niches  int        `json:"niches"`
	}{Labels: d.Labels, Genes: len(d.Genes)}
	for _, sd := range d.Samples {
		sum.Samples = append(sum.Samples, sampleOf{sd.Sample, sd.Name, sd.Cells, sd.Labeled})
	}
	if d.Niches != nil {
		sum.Niches = d.Niches.Niches
	}
	m.Summary, _ = json.Marshal(sum)
	return out
}
