import { useState, useEffect, useCallback, useRef } from 'react';
import type { Card, ProjectConfig, BoardEvent, CardFilter } from '../types';
import { api, isAPIError } from '../api/client';
import { useSSE } from './useSSE';

interface UseBoardResult {
  config: ProjectConfig | null;
  cards: Card[];
  loading: boolean;
  error: string | null;
  connected: boolean;
  refresh: () => Promise<void>;
  updateCardLocally: (cardId: string, updates: Partial<Card>) => void;
  suppressSSE: (cardId: string) => void;
  unsuppressSSE: (cardId: string) => void;
}

export function useBoard(
  project: string,
  filter?: CardFilter,
  onSyncEvent?: (event: BoardEvent) => void,
  onCardCreated?: (event: BoardEvent) => void,
): UseBoardResult {
  const [config, setConfig] = useState<ProjectConfig | null>(null);
  const [cards, setCards] = useState<Card[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const inFlightRef = useRef<Set<string>>(new Set());

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
      setError(isAPIError(err) ? err.error : 'Failed to load board');
    } finally {
      setLoading(false);
    }
  }, [project, filter]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const onSyncEventRef = useRef(onSyncEvent);
  useEffect(() => {
    onSyncEventRef.current = onSyncEvent;
  }, [onSyncEvent]);

  const onCardCreatedRef = useRef(onCardCreated);
  useEffect(() => {
    onCardCreatedRef.current = onCardCreated;
  }, [onCardCreated]);

  const handleEvent = useCallback(
    (event: BoardEvent) => {
      // Forward sync events to the sync handler.
      if (event.type.startsWith('sync.')) {
        onSyncEventRef.current?.(event);
        // Also reload board when changes were pulled.
        if (event.type === 'sync.completed' && event.data?.changes_pulled) {
          fetchData();
        }
        return;
      }

      // Handle project config updates - reload to get new transitions
      if (event.type === 'project.updated' && event.project === project) {
        api.getProject(project).then(setConfig).catch((err) => {
          console.error('Failed to refresh config after project.updated:', err);
        });
        return;
      }

      if (event.project !== project) return;
      if (inFlightRef.current.has(event.card_id)) return;

      if (event.type === 'card.created') {
        onCardCreatedRef.current?.(event);
      }

      switch (event.type) {
        case 'card.created':
        case 'card.updated':
        case 'card.state_changed':
        case 'card.claimed':
        case 'card.released':
        case 'card.stalled':
        case 'card.log_added':
        case 'runner.triggered':
        case 'runner.started':
        case 'runner.failed':
        case 'runner.killed':
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
          }).catch((err) => {
            console.error('Failed to refresh card after SSE event:', event.card_id, err);
          });
          break;

        case 'card.deleted':
          setCards((prev) => prev.filter((c) => c.id !== event.card_id));
          break;
      }
    },
    [project, fetchData]
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

  const suppressSSE = useCallback((cardId: string) => {
    inFlightRef.current.add(cardId);
  }, []);

  const unsuppressSSE = useCallback((cardId: string) => {
    inFlightRef.current.delete(cardId);
  }, []);

  return {
    config,
    cards,
    loading,
    error: error || sseError,
    connected,
    refresh: fetchData,
    updateCardLocally,
    suppressSSE,
    unsuppressSSE,
  };
}
