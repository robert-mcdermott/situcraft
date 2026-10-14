// Polygons for the tissue viewer, without the DOM (tested in Node): reading a geometry tile,
// triangulating rings for fills, finding the ring under a point, and outlines as line segments.
// A ring is vertices start…end-1 of an interleaved [x0, y0, x1, y1, …] array; its last vertex may
// repeat its first.

// A geometry tile as the program sends it (api_view.go): cell u32[p] | starts u32[p+1] |
// xy f32[2v], little-endian (typed arrays view it in place on little-endian machines, which every
// machine SituCraft runs on is).
export function readPolygonTile(buffer, polygons, vertices) {
  const need = 4 * polygons + 4 * (polygons + 1) + 8 * vertices;
  if (polygons === 0) return { cells: new Uint32Array(0), starts: new Uint32Array([0]), xy: new Float32Array(0) };
  if (buffer.byteLength !== need) throw new Error(`a geometry tile of ${buffer.byteLength} bytes, not ${need}`);
  return {
    cells: new Uint32Array(buffer, 0, polygons),
    starts: new Uint32Array(buffer, 4 * polygons, polygons + 1),
    xy: new Float32Array(buffer, 8 * polygons + 4, 2 * vertices),
  };
}

// The vertex count of a ring without its repeated closing vertex.
function openLength(xy, start, end) {
  let n = end - start;
  if (n >= 2 && xy[2 * start] === xy[2 * (end - 1)] && xy[2 * start + 1] === xy[2 * (end - 1) + 1]) n -= 1;
  return n;
}

function signedArea(xy, idx) {
  let a = 0;
  for (let i = 0, j = idx.length - 1; i < idx.length; j = i, i += 1) {
    a += xy[2 * idx[j]] * xy[2 * idx[i] + 1] - xy[2 * idx[i]] * xy[2 * idx[j] + 1];
  }
  return a / 2;
}

function inTriangle(px, py, ax, ay, bx, by, cx, cy) {
  const d1 = (px - bx) * (ay - by) - (ax - bx) * (py - by);
  const d2 = (px - cx) * (by - cy) - (bx - cx) * (py - cy);
  const d3 = (px - ax) * (cy - ay) - (cx - ax) * (py - ay);
  const neg = d1 < 0 || d2 < 0 || d3 < 0;
  const pos = d1 > 0 || d2 > 0 || d3 > 0;
  return !(neg && pos);
}

// Triangulates a ring by ear clipping, appending vertex indexes (into xy) to out. A ring that
// cannot be clipped (it crosses itself) is finished as a fan, which fills its outline's region
// approximately. Returns out.
export function triangulate(xy, start, end, out = []) {
  const n = openLength(xy, start, end);
  if (n < 3) return out;
  let idx = Array.from({ length: n }, (_, i) => start + i);
  // Clip counter-clockwise (in a y-up sense); a y-down ring of positive area is the same thing.
  if (signedArea(xy, idx) < 0) idx.reverse();
  let guard = 0;
  while (idx.length > 3 && guard < 4 * n * n) {
    let clipped = false;
    for (let i = 0; i < idx.length; i += 1) {
      const a = idx[(i + idx.length - 1) % idx.length];
      const b = idx[i];
      const c = idx[(i + 1) % idx.length];
      const ax = xy[2 * a], ay = xy[2 * a + 1], bx = xy[2 * b], by = xy[2 * b + 1], cx = xy[2 * c], cy = xy[2 * c + 1];
      const cross = (bx - ax) * (cy - ay) - (by - ay) * (cx - ax);
      guard += 1;
      if (cross <= 0) continue; // reflex (or flat)
      let inside = false;
      for (const p of idx) {
        if (p === a || p === b || p === c) continue;
        const px = xy[2 * p], py = xy[2 * p + 1];
        if ((px === ax && py === ay) || (px === bx && py === by) || (px === cx && py === cy)) continue;
        if (inTriangle(px, py, ax, ay, bx, by, cx, cy)) {
          inside = true;
          break;
        }
      }
      if (inside) continue;
      out.push(a, b, c);
      idx.splice(i, 1);
      clipped = true;
      break;
    }
    if (!clipped) {
      // No ear: a self-crossing ring. A fan from its first remaining vertex finishes it.
      for (let i = 1; i + 1 < idx.length; i += 1) out.push(idx[0], idx[i], idx[i + 1]);
      return out;
    }
  }
  if (idx.length === 3) out.push(idx[0], idx[1], idx[2]);
  return out;
}

// Whether (x, y) is inside a ring (even–odd rule, so a ring crossing itself counts as its
// outline encloses).
export function pointInRing(xy, start, end, x, y) {
  const n = openLength(xy, start, end);
  let inside = false;
  for (let i = 0, j = n - 1; i < n; j = i, i += 1) {
    const xi = xy[2 * (start + i)], yi = xy[2 * (start + i) + 1];
    const xj = xy[2 * (start + j)], yj = xy[2 * (start + j) + 1];
    if ((yi > y) !== (yj > y) && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) inside = !inside;
  }
  return inside;
}

// A tile's outlines as line segments: two vertices each (positions, and each vertex's polygon
// index in the tile), for gl.LINES.
export function outlineSegments(tile) {
  let segments = 0;
  const p = tile.cells.length;
  for (let i = 0; i < p; i += 1) {
    const n = openLength(tile.xy, tile.starts[i], tile.starts[i + 1]);
    if (n >= 2) segments += n;
  }
  const positions = new Float32Array(4 * segments);
  const polygon = new Float32Array(2 * segments);
  let k = 0;
  for (let i = 0; i < p; i += 1) {
    const start = tile.starts[i];
    const n = openLength(tile.xy, start, tile.starts[i + 1]);
    if (n < 2) continue;
    for (let v = 0; v < n; v += 1) {
      const a = start + v;
      const b = start + ((v + 1) % n);
      positions.set([tile.xy[2 * a], tile.xy[2 * a + 1], tile.xy[2 * b], tile.xy[2 * b + 1]], 4 * k);
      polygon[2 * k] = i;
      polygon[2 * k + 1] = i;
      k += 1;
    }
  }
  return { positions, polygon, count: 2 * segments };
}

// A tile's polygons as triangles: positions and each vertex's polygon index in the tile.
export function fillTriangles(tile) {
  const indexes = [];
  const owner = [];
  for (let i = 0; i < tile.cells.length; i += 1) {
    const before = indexes.length;
    triangulate(tile.xy, tile.starts[i], tile.starts[i + 1], indexes);
    for (let k = before; k < indexes.length; k += 1) owner.push(i);
  }
  const positions = new Float32Array(2 * indexes.length);
  const polygon = new Float32Array(indexes.length);
  indexes.forEach((v, k) => {
    positions[2 * k] = tile.xy[2 * v];
    positions[2 * k + 1] = tile.xy[2 * v + 1];
    polygon[k] = owner[k];
  });
  return { positions, polygon, count: indexes.length };
}
