// What a written figure carries (wave 8, slice 1; requirement E6): its manifest. The figure, and
// panel by panel what it shows, what it was drawn from (a tissue panel: the dataset by its bundle's
// run and fingerprint, the saved view as it was, the artifacts drawn by id) and a digest of what it
// drew; the project, SituCraft's version and commit, when; and a fingerprint, the SHA-256 of all
// that, so that a manifest changed after it was written is caught. It is embedded as SVG
// <metadata>, a PNG iTXt chunk or a PDF attachment. A figure read back is compared with the
// project, panel by panel. After MaveScape 0.8.0's web/lib/figure-manifest.js (Apache-2.0, the
// same author): its embedding and reading; the record is SituCraft's.

import { sha256 } from './sha256.js';
import { marksToSVGSync } from './draw.js';
import { pngChunk } from './png.js';
import { panelLetters, panelSummary } from './figures.js';

export const MANIFEST_FORMAT = 'situcraft-figure-manifest';
export const MANIFEST_VERSION = 1;
export const PNG_KEYWORD = 'situcraft-figure';
export const PDF_ATTACHMENT = 'situcraft-figure.json';
const SVG_ID = 'situcraft-figure';
const encoder = new TextEncoder();
const decoder = new TextDecoder();

// JSON with every object's keys sorted: the same value, the same text.
export function canonicalJSON(value) {
  if (Array.isArray(value)) return `[${value.map((v) => canonicalJSON(v === undefined ? null : v)).join(',')}]`;
  if (value && typeof value === 'object') {
    const keys = Object.keys(value).filter((k) => value[k] !== undefined).sort();
    return `{${keys.map((k) => `${JSON.stringify(k)}:${canonicalJSON(value[k])}`).join(',')}}`;
  }
  return JSON.stringify(value ?? null);
}

export const digest = (value) => sha256(encoder.encode(canonicalJSON(value)));

// --- What a panel was drawn from -----------------------------------------------------------------

// A dataset by its bundle: its run and Xenium identifiers, and a digest of its fingerprint (each
// file's path and size, the small ones' SHA-256).
function datasetRef(project, id) {
  const d = project.datasets.find((x) => x.id === id);
  if (!d) return { id, missing: true };
  return { id: d.id, name: d.name, run: d.run ?? null, experimentUuid: d.experimentUuid ?? null, fingerprint: digest(d.fingerprint ?? null) };
}

function artifactRef(project, id, kind) {
  const a = project.artifacts.find((x) => x.id === id);
  if (!a) return { id, kind, missing: true };
  return { id: a.id, kind: a.kind, name: a.name, ...(a.files ? { files: digest(a.files.map((f) => [f.path, f.sha256])) } : {}) };
}

// The artifacts a view draws: its segmentation (none for the instrument's) and its cells' fill
// (a label set, a clustering, or none for an attribute of the cells).
export function viewArtifacts(view) {
  const out = [];
  if (view.segmentation && view.segmentation !== 'instrument') out.push({ id: view.segmentation, kind: 'segmentation' });
  const fill = view.cells?.fill ?? '';
  const labels = /^labels:([^:]+)/.exec(fill);
  if (labels) out.push({ id: labels[1], kind: 'labels' });
  if (fill.startsWith('clusters:')) out.push({ id: fill.slice('clusters:'.length), kind: 'clustering' });
  return out;
}

// The inputs a panel is drawn from, as the project holds them now.
export function panelInputs(project, panel) {
  if (panel.kind !== 'tissue') return {};
  const view = (project.views ?? []).find((v) => v.id === panel.view);
  if (!view) return { view: { id: panel.view, missing: true } };
  return {
    dataset: datasetRef(project, view.dataset),
    view: { id: view.id, name: view.name, made: digest(view) },
    region: panel.region,
    artifacts: viewArtifacts(view).map((a) => artifactRef(project, a.id, a.kind)),
  };
}

// --- The manifest --------------------------------------------------------------------------------

// Marks as the digest reads them: each image stood in for by a rectangle naming its size and the
// SHA-256 of its pixels, so that a drawing's digest is the same whatever compresses its PNG.
function digestable(marks) {
  return marks.map((m) => {
    if (m.type === 'image') return { type: 'rect', x: m.x, y: m.y, w: m.w, h: m.h, fill: 'none', title: `image ${m.image.width} × ${m.image.height} sha256:${sha256(m.image.rgba)}${m.smooth ? ' smooth' : ''}` };
    if (m.type === 'group') return { ...m, marks: digestable(m.marks) };
    return m;
  });
}

// Each panel's drawing as a digest: the SHA-256 of its marks written as SVG (rounded as the file
// is), so that two drawings of a panel compare whatever the format. drawn: figureMarks's return
// (its marks hold the page's background, then a group per panel in the figure's order).
export function panelDigests(fig, drawn) {
  const out = new Map();
  fig.panels.forEach((p, i) => {
    const group = drawn.marks[i + 1];
    out.set(p.id, sha256(encoder.encode(marksToSVGSync(digestable([group]), 1, 1, { sized: false }))));
  });
  return out;
}

