// Color maps for expression, QC maps and image channels, as 256-entry RGB lookup tables. The
// perceptually uniform maps (viridis, magma, inferno, plasma, cividis) are sampled from
// matplotlib's definitions; the diverging maps (rdbu, puor) from ColorBrewer, for differences
// centered on zero. Adapted from MaveScape 0.8.0 web/lib/colormaps.js.

const STOPS = {
  viridis: ['#440154', '#482878', '#3e4989', '#31688e', '#26828e', '#1f9e89', '#35b779', '#6ece58', '#b5de2b', '#fde725'],
  magma: ['#000004', '#1c1044', '#4f127b', '#812581', '#b5367a', '#e55964', '#fb8761', '#fec287', '#fcfdbf'],
  inferno: ['#000004', '#1f0c48', '#550f6d', '#88226a', '#ba3655', '#e35933', '#f98e09', '#f9cb35', '#fcffa4'],
  plasma: ['#0d0887', '#46039f', '#7201a8', '#9c179e', '#bd3786', '#d8576b', '#ed7953', '#fb9f3a', '#fdca26', '#f0f921'],
  cividis: ['#00224e', '#123570', '#3b496c', '#575d6d', '#707173', '#8a8678', '#a59c74', '#c3b369', '#e1cc55', '#fee838'],
  turbo: ['#30123b', '#4145ab', '#4675ed', '#39a2fc', '#1bcfd4', '#24eca6', '#61fc6c', '#a4fc3b', '#d1e834', '#f3c63a', '#fe9b2d', '#f36315', '#d93806', '#b11901', '#7a0402'],
  classic: ['#1d16d9', '#1677f2', '#14c7e8', '#2ee08c', '#8ae62b', '#e7e51f', '#ffb21a', '#ff6a14', '#e8150f'],
  ocean: ['#0b1f3a', '#123e6b', '#16609a', '#1a85bf', '#2fa8d0', '#5fc6d5', '#9bdfd6', '#d6f2e3'],
  grays: ['#e8e8e8', '#bdbdbd', '#969696', '#737373', '#525252', '#252525', '#000000'],
  blues: ['#f7fbff', '#deebf7', '#c6dbef', '#9ecae1', '#6baed6', '#4292c6', '#2171b5', '#08519c', '#08306b'],
  // Diverging, for differences and z-scores.
  rdbu: ['#053061', '#2166ac', '#4393c3', '#92c5de', '#d1e5f0', '#f7f7f7', '#fddbc7', '#f4a582', '#d6604d', '#b2182b', '#67001f'],
  puor: ['#2d004b', '#542788', '#8073ac', '#b2abd2', '#d8daeb', '#f7f7f7', '#fee0b6', '#fdb863', '#e08214', '#b35806', '#7f3b08'],
};

export const COLORMAP_NAMES = Object.keys(STOPS);
export const SEQUENTIAL = ['viridis', 'magma', 'inferno', 'plasma', 'cividis', 'turbo', 'classic', 'ocean', 'grays', 'blues'];
export const DIVERGING = ['rdbu', 'puor'];

