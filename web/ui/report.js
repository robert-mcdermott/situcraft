// The report (wave 8, slice 7) and the result package (slice 8) as the page writes them: the
// results chosen (each figure drawn as SVG, carrying its manifest; a table of each other result's
// numbers), their methods and references, and their certificate, in one HTML file that checks
// itself as it opens (lib/report.js); a package adds each saved view the figures show, drawn in
// advance as tiles with a reader of its own (lib/view-reader.js), the certificate beside them, a
// README and a list of its files (lib/package.js). Both leave out what lib/redact.js leaves out,
// from the project before anything is drawn or certified from it, and every text either holds is
// searched for it before it is written. In the Reports view, and by remote control (export, what:
// report or package).

import { h, icon, clear, downloadBlob } from './dom.js';
import { toast } from './overlays.js';
import { prefs } from './storage.js';
import { STEP_KINDS, gatherManifests, plural, recordableResults } from '../lib/record.js';
import { absolutePaths, shortFingerprint } from '../lib/certificate.js';
import { reportHTML, reportNumbers } from '../lib/report.js';
import { findLeftOut, leftOutStrings, redact, redactionOf } from '../lib/redact.js';
import { PACKAGE_DATE, packageFolder, packageManifest, packageReadme, packageTexts, safeName } from '../lib/package.js';
import { TILE, readerHTML, tilePlan, tilesOf, viewExtent } from '../lib/view-reader.js';
import { panelBox } from '../lib/figures.js';
import { sha256 } from '../lib/sha256.js';
import { createZip } from '../lib/zip.js';
import { DRAWN_AT, certify } from './certificate.js';
import { paintFigure, writeFigure } from './figures.js';
import { viewTileMarks, viewTilesSource } from './figure-data.js';
import { manifestOf } from './manifests.js';

const encoder = new TextEncoder();
// A view's tiles are JPEG of this quality: to look at (the figures are the record).
const TILE_QUALITY = 0.9;
const many = (n, word) => `${n.toLocaleString('en-US')} ${word}${n === 1 ? '' : 's'}`;

// The report's file name: "report-<title>-<short fingerprint>.html".
export function reportFileName(title, certificate) {
  return `report-${safeName(title, 'report')}-${certificate.fingerprint.slice(0, 8)}.html`;
}

// The app as it draws and certifies a project other than its own (the project redacted).
function withProject(app, project) {
  const store = Object.create(app.store, { project: { value: project, enumerable: true } });
  return Object.create(app, { store: { value: store, enumerable: true } });
}

// A text's people, notes and names replaced, but not within the images it carries (base64).
function replaceOutsideData(text, redaction) {
  return text.split(/(data:[\w/+.-]+;base64,[A-Za-z0-9+/=]+)/).map((part, i) => (i % 2 ? part : redaction.replace(part))).join('');
}

// The results chosen, redacted, drawn and certified: { project (redacted), shown (the app drawing
// it), redaction, ids, name, figures: [{ id, name, svg, description }], figs (their records),
// drawn, certified (certify's), strings (what must not be found: leftOutStrings) }.
async function prepare(app, { title, results, names = false }) {
  const real = app.store.project;
  const ids = [...new Set(results)];
  if (!ids.length) throw new Error('choose the results it reports');
  const redaction = redactionOf(real, { names, people: [prefs.get('reviewer', '')] });
  const project = redact(real, redaction);
  const shown = withProject(app, project);
  const name = redaction.replace(String(title ?? '').trim()) || `Results of ${project.name}`;
  // Each figure drawn once: written into the report, its drawing's digests and the SHA-256 of its
  // SVG certified. (A name a result's file holds, a cohort's units, is replaced in its text.)
  const figures = [];
  const figs = [];
  const drawn = new Map();
  for (const id of ids) {
    const fig = (project.figures ?? []).find((f) => f.id === id);
    if (!fig) continue;
    const out = await writeFigure(shown, fig, 'svg', { dpi: 300, time: DRAWN_AT });
    const svg = replaceOutsideData(out.bytes, redaction);
    drawn.set(id, { manifest: out.manifest, svg: sha256(encoder.encode(svg)) });
    figures.push({ id, name: fig.name, svg, description: redaction.replace(out.drawn.description) });
    figs.push(fig);
  }
  const certified = await certify(shown, { kind: 'results', ids, name }, { drawn, redaction });
  // What must not be found: every path of this computer the project and the steps' manifests hold,
  // and the people, notes and names left out.
  const manifests = await gatherManifests(real, ids, manifestOf);
  const strings = leftOutStrings(redaction, { paths: [...absolutePaths(real), ...absolutePaths([...manifests.values()])] });
  return { project, shown, redaction, ids, name, figures, figs, drawn, certified, strings };
}

