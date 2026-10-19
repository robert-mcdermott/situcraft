// A tissue panel of a figure (wave 8, slice 1; requirement E6): a saved view of a dataset drawn at
// the figure's size. The place shown is the panel's region, in µm (its center and width; its height
// follows the panel's shape). The stains are composited from their samples as the Viewer's shader
// composites them (each visible channel's sample windowed, raised to its gamma and multiplied by
// its color, the channels added and clamped), at the output's resolution, and placed as one
// image; the cells are paths (a path for each fill color and one of every outline), the
// transcripts points, both placed in µm; a scale bar of a round length; and a legend of what is
// drawn. Pure: the page gathers the data (ui/figure-data.js), this draws it, the same marks for
// the same data.

import { pow } from './dmath.js';
import { textWidth } from './draw.js';

// The smallest text a figure sets (pt): a journal's usual minimum.
export const MIN_TEXT = 6;
const INK = '#171b26';

// --- The scale bar -------------------------------------------------------------------------------

// A round length (1, 2 or 5 times a power of ten, in µm): the longest of a fifth of a width in µm
// or less (the shortest, 1 nm, for a narrower width).
export function niceLength(widthUm) {
  const target = widthUm / 5;
  let best = null;
  for (let k = -3; k <= 6; k += 1) {
    for (const m of [1, 2, 5]) {
      const v = Number(`${m}e${k}`);
      if (v <= target && (best === null || v > best)) best = v;
    }
  }
  return best ?? 1e-3;
}

// A length in µm as the scale bar says it ("50 µm", "1.5 mm" from 1 mm on).
export function lengthText(um) {
  if (um >= 1000) return `${Number((um / 1000).toPrecision(6))} mm`;
  return `${Number(um.toPrecision(6))} µm`;
}

// --- The legend ----------------------------------------------------------------------------------

// The legend's entries laid out in rows across a width: each entry a swatch and its text, a group's
// name in bold before its first entry; a scale (a continuous fill) its own row. items: [{ group,
// name, kind: 'square' | 'dot' | 'scale', color, colors? (a scale's stops), min?, max? }].
// Returns { rows: [[{ x, item, width }]], height }.
export function legendLayout(items, width, size) {
  const swatch = size;
  const gap = size * 0.6;
  const lineH = size * 1.45;
  const rows = [];
  let row = [];
  let x = 0;
  let group = null;
  const place = (entry, w) => {
    if (row.length && x + w > width + 0.01) {
      rows.push(row);
      row = [];
      x = 0;
    }
    row.push({ ...entry, x, width: w });
    x += w + gap;
  };
  for (const item of items) {
    if (item.group !== group) {
      group = item.group;
      if (group) place({ heading: `${group}:` }, textWidth(`${group}:`, size, true));
    }
    if (item.kind === 'scale') {
      const bar = Math.min(width * 0.4, size * 10);
      const label = `${item.name} (${item.min} to ${item.max})`;
      place({ item, bar }, bar + gap / 2 + textWidth(label, size));
    } else {
      place({ item }, swatch + gap / 2 + textWidth(item.name, size));
    }
  }
  if (row.length) rows.push(row);
  return { rows, height: rows.length * lineH, lineH, swatch, gap };
}

// --- Layout --------------------------------------------------------------------------------------

// Where a tissue panel draws: its image (the region, filling the box's width) above its legend,
// and the region in µm. panel: { region: { cx, cy, width } }; legend: its items; text: the
// figure's text size. Returns { image: { x, y, w, h } (pt), region: { x0, y0, x1, y1 } (µm),
// umPerPt, legend: { x, y, w, layout } }.
export function tissueLayout(box, panel, legend, text) {
  const size = Math.max(MIN_TEXT, text);
  const laid = legendLayout(legend ?? [], box.w, size);
  const legendH = laid.rows.length ? laid.height + size * 0.4 : 0;
  const image = { x: box.x, y: box.y, w: box.w, h: Math.max(10, box.h - legendH) };
  const umPerPt = panel.region.width / image.w;
  const height = image.h * umPerPt;
  const region = { x0: panel.region.cx - panel.region.width / 2, y0: panel.region.cy - height / 2, x1: panel.region.cx + panel.region.width / 2, y1: panel.region.cy + height / 2 };
  return { image, region, umPerPt, legend: { x: box.x, y: image.y + image.h + size * 0.4, w: box.w, layout: laid }, size };
}

