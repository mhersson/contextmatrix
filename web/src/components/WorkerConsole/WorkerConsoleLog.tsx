import type { LogEntry } from '../../types';
import { logRowKey } from '../../utils/logRowKey';
import { VirtualLogList } from './VirtualLogList';

interface WorkerConsoleLogProps {
  logs: readonly LogEntry[];
  error: string | null;
}

export function WorkerConsoleLog({ logs, error }: WorkerConsoleLogProps) {
  const emptyState = (
    <div
      className="flex-1 flex items-center justify-center text-xs"
      style={{ color: error ? 'var(--red)' : 'var(--grey1)' }}
      role="log"
      aria-live="polite"
      aria-atomic="false"
      aria-label="Worker log"
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
      ariaLabel="Worker log"
      emptyState={emptyState}
    />
  );
}
