// What made a result (wave 8, slice 3), in the Reports view: a result chosen among the project's (or
// a dataset's results, or the whole project's), its record (lib/record.js) shown step by step from
// the bundles to it, each step's method, version, parameters, seed, software and references, and
// the record written as Markdown, JSON or BibTeX, or certified (slice 5, ui/certificate.js).

import { h, icon, clear, downloadBlob } from './dom.js';
import { toast } from './overlays.js';
import { STEP_KINDS, decisionWords, gatherManifests, groupSteps, numbers, parameterText, plural, recordBibTeX, recordJSON, recordMarkdown, recordableResults, resultRecord, scopeRoots } from '../lib/record.js';
import { shortFingerprint } from '../lib/certificate.js';
import { manifestOf } from './manifests.js';
import { certify } from './certificate.js';
import { scopeOfValue, scopeOptions, scopeValue } from './scopes.js';

// The record of a scope (a result's ID, or { kind, id }), its artifacts' manifests read from the
// program.
export async function recordOf(app, scope, { now = new Date().toISOString() } = {}) {
  const project = app.store.project;
  const { roots, subject } = scopeRoots(project, typeof scope === 'string' ? { kind: 'result', id: scope } : scope);
  const manifests = await gatherManifests(project, roots, manifestOf);
  return resultRecord(project, roots, { manifests, program: { version: app.version ?? '', commit: app.commit ?? '' }, now, subject });
}

export const RECORD_FORMATS = {
  markdown: { ext: 'md', mime: 'text/markdown', label: 'Markdown', write: recordMarkdown },
  json: { ext: 'json', mime: 'application/json', label: 'JSON', write: recordJSON },
  bibtex: { ext: 'bib', mime: 'application/x-bibtex', label: 'BibTeX', write: recordBibTeX },
};

// A record's file name: "what-made-<result>.<ext>".
export function recordFileName(record, format) {
  const base = record.result.name.replace(/[^\w.-]+/g, '_').replace(/^_+|_+$/g, '').slice(0, 80) || record.result.id;
  return `what-made-${base}.${RECORD_FORMATS[format].ext}`;
}

