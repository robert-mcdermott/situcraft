package cache

// Image tile pyramids. Each channel's levels are decoded once from the bundle (or, for levels the
// bundle lacks, computed from the level above by averaging 2×2 pixels) into one file per channel
// and level ("c<channel>-l<level>.tiles", magic SCIT). Fields: width, height, tile size, tiles
// across, tiles down. Table entries: A and B are the tile's width and height (tiles at the right
// and bottom edges are narrower). A tile's block is a zlib stream of its samples as 16-bit
// little-endian integers with the bytes split into two planes, every sample's low byte first,
// then every high byte, which deflates better than the samples as they are. A page can inflate
// the block natively and read the planes as a two-row texture.

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

const imageMagic = "SCIT"

// tileEncoding names the tile blocks' encoding in the summary.
const tileEncoding = "zlib-split16"

// SourceLevel is one resolution of a source image plane, read a block (a tile of the source file)
// at a time.
type SourceLevel struct {
	Width, Height           int
	BlockWidth, BlockHeight int
	// Decode returns block (bx, by): BlockWidth×BlockHeight samples, row-major, padded past the
	// image's edges.
	Decode func(bx, by int) ([]uint16, error)
}

// SourceChannel is one channel of the image to cache: its levels, full resolution first.
type SourceChannel struct {
	Name, Purpose string
	Levels        []SourceLevel
}

// ImageSummary is an image entry's summary.
type ImageSummary struct {
	Encoding  string           `json:"encoding"`
	PixelSize float64          `json:"pixelSize"`
	Bits      int              `json:"bits"`
	Channels  []ChannelSummary `json:"channels"`
}

// ChannelSummary describes one cached channel.
type ChannelSummary struct {
	Name    string         `json:"name"`
	Purpose string         `json:"purpose,omitempty"`
	Levels  []LevelSummary `json:"levels"`
}

// LevelSummary describes one cached level.
type LevelSummary struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	Across int `json:"across"`
	Down   int `json:"down"`
	// Source is "decoded" (from the bundle's own level) or "computed" (averaged from the level
	// above, where the bundle has no such level).
	Source string `json:"source"`
}

func imageFileName(channel, level int) string { return fmt.Sprintf("c%d-l%d.tiles", channel, level) }

// planLevels decides each cached level's size and whether it is decoded from the source: a
// source level is used when it is half the size of the level above (rounded either way).
func planLevels(grid Grid, ch SourceChannel) ([]LevelSummary, error) {
	if len(ch.Levels) == 0 || ch.Levels[0].Width != grid.Width || ch.Levels[0].Height != grid.Height {
		return nil, fmt.Errorf("channel %s is not %d×%d pixels", ch.Name, grid.Width, grid.Height)
	}
	var out []LevelSummary
	w, h := grid.Width, grid.Height
	fromSource := true
	for l := 0; l < grid.Levels; l++ {
		if l > 0 {
			pw, ph := w, h
			w, h = ceilDiv(pw, 2), ceilDiv(ph, 2)
			if fromSource && l < len(ch.Levels) {
				s := ch.Levels[l]
				if (s.Width == pw/2 || s.Width == w) && (s.Height == ph/2 || s.Height == h) && s.Width > 0 && s.Height > 0 {
					w, h = s.Width, s.Height
				} else {
					fromSource = false
				}
			} else {
				fromSource = false
			}
		}
		src := "decoded"
		if !fromSource {
			src = "computed"
		}
		lv := ch.Levels[min(l, len(ch.Levels)-1)]
		if fromSource && (lv.BlockWidth <= 0 || lv.BlockHeight <= 0 || lv.Decode == nil) {
			return nil, fmt.Errorf("channel %s level %d has no blocks", ch.Name, l)
		}
		out = append(out, LevelSummary{Width: w, Height: h, Across: ceilDiv(w, grid.Tile), Down: ceilDiv(h, grid.Tile), Source: src})
	}
	return out, nil
}

