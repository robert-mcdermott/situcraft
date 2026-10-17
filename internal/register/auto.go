package register

// Automatic registration of a post-Xenium image to the Xenium image, by their nuclei: the Xenium
// image's DAPI against the image's nuclear signal (an H&E's haematoxylin, an immunofluorescence
// image's nuclear stain), from coarse to fine:
//
//  1. The search, on both images a few hundred pixels across. The image's pixel size is not trusted
//     (an image's metadata often gives the Xenium image's): a sweep of scales a quarter of an octave
//     apart about what the metadata says, each with the image as it is and mirrored. For each, the
//     turn (and what is left of the scale) from the log-polar forms of the magnitude spectra, both
//     turns half a turn apart, each with its translation by phase correlation (and the shifts a
//     correlation's wrapping allows); the candidate whose images' normalized mutual information is
//     highest, over enough of the Xenium image, is kept.
//  2. Refinement over a pyramid: the transform's six (or four, for a similarity) parameters moved by
//     a compass search to maximize the normalized mutual information, at each level twice as fine.
//  3. Local matches: square patches of the Xenium image where the tissue is, each with the image
//     drawn under the transform, their residual shift found by phase correlation; the model fitted
//     to the patches' centres and where they match, matches that disagree with the others dropped,
//     twice over.
//  4. Optionally, a smooth deformation (bspline.go) fitted to what the model leaves of the matches.
//
// Every operation is rounded on its own and shared among cores only where the results do not
// depend on how: the same images give the same transform on every computer.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"situcraft/internal/dmath"
)

// Pyramid is an image's levels, read a region at a time, as a signal high where nuclei are.
type Pyramid interface {
	// Levels are each level's width and height, full resolution first; a level's pixels are the
	// full resolution's times the ratio of their widths.
	Levels() [][2]int
	// Region is the signal of a level's pixels [x0, x0 + w) × [y0, y0 + h); pixels beyond the image
	// are not image.
	Region(level, x0, y0, w, h int) (Plane, error)
}

// Options are what an automatic registration is asked.
type Options struct {
	Model Model
	// NonRigid asks for a deformation after the model.
	NonRigid bool
	// FixedPixel is the Xenium image's pixel size (µm); MovingPixel the image's as its metadata says
	// (0 when it says none), about which scales are searched.
	FixedPixel, MovingPixel float64
	// Start, when given, is a transform to start from (the image's pixels to µm): the search is
	// skipped.
	Start *Affine
	// Progress hears how far it is (0 to 1); Log what it found.
	Progress func(at float64, what string)
	Log      func(format string, args ...any)
}

// Match is a patch's centre on the Xenium image (µm) and where it matched on the image (its
// pixels), how clearly, and whether the fit kept it.
type Match struct {
	Fixed   Point   `json:"fixed"`
	Moving  Point   `json:"moving"`
	Quality float64 `json:"quality"`
	Kept    bool    `json:"kept"`
}

// Candidate is one orientation and scale the search tried.
type Candidate struct {
	Mirrored  bool    `json:"mirrored"`
	PixelUm   float64 `json:"pixelUm"`
	TurnDeg   float64 `json:"turnDeg"`
	NMI       float64 `json:"nmi"`
	Overlap   float64 `json:"overlap"`
	Transform Affine  `json:"transform"`
}

