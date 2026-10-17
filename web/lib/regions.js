// Regions of the tissue (wave 5, slice 5): areas drawn on it (a rectangle, a polygon, freehand or
// with a brush), each named and classed, kept in the project as versions: every change (its shape,
// name or class, or its removal) a version appended, with who made it, when and why, the latest the
// region. A region's shape is a multipolygon in µm (the dataset's coordinates): polygons, each an
// outer ring and the holes in it, each ring a list of [x, y] not repeating its first point.
//
// Regions are written and read as GeoJSON as QuPath writes and reads its annotations: a
// FeatureCollection (or, from QuPath 0.2, an array) of Features, each a Polygon or MultiPolygon in
// the image's full-resolution pixels ((0, 0) its top-left corner), its properties an objectType
// ("annotation"), a name and a classification (a name and a colour: [r, g, b] from QuPath 0.4,
// colorRGB, a packed integer, before). Coordinates are mapped to and from µm by a function: an
// image's transform (lib/transforms.js), the Xenium image's pixel size, or none.

// --- Classes --------------------------------------------------------------------------------------

// QuPath's own classes and their colours, so that a region classed in one is classed alike in the
// other.
export const CLASSES = [
  { name: 'Tumor', color: [200, 0, 0] },
  { name: 'Stroma', color: [150, 200, 150] },
  { name: 'Immune cells', color: [160, 90, 160] },
  { name: 'Necrosis', color: [50, 50, 50] },
  { name: 'Other', color: [255, 200, 0] },
  { name: 'Region*', color: [0, 0, 180] },
  { name: 'Ignore*', color: [180, 180, 180] },
];

// A class's color: QuPath's for its classes, else one made from the name (never gray, which reads as
// Ignore*).
export function classColor(name) {
  const known = CLASSES.find((c) => c.name === name);
  if (known) return known.color;
  let h = 2166136261;
  for (const ch of String(name ?? '')) h = Math.imul(h ^ ch.charCodeAt(0), 16777619) >>> 0;
  const palette = [[31, 119, 180], [255, 127, 14], [44, 160, 44], [214, 39, 40], [148, 103, 189], [140, 86, 75], [227, 119, 194], [188, 189, 34], [23, 190, 207]];
  return palette[h % palette.length];
}

export const colorCSS = ([r, g, b]) => `rgb(${r}, ${g}, ${b})`;

// --- Geometry -------------------------------------------------------------------------------------

// A ring's signed area (positive when its turn is counter-clockwise as x right, y up).
export function ringArea(ring) {
  let a = 0;
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i, i += 1) a += ring[j][0] * ring[i][1] - ring[i][0] * ring[j][1];
  return a / 2;
}

// A multipolygon's area: its outer rings' less their holes'.
export function area(geometry) {
  let a = 0;
  for (const polygon of geometry) polygon.forEach((ring, k) => { a += (k === 0 ? 1 : -1) * Math.abs(ringArea(ring)); });
  return a;
}

export function bounds(geometry) {
  let x0 = Infinity;
  let y0 = Infinity;
  let x1 = -Infinity;
  let y1 = -Infinity;
  for (const polygon of geometry) for (const ring of polygon) for (const [x, y] of ring) {
    x0 = Math.min(x0, x);
    y0 = Math.min(y0, y);
    x1 = Math.max(x1, x);
    y1 = Math.max(y1, y);
  }
  return { x0, y0, x1, y1 };
}

function inRing(ring, x, y) {
  let inside = false;
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i, i += 1) {
    const [xi, yi] = ring[i];
    const [xj, yj] = ring[j];
    if ((yi > y) !== (yj > y) && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) inside = !inside;
  }
  return inside;
}

// Whether a point lies in a multipolygon: in one of its polygons' outer rings and none of its holes
// (a ray crossing each ring's edges, counted odd or even).
export function containsPoint(geometry, x, y) {
  for (const polygon of geometry) {
    if (!inRing(polygon[0], x, y)) continue;
    let hole = false;
    for (let k = 1; k < polygon.length && !hole; k += 1) hole = inRing(polygon[k], x, y);
    if (!hole) return true;
  }
  return false;
}

// The rows of the cells (a cells table: x and y, their centroids in µm) whose centroid lies in the
// region.
export function cellsIn(geometry, cells) {
  const b = bounds(geometry);
  const out = [];
  for (let i = 0; i < cells.n; i += 1) {
    const x = cells.x[i];
    const y = cells.y[i];
    if (x < b.x0 || x > b.x1 || y < b.y0 || y > b.y1) continue;
    if (containsPoint(geometry, x, y)) out.push(i);
  }
  return Uint32Array.from(out);
}

export function rectangle(x0, y0, x1, y1) {
  const [a, b] = [Math.min(x0, x1), Math.max(x0, x1)];
  const [c, d] = [Math.min(y0, y1), Math.max(y0, y1)];
  return [[[[a, c], [b, c], [b, d], [a, d]]]];
}

