// The MQ arithmetic decoder (T.800 Annex C) and the raw bit reader of the selective arithmetic
// coding bypass (D.6).

package jp2k

import "math/bits"

// mqState is a context's probability state (Table C.2) and MPS. States are numbered by index times
// two plus MPS; a context holds its whole state, so a decision needs only one load.
type mqState struct {
	qe         uint32
	nmps, nlps uint8 // the number of the next state after an MPS or an LPS
	mps        uint8
	self       uint8 // this state's number
}

var mqTable [94]mqState

func init() {
	// Table C.2: Qe, NMPS, NLPS, SWITCH for each of the 47 states.
	base := [47][4]uint32{
		{0x5601, 1, 1, 1}, {0x3401, 2, 6, 0}, {0x1801, 3, 9, 0}, {0x0AC1, 4, 12, 0},
		{0x0521, 5, 29, 0}, {0x0221, 38, 33, 0}, {0x5601, 7, 6, 1}, {0x5401, 8, 14, 0},
		{0x4801, 9, 14, 0}, {0x3801, 10, 14, 0}, {0x3001, 11, 17, 0}, {0x2401, 12, 18, 0},
		{0x1C01, 13, 20, 0}, {0x1601, 29, 21, 0}, {0x5601, 15, 14, 1}, {0x5401, 16, 14, 0},
		{0x5101, 17, 15, 0}, {0x4801, 18, 16, 0}, {0x3801, 19, 17, 0}, {0x3401, 20, 18, 0},
		{0x3001, 21, 19, 0}, {0x2801, 22, 19, 0}, {0x2401, 23, 20, 0}, {0x2201, 24, 21, 0},
		{0x1C01, 25, 22, 0}, {0x1801, 26, 23, 0}, {0x1601, 27, 24, 0}, {0x1401, 28, 25, 0},
		{0x1201, 29, 26, 0}, {0x1101, 30, 27, 0}, {0x0AC1, 31, 28, 0}, {0x09C1, 32, 29, 0},
		{0x08A1, 33, 30, 0}, {0x0521, 34, 31, 0}, {0x0441, 35, 32, 0}, {0x02A1, 36, 33, 0},
		{0x0221, 37, 34, 0}, {0x0141, 38, 35, 0}, {0x0111, 39, 36, 0}, {0x0085, 40, 37, 0},
		{0x0049, 41, 38, 0}, {0x0025, 42, 39, 0}, {0x0015, 43, 40, 0}, {0x0009, 44, 41, 0},
		{0x0005, 45, 42, 0}, {0x0001, 45, 43, 0}, {0x5601, 46, 46, 0},
	}
	for i, s := range base {
		for mps := uint32(0); mps < 2; mps++ {
			lps := mps
			if s[3] == 1 {
				lps = 1 - mps
			}
			mqTable[2*i+int(mps)] = mqState{qe: s[0], nmps: uint8(2*s[1] + mps), nlps: uint8(2*s[2] + lps), mps: uint8(mps), self: uint8(2*i) + uint8(mps)}
		}
	}
}

// mqDecoder is the decoder of C.3, with the 32-bit C register holding Chigh in its upper half.
// Bytes past the end of the segment read as 0xFF, as at a terminating marker (C.3.4).
type mqDecoder struct {
	data []byte
	pos  int
	c, a uint32
	ct   int
}

func (m *mqDecoder) byteAt(i int) uint32 {
	if i < len(m.data) {
		return uint32(m.data[i])
	}
	return 0xFF
}

// init is INITDEC (C.3.5).
func (m *mqDecoder) init(data []byte) {
	m.data, m.pos = data, 0
	m.c = m.byteAt(0) << 16
	m.bytein()
	m.c <<= 7
	m.ct -= 7
	m.a = 0x8000
}

// bytein is BYTEIN (C.3.4), with the bit stuffing that follows a 0xFF byte.
func (m *mqDecoder) bytein() {
	if m.byteAt(m.pos) == 0xFF {
		if b1 := m.byteAt(m.pos + 1); b1 > 0x8F {
			m.c += 0xFF00
			m.ct = 8
		} else {
			m.pos++
			m.c += b1 << 9
			m.ct = 7
		}
		return
	}
	m.pos++
	m.c += m.byteAt(m.pos) << 8
	m.ct = 8
}

