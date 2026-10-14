package cache

import (
	"encoding/json"
	"fmt"
)

// Check opens each ready entry of a status with its reader and checks that its files hold what
// its summary says: every image tile of every level present (the coarsest decoded), as many
// transcripts in the tiles as were read and in the coarsest density grid of every gene together,
// and each level's polygons and vertices. It reads tables, not whole entries, so it is quick.
func (c *Cache) Check(s *Status) error {
	for _, e := range s.Entries {
		if e.State != "ready" {
			continue
		}
		dir := c.EntryDir(e.Name)
		var err error
		switch e.Kind {
		case KindImage:
			err = checkImageEntry(dir)
		case KindTranscripts:
			err = checkTranscriptEntry(dir)
		case KindGeometry:
			err = checkGeometryEntry(dir)
		}
		if err != nil {
			return fmt.Errorf("%s (%s): %w", e.Title, e.Name, err)
		}
	}
	return nil
}

func checkImageEntry(dir string) error {
	m, err := readManifest(dir)
	if err != nil {
		return err
	}
	var sum ImageSummary
	if err := json.Unmarshal(m.Summary, &sum); err != nil {
		return err
	}
	for ci, ch := range sum.Channels {
		for l, lv := range ch.Levels {
			f, err := OpenImage(dir, ci, l)
			if err != nil {
				return err
			}
			if f.Width != lv.Width || f.Height != lv.Height || f.Across != lv.Across || f.Down != lv.Down {
				f.Close()
				return fmt.Errorf("channel %s level %d is %d×%d, its summary says %d×%d", ch.Name, l, f.Width, f.Height, lv.Width, lv.Height)
			}
			for i, e := range f.t.Table {
				if e.Length == 0 {
					f.Close()
					return fmt.Errorf("channel %s level %d has no tile %d", ch.Name, l, i)
				}
			}
			if l == len(ch.Levels)-1 {
				for ty := 0; ty < f.Down; ty++ {
					for tx := 0; tx < f.Across; tx++ {
						if _, _, _, err := f.Tile(tx, ty); err != nil {
							f.Close()
							return fmt.Errorf("channel %s level %d tile (%d, %d): %w", ch.Name, l, tx, ty, err)
						}
					}
				}
			}
			f.Close()
		}
	}
	return nil
}

func checkTranscriptEntry(dir string) error {
	idx, err := OpenTranscripts(dir)
	if err != nil {
		return err
	}
	defer idx.Close()
	var points int64
	for _, f := range idx.points {
		for _, e := range f.Table {
			points += int64(e.A)
		}
	}
	if points != idx.Summary.Transcripts {
		return fmt.Errorf("the tiles hold %d transcripts, %d were read", points, idx.Summary.Transcripts)
	}
	if len(idx.Summary.DensityLevels) == 0 {
		return nil
	}
	top := idx.Summary.DensityLevels[len(idx.Summary.DensityLevels)-1]
	all := uint16(len(idx.Summary.Genes))
	var n int64
	g := idx.Params.Grid
	for ty := 0; ty < g.Down(top); ty++ {
		for tx := 0; tx < g.Across(top); tx++ {
			d, err := idx.Density(top, tx, ty)
			if err != nil {
				return err
			}
			for k, gene := range d.Genes {
				if gene == all {
					for i := d.Starts[k]; i < d.Starts[k+1]; i++ {
						n += int64(d.N[i])
					}
				}
			}
		}
	}
	if n != idx.Summary.Transcripts {
		return fmt.Errorf("level %d's density grid of every gene sums to %d, not %d", top, n, idx.Summary.Transcripts)
	}
	return nil
}

func checkGeometryEntry(dir string) error {
	geo, err := OpenGeometry(dir)
	if err != nil {
		return err
	}
	defer geo.Close()
	for _, set := range geo.Summary.Sets {
		for l, lv := range set.Levels {
			f := geo.files[geometryFile(set.Name, l)]
			var polygons, vertices int
			for _, e := range f.Table {
				polygons += int(e.A)
				vertices += int(e.B)
			}
			if polygons != set.Polygons || vertices != lv.Vertices {
				return fmt.Errorf("%s level %d holds %d polygons and %d vertices; its summary says %d and %d", set.Name, l, polygons, vertices, set.Polygons, lv.Vertices)
			}
		}
	}
	return nil
}
