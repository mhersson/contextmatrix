import type { LLMChannel, LLMChannelUsage, LLMChannelView } from '../types';

export function channelLabel(view: LLMChannelView, mode: LLMChannel): string {
  return mode === 'claude_code' ? 'Claude Code' : view.endpoint?.label ?? 'Endpoint';
}

export function otherMode(mode: LLMChannel): LLMChannel {
  return mode === 'claude_code' ? 'endpoint' : 'claude_code';
}

export function usagePct(u: LLMChannelUsage): number {
  return Math.round(u.utilization * 100);
}

/** Meter tone: rejected beats the 80% warning bar. */
export function usageTone(u: LLMChannelUsage): 'ok' | 'warn' | 'stop' {
  if (u.status === 'rejected') return 'stop';
  return usagePct(u) >= 80 ? 'warn' : 'ok';
}

/** The non-stale window with the highest utilization, or null without a live reading. */
export function headlineWindow(usage: LLMChannelUsage[]): LLMChannelUsage | null {
  let best: LLMChannelUsage | null = null;
  for (const u of usage) {
    if (u.stale) continue;
    if (!best || u.utilization > best.utilization) best = u;
  }
  return best;
}

export function rejectedWindow(usage: LLMChannelUsage[]): LLMChannelUsage | null {
  return usage.find((u) => !u.stale && u.status === 'rejected') ?? null;
}

export function windowShort(window: string): string {
  if (window === 'seven_day') return 'wk';
  if (window === 'five_hour') return '5h';
  return window;
}

const windowNames: Record<string, { own: string; short: string }> = {
  seven_day: { own: 'Weekly limit', short: 'weekly' },
  five_hour: { own: '5-hour limit', short: '5-hour' },
};

/** Meter label; on the endpoint it names the subscription, which is not the active channel. */
export function windowLabel(window: string, mode: LLMChannel): string {
  const names = windowNames[window] ?? { own: window, short: window };
  return mode === 'claude_code' ? names.own : `Claude Code ${names.short}`;
}

const resetFormatter = new Intl.DateTimeFormat('en-GB', {
  weekday: 'short',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
});

/** "Thu 14:00" in local time, or null when the reset time is unknown. */
export function formatReset(iso: string): string | null {
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return null;
  return resetFormatter.format(t);
}

export function inFlightTotal(view: LLMChannelView, mode: LLMChannel): number {
  const c = view.in_flight[mode];
  return c ? c.cards + c.chats + c.blueprints : 0;
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** "1 card run, 2 chats" for a mode's live work; zero kinds are left out. */
export function inFlightBreakdown(view: LLMChannelView, mode: LLMChannel): string {
  const c = view.in_flight[mode];
  if (!c) return '';
  const parts: string[] = [];
  if (c.cards) parts.push(plural(c.cards, 'card run', 'card runs'));
  if (c.chats) parts.push(plural(c.chats, 'chat', 'chats'));
  if (c.blueprints) parts.push(plural(c.blueprints, 'blueprint', 'blueprints'));
  return parts.join(', ');
}

const exceptionFeature = { agent: 'Card runs', chat: 'Chats', architect: 'Blueprints' } as const;
const exceptionBackend = { agent: 'the agent', chat: 'the chat backend', architect: 'the architect' } as const;

export interface ChannelException {
  /** Sub-line text: "Blueprints stay on OpenRouter" */
  text: string;
  /** Popover line, with the reason: "Blueprints stay on OpenRouter: the architect has no Claude Code support yet" */
  detail: string;
}

/** Backends without Claude Code support; only worth saying while Claude Code is active. */
export function channelExceptions(view: LLMChannelView): ChannelException[] {
  if (view.mode !== 'claude_code') return [];
  return view.exceptions.map((e) => {
    const feature = exceptionFeature[e.backend] ?? e.backend;
    const reason = `${exceptionBackend[e.backend] ?? e.backend} has no Claude Code support yet`;
    if (e.runs_on === 'none') {
      const text = `${feature} unavailable: no endpoint is configured and ${reason}`;
      return { text, detail: text };
    }
    const text = `${feature} stay on ${channelLabel(view, 'endpoint')}`;
    return { text, detail: `${text}: ${reason}` };
  });
}

export interface SubLineItem {
  /** Rendered emphasised before the text. */
  count?: number;
  text: string;
}

export function subLineText(item: SubLineItem): string {
  return item.count === undefined ? item.text : `${item.count} ${item.text}`;
}

/** The strip's sub-line: only what needs saying, in a fixed order. */
export function subLineItems(view: LLMChannelView): SubLineItem[] {
  const items: SubLineItem[] = [];
  const rejected = view.mode === 'claude_code' ? rejectedWindow(view.usage) : null;
  if (rejected) {
    const reset = formatReset(rejected.resets_at);
    items.push({ text: `Limit reached · ${reset ? `resets ${reset}` : 'reset time not reported'}` });
  }
  const other = otherMode(view.mode);
  const n = inFlightTotal(view, other);
  if (n > 0) items.push({ count: n, text: `still on ${channelLabel(view, other)}` });
  for (const e of channelExceptions(view)) items.push({ text: e.text });
  return items;
}
