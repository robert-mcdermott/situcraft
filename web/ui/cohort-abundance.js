// The Cohort view's differential abundance (wave 7, slice 6; api_abundance.go): each label's cells
// compared between the design's groups with the sample (or the replicate unit) as the unit. Its
// share of the labeled cells (a quasi-binomial model of the design), its density per mm² of tissue
// (a quasi-Poisson model, the tissue's area as offset), and the niches' shares; each contrast's
// effect with its interval, its p-value and the p-value adjusted over the family's tests. A result's
// rows are plotted by group, and a point opens its sample's cells of the label on its tissue.

import { h, icon, clear, downloadBlob } from './dom.js';
import { toast } from './overlays.js';
import { chosenOf, cohortSession, summaryDocument, targetsOf } from './cohort-summary.js';
import { designsOf, makeDesign } from '../lib/design.js';
import { draftOf } from './cohort-design.js';
import { labelSource, refOf } from './label-sources.js';
import { categoricalColor } from '../lib/colormaps.js';
import { ABUNDANCE_FAMILIES, abundanceRecord, abundanceRequest, addAbundance, comparisonsOf, effectWords, rowsByGroup } from '../lib/abundance.js';
import { linearScale, summariesOf } from '../lib/cohort.js';

// What the section chooses and shows, for the session.
export const abundanceSession = { families: new Set(['share', 'density']), level: 0.95, shown: null, family: 'share', pick: null, comparing: null };

const comparisons = new Map();
async function getJSON(url, options = {}) {
  const response = await fetch(url, { cache: 'no-store', ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error ?? `${url}: HTTP ${response.status}`);
  return body;
}

// A comparison's document (abundance.json), read once.
export function comparisonDocument(id) {
  if (!comparisons.has(id)) {
    const p = getJSON(`/api/artifacts/${encodeURIComponent(id)}/files/abundance.json`);
    p.catch(() => comparisons.delete(id));
    comparisons.set(id, p);
  }
  return comparisons.get(id);
}

const shownSummary = (project) => summariesOf(project).find((r) => r.id === cohortSession.shown) ?? summariesOf(project)[0] ?? null;
const fmtP = (p) => (!Number.isFinite(p) ? '—' : p < 1e-4 ? p.toExponential(1) : p.toFixed(4));

// Compares the shown summary's samples by the design draft (a job, unless made already), recorded in
// the project. options: { families, level, summary (an id), spec (a design's specification) }.
export async function compareAbundance(app, options = {}, redraw = () => {}) {
  const project = () => app.store.project;
  const summary = options.summary ? summariesOf(project()).find((r) => r.id === options.summary) : shownSummary(project());
  if (!summary) throw new Error('Summarize the samples first.');
  const draft = draftOf(project());
  const spec = options.spec ?? draft.spec;
  const design = makeDesign(project(), spec);
  const saved = designsOf(project()).find((d) => d.name === draft.name && JSON.stringify(d.spec) === JSON.stringify(design.spec));
  const families = options.families ?? [...abundanceSession.families];
  const request = abundanceRequest(design, summary.id, { families, level: options.level ?? abundanceSession.level, name: saved?.name ?? '', version: saved?.version ?? 0 });
  const answer = await getJSON('/api/cohort/abundance', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request) });
  let manifest = answer.artifact;
  if (!manifest) {
    abundanceSession.comparing = answer.job?.id ?? null;
    redraw();
    try {
      const job = await app.jobs.finished(abundanceSession.comparing);
      if (job?.state !== 'complete') throw new Error(`the comparison ${job?.state ?? 'ended'}${job?.error ? `: ${job.error}` : ''}`);
    } finally {
      abundanceSession.comparing = null;
    }
    manifest = await getJSON(`/api/artifacts/${encodeURIComponent(answer.id)}`);
  }
  const next = addAbundance(project(), abundanceRecord(manifest, { summary: summary.id, design: saved ? { name: saved.name, version: saved.version } : null }));
  if (next !== project()) app.store.commit(next, 'Compare the samples\' abundances');
  abundanceSession.shown = manifest.id;
  redraw();
  return manifest;
}

