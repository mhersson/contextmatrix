import type { ChatSession } from '../../types';
import { isLive, sessionKind } from '../../lib/sessions';

export const SECTION_ROW_CAP = 5;

function activeAt(s: ChatSession): number {
  const t = Date.parse(s.last_active);
  return Number.isNaN(t) ? 0 : t;
}

// Ties break on id so the rows never depend on the order the server listed them in.
function newestFirst(a: ChatSession, b: ChatSession): number {
  return activeAt(b) - activeAt(a) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
}

// No reaper ends a warm-idle blueprint, so only an active or mid-turn one shows past the cap.
function showsPastCap(s: ChatSession): boolean {
  if (sessionKind(s) === 'architect') return s.status === 'active' || s.assistant_working === true;
  return isLive(s);
}

/** Pinned, then the rows showsPastCap keeps, then the newest of the rest up to `cap` rows; the first two groups always show. */
export function sectionRows(sessions: ChatSession[], cap: number = SECTION_ROW_CAP): ChatSession[] {
  const sorted = [...sessions].sort(newestFirst);
  const pinned = sorted.filter((s) => s.pinned === true);
  const live = sorted.filter((s) => s.pinned !== true && showsPastCap(s));
  const rest = sorted.filter((s) => s.pinned !== true && !showsPastCap(s));
  const room = Math.max(0, cap - pinned.length - live.length);
  return [...pinned, ...live, ...rest.slice(0, room)];
}
