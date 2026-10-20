package main

// Transforms placing post-Xenium images: artifacts of kind "transform", each a version of an image's
// alignment, from its pixels (the source system) to the dataset's µm (the destination): Xenium
// Explorer's transformation matrix or keypoints (the file recorded where it lies, by its SHA-256),
// or landmarks placed in SituCraft and fitted as a similarity or an affine transform
// (internal/register), each landmark's residual and leave-one-out error recorded, with who made it,
// a note and the version it was made from. Which version places an image is the project's decision
// (web/lib/transforms.js). Any version is written back in Xenium Explorer's convention.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"situcraft/internal/artifacts"
	"situcraft/internal/register"
)

// The transform artifact's file in its folder.
const transformFile = "transform.json"

// transformDescription is transform.json.
type transformDescription struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	Name    string `json:"name"`
	// Source and Destination are the systems it maps between: an image's full-resolution pixels
	// ((0, 0) the corner of its first pixel) and the dataset's µm.
	Source      coordinateSystem `json:"source"`
	Destination coordinateSystem `json:"destination"`
	// Method is how it was made: "matrix" or "keypoints" (Xenium Explorer's file), or "landmarks"
	// (placed in SituCraft); Model what its fit may change ("affine" for a matrix).
	Method    string          `json:"method"`
	Model     register.Model  `json:"model"`
	Transform register.Affine `json:"transform"`
	// Landmarks are each point placed on both: fixed in µm, moving in the image's pixels.
	Landmarks []landmark  `json:"landmarks,omitempty"`
	Fit       *fitSummary `json:"fit,omitempty"`
	// File is Xenium Explorer's file it was read from, where it lies.
	File *imageSource `json:"file,omitempty"`
	// An automatic registration's (api_register.go): its local matches (those kept fitted, as
	// landmarks are), a deformation after the transform, and what it found.
	Matches      []match             `json:"matches,omitempty"`
	Deformation  *register.BSpline   `json:"deformation,omitempty"`
	Registration *registrationReport `json:"registration,omitempty"`
	// XeniumPixelSize is the dataset's pixel size (µm), which Xenium Explorer's files are in.
	XeniumPixelSize float64 `json:"xeniumPixelSize"`
	// Parent is the version it was made from.
	Parent string `json:"parent,omitempty"`
	By     string `json:"by"`
	Note   string `json:"note,omitempty"`
}

type coordinateSystem struct {
	Name  string `json:"name"`
	Units string `json:"units"`
	// Image is the image artifact whose pixels it is, Dataset the run whose µm.
	Image   string `json:"image,omitempty"`
	Dataset string `json:"dataset,omitempty"`
}

type landmark struct {
	Fixed  [2]float64 `json:"fixed"`
	Moving [2]float64 `json:"moving"`
}

// match is a local match of an automatic registration: a patch's centre on the Xenium image (µm),
// where it matched on the image (its pixels), how clearly, and whether the fit kept it.
type match struct {
	Fixed   [2]float64 `json:"fixed"`
	Moving  [2]float64 `json:"moving"`
	Quality float64    `json:"quality"`
	Kept    bool       `json:"kept"`
}

// keptMatches are an automatic registration's kept matches, as keypoints.
func keptMatches(ms []match) []register.Keypoint {
	var out []register.Keypoint
	for _, m := range ms {
		if m.Kept {
			out = append(out, register.Keypoint{Fixed: register.Point{X: m.Fixed[0], Y: m.Fixed[1]}, Moving: register.Point{X: m.Moving[0], Y: m.Moving[1]}})
		}
	}
	return out
}

// fitSummary is how well a fit fits, in µm: each landmark's residual and leave-one-out error.
type fitSummary struct {
	Residuals   []float64 `json:"residuals"`
	LeaveOneOut []float64 `json:"leaveOneOut,omitempty"`
	RMS         float64   `json:"rms"`
	Max         float64   `json:"max"`
	LooRMS      float64   `json:"looRms,omitempty"`
	LooMax      float64   `json:"looMax,omitempty"`
}