// Shows a sample's cells of a label on its tissue (in the Viewer): its cells chosen (its section's
// or its region's) whose label in the summary's label set is the label. Answers the cells shown.
export async function showSampleCells(app, cellsOf, summaryId, sampleId, label) {
  const doc = await summaryDocument(summaryId);
  const sd = doc.samples.find((s) => s.sample === sampleId);
  if (!sd) throw new Error(`the summary has no sample ${sampleId}`);
  const t = targetsOf(app).targets.find((x) => x.sample.id === sampleId);
  if (!t) throw new Error(`${sd.name}'s bundle is not open on this computer`);
  if (t.seg.id !== sd.segmentation.id) throw new Error(`${sd.name}'s default segmentation is no longer the summary's`);
  const src = await labelSource(app, refOf(sd.labels.id, sd.labels.level));
  const k = src.names.indexOf(label);
  if (k < 0) throw new Error(`${sd.name}'s labels have no ${label}`);
  const chosen = await chosenOf(app, cellsOf, t);
  const mask = new Uint8Array(src.label.length);
  let count = 0;
  for (let i = 0; i < mask.length; i += 1) {
    if (chosen.keep[i] && src.label[i] === k) {
      mask[i] = 1;
      count += 1;
    }
  }
  app.showSpatial({ dataset: t.dataset.id, segmentation: t.seg.id, fill: `labels:${sd.labels.id}:${sd.labels.level}`, focus: { title: `${sd.name}: ${label}`, text: `${count.toLocaleString('en-US')} ${label} cells of ${sd.name}${sd.region ? `, in ${sd.region.name}` : ''}`, mask } });
  return { sample: sd.name, label, cells: count };
}

export function abundanceSection(app, { cellsOf, redraw }) {
  const project = app.store.project;
  const s = abundanceSession;
  const summary = shownSummary(project);
  const card = h('div.seg-card.cohort-abundance');
  const section = h('section.seg-section', h('div.section-title', 'Differential abundance'), card);
  card.append(h('p', 'Each label\'s cells compared between the design\'s groups, with the replicate unit (not the cell) as the unit: its share of the labeled cells by a quasi-binomial model of the design, with its pairing and covariates; its density per mm² of tissue by a quasi-Poisson model with the tissue\'s area as offset; and the niches\' shares as shares. Each effect is the model\'s (an odds ratio or a density ratio) with its interval; p-values are adjusted by Benjamini and Hochberg over each family\'s tests.'));
  if (!summary) {
    card.append(h('p.muted', 'Summarize the samples first: the comparison is of the cohort table\'s numbers.'));
    return section;
  }
  const draft = draftOf(project);
  const design = makeDesign(project, draft.spec);
  const hasNiches = (summary.summary?.niches ?? 0) > 0;
  if (!hasNiches) s.families.delete('niche');
  const boxes = h('div.cohort-boxes', ...ABUNDANCE_FAMILIES.filter((f) => f.id !== 'niche' || hasNiches).map((f) => h('label.check', h('input', {
    type: 'checkbox', checked: s.families.has(f.id),
    onchange: (e) => {
      if (e.target.checked) s.families.add(f.id);
      else s.families.delete(f.id);
      redraw();
    },
  }), f.label)));
  const go = async () => {
    try {
      const m = await compareAbundance(app, {}, redraw);
      toast(`Compared: ${m.name}.`, { kind: 'ok' });
    } catch (error) {
      toast(`The comparison: ${error.message}`, { kind: 'error', timeout: 12000 });
      redraw();
    }
  };
  card.append(
    h('p.muted', design.ok ? `Of ${summary.name}. ${design.words[0] ?? ''}` : `Of ${summary.name}, by the design above.`),
    design.ok ? null : h('p.seg-note', h('span.badge.warn', 'Cannot compare'), ` ${design.problems.join(' ') || 'Name the replicate unit and the groups compared in the design above.'}`),
    boxes,
    h('div.btn-row', h('button.btn.small.primary', { type: 'button', 'data-action': 'compare-abundance', disabled: !design.ok || !s.families.size || Boolean(s.comparing), onclick: go }, icon('compare'), s.comparing ? 'Comparing…' : 'Compare the groups')));
  const list = comparisonsOf(project);
  if (s.shown && !list.some((r) => r.id === s.shown)) s.shown = null;
  const shown = list.find((r) => r.id === s.shown) ?? list[0] ?? null;
  if (list.length > 1) {
    card.append(h('div.btn-row', h('span.muted', 'Comparison'), h('select.input.small.cohort-family', { 'aria-label': 'The comparison shown', onchange: (e) => { s.shown = e.target.value; s.pick = null; redraw(); } },
      ...list.map((r) => h('option', { value: r.id, selected: r.id === shown?.id }, `${r.created?.slice(0, 16).replace('T', ' ') ?? ''} · ${r.name.replace(/^Abundance compared: /, '')}`)))));
  }
  if (shown) {
    const holder = h('div.cohort-results', h('p.muted', 'Reading the comparison…'));
    card.append(holder);
    comparisonDocument(shown.id).then((c) => holder.replaceChildren(...resultParts(app, cellsOf, c, shown, redraw)), (error) => holder.replaceChildren(h('p', h('span.badge.warn', 'Not read'), ` ${error.message}`)));
  }
  return section;
}

