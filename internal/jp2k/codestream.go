// Codestream syntax (T.800 Annex A): the main header, tile-part headers and the marker segments
// that set coding parameters. Parameters are kept as parsed; tile.go resolves which apply to a
// tile-component (A.6: tile COC > tile COD > main COC > main COD, and likewise for QCC/QCD).

package jp2k

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
)

// Marker codes (Table A.2).
const (
	mSOC = 0xFF4F
	mCAP = 0xFF50
	mSIZ = 0xFF51
	mCOD = 0xFF52
	mCOC = 0xFF53
	mTLM = 0xFF55
	mPRF = 0xFF56
	mPLM = 0xFF57
	mPLT = 0xFF58
	mCPF = 0xFF59
	mQCD = 0xFF5C
	mQCC = 0xFF5D
	mRGN = 0xFF5E
	mPOC = 0xFF5F
	mPPM = 0xFF60
	mPPT = 0xFF61
	mCRG = 0xFF63
	mCOM = 0xFF64
	mSOT = 0xFF90
	mSOP = 0xFF91
	mEPH = 0xFF92
	mSOD = 0xFF93
	mEOC = 0xFFD9
)

// Marker segments defined by Part 2 (ISO/IEC 15444-2), refused by name.
var part2Markers = map[int]string{
	0xFF5A: "precinct quantization (QPD marker)",
	0xFF5B: "precinct quantization (QPC marker)",
	0xFF70: "variable DC offset (DCO marker)",
	0xFF71: "visual masking (VMS marker)",
	0xFF72: "downsampling factor styles (DFS marker)",
	0xFF73: "arbitrary decomposition styles (ADS marker)",
	0xFF74: "multiple component transformation (MCT marker)",
	0xFF75: "multiple component transformation collection (MCC marker)",
	0xFF76: "non-linearity point transformation (NLT marker)",
	0xFF77: "multiple component transformation ordering (MCO marker)",
	0xFF78: "component bit depth (CBD marker)",
	0xFF79: "arbitrary transformation kernels (ATK marker)",
}

// Limits beyond which a codestream is refused rather than decoded.
const (
	maxComponents = 16384 // Csiz (A.5.1)
	maxTiles      = 65535 // Isot is 16 bits (A.4.2)
	maxLevels     = 32    // NL (A.6.1)
	// The decoder keeps sample values and code-block magnitudes in 32-bit integers.
	maxPrecision = 31
	// Tiles times components, each of which costs setting up; real images have a few hundred.
	maxTileComps = 1 << 22
)

type sizComp struct {
	prec   int
	signed bool
	dx, dy int // XRsiz, YRsiz
}

// siz holds the image and tile size marker segment (A.5.1).
type siz struct {
	rsiz           int
	x1, y1, x0, y0 int // Xsiz, Ysiz, XOsiz, YOsiz
	tw, th, tx, ty int // XTsiz, YTsiz, XTOsiz, YTOsiz
	comps          []sizComp
}

// compParams are the coding parameters that COC can set per component (SPcod/SPcoc, A.6.1).
type compParams struct {
	levels     int   // NL
	cbw, cbh   int   // code-block width and height exponents (xcb, ycb)
	style      uint8 // code-block style (Table A.19)
	reversible bool  // 5/3 reversible filter; otherwise 9/7 irreversible
	ppx, ppy   []uint8
}

// codParams is a COD marker segment (A.6.1).
type codParams struct {
	scod   uint8
	prog   int
	layers int
	mct    int
	comp   compParams
}

// quantParams is a QCD or QCC marker segment (A.6.4, A.6.5).
type quantParams struct {
	style int // 0 no quantization, 1 scalar derived, 2 scalar expounded
	guard int
	exp   []int
	mant  []int
}

type pocEntry struct {
	rs, cs, le, re, ce, prog int
}

// header collects the parameter marker segments of the main header or of one tile. The
// per-component ones are maps, made when first set: most headers set none, and a slice per tile
// would cost tiles × components.
type header struct {
	cod *codParams
	coc map[int]*compParams
	qcd *quantParams
	qcc map[int]*quantParams
	rgn map[int]int // ROI shift
	poc []pocEntry
}

// tileParts gathers what the tile-parts of one tile contribute.
type tileParts struct {
	hdr    header
	parts  int
	bodies [][]byte
	ppt    []byte // packed packet headers (PPT), in order
	ppm    []byte // this tile's share of the PPM packed packet headers, in order
}

