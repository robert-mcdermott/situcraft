// A guided tour or worked example (lib/tours.js): a card at the foot of the window stepping through
// views of a dataset. Each step opens its view through app.applyView, as a saved view opens,
// without keeping it in the project. A worked example's steps may also do something first (expand
// the nuclei, plant errors, compare two segmentations, cluster and label cells, make them again on
// another segmentation: the card's button runs it as a job and goes on when it is done), show a
// comparison in the Compare view (its labels read through it in the Labels tab), show a clustering
// in the Cell types view, or reveal a planted segmentation's answer key. One tour at a time.

import { h, icon } from './dom.js';
import { segmentationByRole, stepView } from '../lib/tours.js';
import { comparisonsOf, segmentationsOf } from '../lib/segmentations.js';
import { clusteringsOf } from '../lib/clusterings.js';
import { labelSetsOf } from '../lib/labels.js';
import { remakeOnSegmentation } from './remake.js';
import { toast } from './overlays.js';

let active = null;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// The Cell types view's sections a step may bring into view.
const SECTIONS = { map: '.ct-map-host', selection: '.ct-selection', clusters: '.ct-detail', review: '.rv-section' };

export function startTour(app, { tour, dataset }) {
  active?.close();
  let index = 0;
  let working = false;
  const count = h('span.tour-count');
  const title = h('h3');
  const text = h('p');
  const note = h('p.tour-note', { hidden: true });
  const back = h('button.btn.small', { type: 'button', onclick: () => go(index - 1) }, 'Back');
  const next = h('button.btn.small.primary', { type: 'button', onclick: () => advance() });
  const intro = tour.question
    ? h('div.tour-intro', h('p', h('b', 'The question: '), tour.question), h('p.muted', h('b', 'What to expect: '), tour.expect), tour.simulated ? h('p.muted', 'Its errors are simulated.') : null)
    : null;
  const card = h('section.tour-card', { role: 'dialog', 'aria-label': tour.title, 'aria-live': 'polite' },
    h('div.tour-head', icon(tour.question ? 'flask' : 'info'), h('b', tour.title), count, h('button.icon-button.small', { type: 'button', title: 'End the tour', 'aria-label': 'End the tour', onclick: () => close() }, icon('close'))),
    intro, title, text, note,
    h('div.btn-row.tour-actions', back, next));
  document.body.append(card);

  const datasetRecord = () => app.store.project.datasets.find((d) => d.id === dataset);
  const segmentations = () => segmentationsOf(app.store.project, dataset);
  const role = (r) => segmentationByRole(segmentations(), r);
  const comparison = (a, b) => {
    const sa = role(a);
    const sb = role(b);
    return sa && sb ? comparisonsOf(app.store.project, dataset).find((c) => c.a === sa.id && c.b === sb.id && c.state === 'complete') ?? null : null;
  };
  // A segmentation's clustering (its latest of SituCraft's), and that clustering's labels (its
  // latest from markers); the truth's types, imported.
  const clusteringOf = (r) => {
    const s = role(r);
    return s ? clusteringsOf(app.store.project, dataset, s.id).filter((c) => c.state === 'complete' && !c.summary?.source).at(-1) ?? null : null;
  };
  const labelsOf = (r) => {
    const c = clusteringOf(r);
    return c ? labelSetsOf(app.store.project, dataset, c.id).filter((l) => l.state === 'complete' && !l.source && !l.reference).at(-1) ?? null : null;
  };
  const trueTypes = () => {
    const s = role('truth');
    return s ? app.store.project.artifacts.find((a) => a.kind === 'labels' && a.state === 'complete' && a.source && a.segmentation === s.id) ?? null : null;
  };
  const itemOf = (ds) => app.bundles.link(ds.id)?.item ?? app.bundles.itemByPath(ds.path);
  // The truth folder beside the bundle's files, in the path's own separators.
  const truthFolder = (ds) => {
    const sep = ds.path.includes('\\') && !ds.path.includes('/') ? '\\' : '/';
    return { folder: `${ds.path.replace(/[\\/]+$/, '')}${sep}truth`, sep };
  };

  // Whether a step's action is done already.
  function done(step) {
    const d = step.do;
    if (!d) return true;
    if (d.action === 'expand') return Boolean(role(`expansion-${d.distance}`));
    if (d.action === 'plant') return Boolean(role('planted'));
    if (d.action === 'compare') return Boolean(comparison(d.a, d.b));
    if (d.action === 'refine') return Boolean(role('refined'));
    if (d.action === 'import-truth') return Boolean(role('truth'));
    if (d.action === 'cluster') return Boolean(clusteringOf(d.segmentation ?? 'instrument'));
    if (d.action === 'label') return Boolean(labelsOf(d.segmentation ?? 'instrument'));
    if (d.action === 'import-truth-labels') return Boolean(trueTypes());
    if (d.action === 'remake') return Boolean(labelsOf(d.to));
    return true;
  }

  // Runs a step's action in its view (the Segmentation or the Cell types view), which shows its
  // progress.
  async function run(step) {
    const d = step.do;
    const ds = datasetRecord();
    if (step.mode === 'celltypes') {
      for (let k = 0; k < 200 && !app.cellTypesView; k += 1) await sleep(25);
      if (!app.cellTypesView) throw new Error('the Cell types view did not open');
      const before = 'do the steps before this one first';
      if (d.action === 'cluster') {
        const s = role(d.segmentation ?? 'instrument');
        if (!s) throw new Error(`${before}: the segmentation is not made yet`);
        return app.cellTypesView.cluster(ds, { segmentation: s.id, parameters: {} });
      }
      if (d.action === 'label') {
        const c = clusteringOf(d.segmentation ?? 'instrument');
        if (!c) throw new Error(`${before}: the cells are not clustered yet`);
        return app.cellTypesView.label(ds, c.id, { markers: d.markers });
      }
      if (d.action === 'import-truth-labels') {
        const s = role('truth');
        if (!s) throw new Error(`${before}: the truth is not imported yet`);
        const { folder, sep } = truthFolder(ds);
        return app.bundles.importLabels(itemOf(ds), ds.id, { segmentation: s.id, path: `${folder}${sep}cells.parquet`, columns: { idColumn: 'cell_id', column: 'cell_type', confidenceColumn: '', colorColumn: '' } });
      }
      if (d.action === 'remake') {
        const c = clusteringOf(d.from);
        const s = role(d.to);
        if (!c || !s || !labelsOf(d.from)) throw new Error(`${before}: the clustering and its labels, and the other segmentation, are not made yet`);
        const out = await remakeOnSegmentation(app, { item: itemOf(ds), dataset: ds }, c.id, s.id);
        if (out.failed.length) throw new Error(out.failed.map((f) => f.error).join('; '));
        return out;
      }
      return null;
    }
    for (let k = 0; k < 200 && !app.segmentationView; k += 1) await sleep(25);
    if (!app.segmentationView) throw new Error('the Segmentation view did not open');
    if (d.action === 'expand') return app.segmentationView.expand(ds, d.distance);
    if (d.action === 'plant') return app.segmentationView.plant(ds, {});
    if (d.action === 'refine') return app.segmentationView.refine(ds, { preset: d.preset ?? 'balanced' });
    if (d.action === 'import-truth') return app.segmentationView.importSegmentation(ds, { format: 'xenium', paths: [truthFolder(ds).folder] });
    if (d.action === 'compare') {
      const a = role(d.a);
      const b = role(d.b);
      if (!a || !b) throw new Error('do the steps before this one first: one of the two segmentations is not made yet');
      return app.segmentationView.compare(ds, a.id, b.id);
    }
    return null;
  }

  async function advance() {
    if (working) return;
    const step = tour.steps[index];
    if (!done(step)) {
      working = true;
      next.disabled = true;
      back.disabled = true;
      note.hidden = false;
      note.textContent = 'Working… the job\'s progress is shown in the view.';
      try {
        await run(step);
      } catch (error) {
        toast(`${step.title}: ${error.message}`, { kind: 'error', timeout: 10000 });
        working = false;
        go(index);
        return;
      }
      working = false;
    }
    if (index + 1 < tour.steps.length) go(index + 1);
    else close();
  }

  // Shows a step's comparison in the Compare view as the step says.
  async function showComparison(step) {
    const c = step.compare;
    const cmp = comparison(c.a, c.b);
    if (!cmp) {
      note.hidden = false;
      note.textContent = 'The comparison is not made yet: go back to the steps that make it.';
      return null;
    }
    app.compareFocus = cmp.id;
    app.setMode('compare');
    for (let k = 0; k < 200 && !app.compareView; k += 1) await sleep(25);
    const w = await app.compareView?.open(cmp.id);
    if (!w || active !== controller || tour.steps[index] !== step) return w;
    if (c.view) w.setMode(c.view);
    w.setTab('queue');
    const queue = w.setRules({ classes: c.classes ?? [...w.session.classes], by: c.rank, map: c.map });
    if (c.difference && queue.length >= c.difference) w.select(queue[c.difference - 1].index);
    // The Labels tab: each side's labels (the default pair) read through the comparison, the
    // tissue colored by them.
    if (c.tab === 'labels') await w.showLabels({ fill: true });
    else if (c.tab && c.tab !== 'queue') w.setTab(c.tab);
    return w;
  }

  // Shows a step's clustering in the Cell types view: its map and the tissue colored, clusters
  // selected, a section brought into view, the review queue made.
  async function showCellTypes(step) {
    const c = step.celltypes;
    const r = c.segmentation ?? 'instrument';
    const clustering = clusteringOf(r);
    if (!clustering) {
      note.hidden = false;
      note.textContent = 'The cells are not clustered yet: go back to the step that clusters them.';
      return null;
    }
    for (let k = 0; k < 200 && !app.cellTypesView; k += 1) await sleep(25);
    const map = await app.cellTypesView?.show(datasetRecord(), clustering.id);
    if (!map || active !== controller || tour.steps[index] !== step) return map;
    const labels = labelsOf(r);
    const [kind, level] = String(c.color ?? 'clusters').split(':');
    if (kind === 'labels' && labels) await map.setColor({ kind: 'labels', labels: labels.id, level: Number(level ?? 0) });
    else if (kind === 'confidence' && labels) await map.setColor({ kind: 'confidence', labels: labels.id });
    else if (kind === 'clusters' || kind === 'labels' || kind === 'confidence') await map.setColor({ kind: 'clusters', clustering: clustering.id });
    else await map.setColor({ kind: 'gene', gene: c.color });
    if (c.select?.length) map.selectClusters(c.select.map((k) => k - 1));
    else map.select(null);
    if (c.review) await app.labelReview?.makeQueue?.(labels?.id ?? null, 0);
    // The map in view unless the step names another section.
    const section = c.section ?? 'map';
    const selector = section === 'labels' ? (labels ? `[data-labels="${CSS.escape(labels.id)}"]` : null) : SECTIONS[section];
    if (selector) document.querySelector(selector)?.scrollIntoView({ block: 'start' });
    return map;
  }

  function go(i) {
    if (i < 0 || i >= tour.steps.length) return;
    index = i;
    const step = tour.steps[i];
    count.textContent = `${i + 1} of ${tour.steps.length}`;
    title.textContent = step.title;
    text.textContent = step.text;
    note.hidden = true;
    if (intro) intro.hidden = i !== 0;
    back.disabled = i === 0;
    next.disabled = false;
    const last = i + 1 === tour.steps.length;
    next.textContent = !done(step) ? step.label : last ? 'Finish' : 'Next';
    if (step.do && done(step)) {
      note.hidden = false;
      note.textContent = 'Done already: it is kept in the project.';
    }
    if (!datasetRecord()) {
      close();
      return;
    }
    card.classList.toggle('aside', Boolean(step.compare || step.mode === 'celltypes'));
    if (step.compare) {
      app.focusItem({ kind: 'dataset', id: dataset });
      controller.shown = showComparison(step).catch((error) => {
        note.hidden = false;
        note.textContent = `The comparison could not be shown: ${error.message}`;
      });
    } else if (step.celltypes) {
      app.focusItem({ kind: 'dataset', id: dataset });
      app.setMode('celltypes');
      controller.shown = showCellTypes(step).catch((error) => {
        note.hidden = false;
        note.textContent = `The clustering could not be shown: ${error.message}`;
      });
    } else {
      const view = stepView(step, dataset, i);
      // A step's segmentation, by what it is.
      if (view && step.segmentation) {
        const s = role(step.segmentation);
        view.segmentation = s && s.source !== 'instrument' ? s.id : 'instrument';
      }
      // Cells filled by the segmentation's clustering or its labels (at a level).
      const fill = view?.cells.fill ?? '';
      if (fill === 'clusters' || /^labels(:\d+)?$/.test(fill)) {
        const r = step.segmentation ?? 'instrument';
        const c = clusteringOf(r);
        const l = labelsOf(r);
        view.cells.fill = fill === 'clusters' ? (c ? `clusters:${c.id}` : null) : (l ? `labels:${l.id}:${Number(fill.split(':')[1] ?? 0)}` : null);
      }
      if (view) app.applyView(view);
      else {
        app.focusItem({ kind: 'dataset', id: dataset });
        app.setMode(step.mode);
      }
      controller.shown = step.reveal ? (async () => {
        for (let k = 0; k < 200 && !app.segmentationView; k += 1) await sleep(25);
        app.segmentationView?.revealAnswers?.(dataset);
      })() : Promise.resolve();
    }
    next.focus();
  }

  function close() {
    card.remove();
    if (active === controller) active = null;
  }

  const controller = { close, go, advance, step: () => index, card, tour, dataset, open: () => card.isConnected, done: () => done(tour.steps[index]), shown: Promise.resolve() };
  active = controller;
  app.tour = controller;
  go(0);
  return controller;
}
