// Runs the agent benchmark (wave 9, slice 8; requirement A7): each task (benchmark/tasks.mjs) in a
// SituCraft of its own, with the agent under test, graded against the truth; the results are
// written as JSON. Adapted from CytoWeave 0.8.0's benchmark/run.mjs (the same author's).
//
//   node benchmark/run.mjs --agent claude-code --model sonnet [--effort medium] [--repeat 3]
//                          [--tasks id,id] [--out benchmark/results/<file>.json]
//   node benchmark/run.mjs --agent expert --check     # the reference solutions score ≥ 0.9
//   node benchmark/run.mjs --agent none --check       # doing nothing scores 0
//
// Agents: claude-code (Claude Code's own program, logged in as you are: it runs without files,
// shell or web, with SituCraft's tools only), expert (the reference solutions, scripted through the
// same MCP tools, no model) and none (an agent that answers nothing).
// Needs Go (to build SituCraft from this checkout), Chrome, the Lung 2-FOV bundle for its tasks
// (node validation/fetch.mjs xenium-lung-2fov), and for claude-code the claude program.

import { execSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { LUNG, ROOT, buildSituCraft, makeData, mcpClient, openData, runAgent, startSituCraft } from './harness.mjs';
import { BENCHMARK_VERSION, OPEN_DATA, TASKS, parseAnswer } from './tasks.mjs';

const argv = process.argv.slice(2);
const option = (name, fallback = null) => {
  const at = argv.indexOf(`--${name}`);
  return at >= 0 && at + 1 < argv.length && !argv[at + 1].startsWith('--') ? argv[at + 1] : fallback;
};
const agent = option('agent', 'claude-code');
const model = option('model');
const effort = option('effort');
const repeat = Number(option('repeat', '1'));
const maxTurns = Number(option('max-turns', '60'));
const timeoutMs = Number(option('timeout-min', '20')) * 60000;
const check = argv.includes('--check');
const transcripts = option('transcripts');
const only = option('tasks')?.split(',');
const tasks = only ? TASKS.filter((t) => only.includes(t.id)) : TASKS;
if (only && tasks.length !== only.length) {
  console.error(`Unknown task: ${only.filter((id) => !TASKS.some((t) => t.id === id)).join(', ')}. Tasks: ${TASKS.map((t) => t.id).join(', ')}`);
  process.exit(2);
}
if (!['claude-code', 'expert', 'none'].includes(agent)) {
  console.error('--agent is claude-code, expert or none.');
  process.exit(2);
}
if (tasks.some((t) => t.example.kind === 'lung') && !existsSync(join(LUNG, 'experiment.xenium'))) {
  console.error(`The Lung tasks need the Lung 2-FOV bundle in ${LUNG}: fetch it with node validation/fetch.mjs xenium-lung-2fov (10x Genomics, CC BY 4.0, about 280 MB), or leave them out with --tasks.`);
  process.exit(2);
}

const git = (command) => {
  try {
    return execSync(`git ${command}`, { cwd: ROOT, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim();
  } catch {
    return null;
  }
};
const situcraftVersion = /^var version = "([^"]+)"$/m.exec(readFileSync(join(ROOT, 'main.go'), 'utf8'))[1];
const claudeVersion = agent === 'claude-code' ? (() => { try { return execSync('claude --version', { encoding: 'utf8' }).trim(); } catch { return null; } })() : null;

async function runTask(binary, cacheDir, task, rep) {
  const started = Date.now();
  const session = await startSituCraft(binary, { cacheDir });
  try {
    const made = await makeData(session, task.example);
    const truth = await task.truth(made);
    await openData(session, made);
    const context = { task, outputs: session.outputs };
    const prompt = `${OPEN_DATA}\n\n${task.prompt(context)}`;
    let outcome;
    if (agent === 'claude-code') {
      outcome = await runAgent(session, { agent, prompt, model, effort, maxTurns, timeoutMs });
    } else {
      const toolCalls = [];
      let text = '';
      let error = null;
      if (agent === 'expert') {
        const client = await mcpClient(session, 'Benchmark expert');
        const call = async (name, args) => {
          const entry = { name, input: args, ok: false };
          toolCalls.push(entry);
          const result = await client.call(name, args);
          entry.ok = true;
          return result;
        };
        try {
          text = await task.expert(call, context);
        } catch (e) {
          error = e.message;
        }
        await client.close();
      }
      outcome = { answer: text, toolCalls, turns: null, usage: null, costUSD: null, error };
    }
    // The agent's whole session (each message and tool call), for reading what it did.
    if (transcripts && outcome.events) {
      mkdirSync(transcripts, { recursive: true });
      writeFileSync(join(transcripts, `${task.id}-${rep}.jsonl`), `${outcome.events.map((e) => JSON.stringify(e)).join('\n')}\n`);
    }
    const answer = parseAnswer(outcome.answer);
    // What the agent left, read through SituCraft's tools: the project "Experiment", whichever the
    // page shows. An agent that opened other data (an example, files) analyzed another experiment
    // than the task's.
    const grader = await mcpClient(session, 'Benchmark grader');
    let graded;
    let replacedWorkspace;
    try {
      const state = (await grader.call('get_state', {})).data;
      replacedWorkspace = state.project?.name !== 'Experiment' || state.datasets?.length !== made.paths.length;
      graded = await task.grade({ answer, text: outcome.answer, call: grader.call, truth, outputs: session.outputs, toolCalls: outcome.toolCalls, session, made });
    } finally {
      await grader.close();
    }
    const score = graded.parts.reduce((a, p) => a + p.weight * p.score, 0);
    return {
      task: task.id,
      category: task.category,
      repeat: rep,
      score: +score.toFixed(4),
      parts: graded.parts.map((p) => ({ ...p, score: +p.score.toFixed(4) })),
      answer: answer ?? null,
      answered: Boolean(answer),
      replacedWorkspace,
      toolCalls: outcome.toolCalls.length,
      failedToolCalls: outcome.toolCalls.filter((c) => c.ok === false).length,
      tools: [...new Set(outcome.toolCalls.map((c) => c.name))],
      turns: outcome.turns,
      tokens: outcome.usage ? { input: outcome.usage.input_tokens, output: outcome.usage.output_tokens, cacheRead: outcome.usage.cache_read_input_tokens, cacheWrite: outcome.usage.cache_creation_input_tokens } : null,
      costUSD: outcome.costUSD,
      model: outcome.model ?? null,
      timedOut: outcome.timedOut ?? false,
      error: outcome.error ?? null,
      seconds: Math.round((Date.now() - started) / 1000),
      reply: String(outcome.answer ?? '').slice(-2000),
    };
  } finally {
    await session.stop();
  }
}

const binFolder = mkdtempSync(join(tmpdir(), 'situcraft-bench-bin-'));
// The bundles' derived data, shared by the runs (it depends on a bundle's files alone; the results
// each run makes are in its own library).
const cacheDir = mkdtempSync(join(tmpdir(), 'situcraft-bench-cache-'));
const binary = await buildSituCraft(binFolder);
const runs = [];
for (const task of tasks) {
  for (let rep = 1; rep <= repeat; rep += 1) {
    process.stdout.write(`${task.id}${repeat > 1 ? ` #${rep}` : ''} … `);
    // A run the harness failed (a browser that hung, SituCraft that did not start) is not the
    // agent's: it is run again, up to twice, and the attempts are recorded.
    let row;
    for (let attempt = 1; attempt <= 3; attempt += 1) {
      try {
        row = { ...(await runTask(binary, cacheDir, task, rep)), attempts: attempt };
        break;
      } catch (error) {
        row = { task: task.id, category: task.category, repeat: rep, score: 0, parts: [], attempts: attempt, error: `harness: ${error.message}` };
        if (attempt < 3) console.log(`(harness: ${error.message}; running it again)`);
      }
    }
    runs.push(row);
    console.log(`${row.score.toFixed(2)}${row.error ? ` (${row.error})` : ''}${row.toolCalls !== undefined ? `, ${row.toolCalls} tool calls` : ''}${row.seconds ? `, ${row.seconds} s` : ''}`);
    for (const p of row.parts) if (p.score < 1) console.log(`    ${p.score.toFixed(2)} × ${p.weight}  ${p.name}: ${p.detail}`);
  }
}
rmSync(binFolder, { recursive: true, force: true });
rmSync(cacheDir, { recursive: true, force: true });

const mean = (xs) => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : Number.NaN);
const byTask = Object.fromEntries(tasks.map((t) => [t.id, +mean(runs.filter((r) => r.task === t.id).map((r) => r.score)).toFixed(4)]));
const overall = mean(Object.values(byTask));
const result = {
  benchmark: { version: BENCHMARK_VERSION, tasks: tasks.map((t) => ({ id: t.id, category: t.category, title: t.title, data: t.example.kind, seed: t.example.seed ?? null })) },
  agent: { id: agent, program: claudeVersion, model: runs.find((r) => r.model)?.model ?? model, requestedModel: model, effort, maxTurns },
  // Results files are not part of what is measured: earlier results waiting to be committed do not
  // make a run's SituCraft or benchmark modified.
  situcraft: { version: situcraftVersion, commit: git('rev-parse --short HEAD'), dirty: Boolean(git('status --porcelain -- . ":(exclude)benchmark/results"')) },
  date: new Date().toISOString(),
  repeat,
  score: +overall.toFixed(4),
  byTask,
  runs,
};
console.log(`\n${agent}${model ? ` (${model}${effort ? `, ${effort}` : ''})` : ''}: ${overall.toFixed(3)} over ${tasks.length} task${tasks.length === 1 ? '' : 's'}${repeat > 1 ? ` × ${repeat}` : ''}`);

const out = option('out') ?? (check ? null : join(ROOT, 'benchmark', 'results', `${new Date().toISOString().slice(0, 10)}-${agent}${model ? `-${model}` : ''}${effort ? `-${effort}` : ''}.json`));
if (out) {
  mkdirSync(dirname(out), { recursive: true });
  writeFileSync(out, `${JSON.stringify(result, null, 1)}\n`);
  console.log(`Wrote ${out}`);
}
if (check) {
  const failed = runs.filter((r) => /^harness/.test(r.error ?? '') || (agent === 'none' ? r.score !== 0 : r.score < 0.9));
  if (failed.length) {
    console.log(`Check failed: ${failed.map((r) => `${r.task} ${r.score}`).join(', ')} (${agent === 'none' ? 'doing nothing must score 0' : 'the reference solution must score at least 0.9'}).`);
    process.exit(1);
  }
  console.log(agent === 'none' ? 'Check passed: doing nothing scores 0 on every task.' : 'Check passed: every reference solution scores at least 0.9.');
}
// Everything is written: exit even if a failed run left a handle open.
process.exit(0);
