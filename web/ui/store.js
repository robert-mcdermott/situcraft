// Application state: the project (with undo/redo history) and UI state, with change topics so
// components re-render only what changed. Adapted from MaveScape 0.8.0 web/ui/store.js.

const MAX_HISTORY = 300;

// Work that started in one project and finished after another was opened (a job, an agent's
// action, a bundle still being read) must not write into the project now open.
export class ProjectChangedError extends Error {
  constructor(message = 'Another project was opened while this was running, so its result was not added.') {
    super(message);
    this.name = 'ProjectChangedError';
  }
}

export function createStore(initialProject) {
  const listeners = new Set();
  const state = {
    project: initialProject,
    past: [],
    future: [],
    labels: { past: [], future: [] },
    // Who is changing the project while a program's action runs (an agent, a script): their name
    // goes with each change's label in the history.
    actor: null,
    ui: {
      mode: 'welcome',
      // The item of the project tree in focus: { kind: 'dataset' | 'artifact' | 'view' | 'local', id }.
      focus: null,
      // The selected cells (and later transcripts or regions), shared by every view.
      selection: null,
      theme: 'system',
      drawer: { open: false, tab: 'history' },
      expanded: new Set(),
    },
    saved: initialProject,
    busy: new Map(),
    // Counts projects loaded (reset): views keep state for one project and start afresh when it
    // changes, even if they were not shown when it did.
    generation: 0,
  };

  let pending = new Set();
  let scheduled = false;
  const notify = (topics) => {
    for (const topic of topics) pending.add(topic);
    if (scheduled) return;
    scheduled = true;
    queueMicrotask(() => {
      scheduled = false;
      const topics = pending;
      pending = new Set();
      for (const listener of listeners) {
        try {
          listener(topics, state);
        } catch (error) {
          console.error(error);
        }
      }
    });
  };

  return {
    state,
    get project() {
      return state.project;
    },
    get ui() {
      return state.ui;
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    notify,
    // Replaces the project with an edited one, recording it for undo.
    commit(next, label = 'Edit', topics = ['project']) {
      if (!next || next === state.project) return;
      // A project built from another one (captured before that one was replaced).
      if (next.id !== state.project.id) throw new ProjectChangedError();
      state.past.push(state.project);
      state.labels.past.push(state.actor ? `${label} (by ${state.actor})` : label);
      if (state.past.length > MAX_HISTORY) {
        state.past.shift();
        state.labels.past.shift();
      }
      state.future = [];
      state.labels.future = [];
      state.project = next;
      notify(['project', 'history', ...topics]);
    },
    // Replaces the project without an undo step (loading, background results).
    replace(next, topics = ['project']) {
      if (next.id !== state.project.id) throw new ProjectChangedError();
      state.project = next;
      notify(['project', ...topics]);
    },
    // A check that the project open now is still open later: () => true until another is loaded.
    sameProject() {
      const generation = state.generation;
      return () => state.generation === generation;
    },
    reset(next) {
      state.generation += 1;
      state.project = next;
      state.past = [];
      state.future = [];
      state.labels = { past: [], future: [] };
      state.saved = next;
      notify(['project', 'history', 'project-loaded']);
    },
    // Names who makes the changes that follow (null: the person at the window).
    setActor(name) {
      state.actor = name || null;
    },
    canUndo: () => state.past.length > 0,
    canRedo: () => state.future.length > 0,
    undoLabel: () => state.labels.past[state.labels.past.length - 1] ?? '',
    redoLabel: () => state.labels.future[state.labels.future.length - 1] ?? '',
    undo() {
      if (!state.past.length) return null;
      state.future.push(state.project);
      const label = state.labels.past.pop();
      state.labels.future.push(label);
      state.project = state.past.pop();
      notify(['project', 'history']);
      return label;
    },
    redo() {
      if (!state.future.length) return null;
      state.past.push(state.project);
      const label = state.labels.future.pop();
      state.labels.past.push(label);
      state.project = state.future.pop();
      notify(['project', 'history']);
      return label;
    },
    setUI(patch, topics = ['ui']) {
      Object.assign(state.ui, patch);
      notify(topics);
    },
    markSaved(project = state.project) {
      state.saved = project;
      notify(['saved']);
    },
    isDirty: () => state.saved !== state.project,
    setBusy(key, message) {
      if (message) state.busy.set(key, message);
      else state.busy.delete(key);
      notify(['busy']);
    },
  };
}
