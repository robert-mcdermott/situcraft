// The Cohort view's differential expression and spatial statistics between groups (wave 7, slice 7;
// api_expression.go). Expression: each label's pseudobulk (its cells' transcripts summed over each
// replicate unit's samples) by voom and limma's moderated t over the genes every panel has, each
// contrast's log2 fold change with its interval; the genes by p, a volcano, and a gene's values by
// group. Spatial statistics: each sample's numbers from its spatial analyses (neighbor enrichment,
// nearest distances, Moran's I) in a linear model of the design, moderated across
// each family; each statistic's values by group.

import { h, icon, downloadBlob } from './dom.js';
import { toast } from './overlays.js';
import { cohortSession, summaryDocument } from './cohort-summary.js';
import { designsOf, makeDesign } from '../lib/design.js';
import { draftOf } from './cohort-design.js';
import { categoricalColor } from '../lib/colormaps.js';
import { linearScale, summariesOf } from '../lib/cohort.js';
import { SPATIAL_FAMILIES, addTest, expressionRequest, geneRows, labelWords, signed, spatialEffectWords, spatialFamiliesOf, spatialRequest, statisticByGroup, testRecord, testsOf, volcanoPoints } from '../lib/expression.js';

// What the sections choose and show, for the session.
export const expressionSession = { level: 0.95, minCells: 10, shown: null, label: null, contrast: null, gene: null, sort: 'p', filter: '', comparing: null };
export const spatialSession = { families: null, familiesOf: null, level: 0.95, shown: null, family: null, pick: null, comparing: null };

