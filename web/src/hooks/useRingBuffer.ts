import { useRef, useState, useCallback } from 'react';
import type { LogEntry } from '../types';

export interface UseRingBufferResult {
  /** Immutable snapshot of current log entries in insertion order (oldest first). */
  logs: readonly LogEntry[];
  /** Append one or more entries to the buffer. Overwrites oldest entries when full. */
  append: (entries: LogEntry[]) => void;
  /** Clear all entries. */
  clear: () => void;
}

interface RingBufferState {
  /** Pre-allocated fixed-size array. Slots beyond `size` are undefined. */
  buf: (LogEntry | undefined)[];
  /** Index of the next write position (wraps around capacity). */
  head: number;
  /** Number of valid entries currently stored (0..capacity). */
  size: number;
  /** Capacity = maxEntries. */
  capacity: number;
}

/**
 * useRingBuffer — O(1) amortised append with a fixed memory footprint.
 *
 * Internally keeps a pre-allocated array and a head pointer. Appending never
 * copies the entire log; it only overwrites the slot at `head` and advances
 * the pointer. React re-renders are triggered by a small integer state counter
 * that is incremented on every mutating call — no setState with the full array.
 *
 * The `logs` snapshot is rebuilt into a fresh array on each render (not on
 * each append), so consumers always read a stable immutable reference during a
 * single render cycle.
 */
export function useRingBuffer(maxEntries: number): UseRingBufferResult {
  const stateRef = useRef<RingBufferState>({
    buf: new Array<LogEntry | undefined>(maxEntries),
    head: 0,
    size: 0,
    capacity: maxEntries,
  });

  // Increment to force a re-render after a mutation.
  const [, setRenderCount] = useState(0);

  // Snapshot rebuilt each render from the ref — stable within a render cycle.
  const { buf, head, size, capacity } = stateRef.current;
  const logs = buildSnapshot(buf, head, size, capacity);

  const append = useCallback((entries: LogEntry[]) => {
    if (entries.length === 0) return;

    const s = stateRef.current;

    // If the incoming batch is larger than capacity, only keep the last `capacity` entries.
    const src = entries.length > s.capacity ? entries.slice(entries.length - s.capacity) : entries;

    for (const entry of src) {
      s.buf[s.head] = entry;
      s.head = (s.head + 1) % s.capacity;
      if (s.size < s.capacity) {
        s.size++;
      }
    }

    setRenderCount((c) => c + 1);
  }, []);

  const clear = useCallback(() => {
    const s = stateRef.current;
    s.buf = new Array<LogEntry | undefined>(s.capacity);
    s.head = 0;
    s.size = 0;
    setRenderCount((c) => c + 1);
  }, []);

  return { logs, append, clear };
}

/**
 * Build an ordered snapshot from the ring buffer state.
 * Returns entries in insertion order (oldest first).
 */
function buildSnapshot(
  buf: (LogEntry | undefined)[],
  head: number,
  size: number,
  capacity: number,
): readonly LogEntry[] {
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