// The pixels of a raster of the image area at a resolution (dots per inch of its printed size),
// at most limit a side.
export function rasterSize(image, dpi = 300, limit = 6000) {
  const k = dpi / 72;
  return { width: Math.max(1, Math.min(limit, Math.round(image.w * k))), height: Math.max(1, Math.min(limit, Math.round(image.h * k))) };
}

// --- The stains ----------------------------------------------------------------------------------

function rgbOf(color) {
  if (Array.isArray(color)) return color;
  const hex = String(color).replace('#', '');
  const full = hex.length === 3 ? [...hex].map((c) => c + c).join('') : hex;
  return [0, 2, 4].map((i) => Number.parseInt(full.slice(i, i + 2), 16) / 255);
}

// A channel's look-up table: each 16-bit sample's contribution (0 to 1) before its color, as the
// shader computes it: clamp((v − low) / max(high − low, 1), 0, 1) raised to its gamma.
export function channelTable(window, gamma) {
  const table = new Float64Array(65536);
  const span = Math.max(window[1] - window[0], 1);
  for (let v = 0; v < 65536; v += 1) {
    const n = Math.min(1, Math.max(0, (v - window[0]) / span));
    table[v] = gamma === 1 || n === 0 || n === 1 ? n : pow(n, gamma);
  }
  return table;
}

// The stains composited over a region at width × height pixels: each pixel's center in µm, the
// sample under it at the channel's level (the nearest, as the shader's texelFetch), its table's
// value times its color, the channels added and clamped to 1, as 8-bit RGBA (opaque).
// channels: [{ window, gamma, color, grid: { x0, y0, um (µm a sample), cols, rows, data
// (Uint16Array, rows × cols) } }]; samples outside a grid are 0.
export function compositeStains({ width, height, region, channels }) {
  const out = new Uint8Array(width * height * 4);
  const sum = new Float64Array(width * height * 3);
  const umX = (region.x1 - region.x0) / width;
  const umY = (region.y1 - region.y0) / height;
  for (const c of channels) {
    const table = channelTable(c.window, c.gamma ?? 1);
    const [r, g, b] = rgbOf(c.color);
    const { grid } = c;
    const cols = new Int32Array(width);
    for (let px = 0; px < width; px += 1) {
      const s = Math.floor((region.x0 + (px + 0.5) * umX - grid.x0) / grid.um);
      cols[px] = s >= 0 && s < grid.cols ? s : -1;
    }
    for (let py = 0; py < height; py += 1) {
      const row = Math.floor((region.y0 + (py + 0.5) * umY - grid.y0) / grid.um);
      if (row < 0 || row >= grid.rows) continue;
      const base = row * grid.cols;
      for (let px = 0; px < width; px += 1) {
        if (cols[px] < 0) continue;
        const n = table[grid.data[base + cols[px]]];
        if (n === 0) continue;
        const i = 3 * (py * width + px);
        sum[i] += r * n;
        sum[i + 1] += g * n;
        sum[i + 2] += b * n;
      }
    }
  }
  for (let i = 0, j = 0; i < sum.length; i += 3, j += 4) {
    out[j] = Math.round(Math.min(1, sum[i]) * 255);
    out[j + 1] = Math.round(Math.min(1, sum[i + 1]) * 255);
    out[j + 2] = Math.round(Math.min(1, sum[i + 2]) * 255);
    out[j + 3] = 255;
  }
  return out;
}

// --- Drawing -------------------------------------------------------------------------------------

const hex2 = (v) => v.toString(16).padStart(2, '0');
const colorText = (rgba, at) => `#${hex2(rgba[at])}${hex2(rgba[at + 1])}${hex2(rgba[at + 2])}`;
const f2 = (v) => {
  const r = Math.round(v * 100) / 100;
  return Object.is(r, -0) ? '0' : String(r);
};

// Whether a ring (flat [x, y, …] in µm) has a point in, or crosses, a region: by its bounds.
function ringMeets(xy, region) {
  let x0 = Infinity;
  let y0 = Infinity;
  let x1 = -Infinity;
  let y1 = -Infinity;
  for (let i = 0; i < xy.length; i += 2) {
    if (xy[i] < x0) x0 = xy[i];
    if (xy[i] > x1) x1 = xy[i];
    if (xy[i + 1] < y0) y0 = xy[i + 1];
    if (xy[i + 1] > y1) y1 = xy[i + 1];
  }
  return x1 >= region.x0 && x0 <= region.x1 && y1 >= region.y0 && y0 <= region.y1;
}