function resultParts(app, cellsOf, c, record, redraw) {
  const s = abundanceSession;
  if (!c.families.some((f) => f.family === s.family)) s.family = c.families[0]?.family ?? 'share';
  const out = [h('p', h('b', record.name), h('span.muted', ` · artifact `), h('span.mono', record.id)), c.design.pseudoSamples ? h('p.callout.warn.cohort-pseudo-note', h('span', h('b', 'Pseudo-samples, for teaching. '), 'Its units are regions of one section, not independent replicates: these tests describe this section\'s regions, and their p-values are not evidence about subjects.')) : null];
  out.push(h('div.btn-row',
    h('span.muted', 'Show'),
    h('select.input.small.cohort-family', { 'aria-label': 'The family of tests shown', onchange: (e) => { s.family = e.target.value; s.pick = null; redraw(); } },
      ...c.families.map((f) => h('option', { value: f.family, selected: f.family === s.family }, `${f.name} (${f.tests.filter((t) => t.tested && t.padj < 0.05).length} below 0.05 adjusted)`))),
    h('button.btn.small', {
      type: 'button',
      title: 'Every test of the comparison, a row each',
      onclick: () => fetch(`/api/artifacts/${encodeURIComponent(record.id)}/files/results.csv`).then((r) => r.blob()).then((blob) => downloadBlob(blob, `${record.id}-results.csv`)),
    }, icon('download'), 'The results (CSV)')));
  const f = c.families.find((x) => x.family === s.family);
  const article = /^[aeiou]/i.test(f.effect) ? 'an' : 'a';
  out.push(h('p.muted.seg-note', `${f.model}; the effect, ${article} ${f.effect}, with its ${Math.round(100 * c.level)}% interval. ${f.universe}.`));
  const contrasts = [...new Set(f.tests.map((t) => t.contrast))];
  const rows = f.tests.map((t) => {
    const key = `${t.label}|${t.contrast}`;
    const sig = t.tested && t.padj < 0.05;
    return h(`tr${s.pick === key ? '.selected' : ''}${sig ? '.cohort-sig' : ''}`, { 'data-test': key, tabindex: '0', onclick: () => { s.pick = key; redraw(); }, onkeydown: (e) => { if (e.key === 'Enter') { s.pick = key; redraw(); } } },
      h('td', t.label), contrasts.length > 1 ? h('td', t.contrast) : null,
      h('td.r', t.tested ? `${t.ratio.toFixed(2)}×` : h('span.muted', '—')),
      h('td.r', t.tested ? `${t.lo.toFixed(2)} to ${t.hi.toFixed(2)}` : h('span.muted', t.note)),
      h('td.r', t.tested ? fmtP(t.p) : ''),
      h('td.r', t.tested ? fmtP(t.padj) : ''),
      h('td', sig ? h('span.badge', t.ratio > 1 ? 'higher' : 'lower') : ''));
  });
  out.push(h('div', { style: { overflowX: 'auto' } }, h('table.data.seg-table.cohort-abundance-table',
    h('thead', h('tr', h('th', 'Label'), contrasts.length > 1 ? h('th', 'Contrast') : null, h('th.r', f.effect[0].toUpperCase() + f.effect.slice(1)), h('th.r', `${Math.round(100 * c.level)}% interval`), h('th.r', 'p'), h('th.r', 'Adjusted p'), h('th', h('span.sr-only', 'Found')))),
    h('tbody', ...rows))));
  const pick = s.pick && f.tests.find((t) => `${t.label}|${t.contrast}` === s.pick);
  if (pick) out.push(plotPart(app, cellsOf, c, f, pick));
  else out.push(h('p.muted.seg-note', 'Choose a row to see its samples by group.'));
  return out;
}

