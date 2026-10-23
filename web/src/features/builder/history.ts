// A bounded undo / redo over snapshots. Pure: the editor decides when a change
// deserves a step, and this only remembers the steps.
export type History<T> = {
  past: T[];
  future: T[];
};

export const maxSteps = 100;

export function emptyHistory<T>(): History<T> {
  return { past: [], future: [] };
}

// remember records what the editor held before a change. A new change ends the
// future: redoing over it would resurrect something the person has replaced.
export function remember<T>(history: History<T>, before: T): History<T> {
  return { past: [...history.past, before].slice(-maxSteps), future: [] };
}

export function undo<T>(history: History<T>, now: T): { history: History<T>; value: T } | null {
  const value = history.past.at(-1);
  if (value === undefined) return null;
  return { value, history: { past: history.past.slice(0, -1), future: [now, ...history.future] } };
}

export function redo<T>(history: History<T>, now: T): { history: History<T>; value: T } | null {
  const [value, ...rest] = history.future;
  if (value === undefined) return null;
  return { value, history: { past: [...history.past, now].slice(-maxSteps), future: rest } };
}
