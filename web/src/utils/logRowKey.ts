import type { LogEntry } from '../types';

/** Stable per-entry key: server `seq` when present (monotonic, unique within a
 *  stream); client-only gap markers (no seq) fall back to their ts+content. The
 *  key must not depend on array position - the ring buffer shifts indices on
 *  every drop-oldest, which would otherwise remount the whole visible window. */
export function logRowKey(entry: LogEntry): string {
  return entry.seq !== undefined
    ? `s-${entry.seq}`
    : `g-${entry.ts}-${entry.content}`;
}
