// Package cache keeps the data SituCraft derives from a bundle: image tile pyramids, the transcript
// spatial index and geometry tiles. Bundles are read in place and never written; what is derived
// from them lives here, in entries that can be deleted at any time and made again.
//
// An entry is a folder named for its kind and a key: the hash of the cache format, the entry's
// parameters and the SHA-256 of every source file it is built from. So a bundle that moves keeps
// its entries, and one whose files change gets new ones. An entry is built in a folder of its
// own under building/ (a crash leaves at most that folder, which the next build resumes, one
// finished file at a time) and renamed into entries/ when complete, with a manifest recording
// its sources and the size and SHA-256 of each of its files. The manifest and the data files are
// deterministic: an interrupted and resumed build gives the same bytes as one that was not.
//
// The cache has a size limit. When it is exceeded the entries used least recently are removed,
// never one used since the program started.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FormatVersion is the version of every file format the cache writes. Changing a format changes
// it, and with it every entry's key, so old entries are never read (they age out by eviction).
const FormatVersion = 2

// Cache is the derived-data cache in one folder.
type Cache struct {
	root    string // the cache folder (zipped bundles' members are unpacked under it too)
	dir     string // root/derived
	limit   int64
	started time.Time

	mu   sync.Mutex
	memo map[string]memoEntry
	// heartbeat is how often a build touches its lock; a lock older than staleLock is abandoned.
	heartbeat, staleLock time.Duration
}

// Open opens (creating) the cache in folder root with a size limit in bytes (0: no limit).
func Open(root string, limit int64) (*Cache, error) {
	c := &Cache{root: root, dir: filepath.Join(root, "derived"), limit: limit, started: time.Now(), heartbeat: 10 * time.Second, staleLock: time.Minute}
	for _, d := range []string{c.entriesDir(), c.buildingDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	c.memo = c.loadMemo()
	return c, nil
}

// Root is the cache folder.
func (c *Cache) Root() string { return c.root }

// Limit is the size limit in bytes (0: none).
func (c *Cache) Limit() int64 { return c.limit }

func (c *Cache) entriesDir() string  { return filepath.Join(c.dir, "entries") }
func (c *Cache) buildingDir() string { return filepath.Join(c.dir, "building") }

// EntryDir is where a complete entry's files are.
func (c *Cache) EntryDir(name string) string { return filepath.Join(c.entriesDir(), name) }

// SourceFile is one file an entry is built from.
type SourceFile struct {
	Path   string `json:"path"` // in the bundle
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Spec says what an entry is: its kind, its parameters and its sources. Its name follows.
type Spec struct {
	Kind    string       `json:"kind"`
	Params  any          `json:"params"`
	Sources []SourceFile `json:"sources"`
}

// Name is the entry's folder name: its kind and a key hashing the format version, the parameters
// and the sources' checksums.
func (s Spec) Name() (string, error) {
	params, err := json.Marshal(s.Params)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "situcraft-cache\x00%d\x00%s\x00%s", FormatVersion, s.Kind, params)
	for _, src := range s.Sources {
		fmt.Fprintf(h, "\x00%s\x00%d\x00%s", src.Path, src.Size, src.SHA256)
	}
	return s.Kind + "-" + hex.EncodeToString(h.Sum(nil))[:24], nil
}

// FileSum describes one file of an entry.
type FileSum struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest describes a complete entry. It holds nothing that differs between two builds of the
// same entry (the build's times are in build.json).
type Manifest struct {
	Format  int             `json:"format"`
	Kind    string          `json:"kind"`
	Name    string          `json:"name"`
	Params  json.RawMessage `json:"params"`
	Sources []SourceFile    `json:"sources"`
	Files   []FileSum       `json:"files"`
	Summary json.RawMessage `json:"summary"`
}

// Size is the total size of the entry's files.
func (m *Manifest) Size() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// BuildInfo records how an entry was built.
type BuildInfo struct {
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Seconds  float64   `json:"seconds"`
	// Resumed is true when the build continued one that had been interrupted.
	Resumed bool   `json:"resumed"`
	Program string `json:"program"`
}

const (
	manifestName = "manifest.json"
	buildName    = "build.json"
	lockName     = "lock"
	tmpSuffix    = ".tmp"
)

// Lookup finds a complete entry and marks it used. Its files are checked to be present with the
// sizes the manifest records (their checksums are checked by Verify).
func (c *Cache) Lookup(name string) (*Manifest, bool) {
	dir := c.EntryDir(name)
	m, err := readManifest(dir)
	if err != nil || m.Name != name || m.Format != FormatVersion {
		return nil, false
	}
	for _, f := range m.Files {
		info, err := os.Stat(filepath.Join(dir, f.Name))
		if err != nil || info.Size() != f.Size {
			return nil, false
		}
	}
	now := time.Now()
	_ = os.Chtimes(dir, now, now)
	return m, true
}

// Verify checks every file of a complete entry against the checksum its manifest records.
func (c *Cache) Verify(name string) error {
	dir := c.EntryDir(name)
	m, err := readManifest(dir)
	if err != nil {
		return err
	}
	for _, f := range m.Files {
		size, sum, err := hashFile(filepath.Join(dir, f.Name))
		if err != nil {
			return err
		}
		if size != f.Size || sum != f.SHA256 {
			return fmt.Errorf("%s/%s does not match its manifest", name, f.Name)
		}
	}
	return nil
}

func readManifest(dir string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(dir), err)
	}
	return &m, nil
}

func hashFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// ErrBusy is returned when another build holds an entry's building folder.
var ErrBusy = errors.New("another build of this entry is running")

// building is an entry being built.
type building struct {
	c       *Cache
	spec    Spec
	name    string
	dir     string
	resumed bool
	started time.Time
	stop    chan struct{}
	stopped sync.WaitGroup
}

// begin takes (or resumes) the building folder of an entry.
func (c *Cache) begin(spec Spec) (*building, error) {
	name, err := spec.Name()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(c.buildingDir(), name)
	entries, _ := os.ReadDir(dir)
	resumed := false
	for _, e := range entries {
		if e.Name() != lockName && !strings.HasSuffix(e.Name(), tmpSuffix) {
			resumed = true
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := c.lock(dir); err != nil {
		return nil, err
	}
	b := &building{c: c, spec: spec, name: name, dir: dir, resumed: resumed, started: time.Now(), stop: make(chan struct{})}
	b.stopped.Add(1)
	go func() {
		defer b.stopped.Done()
		tick := time.NewTicker(c.heartbeat)
		defer tick.Stop()
		for {
			select {
			case <-b.stop:
				return
			case now := <-tick.C:
				_ = os.Chtimes(filepath.Join(dir, lockName), now, now)
			}
		}
	}()
	return b, nil
}

// lock creates dir's lock file, taking over one whose holder stopped touching it.
func (c *Cache) lock(dir string) error {
	path := filepath.Join(dir, lockName)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			return f.Close()
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		info, statErr := os.Stat(path)
		if statErr == nil && time.Since(info.ModTime()) < c.staleLock {
			return ErrBusy
		}
		_ = os.Remove(path)
	}
	return ErrBusy
}

// release gives up the building folder, keeping what was built for a later resume.
func (b *building) release() {
	select {
	case <-b.stop:
		return
	default:
	}
	close(b.stop)
	b.stopped.Wait()
	_ = os.Remove(filepath.Join(b.dir, lockName))
}

func (b *building) path(name string) string { return filepath.Join(b.dir, name) }

// done reports whether a file of the entry was finished (by this build or an interrupted one).
func (b *building) done(name string) bool {
	_, err := os.Stat(b.path(name))
	return err == nil
}

// create opens a temporary file that becomes name when finished with commitFile.
func (b *building) create(name string) (*os.File, error) {
	return os.Create(b.path(name) + tmpSuffix)
}

// commitFile makes a finished temporary file durable and gives it its name.
func (b *building) commitFile(f *os.File, name string) error {
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(b.path(name)+tmpSuffix, b.path(name))
}

// writeFile writes a whole file of the entry.
func (b *building) writeFile(name string, data []byte) error {
	f, err := b.create(name)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return b.commitFile(f, name)
}

// remove deletes intermediate files once they are no longer needed.
func (b *building) remove(names ...string) {
	for _, n := range names {
		_ = os.Remove(b.path(n))
	}
}

// commit finishes the entry: its finished files are hashed into the manifest (unfinished
// temporary files are deleted) and the folder is renamed into entries/.
func (b *building) commit(summary any) (*Manifest, error) {
	defer b.release()
	params, err := json.Marshal(b.spec.Params)
	if err != nil {
		return nil, err
	}
	rawSummary, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	m := &Manifest{Format: FormatVersion, Kind: b.spec.Kind, Name: b.name, Params: params, Sources: b.spec.Sources, Summary: rawSummary}
	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		n := e.Name()
		if n == lockName || n == manifestName || n == buildName || strings.HasSuffix(n, tmpSuffix) {
			if strings.HasSuffix(n, tmpSuffix) {
				_ = os.Remove(b.path(n))
			}
			continue
		}
		size, sum, err := hashFile(b.path(n))
		if err != nil {
			return nil, err
		}
		m.Files = append(m.Files, FileSum{Name: n, Size: size, SHA256: sum})
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Name < m.Files[j].Name })
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	finished := time.Now()
	info, _ := json.MarshalIndent(BuildInfo{Started: b.started, Finished: finished, Seconds: finished.Sub(b.started).Seconds(), Resumed: b.resumed, Program: program}, "", "  ")
	if err := b.writeFile(buildName, info); err != nil {
		return nil, err
	}
	if err := b.writeFile(manifestName, append(raw, '\n')); err != nil {
		return nil, err
	}
	b.release()
	target := b.c.EntryDir(b.name)
	if _, err := os.Stat(target); err == nil {
		// Another program finished the same entry first: its copy is the same.
		_ = os.RemoveAll(b.dir)
		return m, nil
	}
	if err := os.Rename(b.dir, target); err != nil {
		return nil, err
	}
	return m, nil
}

