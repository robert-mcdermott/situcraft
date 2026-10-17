package register

// Fitting a transform to landmarks: each a point placed on both images, its moving position (the
// post-Xenium image's pixels) and its fixed one (the Xenium image's, or µm). Two models:
//   - a similarity: translation, one scale and a rotation, with a reflection when that fits better
//     (Umeyama 1991, in closed form; Xenium Explorer fits its keypoints so);
//   - an affine transform: a scale along each axis and a shear as well (least squares, from the
//     normal equations of the centred positions).
// How well a fit fits is said two ways: each landmark's residual (its distance from where the fit
// puts it, the landmark among those fitted) and its leave-one-out error (its distance from where the
// fit of the others puts it). Residuals understate the error at a place no landmark marks; the
// leave-one-out error estimates it, as cross-validation does (Stone 1974).
//
// Every operation is a product or sum rounded on its own, in a fixed order, and square roots are
// IEEE 754's: web/lib/transforms.js computes the same fits bit for bit (fitFingerprint).

import (
	"errors"
	"fmt"
	"math"
)

// Model is what a fit may change.
type Model string

const (
	ModelSimilarity Model = "similarity"
	ModelAffine     Model = "affine"
)

// MinKeypoints is the fewest landmarks a model is fitted to: three for either (a similarity's
// reflection is told only by a third; an affine transform has six unknowns).
const MinKeypoints = 3

// Fitted is a fit and how well it fits, in the fixed positions' units.
type Fitted struct {
	Model     Model     `json:"model"`
	Transform Affine    `json:"transform"`
	Residuals []float64 `json:"residuals"`
	// LeaveOneOut are each landmark's leave-one-out errors; empty when too few landmarks remain
	// without one to fit the model (a similarity needs two others, an affine transform three not on
	// a line).
	LeaveOneOut []float64 `json:"leaveOneOut,omitempty"`
	RMS         float64   `json:"rms"`
	Max         float64   `json:"max"`
	LooRMS      float64   `json:"looRms,omitempty"`
	LooMax      float64   `json:"looMax,omitempty"`
}

// ParseModel reads a model's name ("" is a similarity).
func ParseModel(s string) (Model, error) {
	switch Model(s) {
	case "", ModelSimilarity:
		return ModelSimilarity, nil
	case ModelAffine:
		return ModelAffine, nil
	}
	return "", fmt.Errorf("no model %q: similarity or affine", s)
}

// Fit fits the model to the keypoints, moving positions to fixed ones, by least squares.
func Fit(model Model, kps []Keypoint) (Affine, error) {
	switch model {
	case ModelSimilarity:
		return FitSimilarity(kps)
	case ModelAffine:
		return FitAffine(kps)
	}
	return Affine{}, fmt.Errorf("no model %q", model)
}

// FitKeypoints fits the model and says how well it fits.
func FitKeypoints(model Model, kps []Keypoint) (*Fitted, error) {
	t, err := Fit(model, kps)
	if err != nil {
		return nil, err
	}
	f := &Fitted{Model: model, Transform: t, Residuals: Residuals(t, kps)}
	f.RMS, f.Max = rmsMax(f.Residuals)
	if loo := LeaveOneOut(model, kps, t); loo != nil {
		f.LeaveOneOut = loo
		f.LooRMS, f.LooMax = rmsMax(loo)
	}
	return f, nil
}

func rmsMax(v []float64) (float64, float64) {
	sum, worst := 0.0, 0.0
	for _, x := range v {
		sum += mul(x, x)
		worst = math.Max(worst, x)
	}
	return math.Sqrt(sum / float64(len(v))), worst
}

// FitAffine is the affine transform mapping the keypoints' moving positions to their fixed ones
// with the least sum of squared distances. It needs three keypoints not on one line.
func FitAffine(kps []Keypoint) (Affine, error) {
	if len(kps) < MinKeypoints {
		return Affine{}, fmt.Errorf("%d landmarks: an affine transform is fitted to three or more", len(kps))
	}
	t, ok := fitAffine(kps)
	if !ok {
		return Affine{}, errors.New("the landmarks lie on one line: an affine transform needs three not on a line")
	}
	return t, nil
}

func fitAffine(kps []Keypoint) (Affine, bool) {
	n := float64(len(kps))
	var mx, my, fx, fy float64
	for _, k := range kps {
		mx, my, fx, fy = mx+k.Moving.X, my+k.Moving.Y, fx+k.Fixed.X, fy+k.Fixed.Y
	}
	mx, my, fx, fy = mx/n, my/n, fx/n, fy/n
	// The normal equations of the centred positions: [sxx sxy; sxy syy] times each output's two
	// coefficients is its products with the inputs.
	var sxx, sxy, syy, bx, by, cx, cy float64
	for _, k := range kps {
		ax, ay := k.Moving.X-mx, k.Moving.Y-my
		ux, uy := k.Fixed.X-fx, k.Fixed.Y-fy
		sxx += mul(ax, ax)
		sxy += mul(ax, ay)
		syy += mul(ay, ay)
		bx += mul(ax, ux)
		by += mul(ay, ux)
		cx += mul(ax, uy)
		cy += mul(ay, uy)
	}
	det := mul(sxx, syy) - mul(sxy, sxy)
	// On a line, or so nearly that the fit is the rounding's.
	if !(det > mul(1e-12, mul(sxx+syy, sxx+syy))) {
		return Affine{}, false
	}
	t := Affine{
		A: (mul(bx, syy) - mul(by, sxy)) / det,
		B: (mul(by, sxx) - mul(bx, sxy)) / det,
		D: (mul(cx, syy) - mul(cy, sxy)) / det,
		E: (mul(cy, sxx) - mul(cx, sxy)) / det,
	}
	t.C = fx - (mul(t.A, mx) + mul(t.B, my))
	t.F = fy - (mul(t.D, mx) + mul(t.E, my))
	return t, true
}

// LeaveOneOut is each keypoint's distance from where the model fitted to the others puts it; a
// similarity keeps the full fit's handedness (whether it reflects). Nil when too few keypoints
// remain without one to fit the model.
func LeaveOneOut(model Model, kps []Keypoint, full Affine) []float64 {
	need := 2
	if model == ModelAffine {
		need = 3
	}
	if len(kps)-1 < need {
		return nil
	}
	out := make([]float64, len(kps))
	others := make([]Keypoint, 0, len(kps)-1)
	for i, k := range kps {
		others = append(others[:0], kps[:i]...)
		others = append(others, kps[i+1:]...)
		var t Affine
		var ok bool
		if model == ModelAffine {
			t, ok = fitAffine(others)
		} else {
			t, ok = fitProper(others, full.Reflected())
		}
		if !ok {
			return nil
		}
		out[i] = distance(t, k)
	}
	return out
}

// distance is how far t puts a keypoint's moving position from its fixed one.
func distance(t Affine, k Keypoint) float64 {
	x, y := t.Apply(k.Moving.X, k.Moving.Y)
	dx, dy := x-k.Fixed.X, y-k.Fixed.Y
	return math.Sqrt(mul(dx, dx) + mul(dy, dy))
}
