// The benchmark's tasks (wave 9, slice 8; requirement A7). Each is an analysis a user would ask an
// agent for, on data whose answer is known: a simulated tissue or cohort made with a seed of its
// own and, where it carries a planted effect, with that effect moved from the examples' (another
// gene, cell type or sample), so that an answer cannot be recalled from the examples' descriptions;
// or the Lung bundle, whose answers are read from its own CSV copies of the tables, independently of
// SituCraft's readers. Worded as a user would word them, with the tools left to the agent. Each
// task has:
//   example: what the harness makes and opens (harness.mjs makeData): { kind: tissue, cohort or
//     lung, cohort (its preset), seed, params (the simulator's parameters, changed) }
//   prompt(context): the request (context.outputs: a folder the agent may write to)
//   truth(made): the answer, from the simulator's own record of what it made, or the bundle's files
//   grade({ answer, text, call, truth, outputs, toolCalls, session, made }): { parts: [{ name,
//     weight, score, detail }] }, scores between 0 and 1; a run's score is their weighted sum. call
//     reads the project through SituCraft's tools, after the agent is done.
//   expert(call, context): the reference solution, through the same MCP tools; returns the reply
// Grading is deterministic: from what the agent left in the project, the files it wrote and the
// answer line the prompt asks for, never by a model. Adapted from CytoWeave 0.8.0's
// benchmark/tasks.mjs (the same author's); the tasks are SituCraft's.

