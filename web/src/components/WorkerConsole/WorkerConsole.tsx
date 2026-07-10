import { useMemo, useState } from 'react';
import type { LogEntry } from '../../types';
import { WorkerConsoleHeader } from './WorkerConsoleHeader';
import { WorkerConsoleLog } from './WorkerConsoleLog';

interface WorkerConsoleProps {
  logs: readonly LogEntry[];
  connected: boolean;
  error: string | null;
  onClose: () => void;
  onClear: () => void;
  flexBasis?: string;
}

export function WorkerConsole({ logs, connected, error, onClose, onClear, flexBasis }: WorkerConsoleProps) {
  const [cardFilter, setCardFilter] = useState<string>('');

  const uniqueCardIds = useMemo(() => {
    const ids = new Set(logs.map((e) => e.card_id));
    return Array.from(ids).sort();
  }, [logs]);

  const filteredLogs = useMemo(() => {
    if (!cardFilter) return logs;
    return logs.filter((e) => e.card_id === cardFilter);
  }, [logs, cardFilter]);

  return (
    <div
      className="flex flex-col font-mono"
      style={{
        flex: flexBasis ? `0 1 ${flexBasis}` : '2 1 0%',
        background: 'var(--bg-dim)',
        borderTop: '1px solid var(--bg3)',
        minHeight: 0,
      }}
    >
      <WorkerConsoleHeader
        connected={connected}
        cardFilter={cardFilter}
        cardIds={uniqueCardIds}
        onCardFilterChange={setCardFilter}
        onClear={onClear}
        onClose={onClose}
      />
      <WorkerConsoleLog logs={filteredLogs} error={error} />
    </div>
  );
}
