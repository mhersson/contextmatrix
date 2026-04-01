import { useState, useCallback, useEffect } from 'react';

const STORAGE_KEY = 'contextmatrix-collapsed-cards';

export function useCollapsedCards(project: string, validCardIds: string[]): [Set<string>, (cardId: string) => void] {
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    try {
      const stored = localStorage.getItem(`${STORAGE_KEY}-${project}`);
      if (stored) return new Set(JSON.parse(stored));
    } catch { /* ignore */ }
    return new Set();
  });

  useEffect(() => {
    if (validCardIds.length === 0) return;
    const validSet = new Set(validCardIds);
    setCollapsed((prev) => {
      const next = new Set([...prev].filter((id) => validSet.has(id)));
      if (next.size === prev.size) return prev;
      localStorage.setItem(`${STORAGE_KEY}-${project}`, JSON.stringify([...next]));
      return next;
    });
  }, [project, validCardIds]);

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
