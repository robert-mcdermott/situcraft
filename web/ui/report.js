// The report (wave 8, slice 7) as the page writes it: the results chosen (each figure drawn as SVG,
// carrying its manifest; a table of each other result's numbers), their methods and references,
// and their certificate, in one HTML file that checks itself as it opens (lib/report.js). In the
// Reports view, and by remote control (export, what: report).

import { h, icon, clear, downloadBlob } from './dom.js';
import { toast } from './overlays.js';
import { STEP_KINDS, plural, recordableResults } from '../lib/record.js';
import { shortFingerprint } from '../lib/certificate.js';
import { reportHTML, reportNumbers } from '../lib/report.js';
import { sha256 } from '../lib/sha256.js';
import { DRAWN_AT, certify } from './certificate.js';
import { writeFigure } from './figures.js';

const many = (n, word) => `${n.toLocaleString('en-US')} ${word}${n === 1 ? '' : 's'}`;

// The report's file name: "report-<title>-<short fingerprint>.html".
export function reportFileName(title, certificate) {
  const base = String(title).replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '').slice(0, 60) || 'report';
  return `report-${base}-${certificate.fingerprint.slice(0, 8)}.html`;
}

// The report of results (IDs; its figures first, then a table of each other result's numbers, in
// the order given), titled. Answers { html, name, certificate, figures (how many), numbers (how
// many it traces to the certificate) }.
export async function writeReport(app, { title, results }) {
  const project = app.store.project;
  const ids = [...new Set(results)];
  if (!ids.length) throw new Error('choose the results it reports');
  const name = String(title ?? '').trim() || `Results of ${project.name}`;
  // Each figure drawn once: written into the report, its drawing's digests and the SHA-256 of its
  // SVG certified.
  const figures = [];
  const drawn = new Map();
  for (const id of ids) {
    const fig = (project.figures ?? []).find((f) => f.id === id);
    if (!fig) continue;
    const out = await writeFigure(app, fig, 'svg', { dpi: 300, time: DRAWN_AT });
    drawn.set(id, { manifest: out.manifest, svg: sha256(new TextEncoder().encode(out.bytes)) });
    figures.push({ id, name: fig.name, svg: out.bytes, description: out.drawn.description });
  }
  const certified = await certify(app, { kind: 'results', ids, name }, { drawn });
  const html = reportHTML({ title: name, certificate: certified.certificate, zip: certified.bytes, methods: certified.methods, figures, results: ids.filter((id) => !drawn.has(id)) });
  return { html, name: reportFileName(name, certified.certificate), certificate: certified.certificate, figures: figures.length, numbers: reportNumbers(html).length };
}

// The Reports view's section: the report's title, the results chosen and the file written. Answers
// { element, update() }.
export function mountReport(app) {
  const chosen = new Set();
  const title = h('input.input.small', { type: 'text', 'aria-label': 'The report\'s title' });
  const list = h('div.report-results');
  const status = h('span.muted.report-status');
  const button = h('button.btn.primary', { type: 'button', disabled: true, onclick: () => write() }, icon('download'), 'Write the report (.html)');
  const element = h('div.report-section',
    h('div.section-title', 'A report'),
    h('p.muted.reports-intro', 'Results chosen, in one HTML file that opens in any browser, offline: their figures, a table of each result\'s numbers (each traced to its place in the certificate), the methods and their references, and the certificate itself, carried in the file. As it opens, the report checks that the certificate is intact and that each number, figure and paragraph in it is the certificate\'s; given the bundles, situcraft verify makes the results again from the certificate.'),
    h('label.field.record-picker', h('span', 'Title'), title),
    list,
    h('div.btn-row', button, status));
  let shape = '';

  function counted() {
    button.disabled = chosen.size === 0;
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

  async function write() {
    const results = recordableResults(app.store.project).map((r) => r.id).filter((id) => chosen.has(id));
    const busy = toast('Writing the report…', { timeout: 0 });
    button.disabled = true;
    try {
      const out = await writeReport(app, { title: title.value, results });
      downloadBlob(new Blob([out.html], { type: 'text/html' }), out.name);
      element.dataset.written = out.name;
      toast(`Wrote ${out.name}: ${many(out.figures, 'figure')}, ${many(out.numbers, 'number')} traced to its certificate (${shortFingerprint(out.certificate.fingerprint)}).`, { kind: 'ok', timeout: 8000 });
    } catch (error) {
      toast(`The report could not be written: ${error.message}`, { kind: 'error', timeout: 10000 });
    } finally {
      busy.remove();
      counted();
    }
  }

  fill();
  return { element, update: fill };
}
