// The tissue viewer against its data, in headless Chrome (roadmap, wave 1, slice 5):
//
// - every channel of each development bundle drawn at full resolution, at level 1 and at the
//   coarsest level (across tile edges and at the image's edges) equals the derived data's tiles
//   sample for sample, read back before any window or color is applied (budget: no difference);
// - the transcripts the viewer loads for a region equal the program's direct query of the index
//   for that region, for chosen genes and for all transcripts;
// - frames per second while panning at cellular and whole-tissue zoom, and the time for a new and
//   a revisited view to be drawn whole (budgets: 30 frames per second, where the browser has a GPU;
//   500 ms for a revisited view; 500 ms for a region query);
// - the QC view (wave 1, slice 6): every recomputable run metric agrees with the recorded one;
//   brushing a range chooses exactly the cells in it, which the Viewer then shows; a saved filter
//   keeps the cells an independent evaluation here keeps, recorded under the same SHA-256; every
//   QC map draws;
// - the examples and the record (wave 1, slice 7): the examples are listed and none is downloaded
//   unasked; every step of each example's tour opens with its genes; the project saved by SituCraft
//   0.1.0 (validation/projects) opens, finds its moved bundle again, its filters keep the cells they
//   recorded, and its saved views reopen exactly, from the project and from a link;
// - segmentations as artifacts (wave 2, slice 2): each bundle's instrument segmentation registered,
//   its files' SHA-256 as Node computes them, promoted as the default; the 0.1.0 project given its
//   own, the same artifact; the jobs all complete;
// - remote control (wave 2, slice 1): the window driven by a program through the program's
//   remote-control hub: the state described, the tissue shown as asked, a view saved and reopened
//   exactly, a filter, the methods, and a file written only with the token;
// - nucleus expansion (wave 2, slice 3): the Lung bundle's nuclei grown by 5 µm by a program, in
//   the Segmentation view, nearly as the instrument's segmentation; recorded in the project; the
//   same distance again the same artifact, with no job;
// - imports (wave 2, slice 4): the Lung bundle imported into itself as an output bundle of its own
//   run, by remote control and only with the token; another run's refused;
// - comparisons (wave 2, slice 5): the instrument's segmentation and the expansion compared by a
//   program, as the expansion's own comparison counted, recorded and shown;
// - the comparison workspace (wave 2, slice 6): its review queue ranked as asked, both
//   segmentations drawn with one camera in each mode, decisions recorded as the rules allow;
// - segmentations on the tissue (wave 3, slice 8): each kind (an expansion, an import, a
//   refinement, errors planted for practice) drawn as its files hold it: the cell found at points
//   of each the one its label raster holds there, and the fill drawn there the color of that
//   cell's value; the list, its default first; a saved view reopening with its segmentation and
//   cell; show_tissue and select_cell in a segmentation; transcripts with their probabilities;
// - the examples and the release (wave 3, slice 9): the simulated example made here; the worked
//   examples refining the Breast data and scoring segmentations against the simulated truth, as
//   their cards expect; the projects saved by 0.2.0 and 0.3.0 reopened, every artifact they record
//   made again with the same checksums;
// - the examples and the release (wave 4, slice 9): the worked examples clustering and labeling the
//   Lung cells, reading the Breast labels under two segmentations, and the simulated tissue's labels
//   against its truth, as their cards expect; the project saved by 0.4.0 reopened, each clustering,
//   map and label set from markers it records made again with the same summary;
// - post-Xenium images (wave 5, slice 1): the pathology fixture's bundle (XOA 1.0.1) opened and its
//   H&E added by a program, each of three of its pixels drawn where its alignment puts it.
//
// It builds the program, starts it on the bundles with an empty cache and library, builds their
// derived data, and drives the page through window.situcraft (the app, app.js) and window.situcraftViewer
// (ui/mode-viewer.js). Run by validation/run.mjs's "viewer" suite.

