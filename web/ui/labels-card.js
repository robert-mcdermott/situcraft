// Labels in the Cell types view (internal/label, api_labels.go, api_references.go): a marker set
// chosen (one of the project's, kept as versions, or one SituCraft ships), edited, saved as a file
// or read from one; a clustering's cells labeled from it, or from a labeled single-cell reference,
// by a job; labels made elsewhere imported for the segmentation's cells (a table, Xenium Explorer's
// cell groups, the instrument's clusters); the label sets the clustering's cells carry; and the one
// shown: its cells labeled at each level, each cluster's labels and their shares, names for the
// clusters from them, each label's markers and control genes (or reference profile), and the map,
// the tissue and the Viewer colored by its labels.

import { h, icon, clear, formatCount, relativeTime } from './dom.js';
import { toast } from './overlays.js';
import { browseButton } from './file-picker.js';
import { reviewSections } from './review-card.js';
import { remakeOnSegmentation, remaking } from './remake.js';
import { clusterNames, nameClusters, nameOf } from '../lib/clusterings.js';
import { LABEL_PARAMETERS, REFERENCE_PARAMETERS, addLabel, changeLabel, checkLabelParameters, checkReferenceParameters, composition, compositionOf, describeLabels, labelColor, labelSetName, labelSetsFor, labelTree, markerSetFile, markerSetVersions, markerSetsOf, namesFromLabels, oneLevel, parseGenes, removeLabel, saveMarkerSet, withoutMissing } from '../lib/labels.js';

const percent = (v, digits = 0) => (Number.isFinite(v) ? `${(100 * v).toFixed(digits)}%` : '—');
const exact = (v) => (Number.isFinite(v) ? v.toLocaleString('en-US') : '—');

let defaults = null;
export async function labelDefaults() {
  if (!defaults) {
    const response = await fetch('/api/labels/defaults');
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    defaults = await response.json();
  }
  return defaults;
}

// Per clustering, the panel's genes and the sets SituCraft offers, read once.
const offered = new Map();
function startersFor(app, item, clusteringId) {
  if (!offered.has(clusteringId)) {
    const p = app.bundles.markerStarters(item, clusteringId);
    p.catch(() => offered.delete(clusteringId));
    offered.set(clusteringId, p);
  }
  return offered.get(clusteringId);
}

// Per bundle, the instrument's clusterings (its analysis), read once.
const instrumentAnalyses = new Map();
function instrumentClusterings(item, onRead) {
  if (!item) return null;
  if (!instrumentAnalyses.has(item.path)) {
    const entry = { list: null };
    instrumentAnalyses.set(item.path, entry);
    fetch(`${item.url}/analysis`).then((r) => (r.ok ? r.json() : null)).then((a) => {
      entry.list = a?.clusterings ?? [];
      if (entry.list.length) onRead?.();
    }).catch(() => {
      entry.list = [];
    });
  }
  return instrumentAnalyses.get(item.path).list;
}

// The markers of a set its panel lacks, by label (genes compared regardless of case).
function missingOf(set, genes) {
  const panel = new Set(genes.map((g) => g.toUpperCase()));
  const out = {};
  for (const l of [...set.labels, ...(set.states ?? [])]) {
    const lacking = [...l.positive, ...(l.negative ?? [])].filter((g) => !panel.has(g.toUpperCase()));
    if (lacking.length) out[l.name] = lacking;
  }
  return out;
}

function download(name, text) {
  const blob = new Blob([text], { type: 'application/json' });
  const a = h('a', { href: URL.createObjectURL(blob), download: name });
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
}

// Labels a clustering's cells from a marker set, by remote control or the card: request {
// markers (a project set's name or id, or a starter's id or name), set (a marker set in full),
// parameters }. The set is saved in the project first when it is not. Answers the manifest.
export async function labelClustering(app, { t, session, clustering }, request = {}) {
  const d = await labelDefaults();
  session.labels ??= { choice: '', params: null, editing: null, shown: null, level: 0, share: 0.5, open: false };
  const st = session.labels;
  const params = { ...d.parameters, ...(st.params ?? {}), ...(request.parameters ?? {}) };
  const problem = checkLabelParameters(params);
  if (problem) throw new Error(problem);
  const project = () => app.store.project;
  let record = null;
  if (request.set) {
    const checked = await app.bundles.checkMarkers(request.set);
    const known = markerSetsOf(project()).find((r) => r.name === checked.set.name);
    const next = saveMarkerSet(project(), { set: checked.set, checksum: checked.checksum, family: known?.family ?? null, source: { edited: true }, by: request.by ?? 'the user' });
    if (next !== project()) app.store.commit(next, `Add the marker set ${checked.set.name}`);
    record = markerSetsOf(project()).find((r) => r.checksum === checked.checksum);
  } else {
    const offer = await startersFor(app, t.item, clustering.id);
    const sets = markerSetsOf(project());
    const ref = request.markers === undefined || request.markers === null ? '' : String(request.markers);
    record = ref ? sets.find((r) => r.id === ref || r.family === ref) ?? sets.find((r) => r.name.toLowerCase() === ref.toLowerCase()) : null;
    if (!record) {
      const starter = ref ? offer.starters.find((s) => s.id === ref.toLowerCase() || s.set.name.toLowerCase() === ref.toLowerCase()) : offer.starters.find((s) => !s.missing.length);
      if (!starter && !ref && sets.length) record = sets[0];
      else if (!starter) throw new Error(ref ? `No marker set "${ref}": the project's are ${sets.map((r) => `"${r.name}"`).join(', ') || 'none'}; SituCraft's starters ${offer.starters.map((x) => x.id).join(', ')}.` : 'No marker set fits this panel: give one.');
      else {
        const known = sets.find((r) => r.checksum === starter.checksum);
        if (known) record = known;
        else {
          const checked = await app.bundles.checkMarkers(starter.set);
          const next = saveMarkerSet(project(), { set: checked.set, checksum: checked.checksum, source: { starter: starter.id }, by: request.by ?? 'the user' });
          if (next !== project()) app.store.commit(next, `Add the marker set ${checked.set.name}`);
          record = markerSetsOf(project()).find((r) => r.checksum === checked.checksum);
        }
      }
    }
  }
  st.choice = `set:${record.id}`;
  const m = await app.bundles.labelCells(t.item, t.dataset.id, { clustering: clustering.id, markers: record.set, parameters: params }, record.id);
  st.shown = m.id;
  return m;
}

let refDefaults = null;
export async function referenceDefaults() {
  if (!refDefaults) {
    const response = await fetch('/api/references/defaults');
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    refDefaults = await response.json();
  }
  return refDefaults;
}

// Labels a clustering's cells from a reference, by remote control or the card: request { path,
// matrix, labelColumn, geneColumn, parameters }; the columns the program suggests where not given.
// Answers the manifest.
export async function referenceClustering(app, { t, session, clustering }, request = {}) {
  const d = await referenceDefaults();
  session.labels ??= { choice: '', params: null, editing: null, shown: null, level: 0, share: 0.5, open: false };
  const st = session.labels;
  const params = { ...d.parameters, ...(st.ref?.params ?? {}), ...(request.parameters ?? {}) };
  const problem = checkReferenceParameters(params);
  if (problem) throw new Error(problem);
  if (!request.path) throw new Error('Name the reference: its path.');
  let spec = { path: request.path, matrix: request.matrix ?? '', labelColumn: request.labelColumn ?? '', geneColumn: request.geneColumn ?? '' };
  if (!spec.labelColumn && !spec.geneColumn) {
    const read = await app.bundles.readReference(t.item, { path: request.path, clustering: clustering.id });
    if (!read.preview) throw new Error(read.error ?? 'The reference could not be read.');
    spec = { ...read.preview.spec, path: read.preview.spec.path, matrix: spec.matrix || read.preview.spec.matrix || '' };
  }
  const m = await app.bundles.mapReference(t.item, t.dataset.id, { ...spec, clustering: clustering.id, parameters: params });
  st.ref ??= { path: '', read: null, spec: null, params: null, busy: false, problem: null, open: false };
  st.ref.path = request.path;
  st.shown = m.id;
  return m;
}

