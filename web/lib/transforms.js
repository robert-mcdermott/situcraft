// Transforms placing post-Xenium images (api_transforms.go): artifacts of kind 'transform', each a
// version of an image's alignment (Xenium Explorer's matrix or keypoints, or landmarks placed here),
// with its source and destination systems (the image's pixels, the dataset's µm), its landmarks and
// fit, who made it and its parent version. Which one places an image is a decision, as a
// segmentation's promotion is: the latest names it.
//
// The landmark fits are internal/register's, computed the same way bit for bit (fitFingerprint in
// both), so that what the page shows while landmarks are placed is what the program records.

// --- Fits (internal/register/fit.go) ---------------------------------------------------------------

// A transform is its two rows [[a, b, c], [d, e, f]]: (x, y) → (a·x + b·y + c, d·x + e·y + f).
// A landmark is { fixed: [x, y] (µm), moving: [x, y] (the image's pixels) }.

export const MODELS = ['similarity', 'affine'];
export const MIN_LANDMARKS = 3;

function applyRows(t, x, y) {
  return [t[0][0] * x + t[0][1] * y + t[0][2], t[1][0] * x + t[1][1] * y + t[1][2]];
}

function distance(t, k) {
  const [x, y] = applyRows(t, k.moving[0], k.moving[1]);
  const dx = x - k.fixed[0];
  const dy = y - k.fixed[1];
  return Math.sqrt(dx * dx + dy * dy);
}

export function residuals(t, landmarks) {
  return landmarks.map((k) => distance(t, k));
}

function det(t) {
  return t[0][0] * t[1][1] - t[0][1] * t[1][0];
}

// A rotation, scale and translation; reflect mirrors the moving positions' y first (the fit then
// includes that reflection). Null when the landmarks are all at one place.
function fitProper(kps, reflect) {
  const n = kps.length;
  let mx = 0;
  let my = 0;
  let fx = 0;
  let fy = 0;
  for (const k of kps) {
    const y = reflect ? -k.moving[1] : k.moving[1];
    mx += k.moving[0];
    my += y;
    fx += k.fixed[0];
    fy += k.fixed[1];
  }
  mx /= n;
  my /= n;
  fx /= n;
  fy /= n;
  let sxx = 0;
  let sxy = 0;
  let ss = 0;
  for (const k of kps) {
    const y = reflect ? -k.moving[1] : k.moving[1];
    const ax = k.moving[0] - mx;
    const ay = y - my;
    const bx = k.fixed[0] - fx;
    const by = k.fixed[1] - fy;
    sxx += ax * bx + ay * by;
    sxy += ax * by - ay * bx;
    ss += ax * ax + ay * ay;
  }
  if (ss === 0) return null;
  const norm = Math.sqrt(sxx * sxx + sxy * sxy);
  if (norm === 0) return null;
  const s = norm / ss;
  const c = s * (sxx / norm);
  const si = s * (sxy / norm);
  let A = c;
  let B = -si;
  const D = si;
  let E = c;
  const C = fx - (A * mx + B * my);
  const F = fy - (D * mx + E * my);
  if (reflect) {
    B = -B;
    E = -E;
  }
  return [[A, B, C], [D, E, F]];
}

function sumSquares(t, kps) {
  let total = 0;
  for (const r of residuals(t, kps)) total += r * r;
  return total;
}

function fitSimilarity(kps) {
  if (kps.length < MIN_LANDMARKS) throw new Error(`${kps.length} landmarks: a similarity is fitted to three or more (a third tells whether it reflects)`);
  const proper = fitProper(kps, false);
  const mirrored = fitProper(kps, true);
  if (!proper && !mirrored) throw new Error('the landmarks are all at one place: no transform fits them');
  if (!mirrored || (proper && sumSquares(proper, kps) <= sumSquares(mirrored, kps))) return proper;
  return mirrored;
}

function fitAffineRows(kps) {
  const n = kps.length;
  let mx = 0;
  let my = 0;
  let fx = 0;
  let fy = 0;
  for (const k of kps) {
    mx += k.moving[0];
    my += k.moving[1];
    fx += k.fixed[0];
    fy += k.fixed[1];
  }
  mx /= n;
  my /= n;
  fx /= n;
  fy /= n;
  let sxx = 0;
  let sxy = 0;
  let syy = 0;
  let bx = 0;
  let by = 0;
  let cx = 0;
  let cy = 0;
  for (const k of kps) {
    const ax = k.moving[0] - mx;
    const ay = k.moving[1] - my;
    const ux = k.fixed[0] - fx;
    const uy = k.fixed[1] - fy;
    sxx += ax * ax;
    sxy += ax * ay;
    syy += ay * ay;
    bx += ax * ux;
    by += ay * ux;
    cx += ax * uy;
    cy += ay * uy;
  }
  const d = sxx * syy - sxy * sxy;
  if (!(d > 1e-12 * ((sxx + syy) * (sxx + syy)))) return null;
  const A = (bx * syy - by * sxy) / d;
  const B = (by * sxx - bx * sxy) / d;
  const D = (cx * syy - cy * sxy) / d;
  const E = (cy * sxx - cx * sxy) / d;
  return [[A, B, fx - (A * mx + B * my)], [D, E, fy - (D * mx + E * my)]];
}

