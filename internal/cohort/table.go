package cohort

// The cohort table: every sample's numbers, one a row, each with the artifact it came from
// (table.csv), and the pseudobulk, a row a sample's label and a column a gene (pseudobulk.csv).

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"strconv"
)

// The files of the table.
const (
	FileTable          = "table.csv"
	FilePseudobulkCSV  = "pseudobulk.csv"
	tableHeader        = "sample,name,family,key,value,source"
	pseudobulkLeadings = 4 // sample, name, label, cells
)

// Row is one number of the table: a sample's (its id and name), its family (cells, share,
// density, niche, qc, graph, enrichment, nearest, moran, pca), what it is of, its value, and the
// artifact it came from (empty for a niche's share and a principal component's score: the
// summary's own).
type Row struct {
	Sample, Name, Family, Key string
	Value                     float64
	Source                    string
}

// QCKeys name a sample's quality numbers in the table, in order.
var QCKeys = []string{"cells", "cells per mm²", "tissue area µm²", "median transcripts", "median genes", "median area µm²", "median nucleus area µm²", "nucleus share", "transcripts per 100 µm²", "transcripts", "no-transcript share"}

// qcValues are a sample's quality numbers in QCKeys's order, NaN for one its segmentation does not
// record (left out of the table).
func qcValues(sd *SampleDoc) []float64 {
	q := sd.QC
	or := func(p *float64) float64 {
		if p == nil {
			return math.NaN()
		}
		return *p
	}
	return []float64{float64(q.Cells), q.CellsPerMm2, sd.Tissue, q.MedianTranscripts, q.MedianGenes, q.MedianArea, or(q.MedianNucleusArea), or(q.NucleusShare), q.TranscriptsPer100, float64(q.Transcripts), q.NoTranscriptShare}
}

// Table is the document's numbers, sample by sample: its cells by label, their shares and
// densities (from its labels), its niches' shares, its quality (from its segmentation), its
// graph's and analyses' numbers, and its scores on the pseudobulk's principal components.
func Table(d *Document) []Row {
	var out []Row
	for i := range d.Samples {
		sd := &d.Samples[i]
		add := func(family, key string, v float64, source string) {
			out = append(out, Row{sd.Sample, sd.Name, family, key, v, source})
		}
		for k, name := range d.Labels.Names {
			add("cells", name, float64(sd.Counts[k]), sd.Labels.ID)
		}
		for k, name := range d.Labels.Names {
			add("share", name, sd.Shares[k], sd.Labels.ID)
		}
		for k, name := range d.Labels.Names {
			add("density", name, sd.Density[k], sd.Labels.ID)
		}
		for v, share := range sd.NicheShares {
			add("niche", NicheName(v), share, "")
		}
		for k, v := range qcValues(sd) {
			if !math.IsNaN(v) {
				add("qc", QCKeys[k], v, sd.Segmentation.ID)
			}
		}
		for _, st := range sd.Statistics {
			add(st.Family, st.Key, st.Value, st.Source)
		}
		if d.PCA != nil && i < len(d.PCA.Scores) {
			for k, v := range d.PCA.Scores[i] {
				add("pca", fmt.Sprintf("PC%d", k+1), v, "")
			}
		}
	}
	return out
}

// NicheName names a niche in the table: "niche 1", "niche 2", ….
func NicheName(v int) string { return "niche " + strconv.Itoa(v+1) }

// number writes a value as the shortest text that reads back as it.
func number(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// TableCSV is table.csv.
func TableCSV(d *Document) []byte {
	var buf bytes.Buffer
	buf.WriteString(tableHeader + "\n")
	w := csv.NewWriter(&buf)
	for _, r := range Table(d) {
		w.Write([]string{r.Sample, r.Name, r.Family, r.Key, number(r.Value), r.Source})
	}
	w.Flush()
	return buf.Bytes()
}

// PseudobulkCSV is pseudobulk.csv: for each sample's each label, its cells and each gene's
// transcripts (empty for a gene its panel has not).
func PseudobulkCSV(d *Document, pseudobulk [][][]uint32) ([]byte, error) {
	if len(pseudobulk) != len(d.Samples) {
		return nil, fmt.Errorf("cohort: %d samples' pseudobulk for %d samples", len(pseudobulk), len(d.Samples))
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write(append([]string{"sample", "name", "label", "cells"}, d.Genes...))
	row := make([]string, pseudobulkLeadings+len(d.Genes))
	for s := range d.Samples {
		sd := &d.Samples[s]
		for k, name := range d.Labels.Names {
			row[0], row[1], row[2], row[3] = sd.Sample, sd.Name, name, strconv.Itoa(sd.Counts[k])
			for g, v := range pseudobulk[s][k] {
				if v == Missing {
					row[pseudobulkLeadings+g] = ""
				} else {
					row[pseudobulkLeadings+g] = strconv.FormatUint(uint64(v), 10)
				}
			}
			w.Write(row)
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}
