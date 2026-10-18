// Spatial analyses of a segmentation's labeled cells (internal/spatial, api_spatial.go) on the page:
// the questions and their parameters as the program checks them, the requests (each cell's label and
// region as the program reads them), the records in the project, the results read, an enrichment's
// pairs ranked, and the cells each number comes from (a pair's cells and the edges between them; a
// label's cells near another's; a label's cells in a region, or in bands across a region's edge;
// a niche's cells; a gene's local Moran's I classes).

import { sha256 } from './sha256.js';
import { log, square } from './dmath.js';

export const QUESTIONS = [
  {
    id: 'enrichment', label: 'Neighborhood enrichment',
    help: 'Which cell types are neighbors more, or less, often than if the labels were dealt to the same cells at random: for each pair of labels, the neighbors between their cells in a graph against the number expected, as a ratio, with a z-score and p-value from permutations of the labels. Dealing the labels within regions keeps each region\'s composition, so a pair is enriched beyond what the regions\' make-up explains.',
  },
  {
    id: 'cooccurrence', label: 'Co-occurrence across distances',
    help: 'How much likelier a label\'s cells are within a distance of another label\'s cells than of any cell, at distances from close neighbors to the scale of tissue domains (as Squidpy computes it). Above 1, found that near more often than their share; below 1, less. Descriptive: it has no test.',
  },
  {
    id: 'distances', label: 'Nearest distances',
    help: 'How far each label\'s cells are from the nearest cell of each label, against the labels dealt again to the same cells: a median shorter than chance gives says the two lie nearer each other than their numbers explain; longer, that they keep apart. With the distribution of the distances and its envelope.',
  },
  {
    id: 'ripley', label: 'Ripley\'s K and L',
    help: 'How many cells of a label lie within each distance of a cell of the same or another label, in a window (the tissue, or a region), corrected for the window\'s edge (Ripley\'s isotropic correction), as L, a distance (L = r for points scattered at random). Compared with the labels dealt again to the same cells: L above its envelope means the cells gather, or the two labels draw together, at that scale; below, they spread out or keep apart.',
  },
  {
    id: 'regions', label: 'Regions compared',
    help: 'Each region\'s cells by label, two ways: per mm² of the tissue within the region (a density: the region\'s empty glass and holes not counted) and per labeled cell in it (a share), each with a 95% interval; the tissue in none of the regions is a row of its own. Two rows compared as ratios with intervals. The intervals treat cells as placed independently, and the regions of one section are not samples: these describe this section.',
  },
  {
    id: 'boundary', label: 'Across a region\'s edge',
    help: 'Each label\'s density in bands of distance from a region\'s edge, from deep inside to well outside, each band\'s own tissue its denominator: whether a label fills the region, gathers at its margin or stays out. The core (inside, beyond the margin), the margin (within it of the edge, either side) and around (outside, beyond the margin, to the reach) are compared as ratios with intervals, as a tumor\'s core and invasive margin are measured.',
  },
  {
    id: 'niches', label: 'Cellular neighborhoods',
    help: 'Kinds of place in the tissue: each cell\'s neighborhood (its nearest labeled cells, or those within a radius) described by its labels\' shares, and the cells grouped by them (k-means), as niches. A niche is a kind of place, not a kind of cell: an unlabeled cell has the niche of the labeled cells around it. Each niche\'s make-up against the tissue\'s, the labels that define it and its leading genes; the niches found again from other seeds say how stable they are.',
  },
  {
    id: 'moran', label: 'Spatially variable genes', labels: false,
    help: 'Which genes are expressed in a spatial pattern: each gene\'s Moran\'s I over a graph (1 when neighbors\' expression is alike, about 0 for no pattern, below 0 where neighbors differ), tested against the values dealt to the cells at random (analytically and by permutation), adjusted over the genes. For the leading genes, local Moran\'s I shows where the pattern lies: cells high among high, low among low, or unlike their neighbors. Of genes, not labels: the cells need no labels.',
  },
];

export const ENRICHMENT_DEFAULTS = { permutations: 1000, seed: 20250, weight: 'edges' };

