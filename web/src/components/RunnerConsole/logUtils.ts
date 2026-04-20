import type { LogEntryType } from '../../types';

export const TYPE_COLORS: Record<LogEntryType, string> = {
  thinking: 'var(--grey2)',
  text: 'var(--fg)',
  tool_call: 'var(--aqua)',
  stderr: 'var(--yellow)',
  system: 'var(--green)',
  user: 'var(--blue)',
  gap: 'var(--orange)',
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
