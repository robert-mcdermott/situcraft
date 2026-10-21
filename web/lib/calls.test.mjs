import { test } from 'node:test';
import assert from 'node:assert/strict';
import { callDuration, callIDs, callsQuery, callTime, projectChanges } from './calls.js';
import { createProject } from './project.js';
import { addFilter, createTreeFilter, removeFilter } from './filters.js';
import { addRegion, editRegion } from './regions.js';

// What a call changed: the records added, changed (a region's new version) and removed, and a
// decision; the records it left alone, though the project is a new object, not; another project
// opened, nothing.
test('what a call changed in the project', () => {
  let p = createProject('Calls', { id: 'p-1', now: '2026-10-20T10:00:00Z' });
  p = { ...p, datasets: [{ id: 'd-1', name: 'Lung' }] };
  const filter = createTreeFilter({ dataset: 'd-1', name: 'Nest', tree: { region: 'Nest' }, text: 'in Nest', result: { cells: 10, kept: 4, sha256: 'ab' }, id: 'f-1' });
  const ring = [[[0, 0], [10, 0], [10, 10], [0, 10], [0, 0]]];
  const before = addRegion(addFilter(p, filter), { dataset: 'd-1', name: 'Nest', cls: 'Tumor', geometry: [ring], tool: 'polygon', id: 'r-1' });
  assert.deepEqual(projectChanges(before, { ...before }), []);
  let after = editRegion(before, 'r-1', { name: 'Tumor nest' });
  after = removeFilter(after, 'f-1');
  after = addFilter(after, createTreeFilter({ dataset: 'd-1', name: 'Edge', tree: { distance: 'Tumor nest', max: 20 }, text: 'near', result: { cells: 10, kept: 2, sha256: 'cd' }, id: 'f-2' }));
  after = { ...after, decisions: [...after.decisions, { kind: 'promote-segmentation', dataset: 'd-1', artifact: 'seg-1' }] };
  assert.deepEqual(projectChanges(before, after), [
    { id: 'f-2', kind: 'filter', name: 'Edge', change: 'added' },
    { id: 'f-1', kind: 'filter', name: 'Nest', change: 'removed' },
    { id: 'r-1', kind: 'region', name: 'Tumor nest', change: 'changed' },
    { id: 'seg-1', kind: 'decision', name: 'promote-segmentation', change: 'added' },
  ]);
  // The same content in a new object (a project read again) is no change.
  assert.deepEqual(projectChanges(before, JSON.parse(JSON.stringify(before))), []);
  assert.deepEqual(projectChanges(before, { ...after, id: 'p-2' }), []);
  assert.deepEqual(projectChanges(null, after), []);
});

test('how the Calls tab says a call', () => {
  const now = new Date('2026-10-20T15:00:00');
  assert.equal(callTime(new Date('2026-10-20T14:03:07').toISOString(), now), '14:03:07');
  assert.equal(callTime(new Date('2026-10-18T09:05:00').toISOString(), now), '10-18 09:05');
  assert.equal(callDuration(42), '42 ms');
  assert.equal(callDuration(4200), '4.2 s');
  assert.equal(callDuration(125000), '2 min 5 s');
  assert.deepEqual(callIDs({ changed: [{ id: 'f-2' }, { id: '' }], ids: ['lab-1a2b3c', 'f-2'] }), ['f-2', 'lab-1a2b3c']);
  assert.equal(callsQuery({ q: ' tumor nest ', outcome: 'failed' }, { limit: 200 }), 'q=tumor+nest&outcome=failed&limit=200');
  assert.equal(callsQuery({}), '');
});