function ringPath(xy, X, Y) {
  let d = `M${f2(X(xy[0]))} ${f2(Y(xy[1]))}`;
  for (let i = 2; i < xy.length; i += 2) d += `L${f2(X(xy[i]))} ${f2(Y(xy[i + 1]))}`;
  return `${d}Z`;
}

// The legend's marks at (x, y) in rows.
function legendMarks(legend, size) {
  const marks = [];
  const { layout } = legend;
  layout.rows.forEach((row, r) => {
    const y = legend.y + r * layout.lineH;
    const mid = y + layout.lineH / 2;
    for (const e of row) {
      const x = legend.x + e.x;
      if (e.heading) {
        marks.push({ type: 'text', x, y: mid, text: e.heading, size, bold: true, fill: INK, baseline: 'middle' });
        continue;
      }
      const it = e.item;
      if (it.kind === 'scale') {
        const stops = it.colors.length;
        const rgba = new Uint8Array(stops * 4);
        it.colors.forEach((c, i) => {
          const [cr, cg, cb] = rgbOf(c);
          rgba.set([Math.round(cr * 255), Math.round(cg * 255), Math.round(cb * 255), 255], 4 * i);
        });
        marks.push({ type: 'image', x, y: mid - layout.swatch / 2, w: e.bar, h: layout.swatch, image: { width: stops, height: 1, rgba }, smooth: true });
        marks.push({ type: 'text', x: x + e.bar + layout.gap / 2, y: mid, text: `${it.name} (${it.min} to ${it.max})`, size, fill: INK, baseline: 'middle' });
        continue;
      }
      if (it.kind === 'dot') marks.push({ type: 'circle', x: x + layout.swatch / 2, y: mid, r: layout.swatch * 0.32, fill: it.color });
      else marks.push({ type: 'rect', x, y: mid - layout.swatch / 2, w: layout.swatch, h: layout.swatch, fill: it.color, stroke: '#9aa2b1', width: 0.25 });
      marks.push({ type: 'text', x: x + layout.swatch + layout.gap / 2, y: mid, text: it.name, size, fill: INK, baseline: 'middle' });
    }
  });
  return marks;
}

