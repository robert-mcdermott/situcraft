// Datasets in a project: each a Xenium output bundle recorded by where it is, what it is (the run's
// identifiers and a fingerprint of its files) and how it validated. The bundle itself is never
// copied into the project. Pure functions: each returns a new project and leaves its argument alone.

import { touch } from './project.js';

const DESCRIPTION_FILES = new Set(['experiment.xenium', 'metrics_summary.csv', 'gene_panel.json']);

function newId() {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  return `d-${random.replace(/-/g, '').slice(0, 16)}`;
}

// The record of a validated bundle, as the project keeps it.
export function datasetRecord(item, report, options = {}) {
  const s = report.summary;
  const count = (severity) => report.findings.filter((f) => f.severity === severity).length;
  return {
    id: options.id ?? newId(),
    name: report.bundle,
    platform: 'Xenium',
    path: item.path,
    zipped: Boolean(item.zipped ?? s.zipped),
    software: s.software ?? '',
    version: report.version,
    experimentUuid: report.experimentUuid ?? '',
    analysisUuid: report.analysisUuid ?? '',
    panel: s.panel ?? '',
    organism: s.organism ?? '',
    tissue: s.tissue ?? '',
    run: s.run ?? '',
    region: s.region ?? '',
    runStart: s.runStart ?? '',
    cells: s.cells,
    transcripts: s.transcripts,
    pixelSizeUm: s.pixelSizeUm ?? 0,
    extentUm: s.extentUm ?? null,
    channels: s.channels ?? [],
    zPlanes: s.zPlanes ?? 0,
    fingerprint: (report.fingerprint ?? []).map(({ path, size, sha256 }) => (sha256 ? { path, size, sha256 } : { path, size })),
    validation: {
      checked: report.checked,
      support: report.support,
      fatal: count('fatal'),
      warnings: count('warning'),
      info: count('info'),
      metricsCompared: s.metricsCompared ?? 0,
      metricsAgreeing: s.metricsAgreeing ?? 0,
      findings: report.findings.map(({ severity, code, title, count: n }) => (n ? { severity, code, title, count: n } : { severity, code, title })),
    },
    added: options.now ?? new Date().toISOString(),
  };
}

// The dataset already in the project for this bundle (the same place, or the same run and
// analysis), or null.
export function findDataset(project, { path, experimentUuid, analysisUuid }) {
  return project.datasets.find((d) => d.path === path || (experimentUuid && d.experimentUuid === experimentUuid && d.analysisUuid === analysisUuid)) ?? null;
}

// Adds a dataset. A bundle with a fatal finding, or one already present, is refused with a reason.
// An untitled project takes the first dataset's name.
export function addDataset(project, record, now) {
  if (record.validation.fatal > 0) throw new Error(`${record.name} has ${record.validation.fatal} fatal problem${record.validation.fatal === 1 ? '' : 's'}; it cannot be added until they are fixed.`);
  const existing = findDataset(project, record);
  if (existing) throw new Error(`${record.name} is already in this project (as ${existing.name}).`);
  const next = { ...project, datasets: [...project.datasets, record] };
  if (project.name === 'Untitled project' && project.datasets.length === 0) next.name = record.name;
  next.history = [...project.history, { time: now ?? new Date().toISOString(), action: 'add-dataset', detail: `${record.name} (${record.path})` }];
  return touch(next, now);
}

export function removeDataset(project, id, now) {
  const dataset = project.datasets.find((d) => d.id === id);
  if (!dataset) return project;
  return touch({
    ...project,
    datasets: project.datasets.filter((d) => d.id !== id),
    history: [...project.history, { time: now ?? new Date().toISOString(), action: 'remove-dataset', detail: dataset.name }],
  }, now);
}

// Records a dataset's new place (a bundle that was moved), after its fingerprint was checked.
export function relocateDataset(project, id, path, now) {
  const dataset = project.datasets.find((d) => d.id === id);
  if (!dataset || dataset.path === path) return project;
  return touch({
    ...project,
    datasets: project.datasets.map((d) => (d.id === id ? { ...d, path } : d)),
    history: [...project.history, { time: now ?? new Date().toISOString(), action: 'relocate-dataset', detail: `${dataset.name}: ${dataset.path} → ${path}` }],
  }, now);
}

// Compares a dataset's recorded fingerprint with the bundle as it is now:
//   'same'          every file is there with its recorded size, and the run's description files
//                   are byte for byte the ones recorded (times may differ: a copied bundle is the
//                   same bundle);
//   'different-run' its description files differ (another run, or another analysis of it);
//   'changed'       files were added, removed or changed size.
export function compareFingerprint(recorded, current) {
  const now = new Map(current.map((f) => [f.path, f]));
  const before = new Map(recorded.map((f) => [f.path, f]));
  for (const f of recorded) {
    if (DESCRIPTION_FILES.has(f.path) && f.sha256 && now.get(f.path)?.sha256 !== f.sha256) {
      return { status: 'different-run', files: [f.path] };
    }
  }
  const changed = [];
  for (const f of recorded) {
    const c = now.get(f.path);
    if (!c || c.size !== f.size) changed.push(f.path);
  }
  for (const f of current) if (!before.has(f.path)) changed.push(f.path);
  return changed.length ? { status: 'changed', files: changed } : { status: 'same', files: [] };
}
