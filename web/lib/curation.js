// Label versions (wave 4, slice 5): curated label sets — a predicted set (from markers, imported)
// at a level, and a reviewer's edits appended to it, each with who, when and why — and consensus
// label sets — a rule over chosen sets, their labels mapped to common names — kept in the project
// (records of kind 'curated-labels' and 'consensus-labels'); each set's labels per cell, and two
// sets side by side. Pure functions: labels are per segmentation cell (Int32Array, −1 for none),
// with their names.

import { categoricalColor } from './colormaps.js';

// --- Curated label sets ------------------------------------------------------------------------

export function curatedSetsOf(project, datasetId, segmentationId = null) {
  return project.artifacts.filter((a) => a.kind === 'curated-labels' && a.dataset === datasetId && (!segmentationId || a.segmentation === segmentationId));
}

// The project with a curated set begun from a base ({ labels, level, name }), no edits yet.
export function createCurated(project, { dataset, segmentation, base, name, by = 'the user', now = new Date().toISOString(), id = null }) {
  const count = project.artifacts.filter((a) => a.kind === 'curated-labels').length;
  const entry = { kind: 'curated-labels', id: id ?? `cur-${Date.parse(now).toString(36)}-${count + 1}`, dataset, segmentation, base, name, edits: [], by, created: now };
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, entry],
    history: [...project.history, { time: now, action: 'curated-labels', detail: `Began the curated label set "${name}" from ${base.name}` }],
  };
}

// An edit: { kind: 'cells' (cells relabeled), 'cluster' (a clustering's clusters relabeled) or
// 'accept' (cells' labels confirmed); cells (segmentation indices) or clustering and clusters; the
// label (a name; for 'accept', the label confirmed); the reason; who; when }.
export function checkEdit(edit) {
  if (!['cells', 'cluster', 'accept'].includes(edit.kind)) return 'An edit relabels cells or clusters, or accepts cells\' labels.';
  if (!String(edit.reason ?? '').trim()) return 'Say why: every edit keeps its reason.';
  if (!String(edit.label ?? '').trim()) return 'Name the label.';
  if (edit.kind === 'cluster' ? !(edit.clustering && edit.clusters?.length) : !edit.cells?.length) return edit.kind === 'cluster' ? 'Choose the clusters.' : 'Choose the cells.';
  return null;
}

// The project with an edit appended to a curated set (edits are never changed or removed: undoing
// one is another edit).
export function appendEdit(project, curatedId, edit, now = new Date().toISOString()) {
  const problem = checkEdit(edit);
  if (problem) throw new Error(problem);
  const record = project.artifacts.find((a) => a.kind === 'curated-labels' && a.id === curatedId);
  if (!record) throw new Error('The project has no such curated label set.');
  const clean = { ...edit, label: String(edit.label).trim(), reason: String(edit.reason).trim(), by: edit.by ?? 'the user', time: now };
  if (clean.cells) clean.cells = [...new Set(clean.cells)].sort((a, b) => a - b);
  const what = clean.kind === 'cluster' ? `${clean.clusters.length === 1 ? `cluster ${clean.clusters[0] + 1}` : `${clean.clusters.length} clusters`} relabeled "${clean.label}"`
    : clean.kind === 'accept' ? `${clean.cells.length === 1 ? 'a cell\'s' : `${clean.cells.length} cells'`} label "${clean.label}" accepted`
      : `${clean.cells.length === 1 ? 'a cell' : `${clean.cells.length} cells`} relabeled "${clean.label}"`;
  return {
    ...project,
    modified: now,
    artifacts: project.artifacts.map((a) => (a === record ? { ...a, edits: [...a.edits, clean] } : a)),
    history: [...project.history, { time: now, action: 'curated-labels', detail: `${record.name}: ${what} (${clean.reason}; ${clean.by})` }],
  };
}

// A curated set's labels: the base's ({ names, label, colors, confidence }) with its edits applied
// in order; clusterOf(clustering) answers a clustering's cluster per cell (for cluster edits).
// Answers { names, colors, label, confidence, edited (the last edit touching each cell, −1 for
// none), reviewed (cells an edit touched) }.
export function applyCurated(record, base, clusterOf = () => null) {
  const names = [...base.names];
  const colors = [...(base.colors ?? base.names.map((_, k) => categoricalColor(k)))];
  const index = new Map(names.map((n, k) => [n, k]));
  const label = Int32Array.from(base.label);
  const edited = new Int32Array(label.length).fill(-1);
  const of = (name) => {
    if (!index.has(name)) {
      index.set(name, names.length);
      colors.push(categoricalColor(names.length));
      names.push(name);
    }
    return index.get(name);
  };
  record.edits.forEach((e, n) => {
    const k = of(e.label);
    if (e.kind === 'cluster') {
      const cluster = clusterOf(e.clustering);
      if (!cluster) return;
      const set = new Set(e.clusters);
      for (let i = 0; i < label.length; i += 1) {
        if (set.has(cluster[i])) {
          label[i] = k;
          edited[i] = n;
        }
      }
      return;
    }
    for (const i of e.cells) {
      if (i < 0 || i >= label.length) continue;
      if (e.kind === 'cells') label[i] = k;
      edited[i] = n;
    }
  });
  return { names, colors, label, confidence: base.confidence ?? null, edited, reviewed: edited.reduce((s, v) => s + (v >= 0), 0) };
}

// --- Consensus label sets ----------------------------------------------------------------------

export function consensusSetsOf(project, datasetId, segmentationId = null) {
  return project.artifacts.filter((a) => a.kind === 'consensus-labels' && a.dataset === datasetId && (!segmentationId || a.segmentation === segmentationId));
}