const documents = new Map();
async function getJSON(url, options = {}) {
  const response = await fetch(url, { cache: 'no-store', ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error ?? `${url}: HTTP ${response.status}`);
  return body;
}

// A comparison's document (expression.json or spatial-tests.json), read once.
export function testDocument(id, file) {
  const key = `${id}/${file}`;
  if (!documents.has(key)) {
    const p = getJSON(`/api/artifacts/${encodeURIComponent(id)}/files/${file}`);
    p.catch(() => documents.delete(key));
    documents.set(key, p);
  }
  return documents.get(key);
}

export const expressionDocument = (id) => testDocument(id, 'expression.json');
export const spatialDocument = (id) => testDocument(id, 'spatial-tests.json');

// One gene's values in a label's rows (api_expression.go expressionValues).
export function geneValues(id, label, gene) {
  return getJSON(`/api/cohort/expression/values?id=${encodeURIComponent(id)}&label=${encodeURIComponent(label)}&gene=${encodeURIComponent(gene)}`);
}

const shownSummary = (project) => summariesOf(project).find((r) => r.id === cohortSession.shown) ?? summariesOf(project)[0] ?? null;
const fmtP = (p) => (!Number.isFinite(p) ? '—' : p < 1e-4 ? p.toExponential(1) : p.toFixed(4));

// Starts a comparison (a job, unless made already) and records it in the project: the shown summary
// (or options.summary) by the design draft (or options.spec), the request made by makeRequest.
async function compare(app, session, options, redraw, path, makeRequest, commitWords) {
  const project = () => app.store.project;
  const summary = options.summary ? summariesOf(project()).find((r) => r.id === options.summary) : shownSummary(project());
  if (!summary) throw new Error('Summarize the samples first.');
  const draft = draftOf(project());
  const spec = options.spec ?? draft.spec;
  const design = makeDesign(project(), spec);
  const saved = designsOf(project()).find((d) => d.name === draft.name && JSON.stringify(d.spec) === JSON.stringify(design.spec));
  const request = makeRequest(design, summary.id, { name: saved?.name ?? '', version: saved?.version ?? 0 });
  const answer = await getJSON(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
  let manifest = answer.artifact;
  if (!manifest) {
    session.comparing = answer.job?.id ?? null;
    redraw();
    try {
      const job = await app.jobs.finished(session.comparing);
      if (job?.state !== 'complete') throw new Error(`the comparison ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
    } finally {
      session.comparing = null;
    }
    manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
  }
  const next = addTest(project(), testRecord(manifest, { summary: summary.id, design: saved ? { name: saved.name, version: saved.version } : null }));
  if (next !== project()) app.store.commit(next, commitWords);
  session.shown = manifest.id;
  redraw();
  return manifest;
}

// Compares the labels' expression: options { level, minCells, summary, spec }.
export function compareExpression(app, options = {}, redraw = () => {}) {
  const s = expressionSession;
  return compare(app, s, options, redraw, '/api/cohort/expression',
    (design, id, saved) => expressionRequest(design, id, { level: options.level ?? s.level, minCells: options.minCells ?? s.minCells, ...saved }), 'Compare the labels\' expression');
}

// Compares the samples' spatial statistics: options { families, level, summary, spec }.
export function compareSpatial(app, options = {}, redraw = () => {}) {
  const s = spatialSession;
  return compare(app, s, options, redraw, '/api/cohort/spatial-tests',
    (design, id, saved) => spatialRequest(design, id, { families: options.families ?? (s.families ? [...s.families] : []), level: options.level ?? s.level, ...saved }), 'Compare the samples\' spatial statistics');
}

// The draft design's state, as the abundance section says it.
function designParts(project, summary) {
  const design = makeDesign(project, draftOf(project).spec);
  return {
    design,
    parts: [
      h('p.muted', design.ok ? `Of ${summary.name}. ${design.words[0] ?? ''}` : `Of ${summary.name}, by the design above.`),
      design.ok ? null : h('p.seg-note', h('span.badge.warn', 'Cannot compare'), ` ${design.problems.join(' ') || 'Name the replicate unit and the groups compared in the design above.'}`),
    ],
  };
}

// The comparisons of a kind to choose from, and the one shown.
function chooser(project, kind, session, prefix, redraw) {
  const list = testsOf(project, kind);
  if (session.shown && !list.some((r) => r.id === session.shown)) session.shown = null;
  const shown = list.find((r) => r.id === session.shown) ?? list[0] ?? null;
  const select = list.length > 1 ? h('div.btn-row', h('span.muted', 'Comparison'), h('select.input.small.cohort-family', { 'aria-label': `The ${kind === 'expression' ? 'expression' : 'spatial'} comparison shown`, onchange: (e) => { session.shown = e.target.value; redraw(); } },
    ...list.map((r) => h('option', { value: r.id, selected: r.id === shown?.id }, `${r.created?.slice(0, 16).replace('T', ' ') ?? ''} · ${r.name.replace(prefix, '')}`)))) : null;
  return { shown, select };
}

function csvButton(record) {
  return h('button.btn.small', {
    type: 'button',
    title: 'Every test of the comparison, a row each',
    onclick: () => fetch(`/api/artifacts/${encodeURIComponent(record.id)}/files/results.csv`).then((r) => r.blob()).then((blob) => downloadBlob(blob, `${record.id}-results.csv`)),
  }, icon('download'), 'The results (CSV)');
}

export function expressionSection(app, { redraw }) {
  const project = app.store.project;
  const s = expressionSession;
  const summary = shownSummary(project);
  const card = h('div.seg-card.cohort-abundance.cohort-expression');
  const section = h('section.seg-section', h('div.section-title', 'Differential expression'), card);
  card.append(h('p', 'Each label\'s genes compared between the design\'s groups, with the replicate unit (not the cell) as the unit: the label\'s cells\' transcripts summed in each unit (its pseudobulk), over the genes every panel has. Genes too little expressed are left out; libraries are normalized by TMM; voom weighs each value by the precision its mean expression gives it; and each gene\'s model of the design, with its pairing and covariates, is tested by limma\'s moderated t. Each log2 fold change has its interval; p-values are adjusted by Benjamini and Hochberg over every label, gene and contrast tested.'));
  if (!summary) {
    card.append(h('p.muted', 'Summarize the samples first: the comparison is of their pseudobulk.'));
    return section;
  }
  const { design, parts } = designParts(project, summary);
  const go = async () => {
    try {
      const m = await compareExpression(app, {}, redraw);
      toast(`Compared: ${m.name}.`, { kind: 'ok' });
    } catch (error) {
      toast(`The comparison: ${error.message}`, { kind: 'error', timeout: 12000 });
      redraw();
    }
  };
  card.append(...parts,
    h('div.btn-row',
      h('label.muted', { for: 'cohort-min-cells' }, 'Rows with at least'),
      h('input.input.small.cohort-min-cells#cohort-min-cells', { type: 'number', min: '1', step: '1', value: String(s.minCells), onchange: (e) => { s.minCells = Math.max(1, Math.round(Number(e.target.value) || 10)); redraw(); } }),
      h('span.muted', 'cells of a label are tested')),
    h('div.btn-row', h('button.btn.small.primary', { type: 'button', 'data-action': 'compare-expression', disabled: !design.ok || Boolean(s.comparing), onclick: go }, icon('compare'), s.comparing ? 'Comparing…' : 'Compare expression')));
  const { shown, select } = chooser(project, 'expression', s, /^Expression compared: /, redraw);
  if (select) card.append(select);
  if (shown) {
    const holder = h('div.cohort-results', h('p.muted', 'Reading the comparison…'));
    card.append(holder);
    expressionDocument(shown.id).then((e) => holder.replaceChildren(...expressionParts(e, shown, redraw)), (error) => holder.replaceChildren(h('p', h('span.badge.warn', 'Not read'), ` ${error.message}`)));
  }
  return section;
}

function expressionParts(e, record, redraw) {
  const s = expressionSession;
  const tested = e.results.filter((r) => r.tested);
  if (!e.results.some((r) => r.label === s.label)) s.label = (tested[0] ?? e.results[0])?.label ?? null;
  const contrasts = e.design.contrasts.map((c) => c.name);
  if (!contrasts.includes(s.contrast)) s.contrast = contrasts[0];
  const found = (r) => (r.contrasts ?? []).reduce((a, c) => a + c.up + c.down, 0);
  const out = [h('p', h('b', record.name), h('span.muted', ' · artifact '), h('span.mono', record.id)), e.design.pseudoSamples ? h('p.callout.warn.cohort-pseudo-note', h('span', h('b', 'Pseudo-samples, for teaching. '), 'Its units are regions of one section, not independent replicates: these tests describe this section\'s regions, and their p-values are not evidence about subjects.')) : null];
  out.push(h('div.btn-row',
    h('span.muted', 'Label'),
    h('select.input.small.cohort-family', { 'aria-label': 'The label shown', onchange: (ev) => { s.label = ev.target.value; s.gene = null; redraw(); } },
      ...e.results.map((r) => h('option', { value: r.label, selected: r.label === s.label }, r.tested ? `${r.label} (${found(r)} below 0.05 adjusted)` : `${r.label} (untested)`))),
    contrasts.length > 1 ? h('select.input.small', { 'aria-label': 'The contrast shown', onchange: (ev) => { s.contrast = ev.target.value; s.gene = null; redraw(); } },
      ...contrasts.map((c) => h('option', { value: c, selected: c === s.contrast }, c))) : null,
    csvButton(record)));
  out.push(h('p.muted.seg-note', `${labelWords(e, s.label)} ${e.universe}.`));
  const r = e.results.find((x) => x.label === s.label);
  if (!r?.tested) return out;
  const all = geneRows(e, s.label, s.contrast);
  if (!all.some((x) => x.gene === s.gene)) s.gene = all[0]?.gene ?? null;
  const rows = geneRows(e, s.label, s.contrast, { sort: s.sort, filter: s.filter, limit: 15 });
  const level = Math.round(100 * e.level);
  const pick = (gene) => { s.gene = gene; redraw(); };
  // The volcano and the gene chosen side by side, then the genes.
  const row = all.find((x) => x.gene === s.gene);
  out.push(h('div.cohort-plots', h('div', volcanoPart(e, s.label, s.contrast, s.gene, pick), h('p.muted.seg-note', 'Each point a gene; those below an adjusted p of 0.05 marked. A point, or a row below, chooses its gene.')), row ? genePart(e, record, s.label, s.contrast, row) : null));
  out.push(h('div.btn-row',
    h('span.muted', 'Sort by'),
    h('select.input.small', { 'aria-label': 'Sort the genes by', onchange: (ev) => { s.sort = ev.target.value; redraw(); } },
      h('option', { value: 'p', selected: s.sort === 'p' }, 'p-value'), h('option', { value: 'fold', selected: s.sort === 'fold' }, 'size of the fold change')),
    h('input.input.small.cohort-gene-find', { type: 'search', placeholder: 'Find a gene', 'aria-label': 'Find a gene', value: s.filter, oninput: (ev) => { s.filter = ev.target.value; redraw(); } }),
    h('span.muted', all.length > rows.length ? `the first ${rows.length} of ${all.length} genes` : `${rows.length} genes`)));
  out.push(h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table.cohort-abundance-table.cohort-genes-table',
    h('thead', h('tr', h('th', 'Gene'), h('th.r', 'Mean log2 CPM'), h('th.r', 'log2 fold change'), h('th.r', `${level}% interval`), h('th.r', 'p'), h('th.r', 'Adjusted p'), h('th', h('span.sr-only', 'Found')))),
    h('tbody', ...rows.map((x) => {
      const sig = x.padj < 0.05;
      return h(`tr${s.gene === x.gene ? '.selected' : ''}${sig ? '.cohort-sig' : ''}`, { 'data-gene': x.gene, tabindex: '0', onclick: () => pick(x.gene), onkeydown: (ev) => { if (ev.key === 'Enter') pick(x.gene); } },
        h('td', x.gene), h('td.r', x.aveLogCPM.toFixed(2)), h('td.r', signed(x.logFC)), h('td.r', `${signed(x.lo)} to ${signed(x.hi)}`), h('td.r', fmtP(x.p)), h('td.r', fmtP(x.padj)),
        h('td', sig ? h('span.badge', x.logFC > 0 ? 'higher' : 'lower') : ''));
    })))));
  return out;
}

const ns = 'http://www.w3.org/2000/svg';
function el(tag, attrs, ...kids) {
  const n = document.createElementNS(ns, tag);
  for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, String(v));
  for (const kid of kids) n.append(kid);
  return n;
}

// A volcano: each gene's log2 fold change against −log10 p, those below an adjusted p of 0.05
// marked, the gene chosen named. A point chooses its gene (the table does by the keyboard).
function volcanoPart(e, label, contrast, gene, onPick) {
  const points = volcanoPoints(e, label, contrast);
  const W = 480;
  const H = 300;
  const P = 44;
  const big = Math.max(1, ...points.map((p) => Math.abs(p.x)));
  const top = Math.max(2, ...points.map((p) => Math.min(p.y, 50)));
  const sx = linearScale(-big, big, P, W - 12);
  const sy = linearScale(0, top, H - 32, 12);
  const n = points.filter((p) => p.found).length;
  const svg = el('svg', { viewBox: `0 0 ${W} ${H}`, class: 'cohort-dots cohort-volcano', role: 'img', 'aria-label': `${label}, ${contrast}: ${points.length} genes' log2 fold changes against their p-values, ${n} below an adjusted p of 0.05` });
  svg.append(el('line', { x1: P, x2: W - 12, y1: H - 32, y2: H - 32, class: 'cohort-axis' }), el('line', { x1: sx(0), x2: sx(0), y1: 12, y2: H - 32, class: 'cohort-zero' }));
  for (const v of [-big, 0, big]) svg.append(el('text', { x: sx(v), y: H - 16, 'text-anchor': 'middle', class: 'cohort-axis-label' }, signed(v, 1)));
  svg.append(el('text', { x: W / 2, y: H - 3, 'text-anchor': 'middle', class: 'cohort-axis-label' }, 'log2 fold change'));
  for (const v of [0, top]) svg.append(el('text', { x: P - 6, y: sy(v) + 4, 'text-anchor': 'end', class: 'cohort-axis-label' }, v.toFixed(0)));
  svg.append(el('text', { x: 12, y: H / 2, 'text-anchor': 'middle', class: 'cohort-axis-label', transform: `rotate(-90 12 ${H / 2})` }, '−log10 p'));
  for (const p of [...points.filter((q) => !q.found), ...points.filter((q) => q.found)]) {
    const dot = el('circle', { cx: sx(p.x), cy: sy(Math.min(p.y, top)), r: p.gene === gene ? 6 : 3.5, class: `cohort-volcano-point${p.found ? ' found' : ''}${p.gene === gene ? ' chosen' : ''}` });
    dot.append(el('title', {}, `${p.gene}: ${signed(p.x)} log2, p ${fmtP(10 ** -p.y)}`));
    dot.addEventListener('click', () => onPick(p.gene));
    svg.append(dot);
  }
  const chosen = points.find((p) => p.gene === gene);
  if (chosen) svg.append(el('text', { x: sx(chosen.x) + (chosen.x > 0 ? -8 : 8), y: sy(Math.min(chosen.y, top)) - 8, 'text-anchor': chosen.x > 0 ? 'end' : 'start', class: 'cohort-point-label' }, chosen.gene));
  return svg;
}

// A dot plot of each row's value by group, with each group's mean (not interactive).
function dotPlot(groups, { label, fmt }) {
  const values = groups.flatMap((g) => g.points.map((p) => p.value));
  const W = 480;
  const H = 260;
  const P = 52;
  const lo = Math.min(...values);
  const hi = Math.max(...values);
  const pad = (hi - lo) * 0.08 || 0.5;
  const sy = linearScale(lo - pad, hi + pad, H - 34, 14);
  const step = (W - P - 20) / Math.max(1, groups.length);
  const svg = el('svg', { viewBox: `0 0 ${W} ${H}`, class: 'cohort-dots', role: 'img', 'aria-label': label });
  svg.append(el('line', { x1: P, x2: W - 10, y1: H - 34, y2: H - 34, class: 'cohort-axis' }), el('line', { x1: P, x2: P, y1: 12, y2: H - 34, class: 'cohort-axis' }));
  for (const v of [lo, hi]) svg.append(el('text', { x: P - 6, y: sy(v) + 4, 'text-anchor': 'end', class: 'cohort-axis-label' }, fmt(v)));
  groups.forEach((g, gi) => {
    const x0 = P + step * gi + step / 2;
    svg.append(el('text', { x: x0, y: H - 14, 'text-anchor': 'middle', class: 'cohort-axis-label' }, g.group));
    if (g.points.length) svg.append(el('line', { x1: x0 - step * 0.28, x2: x0 + step * 0.28, y1: sy(g.mean), y2: sy(g.mean), class: 'cohort-mean' }));
    g.points.forEach((p, pi) => {
      const x = x0 + (pi - (g.points.length - 1) / 2) * Math.min(14, (step * 0.5) / Math.max(1, g.points.length));
      const dot = el('circle', { cx: x, cy: sy(p.value), r: 6, fill: categoricalColor(gi), class: 'cohort-point' });
      dot.append(el('title', {}, p.title));
      svg.append(dot);
    });
  });
  return svg;
}

// A gene's values by group: each row's log2 count per million as voom has it (read from the
// summary's pseudobulk), with its test.
function genePart(e, record, label, contrast, row) {
  const holder = h('div', h('p.muted', 'Reading the gene\'s values…'));
  geneValues(record.id, label, row.gene).then((v) => {
    const levels = [...new Set(e.design.rows.map((r) => r.group))];
    const groups = levels.map((group) => {
      const points = v.rows.filter((r) => r.group === group).map((r) => ({ value: r.logCPM, title: `${r.unit} (${group}): ${r.logCPM.toFixed(2)} log2 CPM, ${r.count.toLocaleString('en-US')} transcripts of ${Math.round(r.library).toLocaleString('en-US')}` }));
      return { group, points, mean: points.reduce((a, p) => a + p.value, 0) / Math.max(1, points.length) };
    });
    holder.replaceChildren(dotPlot(groups, { label: `${row.gene} in ${label}: each unit's log2 counts per million by group`, fmt: (x) => x.toFixed(1) }),
      h('p.muted.seg-note', 'Each point a replicate unit\'s pseudobulk of the label (its log2 counts per million, of the library TMM normalizes); the lines are the groups\' means.'));
  }, (error) => holder.replaceChildren(h('p', h('span.badge.warn', 'Not read'), ` ${error.message}`)));
  return h('div.cohort-pick',
    h('h3.seg-card-title', `${row.gene} in ${label}: ${contrast}`),
    h('p', `log2 fold change ${signed(row.logFC)} (${signed(row.lo)} to ${signed(row.hi)}), ${(2 ** row.logFC).toFixed(2)}×; moderated t ${row.t.toFixed(2)}, p ${fmtP(row.p)}, adjusted ${fmtP(row.padj)}.`),
    holder);
}

// The summary's spatial families, once read.
const familiesOf = new Map();

export function spatialTestsSection(app, { redraw }) {
  const project = app.store.project;
  const s = spatialSession;
  const summary = shownSummary(project);
  const card = h('div.seg-card.cohort-abundance.cohort-spatial-tests');
  const section = h('section.seg-section', h('div.section-title', 'Spatial statistics between groups'), card);
  card.append(h('p', 'Each sample\'s numbers from its spatial analyses compared between the design\'s groups, with the replicate unit as the unit (a unit\'s value its samples\' mean): a pair of labels\' neighbor enrichment and a label\'s median distance to the nearest cell of another, as log2 ratios; a gene\'s Moran\'s I. Each statistic is a linear model of the design, with its pairing and covariates; a family\'s variances are moderated across its statistics (limma\'s empirical Bayes), and its p-values adjusted by Benjamini and Hochberg over its tests. Niches\' shares are compared with the abundances, above.'));
  if (!summary) {
    card.append(h('p.muted', 'Summarize the samples first, with a spatial question answered on every sample.'));
    return section;
  }
  if (!familiesOf.has(summary.id)) {
    familiesOf.set(summary.id, null);
    summaryDocument(summary.id).then((doc) => { familiesOf.set(summary.id, spatialFamiliesOf(doc)); redraw(); }, () => familiesOf.delete(summary.id));
  }
  const has = familiesOf.get(summary.id);
  const { design, parts } = designParts(project, summary);
  card.append(...parts);
  if (!has) {
    card.append(h('p.muted', 'Reading the summary…'));
    return section;
  }
  if (!has.length) {
    card.append(h('p.muted.seg-note', 'The summary\'s samples have no spatial statistics: in the steps on every sample, answer a spatial question (neighbor enrichment, nearest distances, Moran\'s I), and summarize again.'));
  } else {
    // Every family the summary has, until some are unchosen; again for another summary.
    if (!s.families || s.familiesOf !== summary.id || [...s.families].some((f) => !has.includes(f))) {
      s.families = new Set(has);
      s.familiesOf = summary.id;
    }
    const go = async () => {
      try {
        const m = await compareSpatial(app, {}, redraw);
        toast(`Compared: ${m.name}.`, { kind: 'ok' });
      } catch (error) {
        toast(`The comparison: ${error.message}`, { kind: 'error', timeout: 12000 });
        redraw();
      }
    };
    card.append(h('div.cohort-boxes', ...SPATIAL_FAMILIES.filter((f) => has.includes(f.id)).map((f) => h('label.check', h('input', {
      type: 'checkbox', checked: s.families.has(f.id),
      onchange: (ev) => {
        if (ev.target.checked) s.families.add(f.id);
        else s.families.delete(f.id);
        redraw();
      },
    }), f.label))),
    h('div.btn-row', h('button.btn.small.primary', { type: 'button', 'data-action': 'compare-spatial', disabled: !design.ok || !s.families.size || Boolean(s.comparing), onclick: go }, icon('compare'), s.comparing ? 'Comparing…' : 'Compare the statistics')));
  }
  const { shown, select } = chooser(project, 'spatial-comparison', s, /^Spatial statistics compared: /, redraw);
  if (select) card.append(select);
  if (shown) {
    const holder = h('div.cohort-results', h('p.muted', 'Reading the comparison…'));
    card.append(holder);
    spatialDocument(shown.id).then((c) => holder.replaceChildren(...spatialParts(c, shown, redraw)), (error) => holder.replaceChildren(h('p', h('span.badge.warn', 'Not read'), ` ${error.message}`)));
  }
  return section;
}

function spatialParts(c, record, redraw) {
  const s = spatialSession;
  if (!c.families.some((f) => f.family === s.family)) s.family = c.families[0]?.family ?? null;
  const out = [h('p', h('b', record.name), h('span.muted', ' · artifact '), h('span.mono', record.id)), c.design.pseudoSamples ? h('p.callout.warn.cohort-pseudo-note', h('span', h('b', 'Pseudo-samples, for teaching. '), 'Its units are regions of one section, not independent replicates: these tests describe this section\'s regions, and their p-values are not evidence about subjects.')) : null];
  out.push(h('div.btn-row',
    h('span.muted', 'Show'),
    h('select.input.small.cohort-family', { 'aria-label': 'The family of statistics shown', onchange: (ev) => { s.family = ev.target.value; s.pick = null; redraw(); } },
      ...c.families.map((f) => h('option', { value: f.family, selected: f.family === s.family }, `${SPATIAL_FAMILIES.find((x) => x.id === f.family)?.label ?? f.name} (${f.tests.filter((t) => t.tested && t.padj < 0.05).length} below 0.05 adjusted)`))),
    csvButton(record)));
  const f = c.families.find((x) => x.family === s.family);
  if (!f) return out;
  out.push(h('p', h('b', f.name)));
  const level = Math.round(100 * c.level);
  const prior = f.dfPrior === null ? 'the statistics\' variances agree beyond chance (one variance for all)' : f.dfPrior === 0 ? 'one statistic tested: its own variance' : `the statistics' variances moderated toward a prior of ${f.dfPrior.toFixed(1)} degrees of freedom`;
  out.push(h('p.muted.seg-note', `${f.log ? 'Compared as log2 ratios; the effect, a ratio' : 'The effect, a difference'} (${f.effect}), with its ${level}% interval; ${prior}; tests on ${Number.isInteger(f.dfTotal) ? f.dfTotal : f.dfTotal.toFixed(1)} degrees of freedom. ${f.universe}.`));
  const contrasts = [...new Set(f.tests.map((t) => t.contrast))];
  // The tested first, by p; then the untested.
  const tests = [...f.tests].sort((a, b) => (a.tested === b.tested ? (a.tested ? a.p - b.p : 0) : a.tested ? -1 : 1));
  const shownTests = tests.slice(0, 40);
  out.push(h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table.cohort-abundance-table',
    h('thead', h('tr', h('th', 'Statistic'), contrasts.length > 1 ? h('th', 'Contrast') : null, h('th.r', f.effect[0].toUpperCase() + f.effect.slice(1)), h('th.r', 'p'), h('th.r', 'Adjusted p'), h('th', h('span.sr-only', 'Found')))),
    h('tbody', ...shownTests.map((t) => {
      const key = `${t.key}|${t.contrast}`;
      const sig = t.tested && t.padj < 0.05;
      const up = f.log ? t.ratio > 1 : t.estimate > 0;
      return h(`tr${s.pick === key ? '.selected' : ''}${sig ? '.cohort-sig' : ''}`, { 'data-test': key, tabindex: '0', onclick: () => { s.pick = key; redraw(); }, onkeydown: (ev) => { if (ev.key === 'Enter') { s.pick = key; redraw(); } } },
        h('td', t.key), contrasts.length > 1 ? h('td', t.contrast) : null,
        h('td.r', t.tested ? spatialEffectWords(f, t) : h('span.muted', t.note)),
        h('td.r', t.tested ? fmtP(t.p) : ''), h('td.r', t.tested ? fmtP(t.padj) : ''),
        h('td', sig ? h('span.badge', up ? 'higher' : 'lower') : ''));
    })))));
  if (tests.length > shownTests.length) out.push(h('p.muted.seg-note', `The first ${shownTests.length} of ${tests.length} tests, by p; the results (CSV) have every one.`));
  const pick = s.pick && f.tests.find((t) => `${t.key}|${t.contrast}` === s.pick);
  if (!pick) {
    out.push(h('p.muted.seg-note', 'Choose a row to see its units by group.'));
    return out;
  }
  const groups = statisticByGroup(c, f.family, pick.key).map((g) => ({
    ...g,
    points: g.points.map((p) => ({ ...p, title: `${p.unit} (${g.group}): ${f.log ? `${(2 ** p.value).toFixed(3)}` : p.value.toFixed(3)}` })),
  }));
  const fmt = (v) => (f.log ? (2 ** v).toPrecision(3) : v.toFixed(2));
  out.push(h('div.cohort-pick',
    h('h3.seg-card-title', `${pick.key}: ${pick.contrast}`),
    h('p', pick.tested ? `${f.effect[0].toUpperCase() + f.effect.slice(1)} ${spatialEffectWords(f, pick)}; moderated t ${pick.t.toFixed(2)}, p ${fmtP(pick.p)}, adjusted ${fmtP(pick.padj)}.` : `Untested: ${pick.note}.`),
    dotPlot(groups, { label: `${pick.key}: each unit's value by group${f.log ? ' (on a log scale)' : ''}`, fmt }),
    h('p.muted.seg-note', `Each point a replicate unit's value (its samples' mean${f.log ? ' on the log2 scale' : ''}); the lines are the groups' means.`)));
  return out;
}
