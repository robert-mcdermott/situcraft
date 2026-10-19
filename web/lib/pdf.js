// A minimal PDF writer for figures. A page (PDFPage) is drawn in CSS pixels from the top left,
// like a canvas, and written in points (1/72 inch, 0.75 pt per pixel): filled and stroked paths,
// clipping rectangles, images (RGB with an optional alpha mask, Flate-compressed) and text in
// Helvetica and Helvetica-Bold, the standard fonts every reader has, so text stays text
// (searchable, extractable and sharp at any zoom). Text is encoded as WinAnsi; characters outside
// it are written as their nearest ASCII (− as -, ≥ as >=) or "?". Metadata (title, creator) goes
// in the document information dictionary; attachments (info.attachments: [{ name, mime,
// description, data }]) are embedded files that PDF readers list, uncompressed so that SituCraft
// can read them back (a figure's manifest).
//
// Adapted from MaveScape 0.8.0 (web/lib/pdf.js, Apache-2.0, the same author), itself from
// CytoWeave 0.8.0. Changed: SituCraft as the creator and producer.

const encoder = new TextEncoder();

// Widths (1/1000 em) of WinAnsi codes 32–255 in Helvetica and Helvetica-Bold (Adobe's AFM metrics
// of the standard 14 fonts, as reportlab 5.0.1 lists them); 0 marks codes WinAnsi leaves undefined.
const WIDTHS = {
  regular: [278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, 1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556, 333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584, 0, 556, 0, 222, 556, 333, 1000, 556, 556, 333, 1000, 667, 333, 1000, 0, 611, 0, 0, 222, 222, 333, 333, 350, 556, 1000, 333, 1000, 500, 333, 944, 0, 500, 667, 278, 333, 556, 556, 556, 556, 260, 556, 333, 737, 370, 556, 584, 333, 737, 333, 400, 584, 333, 333, 333, 556, 537, 278, 333, 333, 365, 556, 834, 834, 834, 611, 667, 667, 667, 667, 667, 667, 1000, 722, 667, 667, 667, 667, 278, 278, 278, 278, 722, 722, 778, 778, 778, 778, 778, 584, 778, 722, 722, 722, 722, 667, 667, 611, 556, 556, 556, 556, 556, 556, 889, 500, 556, 556, 556, 556, 278, 278, 278, 278, 556, 556, 556, 556, 556, 556, 556, 584, 611, 556, 556, 556, 556, 500, 556, 500],
  bold: [278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611, 975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556, 333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584, 0, 556, 0, 278, 556, 500, 1000, 556, 556, 333, 1000, 667, 333, 1000, 0, 611, 0, 0, 278, 278, 500, 500, 350, 556, 1000, 333, 1000, 556, 333, 944, 0, 500, 667, 278, 333, 556, 556, 556, 556, 280, 556, 333, 737, 370, 556, 584, 333, 737, 333, 400, 584, 333, 333, 333, 611, 556, 278, 333, 333, 365, 556, 834, 834, 834, 611, 722, 722, 722, 722, 722, 722, 1000, 722, 667, 667, 667, 667, 278, 278, 278, 278, 722, 722, 778, 778, 778, 778, 778, 584, 778, 722, 722, 722, 722, 667, 667, 611, 556, 556, 556, 556, 556, 556, 889, 556, 556, 556, 556, 556, 278, 278, 278, 278, 611, 611, 611, 611, 611, 611, 611, 584, 611, 611, 611, 611, 611, 556, 611, 556],
};

