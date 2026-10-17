// The review record (wave 5, slice 6): every review of a dataset's cells or regions, appended to the
// project (`reviews`) and never changed. A review is an action (accept, relabel, ambiguous, exclude,
// include, a segmentation concern, its resolution, a comment) on cells (of a segmentation), on a
// clustering's clusters, or on a region, with who made it, when and why; the value before and after;
// the segmentation and the label set it was of; each cell's label and the label's confidence as the
// reviewer saw them; and the post-Xenium images' alignments in use (which version placed each, and
// whether it was shown). Accepting and relabeling cells change their labels by an edit appended to a
// curated label set (lib/curation.js), the edit and the review naming each other: the label review
// of wave 4 is the same record, its earlier edits read as reviews.
//
// Pure functions over the project document; the reviews' effect on each cell and region (its
// review state), their colours, and the record written as CSV and JSON and read back.

import { appendEdit } from './curation.js';
import { editRegion, regionsOf } from './regions.js';

// --- Actions -------------------------------------------------------------------------------------

export const ACTIONS = [
  { id: 'accept', label: 'Accept', help: 'Its label (a region\'s class) is right.' },
  { id: 'relabel', label: 'Relabel', help: 'Its label (a region\'s class) is another: say which, and why.' },
  { id: 'ambiguous', label: 'Ambiguous', help: 'It could be one of several labels; name them if you can.' },
  { id: 'exclude', label: 'Exclude', help: 'Leave it out of analyses (an artifact, a fold, outside the tissue): say why.' },
  { id: 'include', label: 'Include again', help: 'Take back an exclusion.' },
  { id: 'concern', label: 'Segmentation concern', help: 'Its segmentation looks wrong: say how.' },
  { id: 'resolve', label: 'Resolve the concern', help: 'The concern is answered (or was mistaken).' },
  { id: 'comment', label: 'Comment', help: 'A note, changing nothing.' },
];

export const CONCERNS = [
  { id: 'merged', label: 'Cells merged', help: 'Two cells or more drawn as one.' },
  { id: 'split', label: 'Cell split', help: 'One cell drawn as several.' },
  { id: 'boundary', label: 'Boundary off', help: 'Its outline misses part of the cell, or takes in another\'s.' },
  { id: 'not-a-cell', label: 'Not a cell', help: 'Debris, background or an artifact.' },
  { id: 'missed', label: 'Cells missed', help: 'Cells here were not segmented.' },
  { id: 'other', label: 'Other', help: 'Say what in the note.' },
];

const LABEL_ACTIONS = new Set(['accept', 'relabel', 'ambiguous']);
const actionLabel = (id) => ACTIONS.find((a) => a.id === id)?.label ?? id;

// --- The record ----------------------------------------------------------------------------------

// The reviews of a dataset (or all), in order: those recorded, and curated edits made before the
// record (or by a path not writing one) read as reviews (`derived`: their label before, the cells'
// IDs, the alignments in use were not kept).
export function reviewsOf(project, datasetId = null) {
  const recorded = (project.reviews ?? []).filter((r) => !datasetId || r.dataset === datasetId);
  const derived = [];
  for (const set of project.artifacts.filter((a) => a.kind === 'curated-labels' && (!datasetId || a.dataset === datasetId))) {
    set.edits.forEach((e, index) => {
      if (e.review) return;
      const seg = project.artifacts.find((a) => a.id === set.segmentation);
      derived.push({
        id: `edit-${set.id}-${index}`, derived: true, time: e.time, by: e.by, action: e.kind === 'accept' ? 'accept' : 'relabel', dataset: set.dataset,
        segmentation: { id: set.segmentation, name: seg?.name ?? set.segmentation }, target: e.kind === 'cluster' ? 'clusters' : 'cells',
        ...(e.kind === 'cluster' ? { clusters: { clustering: e.clustering, clusters: e.clusters } } : { cells: e.cells.map((row) => ({ row, id: null, label: null, confidence: null, previous: null })) }),
        labels: { id: set.id, level: 0, name: set.name, kind: 'curated' }, previous: null, next: e.label, note: e.reason, images: [], edits: [{ set: set.id, index }],
      });
    });
  }
  return [...recorded, ...derived].sort((a, b) => (a.time < b.time ? -1 : a.time > b.time ? 1 : 0));
}

