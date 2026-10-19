// Figures (wave 8, slice 1; requirement E6): a figure is a page of panels kept in the project
// (project.figures). Each panel says what it shows (a tissue panel: the saved view and the place
// of the tissue in µm), never a copy of the data, so a figure is drawn again from the project
// whenever it is shown or written. Sizes are in points (1/72 inch); pages are a journal's column
// widths or a slide. The page is drawn as marks (lib/draw.js), written as SVG or PDF here, and
// painted by the Reports view, or as PNG from that painting.
// After MaveScape 0.8.0's web/lib/figures.js (Apache-2.0, the same author); the panels are
// SituCraft's.

import { marksToSVG, pdfPage, textMarks, textWidth } from './draw.js';
import { writePDF } from './pdf.js';
import { tissuePanel, MIN_TEXT } from './figure-tissue.js';
import { chartPanel } from './charts.js';

export const mm = (v) => (v * 72) / 25.4;
export const toMM = (pt) => Math.round(((pt * 25.4) / 72) * 10) / 10;
const round = (v) => Math.round(v * 100) / 100;

// Page sizes: the widths most journals set figures at (one, one and a half and two columns), and a
// 16:9 slide. Heights are where a page starts; any can be changed.
export const FIGURE_SIZES = [
  { id: 'single', label: 'One column (85 mm)', width: mm(85), height: mm(85) },
  { id: 'onehalf', label: 'One and a half columns (114 mm)', width: mm(114), height: mm(100) },
  { id: 'double', label: 'Two columns (174 mm)', width: mm(174), height: mm(120) },
  { id: 'slide', label: 'Slide (16:9)', width: 960, height: 540 },
];

export const PANEL_KINDS = {
  tissue: 'The tissue (a saved view)',
  qc: 'A QC distribution',
  map: 'The cells\' map',
  spatial: 'A spatial answer',
  cohort: 'A cohort\'s comparison',
  text: 'Text',
};

// What a cohort panel can draw of each kind of comparison.
export const COHORT_PLOTS = { forest: 'The effects with their intervals', units: 'A label\'s or statistic\'s units by group', volcano: 'A label\'s genes (a volcano)', gene: 'A gene by group' };

const LETTERS = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
const INK = '#171b26';
const MUTED = '#4b5468';

export function figureSize(id) {
  return FIGURE_SIZES.find((s) => s.id === id) ?? null;
}

function newId(prefix) {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`;
  return `${prefix}-${random.replace(/-/g, '').slice(0, 16)}`;
}

// A new figure: { id, name, size, width, height (pt), text (pt), letters, panels, created }.
export function createFigure(name, sizeId = 'double', options = {}) {
  const size = figureSize(sizeId) ?? FIGURE_SIZES[2];
  return {
    id: options.id ?? newId('fig'),
    name: String(name || 'Figure').trim(),
    size: size.id,
    width: round(options.width ?? size.width),
    height: round(options.height ?? size.height),
    text: options.text ?? (size.id === 'slide' ? 14 : 7),
    letters: options.letters !== false,
    panels: [],
    created: options.now ?? new Date().toISOString(),
  };
}

// A panel's own fields, checked: a figure from remote control or a project file is drawn only when
// every field is what a figure holds. Throws with what is wrong.
function checkPanel(p, fig) {
  if (!p || typeof p !== 'object') throw new Error('A panel is not an object.');
  if (typeof p.id !== 'string' || !p.id) throw new Error('A panel has no id.');
  if (!PANEL_KINDS[p.kind]) throw new Error(`Panel ${p.id}: "${p.kind}" is not a kind of panel (${Object.keys(PANEL_KINDS).join(', ')}).`);
  for (const k of ['x', 'y', 'w', 'h']) if (!Number.isFinite(p[k])) throw new Error(`Panel ${p.id}: ${k} is not a number.`);
  if (p.w < 20 || p.h < 20) throw new Error(`Panel ${p.id} is smaller than 20 pt.`);
  if (p.x < -1 || p.y < -1 || p.x + p.w > fig.width + 1 || p.y + p.h > fig.height + 1) throw new Error(`Panel ${p.id} is not on the page (${fig.width} × ${fig.height} pt).`);
  if (p.kind === 'text' && typeof p.text !== 'string') throw new Error(`Panel ${p.id}: a text panel has its text.`);
  if (p.kind === 'tissue') {
    if (typeof p.view !== 'string' || !p.view) throw new Error(`Panel ${p.id}: a tissue panel names its saved view.`);
    const r = p.region;
    if (!r || !Number.isFinite(r.cx) || !Number.isFinite(r.cy) || !(r.width > 0)) throw new Error(`Panel ${p.id}: a tissue panel's region is its center and width in µm.`);
    if (p.scaleBar !== undefined && p.scaleBar !== null && !(p.scaleBar > 0)) throw new Error(`Panel ${p.id}: the scale bar's length is in µm, above 0.`);
    if (p.pointSize !== undefined && !(p.pointSize > 0 && p.pointSize <= 12)) throw new Error(`Panel ${p.id}: the transcripts' points are 0 to 12 pt across.`);
    if (p.regions !== undefined && p.regions !== true && p.regions !== 'all' && !(Array.isArray(p.regions) && p.regions.every((r) => typeof r === 'string'))) throw new Error(`Panel ${p.id}: regions is true (all of them) or a list of their ids.`);
  }
  const named = (field, what) => {
    if (typeof p[field] !== 'string' || !p[field]) throw new Error(`Panel ${p.id}: a ${PANEL_KINDS[p.kind].toLowerCase()} panel names its ${what}.`);
  };
  if (p.kind === 'qc') {
    named('dataset', 'dataset');
    named('metric', 'metric');
  }
  if (p.kind === 'map') named('map', 'map');
  if (p.kind === 'spatial') named('analysis', 'analysis');
  if (p.kind === 'cohort') {
    named('comparison', 'comparison');
    if (!COHORT_PLOTS[p.plot]) throw new Error(`Panel ${p.id}: a cohort panel draws ${Object.keys(COHORT_PLOTS).join(', ')}, not "${p.plot}".`);
    if (p.plot === 'gene') named('gene', 'gene');
  }
}

