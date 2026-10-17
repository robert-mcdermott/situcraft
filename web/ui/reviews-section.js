// The Viewer's Reviews (wave 5, slice 6): the reviews of the dataset's cells and regions — how many
// of the cells drawn are accepted, relabeled, ambiguous, excluded, of concern or commented; the cells
// chosen (in the QC view, or a region's) reviewed together; the record, the latest first, each going
// to its cell or region; and the record written as CSV (a row a cell) and JSON. The cells' review
// status is a fill of its own (lib/review-record.js).

import { h, icon, clear, downloadBlob, formatCount } from './dom.js';
import { toast } from './overlays.js';
import { cellReviewState, regionReviewState, reviewsCSV, reviewsJSON, reviewsOf, stateCounts, STATUS_COLORS } from '../lib/review-record.js';
import { cellsState, reviewCells, reviewForm, reviewLine, segmentationFor, labelSetFor } from './reviewing.js';
import { labelSource, refOf } from './label-sources.js';

// options: { app, t ({ dataset, item }), segId() (the segmentation drawn: its artifact id, null for
// the instrument's), cellsDrawn() (its cells table), labelsRef() (the label fill's { id, level } or
// null), shown(imageId), select(row) (selects a cell drawn), region(id) (goes to a region), after(
// curated) (a review made: a curated set begun is shown) }.
export function mountReviews({ app, t, segId, cellsDrawn, labelsRef, shown, select, region, after }) {
  const body = h('div');
  const section = h('section.viewer-section.viewer-reviews-section', h('h3', icon('check'), 'Reviews'), body);
  let all = false;
  let generation = 0;

  async function render() {
    const mine = (generation += 1);
    const project = app.store.project;
    const seg = segmentationFor(project, t.dataset.id, segId());
    const records = reviewsOf(project, t.dataset.id);
    const cells = cellsDrawn();
    clear(body);
    if (!seg || !cells) {
      body.append(h('p.muted.viewer-hint', 'Reviews are of a segmentation\'s cells: draw one.'));
      return;
    }
    const st = cellReviewState(records, seg.id, cells.n);
    const c = stateCounts(st);
    const regions = [...regionReviewState(records).values()];
    const line = STATUS_COLORS.filter((s) => c[s.id]).map((s) => h('span.review-count', h('span.dot', { style: { background: `rgb(${s.color.join(',')})` } }), `${formatCount(c[s.id])} ${s.label.toLowerCase()}`));
    body.append(
      h('p.muted.viewer-hint', `${formatCount(c.reviewed)} of the ${formatCount(cells.n)} cells drawn reviewed${regions.length ? `; ${regions.length} region${regions.length === 1 ? '' : 's'}` : ''}. Fill by Review status to see them.`),
      line.length ? h('div.review-counts', ...line) : null);
    // The cells chosen: the instrument's (the QC view's choices are), reviewed together.
    const chosen = app.selection?.dataset === t.dataset.id ? app.selection : null;
    if (chosen) {
      if (!seg.instrument) body.append(h('p.muted.viewer-hint', `${formatCount(chosen.count)} cells chosen are the instrument's: draw its segmentation to review them.`));
      else {
        const rows = [];
        for (let i = 0; i < chosen.mask.length; i += 1) if (chosen.mask[i]) rows.push(i);
        const set = labelSetFor(project, t.dataset.id, seg.id, labelsRef());
        const src = set ? await labelSource(app, refOf(set.id, set.level)).catch(() => null) : null;
        if (mine !== generation) return;
        const labels = src ? new Set(rows.map((r) => (src.label[r] >= 0 ? src.names[src.label[r]] : null))) : new Set();
        const one = labels.size === 1 ? [...labels][0] : null;
        body.append(h('details.review-chosen', { open: true }, h('summary', `Review the ${formatCount(rows.length)} cells chosen (${chosen.label})`),
          reviewForm({
            names: src?.names ?? [], current: one, canAccept: Boolean(src) && !labels.has(null), state: cellsState(project, t.dataset.id, seg.id, rows, cells.n),
            canLabel: Boolean(set) && set.kind !== 'consensus', labelHint: set ? 'A consensus is made by its rule: fill by one of its sets to review labels.' : 'Fill the cells by a label set to review their labels.',
            submit: async (action, v) => {
              const { curated } = await reviewCells({ app, t, seg, rows, action, labels: set, ...v, shown });
              toast(`Reviewed the ${formatCount(rows.length)} cells chosen: ${action}.`, { kind: 'ok' });
              after(curated && set.kind !== 'curated' ? curated : null);
            },
          })));
      }
    }
    // The record.
    const shownRecords = [...records].reverse().slice(0, all ? records.length : 12);
    body.append(
      records.length ? h('ol.review-log', ...shownRecords.map((r) => h('li', h('button.link', { type: 'button', title: 'Go to it', onclick: () => go(r) }, where(r, seg)), h('span', ` ${reviewLine(r)}`)))) : h('p.muted.viewer-hint', 'No review yet: choose a cell, the cells chosen or a region, and review it.'),
      records.length > 12 ? h('button.btn.small.ghost', { type: 'button', onclick: () => { all = !all; render(); } }, all ? 'Show the latest' : `Show all ${records.length}`) : null,
      h('div.btn-row',
        h('button.btn.small', { type: 'button', disabled: !records.length, title: 'A row for each cell (or region) a review is of', onclick: () => write('csv') }, icon('download'), 'Write the record (CSV)'),
        h('button.btn.small', { type: 'button', disabled: !records.length, title: 'The record as the project keeps it', onclick: () => write('json') }, icon('download'), 'JSON')));
  }

  function where(r, seg) {
    if (r.target === 'region') return r.region.name;
    if (r.target === 'clusters') return `${r.clusters.clusters.length === 1 ? `cluster ${r.clusters.clusters[0] + 1}` : `${r.clusters.clusters.length} clusters`}`;
    const n = r.cells.length;
    const first = r.cells[0];
    return `${n === 1 ? `cell ${first.id ?? `#${first.row}`}` : `${formatCount(n)} cells`}${r.segmentation.id === seg.id ? '' : ` of ${r.segmentation.name}`}`;
  }

  function go(r) {
    if (r.target === 'region') region(r.region.id);
    else if (r.target === 'cells') {
      if (r.segmentation.id !== segmentationFor(app.store.project, t.dataset.id, segId())?.id) toast(`Draw ${r.segmentation.name} to go to its cells.`, { kind: 'error' });
      else select(r.cells[0].row);
    }
  }

  function write(format) {
    const records = reviewsOf(app.store.project, t.dataset.id);
    const name = `${t.dataset.name}-reviews.${format}`;
    const text = format === 'csv' ? reviewsCSV(records, { [t.dataset.id]: t.dataset.name }) : `${JSON.stringify(reviewsJSON(records, { project: app.store.project, datasets: [t.dataset], situcraft: app.version }), null, 1)}\n`;
    downloadBlob(new Blob([text], { type: format === 'csv' ? 'text/csv' : 'application/json' }), name);
    toast(`Wrote ${records.length} review${records.length === 1 ? '' : 's'} of ${t.dataset.name}.`, { kind: 'ok' });
  }

  render();
  return { section, refresh: render };
}
