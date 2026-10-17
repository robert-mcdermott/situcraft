package tiff

// JPEG tiles (TIFF Technical Note 2, compression 7): each tile or strip a baseline JPEG stream, the
// tables it shares with the others often kept in the JPEGTables tag and left out of the stream (an
// "abbreviated" stream); decoded by Go's image/jpeg. A stream of three components holds YCbCr when
// the image's photometric interpretation is YCbCr (6), converted to RGB with JFIF's equations (TIFF's
// defaults: full range, ITU-R BT.601), and RGB as it is when it says RGB (2): the stream itself does
// not say which. Chroma stored at half resolution is brought to full resolution by interpolation,
// the triangle filter (weights 3 and 1 between neighbouring samples, in each direction subsampled)
// that the IJG library and libjpeg-turbo apply by default, rather than repeated: the colours then
// agree with those of the readers most pathology software uses, to rounding.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
)

const (
	PhotometricRGB   = 2
	PhotometricYCbCr = 6
)

func init() { Register(CompressionJPEG, decodeJPEG) }

func decodeJPEG(ifd *IFD, compressed []byte, width, height int) ([]byte, error) {
	if bits := ifd.BitsPerSample[0]; bits != 8 {
		return nil, fmt.Errorf("jpeg: %d bits per sample is not read", bits)
	}
	img, err := jpeg.Decode(bytes.NewReader(withTables(compressed, ifd.JPEGTables)))
	if err != nil {
		return nil, fmt.Errorf("jpeg: %w", err)
	}
	if b := img.Bounds(); b.Dx() != width || b.Dy() != height {
		return nil, fmt.Errorf("jpeg: the stream is %d×%d, the tile %d×%d", b.Dx(), b.Dy(), width, height)
	}
	samples := ifd.samples()
	out := make([]byte, width*height*samples)
	switch m := img.(type) {
	case *image.Gray:
		if samples != 1 {
			return nil, fmt.Errorf("jpeg: a gray stream for %d samples per pixel", samples)
		}
		for y := 0; y < height; y++ {
			copy(out[y*width:(y+1)*width], m.Pix[y*m.Stride:y*m.Stride+width])
		}
	case *image.YCbCr:
		if samples != 3 {
			return nil, fmt.Errorf("jpeg: a three-component stream for %d samples per pixel", samples)
		}
		convert := ifd.Photometric != PhotometricRGB
		cb, cr, stride := upsampleChroma(m, width, height)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				a := m.Y[m.YOffset(x, y)]
				var b, c uint8
				if cb != nil {
					b, c = cb[y*stride+x], cr[y*stride+x]
				} else {
					ci := m.COffset(x, y)
					b, c = m.Cb[ci], m.Cr[ci]
				}
				if convert {
					a, b, c = color.YCbCrToRGB(a, b, c)
				}
				o := 3 * (y*width + x)
				out[o], out[o+1], out[o+2] = a, b, c
			}
		}
	case *image.RGBA:
		if samples != 3 {
			return nil, fmt.Errorf("jpeg: an RGB stream for %d samples per pixel", samples)
		}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				p := m.Pix[y*m.Stride+4*x:]
				o := 3 * (y*width + x)
				out[o], out[o+1], out[o+2] = p[0], p[1], p[2]
			}
		}
	default:
		return nil, fmt.Errorf("jpeg: %T streams are not read", img)
	}
	return out, nil
}

// withTables completes an abbreviated stream with the tables the image shares: JPEGTables is a
// stream of tables alone (SOI, DQT and DHT segments, EOI), so its body goes after the tile's SOI.
// A stream that carries its own tables is complete; a later table replaces an earlier one.
func withTables(stream, tables []byte) []byte {
	if len(tables) < 4 || len(stream) < 2 || tables[0] != 0xFF || tables[1] != 0xD8 || stream[0] != 0xFF || stream[1] != 0xD8 {
		return stream
	}
	body := tables[2:]
	if n := len(body); n >= 2 && body[n-2] == 0xFF && body[n-1] == 0xD9 {
		body = body[:n-2]
	}
	out := make([]byte, 0, len(stream)+len(body))
	out = append(out, 0xFF, 0xD8)
	out = append(out, body...)
	return append(out, stream[2:]...)
}

// upsampleChroma answers a YCbCr image's chroma planes at full resolution (width × height, row
// stride width) by the triangle filter: 4:2:0 subsampled in both directions, 4:2:2 across and 4:4:0
// down. Each full-resolution sample is ¾ of the chroma sample it lies in and ¼ of the next nearest
// (in both directions, 9:3:3:1 over 16); at the plane's edges the edge sample stands for its missing
// neighbour. Like the IJG library, it rounds with alternating offsets (8 and 7 over 16, 1 and 2 over
// 4), so that rounding has no drift. nil for 4:4:4 (and ratios it does not interpolate).
func upsampleChroma(m *image.YCbCr, width, height int) (cb, cr []uint8, stride int) {
	var across, down bool
	switch m.SubsampleRatio {
	case image.YCbCrSubsampleRatio420:
		across, down = true, true
	case image.YCbCrSubsampleRatio422:
		across = true
	case image.YCbCrSubsampleRatio440:
		down = true
	default:
		return nil, nil, 0
	}
	cw, ch := (width+1)/2, (height+1)/2
	if !across {
		cw = width
	}
	if !down {
		ch = height
	}
	plane := func(src []uint8) []uint8 {
		at := func(x, y int) int {
			x = min(max(x, 0), cw-1)
			y = min(max(y, 0), ch-1)
			return int(src[y*m.CStride+x])
		}
		out := make([]uint8, width*height)
		for y := 0; y < height; y++ {
			// The chroma row this row lies in, and the nearer of its neighbours above and below.
			cy, ny := y, y
			if down {
				cy = y / 2
				ny = cy - 1
				if y%2 == 1 {
					ny = cy + 1
				}
			}
			for x := 0; x < width; x++ {
				cx, nx := x, x
				if across {
					cx = x / 2
					nx = cx - 1
					if x%2 == 1 {
						nx = cx + 1
					}
				}
				var v int
				switch {
				case across && down:
					near, far := 3*at(cx, cy)+at(cx, ny), 3*at(nx, cy)+at(nx, ny)
					bias := 8
					if x%2 == 1 {
						bias = 7
					}
					v = (3*near + far + bias) >> 4
				case across:
					bias := 1
					if x%2 == 1 {
						bias = 2
					}
					v = (3*at(cx, cy) + at(nx, cy) + bias) >> 2
				default:
					bias := 1
					if y%2 == 1 {
						bias = 2
					}
					v = (3*at(cx, cy) + at(cx, ny) + bias) >> 2
				}
				out[y*width+x] = uint8(v)
			}
		}
		return out
	}
	return plane(m.Cb), plane(m.Cr), width
}
