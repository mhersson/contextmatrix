import { useEffect, useRef } from 'react';
import type { LogEntry, LogEntryType } from '../../types';

interface RunnerConsoleLogProps {
  logs: LogEntry[];
  error: string | null;
}

const TYPE_COLORS: Record<LogEntryType, string> = {
  thinking: 'var(--grey2)',
  text: 'var(--fg)',
  tool_call: 'var(--aqua)',
  stderr: 'var(--yellow)',
  system: 'var(--green)',
};

const CARD_BADGE_COLORS = [
  'var(--blue)',
  'var(--purple)',
  'var(--aqua)',
  'var(--orange)',
  'var(--yellow)',
];

function cardBadgeColor(cardId: string): string {
  let hash = 0;
  for (let i = 0; i < cardId.length; i++) {
    hash = (hash * 31 + cardId.charCodeAt(i)) >>> 0;
  }
  return CARD_BADGE_COLORS[hash % CARD_BADGE_COLORS.length];
}

function formatTimestamp(ts: string): string {
  try {
    const d = new Date(ts);
    const hh = String(d.getHours()).padStart(2, '0');
    const mm = String(d.getMinutes()).padStart(2, '0');
    const ss = String(d.getSeconds()).padStart(2, '0');
    const ms = String(d.getMilliseconds()).padStart(3, '0');
    return `${hh}:${mm}:${ss}.${ms}`;
  } catch {
    return ts;
  }
}

function LogLine({ entry }: { entry: LogEntry }) {
  const typeColor = TYPE_COLORS[entry.type] ?? 'var(--fg)';
  const badgeColor = cardBadgeColor(entry.card_id);

  return (
    <div className="flex items-baseline gap-2 px-3 py-px text-xs leading-5 hover:bg-[var(--bg0)] transition-colors">
      {/* Timestamp */}
      <span className="flex-shrink-0 tabular-nums" style={{ color: 'var(--grey1)' }}>
        {formatTimestamp(entry.ts)}
      </span>

      {/* Card ID badge */}
      <span
        className="flex-shrink-0 px-1 rounded text-[10px] font-semibold"
        style={{ color: badgeColor, border: `1px solid ${badgeColor}`, opacity: 0.85 }}
      >
        {entry.card_id}
      </span>

      {/* Type indicator */}
      <span className="flex-shrink-0 w-14 text-right" style={{ color: typeColor }}>
        {entry.type}
      </span>

      {/* Content */}
      <span className="min-w-0 break-words whitespace-pre-wrap" style={{ color: 'var(--fg)' }}>
        {entry.content}
      </span>
    </div>
  );
}

const NEAR_BOTTOM_THRESHOLD = 50;

export function RunnerConsoleLog({ logs, error }: RunnerConsoleLogProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const userScrolledUpRef = useRef(false);

  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    userScrolledUpRef.current = distanceFromBottom > NEAR_BOTTOM_THRESHOLD;
  };

  useEffect(() => {
    const el = containerRef.current;
    if (!el || userScrolledUpRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [logs]);

  if (logs.length === 0) {
    return (
      <div
        className="flex-1 flex items-center justify-center text-xs"
        style={{ color: error ? 'var(--red)' : 'var(--grey1)' }}
      >
        {error ?? 'No log entries'}
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className="flex-1 overflow-y-auto min-h-0"
      onScroll={handleScroll}
    >
      {logs.map((entry, idx) => (
        <LogLine key={`${entry.ts}-${entry.card_id}-${idx}`} entry={entry} />
      ))}
    </div>
  );
}
