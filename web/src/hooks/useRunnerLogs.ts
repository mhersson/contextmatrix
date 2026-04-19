import { useState, useEffect, useRef, useCallback } from 'react';
import type { LogEntry } from '../types';

interface UseRunnerLogsOptions {
  project: string;
  enabled: boolean;
  maxEntries?: number;
  /** When set, the hook connects to the card-scoped session endpoint and
   *  receives only events for this card (server-filtered). */
  cardId?: string;
}

interface UseRunnerLogsResult {
  logs: LogEntry[];
  connected: boolean;
  error: string | null;
  clear: () => void;
}

const MAX_RECONNECT_DELAY = 30000;
const INITIAL_RECONNECT_DELAY = 1000;

export function useRunnerLogs({
  project,
  enabled,
  maxEntries = 5000,
  cardId,
}: UseRunnerLogsOptions): UseRunnerLogsResult {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectDelayRef = useRef(INITIAL_RECONNECT_DELAY);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const connectRef = useRef<() => void>(() => {});
  const isMountedRef = useRef(true);
  const maxEntriesRef = useRef(maxEntries);

  // Keep maxEntriesRef in sync
  useEffect(() => {
    maxEntriesRef.current = maxEntries;
  }, [maxEntries]);

  const disconnect = useCallback(() => {
    if (reconnectTimeoutRef.current !== null) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
    reconnectDelayRef.current = INITIAL_RECONNECT_DELAY;
    setConnected(false);
  }, []);

  const connect = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    let url = `/api/runner/logs?project=${encodeURIComponent(project)}`;
    if (cardId) {
      url += `&card_id=${encodeURIComponent(cardId)}`;
    }
    const es = new EventSource(url);
    eventSourceRef.current = es;

    es.onopen = () => {
      setConnected(true);
      setError(null);
      reconnectDelayRef.current = INITIAL_RECONNECT_DELAY;
    };

    es.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (data.type === 'error') {
          setError(data.content || 'Unknown error');
          return;
        }
        const entry = data as LogEntry;
        setLogs((prev) => {
          const next = [...prev, entry];
          return next.length > maxEntriesRef.current
            ? next.slice(next.length - maxEntriesRef.current)
            : next;
        });
      } catch {
        console.error('Failed to parse runner log entry:', event.data);
      }
    };

    es.onerror = () => {
      setConnected(false);
      es.close();
      eventSourceRef.current = null;

      const delay = reconnectDelayRef.current;
      setError((prev) => prev ?? `Disconnected. Reconnecting in ${Math.round(delay / 1000)}s...`);

      reconnectTimeoutRef.current = window.setTimeout(() => {
        if (!isMountedRef.current) return;
        reconnectDelayRef.current = Math.min(
          reconnectDelayRef.current * 2,
          MAX_RECONNECT_DELAY
        );
        connectRef.current();
      }, delay);
    };
  }, [project, cardId]);

  // Keep connectRef in sync with connect
  useEffect(() => {
    connectRef.current = connect;
  }, [connect]);

  // Track mount lifecycle separately — isMountedRef must only change on
  // true mount/unmount, not on every dependency change.
  useEffect(() => {
    isMountedRef.current = true;
    return () => { isMountedRef.current = false; };
  }, []);

  useEffect(() => {
    if (enabled) {
      connect();
    }

    return () => {
      disconnect();
    };
  }, [enabled, connect, disconnect]);

  const clear = useCallback(() => {
    setLogs([]);
  }, []);

  return { logs, connected, error, clear };
}