export function checkFigure(fig) {
  if (!fig || typeof fig !== 'object') throw new Error('A figure is not an object.');
  if (typeof fig.id !== 'string' || !fig.id) throw new Error('A figure has no id.');
  if (typeof fig.name !== 'string') throw new Error(`Figure ${fig.id} has no name.`);
  if (!(fig.width >= 72 && fig.width <= 2000 && fig.height >= 72 && fig.height <= 2000)) throw new Error(`Figure "${fig.name}": its page is 1 to 70 cm a side.`);
  if (!(fig.text >= MIN_TEXT && fig.text <= 36)) throw new Error(`Figure "${fig.name}": its text is ${MIN_TEXT} to 36 pt.`);
  if (!Array.isArray(fig.panels)) throw new Error(`Figure "${fig.name}" has no panels list.`);
  const ids = new Set();
  for (const p of fig.panels) {
    checkPanel(p, fig);
    if (ids.has(p.id)) throw new Error(`Figure "${fig.name}" has two panels ${p.id}.`);
    ids.add(p.id);
  }
  return fig;
}

// --- The project's figures -----------------------------------------------------------------------

export function figuresOf(project) {
  return project.figures ?? [];
}

// The project with a figure added or replaced (by its id), checked.
export function saveFigure(project, fig) {
  checkFigure(fig);
  const list = figuresOf(project);
  const at = list.findIndex((f) => f.id === fig.id);
  const figures = at < 0 ? [...list, fig] : list.map((f, i) => (i === at ? fig : f));
  return { ...project, figures };
}

export function removeFigure(project, id) {
  return { ...project, figures: figuresOf(project).filter((f) => f.id !== id) };
}

// --- Panels on the page --------------------------------------------------------------------------

// A new panel's id in a figure.
export function panelId(fig) {
  let n = fig.panels.length + 1;
  while (fig.panels.some((p) => p.id === `panel-${n}`)) n += 1;
  return `panel-${n}`;
}

// The figure with a panel added where it fits: beside the last row's panels when there is room
// for its width (columns: the page's width by default), else below them. Returns { figure, panel }.
export function addPanel(fig, panel, { columns = 1 } = {}) {
  const gap = 8;
  const w = round(panel.w ?? (fig.width - gap * (columns + 1)) / columns);
  const h = round(panel.h ?? Math.min(w * 0.75, fig.height - 2 * gap));
  const bottom = fig.panels.reduce((y, p) => Math.max(y, p.y + p.h), 0);
  const last = fig.panels.at(-1);
  let x = panel.x;
  let y = panel.y;
  if (x === undefined || y === undefined) {
    if (last && last.x + last.w + gap + w <= fig.width - gap + 0.5) {
      x = last.x + last.w + gap;
      y = last.y;
    } else {
      x = gap;
      y = bottom ? bottom + gap : gap;
    }
  }
  const placed = { id: panel.id ?? panelId(fig), ...panel, x: round(x), y: round(y), w, h: round(Math.min(h, fig.height - y)) };
  return { figure: { ...fig, panels: [...fig.panels, placed] }, panel: placed };
}

