package main

// Post-Xenium images (internal/slide): an H&E or immunofluorescence OME-TIFF added to a dataset with
// its alignment (internal/register), recorded as an artifact of kind "image" that names the image
// and the alignment file where they lie, by their SHA-256, and copies neither. The image is placed
// by its alignment: the transform from its pixels to the Xenium image's, then µm by the dataset's
// pixel size. Its tiles are read from the file when the page asks for them.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"situcraft/internal/artifacts"
	"situcraft/internal/jobs"
	"situcraft/internal/register"
	"situcraft/internal/slide"
	"situcraft/internal/xenium"
)

// The image artifact's file in its folder.
const imageFile = "image.json"

// imageDescription is image.json: the image as read, where it is, its alignment and its
// transform to µm.
type imageDescription struct {
	Format  string            `json:"format"`
	Version int               `json:"version"`
	Name    string            `json:"name"`
	Source  imageSource       `json:"source"`
	Image   slide.Description `json:"image"`
	// Alignment is as its file gave it: the image's pixels to the Xenium image's.
	Alignment     register.Alignment `json:"alignment"`
	AlignmentFile imageSource        `json:"alignmentFile"`
	// XeniumPixelSize is the dataset's pixel size (µm), and Transform the image's pixels to µm.
	XeniumPixelSize float64         `json:"xeniumPixelSize"`
	Transform       register.Affine `json:"transform"`
	// Ranges are each channel's samples from the 0.1th to the 99.9th percentile over its smallest
	// level, the window it is first shown with (none for an RGB image).
	Ranges [][2]int `json:"ranges,omitempty"`
}

type imageSource struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// imageSpec is what a job adding an image is asked.
type imageSpec struct {
	Bundle    string `json:"bundle"`
	Path      string `json:"path"`
	Alignment string `json:"alignment"`
	Name      string `json:"name,omitempty"`
}

func (a *app) registerImages(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/{index}/images/check", a.checkImage)
	mux.HandleFunc("POST /api/local/{index}/images", a.startImage)
	mux.HandleFunc("GET /api/images/{id}", a.describeImage)
	mux.HandleFunc("GET /api/images/{id}/tiles/{level}/{channel}/{tx}/{ty}", a.postImageTile)
}

// checkImage reads what an image is and the alignment files beside it, before it is added; with
// alignment, reads that file too. Only this computer may ask: it names files to read.
func (a *app) checkImage(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may read its files.")
		return
	}
	path, err := namedFile(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	im, err := slide.Open(path)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	defer im.Close()
	answer := map[string]any{"path": path, "image": im.Description, "siblings": slide.Siblings(path)}
	if al := r.URL.Query().Get("alignment"); al != "" {
		apath, err := namedFile(al)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		alignment, err := readAlignmentFile(apath)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		answer["alignment"] = alignmentSummary(alignment)
	}
	writeJSON(w, answer)
}

// namedFile is a file named in a request, as an absolute path.
func namedFile(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("name a file")
	}
	return absPath(p)
}

func readAlignmentFile(path string) (*register.Alignment, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > 4<<20 {
		return nil, fmt.Errorf("%s is %d bytes, too large to be an alignment", filepath.Base(path), info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	al, err := register.ReadAlignment(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return al, nil
}

// alignmentSummary says what an alignment does, in plain numbers.
func alignmentSummary(al *register.Alignment) map[string]any {
	m := al.Matrix
	out := map[string]any{"kind": al.Kind, "matrix": m, "scale": m.Scale(), "rotation": m.Rotation(), "reflected": m.Reflected()}
	if len(al.Residuals) > 0 {
		sum, worst := 0.0, 0.0
		for _, r := range al.Residuals {
			sum += r * r
			worst = max(worst, r)
		}
		out["keypoints"] = len(al.Keypoints)
		out["rmsResidual"] = math.Sqrt(sum / float64(len(al.Residuals)))
		out["maxResidual"] = worst
	}
	return out
}

// startImage starts the job adding an image to a bundle's dataset: {path, alignment, name}.
func (a *app) startImage(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may add its files.")
		return
	}
	var spec imageSpec
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "The request is not an image's: "+err.Error())
		return
	}
	var err error
	if spec.Path, err = namedFile(spec.Path); err != nil {
		writeError(w, http.StatusBadRequest, "The image: "+err.Error())
		return
	}
	if spec.Alignment, err = namedFile(spec.Alignment); err != nil {
		writeError(w, http.StatusBadRequest, "The alignment: "+err.Error()+" (the transformation matrix or keypoints Xenium Explorer saves); an image is placed by its alignment")
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	spec.Bundle = b.Path
	name := b.Name
	b.Close()
	title := fmt.Sprintf("Image %s of %s", filepath.Base(spec.Path), name)
	key := "image\x00" + spec.Bundle + "\x00" + spec.Path + "\x00" + spec.Alignment
	job, _, err := a.jobs.Submit("image", title, key, spec, a.imageJob(spec))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"job": job})
}

