// The segmentations of a project's datasets: records of artifacts of kind 'segmentation' that the
// program keeps (internal/artifacts), and the decisions promoting one per dataset as its default.
// A decision is never removed: the promoted segmentation is the one the dataset's latest decision
// names (among those still complete). The instrument's segmentation is registered when its
// dataset is added and promoted then, the dataset's only segmentation, with that reason recorded.

// The method of segmentations with errors planted for practice (internal/segment/plant).
export const PLANTED_METHOD = 'SituCraft planted errors';

// Where a segmentation comes from: the instrument (a bundle's own, without parameters), an import
// of another tool's (made from imported files), errors planted for practice (simulated), or
// SituCraft (computed).
function sourceOf(lineage) {
  if (lineage.parents?.some((p) => p.kind === 'import')) return 'imported';
  if (lineage.method?.name === PLANTED_METHOD) return 'planted';
  if (lineage.parents?.some((p) => p.kind === 'bundle') && !lineage.parameters) return 'instrument';
  return 'situcraft';
}

// The project's record of a segmentation artifact (its manifest, as the program answers it).
export function segmentationRecord(dataset, manifest, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  const method = lineage.method ?? {};
  return {
    kind: 'segmentation',
    id: manifest.id,
    name: manifest.name,
    dataset: dataset.id,
    source: sourceOf(lineage),
    state: manifest.state,
    created: manifest.created,
    registered: now,
    method: { name: method.name ?? '', version: method.version ?? '', citation: method.citation ?? '', methods: method.detail?.methods ?? [] },
    parameters: lineage.parameters ?? null,
    software: lineage.software ?? [],
    job: lineage.job ? { id: lineage.job.id, kind: lineage.job.kind } : null,
    parents: lineage.parents ?? [],
    files: (manifest.files ?? []).map(({ path, role, size, sha256, location }) => ({ path, role, size, sha256, location: location ?? '' })),
    // An import's sources: each path imported, with the number and size of the files read under
    // it and their digest (the manifest lists every file).
    sources: (method.detail?.sources ?? []).map(({ path, files, size, sha256 }) => ({ path, files, size, sha256 })),
    summary: manifest.summary ?? null,
    findings: manifest.findings ?? [],
  };
}

export function segmentationsOf(project, datasetId) {
  return project.artifacts.filter((a) => a.kind === 'segmentation' && a.dataset === datasetId);
}

// The project with a segmentation recorded (unchanged when it already is).
export function addSegmentation(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'segmentation' && a.id === record.id && a.dataset === record.dataset)) return project;
  const dataset = project.datasets.find((d) => d.id === record.dataset);
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'segmentation', detail: `Registered ${record.name.toLowerCase()} of ${dataset?.name ?? record.dataset} (artifact ${record.id}, ${record.files.length} files by SHA-256)` }],
  };
}

// The project with a segmentation promoted as its dataset's default, for a reason (required).
export function promoteSegmentation(project, { dataset, segmentation, rationale, by = 'the user', now = new Date().toISOString() }) {
  const record = project.artifacts.find((a) => a.kind === 'segmentation' && a.id === segmentation && a.dataset === dataset);
  if (!record) throw new Error('The dataset has no such segmentation.');
  if (record.state !== 'complete') throw new Error(`${record.name} is ${record.state}: only a complete segmentation can be promoted.`);
  if (record.source === 'planted') throw new Error(`${record.name} is simulated, its errors planted for practice: it cannot become the default.`);
  const reason = String(rationale ?? '').trim();
  if (!reason) throw new Error('Say why it is promoted: the reason is recorded with the decision.');
  const name = project.datasets.find((d) => d.id === dataset)?.name ?? dataset;
  return {
    ...project,
    modified: now,
    decisions: [...project.decisions, { kind: 'promote-segmentation', dataset, artifact: segmentation, rationale: reason, by, time: now }],
    history: [...project.history, { time: now, action: 'decision', detail: `Promoted ${record.name.toLowerCase()} as ${name}'s segmentation: ${reason}` }],
  };
}

// The dataset's promoted segmentation and the decision promoting it, or null.
export function promotedSegmentation(project, datasetId) {
  const rejected = new Set();
  for (let i = project.decisions.length - 1; i >= 0; i -= 1) {
    const d = project.decisions[i];
    if (d.kind === 'reject-segmentation' && d.dataset === datasetId) rejected.add(d.artifact);
    if (d.kind !== 'promote-segmentation' || d.dataset !== datasetId || rejected.has(d.artifact)) continue;
    const record = project.artifacts.find((a) => a.kind === 'segmentation' && a.id === d.artifact && a.dataset === datasetId && a.state === 'complete');
    if (record) return { record, decision: d };
  }
  return null;
}