// decoder holds a parsed codestream and the state of one decode.
type decoder struct {
	data       []byte
	opts       Options
	siz        siz
	imgW, imgH int
	ntx, nty   int
	main       header
	usePPM     bool
	tiles      []*tileParts
	sink       func(*tile) error
}

func u16(b []byte) int { return int(binary.BigEndian.Uint16(b)) }
func u32(b []byte) int { return int(binary.BigEndian.Uint32(b)) }

// segment reads the marker segment at pos: its marker, its parameters (after the length field) and
// the position after it.
func (d *decoder) segment(pos int) (marker int, params []byte, next int, err error) {
	data := d.data
	if pos+2 > len(data) {
		return 0, nil, 0, fmt.Errorf("jp2k: codestream ends at offset %d inside a header", pos)
	}
	marker = u16(data[pos:])
	if marker>>8 != 0xFF {
		return 0, nil, 0, fmt.Errorf("jp2k: expected a marker at offset %d, found 0x%04X", pos, marker)
	}
	if marker >= 0xFF30 && marker <= 0xFF3F { // reserved markers without parameters (A.1.3)
		return marker, nil, pos + 2, nil
	}
	if pos+4 > len(data) {
		return 0, nil, 0, fmt.Errorf("jp2k: marker 0x%04X at offset %d is truncated", marker, pos)
	}
	l := u16(data[pos+2:])
	if l < 2 || pos+2+l > len(data) {
		return 0, nil, 0, fmt.Errorf("jp2k: marker segment 0x%04X at offset %d declares %d bytes, %d remain", marker, pos, l, len(data)-pos-2)
	}
	return marker, data[pos+4 : pos+2+l], pos + 2 + l, nil
}

