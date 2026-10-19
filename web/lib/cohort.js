// A cohort's samples summarized the same way (wave 7, api_cohort.go, internal/cohort): the label
// names every sample is counted in, the summary as the project records it, and the cohort table
// (its table.csv) read into families of numbers, each with the artifact it came from.

import { parseTable } from './samples.js';

// The families of the table, in order, and what each number is.
export const FAMILIES = [
  { id: 'cells', label: 'Cells by label', unit: 'cells', digits: 0 },
  { id: 'share', label: 'Shares of the labeled cells', unit: 'share', digits: 3 },
  { id: 'density', label: 'Cells per mm² of tissue', unit: 'per mm²', digits: 1 },
  { id: 'niche', label: 'Niches\' shares (found across the samples)', unit: 'share', digits: 3 },
  { id: 'qc', label: 'Quality', unit: '', digits: 3 },
  { id: 'graph', label: 'The graph', unit: '', digits: 3 },
  { id: 'enrichment', label: 'Neighborhood enrichment (neighbors found ÷ expected)', unit: 'ratio', digits: 3 },
  { id: 'nearest', label: 'Nearest distances (median µm, from → to)', unit: 'µm', digits: 1 },
  { id: 'moran', label: 'Moran\'s I by gene', unit: 'I', digits: 4 },
  { id: 'pca', label: 'The pseudobulk\'s principal components (scores)', unit: 'score', digits: 3 },
];

// The questions whose numbers a summary reads (internal/cohort AnalysisStatistics).
export const SUMMARY_QUESTIONS = ['enrichment', 'distances', 'moran'];

// The cohort's names from a marker set's label sets (a labels description's sets) at a level: every
// label of the level (a label no sample's cells carry is counted as none, not left out), with those
// of coarser levels that cells carry there (used), in the set's order; then any other name used
// (never a state, which cells carry beside their label).
export function vocabularyOf(sets, level, used = new Set()) {
  const names = [];
  for (const s of sets) {
    if (s.state || names.includes(s.name)) continue;
    if ((s.level ?? 0) === level || ((s.level ?? 0) < level && used.has(s.name))) names.push(s.name);
  }
  const states = new Set(sets.filter((s) => s.state).map((s) => s.name));
  for (const n of used) if (!names.includes(n) && !states.has(n)) names.push(n);
  return names;
}

// The names a label source's cells carry (lib/spatial.js labelsRequest's, before it drops the
// unused).
export function usedNames(src) {
  const used = new Set();
  for (const l of src.label) if (l >= 0) used.add(src.names[l]);
  return used;
}

// A bundle open on this computer as a summary's request names it: its index among the open files.
export function itemIndex(item) {
  const m = /\/api\/local\/(\d+)$/.exec(item?.url ?? '');
  return m ? m[1] : null;
}

// A sample's graph: of its segmentation, complete, over exactly its cells (by their checksum), of
// the kind given when there is one; the latest made.
export function graphOfSample(project, datasetId, segmentationId, checksum, kind = null) {
  const list = project.artifacts.filter((a) => a.kind === 'graph' && a.dataset === datasetId && a.segmentation === segmentationId && a.state === 'complete' && a.summary?.choice?.checksum === checksum);
  const ofKind = kind ? list.filter((g) => (g.summary?.kind ?? g.parameters?.kind) === kind) : list;
  return [...ofKind].sort((a, b) => (a.created < b.created ? 1 : -1))[0] ?? null;
}

// A sample's analyses a summary reads over its graph (an enrichment of its labels, Moran's I):
// complete, the latest of each question.
export function graphAnalysesOf(project, datasetId, graphId, labelsId) {
  const list = project.artifacts.filter((a) => a.kind === 'spatial' && a.dataset === datasetId && a.graph === graphId && a.state === 'complete'
    && (a.question === 'moran' || (a.question === 'enrichment' && a.summary?.labels?.id === labelsId)) && !a.summary?.strata?.length);
  return latestByQuestion(list);
}

export function latestByQuestion(list) {
  const out = new Map();
  for (const a of [...list].sort((x, y) => (x.created < y.created ? 1 : -1))) if (!out.has(a.question)) out.set(a.question, a);
  return [...out.values()];
}

// A summary as the project records it: what it is of (the marker set and level, the samples by id)
// beside its manifest's lineage.
export function summaryRecord(manifest, { markerSet = null, level = 0, samples = [] } = {}, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  return {
    kind: 'cohort-summary',
    id: manifest.id,
    name: manifest.name,
    markerSet,
    level,
    samples,
    state: manifest.state,
    created: manifest.created,
    registered: now,
    method: { name: lineage.method?.name ?? '', version: lineage.method?.version ?? '', citation: lineage.method?.citation ?? '' },
    parameters: lineage.parameters ?? null,
    software: lineage.software ?? [],
    job: lineage.job ? { id: lineage.job.id, kind: lineage.job.kind } : null,
    summary: manifest.summary ?? null,
    findings: manifest.findings ?? [],
  };
}

