package simulate

// Post-Xenium images of the simulated section: an H&E and an immunofluorescence image of the same
// cells and nuclei, as a section is stained and scanned after a Xenium run, each placed on the
// section by a known transform, the truth registration is scored against.
//
// Where each lies: a pixel q of the image (full resolution; (0, 0) the corner of its first) is at
// x = A·q on the section (µm), A an affine transform of the image's pixel size, aspect, shear,
// reflection and turn, then displaced by a smooth deformation d(x), a sum of sine waves along each
// axis (the section stretched as it was stained): q lies at x + d(x). The deformation is of another
// family than the B-splines registration may fit, so a method is not scored on its own model.
//
// What each shows:
//   - the H&E, by Beer–Lambert's law: each pixel's optical densities of haematoxylin (dense in the
//     nuclei, faint elsewhere) and eosin (in the cytoplasm, by the cell's type, and the stroma
//     between cells) times the stains' colours, haematoxylin (0.65, 0.70, 0.29) and eosin (0.07,
//     0.99, 0.11) (Ruifrok AC, Johnston DA. Quantification of histochemical staining by color
//     deconvolution. Anal Quant Cytol Histol 2001;23(4):291–299), each varied per section, over
//     the glass's light, blurred by the optics, with the camera's noise; 8-bit RGB;
//   - the immunofluorescence image, two 16-bit channels: DAPI (the nuclei, each of its own
//     brightness) and a membrane stain (the cells' boundaries), blurred, on a background, with
//     noise.
// Each pixel averages 2 × 2 points of the section. Every product is rounded on its own and every
// sine is dmath's: the images are the same on every processor.

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"

	"situcraft/internal/dmath"
	"situcraft/internal/random"
	"situcraft/internal/tiff"
)

// PostXenium are the post-Xenium images a simulation makes of its section.
type PostXenium struct {
	HE *PostImage `json:"he,omitempty"`
	IF *PostImage `json:"if,omitempty"`
}

// PostImage is how a post-Xenium image is placed on the section.
type PostImage struct {
	// PixelUm is its pixels' width (µm) and Aspect their height over their width.
	PixelUm float64 `json:"pixel_um"`
	Aspect  float64 `json:"aspect"`
	// Shear moves each of its rows along x by this share of its y; Mirrored flips it top to
	// bottom; RotationDeg turns it (degrees, counter-clockwise in the section's axes, whose y
	// points down: clockwise as seen).
	Shear       float64 `json:"shear"`
	Mirrored    bool    `json:"mirrored"`
	RotationDeg float64 `json:"rotation_deg"`
	// MarginUm is the glass shown around the section.
	MarginUm float64 `json:"margin_um"`
	// DeformationUm is the most the deformation moves a point along each axis, WavelengthUm the
	// waves' length (0 µm: none).
	DeformationUm float64 `json:"deformation_um"`
	WavelengthUm  float64 `json:"deformation_wavelength_um"`
}

// DefaultPostXenium is an H&E and an immunofluorescence image as 10x's datasets have them: the
// H&E of a 40× scanner's pixels, mirrored and turned a little; the immunofluorescence image of
// larger pixels, turned half a turn; each a little sheared or stretched, and deformed by up to 2 µm.
func DefaultPostXenium() *PostXenium {
	return &PostXenium{
		HE: &PostImage{PixelUm: 0.3638, Aspect: 1.004, Shear: 0.003, Mirrored: true, RotationDeg: 2.5, MarginUm: 40, DeformationUm: 2, WavelengthUm: 300},
		IF: &PostImage{PixelUm: 0.65, Aspect: 1, Shear: 0, Mirrored: false, RotationDeg: -177, MarginUm: 30, DeformationUm: 1.5, WavelengthUm: 400},
	}
}

// Where a simulated bundle keeps its post-Xenium images, and their truth.
const (
	PostXeniumFolder = "post_xenium"
	// RegistrationFile is the truth: where each image lies on the section.
	RegistrationFile = "truth/post_xenium/registration.json"
	heImageName      = "simulated_he_image.ome.tif"
	ifImageName      = "simulated_if_image.ome.tif"
)

