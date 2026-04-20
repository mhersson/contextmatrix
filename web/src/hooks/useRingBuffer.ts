import { useMemo, useSyncExternalStore } from 'react';
import type { LogEntry } from '../types';

export interface UseRingBufferResult {
  logs: readonly LogEntry[];
  append: (entries: LogEntry[]) => void;
  clear: () => void;
}

interface RingBufferStore {
  subscribe: (listener: () => void) => () => void;
  getSnapshot: () => readonly LogEntry[];
  append: (entries: LogEntry[]) => void;
  clear: () => void;
}

function createRingBufferStore(maxEntries: number): RingBufferStore {
  const capacity = Math.max(1, maxEntries);
  let buf: (LogEntry | undefined)[] = new Array<LogEntry | undefined>(capacity);
  let head = 0;
  let size = 0;
  let version = 0;
  let cachedVersion = -1;
  let cachedSnapshot: readonly LogEntry[] = [];
  const listeners = new Set<() => void>();

  function notify() {
    for (const l of listeners) l();
  }

  function buildSnapshot(): readonly LogEntry[] {
    if (size === 0) return [];
    const result: LogEntry[] = new Array(size);
    const start = size < capacity ? 0 : head;
    for (let i = 0; i < size; i++) {
      result[i] = buf[(start + i) % capacity] as LogEntry;
    }
    return result;
  }

  return {
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },

    // Rebuilt on demand once per version bump — append stays O(1).
    getSnapshot() {
      if (cachedVersion !== version) {
        cachedSnapshot = buildSnapshot();
        cachedVersion = version;
      }
      return cachedSnapshot;
    },

    append(entries: LogEntry[]) {
      if (entries.length === 0) return;

      const src = entries.length > capacity ? entries.slice(entries.length - capacity) : entries;

      for (const entry of src) {
        buf[head] = entry;
        head = (head + 1) % capacity;
        if (size < capacity) {
          size++;
        }
      }

      version++;
      notify();
    },

    clear() {
      if (size === 0) return;
      buf = new Array<LogEntry | undefined>(capacity);
      head = 0;
      size = 0;
      version++;
      notify();
    },
  };
}

export function useRingBuffer(maxEntries: number): UseRingBufferResult {
  const store = useMemo(() => createRingBufferStore(maxEntries), [maxEntries]);
  const logs = useSyncExternalStore(store.subscribe, store.getSnapshot);
  return { logs, append: store.append, clear: store.clear };
}