// Unicode characters with a WinAnsi code in 0x80–0x9F (the rest of 0xA0–0xFF is Latin-1).
const WIN_ANSI = new Map([[0x20ac, 0x80], [0x201a, 0x82], [0x0192, 0x83], [0x201e, 0x84], [0x2026, 0x85], [0x2020, 0x86], [0x2021, 0x87], [0x02c6, 0x88], [0x2030, 0x89], [0x0160, 0x8a], [0x2039, 0x8b], [0x0152, 0x8c], [0x017d, 0x8e], [0x2018, 0x91], [0x2019, 0x92], [0x201c, 0x93], [0x201d, 0x94], [0x2022, 0x95], [0x2013, 0x96], [0x2014, 0x97], [0x02dc, 0x98], [0x2122, 0x99], [0x0161, 0x9a], [0x203a, 0x9b], [0x0153, 0x9c], [0x017e, 0x9e], [0x0178, 0x9f]]);
// Characters outside WinAnsi that the labels use, written as their nearest ASCII.
const SUBSTITUTES = new Map([['−', '-'], ['≥', '>='], ['≤', '<='], ['≠', '!='], ['≈', '~'], ['→', '->'], ['←', '<-'], ['∞', 'inf'], ['χ', 'chi'], ['σ', 'sigma'], ['Δ', 'delta'], ['α', 'alpha'], ['β', 'beta'], ['γ', 'gamma'], ['κ', 'kappa'], ['λ', 'lambda'], ['ρ', 'rho'], ['μ', '\u00b5'], ['₁', '1'], ['₂', '2'], ['₀', '0'], ['\u00a0', ' '], ['\u2009', ' '], ['\u202f', ' ']]);

// The WinAnsi bytes of a string.
export function winAnsi(text) {
  const out = [];
  for (const ch of String(text ?? '')) {
    const code = ch.codePointAt(0);
    if (code >= 0x20 && code < 0x7f) out.push(code);
    else if (code >= 0xa0 && code <= 0xff) out.push(code);
    else if (WIN_ANSI.has(code)) out.push(WIN_ANSI.get(code));
    else if (SUBSTITUTES.has(ch)) for (const c of SUBSTITUTES.get(ch)) out.push(c.charCodeAt(0));
    else out.push(0x3f);
  }
  return out;
}

const SUPERSCRIPTS = new Map([...'⁰¹²³⁴⁵⁶⁷⁸⁹'].map((c, i) => [c, String(i)]).concat([['⁺', '+'], ['⁻', '-']]));

// A string split into runs of normal and superscript characters (as plain characters).
function superscriptRuns(text) {
  const runs = [];
  for (const ch of text) {
    const raised = SUPERSCRIPTS.has(ch);
    const plain = raised ? SUPERSCRIPTS.get(ch) : ch;
    if (runs.length && runs.at(-1).raised === raised) runs.at(-1).text += plain;
    else runs.push({ text: plain, raised });
  }
  return runs;
}

// The width of a string in Helvetica (bold or not) at a size, in the size's units.
export function textWidth(text, size, bold = false) {
  const widths = bold ? WIDTHS.bold : WIDTHS.regular;
  let total = 0;
  for (const run of superscriptRuns(String(text ?? ''))) {
    let sum = 0;
    for (const code of winAnsi(run.text)) sum += widths[code - 32] || 556;
    total += run.raised ? sum * 0.7 : sum;
  }
  return (total * size) / 1000;
}

// The longest prefix of a string, with "…", that fits a width.
export function ellipsizeText(text, width, size, bold = false) {
  const value = String(text ?? '');
  if (textWidth(value, size, bold) <= width) return value;
  const chars = [...value];
  while (chars.length && textWidth(`${chars.join('')}…`, size, bold) > width) chars.pop();
  return chars.length ? `${chars.join('')}…` : '';
}

function pdfString(text) {
  return `(${String(text).replace(/\\/g, '\\\\').replace(/\(/g, '\\(').replace(/\)/g, '\\)').replace(/[^\x20-\x7e]/g, '?')})`;
}

// A WinAnsi string literal, bytes above 126 as octal escapes.
function textLiteral(text) {
  let out = '(';
  for (const code of winAnsi(text)) {
    if (code === 0x28 || code === 0x29 || code === 0x5c) out += `\\${String.fromCharCode(code)}`;
    else if (code < 0x7f) out += String.fromCharCode(code);
    else out += `\\${code.toString(8).padStart(3, '0')}`;
  }
  return `${out})`;
}

