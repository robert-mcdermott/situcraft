// Committed fixtures: small .zst files made by the zstd command-line tool from synthetic content
// that the test regenerates from a seed, so each decoded fixture is compared byte for byte.
// testdata/expected.json records each one's decompressed size and SHA-256, the tool's arguments
// and its version. Regenerate them (with zstd installed) by
//
//	go test ./internal/codec/zstd/ -run TestFixtures -update

package zstd

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "regenerate testdata/*.zst and testdata/expected.json with the zstd tool")

// A frame of a fixture: content, the tool's arguments, and whether it is compressed from a pipe
// (so that the frame has no content size).
type part struct {
	content func() []byte
	args    []string
	stdin   bool
	skip    []byte // instead: a skippable frame with this payload
}

type fixture struct {
	name  string
	parts []part
	check func(t *testing.T, s *stats)
}

func one(content func() []byte, args ...string) []part {
	return []part{{content: content, args: args}}
}

func piped(content func() []byte, args ...string) []part {
	return []part{{content: content, args: args, stdin: true}}
}

func gen(g func(uint64, int) []byte, seed uint64, n int) func() []byte {
	return func() []byte { return g(seed, n) }
}

// rleLiterals: a random block, then copies of stretches of it separated by single spaces, so
// that a later block's literals are all one byte.
func rleLiterals() []byte {
	head := genRandom(31, 128<<10)
	out := append([]byte(nil), head...)
	r := rng{32}
	for len(out) < 300<<10 {
		n := 64 + r.intn(400)
		from := r.intn(len(head) - n)
		out = append(out, head[from:from+n]...)
		out = append(out, ' ')
	}
	return out
}

// nibbles: bytes 0 to 7 with a skewed distribution; so few distinct literals make the encoder
// describe the Huffman weights directly rather than FSE-compressed.
func nibbles() []byte {
	r := rng{41}
	out := make([]byte, 50<<10)
	for i := range out {
		u := r.float()
		out[i] = byte(8 * u * u)
	}
	return out
}

// letters: random lowercase letters, with no repeats worth a match at level 1: a compressed
// block of Huffman-coded literals and no sequences.
func letters() []byte {
	r := rng{51}
	out := make([]byte, 1000)
	for i := range out {
		out[i] = byte('a' + r.intn(26))
	}
	return out
}

// records: 64-byte records that differ from the previous one in their first byte only, so that
// every sequence is the same (one literal, a 63-byte match at repeat offset 1) and the encoder
// describes literal lengths and offsets in RLE mode.
func records() []byte {
	r := rng{61}
	tail := genRandom(62, 63)
	out := make([]byte, 0, 300<<10)
	for len(out) < 300<<10 {
		out = append(out, byte(r.next()))
		out = append(out, tail...)
	}
	return out
}

func constant() []byte { return bytes.Repeat([]byte{'A'}, 300<<10) }

func empty() []byte { return []byte{} }

