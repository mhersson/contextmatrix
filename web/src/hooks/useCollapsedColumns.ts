import { useState, useCallback, useMemo } from 'react';

const STORAGE_KEY = 'contextmatrix-collapsed-columns';

function loadFromStorage(project: string): Set<string> {
  try {
    const stored = localStorage.getItem(`${STORAGE_KEY}-${project}`);
    if (stored) return new Set(JSON.parse(stored));
  } catch { /* ignore */ }
  return new Set();
}

export function useCollapsedColumns(project: string, validStates: string[]): [Set<string>, (state: string) => void] {
  // Track [project, collapsed] together so that when project changes we can
  // detect the mismatch during render and synchronously swap to the stored
  // state for the new project without an extra useEffect round-trip.
  const [state, setState] = useState<{ project: string; collapsed: Set<string> }>(() => ({
    project,
    collapsed: loadFromStorage(project),
  }));

  // Derived state during render: project prop changed — reload from localStorage.
  let collapsed = state.collapsed;
  if (state.project !== project) {
    const next = loadFromStorage(project);
    setState({ project, collapsed: next });
    collapsed = next;
  }

  const prunedCollapsed = useMemo(() => {
    if (validStates.length === 0) return collapsed;
    const validSet = new Set(validStates);
    const filtered = new Set([...collapsed].filter((s) => validSet.has(s)));
    return filtered.size === collapsed.size ? collapsed : filtered;
  }, [collapsed, validStates]);

  const toggle = useCallback((colState: string) => {
    setState((prev) => {
      const next = new Set(prev.collapsed);
      if (next.has(colState)) {
        next.delete(colState);
      } else {
        next.add(colState);
      }
      localStorage.setItem(`${STORAGE_KEY}-${project}`, JSON.stringify([...next]));
      return { project: prev.project, collapsed: next };
    });
  }, [project]);

  return [prunedCollapsed, toggle];
}