export const RULES = [
  { id: 'unanimous', label: 'Unanimous', help: 'A cell takes a label when every set labels it, all alike.' },
  { id: 'majority', label: 'Majority', help: 'A cell takes the label at least the given number of sets give it, more than any other.' },
  { id: 'priority', label: 'In order', help: 'A cell takes the first set\'s label, else the next set\'s, in the order listed.' },
];

export function checkRule(rule, members) {
  if (members < 2) return 'A consensus is of two sets or more.';
  if (!RULES.some((r) => r.id === rule.kind)) return 'Choose a rule.';
  if (rule.kind === 'majority' && !(Number.isInteger(rule.min) && rule.min >= 1 && rule.min <= members)) return `The majority is a number of sets from 1 to ${members}.`;
  return null;
}

// The project with a consensus set saved: its members ({ ref, name } each, ref naming a label set
// and level, or a curated set), each member's mapping of its labels to common names (a label not
// mapped keeps its name), and the rule.
export function saveConsensus(project, { dataset, segmentation, name, members, mapping = [], rule, by = 'the user', now = new Date().toISOString(), id = null }) {
  const problem = checkRule(rule, members.length);
  if (problem) throw new Error(problem);
  const count = project.artifacts.filter((a) => a.kind === 'consensus-labels').length;
  const entry = { kind: 'consensus-labels', id: id ?? `con-${Date.parse(now).toString(36)}-${count + 1}`, dataset, segmentation, name, members, mapping, rule, by, created: now };
  const rl = rule.kind === 'majority' ? `a majority of ${rule.min}` : rule.kind === 'unanimous' ? 'unanimity' : 'the sets in order';
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, entry],
    history: [...project.history, { time: now, action: 'consensus-labels', detail: `Made the consensus label set "${name}" of ${members.map((m) => m.name).join(', ')} by ${rl}` }],
  };
}

// A consensus's labels from its members' ({ names, label } each, in order) mapped to common names,
// by the rule: { names, label, votes (the members agreeing with each cell's label), voters (the
// members labeling it) }.
export function consensusOf(rule, members, mapping = []) {
  const n = members[0]?.label.length ?? 0;
  const names = [];
  const index = new Map();
  const common = members.map((m, j) => m.names.map((name) => {
    const to = String(mapping[j]?.[name] ?? name).trim() || name;
    if (!index.has(to)) {
      index.set(to, names.length);
      names.push(to);
    }
    return index.get(to);
  }));
  const label = new Int32Array(n).fill(-1);
  const votes = new Uint8Array(n);
  const voters = new Uint8Array(n);
  const tally = new Map();
  for (let i = 0; i < n; i += 1) {
    tally.clear();
    let first = -1;
    for (let j = 0; j < members.length; j += 1) {
      const k = members[j].label[i];
      if (k < 0) continue;
      const c = common[j][k];
      voters[i] += 1;
      if (first < 0) first = c;
      tally.set(c, (tally.get(c) ?? 0) + 1);
    }
    if (!voters[i]) continue;
    let best = -1;
    let most = 0;
    let tied = false;
    for (const [c, v] of tally) {
      if (v > most) {
        best = c;
        most = v;
        tied = false;
      } else if (v === most) tied = true;
    }
    if (rule.kind === 'priority') {
      label[i] = first;
      votes[i] = tally.get(first);
    } else if (rule.kind === 'unanimous') {
      if (voters[i] === members.length && tally.size === 1) {
        label[i] = best;
        votes[i] = most;
      }
    } else if (most >= rule.min && !tied) {
      label[i] = best;
      votes[i] = most;
    }
  }
  return { names, label, votes, voters };
}

// --- Side by side --------------------------------------------------------------------------------

// Two label sets over the same cells ({ names, label } each): each pair of labels' cells, over the
// cells both label, and the share of them whose labels have the same name.
export function crossTable(a, b) {
  const table = a.names.map(() => new Array(b.names.length).fill(0));
  let cells = 0;
  let same = 0;
  const n = Math.min(a.label.length, b.label.length);
  for (let i = 0; i < n; i += 1) {
    const x = a.label[i];
    const y = b.label[i];
    if (x < 0 || y < 0) continue;
    table[x][y] += 1;
    cells += 1;
    if (a.names[x] === b.names[y]) same += 1;
  }
  const rowSums = table.map((r) => r.reduce((s, v) => s + v, 0));
  const colSums = b.names.map((_, j) => table.reduce((s, r) => s + r[j], 0));
  return { table, cells, rowSums, colSums, agreement: cells ? same / cells : 0 };
}

// A cell's labels' tally among some cells (indices): [{ label, count }] by count, the most first.
export function tally(label, cells) {
  const count = new Map();
  for (const i of cells) {
    const k = label[i];
    if (k >= 0) count.set(k, (count.get(k) ?? 0) + 1);
  }
  return [...count.entries()].map(([k, c]) => ({ label: k, count: c })).sort((x, y) => y.count - x.count || x.label - y.label);
}

// The k nearest cells (indices) to cell i by centroid (x, y), among those with a label.
export function nearestCells(x, y, label, i, k) {
  const best = [];
  for (let j = 0; j < x.length; j += 1) {
    if (j === i || label[j] < 0) continue;
    const d = (x[j] - x[i]) * (x[j] - x[i]) + (y[j] - y[i]) * (y[j] - y[i]);
    if (best.length < k || d < best[best.length - 1][0]) {
      best.push([d, j]);
      best.sort((p, q) => p[0] - q[0] || p[1] - q[1]);
      if (best.length > k) best.pop();
    }
  }
  return best.map(([, j]) => j);
}