// Distances of 10 to 150 µm, the pairs within each.
export const COOCCURRENCE_DEFAULTS = { distances: Array.from({ length: 15 }, (_, i) => 10 * (i + 1)), pairs: 'within' };

// Nearest distances to 100 µm, their distribution every 2.5 µm; Ripley's K to 100 µm every 5.
export const DISTANCES_DEFAULTS = { permutations: 199, seed: 20250, reach: 100, step: 2.5 };
export const RIPLEY_DEFAULTS = { radii: Array.from({ length: 20 }, (_, i) => 5 * (i + 1)), correction: 'isotropic', permutations: 199, seed: 20250 };

// 95% intervals; bands every 10 µm to 100 µm either side of an edge, a margin of 20 µm.
export const REGIONS_DEFAULTS = { level: 0.95 };
export const BOUNDARY_DEFAULTS = { reach: 100, step: 10, margin: 20, level: 0.95 };

// Row-standardized weights, 199 permutations, the 10 leading genes' local Moran's I.
export const MORAN_DEFAULTS = { weights: 'row', permutations: 199, seed: 20250, local: 10, localPermutations: 199 };

// Each cell's 10 nearest labeled cells, 6 niches, the best of 10 starts, 4 further seeds.
export const NICHES_DEFAULTS = { neighbors: 10, radius: 0, niches: 6, starts: 10, seeds: 4, seed: 20250 };

// The most regions compared at once (internal/spatial MaxRegions), and the mark of a cell outside
// the tissue window in a comparison's strata.
export const MAX_REGIONS = 20;
export const OUTSIDE_TISSUE = 1 << 30;

// What is wrong with an enrichment's or a co-occurrence's parameters, as the program would say it,
// or null.
export function checkAnalysisParams(question, p) {
  if (question === 'enrichment') {
    if (!Number.isInteger(p.permutations) || p.permutations < 100 || p.permutations > 100000) return 'Permutations are a whole number from 100 to 100,000.';
    if (!Number.isInteger(p.seed) || p.seed < 0) return 'The seed is a whole number, 0 or more.';
    if (!['edges', 'boundary'].includes(p.weight)) return 'Neighbors are counted by edges or by the boundary shared.';
    return null;
  }
  if (question === 'cooccurrence') {
    const d = p.distances ?? [];
    if (d.length < 1 || d.length > 100) return 'A co-occurrence is at 1 to 100 distances.';
    if (d.some((v, i) => !Number.isFinite(v) || v <= 0 || (i > 0 && v <= d[i - 1]))) return 'The distances increase from more than 0 µm.';
    if (d.at(-1) > 1000) return 'A co-occurrence reaches at most 1,000 µm.';
    if (!['within', 'between'].includes(p.pairs)) return 'Pairs are counted within each distance or between distances.';
    return null;
  }
  if (question === 'distances') {
    if (!Number.isInteger(p.permutations) || p.permutations < 19 || p.permutations > 9999) return 'Permutations are a whole number from 19 to 9,999.';
    if (!(p.reach > 0 && p.reach <= 1000)) return 'The reach is more than 0 and at most 1,000 µm.';
    if (!(p.step > 0) || p.reach / p.step > 200) return 'The step is more than 0 µm and at least the reach over 200.';
    return null;
  }
  if (question === 'ripley') {
    const r = p.radii ?? [];
    if (r.length < 1 || r.length > 200) return 'Ripley\'s K is at 1 to 200 radii.';
    if (r.some((v, i) => !Number.isFinite(v) || v <= 0 || (i > 0 && v <= r[i - 1]))) return 'The radii increase from more than 0 µm.';
    if (r.at(-1) > 500) return 'Ripley\'s K reaches at most 500 µm.';
    if (!['isotropic', 'none'].includes(p.correction)) return 'The edge correction is isotropic or none.';
    if (!Number.isInteger(p.permutations) || p.permutations < 19 || p.permutations > 9999) return 'Permutations are a whole number from 19 to 9,999.';
    return null;
  }
  const level = (v) => (v >= 0.5 && v <= 0.999 ? null : 'The intervals\' level is 0.5 to 0.999.');
  if (question === 'regions') return level(p.level);
  if (question === 'boundary') {
    if (!(p.reach > 0 && p.reach <= 1000)) return 'The bands reach more than 0 and at most 1,000 µm.';
    const steps = p.reach / p.step;
    if (!(p.step > 0) || steps > 100 || Math.abs(steps - Math.round(steps)) > 1e-9) return 'The bands\' step divides the reach into at most 100.';
    if (!(p.margin > 0 && p.margin < p.reach)) return 'The margin is more than 0 and less than the reach.';
    return level(p.level);
  }
  if (question === 'niches') {
    if ((p.neighbors > 0) === (p.radius > 0)) return 'A niche\'s window is the nearest neighbors or a radius, one of them.';
    if (p.neighbors > 0 && !(Number.isInteger(p.neighbors) && p.neighbors >= 2 && p.neighbors <= 200)) return 'A window is of 2 to 200 nearest labeled cells.';
    if (p.radius > 500) return 'A window\'s radius is at most 500 µm.';
    if (!Number.isInteger(p.niches) || p.niches < 2 || p.niches > 30) return '2 to 30 niches.';
    if (!Number.isInteger(p.starts) || p.starts < 1 || p.starts > 100) return 'k-means\'s starts are 1 to 100.';
    if (!Number.isInteger(p.seeds) || p.seeds < 0 || p.seeds > 20) return '0 to 20 further seeds.';
    return null;
  }
  if (question === 'moran') {
    if (!['row', 'binary'].includes(p.weights)) return 'Moran\'s I\'s weights are row (standardized) or binary.';
    if (!Number.isInteger(p.permutations) || (p.permutations !== 0 && (p.permutations < 19 || p.permutations > 9999))) return 'Moran\'s I\'s permutations are 0 (the analytic test) or 19 to 9,999.';
    if (!Number.isInteger(p.local) || p.local < 0 || p.local > 50) return 'Local Moran\'s I is of 0 to 50 genes.';
    if (p.local > 0 && !(Number.isInteger(p.localPermutations) && p.localPermutations >= 19 && p.localPermutations <= 9999)) return 'Local Moran\'s I\'s permutations are 19 to 9,999.';
    return null;
  }
  return `No question ${question}.`;
}

