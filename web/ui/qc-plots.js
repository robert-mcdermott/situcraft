// The QC view's plots, drawn with Canvas 2D: a histogram, box plots by group and a scatter plot of
// the cells' attributes. Dragging across one (brushing) chooses the cells in that range, which the
// tissue then shows; the chosen cells are drawn in the accent color, and cells a filter removes
// are drawn faint.

import { h } from './dom.js';
import { binOf, boxStats, finiteSorted, histogram, histogramBins } from '../lib/plots.js';

function theme() {
  const css = getComputedStyle(document.documentElement);
  const v = (name, fallback) => css.getPropertyValue(name).trim() || fallback;
  return { text: v('--text-2', '#4b5468'), faint: v('--text-3', '#636c7e'), line: v('--line', '#e3e7ef'), bar: v('--line-strong', '#d0d6e2'), accent: v('--accent', '#5b4ce6'), bg: v('--panel', '#fff') };
}

// Round tick values across [lo, hi] (log10 ticks on a log axis).
function ticks(lo, hi, count, log) {
  if (log) {
    const out = [];
    for (let e = Math.floor(Math.log10(Math.max(lo, 1e-9))); 10 ** e <= hi * 1.0001; e += 1) if (10 ** e >= lo * 0.9999) out.push(10 ** e);
    return out.length >= 2 ? out : [lo, hi];
  }
  const step0 = (hi - lo) / Math.max(1, count);
  const mag = 10 ** Math.floor(Math.log10(step0 || 1));
  const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= step0) ?? 10 * mag;
  const out = [];
  for (let t = Math.ceil(lo / step) * step; t <= hi + step * 1e-9; t += step) out.push(Math.abs(t) < step * 1e-9 ? 0 : t);
  return out;
}

const fmt = (v) => {
  const a = Math.abs(v);
  if (a >= 1e4) return `${(v / 1e3).toFixed(0)}k`;
  if (a >= 100 || Number.isInteger(v)) return String(Math.round(v));
  return a >= 1 ? v.toFixed(1) : v.toPrecision(2);
};

// A canvas that redraws at its size and lets the user drag a horizontal range (axis 'x') or a
// rectangle ('xy'). draw(ctx, w, h) draws; brushed({ x0, x1, y0, y1 }) in CSS pixels.
function plotCanvas({ height = 190, axis = 'x', draw, brushed, label }) {
  const canvas = h('canvas.qc-plot', { role: 'img', 'aria-label': label });
  const wrap = h('div.qc-plot-wrap', canvas);
  let drag = null;
  const ctxSize = () => {
    const ratio = window.devicePixelRatio || 1;
    const w = Math.max(120, wrap.clientWidth);
    canvas.style.width = `${w}px`;
    canvas.style.height = `${height}px`;
    canvas.width = Math.round(w * ratio);
    canvas.height = Math.round(height * ratio);
    const ctx = canvas.getContext('2d');
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
    return [ctx, w];
  };
  const redraw = () => {
    const [ctx, w] = ctxSize();
    ctx.clearRect(0, 0, w, height);
    draw(ctx, w, height);
    if (drag?.moved) {
      ctx.fillStyle = 'rgba(91, 76, 230, 0.16)';
      ctx.strokeStyle = theme().accent;
      const x0 = Math.min(drag.x0, drag.x1);
      const x1 = Math.max(drag.x0, drag.x1);
      const y0 = axis === 'xy' ? Math.min(drag.y0, drag.y1) : 0;
      const y1 = axis === 'xy' ? Math.max(drag.y0, drag.y1) : height;
      ctx.fillRect(x0, y0, x1 - x0, y1 - y0);
      ctx.strokeRect(x0 + 0.5, y0 + 0.5, x1 - x0, y1 - y0);
    }
  };
  const point = (event) => {
    const r = canvas.getBoundingClientRect();
    return [event.clientX - r.left, event.clientY - r.top];
  };
  canvas.addEventListener('pointerdown', (event) => {
    if (!brushed) return;
    canvas.setPointerCapture(event.pointerId);
    const [x, y] = point(event);
    drag = { x0: x, y0: y, x1: x, y1: y, moved: false };
  });
  canvas.addEventListener('pointermove', (event) => {
    if (!drag) return;
    [drag.x1, drag.y1] = point(event);
    if (Math.abs(drag.x1 - drag.x0) > 3 || Math.abs(drag.y1 - drag.y0) > 3) drag.moved = true;
    redraw();
  });
  canvas.addEventListener('pointerup', () => {
    if (!drag) return;
    const d = drag;
    drag = null;
    redraw();
    // A click without a drag clears the choice.
    brushed(d.moved ? { x0: Math.min(d.x0, d.x1), x1: Math.max(d.x0, d.x1), y0: Math.min(d.y0, d.y1), y1: Math.max(d.y0, d.y1) } : null);
  });
  const observer = new ResizeObserver(() => redraw());
  observer.observe(wrap);
  return { el: wrap, redraw, dispose: () => observer.disconnect() };
}

