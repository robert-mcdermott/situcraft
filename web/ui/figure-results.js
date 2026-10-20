// What a figure's result panels draw (wave 8, slice 2), gathered from the project and the program
// as the views gather it: a QC distribution of a dataset's cells (the QC view's bins), the cells'
// map (a clustering's map, colored by its clusters or a label set), a spatial answer (an
// enrichment's or nearest distances' heat map, Moran's I's genes), and a cohort's comparison (its
// effects with their intervals, its units by group, a volcano, a gene by group). Each returns
// the chart lib/charts.js draws, its legend and words; the panel draws them (chartPanel).

import { readCells } from '../lib/viewer-data.js';
import { readSegCells } from '../lib/segview.js';
import { finiteSorted, histogram, histogramBins } from '../lib/plots.js';
import { categoricalColor } from '../lib/colormaps.js';
import { clusterNames, mergedOf, nameOf } from '../lib/clusterings.js';
import { labelColor, parseLabelFill } from '../lib/labels.js';
import { labelSource, perCell, refOf } from './label-sources.js';
import { divergingColor } from '../lib/spatial.js';
import { rowsByGroup } from '../lib/abundance.js';
import { SPATIAL_FAMILIES, statisticByGroup, volcanoPoints } from '../lib/expression.js';
import { log } from '../lib/dmath.js';
import { FILLS } from './viewer-panel.js';

const LN2 = 0.6931471805599453;
const log2 = (x) => (x > 0 ? log(x) / LN2 : x === 0 ? -Infinity : NaN);
const count = (n) => Number(n).toLocaleString('en-US');

async function getJSON(url) {
  const response = await fetch(url, { cache: 'no-store' });
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
  return response.json();
}

async function getBuffer(url) {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
  return response.arrayBuffer();
}

function bundleItem(app, dataset) {
  const link = app.bundles.link(dataset.id);
  if (link?.state === 'missing' || link?.state === 'different-run') throw new Error(`${dataset.name}'s bundle is not where the project records it: locate it in the Data view.`);
  const item = link?.item ?? app.bundles.itemByPath(dataset.path);
  if (!item) throw new Error(`${dataset.name}'s bundle is not open on this computer.`);
  return item;
}

const record = (project, id, kinds) => {
  const a = project.artifacts.find((x) => x.id === id && kinds.includes(x.kind));
  if (!a) throw new Error(`The project has no ${kinds.join(' or ')} ${id}.`);
  return a;
};

// The QC metrics a distribution panel draws: the cells table's columns.
export const QC_METRICS = FILLS.filter((f) => f.column).map((f) => ({ id: f.id, label: f.label, column: f.column, integer: f.id === 'transcripts' || f.id === 'total' }));

// --- A QC distribution ---------------------------------------------------------------------------

