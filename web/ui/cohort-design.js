// The Cohort view's design (wave 7, slice 3; lib/design.js): the replicate unit, the groups
// compared and their reference, the covariates, the contrasts and the samples left out, chosen in
// the view; the design said in words, with what it refuses and why, its rows and matrix; saved as
// versions of a name.

import { h, icon } from './dom.js';
import { promptDialog, toast } from './overlays.js';
import { addDesign, designChanges, designFields, designVersions, designsOf, makeDesign, normalizeSpec } from '../lib/design.js';
import { fieldValue, samplesOf } from '../lib/samples.js';

// The design being made, per project id, for the session: its specification and the name it is
// saved under.
const drafts = new Map();

// A project's draft: the last saved design's, else the subject compared by condition.
export function draftOf(project) {
  if (!drafts.has(project.id)) {
    const saved = designsOf(project)[0];
    const fields = designFields(project).map((f) => f.name);
    const spec = saved ? normalizeSpec(saved.spec) : normalizeSpec({ unit: fields.includes('subject') ? 'subject' : '', group: fields.includes('condition') ? 'condition' : '' });
    drafts.set(project.id, { spec, name: saved?.name ?? 'Design' });
  }
  return drafts.get(project.id);
}

// Puts a specification (and a name) in the draft, as the view would show it.
export function setDraft(project, spec, name) {
  const d = draftOf(project);
  d.spec = normalizeSpec(spec);
  if (name) d.name = name;
}