// The one value of a list, or 'mixed'.
function summary(values) {
  const set = new Set(values.map((v) => v ?? 'none'));
  return set.size === 1 ? [...set][0] : 'mixed';
}

// A problem with a review, or null.
export function checkReview(input) {
  const a = input.action;
  if (!ACTIONS.some((x) => x.id === a)) return `A review is one of: ${ACTIONS.map((x) => x.label.toLowerCase()).join(', ')}.`;
  if (!['cells', 'clusters', 'region'].includes(input.target)) return 'A review is of cells, clusters or a region.';
  if (input.target === 'cells' && !input.cells?.length) return 'Choose the cells.';
  if (input.target === 'clusters' && !(input.clusters?.clustering && input.clusters.clusters?.length)) return 'Choose the clusters.';
  if (input.target === 'region' && !input.region) return 'Choose the region.';
  if (input.target !== 'region' && !input.segmentation?.id) return 'Name the segmentation the cells are of.';
  const note = String(input.note ?? '').trim();
  if ((a === 'relabel' || (a === 'accept' && input.target === 'clusters')) && !String(input.label ?? '').trim()) return a === 'relabel' ? 'Name the new label.' : 'Name the clusters\' label accepted.';
  if (a === 'relabel' && !note) return 'Say why: a relabel keeps its reason.';
  if (a === 'exclude' && !note) return 'Say why it is left out.';
  if (a === 'comment' && !note) return 'Write the comment.';
  if (a === 'concern' && !CONCERNS.some((c) => c.id === input.concern)) return `Say how the segmentation looks wrong: ${CONCERNS.map((c) => c.label.toLowerCase()).join(', ')}.`;
  if (a === 'concern' && input.concern === 'other' && !note) return 'Say what the concern is.';
  if (input.target !== 'region' && (a === 'accept' || a === 'relabel')) {
    if (!input.labels) return 'Show a label set to review its labels.';
    if (input.labels.kind === 'consensus') return 'A consensus is made by its rule: review one of its sets, or a curated set begun from it.';
    if (!input.curated) return 'Name the curated set the labels are kept in.';
    if (a === 'accept' && input.target === 'cells' && input.cells.some((c) => c.label === null || c.label === undefined)) return 'Some of the cells have no label to accept: relabel them.';
  }
  return null;
}

