import type { LogEntry, LogEntryType } from '../../types';

export const TYPE_COLORS: Record<LogEntryType, string> = {
  thinking: 'var(--grey2)',
  text: 'var(--fg)',
  tool_call: 'var(--aqua)',
  stderr: 'var(--yellow)',
  system: 'var(--green)',
  user: 'var(--blue)',
};

const CARD_BADGE_COLORS = [
  'var(--blue)',
  'var(--purple)',
  'var(--aqua)',
  'var(--orange)',
  'var(--yellow)',
];

export function cardBadgeColor(cardId: string): string {
  let hash = 0;
  for (let i = 0; i < cardId.length; i++) {
    hash = (hash * 31 + cardId.charCodeAt(i)) >>> 0;
  }
  return CARD_BADGE_COLORS[hash % CARD_BADGE_COLORS.length];
}

export function formatTimestamp(ts: string): string {
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

export function LogLine({ entry }: { entry: LogEntry }) {
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
