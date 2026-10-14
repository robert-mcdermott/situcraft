// Captures the documentation's screenshots, in the light and dark themes, as
// docs/images/<scene>-<theme>.webp: the README shows the one matching the reader's theme. Each scene
// drives a SituCraft built from source through its remote control (actions.go, web/ui/remote.js),
// as any script can, on a fresh, empty library, with the development bundles as its examples (node
// validation/fetch.mjs downloads them), so every run gives the same pictures. Follows MaveScape
// 0.8.0's docs/capture/capture.mjs.
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
async function startSituCraft({ examples = DATA } = {}) {
  const library = mkdtempSync(join(temp, 'library-'));
  places = { library, examples, cache: CACHE };
  const flags = ['--remote-control', '--window', 'none', '--port', String(PORT), '--data-dir', library, '--cache-dir', CACHE, '--examples-dir', examples];
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

// The places shown in the window, as SHOWN names them: every text node and title that holds this
// run's temporary folders.
async function showPlaces() {
  const pairs = Object.entries(places).map(([k, v]) => [v, SHOWN[k]]).sort((a, c) => c[0].length - a[0].length);
  await js(`(() => {
    const pairs = ${JSON.stringify(pairs)};
    const fix = (text) => pairs.reduce((t, [from, to]) => t.split(from).join(to), text);
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) if (pairs.some(([from]) => node.nodeValue.includes(from))) node.nodeValue = fix(node.nodeValue);
    for (const el of document.querySelectorAll('[title]')) el.title = fix(el.title);
    return true;
  })()`);
}

// --- Scenes ---------------------------------------------------------------------------------------

const scenes = {
  // The start page: the examples to download, what to bring.
  async start() {
    await act('set_mode', { mode: 'start' });
    await waitFor(`document.querySelectorAll('.card.example').length === 2`);
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
        situcraft = await startSituCraft(name === 'start' ? { examples: mkdtempSync(join(temp, 'examples-')) } : {});
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
