package segment

import (
	"bufio"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Column streams a file of one value a transcript (assignments.bin, probabilities.bin) a block at
// a time, without holding it whole: 8 bytes a transcript for both is 400 MB at 50 million.
type Column struct {
	name string
	f    *os.File
	zr   io.ReadCloser
	n    int
	at   int
	buf  []byte
}

// OpenColumn opens assignments.bin or probabilities.bin of a segmentation's folder to stream.
func OpenColumn(dir, file string) (*Column, error) {
	magic, ok := map[string]string{FileAssignments: "SCAS", FileProbabilities: "SCPR"}[file]
	if !ok {
		return nil, fmt.Errorf("%s is not a column of transcripts", file)
	}
	f, err := os.Open(filepath.Join(dir, file))
	if err != nil {
		return nil, err
	}
	c := &Column{name: file, f: f}
	fail := func(err error) (*Column, error) {
		f.Close()
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	var head [16]byte
	if _, err := io.ReadFull(f, head[:]); err != nil || string(head[:4]) != magic {
		return fail(fmt.Errorf("not a SituCraft %s file", magic))
	}
	if v := binary.LittleEndian.Uint32(head[4:]); v > formatVersion {
		return fail(fmt.Errorf("written by a newer SituCraft (version %d)", v))
	}
	length := binary.LittleEndian.Uint64(head[8:])
	if c.zr, err = zlib.NewReader(bufio.NewReaderSize(f, 1<<20)); err != nil {
		return fail(err)
	}
	var count [4]byte
	if _, err := io.ReadFull(c.zr, count[:]); err != nil {
		return fail(err)
	}
	c.n = int(binary.LittleEndian.Uint32(count[:]))
	if uint64(4+4*c.n) != length {
		return fail(fmt.Errorf("it claims %d values in %d bytes", c.n, length))
	}
	return c, nil
}

// Len is the values the column holds.
func (c *Column) Len() int { return c.n }

// Next reads the next values into dst, as many as it holds or are left, and returns how many: 0
// once all are read, when the stream is checked to end there with its checksum holding.
func (c *Column) Next(dst []uint32) (int, error) {
	if c.at == c.n {
		if n, err := c.zr.Read(make([]byte, 1)); n != 0 || err != io.EOF {
			if err == nil || err == io.EOF {
				err = errors.New("more data than its header says")
			}
			return 0, fmt.Errorf("%s: %w", c.name, err)
		}
		return 0, nil
	}
	k := min(len(dst), c.n-c.at)
	if cap(c.buf) < 4*k {
		c.buf = make([]byte, 4*k)
	}
	b := c.buf[:4*k]
	if _, err := io.ReadFull(c.zr, b); err != nil {
		return 0, fmt.Errorf("%s: %w", c.name, err)
	}
	for i := range k {
		dst[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
	c.at += k
	return k, nil
}

// Close closes the file.
func (c *Column) Close() error {
	c.zr.Close()
	return c.f.Close()
}