// Niches' names: each one's number and the labels that define it, with their shares of its
// neighborhoods ("6 · Stromal 39% + Endothelial 13%"): a niche is named by what it holds more of
// than the tissue, which need not be most of it.
export function nicheNames(niches, labelNames) {
  const K = labelNames.length;
  return niches.defining.map((ks, v) => `${v + 1} · ${ks.map((k) => `${labelNames[k]} ${Math.round(100 * (niches.composition[v * K + k] ?? 0))}%`).join(' + ')}`);
}

// A niche's cells (stratum: each cell's niche + 1, 0 for none), and their count.
export function nicheCells(stratum, v) {
  const cells = new Uint8Array(stratum.length);
  let count = 0;
  for (let i = 0; i < stratum.length; i += 1) {
    if (stratum[i] === v + 1) {
      cells[i] = 1;
      count += 1;
    }
  }
  return { cells, count };
}

// Regions compared's rows: the regions' names, then the tissue in none of them (as a heading; in a
// sentence, rowNames(regions, true)).
export function rowNames(regions, inSentence = false) {
  const rest = regions.regions.length === 1 ? 'the rest of the tissue' : 'the tissue in none of them';
  return [...regions.regions.map((r) => r.name), inSentence ? rest : rest[0].toUpperCase() + rest.slice(1)];
}

// The cells of label k in row r of regions compared (R rows, the last the tissue in none), by each
// cell's regions (stratum): a Uint8Array over the cells analyzed, and their count.
export function regionCells(stratum, label, R, r, k) {
  const cells = new Uint8Array(label.length);
  let count = 0;
  for (let i = 0; i < label.length; i += 1) {
    const m = stratum[i];
    if (label[i] !== k || m === OUTSIDE_TISSUE) continue;
    if (r === R - 1 ? m === 0 : (m & (1 << r)) !== 0) {
      cells[i] = 1;
      count += 1;
    }
  }
  return { cells, count };
}

