// A project's samples: what a cohort compares (wave 7). Each dataset is a sample until regions of
// it are made samples instead (a tissue microarray's cores, sections on one slide). A sample has
// the user's fields (subject, condition and any added, as text or numbers) and its bundle's, read
// from it (run, slide, the Xenium region, panel, software). Pure functions: each returns a new
// project and leaves its argument alone.
//
// project.samples holds a record only for a sample something was said of: { id, dataset, region
// (a region's id, or null for the whole dataset), name (when given), fields: { name: value } }. A
// dataset with no record is a sample of its own, with no fields. project.sampleFields holds the
// user's fields in order, [{ name, kind: 'text' | 'number' }] (subject and condition until changed).

import { regionsOf } from './regions.js';
import { touch } from './project.js';

export const DEFAULT_FIELDS = [{ name: 'subject', kind: 'text' }, { name: 'condition', kind: 'text' }];
export const FIELD_KINDS = ['text', 'number'];

// The fields read from a sample's bundle: { key, label, of(dataset) }.
export const BUNDLE_FIELDS = [
  { key: 'run', label: 'Run', of: (d) => d.run ?? '' },
  { key: 'slide', label: 'Slide', of: (d) => d.slide ?? '' },
  { key: 'xenium_region', label: 'Xenium region', of: (d) => d.region ?? '' },
  { key: 'panel', label: 'Panel', of: (d) => d.panelDesign || d.panel || '' },
  { key: 'software', label: 'Software', of: (d) => d.software ?? '' },
];

// The table's own columns: a field may not take their names.
const RESERVED = new Set(['sample', 'dataset', 'region', 'cells', 'area_um2', ...BUNDLE_FIELDS.map((f) => f.key)]);

export const sampleFields = (project) => project.sampleFields ?? DEFAULT_FIELDS;

const datasetSampleId = (datasetId) => `s-${datasetId}`;
const regionSampleId = (regionId) => `s-${regionId}`;

// The project's samples, in the order of its datasets (a dataset's regions in the order they were
// drawn): { id, name, dataset (its record), region (its current version, or null), removed (its
// region removed or gone), fields, record (the stored one, or null) }. Names are unique: where two
// would be the same, later ones are numbered.
export function samplesOf(project) {
  const records = project.samples ?? [];
  const out = [];
  for (const dataset of project.datasets) {
    const own = records.filter((s) => s.dataset === dataset.id);
    const ofRegions = own.filter((s) => s.region);
    if (ofRegions.length) {
      const regions = new Map(regionsOf(project, dataset.id, { removed: true }).map((r) => [r.id, r]));
      for (const record of ofRegions) {
        const region = regions.get(record.region) ?? null;
        out.push({ id: record.id, given: record.name ?? '', name: record.name || region?.name || record.region, dataset, region, removed: !region || Boolean(region.removed), fields: record.fields ?? {}, record });
      }
    } else {
      const record = own.find((s) => !s.region) ?? null;
      out.push({ id: record?.id ?? datasetSampleId(dataset.id), given: record?.name ?? '', name: record?.name || dataset.name, dataset, region: null, removed: false, fields: record?.fields ?? {}, record });
    }
  }
  const seen = new Map();
  for (const s of out) {
    const n = (seen.get(s.name) ?? 0) + 1;
    seen.set(s.name, n);
    if (n > 1) s.name = `${s.name} (${n})`;
  }
  return out;
}

// A sample's value of a field: its own, or a bundle field's; '' when it has none (missing is not
// zero).
export function fieldValue(sample, key) {
  const bundle = BUNDLE_FIELDS.find((f) => f.key === key);
  if (bundle) return bundle.of(sample.dataset);
  const v = sample.fields[key];
  return v === undefined || v === null ? '' : v;
}

const history = (project, detail, now) => ({ ...project, history: [...project.history, { time: now, action: 'samples', detail }] });

// The project with a sample's record changed by edit(record), the record made first when the sample
// had none.
function withRecord(project, sampleId, edit) {
  const sample = samplesOf(project).find((s) => s.id === sampleId);
  if (!sample) throw new Error('The project has no such sample.');
  const base = sample.record ?? { id: sample.id, dataset: sample.dataset.id, region: sample.region?.id ?? null, fields: {} };
  const next = edit({ ...base, fields: { ...base.fields } });
  if (!next.name) delete next.name;
  const records = project.samples ?? [];
  return { project: { ...project, samples: sample.record ? records.map((s) => (s.id === sampleId ? next : s)) : [...records, next] }, sample };
}

