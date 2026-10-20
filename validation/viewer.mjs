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
//   H&E added by a program, each of three of its pixels drawn where its alignment puts it;
// - transforms and landmarks (wave 5, slice 2): landmarks made with 10x's matrix fitted back to it as
//   a version, listed in the Alignment view and written as Xenium Explorer's keypoints; a simulated
//   tissue's H&E added unaligned and placed by six landmarks clicked on both images, within its
//   truth's deformation, drawn where the fit puts it;
// - automatic registration (wave 5, slice 3): the same H&E registered by its nuclei with a
//   deformation allowed, within its truth's deformation, drawn where the version puts its pixels;
// - alignment checked in µm (wave 5, slice 4): the landmarks' version checked over the tissue, each
//   patch's measured error against its true one; the automatic version measured less off; the map,
//   a checkerboard and DAPI's edges drawn.
//
// It builds the program, starts it on the bundles with an empty cache and library, builds their
// derived data, and drives the page through window.situcraft (the app, app.js), window.situcraftViewer
// (ui/mode-viewer.js) and window.situcraftAlignment (ui/mode-alignment.js). Run by
// validation/run.mjs's "viewer" suite.

import { execFileSync, spawn, spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import { basename, dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { findChrome, launch, sleep } from './cdp.mjs';
import { inflateRawSync, inflateSync } from 'node:zlib';
import { attributeRange, readCells } from '../web/lib/viewer-data.js';
import { contingency } from '../web/lib/clusterings.js';
import { certaintyColor, readSegCells } from '../web/lib/segview.js';
import { colormapLUT, hexToRgb } from '../web/lib/colormaps.js';
import { placePoint } from '../web/lib/transforms.js';
import { invert, readInverseField } from '../web/lib/overlays.js';
import { divergingColor } from '../web/lib/spatial.js';
import { log as dlog } from '../web/lib/dmath.js';

// A figure's SVG read by the suite (wave 8): a lettered panel's part of it (from its group to the
// next panel's), and an image embedded in a part decoded (8-bit RGBA, unfiltered rows, as
// SituCraft writes them).
function figurePanelSvg(svg, letter) {
  const start = svg.indexOf(`aria-label="Panel ${letter}"`);
  if (start < 0) return '';
  const next = svg.indexOf('aria-label="Panel ', start + 10);
  return svg.slice(start, next < 0 ? svg.length : next);
}
function embeddedImage(part) {
  const m = /href="data:image\/png;base64,([^"]+)"/.exec(part);
  if (!m) return null;
  const b = Buffer.from(m[1], 'base64');
  const chunks = [];
  for (let at = 8; at + 8 <= b.length;) {
    const n = b.readUInt32BE(at);
    chunks.push({ type: b.toString('latin1', at + 4, at + 8), data: b.subarray(at + 8, at + 8 + n) });
    at += 12 + n;
  }
  const head = chunks.find((x) => x.type === 'IHDR').data;
  const width = head.readUInt32BE(0);
  const height = head.readUInt32BE(4);
  const raw = inflateSync(Buffer.concat(chunks.filter((x) => x.type === 'IDAT').map((x) => x.data)));
  const rgba = new Uint8Array(width * height * 4);
  for (let y = 0; y < height; y += 1) {
    if (raw[y * (width * 4 + 1)] !== 0) return { width, height, filtered: true };
    rgba.set(raw.subarray(y * (width * 4 + 1) + 1, (y + 1) * (width * 4 + 1)), y * width * 4);
  }
  return { width, height, rgba };
}

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
// What made a result, as the suite reads it (wave 8, slice 3): whether each step comes after the
// steps it was made from, the steps' kinds, the registry's keys among the references, the Markdown's
// numbered references and the BibTeX's entries.
function recordFacts(rec, md = '', bib = '') {
  const steps = rec?.steps ?? [];
  return {
    ordered: steps.length > 0 && steps.every((s, i) => s.n === i + 1 && s.inputs.every((k) => k < s.n)),
    kinds: steps.map((s) => s.kind),
    keys: (rec?.references ?? []).map((r) => r.key).filter(Boolean),
    numbered: (rec?.references ?? []).every((r, i) => r.n === i + 1) && steps.every((s) => s.references.every((n) => n >= 1 && n <= rec.references.length)),
    mdRefs: (md.split('\n## References\n')[1] ?? '').split('\n').filter((l) => /^\d+\. /.test(l)).length,
    bibEntries: [...bib.matchAll(/^@\w+\{([^,]+),$/gm)].map((m) => m[1]),
  };
}

// A methods text as the suite reads it (wave 8, slice 4): its citations in the order they appear
// ([1, 3–5] is 1, 3, 4, 5), and whether its references are numbered as first cited, every one
// cited.
function methodsFacts(text, references) {
  const cited = [];
  for (const m of String(text ?? '').matchAll(/\[([\d,–\s]+)\]/g)) {
    for (const part of m[1].split(/,\s*/)) {
      const [a, b] = part.split('–').map(Number);
      for (let n = a; n <= (b ?? a); n += 1) cited.push(n);
    }
  }
  const firsts = [...new Set(cited)];
  return { numbered: firsts.length > 0 && firsts.every((n, i) => n === i + 1) && firsts.length === (references ?? []).length, cited: firsts.length };
}

// Canonical JSON as the suite writes it (every object's keys sorted, nothing undefined), and a zip
// read by the suite's own reader (its central directory, each entry stored or deflated): name →
// bytes (wave 8, slice 5).
const canonicalOf = (v) => (Array.isArray(v) ? `[${v.map((x) => canonicalOf(x === undefined ? null : x)).join(',')}]` : v && typeof v === 'object' ? `{${Object.keys(v).filter((k) => v[k] !== undefined).sort().map((k) => `${JSON.stringify(k)}:${canonicalOf(v[k])}`).join(',')}}` : JSON.stringify(v ?? null));
function unzipOwn(buf) {
  let end = buf.length - 22;
  while (end >= 0 && buf.readUInt32LE(end) !== 0x06054b50) end -= 1;
  if (end < 0) throw new Error('not a zip');
  const out = new Map();
  let at = buf.readUInt32LE(end + 16);
  for (let i = 0; i < buf.readUInt16LE(end + 10); i += 1) {
    const method = buf.readUInt16LE(at + 10);
    const packed = buf.readUInt32LE(at + 20);
    const nameLength = buf.readUInt16LE(at + 28);
    const local = buf.readUInt32LE(at + 42);
    const name = buf.toString('utf8', at + 46, at + 46 + nameLength);
    const start = local + 30 + buf.readUInt16LE(local + 26) + buf.readUInt16LE(local + 28);
    const raw = buf.subarray(start, start + packed);
    out.set(name, method === 8 ? inflateRawSync(raw) : Buffer.from(raw));
    at += 46 + nameLength + buf.readUInt16LE(at + 30) + buf.readUInt16LE(at + 32);
  }
  return out;
}

// A zip written by the suite's own writer (wave 8, slice 6): each entry stored, with its CRC-32.
const CRC = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});
const crcOf = (buf) => {
  let c = 0xffffffff;
  for (const b of buf) c = CRC[(c ^ b) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
};
function zipOwn(entries) {
  const locals = [];
  const centrals = [];
  let offset = 0;
  for (const [name, data] of entries) {
    const body = Buffer.isBuffer(data) ? data : Buffer.from(data);
    const nameBuf = Buffer.from(name);
    const head = Buffer.alloc(30);
    head.writeUInt32LE(0x04034b50, 0); head.writeUInt16LE(20, 4); head.writeUInt16LE(0x0800, 6);
    head.writeUInt32LE(crcOf(body), 14); head.writeUInt32LE(body.length, 18); head.writeUInt32LE(body.length, 22); head.writeUInt16LE(nameBuf.length, 26);
    const central = Buffer.alloc(46);
    central.writeUInt32LE(0x02014b50, 0); central.writeUInt16LE(20, 4); central.writeUInt16LE(20, 6); central.writeUInt16LE(0x0800, 8);
    central.writeUInt32LE(crcOf(body), 16); central.writeUInt32LE(body.length, 20); central.writeUInt32LE(body.length, 24); central.writeUInt16LE(nameBuf.length, 28); central.writeUInt32LE(offset, 42);
    locals.push(head, nameBuf, body);
    centrals.push(central, nameBuf);
    offset += 30 + nameBuf.length + body.length;
  }
  const size = centrals.reduce((n, b) => n + b.length, 0);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0); end.writeUInt16LE(entries.length, 8); end.writeUInt16LE(entries.length, 10); end.writeUInt32LE(size, 12); end.writeUInt32LE(offset, 16);
  return Buffer.concat([...locals, ...centrals, end]);
}

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
      downloads.length === 2 && downloads.every((e) => e.state === 'absent' && e.download > 0 && e.license) && generated.map((e) => `${e.name}:${e.kind ?? ''}`).join() === 'simulated-standard:,simulated-cohort:cohort,simulated-null-cohort:cohort,simulated-paired-cohort:cohort' && generated.every((e) => e.state === 'absent' && !e.download),
      'two to download and four to make (a tissue and three cohorts), all absent');
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
        check('viewer', 'remote control: label_cells labels the Lung cells from the panel\'s starter set; the tissue shows each cell in its label\'s color, and a cluster selected on the labels\' colors is its cells', labeled.ok && shownLabels.ok && byLabels.ok ? `${labeled.message} ${JSON.stringify(labelFill)} ${JSON.stringify(stillClusters)}` : labeled.message ?? shownLabels.message ?? byLabels.message, labeled.ok && labeled.data.cells.analyzed === 11820 && labelFill?.segCells === 11898 && labelFill.wrong === 0 && labelFill.clear === 78 && labelFill.grey > 0 && labelFill.legend > 2 && stillClusters.kind === 'labels' && stillClusters.wrong === 0 && stillClusters.size > 0 && stillClusters.legend > 2, 'every cell in its label\'s color, the unlabeled gray, the 78 not analyzed clear; the cluster\'s cells exactly');
        // Figures (wave 8, slice 1): a saved view of the Lung (the DAPI stain, three genes, the cells
        // outlined and filled by their lineage) made a figure's panel, written as SVG, PDF, PNG and
        // its manifest. Read by the suite's own readers: the same bytes when written again at the
        // same time; the manifest the same in every file, its fingerprint the suite's own; the
        // stains' pixels in the SVG the suite's own composite of the tiles' samples (each pixel the
        // sample under its center, windowed, colored), and the Viewer's pixels the same composite
        // within one level; the scale bar's length in µm; the cells drawn those whose outlines meet
        // the region, each filled as the Viewer fills it; the transcripts those of the genes in the
        // region; no text below 6 pt, the PDF's text the SVG's.
        {
          await act('show_tissue', { center: [750, 300], um_per_pixel: 0.4, genes: ['EPCAM', 'PLVAP', 'CD68'], fill: 'labels:lineage', opacity: 0.5, outlines: true, nuclei: false, transcripts: true });
          await act('save_view', { name: 'A figure of the Lung' });
          const made = await act('figure', { name: 'Figure 1', views: ['A figure of the Lung'], size: 'single', width_um: 300 });
          const time = '2026-10-19T12:00:00Z';
          const out = (name) => join(work, name);
          const wrote = {};
          for (const [name, format] of [['figure.svg', 'svg'], ['figure-again.svg', 'svg'], ['figure.pdf', 'pdf'], ['figure-again.pdf', 'pdf'], ['figure.png', 'png'], ['figure.json', 'json']]) wrote[name] = await act('write_figure', { name: 'Figure 1', path: out(name), format, time, overwrite: true });
          const bytes = (name) => (existsSync(out(name)) ? readFileSync(out(name)) : Buffer.alloc(0));
          const svg = bytes('figure.svg').toString('utf8');
          const pdf = bytes('figure.pdf');
          const png = bytes('figure.png');
          const manifest = JSON.parse(bytes('figure.json').toString('utf8') || 'null');
          // The suite's own readers of the manifest in each file.
          const canonical = (v) => (Array.isArray(v) ? `[${v.map((x) => canonical(x === undefined ? null : x)).join(',')}]` : v && typeof v === 'object' ? `{${Object.keys(v).filter((k) => v[k] !== undefined).sort().map((k) => `${JSON.stringify(k)}:${canonical(v[k])}`).join(',')}}` : JSON.stringify(v ?? null));
          const fromSVG = (() => { const m = /<metadata id="situcraft-figure"><!\[CDATA\[([\s\S]*?)\]\]><\/metadata>/.exec(svg); return m ? JSON.parse(m[1]) : null; })();
          const fromPDF = (() => {
            const text = pdf.toString('latin1');
            const m = /\/Type \/EmbeddedFile[^>]*\/Length (\d+)[^]*?stream\n/.exec(text);
            return m ? JSON.parse(pdf.subarray(m.index + m[0].length, m.index + m[0].length + Number(m[1])).toString('utf8')) : null;
          })();
          const pngChunks = (b) => {
            const out2 = [];
            for (let at = 8; at + 8 <= b.length;) {
              const n = b.readUInt32BE(at);
              out2.push({ type: b.toString('latin1', at + 4, at + 8), data: b.subarray(at + 8, at + 8 + n) });
              at += 12 + n;
            }
            return out2;
          };
          const fromPNG = (() => {
            const c = pngChunks(png).find((x) => x.type === 'iTXt' && x.data.toString('latin1', 0, 16) === 'situcraft-figure');
            if (!c) return null;
            let p = c.data.indexOf(0) + 3;
            p = c.data.indexOf(0, p) + 1;
            p = c.data.indexOf(0, p) + 1;
            return JSON.parse(c.data.subarray(p).toString('utf8'));
          })();
          const phys = pngChunks(png).find((x) => x.type === 'pHYs');
          const dpiOfPng = phys ? Math.round(phys.data.readUInt32BE(0) * 0.0254) : null;
          const { fingerprint, ...rest } = manifest ?? {};
          const fingerprintOwn = createHash('sha256').update(canonical(rest)).digest('hex');
          const panel = manifest?.panels?.[0];
          const facts = panel?.facts;
          // The stains' raster in the SVG, decoded by the suite (8-bit RGBA, rows unfiltered, as
          // SituCraft writes them).
          const raster = (() => {
            const m = /<image x="[^"]*" y="[^"]*" width="[^"]*" height="[^"]*" preserveAspectRatio="none" image-rendering="pixelated" href="data:image\/png;base64,([^"]+)"/.exec(svg);
            if (!m) return null;
            const b = Buffer.from(m[1], 'base64');
            const chunks = pngChunks(b);
            const head = chunks.find((x) => x.type === 'IHDR').data;
            const width = head.readUInt32BE(0);
            const height = head.readUInt32BE(4);
            const raw = inflateSync(Buffer.concat(chunks.filter((x) => x.type === 'IDAT').map((x) => x.data)));
            const rgba = new Uint8Array(width * height * 4);
            for (let y = 0; y < height; y += 1) {
              if (raw[y * (width * 4 + 1)] !== 0) return { width, height, filtered: true };
              rgba.set(raw.subarray(y * (width * 4 + 1) + 1, (y + 1) * (width * 4 + 1)), y * width * 4);
            }
            return { width, height, rgba };
          })();
          // The suite's own composite: the view's DAPI, from the tiles' samples at the level drawn.
          await act('open_view', { view: 'A figure of the Lung' });
          const viewNow = await chrome.eval(`(() => { const p = window.situcraft.store.project; const v = p.views.find((x) => x.name === 'A figure of the Lung'); const desc = window.situcraftViewer.desc; return { view: v, grid: desc.grid, image: desc.entries.image, channels: desc.channels.map((c) => c.name) }; })()`);
          const dapi = viewNow.view.channels.find((c) => c.name === 'DAPI');
          const sampleAt = async (level) => {
            const g = viewNow.grid;
            const um = g.pixelSize * 2 ** level;
            const side = g.tile * um;
            const tiles = new Map();
            return async (x, y) => {
              const tx = Math.floor(x / side);
              const ty = Math.floor(y / side);
              const key = `${tx},${ty}`;
              if (!tiles.has(key)) {
                const r = await fetch(`${url}/api/cache/${viewNow.image}/image/${viewNow.channels.indexOf('DAPI')}/${level}/${tx}/${ty}`);
                tiles.set(key, r.ok ? { w: Number(r.headers.get('X-Tile-Width')), h: Number(r.headers.get('X-Tile-Height')), b: new Uint8Array(await r.arrayBuffer()) } : null);
              }
              const t = tiles.get(key);
              const sx = Math.floor(x / um) - tx * g.tile;
              const sy = Math.floor(y / um) - ty * g.tile;
              if (!t || sx < 0 || sy < 0 || sx >= t.w || sy >= t.h) return 0;
              return t.b[sy * t.w + sx] + 256 * t.b[t.w * t.h + sy * t.w + sx];
            };
          };
          const shade = (v) => {
            const n = Math.min(1, Math.max(0, (v - dapi.window[0]) / Math.max(dapi.window[1] - dapi.window[0], 1)));
            const c = [1, 3, 5].map((i) => Number.parseInt(dapi.color.slice(i, i + 2), 16) / 255);
            return c.map((k) => Math.round(Math.min(1, k * n) * 255));
          };
          let pixelsOff = -1;
          if (raster?.rgba && facts?.raster && dapi.gamma === 1) {
            const at = await sampleAt(facts.raster.level);
            const r = facts.region;
            pixelsOff = 0;
            for (let py = 0; py < raster.height; py += 1) {
              for (let px = 0; px < raster.width; px += 1) {
                const want = shade(await at(r.x0 + (px + 0.5) * ((r.x1 - r.x0) / raster.width), r.y0 + (py + 0.5) * ((r.y1 - r.y0) / raster.height)));
                const i = 4 * (py * raster.width + px);
                if (raster.rgba[i] !== want[0] || raster.rgba[i + 1] !== want[1] || raster.rgba[i + 2] !== want[2]) pixelsOff += 1;
              }
            }
          }
          // The Viewer's own pixels, the stain alone, against the same composite at its level.
          await act('show_tissue', { center: [750, 300], um_per_pixel: 0.3, transcripts: false, outlines: false, fill: 'none' });
          const probes = await chrome.eval(`(async () => {
            const v = window.situcraftViewer;
            await v.settled();
            const r = v.renderer;
            const [w, h] = r.size();
            const { screenToWorld } = await import('/lib/viewport.js');
            const level = v.plan().level;
            // The stain alone: a cell selected earlier, or cells chosen, are not drawn over it.
            const kept = [r.selected, r.highlight];
            r.selected = null;
            r.highlight = null;
            const out = [];
            for (const [fx, fy] of [[0.2, 0.2], [0.35, 0.6], [0.5, 0.5], [0.62, 0.31], [0.71, 0.77], [0.8, 0.45], [0.27, 0.83], [0.9, 0.15], [0.45, 0.9]]) {
              const sx = Math.floor(w * fx), sy = Math.floor(h * fy);
              const [x, y] = screenToWorld(r.camera, w, h, sx + 0.5, sy + 0.5);
              out.push({ x, y, color: r.readColor(sx, sy).slice(0, 3) });
            }
            [r.selected, r.highlight] = kept;
            return { level, out };
          })()`);
          const atViewer = await sampleAt(probes.level);
          let viewerOff = 0;
          for (const p of probes.out) {
            const want = shade(await atViewer(p.x, p.y));
            if (want.some((c, k) => Math.abs(c - p.color[k]) > 1)) viewerOff += 1;
          }
          await act('open_view', { view: 'A figure of the Lung' });
          // The drawing in the SVG: the scale bar, the outlines, the fills, the points, the text.
          // The tissue's group (its clip, no group inside it), not the legend after it.
          const groupSvg = (() => { const from = svg.slice(svg.indexOf('aria-label="The tissue of')); return from.slice(0, from.indexOf('</g>')); })();
          const bar = /<rect x="([\d.]+)" y="([\d.]+)" width="([\d.]+)" height="1.5" fill="#ffffff"\/>\s*<text[^>]*>([^<]+)<\/text>/.exec(groupSvg);
          const barUm = bar ? Number(bar[3]) * facts.umPerPt : null;
          const outline = /<path d="([^"]+)" fill="none" stroke="#e8e8e8"/.exec(groupSvg);
          const subpaths = (d) => (d.match(/M/g) ?? []).length;
          const fills = [...groupSvg.matchAll(/<path d="([^"]+)" fill="(#[0-9a-f]{6})"(?: opacity="([\d.]+)")?\/>/g)].map((m) => ({ color: m[2], opacity: Number(m[3] ?? 1), count: subpaths(m[1]) }));
          const circles = (groupSvg.match(/<circle /g) ?? []).length;
          const sizes = [...svg.matchAll(/font-size="([\d.]+)"/g)].map((m) => Number(m[1]));
          const svgTexts = [...svg.matchAll(/<text[^>]*>([^<]*)<\/text>/g)].map((m) => m[1].replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"'));
          // The PDF's text, from its content stream (the suite inflates it).
          const pdfText = pdf.toString('latin1');
          const streamAt = pdfText.indexOf('<< /Filter /FlateDecode /Length');
          const ops = (() => {
            const m = /\/Length (\d+) >>\nstream\n/.exec(pdfText.slice(streamAt));
            if (!m) return '';
            const start = streamAt + m.index + m[0].length;
            return inflateSync(pdf.subarray(start, start + Number(m[1]))).toString('latin1');
          })();
          const pdfSizes = [...ops.matchAll(/\/F[12] ([\d.]+) Tf/g)].map((m) => Number(m[1]));
          const pdfStrings = [...ops.matchAll(/\(((?:[^()\\]|\\.)*)\) Tj/g)].map((m) => m[1].replace(/\\(\d{3})/g, (_, o) => String.fromCharCode(Number.parseInt(o, 8))).replace(/\\(.)/g, '$1'));
          // The cells the region shows, from the outlines' tiles (each polygon once), and the
          // Viewer's fill of each.
          const cellsShown = await chrome.eval(`(async () => {
            const v = window.situcraftViewer;
            const desc = v.desc;
            const r = ${JSON.stringify(facts?.region ?? null)};
            const { readPolygonTile } = await import('/lib/polygons.js');
            const g = desc.grid;
            const side = g.tile * g.pixelSize;
            const info = desc.geometry.find((s) => s.name === 'cells');
            const reach = info.maxExtent;
            const base = desc.polygonsBase ?? '/api/cache/' + desc.entries.geometry + '/polygons';
            const seen = new Set();
            const across = Math.ceil(g.width / g.tile), down = Math.ceil(g.height / g.tile);
            for (let ty = Math.max(0, Math.floor((r.y0 - reach) / side)); ty <= Math.min(down - 1, Math.floor((r.y1 + reach) / side)); ty += 1) {
              for (let tx = Math.max(0, Math.floor((r.x0 - reach) / side)); tx <= Math.min(across - 1, Math.floor((r.x1 + reach) / side)); tx += 1) {
                const res = await fetch(base + '/cells/0/' + tx + '/' + ty);
                const t = readPolygonTile(await res.arrayBuffer(), Number(res.headers.get('X-Polygons')), Number(res.headers.get('X-Vertices')));
                for (let p = 0; p < t.cells.length; p += 1) {
                  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
                  for (let i = t.starts[p]; i < t.starts[p + 1]; i += 1) { x0 = Math.min(x0, t.xy[2 * i]); x1 = Math.max(x1, t.xy[2 * i]); y0 = Math.min(y0, t.xy[2 * i + 1]); y1 = Math.max(y1, t.xy[2 * i + 1]); }
                  if (x1 >= r.x0 && x0 <= r.x1 && y1 >= r.y0 && y0 <= r.y1) seen.add(t.cells[p]);
                }
              }
            }
            for (let k = 0; k < 300 && !v.clusterFill()?.labels; k += 1) await new Promise((res) => setTimeout(res, 100));
            const rgba = v.fillColors();
            const groups = {};
            for (const c of seen) {
              if (c === 0xffffffff || !rgba || rgba[4 * c + 3] === 0) continue;
              const key = '#' + [0, 1, 2].map((j) => rgba[4 * c + j].toString(16).padStart(2, '0')).join('') + '|' + Math.round(0.5 * (rgba[4 * c + 3] / 255) * 100) / 100;
              groups[key] = (groups[key] ?? 0) + 1;
            }
            return { cells: seen.size, groups };
          })()`);
          const fillGroups = Object.fromEntries(fills.map((f) => [`${f.color}|${f.opacity}`, f.count]));
          // The genes' transcripts in the region, of quality 20 or more, from the points' tiles.
          const pointsShown = await chrome.eval(`(async () => {
            const desc = window.situcraftViewer.desc;
            const { readPoints } = await import('/lib/viewer-data.js');
            const r = ${JSON.stringify(facts?.region ?? null)};
            const genes = ['EPCAM', 'PLVAP', 'CD68'].map((n) => desc.transcripts.genes.findIndex((g) => g.name === n));
            const g = desc.grid;
            const side = g.tile * g.pixelSize;
            let n = 0;
            for (let ty = Math.floor(r.y0 / side); ty <= Math.floor(r.y1 / side); ty += 1) {
              for (let tx = Math.floor(r.x0 / side); tx <= Math.floor(r.x1 / side); tx += 1) {
                const p = readPoints(await fetch('/api/cache/' + desc.entries.transcripts + '/points/' + tx + '/' + ty + '?genes=' + genes.join(',') + '&minq=20').then((res) => res.arrayBuffer()));
                for (let k = 0; k < p.genes.length; k += 1) {
                  if (!genes.includes(p.genes[k])) continue;
                  for (let i = p.starts[k]; i < p.starts[k + 1]; i += 1) if (p.x[i] >= r.x0 && p.x[i] <= r.x1 && p.y[i] >= r.y0 && p.y[i] <= r.y1) n += 1;
                }
              }
            }
            return n;
          })()`);
          const readBack = await act('read_figure', { path: out('figure.pdf') });
          const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
          check('viewer', 'a figure of a saved view: written as SVG, PDF and PNG, the same bytes again; its manifest the same in each, its fingerprint the suite\'s own; read back unchanged and drawn the same', `${made.ok ? made.message : made.message}; ${Object.entries(wrote).map(([k, w]) => `${k} ${w.ok ? 'written' : w.message}`).join(', ')}; manifests ${[fromSVG, fromPDF, fromPNG].map((m) => (same(m, manifest) ? 'the same' : 'other')).join(', ')}; fingerprint ${fingerprintOwn === fingerprint ? 'the suite\'s' : 'other'}; PNG ${dpiOfPng} dpi; ${readBack.message ?? readBack.error}`,
            made.ok && Object.values(wrote).every((w) => w.ok) && bytes('figure.svg').equals(bytes('figure-again.svg')) && bytes('figure.pdf').equals(bytes('figure-again.pdf')) && manifest?.format === 'situcraft-figure-manifest'
            && same(fromSVG, manifest) && same(fromPDF, manifest) && same(fromPNG, manifest) && fingerprintOwn === fingerprint && dpiOfPng === 300 && readBack.ok && readBack.data.intact && readBack.data.panels.every((p) => p.status === 'unchanged' && p.same === true),
            'the same bytes; one manifest, the suite\'s fingerprint; 300 dpi; unchanged, drawn the same');
          check('viewer', 'a figure\'s tissue: its stains the suite\'s own composite of the samples, pixel for pixel, and the Viewer\'s within one level; a scale bar its length in µm; the cells whose outlines meet the region, filled as the Viewer fills them; the genes\' transcripts in the region; no text below 6 pt, the PDF\'s text the SVG\'s', `raster ${raster ? `${raster.width} × ${raster.height}${raster.filtered ? ' (filtered)' : ''}` : 'not found'} at level ${facts?.raster?.level}, ${pixelsOff} pixels off; the Viewer at level ${probes.level}, ${viewerOff} of ${probes.out.length} probes off; scale bar "${bar?.[4]}" ${barUm?.toFixed(3)} µm; cells ${facts?.cells} drawn, ${outline ? subpaths(outline[1]) : 0} outlined, ${cellsShown.cells} the suite's; fills ${JSON.stringify(fillGroups)} against ${JSON.stringify(cellsShown.groups)}; ${circles} points, ${pointsShown} the suite's; text ${Math.min(...sizes)} to ${Math.max(...sizes)} pt (PDF ${Math.min(...pdfSizes)}); PDF strings ${same([...pdfStrings].sort(), [...svgTexts].sort()) ? 'the SVG\'s' : JSON.stringify(pdfStrings.slice(0, 6))}`,
            raster?.rgba && raster.width === facts.raster.width && raster.height === facts.raster.height && pixelsOff === 0 && viewerOff === 0
            && bar && bar[4] === `${facts.scaleBar.um} µm` && Math.abs(barUm - facts.scaleBar.um) < 0.01 * facts.umPerPt
            && outline && subpaths(outline[1]) === facts.cells && facts.cells === cellsShown.cells && same(Object.keys(fillGroups).sort(), Object.keys(cellsShown.groups).sort()) && Object.keys(fillGroups).every((k) => fillGroups[k] === cellsShown.groups[k])
            && circles === facts.transcripts && circles === pointsShown && circles > 100
            && Math.min(...sizes) >= 6 && Math.min(...pdfSizes) >= 6 && same([...pdfStrings].sort(), [...svgTexts].sort()),
            'every pixel the suite\'s; every probe within one level; the bar its length; the cells, fills and transcripts the suite\'s; text 6 pt or more, the same in the PDF');
        }

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
            background: (document.querySelector('.seg-refined')?.textContent ?? '').match(/The model's background accounts for [\\d.]+% of the modeled transcripts: false decodes [\\d.]+%, measured by [\\d,]+ negative-control transcripts/)?.[0] ?? '' };
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

        // Regions and reviews on the Lung tissue (wave 5), kept in the project this release saves:
        // a tumor nest drawn and commented on, an edge's artifact excluded with its cells, a cell's
        // segmentation of concern.
        {
          const drawnLung = [
            await act('draw_region', { dataset: 'xenium-lung-2fov', polygon: [[600, 250], [800, 260], [790, 420], [610, 400]], class: 'Tumor', name: 'Tumor nest', by: 'validation' }),
            await act('draw_region', { dataset: 'xenium-lung-2fov', rectangle: [430, 120, 510, 180], class: 'Ignore*', name: 'Edge artifact', by: 'validation' }),
          ];
          const reviewedLung = [
            await act('review', { dataset: 'xenium-lung-2fov', action: 'exclude', region: 'Edge artifact', note: 'Folded tissue at the edge', by: 'validation' }),
            await act('review', { dataset: 'xenium-lung-2fov', action: 'comment', region: 'Tumor nest', note: 'Check against the H&E when it is scanned', by: 'validation' }),
            await act('review', { dataset: 'xenium-lung-2fov', action: 'concern', cells: ['#100'], concern: 'merged', note: 'Two nuclei in one outline', by: 'validation' }),
          ];
          const lungState = await act('reviews', { dataset: 'xenium-lung-2fov' });
          check('viewer', 'regions and reviews on the Lung tissue: a region drawn and commented on, another excluded with its cells, a cell of concern', `${[...drawnLung, ...reviewedLung].map((x) => x.message ?? x.error).join(' | ')}; ${lungState.message ?? lungState.error}`,
            [...drawnLung, ...reviewedLung].every((x) => x.ok) && lungState.data?.counts.excluded === reviewedLung[0].data?.review.cells?.length && lungState.data.counts.excluded > 0 && lungState.data.counts.concern === 1,
            'each as asked; the excluded cells those in the region');
        }

        // Cell graphs of the Lung cells (wave 6): each kind built by remote control over the cells a
        // review did not exclude; a radius graph's edges those of the suite's own exhaustive search
        // over its cells; a graph of a region's cells over those the suite counts in it; its edges
        // drawn where the graph puts them, the selected cell's neighbors listed as the graph has
        // them, and a saved view keeping the graph.
        {
          const built = {};
          for (const [kind, args] of [['delaunay', {}], ['knn', { k: 6 }], ['radius', { radius: 20 }], ['contact', { gap: 1 }]]) built[kind] = await act('build_graph', { dataset: 'xenium-lung-2fov', kind, ...args });
          const excludedLung = (await act('reviews', { dataset: 'xenium-lung-2fov' })).data?.counts.excluded ?? -1;
          const graphsOk = Object.values(built).every((b) => b.ok && b.data.cells.inGraph === 11898 - excludedLung && b.data.cells.excluded === excludedLung && b.data.edges > 0);
          check('viewer', 'remote control: build_graph builds each kind of graph of the Lung cells, those a review excluded left out', Object.values(built).map((b) => b.message ?? b.error).join(' | '),
            graphsOk && excludedLung > 0 && built.contact.data.shared && built.knn.data.isolated === 0, `${11898 - excludedLung} cells each (${excludedLung} excluded); shared boundaries in the contact graph`);
          // The radius graph against an exhaustive search over its own cells' centroids.
          const radius = built.radius.ok ? await chrome.eval(`(async () => {
            const buf = await (await fetch('/api/graphs/${built.radius.data.id}/edges')).arrayBuffer();
            const v = new DataView(buf);
            const n = v.getUint32(0, true), m = v.getUint32(4, true);
            const x = new Float32Array(buf, 12 + 4 * n, n), y = new Float32Array(buf, 12 + 8 * n, n);
            const off = new Uint32Array(buf, 12 + 12 * n, n + 1), nb = new Uint32Array(buf, 16 + 16 * n, m);
            let missing = 0, extra = 0, ends = 0;
            for (let i = 0; i < n; i += 1) {
              const mine = new Set(nb.subarray(off[i], off[i + 1]));
              for (let j = 0; j < n; j += 1) {
                if (j === i) continue;
                const dx = x[j] - x[i], dy = y[j] - y[i];
                const near = Math.sqrt(dx * dx + dy * dy) <= 20;
                if (near) ends += 1;
                if (near && !mine.has(j)) missing += 1;
                if (!near && mine.has(j)) extra += 1;
              }
            }
            return { n, m, ends, missing, extra };
          })()`) : null;
          check('viewer', 'a radius graph\'s edges: those of the suite\'s own search over every pair of its cells', JSON.stringify(radius), radius && radius.ends === radius.m && radius.missing === 0 && radius.extra === 0, 'every pair within 20 µm, and no other');
          // A graph of the cells in the tumor nest.
          const nest = [[600, 250], [800, 260], [790, 420], [610, 400], [600, 250]];
          const windingIn = (ring, px, py) => {
            let w = 0;
            for (let i = 0; i + 1 < ring.length; i += 1) {
              const [ax, ay] = ring[i];
              const [bx, by] = ring[i + 1];
              const side = (bx - ax) * (py - ay) - (px - ax) * (by - ay);
              if (ay <= py) {
                if (by > py && side > 0) w += 1;
              } else if (by <= py && side < 0) w -= 1;
            }
            return w;
          };
          await act('show_tissue', { dataset: 'xenium-lung-2fov', segmentation: 'instrument' });
          const lungCells = await chrome.eval('(() => { const c = window.situcraftViewer.cellsDrawn(); return { x: Array.from(c.x), y: Array.from(c.y) }; })()');
          const inNest = lungCells.x.reduce((n, x, i) => n + (windingIn(nest, x, lungCells.y[i]) !== 0 ? 1 : 0), 0);
          const nestGraph = await act('build_graph', { dataset: 'xenium-lung-2fov', kind: 'delaunay', regions: ['Tumor nest'] });
          check('viewer', 'a graph of a region\'s cells: built over the cells the suite counts in it', `${nestGraph.message ?? nestGraph.error}; the suite counts ${inNest}`,
            nestGraph.ok && nestGraph.data.cells.inGraph === inNest && nestGraph.data.cells.regions?.[0]?.name === 'Tumor nest', `${inNest} cells`);
          // Drawn: the Delaunay graph close up; the overlay light at edges' midpoints and clear
          // between cells; a cell's neighbors in the inspector the graph's.
          const shownGraph = built.delaunay.ok ? await act('show_tissue', { dataset: 'xenium-lung-2fov', graph: built.delaunay.data.id, center: [700, 330], um_per_pixel: 0.25, genes: [], cell: 'nheihaca-1' }) : null;
          await chrome.eval('new Promise((r) => setTimeout(r, 500))');
          const drawnGraph = await chrome.eval(`(async () => {
            const v = window.situcraftViewer;
            const g = v.graph()?.graph;
            if (!g) return null;
            const canvas = document.querySelector('.viewer-graph');
            const ctx = canvas.getContext('2d');
            const cam = v.renderer.camera;
            const [w, hh] = v.renderer.size();
            const at = (x, y) => [Math.round((x - cam.cx) * cam.scale + w / 2), Math.round((y - cam.cy) * cam.scale + hh / 2)];
            let lit = 0, probed = 0;
            for (let i = 0; i < g.n && probed < 40; i += 1) {
              const [sx, sy] = at(g.x[i], g.y[i]);
              if (sx < w * 0.2 || sx > w * 0.8 || sy < hh * 0.2 || sy > hh * 0.8) continue;
              const j = g.neighbors[g.offsets[i]];
              // Within a pixel of the midpoint, light: the line's white over its casing (the casing
              // alone is black).
              const [mx, my] = at((g.x[i] + g.x[j]) / 2, (g.y[i] + g.y[j]) / 2);
              const px = ctx.getImageData(mx - 1, my - 1, 3, 3).data;
              probed += 1;
              let light = false;
              for (let k = 0; k < px.length; k += 4) if (px[k + 3] > 200 && px[k] > 150) light = true;
              if (light) lit += 1;
            }
            const d = v.detail();
            const i = g.indexOf[d.row];
            const want = [];
            for (let k = g.offsets[i]; k < g.offsets[i + 1]; k += 1) want.push(g.rows[g.neighbors[k]]);
            for (let t = 0; t < 50 && !document.querySelector('.viewer-neighbor-list li'); t += 1) await new Promise((r) => setTimeout(r, 100));
            const listed = [...document.querySelectorAll('.viewer-neighbor-list li')].length;
            return { lit, probed, shown: v.graphShown(), neighbors: want.length, listed, layers: v.layers().cells.graph };
          })()`);
          check('viewer', 'a graph drawn: its edges where the graph puts them, a cell\'s neighbors listed as the graph has them', `${shownGraph?.message ?? shownGraph?.error}; ${JSON.stringify(drawnGraph)}`,
            shownGraph?.ok && drawnGraph && drawnGraph.probed >= 20 && drawnGraph.lit === drawnGraph.probed && drawnGraph.shown.count > 0 && !drawnGraph.shown.tooMany && drawnGraph.listed === drawnGraph.neighbors && drawnGraph.neighbors > 0, 'every edge probed drawn; the neighbors listed');
          const graphView = await act('save_view', { name: 'The Delaunay graph close up' });
          await act('show_tissue', { dataset: 'xenium-lung-2fov', graph: 'none' });
          // Cleared: no graph, and nothing left on the overlay.
          const cleared = await chrome.eval(`(() => {
            const c = document.querySelector('.viewer-graph');
            const px = c.getContext('2d').getImageData(0, 0, c.width, c.height).data;
            let alpha = 0;
            for (let k = 3; k < px.length; k += 4) alpha += px[k];
            return window.situcraftViewer.graph() === null && alpha === 0 ? null : { alpha };
          })()`);
          const graphReopened = await act('open_view', { view: 'The Delaunay graph close up' });
          await chrome.eval('new Promise((r) => setTimeout(r, 500))');
          const reGraph = await chrome.eval('window.situcraftViewer.graph()?.id ?? null');
          check('viewer', 'a view of a graph reopens exactly, the graph drawn again', `${graphReopened.message ?? graphReopened.error}; cleared ${cleared === null}, drawn again ${reGraph}`,
            graphView.ok && graphReopened.ok && graphReopened.data.exact === true && cleared === null && reGraph === built.delaunay.data?.id, 'exactly as saved');
        }

        // Neighborhood enrichment and co-occurrence of the Lung cells' classes (wave 6, slice 2), each
        // number against the suite's own: the counts from the graph's edges and the cells' labels, the
        // expectation from its formula, a pair's cells shown, and a co-occurrence ratio from every pair
        // of cells.
        {
          const graph = await act('build_graph', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', kind: 'delaunay' });
          // The instrument's cells labeled from the panel's markers (earlier in the suite).
          const labelsId = graph.ok ? await chrome.eval(`window.situcraft.store.project.artifacts.find((a) => a.kind === 'labels' && a.segmentation === ${JSON.stringify(graph.data.segmentation.id)} && !a.source && !a.reference && a.state === 'complete')?.id ?? null`) : null;
          const markers = { ok: Boolean(labelsId), data: { id: labelsId }, message: 'no label set from markers of the instrument\'s cells' };
          const asked = markers.ok && graph.ok ? await act('spatial_analysis', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', question: 'enrichment', labels: markers.data.id, level: 1, graph: graph.data.id, permutations: 1000 }) : null;
          const own = asked?.ok ? await chrome.eval(`(async () => {
            const a = await (await fetch('/api/artifacts/${asked.data.id}/files/analysis.json')).json();
            const cb = await (await fetch('/api/analyses/${asked.data.id}/cells')).arrayBuffer();
            const n = new DataView(cb).getUint32(0, true);
            const label = new Int32Array(cb, 4 + 4 * n, n);
            const gb = await (await fetch('/api/graphs/${graph.data.id}/edges')).arrayBuffer();
            const gv = new DataView(gb);
            const gn = gv.getUint32(0, true), m = gv.getUint32(4, true);
            const off = new Uint32Array(gb, 12 + 12 * gn, gn + 1), nb = new Uint32Array(gb, 16 + 16 * gn, m);
            const K = a.labels.names.length;
            // The directed counts: each cell's neighbors by label; the edges between labeled cells.
            const count = new Float64Array(K * K);
            const cells = new Float64Array(K);
            let N = 0, E = 0;
            for (let i = 0; i < gn; i += 1) {
              if (label[i] < 0) continue;
              cells[label[i]] += 1;
              N += 1;
              for (let k = off[i]; k < off[i + 1]; k += 1) {
                const j = nb[k];
                if (label[j] < 0) continue;
                count[label[i] * K + label[j]] += 1;
                if (j > i) E += 1;
              }
            }
            // The expectation: each edge's ends take A and B with chance n_A·(n_B − [A = B]) / (N·(N − 1)),
            // in either order.
            let countOff = 0, expOff = 0;
            for (let x = 0; x < K; x += 1) {
              for (let y = 0; y < K; y += 1) {
                const q = x * K + y;
                if (count[q] !== a.enrichment.count[q]) countOff += 1;
                const e = 2 * E * cells[x] * (cells[y] - (x === y ? 1 : 0)) / (N * (N - 1));
                expOff = Math.max(expOff, Math.abs(e - a.enrichment.expected[q]) / Math.max(1, e));
              }
            }
            return { K, N, E, edges: a.enrichment.edges, countOff, expOff };
          })()`) : null;
          check('viewer', 'neighborhood enrichment by remote control: every count the suite\'s own (the graph\'s edges by the cells\' labels), the expectation its formula\'s', `${asked?.message ?? asked?.error ?? markers.message}; ${JSON.stringify(own)}`,
            asked?.ok && own && own.countOff === 0 && own.expOff < 1e-9 && own.E === own.edges && asked.data.pairs.length === own.K * (own.K + 1) / 2 && asked.data.pairs.some((p) => p.padj < 0.05), 'the same counts; the expectation within 10⁻⁹');
          // A pair's cells on the tissue: those with a neighbor of the other label, as the suite counts them.
          const top = asked?.ok ? asked.data.pairs.find((p) => p.a !== p.b && p.padj < 0.05) : null;
          const shownPair = top ? await act('show_tissue', { dataset: 'xenium-lung-2fov', cells_of: { analysis: asked.data.id, a: top.a, b: top.b } }) : null;
          const pairCount = top ? await chrome.eval(`(async () => {
            const a = await (await fetch('/api/artifacts/${asked.data.id}/files/analysis.json')).json();
            const cb = await (await fetch('/api/analyses/${asked.data.id}/cells')).arrayBuffer();
            const n = new DataView(cb).getUint32(0, true);
            const label = new Int32Array(cb, 4 + 4 * n, n);
            const gb = await (await fetch('/api/graphs/${graph.data.id}/edges')).arrayBuffer();
            const gn = new DataView(gb).getUint32(0, true), m = new DataView(gb).getUint32(4, true);
            const off = new Uint32Array(gb, 12 + 12 * gn, gn + 1), nb = new Uint32Array(gb, 16 + 16 * gn, m);
            const A = a.labels.names.indexOf(${JSON.stringify(top.a)}), B = a.labels.names.indexOf(${JSON.stringify(top.b)});
            let cells = 0;
            for (let i = 0; i < gn; i += 1) {
              let touches = false;
              for (let k = off[i]; k < off[i + 1]; k += 1) {
                const j = nb[k];
                if ((label[i] === A && label[j] === B) || (label[i] === B && label[j] === A)) touches = true;
              }
              if (touches) cells += 1;
            }
            return { suite: cells, shown: window.situcraftViewer.focus(), graph: window.situcraftViewer.graph()?.id };
          })()`) : null;
          check('viewer', 'a pair\'s cells shown on the tissue: those with a neighbor of the other label, its graph drawn', `${shownPair?.message ?? shownPair?.error}; ${JSON.stringify(pairCount)}`,
            shownPair?.ok && pairCount && pairCount.shown?.cells === pairCount.suite && pairCount.suite > 0 && pairCount.graph === graph.data.id, `${pairCount?.suite} cells`);
          // Co-occurrence: one pair's ratio at one distance from every pair of cells.
          const co = markers.ok ? await act('spatial_analysis', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', question: 'cooccurrence', labels: markers.data.id, level: 1, up_to: 60, every: 20 }) : null;
          const coOwn = co?.ok ? await chrome.eval(`(async () => {
            const a = await (await fetch('/api/artifacts/${co.data.id}/files/analysis.json')).json();
            const cb = await (await fetch('/api/analyses/${co.data.id}/cells')).arrayBuffer();
            const n = new DataView(cb).getUint32(0, true);
            const rows = new Uint32Array(cb, 4, n), label = new Int32Array(cb, 4 + 4 * n, n);
            // The segmentation's centroids, as the graph of the same cells holds them.
            const gb = await (await fetch('/api/graphs/${graph.data.id}/edges')).arrayBuffer();
            const gn = new DataView(gb).getUint32(0, true);
            const grows = new Uint32Array(gb, 12, gn), gx = new Float32Array(gb, 12 + 4 * gn, gn), gy = new Float32Array(gb, 12 + 8 * gn, gn);
            const t = { x: new Float64Array(a.cells.segmentation), y: new Float64Array(a.cells.segmentation) };
            for (let i = 0; i < gn; i += 1) {
              t.x[grows[i]] = gx[i];
              t.y[grows[i]] = gy[i];
            }
            const K = a.labels.names.length, d = 40, k = a.cooccurrence.distances.indexOf(d);
            const count = new Float64Array(K * K);
            const idx = [];
            for (let i = 0; i < n; i += 1) if (label[i] >= 0) idx.push(i);
            for (let p = 0; p < idx.length; p += 1) {
              const i = idx[p];
              for (let q = p + 1; q < idx.length; q += 1) {
                const j = idx[q];
                const dx = t.x[rows[j]] - t.x[rows[i]], dy = t.y[rows[j]] - t.y[rows[i]];
                if (Math.sqrt(dx * dx + dy * dy) <= d) {
                  count[label[i] * K + label[j]] += 1;
                  count[label[j] * K + label[i]] += 1;
                }
              }
            }
            const row = (x) => count.slice(x * K, (x + 1) * K).reduce((s, v) => s + v, 0);
            const total = count.reduce((s, v) => s + v, 0);
            let worst = 0;
            for (let x = 0; x < K; x += 1) for (let y = 0; y < K; y += 1) if (row(x) > 0 && row(y) > 0) worst = Math.max(worst, Math.abs(count[x * K + y] * total / (row(x) * row(y)) - a.cooccurrence.ratio[k][x * K + y]));
            return { K, k, worst };
          })()`) : null;
          check('viewer', 'co-occurrence by remote control: within 40 µm, every ratio from the suite\'s own count of every pair of cells', `${co?.message ?? co?.error}; ${JSON.stringify(coOwn)}`,
            co?.ok && coOwn && coOwn.k === 1 && coOwn.worst < 1e-9, 'within 10⁻⁹');
        }

        // Nearest distances and Ripley's K of the Lung classes (wave 6, slice 3), against the suite's
        // own: one pair's median nearest distance by an exhaustive search; K without a correction in
        // the tumor nest from its own count of pairs and the polygon's area; and the cells shown
        // within a radius.
        {
          const graph = await act('build_graph', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', kind: 'delaunay' });
          const labelsId = graph.ok ? await chrome.eval(`window.situcraft.store.project.artifacts.find((a) => a.kind === 'labels' && a.segmentation === ${JSON.stringify(graph.data.segmentation.id)} && !a.source && !a.reference && a.state === 'complete')?.id ?? null`) : null;
          const dist = labelsId ? await act('spatial_analysis', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', question: 'distances', labels: labelsId, level: 1, permutations: 49 }) : null;
          const ripley = labelsId ? await act('spatial_analysis', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', question: 'ripley', labels: labelsId, level: 1, window: 'Tumor nest', correction: 'none', up_to: 40, every: 10, permutations: 19 }) : null;
          const own = dist?.ok && ripley?.ok ? await chrome.eval(`(async () => {
            const read = async (id) => {
              const a = await (await fetch('/api/artifacts/' + id + '/files/analysis.json')).json();
              const cb = await (await fetch('/api/analyses/' + id + '/cells')).arrayBuffer();
              const n = new DataView(cb).getUint32(0, true);
              return { a, rows: new Uint32Array(cb, 4, n), label: new Int32Array(cb, 4 + 4 * n, n) };
            };
            // The segmentation's centroids, as the graph of all the cells holds them.
            const gb = await (await fetch('/api/graphs/${graph.data.id}/edges')).arrayBuffer();
            const gn = new DataView(gb).getUint32(0, true);
            const grows = new Uint32Array(gb, 12, gn), gx = new Float32Array(gb, 12 + 4 * gn, gn), gy = new Float32Array(gb, 12 + 8 * gn, gn);
            const X = new Map(), Y = new Map();
            for (let i = 0; i < gn; i += 1) {
              X.set(grows[i], gx[i]);
              Y.set(grows[i], gy[i]);
            }
            const d = await read(${JSON.stringify(dist?.data?.id)});
            const K = d.a.labels.names.length, A = d.a.labels.names.indexOf('Stromal'), B = d.a.labels.names.indexOf('Endothelial');
            const reach = d.a.nearest.reach;
            const near = [];
            for (let i = 0; i < d.rows.length; i += 1) {
              if (d.label[i] !== A) continue;
              let best = Infinity;
              for (let j = 0; j < d.rows.length; j += 1) {
                if (j === i || d.label[j] !== B) continue;
                const dx = X.get(d.rows[j]) - X.get(d.rows[i]), dy = Y.get(d.rows[j]) - Y.get(d.rows[i]);
                best = Math.min(best, Math.fround(Math.sqrt(dx * dx + dy * dy)));
              }
              near.push(Math.min(best, reach));
            }
            near.sort((u, v) => u - v);
            const m = near.length;
            const median = m % 2 ? near[(m - 1) / 2] : (near[m / 2 - 1] + near[m / 2]) / 2;
            // Ripley's K without a correction in the tumor nest: its area by the shoelace, its cells by
            // a winding number, and the pairs within each radius.
            const r = await read(${JSON.stringify(ripley?.data?.id)});
            const nest = [[600, 250], [800, 260], [790, 420], [610, 400]];
            let area = 0;
            for (let k = 0; k < 4; k += 1) area += nest[k][0] * nest[(k + 1) % 4][1] - nest[(k + 1) % 4][0] * nest[k][1];
            area = Math.abs(area) / 2;
            const inside = (px, py) => {
              let w = 0;
              for (let k = 0; k < 4; k += 1) {
                const [ax, ay] = nest[k], [bx, by] = nest[(k + 1) % 4];
                const side = (bx - ax) * (py - ay) - (px - ax) * (by - ay);
                if (ay <= py) {
                  if (by > py && side > 0) w += 1;
                } else if (by <= py && side < 0) w -= 1;
              }
              return w !== 0;
            };
            const R = r.a.ripley.radii.length, E = r.a.labels.names.indexOf('Epithelial');
            const pts = [];
            for (let i = 0; i < r.rows.length; i += 1) if (r.label[i] === E && inside(X.get(r.rows[i]), Y.get(r.rows[i]))) pts.push([X.get(r.rows[i]), Y.get(r.rows[i])]);
            let worst = 0;
            r.a.ripley.radii.forEach((rad, k) => {
              let pairs = 0;
              for (let i = 0; i < pts.length; i += 1) for (let j = i + 1; j < pts.length; j += 1) if (Math.sqrt((pts[j][0] - pts[i][0]) ** 2 + (pts[j][1] - pts[i][1]) ** 2) <= rad) pairs += 2;
              const k0 = area * pairs / (pts.length * (pts.length - 1));
              worst = Math.max(worst, Math.abs(r.a.ripley.k[(E * r.a.labels.names.length + E) * R + k] / k0 - 1));
            });
            return { median, recorded: d.a.nearest.median[A * K + B], cells: pts.length, recordedCells: r.a.ripley.cells[E], area, recordedArea: r.a.window.area, worst };
          })()`) : null;
          check('viewer', 'nearest distances and Ripley\'s K by remote control: a pair\'s median nearest distance the suite\'s exhaustive search\'s; K in the tumor nest, without a correction, from the suite\'s own count of pairs and the polygon\'s area', `${dist?.message ?? dist?.error}; ${ripley?.message ?? ripley?.error}; ${JSON.stringify(own)}`,
            dist?.ok && ripley?.ok && own && Math.abs(own.median - own.recorded) < 1e-6 && own.cells === own.recordedCells && Math.abs(own.area - own.recordedArea) < 1e-6 && own.worst < 1e-9, 'the same median, cells, area and K');
          // The cells within a radius shown: stromal cells with another within 30 µm, as the suite counts them.
          const shownNear = dist?.ok ? await act('show_tissue', { dataset: 'xenium-lung-2fov', cells_of: { analysis: ripley.data.id, a: 'Stromal', b: 'Stromal', distance: 30 } }) : null;
          const nearCount = shownNear?.ok ? await chrome.eval(`(async () => {
            const a = await (await fetch('/api/artifacts/${ripley.data.id}/files/analysis.json')).json();
            const cb = await (await fetch('/api/analyses/${ripley.data.id}/cells')).arrayBuffer();
            const n = new DataView(cb).getUint32(0, true);
            const rows = new Uint32Array(cb, 4, n), label = new Int32Array(cb, 4 + 4 * n, n);
            const t = window.situcraftViewer.cellsDrawn();
            const S = a.labels.names.indexOf('Stromal');
            let cells = 0;
            for (let i = 0; i < n; i += 1) {
              if (label[i] !== S) continue;
              for (let j = 0; j < n; j += 1) {
                if (j !== i && label[j] === S && Math.sqrt((t.x[rows[j]] - t.x[rows[i]]) ** 2 + (t.y[rows[j]] - t.y[rows[i]]) ** 2) <= 30) {
                  cells += 1;
                  break;
                }
              }
            }
            return { suite: cells, shown: window.situcraftViewer.focus() };
          })()`) : null;
          check('viewer', 'the cells within a radius shown on the tissue: stromal cells with another within 30 µm, as the suite counts them', `${shownNear?.message ?? shownNear?.error}; ${JSON.stringify(nearCount)}`,
            shownNear?.ok && nearCount && nearCount.suite > 0 && nearCount.shown?.cells === nearCount.suite, `${nearCount?.suite} cells`);

          // Regions compared and a profile across an edge (wave 6, slice 4), against the suite's own:
          // each label's cells in the tumor nest and in the rest of the tissue by a winding number,
          // and each band's by the suite's own signed distance to the nest's outline; the cells shown
          // at its margin.
          const compared = labelsId ? await act('spatial_analysis', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', question: 'regions', labels: labelsId, level: 1, regions: ['Tumor nest'] }) : null;
          const edge = labelsId ? await act('spatial_analysis', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', question: 'boundary', labels: labelsId, level: 1, region: 'Tumor nest', reach: 40, every: 10, margin: 10 }) : null;
          const regionsOwn = compared?.ok && edge?.ok ? await chrome.eval(`(async () => {
            const read = async (id) => {
              const a = await (await fetch('/api/artifacts/' + id + '/files/analysis.json')).json();
              const cb = await (await fetch('/api/analyses/' + id + '/cells')).arrayBuffer();
              const n = new DataView(cb).getUint32(0, true);
              return { a, n, rows: new Uint32Array(cb, 4, n), label: new Int32Array(cb, 4 + 4 * n, n), stratum: new Int32Array(cb, 4 + 8 * n, n) };
            };
            // The segmentation's centroids, as the graph of all the cells holds them (the Spatial view
            // is shown, not the Viewer).
            const gb = await (await fetch('/api/graphs/${graph?.data?.id}/edges')).arrayBuffer();
            const gn = new DataView(gb).getUint32(0, true);
            const grows = new Uint32Array(gb, 12, gn), gx = new Float32Array(gb, 12 + 4 * gn, gn), gy = new Float32Array(gb, 12 + 8 * gn, gn);
            const t = { x: [], y: [] };
            for (let i = 0; i < gn; i += 1) {
              t.x[grows[i]] = gx[i];
              t.y[grows[i]] = gy[i];
            }
            const nest = [[600, 250], [800, 260], [790, 420], [610, 400]];
            const inside = (px, py) => {
              let w = 0;
              for (let k = 0; k < 4; k += 1) {
                const [ax, ay] = nest[k], [bx, by] = nest[(k + 1) % 4];
                const side = (bx - ax) * (py - ay) - (px - ax) * (by - ay);
                if (ay <= py) {
                  if (by > py && side > 0) w += 1;
                } else if (by <= py && side < 0) w -= 1;
              }
              return w !== 0;
            };
            const signed = (px, py) => {
              let best = Infinity;
              for (let k = 0; k < 4; k += 1) {
                const [ax, ay] = nest[k], [bx, by] = nest[(k + 1) % 4];
                const dx = bx - ax, dy = by - ay;
                const u = Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / (dx * dx + dy * dy)));
                const ex = px - (ax + u * dx), ey = py - (ay + u * dy);
                best = Math.min(best, Math.sqrt(ex * ex + ey * ey));
              }
              return inside(px, py) ? -best : best;
            };
            const c = await read(${JSON.stringify(compared?.data?.id)});
            const K = c.a.labels.names.length;
            const count = new Array(2 * K).fill(0);
            let membership = 0;
            for (let i = 0; i < c.n; i += 1) {
              if (c.stratum[i] === (1 << 30)) continue;
              const inNest = inside(t.x[c.rows[i]], t.y[c.rows[i]]);
              if ((c.stratum[i] === 1) !== inNest) membership += 1;
              if (c.label[i] >= 0) count[(inNest ? 0 : 1) * K + c.label[i]] += 1;
            }
            const regionsDiffer = count.filter((v, q) => v !== c.a.regions.count[q]).length;
            const e = await read(${JSON.stringify(edge?.data?.id)});
            const edges = e.a.boundary.edges;
            const bands = new Array((edges.length + 1) * K).fill(0);
            let bandOf = 0;
            let margin = 0;
            const E = e.a.labels.names.indexOf('Epithelial');
            for (let i = 0; i < e.n; i += 1) {
              if (e.stratum[i] === 0) continue;
              const s = signed(t.x[e.rows[i]], t.y[e.rows[i]]);
              const b = edges.filter((v) => v <= s).length;
              if (e.stratum[i] - 1 !== b) bandOf += 1;
              if (e.label[i] >= 0) bands[b * K + e.label[i]] += 1;
              if (e.label[i] === E && s >= -10 && s < 10) margin += 1;
            }
            const bandsDiffer = bands.filter((v, q) => v !== e.a.boundary.count[q]).length;
            return { regionsDiffer, membership, bandsDiffer, bandOf, inNest: count.slice(0, K).reduce((a, v) => a + v, 0), margin };
          })()`) : null;
          check('viewer', 'regions compared and a profile across an edge by remote control: each label\'s cells in the tumor nest and the rest, and in each band across its edge, as the suite counts them', `${compared?.message ?? compared?.error}; ${edge?.message ?? edge?.error}; ${JSON.stringify(regionsOwn)}`,
            compared?.ok && edge?.ok && regionsOwn && regionsOwn.inNest > 0 && regionsOwn.regionsDiffer === 0 && regionsOwn.membership === 0 && regionsOwn.bandsDiffer === 0 && regionsOwn.bandOf === 0, 'the same counts');
          const shownMargin = edge?.ok ? await act('show_tissue', { dataset: 'xenium-lung-2fov', cells_of: { analysis: edge.data.id, a: 'Epithelial', zone: 'margin' } }) : null;
          const marginShown = shownMargin?.ok ? await chrome.eval('window.situcraftViewer.focus()') : null;
          check('viewer', 'the cells at a region\'s margin shown on the tissue: epithelial cells within 10 µm of the tumor nest\'s edge, as the suite counts them', `${shownMargin?.message ?? shownMargin?.error}; ${JSON.stringify(marginShown)}`,
            shownMargin?.ok && regionsOwn && regionsOwn.margin > 0 && marginShown?.cells === regionsOwn.margin, `${regionsOwn?.margin} cells`);

          // Niches (wave 6, slice 5), against the suite's own: each cell's 10 nearest labeled cells
          // by an exhaustive search, each niche's make-up the mean of its cells' windows, every cell
          // nearest its own niche's center (k-means's fixed point); then the cells filled by niche,
          // and one niche's cells shown.
          const niches = labelsId ? await act('spatial_analysis', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', question: 'niches', labels: labelsId, level: 1, niches: 5, seeds: 2 }) : null;
          const nichesOwn = niches?.ok ? await chrome.eval(`(async () => {
            const a = await (await fetch('/api/artifacts/${niches.data.id}/files/analysis.json')).json();
            const cb = await (await fetch('/api/analyses/${niches.data.id}/cells')).arrayBuffer();
            const n = new DataView(cb).getUint32(0, true);
            const rows = new Uint32Array(cb, 4, n), label = new Int32Array(cb, 4 + 4 * n, n), stratum = new Int32Array(cb, 4 + 8 * n, n);
            const gb = await (await fetch('/api/graphs/${graph?.data?.id}/edges')).arrayBuffer();
            const gn = new DataView(gb).getUint32(0, true);
            const grows = new Uint32Array(gb, 12, gn), gx = new Float32Array(gb, 12 + 4 * gn, gn), gy = new Float32Array(gb, 12 + 8 * gn, gn);
            const X = [], Y = [];
            for (let i = 0; i < gn; i += 1) {
              X[grows[i]] = gx[i];
              Y[grows[i]] = gy[i];
            }
            const K = a.labels.names.length, N = a.niches.niches;
            const labeled = [];
            for (let i = 0; i < n; i += 1) if (label[i] >= 0) labeled.push(i);
            const sums = new Float64Array(N * K), size = new Array(N).fill(0);
            const comp = new Float64Array(n * K);
            for (let i = 0; i < n; i += 1) {
              const px = X[rows[i]], py = Y[rows[i]];
              const near = labeled.map((j) => [(X[rows[j]] - px) ** 2 + (Y[rows[j]] - py) ** 2, j]).sort((u, v) => u[0] - v[0] || u[1] - v[1]).slice(0, 10);
              for (const [, j] of near) comp[i * K + label[j]] += 1 / near.length;
              const v = stratum[i] - 1;
              if (v < 0) continue;
              size[v] += 1;
              for (let k = 0; k < K; k += 1) sums[v * K + k] += comp[i * K + k];
            }
            let worst = 0;
            for (let v = 0; v < N; v += 1) for (let k = 0; k < K; k += 1) worst = Math.max(worst, Math.abs(sums[v * K + k] / size[v] - a.niches.composition[v * K + k]));
            // Each cell nearest its own niche's center.
            let elsewhere = 0;
            for (let i = 0; i < n; i += 1) {
              let best = -1, bd = Infinity;
              for (let v = 0; v < N; v += 1) {
                let d = 0;
                for (let k = 0; k < K; k += 1) d += (comp[i * K + k] - a.niches.composition[v * K + k]) ** 2;
                if (d < bd - 1e-12) [best, bd] = [v, d];
              }
              if (best !== stratum[i] - 1) elsewhere += 1;
            }
            return { niches: N, sizes: size, recorded: a.niches.cells, worst, elsewhere };
          })()`) : null;
          check('viewer', 'niches by remote control: each niche\'s make-up the mean of its cells\' windows by the suite\'s own search of the 10 nearest labeled cells, every cell nearest its own niche\'s center', `${niches?.message ?? niches?.error}; ${JSON.stringify(nichesOwn)}`,
            niches?.ok && nichesOwn && nichesOwn.niches === 5 && JSON.stringify(nichesOwn.sizes) === JSON.stringify(nichesOwn.recorded) && nichesOwn.worst < 1e-9 && nichesOwn.elsewhere === 0, 'the same make-up, each cell in its nearest niche');
          const filled = niches?.ok ? await act('show_tissue', { dataset: 'xenium-lung-2fov', fill: 'niches' }) : null;
          const fillShown = filled?.ok ? await chrome.eval(`(async () => {
            for (let k = 0; k < 100 && !/^Niches/.test(window.situcraftViewer?.clusterFill?.()?.name ?? ''); k += 1) await new Promise((r) => setTimeout(r, 100));
            const f = window.situcraftViewer?.clusterFill?.();
            return f ? { name: f.name, counts: f.sets.map((_, k) => f.counts.get(k) ?? 0) } : null;
          })()`) : null;
          const oneNiche = filled?.ok ? await act('show_tissue', { dataset: 'xenium-lung-2fov', cells_of: { analysis: niches.data.id, niche: 2 } }) : null;
          const oneShown = oneNiche?.ok ? await chrome.eval('window.situcraftViewer.focus()') : null;
          check('viewer', 'niches on the tissue: the cells filled by their niche, as many in each as the analysis holds, and one niche\'s cells shown', `${filled?.message ?? filled?.error}; ${JSON.stringify(fillShown)}; ${oneNiche?.message ?? oneNiche?.error}; ${JSON.stringify(oneShown)}`,
            filled?.ok && nichesOwn && JSON.stringify(fillShown?.counts) === JSON.stringify(nichesOwn.recorded) && oneShown?.cells === nichesOwn.recorded[1], 'the niches\' cells');

          // Spatially variable genes (wave 6, slice 6): each gene's local Moran's I sums to S0 times
          // its I, S0 the suite's own from the graph (with row-standardized weights, the cells with
          // a neighbor); every classed cell's local I of its class's sign; a gene's cells high among
          // high shown on the tissue as many as its classes hold.
          const moran = graph?.ok ? await act('spatial_analysis', { dataset: 'xenium-lung-2fov', segmentation: 'instrument', question: 'moran', graph: graph.data.id, permutations: 49, local: 3, local_permutations: 49 }) : null;
          const moranOwn = moran?.ok ? await chrome.eval(`(async () => {
            const a = await (await fetch('/api/artifacts/${moran.data.id}/files/analysis.json')).json();
            const lb = await (await fetch('/api/analyses/${moran.data.id}/local')).arrayBuffer();
            const v = new DataView(lb);
            const n = v.getUint32(0, true), T = v.getUint32(4, true);
            const gb = await (await fetch('/api/graphs/${graph?.data?.id}/edges')).arrayBuffer();
            const gn = new DataView(gb).getUint32(0, true);
            const offsets = new Uint32Array(gb, 12 + 12 * gn, gn + 1);
            let S0 = 0;
            for (let i = 0; i < gn; i += 1) if (offsets[i + 1] > offsets[i]) S0 += 1;
            const out = { genes: [], S0, recordedS0: a.moran.s0, wrongSign: 0, high: [] };
            for (let k = 0; k < T; k += 1) {
              const g = v.getUint32(8 + 4 * k, true);
              const I = new Float32Array(lb, 8 + 4 * T + 4 * n * k, n);
              const cls = new Uint8Array(lb, 8 + 4 * T + 8 * n * T + n * k, n);
              let sum = 0;
              let high = 0;
              for (let i = 0; i < n; i += 1) {
                sum += I[i];
                if ((cls[i] === 1 || cls[i] === 2) && !(I[i] > 0) || (cls[i] === 3 || cls[i] === 4) && !(I[i] < 0)) out.wrongSign += 1;
                if (cls[i] === 1) high += 1;
              }
              out.genes.push({ gene: a.moran.genes[g], relative: Math.abs(sum / (S0 * a.moran.i[g]) - 1) });
              out.high.push(high);
            }
            return out;
          })()`) : null;
          check('viewer', 'Moran\'s I by remote control: each leading gene\'s local Moran\'s I sums to S0 times its I (S0 the suite\'s own from the graph), every classed cell\'s local I of its class\'s sign', `${moran?.message ?? moran?.error}; ${JSON.stringify(moranOwn)}`,
            moran?.ok && moranOwn && moranOwn.genes.length === 3 && Math.abs(moranOwn.S0 - moranOwn.recordedS0) < 1e-6 && moranOwn.genes.every((x) => x.relative < 1e-4) && moranOwn.wrongSign === 0, 'S0 × I, and each class its sign');
          const hot = moranOwn ? await act('show_tissue', { dataset: 'xenium-lung-2fov', cells_of: { analysis: moran.data.id, gene: moranOwn.genes[0].gene, class: 'high' } }) : null;
          const hotShown = hot?.ok ? await chrome.eval('window.situcraftViewer.focus()') : null;
          check('viewer', 'a gene\'s cells high among high shown on the tissue, as many as its local classes hold', `${hot?.message ?? hot?.error}; ${JSON.stringify(hotShown)}`,
            hot?.ok && hotShown?.cells === moranOwn.high[0] && moranOwn.high[0] > 0, `${moranOwn?.high?.[0]} cells`);

          // Analyses across segmentations (wave 6, slice 7): the enrichment made again on the 5 µm
          // expansion in one step — its label set made again from the same clustering and marker
          // set, its graph of the same definition over the expansion's cells — and recorded as made
          // from it through the two segmentations' comparison; each number read together the
          // suite's own from the two analyses' files, their rank correlation its own; and the cells
          // behind the largest change its own, from the comparison's pairs and the two analyses'
          // labels, shown on the tissue.
          const enrichmentId = graph?.ok ? await chrome.eval(`window.situcraft.store.project.artifacts.find((a) => a.kind === 'spatial' && a.question === 'enrichment' && a.segmentation === ${JSON.stringify(graph.data.segmentation.id)} && a.state === 'complete')?.id ?? null`) : null;
          const remade = enrichmentId ? await act('remake_analysis', { dataset: 'xenium-lung-2fov', analysis: enrichmentId, segmentation: 'Nucleus expansion, 5 µm' }) : null;
          const remadeOwn = remade?.ok ? await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { originOf } = await import('/lib/impact.js');
            const ids = [${JSON.stringify(enrichmentId)}, ${JSON.stringify(remade.data.id)}];
            const [a, b] = await Promise.all(ids.map(async (id) => (await fetch('/api/artifacts/' + id + '/files/analysis.json')).json()));
            const art = (id) => app.store.project.artifacts.find((x) => x.id === id);
            const rec = art(ids[1]), cmp = art(rec.comparison), la = art(a.labels.id), lb = art(b.labels.id), gb = art(b.graph.id);
            const ratios = (x) => {
              const K = x.labels.names.length, out = new Map();
              for (let i = 0; i < K; i += 1) for (let j = i; j < K; j += 1) {
                const e = Number(x.enrichment.expected[i * K + j] ?? NaN);
                out.set('pair:' + [x.labels.names[i], x.labels.names[j]].sort().join('|'), e > 0 ? Number(x.enrichment.count[i * K + j]) / e : NaN);
              }
              return out;
            };
            const ra = ratios(a), rb = ratios(b);
            const keys = [...ra.keys()].filter((k) => rb.has(k));
            const both = keys.filter((k) => Number.isFinite(ra.get(k)) && Number.isFinite(rb.get(k)));
            const rank = (v) => {
              const idx = v.map((_, i) => i).sort((p, q) => v[p] - v[q]);
              const out = new Array(v.length);
              for (let i = 0; i < idx.length;) {
                let j = i;
                while (j + 1 < idx.length && v[idx[j + 1]] === v[idx[i]]) j += 1;
                for (let k = i; k <= j; k += 1) out[idx[k]] = (i + j) / 2;
                i = j + 1;
              }
              return out;
            };
            const x = rank(both.map((k) => ra.get(k))), y = rank(both.map((k) => rb.get(k)));
            const m = (both.length - 1) / 2;
            let sxy = 0, sxx = 0, syy = 0;
            x.forEach((v, i) => { sxy += (v - m) * (y[i] - m); sxx += (v - m) * (v - m); syy += (y[i] - m) * (y[i] - m); });
            const largest = Math.max(...both.map((k) => Math.abs(Math.log2(rb.get(k) / ra.get(k)))));
            const changes = ${JSON.stringify(remade.data.changes)};
            const wrong = changes.filter((r, i) => !(Math.abs(r.a / ra.get(r.key) - 1) < 1e-12 && Math.abs(r.b / rb.get(r.key) - 1) < 1e-12 && Math.abs(r.change - Math.log2(rb.get(r.key) / ra.get(r.key))) < 1e-9 && (i === 0 || Math.abs(r.change) <= Math.abs(changes[i - 1].change)))).map((r) => r.key);
            // The cells behind the largest change, as the page shows them.
            let tr = null;
            for (let k = 0; k < 300 && !tr; k += 1) {
              tr = document.querySelector('.sp-remake .sp-changes tbody tr');
              if (!tr) await new Promise((r) => setTimeout(r, 100));
            }
            tr?.click();
            let panel = null;
            for (let k = 0; k < 600 && !panel; k += 1) {
              await new Promise((r) => setTimeout(r, 100));
              panel = document.querySelector('.sp-behind [data-action="behind-a"]') ? document.querySelector('.sp-behind p')?.textContent : null;
            }
            const shown = /another label or none: ([0-9,]+) under .+, ([0-9,]+) under /.exec(panel ?? '');
            return {
              linked: rec.remakeOf === ids[0] && rec.comparison === ${JSON.stringify(remade.data.comparison)} && [cmp?.a, cmp?.b].sort().join() === [a.segmentation?.id ?? art(ids[0]).segmentation, b.segmentation?.id ?? rec.segmentation].sort().join(),
              labels: lb?.segmentation === rec.segmentation && la?.segmentation === art(ids[0]).segmentation && originOf(la) === originOf(lb) && a.labels.level === b.labels.level && JSON.stringify(a.labels.names) === JSON.stringify(b.labels.names),
              graph: gb?.segmentation === rec.segmentation && JSON.stringify(a.graph.parameters) === JSON.stringify(b.graph.parameters) && a.graph.id !== b.graph.id,
              numbers: keys.length, rho: sxy / Math.sqrt(sxx * syy), largest, wrong, row: tr?.dataset.key ?? null,
              behind: shown ? [Number(shown[1].replaceAll(',', '')), Number(shown[2].replaceAll(',', ''))] : panel,
            };
          })()`) : null;
          const behindA = remadeOwn?.behind?.length ? await chrome.eval(`(async () => {
            document.querySelector('.sp-behind [data-action="behind-a"]').click();
            for (let k = 0; k < 100 && !window.situcraftViewer?.focus(); k += 1) await new Promise((r) => setTimeout(r, 100));
            return window.situcraftViewer?.focus() ?? null;
          })()`) : null;
          // The suite's own: the comparison's pairs, each cell's label in its analysis.
          const pairsShown = remadeOwn ? await act('show_comparison', { dataset: 'xenium-lung-2fov', comparison: remade.data.comparison }) : null;
          const behindOwn = pairsShown?.ok ? await chrome.eval(`(async () => {
            const app = window.situcraft;
            const w = app.compareView.current();
            const ids = [${JSON.stringify(enrichmentId)}, ${JSON.stringify(remade.data.id)}];
            const cmp = app.store.project.artifacts.find((x) => x.id === ${JSON.stringify(remade.data.comparison)});
            const recA = app.store.project.artifacts.find((x) => x.id === ids[0]);
            const aFirst = cmp.a === recA.segmentation;
            // Each cell's label by name from its label set's files (the analyses leave out cells a
            // review excluded): '' for a cell the set covers without a label at the level, null for
            // one it leaves out (not analyzed), which is not compared.
            const sides = await Promise.all(ids.map(async (id) => {
              const x = await (await fetch('/api/artifacts/' + id + '/files/analysis.json')).json();
              const set = await (await fetch('/api/labels/' + encodeURIComponent(x.labels.id))).json();
              const lc = await (await fetch('/api/labels/' + encodeURIComponent(x.labels.id) + '/cells')).arrayBuffer();
              const [, n, levels] = new Uint32Array(lc, 0, 4);
              const level = Math.min(x.labels.level, levels - 1);
              return { names: set.description.sets.map((t) => t.name), cells: new Uint32Array(lc, 16, n), label: new Int32Array(lc, 16 + 8 * n + 4 * n * level, n) };
            }));
            const [sa, sb] = aFirst ? sides : [sides[1], sides[0]];
            const byRow = (s, n) => {
              const out = new Array(n).fill(null);
              s.cells.forEach((r, i) => { out[r] = s.label[i] >= 0 ? s.names[s.label[i]] : ''; });
              return out;
            };
            const nameA = byRow(sa, w.prepared.idsA.length), nameB = byRow(sb, w.prepared.idsB.length);
            const want = new Set(${JSON.stringify(remade.data.changes[0]?.labels ?? [])});
            let countA = 0, countB = 0, other = 0;
            for (const g of w.groups) {
              if (g.a.length !== 1 || g.b.length !== 1) {
                other += 1;
                continue;
              }
              const x = nameA[g.a[0]], y = nameB[g.b[0]];
              if (x === null || y === null || x === y) continue;
              if (want.has(x)) countA += 1;
              if (want.has(y)) countB += 1;
            }
            return aFirst ? { counts: [countA, countB], other } : { counts: [countB, countA], other };
          })()`) : null;
          check('viewer', 'an analysis made again on another segmentation by remote control: the enrichment on the 5 µm expansion, its labels made again from the same marker set and its graph of the same definition, recorded as made from it through the comparison; each number read together the suite\'s own, the rank correlation its own', `${remade?.message ?? remade?.error}; ${JSON.stringify(remadeOwn && { ...remadeOwn, wrong: remadeOwn.wrong.slice(0, 4) })}`,
            remade?.ok && remadeOwn?.linked && remadeOwn.labels && remadeOwn.graph && remade.data.numbers === remadeOwn.numbers && remade.data.onlyA === 0 && remade.data.onlyB === 0 && remadeOwn.wrong.length === 0 && Math.abs(remade.data.rankCorrelation - remadeOwn.rho) < 1e-9 && Math.abs(Math.abs(remade.data.changes[0].change) - remadeOwn.largest) < 1e-9 && remadeOwn.row === remade.data.changes[0].key,
            `${remadeOwn?.numbers} numbers, rank correlation ${remadeOwn?.rho?.toFixed?.(3)}`);
          check('viewer', 'the cells behind the largest change: those of its labels whose counterparts, through the comparison, carry another label or none, under each segmentation the suite\'s own, and shown on the tissue', `${JSON.stringify(remadeOwn?.behind)}; shown ${JSON.stringify(behindA)}; own ${JSON.stringify(behindOwn)}`,
            Array.isArray(remadeOwn?.behind) && behindOwn && behindOwn.other === 0 && remadeOwn.behind[0] === behindOwn.counts[0] && remadeOwn.behind[1] === behindOwn.counts[1] && behindA?.cells === behindOwn.counts[0] && behindOwn.counts[0] > 0,
            `${behindOwn?.counts?.join(' and ')} cells`);
        }

        // The Lung project as 0.2.0 to 0.6.0 saved it (validation/projects/<version>-lung.situcraft.json;
        // from 0.7.0 a release's fixture is a cohort's, below), each reopened, and every artifact
        // SituCraft made that it records made again by this build with the same files' checksums (a
        // comparison by its differences: its comparison.json records the jobs' times; a clustering,
        // a map, a label set from markers, a graph and a spatial analysis, whose records keep no
        // checksums, by their summaries, which hold their numbers exactly; those of a drawn region
        // left out).
        const fixtureOf = (v) => join(root, 'validation', 'projects', `${v}-lung.situcraft.json`);
        for (const v of ['0.2.0', '0.3.0', '0.4.0', '0.5.0', '0.6.0']) {
          if (!existsSync(fixtureOf(v))) continue;
          const text = readFileSync(fixtureOf(v), 'utf8');
          const fixture = JSON.parse(text);
          // An import of SituCraft's own export is not made again by another version: the export's
          // files name the SituCraft that wrote them, so their checksums, and the import's id, differ.
          // Labels imported or from a reference read a file this run wrote, and the instrument's
          // clusters and map are the bundle's.
          const cellTypes = (a) => (a.kind === 'clustering' && !a.summary?.source) || (a.kind === 'embedding' && !a.summary?.source) || (a.kind === 'labels' && !a.source && !a.reference);
          // A graph or analysis of a drawn region is not made again: each run draws its regions anew,
          // each with an id of its own, which names the analyses of it and is in the graphs' records.
          const regionIds = (fixture.regions ?? []).map((r) => r.id);
          const ofRegion = (a) => regionIds.some((id) => JSON.stringify(a).includes(id));
          const spatial = (a) => (a.kind === 'graph' || a.kind === 'spatial') && a.state === 'complete' && !ofRegion(a);
          const made = fixture.artifacts.filter((a) => (a.kind === 'segmentation' && a.source !== 'instrument' && a.method?.name !== 'SituCraft export') || a.kind === 'comparison' || cellTypes(a) || spatial(a));
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
            if (cellTypes(a) || spatial(a)) {
              if (JSON.stringify(m.summary) !== JSON.stringify(a.summary)) differing.push(`${a.name}: another summary`);
              continue;
            }
            const sums = new Map((m.files ?? []).map((f) => [f.path, f.sha256]));
            for (const f of a.files) if (sums.get(f.path) !== f.sha256) differing.push(`${a.name}: ${f.path}`);
          }
          const kinds = [...new Set(made.map((a) => a.kind))].join(', ');
          check('viewer', `${v} Lung project: every artifact SituCraft made is made again with the same checksums`, `${made.length} artifacts (${kinds})${differing.length ? `; ${differing.slice(0, 4).join('; ')}` : ', all the same'}`, made.length >= 5 && differing.length === 0 && (v < '0.4.0' || made.filter(cellTypes).length >= 5) && (v < '0.6.0' || (made.filter((a) => a.kind === 'graph').length >= 5 && made.filter((a) => a.kind === 'spatial').length >= 6)), 'all the same');
          const reopened = await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { promotedSegmentation, exportsOf } = await import('/lib/segmentations.js');
            const { samplesOf } = await import('/lib/samples.js');
            const wait = async (f) => { for (let k = 0; k < 300; k += 1) { const v = f(); if (v) return v; await new Promise((r) => setTimeout(r, 100)); } return null; };
            const doc = ${JSON.stringify(text)};
            const saved = JSON.parse(doc);
            await app.library.saveProject(saved.id, doc);
            await app.openProject(saved.id);
            const dataset = app.store.project.datasets[0];
            const before = (await wait(() => { const l = app.bundles.link(dataset.id); return l && l.state !== 'checking' ? l : null; }))?.state;
            const link = await app.bundles.locate(dataset.id, ${JSON.stringify(join(dataRoot, 'xenium-lung-2fov'))});
            const p = app.store.project;
            return { before, after: link?.state, segmentations: p.artifacts.filter((a) => a.kind === 'segmentation').length, comparisons: p.artifacts.filter((a) => a.kind === 'comparison').length, decisions: p.decisions.length, exports: exportsOf(p, dataset.id).length, default: promotedSegmentation(p, dataset.id)?.record.id, saved: promotedSegmentation(saved, saved.datasets[0].id)?.record.id, regions: JSON.stringify(p.regions ?? []), reviews: JSON.stringify(p.reviews ?? []), samples: samplesOf(p).map((s) => (s.region ? 'region ' + s.name : s.name)).join(), datasetNames: p.datasets.map((d) => d.name).join() };
          })()`);
          // From 0.5.0, its regions and reviews as they were saved.
          const kept = v < '0.5.0' || (reopened.regions === JSON.stringify(fixture.regions ?? []) && reopened.reviews === JSON.stringify(fixture.reviews ?? []) && (fixture.regions ?? []).length >= 2 && (fixture.reviews ?? []).length >= 3);
          check('viewer', `${v} Lung project: it opens and finds its moved bundle; its segmentations, comparisons, decisions, exports${v < '0.5.0' ? '' : ', regions and reviews'} are there, and its default; each dataset a sample`, JSON.stringify({ ...reopened, regions: (fixture.regions ?? []).length, reviews: (fixture.reviews ?? []).length }), reopened.samples === reopened.datasetNames && reopened.before === 'missing' && reopened.after === 'ok' && reopened.segmentations === fixture.artifacts.filter((a) => a.kind === 'segmentation').length && reopened.comparisons >= 2 && reopened.decisions === fixture.decisions.length && reopened.exports >= 1 && reopened.default && reopened.default === reopened.saved && kept, 'missing, then ok; all there');
        }

        // A worked example started in the page (here the bundles are the project's datasets, not
        // examples), run step by step to its end.
        const runWorked = async (bundle, name) => {
          await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { guideFor } = await import('/lib/tours.js');
            const { startTour } = await import('/ui/tour.js');
            const dataset = app.store.project.datasets.find((d) => d.name === ${JSON.stringify(bundle)});
            for (let k = 0; k < 600 && !app.store.project.artifacts.some((a) => a.kind === 'segmentation' && a.dataset === dataset.id); k += 1) await new Promise((r) => setTimeout(r, 100));
            startTour(app, { tour: guideFor(${JSON.stringify(bundle)}, ${JSON.stringify(name)}), dataset: dataset.id });
            return true;
          })()`);
          const out = [];
          for (let k = 0; k < 16; k += 1) {
            const x = await act('tour', { run: true });
            out.push(x);
            if (!x.ok || x.data?.finished) break;
          }
          return out;
        };
        const stepsRun = (steps) => steps.map((x) => (x.ok ? (x.data.finished ? 'finished' : `step ${x.data.step}`) : x.message)).join('; ');
        const finished = (steps) => steps.every((x) => x.ok) && steps.at(-1)?.data?.finished === true;
        const stepOf = (steps, n) => steps.find((x) => x.ok && x.data.step === n)?.data;
        const near = (v, want, tol) => Number.isFinite(v) && Math.abs(v - want) <= tol;
        // An enrichment's numbers as the cards state them: each pair's ratio (found over expected),
        // z and adjusted p by names; the pairs that differ from chance; and a pair's cells with a
        // neighbor of the other, and the edges between them, from the graph's edges.
        const ENRICHMENT_OF = `async (id, pair) => {
          const a = await (await fetch('/api/artifacts/' + id + '/files/analysis.json')).json();
          const names = a.labels.names, K = names.length, e = a.enrichment;
          const at = (x, y) => { const i = names.indexOf(x), j = names.indexOf(y); return Math.min(i, j) * K + Math.max(i, j); };
          const ratio = (x, y) => e.count[at(x, y)] / e.expected[at(x, y)];
          let pairs = 0, significant = 0;
          for (let x = 0; x < K; x += 1) for (let y = x; y < K; y += 1) { pairs += 1; if (e.padj[x * K + y] < 0.05) significant += 1; }
          const out = { labels: K, pairs, significant, ratio, z: (x, y) => e.z[at(x, y)], cells: (x) => e.cells[names.indexOf(x)], graph: a.graph.id, labelsId: a.labels.id };
          if (pair) {
            const cb = await (await fetch('/api/analyses/' + id + '/cells')).arrayBuffer();
            const n = new DataView(cb).getUint32(0, true);
            const label = new Int32Array(cb, 4 + 4 * n, n);
            const gb = await (await fetch('/api/graphs/' + a.graph.id + '/edges')).arrayBuffer();
            const gn = new DataView(gb).getUint32(0, true), m = new DataView(gb).getUint32(4, true);
            const off = new Uint32Array(gb, 12 + 12 * gn, gn + 1), nb = new Uint32Array(gb, 16 + 16 * gn, m);
            const A = names.indexOf(pair[0]), B = names.indexOf(pair[1]);
            const touches = new Uint8Array(gn);
            let edges = 0;
            for (let i = 0; i < gn; i += 1) {
              for (let k = off[i]; k < off[i + 1]; k += 1) {
                const j = nb[k];
                if ((label[i] === A && label[j] === B) || (label[i] === B && label[j] === A)) {
                  touches[i] = 1;
                  if (j > i) edges += 1;
                }
              }
            }
            out.pair = { a: 0, b: 0, edges };
            for (let i = 0; i < gn; i += 1) if (touches[i]) out.pair[label[i] === A ? 'a' : 'b'] += 1;
          }
          return out;
        }`;

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

          // The spatial worked examples on the Breast data (wave 6, slice 10), each number their
          // cards state checked. Which cell types are neighbors: the classes' and the lineages'
          // enrichment over the Delaunay graph, and each pair's cells lit on the tissue (their
          // clustering and labels made above, with the same defaults and set).
          const ns = await runWorked('xenium-breast-2fov', 'neighbors-breast');
          const classesId = stepOf(ns, 6)?.spatial?.analysis;
          const lineagesId = stepOf(ns, 9)?.spatial?.analysis;
          const bn = classesId && lineagesId ? await chrome.eval(`(async () => {
            const of = ${ENRICHMENT_OF};
            const c = await of(${JSON.stringify(classesId)}, ['Epithelial', 'Immune']);
            const l = await of(${JSON.stringify(lineagesId)}, ['T cell', 'Myeloid']);
            const g = (await (await fetch('/api/artifacts/' + c.graph)).json())?.summary;
            const set = await (await fetch('/api/labels/' + c.labelsId)).json();
            const d = set.description;
            const r2 = (v) => Math.round(100 * v) / 100;
            return {
              shares: d.cells.labeled.map((n) => Math.round((100 * n) / d.cells.analyzed)),
              graph: { cells: g?.cells, edges: g?.edges, median: g && Math.round(10 * g.medianLength) / 10 },
              classes: { significant: c.significant, pairs: c.pairs, endothelial: r2(c.ratio('Endothelial', 'Endothelial')), stromal: r2(c.ratio('Stromal', 'Stromal')), immune: r2(c.ratio('Immune', 'Immune')), epiStromal: r2(c.ratio('Epithelial', 'Stromal')), epiImmune: r2(c.ratio('Epithelial', 'Immune')), pair: c.pair },
              lineages: { labels: l.labels, pairs: l.pairs, significant: l.significant, tMyeloid: r2(l.ratio('T cell', 'Myeloid')), z: Math.round(l.z('T cell', 'Myeloid')), tT: r2(l.ratio('T cell', 'T cell')), luminalT: r2(l.ratio('Luminal', 'T cell')), pair: l.pair },
            };
          })()`) : null;
          const nf7 = stepOf(ns, 7)?.focus, nf10 = stepOf(ns, 10)?.focus;
          check('viewer', 'the worked example Which cell types are neighbors: the Breast classes\' and lineages\' enrichment over the Delaunay graph, and each pair\'s cells on the tissue, as its card states', `${stepsRun(ns)}; ${JSON.stringify(bn)}; shown ${nf7?.cells} and ${nf10?.cells}`,
            finished(ns) && bn && JSON.stringify(bn.shares) === '[81,49,6]' && bn.graph.cells === 7275 && bn.graph.edges === 21678 && bn.graph.median === 11.4
            && bn.classes.pairs === 15 && bn.classes.significant === 9 && bn.classes.endothelial === 12.45 && bn.classes.stromal === 4.54 && bn.classes.immune === 3.9 && bn.classes.epiStromal === 0.24 && bn.classes.epiImmune === 0.32
            && bn.classes.pair.a === 651 && bn.classes.pair.b === 299 && bn.classes.pair.edges === 823 && nf7?.cells === 950
            && bn.lineages.labels === 16 && bn.lineages.pairs === 136 && bn.lineages.significant === 54 && bn.lineages.tMyeloid === 4.44 && bn.lineages.z === 22 && bn.lineages.tT === 8.59 && bn.lineages.luminalT === 0.12
            && bn.lineages.pair.a === 74 && bn.lineages.pair.b === 127 && bn.lineages.pair.edges === 164 && nf10?.cells === 201,
            '81%, 49% and 6% labeled; 21,678 edges among 7,275 cells (median 11.4 µm); 9 of 15 class pairs, endothelial 12.45, stromal 4.54, immune 3.90 with themselves, epithelial–stromal 0.24, –immune 0.32 (651 and 299 cells, 823 edges); 54 of 136 lineage pairs, T cell–myeloid 4.44 (z 22; 74 and 127 cells, 164 edges), T–T 8.59, luminal–T 0.12');
          await act('tour', { end: true });

          // Immune cells and a tumor's edge: the region drawn, and each class's density across its
          // edge, in its core, at its margin and around it.
          const es = await runWorked('xenium-breast-2fov', 'tumor-edge');
          const edgeId = stepOf(es, 6)?.spatial?.analysis;
          const be = edgeId ? await chrome.eval(`(async () => {
            const a = await (await fetch('/api/artifacts/${edgeId}/files/analysis.json')).json();
            const b = a.boundary, names = a.labels.names, K = names.length;
            const E = names.indexOf('Epithelial'), I = names.indexOf('Immune');
            const tumor = window.situcraftViewer.regions.list().find((r) => r.name === 'Tumor');
            const r2 = (v) => Math.round(100 * v) / 100;
            return {
              region: tumor ? window.situcraftViewer.regions.rowsIn(tumor.id).length : null,
              epithelial: Math.round(10 * b.ratio[3 * E]) / 10,
              immune: { core: Math.round(b.zoneDensity[I]), margin: Math.round(b.zoneDensity[K + I]), around: Math.round(b.zoneDensity[2 * K + I]), ratio: r2(b.ratio[3 * I]), lo: r2(b.ratioLo[3 * I]), hi: r2(b.ratioHi[3 * I]), atCore: b.zoneCount[I], atMargin: b.zoneCount[K + I] },
            };
          })()`) : null;
          const ef7 = stepOf(es, 7)?.focus, ef8 = stepOf(es, 8)?.focus;
          check('viewer', 'the worked example Immune cells and a tumor\'s edge: the region drawn on the stains, the epithelial cells filling it and the immune cells kept out, and those at its margin and in its core on the tissue, as its card states', `${stepsRun(es)}; ${JSON.stringify(be)}; shown ${ef7?.cells} and ${ef8?.cells}`,
            finished(es) && be && be.region === 5730 && be.epithelial === 4.8 && be.immune.core === 420 && be.immune.margin === 765 && be.immune.around === 1174 && be.immune.ratio === 0.36 && be.immune.lo === 0.28 && be.immune.hi === 0.45
            && be.immune.atMargin === 76 && be.immune.atCore === 230 && ef7?.cells === 76 && ef8?.cells === 230,
            '5,730 cells in the tumor; epithelial cells 4.8 times as dense in its core; immune cells 420 per mm² in the core, 765 at the margin, 1,174 around (0.36, 0.28 to 0.45); 76 at the margin and 230 in the core shown');
          await act('tour', { end: true });
        }

        // The spatial worked examples on the Lung data (wave 6, slice 10), in a project of their own:
        // the suite's Lung project has cells a review excluded, which change the cards' numbers.
        {
          await act('new_project', { name: 'Lung spatial worked examples' });
          await act('open_paths', { paths: [join(dataRoot, 'xenium-lung-2fov')], add: true });
          // Genes in a pattern: every gene's Moran's I over the Delaunay graph, and GJA5's cells
          // high among high on the tissue.
          const ms = await runWorked('xenium-lung-2fov', 'moran-genes');
          const moranId = stepOf(ms, 4)?.spatial?.analysis;
          const lm = moranId ? await chrome.eval(`(async () => {
            const { readLocal } = await import('/lib/spatial.js');
            const a = await (await fetch('/api/artifacts/${moranId}/files/analysis.json')).json();
            const r = a.moran;
            const g = (await (await fetch('/api/artifacts/' + a.graph.id)).json()).summary;
            const order = r.genes.map((_, i) => i).sort((x, y) => r.i[y] - r.i[x]);
            const local = readLocal(await (await fetch('/api/analyses/${moranId}/local')).arrayBuffer());
            const G = r.genes.indexOf('GJA5');
            const classes = local.classes[local.genes.indexOf(G)];
            const of = (cls) => classes.reduce((n, c) => n + (c === cls ? 1 : 0), 0);
            return { cells: g.cells, edges: g.edges, genes: r.genes.length, patterned: r.padj.filter((p) => p < 0.05).length, top: order.slice(0, 5).map((i) => r.genes[i]).join(' '), i: order.slice(0, 3).map((i) => Math.round(1000 * r.i[i]) / 1000), detected: Math.round((1000 * r.detected[G]) / r.cells) / 10, highHigh: of(1), lowHigh: of(4) };
          })()`) : null;
          const mf = stepOf(ms, 5)?.focus;
          check('viewer', 'the worked example Genes in a pattern: Moran\'s I of every Lung gene over the Delaunay graph, GJA5\'s strongest, and its cells high among high on the tissue, as its card states', `${stepsRun(ms)}; ${JSON.stringify(lm)}; shown ${mf?.cells}`,
            finished(ms) && lm && lm.cells === 11898 && lm.edges === 35472 && lm.genes === 289 && lm.patterned === 162 && lm.top === 'GJA5 IGFBP3 CXCL13 PLA2G7 LTBP2' && JSON.stringify(lm.i) === '[0.436,0.411,0.313]' && lm.detected === 1.2 && lm.highHigh === 73 && lm.lowHigh === 286 && mf?.cells === 73,
            '35,472 edges among 11,898 cells; 162 of 289 genes; GJA5 0.436, IGFBP3 0.411, CXCL13 0.313, then PLA2G7 and LTBP2; GJA5 in 1.2% of the cells, 73 high among high and 286 low among high; 73 shown');
          await act('tour', { end: true });

          // An answer under another segmentation: the classes' enrichment, made again on a 3 µm
          // nucleus expansion, and the two read together.
          const rs = await runWorked('xenium-lung-2fov', 'remake-enrichment');
          const rsAt = stepOf(rs, 8)?.spatial;
          const lr = rsAt?.analysis && rsAt?.again ? await chrome.eval(`(async () => {
            const { analysisNumbers, compareNumbers, rankCorrelation } = await import('/lib/spatial.js');
            const p = window.situcraft.store.project;
            const ds = p.datasets.find((x) => x.name === 'xenium-lung-2fov');
            const of = ${ENRICHMENT_OF};
            const e = await of(${JSON.stringify(rsAt.analysis)});
            const read = async (id) => (await fetch('/api/artifacts/' + id + '/files/analysis.json')).json();
            const a = await read(${JSON.stringify(rsAt.analysis)}), b = await read(${JSON.stringify(rsAt.again)});
            const cmp = compareNumbers(analysisNumbers(a), analysisNumbers(b));
            const top = cmp.rows[0];
            const immune = (x) => x.enrichment.cells[x.labels.names.indexOf('Immune')];
            const clustering = p.artifacts.find((x) => x.kind === 'clustering' && x.dataset === ds.id)?.summary;
            const set = await (await fetch('/api/labels/' + e.labelsId)).json();
            const expansion = p.artifacts.find((x) => x.kind === 'segmentation' && x.dataset === ds.id && x.parameters?.distance_um === 3)?.summary?.agreement;
            // The cells behind the largest change, found as the step shows it.
            let behind = null;
            for (let k = 0; k < 200 && !/counterparts/.test(behind ?? ''); k += 1) {
              behind = document.querySelector('.sp-behind p')?.innerText ?? null;
              if (!/counterparts/.test(behind ?? '')) await new Promise((r) => setTimeout(r, 100));
            }
            const r2 = (v) => Math.round(100 * v) / 100;
            const d = set.description;
            return {
              clusters: clustering?.clusters, clustered: clustering?.cells, labeled: Math.round((100 * d.cells.labeled[0]) / d.cells.analyzed),
              significant: e.significant, pairs: e.pairs, stromal: r2(e.ratio('Stromal', 'Stromal')), epiEndothelial: r2(e.ratio('Epithelial', 'Endothelial')), epiStromal: r2(e.ratio('Epithelial', 'Stromal')),
              pixels: expansion && Math.round(10000 * expansion.pixels.agreement) / 100, transcripts: expansion && Math.round(10000 * expansion.transcripts.agreement) / 100,
              rho: Math.round(1000 * rankCorrelation(cmp.rows)) / 1000, numbers: cmp.rows.length, top: { key: top.key, a: r2(top.a), b: r2(top.b) }, immune: [immune(a), immune(b)],
              behind: (behind?.match(/: ([\\d,]+) under .*?, ([\\d,]+) under/) ?? []).slice(1).join(' '),
            };
          })()`) : null;
          check('viewer', 'the worked example An answer under another segmentation: the Lung classes\' enrichment made again on a 3 µm nucleus expansion, the two read together, and the cells behind the largest change, as its card states', `${stepsRun(rs)}; ${JSON.stringify(lr)}`,
            finished(rs) && lr && lr.clusters === 29 && lr.clustered === 11820 && lr.labeled === 47 && lr.pairs === 10 && lr.significant === 7 && lr.stromal === 3.99 && lr.epiEndothelial === 0.78 && lr.epiStromal === 0.79
            && lr.pixels === 90 && lr.transcripts === 95.45 && lr.numbers === 10 && lr.rho === 0.976 && lr.top.key === 'pair:Immune|Immune' && lr.top.a === 1.44 && lr.top.b === 1.94 && JSON.stringify(lr.immune) === '[249,227]' && lr.behind === '44 22',
            '29 clusters of 11,820 cells, 47% labeled; 7 of 10 pairs, stromal 3.99 with themselves, epithelial–endothelial 0.78, –stromal 0.79; the expansion 90.00% of pixels and 95.45% of transcripts alike; rank correlation 0.976; immune–immune 1.44 to 1.94 (249 and 227 immune cells; 44 and 22 behind it)');
          await act('tour', { end: true });

          // Figures of results (wave 8, slice 2): the Lung's distribution of transcripts, its
          // cells' map, its classes' enrichment and its genes in a pattern, one figure, written as
          // SVG and read by the suite: the histogram's counts the suite's own binning of the cells
          // (40 bins from the least to the 99.5th percentile), each bar's height its count; the map
          // a point a cell it maps and its clusters named; the heat map a square for each pair with
          // its ratio's color and a dot for each pair whose adjusted p is below 0.05; the bars the
          // genes in a pattern, strongest first; no text below 6 pt.
          {
            await act('show_map', { dataset: 'xenium-lung-2fov' });
            const ids = await chrome.eval(`(() => { const p = window.situcraft.store.project; const ds = p.datasets.find((x) => x.name === 'xenium-lung-2fov'); const sp = p.artifacts.filter((a) => a.kind === 'spatial' && a.dataset === ds.id && a.state === 'complete'); return { dataset: ds.id, enrichment: sp.find((a) => a.question === 'enrichment')?.id, moran: sp.find((a) => a.question === 'moran')?.id, map: p.artifacts.filter((a) => a.kind === 'embedding' && a.dataset === ds.id).at(-1)?.id }; })()`);
            const made = await act('figure', { name: 'Results of the Lung', size: 'double', columns: 2, height_mm: 160, panels: [{ kind: 'qc', dataset: ids.dataset, metric: 'transcripts' }, { kind: 'map', map: ids.map, color: 'clusters' }, { kind: 'spatial', analysis: ids.enrichment }, { kind: 'spatial', analysis: ids.moran }] });
            const svgPath = join(work, 'results.svg');
            const jsonPath = join(work, 'results.json');
            const w1 = await act('write_figure', { name: 'Results of the Lung', path: svgPath, format: 'svg', time: '2026-10-19T12:00:00Z', overwrite: true });
            const w2 = await act('write_figure', { name: 'Results of the Lung', path: jsonPath, format: 'json', time: '2026-10-19T12:00:00Z', overwrite: true });
            const svg = existsSync(svgPath) ? readFileSync(svgPath, 'utf8') : '';
            const manifest = existsSync(jsonPath) ? JSON.parse(readFileSync(jsonPath, 'utf8')) : null;
            // The suite's own histogram of the cells' transcripts.
            const values = await chrome.eval(`(async () => {
              const app = window.situcraft;
              const ds = app.store.project.datasets.find((x) => x.name === 'xenium-lung-2fov');
              const item = app.bundles.link(ds.id)?.item ?? app.bundles.itemByPath(ds.path);
              const { readCells } = await import('/lib/viewer-data.js');
              const cells = readCells(await fetch(item.url + '/cells').then((r) => r.arrayBuffer()));
              return Array.from(cells.transcripts);
            })()`);
            const sorted = values.filter(Number.isFinite).sort((a, b) => a - b);
            const lo = sorted[0];
            const hi = Math.max(sorted[Math.floor(0.995 * (sorted.length - 1))], lo + 1);
            const edges = hi - lo + 1 <= 40 ? Array.from({ length: hi - lo + 2 }, (_, k) => lo + k - 0.5) : Array.from({ length: 41 }, (_, k) => lo + ((hi - lo) * k) / 40);
            const own = new Array(edges.length - 1).fill(0);
            for (const v of values) {
              if (!(v >= edges[0] && v <= edges.at(-1))) continue;
              let k = edges.findIndex((e, i) => i < edges.length - 1 && v >= e && v < edges[i + 1]);
              if (k < 0) k = edges.length - 2;
              own[k] += 1;
            }
            const qcPart = figurePanelSvg(svg, 'A');
            const bars = [...qcPart.matchAll(/<rect x="([\d.]+)" y="([\d.]+)" width="([\d.]+)" height="([\d.]+)" fill="#5b4ce6"\/>/g)].map((m) => Number(m[4]));
            const nonzero = own.filter((c) => c > 0);
            const top = Math.max(...own);
            const heightsOff = bars.length === nonzero.length ? bars.filter((h, i) => Math.abs(h / Math.max(...bars) - nonzero[i] / top) > 0.01).length : -1;
            // The map: a point each cell mapped, its clusters named.
            const mapInfo = await chrome.eval(`(async () => { const d = await window.situcraft.bundles.mapData(${JSON.stringify(ids.map)}); const c = await window.situcraft.bundles.clusteringData(window.situcraft.store.project.artifacts.find((a) => a.id === ${JSON.stringify(ids.map)}).clustering); const present = new Set(); for (let i = 0; i < d.n; i += 1) if (c.cluster[d.cells[i]] >= 0) present.add(c.cluster[d.cells[i]]); return { n: d.n, clusters: present.size }; })()`);
            const mapPart = figurePanelSvg(svg, 'B');
            const mapPoints = (mapPart.match(/<rect [^>]*width="(0\.9|1\.4|0\.6)" height="(0\.9|1\.4|0\.6)"/g) ?? []).length;
            const mapNames = [...mapPart.matchAll(/<text[^>]*text-anchor="middle"[^>]*>(\d+)<\/text>/g)].length;
            // The heat map against the analysis's numbers.
            const en = await (await fetch(`${url}/api/artifacts/${encodeURIComponent(ids.enrichment)}/files/analysis.json`)).json();
            const K = en.labels.names.length;
            const wantColors = [];
            let wantDots = 0;
            for (let i = 0; i < K; i += 1) {
              for (let j = 0; j < K; j += 1) {
                const q = i * K + j;
                const r = en.enrichment.expected[q] > 0 ? en.enrichment.count[q] / en.enrichment.expected[q] : NaN;
                if (Number.isNaN(r)) continue;
                wantColors.push(divergingColor(r === 0 ? -Infinity : dlog(r) / 0.6931471805599453));
                if ((en.enrichment.padj[q] ?? NaN) < 0.05) wantDots += 1;
              }
            }
            const heatPart = figurePanelSvg(svg, 'C');
            const squares = [...heatPart.matchAll(/<rect x="[\d.]+" y="[\d.]+" width="([\d.]+)" height="([\d.]+)" fill="(rgb\([^)]+\))"\/>/g)].filter((m) => m[1] === m[2]).map((m) => m[3]);
            const heatDots = (heatPart.match(/<circle [^>]*fill="#171b26"/g) ?? []).length;
            const colorsOff = squares.length === wantColors.length ? squares.filter((c, i) => c !== wantColors[i]).length : -1;
            // The bars: the genes in a pattern, strongest first.
            const mo = await (await fetch(`${url}/api/artifacts/${encodeURIComponent(ids.moran)}/files/analysis.json`)).json();
            const pattern = [];
            for (let g = 0; g < mo.moran.genes.length; g += 1) if ((mo.moran.padj[g] ?? NaN) < 0.05 && Number.isFinite(mo.moran.i[g]) && mo.moran.i[g] > mo.moran.expected) pattern.push(g);
            pattern.sort((a, b) => mo.moran.i[b] - mo.moran.i[a]);
            const moranPart = figurePanelSvg(svg, 'D');
            const barNames = [...moranPart.matchAll(/<text[^>]*text-anchor="end"[^>]*>([A-Za-z0-9-]+)<\/text>/g)].map((m) => m[1]).filter((t) => !/^[\d.]+$/.test(t));
            const sizes = [...svg.matchAll(/font-size="([\d.]+)"/g)].map((m) => Number(m[1]));
            const facts = manifest?.panels?.map((p) => p.facts) ?? [];
            check('viewer', 'a figure of results: the Lung\'s distribution of transcripts the suite\'s own binning, bar by bar; its map a point each cell mapped, its clusters named; its enrichment\'s heat map each pair\'s ratio in its color and a dot where it differs from chance; its genes in a pattern strongest first; no text below 6 pt', `${made.ok ? made.message.slice(0, 160) : made.message}; ${w1.ok && w2.ok ? 'written' : `${w1.message} ${w2.message}`}; QC ${bars.length} bars (${nonzero.length} the suite's), ${heightsOff} heights off, counts ${JSON.stringify(facts[0]?.counts) === JSON.stringify(own) ? 'the suite\'s' : 'other'}; map ${mapPoints} points of ${mapInfo.n}, ${mapNames} names of ${mapInfo.clusters}; heat map ${squares.length} squares of ${wantColors.length}, ${colorsOff} colors off, ${heatDots} dots of ${wantDots}; bars ${barNames.join(' ')} against ${pattern.slice(0, barNames.length).map((g) => mo.moran.genes[g]).join(' ')}; text ${Math.min(...sizes)} pt or more`,
              made.ok && w1.ok && w2.ok && bars.length === nonzero.length && heightsOff === 0 && JSON.stringify(facts[0]?.counts) === JSON.stringify(own)
              && mapPoints === mapInfo.n && mapNames === mapInfo.clusters && squares.length === wantColors.length && colorsOff === 0 && heatDots === wantDots
              && barNames.length >= 5 && barNames.join() === pattern.slice(0, barNames.length).map((g) => mo.moran.genes[g]).join() && Math.min(...sizes) >= 6,
              'every bar, point, square, dot and gene the suite\'s; 6 pt or more');
          }

          // What made a result (wave 8, slice 3): the record of the Lung classes' enrichment, each
          // step from the bundle to it after the steps it was made from, with its method, the
          // clustering's seed its parameters' own, and the publications each follows (the bundle
          // Xenium's, the clustering scanpy's and Leiden's, the labels' the panel's starter atlas);
          // written as Markdown, JSON and BibTeX by export, the same record; shown in Reports.
          {
            const ids = await chrome.eval(`(() => { const p = window.situcraft.store.project; const ds = p.datasets.find((x) => x.name === 'xenium-lung-2fov'); return { dataset: ds.id, enrichment: p.artifacts.filter((a) => a.kind === 'spatial' && a.dataset === ds.id && a.state === 'complete' && a.question === 'enrichment').at(-1)?.id }; })()`);
            const time = '2026-10-19T12:00:00Z';
            const asked = await act('record', { result: ids.enrichment, format: 'markdown', time });
            const rec = asked.data;
            const paths = { md: join(work, 'record.md'), json: join(work, 'record.json'), bib: join(work, 'record.bib') };
            const wrote = {};
            for (const [ext, path] of Object.entries(paths)) wrote[ext] = await act('export', { what: 'record', result: ids.enrichment, path, overwrite: true });
            const read = (ext) => (existsSync(paths[ext]) ? readFileSync(paths[ext], 'utf8') : '');
            const fromJSON = JSON.parse(read('json') || 'null');
            const f = recordFacts(rec, read('md'), read('bib'));
            const step = (kind) => rec?.steps.find((x) => x.kind === kind);
            const clustering = step('clustering');
            const shown = await chrome.eval(`(() => ({ record: document.querySelector('.record')?.dataset.record ?? null, steps: document.querySelectorAll('.record-step').length, refs: document.querySelectorAll('.record-refs li').length }))()`);
            const undated = (text) => String(text ?? '').replace(/ on \d{4}-\d{2}-\d{2}:/, ':');
            const sameSteps = fromJSON && JSON.stringify(fromJSON.steps) === JSON.stringify(rec?.steps) && JSON.stringify(fromJSON.references) === JSON.stringify(rec?.references);
            const want = ['janesick2023', 'situcraft', 'wolf2018', 'traag2019', 'tirosh2016', 'travaglini2020', 'schapiro2017', 'palla2022'];
            check('viewer', 'what made a result: the Lung enrichment\'s record, each step after what made it, from the bundle through its segmentation, clustering, marker set, labels and graph; the clustering\'s seed its parameters\'; the publications each follows, numbered; Markdown, JSON and BibTeX the same record; shown in Reports', `${asked.ok ? asked.message.slice(0, 160) : asked.message}; ${Object.entries(wrote).map(([k, w]) => `${k} ${w.ok ? 'written' : w.message}`).join(', ')}; ${f.kinds.join(' > ')}; ${f.ordered ? 'in order' : 'out of order'}; seed ${clustering?.seeds} (parameters ${clustering?.parameters?.seed}); ${rec?.references.length} references (${want.filter((k) => !f.keys.includes(k)).join(', ') || 'all expected'}), Markdown ${f.mdRefs}, BibTeX ${f.bibEntries.length}; JSON ${sameSteps ? 'the same' : 'other'}; shown ${JSON.stringify(shown)}`,
              asked.ok && Object.values(wrote).every((w) => w.ok) && f.ordered && f.numbered && f.kinds[0] === 'dataset' && f.kinds.at(-1) === 'spatial' && ['segmentation', 'clustering', 'marker-set', 'labels', 'graph'].every((k) => f.kinds.includes(k))
              && clustering?.seeds.length === 1 && clustering.seeds[0] === clustering.parameters.seed && want.every((k) => f.keys.includes(k)) && f.mdRefs === rec.references.length && f.bibEntries.length === rec.references.length && f.bibEntries.includes('wolf2018')
              && undated(read('md')) === undated(asked.data.text) && sameSteps && shown.record === ids.enrichment && shown.steps > 0 && shown.refs === rec.references.length,
              'in order, from the bundle; the seed recorded; Xenium, scanpy, Leiden, the starter atlas and the analysis cited; the same record in each format; shown');
          }

          // The methods text (wave 8, slice 4): the Lung enrichment's methods, written from its
          // record, each number the records' own (the clusters and their cells, the share labeled
          // at the class level, the graph's edges, the pairs found among those asked), the seed
          // said, the references numbered as first cited; written as Markdown by export.
          {
            const result = (await chrome.eval(`(() => { const p = window.situcraft.store.project; const ds = p.datasets.find((x) => x.name === 'xenium-lung-2fov'); return p.artifacts.filter((a) => a.kind === 'spatial' && a.dataset === ds.id && a.state === 'complete' && a.question === 'enrichment').at(-1)?.id; })()`));
            const rec = (await act('record', { result })).data;
            const ofKind = (kind) => rec?.steps.find((x) => x.kind === kind)?.id ?? '';
            const facts = await chrome.eval(`(() => { const p = window.situcraft.store.project; const a = (id) => p.artifacts.find((x) => x.id === id) ?? {}; const clu = a(${JSON.stringify(ofKind('clustering'))}); const lab = a(${JSON.stringify(ofKind('labels'))}); const g = a(${JSON.stringify(ofKind('graph'))}); const e = a(${JSON.stringify(result)}); return { clusters: clu.summary?.clusters, cells: clu.summary?.cells, seed: clu.parameters?.seed, labeled: lab.summary?.labeled?.[0], labCells: lab.summary?.cells, level: lab.summary?.levels?.[0], edges: g.summary?.edges, significant: e.summary?.significant, count: e.summary?.labels?.count }; })()`);
            const asked = await act('methods', { result });
            const text = asked.data?.text ?? '';
            const fmt = (n) => Number(n).toLocaleString('en-US');
            const want = [
              `This gave ${fmt(facts.clusters)} clusters of ${fmt(facts.cells)} cells.`,
              `${((100 * facts.labeled) / facts.labCells).toFixed(1)}% at the ${String(facts.level).toLowerCase()} level`,
              `${fmt(facts.edges)} edges`,
              `${fmt(facts.significant)} of ${fmt((facts.count * (facts.count + 1)) / 2)} pairs differed from chance`,
              `seed ${facts.seed}`,
            ];
            const missing = want.filter((w) => !text.includes(w));
            const mf = methodsFacts(text, asked.data?.references);
            const mdPath = join(work, 'methods.md');
            const wrote = await act('export', { what: 'methods', result, path: mdPath, overwrite: true });
            const md = existsSync(mdPath) ? readFileSync(mdPath, 'utf8') : '';
            const inMd = (asked.data?.paragraphs ?? []).every((x) => md.includes(`## ${x.heading}\n\n${x.text}\n`));
            check('viewer', 'the methods of the Lung enrichment: written from its record by topic, each number the records\' own (clusters and cells, the share labeled, the graph\'s edges, the pairs found), the seed said, the references numbered as first cited; written as Markdown', `${asked.ok ? `${(asked.data.paragraphs ?? []).map((x) => x.topic).join(', ')}; ${asked.data.references.length} references` : asked.message}; ${missing.length ? `missing: ${missing.join(' | ')}` : 'every number the records\''}; ${mf.numbered ? 'numbered as cited' : 'not numbered as cited'}; Markdown ${wrote.ok ? (inMd && md.startsWith(`# ${asked.data.title}`) ? 'the same text' : 'other') : wrote.message}`,
              asked.ok && ['data', 'cells', 'spatial', 'software'].every((t) => asked.data.paragraphs.some((x) => x.topic === t)) && missing.length === 0 && mf.numbered && asked.data.references.some((r) => r.includes('10.1038/s41467-023-43458-x')) && asked.data.references.some((r) => r.includes('Travaglini')) && wrote.ok && inMd && md.startsWith(`# ${asked.data.title}`),
              'by topic; every number the records\'; numbered as cited; the Markdown the same text');
          }

          // Certificates (wave 8, slice 5): the Lung enrichment certified, read by the suite's own
          // zip reader: its fingerprint the suite's own over its canonical JSON; each step the
          // record's; every file a step of SituCraft's wrote the SHA-256 of that file in the
          // program's store, every bundle file the SHA-256 of the file in the bundle; every entry
          // as the archive's list says; no path of this computer anywhere; the same bytes again.
          {
            const result = (await chrome.eval(`(() => { const p = window.situcraft.store.project; const ds = p.datasets.find((x) => x.name === 'xenium-lung-2fov'); return p.artifacts.filter((a) => a.kind === 'spatial' && a.dataset === ds.id && a.state === 'complete' && a.question === 'enrichment').at(-1)?.id; })()`));
            const paths = [join(work, 'certificate.zip'), join(work, 'certificate-again.zip')];
            const wrote = [];
            for (const path of paths) wrote.push(await act('export', { what: 'certificate', result, path, overwrite: true }));
            const zip = existsSync(paths[0]) ? readFileSync(paths[0]) : Buffer.alloc(0);
            let entries = new Map();
            try {
              entries = unzipOwn(zip);
            } catch {
              entries = new Map();
            }
            const text = (name) => entries.get(name)?.toString('utf8') ?? '';
            const cert = JSON.parse(text('certificate.json') || 'null');
            const { fingerprint, ...rest } = cert ?? {};
            const own = createHash('sha256').update(canonicalOf(rest)).digest('hex');
            const recordSteps = ((await act('record', { result })).data?.steps ?? []).map((x) => x.id);
            // Every file SituCraft's steps wrote, against its store; every bundle file, against the bundle.
            const sha = (bytes) => createHash('sha256').update(bytes).digest('hex');
            let made = 0;
            const madeOff = [];
            for (const st of (cert?.steps ?? []).filter((x) => x.by === 'program')) {
              for (const f of st.files.filter((x) => x.location !== 'bundle' && x.location !== 'place')) {
                const response = await fetch(`${url}/api/artifacts/${encodeURIComponent(st.id)}/files/${f.path}`);
                const got = response.ok ? sha(Buffer.from(await response.arrayBuffer())) : `HTTP ${response.status}`;
                made += 1;
                if (got !== f.sha256) madeOff.push(`${st.id}/${f.path}`);
              }
            }
            const bundleDir = join(dataRoot, 'xenium-lung-2fov');
            const bundleFiles = (cert?.bundles?.[0]?.files ?? []).filter((f) => f.sha256);
            const bundleOff = bundleFiles.filter((f) => !existsSync(join(bundleDir, f.path)) || sha(readFileSync(join(bundleDir, f.path))) !== f.sha256).map((f) => f.path);
            const listed = JSON.parse(text('manifest.json') || '{"files":[]}').files;
            const listOff = listed.filter((f) => !entries.has(f.path) || sha(entries.get(f.path)) !== f.sha256).map((f) => f.path);
            const leaks = [...entries].filter(([, bytes]) => [dataRoot, work, homedir()].some((p) => bytes.includes(p))).map(([name]) => name);
            const again = existsSync(paths[1]) && zip.equals(readFileSync(paths[1]));
            check('viewer', 'a certificate of the Lung enrichment: its fingerprint the suite\'s own; each step the record\'s; every file a step wrote the SHA-256 of the program\'s, every bundle file the bundle\'s; every entry as listed; no path of this computer; the same bytes again', `${wrote.map((w) => (w.ok ? 'written' : w.message)).join(', ')}; ${entries.size} entries; fingerprint ${own === fingerprint ? 'the suite\'s' : 'other'} (${String(fingerprint).slice(0, 16)}); steps ${JSON.stringify((cert?.steps ?? []).map((x) => x.id)) === JSON.stringify(recordSteps) ? 'the record\'s' : 'other'}; ${made} files made, ${madeOff.length} off${madeOff.length ? ` (${madeOff.slice(0, 3).join(', ')})` : ''}; ${bundleFiles.length} bundle files, ${bundleOff.length} off; ${listed.length} listed, ${listOff.length} off; ${leaks.length ? `paths in ${leaks.join(', ')}` : 'no path'}; ${again ? 'the same bytes again' : 'other bytes again'}`,
              wrote.every((w) => w.ok) && cert?.format === 'situcraft-certificate' && own === fingerprint && JSON.stringify(cert.steps.map((x) => x.id)) === JSON.stringify(recordSteps) && made >= 5 && madeOff.length === 0
              && bundleFiles.length >= 5 && bundleOff.length === 0 && listed.length === entries.size - 1 && listOff.length === 0 && leaks.length === 0 && again && /^SituCraft certificate\n/.test(text('README.txt')) && text('README.txt').includes(fingerprint),
              'the suite\'s fingerprint; the record\'s steps; every SHA-256 the files\'; no path; the same bytes');
          }

          // situcraft verify (wave 8, slice 6): the Lung enrichment's certificate made again from its
          // bundle (found in the data folder by its files' SHA-256) in a SituCraft of its own:
          // verified (exit 0), every step made again; tampered with by the suite's own zip writer (the
          // graph's longest edge changed and everything made consistent again: its manifest, its
          // SHA-256, the fingerprint), found to differ first at the graph (exit 1); without its
          // bundle, not checkable (exit 2). And the Lung's results figure certified and verified: its
          // drawing drawn again in a headless page, the same.
          {
            const sha = (bytes) => createHash('sha256').update(bytes).digest('hex');
            const chromePath = findChrome();
            const reportOf = (path) => (existsSync(path) ? JSON.parse(readFileSync(path, 'utf8')) : null);
            const verifyRun = (zip, data, name) => {
              const report = join(work, `${name}.json`);
              const r = spawnSync(bin, ['verify', '--data', data, '--report', report, '--overwrite', ...(chromePath ? ['--chrome', chromePath] : []), zip], { encoding: 'utf8', timeout: 30 * 60e3 });
              return { status: r.status, out: `${r.stdout ?? ''}${r.stderr ?? ''}`, report: reportOf(report) };
            };
            const certPath = join(work, 'certificate.zip');
            const verified = verifyRun(certPath, dataRoot, 'verified');
            // Tampered with, consistently.
            const entries = unzipOwn(readFileSync(certPath));
            const cert = JSON.parse(entries.get('certificate.json').toString('utf8'));
            const graph = cert.steps.find((x) => x.kind === 'graph');
            const manifest = JSON.parse(entries.get(graph.manifest.path).toString('utf8'));
            manifest.lineage.job.spec.parameters.longest = 40;
            const manifestText = `${JSON.stringify(manifest, null, 1)}\n`;
            graph.manifest.sha256 = sha(manifestText);
            graph.job.spec = sha(canonicalOf(manifest.lineage.job.spec));
            delete cert.fingerprint;
            cert.fingerprint = sha(canonicalOf(cert));
            entries.set(graph.manifest.path, Buffer.from(manifestText));
            entries.set('certificate.json', Buffer.from(JSON.stringify(cert)));
            const tamperedPath = join(work, 'certificate-tampered.zip');
            writeFileSync(tamperedPath, zipOwn([...entries]));
            const tampered = verifyRun(tamperedPath, dataRoot, 'tampered');
            const empty = join(work, 'no-bundles');
            mkdirSync(empty, { recursive: true });
            const missing = verifyRun(certPath, empty, 'missing');
            // The results figure, drawn again in a headless page.
            const figureId = await chrome.eval(`window.situcraft.store.project.figures.find((f) => f.name === 'Results of the Lung')?.id ?? null`);
            const figurePath = join(work, 'certificate-figure.zip');
            const figureMade = await act('export', { what: 'certificate', result: figureId, path: figurePath, overwrite: true });
            const figure = verifyRun(figurePath, dataRoot, 'figure');
            const figureStep = figure.report?.steps?.find((x) => x.kind === 'figure');
            const steps = verified.report?.steps ?? [];
            const madeAgain = steps.filter((x) => x.by === 'program' && x.verdict === 'verified').length;
            check('viewer', 'situcraft verify: the Lung enrichment made again from its bundle, found by its files\' SHA-256, in a SituCraft of its own: verified (exit 0); tampered with, consistently, found to differ first at the graph (exit 1); without its bundle, not checkable (exit 2); the results figure drawn again in a headless page, the same', `verified: exit ${verified.status}, ${verified.report?.verdict}, ${madeAgain} steps made again of ${steps.filter((x) => x.by === 'program').length}; tampered: exit ${tampered.status}, ${tampered.report?.verdict}, first at ${tampered.report?.firstDiffers?.kind} (${tampered.report?.firstDiffers?.detail}); missing: exit ${missing.status}, ${missing.report?.verdict}; figure: ${figureMade.ok ? 'certified' : figureMade.message}, exit ${figure.status}, ${figure.report?.verdict}, ${figureStep?.verdict} (${figureStep?.detail})${verified.status !== 0 ? `; ${verified.out.slice(-600)}` : ''}`,
              verified.status === 0 && verified.report?.verdict === 'verified' && madeAgain >= 5 && steps.every((x) => ['verified', 'as certified'].includes(x.verdict))
              && tampered.status === 1 && tampered.report?.verdict === 'differs' && tampered.report?.firstDiffers?.kind === 'graph'
              && missing.status === 2 && missing.report?.verdict === 'not checkable' && missing.report?.bundles?.[0]?.status === 'missing'
              && figureMade.ok && figure.status === 0 && figureStep?.verdict === 'verified',
              'verified, 0; differs at the graph, 1; not checkable, 2; the figure drawn again the same');
            // Kept for CI to verify on the other processor (SITUCRAFT_CERTIFICATES: a folder).
            const keep = process.env.SITUCRAFT_CERTIFICATES;
            if (keep) {
              mkdirSync(keep, { recursive: true });
              copyFileSync(certPath, join(keep, 'lung-enrichment.zip'));
              if (existsSync(figurePath)) copyFileSync(figurePath, join(keep, 'lung-figure.zip'));
            }
          }

          // The report (wave 8, slice 7): the Lung's results figure and its enrichment in one HTML
          // file, opened from the disk in a browser of its own, as a reader opens it: it asks the
          // network for nothing, checks the certificate it carries and finds every number, the figure
          // and the methods the certificate's; every number it shows is, as the suite reads it, the
          // value at its place in certificate.json, written as the report writes it, and the
          // certificate's fingerprint the suite's own. A number's text edited, it says so and marks
          // it. And situcraft verify, given the report, makes its results again and checks it.
          {
            const ids = await chrome.eval(`(() => { const p = window.situcraft.store.project; const ds = p.datasets.find((x) => x.name === 'xenium-lung-2fov'); return { enrichment: p.artifacts.filter((a) => a.kind === 'spatial' && a.dataset === ds.id && a.state === 'complete' && a.question === 'enrichment').at(-1)?.id ?? null, figure: p.figures.find((f) => f.name === 'Results of the Lung')?.id ?? null }; })()`);
            const reportPath = join(work, 'report.html');
            const wrote = await act('export', { what: 'report', results: [ids.figure, ids.enrichment], title: 'The Lung\'s results', path: reportPath, overwrite: true });
            const html = existsSync(reportPath) ? readFileSync(reportPath, 'utf8') : '';
            // The suite's own reading of it.
            const carried = /<script type="application\/zip" id="situcraft-certificate">([^<]*)<\/script>/.exec(html);
            let entries = new Map();
            try {
              entries = carried ? unzipOwn(Buffer.from(carried[1], 'base64')) : new Map();
            } catch {
              entries = new Map();
            }
            const cert = JSON.parse(entries.get('certificate.json')?.toString('utf8') ?? 'null');
            const { fingerprint, ...rest } = cert ?? {};
            const own = createHash('sha256').update(canonicalOf(rest)).digest('hex');
            const unescape = (t) => t.replace(/&quot;/g, '"').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
            const at = (doc, pointer) => pointer.split('/').slice(1).map((x) => x.replace(/~1/g, '/').replace(/~0/g, '~')).reduce((v, k) => (v === undefined || v === null ? undefined : v[k]), doc);
            // As the report writes a number: whole numbers with commas, others to 4 significant digits.
            const written = (v) => (Number.isInteger(v) ? v.toLocaleString('en-US') : Math.abs(v) < 1e-3 || Math.abs(v) >= 1e6 ? v.toExponential(2) : String(Number(v.toPrecision(4))));
            const shown = [...html.matchAll(/data-pointer="([^"]*)" data-value="([^"]*)"[^>]*>([^<]*)</g)].map((m) => ({ pointer: unescape(m[1]), value: JSON.parse(unescape(m[2])), text: unescape(m[3]) }));
            const shownOff = shown.filter((n) => typeof at(cert, n.pointer) !== 'number' || at(cert, n.pointer) !== n.value || written(at(cert, n.pointer)) !== n.text).map((n) => n.pointer);
            const figureStep = cert?.steps?.find((x) => x.id === ids.figure);
            const svg = /<img data-figure="[^"]*"[^>]*? src="data:image\/svg\+xml;base64,([^"]*)"/.exec(html);
            const svgSame = Boolean(svg && figureStep?.drawing?.svg === createHash('sha256').update(Buffer.from(svg[1], 'base64')).digest('hex'));
            // Opened from the disk, in a browser of its own.
            const opened = async (path) => {
              const browser = await launch({ width: 1100, height: 900, args: (process.env.SITUCRAFT_CHROME_ARGS ?? '').split(' ').filter(Boolean) });
              const asked = [];
              try {
                await browser.send('Network.enable');
                browser.on('Network.requestWillBeSent', (e) => {
                  if (!/^(data|blob|file):/.test(e.request.url)) asked.push(e.request.url);
                });
                await browser.goto(pathToFileURL(path).href, 300);
                let state = null;
                for (let k = 0; k < 80 && !state; k += 1) {
                  state = await browser.eval('document.documentElement.dataset.checked ?? null');
                  if (!state) await sleep(250);
                }
                const seen = await browser.eval(`({ box: document.getElementById('report-check')?.textContent ?? '', numbers: document.querySelectorAll('[data-pointer]').length, differs: document.querySelectorAll('td.differs').length, figures: document.querySelectorAll('img[data-figure]').length, drawn: [...document.querySelectorAll('img[data-figure]')].every((img) => img.complete && img.naturalWidth > 0), paragraphs: document.querySelectorAll('#methods .methods-paragraph').length, link: (document.getElementById('certificate-link')?.href ?? '').startsWith('blob:') })`);
                return { state, asked, ...seen };
              } finally {
                await browser.close();
              }
            };
            const seen = await opened(reportPath);
            // The first number's text edited, its value left as it was.
            const first = /(data-value="[^"]*"[^>]*>)([^<]*)</.exec(html);
            const editedPath = join(work, 'report-edited.html');
            if (first) writeFileSync(editedPath, `${html.slice(0, first.index)}${first[1]}9${first[2]}<${html.slice(first.index + first[0].length)}`);
            const edited = first ? await opened(editedPath) : null;
            const verified = spawnSync(bin, ['verify', '--data', dataRoot, ...(findChrome() ? ['--chrome', findChrome()] : []), reportPath], { encoding: 'utf8', timeout: 30 * 60e3 });
            const verifyOut = `${verified.stdout ?? ''}${verified.stderr ?? ''}`;
            const carriedSaid = /The report carries it: its (\d+) numbers, 1 figure and its methods are the certificate's\./.exec(verifyOut);
            check('viewer', 'a report of the Lung\'s results figure and enrichment: one HTML file, opened from the disk, asking the network for nothing; it finds its certificate intact and every number, the figure and the methods the certificate\'s, each number as the suite reads it at its place in the certificate, the fingerprint the suite\'s own; a number\'s text edited, it says so; situcraft verify, given the report, makes its results again and checks it', `${wrote.ok ? `${wrote.data.numbers} numbers, ${wrote.data.figures} figure, ${Math.round(wrote.data.bytes / 1024)} KB` : wrote.message}; opened: ${seen.state} (${seen.box.slice(0, 120)}); ${seen.asked.length ? `asked ${seen.asked.slice(0, 2).join(', ')}` : 'nothing asked of the network'}; figure ${seen.drawn ? 'drawn' : 'not drawn'}, its SVG ${svgSame ? 'the certified one' : 'other'}; ${shown.length} numbers shown, ${shownOff.length} not the certificate's as the suite reads it; fingerprint ${own === fingerprint ? 'the suite\'s' : 'other'}; edited: ${edited?.state} (${edited?.differs} marked); verify: exit ${verified.status}${carriedSaid ? `, ${carriedSaid[1]} numbers checked` : `; ${verifyOut.slice(-400)}`}`,
              wrote.ok && seen.state === 'intact' && seen.asked.length === 0 && seen.numbers === shown.length && shown.length >= 10 && shownOff.length === 0 && seen.figures === 1 && seen.drawn && svgSame && seen.paragraphs >= 4 && seen.link && own === fingerprint
              && edited?.state === 'differs' && edited?.differs === 1 && verified.status === 0 && Number(carriedSaid?.[1]) === shown.length,
              'intact, offline; every number the certificate\'s; an edit found; verified');
            const keep = process.env.SITUCRAFT_CERTIFICATES;
            if (keep && existsSync(reportPath)) copyFileSync(reportPath, join(keep, 'lung-report.html'));
          }

          // The result package (wave 8, slice 8): the Lung's figure of a saved view, its results
          // figure and its enrichment, the samples' names left out, written as a folder and as a
          // zip (the same files). A review by a person with a note of their own is planted first.
          // The suite reads every file itself: the list's SHA-256 each file's; no path of this
          // computer, its user's name, the person, a note or the dataset's name in any text it holds
          // (its HTML, the SVG figures and the certificate inside it, the certificate's entries).
          // Opened from the disk in a browser of its own, asking the network for nothing: the
          // report intact, its views' thumbnails drawn; each view's reader its tiles, zoomed into
          // its finest level. situcraft verify, given the folder, makes its results again.
          {
            const note = 'Kept to myself: the coverslip cracked near the second FOV';
            await act('review', { dataset: 'xenium-lung-2fov', action: 'comment', cells: ['#7'], note, by: 'Ada Lovelace' });
            // A figure of a saved view of the tissue, whose view the package draws as tiles.
            await act('show_tissue', { dataset: 'xenium-lung-2fov', center: [750, 300], um_per_pixel: 0.4, genes: ['EPCAM', 'PLVAP', 'CD68'], fill: 'labels:lineage', opacity: 0.5, outlines: true, nuclei: false, transcripts: true });
            await act('save_view', { name: 'The Lung, packaged' });
            await act('figure', { name: 'The Lung\'s tissue', views: ['The Lung, packaged'], size: 'single', width_um: 300 });
            const ids = await chrome.eval(`(() => { const p = window.situcraft.store.project; const ds = p.datasets.find((x) => x.name === 'xenium-lung-2fov'); return { figure1: p.figures.find((f) => f.name === 'The Lung\\'s tissue')?.id ?? null, results: p.figures.find((f) => f.name === 'Results of the Lung')?.id ?? null, enrichment: p.artifacts.filter((a) => a.kind === 'spatial' && a.dataset === ds.id && a.state === 'complete' && a.question === 'enrichment').at(-1)?.id ?? null }; })()`);
            const folder = join(work, 'package');
            const zipPath = join(work, 'package.zip');
            const args = { what: 'package', results: [ids.figure1, ids.results, ids.enrichment], title: 'The Lung\'s results', names: true, overwrite: true };
            const wrote = await act('export', { ...args, path: folder });
            const zipped = await act('export', { ...args, path: zipPath });
            // Every file, read by the suite.
            const walk = (dir, base = '') => readdirSync(dir, { withFileTypes: true }).flatMap((e) => (e.isDirectory() ? walk(join(dir, e.name), `${base}${e.name}/`) : [`${base}${e.name}`]));
            const files = existsSync(folder) ? walk(folder).sort() : [];
            const bytes = new Map(files.map((f) => [f, readFileSync(join(folder, f))]));
            const sha = (b) => createHash('sha256').update(b).digest('hex');
            const listed = JSON.parse(bytes.get('manifest.json')?.toString('utf8') ?? '{"files":[]}');
            const listOff = listed.files.filter((f) => !bytes.has(f.path) || sha(bytes.get(f.path)) !== f.sha256).map((f) => f.path);
            const unlisted = files.filter((f) => f !== 'manifest.json' && !listed.files.some((x) => x.path === f));
            // The zip: the same files under its folder.
            let zipSame = false;
            try {
              const entries = unzipOwn(readFileSync(zipPath));
              zipSame = entries.size === files.length && files.every((f) => entries.get(`${zipped.data?.folder}/${f}`)?.equals(bytes.get(f)));
            } catch {
              zipSame = false;
            }
            // Every text it holds, and what it must not hold.
            const texts = [];
            const fromZip = (name, data) => {
              for (const [entry, b] of unzipOwn(data)) if (!/\.(jpg|png)$/.test(entry)) texts.push([`${name}: ${entry}`, b.toString('utf8')]);
            };
            for (const [f, b] of bytes) {
              if (f.endsWith('.zip')) fromZip(f, b);
              else if (!f.endsWith('.jpg')) {
                const text = b.toString('utf8');
                texts.push([f, text]);
                for (const m of text.matchAll(/data:image\/svg\+xml;base64,([A-Za-z0-9+/=]+)/g)) texts.push([`${f}: a figure`, Buffer.from(m[1], 'base64').toString('utf8')]);
                for (const m of text.matchAll(/id="situcraft-certificate">([^<]*)</g)) fromZip(`${f}: its certificate`, Buffer.from(m[1], 'base64'));
              }
            }
            const user = basename(homedir());
            const word = (w) => new RegExp(`(?<![A-Za-z0-9.-])${w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?![A-Za-z0-9-]|\\.[A-Za-z0-9])`);
            const forbidden = [['a path', (t) => t.includes(dataRoot) || t.includes(work) || t.includes(homedir())], ['the user', (t) => new RegExp(`[\\/]${user}[\\/]`).test(t)], ['a person', (t) => t.includes('Ada Lovelace')], ['a note', (t) => t.includes(note) || t.includes('Folded tissue at the edge')], ['the dataset\'s name', (t) => word('xenium-lung-2fov').test(t.replace(/[A-Za-z0-9+/=]{40,}/g, ' '))]];
            const leaks = texts.flatMap(([name, text]) => forbidden.filter(([, test]) => test(text)).map(([what]) => `${what} in ${name}`));
            const tiles = files.filter((f) => f.endsWith('.jpg'));
            const jpeg = tiles.every((f) => bytes.get(f)[0] === 0xff && bytes.get(f)[1] === 0xd8);
            // Opened from the disk, as its reader opens it.
            const opened = async (path, script) => {
              const browser = await launch({ width: 1200, height: 850, args: (process.env.SITUCRAFT_CHROME_ARGS ?? '').split(' ').filter(Boolean) });
              const asked = [];
              try {
                await browser.send('Network.enable');
                browser.on('Network.requestWillBeSent', (e) => {
                  if (!/^(data|blob|file):/.test(e.request.url)) asked.push(e.request.url);
                });
                await browser.goto(pathToFileURL(path).href, 300);
                return { asked, ...(await browser.eval(script)) };
              } finally {
                await browser.close();
              }
            };
            const report = existsSync(join(folder, 'index.html')) ? await opened(join(folder, 'index.html'), `(async () => {
              for (let k = 0; k < 80 && !document.documentElement.dataset.checked; k += 1) await new Promise((r) => setTimeout(r, 250));
              const thumbs = [...document.querySelectorAll('#views img')];
              for (let k = 0; k < 40 && !thumbs.every((i) => i.complete); k += 1) await new Promise((r) => setTimeout(r, 250));
              return { state: document.documentElement.dataset.checked, thumbs: thumbs.length, drawn: thumbs.filter((i) => i.naturalWidth === 512).length, links: [...document.querySelectorAll('#views a')].map((a) => a.getAttribute('href')) };
            })()`) : { asked: [], links: [] };
            const readers = [];
            for (const link of report.links ?? []) {
              readers.push(await opened(join(folder, link), `(async () => {
                const ready = async () => { for (let k = 0; k < 80 && document.documentElement.dataset.ready !== 'true'; k += 1) await new Promise((r) => setTimeout(r, 250)); };
                await ready();
                const first = { level: document.documentElement.dataset.level, shown: [...document.querySelectorAll('#tiles img')].filter((i) => i.style.display !== 'none' && i.naturalWidth === 512).length };
                const stage = document.getElementById('stage');
                stage.focus();
                for (let k = 0; k < 6; k += 1) stage.dispatchEvent(new KeyboardEvent('keydown', { key: '+', bubbles: true }));
                const shown = () => [...document.querySelectorAll('#tiles img')].filter((i) => i.style.display !== 'none');
                for (let k = 0; k < 40 && !shown().every((i) => i.complete); k += 1) await new Promise((r) => setTimeout(r, 250));
                return { first, level: document.documentElement.dataset.level, deepShown: shown().filter((i) => i.naturalWidth === 512).length, missing: Number(document.documentElement.dataset.missing ?? 0), legend: document.querySelectorAll('aside li').length };
              })()`));
            }
            const finestOf = (link) => {
              const view = listed.views?.find((v) => v.file === link);
              return view ? String(view.levels - 1) : null;
            };
            const verified = spawnSync(bin, ['verify', '--data', dataRoot, ...(findChrome() ? ['--chrome', findChrome()] : []), folder], { encoding: 'utf8', timeout: 30 * 60e3 });
            const verifyOut = `${verified.stdout ?? ''}${verified.stderr ?? ''}`;
            const asked = [...report.asked, ...readers.flatMap((r) => r.asked)];
            check('viewer', 'a result package of the Lung\'s figures and enrichment, the samples\' names left out, as a folder and a zip (the same files): its list each file\'s SHA-256; no path of this computer, its user, the person who reviewed, their notes or the dataset\'s name in any text it holds; opened from the disk asking the network for nothing, its report intact and its views drawn, each zoomed to its finest tiles; verified from its bundle', `${wrote.ok ? `${wrote.data.files} files, ${wrote.data.views} view${wrote.data.views === 1 ? '' : 's'} in ${wrote.data.tiles} tiles, ${wrote.data.leftOut.people} person, ${wrote.data.leftOut.notes} note and ${wrote.data.leftOut.names} name left out, ${wrote.data.checked.texts} texts searched by SituCraft` : wrote.message}; zip ${zipped.ok ? (zipSame ? 'the same files' : 'other files') : zipped.message}; ${files.length} files read, ${listOff.length} off the list${unlisted.length ? `, ${unlisted.length} not listed` : ''}; ${texts.length} texts read by the suite, ${leaks.length ? `found ${leaks.slice(0, 3).join(', ')}` : 'nothing left out found'}; ${tiles.length} tiles, ${jpeg ? 'JPEG' : 'not all JPEG'}; report ${report.state}, ${report.drawn}/${report.thumbs} thumbnails; readers ${readers.map((r, k) => `${r.first?.shown} tiles at level ${r.first?.level}, zoomed to level ${r.level} of ${finestOf(report.links[k])} (${r.deepShown} tiles, ${r.missing} missing, ${r.legend} legend items)`).join('; ')}; ${asked.length ? `asked ${asked.slice(0, 2).join(', ')}` : 'nothing asked of the network'}; verify: exit ${verified.status}${verified.status !== 0 ? ` ${verifyOut.slice(-400)}` : ''}`,
              wrote.ok && zipped.ok && zipSame && files.length === wrote.data.files && listOff.length === 0 && unlisted.length === 0 && texts.length >= 20 && leaks.length === 0 && tiles.length >= 4 && jpeg && wrote.data.leftOut.people >= 1 && wrote.data.leftOut.notes >= 1 && wrote.data.leftOut.names >= 1
              && report.state === 'intact' && report.thumbs >= 1 && report.drawn === report.thumbs && readers.length === report.thumbs && readers.every((r, k) => r.first?.shown >= 1 && r.level === finestOf(report.links[k]) && r.deepShown >= 1 && r.missing === 0 && r.legend >= 1) && asked.length === 0
              && verified.status === 0 && /The report carries it: its \d+ numbers, 2 figures and its methods are the certificate's\./.test(verifyOut),
              'the same files; nothing left out found; offline, intact, drawn; verified');
            const keep = process.env.SITUCRAFT_CERTIFICATES;
            if (keep && existsSync(zipPath)) copyFileSync(zipPath, join(keep, 'lung-package.zip'));
          }

          // The worked examples of wave 8 (slice 9), walked step by step in this project, each number
          // their cards state checked: A figure of results, and its methods (the figure's size, its
          // panels and what each shows, its methods' references, its record's steps), and Results
          // others can check (the figure certified and verified in SituCraft itself, its steps made
          // again; the report's numbers; the package's files, view, tiles, what it left out and the
          // texts searched).
          // A Lung worked example started in the page (the bundle is this run's, not a downloaded
          // example), then walked step by step; with steps, only its first ones.
          const walkLung = async (name, steps = 16) => {
            await chrome.eval(`(async () => {
              const app = window.situcraft;
              const { guideFor } = await import('/lib/tours.js');
              const { startTour } = await import('/ui/tour.js');
              const dataset = app.store.project.datasets.find((d) => d.name === 'xenium-lung-2fov');
              for (let k = 0; k < 600 && !app.store.project.artifacts.some((a) => a.kind === 'segmentation' && a.dataset === dataset.id); k += 1) await new Promise((r) => setTimeout(r, 100));
              startTour(app, { tour: guideFor('xenium-lung-2fov', ${JSON.stringify(name)}), dataset: dataset.id });
              return true;
            })()`);
            const out = [];
            for (let k = 0; k < steps; k += 1) {
              const x = await act('tour', { run: true });
              out.push(x);
              if (!x.ok || x.data?.finished) break;
            }
            return out;
          };
          {
            // In a project of the Lung alone, as a reader starts them.
            await act('new_project', { name: 'Lung worked examples of wave 8' });
            await act('open_paths', { paths: [join(dataRoot, 'xenium-lung-2fov')], add: true });
            const walk = (name) => walkLung(name);
            const walked = (steps) => steps.every((x) => x.ok) && steps.at(-1)?.data?.finished;
            const first = await walk('figure-and-methods');
            const fig = await act('figure', { name: 'The Lung\'s results' });
            const methods = await act('methods', { result: 'The Lung\'s results' });
            const record = await act('record', { result: 'The Lung\'s results' });
            check('viewer', 'the worked example A figure of results, and its methods: the figure two columns wide of the view of the tissue, the cells\' map, the classes\' enrichment and the genes in a pattern, each as its cards state; its methods\' references and its record\'s steps', `${first.map((x) => (x.ok ? `step ${x.data?.step ?? 'finished'}` : x.message)).join('; ')}; ${fig.ok ? `${fig.data.width_mm} × ${fig.data.height_mm} mm, ${fig.data.panels.map((p) => p.kind).join(', ')}${fig.data.notes.length ? `, notes: ${fig.data.notes.join(' ')}` : ''}` : fig.message}; methods ${methods.ok ? `${methods.data.references.length} references, ${methods.data.paragraphs.map((x) => x.topic).join(', ')}` : methods.message}; record ${record.ok ? `${record.data.steps.length} steps, ${record.data.references.length} references` : record.message}`,
              walked(first) && fig.ok && fig.data.width_mm === 174 && fig.data.height_mm === 157.3 && fig.data.panels.map((p) => p.kind).join() === 'tissue,map,spatial,spatial' && fig.data.notes.length === 0
              && /400 µm across: the DAPI stain; 2,298 cells outlined and filled by the label set/.test(fig.data.description) && /11,820 cells, colored by their clusters/.test(fig.data.description) && /\(7 pairs\)/.test(fig.data.description) && /The 15 genes of 289 whose Moran's I is highest among the 162 in a pattern/.test(fig.data.description)
              && methods.ok && methods.data.references.length === 23 && methods.data.paragraphs.some((x) => x.topic === 'figures') && record.ok && record.data.steps.length === 11 && record.data.references.length === 22,
              '174 × 157.3 mm; 2,298 cells, 11,820 mapped, 7 pairs, 15 of 162 genes; 23 references; 11 steps and 22 references');
            const second = await walk('results-checked');
            const made = await chrome.eval('window.situcraft.tour?.reports?.() ?? null');
            const v = made?.verification;
            check('viewer', 'the worked example Results others can check: the figure certified and verified in SituCraft itself (its steps made again from the bundle, the figure drawn again), its report\'s numbers and its package\'s files, view and tiles, the dataset\'s name left out, each as its cards state', `${second.map((x) => (x.ok ? `step ${x.data?.step ?? 'finished'}` : x.message)).join('; ')}; verification ${v ? `${v.verdict}, ${v.steps} steps from ${v.bundles} bundle, ${v.madeAgain} made again; ${v.stepVerdicts.map((x) => `${x.n} ${x.verdict}`).join(', ')}` : 'none'}; report ${made?.report ? `${made.report.numbers} numbers` : 'none'}; package ${made?.package ? `${made.package.files} files, ${made.package.views} view in ${made.package.tiles} tiles, ${made.package.leftOut.names} name left out, ${made.package.checked.texts} texts searched` : 'none'}`,
              walked(second) && v?.verdict === 'verified' && v.steps === 11 && v.bundles === 1 && v.madeAgain === 7 && v.stepVerdicts.every((x) => ['verified', 'as certified'].includes(x.verdict))
              && made.report?.numbers === 23 && made.report.figures === 1 && made.package?.files === 25 && made.package.views === 1 && made.package.tiles === 20 && made.package.leftOut.names === 1 && made.package.checked.texts === 35,
              'verified: 11 steps, 7 made again; 23 numbers; 25 files, 1 view in 20 tiles, 1 name left out, 35 texts');
          }

          // The Lung as 0.8.0 saves a figure of its results, and the figure's certificate
          // (validation/projects/0.8.0-lung.situcraft.json and 0.8.0-lung.certificate.zip): written
          // when asked (SITUCRAFT_WRITE_FIXTURE=0.8.0), a project of its own made by the worked
          // example Results others can check, its bundle's place made neutral. Then, as every later
          // version must: the certificate verified from the bundle by situcraft verify (every step
          // made again, the figure drawn again); the project reopened, its bundle found where this
          // run keeps it, and its figure drawn the same as the certificate records.
          {
            const projectFixture = join(root, 'validation', 'projects', '0.8.0-lung.situcraft.json');
            const certificateFixture = join(root, 'validation', 'projects', '0.8.0-lung.certificate.zip');
            const lungDir = join(dataRoot, 'xenium-lung-2fov');
            if (process.env.SITUCRAFT_WRITE_FIXTURE === '0.8.0') {
              await act('new_project', { name: 'The Lung, as SituCraft 0.8.0 saved it' });
              await act('open_paths', { paths: [lungDir], add: true });
              // Its first two steps: the question, and the figure made with all it rests on.
              await walkLung('results-checked', 2);
              await act('tour', { end: true });
              const doc = await chrome.eval(`(async () => (await import('/lib/project.js')).serializeProject(window.situcraft.store.project))()`);
              const neutral = JSON.parse(doc.split(lungDir).join('/data/xenium-lung-2fov').split(dataRoot).join('/data').split(work).join('/data/work'));
              writeFileSync(projectFixture, `${JSON.stringify({ ...neutral, id: 'p-situcraft-0-8-0-lung' }, null, 1)}\n`);
              await act('export', { what: 'certificate', result: 'The Lung\'s results', path: certificateFixture, overwrite: true });
            }
            if (existsSync(projectFixture) && existsSync(certificateFixture)) {
              const fixtureReport = join(work, 'fixture-0.8.0.json');
              const verified = spawnSync(bin, ['verify', '--data', dataRoot, '--report', fixtureReport, '--overwrite', ...(findChrome() ? ['--chrome', findChrome()] : []), certificateFixture], { encoding: 'utf8', timeout: 30 * 60e3 });
              const report = existsSync(fixtureReport) ? JSON.parse(readFileSync(fixtureReport, 'utf8')) : null;
              const certified = JSON.parse(unzipOwn(readFileSync(certificateFixture)).get('certificate.json').toString('utf8'));
              const figureStep = certified.steps.find((x) => x.kind === 'figure');
              const text = readFileSync(projectFixture, 'utf8');
              const reopened = await chrome.eval(`(async () => {
                const app = window.situcraft;
                const doc = ${JSON.stringify(text)};
                const saved = JSON.parse(doc);
                await app.library.saveProject(saved.id, doc);
                await app.openProject(saved.id);
                const d = app.store.project.datasets[0];
                const link = await app.bundles.locate(d.id, ${JSON.stringify(lungDir)});
                return { state: link?.state ?? null, figures: app.store.project.figures.map((f) => f.name), software: saved.software ?? null };
              })()`);
              const drawn = await act('write_figure', { name: 'The Lung\'s results', path: join(work, 'fixture-0.8.0-figure.json'), format: 'json', overwrite: true });
              check('viewer', '0.8.0 Lung project and its figure\'s certificate: the certificate verified from the bundle (its steps made again, the figure drawn again); the project reopened, its bundle found, its figure drawn the same as the certificate records', `verify: exit ${verified.status}, ${report?.verdict}, ${report?.steps?.length} steps (${(report?.steps ?? []).filter((x) => x.verdict === 'verified').length} verified)${verified.status !== 0 ? ` ${`${verified.stdout ?? ''}${verified.stderr ?? ''}`.slice(-400)}` : ''}; certified by ${certified.software.version}; reopened ${JSON.stringify(reopened)}; drawn ${drawn.ok ? (drawn.data.drawing === figureStep?.drawing?.drawing ? 'the same' : 'otherwise') : drawn.message}`,
                verified.status === 0 && report?.verdict === 'verified' && certified.software.version === '0.8.0' && reopened.state === 'ok' && reopened.figures.includes('The Lung\'s results') && drawn.ok && drawn.data.drawing === figureStep?.drawing?.drawing,
                'verified; reopened; the figure drawn the same');
            }
          }
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

        {
          // The post-Xenium worked examples (wave 5, slice 7), on the simulated example's H&E, run step
          // by step in the same project, each number their cards state checked: the H&E placed three
          // ways and each checked (and against the truth); two regions drawn on it, their cells and true
          // types, written in its pixels and read back; the review queue's first twenty decided by the
          // truth and a fold excluded.
          const card = (name) => (async () => {
            await act('tour', { example: 'simulated-standard', name });
            const out = [];
            for (let k = 0; k < 12; k += 1) {
              const x = await act('tour', { run: true });
              out.push(x);
              if (!x.ok || /finished/.test(x.message ?? '')) break;
            }
            return out;
          })();
          const close2 = (v, want) => Number.isFinite(v) && Math.abs(v - want) < 0.0051;
          const alignSteps = await card('align-he');
          const aligned = await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { truePlace } = await import('/lib/simulated-truth.js');
            const { placePoint } = await import('/lib/transforms.js');
            const p = app.store.project;
            const ds = p.datasets.find((x) => x.name === 'simulated-standard');
            const reg = (await (await fetch('/api/read-json?path=' + encodeURIComponent(ds.path + '/truth/post_xenium/registration.json'))).json()).images.find((x) => x.kind === 'he');
            const out = {};
            for (const t of p.artifacts.filter((a) => a.kind === 'transform' && a.dataset === ds.id)) {
              const check = p.artifacts.find((a) => a.kind === 'alignment-check' && a.transform === t.id)?.summary;
              const d = await app.bundles.transformDescription(t.id);
              const desc = { transform: d.transform, deformation: d.deformation ?? null };
              const errs = [];
              for (let v = 0; v < reg.height; v += 20) for (let u = 0; u < reg.width; u += 20) {
                const [tx, ty] = truePlace(reg, u + 0.5, v + 0.5);
                if (tx < 0 || ty < 0 || tx > 500 || ty > 500) continue;
                const [x, y] = placePoint(desc, u + 0.5, v + 0.5);
                errs.push(Math.hypot(x - tx, y - ty));
              }
              errs.sort((a, b) => a - b);
              out[t.summary.method] = { median: check?.median, p90: check?.p90, trueMedian: errs[Math.floor(errs.length / 2)], trueP90: errs[Math.floor(0.9 * errs.length)], fit: d.fit ? { rms: d.fit.rms, looRms: d.fit.looRms } : null };
            }
            return { versions: out, inUse: p.decisions.filter((x) => x.kind === 'use-transform').at(-1)?.artifact, auto: p.artifacts.find((a) => a.kind === 'transform' && a.summary?.method === 'automatic')?.id };
          })()`);
          const av = aligned.versions;
          const t2 = (x) => x && [x.median, x.p90, x.trueMedian, x.trueP90].map((v) => v?.toFixed(2)).join('/');
          check('viewer', 'the worked example Align an H&E three ways: by the matrix, six landmarks and automatically, each checked over the tissue and against the truth, as its card states', `${alignSteps.map((x) => x.ok ? `step ${x.data.step}` : x.message).join('; ')}; matrix ${t2(av.matrix)}, landmarks ${t2(av.landmarks)} (fit ${av.landmarks?.fit?.rms.toFixed(2)}, left out ${av.landmarks?.fit?.looRms.toFixed(2)}), automatic ${t2(av.automatic)} (median/p90 checked, then truly)`,
            alignSteps.every((x) => x.ok) && /finished/.test(alignSteps.at(-1).message ?? '') && aligned.inUse === aligned.auto
            && close2(av.matrix?.median, 0.63) && close2(av.matrix?.p90, 1.09) && close2(av.matrix?.trueMedian, 0.69) && close2(av.matrix?.trueP90, 1.12)
            && close2(av.landmarks?.median, 0.58) && close2(av.landmarks?.p90, 1.19) && close2(av.landmarks?.trueMedian, 0.72) && close2(av.landmarks?.trueP90, 1.22) && close2(av.landmarks?.fit?.rms, 0.57) && close2(av.landmarks?.fit?.looRms, 1.00)
            && close2(av.automatic?.median, 0.41) && close2(av.automatic?.p90, 0.62) && close2(av.automatic?.trueMedian, 0.47) && close2(av.automatic?.trueP90, 0.75),
            'matrix 0.63/1.09 checked, 0.69/1.12 truly; landmarks 0.58/1.19, 0.72/1.22 (fit 0.57, left out 1.00); automatic 0.41/0.62, 0.47/0.75; the automatic version in use');
          await act('tour', { end: true });

          // The H&E in a figure (wave 8, slice 2): a saved view of the simulated tissue showing its
          // H&E by the alignment in use (the automatic one), drawn in a figure; each pixel of the
          // stains' raster checked against the suite's own placing (the deformation's field
          // interpolated by the suite, the transform inverted, the image's pixels interpolated from
          // its tiles), and the Viewer's own pixels against the same placing within a few levels.
          {
            const he = await chrome.eval(`(async () => { const p = window.situcraft.store.project; const ds = p.datasets.find((x) => x.name === 'simulated-standard'); const im = p.artifacts.find((a) => a.kind === 'image' && a.dataset === ds.id && /H&E/.test(a.name)); const d = im ? await window.situcraft.bundles.imageDescription(im.id) : null; return im ? { id: im.id, name: im.name, levels: d.image.levels } : null; })()`);
            await act('show_tissue', { dataset: 'simulated-standard', center: [250, 250], um_per_pixel: 0.25, genes: [], transcripts: false, outlines: false, nuclei: false, fill: 'none', images: { [he?.name]: { visible: true, opacity: 1 } } });
            await act('save_view', { name: 'The H&E' });
            const made = await act('figure', { name: 'The H&E', size: 'single', panels: [{ kind: 'tissue', view: 'The H&E', width_um: 160 }] });
            const svgPath = join(work, 'he.svg');
            const jsonPath = join(work, 'he.json');
            const w1 = await act('write_figure', { name: 'The H&E', path: svgPath, format: 'svg', overwrite: true });
            const w2 = await act('write_figure', { name: 'The H&E', path: jsonPath, format: 'json', overwrite: true });
            const svg = existsSync(svgPath) ? readFileSync(svgPath, 'utf8') : '';
            const manifest = existsSync(jsonPath) ? JSON.parse(readFileSync(jsonPath, 'utf8')) : null;
            const facts = manifest?.panels?.[0]?.facts;
            const placed = facts?.raster?.images?.[0];
            const raster = embeddedImage(figurePanelSvg(svg, 'A'));
            // The suite's own placing of the image: the transform in use, its deformation's field.
            const tf = placed ? await (await fetch(`${url}/api/transforms/${encodeURIComponent(placed.transform)}`)).json() : null;
            const field = tf?.deformation ? readInverseField(await (await fetch(`${url}/api/transforms/${encodeURIComponent(placed.transform)}/inverse`)).arrayBuffer()) : null;
            const inv = tf ? invert(tf.transform) : null;
            const tiles = new Map();
            const pixelOf = async (level, x, y) => {
              const lv = he.levels[level];
              const tx = Math.floor(x / lv.tileWidth);
              const ty = Math.floor(y / lv.tileHeight);
              const key = `${level}/${tx}/${ty}`;
              if (!tiles.has(key)) {
                const r = await fetch(`${url}/api/images/${encodeURIComponent(he.id)}/tiles/${level}/0/${tx}/${ty}`);
                tiles.set(key, r.ok ? { w: Number(r.headers.get('X-Tile-Width')), b: new Uint8Array(await r.arrayBuffer()) } : null);
              }
              const t = tiles.get(key);
              const at = 3 * ((y - ty * lv.tileHeight) * t.w + (x - tx * lv.tileWidth));
              return [t.b[at], t.b[at + 1], t.b[at + 2]];
            };
            const placeColor = async (level, x, y) => {
              let px = x;
              let py = y;
              if (field) {
                const gx = Math.min(field.nx - 1, Math.max(0, (x - field.x0) / field.step));
                const gy = Math.min(field.ny - 1, Math.max(0, (y - field.y0) / field.step));
                const i0 = Math.min(field.nx - 2, Math.floor(gx));
                const j0 = Math.min(field.ny - 2, Math.floor(gy));
                const v = (i, j, k) => field.field[2 * (j * field.nx + i) + k];
                const fx = gx - i0;
                const fy = gy - j0;
                for (const k of [0, 1]) {
                  const d = (v(i0, j0, k) * (1 - fx) + v(i0 + 1, j0, k) * fx) * (1 - fy) + (v(i0, j0 + 1, k) * (1 - fx) + v(i0 + 1, j0 + 1, k) * fx) * fy;
                  if (k === 0) px += d;
                  else py += d;
                }
              }
              const lv = he.levels[level];
              const sx = he.levels[0].width / lv.width;
              const sy = he.levels[0].height / lv.height;
              const lx = (inv[0][0] * px + inv[0][1] * py + inv[0][2]) / sx - 0.5;
              const ly = (inv[1][0] * px + inv[1][1] * py + inv[1][2]) / sy - 0.5;
              const x0 = Math.max(0, Math.min(lv.width - 1, Math.floor(lx)));
              const y0 = Math.max(0, Math.min(lv.height - 1, Math.floor(ly)));
              const x1 = Math.min(lv.width - 1, x0 + 1);
              const y1 = Math.min(lv.height - 1, y0 + 1);
              const ax = Math.min(1, Math.max(0, lx - x0));
              const ay = Math.min(1, Math.max(0, ly - y0));
              const [p00, p10, p01, p11] = await Promise.all([pixelOf(level, x0, y0), pixelOf(level, x1, y0), pixelOf(level, x0, y1), pixelOf(level, x1, y1)]);
              return [0, 1, 2].map((k) => (p00[k] * (1 - ax) + p10[k] * ax) * (1 - ay) + (p01[k] * (1 - ax) + p11[k] * ax) * ay);
            };
            let rasterOff = -1;
            let probed = 0;
            if (raster?.rgba && placed && inv) {
              rasterOff = 0;
              const r = facts.region;
              for (let k = 0; k < 60; k += 1) {
                const px = Math.floor(((k * 37) % 97) / 97 * (raster.width - 20)) + 10;
                const py = Math.floor(((k * 53) % 89) / 89 * (raster.height - 20)) + 10;
                const want = await placeColor(placed.level, r.x0 + (px + 0.5) * ((r.x1 - r.x0) / raster.width), r.y0 + (py + 0.5) * ((r.y1 - r.y0) / raster.height));
                const i = 4 * (py * raster.width + px);
                if (want.some((c, j) => Math.abs(Math.round(c) - raster.rgba[i + j]) > 1)) rasterOff += 1;
                probed += 1;
              }
            }
            // The Viewer's own pixels of the same view, at its level.
            await act('open_view', { view: 'The H&E' });
            const probes = await chrome.eval(`(async () => {
              const v = window.situcraftViewer;
              await v.settled();
              const r = v.renderer;
              const [w, h] = r.size();
              const { screenToWorld } = await import('/lib/viewport.js');
              const { overlayLevel } = await import('/lib/overlays.js');
              const o = r.overlays[0];
              const level = overlayLevel(o.desc.image.levels, o.desc.transform, r.camera.scale);
              const kept = [r.selected, r.highlight];
              r.selected = null;
              r.highlight = null;
              const out = [];
              for (const [fx, fy] of [[0.2, 0.2], [0.35, 0.6], [0.5, 0.5], [0.62, 0.31], [0.71, 0.77], [0.8, 0.45], [0.27, 0.83], [0.9, 0.15], [0.45, 0.9]]) {
                const sx = Math.floor(w * fx), sy = Math.floor(h * fy);
                const [x, y] = screenToWorld(r.camera, w, h, sx + 0.5, sy + 0.5);
                out.push({ x, y, color: r.readColor(sx, sy).slice(0, 3) });
              }
              [r.selected, r.highlight] = kept;
              return { level, out };
            })()`);
            let viewerOff = 0;
            for (const p of probes.out) {
              const want = await placeColor(probes.level, p.x, p.y);
              if (want.some((c, j) => Math.abs(c - p.color[j]) > 4)) viewerOff += 1;
            }
            check('viewer', 'a figure of the H&E: placed by the alignment in use, its pixels the suite\'s own placing of the image through the deformation and the transform, and the Viewer\'s within a few levels', `${made.ok ? made.message.slice(0, 220) : made.message}; ${w1.ok && w2.ok ? 'written' : `${w1.message} ${w2.message}`}; ${placed ? `image at level ${placed.level}, ${placed.covered} pixels covered, by ${placed.transform}${field ? ' (deformed)' : ''}` : 'no image'}; ${rasterOff} of ${probed} raster pixels off; the Viewer at level ${probes.level}, ${viewerOff} of ${probes.out.length} probes off`,
              made.ok && w1.ok && w2.ok && placed && placed.covered === raster?.width * raster?.height && rasterOff === 0 && probed === 60 && viewerOff === 0 && /H&E/.test(made.message ?? ''),
              'the image over every pixel; every probe the suite\'s placing; the Viewer within 4 levels');
          }

          const regionSteps = await card('regions-qupath');
          const drawn = await chrome.eval(`(async () => {
            const app = window.situcraft;
            const v = window.situcraftViewer;
            const { labelSource, refOf } = await import('/ui/label-sources.js');
            const types = app.store.project.artifacts.find((a) => a.kind === 'labels' && /instrument_cell_types/.test(a.name ?? ''));
            const t = types ? await labelSource(app, refOf(types.id, 0)) : null;
            return v.regions.list().map((r) => {
              const rows = v.regions.rowsIn(r.id);
              const c = {};
              for (const i of rows) if (t) c[t.names[t.label[i]]] = (c[t.names[t.label[i]]] ?? 0) + 1;
              return { name: r.name, area: r.area, cells: rows.length, types: c };
            });
          })()`);
          const R = (n) => drawn.find((r) => r.name === n);
          const back = (n) => R(n) && R(`${n} (2)`) && Math.abs(R(n).area - R(`${n} (2)`).area) < 0.05 && R(n).cells === R(`${n} (2)`).cells;
          check('viewer', 'the worked example Regions for QuPath: two domains drawn on the H&E, their cells and true types, written in its pixels and read back as they were, as its card states', `${regionSteps.map((x) => x.ok ? `step ${x.data.step}` : x.message).join('; ')}; ${JSON.stringify(drawn)}`,
            regionSteps.every((x) => x.ok) && /finished/.test(regionSteps.at(-1).message ?? '') && R('Epithelial domain')?.cells === 1043 && Math.round(R('Epithelial domain').area) === 105675 && R('Epithelial domain').types['epithelial-like'] === 440
            && R('Immune domain')?.cells === 475 && Math.round(R('Immune domain').area) === 33650 && R('Immune domain').types['immune-like'] === 319 && back('Epithelial domain') && back('Immune domain'),
            'the epithelial domain 1,043 cells (440 epithelial-like), the immune 475 (319 immune-like); back within 0.05 µm², the same cells');
          await act('tour', { end: true });

          const reviewSteps = await card('pathologist-review');
          const reviewed = await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { labelSource, refOf } = await import('/ui/label-sources.js');
            const p = app.store.project;
            const ds = p.datasets.find((x) => x.name === 'simulated-standard');
            const truthSet = p.artifacts.find((a) => a.kind === 'labels' && /instrument_cell_types/.test(a.name ?? ''));
            const t = await labelSource(app, refOf(truthSet.id, 0));
            const wrong = async (id) => {
              const s = await labelSource(app, refOf(id, 0));
              let n = 0;
              for (let i = 0; i < s.label.length; i += 1) if (s.label[i] >= 0 && s.names[s.label[i]] !== t.names[t.label[i]]) n += 1;
              return n;
            };
            const records = (p.reviews ?? []).filter((r) => r.dataset === ds.id && r.by === 'the worked example');
            const accept = records.filter((r) => r.action === 'accept' && r.target === 'cells');
            const relabel = records.filter((r) => r.action === 'relabel' && r.target === 'cells');
            const excludedCells = records.filter((r) => r.action === 'exclude' && r.target === 'cells').flatMap((r) => r.cells).length;
            const curated = p.artifacts.find((a) => a.kind === 'curated-labels' && a.dataset === ds.id && relabel[0] && a.id === relabel[0].edits[0].set);
            const markers = relabel[0] ? relabel[0].labels.id : null;
            // Each relabel's new label is the cell's true type; each accept's, its label already.
            const right = [...accept, ...relabel].every((r) => r.cells.length === 1 && r.next === (r.action === 'accept' ? r.cells[0].label : t.names[t.label[r.cells[0].row]]) && r.images.length === 1);
            return { records: records.length, accepted: accept.length, relabeled: relabel.length, excludedCells, right, queue: app.labelReview?.queue?.()?.counts?.flagged, before: markers ? await wrong(markers) : null, after: curated ? await wrong(curated.id) : null };
          })()`);
          check('viewer', 'the worked example A pathologist\'s review: the queue\'s first twenty decided by the truth (accepted where right, relabeled where not, each recorded with its provenance), a fold\'s cells excluded, as its card states', `${reviewSteps.map((x) => x.ok ? `step ${x.data.step}` : x.message).join('; ')}; ${JSON.stringify(reviewed)}`,
            reviewSteps.every((x) => x.ok) && /finished/.test(reviewSteps.at(-1).message ?? '') && reviewed.accepted === 18 && reviewed.relabeled === 2 && reviewed.right && reviewed.excludedCells === 43 && reviewed.records === 22 && reviewed.before === 49 && reviewed.after === 47,
            '18 accepted, 2 relabeled (to their true types), 43 cells excluded, 22 reviews; 49 wrong labels before, 47 after');
          await act('tour', { end: true });

          // The spatial worked example on the simulated tissue (wave 6, slice 10): niches of the true
          // cells by their true types, read against the domains the simulator laid out, and the
          // defaults' less stable niches, each number its card states checked; the Viewer filled by
          // each.
          const nicheSteps = await card('niches-domains');
          const fourId = stepOf(nicheSteps, 6)?.spatial?.analysis;
          const sixId = stepOf(nicheSteps, 10)?.spatial?.analysis;
          const fills = [];
          for (const n of [7, 8]) {
            await act('tour', { example: 'simulated-standard', name: 'niches-domains', step: n });
            fills.push(await chrome.eval(`(async () => {
              const v = window.situcraftViewer;
              for (let k = 0; k < 300 && !(v.clusterFill?.()?.labels && v.clusterFill().sets.length === ${n === 7 ? 4 : 6}); k += 1) await new Promise((r) => setTimeout(r, 100));
              const f = v.clusterFill?.();
              return f ? { name: f.name, counts: [...f.counts.values()].sort((a, b) => b - a) } : null;
            })()`));
          }
          await act('tour', { end: true });
          const sn = fourId && sixId ? await chrome.eval(`(async () => {
            const app = window.situcraft;
            const { readAnalyzed } = await import('/lib/spatial.js');
            const { labelSource, refOf } = await import('/ui/label-sources.js');
            const p = app.store.project;
            const ds = p.datasets.find((x) => x.name === 'simulated-standard');
            const read = async (id) => (await fetch('/api/artifacts/' + id + '/files/analysis.json')).json();
            const a = await read(${JSON.stringify(fourId)}), six = await read(${JSON.stringify(sixId)});
            const r = a.niches, names = a.labels.names, K = names.length;
            // Each niche's leading type and its share.
            const lead = r.cells.map((n, v) => {
              let best = 0;
              for (let k = 1; k < K; k += 1) if (r.composition[v * K + k] > r.composition[v * K + best]) best = k;
              return names[best] + ' ' + Math.round(100 * r.composition[v * K + best]) + '% of ' + n;
            });
            // Each true cell's niche against its domain: each domain's niche the one most of its
            // cells lie in.
            const domains = p.artifacts.find((x) => x.kind === 'labels' && x.dataset === ds.id && x.source?.column === 'domain');
            const t = domains ? await labelSource(app, refOf(domains.id, 0)) : null;
            const c = readAnalyzed(await (await fetch('/api/analyses/${fourId}/cells')).arrayBuffer());
            const tally = t ? t.names.map(() => new Array(r.niches).fill(0)) : [];
            for (let i = 0; i < c.n; i += 1) {
              const d = t?.label[c.rows[i]];
              if (d >= 0 && c.stratum[i] > 0) tally[d][c.stratum[i] - 1] += 1;
            }
            let own = 0, all = 0;
            const home = {}, share = [];
            tally.forEach((row, d) => {
              const top = row.indexOf(Math.max(...row));
              const n = row.reduce((s, v) => s + v, 0);
              home[t.names[d]] = top;
              own += row[top];
              all += n;
              share.push(row[top] / n);
            });
            return {
              cells: a.cells.analyzed, niches: r.niches, neighbors: a.parameters.neighbors, lead, agreement: r.agreement,
              domains: t?.names.length ?? 0, own: Math.round(1000 * own / all) / 10, lowest: Math.round(100 * Math.min(...share)), highest: Math.round(100 * Math.max(...share)),
              shared: home['0'] === home['4'] && home['1'] === home['5'] && new Set(Object.values(home)).size === 4,
              six: { niches: six.niches.niches, neighbors: six.parameters.neighbors, agreement: six.niches.agreement.map((v) => Math.round(100 * v) / 100) },
            };
          })()`) : null;
          const ag = sn?.six.agreement ?? [];
          check('viewer', 'the worked example Niches against the domains: four niches of the true cells, each led by one type and stable across seeds, 96.5% of the cells in their domain\'s niche, the six of the defaults less stable, and the Viewer filled by niches and by domains, as its card states', `${stepsRun(nicheSteps)}; ${JSON.stringify(sn)}; fills ${JSON.stringify(fills)}`,
            finished(nicheSteps) && sn && sn.cells === 3030 && sn.niches === 4 && sn.neighbors === 30 && sn.lead.join('; ') === 'epithelial-like 47% of 1078; endothelial-like 53% of 887; immune-like 72% of 554; fibroblast-like 44% of 511' && sn.agreement.every((v) => v === 1)
            && sn.domains === 6 && sn.own === 96.5 && sn.lowest === 93 && sn.highest === 99 && sn.shared
            && sn.six.niches === 6 && sn.six.neighbors === 10 && Math.min(...ag) === 0.66 && Math.max(...ag) === 0.93
            && /^Niches of/.test(fills[0]?.name ?? '') && JSON.stringify(fills[0]?.counts) === '[1078,887,554,511]' && /domain/.test(fills[1]?.name ?? '') && fills[1]?.counts.length === 6 && fills[1].counts.reduce((s, v) => s + v, 0) === 3030,
            'epithelial-like 47% of 1,078, endothelial-like 53% of 887, immune-like 72% of 554, fibroblast-like 44% of 511; the seeds agree (1.00); 96.5% in their domain\'s niche (93% to 99%), domains 0 and 4 sharing one, 1 and 5 another; six niches of 10 agree 0.66 to 0.93; filled by 4 niches, then 6 domains');
        }

        // Open a bundle, the folder browser (browse.go, ui/file-picker.js): the development bundles'
        // folder walked to in the dialog, each bundle marked with its run, region and cells as the
        // suite reads them from its manifest; a file's path typed lists its bundle's folder with it
        // chosen; a bundle double-clicked is opened where it is.
        if (present.includes('xenium-lung-2fov')) {
          await act('new_project', { name: 'Opening' });
          await act('set_mode', { mode: 'data' });
          const own = Object.fromEntries(present.filter((name) => existsSync(join(dataRoot, name, 'experiment.xenium'))).map((name) => {
            const m = JSON.parse(readFileSync(join(dataRoot, name, 'experiment.xenium'), 'utf8'));
            return [name, [m.run_name, m.region_name, Number.isFinite(m.num_cells) ? `${m.num_cells.toLocaleString('en-US')} cells` : ''].filter(Boolean).join(' · ')];
          }));
          const walked = await chrome.eval(`(async () => {
            const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
            const until = async (test) => { for (let k = 0; k < 100 && !test(); k += 1) await sleep(50); return test(); };
            [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Open a bundle').click();
            await until(() => document.querySelector('.picker-crumb'));
            const go = async (path, last) => {
              const input = document.querySelector('.picker-path');
              input.value = path;
              input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
              await until(() => document.querySelector('.picker-crumb.current')?.textContent === last);
            };
            await go(${JSON.stringify(dataRoot)}, ${JSON.stringify(basename(dataRoot))});
            const rows = () => [...document.querySelectorAll('.picker-row')].map((r) => ({ name: r.querySelector('.picker-name').textContent, kind: r.querySelector('.picker-kind').textContent, icon: r.querySelector('.picker-icon svg')?.outerHTML.length ?? 0, selected: r.getAttribute('aria-selected') === 'true', dim: r.classList.contains('dim') }));
            const folder = rows();
            await go(${JSON.stringify(join(dataRoot, 'xenium-lung-2fov', 'gene_panel.json'))}, 'xenium-lung-2fov');
            const inside = { banner: document.querySelector('.picker-inside:not([hidden])')?.textContent ?? '', chosen: rows().filter((r) => r.selected).map((r) => r.name), note: document.querySelector('.picker-note').textContent };
            await go(${JSON.stringify(dataRoot)}, ${JSON.stringify(basename(dataRoot))});
            [...document.querySelectorAll('.picker-row')].find((r) => r.querySelector('.picker-name').textContent === 'xenium-lung-2fov').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }));
            await until(() => !document.querySelector('.picker'));
            await sleep(400);
            return { folder, inside, closed: !document.querySelector('.picker') };
          })()`);
          const openedNow = (await json(`${url}/api/info`)).files ?? [];
          const marked = Object.entries(own).map(([name, words]) => ({ name, words, row: walked.folder.find((r) => r.name === name) }));
          check('viewer', 'Open a bundle: the folder browser walks to the bundles\' folder, each bundle marked with its run, region and cells as its manifest says; a file\'s path lists its bundle with the file chosen; a bundle double-clicked opens where it is',
            `${JSON.stringify(marked.map((m) => [m.name, m.row?.kind]))}; inside ${JSON.stringify(walked.inside)}; opened ${openedNow.map((f) => f.name).join(', ')}`,
            marked.length >= 1 && marked.every((m) => m.row && m.row.kind === (m.words || 'Xenium bundle') && !m.row.dim)
            && /xenium-lung-2fov is a Xenium bundle/.test(walked.inside.banner) && walked.inside.chosen.join() === 'gene_panel.json'
            && walked.closed && openedNow.some((f) => f.kind === 'bundle' && f.path === join(dataRoot, 'xenium-lung-2fov')),
            'every bundle marked as its manifest says; gene_panel.json chosen inside the Lung bundle; the Lung bundle opened');
        }

        // Samples (wave 7, slice 1): the Lung and Breast bundles added together, each a sample with
        // its bundle's run, slide and panel; fields set by remote control, written as CSV and read
        // back exactly, values that do not fit refused with nothing changed; two regions of the Lung
        // made its samples, their cells the suite's own count by centroid and their areas their
        // outlines'; all kept when the project is opened again; the Lung one sample again, with its
        // fields.
        if (present.includes('xenium-breast-2fov')) {
          await act('new_project', { name: 'Samples' });
          const opened = await act('open_paths', { paths: ['xenium-lung-2fov', 'xenium-breast-2fov'].map((b) => join(dataRoot, b)), add: true });
          const listed = await act('samples');
          const ls = listed.data?.samples ?? [];
          check('viewer', 'remote control: bundles opened together are added as a project\'s samples, each a sample of its own with its bundle\'s run, slide and panel', `${opened.message ?? opened.error}; ${listed.message ?? listed.error}`,
            opened.ok && listed.ok && ls.length === 2 && ls.every((s) => s.region === null && s.bundle.run && s.bundle.slide) && ls.map((s) => s.bundle.panel).join() === 'hLung_v1.1,hBreast_v1' && ls[0].cells === 11898 && ls[1].cells === 7275, 'two samples, 11,898 and 7,275 cells; panels hLung_v1.1 and hBreast_v1');
          // Their panels compared (wave 7, slice 5) against the suite's own reading of each bundle's
          // gene_panel.json: the genes both have, in the Lung panel's order; those one has; the shared
          // genes read by other codewords; those with other Ensembl IDs.
          const panelsCompared = await act('compare_panels');
          const panelGenes = (name) => Object.fromEntries(JSON.parse(readFileSync(join(dataRoot, name, 'gene_panel.json'), 'utf8')).payload.targets.filter((tg) => tg.type.descriptor === 'gene').map((tg) => [tg.type.data.name, { id: tg.type.data.id, codewords: [...tg.codewords].sort((x, y) => x - y).join() }]));
          const lungGenes = panelGenes('xenium-lung-2fov');
          const breastGenes = panelGenes('xenium-breast-2fov');
          const bothGenes = Object.keys(lungGenes).filter((g) => breastGenes[g]);
          const oneGene = Object.keys(lungGenes).length + Object.keys(breastGenes).length - 2 * bothGenes.length;
          const pc = panelsCompared.data ?? {};
          check('viewer', 'the samples\' panels compared before any gene is: the genes in both the Lung and the Breast panel, those in one only, and the shared genes read by other codewords, each the suite\'s own count from the panels\' files',
            `${panelsCompared.message ?? panelsCompared.error}; the suite: ${bothGenes.length} in both, ${oneGene} in one`,
            panelsCompared.ok && pc.panels?.length === 2 && JSON.stringify(pc.shared) === JSON.stringify(bothGenes) && pc.partial?.length === oneGene
            && pc.codewords?.length === bothGenes.filter((g) => lungGenes[g].codewords !== breastGenes[g].codewords).length && pc.ids?.length === bothGenes.filter((g) => lungGenes[g].id !== breastGenes[g].id).length,
            `${bothGenes.length} genes in both panels (in order), ${oneGene} in one; codewords and IDs as the files say`);
          const set = await act('set_samples', { fields: [{ name: 'age', kind: 'number' }, { name: 'notes', kind: 'text' }], values: { 'xenium-lung-2fov': { subject: 'P1', condition: 'tumor', age: 0.1 + 0.2, notes: 'said "fine",\nthen left' }, 'xenium-breast-2fov': { subject: 'P2', condition: 'normal', age: 0 } } });
          const csvPath = join(work, 'samples.csv');
          const written = await act('export', { what: 'samples', path: csvPath, overwrite: true });
          const csv = existsSync(csvPath) ? readFileSync(csvPath, 'utf8') : '';
          // Cleared, then read back from the file as a table.
          await act('set_samples', { remove_fields: ['age', 'notes'], values: { 'xenium-lung-2fov': { subject: null, condition: null }, 'xenium-breast-2fov': { subject: '', condition: '' } } });
          const cleared = await act('samples');
          const back = await act('set_samples', { table: csv });
          await act('export', { what: 'samples', path: csvPath, overwrite: true });
          const again = existsSync(csvPath) ? readFileSync(csvPath, 'utf8') : '';
          const fb = back.data?.samples?.map((s) => s.fields) ?? [];
          check('viewer', 'samples\' fields set by remote control, written as CSV and read back exactly (quotes, a line break, a number\'s every digit, a zero)', `${set.message ?? set.error}; ${written.message ?? written.error}; ${back.message ?? back.error}`,
            set.ok && written.ok && back.ok && csv.length > 0 && again === csv && cleared.data?.samples.every((s) => s.fields.subject === null && !('age' in s.fields)) && fb[0]?.age === 0.30000000000000004 && fb[0]?.notes === 'said "fine",\nthen left' && fb[1]?.age === 0 && fb[1]?.notes === null && back.data.fields.map((f) => `${f.name}:${f.kind}`).join() === 'subject:text,condition:text,age:number,notes:text',
            'the same file written again; 0.30000000000000004 and 0 read back as numbers');
          const badValue = await act('set_samples', { values: { 'xenium-breast-2fov': { condition: 'tumor', age: 'old' } } });
          const badTable = await act('set_samples', { table: 'sample,subject\nxenium-lung-2fov,P8\nXENIUM-LUNG-2FOV,P9\n' });
          const unchanged = await act('samples');
          check('viewer', 'samples\' values that do not fit are refused, saying why, and nothing is changed', `${badValue.message ?? badValue.error}; ${badTable.message ?? badTable.error}`,
            !badValue.ok && /not a number/.test(badValue.message ?? badValue.error ?? '') && !badTable.ok && /Two rows name/.test(badTable.message ?? badTable.error ?? '') && unchanged.data?.samples[1].fields.condition === 'normal' && unchanged.data.samples[0].fields.subject === 'P1', 'refused; normal and P1 still');
          // Two regions of the Lung as its samples: cells by centroid, as the suite counts them.
          const coreA = [[100, 100], [400, 100], [400, 400], [100, 400]];
          const coreB = [[700, 300], [1000, 320], [950, 600], [720, 560]];
          await act('draw_region', { dataset: 'xenium-lung-2fov', name: 'Core A', rectangle: [100, 100, 400, 400] });
          await act('draw_region', { dataset: 'xenium-lung-2fov', name: 'Core B', polygon: coreB });
          const asRegions = await act('set_samples', { regions: { dataset: 'xenium-lung-2fov', regions: ['Core B', 'Core A'] }, values: { 'Core A': { subject: 'P1', condition: 'tumor' } } });
          await act('show_tissue', { dataset: 'xenium-lung-2fov', segmentation: 'instrument' });
          const drawnCells = await chrome.eval('(() => { const c = window.situcraftViewer.cellsDrawn(); return { x: Array.from(c.x), y: Array.from(c.y) }; })()');
          const inRing = (ring, px, py) => {
            let inside = false;
            for (let i = 0, j = ring.length - 1; i < ring.length; j = i, i += 1) {
              const [xi, yi] = ring[i];
              const [xj, yj] = ring[j];
              if ((yi > py) !== (yj > py) && px < ((xj - xi) * (py - yi)) / (yj - yi) + xi) inside = !inside;
            }
            return inside;
          };
          const ownCells = (ring) => drawnCells.x.reduce((n, x, i) => n + (inRing(ring, x, drawnCells.y[i]) ? 1 : 0), 0);
          const shoelace = (ring) => Math.abs(ring.reduce((s, [x, y], i) => s + x * ring[(i + 1) % ring.length][1] - ring[(i + 1) % ring.length][0] * y, 0)) / 2;
          const regionSamples = await act('samples');
          const rs = regionSamples.data?.samples ?? [];
          const counted = { a: ownCells(coreA), b: ownCells(coreB) };
          check('viewer', 'a section\'s regions made its samples, in the order drawn: their cells the suite\'s own count by centroid, their areas their outlines\'', `${asRegions.message ?? asRegions.error}; ${JSON.stringify(rs.map((s) => [s.sample, s.region, s.cells, s.area_um2]))}; the suite counts ${JSON.stringify(counted)}`,
            asRegions.ok && rs.length === 3 && rs.map((s) => s.sample).join() === 'Core A,Core B,xenium-breast-2fov' && rs[0].cells === counted.a && rs[1].cells === counted.b && counted.a > 100 && counted.b > 100 && Math.abs(rs[0].area_um2 - 90000) < 1e-6 && Math.abs(rs[1].area_um2 - shoelace(coreB)) < 1e-6 && rs[0].fields.subject === 'P1',
            `${counted.a} and ${counted.b} cells; 90,000 and ${Math.round(shoelace(coreB)).toLocaleString('en-US')} µm²`);
          // Kept with the project.
          await act('new_project', { name: 'Elsewhere' });
          await act('open_project', { project: 'Samples' });
          const reopened = await act('samples');
          const whole = await act('set_samples', { regions: { dataset: 'xenium-lung-2fov', regions: [] } });
          const ws = whole.data?.samples ?? [];
          check('viewer', 'samples and their fields are kept with the project; a section made one sample again has its fields back', `${reopened.message ?? reopened.error}; ${whole.message ?? whole.error}`,
            reopened.ok && JSON.stringify(reopened.data.samples.map((s) => [s.sample, s.cells, s.fields])) === JSON.stringify(rs.map((s) => [s.sample, s.cells, s.fields])) && whole.ok && ws.length === 2 && ws[0].sample === 'xenium-lung-2fov' && ws[0].fields.age === 0.30000000000000004 && ws[0].fields.subject === 'P1',
            'the same samples, cells and fields; the Lung one sample with its own');
        }

        // A simulated cohort (wave 7, slice 2): made here and opened, each sample's bundle added
        // with its subject and condition, and its slide and run read from it; from its files, the
        // panel the same in every sample, the truth's cells each bundle's own, the table the
        // samples', and every effect as the truth measures it the suite's own from its counts.
        {
          // In a project of its own (an empty one would take it in, whatever its name), reopened
          // later by its id.
          await act('new_project', {});
          const made = await act('open_example', { example: 'cohort' });
          const cohortProject = await chrome.eval('window.situcraft.store.project.id');
          const listed = await act('samples');
          const cs = listed.data?.samples ?? [];
          const examplesNow = await json(`${url}/api/examples`);
          const cohortDir = examplesNow.examples.find((e) => e.name === 'simulated-cohort')?.path;
          const own = cohortDir ? (() => {
            const read = (p) => JSON.parse(readFileSync(join(cohortDir, p), 'utf8'));
            const truth = read('truth/cohort.json');
            const desc = read('situcraft-cohort.json');
            const panels = new Set(desc.samples.map((s) => readFileSync(join(cohortDir, s.name, 'gene_panel.json'), 'utf8')));
            const countsMatch = truth.samples.every((s) => {
              const m = read(`${s.name}/situcraft-simulation.json`).made;
              return m.cells === s.cells && JSON.stringify(Object.entries(m.cells_by_type).sort()) === JSON.stringify(Object.entries(s.cells_by_type).sort());
            });
            const table = readFileSync(join(cohortDir, 'samples.csv'), 'utf8').trim().split('\n').slice(1).map((l) => l.split(','));
            // Each effect from the truth's counts: the type's log odds against the cells of the
            // types no share effect changes, in each domain the effect holds in, averaged; or the
            // gene's log odds among its type's transcripts; the groups' means' difference.
            const changed = (type, leader) => truth.effects.some((x) => x.kind === 'share' && x.type === type && (!x.within || x.within === leader));
            const value = (e, s) => {
              if (e.kind === 'level') {
                const l = s.levels.find((x) => x.type === e.type && x.gene === e.gene);
                return Math.log((l.transcripts + 0.5) / (l.type_transcripts - l.transcripts + 0.5));
              }
              const vs = s.domains.filter((d) => !e.within || d.leader === e.within).map((d) => {
                const rest = Object.entries(d.cells_by_type).filter(([type]) => type !== e.type && !changed(type, d.leader)).reduce((a, [, v]) => a + v, 0);
                return Math.log(((d.cells_by_type[e.type] ?? 0) + 0.5) / (rest + 0.5));
              });
              return vs.reduce((a, v) => a + v, 0) / vs.length;
            };
            const mean = (v) => v.reduce((a, x) => a + x, 0) / v.length;
            const effects = truth.effects.map((e) => {
              const of = (g) => truth.samples.filter((s) => s.group === g).map((s) => value(e, s));
              const measured = mean(of(e.group)) - mean(of(truth.design.groups[0]));
              return { of: `${e.kind} ${e.type}${e.within ? ` in ${e.within}` : ''}${e.gene ? ` ${e.gene}` : ''} ×${e.fold}`, measured: Math.exp(measured), off: Math.abs(measured - e.measured_log_fold), z: (e.measured_log_fold - e.log_fold) / e.std_error };
            });
            return { panels: panels.size, countsMatch, table, effects, samples: truth.samples.length, slides: truth.design.slides };
          })() : null;
          check('viewer', 'a simulated cohort made here and opened: each sample added with its subject, condition, slide and run; its panel the same in every sample, the truth\'s cells each bundle\'s own, and each effect the truth measures the suite\'s own from its counts', `${made.message ?? made.error}; ${JSON.stringify(cs.map((s) => [s.sample, s.fields.subject, s.fields.condition, s.bundle.slide]))}; ${JSON.stringify(own && { ...own, table: own.table.length })}`,
            made.ok && cs.length === 8 && cs.map((s) => s.sample).join() === 'S01,S02,S03,S04,S05,S06,S07,S08' && cs.every((s, k) => s.fields.subject === s.sample && s.fields.condition === (k < 4 ? 'control' : 'treated') && s.bundle.slide === `simulated slide ${(k % 2) + 1}` && s.bundle.run === `simulated run ${(k % 2) + 1}` && s.cells > 500)
            && own && own.panels === 1 && own.countsMatch && own.samples === 8 && own.table.every(([name, subject, condition], k) => name === cs[k].sample && subject === cs[k].fields.subject && condition === cs[k].fields.condition)
            && own.effects.length === 3 && own.effects.every((e) => e.off < 1e-9 && Math.abs(e.z) < 3),
            'eight samples, four of each group, on two slides; one panel; the counts and every effect as recorded');

          // The design (wave 7, slice 3), on the simulated cohort: its subjects compared by
          // condition, adjusted for the slide; a field confounded with the condition refused and not
          // saved; the subjects paired; a group of one subject refused; versions kept with the
          // project.
          const adjusted = await act('design', { unit: 'subject', group: 'condition', reference: 'control', covariates: ['slide'] });
          const ad = adjusted.data ?? {};
          await act('set_samples', { fields: [{ name: 'batch', kind: 'text' }], values: Object.fromEntries(cs.map((s, k) => [s.sample, { batch: k < 4 ? 'B1' : 'B2' }])) });
          const confounded = await act('design', { unit: 'subject', group: 'condition', covariates: ['batch'] });
          const notSaved = await act('design', { unit: 'subject', group: 'condition', covariates: ['batch'], save: 'Confounded' });
          const one = await act('design', { unit: 'subject', group: 'condition', exclude: { S06: 'testing', S07: 'testing', S08: 'testing' } });
          const saved1 = await act('design', { unit: 'subject', group: 'condition', reference: 'control', covariates: ['slide'], save: 'Main' });
          // Paired: each control sample's subject that of a treated one.
          await act('set_samples', { values: Object.fromEntries(cs.map((s, k) => [s.sample, { subject: `D${(k % 4) + 1}` }])) });
          const paired = await act('design', { unit: 'subject', group: 'condition', reference: 'control', save: 'Main' });
          await act('new_project', { name: 'Elsewhere' });
          await act('open_project', { project: cohortProject });
          const kept = await act('design');
          check('viewer', 'the design: the cohort\'s subjects compared by condition and adjusted for the slide; a field confounded with the condition, and a group of one subject, refused and not saved; the subjects paired; versions kept with the project', `${adjusted.message ?? adjusted.error} | ${confounded.message ?? confounded.error} | ${notSaved.message ?? notSaved.error} | ${one.message ?? one.error} | ${paired.message ?? paired.error} | ${kept.message ?? kept.error}`,
            adjusted.ok && ad.ok && ad.names?.join() === 'Intercept,condition[T.treated],slide[T.simulated slide 2]' && ad.rows.length === 8 && ad.rank === 3 && ad.df === 5 && ad.matrix[1].join() === '1,0,1' && /4 subjects in control, 4 subjects in treated/.test(adjusted.message)
            && confounded.ok && confounded.data.ok === false && /batch is confounded with condition/.test(confounded.message) && !notSaved.ok && /cannot be saved/.test(notSaved.message ?? notSaved.error ?? '')
            && one.ok && one.data.ok === false && /treated has 1 subject/.test(one.message) && one.data.excluded.length === 3
            && saved1.data?.saved?.version === 1 && paired.data?.paired === true && paired.data.df === 3 && paired.data.names.join() === 'Intercept,condition[T.treated],subject[T.D2],subject[T.D3],subject[T.D4]' && paired.data.saved?.version === 2
            && kept.ok && kept.data.paired === true && kept.data.df === 3,
            'adjusted: 3 coefficients over 8 rows, 5 df; refused twice; paired: 5 over 8, 3 df, version 2; kept');

          // Summaries (wave 7, slice 4), on the simulated cohort: the same steps on every sample
          // (clustered, labeled from one marker set, a graph and three questions each; asked again,
          // answered at once), summarized; every number against the artifact it came from; the true
          // values (each sample's shares its true types', its tissue the section's, the pseudobulk's
          // SIM033 among epithelial-like transcripts the truth's); the niches found together against
          // each sample's own (one sample alone: exactly its own), their defining labels the
          // simulator's domain leaders.
          const steps = await act('cohort_steps', { questions: ['enrichment', 'distances', 'moran'] });
          const stepsAgain = await act('cohort_steps', { questions: ['enrichment', 'distances', 'moran'] });
          const summary = await act('summarize_samples', {});
          const sd = summary.data ?? {};
          const art = (id, file) => json(`${url}/api/artifacts/${encodeURIComponent(id)}/files/${file}`);
          const tableOf = async (family) => (await act('cohort_table', { family })).data;
          const [tCells, tShare, tDensity, tQC, tGraph, tEnrichment, tNearest, tMoran] = await Promise.all(['cells', 'share', 'density', 'qc', 'graph', 'enrichment', 'nearest', 'moran'].map(tableOf));
          const cohortNames = sd.labels?.names ?? [];
          const off = [];
          let compared = 0;
          const agree = (ok, what) => {
            compared += 1;
            if (!ok) off.push(what);
          };
          for (const st of steps.data?.samples ?? []) {
            const row = (t) => t?.samples.find((x) => x.sample === st.sample) ?? { values: {}, sources: {} };
            const labelsDoc = await art(st.labels, 'labels.json');
            for (const set of labelsDoc.sets) agree(row(tCells).values[set.name] === set.cells && row(tCells).sources[set.name] === st.labels, `${st.name} ${set.name} cells`);
            const labeled = cohortNames.reduce((n, l) => n + row(tCells).values[l], 0);
            agree(labeled === labelsDoc.cells.labeled[0], `${st.name} labeled`);
            const tissueArea = row(tQC).values['tissue area µm²'];
            for (const l of cohortNames) {
              agree(row(tShare).values[l] === row(tCells).values[l] / labeled, `${st.name} ${l} share`);
              agree(Math.abs(row(tDensity).values[l] - row(tCells).values[l] / (tissueArea / 1e6)) <= 1e-9 * row(tDensity).values[l], `${st.name} ${l} density`);
            }
            const gm = await json(`${url}/api/artifacts/${encodeURIComponent(st.graph)}`);
            agree(row(tGraph).values['mean neighbors'] === gm.summary.meanDegree && row(tGraph).values['median edge µm'] === gm.summary.medianLength && row(tGraph).values.pieces === gm.summary.components && row(tGraph).sources['mean neighbors'] === st.graph, `${st.name} graph`);
            for (const a of st.answers) {
              const an = await art(a.id, 'analysis.json');
              const n = an.labels?.names ?? [];
              const K = n.length;
              if (a.question === 'enrichment') {
                for (let x = 0; x < K; x += 1) {
                  for (let y = x; y < K; y += 1) {
                    const q = x * K + y;
                    const key = [n[x], n[y]].sort().join('|');
                    if (an.enrichment.expected[q] > 0) agree(row(tEnrichment).values[key] === an.enrichment.count[q] / an.enrichment.expected[q] && row(tEnrichment).sources[key] === a.id, `${st.name} enrichment ${key}`);
                  }
                }
              } else if (a.question === 'distances') {
                for (let x = 0; x < K; x += 1) {
                  for (let y = 0; y < K; y += 1) {
                    const key = `${n[x]}→${n[y]}`;
                    const v = an.nearest.median[x * K + y];
                    agree(v === null ? row(tNearest).values[key] === null : row(tNearest).values[key] === v && row(tNearest).sources[key] === a.id, `${st.name} nearest ${key}`);
                  }
                }
              } else an.moran.genes.forEach((g, k) => agree(row(tMoran).values[g] === an.moran.i[k], `${st.name} Moran's I of ${g}`));
            }
          }
          // The true values.
          const cohortTruth = JSON.parse(readFileSync(join(cohortDir, 'truth/cohort.json'), 'utf8'));
          const pbRows = (await (await fetch(`${url}/api/artifacts/${encodeURIComponent(sd.id ?? '')}/files/pseudobulk.csv`)).text()).trim().split('\n').map((l) => l.split(','));
          const pbGenes = pbRows[0].slice(4);
          const truthOff = cohortTruth.samples.map((t) => {
            const r = tCells?.samples.find((x) => x.name === t.name);
            if (!r) return { sample: t.name, shares: Infinity, tissue: Infinity, level: Infinity };
            const total = Object.values(t.cells_by_type).reduce((n, v) => n + v, 0);
            const labeled = cohortNames.reduce((n, l) => n + r.values[l], 0);
            const shares = Math.max(...cohortNames.map((l) => Math.abs(r.values[l] / labeled - (t.cells_by_type[l] ?? 0) / total)));
            const tissueArea = tQC.samples.find((x) => x.name === t.name).values['tissue area µm²'];
            const lv = t.levels.find((x) => x.type === 'epithelial-like' && x.gene === 'SIM033');
            const pr = pbRows.find((x) => x[1] === t.name && x[2] === 'epithelial-like');
            const sum = pbGenes.reduce((n, g, k) => n + Number(pr[4 + k]), 0);
            const level = Math.log((Number(pr[4 + pbGenes.indexOf('SIM033')]) / sum) / (lv.transcripts / lv.type_transcripts));
            return { sample: t.name, shares: Number(shares.toFixed(4)), tissue: Number((tissueArea / (250 * 250) - 1).toFixed(4)), level: Number(level.toFixed(4)) };
          });
          // The niches found together against each sample's own, with the same parameters.
          // A summary's niches.bin (its envelope, then zlib: the cells, each one's row and niche).
          const cohortNiches = async (id) => {
            const raw = Buffer.from(await (await fetch(`${url}/api/artifacts/${encodeURIComponent(id ?? '')}/files/niches.bin`)).arrayBuffer());
            if (raw.subarray(0, 4).toString() !== 'SCCN') return { n: 0, row: () => 0, niche: () => -1 };
            const body = inflateSync(raw.subarray(16));
            const n = body.readUInt32LE(0);
            return { n, row: (i) => body.readUInt32LE(4 + 4 * i), niche: (i) => body.readInt32LE(4 + 4 * n + 4 * i) };
          };
          const together = await cohortNiches(sd.id);
          const ownNiches = async (st) => {
            const own = await act('spatial_analysis', { question: 'niches', dataset: st.name, labels: st.labels, neighbors: 10, niches: 6, starts: 10, seeds: 4, seed: 20250 });
            if (!own.ok) return new Map();
            const cv = new DataView(await (await fetch(`${url}/api/analyses/${encodeURIComponent(own.data?.id ?? '')}/cells`)).arrayBuffer());
            const m = cv.byteLength >= 4 ? cv.getUint32(0, true) : 0;
            const of = new Map();
            for (let i = 0; i < m; i += 1) of.set(cv.getUint32(4 + 4 * i, true), cv.getInt32(4 + 8 * m + 4 * i, true) - 1);
            return of;
          };
          const aris = [];
          let at = 0;
          for (const st of steps.data?.samples ?? []) {
            const of = await ownNiches(st);
            const n = sd.samples?.find((x) => x.sample === st.sample)?.cells ?? 0;
            const a = [];
            const b = [];
            for (let i = at; i < at + n && i < together.n; i += 1) {
              a.push(together.niche(i));
              b.push(of.get(together.row(i)) ?? -1);
            }
            at += n;
            aris.push(Number(contingency(a, b, 6, 6).ari.toFixed(3)));
          }
          const alone = await act('summarize_samples', { samples: ['S01'] });
          const aloneNiches = await cohortNiches(alone.data?.id);
          const aloneOwn = await ownNiches(steps.data?.samples?.[0] ?? {});
          const aloneN = aloneNiches.n;
          let aloneSame = 0;
          for (let i = 0; i < aloneN; i += 1) if (aloneNiches.niche(i) === aloneOwn.get(aloneNiches.row(i))) aloneSame += 1;
          const leaders = new Set(cohortTruth.samples.flatMap((t) => t.domains.map((x) => x.leader)));
          const defining = new Set((sd.niches ?? []).flatMap((x) => x.defining));
          check('viewer', 'the cohort summarized (wave 7, slice 4): the same steps on every sample, answered at once when asked again; every number the artifact\'s it came from; the shares, tissue and a gene\'s level the truth\'s; the niches found together near each sample\'s own, exactly its own for one sample alone, and defined by the simulator\'s domain leaders',
            `${steps.message ?? steps.error} | ${summary.message ?? summary.error} | ${compared} numbers compared, off: ${off.slice(0, 5).join('; ') || 'none'} | truth ${JSON.stringify(truthOff)} | niches' ARI ${aris.join(' ')}; alone ${aloneSame} of ${aloneN} | leaders ${[...leaders].sort().join(', ')}; defining ${[...defining].sort().join(', ')}`,
            steps.ok && steps.data.samples.length === 8 && steps.data.samples.every((x) => x.answers.length === 3) && stepsAgain.ok && JSON.stringify(stepsAgain.data) === JSON.stringify(steps.data)
            && summary.ok && sd.samples.length === 8 && cohortNames.length === 4 && sd.niches?.length === 6 && sd.genes === 120
            && compared >= 8 * (4 * 3 + 2 + 10 + 16 + 120) && off.length === 0
            && truthOff.every((x) => x.shares < 0.03 && Math.abs(x.tissue) < 0.02 && Math.abs(x.level) < 0.15)
            && aris.length === 8 && aris.every((x) => x > 0.45) && alone.ok && aloneN === sd.samples[0].cells && aloneSame === aloneN
            && [...leaders].every((l) => defining.has(l)),
            'every number its source\'s; shares within 0.03 of the truth\'s, tissue within 2%, SIM033 within 0.15 (log); ARI above 0.45 each; one sample alone exactly its own; every leader defines a niche');

          // Cohort QC (wave 7, slice 5): the standard cohort's quality side by side, no sample far out,
          // its batches (two slides and runs, one panel) holding both groups, its pseudobulk's
          // principal components; the groups made to follow the slides, said to be confounded with
          // them; and a cohort with a sample planted to detect 40% of its transcripts, that sample
          // alone found far out, on its transcripts and genes.
          const qcStandard = await act('cohort_qc', { summary: sd.id });
          const qs = qcStandard.data ?? {};
          const slideOf = Object.fromEntries((qs.samples ?? []).map((x) => [x.name, x.bundle?.slide]));
          await act('set_samples', { values: Object.fromEntries(cs.map((x) => [x.sample, { condition: slideOf[x.sample] === 'simulated slide 1' ? 'control' : 'treated' }])) });
          const qcConfounded = await act('cohort_qc', { summary: sd.id });
          await act('set_samples', { values: Object.fromEntries(cs.map((x, k) => [x.sample, { condition: k < 4 ? 'control' : 'treated' }])) });
          const plantedDir = join(work, 'planted-cohort');
          rmSync(plantedDir, { recursive: true, force: true });
          execFileSync(bin, ['simulate', '--cohort', 'outlier', '--quiet', plantedDir]);
          const plantedTruth = JSON.parse(readFileSync(join(plantedDir, 'truth', 'cohort.json'), 'utf8'));
          await act('new_project', { name: 'A planted outlier' });
          const plantedOpened = await act('open_paths', { paths: [plantedDir], add: true });
          await act('cohort_steps', { questions: [] });
          const plantedSummary = await act('summarize_samples', { niches: 0 });
          const qcPlanted = await act('cohort_qc', {});
          const far = qcPlanted.data?.outliers ?? [];
          const planted = plantedTruth.samples.filter((x) => x.planted).map((x) => x.name);
          const variance = qs.pca?.variance ?? [];
          check('viewer', 'cohort QC: the simulated cohort\'s quality by sample, no sample far out, its slides and runs each holding both groups, its pseudobulk\'s principal components over its one panel; the groups made to follow the slides said to be confounded with them; a planted sample alone found far out, on its transcripts and genes, each with its reason',
            `${qcStandard.message ?? qcStandard.error} | ${qcConfounded.message ?? qcConfounded.error} | ${plantedOpened.message ?? plantedOpened.error} ${plantedSummary.message ?? plantedSummary.error} | ${qcPlanted.message ?? qcPlanted.error}`,
            qcStandard.ok && qs.samples?.length === 8 && qs.outliers?.length === 0 && qs.metrics?.length === 8 && (qs.batches ?? []).filter((x) => x.kind === 'slide').length === 2 && (qs.batches ?? []).filter((x) => x.kind === 'panel').length === 1
            && qs.design?.statements?.length === 0 && qs.panels?.panels?.length === 1 && qs.panels.shared === 120 && variance.length === 7 && variance.every((v, k) => v > 0 && (k === 0 || v <= variance[k - 1])) && variance.reduce((x, v) => x + v, 0) <= 1 + 1e-9
            && qs.samples.every((x) => x.qc.nucleusShare > 0 && x.qc.medianNucleusAreaUm2 > 0 && x.pca?.length === 7)
            && qcConfounded.ok && (qcConfounded.data?.design?.statements ?? []).some((st) => st.kind === 'confounded' && /condition is confounded with the slide/.test(st.text))
            && plantedOpened.ok && plantedSummary.ok && planted.join() === 'S06' && far.length >= 2 && far.every((o) => o.name === 'S06' && o.z < 0)
            && ['medianTranscripts', 'transcriptsPer100um2'].every((m) => far.some((o) => o.metric === m && /times the samples' median/.test(o.reason))),
            'none far out; 2 slides, 1 panel of 120 genes; 7 components; confounded when the groups follow the slides; S06 alone far out, below, on its median transcripts and transcripts per 100 µm²');

          // Differential abundance (wave 7, slice 6): the standard cohort's subjects compared by
          // condition (each subject a sample: unpaired); each label's share and density effect the
          // pooled ratio of its counts (as a model of the groups alone gives), its interval covering
          // the ratio the true cells show; a sample's cells of a label shown on its tissue, as many as
          // the summary counts. A null cohort labeled from its own single-cell reference: its shares
          // the true types', and nothing found.
          await act('open_project', { project: cohortProject });
          await act('set_samples', { values: Object.fromEntries(cs.map((x) => [x.sample, { subject: x.sample }])) });
          await act('design', { unit: 'subject', group: 'condition', reference: 'control' });
          const da = await act('compare_abundance', { summary: sd.id });
          const daf = Object.fromEntries((da.data?.families ?? []).map((f) => [f.family, f]));
          const groupOf = (name) => (cs.findIndex((x) => x.sample === name) < 4 ? 'control' : 'treated');
          const pooled = (count, total) => {
            const by = { control: [0, 0], treated: [0, 0] };
            for (const x of sd.samples ?? []) {
              const g = by[groupOf(x.name)];
              g[0] += count(x);
              g[1] += total(x);
            }
            return by;
          };
          const daOff = [];
          for (const l of cohortNames) {
            const share = daf.share?.tests.find((t) => t.label === l);
            const dens = daf.density?.tests.find((t) => t.label === l);
            const lab = pooled((x) => x.counts[l], (x) => x.labeled);
            const odds = (g) => g[0] / (g[1] - g[0]);
            const area = pooled((x) => x.counts[l], (x) => x.tissueAreaUm2 / 1e6);
            if (!share || Math.abs(share.ratio - odds(lab.treated) / odds(lab.control)) > 1e-9 * share.ratio) daOff.push(`${l}: odds ratio ${share?.ratio}`);
            if (!dens || Math.abs(dens.ratio - (area.treated[0] / area.treated[1]) / (area.control[0] / area.control[1])) > 1e-9 * dens.ratio) daOff.push(`${l}: density ratio ${dens?.ratio}`);
            // The true cells' pooled ratios (every section the same area).
            const tr = { control: [0, 0], treated: [0, 0] };
            for (const ts of cohortTruth.samples) {
              const g = tr[ts.group];
              g[0] += ts.cells_by_type[l] ?? 0;
              g[1] += Object.values(ts.cells_by_type).reduce((n, v) => n + v, 0);
            }
            const trueOdds = odds(tr.treated) / odds(tr.control);
            const trueDensity = tr.treated[0] / tr.control[0];
            if (!(share?.lo <= trueOdds && trueOdds <= share?.hi)) daOff.push(`${l}: the true odds ratio ${trueOdds.toFixed(3)} outside ${share?.lo}–${share?.hi}`);
            if (!(dens?.lo <= trueDensity && trueDensity <= dens?.hi)) daOff.push(`${l}: the true density ratio ${trueDensity.toFixed(3)} outside ${dens?.lo}–${dens?.hi}`);
          }
          const endo = daf.share?.tests.find((t) => t.label === 'endothelial-like');
          const shown = await act('show_sample_cells', { summary: sd.id, sample: 'S05', label: 'endothelial-like' });
          const s05 = (sd.samples ?? []).find((x) => x.name === 'S05');
          const nullDir = join(work, 'null-cohort');
          rmSync(nullDir, { recursive: true, force: true });
          execFileSync(bin, ['simulate', '--cohort', 'null', '--reference', '300', '--quiet', nullDir]);
          const nullTruth = JSON.parse(readFileSync(join(nullDir, 'truth', 'cohort.json'), 'utf8'));
          await act('new_project', { name: 'A null cohort' });
          await act('open_paths', { paths: [nullDir], add: true });
          await act('set_samples', { table: readFileSync(join(nullDir, 'samples.csv'), 'utf8') });
          await act('design', { unit: 'subject', group: 'condition', reference: 'control' });
          const refSteps = await act('cohort_steps', { questions: [], reference: join(nullDir, 'reference.zarr') });
          const refSummary = await act('summarize_samples', { reference: join(nullDir, 'reference.zarr') });
          const nullDA = await act('compare_abundance', { families: ['share', 'density', 'niche'] });
          const refShares = (refSummary.data?.samples ?? []).map((x, k) => {
            const ts = nullTruth.samples[k];
            const total = Object.values(ts.cells_by_type).reduce((n, v) => n + v, 0);
            return Math.max(...Object.entries(x.counts).map(([l, v]) => Math.abs(v / x.labeled - (ts.cells_by_type[l] ?? 0) / total)));
          });
          const nullTests = (nullDA.data?.families ?? []).flatMap((f) => f.tests.filter((t) => t.padj !== undefined));
          check('viewer', 'differential abundance: each label\'s share and density compared by condition, each effect the pooled ratio of its counts and its interval covering the true cells\' ratio; a sample\'s cells of a label shown on its tissue as the summary counts them; a null cohort labeled from its own reference, its shares the true types\', and nothing found',
            `${da.message ?? da.error} | off: ${daOff.join('; ') || 'none'} | ${shown.message ?? shown.error} | ${refSteps.message ?? refSteps.error} ${refSummary.message ?? refSummary.error} | shares off the truth by ${Math.max(...refShares).toFixed(4)} at most | ${nullDA.message ?? nullDA.error}`,
            da.ok && daf.share?.tests.length === 4 && daf.density?.tests.length === 4 && daOff.length === 0 && endo?.ratio > 1 && endo?.df === 6
            && shown.ok && shown.data.cells === s05?.counts['endothelial-like']
            && refSteps.ok && refSteps.data.reference?.labelColumn === 'cell_type' && refSummary.ok && refShares.length === 8 && Math.max(...refShares) < 0.08
            && nullDA.ok && nullTests.length === 14 && nullTests.every((t) => t.padj >= 0.05),
            'odds and density ratios their pooled counts\', every interval covering the truth; S05\'s endothelial cells the summary\'s; reference labels within 0.08 of the truth; the null cohort\'s 14 tests none below 0.05');

          // Differential expression and spatial statistics between groups (wave 7, slice 7): the null
          // cohort (open now) finds no gene below an adjusted p of 0.05, and its spatial statistics,
          // which its steps did not ask for, cannot be compared. The
          // standard cohort's subjects compared by condition: SIM033 higher in the epithelial-like
          // cells, its log2 fold's interval covering the one the true cells show, and few other
          // genes found; the gene's values by unit; the spatial statistics of every family, the
          // epithelial-like and immune-like cells' neighbor enrichment higher in the treated subjects
          // (where the simulator mixes immune-like cells into epithelial-like domains), its interval
          // above 1.
          const nullDE = await act('compare_expression', {});
          const nullSpatial = await act('compare_spatial', {});
          const nullFound = (nullDE.data?.labels ?? []).flatMap((l) => (l.contrasts ?? []).flatMap((c) => c.found));
          await act('open_project', { project: cohortProject });
          const de = await act('compare_expression', { summary: sd.id });
          const epi = (de.data?.labels ?? []).find((l) => l.label === 'epithelial-like');
          const sim033 = epi?.contrasts?.[0]?.found.find((x) => x.gene === 'SIM033');
          const trueLevel = (cohortTruth.effects ?? []).find((e) => e.kind === 'level' && e.gene === 'SIM033');
          const trueLog2 = trueLevel ? trueLevel.measured_log_fold / Math.LN2 : NaN;
          const others = (de.data?.labels ?? []).flatMap((l) => (l.contrasts ?? []).flatMap((c) => c.found.filter((x) => !(l.label === 'epithelial-like' && x.gene === 'SIM033'))));
          const gene = await act('show_gene', { gene: 'SIM033', label: 'epithelial-like' });
          const byGroup = (g) => (gene.data?.rows ?? []).filter((r) => r.group === g).map((r) => r.logCPM);
          const mean = (v) => v.reduce((a, x) => a + x, 0) / Math.max(1, v.length);
          const sp = await act('compare_spatial', { summary: sd.id });
          const spf = Object.fromEntries((sp.data?.families ?? []).map((f) => [f.family, f]));
          const mixed = spf.enrichment?.tests.find((t) => t.key === 'epithelial-like|immune-like');
          check('viewer', 'differential expression and spatial statistics between groups: the null cohort finds nothing (and has no spatial statistics to compare); the standard cohort finds SIM033 higher in the epithelial-like cells, its interval covering the true cells\' fold, few other genes; the gene\'s values by unit; every family of spatial statistics compared, the epithelial-like and immune-like cells\' enrichment higher where the simulator mixes them',
            `${nullDE.message ?? nullDE.error} | ${nullSpatial.message ?? nullSpatial.error} | ${de.message ?? de.error} | true log2 fold ${trueLog2.toFixed(3)} | ${gene.message ?? gene.error} | ${sp.message ?? sp.error} | enrichment ${JSON.stringify(mixed)}`,
            nullDE.ok && nullDE.data.labels.length === 4 && nullFound.length === 0 && !nullSpatial.ok && /no spatial statistics/.test(nullSpatial.message ?? nullSpatial.error ?? '')
            && de.ok && de.data.genes === 120 && de.data.labels.every((l) => l.rows?.length === 8 && l.genes > 80) && sim033?.logFC > 0 && sim033.padj < 0.05 && sim033.lo <= trueLog2 && trueLog2 <= sim033.hi && others.length <= 1
            && gene.ok && gene.data.rows.length === 8 && mean(byGroup('treated')) > mean(byGroup('control'))
            && sp.ok && Object.keys(spf).sort().join() === 'enrichment,moran,nearest' && spf.enrichment.tests.length === 10 && spf.moran.tests.length === 120 && mixed?.ratio > 1 && mixed.ratioLo > 1,
            'the null cohort nothing below 0.05, its spatial statistics refused; SIM033 found in the epithelial-like cells, its interval covering the truth, at most one other gene; treated above control; three families, the epithelial–immune enrichment above 1');

          // A figure of the cohort's comparisons (wave 8, slice 2): the labels' shares by condition
          // (each estimate with its interval on a log axis), the epithelial-like cells' volcano,
          // SIM033 by group and the neighbor enrichments by condition, written as SVG and read by
          // the suite: a row each label, its estimate where its ratio is on the axis's log scale,
          // those found red; a point each gene tested, those found red and SIM033 named; a dot each
          // unit; no text below 6 pt.
          {
            const made = await act('figure', { name: 'The cohort compared', size: 'double', columns: 2, height_mm: 160, panels: [{ kind: 'cohort', comparison: da.data?.id, plot: 'forest', family: 'share' }, { kind: 'cohort', comparison: de.data?.id, plot: 'volcano', label: 'epithelial-like' }, { kind: 'cohort', comparison: de.data?.id, plot: 'gene', label: 'epithelial-like', gene: 'SIM033' }, { kind: 'cohort', comparison: sp.data?.id, plot: 'forest', family: 'enrichment' }] });
            const svgPath = join(work, 'cohort.svg');
            const wrote = await act('write_figure', { name: 'The cohort compared', path: svgPath, format: 'svg', overwrite: true });
            const svg = existsSync(svgPath) ? readFileSync(svgPath, 'utf8') : '';
            const abu = await (await fetch(`${url}/api/artifacts/${encodeURIComponent(da.data?.id)}/files/abundance.json`)).json();
            const share = abu.families.find((f) => f.family === 'share');
            const contrast = abu.design.contrasts[0].name;
            const tests = share.tests.filter((t) => t.contrast === contrast);
            const forest = [...figurePanelSvg(svg, 'A').matchAll(/<rect x="([\d.]+)" y="([\d.]+)" width="([\d.]+)" height="([\d.]+)" fill="(#171b26|#d63b4a)"\/>/g)].filter((m) => m[3] === m[4]).map((m) => ({ x: Number(m[1]) + Number(m[3]) / 2, red: m[5] === '#d63b4a' }));
            // On a log axis, the estimates' places differ as their logs do.
            const tested = tests.filter((t) => t.tested);
            const slope = forest.length >= 2 ? (forest[1].x - forest[0].x) / (Math.log(tested[1].ratio) - Math.log(tested[0].ratio)) : NaN;
            const placesOff = forest.filter((f, i) => Math.abs(f.x - forest[0].x - slope * (Math.log(tested[i].ratio) - Math.log(tested[0].ratio))) > 0.05).length;
            const ex = await (await fetch(`${url}/api/artifacts/${encodeURIComponent(de.data?.id)}/files/expression.json`)).json();
            const epiResult = ex.results.find((r) => r.label === 'epithelial-like');
            const epiContrast = epiResult.contrasts.find((c) => c.contrast === contrast) ?? epiResult.contrasts[0];
            const genesFound = epiContrast.padj.filter((p) => p < 0.05).length;
            const volcano = figurePanelSvg(svg, 'B');
            const vPoints = (volcano.match(/<circle /g) ?? []).length;
            const vRed = (volcano.match(/<circle [^>]*fill="#d63b4a"/g) ?? []).length;
            const dots = (figurePanelSvg(svg, 'C').match(/<circle /g) ?? []).length;
            const spDoc = await (await fetch(`${url}/api/artifacts/${encodeURIComponent(sp.data?.id)}/files/spatial-tests.json`)).json();
            const enr = spDoc.families.find((f) => f.family === 'enrichment').tests.filter((t) => t.contrast === contrast);
            const spRows = [...figurePanelSvg(svg, 'D').matchAll(/<rect x="[\d.]+" y="[\d.]+" width="([\d.]+)" height="([\d.]+)" fill="(#171b26|#d63b4a)"\/>/g)].filter((m) => m[1] === m[2]);
            const sizes = [...svg.matchAll(/font-size="([\d.]+)"/g)].map((m) => Number(m[1]));
            check('viewer', 'a figure of a cohort\'s comparisons: each label\'s share by condition on a log axis, those found red; the epithelial-like cells\' volcano a point each gene tested, those found red and SIM033 named; SIM033 a dot each unit; the enrichments by condition; no text below 6 pt', `${made.ok ? made.message.slice(0, 200) : made.message}; ${wrote.ok ? 'written' : wrote.message}; forest ${forest.length} of ${tests.length} labels, ${placesOff} places off the log scale, ${forest.filter((f) => f.red).length} red of ${tests.filter((t) => t.padj < 0.05).length} found; volcano ${vPoints} points of ${epiResult.genes.length}, ${vRed} red of ${genesFound}; ${dots} units; enrichment ${spRows.length} rows of ${enr.length}, ${spRows.filter((m) => m[3] === '#d63b4a').length} red of ${enr.filter((t) => t.padj < 0.05).length}; text ${Math.min(...sizes)} pt or more`,
              made.ok && wrote.ok && forest.length === tested.length && tested.length === 4 && placesOff === 0 && forest.filter((f) => f.red).length === tests.filter((t) => t.padj < 0.05).length
              && vPoints === epiResult.genes.length && vRed === genesFound && /<text[^>]*>SIM033<\/text>/.test(volcano) && dots === 8
              && spRows.length >= 5 && spRows.length <= enr.length && spRows.filter((m) => m[3] === '#d63b4a').length === enr.slice(0, spRows.length).filter((t) => t.padj < 0.05).length && Math.min(...sizes) >= 6,
              'four labels on the log scale, the found red; a point each gene, the found red, SIM033 named; 8 units; the enrichments; 6 pt or more');
          }

          // What made a cohort's result (wave 8, slice 3): the record of the expression comparison,
          // from the eight bundles through each sample's segmentation, clustering, labels and graph
          // (found in the summary's job, as the program kept it), the design and the summary, to the
          // comparison; the eight samples' steps written once in the Markdown; the methods' papers
          // (muscat, edgeR's quasi-likelihood, TMM, voom, limma, Benjamini–Hochberg) cited.
          {
            const asked = await act('record', { result: de.data?.id, format: 'markdown' });
            const rec = asked.data;
            const f = recordFacts(rec, asked.data?.text ?? '');
            const count = (kind) => f.kinds.filter((k) => k === kind).length;
            const summary = rec?.steps.find((x) => x.kind === 'cohort-summary');
            const fromSummary = new Set((summary?.inputs ?? []).map((n) => rec.steps[n - 1].kind));
            const want = ['crowell2020', 'chen2016', 'robinson2010', 'law2014', 'smyth2004', 'benjamini1995'];
            check('viewer', 'what made a cohort\'s result: the expression comparison\'s record, from the eight bundles through each sample\'s segmentation, clustering, labels and graph, the design and the summary; the samples\' steps written once; the methods\' papers cited', `${asked.ok ? asked.message.slice(0, 160) : asked.message}; ${['dataset', 'segmentation', 'clustering', 'labels', 'graph', 'design', 'cohort-summary', 'expression'].map((k) => `${k} ${count(k)}`).join(', ')}; the summary from ${[...fromSummary].join(', ')}; ${f.ordered ? 'in order' : 'out of order'}; ${want.filter((k) => !f.keys.includes(k)).join(', ') || 'every method\'s paper'} ${want.some((k) => !f.keys.includes(k)) ? 'missing' : 'cited'}`,
              asked.ok && f.ordered && f.numbered && count('dataset') === 8 && count('segmentation') === 8 && count('labels') === 8 && count('graph') >= 8 && count('design') === 1 && count('cohort-summary') === 1 && f.kinds.at(-1) === 'expression'
              && ['dataset', 'segmentation', 'labels', 'graph'].every((k) => fromSummary.has(k)) && /\*\*Xenium output bundles, one for each of 8:\*\*/.test(asked.data.text) && want.every((k) => f.keys.includes(k)) && f.mdRefs === rec.references.length,
              'eight of each, then the design, the summary and the comparison, in order; the bundles written once; every method\'s paper');
          }

          // The methods text of a cohort's result (wave 8, slice 4): the expression comparison's
          // methods say the eight simulated sections once, each sample's same steps once, the design
          // in its words, the summary, the comparison's steps and the genes it found, with their
          // log2 fold changes the comparison's own; and the whole project's methods by topic.
          {
            const asked = await act('methods', { result: de.data?.id });
            const text = asked.data?.text ?? '';
            const exp = await chrome.eval(`(() => window.situcraft.store.project.artifacts.find((a) => a.id === ${JSON.stringify(de.data?.id)})?.summaryOf ?? null)()`);
            const found = (exp?.found ?? []).slice(0, 3).map((f) => `${f.gene} (${f.label}; ${f.contrast}; log2 fold change ${f.logFC >= 0 ? '+' : ''}${f.logFC.toFixed(2)}`);
            const mf = methodsFacts(text, asked.data?.references);
            const whole = await act('methods', { project: true });
            const topics = (whole.data?.paragraphs ?? []).map((x) => x.topic);
            check('viewer', 'the methods of a cohort\'s result: the eight sections said once, each sample\'s same steps once, the design in its words, the summary and the comparison, the genes found with the comparison\'s fold changes; numbered as first cited; the project\'s methods by topic', `${asked.ok ? `${(asked.data.paragraphs ?? []).map((x) => x.topic).join(', ')}; ${asked.data.references.length} references` : asked.message}; found ${found.filter((f) => text.includes(f)).length} of ${found.length} as the comparison has them; ${mf.numbered ? 'numbered as cited' : 'not numbered as cited'}; the project: ${whole.ok ? topics.join(', ') : whole.message}`,
              asked.ok && /The 8 sections \(S01, .* and S08\) were simulated by SituCraft/.test(text) && /The cells of each section were clustered/.test(text) && /were compared by the design "/.test(text) && /Each sample was summarized/.test(text) && /Each label's pseudobulk was compared between the groups/.test(text)
              && found.length > 0 && found.every((f) => text.includes(f)) && mf.numbered && whole.ok && ['data', 'cells', 'spatial', 'cohort', 'figures', 'software'].every((t) => topics.includes(t)) && methodsFacts(whole.data.text, whole.data.references).numbered,
              'the sections once, each step once; the genes found as the comparison has them; numbered as cited; the project by topic');
          }

          // Worked examples on the simulated cohorts (wave 7, slice 9), each run step by step and
          // every number its cards state checked against the comparison's own document: a gene
          // changed in one cell type (the standard cohort, open now), a comparison where nothing
          // differs (the null cohort), and a composition shift found and each subject compared with
          // itself (the cohort before and after, its true cells' odds the suite's own from its truth).
          const walkCohort = async (example, name) => {
            const steps = [await act('tour', { example, name })];
            for (let k = 0; k < 20 && steps.at(-1).ok && !steps.at(-1).data?.finished; k += 1) steps.push(await act('tour', { run: true }));
            await act('tour', { end: true });
            return steps;
          };
          const walked = (steps) => steps.every((x) => x.ok) && steps.at(-1)?.data?.finished === true;
          const shownAt = (steps, n) => steps.find((x) => x.ok && x.data?.step === n)?.data?.cohort?.comparison ?? null;
          const artifactDoc = async (id, file) => (id ? json(`${url}/api/artifacts/${encodeURIComponent(id)}/files/${file}`) : null);
          const geneOf = (e, label, gene) => {
            const r = e?.results.find((x) => x.label === label);
            const i = r?.genes?.findIndex((g) => e.genes[g] === gene) ?? -1;
            return i >= 0 ? { logFC: r.contrasts[0].logFC[i], lo: r.contrasts[0].lo[i], hi: r.contrasts[0].hi[i], padj: r.contrasts[0].padj[i] } : null;
          };
          const foundOf = (e) => (e?.results ?? []).flatMap((r) => (r.contrasts ?? []).flatMap((c) => c.padj.map((q, i) => [q, `${r.label} ${e.genes[r.genes[i]]}`]).filter(([q]) => q < 0.05).map(([, k]) => k)));
          const r2 = (v) => Math.round(100 * v) / 100;
          const geneSteps = await walkCohort('simulated-cohort', 'cohort-gene');
          const geneDoc = await artifactDoc(shownAt(geneSteps, 6), 'expression.json');
          const sim = geneOf(geneDoc, 'epithelial-like', 'SIM033');
          check('viewer', 'the worked example A gene changed in one cell type: the standard cohort\'s subjects compared by condition, SIM033 the only gene found, in the epithelial-like cells, as its card states', `${geneSteps.map((x) => (x.ok ? `step ${x.data?.step ?? 'finished'}` : x.message)).join('; ')}; ${JSON.stringify({ tests: geneDoc?.tests, found: foundOf(geneDoc), sim })}`,
            walked(geneSteps) && geneDoc?.tests === 424 && foundOf(geneDoc).join() === 'epithelial-like SIM033' && r2(sim.logFC) === 0.94 && r2(sim.lo) === 0.59 && r2(sim.hi) === 1.3 && sim.padj < 0.005,
            '424 tests; SIM033 +0.94 log2 (0.59 to 1.30), adjusted p 0.005, alone');
          await act('open_example', { example: 'simulated-null-cohort' });
          const nullWalk = await walkCohort('simulated-null-cohort', 'cohort-null');
          const nullAbu = await artifactDoc(shownAt(nullWalk, 6), 'abundance.json');
          const nullExp = await artifactDoc(shownAt(nullWalk, 8), 'expression.json');
          const nullSpt = await artifactDoc(shownAt(nullWalk, 10), 'spatial-tests.json');
          const abuTests = (nullAbu?.families ?? []).flatMap((f) => f.tests.filter((t) => t.tested));
          const expP = (nullExp?.results ?? []).flatMap((r) => (r.contrasts ?? []).flatMap((c) => c.p.map((p, i) => ({ p, padj: c.padj[i], key: `${r.label} ${nullExp.genes[r.genes[i]]}` }))));
          const lowest = [...expP].sort((a, b) => a.p - b.p)[0];
          const sptTests = (nullSpt?.families ?? []).flatMap((f) => f.tests.filter((t) => t.tested));
          check('viewer', 'the worked example A comparison where nothing differs: the null cohort\'s abundance, expression and arrangement, nothing found, as its cards state', `${nullWalk.map((x) => (x.ok ? `step ${x.data?.step ?? 'finished'}` : x.message)).join('; ')}; abundance ${abuTests.length} tests, lowest p ${Math.min(...abuTests.map((t) => t.p))}; expression ${expP.length} tests, ${expP.filter((x) => x.p < 0.05).length} below 0.05, lowest ${JSON.stringify(lowest)}; enrichment ${sptTests.length}, lowest adjusted ${Math.min(...sptTests.map((t) => t.padj))}`,
            walked(nullWalk) && abuTests.length === 14 && abuTests.every((t) => t.p >= 0.05) && expP.length === 423 && expP.filter((x) => x.p < 0.05).length === 11 && expP.every((x) => x.padj >= 0.05) && lowest?.key === 'epithelial-like SIM015' && r2(lowest.padj) === 0.25
            && sptTests.length === 10 && sptTests.every((t) => t.padj >= 0.05) && r2(Math.min(...sptTests.map((t) => t.padj))) === 0.36,
            '14 abundance tests, none below 0.05; 423 of expression, 11 raw below 0.05, SIM015 the lowest, adjusted 0.25; 10 of enrichment, the lowest adjusted 0.36');
          await act('open_example', { example: 'simulated-paired-cohort' });
          const pairedDir = (await json(`${url}/api/examples`)).examples.find((e) => e.name === 'simulated-paired-cohort')?.path;
          const pairedTruth = pairedDir ? JSON.parse(readFileSync(join(pairedDir, 'truth', 'cohort.json'), 'utf8')) : null;
          const trueOdds = (type) => {
            const by = { before: [0, 0], after: [0, 0] };
            for (const t of pairedTruth?.samples ?? []) {
              by[t.group][0] += t.cells_by_type[type] ?? 0;
              by[t.group][1] += Object.values(t.cells_by_type).reduce((n, v) => n + v, 0);
            }
            const odds = (g) => g[0] / (g[1] - g[0]);
            return odds(by.after) / odds(by.before);
          };
          const shiftWalk = await walkCohort('simulated-paired-cohort', 'cohort-shift');
          const shiftAbu = await artifactDoc(shownAt(shiftWalk, 6), 'abundance.json');
          const testOf = (doc, family, label) => doc?.families.find((f) => f.family === family)?.tests.find((t) => t.label === label);
          const endoShare = testOf(shiftAbu, 'share', 'endothelial-like'), epiShare = testOf(shiftAbu, 'share', 'epithelial-like'), endoD = testOf(shiftAbu, 'density', 'endothelial-like');
          check('viewer', 'the worked example A composition shift found: the cohort before and after compared by a paired design, more endothelial-like cells and fewer epithelial-like ones, near the true cells\' odds, as its cards state', `${shiftWalk.map((x) => (x.ok ? `step ${x.data?.step ?? 'finished'}` : x.message)).join('; ')}; ${JSON.stringify({ endoShare, epiShare, endoD })}; true ${r2(trueOdds('endothelial-like'))} and ${r2(trueOdds('epithelial-like'))}`,
            walked(shiftWalk) && r2(endoShare?.ratio) === 2.19 && r2(endoShare.lo) === 1.54 && r2(endoShare.hi) === 3.12 && endoShare.padj < 0.01 && r2(epiShare?.ratio) === 0.48 && r2(epiShare.lo) === 0.27 && r2(epiShare.hi) === 0.84 && epiShare.padj < 0.05
            && r2(endoD?.ratio) === 1.8 && r2(endoD.lo) === 1.39 && r2(endoD.hi) === 2.33 && r2(trueOdds('endothelial-like')) === 2.26 && r2(trueOdds('epithelial-like')) === 0.47,
            'endothelial-like 2.19× (1.54 to 3.12), density 1.80× (1.39 to 2.33); epithelial-like 0.48× (0.27 to 0.84); the true cells 2.26 and 0.47');
          const pairWalk = await walkCohort('simulated-paired-cohort', 'cohort-paired');
          const sectionsExp = await artifactDoc(shownAt(pairWalk, 7), 'expression.json');
          const pairedExp = await artifactDoc(shownAt(pairWalk, 10), 'expression.json');
          const s1 = geneOf(sectionsExp, 'epithelial-like', 'SIM033'), s2 = geneOf(pairedExp, 'epithelial-like', 'SIM033');
          const pairedFound = foundOf(pairedExp);
          check('viewer', 'the worked example Each subject compared with itself: SIM033 with the sections as units and paired, its interval narrower paired, and the endothelial-like markers in their neighbors found, as its cards state', `${pairWalk.map((x) => (x.ok ? `step ${x.data?.step ?? 'finished'}` : x.message)).join('; ')}; sections ${JSON.stringify(s1)} (${foundOf(sectionsExp).length} found); paired ${JSON.stringify(s2)} (${pairedFound.length} found: ${pairedFound.filter((k) => k.startsWith('epithelial-like')).join(', ')})`,
            walked(pairWalk) && r2(s1?.logFC) === 1.08 && r2(s1.lo) === 0.75 && r2(s1.hi) === 1.41 && foundOf(sectionsExp).join() === 'epithelial-like SIM033'
            && r2(s2?.logFC) === 1.08 && r2(s2.lo) === 0.84 && r2(s2.hi) === 1.31 && pairedFound.length === 22 && ['SIM025', 'SIM026', 'SIM027', 'SIM028', 'SIM029', 'SIM030'].every((g) => pairedFound.includes(`epithelial-like ${g}`)) && pairedExp.design.names.length === 7,
            'sections: +1.08 log2 (0.75 to 1.41), only SIM033; paired: (0.84 to 1.31), 22 genes, the endothelial-like markers SIM025 to SIM030 among them');

          // The project as 0.7.0 saves a cohort (validation/projects/0.7.0-cohort.situcraft.json):
          // the simulated cohort's samples with their subjects and conditions, a saved design, the
          // same steps on every sample, its summary and its comparisons of abundance, expression and
          // arrangement. Written when asked (SITUCRAFT_WRITE_FIXTURE=0.7.0), its folders' places made
          // neutral and given its own id. Then reopened: each sample's bundle found again where this
          // run made it, its samples, fields and design as saved; and the steps, summary and
          // comparisons made again by this build with the same ids, each artifact the project
          // records the same by its summary (a cohort's records keep no checksums; their summaries
          // hold the samples' counts and the comparisons' tests and genes found).
          const cohortFixture = join(root, 'validation', 'projects', '0.7.0-cohort.situcraft.json');
          const cohortHome = (await json(`${url}/api/examples`)).examples.find((e) => e.name === 'simulated-cohort')?.path;
          const cohortFlow = async () => [
            await act('cohort_steps', { questions: ['enrichment', 'distances', 'moran'] }),
            await act('summarize_samples', {}),
            await act('compare_abundance', { families: ['share', 'density', 'niche'], design: 'By condition' }),
            await act('compare_expression', { design: 'By condition' }),
            await act('compare_spatial', { design: 'By condition' }),
          ];
          if (process.env.SITUCRAFT_WRITE_FIXTURE === '0.7.0' && cohortHome) {
            await act('new_project', { name: 'Simulated cohort, as SituCraft 0.7.0 saved it' });
            await act('open_example', { example: 'cohort' });
            await act('design', { unit: 'subject', group: 'condition', reference: 'control', save: 'By condition' });
            await cohortFlow();
            const doc = await chrome.eval(`(async () => (await import('/lib/project.js')).serializeProject(window.situcraft.store.project))()`);
            const neutral = JSON.parse(doc.split(cohortHome).join('/data/examples/simulated-cohort').split(work).join('/data/work'));
            writeFileSync(cohortFixture, `${JSON.stringify({ ...neutral, id: 'p-situcraft-0-7-0-cohort' }, null, 1)}\n`);
          }
          // Reopened against the cohort as 0.7.0 made it: a simulated bundle records the version that
          // wrote it, so the cohort this version makes is another run; the same simulator, told to
          // record 0.7.0 (SITUCRAFT_SIMULATE_AS), makes 0.7.0's bundles again exactly.
          const cohort070 = join(work, 'cohort-0.7.0');
          if (existsSync(cohortFixture) && !existsSync(cohort070)) spawnSync(bin, ['simulate', '--cohort', 'standard', '--quiet', cohort070], { encoding: 'utf8', timeout: 30 * 60e3, env: { ...process.env, SITUCRAFT_SIMULATE_AS: '0.7.0' } });
          if (existsSync(cohortFixture) && existsSync(cohort070)) {
            const text = readFileSync(cohortFixture, 'utf8');
            const fixture = JSON.parse(text);
            const reopened = await chrome.eval(`(async () => {
              const app = window.situcraft;
              const { samplesOf } = await import('/lib/samples.js');
              const { designsOf } = await import('/lib/design.js');
              const doc = ${JSON.stringify(text)};
              const saved = JSON.parse(doc);
              await app.library.saveProject(saved.id, doc);
              await app.openProject(saved.id);
              const states = [];
              for (const d of app.store.project.datasets) {
                const link = await app.bundles.locate(d.id, ${JSON.stringify(cohort070)} + '/' + d.path.split('/').pop());
                states.push(link?.state);
                // Its instrument's segmentation registered, as opening it registers it.
                if (link?.item) await app.bundles.registerSegmentation(link.item, d.id);
              }
              const p = app.store.project;
              return { states, samples: samplesOf(p).map((x) => [x.name, x.fields.subject, x.fields.condition].join(' ')).join(', '), designs: designsOf(p).map((d) => d.name + ' v' + d.version).join() };
            })()`);
            const flow = await cohortFlow();
            // A comparison's record keeps its summary's id as summary and the manifest's as summaryOf.
            const kinds = new Set(['cohort-summary', 'abundance', 'expression', 'spatial-comparison']);
            const perSample = (a) => ['clustering', 'labels', 'graph', 'spatial'].includes(a.kind) && a.state === 'complete' && !a.source && !a.reference;
            const made = fixture.artifacts.filter((a) => kinds.has(a.kind) || perSample(a));
            const differing = [];
            for (const a of made) {
              const m = await fetch(`${url}/api/artifacts/${encodeURIComponent(a.id)}`).then((r) => (r.ok ? r.json() : null));
              if (!m) differing.push(`${a.name}: not made again`);
              else if (JSON.stringify(m.summary) !== JSON.stringify(a.summaryOf ?? a.summary)) differing.push(`${a.name}: another summary`);
            }
            // The summary and comparisons made now are the fixture's, by their ids.
            const ids = flow.slice(1).map((x) => x.data?.id);
            check('viewer', '0.7.0 cohort project: it opens and finds its moved bundles, its samples and design as saved; its steps, summary and comparisons made again the same', `${JSON.stringify(reopened)}; ${flow.map((x) => (x.ok ? x.data?.id ?? 'ok' : x.message)).join('; ')}; ${made.length} artifacts${differing.length ? `; ${differing.slice(0, 4).join('; ')}` : ', all the same'}`,
              reopened.states.length === 8 && reopened.states.every((st) => st === 'ok') && /S01 S01 control/.test(reopened.samples) && reopened.designs === 'By condition v1'
              && flow.every((x) => x.ok) && ids.length === 4 && ids.every((id) => fixture.artifacts.some((a) => a.id === id && kinds.has(a.kind))) && made.filter((a) => kinds.has(a.kind)).length === 4 && made.length === 52 && differing.length === 0,
              '8 bundles found; S01 a control subject; By condition v1; the summary and three comparisons, and the samples\' 48 clusterings, label sets, graphs and analyses, the same');
          }

          // The teaching example on the Breast section (wave 7, slice 9), in a project of its own:
          // six regions as samples, each its own subject, refused; marked pseudo-samples, compared,
          // every result saying so; the places' differences as its cards state.
          if (existsSync(join(dataRoot, 'xenium-breast-2fov', 'experiment.xenium'))) {
            await act('new_project', { name: 'Places in one section' });
            await act('open_paths', { paths: [join(dataRoot, 'xenium-breast-2fov')], add: true });
            await chrome.eval(`(async () => {
              const app = window.situcraft;
              const { guideFor } = await import('/lib/tours.js');
              const { startTour } = await import('/ui/tour.js');
              const dataset = app.store.project.datasets.find((d) => d.name === 'xenium-breast-2fov');
              for (let k = 0; k < 600 && !app.store.project.artifacts.some((a) => a.kind === 'segmentation' && a.dataset === dataset.id); k += 1) await new Promise((r) => setTimeout(r, 100));
              startTour(app, { tour: guideFor('xenium-breast-2fov', 'pseudo-samples'), dataset: dataset.id });
              return true;
            })()`);
            const teach = [];
            for (let k = 0; k < 20; k += 1) {
              const x = await act('tour', { run: true });
              teach.push(x);
              if (!x.ok || x.data?.finished) break;
            }
            const placesAbu = await artifactDoc(teach.find((x) => x.ok && x.data?.step === 10)?.data?.cohort?.comparison, 'abundance.json');
            const refused = await chrome.eval(`(async () => {
              const { makeDesign } = await import('/lib/design.js');
              const d = makeDesign(window.situcraft.store.project, { unit: 'subject', group: 'place', reference: 'tumor' });
              return d.problems.join(' ');
            })()`);
            // The comparison the last step shows, its warning on the page.
            const banner = await chrome.eval(`document.querySelector('.cohort-pseudo-note')?.textContent ?? ''`);
            const immune = testOf(placesAbu, 'share', 'Immune'), epithelial = testOf(placesAbu, 'share', 'Epithelial'), adipocyte = testOf(placesAbu, 'share', 'Adipocyte');
            check('viewer', 'the teaching example Places in one section are not subjects: six Breast regions as samples refused, then marked pseudo-samples and compared, every result saying so, the places\' differences as its cards state', `${teach.map((x) => (x.ok ? `step ${x.data?.step ?? 'finished'}` : x.message)).join('; ')}; refused: ${refused.slice(0, 120)}; ${JSON.stringify({ pseudo: placesAbu?.design?.pseudoSamples, banner: banner.slice(0, 60), immune, epithelial, adipocyte: adipocyte?.note })}`,
              teach.every((x) => x.ok) && teach.at(-1)?.data?.finished === true && /are regions of one section \(xenium-breast-2fov\) given different subjects/.test(refused) && placesAbu?.design?.pseudoSamples === true && placesAbu.design.rows.length === 6 && /^Pseudo-samples, for teaching\. .*not independent replicates/.test(banner)
              && r2(immune?.ratio) === 8.37 && r2(immune.lo) === 1.58 && r2(immune.hi) === 44.49 && r2(immune.padj) === 0.03 && r2(epithelial?.ratio) === 0.04 && adipocyte && !adipocyte.tested && /not estimable/.test(adipocyte.note),
              'refused as subjects; pseudo-samples, 6 rows, the warning shown; immune 8.37× (1.58 to 44.49), adjusted p 0.03; epithelial 0.04×; adipocytes not estimable');
          }
        }
      }

      // Post-Xenium images (wave 5, slice 1): the pathology fixture's bundle (XOA 1.0.1) opened,
      // its H&E added by a program with the alignment found beside it, and drawn where the matrix
      // puts it: at three of its pixels, the Viewer's colour is the image's.
      const pathology = join(dataRoot, 'xenium-breast-cancer-rep1');
      const sample = 'Xenium_FFPE_Human_Breast_Cancer_Rep1';
      const rowsOf = (t) => (u, v) => [t[0][0] * u + t[0][1] * v + t[0][2], t[1][0] * u + t[1][1] * v + t[1][2]];
      // The Viewer's colour where an image's pixel (px, py) is placed by t (a transform's rows, or a
      // function placing a point), against the pixel's own.
      const probe = async (dataset, added, t, px, py) => {
        const [cx, cy] = (typeof t === 'function' ? t : rowsOf(t))(px + 0.5, py + 0.5);
        await act('show_tissue', { dataset, center: [cx, cy], um_per_pixel: 0.002, transcripts: false, outlines: false, channels: { DAPI: { visible: false } }, images: { [added.data.name]: { visible: true, opacity: 1 } } });
        const side = added.data.summary.width > 4096 ? 1024 : 512;
        return chrome.eval(`(async () => {
          const r = window.situcraftViewer.renderer;
          await r.settled();
          const [w, h] = r.size();
          const got = r.readColor(w / 2, h / 2).slice(0, 3);
          const tile = new Uint8Array(await fetch('/api/images/${added.data.id}/tiles/0/0/${Math.floor(px / side)}/${Math.floor(py / side)}').then((x) => x.arrayBuffer()));
          const i = 3 * (${py % side} * ${side} + ${px % side});
          return { pixel: [${px}, ${py}], got, want: [tile[i], tile[i + 1], tile[i + 2]] };
        })()`);
      };
      const worstOf = (probes) => (probes.length ? Math.max(...probes.flatMap((p) => p.got.map((g, k) => Math.abs(g - p.want[k])))) : Infinity);
      if (existsSync(join(pathology, sample, 'experiment.xenium'))) {
        await act('new_project', { name: 'Pathology' });
        const opened = await act('open_paths', { paths: [join(pathology, sample)], add: true });
        const added = await act('add_image', { dataset: sample, path: join(pathology, 'images', `${sample}_he_image.ome.tif`) });
        const probes = [];
        const tenx = added.data?.transform;
        if (added.ok && tenx) {
          for (const [px, py] of [[16342, 14283], [9000, 20000], [24000, 6000]]) probes.push(await probe(sample, added, tenx.summary.transform, px, py));
        }
        check('viewer', 'remote control: add_image adds the pathology fixture\'s H&E with the alignment beside it, and the Viewer draws its pixels where the matrix puts them', `${opened.message ?? opened.error}; ${added.message ?? added.error}; ${JSON.stringify(probes)}`,
          opened.ok && added.ok && tenx?.summary.method === 'matrix' && tenx.summary.reflected && probes.length === 3 && worstOf(probes) <= 2, 'the image\'s color at three of its pixels, within 2');

        // Transforms and landmarks (wave 5, slice 2): landmarks made with 10x's matrix fit back to
        // it as a similarity (10x's is one), recorded as a version that places the image; the
        // Alignment view lists both versions and the landmarks; written as Xenium Explorer's
        // keypoints, which read back in the Xenium image's pixels; the matrix chosen again.
        if (added.ok && tenx) {
          const t = tenx.summary.transform;
          const landmarks = [[2000, 3000], [28000, 2500], [15000, 12000], [5000, 22000], [26000, 21000], [9000, 9000]].map(([u, v]) => ({ fixed: rowsOf(t)(u, v), moving: [u, v] }));
          const aligned = await act('align_image', { dataset: sample, landmarks, model: 'similarity', by: 'validation', note: 'Landmarks made with 10x\'s matrix' });
          const shown = await act('show_alignment', { dataset: sample, transform: aligned.data?.id });
          const ui = await chrome.eval(`({ rows: document.querySelectorAll('.align-landmarks tbody tr').length, versions: document.querySelectorAll('.align-version').length, inUse: document.querySelector('.align-version.in-use b')?.textContent ?? '', fit: document.querySelector('.align-fit')?.innerText ?? '' })`);
          const file = join(work, 'exports', 'he_keypoints.csv');
          mkdirSync(dirname(file), { recursive: true });
          const exported = await act('export', { what: 'alignment', dataset: sample, format: 'keypoints', path: file });
          const lines = existsSync(file) ? readFileSync(file, 'utf8').trim().split('\n') : [];
          const first = lines[1]?.split(',').map(Number) ?? [];
          const back = await act('use_transform', { dataset: sample, transform: tenx.id, rationale: '10x\'s alignment, for the checks that follow' });
          const got = aligned.data?.transform ?? [[0, 0, 0], [0, 0, 0]];
          const worst = Math.max(...[0, 1].flatMap((r) => [0, 1, 2].map((c) => Math.abs(got[r][c] - t[r][c]) / (c === 2 ? 1000 : 1))));
          check('viewer', 'remote control: landmarks made with 10x\'s matrix fit back to it, a version that places the H&E; the Alignment view lists both versions; its keypoints written for Xenium Explorer in its pixels', `${aligned.message ?? aligned.error}; ${shown.message ?? shown.error}; ${JSON.stringify(ui)}; ${exported.message ?? exported.error}; ${lines.slice(0, 2).join(' | ')}; ${back.message ?? back.error}`,
            aligned.ok && aligned.data.fit.rms < 1e-6 && aligned.data.fit.leaveOneOut.length === 6 && worst < 1e-9 && aligned.data.placedBy === aligned.data.id &&
            shown.ok && shown.data.versions.length === 2 && ui.rows === 6 && ui.versions === 2 && /landmarks, similarity/.test(ui.inUse) && /6 landmarks, similarity: residuals 0\.00 µm/.test(ui.fit) &&
            exported.ok && lines[0] === 'fixedX,fixedY,alignmentX,alignmentY' && lines.length === 7 && Math.abs(first[0] * 0.2125 - landmarks[0].fixed[0]) < 1e-6 && first[2] === 2000 && back.ok,
            'the matrix to a millionth of a pixel; 2 versions, 6 rows; 6 keypoints in the Xenium image\'s pixels');
        }
      }

      // Simulated post-Xenium images (wave 5, slice 2): a small simulated tissue with its H&E and
      // immunofluorescence images; the H&E added without an alignment (not drawn), then placed by
      // six landmarks clicked on both images in the Alignment view at places its truth maps (the
      // second image followed to its predicted place from the fourth on), fitted as an affine
      // transform and saved for use; the fit within the truth's deformation of it over the section,
      // and the Viewer's colour at three of its pixels the image's where the fit puts them.
      const simDir = join(work, 'sim-pathology');
      const simSize = [320, 260];
      execFileSync(bin, ['simulate', '--preset', 'small', '--width', String(simSize[0]), '--height', String(simSize[1]), '--post-xenium', '--quiet', simDir]);
      await act('new_project', { name: 'Simulated pathology' });
      const simOpened = await act('open_paths', { paths: [simDir], add: true });
      const simAdded = await act('add_image', { dataset: 'sim-pathology', path: join(simDir, 'post_xenium', 'simulated_he_image.ome.tif'), alignment: null });
      const reg = JSON.parse(readFileSync(join(simDir, 'truth', 'post_xenium', 'registration.json'), 'utf8')).images.find((i) => i.kind === 'he');
      const truth = (u, v) => {
        const [x, y] = rowsOf(reg.transform)(u, v);
        const d = [0, 0];
        for (const w of reg.deformation) d[w.axis] += w.amplitude_um * Math.sin(2 * Math.PI * (w.frequency_per_um[0] * x + w.frequency_per_um[1] * y) + w.phase);
        return [x + d[0], y + d[1]];
      };
      const clicks = [];
      const predictedOff = [];
      let simTF = null;
      let simShown = null;
      if (simAdded.ok) {
        simShown = await act('show_alignment', { dataset: 'sim-pathology' });
        await chrome.eval(`(() => {
          const model = document.querySelector('select[aria-label="What the fit may change"]');
          model.value = 'affine';
          model.dispatchEvent(new Event('change'));
          [...document.querySelectorAll('.align-panel label')].find((l) => l.textContent.includes('Move the images together')).querySelector('input').click();
        })()`);
        // Each place clicked close up (the image there filling the stage), as a person would.
        const clickAt = async (side, x, y, centre = true) => {
          if (centre) await chrome.eval(`window.situcraftAlignment.setCamera('${side}', { cx: ${x}, cy: ${y}, scale: ${side === 'left' ? 8 : 3} })`);
          await chrome.eval('window.situcraftAlignment.settled()');
          const [cx, cy] = await chrome.eval(`window.situcraftAlignment.clientOf('${side}', ${x}, ${y})`);
          await chrome.click(cx, cy);
        };
        for (const [a, b] of [[0.2, 0.2], [0.8, 0.25], [0.5, 0.5], [0.25, 0.8], [0.8, 0.75], [0.55, 0.3]]) {
          const moving = [a * reg.width, b * reg.height];
          const fixed = truth(...moving);
          await clickAt('left', ...fixed);
          // From the fourth landmark on, the image has gone where the fit predicts.
          if (clicks.length >= 3) {
            const cam = await chrome.eval('window.situcraftAlignment.renderers.right.camera');
            predictedOff.push(Math.hypot(cam.cx - moving[0], cam.cy - moving[1]));
          }
          await clickAt('right', ...moving);
          clicks.push({ fixed, moving });
        }
        // Saved: the program fits and records the landmarks, then the version places the image.
        await chrome.eval(`(async () => {
          [...document.querySelectorAll('.align-panel button')].find((b) => b.textContent.includes('Save and use')).click();
          for (let i = 0; i < 300 && !window.situcraftAlignment.state().inUse; i += 1) await new Promise((r) => setTimeout(r, 100));
        })()`);
        const state = await act('get_state');
        const placedBy = state.data?.images?.find((i) => i.dataset === 'sim-pathology')?.placedBy;
        simTF = placedBy ? await fetch(`${url}/api/transforms/${placedBy}`).then((r) => r.json()) : null;
      }
      const simState = await chrome.eval('window.situcraftAlignment?.state()');
      let simWorst = Infinity;
      if (simTF) {
        simWorst = 0;
        for (let v = 0; v < reg.height; v += 25) {
          for (let u = 0; u < reg.width; u += 25) {
            const [tx, ty] = truth(u, v);
            if (tx < 0 || ty < 0 || tx > simSize[0] || ty > simSize[1]) continue;
            const [fx, fy] = rowsOf(simTF.transform)(u, v);
            simWorst = Math.max(simWorst, Math.hypot(fx - tx, fy - ty));
          }
        }
      }
      const simProbes = [];
      if (simTF) for (const [px, py] of [[300, 250], [420, 330], [210, 390]]) simProbes.push(await probe('sim-pathology', simAdded, simTF.transform, px, py));
      const bound = Math.SQRT2 * reg.parameters.deformation_um * 2;
      check('viewer', 'the Alignment view: an image added without an alignment is placed by six landmarks clicked on both images, fitted as an affine transform within its truth\'s deformation, and drawn where the fit puts it', `${simOpened.message ?? simOpened.error}; ${simAdded.message ?? simAdded.error}; ${simShown?.message ?? ''}; predicted ${predictedOff.map((d) => d.toFixed(1)).join(', ')} pixels off; ${simTF ? `${simTF.name}: residuals ${simTF.fit.rms.toFixed(3)} µm, left out ${simTF.fit.looRms.toFixed(3)} µm; at most ${simWorst.toFixed(3)} µm from the truth over the section` : 'not saved'}; ${JSON.stringify(simProbes)}`,
        simOpened.ok && simAdded.ok && simAdded.data.transform === null && simState?.landmarks.length === 6 && simState.landmarks.every((l, i) => Math.hypot(l.fixed[0] - clicks[i].fixed[0], l.fixed[1] - clicks[i].fixed[1]) < 0.2 && Math.hypot(l.moving[0] - clicks[i].moving[0], l.moving[1] - clicks[i].moving[1]) < 0.5) &&
          predictedOff.length === 3 && Math.max(...predictedOff) < 30 && simTF?.model === 'affine' && simTF.method === 'landmarks' && simWorst < bound && simProbes.length === 3 && worstOf(simProbes) <= 2,
        `six landmarks within 0.2 µm and half a pixel of the places clicked; predicted within 30 pixels (the deformation's few µm); the fit within ${bound.toFixed(1)} µm of the truth; the image's color at three pixels, within 2`);

      // Automatic registration (wave 5, slice 3): the same H&E registered by its nuclei with a
      // deformation allowed (and, with the matches a section of this size gives, kept), as a job,
      // the version placing it; within its truth's deformation of the truth, and drawn where the
      // version (its deformation too) puts three of its pixels.
      let autoTF = null;
      const auto = simAdded.ok ? await act('register_image', { dataset: 'sim-pathology', model: 'affine', nonrigid: true, note: 'The viewer suite' }) : null;
      if (auto?.ok) autoTF = await fetch(`${url}/api/transforms/${auto.data.id}`).then((r) => r.json());
      let autoWorst = Infinity;
      const autoProbes = [];
      if (autoTF) {
        autoWorst = 0;
        for (let v = 0; v < reg.height; v += 25) {
          for (let u = 0; u < reg.width; u += 25) {
            const [tx, ty] = truth(u, v);
            if (tx < 0 || ty < 0 || tx > simSize[0] || ty > simSize[1]) continue;
            const [fx, fy] = placePoint(autoTF, u, v);
            autoWorst = Math.max(autoWorst, Math.hypot(fx - tx, fy - ty));
          }
        }
        for (const [px, py] of [[300, 250], [420, 330], [210, 390]]) autoProbes.push(await probe('sim-pathology', simAdded, (x, y) => placePoint(autoTF, x, y), px, py));
      }
      check('viewer', 'remote control: register_image aligns the simulated H&E by its nuclei, a version placing it, within its truth\'s deformation, drawn where it (and any deformation) puts three of its pixels', `${auto?.message ?? auto?.error ?? 'not run'}; at most ${autoWorst.toFixed(3)} µm from the truth over the section; ${JSON.stringify(autoProbes)}`,
        Boolean(auto?.ok) && auto.data.placedBy === auto.data.id && autoTF?.method === 'automatic' && auto.data.deformed && auto.data.kept >= 25 && autoWorst < bound && autoProbes.length === 3 && worstOf(autoProbes) <= 2,
        `placed by it, deformed; within ${bound.toFixed(1)} µm of the truth; the image's color at three pixels, within 2`);

      // Alignment checked in µm (wave 5, slice 4): the six landmarks' version and the automatic one
      // checked by local matches over the tissue; the landmarks' measured errors against the true
      // ones at each patch (the content drawn at a patch's centre c is the image's point the
      // version takes c back to; it belongs where the truth puts that point); the automatic version
      // measured less off; in the Alignment view, the map of where it is off (a square a patch), a
      // checkerboard (at a square of the image, the image's pixel; at one of the stains', not) and
      // DAPI's edges over the image only when asked.
      const versionsNow = await act('get_state');
      const lmId = versionsNow.data?.images?.find((i) => i.dataset === 'sim-pathology')?.alignments.find((x) => /landmarks/.test(x.name))?.id;
      let checkLm = null;
      let checkAuto = null;
      let truthDiff = Infinity;
      let measuredRMS = 0;
      let trueRMS = 0;
      let squares = -1;
      const checker = {};
      let edgeCounts = null;
      if (lmId) {
        checkLm = await act('check_alignment', { dataset: 'sim-pathology', transform: lmId });
        checkAuto = await act('check_alignment', { dataset: 'sim-pathology' });
        if (checkLm.ok) {
          const cd = await fetch(`${url}/api/checks/${checkLm.data.id}`).then((r) => r.json());
          const lm = await fetch(`${url}/api/transforms/${lmId}`).then((r) => r.json());
          const inv = invert(lm.transform);
          let dsq = 0;
          let msq = 0;
          let tsq = 0;
          let n = 0;
          for (const pt of cd.patches.filter((x) => x.matched)) {
            const [qx, qy] = rowsOf(inv)(pt.centre.X, pt.centre.Y);
            const [tx, ty] = truth(qx, qy);
            const ex = pt.centre.X - tx;
            const ey = pt.centre.Y - ty;
            dsq += (pt.error.X - ex) ** 2 + (pt.error.Y - ey) ** 2;
            msq += pt.error.X ** 2 + pt.error.Y ** 2;
            tsq += ex ** 2 + ey ** 2;
            n += 1;
          }
          truthDiff = Math.sqrt(dsq / n);
          measuredRMS = Math.sqrt(msq / n);
          trueRMS = Math.sqrt(tsq / n);
          // The map, and a checkerboard of 20 µm squares: (110, 130) in a square of the image (its
          // cells' indices summing odd), (110, 110) in one of the stains'.
          await act('show_alignment', { dataset: 'sim-pathology', show: lmId, display: 'checkerboard', cell: 20, map: true, edges: false });
          squares = await chrome.eval('window.situcraftAlignment.errorSquares()');
          for (const [key, y] of [['image', [110, 130]], ['stains', [110, 110]]]) {
            const [qx, qy] = rowsOf(inv)(...y);
            const px = Math.floor(qx);
            const py = Math.floor(qy);
            checker[key] = await chrome.eval(`(async () => {
              const a = window.situcraftAlignment;
              a.setCamera('left', { cx: ${rowsOf(lm.transform)(px + 0.5, py + 0.5)[0]}, cy: ${rowsOf(lm.transform)(px + 0.5, py + 0.5)[1]}, scale: 400 });
              await a.settled();
              const [w, h] = a.renderers.left.size();
              const got = a.readColor('left', w / 2, h / 2).slice(0, 3);
              const tile = new Uint8Array(await fetch('/api/images/${simAdded.data.id}/tiles/0/0/${Math.floor(px / 512)}/${Math.floor(py / 512)}').then((x) => x.arrayBuffer()));
              const i = 3 * (${py % 512} * 512 + ${px % 512});
              return { got, image: [tile[i], tile[i + 1], tile[i + 2]] };
            })()`);
          }
          // DAPI's edges, in yellow, over the image at its opacity: some only when asked.
          edgeCounts = {};
          for (const edges of [false, true]) {
            await act('show_alignment', { dataset: 'sim-pathology', show: lmId, display: 'overlay', opacity: 1, map: false, edges });
            edgeCounts[edges] = await chrome.eval(`(async () => {
              const a = window.situcraftAlignment;
              a.setCamera('left', { cx: ${simSize[0] / 2}, cy: ${simSize[1] / 2}, scale: 8 });
              await a.settled();
              return a.countColor('left', [255, 225, 77]);
            })()`);
          }
        }
      }
      const imageSquare = checker.image && Math.max(...checker.image.got.map((g, k) => Math.abs(g - checker.image.image[k]))) <= 2;
      const stainSquare = checker.stains && Math.max(...checker.stains.got.map((g, k) => Math.abs(g - checker.stains.image[k]))) > 20;
      check('viewer', 'an alignment checked in µm: the landmarks\' version measured as its truth says it is off, patch by patch; the automatic one measured less off; its map, a checkerboard and DAPI\'s edges drawn in the Alignment view', `${checkLm?.message ?? checkLm?.error ?? 'no landmarks version'}; measured ${measuredRMS.toFixed(3)} µm root-mean-square, truly ${trueRMS.toFixed(3)}, each patch's ${truthDiff.toFixed(3)} µm from its true error; ${checkAuto?.message ?? checkAuto?.error}; ${squares} squares; checkerboard ${JSON.stringify(checker)}; edges ${JSON.stringify(edgeCounts)}`,
        Boolean(checkLm?.ok && checkAuto?.ok) && truthDiff < 0.5 && Math.abs(measuredRMS - trueRMS) < 0.4 && checkAuto.data.median < checkLm.data.median && squares === checkLm.data.checked && imageSquare && stainSquare && edgeCounts?.false?.n === 0 && edgeCounts?.true?.n > edgeCounts.true.of / 1000,
        'each patch within 0.5 µm of its true error; the automatic version less off; a square a patch; the image in its squares, the stains in theirs; edges only when asked');

      // Regions (wave 5, slice 5). Drawn by hand in the Viewer with each tool where the pointer
      // dragged and clicked; drawn by a program in µm and in the H&E's pixels. Each one's cells (by
      // centroid) against the suite's own count: a winding number (Hormann & Agathos 2001) over the
      // cells' centroids, of the regions as written in µm. Cells chosen in one; one erased from and
      // restored, as versions; all written in the H&E's pixels and read back alike; and a file of
      // annotations as QuPath 0.4 writes them, drawn on the H&E, read in its pixels: its corners where
      // the version in use places them and near where the truth puts them, its hole kept, a detection
      // and a point left out.
      if (auto?.ok && autoTF) {
        await act('show_tissue', { dataset: 'sim-pathology', whole: true, transcripts: false });
        await chrome.eval('window.situcraftViewer.settled()');
        const tool = (name) => chrome.eval(`[...document.querySelectorAll('.viewer-region-tools button')].find((b) => b.textContent.includes('${name}')).click()`);
        const client = (x, y) => chrome.eval(`window.situcraftViewer.regions.clientOf(${x}, ${y})`);
        const umPerCss = await chrome.eval('(() => { const r = window.situcraftViewer.renderer; return (r.ratio || 1) / r.camera.scale; })()');
        const latest = () => chrome.eval('window.situcraftViewer.regions.list().at(-1)');
        const geometryOf = (id) => chrome.eval(`window.situcraft.store.project.regions.find((r) => r.id === '${id}').versions.at(-1).geometry`);
        const shoelace = (ring) => Math.abs(ring.reduce((a, [x, y], i) => a + x * ring[(i + 1) % ring.length][1] - ring[(i + 1) % ring.length][0] * y, 0)) / 2;
        const hand = {};
        // A rectangle dragged from (40, 30) to (120, 95) µm.
        await tool('Rectangle');
        const ra = await client(40, 30);
        const rb = await client(120, 95);
        await chrome.drag([ra, [(ra[0] + rb[0]) / 2, (ra[1] + rb[1]) / 2], rb]);
        const rect = await latest();
        const rg = await geometryOf(rect.id);
        const xs = rg[0][0].map((p) => p[0]);
        const ys = rg[0][0].map((p) => p[1]);
        hand.rectangle = Math.max(Math.abs(Math.min(...xs) - 40), Math.abs(Math.max(...xs) - 120), Math.abs(Math.min(...ys) - 30), Math.abs(Math.max(...ys) - 95));
        // A polygon clicked at five corners, closed by Enter.
        const corners = [[150, 40], [230, 50], [220, 110], [160, 100], [140, 70]];
        await tool('Polygon');
        for (const [x, y] of corners) await chrome.click(...(await client(x, y)));
        await chrome.key('Enter');
        const poly = await latest();
        const pg = await geometryOf(poly.id);
        hand.polygon = pg[0][0].length === 5 ? Math.max(...corners.map(([x, y], k) => Math.hypot(pg[0][0][k][0] - x, pg[0][0][k][1] - y))) : Infinity;
        // Freehand around a circle of 36 points (30 µm about (80, 180)): the area of the path dragged.
        const circle = Array.from({ length: 37 }, (_, k) => [80 + 30 * Math.cos((2 * Math.PI * k) / 36), 180 + 30 * Math.sin((2 * Math.PI * k) / 36)]);
        await tool('Freehand');
        await chrome.drag(await Promise.all(circle.map(([x, y]) => client(x, y))));
        const free = await latest();
        hand.freehand = Math.abs(free.area / shoelace(circle.slice(0, 36)) - 1);
        // A brush stroke 15 CSS pixels wide each side, from (170, 170) to (250, 170) µm: a capsule.
        await tool('Brush');
        await chrome.eval('window.situcraftViewer.regions.setBrush(15, false, false)');
        await chrome.drag(await Promise.all(Array.from({ length: 9 }, (_, k) => client(170 + 10 * k, 170))));
        const brushed = await latest();
        const rr = 15 * umPerCss;
        hand.brush = Math.abs(brushed.area / (2 * rr * 80 + Math.PI * rr * rr) - 1);
        await tool('Brush');
        const handNames = [rect, poly, free, brushed].map((r) => `${r.name} (${r.cls}, ${r.cells} cells)`);
        check('viewer', 'regions drawn by hand in the Viewer: a rectangle dragged, a polygon clicked and closed by Enter, freehand and a brush stroke, each where the pointer went', `${handNames.join('; ')}; rectangle ${hand.rectangle.toFixed(4)} µm and polygon ${hand.polygon.toFixed(4)} µm from the points; freehand's area ${(100 * hand.freehand).toFixed(3)}% from the path's, the brush's ${(100 * hand.brush).toFixed(3)}% from the capsule's (${umPerCss.toFixed(3)} µm a CSS pixel)`,
          hand.rectangle <= umPerCss && hand.polygon <= umPerCss && hand.freehand < 0.005 && hand.brush < 0.01 && new Set([rect.id, poly.id, free.id, brushed.id]).size === 4,
          'within a CSS pixel of the points; areas within 0.5% (freehand) and 1% (the brush)');

        // Drawn by a program: a rectangle in the H&E's pixels and a polygon in µm.
        const inPixels = await act('draw_region', { dataset: 'sim-pathology', rectangle: [100, 100, 400, 350], coordinates: simAdded.data.name, class: 'Necrosis', name: 'In the H&E\'s pixels' });
        const inUm = await act('draw_region', { dataset: 'sim-pathology', polygon: [[250, 180], [300, 190], [290, 240], [240, 230]], class: 'Stroma', note: 'The viewer suite' });
        // Each region's cells against the suite's own count, from the regions written in µm.
        const winding = (ring, x, y) => {
          let w = 0;
          for (let i = 0; i + 1 < ring.length; i += 1) {
            const [ax, ay] = ring[i];
            const [bx, by] = ring[i + 1];
            const side = (bx - ax) * (y - ay) - (x - ax) * (by - ay);
            if (ay <= y) {
              if (by > y && side > 0) w += 1;
            } else if (by <= y && side < 0) w -= 1;
          }
          return w;
        };
        const inFeature = (g, x, y) => (g.type === 'Polygon' ? [g.coordinates] : g.coordinates).some((pl) => winding(pl[0], x, y) !== 0 && pl.slice(1).every((hole) => winding(hole, x, y) === 0));
        const umFile = join(work, 'regions-um.geojson');
        const wroteUm = await act('export', { what: 'regions', dataset: 'sim-pathology', coordinates: 'um', path: umFile });
        const umDoc = wroteUm.ok ? JSON.parse(readFileSync(umFile, 'utf8')) : { features: [] };
        const centroids = await chrome.eval('(() => { const c = window.situcraftViewer.cellsDrawn(); return { x: Array.from(c.x), y: Array.from(c.y) }; })()');
        const listed = await act('regions', { dataset: 'sim-pathology' });
        const counted = umDoc.features.map((f) => {
          const id = f.properties.metadata['SituCraft region'];
          const own = centroids.x.reduce((n, x, i) => n + (inFeature(f.geometry, x, centroids.y[i]) ? 1 : 0), 0);
          return { id, name: f.properties.name, page: listed.data?.regions.find((r) => r.id === id)?.cells, suite: own };
        });
        const differing = counted.filter((c) => c.page !== c.suite);
        // The cells in the H&E pixels' rectangle chosen: as many as the suite counts.
        const chosen = inPixels.ok ? await act('regions', { dataset: 'sim-pathology', choose: inPixels.data.id }) : null;
        const chosenState = await act('get_state');
        const wantChosen = counted.find((c) => c.id === inPixels.data?.id)?.suite;
        check('viewer', 'regions\' cells: each region\'s count, by centroid, the suite\'s own (a winding number over the cells, of the regions written in µm); the cells in one chosen', `${inPixels.message ?? inPixels.error}; ${inUm.message ?? inUm.error}; ${counted.map((c) => `${c.name} ${c.page}/${c.suite}`).join(', ')}; ${chosen?.message ?? chosen?.error}; chosen ${chosenState.data?.chosen?.count}`,
          inPixels.ok && inUm.ok && wroteUm.ok && counted.length === 6 && differing.length === 0 && counted.every((c) => c.suite > 0) && chosen?.ok && chosen.data.chosen.count === wantChosen && chosenState.data?.chosen?.count === wantChosen,
          'the same counts; the cells chosen as many');

        // Versions: the µm polygon erased from with a brush, then its first version restored.
        const erased = inUm.ok ? await act('edit_region', { dataset: 'sim-pathology', region: inUm.data.name, brush: { points: [[270, 210]], radius: 8, erase: true }, note: 'a hole' }) : null;
        const restored = inUm.ok ? await act('edit_region', { dataset: 'sim-pathology', region: inUm.data.name, restore: 1 }) : null;
        const projectFile = join(work, 'regions-project.json');
        const wroteProject = await act('export', { what: 'project', path: projectFile });
        const kept = wroteProject.ok ? JSON.parse(readFileSync(projectFile, 'utf8')).regions?.find((r) => r.id === inUm.data?.id) : null;
        const hole = inUm.ok && erased?.ok ? inUm.data.area - erased.data.area : 0;
        check('viewer', 'a region\'s versions: erased from with the brush, then its first version restored, each kept in the project document', `${erased?.message ?? erased?.error}; ${restored?.message ?? restored?.error}; the hole ${hole.toFixed(1)} µm² against ${(Math.PI * 64).toFixed(1)}; the document keeps ${kept?.versions.length} versions`,
          Boolean(erased?.ok && restored?.ok) && Math.abs(hole / (Math.PI * 64) - 1) < 0.01 && erased.data.version === 2 && restored.data.version === 3 && restored.data.area === inUm.data.area && restored.data.cells === inUm.data.cells && kept?.versions.length === 3 && kept.versions[1].tool === 'brush',
          'the hole within 1% of the brush\'s disc; version 3 the first again, its area and cells');

        // Written in the H&E's pixels: the rectangle drawn in them comes back at its corners; read
        // back in them, every region as it was (its area and cells).
        const pxFile = join(work, 'regions-he.geojson');
        const wrotePx = await act('export', { what: 'regions', dataset: 'sim-pathology', coordinates: simAdded.data.name, path: pxFile });
        const pxDoc = wrotePx.ok ? JSON.parse(readFileSync(pxFile, 'utf8')) : { features: [] };
        const pxRing = pxDoc.features.find((f) => f.properties.metadata['SituCraft region'] === inPixels.data?.id)?.geometry.coordinates[0] ?? [];
        const cornerOff = Math.max(...[[100, 100], [400, 100], [400, 350], [100, 350]].map(([u, v]) => Math.min(...pxRing.map(([a, b]) => Math.hypot(a - u, b - v)))));
        const before = await act('regions', { dataset: 'sim-pathology' });
        const readBack = await act('import_regions', { dataset: 'sim-pathology', path: pxFile, coordinates: simAdded.data.name });
        const after = await act('regions', { dataset: 'sim-pathology' });
        const pairs = (before.data?.regions ?? []).map((r) => [r, after.data?.regions.find((x) => x.name === `${r.name} (2)`)]);
        const worstArea = Math.max(...pairs.map(([a, b]) => (b ? Math.abs(b.area / a.area - 1) : Infinity)));
        const sameCells = pairs.every(([a, b]) => b && b.cells === a.cells);
        check('viewer', 'regions written in the H&E\'s pixels (through its deformed alignment) and read back in them: each as it was', `${wrotePx.message ?? wrotePx.error}; the rectangle drawn in its pixels back within ${cornerOff.toFixed(4)} pixels of its corners; ${readBack.message ?? readBack.error}; areas within ${(100 * worstArea).toFixed(4)}%; cells ${sameCells ? 'the same' : 'differing'}`,
          wrotePx.ok && readBack.ok && autoTF.deformation && cornerOff < 0.01 && pairs.length === 6 && worstArea < 1e-4 && sameCells,
          'corners within 0.01 pixels; areas within 0.01%; the same cells');

        // A file as QuPath 0.4 writes annotations drawn on the H&E: a pentagon classed Tumor, a
        // square with a hole classed Stroma, a detection and a point.
        const pentagon = [[200, 200], [500, 180], [560, 420], [330, 520], [180, 380]];
        const qupath = { type: 'FeatureCollection', features: [
          { type: 'Feature', id: 'b1d8c0a2-4f6e-4a8b-9c1d-2e3f4a5b6c7d', geometry: { type: 'Polygon', coordinates: [[...pentagon, pentagon[0]]] }, properties: { objectType: 'annotation', name: 'Drawn in QuPath', classification: { name: 'Tumor', color: [200, 0, 0] }, isLocked: false } },
          { type: 'Feature', id: 'c2e9d1b3-5a7f-4b9c-8d2e-3f4a5b6c7d8e', geometry: { type: 'Polygon', coordinates: [[[600, 100], [900, 100], [900, 400], [600, 400], [600, 100]], [[700, 200], [700, 300], [800, 300], [800, 200], [700, 200]]] }, properties: { objectType: 'annotation', classification: { name: 'Stroma', color: [150, 200, 150] } } },
          { type: 'Feature', id: 'd3fae2c4-6b8a-4cad-9e3f-4a5b6c7d8e9f', geometry: { type: 'Polygon', coordinates: [[[300, 300], [310, 300], [310, 310], [300, 300]]] }, properties: { objectType: 'detection', classification: { name: 'Tumor', color: [200, 0, 0] } } },
          { type: 'Feature', id: 'e4abf3d5-7c9b-4dbe-8f4a-5b6c7d8e9fa0', geometry: { type: 'Point', coordinates: [250, 250] }, properties: { objectType: 'annotation' } },
        ] };
        const qpFile = join(work, 'qupath-annotations.geojson');
        writeFileSync(qpFile, JSON.stringify(qupath));
        const fromQuPath = await act('import_regions', { dataset: 'sim-pathology', path: qpFile, coordinates: simAdded.data.name });
        const imported = fromQuPath.data?.regions ?? [];
        const qp = imported.find((r) => r.name === 'Drawn in QuPath');
        const qg = qp ? await geometryOf(qp.id) : null;
        const qpColor = qp ? await chrome.eval(`window.situcraft.store.project.regions.find((r) => r.id === '${qp.id}').versions[0].color`) : null;
        const placedOff = qg ? Math.max(...pentagon.map(([u, v]) => {
          const [x, y] = placePoint(autoTF, u, v);
          return Math.min(...qg[0][0].map(([a, b]) => Math.hypot(a - x, b - y)));
        })) : Infinity;
        const truthOff = qg ? Math.max(...pentagon.map(([u, v]) => {
          const [x, y] = truth(u, v);
          return Math.min(...qg[0][0].map(([a, b]) => Math.hypot(a - x, b - y)));
        })) : Infinity;
        const holed = imported.find((r) => r.cls === 'Stroma');
        const hg = holed ? await geometryOf(holed.id) : null;
        // The square's area on the tissue: its outline and its hole's, placed a pixel at a time.
        const placedArea = (x0, y0, x1, y1) => {
          const ring = [];
          for (let u = x0; u < x1; u += 1) ring.push([u, y0]);
          for (let v = y0; v < y1; v += 1) ring.push([x1, v]);
          for (let u = x1; u > x0; u -= 1) ring.push([u, y1]);
          for (let v = y1; v > y0; v -= 1) ring.push([x0, v]);
          return shoelace(ring.map(([u, v]) => placePoint(autoTF, u, v)));
        };
        const holedWant = placedArea(600, 100, 900, 400) - placedArea(700, 200, 800, 300);
        check('viewer', 'annotations as QuPath 0.4 writes them, drawn on the H&E, read in its pixels: placed by its alignment in use (and its deformation), near where the truth puts them; a hole kept; a detection and a point left out', `${fromQuPath.message ?? fromQuPath.error}; the pentagon's corners ${placedOff.toExponential(2)} µm from where the version places them, at most ${truthOff.toFixed(3)} µm from the truth; its color ${JSON.stringify(qpColor)}; the square ${hg?.[0].length} rings, ${holed ? holed.area.toFixed(1) : '?'} µm² against ${holedWant.toFixed(1)}`,
          Boolean(fromQuPath.ok) && imported.length === 2 && JSON.stringify(fromQuPath.data.skipped) === JSON.stringify({ detection: 1, Point: 1 }) && qp?.cls === 'Tumor' && placedOff < 1e-3 && truthOff < bound && JSON.stringify(qpColor) === '[200,0,0]' && hg?.[0].length === 2 && Math.abs(holed.area / holedWant - 1) < 1e-3,
          `corners within 0.001 µm of the version's placing, ${bound.toFixed(1)} µm of the truth; a hole, its area within 0.1% of its outline placed a pixel at a time; skipped: a detection, a point`);

        // Reviews (wave 5, slice 6). Labels imported for the simulated tissue's cells, each with a
        // confidence; a cell relabeled by hand in the Viewer's inspector, with its reason; by a program,
        // cells accepted, one ambiguous, one of concern, a region excluded with its cells, a region
        // relabeled, a comment. Each review's record against the state it changed (the label each
        // relabeled cell now carries in the curated set and no other cell's; the label and confidence the
        // table gave; the cells excluded those in the region; the version of the H&E's alignment in use;
        // the region's new class); the Review status fill against the suite's own reading of the
        // record; the record written as CSV and JSON and read back, row for row.
        {
          const simId = await chrome.eval(`window.situcraft.store.project.datasets.find((d) => d.name === 'sim-pathology').id`);
          const itemUrl = await chrome.eval(`window.situcraft.bundles.link('${simId}').item.url`);
          const own = await chrome.eval('(() => { const c = window.situcraftViewer.cells; return { n: c.n, x: Array.from(c.x) }; })()');
          const ids = (await fetch(`${url}${itemUrl}/cells/lookup`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ rows: [...Array(own.n).keys()] }) }).then((r) => r.json())).ids;
          const typeOf = (i) => (own.x[i] < simSize[0] / 2 ? 'Epithelial' : 'Immune');
          const confOf = (i) => ((i * 37) % 100) / 100;
          const labelsFile = join(work, 'sim-labels.csv');
          writeFileSync(labelsFile, `cell_id,cell_type,confidence\n${ids.map((id, i) => `${id},${typeOf(i)},${confOf(i)}`).join('\n')}\n`);
          const imported = await act('import_labels', { dataset: 'sim-pathology', path: labelsFile });
          await act('show_tissue', { dataset: 'sim-pathology', whole: true, transcripts: false, fill: 'labels', images: { [simAdded.data.name]: { visible: true, opacity: 1 } } });
          // By hand: the cell nearest the middle, relabeled in the inspector.
          const r1 = await chrome.eval(`(() => { const c = window.situcraftViewer.cells; let best = -1, d = Infinity; for (let i = 0; i < c.n; i += 1) { const e = (c.x[i] - ${simSize[0] / 2 - 30}) ** 2 + (c.y[i] - ${simSize[1] / 2}) ** 2; if (e < d) { d = e; best = i; } } return best; })()`);
          const other = typeOf(r1) === 'Epithelial' ? 'Immune' : 'Epithelial';
          await act('select_cell', { dataset: 'sim-pathology', cell: `#${r1}` });
          const byHand = await chrome.eval(`(async () => {
            let form = null;
            for (let k = 0; k < 100 && !form; k += 1) { form = document.querySelector('.viewer-cell .review-form'); if (!form) await new Promise((r) => setTimeout(r, 100)); }
            if (!form) return { error: 'no review form' };
            form.querySelector('.review-by input').value = 'validation';
            form.querySelector('[data-review="relabel"]').click();
            await new Promise((r) => setTimeout(r, 50));
            form.querySelector('[data-review-label]').value = '${other}';
            form.querySelector('[data-review-note]').value = 'Its markers say otherwise';
            form.querySelector('[data-review-go]').click();
            for (let k = 0; k < 100 && !(window.situcraft.store.project.reviews ?? []).length; k += 1) await new Promise((r) => setTimeout(r, 100));
            const p = window.situcraft.store.project;
            return { review: p.reviews?.at(-1) ?? null, fill: window.situcraftViewer.layers().cells.fill, layerShown: window.situcraftViewer.layers().images?.find((l) => l.id === '${simAdded.data.id}')?.visible ?? null };
          })()`);
          const rec = byHand.review;
          const curatedId = rec?.edits?.[0]?.set;
          const curatedLabels = curatedId ? await chrome.eval(`(async () => { const { labelSource, refOf } = await import('/ui/label-sources.js'); const src = await labelSource(window.situcraft, refOf('${curatedId}')); return Array.from(src.label, (k) => (k >= 0 ? src.names[k] : null)); })()`) : [];
          const changed = curatedLabels.map((l, i) => (l !== typeOf(i) ? i : -1)).filter((i) => i >= 0);
          const handOk = rec && rec.action === 'relabel' && rec.by === 'validation' && rec.cells.length === 1 && rec.cells[0].row === r1 && rec.cells[0].id === ids[r1] && rec.cells[0].label === typeOf(r1)
            && Math.abs(rec.cells[0].confidence - confOf(r1)) < 1e-6 && rec.previous === typeOf(r1) && rec.next === other && rec.note === 'Its markers say otherwise' && rec.labels?.kind === 'imported'
            && rec.segmentation?.name && rec.images.length === 1 && rec.images[0].transform === auto.data.id && rec.images[0].shown === byHand.layerShown && changed.length === 1 && changed[0] === r1 && byHand.fill === `labels:${curatedId}:0`;
          check('viewer', 'a cell relabeled by hand in the Viewer\'s inspector: its review records who, why, its label and confidence as the table gave them, the label set, the segmentation and the H&E\'s version in use; the curated set begun carries the new label for that cell and no other, and the Viewer shows it', `${imported.message ?? imported.error}; cell ${ids[r1]} (#${r1}): ${rec ? `${rec.previous} → ${rec.next}, confidence ${rec.cells[0].confidence} (the table's ${confOf(r1)}), images ${JSON.stringify(rec.images)}` : JSON.stringify(byHand)}; ${changed.length} cell${changed.length === 1 ? '' : 's'} changed; the fill ${byHand.fill}`,
            Boolean(imported.ok && handOk), 'the record its state; one cell changed, the one relabeled');

          // By a program, of cells apart from each other and from the region excluded.
          const regionRows = await chrome.eval(`window.situcraftViewer.regions.rowsIn('${inPixels.data.id}')`);
          const free = [...Array(own.n).keys()].filter((i) => i !== r1 && !regionRows.includes(i));
          const epithelial = free.filter((i) => typeOf(i) === 'Epithelial').slice(0, 3);
          const [ambiguousRow, concernRow] = free.filter((i) => typeOf(i) === 'Immune').slice(0, 2);
          const steps = [];
          steps.push(await act('review', { dataset: 'sim-pathology', action: 'accept', cells: epithelial.map((i) => ids[i]), labels: curatedId, by: 'validation' }));
          steps.push(await act('review', { dataset: 'sim-pathology', action: 'ambiguous', cells: [`#${ambiguousRow}`], alternatives: ['Epithelial', 'Immune'], note: 'Mixed markers', by: 'validation' }));
          steps.push(await act('review', { dataset: 'sim-pathology', action: 'concern', cells: [ids[concernRow]], concern: 'split', note: 'One nucleus, two outlines', by: 'validation' }));
          steps.push(await act('review', { dataset: 'sim-pathology', action: 'exclude', region: inPixels.data.name, note: 'Out of focus', by: 'validation' }));
          steps.push(await act('review', { dataset: 'sim-pathology', action: 'relabel', region: inUm.data.name, label: 'Necrosis', note: 'Necrotic on the H&E', by: 'validation' }));
          steps.push(await act('review', { dataset: 'sim-pathology', action: 'comment', region: rect.name, note: 'Check against the IF', by: 'validation' }));
          const refused = await act('review', { dataset: 'sim-pathology', action: 'exclude', cells: ['#3'], by: 'validation' });
          const pageState = await chrome.eval('window.situcraftViewer.reviewState()');
          const excludedOk = JSON.stringify([...pageState.excluded].sort((a, b) => a - b)) === JSON.stringify([...regionRows].sort((a, b) => a - b));
          const relabeledRegion = (await act('regions', { dataset: 'sim-pathology' })).data?.regions.find((r) => r.id === inUm.data.id);
          const acceptRec = steps[0].data?.review;
          const acceptLabels = acceptRec ? await chrome.eval(`(async () => { const { labelSource, refOf } = await import('/ui/label-sources.js'); const src = await labelSource(window.situcraft, refOf('${curatedId}')); return { labels: [${epithelial.join(',')}].map((i) => src.names[src.label[i]]), edited: [${epithelial.join(',')}].map((i) => src.edited[i]) }; })()`) : null;
          check('viewer', 'remote control: review accepts cells (an accept edit each, their labels as they were), marks one ambiguous and one of concern, excludes a region with its cells (those in it exactly), relabels a region (a version of it with the new class), comments on another, and refuses an exclusion without a reason', `${steps.map((x) => x.message ?? x.error).join(' | ')}; refused: ${refused.message ?? refused.error}; excluded ${pageState.excluded.length} cells, the region holds ${regionRows.length}; ${JSON.stringify(pageState.counts)}; ${inUm.data.name} now ${relabeledRegion?.class} (version ${relabeledRegion?.version}); accepted ${JSON.stringify(acceptLabels)}`,
            steps.every((x) => x.ok) && !refused.ok && /Say why it is left out/.test(refused.error ?? refused.message ?? '') && excludedOk && regionRows.length > 0 && pageState.counts.ambiguous === 1 && pageState.counts.concern === 1 && pageState.counts.accepted === 3 && pageState.counts.relabeled === 1
            && relabeledRegion?.class === 'Necrosis' && relabeledRegion.version === 4 && steps[4].data.review.previous === 'Stroma' && steps[4].data.review.next === 'Necrosis' && acceptLabels?.labels.every((l) => l === 'Epithelial') && acceptLabels.edited.every((e) => e >= 0) && acceptRec.edits.length === 1,
            'each as asked; the cells excluded those in the region; the region reclassed; the reason required');

          // The record written and read back; the Review status fill against the suite's own reading.
          const csvFile = join(work, 'reviews.csv');
          const jsonFile = join(work, 'reviews.json');
          const wroteCsv = await act('export', { what: 'reviews', dataset: 'sim-pathology', path: csvFile });
          const wroteJson = await act('export', { what: 'reviews', dataset: 'sim-pathology', format: 'json', path: jsonFile });
          const parseCsv = (text) => {
            const rows = [];
            let row = [];
            let field = '';
            let quoted = false;
            for (let i = 0; i < text.length; i += 1) {
              const ch = text[i];
              if (quoted) {
                if (ch === '"' && text[i + 1] === '"') { field += '"'; i += 1; } else if (ch === '"') quoted = false; else field += ch;
              } else if (ch === '"') quoted = true;
              else if (ch === ',') { row.push(field); field = ''; } else if (ch === '\n') { row.push(field); rows.push(row); row = []; field = ''; } else field += ch;
            }
            const [head, ...body] = rows;
            return body.map((r) => Object.fromEntries(head.map((name, k) => [name, r[k]])));
          };
          const csvRows = wroteCsv.ok ? parseCsv(readFileSync(csvFile, 'utf8')) : [];
          const doc = wroteJson.ok ? JSON.parse(readFileSync(jsonFile, 'utf8')) : { reviews: [] };
          const pageRecords = await chrome.eval(`window.situcraft.store.project.reviews.filter((r) => r.dataset === '${simId}')`);
          const expectedRows = doc.reviews.reduce((n, r) => n + (r.target === 'cells' ? r.cells.length : r.target === 'clusters' ? r.clusters.clusters.length : 1), 0);
          const rowMismatch = csvRows.filter((row) => {
            const r = doc.reviews.find((x) => x.id === row.review);
            if (!r || r.action !== row.action || r.by !== row.reviewer || r.note !== row.note || String(r.next ?? '') !== row.new) return true;
            if (r.target !== 'cells') return row.region !== (r.region?.id ?? '');
            const c = r.cells.find((x) => String(x.row) === row.cell_row);
            return !c || (c.id ?? '') !== row.cell_id || String(c.previous ?? '') !== row.previous || (c.confidence === null ? row.confidence !== '' : Math.abs(Number(row.confidence) - c.confidence) > 1e-9);
          });
          // Each cell's status by the suite's reading of the CSV: the latest of its reviews deciding,
          // excluded first, then a concern, ambiguous, relabeled, accepted, commented.
          const status = Array.from({ length: own.n }, () => ({ label: '', excluded: false, concern: false, commented: false }));
          for (const row of [...csvRows].sort((a, b) => (a.time < b.time ? -1 : a.time > b.time ? 1 : 0))) {
            if (row.target !== 'cells' || row.segmentation !== pageState.segmentation.id) continue;
            const st = status[Number(row.cell_row)];
            if (row.action === 'accept') st.label = 'accepted';
            else if (row.action === 'relabel') st.label = 'relabeled';
            else if (row.action === 'ambiguous') st.label = 'ambiguous';
            else if (row.action === 'exclude') st.excluded = true;
            else if (row.action === 'include') st.excluded = false;
            else if (row.action === 'concern') st.concern = true;
            else if (row.action === 'resolve') st.concern = false;
            else if (row.action === 'comment') st.commented = true;
          }
          const palette = { excluded: [140, 140, 140], concern: [213, 94, 0], ambiguous: [230, 159, 0], relabeled: [0, 114, 178], accepted: [0, 158, 115], commented: [204, 121, 167] };
          const want = status.map((st) => (st.excluded ? palette.excluded : st.concern ? palette.concern : st.label ? palette[st.label] : st.commented ? palette.commented : null));
          await act('show_tissue', { dataset: 'sim-pathology', fill: 'review' });
          const rgba = await chrome.eval(`(async () => { for (let k = 0; k < 50 && !window.situcraftViewer.clusterFill()?.review; k += 1) await new Promise((r) => setTimeout(r, 100)); return Array.from(window.situcraftViewer.fillColors() ?? []); })()`);
          const fillWrong = want.filter((w, i) => (w ? rgba[4 * i + 3] !== 255 || rgba[4 * i] !== w[0] || rgba[4 * i + 1] !== w[1] || rgba[4 * i + 2] !== w[2] : rgba[4 * i + 3] !== 0)).length;
          check('viewer', 'the review record written as CSV (a row a cell or region) and JSON, read back: the JSON the project\'s record, every CSV row its review\'s and cell\'s; the Review status fill each cell\'s status by the suite\'s reading of the CSV', `${wroteCsv.message ?? wroteCsv.error}; ${wroteJson.message ?? wroteJson.error}; ${csvRows.length} rows (${expectedRows} expected), ${rowMismatch.length} differing; ${doc.reviews.length} reviews in the JSON, ${pageRecords.length} in the project; the fill ${fillWrong} of ${own.n} cells otherwise`,
            wroteCsv.ok && wroteJson.ok && doc.format === 'situcraft-reviews' && JSON.stringify(doc.reviews.filter((r) => !r.derived)) === JSON.stringify(pageRecords) && csvRows.length === expectedRows && rowMismatch.length === 0 && rgba.length === 4 * own.n && fillWrong === 0,
            'the same, row for row; every cell\'s color its status');
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
