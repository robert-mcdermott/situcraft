package validation

// Post-Xenium images (wave 5, slice 1; research.md §1.7, §1.8 and §2.1): the pathology fixture,
// the breast cancer section of Janesick et al. (2023), replicate 1 (node validation/fetch.mjs
// xenium-breast-cancer-rep1). Its bundle, of Xenium Onboard Analysis 1.0.1, checked as any; its
// post-Xenium H&E (RGB, deflate) and immunofluorescence (CD20, HER2, DAPI; JPEG 2000) images read a
// tile at a time, every level of every channel the pixels tifffile and imagecodecs read
// (validation/reference/images/levels.py); and their alignments as 10x wrote them, the H&E's scale
// its pixels' size as its metadata says, the immunofluorescence's not (its metadata gives the
// Xenium image's pixel size, its pixels are 3.5 times larger): an image is placed by its alignment.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"situcraft/internal/nuclei"
	"situcraft/internal/register"
	"situcraft/internal/slide"
	"situcraft/internal/xenium"
)

const pathologyFixture = "xenium-breast-cancer-rep1"

// pathologyBundle is the fixture's bundle folder, named as 10x names the sample.
const pathologyBundle = "Xenium_FFPE_Human_Breast_Cancer_Rep1"

func TestPostXeniumImages(t *testing.T) {
	if testing.Short() || os.Getenv("SITUCRAFT_REQUIRE_DATA") == "" {
		t.Skip("set SITUCRAFT_REQUIRE_DATA=1 to read the pathology fixture's images")
	}
	root := filepath.Join(dataRoot(), pathologyFixture)
	if _, err := os.Stat(filepath.Join(root, pathologyBundle, "experiment.xenium")); err != nil {
		if required(pathologyFixture) {
			t.Fatalf("%s is missing (node validation/fetch.mjs %s)", root, pathologyFixture)
		}
		t.Skipf("%s is missing (node validation/fetch.mjs %s)", root, pathologyFixture)
	}

	t.Run("the XOA 1.0.1 bundle", func(t *testing.T) {
		b, err := xenium.Open(filepath.Join(root, pathologyBundle), xenium.Options{CacheDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()
		r, err := xenium.Validate(context.Background(), b, xenium.ValidateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		fatal := 0
		for _, f := range r.Findings {
			if f.Severity == "fatal" {
				fatal++
				t.Errorf("fatal: %s: %s", f.Code, f.Title)
			}
		}
		s := r.Summary
		t.Logf("%s (%s): %d cells, %d transcripts, %d Q20 gene transcripts, %.4f assigned; %d of %d metrics reproduced; %d findings", r.Version, r.Support, s.Cells, s.Transcripts, s.Q20GeneTranscripts, s.AssignedFraction, s.MetricsAgreeing, s.MetricsCompared, len(r.Findings))
		// 10x's metrics_summary.csv: 167,780 cells; the transcripts as their row groups hold them.
		if s.Cells != 167780 || s.Transcripts != 42638083 || r.Support != "supported" {
			t.Errorf("%d cells, %d transcripts, support %s", s.Cells, s.Transcripts, r.Support)
		}
	})

	raw, err := os.ReadFile(filepath.Join("reference", "images", pathologyFixture+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		Images map[string]struct {
			Levels []struct {
				Shape  []int    `json:"shape"`
				Axes   string   `json:"axes"`
				SHA256 []string `json:"sha256"`
			} `json:"levels"`
		} `json:"images"`
	}
	if err := json.Unmarshal(raw, &ref); err != nil {
		t.Fatal(err)
	}
	for name, want := range ref.Images {
		t.Run(name, func(t *testing.T) {
			im, err := slide.Open(filepath.Join(root, "images", name))
			if err != nil {
				t.Fatal(err)
			}
			defer im.Close()
			if len(im.Levels) != len(want.Levels) {
				t.Fatalf("%d levels, tifffile %d", len(im.Levels), len(want.Levels))
			}
			differ := 0
			for l, lv := range want.Levels {
				for c, sum := range lv.SHA256 {
					if got := levelDigest(t, im, l, c); got != sum {
						differ++
						t.Errorf("level %d channel %d: SHA-256 %s, tifffile's %s", l, c, got[:12], sum[:12])
					}
				}
			}
			t.Logf("%s, %d channels, %d levels (%d×%d), %s: %d levels of channels differ from tifffile's", im.Kind, len(im.Channels), len(im.Levels), im.Width, im.Height, im.Compression, differ)
		})
	}

	t.Run("the alignments", func(t *testing.T) {
		for _, c := range []struct {
			image, alignment string
			agrees           bool
		}{
			{"Xenium_FFPE_Human_Breast_Cancer_Rep1_he_image.ome.tif", "Xenium_FFPE_Human_Breast_Cancer_Rep1_he_imagealignment.csv", true},
			{"Xenium_FFPE_Human_Breast_Cancer_Rep1_if_image.ome.tif", "Xenium_FFPE_Human_Breast_Cancer_Rep1_if_imagealignment.csv", false},
		} {
			data, err := os.ReadFile(filepath.Join(root, "images", c.alignment))
			if err != nil {
				t.Fatal(err)
			}
			al, err := register.ReadAlignment(data)
			if err != nil {
				t.Fatal(err)
			}
			im, err := slide.Open(filepath.Join(root, "images", c.image))
			if err != nil {
				t.Fatal(err)
			}
			placed := al.Matrix.Scale() * 0.2125
			// The Xenium image's centre lies on the image.
			inv, _ := al.Matrix.Invert()
			x, y := inv.Apply(35416/2, 25778/2)
			on := x >= 0 && y >= 0 && x < float64(im.Width) && y < float64(im.Height)
			ratio := placed / im.PixelSize
			t.Logf("%s: %s, scale %.5f (%.4f µm a pixel; its metadata says %.4f), turned %.1f°, mirrored %v; the Xenium image's centre at (%.0f, %.0f) of %d×%d", c.image, al.Kind, al.Matrix.Scale(), placed, im.PixelSize, al.Matrix.Rotation(), al.Matrix.Reflected(), x, y, im.Width, im.Height)
			if agrees := math.Abs(ratio-1) < 0.002; agrees != c.agrees || !on {
				t.Errorf("%s: the alignment's pixel size %.4f µm against the metadata's %.4f (agreeing %v, expected %v); the Xenium image's centre on it %v", c.image, placed, im.PixelSize, agrees, c.agrees, on)
			}
			im.Close()
		}
	})

	// Automatic registration (wave 5, slice 3): each image registered to the bundle's DAPI by its
	// nuclei, without its alignment, its scale not taken from its metadata, against 10x's alignment:
	// how far apart the two put the Xenium image's points (over its extent, every 100 µm).
	t.Run("automatic registration", func(t *testing.T) {
		b, err := xenium.Open(filepath.Join(root, pathologyBundle), xenium.Options{CacheDir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		defer b.Close()
		fixed, pixel, _, closeFixed, err := nuclei.FromBundle(b)
		if err != nil {
			t.Fatal(err)
		}
		defer closeFixed()
		ext := [2]float64{float64(fixed.Levels()[0][0]) * pixel, float64(fixed.Levels()[0][1]) * pixel}
		for _, c := range []struct{ image, alignment string }{
			{"Xenium_FFPE_Human_Breast_Cancer_Rep1_he_image.ome.tif", "Xenium_FFPE_Human_Breast_Cancer_Rep1_he_imagealignment.csv"},
			{"Xenium_FFPE_Human_Breast_Cancer_Rep1_if_image.ome.tif", "Xenium_FFPE_Human_Breast_Cancer_Rep1_if_imagealignment.csv"},
		} {
			data, _ := os.ReadFile(filepath.Join(root, "images", c.alignment))
			al, err := register.ReadAlignment(data)
			if err != nil {
				t.Fatal(err)
			}
			tenx := al.Matrix.Then(register.Scaling(pixel))
			im, err := slide.Open(filepath.Join(root, "images", c.image))
			if err != nil {
				t.Fatal(err)
			}
			channel := -1
			if im.Kind == slide.Channels {
				channel = nuclei.NuclearChannel(im.Description)
			}
			moving, what, err := nuclei.FromImage(im, channel, register.HAndE)
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			res, err := register.Register(context.Background(), fixed, moving, register.Options{Model: register.ModelSimilarity, NonRigid: true, FixedPixel: pixel, MovingPixel: im.PixelSize, Log: func(f string, a ...any) { t.Logf("  "+f, a...) }})
			if err != nil {
				t.Fatalf("%s: %v", c.image, err)
			}
			inv, _ := tenx.Invert()
			var sq, worst float64
			n := 0
			for y := 50.0; y < ext[1]; y += 100 {
				for x := 50.0; x < ext[0]; x += 100 {
					u, v := inv.Apply(x, y)
					gx, gy := res.Transform.Apply(u, v)
					d := math.Hypot(gx-x, gy-y)
					sq += d * d
					worst = math.Max(worst, d)
					n++
				}
			}
			rms := math.Sqrt(sq / float64(n))
			kept := 0
			for _, m := range res.Matches {
				if m.Kept {
					kept++
				}
			}
			t.Logf("%s (%s), in %s: %d of %d patches kept; %.4f µm a pixel (10x: %.4f), turned %.2f° (10x: %.2f°); from 10x's alignment %.2f µm root-mean-square, at most %.2f µm; warnings %v",
				c.image, what, time.Since(started).Round(time.Second), kept, len(res.Matches), res.Transform.Scale(), tenx.Scale(), res.Transform.Rotation(), tenx.Rotation(), rms, worst, res.Warnings)
			if res.Transform.Reflected() != tenx.Reflected() || rms > 5 {
				t.Errorf("%s: %.2f µm from 10x's alignment (mirrored %v, 10x's %v)", c.image, rms, res.Transform.Reflected(), tenx.Reflected())
			}
			// Each checked in µm (wave 5, slice 4): where 10x's alignment and the automatic one
			// place the image off, by patches between the registration's.
			for _, v := range []struct {
				name string
				p    register.Placement
			}{{"10x's alignment", register.Placement{T: tenx}}, {"the automatic alignment", register.Placement{T: res.Transform, D: res.Deformation}}} {
				chk, err := register.CheckLocal(context.Background(), fixed, moving, v.p, register.CheckOptions{FixedPixel: pixel})
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("  %s, checked: %d of %d patches matched; off by %.2f µm (median), %.2f (90th percentile), at most %.2f at (%.0f, %.0f) µm; the matches' own error at most %.2f µm",
					v.name, chk.Matched, chk.Checked, chk.Median, chk.P90, chk.Max, chk.Worst.X, chk.Worst.Y, chk.Noise)
				if chk.Matched < chk.Checked/2 || chk.Median > 2 {
					t.Errorf("%s of %s: %d of %d matched, median %.2f µm", v.name, c.image, chk.Matched, chk.Checked, chk.Median)
				}
			}
			im.Close()
		}
	})

	// Keypoints as Xenium Explorer writes them, made with each of 10x's matrices at places spread
	// over the image, fit back to the matrix: 10x's are similarities (one mirrored, one turned half a
	// turn), and the fit's handedness, scale and turn are theirs.
	t.Run("the alignments as keypoints", func(t *testing.T) {
		for _, name := range []string{"Xenium_FFPE_Human_Breast_Cancer_Rep1_he_imagealignment.csv", "Xenium_FFPE_Human_Breast_Cancer_Rep1_if_imagealignment.csv"} {
			data, err := os.ReadFile(filepath.Join(root, "images", name))
			if err != nil {
				t.Fatal(err)
			}
			al, err := register.ReadAlignment(data)
			if err != nil {
				t.Fatal(err)
			}
			m := al.Matrix
			var kps []register.Keypoint
			for _, p := range [][2]float64{{500, 700}, {13000, 400}, {7000, 9000}, {1200, 9500}, {12500, 8800}, {6000, 3000}} {
				x, y := m.Apply(p[0], p[1])
				kps = append(kps, register.Keypoint{Fixed: register.Point{X: x, Y: y}, Moving: register.Point{X: p[0], Y: p[1]}})
			}
			back, err := register.ReadAlignment(register.KeypointsCSV(kps))
			if err != nil {
				t.Fatal(err)
			}
			f := back.Matrix
			worst := 0.0
			for _, d := range []float64{f.A - m.A, f.B - m.B, f.D - m.D, f.E - m.E, (f.C - m.C) / 1e4, (f.F - m.F) / 1e4} {
				worst = math.Max(worst, math.Abs(d))
			}
			t.Logf("%s: a similarity (A = %s, B = %s); its keypoints fitted back within %.2g, residuals at most %.2g pixels", name, map[bool]string{true: "−E", false: "E"}[m.Reflected()], map[bool]string{true: "D", false: "−D"}[m.Reflected()], worst, slices.Max(back.Residuals))
			similar := (m.Reflected() && m.A == -m.E && m.B == m.D) || (!m.Reflected() && m.A == m.E && m.B == -m.D)
			if !similar || worst > 1e-9 || back.Kind != "keypoints" || f.Reflected() != m.Reflected() {
				t.Errorf("%s: %+v fitted as %+v (a similarity: %v)", name, m, f, similar)
			}
		}
	})
}

// levelDigest is the SHA-256 of a level of a channel, row-major as tifffile reads it: a row of
// tiles decoded at a time, over every core.
func levelDigest(t *testing.T, im *slide.Image, l, c int) string {
	t.Helper()
	lv := im.Levels[l]
	px := im.PixelBytes()
	h := sha256.New()
	tiles := make([][]byte, lv.Across)
	errs := make([]error, lv.Across)
	for ty := 0; ty < lv.Down; ty++ {
		var wg sync.WaitGroup
		for tx := 0; tx < lv.Across; tx++ {
			wg.Add(1)
			go func(tx int) {
				defer wg.Done()
				tiles[tx], errs[tx] = im.Tile(l, c, tx, ty)
			}(tx)
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		for y := 0; y < lv.TileHeight && ty*lv.TileHeight+y < lv.Height; y++ {
			for tx := 0; tx < lv.Across; tx++ {
				w := min(lv.TileWidth, lv.Width-tx*lv.TileWidth)
				h.Write(tiles[tx][y*lv.TileWidth*px : (y*lv.TileWidth+w)*px])
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
