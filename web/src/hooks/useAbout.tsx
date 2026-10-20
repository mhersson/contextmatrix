import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { api } from '../api/client';
import { safeGetJSON, safeSetJSON } from '../utils/safeStorage';
import {
  ABOUT_REFRESH_MS,
  ABOUT_SEEN_KEY,
  nextSeen,
  parseSeen,
  unseenCounts,
  type SeenMap,
  type UnseenCounts,
} from '../lib/about';
import type { AboutComponent, AboutResponse } from '../types';

export interface AboutState {
  about: AboutResponse | null;
  seen: SeenMap;
  unseen: UnseenCounts;
  hasUnseen: boolean;
  refresh: () => Promise<AboutResponse | null>;
  markSeen: (components: AboutComponent[]) => void;
}

const inert: AboutState = {
  about: null,
  seen: {},
  unseen: {},
  hasUnseen: false,
  refresh: async () => null,
  markSeen: () => {},
};

const AboutContext = createContext<AboutState>(inert);

const readSeen = (): SeenMap => parseSeen(safeGetJSON<unknown>(ABOUT_SEEN_KEY));

export function AboutProvider({ children }: { children: ReactNode }) {
  const [about, setAbout] = useState<AboutResponse | null>(null);
  const [seen, setSeen] = useState<SeenMap>(readSeen);
  const lastFetch = useRef(0);
  const inFlight = useRef<AbortController | null>(null);

  const refresh = useCallback(async (): Promise<AboutResponse | null> => {
    inFlight.current?.abort();
    const ctrl = new AbortController();
    inFlight.current = ctrl;
    try {
      const res = await api.getAbout(ctrl.signal);
      if (ctrl.signal.aborted) return null;
      lastFetch.current = Date.now();
      setAbout(res);
      return res;
    } catch (err) {
      if (!ctrl.signal.aborted) console.warn('about fetch failed:', err);
      return null;
    }
  }, []);

  useEffect(() => {
    // Mount fetch of an external resource; the setState happens after the await.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void refresh();
    const tick = () => {
      if (document.visibilityState === 'visible') void refresh();
    };
    const interval = setInterval(tick, ABOUT_REFRESH_MS);
    const onVisibility = () => {
      if (document.visibilityState === 'visible' && Date.now() - lastFetch.current >= ABOUT_REFRESH_MS) {
        void refresh();
      }
    };
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      clearInterval(interval);
      document.removeEventListener('visibilitychange', onVisibility);
      inFlight.current?.abort();
    };
  }, [refresh]);

  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === ABOUT_SEEN_KEY) setSeen(readSeen());
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);

  const markSeen = useCallback((components: AboutComponent[]) => {
    const next = nextSeen(readSeen(), components);
    setSeen(next);
    safeSetJSON(ABOUT_SEEN_KEY, next);
  }, []);

  const unseen = useMemo(() => unseenCounts(about, seen), [about, seen]);
  const value = useMemo<AboutState>(
    () => ({ about, seen, unseen, hasUnseen: Object.keys(unseen).length > 0, refresh, markSeen }),
    [about, seen, unseen, refresh, markSeen],
  );

  return <AboutContext.Provider value={value}>{children}</AboutContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAbout(): AboutState {
  return useContext(AboutContext);
}
