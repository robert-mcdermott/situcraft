// Labels (internal/label, api_labels.go): marker sets kept as versions in the project (records of
// kind 'marker-set', each a file SituCraft reads), label sets the program makes from a clustering
// and a marker set or imports from a table for a segmentation's cells (artifacts of kind 'labels',
// recorded with the marker set version they used, or their source), and what the page says of
// them: each cluster's labels, names from them, colors and fills.

import { categoricalColor } from './colormaps.js';

// The parameters, as the program names them, with what each does (the defaults come from the
// program: /api/labels/defaults).
export const LABEL_PARAMETERS = [
  { id: 'margin', label: 'Margin', step: 0.01, min: 0.01, max: 5, help: 'How far a level\'s best label must lead the next for a cell to take it; else the cell stays at the level above. Scores are in log-normalized expression: 0.1 is about one transcript more, in a cell of the median total, of a 6-gene label\'s markers than of the next label\'s.' },
  { id: 'minimum', label: 'Minimum score', step: 0.05, min: -5, max: 5, help: 'The score a label must reach to be taken at all. 0: its markers are expressed above their control genes.' },
  { id: 'state', label: 'State threshold', step: 0.05, min: -5, max: 5, help: 'The score a state\'s markers (proliferation, say) must reach for a cell to carry it. 0.25 is about two transcripts of a 5-gene state in a cell of the median total.' },
  { id: 'bins', label: 'Expression bins', step: 1, min: 2, max: 1000, help: 'The genes are cut into this many bins by mean expression; a label\'s control genes come from its markers\' bins (scanpy\'s n_bins).' },
  { id: 'controls', label: 'Control genes per bin', step: 1, min: 1, max: 10000, help: 'At most this many control genes from each bin, drawn at random from bins holding more (scanpy\'s ctrl_size).' },
  { id: 'seed', label: 'Seed', step: 1, min: 0, max: 2 ** 53, help: 'The draws of control genes from bins holding more than the controls per bin (none on panels of up to 1,200 genes at the defaults).' },
];

// The parameters checked as the program checks them (internal/label Params.Check): an error's
// message, or null.
export function checkLabelParameters(p) {
  for (const spec of LABEL_PARAMETERS) {
    const v = p[spec.id];
    if (!Number.isFinite(v) || v < spec.min || v > spec.max || (spec.step === 1 && !Number.isInteger(v))) {
      return `${spec.label} is ${spec.step === 1 ? 'a whole number ' : 'a number '}from ${spec.min.toLocaleString('en-US')} to ${spec.max.toLocaleString('en-US')}.`;
    }
  }
  if (typeof p.lineage !== 'boolean') return 'Say whether a label\'s lineage\'s markers are left out of its controls.';
  return null;
}

// --- Marker sets: versions in the project ----------------------------------------------------

// The latest version of each marker set in the project, newest first.
export function markerSetsOf(project) {
  const latest = new Map();
  for (const a of project.artifacts) {
    if (a.kind !== 'marker-set') continue;
    const was = latest.get(a.family);
    if (!was || a.version > was.version) latest.set(a.family, a);
  }
  return [...latest.values()].sort((a, b) => (a.created < b.created ? 1 : -1));
}

// The versions of one marker set, oldest first.
export function markerSetVersions(project, family) {
  return project.artifacts.filter((a) => a.kind === 'marker-set' && a.family === family).sort((a, b) => a.version - b.version);
}

