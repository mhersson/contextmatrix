import { describe, expect, it } from 'vitest';
import { formatCost, formatRelativeTime, formatTokens } from './format';

describe('formatCost', () => {
  it('keeps 4 decimals below 10 cents', () => {
    expect(formatCost(0.0123)).toBe('$0.0123');
  });

  it('uses 2 decimals from 10 cents up', () => {
    expect(formatCost(0.1)).toBe('$0.10');
    expect(formatCost(4.99)).toBe('$4.99');
  });
});

describe('formatTokens', () => {
  it('rounds to whole k from 10k up', () => {
    expect(formatTokens(583000)).toBe('583k');
  });

  it('uses one decimal M above a million', () => {
    expect(formatTokens(1234567)).toBe('1.2M');
  });

  it('uses one decimal k between 1k and 10k', () => {
    expect(formatTokens(1500)).toBe('1.5k');
  });

  it('leaves small counts as-is', () => {
    expect(formatTokens(150)).toBe('150');
  });
});

describe('formatRelativeTime', () => {
  const now = Date.parse('2026-09-10T12:00:00Z');

  it('rounds to the coarsest unit that fits', () => {
    expect(formatRelativeTime('2026-09-10T11:59:40Z', now)).toBe('just now');
    expect(formatRelativeTime('2026-09-10T11:55:00Z', now)).toBe('5 min ago');
    expect(formatRelativeTime('2026-09-10T06:00:00Z', now)).toBe('6 h ago');
    expect(formatRelativeTime('2026-09-07T12:00:00Z', now)).toBe('3 d ago');
  });

  it('never reports a future stamp as negative and names an unparseable one', () => {
    expect(formatRelativeTime('2026-09-10T12:00:30Z', now)).toBe('just now');
    expect(formatRelativeTime('not a date', now)).toBe('unknown');
  });
});
