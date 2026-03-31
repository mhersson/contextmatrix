import { useState, useCallback } from 'react';

const STORAGE_KEY = 'contextmatrix-collapsed-columns';

export function useCollapsedColumns(project: string): [Set<string>, (state: string) => void] {
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    try {
      const stored = localStorage.getItem(`${STORAGE_KEY}-${project}`);
      if (stored) return new Set(JSON.parse(stored));
    } catch { /* ignore */ }
    return new Set();
  });

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

  return [collapsed, toggle];
}
