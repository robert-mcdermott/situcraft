package simulate

import (
	"context"
	"crypto/sha256"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"situcraft/internal/register"
	"situcraft/internal/slide"
)

// blob is a nucleus found in an image: its centroid in the image's pixels.
type blob struct{ u, v float64 }

// blobs are the 4-connected pieces of a mask of at least minArea pixels.
func blobs(mask []bool, w, h, minArea int) []blob {
	seen := make([]bool, len(mask))
	var out []blob
	stack := []int{}
	for start := range mask {
		if !mask[start] || seen[start] {
			continue
		}
		var su, sv float64
		n := 0
		stack = append(stack[:0], start)
		seen[start] = true
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := i%w, i/w
			su, sv, n = su+float64(x)+0.5, sv+float64(y)+0.5, n+1
			for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nx, ny := x+d[0], y+d[1]
				if nx >= 0 && ny >= 0 && nx < w && ny < h && mask[ny*w+nx] && !seen[ny*w+nx] {
					seen[ny*w+nx] = true
					stack = append(stack, ny*w+nx)
				}
			}
		}
		if n >= minArea {
			out = append(out, blob{su / float64(n), sv / float64(n)})
		}
	}
	return out
}

// The simulated post-Xenium images show the section where their truth says: nuclei found in each
// image (haematoxylin's red, DAPI's brightness), mapped through the truth, lie on the true nuclei;
// landmarks placed at them fit the image as registration would, the leave-one-out error near the
// true error between them; and the images are the same however many cores render them.
func TestPostXeniumImages(t *testing.T) {
	p := Presets["small"]
	p.PostXenium = DefaultPostXenium()
	dir := t.TempDir()
	folder := filepath.Join(dir, "sim")
	sum, err := Run(context.Background(), p, folder, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.PostXeniumImages) != 2 {
		t.Fatalf("images %v", sum.PostXeniumImages)
	}
	regs, err := ReadRegistrations(folder)
	if err != nil || len(regs) != 2 {
		t.Fatalf("the truth: %v, %v", regs, err)
	}
	// The true nuclei: each visible nucleus's centroid (µm).
	tis, _, _, _, err := simulateTissue(p, func() bool { return false }, func(float64, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	var nuclei [][2]float64
	for _, c := range tis.cells {
		if !c.nucleus {
			continue
		}
		var sx, sy float64
		for _, i := range c.nucleusPixel {
			sx += (float64(int(i)%tis.w) + 0.5) * p.PixelUm
			sy += (float64(int(i)/tis.w) + 0.5) * p.PixelUm
		}
		nuclei = append(nuclei, [2]float64{sx / float64(len(c.nucleusPixel)), sy / float64(len(c.nucleusPixel))})
	}
	nearest := func(x, y float64) (float64, [2]float64) {
		best, at := math.Inf(1), [2]float64{}
		for _, n := range nuclei {
			if d := math.Hypot(n[0]-x, n[1]-y); d < best {
				best, at = d, n
			}
		}
		return best, at
	}
	for _, r := range regs {
		im, err := slide.Open(filepath.Join(folder, filepath.FromSlash(r.Image)))
		if err != nil {
			t.Fatal(err)
		}
		lv := im.Levels[0]
		if lv.Width != r.Width || lv.Height != r.Height || len(im.Levels) < 2 {
			t.Fatalf("%s: %d × %d, %d levels; the truth says %d × %d", r.Image, lv.Width, lv.Height, len(im.Levels), r.Width, r.Height)
		}
		// The nuclei: haematoxylin's optical density in red above 0.2, or DAPI above 1,000.
		mask := make([]bool, lv.Width*lv.Height)
		for ty := 0; ty < lv.Down; ty++ {
			for tx := 0; tx < lv.Across; tx++ {
				tile, err := im.Tile(0, 0, tx, ty)
				if err != nil {
					t.Fatal(err)
				}
				for y := 0; y < lv.TileHeight && ty*lv.TileHeight+y < lv.Height; y++ {
					for x := 0; x < lv.TileWidth && tx*lv.TileWidth+x < lv.Width; x++ {
						k := y*lv.TileWidth + x
						var on bool
						if r.Kind == "he" {
							on = -math.Log10(math.Max(1, float64(tile[3*k]))/241) > 0.2
						} else {
							on = int(tile[2*k])|int(tile[2*k+1])<<8 > 1000
						}
						mask[(ty*lv.TileHeight+y)*lv.Width+tx*lv.TileWidth+x] = on
					}
				}
			}
		}
		im.Close()
		ps := r.Parameters.PixelUm
		found := blobs(mask, lv.Width, lv.Height, int(6/(ps*ps)))
		var dists []float64
		var landmarks []register.Keypoint
		for _, b := range found {
			x, y := r.Map(b.u, b.v)
			d, at := nearest(x, y)
			dists = append(dists, d)
			if d < 0.5 {
				landmarks = append(landmarks, register.Keypoint{Moving: register.Point{X: b.u, Y: b.v}, Fixed: register.Point{X: at[0], Y: at[1]}})
			}
		}
		sort.Float64s(dists)
		median := dists[len(dists)/2]
		t.Logf("%s: %d × %d at %.4f µm; %d nuclei found of %d, mapped through the truth a median %.3f µm from a true nucleus", r.Image, r.Width, r.Height, ps, len(found), len(nuclei), median)
		// A half-pixel error of the truth's convention would be 0.26 µm on the H&E, 0.46 on the
		// immunofluorescence image.
		if len(found) < len(nuclei)*3/4 || median > 0.12 {
			t.Errorf("%s: %d nuclei found, a median %.3f µm from the true ones", r.Image, len(found), median)
		}
		// Twelve landmarks spread over the image (every k-th of those found), fitted as an affine
		// transform: its leave-one-out error against its true error over the section.
		sort.Slice(landmarks, func(i, j int) bool {
			return landmarks[i].Moving.X+landmarks[i].Moving.Y < landmarks[j].Moving.X+landmarks[j].Moving.Y
		})
		var chosen []register.Keypoint
		for i := 0; i < len(landmarks) && len(chosen) < 12; i += max(1, len(landmarks)/12) {
			chosen = append(chosen, landmarks[i])
		}
		fit, err := register.FitKeypoints(register.ModelAffine, chosen)
		if err != nil {
			t.Fatal(err)
		}
		var sq float64
		n := 0
		for v := 0.5; v < float64(r.Height); v += 7 {
			for u := 0.5; u < float64(r.Width); u += 7 {
				tx, ty := r.Map(u, v)
				if tx < 0 || ty < 0 || tx > p.WidthUm || ty > p.HeightUm {
					continue
				}
				fx, fy := fit.Transform.Apply(u, v)
				sq += (fx-tx)*(fx-tx) + (fy-ty)*(fy-ty)
				n++
			}
		}
		trueRMS := math.Sqrt(sq / float64(n))
		t.Logf("%s: 12 landmarks fitted as an affine transform: residuals %.3f µm, left out %.3f µm, the true error over the section %.3f µm", r.Image, fit.RMS, fit.LooRMS, trueRMS)
		if trueRMS > 2*r.Parameters.DeformationUm || fit.LooRMS < trueRMS/3 || fit.LooRMS > 3*trueRMS {
			t.Errorf("%s: left out %.3f µm against a true error of %.3f µm", r.Image, fit.LooRMS, trueRMS)
		}
	}

	// The same images on one core.
	prev := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(prev)
	again := filepath.Join(dir, "again")
	if _, err := Run(context.Background(), p, again, "test", nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range regs {
		a, _ := os.ReadFile(filepath.Join(folder, filepath.FromSlash(r.Image)))
		b, _ := os.ReadFile(filepath.Join(again, filepath.FromSlash(r.Image)))
		if sha256.Sum256(a) != sha256.Sum256(b) {
			t.Errorf("%s differs on one core", r.Image)
		}
	}
}

// Without its deformation, an image's truth is its transform alone, and landmarks at its nuclei fit
// it back to within a small share of a pixel.
func TestPostXeniumWithoutDeformation(t *testing.T) {
	p := Presets["small"]
	p.PostXenium = DefaultPostXenium()
	p.PostXenium.HE.DeformationUm, p.PostXenium.IF = 0, nil
	if err := p.Check(); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(t.TempDir(), "sim")
	if _, err := Run(context.Background(), p, folder, "test", nil); err != nil {
		t.Fatal(err)
	}
	regs, err := ReadRegistrations(folder)
	if err != nil || len(regs) != 1 || len(regs[0].Deformation) != 0 {
		t.Fatalf("%+v, %v", regs, err)
	}
	r := regs[0]
	x, y := r.Map(10, 20)
	tr := r.Transform
	if x != tr[0][0]*10+tr[0][1]*20+tr[0][2] || y != tr[1][0]*10+tr[1][1]*20+tr[1][2] {
		t.Errorf("without a deformation, (10, 20) maps to (%v, %v)", x, y)
	}
	matrix, err := os.ReadFile(filepath.Join(folder, "truth", "post_xenium", "simulated_he_imagealignment.csv"))
	if err != nil {
		t.Fatal(err)
	}
	al, err := register.ReadAlignment(matrix)
	if err != nil || !al.Matrix.Reflected() || math.Abs(al.Matrix.Then(register.Scaling(p.PixelUm)).A-tr[0][0]) > 1e-12 {
		t.Errorf("the truth's matrix %+v, %v", al, err)
	}
	// A mosaic is refused.
	m := Presets["large"]
	m.PostXenium = DefaultPostXenium()
	if err := m.Check(); err == nil {
		t.Error("post-Xenium images of a mosaic accepted")
	}
}
