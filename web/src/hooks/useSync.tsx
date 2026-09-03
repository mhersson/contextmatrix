import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { BoardEvent, SyncStatus } from '../types';
import { api } from '../api/client';
import { useSSEBus } from './useSSEBus';

interface SyncContextValue {
  /** One status per configured boards repo, in config order. */
  syncStatuses: SyncStatus[];
  /** The first repo's status: what a single-repo instance shows. */
  syncStatus: SyncStatus | null;
  /** The status of one repo by name; no name means the first repo. */
  statusFor: (repo?: string) => SyncStatus | null;
  /** Manual sync of one repo, or of every enabled repo without a name. */
  triggerSync: (repo?: string) => Promise<void>;
}

const SyncContext = createContext<SyncContextValue | null>(null);

function eventRepo(event: BoardEvent): string | undefined {
  const data = event.data as { repo?: unknown } | undefined;
  return typeof data?.repo === 'string' ? data.repo : undefined;
}

export function SyncProvider({ children }: { children: ReactNode }) {
  const [syncStatuses, setSyncStatuses] = useState<SyncStatus[]>([]);
  const inFlightRef = useRef(false);
  const { subscribe, reconnectEpoch } = useSSEBus();

  const refresh = useCallback(() => {
    api.getSyncStatuses().then(setSyncStatuses).catch(() => {
      // Sync endpoint may not be available; keep what we have.
    });
  }, []);

  useEffect(() => { refresh(); }, [refresh, reconnectEpoch]);

  useEffect(() => subscribe('sync.*', (event) => {
    if (event.type === 'sync.started') {
      const repo = eventRepo(event);
      setSyncStatuses((prev) => prev.map((s) => (!repo || s.repo === repo ? { ...s, syncing: true } : s)));
      return;
    }
    // completed, conflict, error: refresh from the backend for accurate timestamps
    refresh();
  }), [subscribe, refresh]);

  const triggerSync = useCallback(async (repo?: string) => {
    if (inFlightRef.current) return;
    inFlightRef.current = true;
    setSyncStatuses((prev) => prev.map((s) => (!repo || s.repo === repo ? { ...s, syncing: true } : s)));
    try {
      setSyncStatuses(await api.triggerSync(repo));
    } catch {
      refresh();
    } finally {
      inFlightRef.current = false;
    }
  }, [refresh]);

  const statusFor = useCallback((repo?: string): SyncStatus | null => {
    if (!repo) return syncStatuses[0] ?? null;
    return syncStatuses.find((s) => s.repo === repo) ?? null;
  }, [syncStatuses]);

  const value = useMemo<SyncContextValue>(
    () => ({ syncStatuses, syncStatus: syncStatuses[0] ?? null, statusFor, triggerSync }),
    [syncStatuses, statusFor, triggerSync],
  );

  return <SyncContext.Provider value={value}>{children}</SyncContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useSync(): SyncContextValue {
  const ctx = useContext(SyncContext);
  if (!ctx) {
    throw new Error('useSync must be used within a SyncProvider');
  }
  return ctx;
}
