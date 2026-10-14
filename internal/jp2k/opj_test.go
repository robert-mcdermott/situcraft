// On-the-fly comparison with OpenJPEG: random synthetic images encoded by opj_compress with random
// parameter combinations, decoded by this package and by opj_decompress. Skipped when the OpenJPEG
// tools are not on PATH. JP2K_OPJ_CASES sets the number of cases (default 300), JP2K_OPJ_SEED the
// seed (default fixed, so a failure can be replayed).

package jp2k

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// encCase is one encoding: an image, opj_compress arguments and how to decode it.
type encCase struct {
	name     string
	im       *synthImage
	args     []string
	jp2      bool
	reduce   int
	lossless bool // 5/3 with every layer: the decode must be exact
}

func (c encCase) String() string {
	return fmt.Sprintf("%s %dx%d c%d p%d s%v sub%v reduce%d jp2 %v: %s", c.name, c.im.w, c.im.h, len(c.im.comps), c.im.prec, c.im.signed, c.im.dx, c.reduce, c.jp2, strings.Join(c.args, " "))
}

// randomCase draws an encoding the OpenJPEG encoder accepts (it rejects some combinations, which
// the caller skips).
func randomCase(rng *rand.Rand, i int) encCase {
	pick := func(p float64) bool { return rng.Float64() < p }
	w, h := 8+rng.IntN(120), 8+rng.IntN(120)
	if pick(0.05) {
		w, h = 1+rng.IntN(7), 1+rng.IntN(7)
	}
	ncomp := 1
	switch r := rng.Float64(); {
	case r < 0.35:
		ncomp = 3
	case r < 0.4:
		ncomp = 2 + 2*rng.IntN(2)
	}
	prec := 8
	switch r := rng.Float64(); {
	case r < 0.25:
		prec = 16
	case r < 0.45:
		prec = []int{1, 4, 10, 12, 14}[rng.IntN(5)]
	case r < 0.5 && ncomp == 1:
		prec = []int{20, 24}[rng.IntN(2)]
	}
	signed := pick(0.1)
	dx, dy := make([]int, ncomp), make([]int, ncomp)
	for c := range ncomp {
		dx[c], dy[c] = 1, 1
	}
	if ncomp > 1 && pick(0.15) {
		for c := 1; c < ncomp; c++ {
			dx[c], dy[c] = 1+rng.IntN(2), 1+rng.IntN(2)
		}
		if ncomp == 3 {
			// opj_decompress takes three components with component 1 subsampled horizontally
			// for sYCC and converts them to RGB, so its output would not be the components.
			dx[1] = 1
		}
		// opj_compress reads raw subsampled components of odd-sized images differently from
		// the codestream's sizes, so subsampled images are kept even.
		w, h = w+w%2, h+h%2
	}
	im := makeImage(patterns[rng.IntN(len(patterns))], w, h, ncomp, prec, signed, dx, dy, uint64(i))
	var args []string
	irreversible := pick(0.5)
	if irreversible {
		args = append(args, "-I")
	}
	layers := 1
	if pick(0.6) {
		layers = 1 + rng.IntN(3)
		ratio := 2 + rng.IntN(30)
		var rs []string
		for l := range layers {
			rs = append(rs, strconv.Itoa(max(ratio>>l, 1)))
		}
		if !irreversible && pick(0.5) {
			rs[len(rs)-1] = "1" // the last layer lossless
		}
		args = append(args, "-r", strings.Join(rs, ","))
	}
	tw, th := w, h
	if pick(0.3) {
		tw, th = 4+rng.IntN(w), 4+rng.IntN(h)
		args = append(args, "-t", fmt.Sprintf("%d,%d", tw, th))
		if pick(0.3) {
			ox, oy := rng.IntN(20), rng.IntN(20)
			tx, ty := rng.IntN(ox+1), rng.IntN(oy+1)
			args = append(args, "-d", fmt.Sprintf("%d,%d", ox, oy), "-T", fmt.Sprintf("%d,%d", tx, ty))
		}
	} else if pick(0.15) {
		args = append(args, "-d", fmt.Sprintf("%d,%d", rng.IntN(20), rng.IntN(20)))
	}
	maxRes := 1
	for m := min(tw, th); m >= 2 && maxRes < 8; m /= 2 {
		maxRes++
	}
	nres := 1 + rng.IntN(maxRes)
	args = append(args, "-n", strconv.Itoa(nres))
	cbw, cbh := 2+rng.IntN(5), 2+rng.IntN(5) // exponents 2..6
	args = append(args, "-b", fmt.Sprintf("%d,%d", 1<<cbw, 1<<cbh))
	if pick(0.4) {
		var ps []string
		for range 1 + rng.IntN(3) {
			ps = append(ps, fmt.Sprintf("[%d,%d]", 1<<(2+rng.IntN(6)), 1<<(2+rng.IntN(6))))
		}
		args = append(args, "-c", strings.Join(ps, ","))
	}
	args = append(args, "-p", []string{"LRCP", "RLCP", "RPCL", "PCRL", "CPRL"}[rng.IntN(5)])
	if pick(0.35) {
		args = append(args, "-M", strconv.Itoa(1+rng.IntN(63)))
	}
	if pick(0.2) {
		args = append(args, "-SOP")
	}
	if pick(0.2) {
		args = append(args, "-EPH")
	}
	if pick(0.15) {
		args = append(args, "-TP", []string{"R", "L", "C"}[rng.IntN(3)])
	}
	if pick(0.1) {
		args = append(args, "-PLT")
	}
	if pick(0.1) {
		args = append(args, "-TLM")
	}
	if pick(0.1) {
		args = append(args, "-ROI", fmt.Sprintf("c=%d,U=%d", rng.IntN(ncomp), 1+rng.IntN(12)))
	}
	if pick(0.1) {
		args = append(args, "-GuardBits", strconv.Itoa(1+rng.IntN(4)))
	}
	if ncomp >= 3 && pick(0.2) {
		args = append(args, "-mct", "0")
	}
	if pick(0.08) {
		// Progression order changes for tile 0: two entries covering every packet.
		prog := []string{"LRCP", "RLCP", "RPCL", "PCRL", "CPRL"}
		r1 := 1 + rng.IntN(nres)
		args = append(args, "-POC", fmt.Sprintf("T0=0,0,%d,%d,%d,%s/T0=0,0,%d,%d,%d,%s",
			layers, r1, ncomp, prog[rng.IntN(5)], layers, nres, ncomp, prog[rng.IntN(5)]))
	}
	reduce := 0
	if nres > 1 && pick(0.2) {
		reduce = 1 + rng.IntN(nres-1)
	}
	lossless := !irreversible
	for k, a := range args {
		if a == "-r" && !strings.HasSuffix(args[k+1], ",1") && args[k+1] != "1" {
			lossless = false
		}
	}
	return encCase{name: fmt.Sprintf("case%d", i), im: im, args: args, jp2: pick(0.3), reduce: reduce, lossless: lossless}
}