function fitAffine(kps) {
  if (kps.length < MIN_LANDMARKS) throw new Error(`${kps.length} landmarks: an affine transform is fitted to three or more`);
  const t = fitAffineRows(kps);
  if (!t) throw new Error('the landmarks lie on one line: an affine transform needs three not on a line');
  return t;
}

// Each landmark's distance from where the model fitted to the others puts it (a similarity keeps the
// full fit's handedness); null when too few remain without one.
export function leaveOneOut(model, kps, full) {
  const need = model === 'affine' ? 3 : 2;
  if (kps.length - 1 < need) return null;
  const out = [];
  for (let i = 0; i < kps.length; i += 1) {
    const others = [...kps.slice(0, i), ...kps.slice(i + 1)];
    const t = model === 'affine' ? fitAffineRows(others) : fitProper(others, det(full) < 0);
    if (!t) return null;
    out.push(distance(t, kps[i]));
  }
  return out;
}

function rmsMax(v) {
  let sum = 0;
  let worst = 0;
  for (const x of v) {
    sum += x * x;
    worst = Math.max(worst, x);
  }
  return [Math.sqrt(sum / v.length), worst];
}

// The model fitted to the landmarks and how well it fits: { model, transform, residuals,
// leaveOneOut (or null), rms, max, looRms, looMax } in µm. Throws, saying why, when it cannot fit.
export function fitLandmarks(model, landmarks) {
  if (!MODELS.includes(model)) throw new Error(`no model ${model}: similarity or affine`);
  const transform = model === 'affine' ? fitAffine(landmarks) : fitSimilarity(landmarks);
  const res = residuals(transform, landmarks);
  const [rms, max] = rmsMax(res);
  const loo = leaveOneOut(model, landmarks, transform);
  const [looRms, looMax] = loo ? rmsMax(loo) : [0, 0];
  return { model, transform, residuals: res, leaveOneOut: loo, rms, max, looRms, looMax };
}

// A deformation's displacement at a point (µm): internal/register's cubic B-spline over its grid of
// control points (0 beyond its reach).
export function deformationAt(b, x, y) {
  const u = (x - b.origin[0]) / b.spacing;
  const v = (y - b.origin[1]) / b.spacing;
  const fi = Math.floor(u);
  const fj = Math.floor(v);
  const weights = (t) => {
    const s = 1 - t;
    return [(s * s * s) / 6, (3 * t * t * t - 6 * t * t + 4) / 6, (-3 * t * t * t + 3 * t * t + 3 * t + 1) / 6, (t * t * t) / 6];
  };
  const wx = weights(u - fi);
  const wy = weights(v - fj);
  let dx = 0;
  let dy = 0;
  for (let b2 = 0; b2 < 4; b2 += 1) {
    const j = fj - 1 + b2;
    if (j < 0 || j >= b.ny) continue;
    for (let a = 0; a < 4; a += 1) {
      const i = fi - 1 + a;
      if (i < 0 || i >= b.nx) continue;
      const w = wx[a] * wy[b2];
      dx += w * b.dx[j * b.nx + i];
      dy += w * b.dy[j * b.nx + i];
    }
  }
  return [dx, dy];
}

// Where a version (its description: transform and any deformation) places a point of the image.
export function placePoint(desc, x, y) {
  const [px, py] = applyRows(desc.transform, x, y);
  if (!desc.deformation) return [px, py];
  const [dx, dy] = deformationAt(desc.deformation, px, py);
  return [px + dx, py + dy];
}

// What a transform does, in numbers: its scale (µm per pixel, on average over directions), the
// direction it turns the x axis to (turn: [cos, sin], shown in degrees by the page), whether it
// mirrors, and for an affine transform how unequal its scales are and its shear.
export function transformShape(t) {
  const d = det(t);
  const scale = Math.sqrt(Math.abs(d));
  const sx = Math.sqrt(t[0][0] * t[0][0] + t[1][0] * t[1][0]);
  const sy = Math.abs(d) / sx;
  return { scale, turn: [t[0][0] / sx, t[1][0] / sx], reflected: d < 0, aspect: sy / sx, shear: (t[0][0] * t[0][1] + t[1][0] * t[1][1]) / (sx * sx) };
}