// program names what builds entries, for build.json (set by the host).
var program = "situcraft"

// SetProgram records the program and version building entries.
func SetProgram(name string) { program = name }

// --- Checksums ---------------------------------------------------------------------------------

// The memo records the SHA-256 of each source file by its identity (place, size and time), so
// that a bundle reopened unchanged is not read again.

type memoEntry struct {
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Checked int64  `json:"checked"` // Unix seconds
}

const maxMemo = 20000

func (c *Cache) memoPath() string { return filepath.Join(c.dir, "checksums.json") }

func (c *Cache) loadMemo() map[string]memoEntry {
	var file struct {
		Version int                  `json:"version"`
		Files   map[string]memoEntry `json:"files"`
	}
	raw, err := os.ReadFile(c.memoPath())
	if err != nil || json.Unmarshal(raw, &file) != nil || file.Version != 1 || file.Files == nil {
		return map[string]memoEntry{}
	}
	return file.Files
}

// saveMemo writes the memo (the newest maxMemo records). c.mu is held.
func (c *Cache) saveMemo() error {
	if len(c.memo) > maxMemo {
		keys := make([]string, 0, len(c.memo))
		for k := range c.memo {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return c.memo[keys[i]].Checked > c.memo[keys[j]].Checked })
		for _, k := range keys[maxMemo:] {
			delete(c.memo, k)
		}
	}
	raw, err := json.Marshal(map[string]any{"version": 1, "files": c.memo})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.dir, ".checksums-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), c.memoPath())
}

// KnownChecksum is the recorded SHA-256 of the file with this identity, if there is one.
func (c *Cache) KnownChecksum(identity string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.memo[identity]
	return e.SHA256, ok
}

