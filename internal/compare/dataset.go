package compare

import (
	"situcraft/internal/xenium"
)

// BundleTranscripts reads a bundle's transcripts as the comparison needs them.
func BundleTranscripts(b *xenium.Bundle) (*Transcripts, error) {
	tr, err := b.OpenTranscripts()
	if err != nil {
		return nil, err
	}
	defer tr.Close()
	t := &Transcripts{}
	err = tr.Each(func(batch *xenium.TranscriptBatch) error {
		for i := 0; i < batch.Rows; i++ {
			t.X, t.Y = append(t.X, batch.X[i]), append(t.Y, batch.Y[i])
			t.Q20Gene = append(t.Q20Gene, batch.QV[i] >= 20 && xenium.FeatureKind(string(batch.Feature[i])) == xenium.KindGene)
		}
		return nil
	})
	return t, err
}
