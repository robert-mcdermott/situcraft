package compare

// A comparison's files, an artifact of kind "comparison" (internal/artifacts):
//
//	comparison.json  what was compared and how: the two segmentations, the parameters, every
//	                 measure, and the regions
//	groups.bin "SCCG"  each difference: its class, its cells in A and in B (indices), the IoU, the
//	                 transcripts' Jaccard and the boundary displacement of one cell in each, the
//	                 gene transcripts whose cell changed, and where it lies (µm)
//
// groups.bin has SituCraft's binary envelope (internal/segment: magic, version, length, zlib).

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
)

// Files of a comparison, by role.
const (
	FileDescription = "comparison.json"
	FileGroups      = "groups.bin"
)

// Roles name each file's role in an artifact's manifest.
var Roles = map[string]string{FileDescription: "description", FileGroups: "differences"}

// MethodVersion is the version of the comparison's method, recorded with it.
const MethodVersion = "1"

// Side names a compared segmentation.
type Side struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Description is comparison.json.
type Description struct {
	Format   string     `json:"format"`
	Version  int        `json:"version"`
	Method   string     `json:"method"`
	A        Side       `json:"a"`
	B        Side       `json:"b"`
	MetricsA SegMetrics `json:"metricsA"`
	MetricsB SegMetrics `json:"metricsB"`
	*Result
}

var classIndex = func() map[string]byte {
	m := map[string]byte{}
	for i, c := range Classes {
		m[c] = byte(i)
	}
	return m
}()

// Write writes a comparison's files into dir.
func Write(dir string, a, b Side, r *Result) error {
	d := Description{Format: "situcraft-comparison", Version: 1, Method: "SituCraft segmentation comparison " + MethodVersion, A: a, B: b, MetricsA: r.A, MetricsB: r.B, Result: r}
	raw, err := json.MarshalIndent(d, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileDescription), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	var buf segment.Buffer
	buf.U32(uint32(len(r.Groups)))
	for _, g := range r.Groups {
		buf.Byte(classIndex[g.Class])
		buf.U32(uint32(len(g.A)))
		buf.U32(uint32(len(g.B)))
		for _, c := range g.A {
			buf.U32(uint32(c))
		}
		for _, c := range g.B {
			buf.U32(uint32(c))
		}
		for _, v := range []float32{g.IoU, g.Jaccard, g.Displacement} {
			buf.F32(v)
		}
		buf.U32(g.Changed)
		buf.F32(g.X)
		buf.F32(g.Y)
	}
	return segment.WriteBinary(filepath.Join(dir, FileGroups), "SCCG", &buf)
}

// ReadDescription reads comparison.json.
func ReadDescription(dir string) (*Description, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileDescription))
	if err != nil {
		return nil, err
	}
	d := &Description{Result: &Result{}}
	if err := json.Unmarshal(raw, d); err != nil {
		return nil, fmt.Errorf("%s: %w", FileDescription, err)
	}
	if d.Format != "situcraft-comparison" || d.Version > 1 {
		return nil, fmt.Errorf("%s is not a comparison SituCraft reads (%s, version %d)", FileDescription, d.Format, d.Version)
	}
	d.Result.A, d.Result.B = d.MetricsA, d.MetricsB
	return d, nil
}

// ReadGroups reads groups.bin.
func ReadGroups(dir string) ([]Group, error) {
	r, err := segment.ReadBinary(filepath.Join(dir, FileGroups), "SCCG")
	if err != nil {
		return nil, err
	}
	n := r.Count(21)
	out := make([]Group, 0, n)
	for i := 0; i < n; i++ {
		k := r.Byte()
		if int(k) >= len(Classes) {
			return nil, fmt.Errorf("%s: difference %d has class %d", FileGroups, i, k)
		}
		g := Group{Class: Classes[k]}
		na, nb := r.Count(4), r.Count(4)
		for _, v := range r.U32s(na) {
			g.A = append(g.A, int32(v))
		}
		for _, v := range r.U32s(nb) {
			g.B = append(g.B, int32(v))
		}
		g.IoU, g.Jaccard, g.Displacement = r.F32(), r.F32(), r.F32()
		g.Changed = r.U32()
		g.X, g.Y = r.F32(), r.F32()
		out = append(out, g)
	}
	if err := r.Done(FileGroups); err != nil {
		return nil, err
	}
	return out, nil
}