async function qcData(app, panel) {
  const project = app.store.project;
  const dataset = project.datasets.find((d) => d.id === panel.dataset);
  if (!dataset) throw new Error(`The project has no dataset ${panel.dataset}.`);
  const metric = QC_METRICS.find((m) => m.id === panel.metric);
  if (!metric) throw new Error(`No QC metric ${panel.metric}: ${QC_METRICS.map((m) => m.id).join(', ')}.`);
  const item = bundleItem(app, dataset);
  const seg = panel.segmentation && panel.segmentation !== 'instrument' ? record(project, panel.segmentation, ['segmentation']) : null;
  const cells = seg ? readSegCells(await getBuffer(`${item.url}/segmentations/${encodeURIComponent(seg.id)}/cells`)) : readCells(await getBuffer(`${item.url}/cells`));
  const values = cells[metric.column];
  const sorted = finiteSorted(values);
  const lo = sorted.length ? sorted[0] : 0;
  // As the QC view bins it: the top 0.5% of cells would squeeze the rest into a few bins.
  const hi = sorted.length ? sorted[Math.min(sorted.length - 1, Math.floor(0.995 * (sorted.length - 1)))] : 1;
  const log = Boolean(panel.log);
  const bins = histogramBins(log ? Math.max(lo, 1) : lo, Math.max(hi, lo + 1), { bins: 40, integer: metric.integer, log });
  const { counts } = histogram(values, bins.edges);
  const lines = [];
  let filterWords = '';
  if (panel.filter) {
    const f = record(project, panel.filter, ['filter']);
    for (const c of f.conditions ?? []) {
      if (c.attribute !== metric.id) continue;
      if (Number.isFinite(c.min)) lines.push({ value: c.min, label: 'min' });
      if (Number.isFinite(c.max)) lines.push({ value: c.max, label: 'max' });
    }
    filterWords = `; the filter "${f.name}" version ${f.version}${lines.length ? ` at ${lines.map((l) => l.value).join(' and ')}` : ' (no threshold of it)'}`;
  }
  const shown = counts.reduce((a, b) => a + b, 0);
  return {
    title: `${metric.label}: ${dataset.name}`,
    chart: { kind: 'histogram', spec: { edges: Array.from(bins.edges), counts: Array.from(counts), xLabel: `${metric.label}${log ? ' (log scale)' : ''}`, yLabel: 'Cells', log: bins.log, lines } },
    description: `${metric.label} of ${dataset.name}'s ${count(sorted.length)} cells${seg ? ` (${seg.name})` : ''} in ${counts.length} bins (${count(shown)} drawn, from the least to the 99.5th percentile)${filterWords}.`,
    facts: { cells: sorted.length, drawn: shown, bins: counts.length, counts: Array.from(counts) },
  };
}

// --- The cells' map ------------------------------------------------------------------------------

async function mapData(app, panel) {
  const project = app.store.project;
  const map = record(project, panel.map, ['embedding']);
  const data = await app.bundles.mapData(map.id);
  const color = panel.color ?? 'clusters';
  let colorOf;
  let categories;
  let colorWords;
  if (color === 'clusters') {
    const id = map.clustering ?? map.summary?.clustering?.id;
    const c = await app.bundles.clusteringData(id);
    const names = clusterNames(project, id);
    const merged = mergedOf(names, c.sizes.length);
    colorOf = (row) => (c.cluster[row] >= 0 ? merged[c.cluster[row]] : -1);
    categories = c.sizes.map((_, k) => ({ label: nameOf(names, k), color: categoricalColor(merged[k]) }));
    colorWords = 'their clusters';
  } else {
    const ref = parseLabelFill(color);
    if (!ref) throw new Error(`A map is colored by clusters or labels:<label set>:<level>, not ${color}.`);
    const src = await labelSource(app, refOf(ref.id, ref.level));
    if (src.segCells !== data.segCells) throw new Error('The label set is of another segmentation\'s cells.');
    const sets = src.names.map((name, k) => ({ name, color: src.colors[k] }));
    const per = perCell(src, sets.length);
    colorOf = (row) => per[row];
    categories = [...sets.map((s, k) => ({ label: s.name, color: labelColor(k, sets.length, sets) })), { label: 'Unlabeled', color: labelColor(sets.length, sets.length, sets) }];
    colorWords = `the label set "${src.name}"`;
  }
  const x = new Float64Array(data.n);
  const y = new Float64Array(data.n);
  const colors = new Array(data.n);
  const present = new Set();
  for (let i = 0; i < data.n; i += 1) {
    x[i] = data.xy[2 * i];
    // The map's y grows upward, as the Cell types view draws it.
    y[i] = data.xy[2 * i + 1];
    const k = colorOf(data.cells[i]);
    colors[i] = k >= 0 ? categories[k]?.color ?? '#9aa2b1' : '#9aa2b1';
    if (k >= 0) present.add(k);
  }
  const legend = categories.filter((_, k) => present.has(k));
  // Many groups are named on the map, at their points' median place, rather than in a legend.
  let labels = [];
  if (legend.length > 12) {
    const byGroup = new Map();
    for (let i = 0; i < data.n; i += 1) {
      const k = colorOf(data.cells[i]);
      if (k < 0) continue;
      if (!byGroup.has(k)) byGroup.set(k, { x: [], y: [] });
      byGroup.get(k).x.push(x[i]);
      byGroup.get(k).y.push(y[i]);
    }
    const median = (v) => Float64Array.from(v).sort()[Math.floor(v.length / 2)];
    labels = [...byGroup.entries()].sort((p, q) => p[0] - q[0]).map(([k, g]) => ({ text: String(categories[k].label).replace(/^Cluster /, ''), x: median(g.x), y: median(g.y) }));
  }
  return {
    title: `The cells' map (UMAP)${color === 'clusters' ? ', by cluster' : ''}`,
    chart: { kind: 'scatter', spec: { x, y, colors, xLabel: 'UMAP 1', yLabel: 'UMAP 2', equal: true, size: data.n > 20000 ? 0.6 : data.n > 5000 ? 0.9 : 1.4, opacity: 0.85, labels } },
    legend: labels.length ? [] : legend,
    description: `The map (UMAP) of ${count(data.n)} cells, colored by ${colorWords}${labels.length ? `, each named at its cells' median place` : ''}.`,
    facts: { points: data.n, categories: legend.length },
  };
}

