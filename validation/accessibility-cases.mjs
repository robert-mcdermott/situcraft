// The colors of SituCraft's themes, read from web/styles.css, and the text-on-surface pairs they
// are used in, for the `accessibility` validation suite: every pair must reach WCAG AA contrast
// (4.5:1 for text of normal size), in the light and dark themes, with color-vision-friendly
// colors off and on. Adapted from MaveScape 0.8.0 validation/accessibility-cases.mjs.

import { hexToRgb, luminance, rgbToHex } from '../web/lib/colormaps.js';

// The custom properties of each configuration: { light, dark, 'light+cvd', 'dark+cvd' }.
export function themeTokens(css) {
  const block = (selector) => {
    const start = css.indexOf(`${selector} {`);
    if (start < 0) return {};
    const end = css.indexOf('}', start);
    const out = {};
    for (const m of css.slice(start, end).matchAll(/--([\w-]+):\s*([^;]+);/g)) out[m[1]] = m[2].trim();
    return out;
  };
  const light = block(':root');
  const dark = { ...light, ...block(':root[data-theme="dark"]') };
  const cvd = block(':root[data-cvd]');
  const darkCvd = block(':root[data-cvd][data-theme="dark"]');
  return { light, dark, 'light+cvd': { ...light, ...cvd }, 'dark+cvd': { ...dark, ...cvd, ...darkCvd } };
}

// A color (#rrggbb or rgba(r, g, b, a)) laid over an opaque background, as #rrggbb.
export function over(color, background) {
  const m = /rgba?\(([^)]+)\)/.exec(color);
  if (!m) return color;
  const [r, g, b, a = 1] = m[1].split(',').map(Number);
  const bg = hexToRgb(background);
  return rgbToHex([r, g, b].map((c, i) => a * c + (1 - a) * bg[i]));
}

export function contrast(a, b) {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
  return (x + 0.05) / (y + 0.05);
}

// The text-on-surface pairs of the interface: [{ use, text, surface }] in hex, for a configuration.
export function textPairs(t) {
  const surfaces = ['bg', 'bg-elev', 'panel', 'panel-2', 'hover', 'active'];
  const pairs = [];
  for (const text of ['text', 'text-2', 'text-3']) for (const s of surfaces) pairs.push({ use: `${text} on ${s}`, text: t[text], surface: t[s] });
  pairs.push({ use: 'accent-text on panel', text: t['accent-text'], surface: t.panel });
  pairs.push({ use: 'accent-text on a selected chip', text: t['accent-text'], surface: over(t['accent-soft'], t.panel) });
  pairs.push({ use: 'accent-text on active', text: t['accent-text'], surface: t.active });
  const resolve = (value) => (value?.startsWith('var(--') ? t[value.slice(6, -1)] : value);
  for (const status of ['ok', 'warn', 'danger']) {
    const text = resolve(t[`${status}-text`]) ?? t[status];
    // Badges sit on panels, on table rows (hovered or selected) and on the sidebar.
    for (const s of ['panel', 'panel-2', 'hover', 'active']) pairs.push({ use: `${status} badge on ${s}`, text, surface: over(t[`${status}-soft`], t[s]) });
    for (const s of ['panel', 'panel-2']) pairs.push({ use: `${status} text on ${s}`, text, surface: t[s] });
  }
  pairs.push({ use: 'primary button', text: '#ffffff', surface: t['accent-button'] ?? t.accent });
  return pairs.map((p) => ({ ...p, ratio: contrast(p.text, p.surface) }));
}
