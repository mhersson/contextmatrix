import type { LogEntry } from '../../types';
import { TYPE_COLORS, cardBadgeColor, formatTimestamp } from './logUtils';

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
