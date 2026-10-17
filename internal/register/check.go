package register

// Checking an alignment in µm: where it is off, measured over the tissue by local matches. Each
// square patch of the Xenium image where there is tissue is compared with the image drawn there as
// the alignment places it (its transform and any deformation after it); their residual shift, by
// phase correlation, is how far the alignment draws the image from where it belongs there. The
// patches lie between those an automatic registration matched (a grid offset by half a patch), so
// that a version is not checked by the matches it was fitted to.
//
// A match has an error of its own. Where the alignment's error changes little from a patch to the
// next, the difference between neighbouring patches' errors is the matches' errors': their median
// estimates it (a normal's median absolute value is 0.6745 of its standard deviation). Where the
// alignment's error changes between neighbours, that change is counted too: the estimate is a bound
// of the matches' own error, and an error well above it is the alignment's.

import (
	"context"
	"errors"
	"math"
	"sort"
)

// Placement is a version's map of the image onto the tissue: its transform, then any deformation.
type Placement struct {
	T Affine
	D *BSpline
}

// Forward is where the placement draws a point of the image (its pixels) on the tissue (µm).
func (p Placement) Forward(u, v float64) (float64, float64) {
	x, y := p.T.Apply(u, v)
	if p.D == nil {
		return x, y
	}
	dx, dy := p.D.At(x, y)
	return x + dx, y + dy
}

// LocalError is one patch of a check: its centre on the tissue (µm), how far the alignment draws
// the image there from where it belongs (µm: drawn less true), and the correlation's quality; Matched
// when the patch matched clearly enough to say.
type LocalError struct {
	Centre  Point   `json:"centre"`
	Error   Point   `json:"error"`
	Size    float64 `json:"size"`
	Quality float64 `json:"quality"`
	Matched bool    `json:"matched"`
}

// CheckResult is a check of an alignment over the tissue.
type CheckResult struct {
	Patches []LocalError `json:"patches"`
	// PatchUm is the patches' side and Fine their µm per pixel.
	PatchUm float64 `json:"patchUm"`
	Fine    float64 `json:"fine"`
	// Of the matched patches' errors (µm): their median, 90th percentile, largest and
	// root-mean-square; Noise a bound of the matches' own error (root-mean-square), from
	// neighbouring patches.
	Checked int     `json:"checked"`
	Matched int     `json:"matched"`
	Median  float64 `json:"median"`
	P90     float64 `json:"p90"`
	Max     float64 `json:"max"`
	RMS     float64 `json:"rms"`
	Noise   float64 `json:"noise"`
	// Worst is the centre of the patch the alignment is most off at.
	Worst Point `json:"worst"`
}

// CheckOptions are what a check is asked.
type CheckOptions struct {
	// FixedPixel is the Xenium image's pixel size (µm).
	FixedPixel float64
	Progress   func(at float64, what string)
}