const streamPostXenium = 1 << 36

// maxPostXeniumPixels is the largest post-Xenium image a simulation makes.
const maxPostXeniumPixels = 100e6

func (pi *PostImage) check(what string, need func(bool, string)) {
	need(pi.PixelUm >= 0.1 && pi.PixelUm <= 5, what+": a pixel of 0.1 to 5 µm")
	need(pi.Aspect >= 0.8 && pi.Aspect <= 1.25, what+": an aspect of 0.8 to 1.25")
	need(math.Abs(pi.Shear) <= 0.2, what+": a shear of at most 0.2")
	need(math.Abs(pi.RotationDeg) <= 360, what+": a turn of at most 360°")
	need(pi.MarginUm >= 0 && pi.MarginUm <= 1000, what+": a margin of 0 to 1,000 µm")
	need(pi.DeformationUm >= 0 && pi.DeformationUm <= 50, what+": a deformation of 0 to 50 µm")
	need(pi.DeformationUm == 0 || pi.WavelengthUm >= 20, what+": a deformation's waves of 20 µm or more")
}

// Registration is the truth of where a simulated post-Xenium image lies on the section.
type Registration struct {
	// Image is its file within the bundle, Kind "he" or "if".
	Image  string `json:"image"`
	Kind   string `json:"kind"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	// Transform maps its pixels to µm (A above), as two rows; Deformation then displaces them.
	Transform   [2][3]float64 `json:"transform"`
	Deformation []Wave        `json:"deformation"`
	Parameters  PostImage     `json:"parameters"`
	// Stains are the H&E's haematoxylin and eosin colours (optical densities of red, green and
	// blue, of unit length), as this section's were.
	Stains *[2][3]float64 `json:"stains,omitempty"`
}

// Wave is one term of a deformation: along Axis (0 for x, 1 for y), AmplitudeUm times the sine of
// 2π times the point's product with Frequency (cycles per µm), plus Phase (radians).
type Wave struct {
	Axis        int        `json:"axis"`
	AmplitudeUm float64    `json:"amplitude_um"`
	Frequency   [2]float64 `json:"frequency_per_um"`
	Phase       float64    `json:"phase"`
}

// Map is where a point of the image (its pixels, full resolution) lies on the section (µm).
func (r *Registration) Map(u, v float64) (float64, float64) {
	t := r.Transform
	x := mul(t[0][0], u) + mul(t[0][1], v) + t[0][2]
	y := mul(t[1][0], u) + mul(t[1][1], v) + t[1][2]
	dx, dy := r.Displacement(x, y)
	return x + dx, y + dy
}

// Displacement is the deformation at a point of the section (µm).
func (r *Registration) Displacement(x, y float64) (float64, float64) {
	var d [2]float64
	for _, w := range r.Deformation {
		s, _ := dmath.SinCos(mul(2*math.Pi, mul(w.Frequency[0], x)+mul(w.Frequency[1], y)) + w.Phase)
		d[w.Axis] += mul(w.AmplitudeUm, s)
	}
	return d[0], d[1]
}

// ReadRegistrations reads a simulated bundle's truth of its post-Xenium images.
func ReadRegistrations(bundle string) ([]Registration, error) {
	raw, err := os.ReadFile(filepath.Join(bundle, filepath.FromSlash(RegistrationFile)))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Images []Registration `json:"images"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return doc.Images, nil
}