// imageJob reads the image and its alignment, checksums both and records the artifact.
func (a *app) imageJob(spec imageSpec) jobs.Func {
	return func(ctx context.Context, r *jobs.Run) error {
		b, err := xenium.Open(spec.Bundle, xenium.Options{CacheDir: a.cacheDir})
		if err != nil {
			return err
		}
		pixelSize, run, bundleName := b.Experiment.PixelSize, b.Experiment.AnalysisUUID, b.Name
		manifestSum := ""
		if raw, err := b.ReadSmall("experiment.xenium"); err == nil {
			h := sha256.Sum256(raw)
			manifestSum = hex.EncodeToString(h[:])
		}
		b.Close()
		if !(pixelSize > 0) {
			return errors.New("the dataset's experiment.xenium gives no pixel size: the image cannot be placed in µm")
		}
		r.Progress(0, "Reading the image")
		im, err := slide.Open(spec.Path)
		if err != nil {
			return err
		}
		desc := im.Description
		ranges, err := channelRanges(im)
		im.Close()
		if err != nil {
			return err
		}
		alignment, err := readAlignmentFile(spec.Alignment)
		if err != nil {
			return err
		}
		r.Log("info", "%s: %s, %d×%d, %d levels, %s; aligned by %s (scale %.4f%s).", filepath.Base(spec.Path), kindText(desc), desc.Width, desc.Height, len(desc.Levels), desc.Compression, alignment.Kind, alignment.Matrix.Scale(), map[bool]string{true: ", mirrored", false: ""}[alignment.Matrix.Reflected()])
		r.Progress(0.05, "Checksumming the image")
		src, err := checksum(ctx, spec.Path, func(f float64) { r.Progress(0.05+0.9*f, "Checksumming the image") })
		if err != nil {
			return err
		}
		al, err := checksum(ctx, spec.Alignment, nil)
		if err != nil {
			return err
		}
		name := spec.Name
		if name == "" {
			name = imageName(spec.Path, desc)
		}
		d := imageDescription{
			Format: "situcraft-image", Version: 1, Name: name,
			Source: src, Image: desc, Alignment: *alignment, AlignmentFile: al,
			XeniumPixelSize: pixelSize, Transform: alignment.Matrix.Then(register.Scaling(pixelSize)), Ranges: ranges,
		}
		// The artifact's name: the dataset, the image and the alignment, by their contents.
		h := sha256.Sum256([]byte(run + "\n" + manifestSum + "\n" + src.SHA256 + "\n" + al.SHA256))
		id := "img-" + hex.EncodeToString(h[:10])
		dir, err := a.artifacts.Stage(id)
		if err != nil {
			return err
		}
		raw, _ := json.MarshalIndent(d, "", " ")
		if err := os.WriteFile(filepath.Join(dir, imageFile), append(raw, '\n'), 0o644); err != nil {
			a.artifacts.Discard(id)
			return err
		}
		r.Validating()
		summary := map[string]any{"kind": desc.Kind, "width": desc.Width, "height": desc.Height, "bits": desc.Bits, "compression": desc.Compression, "levels": len(desc.Levels),
			"channels": desc.Channels, "pixelSize": desc.PixelSize, "transform": d.Transform, "alignment": alignmentSummary(alignment),
			"umPerPixel": d.Transform.Scale()}
		sum, _ := json.Marshal(summary)
		jobSpec, _ := json.Marshal(spec)
		m := artifacts.Manifest{
			ID:   id,
			Kind: "image",
			Name: name,
			Lineage: artifacts.Lineage{
				Parents: []artifacts.Parent{{Kind: "bundle", ID: run, Name: bundleName, SHA256: manifestSum}},
				Job:     &artifacts.Job{ID: r.ID(), Kind: "image", Spec: jobSpec},
				Method: artifacts.Method{Name: "Post-Xenium image, aligned by " + map[string]string{"matrix": "its transformation matrix", "keypoints": "its keypoints"}[alignment.Kind],
					Version: "1", Citation: "10x Genomics. Xenium Explorer: image alignment (transformation matrix and keypoints)."},
				Software: []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
			},
			Files: []artifacts.File{
				{Path: imageFile, Role: "description"},
				{Path: src.Path, Role: "image", Size: src.Size, SHA256: src.SHA256, Location: artifacts.InPlace},
				{Path: al.Path, Role: "alignment", Size: al.Size, SHA256: al.SHA256, Location: artifacts.InPlace},
			},
			Summary: sum,
		}
		recorded, err := a.artifacts.Promote(m, checkImageArtifact)
		if err != nil {
			a.artifacts.Discard(id)
			return err
		}
		if recorded.State == artifacts.Invalid {
			return fmt.Errorf("%w: the image's check found fatal problems (artifact %s, kept for review)", jobs.ErrInvalid, recorded.ID)
		}
		r.Log("info", "Recorded the image as artifact %s: %.4f µm per pixel as aligned (its metadata says %s).", recorded.ID, d.Transform.Scale(), pixelSizeText(desc.PixelSize))
		return nil
	}
}