// --- Records and decisions ------------------------------------------------------------------------

// The project's record of a transform artifact (its manifest, as the program answers it).
export function transformRecord(dataset, manifest, now = new Date().toISOString()) {
  const lineage = manifest.lineage ?? {};
  const summary = manifest.summary ?? {};
  return {
    kind: 'transform',
    id: manifest.id,
    name: manifest.name,
    dataset: dataset.id,
    image: summary.image ?? lineage.parents?.find((p) => p.kind === 'image')?.id ?? null,
    parent: summary.parent || null,
    state: manifest.state,
    created: manifest.created,
    registered: now,
    method: { name: lineage.method?.name ?? '', version: lineage.method?.version ?? '', citation: lineage.method?.citation ?? '' },
    software: lineage.software ?? [],
    files: (manifest.files ?? []).map(({ path, role, size, sha256, location }) => ({ path, role, size, sha256, location: location ?? '' })),
    summary,
    findings: manifest.findings ?? [],
  };
}

export function transformsOf(project, imageId) {
  return project.artifacts.filter((a) => a.kind === 'transform' && a.image === imageId);
}

// The project with a transform recorded (unchanged when it already is).
export function addTransform(project, record, now = new Date().toISOString()) {
  if (project.artifacts.some((a) => a.kind === 'transform' && a.id === record.id && a.dataset === record.dataset)) return project;
  const image = project.artifacts.find((a) => a.kind === 'image' && a.id === record.image);
  return {
    ...project,
    modified: now,
    artifacts: [...project.artifacts, record],
    history: [...project.history, { time: now, action: 'transform', detail: `Recorded an alignment of ${image?.name ?? record.image}: ${describeTransform(record)} (artifact ${record.id})` }],
  };
}

// The reason recorded when an image's supplied alignment places it by itself.
export const SUPPLIED_ALIGNMENT = 'Supplied with the image: its only alignment when it was added.';

// The project with a transform chosen to place its image, for a reason.
export function useTransform(project, { transform, rationale, by = 'the user', now = new Date().toISOString() }) {
  const record = project.artifacts.find((a) => a.kind === 'transform' && a.id === transform);
  if (!record) throw new Error('The project has no such alignment.');
  if (record.state !== 'complete') throw new Error(`${record.name} is ${record.state}: only a complete alignment can place an image.`);
  const image = project.artifacts.find((a) => a.kind === 'image' && a.id === record.image);
  const reason = String(rationale ?? '').trim() || `${describeTransform(record)}.`;
  return {
    ...project,
    modified: now,
    decisions: [...(project.decisions ?? []), { kind: 'use-transform', dataset: record.dataset, image: record.image, artifact: transform, rationale: reason, by, time: now }],
    history: [...project.history, { time: now, action: 'decision', detail: `Placed ${image?.name ?? record.image} by ${record.name}: ${reason}` }],
  };
}

// The transform placing an image and the decision choosing it, or null (the image is not aligned).
export function transformInUse(project, imageId) {
  const decisions = project.decisions ?? [];
  for (let i = decisions.length - 1; i >= 0; i -= 1) {
    const d = decisions[i];
    if (d.kind !== 'use-transform' || d.image !== imageId) continue;
    const record = project.artifacts.find((a) => a.kind === 'transform' && a.id === d.artifact && a.state === 'complete');
    if (record) return { record, decision: d };
  }
  return null;
}

// How a transform was made and how well it fits, in a line.
export function describeTransform(record) {
  const s = record.summary ?? {};
  if (s.method === 'automatic') {
    const fit = Number.isFinite(s.rms) ? `: the ${s.kept} kept of ${s.matches} local matches ${s.rms.toFixed(2)} µm from the fit root-mean-square, ${s.looRms.toFixed(2)} µm left out` : '';
    const deformed = s.deformed ? `; deformed (by at most ${s.maxDisplacement.toFixed(1)} µm), each fifth of the matches predicted ${s.heldOut.toFixed(2)} µm off against ${s.modelHeldOut.toFixed(2)} µm by the ${s.model} alone` : '';
    return `Automatic, by the nuclei (${s.channel ?? 'its nuclear stain'}), ${s.model}${fit}${deformed}`;
  }
  const how = {
    matrix: 'Xenium Explorer\'s transformation matrix',
    keypoints: `Xenium Explorer's ${s.landmarks} keypoints`,
    landmarks: `${s.landmarks} landmarks`,
  }[s.method] ?? record.name;
  const fit = Number.isFinite(s.rms) && s.landmarks ? `, ${s.model}: residuals ${s.rms.toFixed(2)} µm root-mean-square${Number.isFinite(s.looRms) && s.looRms > 0 ? `, ${s.looRms.toFixed(2)} µm left out` : ''}` : '';
  return `${how}${fit}`;
}
