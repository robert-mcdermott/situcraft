// Clusterings of a segmentation's cells (internal/cluster, api_clustering.go): artifacts of kind
// 'clustering' the program makes, recorded in the project with the segmentation they cluster, and
// what the page says of their parameters and clusters.

import { keepBytes } from './filters.js';
import { categoricalColor } from './colormaps.js';

// The parameters, as the program names them, with what each does (the defaults come from the
// program: /api/clustering/defaults).
export const PARAMETERS = [
  { id: 'minTranscripts', label: 'Gene transcripts at least', step: 1, min: 0, max: 100000, help: 'The cells analyzed have at least this many gene transcripts of quality 20 or more (when no QC filter chooses them). Cells with very few transcripts cluster by noise.' },
  { id: 'components', label: 'Principal components', step: 1, min: 2, max: 200, help: 'The neighbors are found in the first principal components of the scaled expression. Fewer keep only the strongest patterns; more keep finer ones, and more noise.' },
  { id: 'neighbors', label: 'Neighbors', step: 1, min: 2, max: 200, help: 'Each cell\'s neighborhood in the graph, the cell itself included (as scanpy counts). More neighbors give broader, smoother clusters.' },
  { id: 'resolution', label: 'Resolution', step: 0.1, min: 0.01, max: 50, help: 'Leiden\'s resolution: higher gives more and smaller clusters, lower fewer and larger. There is no right value; try a few and compare the markers.' },
  { id: 'seed', label: 'Seed', step: 1, min: 0, max: 2 ** 53, help: 'Leiden\'s random order of moves. The same seed gives the same clusters on any computer; another seed shows how much of the result is chance.' },
];

// The project's record of a clustering artifact.
export function clusteringRecord(dataset, manifest, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  const seg = (lineage.parents ?? []).find((p) => p.kind === 'segmentation');
  return {
    kind: 'clustering',
    id: manifest.id,
    name: manifest.name,
    dataset: dataset.id,
    segmentation: seg?.id ?? null,
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

export function clusteringsOf(project, datasetId, segmentationId = null) {
  return project.artifacts.filter((a) => a.kind === 'clustering' && a.dataset === datasetId && (!segmentationId || a.segmentation === segmentationId));
}

// The project with a clustering recorded (unchanged when it already is).
export function addClustering(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'clustering' && a.id === record.id && a.dataset === record.dataset)) return project;
  const seg = project.artifacts.find((a) => a.kind === 'segmentation' && a.id === record.segmentation);
  const dataset = project.datasets.find((d) => d.id === record.dataset);
  const s = record.summary ?? {};
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'clustering', detail: `Clustered the cells of ${(seg?.name ?? 'a segmentation').toLowerCase()} of ${dataset?.name ?? record.dataset}: ${s.clusters ?? '?'} clusters of ${s.cells ?? '?'} cells at resolution ${record.parameters?.resolution ?? '?'} (artifact ${record.id})` }],
  };
}

// A cluster's name, counted from 1 as people count them.
export function clusterName(index) {
  return `Cluster ${index + 1}`;
}

// What a clustering is, in a line: its clusters, cells, resolution and the cells it analyzed.
export function describeClustering(c) {
  const s = c.summary ?? {};
  const p = c.parameters ?? {};
  const cells = s.filter ? `the cells the filter ${s.filter.name} keeps` : `cells with at least ${p.minTranscripts ?? '?'} gene transcripts`;
  return `${s.clusters ?? '?'} clusters of ${(s.cells ?? 0).toLocaleString('en-US')} ${cells}, resolution ${p.resolution ?? '?'}, ${p.neighbors ?? '?'} neighbors in ${s.components ?? p.components ?? '?'} components, seed ${p.seed ?? '?'}`;
}

// A QC filter's cells as the program reads them: { id, name, checksum, keep } with keep in base64.
export function filterRequest(filter, keep) {
  const bytes = keepBytes(keep);
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return { id: filter.id, name: `${filter.name} v${filter.version}`, checksum: filter.result.sha256, keep: btoa(binary) };
}

// The parameters checked as the program checks them (internal/cluster Params.Check): an error's
// message, or null.
export function checkParameters(p) {
  for (const spec of PARAMETERS) {
    const v = p[spec.id];
    if (!Number.isFinite(v) || v < spec.min || v > spec.max || (spec.step === 1 && !Number.isInteger(v))) {
      return `${spec.label} is ${spec.step === 1 ? 'a whole number ' : ''}from ${spec.min.toLocaleString('en-US')} to ${spec.max.toLocaleString('en-US')}.`;
    }
  }
  if (p.resolution <= 0) return 'The resolution is above 0.';
  return null;
}

// RGBA bytes per cell for a fill by cluster: each cluster in its categorical color, cells not
// analyzed (−1) transparent.
export function clusterColors(cluster) {
  const out = new Uint8Array(4 * cluster.length);
  const cache = new Map();
  for (let i = 0; i < cluster.length; i += 1) {
    const c = cluster[i];
    if (c < 0) continue;
    if (!cache.has(c)) {
      const v = parseInt(categoricalColor(c).slice(1), 16);
      cache.set(c, [(v >> 16) & 255, (v >> 8) & 255, v & 255, 255]);
    }
    out.set(cache.get(c), 4 * i);
  }
  return out;
}

// The fills a clustering offers the Viewer: 'clusters:<id>' for each complete clustering of a
// segmentation.
export function clusterFills(project, datasetId, segmentationId) {
  return clusteringsOf(project, datasetId, segmentationId).filter((c) => c.state === 'complete').map((c) => ({
    id: `clusters:${c.id}`,
    clustering: c.id,
    label: `Clusters, resolution ${c.parameters?.resolution ?? '?'}${c.summary?.filter ? `, filter ${c.summary.filter.name}` : ''} (${c.summary?.clusters ?? '?'})`,
    help: describeClustering(c),
  }));
}