// The project with a marker set saved: a new version of family (or a new set when family is null),
// unless its checksum is the latest version's. source says where it came from: { starter } or
// { file } or { edited: true }.
export function saveMarkerSet(project, { set, checksum, family = null, source = { edited: true }, by = 'the user', now = new Date().toISOString(), id = null }) {
  const versions = family ? markerSetVersions(project, family) : [];
  const previous = versions.at(-1) ?? null;
  if (previous && previous.checksum === checksum) return project;
  const version = (previous?.version ?? 0) + 1;
  const recordId = id ?? `ms-${checksum.slice(0, 12)}-${version}`;
  const entry = { kind: 'marker-set', id: recordId, family: family ?? recordId, version, parent: previous?.id ?? null, name: set.name, set, checksum, source, by, created: now };
  const from = source.starter ? ` from SituCraft's starter set "${source.starter}"` : source.file ? ` from the file ${source.file}` : '';
  const counts = `${set.labels.length} label${set.labels.length === 1 ? '' : 's'}${set.states?.length ? `, ${set.states.length} state${set.states.length === 1 ? '' : 's'}` : ''}`;
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, entry],
    history: [...project.history, { time: now, action: 'marker-set', detail: `${previous ? 'Saved' : 'Added'} the marker set "${set.name}"${from} (version ${version}: ${counts}; checksum ${checksum.slice(0, 12)})` }],
  };
}

// The marker set as a file to save: SituCraft's format, indented.
export function markerSetFile(set) {
  return `${JSON.stringify(set, null, 1)}\n`;
}

// --- Editing a marker set (copies; the program checks the result) ------------------------------

// A gene list typed: genes split on commas, semicolons and spaces, each once.
export function parseGenes(text) {
  const seen = new Set();
  const out = [];
  for (const g of String(text).split(/[\s,;]+/)) {
    if (!g || seen.has(g.toUpperCase())) continue;
    seen.add(g.toUpperCase());
    out.push(g);
  }
  return out;
}

// The labels in tree order (each after its parent, its children after it), with their depths.
export function labelTree(set) {
  const children = new Map();
  for (const l of set.labels) {
    const key = l.parent ?? '';
    if (!children.has(key)) children.set(key, []);
    children.get(key).push(l);
  }
  const out = [];
  const walk = (parent, depth) => {
    for (const l of children.get(parent) ?? []) {
      out.push({ label: l, depth });
      walk(l.name, depth + 1);
    }
  };
  walk('', 0);
  return out;
}

// A copy of the set with a label added under parent (at the end of its subtree), named uniquely.
export function addLabel(set, parent = '') {
  const taken = new Set([...set.labels, ...(set.states ?? [])].map((l) => l.name.toLowerCase()));
  let name = 'New label';
  for (let k = 2; taken.has(name.toLowerCase()); k += 1) name = `New label ${k}`;
  const label = { name, positive: [], ...(parent ? { parent } : {}) };
  const order = labelTree(set).map((e) => e.label);
  let at = order.length;
  if (parent) {
    const i = order.findIndex((l) => l.name === parent);
    const depthOf = (l) => {
      let d = 0;
      for (let p = l.parent; p; p = set.labels.find((x) => x.name === p)?.parent) d += 1;
      return d;
    };
    const d = depthOf(order[i]);
    at = i + 1;
    while (at < order.length && depthOf(order[at]) > d) at += 1;
  }
  return { ...set, labels: [...order.slice(0, at), label, ...order.slice(at)] };
}

// A copy of the set without a label, its descendants, and states of them.
export function removeLabel(set, name) {
  const gone = new Set([name]);
  for (let grew = true; grew;) {
    grew = false;
    for (const l of set.labels) {
      if (l.parent && gone.has(l.parent) && !gone.has(l.name)) {
        gone.add(l.name);
        grew = true;
      }
    }
  }
  return { ...set, labels: set.labels.filter((l) => !gone.has(l.name)), states: (set.states ?? []).filter((s) => !gone.has(s.parent ?? '')) };
}

// A copy of the set with a label (or state) changed; a rename carries to its children and states.
export function changeLabel(set, name, changes, { state = false } = {}) {
  const list = state ? 'states' : 'labels';
  const next = { ...set, [list]: (set[list] ?? []).map((l) => (l.name === name ? clean({ ...l, ...changes }) : l)) };
  if (!state && changes.name && changes.name !== name) {
    next.labels = next.labels.map((l) => (l.parent === name ? { ...l, parent: changes.name } : l));
    next.states = (next.states ?? []).map((s) => (s.parent === name ? { ...s, parent: changes.name } : s));
  }
  return next;
}

