import type { LogEntryType } from '../../types';
import { idColor } from '../../utils/colorHash';

export const TYPE_COLORS: Record<LogEntryType, string> = {
  thinking: 'var(--grey2)',
  text: 'var(--fg)',
  tool_call: 'var(--aqua)',
  stderr: 'var(--yellow)',
  system: 'var(--green)',
  user: 'var(--blue)',
  gap: 'var(--orange)',
  user_question: 'var(--purple)',
};

export const cardBadgeColor = idColor;

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