// transformRequest is what making a transform is asked: landmarks and a model, or Xenium Explorer's
// file (alignment, a path on this computer); the version it starts from, who and why.
type transformRequest struct {
	Landmarks []landmark `json:"landmarks"`
	Model     string     `json:"model"`
	Alignment string     `json:"alignment"`
	Parent    string     `json:"parent"`
	By        string     `json:"by"`
	Note      string     `json:"note"`
}

// maxLandmarks is the most landmarks a transform is fitted to.
const maxLandmarks = 10000

func (a *app) registerTransforms(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/images/{id}/transforms", a.postTransform)
	mux.HandleFunc("GET /api/transforms/{id}", a.getTransform)
	mux.HandleFunc("GET /api/transforms/{id}/xenium-explorer/{file}", a.exportTransform)
}

// imageOf reads an image artifact's manifest and description.
func (a *app) imageOf(id string) (artifacts.Manifest, imageDescription, error) {
	m, err := a.artifacts.Get(id)
	if err != nil || m.Kind != "image" {
		return m, imageDescription{}, fmt.Errorf("no image %s", id)
	}
	if m.State != artifacts.Complete {
		return m, imageDescription{}, fmt.Errorf("the image %s is %s", id, m.State)
	}
	p, err := a.artifacts.Path(id, imageFile)
	if err != nil {
		return m, imageDescription{}, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return m, imageDescription{}, err
	}
	var d imageDescription
	if err := json.Unmarshal(raw, &d); err != nil {
		return m, imageDescription{}, err
	}
	return m, d, nil
}

// transformOf reads a transform artifact's description.
func (a *app) transformOf(id string) (artifacts.Manifest, transformDescription, error) {
	m, err := a.artifacts.Get(id)
	if err != nil || m.Kind != "transform" {
		return m, transformDescription{}, fmt.Errorf("no alignment %s", id)
	}
	p, err := a.artifacts.Path(id, transformFile)
	if err != nil {
		return m, transformDescription{}, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return m, transformDescription{}, err
	}
	var d transformDescription
	if err := json.Unmarshal(raw, &d); err != nil {
		return m, transformDescription{}, err
	}
	return m, d, nil
}

// postTransform makes a version of an image's alignment, at once: from landmarks, or from Xenium
// Explorer's file (only this computer may name one). Answers the artifact and its description.
func (a *app) postTransform(w http.ResponseWriter, r *http.Request) {
	var req transformRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "The request is not an alignment's: "+err.Error())
		return
	}
	if req.Alignment != "" && !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may read its files.")
		return
	}
	m, d, err := a.makeTransform(r.PathValue("id"), req)
	if err != nil {
		code := http.StatusUnprocessableEntity
		if strings.HasPrefix(err.Error(), "no image") {
			code = http.StatusNotFound
		}
		writeError(w, code, err.Error())
		return
	}
	writeJSON(w, map[string]any{"manifest": m, "transform": d})
}

