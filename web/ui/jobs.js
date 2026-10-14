// The program's jobs (internal/jobs, api_jobs.go), followed over server-sent events: the program
// sends the whole list whenever a job changes, so a page that reloads finds running jobs again and
// nothing polls. Views read app.jobs and redraw on the 'jobs' topic.

const FINISHED = new Set(['complete', 'cancelled', 'failed', 'invalid', 'orphaned']);

export function isFinished(state) {
  return FINISHED.has(state);
}

export function createJobs(app) {
  let list = [];
  const finishedListeners = new Set();
  // Jobs whose end this page has handled, and when it loaded: a job that started and finished
  // between two events, after the page loaded, is reported too; jobs that had finished before it
  // loaded are not.
  const reported = new Set();
  const loaded = Date.now() - 2000;

  function receive(next) {
    const before = new Map(list.map((job) => [job.id, job]));
    list = Array.isArray(next) ? next : [];
    for (const job of list) {
      const old = before.get(job.id);
      if (!old && !isFinished(job.state)) app.log?.(`Started: ${job.title}.`);
      if (isFinished(job.state) && !reported.has(job.id)) {
        reported.add(job.id);
        const seenRunning = old && !isFinished(old.state);
        if (!seenRunning && !(Date.parse(job.created) >= loaded)) continue;
        app.log?.(`${job.title}: ${job.state}${job.error ? ` (${job.error})` : ''}.`);
        for (const listener of finishedListeners) {
          try {
            listener(job);
          } catch (error) {
            console.error(error);
          }
        }
      }
    }
    app.store.notify(['jobs']);
  }

  if (typeof EventSource !== 'undefined') {
    // EventSource reconnects by itself if the connection drops (the first event after a
    // reconnection is the full list again).
    const source = new EventSource('/api/jobs/events');
    source.addEventListener('jobs', (event) => {
      try {
        receive(JSON.parse(event.data));
      } catch (error) {
        console.error('jobs event', error);
      }
    });
  }

  return {
    list: () => list,
    get: (id) => list.find((job) => job.id === id) ?? null,
    // Unfinished jobs, oldest first.
    running: () => list.filter((job) => !isFinished(job.state)),
    // The unfinished job with this key, if there is one.
    forKey: (key) => list.find((job) => job.key === key && !isFinished(job.state)) ?? null,
    async cancel(id) {
      const response = await fetch(`/api/jobs/${encodeURIComponent(id)}/cancel`, { method: 'POST' });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(body.error ?? `HTTP ${response.status}`);
      return body.cancelled;
    },
    // Calls listener(job) whenever a job this page saw running finishes.
    onFinished(listener) {
      finishedListeners.add(listener);
      return () => finishedListeners.delete(listener);
    },
  };
}