// The cells of label k (−1 any label) in bands from to to (indices, both kept) across a region's
// edge, by each cell's band + 1 (stratum; 0 outside the tissue).
export function bandCells(stratum, label, from, to, k) {
  const cells = new Uint8Array(label.length);
  let count = 0;
  for (let i = 0; i < label.length; i += 1) {
    const b = stratum[i] - 1;
    if (b < from || b > to || (k >= 0 ? label[i] !== k : label[i] < 0)) continue;
    cells[i] = 1;
    count += 1;
  }
  return { cells, count };
}

// The bands of a profile across an edge, as { from, to } µm (−Infinity below the first edge,
// +Infinity from the last).
export function bandsOf(edges) {
  return Array.from({ length: edges.length + 1 }, (_, b) => ({ from: b === 0 ? -Infinity : edges[b - 1], to: b === edges.length ? Infinity : edges[b] }));
}

// The bands making a zone of a profile: the core (inside beyond the margin), the margin (within it
// either side) or around (outside beyond it, to the reach), as band indices { from, to } when the
// zone's edges are bands' edges.
export function zoneBands(edges, margin, zone) {
  const at = (v) => edges.findIndex((e) => Math.abs(e - v) < 1e-9);
  const lo = at(-margin);
  const hi = at(margin);
  if (lo < 0 || hi < 0) return null;
  if (zone === 'core') return { from: 0, to: lo };
  if (zone === 'margin') return { from: lo + 1, to: hi };
  return { from: hi + 1, to: edges.length - 1 };
}

// Distances every step up to the farthest: [step, 2·step, …].
export function distancesTo(farthest, step) {
  const out = [];
  for (let k = 1; k * step <= farthest + 1e-9 && out.length < 100; k += 1) out.push(Number((k * step).toPrecision(12)));
  return out;
}

function base64(bytes) {
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(binary);
}

// A number for each cell as the program reads it (api_spatial.go cellCodes): base64 of the count
// and the numbers (32-bit, little-endian), and the SHA-256 of those bytes.
export function codesRequest(values) {
  const bytes = new Uint8Array(4 + 4 * values.length);
  const view = new DataView(bytes.buffer);
  view.setUint32(0, values.length, true);
  for (let i = 0; i < values.length; i += 1) view.setInt32(4 + 4 * i, values[i], true);
  return { codes: base64(bytes), checksum: sha256(bytes) };
}

// Each cell's label from a label source (ui/label-sources.js), with the set it is of: only the labels
// some cell has (a set's levels share one list of names), numbered again in their order.
export function labelsRequest(src) {
  const used = new Uint8Array(src.names.length);
  for (const l of src.label) if (l >= 0) used[l] = 1;
  const index = new Int32Array(src.names.length).fill(-1);
  const names = [];
  const colors = [];
  src.names.forEach((name, k) => {
    if (!used[k]) return;
    index[k] = names.length;
    names.push(name);
    const c = src.colors?.[k];
    colors.push(Array.isArray(c) ? `rgb(${c.join(', ')})` : String(c ?? ''));
  });
  const codes = Int32Array.from(src.label, (l) => (l >= 0 ? index[l] : -1));
  return { id: src.id, kind: src.kind, name: src.name, level: src.level ?? 0, names, colors, ...codesRequest(codes) };
}

// Each of n cells' region (0 in none; k in the kth of regions, the first it lies in), from the rows
// in each region.
export function strataRequest(n, regions) {
  const stratum = new Int32Array(n);
  regions.forEach((r, k) => {
    for (const row of r.rows) if (row < n && stratum[row] === 0) stratum[row] = k + 1;
  });
  return { regions: regions.map((r) => ({ id: r.id, name: r.name, version: r.version })), ...codesRequest(stratum) };
}

