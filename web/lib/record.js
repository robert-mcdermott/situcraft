// What made a result (wave 8, slice 3; requirement E9): each step from the bundles to it, with its
// method, version, parameters, seed, software and references, read from the project's records and,
// where the program kept more, its artifacts' manifests (their parents and their jobs' inputs).
// Written as Markdown, JSON and BibTeX. Deterministic: the same project, result and manifests give
// the same record.

import { REFERENCES, bibtexEntry, citationKeys, referenceByDoi, referenceText, situcraftReference } from './references.js';
import { panelInputs } from './figure-manifest.js';
import { samplesOf } from './samples.js';
import { transformInUse } from './transforms.js';

export const RECORD_FORMAT = 'situcraft-record';
export const RECORD_VERSION = 1;

// Each kind of step, in the order steps of different kinds are written when either may come first.
export const STEP_KINDS = {
  dataset: 'Xenium output bundle',
  image: 'Post-Xenium image',
  transform: 'Image alignment',
  'alignment-check': 'Alignment check',
  region: 'Region',
  filter: 'QC filter',
  segmentation: 'Segmentation',
  comparison: 'Segmentation comparison',
  export: 'Export',
  clustering: 'Clustering',
  'cluster-names': 'Cluster names',
  embedding: 'Map',
  'marker-set': 'Marker set',
  labels: 'Label set',
  'curated-labels': 'Curated label set',
  'consensus-labels': 'Consensus label set',
  graph: 'Cell graph',
  spatial: 'Spatial analysis',
  view: 'Saved view',
  design: 'Design',
  'cohort-summary': 'Cohort summary',
  abundance: 'Abundance comparison',
  expression: 'Expression comparison',
  'spatial-comparison': 'Spatial statistics comparison',
  figure: 'Figure',
};
const RANK = Object.fromEntries(Object.keys(STEP_KINDS).map((k, i) => [k, i]));

// What a record of several results is of.
const SUBJECTS = { project: 'Project', dataset: 'Dataset' };

// The kinds a record can be asked of, as the Reports view lists them.
export const RESULT_KINDS = ['figure', 'abundance', 'expression', 'spatial-comparison', 'cohort-summary', 'spatial', 'graph', 'embedding', 'labels', 'curated-labels', 'consensus-labels', 'clustering', 'comparison', 'segmentation', 'alignment-check', 'transform', 'filter'];

// The project's results a record can be made of, the latest first within each kind.
export function recordableResults(project) {
  const out = [];
  for (const kind of RESULT_KINDS) {
    const list = kind === 'figure' ? project.figures ?? [] : project.artifacts.filter((a) => a.kind === kind && (a.state ?? 'complete') === 'complete');
    for (const r of [...list].sort((a, b) => (a.created < b.created ? 1 : a.created > b.created ? -1 : 0))) out.push({ id: r.id, kind, name: nameOf(kind, r) });
  }
  return out;
}

function nameOf(kind, r) {
  if (kind === 'region') return r.versions?.at(-1)?.name ?? r.id;
  if (kind === 'filter') return `${r.name}, version ${r.version}`;
  if (kind === 'design' || kind === 'marker-set') return `${r.name}, version ${r.version}`;
  if (kind === 'cluster-names') return `Cluster names, version ${r.version}`;
  return r.name ?? r.id;
}

// The results the record of the whole project, or of one of its datasets, starts from: its
// datasets (one's bundle, described even with nothing made of it) and each complete result but a
// filter's older versions and segmentations planted for practice; a dataset's, those made of it.
export function scopeResults(project, datasetId = null) {
  const latest = new Map();
  for (const a of project.artifacts) {
    if (a.kind !== 'filter') continue;
    const key = `${a.dataset}\n${a.name}`;
    if (!latest.has(key) || latest.get(key).version < a.version) latest.set(key, a);
  }
  const keep = (r) => {
    const rec = itemOf(project, r.id).record;
    if (r.kind === 'filter' && latest.get(`${rec.dataset}\n${rec.name}`) !== rec) return false;
    if (r.kind === 'segmentation' && rec.source === 'planted') return false;
    return !datasetId || rec.dataset === datasetId;
  };
  const datasets = project.datasets.filter((d) => !datasetId || d.id === datasetId).map((d) => d.id);
  return [...datasets, ...recordableResults(project).filter(keep).map((r) => r.id)];
}