// makeTransform records a transform of an image: its file read, or its landmarks fitted.
func (a *app) makeTransform(imageID string, req transformRequest) (artifacts.Manifest, transformDescription, error) {
	im, idesc, err := a.imageOf(imageID)
	if err != nil {
		return artifacts.Manifest{}, transformDescription{}, err
	}
	ps := idesc.XeniumPixelSize
	if !(ps > 0) {
		return artifacts.Manifest{}, transformDescription{}, errors.New("the image's dataset gives no pixel size")
	}
	run := ""
	for _, p := range im.Lineage.Parents {
		if p.Kind == "bundle" {
			run = p.ID
		}
	}
	d := transformDescription{
		Format: "situcraft-transform", Version: 1,
		Source:          coordinateSystem{Name: "image:" + imageID, Units: "pixels", Image: imageID},
		Destination:     coordinateSystem{Name: "global", Units: "µm", Dataset: run},
		XeniumPixelSize: ps,
		By:              strings.TrimSpace(req.By),
		Note:            strings.TrimSpace(req.Note),
	}
	if d.By == "" {
		d.By = "the user"
	}
	if len(d.By) > 200 || len(d.Note) > 4000 {
		return artifacts.Manifest{}, transformDescription{}, errors.New("who made it and its note are too long")
	}
	if req.Parent != "" {
		_, pd, err := a.transformOf(req.Parent)
		if err != nil || pd.Source.Image != imageID {
			return artifacts.Manifest{}, transformDescription{}, fmt.Errorf("%s is not an alignment of this image", req.Parent)
		}
		d.Parent = req.Parent
	}
	var file *imageSource
	switch {
	case req.Alignment != "" && len(req.Landmarks) > 0:
		return artifacts.Manifest{}, transformDescription{}, errors.New("give landmarks or Xenium Explorer's file, not both")
	case req.Alignment != "":
		path, err := namedFile(req.Alignment)
		if err != nil {
			return artifacts.Manifest{}, transformDescription{}, err
		}
		al, err := readAlignmentFile(path)
		if err != nil {
			return artifacts.Manifest{}, transformDescription{}, err
		}
		src, err := checksum(context.Background(), path, nil)
		if err != nil {
			return artifacts.Manifest{}, transformDescription{}, err
		}
		file = &src
		d.File = file
		d.Method = al.Kind
		if al.Kind == "matrix" {
			d.Model = register.ModelAffine
			d.Transform = al.Matrix.Then(register.Scaling(ps))
		} else {
			// Xenium Explorer's keypoints are in the Xenium image's pixels: their µm are fitted.
			for _, k := range al.Keypoints {
				d.Landmarks = append(d.Landmarks, landmark{Fixed: [2]float64{k.Fixed.X * ps, k.Fixed.Y * ps}, Moving: [2]float64{k.Moving.X, k.Moving.Y}})
			}
			d.Model = register.ModelSimilarity
		}
	default:
		if len(req.Landmarks) > maxLandmarks {
			return artifacts.Manifest{}, transformDescription{}, fmt.Errorf("%d landmarks: at most %d", len(req.Landmarks), maxLandmarks)
		}
		for i, l := range req.Landmarks {
			for _, v := range []float64{l.Fixed[0], l.Fixed[1], l.Moving[0], l.Moving[1]} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return artifacts.Manifest{}, transformDescription{}, fmt.Errorf("landmark %d: its positions must be numbers", i+1)
				}
			}
		}
		model, err := register.ParseModel(req.Model)
		if err != nil {
			return artifacts.Manifest{}, transformDescription{}, err
		}
		d.Method, d.Model, d.Landmarks = "landmarks", model, req.Landmarks
	}
	if len(d.Landmarks) > 0 {
		fit, err := register.FitKeypoints(d.Model, keypointsOf(d.Landmarks))
		if err != nil {
			return artifacts.Manifest{}, transformDescription{}, err
		}
		d.Transform = fit.Transform
		d.Fit = &fitSummary{Residuals: fit.Residuals, LeaveOneOut: fit.LeaveOneOut, RMS: fit.RMS, Max: fit.Max, LooRMS: fit.LooRMS, LooMax: fit.LooMax}
	}
	if !d.Transform.Valid() {
		return artifacts.Manifest{}, transformDescription{}, errors.New("the alignment cannot be inverted: it maps the image onto a line or a point")
	}
	d.Name = transformName(d, file)
	m, err := a.recordTransform(im, idesc, d, file)
	return m, d, err
}

func keypointsOf(ls []landmark) []register.Keypoint {
	out := make([]register.Keypoint, len(ls))
	for i, l := range ls {
		out[i] = register.Keypoint{Fixed: register.Point{X: l.Fixed[0], Y: l.Fixed[1]}, Moving: register.Point{X: l.Moving[0], Y: l.Moving[1]}}
	}
	return out
}

