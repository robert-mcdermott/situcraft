package imports

// Coordinate transformations, as OME-NGFF and SpatialData write them in an element's attributes:
// identity, scale, translation, affine and sequence, each from an input coordinate system to an
// output one, whose axes name the order of the numbers (x, y, or y, x; other axes such as c and z
// are ignored). Only the plane's two axes matter to a segmentation, so each becomes a 2-D affine
// map.

import (
	"errors"
	"fmt"
	"math"
)

// affine maps (x, y) to (a·x + b·y + c, d·x + e·y + f).
type affine struct{ a, b, c, d, e, f float64 }

func identity() affine { return affine{a: 1, e: 1} }

func (t affine) apply(x, y float64) (float64, float64) {
	return t.a*x + t.b*y + t.c, t.d*x + t.e*y + t.f
}

// then is t followed by u.
func (t affine) then(u affine) affine {
	return affine{
		a: u.a*t.a + u.b*t.d, b: u.a*t.b + u.b*t.e, c: u.a*t.c + u.b*t.f + u.c,
		d: u.d*t.a + u.e*t.d, e: u.d*t.b + u.e*t.e, f: u.d*t.c + u.e*t.f + u.f,
	}
}

func (t affine) invert() (affine, bool) {
	det := t.a*t.e - t.b*t.d
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return affine{}, false
	}
	return affine{
		a: t.e / det, b: -t.b / det, c: (t.b*t.f - t.e*t.c) / det,
		d: -t.d / det, e: t.a / det, f: (t.d*t.c - t.a*t.f) / det,
	}, true
}

func (t affine) scaled(s float64) affine {
	return affine{t.a * s, t.b * s, t.c * s, t.d * s, t.e * s, t.f * s}
}

// axesOf reads a coordinate system's axis names ({"name": …, "axes": [{"name": "x"}, …]}).
func axesOf(system any) []string {
	m, _ := system.(map[string]any)
	list, _ := m["axes"].([]any)
	var out []string
	for _, a := range list {
		switch v := a.(type) {
		case map[string]any:
			name, _ := v["name"].(string)
			out = append(out, name)
		case string:
			out = append(out, v)
		}
	}
	return out
}

func systemName(system any) string {
	switch v := system.(type) {
	case map[string]any:
		name, _ := v["name"].(string)
		return name
	case string:
		return v
	}
	return ""
}

// transformTo finds, among an element's transformations, the one to the coordinate system named,
// and reads it; axes are the element's own (when a transformation names no input).
func transformTo(list []any, system string, axes []string) (affine, error) {
	var names []string
	for _, item := range list {
		t, _ := item.(map[string]any)
		out := systemName(t["output"])
		if out != system {
			if out != "" {
				names = append(names, out)
			}
			continue
		}
		in := axesOf(t["input"])
		if len(in) == 0 {
			in = axes
		}
		return readTransform(t, in)
	}
	if len(names) == 0 {
		return affine{}, fmt.Errorf("it has no transformation to the coordinate system %q", system)
	}
	return affine{}, fmt.Errorf("it has no transformation to %q (only to %v)", system, names)
}

// readTransform reads one transformation whose numbers follow axes.
func readTransform(t map[string]any, axes []string) (affine, error) {
	xi, yi := -1, -1
	for i, a := range axes {
		switch a {
		case "x":
			xi = i
		case "y":
			yi = i
		}
	}
	numbers := func(key string) ([]float64, error) {
		list, _ := t[key].([]any)
		out := make([]float64, len(list))
		for i, v := range list {
			f, ok := v.(float64)
			if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, fmt.Errorf("%s holds %v", key, v)
			}
			out[i] = f
		}
		return out, nil
	}
	pick := func(v []float64, i int, fallback float64) (float64, error) {
		if i < 0 {
			return fallback, nil
		}
		if i >= len(v) {
			return 0, fmt.Errorf("%d numbers for axes %v", len(v), axes)
		}
		return v[i], nil
	}
	typ, _ := t["type"].(string)
	switch typ {
	case "identity":
		return identity(), nil
	case "scale":
		v, err := numbers("scale")
		if err != nil {
			return affine{}, err
		}
		sx, err := pick(v, xi, 1)
		if err != nil {
			return affine{}, err
		}
		sy, err := pick(v, yi, 1)
		if err != nil {
			return affine{}, err
		}
		return affine{a: sx, e: sy}, nil
	case "translation":
		v, err := numbers("translation")
		if err != nil {
			return affine{}, err
		}
		tx, err := pick(v, xi, 0)
		if err != nil {
			return affine{}, err
		}
		ty, err := pick(v, yi, 0)
		if err != nil {
			return affine{}, err
		}
		return affine{a: 1, c: tx, e: 1, f: ty}, nil
	case "affine":
		rows, _ := t["affine"].([]any)
		outAxes := axesOf(t["output"])
		if len(outAxes) == 0 {
			outAxes = axes
		}
		ox, oy := -1, -1
		for i, a := range outAxes {
			switch a {
			case "x":
				ox = i
			case "y":
				oy = i
			}
		}
		if ox < 0 || oy < 0 || xi < 0 || yi < 0 || len(rows) < len(outAxes) {
			return affine{}, errors.New("an affine transformation without x and y")
		}
		row := func(r int) ([]float64, error) {
			list, _ := rows[r].([]any)
			out := make([]float64, len(list))
			for i, v := range list {
				f, ok := v.(float64)
				if !ok {
					return nil, fmt.Errorf("affine holds %v", v)
				}
				out[i] = f
			}
			if len(out) != len(axes)+1 {
				return nil, fmt.Errorf("an affine row of %d numbers for %d axes", len(out), len(axes))
			}
			return out, nil
		}
		rx, err := row(ox)
		if err != nil {
			return affine{}, err
		}
		ry, err := row(oy)
		if err != nil {
			return affine{}, err
		}
		n := len(axes)
		return affine{a: rx[xi], b: rx[yi], c: rx[n], d: ry[xi], e: ry[yi], f: ry[n]}, nil
	case "sequence":
		list, _ := t["transformations"].([]any)
		out := identity()
		for _, item := range list {
			inner, _ := item.(map[string]any)
			in := axesOf(inner["input"])
			if len(in) == 0 {
				in = axes
			}
			step, err := readTransform(inner, in)
			if err != nil {
				return affine{}, err
			}
			out = out.then(step)
		}
		return out, nil
	default:
		return affine{}, fmt.Errorf("the transformation %q is not read", typ)
	}
}
