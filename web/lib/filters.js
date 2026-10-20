// Cell filters: conditions on the cells' QC attributes (lib/cell-attributes.js) that say which
// cells an analysis keeps. A filter never changes the data. Saved, it is an artifact of the project
// (kind "filter") with its definition and its result: the cells it keeps, counted, and the SHA-256
// of which they are, so that applying it again (on reopening, on another computer) can be shown
// to give the same cells. Changing a filter saves a new version; earlier ones stay.

import { attribute, attributeValues } from './cell-attributes.js';
import { sha256 } from './sha256.js';

// A condition: { attribute, min, max } (bounds included; null for none) for a number, or
// { attribute, values: [...] } for a category (the values kept). A cell without a value fails a
// condition on a number.
export function passes(condition, value) {
  if (attribute(condition.attribute)?.categorical) return Number.isFinite(value) && condition.values.includes(value);
  if (!Number.isFinite(value)) return false;
  if (condition.min !== null && condition.min !== undefined && value < condition.min) return false;
  if (condition.max !== null && condition.max !== undefined && value > condition.max) return false;
  return true;
}

// Applies conditions (all must pass): { keep: Uint8Array (1 kept), kept, failing: [cells failing
// each condition], only: [cells failing it and no other] }.
export function evaluate(conditions, cells) {
  const n = cells.n;
  const keep = new Uint8Array(n).fill(1);
  const failing = conditions.map(() => 0);
  const only = conditions.map(() => 0);
  const values = conditions.map((c) => attributeValues(cells, c.attribute));
  for (let i = 0; i < n; i += 1) {
    let fails = 0;
    let which = -1;
    for (let k = 0; k < conditions.length; k += 1) {
      if (!passes(conditions[k], values[k][i])) {
        fails += 1;
        failing[k] += 1;
        which = k;
      }
    }
    if (fails) keep[i] = 0;
    if (fails === 1) only[which] += 1;
  }
  let kept = 0;
  for (let i = 0; i < n; i += 1) kept += keep[i];
  return { keep, kept, failing, only };
}

// Which cells are kept, as bytes: the count of cells (4 bytes, little-endian), then one bit per
// cell, the first cell in the lowest bit. The program reads them so (api_clustering.go).
export function keepBytes(keep) {
  const bytes = new Uint8Array(4 + Math.ceil(keep.length / 8));
  new DataView(bytes.buffer).setUint32(0, keep.length, true);
  for (let i = 0; i < keep.length; i += 1) if (keep[i]) bytes[4 + (i >> 3)] |= 1 << (i & 7);
  return bytes;
}

// The SHA-256 of keepBytes.
export function keepChecksum(keep) {
  return sha256(keepBytes(keep));
}

