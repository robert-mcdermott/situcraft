// Remote control (wave 2, slice 1): actions sent by programs on this computer (scripts, the
// documentation's screenshot capture, and later AI agents) are performed here, in the open window,
// where the user sees them; what they change goes into the project's history like any edit, and can
// be undone. Each action returns { ok, message, data }. The program lists the same actions with
// their arguments (actions.go, GET /api/remote/tools); a Go test checks the two lists agree. The
// connection and the export upload are adapted from MaveScape 0.8.0 web/ui/remote.js.

import { toast } from './overlays.js';
import { cacheJobKey } from './bundles.js';
import { datasetFilters, methodsMarkdown, showMethods, writeMethods } from './methods.js';
import { ATTRIBUTES } from '../lib/cell-attributes.js';
import { decideSegmentation, DECISIONS, EXPORT_FORMATS, promotedSegmentation } from '../lib/segmentations.js';
import { CLASS_INFO, RANKS, cellNames, warnings } from '../lib/review.js';
import { workedExamples } from '../lib/tours.js';
import { describeImage, describePlacement, imagesOf } from '../lib/images.js';
import { describeTransform, transformInUse, transformsOf, useTransform } from '../lib/transforms.js';
import { defaultImageLayer } from '../lib/overlays.js';
import { parseProject, rename, serializeProject } from '../lib/project.js';
import { removeView, sameView, viewLink } from '../lib/views.js';
import { SEG_FILLS, defaultChoice, segmentationChoices } from '../lib/segview.js';
import { clusterName, clusterNames, clusteringsOf, mapsOf, nameClusters } from '../lib/clusterings.js';
import { composition as labelComposition, labelSetName, labelSetsFor, lowConfidenceDefault, namesFromLabels } from '../lib/labels.js';
import { RULES, appendEdit, createCurated, crossTable, curatedSetsOf, saveConsensus } from '../lib/curation.js';
import { labelSource, labelVersions, refOf } from './label-sources.js';
import { defaultPair, impactLines, sideOf } from '../lib/impact.js';
import { remakeOnSegmentation } from './remake.js';

export class ActionError extends Error {}

const plural = (n, word) => `${n.toLocaleString('en-US')} ${word}${n === 1 ? '' : 's'}`;
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
// Two frames, for what an action changed to be drawn; a hidden window draws no frames, so at most
// 150 ms.
const frames = () => Promise.race([new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))), sleep(150)]);

// The views by the names scripts use.
const MODES = { start: 'welcome', welcome: 'welcome', data: 'data', viewer: 'viewer', tissue: 'viewer', qc: 'qc', segmentation: 'segmentation', compare: 'compare', comparison: 'compare', celltypes: 'celltypes', 'cell types': 'celltypes', spatial: 'spatial', cohort: 'cohort', reports: 'reports', jobs: 'jobs' };
const FILLS = ['transcripts', 'total', 'area', 'nucleusArea', 'method'];
// A segmentation's fills besides those (lib/segview.js).
const SEGMENTATION_FILLS = SEG_FILLS.map((f) => f.id).filter((id) => !FILLS.includes(id));

// One of `options` (strings) by a forgiving name: exact, then ignoring case, then the one that
// starts with or contains it. Errors list the choices.
function choose(value, options, what) {
  const text = String(value ?? '').trim();
  const lower = text.toLowerCase();
  const found = options.find((o) => o === text) ?? options.find((o) => o.toLowerCase() === lower);
  if (found) return found;
  const near = options.filter((o) => o.toLowerCase().startsWith(lower));
  if (lower && near.length === 1) return near[0];
  const inside = options.filter((o) => o.toLowerCase().includes(lower));
  if (lower && inside.length === 1) return inside[0];
  throw new ActionError(`${text ? `No ${what} "${text}"` : `Give the ${what}`}. ${what[0].toUpperCase()}${what.slice(1)}s: ${options.join(', ') || 'none'}.`);
}

// Waits for test() to answer something truthy, at most ms; answers it.
async function waitFor(test, ms, what) {
  const until = Date.now() + ms;
  for (;;) {
    const value = test();
    if (value) return value;
    if (Date.now() > until) throw new ActionError(`Timed out waiting for ${what}.`);
    await sleep(60);
  }
}

const finite = (v, what) => {
  const n = Number(v);
  if (!Number.isFinite(n)) throw new ActionError(`${what} is a number.`);
  return n;
};

// A refinement's request (api_refine.go) from refine_segmentation's or refinement_preflight's
// arguments.
const PRESETS = ['balanced', 'conservative', 'flexible', 'low-memory', 'roi-pilot', 'expert'];
function refineRequest(args) {
  const request = {};
  if (args.preset !== undefined) request.preset = choose(args.preset, PRESETS, 'preset');
  for (const key of ['sweeps', 'samples', 'seed', 'layers', 'threads']) {
    if (args[key] === undefined) continue;
    const v = finite(args[key], key);
    if (!Number.isInteger(v) || v < 0) throw new ActionError(`${key} is a whole number.`);
    request[key] = v;
  }
  if (args.min_qv !== undefined) request.min_qv = finite(args.min_qv, 'min_qv');
  if (args.seed_check !== undefined) request.seed_check = Boolean(args.seed_check);
  if (args.regions !== undefined) {
    if (!Array.isArray(args.regions) || !args.regions.length || args.regions.some((r) => !Array.isArray(r) || r.length !== 4)) throw new ActionError('regions is a list of [x0, y0, x1, y1] in µm.');
    if (args.sections) throw new ActionError('Refine regions or sections, not both.');
    request.area = { mode: 'regions', regions: args.regions.map((r) => {
      const [x0, y0, x1, y1] = r.map((v) => finite(v, 'a region\'s corner'));
      if (!(x1 > x0 && y1 > y0)) throw new ActionError('A region\'s second corner is right of and below its first.');
      return { x0_um: x0, y0_um: y0, x1_um: x1, y1_um: y1 };
    }) };
  } else if (args.sections) {
    request.area = { mode: 'sections' };
  }
  return request;
}