// The project with a review appended: { action, by, note, dataset, target ('cells', 'clusters' or
// 'region'), segmentation ({ id, name }), cells ([{ row, id, label (as shown), confidence }]),
// clusters ({ clustering, clusters }), region (its id), labels ({ id, level, name, kind }: the set
// shown), label (relabel: the new one), alternatives (ambiguous), concern, images ([{ image, name,
// transform, version, shown }]), curated (the curated set accepting and relabeling cells edit) }.
// Accepting or relabeling cells or clusters appends an edit to the curated set (one per label
// accepted); relabeling a region appends a version of it with the new class.
export function addReview(project, input, { now = new Date().toISOString(), id = null } = {}) {
  const problem = checkReview(input);
  if (problem) throw new Error(problem);
  const ds = project.datasets.find((d) => d.id === input.dataset);
  if (!ds) throw new Error('The project has no such dataset.');
  const all = project.reviews ?? [];
  const reviewId = id ?? `rev-${Date.parse(now).toString(36)}-${all.length + 1}`;
  const by = String(input.by ?? '').trim() || 'the user';
  const note = String(input.note ?? '').trim() || (input.action === 'accept' ? 'Accepted on review' : input.action === 'include' ? 'Included again' : input.action === 'resolve' ? 'Resolved' : '');
  const record = { id: reviewId, time: now, by, action: input.action, dataset: input.dataset, target: input.target };
  let next = project;
  // What it changes, before and after.
  const prior = reviewsOf(project, input.dataset);
  if (input.target === 'region') {
    const region = regionsOf(project, input.dataset, { removed: true }).find((r) => r.id === input.region);
    if (!region) throw new Error('The dataset has no such region.');
    if (region.removed) throw new Error(`${region.name} is removed.`);
    const state = regionReviewState(prior).get(region.id) ?? {};
    record.region = { id: region.id, name: region.name, version: region.version, cls: region.cls };
    if (input.segmentation) record.segmentation = { id: input.segmentation.id, name: input.segmentation.name };
    if (input.cells?.length) record.cells = cellsOf(input.cells, null);
    [record.previous, record.next] = valuesOf(input, region.cls, state);
    if (input.action === 'relabel') {
      next = editRegion(next, region.id, { cls: String(input.label).trim() }, { by, note: `Relabeled on review: ${note}`, now });
      record.region.version = region.version + 1;
      record.region.cls = String(input.label).trim();
    }
  } else {
    record.segmentation = { id: input.segmentation.id, name: input.segmentation.name };
    if (input.target === 'cells') {
      const states = statesBefore(prior, input.segmentation.id, input.cells.map((c) => c.row));
      record.cells = cellsOf(input.cells, (c, k) => previousOf(input.action, c, states[k]));
      [record.previous, record.next] = valuesOf(input, summary(record.cells.map((c) => c.previous)), null);
    } else {
      record.clusters = { clustering: input.clusters.clustering, clusters: [...input.clusters.clusters] };
      [record.previous, record.next] = valuesOf(input, input.previous ?? null, null);
    }
    if (input.labels) record.labels = { id: input.labels.id, level: input.labels.level ?? 0, name: input.labels.name, kind: input.labels.kind };
    // The labels changed by edits to the curated set, each naming this review.
    if (input.action === 'accept' || input.action === 'relabel') {
      record.edits = [];
      const groups = input.action === 'accept' && input.target === 'cells'
        ? groupBy(input.cells, (c) => c.label)
        : [{ label: input.action === 'accept' ? input.label : String(input.label).trim(), cells: input.cells }];
      for (const g of groups) {
        const edit = input.target === 'clusters'
          ? { kind: 'cluster', clustering: input.clusters.clustering, clusters: input.clusters.clusters, label: g.label, reason: note, by, review: reviewId }
          : { kind: input.action === 'accept' ? 'accept' : 'cells', cells: g.cells.map((c) => c.row), label: g.label, reason: note, by, review: reviewId };
        if (input.action === 'accept' && input.target === 'clusters') edit.kind = 'cluster';
        next = appendEdit(next, input.curated, edit, now);
        const set = next.artifacts.find((a) => a.id === input.curated);
        record.edits.push({ set: set.id, index: set.edits.length - 1 });
      }
    }
  }
  if (input.action === 'ambiguous' && input.alternatives?.length) record.alternatives = [...new Set(input.alternatives.map((x) => String(x).trim()).filter(Boolean))];
  if (input.action === 'concern') record.concern = input.concern;
  record.note = note;
  record.images = (input.images ?? []).map((m) => ({ image: m.image, name: m.name, transform: m.transform ?? null, version: m.version ?? null, shown: m.shown ?? null }));
  const what = record.target === 'region' ? record.region.name : record.target === 'clusters' ? `${record.clusters.clusters.length} cluster${record.clusters.clusters.length === 1 ? '' : 's'}` : `${record.cells.length} cell${record.cells.length === 1 ? '' : 's'}`;
  const change = record.previous === record.next || record.next === null ? '' : `: ${record.previous ?? 'none'} → ${record.next}`;
  return {
    ...next,
    modified: now,
    reviews: [...(next.reviews ?? []), record],
    history: [...next.history, { time: now, action: 'review', detail: `${actionLabel(record.action)} ${what} of ${ds.name}${change}${note ? ` (${note}; ${by})` : ` (${by})`}` }],
  };
}

