// The project library, kept by the SituCraft program in its data folder (served at /api/library):
// projects as JSON documents, small files stored once each by SHA-256, and records kept across
// projects by kind (marker sets, presets): listRecords(kind), getRecord(kind, id),
// putRecord(kind, id, doc), deleteRecord(kind, id). Unlike its siblings' libraries, it has no
// browser-storage fallback: projects refer to bundles by their place on this computer, so
// SituCraft's page needs its program. Adapted from MaveScape 0.8.0 web/ui/storage.js.

// The program's description of itself (/api/info), or null when the page is not served by it.
export async function detectBackend() {
  try {
    const response = await fetch('api/info', { cache: 'no-store' });
    if (!response.ok) return null;
    const info = await response.json();
    return info?.name === 'SituCraft' ? info : null;
  } catch {
    return null;
  }
}

export async function sha256Hex(bytes) {
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
}

async function failure(response, fallback) {
  return new Error((await response.json().catch(() => ({}))).error ?? fallback);
}

export function createLibrary(info) {
  const path = (...parts) => ['api', 'library', ...parts.map(encodeURIComponent)].join('/');
  return {
    location: info.dataDir,

    async listProjects() {
      const response = await fetch(path('projects'), { cache: 'no-store' });
      if (!response.ok) throw await failure(response, 'Could not list the projects.');
      return (await response.json()).projects;
    },

    async loadProject(id) {
      const response = await fetch(path('projects', id), { cache: 'no-store' });
      if (!response.ok) throw await failure(response, 'The project could not be opened.');
      return response.text();
    },

    async saveProject(id, text) {
      const response = await fetch(path('projects', id), { method: 'PUT', body: text, headers: { 'Content-Type': 'application/json' } });
      if (!response.ok) throw await failure(response, 'The project could not be saved.');
    },

    async deleteProject(id) {
      const response = await fetch(path('projects', id), { method: 'DELETE' });
      if (!response.ok) throw await failure(response, 'The project could not be deleted.');
    },

    async hasFile(sha) {
      const response = await fetch(path('has', sha), { cache: 'no-store' });
      return response.ok && (await response.json()).exists === true;
    },

    async getFile(sha) {
      const response = await fetch(path('files', sha));
      if (!response.ok) return null;
      return new Uint8Array(await response.arrayBuffer());
    },

    // Stores bytes and answers with the SHA-256 the program computed while writing them.
    async addFile(bytes) {
      const response = await fetch(path('files'), { method: 'POST', body: bytes });
      if (!response.ok) throw await failure(response, 'The file could not be stored.');
      return response.json();
    },

    async listRecords(kind) {
      const response = await fetch(path('records', kind), { cache: 'no-store' });
      if (!response.ok) throw await failure(response, 'Could not list the library\'s records.');
      return (await response.json()).records;
    },

    async getRecord(kind, id) {
      const response = await fetch(path('records', kind, id), { cache: 'no-store' });
      if (response.status === 404) return null;
      if (!response.ok) throw await failure(response, 'The record could not be read.');
      return response.json();
    },

    async putRecord(kind, id, doc) {
      const response = await fetch(path('records', kind, id), { method: 'PUT', body: JSON.stringify(doc), headers: { 'Content-Type': 'application/json' } });
      if (!response.ok) throw await failure(response, 'The record could not be saved.');
    },

    async deleteRecord(kind, id) {
      const response = await fetch(path('records', kind, id), { method: 'DELETE' });
      if (!response.ok) throw await failure(response, 'The record could not be deleted.');
    },
  };
}

// Hands paths on this computer (bundle folders, images, tables) to the program, as a second
// "situcraft <paths>" launch does. Answers with every item the program now holds and what it
// skipped.
export async function openPaths(paths) {
  const response = await fetch('api/open', { method: 'POST', body: JSON.stringify({ paths }), headers: { 'Content-Type': 'application/json' } });
  if (!response.ok) throw await failure(response, 'The program could not open those paths.');
  return response.json();
}

// Small per-browser preferences (theme, drawer, last project): never analysis state.
export const prefs = {
  get(key, fallback) {
    try {
      const raw = localStorage.getItem(`situcraft:${key}`);
      return raw === null ? fallback : JSON.parse(raw);
    } catch {
      return fallback;
    }
  },
  set(key, value) {
    try {
      localStorage.setItem(`situcraft:${key}`, JSON.stringify(value));
    } catch { /* storage unavailable */ }
  },
};
