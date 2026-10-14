// Package zarr reads Zarr arrays (format 2, and format 3 without sharding) from directory and zip
// stores: the Xenium bundle's cells.zarr.zip, transcripts.zarr.zip, cell_feature_matrix.zarr.zip
// and analysis.zarr.zip, and the SpatialData and AnnData stores SituCraft reads and writes. Written
// from the Zarr storage specifications (version 2; version 3 core) and numcodecs' codec
// descriptions, and checked against the zarr and numcodecs Python packages on the development
// bundles' arrays.
package zarr

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNotFound is returned for a key the store does not hold (a chunk never written).
var ErrNotFound = errors.New("zarr: key not found")

// maxValueBytes bounds a single stored value (a chunk or a metadata document).
const maxValueBytes = 1 << 31

// Store is a key-value view of a Zarr hierarchy; keys use "/" ("cells/.zarray", "masks/0/0.0").
type Store interface {
	Get(key string) ([]byte, error)
	// Keys lists every key, sorted.
	Keys() ([]string, error)
}

type zipStore struct {
	files map[string]*zip.File
	keys  []string
}

// OpenZip reads a zip archive as a store (as Xenium writes its Zarr stores: members uncompressed).
func OpenZip(r io.ReaderAt, size int64) (Store, error) {
	archive, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("zarr: %w", err)
	}
	s := &zipStore{files: map[string]*zip.File{}}
	for _, f := range archive.File {
		name := strings.TrimPrefix(path.Clean("/"+f.Name), "/")
		if strings.HasSuffix(f.Name, "/") || name == "" {
			continue
		}
		s.files[name] = f
		s.keys = append(s.keys, name)
	}
	sort.Strings(s.keys)
	return s, nil
}

func (s *zipStore) Get(key string) ([]byte, error) {
	f, ok := s.files[key]
	if !ok {
		return nil, ErrNotFound
	}
	if f.UncompressedSize64 > maxValueBytes {
		return nil, fmt.Errorf("zarr: %s declares %d bytes", key, f.UncompressedSize64)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("zarr: %s: %w", key, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, int64(f.UncompressedSize64)+1))
	if err != nil {
		return nil, fmt.Errorf("zarr: %s: %w", key, err)
	}
	if uint64(len(data)) != f.UncompressedSize64 {
		return nil, fmt.Errorf("zarr: %s is not the size its archive records", key)
	}
	return data, nil
}

func (s *zipStore) Keys() ([]string, error) { return s.keys, nil }

type dirStore struct{ root string }

// OpenDir reads a directory as a store. Keys never leave it.
func OpenDir(root string) Store { return dirStore{root: root} }

func (s dirStore) Get(key string) ([]byte, error) {
	clean := path.Clean("/" + key)
	if clean == "/" || strings.Contains(key, "\\") {
		return nil, fmt.Errorf("zarr: invalid key %q", key)
	}
	full := filepath.Join(s.root, filepath.FromSlash(clean[1:]))
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, ErrNotFound
	}
	if info.Size() > maxValueBytes {
		return nil, fmt.Errorf("zarr: %s is %d bytes", key, info.Size())
	}
	return os.ReadFile(full)
}

func (s dirStore) Keys() ([]string, error) {
	var keys []string
	err := filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return err
		}
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(keys)
	return keys, err
}