// parse reads the main header and every tile-part header, and gathers each tile's data.
func (d *decoder) parse() error {
	data := d.data
	if len(data) < 2 || u16(data) != mSOC {
		return fmt.Errorf("jp2k: codestream does not start with the SOC marker")
	}
	m, p, pos, err := d.segment(2)
	if err != nil {
		return err
	}
	if m != mSIZ {
		return fmt.Errorf("jp2k: SIZ marker must follow SOC, found 0x%04X", m)
	}
	if err := d.parseSIZ(p); err != nil {
		return err
	}
	type ppmSeg struct {
		z    int
		data []byte
	}
	var ppms []ppmSeg
	for {
		if pos+2 <= len(data) && (u16(data[pos:]) == mSOT || u16(data[pos:]) == mEOC) {
			break
		}
		m, p, next, err := d.segment(pos)
		if err != nil {
			return err
		}
		switch m {
		case mPPM:
			if len(p) < 1 {
				return fmt.Errorf("jp2k: PPM marker segment is empty")
			}
			ppms = append(ppms, ppmSeg{int(p[0]), p[1:]})
		case mTLM, mPLM, mCRG, mCOM, mPRF, mCPF:
			// Pointer, registration, comment and profile segments are not needed to decode.
		default:
			if err := d.parseParam(m, p, &d.main, true); err != nil {
				return err
			}
		}
		pos = next
	}
	if d.main.cod == nil {
		return fmt.Errorf("jp2k: main header has no COD marker")
	}
	if d.main.qcd == nil {
		return fmt.Errorf("jp2k: main header has no QCD marker")
	}
	var ppm []byte
	if len(ppms) > 0 {
		d.usePPM = true
		sort.SliceStable(ppms, func(i, j int) bool { return ppms[i].z < ppms[j].z })
		for _, s := range ppms {
			ppm = append(ppm, s.data...)
		}
	}

	d.tiles = make([]*tileParts, d.ntx*d.nty)
	for pos < len(data) {
		if pos+2 > len(data) {
			break
		}
		m := u16(data[pos:])
		if m == mEOC {
			break
		}
		if m != mSOT {
			return fmt.Errorf("jp2k: expected SOT or EOC marker at offset %d, found 0x%04X", pos, m)
		}
		sot := pos
		_, p, next, err := d.segment(pos)
		if err != nil {
			return err
		}
		if len(p) != 8 {
			return fmt.Errorf("jp2k: SOT marker segment at offset %d has length %d, not 10", pos, len(p)+2)
		}
		isot, psot, tpsot := u16(p), u32(p[2:]), int(p[6])
		if isot >= len(d.tiles) {
			return fmt.Errorf("jp2k: tile-part at offset %d is for tile %d, the image has %d tiles", pos, isot, len(d.tiles))
		}
		tp := d.tiles[isot]
		if tp == nil {
			tp = &tileParts{}
			d.tiles[isot] = tp
		}
		// TPsot counts to 254 (A.4.2); OpenJPEG's encoder lets it wrap past 255, which is
		// accepted.
		if tpsot != tp.parts&0xFF {
			return fmt.Errorf("jp2k: tile %d: tile-part %d found where tile-part %d was expected", isot, tpsot, tp.parts)
		}
		tp.parts++
		pos = next
		var ppts []ppmSeg
		for {
			if pos+2 <= len(data) && u16(data[pos:]) == mSOD {
				pos += 2
				break
			}
			m, p, next, err := d.segment(pos)
			if err != nil {
				return err
			}
			switch m {
			case mPPT:
				if d.usePPM {
					return fmt.Errorf("jp2k: tile %d has a PPT marker although the main header has PPM", isot)
				}
				if len(p) < 1 {
					return fmt.Errorf("jp2k: tile %d: PPT marker segment is empty", isot)
				}
				ppts = append(ppts, ppmSeg{int(p[0]), p[1:]})
			case mPLT, mCOM:
			case mCOD, mCOC, mQCD, mQCC, mRGN:
				if tpsot != 0 {
					return fmt.Errorf("jp2k: tile %d: marker 0x%04X in tile-part %d (allowed only in the first tile-part)", isot, m, tpsot)
				}
				fallthrough
			default:
				if err := d.parseParam(m, p, &tp.hdr, false); err != nil {
					return fmt.Errorf("%w (tile %d)", err, isot)
				}
			}
			pos = next
		}
		sort.SliceStable(ppts, func(i, j int) bool { return ppts[i].z < ppts[j].z })
		for _, s := range ppts {
			tp.ppt = append(tp.ppt, s.data...)
		}
		end := len(data)
		if psot == 0 { // the last tile-part, up to EOC
			if end-pos >= 2 && u16(data[end-2:]) == mEOC {
				end -= 2
			}
		} else {
			end = sot + psot
			if end > len(data) {
				return fmt.Errorf("jp2k: tile %d: tile-part %d declares %d bytes, %d remain (truncated codestream)", isot, tpsot, psot, len(data)-sot)
			}
			if end < pos {
				return fmt.Errorf("jp2k: tile %d: tile-part %d declares %d bytes, less than its header", isot, tpsot, psot)
			}
		}
		if d.usePPM {
			if len(ppm) < 4 {
				return fmt.Errorf("jp2k: tile %d: PPM packet headers run out at tile-part %d", isot, tpsot)
			}
			n := u32(ppm)
			if n > len(ppm)-4 {
				return fmt.Errorf("jp2k: tile %d: PPM declares %d bytes of packet headers for tile-part %d, %d remain", isot, n, tpsot, len(ppm)-4)
			}
			tp.ppm = append(tp.ppm, ppm[4:4+n]...)
			ppm = ppm[4+n:]
		}
		tp.bodies = append(tp.bodies, data[pos:end])
		pos = end
	}
	return nil
}

