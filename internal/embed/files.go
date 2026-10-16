package embed

// A map's files, an artifact of kind "embedding" (internal/artifacts):
//
//	embedding.json   what was laid out and how: the clustering (or the instrument's analysis), the
//	                 parameters and fitted curve, the start, and the map's quality
//	embedding.bin "SCEB"  the cells (indices in the segmentation's cells), each one's place on the
//	                 map, and its reliability score
//
// embedding.bin has SituCraft's binary envelope (internal/segment: magic, version, length, zlib).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"situcraft/internal/artifacts"
	"situcraft/internal/cluster"
	"situcraft/internal/graph"
	"situcraft/internal/segment"
)

// MethodVersion is the version of the method, recorded with every map.
const MethodVersion = "1"

// Files of a map, by role.
const (
	FileDescription = "embedding.json"
	FileMap         = "embedding.bin"
)

// Files lists them in order; Roles names each one's role in a manifest.
var (
	Files = []string{FileDescription, FileMap}
	Roles = map[string]string{FileDescription: "description", FileMap: "map"}
)

// Source says where a map came from when SituCraft did not lay it out (the instrument's analysis).
type Source struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	File    string `json:"file"`
	// Space is what its quality is judged against.
	Space string `json:"space"`
}

// Fitted is how a map was laid out: the curve, the epochs, the edges, the start, and the optimizer
// when not on one thread (more than graph.ExactLimit cells).
type Fitted struct {
	A         float64 `json:"a"`
	B         float64 `json:"b"`
	Epochs    int     `json:"epochs"`
	Edges     int     `json:"edges"`
	Init      string  `json:"init"`
	Optimizer string  `json:"optimizer,omitempty"`
}

// Description is embedding.json.
type Description struct {
	Format       string       `json:"format"`
	Version      int          `json:"version"`
	Method       string       `json:"method"`
	Segmentation cluster.Side `json:"segmentation"`
	Clustering   cluster.Side `json:"clustering"`
	Source       *Source      `json:"source,omitempty"`
	Parameters   *Params      `json:"parameters,omitempty"`
	Fitted       *Fitted      `json:"fitted,omitempty"`
	Cells        int          `json:"cells"`
	Quality      struct {
		K               int     `json:"k"`
		KWide           int     `json:"kWide"`
		Trustworthiness float64 `json:"trustworthiness"`
		Queries         int     `json:"queries"`
		Precision       float64 `json:"precision"`
		Recall          float64 `json:"recall"`
		// Search says how the space's wider neighborhoods were found when not exactly.
		Search *graph.Approximate `json:"search,omitempty"`
	} `json:"quality"`
	Seconds map[string]float64 `json:"seconds,omitempty"`
}

// Map is a map's cells, places and reliability.
type Map struct {
	Cells []uint32
	XY    []float32 // n × 2
	Score []float32
}

// Result is a map made, with its description.
type Result struct {
	Description Description
	Map         Map
}

// Run lays out a clustering's cells (its folder, internal/cluster; clu names it) and judges the map
// against the clustering's principal components.
func Run(ctx context.Context, dir string, clu cluster.Side, p Params, workers int, progress func(float64, string)) (*Result, error) {
	say := func(f float64, m string) {
		if progress != nil {
			progress(f, m)
		}
	}
	say(0, "Reading the clustering")
	cd, err := cluster.ReadDescription(dir)
	if err != nil {
		return nil, err
	}
	cells, _, err := cluster.ReadCells(dir)
	if err != nil {
		return nil, err
	}
	nb, err := cluster.ReadNeighbors(dir)
	if err != nil {
		return nil, err
	}
	pc, err := cluster.ReadPCA(dir)
	if err != nil {
		return nil, err
	}
	n := len(cells)
	if nb.N != n || len(pc.Scores) != n*pc.Components || pc.Components < 2 {
		return nil, errors.New("the clustering's files disagree in size")
	}
	g, err := graph.Fuzzy(nb)
	if err != nil {
		return nil, err
	}
	high := make([]float64, len(pc.Scores))
	for i, v := range pc.Scores {
		high[i] = float64(v)
	}
	fallback := make([]float64, 2*n)
	for i := 0; i < n; i++ {
		fallback[2*i], fallback[2*i+1] = high[i*pc.Components], high[i*pc.Components+1]
	}
	clock := time.Now()
	seconds := map[string]float64{}
	lap := func(name string) {
		seconds[name] = math.Round(time.Since(clock).Seconds()*1000) / 1000
		clock = time.Now()
	}
	say(0.05, "Laying the cells out")
	xy, info, err := Layout(ctx, g, fallback, p, workers, func(f float64) { say(0.05+0.75*f, "Laying the cells out") })
	if err != nil {
		return nil, err
	}
	lap("layout")
	say(0.8, "Judging the map")
	q, err := Assess(ctx, high, pc.Components, xy, n, workers, nb)
	if err != nil {
		return nil, err
	}
	lap("quality")
	r := &Result{}
	d := &r.Description
	d.Format, d.Version, d.Method = "situcraft-embedding", 1, "SituCraft UMAP "+MethodVersion
	d.Segmentation = cd.Segmentation
	pp := p
	d.Parameters = &pp
	d.Fitted = &Fitted{info.A, info.B, info.Epochs, info.Edges, info.Init, info.Optimizer}
	d.Cells = n
	d.Clustering = clu
	d.Seconds = seconds
	setQuality(d, q)
	r.Map = Map{Cells: cells, XY: make([]float32, 2*n), Score: q.Score}
	for i, v := range xy {
		r.Map.XY[i] = float32(v)
	}
	say(1, "The map is ready")
	return r, nil
}

