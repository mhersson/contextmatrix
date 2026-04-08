import { useState, useCallback, useMemo } from 'react';

const STORAGE_KEY = 'contextmatrix-collapsed-cards';

export interface UseCollapsedCardsResult {
  collapsed: Set<string>;
  toggle: (cardId: string) => void;
  collapseMany: (cardIds: string[]) => void;
  expandMany: (cardIds: string[]) => void;
}

export function useCollapsedCards(project: string, validCardIds: string[]): UseCollapsedCardsResult {
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    try {
      const stored = localStorage.getItem(`${STORAGE_KEY}-${project}`);
      if (stored) return new Set(JSON.parse(stored));
    } catch { /* ignore */ }
    return new Set();
  });

  const prunedCollapsed = useMemo(() => {
    if (validCardIds.length === 0) return collapsed;
    const validSet = new Set(validCardIds);
    const filtered = new Set([...collapsed].filter((id) => validSet.has(id)));
    return filtered.size === collapsed.size ? collapsed : filtered;
  }, [collapsed, validCardIds]);

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

  const collapseMany = useCallback((cardIds: string[]) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      for (const id of cardIds) {
        next.add(id);
      }
      localStorage.setItem(`${STORAGE_KEY}-${project}`, JSON.stringify([...next]));
      return next;
    });
  }, [project]);

  const expandMany = useCallback((cardIds: string[]) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      for (const id of cardIds) {
        next.delete(id);
      }
      localStorage.setItem(`${STORAGE_KEY}-${project}`, JSON.stringify([...next]));
      return next;
    });
  }, [project]);

  return { collapsed: prunedCollapsed, toggle, collapseMany, expandMany };
}
