// The renderer's tile cache: tiles fetched from the program, nearest the centre of the view first,
// a few at a time; tiles no longer wanted are not fetched (or stop being fetched); loaded tiles
// are kept up to a budget of bytes, least recently drawn removed first. A tile's GPU resources are
// released with it.

export class TileCache {
  // load(key, signal) → Promise<{ value, bytes }>; dispose(value) releases a tile's resources.
  constructor({ load, dispose, budget = 512 * 2 ** 20, concurrency = 8, onLoad }) {
    this.load = load;
    this.dispose = dispose;
    this.budget = budget;
    this.concurrency = concurrency;
    this.onLoad = onLoad;
    this.entries = new Map(); // key → { state, value, bytes, used, priority, controller, error }
    this.queue = [];
    this.running = 0;
    this.bytes = 0;
    this.frame = 0;
    this.wanted = new Set();
    this.waiters = [];
  }

  // Starts a frame: tiles wanted in it are named by want().
  begin() {
    this.frame += 1;
    this.wanted = new Set();
  }

  // The loaded value of a tile, asking for it (with a priority, lower first) when it is not
  // loaded; undefined until it is.
  want(key, priority = 0) {
    this.wanted.add(key);
    let e = this.entries.get(key);
    if (!e) {
      e = { state: 'queued', priority, used: this.frame };
      this.entries.set(key, e);
      this.queue.push(key);
    }
    e.used = this.frame;
    e.priority = priority;
    return e.state === 'ready' ? e.value : undefined;
  }

  // A loaded tile, without asking for it (for fallbacks).
  peek(key) {
    const e = this.entries.get(key);
    if (e?.state !== 'ready') return undefined;
    e.used = this.frame;
    return e.value;
  }

  // Ends a frame: drops queued tiles no longer wanted, aborts their fetches, starts fetching the
  // most urgent, and keeps within the budget.
  end() {
    this.queue = this.queue.filter((key) => {
      if (this.wanted.has(key)) return true;
      const e = this.entries.get(key);
      if (e?.state === 'queued') this.entries.delete(key);
      return false;
    });
    for (const [key, e] of this.entries) {
      if (e.state === 'loading' && !this.wanted.has(key)) e.controller.abort();
    }
    this.queue.sort((a, b) => this.entries.get(a).priority - this.entries.get(b).priority);
    this.pump();
    this.trim();
    this.settle();
  }

  pump() {
    while (this.running < this.concurrency && this.queue.length) {
      const key = this.queue.shift();
      const e = this.entries.get(key);
      if (!e || e.state !== 'queued') continue;
      e.state = 'loading';
      e.controller = new AbortController();
      this.running += 1;
      this.load(key, e.controller.signal).then(
        ({ value, bytes }) => {
          if (this.entries.get(key) !== e) {
            this.dispose(value);
            return;
          }
          e.state = 'ready';
          e.value = value;
          e.bytes = bytes;
          this.bytes += bytes;
        },
        (error) => {
          if (this.entries.get(key) !== e) return;
          if (error?.name === 'AbortError') this.entries.delete(key);
          else {
            e.state = 'error';
            e.error = error;
          }
        },
      ).finally(() => {
        this.running -= 1;
        this.pump();
        this.onLoad?.(key);
        this.settle();
      });
    }
  }

  trim() {
    if (this.bytes <= this.budget) return;
    const ready = [...this.entries].filter(([key, e]) => e.state === 'ready' && !this.wanted.has(key));
    ready.sort((a, b) => a[1].used - b[1].used);
    for (const [key, e] of ready) {
      if (this.bytes <= this.budget) break;
      this.entries.delete(key);
      this.bytes -= e.bytes;
      this.dispose(e.value);
    }
  }

  // Whether every wanted tile is loaded (or failed).
  idle() {
    for (const key of this.wanted) {
      const e = this.entries.get(key);
      if (!e || e.state === 'queued' || e.state === 'loading') return false;
    }
    return true;
  }

  pending() {
    let n = 0;
    for (const key of this.wanted) {
      const e = this.entries.get(key);
      if (!e || e.state === 'queued' || e.state === 'loading') n += 1;
    }
    return n;
  }

  // Resolves when every wanted tile is loaded.
  whenIdle() {
    if (this.idle()) return Promise.resolve();
    return new Promise((resolve) => this.waiters.push(resolve));
  }

  settle() {
    if (!this.waiters.length || !this.idle()) return;
    const waiters = this.waiters;
    this.waiters = [];
    for (const resolve of waiters) resolve();
  }

  // Drops every tile (when the data they come from change).
  clear(match = () => true) {
    for (const [key, e] of [...this.entries]) {
      if (!match(key)) continue;
      if (e.state === 'loading') e.controller.abort();
      if (e.state === 'ready') {
        this.bytes -= e.bytes;
        this.dispose(e.value);
      }
      this.entries.delete(key);
    }
    this.queue = this.queue.filter((key) => this.entries.has(key));
  }

  errors() {
    let n = 0;
    for (const e of this.entries.values()) if (e.state === 'error') n += 1;
    return n;
  }
}
