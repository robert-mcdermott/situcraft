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

// RGBA bytes per cell for a fill by cluster: each cluster in its categorical color (or, given
// colorOf, the color of the cluster it maps to: merged clusters share one), cells not analyzed (−1)
// transparent.
export function clusterColors(cluster, colorOf = (c) => c) {
  const out = new Uint8Array(4 * cluster.length);
  const cache = new Map();
  for (let i = 0; i < cluster.length; i += 1) {
    const c = cluster[i];
    if (c < 0) continue;
    if (!cache.has(c)) {
      const v = parseInt(categoricalColor(colorOf(c)).slice(1), 16);
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

// --- Maps (internal/embed): artifacts of kind 'embedding' -----------------------------------------

export function embeddingRecord(dataset, manifest, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  const parent = (kind) => (lineage.parents ?? []).find((p) => p.kind === kind)?.id ?? null;
  return {
    kind: 'embedding',
    id: manifest.id,
    name: manifest.name,
    dataset: dataset.id,
    segmentation: parent('segmentation'),
    clustering: parent('clustering'),
    state: manifest.state,
    created: manifest.created,
    registered: now,
    method: { name: lineage.method?.name ?? '', version: lineage.method?.version ?? '', citation: lineage.method?.citation ?? '' },
    parameters: lineage.parameters ?? null,
    summary: manifest.summary ?? null,
    findings: manifest.findings ?? [],
  };
}

export function addEmbedding(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'embedding' && a.id === record.id && a.dataset === record.dataset)) return project;
  const dataset = project.datasets.find((d) => d.id === record.dataset);
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'embedding', detail: `Made ${record.name.charAt(0).toLowerCase()}${record.name.slice(1)} of ${dataset?.name ?? record.dataset}: trustworthiness ${Number.isFinite(record.summary?.trustworthiness) ? record.summary.trustworthiness.toFixed(3) : '?'} (artifact ${record.id})` }],
  };
}

// The maps of a segmentation's cells: SituCraft's of its clusterings, and the instrument's.
export function mapsOf(project, datasetId, segmentationId) {
  return project.artifacts.filter((a) => a.kind === 'embedding' && a.dataset === datasetId && a.segmentation === segmentationId && a.state === 'complete');
}

// --- Cluster names: versions of a clustering's names, the original (numbers) always kept ---------

// The latest names of a clustering's clusters, or null.
export function clusterNames(project, clusteringId) {
  const list = project.artifacts.filter((a) => a.kind === 'cluster-names' && a.clustering === clusteringId);
  return list.length ? list.reduce((a, b) => (b.version > a.version ? b : a)) : null;
}

// A cluster's name in a version of names (or its number).
export function nameOf(names, k) {
  return names?.names?.[k] || clusterName(k);
}

// The project with a new version of a clustering's names (an array, a name or null per cluster),
// appended with what changed; names given to several clusters merge them on the tissue and the map.
export function nameClusters(project, { clustering, names, by = 'the user', now = new Date().toISOString(), id = null }) {
  const record = project.artifacts.find((a) => a.kind === 'clustering' && a.id === clustering);
  if (!record) throw new Error('The project has no such clustering.');
  const count = record.summary?.clusters ?? names.length;
  const clean = Array.from({ length: count }, (_, k) => {
    const v = String(names[k] ?? '').trim();
    return v || null;
  });
  const previous = clusterNames(project, clustering);
  const changed = clean.map((v, k) => [k, v]).filter(([k, v]) => (previous?.names?.[k] ?? null) !== v);
  if (!changed.length) return project;
  const version = (previous?.version ?? 0) + 1;
  const entry = {
    kind: 'cluster-names',
    id: id ?? `cn-${clustering.slice(4, 14)}-${version}`,
    clustering,
    dataset: record.dataset,
    version,
    parent: previous?.id ?? null,
    names: clean,
    by,
    created: now,
  };
  const said = changed.slice(0, 4).map(([k, v]) => (v ? `${clusterName(k)} "${v}"` : `${clusterName(k)} unnamed`)).join(', ');
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, entry],
    history: [...project.history, { time: now, action: 'cluster-names', detail: `Named the clusters of ${record.name.toLowerCase()} (version ${version}): ${said}${changed.length > 4 ? `, and ${changed.length - 4} more` : ''}` }],
  };
}

// Each cluster's representative under its names: clusters sharing a name map to the first of them.
export function mergedOf(names, count) {
  const first = new Map();
  return Array.from({ length: count }, (_, k) => {
    const n = names?.names?.[k];
    if (!n) return k;
    if (!first.has(n)) first.set(n, k);
    return first.get(n);
  });
}

// --- Comparing two clusterings of the same cells ---------------------------------------------------

// The contingency of two clusterings (each cluster per cell, −1 for a cell not clustered) over the
// cells both cluster: { table: [a][b] counts, cells, rowSums, colSums, ari } (the adjusted Rand index
// of Hubert and Arabie 1985, as internal/cluster computes it).
export function contingency(a, b, countA, countB) {
  const table = Array.from({ length: countA }, () => new Array(countB).fill(0));
  let cells = 0;
  for (let i = 0; i < Math.min(a.length, b.length); i += 1) {
    if (a[i] < 0 || b[i] < 0 || a[i] >= countA || b[i] >= countB) continue;
    table[a[i]][b[i]] += 1;
    cells += 1;
  }
  const rowSums = table.map((r) => r.reduce((x, y) => x + y, 0));
  const colSums = Array.from({ length: countB }, (_, j) => table.reduce((x, r) => x + r[j], 0));
  const pairs = (n) => (n * (n - 1)) / 2;
  let index = 0;
  for (const r of table) for (const v of r) index += pairs(v);
  const sa = rowSums.reduce((x, v) => x + pairs(v), 0);
  const sb = colSums.reduce((x, v) => x + pairs(v), 0);
  const expected = cells > 1 ? (sa * sb) / pairs(cells) : 0;
  const maximum = (sa + sb) / 2;
  const ari = maximum === expected ? 1 : (index - expected) / (maximum - expected);
  return { table, cells, rowSums, colSums, ari };
}

// What a map's quality says, in plain words: { level: 'info' | 'warning', text }, or null.
export function mapWarning(q) {
  if (!q || !Number.isFinite(q.trustworthiness)) return null;
  const t = q.trustworthiness;
  const pct = (v) => `${Math.round(100 * v)}%`;
  const keep = Number.isFinite(q.recall) ? `: a cell's 15 nearest cells in expression are, on average, ${pct(q.recall)} among its 75 nearest on the map` : '';
  const space = q.source ? ' against the instrument\'s own components' : '';
  const found = q.search ? ` (the 75 nearest in expression found by NN-Descent: ${(100 * q.search.recall).toFixed(1)}% of the exact on ${q.search.queries.toLocaleString('en-US')} cells)` : '';
  if (t >= 0.9) return { level: 'info', text: `The map keeps neighborhoods well (trustworthiness ${t.toFixed(3)}${space})${keep}${found}.` };
  return { level: 'warning', text: `The map distorts${t < 0.8 ? ' much' : ''} (trustworthiness ${t.toFixed(3)}${space})${keep}${found}. It shows which cells are alike, not how alike: distances between groups, and their sizes, mean little. Judge relationships on the clusters and their markers.` };
}
