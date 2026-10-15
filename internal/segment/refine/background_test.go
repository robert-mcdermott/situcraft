package refine

// The background's calibration on simulated truth (wave 3, slice 7; research.md §3.2, the
// background): tissues like the standard one at several background levels and of each kind,
// refined with the Balanced settings. For each, the background's share of the modelled transcripts
// by the truth, by the model's rates (false decodes and ambient RNA apart, against the simulator's
// expectations) and in no cell after the summary; how many of the transcripts left in no cell truly
// are background (precision) and how much of the background they are (recall); and the cells'
// accuracy. Slow (about half a minute a tissue); run with
//
//	REFINE_BACKGROUND=all go test ./internal/segment/refine -run BackgroundOnSimulatedTruth -v -timeout 30m
//
// or a comma-separated list of the tissues. REFINE_BACKGROUND_GENES=1 breaks the background down by
// the genes' expression; REFINE_PERIMETER, REFINE_BUBBLE and REFINE_NUCLEAR change those settings.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"situcraft/internal/simulate"
)

// backgroundTissues are the standard tissue at several background levels (ambient RNA and false
// decodes, per 100 µm²) and of each kind alone.
var backgroundTissues = []struct {
	name                  string
	ambient, falseDecodes float64
}{
	{"low", 0.6, 0.005},
	{"standard", 3, 0.025},
	{"high", 12, 0.1},
	{"ambient-only", 6, 0},
	{"false-decodes-only", 0, 0.05},
}

func TestBackgroundOnSimulatedTruth(t *testing.T) {
	want := os.Getenv("REFINE_BACKGROUND")
	if want == "" {
		t.Skip("set REFINE_BACKGROUND=all, or to a list of tissues, to run")
	}
	for _, tissue := range backgroundTissues {
		if want != "all" && !slices.Contains(strings.Split(want, ","), tissue.name) {
			continue
		}
		t.Run(tissue.name, func(t *testing.T) {
			p := simulate.Presets["standard"]
			p.Ambient, p.FalseDecodes = tissue.ambient, tissue.falseDecodes
			folder := filepath.Join(t.TempDir(), tissue.name)
			if _, err := simulate.Run(context.Background(), p, folder, "test", nil); err != nil {
				t.Fatal(err)
			}
			in, idx, err := BundleInputs(openBundle(t, folder), 20)
			if err != nil {
				t.Fatal(err)
			}
			opt := DefaultOptions()
			for _, knob := range []struct {
				name string
				to   *float64
			}{{"REFINE_PERIMETER", &opt.Sampler.PerimeterFactor}, {"REFINE_BUBBLE", &opt.Sampler.Bubble}, {"REFINE_NUCLEAR", &opt.Model.NuclearPrior}} {
				if v := os.Getenv(knob.name); v != "" {
					*knob.to, _ = strconv.ParseFloat(v, 64)
				}
			}
			start := time.Now()
			res, err := Run(context.Background(), in, Region{}, opt, nil)
			if err != nil {
				t.Fatal(err)
			}
			took := time.Since(start)
			tr := loadTruth(t, folder)
			n := float64(len(idx.Rows))
			var truth, found, both float64
			for i, r := range idx.Rows {
				isBackground := string(tr.trueCell[r]) == "UNASSIGNED"
				if isBackground {
					truth++
				}
				if res.Assigned[i] == 0 {
					found++
					if isBackground {
						both++
					}
				}
			}
			// The simulator's expected background of quality 20 or more, by kind, as shares.
			kept := (1 - p.BackgroundLowQuality) * p.WidthUm * p.HeightUm / 100 / n
			trueFalse, trueAmbient := kept*p.FalseDecodes*float64(p.Genes), kept*p.Ambient
			falseShare, ambientShare := res.FalseDecodes/n, res.Ambient/n
			counted := map[int]bool{}
			byRow := map[int]string{}
			for i, r := range idx.Rows {
				counted[r] = true
				if c := res.Assigned[i]; c > 0 {
					byRow[r] = idx.CellIDs[res.Kept[c-1]]
				}
			}
			consensus := Footprint(res)
			l := res.State.Lat
			ps := tr.trueMask.PixelSize
			refined := tr.score(counted, func(row int) string { return byRow[row] }, func(x, y int) string {
				cx := int((float64(x) + 0.5) * ps / l.VoxelUm)
				cy := int((float64(y) + 0.5) * ps / l.VoxelUm)
				if cx >= l.W || cy >= l.H {
					return ""
				}
				if c := consensus[cy*l.W+cx]; c > 0 {
					return idx.CellIDs[res.Kept[c-1]]
				}
				return ""
			})
			precision, recall := both/max(1, found), both/max(1, truth)
			t.Logf("%s (ambient %g, false decodes %g per 100 µm²): %d transcripts, %v", tissue.name, tissue.ambient, tissue.falseDecodes, len(idx.Rows), took.Round(time.Second))
			t.Logf("  background: truth %.2f%%; the model's rates %.2f%% (false decodes %.2f%%, expected %.2f%%; ambient RNA %.2f%%, expected %.2f%%); in no cell %.2f%%",
				100*truth/n, 100*(falseShare+ambientShare), 100*falseShare, 100*trueFalse, 100*ambientShare, 100*trueAmbient, 100*found/n)
			t.Logf("  of the transcripts in no cell %.1f%% are background (precision), %.2f%% of all are cells'; of the background %.1f%% is in no cell (recall)", 100*precision, 100*(found-both)/n, 100*recall)
			t.Logf("  %.1f%% of transcripts in their true cell, median IoU %.3f", 100*refined.accuracy, refined.medianIoU)
			if os.Getenv("REFINE_BACKGROUND_GENES") != "" {
				logBackgroundByGene(t, res, in, idx, tr)
			}
			// The negative controls measure the false decodes: within a fifth of the truth.
			if trueFalse > 0 && !(falseShare > 0.8*trueFalse && falseShare < 1.2*trueFalse) {
				t.Errorf("false decodes %.2f%% of the transcripts, the truth's %.2f%%", 100*falseShare, 100*trueFalse)
			}
			// Cells' transcripts left in no cell: at most 1% of the transcripts.
			if wrong := (found - both) / n; wrong > 0.01 {
				t.Errorf("%.2f%% of the transcripts are cells' left in no cell", 100*wrong)
			}
			if refined.accuracy < 0.85 || refined.medianIoU < 0.70 {
				t.Errorf("%.1f%% of transcripts in their true cell, median IoU %.3f", 100*refined.accuracy, refined.medianIoU)
			}
		})
	}
}