// A value as a field of a kind keeps it: a number field's a finite number, a text field's trimmed
// text; null for none (an empty value).
export function fieldOf(kind, value) {
  if (value === null || value === undefined) return null;
  if (kind === 'number') {
    if (typeof value === 'number') {
      if (!Number.isFinite(value)) throw new Error(`${value} is not a number`);
      return value;
    }
    const text = String(value).trim();
    if (!text) return null;
    const v = Number(text);
    if (!Number.isFinite(v) || !/^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/.test(text)) throw new Error(`"${text}" is not a number`);
    return v;
  }
  const text = String(value).trim();
  return text ? text : null;
}

// The project with a sample's field set (null or '' clears it).
export function setSampleField(project, sampleId, name, value, now = new Date().toISOString()) {
  const field = sampleFields(project).find((f) => f.name === name);
  if (!field) throw new Error(`The samples have no field ${name}.`);
  const v = fieldOf(field.kind, value);
  const { project: next, sample } = withRecord(project, sampleId, (r) => {
    if (v === null) delete r.fields[name];
    else r.fields[name] = v;
    return r;
  });
  return touch(history(next, `${sample.name}: ${name} ${v === null ? 'cleared' : `set to ${v}`}`, now), now);
}

// The project with a sample named (an empty name gives it back its dataset's or region's).
export function renameSample(project, sampleId, name, now = new Date().toISOString()) {
  const text = String(name ?? '').trim();
  if (text && samplesOf(project).some((s) => s.id !== sampleId && s.name === text)) throw new Error(`Another sample is named ${text}.`);
  const { project: next, sample } = withRecord(project, sampleId, (r) => ({ ...r, name: text }));
  return touch(history(next, `Sample ${sample.name} ${text ? `named ${text}` : 'given back its own name'}`, now), now);
}

function checkFieldName(project, name, except = null) {
  const text = String(name ?? '').trim();
  if (!text) throw new Error('A field needs a name.');
  if (RESERVED.has(text.toLowerCase())) throw new Error(`${text} is a column of the table already.`);
  if (sampleFields(project).some((f) => f.name !== except && f.name.toLowerCase() === text.toLowerCase())) throw new Error(`The samples have a field ${text} already.`);
  return text;
}

// The project with a field added to its samples, last.
export function addSampleField(project, name, kind = 'text', now = new Date().toISOString()) {
  if (!FIELD_KINDS.includes(kind)) throw new Error(`A field is text or a number, not ${kind}.`);
  const text = checkFieldName(project, name);
  return touch(history({ ...project, sampleFields: [...sampleFields(project), { name: text, kind }] }, `Field ${text} (${kind}) added`, now), now);
}

const mapFields = (project, f) => (project.samples ?? []).map((s) => ({ ...s, fields: f({ ...s.fields }) }));

// The project with a field renamed, in every sample.
export function renameSampleField(project, name, to, now = new Date().toISOString()) {
  if (!sampleFields(project).some((f) => f.name === name)) throw new Error(`The samples have no field ${name}.`);
  const text = checkFieldName(project, to, name);
  if (text === name) return project;
  const next = {
    ...project,
    sampleFields: sampleFields(project).map((f) => (f.name === name ? { ...f, name: text } : f)),
    samples: mapFields(project, (fields) => {
      if (name in fields) {
        fields[text] = fields[name];
        delete fields[name];
      }
      return fields;
    }),
  };
  return touch(history(next, `Field ${name} renamed ${text}`, now), now);
}

// The project with a field removed, and its values.
export function removeSampleField(project, name, now = new Date().toISOString()) {
  if (!sampleFields(project).some((f) => f.name === name)) throw new Error(`The samples have no field ${name}.`);
  const values = (project.samples ?? []).filter((s) => s.fields?.[name] !== undefined).length;
  const next = {
    ...project,
    sampleFields: sampleFields(project).filter((f) => f.name !== name),
    samples: mapFields(project, (fields) => {
      delete fields[name];
      return fields;
    }),
  };
  return touch(history(next, `Field ${name} removed${values ? `, with ${values} value${values === 1 ? '' : 's'}` : ''}`, now), now);
}

// The project with a field's kind changed: to a number only when every value is one.
export function setFieldKind(project, name, kind, now = new Date().toISOString()) {
  if (!FIELD_KINDS.includes(kind)) throw new Error(`A field is text or a number, not ${kind}.`);
  const field = sampleFields(project).find((f) => f.name === name);
  if (!field) throw new Error(`The samples have no field ${name}.`);
  if (field.kind === kind) return project;
  const bad = [];
  const samples = (project.samples ?? []).map((s) => {
    if (s.fields?.[name] === undefined) return s;
    try {
      return { ...s, fields: { ...s.fields, [name]: kind === 'number' ? fieldOf('number', s.fields[name]) : String(s.fields[name]) } };
    } catch {
      bad.push(s.id);
      return s;
    }
  });
  if (bad.length) {
    const which = samplesOf(project).filter((s) => bad.includes(s.id)).map((s) => s.name);
    throw new Error(`${name} is not a number in ${which.slice(0, 4).join(', ')}${which.length > 4 ? ` and ${which.length - 4} more` : ''}.`);
  }
  const next = { ...project, sampleFields: sampleFields(project).map((f) => (f.name === name ? { ...f, kind } : f)), samples };
  return touch(history(next, `Field ${name} made ${kind === 'number' ? 'a number' : 'text'}`, now), now);
}

