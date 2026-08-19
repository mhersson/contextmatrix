function withoutIndex(list: string[], index: number): string[] {
  if (index < 0) return list;
  return [...list.slice(0, index), ...list.slice(index + 1)];
}

function arrayMove(list: string[], from: number, to: number): string[] {
  const result = list.slice();
  const [moved] = result.splice(from, 1);
  result.splice(to, 0, moved);
  return result;
}

/**
 * Computes the manual card order after a drag, without touching React,
 * storage, or dnd-kit.
 *
 * `stored` is the persisted order; `columnIds` is the column's current ids in
 * visual (fallback-sorted) order. The two can disagree - a card may have left
 * the column (still in `stored`, absent from `columnIds`) or never been
 * ordered before (in `columnIds`, absent from `stored`) - so the first step
 * always materializes a full order before resolving the drag.
 *
 * `overId` is one of three things, and the branch order below encodes a
 * different policy for each:
 *   - a card id present in the materialized order - splice `activeId` next
 *     to it;
 *   - a column/state name (a cross-column drop target) - unresolvable,
 *     append `activeId` to the end;
 *   - a stale card id no longer on the board - same append path as above.
 * `to < 0` is checked before `from < 0` so an unresolved over-id always
 * falls back to "append to the end" rather than a stray negative-index
 * insert.
 */
export function applyMove(
  stored: readonly string[],
  columnIds: readonly string[],
  activeId: string,
  overId: string,
): string[] {
  const materialized = [...stored];
  const seen = new Set(stored);
  for (const id of columnIds) {
    if (!seen.has(id)) {
      seen.add(id);
      materialized.push(id);
    }
  }

  if (activeId === overId) return materialized;

  const from = materialized.indexOf(activeId);
  const to = materialized.indexOf(overId);

  if (to < 0) {
    return [...withoutIndex(materialized, from), activeId];
  }

  if (from < 0) {
    return [...materialized.slice(0, to), activeId, ...materialized.slice(to)];
  }

  return arrayMove(materialized, from, to);
}
