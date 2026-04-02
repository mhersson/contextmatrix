import { useState, useEffect, useRef, useCallback } from 'react';
import type { BoardEvent } from '../types';

interface UseSSEOptions {
  project?: string;
  onEvent?: (event: BoardEvent) => void;
}

interface UseSSEResult {
  connected: boolean;
  error: string | null;
}

const MAX_RECONNECT_DELAY = 30000;
const INITIAL_RECONNECT_DELAY = 1000;

export function useSSE({ project, onEvent }: UseSSEOptions): UseSSEResult {
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectDelayRef = useRef(INITIAL_RECONNECT_DELAY);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const onEventRef = useRef(onEvent);
  const connectRef = useRef<() => void>(() => {});
  const isMountedRef = useRef(true);

  // Keep onEventRef in sync with onEvent
  useEffect(() => {
    onEventRef.current = onEvent;
  }, [onEvent]);

  const connect = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    const url = project
      ? `/api/events?project=${encodeURIComponent(project)}`
      : '/api/events';

    const es = new EventSource(url);
    eventSourceRef.current = es;

    es.onopen = () => {
      setConnected(true);
      setError(null);
      reconnectDelayRef.current = INITIAL_RECONNECT_DELAY;
    };

    es.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as BoardEvent;
        onEventRef.current?.(data);
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
          MAX_RECONNECT_DELAY
        );
        connectRef.current();
      }, delay);
    };
  }, [project]);

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

  return { connected, error };
}
