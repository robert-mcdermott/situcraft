// FuzzDecode: any bytes and options must give an image or an error, never a panic, and a decoded
// image must be well formed. The corpus is the committed fixtures; inputs the fuzzer found that
// failed are kept in testdata/fuzz/FuzzDecode as regression cases.

package jp2k

import "testing"

func FuzzDecode(f *testing.F) {
	// Smaller limits keep each input cheap; they bound the same allocations and loops.
	saved := [3]int{maxSamples, maxPrecinctSlots, progressionBudget}
	maxSamples, maxPrecinctSlots, progressionBudget = 1<<20, 1<<16, 1<<22
	f.Cleanup(func() { maxSamples, maxPrecinctSlots, progressionBudget = saved[0], saved[1], saved[2] })
	for _, b := range fixtureFiles(f) {
		f.Add(b, uint8(0))
	}
	f.Add([]byte{0xFF, 0x4F, 0xFF, 0x51, 0, 41}, uint8(9))
	f.Fuzz(func(t *testing.T, data []byte, opt uint8) {
		opts := Options{Reduce: int(opt & 7), Workers: 1 + int(opt>>3&3)}
		img, err := Decode(data, opts)
		if err == nil {
			for c, comp := range img.Components {
				if len(comp.Data) != comp.Width*comp.Height {
					t.Fatalf("component %d has %d samples for %d×%d", c, len(comp.Data), comp.Width, comp.Height)
				}
				lo, hi := int32(0), int32(uint32(1)<<comp.Precision-1)
				if comp.Signed {
					lo, hi = -1<<(comp.Precision-1), 1<<(comp.Precision-1)-1
				}
				for _, v := range comp.Data {
					if v < lo || v > hi {
						t.Fatalf("component %d sample %d outside [%d, %d]", c, v, lo, hi)
					}
				}
			}
		}
		pix, w, h, err := DecodeGray16(data, opts)
		if err == nil && len(pix) != w*h {
			t.Fatalf("DecodeGray16 gave %d samples for %d×%d", len(pix), w, h)
		}
	})
}
