import type { ChatSession, ChatStatus, SessionKind } from '../types';
import { formatCost } from './format';
import { sessionCostText, sessionSubscriptionUSD } from './subscription';

export type SessionsTab = SessionKind | 'playbooks';

/** The `?tab=` value of each Sessions page tab. */
export const SESSIONS_TAB: Record<SessionsTab, string> = { chat: 'chats', architect: 'blueprints', playbooks: 'playbooks' };

export function sessionsTabHref(tab: SessionsTab): string {
  return `/sessions?tab=${SESSIONS_TAB[tab]}`;
}

/** The tab a `?tab=` value names; Chats for a missing or unknown value. */
export function sessionsTab(value: string | null): SessionsTab {
  return (Object.keys(SESSIONS_TAB) as SessionsTab[]).find((t) => SESSIONS_TAB[t] === value) ?? 'chat';
}

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

/** The part of a session's cost that is a subscription estimate; 0 when its cost is unknown. */
export function sessionSubscriptionCostUSD(s: ChatSession): number {
  const total = sessionCostUSD(s);
  if (total === undefined) return 0;
  return sessionSubscriptionUSD({
    total,
    subscription: s.subscription_cost_usd,
    blueprint: sessionKind(s) === 'architect',
    channel: s.llm_channel,
  });
}

/** The session cost with its subscription share, by the same rule as the chat pane header. */
export function sessionCostLabel(s: ChatSession, fmt: (usd: number) => string): string | undefined {
  const v = sessionCostUSD(s);
  return v === undefined ? undefined : sessionCostText(v, sessionSubscriptionCostUSD(s), fmt);
}

export function formatSessionCost(s: ChatSession): string {
  return sessionCostLabel(s, formatCost) ?? '-';
}

export function statusDotColor(status: ChatStatus): string {
  switch (status) {
    case 'active':
      return 'var(--green)';
    case 'warm-idle':
      return 'var(--yellow)';
    default:
      return 'var(--grey0)';
  }
}