// A ring without repeated points (consecutive, or its last its first).
function cleanRing(ring) {
  const out = [];
  for (const p of ring) {
    const q = out.at(-1);
    if (!q || q[0] !== p[0] || q[1] !== p[1]) out.push([p[0], p[1]]);
  }
  while (out.length > 1 && out[0][0] === out.at(-1)[0] && out[0][1] === out.at(-1)[1]) out.pop();
  return out;
}

export function polygonOf(points) {
  return [[cleanRing(points)]];
}

// A path simplified (Douglas DH, Peucker TK. Algorithms for the reduction of the number of points
// required to represent a digitized line or its caricature. Cartographica 1973;10(2):112–122):
// points within tolerance of the line between those kept dropped.
function simplifyPath(points, tolerance) {
  if (points.length < 3) return points.slice();
  const keep = new Uint8Array(points.length);
  keep[0] = keep[points.length - 1] = 1;
  const stack = [[0, points.length - 1]];
  while (stack.length) {
    const [a, b] = stack.pop();
    const [ax, ay] = points[a];
    const [bx, by] = points[b];
    const dx = bx - ax;
    const dy = by - ay;
    const len2 = dx * dx + dy * dy;
    let worst = -1;
    let at = -1;
    for (let i = a + 1; i < b; i += 1) {
      const [px, py] = points[i];
      let d2;
      const ex = px - ax;
      const ey = py - ay;
      if (len2 === 0) d2 = ex * ex + ey * ey;
      else {
        const t = Math.max(0, Math.min(1, (ex * dx + ey * dy) / len2));
        d2 = (ex - t * dx) * (ex - t * dx) + (ey - t * dy) * (ey - t * dy);
      }
      if (d2 > worst) {
        worst = d2;
        at = i;
      }
    }
    if (at >= 0 && worst > tolerance * tolerance) {
      keep[at] = 1;
      stack.push([a, at], [at, b]);
    }
  }
  return points.filter((_, i) => keep[i]);
}

// A ring simplified, split at its point farthest from its first.
export function simplifyRing(ring, tolerance) {
  if (ring.length < 4) return ring.slice();
  let far = 0;
  let worst = -1;
  for (let i = 1; i < ring.length; i += 1) {
    const dx = ring[i][0] - ring[0][0];
    const dy = ring[i][1] - ring[0][1];
    const d = dx * dx + dy * dy;
    if (d > worst) {
      worst = d;
      far = i;
    }
  }
  const a = simplifyPath(ring.slice(0, far + 1), tolerance);
  const b = simplifyPath([...ring.slice(far), ring[0]], tolerance);
  return [...a, ...b.slice(1, -1)];
}

// A ring with points added so that no edge is longer than step.
export function densifyRing(ring, step) {
  const out = [];
  for (let i = 0; i < ring.length; i += 1) {
    const [ax, ay] = ring[i];
    const [bx, by] = ring[(i + 1) % ring.length];
    out.push([ax, ay]);
    const n = Math.ceil(Math.sqrt((bx - ax) * (bx - ax) + (by - ay) * (by - ay)) / step);
    for (let k = 1; k < n; k += 1) out.push([ax + ((bx - ax) * k) / n, ay + ((by - ay) * k) / n]);
  }
  return out;
}

// A multipolygon with each point mapped (densified first, when step is given, so that a map that
// bends straight lines bends them alike).
export function mapGeometry(geometry, f, step = 0) {
  return geometry.map((polygon) => polygon.map((ring) => (step > 0 ? densifyRing(ring, step) : ring).map(([x, y]) => f(x, y))));
}

// --- The brush ------------------------------------------------------------------------------------

// The side of the brush's tiles, in samples, and the most samples (and tiles) its grid keeps.
const TILE = 64;
const MOST_SAMPLES = 16e6;