func (d *decoder) parseSIZ(p []byte) error {
	if len(p) < 36 {
		return fmt.Errorf("jp2k: SIZ marker segment is too short (%d bytes)", len(p)+2)
	}
	s := &d.siz
	s.rsiz = u16(p)
	if s.rsiz&0x4000 != 0 {
		return fmt.Errorf("jp2k: high-throughput block coding (Part 15, Rsiz 0x%04X): %w", s.rsiz, ErrUnsupported)
	}
	if s.rsiz&0x8000 != 0 {
		return fmt.Errorf("jp2k: Part 2 extensions (Rsiz 0x%04X): %w", s.rsiz, ErrUnsupported)
	}
	s.x1, s.y1, s.x0, s.y0 = u32(p[2:]), u32(p[6:]), u32(p[10:]), u32(p[14:])
	s.tw, s.th, s.tx, s.ty = u32(p[18:]), u32(p[22:]), u32(p[26:]), u32(p[30:])
	n := u16(p[34:])
	if n < 1 || n > maxComponents {
		return fmt.Errorf("jp2k: SIZ declares %d components (1 to %d allowed)", n, maxComponents)
	}
	if len(p) < 36+3*n {
		return fmt.Errorf("jp2k: SIZ marker segment is too short for %d components", n)
	}
	if strconv.IntSize == 32 {
		for _, v := range []int{s.x1, s.y1, s.x0, s.y0, s.tw, s.th, s.tx, s.ty} {
			if v < 0 || v > 1<<29 {
				return fmt.Errorf("jp2k: image coordinates of %d are too large for a 32-bit platform", v)
			}
		}
	}
	if s.x1 <= s.x0 || s.y1 <= s.y0 {
		return fmt.Errorf("jp2k: SIZ declares an empty image area (%d..%d × %d..%d)", s.x0, s.x1, s.y0, s.y1)
	}
	if s.tw == 0 || s.th == 0 {
		return fmt.Errorf("jp2k: SIZ declares a zero tile size")
	}
	if s.tx > s.x0 || s.ty > s.y0 || s.tx+s.tw <= s.x0 || s.ty+s.th <= s.y0 {
		return fmt.Errorf("jp2k: SIZ tile grid origin (%d, %d) does not cover the image origin (%d, %d)", s.tx, s.ty, s.x0, s.y0)
	}
	d.ntx = ceilDiv(s.x1-s.tx, s.tw)
	d.nty = ceilDiv(s.y1-s.ty, s.th)
	if d.ntx > maxTiles || d.nty > maxTiles || d.ntx*d.nty > maxTiles {
		return fmt.Errorf("jp2k: SIZ declares %d×%d tiles (at most %d allowed)", d.ntx, d.nty, maxTiles)
	}
	if d.ntx*d.nty*n > maxTileComps {
		return fmt.Errorf("jp2k: SIZ declares %d tiles of %d components (at most %d tile-components allowed)", d.ntx*d.nty, n, maxTileComps)
	}
	s.comps = make([]sizComp, n)
	for i := range s.comps {
		b := p[36+3*i:]
		c := sizComp{prec: int(b[0]&0x7F) + 1, signed: b[0]&0x80 != 0, dx: int(b[1]), dy: int(b[2])}
		if c.prec > 38 {
			return fmt.Errorf("jp2k: component %d declares %d-bit samples (at most 38 allowed)", i, c.prec)
		}
		if c.prec > maxPrecision {
			return fmt.Errorf("jp2k: component %d has %d-bit samples, this decoder supports at most %d: %w", i, c.prec, maxPrecision, ErrUnsupported)
		}
		if c.dx == 0 || c.dy == 0 {
			return fmt.Errorf("jp2k: component %d declares a zero subsampling factor", i)
		}
		s.comps[i] = c
	}
	d.imgW = s.x1 - s.x0
	d.imgH = s.y1 - s.y0
	if d.opts.Reduce > 0 {
		d.imgW = ceilShift(s.x1, d.opts.Reduce) - ceilShift(s.x0, d.opts.Reduce)
		d.imgH = ceilShift(s.y1, d.opts.Reduce) - ceilShift(s.y0, d.opts.Reduce)
	}
	return nil
}

// compSize is the size of component c at the decoded resolution.
func (d *decoder) compSize(c int) (w, h int) {
	s, sc, r := &d.siz, d.siz.comps[c], d.opts.Reduce
	w = ceilShift(ceilDiv(s.x1, sc.dx), r) - ceilShift(ceilDiv(s.x0, sc.dx), r)
	h = ceilShift(ceilDiv(s.y1, sc.dy), r) - ceilShift(ceilDiv(s.y0, sc.dy), r)
	return w, h
}