// The panel's marks. data: { dataset (its name), view (its name), legend (items), background,
// stains: { width, height, rgba } or null, channels: [names] (shown), cells: { rings: [{ cell,
// xy }], fill (RGBA per cell, alpha 0 for none) or null, fillName, opacity, outline: { color,
// width, opacity } or null } or null, nuclei: { rings, outline } or null, transcripts: { x, y (µm), gene (index), genes: [{ name,
// color }] } or null }. options: the panel ({ region, scaleBar?, pointSize? }) and the figure's
// text. Returns { marks, description, notes, facts }.
export function tissuePanel(box, data, options) {
  const size = Math.max(MIN_TEXT, options.text ?? 7);
  const layout = tissueLayout(box, options, data.legend ?? [], size);
  const { image, region, umPerPt } = layout;
  const X = (x) => image.x + (x - region.x0) / umPerPt;
  const Y = (y) => image.y + (y - region.y0) / umPerPt;
  const inner = [{ type: 'rect', x: image.x, y: image.y, w: image.w, h: image.h, fill: data.background ?? '#000000' }];
  const notes = [];
  if (data.stains) inner.push({ type: 'image', x: image.x, y: image.y, w: image.w, h: image.h, image: data.stains, smooth: false });
  // Cells: one path of each fill color, then one of every outline, so that a cell's outline is
  // drawn over its neighbors' fills.
  let cellsDrawn = 0;
  const cells = data.cells;
  if (cells) {
    const shown = cells.rings.filter((r) => ringMeets(r.xy, region));
    const counted = new Set(shown.map((r) => r.cell));
    cellsDrawn = counted.size;
    if (cells.fill) {
      // A path of each color and alpha (a label set's unlabeled cells are fainter), its opacity
      // the layer's times the alpha, as the Viewer blends them.
      const byColor = new Map();
      for (const r of shown) {
        if (r.cell < 0 || cells.fill[4 * r.cell + 3] === 0) continue;
        const key = `${colorText(cells.fill, 4 * r.cell)}|${cells.fill[4 * r.cell + 3]}`;
        if (!byColor.has(key)) byColor.set(key, []);
        byColor.get(key).push(ringPath(r.xy, X, Y));
      }
      for (const key of [...byColor.keys()].sort()) {
        const [color, alpha] = key.split('|');
        inner.push({ type: 'path', d: byColor.get(key).join(''), fill: color, opacity: Math.round((cells.opacity ?? 1) * (Number(alpha) / 255) * 1000) / 1000 });
      }
    }
  }
  // Outlines over the fills: the nuclei's, then the cells', as the Viewer draws them.
  const nuclei = data.nuclei;
  if (nuclei?.outline) {
    const shown = nuclei.rings.filter((r) => ringMeets(r.xy, region));
    if (shown.length) inner.push({ type: 'path', d: shown.map((r) => ringPath(r.xy, X, Y)).join(''), fill: 'none', stroke: nuclei.outline.color, width: nuclei.outline.width ?? 0.25, opacity: nuclei.outline.opacity ?? 1 });
  }
  if (cells?.outline) {
    const shown = cells.rings.filter((r) => ringMeets(r.xy, region));
    if (shown.length) inner.push({ type: 'path', d: shown.map((r) => ringPath(r.xy, X, Y)).join(''), fill: 'none', stroke: cells.outline.color, width: cells.outline.width ?? 0.25, opacity: cells.outline.opacity ?? 1 });
  }
  // Transcripts: a point each, of its gene's color, those in the region.
  let pointsDrawn = 0;
  const t = data.transcripts;
  if (t) {
    const r = (options.pointSize ?? 1.2) / 2;
    for (let i = 0; i < t.x.length; i += 1) {
      const x = t.x[i];
      const y = t.y[i];
      if (x < region.x0 || x > region.x1 || y < region.y0 || y > region.y1) continue;
      inner.push({ type: 'circle', x: Math.round(X(x) * 100) / 100, y: Math.round(Y(y) * 100) / 100, r, fill: t.genes[t.gene[i]].color });
      pointsDrawn += 1;
    }
  }
  // The scale bar: bottom right, light on a dark tissue.
  const barUm = options.scaleBar ?? niceLength(region.x1 - region.x0);
  const barPt = barUm / umPerPt;
  const margin = Math.max(4, size * 0.6);
  const ink = luminanceOf(data.background ?? '#000000') < 0.5 ? '#ffffff' : INK;
  const barY = image.y + image.h - margin - 1.5;
  const barX = image.x + image.w - margin - barPt;
  if (barPt > image.w - 2 * margin) notes.push(`The scale bar (${lengthText(barUm)}) is wider than the tissue shown.`);
  else {
    inner.push({ type: 'rect', x: barX, y: barY, w: barPt, h: 1.5, fill: ink });
    inner.push({ type: 'text', x: barX + barPt / 2, y: barY - size * 0.35, text: lengthText(barUm), size, fill: ink, anchor: 'middle' });
  }
  const marks = [{ type: 'group', clip: { ...image }, label: `The tissue of ${data.dataset}`, marks: inner }];
  marks.push(...legendMarks(layout.legend, size));
  const parts = [];
  if (data.channels?.length) parts.push(`the ${joinList(data.channels)} stain${data.channels.length > 1 ? 's' : ''}`);
  if (cells) parts.push(`${cellsDrawn.toLocaleString('en-US')} cells${cells.outline ? ' outlined' : ''}${cells.fill ? `${cells.outline ? ' and' : ''} filled by ${cells.fillName}` : ''}`);
  if (t) parts.push(`${pointsDrawn.toLocaleString('en-US')} transcripts of ${joinList(t.genes.map((g) => g.name))}`);
  const across = lengthText(region.x1 - region.x0);
  const description = `The tissue of ${data.dataset}${data.view ? ` (the saved view "${data.view}")` : ''}, ${across} across${parts.length ? `: ${parts.join('; ')}` : ''}; the scale bar ${lengthText(barUm)}.`;
  return {
    marks,
    description,
    notes,
    facts: { region, umPerPt, image, scaleBar: { um: barUm, pt: barPt }, cells: cellsDrawn, transcripts: pointsDrawn, raster: data.stains ? { width: data.stains.width, height: data.stains.height, level: data.stains.level ?? null } : null },
  };
}

function joinList(parts) {
  if (parts.length <= 1) return parts.join('');
  return `${parts.slice(0, -1).join(', ')} and ${parts.at(-1)}`;
}

function luminanceOf(color) {
  const [r, g, b] = rgbOf(color);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