// CheckLocal measures where an alignment of moving (the post-Xenium image) to fixed (the Xenium
// image) is off.
func CheckLocal(ctx context.Context, fixed, moving Pyramid, p Placement, opt CheckOptions) (*CheckResult, error) {
	if opt.Progress == nil {
		opt.Progress = func(float64, string) {}
	}
	if !(opt.FixedPixel > 0) {
		return nil, errors.New("the Xenium image's pixel size is needed")
	}
	inv, ok := p.T.Invert()
	if !ok {
		return nil, errors.New("the alignment cannot be inverted")
	}
	fr, mr := newReader(fixed), newReader(moving)
	ext := [2]float64{mul(float64(fr.levels[0][0]), opt.FixedPixel), mul(float64(fr.levels[0][1]), opt.FixedPixel)}
	side := math.Min(160, math.Max(32, math.Min(ext[0], ext[1])/6))
	const px = 128
	fine := side / px
	res := &CheckResult{PatchUm: side, Fine: fine}
	coarse, _, err := signalOf(fr, opt.FixedPixel, side/4)
	if err != nil {
		return nil, err
	}
	coarse = normalized(coarse)
	// Centres between registration's: a grid offset by half a patch, as dense, at most 300.
	var centres [][2]float64
	spacing := side
	for {
		centres = centres[:0]
		for y := side; y+side/2 <= ext[1]; y += spacing {
			for x := side; x+side/2 <= ext[0]; x += spacing {
				if v, ok := coarse.Sample(x/(side/4), y/(side/4)); ok && v > 0.15 {
					centres = append(centres, [2]float64{x, y})
				}
			}
		}
		if len(centres) <= 300 {
			break
		}
		spacing *= 1.25
	}
	if len(centres) == 0 {
		return nil, errors.New("no patch of the Xenium image holds tissue to check by")
	}
	fl := fr.levelFor(opt.FixedPixel, fine)
	ml := mr.levelFor(p.T.Scale(), fine)
	mk := mr.factor(ml)
	res.Patches = make([]LocalError, len(centres))
	errs := make([]error, len(centres))
	done := make(chan struct{}, len(centres))
	go func() {
		n := 0
		for range done {
			n++
			opt.Progress(float64(n)/float64(len(centres)), "Matching patches")
		}
	}()
	parallelFor(len(centres), func(i int) {
		defer func() { done <- struct{}{} }()
		if ctx.Err() != nil {
			return
		}
		c := centres[i]
		res.Patches[i] = LocalError{Centre: Point{c[0], c[1]}, Size: side}
		corner := [2]float64{c[0] - side/2, c[1] - side/2}
		// The Xenium image's patch.
		fpix := mul(opt.FixedPixel, fr.factor(fl))
		x0, y0 := int(math.Floor(corner[0]/fpix))-1, int(math.Floor(corner[1]/fpix))-1
		w := int(math.Ceil(side/fpix)) + 3
		region, err := fr.p.Region(fl, x0, y0, w, w)
		if err != nil {
			errs[i] = err
			return
		}
		fp := region.Warp(Affine{A: fine / fpix, E: fine / fpix, C: (corner[0] - mul(float64(x0), fpix)) / fpix, F: (corner[1] - mul(float64(y0), fpix)) / fpix}, px, px)
		// The image as the alignment draws it there: each pixel's point on the tissue taken back
		// through the deformation (from the last point's, which is near) and the transform.
		qs := make([][2]float64, px*px)
		qx0, qy0, qx1, qy1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		var back [2]float64
		for j := 0; j < px; j++ {
			for k := 0; k < px; k++ {
				x, y := corner[0]+mul(float64(k)+0.5, fine), corner[1]+mul(float64(j)+0.5, fine)
				if p.D != nil {
					if j == 0 && k == 0 {
						back[0], back[1] = p.D.Inverse(x, y)
					} else {
						back[0], back[1] = p.D.inverseFrom(x, y, back[0], back[1])
					}
					x, y = back[0], back[1]
				}
				u, v := inv.Apply(x, y)
				u, v = u/mk, v/mk
				qs[j*px+k] = [2]float64{u, v}
				qx0, qy0, qx1, qy1 = math.Min(qx0, u), math.Min(qy0, v), math.Max(qx1, u), math.Max(qy1, v)
			}
		}
		mx0, my0 := int(math.Floor(qx0))-2, int(math.Floor(qy0))-2
		mregion, err := mr.p.Region(ml, mx0, my0, int(math.Ceil(qx1))-mx0+3, int(math.Ceil(qy1))-my0+3)
		if err != nil {
			errs[i] = err
			return
		}
		mp := Plane{W: px, H: px, Pix: make([]float32, px*px), Mask: make([]bool, px*px)}
		covered := 0
		for k, q := range qs {
			if v, ok := mregion.Sample(q[0]-float64(mx0), q[1]-float64(my0)); ok {
				mp.Pix[k], mp.Mask[k] = float32(v), true
				covered++
			}
		}
		if covered < px*px*9/10 {
			return
		}
		dx, dy, q := PhaseCorrelate(normalized(fp), normalized(mp))
		res.Patches[i].Quality = q
		if q < 6 || math.Abs(dx) > px/4 || math.Abs(dy) > px/4 {
			return
		}
		// F(u) ≈ W(u − d): the content belonging at u is drawn at u − d.
		res.Patches[i].Error = Point{-mul(dx, fine), -mul(dy, fine)}
		res.Patches[i].Matched = true
	})
	close(done)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	summarize(res, spacing)
	return res, nil
}

// inverseFrom is Inverse started from a point near the answer.
func (b *BSpline) inverseFrom(y0, y1, x0, x1 float64) (float64, float64) {
	for k := 0; k < 30; k++ {
		dx, dy := b.At(x0, x1)
		nx, ny := y0-dx, y1-dy
		if math.Abs(nx-x0) < 1e-9 && math.Abs(ny-x1) < 1e-9 {
			return nx, ny
		}
		x0, x1 = nx, ny
	}
	return x0, x1
}

// summarize is a check's statistics, and the matches' own error from neighbouring patches (those a
// spacing apart along either axis).
func summarize(res *CheckResult, spacing float64) {
	var mags []float64
	var sq float64
	worst := -1.0
	for _, p := range res.Patches {
		res.Checked++
		if !p.Matched {
			continue
		}
		m := math.Sqrt(mul(p.Error.X, p.Error.X) + mul(p.Error.Y, p.Error.Y))
		mags = append(mags, m)
		sq += mul(m, m)
		if m > worst {
			worst, res.Worst = m, p.Centre
		}
	}
	res.Matched = len(mags)
	if len(mags) == 0 {
		return
	}
	sort.Float64s(mags)
	at := func(q float64) float64 { return mags[min(len(mags)-1, int(mul(q, float64(len(mags)))))] }
	res.Median, res.P90, res.Max = at(0.5), at(0.9), mags[len(mags)-1]
	res.RMS = math.Sqrt(sq / float64(len(mags)))
	var diffs []float64
	near := func(a, b float64) bool { return math.Abs(a-b) < spacing*0.01 }
	for i, a := range res.Patches {
		if !a.Matched {
			continue
		}
		for _, b := range res.Patches[i+1:] {
			if !b.Matched {
				continue
			}
			dx, dy := b.Centre.X-a.Centre.X, b.Centre.Y-a.Centre.Y
			if (near(dx, spacing) && near(dy, 0)) || (near(dx, 0) && near(dy, spacing)) {
				diffs = append(diffs, math.Abs(a.Error.X-b.Error.X), math.Abs(a.Error.Y-b.Error.Y))
			}
		}
	}
	if len(diffs) >= 4 {
		sort.Float64s(diffs)
		// A component's σ from its differences' median, (σ√2)·0.6745; the 2-D error is σ√2.
		sigma := diffs[len(diffs)/2] / 0.6745 / math.Sqrt2
		res.Noise = mul(sigma, math.Sqrt2)
	}
}
