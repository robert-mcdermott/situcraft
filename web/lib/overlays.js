// Post-Xenium images on the tissue (api_images.go): each placed by its transform, an affine map from
// its full-resolution pixels to µm (any scale, rotation or reflection), drawn a level and a tile at a
// time. The geometry the renderer needs, without WebGL: which level suits a zoom, which of its tiles
// cover the view, and where a tile's corners fall.

// An affine transform as two rows [[a, b, c], [d, e, f]]: (x, y) → (a·x + b·y + c, d·x + e·y + f).
export function apply(t, x, y) {
  return [t[0][0] * x + t[0][1] * y + t[0][2], t[1][0] * x + t[1][1] * y + t[1][2]];
}

export function invert(t) {
  const [[a, b, c], [d, e, f]] = t;
  const det = a * e - b * d;
  if (!det || !Number.isFinite(det)) return null;
  return [[e / det, -b / det, (b * f - e * c) / det], [-d / det, a / det, (d * c - a * f) / det]];
}

// µm per full-resolution pixel (the square root of the transform's area factor).
export function umPerPixel(t) {
  return Math.sqrt(Math.abs(t[0][0] * t[1][1] - t[0][1] * t[1][0]));
}

// The 3×3 matrix (column-major, as WebGL takes it) of the transform.
export function modelMatrix(t) {
  return new Float32Array([t[0][0], t[1][0], 0, t[0][1], t[1][1], 0, t[0][2], t[1][2], 1]);
}

// How many full-resolution pixels one of a level's pixels spans, across and down (levels are halved
// with rounding, so not always exactly a power of two).
export function levelScale(levels, level) {
  return [levels[0].width / levels[level].width, levels[0].height / levels[level].height];
}

// The level to draw at a zoom of pixelsPerUm device pixels per µm: the coarsest whose pixels are no
// larger than a device pixel (the finest when even it is coarser).
export function overlayLevel(levels, transform, pixelsPerUm) {
  const devicePixel = (1 / pixelsPerUm) * (1 + 1e-9);
  const base = umPerPixel(transform);
  let level = 0;
  while (level + 1 < levels.length && base * Math.max(...levelScale(levels, level + 1)) <= devicePixel) level += 1;
  return level;
}

// The tiles of a level covering a rectangle of µm (x0, y0, x1, y1): { tx0, ty0, tx1, ty1 }
// inclusive, or null when the rectangle misses the image.
export function overlayTileRange(levels, level, inverse, rect) {
  const corners = [[rect.x0, rect.y0], [rect.x1, rect.y0], [rect.x0, rect.y1], [rect.x1, rect.y1]].map(([x, y]) => apply(inverse, x, y));
  const [sx, sy] = levelScale(levels, level);
  const lv = levels[level];
  const xs = corners.map((p) => p[0] / sx);
  const ys = corners.map((p) => p[1] / sy);
  const tx0 = Math.max(0, Math.floor(Math.min(...xs) / lv.tileWidth));
  const ty0 = Math.max(0, Math.floor(Math.min(...ys) / lv.tileHeight));
  const tx1 = Math.min(lv.across - 1, Math.floor(Math.max(...xs) / lv.tileWidth));
  const ty1 = Math.min(lv.down - 1, Math.floor(Math.max(...ys) / lv.tileHeight));
  return tx0 > tx1 || ty0 > ty1 ? null : { tx0, ty0, tx1, ty1 };
}

// Where a tile lies in full-resolution pixels and how much of it is image (a tile at the right or
// bottom edge is padded): { origin: [x, y], size: [w, h] } in full-resolution pixels, texels:
// [w, h] of the tile's own.
export function tileGeometry(levels, level, tx, ty) {
  const lv = levels[level];
  const [sx, sy] = levelScale(levels, level);
  const w = Math.min(lv.tileWidth, lv.width - tx * lv.tileWidth);
  const h = Math.min(lv.tileHeight, lv.height - ty * lv.tileHeight);
  return { origin: [tx * lv.tileWidth * sx, ty * lv.tileHeight * sy], size: [w * sx, h * sy], texels: [w, h] };
}

// The rectangle of µm an image covers (the bounding box of its transformed corners).
export function overlayBounds(desc) {
  const lv = desc.image.levels[0];
  const pts = [[0, 0], [lv.width, 0], [0, lv.height], [lv.width, lv.height]].map(([x, y]) => apply(desc.transform, x, y));
  const xs = pts.map((p) => p[0]);
  const ys = pts.map((p) => p[1]);
  return { x0: Math.min(...xs), y0: Math.min(...ys), x1: Math.max(...xs), y1: Math.max(...ys) };
}

// The layer settings an image starts with: an H&E opaque over the stains, an immunofluorescence
// image's channels each in a colour and in the window its samples span (api_images.go's ranges), a
// nuclear stain (DAPI) left off, the Xenium image's own.
const CHANNEL_COLORS = ['#ff5ca8', '#3ddc84', '#ffd23f', '#3fd6e8', '#ff8a3d', '#c58cff', '#ffffff'];
export function defaultImageLayer(desc) {
  const max = (1 << desc.image.bits) - 1;
  let k = 0;
  return {
    id: desc.id,
    visible: true,
    opacity: desc.image.kind === 'rgb' ? 1 : 0.8,
    channels: desc.image.kind === 'rgb' ? [] : desc.image.channels.map((c, i) => {
      const nuclear = /dapi|hoechst|nucle/i.test(c.name);
      const window = desc.ranges?.[i] ? [desc.ranges[i][0], desc.ranges[i][1]] : [0, max];
      return { visible: !nuclear, color: nuclear ? '#4f7cff' : CHANNEL_COLORS[k++ % CHANNEL_COLORS.length], window, gamma: 1 };
    }),
  };
}

// A deformed image's inverse field (api_register.go's /inverse): { x0, y0, step, nx, ny, field (each
// point's displacement back, x then y, µm), max (the largest, µm) }.
export function readInverseField(buffer) {
  const view = new DataView(buffer);
  const magic = String.fromCharCode(...new Uint8Array(buffer, 0, 4));
  if (magic !== 'SCIV') throw new Error(`not an inverse field (${magic})`);
  const nx = view.getUint32(4, true);
  const ny = view.getUint32(8, true);
  const field = new Float32Array(buffer.slice(24, 24 + 8 * nx * ny));
  let max = 0;
  for (let i = 0; i < field.length; i += 2) max = Math.max(max, Math.sqrt(field[i] * field[i] + field[i + 1] * field[i + 1]));
  return { x0: view.getFloat32(12, true), y0: view.getFloat32(16, true), step: view.getFloat32(20, true), nx, ny, field, max };
}