// An item of the project by its id: { kind, record } or null.
export function itemOf(project, id) {
  const d = project.datasets.find((x) => x.id === id);
  if (d) return { kind: 'dataset', record: d };
  const a = project.artifacts.find((x) => x.id === id);
  if (a) return { kind: a.kind, record: a };
  for (const [field, kind] of [['figures', 'figure'], ['views', 'view'], ['regions', 'region'], ['exports', 'export']]) {
    const x = (project[field] ?? []).find((y) => y.id === id);
    if (x) return { kind, record: x };
  }
  return null;
}

// Whether an item is the program's artifact, with a manifest of its own.
export const hasManifest = (item) => Boolean(item && item.kind !== 'dataset' && item.record.registered);

// The ids a job's inputs name (its spec as the manifest records it): segmentations, graphs,
// clusterings, summaries, images and transforms by id; label sets, filters and regions by their
// records' ids; a cohort's analyses. Big values (cells' codes) are not read.
function specIds(spec, out = []) {
  if (!spec || typeof spec !== 'object') return out;
  if (Array.isArray(spec)) {
    for (const x of spec) if (x && typeof x === 'object') specIds(x, out);
    return out;
  }
  for (const [k, v] of Object.entries(spec)) {
    if (['segmentation', 'graph', 'clustering', 'summary', 'image', 'transform'].includes(k) && typeof v === 'string') out.push(v);
    if (['labels', 'filter'].includes(k) && typeof v?.id === 'string') out.push(v.id);
    if (k === 'regions' && Array.isArray(v)) out.push(...v.map((r) => r?.id).filter((x) => typeof x === 'string'));
    if (k === 'analyses' && Array.isArray(v)) out.push(...v.filter((x) => typeof x === 'string'));
    if (v && typeof v === 'object' && !['labels', 'filter', 'regions', 'markers', 'design'].includes(k)) specIds(v, out);
  }
  return out;
}

