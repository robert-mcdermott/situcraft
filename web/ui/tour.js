// A guided tour or worked example (lib/tours.js): a card at the foot of the window stepping through
// views of a dataset. Each step opens its view through app.applyView, as a saved view opens,
// without keeping it in the project. A worked example's steps may also do something first (expand
// the nuclei, plant errors, compare two segmentations, cluster and label cells, make them again on
// another segmentation, build a graph, ask a spatial question, make an answer again: the card's
// button runs it as a job and goes on when it is done), show a comparison in the Compare view (its
// labels read through it in the Labels tab), show a clustering in the Cell types view, show an
// answer in the Spatial view or its cells on the tissue, or reveal a planted segmentation's answer
// key. A cohort's worked example's steps (wave 7) set the samples' fields, make regions samples,
// describe the design, run the same steps on every sample, summarize them and compare the groups,
// and show a design, summary or comparison in the Cohort view. One tour at a time.

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
import { graphsOf } from '../lib/graphs.js';
import { NICHES_DEFAULTS, analysesOf, zoneBands } from '../lib/spatial.js';
import { showAnalysisCells } from './spatial-cells.js';
import { addDesign, designsOf, makeDesign } from '../lib/design.js';
import { draftOf } from './cohort-design.js';
import { addSampleField, sampleFields, samplesOf, setRegionSamples, setSampleField } from '../lib/samples.js';
import { summariesOf } from '../lib/cohort.js';
import { comparisonsOf as abundanceComparisons } from '../lib/abundance.js';
import { testsOf } from '../lib/expression.js';
import { addPanel, createFigure, figuresOf, saveFigure } from '../lib/figures.js';
import { certify } from './certificate.js';
import { verificationView, verifyCertificateFile } from './verify.js';
import { writePackage, writeReport } from './report.js';
import { showDialog } from './overlays.js';

let active = null;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// The actions of the post-Xenium worked examples (runPostXenium).
const POST_XENIUM_ACTIONS = new Set(['add-image', 'landmarks', 'register', 'check', 'draw-regions', 'write-regions', 'read-regions', 'import-instrument-types', 'review-queue', 'exclude-fold']);

// The spatial worked examples' actions (runSpatial).
const SPATIAL_ACTIONS = new Set(['graph', 'ask', 'remake-analysis', 'import-truth-domains']);

// The cohorts' worked examples' actions (runCohort).
// The Reports view's (wave 8, slice 9): a figure of a step's view and the results asked before,
// what made it certified and verified, the report and the package written.
const REPORT_ACTIONS = new Set(['figure', 'verify', 'report', 'package']);
const COHORT_ACTIONS = new Set(['sample-fields', 'region-samples', 'cohort-design', 'cohort-steps', 'summarize', 'compare-abundance', 'compare-expression', 'compare-spatial']);

