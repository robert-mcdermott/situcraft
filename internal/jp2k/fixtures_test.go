// Committed fixtures: small codestreams made by opj_compress from synthetic images, each with
// OpenJPEG's decode (testdata/fixtures.json, testdata/*.ref.gz), so that CI, which has no
// OpenJPEG, still checks this decoder against an independent one. The specs below are the script
// that made them: JP2K_REGEN=1 go test -run TestRegenerateFixtures regenerates them with the
// OpenJPEG tools on PATH, recording their version.

package jp2k

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type fixtureSpec struct {
	name    string
	pattern string
	w, h, n int // size and components
	prec    int
	signed  bool
	sub     []int // per-component subsampling (dx = dy); nil for none
	args    []string
	jp2     bool
	reduce  []int  // decodes to check besides full resolution
	pack    string // "ppm" or "ppt": packet headers moved into marker segments
}

// fixtureSpecs cover Part 1 broadly: precisions, signedness, component transforms, both wavelets,
// every progression order and POC, layers, tiles and tile-parts, offsets, precincts, code-block
// sizes and styles, SOP/EPH, PLT/TLM, ROI, subsampling, guard bits, level counts, tiny images, JP2,
// packed packet headers and resolution reduction. (Part 2 refusals are tested in part2_test.go.)
var fixtureSpecs = []fixtureSpec{
	{name: "gray8-lossless", pattern: "gradient", w: 64, h: 48, n: 1, prec: 8, reduce: []int{1, 3}},
	{name: "gray8-97", pattern: "blobs", w: 64, h: 48, n: 1, prec: 8, args: []string{"-I", "-r", "12"}, reduce: []int{2}},
	{name: "gray8-53-lossy", pattern: "blobs", w: 64, h: 48, n: 1, prec: 8, args: []string{"-r", "12"}},
	{name: "gray16-lossless", pattern: "blobs", w: 48, h: 40, n: 1, prec: 16, args: []string{"-n", "4"}},
	{name: "gray16-97", pattern: "blobs", w: 48, h: 40, n: 1, prec: 16, args: []string{"-I", "-r", "8", "-n", "4"}},
	{name: "xenium-profile", pattern: "blobs", w: 256, h: 256, n: 1, prec: 16, jp2: true, reduce: []int{1, 5},
		args: []string{"-I", "-r", "6", "-p", "RPCL", "-c", "[256,256]", "-b", "64,64", "-n", "6"}},
	{name: "rgb8-rct", pattern: "edges", w: 48, h: 40, n: 3, prec: 8, args: []string{"-n", "4"}},
	{name: "rgb8-ict", pattern: "blobs", w: 48, h: 40, n: 3, prec: 8, args: []string{"-I", "-r", "15", "-n", "4"}},
	{name: "rgb8-no-mct", pattern: "noise", w: 32, h: 24, n: 3, prec: 8, args: []string{"-mct", "0", "-n", "3"}},
	{name: "gray12", pattern: "gradient", w: 40, h: 30, n: 1, prec: 12, args: []string{"-n", "3"}},
	{name: "gray20-lossless", pattern: "blobs", w: 40, h: 30, n: 1, prec: 20, args: []string{"-n", "3"}},
	{name: "gray24-97", pattern: "blobs", w: 40, h: 30, n: 1, prec: 24, args: []string{"-I", "-r", "4", "-n", "3"}},
	{name: "signed20-97", pattern: "gradient", w: 30, h: 20, n: 1, prec: 20, signed: true, args: []string{"-I", "-n", "2"}},
	{name: "gray1", pattern: "checker", w: 33, h: 17, n: 1, prec: 1, args: []string{"-n", "2"}},
	{name: "signed12-97", pattern: "blobs", w: 40, h: 36, n: 1, prec: 12, signed: true, args: []string{"-I", "-r", "5", "-n", "3"}},
	{name: "signed8-lossless", pattern: "noise", w: 30, h: 26, n: 2, prec: 8, signed: true, args: []string{"-n", "3"}},
	{name: "prog-LRCP", pattern: "blobs", w: 56, h: 44, n: 3, prec: 8, reduce: []int{1},
		args: []string{"-p", "LRCP", "-r", "30,10,3", "-c", "[32,32],[16,16]", "-n", "4"}},
	{name: "prog-RLCP", pattern: "blobs", w: 56, h: 44, n: 3, prec: 8,
		args: []string{"-p", "RLCP", "-r", "30,10,3", "-c", "[32,32],[16,16]", "-n", "4"}},
	{name: "prog-RPCL", pattern: "blobs", w: 56, h: 44, n: 3, prec: 8, reduce: []int{2},
		args: []string{"-p", "RPCL", "-r", "30,10,3", "-c", "[32,32],[16,16]", "-n", "4"}},
	{name: "prog-PCRL", pattern: "blobs", w: 56, h: 44, n: 3, prec: 8,
		args: []string{"-p", "PCRL", "-r", "30,10,3", "-c", "[32,32],[16,16]", "-n", "4"}},
	{name: "prog-CPRL", pattern: "blobs", w: 56, h: 44, n: 3, prec: 8,
		args: []string{"-p", "CPRL", "-r", "30,10,3", "-c", "[32,32],[16,16]", "-n", "4"}},
	{name: "prog-POC", pattern: "edges", w: 56, h: 44, n: 3, prec: 8,
		args: []string{"-r", "20,5", "-n", "4", "-POC", "T0=0,0,1,2,3,CPRL/T0=0,0,2,4,3,RLCP"}},
	{name: "tiles", pattern: "gradient", w: 61, h: 47, n: 1, prec: 8, args: []string{"-t", "24,20", "-n", "3"}},
	{name: "tiles-offsets", pattern: "edges", w: 61, h: 47, n: 3, prec: 8, reduce: []int{1},
		args: []string{"-t", "24,20", "-d", "5,3", "-T", "2,1", "-n", "3"}},
	{name: "image-offset-97", pattern: "blobs", w: 50, h: 41, n: 1, prec: 8, args: []string{"-I", "-r", "8", "-d", "7,11", "-n", "4"}},
	{name: "tiles-97-16bit", pattern: "blobs", w: 80, h: 70, n: 1, prec: 16, args: []string{"-I", "-r", "10", "-t", "32,32", "-n", "3"}},
	{name: "tileparts-R", pattern: "blobs", w: 64, h: 48, n: 3, prec: 8, args: []string{"-r", "20,5", "-t", "32,32", "-TP", "R", "-n", "3"}},
	{name: "tileparts-L", pattern: "blobs", w: 64, h: 48, n: 3, prec: 8, args: []string{"-r", "20,5", "-t", "32,32", "-TP", "L", "-n", "3"}},
	{name: "tileparts-C", pattern: "blobs", w: 64, h: 48, n: 3, prec: 8, args: []string{"-r", "20,5", "-t", "32,32", "-TP", "C", "-n", "3"}},
	{name: "cblk-4x4", pattern: "noise", w: 30, h: 22, n: 1, prec: 8, args: []string{"-b", "4,4", "-n", "3"}},
	{name: "cblk-8x64", pattern: "blobs", w: 70, h: 90, n: 1, prec: 8, args: []string{"-b", "8,64", "-r", "6", "-n", "3"}},
	{name: "cblk-64x4", pattern: "edges", w: 90, h: 50, n: 1, prec: 8, args: []string{"-b", "64,4", "-I", "-r", "6", "-n", "3"}},
	{name: "precincts-small", pattern: "blobs", w: 60, h: 52, n: 1, prec: 8, args: []string{"-c", "[8,8],[4,8],[4,4]", "-b", "4,4", "-r", "8,2", "-n", "3"}},
	{name: "mode-bypass", pattern: "noise", w: 48, h: 40, n: 1, prec: 12, args: []string{"-M", "1", "-r", "6,2,1", "-n", "3"}},
	{name: "mode-reset", pattern: "blobs", w: 48, h: 40, n: 1, prec: 8, args: []string{"-M", "2", "-r", "6,2", "-n", "3"}},
	{name: "mode-termall", pattern: "blobs", w: 48, h: 40, n: 1, prec: 8, args: []string{"-M", "4", "-r", "6,2", "-n", "3"}},
	{name: "mode-vcausal", pattern: "edges", w: 48, h: 40, n: 1, prec: 8, args: []string{"-M", "8", "-r", "6,2", "-n", "3"}},
	{name: "mode-pterm", pattern: "blobs", w: 48, h: 40, n: 1, prec: 8, args: []string{"-M", "16", "-r", "6,2", "-n", "3"}},
	{name: "mode-segsym", pattern: "blobs", w: 48, h: 40, n: 1, prec: 8, args: []string{"-M", "32", "-r", "6,2", "-n", "3"}},
	{name: "mode-all-97", pattern: "blobs", w: 48, h: 40, n: 3, prec: 8, args: []string{"-M", "63", "-I", "-r", "8,3", "-n", "3"}},
	{name: "mode-bypass-termall", pattern: "noise", w: 48, h: 40, n: 1, prec: 10, args: []string{"-M", "5", "-r", "6,2,1", "-n", "3"}},
	{name: "sop", pattern: "blobs", w: 48, h: 40, n: 1, prec: 8, args: []string{"-SOP", "-r", "10,3", "-n", "3"}},
	{name: "eph", pattern: "blobs", w: 48, h: 40, n: 1, prec: 8, args: []string{"-EPH", "-r", "10,3", "-n", "3"}},
	{name: "sop-eph-tiles", pattern: "edges", w: 60, h: 44, n: 3, prec: 8, args: []string{"-SOP", "-EPH", "-t", "32,24", "-r", "10,3", "-n", "3"}},
	{name: "plt-tlm", pattern: "blobs", w: 60, h: 44, n: 1, prec: 8, args: []string{"-PLT", "-TLM", "-t", "32,24", "-TP", "R", "-n", "3"}},
	{name: "roi-lossless", pattern: "gradient", w: 48, h: 40, n: 1, prec: 8, args: []string{"-ROI", "c=0,U=5", "-n", "3"}},
	{name: "roi-97", pattern: "blobs", w: 48, h: 40, n: 3, prec: 8, args: []string{"-ROI", "c=1,U=3", "-I", "-r", "10", "-n", "3"}},
	{name: "subsampled", pattern: "blobs", w: 48, h: 40, n: 3, prec: 8, sub: []int{1, 1, 2}, reduce: []int{1}, args: []string{"-n", "3"}},
	{name: "subsampled-97", pattern: "blobs", w: 48, h: 40, n: 4, prec: 8, sub: []int{1, 2, 2, 1}, args: []string{"-I", "-r", "8", "-n", "3"}},
	{name: "guard1", pattern: "blobs", w: 48, h: 40, n: 1, prec: 16, args: []string{"-GuardBits", "1", "-I", "-r", "10", "-n", "3"}},
	{name: "guard4", pattern: "blobs", w: 48, h: 40, n: 1, prec: 16, args: []string{"-GuardBits", "4", "-n", "3"}},
	{name: "levels0-97", pattern: "blobs", w: 40, h: 30, n: 1, prec: 8, args: []string{"-n", "1", "-I", "-r", "5"}},
	{name: "levels7", pattern: "blobs", w: 160, h: 130, n: 1, prec: 8, reduce: []int{4, 7}, args: []string{"-n", "8", "-I", "-r", "40"}},
	{name: "tiny-1x1", pattern: "gradient", w: 1, h: 1, n: 1, prec: 8, args: []string{"-n", "1"}},
	{name: "tiny-3x2-rgb", pattern: "noise", w: 3, h: 2, n: 3, prec: 8, args: []string{"-n", "2"}},
	{name: "tiny-5x1-97", pattern: "gradient", w: 5, h: 1, n: 1, prec: 16, args: []string{"-n", "1", "-I"}},
	{name: "jp2-gray16", pattern: "blobs", w: 40, h: 32, n: 1, prec: 16, jp2: true, args: []string{"-I", "-r", "6", "-n", "3"}},
	{name: "jp2-rgb", pattern: "edges", w: 40, h: 32, n: 3, prec: 8, jp2: true, args: []string{"-n", "3"}},
	{name: "ppm", pattern: "blobs", w: 64, h: 48, n: 3, prec: 8, pack: "ppm", args: []string{"-I", "-r", "10,4", "-t", "32,24", "-n", "3"}},
	{name: "ppt", pattern: "edges", w: 64, h: 48, n: 1, prec: 8, pack: "ppt", args: []string{"-r", "10,4,1", "-EPH", "-n", "3"}},
}

