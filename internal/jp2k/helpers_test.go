// Test helpers: tile extraction from (Big)TIFF files, for the Xenium sample images, and OpenJPEG's
// opj_decompress run as an independent reference decoder, through its command line.
// The TIFF reader here is deliberately minimal and test-only; internal/tiff is the real one.

package jp2k

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type tiffTile struct {
	label string // "IFD 0 tile 3", "IFD 0 SubIFD 2 tile 0"
	data  []byte
}

// tiffTiles returns every tile of every IFD of a TIFF or BigTIFF file, with the tiles of each
// IFD's SubIFDs (pyramid levels) after it.
func tiffTiles(path string) ([]tiffTile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 16 {
		return nil, fmt.Errorf("%s: too short for TIFF", path)
	}
	var bo binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil, fmt.Errorf("%s: not a TIFF file", path)
	}
	big := bo.Uint16(data[2:]) == 43
	var first uint64
	if big {
		first = bo.Uint64(data[8:])
	} else {
		first = uint64(bo.Uint32(data[4:]))
	}
	typeSize := map[uint16]uint64{1: 1, 3: 2, 4: 4, 13: 4, 16: 8, 18: 8}
	type ifd struct {
		offsets, counts, subs []uint64
		next                  uint64
	}
	readIFD := func(off uint64) (ifd, error) {
		var r ifd
		entrySize, countSize := uint64(12), uint64(2)
		if big {
			entrySize, countSize = 20, 8
		}
		if off+countSize > uint64(len(data)) {
			return r, fmt.Errorf("IFD offset %d out of range", off)
		}
		var n uint64
		if big {
			n = bo.Uint64(data[off:])
		} else {
			n = uint64(bo.Uint16(data[off:]))
		}
		base := off + countSize
		if base+n*entrySize+8 > uint64(len(data)) {
			return r, fmt.Errorf("IFD at %d out of range", off)
		}
		for i := range n {
			e := data[base+i*entrySize:]
			tag, typ := bo.Uint16(e), bo.Uint16(e[2:])
			var cnt uint64
			var val []byte
			if big {
				cnt, val = bo.Uint64(e[4:]), e[12:20]
			} else {
				cnt, val = uint64(bo.Uint32(e[4:])), e[8:12]
			}
			size := typeSize[typ]
			if size == 0 {
				continue
			}
			if size*cnt > uint64(len(val)) {
				var o uint64
				if big {
					o = bo.Uint64(val)
				} else {
					o = uint64(bo.Uint32(val))
				}
				if o+size*cnt > uint64(len(data)) {
					return r, fmt.Errorf("tag %d values out of range", tag)
				}
				val = data[o:]
			}
			vals := make([]uint64, cnt)
			for j := range vals {
				switch size {
				case 1:
					vals[j] = uint64(val[j])
				case 2:
					vals[j] = uint64(bo.Uint16(val[2*j:]))
				case 4:
					vals[j] = uint64(bo.Uint32(val[4*j:]))
				case 8:
					vals[j] = bo.Uint64(val[8*j:])
				}
			}
			switch tag {
			case 324:
				r.offsets = vals
			case 325:
				r.counts = vals
			case 330:
				r.subs = vals
			}
		}
		if big {
			r.next = bo.Uint64(data[base+n*entrySize:])
		} else {
			r.next = uint64(bo.Uint32(data[base+n*entrySize:]))
		}
		return r, nil
	}
	var tiles []tiffTile
	add := func(label string, r ifd) error {
		if len(r.offsets) != len(r.counts) {
			return fmt.Errorf("%s: %d tile offsets but %d byte counts", label, len(r.offsets), len(r.counts))
		}
		for i, o := range r.offsets {
			if o+r.counts[i] > uint64(len(data)) {
				return fmt.Errorf("%s: tile %d out of range", label, i)
			}
			tiles = append(tiles, tiffTile{fmt.Sprintf("%s tile %d", label, i), data[o : o+r.counts[i]]})
		}
		return nil
	}
	for k, off := 0, first; off != 0 && k < 10000; k++ {
		r, err := readIFD(off)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", path, err)
		}
		label := fmt.Sprintf("IFD %d", k)
		if err := add(label, r); err != nil {
			return nil, err
		}
		for j, so := range r.subs {
			sr, err := readIFD(so)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", path, err)
			}
			if err := add(fmt.Sprintf("%s SubIFD %d", label, j), sr); err != nil {
				return nil, err
			}
		}
		off = r.next
	}
	return tiles, nil
}

