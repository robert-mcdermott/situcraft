// A guided tour (lib/tours.js): a card at the foot of the window stepping through views of a
// dataset. Each step opens its view through app.applyView, as a saved view opens, without keeping
// it in the project. One tour at a time.

import { h, icon } from './dom.js';
import { stepView } from '../lib/tours.js';

let active = null;

export function startTour(app, { tour, dataset }) {
  active?.close();
  let index = 0;
  const count = h('span.tour-count');
  const title = h('h3');
  const text = h('p');
  const back = h('button.btn.small', { type: 'button', onclick: () => go(index - 1) }, 'Back');
  const next = h('button.btn.small.primary', { type: 'button', onclick: () => (index + 1 < tour.steps.length ? go(index + 1) : close()) });
  const card = h('section.tour-card', { role: 'dialog', 'aria-label': tour.title, 'aria-live': 'polite' },
    h('div.tour-head', icon('info'), h('b', tour.title), count, h('button.icon-button.small', { type: 'button', title: 'End the tour', 'aria-label': 'End the tour', onclick: () => close() }, icon('close'))),
    title, text,
    h('div.btn-row.tour-actions', back, next));
  document.body.append(card);

  function go(i) {
    if (i < 0 || i >= tour.steps.length) return;
    index = i;
    const step = tour.steps[i];
    count.textContent = `${i + 1} of ${tour.steps.length}`;
    title.textContent = step.title;
    text.textContent = step.text;
    back.disabled = i === 0;
    next.textContent = i + 1 < tour.steps.length ? 'Next' : 'Finish';
    if (!app.store.project.datasets.some((d) => d.id === dataset)) {
      close();
      return;
    }
    const view = stepView(step, dataset, i);
    if (view) app.applyView(view);
    else {
      app.focusItem({ kind: 'dataset', id: dataset });
      app.setMode(step.mode);
    }
    next.focus();
  }

  function close() {
    card.remove();
    if (active === controller) active = null;
  }

  const controller = { close, go, step: () => index, card, tour, dataset, open: () => card.isConnected };
  active = controller;
  app.tour = controller;
  go(0);
  return controller;
}