// channelRanges is each channel's 0.1th to 99.9th percentile over its smallest level (every tile
// of it): the window a channel is first shown with. An RGB image has none.
func channelRanges(im *slide.Image) ([][2]int, error) {
	if im.Kind == slide.RGB {
		return nil, nil
	}
	l := len(im.Levels) - 1
	lv := im.Levels[l]
	var out [][2]int
	for c := range im.Channels {
		hist := make([]int64, 1<<im.Bits)
		var n int64
		for ty := 0; ty < lv.Down; ty++ {
			for tx := 0; tx < lv.Across; tx++ {
				tile, err := im.Tile(l, c, tx, ty)
				if err != nil {
					return nil, err
				}
				w, h := min(lv.TileWidth, lv.Width-tx*lv.TileWidth), min(lv.TileHeight, lv.Height-ty*lv.TileHeight)
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						i := y*lv.TileWidth + x
						v := int(tile[i])
						if im.Bits == 16 {
							v = int(tile[2*i]) | int(tile[2*i+1])<<8
						}
						hist[v]++
						n++
					}
				}
			}
		}
		at := func(q float64) int {
			target := int64(q * float64(n))
			var seen int64
			for v, k := range hist {
				seen += k
				if seen > target {
					return v
				}
			}
			return len(hist) - 1
		}
		lo, hi := at(0.001), at(0.999)
		if hi <= lo {
			hi = lo + 1
		}
		out = append(out, [2]int{lo, hi})
	}
	return out, nil
}

func kindText(d slide.Description) string {
	if d.Kind == slide.RGB {
		return "RGB"
	}
	names := make([]string, len(d.Channels))
	for i, c := range d.Channels {
		names[i] = c.Name
	}
	return fmt.Sprintf("%d channels (%s), %d bits", len(d.Channels), strings.Join(names, ", "), d.Bits)
}

func pixelSizeText(s float64) string {
	if s <= 0 {
		return "nothing"
	}
	return strconv.FormatFloat(s, 'f', -1, 64) + " µm"
}

// imageName is an image's name from its file: Xenium Explorer's "<sample>_he_image.ome.tif" an
// H&E, "<sample>_if_image.ome.tif" an IF image.
func imageName(path string, d slide.Description) string {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(base, "_he_") || strings.HasPrefix(base, "he_") || d.Kind == slide.RGB:
		return "H&E (" + filepath.Base(path) + ")"
	case strings.Contains(base, "_if_") || strings.HasPrefix(base, "if_"):
		return "Immunofluorescence (" + filepath.Base(path) + ")"
	}
	return filepath.Base(path)
}

// checksum is a file's SHA-256, read in chunks so that a cancelled job stops.
func checksum(ctx context.Context, path string, progress func(float64)) (imageSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return imageSource{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return imageSource{}, err
	}
	h := sha256.New()
	buf := make([]byte, 8<<20)
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return imageSource{}, err
		}
		n, err := f.Read(buf)
		h.Write(buf[:n])
		done += int64(n)
		if progress != nil && info.Size() > 0 {
			progress(float64(done) / float64(info.Size()))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return imageSource{}, err
		}
	}
	return imageSource{Path: path, Size: info.Size(), SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// checkImageArtifact checks an image artifact before it is recorded: its description reads back,
// its transform is invertible, and the image still opens as described.
func checkImageArtifact(dir string, m *artifacts.Manifest) []artifacts.Finding {
	raw, err := os.ReadFile(filepath.Join(dir, imageFile))
	if err != nil {
		return []artifacts.Finding{{Severity: "fatal", Code: "image.description", Message: "image.json cannot be read: " + err.Error()}}
	}
	var d imageDescription
	if err := json.Unmarshal(raw, &d); err != nil || d.Format != "situcraft-image" {
		return []artifacts.Finding{{Severity: "fatal", Code: "image.description", Message: fmt.Sprintf("image.json is not an image's description (%v)", err)}}
	}
	var out []artifacts.Finding
	if !d.Transform.Valid() {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: "image.transform", Message: "The image's transform to µm cannot be inverted."})
	}
	im, err := slide.Open(d.Source.Path)
	if err != nil {
		return append(out, artifacts.Finding{Severity: "fatal", Code: "image.unreadable", Message: err.Error()})
	}
	defer im.Close()
	if im.Width != d.Image.Width || im.Height != d.Image.Height || len(im.Levels) != len(d.Image.Levels) {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: "image.changed", Message: "The image is not the one described."})
	}
	if d.Image.PixelSize > 0 {
		if ratio := d.Transform.Scale() / d.Image.PixelSize; ratio < 0.95 || ratio > 1.05 {
			out = append(out, artifacts.Finding{Severity: "info", Code: "image.pixel-size", Message: fmt.Sprintf("The alignment makes the image's pixels %.4f µm; its metadata says %.4f µm. The alignment places it.", d.Transform.Scale(), d.Image.PixelSize)})
		}
	}
	for _, n := range d.Image.Notes {
		out = append(out, artifacts.Finding{Severity: "info", Code: "image.planes", Message: n})
	}
	return out
}

