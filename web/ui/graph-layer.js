// A graph of cells drawn over the tissue (lib/graphs.js): each edge a line between two cells'
// centroids, on a canvas over the renderer's, redrawn from its camera after each frame. Edges are
// drawn once they can be read: the graph's median edge at least MIN_EDGE_CSS pixels long on screen,
// and no more than MAX_EDGES in view; farther out, the legend says to zoom in. The selected cell's
// edges are drawn brighter; an emphasis (the edges of a pair of labels, from a spatial analysis)
// draws its edges in its color and the others faint.

import { h } from './dom.js';
import { edgesIn } from '../lib/graphs.js';
import { visibleRect, worldToScreen } from '../lib/viewport.js';

// The most edges drawn in one view, and the shortest the median edge is drawn at (CSS pixels).
export const MAX_EDGES = 40000;
export const MIN_EDGE_CSS = 6;

// mountGraphLayer({ canvas: the renderer's canvas, renderer, onCount(shown) }) → { set(data),
// select(row), emphasize({ test(i, j), color } or null), afterFrame(), shown(), destroy() }.
export function mountGraphLayer({ canvas, renderer, onCount = () => {} }) {
  const overlay = h('canvas.viewer-graph', { 'aria-hidden': 'true' });
  canvas.after(overlay);
  let data = null; // { graph, grid, description }
  let selected = -1; // the selected cell's index among the graph's cells
  let emphasis = null; // { test(i, j): an edge emphasized (indices among the graph's cells), color }
  let last = { count: 0, tooMany: false };

  function draw() {
    const [w, hh] = renderer.size();
    if (overlay.width !== w || overlay.height !== hh) {
      overlay.width = w;
      overlay.height = hh;
    }
    const ctx = overlay.getContext('2d');
    ctx.clearRect(0, 0, w, hh);
    const before = last;
    last = { count: 0, tooMany: false };
    if (data) {
      const { graph, grid, description } = data;
      const cam = renderer.camera;
      const ratio = renderer.ratio || 1;
      const legible = ((description?.lengths?.median ?? 0) * cam.scale) / ratio >= MIN_EDGE_CSS;
      const edges = legible ? edgesIn(graph, grid, visibleRect(cam, w, hh), MAX_EDGES) : null;
      if (!edges) last = { count: 0, tooMany: true };
      else {
        const line = (list, style, width) => {
          if (!list.length) return;
          ctx.beginPath();
          for (let k = 0; k < list.length; k += 2) {
            const [ax, ay] = worldToScreen(cam, w, hh, graph.x[list[k]], graph.y[list[k]]);
            const [bx, by] = worldToScreen(cam, w, hh, graph.x[list[k + 1]], graph.y[list[k + 1]]);
            ctx.moveTo(ax, ay);
            ctx.lineTo(bx, by);
          }
          ctx.strokeStyle = style;
          // At least a pixel and a half: a line a device pixel wide falls across two at half
          // strength, and reads as gray on screens of one device pixel per CSS pixel.
          ctx.lineWidth = Math.max(1.5 * width, width * ratio);
          ctx.stroke();
        };
        const mine = [];
        const rest = [];
        const marked = [];
        for (let k = 0; k < edges.length; k += 2) {
          const i = edges[k];
          const j = edges[k + 1];
          (i === selected || j === selected ? mine : emphasis?.test(i, j) ? marked : rest).push(i, j);
        }
        // A dark casing under each line keeps it legible on bright fills and stains alike; with an
        // emphasis, the other edges are faint.
        line(rest, emphasis ? 'rgba(0, 0, 0, 0.25)' : 'rgba(0, 0, 0, 0.5)', 2.5);
        line(rest, emphasis ? 'rgba(255, 255, 255, 0.3)' : 'rgba(255, 255, 255, 0.85)', 1);
        line(marked, 'rgba(0, 0, 0, 0.6)', 4);
        line(marked, emphasis?.color ?? '#ff4fd8', 2);
        line(mine, 'rgba(0, 0, 0, 0.6)', 4);
        line(mine, 'rgb(255, 216, 77)', 2);
        last = { count: edges.length / 2, tooMany: false };
      }
    }
    if (before.count !== last.count || before.tooMany !== last.tooMany) onCount(last);
  }

  return {
    set(next) {
      data = next;
      selected = -1;
      emphasis = null;
      draw();
    },
    emphasize(next) {
      emphasis = next;
      draw();
    },
    select(row) {
      const g = data?.graph;
      selected = g && row >= 0 && row < g.indexOf.length ? g.indexOf[row] : -1;
      draw();
    },
    afterFrame: draw,
    shown: () => last,
    destroy() {
      overlay.remove();
    },
  };
}
