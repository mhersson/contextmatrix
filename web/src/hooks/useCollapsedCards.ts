import { useState, useCallback, useMemo } from 'react';

const STORAGE_KEY = 'contextmatrix-collapsed-cards';

export interface UseCollapsedCardsResult {
  collapsed: Set<string>;
  toggle: (cardId: string) => void;
  collapseMany: (cardIds: string[]) => void;
  expandMany: (cardIds: string[]) => void;
}

// Safari Private Browsing, quota exhaustion, and disabled storage all throw
// on localStorage access. Wrap both so a throw does not crash the component.
function safeGet(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}
function safeSet(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // ignore - caller already has the in-memory state
  }
}

function loadFromStorage(project: string): Set<string> {
  try {
    const stored = safeGet(`${STORAGE_KEY}-${project}`);
    if (stored) return new Set(JSON.parse(stored));
  } catch { /* ignore */ }
  return new Set();
}

export function useCollapsedCards(project: string, validCardIds: string[]): UseCollapsedCardsResult {
  // Track [project, collapsed] together so that when project changes we can
  // detect the mismatch during render and synchronously swap to the stored
  // state for the new project without an extra useEffect round-trip.
  const [state, setState] = useState<{ project: string; collapsed: Set<string> }>(() => ({
    project,
    collapsed: loadFromStorage(project),
  }));

  // Derived state during render: project prop changed - reload from localStorage.
  // Calling setState here schedules a synchronous re-render before paint
  // (React batches this correctly) and we return the new value immediately so
  // the current render is also correct.
  let collapsed = state.collapsed;
  if (state.project !== project) {
    const next = loadFromStorage(project);
    setState({ project, collapsed: next });
    collapsed = next;
  }

  const prunedCollapsed = useMemo(() => {
    if (validCardIds.length === 0) return collapsed;
    const validSet = new Set(validCardIds);
    const filtered = new Set([...collapsed].filter((id) => validSet.has(id)));
    return filtered.size === collapsed.size ? collapsed : filtered;
  }, [collapsed, validCardIds]);

  const toggle = useCallback((cardId: string) => {
    setState((prev) => {
      const next = new Set(prev.collapsed);
      if (next.has(cardId)) {
        next.delete(cardId);
      } else {
        next.add(cardId);
      }
      safeSet(`${STORAGE_KEY}-${project}`, JSON.stringify([...next]));
      return { project: prev.project, collapsed: next };
    });
  }, [project]);

  const collapseMany = useCallback((cardIds: string[]) => {
    setState((prev) => {
      const next = new Set(prev.collapsed);
      for (const id of cardIds) {
        next.add(id);
      }
      safeSet(`${STORAGE_KEY}-${project}`, JSON.stringify([...next]));
      return { project: prev.project, collapsed: next };
    });
  }, [project]);

  const expandMany = useCallback((cardIds: string[]) => {
    setState((prev) => {
      const next = new Set(prev.collapsed);
      for (const id of cardIds) {
        next.delete(id);
      }
      safeSet(`${STORAGE_KEY}-${project}`, JSON.stringify([...next]));
      return { project: prev.project, collapsed: next };
    });
  }, [project]);

  return { collapsed: prunedCollapsed, toggle, collapseMany, expandMany };
}
