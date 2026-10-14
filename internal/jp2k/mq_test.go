// The MQ decoder against the published test sequence for the MQ arithmetic coder (ITU-T T.88
// Annex H.2, the same coder as T.800 Annex C): 256 decisions in one context, starting in state 0
// with MPS 0.

package jp2k

import (
	"encoding/hex"
	"math/bits"
	"strings"
	"testing"
)

func TestMQTestSequence(t *testing.T) {
	unhex := func(s string) []byte {
		b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	decisions := unhex("00 02 00 51 00 00 00 C0 03 52 87 2A AA AA AA AA 82 C0 20 00 FC D7 9E F6 BF 7F ED 90 4F 46 A3 BF")
	coded := unhex("84 C7 3B FC E1 A1 43 04 02 20 00 00 41 0D BB 86 F4 31 7F FF 88 FF 37 47 1A DB 6A DF FF AC")
	var m mqDecoder
	m.init(coded)
	cx := mqTable[0]
	for i := range 8 * len(decisions) {
		want := uint32(decisions[i/8]>>(7-i%8)) & 1
		if got := m.decode(&cx); got != want {
			t.Fatalf("decision %d: got %d, want %d", i, got, want)
		}
	}
	// The inlined decision used by the coding passes must agree with decode.
	m.init(coded)
	cx = mqTable[0]
	qa, qc, qct := m.a, m.c, m.ct
	for i := range 8 * len(decisions) {
		want := uint32(decisions[i/8]>>(7-i%8)) & 1
		var d uint32
		var nx uint8
		d, qa, qc, nx = mqDecide(qa-cx.qe, qc, cx)
		cx = mqTable[nx]
		if n := bits.LeadingZeros32(qa) - 16; n > qct {
			qa, qc, qct = m.renorm(qa, qc, qct)
		} else {
			qa, qc, qct = qa<<n, qc<<n, qct-n
		}
		if d != want {
			t.Fatalf("mqDecide decision %d: got %d, want %d", i, d, want)
		}
	}
}
