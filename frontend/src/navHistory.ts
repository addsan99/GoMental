// Browser-style visit history for note selections.
//
// Kept out of App.tsx so the coalescing rule below can be tested directly: it's
// easy to get wrong in a way that only shows up as a Back button that needs ten
// presses, which is exactly the kind of bug nobody files.

export type NavHistory = {stack: string[]; index: number};

// 'push' adds an entry. 'replace' overwrites the current one, used while the
// user is arrowing through the note list: the note they settle on is worth a
// history entry, the ones they scrolled past aren't.
export type VisitMode = 'push' | 'replace';

export const HISTORY_MAX = 15;

export function recordVisit(prev: NavHistory, id: string, mode: VisitMode, max = HISTORY_MAX): NavHistory {
  if (!id || prev.stack[prev.index] === id) {
    return prev;
  }
  // Anything ahead of the cursor is a forward history that this visit
  // invalidates, exactly like a browser.
  const truncated = prev.stack.slice(0, prev.index + 1);
  if (mode === 'replace' && prev.index >= 0) {
    truncated[prev.index] = id;
    return {stack: truncated, index: prev.index};
  }
  truncated.push(id);
  const trimmed = truncated.slice(-max);
  return {stack: trimmed, index: trimmed.length - 1};
}
