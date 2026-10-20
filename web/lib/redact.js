// What a report or a result package leaves out (wave 8, slice 8): what the computer and the people
// using it say of themselves, and, when asked, the samples' and subjects' names. Applied to the
// project (and the artifacts' manifests) before anything is written from it, so that the record,
// the methods, the certificate, the figures and the views all say the same; then every file
// written is searched for what was left out (findLeftOut).
//
// - People: each person a record names (who reviewed, drew, decided, curated: every `by` but
//   SituCraft's and a program's) is stood in for by "Person 1", "Person 2", … in the order the
//   project first names them, wherever the name occurs.
// - Notes: each note a person wrote on a review or a region is left out, wherever its text occurs
//   (a curated label's reason and the project's history repeat it).
// - Names, when asked: each sample by "Sample 1", "Sample 2", … in the project's order (its
//   dataset's name too, when the dataset is the sample), each subject by "Subject 1", … and a
//   dataset whose regions are its samples by "Section 1", … wherever they occur.
// - Paths: this computer's are stood in for first, as the certificate stands them in
//   (lib/certificate.js redactPaths: a bundle's folder by situcraft:bundle/<its dataset's ID>), so
//   that a name that is also a folder's changes nothing a job was given but where it lay.

import { samplesOf } from './samples.js';
import { redactPaths } from './certificate.js';

// The `by` of SituCraft and of programs: not people.
export const PROGRAM_AUTHORS = new Set(['SituCraft', 'the user', 'remote control', 'a program on this computer', 'a program, by remote control', 'docs/capture']);

// A review's note SituCraft writes itself.
const OWN_NOTES = new Set(['Accepted on review', 'Included again', 'Resolved']);

// Text that is not words: base64 (a cell selection, label codes) and digests.
const OPAQUE = /^[A-Za-z0-9+/=]{40,}$/;
const OPAQUE_RUNS = /[A-Za-z0-9+/=]{40,}/g;
const escape = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// A name stands alone: not within a longer word or an identifier (a file's name may hold it
// between underscores).
const tokenPattern = (names) => new RegExp(`(?<![A-Za-z0-9.-])(?:${names.map(escape).join('|')})(?![A-Za-z0-9-]|\\.[A-Za-z0-9])`, 'g');

function walkStrings(value, visit, key = null) {
  if (typeof value === 'string') return visit(value, key);
  if (Array.isArray(value)) return value.map((v) => walkStrings(v, visit, key));
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, walkStrings(v, visit, k)]));
  return value;
}

// What to leave out of a project: { people: Map(name → stand-in), notes: [text], names:
// Map(name → stand-in), paths(value) (its paths stood in for), replace(text) }. options: { names
// (leave the samples' and subjects' names out), people (more people's names: the reviewer this
// computer remembers) }.
export function redactionOf(project, { names = false, people = [] } = {}) {
  const persons = new Map();
  const person = (name) => {
    const n = String(name ?? '').trim();
    if (n && !PROGRAM_AUTHORS.has(n) && !persons.has(n)) persons.set(n, `Person ${persons.size + 1}`);
  };
  const notes = new Set();
  const note = (text) => {
    const t = String(text ?? '').trim().replace(/^Relabeled on review: /, '');
    if (t && !OWN_NOTES.has(t)) notes.add(t);
  };
  walkStrings(project, (v, key) => {
    if (key === 'by') person(v);
    return v;
  });
  for (const p of people) person(p);
  for (const r of project.reviews ?? []) note(r.note);
  for (const r of project.regions ?? []) {
    note(r.note);
    for (const v of r.versions ?? []) note(v.note);
  }
  const named = new Map();
  if (names) {
    const samples = samplesOf(project);
    samples.forEach((s, i) => {
      const standIn = `Sample ${i + 1}`;
      for (const n of [s.name, s.given, s.record?.name, s.region?.name, s.region ? null : s.dataset.name]) {
        if (n && !named.has(n)) named.set(n, standIn);
      }
    });
    let sections = 0;
    for (const d of project.datasets) {
      if (d.name && !named.has(d.name)) named.set(d.name, `Section ${(sections += 1)}`);
    }
    let subjects = 0;
    for (const s of samples) {
      const subject = s.fields?.subject;
      if (typeof subject === 'string' && subject.trim() && !named.has(subject.trim())) named.set(subject.trim(), `Subject ${(subjects += 1)}`);
    }
  }
  // One pass over a text: people and names as words, notes wherever they occur.
  const words = new Map([...persons, ...named]);
  const byWord = words.size ? tokenPattern([...words.keys()].sort((a, b) => b.length - a.length)) : null;
  const noteList = [...notes].sort((a, b) => b.length - a.length);
  const replace = (text) => {
    if (OPAQUE.test(text)) return text;
    let out = text;
    for (const n of noteList) if (out.includes(n)) out = out.split(n).join('(a note, left out)');
    return byWord ? out.replace(byWord, (m) => words.get(m)) : out;
  };
  const paths = (value) => redactPaths(value, project);
  return { people: persons, notes: noteList, names: named, paths, replace, counts: { people: persons.size, notes: noteList.length, names: named.size } };
}

