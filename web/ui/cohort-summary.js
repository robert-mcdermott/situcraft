// The Cohort view's summaries (wave 7, slice 4): the same steps on every sample (each dataset's
// cells clustered and labeled from one marker set, so that every sample's labels have the same
// names; a graph of each sample's cells and the same questions asked of it), and the cohort table
// (api_cohort.go, lib/cohort.js): each sample's cells by label, its niches' shares (the niches
// found across the samples together), its quality, its graph's and its answers' numbers and its
// pseudobulk, each number traced to the artifact it came from.

import { h, icon, downloadBlob } from './dom.js';
import { toast } from './overlays.js';
import { labelClustering, referenceClustering } from './labels-card.js';
import { pickPaths } from './file-picker.js';
import { labelSource, refOf } from './label-sources.js';
import { promotedSegmentation } from '../lib/segmentations.js';
import { cellsIn } from '../lib/regions.js';
import { cellReviewState, reviewsOf } from '../lib/review-record.js';
import { DEFAULTS as GRAPH_DEFAULTS, KINDS, cellsRequest, chooseCells, graphParams } from '../lib/graphs.js';
import { DISTANCES_DEFAULTS, ENRICHMENT_DEFAULTS, MORAN_DEFAULTS, NICHES_DEFAULTS, QUESTIONS, labelsRequest } from '../lib/spatial.js';
import { markerSetsOf } from '../lib/labels.js';
import { samplesOf } from '../lib/samples.js';
import { SUMMARY_QUESTIONS, addSummary, familyCSV, graphAnalysesOf, graphOfSample, itemIndex, latestByQuestion, readCohortTable, summariesOf, summaryRecord, usedNames, vocabularyOf } from '../lib/cohort.js';

const QUESTION_DEFAULTS = { enrichment: ENRICHMENT_DEFAULTS, distances: DISTANCES_DEFAULTS, moran: MORAN_DEFAULTS };
const questionLabel = (q) => QUESTIONS.find((x) => x.id === q)?.label ?? q;
const safeName = (s) => s.replace(/[^A-Za-z0-9._-]+/g, '_');
const latest = (list) => [...list].sort((a, b) => (a.created < b.created ? 1 : -1))[0] ?? null;

