import { test } from 'node:test';
import assert from 'node:assert/strict';
import { deltaE2000, lab, paletteReport, simulate } from './colorvision.js';
import { CATEGORICAL, CATEGORICAL_CVD, categoricalColor, colorVisionFriendly, displayColor, displayColormap, setColorVisionFriendly, shownColor } from './colormaps.js';

test('CIEDE2000 matches the published test pairs (Sharma, Wu & Dalal 2005)', () => {
  const pairs = [
    [[50, 2.6772, -79.7751], [50, 0, -82.7485], 2.0425],
    [[50, 3.1571, -77.2803], [50, 0, -82.7485], 2.8615],
    [[50, 2.5, 0], [50, 0, -2.5], 4.3065],
    [[50, 2.5, 0], [73, 25, -18], 27.1492],
    [[50, 2.5, 0], [61, -5, 29], 22.8977],
    [[60.2574, -34.0099, 36.2677], [60.4626, -34.1751, 39.4387], 1.2644],
    [[22.7233, 20.0904, -46.694], [23.0331, 14.973, -42.5619], 2.0373],
  ];
  for (const [a, b, expected] of pairs) assert.ok(Math.abs(deltaE2000(a, b) - expected) < 1e-4, `${deltaE2000(a, b)} vs ${expected}`);
  // White and black in Lab.
  assert.ok(Math.abs(lab('#ffffff')[0] - 100) < 0.01 && Math.abs(lab('#000000')[0]) < 0.01);
});

test('simulated color-vision deficiencies: grays are unchanged, red and green merge for deutans', () => {
  for (const vision of ['protan', 'deutan', 'tritan']) {
    for (const gray of ['#000000', '#808080', '#ffffff']) assert.equal(simulate(gray, vision), gray);
  }
  assert.equal(simulate('#d62728', 'normal'), '#d62728');
  const red = lab(simulate('#e45563', 'deutan'));
  const green = lab(simulate('#3fb27f', 'deutan'));
  assert.ok(deltaE2000(lab('#e45563'), lab('#3fb27f')) > 40, 'distinct with normal vision');
  assert.ok(deltaE2000(red, green) < deltaE2000(lab('#e45563'), lab('#3fb27f')) / 2, 'much closer for a deutan');
  assert.throws(() => simulate('#ffffff', 'mono'));
});

test('the color-vision-friendly palette stays distinguishable where the default does not', () => {
  const friendly = paletteReport(CATEGORICAL_CVD, 8);
  const defaults = paletteReport(CATEGORICAL, 8);
  for (const vision of ['normal', 'protan', 'deutan', 'tritan']) assert.ok(friendly[vision].min >= 10, `${vision} ${friendly[vision].min}`);
  assert.ok(defaults.deutan.min < 2, 'the default palette has colors a deutan cannot tell apart');
  assert.ok(Object.values(paletteReport(CATEGORICAL_CVD)).every((r) => r.min >= 7));
});

test('the setting maps colors for display without changing what is stored', () => {
  const ws = { selections: [{ id: 'a', color: '#123456' }, { id: 'b', color: CATEGORICAL[3] }], conditions: [{ id: 'g', color: '#ff0000' }] };
  try {
    setColorVisionFriendly(false);
    assert.equal(categoricalColor(2), CATEGORICAL[2]);
    assert.equal(shownColor(ws, ws.selections[0]), '#123456');
    assert.equal(displayColor(CATEGORICAL_CVD[4]), CATEGORICAL[4], 'a color stored with the setting on shows as its default');
    assert.equal(displayColormap('classic'), 'classic');
    setColorVisionFriendly(true);
    assert.equal(colorVisionFriendly(), true);
    assert.equal(categoricalColor(2), CATEGORICAL_CVD[2]);
    // Every population in order, whatever color it has.
    assert.equal(shownColor(ws, ws.selections[0]), CATEGORICAL_CVD[0]);
    assert.equal(shownColor(ws, ws.selections[1]), CATEGORICAL_CVD[1]);
    assert.equal(shownColor(ws, ws.conditions[0], 'conditions'), CATEGORICAL_CVD[0]);
    assert.equal(displayColor('#123456'), '#123456', 'a color of neither palette');
    assert.equal(displayColormap('classic'), 'viridis');
    assert.equal(displayColormap('turbo'), 'viridis');
    assert.equal(displayColormap('magma'), 'magma');
    assert.equal(ws.selections[0].color, '#123456', 'nothing stored changes');
  } finally {
    setColorVisionFriendly(false);
  }
});