// An analysis artifact as the project records it.
export function analysisRecord(dataset, manifest, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  const seg = (lineage.parents ?? []).find((p) => p.kind === 'segmentation');
  const graph = (lineage.parents ?? []).find((p) => p.kind === 'graph');
  return {
    kind: 'spatial',
    id: manifest.id,
    name: manifest.name,
    dataset: dataset.id,
    segmentation: seg?.id ?? null,
    graph: graph?.id ?? null,
    question: manifest.summary?.question ?? null,
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

export function analysesOf(project, datasetId, segmentationId = null) {
  return project.artifacts.filter((a) => a.kind === 'spatial' && a.dataset === datasetId && (!segmentationId || a.segmentation === segmentationId));
}

// The project with an analysis recorded (unchanged when it already is).
export function addAnalysis(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'spatial' && a.id === record.id && a.dataset === record.dataset)) return project;
  const dataset = project.datasets.find((d) => d.id === record.dataset);
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'spatial', detail: `${record.name} of ${dataset?.name ?? record.dataset} (artifact ${record.id})` }],
  };
}

// Local Moran's I classes, as local.bin numbers them.
export const LOCAL_CLASSES = ['none', 'high among high', 'low among low', 'high among low', 'low among high'];

// A Moran's I analysis's local values as GET /api/analyses/{id}/local answers them: { n, genes
// (indices), I, P (Float32Array each, gene by gene), classes (Uint8Array each) }.
export function readLocal(buffer) {
  const v = new DataView(buffer);
  const n = v.getUint32(0, true);
  const T = v.getUint32(4, true);
  if (buffer.byteLength !== 8 + 4 * T + 9 * n * T) throw new Error(`local values of ${buffer.byteLength} bytes, not ${8 + 4 * T + 9 * n * T}`);
  const genes = Array.from({ length: T }, (_, k) => v.getUint32(8 + 4 * k, true));
  const base = 8 + 4 * T;
  return {
    n, genes,
    I: genes.map((_, k) => new Float32Array(buffer, base + 4 * n * k, n)),
    P: genes.map((_, k) => new Float32Array(buffer, base + 4 * n * (T + k), n)),
    classes: genes.map((_, k) => new Uint8Array(buffer, base + 8 * n * T + n * k, n)),
  };
}

// A gene's cells of a local class (1 high among high … 4 low among high; 0 every class but none).
export function localCells(local, k, cls) {
  const classes = local.classes[k];
  const cells = new Uint8Array(classes.length);
  let count = 0;
  for (let i = 0; i < classes.length; i += 1) {
    if (cls === 0 ? classes[i] > 0 : classes[i] === cls) {
      cells[i] = 1;
      count += 1;
    }
  }
  return { cells, count };
}

// An analysis's cells as GET /api/analyses/{id}/cells answers them: { n, rows, label, stratum }.
export function readAnalyzed(buffer) {
  const n = new DataView(buffer).getUint32(0, true);
  if (buffer.byteLength !== 4 + 12 * n) throw new Error(`an analysis's cells of ${buffer.byteLength} bytes, not ${4 + 12 * n}`);
  return { n, rows: new Uint32Array(buffer, 4, n), label: new Int32Array(buffer, 4 + 4 * n, n), stratum: new Int32Array(buffer, 4 + 8 * n, n) };
}

const num = (v) => (v === null || v === undefined ? NaN : v);

// An enrichment's pairs (A ≤ B) with their numbers, most enriched first: [{ a, b, count, expected,
// ratio, z, p, padj }] (a and b: indices of the labels).
export function rankPairs(analysis) {
  const e = analysis.enrichment;
  const K = analysis.labels.names.length;
  const out = [];
  for (let a = 0; a < K; a += 1) {
    for (let b = a; b < K; b += 1) {
      const q = a * K + b;
      const expected = num(e.expected[q]);
      out.push({ a, b, count: num(e.count[q]), expected, ratio: expected > 0 ? num(e.count[q]) / expected : NaN, z: num(e.z[q]), p: num(e.p[q]), padj: num(e.padj[q]) });
    }
  }
  return out.sort((x, y) => (Number.isFinite(y.z) ? y.z : -Infinity) - (Number.isFinite(x.z) ? x.z : -Infinity) || x.a - y.a || x.b - y.b);
}

