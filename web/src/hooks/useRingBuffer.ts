import { useMemo, useSyncExternalStore } from 'react';
import type { LogEntry } from '../types';

export interface UseRingBufferResult {
  logs: readonly LogEntry[];
  /**
   * Entries are applied to the buffer immediately; subscribers are notified
   * at most once per 50 ms window so that per-frame SSE appends coalesce
   * into a handful of React commits instead of one commit per frame.
   * `clear` and `prepend` notify synchronously.
   */
  append: (entries: LogEntry[]) => void;
  /**
   * Inserts entries (chronological ASC) before the logical start. Never
   * evicts existing entries: when free space is insufficient, only the
   * NEWEST `capacity - size` entries of the batch are inserted, keeping the
   * transcript contiguous with what is already loaded. Returns the number
   * actually inserted; 0 when the buffer is full (no notification then).
   */
  prepend: (entries: LogEntry[]) => number;
  clear: () => void;
}

/** Coalescing window for deferred subscriber notification. A fixed timeout
 *  (not rAF - throttled in background tabs; not a microtask - drains between
 *  the per-frame onmessage macrotasks and would coalesce nothing). */
const FLUSH_MS = 50;

interface RingBufferStore {
  subscribe: (listener: () => void) => () => void;
  getSnapshot: () => readonly LogEntry[];
  append: (entries: LogEntry[]) => void;
  prepend: (entries: LogEntry[]) => number;
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
  let flushTimer: number | null = null;
  let dirty = false;
  const listeners = new Set<() => void>();

  function notify() {
    for (const l of listeners) l();
  }

  function cancelFlush() {
    if (flushTimer !== null) {
      clearTimeout(flushTimer);
      flushTimer = null;
    }
    dirty = false;
  }

  function flush() {
    flushTimer = null;
    if (!dirty) return;
    dirty = false;
    version++;
    notify();
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

    // Rebuilt on demand once per version bump - append stays O(1).
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

      dirty = true;
      if (flushTimer === null) {
        flushTimer = window.setTimeout(flush, FLUSH_MS);
      }
    },

    // History pages load on explicit user action, so the O(size) rebuild and
    // the synchronous notify are fine. Builds from the LIVE buffer (not the
    // render-cached snapshot) so appends still waiting in the flush window
    // are never lost; the pending flush is cancelled because this notify
    // already publishes them.
    prepend(entries: LogEntry[]): number {
      if (entries.length === 0) return 0;

      const available = capacity - size;
      if (available === 0) return 0;

      const src = entries.length > available ? entries.slice(entries.length - available) : entries;
      const combined = [...src, ...buildSnapshot()];

      cancelFlush();
      buf = new Array<LogEntry | undefined>(capacity);
      for (let i = 0; i < combined.length; i++) {
        buf[i] = combined[i];
      }
      head = combined.length % capacity;
      size = combined.length;
      version++;
      notify();

      return src.length;
    },

    clear() {
      cancelFlush();
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
  return { logs, append: store.append, prepend: store.prepend, clear: store.clear };
}
