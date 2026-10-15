// A guided tour or worked example (lib/tours.js): a card at the foot of the window stepping through
// views of a dataset. Each step opens its view through app.applyView, as a saved view opens,
// without keeping it in the project. A worked example's steps may also do something first (expand
// the nuclei, plant errors, compare two segmentations: the card's button runs it as a job and goes
// on when it is done), show a comparison in the Compare view, or reveal a planted segmentation's
// answer key. One tour at a time.

import { h, icon } from './dom.js';
import { segmentationByRole, stepView } from '../lib/tours.js';
import { comparisonsOf, segmentationsOf } from '../lib/segmentations.js';
import { toast } from './overlays.js';

let active = null;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

export function startTour(app, { tour, dataset }) {
  active?.close();
  let index = 0;
  let working = false;
  const count = h('span.tour-count');
  const title = h('h3');
  const text = h('p');
  const note = h('p.tour-note', { hidden: true });
  const back = h('button.btn.small', { type: 'button', onclick: () => go(index - 1) }, 'Back');
  const next = h('button.btn.small.primary', { type: 'button', onclick: () => advance() });
  const intro = tour.question
    ? h('div.tour-intro', h('p', h('b', 'The question: '), tour.question), h('p.muted', h('b', 'What to expect: '), tour.expect), tour.simulated ? h('p.muted', 'Its errors are simulated.') : null)
    : null;
  const card = h('section.tour-card', { role: 'dialog', 'aria-label': tour.title, 'aria-live': 'polite' },
    h('div.tour-head', icon(tour.question ? 'flask' : 'info'), h('b', tour.title), count, h('button.icon-button.small', { type: 'button', title: 'End the tour', 'aria-label': 'End the tour', onclick: () => close() }, icon('close'))),
    intro, title, text, note,
    h('div.btn-row.tour-actions', back, next));
  document.body.append(card);

  const datasetRecord = () => app.store.project.datasets.find((d) => d.id === dataset);
  const segmentations = () => segmentationsOf(app.store.project, dataset);
  const role = (r) => segmentationByRole(segmentations(), r);
  const comparison = (a, b) => {
    const sa = role(a);
    const sb = role(b);
    return sa && sb ? comparisonsOf(app.store.project, dataset).find((c) => c.a === sa.id && c.b === sb.id && c.state === 'complete') ?? null : null;
  };

  // Whether a step's action is done already.
  function done(step) {
    const d = step.do;
    if (!d) return true;
    if (d.action === 'expand') return Boolean(role(`expansion-${d.distance}`));
    if (d.action === 'plant') return Boolean(role('planted'));
    if (d.action === 'compare') return Boolean(comparison(d.a, d.b));
    if (d.action === 'refine') return Boolean(role('refined'));
    if (d.action === 'import-truth') return Boolean(role('truth'));
    return true;
  }

  // Runs a step's action in the Segmentation view, which shows its progress.
  async function run(step) {
    const d = step.do;
    const ds = datasetRecord();
    for (let k = 0; k < 200 && !app.segmentationView; k += 1) await sleep(25);
    if (!app.segmentationView) throw new Error('the Segmentation view did not open');
    if (d.action === 'expand') return app.segmentationView.expand(ds, d.distance);
    if (d.action === 'plant') return app.segmentationView.plant(ds, {});
    if (d.action === 'refine') return app.segmentationView.refine(ds, { preset: d.preset ?? 'balanced' });
    if (d.action === 'import-truth') {
      // The truth folder beside the bundle's files, in the path's own separators.
      const sep = ds.path.includes('\\') && !ds.path.includes('/') ? '\\' : '/';
      return app.segmentationView.importSegmentation(ds, { format: 'xenium', paths: [`${ds.path.replace(/[\\/]+$/, '')}${sep}truth`] });
    }
    if (d.action === 'compare') {
      const a = role(d.a);
      const b = role(d.b);
      if (!a || !b) throw new Error('do the steps before this one first: one of the two segmentations is not made yet');
      return app.segmentationView.compare(ds, a.id, b.id);
    }
    return null;
  }

  async function advance() {
    if (working) return;
    const step = tour.steps[index];
    if (!done(step)) {
      working = true;
      next.disabled = true;
      back.disabled = true;
      note.hidden = false;
      note.textContent = 'Working… the job\'s progress is shown in the view.';
      try {
        await run(step);
      } catch (error) {
        toast(`${step.title}: ${error.message}`, { kind: 'error', timeout: 10000 });
        working = false;
        go(index);
        return;
      }
      working = false;
    }
    if (index + 1 < tour.steps.length) go(index + 1);
    else close();
  }

  // Shows a step's comparison in the Compare view as the step says.
  async function showComparison(step) {
    const c = step.compare;
    const cmp = comparison(c.a, c.b);
    if (!cmp) {
      note.hidden = false;
      note.textContent = 'The comparison is not made yet: go back to the steps that make it.';
      return null;
    }
    app.compareFocus = cmp.id;
    app.setMode('compare');
    for (let k = 0; k < 200 && !app.compareView; k += 1) await sleep(25);
    const w = await app.compareView?.open(cmp.id);
    if (!w || active !== controller || tour.steps[index] !== step) return w;
    if (c.view) w.setMode(c.view);
    w.setTab('queue');
    const queue = w.setRules({ classes: c.classes ?? [...w.session.classes], by: c.rank, map: c.map });
    if (c.difference && queue.length >= c.difference) w.select(queue[c.difference - 1].index);
    if (c.tab && c.tab !== 'queue') w.setTab(c.tab);
    return w;
  }

  function go(i) {
    if (i < 0 || i >= tour.steps.length) return;
    index = i;
    const step = tour.steps[i];
    count.textContent = `${i + 1} of ${tour.steps.length}`;
    title.textContent = step.title;
    text.textContent = step.text;
    note.hidden = true;
    if (intro) intro.hidden = i !== 0;
    back.disabled = i === 0;
    next.disabled = false;
    const last = i + 1 === tour.steps.length;
    next.textContent = !done(step) ? step.label : last ? 'Finish' : 'Next';
    if (step.do && done(step)) {
      note.hidden = false;
      note.textContent = 'Done already: it is kept in the project.';
    }
    if (!datasetRecord()) {
      close();
      return;
    }
    card.classList.toggle('aside', Boolean(step.compare));
    if (step.compare) {
      app.focusItem({ kind: 'dataset', id: dataset });
      controller.shown = showComparison(step).catch((error) => {
        note.hidden = false;
        note.textContent = `The comparison could not be shown: ${error.message}`;
      });
    } else {
      const view = stepView(step, dataset, i);
      // A step's segmentation, by what it is.
      if (view && step.segmentation) {
        const s = role(step.segmentation);
        view.segmentation = s && s.source !== 'instrument' ? s.id : 'instrument';
      }
      if (view) app.applyView(view);
      else {
        app.focusItem({ kind: 'dataset', id: dataset });
        app.setMode(step.mode);
      }
      controller.shown = step.reveal ? (async () => {
        for (let k = 0; k < 200 && !app.segmentationView; k += 1) await sleep(25);
        app.segmentationView?.revealAnswers?.(dataset);
      })() : Promise.resolve();
    }
    next.focus();
  }

  function close() {
    card.remove();
    if (active === controller) active = null;
  }

  const controller = { close, go, advance, step: () => index, card, tour, dataset, open: () => card.isConnected, done: () => done(tour.steps[index]), shown: Promise.resolve() };
  active = controller;
  app.tour = controller;
  go(0);
  return controller;
}