// A region painted, or erased, with a brush: the region (or nothing) and the stroke sampled on a
// grid of an eighth of the brush's radius, each sample a signed distance, above 0 inside (the
// region's rows filled between its edges' crossings, and near its edges each sample's distance from
// them; the stroke a capsule about each of its segments, the radius less a sample's distance from
// it), so that an edge is found between samples where it lies; the outline traced back by marching
// squares, simplified to a sixteenth of a grid step. A curve is cut by the outline's chords between
// samples: a disc's area comes out about 0.5% small. The grid keeps its samples in tiles of 64 × 64
// only near the region's edges and under the stroke, each other tile wholly in or out, so that a
// large region is painted at the brush's own step (coarser only past 16 million samples).
export function brushGeometry(base, stroke, radius, erase = false) {
  const pts = stroke.length ? stroke : [];
  const b0 = base?.length ? bounds(base) : null;
  const bx0 = Math.min(b0?.x0 ?? Infinity, ...pts.map((p) => p[0] - radius));
  const by0 = Math.min(b0?.y0 ?? Infinity, ...pts.map((p) => p[1] - radius));
  const bx1 = Math.max(b0?.x1 ?? -Infinity, ...pts.map((p) => p[0] + radius));
  const by1 = Math.max(b0?.y1 ?? -Infinity, ...pts.map((p) => p[1] + radius));
  if (!Number.isFinite(bx0)) return base ?? [];
  const edges = [];
  for (const polygon of base ?? []) for (const ring of polygon) for (let i = 0, j = ring.length - 1; i < ring.length; j = i, i += 1) edges.push([ring[j], ring[i]]);
  const segs = pts.length === 1 ? [[pts[0], pts[0]]] : pts.slice(1).map((p, k) => [pts[k], p]);
  let step = Math.max(radius / 8, 1e-3);
  let g = brushGrid(bx0, by0, bx1, by1, step, edges, segs, radius);
  while (!g) {
    step *= 1.5;
    g = brushGrid(bx0, by0, bx1, by1, step, edges, segs, radius);
  }
  const { x0, y0, w, h, tw, th, kept } = g;
  const far = 2 * step;
  const reach = radius + step;
  const cx = (i) => x0 + i * step;
  const cy = (j) => y0 + j * step;
  const tiles = Array.from(kept, (k) => (k ? new Float32Array(TILE * TILE).fill(-far) : null));
  const inside = new Uint8Array(tw * th);
  const value = (i, j) => {
    if (i < 0 || j < 0 || i >= w || j >= h) return -far;
    const tx = Math.floor(i / TILE);
    const ty = Math.floor(j / TILE);
    const t = tiles[ty * tw + tx];
    if (t) return t[(j - ty * TILE) * TILE + i - tx * TILE];
    return inside[ty * tw + tx] ? far : -far;
  };
  // A sample of a tile kept (all those changed are: near an edge, or the stroke): its index in its
  // tile, the tile left in held.
  let held = null;
  const slot = (i, j) => {
    const tx = Math.floor(i / TILE);
    const ty = Math.floor(j / TILE);
    held = tiles[ty * tw + tx];
    return (j - ty * TILE) * TILE + i - tx * TILE;
  };
  // The region: each row's samples between its rings' crossings (odd-even); a tile not kept, as its
  // middle sample is. Each row of tiles is given the edges that reach it.
  const rowEdges = Array.from({ length: th }, () => []);
  for (const e of edges) {
    const [[, ay], [, by]] = e;
    const t0 = Math.max(0, Math.floor((Math.min(ay, by) - y0) / step / TILE));
    const t1 = Math.min(th - 1, Math.floor((Math.max(ay, by) - y0) / step / TILE));
    for (let ty = t0; ty <= t1; ty += 1) rowEdges[ty].push(e);
  }
  const crossings = (list, y) => {
    const xs = [];
    for (const [[ax, ay], [bx, by]] of list) if ((ay > y) !== (by > y)) xs.push(ax + ((y - ay) * (bx - ax)) / (by - ay));
    return xs.sort((a, b) => a - b);
  };
  if (edges.length) {
    for (let ty = 0; ty < th; ty += 1) {
      const xs = crossings(rowEdges[ty], cy(Math.min(h - 1, ty * TILE + TILE / 2)));
      let any = false;
      for (let tx = 0; tx < tw; tx += 1) {
        if (kept[ty * tw + tx]) {
          any = true;
          continue;
        }
        const x = cx(Math.min(w - 1, tx * TILE + TILE / 2));
        let lo = 0;
        let hi = xs.length;
        while (lo < hi) {
          const mid = (lo + hi) >> 1;
          if (xs[mid] < x) lo = mid + 1;
          else hi = mid;
        }
        inside[ty * tw + tx] = lo % 2;
      }
      if (!any) continue;
      for (let j = ty * TILE; j < Math.min(h, ty * TILE + TILE); j += 1) {
        const ys = crossings(rowEdges[ty], cy(j));
        for (let k = 0; k + 1 < ys.length; k += 2) {
          const i0 = Math.max(0, Math.ceil((ys[k] - x0) / step));
          const i1 = Math.min(w - 1, Math.floor((ys[k + 1] - x0) / step));
          for (let tx = Math.floor(i0 / TILE); tx <= Math.floor(i1 / TILE); tx += 1) {
            const t = tiles[ty * tw + tx];
            if (!t) continue;
            const off = (j - ty * TILE) * TILE - tx * TILE;
            for (let i = Math.max(i0, tx * TILE); i <= Math.min(i1, tx * TILE + TILE - 1); i += 1) t[off + i] = far;
          }
        }
      }
    }
    // Near each edge (walked in half steps), each sample's distance from it, signed as filled.
    for (const [[ax, ay], [bx, by]] of edges) {
      const dx = bx - ax;
      const dy = by - ay;
      const len2 = dx * dx + dy * dy;
      walk(ax, ay, bx, by, x0, y0, step, (ci, cj) => {
        for (let j = Math.max(0, cj - 3); j <= Math.min(h - 1, cj + 3); j += 1) {
          for (let i = Math.max(0, ci - 3); i <= Math.min(w - 1, ci + 3); i += 1) {
            const px = cx(i) - ax;
            const py = cy(j) - ay;
            const t = len2 > 0 ? Math.max(0, Math.min(1, (px * dx + py * dy) / len2)) : 0;
            const qx = px - t * dx;
            const qy = py - t * dy;
            const d = Math.sqrt(qx * qx + qy * qy);
            const k = slot(i, j);
            if (d < Math.abs(held[k])) held[k] = held[k] > 0 ? d : -d;
          }
        }
      });
    }
  }
  // The stroke: the radius less each sample's distance from its nearest segment (near them), painted
  // as the larger of it and the region's, erased as the smaller of the region's and its negative.
  for (const [[ax, ay], [bx, by]] of segs) {
    const dx = bx - ax;
    const dy = by - ay;
    const len2 = dx * dx + dy * dy;
    const [i0, j0, i1, j1] = reachOf(ax, ay, bx, by, x0, y0, step, reach, w, h);
    for (let j = j0; j <= j1; j += 1) {
      for (let i = i0; i <= i1; i += 1) {
        const px = cx(i) - ax;
        const py = cy(j) - ay;
        const t = len2 > 0 ? Math.max(0, Math.min(1, (px * dx + py * dy) / len2)) : 0;
        const qx = px - t * dx;
        const qy = py - t * dy;
        const d2 = qx * qx + qy * qy;
        if (d2 > reach * reach) continue;
        const v = radius - Math.sqrt(d2);
        const k = slot(i, j);
        held[k] = erase ? Math.min(held[k], -v) : Math.max(held[k], v);
      }
    }
  }
  // The blocks to trace: those touching a tile kept (a block is its tile's when its top-left sample
  // is in the tile's rows and columns shifted by one, so a tile's blocks reach into the next).
  const visit = new Uint8Array((tw + 1) * (th + 1));
  for (let ty = 0; ty < th; ty += 1) {
    for (let tx = 0; tx < tw; tx += 1) {
      if (!kept[ty * tw + tx]) continue;
      for (const [a, b] of [[0, 0], [1, 0], [0, 1], [1, 1]]) visit[(ty + b) * (tw + 1) + tx + a] = 1;
    }
  }
  const eachBlock = (fn) => {
    for (let ty = 0; ty <= th; ty += 1) {
      for (let tx = 0; tx <= tw; tx += 1) {
        if (!visit[ty * (tw + 1) + tx]) continue;
        for (let j = ty * TILE - 1; j < Math.min(h, ty * TILE + TILE - 1); j += 1) {
          for (let i = tx * TILE - 1; i < Math.min(w, tx * TILE + TILE - 1); i += 1) fn(i, j);
        }
      }
    }
  };
  const near = radius + 3 * step;
  const restore = base?.length ? (ring) => restoreFar(ring, base.flat(), (p) => segs.some(([a, b]) => segmentDistance(p, a, b) <= near), step) : (ring) => ring;
  return trace(value, eachBlock, (i, j) => [cx(i), cy(j)], step, restore);
}

