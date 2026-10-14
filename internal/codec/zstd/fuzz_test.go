// Fuzzing: arbitrary input must never panic, never produce more than the limit, and decode the
// same way whatever dst already holds and whatever an earlier call left in the pooled decoder.
// Seeded with the fixtures and the hand-built frames; crashers found are kept under
// testdata/fuzz/FuzzDecode. Run with
//
//	go test ./internal/codec/zstd/ -run '^$' -fuzz FuzzDecode -fuzztime 60s

package zstd

import (
	"bytes"
	"testing"
)

func FuzzDecode(f *testing.F) {
	for _, fx := range fixtures {
		comp := readFixture(f, fx.name)
		// Large seeds slow every mutation; their first kilobytes still carry valid headers
		// and tables.
		if len(comp) > 32<<10 || len(fx.content()) > 1<<20 {
			comp = comp[:min(len(comp), 4<<10)]
		}
		f.Add(comp)
	}
	for _, c := range handBuiltFrames() {
		f.Add(c.src)
	}
	const limit = 8 << 20
	f.Fuzz(func(t *testing.T, src []byte) {
		out, err := Decode(nil, src, limit)
		if err == nil && len(out) > limit {
			t.Fatalf("decoded %d bytes past the %d-byte limit", len(out), limit)
		}
		prefix := []byte("prefix")
		again, err2 := Decode(append([]byte(nil), prefix...), src, limit)
		if (err == nil) != (err2 == nil) {
			t.Fatalf("decoding twice disagrees: %v, then %v", err, err2)
		}
		if err == nil && !bytes.Equal(again[len(prefix):], out) {
			t.Fatal("decoding after a prefix gives different output")
		}
		if err != nil && err.Error() != err2.Error() {
			t.Fatalf("decoding twice gives different errors: %v, then %v", err, err2)
		}
	})
}
