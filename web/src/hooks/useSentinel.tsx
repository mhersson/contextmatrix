import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { api } from '../api/client';
import type { SentinelCardView, SentinelInfo } from '../types';
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

/** A card's Sentinel run facts: refetched on Sentinel events and on events for this card. */
// eslint-disable-next-line react-refresh/only-export-components
export function useCardSentinel(project: string, id: string): SentinelCardView | null {
  const [view, setView] = useState<SentinelCardView | null>(null);
  const { subscribe, reconnectEpoch } = useSSEBus();
  const [tick, setTick] = useState(0);

  useEffect(() => {
    const ctrl = new AbortController();
    api
      .getCardSentinel(project, id, ctrl.signal)
      .then(setView)
      .catch(() => {
        // Keep the last view; the next event retries.
      });
    return () => ctrl.abort();
  }, [project, id, tick, reconnectEpoch]);

  useEffect(() => {
    const bump = () => setTick((n) => n + 1);
    const offSentinel = subscribe('sentinel.*', bump);
    const offCard = subscribe('card.*', (e) => {
      if (e.project === project && e.card_id === id) bump();
    });
    return () => {
      offSentinel();
      offCard();
    };
  }, [subscribe, project, id]);

  return view;
}
