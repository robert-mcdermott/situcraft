// The calls programs make of SituCraft, as the library's log keeps them (calls.go): what a call
// changed in the project, for the log, and how the drawer's Calls tab says a call.

// The project's records by collection: its key, the kind a record of it is, and its name.
const COLLECTIONS = [
  ['datasets', () => 'dataset', (x) => x.name],
  ['artifacts', (x) => x.kind, (x) => x.name ?? x.title],
  ['regions', () => 'region', (x) => x.versions?.at(-1)?.name],
  ['views', () => 'view', (x) => x.name],
  ['figures', () => 'figure', (x) => x.name],
  ['reviews', () => 'review', (x) => x.name ?? x.target],
  ['samples', () => 'sample', (x) => x.name],
  ['exports', () => 'export', (x) => x.name ?? x.path],
];

// What a call changed in the project: the records it added, changed or removed, each { id, kind,
// name, change }, and the decisions it added. A record the store left alone is the same object, so
// records are compared by identity first, then by content. Another project opened is not a change
// of one.
export function projectChanges(before, after) {
  if (!before || !after || before.id !== after.id) return [];
  const out = [];
  for (const [key, kindOf, nameOf] of COLLECTIONS) {
    const was = new Map((before[key] ?? []).filter((x) => x?.id).map((x) => [x.id, x]));
    const now = new Map((after[key] ?? []).filter((x) => x?.id).map((x) => [x.id, x]));
    const say = (x, change) => out.push({ id: x.id, kind: kindOf(x) ?? key, name: nameOf(x) ?? '', change });
    for (const [id, x] of now) {
      const old = was.get(id);
      if (!old) say(x, 'added');
      else if (old !== x && JSON.stringify(old) !== JSON.stringify(x)) say(x, 'changed');
    }
    for (const [id, x] of was) if (!now.has(id)) say(x, 'removed');
  }
  // Decisions are appended, and have no ids: each new one by what it decided about.
  for (const d of (after.decisions ?? []).slice((before.decisions ?? []).length)) {
    out.push({ id: d.artifact ?? d.dataset ?? '', kind: 'decision', name: d.kind ?? '', change: 'added' });
  }
  return out;
}

// A call's when, as the tab shows it: today's by the time, others' by the day and minute.
export function callTime(iso, now = new Date()) {
  const t = new Date(iso);
  const two = (n) => String(n).padStart(2, '0');
  if (t.toDateString() === now.toDateString()) return `${two(t.getHours())}:${two(t.getMinutes())}:${two(t.getSeconds())}`;
  return `${two(t.getMonth() + 1)}-${two(t.getDate())} ${two(t.getHours())}:${two(t.getMinutes())}`;
}

// How long a call took, in words.
export function callDuration(ms) {
  if (ms < 1000) return `${ms} ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)} s`;
  return `${Math.floor(ms / 60000)} min ${Math.round((ms % 60000) / 1000)} s`;
}

// The ids a call names: those it changed, then those it answered with, each once.
export function callIDs(call) {
  return [...new Set([...(call.changed ?? []).map((c) => c.id).filter(Boolean), ...(call.ids ?? [])])];
}

// The query string of the tab's search: words, a tool, a caller and an outcome.
export function callsQuery({ q = '', tool = '', by = '', outcome = '' } = {}, extra = {}) {
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries({ q: q.trim(), tool, by, outcome, ...extra })) if (v !== '' && v !== null && v !== undefined) params.set(k, String(v));
  return params.toString();
}
