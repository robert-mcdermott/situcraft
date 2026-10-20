// Certificates (wave 8, slice 5; requirement E7; after MaveScape 0.8.0's certificate.js, the same
// author's): what a result (or a dataset's results, or the whole project's) rests on and what it
// reports, fingerprinted, written as a zip with a README saying what it proves and how to check it.
//
// certificate.json holds SituCraft's version and commit; the subject; the project by its digest;
// each bundle it rests on by its run and its files' SHA-256; and each step from the bundles to the
// result, in the record's order (lib/record.js): the program's (a job) with its job's kind and
// spec by digest and every file it made by SHA-256, the page's own (a filter, a design, a marker
// set, labels curated, a decision's subject, a figure) by the digest of its record, a figure with
// its drawing's digests, each with the numbers it reports (its summary). Its fingerprint, the
// SHA-256 of its canonical JSON, names exactly what was certified.
//
// What it proves: that these bundles, analyzed as these steps record, give these files and
// numbers; anyone with the same bundles can make each step again and compare (situcraft verify,
// slice 6). Not that the result is right: that rests on the tissue, the panel and the methods'
// assumptions, which the methods state.
//
// A certificate has no time of its own and keeps no path of the computer that made it: the same
// project and SituCraft give the same certificate. A bundle's folder is written
// situcraft:bundle/<its dataset's ID>, any other file situcraft:file/<its name>.

import { sha256 } from './sha256.js';
import { canonicalJSON } from './figure-manifest.js';
import { itemOf, recordJSON, recordMarkdown } from './record.js';
import { methodsBibTeX, methodsMarkdown } from './methods-text.js';
import { createZip, readZip } from './zip.js';

export const CERTIFICATE_FORMAT = 'situcraft-certificate';
export const CERTIFICATE_VERSION = 1;
export const CERTIFICATE_NAME = 'certificate.json';
export const README_NAME = 'README.txt';
export const FILES_NAME = 'manifest.json';
export const EXIT = { verified: 0, differs: 1, incomplete: 2 };
// Every entry's date: a certificate has no time of its own.
const ZIP_DATE = new Date(Date.UTC(2026, 0, 1));
const encoder = new TextEncoder();
const decoder = new TextDecoder();

const count = (n) => Number(n).toLocaleString('en-US');
const plural = (n, word, many = `${word}s`) => `${count(n)} ${n === 1 ? word : many}`;
const textDigest = (text) => sha256(encoder.encode(text));
export const digestOf = (value) => sha256(encoder.encode(canonicalJSON(value ?? null)));
export const shortFingerprint = (fingerprint) => (fingerprint ? fingerprint.slice(0, 16).replace(/(.{4})(?!$)/g, '$1-') : '');

// --- Paths --------------------------------------------------------------------------------------

const isAbsolute = (s) => s.length > 1 && /^(\/|[A-Za-z]:[\\/]|\\\\)/.test(s);
// An absolute path within a sentence (a project's history says where a bundle was opened from).
const WITHIN = /(?:\/(?:Users|home|private|tmp|var|Volumes|mnt|media|opt|srv|data|root)\/|[A-Za-z]:\\)[^\s)"'`,;]*/g;
const escape = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// A value with this computer's paths stood in for: a bundle's folder (or a file in it) by
// situcraft:bundle/<dataset ID>[/<path in the bundle>], any other absolute path by
// situcraft:file/<its name>; wherever they are, a whole value or within a sentence.
export function redactPaths(value, project) {
  const bundles = project.datasets.filter((d) => d.path).map((d) => [String(d.path).replace(/[\\/]+$/, ''), d.id]).sort((a, b) => b[0].length - a[0].length);
  const path = (text) => {
    for (const [root, id] of bundles) {
      if (text === root || text.startsWith(`${root}/`) || text.startsWith(`${root}\\`)) return `situcraft:bundle/${id}${text.slice(root.length).replace(/\\/g, '/')}`;
    }
    return `situcraft:file/${text.split(/[\\/]/).filter(Boolean).pop() ?? ''}`;
  };
  const one = (text) => {
    if (isAbsolute(text)) return path(text);
    let out = text;
    for (const [root, id] of bundles) out = out.replace(new RegExp(`${escape(root)}(?=[\\/\\s)"'\`,;]|$)((?:[\\/][^\\s)"'\`,;]*)?)`, 'g'), (m, rest) => `situcraft:bundle/${id}${rest.replace(/\\/g, '/')}`);
    return out.replace(WITHIN, (m) => path(m));
  };
  const walk = (v) => {
    if (typeof v === 'string') return one(v);
    if (Array.isArray(v)) return v.map(walk);
    if (v && typeof v === 'object') return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, walk(x)]));
    return v;
  };
  return walk(value);
}