// place makes an image's registration: its transform from its parameters, the image's extent
// covering the section and its margin (and the most the deformation moves it), and its waves.
func place(pi PostImage, kind, name string, widthUm, heightUm float64, s *random.Stream) Registration {
	sn, cs := dmath.SinCos(pi.RotationDeg * math.Pi / 180)
	m := 1.0
	if pi.Mirrored {
		m = -1
	}
	// Scaled (ps, ps·aspect), mirrored, sheared, then turned.
	p00, p01, p11 := pi.PixelUm, mul(pi.Shear, mul(m, mul(pi.PixelUm, pi.Aspect))), mul(m, mul(pi.PixelUm, pi.Aspect))
	l00, l01 := mul(cs, p00), mul(cs, p01)-mul(sn, p11)
	l10, l11 := mul(sn, p00), mul(sn, p01)+mul(cs, p11)
	det := mul(l00, l11) - mul(l01, l10)
	reach := pi.MarginUm + mul(math.Sqrt2, pi.DeformationUm)
	u0, v0, u1, v1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range [][2]float64{{-reach, -reach}, {widthUm + reach, -reach}, {-reach, heightUm + reach}, {widthUm + reach, heightUm + reach}} {
		u := (mul(l11, c[0]) - mul(l01, c[1])) / det
		v := (mul(l00, c[1]) - mul(l10, c[0])) / det
		u0, v0, u1, v1 = math.Min(u0, u), math.Min(v0, v), math.Max(u1, u), math.Max(v1, v)
	}
	u0, v0 = math.Floor(u0), math.Floor(v0)
	r := Registration{Image: PostXeniumFolder + "/" + name, Kind: kind, Width: int(math.Ceil(u1 - u0)), Height: int(math.Ceil(v1 - v0)), Parameters: pi}
	r.Transform = [2][3]float64{{l00, l01, mul(l00, u0) + mul(l01, v0)}, {l10, l11, mul(l10, u0) + mul(l11, v0)}}
	if pi.DeformationUm > 0 {
		for axis := 0; axis < 2; axis++ {
			for k := 0; k < 3; k++ {
				fx, fy := unit(s)
				f := (0.7 + mul(0.6, s.Float64())) / pi.WavelengthUm
				r.Deformation = append(r.Deformation, Wave{Axis: axis, AmplitudeUm: mul(pi.DeformationUm/3, 0.5+mul(0.5, s.Float64())),
					Frequency: [2]float64{mul(f, fx), mul(f, fy)}, Phase: mul(2*math.Pi, s.Float64())})
			}
		}
	}
	return r
}

// sampleAt is what is at a point of the section (µm): its cell's label and its visible nucleus's (0
// for none), and whether the point is on the section at all.
func (t *tissue) sampleAt(x, y float64) (cell, nucleus uint32, on bool) {
	p := t.p
	if !(x >= 0 && y >= 0 && x < p.WidthUm && y < p.HeightUm) {
		return 0, 0, false
	}
	i, j := min(t.w-1, int(x/p.PixelUm)), min(t.h-1, int(y/p.PixelUm))
	k := j*t.w + i
	return t.labels[k], t.nuclei[k], true
}

// rows runs row(y) for every row of an image, over every core; each row's results are its own.
func rows(height int, row func(y int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for y := int(next.Add(1) - 1); y < height; y = int(next.Add(1) - 1) {
				row(y)
			}
		}()
	}
	wg.Wait()
}

// supersample is the points of the section a pixel averages: 2 × 2.
const supersample = 2

