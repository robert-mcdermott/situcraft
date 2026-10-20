// A figure drawn and written (wave 8, slice 1): its panels' data gathered (ui/figure-data.js),
// drawn as marks (lib/figures.js), and written as SVG, PDF or PNG with its manifest embedded
// (lib/figure-manifest.js); a PNG painted on a canvas at its resolution. A written figure read back
// and compared with the project. Shared by the Reports view and remote control.

import { figureMarks, figurePDF, figureSVG, pngSize } from '../lib/figures.js';
import { buildManifest, compareManifest, embedPNG, embedSVG, panelDigests, pdfAttachment, readFigureManifest, withResolution } from '../lib/figure-manifest.js';
import { paintMarks } from '../lib/draw.js';
import { resolveFigure } from './figure-data.js';

export const FORMATS = { svg: 'image/svg+xml', pdf: 'application/pdf', png: 'image/png', json: 'application/json' };

// A figure's file name: its name, made safe.
export function figureFileName(fig, format) {
  return `${(fig.name || 'figure').replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '') || 'figure'}.${format}`;
}

// The figure drawn from the project: { drawn (figureMarks's), resolved }.
export async function drawFigure(app, fig, { dpi = 300 } = {}) {
  const resolved = await resolveFigure(app, fig, { dpi });
  return { drawn: figureMarks(fig, resolved), resolved };
}

// Each image mark's canvas source, made once.
const sources = new WeakMap();
function imageOf(image) {
  if (!sources.has(image)) {
    const canvas = document.createElement('canvas');
    canvas.width = image.width;
    canvas.height = image.height;
    canvas.getContext('2d').putImageData(new ImageData(new Uint8ClampedArray(image.rgba.buffer, image.rgba.byteOffset, image.rgba.length), image.width, image.height), 0, 0);
    sources.set(image, canvas);
  }
  return sources.get(image);
}

// The figure painted on a canvas at a scale (canvas pixels a point).
export function paintFigure(canvas, fig, drawn, scale) {
  canvas.width = Math.max(1, Math.round(fig.width * scale));
  canvas.height = Math.max(1, Math.round(fig.height * scale));
  const ctx = canvas.getContext('2d');
  ctx.setTransform(scale, 0, 0, scale, 0, 0);
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, fig.width, fig.height);
  paintMarks(ctx, drawn.marks, { imageOf });
  return canvas;
}

// The figure written: { bytes (Uint8Array, or text for SVG and JSON), name, mime, manifest,
// drawn }. options: { dpi (a PNG's, and the stains' resolution), time (the time written, ISO: the
// same figure written at the same time is the same bytes in SVG and PDF) }.
export async function writeFigure(app, fig, format, { dpi = 300, time = null } = {}) {
  if (!FORMATS[format]) throw new Error(`A figure is written as ${Object.keys(FORMATS).join(', ')}, not ${format}.`);
  const created = time ?? new Date().toISOString();
  const { drawn, resolved } = await drawFigure(app, fig, { dpi });
  const manifest = buildManifest(app.store.project, fig, drawn, { software: { version: app.version, commit: app.commit }, created });
  let bytes;
  if (format === 'svg') bytes = embedSVG(await figureSVG(fig, resolved, { drawn }), manifest);
  else if (format === 'pdf') bytes = await figurePDF(fig, resolved, { drawn, date: new Date(created), attachments: [pdfAttachment(manifest)] });
  else if (format === 'json') bytes = `${JSON.stringify(manifest, null, 1)}\n`;
  else {
    const size = pngSize(fig, dpi);
    const canvas = paintFigure(document.createElement('canvas'), fig, drawn, size.scale);
    const blob = await new Promise((resolve) => canvas.toBlob(resolve, 'image/png'));
    bytes = embedPNG(withResolution(new Uint8Array(await blob.arrayBuffer()), dpi), manifest);
  }
  return { bytes, name: figureFileName(fig, format), mime: FORMATS[format], manifest, drawn };
}

// A written figure read back (its bytes or SVG text) and compared with the project: what changed
// since, and whether each panel draws the same now. Returns { manifest, comparison, redrawn:
// [{ id, letter, same }] } or null when it carries no manifest.
export async function readBackFigure(app, input, { dpi = 300 } = {}) {
  const manifest = readFigureManifest(input);
  if (!manifest) return null;
  const comparison = compareManifest(manifest, app.store.project);
  let redrawn = [];
  if (comparison.intact) {
    try {
      const { drawn } = await drawFigure(app, manifest.figure, { dpi: manifest.panels.find((p) => p.facts?.raster)?.facts.raster ? dpiOf(manifest) : dpi });
      const digests = panelDigests(manifest.figure, drawn);
      redrawn = manifest.panels.map((p) => ({ id: p.id, letter: p.letter, same: digests.get(p.id) === p.drawing }));
    } catch (error) {
      redrawn = [{ error: error.message }];
    }
  }
  return { manifest, comparison, redrawn };
}

// The resolution a manifest's stains were drawn at, from its first tissue panel's raster and image.
function dpiOf(manifest) {
  const p = manifest.panels.find((x) => x.facts?.raster);
  const width = p.facts.region.x1 - p.facts.region.x0;
  const pts = width / p.facts.umPerPt;
  return Math.round((p.facts.raster.width / pts) * 72);
}