// The ids an item was made from: what its record names, and what its manifest's lineage names.
export function inputsOf(project, item, manifest = null) {
  const r = item.record;
  const ids = [];
  const push = (...xs) => {
    for (const x of xs) if (typeof x === 'string' && x && x !== r.id && x !== 'instrument') ids.push(x);
  };
  switch (item.kind) {
    case 'dataset':
    case 'marker-set':
      break;
    case 'figure':
      for (const p of r.panels ?? []) {
        const inputs = panelInputs(project, p);
        push(inputs.dataset?.id, inputs.view?.id, ...(inputs.artifacts ?? []).map((a) => a.id));
      }
      break;
    case 'view': {
      push(r.dataset, r.segmentation);
      const fill = r.cells?.fill ?? '';
      const labels = /^labels:([^:]+)/.exec(fill);
      if (labels) push(labels[1]);
      if (fill.startsWith('clusters:')) push(fill.slice('clusters:'.length));
      for (const l of r.images ?? []) {
        if (!l.visible || !(l.opacity > 0)) continue;
        push(l.id, transformInUse(project, l.id)?.record?.id);
      }
      break;
    }
    case 'design':
      for (const s of r.samples ?? []) push(s.region ?? s.dataset);
      break;
    case 'cohort-summary': {
      const samples = new Map(samplesOf(project).map((s) => [s.id, s]));
      for (const id of r.samples ?? []) {
        const s = samples.get(id);
        push(s ? s.region?.id ?? s.dataset.id : id.replace(/^s-/, ''));
      }
      push(r.markerSet);
      break;
    }
    case 'abundance':
    case 'expression':
    case 'spatial-comparison': {
      push(r.summary);
      const design = r.design && project.artifacts.find((a) => a.kind === 'design' && a.name === r.design.name && a.version === r.design.version);
      push(design?.id);
      break;
    }
    case 'labels':
      push(r.dataset, r.segmentation, r.clustering, r.markerSet);
      break;
    case 'curated-labels':
      push(r.base?.labels, ...(r.edits ?? []).map((e) => e.clustering));
      break;
    case 'consensus-labels':
      push(...(r.members ?? []).map((m) => m.ref?.id));
      break;
    case 'spatial':
      push(r.dataset, r.segmentation, r.graph, r.summary?.labels?.id, r.summary?.choice?.filter?.id, ...(r.summary?.choice?.regions ?? []).map((x) => x.id));
      break;
    case 'graph':
      push(r.dataset, r.segmentation, r.summary?.choice?.filter?.id, ...(r.summary?.choice?.regions ?? []).map((x) => x.id));
      break;
    case 'comparison':
      push(r.dataset, r.a, r.b);
      break;
    case 'transform':
      push(r.dataset, r.image, r.parent);
      break;
    case 'alignment-check':
      push(r.dataset, r.image, r.transform);
      break;
    case 'segmentation':
      push(r.dataset, ...(r.parents ?? []).filter((p) => p.kind !== 'bundle').map((p) => p.id));
      break;
    default:
      push(r.dataset, r.segmentation, r.clustering);
  }
  const lineage = manifest?.lineage;
  if (lineage) {
    for (const p of lineage.parents ?? []) {
      if (p.kind === 'bundle') {
        if (r.dataset) push(r.dataset);
      } else push(p.id);
    }
    push(...specIds(lineage.job?.spec));
  }
  // Only what the project holds is an input; inputs named twice are named once.
  return [...new Set(ids)].filter((id) => itemOf(project, id));
}

// The manifests a record of a result (or of several) reads, fetched as the walk finds the
// artifacts they describe (fetch: id → manifest, or null when the program no longer has it).
export async function gatherManifests(project, ids, fetch, manifests = new Map()) {
  for (;;) {
    const wanted = walk(project, [ids].flat(), manifests).order.filter((x) => hasManifest(itemOf(project, x)) && !manifests.has(x));
    if (!wanted.length) return manifests;
    for (const x of wanted) {
      let m = null;
      try {
        m = await fetch(x);
      } catch {
        m = null;
      }
      manifests.set(x, m);
    }
  }
}

// The items results were made from, in an order where each comes after its inputs: kinds in
// STEP_KINDS's order where either may come first, then the oldest first.
function walk(project, roots, manifests) {
  const inputs = new Map();
  const stack = [...roots].reverse();
  while (stack.length) {
    const x = stack.pop();
    if (inputs.has(x)) continue;
    const item = itemOf(project, x);
    const list = item ? inputsOf(project, item, manifests.get(x) ?? null) : [];
    inputs.set(x, list);
    for (const y of list) if (!inputs.has(y)) stack.push(y);
  }
  const waiting = new Map([...inputs].map(([x, list]) => [x, new Set(list)]));
  const key = (x) => {
    const item = itemOf(project, x);
    return [RANK[item?.kind] ?? 99, item?.record.created ?? item?.record.added ?? '', x];
  };
  const before = (a, b) => {
    const ka = key(a);
    const kb = key(b);
    for (let i = 0; i < 3; i += 1) if (ka[i] !== kb[i]) return ka[i] < kb[i] ? -1 : 1;
    return 0;
  };
  const order = [];
  while (waiting.size) {
    const ready = [...waiting].filter(([, s]) => !s.size).map(([x]) => x).sort(before);
    // A cycle (which the records should never make) is broken at its first item.
    const next = ready[0] ?? [...waiting.keys()].sort(before)[0];
    order.push(next);
    waiting.delete(next);
    for (const s of waiting.values()) s.delete(next);
  }
  return { order, inputs };
}