async function getJSON(url, options = {}) {
  const response = await fetch(url, { cache: 'no-store', ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error ?? `${url}: HTTP ${response.status}`);
  return body;
}

let clusterDefaults = null;
async function clusteringDefaults() {
  clusterDefaults ??= getJSON('/api/clustering/defaults').catch((error) => {
    clusterDefaults = null;
    throw error;
  });
  return clusterDefaults;
}

// What the sections choose, for the session: the labels' source (a marker set's id, a reference's
// source, '' for SituCraft's starter set that fits the first sample's panel, or 'path' for the
// reference at referencePath) and its level, the graph's kind, the questions, the niches; the steps
// under way, the summary being made, and the summary shown.
export const cohortSession = {
  markers: '', referencePath: '', level: 0, kind: 'delaunay', questions: new Set(['enrichment']),
  niches: true, nicheCount: NICHES_DEFAULTS.niches, neighbors: NICHES_DEFAULTS.neighbors,
  running: null, summarizing: null, shown: null, family: 'cells',
};

// The tables read, per summary id, and the cells each distances answer is of, per analysis id.
const tables = new Map();
const answerCells = new Map();

// --- Each sample's cells and artifacts ---------------------------------------------------------------

// The samples summarized (those whose region is not removed), each with its dataset's bundle open
// on this computer and default segmentation; problems say what keeps one from being summarized
// (issues: each with its sample's id).
export function targetsOf(app) {
  const project = app.store.project;
  const out = [];
  const issues = [];
  for (const sample of samplesOf(project)) {
    if (sample.removed) continue;
    const item = app.bundles.link(sample.dataset.id)?.item ?? app.bundles.itemByPath(sample.dataset.path);
    const seg = promotedSegmentation(project, sample.dataset.id)?.record ?? null;
    if (!item) issues.push({ id: sample.id, text: `${sample.name}: its bundle is not open on this computer` });
    else if (!seg) issues.push({ id: sample.id, text: `${sample.name}: ${sample.dataset.name} has no default segmentation yet` });
    else out.push({ sample, dataset: sample.dataset, item, seg });
  }
  return { targets: out, problems: issues.map((x) => x.text), issues };
}

// A sample's cells chosen (its section's, or its region's; never those a review excluded):
// { keep, count, excluded, n, request } (lib/graphs.js), kept per what they depend on.
const chosenStore = new Map();
export function chosenOf(app, cellsOf, t) {
  const project = app.store.project;
  const reviews = reviewsOf(project, t.dataset.id).filter((r) => r.target === 'cells');
  const key = `${t.seg.id}|${t.sample.region ? `${t.sample.region.id}@${t.sample.region.version}` : ''}|${reviews.length}|${reviews.at(-1)?.time ?? ''}`;
  if (!chosenStore.has(key)) {
    const p = (async () => {
      const cells = await cellsOf(t.item, t.seg);
      const regionRows = t.sample.region ? [cellsIn(t.sample.region.geometry, cells)] : null;
      const state = cellReviewState(reviews, t.seg.id, cells.n);
      const chosen = chooseCells(cells.n, { regionRows, excluded: state.excluded });
      const r = t.sample.region;
      const request = cellsRequest(chosen.keep, { regions: r ? [{ id: r.id, name: r.name, version: r.version }] : [], excluded: chosen.excluded });
      return { ...chosen, n: cells.n, request };
    })();
    p.catch(() => chosenStore.delete(key));
    chosenStore.set(key, p);
  }
  return chosenStore.get(key);
}

// A label set's source: the marker set its cells were labeled from (the set's record's id), or the
// reference ('ref:', the reference file's SHA-256 and its column of labels); null for another.
export function sourceOf(record) {
  if (record?.markerSet) return record.markerSet;
  if (record?.reference?.sha256) return `ref:${record.reference.sha256}:${record.reference.labelColumn ?? ''}`;
  return null;
}

// A dataset's labels from a source on a segmentation: complete, the latest.
export function labelsFrom(project, datasetId, segmentationId, source) {
  return latest(project.artifacts.filter((a) => a.kind === 'labels' && a.dataset === datasetId && a.segmentation === segmentationId && a.state === 'complete' && sourceOf(a) === source));
}

// The sources the steps and the summary label from: the project's marker sets (and those labels
// were made from), and the references labels were made from: { value, name, label, kind, levels,
// file }.
export function sourceChoices(project) {
  const out = markerSetChoices(project).map((r) => ({ value: r.id, name: `${r.name} · v${r.version}`, label: r.name, kind: 'markers', levels: r.set.levels ?? [] }));
  for (const a of project.artifacts) {
    if (a.kind !== 'labels' || !a.reference?.sha256) continue;
    const value = sourceOf(a);
    if (out.some((c) => c.value === value)) continue;
    const file = a.reference.file ?? '';
    const base = file.split(/[\\/]/).filter(Boolean).at(-1) ?? 'a reference';
    out.push({ value, name: `Reference ${base}${a.reference.labelColumn ? ` (${a.reference.labelColumn})` : ''}`, label: `Reference ${base}`, kind: 'reference', levels: [], file });
  }
  return out;
}

// The marker sets the project's labels were made from or holds, latest first.
function markerSetChoices(project) {
  const out = markerSetsOf(project);
  for (const a of project.artifacts) {
    if (a.kind !== 'labels' || !a.markerSet || out.some((r) => r.id === a.markerSet)) continue;
    const r = project.artifacts.find((x) => x.kind === 'marker-set' && x.id === a.markerSet);
    if (r) out.push(r);
  }
  return out;
}

// The source a summary reads: the one chosen, else the first whose labels every sample has.
function summarySource(project, targets) {
  const choices = sourceChoices(project);
  if (choices.some((c) => c.value === cohortSession.markers)) return cohortSession.markers;
  return choices.find((c) => targets.every((t) => labelsFrom(project, t.dataset.id, t.seg.id, c.value)))?.value ?? '';
}

// A distances answer's cells' checksum (analysis.json's), read once.
function cellsChecksum(id) {
  if (!answerCells.has(id)) {
    const p = getJSON(`/api/artifacts/${encodeURIComponent(id)}/files/analysis.json`).then((a) => a.cells?.checksum ?? null);
    p.catch(() => answerCells.delete(id));
    answerCells.set(id, p);
  }
  return answerCells.get(id);
}

// A sample's artifacts a summary reads: its labels (from the marker set), its graph (over its cells,
// of the kind chosen if there is one) and its answers (an enrichment of its labels and Moran's I over
// the graph, nearest distances of its cells and labels), at the level.
export async function artifactsOf(app, cellsOf, t, source, level, kind = cohortSession.kind) {
  const project = app.store.project;
  const labels = source ? labelsFrom(project, t.dataset.id, t.seg.id, source) : null;
  const chosen = await chosenOf(app, cellsOf, t);
  const checksum = chosen.request.checksum;
  const graph = graphOfSample(project, t.dataset.id, t.seg.id, checksum, kind) ?? graphOfSample(project, t.dataset.id, t.seg.id, checksum);
  const atLevel = (a) => a.question === 'moran' || (a.summary?.labels?.level ?? 0) === level;
  const answers = graph ? graphAnalysesOf(project, t.dataset.id, graph.id, labels?.id ?? null).filter(atLevel) : [];
  if (labels) {
    const candidates = [];
    for (const a of project.artifacts) {
      if (a.kind !== 'spatial' || a.dataset !== t.dataset.id || a.segmentation !== t.seg.id || a.question !== 'distances' || a.state !== 'complete' || a.summary?.labels?.id !== labels.id || !atLevel(a)) continue;
      if (await cellsChecksum(a.id) === checksum) candidates.push(a);
    }
    answers.push(...latestByQuestion(candidates));
  }
  return { labels, chosen, graph, answers };
}

// --- The steps -----------------------------------------------------------------------------------------

// Runs the same steps on every sample: each dataset's cells clustered (SituCraft's defaults) and
// labeled from one source, a marker set (the first's resolving a starter, which the others then use)
// or a reference (options.reference, its path; or a reference's source), and each sample's graph
// built and its questions asked, with the same parameters. Steps made already are answered at once.
// onStep(text, done, total) says what is under way. Answers { source, markerSet, level, samples:
// [{ sample, name, clustering, labels, graph, answers }] }.
export async function runSteps(app, cellsOf, options = {}, onStep = () => {}) {
  const o = { level: cohortSession.level, kind: cohortSession.kind, questions: [...cohortSession.questions], ...options };
  // The source asked for, else the one the section chose.
  if (options.markers === undefined && options.reference === undefined) {
    o.markers = cohortSession.markers;
    if (o.markers === 'path') o.reference = cohortSession.referencePath;
  }
  for (const q of o.questions) if (!SUMMARY_QUESTIONS.includes(q)) throw new Error(`No question ${q} of a summary: ${SUMMARY_QUESTIONS.join(', ')}.`);
  if (!KINDS.some((k) => k.id === o.kind)) throw new Error(`No graph of kind ${o.kind}: ${KINDS.map((k) => k.id).join(', ')}.`);
  const { targets, problems } = targetsOf(app);
  if (problems.length) throw new Error(problems.join('; '));
  if (!targets.length) throw new Error('The project has no sample yet.');
  const datasets = [...new Map(targets.map((t) => [t.dataset.id, t])).values()];
  const total = 2 * datasets.length + targets.length * (1 + o.questions.length);
  let done = 0;
  const step = (text) => onStep(text, done, total);
  const project = () => app.store.project;
  const d = await clusteringDefaults();
  const clusterings = new Map();
  for (const t of datasets) {
    step(`Clustering the cells of ${t.dataset.name}`);
    const m = await app.bundles.clusterCells(t.item, t.dataset.id, { segmentation: t.seg.id, parameters: { ...d.parameters } });
    clusterings.set(t.dataset.id, m.id);
    done += 1;
  }
  const known = o.reference ? null : sourceChoices(project()).find((c) => c.value === o.markers) ?? null;
  const path = o.reference || (known?.kind === 'reference' ? known.file : '');
  let source = known?.value ?? null;
  const labels = new Map();
  for (const t of datasets) {
    const clustering = project().artifacts.find((a) => a.kind === 'clustering' && a.id === clusterings.get(t.dataset.id));
    step(`Labeling the cells of ${t.dataset.name}`);
    const ctx = { t: { item: t.item, dataset: t.dataset }, session: {}, clustering };
    const m = path ? await referenceClustering(app, ctx, { path }) : await labelClustering(app, ctx, { markers: source ?? (o.markers === 'path' ? '' : o.markers) ?? '' });
    const record = project().artifacts.find((a) => a.kind === 'labels' && a.id === m.id);
    source ??= sourceOf(record);
    if (sourceOf(record) !== source) throw new Error(`${t.dataset.name}'s cells were labeled from another ${path ? 'reference' : 'marker set'}`);
    labels.set(t.dataset.id, record);
    done += 1;
  }
  cohortSession.markers = source;
  cohortSession.kind = o.kind;
  cohortSession.questions = new Set(o.questions);
  const chosen = sourceChoices(project()).find((c) => c.value === source);
  const level = Math.min(o.level, Math.max(0, (chosen?.levels.length || 1) - 1));
  cohortSession.level = level;
  const out = [];
  for (const t of targets) {
    const chosen = await chosenOf(app, cellsOf, t);
    if (chosen.count < 2) throw new Error(`${t.sample.name}: fewer than two cells are chosen`);
    step(`Building a graph of the cells of ${t.sample.name}`);
    const graph = await app.bundles.buildGraph(t.item, t.dataset.id, { segmentation: t.seg.id, parameters: graphParams(GRAPH_DEFAULTS[o.kind]), cells: chosen.request });
    done += 1;
    let request = null;
    const answers = [];
    for (const q of o.questions) {
      step(`${questionLabel(q)} of ${t.sample.name}`);
      const asked = { question: q, segmentation: t.seg.id, parameters: { ...QUESTION_DEFAULTS[q] } };
      if (q === 'distances') asked.cells = chosen.request;
      else asked.graph = graph.id;
      if (q !== 'moran') {
        request ??= labelsRequest(await labelSource(app, refOf(labels.get(t.dataset.id).id, level)));
        asked.labels = request;
      }
      const m = await app.bundles.runAnalysis(t.item, t.dataset.id, asked);
      answers.push({ question: q, id: m.id });
      done += 1;
    }
    out.push({ sample: t.sample.id, name: t.sample.name, clustering: clusterings.get(t.dataset.id), labels: labels.get(t.dataset.id).id, graph: graph.id, answers });
  }
  onStep('Done', total, total);
  return { source, markerSet: chosen?.kind === 'markers' ? source : null, level, samples: out };
}

// --- The summary -------------------------------------------------------------------------------------

// The summary's request (api_cohort.go): the cohort's label names (every label of the level, those
// of coarser levels the cells carry there), and each sample's bundle, segmentation, cells, labels,
// region, graph and answers; of every sample, or those given (samples: ids).
export async function summaryRequest(app, cellsOf, { source, level, kind = cohortSession.kind, niches = null, closing = null, samples: only = null }) {
  const project = app.store.project;
  const all = targetsOf(app);
  const targets = only ? all.targets.filter((t) => only.includes(t.sample.id)) : all.targets;
  const problems = (only ? all.issues.filter((x) => only.includes(x.id)) : all.issues).map((x) => x.text);
  if (problems.length) throw new Error(problems.join('; '));
  if (!targets.length) throw new Error('The project has no sample yet.');
  const set = sourceChoices(project).find((c) => c.value === source);
  if (!set) throw new Error('Choose the labels every sample has, from one marker set or reference (run the steps first).');
  const samples = [];
  const used = new Set();
  let sets = null;
  const colors = new Map();
  for (const t of targets) {
    const a = await artifactsOf(app, cellsOf, t, source, level, kind);
    if (!a.labels) throw new Error(`${t.sample.name}: its cells are not labeled from ${set.name} yet (run the steps)`);
    const src = await labelSource(app, refOf(a.labels.id, level));
    sets ??= src.description?.sets ?? src.names.map((name) => ({ name, level }));
    for (const n of usedNames(src)) used.add(n);
    src.names.forEach((n, k) => {
      if (!colors.has(n)) colors.set(n, src.colors?.[k]);
    });
    const r = t.sample.region;
    samples.push({
      sample: t.sample.id, name: t.sample.name, dataset: t.dataset.id, item: itemIndex(t.item), segmentation: t.seg.id,
      cells: a.chosen.request, labels: labelsRequest(src),
      ...(r ? { region: { kind: 'region', region: { id: r.id, name: r.name, version: r.version }, geometry: r.geometry } } : {}),
      ...(a.graph ? { graph: a.graph.id } : {}),
      analyses: a.answers.map((x) => x.id),
    });
  }
  const names = vocabularyOf(sets, level, used);
  const levels = set.levels;
  const toRGB = (c) => (Array.isArray(c) ? `rgb(${c.join(', ')})` : String(c ?? ''));
  return {
    labels: { name: levels.length > 1 ? `${set.label}: ${String(levels[level]).toLowerCase()}` : set.label, level, names, colors: names.map((n) => toRGB(colors.get(n))) },
    samples,
    ...(niches ? { niches } : {}),
    ...(closing !== null ? { closing } : {}),
  };
}

// Summarizes the samples (a job, unless made already), recorded in the project. Answers its manifest.
export async function summarize(app, cellsOf, options = {}, redraw = () => {}) {
  const project = () => app.store.project;
  const { targets } = targetsOf(app);
  const o = {
    source: summarySource(project(), targets), level: cohortSession.level, kind: cohortSession.kind,
    niches: cohortSession.niches ? { ...NICHES_DEFAULTS, niches: cohortSession.nicheCount, neighbors: cohortSession.neighbors } : null,
    ...options,
  };
  const request = await summaryRequest(app, cellsOf, o);
  const answer = await getJSON('/api/cohort/summary', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
  let manifest = answer.artifact;
  if (!manifest) {
    cohortSession.summarizing = answer.job?.id ?? null;
    redraw();
    try {
      const job = await app.jobs.finished(cohortSession.summarizing);
      if (job?.state !== 'complete') throw new Error(`the summary ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
    } finally {
      cohortSession.summarizing = null;
    }
    manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
  }
  const next = addSummary(project(), summaryRecord(manifest, { markerSet: o.source?.startsWith('ref:') ? null : o.source, labelsFrom: o.source, level: o.level, samples: request.samples.map((s) => s.sample) }));
  if (next !== project()) app.store.commit(next, `Summarize ${request.samples.length} samples`);
  cohortSession.shown = manifest.id;
  redraw();
  return manifest;
}

// A summary's document (summary.json), read once.
const documents = new Map();
export function summaryDocument(id) {
  if (!documents.has(id)) {
    const p = getJSON(`/api/artifacts/${encodeURIComponent(id)}/files/summary.json`);
    p.catch(() => documents.delete(id));
    documents.set(id, p);
  }
  return documents.get(id);
}

// A summary's table (table.csv) read, once.
export function summaryTable(id) {
  if (!tables.has(id)) {
    const p = fetch(`/api/artifacts/${encodeURIComponent(id)}/files/table.csv`).then(async (response) => {
      if (!response.ok) throw new Error(`its table: HTTP ${response.status}`);
      return readCohortTable(await response.text());
    });
    p.catch(() => tables.delete(id));
    tables.set(id, p);
  }
  return tables.get(id);
}

// --- The sections ------------------------------------------------------------------------------------

// What each sample has of the steps, worked out once per state of the project.
const statuses = new WeakMap();
function statusOf(app, cellsOf, t, source, level, redraw) {
  const project = app.store.project;
  if (!statuses.has(project)) statuses.set(project, new Map());
  const cache = statuses.get(project);
  const key = `${t.sample.id}|${source}|${level}|${cohortSession.kind}`;
  if (!cache.has(key)) {
    cache.set(key, null);
    artifactsOf(app, cellsOf, t, source, level).then((a) => {
      cache.set(key, a);
      redraw();
    }, (error) => cache.set(key, { error }));
  }
  return cache.get(key);
}

export function stepsSection(app, { cellsOf, redraw }) {
  const project = app.store.project;
  const { targets, problems } = targetsOf(app);
  const s = cohortSession;
  const choices = sourceChoices(project);
  if (s.markers && s.markers !== 'path' && !choices.some((c) => c.value === s.markers)) s.markers = '';
  if (s.markers === 'path' && !s.referencePath) s.markers = '';
  const set = choices.find((c) => c.value === s.markers) ?? null;
  const levels = set?.levels ?? [];
  const base = (p) => p.split(/[\\/]/).filter(Boolean).at(-1) ?? p;
  const markerPick = h('select.input.small', {
    'aria-label': 'The marker set or reference every sample is labeled from',
    onchange: async (e) => {
      if (e.target.value === 'choose') {
        const paths = await pickPaths({ title: 'A single-cell reference', confirm: 'Choose', choose: 'file', hint: 'An AnnData object saved with write_zarr (a .zarr folder, or a zip of one), labeled cell by cell: every sample\'s cells are labeled from it.' });
        if (paths?.length) {
          s.referencePath = paths[0];
          s.markers = 'path';
        }
      } else s.markers = e.target.value;
      s.level = 0;
      redraw();
    },
  },
  h('option', { value: '', selected: !s.markers }, 'SituCraft\'s starter set that fits the panel'),
  ...choices.map((c) => h('option', { value: c.value, selected: c.value === s.markers }, c.name)),
  s.referencePath ? h('option', { value: 'path', selected: s.markers === 'path' }, `Reference ${base(s.referencePath)}`) : null,
  h('option', { value: 'choose' }, 'A reference on this computer…'));
  const levelPick = levels.length > 1 ? h('select.input.small', { 'aria-label': 'The level of the labels', onchange: (e) => { s.level = Number(e.target.value); redraw(); } },
    ...levels.map((l, k) => h('option', { value: String(k), selected: k === s.level }, l))) : null;
  const kindPick = h('select.input.small', { 'aria-label': 'The graph of each sample\'s cells', onchange: (e) => { s.kind = e.target.value; redraw(); } },
    ...KINDS.map((k) => h('option', { value: k.id, selected: k.id === s.kind }, k.label)));
  const questionBoxes = h('div.cohort-boxes', ...SUMMARY_QUESTIONS.map((q) => h('label.check', h('input', {
    type: 'checkbox', checked: s.questions.has(q),
    onchange: (e) => {
      if (e.target.checked) s.questions.add(q);
      else s.questions.delete(q);
    },
  }), questionLabel(q))));
  const run = async () => {
    s.running = { text: 'Starting', done: 0, total: 1 };
    redraw();
    try {
      const out = await runSteps(app, cellsOf, {}, (text, done, total) => {
        s.running = { text, done, total };
        redraw();
      });
      toast(`The steps are done for ${out.samples.length} sample${out.samples.length === 1 ? '' : 's'}.`, { kind: 'ok' });
    } catch (error) {
      toast(`The steps: ${error.message}`, { kind: 'error', timeout: 12000 });
    } finally {
      s.running = null;
      redraw();
    }
  };
  const source = s.markers && s.markers !== 'path' ? s.markers : summarySource(project, targets);
  const mark = (v, title) => (v === null ? h('span.muted', '…') : v ? h('span', { title }, '✓') : h('span.muted', '—'));
  const rows = targets.map((t) => {
    const a = statusOf(app, cellsOf, t, source, s.level, redraw);
    const has = (q) => a?.answers?.find((x) => x.question === q) ?? false;
    return h('tr',
      h('td', t.sample.name),
      h('td', a === null ? h('span.muted', '…') : a.error ? h('span.badge.warn', { title: a.error.message }, 'cannot tell') : mark(a.labels ?? false, a.labels?.id)),
      h('td', a === null || a.error ? '' : mark(a.graph ?? false, a.graph ? `${a.graph.name} (${a.graph.id})` : '')),
      ...SUMMARY_QUESTIONS.map((q) => h('td', a === null || a.error ? '' : mark(has(q), has(q)?.id))));
  });
  const running = s.running ? h('div.seg-running', h('div', h('b', s.running.text), ' ', h('span.muted', `${s.running.done} of ${s.running.total} steps`)), h('div.progress', h('div', { style: { width: `${Math.round((100 * s.running.done) / Math.max(1, s.running.total))}%` } }))) : null;
  return h('section.seg-section.cohort-steps',
    h('div.section-title', 'The same steps on every sample'),
    h('div.seg-card',
      h('p', 'Every sample is summarized the same way: each dataset\'s cells clustered with SituCraft\'s defaults and labeled from one marker set or one reference, so that every sample\'s labels have the same names; then a graph of each sample\'s cells (a region sample\'s, its region\'s) and the same questions asked of it, with the same parameters. A step made already is answered at once.'),
      h('div.btn-row', h('span.muted', 'Labels from'), markerPick, levelPick, h('span.muted', 'Graph'), kindPick),
      questionBoxes,
      problems.length ? h('p.seg-note', h('span.badge.warn', 'Cannot run'), ` ${problems.join('; ')}.`) : null,
      h('div.btn-row', h('button.btn.small.primary', { type: 'button', 'data-action': 'cohort-steps', disabled: Boolean(s.running) || !targets.length || problems.length > 0, onclick: run }, icon('play'), s.running ? 'Running…' : `Run on every sample (${targets.length})`)),
      running,
      targets.length ? h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table.cohort-steps-table',
        h('thead', h('tr', h('th', 'Sample'), h('th', 'Labels'), h('th', 'Graph'), ...SUMMARY_QUESTIONS.map((q) => h('th', questionLabel(q))))),
        h('tbody', ...rows))) : null));
}

export function summarySection(app, { cellsOf, redraw }) {
  const project = app.store.project;
  const s = cohortSession;
  const { targets } = targetsOf(app);
  const list = summariesOf(project);
  if (s.shown && !list.some((r) => r.id === s.shown)) s.shown = null;
  const shown = list.find((r) => r.id === s.shown) ?? list[0] ?? null;
  const set = sourceChoices(project).find((c) => c.value === summarySource(project, targets)) ?? null;
  const num = (value, label, min, max, onchange) => h('input.input.small.r', { type: 'number', min, max, step: 1, value: String(value), 'aria-label': label, style: { width: '64px' }, onchange: (e) => onchange(Number(e.target.value)) });
  const job = s.summarizing ? app.jobs.get(s.summarizing) : null;
  const go = async () => {
    try {
      const m = await summarize(app, cellsOf, {}, redraw);
      toast(`Summarized: ${m.name}.`, { kind: 'ok' });
    } catch (error) {
      toast(`The summary: ${error.message}`, { kind: 'error', timeout: 12000 });
      redraw();
    }
  };
  const controls = h('div.btn-row',
    h('label.check', h('input', { type: 'checkbox', checked: s.niches, onchange: (e) => { s.niches = e.target.checked; redraw(); } }), 'Niches across the samples:'),
    num(s.nicheCount, 'The niches to find', 2, 30, (v) => { s.nicheCount = v; }), h('span.muted', 'niches of each cell\'s'),
    num(s.neighbors, 'The labeled cells each window holds', 2, 200, (v) => { s.neighbors = v; }), h('span.muted', 'nearest labeled cells'));
  const parts = [
    h('p', 'The cohort table holds each sample\'s numbers, each traced to the artifact it came from: its cells by label, their shares and their densities per mm² of its tissue (its region\'s, for a region sample); its niches\' shares, the niches found across the samples together so that a niche is the same kind of place in every sample; its quality; its graph\'s numbers; its answers\' (an enrichment\'s ratios, nearest distances, Moran\'s I by gene); and its pseudobulk, each label\'s transcripts gene by gene.'),
    h('p.muted', set ? `Labels from ${set.name}${set.levels.length > 1 ? `, ${String(set.levels[s.level]).toLowerCase()}` : ''}.` : 'Run the steps first: the samples are labeled from one marker set or reference.'),
    s.niches ? controls : h('div.btn-row', controls.firstChild),
    h('div.btn-row', h('button.btn.small.primary', { type: 'button', 'data-action': 'cohort-summarize', disabled: Boolean(s.summarizing) || !set || !targets.length, onclick: go }, icon('table'), s.summarizing ? 'Summarizing…' : `Summarize the ${targets.length} samples`)),
  ];
  if (s.summarizing) parts.push(h('div.seg-running', h('div', h('b', 'Summarizing'), ' ', h('span.muted.cohort-job-message', job?.message ?? 'Starting…')), h('div.progress', h('div.cohort-job-bar', { style: { width: `${Math.round((job?.progress ?? 0) * 100)}%` } }))));
  if (list.length > 1) {
    const words = (r) => {
      const n = r.summary?.samples?.length ?? r.samples.length;
      return `${r.created?.slice(0, 16).replace('T', ' ') ?? ''} · ${r.summary?.labels?.name ?? r.name} · ${n} sample${n === 1 ? '' : 's'}${r.summary?.niches ? `, ${r.summary.niches} niches` : ''}`;
    };
    parts.push(h('div.btn-row', h('span.muted', 'Summary'), h('select.input.small.cohort-family', { 'aria-label': 'The summary shown', onchange: (e) => { s.shown = e.target.value; redraw(); } },
      ...list.map((r) => h('option', { value: r.id, selected: r.id === shown?.id }, words(r))))));
  }
  if (shown) parts.push(tableCard(app, shown, redraw));
  return h('section.seg-section.cohort-summary',
    h('div.section-title', 'The cohort table'),
    h('div.seg-card', ...parts));
}

function tableCard(app, record, redraw) {
  const holder = h('div.cohort-shown', { 'data-summary': record.id }, h('p.muted', 'Reading the table…'));
  Promise.all([summaryTable(record.id), summaryDocument(record.id)]).then(([table, doc]) => {
    holder.replaceChildren(...tableParts(app, record, table, doc, redraw));
  }, (error) => holder.replaceChildren(h('p', h('span.badge.warn', 'Not read'), ` ${error.message}`)));
  return holder;
}

// A key as a column's head: a pair's names apart.
const keyHead = (k) => k.replace(/\|/g, ' | ').replace(/→/g, ' → ');

function tableParts(app, record, table, doc, redraw) {
  const s = cohortSession;
  if (!table.families.some((f) => f.id === s.family)) s.family = table.families[0]?.id ?? 'cells';
  const family = table.families.find((f) => f.id === s.family);
  const pick = h('select.input.small.cohort-family', { 'aria-label': 'The numbers shown', onchange: (e) => { s.family = e.target.value; redraw(); } },
    ...table.families.map((f) => h('option', { value: f.id, selected: f.id === s.family }, `${f.label} (${f.keys.length})`)));
  const shownKeys = family ? family.keys.slice(0, 60) : [];
  const fmt = (v) => (Number.isFinite(v) ? v.toLocaleString('en-US', { maximumFractionDigits: family.digits, minimumFractionDigits: family.id === 'cells' ? 0 : Math.min(family.digits, 1) }) : '—');
  const head = h('tr', h('th', 'Sample'), ...shownKeys.map((k) => h('th.r', { title: k }, keyHead(k))));
  const rows = table.samples.map((smp) => {
    const v = family?.values.get(smp.sample);
    return h('tr', h('td', smp.name), ...shownKeys.map((k) => {
      const x = v?.get(k);
      return h('td.r', { title: x ? `${smp.name}, ${k}: ${x.value}${x.source ? ` (from ${x.source})` : ' (this summary\'s niches)'}` : '' }, x ? fmt(x.value) : h('span.muted', '—'));
    }));
  });
  const sum = record.summary ?? {};
  const base = safeName(`${app.store.project.name}-${record.id}`);
  const file = (path) => fetch(`/api/artifacts/${encodeURIComponent(record.id)}/files/${path}`).then(async (r) => {
    if (!r.ok) throw new Error(`HTTP ${r.status}`);
    return r.blob();
  });
  const save = (path, name) => file(path).then((blob) => downloadBlob(blob, name), (error) => toast(`${path}: ${error.message}`, { kind: 'error' }));
  return [
    h('p', h('b', record.name), h('span.muted', ` · ${sum.samples?.length ?? record.samples.length} samples, ${sum.labels?.names?.length ?? '?'} labels, ${sum.genes ?? '?'} genes${sum.niches ? `, ${sum.niches} niches` : ''} · artifact `), h('span.mono', record.id)),
    h('div.btn-row', h('span.muted', 'Show'), pick,
      h('button.btn.small', { type: 'button', title: 'These numbers as CSV: a row a sample, a column each', onclick: () => downloadBlob(new Blob([familyCSV(table, s.family)], { type: 'text/csv' }), `${base}-${s.family}.csv`) }, icon('download'), 'These as CSV'),
      h('button.btn.small', { type: 'button', title: 'Every number of every sample, a row each, with the artifact it came from', onclick: () => save('table.csv', `${base}-table.csv`) }, icon('download'), 'The cohort table (CSV)'),
      h('button.btn.small', { type: 'button', title: 'Each sample\'s each label\'s transcripts, gene by gene', onclick: () => save('pseudobulk.csv', `${base}-pseudobulk.csv`) }, icon('download'), 'The pseudobulk (CSV)')),
    h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table.cohort-numbers', h('thead', head), h('tbody', ...rows))),
    family && family.keys.length > shownKeys.length ? h('p.muted.seg-note', `The first ${shownKeys.length} of ${family.keys.length}; every one is in the CSV.`) : null,
    family?.id === 'niche' && doc.niches ? nichesList(doc) : null,
    h('p.muted.seg-note', 'Each number\'s tip names the artifact it came from: the labels for cells, shares and densities; the segmentation for quality; the graph or the answer for theirs. A label a sample\'s cells do not carry counts as none. A niche\'s share is of the sample\'s cells with a window.'),
  ];
}

// The niches found across the samples: each one's cells in all of them and the labels that define
// it (those whose share of its windows is at least 1.5 times their share of all the samples'
// labeled cells, else its commonest; internal/spatial DefiningLabels).
function nichesList(doc) {
  const n = doc.niches;
  const names = doc.labels.names;
  return h('div',
    h('p.muted.seg-note', `${n.niches} niches found across the ${doc.samples.length} samples together: windows of each cell's ${n.parameters.neighbors} nearest labeled cells in its own sample, pooled, by k-means (the best of ${n.parameters.starts} starts).${n.agreement?.length ? ` Further seeds agree with them: adjusted Rand index ${Math.min(...n.agreement).toFixed(3)} at the least.` : ''}`),
    h('ul.cohort-niches', ...n.cells.map((cells, v) => h('li', h('b', `niche ${v + 1}`), ` · ${cells.toLocaleString('en-US')} cells · defined by ${(n.defining[v] ?? []).map((k) => names[k]).join(', ') || '—'}`))));
}

// Live progress of the summary being made, without drawing the view again.
export function updateSummaryProgress(root, app) {
  const job = cohortSession.summarizing ? app.jobs.get(cohortSession.summarizing) : null;
  if (!job) return;
  const bar = root.querySelector('.cohort-job-bar');
  const message = root.querySelector('.cohort-job-message');
  if (bar) bar.style.width = `${Math.round((job.progress ?? 0) * 100)}%`;
  if (message) message.textContent = job.message ?? '';
}
