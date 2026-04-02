import { useState, useEffect, useCallback } from 'react';
import type { SyncStatus, BoardEvent } from '../types';
import { api } from '../api/client';

interface UseSyncResult {
  syncStatus: SyncStatus | null;
  triggerSync: () => Promise<void>;
  handleSyncEvent: (event: BoardEvent) => void;
}

export function useSync(): UseSyncResult {
  const [syncStatus, setSyncStatus] = useState<SyncStatus | null>(null);

  useEffect(() => {
    api.getSyncStatus().then(setSyncStatus).catch(() => {
      // Sync endpoint may not be available; leave as null.
    });
  }, []);

  const triggerSync = useCallback(async () => {
    try {
      const status = await api.triggerSync();
      setSyncStatus(status);
    } catch {
      // Refresh status on error.
      api.getSyncStatus().then(setSyncStatus).catch(() => {});
    }
  }, []);

  const handleSyncEvent = useCallback((event: BoardEvent) => {
    switch (event.type) {
      case 'sync.started':
        setSyncStatus((prev) =>
          prev ? { ...prev, syncing: true } : prev
        );
        break;
      case 'sync.completed':
        setSyncStatus((prev) =>
          prev
            ? {
                ...prev,
                syncing: false,
                last_sync_time: event.timestamp,
                last_sync_error: undefined,
              }
            : prev
        );
        break;
      case 'sync.conflict':
      case 'sync.error':
        setSyncStatus((prev) =>
          prev
            ? {
                ...prev,
                syncing: false,
                last_sync_time: event.timestamp,
                last_sync_error:
                  (event.data?.error as string) || 'Sync failed',
              }
            : prev
        );
        break;
    }
  }, []);

  return { syncStatus, triggerSync, handleSyncEvent };
}
