import type { ChatSession } from '../../types';
import { isLive, sessionProjects } from '../../lib/sessions';

export interface SessionFilter {
  query: string;
  project: string;
  status: 'any' | 'live' | 'idle' | 'waiting';
  pinnedOnly: boolean;
}

export const EMPTY_FILTER: SessionFilter = { query: '', project: '', status: 'any', pinnedOnly: false };

export function isEmptyFilter(f: SessionFilter): boolean {
  return f.query.trim() === '' && f.project === '' && f.status === 'any' && !f.pinnedOnly;
}

/** `waitingIds` are the sessions waiting on the viewer; only the Blueprints tab offers that status. */
export function filterSessions(
  sessions: ChatSession[],
  f: SessionFilter,
  waitingIds: ReadonlySet<string> = new Set(),
): ChatSession[] {
  const query = f.query.trim().toLowerCase();
  return sessions.filter((s) => {
    if (query && !(s.title ?? '').toLowerCase().includes(query)) return false;
    if (f.project && !sessionProjects(s).includes(f.project)) return false;
    if (f.status === 'live' && !isLive(s)) return false;
    if (f.status === 'waiting' && !waitingIds.has(s.id)) return false;
    if (f.status === 'idle' && isLive(s)) return false;
    return !f.pinnedOnly || s.pinned === true;
  });
}

/** A Project select's choices, sorted; the selected one stays listed after its rows go, so the select can show it. */
export function projectChoices(names: string[], selected: string): string[] {
  const choices = new Set(names);
  if (selected) choices.add(selected);
  return [...choices].sort((a, b) => a.localeCompare(b));
}

function activeAt(s: ChatSession): number {
  const t = Date.parse(s.last_active);
  return Number.isNaN(t) ? 0 : t;
}

export function newestActivityFirst(sessions: ChatSession[]): ChatSession[] {
  return [...sessions].sort((a, b) => activeAt(b) - activeAt(a));
}
