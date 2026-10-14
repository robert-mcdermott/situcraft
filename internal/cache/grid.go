package cache

import "math"

// TileSize is the side of every tile the cache writes, in pixels of the tile's level.
const TileSize = 512

// Grid is the tiling every entry of a dataset shares: the morphology image's pyramid. Level L has
// pixels 2^L times the full resolution's, and its tile (tx, ty) covers the same tissue (in µm)
// whatever the layer: image pixels, transcripts, density or polygons. The full-resolution image's
// pixel (0, 0) has its corner at (0, 0) µm, as Xenium's coordinates do.
type Grid struct {
	PixelSize float64 `json:"pixelSize"` // µm per full-resolution pixel
	Width     int     `json:"width"`     // full-resolution pixels
	Height    int     `json:"height"`
	Tile      int     `json:"tile"`   // pixels per tile side at every level
	Levels    int     `json:"levels"` // level Levels-1 is one tile
}

// NewGrid makes the grid of an image: as many levels as it takes to reach one tile.
func NewGrid(pixelSize float64, width, height int) Grid {
	g := Grid{PixelSize: pixelSize, Width: width, Height: height, Tile: TileSize, Levels: 1}
	for g.Across(g.Levels-1) > 1 || g.Down(g.Levels-1) > 1 {
		g.Levels++
	}
	return g
}

// TileUm is the side of a tile of the level, in µm.
func (g Grid) TileUm(level int) float64 {
	return float64(g.Tile) * g.PixelSize * float64(int(1)<<level)
}

// Across is the number of tiles across the level.
func (g Grid) Across(level int) int { return ceilDiv(g.Width, g.Tile<<level) }

// Down is the number of tiles down the level.
func (g Grid) Down(level int) int { return ceilDiv(g.Height, g.Tile<<level) }

// TileOf is the tile of the level holding the point (x, y) µm. Points past the edges (Xenium
// places a few transcripts just outside the image) belong to the nearest edge tile.
func (g Grid) TileOf(level int, x, y float64) (tx, ty int) {
	side := g.TileUm(level)
	return clampInt(int(math.Floor(x/side)), 0, g.Across(level)-1), clampInt(int(math.Floor(y/side)), 0, g.Down(level)-1)
}

func (g Grid) valid() bool {
	return g.PixelSize > 0 && !math.IsInf(g.PixelSize, 0) && g.Width > 0 && g.Height > 0 && g.Tile > 0 && g.Levels > 0 && g.Levels <= 30
}

func ceilDiv(a, b int) int { return (a + b - 1) / b }

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