// fixtureEntry is one checked decode in testdata/fixtures.json.
type fixtureEntry struct {
	Name         string `json:"name"`
	File         string `json:"file"`
	Image        string `json:"image"`
	Encoder      string `json:"encoder,omitempty"`
	Reduce       int    `json:"reduce,omitempty"`
	Irreversible bool   `json:"irreversible,omitempty"`
	Prec         int    `json:"prec,omitempty"`
	SHA256       string `json:"sha256,omitempty"` // of OpenJPEG's decode, serialized by refBytes
	Ref          string `json:"ref,omitempty"`    // OpenJPEG's decode (9/7), refBytes gzipped
}

type fixtureManifest struct {
	Generator string         `json:"generator"`
	OpenJPEG  string         `json:"openjpeg"`
	Entries   []fixtureEntry `json:"entries"`
}

const fixtureDir = "testdata"

// refBytes serializes decoded components: per component, width, height and precision as uint32
// and signedness as a byte, then the samples in 1, 2 or 4 bytes, little-endian.
func refBytes(comps []refComp) []byte {
	var b []byte
	for _, c := range comps {
		b = binary.LittleEndian.AppendUint32(b, uint32(c.w))
		b = binary.LittleEndian.AppendUint32(b, uint32(c.h))
		b = binary.LittleEndian.AppendUint32(b, uint32(c.prec))
		sg := byte(0)
		if c.signed {
			sg = 1
		}
		b = append(b, sg)
		for _, v := range c.data {
			switch {
			case c.prec <= 8:
				b = append(b, byte(v))
			case c.prec <= 16:
				b = binary.LittleEndian.AppendUint16(b, uint16(v))
			default:
				b = binary.LittleEndian.AppendUint32(b, uint32(v))
			}
		}
	}
	return b
}