function clean(l) {
  const out = { ...l };
  if (!out.parent) delete out.parent;
  if (!out.negative?.length) delete out.negative;
  if (!out.references?.length) delete out.references;
  return out;
}

// The set with the genes a panel lacks removed (labels left without a positive marker removed with
// them), and what was removed: { set, removed: [{ label, genes }], dropped: [label names] }.
export function withoutMissing(set, missing) {
  const removed = [];
  const fix = (l) => {
    const gone = new Set((missing[l.name] ?? []).map((g) => g.toUpperCase()));
    if (!gone.size) return l;
    removed.push({ label: l.name, genes: missing[l.name] });
    return clean({ ...l, positive: l.positive.filter((g) => !gone.has(g.toUpperCase())), negative: (l.negative ?? []).filter((g) => !gone.has(g.toUpperCase())) });
  };
  let next = { ...set, labels: set.labels.map(fix), states: (set.states ?? []).map(fix) };
  const dropped = [];
  for (const l of [...next.labels]) {
    if (!l.positive.length && next.labels.some((x) => x.name === l.name)) {
      dropped.push(l.name);
      next = removeLabel(next, l.name);
    }
  }
  next.states = next.states.filter((s) => {
    if (s.positive.length) return true;
    dropped.push(s.name);
    return false;
  });
  if (!next.states.length) delete next.states;
  return { set: next, removed, dropped };
}

// --- Label sets: the program's artifacts -------------------------------------------------------

// The project's record of a label set: its clustering and the marker set version it used.
export function labelsRecord(dataset, manifest, markerSet = null, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  const parent = (kind) => (lineage.parents ?? []).find((p) => p.kind === kind)?.id ?? null;
  const params = lineage.parameters ?? {};
  const imported = lineage.method?.name === 'Imported labels';
  return {
    kind: 'labels',
    id: manifest.id,
    name: manifest.name,
    dataset: dataset.id,
    segmentation: parent('segmentation'),
    clustering: parent('clustering'),
    markerSet,
    markers: imported ? null : params.markers ?? null,
    source: imported ? params : null,
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

export function labelSetsOf(project, datasetId, clusteringId = null) {
  return project.artifacts.filter((a) => a.kind === 'labels' && a.dataset === datasetId && (!clusteringId || a.clustering === clusteringId));
}

// The label sets a clustering's cells carry: those made from it, and those imported for its
// segmentation.
export function labelSetsFor(project, clustering) {
  return project.artifacts.filter((a) => a.kind === 'labels' && a.dataset === clustering.dataset && (a.clustering === clustering.id || (a.source && a.segmentation === clustering.segmentation)));
}

// A label set's name in a line: by its marker set, or its source.
export function labelSetName(record) {
  const src = record.source;
  if (!src) return `Labels by ${record.markers?.name ?? '?'}`;
  if (src.format === 'instrument') return `The instrument's ${String(src.title ?? 'clusters').toLowerCase()} clusters`;
  return `${src.column} from ${src.file}`;
}

// The project with a label set recorded (unchanged when it already is).
export function addLabels(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'labels' && a.id === record.id && a.dataset === record.dataset)) return project;
  const clustering = project.artifacts.find((a) => a.kind === 'clustering' && a.id === record.clustering);
  const ms = project.artifacts.find((a) => a.kind === 'marker-set' && a.id === record.markerSet);
  const s = record.summary ?? {};
  const level = s.levels?.[0] ?? 'top';
  if (record.source) {
    const seg = project.artifacts.find((a) => a.kind === 'segmentation' && a.id === record.segmentation);
    return {
      ...project,
      modified: now,
      artifacts: [...project.artifacts, record],
      history: [...project.history, { time: now, action: 'labels', detail: `Imported ${labelSetName(record).charAt(0).toLowerCase()}${labelSetName(record).slice(1)} for the cells of ${(seg?.name ?? 'a segmentation').toLowerCase()}: ${s.labels ?? '?'} labels, ${s.labeled?.[0] ?? '?'} of ${s.cells ?? '?'} cells labeled (SHA-256 ${String(record.source.sha256 ?? '').slice(0, 12)}; artifact ${record.id})` }],
    };
  }
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'labels', detail: `Labeled the cells of ${(clustering?.name ?? 'a clustering').toLowerCase()} by the marker set "${record.markers?.name ?? '?'}"${ms ? ` (version ${ms.version})` : ''}: ${s.labeled?.[0] ?? '?'} of ${s.cells ?? '?'} cells at the ${level.toLowerCase()} level, margin ${record.parameters?.margin ?? '?'} (artifact ${record.id})` }],
  };
}