// parseParam parses a coding-parameter marker segment into h.
func (d *decoder) parseParam(m int, p []byte, h *header, main bool) error {
	ncomp := len(d.siz.comps)
	compIndex := func() (int, []byte, error) {
		if ncomp < 257 {
			if len(p) < 1 {
				return 0, nil, fmt.Errorf("jp2k: marker 0x%04X is too short", m)
			}
			c := int(p[0])
			if c >= ncomp {
				return 0, nil, fmt.Errorf("jp2k: marker 0x%04X names component %d, the image has %d", m, c, ncomp)
			}
			return c, p[1:], nil
		}
		if len(p) < 2 {
			return 0, nil, fmt.Errorf("jp2k: marker 0x%04X is too short", m)
		}
		c := u16(p)
		if c >= ncomp {
			return 0, nil, fmt.Errorf("jp2k: marker 0x%04X names component %d, the image has %d", m, c, ncomp)
		}
		return c, p[2:], nil
	}
	switch m {
	case mCOD:
		cod, err := parseCOD(p)
		if err != nil {
			return err
		}
		h.cod = cod
	case mCOC:
		c, rest, err := compIndex()
		if err != nil {
			return err
		}
		if len(rest) < 1 {
			return fmt.Errorf("jp2k: COC marker for component %d is too short", c)
		}
		if rest[0]&^1 != 0 {
			return fmt.Errorf("jp2k: COC marker for component %d has unknown style flags 0x%02X", c, rest[0])
		}
		cp, err := parseSPco(rest[1:], rest[0]&1 != 0)
		if err != nil {
			return fmt.Errorf("%w (COC, component %d)", err, c)
		}
		if h.coc == nil {
			h.coc = map[int]*compParams{}
		}
		h.coc[c] = cp
	case mQCD:
		q, err := parseQuant(p)
		if err != nil {
			return err
		}
		h.qcd = q
	case mQCC:
		c, rest, err := compIndex()
		if err != nil {
			return err
		}
		q, err := parseQuant(rest)
		if err != nil {
			return fmt.Errorf("%w (QCC, component %d)", err, c)
		}
		if h.qcc == nil {
			h.qcc = map[int]*quantParams{}
		}
		h.qcc[c] = q
	case mRGN:
		c, rest, err := compIndex()
		if err != nil {
			return err
		}
		if len(rest) < 2 {
			return fmt.Errorf("jp2k: RGN marker for component %d is too short", c)
		}
		if rest[0] != 0 {
			return fmt.Errorf("jp2k: region of interest style %d (Part 2 general scaling) for component %d: %w", rest[0], c, ErrUnsupported)
		}
		if h.rgn == nil {
			h.rgn = map[int]int{}
		}
		h.rgn[c] = int(rest[1])
	case mPOC:
		size := 7
		if ncomp >= 257 {
			size = 9
		}
		if len(p) == 0 || len(p)%size != 0 {
			return fmt.Errorf("jp2k: POC marker segment has %d bytes, not a multiple of %d", len(p), size)
		}
		for b := p; len(b) > 0; b = b[size:] {
			var e pocEntry
			if size == 7 {
				e = pocEntry{rs: int(b[0]), cs: int(b[1]), le: u16(b[2:]), re: int(b[4]), ce: int(b[5]), prog: int(b[6])}
			} else {
				e = pocEntry{rs: int(b[0]), cs: u16(b[1:]), le: u16(b[3:]), re: int(b[5]), ce: u16(b[6:]), prog: int(b[8])}
			}
			if e.ce == 0 { // CEpoc 0 means 256 (A.6.6); clamped to the components below
				e.ce = 1 << 16
			}
			if e.prog > 4 {
				return fmt.Errorf("jp2k: POC names progression order %d (0 to 4 allowed)", e.prog)
			}
			h.poc = append(h.poc, e)
		}
	case mCAP:
		return fmt.Errorf("jp2k: extended capabilities (CAP marker, Part 2 or Part 15): %w", ErrUnsupported)
	case mSOT, mSOD, mEOC, mSOC, mSIZ:
		where := "the main header"
		if !main {
			where = "a tile-part header"
		}
		return fmt.Errorf("jp2k: unexpected marker 0x%04X in %s", m, where)
	case mPPM, mTLM, mPLM, mCRG:
		return fmt.Errorf("jp2k: marker 0x%04X is allowed only in the main header", m)
	case mPPT, mPLT:
		return fmt.Errorf("jp2k: marker 0x%04X is allowed only in tile-part headers", m)
	default:
		if name, ok := part2Markers[m]; ok {
			return fmt.Errorf("jp2k: %s, a Part 2 feature: %w", name, ErrUnsupported)
		}
		if m >= 0xFF30 && m <= 0xFF3F {
			return nil
		}
		return fmt.Errorf("jp2k: unknown marker 0x%04X", m)
	}
	return nil
}

