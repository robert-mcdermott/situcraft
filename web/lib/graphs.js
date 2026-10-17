// Graphs of a segmentation's cells (internal/spatial, api_graphs.go) on the page: their records in
// the project, each kind's parameter checked as the program checks it, the cells a graph is built
// over (all, a QC filter's, those in regions; never those a review excluded), the edges as the
// program answers them, a cell's neighbors, and the edges within a part of the tissue.

import { keepBytes, keepChecksum } from './filters.js';

// The kinds, as the Spatial view offers them, each with its parameter (as Params.Check bounds it).
export const KINDS = [
  {
    id: 'delaunay', label: 'Delaunay triangulation',
    param: { id: 'longest', label: 'Longest edge (µm)', min: 0, max: 1000, step: 1, integer: false, zero: 'none' },
    help: 'Joins cells whose centroids are neighbors in the Delaunay triangulation: each cell to the cells around it, however dense the tissue, with no parameter to tune. Across gaps (tears, holes, the tissue\'s edge) the triangulation joins cells far apart, so edges longer than the longest are left out (0 keeps every edge).',
  },
  {
    id: 'knn', label: 'Nearest neighbors',
    param: { id: 'k', label: 'Neighbors of each cell', min: 1, max: 100, step: 1, integer: true },
    help: 'Joins each cell to its k nearest cells (by centroid), and so to any cell of which it is among the k nearest: cells at the tissue\'s edge, or alone, reach farther for theirs. Every cell has at least k neighbors, whatever the density.',
  },
  {
    id: 'radius', label: 'Within a radius',
    param: { id: 'radius', label: 'Radius (µm)', min: 0, max: 500, step: 1, integer: false, above: true },
    help: 'Joins cells whose centroids are at most the radius apart: the same distance everywhere, so dense tissue gives cells many neighbors and sparse tissue few or none. A radius of about two cell diameters (15 to 25 µm) reaches a cell\'s immediate neighbors and the next ones out.',
  },
  {
    id: 'contact', label: 'Contact',
    param: { id: 'gap', label: 'Gap closed (µm)', min: 0, max: 20, step: 0.5, integer: false },
    help: 'Joins cells whose outlines touch in the segmentation, with the length of boundary they share: physical contact, as far as the segmentation drew it. A gap lets cells that nearly touch count as in contact (each grown by half of it into space no cell holds); a cell between two others still keeps them apart.',
  },
];

export const DEFAULTS = {
  delaunay: { kind: 'delaunay', longest: 50 },
  knn: { kind: 'knn', k: 6 },
  radius: { kind: 'radius', radius: 20 },
  contact: { kind: 'contact', gap: 0 },
};

// What is wrong with a graph's parameters, as the program would say it, or null.
export function checkGraphParams(p) {
  const kind = KINDS.find((k) => k.id === p?.kind);
  if (!kind) return `No graph of kind ${p?.kind ?? '(none)'}.`;
  const spec = kind.param;
  const v = p[spec.id];
  const ok = Number.isFinite(v) && (spec.above ? v > spec.min : v >= spec.min) && v <= spec.max && (!spec.integer || Number.isInteger(v));
  if (!ok) return `${spec.label} is ${spec.integer ? 'a whole number ' : ''}${spec.above ? 'more than' : 'from'} ${spec.min} ${spec.above ? 'and at most' : 'to'} ${spec.max.toLocaleString('en-US')}.`;
  return null;
}

// The parameters of a graph's kind alone (as Params.Check keeps them).
export function graphParams(p) {
  const spec = KINDS.find((k) => k.id === p.kind)?.param;
  return spec ? { kind: p.kind, [spec.id]: p[spec.id] } : { kind: p.kind };
}

