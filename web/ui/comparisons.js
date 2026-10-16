// Removing a comparison from the project, from the inspector or the Compare view: asked first,
// refused with the reason when a decision rests on it (lib/segmentations.js), undoable. Its files
// go to the library's trash once no saved project names it (api_compare.go).

import { confirmDialog, toast } from './overlays.js';
import { decisionsCiting, removeComparison } from '../lib/segmentations.js';

export async function askRemoveComparison(app, c) {
  const { project } = app.store;
  const name = (id) => project.artifacts.find((a) => a.kind === 'segmentation' && a.id === id)?.name ?? id;
  if (decisionsCiting(project, c.id).length) {
    try {
      removeComparison(project, c.id);
    } catch (error) {
      toast(error.message, { kind: 'error', timeout: 10000 });
    }
    return false;
  }
  const ok = await confirmDialog({
    title: 'Remove the comparison?',
    message: `The comparison of ${name(c.a)} (A) with ${name(c.b)} (B) leaves this project, and its files go to SituCraft's trash when no other project uses them. The two segmentations stay; they can be compared again at any time.`,
    confirm: 'Remove',
    danger: true,
  });
  if (!ok) return false;
  try {
    if (app.store.ui.focus?.kind === 'comparison' && app.store.ui.focus.id === c.id) app.focusItem?.(null);
    const answer = await app.bundles.removeComparison(c.id);
    if (answer.usedBy?.length) toast(`Removed from this project. Its files stay: ${answer.usedBy.join(', ')} still ${answer.usedBy.length === 1 ? 'uses' : 'use'} it.`, { kind: 'ok', timeout: 8000 });
    else toast('Comparison removed.', { kind: 'ok' });
    return true;
  } catch (error) {
    toast(`The comparison: ${error.message}`, { kind: 'error', timeout: 10000 });
    return false;
  }
}