// What a label set is, in a line.
export function describeLabels(record) {
  const s = record.summary ?? {};
  if (record.source) {
    const src = record.source;
    const from = src.format === 'instrument' ? `${src.tool ?? 'the instrument'} ${src.version ?? ''}`.trim() : `${src.file}, column ${src.column}${src.confidenceColumn ? `, confidence ${src.confidenceColumn}` : ''}`;
    return `${s.labels ?? '?'} labels imported from ${from}, matched to the cells by ${src.idColumn}: ${(s.labeled?.[0] ?? 0).toLocaleString('en-US')} of ${(s.cells ?? 0).toLocaleString('en-US')} cells labeled`;
  }
  const p = record.parameters ?? {};
  const pct = (v) => (s.cells ? `${Math.round((100 * v) / s.cells)}%` : '?');
  const levels = (s.levels ?? []).map((l, i) => `${l.toLowerCase()} ${pct(s.labeled?.[i] ?? 0)}`).join(', ');
  return `${s.labels ?? '?'} labels${s.states ? ` and ${s.states} state${s.states === 1 ? '' : 's'}` : ''} by "${record.markers?.name ?? '?'}" over ${(s.cells ?? 0).toLocaleString('en-US')} cells: labeled at the ${levels}; margin ${p.margin ?? '?'}, minimum ${p.minimum ?? '?'}`;
}

// A label set's cells as /api/labels/{id}/cells answers them: { segCells, n, levels, states, cells,
// cluster, label: [Int32Array per level], lead, confidence, state: [Uint8Array per state] }, the
// label at each level the one the cell carries there (its own, or the one above where it stopped).
export function readLabelCells(buffer) {
  const head = new Uint32Array(buffer, 0, 4);
  const [segCells, n, levels, states] = head;
  let at = 16;
  const u32 = () => {
    const v = new Uint32Array(buffer, at, n);
    at += 4 * n;
    return v;
  };
  const cells = u32();
  const cluster = u32();
  const label = [];
  for (let l = 0; l < levels; l += 1) {
    label.push(new Int32Array(buffer, at, n));
    at += 4 * n;
  }
  const lead = [];
  for (let l = 0; l < levels; l += 1) {
    lead.push(new Float32Array(buffer, at, n));
    at += 4 * n;
  }
  const confidence = new Float32Array(buffer, at, n);
  at += 4 * n;
  const state = [];
  for (let s = 0; s < states; s += 1) {
    state.push(new Uint8Array(buffer, at, n));
    at += n;
  }
  return { segCells, n, levels, states, cells, cluster, label, lead, confidence, state };
}

// Per segmentation cell, the label each carries at a level: −1 for a cell not labeled (not
// analyzed), unlabeled for one analyzed without a label.
export function labelsPerCell(data, level, unlabeled) {
  const out = new Int32Array(data.segCells).fill(-1);
  const lab = data.label[Math.min(level, data.levels - 1)];
  for (let i = 0; i < data.n; i += 1) out[data.cells[i]] = lab[i] >= 0 ? lab[i] : unlabeled;
  return out;
}

// A label's color: its own where the table gave one (sets, the description's), else each label of
// the set its own categorical color; the unlabeled grey.
export function labelColor(k, unlabeled, sets = null) {
  if (k === unlabeled) return '#9a9a9a';
  return sets?.[k]?.color || categoricalColor(k);
}

