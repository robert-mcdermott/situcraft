// Which cells a filter keeps. A filter of conditions on the cells' attributes is applied here
// (lib/filters.js evaluate); one saved from a tree of conditions is applied by the program
// (POST /api/local/{index}/query, queries.go), which evaluates the trees agents and scripts send too.
// Its answers are kept by the filter and what its tree can name (the project's regions, filters and
// cluster names), so that a render can use one already given and ask again when they change.

import { evaluate, isTreeFilter, keepChecksum, keepFromBytes } from '../lib/filters.js';

const answers = new Map(); // key → Promise of { keep, kept, sha256, cells, text }
const given = new Map(); // key → the answer, once given
const failed = new Map(); // key → why it could not be given (asked at once, not again)

// What a tree names beyond the dataset: the project's regions, saved filters and cluster names, as
// the page holds them (unsaved changes counting).
export function treeContext(project) {
  return {
    regions: project.regions ?? [],
    filters: project.artifacts.filter((a) => a.kind === 'filter'),
    clusterNames: project.artifacts.filter((a) => a.kind === 'cluster-names'),
  };
}

function keyOf(url, project, filter) {
  const regions = (project.regions ?? []).map((r) => `${r.id}:${r.versions.length}`).join(',');
  const named = project.artifacts.filter((a) => a.kind === 'filter' || a.kind === 'cluster-names').map((a) => a.id).join(',');
  return `${url}\u0000${filter.id}\u0000${regions}\u0000${named}`;
}

// The program's answer for a tree of a dataset's instrument cells: { keep, kept, sha256, cells, text }.
export async function askTree(url, project, dataset, tree) {
  const response = await fetch(`${url}/query`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ dataset, where: tree, context: treeContext(project) }) });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`);
  const keep = keepFromBytes(Uint8Array.from(atob(body.keep), (c) => c.charCodeAt(0)));
  return { keep, kept: body.kept, sha256: body.sha256, cells: body.cells, text: body.text };
}

// A filter's cells as an answer gives them, and whether they are those it recorded.
function result(filter, { keep, kept, sha256, cells }) {
  return { keep, kept, sha256, same: cells === filter.result.cells && kept === filter.result.kept && sha256 === filter.result.sha256 };
}

// A filter of conditions applied here.
function applied(filter, cells) {
  const { keep, kept } = evaluate(filter.conditions, cells);
  return result(filter, { keep, kept, sha256: keepChecksum(keep), cells: cells.n });
}

// A filter applied again to the dataset's cells (url: the dataset's, cells: its cells table for a
// filter of conditions): { keep, kept, sha256, same }, same when it keeps the cells it recorded.
export async function filterKeep(url, project, filter, cells) {
  if (!isTreeFilter(filter)) return applied(filter, cells);
  const key = keyOf(url, project, filter);
  if (!answers.has(key)) {
    const p = askTree(url, project, filter.dataset, filter.tree).then((answer) => {
      given.set(key, answer);
      return answer;
    });
    p.catch(() => answers.delete(key));
    answers.set(key, p);
  }
  return result(filter, await answers.get(key));
}

// The same at once: the answer when there is one (a filter of conditions always has), { error } when
// the program could not give it, else null, asking for it and calling then() when it comes.
export function filterKeepNow(url, project, filter, cells, then) {
  if (!isTreeFilter(filter)) return applied(filter, cells);
  const key = keyOf(url, project, filter);
  if (given.has(key)) return result(filter, given.get(key));
  if (failed.has(key)) return { error: failed.get(key) };
  if (!answers.has(key)) {
    filterKeep(url, project, filter, cells).then(() => then?.(), (err) => {
      failed.set(key, err);
      then?.();
    });
  }
  return null;
}
