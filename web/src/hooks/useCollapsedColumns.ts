import { useState, useCallback, useMemo } from 'react';

const STORAGE_KEY = 'contextmatrix-collapsed-columns';

export function useCollapsedColumns(project: string, validStates: string[]): [Set<string>, (state: string) => void] {
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    try {
      const stored = localStorage.getItem(`${STORAGE_KEY}-${project}`);
      if (stored) return new Set(JSON.parse(stored));
    } catch { /* ignore */ }
    return new Set();
  });

  const prunedCollapsed = useMemo(() => {
    if (validStates.length === 0) return collapsed;
    const validSet = new Set(validStates);
    const filtered = new Set([...collapsed].filter((s) => validSet.has(s)));
    return filtered.size === collapsed.size ? collapsed : filtered;
  }, [collapsed, validStates]);

  const toggle = useCallback((state: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(state)) {
        next.delete(state);
      } else {
        next.add(state);
      }
      localStorage.setItem(`${STORAGE_KEY}-${project}`, JSON.stringify([...next]));
      return next;
    });
  }, [project]);

  return [prunedCollapsed, toggle];
}
