import type { LogEntry } from '../../types';
import { VirtualLogList } from './VirtualLogList';

interface RunnerConsoleLogProps {
  logs: readonly LogEntry[];
  error: string | null;
}

/** Stable per-entry key: server `seq` when present (monotonic, unique within a
 *  stream); client-only gap markers (no seq) fall back to their ts+content. The
 *  key must not depend on array position — the ring buffer shifts indices on
 *  every drop-oldest, which would otherwise remount the whole visible window. */
// eslint-disable-next-line react-refresh/only-export-components
export function logRowKey(entry: LogEntry): string {
  return entry.seq !== undefined
    ? `s-${entry.seq}`
    : `g-${entry.ts}-${entry.content}`;
}

export function RunnerConsoleLog({ logs, error }: RunnerConsoleLogProps) {
  const emptyState = (
    <div
      className="flex-1 flex items-center justify-center text-xs"
      style={{ color: error ? 'var(--red)' : 'var(--grey1)' }}
      role="log"
      aria-live="polite"
      aria-atomic="false"
      aria-label="Runner log"
    >
      {error ?? 'No log entries'}
    </div>
  );

  return (
    <VirtualLogList
      items={logs}
      getKey={logRowKey}
      className="flex-1 min-h-0"
      role="log"
      ariaLive="polite"
      ariaAtomic={false}
      ariaLabel="Runner log"
      emptyState={emptyState}
    />
  );
}
