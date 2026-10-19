// Differential expression and spatial statistics between groups (wave 7, slice 7; api_expression.go,
// internal/cohort CompareExpression and CompareSpatial): the comparisons' requests from a design
// (lib/design.js makeDesign) and a cohort summary, the comparisons as the project records them, and
// their results as the page shows them (a label's genes by p, its volcano's points, a statistic's
// rows by group).

import { requestDesign } from './abundance.js';
import { log10 } from './dmath.js';

// The families of spatial statistics a comparison can test.
export const SPATIAL_FAMILIES = [
  { id: 'enrichment', label: 'Neighbor enrichment', log: true },
  { id: 'nearest', label: 'Nearest distances', log: true },
  { id: 'moran', label: 'Moran\'s I', log: false },
];

// The families a summary's samples have numbers of (its document's samples' statistics), in
// SPATIAL_FAMILIES' order.
export function spatialFamiliesOf(doc) {
  const has = new Set();
  for (const s of doc.samples ?? []) {
    for (const st of s.statistics ?? []) has.add(st.family);
  }
  return SPATIAL_FAMILIES.filter((f) => has.has(f.id)).map((f) => f.id);
}

// The request for a design's comparison of a summary's labels' pseudobulk: the design (as
// abundanceRequest has it), the intervals' level and the fewest cells of a label a row is tested
// with. A design that cannot be used is refused, saying why.
export function expressionRequest(design, summaryId, { level = 0.95, minCells = 10, name = '', version = 0 } = {}) {
  return { summary: requiredSummary(summaryId), design: requestDesign(design, { name, version }), level, minCells };
}

// The request for a design's comparison of a summary's spatial statistics: the families (none: all
// the summary has) and the intervals' level.
export function spatialRequest(design, summaryId, { families = [], level = 0.95, name = '', version = 0 } = {}) {
  return { summary: requiredSummary(summaryId), design: requestDesign(design, { name, version }), families, level };
}

function requiredSummary(id) {
  if (!id) throw new Error('Summarize the samples first.');
  return id;
}

// A comparison as the project records it: kind expression or spatial-comparison.
export function testRecord(manifest, { summary = null, design = null } = {}, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  return {
    kind: manifest.kind,
    id: manifest.id,
    name: manifest.name,
    summary,
    design,
    state: manifest.state,
    created: manifest.created,
    registered: now,
    method: { name: lineage.method?.name ?? '', version: lineage.method?.version ?? '', citation: lineage.method?.citation ?? '' },
    parameters: lineage.parameters ?? null,
    software: lineage.software ?? [],
    job: lineage.job ? { id: lineage.job.id, kind: lineage.job.kind } : null,
    summaryOf: manifest.summary ?? null,
  };
}

// The project's comparisons of a kind (expression or spatial-comparison), the latest first.
export function testsOf(project, kind) {
  return project.artifacts.filter((a) => a.kind === kind).sort((a, b) => (a.created < b.created ? 1 : -1));
}

// A number with its sign: "+1.31", "−0.80", "0.00".
export function signed(x, digits = 2) {
  const s = Math.abs(x).toFixed(digits);
  return Number(s) === 0 ? s : `${x > 0 ? '+' : '−'}${s}`;
}

// The project with a comparison recorded (unchanged when it already is).
export function addTest(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === record.kind && a.id === record.id)) return project;
  const s = record.summaryOf ?? {};
  let detail;
  if (record.kind === 'expression') {
    const found = (s.found ?? []).slice(0, 4).map((f) => `${f.gene} in ${f.label} ${signed(f.logFC)} log2`);
    const below = s.below ?? 0;
    detail = `Compared the labels' expression (${record.name.replace(/^Expression compared: /, '')}): ${s.tests ?? '?'} tests${below ? `, ${below} below an adjusted p of 0.05 (${found.join(', ')}${below > found.length ? ', …' : ''})` : ', none below an adjusted p of 0.05'}${s.untested?.length ? `; ${s.untested.join(', ')} untested` : ''}`;
  } else {
    const found = (s.found ?? []).slice(0, 4).map((f) => `${f.key} (${f.family}) ${f.ratio ? `${f.ratio.toFixed(2)}×` : signed(f.estimate, 3)}`);
    const n = (s.found ?? []).length;
    detail = `Compared the samples' spatial statistics (${record.name.replace(/^Spatial statistics compared: /, '')}): ${s.tests ?? '?'} tests${n ? `, ${n} below an adjusted p of 0.05 (${found.join(', ')}${n > found.length ? ', …' : ''})` : ', none below an adjusted p of 0.05'}`;
  }
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: record.kind, detail: `${detail} (artifact ${record.id})` }],
  };
}