// The project with a dataset's regions as its samples instead of the dataset (an empty list: the
// dataset a sample again). A region already a sample keeps its record; the dataset's own record is
// kept, for when it is a sample again.
export function setRegionSamples(project, datasetId, regionIds, now = new Date().toISOString()) {
  const dataset = project.datasets.find((d) => d.id === datasetId);
  if (!dataset) throw new Error('The project has no such dataset.');
  const regions = regionsOf(project, datasetId);
  const chosen = [...new Set(regionIds)];
  for (const id of chosen) if (!regions.some((r) => r.id === id)) throw new Error(`${dataset.name} has no region ${id}.`);
  const records = project.samples ?? [];
  const before = records.filter((s) => s.dataset === datasetId && s.region);
  const kept = before.filter((s) => chosen.includes(s.region));
  const added = chosen.filter((id) => !before.some((s) => s.region === id)).map((id) => ({ id: regionSampleId(id), dataset: datasetId, region: id, fields: {} }));
  const dropped = before.length - kept.length;
  const others = records.filter((s) => s.dataset !== datasetId || !s.region);
  // The dataset's regions in the order they were drawn.
  const order = new Map(regions.map((r, k) => [r.id, k]));
  const ofDataset = [...kept, ...added].sort((a, b) => (order.get(a.region) ?? 0) - (order.get(b.region) ?? 0));
  const names = chosen.map((id) => regions.find((r) => r.id === id).name);
  const detail = chosen.length ? `${dataset.name}'s regions as samples: ${names.join(', ')}${dropped ? `; ${dropped} region sample${dropped === 1 ? '' : 's'} and ${dropped === 1 ? 'its' : 'their'} fields dropped` : ''}` : `${dataset.name} a sample again${dropped ? `; ${dropped} region sample${dropped === 1 ? '' : 's'} and ${dropped === 1 ? 'its' : 'their'} fields dropped` : ''}`;
  return touch(history({ ...project, samples: [...others, ...ofDataset] }, detail, now), now);
}

// --- The table as CSV ---------------------------------------------------------------------------------