func parseCOD(p []byte) (*codParams, error) {
	if len(p) < 5 {
		return nil, fmt.Errorf("jp2k: COD marker segment is too short")
	}
	c := &codParams{scod: p[0], prog: int(p[1]), layers: u16(p[2:]), mct: int(p[4])}
	if c.scod&0x18 != 0 {
		return nil, fmt.Errorf("jp2k: code-block partition origin (COD style 0x%02X, Part 2): %w", c.scod, ErrUnsupported)
	}
	if c.scod&^7 != 0 {
		return nil, fmt.Errorf("jp2k: COD has unknown style flags 0x%02X", c.scod)
	}
	if c.prog > 4 {
		return nil, fmt.Errorf("jp2k: COD names progression order %d (0 to 4 allowed)", c.prog)
	}
	if c.layers == 0 {
		return nil, fmt.Errorf("jp2k: COD declares zero quality layers")
	}
	if c.mct > 1 {
		return nil, fmt.Errorf("jp2k: multiple component transform %d (Part 2 array-based transform): %w", c.mct, ErrUnsupported)
	}
	cp, err := parseSPco(p[5:], c.scod&1 != 0)
	if err != nil {
		return nil, fmt.Errorf("%w (COD)", err)
	}
	c.comp = *cp
	return c, nil
}

// parseSPco parses the SPcod/SPcoc parameters (Table A.15).
func parseSPco(p []byte, precincts bool) (*compParams, error) {
	if len(p) < 5 {
		return nil, fmt.Errorf("jp2k: coding style parameters are too short")
	}
	c := &compParams{levels: int(p[0]), cbw: int(p[1]) + 2, cbh: int(p[2]) + 2, style: p[3], reversible: p[4] == 1}
	if c.levels > maxLevels {
		return nil, fmt.Errorf("jp2k: %d decomposition levels declared (at most %d allowed)", c.levels, maxLevels)
	}
	if c.cbw > 10 || c.cbh > 10 || c.cbw+c.cbh > 12 {
		return nil, fmt.Errorf("jp2k: code-block size 2^%d×2^%d is not allowed", c.cbw, c.cbh)
	}
	if c.style&0xC0 != 0 {
		return nil, fmt.Errorf("jp2k: high-throughput code-blocks (code-block style 0x%02X, Part 15): %w", c.style, ErrUnsupported)
	}
	if p[4] > 1 {
		return nil, fmt.Errorf("jp2k: wavelet transform %d (Part 2 arbitrary transformation kernel): %w", p[4], ErrUnsupported)
	}
	n := c.levels + 1
	c.ppx, c.ppy = make([]uint8, n), make([]uint8, n)
	if !precincts {
		for r := range n {
			c.ppx[r], c.ppy[r] = 15, 15
		}
		return c, nil
	}
	if len(p) < 5+n {
		return nil, fmt.Errorf("jp2k: precinct sizes for %d resolutions are missing", n)
	}
	for r := range n {
		v := p[5+r]
		c.ppx[r], c.ppy[r] = v&0xF, v>>4
		if r > 0 && (c.ppx[r] == 0 || c.ppy[r] == 0) {
			return nil, fmt.Errorf("jp2k: precinct size exponent 0 at resolution %d (allowed only at resolution 0)", r)
		}
	}
	return c, nil
}

// parseQuant parses Sqcd/Sqcc and the step sizes that follow (Tables A.28 to A.30).
func parseQuant(p []byte) (*quantParams, error) {
	if len(p) < 1 {
		return nil, fmt.Errorf("jp2k: quantization marker segment is too short")
	}
	q := &quantParams{style: int(p[0] & 0x1F), guard: int(p[0] >> 5)}
	b := p[1:]
	switch q.style {
	case 0:
		for _, v := range b {
			q.exp = append(q.exp, int(v>>3))
			q.mant = append(q.mant, 0)
		}
	case 1, 2:
		if len(b)%2 != 0 {
			return nil, fmt.Errorf("jp2k: quantization step sizes have an odd length")
		}
		for i := 0; i+1 < len(b); i += 2 {
			v := u16(b[i:])
			q.exp = append(q.exp, v>>11)
			q.mant = append(q.mant, v&0x7FF)
		}
		if q.style == 1 && len(q.exp) > 1 {
			q.exp, q.mant = q.exp[:1], q.mant[:1]
		}
	default:
		return nil, fmt.Errorf("jp2k: quantization style %d is not defined by Part 1: %w", q.style, ErrUnsupported)
	}
	if len(q.exp) == 0 {
		return nil, fmt.Errorf("jp2k: quantization marker segment has no step sizes")
	}
	return q, nil
}

// ceilDiv is ceil(a/b) for b > 0 and any a.
func ceilDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a > 0) == (b > 0) {
		q++
	}
	return q
}

// ceilShift is ceil(a/2^s) for a >= 0.
func ceilShift(a, s int) int {
	if s >= 62 {
		if a > 0 {
			return 1
		}
		return 0
	}
	return (a + (1 << s) - 1) >> s
}
