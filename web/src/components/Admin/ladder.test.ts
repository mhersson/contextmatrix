import { describe, expect, it } from 'vitest';
import type { SelectorLadders, TierBars } from '../../types';
import { barRange, clearedTiers, formatBar, isMonotone, ladderKey, laddersEqual, money, panelHasListPrice, snapBar, usdPerMillion } from './ladder';
import { seat } from './selector.fixtures';

const DEFAULTS: TierBars = { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.9 };

describe('clearedTiers', () => {
  it('lists every tier whose bar the prior reaches, lowest first', () => {
    expect(clearedTiers(DEFAULTS, 0.85)).toEqual(['simple', 'moderate', 'complex']);
    expect(clearedTiers(DEFAULTS, 0.9)).toEqual(['simple', 'moderate', 'complex', 'critical']);
    expect(clearedTiers(DEFAULTS, 0.6)).toEqual([]);
  });
});

describe('snapBar', () => {
  it('snaps to a 0.005 step', () => {
    expect(snapBar(0.8574)).toBeCloseTo(0.855, 9);
    expect(snapBar(0.8576)).toBeCloseTo(0.86, 9);
  });
});

describe('barRange', () => {
  const one: SelectorLadders = { coder: DEFAULTS, reviewer: DEFAULTS };

  it('keeps a bar between its neighbours in the same ladder', () => {
    expect(barRange(one, ['coder'], 'complex', 0.65)).toEqual([0.76, 0.9]);
  });

  it('keeps the lowest bar above the floor and the highest at or below 1', () => {
    expect(barRange(one, ['coder'], 'simple', 0.6)).toEqual([0.6, 0.76]);
    expect(barRange(one, ['coder'], 'critical', 0.65)).toEqual([0.82, 1]);
  });

  it('intersects the ranges of every ladder a linked drag writes to', () => {
    const ladders: SelectorLadders = {
      coder: { simple: 0.65, moderate: 0.8, complex: 0.9, critical: 0.95 },
      reviewer: { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.86 },
    };
    expect(barRange(ladders, ['coder', 'reviewer'], 'complex', 0.65)).toEqual([0.8, 0.86]);
    expect(barRange(ladders, ['coder'], 'complex', 0.65)).toEqual([0.8, 0.95]);
  });

  it('reports no range when the ladders cannot share a value', () => {
    const ladders: SelectorLadders = {
      coder: { simple: 0.65, moderate: 0.85, complex: 0.9, critical: 0.95 },
      reviewer: { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.83 },
    };
    expect(barRange(ladders, ['coder', 'reviewer'], 'complex', 0.65)).toBeNull();
  });
});

describe('ladder equality and monotonicity', () => {
  const a: SelectorLadders = { coder: { ...DEFAULTS }, reviewer: { ...DEFAULTS } };

  it('compares by value with a tolerance', () => {
    expect(laddersEqual(a, { coder: { ...DEFAULTS }, reviewer: { ...DEFAULTS, critical: 0.9 + 1e-12 } })).toBe(true);
    expect(laddersEqual(a, { coder: { ...DEFAULTS }, reviewer: { ...DEFAULTS, critical: 0.93 } })).toBe(false);
  });

  it('keys equal ladders identically', () => {
    expect(ladderKey(a)).toBe(ladderKey({ coder: { ...DEFAULTS }, reviewer: { ...DEFAULTS } }));
    expect(ladderKey(a)).not.toBe(ladderKey({ coder: { ...DEFAULTS, complex: 0.85 }, reviewer: { ...DEFAULTS } }));
  });

  it('accepts equal neighbours and rejects a lower rung above a higher one', () => {
    expect(isMonotone(DEFAULTS)).toBe(true);
    expect(isMonotone({ ...DEFAULTS, complex: 0.76 })).toBe(true);
    expect(isMonotone({ ...DEFAULTS, complex: 0.7 })).toBe(false);
  });
});

describe('formatting', () => {
  it('drops a trailing zero from three decimals', () => {
    expect(formatBar(0.9)).toBe('0.90');
    expect(formatBar(0.855)).toBe('0.855');
    expect(formatBar(1)).toBe('1.00');
  });

  it('prices per million tokens with two decimals under a dollar', () => {
    expect(usdPerMillion(2e-6)).toBe('$2.0/M');
    expect(usdPerMillion(6.5e-7)).toBe('$0.65/M');
    expect(usdPerMillion(1.2e-5)).toBe('$12.0/M');
  });

  it('keeps three significant figures so a tiny price never prints as zero', () => {
    expect(money(2e-6)).toBe('$2');
    expect(money(1.5e-8)).toBe('$0.015');
    expect(money(1.23456e-5)).toBe('$12.3');
  });
});

describe('panelHasListPrice', () => {
  it('is true when any seat carries the AA list price', () => {
    expect(panelHasListPrice([seat('a/cheap', 2e-6, false), seat('b/pricey', 2e-5, true, 'complex', { price_source: 'aa' })])).toBe(true);
    expect(panelHasListPrice([seat('a/cheap', 2e-6, false)])).toBe(false);
    expect(panelHasListPrice([])).toBe(false);
  });
});
