import { test } from 'node:test';
import assert from 'node:assert/strict';
import { REFINE_METHOD, cellFlags, convergence, flagsOf, isRefinement, limitations } from './refine.js';

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