// Checksum returns the SHA-256 of the file with this identity, reading it (through open) only
// when it is not already recorded. read reports bytes as they are read.
func (c *Cache) Checksum(identity string, size int64, open func() (io.ReadCloser, error), read func(n int64)) (string, error) {
	if sum, ok := c.KnownChecksum(identity); ok {
		return sum, nil
	}
	r, err := open()
	if err != nil {
		return "", err
	}
	defer r.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	var total int64
	for {
		n, err := r.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			total += int64(n)
			if read != nil {
				read(int64(n))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	if total != size {
		return "", fmt.Errorf("read %d bytes of a file of %d", total, size)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	c.mu.Lock()
	defer c.mu.Unlock()
	c.memo[identity] = memoEntry{SHA256: sum, Size: size, Checked: time.Now().Unix()}
	return sum, c.saveMemo()
}

// --- Size and eviction -------------------------------------------------------------------------

// Item is one thing the cache holds.
type Item struct {
	Name  string    `json:"name"`
	Kind  string    `json:"kind"`  // "image", "transcripts", "geometry", "unzipped"
	State string    `json:"state"` // "complete", "partial" (being or once built) or "unpacked"
	Size  int64     `json:"size"`
	Used  time.Time `json:"used"`
	// InUse is true for what was used since the program started (never evicted).
	InUse bool `json:"inUse"`
	path  string
}

// Usage is what the cache holds.
type Usage struct {
	Dir   string `json:"dir"`
	Limit int64  `json:"limit"`
	Size  int64  `json:"size"`
	Items []Item `json:"items"`
}

// Usage lists everything the cache holds, most recently used first.
func (c *Cache) Usage() (Usage, error) {
	u := Usage{Dir: c.root, Limit: c.limit}
	add := func(parent, state string, kindOf func(name string) string) error {
		entries, err := os.ReadDir(parent)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(parent, e.Name())
			info, err := os.Stat(p)
			if err != nil {
				continue
			}
			used := info.ModTime()
			if state == "partial" {
				// A partial build is in use while its lock is fresh.
				if lock, err := os.Stat(filepath.Join(p, lockName)); err == nil && lock.ModTime().After(used) {
					used = lock.ModTime()
				}
			}
			size := dirSize(p)
			u.Items = append(u.Items, Item{Name: e.Name(), Kind: kindOf(e.Name()), State: state, Size: size, Used: used, InUse: !used.Before(c.started), path: p})
			u.Size += size
		}
		return nil
	}
	kindOf := func(name string) string {
		if i := strings.LastIndexByte(name, '-'); i > 0 {
			return name[:i]
		}
		return name
	}
	if err := add(c.entriesDir(), "complete", kindOf); err != nil {
		return u, err
	}
	if err := add(c.buildingDir(), "partial", kindOf); err != nil {
		return u, err
	}
	if err := add(filepath.Join(c.root, "unzipped"), "unpacked", func(string) string { return "unzipped" }); err != nil {
		return u, err
	}
	u.Size += fileSize(c.memoPath())
	sort.Slice(u.Items, func(i, j int) bool { return u.Items[i].Used.After(u.Items[j].Used) })
	return u, nil
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// Evict removes what was used least recently until the cache is within its limit. Nothing used
// since the program started is removed, so the open datasets' entries stay even when they alone
// exceed the limit. It returns what it removed.
func (c *Cache) Evict() ([]Item, error) {
	if c.limit <= 0 {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	u, err := c.Usage()
	if err != nil {
		return nil, err
	}
	var removed []Item
	for i := len(u.Items) - 1; i >= 0 && u.Size > c.limit; i-- {
		it := u.Items[i]
		if it.InUse {
			continue
		}
		if err := os.RemoveAll(it.path); err != nil {
			return removed, err
		}
		u.Size -= it.Size
		removed = append(removed, it)
	}
	return removed, nil
}

// Remove deletes one thing the cache holds (by its name in Usage), unless it is being built.
func (c *Cache) Remove(name string) error {
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return fmt.Errorf("no cache item %q", name)
	}
	for _, parent := range []string{c.entriesDir(), c.buildingDir(), filepath.Join(c.root, "unzipped")} {
		p := filepath.Join(parent, name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if lock, err := os.Stat(filepath.Join(p, lockName)); err == nil && time.Since(lock.ModTime()) < c.staleLock {
			return ErrBusy
		}
		return os.RemoveAll(p)
	}
	return fmt.Errorf("no cache item %q", name)
}

// ParseSize reads a size such as "10GB", "500 MB", "2.5G" or "0" (decimal units: 1 GB is 10^9
// bytes).
func ParseSize(s string) (int64, error) {
	t := strings.ToUpper(strings.TrimSpace(s))
	units := []struct {
		suffix string
		scale  float64
	}{{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"KB", 1e3}, {"T", 1e12}, {"G", 1e9}, {"M", 1e6}, {"K", 1e3}, {"B", 1}}
	scale := 1.0
	for _, u := range units {
		if strings.HasSuffix(t, u.suffix) {
			t, scale = strings.TrimSpace(strings.TrimSuffix(t, u.suffix)), u.scale
			break
		}
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil || v < 0 || v*scale > 1e18 {
		return 0, fmt.Errorf("%q is not a size (for example 10GB or 500MB)", s)
	}
	return int64(v * scale), nil
}