// Items grouped by a key, in the order the keys first come: [{ label, cells }].
function groupBy(items, key) {
  const groups = new Map();
  for (const it of items) {
    const k = key(it);
    if (!groups.has(k)) groups.set(k, []);
    groups.get(k).push(it);
  }
  return [...groups.entries()].map(([label, cells]) => ({ label, cells }));
}

function cellsOf(cells, previous) {
  return cells.map((c, k) => ({ row: c.row, id: c.id ?? null, label: c.label ?? null, confidence: Number.isFinite(c.confidence) ? c.confidence : null, previous: previous ? previous(c, k) : null }));
}

// A cell's value before the action: its label (a label action), whether it was excluded, or its
// concern.
function previousOf(action, cell, state) {
  if (LABEL_ACTIONS.has(action)) return cell.label ?? null;
  if (action === 'exclude' || action === 'include') return state.excluded ? 'excluded' : 'included';
  if (action === 'concern' || action === 'resolve') return state.concern ?? 'none';
  return null;
}

// The record's value before and after.
function valuesOf(input, previous, regionState) {
  const a = input.action;
  if (input.target === 'region' && regionState) {
    if (a === 'exclude' || a === 'include') previous = regionState.excluded ? 'excluded' : 'included';
    if (a === 'concern' || a === 'resolve') previous = regionState.concern ?? 'none';
  }
  switch (a) {
    case 'accept': return [previous, input.target === 'clusters' ? String(input.label) : previous];
    case 'relabel': return [previous, String(input.label).trim()];
    case 'ambiguous': return [previous, 'ambiguous'];
    case 'exclude': return [previous, 'excluded'];
    case 'include': return [previous, 'included'];
    case 'concern': return [previous, input.concern];
    case 'resolve': return [previous, 'none'];
    default: return [null, null];
  }
}

// Each row's state before a review, from the reviews of a segmentation's cells.
function statesBefore(reviews, segmentationId, rows) {
  const wanted = new Map(rows.map((r) => [r, { excluded: false, concern: null }]));
  for (const r of reviews) {
    if (r.target !== 'cells' || r.segmentation?.id !== segmentationId) continue;
    for (const c of r.cells) {
      const s = wanted.get(c.row);
      if (!s) continue;
      if (r.action === 'exclude') s.excluded = true;
      if (r.action === 'include') s.excluded = false;
      if (r.action === 'concern') s.concern = r.concern;
      if (r.action === 'resolve') s.concern = null;
    }
  }
  return rows.map((r) => wanted.get(r));
}

// --- Review states -------------------------------------------------------------------------------

export const LABEL_STATUS = ['', 'accepted', 'relabeled', 'ambiguous'];

// Each cell's state from the reviews of a segmentation's n cells, the latest deciding: its label's
// (0 none, 1 accepted, 2 relabeled, 3 ambiguous), excluded (0 or 1), its concern (0 none, else 1 +
// its index in CONCERNS), its comments, and how many cells any review touched. clusterOf(clustering)
// answers a clustering's cluster per cell, for reviews of clusters (else they are not counted).
export function cellReviewState(reviews, segmentationId, n, clusterOf = () => null) {
  const label = new Uint8Array(n);
  const excluded = new Uint8Array(n);
  const concern = new Uint8Array(n);
  const comments = new Uint16Array(n);
  const touched = new Uint8Array(n);
  const apply = (r, i) => {
    if (i < 0 || i >= n) return;
    touched[i] = 1;
    switch (r.action) {
      case 'accept': label[i] = 1; break;
      case 'relabel': label[i] = 2; break;
      case 'ambiguous': label[i] = 3; break;
      case 'exclude': excluded[i] = 1; break;
      case 'include': excluded[i] = 0; break;
      case 'concern': concern[i] = 1 + CONCERNS.findIndex((c) => c.id === r.concern); break;
      case 'resolve': concern[i] = 0; break;
      default: comments[i] += 1;
    }
  };
  for (const r of reviews) {
    if (r.segmentation?.id !== segmentationId) continue;
    if (r.target === 'cells') for (const c of r.cells) apply(r, c.row);
    else if (r.target === 'clusters') {
      const cluster = clusterOf(r.clusters.clustering);
      if (!cluster) continue;
      const set = new Set(r.clusters.clusters);
      for (let i = 0; i < Math.min(n, cluster.length); i += 1) if (set.has(cluster[i])) apply(r, i);
    }
  }
  let reviewed = 0;
  for (let i = 0; i < n; i += 1) reviewed += touched[i];
  return { label, excluded, concern, comments, reviewed };
}