// decode is DECODE (C.3.2) for the context cx. The coder puts the LPS sub-interval (size Qe) at
// the bottom of the interval and the MPS sub-interval above it, and exchanges them when the MPS
// sub-interval would be the smaller (conditional exchange). RENORMD (C.3.3) shifts by the count
// of leading zeros of A at once rather than bit by bit, reading a byte whenever CT runs out.
func (m *mqDecoder) decode(cx *mqState) uint32 {
	s := *cx
	a := m.a - s.qe
	c := m.c
	var d uint32
	if q := s.qe << 16; c >= q {
		c -= q
		if a&0x8000 != 0 {
			m.a, m.c = a, c
			return uint32(s.mps)
		}
		if a < s.qe { // MPS_EXCHANGE (C.3.2)
			d = 1 - uint32(s.mps)
			*cx = mqTable[s.nlps]
		} else {
			d = uint32(s.mps)
			*cx = mqTable[s.nmps]
		}
	} else {
		if a < s.qe { // LPS_EXCHANGE
			d = uint32(s.mps)
			*cx = mqTable[s.nmps]
		} else {
			d = 1 - uint32(s.mps)
			*cx = mqTable[s.nlps]
		}
		a = s.qe
	}
	n := bits.LeadingZeros32(a) - 16
	ct := m.ct
	for n > 0 {
		if ct == 0 {
			m.c = c
			m.bytein()
			c, ct = m.c, m.ct
		}
		k := min(n, ct)
		a <<= k
		c <<= k
		ct -= k
		n -= k
	}
	m.a, m.c, m.ct = a, c, ct
	return d
}

// mqDecide makes the decision of DECODE (C.3.2) for a context in state st, with A (less Qe) and C
// held by the caller: the region of C picks MPS or LPS, the conditional exchange swaps them, and
// the context's next state is st itself unless renormalization follows. It selects with
// arithmetic instead of branches, which the decision would mispredict, and is small enough for Go
// to inline. The caller stores the next state and renormalizes.
func mqDecide(a, c uint32, st mqState) (d, a2, c2 uint32, next uint8) {
	q := st.qe << 16
	lps := uint32((uint64(c) - uint64(q)) >> 63) // C below the MPS region (C < Qe·2^16)
	x := (a-st.qe)>>31 ^ lps                     // after an exchange (A < Qe) the regions swap
	next = st.nmps + uint8(x)*(st.nlps-st.nmps)
	c -= q &^ -lps                          // the MPS region is above the LPS one
	a += (st.qe - a) & -lps                 // the LPS region has size Qe
	next += uint8(a>>15) * (st.self - next) // no renormalization: the state stays
	return x ^ uint32(st.mps), a, c, next
}

// renorm is RENORMD (C.3.3) for registers held by the caller, when renormalization needs more
// bits than are left in C.
func (m *mqDecoder) renorm(a, c uint32, ct int) (uint32, uint32, int) {
	for a&0x8000 == 0 {
		if ct == 0 {
			m.c = c
			m.bytein()
			c, ct = m.c, m.ct
		}
		a <<= 1
		c <<= 1
		ct--
	}
	return a, c, ct
}

// rawDecoder reads the raw (bypassed) bits of a selective arithmetic coding bypass segment:
// most significant bit first, skipping the stuffed bit after a 0xFF byte. Bits past the end of
// the segment read as 1.
type rawDecoder struct {
	data []byte
	pos  int
	c    uint32
	ct   uint
}

func (r *rawDecoder) init(data []byte) {
	r.data, r.pos, r.c, r.ct = data, 0, 0, 0
}

func (r *rawDecoder) bit() uint32 {
	if r.ct == 0 {
		prev := r.c
		r.c = 0xFF
		if r.pos < len(r.data) {
			r.c = uint32(r.data[r.pos])
			r.pos++
		}
		r.ct = 8
		if prev == 0xFF {
			r.ct = 7
		}
	}
	r.ct--
	return (r.c >> r.ct) & 1
}
