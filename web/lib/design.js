// The experimental design (wave 7, requirement H1): which samples are replicates of what, before
// any comparison of groups. A design names the replicate unit (a field of the samples, such as the
// subject), the groups compared (a field and its reference level), the contrasts, and covariates;
// a unit in more than one group pairs them. Its rows are the units in each group, a unit's samples
// in one group being technical replicates, combined; its columns, as a model formula's would be,
// the intercept, the groups against the reference, the units when paired, and the covariates. A
// design that cannot be estimated, or whose groups cannot be told from a batch, is refused, saying
// why. Pure functions; saved designs are kept in the project as versions (kind "design").
//
// Coding is treatment coding, as patsy's (and R's) default: a factor's levels in code-point order,
// its first (or the reference given) dropped, a column of ones and zeros for each other level, named
// field[T.level]; a number field's values as they are, after the factors (as patsy orders terms).
// validation/reference/designs/ checks the matrices against patsy's for the same formulas.

import { BUNDLE_FIELDS, fieldValue, sampleFields, samplesOf } from './samples.js';

const byCode = (a, b) => (a < b ? -1 : a > b ? 1 : 0);
const plural = (n, word, many = `${word}s`) => `${n.toLocaleString('en-US')} ${n === 1 ? word : many}`;

// The fields a design can use: the user's, then the bundle's batch fields (run, slide, panel).
export function designFields(project) {
  return [...sampleFields(project).map((f) => ({ name: f.name, kind: f.kind, bundle: false })), ...BUNDLE_FIELDS.filter((f) => ['run', 'slide', 'panel'].includes(f.key)).map((f) => ({ name: f.key, kind: 'text', bundle: true, label: f.label }))];
}

// The batch fields checked for confounding whether or not the design uses them.
const BATCH_FIELDS = ['slide', 'run', 'panel'];

// Names in words: "a", "a and b", "a, b and c".
const listed = (names) => (names.length < 3 ? names.join(' and ') : `${names.slice(0, -1).join(', ')} and ${names.at(-1)}`);

// The levels of a field's values in code-point order.
const levelsOf = (values) => [...new Set(values)].sort(byCode);

// A design's matrix for a set of terms over rows: [{ name, kind ('intercept', 'factor',
// 'number'), reference }], each row an object of the terms' values. Answers { names, columns
// (by term: [column indices]), matrix (rows × columns) }.
export function designMatrix(rows, terms) {
  const names = [];
  const columns = [];
  const cols = [];
  for (const term of terms) {
    const at = [];
    if (term.kind === 'intercept') {
      names.push('Intercept');
      cols.push(rows.map(() => 1));
      at.push(names.length - 1);
    } else if (term.kind === 'number') {
      names.push(term.name);
      cols.push(rows.map((r) => r[term.name]));
      at.push(names.length - 1);
    } else {
      const levels = levelsOf(rows.map((r) => String(r[term.name])));
      const reference = term.reference ?? levels[0];
      for (const level of levels) {
        if (level === reference) continue;
        names.push(`${term.name}[T.${level}]`);
        cols.push(rows.map((r) => (String(r[term.name]) === level ? 1 : 0)));
        at.push(names.length - 1);
      }
    }
    columns.push(at);
  }
  return { names, columns, matrix: rows.map((_, i) => cols.map((c) => c[i])) };
}

// The Euclidean norm (a square root of a sum, the same in every engine; Math.hypot is not).
const norm2 = (v) => {
  let s = 0;
  for (const x of v) s += x * x;
  return Math.sqrt(s);
};

// The rank the columns add in order: each column's part not in the span of those before it
// (modified Gram–Schmidt, twice), kept when its norm is more than 1e-9 of the column's own.
// Answers { rank, independent: [whether each column adds to the rank] }.
export function columnRank(matrix) {
  const n = matrix.length;
  const p = n ? matrix[0].length : 0;
  const basis = [];
  const independent = [];
  for (let j = 0; j < p; j += 1) {
    let v = matrix.map((row) => row[j]);
    const norm0 = norm2(v);
    for (let pass = 0; pass < 2; pass += 1) {
      for (const q of basis) {
        let dot = 0;
        for (let i = 0; i < n; i += 1) dot += q[i] * v[i];
        v = v.map((x, i) => x - dot * q[i]);
      }
    }
    const norm = norm2(v);
    const adds = norm0 > 0 && norm > 1e-9 * norm0;
    independent.push(adds);
    if (adds) basis.push(v.map((x) => x / norm));
  }
  return { rank: basis.length, independent };
}

