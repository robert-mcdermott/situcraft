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
//   segmentations drawn with one camera in each mode, decisions recorded as the rules allow.
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
import { readCells } from '../web/lib/viewer-data.js';

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
    check('viewer', 'the example datasets are listed, and none is downloaded unasked', examples.examples.map((e) => `${e.name}: ${e.state}`).join(', '), examples.examples.length === 2 && examples.examples.every((e) => e.state === 'absent' && e.download > 0 && e.license), 'two, absent');
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
        // shown with its convergence and limitations.
        const comparisonsBefore = await chrome.eval(`window.situcraft.store.project.artifacts.filter((a) => a.kind === 'comparison').length`);
        const refined = await act('refine_segmentation', { dataset: 'xenium-lung-2fov', sweeps: 40, samples: 20 });
        const rs = refined.ok ? refined.data.summary : {};
        check('viewer', 'remote control: refine_segmentation refines the Lung bundle\'s cells from its transcripts and compares them with the instrument\'s', refined.message ?? refined.error,
          refined.ok && refined.data.state === 'complete' && rs.cells >= 11800 && rs.assignedFraction > 0.5 && rs.diagnostics?.samples === 20 && refined.data.comparison?.summary?.classes?.['one-to-one'] > 0, 'about 11,898 cells, their diagnostics, and a comparison with the instrument\'s');
        const shownRefined = await chrome.eval(`(() => {
          const app = window.situcraft;
          const r = app.store.project.artifacts.find((a) => a.kind === 'segmentation' && a.method?.name === 'SituCraft transcript-informed segmentation');
          return { files: r ? r.files.map((f) => f.path).sort().join() : '', comparisons: app.store.project.artifacts.filter((a) => a.kind === 'comparison').length, card: Boolean(document.querySelector('.seg-refined')), lines: document.querySelectorAll('.seg-refined .seg-differs li').length };
        })()`);
        check('viewer', 'the refinement is recorded with its probabilities and diagnostics, its comparison too, and shown with its convergence and limitations', JSON.stringify(shownRefined),
          /diagnostics\.json/.test(shownRefined.files) && /probabilities\.bin/.test(shownRefined.files) && shownRefined.comparisons === comparisonsBefore + 1 && shownRefined.card && shownRefined.lines >= 5, 'its 8 files; one more comparison; the card with its convergence and limitations');
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

        // The project as 0.2.0 saved it (validation/projects/0.2.0-lung.situcraft.json): written
        // when asked (SITUCRAFT_WRITE_FIXTURE=1), the places of this run's folders made neutral and
        // given its own id; then
        // reopened, and every artifact SituCraft made that it records is made again by this build
        // with the same files' checksums (a comparison by its differences: its comparison.json
        // records the jobs' times).
        const fixture2Path = join(root, 'validation', 'projects', '0.2.0-lung.situcraft.json');
        if (process.env.SITUCRAFT_WRITE_FIXTURE) {
          const doc = await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { rename, serializeProject } = await import('/lib/project.js');
            app.store.commit(rename(app.store.project, 'Lung example, as SituCraft 0.2.0 saved it'), 'Rename');
            return serializeProject(app.store.project);
          })()`);
          // Its own id: it grew from the 0.1.0 project in this run.
          const neutral = JSON.parse(doc.split(join(dataRoot, 'xenium-lung-2fov')).join('/data/examples/xenium-lung-2fov').split(work).join('/data/work'));
          writeFileSync(fixture2Path, `${JSON.stringify({ ...neutral, id: 'p-situcraft-0-2-0-lung' }, null, 1)}\n`);
        }
        if (existsSync(fixture2Path)) {
          const text2 = readFileSync(fixture2Path, 'utf8');
          const fixture2 = JSON.parse(text2);
          // An import of SituCraft's own export is not made again by another version: the export's
          // files name the SituCraft that wrote them, so their checksums, and the import's id, differ.
          const made = fixture2.artifacts.filter((a) => (a.kind === 'segmentation' && a.source !== 'instrument' && a.method?.name !== 'SituCraft export') || a.kind === 'comparison');
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
            const sums = new Map((m.files ?? []).map((f) => [f.path, f.sha256]));
            for (const f of a.files) if (sums.get(f.path) !== f.sha256) differing.push(`${a.name}: ${f.path}`);
          }
          check('viewer', '0.2.0 Lung project: every artifact SituCraft made is made again with the same checksums', `${made.length} artifacts${differing.length ? `; ${differing.slice(0, 4).join('; ')}` : ', all the same'}`, made.length >= 5 && differing.length === 0, 'all the same');
          const reopened2 = await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { promotedSegmentation, exportsOf } = await import('/lib/segmentations.js');
            const wait = async (f) => { for (let k = 0; k < 300; k += 1) { const v = f(); if (v) return v; await new Promise((r) => setTimeout(r, 100)); } return null; };
            const doc = ${JSON.stringify(text2)};
            const id = JSON.parse(doc).id;
            await app.library.saveProject(id, doc);
            await app.openProject(id);
            const dataset = app.store.project.datasets[0];
            const before = (await wait(() => { const l = app.bundles.link(dataset.id); return l && l.state !== 'checking' ? l : null; }))?.state;
            const link = await app.bundles.locate(dataset.id, ${JSON.stringify(join(dataRoot, 'xenium-lung-2fov'))});
            const p = app.store.project;
            return { before, after: link?.state, segmentations: p.artifacts.filter((a) => a.kind === 'segmentation').length, comparisons: p.artifacts.filter((a) => a.kind === 'comparison').length, decisions: p.decisions.length, exports: exportsOf(p, dataset.id).length, default: promotedSegmentation(p, dataset.id)?.record.source };
          })()`);
          check('viewer', '0.2.0 Lung project: it opens and finds its moved bundle; its segmentations, comparisons, decisions and exports are there, the instrument\'s the default', JSON.stringify(reopened2), reopened2.before === 'missing' && reopened2.after === 'ok' && reopened2.segmentations === fixture2.artifacts.filter((a) => a.kind === 'segmentation').length && reopened2.comparisons >= 2 && reopened2.decisions === fixture2.decisions.length && reopened2.exports >= 1 && reopened2.default === 'instrument', 'missing, then ok; all there');
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
        }
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