// A graph's definition in words: "Delaunay, edges to 50 µm".
export function describeGraph(p) {
  switch (p?.kind) {
    case 'delaunay': return p.longest > 0 ? `Delaunay, edges to ${p.longest} µm` : 'Delaunay, every edge';
    case 'knn': return `${p.k} nearest neighbors`;
    case 'radius': return `Within ${p.radius} µm`;
    case 'contact': return p.gap > 0 ? `Contact, gaps to ${p.gap} µm` : 'Contact';
  }
  return 'A graph';
}

// The cells a graph is built over: of a segmentation's n cells, those a filter keeps (keep, or all)
// and in any of the regions (regionRows: lists of rows, or null for anywhere), never those a review
// excluded (excluded[row] = 1). Answers { keep (Uint8Array), count, excluded: the cells chosen
// otherwise that a review excluded }.
export function chooseCells(n, { keep = null, regionRows = null, excluded = null } = {}) {
  const inRegions = regionRows ? new Uint8Array(n) : null;
  for (const rows of regionRows ?? []) for (const r of rows) if (r < n) inRegions[r] = 1;
  const out = new Uint8Array(n);
  let count = 0;
  let left = 0;
  for (let i = 0; i < n; i += 1) {
    if ((keep && !keep[i]) || (inRegions && !inRegions[i])) continue;
    if (excluded?.[i]) {
      left += 1;
      continue;
    }
    out[i] = 1;
    count += 1;
  }
  return { keep: out, count, excluded: left };
}

// The cells as a graph's request sends them (api_graphs.go graphCells): their bits (base64), checksum
// and how they were chosen ({ filter, regions, excluded }).
export function cellsRequest(keep, { filter = null, regions = null, excluded = 0 } = {}) {
  const bytes = keepBytes(keep);
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return {
    keep: btoa(binary), checksum: keepChecksum(keep),
    ...(filter ? { filter } : {}), ...(regions?.length ? { regions } : {}), ...(excluded ? { excluded } : {}),
  };
}

// A graph artifact as the project records it.
export function graphRecord(dataset, manifest, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  const seg = (lineage.parents ?? []).find((p) => p.kind === 'segmentation');
  return {
    kind: 'graph',
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

export function graphsOf(project, datasetId, segmentationId = null) {
  return project.artifacts.filter((a) => a.kind === 'graph' && a.dataset === datasetId && (!segmentationId || a.segmentation === segmentationId));
}

// The project with a graph recorded (unchanged when it already is).
export function addGraph(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'graph' && a.id === record.id && a.dataset === record.dataset)) return project;
  const dataset = project.datasets.find((d) => d.id === record.dataset);
  const s = record.summary ?? {};
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'graph', detail: `Built a graph of the cells of ${dataset?.name ?? record.dataset}: ${record.name}; ${s.cells ?? '?'} cells, ${s.edges ?? '?'} edges (artifact ${record.id})` }],
  };
}

// A graph as GET /api/graphs/{id}/edges answers it: { n, rows, x, y, offsets, neighbors, length,
// shared (null unless a contact graph), indexOf (row → index among the graph's cells, −1 none) }.
export function readGraph(buffer) {
  const view = new DataView(buffer);
  if (buffer.byteLength < 12) throw new Error('a graph answer too short');
  const n = view.getUint32(0, true);
  const m = view.getUint32(4, true);
  const hasShared = view.getUint8(8) === 1;
  const size = 12 + 12 * n + 4 * (n + 1) + 8 * m + (hasShared ? 4 * m : 0);
  if (buffer.byteLength !== size) throw new Error(`a graph answer of ${buffer.byteLength} bytes, not ${size}`);
  let at = 12;
  const take = (Type, count) => {
    const a = new Type(buffer, at, count);
    at += Type.BYTES_PER_ELEMENT * count;
    return a;
  };
  const rows = take(Uint32Array, n);
  const x = take(Float32Array, n);
  const y = take(Float32Array, n);
  const offsets = take(Uint32Array, n + 1);
  const neighbors = take(Uint32Array, m);
  const length = take(Float32Array, m);
  const shared = hasShared ? take(Float32Array, m) : null;
  const top = n ? rows[n - 1] + 1 : 0;
  const indexOf = new Int32Array(top).fill(-1);
  for (let i = 0; i < n; i += 1) indexOf[rows[i]] = i;
  return { n, rows, x, y, offsets, neighbors, length, shared, indexOf };
}