// A pair's cells in a graph (labels A and B; A = B for a label with itself): the cells of each with
// a neighbor of the other, and the edges between them. label: each graph cell's label. Answers
// { cells (Uint8Array over the graph's cells), a, b (how many of each), edges }.
export function pairCells(graph, label, A, B) {
  const cells = new Uint8Array(graph.n);
  let edges = 0;
  for (let i = 0; i < graph.n; i += 1) {
    const li = label[i];
    if (li !== A && li !== B) continue;
    for (let k = graph.offsets[i]; k < graph.offsets[i + 1]; k += 1) {
      const j = graph.neighbors[k];
      const lj = label[j];
      if ((li === A && lj === B) || (li === B && lj === A)) {
        cells[i] = 1;
        if (j > i) edges += 1;
      }
    }
  }
  let a = 0;
  let b = 0;
  for (let i = 0; i < graph.n; i += 1) {
    if (!cells[i]) continue;
    if (label[i] === A) a += 1;
    if (label[i] === B && A !== B) b += 1;
  }
  return { cells, a, b, edges };
}

// The cells of labels A and B within distance d of each other (or between dmin and d): each B cell
// with an A cell that near, and each such A cell. x, y, label: the cells analyzed. Answers
// { cells (Uint8Array), a, b }.
export function nearCells(x, y, label, A, B, d, dmin = -Infinity) {
  const n = label.length;
  const cells = new Uint8Array(n);
  const side = Math.max(d, 1e-6);
  const grid = new Map();
  for (let i = 0; i < n; i += 1) {
    if (label[i] !== B) continue;
    const key = `${Math.floor(x[i] / side)},${Math.floor(y[i] / side)}`;
    if (!grid.has(key)) grid.set(key, []);
    grid.get(key).push(i);
  }
  for (let i = 0; i < n; i += 1) {
    if (label[i] !== A) continue;
    const gx = Math.floor(x[i] / side);
    const gy = Math.floor(y[i] / side);
    for (let dy = -1; dy <= 1; dy += 1) {
      for (let dx = -1; dx <= 1; dx += 1) {
        for (const j of grid.get(`${gx + dx},${gy + dy}`) ?? []) {
          if (j === i) continue;
          const ex = x[j] - x[i];
          const ey = y[j] - y[i];
          const dist = Math.sqrt(ex * ex + ey * ey);
          if (dist <= d && dist > dmin) {
            cells[i] = 1;
            cells[j] = 1;
          }
        }
      }
    }
  }
  let a = 0;
  let b = 0;
  for (let i = 0; i < n; i += 1) {
    if (!cells[i]) continue;
    if (label[i] === A) a += 1;
    if (label[i] === B && A !== B) b += 1;
  }
  return { cells, a, b };
}

// A color for a log₂ ratio on a diverging scale clamped at ±limit: red for more than expected, blue
// for less, white for as expected (ColorBrewer's RdBu, reversed).
const RDBU = [[5, 48, 97], [33, 102, 172], [67, 147, 195], [146, 197, 222], [209, 229, 240], [247, 247, 247], [253, 219, 199], [244, 165, 130], [214, 96, 77], [178, 24, 43], [103, 0, 31]];
export function divergingColor(log2ratio, limit = 2) {
  if (!Number.isFinite(log2ratio)) return log2ratio === -Infinity ? 'rgb(5, 48, 97)' : 'transparent';
  const t = (Math.max(-limit, Math.min(limit, log2ratio)) / limit + 1) / 2 * (RDBU.length - 1);
  const k = Math.min(RDBU.length - 2, Math.floor(t));
  const f = t - k;
  const c = RDBU[k].map((v, i) => Math.round(v + f * (RDBU[k + 1][i] - v)));
  return `rgb(${c.join(', ')})`;
}

// --- An analysis made again on another segmentation (wave 6, slice 7) ---

