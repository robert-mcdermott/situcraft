// One drawing, three outputs: a figure is a list of marks, painted on a canvas for its preview,
// written as SVG, and written as PDF (lib/pdf.js), so a panel is drawn once and looks the same in
// each, in Node as in the page. Coordinates are in the figure's units (points, 1/72 inch), x right
// and y down from the top left.
//
// Marks are plain objects:
//   { type: 'rect', x, y, w, h, fill?, stroke?, width?, radius?, dash?, opacity?, title? }
//   { type: 'hatch', x, y, w, h, fill, stroke, spacing?, width?, title? }   diagonal lines on a fill
//   { type: 'line', x1, y1, x2, y2, stroke, width?, dash?, opacity? }
//   { type: 'path', d, fill?, stroke?, width?, dash?, opacity?, rule?, title? }   d: absolute M L H V C Z;
//       rule 'evenodd' fills by the even-odd rule (holes), else nonzero
//   { type: 'circle', x, y, r, fill?, stroke?, width?, opacity?, title? }
//   { type: 'text', x, y, text, size, bold?, fill?, anchor?, baseline?, rotate?, title? }
//       anchor 'start' | 'middle' | 'end'; baseline 'alphabetic' | 'middle' | 'top'; rotate in
//       degrees clockwise about (x, y)
//   { type: 'image', x, y, w, h, image: { width, height, rgba, png? }, smooth? }
//   { type: 'group', clip?: { x, y, w, h }, marks, label? }
// Text is set in Helvetica (Arial where the system has no Helvetica), whose widths textWidth gives
// for layout; PDF writes it as text in the standard font every reader has.
//
// Adapted from MaveScape 0.8.0 (web/lib/draw.js, Apache-2.0, the same author). Changed: a path's
// fill rule.

import { PDFPage, ellipsizeText, parseColor, textWidth } from './pdf.js';
import { encodePNG } from './png.js';

export { ellipsizeText, textWidth };

export const FONT = 'Helvetica, Arial, sans-serif';

// The baseline's offset below y for a baseline name, as lib/pdf.js places text.
export function baselineShift(baseline, size) {
  if (baseline === 'top') return 0.8 * size;
  if (baseline === 'middle') return 0.33 * size;
  if (baseline === 'bottom') return -0.2 * size;
  return 0;
}

const num = (v) => {
  const r = Math.round(v * 100) / 100;
  return Object.is(r, -0) ? '0' : String(r);
};
const esc = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

// --- Paths ------------------------------------------------------------------------------------------

// An SVG path of absolute commands (M, L, H, V, C, Z) as [{ op, args }] in absolute coordinates.
export function parsePath(d) {
  const tokens = String(d).match(/[MLHVCZmlhvcz]|-?(?:\d+\.?\d*|\.\d+)(?:e[-+]?\d+)?/g) ?? [];
  const out = [];
  let i = 0;
  let x = 0;
  let y = 0;
  let op = null;
  const take = (n) => {
    const args = tokens.slice(i, i + n).map(Number);
    if (args.length < n || args.some((v) => !Number.isFinite(v))) throw new Error(`A path ends early: "${d}"`);
    i += n;
    return args;
  };
  while (i < tokens.length) {
    if (/[A-Za-z]/.test(tokens[i])) {
      op = tokens[i];
      i += 1;
      if (op !== op.toUpperCase()) throw new Error(`Paths take absolute commands only: "${op}" in "${d}"`);
      if (op === 'Z') {
        out.push({ op: 'Z', args: [] });
        continue;
      }
    }
    if (op === 'M' || op === 'L') {
      [x, y] = take(2);
      out.push({ op, args: [x, y] });
      if (op === 'M') op = 'L';
    } else if (op === 'H') {
      [x] = take(1);
      out.push({ op: 'L', args: [x, y] });
    } else if (op === 'V') {
      [y] = take(1);
      out.push({ op: 'L', args: [x, y] });
    } else if (op === 'C') {
      const a = take(6);
      [x, y] = [a[4], a[5]];
      out.push({ op: 'C', args: a });
    } else {
      throw new Error(`A path needs a command first: "${d}"`);
    }
  }
  return out;
}

function pathText(segments) {
  return segments.map((s) => `${s.op}${s.args.map(num).join(' ')}`).join('');
}

// --- Placing ----------------------------------------------------------------------------------------

