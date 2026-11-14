import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { api } from '../api/client';
import type { PlaybookSummary } from '../types';
import { useSSEBus } from './useSSEBus';

const EVENT_DEBOUNCE_MS = 1_000;

// Card events that can move a playbook's progress; log and claim traffic cannot.
const CARD_EVENTS = ['card.created', 'card.updated', 'card.state_changed', 'card.deleted'];

interface PlaybooksState {
  playbooks: PlaybookSummary[];
  /** False until the first fetch succeeds. */
  loaded: boolean;
  error: string | null;
  refresh: () => void;
}

const PlaybooksContext = createContext<PlaybooksState>({
  playbooks: [],
  loaded: false,
  error: null,
  refresh: () => undefined,
});

/** The playbook list: fetched on mount and reconnect, refetched (debounced) when a playbook or a card's progress changes. */
export function PlaybooksProvider({ children }: { children: ReactNode }) {
  const [playbooks, setPlaybooks] = useState<PlaybookSummary[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { subscribe, reconnectEpoch } = useSSEBus();
  // A slower, older answer never overwrites a newer one.
  const seqRef = useRef(0);

  const refresh = useCallback(() => {
    const seq = ++seqRef.current;
    api
      .listPlaybooks()
      .then((list) => {
        if (seq !== seqRef.current) return;
        setPlaybooks(list);
        setLoaded(true);
        setError(null);
      })
      .catch((e: unknown) => {
        if (seq === seqRef.current) setError(e instanceof Error ? e.message : String(e));
      });
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh, reconnectEpoch]);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const onEvent = () => {
      clearTimeout(timer);
      timer = setTimeout(refresh, EVENT_DEBOUNCE_MS);
    };
    const offs = ['playbook.*', ...CARD_EVENTS].map((pattern) => subscribe(pattern, onEvent));
    return () => {
      offs.forEach((off) => off());
      clearTimeout(timer);
    };
  }, [subscribe, refresh]);

  const value = useMemo(() => ({ playbooks, loaded, error, refresh }), [playbooks, loaded, error, refresh]);
  return <PlaybooksContext.Provider value={value}>{children}</PlaybooksContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function usePlaybooks(): PlaybooksState {
  return useContext(PlaybooksContext);
}
