import { useCallback, useState } from 'react';
import type { SortMode } from '../types';
import { safeGetJSON, safeSetJSON } from '../utils/safeStorage';

function storageKey(project: string): string {
  return `contextmatrix-column-sort-${project}`;
}

function loadRecord(project: string): Record<string, SortMode> {
  return safeGetJSON<Record<string, SortMode>>(storageKey(project)) ?? {};
}

/**
 * Per-project column sort preferences persisted to localStorage.
 *
 * Returns a tuple [getSort, setSort]:
 * - `getSort(state)` returns the current sort mode for a column (defaults to
 *   `'recent'` for states not in the record).
 * - `setSort(state, mode)` updates the record, persists it, and prunes
 *   orphaned states (keys not in the current `states` array).
 *
 * On mount (and project change), reads from localStorage and prunes orphans.
 */
export function useColumnSort(
  project: string,
  states: string[],
): [(state: string) => SortMode, (state: string, mode: SortMode) => void] {
  const [record, setRecord] = useState<Record<string, SortMode>>(() => {
    const r = loadRecord(project);
    return pruneOrphans(r, states);
  });

  // Detect project change and reload.
  const [prevProject, setPrevProject] = useState(project);
  if (project !== prevProject) {
    setPrevProject(project);
    const r = loadRecord(project);
    const pruned = pruneOrphans(r, states);
    setRecord(pruned);
  }

  const getSort = useCallback(
    (state: string): SortMode => record[state] ?? 'recent',
    [record],
  );

  const setSort = useCallback(
    (state: string, mode: SortMode) => {
      setRecord((prev) => {
        const next = { ...prev, [state]: mode };
        const pruned = pruneOrphans(next, states);
        safeSetJSON(storageKey(project), pruned);
        return pruned;
      });
    },
    [project, states],
  );

  return [getSort, setSort];
}

function pruneOrphans(
  record: Record<string, SortMode>,
  states: string[],
): Record<string, SortMode> {
  const valid = new Set(states);
  let changed = false;
  const result: Record<string, SortMode> = {};
  for (const [key, value] of Object.entries(record)) {
    if (valid.has(key)) {
      result[key] = value;
    } else {
      changed = true;
    }
  }
  // If we removed orphans and the previous value was the same object identity,
  // return the pruned copy only when something actually changed.
  if (!changed && Object.keys(result).length === Object.keys(record).length) {
    return record;
  }
  return result;
}
