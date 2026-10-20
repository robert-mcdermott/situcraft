// What the Reports view's records, methods and certificates are of, as their pickers offer it: the
// project, a dataset (and everything made of it) or a result.

import { h } from './dom.js';
import { STEP_KINDS, plural, recordableResults } from '../lib/record.js';

// A scope as the pickers write it, and back: 'project', 'dataset:<ID>', 'result:<ID>'.
export const scopeValue = (scope) => (scope.kind === 'project' ? 'project' : `${scope.kind}:${scope.id}`);
export const scopeOfValue = (value) => (value === 'project' ? { kind: 'project' } : value.startsWith('dataset:') ? { kind: 'dataset', id: value.slice(8) } : { kind: 'result', id: value.replace(/^result:/, '') });

// The options of a picker of scopes: the project, each dataset, each result by kind.
export function scopeOptions(project, current, { none = null } = {}) {
  const groups = new Map();
  for (const r of recordableResults(project)) {
    if (!groups.has(r.kind)) groups.set(r.kind, []);
    groups.get(r.kind).push(r);
  }
  return [
    none ? h('option', { value: '', selected: !current }, none) : null,
    h('option', { value: 'project', selected: current === 'project' }, `The project, ${project.name}`),
    project.datasets.length ? h('optgroup', { label: 'Datasets' }, ...project.datasets.map((d) => h('option', { value: `dataset:${d.id}`, selected: current === `dataset:${d.id}` }, d.name))) : null,
    ...[...groups].map(([kind, list]) => h('optgroup', { label: plural(STEP_KINDS[kind] ?? kind) }, ...list.map((r) => h('option', { value: `result:${r.id}`, selected: current === `result:${r.id}` }, r.name)))),
  ];
}