// Throws when any text holds what was left out.
function refuseLeftOut(found, what) {
  if (!found.length) return;
  const where = [...new Set(found.map((f) => `${f.what} in ${f.file}`))];
  throw new Error(`the ${what} would hold ${where.slice(0, 3).join('; ')}${where.length > 3 ? ` and ${where.length - 3} more` : ''}: it was not written`);
}

// The report of results (IDs; its figures first, then a table of each other result's numbers, in
// the order given), titled. options: { names (leave out the samples' and subjects' names) }.
// Answers { html, name, certificate, figures (how many), numbers (how many it traces to the
// certificate), leftOut, checked: { texts, strings } }.
export async function writeReport(app, { title, results, names = false }) {
  const p = await prepare(app, { title, results, names });
  const c = p.certified.certificate;
  const html = reportHTML({ title: p.name, certificate: c, zip: p.certified.bytes, methods: p.certified.methods, figures: p.figures, results: p.ids.filter((id) => !p.drawn.has(id)) });
  const name = reportFileName(p.name, c);
  const texts = await packageTexts([{ name, data: html }]);
  refuseLeftOut(findLeftOut(texts, p.strings), 'report');
  return { html, name, certificate: c, figures: p.figures.length, numbers: reportNumbers(html).length, leftOut: p.redaction.counts, checked: { texts: texts.length, strings: p.strings.length } };
}

// The saved views a package's figures show: [{ view, panels: [{ region, aspect }], regions (true,
// or the IDs of the regions drawn) }], each once, in the figures' order.
function viewsShown(project, figs) {
  const out = new Map();
  for (const fig of figs) {
    for (const panel of fig.panels.filter((x) => x.kind === 'tissue')) {
      const view = (project.views ?? []).find((v) => v.id === panel.view);
      if (!view) continue;
      if (!out.has(view.id)) out.set(view.id, { view, panels: [], regions: [] });
      const entry = out.get(view.id);
      const box = panelBox(fig, panel);
      if (panel.region) entry.panels.push({ region: panel.region, aspect: box.h / box.w });
      if (panel.regions === true || panel.regions === 'all') entry.regions = true;
      else if (Array.isArray(panel.regions) && entry.regions !== true) entry.regions = [...new Set([...entry.regions, ...panel.regions])];
    }
  }
  return [...out.values()];
}

