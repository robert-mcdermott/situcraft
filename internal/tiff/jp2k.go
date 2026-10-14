package tiff

// JPEG 2000 tiles: Xenium's morphology images (compression 34712) and the JPEG 2000 codes of
// whole-slide images (33003 and 33005, as Aperio registered them), decoded by internal/jp2k.

import (
	"encoding/binary"
	"fmt"

	"situcraft/internal/jp2k"
)

const (
	CompressionAperioJP2KYCbCr = 33003
	CompressionAperioJP2KRGB   = 33005
)

func init() {
	for _, code := range []int{CompressionJP2K, CompressionAperioJP2KYCbCr, CompressionAperioJP2KRGB} {
		Register(code, decodeJP2K)
	}
}

// decodeJP2K decodes one tile's JPEG 2000 codestream (or JP2 file) into little-endian samples,
// interleaved as the tile's samples per pixel.
func decodeJP2K(ifd *IFD, compressed []byte, width, height int) ([]byte, error) {
	bits := ifd.BitsPerSample[0]
	if bits != 8 && bits != 16 {
		return nil, fmt.Errorf("jpeg 2000: %d bits per sample is not read", bits)
	}
	samples := ifd.samples()
	if samples == 1 && bits == 16 {
		pix, w, h, err := jp2k.DecodeGray16(compressed, jp2k.Options{})
		if err != nil {
			return nil, err
		}
		if w != width || h != height {
			return nil, fmt.Errorf("jpeg 2000: the codestream is %d×%d, the tile %d×%d", w, h, width, height)
		}
		out := make([]byte, 2*len(pix))
		for i, v := range pix {
			binary.LittleEndian.PutUint16(out[2*i:], v)
		}
		return out, nil
	}
	img, err := jp2k.Decode(compressed, jp2k.Options{})
	if err != nil {
		return nil, err
	}
	if len(img.Components) != samples {
		return nil, fmt.Errorf("jpeg 2000: %d components for %d samples per pixel", len(img.Components), samples)
	}
	bytesPerSample := bits / 8
	out := make([]byte, width*height*samples*bytesPerSample)
	for c, comp := range img.Components {
		if comp.Width != width || comp.Height != height {
			return nil, fmt.Errorf("jpeg 2000: component %d is %d×%d, the tile %d×%d (subsampled components are not read)", c, comp.Width, comp.Height, width, height)
		}
		for i, v := range comp.Data {
			at := (i*samples + c) * bytesPerSample
			if bytesPerSample == 1 {
				out[at] = byte(v)
			} else {
				binary.LittleEndian.PutUint16(out[at:], uint16(v))
			}
		}
	}
	return out, nil
}
