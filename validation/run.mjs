// SituCraft's validation suite. Unit tests (go test, web/lib/*.test.mjs) check functions in
// isolation; this suite checks whole results against independent references and known truth
// (committed outputs of independent tools, validation/reference/; simulated tissues), and reports
// how closely SituCraft agrees, against stated tolerances (situcraft-spec/conventions.md, "Tests
// and validation").
//
//   node validation/run.mjs [suite …] [--verbose] [--require-data]
//
// Suites: accessibility (run by default); viewer (external data and Chrome: the tissue viewer in
// headless Chrome against its data, validation/viewer.mjs; run only when named). Exits with status
// 1 when a check fails.
//
// Suites marked "external data" need files that node validation/fetch.mjs downloads into
// validation/cache/ (wave 1, slice 2); without them the suite is skipped, and fails with
// --require-data, as in CI. Adapted from MaveScape 0.8.0 validation/run.mjs.

import { readFileSync } from 'node:fs';
import { textPairs, themeTokens } from './accessibility-cases.mjs';
import { VISIONS, lab as labOf, paletteReport, simulate } from '../web/lib/colorvision.js';
import { CATEGORICAL, CATEGORICAL_CVD, colormapColor } from '../web/lib/colormaps.js';
import { viewerSuite } from './viewer.mjs';

const args = process.argv.slice(2);
const verbose = args.includes('--verbose');
const requireData = args.includes('--require-data');
const requested = args.filter((a) => !a.startsWith('--'));

const results = [];
function check(suite, name, value, ok, required) {
  results.push({ suite, name, value, ok, required });
  if (verbose || !ok) console.log(`${ok ? 'ok  ' : 'FAIL'} ${suite}: ${name} — ${value} (required ${required})`);
}

const fmt = (v, d = 3) => (Number.isFinite(v) ? Number(v.toFixed(d)).toString() : String(v));

// A suite throws MissingData when its external data are not in validation/cache/.
class MissingData extends Error {}

const suites = {
  // External data and Chrome: run only when named.
  viewer: () => viewerSuite({ check, MissingData, verbose }),

  // Every text-on-surface pair of the interface reaches WCAG AA contrast (4.5:1), in both themes
  // with color-vision-friendly colors off and on; the palettes stay apart with protanopia,
  // deuteranopia and tritanopia (Machado et al. 2009; CIEDE2000); and the sequential map read as
  // "more" gets lighter steadily in every vision.
  accessibility() {
    const css = readFileSync(new URL('../web/styles.css', import.meta.url), 'utf8');
    const tokens = themeTokens(css);
    for (const [name, t] of Object.entries(tokens)) {
      const pairs = textPairs(t);
      const failing = pairs.filter((p) => !(p.ratio >= 4.5));
      const worst = pairs.reduce((a, b) => (b.ratio < a.ratio ? b : a));
      check('accessibility', `text contrast, ${name} theme${name.includes('cvd') ? ' (color-vision-friendly colors)' : ''}: ${pairs.length} text-on-surface pairs`, failing.length ? failing.map((p) => `${p.use} ${fmt(p.ratio, 3)}`).join('; ') : `lowest ${fmt(worst.ratio, 3)} (${worst.use})`, !failing.length, '≥ 4.5:1');
    }
    const describe = (report) => VISIONS.map((v) => `${v} ${fmt(report[v].min, 3)}`).join(', ');
    const friendly8 = paletteReport(CATEGORICAL_CVD, 8);
    const friendly20 = paletteReport(CATEGORICAL_CVD);
    check('accessibility', 'color-vision-friendly palette: smallest CIEDE2000 between any two of the first 8 colors', describe(friendly8), VISIONS.every((v) => friendly8[v].min >= 10), '≥ 10 in every vision');
    check('accessibility', 'color-vision-friendly palette: the same for all 20 colors', describe(friendly20), VISIONS.every((v) => friendly20[v].min >= 7), '≥ 7 in every vision');
    const default8 = paletteReport(CATEGORICAL, 8);
    check('accessibility', 'default palette, first 8 colors (why the setting exists)', describe(default8), true, 'reported');
    for (const name of ['light', 'dark', 'light+cvd', 'dark+cvd']) {
      const t = tokens[name];
      const report = paletteReport([t.ok, t.warn, t.danger]);
      const required = name.includes('cvd');
      check('accessibility', `status colors (ok, warning, danger), ${name}: smallest CIEDE2000`, describe(report), !required || VISIONS.every((v) => report[v].min >= 9), required ? '≥ 9 in every vision' : 'reported');
    }
    const monotone = (name) => VISIONS.map((v) => {
      const L = Array.from({ length: 33 }, (_, i) => labOf(simulate(colormapColor(name, i / 32), v))[0]);
      let reversals = 0;
      for (let i = 1; i < L.length; i += 1) if (L[i] < L[i - 1] - 0.5) reversals += 1;
      return reversals;
    });
    const viridis = monotone('viridis');
    check('accessibility', 'viridis: lightness never falls along the map, in every vision', `${viridis.reduce((a, b) => a + b, 0)} reversals`, viridis.every((r) => r === 0), '0');
    // The diverging maps for differences (centered on zero): their two halves must stay apart in
    // every vision, or a decrease would look like an increase.
    for (const name of ['rdbu', 'puor']) {
      const report = paletteReport([colormapColor(name, 0.1), colormapColor(name, 0.9)]);
      check('accessibility', `${name}: its two ends (0.1 and 0.9) apart in every vision, smallest CIEDE2000`, describe(report), VISIONS.every((v) => report[v].min >= 20), '≥ 20 in every vision');
    }
  },
};

// --- Run ------------------------------------------------------------------------------------------

const explicitOnly = new Set(['viewer']);
const names = requested.length ? requested : Object.keys(suites).filter((name) => !explicitOnly.has(name));
const skipped = [];
const started = performance.now();
for (const name of names) {
  if (!suites[name]) {
    console.error(`Unknown suite ${name}. Suites: ${Object.keys(suites).join(', ')}`);
    process.exit(2);
  }
  const t0 = performance.now();
  try {
    await suites[name]();
  } catch (error) {
    if (error instanceof MissingData) {
      skipped.push(name);
      console.log(`- ${name}: skipped (external data: ${error.message})`);
      if (requireData) check(name, 'external data present', error.message, false, 'present (--require-data)');
      continue;
    }
    check(name, 'suite ran', error.stack?.split('\n').slice(0, 3).join(' | ') ?? error.message, false, 'no error');
  }
  const mine = results.filter((r) => r.suite === name);
  console.log(`${mine.every((r) => r.ok) ? '✓' : '✗'} ${name}: ${mine.filter((r) => r.ok).length}/${mine.length} checks (${((performance.now() - t0) / 1000).toFixed(1)} s)`);
}
const failed = results.filter((r) => !r.ok);
console.log(`\n${results.length - failed.length}/${results.length} checks passed in ${((performance.now() - started) / 1000).toFixed(1)} s.`);
if (skipped.length) console.log(`Skipped for want of external data: ${skipped.join(', ')} (node validation/fetch.mjs downloads it).`);
if (failed.length) process.exit(1);
