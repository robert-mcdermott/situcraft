import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ATTRIBUTES, attributeValues } from './cell-attributes.js';
import { addFilter, compareKeeps, createFilter, describeCondition, effect, evaluate, keepChecksum, removeFilter, verifyFilter, versions } from './filters.js';
import { bin2d, binOf, boxStats, countMarked, finiteSorted, histogram, histogramBins, quantile, selectRange, selectRect } from './plots.js';
import { sha256 } from './sha256.js';
import { createProject } from './project.js';

// A small cells table, as lib/viewer-data.js readCells gives it.
function table() {
  const n = 6;
  const f = (v) => Float32Array.from(v);
  const u = (v) => Uint32Array.from(v);
  return {
    n, methods: 2,
    x: f([0, 1, 2, 3, 4, 5]), y: f([0, 0, 0, 1, 1, 1]),
    area: f([50, 100, NaN, 80, 20, 60]), nucleusArea: f([25, 30, 10, 0, 10, 30]),
    transcripts: u([10, 0, 5, 40, 3, 25]), total: u([11, 0, 5, 42, 3, 26]),
    controlProbes: u([1, 0, 0, 1, 0, 0]), controlCodewords: u([0, 0, 0, 1, 0, 1]),
    genes: u([8, 0, 4, 20, 3, 15]), nuclear: u([5, 0, 5, 10, 0, 20]),
    method: Uint8Array.from([0, 1, 0, 255, 1, 0]),
  };
}

test('cell attributes come from the cells table, NaN where a cell has none', () => {
  const cells = table();
  assert.deepEqual([...attributeValues(cells, 'controls')], [1, 0, 0, 2, 0, 1]);
  assert.deepEqual([...attributeValues(cells, 'density')].map((v) => (Number.isNaN(v) ? 'NaN' : v)), [20, 0, 'NaN', 50, 15, (100 * 25) / 60]);
  const share = attributeValues(cells, 'nuclearShare');
  assert.equal(share[0], 0.5);
  assert.ok(Number.isNaN(share[1]));
  assert.ok(Number.isNaN(attributeValues(cells, 'method')[3]));
  assert.equal(attributeValues(cells, 'genes'), attributeValues(cells, 'genes'), 'cached');
  for (const a of ATTRIBUTES) assert.equal(attributeValues(cells, a.id).length, 6, a.id);
  assert.throws(() => attributeValues(cells, 'height'));
});

test('filters keep the cells passing every condition, bounds included', () => {
  const cells = table();
  const conditions = [{ attribute: 'transcripts', min: 5, max: null }, { attribute: 'area', min: null, max: 90 }, { attribute: 'method', values: [0] }];
  const r = evaluate(conditions, cells);
  // Cell 0 passes all; 1 fails all three; 2 has no area (it fails that alone); 3 has no method
  // (alone); 4 fails transcripts and method; 5 passes.
  assert.deepEqual([...r.keep], [1, 0, 0, 0, 0, 1]);
  assert.equal(r.kept, 2);
  assert.deepEqual(r.failing, [2, 2, 3]);
  assert.deepEqual(r.only, [0, 1, 1]);
  assert.equal(evaluate([], cells).kept, 6);
  assert.equal(describeCondition(conditions[0]), 'Gene transcripts ≥ 5');
  assert.equal(describeCondition({ attribute: 'area', min: 10, max: 90 }), 'Cell area from 10 to 90 µm²');
  assert.equal(describeCondition(conditions[2], ['by boundary stain']), 'Segmentation method is by boundary stain');
});

test('a filter records its result and reproduces it exactly', () => {
  const cells = table();
  const keep = Uint8Array.from([1, 0, 0, 0, 0, 1]);
  // Six cells (06 00 00 00), cells 0 and 5 kept (bits 0 and 5: 0x21).
  assert.equal(keepChecksum(keep), sha256(Uint8Array.from([6, 0, 0, 0, 0x21])));
  const first = createFilter({ dataset: 'd1', name: 'Enough transcripts', conditions: [{ attribute: 'transcripts', min: 5, max: null }], cells, now: '2026-10-14T00:00:00Z', id: 'f-a' });
  assert.deepEqual(first.result, { cells: 6, kept: 4, sha256: keepChecksum(Uint8Array.from([1, 0, 1, 1, 0, 1])) });
  assert.equal(first.version, 1);
  assert.ok(verifyFilter(first, cells).same);
  // The same definition on other cells is not the same result.
  const other = { ...table(), transcripts: Uint32Array.from([10, 0, 5, 40, 3, 2]) };
  assert.equal(verifyFilter(first, other).same, false);
  const second = createFilter({ dataset: 'd1', name: 'Enough transcripts', conditions: [{ attribute: 'transcripts', min: 10, max: null }], cells, previous: first, id: 'f-b' });
  assert.equal(second.version, 2);
  assert.equal(second.parent, 'f-a');
  let project = createProject('p');
  project = addFilter(addFilter(project, first), second);
  assert.deepEqual(versions(project, 'd1', 'Enough transcripts').map((f) => f.version), [1, 2]);
  assert.equal(project.history.length, 2);
  assert.equal(removeFilter(project, 'f-a').artifacts.length, 1);
  assert.deepEqual(compareKeeps(verifyFilter(first, cells).keep, verifyFilter(second, cells).keep), { both: 3, onlyA: 1, onlyB: 0, neither: 2 });
  const e = effect(verifyFilter(second, cells).keep, attributeValues(cells, 'transcripts'));
  assert.equal(e.keptSum, 75);
  assert.equal(e.allSum, 83);
  assert.equal(e.keptMedian, 25);
});

