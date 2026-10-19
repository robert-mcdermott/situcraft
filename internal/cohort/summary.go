// Package cohort is SituCraft's comparison of samples (wave 7): each sample summarized the same way
// (summary.go), and groups of samples compared with the sample as the unit.
//
// A sample's summary is of the cells chosen (its section's, or its region's, never those a review
// excluded), each labeled from the cohort's label set (one vocabulary of names for every sample):
// its cells by label, their shares of the labeled cells and their densities per mm² of its tissue;
// its quality (the cells' median transcripts, genes, areas and nuclei, transcripts per area); and
// its pseudobulk, each label's cells' gene transcripts summed gene by gene (Crowell et al. 2020).
package cohort

import (
	"errors"
	"fmt"
	"sort"

	"situcraft/internal/segment"
)

// Input is one sample's cells as its summary reads them: the segmentation's cells and counts
// (quality-20 gene transcripts, cell by gene), the cells chosen, each cell's label (0 to K − 1 in
// the cohort's vocabulary, or −1), and the area of its tissue (µm²).
type Input struct {
	Cells  []segment.Cell
	Counts *segment.Counts
	Keep   []bool
	Label  []int32
	Tissue float64
}

// QC is a sample's quality, over its cells chosen.
type QC struct {
	Cells             int     `json:"cells"`
	CellsPerMm2       float64 `json:"cellsPerMm2"`
	MedianTranscripts float64 `json:"medianTranscripts"`
	MedianGenes       float64 `json:"medianGenes"`
	MedianArea        float64 `json:"medianAreaUm2"`
	MedianNucleusArea float64 `json:"medianNucleusAreaUm2"`
	NucleusShare      float64 `json:"nucleusShare"`
	TranscriptsPer100 float64 `json:"transcriptsPer100um2"`
	Transcripts       int64   `json:"transcripts"`
	NoTranscriptShare float64 `json:"noTranscriptShare"`
}

// Summary is a sample's numbers: its cells chosen and labeled, its tissue's area, each label's
// cells, share of the labeled cells and density per mm² of tissue, its quality, and its pseudobulk
// (by label, each gene's transcripts in the counts' gene order).
type Summary struct {
	Cells      int        `json:"cells"`
	Labeled    int        `json:"labeled"`
	Tissue     float64    `json:"tissueAreaUm2"`
	Counts     []int      `json:"counts"`
	Shares     []float64  `json:"shares"`
	Density    []float64  `json:"densityPerMm2"`
	QC         QC         `json:"qc"`
	Pseudobulk [][]uint64 `json:"-"`
}

// Summarize summarizes a sample over K labels.
func Summarize(in Input, K int) (*Summary, error) {
	n := len(in.Cells)
	if len(in.Keep) != n || len(in.Label) != n {
		return nil, errors.New("cohort: the cells, their choice and their labels differ in length")
	}
	if in.Counts == nil || len(in.Counts.Offsets) != n+1 {
		return nil, errors.New("cohort: the counts are not of the segmentation's cells")
	}
	if !(in.Tissue > 0) {
		return nil, errors.New("cohort: the sample has no tissue")
	}
	s := &Summary{Tissue: in.Tissue, Counts: make([]int, K), Shares: make([]float64, K), Density: make([]float64, K), Pseudobulk: make([][]uint64, K)}
	G := len(in.Counts.Genes)
	for k := range s.Pseudobulk {
		s.Pseudobulk[k] = make([]uint64, G)
	}
	var transcripts, genes, areas, nuclei []float64
	var sumT int64
	var sumA float64
	nucleated, empty := 0, 0
	for i, c := range in.Cells {
		if !in.Keep[i] {
			continue
		}
		l := in.Label[i]
		if l < -1 || l >= int32(K) {
			return nil, fmt.Errorf("cohort: cell %d's label %d is not one of the %d", i, l, K)
		}
		s.Cells++
		transcripts = append(transcripts, float64(c.Transcripts))
		genes = append(genes, float64(c.Genes))
		areas = append(areas, float64(c.Area))
		sumT += int64(c.Transcripts)
		sumA += float64(c.Area)
		if c.Transcripts == 0 {
			empty++
		}
		if c.Nucleus != "" && c.NucleusArea > 0 {
			nucleated++
			nuclei = append(nuclei, float64(c.NucleusArea))
		}
		if l < 0 {
			continue
		}
		s.Labeled++
		s.Counts[l]++
		row := s.Pseudobulk[l]
		for k := in.Counts.Offsets[i]; k < in.Counts.Offsets[i+1]; k++ {
			row[in.Counts.Gene[k]] += uint64(in.Counts.Count[k])
		}
	}
	mm2 := in.Tissue / 1e6
	for k := range s.Counts {
		if s.Labeled > 0 {
			s.Shares[k] = float64(s.Counts[k]) / float64(s.Labeled)
		}
		s.Density[k] = float64(s.Counts[k]) / mm2
	}
	q := &s.QC
	q.Cells = s.Cells
	q.CellsPerMm2 = float64(s.Cells) / mm2
	q.MedianTranscripts, q.MedianGenes, q.MedianArea, q.MedianNucleusArea = median(transcripts), median(genes), median(areas), median(nuclei)
	q.Transcripts = sumT
	if s.Cells > 0 {
		q.NucleusShare = float64(nucleated) / float64(s.Cells)
		q.NoTranscriptShare = float64(empty) / float64(s.Cells)
	}
	if sumA > 0 {
		q.TranscriptsPer100 = float64(sumT) / sumA * 100
	}
	return s, nil
}

// median is the middle value (the mean of the two middle ones), 0 for none.
func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
