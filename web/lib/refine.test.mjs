import { test } from 'node:test';
import assert from 'node:assert/strict';
import { REFINE_METHOD, STUDIO_STEPS, areaText, bytes, memory, cellFlags, convergence, duration, failureOf, flagsOf, isRefinement, limitations, load, parseRegion, presetSpec, readRecipe, regionText, resolvePreset, settingProblem, settingValue, withSetting } from './refine.js';

const summary = { sweeps: 600, samples: 500, splitHalfAgreement: 0.982, meanProbability: 0.913, lowProbabilityShare: 0.041, logLikelihoodDriftPerTranscript: 0.0001, voxelsMovedPerSweep: 6627.4, cellsLowTranscripts: 12, cellsNoNucleus: 0, cellsUncertainBoundaries: 31, cellsLeftOut: 2 };

test('convergence: settled by Geweke\'s diagnostic, stability described, drift said plainly', () => {
  const settled = convergence({ ...summary, gewekeZ: -1.3 });
  assert.equal(settled[0].ok, true);
  assert.match(settled[0].text, /^Settled: the log-likelihood's mean is the same early and late in the last 500 sweeps \(Geweke's z = -1\.3\)\.$/);
  assert.match(settled[1].text, /^Stability: 98\.2% of transcripts are in the same cell over both halves/);
  assert.match(settled[2].text, /mean posterior probability of 0\.913; 4\.1% of transcripts/);
  assert.match(settled[3].text, /^6,627 voxels still move each sweep/);
  const drifting = convergence({ ...summary, gewekeZ: 9.5, logLikelihoodDriftPerTranscript: 0.0123 });
  assert.equal(drifting[0].ok, false);
  assert.match(drifting[0].text, /^Still drifting slowly: the log-likelihood changed by 0\.0123 per transcript .* \(Geweke's z = 9\.5\)\. .*run more sweeps to be sure\.$/);
  assert.deepEqual(convergence(null), []);
  // Too few sweeps summarized to judge: said so, whatever z says.
  const short = convergence({ ...summary, samples: 40, gewekeZ: 0, gewekeJudged: false });
  assert.equal(short[0].ok, false);
  assert.match(short[0].text, /^Not judged: only 40 sweeps were summarized, and Geweke's diagnostic of convergence needs 100 or more\./);
  assert.equal(convergence({ ...summary, samples: 40, gewekeZ: 0 })[0].ok, false); // recorded before gewekeJudged
  assert.equal(convergence({ ...summary, gewekeZ: 0, gewekeJudged: true })[0].ok, true);
});

test('limitations name each flagged group with its threshold, and nothing for none', () => {
  const l = limitations(summary);
  assert.deepEqual(l.map((x) => [x.key, x.n]), [['lowTranscripts', 12], ['uncertainBoundaries', 31], ['leftOut', 2]]);
  assert.match(l[0].text, /fewer than 10 gene transcripts/);
  assert.match(l[1].text, /fewer than 75% of the sweeps/);
  assert.deepEqual(limitations({ ...summary, cellsLowTranscripts: 0, cellsUncertainBoundaries: 0, cellsLeftOut: 0 }), []);
});

test('each cell\'s flags, from the diagnostics', () => {
  const flags = cellFlags({ limitations: { lowTranscripts: ['c2', 'c9'], uncertainBoundaries: ['c2'], noNucleus: [], leftOut: ['x-1'] } });
  assert.deepEqual(flags.get('c2'), ['few transcripts', 'uncertain boundary']);
  assert.equal(flags.has('x-1'), false); // a cell left out is in no segmentation's cells
  assert.equal(flagsOf(flags, ['c1', 'c2', 'c9']), 'c2: few transcripts, uncertain boundary; c9: few transcripts');
  assert.equal(flagsOf(flags, ['c1']), '');
  assert.equal(cellFlags(null).size, 0);
});

test('a refinement is known by its method', () => {
  assert.equal(isRefinement({ method: { name: REFINE_METHOD } }), true);
  assert.equal(isRefinement({ method: { name: 'SituCraft nucleus expansion' } }), false);
});

test('convergence adds a second seed\'s agreement, and what was refined and held', () => {
  const pilot = convergence({ ...summary, gewekeZ: 1, seedAgreement: 0.934, regions: 1, cellsRefined: 57, cellsHeld: 112 });
  assert.match(pilot.at(-2).text, /^A second seed put 93\.4% of the transcripts in refined cells in the same cell\.$/);
  assert.equal(pilot.at(-2).ok, true);
  assert.match(pilot.at(-1).text, /^57 cells refined in one region, 112 about them held as the instrument drew them\.$/);
  assert.match(convergence({ ...summary, gewekeZ: 1, regions: 1, cellsRefined: 1, cellsHeld: 7 }).at(-1).text, /^1 cell refined in one region, 7 about it held/);
  const chance = convergence({ ...summary, gewekeZ: 1, seedAgreement: 0.7 });
  assert.equal(chance.at(-1).ok, false);
  assert.match(chance.at(-1).text, /much of the result is chance here/);
  assert.equal(convergence({ ...summary, gewekeZ: 1 }).length, 4);
});

// The presets as the program serves them (GET /api/refinement/presets), cut down.
const options = (perimeter, voxel = 1) => ({ model: { components: 10, nuclear_prior: 0.8 }, sampler: { perimeter_factor: perimeter, bubble: 0.05, reposition: true }, stages: [{ voxel_um: voxel, layers: 1, sweeps: 600 }], block_um: 40, samples: 500, seed: 20250 });
const presets = [
  { id: 'balanced', options: options(1.6) },
  { id: 'conservative', options: options(1.3) },
  { id: 'low-memory', options: options(1.6, 2), gcPercent: 20 },
  { id: 'roi-pilot', options: options(1.6), area: 'regions', seedCheck: true },
  { id: 'expert', options: options(1.6), expert: true },
];

test('the studio\'s steps are the requirement\'s flow', () => {
  assert.deepEqual(STUDIO_STEPS.map((s) => s.label), ['Input', 'Compute', 'Model', 'Review', 'Run', 'Compare']);
});

test('a preset\'s spec keeps the chosen seed, area and quality; the pilot asks for regions', () => {
  const base = presetSpec(presets, 'balanced');
  assert.deepEqual(base, { preset: 'balanced', options: options(1.6), area: { mode: 'whole' }, min_qv: 20, seed_check: false });
  const mine = { ...base, options: { ...base.options, seed: 7 }, area: { mode: 'sections' }, min_qv: 25 };
  const low = presetSpec(presets, 'low-memory', mine);
  assert.equal(low.options.seed, 7);
  assert.equal(low.options.stages[0].voxel_um, 2);
  assert.equal(low.gc_percent, 20);
  assert.deepEqual([low.area, low.min_qv], [{ mode: 'sections' }, 25]);
  const pilot = presetSpec(presets, 'roi-pilot', mine);
  assert.deepEqual([pilot.area, pilot.seed_check], [{ mode: 'regions', regions: [] }, true]);
  assert.throws(() => presetSpec(presets, 'nonesuch'), /No preset nonesuch/);
});

test('changing a setting but the seed makes the settings Expert, as the program resolves them', () => {
  const base = presetSpec(presets, 'conservative');
  assert.equal(resolvePreset(withSetting(base, 'seed', 99, presets), presets), 'conservative');
  assert.equal(withSetting(base, 'seed', 99, presets).preset, 'conservative');
  assert.equal(withSetting(base, 'min_qv', 30, presets).preset, 'conservative');
  const changed = withSetting(base, 'sampler.perimeter_factor', 2, presets);
  assert.equal(changed.preset, 'expert');
  assert.equal(base.options.sampler.perimeter_factor, 1.3); // the spec given is left as it was
  assert.equal(withSetting(changed, 'sampler.perimeter_factor', 1.3, presets).preset, 'expert'); // Expert stays Expert
  // Sweeps on the last stage, the samples kept within them.
  const short = withSetting(presetSpec(presets, 'balanced'), 'stages.sweeps', 120, presets);
  assert.deepEqual([settingValue(short, 'stages.sweeps'), settingValue(short, 'samples'), short.preset], [120, 100, 'expert']);
  assert.equal(settingValue(base, 'gc_percent'), 50);
  assert.equal(settingValue(presetSpec(presets, 'low-memory'), 'gc_percent'), 20);
  assert.equal(withSetting(presetSpec(presets, 'roi-pilot'), 'seed_check', false, presets).preset, 'expert');
});

test('a setting\'s bounds, in words', () => {
  const sweeps = { key: 'stages.sweeps', name: 'Sweeps', min: 10, max: 100000 };
  assert.equal(settingProblem(sweeps, 600), '');
  assert.equal(settingProblem(sweeps, 5), 'Sweeps is from 10 to 100,000.');
  assert.equal(settingProblem(sweeps, 60.5), 'Sweeps is a whole number.');
  assert.equal(settingProblem({ key: 'stages.voxel_um', name: 'Voxel size', unit: 'µm', min: 0.25, max: 16 }, 32), 'Voxel size is from 0.25 to 16 µm.');
  assert.equal(settingProblem({ key: 'x', name: 'X', min: 0, max: 1 }, Number.NaN), 'X is a number.');
  assert.equal(settingProblem({ key: 'sampler.reposition', name: 'Drift' }, true), '');
});

test('regions typed, and areas said', () => {
  assert.deepEqual(parseRegion('10, 20, 110.5, 220'), { x0_um: 10, y0_um: 20, x1_um: 110.5, y1_um: 220 });
  assert.deepEqual(parseRegion('10 20 110 220'), { x0_um: 10, y0_um: 20, x1_um: 110, y1_um: 220 });
  assert.throws(() => parseRegion('10, 20, 30'), /four numbers/);
  assert.throws(() => parseRegion('100, 20, 30, 40'), /right of and below/);
  assert.equal(regionText({ x0_um: 10.2, y0_um: 20, x1_um: 110.5, y1_um: 220 }), '(10, 20)–(111, 220) µm');
  assert.equal(areaText({ mode: 'whole' }), 'the whole tissue');
  assert.match(areaText({ mode: 'sections' }), /more than 100 µm apart/);
  assert.equal(areaText({ mode: 'regions', regions: [] }), 'a region, not chosen yet');
  assert.equal(areaText({ mode: 'regions', regions: [parseRegion('0,0,10,10'), parseRegion('20,20,30,30')] }), '2 regions: (0, 0)–(10, 10) µm; (20, 20)–(30, 30) µm');
});

test('a recipe is read, and others refused with why', () => {
  const recipe = { format: 'situcraft-recipe', version: 1, kind: 'refinement', dataset: { name: 'Lung' }, refinement: { preset: 'low-memory', options: options(1.6, 2), area: { mode: 'whole' }, min_qv: 20, gc_percent: 20 } };
  const r = readRecipe(JSON.stringify(recipe));
  assert.equal(r.spec.preset, 'low-memory');
  assert.equal(r.spec.gc_percent, 20);
  assert.equal(r.dataset.name, 'Lung');
  assert.throws(() => readRecipe('{'), /Not a recipe/);
  assert.throws(() => readRecipe(JSON.stringify({ ...recipe, format: 'other' })), /Not a SituCraft recipe/);
  assert.throws(() => readRecipe(JSON.stringify({ ...recipe, version: 2 })), /version 2.*newer one/);
  assert.throws(() => readRecipe(JSON.stringify({ ...recipe, kind: 'export' })), /for export, not a refinement/);
});

test('sizes, times, loads and failures as the program words them', () => {
  assert.deepEqual([bytes(512), bytes(2048), bytes(89e6), bytes(3.4e9), bytes(-1)], ['512 bytes', '2 kB', '89 MB', '3.4 GB', 'unknown']);
  assert.deepEqual([memory(34359738368), memory(93323264), memory(-1)], ['32.0 GB', '89 MB', 'unknown']);
  assert.deepEqual([duration(0.2), duration(45), duration(600), duration(7200)], ['1 second', '45 seconds', '10 minutes', '2.0 hours']);
  assert.deepEqual(load(4e9, 16e9), { share: 0.25, level: 'ok' });
  assert.equal(load(9e9, 16e9).level, 'warn');
  assert.equal(load(15e9, 16e9).level, 'danger');
  assert.equal(load(1, 0).level, 'unknown');
  assert.deepEqual(failureOf({ error: 'The refinement needs about 20 GB of memory; the computer has 16 GB.', failure: { class: 'memory', remedy: 'Refine a region first.' } }),
    { title: 'Memory', message: 'The refinement needs about 20 GB of memory; the computer has 16 GB.', remedy: 'Refine a region first.' });
  assert.equal(failureOf({ error: 'x' }), null);
});