test('histograms, quantiles and box statistics', () => {
  assert.equal(quantile([1, 2, 3, 4], 0.5), 2.5);
  assert.equal(quantile([1, 2, 3, 4], 1), 4);
  const sorted = finiteSorted([5, NaN, 1, 3, Infinity]);
  assert.deepEqual([...sorted], [1, 3, 5]);
  const ints = histogramBins(0, 4, { integer: true });
  assert.deepEqual([...ints.edges], [-0.5, 0.5, 1.5, 2.5, 3.5, 4.5]);
  const even = histogramBins(0, 10, { bins: 5 });
  assert.deepEqual([...even.edges], [0, 2, 4, 6, 8, 10]);
  assert.equal(binOf(even.edges, 10), 4);
  assert.equal(binOf(even.edges, 2), 1);
  assert.equal(binOf(even.edges, -1), -1);
  assert.equal(binOf(even.edges, NaN), -1);
  const logBins = histogramBins(1, 1000, { bins: 3, log: true });
  assert.ok(Math.abs(logBins.edges[1] - 10) < 1e-9 && Math.abs(logBins.edges[3] - 1000) < 1e-9);
  const h = histogram([0, 1, 1, 9, 10, NaN], even.edges, Uint8Array.from([1, 0, 1, 0, 1, 1]));
  assert.deepEqual([...h.counts], [3, 0, 0, 0, 2]);
  assert.deepEqual([...h.marked], [2, 0, 0, 0, 1]);
  const box = boxStats(Float64Array.from([1, 2, 3, 4, 5, 6, 7, 8, 100]));
  assert.equal(box.median, 5);
  assert.equal(box.q1, 3);
  assert.equal(box.q3, 7);
  assert.equal(box.whiskerHi, 8);
  assert.equal(box.max, 100);
  assert.equal(boxStats([]).n, 0);
});

test('brushing selects exactly the cells in range', () => {
  const values = [1, 5, NaN, 10, 5.0001];
  assert.deepEqual([...selectRange(values, 5, 10)], [0, 1, 0, 1, 1]);
  assert.deepEqual([...selectRange(values, 5, 10, Uint8Array.from([1, 1, 1, 0, 1]))], [0, 1, 0, 0, 1]);
  assert.deepEqual([...selectRect([0, 1, 2], [0, 1, 2], 2, 0.5, 0.5, 3)], [0, 1, 1]);
  assert.equal(countMarked(Uint8Array.from([1, 0, 1])), 2);
  const grid = bin2d([0, 0.5, 1, 1], [0, 0.5, 1, 1], 2, 2, 0, 1, 0, 1);
  assert.deepEqual([...grid], [1, 0, 0, 3]);
});

test('run metrics are listed with their explanation and SituCraft\'s value', async () => {
  const { METRICS, formatMetric, metricRows } = await import('./metrics.js');
  const recorded = [
    { name: 'run_name', text: 'run' },
    { name: 'fraction_transcripts_assigned', text: '0.98', value: 0.98 },
    { name: 'thickness_transcripts_high_quality', text: '7.6', value: 7.6 },
    { name: 'fraction_of_nuclei_without_cell', text: '' },
    { name: 'some_new_metric', text: '3', value: 3 },
  ];
  const checks = [
    { name: 'fraction_transcripts_assigned', source: 'metrics_summary.csv', recorded: 0.98, computed: 0.9800000001, agrees: true },
    { name: 'num_cells', source: 'experiment.xenium', recorded: 5, computed: 5, agrees: true },
  ];
  const rows = metricRows(recorded, checks);
  assert.deepEqual(rows.map((r) => r.name), ['fraction_transcripts_assigned', 'thickness_transcripts_high_quality', 'some_new_metric']);
  assert.equal(rows[0].agrees, true);
  assert.equal(rows[0].computed, 0.9800000001);
  assert.match(rows[0].how, /assigned to a cell/);
  assert.equal(rows[1].agrees, null);
  assert.match(rows[1].why, /not in the bundle/);
  assert.equal(rows[2].label, 'some new metric');
  assert.equal(rows[2].why, 'SituCraft does not recompute it.');
  assert.equal(formatMetric(0.9060656, 'share'), '90.61%');
  assert.equal(formatMetric(1035543, 'count'), '1,035,543');
  assert.equal(formatMetric(0.000372551, 'number'), '3.726e-4');
  assert.equal(formatMetric(NaN, 'number'), '—');
  // Every explained metric belongs to a group and says how it is computed or why not.
  for (const [name, m] of Object.entries(METRICS)) assert.ok(m.group && m.label && m.help && (m.how || m.why), name);
});