// renderHE is the H&E: each pixel's optical densities of haematoxylin and eosin, averaged over its
// points, blurred, then its light through them.
func (t *tissue) renderHE(r *Registration) *tiff.PlaneRGB {
	p := t.p
	seed := p.seed(streamPostXenium)
	// Each nucleus's haematoxylin, each cell's eosin (its type's, varied), and each type's.
	typeEosin := make([]float64, len(p.Types))
	ts := random.New(seed, streamPostXenium+1)
	for i := range typeEosin {
		typeEosin[i] = 0.18 + mul(0.22, ts.Float64())
	}
	hn := make([]float64, len(t.cells)+1)
	ec := make([]float64, len(t.cells)+1)
	for k := range t.cells {
		s := random.New(seed, streamPostXenium+1<<20+uint64(k))
		hn[k+1] = s.LogNormal(dmath.Log(0.85), 0.2)
		ec[k+1] = mul(typeEosin[t.cells[k].typ], s.LogNormal(0, 0.12))
	}
	// The stains' colours, varied for this section.
	sv := random.New(seed, streamPostXenium+2)
	stains := [2][3]float64{{0.65, 0.70, 0.29}, {0.07, 0.99, 0.11}}
	for i := range stains {
		var n float64
		for c := range stains[i] {
			stains[i][c] = mul(stains[i][c], 1+mul(0.04, sv.Normal()))
			n += mul(stains[i][c], stains[i][c])
		}
		n = math.Sqrt(n)
		for c := range stains[i] {
			stains[i][c] /= n
		}
	}
	r.Stains = &stains
	w, h := r.Width, r.Height
	odH, odE := make([]float32, w*h), make([]float32, w*h)
	rows(h, func(y int) {
		s := random.New(seed, streamPostXenium+1<<30+uint64(y))
		for x := 0; x < w; x++ {
			var sh, se float64
			for j := 0; j < supersample; j++ {
				for i := 0; i < supersample; i++ {
					px, py := r.Map(float64(x)+(float64(i)+0.5)/supersample, float64(y)+(float64(j)+0.5)/supersample)
					cell, nucleus, on := t.sampleAt(px, py)
					switch {
					case !on:
					case nucleus > 0:
						sh, se = sh+hn[nucleus], se+0.12
					case cell > 0:
						sh, se = sh+0.05, se+ec[cell]
					default:
						sh, se = sh+0.02, se+0.09
					}
				}
			}
			n := float64(supersample * supersample)
			// Chromatin's and the stain's grain.
			odH[y*w+x] = float32(mul(sh/n, math.Max(0, 1+mul(0.12, s.Normal()))))
			odE[y*w+x] = float32(mul(se/n, math.Max(0, 1+mul(0.05, s.Normal()))))
		}
	})
	odH, odE = boxBlur(odH, w, h), boxBlur(odE, w, h)
	glass := [3]float64{241, 238, 243}
	out := &tiff.PlaneRGB{Width: w, Height: h, Pix: make([]uint8, 3*w*h)}
	rows(h, func(y int) {
		s := random.New(seed, streamPostXenium+3<<30+uint64(y))
		for x := 0; x < w; x++ {
			i := y*w + x
			for c := 0; c < 3; c++ {
				od := mul(float64(odH[i]), stains[0][c]) + mul(float64(odE[i]), stains[1][c])
				v := mul(glass[c], dmath.Exp(-mul(math.Ln10, od))) + mul(1.2, s.Normal())
				out.Pix[3*i+c] = uint8(math.Max(0, math.Min(255, math.Round(v))))
			}
		}
	})
	return out
}

