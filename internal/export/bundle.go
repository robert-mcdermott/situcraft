package export

import (
	"path/filepath"

	"situcraft/internal/xenium"
)

// bundleDataset is a Xenium output bundle as the dataset an export reads.
type bundleDataset struct{ b *xenium.Bundle }

// FromBundle is a bundle as an export's dataset.
func FromBundle(b *xenium.Bundle) Dataset { return bundleDataset{b} }

func (d bundleDataset) Name() string { return d.b.Name }

func (d bundleDataset) Transcripts(fn func(*xenium.TranscriptBatch) error) error {
	t, err := d.b.OpenTranscripts()
	if err != nil {
		return err
	}
	defer t.Close()
	return t.Each(fn)
}

// GeneIDs are the gene panel's Ensembl identifiers, else the cell-feature matrix's.
func (d bundleDataset) GeneIDs() map[string]string {
	out := map[string]string{}
	if d.b.Panel != nil {
		for _, t := range d.b.Panel.Targets {
			if t.ID != "" {
				out[t.Name] = t.ID
			}
		}
	}
	if len(out) == 0 {
		if m, err := d.b.ReadMatrix(); err == nil {
			for _, f := range m.Features {
				out[f.Name] = f.ID
			}
		}
	}
	return out
}

func (d bundleDataset) NucleusMask() (Mask, error) {
	if !d.b.Has("cells.zarr.zip") {
		return nil, nil
	}
	m, err := d.b.OpenMask(xenium.NucleusMask)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (d bundleDataset) ImageSize() (int, int) {
	img, err := d.b.FocusImage()
	if err != nil || img == nil || len(img.Channels) == 0 || len(img.Channels[0].Levels) == 0 {
		return 0, 0
	}
	return img.Channels[0].Levels[0].Width, img.Channels[0].Levels[0].Height
}

func (d bundleDataset) BundleFolder() string {
	if d.b.Zipped {
		return ""
	}
	if filepath.Base(d.b.Path) == "experiment.xenium" {
		return filepath.Dir(d.b.Path)
	}
	return d.b.Path
}

func (d bundleDataset) Run() map[string]string {
	e := d.b.Experiment
	return map[string]string{"experimentUuid": e.ExperimentUUID, "analysisUuid": e.AnalysisUUID, "run": e.RunName, "region": e.RegionName, "bundle": d.b.Path}
}
