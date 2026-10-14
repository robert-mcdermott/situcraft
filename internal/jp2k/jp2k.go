// Package jp2k decodes JPEG 2000 images, JP2 files and raw codestreams, as specified by
// ITU-T Rec. T.800 | ISO/IEC 15444-1 (JPEG 2000 image coding system: core coding system, "Part 1").
// It is SituCraft's own decoder, written from the standard, for the JPEG 2000 tiles of Xenium
// morphology images (TIFF compression 34712) and for H&E images. Annex references in comments are
// to T.800. Features of later parts (Part 2 extensions, Part 15 high-throughput coding) are
// refused with an error wrapping ErrUnsupported and naming the feature.
//
// Decoding is strict: a malformed or truncated codestream is an error, with one exception that
// the standard's scalability allows and OpenJPEG's encoder relies on: a tile whose data ends at a
// packet boundary decodes with its remaining packets empty. Images are limited to 2^28 samples.
package jp2k

import (
	"errors"
	"fmt"
	"runtime"
)

// Component is one decoded image component.
type Component struct {
	Width, Height int // in samples, after subsampling
	Precision     int // bits
	Signed        bool
	// Width*Height samples, row-major, after the inverse component transform, the DC level
	// shift and clamping to the component's range.
	Data []int32
}

// Image is a decoded image.
type Image struct {
	// Reference grid size of the image area (Xsiz-XOsiz, Ysiz-YOsiz); with Options.Reduce, that
	// of the reduced resolution (ceil(Xsiz/2^Reduce) - ceil(XOsiz/2^Reduce), likewise in y).
	Width, Height int
	Components    []Component
}

// Options controls decoding.
type Options struct {
	// Reduce discards the Reduce highest resolution levels (decodes at 1/2^Reduce size); 0 decodes
	// in full.
	Reduce int
	// Workers bounds parallelism (code blocks are decoded concurrently); 0 means
	// runtime.GOMAXPROCS(0). Results do not depend on it.
	Workers int

	// opjHighpass makes the inverse 9/7 transform use OpenJPEG's approximate high-pass scaling
	// (see invK97OPJ), for the tests' bit-exact comparisons. Unexported: decoding follows the
	// standard.
	opjHighpass bool
}

// ErrUnsupported is wrapped by errors for valid JPEG 2000 features this decoder does not
// implement (those of Part 2 and Part 15).
var ErrUnsupported = errors.New("unsupported JPEG 2000 feature")

// maxSamples bounds the total number of decoded samples of an image (all components), so that a
// small hostile codestream cannot make the decoder allocate without limit. 2^28 samples is
// 1 GiB of int32 output; the fuzz test lowers it.
var maxSamples = 1 << 28

// Decode decodes a JP2 file or a raw J2K codestream. JP2 colour boxes (palette, component
// mapping, channel definitions) are not applied: the components are the codestream's.
func Decode(data []byte, opts Options) (*Image, error) {
	d, err := newDecoder(data, opts)
	if err != nil {
		return nil, err
	}
	img := &Image{Width: d.imgW, Height: d.imgH, Components: make([]Component, len(d.siz.comps))}
	total := 0
	for c, sc := range d.siz.comps {
		w, h := d.compSize(c)
		if err := checkSamples(w, h, &total); err != nil {
			return nil, err
		}
		img.Components[c] = Component{Width: w, Height: h, Precision: sc.prec, Signed: sc.signed}
	}
	for c := range img.Components {
		comp := &img.Components[c]
		comp.Data = make([]int32, comp.Width*comp.Height)
	}
	d.sink = func(t *tile) error { return storeInt32(d, t, img) }
	if err := d.decodeTiles(); err != nil {
		return nil, err
	}
	return img, nil
}

// DecodeGray16 is the fast path for one unsigned component of up to 16 bits (the Xenium profile):
// it returns the samples as uint16 without the int32 intermediate.
func DecodeGray16(data []byte, opts Options) (pix []uint16, width, height int, err error) {
	d, err := newDecoder(data, opts)
	if err != nil {
		return nil, 0, 0, err
	}
	if len(d.siz.comps) != 1 {
		return nil, 0, 0, fmt.Errorf("jp2k: DecodeGray16 needs one component, the image has %d", len(d.siz.comps))
	}
	sc := d.siz.comps[0]
	if sc.signed || sc.prec > 16 {
		kind := "unsigned"
		if sc.signed {
			kind = "signed"
		}
		return nil, 0, 0, fmt.Errorf("jp2k: DecodeGray16 needs an unsigned component of at most 16 bits, the image has %d-bit %s samples", sc.prec, kind)
	}
	width, height = d.compSize(0)
	if err := checkSamples(width, height, new(int)); err != nil {
		return nil, 0, 0, err
	}
	pix = make([]uint16, width*height)
	d.sink = func(t *tile) error { return storeGray16(d, t, pix, width, height) }
	if err := d.decodeTiles(); err != nil {
		return nil, 0, 0, err
	}
	return pix, width, height, nil
}

// checkSamples adds a component of w×h samples to *total and checks the total against maxSamples,
// without overflowing.
func checkSamples(w, h int, total *int) error {
	if w < 0 || h < 0 || h > 0 && w > (maxSamples-*total)/h {
		return fmt.Errorf("jp2k: image of %d×%d samples exceeds the limit of %d samples", w, h, maxSamples)
	}
	*total += w * h
	return nil
}

func (o Options) workers() int {
	if o.Workers > 0 {
		return o.Workers
	}
	return runtime.GOMAXPROCS(0)
}

// newDecoder finds the codestream, parses its headers and checks the options against it.
func newDecoder(data []byte, opts Options) (*decoder, error) {
	cs, err := codestream(data)
	if err != nil {
		return nil, err
	}
	if opts.Reduce < 0 {
		return nil, fmt.Errorf("jp2k: Reduce %d is negative", opts.Reduce)
	}
	d := &decoder{data: cs, opts: opts}
	if err := d.parse(); err != nil {
		return nil, err
	}
	return d, nil
}