const M = { left: 46, right: 10, top: 10, bottom: 30 };

// A histogram of values. options: { label, integer, log, keep (filter mask), selection (mask),
// onBrush(lo, hi) with lo > hi for none }.
export function histogramPlot(values, options) {
  const sorted = finiteSorted(values, options.keep);
  const lo = sorted.length ? sorted[0] : 0;
  // The top 0.5% of cells would squeeze the rest into a few bins.
  const hi = sorted.length ? sorted[Math.min(sorted.length - 1, Math.floor(0.995 * (sorted.length - 1)))] : 1;
  const bins = histogramBins(options.log ? Math.max(lo, 1) : lo, Math.max(hi, lo + 1), { bins: 40, integer: options.integer, log: options.log });
  const edges = bins.edges;
  let selection = options.selection;
  let counts;
  let marked;
  const recount = () => {
    ({ counts, marked } = histogram(values, edges, selection));
    if (options.keep) {
      // Bars count the kept cells; the selection among them.
      const keptValues = Float64Array.from(values, (v, i) => (options.keep[i] ? v : NaN));
      ({ counts, marked } = histogram(keptValues, edges, selection));
    }
  };
  recount();
  const x0 = edges[0];
  const x1 = edges[edges.length - 1];
  const sx = (v, w) => {
    const t = bins.log ? (Math.log10(v) - Math.log10(x0)) / (Math.log10(x1) - Math.log10(x0)) : (v - x0) / (x1 - x0);
    return M.left + t * (w - M.left - M.right);
  };
  const invert = (px, w) => {
    const t = Math.min(1, Math.max(0, (px - M.left) / (w - M.left - M.right)));
    return bins.log ? 10 ** (Math.log10(x0) + t * (Math.log10(x1) - Math.log10(x0))) : x0 + t * (x1 - x0);
  };
  let width = 300;
  const plot = plotCanvas({
    label: `Histogram of ${options.label}`,
    draw(ctx, w, height) {
      width = w;
      const t = theme();
      const max = Math.max(1, ...counts);
      const sy = (c) => height - M.bottom - (c / max) * (height - M.top - M.bottom);
      for (let k = 0; k < counts.length; k += 1) {
        const a = sx(edges[k], w);
        const b = sx(edges[k + 1], w);
        ctx.fillStyle = t.bar;
        ctx.fillRect(a + 0.5, sy(counts[k]), Math.max(1, b - a - 1), height - M.bottom - sy(counts[k]));
        if (marked[k]) {
          ctx.fillStyle = t.accent;
          ctx.fillRect(a + 0.5, sy(marked[k]), Math.max(1, b - a - 1), height - M.bottom - sy(marked[k]));
        }
      }
      axes(ctx, w, height, t, ticks(x0, x1, 5, bins.log).map((v) => [sx(v, w), fmt(v)]), ticks(0, max, 3, false).map((c) => [sy(c), fmt(c)]), options.label, 'cells');
    },
    brushed: options.onBrush ? (b) => (b ? options.onBrush(invert(b.x0, width), invert(b.x1, width)) : options.onBrush(1, 0)) : null,
  });
  return {
    el: plot.el,
    dispose: plot.dispose,
    // Clicking a bar's range: the bins' edges, for adding a filter condition.
    edges,
    binOf: (v) => binOf(edges, v),
    setSelection(mask) {
      selection = mask;
      recount();
      plot.redraw();
    },
  };
}

