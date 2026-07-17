import { useCallback } from 'react';
import { useProjectScopedSet } from './useProjectScopedSet';

const STORAGE_KEY = 'contextmatrix-collapsed-cards';

export interface UseCollapsedCardsResult {
  collapsed: Set<string>;
  toggle: (cardId: string) => void;
  collapseMany: (cardIds: string[]) => void;
  expandMany: (cardIds: string[]) => void;
}

export function useCollapsedCards(project: string, validCardIds: string[]): UseCollapsedCardsResult {
  const { values, toggle, update } = useProjectScopedSet(STORAGE_KEY, project, validCardIds);

  const collapseMany = useCallback((cardIds: string[]) => {
    update((next) => {
      for (const id of cardIds) {
        next.add(id);
      }
    });
  }, [update]);

  const expandMany = useCallback((cardIds: string[]) => {
    update((next) => {
      for (const id of cardIds) {
        next.delete(id);
      }
    });
  }, [update]);

  return { collapsed: values, toggle, collapseMany, expandMany };
}
