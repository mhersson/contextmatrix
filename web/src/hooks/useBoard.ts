import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import type { Card, ProjectConfig, BoardEvent, CardFilter } from '../types';
import { api, isAPIError } from '../api/client';
import { useSSEBus } from './useSSEBus';

// Monotonic request counter - mirrors the useProjectSummaries pattern so only
// the latest fetchData call commits its result to state.  Declared at module
// scope to avoid needing to thread it through the hook signature.
let _globalReqId = 0;

interface UseBoardResult {
  config: ProjectConfig | null;
  cards: Card[];
  loading: boolean;
  error: string | null;
  connected: boolean;
  refresh: () => Promise<void>;
  refreshCard: (cardId: string) => Promise<void>;
  updateCardLocally: (cardId: string, updates: Partial<Card>) => void;
  removeCardLocally: (cardId: string) => void;
  suppressSSE: (cardId: string) => void;
  unsuppressSSE: (cardId: string) => void;
}

/**
 * `useBoard` manages the cards + config for a project and subscribes to SSE
 * board updates.
 *
 * Caller contract for `filter`: callers may pass a literal object each render
 * (e.g. `useBoard(project, { state: 'todo' })`) - the hook stabilizes the
 * reference internally using a JSON-string key, so inline object literals do
 * not trigger re-fetch loops.
 */
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

  // Stabilize the filter reference against value-equal but reference-different
  // inputs. JSON.stringify is sufficient here: CardFilter is a flat bag of
  // primitives / string arrays, no Dates or cycles.
  const filterKey = filter ? JSON.stringify(filter) : '';
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const stableFilter = useMemo(() => filter, [filterKey]);

  // Per-instance monotonic request id - only the latest fetchData call commits
  // its results to state. This prevents a slower response from a stale request
  // (e.g. triggered by SSE sync.completed) from overwriting a newer one.
  const reqIdRef = useRef(0);

  const fetchData = useCallback(async () => {
    if (!project) {
      setConfig(null);
      setCards([]);
      setLoading(false);
      return;
    }

    // Stamp this request. Also stamp the global counter so concurrent hook
    // instances (different projects) don't share the same id space.
    const reqId = ++reqIdRef.current;
    ++_globalReqId;

    setLoading(true);
    setError(null);

    try {
      const [projectConfig, projectCards] = await Promise.all([
        api.getProject(project),
        api.getCards(project, stableFilter),
      ]);
      // Discard stale results - a newer fetchData call already took over.
      if (reqId !== reqIdRef.current) return;
      setConfig(projectConfig);
      setCards(projectCards);
    } catch (err) {
      if (reqId !== reqIdRef.current) return;
      setError(isAPIError(err) ? err.error : 'Failed to load board');
    } finally {
      if (reqId === reqIdRef.current) setLoading(false);
    }
  }, [project, stableFilter]);

  // Reset state on project/filter change (render-time pattern).
  const [prevProject, setPrevProject] = useState(project);
  const [prevFilter, setPrevFilter] = useState(stableFilter);
  if (project !== prevProject || stableFilter !== prevFilter) {
    setPrevProject(project);
    setPrevFilter(stableFilter);
    if (!project) {
      setConfig(null);
      setCards([]);
      setLoading(false);
      setError(null);
    } else {
      setLoading(true);
      setError(null);
    }
  }

  // Single mount/re-mount effect that delegates entirely to fetchData.
  // No duplicate inline fetch - avoids the stale-cancel race the old code had.
  // setState-in-effect is intentional here: this effect's purpose is to trigger
  // a data refetch when project/filter changes, and fetchData necessarily
  // manages loading/error state as part of that fetch.
  useEffect(() => {
    if (!project) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void fetchData();
  }, [project, stableFilter, fetchData]);

  // Replace-by-id merge shared by the SSE refetch below and refreshCard: if
  // the card is already in state its entry is replaced in place, otherwise it
  // is appended.
  const mergeCard = useCallback((card: Card) => {
    setCards((prev) => {
      const index = prev.findIndex((c) => c.id === card.id);
      if (index >= 0) {
        const updated = [...prev];
        updated[index] = card;
        return updated;
      }
      return [...prev, card];
    });
  }, []);

  // Fetches the single-card GET for `cardId` and merges the result into
  // state. Single-card GET carries fields the list endpoint omits (e.g.
  // `subtask_cost_usd`) - callers use this to hydrate a card the list
  // response under-populated, without waiting for an SSE event that may
  // never arrive (a finished card emits no further events).
  const refreshCard = useCallback(
    async (cardId: string) => {
      if (!project) return;
      try {
        const card = await api.getCard(project, cardId);
        mergeCard(card);
      } catch (err) {
        console.error('Failed to refresh card:', cardId, err);
      }
    },
    [project, mergeCard]
  );

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

      // card.deleted must NOT be suppressed by the in-flight guard: a delete
      // that races with a patchCard must still remove the card from local state
      // so it does not linger after the server has permanently removed it.
      if (event.type === 'card.deleted') {
        setCards((prev) => prev.filter((c) => c.id !== event.card_id));
        return;
      }

      // Suppress refresh-style SSE events while a patchCard is in flight to
      // avoid a stale server snapshot from overwriting an optimistic update.
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
        case 'worker.triggered':
        case 'worker.started':
        case 'worker.failed':
        case 'worker.killed':
          api.getCard(project, event.card_id).then(mergeCard).catch((err) => {
            console.error('Failed to refresh card after SSE event:', event.card_id, err);
          });
          break;
      }
    },
    [project, fetchData, mergeCard]
  );

  const { subscribe, connected, error: sseError, reconnectEpoch } = useSSEBus();

  // Resync after an SSE outage: useSSEBus bumps reconnectEpoch on every
  // true reconnect (never on the initial connect). Events published while
  // disconnected are otherwise silently lost and the board stays stale
  // until the user manually refreshes.
  const reconnectEpochRef = useRef(reconnectEpoch);
  useEffect(() => {
    if (reconnectEpoch !== reconnectEpochRef.current) {
      reconnectEpochRef.current = reconnectEpoch;
      void fetchData();
    }
  }, [reconnectEpoch, fetchData]);

  useEffect(() => {
    // Board reacts to card mutations, worker lifecycle, sync pulls that may
    // bring new card data, and project config updates (to pick up new
    // transitions). We register one subscriber per pattern instead of a
    // wildcard so unrelated events (e.g. other projects' activity) do not
    // reach the handler.
    const unsubCard = subscribe('card.*', handleEvent);
    const unsubWorker = subscribe('worker.*', handleEvent);
    const unsubSync = subscribe('sync.*', handleEvent);
    const unsubProjectUpdated = subscribe('project.updated', handleEvent);
    return () => {
      unsubCard();
      unsubWorker();
      unsubSync();
      unsubProjectUpdated();
    };
  }, [subscribe, handleEvent]);

  const updateCardLocally = useCallback((cardId: string, updates: Partial<Card>) => {
    setCards((prev) =>
      prev.map((card) =>
        card.id === cardId ? { ...card, ...updates } : card
      )
    );
  }, []);

  const removeCardLocally = useCallback((cardId: string) => {
    setCards((prev) => prev.filter((card) => card.id !== cardId));
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
    refreshCard,
    updateCardLocally,
    removeCardLocally,
    suppressSSE,
    unsuppressSSE,
  };
}