// Marks moved by (dx, dy) and scaled by k about the origin: text, strokes and dashes scale with
// them. For drawings made at another size placed in a figure.
export function placeMarks(marks, { dx = 0, dy = 0, k = 1 } = {}) {
  const X = (x) => x * k + dx;
  const Y = (y) => y * k + dy;
  const L = (v) => v * k;
  return marks.map((m) => {
    const width = m.width === undefined ? undefined : L(m.width);
    const dash = m.dash ? m.dash.map(L) : m.dash;
    switch (m.type) {
      case 'rect': case 'hatch': case 'image':
        return { ...m, x: X(m.x), y: Y(m.y), w: L(m.w), h: L(m.h), ...(m.radius ? { radius: L(m.radius) } : {}), ...(m.spacing ? { spacing: L(m.spacing) } : {}), ...(width === undefined ? {} : { width }), ...(dash ? { dash } : {}) };
      case 'line':
        return { ...m, x1: X(m.x1), y1: Y(m.y1), x2: X(m.x2), y2: Y(m.y2), ...(width === undefined ? {} : { width }), ...(dash ? { dash } : {}) };
      case 'circle':
        return { ...m, x: X(m.x), y: Y(m.y), r: L(m.r), ...(width === undefined ? {} : { width }) };
      case 'text':
        return { ...m, x: X(m.x), y: Y(m.y), size: L(m.size) };
      case 'path':
        return { ...m, d: pathText(parsePath(m.d).map((s) => ({ op: s.op, args: s.args.map((v, j) => (j % 2 ? Y(v) : X(v))) }))), ...(width === undefined ? {} : { width }) };
      case 'group':
        return { ...m, ...(m.clip ? { clip: { x: X(m.clip.x), y: Y(m.clip.y), w: L(m.clip.w), h: L(m.clip.h) } } : {}), marks: placeMarks(m.marks, { dx, dy, k }) };
      default:
        throw new Error(`Unknown mark "${m.type}"`);
    }
  });
}

// Every text mark, groups opened: for checks (the smallest text) and descriptions.
export function textMarks(marks, out = []) {
  for (const m of marks) {
    if (m.type === 'text') out.push(m);
    else if (m.type === 'group') textMarks(m.marks, out);
  }
  return out;
}

// The diagonal lines of a hatch, as segments clipped to its box by the caller.
function hatchLines(m) {
  const s = m.spacing ?? 3;
  const lines = [];
  for (let c = -m.h; c < m.w + s; c += s) lines.push([m.x + c, m.y + m.h, m.x + c + m.h, m.y]);
  return lines;
}

// --- SVG --------------------------------------------------------------------------------------------

async function pngOf(image) {
  if (!image.png) image.png = await encodePNG(image.rgba, image.width, image.height);
  return image.png;
}

function base64(bytes) {
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(binary);
}

// The marks as an SVG document of width × height points. options: { title, desc, metadata (text
// placed in <metadata>, escaped), background, sized (false: a viewBox only, for a page that sizes
// it), font }. Deterministic: the same marks, the same text.
export async function marksToSVG(marks, width, height, options = {}) {
  const images = [];
  const collect = (list) => {
    for (const m of list) {
      if (m.type === 'image') images.push(m.image);
      else if (m.type === 'group') collect(m.marks);
    }
  };
  collect(marks);
  for (const image of images) await pngOf(image);
  return marksToSVGSync(marks, width, height, options);
}

