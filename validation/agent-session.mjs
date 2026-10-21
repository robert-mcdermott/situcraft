// A scripted agent session (wave 9, slice 9): `situcraft mcp` driven over stdin and stdout as an AI
// agent's client drives it, with no model, every tool called on the examples (the simulated tissue,
// the simulated cohort and the Lung) in the window a person watches, a headless Chrome. It checks
// the protocol (initialize, tools/list equal to the actions with their titles, hints and schemas,
// errors); that a server given no leave offers the tools that read and show alone and refuses the
// others, saying which leave they need; that every tool answers; that each tool said to read only
// leaves the project's history as it was, and that every change the agent made carries its name
// in it; that pictures come as images and progress before the answer; that every call is a line of
// the library's log; the guide's sessions (docs/site/pages/docs/agents.html), each call in its order
// and every number their answers quote; a recipe made again by situcraft run with the same ids and
// verified; and that stdout carries nothing but JSON-RPC and the program stops when stdin closes.
// Adapted from MaveScape 0.8.0's validation/agent-session.mjs (the same author's).
//
//   node validation/agent-session.mjs [--verbose]
//
// Needs Go (to build SituCraft), Chrome, Chromium, Edge or Brave (CHROME=path; SITUCRAFT_CHROME_ARGS
// for more of its flags) and the Lung 2-FOV bundle (node validation/fetch.mjs xenium-lung-2fov).
// Exits with status 1 when a check fails, 2 when it cannot run.

import { spawn, spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gunzipSync } from 'node:zlib';
import { findChrome, launch, sleep } from './cdp.mjs';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const LUNG = join(ROOT, 'validation', 'cache', 'xenium-lung-2fov');
const verbose = process.argv.includes('--verbose');
const AGENT = 'Agent session';
const results = [];
function check(name, value, ok, required = '') {
  results.push({ name, ok });
  if (verbose || !ok) console.log(`${ok ? 'ok  ' : 'FAIL'} ${name} — ${value}${required ? ` (required ${required})` : ''}`);
}
if (!existsSync(join(LUNG, 'experiment.xenium'))) {
  console.error(`agent-session: the Lung 2-FOV bundle is not in ${LUNG}: node validation/fetch.mjs xenium-lung-2fov`);
  process.exit(2);
}
if (!findChrome()) {
  console.error('agent-session: no Chrome, Chromium, Edge or Brave found; set CHROME.');
  process.exit(2);
}

// --- situcraft mcp, and an MCP client on its stdio ------------------------------------------------

const temp = mkdtempSync(join(tmpdir(), 'situcraft-agent-'));
const library = join(temp, 'library');
const cache = join(temp, 'cache');
const examples = join(temp, 'examples');
const out = join(temp, 'out');
mkdirSync(examples);
mkdirSync(out);
// The Lung where the examples are kept, as a download leaves it; the simulated ones are made there.
symlinkSync(LUNG, join(examples, 'xenium-lung-2fov'), 'dir');
const binary = join(temp, process.platform === 'win32' ? 'situcraft.exe' : 'situcraft');
await new Promise((done, fail) => spawn('go', ['build', '-o', binary, '.'], { cwd: ROOT, stdio: 'inherit' }).on('exit', (code) => (code ? fail(new Error('go build failed (is Go installed?)')) : done())));
const tools = JSON.parse(readFileSync(join(ROOT, 'clients', 'tools.json'), 'utf8'));