// The section: { element, update(), show(id) }.
export function mountRecord(app) {
  const picker = h('select.input.small', { 'aria-label': 'What the record is of', onchange: (e) => show(e.target.value ? scopeOfValue(e.target.value) : null) });
  const body = h('div.record-body');
  const element = h('div.record',
    h('div.section-title', 'What made a result'),
    h('p.muted.reports-intro', 'Each step from the bundles to a result (or to a dataset\'s results, or the whole project\'s): its method and version, its parameters and seed, the software that ran it and the publications it follows, read from the project and the program\'s records of what each job was given. Written as Markdown, JSON or BibTeX, or certified: a zip of what it rests on and what it reports, fingerprinted, that another computer checks by making it again.'),
    h('label.field.record-picker', h('span', 'Of'), picker),
    body);
  let current = null;
  let shape = '';
  let generation = 0;

  function fillPicker() {
    const p = app.store.project;
    const next = JSON.stringify([p.name, p.datasets.map((d) => [d.id, d.name]), recordableResults(p)]);
    if (next === shape) return;
    shape = next;
    clear(picker).append(...scopeOptions(p, current, { none: 'Choose what made what…' }));
    if (current && ![...picker.options].some((o) => o.value === current)) show(null);
  }

  async function show(scope, { scroll = false } = {}) {
    if (typeof scope === 'string') scope = { kind: 'result', id: scope };
    current = scope ? scopeValue(scope) : null;
    picker.value = current ?? '';
    const mine = (generation += 1);
    if (!scope) {
      clear(body);
      element.dataset.record = '';
      return;
    }
    clear(body).append(h('p.muted', 'Reading its record…'));
    try {
      const record = await recordOf(app, scope);
      if (mine !== generation) return;
      clear(body).append(...recordView(record, scope));
      element.dataset.record = scope.kind === 'project' ? app.store.project.id : scope.id;
      if (scroll) element.scrollIntoView?.({ block: 'start' });
    } catch (error) {
      if (mine === generation) clear(body).append(h('p.muted', `Its record could not be read: ${error.message}`));
    }
  }

  async function writeCertificateFile(scope) {
    const busy = toast('Certifying…', { timeout: 0 });
    try {
      const out = await certify(app, scope);
      downloadBlob(new Blob([out.bytes], { type: 'application/zip' }), out.name);
      toast(`Wrote ${out.name}: fingerprint ${shortFingerprint(out.certificate.fingerprint)}.`, { kind: 'ok', timeout: 8000 });
    } catch (error) {
      toast(`It could not be certified: ${error.message}`, { kind: 'error', timeout: 10000 });
    } finally {
      busy.remove();
    }
  }

  function recordView(record, scope) {
    const refText = (list) => (list.length ? `[${numbers(list)}]` : '');
    const steps = groupSteps(record).map((g) => {
      const s = g[0];
      const kind = STEP_KINDS[s.kind] ?? s.kind;
      const label = g.length === 1 ? `${s.n}` : `${s.n}–${g.at(-1).n}`;
      const inputs = [...new Set(g.flatMap((x) => x.inputs))].sort((a, b) => a - b);
      const params = s.kind === 'dataset' ? '' : parameterText(s.parameters);
      const decisions = [...new Set(g.flatMap((x) => x.decisions.map((d) => `${decisionWords(d.kind)}${d.rationale ? `: "${d.rationale}"` : ''} (${d.by ?? ''})`)))];
      return h('li.record-step', { 'data-step': s.n, value: s.n },
        h('div.record-step-head', h('span.badge', kind), ' ',
          g.length === 1 ? h('b', s.name) : h('b', `${plural(kind)}, one for each of ${g.length}`),
          g.length > 1 ? h('span.muted', ` (steps ${label})`) : null),
        g.length > 1 ? h('div.muted.record-items', g.map((x) => (x.dataset && x.name === s.name ? x.dataset : x.dataset ? `${x.dataset}: ${x.name}` : x.name)).join('; ')) : null,
        h('dl.kv.record-kv',
          s.method?.name ? [h('dt', 'Method'), h('dd', `${s.method.name}${s.method.version ? `, version ${s.method.version}` : ''}`)] : null,
          inputs.length ? [h('dt', 'From'), h('dd', `step${inputs.length === 1 ? '' : 's'} ${numbers(inputs)}`)] : null,
          s.kind === 'dataset' ? g.map((x) => [h('dt', g.length > 1 ? x.name : 'Data'), h('dd', [x.parameters?.panel, Number.isFinite(x.parameters?.cells) ? `${x.parameters.cells.toLocaleString('en-US')} cells` : ''].filter(Boolean).join(', '))]) : null,
          params ? [h('dt', 'Parameters'), h('dd', params)] : null,
          s.seeds.length ? [h('dt', 'Seed'), h('dd.mono', s.seeds.join(', '))] : null,
          s.software.length ? [h('dt', 'Software'), h('dd', s.software.map((x) => `${x.name} ${x.version}`.trim()).join('; '))] : null,
          s.by ? [h('dt', 'By'), h('dd', s.by)] : null,
          decisions.length ? [h('dt', 'Decided'), h('dd', decisions.join(' '))] : null,
          s.note ? [h('dt', 'Note'), h('dd', s.note)] : null,
          s.references.length ? [h('dt', 'References'), h('dd', refText(s.references))] : null));
    });
    const write = (format) => {
      const f = RECORD_FORMATS[format];
      downloadBlob(new Blob([f.write(record)], { type: f.mime }), recordFileName(record, format));
      toast(`Wrote the record of ${record.result.name} as ${f.label}.`, { kind: 'ok' });
    };
    return [
      h('p', `${{ project: 'The project', dataset: 'The dataset' }[record.result.kind] ?? STEP_KINDS[record.result.kind] ?? record.result.kind}: ${record.steps.length} step${record.steps.length === 1 ? '' : 's'}, ${record.references.length} reference${record.references.length === 1 ? '' : 's'}, from ${record.steps.filter((s) => s.kind === 'dataset').length} bundle${record.steps.filter((s) => s.kind === 'dataset').length === 1 ? '' : 's'}.`),
      h('div.btn-row',
        h('span.field-label', 'Write it as'),
        ...Object.entries(RECORD_FORMATS).map(([id, f]) => h('button.btn.small', { type: 'button', onclick: () => write(id) }, icon('download'), f.label)),
        h('button.btn.small', { type: 'button', title: 'A zip of what it rests on and what it reports, fingerprinted, with a README saying how to check it', onclick: () => writeCertificateFile(scope) }, icon('download'), 'Certificate')),
      h('ol.record-steps', ...steps),
      record.software.length ? h('div.record-software', h('div.muted', 'Software'), h('ul.inspector-list', ...record.software.map((x) => h('li', `${x.name} ${x.version}`.trim())))) : null,
      record.references.length ? h('div.record-references', h('div.muted', 'References'), h('ol.record-refs', ...record.references.map((r) => h('li', { value: r.n }, r.text, r.doi && !r.text.includes(r.doi) ? ` doi:${r.doi}` : '')))) : null,
    ];
  }

  fillPicker();
  return {
    element,
    update: fillPicker,
    show,
    shown: () => current,
  };
}
