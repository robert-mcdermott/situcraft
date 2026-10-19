// Captures the documentation's screenshots, in the light and dark themes, as
// docs/images/<scene>-<theme>.webp: the README and the website (docs/site/build.mjs copies them)
// show the one matching the reader's theme. Each scene drives a SituCraft built from source
// through its remote control (actions.go, web/ui/remote.js), as any script can, on a fresh, empty
// library, with the development bundles as its examples (node validation/fetch.mjs downloads
// them), so every run gives the same pictures. Follows MaveScape 0.8.0's docs/capture/capture.mjs.
//
//   node docs/capture/capture.mjs [scene …] [--theme light|dark|both] [--audit [--no-shots]]
//
// --audit runs axe-core (node validation/fetch.mjs axe-core fetches it) in every scene with the
// WCAG 2.1 A and AA rules, and writes the violations to docs/capture/audit.json (one entry per scene
// and theme; a run of some scenes replaces only theirs) and a summary to the console; with
// --no-shots, no picture is written.
//
// Needs Go (to build SituCraft) and Chrome, Chromium, Edge or Brave (CHROME=path).

import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { launch, sleep } from '../../validation/cdp.mjs';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const IMAGES = join(ROOT, 'docs/images');
// A port of SituCraft's range the viewer suite (8857) does not use.
const PORT = 8855;
const DATA = process.env.SITUCRAFT_DATA || join(ROOT, 'validation/cache');
// What the pictures show as the places of the library, the examples and the cache (not this run's
// temporary ones): macOS's defaults.
const SHOWN = {
  library: '~/Library/Application Support/SituCraft',
  examples: '~/Library/Application Support/SituCraft/examples',
  cache: '~/Library/Caches/situcraft',
  exports: '~/Documents/SituCraft exports',
  references: '~/Documents/references',
};

const args = process.argv.slice(2);
const option = (name) => {
  const i = args.indexOf(`--${name}`);
  return i >= 0 ? args[i + 1] : null;
};
const themes = { light: ['light'], dark: ['dark'], both: ['light', 'dark'] }[option('theme') ?? 'both'];
if (!themes) throw new Error('--theme is light, dark or both');
const wanted = args.filter((a, i) => !a.startsWith('--') && args[i - 1] !== '--theme');
const audit = args.includes('--audit');
const shots = !args.includes('--no-shots');
const AXE = join(ROOT, 'validation/cache/axe-core/axe.min.js');
if (audit && !existsSync(AXE)) throw new Error('axe-core is missing: run node validation/fetch.mjs axe-core');
for (const bundle of ['xenium-lung-2fov', 'xenium-breast-2fov']) {
  if (!existsSync(join(DATA, bundle, 'experiment.xenium'))) throw new Error(`The scenes need the development bundles: run node validation/fetch.mjs (${bundle} is missing)`);
}

// --- SituCraft ------------------------------------------------------------------------------------

const temp = mkdtempSync(join(tmpdir(), 'situcraft-capture-'));
const binary = join(temp, process.platform === 'win32' ? 'situcraft.exe' : 'situcraft');
// One cache for every scene: its entries are named for their sources, so scenes share them safely
// and each bundle's derived data are built once.
const CACHE = join(temp, 'cache');
let URL = '';
let token = '';
let places = {};

// Started per scene on an empty library, so that no scene sees another's projects. examples: the
// folder of examples (the development bundles by default; an empty one shows them to download).
// cache: a cache of the scene's own (the jobs scene, whose jobs build what the shared one holds).
async function startSituCraft({ examples = DATA, cache = CACHE } = {}) {
  const library = mkdtempSync(join(temp, 'library-'));
  places = { library, examples, cache, exports: mkdtempSync(join(temp, 'exports-')) };
  const flags = ['--remote-control', '--window', 'none', '--port', String(PORT), '--data-dir', library, '--cache-dir', cache, '--examples-dir', examples];
  const server = spawn(binary, flags, { cwd: temp, stdio: ['ignore', 'pipe', 'inherit'] });
  let output = '';
  server.stdout.on('data', (chunk) => { output += chunk; });
  const stop = async () => {
    const exited = new Promise((done) => server.once('exit', done));
    server.kill();
    await Promise.race([exited, sleep(3000)]);
  };
  for (let i = 0; i < 150; i += 1) {
    const address = /running at (http:\/\/[\d.]+:\d+)/.exec(output)?.[1];
    if (address && existsSync(join(library, 'remote.json'))) {
      URL = `${address}/`;
      token = JSON.parse(readFileSync(join(library, 'remote.json'), 'utf8')).token;
      return { stop };
    }
    await sleep(100);
  }
  await stop();
  throw new Error(`SituCraft did not start:\n${output}`);
}