func transformName(d transformDescription, file *imageSource) string {
	switch d.Method {
	case "automatic":
		kept := len(keptMatches(d.Matches))
		name := fmt.Sprintf("Automatic, %s, %d matches", d.Model, kept)
		if d.Deformation != nil {
			name += ", deformed"
		}
		return name
	case "matrix":
		return "Xenium Explorer's transformation matrix (" + filepath.Base(file.Path) + ")"
	case "keypoints":
		return fmt.Sprintf("Xenium Explorer's %d keypoints (%s)", len(d.Landmarks), filepath.Base(file.Path))
	}
	return fmt.Sprintf("%d landmarks, %s, by %s", len(d.Landmarks), d.Model, d.By)
}

// article is a word with "a" or "an" before it.
func article(word string) string {
	if word != "" && strings.ContainsRune("aeiou", rune(word[0])) {
		return "an " + word
	}
	return "a " + word
}

// methodOf is how a transform was made, as its artifact's lineage says it.
func methodOf(d transformDescription) artifacts.Method {
	umeyama := "Umeyama S. Least-squares estimation of transformation parameters between two point patterns. IEEE Trans Pattern Anal Mach Intell 1991;13(4):376–380. doi:10.1109/34.88573"
	stone := "Stone M. Cross-validatory choice and assessment of statistical predictions. J R Stat Soc B 1974;36(2):111–147 (leave-one-out errors)"
	explorer := "10x Genomics. Xenium Explorer: image alignment (transformation matrix and keypoints)."
	switch {
	case d.Method == "matrix":
		return artifacts.Method{Name: "Aligned by Xenium Explorer's transformation matrix", Version: "1", Citation: explorer}
	case d.Method == "keypoints":
		return artifacts.Method{Name: "Aligned by Xenium Explorer's keypoints: a similarity fitted by least squares", Version: "1", Citation: explorer + " " + umeyama + "; " + stone}
	case d.Method == "automatic":
		cite := "Ruifrok AC, Johnston DA. Quantification of histochemical staining by color deconvolution. Anal Quant Cytol Histol 2001;23(4):291–299; Kuglin CD, Hines DC. The phase correlation image alignment method. Proc IEEE Conf Cybernetics and Society 1975; Reddy BS, Chatterji BN. An FFT-based technique for translation, rotation, and scale-invariant image registration. IEEE Trans Image Process 1996;5(8):1266–1271; Studholme C, Hill DLG, Hawkes DJ. An overlap invariant entropy measure of 3D medical image alignment. Pattern Recognit 1999;32(1):71–86; Hooke R, Jeeves TA. \"Direct search\" solution of numerical and statistical problems. J ACM 1961;8:212–229; Foroosh H, Zerubia J, Berthod M. Extension of phase correlation to subpixel registration. IEEE Trans Image Process 2002;11(3):188–200; " + umeyama
		name := "Aligned automatically by the nuclei: " + article(string(d.Model)) + " found by phase correlation, refined by normalized mutual information and fitted to local matches"
		if d.Deformation != nil {
			name += ", then a smooth deformation"
			cite += "; Rueckert D, Sonoda LI, Hayes C, Hill DLG, Leach MO, Hawkes DJ. Nonrigid registration using free-form deformations. IEEE Trans Med Imaging 1999;18(8):712–721"
		}
		return artifacts.Method{Name: name, Version: "1", Citation: cite}
	case d.Model == register.ModelAffine:
		return artifacts.Method{Name: "Aligned by landmarks: an affine transform fitted by least squares", Version: "1", Citation: stone}
	}
	return artifacts.Method{Name: "Aligned by landmarks: a similarity fitted by least squares", Version: "1", Citation: umeyama + "; " + stone}
}