// What making an analysis again on another segmentation of its dataset needs, or why it cannot be:
// { problem } or { analysis (its record), segmentation (the other's record), labels: { clustering,
// label (the set's record), level } or null, graph: { parameters, regions } or null, regions (the
// cells chosen in them, by id), notes }. analysis: its analysis.json.
export function remakeAnalysisPlan(project, record, analysis, segmentationId) {
  const seg = project.artifacts.find((a) => a.kind === 'segmentation' && a.id === segmentationId);
  if (!seg || seg.dataset !== record.dataset) return { problem: 'No such segmentation of the analysis\'s dataset.' };
  if (seg.id === record.segmentation) return { problem: `The analysis is already of ${seg.name}.` };
  if (seg.state !== 'complete') return { problem: `${seg.name} is not complete.` };
  const notes = [];
  let labels = null;
  if (analysis.question !== 'moran') {
    const id = analysis.labels.id;
    const kind = analysis.labels.kind;
    if (kind === 'curated' || kind === 'consensus') return { problem: `Its labels are a ${kind} set, made for the cells of its segmentation: make the analysis again with labels from markers or a reference.` };
    if (kind === 'niches') return { problem: 'Its labels are niches: make the niches\' analysis again first, then ask this question of them.' };
    const set = project.artifacts.find((a) => a.kind === 'labels' && a.id === id);
    if (!set) return { problem: 'Its label set is no longer in the project.' };
    if (set.source) return { problem: `Its labels were imported for the cells of its segmentation (${set.source.file ?? 'a table'}): import labels of the other's cells, or label them from markers.` };
    if (!set.clustering) return { problem: 'Its label set names no clustering to make again.' };
    labels = { clustering: set.clustering, label: set, level: analysis.labels.level };
  }
  const choice = analysis.cells ?? {};
  if (choice.filter) notes.push(`The cells were those the QC filter ${choice.filter.name} kept; on ${seg.name} they are all the cells${choice.regions?.length ? ' in the regions' : ''}.`);
  const regions = (choice.regions ?? []).map((r) => r.id);
  const graph = analysis.graph ? { parameters: analysis.graph.parameters, regions } : null;
  return { analysis: record, segmentation: seg, labels, graph, regions, notes };
}

const finiteOr = (v) => (v === null || v === undefined ? NaN : v);

// An analysis's numbers, each keyed by what it is of (labels and genes by name, so the same set made
// again on another segmentation lines up): [{ key, text, value, scale ('log': a ratio, compared by
// log₂; 'linear'), labels (names it is of), unit }].
export function analysisNumbers(analysis) {
  const out = [];
  const names = analysis.labels?.names ?? [];
  const K = names.length;
  const push = (key, text, value, scale, labels = [], unit = '') => out.push({ key, text, value: finiteOr(value), scale, labels, unit });
  if (analysis.enrichment) {
    // An unordered pair: keyed by its names in order, whatever the sets' order of labels.
    for (const p of rankPairs(analysis)) {
      const pair = [names[p.a], names[p.b]].sort();
      push(`pair:${pair.join('|')}`, `${pair.join(' – ')}, neighbors found over expected`, p.ratio, 'log', pair);
    }
  }
  if (analysis.cooccurrence) {
    const co = analysis.cooccurrence;
    co.distances.forEach((d, k) => {
      for (let a = 0; a < K; a += 1) for (let b = 0; b < K; b += 1) push(`co:${names[a]}|${names[b]}@${d}`, `${names[b]} within ${d} µm of ${names[a]}`, co.ratio[k][a * K + b], 'log', [names[a], names[b]]);
    });
  }
  if (analysis.nearest) {
    const r = analysis.nearest;
    for (let q = 0; q < K * K; q += 1) push(`near:${names[Math.floor(q / K)]}|${names[q % K]}`, `${names[Math.floor(q / K)]} → the nearest ${names[q % K]}, median`, r.median[q], 'linear', [names[Math.floor(q / K)], names[q % K]], 'µm');
  }
  if (analysis.ripley) {
    const r = analysis.ripley;
    const R = r.radii.length;
    for (let q = 0; q < K * K; q += 1) {
      r.radii.forEach((rad, k) => push(`L:${names[Math.floor(q / K)]}|${names[q % K]}@${rad}`, `L(${rad} µm) − r, ${Math.floor(q / K) === q % K ? names[q % K] : `${names[q % K]} around ${names[Math.floor(q / K)]}`}`, finiteOr(r.l[q * R + k]) - rad, 'linear', [names[Math.floor(q / K)], names[q % K]], 'µm'));
    }
  }
  if (analysis.regions) {
    const r = analysis.regions;
    const rows = rowNames(r);
    rows.forEach((row, i) => names.forEach((n, k) => push(`density:${row}|${n}`, `${n} in ${row}, cells per mm²`, r.density[i * K + k], 'log', [n])));
  }
  if (analysis.boundary) {
    const b = analysis.boundary;
    names.forEach((n, k) => push(`edge:${n}`, `${n}, the core over around`, b.ratio[k * 3], 'log', [n]));
  }
  if (analysis.moran) {
    const r = analysis.moran;
    r.genes.forEach((g, i) => push(`gene:${g}`, `${g}, Moran's I`, r.i[i], 'linear'));
  }
  return out;
}