// --- The certificate ----------------------------------------------------------------------------

// A bundle as the certificate names it: its run, and its files by SHA-256 (those its dataset's
// fingerprint records, and those the instrument's segmentation read).
function bundleOf(project, dataset, manifests) {
  const files = new Map();
  for (const f of dataset.fingerprint ?? []) files.set(f.path, { path: f.path, size: f.size ?? null, sha256: f.sha256 ?? null });
  for (const [id, m] of manifests) {
    if (!m || itemOf(project, id)?.record?.dataset !== dataset.id) continue;
    for (const f of m.files ?? []) {
      if (f.location !== 'bundle') continue;
      files.set(f.path, { path: f.path, size: f.size ?? files.get(f.path)?.size ?? null, sha256: f.sha256 });
    }
  }
  return {
    dataset: dataset.id, name: dataset.name, run: dataset.run ?? null, region: dataset.region ?? null,
    analysisUuid: dataset.analysisUuid ?? null, experimentUuid: dataset.experimentUuid ?? null,
    software: dataset.software ?? null, panel: dataset.panel ?? null, cells: dataset.cells ?? null, transcripts: dataset.transcripts ?? null,
    files: [...files.values()].sort((a, b) => (a.path < b.path ? -1 : 1)),
  };
}

// What a certificate's steps are not made again from, and why: [{ step, what, why }].
function notMadeAgain(project, steps) {
  const out = [];
  for (const s of steps) {
    const r = itemOf(project, s.id)?.record ?? {};
    const say = (why) => out.push({ step: s.n, what: s.name, why });
    if (s.kind === 'dataset') say('the instrument\'s output: its files are checked by their SHA-256');
    else if (s.kind === 'segmentation' && r.source === 'instrument') say('the instrument\'s segmentation, read from its bundle: its files are checked by their SHA-256');
    else if (s.kind === 'segmentation' && r.source === 'imported') say(`made by ${r.method?.name ?? 'another tool'} outside SituCraft: the files read are checked by their SHA-256`);
    else if (s.kind === 'labels' && r.source) say(`labels read from ${r.source.format === 'instrument' ? 'the instrument\'s clusters' : r.source.file ?? 'a file'}: checked by the SHA-256 of what was read`);
    else if (s.kind === 'image') say('an image read where it lies: checked by its SHA-256');
    else if (['region', 'curated-labels', 'cluster-names', 'marker-set', 'design', 'consensus-labels', 'view'].includes(s.kind)) say('a person\'s work, recorded as made (its digest); what follows from it is made again');
  }
  return out;
}