export function installRemote(app) {
  const { store } = app;
  const project = () => store.project;
  // Who sent the action being performed (what the script calls itself).
  let author = 'a program on this computer';
  let counter = 0;

  // --- Resolution of names --------------------------------------------------------------------

  // A dataset by name or id; by default the one in focus (or a focused view's or filter's), else
  // the first.
  function resolveDataset(ref) {
    const datasets = project().datasets;
    if (!datasets.length) throw new ActionError('The project has no dataset yet: add one (open_example, open_paths, add_dataset).');
    if (ref) {
      const byId = datasets.find((d) => d.id === ref);
      if (byId) return byId;
      const name = choose(ref, datasets.map((d) => d.name), 'dataset');
      return datasets.find((d) => d.name === name);
    }
    const focus = store.ui.focus;
    const fromFocus = focus?.kind === 'dataset' ? focus.id
      : focus?.kind === 'view' ? project().views.find((v) => v.id === focus.id)?.dataset
        : focus?.kind === 'filter' ? project().artifacts.find((a) => a.id === focus.id)?.dataset : null;
    const shown = window.situcraftViewer?.dataset ?? window.situcraftQC?.dataset;
    return datasets.find((d) => d.id === fromFocus) ?? datasets.find((d) => d.id === shown) ?? datasets[0];
  }

  function resolveView(ref) {
    const views = project().views;
    if (!views.length) throw new ActionError('The project has no saved views: save_view saves one.');
    const byId = views.find((v) => v.id === ref);
    if (byId) return byId;
    const name = choose(ref, views.map((v) => v.name), 'saved view');
    return views.find((v) => v.name === name);
  }

  // A filter by id, or by name (its latest version; "name v2" or "name, version 2" for another).
  function resolveFilter(ref, dataset = null) {
    const filters = project().artifacts.filter((a) => a.kind === 'filter' && (!dataset || a.dataset === dataset.id));
    if (!filters.length) throw new ActionError(`${dataset ? `${dataset.name} has` : 'The project has'} no filters: save_filter saves one.`);
    const byId = filters.find((f) => f.id === ref);
    if (byId) return byId;
    const m = /^(.*?)(?:,?\s+v(?:ersion)?\s*(\d+))$/i.exec(String(ref).trim());
    const name = choose(m ? m[1] : ref, [...new Set(filters.map((f) => f.name))], 'filter');
    const of = filters.filter((f) => f.name === name).sort((a, b) => a.version - b.version);
    if (!m) return of.at(-1);
    const found = of.find((f) => f.version === Number(m[2]));
    if (!found) throw new ActionError(`${name} has versions ${of.map((f) => f.version).join(', ')}.`);
    return found;
  }

  function resolveExample(ref) {
    const list = app.examples.list() ?? [];
    const names = list.map((e) => e.name);
    const text = String(ref ?? '').trim().toLowerCase();
    const short = { lung: 'xenium-lung-2fov', breast: 'xenium-breast-2fov', simulated: 'simulated-standard' }[text];
    return list.find((e) => e.name === choose(short ?? ref, names, 'example'));
  }

  // The bundle on this computer a dataset is read from, failing when it is not found.
  function bundleOf(dataset) {
    const link = app.bundles.link(dataset.id);
    if (link?.state === 'missing' || link?.state === 'different-run') throw new ActionError(`${dataset.name}'s bundle is not where the project recorded it (${dataset.path}): locate it in the Data view first.`);
    const item = link?.item ?? app.bundles.itemByPath(dataset.path);
    if (!item) throw new ActionError(`${dataset.name}'s bundle is not open on this computer.`);
    return item;
  }

  // --- Waiting for the window ------------------------------------------------------------------

  const hooks = (mode) => (mode === 'qc' ? window.situcraftQC : mode === 'viewer' ? window.situcraftViewer : null);

  // Waits for a view (viewer or qc) to show a dataset, its pending saved view applied and its
  // tiles drawn.
  async function shown(mode, dataset) {
    const hook = await waitFor(() => store.ui.mode === mode && hooks(mode)?.dataset === dataset.id && !app.pendingView && hooks(mode), 30 * 60000, `${dataset.name} to open in the ${mode === 'qc' ? 'QC view' : 'Viewer'} (its derived data may still be building)`);
    await hook.settled?.();
    await frames();
    return hook;
  }

  // Opens a dataset in the Viewer or the QC view (as it was shown there last).
  async function openIn(mode, dataset) {
    bundleOf(dataset);
    if (!(store.ui.mode === mode && hooks(mode)?.dataset === dataset.id)) {
      app.focusItem({ kind: 'dataset', id: dataset.id });
      if (store.ui.mode !== mode) await app.setMode(mode);
    }
    return shown(mode, dataset);
  }

  // Applies a view (saved, or made here) and waits for it to be shown.
  async function apply(view, dataset) {
    app.applyView(view);
    const hook = await shown(view.mode, dataset);
    // The segmentation it draws, read.
    await hook.segmentationReady?.();
    return hook;
  }

  async function settleMode(mode) {
    if (store.ui.mode !== mode) await app.setMode(mode);
    await frames();
  }

  // Waits for a bundle's derived data to be built (the job the page started when it was added).
  async function derivedData(item) {
    const key = cacheJobKey(item.path);
    const started = Date.now();
    await waitFor(() => {
      const entry = app.bundles.cache(item.path);
      if (entry?.status?.ready) return true;
      const ended = app.jobs.list().filter((j) => j.key === key).at(-1);
      if (ended && ['failed', 'invalid', 'cancelled'].includes(ended.state) && Date.parse(ended.created) >= started - 60000) throw new ActionError(`${item.name}'s derived data were not built: ${ended.error || ended.state}.`);
      return false;
    }, 30 * 60000, `${item.name}'s derived data to be built`);
  }

  async function reconnected() {
    await waitFor(() => project().datasets.every((d) => app.bundles.link(d.id) && app.bundles.link(d.id).state !== 'checking'), 120000, 'the project\'s bundles to be found');
  }

  // --- Descriptions ----------------------------------------------------------------------------

  function datasetData(d) {
    const link = app.bundles.link(d.id);
    const item = link?.item ?? app.bundles.itemByPath(d.path);
    const cache = item ? app.bundles.cache(item.path) : null;
    const v = d.validation ?? {};
    return {
      id: d.id, name: d.name, path: d.path, panel: d.panel, organism: d.organism, tissue: d.tissue, software: d.software,
      cells: d.cells, transcripts: d.transcripts, channels: d.channels,
      bundle: link?.state ?? 'not checked',
      check: { fatal: v.fatal ?? 0, warnings: v.warnings ?? 0, notes: v.info ?? 0, metricsCompared: v.metricsCompared ?? 0, metricsAgreeing: v.metricsAgreeing ?? 0 },
      derivedData: cache?.status ? (cache.status.ready ? 'ready' : 'not built') : (app.bundles.cacheJob?.(item ?? { path: '' }) ? 'building' : 'unknown'),
      example: app.examples.forDataset(d)?.name ?? null,
    };
  }

  const datasetName = (id) => project().datasets.find((d) => d.id === id)?.name ?? id;

  function filterData(f) {
    return { id: f.id, name: f.name, version: f.version, dataset: datasetName(f.dataset), conditions: f.conditions, cells: f.result.cells, kept: f.result.kept, sha256: f.result.sha256 };
  }

  const link = (view) => viewLink(location.href, project().id, view.id);

  function viewData(v) {
    const seg = v.segmentation && v.segmentation !== 'instrument' ? project().artifacts.find((a) => a.kind === 'segmentation' && a.id === v.segmentation) : null;
    return { id: v.id, name: v.name, dataset: datasetName(v.dataset), mode: v.mode, segmentation: v.segmentation === 'instrument' || !v.segmentation ? 'instrument' : seg?.name ?? v.segmentation, camera: v.camera, genes: v.transcripts?.genes ?? [], cell: v.cell?.id ?? null, ...(v.qc ? { qc: v.qc } : {}), link: link(v) };
  }

  // What the Viewer or the QC view shows now, as a saved view records it.
  function showing() {
    const mode = store.ui.mode;
    const hook = hooks(mode);
    if (!hook?.captureView) return null;
    const v = hook.captureView('');
    const { id, name, created, ...rest } = v;
    return { ...rest, dataset: datasetName(v.dataset) };
  }

  // --- Actions --------------------------------------------------------------------------------

  const actions = {
    async get_state() {
      const p = project();
      const filters = p.artifacts.filter((a) => a.kind === 'filter');
      const mode = store.ui.mode === 'welcome' ? 'start' : store.ui.mode;
      const chosen = app.selection ? { dataset: datasetName(app.selection.dataset), count: app.selection.count, label: app.selection.label ?? null } : null;
      const tour = app.tour?.open?.() ? { title: app.tour.tour.title, step: app.tour.step() + 1, steps: app.tour.tour.steps.length, showing: app.tour.tour.steps[app.tour.step()].title } : null;
      const data = {
        project: { id: p.id, name: p.name, created: p.created, modified: p.modified, history: p.history.length },
        segmentations: p.artifacts.filter((a) => a.kind === 'segmentation').map((a) => ({ id: a.id, name: a.name, dataset: datasetName(a.dataset), state: a.state, default: promotedSegmentation(p, a.dataset)?.record.id === a.id })),
        images: p.artifacts.filter((a) => a.kind === 'image').map((a) => ({ id: a.id, name: a.name, dataset: datasetName(a.dataset), path: a.image, placedBy: transformInUse(p, a.id)?.record.id ?? null, alignments: transformsOf(p, a.id).map((x) => ({ id: x.id, name: x.name, how: describeTransform(x) })) })),
        mode,
        focus: store.ui.focus ?? null,
        datasets: p.datasets.map(datasetData),
        filters: filters.map(filterData),
        views: p.views.map(viewData),
        chosen,
        showing: showing(),
        tour,
        local: app.localItems.map((i) => ({ name: i.name, kind: i.kind, path: i.path })),
        examples: (app.examples.list() ?? []).map((e) => ({ name: e.name, title: e.title, state: e.state, download: e.download, size: e.size, license: e.license })),
        jobs: app.jobs.list().filter((j) => !['complete', 'cancelled'].includes(j.state)).map((j) => ({ id: j.id, title: j.title, state: j.state, progress: j.progress, message: j.message ?? '', resumable: Boolean(j.resumable) })),
        library: app.library.location,
      };
      return { message: `${p.name}: ${plural(p.datasets.length, 'dataset')}, ${plural(filters.length, 'filter')}, ${plural(p.views.length, 'saved view')}; showing ${mode}.`, data };
    },

    async new_project(args) {
      await app.newProject();
      if (args.name) store.commit(rename(project(), String(args.name)), 'Rename project');
      return { message: `Started a new project, ${project().name}.`, data: { id: project().id, name: project().name } };
    },

    async open_project(args) {
      const list = await app.library.listProjects();
      if (!list.length) throw new ActionError('The library holds no projects.');
      const byId = list.find((p) => p.id === args.project);
      const name = byId ? null : choose(args.project, list.map((p) => p.name || 'Untitled project'), 'project');
      const found = byId ?? list.find((p) => (p.name || 'Untitled project') === name);
      await app.openProject(found.id);
      if (project().id !== found.id) throw new ActionError(`${found.name} could not be opened.`);
      await reconnected();
      return { message: `Opened ${project().name}: ${plural(project().datasets.length, 'dataset')}.`, data: { id: project().id, name: project().name, datasets: project().datasets.map(datasetData) } };
    },

    async open_paths(args, event) {
      // The program has opened the items (remote.go): the page takes its list again.
      const info = await fetch('api/info', { cache: 'no-store' }).then((r) => r.json()).catch(() => null);
      if (info) app.setLocalItems(info.files);
      const items = (event.files ?? []).map((f) => app.localItems.find((i) => i.url === f.url) ?? f);
      const opened = [];
      for (const item of items) {
        if (item.kind === 'project') {
          await app.openArchive(item);
          await reconnected();
          opened.push({ name: item.name, kind: 'project archive', project: project().name });
        } else if (item.kind === 'bundle' || item.kind === 'archive') {
          const report = await app.bundles.validate(item).catch((error) => {
            throw new ActionError(`${item.name}: ${error.message}`);
          });
          const fatal = report.findings.filter((f) => f.severity === 'fatal').length;
          const entry = { name: item.name, kind: 'bundle', path: item.path, fatal, warnings: report.findings.filter((f) => f.severity === 'warning').length };
          if (args.add && !fatal) {
            const dataset = app.bundles.datasetFor(item) ?? (await app.bundles.add(item));
            await derivedData(item);
            await app.bundles.registerSegmentation(item, dataset.id);
            entry.dataset = dataset.name;
          }
          opened.push(entry);
        } else {
          opened.push({ name: item.name, kind: item.kind });
        }
      }
      const bundle = items.find((i) => i.kind === 'bundle' || i.kind === 'archive');
      if (bundle && !items.some((i) => i.kind === 'project')) {
        const dataset = app.bundles.datasetFor(bundle);
        app.focusItem(dataset ? { kind: 'dataset', id: dataset.id } : { kind: 'local', id: bundle.url });
        await settleMode('data');
      }
      return { message: `Opened ${opened.map((o) => `${o.name}${o.dataset ? ' (added to the project)' : o.fatal ? ` (${plural(o.fatal, 'fatal problem')})` : ''}`).join(', ')}.`, data: { opened } };
    },

    async add_dataset(args) {
      const bundles = app.bundles.items();
      if (!bundles.length) throw new ActionError('No bundle is open on this computer: open_paths opens one.');
      const ref = String(args.bundle ?? '');
      const item = bundles.find((b) => b.path === ref || b.path === ref.replace(/[\\/]+$/, '')) ?? bundles.find((b) => b.name === choose(ref, bundles.map((x) => x.name), 'bundle'));
      const report = await app.bundles.validate(item);
      const fatal = report.findings.filter((f) => f.severity === 'fatal');
      if (fatal.length) throw new ActionError(`${item.name} has ${plural(fatal.length, 'fatal problem')}: ${fatal.map((f) => f.title).join('; ')}.`);
      const known = app.bundles.datasetFor(item);
      const dataset = known ?? (await app.bundles.add(item));
      if (args.wait !== false) {
        await derivedData(item);
        await app.bundles.registerSegmentation(item, dataset.id);
      }
      app.focusItem({ kind: 'dataset', id: dataset.id });
      return { message: `${known ? `${dataset.name} was already in the project` : `Added ${dataset.name} to the project`}${args.wait !== false ? '; its derived data are built' : ''}.`, data: datasetData(dataset) };
    },

    async open_example(args) {
      if (!app.examples.list()) await app.examples.refresh();
      let example = resolveExample(args.example);
      if (example.state !== 'ready') {
        // A generated example is made here, nothing downloaded.
        if (!args.download && !example.generated) throw new ActionError(`${example.title} is not on this computer: pass download: true to download it from 10x Genomics (${(example.download / 1e6).toFixed(0)} MB).`);
        await app.examples.download(example.name);
        await waitFor(() => !app.examples.job(example.name) && app.examples.list().find((e) => e.name === example.name)?.state === 'ready', 60 * 60000, `${example.title} to ${example.generated ? 'be made' : 'download'}`).catch(async (error) => {
          await app.examples.refresh();
          throw error;
        });
        example = resolveExample(example.name);
      }
      const { dataset, item } = await app.examples.inProject(example.name);
      await derivedData(item);
      await app.bundles.registerSegmentation(item, dataset.id);
      app.focusItem({ kind: 'dataset', id: dataset.id });
      await settleMode('data');
      return { message: `Opened ${example.title} in ${project().name}.`, data: { project: project().name, dataset: datasetData(dataset) } };
    },

    async tour(args) {
      if (args.end) {
        app.tour?.close();
        return { message: 'The tour is closed.' };
      }
      if (!app.examples.list()) await app.examples.refresh();
      let card = app.tour?.open?.() ? app.tour : null;
      if (args.example !== undefined || !card) {
        if (args.example === undefined) throw new ActionError('Name the example whose tour to take (xenium-lung-2fov, xenium-breast-2fov or simulated-standard).');
        const example = resolveExample(args.example);
        if (example.state !== 'ready') throw new ActionError(`${example.title} is not on this computer: ${example.generated ? 'open_example makes it' : 'open_example with download: true downloads it'}.`);
        const names = ['tour', ...workedExamples(example.name).map((w) => w.id)];
        const name = args.name === undefined ? 'tour' : choose(args.name, names, 'tour or worked example');
        card = await app.examples.tour(example.name, name);
        if (!card) throw new ActionError(`${example.title}'s ${name === 'tour' ? 'tour' : `worked example ${name}`} could not start.`);
        const { item, dataset } = await app.examples.inProject(example.name);
        await derivedData(item);
        // A worked example's steps start from the dataset's segmentation.
        await app.bundles.registerSegmentation(item, dataset.id);
      }
      const steps = card.tour.steps;
      if (args.step !== undefined) {
        const at = Math.round(finite(args.step, 'step')) - 1;
        if (at < 0 || at >= steps.length) throw new ActionError(`The tour has steps 1 to ${steps.length}.`);
        card.go(at);
      }
      if (args.run) {
        const before = card.step();
        await card.advance();
        if (card.open() && card.step() === before) throw new ActionError(`Step ${before + 1} (${steps[before].title}) could not be done.`);
      }
      if (!card.open()) return { message: `${card.tour.title} is finished.`, data: { tour: card.tour.title, finished: true } };
      const at = card.step();
      const step = steps[at];
      await card.shown;
      const dataset = project().datasets.find((d) => d.id === card.dataset);
      if (step.mode === 'viewer' || step.mode === 'qc') await shown(step.mode, dataset);
      else await settleMode(step.compare ? 'compare' : step.mode);
      await frames();
      const data = { tour: card.tour.title, step: at + 1, steps: steps.length, title: step.title, text: step.text, mode: step.mode, done: card.done() };
      if (step.do) data.action = step.do.action;
      const w = step.compare ? app.compareView?.current?.() : null;
      if (w) {
        data.comparison = { id: w.id, names: w.names, mode: w.session.mode, rank: w.session.by, tab: w.session.tab, queue: w.state.queue?.length ?? 0, classes: w.description.classes };
        if (w.session.selected !== null) data.comparison.selected = { class: w.groups[w.session.selected].class, ...cellNames(w.prepared, w.groups[w.session.selected], 6) };
        // The Labels tab: the two label sets read through the comparison.
        const im = w.session.tab === 'labels' ? w.session.impact?.data?.impact : null;
        if (im) data.comparison.labels = { a: w.session.impact.a, b: w.session.impact.b, matched: { cells: im.matched.cells, labeled: im.matched.labeled, agreement: im.matched.agreement, labeledOne: im.matched.labeledOne }, split: im.split, merged: im.merged, sensitive: { a: im.sensitive[0], b: im.sensitive[1] }, mecr: im.mixing ? { a: im.mixing.allA.mecr, b: im.mixing.allB.mecr } : null, fill: Boolean(w.session.impact.fill) };
      }
      // A Cell types step: the clustering and the map shown, their colors and selection, and the
      // review queue made.
      if (step.celltypes) {
        const st = app.cellTypesView?.map?.()?.state;
        const queue = step.celltypes.review ? app.labelReview?.queue?.() : null;
        data.celltypes = { clustering: st?.clustersOf ?? null, map: st?.mapId ?? null, cells: st?.map?.n ?? 0, color: st?.color ?? null, selected: st?.count ?? 0, ...(queue ? { review: queue.counts, against: queue.against } : {}) };
      }
      return { message: `${card.tour.title}, step ${at + 1} of ${steps.length}: ${step.title}. ${step.text}`, data };
    },

    async set_mode(args) {
      const key = String(args.mode ?? '').trim().toLowerCase();
      const mode = MODES[key] ?? MODES[choose(key, Object.keys(MODES), 'view')];
      await app.setMode(mode);
      await frames();
      return { message: `Showing ${mode === 'welcome' ? 'the start page' : `the ${mode} view`}.`, data: { mode: mode === 'welcome' ? 'start' : mode } };
    },

    async focus(args) {
      if (!args.name) {
        app.focusItem(null);
        return { message: 'Nothing in focus.' };
      }
      const p = project();
      const all = [
        ...p.datasets.map((d) => ({ kind: 'dataset', name: d.name, id: d.id })),
        ...p.artifacts.filter((a) => a.kind === 'filter').map((f) => ({ kind: 'filter', name: `${f.name} v${f.version}`, id: f.id })),
        ...p.views.map((v) => ({ kind: 'view', name: v.name, id: v.id })),
        ...app.localItems.map((i) => ({ kind: 'local', name: i.name, id: i.url })),
      ].filter((x) => !args.kind || x.kind === args.kind);
      if (args.kind && !['dataset', 'filter', 'view', 'local'].includes(args.kind)) throw new ActionError('kind is dataset, filter, view or local.');
      let item = null;
      if (args.kind === 'filter' || (!args.kind && !all.some((x) => x.name.toLowerCase() === String(args.name).toLowerCase()) && p.artifacts.some((a) => a.kind === 'filter' && a.name === args.name))) {
        const f = resolveFilter(args.name);
        item = { kind: 'filter', name: `${f.name} v${f.version}`, id: f.id };
      } else {
        const name = choose(args.name, all.map((x) => x.name), args.kind ?? 'item');
        item = all.find((x) => x.name === name);
      }
      if (item.kind === 'view') app.applyView(p.views.find((v) => v.id === item.id));
      else app.focusItem({ kind: item.kind, id: item.id });
      await frames();
      return { message: `${item.name} (${item.kind}) is in focus.`, data: item };
    },

    async show_tissue(args) {
      const dataset = resolveDataset(args.dataset);
      const v = await openIn('viewer', dataset);
      const desc = v.desc;
      const view = { ...v.captureView(''), id: `remote-${(counter += 1)}` };
      const missing = [];
      // Where.
      if (args.whole) view.camera = null;
      else if (args.center !== undefined || args.um_per_pixel !== undefined || args.width_um !== undefined) {
        const [width] = v.stageSize();
        let umPerCss = view.camera.umPerCss;
        if (args.um_per_pixel !== undefined) umPerCss = finite(args.um_per_pixel, 'um_per_pixel');
        else if (args.width_um !== undefined) umPerCss = finite(args.width_um, 'width_um') / Math.max(1, width);
        if (!(umPerCss > 0)) throw new ActionError('The zoom is a positive number of µm per pixel.');
        const center = args.center ?? [view.camera.cx, view.camera.cy];
        if (!Array.isArray(center) || center.length !== 2) throw new ActionError('center is [x, y] in µm.');
        view.camera = { cx: finite(center[0], 'center x'), cy: finite(center[1], 'center y'), umPerCss: Number(umPerCss.toPrecision(12)) };
      }
      // Transcripts.
      const panel = (desc.transcripts?.genes ?? []).map((g) => g.name);
      if (args.genes !== undefined) {
        if (!Array.isArray(args.genes)) throw new ActionError('genes is a list of gene names.');
        if (args.genes.length > 16) throw new ActionError('At most 16 genes are shown at once.');
        const genes = [];
        for (const g of args.genes) {
          const exact = panel.find((n) => n === g) ?? panel.find((n) => n.toLowerCase() === String(g).toLowerCase());
          if (exact) genes.push(exact);
          else missing.push(`gene ${g}`);
        }
        view.transcripts.genes = genes;
        if (genes.length) view.transcripts.all = false;
      }
      if (args.all_transcripts !== undefined) view.transcripts.all = Boolean(args.all_transcripts);
      if (args.transcripts !== undefined) view.transcripts.visible = Boolean(args.transcripts);
      if (args.genes?.length || args.all_transcripts) view.transcripts.visible = args.transcripts ?? true;
      if (args.min_quality !== undefined) view.transcripts.minQ = Math.max(0, finite(args.min_quality, 'min_quality'));
      if (args.point_size !== undefined) view.transcripts.pointSize = Math.min(8, Math.max(1, finite(args.point_size, 'point_size')));
      // Stains.
      if (args.channels !== undefined) {
        if (typeof args.channels !== 'object' || Array.isArray(args.channels)) throw new ActionError('channels is an object by stain name: {"DAPI": {"visible": true, "color": "#4f7cff"}}.');
        for (const [name, patch] of Object.entries(args.channels)) {
          const stain = choose(name, view.channels.map((c) => c.name), 'stain');
          const c = view.channels.find((x) => x.name === stain);
          if (patch.visible !== undefined) c.visible = Boolean(patch.visible);
          if (patch.color !== undefined) {
            if (!/^#[0-9a-f]{6}$/i.test(String(patch.color))) throw new ActionError(`${stain}'s color is #rrggbb.`);
            c.color = String(patch.color).toLowerCase();
          }
          if (patch.window !== undefined) {
            if (!Array.isArray(patch.window) || patch.window.length !== 2 || !(finite(patch.window[1], 'window') > finite(patch.window[0], 'window'))) throw new ActionError(`${stain}'s window is [low, high], low below high.`);
            c.window = [Number(patch.window[0]), Number(patch.window[1])];
          }
          if (patch.gamma !== undefined) c.gamma = Math.min(4, Math.max(0.2, finite(patch.gamma, 'gamma')));
        }
      }
      // Cells: which segmentation, how drawn, which cell.
      if (args.segmentation !== undefined) view.segmentation = segmentationRef(dataset, args.segmentation);
      if (args.outlines !== undefined) view.cells.outlines = Boolean(args.outlines);
      if (args.nuclei !== undefined) view.cells.nuclei = Boolean(args.nuclei);
      if (args.fill !== undefined) view.cells.fill = args.fill === null || /^none$/i.test(String(args.fill)) ? null : /^clusters\b/i.test(String(args.fill)) ? clusterFill(dataset, view.segmentation, String(args.fill)) : /^labels\b/i.test(String(args.fill)) ? await labelFill(dataset, view.segmentation, String(args.fill)) : choose(args.fill, view.segmentation === 'instrument' ? FILLS : [...FILLS.filter((f) => f !== 'method'), ...SEGMENTATION_FILLS], 'fill');
      if (args.opacity !== undefined) view.cells.opacity = Math.min(1, Math.max(0, finite(args.opacity, 'opacity')));
      if (args.color_by !== undefined) {
        if (choose(args.color_by, ['gene', 'probability'], 'coloring') === 'probability') view.transcripts.colorBy = 'probability';
        else delete view.transcripts.colorBy;
      }
      if (args.cell !== undefined) view.cell = args.cell ? await cellRef(dataset, args.cell, view.segmentation) : null;
      // Post-Xenium images, by name or artifact id.
      if (args.images !== undefined) {
        if (typeof args.images !== 'object' || Array.isArray(args.images) || args.images === null) throw new ActionError('images is an object by image name or ID: {"<name>": {"visible": true, "opacity": 0.5}}.');
        const records = imagesOf(project(), dataset.id);
        if (!records.length) throw new ActionError(`${dataset.name} has no post-Xenium image: add_image adds one.`);
        view.images = (view.images ?? []).map((l) => ({ ...l, channels: l.channels.map((c) => ({ ...c })) }));
        for (const [ref, patch] of Object.entries(args.images)) {
          const rec = records.find((r) => r.id === ref) ?? records.find((r) => r.name === choose(ref, records.map((x) => x.name), 'image'));
          let layer = view.images.find((l) => l.id === rec.id);
          if (!layer) {
            layer = defaultImageLayer(await app.bundles.imageDescription(rec.id));
            view.images.push(layer);
          }
          if (patch.visible !== undefined) layer.visible = Boolean(patch.visible);
          if (patch.opacity !== undefined) layer.opacity = Math.min(1, Math.max(0, finite(patch.opacity, 'opacity')));
          if (patch.channels !== undefined) {
            const desc = await app.bundles.imageDescription(rec.id);
            const names = desc.image.channels.map((c) => c.name);
            if (desc.image.kind === 'rgb') throw new ActionError(`${rec.name} is RGB: it has no channels to set.`);
            for (const [name, cp] of Object.entries(patch.channels)) {
              const c = layer.channels[names.indexOf(choose(name, names, 'channel'))];
              if (cp.visible !== undefined) c.visible = Boolean(cp.visible);
              if (cp.color !== undefined) {
                if (!/^#[0-9a-f]{6}$/i.test(String(cp.color))) throw new ActionError(`${name}'s color is #rrggbb.`);
                c.color = String(cp.color).toLowerCase();
              }
              if (cp.window !== undefined) {
                if (!Array.isArray(cp.window) || cp.window.length !== 2 || !(finite(cp.window[1], 'window') > finite(cp.window[0], 'window'))) throw new ActionError(`${name}'s window is [low, high], low below high.`);
                c.window = [Number(cp.window[0]), Number(cp.window[1])];
              }
              if (cp.gamma !== undefined) c.gamma = Math.min(4, Math.max(0.2, finite(cp.gamma, 'gamma')));
            }
          }
        }
      }
      const shownNow = await apply(view, dataset);
      if (view.cell) await waitFor(() => shownNow.detail()?.row === view.cell.row, 30000, 'the cell to be read');
      const now = showing();
      return { message: `${dataset.name} in the Viewer${now.camera ? ` at (${now.camera.cx.toFixed(0)}, ${now.camera.cy.toFixed(0)}) µm, ${now.camera.umPerCss.toPrecision(3)} µm per pixel` : ''}${now.transcripts.genes.length ? `, ${now.transcripts.genes.join(', ')}` : ''}${missing.length ? `. Not in this dataset: ${missing.join(', ')}` : ''}.`, data: { showing: now, missing } };
    },

    async add_image(args) {
      const dataset = resolveDataset(args.dataset);
      const link = app.bundles.link(dataset.id);
      const item = link?.item ?? app.bundles.itemByPath(dataset.path);
      if (!item) throw new ActionError(`${dataset.name}'s bundle was not found where the project records it.`);
      if (args.path === undefined) throw new ActionError('Name the image: path, an OME-TIFF on this computer.');
      let alignment = args.alignment === undefined || args.alignment === null ? '' : String(args.alignment);
      try {
        if (!alignment && args.alignment === undefined) {
          const checked = await app.bundles.checkImage(item, String(args.path));
          alignment = checked.siblings?.[0] ?? '';
        }
        const manifest = await app.bundles.addImage(item, dataset.id, { path: String(args.path), alignment, ...(args.name ? { name: String(args.name) } : {}) });
        const record = imagesOf(project(), dataset.id).find((r) => r.id === manifest.id);
        const placed = manifest.transform ? `Placed by ${manifest.transform.name}: ${describePlacement(manifest.transform.summary)}.` : 'Not aligned: align_image places it by landmarks.';
        return {
          message: `${manifest.name} added to ${dataset.name}: ${record ? describeImage(record) : ''}. ${placed}`,
          data: { id: manifest.id, name: manifest.name, image: record?.image, transform: manifest.transform ? { id: manifest.transform.id, name: manifest.transform.name, summary: manifest.transform.summary } : null, summary: manifest.summary, findings: manifest.findings },
        };
      } catch (error) {
        throw new ActionError(error.message);
      }
    },

    async align_image(args) {
      const dataset = resolveDataset(args.dataset);
      const record = imageRef(dataset, args.image);
      if (!Array.isArray(args.landmarks)) throw new ActionError('landmarks is a list of {"fixed": [x, y] (µm), "moving": [x, y] (the image\'s pixels)}.');
      const landmarks = args.landmarks.map((l, i) => {
        const pair = (v, what) => {
          if (!Array.isArray(v) || v.length !== 2) throw new ActionError(`Landmark ${i + 1}'s ${what} is [x, y].`);
          return [finite(v[0], `landmark ${i + 1}'s ${what}`), finite(v[1], `landmark ${i + 1}'s ${what}`)];
        };
        return { fixed: pair(l?.fixed, 'fixed position'), moving: pair(l?.moving, 'moving position') };
      });
      const model = args.model === undefined ? 'similarity' : choose(args.model, ['similarity', 'affine'], 'model');
      const parent = args.parent === undefined ? '' : transformRef(record, args.parent).id;
      const use = args.use === undefined ? true : Boolean(args.use);
      const by = args.by ? String(args.by) : author;
      const note = args.note ? String(args.note) : '';
      try {
        const answer = await app.bundles.makeTransform(dataset.id, record.id, { landmarks, model, parent, by, note }, { use, rationale: note, by });
        const fit = answer.transform.fit;
        return {
          message: `${answer.manifest.name}: residuals ${fit.rms.toFixed(3)} µm root-mean-square (at most ${fit.max.toFixed(3)})${fit.leaveOneOut ? `, left out ${fit.looRms.toFixed(3)} µm (at most ${fit.looMax.toFixed(3)})` : ''}. ${use ? `It places ${record.name}.` : 'Saved as a version; use_transform chooses it.'}`,
          data: { id: answer.manifest.id, name: answer.manifest.name, transform: answer.transform.transform, model: answer.transform.model, fit, placedBy: transformInUse(project(), record.id)?.record.id ?? null },
        };
      } catch (error) {
        throw new ActionError(error.message);
      }
    },

    async register_image(args) {
      const dataset = resolveDataset(args.dataset);
      const record = imageRef(dataset, args.image);
      const item = bundleOf(dataset);
      const model = args.model === undefined ? 'similarity' : choose(args.model, ['similarity', 'affine'], 'model');
      const by = args.by ? String(args.by) : author;
      const note = args.note ? String(args.note) : '';
      const request = { model, nonrigid: Boolean(args.nonrigid), by, note };
      if (args.start !== undefined) request.start = transformRef(record, args.start).id;
      if (args.channel !== undefined) {
        const desc = await app.bundles.imageDescription(record.id);
        if (desc.image.kind === 'rgb') throw new ActionError(`${record.name} is RGB: its haematoxylin is registered, it has no channel to name.`);
        const names = desc.image.channels.map((c) => c.name);
        request.channel = names.indexOf(choose(String(args.channel), names, 'channel'));
      }
      const use = args.use === undefined ? true : Boolean(args.use);
      try {
        const m = await app.bundles.registerImage(item, dataset.id, record.id, request, { use, rationale: note || 'Aligned automatically by the nuclei.', by });
        const rec = project().artifacts.find((a) => a.kind === 'transform' && a.id === m.id);
        const d = await app.bundles.transformDescription(m.id);
        return {
          message: `${m.name}: ${rec ? describeTransform(rec) : ''}. ${use ? `It places ${record.name}.` : 'Saved as a version; use_transform chooses it.'}${d.registration?.warnings?.length ? ` ${d.registration.warnings.join(' ')}` : ''}`,
          data: { id: m.id, name: m.name, transform: d.transform, model: d.model, deformed: Boolean(d.deformation), fit: d.fit, registration: d.registration, matches: (d.matches ?? []).length, kept: (d.matches ?? []).filter((x) => x.kept).length, placedBy: transformInUse(project(), record.id)?.record.id ?? null },
        };
      } catch (error) {
        throw new ActionError(error.message);
      }
    },

    async use_transform(args) {
      const dataset = resolveDataset(args.dataset);
      const record = imageRef(dataset, args.image);
      const tf = transformRef(record, args.transform);
      const by = args.by ? String(args.by) : author;
      app.store.commit(useTransform(project(), { transform: tf.id, rationale: args.rationale ? String(args.rationale) : '', by }), `Place ${record.name} by ${tf.name}`);
      return { message: `${record.name} is placed by ${tf.name}: ${describePlacement(tf.summary)}.`, data: { image: record.id, transform: tf.id } };
    },

    async show_alignment(args) {
      const dataset = resolveDataset(args.dataset);
      const record = imageRef(dataset, args.image);
      const tf = args.transform === undefined ? null : transformRef(record, args.transform);
      bundleOf(dataset);
      await app.showAlignment(dataset.id, record.id, { transform: tf?.id ?? null });
      const hook = await waitFor(() => store.ui.mode === 'alignment' && window.situcraftAlignment?.image === record.id && window.situcraftAlignment, 10 * 60000, `${record.name} to open in the Alignment view`);
      if (tf) await waitFor(() => hook.state().parent === tf.id || !['landmarks', 'keypoints'].includes(tf.summary?.method), 30000, 'its landmarks to load');
      await hook.settled();
      await frames();
      const st = hook.state();
      const inUse = transformInUse(project(), record.id);
      return {
        message: `${record.name} in the Alignment view: ${plural(st.landmarks.length, 'landmark')}${st.fit ? `, residuals ${st.fit.rms.toFixed(3)} µm root-mean-square` : ''}; ${inUse ? `placed by ${inUse.record.name}` : 'not aligned'}.`,
        data: { ...st, versions: transformsOf(project(), record.id).map((x) => ({ id: x.id, name: x.name, how: describeTransform(x), parent: x.parent, inUse: inUse?.record.id === x.id })) },
      };
    },

    async select_cell(args) {
      const dataset = resolveDataset(args.dataset);
      const v = await openIn('viewer', dataset);
      if (!args.cell) {
        await v.select(-1);
        return { message: 'No cell selected.' };
      }
      await v.segmentationReady?.();
      const drawn = v.segmentation?.() ?? null;
      const ref = await cellRef(dataset, args.cell, drawn ?? 'instrument');
      await v.select(ref.row);
      const d = await waitFor(() => (v.detail()?.row === ref.row && v.detail()?.id === ref.id ? v.detail() : null), 30000, 'the cell to be read');
      if (args.center !== false) {
        const cam = v.renderer.camera;
        v.setCamera({ ...cam, cx: d.x, cy: d.y });
        await v.settled();
      }
      await frames();
      // A cell of a segmentation other than the instrument's.
      if (d.segmentation) {
        return {
          message: `Cell ${d.id} of ${d.segmentation.name}: ${d.transcripts} gene transcripts (quality 20 or more) of ${d.genesDetected} genes${d.instrument ? `, from the instrument's cell ${d.instrument.id}` : ''}${d.limitations.length ? `; flagged: ${d.limitations.join(', ')}` : ''}.`,
          data: { id: d.id, row: d.row, segmentation: d.segmentation, x: d.x, y: d.y, area: d.area, nucleusArea: d.nucleusArea, instrument: d.instrument, transcripts: d.transcripts, assigned: d.assigned, genesDetected: d.genesDetected, genes: d.genes.map((g) => ({ gene: g.name, count: g.count })), limitations: d.limitations, boundaryCertainty: d.boundaryCertainty, meanProbability: d.meanProbability },
        };
      }
      return {
        message: `Cell ${d.id}: ${d.counted.q20Genes} gene transcripts (quality 20 or more) of ${d.genesDetected} genes${d.method ? `, ${d.method.replace(/^Segmented /, 'segmented ')}` : ''}.`,
        data: { id: d.id, row: d.row, x: d.x, y: d.y, area: d.area ?? null, nucleusArea: d.nucleusArea ?? null, method: d.method ?? null, transcripts: d.counted, genesDetected: d.genesDetected, genes: d.genes.map((g) => ({ gene: g.name, count: g.count })), recorded: d.recorded },
      };
    },

    async save_view(args) {
      const name = String(args.name ?? '').trim();
      if (!name) throw new ActionError('Give the view a name.');
      const mode = store.ui.mode;
      const hook = hooks(mode);
      if (!hook?.saveView) throw new ActionError('Only the Viewer and the QC view have views to save: show_tissue or set_qc_map shows one first.');
      await hook.settled?.();
      const view = hook.saveView(name);
      return { message: `Saved ${name} (${mode === 'qc' ? 'QC' : 'Viewer'}); its link reopens it.`, data: viewData(view) };
    },

    async open_view(args) {
      const view = resolveView(args.view);
      const dataset = project().datasets.find((d) => d.id === view.dataset);
      if (!dataset) throw new ActionError(`${view.name} is of a dataset the project no longer has.`);
      bundleOf(dataset);
      const hook = await apply(view, dataset);
      if (view.cell) await waitFor(() => hook.detail?.()?.row === view.cell.row, 30000, 'the view\'s cell to be read');
      const now = hook.captureView(view.name);
      return { message: `Opened ${view.name}${sameView(now, view) ? ', exactly as saved' : ', but what it shows differs from what it recorded (the dataset changed)'}.`, data: { ...viewData(view), exact: sameView(now, view) } };
    },

    async remove_view(args) {
      const view = resolveView(args.view);
      store.commit(removeView(project(), view.id), `Remove view ${view.name}`);
      return { message: `Removed ${view.name} (undo brings it back).` };
    },

    async qc_metrics(args) {
      const dataset = resolveDataset(args.dataset);
      const q = await openIn('qc', dataset);
      const rows = q.metricRows();
      const checked = rows.filter((r) => r.agrees !== null);
      const agree = checked.filter((r) => r.agrees);
      return {
        message: `${dataset.name}: ${plural(rows.length, 'recorded metric')}; SituCraft recomputes ${checked.length} from the data, and ${agree.length === checked.length ? 'all agree' : `${agree.length} agree (differing: ${checked.filter((r) => !r.agrees).map((r) => r.name).join(', ')})`}.`,
        data: { metrics: rows.map((r) => ({ name: r.name, label: r.label, unit: r.unit, recorded: r.recorded, computed: r.computed, agrees: r.agrees, measures: r.help, how: r.how, ...(r.why ? { notRecomputed: r.why } : {}) })) },
      };
    },

    async set_qc_map(args) {
      const dataset = resolveDataset(args.dataset);
      const q = await openIn('qc', dataset);
      const maps = q.maps();
      const ids = maps.map((m) => m.id);
      const key = ids.includes(args.map) ? args.map : maps.find((m) => m.label === choose(args.map, maps.map((x) => x.label), 'map'))?.id;
      const view = { ...q.captureView(''), id: `remote-${(counter += 1)}` };
      view.qc = { map: key, filter: q.filter() };
      if (args.filter !== undefined) view.qc.filter = args.filter === null || /^none$/i.test(String(args.filter)) ? null : resolveFilter(args.filter, dataset).id;
      if (args.center !== undefined || args.um_per_pixel !== undefined) {
        const center = args.center ?? [view.camera.cx, view.camera.cy];
        view.camera = { cx: finite(center[0], 'center x'), cy: finite(center[1], 'center y'), umPerCss: args.um_per_pixel !== undefined ? finite(args.um_per_pixel, 'um_per_pixel') : view.camera.umPerCss };
      }
      await apply(view, dataset);
      const filter = view.qc.filter ? project().artifacts.find((a) => a.id === view.qc.filter) : null;
      return { message: `${dataset.name}: ${maps.find((m) => m.id === key).label}${filter ? `, with ${filter.name} v${filter.version} in use` : ''}.`, data: { map: key, filter: filter ? filterData(filter) : null } };
    },

    async select_cells(args) {
      const dataset = resolveDataset(args.dataset);
      if (args.clear) {
        app.selection = null;
        store.notify(['selection']);
        return { message: 'No cells chosen.' };
      }
      const q = await openIn('qc', dataset);
      const numeric = ATTRIBUTES.filter((a) => !a.categorical);
      const id = numeric.find((a) => a.id === args.attribute)?.id ?? numeric.find((a) => a.label === choose(args.attribute, numeric.map((x) => x.label), 'attribute'))?.id;
      const lo = args.min === undefined || args.min === null ? -Infinity : finite(args.min, 'min');
      const hi = args.max === undefined || args.max === null ? Infinity : finite(args.max, 'max');
      if (lo > hi) throw new ActionError('min is above max.');
      const count = q.brush(id, lo, hi);
      await frames();
      return { message: `Chose ${plural(count, 'cell')} of ${dataset.name} by ${id}${Number.isFinite(lo) ? ` from ${lo}` : ''}${Number.isFinite(hi) ? ` to ${hi}` : ''}; the QC view, the Viewer and the inspector show them.`, data: { attribute: id, min: Number.isFinite(lo) ? lo : null, max: Number.isFinite(hi) ? hi : null, count } };
    },

    async save_filter(args) {
      const dataset = resolveDataset(args.dataset);
      const name = String(args.name ?? '').trim();
      if (!name) throw new ActionError('Give the filter a name.');
      if (!Array.isArray(args.conditions) || !args.conditions.length) throw new ActionError('conditions is a list: [{"attribute": "transcripts", "min": 20}, …].');
      const q = await openIn('qc', dataset);
      const methods = q.desc.cells?.methods ?? [];
      const conditions = args.conditions.map((c) => {
        const attribute = ATTRIBUTES.find((a) => a.id === c?.attribute) ?? ATTRIBUTES.find((a) => a.label === choose(c?.attribute, ATTRIBUTES.map((x) => x.label), 'attribute'));
        if (attribute.categorical) {
          if (!Array.isArray(c.values) || !c.values.length) throw new ActionError(`The ${attribute.id} condition lists its values: segmentation methods by name or number (from 0): ${methods.map((m, i) => `${i} ${m}`).join('; ')}.`);
          return { attribute: attribute.id, values: c.values.map((v) => (Number.isInteger(v) && v >= 0 && v < methods.length ? v : methods.indexOf(choose(v, methods, 'segmentation method')))) };
        }
        const min = c.min === undefined || c.min === null ? null : finite(c.min, `${attribute.id}'s min`);
        const max = c.max === undefined || c.max === null ? null : finite(c.max, `${attribute.id}'s max`);
        if (min === null && max === null) throw new ActionError(`The ${attribute.id} condition needs a min, a max or both.`);
        if (min !== null && max !== null && min > max) throw new ActionError(`The ${attribute.id} condition's min is above its max.`);
        return { attribute: attribute.id, min, max };
      });
      const filter = q.saveFilter(name, conditions);
      await frames();
      return { message: `Saved ${filter.name}, version ${filter.version}: it keeps ${plural(filter.result.kept, 'cell')} of ${filter.result.cells.toLocaleString('en-US')}.`, data: filterData(filter) };
    },

    async segmentations(args) {
      const p = project();
      const datasets = args.dataset ? [resolveDataset(args.dataset)] : p.datasets;
      const list = datasets.flatMap((d) => p.artifacts.filter((a) => a.kind === 'segmentation' && a.dataset === d.id).map((a) => {
        const promoted = promotedSegmentation(p, d.id);
        return {
          id: a.id, name: a.name, dataset: d.name, state: a.state, source: a.source,
          default: promoted?.record.id === a.id, ...(promoted?.record.id === a.id ? { decision: { by: promoted.decision.by, reason: promoted.decision.rationale, time: promoted.decision.time } } : {}),
          method: a.method, software: a.software, job: a.job, parents: a.parents, parameters: a.parameters, summary: a.summary, files: a.files, findings: a.findings,
        };
      }));
      const registering = datasets.filter((d) => app.bundles.registeringSegmentation?.(d.id)).map((d) => d.name);
      if (args.show && list.length) {
        app.focusItem({ kind: 'segmentation', id: list[0].id });
        await frames();
      }
      return { message: `${plural(list.length, 'segmentation')}${list.length ? `: ${list.map((x) => `${x.name} of ${x.dataset} (${x.state}${x.default ? ', default' : ''})`).join('; ')}` : ''}${registering.length ? `; being registered: ${registering.join(', ')}` : ''}.`, data: { segmentations: list, registering } };
    },

    async expand_nuclei(args) {
      const dataset = resolveDataset(args.dataset);
      const distance = args.distance_um === undefined ? 5 : finite(args.distance_um, 'distance_um');
      if (distance < 0 || distance > 100) throw new ActionError('distance_um is from 0 to 100.');
      await settleMode('segmentation');
      let manifest;
      try {
        manifest = await app.segmentationView.expand(dataset, distance);
      } catch (error) {
        throw new ActionError(error.message);
      }
      await frames();
      const s = manifest.summary ?? {};
      const g = s.agreement;
      const pc = (v) => `${(100 * v).toFixed(2)}%`;
      return {
        message: `${manifest.name} of ${dataset.name} (${manifest.state}): ${plural(s.cells ?? 0, 'cell')}, ${pc(s.assignedFraction ?? 0)} of gene transcripts assigned${g ? `; the same cells as the instrument's segmentation for ${pc(g.pixels.agreement)} of pixels and ${pc(g.transcripts.agreement)} of transcripts` : ''}.`,
        data: { id: manifest.id, name: manifest.name, dataset: dataset.name, state: manifest.state, parameters: manifest.lineage?.parameters ?? null, summary: s, findings: manifest.findings ?? [] },
      };
    },

    async refine_segmentation(args) {
      const dataset = resolveDataset(args.dataset);
      const request = refineRequest(args);
      await settleMode('segmentation');
      let answer;
      try {
        answer = await app.segmentationView.refine(dataset, request);
      } catch (error) {
        const remedy = error.job?.failure?.remedy;
        throw new ActionError(`${error.message}${remedy ? ` What to do: ${remedy}` : ''}`);
      }
      await frames();
      const m = answer.manifest;
      const s = m.summary ?? {};
      const d = s.diagnostics ?? {};
      const c = answer.comparison;
      const pc = (v) => `${(100 * v).toFixed(2)}%`;
      return {
        message: `${m.name} of ${dataset.name} (${m.state}): ${plural(s.cells ?? 0, 'cell')}${d.patches ? ` (refined in ${d.patches.toLocaleString('en-US')} patches)` : ''}${d.cellsHeld ? ` (${plural(d.cellsRefined ?? 0, 'cell')} refined, ${d.cellsHeld} held about ${plural(d.regions ?? 1, 'region')})` : ''}, ${pc(s.assignedFraction ?? 0)} of gene transcripts assigned; Geweke's z ${(d.gewekeZ ?? 0).toFixed(1)}${d.gewekeJudged === false ? ' (too few sweeps summarized to judge)' : ''}; ${pc(d.splitHalfAgreement ?? 0)} of transcripts in the same cell over both halves of the last ${d.samples ?? 0} sweeps${Number.isFinite(d.seedAgreement) ? `; a second seed agreed on ${pc(d.seedAgreement)}` : ''}${c ? `; compared with the instrument's segmentation, ${pc(c.summary?.changedFraction ?? 0)} of gene transcripts changed cell` : ''}.`,
        data: { id: m.id, name: m.name, dataset: dataset.name, state: m.state, parameters: m.lineage?.parameters ?? null, summary: s, findings: m.findings ?? [], comparison: c ? { id: c.id, summary: c.summary ?? null } : null },
      };
    },

    async refinement_preflight(args) {
      const dataset = resolveDataset(args.dataset);
      const request = refineRequest(args);
      await settleMode('segmentation');
      let answer;
      try {
        answer = await app.segmentationView.refinementPreflight(dataset, request);
      } catch (error) {
        throw new ActionError(error.message);
      }
      const p = answer.preflight;
      const e = p.estimate ?? {};
      const size = (n) => (n >= 1e9 ? `${(n / 1e9).toFixed(1)} GB` : `${Math.round(n / 1e6)} MB`);
      const covers = e.regions ? `refines ${plural(e.cells, 'cell')}${e.cellsHeld ? ` (${e.cellsHeld} held about them)` : ''} in ${p.patches?.length ? `${e.regions.toLocaleString('en-US')} patches` : plural(e.regions, 'region')}, ${plural(e.transcripts, 'transcript')} modelled; needs about ${size(e.peakMemoryBytes)} of memory${p.memoryBytes ? ` (the computer has ${size(p.memoryBytes)})` : ''}, ${size(e.diskBytes)} of disk${p.freeDiskBytes >= 0 ? ` (${size(p.freeDiskBytes)} free)` : ''} and about ${Math.max(1, Math.round(e.seconds))} s on ${e.cores} threads` : 'covers nothing yet';
      const problems = p.problems.map((f) => `${f.message}${f.remedy ? ` ${f.remedy}` : ''}`);
      return {
        message: `${answer.name ?? 'The refinement'} of ${dataset.name} (${p.spec.preset}) ${covers}.${problems.length ? ` Problems: ${problems.join(' ')}` : ' No problems.'}${p.risks.length ? ` Risks: ${p.risks.map((f) => f.message).join(' ')}` : ''}${answer.artifact ? ' It was made already.' : ''}`,
        data: { id: answer.id ?? null, name: answer.name, dataset: dataset.name, made: Boolean(answer.artifact), preflight: p, recipe: answer.recipe },
      };
    },

    async import_segmentation(args) {
      const dataset = resolveDataset(args.dataset);
      const format = choose(args.format, ['spatialdata', 'baysor', 'geojson', 'xenium', 'situcraft'], 'format');
      if (!Array.isArray(args.paths) || !args.paths.length || args.paths.some((p) => typeof p !== 'string' || !p.trim())) throw new ActionError('paths is a list of one or two files.');
      const options = { format, paths: args.paths.map((p) => p.trim()) };
      if (args.units !== undefined) options.units = choose(args.units, ['um', 'pixels'], 'units');
      for (const key of ['coordinate_system', 'cells', 'points', 'table', 'cell_column', 'cell_property', 'tool', 'tool_version', 'citation']) {
        if (args[key] !== undefined && String(args[key]).trim()) options[key] = String(args[key]).trim();
      }
      await settleMode('segmentation');
      let manifest;
      try {
        manifest = await app.segmentationView.importSegmentation(dataset, options);
      } catch (error) {
        throw new ActionError(error.message);
      }
      await frames();
      const s = manifest.summary ?? {};
      const fatal = (manifest.findings ?? []).filter((f) => f.severity === 'fatal');
      return {
        message: `${manifest.name} into ${dataset.name} (${manifest.state}): ${plural(s.cells ?? 0, 'cell')}, ${((s.assignedFraction ?? 0) * 100).toFixed(2)}% of gene transcripts assigned${fatal.length ? `; ${fatal.map((f) => f.message).join(' ')}` : ''}.`,
        data: { id: manifest.id, name: manifest.name, dataset: dataset.name, state: manifest.state, method: manifest.lineage?.method ?? null, parameters: manifest.lineage?.parameters ?? null, summary: s, findings: manifest.findings ?? [] },
      };
    },

    async plant_errors(args) {
      const dataset = resolveDataset(args.dataset);
      const params = {};
      for (const key of ['merges', 'splits', 'missed', 'grown', 'seed']) {
        if (args[key] === undefined) continue;
        const v = finite(args[key], key);
        if (!Number.isInteger(v) || v < 0) throw new ActionError(`${key} is a whole number, 0 or more.`);
        params[key] = v;
      }
      if (args.grow_um !== undefined) params.grow_um = finite(args.grow_um, 'grow_um');
      await settleMode('segmentation');
      let manifest;
      try {
        manifest = await app.segmentationView.plant(dataset, params);
      } catch (error) {
        throw new ActionError(error.message);
      }
      await frames();
      const p = manifest.lineage?.parameters ?? {};
      const data = { id: manifest.id, name: manifest.name, dataset: dataset.name, state: manifest.state, parameters: p, summary: manifest.summary ?? {}, findings: manifest.findings ?? [] };
      if (args.reveal) data.errors = (await app.bundles.plantedErrors(manifest.id)).errors;
      return {
        message: `${manifest.name} in ${dataset.name}'s segmentation (${manifest.state}): ${p.merges} merges, ${p.splits} splits, ${p.missed} missed cells and ${p.grown} drawn ${p.grow_um} µm too wide, in ${plural(manifest.summary?.cells ?? 0, 'cell')}. Compare it with the instrument's to find them.`,
        data,
      };
    },

    async cluster_cells(args) {
      const dataset = resolveDataset(args.dataset);
      const segs = project().artifacts.filter((a) => a.kind === 'segmentation' && a.dataset === dataset.id && a.state === 'complete' && a.source !== 'planted');
      if (!segs.length) throw new ActionError(`${dataset.name} has no complete segmentation yet.`);
      let seg = segs.find((a) => a.id === promotedSegmentation(project(), dataset.id)?.record.id) ?? segs[0];
      if (args.segmentation !== undefined) {
        const ref = String(args.segmentation);
        seg = segs.find((a) => a.id === ref) ?? segs.find((a) => a.name === choose(ref, segs.map((x) => x.name), 'segmentation'));
      }
      const parameters = {};
      for (const [arg, key] of [['min_transcripts', 'minTranscripts'], ['components', 'components'], ['neighbors', 'neighbors'], ['resolution', 'resolution'], ['seed', 'seed']]) {
        if (args[arg] !== undefined) parameters[key] = finite(args[arg], arg);
      }
      let filterId;
      if (args.filter !== undefined) {
        if (seg.source !== 'instrument') throw new ActionError('A QC filter chooses cells of the instrument\'s segmentation.');
        const filters = project().artifacts.filter((a) => a.kind === 'filter' && a.dataset === dataset.id);
        const named = filters.map((f) => `${f.name} v${f.version}`);
        const ref = String(args.filter);
        const byId = filters.find((f) => f.id === ref);
        const latest = (name) => filters.filter((f) => f.name === name).sort((a, b) => b.version - a.version)[0];
        filterId = byId?.id ?? (named.includes(ref) ? filters[named.indexOf(ref)].id : latest(choose(ref, [...new Set(filters.map((f) => f.name))], 'filter'))?.id);
      }
      await settleMode('celltypes');
      let manifest;
      try {
        manifest = await app.cellTypesView.cluster(dataset, { segmentation: seg.id, parameters, filterId });
      } catch (error) {
        throw new ActionError(error.message);
      }
      await frames();
      const { description: d } = await app.bundles.clusteringData(manifest.id);
      return {
        message: `${seg.name} of ${dataset.name}: ${plural(d.clusters.count, 'cluster')} of ${plural(d.cells.analyzed, 'cell')} (resolution ${d.parameters.resolution}, modularity ${d.clusters.modularity.toFixed(4)}); the largest's leading markers ${(d.top[0] ?? []).slice(0, 3).map((m) => m.gene).join(', ')}.`,
        data: { id: manifest.id, segmentation: { id: seg.id, name: seg.name }, parameters: d.parameters, cells: d.cells, genes: d.genes, components: d.pca.components, varianceRatio: d.pca.varianceRatio, ...(d.pca.solver ? { solver: d.pca.solver } : {}), edges: d.graph.edges, ...(d.graph.search ? { search: d.graph.search } : {}), clusters: d.clusters, markers: d.top.map((list) => list.slice(0, 5).map(({ gene, logfc, pctIn, pctOut, padj }) => ({ gene, logfc, pctIn, pctOut, padj }))) },
      };
    },

    async show_map(args) {
      const dataset = resolveDataset(args.dataset);
      const list = clusteringsOf(project(), dataset.id).filter((c) => c.state === 'complete');
      if (!list.length) throw new ActionError(`${dataset.name} has no clustering: cluster_cells makes one.`);
      const c = clusteringRef(list, args.clustering);
      await settleMode('celltypes');
      let map;
      try {
        map = await app.cellTypesView.show(dataset, c.id);
      } catch (error) {
        throw new ActionError(error.message);
      }
      if (!map) throw new ActionError('The map could not be shown.');
      const maps = mapsOf(project(), dataset.id, c.segmentation);
      if (args.map !== undefined) {
        const ref = String(args.map);
        const m = /^instrument/i.test(ref) ? maps.find((x) => x.name === 'The instrument\'s map') : maps.find((x) => x.id === ref);
        if (!m) throw new ActionError(`No map ${ref} of ${dataset.name}'s cells; its maps are ${maps.map((x) => `${x.id} (${x.name})`).join(', ') || 'none'}.`);
        await map.setMap(m.id);
      }
      if (args.color_by !== undefined) {
        const v = String(args.color_by);
        if (/^(labels?|confidence)(:|$)/i.test(v)) await map.setColor(await labelColor(dataset, c, v));
        else if (/^clusters?$/i.test(v)) await map.setColor({ kind: 'clusters', clustering: c.id });
        else if (/^clusters:/i.test(v)) await map.setColor({ kind: 'clusters', clustering: clusteringRef(list, v.slice(v.indexOf(':') + 1)).id });
        else if (/^reliability$/i.test(v)) await map.setColor({ kind: 'reliability' });
        else if (/^(transcripts|area)$/i.test(v)) await map.setColor({ kind: 'attribute', attribute: v.toLowerCase() });
        else await map.setColor({ kind: 'gene', gene: v });
      }
      if (args.select !== undefined) {
        const sel = args.select;
        if (sel === null || sel === 'none') map.select(null);
        else if (Array.isArray(sel.clusters)) map.selectClusters(sel.clusters.map((k) => finite(k, 'cluster') - 1));
        else if (Array.isArray(sel.polygon) && sel.polygon.length >= 3) {
          const polygon = sel.polygon.map(([x, y]) => [finite(x, 'x'), finite(y, 'y')]);
          if (/^tissue$/i.test(String(sel.on ?? 'map'))) map.selectOnTissue(polygon);
          else map.selectOnMap(polygon);
        } else throw new ActionError('select is {"clusters": [numbers from 1]}, {"polygon": [[x, y], …], "on": "map" | "tissue"} or "none".');
      }
      await frames();
      const st = map.state;
      const composition = {};
      if (st.count && st.groups && st.color.kind === 'labels') {
        const { description } = await app.bundles.labelData(st.color.labels);
        for (let i = 0; i < st.groups.length; i += 1) {
          if (st.selected[i] && st.groups[i] >= 0) {
            const key = description.sets[st.groups[i]]?.name ?? 'No label';
            composition[key] = (composition[key] ?? 0) + 1;
          }
        }
      } else if (st.count && st.clusters) {
        // By the clusters coloring the cells, else the clustering shown.
        const byColor = st.color.kind === 'clusters' && st.groups;
        const of = byColor ? st.groups : st.clusters;
        const names = clusterNames(project(), byColor ? st.color.clustering : c.id);
        for (let i = 0; i < of.length; i += 1) {
          if (st.selected[i] && of[i] >= 0) {
            const key = names?.names?.[of[i]] || clusterName(of[i]);
            composition[key] = (composition[key] ?? 0) + 1;
          }
        }
      }
      const shownMap = maps.find((m) => m.id === st.mapId);
      return {
        message: `${c.name} on ${shownMap?.name.toLowerCase() ?? 'no map'} and the tissue${st.count ? `: ${plural(st.count, 'cell')} selected` : ''}.`,
        data: { clustering: c.id, map: st.mapId, trustworthiness: shownMap?.summary?.trustworthiness ?? null, color: st.color, selected: st.count, composition },
      };
    },

    async name_clusters(args) {
      const dataset = resolveDataset(args.dataset);
      const list = clusteringsOf(project(), dataset.id).filter((c) => c.state === 'complete');
      if (!list.length) throw new ActionError(`${dataset.name} has no clustering.`);
      const c = clusteringRef(list, args.clustering);
      if (!args.names || typeof args.names !== 'object' || Array.isArray(args.names)) throw new ActionError('names is an object by cluster number (from 1): {"1": "T cells"}; an empty name removes one.');
      const current = clusterNames(project(), c.id)?.names ?? [];
      const names = Array.from({ length: c.summary?.clusters ?? 0 }, (_, k) => current[k] ?? null);
      for (const [key, value] of Object.entries(args.names)) {
        const k = Number(key) - 1;
        if (!Number.isInteger(k) || k < 0 || k >= names.length) throw new ActionError(`${c.name} has clusters 1 to ${names.length}; ${key} is not one.`);
        names[k] = value === null ? null : String(value);
      }
      const next = nameClusters(project(), { clustering: c.id, names, by: 'remote control' });
      if (next === project()) return { message: 'The names are as they were.', data: { version: clusterNames(project(), c.id)?.version ?? 0 } };
      store.commit(next, `Name the clusters of ${c.name}`);
      const version = clusterNames(project(), c.id);
      return { message: `${c.name}: names version ${version.version}.`, data: { version: version.version, names: version.names } };
    },

    async label_cells(args) {
      const dataset = resolveDataset(args.dataset);
      const list = clusteringsOf(project(), dataset.id).filter((c) => c.state === 'complete');
      if (!list.length) throw new ActionError(`${dataset.name} has no clustering: cluster_cells makes one.`);
      const c = clusteringRef(list, args.clustering);
      const parameters = {};
      for (const key of ['margin', 'minimum', 'state', 'bins', 'controls', 'seed']) if (args[key] !== undefined) parameters[key] = finite(args[key], key);
      if (args.lineage !== undefined) parameters.lineage = Boolean(args.lineage);
      if (args.set !== undefined && (typeof args.set !== 'object' || Array.isArray(args.set) || args.set === null)) throw new ActionError('set is a marker set in full: {"format": "situcraft-markers", "version": 1, "name": …, "levels": […], "labels": […]}.');
      await settleMode('celltypes');
      let manifest;
      try {
        manifest = await app.cellTypesView.label(dataset, c.id, { markers: args.markers, set: args.set, parameters, by: 'remote control' });
      } catch (error) {
        throw new ActionError(error.missing ? `The panel lacks some of the set's markers: ${Object.entries(error.missing).map(([l, g]) => `${l} (${g.join(', ')})`).join('; ')}.` : error.message);
      }
      await frames();
      const { description: d } = await app.bundles.labelData(manifest.id);
      const deepest = d.levels.length - 1;
      const names = clusterNames(project(), c.id);
      const clusters = labelComposition(d, deepest).map((row) => ({ cluster: row.cluster + 1, name: names?.names?.[row.cluster] ?? null, cells: row.cells, labels: row.labels.slice(0, 3).map((e) => ({ label: e.name, share: Number(e.share.toFixed(4)) })) }));
      let named = null;
      if (args.name_clusters !== undefined && args.name_clusters !== false) {
        const share = args.name_clusters === true ? 0.5 : finite(args.name_clusters, 'name_clusters');
        if (share < 0.3 || share > 1) throw new ActionError('name_clusters is the share of a cluster\'s cells its label must hold, from 0.3 to 1 (or true for 0.5).');
        const proposal = namesFromLabels(d, share);
        const merged = proposal.map((p, k) => p ?? names?.names?.[k] ?? null);
        const next = nameClusters(project(), { clustering: c.id, names: merged, by: 'remote control' });
        if (next !== project()) store.commit(next, `Name the clusters of ${c.name} from ${d.markers.name}`);
        named = { version: clusterNames(project(), c.id)?.version ?? 0, names: proposal };
      }
      const pct = (v) => `${Math.round((100 * v) / d.cells.analyzed)}%`;
      return {
        message: `${c.name} labeled by ${d.markers.name}: ${d.levels.map((level, l) => `${level.toLowerCase()} ${pct(d.cells.labeled[l])}`).join(', ')} of ${plural(d.cells.analyzed, 'cell')}${named ? `; ${named.names.filter(Boolean).length} clusters named from the labels (names version ${named.version})` : ''}.`,
        data: { id: manifest.id, clustering: c.id, markers: d.markers, parameters: d.parameters, levels: d.levels, cells: d.cells, labels: d.sets.map((x) => ({ name: x.name, level: x.state ? null : d.levels[x.level], state: Boolean(x.state), cells: x.cells, positive: x.positive, negative: x.negative ?? [], controls: x.positiveControls.length })), clusters, named },
      };
    },

    async reference_labels(args) {
      const dataset = resolveDataset(args.dataset);
      const list = clusteringsOf(project(), dataset.id).filter((c) => c.state === 'complete');
      if (!list.length) throw new ActionError(`${dataset.name} has no clustering: cluster_cells makes one.`);
      const c = clusteringRef(list, args.clustering);
      if (args.path === undefined) throw new ActionError('Name the reference: path, an AnnData object in Zarr or a table of label profiles on this computer.');
      const parameters = {};
      for (const key of ['tune', 'margin', 'markers']) if (args[key] !== undefined) parameters[key] = finite(args[key], key);
      await settleMode('celltypes');
      let manifest;
      try {
        manifest = await app.cellTypesView.reference(dataset, c.id, { path: String(args.path), matrix: args.matrix === undefined ? '' : String(args.matrix), labelColumn: args.label_column === undefined ? '' : String(args.label_column), geneColumn: args.gene_column === undefined ? '' : String(args.gene_column), parameters });
      } catch (error) {
        throw new ActionError(error.message);
      }
      await frames();
      const { description: d } = await app.bundles.labelData(manifest.id);
      const ref = d.reference;
      const names = clusterNames(project(), c.id);
      const clusters = labelComposition(d, 0).map((row) => ({ cluster: row.cluster + 1, name: names?.names?.[row.cluster] ?? null, cells: row.cells, labels: row.labels.slice(0, 3).map((e) => ({ label: e.name, share: Number(e.share.toFixed(4)) })) }));
      let named = null;
      if (args.name_clusters !== undefined && args.name_clusters !== false) {
        const share = args.name_clusters === true ? 0.5 : finite(args.name_clusters, 'name_clusters');
        if (share < 0.3 || share > 1) throw new ActionError('name_clusters is the share of a cluster\'s cells its label must hold, from 0.3 to 1 (or true for 0.5).');
        const proposal = namesFromLabels(d, share);
        const merged = proposal.map((p, k) => p ?? names?.names?.[k] ?? null);
        const next = nameClusters(project(), { clustering: c.id, names: merged, by: 'remote control' });
        if (next !== project()) store.commit(next, `Name the clusters of ${c.name} from ${ref.file}`);
        named = { version: clusterNames(project(), c.id)?.version ?? 0, names: proposal };
      }
      const pct = (v) => `${Math.round((100 * v) / d.cells.analyzed)}%`;
      return {
        message: `${c.name} labeled from ${ref.file}: ${pct(d.cells.labeled[0])} of ${plural(d.cells.analyzed, 'cell')}, ${plural(d.sets.length, 'label')}; ${ref.shared.length} of the panel's genes shared, ${ref.compared.length} compared; ${plural(ref.fineTuned, 'cell')} fine-tuned${named ? `; ${named.names.filter(Boolean).length} clusters named from the labels (names version ${named.version})` : ''}.`,
        data: { id: manifest.id, clustering: c.id, reference: { file: ref.file, sha256: ref.sha256, format: ref.format, matrix: ref.matrix ?? null, labelColumn: ref.labelColumn ?? null, geneColumn: ref.geneColumn ?? null, values: ref.values, labels: ref.labels.length, shared: ref.shared.length, missing: ref.missing, compared: ref.compared.length, perPair: ref.perPair }, parameters: ref.parameters, cells: d.cells, fineTuned: ref.fineTuned, changed: ref.changed, labels: d.sets.map((x, k) => ({ name: x.name, referenceCells: ref.cells[k], cells: x.cells })), clusters, named },
      };
    },

    async import_labels(args) {
      const dataset = resolveDataset(args.dataset);
      const link = app.bundles.link(dataset.id);
      const item = link?.item ?? app.bundles.itemByPath(dataset.path);
      if (!item) throw new ActionError(`${dataset.name}'s bundle was not found where the project records it.`);
      const segs = project().artifacts.filter((a) => a.kind === 'segmentation' && a.dataset === dataset.id && a.state === 'complete' && a.source !== 'planted');
      if (!segs.length) throw new ActionError(`${dataset.name} has no complete segmentation yet.`);
      let seg = segs.find((a) => a.id === promotedSegmentation(project(), dataset.id)?.record.id) ?? segs[0];
      if (args.segmentation !== undefined) {
        const ref = String(args.segmentation);
        seg = /^(the )?instrument('s)?$/i.test(ref) ? segs.find((a) => a.source === 'instrument') : segs.find((a) => a.id === ref) ?? segs.find((a) => a.name === choose(ref, segs.map((x) => x.name), 'segmentation'));
        if (!seg) throw new ActionError(`${dataset.name} has no such segmentation.`);
      }
      if ((args.path === undefined) === (args.instrument === undefined)) throw new ActionError('Name a table (path) or one of the instrument\'s clusterings (instrument: graph-based, k-means N), not both.');
      const request = { segmentation: seg.id };
      if (args.instrument !== undefined) {
        if (seg.source !== 'instrument') throw new ActionError('The instrument\'s clusters are of its own segmentation: segmentation instrument.');
        const listed = await app.bundles.instrumentAnalysis(item);
        if (!listed.clusterings?.length) throw new ActionError(`${dataset.name} holds no analysis of the instrument's.`);
        const ref = String(args.instrument);
        const titles = listed.clusterings.map((c) => c.title);
        request.instrument = (listed.clusterings.find((c) => c.name === ref) ?? listed.clusterings.find((c) => c.title === choose(ref.replace(/^k-means (\d+)$/i, 'K-means ($1 clusters)'), titles, 'clustering'))).name;
      } else {
        request.path = String(args.path);
        let read;
        try {
          read = await app.bundles.readLabelsTable(item, { path: request.path, segmentation: seg.id, ...(args.id_column ? { columns: { idColumn: String(args.id_column) } } : {}) });
        } catch (error) {
          throw new ActionError(error.message);
        }
        const roles = read.roles;
        request.columns = {
          idColumn: String(args.id_column ?? roles.id ?? ''),
          column: String(args.column ?? roles.label ?? ''),
          confidenceColumn: String(args.confidence_column ?? roles.confidence ?? ''),
          colorColumn: String(args.color_column ?? roles.color ?? ''),
        };
        if (!request.columns.idColumn || !request.columns.column) throw new ActionError(`Name the columns: ${read.file} has ${read.columns.map((c) => c.name).join(', ')}; give id_column and column.`);
      }
      let manifest;
      try {
        manifest = await app.bundles.importLabels(item, dataset.id, request);
      } catch (error) {
        throw new ActionError(error.message);
      }
      await settleMode('celltypes');
      const record = project().artifacts.find((a) => a.kind === 'labels' && a.id === manifest.id);
      const { description: d } = await app.bundles.labelData(manifest.id);
      return {
        message: `${labelSetName(record)} imported for ${seg.name} of ${dataset.name}: ${plural(d.sets.length, 'label')}, ${plural(d.cells.labeled[0], 'cell')} of ${d.cells.analyzed.toLocaleString('en-US')} labeled.`,
        data: { id: manifest.id, segmentation: { id: seg.id, name: seg.name }, source: d.source, cells: d.cells, labels: d.sets.map((x) => ({ name: x.name, cells: x.cells, ...(x.color ? { color: x.color } : {}) })) },
      };
    },

    async review_labels(args) {
      const { dataset, seg, item, version } = labelVersionArg(args);
      const src = await labelSource(app, refOf(version.id, version.level));
      const neighbors = args.neighbors === undefined ? 10 : finite(args.neighbors, 'neighbors');
      const implausible = args.implausible === undefined ? 0.2 : finite(args.implausible, 'implausible');
      let low = args.low_confidence === undefined ? null : finite(args.low_confidence, 'low_confidence');
      if (args.low_confidence === undefined && (version.kind === 'markers' || version.kind === 'reference')) low = lowConfidenceDefault(project().artifacts.find((a) => a.id === version.id));
      const curated = version.kind === 'curated' ? project().artifacts.find((a) => a.id === version.id) : curatedSetsOf(project(), dataset.id, seg.id).find((c) => c.base.labels === version.id && c.base.level === version.level);
      const reviewed = [];
      if (curated) (await labelSource(app, refOf(curated.id))).edited.forEach((e, i) => e >= 0 && reviewed.push(i));
      const enc = (typed) => {
        const bytes = new Uint8Array(typed.buffer, typed.byteOffset, typed.byteLength);
        let t = '';
        for (let i = 0; i < bytes.length; i += 0x8000) t += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
        return btoa(t);
      };
      const body = { labels: enc(src.label), neighbors, implausible, lowConfidence: low, limit: args.limit === undefined ? 20 : finite(args.limit, 'limit'), reviewed };
      if (src.confidence) body.confidence = enc(src.confidence);
      let against = null;
      if (args.against !== undefined) {
        against = againstArg(dataset, seg, args.against);
        const other = await labelSource(app, refOf(against.version.id, against.version.level));
        const mineIsA = against.comparison.a === seg.id;
        const impact = await app.bundles.comparisonImpact(item, against.comparison.id, mineIsA ? { a: sideOf(src), b: sideOf(other) } : { a: sideOf(other), b: sideOf(src) });
        body.other = mineIsA ? impact.sensitiveA : impact.sensitiveB;
        body.others = impact.impact.others;
      }
      const response = await fetch(`${item.url}/segmentations/${encodeURIComponent(seg.id)}/review`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      const answer = await response.json();
      if (!response.ok) throw new ActionError(answer.error ?? `HTTP ${response.status}`);
      const c = answer.counts;
      return {
        message: `${version.name}: ${plural(c.flagged, 'cell')} to review of ${c.labeled.toLocaleString('en-US')} labeled (${c.implausible} implausible, ${against ? `${c.sensitive} whose label changes under ${against.segName}, ` : ''}${c.lowConfidence} of low confidence, ${c.both} for two reasons or more; ${c.reviewed} reviewed already).`,
        data: { labels: version.id, level: version.level, counts: c, neighbors, implausible, lowConfidence: low, against: against ? { labels: against.version.id, level: against.version.level, segmentation: against.segName, comparison: against.comparison.id } : null, queue: answer.queue.map((it) => ({ cell: it.cell, label: src.names[src.label[it.cell]], reasons: it.reasons, share: it.share, neighborsLabel: src.names[it.dominant] ?? null, ...(it.other ? { other: it.other } : {}), confidence: it.confidence })) },
      };
    },

    async repeat_on_segmentation(args) {
      const dataset = resolveDataset(args.dataset);
      const link = app.bundles.link(dataset.id);
      const item = link?.item ?? app.bundles.itemByPath(dataset.path);
      if (!item) throw new ActionError(`${dataset.name}'s bundle was not found where the project records it.`);
      const list = clusteringsOf(project(), dataset.id).filter((c) => c.state === 'complete' && !c.summary?.source);
      if (!list.length) throw new ActionError(`${dataset.name} has no clustering of SituCraft's: cluster_cells makes one.`);
      const c = clusteringRef(list, args.clustering);
      if (args.segmentation === undefined) throw new ActionError('Name the segmentation to make the clustering and its labels again on (segmentation).');
      const target = segmentationArg(dataset, args.segmentation);
      let out;
      try {
        out = await remakeOnSegmentation(app, { item, dataset }, c.id, target.id);
      } catch (error) {
        throw new ActionError(error.message);
      }
      const labels = [];
      for (const m of out.labels) {
        const d = (await app.bundles.labelData(m.id)).description;
        const record = project().artifacts.find((a) => a.id === m.id);
        labels.push({ id: m.id, name: record ? labelSetName(record) : m.name, levels: d.levels, cells: d.cells });
      }
      const s = out.clustering.summary ?? {};
      return {
        message: `${c.name} made again on ${target.name}: ${s.clusters ?? '?'} clusters of ${(s.cells ?? 0).toLocaleString('en-US')} cells, ${plural(labels.length, 'label set')}${labels.length ? ` (${labels.map((l) => `${l.name}: ${Math.round((100 * l.cells.labeled[0]) / Math.max(1, l.cells.analyzed))}% labeled`).join('; ')})` : ''}.${out.failed.length ? ` Not made: ${out.failed.map((f) => f.error).join('; ')}.` : ''}${out.skipped.length ? ` ${plural(out.skipped.length, 'set')} not made again (imported for the other segmentation's cells).` : ''}`,
        data: { clustering: { id: out.clustering.id, segmentation: target.id, clusters: s.clusters, cells: s.cells }, labels, failed: out.failed, skipped: out.skipped, notes: out.notes },
      };
    },

    async label_impact(args) {
      const dataset = resolveDataset(args.dataset);
      const p = project();
      const list = p.artifacts.filter((a) => a.kind === 'comparison' && a.dataset === dataset.id && a.state === 'complete');
      if (!list.length) throw new ActionError(`${dataset.name} has no comparison: compare_segmentations makes one.`);
      const segName = (id) => p.artifacts.find((a) => a.kind === 'segmentation' && a.id === id)?.name ?? id;
      const named = list.map((c) => ({ c, name: `${segName(c.a)} and ${segName(c.b)}` }));
      let cmp = list.at(-1);
      if (args.comparison !== undefined) {
        const ref = String(args.comparison);
        cmp = list.find((c) => c.id === ref) ?? named.find((x) => x.name === choose(ref, named.map((y) => y.name), 'comparison')).c;
      }
      const va = labelVersions(p, dataset.id, cmp.a);
      const vb = labelVersions(p, dataset.id, cmp.b);
      for (const [vs, id] of [[va, cmp.a], [vb, cmp.b]]) {
        if (!vs.length) throw new ActionError(`${segName(id)} has no labels: repeat_on_segmentation makes a clustering and its labels again on it.`);
      }
      const pickOf = (vs, ref, side) => {
        if (ref === undefined) return null;
        const text = String(ref);
        const [id, lvl] = /:\d+$/.test(text) ? [text.slice(0, text.lastIndexOf(':')), Number(text.slice(text.lastIndexOf(':') + 1)) - 1] : [text, 0];
        const v = vs.filter((x) => x.id === id || x.name === text)[lvl] ?? null;
        if (!v) throw new ActionError(`No label set ${text} of ${side}; its sets are ${[...new Set(vs.map((x) => `${x.id} (${x.name})`))].join(', ')}.`);
        return v;
      };
      const [da, db] = defaultPair(p, va, vb);
      const a = pickOf(va, args.labels_a, segName(cmp.a)) ?? da;
      const b = pickOf(vb, args.labels_b, segName(cmp.b)) ?? db;
      await settleMode('compare');
      const w = await app.compareView.open(cmp.id);
      if (!w) throw new ActionError('The comparison could not be opened.');
      let data;
      try {
        data = await w.showLabels({ a: `${a.id}:${a.level}`, b: `${b.id}:${b.level}`, fill: args.color === undefined ? undefined : Boolean(args.color) });
      } catch (error) {
        throw new ActionError(error.message);
      }
      await frames();
      const im = data.impact;
      return {
        message: `${a.name} (${segName(cmp.a)}) against ${b.name} (${segName(cmp.b)}): ${impactLines(im, segName(cmp.a), segName(cmp.b)).join(' ')}`,
        data: {
          comparison: cmp.id, a: { labels: a.id, level: a.level, name: a.name }, b: { labels: b.id, level: b.level, name: b.name },
          cellsA: im.cellsA, cellsB: im.cellsB, composition: im.composition, matched: { ...im.matched, transitions: im.matched.transitions.slice(0, 20) },
          split: im.split, merged: im.merged, rearranged: im.rearranged, onlyA: im.onlyA, onlyB: im.onlyB, changed: im.changed.length, sensitive: { a: im.sensitive[0], b: im.sensitive[1] },
          mixing: im.mixing ? { pairs: im.mixing.pairs, of: data.answer.pairs, allA: im.mixing.allA, allB: im.mixing.allB, a: im.mixing.a, b: im.mixing.b } : null,
        },
      };
    },

    async label_evidence(args) {
      const { seg, item, version } = labelVersionArg(args);
      const cell = finite(String(args.cell ?? '').replace(/^#/, ''), 'cell');
      const clusterings = clusteringsOf(project(), seg.dataset).filter((c) => c.state === 'complete' && c.segmentation === seg.id);
      const c = clusteringRef(clusterings, args.clustering);
      if (!c) throw new ActionError('The segmentation has no clustering: cluster_cells makes one.');
      const src = await labelSource(app, refOf(version.id, version.level));
      const marker = src.kind === 'labels' && src.record && !src.record.source && src.record.clustering === c.id ? src.id : src.kind === 'curated' && src.base?.record && !src.base.record.source && src.base.record.clustering === c.id ? src.base.id : null;
      const response = await fetch(`${item.url}/clusterings/${encodeURIComponent(c.id)}/cells/${cell}/evidence${marker ? `?labels=${encodeURIComponent(marker)}` : ''}`);
      const ev = await response.json();
      if (!response.ok) throw new ActionError(ev.error ?? `HTTP ${response.status}`);
      const versions = labelVersions(project(), seg.dataset, seg.id);
      const labels = {};
      for (const v of versions.slice(0, 12)) {
        const s = await labelSource(app, refOf(v.id, v.level));
        labels[v.name] = s.label[cell] >= 0 ? s.names[s.label[cell]] : null;
      }
      const curated = curatedSetsOf(project(), seg.dataset, seg.id);
      const reviews = curated.flatMap((r) => r.edits.filter((e) => e.cells?.includes(cell)).map((e) => ({ set: r.name, kind: e.kind, label: e.label, reason: e.reason, by: e.by, time: e.time })));
      // From a reference: each round's leading labels, and the genes for the best and the next.
      const referenceOf = (e) => (e ? {
        reference: e.reference, label: e.label >= 0 ? e.rounds.at(-1).scores[0].name : null, lead: e.lead,
        rounds: e.rounds.map((r) => ({ genes: r.genes, scores: r.scores.slice(0, 5).map((x) => ({ label: x.name, correlation: Number(x.score.toFixed(4)) })) })),
        forBest: e.forBest.map((g) => `${g.gene} ${g.count}`), forNext: e.forNext.map((g) => `${g.gene} ${g.count}`),
      } : null);
      const levels = (ev.labels?.levels ?? []).map((lv) => ({ level: lv.level, label: lv.label >= 0 ? lv.evidence[0].name : null, lead: lv.lead, best: lv.evidence[0]?.name, next: lv.evidence[1]?.name ?? null, markers: lv.evidence.map((le) => ({ label: le.name, score: le.score, positive: le.positive.genes.map((g) => `${g.gene} ${g.count}`), negative: (le.negative?.genes ?? []).map((g) => `${g.gene} ${g.count}`) })) }));
      return {
        message: `Cell #${cell}: ${Object.entries(labels).map(([k, v]) => `${k}: ${v ?? 'no label'}`).join('; ')}${reviews.length ? `; reviewed ${reviews.length} time${reviews.length === 1 ? '' : 's'}` : ''}.`,
        data: { cell, cluster: ev.cluster >= 0 ? ev.cluster + 1 : null, labels, levels, reference: referenceOf(ev.reference), expressionNeighbors: ev.neighbors, reviews },
      };
    },

    async curate_labels(args) {
      const { dataset, seg, version } = labelVersionArg(args);
      if (version.kind === 'consensus') throw new ActionError('A consensus is made by its rule: curate one of its sets, or a curated set begun from it.');
      if (!args.reason || !String(args.reason).trim()) throw new ActionError('Say why (reason): every edit keeps its reason.');
      let target = version.kind === 'curated' ? project().artifacts.find((a) => a.id === version.id) : curatedSetsOf(project(), dataset.id, seg.id).find((c) => c.base.labels === version.id && c.base.level === version.level);
      if (!target) {
        const name = args.name ? String(args.name) : `${version.name}, curated`;
        store.commit(createCurated(project(), { dataset: dataset.id, segmentation: seg.id, base: { labels: version.id, level: version.level, name: version.name }, name, by: 'remote control' }), `Begin the curated label set ${name}`);
        target = curatedSetsOf(project(), dataset.id, seg.id).at(-1);
      }
      const edit = { reason: String(args.reason), by: args.by ? String(args.by) : 'remote control' };
      if (Array.isArray(args.clusters)) {
        const clusterings = clusteringsOf(project(), dataset.id).filter((c) => c.state === 'complete' && c.segmentation === seg.id);
        const c = clusteringRef(clusterings, args.clustering);
        if (!c) throw new ActionError('The segmentation has no clustering.');
        Object.assign(edit, { kind: 'cluster', clustering: c.id, clusters: args.clusters.map((k) => finite(k, 'cluster') - 1), label: String(args.label ?? '') });
      } else if (Array.isArray(args.cells)) {
        const cells = args.cells.map((v) => finite(String(v).replace(/^#/, ''), 'cell'));
        let label = args.label === undefined ? null : String(args.label);
        if (args.accept) {
          const src = await labelSource(app, refOf(target.id));
          const own = [...new Set(cells.map((i) => src.label[i]))];
          if (own.length !== 1 || own[0] < 0) throw new ActionError('Accept cells of one label at a time (each has a label).');
          label = src.names[own[0]];
        }
        Object.assign(edit, { kind: args.accept ? 'accept' : 'cells', cells, label });
      } else throw new ActionError('Name cells (their rows, as #123) or clusters (numbers from 1) of a clustering.');
      let next;
      try {
        next = appendEdit(project(), target.id, edit);
      } catch (error) {
        throw new ActionError(error.message);
      }
      store.commit(next, `Curate ${target.name}`);
      const record = project().artifacts.find((a) => a.id === target.id);
      return { message: `${record.name}: ${record.edits.length} edit${record.edits.length === 1 ? '' : 's'}; the last: ${project().history.at(-1).detail}.`, data: { id: record.id, base: record.base, edits: record.edits.length, last: record.edits.at(-1) } };
    },

    async consensus_labels(args) {
      const dataset = resolveDataset(args.dataset);
      const seg = segmentationArg(dataset, args.segmentation);
      const versions = labelVersions(project(), dataset.id, seg.id).filter((v) => v.kind !== 'consensus');
      if (!Array.isArray(args.members) || args.members.length < 2) throw new ActionError(`Name two sets or more (members): ${versions.map((v) => `${v.id}${v.level ? `:${v.level}` : ''} (${v.name})`).join(', ')}.`);
      const members = args.members.map((m) => {
        const [id, lv] = String(m).split(':');
        const v = versions.find((x) => x.id === id && x.level === Number(lv ?? 0)) ?? versions.find((x) => x.name === String(m));
        if (!v) throw new ActionError(`No label set ${m}; the segmentation's are ${versions.map((x) => `${x.id}${x.level ? `:${x.level}` : ''} (${x.name})`).join(', ')}.`);
        return v;
      });
      const rule = { kind: choose(String(args.rule ?? 'majority'), RULES.map((r) => r.id), 'rule') };
      if (rule.kind === 'majority') rule.min = args.min === undefined ? Math.floor(members.length / 2) + 1 : finite(args.min, 'min');
      const mapping = members.map((_, j) => (Array.isArray(args.mapping) ? args.mapping[j] ?? {} : {}));
      const name = args.name ? String(args.name) : 'Consensus';
      let next;
      try {
        next = saveConsensus(project(), { dataset: dataset.id, segmentation: seg.id, name, members: members.map((v) => ({ ref: refOf(v.id, v.level), name: v.name })), mapping, rule, by: 'remote control' });
      } catch (error) {
        throw new ActionError(error.message);
      }
      store.commit(next, `Make the consensus label set ${name}`);
      const record = project().artifacts.filter((a) => a.kind === 'consensus-labels').at(-1);
      const src = await labelSource(app, refOf(record.id));
      let labeled = 0;
      for (const k of src.label) if (k >= 0) labeled += 1;
      const covered = src.voters.reduce((t, v) => t + (v > 0), 0);
      const first = await labelSource(app, refOf(members[0].id, members[0].level));
      const second = await labelSource(app, refOf(members[1].id, members[1].level));
      const agreement = crossTable(first, second).agreement;
      return { message: `${name}: ${labeled.toLocaleString('en-US')} of the ${covered.toLocaleString('en-US')} cells the sets label take a label by ${rule.kind}${rule.min ? ` (${rule.min})` : ''}; the first two sets agree on ${(100 * agreement).toFixed(1)}%.`, data: { id: record.id, rule, members: members.map((v) => v.name), labeled, covered, agreement } };
    },

    async instrument_clusters(args) {
      const dataset = resolveDataset(args.dataset);
      const link = app.bundles.link(dataset.id);
      const item = link?.item ?? app.bundles.itemByPath(dataset.path);
      if (!item) throw new ActionError(`${dataset.name}'s bundle was not found where the project records it.`);
      const listed = await app.bundles.instrumentAnalysis(item);
      if (!listed.clusterings?.length) throw new ActionError(`${dataset.name} holds no analysis of the instrument's.`);
      const ref = String(args.clustering ?? 'graph-based');
      const titles = listed.clusterings.map((c) => c.title);
      const chosen = listed.clusterings.find((c) => c.name === ref) ?? listed.clusterings.find((c) => c.title === choose(ref.replace(/^k-means (\d+)$/i, 'K-means ($1 clusters)'), titles, 'clustering'));
      await settleMode('celltypes');
      let manifest;
      try {
        manifest = await app.cellTypesView.registerInstrument(dataset, chosen.name);
      } catch (error) {
        throw new ActionError(error.message);
      }
      return { message: `${manifest.name} of ${dataset.name}: ${manifest.summary?.clusters} clusters of ${(manifest.summary?.cells ?? 0).toLocaleString('en-US')} cells.`, data: { id: manifest.id, summary: manifest.summary } };
    },

    async export_segmentation(args) {
      const dataset = resolveDataset(args.dataset);
      const p = project();
      const segs = p.artifacts.filter((a) => a.kind === 'segmentation' && a.dataset === dataset.id && a.state === 'complete');
      let seg = promotedSegmentation(p, dataset.id)?.record;
      if (args.segmentation !== undefined) {
        const ref = String(args.segmentation);
        seg = segs.find((x) => x.id === ref) ?? segs.find((x) => x.name === choose(ref, segs.map((y) => y.name), 'segmentation'));
      }
      if (!seg) throw new ActionError(`${dataset.name} has no complete segmentation to export.`);
      if (!Array.isArray(args.formats) || !args.formats.length) throw new ActionError(`formats is a list of one or more of ${Object.keys(EXPORT_FORMATS).join(', ')}.`);
      const formats = [...new Set(args.formats.map((f) => choose(f, Object.keys(EXPORT_FORMATS), 'format')))];
      const folder = String(args.folder ?? '').trim();
      if (!folder) throw new ActionError('Give folder: the absolute path of the export\'s folder.');
      await settleMode('segmentation');
      let manifest;
      try {
        manifest = await app.segmentationView.exportSegmentation(dataset, { segmentation: seg.id, formats, folder, overwrite: Boolean(args.overwrite) });
      } catch (error) {
        throw new ActionError(error.message);
      }
      await frames();
      const size = manifest.files.reduce((sum, f) => sum + f.size, 0);
      const notes = manifest.findings.filter((f) => f.severity !== 'info');
      return {
        message: `${seg.name} of ${dataset.name} exported as ${formats.join(', ')} into ${folder}: ${plural(manifest.files.length, 'file')}, ${(size / 1e6).toFixed(1)} MB${notes.length ? `; ${notes.map((f) => f.message).join(' ')}` : ''}.`,
        data: manifest,
      };
    },

    async compare_segmentations(args) {
      const dataset = resolveDataset(args.dataset);
      const segs = project().artifacts.filter((a) => a.kind === 'segmentation' && a.dataset === dataset.id && a.state === 'complete');
      if (segs.length < 2) throw new ActionError(`${dataset.name} has ${plural(segs.length, 'complete segmentation')}; a comparison needs two.`);
      const find = (ref, what) => {
        const ids = segs.map((x) => x.id);
        if (ids.includes(ref)) return ref;
        const name = choose(ref, segs.map((x) => x.name), what);
        return segs.find((x) => x.name === name).id;
      };
      const a = args.a !== undefined ? find(args.a, 'segmentation A') : (promotedSegmentation(project(), dataset.id)?.record.id ?? segs[0].id);
      const b = args.b !== undefined ? find(args.b, 'segmentation B') : [...segs].reverse().find((x) => x.id !== a)?.id;
      if (a === b) throw new ActionError('A and B are the same segmentation.');
      await settleMode('segmentation');
      let result;
      try {
        result = await app.segmentationView.compare(dataset, a, b);
      } catch (error) {
        throw new ActionError(error.message);
      }
      await frames();
      const d = result.description;
      const nameOf = (id) => segs.find((x) => x.id === id)?.name ?? id;
      const classes = Object.entries(d.classes ?? {}).map(([c, n]) => `${n.toLocaleString('en-US')} ${c}`).join(', ');
      return {
        message: `${nameOf(a)} (A) and ${nameOf(b)} (B): ${classes}; ${(100 * d.pixelAgreement).toFixed(2)}% of pixels in the same cell, ${(100 * d.changesQ20.fraction).toFixed(2)}% of gene transcripts changed cell.`,
        data: { id: result.manifest.id, a: { id: a, name: nameOf(a), ...d.metricsA }, b: { id: b, name: nameOf(b), ...d.metricsB }, classes: d.classes, cellsA: d.cellsA, cellsB: d.cellsB, changes: d.changes, changesQ20: d.changesQ20, iou: d.iou, displacement: d.displacement, pixelAgreement: d.pixelAgreement, coexpression: d.coexpression, params: d.params },
      };
    },

    async show_comparison(args) {
      const dataset = resolveDataset(args.dataset);
      const p = project();
      const list = p.artifacts.filter((a) => a.kind === 'comparison' && a.dataset === dataset.id && a.state === 'complete');
      if (!list.length) throw new ActionError(`${dataset.name} has no comparison: compare_segmentations makes one.`);
      const segName = (id) => p.artifacts.find((a) => a.kind === 'segmentation' && a.id === id)?.name ?? id;
      const named = list.map((c) => ({ c, name: `${segName(c.a)} and ${segName(c.b)}` }));
      let cmp = list.at(-1);
      if (args.comparison !== undefined) {
        const ref = String(args.comparison);
        cmp = list.find((c) => c.id === ref) ?? named.find((x) => x.name === choose(ref, named.map((y) => y.name), 'comparison')).c;
      }
      await settleMode('compare');
      const w = await app.compareView.open(cmp.id);
      if (!w) throw new ActionError('The comparison could not be opened.');
      if (args.mode !== undefined) w.setMode(choose(args.mode, ['side', 'overlay', 'flicker'], 'mode'));
      const classes = args.classes === undefined ? undefined : args.classes.map((c) => choose(c, Object.keys(CLASS_INFO), 'class'));
      const by = args.rank === undefined ? undefined : choose(args.rank, Object.keys(RANKS), 'order');
      const queue = w.setRules({ classes, by, minChanged: args.min_changed === undefined ? undefined : finite(args.min_changed, 'min_changed'), map: args.map === undefined ? undefined : Boolean(args.map) });
      let chosen = null;
      if (args.cell !== undefined) {
        const id = String(args.cell);
        const a = w.prepared.idsA.indexOf(id);
        const b = w.prepared.idsB.indexOf(id);
        const g = a >= 0 ? w.prepared.ofA[a] : b >= 0 ? w.prepared.ofB[b] : -1;
        if (g < 0) throw new ActionError(`Neither segmentation has a cell ${id}.`);
        chosen = g;
      } else if (args.difference !== undefined) {
        const k = finite(args.difference, 'difference');
        if (k < 1 || k > queue.length) throw new ActionError(`difference is from 1 to ${queue.length} (the queue's places).`);
        chosen = queue[k - 1].index;
      }
      if (chosen !== null) w.select(chosen);
      await frames();
      const describe = (g) => {
        const names = cellNames(w.prepared, g, 6);
        return { difference: g.index, class: g.class, a: names.a, b: names.b, reassigned: g.changed, iou: g.a.length === 1 && g.b.length === 1 ? g.iou : null, mixedA: g.mixedA, mixedB: g.mixedB, x: g.x, y: g.y };
      };
      const d = w.description;
      return {
        message: `${w.names[0]} (A) and ${w.names[1]} (B), ${w.session.mode === 'side' ? 'side by side' : w.session.mode}: ${queue.length.toLocaleString('en-US')} differences in the queue, by ${RANKS[w.session.by].label.toLowerCase()}${chosen !== null ? `; showing a ${w.groups[chosen].class} difference` : ''}.`,
        data: { id: cmp.id, mode: w.session.mode, map: w.session.map, rank: w.session.by, classes: [...w.session.classes], queue: queue.length, top: queue.slice(0, 10).map(describe), selected: chosen === null ? null : describe(w.groups[chosen]), cautions: warnings(d) },
      };
    },

    async decide_segmentation(args) {
      const dataset = resolveDataset(args.dataset);
      const p = project();
      const segs = p.artifacts.filter((a) => a.kind === 'segmentation' && a.dataset === dataset.id);
      const ref = String(args.segmentation ?? '');
      const seg = segs.find((s) => s.id === ref) ?? segs.find((s) => s.name === choose(ref, segs.map((x) => x.name), 'segmentation'));
      const decision = choose(args.decision, Object.keys(DECISIONS), 'decision');
      const comparison = args.comparison === undefined ? null : String(args.comparison);
      if (comparison && !p.artifacts.some((a) => a.kind === 'comparison' && a.id === comparison)) throw new ActionError(`No comparison ${comparison}.`);
      let next;
      try {
        next = decideSegmentation(p, { dataset: dataset.id, segmentation: seg.id, decision, rationale: args.rationale, comparison, by: 'a program, by remote control' });
      } catch (error) {
        throw new ActionError(error.message);
      }
      app.store.commit(next, `${DECISIONS[decision].done} ${seg.name}`);
      await frames();
      const promoted = promotedSegmentation(app.store.project, dataset.id);
      return { message: `${DECISIONS[decision].done} ${seg.name} of ${dataset.name}. The default is ${promoted?.record.name ?? 'none'}.`, data: { segmentation: seg.id, decision, default: promoted?.record.id ?? null, decisions: app.store.project.decisions.filter((d) => d.dataset === dataset.id).length } };
    },

    async jobs(args) {
      if (args.show) await settleMode('jobs');
      if (!args.job) {
        const list = app.jobs.list().map((j) => ({ id: j.id, kind: j.kind, title: j.title, state: j.state, progress: j.progress, message: j.message ?? '', runs: j.runs ?? 0, created: j.created, started: j.started ?? null, ended: j.ended ?? null, error: j.error ?? null, resumable: Boolean(j.resumable) }));
        const by = (s) => list.filter((j) => j.state === s).length;
        return { message: `${plural(list.length, 'job')}: ${list.filter((j) => !['complete', 'cancelled', 'failed', 'invalid', 'orphaned'].includes(j.state)).length} running or waiting, ${by('orphaned')} interrupted, ${by('complete')} complete, ${by('failed') + by('invalid')} failed.`, data: { jobs: list } };
      }
      const job = app.jobs.get(String(args.job));
      if (!job) throw new ActionError(`No job ${args.job}. Jobs: ${app.jobs.list().map((j) => j.id).join(', ') || 'none'}.`);
      if (args.do !== undefined) {
        const what = choose(args.do, ['cancel', 'resume', 'discard'], 'thing to do');
        try {
          if (what === 'cancel') {
            if (!(await app.jobs.cancel(job.id))) throw new ActionError(`${job.title} has finished: nothing to stop.`);
          } else if (what === 'resume') await app.jobs.resume(job.id);
          else await app.jobs.discard(job.id);
        } catch (error) {
          if (error instanceof ActionError) throw error;
          throw new ActionError(error.message);
        }
        return { message: `${job.title}: ${{ cancel: 'asked to stop', resume: 'resumed', discard: 'discarded' }[what]}.`, data: { id: job.id, done: what } };
      }
      const full = await app.jobs.full(job.id);
      return { message: `${full.title}: ${full.state}${full.error ? ` (${full.error})` : ''}.`, data: full };
    },

    async methods(args) {
      const dataset = resolveDataset(args.dataset);
      const item = app.bundles.link(dataset.id)?.item ?? app.bundles.itemByPath(dataset.path);
      const report = item ? await app.bundles.validate(item).catch(() => null) : null;
      const { latest } = datasetFilters(project(), dataset);
      const chosen = args.filters === undefined ? latest : args.filters.map((f) => resolveFilter(f, dataset));
      const written = await writeMethods(app, dataset, report, chosen);
      if (args.show) {
        app.focusItem({ kind: 'dataset', id: dataset.id });
        await settleMode('data');
        await showMethods(app, dataset, report, { chosen });
        await frames();
      }
      return { message: written.text, data: { dataset: dataset.name, text: written.text, references: written.references, filters: chosen.map(filterData) } };
    },

    async export(args) {
      const what = choose(args.what, ['project', 'archive', 'methods', 'alignment'], 'kind of file');
      if (what === 'alignment') {
        const dataset = resolveDataset(args.dataset);
        const record = imageRef(dataset, args.image);
        const tf = args.transform === undefined ? transformInUse(project(), record.id)?.record : transformRef(record, args.transform);
        if (!tf) throw new ActionError(`${record.name} is not aligned: name an alignment (transform).`);
        const format = args.format === undefined ? 'matrix' : choose(args.format, ['matrix', 'keypoints'], 'format');
        const response = await fetch(`/api/transforms/${encodeURIComponent(tf.id)}/xenium-explorer/${format}`, { cache: 'no-store' });
        if (!response.ok) throw new ActionError((await response.json().catch(() => ({}))).error ?? `HTTP ${response.status}`);
        return { file: await response.text(), message: `${tf.name} as Xenium Explorer's ${format === 'matrix' ? 'transformation matrix' : 'keypoints'} for ${record.name}.` };
      }
      if (what === 'project') {
        return { file: `${JSON.stringify(parseProject(serializeProject(project())), null, 2)}\n`, message: `The project document of ${project().name}.` };
      }
      if (what === 'methods') {
        const dataset = resolveDataset(args.dataset);
        const result = await actions.methods({ dataset: dataset.id });
        return { file: methodsMarkdown(dataset, result.data), message: `The methods of ${dataset.name}, with ${plural(result.data.references.length, 'reference')}.` };
      }
      // An archive is written by the program itself (api_project.go), beside the bundles it names.
      const response = await fetch('/api/project-archive', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path: args.path, project: project(), overwrite: Boolean(args.overwrite) }) });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new ActionError(`The archive was not written: ${body.error ?? `HTTP ${response.status}`}`);
      return { message: `Wrote ${body.path} (${body.bytes} bytes): ${project().name}, its bundles recorded relative to it.`, data: { path: body.path, bytes: body.bytes } };
    },
  };

  // A post-Xenium image of a dataset by its name or artifact ID (its only image if not named).
  function imageRef(dataset, ref) {
    const records = imagesOf(project(), dataset.id);
    if (!records.length) throw new ActionError(`${dataset.name} has no post-Xenium image: add_image adds one.`);
    if (ref === undefined || ref === null || ref === '') {
      if (records.length > 1) throw new ActionError(`${dataset.name} has ${records.length} images (${records.map((r) => r.name).join('; ')}): name one (image).`);
      return records[0];
    }
    return records.find((r) => r.id === String(ref)) ?? records.find((r) => r.name === choose(String(ref), records.map((x) => x.name), 'image'));
  }

  // A version of an image's alignment by its name or artifact ID ('in use' for the one placing it).
  function transformRef(record, ref) {
    const list = transformsOf(project(), record.id);
    if (/^in use$/i.test(String(ref))) {
      const used = transformInUse(project(), record.id);
      if (!used) throw new ActionError(`${record.name} is not aligned.`);
      return used.record;
    }
    const byId = list.find((x) => x.id === String(ref));
    if (byId) return byId;
    if (!list.length) throw new ActionError(`${record.name} has no alignment yet: align_image or add_image with an alignment makes one.`);
    return list.find((x) => x.name === choose(String(ref), list.map((x) => x.name), 'alignment'));
  }

  // A cell of a dataset by its ID, or by its row as "#123": { row, id }.
  // A cell by its ID (or "#row") in a segmentation of a dataset ('instrument' for the dataset's
  // own): { row, id }.
  async function cellRef(dataset, ref, segmentation = 'instrument') {
    const item = bundleOf(dataset);
    const text = String(ref).trim();
    const name = /^#\d+$/.test(text) ? text.slice(1) : `id:${encodeURIComponent(text)}`;
    const base = segmentation && segmentation !== 'instrument' ? `${item.url}/segmentations/${encodeURIComponent(segmentation)}` : item.url;
    const response = await fetch(`${base}/cells/${name}?top=0`, { cache: 'no-store' });
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new ActionError(body.error ?? `No cell ${text} in ${dataset.name}.`);
    return { row: body.row, id: body.id };
  }

  // A segmentation of a dataset by its name or artifact ID ('instrument', 'default' too), as a view
  // records it: 'instrument' or the artifact's ID.
  // A clustering by id or name ("resolution 1", "The instrument's graph-based clusters"); the latest
  // of SituCraft's if not given.
  function clusteringRef(list, ref) {
    if (ref === undefined || ref === null || ref === '') return list.filter((c) => !c.summary?.source).at(-1) ?? list.at(-1);
    const text = String(ref);
    const byId = list.find((c) => c.id === text);
    if (byId) return byId;
    const names = list.map((c) => c.name);
    return list.find((c) => c.name === choose(text, names, 'clustering'));
  }

  // A fill by a clustering of the segmentation drawn: 'clusters' for its latest, 'clusters:<id>'.
  function clusterFill(dataset, segRef, text) {
    const seg = segRef === 'instrument' ? segmentationChoices(project(), dataset.id).find((c) => c.instrument)?.id : segRef;
    const list = clusteringsOf(project(), dataset.id, seg).filter((c) => c.state === 'complete');
    const id = text.includes(':') ? text.slice(text.indexOf(':') + 1).trim() : list.at(-1)?.id;
    if (!list.some((c) => c.id === id)) throw new ActionError(list.length ? `No clustering ${id} of the segmentation drawn; its clusterings are ${list.map((c) => c.id).join(', ')}.` : 'The segmentation drawn has no clustering: cluster_cells makes one.');
    return `clusters:${id}`;
  }

  // A segmentation by name or id (the default if not given).
  function segmentationArg(dataset, ref) {
    const segs = project().artifacts.filter((a) => a.kind === 'segmentation' && a.dataset === dataset.id && a.state === 'complete' && a.source !== 'planted');
    if (!segs.length) throw new ActionError(`${dataset.name} has no complete segmentation yet.`);
    if (ref === undefined) return segs.find((a) => a.id === promotedSegmentation(project(), dataset.id)?.record.id) ?? segs[0];
    const text = String(ref);
    const seg = /^(the )?instrument('s)?$/i.test(text) ? segs.find((a) => a.source === 'instrument') : segs.find((a) => a.id === text) ?? segs.find((a) => a.name === choose(text, segs.map((x) => x.name), 'segmentation'));
    if (!seg) throw new ActionError(`${dataset.name} has no such segmentation.`);
    return seg;
  }

  // A label version of a segmentation (labels: a label set's id, a curated or consensus set's; the
  // latest if not given; level: a name or number from 1).
  function labelVersionArg(args) {
    const dataset = resolveDataset(args.dataset);
    const seg = segmentationArg(dataset, args.segmentation);
    const link = app.bundles.link(dataset.id);
    const item = link?.item ?? app.bundles.itemByPath(dataset.path);
    if (!item) throw new ActionError(`${dataset.name}'s bundle was not found where the project records it.`);
    const versions = labelVersions(project(), dataset.id, seg.id);
    if (!versions.length) throw new ActionError(`${seg.name} has no labels: label_cells or import_labels makes them.`);
    const ref = args.labels === undefined ? null : String(args.labels);
    const of = ref ? versions.filter((v) => v.id === ref) : versions.filter((v) => v.id === versions.filter((x) => x.kind !== 'consensus').at(-1)?.id);
    if (!of.length) throw new ActionError(`No label set ${ref} of ${seg.name}; its sets are ${[...new Set(versions.map((v) => `${v.id} (${v.name})`))].join(', ')}.`);
    let version = of[0];
    if (args.level !== undefined && of.length > 1) {
      const text = String(args.level);
      version = /^\d+$/.test(text) ? of[Number(text) - 1] : of.find((v) => v.name.toLowerCase().endsWith(`: ${text.toLowerCase()}`));
      if (!version) throw new ActionError(`The set's levels are 1 to ${of.length}.`);
    }
    return { dataset, seg, item, version };
  }

  // A label version of another of the dataset's segmentations (ref: a set's id, 'id:level', or its
  // name) and the comparison of the two segmentations: { version, comparison, segName }.
  function againstArg(dataset, seg, ref) {
    const text = String(ref);
    const [id, lvl] = text.includes(':') && /:\d+$/.test(text) ? [text.slice(0, text.lastIndexOf(':')), Number(text.slice(text.lastIndexOf(':') + 1)) - 1] : [text, null];
    const segs = project().artifacts.filter((a) => a.kind === 'segmentation' && a.dataset === dataset.id && a.id !== seg.id);
    for (const other of segs) {
      const vs = labelVersions(project(), dataset.id, other.id);
      const of = vs.filter((v) => v.id === id || v.name === text);
      if (!of.length) continue;
      const version = lvl === null ? of[0] : of[lvl];
      if (!version) throw new ActionError(`The set's levels are 1 to ${of.length}.`);
      const comparison = project().artifacts.find((a) => a.kind === 'comparison' && a.state === 'complete' && ((a.a === seg.id && a.b === other.id) || (a.b === seg.id && a.a === other.id)));
      if (!comparison) throw new ActionError(`${seg.name} and ${other.name} have not been compared: compare_segmentations compares them.`);
      return { version, comparison, segName: other.name };
    }
    throw new ActionError(`No label set ${text} of another segmentation of ${dataset.name}.`);
  }

  // A label set of a clustering by id, its latest if not given; and a level by name or number from 1.
  function labelsOf(dataset, clusteringId, ref) {
    const clustering = project().artifacts.find((a) => a.kind === 'clustering' && a.id === clusteringId);
    const sets = labelSetsFor(project(), clustering).filter((r) => r.state === 'complete');
    if (!sets.length) throw new ActionError('The clustering has no label set: label_cells makes one.');
    if (!ref) return sets.at(-1);
    const r = sets.find((x) => x.id === ref);
    if (!r) throw new ActionError(`No label set ${ref} of the clustering; its label sets are ${sets.map((x) => x.id).join(', ')}.`);
    return r;
  }
  function levelOf(r, text) {
    const levels = r.summary?.levels ?? [];
    if (text === undefined || text === '') return 0;
    if (/^\d+$/.test(text)) {
      const l = Number(text) - 1;
      if (l < 0 || l >= levels.length) throw new ActionError(`The labels have levels 1 to ${levels.length} (${levels.join(', ')}).`);
      return l;
    }
    return levels.indexOf(choose(text, levels, 'level'));
  }

  // show_map's colorings by labels: labels[:<level>] or labels:<id>[:<level>] (the clustering's
  // latest label set, its first level), confidence[:<id>].
  async function labelColor(dataset, c, text) {
    const parts = text.split(':').map((x) => x.trim());
    if (/^(cur|con)-/.test(parts[1] ?? '')) {
      if (!project().artifacts.some((a) => a.id === parts[1] && a.segmentation === c.segmentation)) throw new ActionError(`No curated or consensus label set ${parts[1]} of the clustering's segmentation.`);
      return { kind: /^confidence$/i.test(parts[0]) ? 'confidence' : 'labels', labels: parts[1], level: 0 };
    }
    const id = parts[1]?.startsWith('lab-') ? parts[1] : null;
    const r = labelsOf(dataset, c.id, id);
    if (/^confidence$/i.test(parts[0])) return { kind: 'confidence', labels: r.id };
    return { kind: 'labels', labels: r.id, level: levelOf(r, id ? parts[2] : parts[1]) };
  }

  // A fill by a label set of the segmentation drawn: 'labels[:<level>]' for its latest,
  // 'labels:<id>[:<level>]'.
  async function labelFill(dataset, segRef, text) {
    const seg = segRef === 'instrument' ? segmentationChoices(project(), dataset.id).find((c) => c.instrument)?.id : segRef;
    const version = /^labels:((cur|con)-[\w-]+)/i.exec(text)?.[1];
    if (version) {
      if (!project().artifacts.some((a) => a.id === version && a.segmentation === seg)) throw new ActionError(`No curated or consensus label set ${version} of the segmentation drawn.`);
      return `labels:${version}:0`;
    }
    const sets = project().artifacts.filter((a) => a.kind === 'labels' && a.dataset === dataset.id && a.segmentation === seg && a.state === 'complete');
    if (!sets.length) throw new ActionError('The segmentation drawn has no label set: label_cells makes one.');
    const parts = text.split(':').map((x) => x.trim());
    const id = parts[1]?.startsWith('lab-') ? parts[1] : null;
    const r = id ? sets.find((x) => x.id === id) : sets.at(-1);
    if (!r) throw new ActionError(`No label set ${id} of the segmentation drawn; its label sets are ${sets.map((x) => x.id).join(', ')}.`);
    return `labels:${r.id}:${levelOf(r, id ? parts[2] : parts[1])}`;
  }

  function segmentationRef(dataset, ref) {
    const list = segmentationChoices(project(), dataset.id);
    const text = String(ref ?? '').trim();
    let c;
    if (/^default$/i.test(text)) c = defaultChoice(project(), dataset.id);
    else if (/^(the )?instrument('s)?$/i.test(text)) c = list.find((x) => x.instrument) ?? { instrument: true };
    else c = list.find((x) => x.id === text) ?? list.find((x) => x.name === choose(text, list.map((y) => y.name), 'segmentation'));
    return !c || c.instrument ? 'instrument' : c.id;
  }

  // --- Connection -----------------------------------------------------------------------------

  async function perform(event) {
    let outcome;
    try {
      const handler = Object.hasOwn(actions, event.action) ? actions[event.action] : null;
      if (!handler) throw new ActionError(`Unknown action "${event.action}". Actions: ${Object.keys(actions).join(', ')}.`);
      const args = typeof event.args === 'object' && event.args && !Array.isArray(event.args) ? event.args : {};
      author = event.client || 'a program on this computer';
      app.log(`${author}: ${event.action}${Object.keys(args).length ? ` ${JSON.stringify(args).slice(0, 200)}` : ''}`);
      // A dialog left open (one an earlier action showed, or the user's) would hide what this
      // action shows: it is closed as Escape closes it, its question unanswered.
      if (event.action !== 'get_state' && document.querySelector('.scrim')) {
        document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
        await frames();
      }
      const result = await handler(args, event);
      let message = result.message ?? 'Done.';
      // A file: the program writes it where the caller asked.
      if (result.file !== undefined) {
        if (!event.output) throw new ActionError('Writing files needs the SituCraft program.');
        const response = await fetch(event.output, { method: 'POST', body: result.file, headers: { 'Content-Type': 'application/octet-stream' } });
        const written = await response.json().catch(() => ({}));
        if (!response.ok) throw new ActionError(`The file could not be written: ${written.error ?? `HTTP ${response.status}`}`);
        message = `Wrote ${written.path} (${written.bytes} bytes). ${message}`;
      }
      outcome = { ok: true, message, data: result.data ?? null };
    } catch (error) {
      outcome = { ok: false, message: error.message ?? String(error) };
      if (!(error instanceof ActionError)) console.error(error);
    }
    await fetch(`api/remote/result/${encodeURIComponent(event.id)}`, { method: 'POST', body: JSON.stringify(outcome), headers: { 'Content-Type': 'application/json' } }).catch(() => {});
  }

  let source = null;
  let announced = false;
  // Only the run of SituCraft this page was opened from takes it as its window: when the
  // connection drops and another run answers (SituCraft started again, maybe another version or
  // library), the page stops taking remote control and asks to be reloaded.
  const session = app.info?.session ?? '';
  const connect = () => {
    source = new EventSource(`api/remote/events?session=${encodeURIComponent(session)}`);
    source.addEventListener('open', () => {
      if (!announced) app.log('Remote control is on: programs on this computer can drive this window (situcraft --remote-control).');
      announced = true;
    });
    source.addEventListener('action', (message) => {
      let event;
      try {
        event = JSON.parse(message.data);
      } catch {
        return;
      }
      perform(event);
    });
    source.onerror = async () => {
      source.close();
      const now = await fetch('api/info', { cache: 'no-store' }).then((r) => (r.ok ? r.json() : null)).catch(() => null);
      if (now?.session && now.session !== session) {
        const words = `SituCraft was started again (${now.version}): this page no longer takes remote control. Reload it to work with that run.`;
        app.log(words);
        toast(words, { kind: 'error', timeout: 0 });
        return;
      }
      setTimeout(connect, 2000);
    };
  };
  connect();
  app.remoteActions = actions;
  toast('Remote control is on: programs on this computer can drive this window.', { timeout: 3000 });
  return { disconnect: () => source?.close(), actions };
}