export function updatePanel(fig, id, patch) {
  return { ...fig, panels: fig.panels.map((p) => (p.id === id ? { ...p, ...patch } : p)) };
}

export function removePanel(fig, id) {
  return { ...fig, panels: fig.panels.filter((p) => p.id !== id) };
}

// The panels' letters, A, B, C… in reading order: rows from the top (a panel starting above the
// middle of a row's first panel joins it), each row from the left. Panels with letter: false get
// none. Map(panel id → letter).
export function panelLetters(fig) {
  const lettered = fig.panels.filter((p) => p.letter !== false).slice().sort((a, b) => a.y - b.y || a.x - b.x);
  const rows = [];
  for (const p of lettered) {
    const row = rows.at(-1);
    if (row && p.y < row.top + row.height / 2) row.panels.push(p);
    else rows.push({ top: p.y, height: p.h, panels: [p] });
  }
  const out = new Map();
  let i = 0;
  for (const row of rows) {
    for (const p of row.panels.sort((a, b) => a.x - b.x)) {
      out.set(p.id, LETTERS[i % 26].repeat(Math.floor(i / 26) + 1));
      i += 1;
    }
  }
  return out;
}

// The box a panel draws in: its own, less the space of its letter above it.
export function panelBox(fig, panel) {
  const letter = fig.letters && panel.letter !== false;
  const head = letter ? fig.text + 4 : 0;
  return { x: panel.x, y: panel.y + head, w: panel.w, h: panel.h - head };
}

// --- Drawing --------------------------------------------------------------------------------------

function wrap(text, width, size, bold) {
  const lines = [];
  for (const paragraph of String(text).split('\n')) {
    let line = '';
    for (const word of paragraph.split(/\s+/).filter(Boolean)) {
      const next = line ? `${line} ${word}` : word;
      if (line && textWidth(next, size, bold) > width) {
        lines.push(line);
        line = word;
      } else line = next;
    }
    lines.push(line);
  }
  return lines;
}

function textPanel(box, panel, fig) {
  const size = panel.size ?? fig.text;
  const bold = Boolean(panel.bold);
  const lines = wrap(panel.text, box.w, size, bold);
  const lineH = size * 1.25;
  const marks = [];
  const x = panel.align === 'center' ? box.x + box.w / 2 : panel.align === 'right' ? box.x + box.w : box.x;
  const anchor = panel.align === 'center' ? 'middle' : panel.align === 'right' ? 'end' : 'start';
  lines.forEach((line, i) => {
    if (line && (i + 1) * lineH <= box.h + 0.5) marks.push({ type: 'text', x, y: box.y + i * lineH, text: line, size, bold, fill: INK, anchor, baseline: 'top' });
  });
  const shown = Math.floor((box.h + 0.5) / lineH);
  const notes = lines.length > shown ? [`${lines.length - shown} line${lines.length - shown > 1 ? 's' : ''} of text do not fit: make the panel taller.`] : [];
  return { marks, description: String(panel.text).replace(/\s+/g, ' ').trim(), notes };
}

function missingPanel(box, message, fig) {
  return {
    marks: [
      { type: 'rect', x: box.x + 0.5, y: box.y + 0.5, w: box.w - 1, h: box.h - 1, fill: 'none', stroke: '#c3c9d4', width: 0.75, dash: [3, 3] },
      { type: 'text', x: box.x + box.w / 2, y: box.y + box.h / 2, text: message, size: Math.max(MIN_TEXT, fig.text), fill: MUTED, anchor: 'middle', baseline: 'middle' },
    ],
    description: message,
    notes: [message],
  };
}

// What a panel shows, in a few words (the Reports view's list).
export function panelSummary(panel, project = null) {
  if (panel.kind === 'text') return `text "${panel.text.length > 40 ? `${panel.text.slice(0, 39)}…` : panel.text}"`;
  if (panel.kind === 'tissue') {
    const view = project?.views?.find((v) => v.id === panel.view);
    return `the saved view "${view?.name ?? panel.view}", ${round(panel.region.width)} µm across`;
  }
  const artifact = (id) => project?.artifacts?.find((a) => a.id === id)?.name ?? id;
  if (panel.kind === 'qc') return `the distribution of ${panel.metric} in ${project?.datasets?.find((d) => d.id === panel.dataset)?.name ?? panel.dataset}${panel.log ? ' (log scale)' : ''}`;
  if (panel.kind === 'map') return `the map "${artifact(panel.map)}" colored by ${panel.color ?? 'clusters'}`;
  if (panel.kind === 'spatial') return `the answer "${artifact(panel.analysis)}"`;
  if (panel.kind === 'cohort') return `${COHORT_PLOTS[panel.plot].replace(/^The /, 'the ').replace(/^A /, 'a ')} of "${artifact(panel.comparison)}"${panel.label ? `, ${panel.label}` : ''}${panel.gene ? `, ${panel.gene}` : ''}`;
  return panel.kind;
}