// Each region's state from the reviews: { status ('accepted', 'relabeled', 'ambiguous' or ''),
// excluded, concern (an id, or null), comments }, by region id.
export function regionReviewState(reviews) {
  const out = new Map();
  for (const r of reviews) {
    if (r.target !== 'region') continue;
    const s = out.get(r.region.id) ?? { status: '', excluded: false, concern: null, comments: 0 };
    switch (r.action) {
      case 'accept': s.status = 'accepted'; break;
      case 'relabel': s.status = 'relabeled'; break;
      case 'ambiguous': s.status = 'ambiguous'; break;
      case 'exclude': s.excluded = true; break;
      case 'include': s.excluded = false; break;
      case 'concern': s.concern = r.concern; break;
      case 'resolve': s.concern = null; break;
      default: s.comments += 1;
    }
    out.set(r.region.id, s);
  }
  return out;
}

// The states' counts: { accepted, relabeled, ambiguous, excluded, concern, commented, reviewed }.
export function stateCounts(state) {
  const out = { accepted: 0, relabeled: 0, ambiguous: 0, excluded: 0, concern: 0, commented: 0, reviewed: state.reviewed };
  for (let i = 0; i < state.label.length; i += 1) {
    if (state.label[i]) out[LABEL_STATUS[state.label[i]]] += 1;
    out.excluded += state.excluded[i];
    out.concern += state.concern[i] ? 1 : 0;
    out.commented += state.comments[i] ? 1 : 0;
  }
  return out;
}

// The colours of the states (Okabe and Ito's palette, apart in every colour vision), the first that
// holds of a cell drawn: excluded, a concern, ambiguous, relabeled, accepted, commented.
export const STATUS_COLORS = [
  { id: 'excluded', label: 'Excluded', color: [140, 140, 140] },
  { id: 'concern', label: 'Segmentation concern', color: [213, 94, 0] },
  { id: 'ambiguous', label: 'Ambiguous', color: [230, 159, 0] },
  { id: 'relabeled', label: 'Relabeled', color: [0, 114, 178] },
  { id: 'accepted', label: 'Accepted', color: [0, 158, 115] },
  { id: 'commented', label: 'Commented', color: [204, 121, 167] },
];

// Each cell's colour by its state (RGBA, clear for none).
export function reviewColors(state) {
  const n = state.label.length;
  const out = new Uint8Array(4 * n);
  const set = (i, k) => {
    const [r, g, b] = STATUS_COLORS[k].color;
    out[4 * i] = r;
    out[4 * i + 1] = g;
    out[4 * i + 2] = b;
    out[4 * i + 3] = 255;
  };
  for (let i = 0; i < n; i += 1) {
    if (state.excluded[i]) set(i, 0);
    else if (state.concern[i]) set(i, 1);
    else if (state.label[i] === 3) set(i, 2);
    else if (state.label[i] === 2) set(i, 3);
    else if (state.label[i] === 1) set(i, 4);
    else if (state.comments[i]) set(i, 5);
  }
  return out;
}

// --- Written and read back -----------------------------------------------------------------------

