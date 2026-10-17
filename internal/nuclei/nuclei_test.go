package nuclei

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"testing"

	"situcraft/internal/register"
	"situcraft/internal/simulate"
	"situcraft/internal/slide"
	"situcraft/internal/xenium"
)

// errorOver is how far a registration puts the image's pixels from where its truth does, over the
// section: root-mean-square and most (µm), every 5 pixels.
func errorOver(r *simulate.Registration, res *register.Result, deformed bool, w, h float64) (rms, worst float64) {
	var sq float64
	n := 0
	for v := 0.5; v < float64(r.Height); v += 5 {
		for u := 0.5; u < float64(r.Width); u += 5 {
			tx, ty := r.Map(u, v)
			if tx < 0 || ty < 0 || tx > w || ty > h {
				continue
			}
			x, y := res.Transform.Apply(u, v)
			if deformed && res.Deformation != nil {
				dx, dy := res.Deformation.At(x, y)
				x, y = x+dx, y+dy
			}
			e := math.Hypot(x-tx, y-ty)
			sq += e * e
			worst = math.Max(worst, e)
			n++
		}
	}
	return math.Sqrt(sq / float64(n)), worst
}

// Each simulated post-Xenium image registered to the Xenium image, against its truth: the search
// finds its scale (whatever the metadata says), turn and mirror; the affine fit is within its
// deformation of the truth, and the deformation brings it closer.
func TestRegistersSimulatedImages(t *testing.T) {
	if testing.Short() || raceDetector {
		t.Skip("simulates a tissue and registers two images four times (TestRegistrationIsReproducible runs under the race detector)")
	}
	p := simulate.Presets["small"]
	p.WidthUm, p.HeightUm = 360, 280
	p.PostXenium = simulate.DefaultPostXenium()
	// The immunofluorescence image undeformed: what the affine fit alone does.
	p.PostXenium.IF.DeformationUm = 0
	folder := filepath.Join(t.TempDir(), "sim")
	if _, err := simulate.Run(context.Background(), p, folder, "test", nil); err != nil {
		t.Fatal(err)
	}
	regs, err := simulate.ReadRegistrations(folder)
	if err != nil {
		t.Fatal(err)
	}
	b, err := xenium.Open(folder, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	fixed, pixel, name, closeFixed, err := FromBundle(b)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFixed()
	for _, r := range regs {
		im, err := slide.Open(filepath.Join(folder, filepath.FromSlash(r.Image)))
		if err != nil {
			t.Fatal(err)
		}
		channel := -1
		if im.Kind == slide.Channels {
			channel = NuclearChannel(im.Description)
		}
		moving, what, err := FromImage(im, channel, register.HAndE)
		if err != nil {
			t.Fatal(err)
		}
		for _, metadata := range []float64{im.PixelSize, 0.2125} {
			res, err := register.Register(context.Background(), fixed, moving, register.Options{Model: register.ModelAffine, NonRigid: true, FixedPixel: pixel, MovingPixel: metadata, Log: func(f string, a ...any) { t.Logf("  "+f, a...) }})
			if err != nil {
				t.Fatalf("%s: %v", r.Image, err)
			}
			kept := 0
			for _, m := range res.Matches {
				if m.Kept {
					kept++
				}
			}
			arms, aworst := errorOver(&r, res, false, p.WidthUm, p.HeightUm)
			drms, dworst := errorOver(&r, res, true, p.WidthUm, p.HeightUm)
			t.Logf("%s (%s against %s; its metadata %.4f µm): %d of %d patches kept; affine %.3f µm off (at most %.3f), deformed %.3f (at most %.3f); deformation %s; warnings %v",
				r.Image, what, name, metadata, kept, len(res.Matches), arms, aworst, drms, dworst, map[bool]string{true: "kept", false: "none"}[res.Deformation != nil], res.Warnings)
			// A deformation is kept where the image was deformed, and not where it was not.
			if arms > math.Max(0.25, 1.5*r.Parameters.DeformationUm) || drms > math.Max(0.25, arms) || drms > 0.6 || (res.Deformation != nil) != (r.Parameters.DeformationUm > 0) {
				t.Errorf("%s: affine %.3f µm off, deformed %.3f", r.Image, arms, drms)
			}
		}
		im.Close()
	}
}

// A registration is the same, bit for bit, on every computer and however many cores make it: its
// transform's and deformation's fingerprint (FNV-1a of their bits) is the one pinned here, which a
// computer of another kind (the continuous integration's) checks.
func TestRegistrationIsReproducible(t *testing.T) {
	p := simulate.Presets["small"]
	p.WidthUm, p.HeightUm = 200, 160
	p.PostXenium = simulate.DefaultPostXenium()
	p.PostXenium.IF = nil
	folder := filepath.Join(t.TempDir(), "sim")
	if _, err := simulate.Run(context.Background(), p, folder, "test", nil); err != nil {
		t.Fatal(err)
	}
	b, err := xenium.Open(folder, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	fixed, pixel, _, closeFixed, err := FromBundle(b)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFixed()
	im, err := slide.Open(filepath.Join(folder, simulate.PostXeniumFolder, "simulated_he_image.ome.tif"))
	if err != nil {
		t.Fatal(err)
	}
	defer im.Close()
	run := func() string {
		moving, _, err := FromImage(im, -1, register.HAndE)
		if err != nil {
			t.Fatal(err)
		}
		res, err := register.Register(context.Background(), fixed, moving, register.Options{Model: register.ModelAffine, NonRigid: true, FixedPixel: pixel, MovingPixel: im.PixelSize})
		if err != nil {
			t.Fatal(err)
		}
		return fingerprint(res)
	}
	got := run()
	if !raceDetector {
		prev := runtime.GOMAXPROCS(1)
		again := run()
		runtime.GOMAXPROCS(prev)
		if again != got {
			t.Errorf("on one core %s, on all %s", again, got)
		}
	}
	if got != "8fd08d1f" {
		t.Errorf("fingerprint %s, pinned 8fd08d1f", got)
	}
}

// fingerprint is FNV-1a of a registration's transform's and deformation's bits.
func fingerprint(res *register.Result) string {
	h := uint32(2166136261)
	add := func(v float64) {
		bits := math.Float64bits(v)
		for i := 0; i < 8; i++ {
			h ^= uint32(bits >> (8 * i) & 0xff)
			h *= 16777619
		}
	}
	t := res.Transform
	for _, v := range []float64{t.A, t.B, t.C, t.D, t.E, t.F} {
		add(v)
	}
	if d := res.Deformation; d != nil {
		for k := range d.DX {
			add(d.DX[k])
			add(d.DY[k])
		}
	}
	return fmt.Sprintf("%08x", h)
}

// A check of an alignment, against the truth: the simulated H&E placed by its true transform
// without its deformation is off at each patch's centre by the deformation there (drawn less true:
// minus the displacement), which the check measures to within a fraction of a micron, without bias,
// its bound of the matches' own error above what they are off by; placed by its automatic
// registration with a deformation, it is off far less, and the bound tells that error is the
// alignment's.
func TestCheckAgainstTruth(t *testing.T) {
	if testing.Short() || raceDetector {
		t.Skip("simulates a tissue and checks two alignments")
	}
	p := simulate.Presets["small"]
	p.WidthUm, p.HeightUm = 360, 280
	p.PostXenium = simulate.DefaultPostXenium()
	p.PostXenium.IF = nil
	folder := filepath.Join(t.TempDir(), "sim")
	if _, err := simulate.Run(context.Background(), p, folder, "test", nil); err != nil {
		t.Fatal(err)
	}
	regs, err := simulate.ReadRegistrations(folder)
	if err != nil {
		t.Fatal(err)
	}
	r := regs[0]
	b, err := xenium.Open(folder, xenium.Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	fixed, pixel, _, closeFixed, err := FromBundle(b)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFixed()
	im, err := slide.Open(filepath.Join(folder, filepath.FromSlash(r.Image)))
	if err != nil {
		t.Fatal(err)
	}
	defer im.Close()
	moving, _, err := FromImage(im, -1, register.HAndE)
	if err != nil {
		t.Fatal(err)
	}
	tr := r.Transform
	affine := register.Affine{A: tr[0][0], B: tr[0][1], C: tr[0][2], D: tr[1][0], E: tr[1][1], F: tr[1][2]}
	check, err := register.CheckLocal(context.Background(), fixed, moving, register.Placement{T: affine}, register.CheckOptions{FixedPixel: pixel})
	if err != nil {
		t.Fatal(err)
	}
	var sq, trueSq float64
	n := 0
	for _, pt := range check.Patches {
		if !pt.Matched {
			continue
		}
		dx, dy := r.Displacement(pt.Centre.X, pt.Centre.Y)
		ex, ey := pt.Error.X+dx, pt.Error.Y+dy
		sq += ex*ex + ey*ey
		trueSq += dx*dx + dy*dy
		n++
	}
	off := math.Sqrt(sq / float64(n))
	var mx, my float64
	for _, pt := range check.Patches {
		if pt.Matched {
			dx, dy := r.Displacement(pt.Centre.X, pt.Centre.Y)
			mx, my = mx+pt.Error.X+dx, my+pt.Error.Y+dy
		}
	}
	t.Logf("the measured errors less the true, on average: (%.3f, %.3f) µm", mx/float64(n), my/float64(n))
	trueRMS := math.Sqrt(trueSq / float64(n))
	t.Logf("by its transform without its deformation: %d of %d patches matched; measured %.3f µm root-mean-square (median %.3f, at most %.3f), truly %.3f; each measured error %.3f µm from the true one, root-mean-square; the matches' own error estimated %.3f µm",
		check.Matched, check.Checked, check.RMS, check.Median, check.Max, trueRMS, off, check.Noise)
	if check.Matched < check.Checked*3/4 || off > 0.5 || math.Abs(check.RMS-trueRMS) > 0.4 || math.Hypot(mx, my)/float64(n) > 0.1 || check.Noise < off/2 {
		t.Errorf("measured %.3f against %.3f µm truly; %.3f off each; noise %.3f", check.RMS, trueRMS, off, check.Noise)
	}
	// The automatic registration with its deformation.
	res, err := register.Register(context.Background(), fixed, moving, register.Options{Model: register.ModelAffine, NonRigid: true, FixedPixel: pixel, MovingPixel: im.PixelSize})
	if err != nil {
		t.Fatal(err)
	}
	good, err := register.CheckLocal(context.Background(), fixed, moving, register.Placement{T: res.Transform, D: res.Deformation}, register.CheckOptions{FixedPixel: pixel})
	if err != nil {
		t.Fatal(err)
	}
	var gx, gy float64
	for _, pt := range good.Patches {
		if pt.Matched {
			gx, gy = gx+pt.Error.X, gy+pt.Error.Y
		}
	}
	t.Logf("its errors on average (%.3f, %.3f) µm", gx/float64(good.Matched), gy/float64(good.Matched))
	t.Logf("by its automatic registration (deformed %v): measured %.3f µm root-mean-square (median %.3f, at most %.3f); the matches' own error %.3f µm", res.Deformation != nil, good.RMS, good.Median, good.Max, good.Noise)
	if res.Deformation == nil || good.RMS > check.RMS/2 || good.Median > 0.5 || good.Noise > good.RMS/2 {
		t.Errorf("the automatic registration checked %.3f µm root-mean-square, against %.3f without the deformation", good.RMS, check.RMS)
	}
}