// --- A spatial answer ----------------------------------------------------------------------------

async function spatialData(app, panel) {
  const project = app.store.project;
  const a = record(project, panel.analysis, ['spatial']);
  const doc = await getJSON(`/api/artifacts/${encodeURIComponent(a.id)}/files/analysis.json`);
  const names = doc.labels?.names ?? [];
  const K = names.length;
  const stops = [-2, -1.5, -1, -0.5, 0, 0.5, 1, 1.5, 2].map((v) => divergingColor(v));
  if (doc.question === 'enrichment') {
    const e = doc.enrichment;
    const ratio = (i, j) => (e.expected[i * K + j] > 0 ? e.count[i * K + j] / e.expected[i * K + j] : NaN);
    let significant = 0;
    for (let i = 0; i < K; i += 1) for (let j = i; j < K; j += 1) if ((e.padj[i * K + j] ?? NaN) < 0.05) significant += 1;
    return {
      title: `Neighbor enrichment${doc.labels.name ? `: ${String(doc.labels.name).split(': ').at(-1)}` : ''}`,
      chart: { kind: 'heatmap', spec: { rows: names, nameColors: doc.labels.colors ?? null, color: (i, j) => { const v = log2(ratio(i, j)); return Number.isFinite(v) || v === -Infinity ? divergingColor(v) : null; }, marked: (i, j) => (e.padj[i * K + j] ?? NaN) < 0.05, bar: { stops, low: '¼× (fewer)', high: '4× (more)' } } },
      description: `The ratio of neighbors found to expected for each pair of ${K} labels (${doc.labels.name ?? 'labels'}), red more and blue fewer, clamped at four times; a dot where the adjusted p is below 0.05 (${significant} pairs).`,
      facts: { question: 'enrichment', labels: K },
    };
  }
  if (doc.question === 'distances') {
    const r = doc.nearest;
    const ratio = (i, j) => (r.permMean[i * K + j] > 0 ? r.median[i * K + j] / r.permMean[i * K + j] : NaN);
    return {
      title: `Nearest distances${doc.labels.name ? `: ${String(doc.labels.name).split(': ').at(-1)}` : ''}`,
      chart: { kind: 'heatmap', spec: { rows: names, nameColors: doc.labels.colors ?? null, color: (i, j) => { const v = -log2(ratio(i, j)); return Number.isFinite(v) ? divergingColor(v) : null; }, marked: (i, j) => (r.padj[i * K + j] ?? NaN) < 0.05, bar: { stops, low: '¼× (farther)', high: '4× (nearer)' } } },
      description: `From each row's label to the nearest cell of each column's, the median distance against chance's (red nearer, blue farther, clamped at four times), for ${K} labels; a dot where the adjusted p is below 0.05.`,
      facts: { question: 'distances', labels: K },
    };
  }
  if (doc.question === 'moran') {
    const m = doc.moran;
    const rows = [];
    for (let g = 0; g < m.genes.length; g += 1) if ((m.padj[g] ?? NaN) < 0.05 && Number.isFinite(m.i[g]) && m.i[g] > m.expected) rows.push(g);
    rows.sort((p, q) => m.i[q] - m.i[p]);
    const top = rows.slice(0, panel.top ?? 15);
    return {
      title: 'Genes in a pattern (Moran\'s I)',
      chart: { kind: 'bars', spec: { items: top.map((g) => ({ label: m.genes[g], value: m.i[g] })), xLabel: 'Moran\'s I', reference: m.expected } },
      description: `The ${top.length} genes of ${count(m.genes.length)} whose Moran's I is highest among the ${count(rows.length)} in a pattern (adjusted p below 0.05); the dashed line its expectation without one (${m.expected.toFixed(4)}).`,
      facts: { question: 'moran', genes: top.length },
    };
  }
  throw new Error(`An answer of "${doc.question}" is not drawn in figures yet: enrichment, distances and Moran's I are.`);
}