func parseRefBytes(b []byte) ([]refComp, error) {
	var comps []refComp
	for len(b) > 0 {
		if len(b) < 13 {
			return nil, errors.New("short reference")
		}
		c := refComp{w: int(binary.LittleEndian.Uint32(b)), h: int(binary.LittleEndian.Uint32(b[4:])),
			prec: int(binary.LittleEndian.Uint32(b[8:])), signed: b[12] == 1}
		b = b[13:]
		size := 4
		if c.prec <= 8 {
			size = 1
		} else if c.prec <= 16 {
			size = 2
		}
		n := c.w * c.h
		if len(b) < n*size {
			return nil, errors.New("short reference")
		}
		c.data = make([]int32, n)
		for i := range c.data {
			switch size {
			case 1:
				if c.signed {
					c.data[i] = int32(int8(b[i]))
				} else {
					c.data[i] = int32(b[i])
				}
			case 2:
				v := binary.LittleEndian.Uint16(b[2*i:])
				if c.signed {
					c.data[i] = int32(int16(v))
				} else {
					c.data[i] = int32(v)
				}
			case 4:
				c.data[i] = int32(binary.LittleEndian.Uint32(b[4*i:]))
			}
		}
		b = b[n*size:]
		comps = append(comps, c)
	}
	return comps, nil
}

