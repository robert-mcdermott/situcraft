// The map of the scale test's tissue drawn in headless Chrome (validation/scale_test.go): a SituCraft
// already running on the tissue, whose clustering and map are made, is opened in a page; the
// dataset is added, the clustering shown (its map fetched and every cell drawn, beside the tissue's
// outlines), and the map colored again by cluster. Prints one line of JSON: the cells drawn, the
// seconds to show the map, and the milliseconds to color it again.
//
// Usage: node validation/scale-map.mjs <url> <token> <bundle folder>

import { launch } from './cdp.mjs';

const [url, token, bundle] = process.argv.slice(2);
const base = url.endsWith('/') ? url : `${url}/`;

async function act(action, args = {}) {
  const response = await fetch(`${base}api/remote/action`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-SituCraft-Token': token }, body: JSON.stringify({ action, args, client: 'validation/scale-map' }) });
  const result = await response.json();
  if (!result.ok) throw new Error(`${action}: ${result.message ?? result.error}`);
  return result;
}

const args = process.env.SITUCRAFT_CHROME_ARGS ? process.env.SITUCRAFT_CHROME_ARGS.split(' ') : [];
const b = await launch({ width: 1600, height: 1000, args });
try {
  await b.goto(base, 3000);
  await act('add_dataset', { bundle });
  await act('cluster_cells', {});
  const shown = Date.now();
  await act('show_map', {});
  const seconds = (Date.now() - shown) / 1000;
  const drawn = await b.eval(`(async () => {
    const map = window.situcraft.cellTypesView.map();
    await map.settled();
    const t0 = performance.now();
    await map.setColor({ kind: 'clusters', clustering: map.state.clustersOf });
    await map.settled();
    return { cells: map.state.map?.n ?? 0, recolor: performance.now() - t0 };
  })()`);
  console.log(JSON.stringify({ cells: drawn.cells, seconds, recolorMs: Math.round(drawn.recolor) }));
} finally {
  await b.close();
}