export function designSection(app, { commit, redraw }) {
  const project = app.store.project;
  const draft = draftOf(project);
  const spec = draft.spec;
  const fields = designFields(project);
  const label = (f) => (f.bundle ? `${f.label} (from the bundle)` : f.name);
  const change = (patch) => {
    Object.assign(spec, patch);
    redraw();
  };
  const design = makeDesign(project, spec);

  const select = (id, value, options, onchange, empty = 'Choose…', name = null) => h(`select.input.small#${id}`, { 'aria-label': name, onchange: (e) => onchange(e.target.value) },
    h('option', { value: '', selected: !value }, empty),
    ...options.map((o) => h('option', { value: o.value, selected: o.value === value }, o.text)));
  const fieldOptions = fields.map((f) => ({ value: f.name, text: label(f) }));
  const levels = [...new Set(samplesOf(project).map((s) => fieldValue(s, spec.group)).filter((v) => v !== '').map(String))].sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));

  const covariates = h('div.cohort-boxes', ...fields.filter((f) => f.name !== spec.unit && f.name !== spec.group).map((f) => h('label.check', h('input', {
    type: 'checkbox', checked: spec.covariates.includes(f.name),
    onchange: (e) => change({ covariates: e.target.checked ? [...spec.covariates, f.name] : spec.covariates.filter((c) => c !== f.name) }),
  }), label(f), h('span.muted', f.kind === 'number' ? ' (a number)' : ''))));

  // Contrasts: each level against the reference unless others are chosen.
  const contrastRows = (design.contrasts.length ? design.contrasts : []).map((c) => h('li', c.name, spec.contrasts.length ? h('button.btn.small.ghost', { type: 'button', 'aria-label': `Remove the contrast ${c.name}`, onclick: () => change({ contrasts: spec.contrasts.filter(([a, b]) => `${a} − ${b}` !== c.name) }) }, icon('close')) : null));
  const a = select('cd-contrast-a', '', levels.map((l) => ({ value: l, text: l })), () => {}, 'A level', 'The contrast\'s level');
  const b = select('cd-contrast-b', '', levels.map((l) => ({ value: l, text: l })), () => {}, 'against', 'The level it is against');
  const addContrast = h('button.btn.small', {
    type: 'button',
    onclick: () => {
      if (!a.value || !b.value || a.value === b.value) {
        toast('Choose two different levels.', { kind: 'error' });
        return;
      }
      const base = spec.contrasts.length ? spec.contrasts : design.contrasts.map((c) => [c.a, c.b]);
      change({ contrasts: [...base, [a.value, b.value]] });
    },
  }, icon('plus'), 'Add a contrast');

  // Samples left out, each with its reason.
  const outList = Object.entries(spec.exclude).map(([id, reason]) => {
    const s = samplesOf(project).find((x) => x.id === id);
    return h('li', h('b', s?.name ?? id), ` (${reason}) `, h('button.btn.small.ghost', { type: 'button', onclick: () => { const ex = { ...spec.exclude }; delete ex[id]; change({ exclude: ex }); } }, 'Put back'));
  });
  const leaveOut = select('cd-leave-out', '', samplesOf(project).filter((s) => !spec.exclude[s.id]).map((s) => ({ value: s.id, text: s.name })), async (id) => {
    if (!id) return;
    const reason = await promptDialog({ title: 'Leave a sample out', label: 'Why', placeholder: 'a fold across the section, failed QC…', confirm: 'Leave it out', hint: 'The reason is kept with the design.' });
    if (reason) change({ exclude: { ...spec.exclude, [id]: reason } });
    else redraw();
  }, 'Leave a sample out…', 'Leave a sample out');

  // The design: refusals, notes, words, rows and matrix.
  const result = [];
  if (design.problems.length) result.push(h('div.callout.danger', h('b', 'This design cannot be used:'), h('ul', ...design.problems.map((p) => h('li', p)))));
  if (design.notes.length) result.push(h('div.callout.warn', h('ul', ...design.notes.map((n) => h('li', n)))));
  if (design.words.length) result.push(h('div.cohort-words', ...design.words.map((w) => h('p', w))));
  if (design.matrix.length) {
    const nameOf = new Map(design.samples.map((s) => [s.id, s.name]));
    result.push(h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table.cohort-matrix',
      h('thead', h('tr', h('th', `${spec.unit} in ${spec.group}`), h('th', 'Samples'), ...design.names.map((n) => h('th.r', n)))),
      h('tbody', ...design.rows.map((r, i) => h('tr', h('td', `${r.unit} · ${r.group}`), h('td', r.samples.map((id) => nameOf.get(id)).join(', ')), ...design.matrix[i].map((v) => h('td.r', String(v)))))))));
  }

  // Saving, and the saved designs.
  const nameInput = h('input.input.small', { value: draft.name, 'aria-label': 'The design\'s name', onchange: (e) => { draft.name = e.target.value.trim() || 'Design'; redraw(); } });
  const next = designVersions(project, draft.name).length + 1;
  const save = h('button.btn.small.primary', {
    type: 'button', disabled: !design.ok,
    title: design.ok ? 'Keep this design in the project, as a version of its name' : 'A design that cannot be used is not saved',
    onclick: () => commit((p) => addDesign(p, design, { name: draft.name }).project, `Save design ${draft.name}`),
  }, icon('save'), `Save as ${draft.name}, version ${next}`);
  const saved = designsOf(project).map((d) => {
    const changes = designChanges(project, d);
    return h('tr', { 'data-design': d.id, class: app.store.ui.focus?.kind === 'design' && app.store.ui.focus.id === d.id ? 'selected' : '' },
      h('td', h('b', d.name), h('span.muted', ` v${d.version}`)),
      h('td', d.result.words[0]),
      h('td', changes.length ? h('span.badge.warn', { title: changes.join('; ') }, 'the samples changed') : h('span.badge.ok', 'as saved')),
      h('td', h('button.btn.small', { type: 'button', onclick: () => { setDraft(project, d.spec, d.name); redraw(); } }, 'Use')));
  });

  return h('section.seg-section.cohort-design',
    h('div.section-title', 'Design'),
    h('div.seg-card',
      h('p', 'Before any comparison, name what is compared and what replicates it. Cells are not replicates, and neither are regions of one section: the replicate unit is the subject, patient or animal each sample comes from. Samples of one unit in one group are technical replicates, combined; a unit in more than one group pairs them.'),
      h('div.seg-import-grid',
        h('label.field', { for: 'cd-unit' }, h('span', 'Replicate unit'), select('cd-unit', spec.unit, fieldOptions, (v) => change({ unit: v }))),
        h('label.field', { for: 'cd-group' }, h('span', 'Groups compared'), select('cd-group', spec.group, fieldOptions, (v) => change({ group: v, reference: '', contrasts: [] }))),
        h('label.field', { for: 'cd-reference' }, h('span', 'Reference group'), select('cd-reference', spec.reference || design.reference || '', levels.map((l) => ({ value: l, text: l })), (v) => change({ reference: v, contrasts: [] }), 'The first'))),
      h('div.field', h('span', 'Adjust for'), covariates),
      h('div.field', h('span', 'Contrasts'), h('ul.cohort-list', ...contrastRows), levels.length > 2 ? h('div.btn-row', a, b, addContrast) : null),
      h('div.field', h('span', 'Samples left out'), outList.length ? h('ul.cohort-list', ...outList) : h('p.muted.seg-note', 'None.'), h('div.btn-row', leaveOut)),
      // Regions of one section as separate units: refused, unless a teaching example says so.
      samplesOf(project).some((s) => s.region) || spec.pseudoSamples ? h('label.check.cohort-pseudo', h('input', {
        type: 'checkbox', checked: Boolean(spec.pseudoSamples),
        onchange: (e) => change({ pseudoSamples: e.target.checked }),
      }), 'Regions of one section as pseudo-samples, for teaching only: they are not independent replicates') : null,
      ...result,
      h('div.btn-row', nameInput, save)),
    saved.length ? h('div.seg-card', { style: { marginTop: '10px' } },
      h('h3.seg-card-title', 'Saved designs'),
      h('table.data.seg-table', h('thead', h('tr', h('th', 'Design'), h('th', 'What it compares'), h('th', 'Samples'), h('th', h('span.sr-only', 'Use')))), h('tbody', ...saved))) : null);
}
