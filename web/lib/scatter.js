// A scatter plot of many points (a map of cells), drawn into an image: the view (which part of the
// plane shows, at what scale), points rasterized as small squares in their colors (the selected on
// top, the others dimmed while there is a selection), the point nearest a place, and the points
// inside a drawn polygon. Pure functions: the page draws the image on a 2-D canvas; tests run in
// Node.

// A view: the plane's point at the image's center (cx, cy) and the plane's units per pixel.
export function fitView(xy, n, width, height, pad = 0.04) {
  let x0 = Infinity; let y0 = Infinity; let x1 = -Infinity; let y1 = -Infinity;
  for (let i = 0; i < n; i += 1) {
    const x = xy[2 * i];
    const y = xy[2 * i + 1];
    if (!Number.isFinite(x) || !Number.isFinite(y)) continue;
    if (x < x0) x0 = x;
    if (x > x1) x1 = x;
    if (y < y0) y0 = y;
    if (y > y1) y1 = y;
  }
  if (!(x1 >= x0)) return { cx: 0, cy: 0, scale: 1 };
  const w = Math.max(x1 - x0, 1e-9);
  const h = Math.max(y1 - y0, 1e-9);
  const scale = Math.max(w / (width * (1 - 2 * pad)), h / (height * (1 - 2 * pad)));
  return { cx: (x0 + x1) / 2, cy: (y0 + y1) / 2, scale };
}

// The plane's point at a pixel, and back. The plane's y grows upward on the image.
export function toPlane(view, width, height, px, py) {
  return [view.cx + (px - width / 2) * view.scale, view.cy - (py - height / 2) * view.scale];
}
export function toPixel(view, width, height, x, y) {
  return [width / 2 + (x - view.cx) / view.scale, height / 2 - (y - view.cy) / view.scale];
}

// The view zoomed by factor about a pixel (it stays under the pointer).
export function zoomAt(view, width, height, px, py, factor) {
  const [x, y] = toPlane(view, width, height, px, py);
  const scale = view.scale / factor;
  return { cx: x - (px - width / 2) * scale, cy: y + (py - height / 2) * scale, scale };
}

// RGBA pixels: each point a size × size square in its color (colors: RGBA bytes per point, alpha 0
// to skip it). With a selection (a mask per point), the others are drawn first at dim of their
// opacity and the selected over them; without, in order. Background transparent.
export function rasterize({ xy, n, colors, view, width, height, size = 3, selected = null, dim = 0.18 }) {
  const out = new Uint8ClampedArray(4 * width * height);
  const half = Math.floor(size / 2);
  const draw = (i, alpha) => {
    const a = (colors[4 * i + 3] / 255) * alpha;
    if (!(a > 0)) return;
    const px = Math.round(width / 2 + (xy[2 * i] - view.cx) / view.scale) - half;
    const py = Math.round(height / 2 - (xy[2 * i + 1] - view.cy) / view.scale) - half;
    if (px + size <= 0 || py + size <= 0 || px >= width || py >= height) return;
    const r = colors[4 * i];
    const g = colors[4 * i + 1];
    const b = colors[4 * i + 2];
    for (let y = Math.max(0, py); y < Math.min(height, py + size); y += 1) {
      for (let x = Math.max(0, px); x < Math.min(width, px + size); x += 1) {
        const o = 4 * (y * width + x);
        const under = out[o + 3] / 255;
        const total = a + under * (1 - a);
        out[o] = (r * a + out[o] * under * (1 - a)) / total;
        out[o + 1] = (g * a + out[o + 1] * under * (1 - a)) / total;
        out[o + 2] = (b * a + out[o + 2] * under * (1 - a)) / total;
        out[o + 3] = total * 255;
      }
    }
  };
  if (!selected) {
    for (let i = 0; i < n; i += 1) draw(i, 1);
    return out;
  }
  for (let i = 0; i < n; i += 1) if (!selected[i]) draw(i, dim);
  for (let i = 0; i < n; i += 1) if (selected[i]) draw(i, 1);
  return out;
}