func setQuality(d *Description, q *Quality) {
	d.Quality.K, d.Quality.KWide = q.K, q.KWide
	d.Quality.Trustworthiness, d.Quality.Queries = q.Trustworthiness, q.Queries
	d.Quality.Precision, d.Quality.Recall, d.Quality.Search = q.Precision, q.Recall, q.Search
}

// FromSource makes a map from places another tool computed (the instrument's), judged against the
// space it gives (its principal components): cells and xy (n × 2) and high (n × dim) in the same
// order.
func FromSource(ctx context.Context, seg, clu cluster.Side, src Source, cells []uint32, xy []float64, high []float64, dim, workers int) (*Result, error) {
	n := len(cells)
	if len(xy) != 2*n || len(high) != dim*n || n < 4 {
		return nil, errors.New("the map's places and space do not name the same cells")
	}
	q, err := Assess(ctx, high, dim, xy, n, workers)
	if err != nil {
		return nil, err
	}
	r := &Result{}
	d := &r.Description
	d.Format, d.Version, d.Method = "situcraft-embedding", 1, src.Tool+" UMAP"
	d.Segmentation, d.Clustering, d.Source, d.Cells = seg, clu, &src, n
	setQuality(d, q)
	r.Map = Map{Cells: cells, XY: make([]float32, 2*n), Score: q.Score}
	for i, v := range xy {
		r.Map.XY[i] = float32(v)
	}
	return r, nil
}

// Write writes a map's files into dir.
func Write(dir string, r *Result) error {
	raw, err := json.MarshalIndent(r.Description, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileDescription), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	var b segment.Buffer
	b.U32(uint32(len(r.Map.Cells)))
	b.U32s(r.Map.Cells)
	b.F32s(r.Map.XY)
	b.F32s(r.Map.Score)
	return segment.WriteBinary(filepath.Join(dir, FileMap), "SCEB", &b)
}

// ReadDescription reads embedding.json.
func ReadDescription(dir string) (*Description, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileDescription))
	if err != nil {
		return nil, err
	}
	var d Description
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", FileDescription, err)
	}
	if d.Format != "situcraft-embedding" || d.Version > 1 {
		return nil, fmt.Errorf("%s is not a map SituCraft reads (%s, version %d)", FileDescription, d.Format, d.Version)
	}
	return &d, nil
}

// ReadMap reads embedding.bin.
func ReadMap(dir string) (*Map, error) {
	r, err := segment.ReadBinary(filepath.Join(dir, FileMap), "SCEB")
	if err != nil {
		return nil, err
	}
	n := r.Count(16)
	m := &Map{Cells: r.U32s(n), XY: r.F32s(2 * n), Score: r.F32s(n)}
	return m, r.Done(FileMap)
}

// Digest is the SHA-256 of a map's content: its places and scores, and its description without
// the run's timings.
func Digest(dir string) (string, error) {
	d, err := ReadDescription(dir)
	if err != nil {
		return "", err
	}
	d.Seconds = nil
	raw, _ := json.Marshal(d)
	h := sha256.New()
	h.Write(raw)
	b, err := os.ReadFile(filepath.Join(dir, FileMap))
	if err != nil {
		return "", err
	}
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Summary is what the check reports of a map.
type Summary struct {
	Clustering      cluster.Side `json:"clustering"`
	Cells           int          `json:"cells"`
	Trustworthiness float64      `json:"trustworthiness"`
	Precision       float64      `json:"precision"`
	Recall          float64      `json:"recall"`
	Source          string       `json:"source,omitempty"`
	// Search: how the wider neighborhoods were found when not exactly.
	Search *graph.Approximate `json:"search,omitempty"`
}

// Check examines a staged map: its files read and agree; its cells are in order, its places and
// scores finite, the scores between 0 and 1.
func Check(dir string, m *artifacts.Manifest) []artifacts.Finding {
	var out []artifacts.Finding
	fatal := func(code, format string, args ...any) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: code, Message: fmt.Sprintf(format, args...)})
	}
	d, err := ReadDescription(dir)
	if err != nil {
		fatal("embedding.unreadable", "%v", err)
		return out
	}
	mp, err := ReadMap(dir)
	if err != nil {
		fatal("embedding.unreadable", "%v", err)
		return out
	}
	if len(mp.Cells) != d.Cells {
		fatal("embedding.sizes", "The map holds %d cells; its description says %d.", len(mp.Cells), d.Cells)
	}
	for i, c := range mp.Cells {
		if i > 0 && c <= mp.Cells[i-1] {
			fatal("embedding.cells", "The map's cells are not in order (cell %d).", i)
			break
		}
	}
	for _, v := range mp.XY {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			fatal("embedding.values", "A cell's place on the map is not a finite number.")
			break
		}
	}
	for _, v := range mp.Score {
		if !(v >= 0 && v <= 1) {
			fatal("embedding.values", "A cell's reliability is not between 0 and 1.")
			break
		}
	}
	if t := d.Quality.Trustworthiness; !(t >= 0 && t <= 1) {
		fatal("embedding.values", "The trustworthiness %v is not between 0 and 1.", t)
	}
	s := Summary{Clustering: d.Clustering, Cells: d.Cells, Trustworthiness: d.Quality.Trustworthiness, Precision: d.Quality.Precision, Recall: d.Quality.Recall, Search: d.Quality.Search}
	if d.Source != nil {
		s.Source = d.Source.Tool
	}
	m.Summary, _ = json.Marshal(s)
	return out
}