// Box plots of values by group. groups: [{ label, index (the group value), color }]; groupOf:
// per-cell group values. Brushing a vertical range chooses cells in it.
export function boxPlot(values, groupOf, groups, options) {
  let selection = options.selection;
  const stats = groups.map((g) => {
    const vals = Float64Array.from(values, (v, i) => (groupOf[i] === g.index && (!options.keep || options.keep[i]) ? v : NaN));
    return { ...g, box: boxStats(finiteSorted(vals)), values: vals };
  }).filter((g) => g.box.n > 0);
  const all = stats.flatMap((g) => [g.box.whiskerLo, g.box.whiskerHi]);
  const y0 = Math.min(...all, 0);
  const y1 = Math.max(...all, y0 + 1);
  let height = 190;
  const sy = (v) => height - M.bottom - ((v - y0) / (y1 - y0)) * (height - M.top - M.bottom);
  const invert = (py) => y0 + ((height - M.bottom - py) / (height - M.top - M.bottom)) * (y1 - y0);
  const plot = plotCanvas({
    axis: 'xy',
    label: `Box plots of ${options.label} by ${options.groupLabel}`,
    draw(ctx, w, hgt) {
      height = hgt;
      const t = theme();
      const band = (w - M.left - M.right) / Math.max(1, stats.length);
      stats.forEach((g, k) => {
        const cx = M.left + band * (k + 0.5);
        const half = Math.min(28, band * 0.3);
        const b = g.box;
        ctx.strokeStyle = t.text;
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(cx, sy(b.whiskerLo));
        ctx.lineTo(cx, sy(b.q1));
        ctx.moveTo(cx, sy(b.q3));
        ctx.lineTo(cx, sy(b.whiskerHi));
        ctx.stroke();
        ctx.fillStyle = g.color;
        ctx.globalAlpha = 0.35;
        ctx.fillRect(cx - half, sy(b.q3), 2 * half, sy(b.q1) - sy(b.q3));
        ctx.globalAlpha = 1;
        ctx.strokeRect(cx - half + 0.5, sy(b.q3) + 0.5, 2 * half, sy(b.q1) - sy(b.q3));
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.moveTo(cx - half, sy(b.median));
        ctx.lineTo(cx + half, sy(b.median));
        ctx.stroke();
        ctx.lineWidth = 1;
        // The chosen cells of the group, as a dot strip beside the box.
        if (selection) {
          ctx.fillStyle = t.accent;
          for (let i = 0; i < g.values.length; i += 1) {
            if (selection[i] && Number.isFinite(g.values[i]) && g.values[i] <= y1 && g.values[i] >= y0) ctx.fillRect(cx + half + 4, sy(g.values[i]) - 1, 6, 2);
          }
        }
        ctx.fillStyle = t.faint;
        ctx.font = '11px system-ui, sans-serif';
        ctx.textAlign = 'center';
        const name = g.label.length > 22 ? `${g.label.slice(0, 21)}…` : g.label;
        ctx.fillText(`${name} (${g.box.n})`, cx, hgt - 8);
      });
      axes(ctx, w, hgt, t, [], ticks(y0, y1, 4, false).map((v) => [sy(v), fmt(v)]), '', options.label);
    },
    brushed: options.onBrush ? (b) => (b ? options.onBrush(invert(b.y1), invert(b.y0)) : options.onBrush(1, 0)) : null,
  });
  return {
    el: plot.el,
    dispose: plot.dispose,
    setSelection(mask) {
      selection = mask;
      plot.redraw();
    },
  };
}

