import { describe, expect, it } from 'vitest';
import { formatCost, formatTokens } from './format';

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
