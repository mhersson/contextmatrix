import { useState, useEffect, useCallback } from 'react';
import type { Card, ProjectConfig, BoardEvent, CardFilter } from '../types';
import { api } from '../api/client';
import { useSSE } from './useSSE';

interface UseBoardResult {
  config: ProjectConfig | null;
  cards: Card[];
  loading: boolean;
  error: string | null;
  connected: boolean;
  refresh: () => Promise<void>;
  updateCardLocally: (cardId: string, updates: Partial<Card>) => void;
}

export function useBoard(project: string, filter?: CardFilter): UseBoardResult {
  const [config, setConfig] = useState<ProjectConfig | null>(null);
  const [cards, setCards] = useState<Card[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    if (!project) {
      setConfig(null);
      setCards([]);
      setLoading(false);
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const [projectConfig, projectCards] = await Promise.all([
        api.getProject(project),
        api.getCards(project, filter),
      ]);
      setConfig(projectConfig);
      setCards(projectCards);
    } catch (err) {
      const message =
        err && typeof err === 'object' && 'error' in err
          ? (err as { error: string }).error
          : 'Failed to load board';
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [project, filter]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const handleEvent = useCallback(
    (event: BoardEvent) => {
      if (event.project !== project) return;

      switch (event.type) {
        case 'card.created':
        case 'card.updated':
        case 'card.state_changed':
        case 'card.claimed':
        case 'card.released':
        case 'card.stalled':
        case 'card.log_added':
          api.getCard(project, event.card_id).then((card) => {
            setCards((prev) => {
              const index = prev.findIndex((c) => c.id === card.id);
              if (index >= 0) {
                const updated = [...prev];
                updated[index] = card;
                return updated;
              }
              return [...prev, card];
            });
          });
          break;

        case 'card.deleted':
          setCards((prev) => prev.filter((c) => c.id !== event.card_id));
          break;
      }
    },
    [project]
  );

  const { connected, error: sseError } = useSSE({
    project,
    onEvent: handleEvent,
  });

  const updateCardLocally = useCallback((cardId: string, updates: Partial<Card>) => {
    setCards((prev) =>
      prev.map((card) =>
        card.id === cardId ? { ...card, ...updates } : card
      )
    );
  }, []);

  return {
    config,
    cards,
    loading,
    error: error || sseError,
    connected,
    refresh: fetchData,
    updateCardLocally,
  };
}