// '#rgb', '#rrggbb', '#rrggbbaa', 'rgb(…)', 'rgba(…)' → { r, g, b (0–1), a }; null for 'none'.
export function parseColor(color) {
  if (!color || color === 'none' || color === 'transparent') return null;
  const value = String(color).trim();
  let m = value.match(/^#([0-9a-f]{3,8})$/i);
  if (m) {
    let hex = m[1];
    if (hex.length === 3 || hex.length === 4) hex = [...hex].map((c) => c + c).join('');
    const n = (i) => Number.parseInt(hex.slice(i, i + 2), 16) / 255;
    return { r: n(0), g: n(2), b: n(4), a: hex.length === 8 ? n(6) : 1 };
  }
  m = value.match(/^rgba?\(([^)]+)\)$/i);
  if (m) {
    const parts = m[1].split(/[\s,/]+/).filter(Boolean).map(Number.parseFloat);
    return { r: parts[0] / 255, g: parts[1] / 255, b: parts[2] / 255, a: parts.length > 3 ? parts[3] : 1 };
  }
  return { r: 0, g: 0, b: 0, a: 1 };
}

const num = (v) => {
  const r = Math.round(v * 1000) / 1000;
  return Object.is(r, -0) ? '0' : String(r);
};

// A page drawn in CSS pixels (x right, y down from the top left).
export class PDFPage {
  constructor(width, height, { scale = 0.75 } = {}) {
    this.width = width;
    this.height = height;
    this.scale = scale;
    this.ops = [];
    this.images = [];
    this.alphas = new Map();
    this.path = [];
  }

  get points() {
    return { width: this.width * this.scale, height: this.height * this.scale };
  }

  X(x) {
    return num(x * this.scale);
  }

  Y(y) {
    return num((this.height - y) * this.scale);
  }

  alpha(fill, stroke) {
    const key = `${num(fill)}/${num(stroke)}`;
    if (!this.alphas.has(key)) this.alphas.set(key, { name: `GS${this.alphas.size}`, fill, stroke });
    this.ops.push(`/${this.alphas.get(key).name} gs`);
  }

  setFill(color) {
    const c = parseColor(color);
    if (!c) return false;
    this.ops.push(`${num(c.r)} ${num(c.g)} ${num(c.b)} rg`);
    return c;
  }

  setStroke(color, width = 1, { dash = null, cap = 0, join = 0 } = {}) {
    const c = parseColor(color);
    if (!c) return false;
    this.ops.push(`${num(c.r)} ${num(c.g)} ${num(c.b)} RG ${num(width * this.scale)} w ${cap} J ${join} j ${dash ? `[${dash.map((d) => num(d * this.scale)).join(' ')}] 0 d` : '[] 0 d'}`);
    return c;
  }

  save() {
    this.ops.push('q');
  }

  restore() {
    this.ops.push('Q');
  }

  // Paths: moveTo, lineTo, rect and closePath collect; fill, stroke or both draw and clear.
  moveTo(x, y) {
    this.path.push(`${this.X(x)} ${this.Y(y)} m`);
  }

  lineTo(x, y) {
    this.path.push(`${this.X(x)} ${this.Y(y)} l`);
  }

  rect(x, y, w, h) {
    this.path.push(`${this.X(x)} ${this.Y(y + h)} ${num(w * this.scale)} ${num(h * this.scale)} re`);
  }

  curveTo(x1, y1, x2, y2, x, y) {
    this.path.push(`${this.X(x1)} ${this.Y(y1)} ${this.X(x2)} ${this.Y(y2)} ${this.X(x)} ${this.Y(y)} c`);
  }

  closePath() {
    this.path.push('h');
  }

  // A rectangle with rounded corners (Bézier quarter circles).
  roundRect(x, y, w, h, radius) {
    const r = Math.min(radius, w / 2, h / 2);
    const k = 0.5523 * r;
    this.moveTo(x + r, y);
    this.lineTo(x + w - r, y);
    this.curveTo(x + w - r + k, y, x + w, y + r - k, x + w, y + r);
    this.lineTo(x + w, y + h - r);
    this.curveTo(x + w, y + h - r + k, x + w - r + k, y + h, x + w - r, y + h);
    this.lineTo(x + r, y + h);
    this.curveTo(x + r - k, y + h, x, y + h - r + k, x, y + h - r);
    this.lineTo(x, y + r);
    this.curveTo(x, y + r - k, x + r - k, y, x + r, y);
    this.closePath();
  }

