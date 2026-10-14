// The segmentations of a project's datasets: records of artifacts of kind 'segmentation' that the
// program keeps (internal/artifacts), and the decisions promoting one per dataset as its default.
// A decision is never removed: the promoted segmentation is the one the dataset's latest decision
// names (among those still complete). The instrument's segmentation is registered when its
// dataset is added and promoted then, the dataset's only segmentation, with that reason recorded.

// Where a segmentation comes from: the instrument (a bundle's own, without parameters), an import
// of another tool's (made from imported files), or SituCraft (computed).
function sourceOf(lineage) {
  if (lineage.parents?.some((p) => p.kind === 'import')) return 'imported';
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
  for (let i = project.decisions.length - 1; i >= 0; i -= 1) {
    const d = project.decisions[i];
    if (d.kind !== 'promote-segmentation' || d.dataset !== datasetId) continue;
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
