import { createContext, useContext, useRef, useState, useEffect, useCallback } from 'react';
import type { ReactNode } from 'react';
import type { BoardEvent } from '../types';

const MAX_RECONNECT_DELAY = 30000;
const INITIAL_RECONNECT_DELAY = 1000;

type Subscriber = (event: BoardEvent) => void;

interface SSEBusContextValue {
  subscribe: (onEvent: Subscriber) => () => void;
  connected: boolean;
  error: string | null;
}

const SSEBusContext = createContext<SSEBusContextValue | null>(null);

interface SSEProviderProps {
  children: ReactNode;
}

export function SSEProvider({ children }: SSEProviderProps) {
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const subscribersRef = useRef<Set<Subscriber>>(new Set());
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
        subscribersRef.current.forEach((sub) => sub(data));
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

  const subscribe = useCallback((onEvent: Subscriber): (() => void) => {
    subscribersRef.current.add(onEvent);
    return () => {
      subscribersRef.current.delete(onEvent);
    };
  }, []);

  return (
    <SSEBusContext.Provider value={{ subscribe, connected, error }}>
      {children}
    </SSEBusContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useSSEBus(): SSEBusContextValue {
  const ctx = useContext(SSEBusContext);
  if (!ctx) {
    throw new Error('useSSEBus must be used inside SSEProvider');
  }
  return ctx;
}
