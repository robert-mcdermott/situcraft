package register

// The alignment files Xenium Explorer writes for a post-Xenium image (10x Genomics, Xenium
// Explorer, "Image alignment"):
//   - the transformation matrix (<image>_imagealignment.csv): a 3×3 affine matrix, row-major, its
//     last row 0, 0, 1, mapping the image's pixels to the Xenium image's pixels (its scale is the
//     image's pixel size over the Xenium image's);
//   - the keypoints (<image>_keypoints.csv): columns fixedX, fixedY (the Xenium image's pixels) and
//     alignmentX, alignmentY (the image's), each row a landmark placed on both; Xenium Explorer fits
//     them by least squares as a similarity (translation, one scale, rotation and reflection; Horn
//     1987, in two dimensions). The same fit here: Umeyama (1991), with and without a reflection,
//     the better taken.

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Point is a position in pixels.
type Point struct{ X, Y float64 }

// Keypoint is one landmark on both images: Fixed on the Xenium image, Moving on the post-Xenium
// image, each in its pixels.
type Keypoint struct {
	Fixed  Point `json:"fixed"`
	Moving Point `json:"moving"`
}

// Alignment is a post-Xenium image's alignment as a file gave it.
type Alignment struct {
	// Kind is "matrix" or "keypoints".
	Kind string `json:"kind"`
	// Matrix maps the image's pixels to the Xenium image's pixels.
	Matrix    Affine     `json:"matrix"`
	Keypoints []Keypoint `json:"keypoints,omitempty"`
	// Residuals are each keypoint's distance from where the fit puts it, in the Xenium image's
	// pixels.
	Residuals []float64 `json:"residuals,omitempty"`
}

// ReadAlignment reads a transformation matrix or a keypoints file, by what it holds.
func ReadAlignment(data []byte) (*Alignment, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("the alignment is not CSV: %w", err)
	}
	var kept [][]string
	for _, row := range rows {
		if len(row) > 0 && !(len(row) == 1 && strings.TrimSpace(row[0]) == "") {
			kept = append(kept, row)
		}
	}
	if len(kept) == 0 {
		return nil, errors.New("the alignment file is empty")
	}
	if strings.EqualFold(strings.TrimSpace(kept[0][0]), "fixedX") {
		return readKeypoints(kept)
	}
	return readMatrix(kept)
}

func readMatrix(rows [][]string) (*Alignment, error) {
	if len(rows) != 3 {
		return nil, fmt.Errorf("a transformation matrix has 3 rows, not %d (a keypoints file starts with fixedX,fixedY,alignmentX,alignmentY)", len(rows))
	}
	var m [3][3]float64
	for i, row := range rows {
		if len(row) != 3 {
			return nil, fmt.Errorf("row %d of the transformation matrix has %d values, not 3", i+1, len(row))
		}
		for j, v := range row {
			x, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
				return nil, fmt.Errorf("row %d of the transformation matrix: %q is not a number", i+1, v)
			}
			m[i][j] = x
		}
	}
	if m[2][0] != 0 || m[2][1] != 0 || m[2][2] != 1 {
		return nil, fmt.Errorf("the matrix's last row is %v, not 0, 0, 1: it is not an affine transform", m[2])
	}
	t := Affine{m[0][0], m[0][1], m[0][2], m[1][0], m[1][1], m[1][2]}
	if !t.Valid() {
		return nil, errors.New("the transformation matrix cannot be inverted: it maps the image onto a line or a point")
	}
	return &Alignment{Kind: "matrix", Matrix: t}, nil
}

func readKeypoints(rows [][]string) (*Alignment, error) {
	col := map[string]int{}
	for i, name := range rows[0] {
		col[strings.ToLower(strings.TrimSpace(name))] = i
	}
	for _, name := range []string{"fixedx", "fixedy", "alignmentx", "alignmenty"} {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("the keypoints file has no %s column", name)
		}
	}
	var kps []Keypoint
	for i, row := range rows[1:] {
		get := func(name string) (float64, error) {
			j := col[name]
			if j >= len(row) {
				return 0, fmt.Errorf("keypoint %d has no %s", i+1, name)
			}
			v, err := strconv.ParseFloat(strings.TrimSpace(row[j]), 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return 0, fmt.Errorf("keypoint %d: %s %q is not a number", i+1, name, row[j])
			}
			return v, nil
		}
		var vals [4]float64
		for k, name := range []string{"fixedx", "fixedy", "alignmentx", "alignmenty"} {
			v, err := get(name)
			if err != nil {
				return nil, err
			}
			vals[k] = v
		}
		kps = append(kps, Keypoint{Fixed: Point{vals[0], vals[1]}, Moving: Point{vals[2], vals[3]}})
	}
	t, err := FitSimilarity(kps)
	if err != nil {
		return nil, err
	}
	return &Alignment{Kind: "keypoints", Matrix: t, Keypoints: kps, Residuals: Residuals(t, kps)}, nil
}