// Summary is what the check reports of a comparison (the artifact's summary).
type Summary struct {
	A              Side           `json:"a"`
	B              Side           `json:"b"`
	Classes        map[string]int `json:"classes"`
	PixelAgreement float64        `json:"pixelAgreement"`
	Changed        float64        `json:"changedFraction"`
	IoU            Float          `json:"medianIoU"`
	CoexpressionA  Float          `json:"spuriousCoexpressionA"`
	CoexpressionB  Float          `json:"spuriousCoexpressionB"`
	MECRA          Float          `json:"mecrA"`
	MECRB          Float          `json:"mecrB"`
}

// Check examines a staged comparison: its files read, every cell of each segmentation is in exactly
// one difference whose class fits its shape, and the counts agree with the description.
func Check(dir string, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(code, format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: code, Message: fmt.Sprintf(format, args...)})
	}
	d, err := ReadDescription(dir)
	if err != nil {
		fatal("comparison.unreadable", "%v", err)
		return out
	}
	groups, err := ReadGroups(dir)
	if err != nil {
		fatal("comparison.unreadable", "%v", err)
		return out
	}
	seenA, seenB := make([]int, d.MetricsA.Cells), make([]int, d.MetricsB.Cells)
	classes := map[string]int{}
	bad, outside := 0, 0
	for _, g := range groups {
		for _, c := range g.A {
			if c < 0 || int(c) >= len(seenA) {
				outside++
				continue
			}
			seenA[c]++
		}
		for _, c := range g.B {
			if c < 0 || int(c) >= len(seenB) {
				outside++
				continue
			}
			seenB[c]++
		}
		classes[g.Class]++
		var fits bool
		switch g.Class {
		case OneToOne, Expanded, Contracted, AssignmentOnly:
			fits = len(g.A) == 1 && len(g.B) == 1 || g.Class == AssignmentOnly && len(g.A) > 0 && len(g.B) > 0
		case Split:
			fits = len(g.A) == 1 && len(g.B) > 1
		case Merged:
			fits = len(g.A) > 1 && len(g.B) == 1
		case Rearranged:
			fits = len(g.A) > 1 && len(g.B) > 1
		case OnlyA:
			fits = len(g.A) == 1 && len(g.B) == 0
		case OnlyB:
			fits = len(g.A) == 0 && len(g.B) == 1
		}
		if !fits || math.IsNaN(float64(g.IoU)) || g.IoU < 0 || g.IoU > 1 {
			bad++
		}
	}
	if outside > 0 {
		fatal("groups.unknown-cell", "%d differences name cells the segmentations do not have.", outside)
	}
	if bad > 0 {
		fatal("groups.class", "%d differences have a class their cells do not fit, or an IoU outside 0–1.", bad)
	}
	missing, twice := 0, 0
	for _, seen := range [][]int{seenA, seenB} {
		for _, n := range seen {
			if n == 0 {
				missing++
			}
			if n > 1 {
				twice++
			}
		}
	}
	if missing > 0 || twice > 0 {
		fatal("groups.cells", "%d cells are in no difference and %d in several: every cell is in one.", missing, twice)
	}
	for _, c := range Classes {
		if classes[c] != d.Classes[c] {
			fatal("groups.counts", "The description counts %d differences %s; the file holds %d.", d.Classes[c], c, classes[c])
		}
	}
	for _, c := range []Changes{d.Changes, d.ChangesQ20} {
		if c.Changed != c.Gained+c.Lost+c.Moved || c.Changed > c.Transcripts {
			fatal("changes.counts", "The changed transcripts (%d) are not those gained, lost and moved.", c.Changed)
		}
	}
	g := d.Regions
	n := g.Cols * g.Rows
	for _, l := range []int{len(g.CellsA), len(g.CellsB), len(g.Transcripts), len(g.Changed), len(g.Pixels), len(g.MeanIoU)} {
		if l != n {
			fatal("regions.shape", "The regions are not %d × %d.", g.Cols, g.Rows)
			break
		}
	}
	s := Summary{A: d.A, B: d.B, Classes: d.Classes, PixelAgreement: d.PixelAgreement, Changed: d.ChangesQ20.Fraction, IoU: d.IoU.Median,
		CoexpressionA: d.MetricsA.Coexpression, CoexpressionB: d.MetricsB.Coexpression, MECRA: d.MetricsA.MECR, MECRB: d.MetricsB.MECR}
	m.Summary, _ = json.Marshal(s)
	return out
}
