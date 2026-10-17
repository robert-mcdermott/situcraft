// The review record (lib/review-record.js): reviews of cells and regions with their provenance, the
// labels they change through curated sets, the states they leave, and the record written and read
// back.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { addReview, cellReviewState, checkReview, parseCSV, regionReviewState, reviewColors, reviewsCSV, reviewsJSON, reviewsOf, stateCounts, STATUS_COLORS, CSV_COLUMNS } from './review-record.js';
import { applyCurated, appendEdit, createCurated } from './curation.js';
import { addRegion, rectangle, regionsOf } from './regions.js';

const SEG = { id: 'seg-1', name: 'Instrument segmentation' };
const LABELS = { id: 'lab-1', level: 0, name: 'From markers', kind: 'labels' };
const IMAGES = [{ image: 'img-1', name: 'H&E', transform: 'tf-2', version: 'Automatic, affine', shown: true }];
// Five cells' labels and confidences in the predicted set: T, T, B, none, B.
const base = { names: ['T cell', 'B cell'], label: Int32Array.from([0, 0, 1, -1, 1]), confidence: Float32Array.from([0.9, 0.2, 0.7, NaN, 0.4]) };
const shown = (rows) => rows.map((row) => ({ row, id: `cell-${row}`, label: base.label[row] >= 0 ? base.names[base.label[row]] : null, confidence: base.confidence[row] }));

function project() {
  let p = { id: 'p-1', name: 'Test', datasets: [{ id: 'd1', name: 'Rep1' }], artifacts: [{ kind: 'segmentation', id: 'seg-1', name: 'Instrument segmentation' }], history: [], decisions: [], views: [] };
  p = createCurated(p, { dataset: 'd1', segmentation: 'seg-1', base: { labels: 'lab-1', level: 0, name: 'From markers' }, name: 'From markers, reviewed', now: '2026-01-01T00:00:00Z', id: 'cur-1' });
  return p;
}
const curatedLabels = (p) => applyCurated(p.artifacts.find((a) => a.id === 'cur-1'), base);

test('a relabel: its provenance recorded, the labels changed through the curated set, each naming the other', () => {
  let p = project();
  p = addReview(p, { action: 'relabel', by: 'RM', note: 'CD3 negative, CD19 high', dataset: 'd1', target: 'cells', segmentation: SEG, cells: shown([0, 1]), labels: LABELS, label: 'B cell', images: IMAGES, curated: 'cur-1' }, { now: '2026-01-02T00:00:00Z' });
  const r = p.reviews[0];
  assert.deepEqual([r.action, r.by, r.target, r.previous, r.next, r.note], ['relabel', 'RM', 'cells', 'T cell', 'B cell', 'CD3 negative, CD19 high']);
  assert.deepEqual(r.cells.map((c) => [c.row, c.id, c.label, Math.round(c.confidence * 10) / 10, c.previous]), [[0, 'cell-0', 'T cell', 0.9, 'T cell'], [1, 'cell-1', 'T cell', 0.2, 'T cell']]);
  assert.deepEqual(r.segmentation, SEG);
  assert.deepEqual(r.labels, LABELS);
  assert.deepEqual(r.images, IMAGES);
  assert.deepEqual(r.edits, [{ set: 'cur-1', index: 0 }]);
  const set = p.artifacts.find((a) => a.id === 'cur-1');
  assert.equal(set.edits[0].review, r.id);
  // The state it changed: the cells' labels now the new one, no other.
  const after = curatedLabels(p);
  assert.deepEqual([...after.label].map((k) => (k >= 0 ? after.names[k] : null)), ['B cell', 'B cell', 'B cell', null, 'B cell']);
  assert.match(p.history.at(-1).detail, /Relabel 2 cells of Rep1: T cell → B cell \(CD3 negative, CD19 high; RM\)/);
});

test('accepting cells of two labels: one review, an edit for each label', () => {
  let p = project();
  p = addReview(p, { action: 'accept', by: 'AB', dataset: 'd1', target: 'cells', segmentation: SEG, cells: shown([1, 2]), labels: LABELS, curated: 'cur-1' }, { now: '2026-01-02T00:00:00Z' });
  const r = p.reviews[0];
  assert.deepEqual([r.previous, r.next, r.note, r.edits.length], ['mixed', 'mixed', 'Accepted on review', 2]);
  const set = p.artifacts.find((a) => a.id === 'cur-1');
  assert.deepEqual(set.edits.map((e) => [e.kind, e.label, e.cells]), [['accept', 'T cell', [1]], ['accept', 'B cell', [2]]]);
  assert.throws(() => addReview(p, { action: 'accept', dataset: 'd1', target: 'cells', segmentation: SEG, cells: shown([3]), labels: LABELS, curated: 'cur-1' }), /no label to accept/);
});