// RGBA bytes per cell for labels per cell (labelsPerCell): cells not analyzed transparent.
export function labelColors(perCell, unlabeled, sets = null) {
  const out = new Uint8Array(4 * perCell.length);
  const cache = new Map();
  for (let i = 0; i < perCell.length; i += 1) {
    const k = perCell[i];
    if (k < 0) continue;
    if (!cache.has(k)) {
      const v = parseInt(labelColor(k, unlabeled, sets).slice(1), 16);
      cache.set(k, [(v >> 16) & 255, (v >> 8) & 255, v & 255, k === unlabeled ? 150 : 255]);
    }
    out.set(cache.get(k), 4 * i);
  }
  return out;
}

// A description's composition for a clustering's cells, counted here (imported labels are of a
// segmentation's cells, in no clustering): each cluster's cells (cluster per segmentation cell, −1
// for none) by the label they carry at each level, the last count those without a label.
export function compositionOf(data, cluster, clusters) {
  const sets = data.description.sets.length;
  const levels = data.cells.levels;
  const comp = Array.from({ length: levels }, () => Array.from({ length: clusters }, () => new Array(sets + 1).fill(0)));
  for (let i = 0; i < data.cells.n; i += 1) {
    const c = cluster[data.cells.cells[i]];
    if (!(c >= 0 && c < clusters)) continue;
    for (let l = 0; l < levels; l += 1) {
      const k = data.cells.label[l][i];
      comp[l][c][k >= 0 ? k : sets] += 1;
    }
  }
  return comp;
}

// Each cluster's labels at a level (from the description's composition), largest first: [{ cluster,
// cells, labels: [{ label (index, or null for none), name, count, share }] }].
export function composition(description, level) {
  const sets = description.sets;
  return (description.composition?.[level] ?? []).map((counts, c) => {
    const cells = counts.reduce((a, b) => a + b, 0);
    const labels = counts.map((count, k) => ({ label: k < sets.length ? k : null, name: k < sets.length ? sets[k].name : 'No label', count, share: cells ? count / cells : 0 }))
      .filter((e) => e.count > 0).sort((a, b) => b.count - a.count || (a.label ?? 1e9) - (b.label ?? 1e9));
    return { cluster: c, cells, labels };
  });
}

// Names for a clustering's clusters from a label set: each cluster the most specific label that at
// least share of its cells carry (from the deepest level up), or null where none does.
export function namesFromLabels(description, share = 0.5) {
  const levels = description.levels.length;
  return Array.from({ length: description.clusters }, (_, c) => {
    for (let l = levels - 1; l >= 0; l -= 1) {
      const counts = description.composition[l][c];
      const cells = counts.reduce((a, b) => a + b, 0);
      let best = -1;
      for (let k = 0; k < description.sets.length; k += 1) if (counts[k] > 0 && (best < 0 || counts[k] > counts[best])) best = k;
      if (best >= 0 && cells && counts[best] / cells >= share) return description.sets[best].name;
    }
    return null;
  });
}

// The fills a label set offers the Viewer: 'labels:<id>:<level>' for each level of each complete
// label set of a segmentation's clusterings.
export function labelFills(project, datasetId, segmentationId) {
  return project.artifacts.filter((a) => a.kind === 'labels' && a.dataset === datasetId && a.segmentation === segmentationId && a.state === 'complete').flatMap((r) => (r.summary?.levels ?? []).map((level, l) => ({
    id: `labels:${r.id}:${l}`,
    labels: r.id,
    level: l,
    label: r.source ? `Labels: ${labelSetName(r)}` : `${labelSetName(r)}: ${level.toLowerCase()}`,
    help: describeLabels(r),
  })));
}

// A fill's label set and level, or null.
export function parseLabelFill(fill) {
  const m = /^labels:([^:]+):(\d+)$/.exec(fill ?? '');
  return m ? { id: m[1], level: Number(m[2]) } : null;
}