// The manifest of a figure drawn from a project. context: { software: { version, commit },
// created }.
export function buildManifest(project, fig, drawn, context = {}) {
  const digests = panelDigests(fig, drawn);
  const letters = fig.letters ? panelLetters(fig) : new Map();
  const record = {
    format: MANIFEST_FORMAT,
    version: MANIFEST_VERSION,
    created: context.created ?? null,
    software: { name: 'SituCraft', version: context.software?.version ?? null, commit: context.software?.commit || null },
    project: { id: project.id, name: project.name },
    figure: fig,
    description: drawn.description,
    panels: fig.panels.map((p, i) => ({ id: p.id, letter: letters.get(p.id) ?? null, kind: p.kind, shows: panelSummary(p, project), drawing: digests.get(p.id), inputs: panelInputs(project, p), ...(drawn.panels[i]?.facts ? { facts: factsOf(drawn.panels[i].facts) } : {}) })),
  };
  record.drawing = digest(record.panels.map((p) => p.drawing));
  return { ...record, fingerprint: digest(record) };
}

// What a tissue panel drew, as the manifest keeps it: the region (µm) and µm a point as computed
// (exactly: they are the same numbers on every computer), the scale bar, the cells and transcripts
// drawn and the stains' pixels and level.
function factsOf(f) {
  return { region: f.region, umPerPt: f.umPerPt, scaleBar: f.scaleBar, cells: f.cells, transcripts: f.transcripts, raster: f.raster };
}

// Whether a manifest is as it was written: { ok, problem? }.
export function checkManifest(record) {
  if (!record || record.format !== MANIFEST_FORMAT) return { ok: false, problem: 'It is not a SituCraft figure manifest.' };
  if (record.version > MANIFEST_VERSION) return { ok: false, problem: `It was written by a newer SituCraft (manifest version ${record.version}).` };
  const { fingerprint, ...rest } = record;
  if (fingerprint !== digest(rest)) return { ok: false, problem: 'The manifest was changed after it was written: its fingerprint does not follow from what it holds.' };
  return { ok: true };
}

// --- Embedded in the files -----------------------------------------------------------------------

const PNG_SIGNATURE = [137, 80, 78, 71, 13, 10, 26, 10];
const u32 = (b, at) => ((b[at] << 24) | (b[at + 1] << 16) | (b[at + 2] << 8) | b[at + 3]) >>> 0;

function parse(json) {
  try {
    const record = JSON.parse(json);
    return record?.format === MANIFEST_FORMAT ? record : null;
  } catch {
    return null;
  }
}

// SVG: a <metadata> element first in the document, the JSON in CDATA.
export function embedSVG(svg, record) {
  const json = JSON.stringify(record).replace(/]]>/g, ']]]]><![CDATA[>');
  const open = svg.indexOf('<svg');
  const end = open < 0 ? -1 : svg.indexOf('>', open);
  if (end < 0) throw new Error('Not an SVG document.');
  return `${svg.slice(0, end + 1)}\n<metadata id="${SVG_ID}"><![CDATA[${json}]]></metadata>${svg.slice(end + 1)}`;
}

export function readSVG(text) {
  const match = new RegExp(`<metadata[^>]*id="${SVG_ID}"[^>]*>([\\s\\S]*?)</metadata>`).exec(text);
  return match ? parse(match[1].replace(/<!\[CDATA\[|\]\]>/g, '')) : null;
}

// The chunks of a PNG with one inserted before the first of a type ('IEND', 'IDAT').
function insertChunk(bytes, chunk, before) {
  let at = 8;
  while (at + 8 <= bytes.length) {
    if (String.fromCharCode(...bytes.subarray(at + 4, at + 8)) === before) break;
    at += 12 + u32(bytes, at);
  }
  if (at + 8 > bytes.length) throw new Error(`The PNG image has no ${before} chunk.`);
  const out = new Uint8Array(bytes.length + chunk.length);
  out.set(bytes.subarray(0, at), 0);
  out.set(chunk, at);
  out.set(bytes.subarray(at), at + chunk.length);
  return out;
}

// PNG: an uncompressed iTXt chunk (keyword situcraft-figure, UTF-8 JSON) before IEND.
export function embedPNG(bytes, record) {
  if (!PNG_SIGNATURE.every((b, i) => bytes[i] === b)) throw new Error('Not a PNG image.');
  const keyword = encoder.encode(PNG_KEYWORD);
  const text = encoder.encode(JSON.stringify(record));
  const data = new Uint8Array(keyword.length + 5 + text.length);
  data.set(keyword, 0);
  data.set(text, keyword.length + 5);
  return insertChunk(bytes, pngChunk('iTXt', data), 'IEND');
}