func TestOPJRandom(t *testing.T) {
	if opjCompress == "" || opjDecompress == "" {
		t.Skip("opj_compress or opj_decompress is not on PATH")
	}
	n := 300
	if testing.Short() {
		n = 40
	}
	if s := os.Getenv("JP2K_OPJ_CASES"); s != "" {
		n, _ = strconv.Atoi(s)
	}
	seed := uint64(20261013)
	if s := os.Getenv("JP2K_OPJ_SEED"); s != "" {
		seed, _ = strconv.ParseUint(s, 10, 64)
	}
	only := -1 // JP2K_OPJ_ONLY replays one case
	if s := os.Getenv("JP2K_OPJ_ONLY"); s != "" {
		only, _ = strconv.Atoi(s)
		n = max(n, only+1)
	}
	rng := rand.New(rand.NewPCG(seed, 1))
	var st opjTally
	for i := range n {
		c := randomCase(rng, i)
		if only >= 0 && i != only {
			continue
		}
		data, err := opjEncode(t, c.im, c.args, c.jp2)
		if only >= 0 && err == nil {
			_ = os.WriteFile(filepath.Join(os.TempDir(), "jp2k-case.j2k"), data, 0o644)
			t.Logf("%v: wrote %s", c, filepath.Join(os.TempDir(), "jp2k-case.j2k"))
		}
		if err != nil {
			continue // a combination the encoder refuses
		}
		ref, err := opjDecode(t, data, c.reduce)
		if err != nil {
			continue // or that OpenJPEG cannot decode
		}
		checkAgainstOPJ(t, c, data, ref, 1+i%4, &st)
	}
	t.Logf("%v", st)
}

// opjTally counts the outcomes of comparisons with OpenJPEG.
type opjTally struct {
	cases, exact, emulatedExact int
	opjWrong                    int // lossless cases where OpenJPEG's decode is not the original
	skipped                     int // cases OpenJPEG is known to decode wrongly (bypass with ROI, > 255 tile-parts)
	encoderBad                  int // codestreams OpenJPEG's encoder made with too many bit-planes
	standard                    diffStats
}

func (s opjTally) String() string {
	return fmt.Sprintf("%d cases: %d exact (lossless or 5/3), %d irreversible bit-exact with OpenJPEG's high-pass constant, %d lossless where OpenJPEG's decode differs from the original, %d skipped (bypass with ROI or more than 255 tile-parts, which OpenJPEG decodes wrongly), %d refused (more bit-planes than signalled, an OpenJPEG encoder error); standard 9/7 vs OpenJPEG: %v",
		s.cases, s.exact, s.emulatedExact, s.opjWrong, s.skipped, s.encoderBad, s.standard)
}

