// Panning and zooming a tissue canvas (render/renderer.js): dragging pans, the wheel (or a pinch)
// zooms about the pointer, a double-click zooms in, and a click without a drag reports where it
// was. Shared by the Viewer and the QC view.

import { screenToWorld, zoomAt } from '../lib/viewport.js';

// options: { onClick(sx, sy) in device pixels, onMove(x, y) in µm or null when the pointer
// leaves, limits() → { minScale, maxScale } }. Returns { zoomBy(factor, sx, sy), dispose() }.
export function attachNavigation(canvas, renderer, options = {}) {
  let drag = null;
  const limits = () => options.limits?.() ?? { minScale: (renderer.ratio || 1) / 400, maxScale: (renderer.ratio || 1) * 60 };
  const devicePoint = (event) => {
    const r = canvas.getBoundingClientRect();
    const ratio = renderer.ratio || 1;
    return [(event.clientX - r.left) * ratio, (event.clientY - r.top) * ratio];
  };
  function zoomBy(factor, sx, sy) {
    const [w, h] = renderer.size();
    renderer.setCamera(zoomAt(renderer.camera, w, h, sx ?? w / 2, sy ?? h / 2, factor, limits()));
  }
  const down = (event) => {
    if (event.button !== 0) return;
    canvas.setPointerCapture(event.pointerId);
    const [x, y] = devicePoint(event);
    drag = { x, y, camera: renderer.camera, moved: false };
  };
  const move = (event) => {
    const [x, y] = devicePoint(event);
    if (options.onMove) {
      const [w, h] = renderer.size();
      options.onMove(...screenToWorld(renderer.camera, w, h, x, y));
    }
    if (!drag) return;
    const dx = x - drag.x;
    const dy = y - drag.y;
    if (Math.hypot(dx, dy) > 3) drag.moved = true;
    if (drag.moved) renderer.setCamera({ ...drag.camera, cx: drag.camera.cx - dx / drag.camera.scale, cy: drag.camera.cy - dy / drag.camera.scale });
  };
  const up = (event) => {
    if (!drag) return;
    const moved = drag.moved;
    drag = null;
    if (!moved) options.onClick?.(...devicePoint(event));
  };
  const cancel = () => {
    drag = null;
  };
  const leave = () => options.onMove?.(null, null);
  const wheel = (event) => {
    event.preventDefault();
    const [x, y] = devicePoint(event);
    // A pinch on a trackpad comes as a wheel with ctrl held, in smaller steps.
    const speed = event.ctrlKey ? 0.01 : 0.0018;
    const delta = event.deltaMode === 1 ? event.deltaY * 16 : event.deltaY;
    zoomBy(Math.exp(-delta * speed), x, y);
  };
  const dbl = (event) => zoomBy(2, ...devicePoint(event));
  canvas.addEventListener('pointerdown', down);
  canvas.addEventListener('pointermove', move);
  canvas.addEventListener('pointerup', up);
  canvas.addEventListener('pointercancel', cancel);
  canvas.addEventListener('pointerleave', leave);
  canvas.addEventListener('wheel', wheel, { passive: false });
  canvas.addEventListener('dblclick', dbl);
  return {
    zoomBy,
    dispose() {
      canvas.removeEventListener('pointerdown', down);
      canvas.removeEventListener('pointermove', move);
      canvas.removeEventListener('pointerup', up);
      canvas.removeEventListener('pointercancel', cancel);
      canvas.removeEventListener('pointerleave', leave);
      canvas.removeEventListener('wheel', wheel);
      canvas.removeEventListener('dblclick', dbl);
    },
  };
}