// ctx: { t (the view's target), seg, session (the dataset's), clustering (the record shown),
// cellMap, render(force) }. Answers the sections to show.
export function labelsSections(app, ctx) {
  const { t, session, clustering } = ctx;
  if (!clustering || !t.item) return [];
  session.labels ??= { choice: '', params: null, editing: null, shown: null, level: 0, share: 0.5, open: false };
  const st = session.labels;
  if (!st.params) {
    labelDefaults().then((d) => {
      st.params ??= { ...d.parameters };
      ctx.render(true);
    }).catch((error) => toast(`The labels' defaults: ${error.message}`, { kind: 'error' }));
    return [];
  }
  const host = h('section.seg-section.lb-section', h('div.section-title', 'Labels from markers'), h('p.muted.seg-note', 'Reading the marker sets…'));
  const rest = h('div.lb-rest');
  startersFor(app, t.item, clustering.id).then((offer) => {
    if (!host.isConnected && !document.body.contains(host)) return;
    clear(host).append(h('div.section-title', 'Labels from markers'), ...(st.editing ? editor(offer) : chooser(offer)));
    clear(rest).append(...[referenceCard(), importCard(), listCard(), shownCard(), ...reviewSections(app, ctx)].filter(Boolean));
  }).catch((error) => clear(host).append(h('div.section-title', 'Labels from markers'), h('p.muted.seg-note', `The marker sets could not be read: ${error.message}`)));

  // --- Choosing a marker set, and labeling ---
  function choices(offer) {
    const project = app.store.project;
    const mine = markerSetsOf(project).map((r) => ({ value: `set:${r.id}`, label: `${r.name} (version ${r.version})`, set: r.set, record: r, missing: missingOf(r.set, offer.genes) }));
    const inProject = new Set(markerSetsOf(project).flatMap((r) => markerSetVersions(project, r.family).map((v) => v.checksum)));
    const starters = offer.starters.filter((s) => !inProject.has(s.checksum)).map((s) => ({ value: `starter:${s.id}`, label: `${s.set.name} (SituCraft's${s.missing.length ? `; the panel lacks ${s.missing.length} of its genes` : ''})`, set: s.set, starter: s, missing: missingOf(s.set, offer.genes) }));
    return [...mine, ...starters];
  }

  function chooser(offer) {
    const list = choices(offer);
    if (!list.some((c) => c.value === st.choice)) st.choice = (list.find((c) => !Object.keys(c.missing).length) ?? list[0])?.value ?? '';
    const chosen = list.find((c) => c.value === st.choice);
    const pick = h('select.input', { id: 'lb-set', 'data-role': 'marker-set' }, ...list.map((c) => h('option', { value: c.value }, c.label)));
    pick.value = st.choice;
    pick.addEventListener('change', () => {
      st.choice = pick.value;
      ctx.render(true);
    });
    const set = chosen?.set;
    const missing = chosen ? Object.entries(chosen.missing) : [];
    const lacking = [...new Set(missing.flatMap(([, g]) => g))];
    const levels = set ? set.levels : [];
    const depth = new Map(labelTree(set ?? { labels: [] }).map((e) => [e.label.name, e.depth]));
    const perLevel = levels.map((_, l) => (set?.labels ?? []).filter((x) => depth.get(x.name) === l).length);
    const inputs = LABEL_PARAMETERS.map((spec) => {
      const input = h('input.input', { type: 'number', id: `lb-${spec.id}`, min: String(spec.min), max: String(spec.max), step: String(spec.step), value: String(st.params[spec.id]), title: spec.help });
      input.addEventListener('change', () => {
        st.params[spec.id] = Number(input.value);
      });
      return h('label.field', { for: `lb-${spec.id}`, title: spec.help }, h('span', spec.label), input);
    });
    const lineage = h('input', { type: 'checkbox', id: 'lb-lineage', checked: st.params.lineage });
    lineage.addEventListener('change', () => {
      st.params.lineage = lineage.checked;
    });
    const running = app.bundles.labelings(t.dataset.id).filter((e) => e.clustering === clustering.id);
    const go = h('button.btn.primary', { type: 'button', 'data-action': 'label', disabled: !set || lacking.length > 0, onclick: () => run(chosen) }, icon('play'), 'Label the cells');
    return [
      h('div.seg-card',
        h('p', 'Each cell is scored for each label by its markers\' expression against control genes of matched expression (Tirosh et al. 2016, as scanpy\'s score_genes computes it); a label\'s negative markers count against it where the cell expresses them. Labels are taken from the top of the hierarchy down: at each level the best label is taken when it leads the next by the margin; otherwise the cell stays at the level above. The marker set and every parameter are recorded with the labels.'),
        h('div.seg-import-grid', h('label.field', { for: 'lb-set' }, h('span', 'Marker set'), pick)),
        set ? h('p.muted.seg-note', set.about ?? '', set.about ? ' ' : '', `${set.labels.length} labels on ${levels.length} level${levels.length === 1 ? '' : 's'} (${levels.map((l, i) => `${l.toLowerCase()} ${perLevel[i]}`).join(', ')})${set.states?.length ? `; ${set.states.length} state${set.states.length === 1 ? '' : 's'} (${set.states.map((s) => s.name.toLowerCase()).join(', ')})` : ''}.`) : null,
        set?.references?.length ? h('details.lb-sources', h('summary', `Sources (${set.references.length})`), h('ul', ...set.references.map((r) => h('li', r.citation, ' ', h('span.mono', `doi:${r.doi}`))))) : null,
        lacking.length ? h('div.lb-missing', icon('warning'), h('span', `This panel lacks ${lacking.length} of the set's markers (${lacking.slice(0, 12).join(', ')}${lacking.length > 12 ? ', …' : ''}). Every marker must be in the panel.`),
          h('button.btn.small', { type: 'button', onclick: () => dropMissing(chosen) }, 'Remove them (a new version)')) : null,
        h('div.btn-row',
          h('button.btn.small', { type: 'button', disabled: !set, onclick: () => edit(chosen) }, icon('edit'), 'Edit'),
          h('button.btn.small', { type: 'button', onclick: () => importFile() }, icon('upload'), 'Read a file…'),
          h('button.btn.small', { type: 'button', disabled: !set, onclick: () => download(`${set.name.replace(/[^\w.-]+/g, '_')}.markers.json`, markerSetFile(set)) }, icon('download'), 'Save as a file')),
        h('details.seg-advanced', { open: st.open, ontoggle: (event) => { st.open = event.currentTarget.open; } },
          h('summary', 'Parameters'),
          h('div.seg-import-grid', ...inputs),
          h('label.lb-check', { for: 'lb-lineage', title: 'On a targeted panel, genes of matched expression are often markers, and a type\'s class\'s markers its own cells express: leaving them out of its controls keeps them from canceling its score.' }, lineage, ' Leave each label\'s lineage\'s markers (its class\'s and subtypes\') out of its control genes'),
          h('p.muted.seg-note', 'Hover a parameter for what it does. The bins and controls are scanpy\'s defaults; the margin is SituCraft\'s.')),
        h('div.seg-form', go),
        ...running.map((e) => progressBar(e)),
        h('p.muted.seg-cite', 'Methods: gene set scores against control genes binned by mean expression (Tirosh et al. 2016, Science 352:189–196), as scanpy\'s score_genes (Wolf et al. 2018); labels assigned from the top of the hierarchy down by a margin.')),
    ];
  }

  function progressBar(e) {
    const job = e.job ? app.jobs.get(e.job) : null;
    return h('div.seg-running', h('div', h('b', 'Labeling the cells'), ' ', h('span.muted', job?.message ?? 'Starting…')), h('div.progress', h('div', { style: { width: `${Math.round((job?.progress ?? 0) * 100)}%` } })));
  }

  // Saves a chosen set into the project when it is not (a starter, as version 1), answering its record.
  async function ensureSaved(chosen) {
    if (chosen.record) return chosen.record;
    const checked = await app.bundles.checkMarkers(chosen.set);
    const next = saveMarkerSet(app.store.project, { set: checked.set, checksum: checked.checksum, source: { starter: chosen.starter.id } });
    if (next !== app.store.project) app.store.commit(next, `Add the marker set ${checked.set.name}`);
    return markerSetsOf(app.store.project).find((r) => r.checksum === checked.checksum);
  }

  async function run(chosen) {
    const problem = checkLabelParameters(st.params);
    if (problem) {
      toast(problem, { kind: 'error' });
      return;
    }
    try {
      const record = await ensureSaved(chosen);
      const m = await labelClustering(app, ctx, { markers: record.id, parameters: { ...st.params } });
      ctx.render(true);
      const s = m.summary ?? {};
      toast(`Labeled: ${formatCount(s.labeled?.[0] ?? 0)} of ${formatCount(s.cells ?? 0)} cells at the ${String(s.levels?.[0] ?? 'top').toLowerCase()} level.`, { kind: 'ok' });
    } catch (error) {
      if (error.missing) toast(`The panel lacks some markers: ${Object.entries(error.missing).map(([l, g]) => `${l} (${g.join(', ')})`).join('; ')}.`, { kind: 'error', timeout: 12000 });
      else toast(`The labels: ${error.message}`, { kind: 'error', timeout: 12000 });
    }
  }

  async function dropMissing(chosen) {
    try {
      const { set, removed, dropped } = withoutMissing(chosen.set, chosen.missing);
      const checked = await app.bundles.checkMarkers(set);
      const family = chosen.record?.family ?? null;
      const next = saveMarkerSet(app.store.project, { set: checked.set, checksum: checked.checksum, family, source: chosen.starter ? { starter: chosen.starter.id } : { edited: true } });
      if (next !== app.store.project) app.store.commit(next, `Remove the markers ${t.dataset.name}'s panel lacks from ${set.name}`);
      st.choice = `set:${markerSetsOf(app.store.project).find((r) => r.checksum === checked.checksum)?.id}`;
      ctx.render(true);
      toast(`Removed ${removed.reduce((n, r) => n + r.genes.length, 0)} markers${dropped.length ? `, and ${dropped.join(', ')}, left without one` : ''}.`, { kind: 'ok', timeout: 8000 });
    } catch (error) {
      toast(`The marker set: ${error.message}`, { kind: 'error', timeout: 10000 });
    }
  }

  function importFile() {
    const input = h('input', { type: 'file', accept: '.json,application/json', style: { display: 'none' } });
    input.addEventListener('change', async () => {
      const file = input.files?.[0];
      input.remove();
      if (!file) return;
      try {
        const checked = await app.bundles.checkMarkers(JSON.parse(await file.text()));
        const known = markerSetsOf(app.store.project).find((r) => r.name === checked.set.name);
        const next = saveMarkerSet(app.store.project, { set: checked.set, checksum: checked.checksum, family: known?.family ?? null, source: { file: file.name } });
        if (next !== app.store.project) app.store.commit(next, `Read the marker set ${checked.set.name}`);
        st.choice = `set:${markerSetsOf(app.store.project).find((r) => r.checksum === checked.checksum)?.id}`;
        ctx.render(true);
        toast(`${file.name}: the marker set ${checked.set.name}${known ? `, a new version of the project's` : ''}.`, { kind: 'ok' });
      } catch (error) {
        toast(`${file.name}: ${error.message}`, { kind: 'error', timeout: 10000 });
      }
    });
    document.body.append(input);
    input.click();
  }

  // --- The editor: a working copy, saved as a new version ---
  function edit(chosen) {
    st.editing = { set: structuredClone(chosen.set), family: chosen.record?.family ?? null, starter: chosen.starter?.id ?? null, base: chosen.record?.version ?? null, problem: null };
    ctx.render(true);
  }

  function editor(offer) {
    const e = st.editing;
    const set = e.set;
    const panel = new Set(offer.genes.map((g) => g.toUpperCase()));
    const redraw = () => ctx.render(true);
    const genesField = (l, key, isState, label) => {
      const input = h('input.input.small.lb-genes', { type: 'text', value: (l[key] ?? []).join(', '), 'aria-label': `${l.name}: ${label}`, list: 'lb-panel', 'data-genes': `${l.name}|${key}` });
      const lacking = (l[key] ?? []).filter((g) => !panel.has(g.toUpperCase()));
      const note = h('span.lb-lacking', lacking.length ? `not in the panel: ${lacking.join(', ')}` : '');
      input.addEventListener('change', () => {
        e.set = changeLabel(e.set, l.name, { [key]: parseGenes(input.value) }, { state: isState });
        const now = (isState ? e.set.states : e.set.labels).find((x) => x.name === l.name);
        const still = (now?.[key] ?? []).filter((g) => !panel.has(g.toUpperCase()));
        note.textContent = still.length ? `not in the panel: ${still.join(', ')}` : '';
      });
      return h('td', input, note);
    };
    const nameField = (l, isState, depth = 0) => {
      const input = h('input.input.small', { type: 'text', value: l.name, 'aria-label': isState ? 'The state\'s name' : 'The label\'s name', style: { marginLeft: `${depth * 18}px` } });
      input.addEventListener('change', () => {
        const name = input.value.trim();
        if (!name || name === l.name) return;
        e.set = changeLabel(e.set, l.name, { name }, { state: isState });
        redraw();
      });
      return h('td', input);
    };
    const tree = labelTree(set);
    const rows = tree.map(({ label: l, depth }) => h('tr',
      nameField(l, false, depth),
      h('td.muted', set.levels[depth] ?? '—'),
      genesField(l, 'positive', false, 'positive markers'),
      genesField(l, 'negative', false, 'negative markers'),
      h('td.lb-actions',
        depth + 1 < set.levels.length ? h('button.icon-button', { type: 'button', title: `Add a label under ${l.name}`, 'aria-label': `Add a label under ${l.name}`, onclick: () => { e.set = addLabel(e.set, l.name); redraw(); } }, icon('plus')) : null,
        h('button.icon-button', { type: 'button', title: `Remove ${l.name} and the labels under it`, 'aria-label': `Remove ${l.name}`, onclick: () => { e.set = removeLabel(e.set, l.name); redraw(); } }, icon('trash')))));
    const stateRows = (set.states ?? []).map((s) => {
      const of = h('select.input.small', { 'aria-label': `The cells ${s.name} applies to` }, h('option', { value: '' }, 'any cell'), ...tree.map(({ label: l }) => h('option', { value: l.name }, l.name)));
      of.value = s.parent ?? '';
      of.addEventListener('change', () => {
        e.set = changeLabel(e.set, s.name, { parent: of.value }, { state: true });
      });
      return h('tr', nameField(s, true), h('td', of), genesField(s, 'positive', true, 'positive markers'), genesField(s, 'negative', true, 'negative markers'),
        h('td.lb-actions', h('button.icon-button', { type: 'button', title: `Remove ${s.name}`, 'aria-label': `Remove ${s.name}`, onclick: () => { e.set = { ...e.set, states: e.set.states.filter((x) => x.name !== s.name) }; redraw(); } }, icon('trash'))));
    });
    const nameInput = h('input.input', { type: 'text', id: 'lb-name', value: set.name });
    nameInput.addEventListener('change', () => {
      e.set = { ...e.set, name: nameInput.value };
    });
    const levelsInput = h('input.input', { type: 'text', id: 'lb-levels', value: set.levels.join(', '), title: 'The levels\' names, the top first, separated by commas (at most 6)' });
    levelsInput.addEventListener('change', () => {
      e.set = { ...e.set, levels: levelsInput.value.split(',').map((x) => x.trim()).filter(Boolean) };
      redraw();
    });
    const save = async () => {
      try {
        const checked = await app.bundles.checkMarkers(e.set);
        const next = saveMarkerSet(app.store.project, { set: checked.set, checksum: checked.checksum, family: e.family, source: { edited: true } });
        if (next === app.store.project) {
          toast('No change to save.', { kind: 'info' });
        } else {
          app.store.commit(next, `Edit the marker set ${checked.set.name}`);
        }
        const saved = markerSetsOf(app.store.project).find((r) => r.checksum === checked.checksum);
        st.choice = saved ? `set:${saved.id}` : st.choice;
        st.editing = null;
        ctx.render(true);
      } catch (error) {
        e.problem = error.message;
        redraw();
      }
    };
    return [
      h('div.seg-card.lb-editor',
        h('p', e.base ? `Editing version ${e.base} of ${set.name}: saving makes version ${e.base + 1}, and the earlier versions, and the labels made from them, are kept.` : 'Editing a new marker set: saving adds it to the project as version 1.'),
        h('div.seg-import-grid', h('label.field', { for: 'lb-name' }, h('span', 'Name'), nameInput), h('label.field', { for: 'lb-levels' }, h('span', 'Levels'), levelsInput)),
        h('datalist', { id: 'lb-panel' }, ...offer.genes.map((g) => h('option', { value: g }))),
        h('div.table-wrap', h('table.data-table.seg-table.lb-edit-table',
          h('thead', h('tr', h('th', 'Label'), h('th', 'Level'), h('th', 'Positive markers'), h('th', 'Negative markers'), h('th', ''))),
          h('tbody', ...rows))),
        h('div.btn-row', h('button.btn.small', { type: 'button', onclick: () => { e.set = addLabel(e.set); redraw(); } }, icon('plus'), `Add a label at the ${String(set.levels[0] ?? 'top').toLowerCase()} level`)),
        h('div.section-title', { style: { marginTop: '10px' } }, 'States'),
        h('p.muted.seg-note', 'A state (proliferating, say) is carried besides a label, where its markers\' score reaches the state threshold; of one label\'s cells only, when it names one.'),
        stateRows.length ? h('div.table-wrap', h('table.data-table.seg-table.lb-edit-table', h('thead', h('tr', h('th', 'State'), h('th', 'Of'), h('th', 'Positive markers'), h('th', 'Negative markers'), h('th', ''))), h('tbody', ...stateRows))) : null,
        h('div.btn-row', h('button.btn.small', { type: 'button', onclick: () => {
          const taken = new Set([...e.set.labels, ...(e.set.states ?? [])].map((l) => l.name.toLowerCase()));
          let name = 'New state';
          for (let k = 2; taken.has(name.toLowerCase()); k += 1) name = `New state ${k}`;
          e.set = { ...e.set, states: [...(e.set.states ?? []), { name, positive: [] }] };
          redraw();
        } }, icon('plus'), 'Add a state')),
        h('p.muted.seg-note', 'Markers are gene names, separated by commas; the panel\'s genes are offered as you type. A label needs a positive marker; a gene is either positive or negative. The program checks the set when it is saved.'),
        e.problem ? h('div.lb-missing', icon('warning'), h('span', e.problem)) : null,
        h('div.btn-row', h('button.btn.primary', { type: 'button', onclick: save }, icon('save'), e.base ? `Save as version ${e.base + 1}` : 'Save'), h('button.btn', { type: 'button', onclick: () => { st.editing = null; ctx.render(true); } }, 'Cancel'))),
    ];
  }

  // --- Labels from a reference ---
  function referenceCard() {
    st.ref ??= { path: '', read: null, spec: null, params: null, busy: false, problem: null, open: false };
    const rf = st.ref;
    if (!rf.params) {
      referenceDefaults().then((d) => {
        rf.params ??= { ...d.parameters };
        ctx.render(true);
      }).catch((error) => toast(`The reference's defaults: ${error.message}`, { kind: 'error' }));
      return null;
    }
    if (rf.read && rf.read.clustering !== clustering.id) Object.assign(rf, { read: null, spec: null, problem: null });
    const running = app.bundles.referenceMappings(t.dataset.id).filter((e) => e.clustering === clustering.id);
    const pathInput = h('input.input', { type: 'text', id: 'lb-ref-path', value: rf.path, placeholder: '/data/reference.zarr', 'aria-label': 'The reference: its path' });
    pathInput.addEventListener('change', () => {
      rf.path = pathInput.value.trim();
    });
    const read = async (spec = null) => {
      rf.path = pathInput.value.trim() || rf.path;
      if (!rf.path) {
        toast('Name a reference: its path.', { kind: 'error' });
        return;
      }
      rf.busy = true;
      rf.problem = null;
      ctx.render(true);
      try {
        const answer = await app.bundles.readReference(t.item, { path: rf.path, clustering: clustering.id, ...(spec ?? {}), parameters: { ...rf.params } });
        rf.read = { ...answer, clustering: clustering.id };
        rf.spec = answer.preview?.spec ?? { ...answer.description.suggested, ...(spec ?? {}) };
      } catch (error) {
        rf.read = null;
        rf.problem = error.message;
      }
      rf.busy = false;
      ctx.render(true);
    };
    const doMap = async () => {
      const problem = checkReferenceParameters(rf.params);
      if (problem) {
        toast(problem, { kind: 'error' });
        return;
      }
      try {
        const m = await referenceClustering(app, ctx, { ...rf.spec, path: rf.path, parameters: { ...rf.params } });
        Object.assign(rf, { read: null, spec: null, problem: null });
        ctx.render(true);
        const s = m.summary ?? {};
        toast(`Labeled from ${s.reference?.file ?? 'the reference'}: ${formatCount(s.labeled?.[0] ?? 0)} of ${formatCount(s.cells ?? 0)} cells.`, { kind: 'ok' });
      } catch (error) {
        rf.problem = error.message;
        ctx.render(true);
      }
    };
    const parts = [
      h('p', 'A labeled single-cell reference: an AnnData object in Zarr (a .zarr folder, or a zip of one) whose cells carry their labels in obs, or a table of each label\'s mean expression (CSV, TSV or Parquet; a row per label, or a row per gene). Each cell is correlated with each label\'s profile over the genes that set the labels apart, then again on the leading labels\' own genes until one is left. An .h5ad file is read once written as Zarr (anndata\'s write_zarr).'),
      h('div.lb-import-row', h('label.field', { for: 'lb-ref-path' }, h('span', 'Reference'), pathInput), browseButton(pathInput, { title: 'A labeled single-cell reference' }), h('button.btn', { type: 'button', disabled: rf.busy, 'data-action': 'read-reference', onclick: () => read() }, icon('file'), rf.busy ? 'Reading…' : 'Read')),
    ];
    if (rf.read) parts.push(...referenceReadout(rf, read, doMap));
    if (rf.problem) parts.push(h('div.lb-missing', icon('warning'), h('span', rf.problem)));
    parts.push(...running.map((e) => {
      const job = e.job ? app.jobs.get(e.job) : null;
      return h('div.seg-running', h('div', h('b', 'Labeling from the reference'), ' ', h('span.muted', job?.message ?? 'Starting…')), h('div.progress', h('div', { style: { width: `${Math.round((job?.progress ?? 0) * 100)}%` } })));
    }));
    parts.push(h('p.muted.seg-cite', 'Method: each cell\'s Spearman correlation with each label\'s profile over the genes most higher in one label than another, fine-tuned on the leading labels\' genes (Aran et al. 2019, Nat Immunol 20:163–172).'));
    return h('section.seg-section', h('div.section-title', 'Labels from a reference'), h('div.seg-card.lb-import.lb-reference', ...parts));
  }

  // What the program read of a reference: its matrix, label and gene columns, and how it meets the
  // panel; the mapping's parameters.
  function referenceReadout(rf, read, doMap) {
    const { description: d, preview: pv, error } = rf.read;
    const spec = rf.spec ?? {};
    const select = (id, label, options, value, onchange) => {
      const el = h('select.input', { id }, ...options.map((o) => h('option', { value: o.value }, o.label)));
      el.value = value ?? '';
      el.addEventListener('change', () => onchange(el.value));
      return h('label.field', { for: id }, h('span', label), el);
    };
    const anndata = d.format === 'anndata';
    const fields = [];
    if (anndata) {
      fields.push(select('lb-ref-matrix', 'Expression', d.matrices.map((m) => ({ value: m.name, label: `${m.name}: ${m.values} (${exact(m.rows)} × ${exact(m.cols)})` })), spec.matrix, (v) => read({ ...spec, matrix: v })));
      fields.push(select('lb-ref-label', 'Labels (obs)', d.labels.map((c) => ({ value: c.name, label: `${c.name} (${exact(c.distinct)} labels; ${c.examples.slice(0, 3).join(', ')})` })), spec.labelColumn, (v) => read({ ...spec, labelColumn: v })));
      fields.push(select('lb-ref-gene', 'Gene names (var)', d.genes.map((g) => ({ value: g.name, label: `${g.name || 'the index'} (${exact(g.shared)} of the panel's genes)` })), spec.geneColumn, (v) => read({ ...spec, geneColumn: v })));
    } else {
      const orientation = [{ value: '', label: `A row per label: the columns are genes (${exact(d.columnGenes)} of the panel's)` }, ...d.genes.map((g) => ({ value: g.name, label: `A row per gene, named in ${g.name} (${exact(g.shared)} of the panel's)` }))];
      fields.push(select('lb-ref-gene', 'Layout', orientation, spec.geneColumn ?? '', (v) => read({ path: rf.path, geneColumn: v, labelColumn: v ? '' : d.labels[0]?.name ?? '' })));
      if (!spec.geneColumn) fields.push(select('lb-ref-label', 'Labels', d.labels.map((c) => ({ value: c.name, label: `${c.name} (${exact(c.distinct)} values; ${c.examples.slice(0, 3).join(', ')})` })), spec.labelColumn, (v) => read({ ...spec, geneColumn: '', labelColumn: v })));
    }
    const inputs = REFERENCE_PARAMETERS.map((p) => {
      const input = h('input.input', { type: 'number', id: `lb-ref-${p.id}`, min: String(p.min), max: String(p.max), step: String(p.step), value: String(rf.params[p.id]), title: p.help });
      input.addEventListener('change', () => {
        rf.params[p.id] = Number(input.value);
      });
      return h('label.field', { for: `lb-ref-${p.id}`, title: p.help }, h('span', p.label), input);
    });
    const out = [
      h('p.muted.seg-note', `${d.file}: ${anndata ? `an AnnData object of ${exact(d.rows)} cells` : `a table of profiles, ${exact(d.rows)} rows`}.`),
      h('div.seg-import-grid', ...fields),
    ];
    if (pv) {
      const shownLabels = pv.labels.slice(0, 14).map((name, k) => (pv.cells[k] ? `${name} (${exact(pv.cells[k])})` : name)).join(', ');
      const few = pv.labels.filter((_, k) => pv.cells[k] > 0 && pv.cells[k] < 10);
      const sharedShare = pv.shared / (pv.shared + pv.missing.length);
      out.push(h('dl.kv.seg-kv',
        h('dt', 'Labels'), h('dd', `${exact(pv.labels.length)}: ${shownLabels}${pv.labels.length > 14 ? ', …' : ''}`),
        h('dt', 'Genes'), h('dd', `${exact(pv.shared)} of the panel's ${exact(pv.shared + pv.missing.length)} are in the reference (of its ${exact(pv.refGenes)})`, pv.missing.length ? h('span.muted', { title: pv.missing.join(', ') }, `; it lacks ${pv.missing.slice(0, 8).join(', ')}${pv.missing.length > 8 ? `, and ${pv.missing.length - 8} more` : ''}`) : null),
        h('dt', 'Compared'), h('dd', `${exact(pv.compared)} genes in the first round (up to ${exact(pv.perPair)} per pair of labels)`),
        h('dt', 'Values'), h('dd', pv.values === 'counts' ? 'counts: each reference cell scaled to the median total, then log1p, and averaged by label' : pv.values === 'as given' ? 'each label\'s profile as given' : 'log-normalized, averaged by label as given')),
      );
      if (sharedShare < 0.5) out.push(h('div.lb-missing', icon('warning'), h('span', `The reference has only ${percent(sharedShare)} of the panel's genes: check the gene names column (symbols, not ids), or the species.`)));
      if (few.length) out.push(h('p.muted.seg-note', `${few.length} label${few.length === 1 ? ' has' : 's have'} fewer than 10 reference cells (${few.slice(0, 5).join(', ')}${few.length > 5 ? ', …' : ''}): their profiles are noisy.`));
    }
    if (error) out.push(h('div.lb-missing', icon('warning'), h('span', error)));
    out.push(h('details.seg-advanced', { open: rf.open, ontoggle: (event) => { rf.open = event.currentTarget.open; } },
      h('summary', 'Parameters'),
      h('div.seg-import-grid', ...inputs),
      h('p.muted.seg-note', 'Hover a parameter for what it does. The fine-tuning threshold and the genes per pair are the published defaults; the margin is SituCraft\'s.')));
    out.push(h('div.btn-row', h('button.btn.primary', { type: 'button', 'data-action': 'map-reference', disabled: !pv || Boolean(error), onclick: doMap }, icon('play'), 'Label the cells from the reference')));
    return out;
  }

  // --- Labels made elsewhere, imported for the segmentation's cells ---
  function importCard() {
    const seg = ctx.seg;
    if (!seg) return null;
    st.imp ??= { path: '', read: null, roles: null, instrument: '', busy: false, problem: null };
    const im = st.imp;
    if (im.read && im.read.segmentation?.id !== seg.id) Object.assign(im, { read: null, roles: null, problem: null });
    const instrument = seg.source === 'instrument';
    const analysis = instrument ? instrumentClusterings(t.item, () => ctx.render(true)) : null;
    const running = app.bundles.labelImports(t.dataset.id).filter((e) => e.segmentation === seg.id);
    const pathInput = h('input.input', { type: 'text', id: 'lb-import-path', value: im.path, placeholder: '/data/annotations.csv', 'aria-label': 'The table of labels: its path' });
    pathInput.addEventListener('change', () => {
      im.path = pathInput.value.trim();
    });
    const read = async (columns = null) => {
      im.path = pathInput.value.trim();
      if (!im.path) {
        toast('Name a table of labels: its path.', { kind: 'error' });
        return;
      }
      im.busy = true;
      im.problem = null;
      ctx.render(true);
      try {
        im.read = await app.bundles.readLabelsTable(t.item, { path: im.path, segmentation: seg.id, ...(columns ? { columns } : {}) });
        im.roles = { ...im.read.roles, ...(columns ? { id: columns.idColumn } : {}) };
      } catch (error) {
        im.read = null;
        im.problem = error.message;
      }
      im.busy = false;
      ctx.render(true);
    };
    const doImport = async (request) => {
      try {
        const m = await app.bundles.importLabels(t.item, t.dataset.id, request);
        st.shown = m.id;
        Object.assign(im, { read: null, roles: null, problem: null });
        ctx.render(true);
        toast(`Imported ${m.summary?.labels ?? '?'} labels for ${formatCount(m.summary?.labeled?.[0] ?? 0)} of ${formatCount(m.summary?.cells ?? 0)} cells.`, { kind: 'ok' });
      } catch (error) {
        im.problem = error.message;
        ctx.render(true);
      }
    };
    const parts = [
      h('p', `Labels from another tool or an earlier analysis, matched to the cells of ${seg.name.toLowerCase()} by their ids: a CSV or TSV table, a Parquet file, an AnnData object's obs in Zarr (or a SpatialData store's table), or Xenium Explorer's cell groups (cell_id, group, color). A table naming cells this segmentation does not have is refused; its cells the table does not name are counted.`),
      h('div.lb-import-row', h('label.field', { for: 'lb-import-path' }, h('span', 'Table'), pathInput), browseButton(pathInput, { title: 'A table of labels' }), h('button.btn', { type: 'button', disabled: im.busy, 'data-action': 'read-labels', onclick: () => read() }, icon('file'), im.busy ? 'Reading…' : 'Read')),
    ];
    if (im.read) parts.push(...readout(im, seg, read, doImport));
    if (im.problem) parts.push(h('div.lb-missing', icon('warning'), h('span', im.problem)));
    if (analysis?.length) {
      const pick = h('select.input', { id: 'lb-instrument', 'aria-label': 'The instrument\'s clusters' }, ...analysis.map((c) => h('option', { value: c.name }, `${c.title} (${c.clusters} clusters)`)));
      if (im.instrument) pick.value = im.instrument;
      pick.addEventListener('change', () => {
        im.instrument = pick.value;
      });
      parts.push(h('div.lb-import-row', h('label.field', { for: 'lb-instrument' }, h('span', 'Or the instrument\'s clusters'), pick),
        h('button.btn', { type: 'button', onclick: () => doImport({ instrument: pick.value, segmentation: seg.id }) }, icon('upload'), 'Import as labels')));
    }
    parts.push(...running.map((e) => {
      const job = e.job ? app.jobs.get(e.job) : null;
      return h('div.seg-running', h('div', h('b', 'Importing labels'), ' ', h('span.muted', job?.message ?? 'Starting…')), h('div.progress', h('div', { style: { width: `${Math.round((job?.progress ?? 0) * 100)}%` } })));
    }));
    return h('section.seg-section', h('div.section-title', 'Labels made elsewhere'), h('div.seg-card.lb-import', ...parts));
  }

  // What the program read of a table: its columns for each role, how its ids match, its labels.
  function readout(im, seg, read, doImport) {
    const r = im.read;
    const roles = im.roles;
    const text = r.columns.filter((c) => c.kind === 'text');
    const numbers = r.columns.filter((c) => c.kind === 'number');
    const pick = (id, label, list, value, optional, onchange) => {
      const el = h('select.input', { id }, ...(optional ? [h('option', { value: '' }, 'none')] : []), ...list.map((c) => h('option', { value: c.name }, `${c.name} (${c.distinct.toLocaleString('en-US')} values${c.missing ? `, ${c.missing.toLocaleString('en-US')} missing` : ''}; ${c.examples.slice(0, 3).join(', ')})`)));
      el.value = value ?? '';
      el.addEventListener('change', () => onchange(el.value));
      return h('label.field', { for: id }, h('span', label), el);
    };
    const m = r.match;
    const labelCol = r.columns.find((c) => c.name === roles.label);
    const refused = m && (m.notInSegmentation > 0 || m.duplicates > 0);
    const matchText = !m ? 'Choose the column of the cells\' ids.'
      : refused ? `${m.notInSegmentation ? `${m.notInSegmentation.toLocaleString('en-US')} of the ${m.rows.toLocaleString('en-US')} rows name cells ${seg.name.toLowerCase()} does not have (${m.examples.join(', ')})` : ''}${m.notInSegmentation && m.duplicates ? '; ' : ''}${m.duplicates ? `${m.duplicates.toLocaleString('en-US')} name a cell another row names (${m.duplicateExamples.join(', ')})` : ''}: the table cannot be imported for these cells.`
        : `All ${m.matched.toLocaleString('en-US')} rows name cells of ${seg.name.toLowerCase()}; ${m.cellsNotNamed.toLocaleString('en-US')} of its ${m.cells.toLocaleString('en-US')} cells are not named, and will have no label.`;
    const others = (r.others ?? []).filter((o) => o.notInSegmentation === 0);
    return [
      h('p.muted.seg-note', `${r.file}: ${r.format === 'xenium-explorer' ? 'Xenium Explorer\'s cell groups' : r.format === 'anndata' ? `an AnnData obs (${r.path})` : r.format === 'parquet' ? 'Parquet' : 'a table'}, ${r.rows.toLocaleString('en-US')} rows, ${r.columns.length} columns; SHA-256 ${r.sha256.slice(0, 12)}.`),
      h('div.seg-import-grid',
        pick('lb-col-id', 'The cells\' ids', r.columns, roles.id, false, (v) => read({ idColumn: v, column: roles.label })),
        pick('lb-col-label', 'Their labels', text.concat(numbers), roles.label, false, (v) => {
          roles.label = v;
          ctx.render(true);
        }),
        pick('lb-col-confidence', 'Confidence', numbers, roles.confidence, true, (v) => {
          roles.confidence = v;
        }),
        pick('lb-col-color', 'Colors', text, roles.color, true, (v) => {
          roles.color = v;
        })),
      labelCol ? h('p.muted.seg-note', `${labelCol.name}: ${labelCol.distinct.toLocaleString('en-US')} labels (${labelCol.examples.join(', ')}${labelCol.distinct > labelCol.examples.length ? ', …' : ''})${labelCol.missing ? `; ${labelCol.missing.toLocaleString('en-US')} rows without one` : ''}.`) : null,
      h(refused ? 'div.lb-missing' : 'p.seg-note', refused ? icon('warning') : null, h('span', matchText)),
      others.length ? h('div.btn-row', h('span.muted', 'Its ids are the cells of '), ...others.map((o) => h('button.btn.small', { type: 'button', onclick: () => {
        ctx.session.segmentation = o.id;
        ctx.session.shown = null;
        ctx.render(true);
      } }, o.name))) : null,
      h('div.btn-row', h('button.btn.primary', { type: 'button', 'data-action': 'import-labels', disabled: refused || !m || !roles.label || roles.label === roles.id, onclick: () => doImport({ path: im.path, segmentation: seg.id, columns: { idColumn: roles.id, column: roles.label, confidenceColumn: roles.confidence || '', colorColumn: roles.color || '' } }) }, icon('upload'), 'Import the labels')),
    ];
  }

  // --- The label sets the clustering's cells carry ---
  function listCard() {
    const list = labelSetsFor(app.store.project, clustering).filter((r) => r.state === 'complete');
    if (!list.length) return null;
    if (!list.some((r) => r.id === st.shown)) st.shown = list.at(-1).id;
    const project = app.store.project;
    const rows = list.map((r) => {
      const s = r.summary ?? {};
      const ms = project.artifacts.find((a) => a.kind === 'marker-set' && a.id === r.markerSet);
      const choose = () => {
        st.shown = r.id;
        st.level = Math.min(st.level, (s.levels?.length ?? 1) - 1);
        ctx.render(true);
      };
      return h(`tr${st.shown === r.id ? '.selected' : ''}`, { tabIndex: 0, style: { cursor: 'pointer' }, 'data-labels': r.id, onclick: choose, onkeydown: (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          choose();
        }
      } },
      h('td', oneLevel(r) ? labelSetName(r) : r.markers?.name ?? '?', ms ? h('span.muted', ` v${ms.version}`) : null, r.source ? h('span.muted', ' (imported)') : r.reference ? h('span.muted', ' (reference)') : null),
      h('td.r', { title: r.reference ? 'The margin, and the fine-tuning threshold' : null }, r.source ? '—' : r.reference ? `${r.parameters?.margin ?? '—'} (${r.parameters?.tune ?? '—'})` : String(r.parameters?.margin ?? '—')),
      ...(s.levels ?? []).slice(0, 3).map((_, l) => h('td.r', percent((s.labeled?.[l] ?? 0) / (s.cells || 1)))),
      ...Array.from({ length: Math.max(0, 3 - (s.levels?.length ?? 0)) }, () => h('td.r', '')),
      h('td.muted', relativeTime(r.created)),
      h('td', h('button.icon-button', { type: 'button', title: 'Show the labels on the tissue in the Viewer', 'aria-label': 'Show the labels on the tissue in the Viewer', onclick: (event) => {
        event.stopPropagation();
        app.showLabels(r, st.shown === r.id ? st.level : 0);
      } }, icon('tissue'))));
    });
    return h('section.seg-section',
      h('div.section-title', `Label sets of ${clustering.name.charAt(0).toLowerCase()}${clustering.name.slice(1)}`),
      h('div.table-wrap', h('table.data-table.seg-table',
        h('thead', h('tr', h('th', 'Labels'), h('th.r', 'Margin'), h('th.r', { title: 'The share of the cells labeled at the first level' }, 'Level 1'), h('th.r', 'Level 2'), h('th.r', 'Level 3'), h('th', 'Made'), h('th', ''))),
        h('tbody', ...rows))),
      remakeRow());
  }

  // The clustering and its label sets made again on another of the dataset's segmentations.
  function remakeRow() {
    if (clustering.summary?.source) return null;
    const others = app.store.project.artifacts.filter((a) => a.kind === 'segmentation' && a.dataset === t.dataset.id && a.state === 'complete' && a.id !== clustering.segmentation && a.source !== 'planted');
    if (!others.length) return null;
    st.remakeTo = others.some((o) => o.id === st.remakeTo) ? st.remakeTo : others.at(-1).id;
    const pick = h('select.input', { id: 'lb-remake-seg' }, ...others.map((o) => h('option', { value: o.id }, o.name)));
    pick.value = st.remakeTo;
    pick.addEventListener('change', () => {
      st.remakeTo = pick.value;
    });
    const going = remaking(clustering.id);
    const run = async () => {
      const target = others.find((o) => o.id === st.remakeTo);
      try {
        const out = await remakeOnSegmentation(app, t, clustering.id, st.remakeTo, () => ctx.render(true));
        const made = `${out.labels.length} label set${out.labels.length === 1 ? '' : 's'}`;
        const problems = [...out.failed.map((f) => f.error), ...out.skipped.map((x) => `${labelSetName(app.store.project.artifacts.find((a) => a.id === x.id) ?? {})}: not made again (${x.why})`)];
        toast(`Made again on ${target.name}: ${out.clustering.summary?.clusters ?? '?'} clusters and ${made}.${problems.length ? ` ${problems.join('; ')}.` : ''} Compare the labels in the Compare view.`, { kind: problems.length ? 'info' : 'ok', timeout: 12000 });
      } catch (error) {
        toast(`Made again: ${error.message}`, { kind: 'error', timeout: 12000 });
      }
      ctx.render(true);
    };
    return h('div.lb-remake',
      h('div.lb-import-row', h('label.field', { for: 'lb-remake-seg', title: 'With the same parameters: the clustering, its map, and each label set from markers or a reference. Then compare the labels under the two segmentations in the Compare view.' }, h('span', 'Make this clustering and its label sets again on'), pick),
        h('button.btn', { type: 'button', 'data-action': 'remake', disabled: going.length > 0, onclick: run }, icon('copy'), 'Make again')),
      ...going.map((g) => h('div.seg-running', h('div', h('b', `Making again on ${others.find((o) => o.id === g.segmentation)?.name ?? 'another segmentation'}`), ' ', h('span.muted', g.step)))));
  }

  // --- The label set shown ---
  function shownCard() {
    const r = labelSetsFor(app.store.project, clustering).find((x) => x.id === st.shown && x.state === 'complete');
    if (!r) return null;
    const box = h('section.seg-section.lb-shown', h('div.section-title', 'The labels'), h('p.muted.seg-note', 'Reading the labels…'));
    Promise.all([app.bundles.labelData(r.id), r.source ? app.bundles.clusteringData(clustering.id) : null]).then(([data, cd]) => {
      if (!box.isConnected && !document.body.contains(box)) return;
      // Imported labels are of the segmentation's cells: each cluster's counted here.
      const d = cd ? { ...data.description, clusters: cd.sizes.length, composition: compositionOf(data, cd.cluster, cd.sizes.length) } : data.description;
      clear(box).append(h('div.section-title', 'The labels'), ...shown(r, data, d));
    }).catch((error) => clear(box).append(h('div.section-title', 'The labels'), h('p.muted.seg-note', `The labels could not be read: ${error.message}`)));
    return box;
  }

  function shown(r, data, d) {
    const sets = d.sets;
    const src = r.source;
    const ref = d.reference;
    const levels = d.levels;
    const level = Math.min(st.level, levels.length - 1);
    const n = d.cells.analyzed;
    const project = app.store.project;
    const ms = project.artifacts.find((a) => a.kind === 'marker-set' && a.id === r.markerSet);
    const names = clusterNames(project, clustering.id);
    const unlabeled = sets.length;
    const colorBy = (color) => {
      ctx.cellMap?.setColor(color);
      document.querySelector('.ct-map-host')?.scrollIntoView({ block: 'nearest' });
    };
    const levelButtons = levels.length > 1 ? h('div.lb-levels', { role: 'group', 'aria-label': 'Level' }, ...levels.map((name, l) => h(`button.btn.small${l === level ? '.primary' : ''}`, { type: 'button', 'aria-pressed': String(l === level), onclick: () => {
      st.level = l;
      ctx.render(true);
    } }, name))) : null;
    const kv = ref
      ? h('dl.kv.seg-kv',
        h('dt', 'Reference'), h('dd', `${ref.file}${ref.labelColumn ? `, labels from ${ref.labelColumn}` : ''}${ref.matrix ? `, ${ref.matrix}` : ''}`, h('span.muted.mono', ` ${String(ref.sha256).slice(0, 12)}`)),
        h('dt', 'Genes'), h('dd', `${exact(ref.shared.length)} of the panel's genes shared`, ref.missing.length ? h('span.muted', { title: ref.missing.join(', ') }, ` (it lacks ${exact(ref.missing.length)})`) : null, `; ${exact(ref.compared.length)} compared in the first round, up to ${exact(ref.perPair)} per pair of labels`),
        h('dt', 'Cells labeled'), h('dd', `${exact(d.cells.labeled[0])} (${percent(d.cells.labeled[0] / n)})`, h('span.muted', ` of ${exact(n)}; ${exact(d.cells.unlabeled)} without a label`)),
        h('dt', 'Fine-tuning'), h('dd', `${exact(ref.fineTuned)} cells scored again on their leading labels' genes; ${exact(ref.changed)} took a label other than their first round's best`),
        h('dt', 'Parameters'), h('dd', `fine-tuning within ${ref.parameters.tune}, margin ${ref.parameters.margin}, ${ref.parameters.markers ? `${ref.parameters.markers} genes per pair` : `genes per pair by the labels' number (${ref.perPair})`}`),
        h('dt', 'Artifact'), h('dd.mono', r.id))
      : src
      ? h('dl.kv.seg-kv',
        h('dt', 'Source'), h('dd', src.format === 'instrument' ? `${src.tool} ${src.version}: its ${String(src.title).toLowerCase()} clusters (${src.file})` : `${src.file}, column ${src.column}`, h('span.muted.mono', ` ${String(src.sha256).slice(0, 12)}`)),
        h('dt', 'Matched by'), h('dd', `${src.idColumn}: ${exact(src.rows)} rows`),
        src.confidenceColumn ? h('dt', 'Confidence') : null, src.confidenceColumn ? h('dd', src.confidenceColumn) : null,
        h('dt', 'Cells labeled'), h('dd', `${exact(d.cells.labeled[0])} (${percent(d.cells.labeled[0] / n)})`, h('span.muted', ` of the segmentation's ${exact(n)}; ${exact(d.cells.unlabeled)} without a label`)),
        h('dt', 'Artifact'), h('dd.mono', r.id))
      : h('dl.kv.seg-kv',
        h('dt', 'Marker set'), h('dd', d.markers.name, ms ? ` (version ${ms.version})` : '', h('span.muted.mono', ` ${d.markers.checksum.slice(0, 12)}`)),
        h('dt', 'Cells labeled'), h('dd', levels.map((name, l) => `${name.toLowerCase()} ${exact(d.cells.labeled[l])} (${percent(d.cells.labeled[l] / n)})`).join('; '), h('span.muted', ` of ${exact(n)}; ${exact(d.cells.unlabeled)} without a label`)),
        ...sets.filter((s) => s.state).map((s) => [h('dt', s.name), h('dd', `${exact(s.cells)} cells (${percent(s.cells / n, 1)})`)]).flat(),
        h('dt', 'Parameters'), h('dd', `margin ${d.parameters.margin}, minimum ${d.parameters.minimum}, state threshold ${d.parameters.state}; ${d.parameters.bins} bins, up to ${d.parameters.controls} controls per bin${d.parameters.lineage ? ', the lineage\'s markers left out of each label\'s controls' : ''}`),
        h('dt', 'Artifact'), h('dd.mono', r.id));
    // Each cluster's labels at the level, and names from them.
    const rows = composition(d, level).map((row) => {
      const bar = h('div.ct-composition', { role: 'img', 'aria-label': `${nameOf(names, row.cluster)} by label` }, ...row.labels.map((e) => h('span', { style: { width: `${100 * e.share}%`, background: labelColor(e.label ?? unlabeled, unlabeled, sets) }, title: `${e.name}: ${formatCount(e.count)} (${percent(e.share)})` })));
      const lead = row.labels.slice(0, 3).map((e) => `${e.name} ${percent(e.share)}`).join(', ');
      return h('tr', { 'data-cluster': row.cluster }, h('td', nameOf(names, row.cluster)), h('td.r', exact(row.cells)), h('td.lb-bar', bar), h('td', h('span.ct-genes', lead)));
    });
    const proposal = namesFromLabels(d, st.share);
    const shareInput = h('input.input.small', { type: 'number', id: 'lb-share', min: '0.3', max: '1', step: '0.05', value: String(st.share), title: 'The share of a cluster\'s cells the label must hold' });
    shareInput.addEventListener('change', () => {
      st.share = Math.min(1, Math.max(0.3, Number(shareInput.value) || 0.5));
      ctx.render(true);
    });
    const proposed = proposal.filter(Boolean).length;
    const applyNames = () => {
      const merged = proposal.map((p, k) => p ?? names?.names?.[k] ?? null);
      const next = nameClusters(app.store.project, { clustering: clustering.id, names: merged });
      if (next === app.store.project) {
        toast('The clusters already have these names.', { kind: 'info' });
        return;
      }
      app.store.commit(next, `Name the clusters of ${clustering.name} from ${labelSetName(r)}`);
      toast(`Named ${proposed} clusters from the labels (a new version of the names).`, { kind: 'ok' });
    };
    // An imported label: its color and cells; chosen, its cells selected on the map and the tissue.
    const importedRows = src ? sets.map((s, k) => h('tr', { tabIndex: 0, style: { cursor: 'pointer' }, title: `Select the cells of ${s.name} on the map and the tissue`, onclick: async () => {
      await ctx.cellMap?.setColor({ kind: 'labels', labels: r.id, level: 0 });
      ctx.cellMap?.selectGroups([k]);
      document.querySelector('.ct-map-host')?.scrollIntoView({ block: 'nearest' });
    } },
      h('td', h('span.ct-swatch', { style: { background: labelColor(k, unlabeled, sets) }, 'aria-hidden': 'true' }), s.name),
      h('td.r', exact(s.cells)),
      h('td.r', percent(s.cells / n, 1)))) : [];
    // A reference's label: its reference cells and the cells taking it; chosen, the map colored by
    // each cell's correlation with its profile.
    const refRows = ref ? sets.map((s, k) => h('tr', { tabIndex: 0, style: { cursor: 'pointer' }, title: `Color the map and the tissue by each cell's correlation with ${s.name}'s profile`, onclick: () => colorBy({ kind: 'score', labels: r.id, set: k, name: s.name }) },
      h('td', h('span.ct-swatch', { style: { background: labelColor(k, unlabeled, sets) }, 'aria-hidden': 'true' }), s.name),
      h('td.r', ref.cells[k] ? exact(ref.cells[k]) : '—'),
      h('td.r', exact(s.cells)),
      h('td.r', percent(s.cells / n, 1)))) : [];
    // Each label: its level, cells, markers and controls; chosen, the map colored by its score.
    const labelRows = src || ref ? [] : labelTree({ labels: sets.filter((s) => !s.state).map((s) => ({ name: s.name, parent: s.parent >= 0 ? sets[s.parent].name : undefined })) }).map(({ label, depth }) => {
      const k = sets.findIndex((s) => s.name === label.name && !s.state);
      const s = sets[k];
      return h('tr', { tabIndex: 0, style: { cursor: 'pointer' }, title: `Color the map and the tissue by ${s.name}'s score`, onclick: () => colorBy({ kind: 'score', labels: r.id, set: k, name: s.name }) },
        h('td', h('span', { style: { marginLeft: `${depth * 16}px` } }, h('span.ct-swatch', { style: { background: labelColor(k, unlabeled, sets) }, 'aria-hidden': 'true' }), s.name)),
        h('td.muted', levels[s.level]),
        h('td.r', exact(s.cells)),
        h('td', h('span.ct-genes', s.positive.join(', '))),
        h('td', h('span.ct-genes', (s.negative ?? []).join(', '))),
        h('td.r', { title: `Controls of the positive markers: ${s.positiveControls.join(', ')}${s.negativeControls?.length ? `; of the negative: ${s.negativeControls.join(', ')}` : ''}` }, String(s.positiveControls.length)));
    });
    const stateRows = sets.map((s, k) => [s, k]).filter(([s]) => s.state).map(([s, k]) => h('tr', { tabIndex: 0, style: { cursor: 'pointer' }, onclick: () => colorBy({ kind: 'score', labels: r.id, set: k, name: s.name }) },
      h('td', s.name), h('td.muted', s.parent >= 0 ? `of ${sets[s.parent].name}` : 'any cell'), h('td.r', exact(s.cells)), h('td', h('span.ct-genes', s.positive.join(', '))), h('td', h('span.ct-genes', (s.negative ?? []).join(', '))), h('td.r', String(s.positiveControls.length))));
    return [
      h('div.ct-summary', kv),
      h('div.btn-row', levelButtons,
        h('button.btn.small.primary', { type: 'button', 'data-action': 'color-labels', onclick: () => colorBy({ kind: 'labels', labels: r.id, level }) }, icon('tag'), 'Color the map and the tissue'),
        !src || src.confidenceColumn ? h('button.btn.small', { type: 'button', onclick: () => colorBy({ kind: 'confidence', labels: r.id }) }, 'Confidence') : null,
        h('button.btn.small', { type: 'button', onclick: () => app.showLabels(r, level) }, icon('tissue'), 'Show in the Viewer')),
      h('div.section-title', { style: { marginTop: '12px' } }, src || ref ? 'Each cluster\'s labels' : `Each cluster's labels: ${levels[level].toLowerCase()}`),
      h('div.table-wrap', h('table.data-table.seg-table.lb-composition',
        h('thead', h('tr', h('th', 'Cluster'), h('th.r', 'Cells'), h('th', 'Labels'), h('th', 'Leading'))),
        h('tbody', ...rows))),
      h('p.muted.seg-note', ref ? 'Each cluster\'s cells by the reference\'s label they took ("No label": cells whose best label did not lead by the margin, or that express too few of the genes compared). A cluster split between labels may hold two kinds of cells, or the reference may part cells the panel cannot.' : src ? `The imported labels of each cluster's cells ("No label": cells the table does not label). A cluster split between labels may hold two kinds of cells, or the clusters and the labels may part cells differently.` : 'A cell that takes no label at a level is counted under the label above, where it stopped; "No label" cells took none at the top. A cluster split between labels may hold two kinds of cells, or cells whose outlines hold parts of two.'),
      h('div.lb-naming',
        h('label.field', { for: 'lb-share' }, h('span', 'Name each cluster by the most specific label at least this share of its cells carry'), shareInput),
        h('button.btn.small', { type: 'button', disabled: !proposed, 'data-action': 'name-from-labels', onclick: applyNames }, icon('tag'), `Name ${proposed} cluster${proposed === 1 ? '' : 's'}`),
        h('span.muted', proposed ? `${proposal.map((p, k) => (p ? `${nameOf(null, k)} "${p}"` : null)).filter(Boolean).slice(0, 4).join(', ')}${proposed > 4 ? ', …' : ''}: a new version of the names; clusters without such a label keep theirs.` : 'No cluster has a label held by that share of its cells.')),
      h('div.section-title', { style: { marginTop: '12px' } }, 'Labels'),
      ref ? h('div.table-wrap', h('table.data-table.seg-table.lb-labels',
        h('thead', h('tr', h('th', 'Label'), h('th.r', { title: 'The reference\'s cells of the label, its profile made from them' }, 'Reference cells'), h('th.r', 'Cells'), h('th.r', { title: 'Of the cells labeled' }, 'Share'))),
        h('tbody', ...refRows)))
        : src ? h('div.table-wrap', h('table.data-table.seg-table.lb-labels',
        h('thead', h('tr', h('th', 'Label'), h('th.r', 'Cells'), h('th.r', { title: 'Of the segmentation\'s cells' }, 'Share'))),
        h('tbody', ...importedRows))) : h('div.table-wrap', h('table.data-table.seg-table.lb-labels',
        h('thead', h('tr', h('th', 'Label'), h('th', 'Level'), h('th.r', { title: 'Cells carrying it as their most specific label' }, 'Cells'), h('th', 'Positive markers'), h('th', 'Negative markers'), h('th.r', { title: 'Control genes of its positive markers (hover a row for them)' }, 'Controls'))),
        h('tbody', ...labelRows))),
      stateRows.length ? h('div.table-wrap', h('table.data-table.seg-table.lb-labels', h('thead', h('tr', h('th', 'State'), h('th', 'Of'), h('th.r', 'Cells'), h('th', 'Positive markers'), h('th', 'Negative markers'), h('th.r', 'Controls'))), h('tbody', ...stateRows))) : null,
      h('p.muted.seg-note', ref ? 'Choose a label to color the map and the tissue by each cell\'s correlation with its profile (the first round\'s, over the genes compared). A cell\'s confidence is its label\'s lead over the next, in correlation, in the round that decided it.' : src ? 'Choose a label to select its cells on the map and the tissue.' : 'Choose a label to color the map and the tissue by its score: its markers\' mean expression less their control genes\'. The controls are genes of the markers\' bins of mean expression; on a targeted panel many of them are other types\' markers, so a type that dominates the tissue scores low against its own most expressed genes, and its cells may stay without a label.'),
      h('p.muted.seg-cite', describeLabels(r)),
    ];
  }

  return [host, rest];
}