  draw({ fill = null, stroke = null, width = 1, dash = null, cap = 0, join = 0 } = {}) {
    if (!this.path.length) return;
    const f = fill ? parseColor(fill) : null;
    const s = stroke ? parseColor(stroke) : null;
    if (!f && !s) {
      this.path = [];
      return;
    }
    const translucent = (f && f.a < 1) || (s && s.a < 1);
    if (translucent) {
      this.save();
      this.alpha(f?.a ?? 1, s?.a ?? 1);
    }
    if (f) this.setFill(fill);
    if (s) this.setStroke(stroke, width, { dash, cap, join });
    this.ops.push(this.path.join(' '), f && s ? 'B' : f ? 'f' : 'S');
    if (translucent) this.restore();
    this.path = [];
  }

  fillRect(x, y, w, h, color) {
    this.rect(x, y, w, h);
    this.draw({ fill: color });
  }

  strokeRect(x, y, w, h, color, width = 1) {
    this.rect(x, y, w, h);
    this.draw({ stroke: color, width });
  }

  line(x0, y0, x1, y1, color, width = 1, options = {}) {
    this.moveTo(x0, y0);
    this.lineTo(x1, y1);
    this.draw({ stroke: color, width, ...options });
  }

  // Clips what follows (until restore) to a rectangle.
  clipRect(x, y, w, h) {
    this.ops.push(`${this.X(x)} ${this.Y(y + h)} ${num(w * this.scale)} ${num(h * this.scale)} re W n`);
  }

  // Text at (x, y): align left, center or right of x; baseline top, middle, bottom or
  // alphabetic (y is the baseline); rotate in degrees counterclockwise; halo: { color, width }
  // strokes the glyphs' outline under the fill (as gate labels are drawn over events).
  text(value, x, y, { size = 12, bold = false, color = '#000000', align = 'left', baseline = 'alphabetic', rotate = 0, halo = null } = {}) {
    const text = String(value ?? '');
    if (!text) return;
    // Superscript digits (axis ticks such as 10⁴) are set smaller and raised.
    const runs = superscriptRuns(text);
    const width = runs.reduce((sum, r) => sum + textWidth(r.text, r.raised ? size * 0.7 : size, bold), 0);
    const dx = align === 'center' ? -width / 2 : align === 'right' ? -width : 0;
    const dy = baseline === 'top' ? 0.8 * size : baseline === 'middle' ? 0.33 * size : baseline === 'bottom' ? -0.2 * size : 0;
    // Text turns by quarter turns only, given exactly, so that a page is the same bytes in every
    // JavaScript engine (Math.cos and Math.sin may differ in their last bit; determinism.test.mjs).
    const turns = Math.round(rotate / 90);
    if (Math.abs(rotate - turns * 90) > 1e-9) throw new Error(`Text turns by quarter turns only, not ${rotate}°.`);
    const [cos, sin] = [[1, 0], [0, 1], [-1, 0], [0, -1]][((turns % 4) + 4) % 4];
    // The offset (dx along the text, dy down the page) turned with the text.
    const px = x + dx * cos + dy * sin;
    const py = y - dx * sin + dy * cos;
    const font = bold ? '/F2' : '/F1';
    const matrix = `${num(cos)} ${num(sin)} ${num(-sin)} ${num(cos)} ${this.X(px)} ${this.Y(py)} Tm`;
    const shown = runs.map((r) => `${font} ${num((r.raised ? size * 0.7 : size) * this.scale)} Tf ${r.raised ? num(size * 0.38 * this.scale) : 0} Ts ${textLiteral(r.text)} Tj`).join(' ');
    const c = parseColor(color) ?? { r: 0, g: 0, b: 0, a: 1 };
    if (halo) {
      this.save();
      this.setStroke(halo.color ?? '#ffffff', halo.width ?? 3, { join: 1 });
      this.ops.push(`BT 1 Tr ${matrix} ${shown} ET`);
      this.restore();
    }
    if (c.a < 1) {
      this.save();
      this.alpha(c.a, 1);
    }
    this.ops.push(`${num(c.r)} ${num(c.g)} ${num(c.b)} rg BT 0 Tr ${matrix} ${shown} ET`);
    if (c.a < 1) this.restore();
  }

