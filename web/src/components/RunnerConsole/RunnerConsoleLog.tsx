import type { LogEntry } from '../../types';
import { VirtualLogList } from './VirtualLogList';

interface RunnerConsoleLogProps {
  logs: readonly LogEntry[];
  error: string | null;
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
      getKey={(entry, idx) => `${entry.ts}-${entry.card_id}-${idx}`}
      className="flex-1 min-h-0"
      role="log"
      ariaLive="polite"
      ariaAtomic={false}
      ariaLabel="Runner log"
      emptyState={emptyState}
    />
  );
}
