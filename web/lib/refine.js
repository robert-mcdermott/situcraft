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
  // Artifacts before the studio did not record whether it was judged: it is when 100 or more
  // sweeps were summarized.
  const judged = s.gewekeJudged ?? s.samples >= 100;
  const settled = Math.abs(z) <= 2;
  if (!judged) {
    out.push({ ok: false, text: `Not judged: only ${count(s.samples)} sweeps were summarized, and Geweke's diagnostic of convergence needs 100 or more. Run more sweeps to know whether the run settled.` });
  } else out.push({
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
  if (Number.isFinite(s.seedAgreement)) {
    out.push({
      ok: s.seedAgreement >= 0.9,
      text: `A second seed put ${pct(s.seedAgreement)} of the transcripts in refined cells in the same cell${s.seedAgreement >= 0.9 ? '' : ': much of the result is chance here, so read its boundaries with care'}.`,
    });
  }
  if (s.regions > 1 || s.patches || s.cellsHeld) {
    const regions = s.regions === 1 ? 'one region' : `${count(s.regions)} regions`;
    out.push({ ok: true, text: `${count(s.cellsRefined)} cell${s.cellsRefined === 1 ? '' : 's'} refined in ${s.patches ? `${count(s.patches)} patches of ${regions}, stitched into one segmentation` : regions}${s.cellsHeld ? `, ${count(s.cellsHeld)} about ${s.cellsRefined === 1 ? 'it' : 'them'} held as the instrument drew them` : ''}.` });
  }
  return out;
}

// What a diagnostics summary says of the background: a plain statement, or '' for a run recorded
// before it was measured. The false decodes are measured by the negative controls; the ambient RNA
// less surely, since where cells cover the tissue the ambient RNA of genes they express cannot be
// told from their own (research.md §3.2, the background: on simulated tissues, from a quarter of
// the truth to twice it).
export function background(s) {
  if (!Number.isFinite(s?.falseDecodeShare) || !Number.isFinite(s?.ambientShare)) return '';
  const controls = s.negativeControls
    ? `measured by ${count(s.negativeControls)} negative-control transcripts`
    : 'with no negative controls in the region to measure them';
  return `The model's background accounts for ${pct(s.falseDecodeShare + s.ambientShare)} of the modeled transcripts: false decodes ${pct(s.falseDecodeShare)}, ${controls}, and ambient RNA ${pct(s.ambientShare)}, estimated less surely (where cells cover the tissue, ambient RNA of genes they express cannot be told from their own: on simulated tissues this part was found at a quarter to twice the truth). ${pct(s.inBackgroundShare ?? 0, 2)} of transcripts are left in no cell, where the background explains them better than any cell; the rest of the background is counted in the cells.`;
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
export const GENERAL_LIMITATION = 'It cannot create cells absent from the instrument\'s segmentation, and where neighboring cells express the same genes, the boundary between them is uncertain.';

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

// --- The segmentation studio (api_refine.go): its steps, and a spec as the page edits it ---

export const STUDIO_STEPS = [
  { id: 'input', label: 'Input', about: 'The transcripts, the cells it starts from, and what it covers' },
  { id: 'compute', label: 'Compute', about: 'Threads, memory, disk and time' },
  { id: 'model', label: 'Model', about: 'A preset, or every setting' },
  { id: 'review', label: 'Review', about: 'What will run, what it risks, and its recipe' },
  { id: 'run', label: 'Run', about: 'Its progress, and stopping it' },
  { id: 'compare', label: 'Compare', about: 'Its convergence, its limitations and what changed' },
];

const copy = (x) => JSON.parse(JSON.stringify(x));

// A canonical form for comparing settings: keys sorted at every level.
function canonical(x) {
  if (Array.isArray(x)) return x.map(canonical);
  if (x && typeof x === 'object') return Object.fromEntries(Object.keys(x).sort().map((k) => [k, canonical(x[k])]));
  return x;
}
const same = (a, b) => JSON.stringify(canonical(a)) === JSON.stringify(canonical(b));

// A preset's spec, keeping what was chosen of the seed, the area and the quality: a preset is
// settings, not data. The ROI pilot asks for regions.
export function presetSpec(presets, id, from = null) {
  const p = presets.find((x) => x.id === id);
  if (!p) throw new Error(`No preset ${id}.`);
  const spec = { preset: p.id, options: copy(p.options), area: { mode: 'whole' }, min_qv: 20, seed_check: Boolean(p.seedCheck) };
  if (p.gcPercent) spec.gc_percent = p.gcPercent;
  if (from) {
    spec.options.seed = from.options.seed;
    spec.area = copy(from.area);
    spec.min_qv = from.min_qv;
  }
  if (p.area === 'regions' && spec.area.mode !== 'regions') spec.area = { mode: 'regions', regions: spec.area.regions ?? [] };
  return spec;
}

// The preset a spec's settings are, as the program resolves it: its own while they are the
// preset's but for the seed (and what it covers), Expert otherwise.
export function resolvePreset(spec, presets) {
  if (!spec.preset || spec.preset === 'expert') return spec.preset;
  const p = presets.find((x) => x.id === spec.preset);
  if (!p) return spec.preset;
  const mine = { ...spec.options, seed: p.options.seed };
  return same(mine, p.options) && Boolean(spec.seed_check) === Boolean(p.seedCheck) ? spec.preset : 'expert';
}

// Where a setting's key (refine.SettingsHelp) is in a spec: stages.* on the last stage.
function place(spec, key) {
  const [head, tail] = key.split('.');
  if (!tail) {
    if (key === 'seed' || key === 'samples' || key === 'block_um') return [spec.options, key];
    return [spec, key];
  }
  if (head === 'stages') return [spec.options.stages.at(-1), tail];
  return [spec.options[head], tail];
}

export function settingValue(spec, key) {
  const [obj, k] = place(spec, key);
  if (key === 'gc_percent') return obj[k] || 50;
  if (key === 'seed_check') return Boolean(obj[k]);
  return obj[k];
}

// A spec with a setting changed, its preset resolved again (the seed and the quality are the
// data's choices, not the preset's).
export function withSetting(spec, key, value, presets) {
  const next = copy(spec);
  const [obj, k] = place(next, key);
  obj[k] = value;
  if (key === 'stages.sweeps' && next.options.samples > value) next.options.samples = Math.max(1, Math.floor((value * 5) / 6));
  next.preset = resolvePreset(next, presets);
  return next;
}

// What is wrong with a setting's value, by its bounds ('' for nothing).
export function settingProblem(setting, value) {
  if (typeof value === 'boolean') return '';
  if (!Number.isFinite(value)) return `${setting.name} is a number.`;
  const whole = ['seed', 'samples', 'stages.sweeps', 'stages.layers', 'model.components', 'gc_percent'].includes(setting.key);
  if (whole && !Number.isInteger(value)) return `${setting.name} is a whole number.`;
  if ((setting.min || setting.max) && (value < setting.min || value > setting.max)) return `${setting.name} is from ${setting.min.toLocaleString('en-US')} to ${setting.max.toLocaleString('en-US')}${setting.unit && setting.unit !== '%' ? ` ${setting.unit}` : ''}.`;
  return '';
}

// A region typed as "x0, y0, x1, y1" (µm).
export function parseRegion(text) {
  const v = String(text).split(/[\s,;]+/).filter(Boolean).map(Number);
  if (v.length !== 4 || v.some((x) => !Number.isFinite(x))) throw new Error('A region is four numbers in µm: x0, y0, x1, y1.');
  const [x0, y0, x1, y1] = v;
  if (!(x1 > x0 && y1 > y0)) throw new Error('A region\'s second corner is right of and below its first.');
  return { x0_um: x0, y0_um: y0, x1_um: x1, y1_um: y1 };
}

export const regionText = (r) => `(${Math.round(r.x0_um)}, ${Math.round(r.y0_um)})–(${Math.round(r.x1_um)}, ${Math.round(r.y1_um)}) µm`;

// What an area covers, in words.
export function areaText(area) {
  if (area.mode === 'sections') return `the tissue's separate sections, each refined apart (cells more than ${area.section_gap_um || 100} µm apart are separate)`;
  if (area.mode === 'regions') return area.regions?.length ? `${area.regions.length === 1 ? 'the region' : `${area.regions.length} regions:`} ${area.regions.map(regionText).join('; ')}` : 'a region, not chosen yet';
  return 'the whole tissue';
}

// What a preflight says of patches (internal/segment/refine Units): a region too large for one run
// is refined in overlapping patches, stitched; '' when it is not.
export function patchesText(pre) {
  const patches = pre?.patches ?? [];
  if (!patches.length) return '';
  const side = (u) => Math.round(Math.max(u.own.x1_um - u.own.x0_um, u.own.y1_um - u.own.y0_um));
  const sides = patches.map(side);
  const lo = Math.min(...sides);
  const hi = Math.max(...sides);
  return `Too large for one run: refined in ${patches.length} patches of ${lo === hi ? `${lo} µm` : `${lo} to ${hi} µm`}, one after another, each also refining the cells within 40 µm of its square; the segmentation takes each place from the patch it lies most inside of. A patch's memory is what the run needs, whatever the tissue's size.`;
}

// A recipe (recipe.go) read: its spec and the dataset it was made for. Throws why it is not one
// this page reads.
export function readRecipe(text) {
  let r;
  try {
    r = JSON.parse(text);
  } catch (error) {
    throw new Error(`Not a recipe: ${error.message}`);
  }
  if (r?.format !== 'situcraft-recipe') throw new Error('Not a SituCraft recipe.');
  if (!(r.version >= 1 && r.version <= 1)) throw new Error(`A recipe of version ${r.version}, which this SituCraft does not read: a newer one made it.`);
  if (r.kind !== 'refinement') throw new Error(`A recipe for ${r.kind}, not a refinement.`);
  const s = r.refinement;
  if (!s?.options?.stages?.length) throw new Error('The recipe has no settings.');
  return { spec: { preset: s.preset ?? 'expert', options: s.options, area: s.area ?? { mode: 'whole' }, min_qv: s.min_qv ?? 20, seed_check: Boolean(s.seed_check), ...(s.gc_percent ? { gc_percent: s.gc_percent } : {}) }, dataset: r.dataset ?? null };
}

// Sizes and times as the program writes them: memory in binary units, as computers state theirs
// (refine.Memory); disk in decimal units, as disks do (refine.Bytes); times (refine.Duration).
export function memory(n) {
  if (!(n >= 0)) return 'unknown';
  if (n >= 2 ** 30) return `${(n / 2 ** 30).toFixed(1)} GB`;
  if (n >= 2 ** 20) return `${Math.round(n / 2 ** 20)} MB`;
  if (n >= 2 ** 10) return `${Math.round(n / 2 ** 10)} kB`;
  return `${n} bytes`;
}

export function bytes(n) {
  if (!(n >= 0)) return 'unknown';
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)} GB`;
  if (n >= 1e6) return `${Math.round(n / 1e6)} MB`;
  if (n >= 1e3) return `${Math.round(n / 1e3)} kB`;
  return `${n} bytes`;
}

export function duration(s) {
  if (s >= 5400) return `${(s / 3600).toFixed(1)} hours`;
  if (s >= 90) return `${Math.round(s / 60)} minutes`;
  const n = Math.round(Math.max(1, s));
  return `${n} second${n === 1 ? '' : 's'}`;
}

// How full a resource would be: its share, and a level (ok, warn: past half; danger: past the
// preflight's stop, refine.memoryStop).
export function load(need, have) {
  if (!(have > 0) || !(need >= 0)) return { share: null, level: 'unknown' };
  const share = need / have;
  return { share, level: share > 0.85 ? 'danger' : share > 0.5 ? 'warn' : 'ok' };
}

const FAILURE_CLASS = { prior: 'The cells to start from', input: 'The bundle', memory: 'Memory', storage: 'Disk space', region: 'The region', settings: 'The settings' };

// A failed job's classified failure, in words: { title, message, remedy } (null when it has none).
export function failureOf(job) {
  if (!job?.failure) return null;
  return { title: FAILURE_CLASS[job.failure.class] ?? 'The refinement', message: job.error ?? '', remedy: job.failure.remedy };
}