// The reason recorded when the instrument's segmentation becomes a dataset's default by itself.
export const INSTRUMENT_DEFAULT = 'The instrument\'s segmentation: the dataset\'s only segmentation when it was added.';

// The project with a dataset's instrument segmentation recorded, and promoted when the dataset
// has no promoted segmentation yet.
export function withInstrumentSegmentation(project, dataset, manifest, now = new Date().toISOString()) {
  let next = addSegmentation(project, segmentationRecord(dataset, manifest, now), now);
  if (manifest.state === 'complete' && !promotedSegmentation(next, dataset.id)) {
    next = promoteSegmentation(next, { dataset: dataset.id, segmentation: manifest.id, rationale: INSTRUMENT_DEFAULT, by: 'SituCraft', now });
  }
  return next;
}

// --- Comparisons of two segmentations (artifacts of kind 'comparison', internal/compare) ---------

// The project's record of a comparison artifact: the two segmentations (A and B), its headline
// numbers, how it was made.
export function comparisonRecord(dataset, manifest, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  const [a, b] = (lineage.parents ?? []).filter((p) => p.kind === 'segmentation');
  return {
    kind: 'comparison',
    id: manifest.id,
    name: manifest.name,
    dataset: dataset.id,
    a: a?.id ?? null,
    b: b?.id ?? null,
    state: manifest.state,
    created: manifest.created,
    registered: now,
    method: { name: lineage.method?.name ?? '', version: lineage.method?.version ?? '', citation: lineage.method?.citation ?? '' },
    parameters: lineage.parameters ?? null,
    job: lineage.job ? { id: lineage.job.id, kind: lineage.job.kind } : null,
    summary: manifest.summary ?? null,
    findings: manifest.findings ?? [],
  };
}

export function comparisonsOf(project, datasetId) {
  return project.artifacts.filter((a) => a.kind === 'comparison' && a.dataset === datasetId);
}

// The project with a comparison recorded (unchanged when it already is).
export function addComparison(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'comparison' && a.id === record.id && a.dataset === record.dataset)) return project;
  const names = (id) => project.artifacts.find((a) => a.kind === 'segmentation' && a.id === id)?.name ?? id;
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'comparison', detail: `Compared ${names(record.a)} (A) with ${names(record.b)} (B) (artifact ${record.id})` }],
  };
}

// The decisions resting on a comparison (a promotion made from it, say).
export function decisionsCiting(project, comparisonId) {
  return project.decisions.filter((d) => d.comparison === comparisonId);
}

// The project without a comparison, the removal recorded in its history. A comparison a decision
// cites stays: the methods paragraph tells how the default was chosen from it. Its files are left
// to the program (api_compare.go), which moves them to the trash once no project names it.
export function removeComparison(project, id, now = new Date().toISOString()) {
  const record = project.artifacts.find((a) => a.kind === 'comparison' && a.id === id);
  if (!record) throw new Error('The project has no such comparison.');
  const names = (sid) => project.artifacts.find((a) => a.kind === 'segmentation' && a.id === sid)?.name ?? sid;
  const citing = decisionsCiting(project, id);
  if (citing.length) {
    const d = citing.at(-1);
    const done = Object.values(DECISIONS).find((x) => x.kind === d.kind)?.done.toLowerCase() ?? 'decided';
    throw new Error(`The comparison of ${names(record.a)} with ${names(record.b)} cannot be removed: ${names(d.artifact).toLowerCase()} was ${done} from it, and the methods paragraph says so.`);
  }
  return {
    ...project,
    modified: now,
    artifacts: project.artifacts.filter((a) => a !== record),
    history: [...project.history, { time: now, action: 'comparison', detail: `Removed the comparison of ${names(record.a)} (A) with ${names(record.b)} (B) (artifact ${id})` }],
  };
}

// --- Decisions about segmentations ---------------------------------------------------------------

// What can be decided of a segmentation: promote it (the dataset's default), keep it for
// exploration, or reject it. Each decision has a reason, is appended, and is never removed; a
// segmentation's status is its latest decision.
export const DECISIONS = {
  promote: { kind: 'promote-segmentation', label: 'Promote', done: 'Promoted' },
  keep: { kind: 'keep-segmentation', label: 'Keep for exploration', done: 'Kept for exploration' },
  reject: { kind: 'reject-segmentation', label: 'Reject', done: 'Rejected' },
};

// The latest decision about a segmentation of a dataset, or null.
export function segmentationDecision(project, datasetId, segmentationId) {
  for (let i = project.decisions.length - 1; i >= 0; i -= 1) {
    const d = project.decisions[i];
    if (d.dataset === datasetId && d.artifact === segmentationId && Object.values(DECISIONS).some((x) => x.kind === d.kind)) return d;
  }
  return null;
}