// --- A cohort's comparison -----------------------------------------------------------------------

const KIND_FILES = { abundance: 'abundance.json', expression: 'expression.json', 'spatial-comparison': 'spatial-tests.json' };

async function cohortData(app, panel) {
  const project = app.store.project;
  const a = record(project, panel.comparison, Object.keys(KIND_FILES));
  const doc = await getJSON(`/api/artifacts/${encodeURIComponent(a.id)}/files/${KIND_FILES[a.kind]}`);
  const contrasts = doc.design?.contrasts ?? [];
  const contrast = panel.contrast ?? contrasts[0]?.name ?? contrasts[0];
  const levels = [...new Set((doc.design?.rows ?? []).map((r) => r.group))];
  const colors = levels.map((_, k) => categoricalColor(k));
  if (a.kind === 'abundance') {
    const f = doc.families.find((x) => x.family === (panel.family ?? doc.families[0]?.family));
    if (!f) throw new Error(`The comparison has no family ${panel.family}: ${doc.families.map((x) => x.family).join(', ')}.`);
    if (panel.plot === 'units') {
      const label = panel.label ?? f.labels[0];
      const groups = rowsByGroup(doc, f.family, label);
      const share = f.family !== 'density';
      return {
        title: `${label}: ${f.name ?? f.family}`,
        chart: { kind: 'dots', spec: { groups, colors, yLabel: share ? 'Share of the labeled cells' : 'Cells per mm²', format: share ? (v) => `${Number((100 * v).toPrecision(3))}%` : undefined } },
        description: `Each unit's ${share ? 'share of the labeled cells' : 'cells per mm²'} of ${label}, by group (${groups.map((g) => `${g.group} ${g.points.length}`).join(', ')}), each group's mean a line.`,
        facts: { plot: 'units', units: groups.reduce((n, g) => n + g.points.length, 0) },
      };
    }
    const tests = f.tests.filter((t) => t.contrast === contrast);
    return {
      title: `${f.name ?? f.family}: ${contrast}`,
      chart: { kind: 'forest', spec: { rows: tests.map((t) => ({ label: t.label, estimate: t.tested ? t.ratio : NaN, lo: t.lo, hi: t.hi, marked: t.padj < 0.05, note: t.tested ? '' : 'untested' })), log: true, reference: 1, xLabel: f.family === 'density' ? 'Density ratio' : 'Odds ratio' } },
      description: `Each label's ${f.family === 'density' ? 'density ratio' : 'odds ratio'} ${contrast} with its ${Math.round((doc.level ?? 0.95) * 100)}% interval, on a log scale; red where the adjusted p is below 0.05 (${tests.filter((t) => t.padj < 0.05).length} of ${tests.length}).`,
      facts: { plot: 'forest', rows: tests.length },
    };
  }
  if (a.kind === 'expression') {
    const label = panel.label ?? doc.results.find((r) => r.tested)?.label;
    if (panel.plot === 'gene') {
      if (!panel.gene) throw new Error('Name the gene.');
      const v = await getJSON(`/api/cohort/expression/values?id=${encodeURIComponent(a.id)}&label=${encodeURIComponent(label)}&gene=${encodeURIComponent(panel.gene)}`);
      const groups = levels.map((group) => {
        const points = v.rows.filter((r) => r.group === group).map((r) => ({ value: r.logCPM }));
        return { group, points, mean: points.reduce((s, p) => s + p.value, 0) / Math.max(1, points.length) };
      });
      return {
        title: `${panel.gene} in ${label}`,
        chart: { kind: 'dots', spec: { groups, colors, yLabel: 'log2 counts per million' } },
        description: `Each unit's log2 counts per million of ${panel.gene} in ${label}'s pseudobulk, by group, each group's mean a line.`,
        facts: { plot: 'gene', units: groups.reduce((n, g) => n + g.points.length, 0) },
      };
    }
    const points = volcanoPoints(doc, label, contrast);
    const found = points.filter((p) => p.found).length;
    return {
      title: `${label}: ${contrast}`,
      chart: { kind: 'volcano', spec: { points, named: panel.named ?? 5 } },
      description: `Each of ${count(points.length)} genes' log2 fold change in ${label} (${contrast}) against its −log10 p; red where the adjusted p is below 0.05 (${found}), the strongest named.`,
      facts: { plot: 'volcano', genes: points.length, found },
    };
  }
  // Spatial statistics compared.
  const f = doc.families.find((x) => x.family === (panel.family ?? doc.families[0]?.family));
  if (!f) throw new Error(`The comparison has no family ${panel.family}.`);
  const fam = SPATIAL_FAMILIES.find((x) => x.id === f.family) ?? { log: false, label: f.family };
  const keyName = (k) => String(k).replace('|', ' – ');
  if (panel.plot === 'units') {
    const key = panel.key ?? f.keys[0];
    const groups = statisticByGroup(doc, f.family, key).map((g) => ({ ...g, points: g.points.filter((p) => Number.isFinite(p.value)) }));
    return {
      title: `${keyName(key)}: ${fam.label}`,
      chart: { kind: 'dots', spec: { groups, colors, yLabel: fam.log ? `${fam.label} (log2)` : fam.label } },
      description: `Each unit's ${fam.label.toLowerCase()}${fam.log ? ' (log2)' : ''} of ${keyName(key)}, by group, each group's mean a line.`,
      facts: { plot: 'units', units: groups.reduce((n, g) => n + g.points.length, 0) },
    };
  }
  const tests = f.tests.filter((t) => t.contrast === contrast);
  return {
    title: `${fam.label}: ${contrast}`,
    chart: { kind: 'forest', spec: { rows: tests.map((t) => ({ label: keyName(t.key), estimate: t.tested ? (fam.log ? t.ratio : t.estimate) : NaN, lo: fam.log ? t.ratioLo : t.lo, hi: fam.log ? t.ratioHi : t.hi, marked: t.padj < 0.05, note: t.tested ? '' : 'untested' })), log: fam.log, reference: fam.log ? 1 : 0, xLabel: fam.log ? 'Ratio' : 'Difference' } },
    description: `Each statistic's ${fam.log ? 'ratio' : 'difference'} ${contrast} with its interval${fam.log ? ', on a log scale' : ''}; red where the adjusted p is below 0.05 (${tests.filter((t) => t.padj < 0.05).length} of ${tests.length}).`,
    facts: { plot: 'forest', rows: tests.length },
  };
}

export const RESULT_KINDS = { qc: qcData, map: mapData, spatial: spatialData, cohort: cohortData };

// A result panel's data: { title, chart, legend, description, facts }; throws what is missing.
export function resultPanelData(app, panel) {
  const gather = RESULT_KINDS[panel.kind];
  if (!gather) throw new Error(`SituCraft does not draw "${panel.kind}" panels.`);
  return gather(app, panel);
}