// The seeds among a step's parameters, wherever they are.
function seedsOf(params, out = []) {
  if (!params || typeof params !== 'object') return out;
  for (const [k, v] of Object.entries(params)) {
    if (/^seed$/i.test(k) && Number.isFinite(v)) out.push(v);
    else if (v && typeof v === 'object') seedsOf(v, out);
  }
  return [...new Set(out)];
}

// The references a step cites: { keys (the registry's), others: [{ text, doi }] }.
function citedBy(citation, extra = []) {
  const { keys, unknown } = citationKeys(citation ?? '');
  const others = unknown.map((text) => ({ text, doi: /doi:\s*(10\.\S+?)\.?$/i.exec(text)?.[1] ?? null }));
  for (const ref of extra) {
    const key = referenceByDoi(ref.doi);
    if (key) {
      if (!keys.includes(key)) keys.push(key);
    } else if (!others.some((o) => o.doi && o.doi.toLowerCase() === String(ref.doi).toLowerCase())) others.push({ text: ref.citation ?? ref.doi, doi: ref.doi ?? null });
  }
  return { keys, others };
}

const count = (n) => Number(n).toLocaleString('en-US');

// A step of the record: what it is, and how it was made.
function stepOf(project, id, item, manifest, inputs) {
  const r = item.record;
  const lineage = manifest?.lineage ?? {};
  const step = {
    id,
    kind: item.kind,
    name: nameOf(item.kind, r),
    made: r.created ?? r.added ?? null,
    method: null,
    parameters: null,
    seeds: [],
    software: [],
    references: { keys: [], others: [] },
    inputs,
    decisions: (project.decisions ?? []).filter((d) => d.artifact === id).map(({ kind, rationale, by, time, comparison }) => ({ kind, rationale, by, time, ...(comparison ? { comparison } : {}) })),
  };
  const dataset = item.kind !== 'dataset' && r.dataset ? project.datasets.find((d) => d.id === r.dataset) : null;
  if (dataset) step.dataset = dataset.name;
  if (item.kind === 'dataset') {
    const simulated = r.validation?.support === 'simulated';
    // A simulated bundle names its layout and its simulator ("xenium-2.0.0 layout, situcraft-simulate 0.6.0").
    const simulator = /situcraft-simulate ([\w.-]+)/.exec(r.software ?? '')?.[1] ?? '';
    step.method = simulated ? { name: 'Simulated by SituCraft (situcraft simulate)', version: simulator } : { name: 'Xenium Onboard Analysis', version: r.version ?? '' };
    step.software = simulated ? [{ name: 'situcraft simulate', version: simulator }] : r.software ? [{ name: 'Xenium Onboard Analysis', version: r.software }] : [];
    step.parameters = { panel: r.panel ?? '', run: r.run ?? '', region: r.region ?? '', cells: r.cells, transcripts: r.transcripts, analysisUuid: r.analysisUuid ?? '' };
    step.references = simulated ? { keys: [], others: [] } : { keys: ['janesick2023'], others: [] };
    const v = r.validation ?? {};
    step.note = `Read and checked by SituCraft: ${count(v.fatal ?? 0)} fatal problems, ${count(v.warnings ?? 0)} warnings${v.metricsCompared ? `; ${count(v.metricsAgreeing ?? 0)} of the ${count(v.metricsCompared)} values the run recorded recomputed alike` : ''}.`;
    return step;
  }
  if (item.kind === 'marker-set') {
    step.method = { name: 'Marker set', version: String(r.version) };
    step.parameters = { labels: (r.set?.labels ?? []).length, levels: r.set?.levels ?? [], checksum: r.checksum, starter: r.source?.starter ?? null };
    step.references = citedBy('', r.set?.references ?? []);
    step.by = r.by ?? null;
    return step;
  }
  if (item.kind === 'design') {
    step.method = { name: 'Design', version: String(r.version) };
    step.parameters = { ...r.spec, samples: (r.samples ?? []).length, excluded: (r.excluded ?? []).length, model: r.result?.names ?? [], df: r.result?.df ?? null };
    step.note = (r.result?.words ?? []).join(' ');
    return step;
  }
  if (item.kind === 'filter') {
    step.method = { name: 'QC filter', version: String(r.version) };
    step.parameters = { conditions: (r.conditions ?? []).map((c) => c.text ?? JSON.stringify(c)), kept: r.result?.kept, cells: r.result?.cells, checksum: r.result?.sha256 };
    return step;
  }
  if (item.kind === 'region') {
    const v = r.versions?.at(-1) ?? {};
    step.method = { name: 'Region', version: String(r.versions?.length ?? 1) };
    step.parameters = { class: v.cls ?? '', tool: v.tool ?? '' };
    step.by = v.by ?? null;
    step.made = r.versions?.[0]?.time ?? null;
    return step;
  }
  if (item.kind === 'curated-labels' || item.kind === 'consensus-labels' || item.kind === 'cluster-names') {
    step.method = { name: STEP_KINDS[item.kind], version: r.version ? String(r.version) : '' };
    step.parameters = item.kind === 'curated-labels' ? { edits: (r.edits ?? []).length, base: r.base?.name ?? r.base?.labels }
      : item.kind === 'consensus-labels' ? { rule: r.rule, members: (r.members ?? []).length } : { named: (r.names ?? []).filter(Boolean).length };
    step.by = r.by ?? null;
    return step;
  }
  if (item.kind === 'view') {
    step.method = { name: 'Saved view', version: '' };
    return step;
  }
  if (item.kind === 'figure') {
    step.method = { name: 'SituCraft figure', version: '1' };
    step.parameters = { size: r.size, width: r.width, height: r.height, panels: (r.panels ?? []).length };
    return step;
  }
  const method = r.method ?? lineage.method ?? null;
  step.method = method ? { name: method.name ?? '', version: method.version ?? '' } : null;
  step.parameters = r.parameters ?? lineage.parameters ?? null;
  step.seeds = seedsOf(step.parameters);
  step.software = (r.software?.length ? r.software : lineage.software ?? []).map(({ name, version, commit }) => ({ name, version, ...(commit ? { commit } : {}) }));
  // A label set from markers also cites the marker set's references.
  const markers = item.kind === 'labels' && r.markerSet ? project.artifacts.find((a) => a.id === r.markerSet)?.set?.references ?? [] : [];
  step.references = citedBy(method?.citation ?? '', markers);
  return step;
}

