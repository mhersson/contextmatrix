import type { ChatSession, ChatStatus, SessionKind } from '../types';
import { formatCost } from './format';

/** The `?tab=` value of each kind's Sessions page tab. */
export const SESSIONS_TAB: Record<SessionKind, string> = { chat: 'chats', architect: 'blueprints' };

export function sessionKind(s: ChatSession): SessionKind {
  return s.kind === 'architect' ? 'architect' : 'chat';
}

export function isLive(s: ChatSession): boolean {
  return s.status === 'active' || s.status === 'warm-idle';
}

export function sessionProjects(s: ChatSession): string[] {
  if (sessionKind(s) === 'architect') return s.workspace ?? [];
  return s.project ? [s.project] : [];
}

/** A blueprint's cost is the architect's recorded spend, never CM's estimate; undefined until recorded. */
export function sessionCostUSD(s: ChatSession): number | undefined {
  return sessionKind(s) === 'architect' ? s.spent_usd : (s.estimated_cost_usd ?? 0);
}

export function formatSessionCost(s: ChatSession): string {
  const v = sessionCostUSD(s);
  return v === undefined ? '-' : formatCost(v);
}

export function statusDotColor(status: ChatStatus): string | null {
  switch (status) {
    case 'active':
      return 'var(--green)';
    case 'warm-idle':
      return 'var(--yellow)';
    default:
      return null;
  }
}