// renderIF is the immunofluorescence image: DAPI in the nuclei, a membrane stain on the cells'
// boundaries (where a point's neighbours 0.6 µm away belong to another cell), averaged over each
// pixel's points, blurred, on a background, with noise.
func (t *tissue) renderIF(r *Registration) []*tiff.Plane16 {
	p := t.p
	seed := p.seed(streamPostXenium)
	dapi := make([]float64, len(t.cells)+1)
	membrane := make([]float64, len(t.cells)+1)
	for k := range t.cells {
		s := random.New(seed, streamPostXenium+2<<20+uint64(k))
		dapi[k+1] = s.LogNormal(dmath.Log(2400), 0.25)
		membrane[k+1] = s.LogNormal(dmath.Log(1600), 0.3)
	}
	const reach = 0.6
	w, h := r.Width, r.Height
	planes := [2][]float32{make([]float32, w*h), make([]float32, w*h)}
	rows(h, func(y int) {
		for x := 0; x < w; x++ {
			var sd, sm float64
			for j := 0; j < supersample; j++ {
				for i := 0; i < supersample; i++ {
					px, py := r.Map(float64(x)+(float64(i)+0.5)/supersample, float64(y)+(float64(j)+0.5)/supersample)
					cell, nucleus, on := t.sampleAt(px, py)
					if !on {
						continue
					}
					sd += dapi[nucleus]
					// On a boundary when a neighbour is another cell's or none's: the membrane of the
					// cell there (the larger label where two meet).
					edge, boundary := cell, false
					for _, d := range [4][2]float64{{reach, 0}, {-reach, 0}, {0, reach}, {0, -reach}} {
						if other, _, _ := t.sampleAt(px+d[0], py+d[1]); other != cell {
							edge, boundary = max(edge, other), true
						}
					}
					switch {
					case boundary && edge > 0:
						sm += membrane[edge]
					case cell > 0:
						sm += 120
					}
				}
			}
			n := float64(supersample * supersample)
			planes[0][y*w+x], planes[1][y*w+x] = float32(sd/n), float32(sm/n)
		}
	})
	out := make([]*tiff.Plane16, 2)
	for c := range planes {
		blurred := boxBlur(planes[c], w, h)
		out[c] = &tiff.Plane16{Width: w, Height: h, Pix: make([]uint16, w*h)}
		rows(h, func(y int) {
			s := random.New(seed, streamPostXenium+uint64(c+5)<<30+uint64(y))
			for x := 0; x < w; x++ {
				v := 110 + float64(blurred[y*w+x]) + mul(9, s.Normal())
				out[c].Pix[y*w+x] = uint16(math.Max(0, math.Min(65535, math.Round(v))))
			}
		})
	}
	return out
}

// postLevels is how many halvings bring an image within 256 pixels.
func postLevels(w, h int) int {
	levels := 0
	for s := max(w, h); s > 256; s = (s + 1) / 2 {
		levels++
	}
	return levels
}

func postXML(name string, r *Registration, typ string, channels []string, software string) string {
	ps := strconv.FormatFloat(r.Parameters.PixelUm, 'f', -1, 64)
	psy := strconv.FormatFloat(mul(r.Parameters.PixelUm, r.Parameters.Aspect), 'f', -1, 64)
	sizeC, ch, data := len(channels), "", ""
	if typ == "uint8" {
		sizeC = 3
		ch = `<Channel ID="Channel:0:0" SamplesPerPixel="3"/>`
		data = `<TiffData IFD="0" PlaneCount="1"/>`
	} else {
		for c, n := range channels {
			ch += fmt.Sprintf(`<Channel ID="Channel:0:%d" Name="%s" SamplesPerPixel="1"/>`, c, html.EscapeString(n))
			data += fmt.Sprintf(`<TiffData IFD="%d" FirstC="%d" FirstZ="0" FirstT="0" PlaneCount="1"/>`, c, c)
		}
	}
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06" Creator="SituCraft simulate ` + software + `">` +
		`<Image ID="Image:0" Name="` + html.EscapeString(name) + `"><Pixels ID="Pixels:0" DimensionOrder="XYCZT" Type="` + typ + `" BigEndian="false"` +
		fmt.Sprintf(` SizeX="%d" SizeY="%d" SizeZ="1" SizeC="%d" SizeT="1"`, r.Width, r.Height, sizeC) +
		` PhysicalSizeX="` + ps + `" PhysicalSizeXUnit="µm" PhysicalSizeY="` + psy + `" PhysicalSizeYUnit="µm">` +
		ch + data + `</Pixels></Image></OME>`
}

// matrixCSV is a registration's transform without its deformation as Xenium Explorer's matrix: the
// image's pixels to the Xenium image's.
func matrixCSV(r *Registration, pixelUm float64) []byte {
	f := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	t := r.Transform
	return []byte(f(t[0][0]/pixelUm) + "," + f(t[0][1]/pixelUm) + "," + f(t[0][2]/pixelUm) + "\n" +
		f(t[1][0]/pixelUm) + "," + f(t[1][1]/pixelUm) + "," + f(t[1][2]/pixelUm) + "\n0,0,1\n")
}

