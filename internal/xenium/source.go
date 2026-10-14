package xenium

// A bundle's files, read where they are: a directory, or a zip archive (the form 10x Genomics
// publishes bundles in). Nothing here writes to the source. Zip members stored without
// compression are read in place; compressed ones are inflated once into the cache directory
// (checked by their CRC-32) the first time they are opened, so that readers get random access.

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ReaderAt is an open file of the bundle.
type ReaderAt interface {
	io.ReaderAt
	io.Closer
	Size() int64
}

// FileInfo describes one file of the bundle.
type FileInfo struct {
	Path     string `json:"path"` // relative to the bundle, with "/"
	Size     int64  `json:"size"`
	ModTime  int64  `json:"modTime"`            // Unix seconds (the archive's when zipped)
	Stored   bool   `json:"stored,omitempty"`   // in a zip: stored without compression (read in place)
	External bool   `json:"external,omitempty"` // in a zip: needs inflating before random access
}

type source interface {
	files() []FileInfo
	open(name string) (ReaderAt, error)
	// identity names a file's current state cheaply (place, size and time; no reading).
	identity(name string) (string, error)
	close() error
}

type osFile struct {
	*os.File
	size int64
}

func (f osFile) Size() int64 { return f.size }

// dirSource is a bundle folder.
type dirSource struct {
	root  string
	infos []FileInfo
}

func openDir(root string) (*dirSource, error) {
	s := &dirSource{root: root}
	// Links are followed (bundles often sit behind them on shared storage), each real folder
	// visited once so that a link back up cannot loop.
	visited := map[string]bool{}
	var walk func(dir, rel string, depth int) error
	walk = func(dir, rel string, depth int) error {
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return err
		}
		if visited[real] || depth > 8 {
			return nil
		}
		visited[real] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			full := filepath.Join(dir, e.Name())
			info, err := os.Stat(full)
			if err != nil {
				continue // a broken link
			}
			name := path.Join(rel, e.Name())
			if info.IsDir() {
				if err := walk(full, name, depth+1); err != nil {
					return err
				}
				continue
			}
			s.infos = append(s.infos, FileInfo{Path: name, Size: info.Size(), ModTime: info.ModTime().Unix()})
			if len(s.infos) > maxBundleFiles {
				return fmt.Errorf("more than %d files: not an output bundle", maxBundleFiles)
			}
		}
		return nil
	}
	if err := walk(root, "", 0); err != nil {
		return nil, err
	}
	sort.Slice(s.infos, func(i, j int) bool { return s.infos[i].Path < s.infos[j].Path })
	return s, nil
}

func (s *dirSource) files() []FileInfo { return s.infos }

func (s *dirSource) open(name string) (ReaderAt, error) {
	clean := path.Clean("/" + name)
	if clean == "/" {
		return nil, fs.ErrNotExist
	}
	f, err := os.Open(filepath.Join(s.root, filepath.FromSlash(clean[1:])))
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return osFile{f, info.Size()}, nil
}

func (s *dirSource) identity(name string) (string, error) {
	clean := path.Clean("/" + name)
	full, err := filepath.Abs(filepath.Join(s.root, filepath.FromSlash(clean[1:])))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("file\x00%s\x00%d\x00%d", full, info.Size(), info.ModTime().UnixNano()), nil
}

func (s *dirSource) close() error { return nil }

// zipSource is a zipped bundle.
type zipSource struct {
	file    *os.File
	archive *zip.Reader
	prefix  string // the folder inside the archive holding experiment.xenium ("" at the root)
	members map[string]*zip.File
	infos   []FileInfo
	// cache is where compressed members are inflated, under a folder named for the archive.
	cache string
	// archive is the zip's own place, size and time, which every member's identity includes.
	whole string
	mu    sync.Mutex
}

type sectionFile struct {
	*io.SectionReader
}

func (s sectionFile) Close() error { return nil }

func openZip(archivePath, cacheDir string) (*zipSource, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	archive, err := zip.NewReader(f, info.Size())
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("not a readable zip archive: %w", err)
	}
	s := &zipSource{file: f, archive: archive, members: map[string]*zip.File{}}
	// The bundle may sit at the archive's root or in one folder inside it.
	for _, m := range archive.File {
		name := path.Clean("/" + m.Name)[1:]
		if path.Base(name) == manifestName && strings.Count(name, "/") <= 1 {
			if dir := path.Dir(name); dir != "." {
				s.prefix = dir + "/"
			}
			break
		}
	}
	for _, m := range archive.File {
		name := path.Clean("/" + m.Name)[1:]
		if strings.HasSuffix(m.Name, "/") || !strings.HasPrefix(name, s.prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, s.prefix)
		if rel == "" || strings.HasPrefix(path.Base(rel), ".") {
			continue
		}
		s.members[rel] = m
		s.infos = append(s.infos, FileInfo{Path: rel, Size: int64(m.UncompressedSize64), ModTime: m.Modified.Unix(), Stored: m.Method == zip.Store, External: m.Method != zip.Store})
	}
	if len(s.infos) > maxBundleFiles {
		f.Close()
		return nil, fmt.Errorf("more than %d files: not an output bundle", maxBundleFiles)
	}
	sort.Slice(s.infos, func(i, j int) bool { return s.infos[i].Path < s.infos[j].Path })
	identity := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", archivePath, info.Size(), info.ModTime().UnixNano())))
	s.cache = filepath.Join(cacheDir, "unzipped", hex.EncodeToString(identity[:8]))
	abs, err := filepath.Abs(archivePath)
	if err != nil {
		f.Close()
		return nil, err
	}
	s.whole = fmt.Sprintf("zip\x00%s\x00%d\x00%d", abs, info.Size(), info.ModTime().UnixNano())
	return s, nil
}

func (s *zipSource) identity(name string) (string, error) {
	m, ok := s.members[name]
	if !ok {
		return "", fs.ErrNotExist
	}
	return fmt.Sprintf("%s\x00%s\x00%d\x00%08x", s.whole, name, m.UncompressedSize64, m.CRC32), nil
}

func (s *zipSource) files() []FileInfo { return s.infos }

func (s *zipSource) open(name string) (ReaderAt, error) {
	m, ok := s.members[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if m.Method == zip.Store {
		offset, err := m.DataOffset()
		if err != nil {
			return nil, err
		}
		return sectionFile{io.NewSectionReader(s.file, offset, int64(m.UncompressedSize64))}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	target := filepath.Join(s.cache, filepath.FromSlash(name))
	if info, err := os.Stat(target); err != nil || info.Size() != int64(m.UncompressedSize64) {
		if err := inflate(m, target); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	f, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	// The unpacked copy is in use: its time keeps the cache's eviction from taking it first.
	now := time.Now()
	_ = os.Chtimes(s.cache, now, now)
	return osFile{f, int64(m.UncompressedSize64)}, nil
}

// inflate writes a compressed member to target through a temporary file, checking its CRC-32.
func inflate(m *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := m.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp, err := os.CreateTemp(filepath.Dir(target), ".inflate-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	hash := crc32.NewIEEE()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(rc, int64(m.UncompressedSize64)+1))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if n != int64(m.UncompressedSize64) || hash.Sum32() != m.CRC32 {
		return errors.New("the archive member does not match its size and CRC-32")
	}
	return os.Rename(tmp.Name(), target)
}

func (s *zipSource) close() error { return s.file.Close() }