function segmentDistance([px, py], [ax, ay], [bx, by]) {
  const dx = bx - ax;
  const dy = by - ay;
  const len2 = dx * dx + dy * dy;
  const t = len2 > 0 ? Math.max(0, Math.min(1, ((px - ax) * dx + (py - ay) * dy) / len2)) : 0;
  const qx = px - ax - t * dx;
  const qy = py - ay - t * dy;
  return Math.sqrt(qx * qx + qy * qy);
}

// The edge of the rings nearest a point: { d, o (the ring), e (its edge, from point e), t (along it) }.
function nearestEdge(rings, p) {
  let best = { d: Infinity };
  rings.forEach((ring, o) => {
    for (let e = 0; e < ring.length; e += 1) {
      const a = ring[e];
      const b = ring[(e + 1) % ring.length];
      const d = segmentDistance(p, a, b);
      if (d < best.d) {
        const dx = b[0] - a[0];
        const dy = b[1] - a[1];
        const len2 = dx * dx + dy * dy;
        best = { d, o, e, t: len2 > 0 ? ((p[0] - a[0]) * dx + (p[1] - a[1]) * dy) / len2 : 0 };
      }
    }
  });
  return best;
}

const pathLength = (pts) => pts.slice(1).reduce((a, p, k) => a + Math.sqrt((p[0] - pts[k][0]) * (p[0] - pts[k][0]) + (p[1] - pts[k][1]) * (p[1] - pts[k][1])), 0);