// buildImage writes every channel's pyramid. progress is called with tiles done and the total.
func buildImage(ctx context.Context, b *building, grid Grid, bits int, chans []SourceChannel, progress func(done, total int)) (*ImageSummary, error) {
	sum := &ImageSummary{Encoding: tileEncoding, PixelSize: grid.PixelSize, Bits: bits}
	total := 0
	for _, ch := range chans {
		levels, err := planLevels(grid, ch)
		if err != nil {
			return nil, err
		}
		sum.Channels = append(sum.Channels, ChannelSummary{Name: ch.Name, Purpose: ch.Purpose, Levels: levels})
		for _, l := range levels {
			total += l.Across * l.Down
		}
	}
	done := 0
	for c, ch := range chans {
		for l, level := range sum.Channels[c].Levels {
			name := imageFileName(c, l)
			tiles := level.Across * level.Down
			if b.done(name) {
				done += tiles
				progress(done, total)
				continue
			}
			var source func(tx, ty int) ([]uint16, error)
			var above *imageFile
			if level.Source == "decoded" {
				source = decodedTiles(grid, ch.Levels[l], level)
			} else {
				var err error
				if above, err = openImageFile(b.path(imageFileName(c, l-1))); err != nil {
					return nil, err
				}
				source = computedTiles(grid, above, level)
			}
			err := writeImageLevel(ctx, b, name, grid, level, source, func(n int) {
				done += n
				progress(done, total)
			})
			if above != nil {
				above.Close()
			}
			if err != nil {
				return nil, fmt.Errorf("channel %s, level %d: %w", ch.Name, l, err)
			}
		}
	}
	return sum, nil
}

// writeImageLevel writes one level's file, a row of tiles at a time, encoding the row's tiles in
// parallel.
func writeImageLevel(ctx context.Context, b *building, name string, grid Grid, level LevelSummary, source func(tx, ty int) ([]uint16, error), tilesDone func(int)) error {
	f, err := b.create(name)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		f.Close()
		return err
	}
	w, err := newTableWriter(f, imageMagic, [6]uint32{uint32(level.Width), uint32(level.Height), uint32(grid.Tile), uint32(level.Across), uint32(level.Down)}, level.Across*level.Down)
	if err != nil {
		return fail(err)
	}
	workers := runtime.GOMAXPROCS(0)
	for ty := 0; ty < level.Down; ty++ {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		blocks := make([][]byte, level.Across)
		errs := make([]error, level.Across)
		pixels := make([][]uint16, level.Across)
		for tx := 0; tx < level.Across; tx++ {
			if pixels[tx], err = source(tx, ty); err != nil {
				return fail(err)
			}
		}
		var wg sync.WaitGroup
		next := make(chan int)
		for range min(workers, level.Across) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for tx := range next {
					blocks[tx], errs[tx] = encodeTile(pixels[tx])
				}
			}()
		}
		for tx := 0; tx < level.Across; tx++ {
			next <- tx
		}
		close(next)
		wg.Wait()
		for tx := 0; tx < level.Across; tx++ {
			if errs[tx] != nil {
				return fail(errs[tx])
			}
			tw, th := tileSize(level, grid.Tile, tx, ty)
			if err := w.block(ty*level.Across+tx, blocks[tx], uint32(tw), uint32(th)); err != nil {
				return fail(err)
			}
		}
		tilesDone(level.Across)
	}
	if err := w.finish(); err != nil {
		return fail(err)
	}
	return b.commitFile(f, name)
}

func tileSize(level LevelSummary, tile, tx, ty int) (int, int) {
	return min(tile, level.Width-tx*tile), min(tile, level.Height-ty*tile)
}

// decodedTiles reads a level's tiles from the source's blocks, keeping the decoded blocks of the
// current rows (tiles are read row by row).
func decodedTiles(grid Grid, src SourceLevel, level LevelSummary) func(tx, ty int) ([]uint16, error) {
	blocks := map[[2]int][]uint16{}
	lowestRow := 0
	return func(tx, ty int) ([]uint16, error) {
		tw, th := tileSize(level, grid.Tile, tx, ty)
		x0, y0 := tx*grid.Tile, ty*grid.Tile
		// Blocks above this row of tiles are no longer needed.
		if row := y0 / src.BlockHeight; row > lowestRow {
			for k := range blocks {
				if k[1] < row {
					delete(blocks, k)
				}
			}
			lowestRow = row
		}
		out := make([]uint16, tw*th)
		for by := y0 / src.BlockHeight; by <= (y0+th-1)/src.BlockHeight; by++ {
			for bx := x0 / src.BlockWidth; bx <= (x0+tw-1)/src.BlockWidth; bx++ {
				key := [2]int{bx, by}
				block, ok := blocks[key]
				if !ok {
					var err error
					if block, err = src.Decode(bx, by); err != nil {
						return nil, err
					}
					if len(block) != src.BlockWidth*src.BlockHeight {
						return nil, fmt.Errorf("block (%d, %d) has %d samples, not %d", bx, by, len(block), src.BlockWidth*src.BlockHeight)
					}
					blocks[key] = block
				}
				// The part of the block inside this tile.
				bx0, by0 := bx*src.BlockWidth, by*src.BlockHeight
				ix0, iy0 := max(x0, bx0), max(y0, by0)
				ix1, iy1 := min(x0+tw, bx0+src.BlockWidth), min(y0+th, by0+src.BlockHeight)
				for y := iy0; y < iy1; y++ {
					copy(out[(y-y0)*tw+(ix0-x0):(y-y0)*tw+(ix1-x0)], block[(y-by0)*src.BlockWidth+(ix0-bx0):(y-by0)*src.BlockWidth+(ix1-bx0)])
				}
			}
		}
		return out, nil
	}
}