// A session of situcraft mcp with these flags: one JSON-RPC message per line on stdout, replies by
// id, notifications kept in the order they came (with the replies' places among them).
function serve(flags) {
  const child = spawn(binary, ['mcp', ...flags, '--window', 'none', '--data-dir', library, '--cache-dir', cache, '--examples-dir', examples], { cwd: temp, stdio: ['pipe', 'pipe', 'pipe'] });
  const s = { child, stderr: '', notJSON: [], arrived: [], pending: new Map(), next: 1 };
  child.stderr.on('data', (chunk) => { s.stderr += chunk; });
  s.exited = new Promise((done) => child.on('exit', (code) => done(code)));
  let buffered = '';
  child.stdout.on('data', (chunk) => {
    buffered += chunk;
    for (let at = buffered.indexOf('\n'); at >= 0; at = buffered.indexOf('\n')) {
      const line = buffered.slice(0, at);
      buffered = buffered.slice(at + 1);
      if (!line.trim()) continue;
      let message;
      try {
        message = JSON.parse(line);
      } catch {
        s.notJSON.push(line);
        continue;
      }
      s.arrived.push(message);
      if (message.id !== undefined && s.pending.has(message.id)) {
        s.pending.get(message.id)(message);
        s.pending.delete(message.id);
      }
    }
  });
  s.rpc = (method, params) => {
    const id = s.next++;
    child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id, method, ...(params ? { params } : {}) })}\n`);
    return new Promise((done, fail) => {
      s.pending.set(id, done);
      setTimeout(() => {
        if (s.pending.delete(id)) fail(new Error(`${method} got no reply`));
      }, 20 * 60 * 1000);
    });
  };
  s.notify = (method) => child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', method })}\n`);
  // The agent closes stdin: SituCraft stops. Answers its exit code, or 'still running'.
  s.close = async () => {
    child.stdin.end();
    const code = await Promise.race([s.exited, sleep(15000).then(() => 'still running')]);
    if (code === 'still running') child.kill();
    return code;
  };
  return s;
}
const initialize = (s) => s.rpc('initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'agent-session', title: AGENT, version: '1' } });

// --- With no leave: the tools that read and show -----------------------------------------------

{
  const s = serve([]);
  const init = await initialize(s);
  s.notify('notifications/initialized');
  const offered = (await s.rpc('tools/list')).result.tools.map((t) => t.name);
  const reading = tools.filter((t) => t.annotations.readOnlyHint).map((t) => t.name);
  const refused = await s.rpc('tools/call', { name: 'cluster_cells', arguments: {} });
  const text = refused.result?.content?.[0]?.text ?? refused.error?.message ?? '';
  const code = await s.close();
  check('situcraft mcp with no leave: the tools that read and show offered (and jobs, whose stopping needs compute), a job refused naming the leave it needs, and SituCraft stopping when stdin closes', `${offered.length} offered of ${tools.length}, ${reading.length} read only; "${text.slice(0, 110)}"; exit ${code}`,
    Boolean(init.result?.serverInfo) && reading.every((n) => offered.includes(n)) && offered.every((n) => reading.includes(n) || n === 'jobs') && !offered.includes('cluster_cells') && refused.result?.isError && /--allow compute/.test(text) && code === 0 && /read and show/.test(s.stderr));
}

// --- Every leave: the session --------------------------------------------------------------------

const s = serve(['--allow', 'all']);
const called = new Set();
let hints = new Map();
let page = null;
// The open project and its history's length and last change; a tool that opens another project
// (open_project) changes which is open, not a project's history.
const historyHead = async () => (page ? JSON.parse(await page.eval('JSON.stringify([window.situcraft.store.project.id, window.situcraft.store.state.labels.past.length, window.situcraft.store.state.labels.past.at(-1) ?? null])')) : null);
const sameHistory = (a, b) => a[0] !== b[0] || (a[1] === b[1] && a[2] === b[2]);
const readOnlyChanged = [];
// The calls sent, as the log must hold them: done, and refused or failed (a call the server
// refuses before the page sees it, for its leave or a missing argument, is refused; the read-only
// session's refusal is in the same library).
const logged = { done: 0, notDone: 1 };
// A tool's call: { error } for a JSON-RPC error, else { isError, text, message, data, changed, result }.
async function call(name, args = {}, meta = null) {
  called.add(name);
  const before = hints.get(name)?.readOnlyHint ? await historyHead() : null;
  const reply = await s.rpc('tools/call', { name, arguments: args, ...(meta ? { _meta: meta } : {}) });
  if (reply.error) {
    logged.notDone += 1;
    return { error: reply.error };
  }
  const r = reply.result;
  const text = r.content?.find((c) => c.type === 'text')?.text ?? '';
  if (before !== null && !sameHistory(before, await historyHead())) readOnlyChanged.push(name);
  if (r.isError) logged.notDone += 1;
  else logged.done += 1;
  return { isError: r.isError === true, text, message: r.structuredContent?.message, data: r.structuredContent?.data, changed: r.structuredContent?.changed, result: r, id: reply.id };
}
async function tool(name, args = {}, meta = null) {
  const r = await call(name, args, meta);
  if (r.error || r.isError) throw new Error(`${name} ${JSON.stringify(args).slice(0, 160)}: ${r.error?.message ?? r.text}`);
  return r;
}
const images = (r) => (r.result?.content ?? []).filter((c) => c.type === 'image');
const isJPEG = (c) => c?.mimeType === 'image/jpeg' && Buffer.from(c.data, 'base64').subarray(0, 3).equals(Buffer.from([0xff, 0xd8, 0xff]));
const isPNG = (c) => c?.mimeType === 'image/png' && Buffer.from(c.data, 'base64').subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]));
const table = (t) => t.rows.map((row) => Object.fromEntries(t.columns.map((c, i) => [c, row[i]])));
const r2 = (v) => Number(v).toFixed(2);