export function hexToRgb(hex) {
  const value = hex.replace('#', '');
  const full = value.length === 3 ? value.split('').map((c) => c + c).join('') : value;
  const n = Number.parseInt(full, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

export function rgbToHex([r, g, b]) {
  return `#${[r, g, b].map((v) => Math.round(Math.max(0, Math.min(255, v))).toString(16).padStart(2, '0')).join('')}`;
}

// --- Color-vision-friendly colors (a setting) ---------------------------------------------------
//
// With the setting on, categorical colors come from CATEGORICAL_CVD, colors of the default
// palette stored in a workspace (selections, conditions) are shown as their counterparts
// (displayColor),
// and the rainbow maps (classic, turbo), whose red and green ends look alike to many people, are
// drawn as viridis. The workspace is not changed: turning the setting off restores every color.
let friendly = false;

export function setColorVisionFriendly(on) {
  friendly = Boolean(on);
}

export function colorVisionFriendly() {
  return friendly;
}

const RAINBOW = new Set(['classic', 'turbo']);

// The map actually drawn for a requested one.
export function displayColormap(name) {
  return friendly && RAINBOW.has(name) ? 'viridis' : name;
}

const lutCache = new Map();

// A Uint8Array of 256 × 3 RGB values.
export function colormapLUT(requested = 'viridis', reversed = false) {
  const name = displayColormap(requested);
  const key = `${name}:${reversed}`;
  let lut = lutCache.get(key);
  if (lut) return lut;
  const stops = (STOPS[name] ?? STOPS.viridis).map(hexToRgb);
  if (reversed) stops.reverse();
  lut = new Uint8Array(256 * 3);
  for (let i = 0; i < 256; i += 1) {
    const t = (i / 255) * (stops.length - 1);
    const k = Math.min(stops.length - 2, Math.floor(t));
    const f = t - k;
    for (let c = 0; c < 3; c += 1) lut[i * 3 + c] = Math.round(stops[k][c] + (stops[k + 1][c] - stops[k][c]) * f);
  }
  lutCache.set(key, lut);
  return lut;
}

export function colormapColor(name, t) {
  const lut = colormapLUT(name);
  const i = Math.max(0, Math.min(255, Math.round(t * 255)));
  return rgbToHex([lut[i * 3], lut[i * 3 + 1], lut[i * 3 + 2]]);
}

export function colormapCSS(name, steps = 12) {
  const parts = [];
  for (let i = 0; i <= steps; i += 1) parts.push(`${colormapColor(name, i / steps)} ${((100 * i) / steps).toFixed(1)}%`);
  return `linear-gradient(90deg, ${parts.join(', ')})`;
}

// Categorical colors for conditions, replicates and selections: 20 distinguishable hues that hold
// up on white and dark backgrounds (in the spirit of Glasbey and Tableau palettes).
export const CATEGORICAL = [
  '#4c78e0', '#f0803c', '#3fb27f', '#e45563', '#9a6fd8', '#c99a2e', '#2bb3c0', '#e86fb3',
  '#7c8a2b', '#8f5b3a', '#5c6bc0', '#26a69a', '#ef6c00', '#ad1457', '#6d4c41', '#00897b',
  '#7e57c2', '#c0ca33', '#d84315', '#546e7a',
];

// The color-vision-friendly counterpart: Okabe & Ito's six colors (2008, without black and
// yellow, which do not show as lines on white), then colors chosen one at a time to be as far as
// possible (CIEDE2000) from those already chosen as seen with normal vision, protanopia,
// deuteranopia and tritanopia (colorvision.js), among colors with a chroma of at least 30 that
// show on light and dark plots (contrast ≥ 2.5). Any two of the first eight differ by ≥ 11 in
// every vision, any two of the twenty by ≥ 7 (the default palette: 0.8 with deuteranopia).
export const CATEGORICAL_CVD = [
  '#0072b2', '#e69f00', '#009e73', '#d55e00', '#cc79a7', '#56b4e9', '#993366', '#bb0000',
  '#226644', '#8877ff', '#cc1199', '#6611ff', '#99aa66', '#aa6688', '#bb2244', '#888844',
  '#ee6688', '#6655ff', '#995566', '#aa77cc',
];

const TO_FRIENDLY = new Map(CATEGORICAL.map((c, i) => [c, CATEGORICAL_CVD[i]]));
const FROM_FRIENDLY = new Map(CATEGORICAL_CVD.map((c, i) => [c, CATEGORICAL[i]]));

// A stored color as shown: one of either palette as its counterpart in the palette in use; any
// other color (chosen by the user) as it is.
export function displayColor(color) {
  if (typeof color !== 'string') return color;
  const key = color.toLowerCase();
  return (friendly ? TO_FRIENDLY.get(key) : FROM_FRIENDLY.get(key)) ?? color;
}

// The color a workspace item (a selection set or a condition; kind 'selections' or 'conditions')
// is shown in: its own, or with color-vision-friendly colors on, the friendly palette in the
// workspace's order, so that every item stays distinguishable whatever colors it was given (they
// return when the setting is off).
export function shownColor(ws, item, kind = 'selections') {
  if (!item) return undefined;
  if (!friendly) return item.color;
  const index = (ws?.[kind] ?? []).findIndex((x) => x.id === item.id);
  return index >= 0 ? categoricalColor(index) : displayColor(item.color);
}

export function categoricalColor(index) {
  const palette = friendly ? CATEGORICAL_CVD : CATEGORICAL;
  const n = palette.length;
  if (index < n) return palette[index];
  // Beyond the palette: golden-angle hues.
  const hue = (index * 137.508) % 360;
  return hslToHex(hue, 62, 52);
}

export function hslToHex(h, s, l) {
  const sat = s / 100;
  const light = l / 100;
  const k = (n) => (n + h / 30) % 12;
  const a = sat * Math.min(light, 1 - light);
  const f = (n) => light - a * Math.max(-1, Math.min(k(n) - 3, Math.min(9 - k(n), 1)));
  return rgbToHex([f(0) * 255, f(8) * 255, f(4) * 255]);
}

// Relative luminance (WCAG) for choosing readable text on a color.
export function luminance(hex) {
  const [r, g, b] = hexToRgb(hex).map((v) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