// A scatter plot of two attributes; brushing a rectangle chooses the cells in it.
export function scatterPlot(xs, ys, options) {
  let selection = options.selection;
  const xsSorted = finiteSorted(xs, options.keep);
  const ysSorted = finiteSorted(ys, options.keep);
  const pick = (s, q) => (s.length ? s[Math.min(s.length - 1, Math.floor(q * (s.length - 1)))] : 0);
  const x0 = Math.min(0, pick(xsSorted, 0));
  const x1 = Math.max(pick(xsSorted, 0.995), x0 + 1);
  const y0 = Math.min(0, pick(ysSorted, 0));
  const y1 = Math.max(pick(ysSorted, 0.995), y0 + 1);
  let size = [300, 220];
  const sx = (v) => M.left + ((v - x0) / (x1 - x0)) * (size[0] - M.left - M.right);
  const sy = (v) => size[1] - M.bottom - ((v - y0) / (y1 - y0)) * (size[1] - M.top - M.bottom);
  const ix = (px) => x0 + ((px - M.left) / (size[0] - M.left - M.right)) * (x1 - x0);
  const iy = (py) => y0 + ((size[1] - M.bottom - py) / (size[1] - M.top - M.bottom)) * (y1 - y0);
  const plot = plotCanvas({
    axis: 'xy',
    height: 220,
    label: `Scatter plot of ${options.yLabel} against ${options.xLabel}`,
    draw(ctx, w, hgt) {
      size = [w, hgt];
      const t = theme();
      const n = xs.length;
      // Many cells are drawn small and translucent; chosen cells last, on top.
      const r = n > 20000 ? 1 : 1.6;
      ctx.globalAlpha = n > 20000 ? 0.25 : 0.5;
      ctx.fillStyle = t.text;
      for (let i = 0; i < n; i += 1) {
        if (options.keep && !options.keep[i]) continue;
        if (selection && selection[i]) continue;
        const x = xs[i];
        const y = ys[i];
        if (!(x >= x0 && x <= x1 && y >= y0 && y <= y1)) continue;
        ctx.fillRect(sx(x) - r / 2, sy(y) - r / 2, r, r);
      }
      if (selection) {
        ctx.globalAlpha = 0.85;
        ctx.fillStyle = t.accent;
        for (let i = 0; i < n; i += 1) {
          if (!selection[i] || (options.keep && !options.keep[i])) continue;
          const x = xs[i];
          const y = ys[i];
          if (!(x >= x0 && x <= x1 && y >= y0 && y <= y1)) continue;
          ctx.fillRect(sx(x) - r, sy(y) - r, 2 * r, 2 * r);
        }
      }
      ctx.globalAlpha = 1;
      axes(ctx, w, hgt, t, ticks(x0, x1, 5, false).map((v) => [sx(v), fmt(v)]), ticks(y0, y1, 4, false).map((v) => [sy(v), fmt(v)]), options.xLabel, options.yLabel);
    },
    brushed: options.onBrush ? (b) => options.onBrush(b ? { x0: ix(b.x0), x1: ix(b.x1), y0: iy(b.y1), y1: iy(b.y0) } : null) : null,
  });
  return {
    el: plot.el,
    dispose: plot.dispose,
    setSelection(mask) {
      selection = mask;
      plot.redraw();
    },
  };
}

function axes(ctx, w, height, t, xticks, yticks, xLabel, yLabel) {
  ctx.strokeStyle = t.line;
  ctx.fillStyle = t.faint;
  ctx.font = '10.5px system-ui, sans-serif';
  ctx.lineWidth = 1;
  ctx.beginPath();
  ctx.moveTo(M.left + 0.5, M.top);
  ctx.lineTo(M.left + 0.5, height - M.bottom + 0.5);
  ctx.lineTo(w - M.right, height - M.bottom + 0.5);
  ctx.stroke();
  ctx.textAlign = 'center';
  for (const [x, text] of xticks) ctx.fillText(text, x, height - M.bottom + 13);
  ctx.textAlign = 'right';
  for (const [y, text] of yticks) ctx.fillText(text, M.left - 5, y + 3);
  if (xLabel) {
    ctx.textAlign = 'right';
    ctx.fillText(xLabel, w - M.right, height - 3);
  }
  if (yLabel) {
    ctx.save();
    ctx.translate(10, M.top);
    ctx.rotate(-Math.PI / 2);
    ctx.textAlign = 'right';
    ctx.fillText(yLabel, 0, 0);
    ctx.restore();
  }
}
