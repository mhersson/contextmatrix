import { createContext, useContext, useRef, useState, useEffect, useCallback, useMemo } from 'react';
import type { ReactNode } from 'react';
import { SESSION_EXPIRED_EVENT } from '../api/client';
import type { BoardEvent } from '../types';

const MAX_RECONNECT_DELAY = 30000;
const INITIAL_RECONNECT_DELAY = 1000;

type Subscriber = (event: BoardEvent) => void;

/**
 * Pattern for filtering SSE events by type prefix.
 *
 * - `'*'` matches every event.
 * - `'<prefix>.*'` (e.g. `'card.*'`, `'worker.*'`, `'project.*'`, `'sync.*'`)
 *   matches every event whose `type` starts with `<prefix>.`.
 * - Any other string is treated as an exact match against `event.type`
 *   (e.g. `'card.updated'`).
 */
export type SSEPattern = '*' | 'card.*' | 'worker.*' | 'project.*' | 'sync.*' | string;

interface SSEBusContextValue {
  subscribe: (pattern: SSEPattern, onEvent: Subscriber) => () => void;
  connected: boolean;
  error: string | null;
  /**
   * Increments once per true reconnect (a successful open that follows at
   * least one prior successful open) - never on the initial connect.
   * Consumers that need to resync state lost during an SSE outage (e.g.
   * useBoard) watch this: any change after mount means "we just recovered
   * from a disconnect, missed events may exist, refetch."
   */
  reconnectEpoch: number;
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
// - 'card'           → 'e:card'   (exact bucket - distinct from 'p:card')
function bucketKey(pattern: SSEPattern): string {
  if (pattern === '*') return '*';
  if (pattern.endsWith('.*')) return 'p:' + pattern.slice(0, -2);
  return 'e:' + pattern;
}

// Extract the prefix of an event type (substring before the first '.').
// 'card.updated' → 'card'; 'worker.started' → 'worker'.
//
// Returns '' for types with no dot ('card', 'sync') so prefix-pattern
// dispatch skips them entirely - a 'card.*' subscriber should not match a
// bare event whose type is the literal string 'card'.
function eventPrefix(type: string): string {
  const dot = type.indexOf('.');
  return dot === -1 ? '' : type.slice(0, dot);
}

export function SSEProvider({ children }: SSEProviderProps) {
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [reconnectEpoch, setReconnectEpoch] = useState(0);

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
  const hasConnectedOnceRef = useRef(false);

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

      if (hasConnectedOnceRef.current) {
        setReconnectEpoch((n) => n + 1);
      } else {
        hasConnectedOnceRef.current = true;
      }
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
      // EventSource can't read HTTP status codes, but readyState at the
      // moment onerror fires distinguishes a dead session from a transient
      // drop: the server closing the stream (e.g. 401 once the session has
      // expired) leaves readyState CLOSED with no browser-level retry,
      // while a network blip leaves it CONNECTING (the browser is already
      // retrying). Must be read before es.close() below, which itself
      // forces readyState to CLOSED. An intentional close (unmount, our own
      // reconnect churn) never calls this handler at all, so this can't
      // misfire during teardown.
      const sessionExpired = es.readyState === EventSource.CLOSED;

      setConnected(false);
      es.close();
      eventSourceRef.current = null;

      if (sessionExpired) {
        window.dispatchEvent(new Event(SESSION_EXPIRED_EVENT));
      }

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
    () => ({ subscribe, connected, error, reconnectEpoch }),
    [subscribe, connected, error, reconnectEpoch],
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