function newId() {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`;
  return `f-${random.replace(/-/g, '').slice(0, 16)}`;
}

// A filter artifact for a dataset, with its result on the cells.
export function createFilter({ dataset, name, conditions, cells, previous = null, now = new Date().toISOString(), id = newId() }) {
  const { keep, kept } = evaluate(conditions, cells);
  return {
    kind: 'filter',
    id,
    name,
    version: previous ? previous.version + 1 : 1,
    parent: previous?.id ?? null,
    dataset,
    created: now,
    conditions: conditions.map((c) => ({ ...c })),
    result: { cells: cells.n, kept, sha256: keepChecksum(keep) },
  };
}

// A filter saved from a tree of conditions (all, any and not of conditions on a cell's attributes,
// its label, cluster or niche, its region or its distance from one, its count of a gene;
// internal/query), which the program evaluates (POST /api/local/{index}/query): the tree, the
// program's description of it, and its result as the program gave it ({ cells, kept, sha256 }).
export function createTreeFilter({ dataset, name, tree, text, result, previous = null, now = new Date().toISOString(), id = newId() }) {
  return {
    kind: 'filter',
    id,
    name,
    version: previous ? previous.version + 1 : 1,
    parent: previous?.id ?? null,
    dataset,
    created: now,
    tree,
    text,
    result: { cells: result.cells, kept: result.kept, sha256: result.sha256 },
  };
}

// Whether a filter is a tree of conditions (the program applies it), not conditions on attributes.
export const isTreeFilter = (filter) => Boolean(filter?.tree);

// What a filter keeps, in words: its conditions, or its tree as the program described it.
export function describeFilter(filter, methods = []) {
  if (isTreeFilter(filter)) return filter.text ?? JSON.stringify(filter.tree);
  return filter.conditions.map((c) => describeCondition(c, methods)).join('; ') || 'No conditions';
}

// Which cells are kept (1 kept) from keepBytes.
export function keepFromBytes(bytes) {
  const n = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength).getUint32(0, true);
  if (bytes.length !== 4 + Math.ceil(n / 8)) throw new Error(`${bytes.length} bytes of which cells are kept, for ${n} cells`);
  const keep = new Uint8Array(n);
  for (let i = 0; i < n; i += 1) keep[i] = (bytes[4 + (i >> 3)] >> (i & 7)) & 1;
  return keep;
}

// Whether a filter, applied again to the cells, keeps the cells it recorded (a filter of
// conditions; the program applies a tree, ui/filter-keeps.js).
export function verifyFilter(filter, cells) {
  const { keep, kept } = evaluate(filter.conditions, cells);
  const sum = keepChecksum(keep);
  return { same: cells.n === filter.result.cells && kept === filter.result.kept && sum === filter.result.sha256, kept, sha256: sum, keep };
}

// How two filters' kept cells overlap.
export function compareKeeps(a, b) {
  const out = { both: 0, onlyA: 0, onlyB: 0, neither: 0 };
  for (let i = 0; i < a.length; i += 1) {
    if (a[i] && b[i]) out.both += 1;
    else if (a[i]) out.onlyA += 1;
    else if (b[i]) out.onlyB += 1;
    else out.neither += 1;
  }
  return out;
}

// What a filter keeps of an attribute: the sum and median over kept cells, and over all.
export function effect(keep, values) {
  const kept = [];
  const all = [];
  let keptSum = 0;
  let allSum = 0;
  for (let i = 0; i < values.length; i += 1) {
    const v = values[i];
    if (!Number.isFinite(v)) continue;
    all.push(v);
    allSum += v;
    if (keep[i]) {
      kept.push(v);
      keptSum += v;
    }
  }
  const median = (v) => {
    if (!v.length) return NaN;
    v.sort((x, y) => x - y);
    const m = v.length >> 1;
    return v.length % 2 ? v[m] : (v[m - 1] + v[m]) / 2;
  };
  return { keptSum, allSum, keptMedian: median(kept), allMedian: median(all) };
}

// A filter's versions in a project: all filters of a dataset with its name, oldest first.
export function versions(project, dataset, name) {
  return project.artifacts.filter((a) => a.kind === 'filter' && a.dataset === dataset && a.name === name).sort((a, b) => a.version - b.version);
}

// The project with a filter saved (a new artifact; nothing is replaced).
export function addFilter(project, filter, now = new Date().toISOString()) {
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, filter],
    history: [...project.history, { time: now, action: 'filter', detail: `Saved filter ${filter.name} (version ${filter.version}): ${filter.result.kept} of ${filter.result.cells} cells kept` }],
  };
}

// The project without a filter version.
export function removeFilter(project, id, now = new Date().toISOString()) {
  const filter = project.artifacts.find((a) => a.id === id);
  if (!filter) return project;
  return {
    ...project,
    modified: now,
    artifacts: project.artifacts.filter((a) => a.id !== id),
    history: [...project.history, { time: now, action: 'filter', detail: `Removed filter ${filter.name} (version ${filter.version})` }],
  };
}

// A condition in words: "Gene transcripts ≥ 10", "Segmentation method is A or B".
export function describeCondition(c, methods = []) {
  const a = attribute(c.attribute);
  const label = a?.label ?? c.attribute;
  if (a?.categorical) return `${label} is ${c.values.map((v) => methods[v] ?? v).join(' or ')}`;
  const unit = a?.unit ? ` ${a.unit}` : '';
  const has = (v) => v !== null && v !== undefined;
  if (has(c.min) && has(c.max)) return `${label} from ${c.min} to ${c.max}${unit}`;
  if (has(c.min)) return `${label} ≥ ${c.min}${unit}`;
  if (has(c.max)) return `${label} ≤ ${c.max}${unit}`;
  return `${label} recorded`;
}