// The record of a result: { format, version, result: { id, kind, name }, program, recorded, steps,
// software, references: [{ n, key, text, doi, url }] }. Each step: { n, id, kind, name, made,
// method, parameters, seeds, software, references (numbers), inputs (step numbers), decisions,
// note?, by?, dataset? }. Of several results (ids a list), the record of them all, its result the
// subject given ({ id, kind: 'project' or 'dataset', name }).
export function resultRecord(project, ids, { manifests = new Map(), program = { version: '', commit: '' }, now = null, subject = null } = {}) {
  const roots = [ids].flat();
  for (const id of roots) if (!itemOf(project, id)) throw new Error(`The project has no result ${id}.`);
  if (roots.length !== 1 && !subject) throw new Error('A record of several results names its subject.');
  const item = roots.length === 1 ? itemOf(project, roots[0]) : null;
  const { order, inputs } = walk(project, roots, manifests);
  const steps = order.map((x) => stepOf(project, x, itemOf(project, x), manifests.get(x) ?? null, inputs.get(x) ?? []));
  const numberOf = new Map(order.map((x, i) => [x, i + 1]));
  // References numbered as first cited; SituCraft itself with the first step it made.
  const refs = [];
  const cite = (ref) => {
    let i = refs.findIndex((x) => (ref.key && x.key === ref.key) || (!ref.key && x.text === ref.text));
    if (i < 0) {
      refs.push(ref);
      i = refs.length - 1;
    }
    return i + 1;
  };
  const situcraft = situcraftReference(program.version);
  for (const s of steps) {
    const numbers = [];
    if (s.software.some((x) => x.name === 'situcraft') || s.kind === 'figure') numbers.push(cite({ key: 'situcraft', text: referenceText(situcraft), url: situcraft.url }));
    for (const key of s.references.keys) numbers.push(cite({ key, text: referenceText(REFERENCES[key]), doi: REFERENCES[key].doi ?? null, url: REFERENCES[key].url ?? null }));
    for (const o of s.references.others) numbers.push(cite({ key: null, text: o.text, doi: o.doi }));
    s.references = [...new Set(numbers)].sort((a, b) => a - b);
    s.inputs = s.inputs.map((x) => numberOf.get(x)).filter(Boolean).sort((a, b) => a - b);
    s.n = numberOf.get(s.id);
  }
  const software = [];
  for (const s of steps) for (const x of s.software) if (!software.some((y) => y.name === x.name && y.version === x.version)) software.push({ name: x.name, version: x.version });
  return {
    format: RECORD_FORMAT,
    version: RECORD_VERSION,
    result: subject ?? { id: roots[0], kind: item.kind, name: nameOf(item.kind, item.record) },
    program: { name: 'SituCraft', version: program.version ?? '', ...(program.commit ? { commit: program.commit } : {}) },
    ...(now ? { recorded: now } : {}),
    steps: steps.map(({ n, ...rest }) => ({ n, ...rest })),
    software,
    references: refs.map((r, i) => ({ n: i + 1, key: r.key, text: r.text, ...(r.doi ? { doi: r.doi } : {}), ...(r.url ? { url: r.url } : {}) })),
  };
}

