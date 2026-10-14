package cache

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
)

// ImageEntry is an open image entry: its summary and, opened as they are first read, its channels'
// levels.
type ImageEntry struct {
	Summary ImageSummary
	dir     string
	mu      sync.Mutex
	files   map[[2]int]*imageFile
}

// OpenImageEntry opens a complete image entry.
func OpenImageEntry(dir string) (*ImageEntry, error) {
	m, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	e := &ImageEntry{dir: dir, files: map[[2]int]*imageFile{}}
	if err := json.Unmarshal(m.Summary, &e.Summary); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *ImageEntry) level(channel, level int) (*imageFile, error) {
	if channel < 0 || channel >= len(e.Summary.Channels) || level < 0 || level >= len(e.Summary.Channels[channel].Levels) {
		return nil, fmt.Errorf("no channel %d level %d", channel, level)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	key := [2]int{channel, level}
	if f, ok := e.files[key]; ok {
		return f, nil
	}
	f, err := openImageFile(filepath.Join(e.dir, imageFileName(channel, level)))
	if err != nil {
		return nil, err
	}
	lv := e.Summary.Channels[channel].Levels[level]
	if f.Width != lv.Width || f.Height != lv.Height {
		f.Close()
		return nil, errDamaged
	}
	e.files[key] = f
	return f, nil
}

// RawTile is a tile as stored (a zlib stream of its samples split into a low-byte and a
// high-byte plane) and its size.
func (e *ImageEntry) RawTile(channel, level, tx, ty int) ([]byte, int, int, error) {
	f, err := e.level(channel, level)
	if err != nil {
		return nil, 0, 0, err
	}
	return f.RawTile(tx, ty)
}

// Tile is a tile's samples, row-major, and its size.
func (e *ImageEntry) Tile(channel, level, tx, ty int) ([]uint16, int, int, error) {
	f, err := e.level(channel, level)
	if err != nil {
		return nil, 0, 0, err
	}
	return f.Tile(tx, ty)
}

// Close releases the entry's files.
func (e *ImageEntry) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, f := range e.files {
		f.Close()
	}
	e.files = map[[2]int]*imageFile{}
	return nil
}

// RawTile is a set's tile of a level as stored (a zlib stream of its columns) with its numbers
// of polygons and vertices.
func (g *Geometry) RawTile(set string, level, tx, ty int) ([]byte, int, int, error) {
	f, ok := g.files[geometryFile(set, level)]
	if !ok {
		return nil, 0, 0, fmt.Errorf("no %s at level %d", set, level)
	}
	if tx < 0 || ty < 0 || tx >= int(f.Fields[1]) || ty >= int(f.Fields[2]) {
		return nil, 0, 0, fmt.Errorf("no tile (%d, %d) at level %d", tx, ty, level)
	}
	block, e, err := f.block(ty*int(f.Fields[1]) + tx)
	if err != nil {
		return nil, 0, 0, err
	}
	return block, int(e.A), int(e.B), nil
}

// Set is a polygon set's summary, if the entry has the set.
func (g *Geometry) Set(name string) (SetSummary, bool) {
	for _, s := range g.Summary.Sets {
		if s.Name == name {
			return s, true
		}
	}
	return SetSummary{}, false
}