  // An image ({ width, height, rgb, alpha? } or { width, height, rgba }) stretched over a box;
  // interpolate false keeps pixels sharp (event rasters).
  image(image, x, y, w, h, { interpolate = false } = {}) {
    let { rgb, alpha } = image;
    if (image.rgba) {
      const n = image.width * image.height;
      rgb = new Uint8Array(n * 3);
      alpha = new Uint8Array(n);
      let opaque = true;
      for (let i = 0; i < n; i += 1) {
        rgb[i * 3] = image.rgba[i * 4];
        rgb[i * 3 + 1] = image.rgba[i * 4 + 1];
        rgb[i * 3 + 2] = image.rgba[i * 4 + 2];
        alpha[i] = image.rgba[i * 4 + 3];
        if (alpha[i] !== 255) opaque = false;
      }
      if (opaque) alpha = null;
    }
    const name = `Im${this.images.length}`;
    this.images.push({ name, width: image.width, height: image.height, rgb, alpha: alpha ?? null, interpolate });
    this.ops.push(`q ${num(w * this.scale)} 0 0 ${num(h * this.scale)} ${this.X(x)} ${this.Y(y + h)} cm /${name} Do Q`);
  }
}

// zlib-wrapped deflate, as FlateDecode expects.
export async function zlibDeflate(bytes) {
  if (typeof CompressionStream === 'undefined') throw new Error('Compression is not available in this browser.');
  const stream = new Blob([bytes]).stream().pipeThrough(new CompressionStream('deflate'));
  return new Uint8Array(await new Response(stream).arrayBuffer());
}

// RGBA (as from canvas getImageData) to packed RGB on white (PDF images here have no alpha).
export function rgbaToRgb(rgba) {
  const n = rgba.length / 4;
  const out = new Uint8Array(n * 3);
  for (let i = 0; i < n; i += 1) {
    const a = rgba[i * 4 + 3] / 255;
    out[i * 3] = Math.round(rgba[i * 4] * a + 255 * (1 - a));
    out[i * 3 + 1] = Math.round(rgba[i * 4 + 1] * a + 255 * (1 - a));
    out[i * 3 + 2] = Math.round(rgba[i * 4 + 2] * a + 255 * (1 - a));
  }
  return out;
}

function pdfDate(date) {
  const two = (v) => String(v).padStart(2, '0');
  return `D:${date.getUTCFullYear()}${two(date.getUTCMonth() + 1)}${two(date.getUTCDate())}${two(date.getUTCHours())}${two(date.getUTCMinutes())}${two(date.getUTCSeconds())}Z`;
}