// --- Written --------------------------------------------------------------------------------------

const md = (s) => String(s ?? '').replace(/([\\`*_[\]<>])/g, '\\$1');
// Markdown's strong emphasis.
const strong = (s) => ['', s, ''].join('**');

// A step's parameters in words: "key = value", nested keys joined by dots, a checksum shortened,
// long lists counted; colors and seeds left out (seeds are said on their own).
export function parameterText(params) {
  const out = [];
  const value = (v) => {
    if (typeof v === 'string') return /^[0-9a-f]{40,}$/.test(v) ? `${v.slice(0, 12)}…` : v.length > 80 ? `${v.slice(0, 77)}…` : v;
    if (typeof v === 'number') return Number.isInteger(v) ? String(v) : String(Number(v.toPrecision(6)));
    return String(v);
  };
  const walkParams = (prefix, v) => {
    if (v === null || v === undefined || v === '') return;
    if (Array.isArray(v)) {
      if (!v.length) return;
      if (v.every((x) => x === null || typeof x !== 'object')) out.push(`${prefix} = ${v.length > 12 ? `${v.length} values` : v.map(value).join(', ')}`);
      else if (v.every((x) => typeof x?.name === 'string')) out.push(`${prefix} = ${v.map((x) => x.name).join(', ')}`);
      else out.push(`${prefix} = ${v.length} items`);
      return;
    }
    if (typeof v === 'object') {
      for (const [k, x] of Object.entries(v)) if (!/^(colors?|seed|codes|keep)$/i.test(k)) walkParams(prefix ? `${prefix}.${k}` : k, x);
      return;
    }
    out.push(`${prefix} = ${value(v)}`);
  };
  walkParams('', params);
  return out.join('; ');
}

export const numbers = (list) => {
  // [1, 2, 3, 5] → "1–3, 5"
  const out = [];
  for (let i = 0; i < list.length; i += 1) {
    let j = i;
    while (j + 1 < list.length && list[j + 1] === list[j] + 1) j += 1;
    out.push(j > i + 1 ? `${list[i]}–${list[j]}` : j === i + 1 ? `${list[i]}, ${list[j]}` : `${list[i]}`);
    i = j;
  }
  return out.join(', ');
};

// What is the same of steps written once for many items.
const signature = (s) => JSON.stringify([s.kind, s.method, s.kind === 'dataset' ? null : s.parameters, s.software, s.references, s.seeds, s.note && s.kind !== 'dataset' ? s.note : null]);

// The record's steps as they are written: consecutive steps the same but for their items (the same
// kind, method, parameters, software and references, none made from another) together; the result
// on its own.
export function groupSteps(record) {
  const groups = [];
  for (const s of record.steps) {
    const last = groups.at(-1);
    if (last && signature(last[0]) === signature(s) && s.n !== record.steps.length && !s.inputs.some((i) => last.some((x) => x.n === i))) last.push(s);
    else groups.push([s]);
  }
  return groups;
}

// The record as Markdown: the result, its steps (consecutive steps the same but for their items
// written once), the software and the references.
export function recordMarkdown(record) {
  const lines = [`# What made ${md(record.result.name)}`, ''];
  lines.push(`${SUBJECTS[record.result.kind] ?? STEP_KINDS[record.result.kind] ?? record.result.kind} \`${record.result.id}\`, recorded by SituCraft${record.program.version ? ` ${record.program.version}` : ''}${record.recorded ? ` on ${record.recorded.slice(0, 10)}` : ''}: ${record.steps.length} ${record.steps.length === 1 ? 'step' : 'steps'}, each after the steps it was made from.`, '', '## Steps', '');
  for (const g of groupSteps(record)) {
    const s = g[0];
    const kind = STEP_KINDS[s.kind] ?? s.kind;
    const lower = /^(Xenium|QC)/.test(kind) ? kind : kind.charAt(0).toLowerCase() + kind.slice(1);
    const label = g.length === 1 ? `${s.n}.` : `${s.n}–${g.at(-1).n}.`;
    const parts = [];
    const each = `${plural(kind)}, one for each of ${g.length}:`;
    if (g.length === 1) parts.push(`${strong(md(s.name))} (${lower} \`${s.id}\`${s.made ? `, ${s.made.slice(0, 10)}` : ''})`);
    else if (g.every((x) => x.name === s.name && x.dataset)) parts.push(`${strong(each)} ${md(s.name)}, of ${g.map((x) => `${md(x.dataset)} (\`${x.id}\`)`).join(', ')}`);
    else parts.push(`${strong(each)} ${g.map((x) => `${x.dataset && x.name !== x.dataset ? `${md(x.dataset)}: ` : ''}${md(x.name)} (\`${x.id}\`)`).join('; ')}`);
    const inputs = [...new Set(g.flatMap((x) => x.inputs))].sort((a, b) => a - b);
    const sentences = [];
    if (s.method?.name) sentences.push(`Method: ${md(s.method.name)}${s.method.version ? `, version ${md(s.method.version)}` : ''}.`);
    if (inputs.length) sentences.push(`From step${inputs.length === 1 ? '' : 's'} ${numbers(inputs)}.`);
    if (s.kind === 'dataset') {
      for (const x of g) {
        const p = x.parameters ?? {};
        sentences.push(`${g.length > 1 ? `${md(x.name)}: ` : ''}${[p.panel, Number.isFinite(p.cells) ? `${count(p.cells)} cells` : '', Number.isFinite(p.transcripts) ? `${count(p.transcripts)} transcripts` : '', p.analysisUuid ? `analysis ${p.analysisUuid}` : ''].filter(Boolean).join(', ')}.`);
      }
      const notes = [...new Set(g.map((x) => x.note).filter(Boolean))];
      if (notes.length === 1) sentences.push(md(notes[0]));
      else for (const x of g) if (x.note) sentences.push(`${md(x.name)}: ${md(x.note)}`);
    } else {
      const text = parameterText(s.parameters);
      if (text) sentences.push(`Parameters: ${md(text)}.`);
      if (s.note) sentences.push(md(s.note));
    }
    if (s.seeds.length) sentences.push(`Seed${s.seeds.length === 1 ? '' : 's'}: ${s.seeds.join(', ')}.`);
    if (s.software.length) sentences.push(`Software: ${s.software.map((x) => md(`${x.name} ${x.version}`.trim())).join('; ')}.`);
    if (s.by) sentences.push(`By ${md(s.by)}.`);
    // Decisions: once when each item's are the same.
    const said = (x) => x.decisions.map((d) => `${md(decisionWords(d.kind))}${d.rationale ? `: "${md(d.rationale)}"` : ''} (${md(d.by ?? '')}${d.time ? `, ${d.time.slice(0, 10)}` : ''}).`).join(' ');
    if (g.length > 1 && g.every((x) => said(x) === said(s))) {
      if (said(s)) sentences.push(`Each: ${said(s).charAt(0).toLowerCase()}${said(s).slice(1)}`);
    } else for (const x of g) if (said(x)) sentences.push(`${g.length > 1 ? `${md(x.dataset ?? x.name)}: ` : ''}${said(x)}`);
    if (s.references.length) sentences.push(`References: ${numbers(s.references)}.`);
    lines.push(`${label} ${parts.join(', ')}. ${sentences.join(' ')}`.trim(), '');
  }
  if (record.software.length) {
    lines.push('## Software', '');
    for (const x of record.software) lines.push(`- ${md(`${x.name} ${x.version}`.trim())}`);
    lines.push('');
  }
  if (record.references.length) {
    lines.push('## References', '');
    for (const r of record.references) lines.push(`${r.n}. ${md(r.text)}${r.doi && !r.text.includes(r.doi) ? ` doi:${md(r.doi)}` : ''}`);
    lines.push('');
  }
  return lines.join('\n');
}

// A kind of step, of many: "Spatial analyses", "Cohort summaries", "Cluster names".
export const plural = (kind) => (kind.endsWith('sis') ? `${kind.slice(0, -3)}ses` : kind.endsWith('s') ? kind : kind.endsWith('y') ? `${kind.slice(0, -1)}ies` : `${kind}s`);

export function decisionWords(kind) {
  return {
    'promote-segmentation': 'Chosen as the dataset\'s segmentation',
    'keep-segmentation': 'Kept for exploration',
    'reject-segmentation': 'Rejected',
    'use-transform': 'Chosen to place the image',
  }[kind] ?? kind;
}

// The record as JSON, its keys in a fixed order.
export function recordJSON(record) {
  return `${JSON.stringify(record, null, 2)}\n`;
}

// The record's references as BibTeX.
export function recordBibTeX(record) {
  return referencesBibTeX(record.references, record.program.version);
}

// References ([{ n, key, text, doi }]) as BibTeX: the registry's under their keys, SituCraft (at a
// version) as software, others as notes under ref<n>.
export function referencesBibTeX(references, version = '') {
  const entries = references.map((r) => {
    if (r.key === 'situcraft') return bibtexEntry('situcraft', situcraftReference(version));
    if (r.key && REFERENCES[r.key]) return bibtexEntry(r.key, REFERENCES[r.key]);
    const fields = [`  note = {${String(r.text).replace(/[{}]/g, '')}}`, ...(r.doi ? [`  doi = {${r.doi}}`] : [])];
    return `@misc{ref${r.n},\n${fields.join(',\n')}\n}`;
  });
  return entries.length ? `${entries.join('\n\n')}\n` : '';
}