test('what a review needs', () => {
  const cells = shown([0]);
  const base2 = { dataset: 'd1', target: 'cells', segmentation: SEG, cells };
  assert.match(checkReview({ ...base2, action: 'relabel', label: 'B cell', labels: LABELS, curated: 'cur-1' }), /Say why/);
  assert.match(checkReview({ ...base2, action: 'exclude' }), /Say why it is left out/);
  assert.match(checkReview({ ...base2, action: 'concern' }), /how the segmentation looks wrong/);
  assert.match(checkReview({ ...base2, action: 'comment' }), /Write the comment/);
  assert.match(checkReview({ ...base2, action: 'accept', labels: { ...LABELS, kind: 'consensus' }, curated: 'x' }), /consensus is made by its rule/);
  assert.match(checkReview({ ...base2, action: 'accept' }), /Show a label set/);
  assert.match(checkReview({ ...base2, action: 'shrug' }), /A review is one of/);
  assert.equal(checkReview({ ...base2, action: 'ambiguous', alternatives: ['T cell', 'NK cell'] }), null);
});

test('states: the latest review decides; exclusions and concerns taken back; values before and after', () => {
  let p = project();
  const t = (d) => `2026-01-0${d}T00:00:00Z`;
  const cells = (rows) => ({ dataset: 'd1', target: 'cells', segmentation: SEG, cells: shown(rows), labels: LABELS });
  p = addReview(p, { ...cells([0, 1]), action: 'exclude', note: 'A fold' }, { now: t(2) });
  p = addReview(p, { ...cells([1, 2]), action: 'exclude', note: 'Out of focus' }, { now: t(3) });
  assert.deepEqual([p.reviews[1].previous, p.reviews[1].cells.map((c) => c.previous)], ['mixed', ['excluded', 'included']]);
  p = addReview(p, { ...cells([0]), action: 'include' }, { now: t(4) });
  p = addReview(p, { ...cells([4]), action: 'concern', concern: 'merged', note: 'Two nuclei' }, { now: t(5) });
  p = addReview(p, { ...cells([4]), action: 'ambiguous', alternatives: ['B cell', 'Plasma cell', 'B cell'] }, { now: t(6) });
  p = addReview(p, { ...cells([3]), action: 'comment', note: 'Near the vessel' }, { now: t(7) });
  p = addReview(p, { ...cells([2]), action: 'relabel', label: 'T cell', note: 'CD3', curated: 'cur-1' }, { now: t(8) });
  assert.deepEqual(p.reviews[4].alternatives, ['B cell', 'Plasma cell']);
  assert.deepEqual([p.reviews[2].previous, p.reviews[2].next, p.reviews[3].next], ['excluded', 'included', 'merged']);
  const st = cellReviewState(reviewsOf(p, 'd1'), 'seg-1', 5);
  assert.deepEqual([...st.excluded], [0, 1, 1, 0, 0]);
  assert.deepEqual([...st.label], [0, 0, 2, 0, 3]);
  assert.deepEqual([...st.concern], [0, 0, 0, 0, 1]);
  assert.deepEqual([...st.comments], [0, 0, 0, 1, 0]);
  assert.equal(st.reviewed, 5);
  assert.deepEqual(stateCounts(st), { accepted: 0, relabeled: 1, ambiguous: 1, excluded: 2, concern: 1, commented: 1, reviewed: 5 });
  // Colours: excluded before all, then a concern, ambiguous, relabeled; none clear.
  const rgba = reviewColors(st);
  const at = (i) => [...rgba.slice(4 * i, 4 * i + 3)];
  const color = (id) => STATUS_COLORS.find((s) => s.id === id).color;
  assert.deepEqual([at(0), at(1), at(2), at(3), at(4)], [[0, 0, 0], color('excluded'), color('excluded'), color('commented'), color('concern')]);
  assert.equal(rgba[3], 0);
  p = addReview(p, { ...cells([4]), action: 'resolve' }, { now: t(9) });
  assert.equal(cellReviewState(reviewsOf(p), 'seg-1', 5).concern[4], 0);
});

