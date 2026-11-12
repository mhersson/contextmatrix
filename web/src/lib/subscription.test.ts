import { describe, expect, it } from 'vitest';
import type { UsageBucket } from '../types';
import {
  cardTotalText,
  parkedResetLabel,
  sessionCostText,
  sessionSubscriptionUSD,
  subscriptionShareText,
  subscriptionUSD,
  usageLimitText,
} from './subscription';

const usd = (n: number) => `$${n.toFixed(2)}`;

/** Thursday 2026-11-12 14:00 in the test runner's local time. */
const thu14 = new Date(2026, 10, 12, 14, 0).toISOString();

function bucket(cost: number, basis?: 'subscription'): UsageBucket {
  return {
    agent: 'a',
    model: 'm',
    prompt_tokens: 1,
    completion_tokens: 1,
    cost_usd: cost,
    cost_source: basis ? 'estimated' : 'actual',
    ...(basis ? { cost_basis: basis } : {}),
  };
}

describe('subscriptionUSD', () => {
  it('sums only subscription-basis buckets', () => {
    expect(subscriptionUSD([bucket(1), bucket(0.25, 'subscription'), bucket(0.5, 'subscription')])).toBe(0.75);
    expect(subscriptionUSD([bucket(1)])).toBe(0);
    expect(subscriptionUSD([])).toBe(0);
  });
});

describe('cardTotalText', () => {
  it('leaves a metered-only total to the plain amount', () => {
    expect(cardTotalText(3, 0, usd)).toBeNull();
  });

  it('labels an all-subscription total as API-equivalent', () => {
    expect(cardTotalText(1.5, 1.5, usd)).toBe('≈ $1.50 API-equivalent');
  });

  it('treats float noise between the sums as all-subscription', () => {
    expect(cardTotalText(0.1 + 0.2, 0.3, usd)).toBe('≈ $0.30 API-equivalent');
  });

  it('splits a mixed total and carries the mark on the total', () => {
    expect(cardTotalText(5, 2, usd)).toBe('$5.00 total · $2.00 of it subscription estimate');
    expect(cardTotalText(5, 2, usd, '*')).toBe('$5.00* total · $2.00 of it subscription estimate');
  });
});

describe('sessionCostText', () => {
  it('follows the chat rule', () => {
    expect(sessionCostText(1.25, 0, usd)).toBe('$1.25');
    expect(sessionCostText(1.25, 1.25, usd)).toBe('≈ $1.25');
    expect(sessionCostText(3, 1, usd)).toBe('$3.00 (≈ $1.00 subscription)');
  });
});

describe('sessionSubscriptionUSD', () => {
  it('caps the recorded subscription part at the total', () => {
    expect(sessionSubscriptionUSD({ total: 2, subscription: 0.5, blueprint: false, channel: 'claude_code' })).toBe(0.5);
    expect(sessionSubscriptionUSD({ total: 1, subscription: 3, blueprint: true, channel: 'endpoint' })).toBe(1);
    expect(sessionSubscriptionUSD({ total: 1, blueprint: false })).toBe(0);
  });

  it("counts a blueprint's whole spend while it runs on Claude Code", () => {
    expect(sessionSubscriptionUSD({ total: 2, subscription: 0.5, blueprint: true, channel: 'claude_code' })).toBe(2);
  });
});

describe('subscriptionShareText', () => {
  it('names the subscription part of a total', () => {
    expect(subscriptionShareText(2.5, usd)).toBe('incl. ≈ $2.50 subscription estimate');
  });
});

describe('parkedResetLabel', () => {
  const before = Date.parse(thu14) - 60_000;
  const after = Date.parse(thu14) + 60_000;

  it('names a future reset in local time', () => {
    expect(parkedResetLabel(thu14, before)).toBe('Parked - usage limit, resets Thu 14:00');
  });

  it('says the reset passed once it has', () => {
    expect(parkedResetLabel(thu14, after)).toBe('Parked - usage limit reset Thu 14:00');
  });

  it('gives nothing without a usable reset time', () => {
    expect(parkedResetLabel(undefined, before)).toBeNull();
    expect(parkedResetLabel('not a time', before)).toBeNull();
    expect(parkedResetLabel('0001-01-01T00:00:00Z', before)).toBeNull();
  });
});

describe('usageLimitText', () => {
  it('names the reset time when the vendor reported one', () => {
    expect(usageLimitText({ window: 'five_hour', resets_at: thu14 })).toBe(
      'Claude usage limit reached · resets Thu 14:00',
    );
  });

  it('says the reset time is unknown otherwise', () => {
    expect(usageLimitText({})).toBe('Claude usage limit reached · reset time not reported');
    expect(usageLimitText({ resets_at: '0001-01-01T00:00:00Z' })).toBe(
      'Claude usage limit reached · reset time not reported',
    );
  });
});
