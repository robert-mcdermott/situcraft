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
// - remote control (wave 2, slice 1): the window driven by a program through the program's
//   remote-control hub: the state described, the tissue shown as asked, a view saved and reopened
//   exactly, a filter, the methods, and a file written only with the token.
//
// It builds the program, starts it on the bundles with an empty cache and library, builds their
// derived data, and drives the page through window.situcraft (the app, app.js) and window.situcraftViewer
// (ui/mode-viewer.js). Run by validation/run.mjs's "viewer" suite.

import { execFileSync, spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
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
        check('viewer', 'remote control: the methods paragraph, citing the data and SituCraft', methods.ok ? `${methods.data.references.length} references` : methods.message, methods.ok && /SituCraft 0\.1\.0/.test(methods.data.text) && methods.data.references.some((r) => r.includes('10x Genomics')), 'the version and the data cited');
        const target = join(work, 'project.json');
        const refused = await act('export', { what: 'project', path: target }, false);
        const exported = await act('export', { what: 'project', path: target });
        const written = existsSync(target) ? JSON.parse(readFileSync(target, 'utf8')) : null;
        check('viewer', 'remote control: export writes the project only with the token', `${refused.status} without, ${exported.ok ? 'written' : exported.message} with`, refused.status === 401 && exported.ok && written?.views?.length === 4 && written.artifacts.length === 3, '401, then the project written');
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