// A result's rows by group: each unit's value, the groups' means, and each point opening its
// samples' cells of the label on their tissue.
function plotPart(app, cellsOf, c, f, test) {
  const groups = rowsByGroup(c, f.family, test.label);
  const values = groups.flatMap((g) => g.points.map((p) => p.value));
  const W = 480;
  const H = 280;
  const P = 48;
  const share = f.family !== 'density';
  const sy = linearScale(Math.min(0, ...values), Math.max(...values), H - 34, 16);
  const step = (W - P - 20) / Math.max(1, groups.length);
  const ns = 'http://www.w3.org/2000/svg';
  const el = (tag, attrs, ...kids) => {
    const n = document.createElementNS(ns, tag);
    for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, String(v));
    for (const kid of kids) n.append(kid);
    return n;
  };
  const fmt = (v) => (share ? `${(100 * v).toFixed(1)}%` : v.toLocaleString('en-US', { maximumFractionDigits: 0 }));
  // A group, not an image: its points are buttons.
  const svg = el('svg', { viewBox: `0 0 ${W} ${H}`, class: 'cohort-dots', role: 'group', 'aria-label': `${test.label}: each unit's ${share ? 'share' : 'density'} by group` });
  svg.append(el('line', { x1: P, x2: W - 10, y1: H - 34, y2: H - 34, class: 'cohort-axis' }), el('line', { x1: P, x2: P, y1: 12, y2: H - 34, class: 'cohort-axis' }));
  for (const v of [Math.min(0, ...values), Math.max(...values)]) svg.append(el('text', { x: P - 6, y: sy(v) + 4, 'text-anchor': 'end', class: 'cohort-axis-label' }, fmt(v)));
  const status = h('p.muted.seg-note', { 'aria-live': 'polite' }, 'A point opens its samples\' cells of the label on their tissue.');
  groups.forEach((g, gi) => {
    const x0 = P + step * gi + step / 2;
    svg.append(el('text', { x: x0, y: H - 14, 'text-anchor': 'middle', class: 'cohort-axis-label' }, g.group));
    svg.append(el('line', { x1: x0 - step * 0.28, x2: x0 + step * 0.28, y1: sy(g.mean), y2: sy(g.mean), class: 'cohort-mean' }));
    g.points.forEach((p, pi) => {
      const x = x0 + (pi - (g.points.length - 1) / 2) * Math.min(14, (step * 0.5) / Math.max(1, g.points.length));
      const dot = el('circle', { cx: x, cy: sy(p.value), r: 6, fill: categoricalColor(gi), class: 'cohort-point', tabindex: f.family === 'niche' ? -1 : 0, role: f.family === 'niche' ? 'presentation' : 'button', 'aria-label': `${p.unit}: ${fmt(p.value)}` });
      dot.append(el('title', {}, `${p.unit} (${g.group}): ${fmt(p.value)}, ${p.count.toLocaleString('en-US')} of ${share ? p.total.toLocaleString('en-US') : `${p.total.toFixed(3)} mm²`}`));
      if (f.family !== 'niche') {
        const open = async () => {
          try {
            const shown = await showSampleCells(app, cellsOf, c.summary.id, p.samples[0], test.label);
            status.textContent = `Showing ${shown.cells.toLocaleString('en-US')} ${test.label} cells of ${shown.sample} in the Viewer.`;
          } catch (error) {
            toast(`The cells: ${error.message}`, { kind: 'error' });
          }
        };
        dot.addEventListener('click', open);
        dot.addEventListener('keydown', (e) => { if (e.key === 'Enter') open(); });
      }
      svg.append(dot);
    });
  });
  return h('div.cohort-pick',
    h('h3.seg-card-title', `${test.label}: ${test.contrast}`),
    h('p', test.tested ? `${f.effect[0].toUpperCase() + f.effect.slice(1)} ${effectWords(test)}; p ${fmtP(test.p)}, adjusted ${fmtP(test.padj)}; dispersion ${test.dispersion.toFixed(2)} on ${test.df} degrees of freedom.` : `Untested: ${test.note}.`),
    svg, status);
}