// refComp is one component as decoded by OpenJPEG.
type refComp struct {
	w, h, prec int
	signed     bool
	data       []int32
}

var opjDecompress, opjCompress string

func init() {
	opjDecompress, _ = exec.LookPath("opj_decompress")
	opjCompress, _ = exec.LookPath("opj_compress")
}

// opjDecode decodes data (a JP2 file or a codestream) with opj_decompress into PGX files, one per
// component, and reads them back. A JP2 file's codestream is decoded bare, because opj_decompress
// applies the JP2 colour specification (converting sYCC to RGB, say) and this package does not.
func opjDecode(t testing.TB, data []byte, reduce int) ([]refComp, error) {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "in.j2k")
	cs, err := codestream(data)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(in, cs, 0o644); err != nil {
		return nil, err
	}
	args := []string{"-quiet", "-i", in, "-o", filepath.Join(dir, "out.pgx")}
	if reduce > 0 {
		args = append(args, "-r", strconv.Itoa(reduce))
	}
	if out, err := exec.Command(opjDecompress, args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("opj_decompress: %v: %s", err, out)
	}
	var comps []refComp
	for c := 0; ; c++ {
		b, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("out_%d.pgx", c)))
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return nil, err
		}
		rc, err := parsePGX(b)
		if err != nil {
			return nil, err
		}
		comps = append(comps, rc)
	}
	if len(comps) == 0 {
		return nil, fmt.Errorf("opj_decompress wrote no PGX files")
	}
	return comps, nil
}

// parsePGX reads a PGX file as opj_decompress writes it: "PG ML + 16 w h\n" (ML: most significant
// byte first, LM: least significant first) then 1, 2 or 4 bytes per sample.
func parsePGX(b []byte) (refComp, error) {
	nl := bytes.IndexByte(b, '\n')
	if nl < 0 || !bytes.HasPrefix(b, []byte("PG")) {
		return refComp{}, fmt.Errorf("not a PGX file")
	}
	hdr := strings.ReplaceAll(string(b[2:nl]), "+", " + ")
	hdr = strings.ReplaceAll(hdr, "-", " - ")
	f := strings.Fields(hdr)
	if len(f) != 5 {
		return refComp{}, fmt.Errorf("PGX header %q", b[:nl])
	}
	var rc refComp
	bigEndian := f[0] == "ML"
	rc.signed = f[1] == "-"
	var err1, err2, err3 error
	rc.prec, err1 = strconv.Atoi(f[2])
	rc.w, err2 = strconv.Atoi(f[3])
	rc.h, err3 = strconv.Atoi(f[4])
	if err1 != nil || err2 != nil || err3 != nil {
		return refComp{}, fmt.Errorf("PGX header %q", b[:nl])
	}
	size := 1
	if rc.prec > 16 {
		size = 4
	} else if rc.prec > 8 {
		size = 2
	}
	body := b[nl+1:]
	if len(body) < rc.w*rc.h*size {
		return refComp{}, fmt.Errorf("PGX body has %d bytes, want %d", len(body), rc.w*rc.h*size)
	}
	rc.data = make([]int32, rc.w*rc.h)
	for i := range rc.data {
		var v uint32
		switch size {
		case 1:
			v = uint32(body[i])
		case 2:
			if bigEndian {
				v = uint32(binary.BigEndian.Uint16(body[2*i:]))
			} else {
				v = uint32(binary.LittleEndian.Uint16(body[2*i:]))
			}
		case 4:
			if bigEndian {
				v = binary.BigEndian.Uint32(body[4*i:])
			} else {
				v = binary.LittleEndian.Uint32(body[4*i:])
			}
		}
		x := int32(v)
		if rc.signed { // sign-extend from the sample size
			switch size {
			case 1:
				x = int32(int8(v))
			case 2:
				x = int32(int16(v))
			}
		}
		rc.data[i] = x
	}
	return rc, nil
}