// Two analyses' numbers read together: those of both, with the change (log₂ of the ratio for a
// ratio; the difference otherwise), the largest first; and how many are of one only.
export function compareNumbers(a, b) {
  const byKey = new Map(b.map((x) => [x.key, x]));
  const rows = [];
  let onlyA = 0;
  for (const x of a) {
    const y = byKey.get(x.key);
    if (!y) {
      onlyA += 1;
      continue;
    }
    byKey.delete(x.key);
    const change = x.scale === 'log' ? (x.value > 0 && y.value > 0 ? log(y.value / x.value) / Math.LN2 : NaN) : y.value - x.value;
    rows.push({ key: x.key, text: x.text, a: x.value, b: y.value, change, scale: x.scale, labels: x.labels, unit: x.unit });
  }
  rows.sort((p, q) => (Number.isFinite(q.change) ? Math.abs(q.change) : -1) - (Number.isFinite(p.change) ? Math.abs(p.change) : -1) || (p.key < q.key ? -1 : 1));
  return { rows, onlyA, onlyB: byKey.size };
}

// How alike two analyses' numbers are overall: the rank correlation (Spearman's, ties broken by
// order) of the numbers of both; NaN with fewer than three.
export function rankCorrelation(rows) {
  const both = rows.filter((r) => Number.isFinite(r.a) && Number.isFinite(r.b));
  if (both.length < 3) return NaN;
  const ranks = (vs) => {
    const out = new Array(vs.length);
    vs.map((v, i) => [v, i]).sort((p, q) => p[0] - q[0] || p[1] - q[1]).forEach(([, i], k) => { out[i] = k; });
    return out;
  };
  const ra = ranks(both.map((r) => r.a));
  const rb = ranks(both.map((r) => r.b));
  const m = (both.length - 1) / 2;
  let sab = 0;
  let saa = 0;
  let sbb = 0;
  ra.forEach((x, i) => {
    sab += (x - m) * (rb[i] - m);
    saa += square(x - m);
    sbb += square(rb[i] - m);
  });
  return sab / Math.sqrt(saa * sbb);
}

// The project with an analysis recorded as another's made again on another segmentation, through a
// comparison of the two.
export function linkRemake(project, id, of, comparison) {
  return { ...project, artifacts: project.artifacts.map((a) => (a.kind === 'spatial' && a.id === id ? { ...a, remakeOf: of, comparison } : a)) };
}

// The analyses linked to one by making again: those made from it, and the one it was made from.
export function remakesOf(project, id) {
  const record = project.artifacts.find((a) => a.kind === 'spatial' && a.id === id);
  return project.artifacts.filter((a) => a.kind === 'spatial' && a.id !== id && a.state === 'complete' && (a.remakeOf === id || record?.remakeOf === a.id || (record?.remakeOf && a.remakeOf === record.remakeOf)));
}
