// Large columns in memory that workers can share (a million cells' attributes and expression). When the
// page is cross-origin isolated (the SituCraft program sends the headers that allow it), columns
// are allocated on SharedArrayBuffers: a worker given a column then reads the same memory instead
// of a copy, so analyses of large tables do not double the memory they use (and do not fail to
// start: a browser refuses to copy more than about a gigabyte to a worker). Elsewhere columns are
// ordinary arrays. Adapted from MaveScape 0.8.0 web/lib/memory.js.
//
// Shared memory cannot be transferred, hashed (crypto.subtle), stored (Blob, fetch) or decoded as
// text directly: code that does so copies it first (plainCopy).

export function canShare() {
  return typeof SharedArrayBuffer === 'function' && globalThis.crossOriginIsolated === true;
}

// A Float32Array of n values, on shared memory when it can be.
export function float32(n) {
  return canShare() ? new Float32Array(new SharedArrayBuffer(n * 4)) : new Float32Array(n);
}

export function isShared(array) {
  return typeof SharedArrayBuffer === 'function' && array?.buffer instanceof SharedArrayBuffer;
}

// The bytes of a typed array on an ordinary ArrayBuffer of their own.
export function plainCopy(array) {
  const out = new Uint8Array(array.byteLength);
  out.set(new Uint8Array(array.buffer, array.byteOffset, array.byteLength));
  return out;
}

// A transfer list without shared buffers (which are shared, not transferred).
export function transferable(buffers = []) {
  return buffers.filter((buffer) => !(typeof SharedArrayBuffer === 'function' && buffer instanceof SharedArrayBuffer));
}
