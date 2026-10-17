// Reviewing cells and regions (wave 5, slice 6; lib/review-record.js): what a review records,
// gathered where it is made — the cells' IDs (the program answers them), each cell's label and its
// confidence in the label set shown, the curated set accepting and relabeling edit (begun from the
// set shown when there is none), the post-Xenium images' alignments in use and whether each was
// shown — and the form every place a review is made shows: the Viewer's cell and the cells chosen,
// a region, the Cell types view's review queue.

import { h, icon, clear } from './dom.js';
import { toast } from './overlays.js';
import { prefs } from './storage.js';
import { ACTIONS, CONCERNS, addReview, cellReviewState, regionReviewState, reviewsOf } from '../lib/review-record.js';
import { createCurated, curatedSetsOf } from '../lib/curation.js';
import { placedImages } from '../lib/images.js';
import { segmentationChoices } from '../lib/segview.js';
import { labelSource, labelVersions, refOf } from './label-sources.js';

export const reviewer = () => prefs.get('reviewer', '') || '';
export const setReviewer = (name) => prefs.set('reviewer', String(name ?? '').trim());

async function postJSON(url, body) {
  const response = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const out = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(out.error ?? `HTTP ${response.status}`);
  return out;
}

// A dataset's segmentation as a review names it ({ id, name, instrument }): by its artifact's id,
// null for the instrument's (the dataset's own cells).
export function segmentationFor(project, datasetId, id) {
  const choices = segmentationChoices(project, datasetId);
  const c = id ? choices.find((x) => x.id === id) : choices.find((x) => x.instrument);
  return c ? { id: c.id, name: c.name, instrument: c.instrument } : null;
}

// The IDs of a segmentation's rows (the program's lookup), or the rows of IDs.
const lookupUrl = (item, seg) => (seg.instrument ? `${item.url}/cells/lookup` : `${item.url}/segmentations/${encodeURIComponent(seg.id)}/cells/lookup`);
export async function cellIds(item, seg, rows) {
  return (await postJSON(lookupUrl(item, seg), { rows: [...rows] })).ids;
}
export async function cellRows(item, seg, ids) {
  return (await postJSON(lookupUrl(item, seg), { ids: [...ids] })).rows;
}

// The dataset's post-Xenium images and the version of its alignment placing each, and whether
// each was shown (shown(imageId): true, false, or null when not known).
export function imagesInUse(project, datasetId, shown = () => null) {
  return placedImages(project, datasetId).map(({ record, transform }) => ({ image: record.id, name: record.name, transform: transform?.id ?? null, version: transform?.name ?? null, shown: shown(record.id) }));
}

// A label set by its fill's reference ({ id, level }) as a review names it, with its kind.
export function labelSetFor(project, datasetId, segId, ref) {
  if (!ref) return null;
  const v = labelVersions(project, datasetId, segId).find((x) => x.id === ref.id && x.level === (ref.level ?? 0));
  return v ? { id: v.id, level: v.level, name: v.name, kind: v.kind } : null;
}

// The curated set a review of a label set edits: the set itself when curated, else the one begun
// from it (begun now when there is none). Answers { project, id }.
export function curatedFor(project, { dataset, segmentation, labels, by }) {
  if (labels.kind === 'curated') return { project, id: labels.id };
  const found = curatedSetsOf(project, dataset, segmentation).find((c) => c.base.labels === labels.id && c.base.level === labels.level);
  if (found) return { project, id: found.id };
  const next = createCurated(project, { dataset, segmentation, base: { labels: labels.id, level: labels.level, name: labels.name }, name: `${labels.name}, reviewed`, by });
  return { project: next, id: curatedSetsOf(next, dataset, segmentation).at(-1).id };
}

// Reviews cells of a segmentation: { app, t ({ dataset, item }), seg ({ id, name, instrument }),
// rows, action, labels (a label set's reference, { id, level }, or null), label, alternatives,
// concern, note, by, shown (imageId → shown) }. Commits the review (and any curated set it begins)
// and answers it, with the curated set's id when it edited one.
export async function reviewCells({ app, t, seg, rows, action, labels = null, label, alternatives, concern, note, by = reviewer(), shown }) {
  const project = app.store.project;
  const set = labelSetFor(project, t.dataset.id, seg.id, labels);
  const [ids, src] = await Promise.all([cellIds(t.item, seg, rows), set ? labelSource(app, refOf(set.id, set.level)) : null]);
  if (src && src.segCells !== undefined && rows.some((r) => r >= src.segCells)) throw new Error('The label set is of another segmentation.');
  const cells = rows.map((row, k) => ({ row, id: ids[k] || null, label: src && src.label[row] >= 0 ? src.names[src.label[row]] : null, confidence: src?.confidence ? src.confidence[row] : null }));
  let next = app.store.project;
  let curated = null;
  if (set && (action === 'accept' || action === 'relabel')) {
    if (set.kind === 'consensus') throw new Error('A consensus is made by its rule: review one of its sets, or a curated set begun from it.');
    ({ project: next, id: curated } = curatedFor(next, { dataset: t.dataset.id, segmentation: seg.id, labels: set, by: by || 'the user' }));
  }
  next = addReview(next, { action, by, note, dataset: t.dataset.id, target: 'cells', segmentation: { id: seg.id, name: seg.name }, cells, labels: set, label, alternatives, concern, images: imagesInUse(next, t.dataset.id, shown), curated });
  const record = next.reviews.at(-1);
  app.store.commit(next, `Review ${rows.length === 1 ? `cell ${cells[0].id ?? `#${rows[0]}`}` : `${rows.length} cells`}`);
  return { record, curated };
}

