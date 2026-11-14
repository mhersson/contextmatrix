import { useState } from 'react';

/** Tick boxes on a table's rows: only ticks on shown rows count, and a row that stops showing loses its tick. */
export function useRowSelection(shownIds: string[]) {
  const [picked, setPicked] = useState<ReadonlySet<string>>(() => new Set());
  const selected = shownIds.filter((id) => picked.has(id));
  // Pruned while rendering, so a tick dropped by a tab switch, a filter or a refresh never comes back.
  const shownKey = shownIds.join(' ');
  const [prunedFor, setPrunedFor] = useState(shownKey);
  if (prunedFor !== shownKey) {
    setPrunedFor(shownKey);
    setPicked(new Set(selected));
  }

  const toggle = (id: string) =>
    setPicked((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  const toggleAll = () => setPicked(selected.length === shownIds.length ? new Set() : new Set(shownIds));

  /** Replaces the ticks, e.g. to keep only the rows a bulk action failed on. */
  const tickOnly = (ids: string[]) => setPicked(new Set(ids));

  return { picked, selected, toggle, toggleAll, tickOnly };
}