// The result package of results, as a zip of its folder. options: as writeReport's; onProgress(text)
// says how far it has come. Answers { zip, name (the zip's), folder, certificate, views, tiles,
// files, numbers, leftOut, checked }.
export async function writePackage(app, { title, results, names = false, onProgress = () => {} }) {
  onProgress('Drawing the figures and certifying');
  const p = await prepare(app, { title, results, names });
  const c = p.certified.certificate;
  const files = [];
  const views = [];
  let tiles = 0;
  const canvas = document.createElement('canvas');
  for (const [k, { view, panels, regions }] of viewsShown(p.project, p.figs).entries()) {
    const slug = `${k + 1}-${safeName(view.name, 'view')}`;
    const extent = viewExtent(view, panels);
    const source = await viewTilesSource(p.shown, view, extent, { regions });
    const levels = tilePlan(extent, { umPerPx: view.camera.umPerCss, fullRes: source.fullRes });
    const list = tilesOf(extent, levels, `views/${slug}`);
    for (const [i, t] of list.entries()) {
      onProgress(`Drawing the view ${view.name}: tile ${i + 1} of ${list.length}`);
      paintFigure(canvas, { width: TILE, height: TILE }, { marks: await viewTileMarks(p.shown, source, t.region, TILE) }, 1);
      const blob = await new Promise((resolve) => canvas.toBlob(resolve, 'image/jpeg', TILE_QUALITY));
      files.push({ name: t.path, data: new Uint8Array(await blob.arrayBuffer()), compress: false });
    }
    tiles += list.length;
    const dataset = p.project.datasets.find((d) => d.id === view.dataset);
    const across = extent.x1 - extent.x0;
    const description = `${dataset?.name ?? 'Its dataset'}, ${across >= 1000 ? `${(across / 1000).toFixed(2)} mm` : `${Math.round(across)} µm`} across, in ${many(levels.length, 'level')} of tiles.`;
    files.push({ name: `views/${slug}.html`, data: readerHTML({ title: view.name, dataset: dataset?.name ?? '', project: p.project.name, software: app.version ?? '', extent, levels, dir: slug, tile: TILE, legend: source.legend, regions: source.regions, back: '../index.html' }) });
    views.push({ name: view.name, file: `views/${slug}.html`, thumb: `views/${slug}/0/0-0.jpg`, description, tiles: list.length, levels: levels.length });
  }
  onProgress('Writing the report and checking what it holds');
  const html = reportHTML({ title: p.name, certificate: c, zip: p.certified.bytes, methods: p.certified.methods, figures: p.figures, results: p.ids.filter((id) => !p.drawn.has(id)), views: views.map((v) => ({ name: v.name, href: v.file, thumb: v.thumb, description: v.description })) });
  const certificateFile = `certificate-${c.fingerprint.slice(0, 8)}.zip`;
  files.unshift({ name: 'index.html', data: html }, { name: certificateFile, data: p.certified.bytes, compress: false });
  // Every text searched for what was left out: the files', what the zips and the report carry, then
  // the README and the list of files, which say what was searched.
  const texts = await packageTexts(files);
  const checked = { texts: texts.length + 2, strings: p.strings.length };
  const info = { title: p.name, certificate: c, certificateFile, leftOut: p.redaction.counts, checked, views };
  files.push({ name: 'README.txt', data: packageReadme(info) });
  const manifest = packageManifest(files, info);
  refuseLeftOut(findLeftOut([...texts, { name: 'README.txt', text: files.at(-1).data }, { name: 'manifest.json', text: manifest }], p.strings), 'package');
  files.push({ name: 'manifest.json', data: manifest });
  const folder = packageFolder(p.name, c.fingerprint);
  const zip = await createZip(files.map((f) => ({ ...f, name: `${folder}/${f.name}` })), { date: PACKAGE_DATE, utc: true });
  return { zip, name: `${folder}.zip`, folder, certificate: c, views: views.length, tiles, files: files.length, numbers: reportNumbers(html).length, leftOut: p.redaction.counts, checked };
}