// PNG: its printed resolution (a pHYs chunk, pixels a metre) before its first image data, so that
// a 300 dpi figure prints at its size.
export function withResolution(bytes, dpi) {
  const perMetre = Math.round(dpi / 0.0254);
  const data = new Uint8Array(9);
  const view = new DataView(data.buffer);
  view.setUint32(0, perMetre);
  view.setUint32(4, perMetre);
  data[8] = 1;
  return insertChunk(bytes, pngChunk('pHYs', data), 'IDAT');
}

export function readPNG(bytes) {
  if (!PNG_SIGNATURE.every((b, i) => bytes[i] === b)) return null;
  let at = 8;
  while (at + 8 <= bytes.length) {
    const length = u32(bytes, at);
    const type = String.fromCharCode(...bytes.subarray(at + 4, at + 8));
    if (type === 'iTXt') {
      const data = bytes.subarray(at + 8, at + 8 + length);
      const nul = data.indexOf(0);
      if (decoder.decode(data.subarray(0, nul)) === PNG_KEYWORD && data[nul + 1] === 0) {
        let p = nul + 3;
        p = data.indexOf(0, p) + 1;
        p = data.indexOf(0, p) + 1;
        return parse(decoder.decode(data.subarray(p)));
      }
    }
    if (type === 'IEND') break;
    at += 12 + length;
  }
  return null;
}

// PDF: an attachment (lib/pdf.js writes it uncompressed).
export function pdfAttachment(record) {
  return { name: PDF_ATTACHMENT, mime: 'application/json', description: 'What this SituCraft figure shows and was drawn from (its manifest)', data: encoder.encode(JSON.stringify(record)) };
}

export function readPDF(bytes) {
  if (String.fromCharCode(...bytes.subarray(0, 5)) !== '%PDF-') return null;
  const text = new TextDecoder('latin1').decode(bytes);
  const re = /\/Type\s*\/EmbeddedFile\b[\s\S]{0,400}?\/Length\s+(\d+)[\s\S]{0,400}?>>\s*stream\r?\n/g;
  let match;
  while ((match = re.exec(text))) {
    const start = match.index + match[0].length;
    const record = parse(decoder.decode(bytes.subarray(start, start + Number(match[1]))));
    if (record) return record;
  }
  return null;
}

// The manifest of a written figure (SVG text, or SVG, PNG, PDF or JSON bytes), or null.
export function readFigureManifest(input) {
  if (typeof input === 'string') return readSVG(input) ?? parse(input);
  const bytes = input instanceof Uint8Array ? input : new Uint8Array(input);
  return readPNG(bytes) ?? readPDF(bytes) ?? (() => {
    const text = decoder.decode(bytes.subarray(0, Math.min(bytes.length, 64 * 1024 * 1024)));
    return readSVG(text) ?? parse(text);
  })();
}

// --- Compared with a project ---------------------------------------------------------------------

// What changed since a figure was written, panel by panel, in words: { intact, problem, panels:
// [{ id, letter, shows, status: 'unchanged' | 'changed' | 'missing', changes: [text] }] }. Whether
// it draws the same is answered by drawing it again (panelDigests).
export function compareManifest(record, project) {
  const check = checkManifest(record);
  const panels = record.panels.map((p) => {
    if (p.kind !== 'tissue') return { id: p.id, letter: p.letter, shows: p.shows, status: 'unchanged', changes: [] };
    const now = panelInputs(project, { kind: 'tissue', view: p.inputs?.view?.id, region: p.inputs?.region });
    const changes = [];
    let missing = false;
    if (!now.view || now.view.missing) {
      missing = true;
      changes.push(`the saved view "${p.inputs?.view?.name ?? p.inputs?.view?.id}" is not in this project`);
    } else {
      if (now.view.made !== p.inputs.view.made) changes.push(`the saved view "${now.view.name}" was saved again since`);
      if (now.dataset.missing) {
        missing = true;
        changes.push('its dataset is not in this project');
      } else if (now.dataset.fingerprint !== p.inputs.dataset.fingerprint) changes.push(`${now.dataset.name}'s bundle is not the one it was drawn from`);
      for (const a of p.inputs.artifacts ?? []) {
        const found = now.artifacts.find((x) => x.id === a.id);
        if (!found || found.missing) {
          missing = true;
          changes.push(`the ${a.kind} ${a.name ?? a.id} is not in this project`);
        } else if (found.files !== a.files) changes.push(`the ${a.kind} ${a.name ?? a.id} has other files`);
      }
    }
    return { id: p.id, letter: p.letter, shows: p.shows, status: missing ? 'missing' : changes.length ? 'changed' : 'unchanged', changes };
  });
  return { intact: check.ok, problem: check.problem ?? null, sameProject: record.project?.id === project.id, panels };
}

