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
import { downloadBlob } from './dom.js';
import { imagesOf } from '../lib/images.js';
import { checksOf, transformsOf } from '../lib/transforms.js';
import { addRegion, polygonOf, rectangle, regionsOf } from '../lib/regions.js';
import { landmarksFromTruth } from '../lib/simulated-truth.js';
import { reviewCells, reviewRegion, segmentationFor } from './reviewing.js';
import { labelSource, refOf } from './label-sources.js';

let active = null;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// The actions of the post-Xenium worked examples (runPostXenium).
const POST_XENIUM_ACTIONS = new Set(['add-image', 'landmarks', 'register', 'check', 'draw-regions', 'write-regions', 'read-regions', 'import-instrument-types', 'review-queue', 'exclude-fold']);

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

  // The simulated tissue's post-Xenium images (wave 5): the H&E (or the immunofluorescence image)
  // added to the dataset, a version of its alignment by how it was made (matrix, landmarks,
  // automatic), the true types of the instrument's cells, imported as labels, and the reviews the
  // worked example made.
  const IMAGE_FILES = { he: 'simulated_he_image', if: 'simulated_if_image' };
  const imageOf = (key) => imagesOf(app.store.project, dataset).find((r) => r.name.includes(IMAGE_FILES[key])) ?? null;
  const versionOf = (key, method) => {
    const img = imageOf(key);
    return img ? transformsOf(app.store.project, img.id).filter((t) => t.summary?.method === method).at(-1) ?? null : null;
  };
  const instrumentTypes = () => {
    const s = role('instrument');
    return s ? app.store.project.artifacts.find((a) => a.kind === 'labels' && a.state === 'complete' && a.source && a.segmentation === s.id && /instrument_cell_types/.test(a.name ?? '')) ?? null : null;
  };
  const BY = 'the worked example';
  const reviewedByExample = () => (app.store.project.reviews ?? []).filter((r) => r.dataset === dataset && r.by === BY);
  // The Viewer's hook, once it shows the dataset.
  const viewer = async () => {
    for (let k = 0; k < 400 && !(window.situcraftViewer?.dataset === dataset && window.situcraftViewer.regions); k += 1) await sleep(25);
    if (window.situcraftViewer?.dataset !== dataset) throw new Error('the Viewer did not open');
    return window.situcraftViewer;
  };

  // Whether a step's action is done already.
  function done(step) {
    const d = step.do;
    if (!d) return true;
    if (d.action === 'add-image') return Boolean(imageOf(d.image) && (d.alignment !== 'matrix' || versionOf(d.image, 'matrix')));
    if (d.action === 'landmarks') return Boolean(versionOf(d.image, 'landmarks'));
    if (d.action === 'register') return Boolean(versionOf(d.image, 'automatic'));
    if (d.action === 'check') return d.versions.every((m) => versionOf(d.image, m) && checksOf(app.store.project, versionOf(d.image, m).id).length);
    if (d.action === 'draw-regions') return d.regions.every((r) => regionsOf(app.store.project, dataset).some((x) => x.name === r.name));
    if (d.action === 'write-regions' || d.action === 'read-regions') return false;
    if (d.action === 'import-instrument-types') return Boolean(instrumentTypes());
    if (d.action === 'review-queue') return reviewedByExample().filter((r) => r.target === 'cells' && (r.action === 'accept' || r.action === 'relabel')).length >= d.first;
    if (d.action === 'exclude-fold') return reviewedByExample().some((r) => r.target === 'region' && r.region.name === d.region.name && r.action === 'exclude');
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
    if (POST_XENIUM_ACTIONS.has(d.action)) return runPostXenium(d, ds);
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

  // The post-Xenium images' actions: adding an image (with the matrix beside it, as 10x supplies
  // one), landmarks where the truth places points of it, automatic alignment, checks, regions drawn,
  // written in its pixels and read back, the instrument's cells' true types imported, the review
  // queue decided by them, a fold excluded.
  async function runPostXenium(d, ds) {
    const item = itemOf(ds);
    const { folder, sep } = truthFolder(ds);
    const base = ds.path.replace(/[\\/]+$/, '');
    const before = 'do the steps before this one first';
    if (d.action === 'add-image') {
      if (imageOf(d.image)) return null;
      const name = IMAGE_FILES[d.image];
      return app.bundles.addImage(item, ds.id, { path: `${base}${sep}post_xenium${sep}${name}.ome.tif`, alignment: d.alignment === 'matrix' ? `${folder}${sep}post_xenium${sep}${name}alignment.csv` : null });
    }
    const img = imageOf(d.image ?? 'he');
    if (['landmarks', 'register', 'check', 'write-regions', 'read-regions'].includes(d.action) && !img) throw new Error(`${before}: the image is not added yet`);
    if (d.action === 'landmarks') {
      const response = await fetch(`/api/read-json?path=${encodeURIComponent(`${folder}${sep}post_xenium${sep}registration.json`)}`, { cache: 'no-store' });
      if (!response.ok) throw new Error('the simulation\'s truth (truth/post_xenium/registration.json) could not be read');
      const truth = (await response.json()).images.find((x) => x.kind === d.image);
      const landmarks = landmarksFromTruth(truth, d.within, d.count);
      return app.bundles.makeTransform(ds.id, img.id, { landmarks, model: d.model, by: BY, note: `${landmarks.length} landmarks where the simulation's truth places ${landmarks.length} points of the image` }, { use: false });
    }
    if (d.action === 'register') {
      return app.bundles.registerImage(item, ds.id, img.id, { model: d.model, nonrigid: Boolean(d.nonrigid), by: BY, note: 'Aligned automatically by the nuclei' }, { use: true, rationale: 'Aligned automatically by the nuclei, with a smooth deformation where the section stretched.', by: BY });
    }
    if (d.action === 'check') {
      for (const m of d.versions) {
        const v = versionOf(d.image, m);
        if (!v) throw new Error(`${before}: there is no version by ${m} yet`);
        if (!checksOf(app.store.project, v.id).length) await app.bundles.checkAlignment(item, ds.id, img.id, v.id);
      }
      return null;
    }
    if (d.action === 'draw-regions') {
      let next = app.store.project;
      for (const r of d.regions) {
        if (regionsOf(next, ds.id).some((x) => x.name === r.name)) continue;
        const geometry = r.rectangle ? rectangle(...r.rectangle) : polygonOf(r.polygon);
        next = addRegion(next, { dataset: ds.id, name: r.name, cls: r.cls, geometry, tool: r.rectangle ? 'rectangle' : 'polygon', layer: 'the H&E', by: BY, note: 'Drawn by the worked example' });
      }
      app.store.commit(next, 'Draw the worked example\'s regions');
      return null;
    }
    if (d.action === 'write-regions' || d.action === 'read-regions') {
      const v = await viewer();
      const doc = await v.regions.exportDocument(img.id, regionsOf(app.store.project, ds.id).filter((r) => r.by === BY && !/\(\d+\)$/.test(r.name)).map((r) => r.id));
      if (d.action === 'write-regions') {
        downloadBlob(new Blob([`${JSON.stringify(doc, null, 1)}\n`], { type: 'application/geo+json' }), `${ds.name}-regions-he.geojson`);
        lastWritten = doc;
        return null;
      }
      return v.regions.importRegions(lastWritten ?? doc, img.id, 'the file written for QuPath', BY);
    }
    if (d.action === 'import-instrument-types') {
      if (instrumentTypes()) return null;
      const s = role('instrument');
      return app.bundles.importLabels(item, ds.id, { segmentation: s.id, path: `${folder}${sep}instrument_cell_types.csv`, columns: { idColumn: 'cell_id', column: 'cell_type', confidenceColumn: '', colorColumn: '' } });
    }
    if (d.action === 'review-queue') {
      const labels = labelsOf('instrument');
      if (!labels) throw new Error(`${before}: the cells are not labeled yet`);
      if (!instrumentTypes()) await runPostXenium({ action: 'import-instrument-types' }, ds);
      for (let k = 0; k < 200 && !app.labelReview; k += 1) await sleep(25);
      await app.labelReview?.makeQueue?.(labels.id, 0, { against: null });
      const queue = app.labelReview?.queue?.();
      if (!queue?.queue?.length) throw new Error('the review queue is empty');
      const [predicted, truth] = await Promise.all([labelSource(app, refOf(labels.id, 0)), labelSource(app, refOf(instrumentTypes().id, 0))]);
      const seg = segmentationFor(app.store.project, ds.id, null);
      const done = new Set(reviewedByExample().filter((r) => r.target === 'cells').flatMap((r) => r.cells.map((c) => c.row)));
      for (const it of queue.queue.slice(0, d.first)) {
        if (done.has(it.cell)) continue;
        const has = predicted.names[predicted.label[it.cell]];
        const is = truth.names[truth.label[it.cell]];
        await reviewCells({ app, t: { dataset: ds, item }, seg, rows: [it.cell], action: has === is ? 'accept' : 'relabel', labels: { id: labels.id, level: 0 }, label: is,
          note: has === is ? `Its type, ${is}: it belongs where it is, though few of its neighbors share it` : `Its type is ${is} (the simulation's truth), not ${has}`, by: BY });
      }
      return null;
    }
    if (d.action === 'exclude-fold') {
      if (!regionsOf(app.store.project, ds.id).some((x) => x.name === d.region.name)) await runPostXenium({ action: 'draw-regions', regions: [d.region] }, ds);
      const region = regionsOf(app.store.project, ds.id).find((x) => x.name === d.region.name);
      const v = await viewer();
      const seg = segmentationFor(app.store.project, ds.id, v.segmentation());
      const rows = v.regions.rowsIn(region.id);
      await reviewRegion({ app, t: { dataset: ds, item }, regionId: region.id, action: 'exclude', note: d.note, by: BY, seg, rows });
      if (rows.length) await reviewCells({ app, t: { dataset: ds, item }, seg, rows, action: 'exclude', note: `In ${region.name}, excluded: ${d.note}`, by: BY });
      return null;
    }
    return null;
  }
  let lastWritten = null;

  // Shows an image's alignment in the Alignment view as a step says: a version by how it was made
  // (or 'in use'), over the stains, as a checkerboard, with DAPI's edges, and where it is off.
  async function showAlignment(step) {
    const a = step.alignment;
    const img = imageOf(a.image);
    if (!img) {
      note.hidden = false;
      note.textContent = 'The image is not added yet: go back to the step that adds it.';
      return null;
    }
    app.focusItem({ kind: 'dataset', id: dataset });
    await app.showAlignment(dataset, img.id, {});
    let hook = null;
    for (let k = 0; k < 400 && !hook; k += 1) {
      hook = window.situcraftAlignment?.image === img.id ? window.situcraftAlignment : null;
      if (!hook) await sleep(25);
    }
    if (!hook || active !== controller || tour.steps[index] !== step) return hook;
    const version = a.show === 'in use' ? 'in-use' : versionOf(a.image, a.show)?.id ?? 'in-use';
    hook.setView({ show: version, as: a.display ?? 'overlay', ...(a.opacity !== undefined ? { opacity: a.opacity } : {}), ...(a.cell ? { cell: a.cell } : {}), edges: Boolean(a.edges), outlines: false, map: Boolean(a.map) });
    return hook;
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
    if (c.review) await app.labelReview?.makeQueue?.(labels?.id ?? null, 0, c.against === 'none' ? { against: null } : {});
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
    card.classList.toggle('aside', Boolean(step.compare || step.mode === 'celltypes' || step.mode === 'alignment'));
    if (step.alignment) {
      controller.shown = showAlignment(step).catch((error) => {
        note.hidden = false;
        note.textContent = `The alignment could not be shown: ${error.message}`;
      });
    } else if (step.compare) {
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
      // The post-Xenium images a step shows, by what they are.
      if (view && step.images) {
        view.images = Object.entries(step.images).map(([key, l]) => [imageOf(key), l]).filter(([img]) => img).map(([img, l]) => ({ id: img.id, visible: l.visible, opacity: l.opacity, channels: [] }));
      }
      const fill = view?.cells.fill ?? '';
      if (fill === 'true-types') view.cells.fill = instrumentTypes() ? `labels:${instrumentTypes().id}:0` : null;
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