// computedTiles makes a level's tiles by averaging 2×2 pixels of the level above (rounding half
// up; at an odd edge, the pixels there are).
func computedTiles(grid Grid, above *imageFile, level LevelSummary) func(tx, ty int) ([]uint16, error) {
	return func(tx, ty int) ([]uint16, error) {
		tw, th := tileSize(level, grid.Tile, tx, ty)
		out := make([]uint16, tw*th)
		sums := make([]uint32, tw*th)
		counts := make([]uint8, tw*th)
		for j := 0; j < 2; j++ {
			for i := 0; i < 2; i++ {
				cx, cy := 2*tx+i, 2*ty+j
				if cx >= above.Across || cy >= above.Down {
					continue
				}
				pix, cw, ch, err := above.Tile(cx, cy)
				if err != nil {
					return nil, err
				}
				for y := 0; y < ch; y++ {
					oy := (j*grid.Tile + y) / 2
					if oy >= th {
						continue
					}
					for x := 0; x < cw; x++ {
						ox := (i*grid.Tile + x) / 2
						if ox >= tw {
							continue
						}
						sums[oy*tw+ox] += uint32(pix[y*cw+x])
						counts[oy*tw+ox]++
					}
				}
			}
		}
		for k := range out {
			if counts[k] > 0 {
				out[k] = uint16((sums[k] + uint32(counts[k])/2) / uint32(counts[k]))
			}
		}
		return out, nil
	}
}

// encodeTile deflates a tile's samples with their bytes split into planes.
func encodeTile(pix []uint16) ([]byte, error) {
	split := make([]byte, 2*len(pix))
	for i, v := range pix {
		split[i], split[len(pix)+i] = byte(v), byte(v>>8)
	}
	return deflate(split)
}

// decodeTile inflates a tile of n samples.
func decodeTile(block []byte, n int) ([]uint16, error) {
	split, err := inflate(block, 2*int64(n))
	if err != nil {
		return nil, err
	}
	pix := make([]uint16, n)
	for i := range pix {
		pix[i] = uint16(split[i]) | uint16(split[n+i])<<8
	}
	return pix, nil
}

// imageFile is one open channel level.
type imageFile struct {
	t                       *tableFile
	Width, Height, TileSide int
	Across, Down            int
}

func openImageFile(path string) (*imageFile, error) {
	t, err := openTableFile(path, imageMagic, func(f [6]uint32) (int, error) {
		if f[2] == 0 || f[2] > 1<<14 || f[0] == 0 || f[1] == 0 || f[3] != uint32(ceilDiv(int(f[0]), int(f[2]))) || f[4] != uint32(ceilDiv(int(f[1]), int(f[2]))) {
			return 0, errDamaged
		}
		return int(f[3]) * int(f[4]), nil
	})
	if err != nil {
		return nil, err
	}
	return &imageFile{t: t, Width: int(t.Fields[0]), Height: int(t.Fields[1]), TileSide: int(t.Fields[2]), Across: int(t.Fields[3]), Down: int(t.Fields[4])}, nil
}

// RawTile is tile (tx, ty)'s block as stored (a zlib stream of split 16-bit samples) and its size.
func (f *imageFile) RawTile(tx, ty int) ([]byte, int, int, error) {
	if tx < 0 || ty < 0 || tx >= f.Across || ty >= f.Down {
		return nil, 0, 0, fmt.Errorf("no tile (%d, %d)", tx, ty)
	}
	block, e, err := f.t.block(ty*f.Across + tx)
	if err != nil {
		return nil, 0, 0, err
	}
	w, h := min(f.TileSide, f.Width-tx*f.TileSide), min(f.TileSide, f.Height-ty*f.TileSide)
	if int(e.A) != w || int(e.B) != h || block == nil {
		return nil, 0, 0, errDamaged
	}
	return block, w, h, nil
}

// Tile is tile (tx, ty)'s samples, row-major, and its size.
func (f *imageFile) Tile(tx, ty int) ([]uint16, int, int, error) {
	block, w, h, err := f.RawTile(tx, ty)
	if err != nil {
		return nil, 0, 0, err
	}
	pix, err := decodeTile(block, w*h)
	return pix, w, h, err
}

func (f *imageFile) Close() error { return f.t.Close() }
