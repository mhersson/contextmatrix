import { describe, it, expect } from 'vitest';
import type { SentinelState, SentinelStatus } from '../types';
import { spendLine, stripText, stripTone, windowLines } from './sentinel';

function status(over: Partial<SentinelStatus> = {}): SentinelStatus {
  return {
    enabled: true,
    state: 'watching',
    mode: 'endpoint',
    healing: 0,
    healed_today: 0,
    spent_today_usd: 0,
    daily_cap_usd: 20,
    headroom: 0.85,
    ...over,
  };
}

describe('stripText', () => {
  it.each<[Partial<SentinelStatus>, string]>([
    [{ state: 'watching' }, 'watching'],
    [{ state: 'working', healing: 2 }, 'healing 2'],
    [{ state: 'paused', reason: 'daily cap reached' }, 'daily cap reached'],
    [{ state: 'not_running', reason: 'controller stopped' }, 'not running'],
  ])('%j -> %s', (over, want) => {
    expect(stripText(status(over))).toBe(want);
  });
});

describe('stripTone', () => {
  it.each<[SentinelState, string]>([
    ['watching', 'active'],
    ['working', 'active'],
    ['paused', 'paused'],
    ['not_running', 'stopped'],
  ])('%s -> %s', (state, want) => {
    expect(stripTone(status({ state }))).toBe(want);
  });
});

describe('spendLine', () => {
  it('formats spend against the cap', () => {
    expect(spendLine(status({ spent_today_usd: 1.2, daily_cap_usd: 20 }))).toBe('$1.20 of $20.00 today');
  });
});

describe('windowLines', () => {
  it('formats each window against the threshold', () => {
    const s = status({ mode: 'claude_code', windows: [{ window: 'five_hour', utilization: 0.62 }] });
    expect(windowLines(s)).toEqual(['five_hour 62% of 85%']);
  });
  it('is empty without windows', () => {
    expect(windowLines(status())).toEqual([]);
  });
});
