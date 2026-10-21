// The benchmark harness: SituCraft as an agent meets it, and the agent under test (wave 9, slice 8;
// requirement A7).
//
// For each run, the harness starts "situcraft mcp" itself (built from this checkout) with an empty
// library and every leave (--allow all), opens its page in a headless Chrome, makes the task's data
// (a simulated tissue or cohort with the task's seed and parameters, or the Lung bundle), and opens
// it as the project "Experiment" through SituCraft's own tools. The simulator's truth is read and
// then removed from the bundles before they are opened, so that no tool can read it. The MCP
// conversation is served on a local socket, one conversation after another: the harness's, which
// prepares the project; the agent's, a separate program whose only MCP server is
// benchmark/mcp-relay.mjs on that socket, working from an empty folder through SituCraft's tools in
// the page the harness watches; and the grader's, which reads what the agent left in the project.
// Adapted from CytoWeave 0.8.0's benchmark/harness.mjs (the same author's); the data, the truth's
// removal and the conversations in turn are SituCraft's.

import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { connect, createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { launch, sleep } from '../validation/cdp.mjs';

export const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
// The Lung 2-FOV bundle as the validation suite fetches it (node validation/fetch.mjs xenium-lung-2fov).
export const LUNG = join(ROOT, 'validation', 'cache', 'xenium-lung-2fov');
const RELAY = join(ROOT, 'benchmark', 'mcp-relay.mjs');
const PAGE_TIMEOUT = 10 * 60 * 1000;
const JOBS_TIMEOUT = 15 * 60 * 1000;

// A promise that rejects after ms (the work is not stopped; the run is).
function withTimeout(promise, ms, what) {
  let timer;
  return Promise.race([promise, new Promise((_, fail) => { timer = setTimeout(() => fail(new Error(`${what} within ${Math.round(ms / 1000)} s.`)), ms); })]).finally(() => clearTimeout(timer));
}

// Runs the binary with arguments; resolves with its standard output, rejects with its error.
export function runBinary(binary, args, options = {}) {
  return new Promise((done, fail) => {
    const child = spawn(binary, args, { stdio: ['ignore', 'pipe', 'pipe'], ...options });
    let out = '';
    let err = '';
    child.stdout.on('data', (chunk) => { out += chunk; });
    child.stderr.on('data', (chunk) => { err += chunk; });
    child.on('error', fail);
    child.on('exit', (code) => (code ? fail(Object.assign(new Error(`${args[0]} failed (${code}): ${err.trim().slice(-2000)}`), { code, out, err })) : done(out)));
  });
}
// SituCraft built from this checkout into a folder: the binary's path.
export async function buildSituCraft(folder) {
  const binary = join(folder, process.platform === 'win32' ? 'situcraft.exe' : 'situcraft');
  await new Promise((done, fail) => spawn('go', ['build', '-o', binary, '.'], { cwd: ROOT, stdio: 'inherit' })
    .on('exit', (code) => (code ? fail(new Error('go build failed (is Go installed?)')) : done())));
  return binary;
}

// One run's SituCraft: "situcraft mcp" with its page in a headless Chrome. Returns { url, page (code
// run in the page with app bound), waitFor, mcpConfig (the agent's MCP configuration file),
// socketPath, workdir (the agent's empty working folder), outputs (a folder the task may name),
// data (where the task's bundles are made), binary, cacheDir, stop }.
export async function startSituCraft(binary, { cacheDir, width = 1400, height = 900 } = {}) {
  const temp = mkdtempSync(join(tmpdir(), 'situcraft-bench-'));
  const workdir = join(temp, 'work');
  const outputs = join(temp, 'outputs');
  const data = join(temp, 'data');
  for (const folder of [workdir, outputs, data]) mkdirSync(folder);
  const cache = cacheDir ?? join(temp, 'cache');
  const mcp = spawn(binary, ['mcp', '--allow', 'all', '--window', 'none', '--data-dir', join(temp, 'library'), '--cache-dir', cache, '--examples-dir', join(temp, 'examples')], { stdio: ['pipe', 'pipe', 'pipe'] });
  let log = '';
  mcp.stderr.on('data', (chunk) => { log += chunk; });
  // The MCP conversations, one at a time, each relayed to and from situcraft mcp: a connection
  // made while another is open is refused.
  const socketPath = process.platform === 'win32' ? `\\\\.\\pipe\\situcraft-bench-${process.pid}-${Date.now()}` : join(temp, 'mcp.sock');
  let current = null;
  const server = createServer((socket) => {
    if (current) {
      socket.destroy();
      return;
    }
    current = socket;
    socket.pipe(mcp.stdin, { end: false });
    mcp.stdout.pipe(socket);
    socket.on('error', () => {});
    socket.on('close', () => {
      socket.unpipe(mcp.stdin);
      mcp.stdout.unpipe(socket);
      if (current === socket) current = null;
    });
  });
  await new Promise((done) => server.listen(socketPath, done));
  // A page that fails to start (a Chrome that hangs, say) must leave nothing running: the relay's
  // socket and situcraft mcp would keep the benchmark from exiting.
  let browser = null;
  try {
    const mcpConfig = join(temp, 'mcp.json');
    writeFileSync(mcpConfig, JSON.stringify({ mcpServers: { situcraft: { command: process.execPath, args: [RELAY, socketPath] } } }, null, 1));
    let url = null;
    for (let i = 0; i < 300 && !url; i += 1) {
      url = /is running at (http:\/\/[\d.]+:\d+)/.exec(log)?.[1] ?? null;
      if (!url) await sleep(100);
    }
    if (!url) throw new Error(`situcraft mcp did not start: ${log.trim()}`);
    browser = await launch({ width, height, args: (process.env.SITUCRAFT_CHROME_ARGS ?? '').split(' ').filter(Boolean) });
    await withTimeout(browser.goto(`${url}/`), 60000, 'The SituCraft page did not open');
    // Every call into the page has a deadline: a browser that hangs must fail the run, not stall
    // the benchmark.
    const page = (code, timeout = PAGE_TIMEOUT) => withTimeout(browser.eval(`(async () => { const app = window.situcraft; ${code} })()`), timeout, 'The SituCraft page did not answer');
    const waitFor = async (expression, timeout = 240000) => {
      const start = Date.now();
      while (Date.now() - start < timeout) {
        if (await withTimeout(browser.eval(expression), Math.max(1000, timeout - (Date.now() - start)), 'The SituCraft page did not answer')) return;
        await sleep(300);
      }
      throw new Error(`Timed out waiting for ${expression}`);
    };
    await waitFor('Boolean(window.situcraft?.remote)', 60000);
    const stop = async () => {
      await browser.close().catch(() => {});
      server.close();
      mcp.stdin.end();
      const exited = new Promise((done) => mcp.once('exit', done));
      await Promise.race([exited, sleep(5000)]);
      mcp.kill();
      rmSync(temp, { recursive: true, force: true, maxRetries: 5 });
    };
    return { url, page, waitFor, mcpConfig, socketPath, workdir, outputs, data, binary, cacheDir: cache, log: () => log, stop };
  } catch (error) {
    await browser?.close().catch(() => {});
    server.close();
    mcp.kill();
    rmSync(temp, { recursive: true, force: true, maxRetries: 5 });
    throw error;
  }
}

// A scripted MCP client on the session's socket, as an agent's program would be (the harness's
// preparation, the expert reference solutions, the graders): call(name, args) resolves with
// { message, data, changed } or rejects with the tool's error.
export async function mcpClient(session, clientName = 'Benchmark expert') {
  const socket = connect(session.socketPath);
  await new Promise((done, fail) => { socket.once('connect', done); socket.once('error', fail); });
  let buffer = '';
  let next = 1;
  const pending = new Map();
  socket.on('data', (chunk) => {
    buffer += chunk;
    let at;
    while ((at = buffer.indexOf('\n')) >= 0) {
      const line = buffer.slice(0, at).trim();
      buffer = buffer.slice(at + 1);
      if (!line) continue;
      const message = JSON.parse(line);
      const waiting = pending.get(message.id);
      if (!waiting) continue;
      pending.delete(message.id);
      if (message.error) waiting.fail(new Error(message.error.message));
      else waiting.done(message.result);
    }
  });
  const closed = new Promise((done) => socket.once('close', done));
  socket.on('close', () => {
    for (const waiting of pending.values()) waiting.fail(new Error('The MCP connection closed.'));
    pending.clear();
  });
  const request = (method, params) => new Promise((done, fail) => {
    const id = next++;
    pending.set(id, { done, fail });
    socket.write(`${JSON.stringify({ jsonrpc: '2.0', id, method, params })}\n`);
  });
  await request('initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: clientName, version: '1' } });
  socket.write(`${JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' })}\n`);
  return {
    async call(name, args = {}) {
      const result = await request('tools/call', { name, arguments: args });
      if (result.isError) throw new Error(`${name}: ${result.content?.[0]?.text ?? 'failed'}`);
      return result.structuredContent ?? { message: result.content?.[0]?.text ?? '' };
    },
    // Ends the conversation and waits until the harness's end of it is free for the next.
    async close() {
      socket.end();
      await Promise.race([closed, sleep(5000)]);
      await sleep(100);
    },
  };
}

// --- The task's data -------------------------------------------------------------------------------

// A simulated tissue or cohort made by "situcraft simulate" into folder: the preset's parameters
// (or a cohort's), changed by params when given, and the seed.
async function simulate(binary, folder, { cohort, preset = 'standard', seed, params }) {
  const base = cohort ? ['simulate', '--cohort', cohort] : ['simulate', '--preset', preset];
  const args = [...base, '--seed', String(seed), '--quiet'];
  if (params) {
    const file = `${folder}-params.json`;
    writeFileSync(file, JSON.stringify(params(JSON.parse(await runBinary(binary, [...base, '--print-params'])))));
    args.push('--params', file);
  }
  await runBinary(binary, [...args, folder]);
}

const readJSON = (path) => JSON.parse(readFileSync(path, 'utf8'));

// Makes the task's data in the session's data folder and takes the simulator's truth out of it.
// Answers what was made: { kind, paths (the bundles, in order), samples (a cohort's subjects and
// conditions as CSV), truth (what the simulator recorded, read before it was removed) }.
export async function makeData(session, example) {
  if (example.kind === 'lung') {
    if (!existsSync(join(LUNG, 'experiment.xenium'))) throw Object.assign(new Error(`The Lung bundle is not in ${LUNG}: fetch it first with node validation/fetch.mjs xenium-lung-2fov (10x Genomics, CC BY 4.0, about 280 MB).`), { setup: true });
    return { kind: 'lung', paths: [LUNG], samples: null, truth: { folder: LUNG } };
  }
  if (example.kind === 'tissue') {
    const folder = join(session.data, 'section');
    await simulate(session.binary, folder, example);
    const truth = {
      cellTypes: readFileSync(join(folder, 'truth', 'instrument_cell_types.csv'), 'utf8'),
      simulation: readJSON(join(folder, 'situcraft-simulation.json')),
    };
    rmSync(join(folder, 'truth'), { recursive: true, force: true });
    return { kind: 'tissue', paths: [folder], samples: null, truth };
  }
  if (example.kind === 'cohort') {
    const folder = join(session.data, 'cohort');
    await simulate(session.binary, folder, example);
    const truth = { cohort: readJSON(join(folder, 'truth', 'cohort.json')) };
    const samples = readFileSync(join(folder, 'samples.csv'), 'utf8');
    const names = readdirSync(folder).filter((name) => existsSync(join(folder, name, 'experiment.xenium'))).sort();
    // The cohort's own record names its effects; each sample's truth names its cells' types.
    for (const name of ['truth', 'samples.csv', 'situcraft-cohort.json']) rmSync(join(folder, name), { recursive: true, force: true });
    for (const name of names) rmSync(join(folder, name, 'truth'), { recursive: true, force: true });
    return { kind: 'cohort', paths: names.map((name) => join(folder, name)), samples, truth };
  }
  throw new Error(`No kind of data "${example.kind}".`);
}

// Waits until SituCraft has no job queued or running (the bundles' derived data, say).
export async function waitForJobs(client, timeout = JOBS_TIMEOUT) {
  const start = Date.now();
  while (Date.now() - start < timeout) {
    const { jobs } = (await client.call('jobs', {})).data;
    if (!jobs.some((j) => j.state === 'queued' || j.state === 'running')) return;
    await sleep(1000);
  }
  throw new Error('SituCraft\'s jobs did not finish.');
}

// Opens what was made as the project "Experiment", as a user would have it: the bundles added
// (their derived data built), a cohort's samples given their subjects and conditions.
export async function openData(session, made) {
  const client = await mcpClient(session, 'Benchmark harness');
  try {
    await client.call('new_project', { name: 'Experiment' });
    await client.call('open_paths', { paths: made.paths, add: true });
    if (made.samples) await client.call('set_samples', { table: made.samples });
    await waitForJobs(client);
    await client.call('set_mode', { mode: 'data' });
  } finally {
    await client.close();
  }
}

// --- Agents ---------------------------------------------------------------------------------------

// The environment of the agent's program: this program's, without what marks a Claude Code session
// (the agent runs as a session of its own, not one nested in another).
function agentEnvironment() {
  const env = { ...process.env };
  for (const key of Object.keys(env)) if (/^CLAUDE_?CODE|^CLAUDECODE$/.test(key)) delete env[key];
  return env;
}

// Agents the harness can run: each turns a run's settings into a command, and its output into
// { answer, turns, toolCalls: [{ name, input, ok }], usage, costUSD, durationMs, model, error }.
export const AGENTS = {
  'claude-code': {
    label: 'Claude Code',
    command({ prompt, model, effort, mcpConfig, maxTurns }) {
      const args = [
        '-p', prompt,
        '--output-format', 'stream-json', '--verbose',
        '--mcp-config', mcpConfig, '--strict-mcp-config',
        // No built-in tools (no files, shell or web): SituCraft's tools only.
        '--tools', '',
        '--allowedTools', 'mcp__situcraft',
        '--permission-mode', 'dontAsk',
        // Settings of this folder only (an empty one): not the user's hooks, plugins or defaults.
        '--setting-sources', 'project',
        '--disable-slash-commands',
        '--no-session-persistence',
      ];
      if (model) args.push('--model', model);
      if (effort) args.push('--effort', effort);
      if (maxTurns) args.push('--max-turns', String(maxTurns));
      return { command: 'claude', args };
    },
    parse(stdout) {
      const events = stdout.split('\n').filter((l) => l.trim().startsWith('{')).map((l) => {
        try {
          return JSON.parse(l);
        } catch {
          return null;
        }
      }).filter(Boolean);
      const calls = new Map();
      for (const e of events) {
        for (const part of e.message?.content ?? []) {
          if (e.type === 'assistant' && part.type === 'tool_use') calls.set(part.id, { name: part.name.replace(/^mcp__situcraft__/, ''), input: part.input ?? null, ok: null });
          if (e.type === 'user' && part.type === 'tool_result' && calls.has(part.tool_use_id)) calls.get(part.tool_use_id).ok = !part.is_error;
        }
      }
      const result = events.findLast((e) => e.type === 'result');
      const init = events.find((e) => e.type === 'system' && e.subtype === 'init');
      return {
        answer: result?.result ?? '',
        turns: result?.num_turns ?? null,
        toolCalls: [...calls.values()],
        usage: result?.usage ?? null,
        costUSD: result?.total_cost_usd ?? null,
        durationMs: result?.duration_ms ?? null,
        model: init?.model ?? null,
        tools: init?.tools?.length ?? null,
        mcp: init?.mcp_servers ?? null,
        error: result?.is_error ? result.subtype : null,
        events,
      };
    },
  },
};

// Runs an agent on a prompt against a session; resolves with its parsed output and exit code.
export function runAgent(session, { agent = 'claude-code', prompt, model, effort, maxTurns = 60, timeoutMs = 20 * 60 * 1000 }) {
  const adapter = AGENTS[agent];
  if (!adapter) throw new Error(`Unknown agent ${agent}: ${Object.keys(AGENTS).join(', ')}`);
  const { command, args } = adapter.command({ prompt, model, effort, mcpConfig: session.mcpConfig, maxTurns });
  return new Promise((done) => {
    const started = Date.now();
    const child = spawn(command, args, { cwd: session.workdir, env: agentEnvironment(), stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk; });
    child.stderr.on('data', (chunk) => { stderr += chunk; });
    const timer = setTimeout(() => child.kill('SIGTERM'), timeoutMs);
    child.on('error', (error) => { stderr += error.message; });
    child.on('close', (code, signal) => {
      clearTimeout(timer);
      const parsed = adapter.parse(stdout);
      done({ ...parsed, exitCode: code, timedOut: signal === 'SIGTERM' && Date.now() - started >= timeoutMs, wallMs: Date.now() - started, stderr: stderr.slice(-4000) });
    });
  });
}
