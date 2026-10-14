package xenium

// The morphology images: the in-focus channels (DAPI, and the stains of multimodal segmentation),
// each a plane of one OME-TIFF image that may span several files, and the z-stack.

import (
	"fmt"
	"path"

	"situcraft/internal/tiff"
)

// Level is one resolution of an image plane.
type Level struct {
	Width, Height int
	Tiles         int
	TileWidth     int
	TileHeight    int
	Compression   int
}

// Channel is one morphology image plane.
type Channel struct {
	Index   int
	Name    string // "DAPI", "ATP1A1/CD45/E-Cadherin", …
	Purpose string // Xenium's MapAnnotation: "Nuclear", "Boundary", "Interior RNA", "Interior protein"
	File    string // the bundle file holding it
	IFD     int
	Z       int
	// Levels are the plane's resolutions, full first.
	Levels []Level
	// PixelSize is the plane's physical pixel size in µm (0 when the OME-XML does not say).
	PixelSize float64
	Bits      int
}

// Image describes one OME-TIFF image of the bundle: its channels (and z planes).
type Image struct {
	Role      string // "morphology_focus" or "morphology"
	Files     []string
	Channels  []Channel
	SizeZ     int
	PixelType string
}

// ReadImage describes an image from the OME-XML of its first file and the image directories of
// every file it spans.
func (b *Bundle) ReadImage(role, first string) (*Image, error) {
	img := &Image{Role: role}
	files := map[string]*tiff.File{}
	openTIFF := func(name string) (*tiff.File, error) {
		if t, ok := files[name]; ok {
			return t, nil
		}
		f, err := b.OpenFile(name)
		if err != nil {
			return nil, err
		}
		t, err := tiff.Open(f, f.Size())
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		// The descriptions are read; the file is closed (tiles are read through new handles later).
		f.Close()
		files[name] = t
		img.Files = append(img.Files, name)
		return t, nil
	}
	t, err := openTIFF(first)
	if err != nil {
		return nil, err
	}
	ome, err := tiff.ParseOME(t.IFDs[0].Description)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", first, err)
	}
	px := ome.Images[0].Pixels
	img.SizeZ, img.PixelType = px.SizeZ, px.Type
	planes, err := px.Planes(ome.UUID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", first, err)
	}
	for _, p := range planes {
		file := first
		if p.FileName != "" {
			file = path.Join(path.Dir(first), p.FileName)
		}
		tf, err := openTIFF(file)
		if err != nil {
			return nil, err
		}
		if p.IFD >= len(tf.IFDs) {
			return nil, fmt.Errorf("%s: plane z=%d c=%d is in image directory %d, but the file has %d", file, p.Z, p.C, p.IFD, len(tf.IFDs))
		}
		ifd := tf.IFDs[p.IFD]
		ch := Channel{Index: p.C, File: file, IFD: p.IFD, Z: p.Z, PixelSize: px.PhysicalSizeX, Bits: ifd.BitsPerSample[0]}
		if p.C < len(px.Channels) {
			ch.Name = px.Channels[p.C].Name
			ch.Purpose = px.Channels[p.C].Annotations["Purpose"]
		}
		for _, level := range append([]*tiff.IFD{ifd}, ifd.SubIFDs...) {
			ch.Levels = append(ch.Levels, Level{Width: level.Width, Height: level.Height, Tiles: len(level.Offsets), TileWidth: level.BlockWidth, TileHeight: level.BlockHeight, Compression: level.Compression})
		}
		img.Channels = append(img.Channels, ch)
	}
	return img, nil
}

// FocusImage describes the morphology-focus image (nil, nil when the bundle has none).
func (b *Bundle) FocusImage() (*Image, error) {
	files := b.focusFiles()
	if len(files) == 0 {
		return nil, nil
	}
	first := b.experimentImage(b.Experiment.Images.MorphologyFocus, files[0])
	return b.ReadImage("morphology_focus", first)
}

// ZStack describes the morphology z-stack (nil, nil when the bundle has none).
func (b *Bundle) ZStack() (*Image, error) {
	name := b.experimentImage(b.Experiment.Images.Morphology, "morphology.ome.tif")
	if !b.Has(name) {
		return nil, nil
	}
	return b.ReadImage("morphology", name)
}
