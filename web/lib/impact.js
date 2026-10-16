// Labels across segmentations (internal/compare's impact.go, api_impact.go): which label sets of two
// segmentations to compare, what making a clustering and its labels again on another segmentation
// takes, and the impact in words. Pure functions over the project, for the page and Node's tests.

// Bytes of a typed array in base64, as the program reads per-cell arrays.
export function base64Of(typed) {
  const bytes = new Uint8Array(typed.buffer, typed.byteOffset, typed.byteLength);
  let s = '';
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(s);
}

// Per-cell flags from base64 of little-endian int32.
export function int32sOf(text) {
  const raw = atob(text);
  const bytes = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i += 1) bytes[i] = raw.charCodeAt(i);
  return new Int32Array(bytes.buffer, 0, bytes.length >> 2);
}

// A label set (ui/label-sources.js's labelSource) as one side of the impact request.
export function sideOf(src) {
  const covered = new Uint8Array(src.segCells);
  for (let i = 0; i < src.segCells; i += 1) covered[i] = src.analyzed[i] ? 1 : 0;
  return { labels: base64Of(Int32Array.from(src.label)), covered: base64Of(covered), names: src.names };
}

// What a label set was made from, for pairing it with one made the same way on another
// segmentation: a marker set's family and the parameters, or a reference's checksum and the
// parameters; null for imported, curated and consensus sets.
export function originOf(record, level = 0) {
  if (!record || record.kind !== 'labels' || record.source) return null;
  const params = { ...(record.parameters ?? {}) };
  delete params.markers;
  if (record.reference) return `reference ${record.reference.sha256} ${record.reference.labelColumn ?? ''} ${record.reference.geneColumn ?? ''} ${JSON.stringify(params)}`;
  return `markers ${record.markers?.name ?? ''} ${record.markers?.checksum ?? ''} ${JSON.stringify(params)} ${level}`;
}

// The label sets to compare by default, of the versions of A's segmentation and of B's (each
// { id, level, kind, name }, ui/label-sources.js's labelVersions): two made the same way (the same
// marker set and parameters, or the same reference), the latest such pair; else the latest of
// each; each at its first level (the coarsest: the finer are a choice away). Null for a side
// without a version.
export function defaultPair(project, versionsA, versionsB) {
  const record = (v) => project.artifacts.find((a) => a.id === v.id) ?? null;
  const first = (list, v) => (v ? list.find((x) => x.id === v.id && x.level === 0) ?? v : null);
  for (let i = versionsB.length - 1; i >= 0; i -= 1) {
    const o = originOf(record(versionsB[i]), versionsB[i].level);
    if (!o) continue;
    for (let j = versionsA.length - 1; j >= 0; j -= 1) {
      if (originOf(record(versionsA[j]), versionsA[j].level) === o) return [first(versionsA, versionsA[j]), first(versionsB, versionsB[i])];
    }
  }
  return [first(versionsA, versionsA.at(-1)), first(versionsB, versionsB.at(-1))];
}

