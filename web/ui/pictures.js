// Pictures for agents and scripts (wave 9, slice 2): what was asked to be seen, drawn as a figure's
// panel is (ui/figures.js) on a page made for the moment and never kept, and given as an image's
// data URL, which the MCP server hands an agent as image content (mcp.go) and a script reads as it
// is: the tissue as the Viewer shows it, a clustering's map, a figure.

import { createFigure } from '../lib/figures.js';
import { drawFigure, paintFigure } from './figures.js';

// The page the picture's panel fills: 480 points wide, its text 7 pt.
const PAGE_WIDTH = 480;

// A picture's width in pixels, from an action's picture_width (1,000 if not given; 300 to 2,000).
export function pictureWidth(args) {
  const width = args.picture_width === undefined ? 1000 : Number(args.picture_width);
  if (!Number.isFinite(width) || width < 300 || width > 2000) throw new Error('picture_width is 300 to 2,000 pixels.');
  return Math.round(width);
}

// One panel filling a page made for it, painted width pixels wide: { caption, image (a data URL),
// width, height, notes }. A tissue is a JPEG (its stains photographs), anything else a PNG.
export async function panelPicture(app, panel, { width = 1000, aspect = 0.75, caption = '' } = {}) {
  const fig = createFigure('Picture', 'slide', { id: 'fig-picture', width: PAGE_WIDTH, height: Math.round(PAGE_WIDTH * aspect), letters: false, text: 7 });
  fig.panels = [{ id: 'picture', ...panel, x: 0, y: 0, w: fig.width, h: fig.height }];
  const scale = width / fig.width;
  const { drawn, resolved } = await drawFigure(app, fig, { dpi: Math.round(72 * scale) });
  const missing = resolved.get('picture')?.missing;
  if (missing) throw new Error(missing);
  const canvas = paintFigure(document.createElement('canvas'), fig, drawn, scale);
  const image = panel.kind === 'tissue' ? canvas.toDataURL('image/jpeg', 0.85) : canvas.toDataURL('image/png');
  return { caption, image, width: canvas.width, height: canvas.height, notes: drawn.notes };
}

// The tissue as the Viewer shows it: its view (captured from the Viewer, never saved) over the
// stage's extent, the stage's shape kept (between 1:2 and 6:5). A page not shown has no stage: the
// extent the Viewer then fits the tissue to (1600 by 1000).
export function tissuePicture(app, view, stage, { width = 1000, caption = '' } = {}) {
  const [w, h] = stage?.[0] > 0 && stage?.[1] > 0 ? stage : [1600, 1000];
  const camera = view.camera;
  if (!camera) throw new Error('The Viewer shows no tissue to picture yet.');
  const panel = { kind: 'tissue', view: view.id, inlineView: view, region: { cx: camera.cx, cy: camera.cy, width: Number((camera.umPerCss * w).toPrecision(6)) }, regions: true };
  return panelPicture(app, panel, { width, aspect: Math.min(1.2, Math.max(0.5, h / Math.max(1, w))), caption });
}

// A figure of the project as it draws now, width pixels wide (a PNG): { caption, image, width,
// height, notes }. drawn: drawFigure's, when it was drawn already.
export async function figurePicture(app, fig, { width = 1000, drawn = null } = {}) {
  const marks = drawn ?? (await drawFigure(app, fig, { dpi: Math.round((72 * width) / fig.width) })).drawn;
  const canvas = paintFigure(document.createElement('canvas'), fig, marks, width / fig.width);
  return { caption: fig.name, image: canvas.toDataURL('image/png'), width: canvas.width, height: canvas.height, notes: marks.notes };
}
