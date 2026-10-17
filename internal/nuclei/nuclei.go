// Package nuclei gives registration (internal/register) the nuclear signal of the images it aligns,
// a region of a level at a time: the Xenium image's DAPI from a bundle's morphology image, and a
// post-Xenium image's nuclear stain, an H&E's haematoxylin by colour deconvolution or an
// immunofluorescence image's nuclear channel. Decoded blocks are kept, a few hundred at most.
package nuclei

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"situcraft/internal/cache"
	"situcraft/internal/register"
	"situcraft/internal/slide"
	"situcraft/internal/xenium"
)

type blockKey struct{ level, bx, by int }

// blocks is a pyramid read a block at a time.
type blocks struct {
	levels [][2]int
	// size is a level's blocks' width and height; decode a block's signal, row by row, padded
	// past the image's edges.
	size   func(level int) (int, int)
	decode func(level, bx, by int) ([]float32, error)

	mu    sync.Mutex
	kept  map[blockKey][]float32
	order []blockKey
}

const keptBlocks = 384

func (p *blocks) Levels() [][2]int { return p.levels }

func (p *blocks) block(k blockKey) ([]float32, error) {
	p.mu.Lock()
	if b, ok := p.kept[k]; ok {
		p.mu.Unlock()
		return b, nil
	}
	p.mu.Unlock()
	b, err := p.decode(k.level, k.bx, k.by)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.kept == nil {
		p.kept = map[blockKey][]float32{}
	}
	if _, ok := p.kept[k]; !ok {
		p.kept[k] = b
		p.order = append(p.order, k)
		if len(p.order) > keptBlocks {
			delete(p.kept, p.order[0])
			p.order = p.order[1:]
		}
	}
	return b, nil
}

// Region reads a level's pixels [x0, x0 + w) × [y0, y0 + h); those beyond the image are not image.
func (p *blocks) Region(level, x0, y0, w, h int) (register.Plane, error) {
	if level < 0 || level >= len(p.levels) || w <= 0 || h <= 0 {
		return register.Plane{}, fmt.Errorf("no region %d × %d of level %d", w, h, level)
	}
	lw, lh := p.levels[level][0], p.levels[level][1]
	bw, bh := p.size(level)
	out := register.Plane{W: w, H: h, Pix: make([]float32, w*h), Mask: make([]bool, w*h)}
	ix0, iy0, ix1, iy1 := max(x0, 0), max(y0, 0), min(x0+w, lw), min(y0+h, lh)
	if ix0 >= ix1 || iy0 >= iy1 {
		return out, nil
	}
	for by := iy0 / bh; by <= (iy1-1)/bh; by++ {
		for bx := ix0 / bw; bx <= (ix1-1)/bw; bx++ {
			b, err := p.block(blockKey{level, bx, by})
			if err != nil {
				return register.Plane{}, err
			}
			for y := max(iy0, by*bh); y < min(iy1, (by+1)*bh); y++ {
				for x := max(ix0, bx*bw); x < min(ix1, (bx+1)*bw); x++ {
					k := (y-y0)*w + (x - x0)
					out.Pix[k] = b[(y-by*bh)*bw+(x-bx*bw)]
					out.Mask[k] = true
				}
			}
		}
	}
	return out, nil
}

// FromBundle is a bundle's DAPI (its morphology image's nuclear channel), its pixel size and the
// channel's name; close releases its files.
func FromBundle(b *xenium.Bundle) (pyramid register.Pyramid, pixel float64, name string, close func(), err error) {
	img, err := b.FocusImage()
	if err != nil {
		return nil, 0, "", nil, err
	}
	if img == nil {
		return nil, 0, "", nil, errors.New("the bundle has no morphology image to register to")
	}
	chans, _, closeAll, err := cache.ImageSources(b, img)
	if err != nil {
		return nil, 0, "", nil, err
	}
	pick := 0
	for i, c := range chans {
		if c.Purpose == "Nuclear" || strings.EqualFold(c.Name, "DAPI") {
			pick = i
			break
		}
	}
	ch := chans[pick]
	p := &blocks{
		size: func(l int) (int, int) { return ch.Levels[l].BlockWidth, ch.Levels[l].BlockHeight },
		decode: func(l, bx, by int) ([]float32, error) {
			raw, err := ch.Levels[l].Decode(bx, by)
			if err != nil {
				return nil, err
			}
			out := make([]float32, len(raw))
			for i, v := range raw {
				out[i] = float32(v)
			}
			return out, nil
		},
	}
	for _, l := range ch.Levels {
		p.levels = append(p.levels, [2]int{l.Width, l.Height})
	}
	pixel = b.Experiment.PixelSize
	if !(pixel > 0) {
		closeAll()
		return nil, 0, "", nil, errors.New("the bundle's experiment.xenium gives no pixel size")
	}
	return p, pixel, ch.Name, closeAll, nil
}

