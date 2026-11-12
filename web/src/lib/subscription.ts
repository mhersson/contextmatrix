import type { ChatUsageLimit, UsageBucket } from '../types';
import { formatReset } from './llmChannel';

export const SUBSCRIPTION_TOOLTIP = 'API-equivalent estimate at list price; billed by the Claude subscription';

// The total and the subscription part come from separate float sums; a gap
// under a micro-dollar is rounding, not metered spend.
const EPSILON = 1e-6;

type Fmt = (usd: number) => string;

export function isSubscriptionBucket(b: UsageBucket): boolean {
  return b.cost_basis === 'subscription';
}

export function subscriptionUSD(buckets: readonly UsageBucket[]): number {
  return buckets.reduce((sum, b) => (isSubscriptionBucket(b) ? sum + b.cost_usd : sum), 0);
}

function allSubscription(total: number, subscription: number): boolean {
  return total - subscription <= EPSILON;
}

/** A card total that holds a subscription estimate; null when it holds none. `mark` follows the total. */
export function cardTotalText(total: number, subscription: number, fmt: Fmt, mark = ''): string | null {
  if (subscription <= 0) return null;
  if (allSubscription(total, subscription)) return `≈ ${fmt(total)} API-equivalent`;
  return `${fmt(total)}${mark} total · ${fmt(subscription)} of it subscription estimate`;
}

/** The chat rule: "≈ $Y" when all of it is subscription, "$X (≈ $Y subscription)" when mixed. */
export function sessionCostText(total: number, subscription: number, fmt: Fmt): string {
  if (subscription <= 0) return fmt(total);
  if (allSubscription(total, subscription)) return `≈ ${fmt(total)}`;
  return `${fmt(total)} (≈ ${fmt(subscription)} subscription)`;
}

export interface SessionCostInput {
  total: number;
  subscription?: number;
  blueprint: boolean;
  channel?: string;
}

/**
 * The subscription part of a session's cost. A blueprint's total is the
 * architect's own spend, which carries no basis, so on Claude Code all of it
 * counts; elsewhere CM's recorded part is capped at the total.
 */
export function sessionSubscriptionUSD({ total, subscription, blueprint, channel }: SessionCostInput): number {
  if (blueprint && channel === 'claude_code') return total;
  return Math.min(Math.max(subscription ?? 0, 0), total);
}

export function subscriptionShareText(subscription: number, fmt: Fmt): string {
  return `incl. ≈ ${fmt(subscription)} subscription estimate`;
}

/** The tooltip of an amount marked ≈ that is only partly subscription: the shared sentence, then the share. */
export function subscriptionShareTitle(subscription: number, fmt: Fmt): string {
  return `${SUBSCRIPTION_TOOLTIP}\n${subscriptionShareText(subscription, fmt)}`;
}

/** The tooltip for an amount's mark: the shared sentence goes wherever ≈ shows. */
export function markTitle(mark: string): string | undefined {
  return mark === '≈' ? SUBSCRIPTION_TOOLTIP : undefined;
}

/** The parked label for a run a usage limit stopped; null without a usable reset time. */
export function parkedResetLabel(resetsAt: string | undefined, now: number = Date.now()): string | null {
  if (!resetsAt) return null;
  const reset = formatReset(resetsAt);
  if (!reset) return null;
  return Date.parse(resetsAt) > now
    ? `Parked - usage limit, resets ${reset}`
    : `Parked - usage limit reset ${reset}`;
}

export function usageLimitText(limit: ChatUsageLimit): string {
  const reset = limit.resets_at ? formatReset(limit.resets_at) : null;
  return `Claude usage limit reached · ${reset ? `resets ${reset}` : 'reset time not reported'}`;
}