// The cells holding at least `min` transcripts of a gene, from a bundle's cell_feature_matrix (MEX),
// not SituCraft's readers.
function cellsWithGene(bundle, gene, min) {
  const folder = join(bundle, 'cell_feature_matrix');
  const features = gunzipSync(readFileSync(join(folder, 'features.tsv.gz'))).toString().trim().split('\n').map((l) => l.split('\t')[1]);
  const row = features.indexOf(gene) + 1;
  let n = 0;
  for (const line of gunzipSync(readFileSync(join(folder, 'matrix.mtx.gz'))).toString().split('\n')) {
    if (!line || line.startsWith('%')) continue;
    const [g, , v] = line.split(' ').map(Number);
    if (g === row && v >= min) n += 1;
  }
  return n;
}

let browser = null;
let url = null;
try {
  for (let i = 0; i < 300 && !/is running at http/.test(s.stderr); i += 1) await sleep(100);
  url = /is running at (http:\/\/[\d.]+:\d+)/.exec(s.stderr)?.[1];
  check('situcraft mcp --allow all starts SituCraft with remote control on, its banner on stderr', s.stderr.split('\n').filter(Boolean).slice(0, 2).join(' | '), Boolean(url) && /edits, compute and files/.test(s.stderr));

  // --- The protocol ---------------------------------------------------------------------------
  const init = await initialize(s);
  s.notify('notifications/initialized');
  check('initialize: the protocol version asked, SituCraft named with its version, its tools, and instructions saying where to start and that results are never a diagnosis', `${init.result.protocolVersion}; ${init.result.serverInfo.name} ${init.result.serverInfo.version}; ${init.result.instructions.length} characters of instructions`,
    init.result.protocolVersion === '2025-06-18' && init.result.serverInfo.name === 'situcraft' && init.result.capabilities.tools && /Start with get_state/.test(init.result.instructions) && /never a diagnosis/.test(init.result.instructions));
  const listed = (await s.rpc('tools/list')).result.tools;
  hints = new Map(listed.map((t) => [t.name, t.annotations]));
  const same = listed.length === tools.length && listed.every((t, i) => t.name === tools[i].name && t.title === tools[i].title && t.description === tools[i].description && JSON.stringify(t.inputSchema) === JSON.stringify(tools[i].inputSchema) && JSON.stringify(t.annotations) === JSON.stringify(tools[i].annotations));
  const kinds = { read: listed.filter((t) => t.annotations.readOnlyHint).length, destructive: listed.filter((t) => t.annotations.destructiveHint).length, world: listed.filter((t) => t.annotations.openWorldHint).length };
  check('tools/list: the actions, in order, each with its title, description, arguments\' schema and MCP\'s hints, as clients/tools.json has them', `${listed.length} tools (${kinds.read} read only, ${kinds.destructive} destructive, ${kinds.world} reaching the network)`, same && listed.length > 80);
  const unknown = await s.rpc('tools/call', { name: 'no_such_tool', arguments: {} });
  const missing = await call('get_result', {});
  const nonsense = await s.rpc('nonsense/method');
  check('a tool that does not exist, a required argument left out and an unknown method: errors that say so', `${unknown.error?.message}; ${missing.text}; ${nonsense.error?.message}`, unknown.error?.code === -32602 && /no_such_tool/.test(unknown.error.message) && missing.isError && /id is required/.test(missing.text) && nonsense.error?.code === -32601);
  logged.notDone += 1;

  // --- The window the person watches ----------------------------------------------------------
  browser = await launch({ width: 1440, height: 900, scale: 1, args: (process.env.SITUCRAFT_CHROME_ARGS ?? '').split(' ').filter(Boolean) });
  const errors = [];
  browser.on('Runtime.exceptionThrown', (e) => errors.push(e.exceptionDetails?.exception?.description ?? e.exceptionDetails?.text));
  await browser.goto(`${url}/`, 1500);
  for (let i = 0; i < 100 && !(await browser.eval('Boolean(window.situcraft?.remote)')); i += 1) await sleep(100);
  page = browser;

  // --- The guide's first session: the simulated tissue's cell types and neighbors --------------
  // The calls docs/site/pages/docs/agents.html shows (A session on the simulated tissue), in its
  // order, and every number its answers quote.
  const empty = await tool('get_state');
  const opened = await tool('open_example', { example: 'simulated' });
  const state = await tool('get_state');
  const tissue = state.data.datasets[0];
  const bundle = tissue.path;
  const clusters = await tool('cluster_cells', {});
  const labels = await tool('label_cells', { markers: 'simulated' });
  await tool('build_graph', {});
  const enrichment = await tool('spatial_analysis', { question: 'enrichment', labels: labels.data.id });
  const pairs = table((await tool('get_result', { id: enrichment.data.id, limit: 0 })).data.table);
  const pair = (a, b) => pairs.find((p) => (p.a === a && p.b === b) || (p.a === b && p.b === a));
  const mixed = pairs.filter((p) => p.a < p.b);
  const session1 = {
    cells: tissue.cells,
    clusters: clusters.data.clusters.count,
    labeled: labels.data.cells.labeled[0],
    byLabel: Object.fromEntries(labels.data.labels.map((l) => [l.name, l.cells])),
    same: ['epithelial-like', 'fibroblast-like', 'immune-like', 'endothelial-like'].map((t) => r2(pair(t, t).count / pair(t, t).expected)),
    mixed: mixed.sort((x, y) => x.z - y.z).map((p) => `${p.a}/${p.b} ${r2(p.count / p.expected)} z ${p.z.toFixed(1)} padj ${Number(p.padj).toPrecision(2)}`),
  };
  const MIXED = [
    'endothelial-like/epithelial-like 0.26 z -26.0 padj 0.0010', 'epithelial-like/immune-like 0.49 z -23.3 padj 0.0010', 'endothelial-like/fibroblast-like 0.41 z -21.3 padj 0.0010',
    'fibroblast-like/immune-like 0.77 z -10.6 padj 0.0010', 'endothelial-like/immune-like 1.12 z 5.4 padj 0.0010', 'epithelial-like/fibroblast-like 1.38 z 12.8 padj 0.0010',
  ];
  check('the guide\'s session on the simulated tissue, first answer: 2,925 cells in 5 clusters, 2,887 labeled (epithelial-like 621, fibroblast-like 636, immune-like 996, endothelial-like 634); each type next to its own kind 1.34 to 2.27 times as often as at random; of two types, endothelial-like and epithelial-like least (0.26×), epithelial-like and fibroblast-like most (1.38×)', `${empty.message.slice(0, 50)} → ${opened.message.slice(0, 60)}…; ${JSON.stringify(session1)}`,
    session1.cells === 2925 && session1.clusters === 5 && session1.labeled === 2887 && labels.data.cells.unlabeled === 38 && session1.byLabel['epithelial-like'] === 621 && session1.byLabel['fibroblast-like'] === 636 && session1.byLabel['immune-like'] === 996 && session1.byLabel['endothelial-like'] === 634
    && session1.same.join() === '2.27,1.61,1.34,2.18' && JSON.stringify(session1.mixed) === JSON.stringify(MIXED));
  // "Which immune-like cells hold an epithelial marker? Keep them as a filter."
  const contaminated = { all: [{ labels: labels.data.id, in: ['immune-like'] }, { any: ['SIM001', 'SIM002', 'SIM003'].map((gene) => ({ gene, min: 2 })) }] };
  const asked = await tool('query_cells', { where: contaminated, columns: ['transcripts', 'SIM001'], limit: 5 });
  const saved = await tool('save_filter', { name: 'Immune-like with epithelial transcripts', where: contaminated });
  const filters = (await tool('get_state')).data.filters;
  check('the guide\'s session on the simulated tissue, second answer: 256 immune-like cells holding two or more transcripts of an epithelial marker, a page of them, and kept as a filter with the same cells, the answer saying what it added', `${asked.message.slice(0, 120)}; ${saved.message.slice(0, 80)}; ${JSON.stringify(saved.changed)}`,
    asked.data.kept === 256 && asked.data.rows.rows.length === 5 && /keeps 256 cells/.test(saved.message) && saved.changed?.some((c) => c.kind === 'filter' && c.change === 'added') && filters.some((f) => f.name === 'Immune-like with epithelial transcripts'));
  session1.contaminated = asked.data.kept;
  // "Write the enrichment's recipe": made again by situcraft run on the bundle, the same ids, its
  // certificate verified.
  const recipe = await tool('recipe', { id: enrichment.data.id });
  const recipeFile = join(out, 'enrichment.recipe.json');
  writeFileSync(recipeFile, `${JSON.stringify(recipe.data, null, 1)}\n`);
  const runOut = join(out, 'run');
  const ran = spawnSync(binary, ['run', '--recipe', recipeFile, '--bundle', bundle, '--out', runOut], { encoding: 'utf8', timeout: 20 * 60e3 });
  const runJSON = existsSync(join(runOut, 'run.json')) ? JSON.parse(readFileSync(join(runOut, 'run.json'), 'utf8')) : null;
  const verified = spawnSync(binary, ['verify', '--data', dirname(bundle), '--no-page', '--report', join(out, 'run-verified.json'), join(runOut, 'certificate.zip')], { encoding: 'utf8', timeout: 20 * 60e3 });
  const verdict = existsSync(join(out, 'run-verified.json')) ? JSON.parse(readFileSync(join(out, 'run-verified.json'), 'utf8')).verdict : null;
  session1.recipe = { steps: recipe.data.steps.length, seconds: runJSON?.seconds };
  check('the guide\'s session on the simulated tissue, the recipe: its 5 steps from the bundle to the enrichment, no path of this computer in it; made again by situcraft run with the same id, and its certificate verified by situcraft verify', `${recipe.data.steps.map((x) => x.job).join(' → ')}; run exit ${ran.status}, ${runJSON?.result?.id} ${runJSON?.result?.sameAsRecipe ? 'as the recipe\'s' : 'otherwise'}; verify exit ${verified.status}, ${verdict}`,
    recipe.data.steps.length === 5 && !JSON.stringify(recipe.data).includes(temp) && ran.status === 0 && runJSON?.result?.id === enrichment.data.id && runJSON.result.sameAsRecipe && verified.status === 0 && verdict === 'verified');
  writeFileSync(join(out, 'session1.json'), JSON.stringify(session1, null, 1));

  // --- The rest of the tissue's tools ----------------------------------------------------------
  await tool('list_projects');
  const described = await tool('describe_project', {});
  const metrics = (await tool('qc_metrics', {})).data.metrics;
  await tool('set_qc_map', { map: 'cell:transcripts' });
  const chosen = await tool('select_cells', { attribute: 'transcripts', min: 200 });
  const sim001 = await tool('query_cells', { where: { gene: 'SIM001', min: 3 } });
  const transcripts = await tool('query_transcripts', { where: { box: [0, 0, 100, 100] } });
  const region = await tool('summarize_region', { box: [0, 0, 250, 250], labels: labels.data.id });
  check('describe_project, qc_metrics, select_cells, query_cells, query_transcripts and summarize_region: the results named, every recomputed metric agreeing, a gene\'s cells as many as the bundle\'s matrix holds', `${described.data.results.length} results; ${metrics.filter((m) => m.agrees).length} metrics agree; ${chosen.message.slice(0, 50)}; SIM001 ≥ 3 in ${sim001.data.kept} (the matrix: ${cellsWithGene(bundle, 'SIM001', 3)}); ${transcripts.data.kept} transcripts in a box; ${region.message.slice(0, 60)}`,
    described.data.results.some((r) => r.id === enrichment.data.id) && metrics.every((m) => m.agrees !== false) && sim001.data.kept === cellsWithGene(bundle, 'SIM001', 3) && transcripts.data.kept > 0 && region.data);
  const shown = await tool('show_tissue', { fill: 'labels', picture: true, picture_width: 500 });
  const map = await tool('show_map', { picture: true, picture_width: 500 });
  check('show_tissue and show_map with picture: the tissue as a JPEG, the map as a PNG, after the text', `${shown.result.content.map((c) => c.mimeType ?? c.type).join(', ')}; ${map.result.content.map((c) => c.mimeType ?? c.type).join(', ')}`, isJPEG(images(shown)[0]) && isPNG(images(map)[0]) && shown.result.content[0].type === 'text');
  await tool('select_cell', { cell: '#10' });
  await tool('save_view', { name: 'Agent view' });
  await tool('save_view', { name: 'Scratch view' });
  await tool('open_view', { view: 'Agent view' });
  await tool('remove_view', { view: 'Scratch view' });
  await tool('focus', { kind: 'view', name: 'Agent view' });
  await tool('set_mode', { mode: 'segmentation' });
  await tool('segmentations', {});
  const expansion = await tool('expand_nuclei', { distance_um: 3 });
  const pilot = { regions: [[100, 100, 400, 400]], sweeps: 30, samples: 10 };
  await tool('refinement_preflight', pilot);
  const progressFrom = s.arrived.length;
  const refined = await tool('refine_segmentation', pilot, { progressToken: 'refine' });
  const since = s.arrived.slice(progressFrom);
  const replyAt = since.findIndex((m) => m.id === refined.id);
  const progress = since.filter((m) => m.method === 'notifications/progress');
  check('refine_segmentation of a region, a long call with a progress token: answered, any progress under its token and before the answer', `${refined.message.slice(0, 90)}…; ${progress.length} progress notification(s)`, Boolean(refined.data) && progress.every((m) => m.params.progressToken === 'refine') && since.slice(replyAt + 1).every((m) => m.method !== 'notifications/progress'));
  const truthImport = await tool('import_segmentation', { format: 'xenium', paths: [join(bundle, 'truth')], tool: 'the simulator' });
  const expansionName = expansion.data.name ?? 'Nucleus expansion, 3 µm';
  const compared = await tool('compare_segmentations', { a: 'Instrument segmentation', b: expansionName });
  await tool('show_comparison', {});
  await tool('decide_segmentation', { segmentation: expansionName, decision: 'keep', rationale: 'For comparing labels under both', comparison: compared.data.id });
  await tool('plant_errors', {});
  await tool('export_segmentation', { segmentation: expansionName, formats: ['geojson'], folder: join(out, 'expansion') });
  await tool('name_clusters', { names: { 1: 'First cluster' } });
  const truthLabels = await tool('import_labels', { path: join(bundle, 'truth', 'instrument_cell_types.csv'), id_column: 'cell_id', column: 'cell_type' });
  await tool('review_labels', { labels: labels.data.id, limit: 20 });
  await tool('label_evidence', { cell: '#10' });
  await tool('curate_labels', { labels: labels.data.id, cells: ['#10'], label: 'immune-like', reason: 'Its neighbors are immune-like' });
  await tool('review', { action: 'comment', cells: ['#11'], note: 'Looked at by the agent session' });
  await tool('reviews', {});
  await tool('consensus_labels', { members: [labels.data.id, truthLabels.data.id], rule: 'unanimous', name: 'Agreed with the truth' });
  await tool('repeat_on_segmentation', { segmentation: expansionName });
  await tool('label_impact', { comparison: compared.data.id });
  await tool('remake_analysis', { analysis: enrichment.data.id, segmentation: expansionName });
  check('segmentations, an expansion, the truth imported, compared, decided, errors planted and an export; clusters named, the truth\'s labels imported, labels reviewed, curated, reviewed and agreed; the clustering and labels repeated on the expansion, their impact read and the enrichment made again on it', `${truthImport.message.slice(0, 70)}…; ${compared.message.slice(0, 70)}…`, Boolean(truthImport.data && compared.data?.id));

  // Post-Xenium images: the H&E added unaligned, registered, placed by landmarks from its true
  // placement, the registration used, shown and checked.
  const truthImages = JSON.parse(readFileSync(join(bundle, 'truth', 'post_xenium', 'registration.json'), 'utf8')).images;
  const he = truthImages.find((x) => x.kind === 'he');
  const place = ([x, y]) => [he.transform[0][0] * x + he.transform[0][1] * y + he.transform[0][2], he.transform[1][0] * x + he.transform[1][1] * y + he.transform[1][2]];
  await tool('add_image', { path: join(bundle, he.image), alignment: null });
  const registered = await tool('register_image', { model: 'similarity' });
  await tool('align_image', { landmarks: [[300, 300], [1300, 320], [320, 1300], [1300, 1300]].map((q) => ({ moving: q, fixed: place(q) })), use: false, note: 'From the true placement' });
  await tool('use_transform', { transform: registered.data.id ?? registered.data.transform?.id, rationale: 'The automatic registration' });
  await tool('show_alignment', {});
  const checked = await tool('check_alignment', {});
  check('add_image, register_image, align_image, use_transform, show_alignment and check_alignment: the H&E placed and checked', `${registered.message.slice(0, 80)}…; ${checked.message.slice(0, 80)}…`, Boolean(registered.data && checked.data));

  // Regions: drawn, changed, listed, written and read back.
  await tool('draw_region', { rectangle: [100, 100, 200, 200], name: 'A box', class: 'Tumor' });
  await tool('edit_region', { region: 'A box', brush: { points: [[150, 150]], radius: 10, erase: true }, note: 'A hole' });
  const regionsListed = await tool('regions', {});
  const regionsFile = join(out, 'regions.geojson');
  await tool('export', { what: 'regions', path: regionsFile });
  await tool('edit_region', { region: 'A box', remove: true });
  await tool('import_regions', { path: regionsFile });
  const regionsBack = (await tool('regions', {})).data;

  // The record: a figure written and read back, the record and methods, a certificate verified in
  // SituCraft itself, a filter computed again.
  await tool('figure', { name: 'Agent figure', views: ['Agent view'] });
  const figureFile = join(out, 'agent-figure.svg');
  await tool('write_figure', { name: 'Agent figure', path: figureFile, format: 'svg' });
  const readBack = await tool('read_figure', { path: figureFile });
  await tool('record', { result: enrichment.data.id });
  const methods = await tool('methods', { result: enrichment.data.id });
  const certificate = join(out, 'enrichment.certificate.zip');
  await tool('export', { what: 'certificate', result: enrichment.data.id, path: certificate });
  const inSituCraft = await tool('verify_certificate', { path: certificate });
  const filterId = saved.changed.find((c) => c.kind === 'filter').id;
  const records = await tool('verify_records', { steps: [{ id: filterId, kind: 'filter' }] });
  check('regions drawn, erased from, written as GeoJSON and read back; a figure written and read back unchanged; the record and methods; a certificate verified in SituCraft itself; a filter computed again the same', `${regionsListed.data.regions?.length} → ${regionsBack.regions?.length} regions; figure ${readBack.data.intact ? 'intact' : 'changed'}; ${methods.message.slice(0, 50)}…; ${inSituCraft.data.verdict}; filter ${JSON.stringify(records.data.steps?.[0] ?? records.data[0] ?? records.data).slice(0, 80)}`,
    regionsBack.regions?.some((r) => r.name === 'A box') && readBack.data.intact && inSituCraft.data.verdict === 'verified' && (records.data.steps ?? records.data).every?.((x) => x.same !== false));

  // Jobs, a tour, and opening data by path into another project.
  const jobs = (await tool('jobs', {})).data.jobs;
  await tool('jobs', { job: jobs[0].id });
  await tool('tour', { example: 'simulated-standard', name: 'score-against-truth' });
  await tool('tour', { end: true });
  const first = (await tool('get_state')).data.project.name;
  await tool('new_project', { name: 'Agent project' });
  await tool('open_paths', { paths: [bundle] });
  await tool('add_dataset', { bundle: 'simulated-standard' });
  const brought = await tool('add_results', { ids: [clusters.data.id, labels.data.id] });
  await tool('open_project', { project: first });
  check('jobs, a tour started and ended, a project of the agent\'s with the bundle opened by path, added, and results of the library added to it; the first project opened again', `${jobs.length} jobs; ${brought.message.slice(0, 90)}…`, jobs.length > 5 && /added|Added/.test(brought.message));

  // --- The guide's second session: the simulated cohort's genes ------------------------------
  const cohort = await tool('open_example', { example: 'cohort' });
  const samples = (await tool('samples', {})).data.samples;
  await tool('compare_panels', {});
  const design = await tool('design', { unit: 'subject', group: 'condition', reference: 'control', save: 'By condition' });
  await tool('cohort_steps', { markers: 'simulated' });
  const summary = await tool('summarize_samples', {});
  const qc = await tool('cohort_qc', {});
  const expression = await tool('compare_expression', { design: 'By condition' });
  const found = (await tool('get_result', { id: expression.data.id, limit: 1 })).data.summary;
  const tests = table((await tool('get_result', { id: expression.data.id, limit: 0 })).data.table);
  const sim033 = tests.find((t) => t.gene === 'SIM033' && t.label === 'epithelial-like');
  const gene = await tool('show_gene', { gene: 'SIM033', label: 'epithelial-like' });
  const session2 = {
    samples: samples.length,
    tests: found.tests,
    found: found.found.map((f) => `${f.gene} in ${f.label}`),
    logFC: r2(sim033.logFC), lo: r2(sim033.lo), hi: r2(sim033.hi), padj: Number(sim033.padj).toPrecision(2),
    outliers: qc.data.outliers.length,
  };
  check('the guide\'s session on the simulated cohort: eight samples of four control and four treated subjects; the design, the same steps on every sample and their summary, no sample far out; 424 tests, SIM033 in the epithelial-like cells alone found, +0.94 log2 (0.59 to 1.30)', `${cohort.message.slice(0, 60)}…; ${design.data.words?.[1] ?? ''}; ${summary.message.slice(0, 50)}…; ${JSON.stringify(session2)}`,
    session2.samples === 8 && /6 degrees of freedom left/.test(design.data.words.join(' ')) && session2.tests === 424 && session2.found.length === 1 && session2.found[0] === 'SIM033 in epithelial-like' && session2.logFC === '0.94' && session2.lo === '0.59' && session2.hi === '1.30' && session2.outliers === 0);
  writeFileSync(join(out, 'session2.json'), JSON.stringify(session2, null, 1));
  // The rest of the cohort's tools.
  await tool('set_samples', { fields: [{ name: 'batch', kind: 'text' }], values: { S01: { batch: 'first' } } });
  await tool('cohort_table', { family: 'share' });
  await tool('compare_abundance', { design: 'By condition' });
  await tool('compare_spatial', { design: 'By condition' });
  await tool('show_sample_cells', { sample: 'S05', label: 'endothelial-like' });
  const cohortDir = dirname(samples[0].path ?? (await tool('get_state')).data.datasets[0].path);
  await tool('reference_labels', { dataset: 'S01', path: join(cohortDir, 'reference.zarr') });
  check('show_gene, the cohort\'s other comparisons and a reference\'s labels: SIM033\'s units by group shown', gene.message.slice(0, 120), Boolean(gene.data));

  // --- The Lung: the instrument's own clusters -------------------------------------------------
  await tool('open_example', { example: 'lung' });
  const instrument = await tool('instrument_clusters', {});
  check('instrument_clusters on the Lung: the instrument\'s graph-based clustering added', instrument.message.slice(0, 100), Boolean(instrument.data));

  // --- What the session as a whole shows -----------------------------------------------------
  const never = listed.map((t) => t.name).filter((n) => !called.has(n));
  check('every tool called', `${called.size} of ${listed.length}${never.length ? `; never: ${never.join(', ')}` : ''}`, !never.length);
  check('every tool said to read only left the project\'s history as it was', readOnlyChanged.length ? `changed it: ${readOnlyChanged.join(', ')}` : `${[...called].filter((n) => hints.get(n)?.readOnlyHint).length} read-only tools, none changed it`, !readOnlyChanged.length);
  const history = JSON.parse(await page.eval('JSON.stringify(window.situcraft.store.state.labels.past)'));
  check('every change the agent made in the open project carries its name in the history', `${history.filter((l) => l.endsWith(`(by ${AGENT})`)).length} of ${history.length} changes by "${AGENT}"`, history.length > 0 && history.every((l) => l.endsWith(`(by ${AGENT})`)));
  const log = await (await fetch(`${url}/api/calls?by=${encodeURIComponent(AGENT)}&limit=1000`)).json();
  const outcomes = { done: 0, refused: 0, failed: 0 };
  for (const c of log.calls ?? []) outcomes[c.outcome] += 1;
  const leaves = (log.calls ?? []).map((c) => c.leave?.length ?? 0);
  check('every call is a line of the library\'s log, once, under the agent\'s name, as an agent\'s with its leave (none in the read-only session, every leave in this one)', `${log.total} lines: ${JSON.stringify(outcomes)}; sent ${JSON.stringify(logged)}`,
    outcomes.done === logged.done && outcomes.refused + outcomes.failed === logged.notDone && (log.calls ?? []).every((c) => c.via === 'agent') && leaves.filter((n) => n === 0).length === 1 && leaves.filter((n) => n === 3).length === leaves.length - 1);
  check('the page threw nothing', errors.length ? errors[0].slice(0, 200) : 'nothing', !errors.length);
} catch (error) {
  check('the session ran to its end', error.stack ?? error.message, false);
} finally {
  // The agent closes stdin: SituCraft stops, its connection file removed.
  const code = await s.close();
  check('stdout carried nothing but JSON-RPC, and closing stdin stopped SituCraft and removed its connection file', `${s.notJSON.length} other lines; exit ${code}; remote.json ${existsSync(join(library, 'remote.json')) ? 'still there' : 'removed'}`, !s.notJSON.length && code === 0 && !existsSync(join(library, 'remote.json')));
  await browser?.close();
  if (process.env.SITUCRAFT_AGENT_KEEP) console.log(`Kept ${temp}`);
  else rmSync(temp, { recursive: true, force: true });
}

const failed = results.filter((r) => !r.ok);
console.log(`agent-session: ${results.length - failed.length}/${results.length} checks passed`);
process.exit(failed.length ? 1 : 0);