// recordTransform records a transform as an artifact named by its description's checksum: the same
// version made again is the same artifact.
func (a *app) recordTransform(im artifacts.Manifest, idesc imageDescription, d transformDescription, file *imageSource) (artifacts.Manifest, error) {
	raw, _ := json.MarshalIndent(d, "", " ")
	raw = append(raw, '\n')
	h := sha256.Sum256(raw)
	id := "tf-" + hex.EncodeToString(h[:10])
	if existing, err := a.artifacts.Get(id); err == nil {
		return existing, nil
	}
	dir, err := a.artifacts.Stage(id)
	if err != nil {
		return artifacts.Manifest{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, transformFile), raw, 0o644); err != nil {
		a.artifacts.Discard(id)
		return artifacts.Manifest{}, err
	}
	t := d.Transform
	summary := map[string]any{"image": im.ID, "method": d.Method, "model": d.Model, "landmarks": len(d.Landmarks), "transform": t,
		"umPerPixel": t.Scale(), "rotation": t.Rotation(), "reflected": t.Reflected(), "by": d.By, "parent": d.Parent, "imagePixelSize": idesc.Image.PixelSize}
	if d.Fit != nil {
		summary["rms"], summary["max"], summary["looRms"], summary["looMax"] = d.Fit.RMS, d.Fit.Max, d.Fit.LooRMS, d.Fit.LooMax
	}
	if d.Method == "automatic" {
		summary["matches"], summary["kept"] = len(d.Matches), len(keptMatches(d.Matches))
		if r := d.Registration; r != nil {
			summary["channel"], summary["nmi"], summary["heldOut"], summary["modelHeldOut"], summary["deformedRms"] = r.Channel, r.NMI, r.HeldOut, r.ModelHeldOut, r.DeformedRMS
		}
		summary["deformed"] = d.Deformation != nil
		if d.Deformation != nil {
			summary["maxDisplacement"] = d.Registration.MaxDisplacement
		}
	}
	sum, _ := json.Marshal(summary)
	params, _ := json.Marshal(map[string]any{"model": d.Model, "landmarks": len(d.Landmarks), "by": d.By, "note": d.Note})
	parents := []artifacts.Parent{{Kind: "image", ID: im.ID, Name: im.Name, SHA256: idesc.Source.SHA256}}
	if d.Parent != "" {
		parents = append(parents, artifacts.Parent{Kind: "transform", ID: d.Parent})
	}
	m := artifacts.Manifest{
		ID: id, Kind: "transform", Name: d.Name,
		Lineage: artifacts.Lineage{
			Parents:    parents,
			Method:     methodOf(d),
			Parameters: params,
			Software:   []artifacts.Software{{Name: "situcraft", Version: version, Commit: buildCommit()}},
		},
		Files:   []artifacts.File{{Path: transformFile, Role: "transform"}},
		Summary: sum,
	}
	if file != nil {
		m.Files = append(m.Files, artifacts.File{Path: file.Path, Role: "alignment", Size: file.Size, SHA256: file.SHA256, Location: artifacts.InPlace})
	}
	recorded, err := a.artifacts.Promote(m, checkTransformArtifact)
	if err != nil {
		a.artifacts.Discard(id)
		return artifacts.Manifest{}, err
	}
	if recorded.State == artifacts.Invalid {
		return recorded, fmt.Errorf("the alignment's check found fatal problems (artifact %s, kept for review)", recorded.ID)
	}
	return recorded, nil
}