// A cell's neighbors in a graph, by the segmentation's rows: [{ row, length, shared }], nearest
// first; null when the cell is not in the graph.
export function neighborsOf(graph, row) {
  const i = row < graph.indexOf.length ? graph.indexOf[row] : -1;
  if (i < 0) return null;
  const out = [];
  for (let k = graph.offsets[i]; k < graph.offsets[i + 1]; k += 1) {
    out.push({ row: graph.rows[graph.neighbors[k]], length: graph.length[k], shared: graph.shared ? graph.shared[k] : null });
  }
  return out.sort((a, b) => a.length - b.length || a.row - b.row);
}

// A grid of a graph's cells, for finding those in a part of the tissue: squares of side `side` µm.
export function gridOf(graph, side = 50) {
  let x0 = Infinity;
  let y0 = Infinity;
  let x1 = -Infinity;
  let y1 = -Infinity;
  for (let i = 0; i < graph.n; i += 1) {
    x0 = Math.min(x0, graph.x[i]);
    y0 = Math.min(y0, graph.y[i]);
    x1 = Math.max(x1, graph.x[i]);
    y1 = Math.max(y1, graph.y[i]);
  }
  if (!graph.n) return { side, x0: 0, y0: 0, across: 0, down: 0, start: new Uint32Array(1), cells: new Uint32Array(0) };
  const across = Math.floor((x1 - x0) / side) + 1;
  const down = Math.floor((y1 - y0) / side) + 1;
  const square = (i) => Math.floor((graph.y[i] - y0) / side) * across + Math.floor((graph.x[i] - x0) / side);
  const start = new Uint32Array(across * down + 1);
  for (let i = 0; i < graph.n; i += 1) start[square(i) + 1] += 1;
  for (let s = 0; s < across * down; s += 1) start[s + 1] += start[s];
  const at = start.slice(0, across * down);
  const cells = new Uint32Array(graph.n);
  for (let i = 0; i < graph.n; i += 1) cells[at[square(i)]++] = i;
  return { side, x0, y0, across, down, start, cells };
}

// The edges with a cell in a rectangle of the tissue ({ x0, y0, x1, y1 } µm), each once: a flat
// list of index pairs among the graph's cells; null when there are more than `limit`.
export function edgesIn(graph, grid, rect, limit = Infinity) {
  const out = [];
  if (!grid.across) return out;
  const c0 = Math.max(0, Math.floor((rect.x0 - grid.x0) / grid.side));
  const c1 = Math.min(grid.across - 1, Math.floor((rect.x1 - grid.x0) / grid.side));
  const r0 = Math.max(0, Math.floor((rect.y0 - grid.y0) / grid.side));
  const r1 = Math.min(grid.down - 1, Math.floor((rect.y1 - grid.y0) / grid.side));
  const inside = (i) => graph.x[i] >= rect.x0 && graph.x[i] <= rect.x1 && graph.y[i] >= rect.y0 && graph.y[i] <= rect.y1;
  for (let r = r0; r <= r1; r += 1) {
    for (let c = c0; c <= c1; c += 1) {
      const s = r * grid.across + c;
      for (let k = grid.start[s]; k < grid.start[s + 1]; k += 1) {
        const i = grid.cells[k];
        if (!inside(i)) continue;
        for (let e = graph.offsets[i]; e < graph.offsets[i + 1]; e += 1) {
          const j = graph.neighbors[e];
          // Each edge once: from its lower cell, or from the one inside when the other is not.
          if (j < i && inside(j)) continue;
          out.push(i, j);
          if (out.length > 2 * limit) return null;
        }
      }
    }
  }
  return out;
}
