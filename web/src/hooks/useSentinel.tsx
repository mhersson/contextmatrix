import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { api } from '../api/client';
import type { SentinelInfo } from '../types';
import { useSSEBus } from './useSSEBus';

const POLL_MS = 60_000;
const EVENT_DEBOUNCE_MS = 1_000;

interface SentinelState {
  info: SentinelInfo | null;
  refresh: () => void;
}

const SentinelContext = createContext<SentinelState>({ info: null, refresh: () => undefined });

/** Sentinel's status: fetched on mount and reconnect, refetched on sentinel events and every minute. */
export function SentinelProvider({ children }: { children: ReactNode }) {
  const [info, setInfo] = useState<SentinelInfo | null>(null);
  const { subscribe, reconnectEpoch } = useSSEBus();
  // A slower, older answer never overwrites a newer one.
  const seqRef = useRef(0);

  const refresh = useCallback(() => {
    const seq = ++seqRef.current;
    api
      .getSentinel()
      .then((v) => {
        if (seq === seqRef.current) setInfo(v);
      })
      .catch(() => {
        // Keep the last info; the next poll or event retries.
      });
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh, reconnectEpoch]);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = subscribe('sentinel.*', () => {
      clearTimeout(timer);
      timer = setTimeout(refresh, EVENT_DEBOUNCE_MS);
    });
    const poll = setInterval(refresh, POLL_MS);
    return () => {
      off();
      clearTimeout(timer);
      clearInterval(poll);
    };
  }, [subscribe, refresh]);

  const value = useMemo(() => ({ info, refresh }), [info, refresh]);
  return <SentinelContext.Provider value={value}>{children}</SentinelContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useSentinel(): SentinelState {
  return useContext(SentinelContext);
}