// A ring traced from a region painted, with its stretches away from the stroke (isNear false) put
// back as the region's own rings had them: each stretch's ends found on an edge of one of them (within
// a sixteenth of a step), the points between taken from it in the traced ring's direction, when
// that path is as long as the traced one (within 1% and two steps); a ring away from the stroke
// throughout is the region's own ring it lies on. A brush stroke so changes a region only near it.
function restoreFar(ring, rings, isNear, step) {
  const tol = step / 16;
  const n = ring.length;
  const near = ring.map(isNear);
  const sign = Math.sign(ringArea(ring));
  const oriented = (r) => (Math.sign(ringArea(r)) === sign ? r : [...r].reverse());
  if (!near.some(Boolean)) {
    // Its first point on an edge (not at a corner the trace rounded).
    let hit = { d: Infinity };
    for (let k = 0; k < n && hit.d > tol; k += 1) hit = nearestEdge(rings, ring[k]);
    const own = rings[hit.o];
    if (hit.d <= tol && Math.abs(Math.abs(ringArea(own)) - Math.abs(ringArea(ring))) <= 0.01 * Math.abs(ringArea(own)) + pathLength([...own, own[0]]) * step) return oriented(own);
    return ring;
  }
  // The far stretches, each from a near point's next to the next near point's previous.
  const start = near.indexOf(true);
  const out = [];
  let k = 0;
  while (k < n) {
    const idx = (start + k) % n;
    if (near[idx]) {
      out.push(ring[idx]);
      k += 1;
      continue;
    }
    let len = 0;
    while (k + len < n && !near[(start + k + len) % n]) len += 1;
    const run = Array.from({ length: len }, (_, m) => ring[(start + k + m) % n]);
    out.push(...spliced(run, rings, tol));
    k += len;
  }
  return out;
}

// A far stretch of a traced ring, its points between its ends replaced by the region's own.
function spliced(run, rings, tol) {
  if (run.length < 3) return run;
  let a = 0;
  let b = run.length - 1;
  let pa = nearestEdge(rings, run[a]);
  while (pa.d > tol && a < b - 1) pa = nearestEdge(rings, run[(a += 1)]);
  let pb = nearestEdge(rings, run[b]);
  while (pb.d > tol && b > a + 1) pb = nearestEdge(rings, run[(b -= 1)]);
  if (pa.d > tol || pb.d > tol || pa.o !== pb.o) return run;
  const own = rings[pa.o];
  const m = own.length;
  // The traced ring's direction along the region's ring, from how its stretch runs.
  const mid = nearestEdge([own], run[(a + b) >> 1]);
  const pos = (h) => h.e + h.t;
  const ahead = (x, y) => (((y - x) % m) + m) % m;
  const forward = ahead(pos(pa), pos(mid)) <= ahead(pos(pa), pos(pb));
  const between = [];
  if (forward) {
    if (!(pa.e === pb.e && pa.t <= pb.t)) for (let e = (pa.e + 1) % m; ; e = (e + 1) % m) {
      between.push(own[e]);
      if (e === pb.e || between.length > m) break;
    }
  } else if (!(pa.e === pb.e && pa.t >= pb.t)) {
    for (let e = pa.e; ; e = (e - 1 + m) % m) {
      between.push(own[e]);
      if (e === (pb.e + 1) % m || between.length > m) break;
    }
  }
  const path = [run[a], ...between, run[b]];
  const traced = run.slice(a, b + 1);
  const lt = pathLength(traced);
  if (between.length > m || Math.abs(pathLength(path) - lt) > 0.01 * lt + 32 * tol) return run;
  return [...run.slice(0, a), ...path, ...run.slice(b + 1)];
}

// Calls f with the grid's sample nearest each point every half step along a segment.
function walk(ax, ay, bx, by, x0, y0, step, f) {
  const dx = bx - ax;
  const dy = by - ay;
  const n = Math.max(1, Math.ceil(Math.sqrt(dx * dx + dy * dy) / (step / 2)));
  for (let m = 0; m <= n; m += 1) f(Math.round((ax + (dx * m) / n - x0) / step), Math.round((ay + (dy * m) / n - y0) / step));
}

// The samples within reach of a segment's bounds: [i0, j0, i1, j1], within the grid.
function reachOf(ax, ay, bx, by, x0, y0, step, reach, w, h) {
  return [
    Math.max(0, Math.floor((Math.min(ax, bx) - reach - x0) / step)),
    Math.max(0, Math.floor((Math.min(ay, by) - reach - y0) / step)),
    Math.min(w - 1, Math.ceil((Math.max(ax, bx) + reach - x0) / step)),
    Math.min(h - 1, Math.ceil((Math.max(ay, by) + reach - y0) / step)),
  ];
}

