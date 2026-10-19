// Differential abundance (wave 7, slice 6; api_abundance.go, internal/cohort Abundance): the
// comparison's request from a design (lib/design.js makeDesign) and a cohort summary, the
// comparison as the project records it, and a result's rows by group for its plot.

// The families a comparison can test.
export const ABUNDANCE_FAMILIES = [
  { id: 'share', label: 'Shares of the labeled cells', effect: 'odds ratio' },
  { id: 'density', label: 'Cells per mm² of tissue', effect: 'density ratio' },
  { id: 'niche', label: 'Niches\' shares', effect: 'odds ratio' },
];

// The request for a design's comparison of a summary's samples: the design's columns, its rows
// (each a unit in a group, its samples by id, its columns' values), its contrasts, the samples it
// leaves out with their reasons; the families and the intervals' level. A design that cannot be
// used is refused, saying why.
export function abundanceRequest(design, summaryId, { families = ['share', 'density'], level = 0.95, name = '', version = 0 } = {}) {
  const d = requestDesign(design, { name, version });
  if (!summaryId) throw new Error('Summarize the samples first.');
  return { summary: summaryId, design: d, families, level };
}

// A design as a comparison's request has it (internal/cohort ComparisonDesign): its name and
// version when saved, its columns, rows, contrasts and the samples it leaves out. A design that
// cannot be used is refused, saying why.
export function requestDesign(design, { name = '', version = 0 } = {}) {
  if (!design.ok) throw new Error(`The design cannot be used: ${design.problems.join(' ')}`);
  const rows = design.rows.map((r, k) => ({ unit: String(r.unit), group: String(r.group), samples: [...r.samples], x: design.matrix[k] }));
  return {
    ...(name ? { name } : {}),
    ...(version ? { version } : {}),
    spec: design.spec,
    names: design.names,
    rows,
    contrasts: design.contrasts.map((c) => ({ name: c.name, vector: c.vector })),
    ...(design.excluded?.length ? { excluded: design.excluded.map((e) => ({ sample: e.id, name: e.name, reason: e.reason })) } : {}),
  };
}

// A comparison as the project records it.
export function abundanceRecord(manifest, { summary = null, design = null } = {}, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  return {
    kind: 'abundance',
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
    findings: manifest.findings ?? [],
  };
}

// The project's comparisons, the latest first.
export function comparisonsOf(project) {
  return project.artifacts.filter((a) => a.kind === 'abundance').sort((a, b) => (a.created < b.created ? 1 : -1));
}

// The project with a comparison recorded (unchanged when it already is).
export function addAbundance(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'abundance' && a.id === record.id)) return project;
  const s = record.summaryOf ?? {};
  const found = (s.found ?? []).map((f) => `${f.label} ${f.ratio.toFixed(2)}×`);
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'abundance', detail: `Compared the samples' abundances (${record.name.replace(/^Abundance compared: /, '')}): ${s.tests ?? '?'} tests${found.length ? `, ${found.length} below an adjusted p of 0.05 (${found.slice(0, 4).join(', ')}${found.length > 4 ? ', …' : ''})` : ', none below an adjusted p of 0.05'} (artifact ${record.id})` }],
  };
}

// A result's values by group, for its plot: each design row's value of the label (its share or
// density), unit and samples, grouped by the rows' groups in the design's order of levels.
export function rowsByGroup(comparison, familyId, label) {
  const f = comparison.families.find((x) => x.family === familyId);
  const k = f ? f.labels.indexOf(label) : -1;
  if (k < 0) return [];
  const rows = comparison.design.rows;
  const levels = [...new Set(rows.map((r) => r.group))];
  return levels.map((group) => ({
    group,
    points: rows.map((r, i) => ({ row: i, unit: r.unit, samples: r.samples, value: f.values[k][i], count: f.counts[k][i], total: f.totals[i] })).filter((p) => rows[p.row].group === group),
  })).map((g) => ({ ...g, mean: g.points.reduce((a, p) => a + p.value, 0) / Math.max(1, g.points.length) }));
}

// An effect in words: "1.84× (1.21 to 2.80)".
export function effectWords(test, digits = 2) {
  if (!test.tested) return test.note || 'untested';
  return `${test.ratio.toFixed(digits)}× (${test.lo.toFixed(digits)} to ${test.hi.toFixed(digits)})`;
}