var fixtures = []fixture{
	{"text-l1", one(gen(genText, 1, 300<<10), "-1"), wantFeatures("fourStreams", "crossBlock", "checksums")},
	{"text-l3", one(gen(genText, 1, 300<<10), "-3"), wantFeatures("fourStreams", "crossBlock")},
	{"text-l9", one(gen(genText, 1, 300<<10), "-9"), wantFeatures("fourStreams", "crossBlock")},
	{"text-l19", one(gen(genText, 1, 300<<10), "-19"), wantFeatures("fourStreams", "crossBlock", "fseWeights")},
	{"text-ultra22", one(gen(genText, 1, 300<<10), "--ultra", "-22"), wantFeatures("fourStreams", "crossBlock")},
	{"floats-l3", one(gen(genFloats, 2, 200<<10), "-3"), nil},
	{"floats-l19", one(gen(genFloats, 2, 200<<10), "-19"), nil},
	{"mixed-fast", one(gen(genMixed, 3, 200<<10), "--fast=3"), nil},
	{"repeats-l19", one(gen(genRepeats, 16, 300<<10), "-19"), wantFeatures("crossBlock", "repeats")},
	{"runs-l3", one(gen(genRuns, 17, 100<<10), "-3"), wantFeatures("overlaps")},
	{"text-nocheck", one(gen(genText, 4, 50<<10), "-3", "--no-check"), func(t *testing.T, s *stats) {
		if s.checksums != 0 {
			t.Errorf("%d frames with a checksum; want none", s.checksums)
		}
	}},
	{"text-nosize", piped(gen(genText, 5, 50<<10), "-3", "--no-content-size"), wantFeatures("unknownSize", "windowDescriptor")},
	{"long27", piped(gen(genLong, 6, 9<<20), "-3", "--long=27"), func(t *testing.T, s *stats) {
		if s.maxOffset < 8<<20 {
			t.Errorf("longest match offset %d; want one beyond 8 MiB", s.maxOffset)
		}
	}},
	{"smallwindow", one(gen(genText, 7, 40<<10), "-19", "--zstd=wlog=10"), wantFeatures("windowDescriptor")},
	{"fcs1", one(gen(genText, 8, 100), "-3"), wantFeatures("singleSegment")},
	{"fcs2", one(gen(genText, 9, 1000), "-3"), wantFeatures("singleSegment")},
	{"fcs4", one(gen(genFloats, 10, 100000), "-3"), wantFeatures("singleSegment")},
	{"empty", one(empty, "-3"), nil},
	{"rle", one(constant, "-3"), wantFeatures("rleBlocks")},
	{"random", one(gen(genRandom, 11, 100<<10), "-3"), wantFeatures("rawBlocks")},
	{"rle-literals", one(rleLiterals, "-19"), wantFeatures("rleLiterals", "rawBlocks")},
	{"nibbles", one(nibbles, "-3"), wantFeatures("directWeights")},
	{"letters", one(letters, "-1"), wantFeatures("emptySequences")},
	{"records", one(records, "-3"), wantFeatures("LL-rle", "OF-rle")},
	{"multi", []part{
		{content: gen(genText, 12, 30<<10), args: []string{"-3"}},
		{content: gen(genFloats, 13, 30<<10), args: []string{"-19", "--no-check"}},
		{content: gen(genRandom, 14, 5<<10), args: []string{"-1"}, stdin: true},
	}, func(t *testing.T, s *stats) {
		if s.frames != 3 {
			t.Errorf("%d frames; want 3", s.frames)
		}
	}},
	{"skippable", []part{
		{skip: []byte("SituCraft skippable frame test")},
		{content: gen(genText, 15, 20<<10), args: []string{"-3"}},
		{skip: []byte{}},
	}, func(t *testing.T, s *stats) {
		if s.skippable != 2 || s.frames != 1 {
			t.Errorf("%d skippable and %d Zstandard frames; want 2 and 1", s.skippable, s.frames)
		}
	}},
}

// wantFeatures checks that the named feature counters are non-zero.
func wantFeatures(names ...string) func(t *testing.T, s *stats) {
	return func(t *testing.T, s *stats) {
		t.Helper()
		f := featureCounts(s)
		for _, n := range names {
			if f[n] == 0 {
				t.Errorf("feature %s not met", n)
			}
		}
	}
}

// featureCounts flattens the counters so that coverage can be checked by name.
func featureCounts(s *stats) map[string]int {
	f := map[string]int{
		"frames": s.frames, "skippable": s.skippable, "checksums": s.checksums,
		"rawBlocks": s.rawBlocks, "rleBlocks": s.rleBlocks, "compressedBlocks": s.compressedBlocks,
		"rawLiterals": s.rawLiterals, "rleLiterals": s.rleLiterals,
		"huffLiterals": s.huffLiterals, "treelessLiterals": s.treelessLiterals,
		"oneStream": s.oneStream, "fourStreams": s.fourStreams,
		"directWeights": s.directWeights, "fseWeights": s.fseWeights,
		"emptySequences": s.emptySequences, "sequences": s.sequences,
		"overlaps": s.overlaps, "crossBlock": s.crossBlock,
		"windowDescriptor": s.windowDescriptor, "singleSegment": s.singleSegment, "unknownSize": s.unknownSize,
	}
	f["repeats"] = s.repeats[0] + s.repeats[1] + s.repeats[2] + s.repeats[3]
	for k, field := range []string{"LL", "OF", "ML"} {
		for m, mode := range []string{"predefined", "rle", "fse", "repeat"} {
			f[field+"-"+mode] = s.modes[k][m]
		}
	}
	for k := range s.repeats {
		f["repeat"+string(rune('1'+k))] = s.repeats[k]
	}
	return f
}

type expectation struct {
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
	Zstd   string `json:"zstd"`
}

type expectations struct {
	Tool     string                 `json:"tool"`
	Fixtures map[string]expectation `json:"fixtures"`
}

