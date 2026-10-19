package cohort

// The samples' quality side by side (wave 7, slice 5): each quality number flagged where a sample
// is far from the cohort's others, saying why, never passed or failed; and the numbers by batch
// (each slide's, run's and panel's samples). A sample far out is a sample to look at: whether it
// is left out of a comparison is the user's choice, with a reason the design records.
//
// A sample is far out on a number when its robust z-score, its distance from the samples' median
// over 1.4826 times their median absolute deviation (as Iglewicz and Hoaglin's modified z-score
// is), is at least 3.5 (their threshold), and the difference matters: at least 20% of the median
// for a quantity (compared on a log scale, as counts and areas vary by factors), at least 5
// points for a share. Where half the samples are alike (a median absolute deviation of 0), the
// mean absolute deviation times 1.2533 is the spread instead. Fewer than five samples are not
// enough to call one far out.

import (
	"fmt"
	"math"
	"sort"

	"situcraft/internal/dmath"
)

// QCMetric is a quality number a sample may be far out on: its key in QC, its name, and whether it
// is a share (compared as it is) or a quantity (compared on a log scale).
type QCMetric struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Share bool   `json:"share,omitempty"`
}

// QCMetrics are the numbers compared across samples: those of a sample's quality, not its size
// (its cells and transcripts in all, and its tissue's area, follow the section's size).
var QCMetrics = []QCMetric{
	{Key: "cellsPerMm2", Name: "cells per mm²"},
	{Key: "medianTranscripts", Name: "median transcripts"},
	{Key: "medianGenes", Name: "median genes"},
	{Key: "transcriptsPer100um2", Name: "transcripts per 100 µm²"},
	{Key: "medianAreaUm2", Name: "median cell area"},
	{Key: "medianNucleusAreaUm2", Name: "median nucleus area"},
	{Key: "nucleusShare", Name: "share of cells with a nucleus", Share: true},
	{Key: "noTranscriptShare", Name: "share of cells without a transcript", Share: true},
}

// OutlierRule is when a sample is called far out on a number.
type OutlierRule struct {
	Z          float64 `json:"z"`
	Fold       float64 `json:"fold"`
	Points     float64 `json:"points"`
	MinSamples int     `json:"minSamples"`
}

// DefaultOutlierRule: a robust z-score of 3.5, 20% from the median (a quantity) or 5 points (a
// share), five samples at least.
var DefaultOutlierRule = OutlierRule{Z: 3.5, Fold: 1.2, Points: 0.05, MinSamples: 5}

// Outlier is a sample far out on a number: its value, the samples' median and spread (on the scale
// compared), its robust z-score, and why, in words.
type Outlier struct {
	Sample string  `json:"sample"`
	Name   string  `json:"name"`
	Metric string  `json:"metric"`
	Value  float64 `json:"value"`
	Median float64 `json:"median"`
	Spread float64 `json:"spread"`
	Z      float64 `json:"z"`
	Reason string  `json:"reason"`
}

// MetricValue is a sample's value of a quality number (false where its segmentation records none).
func MetricValue(q QC, key string) (float64, bool) {
	switch key {
	case "cellsPerMm2":
		return q.CellsPerMm2, true
	case "medianTranscripts":
		return q.MedianTranscripts, true
	case "medianGenes":
		return q.MedianGenes, true
	case "transcriptsPer100um2":
		return q.TranscriptsPer100, true
	case "medianAreaUm2":
		return q.MedianArea, true
	case "medianNucleusAreaUm2":
		if q.MedianNucleusArea == nil {
			return 0, false
		}
		return *q.MedianNucleusArea, true
	case "nucleusShare":
		if q.NucleusShare == nil {
			return 0, false
		}
		return *q.NucleusShare, true
	case "noTranscriptShare":
		return q.NoTranscriptShare, true
	}
	return 0, false
}

// robust is the median of values and their spread: 1.4826 times the median absolute deviation, or
// where that is 0, 1.2533 times the mean absolute deviation from the median.
func robust(v []float64) (float64, float64) {
	m := median(v)
	dev := make([]float64, len(v))
	var sum float64
	for i, x := range v {
		dev[i] = math.Abs(x - m)
		sum += dev[i]
	}
	if mad := median(dev); mad > 0 {
		return m, mul(1.4826, mad)
	}
	return m, mul(1.2533, sum/float64(len(v)))
}