// A grid over the points, for finding the one nearest a place.
export function pointGrid(xy, n) {
  let x0 = Infinity; let y0 = Infinity; let x1 = -Infinity; let y1 = -Infinity;
  for (let i = 0; i < n; i += 1) {
    x0 = Math.min(x0, xy[2 * i]); x1 = Math.max(x1, xy[2 * i]);
    y0 = Math.min(y0, xy[2 * i + 1]); y1 = Math.max(y1, xy[2 * i + 1]);
  }
  const side = Math.max(1, Math.floor(Math.sqrt(n / 4)));
  const w = Math.max(x1 - x0, y1 - y0, 1e-9) / side;
  const cell = (v, o) => Math.min(side - 1, Math.max(0, Math.floor((v - o) / w)));
  const start = new Int32Array(side * side + 1);
  const at = new Int32Array(n);
  for (let i = 0; i < n; i += 1) {
    at[i] = cell(xy[2 * i + 1], y0) * side + cell(xy[2 * i], x0);
    start[at[i] + 1] += 1;
  }
  for (let c = 0; c < side * side; c += 1) start[c + 1] += start[c];
  const members = new Int32Array(n);
  const fill = start.slice(0, side * side);
  for (let i = 0; i < n; i += 1) members[fill[at[i]]++] = i;
  return { xy, n, side, w, x0, y0, start, members, cell };
}

// The point nearest (x, y) within maxDistance (plane units), or −1; ties go to the first.
export function nearestPoint(grid, x, y, maxDistance) {
  const { xy, side, w, x0, y0, start, members, cell } = grid;
  const reach = Math.ceil(maxDistance / w);
  const cx = cell(x, x0);
  const cy = cell(y, y0);
  let best = -1;
  let bestD = maxDistance * maxDistance;
  for (let gy = Math.max(0, cy - reach); gy <= Math.min(side - 1, cy + reach); gy += 1) {
    for (let gx = Math.max(0, cx - reach); gx <= Math.min(side - 1, cx + reach); gx += 1) {
      const c = gy * side + gx;
      for (let k = start[c]; k < start[c + 1]; k += 1) {
        const i = members[k];
        const dx = xy[2 * i] - x;
        const dy = xy[2 * i + 1] - y;
        const d = dx * dx + dy * dy;
        if (d < bestD || (d === bestD && best >= 0 && i < best)) {
          best = i;
          bestD = d;
        }
      }
    }
  }
  return best;
}

// Whether (x, y) lies inside a polygon ([[x, y], …]), by the even–odd rule.
export function insidePolygon(polygon, x, y) {
  let inside = false;
  for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i, i += 1) {
    const [xi, yi] = polygon[i];
    const [xj, yj] = polygon[j];
    if ((yi > y) !== (yj > y) && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) inside = !inside;
  }
  return inside;
}

// The points inside a polygon: a mask per point.
export function pointsInPolygon(xy, n, polygon) {
  const out = new Uint8Array(n);
  if (polygon.length < 3) return out;
  let x0 = Infinity; let y0 = Infinity; let x1 = -Infinity; let y1 = -Infinity;
  for (const [x, y] of polygon) {
    x0 = Math.min(x0, x); x1 = Math.max(x1, x); y0 = Math.min(y0, y); y1 = Math.max(y1, y);
  }
  for (let i = 0; i < n; i += 1) {
    const x = xy[2 * i];
    const y = xy[2 * i + 1];
    if (x < x0 || x > x1 || y < y0 || y > y1) continue;
    if (insidePolygon(polygon, x, y)) out[i] = 1;
  }
  return out;
}

// RGBA bytes per point for values on a colormap's lookup table (256 × 3) over [lo, hi]; values
// that are not numbers are drawn grey, faint.
export function valueColors(values, lut, lo, hi) {
  const out = new Uint8Array(4 * values.length);
  const span = hi - lo || 1;
  for (let i = 0; i < values.length; i += 1) {
    const v = values[i];
    if (!Number.isFinite(v)) {
      out.set([128, 128, 128, 60], 4 * i);
      continue;
    }
    const k = Math.max(0, Math.min(255, Math.round((255 * (v - lo)) / span)));
    out.set([lut[3 * k], lut[3 * k + 1], lut[3 * k + 2], 255], 4 * i);
  }
  return out;
}