// FitSimilarity is the similarity (translation, one scale, rotation, and a reflection when that
// fits better) mapping the keypoints' moving positions to their fixed ones with the least sum of
// squared distances (Umeyama 1991). It needs three keypoints not on one line.
func FitSimilarity(kps []Keypoint) (Affine, error) {
	if len(kps) < MinKeypoints {
		return Affine{}, fmt.Errorf("%d landmarks: a similarity is fitted to three or more (a third tells whether it reflects)", len(kps))
	}
	proper, okP := fitProper(kps, false)
	mirrored, okM := fitProper(kps, true)
	if !okP && !okM {
		return Affine{}, errors.New("the keypoints are all at one place: no transform fits them")
	}
	if !okM || okP && sumSquares(proper, kps) <= sumSquares(mirrored, kps) {
		return proper, nil
	}
	return mirrored, nil
}

// fitProper fits a rotation, scale and translation; reflect mirrors the moving positions' y first
// (the fit then includes that reflection).
func fitProper(kps []Keypoint, reflect bool) (Affine, bool) {
	n := float64(len(kps))
	var mx, my, fx, fy float64
	for _, k := range kps {
		y := k.Moving.Y
		if reflect {
			y = -y
		}
		mx, my, fx, fy = mx+k.Moving.X, my+y, fx+k.Fixed.X, fy+k.Fixed.Y
	}
	mx, my, fx, fy = mx/n, my/n, fx/n, fy/n
	var sxx, sxy, ss float64
	for _, k := range kps {
		y := k.Moving.Y
		if reflect {
			y = -y
		}
		ax, ay := k.Moving.X-mx, y-my
		bx, by := k.Fixed.X-fx, k.Fixed.Y-fy
		sxx += mul(ax, bx) + mul(ay, by)
		sxy += mul(ax, by) - mul(ay, bx)
		ss += mul(ax, ax) + mul(ay, ay)
	}
	if ss == 0 {
		return Affine{}, false
	}
	norm := math.Sqrt(mul(sxx, sxx) + mul(sxy, sxy))
	if norm == 0 {
		return Affine{}, false
	}
	s := norm / ss
	c, si := mul(s, sxx/norm), mul(s, sxy/norm)
	t := Affine{A: c, B: -si, D: si, E: c}
	t.C = fx - (mul(t.A, mx) + mul(t.B, my))
	t.F = fy - (mul(t.D, mx) + mul(t.E, my))
	if reflect {
		// The fit was of (x, −y): fold the reflection into the transform.
		t.B, t.E = -t.B, -t.E
	}
	return t, true
}

func sumSquares(t Affine, kps []Keypoint) float64 {
	total := 0.0
	for _, r := range Residuals(t, kps) {
		total += mul(r, r)
	}
	return total
}

// Residuals are each keypoint's distance from where t puts its moving position, in the fixed
// positions' units.
func Residuals(t Affine, kps []Keypoint) []float64 {
	out := make([]float64, len(kps))
	for i, k := range kps {
		out[i] = distance(t, k)
	}
	return out
}

// MatrixCSV is a transformation matrix as Xenium Explorer writes it: the image's pixels to the
// Xenium image's, three rows of three, each number as short as reads back the same.
func MatrixCSV(m Affine) []byte {
	f := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	return []byte(f(m.A) + "," + f(m.B) + "," + f(m.C) + "\n" + f(m.D) + "," + f(m.E) + "," + f(m.F) + "\n0,0,1\n")
}

// KeypointsCSV is keypoints as Xenium Explorer writes them: each landmark's position on the Xenium
// image (fixedX, fixedY) and on the image (alignmentX, alignmentY), in their pixels.
func KeypointsCSV(kps []Keypoint) []byte {
	f := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	var b strings.Builder
	b.WriteString("fixedX,fixedY,alignmentX,alignmentY\n")
	for _, k := range kps {
		b.WriteString(f(k.Fixed.X) + "," + f(k.Fixed.Y) + "," + f(k.Moving.X) + "," + f(k.Moving.Y) + "\n")
	}
	return []byte(b.String())
}