// imageRefComps describes a decoded image in refComp form.
func imageRefComps(img *Image) []refComp {
	var out []refComp
	for _, c := range img.Components {
		out = append(out, refComp{w: c.Width, h: c.Height, prec: c.Precision, signed: c.Signed, data: c.Data})
	}
	return out
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// TestFixtures decodes every committed fixture and checks it against OpenJPEG's recorded decode:
// a 5/3 decode must equal it exactly; a 9/7 decode must equal it exactly with OpenJPEG's
// high-pass constant and lie within opjTolerance of it with the standard's.
func TestFixtures(t *testing.T) {
	m, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) < len(fixtureSpecs) {
		t.Fatalf("manifest has %d entries for %d specs; regenerate with JP2K_REGEN=1", len(m.Entries), len(fixtureSpecs))
	}
	for _, e := range m.Entries {
		data, err := os.ReadFile(filepath.Join(fixtureDir, e.File))
		if err != nil {
			t.Fatal(err)
		}
		label := fmt.Sprintf("%s (reduce %d)", e.Name, e.Reduce)
		img, err := Decode(data, Options{Reduce: e.Reduce})
		if err != nil {
			t.Errorf("%s: %v", label, err)
			continue
		}
		if !e.Irreversible {
			if got := sha(refBytes(imageRefComps(img))); got != e.SHA256 {
				t.Errorf("%s: decode hash %s, OpenJPEG's %s", label, got, e.SHA256)
			}
			continue
		}
		emu, err := Decode(data, Options{Reduce: e.Reduce, opjHighpass: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := sha(refBytes(imageRefComps(emu))); got != e.SHA256 {
			t.Errorf("%s: decode with OpenJPEG's high-pass constant has hash %s, OpenJPEG's %s", label, got, e.SHA256)
		}
		ref, err := loadRef(e.Ref)
		if err != nil {
			t.Fatal(err)
		}
		s, err := compareRef(img, ref)
		if err != nil {
			t.Errorf("%s: %v", label, err)
		} else if s.maxAbs > opjTolerance(e.Prec) {
			t.Errorf("%s: %v", label, s)
		}
	}
}

func loadManifest() (*fixtureManifest, error) {
	b, err := os.ReadFile(filepath.Join(fixtureDir, "fixtures.json"))
	if err != nil {
		return nil, err
	}
	var m fixtureManifest
	return &m, json.Unmarshal(b, &m)
}

func loadRef(name string) ([]refComp, error) {
	f, err := os.Open(filepath.Join(fixtureDir, name))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	return parseRefBytes(b)
}

// fixtureFiles lists the codestream files of the committed fixtures (for the fuzz corpus).
func fixtureFiles(t testing.TB) [][]byte {
	m, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var out [][]byte
	for _, e := range m.Entries {
		if seen[e.File] {
			continue
		}
		seen[e.File] = true
		b, err := os.ReadFile(filepath.Join(fixtureDir, e.File))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

// TestRegenerateFixtures rewrites testdata from fixtureSpecs with the OpenJPEG tools. It runs only
// with JP2K_REGEN=1.
func TestRegenerateFixtures(t *testing.T) {
	if os.Getenv("JP2K_REGEN") != "1" {
		t.Skip("set JP2K_REGEN=1 to regenerate the fixtures")
	}
	if opjCompress == "" || opjDecompress == "" {
		t.Fatal("opj_compress and opj_decompress must be on PATH")
	}
	version, _ := exec.Command(opjDecompress, "-h").CombinedOutput()
	m := fixtureManifest{
		Generator: "internal/jp2k/fixtures_test.go TestRegenerateFixtures (JP2K_REGEN=1)",
		OpenJPEG:  regexp.MustCompile(`openjp2 library v[0-9]+(\.[0-9]+)*`).FindString(string(version)),
	}
	old, _ := filepath.Glob(filepath.Join(fixtureDir, "*.ref.gz"))
	for _, f := range old {
		os.Remove(f)
	}
	for i, s := range fixtureSpecs {
		sub := s.sub
		if sub == nil {
			sub = make([]int, s.n)
			for k := range sub {
				sub[k] = 1
			}
		}
		im := makeImage(s.pattern, s.w, s.h, s.n, s.prec, s.signed, sub, sub, uint64(1000+i))
		data, err := opjEncode(t, im, s.args, s.jp2)
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if s.pack != "" {
			if data, err = packHeaders(data, s.pack == "ppm"); err != nil {
				t.Fatalf("%s: %v", s.name, err)
			}
		}
		file := s.name + ".j2k"
		if s.jp2 {
			file = s.name + ".jp2"
		}
		if err := os.WriteFile(filepath.Join(fixtureDir, file), data, 0o644); err != nil {
			t.Fatal(err)
		}
		desc := fmt.Sprintf("%s %dx%d, %d components, %d-bit", s.pattern, s.w, s.h, s.n, s.prec)
		if s.signed {
			desc += " signed"
		}
		if s.sub != nil {
			desc += fmt.Sprintf(", subsampling %v", s.sub)
		}
		if s.pack != "" {
			desc += ", packet headers in " + strings.ToUpper(s.pack)
		}
		base := fixtureEntry{Name: s.name, File: file, Image: desc, Encoder: strings.Join(s.args, " "), Prec: s.prec}
		for _, r := range append([]int{0}, s.reduce...) {
			e := base
			e.Reduce = r
			e.Irreversible = slicesContain(s.args, "-I")
			ref, err := opjDecode(t, data, r)
			if err != nil {
				t.Fatalf("%s reduce %d: %v", s.name, r, err)
			}
			rb := refBytes(ref)
			e.SHA256 = sha(rb)
			if e.Irreversible {
				e.Ref = fmt.Sprintf("%s-r%d.ref.gz", s.name, r)
				var buf bytes.Buffer
				zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
				zw.Write(rb)
				zw.Close()
				if err := os.WriteFile(filepath.Join(fixtureDir, e.Ref), buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			m.Entries = append(m.Entries, e)
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "fixtures.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