// A label's tests of a contrast, a row a gene: its name, mean log2 count per million, log2 fold
// change with its interval, t, p and adjusted p; sorted by p (or by the fold's size), the first
// limit (all when 0), those whose names contain the filter (case aside).
export function geneRows(expression, label, contrast, { sort = 'p', limit = 0, filter = '' } = {}) {
  const r = expression.results.find((x) => x.label === label);
  const c = r?.contrasts?.find((x) => x.contrast === contrast);
  if (!r?.tested || !c) return [];
  const want = filter.trim().toLowerCase();
  let rows = r.genes.map((g, i) => ({ gene: expression.genes[g], aveLogCPM: r.aveLogCPM[i], logFC: c.logFC[i], lo: c.lo[i], hi: c.hi[i], t: c.t[i], p: c.p[i], padj: c.padj[i] }));
  if (want) rows = rows.filter((x) => x.gene.toLowerCase().includes(want));
  rows.sort(sort === 'fold' ? (a, b) => Math.abs(b.logFC) - Math.abs(a.logFC) || a.p - b.p : (a, b) => a.p - b.p || Math.abs(b.logFC) - Math.abs(a.logFC));
  return limit ? rows.slice(0, limit) : rows;
}

// A volcano's points: each tested gene's log2 fold change and −log10 p, and whether its adjusted p
// is below 0.05.
export function volcanoPoints(expression, label, contrast) {
  return geneRows(expression, label, contrast).map((x) => ({ gene: x.gene, x: x.logFC, y: x.p > 0 ? -log10(x.p) : 320, found: x.padj < 0.05 }));
}

// A label's comparison in words: the rows used, the genes tested and filtered, the prior.
export function labelWords(expression, label) {
  const r = expression.results.find((x) => x.label === label);
  if (!r) return '';
  const rows = expression.design.rows.length;
  if (!r.tested) return `Untested: ${r.note}.`;
  const left = rows - r.rows.length;
  const prior = r.dfPrior === null ? 'the genes\' variances agree beyond chance (one variance for all)' : `the genes' variances moderated toward a prior of ${r.dfPrior.toFixed(1)} degrees of freedom`;
  return `${r.rows.length} of ${rows} rows (${left ? `${left} with fewer than ${expression.minCells} of its cells left out` : `each with ${expression.minCells} of its cells or more`}); ${r.genes.length} genes tested, ${r.filtered} too little expressed; ${prior}; tests on ${Number.isInteger(r.dfTotal) ? r.dfTotal : r.dfTotal.toFixed(1)} degrees of freedom.`;
}

// A statistic's values by group, for its plot: each design row's value (null when its samples have
// none), unit and samples, grouped by the rows' groups in the design's order of levels.
export function statisticByGroup(comparison, familyId, key) {
  const f = comparison.families.find((x) => x.family === familyId);
  const k = f ? f.keys.indexOf(key) : -1;
  if (k < 0) return [];
  const rows = comparison.design.rows;
  const levels = [...new Set(rows.map((r) => r.group))];
  return levels.map((group) => {
    const points = rows.map((r, i) => ({ row: i, unit: r.unit, samples: r.samples, value: f.values[k][i] })).filter((p) => rows[p.row].group === group && p.value !== null);
    return { group, points, mean: points.reduce((a, p) => a + p.value, 0) / Math.max(1, points.length) };
  });
}

// A spatial test's effect in words: "1.84× (1.21 to 2.80)" for a ratio, "+0.042 (−0.010 to
// +0.094)" for a difference.
export function spatialEffectWords(family, test) {
  if (!test.tested) return test.note || 'untested';
  if (family.log) return `${test.ratio.toFixed(2)}× (${test.ratioLo.toFixed(2)} to ${test.ratioHi.toFixed(2)})`;
  return `${signed(test.estimate, 3)} (${signed(test.lo, 3)} to ${signed(test.hi, 3)})`;
}