// An action through remote control, as any script sends it.
async function act(action, actionArgs = {}) {
  const response = await fetch(`${URL}api/remote/action`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-SituCraft-Token': token }, body: JSON.stringify({ action, args: actionArgs, client: 'docs/capture' }) });
  const result = await response.json();
  if (!result.ok) throw new Error(`${action}: ${result.message ?? result.error}`);
  return result;
}

// --- Page helpers ---------------------------------------------------------------------------------

let b;
const js = (expression) => b.eval(expression);

async function waitFor(expression, timeout = 60000) {
  const start = Date.now();
  while (Date.now() - start < timeout) {
    if (await js(expression)) return true;
    await sleep(200);
  }
  throw new Error(`Timed out waiting for ${expression}`);
}

const scrollTo = async (selector, block = 'start') => {
  await js(`document.querySelector(${JSON.stringify(selector)})?.scrollIntoView({ block: ${JSON.stringify(block)} })`);
  await sleep(500);
};
const clearToasts = () => js(`document.querySelectorAll('.toast').forEach((t) => t.remove())`);

// The places shown in the window, as SHOWN names them: every text node, title and text field that
// holds this run's temporary folders.
async function showPlaces() {
  const pairs = Object.entries(places).map(([k, v]) => [v, SHOWN[k]]).sort((a, c) => c[0].length - a[0].length);
  await js(`(() => {
    const pairs = ${JSON.stringify(pairs)};
    const fix = (text) => pairs.reduce((t, [from, to]) => t.split(from).join(to), text);
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) if (pairs.some(([from]) => node.nodeValue.includes(from))) node.nodeValue = fix(node.nodeValue);
    for (const el of document.querySelectorAll('[title]')) el.title = fix(el.title);
    for (const el of document.querySelectorAll('input[type=text]')) el.value = fix(el.value);
    // A command's arguments, quoted as SituCraft quotes them once the places shown hold spaces.
    for (const el of document.querySelectorAll('.seg-command code')) {
      el.textContent = el.textContent.replace(/(--[a-z-]+=)([^'].*?)(?= --|$)/g, (all, flag, value) => (/\\s/.test(value) ? flag + "'" + value + "'" : all));
    }
    return true;
  })()`);
}

// --- Scenes ---------------------------------------------------------------------------------------

const scenes = {
  // The start page: the examples to download or make, what to bring.
  async start() {
    await act('set_mode', { mode: 'start' });
    await waitFor(`document.querySelectorAll('.card.example').length === 4`);
  },
  // The Lung example checked: what the run was, its findings, its derived data.
  async data() {
    await act('open_example', { example: 'lung' });
    await act('set_mode', { mode: 'data' });
  },
  // The Breast section: the nuclear and boundary stains, the cells' outlines.
  async viewer() {
    await act('open_example', { example: 'breast' });
    await act('show_tissue', { center: [620, 400], width_um: 520, genes: [] });
  },
  // Transcripts of four genes in the Lung section, and a cell in the inspector.
  async 'viewer-genes'() {
    await act('open_example', { example: 'lung' });
    await act('show_tissue', { genes: ['EPCAM', 'COL5A2', 'PLVAP', 'CD68'], channels: { DAPI: { color: '#8c8c8c' } }, center: [750, 300], um_per_pixel: 0.3, cell: 'neddaiac-1' });
  },
  // How each Breast cell was found, from the boundary or interior stains or by expanding a nucleus.
  async 'viewer-methods'() {
    await act('open_example', { example: 'breast' });
    await act('show_tissue', { fill: 'method', opacity: 0.6, center: [300, 300], um_per_pixel: 0.45 });
  },
  // QC of the Lung run: the metrics recomputed, the cells colored by their transcripts.
  async qc() {
    await act('open_example', { example: 'lung' });
    await act('qc_metrics');
    await act('set_qc_map', { map: 'cell:transcripts' });
  },
  // Where the Breast section's transcripts fall outside cells.
  async 'qc-outside'() {
    await act('open_example', { example: 'breast' });
    await act('set_qc_map', { map: 'bin:outside' });
  },
  // A filter saved in two versions and compared, the second in use on the map.
  async 'qc-filter'() {
    await act('open_example', { example: 'lung' });
    await act('save_filter', { name: 'Enough signal', conditions: [{ attribute: 'transcripts', min: 20 }] });
    await act('save_filter', { name: 'Enough signal', conditions: [{ attribute: 'transcripts', min: 20 }, { attribute: 'area', min: 15, max: 400 }] });
    await act('set_qc_map', { map: 'cell:transcripts', filter: 'Enough signal' });
    await js(`[...document.querySelectorAll('.qc-tab')].find((b) => b.textContent === 'Filters')?.click()`);
    await sleep(400);
  },
  // Saved views in the project, one open with its link in the inspector.
  async 'saved-views'() {
    await act('open_example', { example: 'lung' });
    await act('show_tissue', { genes: ['VWF', 'PLVAP', 'RGS5', 'PDGFRB'], channels: { DAPI: { color: '#8c8c8c' } }, center: [450, 150], um_per_pixel: 0.35 });
    await act('save_view', { name: 'Vessels and pericytes' });
    await act('show_tissue', { genes: ['EPCAM', 'COL5A2', 'PLVAP', 'CD68'], center: [750, 300], um_per_pixel: 0.4 });
    await act('save_view', { name: 'Epithelium and stroma' });
    await act('set_qc_map', { map: 'bin:outside' });
    await act('save_view', { name: 'Transcripts outside cells' });
    await act('open_view', { view: 'Vessels and pericytes' });
    await act('focus', { kind: 'view', name: 'Vessels and pericytes' });
  },
  // The Breast example's methods paragraph, its data cited.
  async methods() {
    await act('open_example', { example: 'breast' });
    await act('methods', { show: true });
    await waitFor(`Boolean(document.querySelector('.dialog .methods-text'))`);
  },
  // The Lung example's guided tour, at its third step.
  async tour() {
    await act('tour', { example: 'lung', step: 3 });
  },
  // The Breast example's instrument segmentation as an artifact: its methods, files by SHA-256
  // and check, in the inspector (wave 2, slice 2).
  async segmentation() {
    await act('open_example', { example: 'breast' });
    await act('segmentations', { show: true });
  },
  // The Lung example's nuclei grown by 5 µm, compared with the instrument's segmentation, in the
  // Segmentation view (wave 2, slice 3).
  async expansion() {
    await act('open_example', { example: 'lung' });
    await act('expand_nuclei', { distance_um: 5 });
    await js(`document.querySelector('.seg-section:last-child')?.scrollIntoView({ block: 'end' })`);
    await sleep(300);
  },
  // The Breast example's instrument segmentation compared with its nuclei grown by 5 µm: the
  // differences by class and each segmentation's measures (wave 2, slice 5).
  async comparison() {
    await act('open_example', { example: 'breast' });
    await act('expand_nuclei', { distance_um: 5 });
    await act('compare_segmentations', {});
    await waitFor(`document.querySelectorAll('.seg-compare-detail table').length === 2`);
    await js(`document.querySelector('.seg-compare-detail')?.scrollIntoView({ block: 'start' })`);
    await sleep(300);
  },
  // The Breast example's comparison in the Compare workspace: A and B side by side with one camera,
  // the review queue's first difference (a split) chosen (wave 2, slice 6).
  async workspace() {
    await act('open_example', { example: 'breast' });
    await act('expand_nuclei', { distance_um: 5 });
    await act('compare_segmentations', {});
    await act('show_comparison', { mode: 'side', rank: 'reassigned', difference: 1 });
    await js(`(async () => { const w = window.situcraft.compareView.current(); for (const r of w.state.renderers) await r.renderer.settled(); return true; })()`);
    await sleep(500);
  },
  // The same comparison over the whole tissue: B's outlines over A's, and the map of where they
  // disagree.
  async 'workspace-map'() {
    await act('open_example', { example: 'breast' });
    await act('expand_nuclei', { distance_um: 5 });
    await act('compare_segmentations', {});
    await act('show_comparison', { mode: 'overlay', map: true });
    await js(`(async () => { const w = window.situcraft.compareView.current(); for (const r of w.state.renderers) await r.renderer.settled(); return true; })()`);
    await sleep(500);
  },
  // The Lung example's segmentation imported from SpatialData (written by spatialdata-io:
  // validation/spatialdata.py), beside the instrument's, in the Segmentation view and the inspector
  // (wave 2, slice 4).
  async import() {
    const store = join(DATA, 'xenium-lung-2fov.spatialdata.zarr');
    if (!existsSync(store)) throw new Error(`${store} is missing: uv run --with 'spatialdata-io==0.7.1' --with 'spatialdata==0.8.0' python -I validation/spatialdata.py`);
    await act('open_example', { example: 'lung' });
    await act('import_segmentation', { format: 'spatialdata', paths: [store], units: 'pixels', tool: 'spatialdata-io', tool_version: '0.7.1' });
    await js(`document.querySelector('.workbench-scroll')?.scrollTo(0, 0)`);
    await sleep(300);
  },
  // The Lung example's worked example: errors planted for practice and compared with the
  // instrument's segmentation, the review queue's first difference chosen, the example's card
  // beside the queue (wave 2, slice 7).
  async worked() {
    await act('tour', { example: 'lung', name: 'planted-errors' });
    await act('tour', { run: true });
    const step = await act('tour', { run: true });
    await waitFor(`Boolean(window.situcraft.compareView?.current?.()) || document.querySelector('.tour-note')?.textContent`);
    const note = await js(`window.situcraft.compareView?.current?.() ? '' : document.querySelector('.tour-note')?.textContent`);
    if (note) throw new Error(`step ${step.data?.step}: ${note}`);
    await js(`(async () => { const w = window.situcraft.compareView.current(); for (const r of w.state.renderers) await r.renderer.settled(); return true; })()`);
    await sleep(500);
  },
  // The segmentation studio on the Breast example (wave 3, slice 5): the Balanced preset's
  // preflight, at the Compute step, what the run needs against what the computer has.
  async studio() {
    await act('open_example', { example: 'breast' });
    await act('refinement_preflight', { preset: 'balanced' });
    await js(`[...document.querySelectorAll('.studio-step')].find((b) => b.textContent.includes('Compute'))?.click()`);
    await sleep(400);
    await scrollTo('.studio-steps', 'start');
  },
  // The Lung example refined from its transcripts (Balanced) and drawn on the tissue (wave 3,
  // slice 8): cells filled by their boundary certainty, transcripts by their probability of their
  // cell, a cell in the inspector.
  async refined() {
    await act('open_example', { example: 'lung' });
    const refined = await act('refine_segmentation', { preset: 'balanced' });
    await act('show_tissue', { segmentation: refined.data.id, fill: 'certainty', opacity: 0.6, all_transcripts: true, color_by: 'probability', channels: { DAPI: { color: '#8c8c8c' } }, center: [750, 300], um_per_pixel: 0.25, cell: 'c100' });
    await js(`window.situcraftViewer.settled()`);
  },
  // The Breast example's cells clustered (wave 4, slice 1): the Cell types view, its clusters with
  // their sizes and leading markers, the components' variances.
  async celltypes() {
    await act('open_example', { example: 'breast' });
    await act('cluster_cells', {});
    await waitFor(`Boolean(document.querySelector('.ct-markers tbody tr'))`);
    await scrollTo('.ct-detail', 'start');
  },
  // The Breast example's map beside its tissue (wave 4, slice 2): clusters named (the same name
  // merging two), the T cells selected on both, the selection's composition.
  async cellmap() {
    await act('open_example', { example: 'breast' });
    await act('cluster_cells', {});
    await act('name_clusters', { names: { 1: 'Tumor', 5: 'Tumor', 3: 'Dividing tumor', 6: 'Macrophages', 7: 'Fibroblasts', 9: 'T cells', 12: 'Endothelium' } });
    await act('show_map', { color_by: 'clusters', select: { clusters: [9] } });
    await waitFor(`Boolean(document.querySelector('.ct-composition'))`);
    await scrollTo('.cell-map', 'start');
    await js(`window.situcraft.cellTypesView.map().settled()`);
    await sleep(400);
  },
  // The Breast example labeled from the breast starter set (wave 4, slice 3): each cluster's labels
  // at the lineage level as bars, names for the clusters from them, the labels with their markers.
  async labels() {
    await act('open_example', { example: 'breast' });
    await act('cluster_cells', {});
    await act('label_cells', { markers: 'breast' });
    await waitFor(`Boolean(document.querySelector('.lb-composition tbody tr'))`);
    await js(`[...document.querySelectorAll('.lb-levels button')].find((b) => b.textContent === 'Lineage')?.click()`);
    await waitFor(`document.querySelector('.lb-levels button[aria-pressed="true"]')?.textContent === 'Lineage' && Boolean(document.querySelector('.lb-composition tbody tr'))`);
    await scrollTo('.lb-shown', 'start');
    await sleep(300);
  },
  // The Lung example's instrument clusters imported as labels (wave 4, slice 4): each of
  // SituCraft's clusters by the instrument's, the labels with their cells.
  async imported() {
    await act('open_example', { example: 'lung' });
    await act('cluster_cells', {});
    await act('import_labels', { segmentation: 'instrument', instrument: 'graph-based' });
    await waitFor(`Boolean(document.querySelector('.lb-composition tbody tr'))`);
    await scrollTo('.lb-shown', 'start');
    await sleep(300);
  },
  // The Breast example's labels reviewed (wave 4, slice 5): the queue, and the first cell's
  // evidence — its markers, why not its neighbors' label, its neighbors.
  async review() {
    await act('open_example', { example: 'breast' });
    await act('cluster_cells', {});
    await act('label_cells', { markers: 'breast' });
    await waitFor(`Boolean(document.querySelector('[data-action=review-queue]'))`);
    await js(`document.querySelector('[data-action=review-queue]').click()`);
    await waitFor(`Boolean(document.querySelector('.rv-table tbody tr'))`);
    await js(`document.querySelector('.rv-table tbody tr').click()`);
    await waitFor(`Boolean(document.querySelector('.rv-evidence .review-form'))`);
    await scrollTo('.rv-evidence', 'start');
    await sleep(300);
  },
  // The simulated example labeled from a single-cell reference of its types (wave 4, slice 6),
  // written by situcraft simulate --reference: the reference read against the panel, the cells'
  // labels by cluster, each label's reference cells and cells.
  async reference() {
    places.references = join(temp, 'references');
    mkdirSync(places.references, { recursive: true });
    const made = join(places.references, 'simulated');
    if (!existsSync(`${made}-reference.zarr`)) {
      await new Promise((done, fail) => spawn(binary, ['simulate', '--quiet', '--reference', '300', made], { cwd: temp, stdio: 'ignore' }).on('exit', (code) => (code ? fail(new Error('situcraft simulate --reference failed')) : done())));
    }
    const path = `${made}-reference.zarr`;
    await act('open_example', { example: 'simulated-standard' });
    await act('cluster_cells', {});
    await act('reference_labels', { path, name_clusters: true });
    await waitFor(`Boolean(document.querySelector('.lb-shown .lb-labels tbody tr'))`);
    await js(`(() => { const input = document.querySelector('#lb-ref-path'); input.value = ${JSON.stringify(path)}; input.dispatchEvent(new Event('change')); document.querySelector('[data-action=read-reference]').click(); })()`);
    await waitFor(`Boolean(document.querySelector('[data-action=map-reference]'))`);
    await scrollTo('section:has(> .lb-reference)', 'start');
    await sleep(300);
  },
  // The Breast example's labels under two segmentations (wave 4, slice 7): the instrument's and a
  // 5 µm nucleus expansion compared, the clustering and its labels made again on the expansion,
  // and the comparison's Labels tab with the tissue colored by each side's labels.
  async impact() {
    await act('open_example', { example: 'breast' });
    await act('cluster_cells', {});
    await act('label_cells', { markers: 'breast' });
    await act('expand_nuclei', { distance_um: 5 });
    await act('compare_segmentations', { a: 'Instrument segmentation', b: 'Nucleus expansion, 5 µm' });
    await act('repeat_on_segmentation', { segmentation: 'Nucleus expansion, 5 µm' });
    await act('label_impact', { color: true });
    await waitFor(`Boolean(document.querySelector('.cmp-impact-table tbody tr'))`);
    await js(`(async () => { const w = window.situcraft.compareView.current(); for (const r of w.state.renderers) await r.renderer.settled(); })()`);
    await sleep(400);
  },
  // The simulated tissue's labels against its truth, a worked example (wave 4, slice 9): its true
  // types and the instrument's cells labeled from the types' markers read through their
  // comparison, both tissues colored by type, the example's card beside the Labels tab.
  async 'worked-labels'() {
    await act('open_example', { example: 'simulated' });
    await act('tour', { example: 'simulated', name: 'labels-against-truth' });
    let step = null;
    for (let k = 0; k < 6; k += 1) step = await act('tour', { run: true });
    if (step?.data?.step !== 7) throw new Error(`the worked example stopped at step ${step?.data?.step}: ${step?.message}`);
    await waitFor(`Boolean(document.querySelector('.cmp-impact-table tbody tr'))`);
    await js(`(async () => { const w = window.situcraft.compareView.current(); for (const r of w.state.renderers) await r.renderer.settled(); })()`);
    await sleep(400);
  },
  // The simulated tissue's H&E aligned three ways, a worked example (wave 5, slice 7): in the
  // Alignment view, the matrix's version over the stains with where it is off, patch by patch.
  async alignment() {
    await act('open_example', { example: 'simulated', remake: true });
    await act('tour', { example: 'simulated', name: 'align-he' });
    let step = null;
    for (let k = 0; k < 5; k += 1) step = await act('tour', { run: true });
    if (step?.data?.step !== 6) throw new Error(`the worked example stopped at step ${step?.data?.step}: ${step?.message}`);
    await waitFor(`(window.situcraftAlignment?.errorSquares?.() ?? 0) > 0`);
    await js(`window.situcraftAlignment.settled()`);
    await sleep(400);
  },
  // Regions drawn on the H&E (wave 5, slice 5): the worked example's two domains over the cells
  // filled by their true types, and the Viewer's Regions listing them.
  async regions() {
    await act('open_example', { example: 'simulated', remake: true });
    await act('tour', { example: 'simulated', name: 'regions-qupath' });
    let step = null;
    for (let k = 0; k < 3; k += 1) step = await act('tour', { run: true });
    if (step?.data?.step !== 4) throw new Error(`the worked example stopped at step ${step?.data?.step}: ${step?.message}`);
    await act('tour', { run: true });
    await act('tour', { step: 4 });
    await waitFor(`Boolean(window.situcraftViewer?.clusterFill?.())`);
    await scrollTo('.viewer-regions-section');
    await js(`window.situcraftViewer.settled()`);
    await sleep(400);
  },
  // A pathologist's review, a worked example (wave 5, slices 6 and 7): the cells filled by their
  // review status, the fold's excluded, and the Viewer's Reviews counting them and listing the record.
  async reviews() {
    await act('open_example', { example: 'simulated', remake: true });
    await act('tour', { example: 'simulated', name: 'pathologist-review' });
    let step = null;
    for (let k = 0; k < 6; k += 1) step = await act('tour', { run: true });
    if (step?.data?.step !== 7) throw new Error(`the worked example stopped at step ${step?.data?.step}: ${step?.message}`);
    await act('show_tissue', { dataset: 'simulated-standard', center: [300, 120], um_per_pixel: 0.35 });
    await waitFor(`Boolean(window.situcraftViewer?.clusterFill?.()?.review)`);
    await scrollTo('.viewer-reviews-section');
    await js(`window.situcraftViewer.settled()`);
    await sleep(400);
  },
  // The Lung example's cells as a graph (wave 6, slice 1): the Spatial view, the Delaunay graph's
  // definition, and its numbers with histograms of each cell's neighbors and the edges' lengths.
  async spatial() {
    await act('open_example', { example: 'lung' });
    await act('build_graph', { kind: 'delaunay' });
    await waitFor(`Boolean(document.querySelector('.sp-plots canvas'))`);
    await scrollTo('.sp-detail', 'start');
    await sleep(300);
  },
  // The graph on the tissue close up (wave 6, slice 1): its edges over the cells, a cell's in
  // yellow, and the cell's neighbors in the inspector.
  async graph() {
    await act('open_example', { example: 'lung' });
    const built = await act('build_graph', { kind: 'delaunay' });
    await act('show_tissue', { graph: built.data.id, center: [700, 330], um_per_pixel: 0.22, genes: ['EPCAM', 'CD68'], cell: 'nheihaca-1', channels: { DAPI: { color: '#8c8c8c' } } });
    await waitFor(`Boolean(document.querySelector('.viewer-neighbor-list li'))`);
    await js(`window.situcraftViewer.settled()`);
    await sleep(400);
  },
  // Which cell types are neighbors (wave 6, slice 2): the Lung example's classes' neighborhood
  // enrichment over its Delaunay graph, the heat map and the pairs most and least often neighbors,
  // one pair chosen.
  async enrichment() {
    await act('open_example', { example: 'lung' });
    await act('cluster_cells', {});
    const markers = await act('label_cells', { markers: 'lung' });
    await act('build_graph', { kind: 'delaunay' });
    await act('spatial_analysis', { question: 'enrichment', labels: markers.data.id, level: 1 });
    await waitFor(`Boolean(document.querySelector('.sp-heatmap'))`);
    await js(`document.querySelector('.sp-square[data-pair="2,3"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))`);
    await waitFor(`Boolean(document.querySelector('.sp-pair'))`);
    await scrollTo('.sp-answer', 'start');
    await sleep(300);
  },
  // At what scale (wave 6, slice 3): Ripley's L of the Lung classes in the tissue, the stromal cells'
  // curve above the envelope of the labels dealt at random.
  async ripley() {
    await act('open_example', { example: 'lung' });
    await act('cluster_cells', {});
    const markers = await act('label_cells', { markers: 'lung' });
    await act('spatial_analysis', { question: 'ripley', labels: markers.data.id, level: 1 });
    await waitFor(`Boolean(document.querySelector('.sp-answer .sp-chart'))`);
    // Each choice draws the answer again, so the second select is found after the first is drawn.
    for (const id of ['sp-from', 'sp-to']) {
      await waitFor(`Boolean(document.getElementById('${id}'))`);
      await js(`(() => { const el = document.getElementById('${id}'); el.value = String([...el.options].findIndex((o) => o.text === 'Stromal')); el.dispatchEvent(new Event('change', { bubbles: true })); })()`);
      await waitFor(`document.getElementById('${id}')?.selectedOptions[0]?.text === 'Stromal'`);
    }
    await scrollTo('.sp-answer .sp-pair-tables', 'start');
    await sleep(300);
  },
  // Regions compared (wave 6, slice 4): two stromal regions of the Lung example drawn by their
  // stromal and endothelial cells, and each class's density in them and in the rest of the tissue,
  // the first region over the rest as a forest plot.
  async compared() {
    await act('open_example', { example: 'lung' });
    await act('cluster_cells', {});
    const markers = await act('label_cells', { markers: 'lung' });
    await act('draw_region', { dataset: 'xenium-lung-2fov', polygon: [[260, 0], [420, 0], [440, 80], [400, 170], [300, 170], [255, 100]], class: 'Stroma', name: 'Vessel stroma', by: 'docs/capture' });
    await act('draw_region', { dataset: 'xenium-lung-2fov', polygon: [[60, 470], [200, 460], [250, 560], [240, 700], [120, 760], [0, 720], [0, 640]], class: 'Stroma', name: 'Lower stroma', by: 'docs/capture' });
    await act('spatial_analysis', { question: 'regions', labels: markers.data.id, level: 1, regions: ['Vessel stroma', 'Lower stroma'] });
    await waitFor(`Boolean(document.querySelector('.sp-answer .sp-forest'))`);
    await scrollTo('.sp-answer .sp-regions-table', 'start');
    await sleep(300);
  },
  // Cellular neighborhoods (wave 6, slice 5): the Lung classes' six niches, their make-up against the
  // tissue's as a heat map, and each one's defining labels and leading genes, one niche chosen.
  async niches() {
    await act('open_example', { example: 'lung' });
    await act('cluster_cells', {});
    const markers = await act('label_cells', { markers: 'lung' });
    await act('spatial_analysis', { question: 'niches', labels: markers.data.id, level: 1 });
    await waitFor(`Boolean(document.querySelector('.sp-answer .sp-niches'))`);
    await js(`document.querySelector('.sp-niches tr[data-niche="5"]')?.click()`);
    await waitFor(`Boolean(document.querySelector('.sp-answer .sp-pair'))`);
    await scrollTo('.sp-answer .sp-heatmap-wrap', 'start');
    await sleep(300);
  },
  // The niches on the tissue (wave 6, slice 5): the Lung cells filled by their niche, the legend
  // naming each one.
  async 'niches-tissue'() {
    await act('open_example', { example: 'lung' });
    await act('cluster_cells', {});
    const markers = await act('label_cells', { markers: 'lung' });
    await act('spatial_analysis', { question: 'niches', labels: markers.data.id, level: 1 });
    await act('show_tissue', { dataset: 'xenium-lung-2fov', fill: 'niches', opacity: 0.85, transcripts: false, whole: true, channels: { DAPI: { color: '#8c8c8c' } } });
    await waitFor(`Boolean(window.situcraftViewer?.clusterFill?.())`);
    await js(`window.situcraftViewer.settled()`);
    await sleep(400);
  },
  // Spatially variable genes (wave 6, slice 6): the Lung genes' Moran's I over the Delaunay graph,
  // the genes in a pattern and dispersed, one gene chosen with its local Moran's I.
  async moran() {
    await act('open_example', { example: 'lung' });
    await act('build_graph', { kind: 'delaunay' });
    await act('spatial_analysis', { question: 'moran' });
    await waitFor(`Boolean(document.querySelector('.sp-answer .sp-genes'))`);
    await js(`document.querySelector('.sp-genes tr[data-gene="GJA5"]')?.click()`);
    await waitFor(`Boolean(document.querySelector('.sp-answer .sp-pair'))`);
    await scrollTo('.sp-answer .sp-pair-tables', 'start');
    await sleep(300);
  },
  // A gene's local Moran's I on the tissue (wave 6, slice 6): the Lung cells high among high in
  // GJA5's expression (an artery's endothelium, in the stroma at the section's top left), with its
  // transcripts.
  async 'moran-tissue'() {
    await act('open_example', { example: 'lung' });
    await act('build_graph', { kind: 'delaunay' });
    const moran = await act('spatial_analysis', { question: 'moran' });
    await act('show_tissue', { dataset: 'xenium-lung-2fov', cells_of: { analysis: moran.data.id, gene: 'GJA5', class: 'high' }, genes: ['GJA5'], center: [395, 115], width_um: 300, channels: { DAPI: { color: '#8c8c8c' } } });
    await waitFor(`Boolean(window.situcraftViewer?.focus?.())`);
    await js(`window.situcraftViewer.settled()`);
    await sleep(400);
  },
  // An answer made again on another segmentation (wave 6, slice 7): the Lung classes' enrichment
  // made again on a 3 µm nucleus expansion, the two read together, the largest change chosen with
  // the cells behind it.
  async remake() {
    await act('open_example', { example: 'lung' });
    await act('expand_nuclei', { dataset: 'xenium-lung-2fov', distance_um: 3 });
    await act('cluster_cells', {});
    const markers = await act('label_cells', { markers: 'lung' });
    await act('build_graph', { segmentation: 'instrument', kind: 'delaunay' });
    const first = await act('spatial_analysis', { segmentation: 'instrument', question: 'enrichment', labels: markers.data.id, level: 1 });
    await act('remake_analysis', { analysis: first.data.id, segmentation: 'Nucleus expansion, 3 µm' });
    await waitFor(`Boolean(document.querySelector('.sp-changes tbody tr'))`);
    await js(`document.querySelector('.sp-changes tbody tr')?.click()`);
    await waitFor(`Boolean(document.querySelector('.sp-behind [data-action="behind-a"]'))`);
    await scrollTo('.sp-remake', 'start');
    await sleep(300);
  },
  // Across a region's edge (wave 6, slice 4): the epithelial cells' profile across the vessel
  // stroma's edge, sparse inside and dense outside, with each class's reading.
  async edge() {
    await act('open_example', { example: 'lung' });
    await act('cluster_cells', {});
    const markers = await act('label_cells', { markers: 'lung' });
    await act('draw_region', { dataset: 'xenium-lung-2fov', polygon: [[260, 0], [420, 0], [440, 80], [400, 170], [300, 170], [255, 100]], class: 'Stroma', name: 'Vessel stroma', by: 'docs/capture' });
    await act('spatial_analysis', { question: 'boundary', labels: markers.data.id, level: 1, region: 'Vessel stroma' });
    await waitFor(`Boolean(document.querySelector('.sp-answer .sp-profile'))`);
    await scrollTo('.sp-answer .sp-readings', 'start');
    await sleep(300);
  },
  // The Breast example's clusters on the tissue (wave 4, slice 1): the cells filled by cluster,
  // with the legend.
  async clusters() {
    await act('open_example', { example: 'breast' });
    await act('cluster_cells', {});
    await act('show_tissue', { fill: 'clusters', opacity: 0.8, transcripts: false, center: [620, 380], um_per_pixel: 0.75, channels: { DAPI: { color: '#8c8c8c' } } });
    await waitFor(`Boolean(window.situcraftViewer?.clusterFill?.())`);
    await js(`window.situcraftViewer.settled()`);
  },
  // The Lung example's instrument segmentation exported as SpatialData, AnnData and Xenium
  // Ranger's inputs: the formats, the folder, the export listed with the prepared command
  // (wave 2, slice 7).
  async export() {
    await act('open_example', { example: 'lung' });
    await act('export_segmentation', { formats: ['spatialdata', 'anndata', 'xenium-ranger'], folder: join(places.exports, 'xenium-lung-2fov-instrument') });
    await scrollTo('.seg-exports', 'end');
  },
  // The Cohort view (wave 7, slice 1): the Lung and Breast sections added together as a project's
  // samples, named, with a field given them and those read from their bundles.
  async samples() {
    await act('new_project', { name: 'Two sections' });
    await act('open_paths', { paths: ['xenium-lung-2fov', 'xenium-breast-2fov'].map((name) => join(DATA, name)), add: true });
    await act('set_samples', {
      fields: [{ name: 'tissue', kind: 'text' }],
      names: { 'xenium-lung-2fov': 'Lung section', 'xenium-breast-2fov': 'Breast section' },
      values: { 'xenium-lung-2fov': { tissue: 'lung' }, 'xenium-breast-2fov': { tissue: 'breast' } },
    });
    await act('samples');
    // The inspector closed (⇧⌘\), so that the table shows every column.
    await js(`document.getElementById('app').classList.add('inspector-closed')`);
    await clearToasts();
    await sleep(400);
  },
  // The design (wave 7, slice 3): the simulated cohort's subjects compared by condition, adjusted
  // for the slide, said in words with its rows and matrix.
  async design() {
    await act('open_example', { example: 'cohort' });
    await act('design', { unit: 'subject', group: 'condition', reference: 'control', covariates: ['slide'] });
    await js(`document.getElementById('app').classList.add('inspector-closed')`);
    await sleep(600);
    await scrollTo('.cohort-design');
    await clearToasts();
  },
  // Summaries (wave 7, slice 4): the same steps on every sample of the simulated cohort, the samples
  // summarized, and the cohort table's shares of the labeled cells.
  async summary() {
    await act('open_example', { example: 'cohort' });
    await act('cohort_steps', { questions: ['enrichment'] });
    await act('summarize_samples', {});
    await act('cohort_table', { family: 'share' });
    await js(`document.getElementById('app').classList.add('inspector-closed')`);
    await sleep(600);
    await scrollTo('.cohort-summary');
    await clearToasts();
  },
  // The Jobs view after both examples were opened: each job's state and times, one's log open.
  async jobs() {
    await act('open_example', { example: 'lung' });
    await act('new_project', { name: 'Breast' });
    await act('open_example', { example: 'breast' });
    await act('jobs', { show: true });
    // Its log, once the list has settled (a job finishing redraws it).
    await sleep(1000);
    await waitFor(`(() => { const card = [...document.querySelectorAll('.job-card')].find((c) => c.textContent.startsWith('Derived data of xenium-breast')); const toggle = card?.querySelector('.job-foot button.link'); if (!toggle) return false; if (toggle.getAttribute('aria-expanded') !== 'true') toggle.click(); return true; })()`);
    await waitFor(`document.querySelectorAll('.job-event').length > 3`);
  },
};

// --- Run ------------------------------------------------------------------------------------------

// axe-core in the page: violations of the WCAG 2.1 A and AA rules, with up to five elements each.
async function runAxe() {
  await js(`if (!window.axe) (0, eval)(${JSON.stringify(readFileSync(AXE, 'utf8'))});`);
  return js(`(async () => {
    const result = await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] }, resultTypes: ['violations'] });
    return result.violations.map((v) => ({ id: v.id, impact: v.impact, help: v.help, nodes: v.nodes.slice(0, 5).map((n) => ({ target: n.target.join(' '), summary: n.failureSummary?.split('\\n').slice(0, 3).join(' ') })), count: v.nodes.length }));
  })()`);
}
const audits = [];
const AUDIT_FILE = join(ROOT, 'docs/capture/audit.json');

// Scene names, from the definitions in this file: in an object literal a repeated name silently
// replaces the scene before it, and its pictures overwrite the other's.
{
  const source = readFileSync(fileURLToPath(import.meta.url), 'utf8');
  const block = source.slice(source.indexOf('const scenes = {'), source.indexOf('\n};\n', source.indexOf('const scenes = {')));
  const defined = [...block.matchAll(/^ {2}async '?([\w-]+)'?\(\) \{/gm)].map((m) => m[1]);
  const repeated = defined.filter((name, i) => defined.indexOf(name) !== i);
  if (repeated.length) throw new Error(`Scenes defined twice: ${[...new Set(repeated)].join(', ')}. Rename one: a scene's name is the name of its pictures.`);
  if (defined.length !== Object.keys(scenes).length) throw new Error(`Found ${defined.length} scene definitions but ${Object.keys(scenes).length} scenes; keep each scene as "  async name() {" so they can be checked.`);
}

const names = wanted.length ? wanted : Object.keys(scenes);
for (const name of names) if (!scenes[name]) throw new Error(`Unknown scene ${name}. Scenes: ${Object.keys(scenes).join(', ')}`);
mkdirSync(IMAGES, { recursive: true });
await new Promise((done, fail) => spawn('go', ['build', '-o', binary, '.'], { cwd: ROOT, stdio: 'inherit' }).on('exit', (code) => (code ? fail(new Error('go build failed (is Go installed?)')) : done())));
try {
  for (const theme of themes) {
    for (const name of names) {
      let situcraft = null;
      b = null;
      try {
        // The start page shows the examples as a first run does: not yet downloaded.
        situcraft = await startSituCraft(name === 'start' ? { examples: mkdtempSync(join(temp, 'examples-')) } : name === 'jobs' ? { cache: mkdtempSync(join(temp, 'cache-')) } : {});
        // A Chrome that does not start is started again once; a scene that cannot start fails
        // alone, not the scenes after it.
        b = await launch({ width: 1600, height: 1000, scale: 1.25 }).catch(async () => {
          await sleep(2000);
          return launch({ width: 1600, height: 1000, scale: 1.25 });
        });
        await b.theme(theme === 'dark');
        await b.goto(URL, 800);
        await waitFor('Boolean(window.situcraft?.remote)', 30000);
        // No commit in the status bar: the pictures are of the release, not of a build.
        await js(`(() => { const app = window.situcraft; app.commit = ''; app.store.notify(['library']); return true; })()`);
        await scenes[name]();
        await clearToasts();
        await sleep(800);
        await showPlaces();
        if (shots) await b.capture(join(IMAGES, `${name}-${theme}.webp`), { format: 'webp', quality: 86 });
        if (audit) {
          const found = await runAxe();
          audits.push({ scene: name, theme, violations: found });
          console.log(`${name} (${theme}): ${found.length ? found.map((v) => `${v.id} ×${v.count} (${v.impact})`).join(', ') : 'no violations'}`);
        } else console.log(`${name} (${theme})`);
      } catch (error) {
        console.error(`${name} (${theme}) failed: ${error.message}`);
        process.exitCode = 1;
      } finally {
        await b?.close();
        await situcraft?.stop();
      }
    }
  }
} finally {
  rmSync(temp, { recursive: true, force: true });
  if (audit) {
    // Merged with the audits of the scenes not run now; scenes that no longer exist are dropped.
    let previous = [];
    try {
      previous = JSON.parse(readFileSync(AUDIT_FILE, 'utf8'));
    } catch { /* none yet */ }
    const key = (a) => `${a.scene}|${a.theme}`;
    const fresh = new Map(audits.map((a) => [key(a), a]));
    const order = Object.keys(scenes);
    const merged = [...previous.filter((a) => !fresh.has(key(a)) && scenes[a.scene]), ...audits]
      .sort((a, c) => order.indexOf(a.scene) - order.indexOf(c.scene) || a.theme.localeCompare(c.theme));
    writeFileSync(AUDIT_FILE, `${JSON.stringify(merged, null, 1)}\n`);
    const failing = merged.filter((a) => a.violations.length);
    console.log(`\naxe-core: ${audits.length} scene captures; docs/capture/audit.json: ${merged.length} captures of ${order.length} scenes, ${failing.length} with violations${failing.length ? ` (${failing.slice(0, 6).map((a) => `${a.scene} ${a.theme}`).join(', ')})` : ''}.`);
  }
}