// diffStats accumulates the comparison of decoded samples with reference samples.
type diffStats struct {
	samples, differing int64
	maxAbs             int64
	sumSq              float64
}

func (s *diffStats) add(got, want []int32) {
	for i := range got {
		d := int64(got[i]) - int64(want[i])
		if d != 0 {
			s.differing++
			s.sumSq += float64(d * d)
			s.maxAbs = max(s.maxAbs, d, -d)
		}
	}
	s.samples += int64(len(got))
}

func (s *diffStats) merge(o diffStats) {
	s.samples += o.samples
	s.differing += o.differing
	s.maxAbs = max(s.maxAbs, o.maxAbs)
	s.sumSq += o.sumSq
}

func (s diffStats) String() string {
	frac, rmse := 0.0, 0.0
	if s.samples > 0 {
		frac = float64(s.differing) / float64(s.samples)
		rmse = math.Sqrt(s.sumSq / float64(s.samples))
	}
	return fmt.Sprintf("%d samples, max |diff| %d, %d differing (%.3g%%), RMSE %.4g", s.samples, s.maxAbs, s.differing, 100*frac, rmse)
}

// compareRef compares a decoded image with OpenJPEG's components.
func compareRef(img *Image, ref []refComp) (diffStats, error) {
	var s diffStats
	if len(img.Components) != len(ref) {
		return s, fmt.Errorf("%d components, OpenJPEG has %d", len(img.Components), len(ref))
	}
	for c, rc := range ref {
		comp := img.Components[c]
		if comp.Width != rc.w || comp.Height != rc.h {
			return s, fmt.Errorf("component %d is %d×%d, OpenJPEG's is %d×%d", c, comp.Width, comp.Height, rc.w, rc.h)
		}
		if comp.Precision != rc.prec || comp.Signed != rc.signed {
			return s, fmt.Errorf("component %d has precision %d signed %v, OpenJPEG's %d %v", c, comp.Precision, comp.Signed, rc.prec, rc.signed)
		}
		s.add(comp.Data, rc.data)
	}
	return s, nil
}

// mustCS returns the codestream of a JP2 file or codestream known to be valid.
func mustCS(b []byte) []byte {
	c, err := codestream(b)
	if err != nil {
		panic(err)
	}
	return c
}

// synthImage is a synthetic test image: components of possibly different sizes (subsampling),
// with samples in each component's range.
type synthImage struct {
	w, h   int // reference grid size
	prec   int
	signed bool
	dx, dy []int // per component subsampling
	comps  [][]int32
}

// pattern names the content of a synthetic image.
var patterns = []string{"gradient", "noise", "edges", "checker", "blobs"}

// makeImage builds a deterministic synthetic image with ncomp components.
func makeImage(pattern string, w, h, ncomp, prec int, signed bool, dx, dy []int, seed uint64) *synthImage {
	im := &synthImage{w: w, h: h, prec: prec, signed: signed, dx: dx, dy: dy}
	rng := seed*0x9E3779B97F4A7C15 + 1
	next := func() uint64 { // splitmix64
		rng += 0x9E3779B97F4A7C15
		z := rng
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		return z ^ (z >> 31)
	}
	maxv := int64(1)<<prec - 1
	for c := range ncomp {
		cw, ch := ceilDiv(w, dx[c]), ceilDiv(h, dy[c])
		data := make([]int32, cw*ch)
		for y := range ch {
			for x := range cw {
				var v float64 // in [0, 1]
				switch pattern {
				case "gradient":
					v = (float64(x)/float64(max(cw-1, 1)) + float64(y)/float64(max(ch-1, 1)) + float64(c)*0.3) / 2.6
				case "noise":
					v = float64(next()>>11) / (1 << 53)
				case "edges":
					v = 0.15
					if (x+2*y+7*c)%37 < 18 {
						v = 0.85
					}
					if x > cw/2 {
						v = 1 - v
					}
				case "checker":
					if (x/5+y/3+c)%2 == 0 {
						v = 1
					}
				case "blobs":
					fx, fy := float64(x)/float64(cw), float64(y)/float64(ch)
					v = 0.5 + 0.25*math.Sin(fx*9+float64(c))*math.Cos(fy*7) + 0.2*float64(next()>>11)/(1<<53) - 0.1
				}
				v = min(max(v, 0), 1)
				s := int64(math.Round(v * float64(maxv)))
				if signed {
					s -= 1 << (prec - 1)
				}
				data[y*cw+x] = int32(s)
			}
		}
		im.comps = append(im.comps, data)
	}
	return im
}