const csvField = (v) => {
  const s = v === null || v === undefined ? '' : String(v);
  return /[",\n\r]/.test(s) || /^\s|\s$/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
};

// The samples as CSV: the sample's name, its dataset and region, the user's fields, then its
// bundle's (read only, for reference).
export function samplesCSV(project) {
  const fields = sampleFields(project);
  const head = ['sample', 'dataset', 'region', ...fields.map((f) => f.name), ...BUNDLE_FIELDS.map((f) => f.key)];
  const lines = [head.map(csvField).join(',')];
  for (const s of samplesOf(project)) {
    lines.push([s.name, s.dataset.name, s.region?.name ?? '', ...fields.map((f) => fieldValue(s, f.name)), ...BUNDLE_FIELDS.map((f) => f.of(s.dataset))].map(csvField).join(','));
  }
  return `${lines.join('\n')}\n`;
}

// A delimited table's rows (RFC 4180: quoted fields, doubled quotes, line breaks within quotes;
// commas, or tabs when the header has them and no comma): [header, ...rows].
export function parseTable(text) {
  const src = String(text).replace(/^﻿/, '');
  const first = src.split(/\r?\n/, 1)[0] ?? '';
  const sep = !first.includes(',') && first.includes('\t') ? '\t' : ',';
  const rows = [];
  let row = [];
  let field = '';
  let quoted = false;
  for (let i = 0; i < src.length; i += 1) {
    const ch = src[i];
    if (quoted) {
      if (ch === '"') {
        if (src[i + 1] === '"') {
          field += '"';
          i += 1;
        } else quoted = false;
      } else field += ch;
    } else if (ch === '"' && field === '') quoted = true;
    else if (ch === sep) {
      row.push(field);
      field = '';
    } else if (ch === '\n' || ch === '\r') {
      if (ch === '\r' && src[i + 1] === '\n') i += 1;
      row.push(field);
      rows.push(row);
      row = [];
      field = '';
    } else field += ch;
  }
  if (quoted) throw new Error('the table ends inside a quoted field');
  if (field || row.length) {
    row.push(field);
    rows.push(row);
  }
  return rows.filter((r) => r.length > 1 || r[0].trim());
}

// The project with a table's values read into its samples, and what was done: each row matched to
// a sample by its sample column (exactly, then ignoring case and spaces at the ends); its columns
// that are fields set (an empty cell clears); new columns added as fields (numbers when every value
// is one); the table's own and the bundle's columns left alone. Refused, saying why, when no column
// names the samples, a column is named twice, two rows name one sample, or a value does not fit.
// Answers { project, matched, unmatched (rows' names), untouched (samples not in the table),
// added (fields), ignored (columns) }.
export function readSamplesTable(project, text, now = new Date().toISOString()) {
  const rows = parseTable(text);
  if (rows.length < 1) throw new Error('The table is empty.');
  const header = rows[0].map((h) => h.trim());
  const lower = header.map((h) => h.toLowerCase());
  const dup = header.find((h, k) => h && lower.indexOf(h.toLowerCase()) !== k);
  if (dup) throw new Error(`The column ${dup} is in the table twice.`);
  const key = ['sample', 'sample_id', 'sample id', 'name'].map((k) => lower.indexOf(k)).find((k) => k >= 0);
  if (key === undefined) throw new Error('No column names the samples: call it sample.');
  const samples = samplesOf(project);
  const byName = new Map(samples.map((s) => [s.name, s]));
  const byLoose = new Map();
  for (const s of samples) {
    const k = s.name.trim().toLowerCase();
    byLoose.set(k, byLoose.has(k) ? null : s);
  }
  const ignored = [];
  const existing = new Map(sampleFields(project).map((f) => [f.name.toLowerCase(), f]));
  const columns = [];
  header.forEach((h, k) => {
    if (k === key) return;
    if (!h) {
      if (rows.slice(1).some((r) => (r[k] ?? '').trim())) throw new Error(`Column ${k + 1} has values but no name.`);
      return;
    }
    if (RESERVED.has(h.toLowerCase())) {
      ignored.push(h);
      return;
    }
    columns.push({ k, name: existing.get(h.toLowerCase())?.name ?? h, field: existing.get(h.toLowerCase()) ?? null });
  });
  const matched = [];
  const unmatched = [];
  const seen = new Set();
  for (const r of rows.slice(1)) {
    const name = (r[key] ?? '').trim();
    if (!name) {
      if (r.some((v) => v.trim())) unmatched.push('(no sample named)');
      continue;
    }
    const sample = byName.get(name) ?? byLoose.get(name.toLowerCase()) ?? null;
    if (!sample) {
      unmatched.push(name);
      continue;
    }
    if (seen.has(sample.id)) throw new Error(`Two rows name the sample ${sample.name}.`);
    seen.add(sample.id);
    matched.push({ sample, row: r });
  }
  // New columns: numbers when every value given is one.
  const added = [];
  let next = project;
  for (const c of columns) {
    if (c.field) continue;
    const values = matched.map(({ row }) => (row[c.k] ?? '').trim()).filter(Boolean);
    let kind = values.length ? 'number' : 'text';
    for (const v of values) {
      try {
        fieldOf('number', v);
      } catch {
        kind = 'text';
      }
    }
    next = addSampleField(next, c.name, kind, now);
    c.field = { name: c.name, kind };
    added.push(c.field);
  }
  // Every value checked before any is set.
  const sets = [];
  for (const { sample, row } of matched) {
    const fields = {};
    for (const c of columns) {
      try {
        fields[c.field.name] = fieldOf(c.field.kind, row[c.k] ?? '');
      } catch (error) {
        throw new Error(`${sample.name}, ${c.field.name}: ${error.message}.`);
      }
    }
    sets.push({ sample, fields });
  }
  for (const { sample, fields } of sets) {
    ({ project: next } = withRecord(next, sample.id, (r) => {
      for (const [name, v] of Object.entries(fields)) {
        if (v === null) delete r.fields[name];
        else r.fields[name] = v;
      }
      return r;
    }));
  }
  const untouched = samples.filter((s) => !seen.has(s.id)).map((s) => s.name);
  next = touch(history(next, `Samples' table read: ${matched.length} sample${matched.length === 1 ? '' : 's'} matched${unmatched.length ? `, ${unmatched.length} row${unmatched.length === 1 ? '' : 's'} not` : ''}${added.length ? `; fields added: ${added.map((f) => f.name).join(', ')}` : ''}`, now), now);
  return { project: next, matched: matched.map((m) => m.sample.name), unmatched, untouched, added, ignored };
}