// The Reports view's section: the report's title, the results chosen, whether the samples' and
// subjects' names are left out, and the report or the package written. Answers { element, update() }.
export function mountReport(app) {
  const chosen = new Set();
  const title = h('input.input.small', { type: 'text', 'aria-label': 'The report\'s title' });
  const names = h('input', { type: 'checkbox' });
  const list = h('div.report-results');
  const status = h('span.muted.report-status');
  const reportButton = h('button.btn.primary', { type: 'button', disabled: true, onclick: () => write('report') }, icon('download'), 'Write the report (.html)');
  const packageButton = h('button.btn', { type: 'button', disabled: true, title: 'The report, each saved view its figures show drawn in advance as tiles, and the certificate, in a folder that opens in any browser', onclick: () => write('package') }, icon('download'), 'Write a package (.zip)');
  const element = h('div.report-section',
    h('div.section-title', 'A report'),
    h('p.muted.reports-intro', 'Results chosen, in one HTML file that opens in any browser, offline: their figures, a table of each result\'s numbers (each traced to its place in the certificate), the methods and their references, and the certificate itself, carried in the file. As it opens, the report checks that the certificate is intact and that each number, figure and paragraph in it is the certificate\'s; given the bundles, situcraft verify makes the results again from the certificate. A package adds the saved views the figures show, drawn in advance as tiles to move about and zoom into, in a folder that opens without SituCraft. Both leave out where files lay on this computer, people\'s names and their notes, and every file is searched for them before it is written.'),
    h('label.field.record-picker', h('span', 'Title'), title),
    list,
    h('label.viewer-toggle', names, 'Leave out the samples\' and subjects\' names'),
    h('div.btn-row', reportButton, packageButton, status));
  let shape = '';

  function counted() {
    reportButton.disabled = packageButton.disabled = chosen.size === 0;
    status.textContent = chosen.size ? `${chosen.size} chosen` : 'Choose the results it reports.';
  }

  function fill() {
    const p = app.store.project;
    title.placeholder = `Results of ${p.name}`;
    const results = recordableResults(p);
    const next = JSON.stringify([p.name, results]);
    if (next === shape) return;
    shape = next;
    for (const id of [...chosen]) if (!results.some((r) => r.id === id)) chosen.delete(id);
    const groups = new Map();
    for (const r of results) {
      if (!groups.has(r.kind)) groups.set(r.kind, []);
      groups.get(r.kind).push(r);
    }
    clear(list).append(...(results.length
      ? [...groups].map(([kind, items]) => h('div.report-group',
        h('div.muted', plural(STEP_KINDS[kind] ?? kind)),
        ...items.map((r) => h('label.viewer-toggle', h('input', { type: 'checkbox', value: r.id, checked: chosen.has(r.id), onchange: (event) => { if (event.target.checked) chosen.add(r.id); else chosen.delete(r.id); counted(); } }), r.name))))
      : [h('p.muted', 'No result yet: a report is of the project\'s results and figures.')]));
    counted();
  }

  async function write(what) {
    const results = recordableResults(app.store.project).map((r) => r.id).filter((id) => chosen.has(id));
    const busy = toast(what === 'package' ? 'Writing the package…' : 'Writing the report…', { timeout: 0 });
    reportButton.disabled = packageButton.disabled = true;
    try {
      if (what === 'package') {
        const out = await writePackage(app, { title: title.value, results, names: names.checked, onProgress: (text) => { busy.querySelector('span')?.replaceChildren(text); } });
        downloadBlob(new Blob([out.zip], { type: 'application/zip' }), out.name);
        element.dataset.written = out.name;
        toast(`Wrote ${out.name}: the report, ${many(out.views, 'view')} in ${many(out.tiles, 'tile')} and the certificate (${shortFingerprint(out.certificate.fingerprint)}); ${many(out.checked.texts, 'text')} searched, nothing left out found. Unzip it and open index.html.`, { kind: 'ok', timeout: 10000 });
      } else {
        const out = await writeReport(app, { title: title.value, results, names: names.checked });
        downloadBlob(new Blob([out.html], { type: 'text/html' }), out.name);
        element.dataset.written = out.name;
        toast(`Wrote ${out.name}: ${many(out.figures, 'figure')}, ${many(out.numbers, 'number')} traced to its certificate (${shortFingerprint(out.certificate.fingerprint)}).`, { kind: 'ok', timeout: 8000 });
      }
    } catch (error) {
      toast(`The ${what} could not be written: ${error.message}`, { kind: 'error', timeout: 12000 });
    } finally {
      busy.remove();
      counted();
    }
  }

  fill();
  return { element, update: fill };
}