// A design from the project's samples and a specification: { unit, group, reference, contrasts
// ([[a, b]]: a − b; each level against the reference if not given), covariates (field names),
// exclude ({ sample id: reason }) }. Answers the design: { ok, problems (why it cannot be
// estimated), notes, words (what it is, in sentences), samples (included, each with its unit,
// group, covariates and row), excluded ([{ id, name, reason }]), rows ([{ unit, group, samples,
// values }]), names, matrix, terms, levels, reference, contrasts ([{ name, a, b, vector }]),
// paired, rank, df, balance (each batch field's samples by group) }.
export function makeDesign(project, spec = {}) {
  const fields = designFields(project);
  const fieldNamed = (name) => fields.find((f) => f.name === name) ?? null;
  const problems = [];
  const notes = [];
  const words = [];
  const out = { ok: false, spec: normalizeSpec(spec), problems, notes, words, samples: [], excluded: [], rows: [], names: [], matrix: [], terms: [], levels: [], reference: null, contrasts: [], paired: false, pseudoSamples: false, rank: 0, df: 0, balance: [] };
  const { unit, group, covariates, exclude } = out.spec;
  if (!unit || !fieldNamed(unit)) problems.push(unit ? `There is no field ${unit} to be the replicate unit.` : 'Choose the replicate unit: the field that names each biological replicate (the subject, patient or animal).');
  if (!group || !fieldNamed(group)) problems.push(group ? `There is no field ${group} to compare.` : 'Choose the field whose groups are compared (the condition).');
  if (unit && group && unit === group) problems.push('The replicate unit and the groups are different fields.');
  for (const c of covariates) {
    if (!fieldNamed(c)) problems.push(`There is no field ${c} to adjust for.`);
    else if (c === unit || c === group) problems.push(`${c} is already in the design: it cannot also be a covariate.`);
  }
  if (problems.length) return out;

  // The samples: those with every value the design reads, not left out and not of a region removed.
  const value = (s, name) => {
    const v = fieldValue(s, name);
    return v === '' || v === null || v === undefined ? null : v;
  };
  for (const s of samplesOf(project)) {
    const reason = exclude[s.id];
    if (reason) {
      out.excluded.push({ id: s.id, name: s.name, reason });
      continue;
    }
    if (s.removed) {
      out.excluded.push({ id: s.id, name: s.name, reason: 'its region was removed' });
      continue;
    }
    const missing = [unit, group, ...covariates].filter((name) => value(s, name) === null);
    if (missing.length) {
      out.excluded.push({ id: s.id, name: s.name, reason: `no ${missing.join(' or ')}` });
      continue;
    }
    out.samples.push({ id: s.id, name: s.name, dataset: s.dataset.id, datasetName: s.dataset.name, region: s.region?.id ?? null, unit: String(value(s, unit)), group: String(value(s, group)), values: Object.fromEntries(covariates.map((c) => [c, fieldNamed(c).kind === 'number' ? Number(value(s, c)) : String(value(s, c))])), batch: Object.fromEntries(BATCH_FIELDS.map((b) => [b, String(fieldValue(s, b) || '—')])) });
  }
  const missingValues = out.excluded.filter((e) => e.reason.startsWith('no '));
  if (missingValues.length) notes.push(`${plural(missingValues.length, 'sample')} without a value the design reads ${missingValues.length === 1 ? 'is' : 'are'} left out: ${missingValues.map((e) => `${e.name} (${e.reason})`).join(', ')}.`);
  const leftOut = out.excluded.filter((e) => !e.reason.startsWith('no '));
  if (leftOut.length) notes.push(`Left out: ${leftOut.map((e) => `${e.name} (${e.reason})`).join(', ')}.`);

  // The groups and the reference.
  const levelsFound = levelsOf(out.samples.map((s) => s.group));
  out.reference = out.spec.reference && levelsFound.includes(out.spec.reference) ? out.spec.reference : null;
  if (out.spec.reference && !out.reference) problems.push(`No sample is in ${out.spec.reference}, the reference ${group}.`);
  if (levelsFound.length < 2) {
    problems.push(levelsFound.length ? `Every sample is in one ${group}, ${levelsFound[0]}: a comparison needs two groups.` : 'No sample has every value the design reads.');
    return out;
  }
  out.reference ??= levelsFound[0];
  out.levels = [out.reference, ...levelsFound.filter((l) => l !== out.reference)];

  // Units are independent: regions of one section are not separate replicates.
  const unitsOfDataset = new Map();
  for (const s of out.samples) {
    if (!unitsOfDataset.has(s.dataset)) unitsOfDataset.set(s.dataset, new Map());
    const m = unitsOfDataset.get(s.dataset);
    if (!m.has(s.unit)) m.set(s.unit, s.name);
  }
  for (const [, m] of unitsOfDataset) {
    if (m.size > 1) {
      const ds = out.samples.find((s) => m.has(s.unit)).datasetName;
      if (out.spec.pseudoSamples) {
        // For teaching only: kept, and said at the top of every comparison made of it.
        out.pseudoSamples = true;
        notes.unshift(`Pseudo-samples, for teaching: ${listed([...m.values()])} are regions of one section (${ds}) given different ${unit}s. They are not independent replicates: a comparison of them describes this section's regions, not tissue of different ${unit}s, and its p-values are not evidence about ${unit}s.`);
      } else {
        problems.push(`${listed([...m.values()])} are regions of one section (${ds}) given different ${unit}s: regions of one section of one tissue are not independent replicates. Give them one ${unit} (their samples are then combined), or compare sections.`);
      }
    }
  }

  // The rows: a unit in a group, its samples combined; a covariate one value a row.
  const rowOf = new Map();
  for (const s of out.samples) {
    const key = `${s.unit}\u0000${s.group}`;
    if (!rowOf.has(key)) {
      rowOf.set(key, out.rows.length);
      out.rows.push({ unit: s.unit, group: s.group, samples: [], values: { ...s.values } });
    }
    const row = out.rows[rowOf.get(key)];
    row.samples.push(s.id);
    s.row = rowOf.get(key);
    for (const c of covariates) {
      if (row.values[c] !== s.values[c]) problems.push(`${c} differs between ${s.unit}'s samples in ${s.group} (${row.values[c]} and ${s.values[c]}): a unit's covariate is one value in each group.`);
    }
  }
  const combined = out.rows.filter((r) => r.samples.length > 1);
  const nameOf = new Map(out.samples.map((s) => [s.id, s.name]));
  if (combined.length) notes.push(`Technical replicates combined: ${combined.map((r) => `${r.unit} in ${r.group} (${r.samples.map((id) => nameOf.get(id)).join(', ')})`).join('; ')}.`);

  // Units in each group; pairing.
  const unitsIn = (level) => new Set(out.rows.filter((r) => r.group === level).map((r) => r.unit));
  for (const level of out.levels) {
    const n = unitsIn(level).size;
    if (n < 2) problems.push(`${level} has ${plural(n, unit)}: a group needs at least two, so that the variation between ${unit}s can be measured.`);
  }
  const groupsOfUnit = new Map();
  for (const r of out.rows) groupsOfUnit.set(r.unit, (groupsOfUnit.get(r.unit) ?? 0) + 1);
  const pairedUnits = [...groupsOfUnit.values()].filter((n) => n > 1).length;
  out.paired = pairedUnits > 0;
  const allUnits = [...groupsOfUnit.keys()];
  if (out.paired && pairedUnits < allUnits.length) notes.push(`${plural(allUnits.length - pairedUnits, unit)} ${allUnits.length - pairedUnits === 1 ? 'is' : 'are'} in one ${group} only: paired, the design compares each ${unit} with itself, and ${allUnits.length - pairedUnits === 1 ? 'that one adds' : 'those add'} nothing to the difference between the groups.`);

  // The matrix, term by term: the rank each adds says what is confounded with what.
  const rowValues = out.rows.map((r) => ({ [group]: r.group, [unit]: r.unit, ...r.values }));
  const terms = [{ name: 'Intercept', kind: 'intercept' }, { name: group, kind: 'factor', reference: out.reference }];
  if (out.paired) terms.push({ name: unit, kind: 'factor' });
  // The factors before the numbers, each in the order given, as patsy orders a formula's terms.
  for (const c of covariates) if (fieldNamed(c).kind !== 'number') terms.push({ name: c, kind: 'factor' });
  for (const c of covariates) if (fieldNamed(c).kind === 'number') terms.push({ name: c, kind: 'number' });
  out.terms = terms;
  const dm = designMatrix(rowValues, terms);
  out.names = dm.names;
  out.matrix = dm.matrix;
  const { rank, independent } = columnRank(dm.matrix);
  out.rank = rank;
  terms.forEach((term, t) => {
    const lost = dm.columns[t].filter((j) => !independent[j]);
    if (!lost.length || term.kind === 'intercept') return;
    const earlier = terms.slice(0, t).filter((x) => x.kind !== 'intercept').map((x) => x.name);
    if (term.name === group) problems.push(`${group} cannot be estimated.`);
    else if (term.kind === 'number') problems.push(`${term.name} is determined by ${earlier.join(' and ')}: it cannot be told apart from ${earlier.length > 1 ? 'them' : 'it'}. Leave it out.`);
    else problems.push(`${term.name} is confounded with ${earlier.join(' and ')}: ${lost.map((j) => dm.names[j]).join(', ')} ${lost.length === 1 ? 'is a combination' : 'are combinations'} of the columns before. Leave it out, or add samples that set them apart.`);
  });
  out.df = out.rows.length - rank;
  if (out.rows.length && out.df <= 0) problems.push(`No degrees of freedom are left: ${plural(out.rows.length, 'row')} (${unit}s in ${group}s) and ${plural(rank, 'coefficient')}. Adjust for fewer covariates, or add ${unit}s.`);
  else if (out.df > 0 && out.df < 3) notes.push(`Only ${plural(out.df, 'degree')} of freedom ${out.df === 1 ? 'is' : 'are'} left for the variation between ${unit}s: estimates of it will be rough.`);

  // Batches: a batch that separates the groups makes their difference the batch's.
  for (const b of BATCH_FIELDS) {
    const label = BUNDLE_FIELDS.find((f) => f.key === b).label.toLowerCase();
    const values = levelsOf(out.samples.map((s) => s.batch[b]));
    const table = values.map((v) => ({ value: v, byGroup: Object.fromEntries(out.levels.map((l) => [l, out.samples.filter((s) => s.batch[b] === v && s.group === l).length])) }));
    out.balance.push({ field: b, label, table });
    if (values.length < 2 || covariates.includes(b)) continue;
    const separates = table.every((row) => out.levels.filter((l) => row.byGroup[l] > 0).length === 1) && out.levels.every((l) => table.filter((row) => row.byGroup[l] > 0).length >= 1);
    if (separates) {
      problems.push(`${group} is confounded with the ${label}: ${table.map((row) => `${row.value} holds only ${out.levels.find((l) => row.byGroup[l] > 0)}`).join('; ')}. A difference between the groups could be the ${label}'s; compare groups whose samples share ${label}s.`);
    } else if (table.some((row) => out.levels.filter((l) => row.byGroup[l] > 0).length < out.levels.length)) {
      notes.push(`Not every ${label} holds every ${group}: ${table.map((row) => `${row.value}: ${out.levels.map((l) => `${row.byGroup[l]} ${l}`).join(', ')}`).join('; ')}. Consider adjusting for the ${label}.`);
    }
  }

  // Contrasts: each level against the reference, or those asked.
  const coefficient = (level) => dm.names.indexOf(`${group}[T.${level}]`);
  const asked = out.spec.contrasts.length ? out.spec.contrasts : out.levels.slice(1).map((l) => [l, out.reference]);
  for (const [a, b] of asked) {
    if (!out.levels.includes(a) || !out.levels.includes(b) || a === b) {
      problems.push(`The contrast ${a} − ${b} is not of two of the ${group}s ${out.levels.join(', ')}.`);
      continue;
    }
    const vector = dm.names.map(() => 0);
    if (a !== out.reference) vector[coefficient(a)] += 1;
    if (b !== out.reference) vector[coefficient(b)] -= 1;
    out.contrasts.push({ name: `${a} − ${b}`, a, b, vector });
  }

  out.ok = problems.length === 0;
  // In words.
  const nUnits = allUnits.length;
  words.push(`Compares ${group}: ${out.levels.slice(1).join(', ')} against ${out.reference} (the reference), ${plural(out.samples.length, 'sample')} in all.`);
  if (out.paired) {
    words.push(`The replicate unit is the ${unit}: ${plural(nUnits, unit)}, ${pairedUnits === nUnits ? 'each' : `${pairedUnits} of them`} in more than one ${group}, so the design is paired: each ${unit} is compared with itself, and the ${unit}s enter the model.`);
  } else {
    words.push(`The replicate unit is the ${unit}: ${out.levels.map((l) => `${plural(unitsIn(l).size, unit)} in ${l}`).join(', ')}.`);
  }
  if (covariates.length) words.push(`Adjusted for ${covariates.map((c) => (fieldNamed(c).kind === 'number' ? `${c} (a number)` : `${c} (${plural(levelsOf(out.rows.map((r) => String(r.values[c]))).length, 'level')})`)).join(' and ')}.`);
  if (out.pseudoSamples) words.push(`Its ${unit}s are pseudo-samples: regions of one section, compared for teaching, not as replicates.`);
  words.push(`The model: ${dm.names.join(' + ')}; ${plural(dm.names.length, 'coefficient')} over ${plural(out.rows.length, 'row')} (${unit}s in ${group}s), ${plural(Math.max(0, out.df), 'degree')} of freedom left for the variation between ${unit}s.`);
  for (const c of out.contrasts) words.push(`${c.name}: from ${plural(unitsIn(c.a).size, unit)} in ${c.a} and ${plural(unitsIn(c.b).size, unit)} in ${c.b}${out.paired ? `, ${plural([...unitsIn(c.a)].filter((u) => unitsIn(c.b).has(u)).length, unit)} in both` : ''}.`);
  return out;
}

