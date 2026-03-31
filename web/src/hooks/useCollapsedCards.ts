import { useState, useCallback } from 'react';

const STORAGE_KEY = 'contextmatrix-collapsed-cards';

export function useCollapsedCards(project: string): [Set<string>, (cardId: string) => void] {
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    try {
      const stored = localStorage.getItem(`${STORAGE_KEY}-${project}`);
      if (stored) return new Set(JSON.parse(stored));
    } catch { /* ignore */ }
    return new Set();
  });

  const toggle = useCallback((cardId: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(cardId)) {
        next.delete(cardId);
      } else {
        next.add(cardId);
      }
      localStorage.setItem(`${STORAGE_KEY}-${project}`, JSON.stringify([...next]));
      return next;
    });
  }, [project]);

  return [collapsed, toggle];
}
