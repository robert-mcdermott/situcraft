// Color-vision deficiency: how colors look to people with protanopia, deuteranopia and
// tritanopia (Machado, Oliveira & Fernandes 2009, IEEE TVCG 15:1291, doi:10.1109/TVCG.2009.113,
// severity 1, applied to linear sRGB), and how far apart two colors look (CIEDE2000; Sharma, Wu &
// Dalal 2005, doi:10.1002/col.20070). Used to check the palettes (validation `accessibility`).
// Adapted from MaveScape 0.8.0 web/lib/colorvision.js.

import { hexToRgb, rgbToHex } from './colormaps.js';

const MACHADO = {
  protan: [0.152286, 1.052583, -0.204868, 0.114503, 0.786281, 0.099216, -0.003882, -0.048116, 1.051998],
  deutan: [0.367322, 0.860646, -0.227968, 0.280085, 0.672501, 0.047413, -0.01182, 0.04294, 0.968881],
  tritan: [1.255528, -0.076749, -0.178779, -0.078411, 0.930809, 0.147602, 0.004733, 0.691367, 0.3039],
};
export const VISIONS = ['normal', 'protan', 'deutan', 'tritan'];

const toLinear = (c) => {
  const v = c / 255;
  return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
};
const fromLinear = (v) => {
  const c = Math.min(1, Math.max(0, v));
  return 255 * (c <= 0.0031308 ? 12.92 * c : 1.055 * c ** (1 / 2.4) - 0.055);
};

// A color as seen with a deficiency ('normal' returns it unchanged).
export function simulate(hex, vision) {
  if (vision === 'normal') return hex;
  const m = MACHADO[vision];
  if (!m) throw new Error(`Unknown vision ${vision}.`);
  const [r, g, b] = hexToRgb(hex).map(toLinear);
  return rgbToHex([m[0] * r + m[1] * g + m[2] * b, m[3] * r + m[4] * g + m[5] * b, m[6] * r + m[7] * g + m[8] * b].map(fromLinear));
}

// CIE L*a*b* (D65) of an sRGB color.
export function lab(hex) {
  const [r, g, b] = hexToRgb(hex).map(toLinear);
  const x = (0.4124564 * r + 0.3575761 * g + 0.1804375 * b) / 0.95047;
  const y = 0.2126729 * r + 0.7151522 * g + 0.072175 * b;
  const z = (0.0193339 * r + 0.119192 * g + 0.9503041 * b) / 1.08883;
  const f = (t) => (t > 216 / 24389 ? Math.cbrt(t) : (24389 / 27 * t + 16) / 116);
  return [116 * f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z))];
}

// CIEDE2000 color difference of two Lab colors.
export function deltaE2000([L1, a1, b1], [L2, a2, b2]) {
  const rad = Math.PI / 180;
  const C1 = Math.hypot(a1, b1);
  const C2 = Math.hypot(a2, b2);
  const Cm = (C1 + C2) / 2;
  const G = 0.5 * (1 - Math.sqrt(Cm ** 7 / (Cm ** 7 + 25 ** 7)));
  const ap1 = (1 + G) * a1;
  const ap2 = (1 + G) * a2;
  const Cp1 = Math.hypot(ap1, b1);
  const Cp2 = Math.hypot(ap2, b2);
  const hp = (b, ap) => (b === 0 && ap === 0 ? 0 : ((Math.atan2(b, ap) / rad) + 360) % 360);
  const hp1 = hp(b1, ap1);
  const hp2 = hp(b2, ap2);
  const dL = L2 - L1;
  const dC = Cp2 - Cp1;
  let dh = 0;
  if (Cp1 * Cp2 !== 0) {
    dh = hp2 - hp1;
    if (dh > 180) dh -= 360;
    else if (dh < -180) dh += 360;
  }
  const dH = 2 * Math.sqrt(Cp1 * Cp2) * Math.sin((dh * rad) / 2);
  const Lm = (L1 + L2) / 2;
  const Cpm = (Cp1 + Cp2) / 2;
  let hm = hp1 + hp2;
  if (Cp1 * Cp2 !== 0) {
    if (Math.abs(hp1 - hp2) > 180) hm += hm < 360 ? 360 : -360;
    hm /= 2;
  }
  const T = 1 - 0.17 * Math.cos((hm - 30) * rad) + 0.24 * Math.cos(2 * hm * rad) + 0.32 * Math.cos((3 * hm + 6) * rad) - 0.2 * Math.cos((4 * hm - 63) * rad);
  const dTheta = 30 * Math.exp(-(((hm - 275) / 25) ** 2));
  const RC = 2 * Math.sqrt(Cpm ** 7 / (Cpm ** 7 + 25 ** 7));
  const SL = 1 + (0.015 * (Lm - 50) ** 2) / Math.sqrt(20 + (Lm - 50) ** 2);
  const SC = 1 + 0.045 * Cpm;
  const SH = 1 + 0.015 * Cpm * T;
  const RT = -Math.sin(2 * dTheta * rad) * RC;
  return Math.sqrt((dL / SL) ** 2 + (dC / SC) ** 2 + (dH / SH) ** 2 + RT * (dC / SC) * (dH / SH));
}

// How distinguishable a palette's colors are: for each vision, the smallest CIEDE2000 between any
// two of its first n colors, and the pair. { [vision]: { min, pair: [i, j] } }.
export function paletteReport(colors, n = colors.length) {
  const out = {};
  for (const vision of VISIONS) {
    const labs = colors.slice(0, n).map((c) => lab(simulate(c, vision)));
    let min = Infinity;
    let pair = null;
    for (let i = 0; i < labs.length; i += 1) {
      for (let j = i + 1; j < labs.length; j += 1) {
        const d = deltaE2000(labs[i], labs[j]);
        if (d < min) { min = d; pair = [i, j]; }
      }
    }
    out[vision] = { min, pair };
  }
  return out;
}