// The brush's grid at a step: its first sample (half a step off the shapes' bounds, so that an
// edge along the grid falls midway between samples), its size in samples and tiles, and the tiles
// kept: those within three samples of an edge of the region, or within the stroke's reach. Null when
// it would keep more than the most samples.
function brushGrid(bx0, by0, bx1, by1, step, edges, segs, radius) {
  const x0 = bx0 - 2.5 * step;
  const y0 = by0 - 2.5 * step;
  const w = Math.ceil((bx1 + 2 * step - x0) / step) + 1;
  const h = Math.ceil((by1 + 2 * step - y0) / step) + 1;
  const tw = Math.ceil(w / TILE);
  const th = Math.ceil(h / TILE);
  if (tw * th > MOST_SAMPLES) return null;
  const kept = new Uint8Array(tw * th);
  let n = 0;
  const keep = (i0, j0, i1, j1) => {
    for (let ty = Math.max(0, Math.floor(j0 / TILE)); ty <= Math.min(th - 1, Math.floor(j1 / TILE)); ty += 1) {
      for (let tx = Math.max(0, Math.floor(i0 / TILE)); tx <= Math.min(tw - 1, Math.floor(i1 / TILE)); tx += 1) {
        if (!kept[ty * tw + tx]) {
          kept[ty * tw + tx] = 1;
          n += 1;
        }
      }
    }
  };
  for (const [[ax, ay], [bx, by]] of edges) {
    walk(ax, ay, bx, by, x0, y0, step, (ci, cj) => keep(ci - 3, cj - 3, ci + 3, cj + 3));
    if (n * TILE * TILE > MOST_SAMPLES) return null;
  }
  for (const [[ax, ay], [bx, by]] of segs) keep(...reachOf(ax, ay, bx, by, x0, y0, step, radius + step, w, h));
  if (n * TILE * TILE > MOST_SAMPLES) return null;
  return { x0, y0, w, h, tw, th, kept };
}

// The outline of a grid's samples that are in (value(i, j) above 0), by marching squares over the
// blocks eachBlock gives (each by its top-left sample): each 2 × 2 block's in and out samples give
// the edges between them, each crossing a pair of samples where their values, linearly, are 0;
// joined into closed rings, each simplified then refined; a ring inside an odd number of others is
// a hole of the innermost of them. A saddle (two diagonal samples in) is cut between them.
function trace(value, eachBlock, at, step, refine = (ring) => ring) {
  // Edges keyed by their start (the midpoint of a sample pair, as twice its grid coordinates), so
  // that each is followed by the one starting where it ends; they run with the inside on their left.
  const next = new Map();
  const key = (x2, y2) => `${x2},${y2}`;
  const add = (a, b) => next.set(key(...a), b);
  const v = (i, j) => (value(i, j) > 0 ? 1 : 0);
  // Where a sample pair's values cross 0: the pair from its key (a horizontal pair's x2 is odd).
  const crossing = (x2, y2) => {
    const [ia, ja] = x2 % 2 ? [(x2 - 1) / 2, y2 / 2] : [x2 / 2, (y2 - 1) / 2];
    const [ib, jb] = x2 % 2 ? [ia + 1, ja] : [ia, ja + 1];
    const fa = value(ia, ja);
    const t = fa / (fa - value(ib, jb));
    const [ax, ay] = at(ia, ja);
    const [bx, by] = at(ib, jb);
    return [ax + t * (bx - ax), ay + t * (by - ay)];
  };
  eachBlock((i, j) => {
    const tl = v(i, j);
    const tr = v(i + 1, j);
    const br = v(i + 1, j + 1);
    const bl = v(i, j + 1);
    const c = (tl << 3) | (tr << 2) | (br << 1) | bl;
    if (c === 0 || c === 15) return;
    // Midpoints of the block's sides, as twice the grid's coordinates.
    const T = [2 * i + 1, 2 * j];
    const R = [2 * i + 2, 2 * j + 1];
    const B = [2 * i + 1, 2 * j + 2];
    const L = [2 * i, 2 * j + 1];
    switch (c) {
      case 1: add(L, B); break;
      case 2: add(B, R); break;
      case 3: add(L, R); break;
      case 4: add(R, T); break;
      case 5: add(L, B); add(R, T); break;
      case 6: add(B, T); break;
      case 7: add(L, T); break;
      case 8: add(T, L); break;
      case 9: add(T, B); break;
      case 10: add(T, L); add(B, R); break;
      case 11: add(T, R); break;
      case 12: add(R, L); break;
      case 13: add(R, B); break;
      case 14: add(B, L); break;
      default: break;
    }
  });
  const rings = [];
  for (const start of [...next.keys()]) {
    if (!next.has(start)) continue;
    const ring = [];
    let k = start;
    while (next.has(k)) {
      const [x2, y2] = k.split(',').map(Number);
      ring.push(crossing(x2, y2));
      const b = next.get(k);
      next.delete(k);
      k = key(...b);
    }
    const simple = refine(simplifyRing(ring, step / 16));
    if (simple.length >= 3 && Math.abs(ringArea(simple)) >= step * step) rings.push(simple);
  }
  // Holes into the polygons whose outer ring holds them.
  const depth = rings.map((r) => rings.filter((o) => o !== r && inRing(o, r[0][0], r[0][1])).length);
  const polygons = [];
  const outer = new Map();
  rings.forEach((r, k) => {
    if (depth[k] % 2 === 0) {
      outer.set(r, [r]);
      polygons.push(outer.get(r));
    }
  });
  rings.forEach((r, k) => {
    if (depth[k] % 2 === 0) return;
    const hosts = rings.filter((o, m) => depth[m] === depth[k] - 1 && inRing(o, r[0][0], r[0][1]));
    outer.get(hosts[0])?.push(r);
  });
  return polygons;
}