import { createReadStream, existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { createInterface } from 'node:readline';
import { createGunzip } from 'node:zlib';
import { runBinary } from './harness.mjs';

export const BENCHMARK_VERSION = '1';

// What every request starts with: where the data are, as a user would say it.
export const OPEN_DATA = 'The data for this are already open in SituCraft, in the project "Experiment": analyze them there.';

// The panel every simulated cohort here shares: the examples' (situcraft simulate --cohort), so that
// its genes are those whose levels are known (SIM033, SIM045, SIM072 and SIM090 the most expressed
// of the genes no type is marked by).
const PANEL_SEED = 20261019;

// --- Answers and scores ---------------------------------------------------------------------------

const answerLine = (shape) => `When you are done, end your reply with one line that starts with ANSWER: followed by JSON of this form: ${shape}`;

// The JSON after the last "ANSWER:" of a reply (in or out of a code block), or null.
export function parseAnswer(text) {
  const at = String(text ?? '').lastIndexOf('ANSWER:');
  if (at < 0) return null;
  const rest = text.slice(at + 7).replace(/^[\s`]*(json)?/i, '');
  const start = rest.indexOf('{');
  if (start < 0) return null;
  let depth = 0;
  for (let i = start; i < rest.length; i += 1) {
    if (rest[i] === '{') depth += 1;
    else if (rest[i] === '}') {
      depth -= 1;
      if (depth === 0) {
        try {
          return JSON.parse(rest.slice(start, i + 1));
        } catch {
          return null;
        }
      }
    }
  }
  return null;
}

const clamp = (v) => Math.max(0, Math.min(1, v));
// 1 at an error up to `full`, falling linearly to 0 at `zero`.
const closeness = (error, full, zero) => (Number.isFinite(error) ? clamp(1 - (error - full) / (zero - full)) : 0);
const number = (v) => (typeof v === 'number' ? v : Number.parseFloat(String(v ?? '').replace(/[^\d.eE+-]/g, '')));
const norm = (s) => String(s ?? '').toLowerCase().replace(/[\s_-]+/g, ' ').trim();
const part = (name, weight, score, detail) => ({ name, weight, score: clamp(score), detail });
const list = (v) => (Array.isArray(v) ? v : []);
const mean = (xs) => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : 0);
const percent = (v) => `${(100 * v).toFixed(1)}%`;

// A simulated type's stem ("immune-like" → "immune"): a label or a name answered is that type when
// it holds the stem ("Immune cells", "immune-like").
const stem = (type) => norm(type).replace(/ like$/, '');
const isType = (name, type) => Boolean(name) && norm(name).includes(stem(type));
// The answer's value for each type, from an object keyed by the agent's names for them.
function byType(object, types) {
  const entries = Object.entries(object && typeof object === 'object' ? object : {});
  return Object.fromEntries(types.map((t) => [t, entries.find(([k]) => isType(k, t))?.[1]]));
}
const isUp = (v) => /^(up|increase|increased|higher|raised|more)\b/.test(norm(v));
const isDown = (v) => /^(down|decrease|decreased|lower|reduced|fewer|less)\b/.test(norm(v));
const direction = (fold) => (fold > 1 ? 'up' : 'down');
const sameDirection = (said, fold) => (fold > 1 ? isUp(said) : isDown(said));

// --- The project, read after the agent ------------------------------------------------------------

const PROJECT = 'Experiment';

// The project's results ({ id, kind, name, … }), as describe_project lists them.
async function results(call) {
  return list((await call('describe_project', { project: PROJECT })).data?.results);
}

// The ids of the cells a tree keeps, every page of them.
async function cellIds(call, where) {
  const ids = [];
  for (let offset = 0; ; ) {
    const data = (await call('query_cells', { project: PROJECT, where, limit: 0, offset })).data;
    const at = data.rows.columns.indexOf('id');
    for (const row of data.rows.rows) ids.push(row[at]);
    offset += data.rows.rows.length;
    if (!data.rows.rows.length || offset >= data.kept) return ids;
  }
}

// How much of the truth a label set gets right: the share of the cells whose label (at its first
// level) is their true type.
async function agreement(call, id, truth) {
  const table = (await call('get_result', { id, limit: 0 })).data?.table;
  if (!table) return null;
  const level = table.columns.indexOf('level');
  const label = table.columns.indexOf('label');
  const first = table.rows[0]?.[level];
  let right = 0;
  for (const row of table.rows.filter((r) => r[level] === first)) {
    const name = row[label];
    const type = truth.types.find((t) => isType(name, t));
    if (!type) continue;
    for (const cell of await cellIds(call, { labels: id, level: 0, in: [name] })) if (truth.types[truth.typeOf.get(cell)] === type) right += 1;
  }
  return right / truth.typeOf.size;
}

// A comparison's design terms, from its record.
async function designTerms(call, id) {
  return list((await call('get_result', { id, limit: 1 })).data?.parameters?.design?.names);
}

// --- Truth ----------------------------------------------------------------------------------------

// The instrument's cells' true types (truth/instrument_cell_types.csv): { types, typeOf (cell id →
// index of its type), percent (each type's percentage of the cells) }.
function cellTypeTruth(made) {
  const types = made.truth.simulation.parameters.types.map((t) => t.name);
  const typeOf = new Map();
  for (const line of made.truth.cellTypes.trim().split('\n').slice(1)) {
    const [id, , type] = line.split(',');
    if (types.includes(type)) typeOf.set(id, types.indexOf(type));
  }
  const counts = types.map((_, k) => [...typeOf.values()].filter((v) => v === k).length);
  return { types, typeOf, percent: Object.fromEntries(types.map((t, k) => [t, (100 * counts[k]) / typeOf.size])) };
}

// A gzipped CSV's rows, each as an object of its header's columns (quotes taken off).
async function* csvRows(path) {
  const lines = createInterface({ input: createReadStream(path).pipe(createGunzip()), crlfDelay: Infinity });
  let header = null;
  for await (const line of lines) {
    const fields = line.split(',').map((f) => f.replace(/^"|"$/g, ''));
    if (!header) header = fields;
    else yield Object.fromEntries(header.map((h, i) => [h, fields[i]]));
  }
}

// The Lung bundle's numbers, from its CSV copies of its tables and its metrics, not SituCraft's
// readers: each cell's transcripts of some genes (quality 20 or more, as Xenium counts them), each
// cell's transcripts, and the run's recorded cells and median transcripts per cell. Read once.
const lungCache = new Map();
function lungTruth(folder) {
  if (!lungCache.has(folder)) {
    lungCache.set(folder, (async () => {
      const genes = new Set(['VWF', 'PLVAP', 'EPCAM', 'ACE']);
      const counts = new Map();
      for await (const t of csvRows(join(folder, 'transcripts.csv.gz'))) {
        if (!genes.has(t.feature_name) || t.cell_id === 'UNASSIGNED' || Number(t.qv) < 20) continue;
        const cell = counts.get(t.cell_id) ?? {};
        cell[t.feature_name] = (cell[t.feature_name] ?? 0) + 1;
        counts.set(t.cell_id, cell);
      }
      const transcripts = [];
      for await (const c of csvRows(join(folder, 'cells.csv.gz'))) transcripts.push(Number(c.transcript_counts));
      const [header, values] = readFileSync(join(folder, 'metrics_summary.csv'), 'utf8').trim().split('\n').map((l) => l.split(','));
      const metric = (name) => Number(values[header.indexOf(name)]);
      return { counts, transcripts, cellsDetected: metric('num_cells_detected'), medianTranscripts: metric('median_transcripts_per_cell') };
    })());
  }
  return lungCache.get(folder);
}

// --- The tasks ------------------------------------------------------------------------------------

export const TASKS = [
  {
    id: 'tissue-cell-types',
    category: 'cell types',
    title: 'Label every cell with its type',
    // The examples' tissue holds its four types about equally; here the fibroblast-like and
    // immune-like are rarer and the endothelial-like commoner (their odds), so that no even split is
    // right.
    example: { kind: 'tissue', seed: 51001, params: (p) => ({ ...p, type_odds: [1, 0.6, 0.25, 2.5] }) },
    prompt: () => `Label every cell of this tissue with its cell type, and tell me what percentage of the cells each type makes up.\n\n${answerLine('{"percentages": {"<cell type>": <number>, ...}}')}`,
    truth: cellTypeTruth,
    async grade({ answer, call, truth }) {
      let best = null;
      for (const r of (await results(call)).filter((x) => x.kind === 'labels')) {
        const share = await agreement(call, r.id, truth);
        if (share !== null && (!best || share > best.share)) best = { share, name: r.name };
      }
      const said = byType(answer?.percentages, truth.types);
      const errors = truth.types.map((t) => Math.abs(number(said[t]) - truth.percent[t]));
      return { parts: [
        part('a label set agreeing with the true types (full credit from 90% of the cells)', 0.6, best ? closeness(1 - best.share, 0.1, 0.4) : 0, best ? `${percent(best.share)} of the cells labeled as their true type` : 'no label set'),
        part('each type\'s percentage (within 3 points)', 0.4, mean(errors.map((e) => closeness(e, 3, 10))), truth.types.map((t, k) => `${t} ${Number.isFinite(errors[k]) ? number(said[t]) : '—'} (true ${truth.percent[t].toFixed(1)})`).join(', ')),
      ] };
    },
    async expert(call) {
      await call('cluster_cells', {});
      const labels = (await call('label_cells', { markers: 'simulated' })).data;
      const percentages = Object.fromEntries(labels.labels.map((l) => [l.name, (100 * l.cells) / labels.cells.analyzed]));
      return `ANSWER: ${JSON.stringify({ percentages })}`;
    },
  },
  {
    id: 'tissue-neighbors',
    category: 'spatial',
    title: 'Find the two cell types that neighbor each other',
    // Fibroblast-like cells six times as likely in the domains immune-like cells lead (their odds
    // against each other type there): the only pair of different types neighbors more often than
    // at random.
    example: { kind: 'tissue', seed: 51002, params: (p) => ({ ...p, domain_odds: [{ leader: 2, type: 1, odds: 6 }] }) },
    prompt: () => `Which two different cell types in this tissue are neighbors of each other more often than chance would make them, more than any other pair of different types? Test it.\n\n${answerLine('{"pair": ["<cell type>", "<cell type>"]}')}`,
    truth(made) {
      const { types, domain_odds: odds } = made.truth.simulation.parameters;
      return { pair: [types[odds[0].leader].name, types[odds[0].type].name] };
    },
    async grade({ answer, call, truth }) {
      const said = list(answer?.pair);
      const right = said.length === 2 && truth.pair.every((t) => said.some((s) => isType(s, t)));
      const tested = (await results(call)).some((r) => r.kind === 'spatial' && /enrichment/i.test(r.name));
      return { parts: [
        part(`the pair (${truth.pair.join(' and ')})`, 0.7, right ? 1 : 0, said.join(' and ') || 'none'),
        part('a neighborhood enrichment in the project', 0.3, tested ? 1 : 0, tested ? 'made' : 'none'),
      ] };
    },
    async expert(call) {
      await call('cluster_cells', {});
      const labels = (await call('label_cells', { markers: 'simulated' })).data;
      await call('build_graph', {});
      const enrichment = (await call('spatial_analysis', { question: 'enrichment', labels: labels.id })).data;
      const table = (await call('get_result', { id: enrichment.id, limit: 0 })).data.table;
      const at = (name) => table.columns.indexOf(name);
      const [top] = table.rows.filter((r) => r[at('a')] !== r[at('b')]).sort((x, y) => y[at('z')] - x[at('z')]);
      return `ANSWER: ${JSON.stringify({ pair: [top[at('a')], top[at('b')]] })}`;
    },
  },
  {
    id: 'cohort-outlier',
    category: 'cohort QC',
    title: 'Find the sample with a quality problem',
    // The examples' outlier cohort has its sixth sample detect 40% of the transcripts; here the third.
    example: { kind: 'cohort', cohort: 'outlier', seed: 51003, params: (p) => ({ ...p, panel_seed: PANEL_SEED, outliers: [{ sample: 3, detection: 0.4 }] }) },
    prompt: () => `These eight sections are of eight subjects, four control and four treated. Before the groups are compared, check the samples' quality: is any sample far out from the others, and what is wrong with it?\n\n${answerLine('{"outliers": ["<sample>", ...], "problem": "<what is wrong>"}')}`,
    truth: (made) => ({ outliers: made.truth.cohort.samples.filter((s) => s.detection < 0.6).map((s) => s.name) }),
    async grade({ answer, call, truth }) {
      const said = list(answer?.outliers).map((s) => norm(s));
      const right = truth.outliers.every((s) => said.includes(norm(s)));
      const summarized = (await results(call)).some((r) => r.kind === 'cohort-summary');
      return { parts: [
        part(`the sample (${truth.outliers.join(', ')}, and no other)`, 0.6, right ? (said.length === truth.outliers.length ? 1 : 0.5) : 0, list(answer?.outliers).join(', ') || 'none'),
        part('what is wrong (it detected fewer transcripts)', 0.2, right && /transcript|count|detect|sensitiv|signal|librar|depth|molecule|gene|express|efficien/.test(norm(answer?.problem)) ? 1 : 0, answer?.problem ?? 'none'),
        part('the samples summarized in the project, with their quality', 0.2, summarized ? 1 : 0, summarized ? 'a cohort summary' : 'none'),
      ] };
    },
    async expert(call) {
      await call('cohort_steps', { markers: 'simulated' });
      await call('summarize_samples', {});
      const qc = (await call('cohort_qc', {})).data;
      const outliers = [...new Set(qc.outliers.map((o) => o.name))];
      return `ANSWER: ${JSON.stringify({ outliers, problem: qc.outliers.map((o) => o.reason).join('; ') })}`;
    },
  },
  {
    id: 'cohort-expression',
    category: 'differential expression',
    title: 'Find the gene a treatment changes',
    // The examples raise SIM033 in the epithelial-like cells; here SIM072 in the fibroblast-like.
    example: { kind: 'cohort', cohort: 'standard', seed: 51004, params: (p) => ({ ...p, panel_seed: PANEL_SEED, effects: [{ kind: 'level', group: 'treated', type: 'fibroblast-like', gene: 'SIM072', fold: 3 }] }) },
    prompt: () => `These eight sections are of eight subjects, four control and four treated. Does the treatment change any gene's expression in one of the cell types? Name each gene it changes, the cell type, and whether the gene goes up or down.\n\n${answerLine('{"changes": [{"gene": "<gene>", "cellType": "<cell type>", "direction": "up" | "down"}, ...]}')}`,
    truth: (made) => made.truth.cohort.effects.find((e) => e.kind === 'level'),
    async grade({ answer, call, truth }) {
      const changes = list(answer?.changes);
      const found = changes.find((c) => norm(c?.gene) === norm(truth.gene));
      const compared = (await results(call)).some((r) => r.kind === 'expression');
      return { parts: [
        part(`the gene (${truth.gene})`, 0.35, found ? 1 : 0, changes.map((c) => c?.gene).join(', ') || 'none'),
        part(`its cell type (${truth.type})`, 0.25, found && isType(found.cellType, truth.type) ? 1 : 0, found?.cellType ?? '—'),
        part(`its direction (${direction(truth.fold)})`, 0.1, found && sameDirection(found.direction, truth.fold) ? 1 : 0, found?.direction ?? '—'),
        part('no other change claimed', 0.1, found && changes.length === 1 ? 1 : 0, `${changes.length} claimed`),
        part('the groups\' expression compared in the project', 0.2, compared ? 1 : 0, compared ? 'compared' : 'none'),
      ] };
    },
    async expert(call) {
      await call('design', { unit: 'subject', group: 'condition', reference: 'control', save: 'By condition' });
      await call('cohort_steps', { markers: 'simulated' });
      await call('summarize_samples', {});
      const compared = (await call('compare_expression', { design: 'By condition' })).data;
      const found = (await call('get_result', { id: compared.id, limit: 1 })).data.summary.found;
      return `ANSWER: ${JSON.stringify({ changes: found.map((f) => ({ gene: f.gene, cellType: f.label, direction: f.logFC > 0 ? 'up' : 'down' })) })}`;
    },
  },
  {
    id: 'cohort-null',
    category: 'statistics',
    title: 'Compare groups that do not differ',
    example: { kind: 'cohort', cohort: 'null', seed: 51005, params: (p) => ({ ...p, panel_seed: PANEL_SEED }) },
    prompt: () => `These eight sections are of eight subjects, four control and four treated. Does the treatment change how much of each cell type the tissue holds, or any gene's expression in any cell type? Give empty lists where nothing changes.\n\n${answerLine('{"abundance": ["<cell type>", ...], "expression": [{"gene": "<gene>", "cellType": "<cell type>"}, ...]}')}`,
    truth: (made) => ({ effects: made.truth.cohort.effects }),
    async grade({ answer, call }) {
      const kinds = new Set((await results(call)).map((r) => r.kind));
      const none = (v) => Array.isArray(v) && v.length === 0;
      return { parts: [
        part('cell types\' abundance compared, and nothing claimed', 0.5, kinds.has('abundance') && none(answer?.abundance) ? 1 : 0, `${kinds.has('abundance') ? 'compared' : 'not compared'}; ${Array.isArray(answer?.abundance) ? `${answer.abundance.length} claimed` : 'no answer'}`),
        part('genes\' expression compared, and nothing claimed', 0.5, kinds.has('expression') && none(answer?.expression) ? 1 : 0, `${kinds.has('expression') ? 'compared' : 'not compared'}; ${Array.isArray(answer?.expression) ? `${answer.expression.length} claimed` : 'no answer'}`),
      ] };
    },
    async expert(call) {
      await call('design', { unit: 'subject', group: 'condition', reference: 'control', save: 'By condition' });
      await call('cohort_steps', { markers: 'simulated' });
      await call('summarize_samples', {});
      const abundance = (await call('compare_abundance', { design: 'By condition' })).data;
      const expression = (await call('compare_expression', { design: 'By condition' })).data;
      const found = async (id) => (await call('get_result', { id, limit: 1 })).data.summary.found;
      return `ANSWER: ${JSON.stringify({ abundance: [...new Set((await found(abundance.id)).map((f) => f.label))], expression: (await found(expression.id)).map((f) => ({ gene: f.gene, cellType: f.label })) })}`;
    },
  },
  {
    id: 'cohort-paired',
    category: 'study design',
    title: 'Find the cell type that changes after treatment',
    // The examples' paired cohort has the endothelial-like share rise after; here the immune-like
    // falls, to a quarter of its odds.
    example: { kind: 'cohort', cohort: 'paired', seed: 51006, params: (p) => ({ ...p, panel_seed: PANEL_SEED, effects: [{ kind: 'share', group: 'after', type: 'immune-like', fold: 0.25 }] }) },
    prompt: () => `Six subjects were each sampled before and after treatment, twelve sections in all. Does the treatment change the share of any cell type in the tissue? Which, and does it go up or down?\n\n${answerLine('{"changes": [{"cellType": "<cell type>", "direction": "up" | "down"}, ...]}')}`,
    truth: (made) => made.truth.cohort.effects.find((e) => e.kind === 'share'),
    async grade({ answer, call, truth }) {
      const changes = list(answer?.changes);
      const found = changes.find((c) => isType(c?.cellType, truth.type));
      let paired = false;
      for (const r of (await results(call)).filter((x) => x.kind === 'abundance')) if ((await designTerms(call, r.id)).some((t) => /^subject\b/i.test(t))) paired = true;
      return { parts: [
        part(`the cell type (${truth.type})`, 0.5, found ? 1 : 0, changes.map((c) => c?.cellType).join(', ') || 'none'),
        part(`its direction (${direction(truth.fold)})`, 0.2, found && sameDirection(found.direction, truth.fold) ? 1 : 0, found?.direction ?? '—'),
        part('abundance compared with each subject paired with itself', 0.3, paired ? 1 : 0, paired ? 'paired by subject' : 'no paired comparison'),
      ] };
    },
    async expert(call) {
      await call('design', { unit: 'subject', group: 'condition', reference: 'before', save: 'Before and after' });
      await call('cohort_steps', { markers: 'simulated' });
      await call('summarize_samples', {});
      const compared = (await call('compare_abundance', { design: 'Before and after' })).data;
      const found = (await call('get_result', { id: compared.id, limit: 1 })).data.summary.found.filter((f) => f.family === 'share');
      return `ANSWER: ${JSON.stringify({ changes: found.map((f) => ({ cellType: f.label, direction: f.ratio > 1 ? 'up' : 'down' })) })}`;
    },
  },
  {
    id: 'lung-endothelial',
    category: 'querying cells',
    title: 'Count and save the endothelial cells of a lung section',
    example: { kind: 'lung' },
    prompt: () => `In this lung section, call a cell endothelial when it holds at least two transcripts of VWF or at least two of PLVAP, and no EPCAM transcript. How many endothelial cells are there, and how many of them hold at least one ACE transcript? Save the endothelial cells as a filter named "Endothelial cells".\n\n${answerLine('{"endothelial": <number>, "withACE": <number>}')}`,
    async truth(made) {
      const { counts } = await lungTruth(made.truth.folder);
      const endothelial = new Set([...counts].filter(([, c]) => ((c.VWF ?? 0) >= 2 || (c.PLVAP ?? 0) >= 2) && !c.EPCAM).map(([id]) => id));
      return { endothelial, withACE: [...endothelial].filter((id) => counts.get(id).ACE >= 1).length };
    },
    async grade({ answer, call, truth }) {
      const n = number(answer?.endothelial);
      const ace = number(answer?.withACE);
      let kept = null;
      try {
        kept = new Set(await cellIds(call, { filter: 'Endothelial cells' }));
      } catch {
        kept = null;
      }
      const shared = kept ? [...kept].filter((id) => truth.endothelial.has(id)).length : 0;
      const jaccard = kept ? shared / (kept.size + truth.endothelial.size - shared) : 0;
      return { parts: [
        part('the number of endothelial cells (within 1%)', 0.3, closeness(Math.abs(n - truth.endothelial.size) / truth.endothelial.size, 0.01, 0.1), `${Number.isFinite(n) ? n : 'none'} (true ${truth.endothelial.size})`),
        part('how many of them hold ACE (within 1)', 0.2, closeness(Math.abs(ace - truth.withACE), 1, 0.1 * truth.withACE), `${Number.isFinite(ace) ? ace : 'none'} (true ${truth.withACE})`),
        part('a filter "Endothelial cells" keeping exactly them', 0.5, kept ? closeness(1 - jaccard, 0.01, 0.2) : 0, kept ? `${kept.size} cells, Jaccard index ${jaccard.toFixed(3)}` : 'no filter "Endothelial cells"'),
      ] };
    },
    async expert(call) {
      const endothelial = { all: [{ any: [{ gene: 'VWF', min: 2 }, { gene: 'PLVAP', min: 2 }] }, { not: { gene: 'EPCAM' } }] };
      const n = (await call('query_cells', { where: endothelial })).data.kept;
      const withACE = (await call('query_cells', { where: { all: [endothelial, { gene: 'ACE' }] } })).data.kept;
      await call('save_filter', { name: 'Endothelial cells', where: endothelial });
      return `ANSWER: ${JSON.stringify({ endothelial: n, withACE })}`;
    },
  },
  {
    id: 'lung-qc',
    category: 'quality control',
    title: 'Read a run\'s quality',
    example: { kind: 'lung' },
    prompt: () => `Check this run's quality. How many cells were detected, what is the median number of transcripts per cell, and what percentage of the cells hold fewer than 30 transcripts?\n\n${answerLine('{"cells": <number>, "medianTranscripts": <number>, "under30Percent": <number>}')}`,
    async truth(made) {
      const t = await lungTruth(made.truth.folder);
      return { cells: t.cellsDetected, medianTranscripts: t.medianTranscripts, under30Percent: (100 * t.transcripts.filter((v) => v < 30).length) / t.transcripts.length };
    },
    async grade({ answer, truth }) {
      const cells = number(answer?.cells);
      const median = number(answer?.medianTranscripts);
      const under30 = number(answer?.under30Percent);
      return { parts: [
        part('the cells detected', 0.3, closeness(Math.abs(cells - truth.cells), 0, 100), `${Number.isFinite(cells) ? cells : 'none'} (true ${truth.cells})`),
        part('the median transcripts per cell (within 1)', 0.3, closeness(Math.abs(median - truth.medianTranscripts), 1, 10), `${Number.isFinite(median) ? median : 'none'} (true ${truth.medianTranscripts})`),
        part('the percentage of cells with fewer than 30 transcripts (within 0.5 points)', 0.4, closeness(Math.abs(under30 - truth.under30Percent), 0.5, 3), `${Number.isFinite(under30) ? under30 : 'none'} (true ${truth.under30Percent.toFixed(2)})`),
      ] };
    },
    async expert(call) {
      const metrics = (await call('qc_metrics', {})).data.metrics;
      const recorded = (name) => metrics.find((m) => m.name === name).recorded;
      const under = (await call('query_cells', { where: { attribute: 'transcripts', max: 29 } })).data;
      return `ANSWER: ${JSON.stringify({ cells: recorded('num_cells_detected'), medianTranscripts: recorded('median_transcripts_per_cell'), under30Percent: (100 * under.kept) / under.cells })}`;
    },
  },
  {
    id: 'tissue-report',
    category: 'reporting',
    title: 'Make a figure of the cell types and write its methods',
    example: { kind: 'tissue', seed: 51007 },
    prompt: ({ outputs }) => `Label this tissue's cells by type. Then make a figure of the whole tissue with its cells colored by type and save it as a PDF at ${join(outputs, 'cell-types.pdf')}, and write the methods behind the labels, with their references, to ${join(outputs, 'methods.md')}.\n\n${answerLine('{"figure": "<path>", "methods": "<path>"}')}`,
    truth: () => ({ figure: 'cell-types.pdf', methods: 'methods.md', cites: 'Tirosh' }),
    async grade({ call, outputs, truth }) {
      const pdf = join(outputs, truth.figure);
      const written = existsSync(pdf) && readFileSync(pdf).subarray(0, 5).toString() === '%PDF-';
      let read = null;
      let fill = '';
      if (written) {
        try {
          read = (await call('read_figure', { path: pdf })).data;
          fill = (await call('figure', { name: read.name })).message ?? '';
        } catch {
          read = null;
        }
      }
      const md = join(outputs, truth.methods);
      const methods = existsSync(md) ? readFileSync(md, 'utf8') : '';
      return { parts: [
        part('the figure written as a PDF', 0.2, written ? 1 : 0, written ? truth.figure : 'no PDF'),
        part('SituCraft\'s figure of this project, unchanged', 0.2, read?.intact && read?.sameProject ? 1 : 0, read ? `intact ${read.intact}, this project ${read.sameProject}` : '—'),
        part('its cells filled by their labels', 0.2, /filled by the label set/.test(fill) ? 1 : 0, fill.slice(0, 200) || '—'),
        part('the methods written', 0.2, methods.length > 200 ? 1 : 0, methods ? `${methods.length} characters` : 'no file'),
        part('citing how the cells were labeled (Tirosh et al. 2016)', 0.2, methods.includes(truth.cites) ? 1 : 0, methods.includes(truth.cites) ? 'cited' : 'not cited'),
      ] };
    },
    async expert(call, { outputs }) {
      await call('cluster_cells', {});
      const labels = (await call('label_cells', { markers: 'simulated' })).data;
      await call('show_tissue', { fill: 'labels', whole: true });
      await call('save_view', { name: 'Cell types' });
      await call('figure', { name: 'Cell types', views: ['Cell types'] });
      const figure = join(outputs, 'cell-types.pdf');
      await call('write_figure', { name: 'Cell types', path: figure, format: 'pdf' });
      const methods = join(outputs, 'methods.md');
      await call('export', { what: 'methods', path: methods, result: labels.id });
      return `ANSWER: ${JSON.stringify({ figure, methods })}`;
    },
  },
  {
    id: 'tissue-certificate',
    category: 'the record',
    title: 'Certify an analysis so others can check it',
    example: { kind: 'tissue', seed: 51008 },
    prompt: ({ outputs }) => `Find which cell types in this tissue neighbor each other more or less often than chance, then write a certificate of that analysis, which others can check, to ${join(outputs, 'neighbors-certificate.zip')}.\n\n${answerLine('{"certificate": "<path>"}')}`,
    truth: () => ({ file: 'neighbors-certificate.zip' }),
    async grade({ outputs, truth, session, made }) {
      const zip = join(outputs, truth.file);
      if (!existsSync(zip)) return { parts: [part('the certificate written', 0.3, 0, 'no file'), part('of a neighborhood enrichment', 0.3, 0, '—'), part('verified by situcraft verify', 0.4, 0, '—')] };
      // situcraft verify, in a folder of its own, finding the bundle where the certificate names it.
      const folder = mkdtempSync(join(tmpdir(), 'situcraft-bench-verify-'));
      const report = join(folder, 'verification.json');
      let failure = null;
      try {
        await runBinary(session.binary, ['verify', '--data', dirname(made.paths[0]), '--report', report, zip], { cwd: folder });
      } catch (error) {
        failure = error.message;
      }
      const found = existsSync(report) ? JSON.parse(readFileSync(report, 'utf8')) : null;
      rmSync(folder, { recursive: true, force: true });
      const subject = JSON.stringify(found?.subject ?? '');
      return { parts: [
        part('the certificate written', 0.3, found ? 1 : 0.5, found ? truth.file : `not read: ${failure?.slice(0, 200)}`),
        part('of a neighborhood enrichment', 0.3, /enrichment/i.test(subject) ? 1 : 0, subject.slice(0, 200) || '—'),
        part('verified by situcraft verify', 0.4, found?.verdict === 'verified' ? 1 : 0, found?.verdict ?? '—'),
      ] };
    },
    async expert(call, { outputs }) {
      await call('cluster_cells', {});
      const labels = (await call('label_cells', { markers: 'simulated' })).data;
      await call('build_graph', {});
      const enrichment = (await call('spatial_analysis', { question: 'enrichment', labels: labels.id })).data;
      const certificate = join(outputs, 'neighbors-certificate.zip');
      await call('export', { what: 'certificate', path: certificate, result: enrichment.id });
      return `ANSWER: ${JSON.stringify({ certificate })}`;
    },
  },
];
