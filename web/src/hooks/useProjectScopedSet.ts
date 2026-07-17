import { useState, useCallback, useMemo } from 'react';
import { safeGetJSON, safeSetJSON } from '../utils/safeStorage';

export interface ProjectScopedSet {
  values: Set<string>;
  toggle: (id: string) => void;
  update: (mutate: (next: Set<string>) => void) => void;
}

function loadFromStorage(key: string): Set<string> {
  try {
    const stored = safeGetJSON<string[]>(key);
    if (stored) return new Set(stored);
  } catch { /* ignore - non-iterable stored value */ }

  return new Set();
}

/**
 * A Set of ids persisted to localStorage under `${storageKey}-${project}`,
 * reloaded when the project changes and pruned against the currently valid
 * ids. Shared core of useCollapsedCards and useCollapsedColumns.
 */
export function useProjectScopedSet(
  storageKey: string,
  project: string,
  validIds: string[],
): ProjectScopedSet {
  // Track [project, values] together so that when project changes we can
  // detect the mismatch during render and synchronously swap to the stored
  // state for the new project without an extra useEffect round-trip.
  const [state, setState] = useState<{ project: string; values: Set<string> }>(() => ({
    project,
    values: loadFromStorage(`${storageKey}-${project}`),
  }));

  // Derived state during render: project prop changed - reload from localStorage.
  // Calling setState here schedules a synchronous re-render before paint
  // (React batches this correctly) and we return the new value immediately so
  // the current render is also correct.
  let values = state.values;
  if (state.project !== project) {
    const next = loadFromStorage(`${storageKey}-${project}`);
    setState({ project, values: next });
    values = next;
  }

  const pruned = useMemo(() => {
    if (validIds.length === 0) return values;
    const validSet = new Set(validIds);
    const filtered = new Set([...values].filter((id) => validSet.has(id)));

    return filtered.size === values.size ? values : filtered;
  }, [values, validIds]);

  const update = useCallback((mutate: (next: Set<string>) => void) => {
    setState((prev) => {
      const next = new Set(prev.values);
      mutate(next);
      safeSetJSON(`${storageKey}-${project}`, [...next]);

      return { project: prev.project, values: next };
    });
  }, [storageKey, project]);

  const toggle = useCallback((id: string) => {
    update((next) => {
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
    });
  }, [update]);

  return { values: pruned, toggle, update };
}