// A specification with every part present: { unit, group, reference, contrasts, covariates,
// exclude }.
export function normalizeSpec(spec = {}) {
  return {
    unit: spec.unit ?? '',
    group: spec.group ?? '',
    reference: spec.reference ?? '',
    contrasts: (spec.contrasts ?? []).map(([a, b]) => [String(a), String(b)]),
    covariates: [...(spec.covariates ?? [])],
    exclude: { ...(spec.exclude ?? {}) },
    // A teaching example's regions of one section as separate units (pseudo-samples): only when set.
    ...(spec.pseudoSamples ? { pseudoSamples: true } : {}),
  };
}

// --- Saved designs ------------------------------------------------------------------------------

function newId() {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`;
  return `des-${random.replace(/-/g, '').slice(0, 16)}`;
}

// A project's designs, by name, the latest version of each first; or a name's versions, oldest
// first.
export function designsOf(project) {
  const all = project.artifacts.filter((a) => a.kind === 'design');
  const latest = new Map();
  for (const d of all) if (!latest.has(d.name) || latest.get(d.name).version < d.version) latest.set(d.name, d);
  return [...latest.values()].sort((a, b) => (a.created < b.created ? 1 : -1));
}

export function designVersions(project, name) {
  return project.artifacts.filter((a) => a.kind === 'design' && a.name === name).sort((a, b) => a.version - b.version);
}

// A design's record: its specification, the samples as they were (each one's unit, group and
// covariates), the samples left out and why, and what it is (its rows, matrix, contrasts and
// words). Only a design that can be estimated is saved.
export function designRecord(design, { name = 'Design', previous = null, now = new Date().toISOString(), id = newId() } = {}) {
  if (!design.ok) throw new Error(`The design cannot be saved: ${design.problems[0]}`);
  return {
    kind: 'design',
    id,
    name,
    version: previous ? previous.version + 1 : 1,
    parent: previous?.id ?? null,
    created: now,
    spec: design.spec,
    samples: design.samples.map((s) => ({ id: s.id, name: s.name, dataset: s.dataset, region: s.region, unit: s.unit, group: s.group, values: s.values, row: s.row })),
    excluded: design.excluded,
    result: {
      levels: design.levels, reference: design.reference, paired: design.paired,
      rows: design.rows.map((r) => ({ unit: r.unit, group: r.group, samples: r.samples })),
      names: design.names, matrix: design.matrix, rank: design.rank, df: design.df,
      contrasts: design.contrasts, words: design.words, notes: design.notes,
    },
  };
}

// The project with a design saved as a new version of its name.
export function addDesign(project, design, { name = 'Design', now = new Date().toISOString(), id } = {}) {
  const previous = designVersions(project, name).at(-1) ?? null;
  const record = designRecord(design, { name, previous, now, id });
  return {
    project: {
      ...project,
      modified: now,
      artifacts: [...project.artifacts, record],
      history: [...project.history, { time: now, action: 'design', detail: `Saved design ${name} (version ${record.version}): ${design.words[0]}` }],
    },
    record,
  };
}

// Whether a saved design still describes the samples as they are: the same samples with the same
// units, groups and covariates. Answers the differences, in words (empty when the same).
export function designChanges(project, record) {
  const now = makeDesign(project, record.spec);
  const was = new Map(record.samples.map((s) => [s.id, s]));
  const is = new Map(now.samples.map((s) => [s.id, s]));
  const out = [];
  for (const [id, s] of was) {
    const t = is.get(id);
    if (!t) out.push(`${s.name} is no longer in it`);
    else if (t.unit !== s.unit || t.group !== s.group || JSON.stringify(t.values) !== JSON.stringify(s.values)) out.push(`${s.name}'s values changed`);
  }
  for (const [id, t] of is) if (!was.has(id)) out.push(`${t.name} is new`);
  return out;
}
