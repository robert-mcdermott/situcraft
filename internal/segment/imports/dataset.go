package imports

import (
	"errors"

	"situcraft/internal/xenium"
)

// bundleDataset is a Xenium bundle as the dataset an import segments.
type bundleDataset struct {
	b             *xenium.Bundle
	width, height int
	pixelSize     float64
}

// BundleDataset is a bundle as a dataset: its pixel grid is its segmentation masks' (else its
// morphology image's), at the manifest's pixel size.
func BundleDataset(b *xenium.Bundle) (Dataset, error) {
	d := &bundleDataset{b: b, pixelSize: b.Experiment.PixelSize}
	if b.Has("cells.zarr.zip") {
		if m, err := b.OpenMask(xenium.NucleusMask); err == nil {
			d.width, d.height = m.Size()
			if !(d.pixelSize > 0) {
				d.pixelSize = m.PixelSize
			}
			m.Close()
		}
	}
	if d.width == 0 {
		img, err := b.FocusImage()
		if err != nil || img == nil || len(img.Channels) == 0 || len(img.Channels[0].Levels) == 0 {
			return nil, errors.New("the dataset's image size is unknown (no segmentation masks or morphology image)")
		}
		d.width, d.height = img.Channels[0].Levels[0].Width, img.Channels[0].Levels[0].Height
	}
	if !(d.pixelSize > 0) {
		return nil, errors.New("the dataset's pixel size is unknown")
	}
	return d, nil
}

func (d *bundleDataset) Name() string { return d.b.Name }

func (d *bundleDataset) Grid() (int, int, float64) { return d.width, d.height, d.pixelSize }

func (d *bundleDataset) Run() RunIdentity { return runOf(d.b) }

func runOf(b *xenium.Bundle) RunIdentity {
	e := b.Experiment
	return RunIdentity{ExperimentUUID: e.ExperimentUUID, Name: e.RunName, Region: e.RegionName, Start: e.RunStartTime}
}

func (d *bundleDataset) Transcripts(fn func(*xenium.TranscriptBatch) error) error {
	t, err := d.b.OpenTranscripts()
	if err != nil {
		return err
	}
	defer t.Close()
	return t.Each(fn)
}