// The project's summaries, the latest first.
export function summariesOf(project) {
  return project.artifacts.filter((a) => a.kind === 'cohort-summary').sort((a, b) => (a.created < b.created ? 1 : -1));
}

// The project with a summary recorded (unchanged when it already is).
export function addSummary(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'cohort-summary' && a.id === record.id)) return project;
  const s = record.summary ?? {};
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'cohort-summary', detail: `Summarized ${s.samples?.length ?? record.samples.length} samples by ${s.labels?.name ?? 'their labels'}: ${s.labels?.names?.length ?? '?'} labels, ${s.genes ?? '?'} genes${s.niches ? `, ${s.niches} niches across them` : ''} (artifact ${record.id})` }],
  };
}

// The cohort table (table.csv) read: { samples: [{ sample, name }], families: [{ id, label, unit,
// digits, keys, values: Map(sample → Map(key → { value, source })) }] }, families in FAMILIES's
// order, keys and samples in the table's.
export function readCohortTable(text) {
  const rows = parseTable(text);
  const want = ['sample', 'name', 'family', 'key', 'value', 'source'];
  if (!rows.length || rows[0].join(',') !== want.join(',')) throw new Error('not a cohort table: its header is not sample, name, family, key, value, source');
  const samples = [];
  const seen = new Set();
  const byFamily = new Map();
  for (const r of rows.slice(1)) {
    if (r.length !== want.length) throw new Error(`a row of ${r.length} fields, not ${want.length}`);
    const [sample, name, family, key, value, source] = r;
    if (!seen.has(sample)) {
      seen.add(sample);
      samples.push({ sample, name });
    }
    if (!byFamily.has(family)) byFamily.set(family, { keys: [], known: new Set(), values: new Map() });
    const f = byFamily.get(family);
    if (!f.known.has(key)) {
      f.known.add(key);
      f.keys.push(key);
    }
    if (!f.values.has(sample)) f.values.set(sample, new Map());
    f.values.get(sample).set(key, { value: Number(value), source });
  }
  const order = FAMILIES.map((f) => f.id);
  const families = [...byFamily.entries()]
    .sort(([a], [b]) => (order.indexOf(a) < 0 ? 99 : order.indexOf(a)) - (order.indexOf(b) < 0 ? 99 : order.indexOf(b)))
    .map(([id, f]) => ({ ...(FAMILIES.find((x) => x.id === id) ?? { id, label: id, unit: '', digits: 3 }), keys: f.keys, values: f.values }));
  return { samples, families };
}

// A family's numbers as a wide table: a row a sample, a column a key (empty where a sample has no
// such number), as CSV.
export function familyCSV(table, familyId) {
  const f = table.families.find((x) => x.id === familyId);
  if (!f) return '';
  const quote = (s) => (/[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s);
  const lines = [['sample', 'name', ...f.keys].map(quote).join(',')];
  for (const s of table.samples) {
    const v = f.values.get(s.sample);
    lines.push([s.sample, s.name, ...f.keys.map((k) => (v?.has(k) ? String(v.get(k).value) : ''))].map(quote).join(','));
  }
  return `${lines.join('\n')}\n`;
}

// --- Cohort QC (wave 7, slice 5) ---------------------------------------------------------------

// A summary's quality by sample: each sample's batch (its bundle's slide, run and panel), its value
// of each quality number the summary compared (null where its segmentation records none), and the
// numbers it is far out on (the summary's outliers, by metric).
export function qcRows(doc) {
  const flags = new Map();
  for (const o of doc.outliers ?? []) {
    if (!flags.has(o.sample)) flags.set(o.sample, {});
    flags.get(o.sample)[o.metric] = o;
  }
  const panelName = (p) => `${p?.design || p?.name || 'not recorded'}${p?.version ? ` ${p.version}` : ''}`;
  return doc.samples.map((s) => ({
    sample: s.sample,
    name: s.name,
    slide: s.bundle?.slide || 'not recorded',
    run: s.bundle?.run || 'not recorded',
    panel: panelName(s.bundle?.panel),
    values: Object.fromEntries((doc.qcMetrics ?? []).map((m) => [m.key, s.qc?.[m.key] ?? null])),
    flags: flags.get(s.sample) ?? {},
  }));
}

// The design's sentences about batches: the groups confounded with a slide, run or panel (it cannot
// be used), or batches that do not hold every group (it can, adjusted for the batch).
export function batchStatements(design) {
  return [
    ...(design.problems ?? []).filter((p) => /is confounded with the (slide|run|panel)\b/.test(p)).map((text) => ({ kind: 'confounded', text })),
    ...(design.notes ?? []).filter((n) => /^Not every (slide|run|panel)\b/.test(n)).map((text) => ({ kind: 'uneven', text })),
  ];
}

// A linear map of [lo, hi] onto [from, to], a little margin kept; a flat range onto the middle.
export function linearScale(lo, hi, from, to, margin = 0.08) {
  if (!(hi > lo)) return () => (from + to) / 2;
  const pad = (hi - lo) * margin;
  const a = lo - pad;
  const b = hi + pad;
  return (v) => from + ((v - a) / (b - a)) * (to - from);
}
