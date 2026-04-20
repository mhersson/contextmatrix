import { useMemo, useSyncExternalStore } from 'react';
import type { LogEntry } from '../types';

export interface UseRingBufferResult {
  /** Immutable snapshot of current log entries in insertion order (oldest first). */
  logs: readonly LogEntry[];
  /** Append one or more entries to the buffer. Overwrites oldest entries when full. */
  append: (entries: LogEntry[]) => void;
  /** Clear all entries. */
  clear: () => void;
}

interface RingBufferStore {
  subscribe: (listener: () => void) => () => void;
  getSnapshot: () => readonly LogEntry[];
  append: (entries: LogEntry[]) => void;
  clear: () => void;
}

/**
 * Create a plain JS ring-buffer store compatible with useSyncExternalStore.
 *
 * Maintains a pre-allocated fixed-size array and a head pointer. Appending
 * never copies the entire log; it only overwrites the slot at `head` and
 * advances the pointer — O(1) amortised per entry.
 *
 * The `getSnapshot` method returns a cached `readonly LogEntry[]` that is only
 * rebuilt when the buffer contents change, giving stable references across
 * calls during the same render.
 */
function createRingBufferStore(capacity: number): RingBufferStore {
  let buf: (LogEntry | undefined)[] = new Array<LogEntry | undefined>(capacity);
  let head = 0;
  let size = 0;
  let snapshot: readonly LogEntry[] = [];
  const listeners = new Set<() => void>();

  function notify() {
    for (const l of listeners) l();
  }

  function buildSnapshot(): readonly LogEntry[] {
    if (size === 0) return [];
    const result: LogEntry[] = new Array(size);
    if (size < capacity) {
      // Buffer has not wrapped yet — entries are contiguous starting at index 0.
      for (let i = 0; i < size; i++) {
        result[i] = buf[i] as LogEntry;
      }
    } else {
      // Buffer is full and has wrapped. Oldest entry is at `head`.
      for (let i = 0; i < size; i++) {
        result[i] = buf[(head + i) % capacity] as LogEntry;
      }
    }
    return result;
  }

  return {
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },

    getSnapshot() {
      return snapshot;
    },

    append(entries: LogEntry[]) {
      if (entries.length === 0) return;

      // If the incoming batch is larger than capacity, only keep the last `capacity` entries.
      const src = entries.length > capacity ? entries.slice(entries.length - capacity) : entries;

      for (const entry of src) {
        buf[head] = entry;
        head = (head + 1) % capacity;
        if (size < capacity) {
          size++;
        }
      }

      snapshot = buildSnapshot();
      notify();
    },

    clear() {
      buf = new Array<LogEntry | undefined>(capacity);
      head = 0;
      size = 0;
      snapshot = [];
      notify();
    },
  };
}

/**
 * useRingBuffer — O(1) amortised append with a fixed memory footprint.
 *
 * Internally keeps a pre-allocated array and a head pointer. Appending never
 * copies the entire log; it only overwrites the slot at `head` and advances
 * the pointer. Uses `useSyncExternalStore` for concurrent-safe reads —
 * eliminates render-phase ref access and automatically memoises the snapshot.
 *
 * The `logs` snapshot is rebuilt only when the buffer changes, so consumers
 * always read a stable immutable reference during a single render cycle.
 */
export function useRingBuffer(maxEntries: number): UseRingBufferResult {
  const store = useMemo(() => createRingBufferStore(maxEntries), [maxEntries]);
  const logs = useSyncExternalStore(store.subscribe, store.getSnapshot);
  return { logs, append: store.append, clear: store.clear };
}
