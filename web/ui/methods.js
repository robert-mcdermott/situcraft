// The methods text (lib/methods-text.js) as the window shows it and remote control asks for it: of
// the project, of a dataset (its filters chosen, by default the latest version of each) or of a
// result, written from its record (the artifacts' manifests read from the program), each dataset's
// validation report and an example's source. Shown in the Data view (a dataset's) and in the
// Reports view (any).

import { h, icon, clear, formatCount, downloadBlob } from './dom.js';
import { showDialog, toast } from './overlays.js';
import { exampleSource } from '../lib/examples.js';
import { gatherManifests, itemOf, recordableResults, resultRecord, scopeRoots } from '../lib/record.js';
import { scopeOfValue, scopeOptions, scopeValue } from './scopes.js';
import { methodsBibTeX, methodsMarkdown, methodsPlain, methodsText } from '../lib/methods-text.js';
import { manifestOf } from './manifests.js';

// The dataset's filters, and the latest version of each.
export function datasetFilters(project, dataset) {
  const filters = project.artifacts.filter((a) => a.kind === 'filter' && a.dataset === dataset.id);
  const latest = new Map();
  for (const f of filters) if (!latest.has(f.name) || latest.get(f.name).version < f.version) latest.set(f.name, f);
  return { filters, latest: [...latest.values()] };
}

// The record of a scope and its methods: { project, manifests, record (without a time), methods }.
// scope: { kind: 'project' }, { kind: 'dataset', id, filters (ids: those described; the latest
// version of each if not given) } or { kind: 'result', id }.
export async function recordAndMethods(app, scope) {
  const project = app.store.project;
  const { roots, subject } = scopeRoots(project, scope);
  const manifests = await gatherManifests(project, roots, manifestOf);
  const record = resultRecord(project, roots, { manifests, program: { version: app.version ?? '', commit: app.commit ?? '' }, subject });
  // Each dataset's validation report (its panel's genes, the transcripts counted, the instrument's
  // segmentation methods) and, for an example, its source.
  if (!app.examples.list()) await app.examples.refresh().catch(() => null);
  const reports = {};
  const sources = {};
  for (const s of record.steps.filter((x) => x.kind === 'dataset')) {
    const dataset = itemOf(project, s.id).record;
    const item = app.bundles.link(dataset.id)?.item ?? app.bundles.itemByPath(dataset.path);
    reports[dataset.id] = item ? await app.bundles.validate(item).catch(() => null) : null;
    sources[dataset.id] = exampleSource(app.examples.forDataset(dataset));
  }
  return { project, manifests, record, methods: methodsText(project, record, { reports, sources, scope: scope.kind }) };
}

// The methods text of a scope (as recordAndMethods).
export async function writeMethods(app, scope) {
  return (await recordAndMethods(app, scope)).methods;
}

export const METHODS_FORMATS = {
  markdown: { ext: 'md', mime: 'text/markdown', label: 'Markdown', write: methodsMarkdown },
  text: { ext: 'txt', mime: 'text/plain', label: 'Text', write: methodsPlain },
  bibtex: { ext: 'bib', mime: 'application/x-bibtex', label: 'BibTeX', write: methodsBibTeX },
};

export function methodsFileName(m, format) {
  const base = m.subject.name.replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '').slice(0, 80) || m.subject.id;
  return `methods-${base}.${METHODS_FORMATS[format].ext}`;
}

// The text's paragraphs (each under its heading) and its references, as the window shows them.
function methodsBody(m) {
  if (!m.paragraphs.length) return [h('p.muted', 'Nothing to describe yet: add a dataset, or make a result.')];
  return [
    h('div.methods-text', ...m.paragraphs.map((p) => h('section.methods-paragraph', { 'data-topic': p.topic }, h('h4', p.heading), h('p', p.text)))),
    m.references.length ? h('div.section-title', 'References') : null,
    m.references.length ? h('ol.methods-refs', ...m.references.map((r) => h('li', { value: r.n }, r.text, r.doi && !r.text.includes(r.doi) ? ` doi:${r.doi}` : ''))) : null,
  ];
}

async function copyMethods(m) {
  try {
    await navigator.clipboard.writeText(methodsPlain(m));
    toast('Copied the methods and their references.', { kind: 'ok' });
    return true;
  } catch {
    toast('The clipboard is not available: select the text and copy it.', { kind: 'error' });
    return false;
  }
}