// Reviews a region: { app, t, regionId, action, label (relabel: its new class), alternatives,
// concern, note, by, seg (the segmentation drawn, for the cells counted), cells (rows in it), shown }.
export async function reviewRegion({ app, t, regionId, action, label, alternatives, concern, note, by = reviewer(), seg = null, rows = null, shown }) {
  let cells = null;
  if (seg && rows?.length) {
    const ids = await cellIds(t.item, seg, rows);
    cells = rows.map((row, k) => ({ row, id: ids[k] || null }));
  }
  const next = addReview(app.store.project, { action, by, note, dataset: t.dataset.id, target: 'region', region: regionId, segmentation: seg ? { id: seg.id, name: seg.name } : null, cells, label, alternatives, concern, images: imagesInUse(app.store.project, t.dataset.id, shown) });
  const record = next.reviews.at(-1);
  app.store.commit(next, `Review ${record.region.name}`);
  return { record };
}

// The reviews of a segmentation's cell (by row) or of a region, the latest first.
export function reviewsOfCell(project, datasetId, segId, row) {
  return reviewsOf(project, datasetId).filter((r) => r.segmentation?.id === segId && r.target === 'cells' && r.cells.some((c) => c.row === row)).reverse();
}
export function reviewsOfRegion(project, datasetId, regionId) {
  return reviewsOf(project, datasetId).filter((r) => r.target === 'region' && r.region.id === regionId).reverse();
}

// A review in a line: its action, its change, why, who and when.
export function reviewLine(r) {
  const a = ACTIONS.find((x) => x.id === r.action)?.label ?? r.action;
  const concern = r.concern ? `: ${CONCERNS.find((c) => c.id === r.concern)?.label.toLowerCase() ?? r.concern}` : '';
  const change = r.action === 'relabel' ? ` ${r.previous ?? '?'} → ${r.next}` : r.action === 'accept' && r.next ? ` "${r.next}"` : r.action === 'ambiguous' && r.alternatives?.length ? ` (${r.alternatives.join(' or ')})` : '';
  return `${a}${concern}${change}${r.note ? `: ${r.note}` : ''} — ${r.by}, ${new Date(r.time).toLocaleString()}${r.derived ? ' (a label edit)' : ''}`;
}