// The certificate of a record (lib/record.js; made without a time) and its methods
// (lib/methods-text.js). options: { manifests (artifact ID → its manifest, every program step's),
// figures (figure ID → { drawing, panels: [{ id, letter, drawing }] }, as its manifest has them),
// software: { version, commit } }. Throws when a step SituCraft made is not complete, or its
// manifest is missing. Answers { certificate, files: [{ name, data }] } without the README and
// the files' list (writeCertificate adds them).
export function buildCertificate(project, record, methods, { manifests = new Map(), figures = new Map(), software = {} } = {}) {
  const red = (v) => redactPaths(v, project);
  const files = [];
  const add = (name, text) => {
    files.push({ name, data: text });
    return { path: name, sha256: textDigest(text) };
  };
  const steps = [];
  const bundles = [];
  for (const s of record.steps) {
    const item = itemOf(project, s.id);
    const r = item?.record ?? {};
    const base = { n: s.n, id: s.id, kind: s.kind, name: s.name, inputs: s.inputs, method: s.method, parameters: s.parameters === null ? null : red(s.parameters), seeds: s.seeds, software: s.software, references: s.references, decisions: s.decisions };
    if (s.kind === 'dataset') {
      bundles.push(bundleOf(project, r, manifests));
      steps.push({ ...base, by: 'instrument', bundle: r.id });
      continue;
    }
    if (r.registered) {
      const m = manifests.get(s.id);
      if (!m) throw new Error(`${s.name} (${s.id}) is not in SituCraft's store of results: it cannot be certified.`);
      if ((m.state ?? r.state) !== 'complete') throw new Error(`${s.name} is ${m.state ?? r.state}: only complete results are certified.`);
      const manifest = red(m);
      const text = `${JSON.stringify(manifest, null, 1)}\n`;
      steps.push({
        ...base,
        by: 'program',
        job: manifest.lineage?.job ? { kind: manifest.lineage.job.kind ?? null, spec: digestOf(manifest.lineage.job.spec ?? null) } : null,
        manifest: add(`artifacts/${s.id}/manifest.json`, text),
        files: (manifest.files ?? []).map(({ path, role, size, sha256: sum, location }) => ({ path, role: role ?? '', size: size ?? null, sha256: sum, location: location ?? '' })),
        summary: m.summary ?? null,
      });
      continue;
    }
    const step = { ...base, by: 'page', record: digestOf(red(r)), summary: r.summary ?? r.summaryOf ?? r.result ?? null };
    const drawn = figures.get(s.id);
    if (drawn) step.drawing = { drawing: drawn.drawing, panels: drawn.panels };
    steps.push(step);
  }
  const projectText = `${JSON.stringify(red(project), null, 1)}\n`;
  const certificate = {
    format: CERTIFICATE_FORMAT,
    version: CERTIFICATE_VERSION,
    software: { name: 'SituCraft', version: software.version ?? null, commit: software.commit || null },
    subject: record.result,
    project: { id: project.id, name: project.name, modified: project.modified, ...add('project.json', projectText) },
    record: add('record.json', recordJSON(red(record))),
    methods: add('methods.md', methodsMarkdown(red(methods))),
    bundles,
    steps,
    notMadeAgain: notMadeAgain(project, steps),
  };
  add('record.md', recordMarkdown(red(record)));
  add('references.bib', methodsBibTeX(methods));
  certificate.fingerprint = certificateFingerprint(certificate);
  return { certificate, files };
}

export function certificateFingerprint(certificate) {
  const { fingerprint, ...rest } = certificate;
  return digestOf(rest);
}

// JSON indented by one space, its lists of numbers or text on one line.
export function certificateText(value) {
  const flat = (v) => Array.isArray(v) && v.every((x) => x === null || typeof x !== 'object');
  const write = (v, indent) => {
    if (v === null || typeof v !== 'object' || flat(v)) return JSON.stringify(v);
    const inner = `${indent} `;
    if (Array.isArray(v)) return v.length ? `[\n${v.map((x) => `${inner}${write(x, inner)}`).join(',\n')}\n${indent}]` : '[]';
    const entries = Object.entries(v).filter(([, x]) => x !== undefined);
    return entries.length ? `{\n${entries.map(([k, x]) => `${inner}${JSON.stringify(k)}: ${write(x, inner)}`).join(',\n')}\n${indent}}` : '{}';
  };
  return `${write(value, '')}\n`;
}

