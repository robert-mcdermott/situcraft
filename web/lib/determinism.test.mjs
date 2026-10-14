// Results must not depend on the JavaScript engine: Math.log, Math.exp, Math.pow and the other
// transcendental functions (and the ** operator, which is Math.pow) give different last bits in
// different engines and versions (dmath.js). Computation in the page uses dmath.js instead; only
// the color modules, which draw and check contrast and never reach a result, may use them.
// Adapted from MaveScape 0.8.0 web/lib/determinism.test.mjs.

import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import test from 'node:test';

const DISPLAY_ONLY = new Set(['colormaps.js', 'colorvision.js', 'dmath.js']);
const TRANSCENDENTAL = /\bMath\.(log|log10|log2|log1p|exp|expm1|pow|cbrt|sinh|cosh|tanh|asinh|acosh|atanh|sin|cos|tan|asin|acos|atan|atan2|hypot)\b/;
// ** with anything but an exact power of two (2 ** 31) as its operands.
const POWER = /\*\*/;
const EXACT_POWER = /\b2 \*\* \d+\b/g;

const code = (text) => text
  .replace(/\/\*[\s\S]*?\*\//g, '')
  .replace(/\/\/.*$/gm, '')
  .replace(/'(?:[^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"|`(?:[^`\\]|\\.)*`/g, '""');

test('no engine-dependent math in the modules that compute results', () => {
  const dir = new URL('.', import.meta.url);
  const found = [];
  for (const name of readdirSync(dir).filter((f) => f.endsWith('.js') && !DISPLAY_ONLY.has(f))) {
    const lines = code(readFileSync(new URL(name, dir), 'utf8')).split('\n');
    lines.forEach((line, i) => {
      if (TRANSCENDENTAL.test(line) || POWER.test(line.replace(EXACT_POWER, ''))) found.push(`${name}:${i + 1}: ${line.trim()}`);
    });
  }
  assert.deepEqual(found, [], `use dmath.js (log, exp, log10, pow, square):\n${found.join('\n')}`);
});