// The review form: { kind ('cells' or 'region'), names (the labels or classes to relabel to),
// current (the label shown, or null when there is none or several), canAccept, state ({ excluded
// (all are), someExcluded, concern (all have it), someConcern }), canLabel (labels can be accepted or
// relabeled; else labelHint says why not), excludeAlso (a region's: what else excluding it may
// exclude, offered checked, its answer as `also`), submit(action, { label, alternatives, concern,
// note, also, by }) (a promise) }. Accept, include and resolve act at once; the others ask what and
// why first.
export function reviewForm({ kind = 'cells', names = [], current = null, canAccept = current !== null, state = {}, canLabel = true, labelHint = '', excludeAlso = null, submit }) {
  const host = h('div.review-form');
  const ask = h('div.review-ask');
  const by = h('input.input.small', { type: 'text', value: reviewer(), placeholder: 'Your name or initials', 'aria-label': 'Reviewer', onchange: (e) => setReviewer(e.target.value) });
  const run = async (action, values = {}) => {
    if (!by.value.trim()) {
      toast('Say who reviews (your name or initials): every review keeps it.', { kind: 'error' });
      by.focus();
      return;
    }
    setReviewer(by.value);
    try {
      busy(true);
      await submit(action, { ...values, by: by.value.trim() });
      clear(ask);
    } catch (error) {
      toast(error.message, { kind: 'error' });
    } finally {
      busy(false);
    }
  };
  const buttons = [];
  const button = (action, label, glyph, onclick, disabled = false, title = ACTIONS.find((a) => a.id === action)?.help ?? '') => {
    const b = h('button.btn.small', { type: 'button', title, disabled, 'data-review': action, onclick }, icon(glyph), label);
    buttons.push(b);
    return b;
  };
  const busy = (on) => buttons.forEach((b) => { b.disabled = on || b.dataset.off === '1'; });
  const note = (placeholder) => h('input.input', { type: 'text', placeholder, 'aria-label': 'Why', 'data-review-note': '' });
  const form = (title, fields, go, label) => {
    clear(ask).append(h('div.review-ask-box', h('div.muted', title), ...fields, h('div.btn-row', h('button.btn.small.primary', { type: 'button', 'data-review-go': '', onclick: go }, label), h('button.btn.small.ghost', { type: 'button', onclick: () => clear(ask) }, 'Cancel'))));
    ask.querySelector('input, select')?.focus();
  };
  const what = kind === 'region' ? 'class' : 'label';
  const labelPick = () => {
    const sel = h('select.input', { 'aria-label': `The new ${what}`, 'data-review-label': '' }, ...names.filter((n) => n !== current).map((n) => h('option', { value: n }, n)), h('option', { value: '' }, `A new ${what}…`));
    const fresh = h('input.input', { type: 'text', placeholder: `The new ${what}`, hidden: names.length > 0, 'aria-label': `A new ${what}`, 'data-review-new': '' });
    sel.addEventListener('change', () => { fresh.hidden = sel.value !== ''; });
    if (!names.filter((n) => n !== current).length) sel.value = '';
    return [sel, fresh, () => sel.value || fresh.value.trim()];
  };
  const labelOff = !canLabel;
  const accept = button('accept', current ? `Accept "${current}"` : 'Accept their labels', 'check', () => run('accept', {}), labelOff || !canAccept, labelOff ? labelHint : undefined);
  const relabel = button('relabel', 'Relabel…', 'edit', () => {
    const [sel, fresh, value] = labelPick();
    const why = note('Why (kept with the review)');
    form(`Relabel ${current ? `from "${current}"` : ''}`, [sel, fresh, why], () => run('relabel', { label: value(), note: why.value }), 'Relabel');
  }, labelOff, labelOff ? labelHint : undefined);
  const ambiguous = button('ambiguous', 'Ambiguous…', 'help', () => {
    const boxes = names.map((n) => h('label.review-chip', h('input', { type: 'checkbox', value: n }), n));
    const why = note('What makes it ambiguous (optional)');
    form('It could be (optional):', [h('div.review-chips', ...boxes), why], () => run('ambiguous', { alternatives: boxes.map((b) => b.querySelector('input')).filter((x) => x.checked).map((x) => x.value), note: why.value }), 'Mark ambiguous');
  });
  const exclude = state.excluded ? null : button('exclude', 'Exclude…', 'close', () => {
    const why = note('Why it is left out (a fold, debris, outside the tissue…)');
    const also = excludeAlso ? h('input', { type: 'checkbox', checked: true, 'data-review-also': '' }) : null;
    form('Exclude from analyses', [why, also ? h('label.viewer-toggle', also, excludeAlso) : null], () => run('exclude', { note: why.value, also: Boolean(also?.checked) }), 'Exclude');
  });
  const include = state.excluded || state.someExcluded ? button('include', 'Include again', 'undo', () => run('include', {})) : null;
  const concern = state.concern ? null : button('concern', 'Segmentation…', 'warning', () => {
    const sel = h('select.input', { 'aria-label': 'The concern', 'data-review-concern': '' }, ...CONCERNS.map((c) => h('option', { value: c.id, title: c.help }, c.label)));
    const why = note('What you see (needed for Other)');
    form('A segmentation concern', [sel, why], () => run('concern', { concern: sel.value, note: why.value }), 'Record the concern');
  });
  const resolve = state.concern || state.someConcern ? button('resolve', 'Resolve the concern', 'check', () => run('resolve', {}), false, state.concern ? `The concern: ${CONCERNS.find((c) => c.id === state.concern)?.label ?? state.concern}` : 'Resolve the cells\' concerns') : null;
  const comment = button('comment', 'Comment…', 'edit', () => {
    const text = note('The comment');
    form('A comment', [text], () => run('comment', { note: text.value }), 'Comment');
  });
  for (const b of buttons) if (b.disabled) b.dataset.off = '1';
  host.append(h('div.btn-row.review-actions', accept, relabel, ambiguous, exclude, include, concern, resolve, comment), ask, h('label.review-by', h('span.muted', 'Reviewer'), by));
  return host;
}

// The review state of cells (rows) of a segmentation of n cells, for the form: whether all or some
// are excluded, their concern when they share one, whether some have one.
export function cellsState(project, datasetId, segId, rows, n) {
  const st = cellReviewState(reviewsOf(project, datasetId).filter((r) => r.target === 'cells'), segId, n);
  const ex = rows.filter((r) => st.excluded[r]).length;
  const concerns = new Set(rows.map((r) => st.concern[r]));
  const one = concerns.size === 1 ? [...concerns][0] : 0;
  return { excluded: rows.length > 0 && ex === rows.length, someExcluded: ex > 0, concern: one ? CONCERNS[one - 1].id : null, someConcern: rows.some((r) => st.concern[r]) };
}
export function regionState(project, datasetId, regionId) {
  return regionReviewState(reviewsOf(project, datasetId)).get(regionId) ?? { status: '', excluded: false, concern: null, comments: 0 };
}