// --- Regions in the project -----------------------------------------------------------------------

function newRegionId() {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`;
  return `r-${random.replace(/-/g, '').slice(0, 16)}`;
}

// The dataset's regions as they are now (their latest versions; removed ones not), in the order
// they were drawn: { id, version (from 1), versions, name, cls, color, geometry, tool, layer, by,
// time, note }.
export function regionsOf(project, datasetId, { removed = false } = {}) {
  return (project.regions ?? [])
    .filter((r) => r.dataset === datasetId)
    .map((r) => ({ id: r.id, dataset: r.dataset, version: r.versions.length, versions: r.versions, ...r.versions.at(-1) }))
    .filter((r) => removed || !r.removed);
}

// A name for a new region of a class: the class and the next number.
export function nextRegionName(project, datasetId, cls) {
  const names = new Set((project.regions ?? []).filter((r) => r.dataset === datasetId).flatMap((r) => r.versions.map((v) => v.name)));
  for (let k = 1; ; k += 1) if (!names.has(`${cls} ${k}`)) return `${cls} ${k}`;
}

// A name no other region of the dataset has now (a region is found by its name): the name, else it
// numbered, "Tumor (2)", as a file read may repeat names.
export function uniqueRegionName(project, datasetId, name) {
  const taken = new Set(regionsOf(project, datasetId).map((r) => r.name));
  if (!taken.has(name)) return name;
  for (let k = 2; ; k += 1) if (!taken.has(`${name} (${k})`)) return `${name} (${k})`;
}

function checkName(project, datasetId, name, id = null) {
  if (regionsOf(project, datasetId).some((r) => r.name === name && r.id !== id)) throw new Error(`Another region of this dataset is named ${name}.`);
}

const areaText = (geometry) => `${Math.round(area(geometry)).toLocaleString('en-US')} µm²`;

// The project with a region added: its first version (its colour its class's unless given, as a
// file read gives it).
export function addRegion(project, { dataset, name, cls = 'Region*', color = classColor(cls), geometry, tool, layer = '', by = 'the user', note = '', now = new Date().toISOString(), id = newRegionId() }) {
  if (!geometry?.length || !(area(geometry) > 0)) throw new Error('A region needs an area.');
  const ds = project.datasets.find((d) => d.id === dataset);
  if (!ds) throw new Error('The project has no such dataset.');
  const version = { name: String(name).trim() || nextRegionName(project, dataset, cls), cls, color, geometry, tool, layer, by, time: now, note };
  checkName(project, dataset, version.name);
  return {
    ...project,
    modified: now,
    regions: [...(project.regions ?? []), { id, dataset, versions: [version] }],
    history: [...project.history, { time: now, action: 'region', detail: `Drew ${version.name} (${cls}, ${areaText(geometry)}, ${tool}) on ${ds.name}${note ? `: ${note}` : ''}` }],
  };
}

// The project with a region changed, a version appended: its name, class or shape, or its removal
// (removed: true) or return (removed: false).
export function editRegion(project, id, patch, { by = 'the user', note = '', tool, now = new Date().toISOString() } = {}) {
  const region = (project.regions ?? []).find((r) => r.id === id);
  if (!region) throw new Error('The project has no such region.');
  const last = region.versions.at(-1);
  const next = { ...last, ...patch, by, time: now, note };
  if (tool) next.tool = tool;
  if (patch.cls && !patch.color) next.color = classColor(patch.cls);
  if (!patch.removed) delete next.removed;
  if (patch.removed === false) delete next.removed;
  if (!next.geometry?.length || !(area(next.geometry) > 0)) throw new Error('A region needs an area: to take it away, remove it.');
  if (!next.removed && (next.name !== last.name || last.removed)) checkName(project, region.dataset, next.name, id);
  const what = patch.removed ? 'Removed' : patch.removed === false ? 'Returned' : patch.geometry ? 'Reshaped' : 'Changed';
  const changes = [];
  if (patch.name !== undefined && patch.name !== last.name) changes.push(`named ${patch.name}`);
  if (patch.cls !== undefined && patch.cls !== last.cls) changes.push(`classed ${patch.cls}`);
  if (patch.geometry) changes.push(areaText(patch.geometry));
  return {
    ...project,
    modified: now,
    regions: project.regions.map((r) => (r.id === id ? { ...r, versions: [...r.versions, next] } : r)),
    history: [...project.history, { time: now, action: 'region', detail: `${what} ${last.name}${changes.length ? ` (${changes.join(', ')})` : ''}, version ${region.versions.length + 1}${note ? `: ${note}` : ''}` }],
  };
}

// --- GeoJSON as QuPath writes and reads it --------------------------------------------------------

const round = (v) => Math.round(v * 1000) / 1000;

function closed(ring) {
  return [...ring.map(([x, y]) => [round(x), round(y)]), [round(ring[0][0]), round(ring[0][1])]];
}

// A deterministic UUID-like id for a region's version, as QuPath gives each object one.
function featureId(id, version) {
  let a = 2166136261;
  let b = 2654435761;
  const text = `${id}:${version}`;
  for (const ch of text) {
    a = Math.imul(a ^ ch.charCodeAt(0), 16777619) >>> 0;
    b = Math.imul(b ^ ch.charCodeAt(0), 2246822519) >>> 0;
  }
  const hex = (n) => n.toString(16).padStart(8, '0');
  const s = `${hex(a)}${hex(b)}${hex(a ^ b)}${hex(Math.imul(a, 31) >>> 0)}`;
  return `${s.slice(0, 8)}-${s.slice(8, 12)}-4${s.slice(13, 16)}-8${s.slice(17, 20)}-${s.slice(20, 32)}`;
}

// The regions as a FeatureCollection QuPath imports: each a Feature of objectType annotation, its
// name and classification, its shape in the coordinates toCoords gives (from µm), densified every
// step µm first when given (a deformation bends straight lines). What SituCraft knows of it rides in
// metadata, which QuPath keeps as text.
export function toGeoJSON(regions, { toCoords = (x, y) => [x, y], step = 0 } = {}) {
  return {
    type: 'FeatureCollection',
    features: regions.map((r) => {
      const polygons = mapGeometry(r.geometry, toCoords, step).map((p) => p.map(closed));
      return {
        type: 'Feature',
        id: featureId(r.id, r.version),
        geometry: polygons.length === 1 ? { type: 'Polygon', coordinates: polygons[0] } : { type: 'MultiPolygon', coordinates: polygons },
        properties: {
          objectType: 'annotation',
          name: r.name,
          classification: { name: r.cls, color: r.color ?? classColor(r.cls) },
          isLocked: false,
          metadata: { 'SituCraft region': r.id, 'SituCraft version': String(r.version), 'SituCraft by': r.by ?? '' },
        },
      };
    }),
  };
}

// A classification's colour as QuPath wrote it: [r, g, b] (0.4 on), or colorRGB, an ARGB integer
// (0.2, 0.3).
function colorOf(c) {
  if (Array.isArray(c?.color) && c.color.length >= 3) return c.color.slice(0, 3).map((v) => Math.max(0, Math.min(255, Math.round(Number(v)))));
  if (Number.isFinite(c?.colorRGB)) {
    const v = c.colorRGB >>> 0;
    return [(v >> 16) & 255, (v >> 8) & 255, v & 255];
  }
  return null;
}

// Regions from GeoJSON as QuPath writes it (a FeatureCollection, an array of Features, or one): its
// annotations' polygons, their coordinates taken to µm by fromCoords (densified every step of their
// own units first, when given); detections and cells (objectType detection, cell, tile) are not
// regions, and shapes other than polygons (points, lines) have no area; each kind left out is
// counted.
export function fromGeoJSON(doc, { fromCoords = (x, y) => [x, y], step = 0 } = {}) {
  const features = Array.isArray(doc) ? doc : doc?.type === 'FeatureCollection' ? doc.features ?? [] : doc?.type === 'Feature' ? [doc] : null;
  if (!features) throw new Error('This is not GeoJSON of objects: neither a FeatureCollection, an array of Features nor a Feature.');
  const skipped = {};
  const skip = (why) => { skipped[why] = (skipped[why] ?? 0) + 1; };
  const regions = [];
  for (const f of features) {
    const props = f?.properties ?? {};
    const kind = String(props.objectType ?? props.object_type ?? (f?.id === 'PathDetectionObject' ? 'detection' : f?.id === 'PathCellObject' ? 'cell' : 'annotation')).toLowerCase();
    if (kind !== 'annotation') {
      skip(kind);
      continue;
    }
    const g = f?.geometry;
    let polygons;
    if (g?.type === 'Polygon') polygons = [g.coordinates];
    else if (g?.type === 'MultiPolygon') polygons = g.coordinates;
    else if (g?.type === 'GeometryCollection') polygons = (g.geometries ?? []).flatMap((x) => (x.type === 'Polygon' ? [x.coordinates] : x.type === 'MultiPolygon' ? x.coordinates : []));
    else {
      skip(g?.type ?? 'no shape');
      continue;
    }
    const geometry = polygons
      .map((p) => p.map((ring) => cleanRing(ring.map((pt) => [Number(pt[0]), Number(pt[1])]))).filter((ring) => ring.length >= 3))
      .filter((p) => p.length);
    const mapped = mapGeometry(geometry, fromCoords, step);
    if (!mapped.length || !(area(mapped) > 0)) {
      skip('no area');
      continue;
    }
    const cls = props.classification?.name ?? props.classification?.names?.join(': ') ?? 'Region*';
    regions.push({ name: String(props.name ?? '').trim(), cls, color: colorOf(props.classification) ?? classColor(cls), geometry: mapped, source: props.metadata?.['SituCraft region'] ?? null });
  }
  return { regions, skipped };
}