function writeButtons(get) {
  return Object.entries(METHODS_FORMATS).filter(([id]) => id !== 'text').map(([id, f]) => h('button.btn.small', { type: 'button', onclick: () => {
    const m = get();
    if (!m) return;
    downloadBlob(new Blob([f.write(m)], { type: f.mime }), methodsFileName(m, id));
  } }, icon('download'), f.label));
}

// A dataset's methods in a dialog (the Data view's Methods), its filters to describe chosen.
export async function showMethods(app, dataset, { chosen: initial = null } = {}) {
  const { filters, latest } = datasetFilters(app.store.project, dataset);
  const chosen = new Set((initial ?? latest).map((f) => f.id));
  const body = h('div.methods-body', h('p.muted', 'Writing…'));
  let current = null;
  async function write() {
    current = await writeMethods(app, { kind: 'dataset', id: dataset.id, filters: filters.filter((f) => chosen.has(f.id)).map((f) => f.id) });
    clear(body).append(...methodsBody(current));
  }
  await write();
  const choices = filters.length ? h('div.methods-filters', h('b', 'Filters to describe'),
    ...filters.map((f) => h('label.viewer-toggle', h('input', { type: 'checkbox', checked: chosen.has(f.id), onchange: (event) => { if (event.target.checked) chosen.add(f.id); else chosen.delete(f.id); write(); } }),
      `${f.name}, version ${f.version} (${formatCount(f.result.kept)} of ${formatCount(f.result.cells)} cells)`))) : null;
  return showDialog({
    title: `Methods: ${dataset.name}`,
    width: 'wide',
    content: [
      h('p.muted', 'Written from what the project records of this dataset and everything made of it, step by step, with SituCraft\'s version. Check it against what you did, and add what was done elsewhere.'),
      body,
      choices,
      h('div.btn-row', h('span.field-label', 'Write it as'), ...writeButtons(() => current)),
    ],
    buttons: [
      { label: 'Close', ghost: true },
      { label: 'Copy', primary: true, onClick: async () => ((await copyMethods(current)) ? undefined : false) },
    ],
  });
}

// The Reports view's section: the methods of the project, a dataset or a result, chosen. Answers {
// element, update(), show(scope) }.
export function mountMethods(app) {
  const picker = h('select.input.small', { 'aria-label': 'What the methods describe', onchange: (e) => show(scopeOfValue(e.target.value)) });
  const body = h('div.record-body');
  const element = h('div.methods-section',
    h('div.section-title', 'Methods'),
    h('p.muted.reports-intro', 'A methods section written from the record: every step from the bundles, said as what was done, with its parameters, seeds and software, in paragraphs by topic, its references numbered as first cited. Of the whole project, a dataset (and everything made of it) or a result. Check it against what you did, and add what was done elsewhere.'),
    h('label.field.record-picker', h('span', 'Of'), picker),
    body);
  let current = 'project';
  let written = null;
  let shape = '';
  let generation = 0;
  function fillPicker() {
    const p = app.store.project;
    const next = JSON.stringify([p.name, p.datasets.map((d) => [d.id, d.name]), recordableResults(p)]);
    if (next === shape) return false;
    shape = next;
    clear(picker).append(...scopeOptions(p, current));
    return true;
  }

  async function show(scope, { scroll = false } = {}) {
    current = scopeValue(scope);
    picker.value = current;
    const mine = (generation += 1);
    clear(body).append(h('p.muted', 'Writing…'));
    try {
      const m = await writeMethods(app, scope);
      if (mine !== generation) return null;
      written = m;
      clear(body).append(
        h('div.btn-row', h('button.btn.small.primary', { type: 'button', onclick: () => copyMethods(m) }, icon('copy'), 'Copy'), h('span.field-label', 'Write it as'), ...writeButtons(() => written)),
        ...methodsBody(m));
      element.dataset.methods = current;
      if (scroll) element.scrollIntoView?.({ block: 'start' });
      return m;
    } catch (error) {
      if (mine === generation) clear(body).append(h('p.muted', `The methods could not be written: ${error.message}`));
      return null;
    }
  }

  fillPicker();
  show(scopeOfValue(current));
  return {
    element,
    // Written again when the project's results change.
    update() {
      if (fillPicker()) {
        if (current !== 'project' && ![...picker.options].some((o) => o.value === current)) current = 'project';
        show(scopeOfValue(current));
      }
    },
    show,
    shown: () => current,
  };
}
