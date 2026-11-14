import { createContext, useContext, useEffect, useMemo, useRef, useState, useCallback, type ReactNode } from 'react';
import { api } from '../api/client';
import { projectCounts as countByProject, tabTitle } from '../lib/attention';
import type { AttentionItem } from '../types';
import { useSSEBus } from './useSSEBus';

interface AttentionState {
  /** Newest first. */
  items: AttentionItem[];
  /** False until the first answer, so nothing claims "nothing waits" early. */
  loaded: boolean;
  blueprints: ReadonlyMap<string, AttentionItem>;
  playbooks: ReadonlyMap<string, AttentionItem>;
  projectCounts: ReadonlyMap<string, number>;
}

const EMPTY: AttentionState = {
  items: [],
  loaded: false,
  blueprints: new Map(),
  playbooks: new Map(),
  projectCounts: new Map(),
};

const AttentionContext = createContext<AttentionState>(EMPTY);

function byKind(items: readonly AttentionItem[], kind: AttentionItem['kind']): Map<string, AttentionItem> {
  return new Map(items.filter((i) => i.kind === kind).map((i) => [i.id, i]));
}

/** What waits for the viewer: fetched on mount and reconnect, refetched on attention.changed; also owns the tab title. */
export function AttentionProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<AttentionItem[]>([]);
  const [loaded, setLoaded] = useState(false);
  const { subscribe, reconnectEpoch } = useSSEBus();
  // A slower, older answer never overwrites a newer one.
  const seqRef = useRef(0);

  const refresh = useCallback(() => {
    const seq = ++seqRef.current;
    api
      .getAttention()
      .then((v) => {
        if (seq !== seqRef.current) return;
        setItems(v);
        setLoaded(true);
      })
      .catch(() => {
        // Keep the last list; the next event retries.
      });
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh, reconnectEpoch]);

  useEffect(() => subscribe('attention.changed', refresh), [subscribe, refresh]);

  useEffect(() => {
    document.title = tabTitle(items.length);
    return () => {
      document.title = tabTitle(0);
    };
  }, [items.length]);

  const value = useMemo<AttentionState>(
    () => ({
      items,
      loaded,
      blueprints: byKind(items, 'blueprint'),
      playbooks: byKind(items, 'playbook'),
      projectCounts: countByProject(items),
    }),
    [items, loaded],
  );
  return <AttentionContext.Provider value={value}>{children}</AttentionContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAttention(): AttentionState {
  return useContext(AttentionContext);
}