// writeInput writes the image in a format opj_compress reads and returns the path and the extra
// arguments it needs: PGM/PPM (maxval 2^prec-1, which sets the precision) for unsigned images of
// one or three components without subsampling, PGX for one component of more than 16 bits,
// otherwise raw (-F).
func (im *synthImage) writeInput(dir string) (string, []string, error) {
	sub := false
	for c := range im.comps {
		sub = sub || im.dx[c] != 1 || im.dy[c] != 1
	}
	n := len(im.comps)
	bytesPer := 1
	if im.prec > 8 {
		bytesPer = 2
	}
	if !sub && !im.signed && (n == 1 || n == 3) && im.prec <= 16 {
		var buf bytes.Buffer
		magic := "P5"
		if n == 3 {
			magic = "P6"
		}
		fmt.Fprintf(&buf, "%s\n%d %d\n%d\n", magic, im.w, im.h, 1<<im.prec-1)
		for i := range im.w * im.h {
			for c := range n {
				v := im.comps[c][i]
				if bytesPer == 2 {
					buf.WriteByte(byte(v >> 8))
				}
				buf.WriteByte(byte(v))
			}
		}
		path := filepath.Join(dir, "in.pgm")
		if n == 3 {
			path = filepath.Join(dir, "in.ppm")
		}
		return path, nil, os.WriteFile(path, buf.Bytes(), 0o644)
	}
	if n == 1 && !sub && im.prec > 16 {
		// PGX (one component, most significant byte first), the only input past 16 bits.
		sign := "+"
		if im.signed {
			sign = "-"
		}
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "PG ML %s %d %d %d\n", sign, im.prec, im.w, im.h)
		for _, v := range im.comps[0] {
			buf.Write(binary.BigEndian.AppendUint32(nil, uint32(v)))
		}
		path := filepath.Join(dir, "in.pgx")
		return path, nil, os.WriteFile(path, buf.Bytes(), 0o644)
	}
	// Raw: planar components, big-endian 16-bit samples (.raw; .rawl is little-endian).
	var buf bytes.Buffer
	for c := range n {
		for _, v := range im.comps[c] {
			if bytesPer == 2 {
				buf.WriteByte(byte(v >> 8))
			}
			buf.WriteByte(byte(v))
		}
	}
	sign := "u"
	if im.signed {
		sign = "s"
	}
	f := fmt.Sprintf("%d,%d,%d,%d,%s", im.w, im.h, n, im.prec, sign)
	if sub {
		var parts []string
		for c := range n {
			parts = append(parts, fmt.Sprintf("%dx%d", im.dx[c], im.dy[c]))
		}
		f += "@" + strings.Join(parts, ":")
	}
	path := filepath.Join(dir, "in.raw")
	return path, []string{"-F", f}, os.WriteFile(path, buf.Bytes(), 0o644)
}

// opjEncode compresses the image with opj_compress and the given arguments, into a .j2k
// codestream or, when jp2 is set, a .jp2 file.
func opjEncode(t testing.TB, im *synthImage, args []string, jp2 bool) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	in, extra, err := im.writeInput(dir)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "out.j2k")
	if jp2 {
		out = filepath.Join(dir, "out.jp2")
	}
	all := append([]string{"-i", in, "-o", out}, extra...)
	all = append(all, args...)
	if msg, err := exec.Command(opjCompress, all...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("opj_compress %s: %v: %s", strings.Join(args, " "), err, msg)
	}
	return os.ReadFile(out)
}
