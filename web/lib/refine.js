// SituCraft's transcript-informed segmentation (internal/segment/refine) as the page shows it: what
// its diagnostics say of its convergence, and the limitations it flags per cell (research.md §3.2).

// The method's name, as its artifacts record it.
export const REFINE_METHOD = 'SituCraft transcript-informed segmentation';

export function isRefinement(record) {
  return record?.method?.name === REFINE_METHOD || record?.lineage?.method?.name === REFINE_METHOD;
}

const pct = (v, digits = 1) => `${(100 * v).toFixed(digits)}%`;
const count = (n) => Number(n ?? 0).toLocaleString('en-US');

// What a diagnostics summary says of the run's convergence: [{ ok, text }]. The verdict is
// Geweke's diagnostic of the log-likelihood over the summarized sweeps (|z| ≤ 2: settled); the
// split-half agreement says how stable the assignments are, which the posterior's own uncertainty
// bounds below 1.
export function convergence(s) {
  if (!s) return [];
  const out = [];
  const z = s.gewekeZ ?? 0;
  const settled = Math.abs(z) <= 2;
  out.push({
    ok: settled,
    text: settled
      ? `Settled: the log-likelihood's mean is the same early and late in the last ${count(s.samples)} sweeps (Geweke's z = ${z.toFixed(1)}).`
      : `Still drifting slowly: the log-likelihood changed by ${Math.abs(s.logLikelihoodDriftPerTranscript ?? 0).toFixed(4)} per transcript between the halves of the last ${count(s.samples)} sweeps (Geweke's z = ${z.toFixed(1)}). On simulated tissues, longer runs change the result little; run more sweeps to be sure.`,
  });
  out.push({
    ok: true,
    text: `Stability: ${pct(s.splitHalfAgreement)} of transcripts are in the same cell over both halves of those sweeps; most of the rest are between cells, as their probabilities say.`,
  });
  out.push({
    ok: s.lowProbabilityShare < 0.1,
    text: `Each transcript's cell has a mean posterior probability of ${s.meanProbability.toFixed(3)}; ${pct(s.lowProbabilityShare)} of transcripts are in their cell in fewer than half of the sweeps.`,
  });
  out.push({ ok: true, text: `${count(Math.round(s.voxelsMovedPerSweep))} voxels still move each sweep at the end, as boundaries do under an uncertain posterior.` });
  return out;
}

// The limitations a run flags, as plain statements: [{ key, n, text }] with n cells each.
export function limitations(s, thresholds = { lowTranscripts: 10, uncertainShare: 0.75 }) {
  if (!s) return [];
  const out = [];
  if (s.cellsLowTranscripts) out.push({ key: 'lowTranscripts', n: s.cellsLowTranscripts, text: `${count(s.cellsLowTranscripts)} cells have fewer than ${thresholds.lowTranscripts} gene transcripts: their boundaries come more from the prior than from their transcripts.` });
  if (s.cellsUncertainBoundaries) out.push({ key: 'uncertainBoundaries', n: s.cellsUncertainBoundaries, text: `${count(s.cellsUncertainBoundaries)} cells have uncertain boundaries: their voxels are theirs in fewer than ${Math.round(100 * thresholds.uncertainShare)}% of the sweeps, on average.` });
  if (s.cellsNoNucleus) out.push({ key: 'noNucleus', n: s.cellsNoNucleus, text: `${count(s.cellsNoNucleus)} cells started without a nucleus: no nuclear prior holds them.` });
  if (s.cellsLeftOut) out.push({ key: 'leftOut', n: s.cellsLeftOut, text: `${count(s.cellsLeftOut)} of the instrument's cells were left out: their nuclei share a voxel with another's at the finest resolution.` });
  return out;
}

// What every refinement shares, whatever its cells.
export const GENERAL_LIMITATION = 'It cannot create cells absent from the instrument\'s segmentation, and where neighbouring cells express the same genes, the boundary between them is uncertain.';

const FLAG_TEXT = {
  lowTranscripts: 'few transcripts',
  uncertainBoundaries: 'uncertain boundary',
  noNucleus: 'no nucleus',
};

// Each flagged cell's limitations, from diagnostics.json: Map id → [plain words].
export function cellFlags(diagnostics) {
  const out = new Map();
  const l = diagnostics?.limitations ?? {};
  for (const key of ['lowTranscripts', 'uncertainBoundaries', 'noNucleus']) {
    for (const id of l[key] ?? []) {
      if (!out.has(id)) out.set(id, []);
      out.get(id).push(FLAG_TEXT[key]);
    }
  }
  return out;
}

// A cell list's flags in words ("c12: few transcripts; c40: uncertain boundary"), or ''.
export function flagsOf(flags, ids) {
  return ids.filter((id) => flags.has(id)).map((id) => `${id}: ${flags.get(id).join(', ')}`).join('; ');
}