// Result is a registration: the transform from the image's pixels to µm, a deformation after it,
// the local matches and the model's fit to those kept, and how the search went.
type Result struct {
	Transform   Affine    `json:"transform"`
	Deformation *BSpline  `json:"deformation,omitempty"`
	Matches     []Match   `json:"matches"`
	Fit         *Fitted   `json:"fit,omitempty"`
	Best        Candidate `json:"best"`
	// Candidates are the search's best few, best first.
	Candidates []Candidate `json:"candidates,omitempty"`
	// NMI is the images' normalized mutual information under the transform at the finest level
	// refined, Resolution that level's µm per pixel.
	NMI        float64 `json:"nmi"`
	Resolution float64 `json:"resolution"`
	// PatchUm is the local matches' patches' side, and Fine their µm per pixel.
	PatchUm float64 `json:"patchUm"`
	Fine    float64 `json:"fine"`
	// DeformedRMS and HeldOut are the matches' errors with the deformation: fitted, and each fifth
	// predicted by the deformation fitted to the others (µm); Smoothness the penalty used.
	DeformedRMS float64  `json:"deformedRms,omitempty"`
	HeldOut     float64  `json:"heldOut,omitempty"`
	AffineHeld  float64  `json:"affineHeldOut,omitempty"`
	Smoothness  float64  `json:"smoothness,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

// reader reads a pyramid at any resolution, whole or a region, caching whole levels.
type reader struct {
	p      Pyramid
	levels [][2]int
	whole  map[int]Plane
}

func newReader(p Pyramid) *reader {
	return &reader{p: p, levels: p.Levels(), whole: map[int]Plane{}}
}

// factor is how many full-resolution pixels a level's pixel spans.
func (r *reader) factor(level int) float64 {
	return float64(r.levels[0][0]) / float64(r.levels[level][0])
}

// levelFor is the coarsest level whose pixels (pixel µm at full resolution) are no larger than res.
func (r *reader) levelFor(pixel, res float64) int {
	l := 0
	for l+1 < len(r.levels) && mul(pixel, r.factor(l+1)) <= res*(1+1e-9) {
		l++
	}
	return l
}

// at is the whole image at res µm a pixel (pixel µm at full resolution), its signal normalized
// over its level, and how many full-resolution pixels its pixel spans.
func (r *reader) at(pixel, res float64) (Plane, float64, error) {
	l := r.levelFor(pixel, res)
	p, ok := r.whole[l]
	if !ok {
		var err error
		if p, err = r.p.Region(l, 0, 0, r.levels[l][0], r.levels[l][1]); err != nil {
			return Plane{}, 0, err
		}
		p = normalized(p)
		r.whole[l] = p
	}
	f := res / mul(pixel, r.factor(l))
	if f < 1 {
		f = 1
	}
	return p.Downsample(f), mul(f, r.factor(l)), nil
}

// normalized maps a signal's 1st to 99.5th percentiles onto [0, 1].
func normalized(p Plane) Plane {
	q := p.Percentiles(0.01, 0.995)
	lo, hi := q[0], q[1]
	if hi <= lo {
		hi = lo + 1
	}
	return p.Normalized(lo, hi)
}

// Register registers moving (the post-Xenium image) to fixed (the Xenium image).
func Register(ctx context.Context, fixed, moving Pyramid, opt Options) (*Result, error) {
	if opt.Progress == nil {
		opt.Progress = func(float64, string) {}
	}
	if opt.Log == nil {
		opt.Log = func(string, ...any) {}
	}
	if opt.Model != ModelSimilarity && opt.Model != ModelAffine {
		return nil, fmt.Errorf("no model %q: similarity or affine", opt.Model)
	}
	if !(opt.FixedPixel > 0) {
		return nil, errors.New("the Xenium image's pixel size is needed")
	}
	fr, mr := newReader(fixed), newReader(moving)
	ext := [2]float64{mul(float64(fr.levels[0][0]), opt.FixedPixel), mul(float64(fr.levels[0][1]), opt.FixedPixel)}
	res := &Result{}

	// 1. The search.
	coarse := math.Max(ext[0], ext[1]) / 256
	var T Affine
	if opt.Start != nil {
		T = *opt.Start
		res.Best = Candidate{Transform: T, PixelUm: T.Scale(), Mirrored: T.Reflected()}
	} else {
		opt.Progress(0, "Searching for the image's scale, turn and place")
		cands, err := search(ctx, fr, mr, opt, coarse)
		if err != nil {
			return nil, err
		}
		res.Candidates = cands[:min(5, len(cands))]
		res.Best = cands[0]
		T = cands[0].Transform
		opt.Log("The search: %s, %.4f µm a pixel, turned %.1f°: normalized mutual information %.3f over %.0f%% of the Xenium image.", map[bool]string{true: "mirrored", false: "as it is"}[res.Best.Mirrored], res.Best.PixelUm, res.Best.TurnDeg, res.Best.NMI, 100*res.Best.Overlap)
	}

	// 2. Refinement by mutual information, a level at a time.
	opt.Progress(0.3, "Refining by mutual information")
	finest := math.Max(math.Max(4*opt.FixedPixel, 2*T.Scale()), math.Max(ext[0], ext[1])/2048)
	for r := coarse / 2; ; r /= 2 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r = math.Max(r, finest)
		var err error
		var nmi float64
		if T, nmi, err = refine(fr, mr, opt, T, r); err != nil {
			return nil, err
		}
		res.NMI, res.Resolution = nmi, r
		opt.Log("Refined at %.2f µm a pixel: normalized mutual information %.3f.", r, nmi)
		if r <= finest {
			break
		}
	}

	// 3. Local matches.
	opt.Progress(0.6, "Matching patches")
	var err error
	if T, err = localMatches(ctx, fr, mr, opt, T, ext, res); err != nil {
		return nil, err
	}
	res.Transform = T

	// 4. A deformation.
	if opt.NonRigid {
		opt.Progress(0.9, "Fitting a deformation")
		if err := deform(opt, ext, res); err != nil {
			res.Warnings = append(res.Warnings, "No deformation was fitted: "+err.Error())
		}
	}
	opt.Progress(1, "Done")
	return res, nil
}

// signalOf is a pyramid's signal at a resolution, normalized, and its pixel's span.
func signalOf(r *reader, pixel, res float64) (Plane, float64, error) {
	return r.at(pixel, res)
}

// toWorking is the map from the image's full-resolution pixels to a working plane's (k
// full-resolution pixels to its pixel; mirrored, top to bottom about its height h).
func toWorking(k float64, mirrored bool, h int) Affine {
	if mirrored {
		return Affine{A: 1 / k, E: -1 / k, F: float64(h)}
	}
	return Affine{A: 1 / k, E: 1 / k}
}

// search tries every scale, both handednesses and both turns, best first.
func search(ctx context.Context, fr, mr *reader, opt Options, coarse float64) ([]Candidate, error) {
	F, _, err := signalOf(fr, opt.FixedPixel, coarse)
	if err != nil {
		return nil, err
	}
	fixedT := newTransformed(F)
	centre := opt.MovingPixel
	steps := 8 // a quarter octave each, two octaves each way
	if !(centre > 0) {
		centre, steps = 0.5, 12
	}
	var out []Candidate
	total := 2 * (2*steps + 1)
	done := 0
	for _, mirrored := range []bool{false, true} {
		for s := -steps; s <= steps; s++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			pixel := mul(centre, dmath.Exp(mul(float64(s)/4, math.Ln2)))
			M, k, err := signalOf(mr, pixel, coarse)
			if err != nil {
				return nil, err
			}
			// An image a scale makes over four times the Xenium image's size, or under 8 pixels,
			// is not of its section (and its canvas would be costly).
			if M.W < 8 || M.H < 8 || max(M.W, M.H) > 4*max(F.W, F.H) {
				done++
				continue
			}
			if mirrored {
				M = M.FlipY()
			}
			turn, scale, _ := fixedT.turnAndScale(M)
			// What is left of the scale beyond a fifth of an octave is another scale's to find.
			if scale > 1.148698354997035 || scale < 0.8705505632961241 {
				scale = 1
			}
			for _, t := range []float64{turn, turn + math.Pi} {
				// From the working plane's pixels to F's: shrink by scale and turn back by t about
				// M's centre, then the shift.
				sn, cs := dmath.SinCos(-t)
				g := 1 / scale
				R := Affine{A: mul(g, cs), B: -mul(g, sn), D: mul(g, sn), E: mul(g, cs)}
				// M turned into its own frame, large enough for all of it.
				corners := [][2]float64{{0, 0}, {float64(M.W), 0}, {0, float64(M.H)}, {float64(M.W), float64(M.H)}}
				x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
				for _, c := range corners {
					x, y := R.Apply(c[0]-float64(M.W)/2, c[1]-float64(M.H)/2)
					x0, y0, x1, y1 = math.Min(x0, x), math.Min(y0, y), math.Max(x1, x), math.Max(y1, y)
				}
				fw, fh := int(math.Ceil(x1-x0)), int(math.Ceil(y1-y0))
				R.C, R.F = mul(-R.A, float64(M.W)/2)-mul(R.B, float64(M.H)/2)-x0, mul(-R.D, float64(M.W)/2)-mul(R.E, float64(M.H)/2)-y0
				inv, _ := R.Invert()
				turned := M.Warp(inv, fw, fh)
				dx, dy, _ := fixedT.phaseCorrelate(turned)
				n := float64(nextPow2(max(F.W, F.H, fw, fh)))
				best := Candidate{NMI: -1}
				for _, wx := range []float64{-n, 0, n} {
					for _, wy := range []float64{-n, 0, n} {
						// The turned frame's rectangle on F's: too little of F under it, no use.
						ox := math.Min(float64(F.W), dx+wx+float64(fw)) - math.Max(0, dx+wx)
						oy := math.Min(float64(F.H), dy+wy+float64(fh)) - math.Max(0, dy+wy)
						if ox <= 0 || oy <= 0 || mul(ox, oy) < mul(0.3, float64(F.W*F.H)) {
							continue
						}
						// F(x) ≈ turned(x − d): a working plane's pixel u lies at R(u) + d of F.
						S := R
						S.C, S.F = S.C+dx+wx, S.F+dy+wy
						toF := toWorking(k, mirrored, M.H).Then(S)
						toM, ok := S.Invert()
						if !ok {
							continue
						}
						nmi, overlap := NMIUnder(F, M, toM, 32, 2)
						if overlap < 0.3 || nmi <= best.NMI {
							continue
						}
						T := toF.Then(Scaling(coarse))
						best = Candidate{Mirrored: mirrored, PixelUm: T.Scale(), TurnDeg: T.Rotation(), NMI: nmi, Overlap: overlap, Transform: T}
					}
				}
				if best.NMI > 0 {
					out = append(out, best)
				}
			}
			done++
			opt.Progress(mul(0.3, float64(done)/float64(total)), "Searching for the image's scale, turn and place")
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no scale or turn of the image overlaps the Xenium image: are they of the same section?")
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].NMI > out[j].NMI })
	return out, nil
}

// params are a perturbation of a transform: a shift (µm), a turn, the logarithm of a scale and of
// an aspect, and a shear, about a centre (µm).
func perturbation(p []float64, c [2]float64) Affine {
	sn, cs := dmath.SinCos(p[2])
	sx, sy := dmath.Exp(p[3]), dmath.Exp(p[3])
	var shear float64
	if len(p) > 4 {
		sx, sy = dmath.Exp(p[3]+p[4]/2), dmath.Exp(p[3]-p[4]/2)
		shear = p[5]
	}
	// L = R · diag(sx, sy) · [[1, shear], [0, 1]]
	l00, l01 := mul(cs, sx), mul(cs, mul(sx, shear))-mul(sn, sy)
	l10, l11 := mul(sn, sx), mul(sn, mul(sx, shear))+mul(cs, sy)
	return Affine{A: l00, B: l01, D: l10, E: l11,
		C: c[0] + p[0] - (mul(l00, c[0]) + mul(l01, c[1])),
		F: c[1] + p[1] - (mul(l10, c[0]) + mul(l11, c[1]))}
}

// refine moves the transform to maximize the normalized mutual information at res µm a pixel.
func refine(fr, mr *reader, opt Options, T Affine, res float64) (Affine, float64, error) {
	F, _, err := signalOf(fr, opt.FixedPixel, res)
	if err != nil {
		return T, 0, err
	}
	M, k, err := signalOf(mr, T.Scale(), res)
	if err != nil {
		return T, 0, err
	}
	extent := math.Max(float64(F.W), float64(F.H))
	c := [2]float64{mul(float64(F.W), res) / 2, mul(float64(F.H), res) / 2}
	np := 4
	if opt.Model == ModelAffine {
		np = 6
	}
	step := []float64{2 * res, 2 * res, 2 / extent, 2 / extent, 2 / extent, 2 / extent}[:np]
	least := []float64{res / 8, res / 8, 1 / (8 * extent), 1 / (8 * extent), 1 / (8 * extent), 1 / (8 * extent)}[:np]
	stride := max(1, int(math.Sqrt(float64(F.W*F.H)/250000)))
	bins := 32
	cost := func(p []float64) float64 {
		Tp := T.Then(perturbation(p, c))
		inv, ok := Tp.Invert()
		if !ok {
			return 0
		}
		// F's pixel → µm → the image's pixel → M's pixel.
		toM := Scaling(res).Then(inv).Then(Scaling(1 / k))
		nmi, overlap := NMIUnder(F, M, toM, bins, stride)
		if overlap < 0.2 {
			return 0
		}
		return -nmi
	}
	p, best, _ := patternSearch(cost, make([]float64, np), step, least, 600)
	return T.Then(perturbation(p, c)), -best, nil
}

// localMatches matches patches twice over, refitting the model to the matches kept each time.
func localMatches(ctx context.Context, fr, mr *reader, opt Options, T Affine, ext [2]float64, res *Result) (Affine, error) {
	// Patches about a sixth of the tissue's smaller side, 32 to 160 µm, 128 pixels a side.
	side := math.Min(160, math.Max(32, math.Min(ext[0], ext[1])/6))
	const px = 128
	fine := side / px
	res.PatchUm, res.Fine = side, fine
	// Where the tissue is: the Xenium image's signal at the patches' scale.
	coarse, _, err := signalOf(fr, opt.FixedPixel, side/4)
	if err != nil {
		return T, err
	}
	coarse = normalized(coarse)
	var centres [][2]float64
	spacing := side
	for {
		centres = centres[:0]
		for y := side / 2; y+side/2 <= ext[1]; y += spacing {
			for x := side / 2; x+side/2 <= ext[0]; x += spacing {
				if v, ok := coarse.Sample(x/(side/4), y/(side/4)); ok && v > 0.15 {
					centres = append(centres, [2]float64{x, y})
				}
			}
		}
		if len(centres) <= 200 {
			break
		}
		spacing *= 1.25
	}
	if len(centres) == 0 {
		res.Warnings = append(res.Warnings, "No patch of the Xenium image holds tissue: the alignment rests on mutual information alone.")
		return T, nil
	}
	fl := fr.levelFor(opt.FixedPixel, fine)
	for pass := 0; pass < 2; pass++ {
		if err := ctx.Err(); err != nil {
			return T, err
		}
		inv, _ := T.Invert()
		ml := mr.levelFor(T.Scale(), fine)
		mk := mr.factor(ml)
		matches := make([]Match, len(centres))
		ok := make([]bool, len(centres))
		errs := make([]error, len(centres))
		parallelFor(len(centres), func(i int) {
			c := centres[i]
			// The Xenium image's patch.
			fk := fr.factor(fl)
			fpix := mul(opt.FixedPixel, fk)
			x0, y0 := int(math.Floor((c[0]-side/2)/fpix))-1, int(math.Floor((c[1]-side/2)/fpix))-1
			w, h := int(math.Ceil(side/fpix))+3, int(math.Ceil(side/fpix))+3
			region, err := fr.p.Region(fl, x0, y0, w, h)
			if err != nil {
				errs[i] = err
				return
			}
			// Region pixel → patch pixel: µm = (x0 + u)·fpix; patch pixel = (µm − corner)/fine.
			corner := [2]float64{c[0] - side/2, c[1] - side/2}
			toRegion := Affine{A: fine / fpix, E: fine / fpix, C: (corner[0] - mul(float64(x0), fpix)) / fpix, F: (corner[1] - mul(float64(y0), fpix)) / fpix}
			fp := region.Warp(toRegion, px, px)
			// The image's patch, under the transform: patch pixel → µm → its pixel → its level's.
			toImage := Affine{A: fine, E: fine, C: corner[0], F: corner[1]}.Then(inv)
			qs := [][2]float64{}
			for _, u := range [][2]float64{{0, 0}, {px, 0}, {0, px}, {px, px}} {
				x, y := toImage.Apply(u[0], u[1])
				qs = append(qs, [2]float64{x / mk, y / mk})
			}
			qx0, qy0, qx1, qy1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
			for _, q := range qs {
				qx0, qy0, qx1, qy1 = math.Min(qx0, q[0]), math.Min(qy0, q[1]), math.Max(qx1, q[0]), math.Max(qy1, q[1])
			}
			mx0, my0 := int(math.Floor(qx0))-2, int(math.Floor(qy0))-2
			mregion, err := mr.p.Region(ml, mx0, my0, int(math.Ceil(qx1))-mx0+3, int(math.Ceil(qy1))-my0+3)
			if err != nil {
				errs[i] = err
				return
			}
			mp := mregion.Warp(toImage.Then(Affine{A: 1 / mk, E: 1 / mk, C: -float64(mx0), F: -float64(my0)}), px, px)
			covered := 0
			for _, m := range mp.Mask {
				if m {
					covered++
				}
			}
			if covered < px*px*9/10 {
				return
			}
			dx, dy, q := PhaseCorrelate(normalized(fp), normalized(mp))
			if q < 6 || math.Abs(dx) > px/4 || math.Abs(dy) > px/4 {
				return
			}
			// F(u) ≈ W(u − d): the Xenium image's centre is where the image's (centre − d) is drawn.
			qx, qy := toImage.Apply(float64(px)/2-dx, float64(px)/2-dy)
			matches[i] = Match{Fixed: Point{c[0], c[1]}, Moving: Point{qx, qy}, Quality: q}
			ok[i] = true
		})
		for _, err := range errs {
			if err != nil {
				return T, err
			}
		}
		var found []Match
		for i := range matches {
			if ok[i] {
				found = append(found, matches[i])
			}
		}
		res.Matches = found
		if len(found) < 6 {
			res.Warnings = append(res.Warnings, fmt.Sprintf("Only %d patches matched: the alignment rests on mutual information; check it.", len(found)))
			return T, nil
		}
		fit, kept, err := robustFit(opt.Model, found, fine)
		if err != nil {
			return T, err
		}
		for i := range res.Matches {
			res.Matches[i].Kept = kept[i]
		}
		res.Fit = fit
		T = fit.Transform
		n := 0
		for _, k := range kept {
			if k {
				n++
			}
		}
		opt.Log("Pass %d: %d of %d patches matched, %d kept; residuals %.3f µm root-mean-square, left out %.3f µm.", pass+1, len(found), len(centres), n, fit.RMS, fit.LooRMS)
	}
	return T, nil
}

// robustFit fits the model to the matches, dropping those whose residual is over three robust
// standard deviations of the others' (and over a working pixel), until none is.
func robustFit(model Model, ms []Match, fine float64) (*Fitted, []bool, error) {
	kept := make([]bool, len(ms))
	for i := range kept {
		kept[i] = true
	}
	for round := 0; ; round++ {
		var kps []Keypoint
		for i, m := range ms {
			if kept[i] {
				kps = append(kps, Keypoint{Fixed: m.Fixed, Moving: m.Moving})
			}
		}
		fit, err := FitKeypoints(model, kps)
		if err != nil {
			return nil, nil, err
		}
		res := Residuals(fit.Transform, keypointsOfMatches(ms))
		var r []float64
		for i, k := range kept {
			if k {
				r = append(r, res[i])
			}
		}
		sort.Float64s(r)
		sigma := mul(1.4826, r[len(r)/2])
		limit := math.Max(mul(3, sigma), fine)
		changed := false
		for i := range ms {
			k := res[i] <= limit
			if k != kept[i] {
				kept[i], changed = k, true
			}
		}
		count := 0
		for _, k := range kept {
			if k {
				count++
			}
		}
		if !changed || round >= 10 || count < 6 {
			return fit, kept, nil
		}
	}
}

func keypointsOfMatches(ms []Match) []Keypoint {
	out := make([]Keypoint, len(ms))
	for i, m := range ms {
		out[i] = Keypoint{Fixed: m.Fixed, Moving: m.Moving}
	}
	return out
}

// deform fits a deformation to what the transform leaves of the kept matches, and how well it
// predicts each fifth of them from the rest, against the model alone.
func deform(opt Options, ext [2]float64, res *Result) error {
	var p, d [][2]float64
	var kps []Keypoint
	for _, m := range res.Matches {
		if !m.Kept {
			continue
		}
		x, y := res.Transform.Apply(m.Moving.X, m.Moving.Y)
		p = append(p, [2]float64{x, y})
		d = append(d, [2]float64{m.Fixed.X - x, m.Fixed.Y - y})
		kps = append(kps, Keypoint{Fixed: m.Fixed, Moving: m.Moving})
	}
	// Fewer matches cross-validate too coarsely (folds of a few points) to tell a deformation from
	// the matches' noise, and leave it to extrapolate between them.
	if len(p) < 25 {
		return fmt.Errorf("%d matches kept; a deformation needs 25 or more", len(p))
	}
	// A grid of about eight cells across the tissue, no finer than two patches, at most 24 across.
	spacing := math.Max(math.Max(ext[0], ext[1])/8, 2*res.PatchUm)
	spacing = math.Max(spacing, math.Max(ext[0], ext[1])/24)
	// Each fifth of the matches predicted from the others: by the model alone, and by deformations
	// of a few smoothnesses after it; the smoothness that predicts best is kept, and the deformation
	// only when it predicts clearly better than the model alone (else it would follow the matches'
	// noise).
	heldOut := func(lambda float64) (float64, error) {
		var sq float64
		for fold := 0; fold < 5; fold++ {
			var tp, td [][2]float64
			for k := range p {
				if k%5 != fold {
					tp, td = append(tp, p[k]), append(td, d[k])
				}
			}
			bf := newBSpline(0, 0, ext[0], ext[1], spacing)
			if err := bf.fit(tp, td, lambda); err != nil {
				return 0, err
			}
			for k := fold; k < len(p); k += 5 {
				dx, dy := bf.At(p[k][0], p[k][1])
				ex, ey := d[k][0]-dx, d[k][1]-dy
				sq += mul(ex, ex) + mul(ey, ey)
			}
		}
		return math.Sqrt(sq / float64(len(p))), nil
	}
	var affSq float64
	for fold := 0; fold < 5; fold++ {
		var tk []Keypoint
		for k := range kps {
			if k%5 != fold {
				tk = append(tk, kps[k])
			}
		}
		af, err := Fit(opt.Model, tk)
		if err != nil {
			return err
		}
		for k := fold; k < len(kps); k += 5 {
			ax, ay := af.Apply(kps[k].Moving.X, kps[k].Moving.Y)
			ex, ey := ax-kps[k].Fixed.X, ay-kps[k].Fixed.Y
			affSq += mul(ex, ex) + mul(ey, ey)
		}
	}
	res.AffineHeld = math.Sqrt(affSq / float64(len(kps)))
	best, bestLambda := math.Inf(1), 0.0
	for _, lambda := range []float64{0.01, 0.1, 1, 10} {
		h, err := heldOut(lambda)
		if err != nil {
			return err
		}
		if h < best {
			best, bestLambda = h, lambda
		}
	}
	res.HeldOut = best
	if !(best < mul(0.9, res.AffineHeld)) {
		return fmt.Errorf("a deformation predicts the matches no better than the %s alone (each fifth %.3f µm off from the rest, against %.3f µm): the image is not deformed beyond what the matches can tell", opt.Model, best, res.AffineHeld)
	}
	b, used, err := FitBSpline(p, d, 0, 0, ext[0], ext[1], spacing, bestLambda)
	if err != nil {
		return err
	}
	res.Deformation, res.Smoothness = b, used
	var sq float64
	for k := range p {
		dx, dy := b.At(p[k][0], p[k][1])
		ex, ey := d[k][0]-dx, d[k][1]-dy
		sq += mul(ex, ex) + mul(ey, ey)
	}
	res.DeformedRMS = math.Sqrt(sq / float64(len(p)))
	opt.Log("A deformation of %.0f µm cells (at most %.2f µm over the Xenium image; smoothness %g): the matches %.3f µm off, each fifth predicted from the rest %.3f µm off (the %s alone: %.3f µm).", spacing, b.MaxDisplacement(0, 0, ext[0], ext[1]), used, res.DeformedRMS, res.HeldOut, opt.Model, res.AffineHeld)
	return nil
}