// As marksToSVG, without waiting: every image mark has its PNG (image.png) already.
export function marksToSVGSync(marks, width, height, options = {}) {
  const defs = [];
  const patterns = new Map();
  let clips = 0;
  const pattern = (m) => {
    const key = `${m.fill}|${m.stroke}|${m.spacing ?? 3}|${m.width ?? 0.6}`;
    if (!patterns.has(key)) {
      const id = `hatch${patterns.size + 1}`;
      const s = m.spacing ?? 3;
      patterns.set(key, id);
      defs.push(`<pattern id="${id}" width="${num(s)}" height="${num(s)}" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="${num(s)}" height="${num(s)}" fill="${m.fill}"/><line x1="0" y1="0" x2="0" y2="${num(s)}" stroke="${m.stroke}" stroke-width="${num(m.width ?? 0.6)}"/></pattern>`);
    }
    return patterns.get(key);
  };
  const paint = (m) => {
    const fill = m.fill ?? 'none';
    const stroke = m.stroke ? ` stroke="${m.stroke}" stroke-width="${num(m.width ?? 1)}"${m.dash ? ` stroke-dasharray="${m.dash.map(num).join(' ')}"` : ''}` : '';
    const opacity = m.opacity !== undefined && m.opacity < 1 ? ` opacity="${num(m.opacity)}"` : '';
    return ` fill="${fill}"${stroke}${opacity}`;
  };
  const title = (m) => (m.title ? `<title>${esc(m.title)}</title>` : '');
  const write = (list, out) => {
    for (const m of list) {
      if (m.type === 'rect') {
        const close = m.title ? `>${title(m)}</rect>` : '/>';
        out.push(`<rect x="${num(m.x)}" y="${num(m.y)}" width="${num(m.w)}" height="${num(m.h)}"${m.radius ? ` rx="${num(m.radius)}"` : ''}${paint(m)}${close}`);
      } else if (m.type === 'hatch') {
        const close = m.title ? `>${title(m)}</rect>` : '/>';
        out.push(`<rect x="${num(m.x)}" y="${num(m.y)}" width="${num(m.w)}" height="${num(m.h)}" fill="url(#${pattern(m)})"${close}`);
      } else if (m.type === 'line') {
        out.push(`<line x1="${num(m.x1)}" y1="${num(m.y1)}" x2="${num(m.x2)}" y2="${num(m.y2)}" stroke="${m.stroke}" stroke-width="${num(m.width ?? 1)}"${m.dash ? ` stroke-dasharray="${m.dash.map(num).join(' ')}"` : ''}${m.opacity !== undefined && m.opacity < 1 ? ` opacity="${num(m.opacity)}"` : ''}/>`);
      } else if (m.type === 'path') {
        const close = m.title ? `>${title(m)}</path>` : '/>';
        out.push(`<path d="${pathText(parsePath(m.d))}"${paint(m)}${m.rule === 'evenodd' ? ' fill-rule="evenodd"' : ''}${close}`);
      } else if (m.type === 'circle') {
        out.push(`<circle cx="${num(m.x)}" cy="${num(m.y)}" r="${num(m.r)}"${paint(m)}/>`);
      } else if (m.type === 'text') {
        const y = m.y + baselineShift(m.baseline, m.size);
        const rotate = m.rotate ? ` transform="rotate(${num(m.rotate)} ${num(m.x)} ${num(m.y)})"` : '';
        out.push(`<text x="${num(m.x)}" y="${num(y)}" font-size="${num(m.size)}"${m.bold ? ' font-weight="bold"' : ''}${m.anchor && m.anchor !== 'start' ? ` text-anchor="${m.anchor}"` : ''} fill="${m.fill ?? '#171b26'}"${rotate}>${esc(m.text)}${title(m)}</text>`);
      } else if (m.type === 'image') {
        if (!m.image.png) throw new Error('An image mark has no PNG yet: write the SVG with marksToSVG.');
        out.push(`<image x="${num(m.x)}" y="${num(m.y)}" width="${num(m.w)}" height="${num(m.h)}" preserveAspectRatio="none"${m.smooth ? '' : ' image-rendering="pixelated"'} href="data:image/png;base64,${base64(m.image.png)}"/>`);
      } else if (m.type === 'group') {
        let open = '<g';
        if (m.clip) {
          clips += 1;
          defs.push(`<clipPath id="clip${clips}"><rect x="${num(m.clip.x)}" y="${num(m.clip.y)}" width="${num(m.clip.w)}" height="${num(m.clip.h)}"/></clipPath>`);
          open += ` clip-path="url(#clip${clips})"`;
        }
        // A named group is a group to assistive technology (a bare g may not carry a name).
        if (m.label) open += ` role="group" aria-label="${esc(m.label)}"`;
        out.push(`${open}>`);
        write(m.marks, out);
        out.push('</g>');
      } else {
        throw new Error(`Unknown mark "${m.type}"`);
      }
    }
  };
  const body = [];
  write(marks, body);
  const size = options.sized === false ? '' : ` width="${num(width)}pt" height="${num(height)}pt"`;
  const head = [`<svg xmlns="http://www.w3.org/2000/svg"${size} viewBox="0 0 ${num(width)} ${num(height)}" font-family="${esc(options.font ?? FONT)}" role="img">`];
  if (options.title) head.push(`<title>${esc(options.title)}</title>`);
  if (options.desc) head.push(`<desc>${esc(options.desc)}</desc>`);
  if (options.metadata) head.push(`<metadata>${esc(options.metadata)}</metadata>`);
  if (defs.length) head.push(`<defs>${defs.join('')}</defs>`);
  if (options.background) head.push(`<rect width="${num(width)}" height="${num(height)}" fill="${options.background}"/>`);
  return `${[...head, ...body, '</svg>'].join('\n')}\n`;
}