// The Cohort view's sections a cohort step may show.
const COHORT_SECTIONS = { samples: '.cohort-table', design: '.cohort-design', steps: '.cohort-steps', summary: '.cohort-summary', qc: '.cohort-qc', abundance: '.cohort-abundance:not(.cohort-expression):not(.cohort-spatial-tests) .cohort-results', expression: '.cohort-expression .cohort-results', spatial: '.cohort-spatial-tests .cohort-results' };

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
    ? h('div.tour-intro', h('p', h('b', 'The question: '), tour.question), h('p.muted', h('b', 'What to expect: '), tour.expect), tour.note || tour.simulated ? h('p.muted', tour.note ?? 'Its errors are simulated.') : null)
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
    return s ? app.store.project.artifacts.find((a) => a.kind === 'labels' && a.state === 'complete' && a.source && a.segmentation === s.id && a.source.column !== 'domain') ?? null : null;
  };
  // The truth's domains (truth/cells.parquet's domain column), imported as labels of the true cells.
  const trueDomains = () => {
    const s = role('truth');
    return s ? app.store.project.artifacts.find((a) => a.kind === 'labels' && a.state === 'complete' && a.source && a.segmentation === s.id && a.source.column === 'domain') ?? null : null;
  };
  // A segmentation's graph of a kind (its latest); the labels a spatial question asks of (the
  // segmentation's labels from markers, or the truth's types); an answer of a step's question (of
  // the segmentation and labels at the level, the region's edge, the niches' window and count),
  // not one made again; and that answer made again on another segmentation.
  const graphOf = (r, kind) => {
    const s = role(r);
    return s ? graphsOf(app.store.project, dataset, s.id).filter((g) => g.state === 'complete' && g.parameters?.kind === kind).at(-1) ?? null : null;
  };
  const labelsFor = (q) => (q.labels === 'true-types' ? trueTypes() : labelsOf(q.segmentation ?? 'instrument'));
  const analysisOf = (q) => {
    const s = role(q.segmentation ?? 'instrument');
    if (!s) return null;
    const labels = q.question === 'moran' ? null : labelsFor(q);
    if (q.question !== 'moran' && !labels) return null;
    return analysesOf(app.store.project, dataset, s.id).filter((a) => a.state === 'complete' && a.question === q.question && !a.remakeOf
      && (q.question === 'moran' || (a.summary?.labels?.id === labels.id && (a.summary?.labels?.level ?? 0) === (q.level ?? 0)))
      && (q.region === undefined || a.summary?.edge?.name === q.region)
      && (q.niches === undefined || a.summary?.niches?.length === q.niches)
      && (q.neighbors === undefined || a.parameters?.neighbors === q.neighbors)).at(-1) ?? null;
  };
  const remadeOf = (q) => {
    const from = analysisOf(q);
    const to = role(q.to);
    return from && to ? analysesOf(app.store.project, dataset, to.id).find((a) => a.state === 'complete' && a.remakeOf === from.id) ?? null : null;
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
    if (d.action === 'graph') return Boolean(graphOf(d.segmentation ?? 'instrument', d.kind));
    if (d.action === 'ask') return Boolean(analysisOf(d));
    if (d.action === 'remake-analysis') return Boolean(remadeOf(d));
    if (d.action === 'import-truth-domains') return Boolean(trueDomains());
    if (d.action === 'sample-fields') return fieldsSet(d);
    if (d.action === 'region-samples') return regionSamplesMade(d);
    if (d.action === 'cohort-design') return Boolean(!d.refused && d.save && savedDesign(d) && drafted(d));
    if (d.action === 'figure') return Boolean(figureNamed(d.name));
    // A certificate verified, a report and a package written: each time asked.
    if (REPORT_ACTIONS.has(d.action)) return false;
    // The steps, the summary and the comparisons are answered at once when made already.
    if (COHORT_ACTIONS.has(d.action)) return false;
    return true;
  }

  // Runs a step's action in its view (the Segmentation or the Cell types view), which shows its
  // progress.
  async function run(step) {
    const d = step.do;
    const ds = datasetRecord();
    if (POST_XENIUM_ACTIONS.has(d.action)) return runPostXenium(d, ds);
    if (SPATIAL_ACTIONS.has(d.action)) return runSpatial(d, ds);
    if (COHORT_ACTIONS.has(d.action)) return runCohort(d, ds);
    if (REPORT_ACTIONS.has(d.action)) return runReports(step, d);
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
        next = addRegion(next, { dataset: ds.id, name: r.name, cls: r.cls, geometry, tool: r.rectangle ? 'rectangle' : 'polygon', layer: r.layer ?? 'the H&E', by: BY, note: 'Drawn by the worked example' });
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

  // The spatial worked examples' actions: a graph built, a question asked, an answer made again on
  // another segmentation, the truth's domains imported as labels of the true cells.
  // --- The Reports view's actions (wave 8, slice 9) ---------------------------------------------

  const figureNamed = (name) => figuresOf(app.store.project).find((f) => f.name === name) ?? null;
  // What made the figure's results, asked on the instrument's cells (the classes' enrichment).
  const ENRICHMENT = { question: 'enrichment', segmentation: 'instrument', level: 0 };
  const MORAN = { question: 'moran', segmentation: 'instrument' };
  const mapOf = () => {
    const c = clusteringOf('instrument');
    return c ? app.store.project.artifacts.filter((a) => a.kind === 'embedding' && a.clustering === c.id && a.state === 'complete').at(-1) ?? null : null;
  };
  // A result a report step names: the figure, the enrichment or Moran's I.
  const resultOf = (key, d) => (key === 'figure' ? figureNamed(d.figure) : key === 'enrichment' ? analysisOf(ENRICHMENT) : key === 'moran' ? analysisOf(MORAN) : null);
  // What the report actions made, for remote control: { figure, verification, report, package }.
  const made = {};
  // The steps a figure of results rests on, each made when it is not yet, in its view.
  const PREPARE = [
    { mode: 'celltypes', do: { action: 'cluster' } },
    { mode: 'celltypes', do: { action: 'label', markers: 'lung' } },
    { mode: 'spatial', do: { action: 'graph', kind: 'delaunay' } },
    { mode: 'spatial', do: { action: 'ask', ...ENRICHMENT } },
    { mode: 'spatial', do: { action: 'ask', ...MORAN } },
  ];

  async function runReports(step, d) {
    const before = 'do the steps before this one first';
    if (d.action === 'figure') {
      if (d.prepare) {
        for (const s of PREPARE) {
          if (done(s)) continue;
          app.focusItem({ kind: 'dataset', id: dataset });
          app.setMode(s.mode);
          await run(s);
        }
        applyStepView(step, index);
      }
      const map = mapOf();
      const enrichment = analysisOf(ENRICHMENT);
      const moran = analysisOf(MORAN);
      if (!map || !enrichment || !moran) throw new Error(`${before}: the map, the classes' enrichment and the genes in a pattern are not made yet`);
      // The Viewer's view, as the step shows it, saved; then a figure of it and the results.
      const hook = await viewer();
      await hook.settled?.();
      const view = (app.store.project.views ?? []).find((v) => v.name === d.view && v.dataset === dataset) ?? hook.saveView(d.view);
      let fig = createFigure(d.name, 'double');
      // Two panels a row, each nine tenths as tall as it is wide: room for the genes in a pattern.
      const h = Math.round(((fig.width - 24) / 2) * 0.9);
      for (const panel of [
        { kind: 'tissue', view: view.id, region: { cx: view.camera.cx, cy: view.camera.cy, width: Math.round(view.camera.umPerCss * 1000 * 1e6) / 1e6 } },
        { kind: 'map', map: map.id, color: 'clusters' },
        { kind: 'spatial', analysis: enrichment.id },
        { kind: 'spatial', analysis: moran.id },
      ]) fig = addPanel({ ...fig, height: 2 * h + 40 }, { ...panel, h }, { columns: 2 }).figure;
      fig = { ...fig, height: Math.round((fig.panels.reduce((y, p) => Math.max(y, p.y + p.h), 0) + 8) * 100) / 100 };
      app.store.commit(saveFigure(app.store.project, fig), `New figure ${fig.name}`);
      made.figure = fig.id;
      return fig;
    }
    const fig = figureNamed(d.figure);
    if (!fig) throw new Error(`${before}: the figure is not made yet`);
    if (d.action === 'verify') {
      const out = await certify(app, { kind: 'result', id: fig.id });
      const report = await verifyCertificateFile(app, { file: new Blob([out.bytes]), name: out.name });
      made.verification = { name: out.name, fingerprint: out.certificate.fingerprint, steps: out.certificate.steps.length, bundles: out.certificate.bundles.length, verdict: report.verdict, madeAgain: report.steps.filter((x) => x.by === 'program' && x.verdict === 'verified').length, stepVerdicts: report.steps.map(({ n, kind, verdict }) => ({ n, kind, verdict })) };
      showDialog({ title: `Verified: ${out.name}`, width: 'wide', content: [verificationView(report)], buttons: [{ label: 'Close', primary: true }] });
      return report;
    }
    const results = d.results.map((key) => resultOf(key, d));
    if (results.some((r) => !r)) throw new Error(`${before}: the figure and the results it reports are not made yet`);
    const options = { title: d.title, results: results.map((r) => r.id), names: Boolean(d.names) };
    if (d.action === 'report') {
      const out = await writeReport(app, options);
      downloadBlob(new Blob([out.html], { type: 'text/html' }), out.name);
      made.report = { name: out.name, numbers: out.numbers, figures: out.figures, fingerprint: out.certificate.fingerprint, leftOut: out.leftOut };
      return out;
    }
    const out = await writePackage(app, options);
    downloadBlob(new Blob([out.zip], { type: 'application/zip' }), out.name);
    made.package = { name: out.name, files: out.files, views: out.views, tiles: out.tiles, numbers: out.numbers, leftOut: out.leftOut, checked: out.checked };
    return out;
  }

  // Shows a step of the Reports view: the figure it names selected, its methods or what made it.
  async function showReports(step) {
    const r = step.reports;
    app.setMode('reports');
    for (let k = 0; k < 200 && !app.reportsView; k += 1) await sleep(25);
    const fig = figureNamed(r.figure);
    if (!app.reportsView || !fig) return;
    if (r.show === 'figure') app.reportsView.select(fig.id);
    if (r.show === 'methods') await app.reportsView.showMethods({ kind: 'result', id: fig.id });
    if (r.show === 'record') await app.reportsView.showRecord(fig.id);
  }

  async function runSpatial(d, ds) {
    const before = 'do the steps before this one first';
    for (let k = 0; k < 200 && !app.spatialView; k += 1) await sleep(25);
    if (!app.spatialView) throw new Error('the Spatial view did not open');
    if (d.action === 'import-truth-domains') {
      const s = role('truth');
      if (!s) throw new Error(`${before}: the truth is not imported yet`);
      const { folder, sep } = truthFolder(ds);
      return app.bundles.importLabels(itemOf(ds), ds.id, { segmentation: s.id, path: `${folder}${sep}cells.parquet`, columns: { idColumn: 'cell_id', column: 'domain', confidenceColumn: '', colorColumn: '' } });
    }
    const s = role(d.segmentation ?? 'instrument');
    if (!s) throw new Error(`${before}: the segmentation is not made yet`);
    if (d.action === 'graph') return app.spatialView.build(ds, { segmentation: s.id, kind: d.kind, params: d.params ?? {} });
    if (d.action === 'ask') {
      const ask = { segmentation: s.id, question: d.question, ...(d.params ?? {}) };
      if (d.question !== 'moran') {
        const labels = labelsFor(d);
        if (!labels) throw new Error(`${before}: the cells are not labeled yet`);
        ask.labels = `${labels.id}|${d.level ?? 0}`;
      }
      if (d.question === 'enrichment' || d.question === 'moran') {
        const g = graphOf(d.segmentation ?? 'instrument', d.graph ?? 'delaunay');
        if (!g) throw new Error(`${before}: the graph is not built yet`);
        ask.graph = g.id;
      }
      if (d.region !== undefined) {
        const region = regionsOf(app.store.project, ds.id).find((r) => r.name === d.region);
        if (!region) throw new Error(`${before}: the region ${d.region} is not drawn yet`);
        ask.edge = region.id;
      }
      if (d.niches !== undefined) ask.niches = d.niches;
      if (d.neighbors !== undefined) ask.neighbors = d.neighbors;
      return app.spatialView.ask(ds, ask);
    }
    if (d.action === 'remake-analysis') {
      const from = analysisOf(d);
      const to = role(d.to);
      if (!from || !to) throw new Error(`${before}: the answer, and the other segmentation, are not made yet`);
      return app.spatialView.remake(ds, from.id, to.id);
    }
    return null;
  }

  // Shows a step's answer in the Spatial view, with what in it the step chooses; or, made again on
  // another segmentation, the first read with it.
  // The cohorts' worked examples: the samples' fields (each sample's name as a field, or values by
  // sample name), regions made samples, the design (saved under a name, so that its comparisons
  // are found by it), and the Cohort view's steps, summary and comparisons.
  const fieldValues = (d) => {
    const samples = samplesOf(app.store.project);
    if (d.name) return samples.filter((x) => !x.removed).map((x) => ({ sample: x, field: d.name, value: x.name }));
    return Object.entries(d.values ?? {}).flatMap(([name, fields]) => {
      const x = samples.find((y) => y.name === name);
      return x ? Object.entries(fields).map(([field, value]) => ({ sample: x, field, value: String(value) })) : [];
    });
  };
  const fieldsSet = (d) => {
    const want = fieldValues(d);
    return want.length > 0 && want.every((w) => String(w.sample.fields[w.field] ?? '') === w.value);
  };
  const regionSamplesMade = (d) => d.regions.every((name) => samplesOf(app.store.project).some((x) => x.region?.name === name && !x.removed));
  const specOf = (d) => ({ unit: d.unit, group: d.group, reference: d.reference ?? '', contrasts: [], covariates: d.covariates ?? [], exclude: {}, ...(d.pseudoSamples ? { pseudoSamples: true } : {}) });
  // Whether the Cohort view's draft is the step's design (the comparisons are made of the draft).
  const drafted = (d) => {
    const draft = draftOf(app.store.project);
    return draft.name === d.save && JSON.stringify(makeDesign(app.store.project, draft.spec).spec) === JSON.stringify(makeDesign(app.store.project, specOf(d)).spec);
  };
  const savedDesign = (d) => {
    const spec = makeDesign(app.store.project, specOf(d)).spec;
    return designsOf(app.store.project).find((x) => x.name === d.save && JSON.stringify(x.spec) === JSON.stringify(spec)) ?? null;
  };
  // A cohort's comparison of a kind (abundance, expression, spatial), the latest, of the design so
  // named when one is.
  const cohortComparison = (kind, design) => {
    const p = app.store.project;
    const list = kind === 'abundance' ? abundanceComparisons(p) : testsOf(p, kind === 'expression' ? 'expression' : 'spatial-comparison');
    return list.find((r) => r.state === 'complete' && (!design || r.design?.name === design)) ?? null;
  };

  async function runCohort(d, ds) {
    const project = app.store.project;
    if (d.action === 'sample-fields') {
      const want = fieldValues(d);
      if (!want.length) throw new Error('do the steps before this one first: the samples are not there yet');
      let next = project;
      for (const field of new Set(want.map((w) => w.field))) {
        if (!sampleFields(next).some((f) => f.name === field)) next = addSampleField(next, field, 'text');
      }
      for (const w of want) next = setSampleField(next, w.sample.id, w.field, w.value);
      app.store.commit(next, 'Set the worked example\'s fields');
      return null;
    }
    if (d.action === 'region-samples') {
      const regions = d.regions.map((name) => regionsOf(project, ds.id).find((r) => r.name === name));
      if (regions.some((r) => !r)) throw new Error('do the steps before this one first: the regions are not drawn yet');
      app.store.commit(setRegionSamples(project, ds.id, regions.map((r) => r.id)), 'Make the regions samples');
      return null;
    }
    app.setMode('cohort');
    for (let k = 0; k < 200 && !app.cohortView; k += 1) await sleep(25);
    const view = app.cohortView;
    if (!view) throw new Error('the Cohort view did not open');
    if (d.action === 'cohort-design') {
      const design = makeDesign(app.store.project, specOf(d));
      // A design the example shows refused: drafted and shown, not saved.
      if (d.refused) {
        view.showDesign(design.spec, 'Refused');
        return design;
      }
      if (!design.ok) throw new Error(`the design cannot be used: ${design.problems.join(' ')}`);
      if (d.save && !savedDesign(d)) app.store.commit(addDesign(app.store.project, design, { name: d.save }).project, `Save design ${d.save}`);
      view.showDesign(design.spec, d.save ?? 'Design');
      return design;
    }
    if (d.action === 'cohort-steps') return view.steps({ ...(d.questions ? { questions: d.questions } : {}), ...(d.markers ? { markers: d.markers } : {}) });
    if (d.action === 'summarize') return view.summarize({ niches: d.niches === 0 ? null : { ...NICHES_DEFAULTS, niches: d.niches ?? NICHES_DEFAULTS.niches } });
    if (d.action === 'compare-abundance') return view.abundance(d.families ? { families: d.families } : {});
    if (d.action === 'compare-expression') return view.expression({});
    if (d.action === 'compare-spatial') return view.spatial(d.families ? { families: d.families } : {});
    return null;
  }

  // Shows the Cohort view's section a step names: the samples, the design, the steps, the
  // summary's table (a family of it) or its QC, or a comparison (of the design named), a family
  // and a result chosen, a label and gene.
  async function showCohort(step) {
    const c = step.cohort ?? {};
    app.setMode('cohort');
    for (let k = 0; k < 200 && !app.cohortView; k += 1) await sleep(25);
    const view = app.cohortView;
    if (!view) return null;
    let shown = null;
    if (['abundance', 'expression', 'spatial'].includes(c.show)) {
      const r = cohortComparison(c.show, c.design);
      if (!r) {
        note.hidden = false;
        note.textContent = 'The comparison is not made yet: go back to the steps that make it.';
        return null;
      }
      if (c.show === 'abundance') shown = await view.comparison(r.id, { family: c.family ?? null, pick: c.pick ?? null });
      if (c.show === 'expression') shown = await view.expressionResult(r.id, { label: c.label ?? null, gene: c.gene ?? null });
      if (c.show === 'spatial') shown = await view.spatialResult(r.id, { family: c.family ?? null, pick: c.pick ?? null });
    } else if (c.show === 'summary' || c.show === 'qc') {
      const r = summariesOf(app.store.project)[0];
      if (r) shown = c.show === 'qc' ? await view.qc(r.id) : await view.table(r.id, c.family ?? null);
    }
    if (active !== controller || tour.steps[index] !== step) return shown;
    const selector = COHORT_SECTIONS[c.show ?? 'samples'];
    for (let k = 0; k < 200 && !document.querySelector(selector); k += 1) await sleep(25);
    document.querySelector(selector)?.scrollIntoView({ block: 'start' });
    return shown;
  }

  async function showSpatial(step) {
    const q = step.spatial;
    const analysis = analysisOf(q);
    const again = q.to ? remadeOf(q) : null;
    if (!analysis || (q.to && !again)) {
      note.hidden = false;
      note.textContent = 'The answer is not made yet: go back to the steps that make it.';
      return null;
    }
    app.focusItem({ kind: 'dataset', id: dataset });
    app.setMode('spatial');
    for (let k = 0; k < 200 && !app.spatialView?.show; k += 1) await sleep(25);
    const choice = { ...(q.choose ?? {}) };
    if (again) choice.readWith = again.id;
    const shown = await app.spatialView.show(datasetRecord(), analysis.id, choice);
    if (active !== controller || tour.steps[index] !== step) return shown;
    await sleep(50);
    const at = q.section ? document.querySelector(q.section) : document.querySelector('.sp-answer');
    at?.scrollIntoView({ block: 'start' });
    return shown;
  }

  // Shows an answer's cells on the tissue, as the answer's Show these cells does: a pair's cells,
  // a label's in a zone of a region's edge, a gene's cells of a local class, or a niche's.
  async function showCells(step) {
    const c = step.cells_of;
    const record = analysisOf(c);
    if (!record) {
      note.hidden = false;
      note.textContent = 'The answer is not made yet: go back to the steps that make it.';
      return null;
    }
    const data = await app.bundles.analysisData(record.id);
    const names = data.analysis.labels?.names ?? [];
    const ds = datasetRecord();
    const segmentation = app.store.project.artifacts.find((x) => x.kind === 'segmentation' && x.id === record.segmentation);
    const base = { dataset, item: itemOf(ds), segmentation, analysis: data.analysis, cells: data.cells };
    if (data.analysis.question === 'moran') return showAnalysisCells(app, { ...base, local: data.local, gene: data.analysis.moran.genes.indexOf(c.gene), localClass: c.localClass ?? 1 });
    if (data.analysis.question === 'niches') return showAnalysisCells(app, { ...base, id: record.id, niche: c.niche === undefined ? -1 : c.niche - 1 });
    if (data.analysis.question === 'boundary') {
      const p = data.analysis.parameters;
      const where = { core: `in the core (inside, beyond ${p.margin} µm)`, margin: `at the margin (within ${p.margin} µm of the edge)`, around: `around (outside, ${p.margin} to ${p.reach} µm)` }[c.zone];
      return showAnalysisCells(app, { ...base, a: names.indexOf(c.a), bands: zoneBands(data.analysis.boundary.edges, p.margin, c.zone), where });
    }
    return showAnalysisCells(app, { ...base, a: names.indexOf(c.a), b: names.indexOf(c.b ?? c.a), k: 0 });
  }

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
    card.classList.toggle('aside', Boolean(step.compare || step.mode === 'celltypes' || step.mode === 'alignment' || step.spatial || step.mode === 'cohort'));
    if (step.mode === 'cohort') {
      controller.shown = showCohort(step).catch((error) => {
        note.hidden = false;
        note.textContent = `The cohort could not be shown: ${error.message}`;
      });
    } else if (step.spatial) {
      controller.shown = showSpatial(step).catch((error) => {
        note.hidden = false;
        note.textContent = `The answer could not be shown: ${error.message}`;
      });
    } else if (step.cells_of) {
      controller.shown = showCells(step).catch((error) => {
        note.hidden = false;
        note.textContent = `The cells could not be shown: ${error.message}`;
      });
    } else if (step.alignment) {
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
    } else if (step.reports) {
      controller.shown = showReports(step).catch((error) => {
        note.hidden = false;
        note.textContent = `The Reports view could not be shown: ${error.message}`;
      });
    } else {
      applyStepView(step, i);
      controller.shown = step.reveal ? (async () => {
        for (let k = 0; k < 200 && !app.segmentationView; k += 1) await sleep(25);
        app.segmentationView?.revealAnswers?.(dataset);
      })() : Promise.resolve();
    }
    next.focus();
  }

  // A step's view of the tissue (or QC) applied, its fill and images by what they are; a step of
  // another view shown.
  function applyStepView(step, i) {
    {
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
      // The niches of a step's question, and the truth's domains, as label sets of the true cells.
      if (fill === 'niches') view.cells.fill = analysisOf({ ...step.niches, question: 'niches' }) ? `labels:${analysisOf({ ...step.niches, question: 'niches' }).id}:0` : null;
      if (fill === 'true-domains') view.cells.fill = trueDomains() ? `labels:${trueDomains().id}:0` : null;
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
    }
  }

  function close() {
    card.remove();
    if (active === controller) active = null;
  }

  // A step's spatial answer, and the answer made again on another segmentation: for remote control.
  const answers = (step) => {
    const q = step.spatial ?? step.cells_of;
    return q ? { analysis: analysisOf(q)?.id ?? null, again: q.to ? remadeOf(q)?.id ?? null : null } : null;
  };
  // A cohort step's summary and comparison: for remote control.
  const cohortAnswers = (step) => {
    const c = step.cohort;
    if (!c) return null;
    return { summary: summariesOf(app.store.project)[0]?.id ?? null, comparison: ['abundance', 'expression', 'spatial'].includes(c.show) ? cohortComparison(c.show, c.design)?.id ?? null : null };
  };
  const controller = { close, go, advance, step: () => index, card, tour, dataset, open: () => card.isConnected, done: () => done(tour.steps[index]), answers: () => answers(tour.steps[index]), cohort: () => cohortAnswers(tour.steps[index]), reports: () => ({ ...made }), shown: Promise.resolve() };
  active = controller;
  app.tour = controller;
  go(0);
  return controller;
}