// NuclearChannel is the index of an image's nuclear stain by its name (DAPI, Hoechst, a nuclear
// stain), or −1.
func NuclearChannel(d slide.Description) int {
	for i, c := range d.Channels {
		n := strings.ToLower(c.Name)
		if strings.Contains(n, "dapi") || strings.Contains(n, "hoechst") || strings.Contains(n, "nucle") {
			return i
		}
	}
	return -1
}

// FromImage is a post-Xenium image's nuclear signal: an RGB image's haematoxylin, by colour
// deconvolution with these stains against its glass's light (each channel's 99.5th percentile
// over its smallest level), or channel's samples; and what it is.
func FromImage(im *slide.Image, channel int, stains register.Stains) (register.Pyramid, string, error) {
	p := &blocks{size: func(l int) (int, int) { return im.Levels[l].TileWidth, im.Levels[l].TileHeight }}
	for _, l := range im.Levels {
		p.levels = append(p.levels, [2]int{l.Width, l.Height})
	}
	if im.Kind == slide.RGB {
		bg, err := glass(im)
		if err != nil {
			return nil, "", err
		}
		d := register.NewDeconvolution(stains, bg)
		p.decode = func(l, bx, by int) ([]float32, error) {
			tile, err := im.Tile(l, 0, bx, by)
			if err != nil {
				return nil, err
			}
			out := make([]float32, len(tile)/3)
			for i := range out {
				out[i] = float32(d.Haematoxylin(tile[3*i], tile[3*i+1], tile[3*i+2]))
			}
			return out, nil
		}
		return p, fmt.Sprintf("hematoxylin, by color deconvolution (its glass %.0f, %.0f, %.0f)", bg[0], bg[1], bg[2]), nil
	}
	if channel < 0 || channel >= len(im.Channels) {
		return nil, "", fmt.Errorf("no channel %d: the image has %d", channel, len(im.Channels))
	}
	px := im.PixelBytes()
	p.decode = func(l, bx, by int) ([]float32, error) {
		tile, err := im.Tile(l, channel, bx, by)
		if err != nil {
			return nil, err
		}
		out := make([]float32, len(tile)/px)
		for i := range out {
			if px == 2 {
				out[i] = float32(uint16(tile[2*i]) | uint16(tile[2*i+1])<<8)
			} else {
				out[i] = float32(tile[i])
			}
		}
		return out, nil
	}
	return p, im.Channels[channel].Name, nil
}

// glass is an RGB image's light where there is no tissue: each channel's 99.5th percentile over
// its smallest level.
func glass(im *slide.Image) ([3]float64, error) {
	l := len(im.Levels) - 1
	lv := im.Levels[l]
	var vals [3][]uint8
	for ty := 0; ty < lv.Down; ty++ {
		for tx := 0; tx < lv.Across; tx++ {
			tile, err := im.Tile(l, 0, tx, ty)
			if err != nil {
				return [3]float64{}, err
			}
			for y := 0; y < lv.TileHeight && ty*lv.TileHeight+y < lv.Height; y++ {
				for x := 0; x < lv.TileWidth && tx*lv.TileWidth+x < lv.Width; x++ {
					k := 3 * (y*lv.TileWidth + x)
					for c := 0; c < 3; c++ {
						vals[c] = append(vals[c], tile[k+c])
					}
				}
			}
		}
	}
	var bg [3]float64
	for c := range vals {
		v := vals[c]
		if len(v) == 0 {
			return [3]float64{}, errors.New("the image's smallest level is empty")
		}
		sort.Slice(v, func(a, b int) bool { return v[a] < v[b] })
		bg[c] = math.Max(1, float64(v[min(len(v)-1, len(v)*995/1000)]))
	}
	return bg, nil
}