// writePostXenium renders and writes the post-Xenium images the parameters ask for, and their truth.
func (wr *writer) writePostXenium() error {
	px := wr.t.p.PostXenium
	if px == nil {
		return nil
	}
	p := wr.t.p
	if err := os.MkdirAll(filepath.Join(wr.dir, PostXeniumFolder), 0o755); err != nil {
		return err
	}
	truth := filepath.Join(wr.dir, filepath.FromSlash(filepath.Dir(RegistrationFile)))
	if err := os.MkdirAll(truth, 0o755); err != nil {
		return err
	}
	placed := func(pi *PostImage, kind, name string, stream uint64) (*Registration, error) {
		r := place(*pi, kind, name, p.WidthUm, p.HeightUm, random.New(p.seed(streamPostXenium), stream))
		if float64(r.Width)*float64(r.Height) > maxPostXeniumPixels {
			return nil, fmt.Errorf("the %s would be %d × %d pixels, more than %d million: enlarge its pixels or shrink the field", kind, r.Width, r.Height, int(maxPostXeniumPixels/1e6))
		}
		return &r, nil
	}
	var made []Registration
	software := "SituCraft simulate " + wr.software
	if px.HE != nil {
		r, err := placed(px.HE, "he", heImageName, streamPostXenium+10)
		if err != nil {
			return err
		}
		img := wr.t.renderHE(r)
		if err := tiff.WriteRGBOMETIFF(filepath.Join(wr.dir, filepath.FromSlash(r.Image)), img, tiff.WriteOptions{Description: postXML("simulated H&E", r, "uint8", nil, wr.software), Software: software, Tile: 512, Levels: postLevels(r.Width, r.Height)}); err != nil {
			return err
		}
		made = append(made, *r)
	}
	if px.IF != nil {
		r, err := placed(px.IF, "if", ifImageName, streamPostXenium+11)
		if err != nil {
			return err
		}
		planes := wr.t.renderIF(r)
		if err := tiff.WriteChannelsOMETIFF(filepath.Join(wr.dir, filepath.FromSlash(r.Image)), planes, tiff.WriteOptions{Description: postXML("simulated immunofluorescence", r, "uint16", []string{"DAPI", "Membrane"}, wr.software), Software: software, Tile: 512, Levels: postLevels(r.Width, r.Height)}); err != nil {
			return err
		}
		made = append(made, *r)
	}
	if len(made) == 0 {
		return errors.New("post-Xenium images were asked for, but neither an H&E nor an immunofluorescence image")
	}
	for i := range made {
		name := filepath.Base(made[i].Image)
		stem := name[:len(name)-len("_image.ome.tif")]
		if err := os.WriteFile(filepath.Join(truth, stem+"_imagealignment.csv"), matrixCSV(&made[i], p.PixelUm), 0o644); err != nil {
			return err
		}
	}
	wr.postXenium = made
	return writeJSON(filepath.Join(truth, "registration.json"), map[string]any{
		"format": "situcraft-simulated-registration", "version": 1,
		"about": "Where each simulated post-Xenium image lies on the section: a pixel q of the image (full resolution, (0, 0) the corner of its first) is at x = transform·q (µm, the tissue's coordinates), then displaced by the deformation: along each axis, the sum of its waves' amplitude_um × sin(2π (frequency_per_um · x) + phase). " +
			"<image>_imagealignment.csv beside this is the transform without its deformation, as Xenium Explorer writes an alignment (the image's pixels to the Xenium image's).",
		"xenium_pixel_um": p.PixelUm,
		"images":          made,
	})
}

// postXeniumAbout says what of the post-Xenium images a simulated bundle's description says.
func postXeniumAbout(made []Registration) string {
	if len(made) == 0 {
		return ""
	}
	return " Its post-Xenium images (an H&E and an immunofluorescence image of the same cells) are in " + PostXeniumFolder + "/, each placed by a known transform and deformation: their truth is " + RegistrationFile + "."
}