// checkTransformArtifact checks a transform before it is recorded: its description reads back, it
// can be inverted, and its landmarks fit as recorded. It notes when the alignment and the image's
// metadata disagree on the image's pixel size.
func checkTransformArtifact(dir string, m *artifacts.Manifest) []artifacts.Finding {
	raw, err := os.ReadFile(filepath.Join(dir, transformFile))
	if err != nil {
		return []artifacts.Finding{{Severity: "fatal", Code: "transform.description", Message: "transform.json cannot be read: " + err.Error()}}
	}
	var d transformDescription
	if err := json.Unmarshal(raw, &d); err != nil || d.Format != "situcraft-transform" {
		return []artifacts.Finding{{Severity: "fatal", Code: "transform.description", Message: fmt.Sprintf("transform.json is not an alignment's description (%v)", err)}}
	}
	var out []artifacts.Finding
	if !d.Transform.Valid() {
		out = append(out, artifacts.Finding{Severity: "fatal", Code: "transform.singular", Message: "The transform cannot be inverted."})
	}
	if len(d.Landmarks) > 0 {
		fit, err := register.FitKeypoints(d.Model, keypointsOf(d.Landmarks))
		if err != nil || fit.Transform != d.Transform || d.Fit == nil || fit.RMS != d.Fit.RMS {
			out = append(out, artifacts.Finding{Severity: "fatal", Code: "transform.fit", Message: "The landmarks do not fit as recorded."})
		}
	}
	if kept := keptMatches(d.Matches); d.Method == "automatic" && len(kept) >= register.MinKeypoints {
		fit, err := register.FitKeypoints(d.Model, kept)
		if err != nil || fit.Transform != d.Transform {
			out = append(out, artifacts.Finding{Severity: "fatal", Code: "transform.fit", Message: "The kept matches do not fit as recorded."})
		}
	}
	if r := d.Registration; r != nil {
		for _, w := range r.Warnings {
			out = append(out, artifacts.Finding{Severity: "warning", Code: "transform.registration", Message: w})
		}
	}
	var s struct {
		ImagePixelSize float64 `json:"imagePixelSize"`
	}
	json.Unmarshal(m.Summary, &s)
	if s.ImagePixelSize > 0 {
		if ratio := d.Transform.Scale() / s.ImagePixelSize; ratio < 0.95 || ratio > 1.05 {
			out = append(out, artifacts.Finding{Severity: "info", Code: "transform.pixel-size", Message: fmt.Sprintf("The alignment makes the image's pixels %.4f µm; its metadata says %.4f µm. The alignment places it.", d.Transform.Scale(), s.ImagePixelSize)})
		}
	}
	return out
}

func (a *app) getTransform(w http.ResponseWriter, r *http.Request) {
	_, d, err := a.transformOf(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, d)
}

// exportTransform writes a transform in Xenium Explorer's convention, to load with its image there:
// the transformation matrix (the image's pixels to the Xenium image's), or the keypoints of a
// version made from landmarks (their fixed positions in the Xenium image's pixels).
func (a *app) exportTransform(w http.ResponseWriter, r *http.Request) {
	m, d, err := a.transformOf(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	data, name, err := xeniumExplorerFile(m, d, r.PathValue("file"), a.imageFileName(d.Source.Image))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, name))
	w.Write(data)
}

// imageFileName is an image artifact's file's name ("" when it cannot be read).
func (a *app) imageFileName(id string) string {
	_, d, err := a.imageOf(id)
	if err != nil {
		return ""
	}
	return filepath.Base(d.Source.Path)
}

// xeniumExplorerFile is a transform as Xenium Explorer's matrix or keypoints, and the name Xenium
// Explorer gives it after the image ("<sample>_he_imagealignment.csv" for "<sample>_he_image.ome.tif").
func xeniumExplorerFile(m artifacts.Manifest, d transformDescription, which, imageName string) ([]byte, string, error) {
	ps := d.XeniumPixelSize
	if !(ps > 0) {
		return nil, "", errors.New("the alignment records no pixel size of its dataset")
	}
	stem := imageName
	for _, ext := range []string{".ome.tiff", ".ome.tif", ".tiff", ".tif"} {
		if strings.HasSuffix(strings.ToLower(stem), ext) {
			stem = stem[:len(stem)-len(ext)]
			break
		}
	}
	stem = strings.TrimSuffix(stem, "_image")
	if stem == "" {
		stem = m.ID
	}
	switch which {
	case "matrix":
		return register.MatrixCSV(d.Transform.Then(register.Scaling(1 / ps))), stem + "_imagealignment.csv", nil
	case "keypoints":
		kps := keypointsOf(d.Landmarks)
		if d.Method == "automatic" {
			kps = keptMatches(d.Matches)
		}
		if len(kps) == 0 {
			return nil, "", errors.New("this alignment has no landmarks (it is a transformation matrix): write its matrix")
		}
		for i := range kps {
			kps[i].Fixed.X, kps[i].Fixed.Y = kps[i].Fixed.X/ps, kps[i].Fixed.Y/ps
		}
		return register.KeypointsCSV(kps), stem + "_keypoints.csv", nil
	}
	return nil, "", fmt.Errorf("no file %q: matrix or keypoints", which)
}
