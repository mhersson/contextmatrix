import { useMemo, useState } from 'react';
import type { LogEntry } from '../../types';
import { RunnerConsoleHeader } from './RunnerConsoleHeader';
import { RunnerConsoleLog } from './RunnerConsoleLog';

interface RunnerConsoleProps {
  logs: LogEntry[];
  connected: boolean;
  onClose: () => void;
  onClear: () => void;
  flexBasis?: string;
}

export function RunnerConsole({ logs, connected, onClose, onClear, flexBasis }: RunnerConsoleProps) {
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
      <RunnerConsoleHeader
        connected={connected}
        cardFilter={cardFilter}
        cardIds={uniqueCardIds}
        onCardFilterChange={setCardFilter}
        onClear={onClear}
        onClose={onClose}
      />
      <RunnerConsoleLog logs={filteredLogs} />
    </div>
  );
}
