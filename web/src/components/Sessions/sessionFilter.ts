import type { ChatSession } from '../../types';
import { isLive, sessionProjects } from '../../lib/sessions';

export interface SessionFilter {
  query: string;
  project: string;
  status: 'any' | 'live' | 'idle';
  pinnedOnly: boolean;
}

export const EMPTY_FILTER: SessionFilter = { query: '', project: '', status: 'any', pinnedOnly: false };

export function isEmptyFilter(f: SessionFilter): boolean {
  return f.query.trim() === '' && f.project === '' && f.status === 'any' && !f.pinnedOnly;
}

export function filterSessions(sessions: ChatSession[], f: SessionFilter): ChatSession[] {
  const query = f.query.trim().toLowerCase();
  return sessions.filter((s) => {
    if (query && !(s.title ?? '').toLowerCase().includes(query)) return false;
    if (f.project && !sessionProjects(s).includes(f.project)) return false;
    if (f.status === 'live' && !isLive(s)) return false;
    if (f.status === 'idle' && isLive(s)) return false;
    return !f.pinnedOnly || s.pinned === true;
  });
}

function activeAt(s: ChatSession): number {
  const t = Date.parse(s.last_active);
  return Number.isNaN(t) ? 0 : t;
}

export function newestActivityFirst(sessions: ChatSession[]): ChatSession[] {
  return [...sessions].sort((a, b) => activeAt(b) - activeAt(a));
}
