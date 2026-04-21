import { createContext, useContext, useRef, useState, useEffect, useCallback, useMemo } from 'react';
import type { ReactNode } from 'react';
import type { BoardEvent } from '../types';

const MAX_RECONNECT_DELAY = 30000;
const INITIAL_RECONNECT_DELAY = 1000;

type Subscriber = (event: BoardEvent) => void;

/**
 * Pattern for filtering SSE events by type prefix.
 *
 * - `'*'` matches every event.
 * - `'<prefix>.*'` (e.g. `'card.*'`, `'runner.*'`, `'project.*'`, `'sync.*'`)
 *   matches every event whose `type` starts with `<prefix>.`.
 * - Any other string is treated as an exact match against `event.type`
 *   (e.g. `'card.updated'`).
 */
export type SSEPattern = '*' | 'card.*' | 'runner.*' | 'project.*' | 'sync.*' | string;

interface SSEBusContextValue {
  subscribe: (pattern: SSEPattern, onEvent: Subscriber) => () => void;
  connected: boolean;
  error: string | null;
}

const SSEBusContext = createContext<SSEBusContextValue | null>(null);

interface SSEProviderProps {
  children: ReactNode;
}

// Split a pattern into its "bucket" key. Buckets are sigil-prefixed so a
// prefix subscription to 'card.*' and an exact-match subscription to the
// literal string 'card' (no dot) cannot collide on the same bucket:
//
// - '*'              → '*'        (wildcard bucket, receives every event)
// - 'card.*'         → 'p:card'   (prefix bucket)
// - 'card.updated'   → 'e:card.updated' (exact bucket)
// - 'card'           → 'e:card'   (exact bucket — distinct from 'p:card')
function bucketKey(pattern: SSEPattern): string {
  if (pattern === '*') return '*';
  if (pattern.endsWith('.*')) return 'p:' + pattern.slice(0, -2);
  return 'e:' + pattern;
}

// Extract the prefix of an event type (substring before the first '.').
// 'card.updated' → 'card'; 'runner.started' → 'runner'.
//
// Returns '' for types with no dot ('card', 'sync') so prefix-pattern
// dispatch skips them entirely — a 'card.*' subscriber should not match a
// bare event whose type is the literal string 'card'.
function eventPrefix(type: string): string {
  const dot = type.indexOf('.');
  return dot === -1 ? '' : type.slice(0, dot);
}

export function SSEProvider({ children }: SSEProviderProps) {
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Map keyed by bucket (sigil-prefixed to avoid collisions):
  //   '*'              → wildcard subscribers
  //   'p:card'         → subscribers registered with 'card.*' (prefix match)
  //   'e:card.updated' → exact-match subscribers for that event type
  //   'e:card'         → exact-match subscribers for the literal type 'card'
  // Fan-out is O(matching buckets), not O(all subscribers).
  const subscribersRef = useRef<Map<string, Set<Subscriber>>>(new Map());
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectDelayRef = useRef(INITIAL_RECONNECT_DELAY);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const isMountedRef = useRef(true);
  const connectRef = useRef<() => void>(() => {});

  const connect = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    const es = new EventSource('/api/events');
    eventSourceRef.current = es;

    es.onopen = () => {
      setConnected(true);
      setError(null);
      reconnectDelayRef.current = INITIAL_RECONNECT_DELAY;
    };

    es.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as BoardEvent;
        const buckets = subscribersRef.current;

        // Wildcard subscribers
        const wild = buckets.get('*');
        wild?.forEach((sub) => sub(data));

        // Prefix subscribers (e.g. 'card.*' → bucket 'p:card')
        const prefix = eventPrefix(data.type);
        if (prefix) {
          const prefixSubs = buckets.get('p:' + prefix);
          prefixSubs?.forEach((sub) => sub(data));
        }

        // Exact-match subscribers (e.g. 'card.updated' → bucket 'e:card.updated')
        const exact = buckets.get('e:' + data.type);
        exact?.forEach((sub) => sub(data));
      } catch {
        console.error('Failed to parse SSE event:', event.data);
      }
    };

    es.onerror = () => {
      setConnected(false);
      es.close();
      eventSourceRef.current = null;

      const delay = reconnectDelayRef.current;
      setError(`Disconnected. Reconnecting in ${Math.round(delay / 1000)}s...`);

      reconnectTimeoutRef.current = window.setTimeout(() => {
        if (!isMountedRef.current) return;
        reconnectDelayRef.current = Math.min(
          reconnectDelayRef.current * 2,
          MAX_RECONNECT_DELAY,
        );
        connectRef.current();
      }, delay);
    };
  }, []);

  // Keep connectRef in sync with connect
  useEffect(() => {
    connectRef.current = connect;
  }, [connect]);

  useEffect(() => {
    isMountedRef.current = true;
    connect();

    return () => {
      isMountedRef.current = false;
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }
    };
  }, [connect]);

  const subscribe = useCallback((pattern: SSEPattern, onEvent: Subscriber): (() => void) => {
    const key = bucketKey(pattern);
    const buckets = subscribersRef.current;
    let bucket = buckets.get(key);
    if (!bucket) {
      bucket = new Set();
      buckets.set(key, bucket);
    }
    bucket.add(onEvent);
    return () => {
      const b = buckets.get(key);
      if (!b) return;
      b.delete(onEvent);
      if (b.size === 0) {
        buckets.delete(key);
      }
    };
  }, []);

  const value = useMemo<SSEBusContextValue>(
    () => ({ subscribe, connected, error }),
    [subscribe, connected, error],
  );

  return <SSEBusContext.Provider value={value}>{children}</SSEBusContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useSSEBus(): SSEBusContextValue {
  const ctx = useContext(SSEBusContext);
  if (!ctx) {
    throw new Error('useSSEBus must be used inside SSEProvider');
  }
  return ctx;
}