// --- Canvas -----------------------------------------------------------------------------------------

// The marks painted on a 2D context (already scaled to the figure's units). imageOf(image) gives
// something drawImage takes for an image mark (the page makes one from its RGBA).
export function paintMarks(ctx, marks, { imageOf = null } = {}) {
  const fillStroke = (m) => {
    const alpha = m.opacity !== undefined && m.opacity < 1;
    if (alpha) {
      ctx.save();
      ctx.globalAlpha = m.opacity;
    }
    if (m.fill && m.fill !== 'none') {
      ctx.fillStyle = m.fill;
      ctx.fill(m.rule === 'evenodd' ? 'evenodd' : 'nonzero');
    }
    if (m.stroke) {
      ctx.strokeStyle = m.stroke;
      ctx.lineWidth = m.width ?? 1;
      ctx.setLineDash?.(m.dash ?? []);
      ctx.stroke();
      ctx.setLineDash?.([]);
    }
    if (alpha) ctx.restore();
  };
  for (const m of marks) {
    if (m.type === 'rect') {
      ctx.beginPath();
      if (m.radius && ctx.roundRect) ctx.roundRect(m.x, m.y, m.w, m.h, m.radius);
      else ctx.rect(m.x, m.y, m.w, m.h);
      fillStroke(m);
    } else if (m.type === 'hatch') {
      ctx.save();
      ctx.beginPath();
      ctx.rect(m.x, m.y, m.w, m.h);
      ctx.clip();
      ctx.fillStyle = m.fill;
      ctx.fillRect(m.x, m.y, m.w, m.h);
      ctx.strokeStyle = m.stroke;
      ctx.lineWidth = m.width ?? 0.6;
      ctx.beginPath();
      for (const [x1, y1, x2, y2] of hatchLines(m)) {
        ctx.moveTo(x1, y1);
        ctx.lineTo(x2, y2);
      }
      ctx.stroke();
      ctx.restore();
    } else if (m.type === 'line') {
      ctx.beginPath();
      ctx.moveTo(m.x1, m.y1);
      ctx.lineTo(m.x2, m.y2);
      fillStroke({ stroke: m.stroke, width: m.width, dash: m.dash, opacity: m.opacity });
    } else if (m.type === 'path') {
      ctx.beginPath();
      for (const s of parsePath(m.d)) {
        if (s.op === 'M') ctx.moveTo(...s.args);
        else if (s.op === 'L') ctx.lineTo(...s.args);
        else if (s.op === 'C') ctx.bezierCurveTo(...s.args);
        else ctx.closePath();
      }
      fillStroke(m);
    } else if (m.type === 'circle') {
      ctx.beginPath();
      ctx.arc(m.x, m.y, m.r, 0, Math.PI * 2);
      fillStroke(m);
    } else if (m.type === 'text') {
      ctx.save();
      ctx.translate(m.x, m.y);
      if (m.rotate) ctx.rotate((m.rotate * Math.PI) / 180);
      ctx.font = `${m.bold ? 'bold ' : ''}${m.size}px ${FONT}`;
      ctx.fillStyle = m.fill ?? '#171b26';
      ctx.textAlign = m.anchor === 'end' ? 'right' : m.anchor === 'middle' ? 'center' : 'left';
      ctx.textBaseline = 'alphabetic';
      ctx.fillText(m.text, 0, baselineShift(m.baseline, m.size));
      ctx.restore();
    } else if (m.type === 'image') {
      const source = imageOf?.(m.image);
      if (source) {
        ctx.save();
        ctx.imageSmoothingEnabled = Boolean(m.smooth);
        ctx.drawImage(source, m.x, m.y, m.w, m.h);
        ctx.restore();
      }
    } else if (m.type === 'group') {
      ctx.save();
      if (m.clip) {
        ctx.beginPath();
        ctx.rect(m.clip.x, m.clip.y, m.clip.w, m.clip.h);
        ctx.clip();
      }
      paintMarks(ctx, m.marks, { imageOf });
      ctx.restore();
    }
  }
}