// openImages are the images the page draws, kept open.
type openImages struct {
	mu   sync.Mutex
	open map[string]*openImage
}

type openImage struct {
	im   *slide.Image
	desc imageDescription
}

const maxOpenImages = 8

// image answers an image artifact's description and its open image.
func (a *app) image(id string) (*openImage, error) {
	a.images.mu.Lock()
	defer a.images.mu.Unlock()
	if o, ok := a.images.open[id]; ok {
		return o, nil
	}
	m, err := a.artifacts.Get(id)
	if err != nil || m.Kind != "image" {
		return nil, fmt.Errorf("no image %s", id)
	}
	if m.State != artifacts.Complete {
		return nil, fmt.Errorf("the image %s is %s", id, m.State)
	}
	p, err := a.artifacts.Path(id, imageFile)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var d imageDescription
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	im, err := slide.Open(d.Source.Path)
	if err != nil {
		return nil, fmt.Errorf("the image is not where it was recorded (%s): %w", d.Source.Path, err)
	}
	if im.Width != d.Image.Width || im.Height != d.Image.Height || len(im.Levels) != len(d.Image.Levels) {
		im.Close()
		return nil, fmt.Errorf("%s is not the image recorded: it changed", d.Source.Path)
	}
	if a.images.open == nil {
		a.images.open = map[string]*openImage{}
	}
	if len(a.images.open) >= maxOpenImages {
		for k, o := range a.images.open {
			o.im.Close()
			delete(a.images.open, k)
			break
		}
	}
	o := &openImage{im: im, desc: d}
	a.images.open[id] = o
	return o, nil
}

func (a *app) describeImage(w http.ResponseWriter, r *http.Request) {
	o, err := a.image(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, o.desc)
}

// postImageTile answers one tile of an image's level and channel, TileWidth × TileHeight (in the
// headers): an RGB tile's pixels as RGB bytes; a channel's samples as the stains' tiles hold theirs,
// every sample's low byte, then every high byte (zero for 8-bit samples), so that the page draws
// both alike. Sent as they are: the page and the program share a computer.
func (a *app) postImageTile(w http.ResponseWriter, r *http.Request) {
	o, err := a.image(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var n [4]int
	for i, k := range []string{"level", "channel", "tx", "ty"} {
		if n[i], err = strconv.Atoi(r.PathValue(k)); err != nil {
			writeError(w, http.StatusBadRequest, k+" is not a number")
			return
		}
	}
	tile, err := o.im.Tile(n[0], n[1], n[2], n[3])
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	lv := o.im.Levels[n[0]]
	if o.im.Kind == slide.Channels {
		tile = splitPlanes(tile, o.im.Bits)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Tile-Width", strconv.Itoa(lv.TileWidth))
	w.Header().Set("X-Tile-Height", strconv.Itoa(lv.TileHeight))
	// The renderer keeps the tiles it draws; a browser's cache of megabytes a tile would only fill
	// the disk.
	w.Header().Set("Cache-Control", "no-store")
	w.Write(tile)
}

// splitPlanes lays samples out as two byte planes, the low bytes then the high.
func splitPlanes(samples []byte, bits int) []byte {
	if bits == 8 {
		out := make([]byte, 2*len(samples))
		copy(out, samples)
		return out
	}
	n := len(samples) / 2
	out := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		out[i], out[n+i] = samples[2*i], samples[2*i+1]
	}
	return out
}
