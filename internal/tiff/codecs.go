package tiff

// The codecs every TIFF reader needs, and the predictors (TIFF 6.0 §9 PackBits, §13 LZW, §14
// differencing predictor; deflate as the TIFF Technical Note for Adobe Deflate defines it, a zlib
// stream; the floating-point predictor of Adobe Photoshop TIFF Technical Note 3).

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
)

func decodeNone(ifd *IFD, compressed []byte, width, height int) ([]byte, error) {
	return compressed, nil
}

func decodeDeflate(ifd *IFD, compressed []byte, width, height int) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("deflate: %w", err)
	}
	want := ifd.BlockBytes(width, height)
	out, err := io.ReadAll(io.LimitReader(reader, int64(want)+1))
	if err != nil {
		return nil, fmt.Errorf("deflate: %w", err)
	}
	return out, nil
}

func decodePackBits(ifd *IFD, compressed []byte, width, height int) ([]byte, error) {
	want := ifd.BlockBytes(width, height)
	out := make([]byte, 0, want)
	for p := 0; p < len(compressed) && len(out) < want; {
		n := int(int8(compressed[p]))
		p++
		switch {
		case n >= 0:
			if p+n+1 > len(compressed) {
				return nil, errors.New("packbits: literal run past the end")
			}
			out = append(out, compressed[p:p+n+1]...)
			p += n + 1
		case n != -128:
			if p >= len(compressed) {
				return nil, errors.New("packbits: repeat run past the end")
			}
			for k := 0; k < 1-n; k++ {
				out = append(out, compressed[p])
			}
			p++
		}
	}
	return out, nil
}

// decodeLZW reads TIFF's LZW: codes most significant bit first, 9 to 12 bits wide, Clear 256 and
// EndOfInformation 257, the width growing one code early ("early change").
func decodeLZW(ifd *IFD, compressed []byte, width, height int) ([]byte, error) {
	const clear, eoi = 256, 257
	want := ifd.BlockBytes(width, height)
	out := make([]byte, 0, want)
	// The table: each code's last byte, its prefix code, its first byte and its length.
	var (
		suffix [4096]byte
		prefix [4096]int32
		first  [4096]byte
		length [4096]int32
	)
	for i := 0; i < 256; i++ {
		suffix[i], first[i], prefix[i], length[i] = byte(i), byte(i), -1, 1
	}
	next, codeWidth, previous := 258, 9, -1
	var acc uint32
	have := 0
	p := 0
	emit := func(code int) {
		n := int(length[code])
		start := len(out)
		for k := 0; k < n; k++ {
			out = append(out, 0)
		}
		for c, k := code, start+n-1; c >= 0; c, k = int(prefix[c]), k-1 {
			out[k] = suffix[c]
		}
	}
	for len(out) <= want {
		for have < codeWidth {
			if p >= len(compressed) {
				return out, nil // the stream may end without EndOfInformation
			}
			acc = acc<<8 | uint32(compressed[p])
			p++
			have += 8
		}
		code := int(acc>>(have-codeWidth)) & (1<<codeWidth - 1)
		have -= codeWidth
		switch {
		case code == clear:
			next, codeWidth, previous = 258, 9, -1
			continue
		case code == eoi:
			return out, nil
		case previous < 0:
			if code > 255 {
				return nil, errors.New("lzw: the first code after Clear is not a byte")
			}
			emit(code)
		case code < next:
			emit(code)
			if next < 4096 {
				suffix[next], prefix[next], first[next], length[next] = first[code], int32(previous), first[previous], length[previous]+1
				next++
			}
		case code == next && next < 4096:
			// The code being defined: the previous string and its own first byte.
			suffix[next], prefix[next], first[next], length[next] = first[previous], int32(previous), first[previous], length[previous]+1
			next++
			emit(code)
		default:
			return nil, fmt.Errorf("lzw: code %d before it is defined", code)
		}
		previous = code
		if next+1 >= 1<<codeWidth && codeWidth < 12 {
			codeWidth++
		}
	}
	return out, nil
}

// undoDifferences undoes horizontal differencing (predictor 2) on little-endian samples.
func undoDifferences(b []byte, width, height, samples, bytesPerSample int) {
	row := width * samples * bytesPerSample
	for y := 0; y < height; y++ {
		line := b[y*row : (y+1)*row]
		switch bytesPerSample {
		case 1:
			for i := samples; i < len(line); i++ {
				line[i] += line[i-samples]
			}
		case 2:
			for i := samples * 2; i+1 < len(line); i += 2 {
				v := uint16(line[i]) | uint16(line[i+1])<<8
				v += uint16(line[i-2*samples]) | uint16(line[i-2*samples+1])<<8
				line[i], line[i+1] = byte(v), byte(v>>8)
			}
		case 4:
			for i := samples * 4; i+3 < len(line); i += 4 {
				j := i - 4*samples
				v := uint32(line[i]) | uint32(line[i+1])<<8 | uint32(line[i+2])<<16 | uint32(line[i+3])<<24
				v += uint32(line[j]) | uint32(line[j+1])<<8 | uint32(line[j+2])<<16 | uint32(line[j+3])<<24
				line[i], line[i+1], line[i+2], line[i+3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
			}
		}
	}
}

// undoFloatPredictor undoes the floating-point predictor (predictor 3): each row's bytes were
// split into planes by significance (most significant first) and then differenced byte by byte
// with a stride of the samples per pixel. The values come back little-endian.
func undoFloatPredictor(b []byte, width, height, samples, bytesPerSample int) error {
	if bytesPerSample != 2 && bytesPerSample != 4 && bytesPerSample != 8 {
		return fmt.Errorf("floating-point predictor on %d-byte samples", bytesPerSample)
	}
	count := width * samples
	row := count * bytesPerSample
	planes := make([]byte, row)
	for y := 0; y < height; y++ {
		line := b[y*row : (y+1)*row]
		copy(planes, line)
		for i := samples; i < row; i++ {
			planes[i] += planes[i-samples]
		}
		for k := 0; k < count; k++ {
			for s := 0; s < bytesPerSample; s++ {
				line[k*bytesPerSample+bytesPerSample-1-s] = planes[s*count+k]
			}
		}
	}
	return nil
}