// A value (the project, a manifest, a validation report) with what the redaction leaves out
// stood in for: its paths, a note's own field emptied, every text's people, names and notes
// replaced.
export function redact(value, redaction) {
  return walkStrings(redaction.paths(value), (v, key) => {
    if (key === 'note' && redaction.notes.includes(v.trim().replace(/^Relabeled on review: /, ''))) return '';
    return redaction.replace(v);
  });
}

// --- The check ----------------------------------------------------------------------------------

const HOME = /^(\/Users\/[^/]+|\/home\/[^/]+|[A-Za-z]:\\Users\\[^\\]+)/;

// What a package must not hold, to search for: [{ what, text, word (searched as a word) }].
// paths: absolute paths of this computer (its bundles', images', files'); the home folders they lie
// in are searched for too. A name shorter than three characters is not searched for (it would be
// found within other words); its fields are emptied all the same.
export function leftOutStrings(redaction, { paths = [] } = {}) {
  const out = [];
  const seen = new Set();
  const add = (what, text, word = false) => {
    const t = String(text ?? '').trim();
    if (t.length < 3 || seen.has(`${what}\u0000${t}`)) return;
    seen.add(`${what}\u0000${t}`);
    out.push({ what, text: t, word });
  };
  for (const p of paths) {
    if (!p || !(/^(\/|[A-Za-z]:\\|\\\\)/.test(p))) continue;
    add('a path', String(p).replace(/[\\/]+$/, ''));
    const home = HOME.exec(p)?.[1];
    if (home) add('a home folder', home);
  }
  for (const name of redaction.people.keys()) add('a person\'s name', name, true);
  for (const text of redaction.notes) add('a note', text);
  for (const name of redaction.names.keys()) add('a sample\'s or subject\'s name', name, true);
  return out;
}

const htmlEscape = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

// Where any of the strings is found in the texts: [{ file, what }] (each file and kind once).
// texts: [{ name, text }], every text the package holds (its files', and what they carry inside:
// a zip's entries, an image's SVG). A name is searched for as a word, outside base64 (where one
// may occur by chance, as the redaction leaves base64 alone); a path or a note anywhere.
export function findLeftOut(texts, strings) {
  const searches = strings.map((s) => {
    const forms = [...new Set([s.text, htmlEscape(s.text), JSON.stringify(s.text).slice(1, -1)])];
    const pattern = s.word ? tokenPattern(forms) : null;
    const test = pattern
      ? (t) => {
        pattern.lastIndex = 0;
        return pattern.test(t);
      }
      : (t) => forms.some((f) => t.includes(f));
    return { ...s, test };
  });
  const found = [];
  for (const { name, text } of texts) {
    const kinds = new Set();
    const words = text.replace(OPAQUE_RUNS, ' ');
    for (const s of searches) {
      if (!kinds.has(s.what) && s.test(s.word ? words : text)) {
        kinds.add(s.what);
        found.push({ file: name, what: s.what });
      }
    }
  }
  return found;
}