export const CSV_COLUMNS = ['review', 'time', 'reviewer', 'action', 'dataset', 'dataset_name', 'segmentation', 'segmentation_name', 'target', 'cell_id', 'cell_row', 'clustering', 'cluster', 'region', 'region_name', 'region_version', 'label_set', 'label_set_name', 'label_set_level', 'label', 'confidence', 'previous', 'new', 'concern', 'alternatives', 'note', 'images', 'derived'];

const csvField = (v) => {
  const s = v === null || v === undefined ? '' : String(v);
  return /[",\n\r]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
};

// The reviews as CSV, a row for each cell (or cluster, or the region) a review is of: a cell's own
// label as shown, its confidence and its value before; the review's value after.
export function reviewsCSV(reviews, datasetNames = {}) {
  const lines = [CSV_COLUMNS.join(',')];
  for (const r of reviews) {
    const images = (r.images ?? []).map((m) => `${m.name}: ${m.version ?? 'not aligned'} (${m.transform ?? '-'})${m.shown === true ? ', shown' : m.shown === false ? ', not shown' : ''}`).join('; ');
    const base = {
      review: r.id, time: r.time, reviewer: r.by, action: r.action, dataset: r.dataset, dataset_name: datasetNames[r.dataset] ?? '', segmentation: r.segmentation?.id ?? '', segmentation_name: r.segmentation?.name ?? '', target: r.target,
      region: r.region?.id ?? '', region_name: r.region?.name ?? '', region_version: r.region?.version ?? '',
      label_set: r.labels?.id ?? '', label_set_name: r.labels?.name ?? '', label_set_level: r.labels ? r.labels.level : '', previous: r.previous ?? '', new: r.next ?? '',
      concern: r.concern ?? '', alternatives: (r.alternatives ?? []).join('; '), note: r.note ?? '', images, derived: r.derived ? 'yes' : '',
    };
    const rows = r.target === 'cells' ? r.cells.map((c) => ({ ...base, cell_id: c.id ?? '', cell_row: c.row, label: c.label ?? '', confidence: c.confidence ?? '', previous: c.previous ?? base.previous }))
      : r.target === 'clusters' ? r.clusters.clusters.map((k) => ({ ...base, clustering: r.clusters.clustering, cluster: k + 1 }))
        : [{ ...base }];
    for (const row of rows) lines.push(CSV_COLUMNS.map((col) => csvField(row[col])).join(','));
  }
  return `${lines.join('\n')}\n`;
}

// The reviews as JSON: the record itself, with what it is of.
export function reviewsJSON(reviews, { project, datasets = [], situcraft = '', now = new Date().toISOString() } = {}) {
  return {
    format: 'situcraft-reviews',
    version: 1,
    written: now,
    situcraft,
    project: project ? { id: project.id, name: project.name } : null,
    datasets: datasets.map((d) => ({ id: d.id, name: d.name })),
    actions: ACTIONS.map(({ id, label }) => ({ id, label })),
    concerns: CONCERNS.map(({ id, label }) => ({ id, label })),
    reviews,
  };
}

// A CSV file's rows (RFC 4180: quoted fields, doubled quotes, line breaks within quotes), as objects
// by its header.
export function parseCSV(text) {
  const rows = [];
  let row = [];
  let field = '';
  let quoted = false;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (quoted) {
      if (ch === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i += 1;
        } else quoted = false;
      } else field += ch;
    } else if (ch === '"') quoted = true;
    else if (ch === ',') {
      row.push(field);
      field = '';
    } else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && text[i + 1] === '\n') i += 1;
      row.push(field);
      rows.push(row);
      row = [];
      field = '';
    } else field += ch;
  }
  if (field || row.length) {
    row.push(field);
    rows.push(row);
  }
  const [head, ...body] = rows;
  return body.filter((r) => r.length > 1 || r[0]).map((r) => Object.fromEntries(head.map((h, k) => [h, r[k] ?? ''])));
}
