// The benchmark's results as HTML for the website (docs/site/build.mjs replaces <benchmark-results>
// with it): every results file in benchmark/results/, a leaderboard of agents and models with the
// reference solution and an agent that does nothing as the bounds, and each task's mean score.
// Adapted from CytoWeave 0.8.0's benchmark/report.mjs (the same author's).

import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { TASKS } from './tasks.mjs';

const esc = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
// One decimal (99.7% is not 100%), none when whole.
const pct = (v) => (Number.isFinite(v) ? `${+(100 * v).toFixed(1)}%` : '—');
// "claude-opus-5-5" → "Claude Opus 5.5", "claude-haiku-4-5-20251001" → "Claude Haiku 4.5".
const modelName = (id) => {
  const m = /^claude-([a-z]+)-(\d+)-(\d+)(?:-\d{8})?$/.exec(String(id ?? ''));
  return m ? `Claude ${m[1][0].toUpperCase()}${m[1].slice(1)} ${m[2]}.${m[3]}` : String(id ?? '');
};
const AGENT_LABELS = { 'claude-code': 'Claude Code' };
const mean = (xs) => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : Number.NaN);

export function readResults(folder) {
  if (!existsSync(folder)) return [];
  return readdirSync(folder).filter((f) => f.endsWith('.json')).sort().map((f) => ({ file: f, ...JSON.parse(readFileSync(join(folder, f), 'utf8')) }));
}

function label(r) {
  if (r.agent.id === 'expert') return 'Reference solution (scripted, no model)';
  if (r.agent.id === 'none') return 'No answer (lower bound)';
  return `${AGENT_LABELS[r.agent.id] ?? r.agent.id}, ${modelName(r.agent.model ?? r.agent.requestedModel)}${r.agent.effort ? ` (${r.agent.effort} effort)` : ''}`;
}

export function benchmarkHTML(folder) {
  const results = readResults(folder);
  if (!results.length) return '<p>No results yet.</p>';
  // The reference solution first, then agents by score, then the agent that answers nothing.
  const ordered = [...results].sort((a, b) => (b.agent.id === 'expert') - (a.agent.id === 'expert') || (a.agent.id === 'none') - (b.agent.id === 'none') || b.score - a.score);
  const board = ordered.map((r) => {
    const runs = r.runs ?? [];
    const calls = mean(runs.filter((x) => Number.isFinite(x.toolCalls)).map((x) => x.toolCalls));
    const cost = runs.some((x) => Number.isFinite(x.costUSD)) ? mean(runs.map((x) => x.costUSD ?? 0)) : null;
    const answered = runs.filter((x) => x.answered).length;
    const replaced = runs.filter((x) => x.replacedWorkspace).length;
    return `<tr><td>${esc(label(r))}</td><td class="r"><strong>${pct(r.score)}</strong></td><td class="r">${r.benchmark.tasks.length} × ${r.repeat}</td><td class="r">${answered} of ${runs.length}${replaced ? ` (${replaced} on other data)` : ''}</td><td class="r">${Number.isFinite(calls) ? calls.toFixed(1) : '—'}</td><td class="r">${cost === null ? '—' : `$${cost.toFixed(2)}`}</td><td class="nowrap">${esc(r.date.slice(0, 10))}</td><td class="nowrap">${esc(`${r.situcraft.version}${r.situcraft.commit ? ` (${r.situcraft.commit}${r.situcraft.dirty ? '+' : ''})` : ''}`)}</td></tr>`;
  }).join('\n');
  const columns = ordered.filter((r) => r.agent.id !== 'none');
  const taskRows = TASKS.map((t) => `<tr><td><strong>${esc(t.title)}</strong><br><span class="muted">${esc(t.category)}</span></td>${columns.map((r) => `<td class="r">${r.byTask?.[t.id] === undefined ? '—' : pct(r.byTask[t.id])}</td>`).join('')}</tr>`).join('\n');
  return `<div class="table-wrap">
        <table>
          <thead><tr><th>Agent and model</th><th class="r">Score</th><th class="r">Tasks × runs</th><th class="r">Answered</th><th class="r">Tool calls per run</th><th class="r">Cost per run</th><th>Run on</th><th>SituCraft</th></tr></thead>
          <tbody>
${board}
          </tbody>
        </table>
      </div>
      <h3 id="by-task">By task</h3>
      <div class="table-wrap">
        <table>
          <thead><tr><th>Task</th>${columns.map((r) => `<th class="r">${esc(label(r).replace(/^Claude Code, /, ''))}</th>`).join('')}</tr></thead>
          <tbody>
${taskRows}
          </tbody>
        </table>
      </div>`;
}