const SUBJECTS = { project: 'the project', dataset: 'the dataset' };

// The README: what the certificate is of, what it proves, how to check it, what it holds.
export function readme(certificate) {
  const c = certificate;
  const v = c.software.version ?? '?';
  const program = c.steps.filter((s) => s.by === 'program');
  const page = c.steps.filter((s) => s.by === 'page');
  const madeFiles = program.reduce((n, s) => n + s.files.filter((f) => f.location !== 'bundle').length, 0);
  const row = (name, text) => `  ${name.padEnd(28)} ${text}`;
  const of = SUBJECTS[c.subject.kind] ? `${SUBJECTS[c.subject.kind]} ${c.subject.name}` : `${c.subject.name} (${c.subject.kind}, ${c.subject.id})`;
  const lines = [
    'SituCraft certificate',
    '=====================',
    '',
    `Of:           ${of}`,
    `Project:      ${c.project.name}, as of ${c.project.modified} (its last change)`,
    `Certified by: SituCraft ${v}${c.software.commit ? ` (commit ${c.software.commit.slice(0, 12)})` : ''}`,
    `Fingerprint:  ${c.fingerprint}`,
    `              (short form ${shortFingerprint(c.fingerprint)}; cite it with the result)`,
    '',
    `This certificate holds what the result rests on and what it reports: ${plural(c.bundles.length, 'bundle')} (their`,
    `files by SHA-256) and the ${plural(c.steps.length, 'step')} from them to it, in the order each was made from the`,
    `others: ${plural(program.length, 'step')} SituCraft's jobs made (each job's kind, its spec by digest and the`,
    `${plural(madeFiles, 'file')} they wrote, by SHA-256) and ${plural(page.length, 'record')} of the page's own (by digest), each with`,
    'the numbers it reports; the project as it was; the record of each step and the methods.',
    '',
    'What it proves',
    '--------------',
    'That these bundles, analyzed as these steps record, give these files and numbers: anyone with',
    'the same bundles can make each step again and compare its files bit for bit (SituCraft gives',
    'the same bits on every computer), and the record is the one certified (its fingerprint). It',
    'does not prove that the result is right: that rests on the tissue, the panel and the methods\'',
    'assumptions, which the methods (methods.md) state.',
    '',
    'To check it',
    '-----------',
    `Install SituCraft ${v} or later (https://github.com/robert-mcdermott/situcraft/releases), then run:`,
    '  situcraft verify <this file> --data <a folder holding the bundles>',
    'The bundles are found in the folder by their files\' SHA-256, whatever their folders are named.',
    'Exit status: 0 verified, 1 something differs (listed), 2 it could not be checked (a bundle or',
    'a file missing). Another version of SituCraft can check it too, and says so: a step may then',
    'differ where the version changed how it is made.',
    '',
    'Contents',
    '--------',
    row(CERTIFICATE_NAME, 'what was certified: the bundles, each step, the numbers reported'),
    row('project.json', 'the project as it was'),
    row('record.md, record.json', 'each step from the bundles to the result'),
    row('methods.md, references.bib', 'the methods, written from the record, and their references'),
    row('artifacts/<ID>/manifest.json', 'each step SituCraft\'s jobs made: its job, parameters and files'),
    row(FILES_NAME, 'every file of this archive with its SHA-256'),
    '',
    'Not made again',
    '--------------',
    ...(c.notMadeAgain.length ? c.notMadeAgain.map((x) => `  - Step ${x.n ?? x.step}, ${x.what}: ${x.why}.`) : ['  (nothing)']),
    '',
    'Paths',
    '-----',
    'Where files lay on the computer that certified it is not kept: a bundle\'s folder is written',
    'situcraft:bundle/<its dataset\'s ID>, any other file situcraft:file/<its name>.',
  ];
  return `${lines.join('\n')}\n`;
}