// Outliers are the samples far out on each quality number, by the rule.
func Outliers(samples []SampleDoc, rule OutlierRule) []Outlier {
	out := []Outlier{}
	for _, metric := range QCMetrics {
		var idx []int
		var raw, scaled []float64
		for i, sd := range samples {
			v, ok := MetricValue(sd.QC, metric.Key)
			if !ok || math.IsNaN(v) {
				continue
			}
			idx = append(idx, i)
			raw = append(raw, v)
			if metric.Share {
				scaled = append(scaled, v)
			} else {
				scaled = append(scaled, dmath.Log(1+v))
			}
		}
		if len(idx) < rule.MinSamples {
			continue
		}
		m, spread := robust(scaled)
		if !(spread > 0) {
			continue
		}
		mraw := median(raw)
		for j, i := range idx {
			z := (scaled[j] - m) / spread
			far := math.Abs(scaled[j] - m)
			matters := far >= rule.Points
			if !metric.Share {
				matters = far >= dmath.Log(rule.Fold)
			}
			if math.Abs(z) < rule.Z || !matters {
				continue
			}
			o := Outlier{Sample: samples[i].Sample, Name: samples[i].Name, Metric: metric.Key, Value: raw[j], Median: mraw, Spread: spread, Z: z}
			side := "above"
			if z < 0 {
				side = "below"
			}
			if metric.Share {
				o.Reason = fmt.Sprintf("%s %.1f%%, %.1f points %s the samples' median of %.1f%% (robust z %+.1f)", metric.Name, 100*raw[j], 100*math.Abs(raw[j]-mraw), side, 100*mraw, z)
			} else {
				o.Reason = fmt.Sprintf("%s %s, %s times the samples' median of %s (robust z %+.1f)", metric.Name, number3(raw[j]), number3(raw[j]/mraw), number3(mraw), z)
			}
			out = append(out, o)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return math.Abs(out[a].Z) > math.Abs(out[b].Z) })
	return out
}

// number3 writes a number with three significant digits ("112", "0.401", "11,479").
func number3(v float64) string {
	if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Sprint(v)
	}
	digits := 2 - int(math.Floor(math.Log10(math.Abs(v))))
	if digits < 0 {
		digits = 0
	}
	s := fmt.Sprintf("%.*f", digits, v)
	if digits == 0 && math.Abs(v) >= 10000 {
		return formatThousands(s)
	}
	return s
}

func formatThousands(s string) string {
	neg := s[0] == '-'
	if neg {
		s = s[1:]
	}
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// BundleFacts are what a sample's bundle says of where it was made: its run, slide, Xenium region,
// the software that analyzed it, and its panel (the batches a cohort's samples fall in).
type BundleFacts struct {
	Run      string   `json:"run"`
	Slide    string   `json:"slide"`
	Region   string   `json:"region"`
	Software string   `json:"software"`
	Panel    PanelRef `json:"panel"`
}

// Batch is one batch's samples (a slide's, a run's or a panel's) and the median of each quality
// number over them.
type Batch struct {
	Kind    string             `json:"kind"`
	Name    string             `json:"name"`
	Samples []string           `json:"samples"`
	Medians map[string]float64 `json:"medians"`
}

// PanelName names a panel as its batch: its design, else its name, with its version.
func PanelName(p PanelRef) string {
	name := p.Design
	if name == "" {
		name = p.Name
	}
	if p.Version != "" {
		name += " " + p.Version
	}
	return name
}

// Batches are the samples by slide, by run and by panel, each batch with its quality numbers'
// medians, in the order the samples first fall in them.
func Batches(samples []SampleDoc) []Batch {
	out := []Batch{}
	for _, kind := range []string{"slide", "run", "panel"} {
		var names []string
		members := map[string][]int{}
		for i, sd := range samples {
			b := sd.Bundle
			name := map[string]string{"slide": b.Slide, "run": b.Run, "panel": PanelName(b.Panel)}[kind]
			if name == "" {
				name = "not recorded"
			}
			if _, ok := members[name]; !ok {
				names = append(names, name)
			}
			members[name] = append(members[name], i)
		}
		for _, name := range names {
			b := Batch{Kind: kind, Name: name, Medians: map[string]float64{}}
			for _, metric := range QCMetrics {
				var v []float64
				for _, i := range members[name] {
					if x, ok := MetricValue(samples[i].QC, metric.Key); ok {
						v = append(v, x)
					}
				}
				if len(v) > 0 {
					b.Medians[metric.Key] = median(v)
				}
			}
			for _, i := range members[name] {
				b.Samples = append(b.Samples, samples[i].Sample)
			}
			out = append(out, b)
		}
	}
	return out
}