import { execFileSync, spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { findChrome, launch, sleep } from './cdp.mjs';
import { inflateSync } from 'node:zlib';
import { attributeRange, readCells } from '../web/lib/viewer-data.js';
import { certaintyColor, readSegCells } from '../web/lib/segview.js';
import { colormapLUT, hexToRgb } from '../web/lib/colormaps.js';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const BUNDLES = ['xenium-lung-2fov', 'xenium-breast-2fov'];
const BUDGET = { fps: 30, revisitMs: 500, queryMs: 500 };

function startProgram(bin, args) {
  const program = spawn(bin, args, { stdio: ['ignore', 'pipe', 'pipe'] });
  const url = new Promise((resolve, reject) => {
    let out = '';
    const timer = setTimeout(() => reject(new Error(`the program did not start: ${out}`)), 60000);
    program.stdout.on('data', (chunk) => {
      out += chunk;
      const m = out.match(/is running at (http:\/\/\S+)/);
      if (m) {
        clearTimeout(timer);
        resolve(m[1]);
      }
    });
    program.on('exit', (code) => reject(new Error(`the program exited (${code}): ${out}`)));
  });
  return { program, url };
}

async function json(url, options) {
  const response = await fetch(url, options);
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status} ${await response.text()}`);
  return response.json();
}

// A segmentation's label raster (internal/segment mask.bin: a header, then zlib: width, height,
// pixel size, each row's (label, length) runs as unsigned varints): { pixel, label(x, y) }.
function readMask(buffer) {
  if (buffer.subarray(0, 4).toString() !== 'SCMK') throw new Error('not a label raster');
  const body = inflateSync(buffer.subarray(16));
  let at = 0;
  const width = body.readUInt32LE(0);
  const height = body.readUInt32LE(4);
  const pixel = body.readDoubleLE(8);
  at = 16;
  const uvarint = () => {
    let x = 0;
    for (let shift = 0; ; shift += 7) {
      const b = body[at];
      at += 1;
      x += (b & 0x7f) * 2 ** shift;
      if (b < 0x80) return x;
    }
  };
  const rows = [];
  for (let y = 0; y < height; y += 1) {
    const runs = [];
    for (let n = uvarint(); n > 0; n -= 1) runs.push([uvarint(), uvarint()]);
    rows.push(runs);
  }
  return {
    width, height, pixel,
    label(x, y) {
      let px = 0;
      for (const [label, length] of rows[y] ?? []) {
        if (x < px + length) return label;
        px += length;
      }
      return 0;
    },
  };
}

// The points answer (internal/view): [{ gene, x, y }].
function readPoints(buffer) {
  const words = new Uint32Array(buffer, 4, 3);
  const [n, g] = [words[0], words[1]];
  let at = 16;
  const starts = new Uint32Array(buffer, at, g + 1);
  at += 4 * (g + 1);
  const x = new Float32Array(buffer, at, n);
  const y = new Float32Array(buffer, at + 4 * n, n);
  const genes = new Uint16Array(buffer, at + 12 * n, g);
  const out = [];
  for (let k = 0; k < g; k += 1) for (let i = starts[k]; i < starts[k + 1]; i += 1) out.push({ gene: genes[k], x: x[i], y: y[i] });
  return out;
}

const key = (p) => `${p.gene}:${p.x}:${p.y}`;

export async function viewerSuite({ check, MissingData, verbose }) {
  const dataRoot = process.env.SITUCRAFT_DATA ?? join(root, 'validation', 'cache');
  const present = BUNDLES.filter((b) => existsSync(join(dataRoot, b, 'experiment.xenium')));
  if (!present.length) throw new MissingData('the development bundles (node validation/fetch.mjs)');
  if (!findChrome()) throw new MissingData('Chrome, Chromium, Edge or Brave (set CHROME)');
  const work = mkdtempSync(join(tmpdir(), 'situcraft-viewer-'));
  const bin = join(work, process.platform === 'win32' ? 'situcraft.exe' : 'situcraft');
  execFileSync('go', ['build', '-o', bin, '.'], { cwd: root, stdio: 'inherit' });
  const { program, url: started } = startProgram(bin, ['--remote-control', '--window', 'none', '--port', '8857', '--data-dir', join(work, 'library'), '--cache-dir', join(work, 'cache'), ...present.map((b) => join(dataRoot, b))]);
  let chrome = null;
  try {
    const url = await started;
    // The derived data, built as the program builds them for a project.
    const info = await json(`${url}/api/info`);
    for (const [i, file] of info.files.entries()) {
      const answer = await json(`${url}/api/local/${i}/cache`, { method: 'POST' });
      const t0 = performance.now();
      let job = answer.job;
      while (job && !['complete', 'cancelled', 'failed', 'invalid'].includes(job.state)) {
        await sleep(250);
        job = await json(`${url}/api/jobs/${job.id}`);
      }
      check('viewer', `${file.name}: derived data built`, job ? `${job.state} in ${((performance.now() - t0) / 1000).toFixed(1)} s` : 'already built', !job || job.state === 'complete', 'complete');
    }
    chrome = await launch({ width: 1800, height: 1150, scale: 1, args: (process.env.SITUCRAFT_CHROME_ARGS ?? '').split(' ').filter(Boolean) });
    const errors = [];
    chrome.on('Runtime.exceptionThrown', (e) => errors.push(e.exceptionDetails?.exception?.description ?? e.exceptionDetails?.text));
    await chrome.goto(url, 3000);
    const gpu = await chrome.eval(`(() => { const gl = document.createElement('canvas').getContext('webgl2'); if (!gl) return 'none'; const d = gl.getExtension('WEBGL_debug_renderer_info'); return d ? gl.getParameter(d.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER); })()`);
    const software = /swiftshader|llvmpipe|software|none/i.test(gpu);
    check('viewer', 'the browser\'s WebGL2 renderer', gpu, gpu !== 'none', 'WebGL2');

    for (const [index, name] of info.files.map((f) => f.name).entries()) {
      // Into the project, and into the viewer.
      const opened = await chrome.eval(`(async () => {
        const app = window.situcraft;
        const item = app.bundles.items().find((b) => b.name === ${JSON.stringify(name)});
        await app.bundles.validate(item);
        const record = app.bundles.datasetFor(item) ?? await app.bundles.add(item);
        app.focusItem({ kind: 'dataset', id: record.id });
        app.setMode('viewer');
        for (let k = 0; k < 300; k += 1) {
          if (window.situcraftViewer?.desc.name === ${JSON.stringify(name)}) return true;
          await new Promise((r) => setTimeout(r, 100));
        }
        return false;
      })()`);
      check('viewer', `${name}: opens in the viewer`, String(opened), opened, 'true');
      if (!opened) continue;
      // Its instrument segmentation, registered as an artifact: each file's SHA-256 against Node's.
      const seg = await chrome.eval(`(async () => {
        const app = window.situcraft;
        const dataset = app.store.project.datasets.find((d) => d.name === ${JSON.stringify(name)});
        for (let k = 0; k < 600; k += 1) {
          const record = app.store.project.artifacts.find((a) => a.kind === 'segmentation' && a.dataset === dataset.id);
          if (record) {
            const { promotedSegmentation } = await import('/lib/segmentations.js');
            return { record, promoted: promotedSegmentation(app.store.project, dataset.id)?.record.id === record.id };
          }
          await new Promise((r) => setTimeout(r, 100));
        }
        return null;
      })()`);
      if (seg) {
        const differing = seg.record.files.filter((f) => createHash('sha256').update(readFileSync(join(dataRoot, name, f.path))).digest('hex') !== f.sha256).map((f) => f.path);
        check('viewer', `${name}: its instrument segmentation is an artifact, every file's SHA-256 as Node computes it`, `${seg.record.id}, ${seg.record.files.length} files${differing.length ? `; differing: ${differing.join(', ')}` : ', all the same'}; ${seg.record.state}${seg.promoted ? ', the default' : ''}`, seg.record.state === 'complete' && seg.promoted && differing.length === 0 && seg.record.files.length >= 6, 'complete, the default, every checksum the same');
        const cells = seg.record.method.methods.reduce((n, m) => n + m.cells, 0);
        check('viewer', `${name}: its segmentation's methods count every cell`, seg.record.method.methods.map((m) => `${m.method.replace(/^Segmented /, '')}: ${m.cells}`).join('; '), cells === seg.record.summary.cells && cells > 0, 'their sum is the cells');
      } else check('viewer', `${name}: its instrument segmentation is an artifact`, 'not registered', false, 'registered');
      const desc = await chrome.eval('window.situcraftViewer.desc');

      // --- Every channel's samples, exactly ---
      let compared = 0;
      let differing = 0;
      const where = [];
      for (const [c, channel] of desc.channels.entries()) {
        const top = channel.levels.length - 1;
        for (const level of [...new Set([0, 1, top])]) {
          const lv = channel.levels[level];
          const regions = level === top
            ? [{ x0: 0, y0: 0, w: Math.min(lv.width, 512), h: Math.min(lv.height, 512) }]
            : [{ x0: 384, y0: 384, w: 256, h: 256 }, { x0: lv.width - 200, y0: lv.height - 150, w: 200, h: 150 }];
          for (const r of regions) {
            const got = await chrome.eval(`window.situcraftViewer.rawCapture(${JSON.stringify({ channel: c, level, ...r })}).then((a) => Array.from(a))`);
            // The reference: the derived data's tiles, inflated and joined here.
            const want = new Uint16Array(r.w * r.h);
            const t = desc.grid.tile;
            for (let ty = Math.floor(r.y0 / t); ty <= Math.floor((r.y0 + r.h - 1) / t); ty += 1) {
              for (let tx = Math.floor(r.x0 / t); tx <= Math.floor((r.x0 + r.w - 1) / t); tx += 1) {
                const response = await fetch(`${url}/api/cache/${desc.entries.image}/image/${c}/${level}/${tx}/${ty}`);
                const w = Number(response.headers.get('X-Tile-Width'));
                const h = Number(response.headers.get('X-Tile-Height'));
                const split = new Uint8Array(await response.arrayBuffer());
                for (let y = 0; y < h; y += 1) {
                  for (let x = 0; x < w; x += 1) {
                    const px = tx * t + x - r.x0;
                    const py = ty * t + y - r.y0;
                    if (px < 0 || py < 0 || px >= r.w || py >= r.h) continue;
                    want[py * r.w + px] = split[y * w + x] | (split[w * h + y * w + x] << 8);
                  }
                }
              }
            }
            let diff = 0;
            for (let i = 0; i < want.length; i += 1) if (got[i] !== want[i]) diff += 1;
            compared += want.length;
            differing += diff;
            if (diff) where.push(`${channel.name} level ${level} at (${r.x0}, ${r.y0}): ${diff}`);
          }
        }
      }
      check('viewer', `${name}: samples drawn, of ${desc.channels.length} channels at levels 0, 1 and the coarsest, against the derived data's tiles`, differing ? `${differing} of ${compared} differ (${where.slice(0, 4).join('; ')})` : `${compared} samples, all equal`, compared > 0 && differing === 0, '0 differences');

      // --- Points of a region equal the program's query ---
      const genes = desc.transcripts.genes.map((g, i) => [g, i]).filter(([g]) => g.kind === 'gene').sort((a, b) => b[0].q20 - a[0].q20).slice(0, 3).map(([, i]) => i);
      const rect = { x0: 300, y0: 200, x1: 500, y1: 350 };
      for (const [label, patch, query] of [
        ['three genes', { all: false, visible: true, minQ: 20 }, `genes=${genes.join(',')}&minq=20`],
        ['all transcripts', { all: true, visible: true, minQ: 0 }, 'minq=0'],
      ]) {
        const loaded = await chrome.eval(`(async () => {
          const v = window.situcraftViewer;
          v.setGenes(${JSON.stringify(genes)});
          v.setTranscripts(${JSON.stringify(patch)});
          v.setCamera(v.cameraFor(${JSON.stringify(rect)}));
          await v.settled();
          return { mode: v.plan().transcripts?.mode, points: v.loadedPoints(${JSON.stringify(rect)}) };
        })()`);
        const t0 = performance.now();
        await fetch(`${url}/api/local/${index}/points?x0=${rect.x0}&y0=${rect.y0}&x1=${rect.x1}&y1=${rect.y1}&${query}`).then((r) => r.arrayBuffer());
        const coldMs = performance.now() - t0;
        const t1 = performance.now();
        const reference = readPoints(await fetch(`${url}/api/local/${index}/points?x0=${rect.x0}&y0=${rect.y0}&x1=${rect.x1}&y1=${rect.y1}&${query}`).then((r) => r.arrayBuffer()));
        const warmMs = performance.now() - t1;
        const a = loaded.points.map(key).sort();
        const b = reference.map(key).sort();
        const same = a.length === b.length && a.every((k, i) => k === b[i]);
        check('viewer', `${name}: transcripts loaded for a 200 × 150 µm region (${label}) against the program's query of the index`, `${a.length} drawn as ${loaded.mode}, ${b.length} queried${same ? ', the same' : ', differing'}`, loaded.mode === 'points' && b.length > 0 && same, 'the same');
        check('viewer', `${name}: region query time (${label})`, `${coldMs.toFixed(0)} ms, then ${warmMs.toFixed(0)} ms`, warmMs <= BUDGET.queryMs, `≤ ${BUDGET.queryMs} ms once warm`);
      }

      // --- Frame rate and the time to draw a view ---
      const perf = await chrome.eval(`(async () => {
        const v = window.situcraftViewer;
        v.setTranscripts({ all: false, visible: true, minQ: 20 });
        const close = v.cameraFor({ x0: 100, y0: 250, x1: 400, y1: 450 });
        const farther = { ...close, cx: close.cx + 700 };
        const whole = v.cameraFor({ x0: 0, y0: 0, x1: ${desc.grid.width * desc.grid.pixelSize}, y1: ${desc.grid.height * desc.grid.pixelSize} });
        const newView = await v.timeTo(close);
        const cold = await v.pan(close, farther, 2500);
        const warm = await v.pan(farther, close, 2500);
        const overview = await v.pan(whole, { ...whole, scale: whole.scale * 3 }, 2500);
        await v.timeTo(whole);
        const revisit = await v.timeTo(close);
        return { newView, revisit, cold, warm, overview };
      })()`);
      const fps = (p) => `${p.fps.toFixed(0)} fps (${p.frames} frames; 95% within ${p.p95ms.toFixed(0)} ms; drawing ${p.drawMs.toFixed(1)} ms)`;
      check('viewer', `${name}: panning at cellular zoom, data still arriving`, fps(perf.cold), true, 'reported');
      check('viewer', `${name}: panning at cellular zoom, data loaded`, fps(perf.warm), software || perf.warm.fps >= BUDGET.fps, software ? 'reported (software WebGL)' : `≥ ${BUDGET.fps} fps`);
      check('viewer', `${name}: zooming over the whole tissue`, fps(perf.overview), software || perf.overview.fps >= BUDGET.fps, software ? 'reported (software WebGL)' : `≥ ${BUDGET.fps} fps`);
      check('viewer', `${name}: a new view drawn whole`, `${perf.newView.toFixed(0)} ms`, true, 'reported');
      check('viewer', `${name}: a revisited view drawn whole`, `${perf.revisit.toFixed(0)} ms`, perf.revisit <= BUDGET.revisitMs, `≤ ${BUDGET.revisitMs} ms`);
      if (verbose) console.log(name, JSON.stringify(perf));

      // --- The QC view ---
      const qc = await chrome.eval(`(async () => {
        window.situcraft.setMode('qc');
        for (let k = 0; k < 300 && window.situcraftQC?.desc.name !== ${JSON.stringify(name)}; k += 1) await new Promise((r) => setTimeout(r, 100));
        const q = window.situcraftQC;
        if (!q) return null;
        const rows = q.metricRows();
        return { checked: rows.filter((r) => r.agrees !== null).length, agree: rows.filter((r) => r.agrees === true).length, differing: rows.filter((r) => r.agrees === false).map((r) => r.name) };
      })()`);
      check('viewer', `${name}: run metrics in the QC view, recomputed from the data`, qc ? `${qc.agree} of ${qc.checked} agree${qc.differing.length ? `; differing: ${qc.differing.join(', ')}` : ''}` : 'the QC view did not open', qc && qc.checked >= 20 && qc.agree === qc.checked, 'all agree (at least 20)');
      if (!qc) continue;
      // The cells table, read here to evaluate brushes and filters independently of the page.
      const cells = readCells(await fetch(`${url}/api/local/${index}/cells`).then((r) => r.arrayBuffer()));
      const chosen = await chrome.eval('window.situcraftQC.brush("transcripts", 20, 100)');
      let direct = 0;
      for (let i = 0; i < cells.n; i += 1) if (cells.transcripts[i] >= 20 && cells.transcripts[i] <= 100) direct += 1;
      check('viewer', `${name}: brushing gene transcripts 20–100 chooses`, `${chosen} cells; counted here, ${direct}`, chosen === direct && direct > 0, 'the same');
      const linked = await chrome.eval(`(async () => {
        window.situcraft.setMode('viewer');
        for (let k = 0; k < 100 && !window.situcraftViewer; k += 1) await new Promise((r) => setTimeout(r, 100));
        await new Promise((r) => setTimeout(r, 300));
        return Boolean(window.situcraftViewer?.renderer.highlight);
      })()`);
      check('viewer', `${name}: the cells chosen in the QC view show in the Viewer`, String(linked), linked, 'true');
      const conditions = [{ attribute: 'transcripts', min: 20, max: null }, { attribute: 'area', min: 15, max: 400 }];
      const saved = await chrome.eval(`(async () => {
        window.situcraft.setMode('qc');
        for (let k = 0; k < 300 && window.situcraftQC?.desc.name !== ${JSON.stringify(name)}; k += 1) await new Promise((r) => setTimeout(r, 100));
        return window.situcraftQC.saveFilter('Harness filter', ${JSON.stringify(conditions)});
      })()`);
      // The same filter evaluated here: one bit per cell, the first cell in the lowest bit, after
      // the count of cells (lib/filters.js keepChecksum), hashed by Node's own SHA-256.
      const bits = Buffer.alloc(4 + Math.ceil(cells.n / 8));
      bits.writeUInt32LE(cells.n, 0);
      let kept = 0;
      for (let i = 0; i < cells.n; i += 1) {
        const area = cells.area[i];
        if (cells.transcripts[i] >= 20 && area >= 15 && area <= 400) {
          kept += 1;
          bits[4 + (i >> 3)] |= 1 << (i & 7);
        }
      }
      const sum = createHash('sha256').update(bits).digest('hex');
      check('viewer', `${name}: a saved filter's cells against an evaluation here`, `${saved.result.kept} kept (here ${kept}); SHA-256 ${saved.result.sha256 === sum ? 'the same' : 'different'}`, saved.result.kept === kept && saved.result.sha256 === sum, 'the same cells and checksum');
      const maps = await chrome.eval(`(async () => {
        const q = window.situcraftQC;
        const out = [];
        for (const id of ['cell:transcripts', 'cell:method', 'bin:outside', 'bin:quality', 'bin:controls', 'bin:density']) {
          q.setMap(id);
          await q.settled();
          out.push([id, q.renderer.gl.getError()]);
        }
        return out;
      })()`);
      const bad = maps.filter(([, e]) => e !== 0);
      check('viewer', `${name}: every QC map draws`, bad.length ? bad.map(([id, e]) => `${id}: GL error ${e}`).join('; ') : `${maps.length} maps, no WebGL error`, bad.length === 0, 'no error');
    }

    // --- The examples and their tours (wave 1, slice 7) ---
    const examples = await json(`${url}/api/examples`);
    const downloads = examples.examples.filter((e) => !e.generated);
    const generated = examples.examples.filter((e) => e.generated);
    check('viewer', 'the example datasets are listed, and none is downloaded or made unasked', examples.examples.map((e) => `${e.name}: ${e.state}`).join(', '),
      downloads.length === 2 && downloads.every((e) => e.state === 'absent' && e.download > 0 && e.license) && generated.length === 1 && generated[0].name === 'simulated-standard' && generated[0].state === 'absent' && !generated[0].download,
      'two to download and one to make, all absent');
    for (const name of info.files.map((f) => f.name)) {
      const toured = await chrome.eval(`(async () => {
        const app = window.situcraft;
        const { TOURS } = await import('/lib/tours.js');
        const { startTour } = await import('/ui/tour.js');
        const tour = TOURS[${JSON.stringify(name)}];
        const item = app.bundles.items().find((b) => b.name === ${JSON.stringify(name)});
        const dataset = app.bundles.datasetFor(item);
        if (!tour || !dataset) return null;
        const wait = async (f) => { for (let k = 0; k < 300; k += 1) { if (f()) return true; await new Promise((r) => setTimeout(r, 100)); } return false; };
        const tourCard = startTour(app, { tour, dataset: dataset.id });
        const problems = [];
        for (const [i, step] of tour.steps.entries()) {
          tourCard.go(i);
          const ok = await wait(() => app.store.ui.mode === step.mode && (step.mode === 'data' || (step.mode === 'viewer' ? window.situcraftViewer : window.situcraftQC)?.desc.name === ${JSON.stringify(name)}) && !app.pendingView);
          if (!ok) { problems.push(\`step \${i + 1} did not open\`); continue; }
          if (step.mode === 'viewer') {
            const genes = window.situcraftViewer.renderer.layers.transcripts.genes.length;
            if (genes !== (step.transcripts?.genes ?? []).length) problems.push(\`step \${i + 1}: \${genes} genes shown\`);
          }
        }
        tourCard.close();
        return { steps: tour.steps.length, problems };
      })()`);
      check('viewer', `${name}: every step of its tour opens, with its genes`, toured ? (toured.problems.length ? toured.problems.join('; ') : `${toured.steps} steps`) : 'no tour', toured && toured.problems.length === 0, 'every step');
    }

    // --- A project saved by 0.1.0 reopens exactly (validation/projects) ---
    if (present.includes('xenium-lung-2fov')) {
      const text = readFileSync(join(root, 'validation', 'projects', '0.1.0-lung.situcraft.json'), 'utf8');
      const fixture = JSON.parse(text);
      const lungIndex = info.files.findIndex((f) => f.name === 'xenium-lung-2fov');
      const reopened = await chrome.eval(`(async () => {
        const app = window.situcraft;
        const wait = async (f) => { for (let k = 0; k < 300; k += 1) { const v = f(); if (v) return v; await new Promise((r) => setTimeout(r, 100)); } return null; };
        const doc = ${JSON.stringify(text)};
        const id = JSON.parse(doc).id;
        await app.library.saveProject(id, doc);
        await app.openProject(id);
        const dataset = app.store.project.datasets[0];
        const before = (await wait(() => { const l = app.bundles.link(dataset.id); return l && l.state !== 'checking' ? l : null; }))?.state;
        const link = await app.bundles.locate(dataset.id, ${JSON.stringify(join(dataRoot, 'xenium-lung-2fov'))});
        return { before, after: link?.state, name: app.store.project.name, views: app.store.project.views.length, filters: app.store.project.artifacts.length };
      })()`);
      // A 0.1.0 project gets its datasets' instrument segmentations when its bundles are found: the
      // same artifact as the same bundle's in another project.
      const migrated = await chrome.eval(`(async () => {
        const app = window.situcraft;
        for (let k = 0; k < 600; k += 1) {
          const record = app.store.project.artifacts.find((a) => a.kind === 'segmentation');
          if (record) return record.id;
          await new Promise((r) => setTimeout(r, 100));
        }
        return null;
      })()`);
      const lungArtifact = (await json(`${url}/api/local/${lungIndex}/segmentation`)).artifact?.id;
      check('viewer', 'the 0.1.0 Lung project gets its instrument segmentation, the same artifact as the Lung dataset\'s elsewhere', `${migrated ?? 'none'}${migrated === lungArtifact ? ', the same' : `, not ${lungArtifact}`}`, Boolean(migrated) && migrated === lungArtifact, 'the same artifact');
      check('viewer', 'the 0.1.0 Lung project opens; its bundle, moved, is located again', reopened ? `${reopened.views} views, ${reopened.filters} filters; the bundle ${reopened.before}, then ${reopened.after}` : 'it did not open', reopened?.before === 'missing' && reopened?.after === 'ok' && reopened.views === fixture.views.length, 'missing, then ok');
      // Its filters, evaluated here, keep the cells they recorded.
      const cells = readCells(await fetch(`${url}/api/local/${lungIndex}/cells`).then((r) => r.arrayBuffer()));
      for (const filter of fixture.artifacts.filter((a) => a.kind === 'filter')) {
        const bits = Buffer.alloc(4 + Math.ceil(cells.n / 8));
        bits.writeUInt32LE(cells.n, 0);
        let kept = 0;
        for (let i = 0; i < cells.n; i += 1) {
          const value = (c) => cells[c.attribute][i];
          if (filter.conditions.every((c) => (c.min === null || value(c) >= c.min) && (c.max === null || value(c) <= c.max))) {
            kept += 1;
            bits[4 + (i >> 3)] |= 1 << (i & 7);
          }
        }
        const sum = createHash('sha256').update(bits).digest('hex');
        check('viewer', `0.1.0 Lung project: filter ${filter.name} version ${filter.version} keeps the cells it recorded`, `${kept} kept (recorded ${filter.result.kept}); SHA-256 ${sum === filter.result.sha256 ? 'the same' : 'different'}`, kept === filter.result.kept && sum === filter.result.sha256, 'the same cells and checksum');
      }
      // Its saved views reopen exactly, from the project tree and from a link.
      for (const view of fixture.views) {
        const same = await chrome.eval(`(async () => {
          const app = window.situcraft;
          const { sameView } = await import('/lib/views.js');
          const view = app.store.project.views.find((v) => v.id === ${JSON.stringify(view.id)});
          app.applyView(view);
          const target = () => (view.mode === 'qc' ? window.situcraftQC : window.situcraftViewer);
          for (let k = 0; k < 300 && !(app.store.ui.mode === view.mode && target()?.desc && !app.pendingView); k += 1) await new Promise((r) => setTimeout(r, 100));
          await new Promise((r) => setTimeout(r, 1500));
          const captured = target().captureView(view.name);
          return { same: sameView(captured, view), captured: JSON.stringify(captured.camera) + JSON.stringify(captured.cell) };
        })()`);
        check('viewer', `0.1.0 Lung project: view "${view.name}" reopens exactly`, same.same ? 'the same' : `differs: ${same.captured}`, same.same, 'the same');
      }
      const linked = await chrome.eval(`(async () => {
        const app = window.situcraft;
        const { sameView, viewLink } = await import('/lib/views.js');
        const view = app.store.project.views.find((v) => v.mode === 'viewer' && v.cell);
        location.hash = new URL(viewLink(location.href, app.store.project.id, view.id)).hash;
        for (let k = 0; k < 300 && !(app.store.ui.mode === 'viewer' && window.situcraftViewer?.desc && !app.pendingView); k += 1) await new Promise((r) => setTimeout(r, 100));
        await new Promise((r) => setTimeout(r, 1500));
        return sameView(window.situcraftViewer.captureView(view.name), view);
      })()`);
      check('viewer', '0.1.0 Lung project: a saved view\'s link reopens it exactly', String(linked), linked, 'true');
    }
    // --- Remote control (wave 2, slice 1): the same window driven by a program ---
    {
      const { token } = JSON.parse(readFileSync(join(work, 'library', 'remote.json'), 'utf8'));
      const act = async (action, args = {}, withToken = true) => {
        const response = await fetch(`${url}/api/remote/action`, { method: 'POST', headers: { 'Content-Type': 'application/json', ...(withToken ? { 'X-SituCraft-Token': token } : {}) }, body: JSON.stringify({ action, args, client: 'validation' }) });
        const body = await response.json();
        return { status: response.status, ...body };
      };
      await chrome.eval(`(async () => { for (let k = 0; k < 100 && !window.situcraft?.remote; k += 1) await new Promise((r) => setTimeout(r, 100)); return true; })()`);
      const state = await act('get_state');
      check('viewer', 'remote control: get_state describes the open project', state.ok ? state.message : state.message ?? state.error, state.ok && state.data.datasets.length >= 1 && state.data.views.length === 3, 'the 0.1.0 Lung project');
      const segmentations = await act('segmentations');
      check('viewer', 'remote control: segmentations lists the default one with its lineage', segmentations.message ?? segmentations.error, segmentations.ok && segmentations.data.segmentations.length === 1 && segmentations.data.segmentations[0].default && segmentations.data.segmentations[0].job?.id, 'one, the default, made by a job');
      const jobList = await act('jobs');
      const failedJobs = jobList.ok ? jobList.data.jobs.filter((j) => !['complete'].includes(j.state)) : [];
      check('viewer', 'remote control: jobs lists every job, all complete', jobList.message ?? jobList.error, jobList.ok && jobList.data.jobs.length >= 2 * info.files.length && failedJobs.length === 0, 'derived data and a segmentation for each bundle, complete');
      const lung = present.includes('xenium-lung-2fov');
      if (lung) {
        const shown = await act('show_tissue', { genes: ['EPCAM', 'PLVAP'], center: [750, 300], um_per_pixel: 0.4, cell: 'neddaiac-1', fill: 'transcripts' });
        const page = await chrome.eval('(() => { const v = window.situcraftViewer; const c = v.captureView(""); return { genes: c.transcripts.genes, camera: c.camera, cell: v.detail()?.id, fill: c.cells.fill }; })()');
        const agrees = shown.ok && page.genes.join() === 'EPCAM,PLVAP' && page.camera.cx === 750 && page.camera.cy === 300 && page.camera.umPerCss === 0.4 && page.cell === 'neddaiac-1' && page.fill === 'transcripts';
        check('viewer', 'remote control: show_tissue shows what it was asked, as the page has it', shown.ok ? JSON.stringify(page) : shown.message, agrees, 'the genes, place, zoom, cell and fill asked for');
        const saved = await act('save_view', { name: 'By remote control' });
        await act('show_tissue', { whole: true, genes: [] });
        const reopened = await act('open_view', { view: 'By remote control' });
        check('viewer', 'remote control: a view saved by a program reopens exactly', reopened.message ?? reopened.error, saved.ok && reopened.ok && reopened.data.exact === true, 'exactly as saved');
        const filter = await act('save_filter', { name: 'By remote control', conditions: [{ attribute: 'transcripts', min: 20 }, { attribute: 'area', min: 15, max: 400 }] });
        const recorded = JSON.parse(readFileSync(join(root, 'validation', 'projects', '0.1.0-lung.situcraft.json'), 'utf8')).artifacts.find((a) => a.version === 2).result;
        check('viewer', 'remote control: a filter saved by a program keeps the cells the 0.1.0 project\'s identical filter recorded', filter.ok ? `${filter.data.kept} kept, SHA-256 ${filter.data.sha256 === recorded.sha256 ? 'the same' : 'different'}` : filter.message, filter.ok && filter.data.kept === recorded.kept && filter.data.sha256 === recorded.sha256, 'the same cells and checksum');
        const methods = await act('methods', { dataset: 'xenium-lung-2fov' });
        check('viewer', 'remote control: the methods paragraph, citing the data and SituCraft', methods.ok ? `${methods.data.references.length} references` : methods.message, methods.ok && methods.data.text.includes(`SituCraft ${info.version}`) && methods.data.references.some((r) => r.includes('10x Genomics')), 'the version and the data cited');
        const target = join(work, 'project.json');
        const refused = await act('export', { what: 'project', path: target }, false);
        const exported = await act('export', { what: 'project', path: target });
        const written = existsSync(target) ? JSON.parse(readFileSync(target, 'utf8')) : null;
        check('viewer', 'remote control: export writes the project only with the token', `${refused.status} without, ${exported.ok ? 'written' : exported.message} with`, refused.status === 401 && exported.ok && written?.views?.length === 4 && written.artifacts.filter((a) => a.kind === 'filter').length === 3 && written.artifacts.filter((a) => a.kind === 'segmentation').length === 1, '401, then the project written');
        // Clusters (wave 4, slice 1): the instrument's segmentation clustered by a program, and its
        // clusters drawn on the tissue: each cell in its cluster's color, cells not analyzed clear.
        const clustered = await act('cluster_cells', { dataset: 'xenium-lung-2fov' });
        const shownClusters = await act('show_tissue', { fill: 'clusters', whole: true });
        const clusterFill = await chrome.eval(`(async () => {
          const v = window.situcraftViewer;
          for (let k = 0; k < 300 && !v.clusterFill(); k += 1) await new Promise((r) => setTimeout(r, 100));
          const f = v.clusterFill();
          if (!f) return null;
          const cells = await fetch('/api/clusterings/' + f.id + '/cells').then((r) => r.json());
          const { categoricalColor } = await import('/lib/colormaps.js');
          const rgba = v.fillColors();
          let wrong = 0;
          let clear = 0;
          for (let i = 0; i < cells.cluster.length; i += 1) {
            const c = cells.cluster[i];
            if (c < 0) {
              if (rgba[4 * i + 3] === 0) clear += 1;
              else wrong += 1;
              continue;
            }
            const hex = '#' + [0, 1, 2].map((j) => rgba[4 * i + j].toString(16).padStart(2, '0')).join('');
            if (hex !== categoricalColor(c).toLowerCase() || rgba[4 * i + 3] !== 255) wrong += 1;
          }
          return { cells: cells.cluster.length, drawn: v.cellsDrawn().n, wrong, clear, clusters: cells.clusters, legend: document.querySelectorAll('.viewer-legend-item').length };
        })()`);
        check('viewer', 'remote control: cluster_cells clusters the Lung cells, and the tissue shows each cell in its cluster\'s color', clustered.ok && shownClusters.ok ? `${clustered.message} ${JSON.stringify(clusterFill)}` : clustered.message ?? shownClusters.message, clustered.ok && clustered.data.cells.analyzed === 11820 && clusterFill?.cells === 11898 && clusterFill.drawn === 11898 && clusterFill.wrong === 0 && clusterFill.clear === 78 && clusterFill.legend >= Math.min(24, clusterFill.clusters), 'every cell in its cluster\'s color, the 78 not analyzed clear, a legend');
        // The map (wave 4, slice 2): the clustering's map beside the tissue; a cluster selected is
        // exactly its cells on both, and a polygon drawn on the map selects exactly the cells whose
        // places lie inside it, highlighted on the tissue.
        const mapShown = await act('show_map', { dataset: 'xenium-lung-2fov', select: { clusters: [2] } });
        const linked = await chrome.eval(`(async () => {
          const v = window.situcraft.cellTypesView;
          const map = v.map();
          const st = map.state;
          const cells = await fetch('/api/clusterings/' + st.color.clustering + '/cells').then((r) => r.json());
          let wrong = 0;
          let size = 0;
          for (let i = 0; i < cells.cluster.length; i += 1) {
            const want = cells.cluster[i] === 1 ? 1 : 0;
            size += want;
            if (st.selected[i] !== want) wrong += 1;
          }
          const highlight = Boolean(st.renderer.highlight);
          // A polygon around the middle of the map's places.
          const data = st.map;
          let x0 = Infinity, x1 = -Infinity, y0 = Infinity, y1 = -Infinity;
          for (let i = 0; i < data.n; i += 1) { x0 = Math.min(x0, data.xy[2 * i]); x1 = Math.max(x1, data.xy[2 * i]); y0 = Math.min(y0, data.xy[2 * i + 1]); y1 = Math.max(y1, data.xy[2 * i + 1]); }
          const cx = (x0 + x1) / 2, cy = (y0 + y1) / 2, rx = (x1 - x0) / 4, ry = (y1 - y0) / 4;
          const polygon = [[cx - rx, cy - ry], [cx + rx, cy - ry], [cx + rx, cy + ry], [cx - rx, cy]];
          // Drawn pixels on the map.
          const canvas = document.querySelector('.cm-map');
          const px = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data;
          let drawn = 0;
          for (let i = 3; i < px.length; i += 4) if (px[i] > 0) drawn += 1;
          return { wrong, size, count: st.count, highlight, polygon, segCells: data.segCells, drawn };
        })()`);
        const lassoed = await act('show_map', { dataset: 'xenium-lung-2fov', select: { polygon: linked.polygon, on: 'map' } });
        const inside = await chrome.eval(`(async () => {
          const st = window.situcraft.cellTypesView.map().state;
          const data = st.map;
          const poly = ${JSON.stringify(linked.polygon)};
          const inPoly = (x, y) => { let c = false; for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) { const [xi, yi] = poly[i]; const [xj, yj] = poly[j]; if ((yi > y) !== (yj > y) && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) c = !c; } return c; };
          let want = 0, wrong = 0;
          const mask = new Uint8Array(st.selected.length);
          for (let i = 0; i < data.n; i += 1) if (inPoly(data.xy[2 * i], data.xy[2 * i + 1])) mask[data.cells[i]] = 1;
          for (let i = 0; i < mask.length; i += 1) { want += mask[i]; if (mask[i] !== st.selected[i]) wrong += 1; }
          return { want, wrong, count: st.count };
        })()`);
        check('viewer', 'remote control: show_map selects a cluster\'s cells on the map and the tissue, and the cells a polygon on the map holds', mapShown.ok && lassoed.ok ? `cluster 2: ${linked.count} of ${linked.size} cells, ${linked.wrong} wrong; polygon: ${inside.count} of ${inside.want}, ${inside.wrong} wrong; ${linked.drawn} map pixels drawn` : mapShown.message ?? lassoed.message, mapShown.ok && linked.wrong === 0 && linked.size > 0 && linked.highlight && linked.segCells === 11898 && linked.drawn > 1000 && inside.wrong === 0 && inside.want > 100 && lassoed.data.selected === inside.want, 'exactly the cluster\'s cells, then exactly the cells inside, highlighted');
        // Labels from markers (wave 4, slice 3): the Lung cells labeled by a program from the panel's
        // starter set; the tissue filled by their labels at the lineage level, each cell in its
        // label's color (the one it carries there), unlabeled cells grey, cells not analyzed
        // clear; and, colored by labels, a cluster selected is still exactly its cells.
        const labeled = await act('label_cells', { dataset: 'xenium-lung-2fov', markers: 'lung' });
        const shownLabels = await act('show_tissue', { fill: 'labels:lineage', whole: true });
        const labelFill = await chrome.eval(`(async () => {
          const v = window.situcraftViewer;
          for (let k = 0; k < 300 && !v.clusterFill()?.labels; k += 1) await new Promise((r) => setTimeout(r, 100));
          const f = v.clusterFill();
          if (!f?.labels) return null;
          const id = window.situcraft.store.project.artifacts.filter((a) => a.kind === 'labels').at(-1).id;
          const buffer = await fetch('/api/labels/' + id + '/cells').then((r) => r.arrayBuffer());
          const [segCells, n, levels] = new Uint32Array(buffer, 0, 3);
          const cells = new Uint32Array(buffer, 16, n);
          const lineage = new Int32Array(buffer, 16 + 8 * n + 4 * n, n);
          const { categoricalColor } = await import('/lib/colormaps.js');
          const rgba = v.fillColors();
          const want = new Int32Array(segCells).fill(-2);
          for (let i = 0; i < n; i += 1) want[cells[i]] = lineage[i];
          let wrong = 0, clear = 0, grey = 0;
          for (let i = 0; i < segCells; i += 1) {
            const hex = '#' + [0, 1, 2].map((j) => rgba[4 * i + j].toString(16).padStart(2, '0')).join('');
            if (want[i] === -2) { if (rgba[4 * i + 3] === 0) clear += 1; else wrong += 1; continue; }
            if (want[i] === -1) { if (hex === '#9a9a9a' && rgba[4 * i + 3] === 150) grey += 1; else wrong += 1; continue; }
            if (hex !== categoricalColor(want[i]).toLowerCase() || rgba[4 * i + 3] !== 255) wrong += 1;
          }
          return { segCells, n, levels, wrong, clear, grey, legend: document.querySelectorAll('.viewer-legend-item').length };
        })()`);
        const byLabels = await act('show_map', { dataset: 'xenium-lung-2fov', color_by: 'labels', select: { clusters: [2] } });
        const stillClusters = await chrome.eval(`(async () => {
          const st = window.situcraft.cellTypesView.map().state;
          const cells = await fetch('/api/clusterings/' + st.clustersOf + '/cells').then((r) => r.json());
          let wrong = 0, size = 0;
          for (let i = 0; i < cells.cluster.length; i += 1) { const w = cells.cluster[i] === 1 ? 1 : 0; size += w; if (st.selected[i] !== w) wrong += 1; }
          return { wrong, size, kind: st.color.kind, legend: document.querySelectorAll('.cm-legend-item').length };
        })()`);
        check('viewer', 'remote control: label_cells labels the Lung cells from the panel\'s starter set; the tissue shows each cell in its label\'s color, and a cluster selected on the labels\' colors is its cells', labeled.ok && shownLabels.ok && byLabels.ok ? `${labeled.message} ${JSON.stringify(labelFill)} ${JSON.stringify(stillClusters)}` : labeled.message ?? shownLabels.message ?? byLabels.message, labeled.ok && labeled.data.cells.analyzed === 11820 && labelFill?.segCells === 11898 && labelFill.wrong === 0 && labelFill.clear === 78 && labelFill.grey > 0 && labelFill.legend > 2 && stillClusters.kind === 'labels' && stillClusters.wrong === 0 && stillClusters.size > 0 && stillClusters.legend > 2, 'every cell in its label\'s color, the unlabeled grey, the 78 not analyzed clear; the cluster\'s cells exactly');
        // Imported labels (wave 4, slice 4): a table of three of the Lung cells' labels (their ids
        // as select_cell answers them), each cell taking its row's label and the others none; a
        // table naming a cell the segmentation does not have refused, with how many; the
        // instrument's graph-based clusters imported for every cell it clusters.
        const picked = [];
        for (const row of [10, 500, 4000]) {
          const a = await act('select_cell', { dataset: 'xenium-lung-2fov', cell: `#${row}` });
          picked.push({ row, id: a.data?.id, label: row === 4000 ? 'Macrophage' : 'Tumour' });
        }
        const table = join(work, 'labels.csv');
        writeFileSync(table, `cell_id,cell_type,score\n${picked.map((p, k) => `${p.id},${p.label},${(k + 1) / 4}`).join('\n')}\n`);
        const foreign = join(work, 'foreign.csv');
        writeFileSync(foreign, `cell_id,cell_type\n${picked[0].id},Tumour\nzzzzzzzz-1,T cell\n`);
        const imported = await act('import_labels', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', path: table });
        const refusedTable = await act('import_labels', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', path: foreign });
        const instrumentLabels = await act('import_labels', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', instrument: 'graph-based' });
        const importedCells = imported.ok ? await chrome.eval(`(async () => {
          const buffer = await fetch('/api/labels/${imported.data.id}/cells').then((r) => r.arrayBuffer());
          const [segCells, n] = new Uint32Array(buffer, 0, 2);
          const label = new Int32Array(buffer, 16 + 8 * n, n);
          const set = await fetch('/api/labels/${imported.data.id}').then((r) => r.json());
          const rows = ${JSON.stringify(picked.map((p) => p.row))};
          let others = 0;
          for (let i = 0; i < n; i += 1) if (!rows.includes(i) && label[i] >= 0) others += 1;
          return { segCells, n, labels: rows.map((r) => set.description.sets[label[r]]?.name ?? null), confidence: set.description.source.confidenceColumn, others };
        })()`) : null;
        check('viewer', 'remote control: import_labels takes a table\'s labels for the cells it names, refuses one naming a cell the segmentation does not have, and takes the instrument\'s clusters', imported.ok && instrumentLabels.ok ? `${imported.message} ${JSON.stringify(importedCells)}; refused: ${refusedTable.message}; ${instrumentLabels.message}` : imported.message ?? instrumentLabels.message, imported.ok && importedCells?.n === 11898 && importedCells.others === 0 && importedCells.labels.join() === picked.map((p) => p.label).join() && importedCells.confidence === 'score' && !refusedTable.ok && /1 of foreign\.csv's 2 rows name cells this segmentation does not have \(zzzzzzzz-1\)/.test(refusedTable.message) && instrumentLabels.ok && instrumentLabels.data.cells.labeled[0] === 11897 && instrumentLabels.data.labels.length === 17, 'the three cells their rows\' labels and no other; refused with the count; 11,897 cells in 17 clusters');
        // Label versions and review (wave 4, slice 5): the Lung labels from markers' review queue
        // (each flagged cell's agreement at most the threshold, the most doubtful first); a cell
        // relabeled with a reason in a curated set, drawn in its new label's color and every other
        // cell as in its base set; a consensus of the two.
        const markerSet = labeled.ok ? labeled.data.id : null;
        const queued = markerSet ? await act('review_labels', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', labels: markerSet, limit: 50 }) : { message: 'no label set' };
        const shares = queued.ok ? queued.data.queue.filter((q) => q.reasons.includes('implausible')).map((q) => q.share) : [];
        const relabelCell = queued.ok && queued.data.queue.length ? queued.data.queue[0].cell : 10;
        const curated = markerSet ? await act('curate_labels', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', labels: markerSet, cells: [`#${relabelCell}`], label: 'Reviewed: doublet', reason: 'two nuclei in one outline', by: 'validation' }) : { message: 'no label set' };
        const curatedFill = curated.ok ? await act('show_tissue', { fill: `labels:${curated.data.id}`, whole: true }) : curated;
        const curatedColors = curated.ok ? await chrome.eval(`(async () => {
          const v = window.situcraftViewer;
          for (let k = 0; k < 300 && !(v.clusterFill()?.labels && v.clusterFill().sets.some((x) => x.name === 'Reviewed: doublet')); k += 1) await new Promise((r) => setTimeout(r, 100));
          const f = v.clusterFill();
          const rgba = v.fillColors();
          const { labelSource, refOf } = await import('/ui/label-sources.js');
          const base = await labelSource(window.situcraft, refOf('${markerSet}', 0));
          const hex = (i) => '#' + [0, 1, 2].map((j) => rgba[4 * i + j].toString(16).padStart(2, '0')).join('');
          const k = f.sets.findIndex((x) => x.name === 'Reviewed: doublet');
          let others = 0;
          for (let i = 0; i < base.segCells; i += 1) {
            if (i === ${relabelCell} || base.label[i] < 0) continue;
            if (hex(i) !== base.colors[base.label[i]].toLowerCase()) others += 1;
          }
          return { relabeled: hex(${relabelCell}) === f.sets[k].color.toLowerCase(), newIndex: k, baseLabels: base.names.length, others };
        })()`) : null;
        const consensus = curated.ok ? await act('consensus_labels', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', members: [markerSet, curated.data.id], rule: 'majority', min: 2, name: 'Agreed' }) : curated;
        check('viewer', 'remote control: review_labels ranks the cells to review; curate_labels relabels one with its reason, drawn in its new label\'s color, the others as before; consensus_labels agrees', queued.ok && curated.ok && consensus.ok ? `${queued.message} ${curated.message} ${JSON.stringify(curatedColors)} ${consensus.message}` : queued.message ?? curated.message ?? consensus.message, queued.ok && queued.data.queue.length > 0 && shares.every((v, i) => v <= 0.2 && (i === 0 || v >= shares[i - 1])) && curated.ok && curated.data.edits === 1 && curated.data.last.reason === 'two nuclei in one outline' && curatedFill.ok && curatedColors?.relabeled && curatedColors.newIndex === curatedColors.baseLabels && curatedColors.others === 0 && consensus.ok && consensus.data.labeled === consensus.data.covered - 1, 'the implausible by agreement, rising, at most 0.2; the cell its new color, no other changed; the two agree but on the cell relabeled');
        // Labels from a reference (wave 4, slice 6): a table of four classes' profiles over their
        // markers (a row per gene; one gene the panel lacks), the Lung cells mapped to it; each
        // cell drawn in its label's color; a cell's evidence its recorded first-round scores; a
        // reference of none of the panel's genes refused.
        const classes = { Epithelial: ['EPCAM', 'CDH1', 'ELF3'], Immune: ['CD3E', 'CD2', 'MS4A1', 'CD68', 'AIF1'], Endothelial: ['CLDN5', 'VWF', 'RAMP2'], Stromal: ['PDGFRA', 'PDGFRB', 'FBN1', 'DES'] };
        const genes = [...Object.values(classes).flat(), 'NOT_A_GENE'];
        const profilesTable = join(work, 'profiles.csv');
        writeFileSync(profilesTable, `gene,${Object.keys(classes).join(',')}\n${genes.map((g) => `${g},${Object.values(classes).map((list) => (list.includes(g) ? 2.5 : 0.1)).join(',')}`).join('\n')}\n`);
        const elsewhere = join(work, 'elsewhere.csv');
        writeFileSync(elsewhere, 'gene,A,B\nNOT_A,1,0\nNOT_B,0,1\n');
        const mapped = await act('reference_labels', { dataset: 'xenium-lung-2fov', path: profilesTable });
        const refusedReference = await act('reference_labels', { dataset: 'xenium-lung-2fov', path: elsewhere });
        const referenceFill = mapped.ok ? await act('show_tissue', { fill: `labels:${mapped.data.id}`, whole: true }) : mapped;
        const referenceCells = mapped.ok ? await chrome.eval(`(async () => {
          const v = window.situcraftViewer;
          for (let k = 0; k < 300 && !(v.clusterFill()?.labels === '${mapped.data.id}'); k += 1) await new Promise((r) => setTimeout(r, 100));
          const buffer = await fetch('/api/labels/${mapped.data.id}/cells').then((r) => r.arrayBuffer());
          const [segCells, n] = new Uint32Array(buffer, 0, 2);
          const cells = new Uint32Array(buffer, 16, n);
          const label = new Int32Array(buffer, 16 + 8 * n, n);
          const { categoricalColor } = await import('/lib/colormaps.js');
          const rgba = v.fillColors();
          let wrong = 0;
          for (let i = 0; i < n; i += 1) {
            if (label[i] < 0) continue;
            const hex = '#' + [0, 1, 2].map((j) => rgba[4 * cells[i] + j].toString(16).padStart(2, '0')).join('');
            if (hex !== categoricalColor(label[i]).toLowerCase()) wrong += 1;
          }
          const scores = new Float32Array(await fetch('/api/labels/${mapped.data.id}/scores?set=0').then((r) => r.arrayBuffer()));
          const app = window.situcraft;
          const item = app.bundles.link(app.store.project.datasets.find((d) => d.name === 'xenium-lung-2fov').id).item;
          const ev = await fetch(item.url + '/clusterings/${mapped.data.clustering}/cells/' + cells[7] + '/evidence?labels=${mapped.data.id}').then((r) => r.json());
          const first = ev.reference?.rounds?.[0]?.scores.find((x) => x.label === 0)?.score;
          return { segCells, n, wrong, evidence: Boolean(ev.reference), label: ev.reference?.label, recordedLabel: label[7], same: Math.fround(first) === scores[cells[7]] };
        })()`) : null;
        check('viewer', 'remote control: reference_labels labels the Lung cells from a table of profiles, each drawn in its label\'s color, a cell\'s evidence its recorded scores; a reference sharing no gene with the panel refused', mapped.ok ? `${mapped.message} ${JSON.stringify(referenceCells)}; refused: ${refusedReference.message}` : mapped.message, mapped.ok && mapped.data.reference.shared === 15 && mapped.data.reference.missing.length > 0 && mapped.data.labels.length === 4 && mapped.data.cells.analyzed === 11820 && referenceFill.ok && referenceCells?.wrong === 0 && referenceCells.evidence && referenceCells.label === referenceCells.recordedLabel && referenceCells.same && !refusedReference.ok && /shares 0 of the panel's genes/.test(refusedReference.message), 'four labels over the 15 genes the panel has; every labeled cell in its color; the evidence as recorded; refused');
        // Nucleus expansion (wave 2, slice 3), run by a program in the Segmentation view.
        const expanded = await act('expand_nuclei', { dataset: 'xenium-lung-2fov', distance_um: 5 });
        const g = expanded.ok ? expanded.data.summary.agreement : null;
        check('viewer', 'remote control: expand_nuclei grows the Lung bundle\'s nuclei by 5 µm, nearly as the instrument did', expanded.message ?? expanded.error, expanded.ok && expanded.data.state === 'complete' && expanded.data.summary.cells === 11898 && g.cells.paired === 11898 && g.pixels.agreement >= 0.98 && g.transcripts.agreement >= 0.985, '11,898 cells, paired one to one; at least 98% of pixels and 98.5% of transcripts in the same cells');
        const shownExpansion = await chrome.eval(`(() => {
          const app = window.situcraft;
          const record = app.store.project.artifacts.find((a) => a.kind === 'segmentation' && a.source === 'situcraft');
          return { mode: app.store.ui.mode, record: record ? { id: record.id, files: record.files.length, parents: record.parents.map((p) => p.kind).join(), distance: record.parameters?.distance_um } : null, rows: document.querySelectorAll('.seg-table tbody tr').length, differs: document.querySelectorAll('.seg-differs li').length };
        })()`);
        check('viewer', 'the expansion is recorded in the project and shown with what differs', JSON.stringify(shownExpansion), shownExpansion.mode === 'segmentation' && shownExpansion.record?.id === expanded.data?.id && shownExpansion.record.files === 6 && shownExpansion.record.parents === 'bundle,segmentation' && shownExpansion.record.distance === 5 && shownExpansion.rows === 2 && shownExpansion.differs >= 2, 'recorded with its files and lineage; two segmentations; the differences described');
        const jobsBefore = (await act('jobs')).data?.jobs.length;
        const again = await act('expand_nuclei', { dataset: 'xenium-lung-2fov' });
        const jobsAfter = (await act('jobs')).data?.jobs.length;
        check('viewer', 'expanding by the same distance again answers the same artifact, with no job', `${again.ok ? again.data.id : again.message}; jobs ${jobsBefore} then ${jobsAfter}`, again.ok && again.data.id === expanded.data?.id && jobsAfter === jobsBefore, 'the same artifact, no new job');
        // Imports (wave 2, slice 4): the Lung bundle, as an output bundle of its own run, imports
        // as the instrument's segmentation; the Breast bundle, of another run, is refused; an import
        // needs the token.
        const lungPath = join(dataRoot, 'xenium-lung-2fov');
        const refusedImport = await act('import_segmentation', { dataset: 'xenium-lung-2fov', format: 'xenium', paths: [lungPath] }, false);
        const selfImport = await act('import_segmentation', { dataset: 'xenium-lung-2fov', format: 'xenium', paths: [lungPath] });
        const si = selfImport.ok ? selfImport.data.summary : null;
        check('viewer', 'remote control: import_segmentation imports a Xenium output bundle of the same run, only with the token', `${refusedImport.status} without; ${selfImport.message ?? selfImport.error}`, refusedImport.status === 401 && selfImport.ok && selfImport.data.state === 'complete' && si.cells === 11898 && Math.abs(si.assignedFraction - segmentations.data.segmentations[0].summary.assignedFraction) < 1e-6, '401, then 11,898 cells assigning what the instrument\'s do');
        const importedRow = await chrome.eval(`(() => { const app = window.situcraft; const r = app.store.project.artifacts.find((a) => a.kind === 'segmentation' && a.source === 'imported'); return r ? { id: r.id, parents: r.parents.map((p) => p.kind).join(), files: r.files.length, rows: document.querySelectorAll('.seg-table tbody tr').length } : null; })()`);
        check('viewer', 'the import is recorded in the project as imported, with its lineage', JSON.stringify(importedRow), importedRow?.id === selfImport.data?.id && importedRow.parents === 'bundle,import' && importedRow.files === 6 && importedRow.rows === 3, 'recorded; three segmentations shown');
        // Comparisons (wave 2, slice 5): the instrument's segmentation and the 5 µm expansion, every
        // cell one-to-one and the pixels and transcripts that changed those the expansion's own
        // comparison counted.
        const compared = await act('compare_segmentations', { dataset: 'xenium-lung-2fov', a: 'Instrument segmentation', b: 'Nucleus expansion, 5 µm' });
        const cd = compared.ok ? compared.data : null;
        check('viewer', 'remote control: compare_segmentations pairs every Lung cell one to one with its 5 µm expansion, as the expansion found', compared.message ?? compared.error, compared.ok && cd.classes['one-to-one'] === 11898 && Object.keys(cd.classes).length === 1 && cd.pixelAgreement === g.pixels.agreement && cd.changes.changed === g.transcripts.otherCell + g.transcripts.situcraftOnly + g.transcripts.instrumentOnly && cd.a.spuriousCoexpression > 0, '11,898 one-to-one; the expansion\'s pixel agreement and changed transcripts');
        const shownComparison = await chrome.eval(`(() => ({ rows: document.querySelectorAll('.seg-comparisons tbody tr').length, detail: document.querySelectorAll('.seg-compare-detail table').length, project: window.situcraft.store.project.artifacts.filter((a) => a.kind === 'comparison').length }))()`);
        check('viewer', 'the comparison is recorded in the project and shown with its measures', JSON.stringify(shownComparison), shownComparison.rows === 1 && shownComparison.detail === 2 && shownComparison.project === 1, 'one comparison, its two tables');
        // The comparison workspace (wave 2, slice 6): every Lung cell one-to-one, so the queue of
        // differing classes is empty; with one-to-one kept and ranked by disagreement, the least
        // overlapping cell first; both segmentations drawn on the tissue, in each mode.
        const empty = await act('show_comparison', { dataset: 'xenium-lung-2fov' });
        const ranked = await act('show_comparison', { dataset: 'xenium-lung-2fov', classes: ['one-to-one'], rank: 'disagreement', difference: 1, mode: 'side', map: true });
        const top = ranked.ok ? ranked.data.top : [];
        const sorted = top.every((x, i) => i === 0 || x.iou >= top[i - 1].iou);
        check('viewer', 'remote control: show_comparison ranks the review queue by the rules asked', `${empty.ok ? empty.data.queue : empty.message} differing; ${ranked.message ?? ranked.error}`, empty.ok && empty.data.queue === 0 && ranked.ok && ranked.data.queue === 11898 && sorted && ranked.data.selected?.difference === top[0].difference, 'none differing; 11,898 one-to-one, least overlap first, the first shown');
        const drawn = async (mode) => {
          await act('show_comparison', { dataset: 'xenium-lung-2fov', mode });
          return chrome.eval(`(async () => {
            const w = window.situcraft.compareView.current();
            for (const r of w.state.renderers) await r.renderer.settled();
            return { mode: w.session.mode, canvases: w.state.renderers.length, transparent: w.state.renderers.map((r) => r.renderer.transparent), outlines: w.state.renderers.map((r) => r.renderer.caches.shapes.size ?? r.renderer.caches.shapes.entries?.size ?? -1), same: w.state.renderers.every((r) => r.renderer.camera.cx === w.session.camera.cx && r.renderer.camera.scale === w.session.camera.scale) };
          })()`);
        };
        const modes = [];
        for (const mode of ['side', 'overlay', 'flicker']) modes.push(await drawn(mode));
        check('viewer', 'the workspace draws A and B with one camera, side by side, overlaid and in turns', JSON.stringify(modes), modes.every((m) => m.canvases === 2 && m.same) && modes[1].transparent.join() === 'false,true' && modes[0].mode === 'side' && modes[2].mode === 'flicker', 'two canvases, one camera; B transparent over A in the overlay');
        const refusedReject = await act('decide_segmentation', { dataset: 'xenium-lung-2fov', segmentation: 'Instrument segmentation', decision: 'reject', rationale: 'Testing' });
        const kept = await act('decide_segmentation', { dataset: 'xenium-lung-2fov', segmentation: 'Nucleus expansion, 5 µm', decision: 'keep', rationale: 'The same cells as the instrument\'s: kept as a baseline', comparison: cd?.id });
        const noReason = await act('decide_segmentation', { dataset: 'xenium-lung-2fov', segmentation: 'Nucleus expansion, 5 µm', decision: 'promote', rationale: '  ' });
        const decided = await chrome.eval(`(() => { const p = window.situcraft.store.project; return p.decisions.filter((d) => d.kind === 'keep-segmentation').map((d) => ({ by: d.by, comparison: d.comparison ?? null })); })()`);
        check('viewer', 'remote control: decisions need a reason, never reject the default, and are appended with their comparison', `${refusedReject.message}; ${kept.message}; ${noReason.message}`, !refusedReject.ok && /the default/.test(refusedReject.message) && kept.ok && !noReason.ok && decided.length === 1 && decided[0].comparison === cd?.id && kept.data.default !== null, 'the default kept; the decision recorded with its comparison');
        // Labels across segmentations (wave 4, slice 7): the Lung clustering and its labels (from
        // the starter set and the table of profiles) made again on the expansion in one step; the
        // two segmentations' labels read through the comparison, every cell one to one, the tissue
        // colored by them; the review queue's segmentation-sensitive cells those the impact counts.
        const repeated = await act('repeat_on_segmentation', { dataset: 'xenium-lung-2fov', segmentation: 'Nucleus expansion, 5 µm' });
        const impact = repeated.ok ? await act('label_impact', { dataset: 'xenium-lung-2fov', comparison: 'Instrument segmentation and Nucleus expansion, 5 µm', color: true }) : repeated;
        const colored = impact.ok ? await chrome.eval(`(() => {
          const w = window.situcraft.compareView.current();
          return { tab: w.session.tab, fill: w.session.impact.fill, legend: document.querySelectorAll('.cmp-legend-item').length, rows: document.querySelectorAll('.cmp-impact-table tbody tr').length };
        })()`) : null;
        const sensitiveQueue = impact.ok ? await act('review_labels', { dataset: 'xenium-lung-2fov', segmentation: 'Instrument segmentation', labels: impact.data.a.labels, against: impact.data.b.labels, limit: 500 }) : impact;
        const im = impact.data;
        check('viewer', 'remote control: repeat_on_segmentation makes the Lung labels again on the expansion; label_impact reads both through the comparison and colors the tissue by them; review_labels flags the cells whose label changes', impact.ok && sensitiveQueue.ok ? `${repeated.message} ${impact.message} ${JSON.stringify(colored)}; ${sensitiveQueue.message}` : impact.message ?? sensitiveQueue.message,
          repeated.ok && repeated.data.labels.length === 2 && !repeated.data.failed.length && impact.ok && im.matched.cells > 11000 && im.matched.agreement >= 0.95 && im.split.groups === 0 && im.merged.groups === 0 && im.composition.length >= 4 && colored?.tab === 'labels' && colored.fill && colored.legend >= 4 && colored.rows === im.composition.length && sensitiveQueue.ok && sensitiveQueue.data.counts.sensitive === im.sensitive.a && sensitiveQueue.data.queue.some((q) => q.reasons.includes('segmentation-sensitive')),
          'two label sets made again; every matched cell one to one, at least 95% labeled alike; the tissue in the labels\' colors; the queue\'s sensitive cells the impact\'s');
        if (present.includes('xenium-breast-2fov')) {
          const otherRun = await act('import_segmentation', { dataset: 'xenium-lung-2fov', format: 'xenium', paths: [join(dataRoot, 'xenium-breast-2fov')] });
          check('viewer', 'an output bundle of another run is refused, saying why', otherRun.message ?? otherRun.error, !otherRun.ok && /another run|of run/.test(otherRun.message ?? otherRun.error ?? ''), 'refused: another run');
        }

        // The worked example on the Lung data (wave 2, slice 7): errors planted for practice, found
        // by the comparison, each as what it is; the planted segmentation rejected, never promoted.
        const started = await chrome.eval(`(async () => {
          const app = window.situcraft;
          const { guideFor } = await import('/lib/tours.js');
          const { startTour } = await import('/ui/tour.js');
          const dataset = app.store.project.datasets.find((d) => d.name === 'xenium-lung-2fov');
          startTour(app, { tour: guideFor('xenium-lung-2fov', 'planted-errors'), dataset: dataset.id });
          return app.tour.tour.steps.length;
        })()`);
        const planting = await act('tour', { run: true });
        const comparing = await act('tour', { run: true });
        const pc = comparing.data?.comparison;
        check('viewer', 'the worked example plants errors, compares, and opens the review queue on them', `${started} steps; ${planting.message ?? planting.error}; ${comparing.ok ? `step ${comparing.data.step}, ${pc?.queue} differences, the first ${pc?.selected?.class}` : comparing.message}`,
          planting.ok && planting.data.step === 2 && comparing.ok && comparing.data.step === 3 && pc?.queue === 67 && ['merged', 'split', 'only in A'].includes(pc?.selected?.class), 'step 3: 67 differences, a planted one first');
        const contamination = await act('tour', { step: 4 });
        check('viewer', 'ranked by contamination, a planted merge comes first', contamination.message ?? contamination.error, contamination.ok && contamination.data.comparison?.rank === 'contamination' && contamination.data.comparison?.selected?.class === 'merged', 'merged');
        const key = await act('plant_errors', { dataset: 'xenium-lung-2fov', reveal: true });
        await act('show_comparison', { dataset: 'xenium-lung-2fov' });
        const found = key.ok ? await chrome.eval(`(() => {
          const w = window.situcraft.compareView.current();
          const errors = ${JSON.stringify(key.data?.errors ?? [])};
          const want = { merge: 'merged', split: 'split', missed: 'only in A', grown: 'expanded' };
          const wrong = errors.filter((e) => {
            const at = w.prepared.idsA.indexOf(e.cells[0]);
            return at < 0 || w.groups[w.prepared.ofA[at]].class !== want[e.kind];
          }).map((e) => e.kind + ' ' + e.cells[0]);
          const planted = new Set(errors.flatMap((e) => e.cells));
          const stray = w.groups.filter((g) => g.class !== 'one-to-one' && !g.a.some((c) => planted.has(w.prepared.idsA[c]))).length;
          return { errors: errors.length, wrong, stray };
        })()`) : null;
        check('viewer', 'every planted error is the difference it should be, and nothing else differs', found ? `${found.errors} errors; ${found.wrong.length ? `wrong: ${found.wrong.slice(0, 4).join(', ')}` : 'each as planted'}; ${found.stray} other differences` : key.message, found && found.errors === 60 && !found.wrong.length && found.stray === 0, '60, each as planted; no other');
        const promotePlanted = await act('decide_segmentation', { dataset: 'xenium-lung-2fov', segmentation: 'Planted errors (simulated)', decision: 'promote', rationale: 'Testing' });
        const rejectPlanted = await act('decide_segmentation', { dataset: 'xenium-lung-2fov', segmentation: 'Planted errors (simulated)', decision: 'reject', rationale: 'Its planted errors were found' });
        check('viewer', 'a planted segmentation is never promoted, and is rejected with its reason', `${promotePlanted.message}; ${rejectPlanted.message ?? rejectPlanted.error}`, !promotePlanted.ok && /simulated/.test(promotePlanted.message) && rejectPlanted.ok, 'refused; rejected');
        await act('tour', { end: true });

        // The worked example clustering and labeling the Lung cells (wave 4, slice 9), as its card
        // expects: here its clustering and labels are made already, with the same defaults and set.
        // Started in the page: here the bundles are the project's datasets, not examples.
        await chrome.eval(`(async () => {
          const app = window.situcraft;
          const { guideFor } = await import('/lib/tours.js');
          const { startTour } = await import('/ui/tour.js');
          const dataset = app.store.project.datasets.find((d) => d.name === 'xenium-lung-2fov');
          startTour(app, { tour: guideFor('xenium-lung-2fov', 'cluster-label'), dataset: dataset.id });
          return true;
        })()`);
        const lsteps = [];
        for (let k = 0; k < 8; k += 1) lsteps.push(await act('tour', { run: true }));
        const lungAt = (n) => lsteps.find((x) => x.ok && x.data.step === n)?.data;
        const lungTour = await chrome.eval(`(async () => {
          const labels = ${JSON.stringify(lungAt(6)?.celltypes?.color?.labels ?? '')};
          const clustering = ${JSON.stringify(lungAt(3)?.celltypes?.clustering ?? '')};
          const set = labels ? await fetch('/api/labels/' + labels).then((r) => r.json()) : null;
          const c = clustering ? await fetch('/api/artifacts/' + clustering).then((r) => r.json()) : null;
          const d = set?.description;
          // The last step's Viewer, its cells filled by the labels.
          for (let k = 0; k < 300 && !window.situcraftViewer?.clusterFill?.()?.labels; k += 1) await new Promise((r) => setTimeout(r, 100));
          return { clusters: c?.summary?.clusters, shares: d ? d.cells.labeled.map((n) => Math.round((100 * n) / d.cells.analyzed)) : null, fill: window.situcraftViewer?.clusterFill?.()?.labels ? 'labels' : null };
        })()`);
        const lr = lungAt(8)?.celltypes?.review;
        check('viewer', 'the Lung cell-types worked example clusters, maps, labels and reviews as its card expects', `${lsteps.map((x) => x.ok ? `step ${x.data.step}` : x.message).join('; ')}; ${JSON.stringify(lungTour)}; cluster 13 ${lungAt(4)?.celltypes?.selected} cells; review ${JSON.stringify(lr)}`,
          lsteps.every((x) => x.ok) && lsteps.at(-1).data.step === 9 && lungAt(3)?.celltypes?.cells === 11820 && lungTour.clusters === 29 && lungAt(4)?.celltypes?.selected === 364 && lungAt(6)?.celltypes?.color?.kind === 'labels' && JSON.stringify(lungTour.shares) === '[47,12,1]' && lr?.labeled === 5555 && Math.abs(lr.implausible - 821) <= 5 && Math.abs(lr.lowConfidence - 1533) <= 5 && lr.flagged >= 1950 && lr.flagged <= 2150 && lungTour.fill === 'labels',
          '29 clusters of 11,820 cells; cluster 13 of 364; 47%, 12% and 1% labeled; about 2,000 of 5,555 to review (821 implausible, 1,533 of low confidence; here also those whose label changes under the expansion); the Viewer filled by labels');
        await act('tour', { end: true });

        // Exports (wave 2, slice 7): only with the token; the manifest's totals; recorded in the
        // project; the SpatialData store imported back as the instrument's segmentation.
        const folder = join(work, 'exports', 'xenium-lung-2fov instrument');
        mkdirSync(join(work, 'exports'), { recursive: true });
        const exportArgs = { dataset: 'xenium-lung-2fov', segmentation: 'Instrument segmentation', formats: ['spatialdata', 'parquet', 'xenium-ranger'], folder };
        const refusedExport = await act('export_segmentation', exportArgs, false);
        const exportedSeg = await act('export_segmentation', exportArgs);
        const em = exportedSeg.data ?? {};
        const recordedExport = await chrome.eval(`(() => (window.situcraft.store.project.exports ?? []).map((x) => ({ folder: x.folder, files: x.files, formats: x.formats.join(), labels: x.content?.labelsSha256 })))()`);
        check('viewer', 'remote control: export_segmentation writes SpatialData, Parquet and Xenium Ranger\'s inputs only with the token, and the project records it', `${refusedExport.status} without; ${exportedSeg.message ?? exportedSeg.error}`,
          refusedExport.status === 401 && exportedSeg.ok && em.counts?.cells === 11898 && em.files.length > 10 && /--cells=/.test(em.xeniumRanger?.command ?? '') && recordedExport.length === 1 && recordedExport[0].labels === em.content?.labelsSha256 && existsSync(join(folder, 'situcraft-export.json')), '401, then 11,898 cells written and recorded');
        const plantedExport = await act('export_segmentation', { dataset: 'xenium-lung-2fov', segmentation: 'Planted errors (simulated)', formats: ['geojson'], folder: join(work, 'exports', 'planted') });
        check('viewer', 'a planted segmentation is never exported', plantedExport.message ?? plantedExport.error, !plantedExport.ok && /simulated/.test(plantedExport.message ?? '') && !existsSync(join(work, 'exports', 'planted')), 'refused, nothing written');
        const back = await act('import_segmentation', { dataset: 'xenium-lung-2fov', format: 'spatialdata', paths: [join(folder, 'spatialdata.zarr')], tool: 'SituCraft export' });
        const bs = back.data?.summary ?? {};
        check('viewer', 'the exported SpatialData store imports back as the instrument\'s segmentation', back.message ?? back.error, back.ok && back.data.state === 'complete' && bs.cells === 11898 && Math.abs(bs.assignedFraction - segmentations.data.segmentations[0].summary.assignedFraction) < 1e-9, '11,898 cells, the same assigned fraction');
        // The transcript-informed segmentation (wave 3, slice 4), a short run: recorded with its
        // probabilities and diagnostics, compared with the instrument's segmentation at once, and
        // shown with its convergence, its background (slice 7: the false decodes measured by the
        // negative controls) and its limitations.
        const comparisonsBefore = await chrome.eval(`window.situcraft.store.project.artifacts.filter((a) => a.kind === 'comparison').length`);
        const refined = await act('refine_segmentation', { dataset: 'xenium-lung-2fov', sweeps: 40, samples: 20 });
        const rs = refined.ok ? refined.data.summary : {};
        check('viewer', 'remote control: refine_segmentation refines the Lung bundle\'s cells from its transcripts and compares them with the instrument\'s', refined.message ?? refined.error,
          refined.ok && refined.data.state === 'complete' && rs.cells >= 11800 && rs.assignedFraction > 0.5 && rs.diagnostics?.samples === 20 && refined.data.comparison?.summary?.classes?.['one-to-one'] > 0, 'about 11,898 cells, their diagnostics, and a comparison with the instrument\'s');
        const shownRefined = await chrome.eval(`(() => {
          const app = window.situcraft;
          const r = app.store.project.artifacts.find((a) => a.kind === 'segmentation' && a.method?.name === 'SituCraft transcript-informed segmentation');
          return { files: r ? r.files.map((f) => f.path).sort().join() : '', comparisons: app.store.project.artifacts.filter((a) => a.kind === 'comparison').length, card: Boolean(document.querySelector('.seg-refined')), lines: document.querySelectorAll('.seg-refined .seg-differs li').length,
            background: (document.querySelector('.seg-refined')?.textContent ?? '').match(/The model's background accounts for [\\d.]+% of the modelled transcripts: false decodes [\\d.]+%, measured by [\\d,]+ negative-control transcripts/)?.[0] ?? '' };
        })()`);
        check('viewer', 'the refinement is recorded with its probabilities and diagnostics, its comparison too, and shown with its convergence, background and limitations', JSON.stringify(shownRefined),
          /diagnostics\.json/.test(shownRefined.files) && /probabilities\.bin/.test(shownRefined.files) && shownRefined.comparisons === comparisonsBefore + 1 && shownRefined.card && shownRefined.lines >= 5 && shownRefined.background !== '', 'its 9 files; one more comparison; the card with its convergence, background (the controls measuring the false decodes) and limitations');

        // Segmentations on the tissue (wave 3, slice 8). The list: every complete segmentation,
        // the default first.
        const lungItem = `${url}/api/local/${info.files.findIndex((f) => f.name === 'xenium-lung-2fov')}`;
        await act('show_tissue', { dataset: 'xenium-lung-2fov', segmentation: 'default' });
        const listed = await chrome.eval(`(async () => {
          const app = window.situcraft;
          const { promotedSegmentation } = await import('/lib/segmentations.js');
          const dataset = app.store.project.datasets.find((d) => d.name === 'xenium-lung-2fov');
          const complete = app.store.project.artifacts.filter((a) => a.kind === 'segmentation' && a.dataset === dataset.id && a.state === 'complete').map((a) => a.id);
          const choices = window.situcraftViewer.choices();
          return { complete, listed: choices.map((c) => c.id), first: choices[0]?.id, isDefault: choices[0]?.isDefault, promoted: promotedSegmentation(app.store.project, dataset.id)?.record.id, picker: [...document.querySelectorAll('.viewer-panel select[aria-label="Segmentation drawn"] option')].map((o) => o.value) };
        })()`);
        check('viewer', 'the Viewer lists every complete segmentation of the dataset, its default first', JSON.stringify({ listed: listed.listed.length, complete: listed.complete.length, first: listed.first === listed.promoted }),
          listed.listed.length === listed.complete.length && listed.complete.every((id) => listed.listed.includes(id)) && listed.first === listed.promoted && listed.isDefault && listed.picker.join() === listed.listed.join(), 'all of them, in the picker; the default first');
        // Each kind drawn as its files hold it: at cells' centroids (where its raster holds the
        // cell itself), the cell found is that one, and the fill drawn is the color of its value.
        const planted = await chrome.eval(`window.situcraft.store.project.artifacts.find((a) => a.kind === 'segmentation' && a.source === 'planted')?.id ?? null`);
        const kinds = [['an expansion', expanded.data?.id, 'transcripts'], ['an import', selfImport.data?.id, 'transcripts'], ['a refinement', refined.data?.id, 'certainty'], ['errors planted for practice', planted, 'area']];
        const viridis = colormapLUT('viridis');
        for (const [kind, id, fill] of kinds) {
          if (!id) {
            check('viewer', `${kind} is drawn as its files hold it`, 'not made', false, 'made earlier in this suite');
            continue;
          }
          const mask = readMask(Buffer.from(await (await fetch(`${url}/api/artifacts/${id}/files/mask.bin`)).arrayBuffer()));
          const table = readSegCells(await (await fetch(`${lungItem}/segmentations/${id}/cells`)).arrayBuffer());
          const [lo, hi] = attributeRange(table[fill === 'transcripts' ? 'transcripts' : fill === 'area' ? 'area' : 'certainty']);
          const expected = (k) => {
            if (fill === 'certainty') return Number.isFinite(table.certainty[k]) ? hexToRgb(certaintyColor(table.certainty[k])) : null;
            const v = table[fill][k];
            const i = Math.max(0, Math.min(255, Math.round((255 * (v - lo)) / (hi - lo))));
            return [viridis[3 * i], viridis[3 * i + 1], viridis[3 * i + 2]];
          };
          // Pixels anywhere in its cells, each well inside its cell (its 7 × 7 pixels the same
          // cell, clear of the outline's simplification), from a fixed sequence.
          const samples = [];
          let seed = 12345;
          const next = () => {
            seed = (seed * 1103515245 + 12345) % 2147483648;
            return seed / 2147483648;
          };
          for (let tries = 0; samples.length < 24 && tries < 200000; tries += 1) {
            const px = 3 + Math.floor(next() * (mask.width - 6));
            const py = 3 + Math.floor(next() * (mask.height - 6));
            const label = mask.label(px, py);
            if (!label || !expected(label - 1)) continue;
            let inside = true;
            for (let dy = -3; dy <= 3 && inside; dy += 1) for (let dx = -3; dx <= 3 && inside; dx += 1) inside = mask.label(px + dx, py + dy) === label;
            if (inside) samples.push({ cell: label - 1, x: (px + 0.5) * mask.pixel, y: (py + 0.5) * mask.pixel, color: expected(label - 1) });
          }
          const got = await chrome.eval(`(async () => {
            const v = window.situcraftViewer;
            await v.setSegmentation(${JSON.stringify(id)});
            v.setChannelsVisible(false);
            v.setTranscripts({ visible: false });
            v.setCells({ outlines: true, nuclei: false, fill: ${JSON.stringify(fill)}, opacity: 1 });
            const out = [];
            for (const s of ${JSON.stringify(samples)}) {
              v.setCamera({ cx: s.x, cy: s.y, scale: (window.devicePixelRatio || 1) / 0.25 });
              await v.settled();
              const [sx, sy] = v.worldToScreen(s.x, s.y);
              out.push({ cell: s.cell, picked: v.pick(sx, sy), color: v.readColor(sx, sy).slice(0, 3) });
            }
            return { drawn: v.segmentation(), out };
          })()`);
          const wrongCell = got.out.filter((o) => o.picked !== o.cell);
          const wrongColor = got.out.filter((o, i) => o.color.some((c, j) => Math.abs(c - samples[i].color[j]) > 1));
          check('viewer', `${kind} is drawn as its files hold it`, `${got.out.length} cells; ${wrongCell.length} found as another (${wrongCell.slice(0, 2).map((o) => `${o.cell}→${o.picked}`).join(', ')}); ${wrongColor.length} filled otherwise`,
            got.drawn === id && samples.length >= 20 && wrongCell.length === 0 && wrongColor.length === 0, `its cells where its raster has them, filled by ${fill}`);
        }
        // A view of the refinement, its fill, its transcripts by probability and a cell, saved,
        // reopens with all of them; select_cell answers its cell.
        const refinedShown = await act('show_tissue', { dataset: 'xenium-lung-2fov', segmentation: refined.data?.id, fill: 'certainty', opacity: 0.6, all_transcripts: true, color_by: 'probability', center: [750, 300], um_per_pixel: 0.25, cell: 'c100' });
        const segPage = await chrome.eval(`(() => { const v = window.situcraftViewer; v.setChannelsVisible(true); return { seg: v.segmentation(), cell: v.detail()?.id, of: v.detail()?.segmentation?.id, colorBy: v.layers().transcripts.colorBy }; })()`);
        check('viewer', 'remote control: show_tissue draws the segmentation asked, with its cell', refinedShown.ok ? JSON.stringify(segPage) : refinedShown.message,
          refinedShown.ok && refinedShown.data.showing.segmentation === refined.data?.id && segPage.seg === refined.data?.id && segPage.cell === 'c100' && segPage.of === refined.data?.id && segPage.colorBy === 'probability', 'the refinement, its cell c100, transcripts by probability');
        const points = await chrome.eval(`(async () => {
          const v = window.situcraftViewer;
          await v.settled();
          const rect = { x0: 740, y0: 290, x1: 760, y1: 310 };
          const loaded = v.loadedPoints(rect);
          const n = v.cellsDrawn().n;
          return { loaded: loaded.length, withP: loaded.filter((p) => p.probability !== null && (Number.isNaN(p.probability) || p.probability >= 0 && p.probability <= 1)).length, cells: loaded.filter((p) => p.cell === 0xffffffff || p.cell < n).length };
        })()`);
        const own = readPoints(await (await fetch(`${lungItem}/points?x0=740&y0=290&x1=760&y1=310&minq=20`)).arrayBuffer());
        check('viewer', 'its transcripts carry their probability and their cell in it, the same transcripts as the dataset\'s', JSON.stringify({ ...points, dataset: own.length }),
          points.loaded > 0 && points.loaded === own.length && points.withP === points.loaded && points.cells === points.loaded, 'every one, with a probability and a cell of the refinement');
        const segView = await act('save_view', { name: 'The refinement on the tissue' });
        await act('show_tissue', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', fill: 'none', color_by: 'gene', all_transcripts: false, cell: null });
        const segReopened = await act('open_view', { view: 'The refinement on the tissue' });
        const reopenedPage = await chrome.eval(`(() => { const v = window.situcraftViewer; return { seg: v.segmentation(), cell: v.detail()?.id, fill: v.layers().cells.fill }; })()`);
        check('viewer', 'a saved view reopens with its segmentation, its fill and its cell', `${segReopened.message ?? segReopened.error}; ${JSON.stringify(reopenedPage)}`,
          segView.ok && segReopened.ok && segReopened.data.exact === true && reopenedPage.seg === refined.data?.id && reopenedPage.cell === 'c100' && reopenedPage.fill === 'certainty', 'exactly as saved: the refinement, certainty, c100');
        const selectedSeg = await act('select_cell', { dataset: 'xenium-lung-2fov', cell: 'c200' });
        check('viewer', 'remote control: select_cell selects a cell of the segmentation drawn, with the instrument\'s it came from', selectedSeg.message ?? selectedSeg.error,
          selectedSeg.ok && selectedSeg.data.id === 'c200' && selectedSeg.data.segmentation?.id === refined.data?.id && selectedSeg.data.instrument?.id && Array.isArray(selectedSeg.data.limitations), 'c200 of the refinement, from an instrument\'s cell');
        // The Viewer as the rest of the suite expects it: the instrument's cells, every stain.
        await act('show_tissue', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', fill: 'none', color_by: 'gene', all_transcripts: false, transcripts: true, cell: null });
        await chrome.eval('window.situcraftViewer.setChannelsVisible(true)');
        await act('set_mode', { mode: 'segmentation' });
        // The segmentation studio (wave 3, slice 5): its six steps; a pilot's preflight on a region
        // (refused without one); the pilot run twice, its seeds' agreement recorded and the rest of
        // the tissue kept; a region holding no nucleus failing, classified, with what to do.
        const steps = await chrome.eval(`[...document.querySelectorAll('.studio .studio-step')].map((b) => b.textContent).join(' ')`);
        const pilotArgs = { dataset: 'xenium-lung-2fov', preset: 'roi-pilot', regions: [[500, 300, 700, 450]], sweeps: 40, samples: 20 };
        const preflight = await act('refinement_preflight', pilotArgs);
        const pp = preflight.ok ? preflight.data.preflight : { problems: [], regions: [], estimate: {} };
        check('viewer', 'the studio\'s six steps; refinement_preflight says what a pilot on a region models, covers and costs, with its recipe', `${steps}; ${preflight.message ?? preflight.error}`,
          /Input.*Compute.*Model.*Review.*Run.*Compare/.test(steps) && preflight.ok && pp.problems.length === 0 && pp.regions.length === 1 && pp.estimate.cells > 0 && pp.estimate.cellsHeld > 0 && pp.estimate.peakMemoryBytes > 0 && preflight.data.recipe?.format === 'situcraft-recipe',
          'no problems; one region, its cells refined and held; an estimate; a recipe');
        const noRegion = await act('refinement_preflight', { dataset: 'xenium-lung-2fov', preset: 'roi-pilot' });
        check('viewer', 'a pilot without a region is refused, saying why', noRegion.message ?? noRegion.error, !noRegion.ok && /no region was chosen/.test(noRegion.message ?? noRegion.error ?? ''), 'refused: no region was chosen');
        const pilot = await act('refine_segmentation', pilotArgs);
        const pd = pilot.ok ? pilot.data.summary.diagnostics ?? {} : {};
        check('viewer', 'remote control: the pilot refines its region twice, records the seeds\' agreement, and keeps the instrument\'s cells elsewhere', pilot.message ?? pilot.error,
          pilot.ok && pilot.data.state === 'complete' && pilot.data.summary.cells === 11898 && pd.regions === 1 && pd.cellsRefined > 0 && pd.cellsHeld > 0 && pd.seedAgreement > 0.5 && pilot.data.parameters?.second_seed === 20251,
          'the region\'s cells refined, those about it held, a second seed\'s agreement');
        const failed = await act('refine_segmentation', { dataset: 'xenium-lung-2fov', regions: [[600.11, 400.11, 600.13, 400.13]], sweeps: 20, samples: 10 });
        const failure = await chrome.eval(`document.querySelector('.studio-failure')?.innerText ?? ''`);
        check('viewer', 'a region holding no nucleus fails, classified, with what to do, in the answer and the studio', `${failed.message ?? failed.error}; the studio: ${failure.replace(/\s+/g, ' ')}`,
          !failed.ok && /holds no cell's nucleus/.test(failed.message ?? failed.error ?? '') && /What to do/.test(failed.message ?? failed.error ?? '') && /What to do/.test(failure), 'refused by the job, with the region\'s remedy');

        // The projects as 0.2.0, 0.3.0 and 0.4.0 saved them
        // (validation/projects/<version>-lung.situcraft.json): this release's written when asked
        // (SITUCRAFT_WRITE_FIXTURE=1), the places of this run's folders made neutral and given its
        // own id; then each reopened, and every artifact SituCraft made that it records made again by
        // this build with the same files' checksums (a comparison by its differences: its
        // comparison.json records the jobs' times; a clustering, a map and a label set from markers,
        // whose records keep no checksums, by their summaries, which hold their numbers exactly).
        const fixtureOf = (v) => join(root, 'validation', 'projects', `${v}-lung.situcraft.json`);
        if (process.env.SITUCRAFT_WRITE_FIXTURE) {
          const doc = await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { rename, serializeProject } = await import('/lib/project.js');
            app.store.commit(rename(app.store.project, 'Lung example, as SituCraft 0.4.0 saved it'), 'Rename');
            return serializeProject(app.store.project);
          })()`);
          // Its own id: it grew from the 0.1.0 project in this run.
          const neutral = JSON.parse(doc.split(join(dataRoot, 'xenium-lung-2fov')).join('/data/examples/xenium-lung-2fov').split(work).join('/data/work'));
          writeFileSync(fixtureOf('0.4.0'), `${JSON.stringify({ ...neutral, id: 'p-situcraft-0-4-0-lung' }, null, 1)}\n`);
        }
        for (const v of ['0.2.0', '0.3.0', '0.4.0']) {
          if (!existsSync(fixtureOf(v))) continue;
          const text = readFileSync(fixtureOf(v), 'utf8');
          const fixture = JSON.parse(text);
          // An import of SituCraft's own export is not made again by another version: the export's
          // files name the SituCraft that wrote them, so their checksums, and the import's id, differ.
          // Labels imported or from a reference read a file this run wrote, and the instrument's
          // clusters and map are the bundle's.
          const cellTypes = (a) => (a.kind === 'clustering' && !a.summary?.source) || (a.kind === 'embedding' && !a.summary?.source) || (a.kind === 'labels' && !a.source && !a.reference);
          const made = fixture.artifacts.filter((a) => (a.kind === 'segmentation' && a.source !== 'instrument' && a.method?.name !== 'SituCraft export') || a.kind === 'comparison' || cellTypes(a));
          const differing = [];
          for (const a of made) {
            const m = await fetch(`${url}/api/artifacts/${encodeURIComponent(a.id)}`).then((r) => (r.ok ? r.json() : null));
            if (!m) {
              differing.push(`${a.name}: not made again`);
              continue;
            }
            if (a.kind === 'comparison') {
              if (JSON.stringify(m.summary?.classes) !== JSON.stringify(a.summary?.classes) || m.summary?.pixelAgreement !== a.summary?.pixelAgreement) differing.push(`${a.name}: other differences`);
              continue;
            }
            if (cellTypes(a)) {
              if (JSON.stringify(m.summary) !== JSON.stringify(a.summary)) differing.push(`${a.name}: another summary`);
              continue;
            }
            const sums = new Map((m.files ?? []).map((f) => [f.path, f.sha256]));
            for (const f of a.files) if (sums.get(f.path) !== f.sha256) differing.push(`${a.name}: ${f.path}`);
          }
          const kinds = [...new Set(made.map((a) => a.kind))].join(', ');
          check('viewer', `${v} Lung project: every artifact SituCraft made is made again with the same checksums`, `${made.length} artifacts (${kinds})${differing.length ? `; ${differing.slice(0, 4).join('; ')}` : ', all the same'}`, made.length >= 5 && differing.length === 0 && (v < '0.4.0' || made.filter(cellTypes).length >= 5), 'all the same');
          const reopened = await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { promotedSegmentation, exportsOf } = await import('/lib/segmentations.js');
            const wait = async (f) => { for (let k = 0; k < 300; k += 1) { const v = f(); if (v) return v; await new Promise((r) => setTimeout(r, 100)); } return null; };
            const doc = ${JSON.stringify(text)};
            const saved = JSON.parse(doc);
            await app.library.saveProject(saved.id, doc);
            await app.openProject(saved.id);
            const dataset = app.store.project.datasets[0];
            const before = (await wait(() => { const l = app.bundles.link(dataset.id); return l && l.state !== 'checking' ? l : null; }))?.state;
            const link = await app.bundles.locate(dataset.id, ${JSON.stringify(join(dataRoot, 'xenium-lung-2fov'))});
            const p = app.store.project;
            return { before, after: link?.state, segmentations: p.artifacts.filter((a) => a.kind === 'segmentation').length, comparisons: p.artifacts.filter((a) => a.kind === 'comparison').length, decisions: p.decisions.length, exports: exportsOf(p, dataset.id).length, default: promotedSegmentation(p, dataset.id)?.record.id, saved: promotedSegmentation(saved, saved.datasets[0].id)?.record.id };
          })()`);
          check('viewer', `${v} Lung project: it opens and finds its moved bundle; its segmentations, comparisons, decisions and exports are there, and its default`, JSON.stringify(reopened), reopened.before === 'missing' && reopened.after === 'ok' && reopened.segmentations === fixture.artifacts.filter((a) => a.kind === 'segmentation').length && reopened.comparisons >= 2 && reopened.decisions === fixture.decisions.length && reopened.exports >= 1 && reopened.default && reopened.default === reopened.saved, 'missing, then ok; all there');
        }

        // The worked example on the Breast data: the instrument's cells, drawn from stains, against
        // a 5 µm nucleus expansion, as research.md §4 records them.
        if (present.includes('xenium-breast-2fov')) {
          await act('new_project', { name: 'Breast worked example' });
          await act('open_paths', { paths: [join(dataRoot, 'xenium-breast-2fov')], add: true });
          await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { guideFor } = await import('/lib/tours.js');
            const { startTour } = await import('/ui/tour.js');
            const dataset = app.store.project.datasets.find((d) => d.name === 'xenium-breast-2fov');
            for (let k = 0; k < 600 && !app.store.project.artifacts.some((a) => a.kind === 'segmentation' && a.dataset === dataset.id); k += 1) await new Promise((r) => setTimeout(r, 100));
            startTour(app, { tour: guideFor('xenium-breast-2fov', 'compare-expansion'), dataset: dataset.id });
            return true;
          })()`);
          const steps = [];
          for (let k = 0; k < 3; k += 1) steps.push(await act('tour', { run: true }));
          const bc = steps.at(-1).data?.comparison;
          check('viewer', 'the Breast worked example expands the nuclei, compares, and shows splits and merges first', steps.map((x) => x.ok ? `step ${x.data.step}` : x.message).join('; ') + (bc ? `; ${JSON.stringify(bc.classes)}; the first ${bc.selected?.class}` : ''),
            steps.every((x) => x.ok) && steps.at(-1).data.step === 4 && bc?.classes?.split === 263 && bc.classes.merged === 128 && bc.classes['only in A'] === 119 && ['split', 'merged', 'only in A'].includes(bc.selected?.class), 'step 4: 263 splits, 128 merges, 119 only in A; one of them first');
          await act('tour', { end: true });

          // The worked example refining the Breast data (wave 3, slice 9): what its card says to
          // expect, as it happens.
          await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { guideFor } = await import('/lib/tours.js');
            const { startTour } = await import('/ui/tour.js');
            const dataset = app.store.project.datasets.find((d) => d.name === 'xenium-breast-2fov');
            startTour(app, { tour: guideFor('xenium-breast-2fov', 'refine-compare'), dataset: dataset.id });
            return true;
          })()`);
          const rsteps = [];
          for (let k = 0; k < 4; k += 1) rsteps.push(await act('tour', { run: true }));
          const rc = rsteps.at(-1).data?.comparison;
          const rn = await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { segmentationsOf } = await import('/lib/segmentations.js');
            const { segmentationByRole } = await import('/lib/tours.js');
            const p = app.store.project;
            const d = p.datasets.find((x) => x.name === 'xenium-breast-2fov');
            const segs = segmentationsOf(p, d.id);
            const inst = segmentationByRole(segs, 'instrument'), refined = segmentationByRole(segs, 'refined');
            const c = p.artifacts.find((x) => x.kind === 'comparison' && x.a === inst?.id && x.b === refined?.id);
            return { cells: refined?.summary?.cells, changed: c?.summary?.changedFraction, coA: c?.summary?.spuriousCoexpressionA, coB: c?.summary?.spuriousCoexpressionB };
          })()`);
          const near = (v, want, tol) => Number.isFinite(v) && Math.abs(v - want) <= tol;
          check('viewer', 'the Breast refinement worked example refines, shows its certainty and what moved, as its card expects', `${rsteps.map((x) => x.ok ? `step ${x.data.step}` : x.message).join('; ')}; ${JSON.stringify(rc?.classes ?? {})}; ${JSON.stringify(rn)}`,
            rsteps.every((x) => x.ok) && rsteps.at(-1).data.step === 5 && rn.cells === 7275 && near(rc?.classes?.['one-to-one'], 4700, 250) && near(rc?.classes?.expanded, 1700, 150) && near(rc?.classes?.contracted, 760, 80) && near(rn.changed, 0.23, 0.02) && near(rn.coA, 1.77, 0.03) && near(rn.coB, 1.83, 0.03),
            '7,275 cells; about 4,700 the same, 1,700 grown, 760 shrunk; 23% of transcripts moved; co-expression 1.77 and 1.83');
          await act('tour', { end: true });

          // The worked example reading the Breast labels under two segmentations (wave 4, slice 9):
          // the instrument's cells clustered and labeled, the labels made again on the expansion
          // (made above), both read through the comparison, and the cells whose label changes
          // reviewed, as its card expects.
          await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { guideFor } = await import('/lib/tours.js');
            const { startTour } = await import('/ui/tour.js');
            const dataset = app.store.project.datasets.find((d) => d.name === 'xenium-breast-2fov');
            startTour(app, { tour: guideFor('xenium-breast-2fov', 'labels-two-segmentations'), dataset: dataset.id });
            return true;
          })()`);
          const bsteps = [];
          for (let k = 0; k < 9; k += 1) bsteps.push(await act('tour', { run: true }));
          const bl = bsteps.find((x) => x.ok && x.data.step === 8)?.data?.comparison?.labels;
          const bct = bsteps.at(-1)?.data?.celltypes;
          check('viewer', 'the Breast labels worked example reads both segmentations\' labels through the comparison and reviews the cells whose label changes, as its card expects', `${bsteps.map((x) => x.ok ? `step ${x.data.step}` : x.message).join('; ')}; ${JSON.stringify(bl)}; review ${JSON.stringify(bct?.review)} against ${bct?.against}`,
            bsteps.every((x) => x.ok) && bsteps.at(-1).data.step === 10 && bl?.matched.cells === 6512 && near(bl.matched.agreement, 0.969, 0.002) && bl.split.groups === 223 && bl.merged.groups === 106 && near(bl.mecr?.a, 0.0466, 0.001) && near(bl.mecr?.b, 0.0513, 0.001) && bl.fill && bct?.review?.sensitive === bl.sensitive.a && near(bl.sensitive.a, 688, 10) && /expansion/i.test(bct.against ?? ''),
            '6,512 matched, 96.9% of those labeled under both alike; 223 split, 106 merged; mixing 0.047 and 0.051; about 688 cells flagged against the expansion');
          await act('tour', { end: true });
        }

        // The simulated example, made on this computer, and its worked example scoring the
        // instrument's segmentation and a refinement against the truth (wave 3, slice 9).
        const simulatedMade = await act('open_example', { example: 'simulated' });
        await act('tour', { example: 'simulated-standard', name: 'score-against-truth' });
        const tsteps = [];
        for (let k = 0; k < 5; k += 1) tsteps.push(await act('tour', { run: true }));
        const scores = await chrome.eval(`(async () => {
          const app = window.situcraft;
          const { segmentationsOf } = await import('/lib/segmentations.js');
          const { segmentationByRole } = await import('/lib/tours.js');
          const p = app.store.project;
          const d = p.datasets.find((x) => x.name === 'simulated-standard');
          const segs = segmentationsOf(p, d.id);
          const truth = segmentationByRole(segs, 'truth');
          const score = (role) => p.artifacts.find((x) => x.kind === 'comparison' && x.a === truth?.id && x.b === segmentationByRole(segs, role)?.id)?.summary;
          const s = (role) => { const c = score(role); return c ? { iou: c.medianIoU, changed: c.changedFraction } : null; };
          return { truth: Boolean(truth), instrument: s('instrument'), refined: s('refined') };
        })()`);
        const tc = tsteps.at(-1).data?.comparison;
        const near = (v, want, tol) => Number.isFinite(v) && Math.abs(v - want) <= tol;
        check('viewer', 'the simulated example is made here; its worked example imports the truth, refines, and scores both against it as its card expects', `${simulatedMade.message ?? simulatedMade.error}; ${tsteps.map((x) => x.ok ? `step ${x.data.step}` : x.message).join('; ')}; ${JSON.stringify(scores)}`,
          simulatedMade.ok && tsteps.every((x) => x.ok) && tsteps.at(-1).data.step === 6 && tc?.mode === 'overlay' && scores.truth && near(scores.instrument?.iou, 0.62, 0.02) && near(scores.instrument?.changed, 0.20, 0.02) && near(scores.refined?.iou, 0.72, 0.02) && near(scores.refined?.changed, 0.17, 0.02),
          'the instrument\'s: median IoU 0.62, 20% changed; the refinement\'s: 0.72, 17%');
        await act('tour', { end: true });

        // The worked example reading the simulated tissue's labels against its truth (wave 4,
        // slice 9): the true types imported, the instrument's cells labeled from the types' markers
        // and read against them, then the refinement's (made above), as its card expects.
        await act('tour', { example: 'simulated-standard', name: 'labels-against-truth' });
        const lsim = [];
        for (let k = 0; k < 10; k += 1) lsim.push(await act('tour', { run: true }));
        const simAt = (n) => lsim.find((x) => x.ok && x.data.step === n)?.data?.comparison?.labels;
        const simInst = simAt(7);
        const simRef = simAt(11);
        check('viewer', 'the simulated labels worked example reads the instrument\'s and the refinement\'s labels against the true types, as its card expects', `${lsim.map((x) => x.ok ? `step ${x.data.step}` : x.message).join('; ')}; instrument ${JSON.stringify(simInst && { ...simInst.matched, mecr: simInst.mecr })}; refinement ${JSON.stringify(simRef && { ...simRef.matched, mecr: simRef.mecr })}`,
          lsim.every((x) => x.ok) && lsim.at(-1).data.step === 11 && simInst?.matched.cells === 2916 && near(simInst.matched.agreement, 0.983, 0.003) && near(simInst.mecr?.a, 0.063, 0.002) && near(simInst.mecr?.b, 0.221, 0.005) && near(simRef?.matched.agreement, 0.9986, 0.002) && near(simRef?.mecr?.b, 0.090, 0.003),
          'the instrument\'s: 98.3% their true type, mixing 0.22 against the truth\'s 0.06; the refinement\'s: 99.9%, mixing 0.09');
        await act('tour', { end: true });
      }

      // Post-Xenium images (wave 5, slice 1): the pathology fixture's bundle (XOA 1.0.1) opened,
      // its H&E added by a program with the alignment found beside it, and drawn where the matrix
      // puts it: at three of its pixels, the Viewer's colour is the image's.
      const pathology = join(dataRoot, 'xenium-breast-cancer-rep1');
      const sample = 'Xenium_FFPE_Human_Breast_Cancer_Rep1';
      if (existsSync(join(pathology, sample, 'experiment.xenium'))) {
        await act('new_project', { name: 'Pathology' });
        const opened = await act('open_paths', { paths: [join(pathology, sample)], add: true });
        const added = await act('add_image', { dataset: sample, path: join(pathology, 'images', `${sample}_he_image.ome.tif`) });
        const probes = [];
        if (added.ok) {
          const t = added.data.summary.transform;
          for (const [px, py] of [[16342, 14283], [9000, 20000], [24000, 6000]]) {
            const cx = t[0][0] * (px + 0.5) + t[0][1] * (py + 0.5) + t[0][2];
            const cy = t[1][0] * (px + 0.5) + t[1][1] * (py + 0.5) + t[1][2];
            await act('show_tissue', { dataset: sample, center: [cx, cy], um_per_pixel: 0.002, transcripts: false, outlines: false, channels: { DAPI: { visible: false } }, images: { [added.data.name]: { visible: true, opacity: 1 } } });
            probes.push(await chrome.eval(`(async () => {
              const r = window.situcraftViewer.renderer;
              await r.settled();
              const [w, h] = r.size();
              const got = r.readColor(w / 2, h / 2).slice(0, 3);
              const tile = new Uint8Array(await fetch('/api/images/${added.data.id}/tiles/0/0/${Math.floor(px / 1024)}/${Math.floor(py / 1024)}').then((x) => x.arrayBuffer()));
              const i = 3 * (${py % 1024} * 1024 + ${px % 1024});
              return { pixel: [${px}, ${py}], got, want: [tile[i], tile[i + 1], tile[i + 2]] };
            })()`));
          }
        }
        const worst = probes.length ? Math.max(...probes.flatMap((p) => p.got.map((g, k) => Math.abs(g - p.want[k])))) : Infinity;
        check('viewer', 'remote control: add_image adds the pathology fixture\'s H&E with the alignment beside it, and the Viewer draws its pixels where the matrix puts them', `${opened.message ?? opened.error}; ${added.message ?? added.error}; ${JSON.stringify(probes)}`,
          opened.ok && added.ok && added.data.summary.alignment.kind === 'matrix' && added.data.summary.alignment.reflected && probes.length === 3 && worst <= 2, 'the image\'s colour at three of its pixels, within 2');
      }
    }
    check('viewer', 'no uncaught errors in the page', errors.length ? errors.slice(0, 3).join(' | ') : 'none', errors.length === 0, 'none');
  } finally {
    await chrome?.close();
    program.kill();
    await sleep(500);
    try { rmSync(work, { recursive: true, force: true, maxRetries: 5 }); } catch { /* a temporary folder */ }
  }
}