// What making a clustering and its label sets again on another segmentation takes: the
// clustering's parameters (a QC filter's cells are the instrument segmentation's, so the cells
// are chosen by the rule instead), and each label set from markers or a reference made from it,
// with how to make it; the sets that cannot be made again, with why. problem: why nothing can be.
export function remakePlan(project, clusteringId, segmentationId) {
  const c = project.artifacts.find((a) => a.kind === 'clustering' && a.id === clusteringId);
  const seg = project.artifacts.find((a) => a.kind === 'segmentation' && a.id === segmentationId);
  if (!c) return { problem: 'No such clustering in the project.' };
  if (!seg || seg.dataset !== c.dataset) return { problem: 'No such segmentation of the clustering\'s dataset.' };
  if (seg.id === c.segmentation) return { problem: `The clustering is already of ${seg.name.toLowerCase()}.` };
  if (seg.state !== 'complete') return { problem: `${seg.name} is not complete.` };
  if (c.summary?.source) return { problem: `${c.name} is ${c.summary.source.tool ?? 'another tool'}'s clustering: make one of SituCraft's first, and its labels.` };
  const parameters = { ...(c.parameters ?? {}) };
  const notes = [];
  if (c.summary?.filter) notes.push(`The cells were those the QC filter ${c.summary.filter.name} kept of the instrument's segmentation; on ${seg.name.toLowerCase()} they are those with at least ${parameters.minTranscripts ?? 10} gene transcripts.`);
  const labels = [];
  const skipped = [];
  for (const r of project.artifacts.filter((a) => a.kind === 'labels' && a.state === 'complete' && a.clustering === clusteringId)) {
    const params = { ...(r.parameters ?? {}) };
    delete params.markers;
    if (r.source) skipped.push({ id: r.id, why: 'imported for the cells of its segmentation' });
    else if (r.reference) labels.push({ kind: 'reference', record: r, parameters: params });
    else {
      const ms = project.artifacts.find((a) => a.kind === 'marker-set' && a.id === r.markerSet);
      if (!ms) skipped.push({ id: r.id, why: 'its marker set is no longer in the project' });
      else labels.push({ kind: 'markers', record: r, set: ms.set, markerSet: ms.id, parameters: params });
    }
  }
  return { clustering: c, segmentation: seg, parameters, labels, skipped, notes };
}

const pct = (v, digits = 1) => (Number.isFinite(v) ? `${(100 * v).toFixed(digits)}%` : '—');

// The impact in a few sentences: matched cells labeled alike, split and merged cells' fates, the
// marker mixing of all labeled cells.
export function impactLines(im, nameA = 'A', nameB = 'B') {
  const lines = [];
  const m = im.matched;
  if (m.cells) lines.push(`Of the ${m.cells.toLocaleString('en-US')} cells matched one to one, ${m.labeled.toLocaleString('en-US')} are labeled under both: ${pct(m.agreement)} alike${m.labeledOne ? `; ${m.labeledOne.toLocaleString('en-US')} are labeled under one only` : ''}.`);
  const s = im.split;
  if (s.groups) lines.push(`${s.groups.toLocaleString('en-US')} labeled cells of ${nameA} are split under ${nameB}: every piece keeps the cell's label in ${pct(s.kept / s.groups, 0)}, some pieces in ${pct(s.partly / s.groups, 0)}, none in ${pct(s.changed / s.groups, 0)}; ${pct(s.mixed / s.groups, 0)} split into cells of two labels or more.`);
  const g = im.merged;
  if (g.groups) lines.push(`${g.groups.toLocaleString('en-US')} labeled cells of ${nameB} merge cells of ${nameA}: every cell merged carried its label in ${pct(g.kept / g.groups, 0)}, some in ${pct(g.partly / g.groups, 0)}, none in ${pct(g.changed / g.groups, 0)}; ${pct(g.mixed / g.groups, 0)} join cells of two labels or more.`);
  const x = im.mixing;
  const f4 = (v) => (Number.isFinite(v) ? v.toFixed(4) : '—');
  if (x) {
    // The label whose mixing changes most, of those under both.
    let most = null;
    for (const a of x.a) {
      const b = x.b.find((y) => y.name === a.name);
      if (b && Number.isFinite(a.mecr) && Number.isFinite(b.mecr) && (!most || Math.abs(b.mecr - a.mecr) > Math.abs(most.b - most.a))) most = { name: a.name, a: a.mecr, b: b.mecr };
    }
    lines.push(`Marker mixing (the mutually exclusive co-expression rate) of the labeled cells: ${f4(x.allA.mecr)} under ${nameA}, ${f4(x.allB.mecr)} under ${nameB}${most ? `; it changes most among ${most.name} cells (${f4(most.a)} → ${f4(most.b)})` : ''}.`);
  }
  return lines;
}

// Each name's color for both sides: A's colors for A's names, B's for B's others.
export function sharedColors(a, b) {
  const color = new Map();
  a.names.forEach((n, k) => color.set(n, a.colors[k]));
  b.names.forEach((n, k) => {
    if (!color.has(n)) color.set(n, b.colors[k]);
  });
  return color;
}
