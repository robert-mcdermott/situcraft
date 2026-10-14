// A promise-based client for module workers that speak the siblings' protocol (MaveScape's):
// { id, type, payload } → { id, progress: [fraction, message] }* then { id, result } | { id, error }.

import { transferable } from '../lib/memory.js';

export class WorkerClient {
  // options: { max (workers at once), retire (end a worker once it has nothing to do: a million-row
  // table or run leaves hundreds of megabytes in a worker's heap, released only when it ends) }.
  constructor(url, options = {}) {
    this.url = url;
    this.max = options.max ?? 1;
    this.retire = options.retire ?? false;
    this.workers = [];
    this.pending = new Map();
    this.queue = [];
    this.nextId = 1;
  }

  spawn() {
    const worker = new Worker(new URL(this.url, import.meta.url), { type: 'module' });
    const slot = { worker, busy: false, jobId: null };
    worker.onmessage = (event) => this.receive(slot, event.data);
    worker.onerror = (event) => {
      const job = this.pending.get(slot.jobId);
      if (job) {
        this.pending.delete(slot.jobId);
        job.reject(new Error(event.message || 'The worker failed.'));
      }
      slot.busy = false;
      this.drain();
    };
    this.workers.push(slot);
    return slot;
  }

  receive(slot, message) {
    const job = this.pending.get(message.id);
    if (!job) return;
    if (message.progress) {
      job.onProgress?.(...message.progress);
      return;
    }
    this.pending.delete(message.id);
    slot.busy = false;
    slot.jobId = null;
    if (message.error) job.reject(new Error(message.error));
    else job.resolve(message.result);
    this.drain();
    if (this.retire && !slot.busy && !this.queue.length) {
      slot.worker.terminate();
      this.workers = this.workers.filter((w) => w !== slot);
    }
  }

  drain() {
    while (this.queue.length) {
      let slot = this.workers.find((w) => !w.busy);
      if (!slot && this.workers.length < this.max) slot = this.spawn();
      if (!slot) return;
      const job = this.queue.shift();
      if (job.canceled) continue;
      slot.busy = true;
      slot.jobId = job.id;
      job.slot = slot;
      try {
        slot.worker.postMessage({ id: job.id, type: job.type, payload: job.payload }, transferable(job.transfer));
      } catch (error) {
        // The browser could not hand the data over (too large to copy, or not transferable).
        this.pending.delete(job.id);
        slot.busy = false;
        slot.jobId = null;
        job.reject(new Error(error.name === 'DataCloneError' ? `The data could not be passed to the worker: ${error.message}` : error.message));
      }
    }
  }

  // Runs a request; returns { promise, cancel }. Canceling a running job terminates its worker.
  run(type, payload, options = {}) {
    const id = this.nextId++;
    let resolve;
    let reject;
    const promise = new Promise((res, rej) => {
      resolve = res;
      reject = rej;
    });
    const job = { id, type, payload, transfer: options.transfer, onProgress: options.onProgress, resolve, reject, canceled: false, slot: null };
    this.pending.set(id, job);
    this.queue.push(job);
    this.drain();
    const cancel = () => {
      if (!this.pending.has(id)) return;
      job.canceled = true;
      this.pending.delete(id);
      if (job.slot) {
        job.slot.worker.terminate();
        this.workers = this.workers.filter((w) => w !== job.slot);
      }
      reject(Object.assign(new Error('Canceled'), { canceled: true }));
      this.drain();
    };
    return { promise, cancel };
  }

  call(type, payload, options) {
    return this.run(type, payload, options).promise;
  }
}
