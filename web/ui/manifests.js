// The program's artifact manifests as the page reads them (GET /api/artifacts/<ID>), each kept once
// complete: a complete artifact's manifest does not change.

const kept = new Map();

// An artifact's manifest, or null when the program no longer has it.
export async function manifestOf(id) {
  if (kept.has(id)) return kept.get(id);
  const response = await fetch(`/api/artifacts/${encodeURIComponent(id)}`);
  if (!response.ok) return null;
  const m = await response.json();
  if (m.state === 'complete') kept.set(id, m);
  return m;
}