func (f fixture) content() []byte {
	var out []byte
	for _, p := range f.parts {
		if p.content != nil {
			out = append(out, p.content()...)
		}
	}
	return out
}

func (f fixture) argsText() string {
	var parts []string
	for _, p := range f.parts {
		switch {
		case p.content == nil:
			parts = append(parts, "(skippable frame)")
		case p.stdin:
			parts = append(parts, strings.Join(p.args, " ")+" (from a pipe)")
		default:
			parts = append(parts, strings.Join(p.args, " "))
		}
	}
	return strings.Join(parts, " + ")
}

func TestFixtures(t *testing.T) {
	if *update {
		regenerateFixtures(t)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var exp expectations
	if err := json.Unmarshal(raw, &exp); err != nil {
		t.Fatal(err)
	}
	var total stats
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			e, ok := exp.Fixtures[f.name]
			if !ok {
				t.Fatalf("no expectation recorded for %s", f.name)
			}
			comp, err := os.ReadFile(filepath.Join("testdata", f.name+".zst"))
			if err != nil {
				t.Fatal(err)
			}
			got, st, err := decodeWithStats(comp, 0)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			total.add(st)
			sum := sha256.Sum256(got)
			if len(got) != e.Size || hex.EncodeToString(sum[:]) != e.SHA256 {
				t.Errorf("decoded %d bytes with SHA-256 %x; want %d bytes, %s", len(got), sum, e.Size, e.SHA256)
			}
			if want := f.content(); !bytes.Equal(got, want) {
				t.Errorf("decoded content differs from the regenerated input at byte %d", firstDiff(got, want))
			}
			// The public entry point agrees, appending to a non-empty dst.
			prefix := []byte("prefix")
			out, err := Decode(append([]byte(nil), prefix...), comp, 0)
			if err != nil || !bytes.Equal(out[:len(prefix)], prefix) || !bytes.Equal(out[len(prefix):], got) {
				t.Errorf("Decode with a prefix: %v", err)
			}
			if f.check != nil {
				f.check(t, st)
			}
		})
	}
	// Between them the fixtures exercise every part of the format the decoder implements.
	f := featureCounts(&total)
	for _, name := range []string{
		"frames", "skippable", "checksums", "rawBlocks", "rleBlocks", "compressedBlocks",
		"rawLiterals", "rleLiterals", "huffLiterals", "treelessLiterals", "oneStream", "fourStreams",
		"directWeights", "fseWeights", "emptySequences", "overlaps", "crossBlock",
		"windowDescriptor", "singleSegment", "unknownSize",
		"LL-predefined", "LL-rle", "LL-fse", "LL-repeat",
		"OF-predefined", "OF-rle", "OF-fse", "OF-repeat",
		"ML-predefined", "ML-rle", "ML-fse", "ML-repeat",
		"repeat1", "repeat2", "repeat3", "repeat4",
	} {
		if f[name] == 0 {
			t.Errorf("no fixture exercises %s", name)
		}
	}
	t.Logf("features met: %+v", total)
}

// regenerateFixtures writes the fixtures and expected.json with the zstd tool.
func regenerateFixtures(t *testing.T) {
	exe := zstdCLI(t)
	version, err := exec.Command(exe, "-V").Output()
	if err != nil {
		t.Fatal(err)
	}
	exp := expectations{Tool: strings.TrimSpace(string(version)), Fixtures: map[string]expectation{}}
	for _, f := range fixtures {
		var comp []byte
		for _, p := range f.parts {
			if p.content == nil {
				comp = binary.LittleEndian.AppendUint32(comp, skippableMagic|3)
				comp = binary.LittleEndian.AppendUint32(comp, uint32(len(p.skip)))
				comp = append(comp, p.skip...)
				continue
			}
			comp = append(comp, cliCompress(t, p.content(), p.args, p.stdin)...)
		}
		content := f.content()
		// The tool itself must agree before its output becomes a reference.
		cmd := exec.Command(exe, "-q", "-d", "-c")
		cmd.Stdin = bytes.NewReader(comp)
		back, err := cmd.Output()
		if err != nil || !bytes.Equal(back, content) {
			t.Fatalf("%s: zstd -d does not give back the input: %v", f.name, err)
		}
		if err := os.WriteFile(filepath.Join("testdata", f.name+".zst"), comp, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		exp.Fixtures[f.name] = expectation{Size: len(content), SHA256: hex.EncodeToString(sum[:]), Zstd: f.argsText()}
	}
	raw, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("testdata", "expected.json"), append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
