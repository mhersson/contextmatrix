import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from 'react';
import type { LogEntry } from '../types';

interface UseCardLogCacheResult {
  cache: ReadonlyMap<string, readonly LogEntry[]>;
  reset: () => void;
  invalidate: (cardId: string) => void;
}

interface CardLogCacheStore {
  subscribe: (listener: () => void) => () => void;
  getSnapshot: () => ReadonlyMap<string, readonly LogEntry[]>;
  append: (cardId: string, entries: readonly LogEntry[]) => void;
  reset: () => void;
  invalidate: (cardId: string) => void;
  lengthFor: (cardId: string) => number;
}

function createStore(): CardLogCacheStore {
  let cache: Map<string, readonly LogEntry[]> = new Map();
  let snapshot: ReadonlyMap<string, readonly LogEntry[]> = cache;
  const listeners = new Set<() => void>();

  function notify() {
    // useSyncExternalStore requires getSnapshot() to return a stable
    // reference between notifies — clone whenever the contents change.
    snapshot = new Map(cache);
    for (const l of listeners) l();
  }

  return {
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    getSnapshot() {
      return snapshot;
    },
    append(cardId, entries) {
      if (entries.length === 0) return;
      const existing = cache.get(cardId) ?? [];
      cache.set(cardId, [...existing, ...entries]);
      notify();
    },
    reset() {
      if (cache.size === 0) return;
      cache = new Map();
      notify();
    },
    invalidate(cardId) {
      if (!cache.has(cardId)) return;
      cache = new Map(cache);
      cache.delete(cardId);
      notify();
    },
    lengthFor(cardId) {
      return cache.get(cardId)?.length ?? 0;
    },
  };
}

/**
 * Per-card log buffer cache that survives `cardId` changes.
 *
 * `useRunnerLogs` keeps a single ring buffer that's wiped whenever its
 * `cardId` flips, so visiting card B between closing and reopening card
 * A would otherwise lose A's transcript and force a fresh server-side
 * snapshot replay. This hook accumulates each card's entries into a
 * separate slot keyed by cardId. Reopening any previously-streamed card
 * returns its cache instantly while the SSE reconnects in the
 * background.
 *
 * Dedup model: SSE events arrive in append-only order, both during a
 * live session and during snapshot replay on reconnect. The snapshot's
 * prefix therefore always matches what we've already cached, so every
 * new entry is at the tail. We append `liveLogs.slice(cached.length)`
 * — a length-based comparison — instead of trying to dedup by `seq`,
 * because the server currently emits `seq=0` for every event from the
 * runner and seq-based dedup wedges the cache after the first event.
 *
 * The first effect run after a `cardId` change is intentionally
 * skipped: `liveLogs` at that moment still references the previous
 * card's entries (the underlying ring buffer's clear-on-cardId
 * notification fires after this commit). React delivers a follow-up
 * render with the cleared buffer, at which point the length-based
 * append picks up cleanly.
 *
 * Implemented as an external store + `useSyncExternalStore` so the
 * effect updates the cache by calling store methods (which notify
 * subscribers) instead of setState — keeping accumulation off React's
 * derived-state path.
 */
export function useCardLogCache(
  liveLogs: readonly LogEntry[],
  cardId: string | null,
): UseCardLogCacheResult {
  const store = useMemo(() => createStore(), []);
  const cache = useSyncExternalStore(store.subscribe, store.getSnapshot);
  const prevCardIdRef = useRef<string | null>(null);

  useEffect(() => {
    if (cardId === null) {
      prevCardIdRef.current = null;
      return;
    }
    if (prevCardIdRef.current !== cardId) {
      // First commit for this cardId — liveLogs is stale from the previous
      // card. Skip; the follow-up render with the cleared buffer will
      // process normally.
      prevCardIdRef.current = cardId;
      return;
    }
    const cachedLen = store.lengthFor(cardId);
    if (liveLogs.length <= cachedLen) {
      // No new entries (or stream just cleared/reconnecting — preserve cache).
      return;
    }
    store.append(cardId, liveLogs.slice(cachedLen));
  }, [liveLogs, cardId, store]);

  const reset = useCallback(() => {
    prevCardIdRef.current = null;
    store.reset();
  }, [store]);

  // Drop a single card's slot. Also resets the per-card "first commit"
  // marker so the next render with the same cardId is treated like a
  // fresh attach: stale liveLogs from the previous session (which the
  // caller is about to replace) cannot re-populate the slot before the
  // upstream stream actually clears.
  const invalidate = useCallback((id: string) => {
    store.invalidate(id);
    if (prevCardIdRef.current === id) {
      prevCardIdRef.current = null;
    }
  }, [store]);

  return { cache, reset, invalidate };
}
