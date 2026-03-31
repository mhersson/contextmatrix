import { useState, useEffect, useCallback, useRef } from 'react';
import type { Card, ProjectConfig, BoardEvent } from '../types';
import { api } from '../api/client';
import { useSSE } from './useSSE';

interface BoardData {
  config: ProjectConfig;
  cards: Card[];
}

interface UseAllBoardsResult {
  boards: Map<string, BoardData>;
  loading: boolean;
  error: string | null;
}

export function useAllBoards(projectNames: string[]): UseAllBoardsResult {
  const [boards, setBoards] = useState<Map<string, BoardData>>(new Map());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const projectNamesRef = useRef(projectNames);
  projectNamesRef.current = projectNames;

  const fetchAll = useCallback(async () => {
    const names = projectNamesRef.current;
    if (names.length === 0) {
      setBoards(new Map());
      setLoading(false);
      return;
    }

    try {
      const results = await Promise.allSettled(
        names.map(async (name) => {
          const [config, cards] = await Promise.all([
            api.getProject(name),
            api.getCards(name),
          ]);
          return [name, { config, cards }] as const;
        })
      );

      const map = new Map<string, BoardData>();
      for (const result of results) {
        if (result.status === 'fulfilled') {
          map.set(result.value[0], result.value[1]);
        }
      }
      setBoards(map);
      setError(null);
    } catch {
      setError('Failed to load boards');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAll();
  }, [fetchAll, projectNames.length]);

  const handleEvent = useCallback(
    (event: BoardEvent) => {
      if (!event.type.startsWith('card.')) return;
      const name = event.project;
      if (!projectNamesRef.current.includes(name)) return;

      if (event.type === 'card.deleted') {
        setBoards((prev) => {
          const board = prev.get(name);
          if (!board) return prev;
          const next = new Map(prev);
          next.set(name, { ...board, cards: board.cards.filter((c) => c.id !== event.card_id) });
          return next;
        });
        return;
      }

      api.getCard(name, event.card_id).then((card) => {
        setBoards((prev) => {
          const board = prev.get(name);
          if (!board) return prev;
          const next = new Map(prev);
          const idx = board.cards.findIndex((c) => c.id === card.id);
          const cards = [...board.cards];
          if (idx >= 0) cards[idx] = card;
          else cards.push(card);
          next.set(name, { ...board, cards });
          return next;
        });
      });
    },
    []
  );

  useSSE({ onEvent: handleEvent });

  return { boards, loading, error };
}