// logBackgroundByGene logs the background by gene, in fifths by their transcripts: the truth's, the
// model's rates' (and their ambient part) and the transcripts the summary leaves in no cell.
func logBackgroundByGene(t *testing.T, res *Result, in *Inputs, idx *BundleIndex, tr *truthOf) {
	t.Helper()
	m := res.Model
	g := m.Genes
	all, bgTrue, bgFound := make([]float64, g), make([]float64, g), make([]float64, g)
	for i, r := range idx.Rows {
		j := in.Transcripts.Gene[i]
		all[j]++
		if string(tr.trueCell[r]) == "UNASSIGNED" {
			bgTrue[j]++
		}
		if res.Assigned[i] == 0 {
			bgFound[j]++
		}
	}
	order := make([]int, g)
	for j := range order {
		order[j] = j
	}
	slices.SortFunc(order, func(a, b int) int { return int(all[a] - all[b]) })
	for q := 0; q < 5; q++ {
		var a, bt, bf, rates, ambient float64
		for _, j := range order[q*g/5 : (q+1)*g/5] {
			a += all[j]
			bt += bgTrue[j]
			bf += bgFound[j]
			rates += m.Bg[j] * m.Region
			ambient += m.Ambient[j] * m.Region
		}
		t.Logf("  genes %d–%d by transcripts (%.0f): background truth %.0f, the rates %.0f (ambient %.0f), in no cell %.0f", q*g/5+1, (q+1)*g/5, a, bt, rates, ambient, bf)
	}
}
