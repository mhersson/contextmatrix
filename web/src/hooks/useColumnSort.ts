import { useCallback, useState } from 'react';
import type { SortMode } from '../types';
import { safeGetJSON, safeSetJSON } from '../utils/safeStorage';

const SORT_MODES: ReadonlySet<string> = new Set<SortMode>([
  'recent',
  'id-asc',
  'id-desc',
  'priority',
  'type',
  'manual',
]);

function storageKey(project: string): string {
  return `contextmatrix-column-sort-${project}`;
}

/**
 * Reads the stored record, dropping entries whose state is not on the board and
 * entries whose value is not a known sort mode (hand-edited or retired values).
 */
function loadRecord(project: string, states: string[]): Record<string, SortMode> {
  const stored = safeGetJSON<Record<string, string>>(storageKey(project));
  if (!stored) return {};

  return prune(stored, states);
}

function prune(record: Record<string, string>, states: string[]): Record<string, SortMode> {
  const valid = new Set(states);
  const result: Record<string, SortMode> = {};
  for (const [state, mode] of Object.entries(record)) {
    if (valid.has(state) && SORT_MODES.has(mode)) result[state] = mode as SortMode;
  }

  return result;
}

/**
 * Per-project column sort preferences persisted to localStorage.
 *
 * Returns a tuple [getSort, setSort]:
 * - `getSort(state)` returns the current sort mode for a column (defaults to
 *   `'recent'` for states not in the record).
 * - `setSort(state, mode)` updates the record and persists it, pruning states
 *   that are no longer on the board.
 */
export function useColumnSort(
  project: string,
  states: string[],
): [(state: string) => SortMode, (state: string, mode: SortMode) => void] {
  // Track [project, record] together so a project change is detected during
  // render and the stored record for the new project is returned immediately,
  // without an extra useEffect round-trip.
  const [state, setState] = useState<{ project: string; record: Record<string, SortMode> }>(() => ({
    project,
    record: loadRecord(project, states),
  }));

  let record = state.record;
  if (state.project !== project) {
    record = loadRecord(project, states);
    setState({ project, record });
  }

  const getSort = useCallback(
    (column: string): SortMode => record[column] ?? 'recent',
    [record],
  );

  const setSort = useCallback(
    (column: string, mode: SortMode) => {
      setState((prev) => {
        const next = prune({ ...prev.record, [column]: mode }, states);
        safeSetJSON(storageKey(project), next);

        return { project: prev.project, record: next };
      });
    },
    [project, states],
  );

  return [getSort, setSort];
}