// The certificate as a zip: README.txt, certificate.json, the files it names, and manifest.json
// (every other file with its SHA-256). The same certificate gives the same bytes.
export async function writeCertificate(built) {
  const { certificate, files } = built;
  const all = [
    { name: README_NAME, data: readme(certificate) },
    { name: CERTIFICATE_NAME, data: certificateText(certificate) },
    ...[...files].sort((a, b) => (a.name < b.name ? -1 : 1)),
  ];
  const listed = all.map((f) => {
    const bytes = typeof f.data === 'string' ? encoder.encode(f.data) : f.data;
    return { path: f.name, size: bytes.length, sha256: sha256(bytes) };
  });
  all.push({ name: FILES_NAME, data: `${JSON.stringify({ format: 'situcraft-certificate-files', version: 1, files: listed }, null, 1)}\n` });
  return createZip(all, { date: ZIP_DATE, utc: true });
}

// --- Reading ------------------------------------------------------------------------------------

// A certificate read from its zip: { certificate, files (name → bytes), problems: [text] (an
// archive edited since: its fingerprint, a file's SHA-256, a file named but missing) }. Throws when
// the bytes are not a SituCraft certificate.
export async function readCertificate(bytes) {
  let files;
  try {
    files = await readZip(bytes);
  } catch (error) {
    throw new Error(`This is not a SituCraft certificate: ${error.message}`);
  }
  if (!files.has(CERTIFICATE_NAME)) throw new Error(`This zip is not a SituCraft certificate: it holds no ${CERTIFICATE_NAME}.`);
  let certificate;
  try {
    certificate = JSON.parse(decoder.decode(files.get(CERTIFICATE_NAME)));
  } catch (error) {
    throw new Error(`${CERTIFICATE_NAME} cannot be read: ${error.message}`);
  }
  if (certificate?.format !== CERTIFICATE_FORMAT) throw new Error(`${CERTIFICATE_NAME} is not a SituCraft certificate.`);
  if (!(certificate.version >= 1) || certificate.version > CERTIFICATE_VERSION) throw new Error(`The certificate was written by a newer SituCraft (format ${certificate.version}); check it with SituCraft ${certificate.software?.version ?? 'of that version'} or later.`);
  const problems = [];
  if (certificateFingerprint(certificate) !== certificate.fingerprint) problems.push(`${CERTIFICATE_NAME} is not as certified: its fingerprint does not match its content.`);
  const named = [certificate.project, certificate.record, certificate.methods, ...certificate.steps.map((s) => s.manifest).filter(Boolean)];
  for (const f of named) {
    const data = files.get(f.path);
    if (!data) problems.push(`${f.path} is missing.`);
    else if (sha256(data) !== f.sha256) problems.push(`${f.path} is not as certified (its SHA-256 differs).`);
  }
  if (files.has(FILES_NAME)) {
    try {
      const list = JSON.parse(decoder.decode(files.get(FILES_NAME))).files ?? [];
      for (const f of list) {
        const data = files.get(f.path);
        if (data && sha256(data) !== f.sha256 && f.path !== CERTIFICATE_NAME && !named.some((x) => x.path === f.path)) problems.push(`${f.path} is not as written (its SHA-256 differs from ${FILES_NAME}'s).`);
      }
    } catch {
      problems.push(`${FILES_NAME} cannot be read.`);
    }
  }
  return { certificate, files, problems };
}

// A certificate in a few words, for a window or a terminal.
export function certificateSummary(certificate) {
  const c = certificate;
  const program = c.steps.filter((s) => s.by === 'program').length;
  return `${c.subject.name}: ${plural(c.steps.length, 'step')} from ${plural(c.bundles.length, 'bundle')} (${program} made by SituCraft's jobs, ${c.steps.length - program - c.bundles.length} the page's own), fingerprint ${shortFingerprint(c.fingerprint)}.`;
}