// The project with a decision about a segmentation: promote, keep or reject, with a reason
// (required); comparison names the comparison it was made from, if any. The default cannot be
// rejected (promote another first), and only a complete segmentation can be promoted or kept.
export function decideSegmentation(project, { dataset, segmentation, decision, rationale, comparison = null, by = 'the user', now = new Date().toISOString() }) {
  const what = DECISIONS[decision];
  if (!what) throw new Error(`A decision is ${Object.keys(DECISIONS).join(', ')}.`);
  const record = project.artifacts.find((a) => a.kind === 'segmentation' && a.id === segmentation && a.dataset === dataset);
  if (!record) throw new Error('The dataset has no such segmentation.');
  const reason = String(rationale ?? '').trim();
  if (!reason) throw new Error('Say why: the reason is recorded with the decision.');
  if (decision === 'promote') {
    const next = promoteSegmentation(project, { dataset, segmentation, rationale: reason, by, now });
    if (!comparison) return next;
    const decisions = [...next.decisions];
    decisions[decisions.length - 1] = { ...decisions.at(-1), comparison };
    return { ...next, decisions };
  }
  if (record.state !== 'complete' && decision === 'keep') throw new Error(`${record.name} is ${record.state}: only a complete segmentation is kept for exploration.`);
  if (decision === 'reject' && promotedSegmentation(project, dataset)?.record.id === segmentation) {
    throw new Error(`${record.name} is the default: promote another before rejecting it.`);
  }
  const name = project.datasets.find((d) => d.id === dataset)?.name ?? dataset;
  return {
    ...project,
    modified: now,
    decisions: [...project.decisions, { kind: what.kind, dataset, artifact: segmentation, rationale: reason, by, time: now, ...(comparison ? { comparison } : {}) }],
    history: [...project.history, { time: now, action: 'decision', detail: `${what.done} ${record.name.toLowerCase()} of ${name}: ${reason}` }],
  };
}

// --- Exports (internal/export) -------------------------------------------------------------------

// The formats exported (internal/export), each with what it holds.
export const EXPORT_FORMATS = {
  spatialdata: { label: 'SpatialData Zarr', about: 'Cells as labels and polygons, transcripts as points with their cells, counts as an AnnData table; µm (spatialdata, napari-spatialdata)' },
  anndata: { label: 'AnnData Zarr', about: 'Counts with each cell\'s measures and centroid (scanpy, anndata; Zarr format 2)' },
  parquet: { label: 'Parquet', about: 'Cells, transcripts with their cells, outlines and counts, in Xenium\'s column names' },
  csv: { label: 'CSV', about: 'The same four tables, gzipped' },
  mex: { label: 'Matrix Market', about: 'The counts as Xenium and Cell Ranger write them (Seurat\'s Read10X)' },
  geojson: { label: 'GeoJSON', about: 'Each cell\'s outline as a polygon naming it, µm (QuPath, GIS tools)' },
  'xenium-ranger': { label: 'Xenium Ranger inputs', about: 'Label masks and the import-segmentation command, its inputs checked (uncompressed: about 8 bytes a pixel)' },
};

// The project's record of an export: what was written where, from which segmentation, by which
// SituCraft, with its totals and content fingerprints (situcraft-export.json lists every file by
// its SHA-256). An export leaves the project's keeping: the record says what it was.
export function exportRecord(dataset, segmentation, manifest, folder, now = new Date().toISOString()) {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`;
  return {
    kind: 'export',
    id: `out-${random.replace(/-/g, '').slice(0, 16)}`,
    dataset: dataset.id,
    segmentation: segmentation.id,
    name: segmentation.name,
    formats: Object.keys(manifest.formats ?? {}),
    folder,
    created: manifest.created ?? now,
    recorded: now,
    software: manifest.software ?? '',
    files: (manifest.files ?? []).length,
    size: (manifest.files ?? []).reduce((sum, f) => sum + (f.size ?? 0), 0),
    counts: manifest.counts ?? null,
    content: manifest.content ?? null,
    findings: (manifest.findings ?? []).map(({ format, severity, code, message }) => ({ format, severity, code, message })),
    xeniumRanger: manifest.xeniumRanger?.command ?? null,
  };
}

export function exportsOf(project, datasetId) {
  return (project.exports ?? []).filter((e) => e.dataset === datasetId);
}

// The project with an export recorded.
export function addExport(project, record, now = new Date().toISOString()) {
  const dataset = project.datasets.find((d) => d.id === record.dataset);
  return {
    ...project,
    modified: now,
    exports: [...(project.exports ?? []), record],
    history: [...project.history, { time: now, action: 'export', detail: `Exported ${record.name.toLowerCase()} of ${dataset?.name ?? record.dataset} as ${record.formats.join(', ')} into ${record.folder} (${record.files} files by SHA-256)` }],
  };
}
