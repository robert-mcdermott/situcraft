// The tissue viewer's geometry, without the DOM (tested in Node): a camera over the tissue in µm,
// which level of the image pyramid and which of its tiles a view needs, and the scale bar.
// Coordinates are Xenium's: x to the right and y down, in µm from the image's top-left corner.
// Screen positions are in device pixels unless named CSS. Only exact arithmetic is used (no
// logarithms or powers, whose last bits differ between engines: determinism.test.mjs).

// 2^k for a level k, exactly.
function pow2(k) {
  let v = 1;
  for (let i = 0; i < k; i += 1) v *= 2;
  return v;
}

// A camera: the tissue point at the canvas's centre (µm) and the zoom (device pixels per µm).
export function camera(cx, cy, scale) {
  return { cx, cy, scale };
}

export function screenToWorld(cam, width, height, sx, sy) {
  return [cam.cx + (sx - width / 2) / cam.scale, cam.cy + (sy - height / 2) / cam.scale];
}

export function worldToScreen(cam, width, height, x, y) {
  return [(x - cam.cx) * cam.scale + width / 2, (y - cam.cy) * cam.scale + height / 2];
}

// The tissue a canvas shows: { x0, y0, x1, y1 } in µm.
export function visibleRect(cam, width, height) {
  const hw = width / 2 / cam.scale;
  const hh = height / 2 / cam.scale;
  return { x0: cam.cx - hw, y0: cam.cy - hh, x1: cam.cx + hw, y1: cam.cy + hh };
}

// The 3×3 matrix (column-major, as WebGL takes it) from µm to clip space.
export function viewMatrix(cam, width, height) {
  const sx = (2 * cam.scale) / width;
  const sy = (-2 * cam.scale) / height;
  return new Float32Array([sx, 0, 0, 0, sy, 0, -cam.cx * sx, -cam.cy * sy, 1]);
}

// Zooms by factor keeping the tissue under (sx, sy) where it is, within [minScale, maxScale].
export function zoomAt(cam, width, height, sx, sy, factor, { minScale = 1e-4, maxScale = 1e3 } = {}) {
  const scale = Math.min(maxScale, Math.max(minScale, cam.scale * factor));
  const [wx, wy] = screenToWorld(cam, width, height, sx, sy);
  return { cx: wx - (sx - width / 2) / scale, cy: wy - (sy - height / 2) / scale, scale };
}

// A camera showing a rectangle whole, with a margin (a fraction of the canvas) around it.
export function fitCamera(rect, width, height, margin = 0.04) {
  const w = Math.max(1e-6, rect.x1 - rect.x0);
  const h = Math.max(1e-6, rect.y1 - rect.y0);
  const scale = Math.min((width * (1 - 2 * margin)) / w, (height * (1 - 2 * margin)) / h);
  return { cx: (rect.x0 + rect.x1) / 2, cy: (rect.y0 + rect.y1) / 2, scale };
}

// The grid's tile side at a level, in µm (internal/cache/grid.go).
export function tileUm(grid, level) {
  return grid.tile * grid.pixelSize * pow2(level);
}

// The image level to draw at a zoom: the coarsest whose pixels are no larger than a device pixel,
// so the image stays sharp (level 0 when zoomed in past full resolution).
export function imageLevel(grid, cam) {
  const umPerPixel = (1 / cam.scale) * (1 + 1e-9);
  let level = 0;
  while (level + 1 < grid.levels && grid.pixelSize * pow2(level + 1) <= umPerPixel) level += 1;
  return level;
}

// The tiles of a level covering a rectangle: { tx0, ty0, tx1, ty1 } (inclusive), within the
// level's across × down tiles; null when the rectangle misses them.
export function tileRange(grid, level, rect, across, down) {
  const side = tileUm(grid, level);
  const tx0 = Math.max(0, Math.floor(rect.x0 / side));
  const ty0 = Math.max(0, Math.floor(rect.y0 / side));
  const tx1 = Math.min(across - 1, Math.floor(rect.x1 / side));
  const ty1 = Math.min(down - 1, Math.floor(rect.y1 / side));
  if (tx1 < tx0 || ty1 < ty0) return null;
  return { tx0, ty0, tx1, ty1 };
}

// The grid's tiles across and down a level (internal/cache/grid.go).
export function gridAcross(grid, level) {
  return Math.ceil(grid.width / (grid.tile * pow2(level)));
}

export function gridDown(grid, level) {
  return Math.ceil(grid.height / (grid.tile * pow2(level)));
}

// The tiles of the grid holding a rectangle's points (points past the image's edges belong to the
// edge tiles, as the index stores them).
export function gridTileRange(grid, level, rect) {
  const across = gridAcross(grid, level);
  const down = gridDown(grid, level);
  const side = tileUm(grid, level);
  const clamp = (v, hi) => Math.max(0, Math.min(hi, v));
  return {
    tx0: clamp(Math.floor(rect.x0 / side), across - 1),
    ty0: clamp(Math.floor(rect.y0 / side), down - 1),
    tx1: clamp(Math.floor(rect.x1 / side), across - 1),
    ty1: clamp(Math.floor(rect.y1 / side), down - 1),
  };
}

// The density level to draw: the one whose bins are closest to `binPx` CSS pixels, among the
// levels that have density grids.
export function densityLevel(grid, bins, levels, umPerCssPixel, binPx = 2.5) {
  if (!levels.length) return null;
  let best = levels[0];
  let bestError = Infinity;
  for (const level of levels) {
    const ratio = tileUm(grid, level) / bins / (binPx * umPerCssPixel);
    const error = ratio >= 1 ? ratio : 1 / ratio;
    if (error < bestError * (1 - 1e-9)) {
      best = level;
      bestError = error;
    }
  }
  return best;
}

// A scale bar about `targetCss` CSS pixels long: a round length (1, 2 or 5 × 10^k µm, or mm).
export function scaleBar(umPerCssPixel, targetCss = 120) {
  const raw = umPerCssPixel * targetCss;
  let power = 1;
  while (power * 10 <= raw) power *= 10;
  while (power > raw) power /= 10;
  let um = power;
  for (const step of [1, 2, 5, 10]) if (step * power <= raw) um = step * power;
  const label = um >= 1000 ? `${um / 1000} mm` : um >= 1 ? `${um} µm` : `${Math.round(um * 1000)} nm`;
  return { um, css: um / umPerCssPixel, label };
}

// Whether two rectangles overlap.
export function overlaps(a, b) {
  return a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1;
}