test('a region reviewed: relabeled as a version of it, its state', () => {
  let p = project();
  p = addRegion(p, { dataset: 'd1', name: 'Front', cls: 'Tumor', geometry: rectangle(0, 0, 10, 10), tool: 'rectangle', id: 'r-1', now: '2026-01-01T00:00:00Z' });
  p = addReview(p, { action: 'relabel', label: 'Stroma', note: 'Mostly fibroblasts', dataset: 'd1', target: 'region', region: 'r-1', by: 'RM', images: IMAGES }, { now: '2026-01-02T00:00:00Z' });
  const r = p.reviews[0];
  assert.deepEqual([r.previous, r.next, r.region.version, r.region.cls], ['Tumor', 'Stroma', 2, 'Stroma']);
  assert.equal(regionsOf(p, 'd1')[0].cls, 'Stroma');
  assert.equal(regionsOf(p, 'd1')[0].version, 2);
  p = addReview(p, { action: 'exclude', note: 'Folded', dataset: 'd1', target: 'region', region: 'r-1' }, { now: '2026-01-03T00:00:00Z' });
  assert.deepEqual([p.reviews[1].previous, p.reviews[1].next], ['included', 'excluded']);
  assert.deepEqual(regionReviewState(p.reviews).get('r-1'), { status: 'relabeled', excluded: true, concern: null, comments: 0 });
  assert.throws(() => addReview(p, { action: 'accept', dataset: 'd1', target: 'region', region: 'r-9' }), /no such region/);
});

test('curated edits made before the record are read as reviews', () => {
  let p = project();
  p = appendEdit(p, 'cur-1', { kind: 'cells', cells: [2], label: 'T cell', reason: 'An older edit', by: 'XY' }, '2026-01-02T00:00:00Z');
  p = addReview(p, { action: 'comment', note: 'Later', dataset: 'd1', target: 'cells', segmentation: SEG, cells: shown([2]) }, { now: '2026-01-03T00:00:00Z' });
  const all = reviewsOf(p, 'd1');
  assert.deepEqual(all.map((r) => [r.action, r.derived ?? false, r.by]), [['relabel', true, 'XY'], ['comment', false, 'the user']]);
  assert.deepEqual(all[0].edits, [{ set: 'cur-1', index: 0 }]);
  assert.equal(cellReviewState(all, 'seg-1', 5).label[2], 2);
});

test('the record written as CSV and JSON, read back', () => {
  let p = project();
  p = addReview(p, { action: 'relabel', by: 'R, "M"', note: 'Line one\nline two, with a comma', dataset: 'd1', target: 'cells', segmentation: SEG, cells: shown([0, 1]), labels: LABELS, label: 'B cell', images: IMAGES, curated: 'cur-1' }, { now: '2026-01-02T00:00:00Z' });
  p = addRegion(p, { dataset: 'd1', name: 'Front', cls: 'Tumor', geometry: rectangle(0, 0, 10, 10), tool: 'rectangle', id: 'r-1' });
  p = addReview(p, { action: 'concern', concern: 'missed', note: 'Unsegmented nuclei', dataset: 'd1', target: 'region', region: 'r-1' }, { now: '2026-01-03T00:00:00Z' });
  const records = reviewsOf(p, 'd1');
  const rows = parseCSV(reviewsCSV(records, { d1: 'Rep1' }));
  assert.equal(rows.length, 3);
  assert.deepEqual(Object.keys(rows[0]), CSV_COLUMNS);
  assert.deepEqual(rows.map((r) => [r.review, r.reviewer, r.action, r.cell_id, r.cell_row, r.label, r.confidence.slice(0, 3), r.previous, r.new, r.region_name]),
    [[records[0].id, 'R, "M"', 'relabel', 'cell-0', '0', 'T cell', '0.8', 'T cell', 'B cell', ''], [records[0].id, 'R, "M"', 'relabel', 'cell-1', '1', 'T cell', '0.2', 'T cell', 'B cell', ''], [records[1].id, 'the user', 'concern', '', '', '', '', 'none', 'missed', 'Front']]);
  assert.equal(rows[0].note, 'Line one\nline two, with a comma');
  assert.equal(rows[0].images, 'H&E: Automatic, affine (tf-2), shown');
  const doc = JSON.parse(JSON.stringify(reviewsJSON(records, { project: p, datasets: p.datasets, situcraft: '0.5.0', now: 't' })));
  assert.equal(doc.format, 'situcraft-reviews');
  assert.deepEqual(doc.reviews, records);
});
