package cohort

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

// samplesQC makes samples with quality numbers: each one's median transcripts, its share without a
// transcript, its share with a nucleus (nil: not recorded), and its slide.
func samplesQC(transcripts, empty []float64, nuclei []*float64, slides []string) []SampleDoc {
	out := make([]SampleDoc, len(transcripts))
	for i := range out {
		out[i] = SampleDoc{Sample: "s-" + string(rune('a'+i)), Name: "S" + string(rune('A'+i)),
			Bundle: BundleFacts{Slide: slides[i], Run: "run " + slides[i], Panel: PanelRef{Design: "hLung_v1.1", Version: "1.1.0"}}}
		out[i].QC = QC{MedianTranscripts: transcripts[i], NoTranscriptShare: empty[i], NucleusShare: nuclei[i], CellsPerMm2: 1000, MedianGenes: 40, TranscriptsPer100: 150, MedianArea: 80}
	}
	return out
}

// Worked by hand: six samples, one with a fifth of the others' transcripts, one with a fifth of its
// cells without a transcript, one with fewer nuclei where the others agree exactly (the mean
// absolute deviation the spread), and one a little off where the others agree closely (not far
// enough to matter).
func TestOutliersByHand(t *testing.T) {
	one := 1.0
	low := 0.6
	nuclei := []*float64{&one, &one, &one, &one, &low, &one}
	s := samplesQC([]float64{100, 104, 98, 102, 101, 20}, []float64{0.01, 0.012, 0.011, 0.009, 0.01, 0.2}, nuclei, []string{"1", "1", "1", "2", "2", "2"})
	got := Outliers(s, DefaultOutlierRule)
	keys := map[string]Outlier{}
	for _, o := range got {
		keys[o.Name+" "+o.Metric] = o
	}
	if len(got) != 3 {
		t.Fatalf("%d flags: %+v", len(got), got)
	}
	// Transcripts: log(1 + x); median of the logs between 100's and 101's.
	o, ok := keys["SF medianTranscripts"]
	logs := []float64{math.Log(101), math.Log(105), math.Log(99), math.Log(103), math.Log(102), math.Log(21)}
	m := (math.Log(101) + math.Log(102)) / 2
	dev := []float64{}
	for _, v := range logs {
		dev = append(dev, math.Abs(v-m))
	}
	z := (math.Log(21) - m) / (1.4826 * median(dev))
	if !ok || math.Abs(o.Z-z) > 1e-9 || o.Value != 20 || o.Median != 100.5 || !strings.Contains(o.Reason, "median transcripts 20.0, 0.199 times the samples' median of 100") || !strings.Contains(o.Reason, "robust z -") {
		t.Errorf("transcripts: %+v (z want %.4f)", o, z)
	}
	if o, ok := keys["SF noTranscriptShare"]; !ok || o.Z <= 0 || !strings.Contains(o.Reason, "share of cells without a transcript 20.0%, 18.9 points above the samples' median of 1.0%") {
		t.Errorf("no transcript: %+v", o)
	}
	// Nuclei: five alike, so the spread is 1.2533 times the mean absolute deviation (0.4 / 6).
	if o, ok := keys["SE nucleusShare"]; !ok || math.Abs(o.Z-(-0.4/(1.2533*0.4/6))) > 1e-9 || math.Abs(o.Spread-1.2533*0.4/6) > 1e-15 {
		t.Errorf("nuclei: %+v", o)
	}
	// Strongest first.
	if math.Abs(got[0].Z) < math.Abs(got[1].Z) || math.Abs(got[1].Z) < math.Abs(got[2].Z) {
		t.Errorf("order: %v %v %v", got[0].Z, got[1].Z, got[2].Z)
	}
	// Close agreement, one 3% off: far by its z-score, not by enough to matter.
	close := samplesQC([]float64{100, 100.1, 100, 99.9, 100, 103}, []float64{0.01, 0.01, 0.01, 0.01, 0.01, 0.01}, make([]*float64, 6), []string{"1", "1", "1", "1", "1", "1"})
	if got := Outliers(close, DefaultOutlierRule); len(got) != 0 {
		t.Errorf("a 3%% difference flagged: %+v", got)
	}
	// Four samples are too few to call one far out; the nuclei not recorded are left out.
	if got := Outliers(s[:4], DefaultOutlierRule); len(got) != 0 {
		t.Errorf("four samples: %+v", got)
	}
	if _, ok := MetricValue(close[0].QC, "nucleusShare"); ok {
		t.Error("a nucleus share not recorded has a value")
	}
}

func TestBatches(t *testing.T) {
	one := 1.0
	s := samplesQC([]float64{100, 104, 98, 102, 101, 20}, []float64{0.01, 0.012, 0.011, 0.009, 0.01, 0.2}, []*float64{&one, nil, &one, nil, &one, nil}, []string{"1", "1", "1", "2", "2", ""})
	b := Batches(s)
	var got []string
	for _, x := range b {
		got = append(got, x.Kind+":"+x.Name+":"+strings.Join(x.Samples, ","))
	}
	want := []string{"slide:1:s-a,s-b,s-c", "slide:2:s-d,s-e", "slide:not recorded:s-f", "run:run 1:s-a,s-b,s-c", "run:run 2:s-d,s-e", "run:run :s-f", "panel:hLung_v1.1 1.1.0:s-a,s-b,s-c,s-d,s-e,s-f"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batches %v,\nwant %v", got, want)
	}
	if b[0].Medians["medianTranscripts"] != 100 || b[1].Medians["medianTranscripts"] != 101.5 || b[1].Medians["nucleusShare"] != 1 || b[2].Medians["nucleusShare"] != 0 && len(b[2].Medians) != 7 {
		t.Errorf("medians %v %v %v", b[0].Medians, b[1].Medians, b[2].Medians)
	}
	if _, ok := b[2].Medians["nucleusShare"]; ok {
		t.Error("a batch without nuclei recorded has a median nucleus share")
	}
	if number3(11479.2) != "11,479" || number3(0.40123) != "0.401" || number3(112.4) != "112" || number3(-3.14159) != "-3.14" {
		t.Errorf("number3: %s %s %s %s", number3(11479.2), number3(0.40123), number3(112.4), number3(-3.14159))
	}
}