// pages: PDFPage objects, or (a page that is one picture) { width, height (points), image:
// { width, height, rgb } }. info: { title, creator, subject, date, attachments }.
export async function writePDF(pages, info = {}) {
  const objects = [];
  const add = (content) => {
    objects.push(content);
    return objects.length;
  };
  const catalogId = 1;
  const pagesId = 2;
  objects.push(null, null); // reserved for catalog and pages
  const usesText = pages.some((p) => p instanceof PDFPage && p.ops.some((op) => op.includes(' Tf')));
  const fonts = usesText ? {
    F1: add(encoder.encode('<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>')),
    F2: add(encoder.encode('<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>')),
  } : null;
  const stream = (dict, data) => concat([encoder.encode(`<< ${dict}/Length ${data.length} >>\nstream\n`), data, encoder.encode('\nendstream')]);
  const pageIds = [];
  for (const source of pages) {
    let page = source;
    if (!(page instanceof PDFPage)) {
      // A picture page in points: drawn as one image filling a page of that size.
      page = new PDFPage(source.width, source.height, { scale: 1 });
      page.image(source.image, 0, 0, source.width, source.height, { interpolate: true });
    }
    const xobjects = [];
    for (const image of page.images) {
      let smask = '';
      if (image.alpha) {
        const maskData = await zlibDeflate(image.alpha);
        const maskId = add(stream(`/Type /XObject /Subtype /Image /Width ${image.width} /Height ${image.height} /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /FlateDecode `, maskData));
        smask = `/SMask ${maskId} 0 R `;
      }
      const data = await zlibDeflate(image.rgb);
      const imageId = add(stream(`/Type /XObject /Subtype /Image /Width ${image.width} /Height ${image.height} /ColorSpace /DeviceRGB /BitsPerComponent 8 ${image.interpolate ? '' : '/Interpolate false '}${smask}/Filter /FlateDecode `, data));
      xobjects.push(`/${image.name} ${imageId} 0 R`);
    }
    const content = await zlibDeflate(encoder.encode(page.ops.join('\n')));
    const contentId = add(stream('/Filter /FlateDecode ', content));
    const states = [...page.alphas.values()].map((s) => `/${s.name} << /Type /ExtGState /ca ${num(s.fill)} /CA ${num(s.stroke)} >>`);
    const resources = [
      fonts && page.ops.some((op) => op.includes(' Tf')) ? `/Font << /F1 ${fonts.F1} 0 R /F2 ${fonts.F2} 0 R >>` : '',
      xobjects.length ? `/XObject << ${xobjects.join(' ')} >>` : '',
      states.length ? `/ExtGState << ${states.join(' ')} >>` : '',
    ].filter(Boolean).join(' ');
    const { width, height } = page.points;
    pageIds.push(add(encoder.encode(`<< /Type /Page /Parent ${pagesId} 0 R /MediaBox [0 0 ${num(width)} ${num(height)}] /Resources << ${resources} >> /Contents ${contentId} 0 R >>`)));
  }
  const specs = [];
  for (const attachment of info.attachments ?? []) {
    const mime = String(attachment.mime ?? 'application/octet-stream').replace(/\//g, '#2F');
    const fileId = add(concat([encoder.encode(`<< /Type /EmbeddedFile /Subtype /${mime} /Length ${attachment.data.length} /Params << /Size ${attachment.data.length} >> >>\nstream\n`), attachment.data, encoder.encode('\nendstream')]));
    const specId = add(encoder.encode(`<< /Type /Filespec /F ${pdfString(attachment.name)} /UF ${pdfString(attachment.name)} /Desc ${pdfString(attachment.description ?? attachment.name)} /AFRelationship /Source /EF << /F ${fileId} 0 R /UF ${fileId} 0 R >> >>`));
    specs.push({ name: attachment.name, id: specId });
  }
  const names = specs.length ? ` /Names << /EmbeddedFiles << /Names [${specs.map((s) => `${pdfString(s.name)} ${s.id} 0 R`).join(' ')}] >> >> /AF [${specs.map((s) => `${s.id} 0 R`).join(' ')}]` : '';
  objects[catalogId - 1] = encoder.encode(`<< /Type /Catalog /Pages ${pagesId} 0 R${names} >>`);
  objects[pagesId - 1] = encoder.encode(`<< /Type /Pages /Kids [${pageIds.map((id) => `${id} 0 R`).join(' ')}] /Count ${pageIds.length} >>`);
  const subject = info.subject ? ` /Subject ${pdfString(info.subject)}` : '';
  if (!(info.date instanceof Date) || Number.isNaN(info.date.getTime())) throw new Error('writePDF needs the creation date (info.date), so that the same figure is the same bytes.');
  const infoId = add(encoder.encode(`<< /Title ${pdfString(info.title ?? 'SituCraft figure')}${subject} /Creator ${pdfString(info.creator ?? 'SituCraft')} /Producer ${pdfString('SituCraft')} /CreationDate ${pdfString(pdfDate(info.date))} >>`));

  const parts = [encoder.encode('%PDF-1.4\n%\xe2\xe3\xcf\xd3\n')];
  let offset = parts[0].length;
  const offsets = [];
  objects.forEach((body, i) => {
    offsets.push(offset);
    const chunk = concat([encoder.encode(`${i + 1} 0 obj\n`), body, encoder.encode('\nendobj\n')]);
    parts.push(chunk);
    offset += chunk.length;
  });
  const xrefOffset = offset;
  let xref = `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  for (const o of offsets) xref += `${String(o).padStart(10, '0')} 00000 n \n`;
  xref += `trailer\n<< /Size ${objects.length + 1} /Root ${catalogId} 0 R /Info ${infoId} 0 R >>\nstartxref\n${xrefOffset}\n%%EOF\n`;
  parts.push(encoder.encode(xref));
  return concat(parts);
}

function concat(chunks) {
  const total = chunks.reduce((sum, c) => sum + c.length, 0);
  const out = new Uint8Array(total);
  let at = 0;
  for (const c of chunks) {
    out.set(c, at);
    at += c.length;
  }
  return out;
}