// The page as marks. resolved: Map(panel id → the panel's data: a tissue panel's (figure-tissue.js);
// notes to say beside any; or { missing: message } when it cannot be drawn). Returns { marks,
// panels: [{ id, letter, kind, description, notes, marks }], description, notes, smallest (the
// smallest text, pt) }.
export function figureMarks(fig, resolved) {
  const letters = fig.letters ? panelLetters(fig) : new Map();
  const letterSize = fig.text + 2;
  const marks = [{ type: 'rect', x: 0, y: 0, w: fig.width, h: fig.height, fill: '#ffffff' }];
  const panels = [];
  for (const panel of fig.panels) {
    const letter = letters.get(panel.id) ?? null;
    const box = panelBox(fig, panel);
    const data = resolved?.get(panel.id);
    let drawn;
    if (panel.kind === 'text') drawn = textPanel(box, panel, fig);
    else if (!data || data.missing) drawn = missingPanel(box, data?.missing ?? 'Not drawn yet', fig);
    else if (panel.kind === 'tissue') drawn = tissuePanel(box, data, { ...panel, text: fig.text });
    else if (data.chart) drawn = chartPanel(box, data, { text: fig.text, ...(panel.title !== undefined ? { title: panel.title } : {}) });
    else drawn = missingPanel(box, `SituCraft does not draw "${panel.kind}" panels.`, fig);
    if (data?.notes?.length) drawn = { ...drawn, notes: [...data.notes, ...(drawn.notes ?? [])] };
    const children = [...drawn.marks];
    if (letter) children.unshift({ type: 'text', x: panel.x, y: panel.y, text: letter, size: letterSize, bold: true, fill: INK, baseline: 'top' });
    marks.push({ type: 'group', clip: { x: panel.x, y: panel.y, w: panel.w, h: panel.h }, label: letter ? `Panel ${letter}` : panel.id, marks: children });
    panels.push({ id: panel.id, letter, kind: panel.kind, description: drawn.description, notes: drawn.notes ?? [], marks: children, ...(drawn.facts ? { facts: drawn.facts } : {}) });
  }
  const sizes = textMarks(marks).map((m) => m.size);
  const smallest = sizes.length ? Math.min(...sizes) : null;
  const notes = panels.flatMap((p) => p.notes.map((n) => `${p.letter ? `Panel ${p.letter}` : 'A panel'}: ${n}`));
  return { marks, panels, description: describeFigure(fig, panels), notes, smallest };
}

export function describeFigure(fig, panels) {
  const parts = panels.map((p) => `${p.letter ? `Panel ${p.letter}` : 'A panel'}: ${p.description}`);
  return `${fig.name}, ${toMM(fig.width)} × ${toMM(fig.height)} mm, ${panels.length} panel${panels.length === 1 ? '' : 's'}.${parts.length ? ` ${parts.join(' ')}` : ''}`;
}

// The figure as SVG text (its description in <desc>). options: { metadata }.
export async function figureSVG(fig, resolved, options = {}) {
  const drawn = options.drawn ?? figureMarks(fig, resolved);
  return marksToSVG(drawn.marks, fig.width, fig.height, { title: fig.name, desc: drawn.description, metadata: options.metadata });
}

// The figure as a one-page PDF (vector; text in Helvetica). options: { date (required: the same
// figure, the same bytes), attachments }.
export async function figurePDF(fig, resolved, options = {}) {
  const drawn = options.drawn ?? figureMarks(fig, resolved);
  return writePDF([pdfPage(drawn.marks, fig.width, fig.height)], { title: fig.name, subject: drawn.description, date: options.date, attachments: options.attachments ?? [] });
}

// The pixel size of a figure's PNG at a resolution (dots per inch of its printed size).
export function pngSize(fig, dpi = 300) {
  return { width: Math.round((fig.width / 72) * dpi), height: Math.round((fig.height / 72) * dpi), scale: dpi / 72 };
}