// --- PDF --------------------------------------------------------------------------------------------

// A color with an opacity, as lib/pdf.js reads it (a translucent fill or stroke).
function withOpacity(color, opacity) {
  if (!color || color === 'none' || opacity === undefined || opacity >= 1) return color;
  const c = parseColor(color);
  return c ? `rgba(${Math.round(c.r * 255)}, ${Math.round(c.g * 255)}, ${Math.round(c.b * 255)}, ${opacity})` : color;
}

// The marks written on a PDFPage drawn in points (scale 1).
export function marksToPDF(page, marks) {
  for (const raw of marks) {
    const m = raw.opacity !== undefined && raw.opacity < 1 ? { ...raw, fill: withOpacity(raw.fill, raw.opacity), stroke: withOpacity(raw.stroke, raw.opacity) } : raw;
    if (m.type === 'rect') {
      if (m.radius) page.roundRect(m.x, m.y, m.w, m.h, m.radius);
      else page.rect(m.x, m.y, m.w, m.h);
      page.draw({ fill: m.fill === 'none' ? null : m.fill, stroke: m.stroke ?? null, width: m.width ?? 1, dash: m.dash ?? null });
    } else if (m.type === 'hatch') {
      page.save();
      page.clipRect(m.x, m.y, m.w, m.h);
      page.fillRect(m.x, m.y, m.w, m.h, m.fill);
      for (const [x1, y1, x2, y2] of hatchLines(m)) {
        page.moveTo(x1, y1);
        page.lineTo(x2, y2);
      }
      page.draw({ stroke: m.stroke, width: m.width ?? 0.6 });
      page.restore();
    } else if (m.type === 'line') {
      page.line(m.x1, m.y1, m.x2, m.y2, m.stroke, m.width ?? 1, m.dash ? { dash: m.dash } : {});
    } else if (m.type === 'path') {
      for (const s of parsePath(m.d)) {
        if (s.op === 'M') page.moveTo(...s.args);
        else if (s.op === 'L') page.lineTo(...s.args);
        else if (s.op === 'C') page.curveTo(...s.args);
        else page.closePath();
      }
      page.draw({ fill: m.fill === 'none' ? null : m.fill, stroke: m.stroke ?? null, width: m.width ?? 1, dash: m.dash ?? null, join: 1, evenOdd: m.rule === 'evenodd' });
    } else if (m.type === 'circle') {
      const k = 0.5523 * m.r;
      page.moveTo(m.x + m.r, m.y);
      page.curveTo(m.x + m.r, m.y + k, m.x + k, m.y + m.r, m.x, m.y + m.r);
      page.curveTo(m.x - k, m.y + m.r, m.x - m.r, m.y + k, m.x - m.r, m.y);
      page.curveTo(m.x - m.r, m.y - k, m.x - k, m.y - m.r, m.x, m.y - m.r);
      page.curveTo(m.x + k, m.y - m.r, m.x + m.r, m.y - k, m.x + m.r, m.y);
      page.closePath();
      page.draw({ fill: m.fill === 'none' ? null : m.fill, stroke: m.stroke ?? null, width: m.width ?? 1 });
    } else if (m.type === 'text') {
      page.text(m.text, m.x, m.y, { size: m.size, bold: Boolean(m.bold), color: m.fill ?? '#171b26', align: m.anchor === 'end' ? 'right' : m.anchor === 'middle' ? 'center' : 'left', baseline: m.baseline ?? 'alphabetic', rotate: -(m.rotate ?? 0) });
    } else if (m.type === 'image') {
      page.image(m.image, m.x, m.y, m.w, m.h, { interpolate: Boolean(m.smooth) });
    } else if (m.type === 'group') {
      page.save();
      if (m.clip) page.clipRect(m.clip.x, m.clip.y, m.clip.w, m.clip.h);
      marksToPDF(page, m.marks);
      page.restore();
    }
  }
  return page;
}

// A page of width × height points with the marks on it.
export function pdfPage(marks, width, height) {
  return marksToPDF(new PDFPage(width, height, { scale: 1 }), marks);
}