// checkAgainstOPJ decodes one encoding and checks it: a lossless encoding must reproduce the
// original image exactly; any other 5/3 encoding must equal OpenJPEG's output; a 9/7 encoding must
// equal it bit for bit when decoded with OpenJPEG's high-pass constant, and with the standard's
// it is compared within opjTolerance.
func checkAgainstOPJ(t *testing.T, c encCase, data []byte, ref []refComp, workers int, st *opjTally) {
	t.Helper()
	st.cases++
	img, err := Decode(data, Options{Reduce: c.reduce, Workers: workers})
	if err != nil {
		// OpenJPEG's encoder can code more bit-planes than the quantization allows (the RCT's
		// extra bit overflowing the guard bits), a codestream this decoder refuses; OpenJPEG's
		// own decode of it is then not the original either.
		if c.lossless && strings.Contains(err.Error(), "coding passes") {
			full := ref
			if c.reduce > 0 {
				full, _ = opjDecode(t, data, 0)
			}
			var sr diffStats
			for k := range c.im.comps {
				if k < len(full) && len(full[k].data) == len(c.im.comps[k]) {
					sr.add(full[k].data, c.im.comps[k])
				}
			}
			if sr.differing > 0 {
				st.encoderBad++
				return
			}
		}
		t.Errorf("%v: %v", c, err)
		return
	}
	s, err := compareRef(img, ref)
	if err != nil {
		t.Errorf("%v: %v", c, err)
		return
	}
	irreversible := slicesContain(c.args, "-I")
	bypassROI := slicesContain(c.args, "-ROI") && modeHas(c.args, cbBypass)
	// OpenJPEG's encoder numbers tile-parts past 255 (TPsot wraps) and its decoder then drops
	// them.
	if d, err := newDecoder(data, Options{}); err == nil {
		for _, tp := range d.tiles {
			if tp != nil && tp.parts > 255 {
				bypassROI = true
			}
		}
	}
	if c.lossless && c.reduce == 0 {
		// The decode must reproduce the original, or, where OpenJPEG's encoder was not lossless
		// (it is not for a lone sample at an odd coordinate), OpenJPEG's own decode.
		var so diffStats
		for k := range c.im.comps {
			so.add(img.Components[k].Data, c.im.comps[k])
		}
		if so.differing > 0 && s.differing > 0 {
			t.Errorf("%v: lossless decode differs from the original (%v) and from OpenJPEG (%v)%s", c, so, s, firstDiff(img, c.im.comps))
		}
		if s.differing > 0 {
			st.opjWrong++
		}
		st.exact++
		return
	}
	if bypassROI {
		st.skipped++
		return
	}
	if !irreversible {
		if s.differing > 0 {
			t.Errorf("%v: 5/3 decode differs from OpenJPEG: %v", c, s)
		}
		st.exact++
		return
	}
	emu, err := Decode(data, Options{Reduce: c.reduce, Workers: workers, opjHighpass: true})
	if err != nil {
		t.Errorf("%v: %v", c, err)
		return
	}
	se, _ := compareRef(emu, ref)
	if se.differing > 0 {
		t.Errorf("%v: with OpenJPEG's high-pass constant, decode differs from OpenJPEG: %v", c, se)
	} else {
		st.emulatedExact++
	}
	if tol := opjTolerance(c.im.prec); s.maxAbs > tol {
		t.Errorf("%v: standard decode differs from OpenJPEG by more than %d: %v", c, tol, s)
	}
	st.standard.merge(s)
}

// opjTolerance is how far the standard 9/7 decode may stray from OpenJPEG's: one level for
// rounding, plus what OpenJPEG's approximate high-pass constant (3.3e-5 relative, on coefficients
// up to a few times full scale) can add at this precision: seen up to 4 for 16-bit noise, 24 for
// 24 bits.
func opjTolerance(prec int) int64 {
	return 1 + int64(math.Ldexp(1e-4, prec))
}

func slicesContain(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

// modeHas reports whether the -M argument includes the code-block style flag f.
func modeHas(args []string, f int) bool {
	for i, a := range args {
		if a == "-M" && i+1 < len(args) {
			m, _ := strconv.Atoi(args[i+1])
			return m&f != 0
		}
	}
	return false
}

// firstDiff describes the first sample where img differs from want.
func firstDiff(img *Image, want [][]int32) string {
	for k := range want {
		comp := img.Components[k]
		for i, v := range want[k] {
			if comp.Data[i] != v {
				return fmt.Sprintf("; first at component %d (%d, %d): %d, want %d", k, i%comp.Width, i/comp.Width, comp.Data[i], v)
			}
		}
	}
	return ""
}
