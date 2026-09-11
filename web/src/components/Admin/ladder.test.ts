import { describe, expect, it } from 'vitest';
import type { SelectorCandidate, SelectorLadders, TierBars } from '../../types';
import {
  bandsFor,
  barRange,
  clampBar,
  formatBar,
  isMonotone,
  ladderKey,
  laddersEqual,
  panelHasListPrice,
  railValue,
  railY,
  snapBar,
  tierOf,
  usdPerMillion,
} from './ladder';
import { seat } from './selector.fixtures';

const DEFAULTS: TierBars = { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.9 };

function cand(slug: string, coder: number, reviewer: number): SelectorCandidate {
  return { slug, creator: slug.split('/')[0], coder_prior: coder, reviewer_prior: reviewer, prompt_price_per_tok: 1e-6, completion_price_per_tok: 1e-6, context_window: 200000, price_source: 'gateway', scored_from: '' };
}

describe('tierOf', () => {
  it('returns the strictest tier the prior clears, null below every bar', () => {
    expect(tierOf(DEFAULTS, 0.95)).toBe('critical');
    expect(tierOf(DEFAULTS, 0.9)).toBe('critical');
    expect(tierOf(DEFAULTS, 0.85)).toBe('complex');
    expect(tierOf(DEFAULTS, 0.76)).toBe('moderate');
    expect(tierOf(DEFAULTS, 0.7)).toBe('simple');
    expect(tierOf(DEFAULTS, 0.6)).toBeNull();
  });
});

describe('rail geometry', () => {
  it('maps the axis ends to 0% and 100% and clamps outside priors to the edge', () => {
    expect(railY(1)).toBe(0);
    expect(railY(0.35)).toBe(100);
    expect(railY(0.2)).toBe(100);
    expect(railY(0.675)).toBeCloseTo(50, 9);
  });

  it('snaps a rail fraction to a 0.005 step', () => {
    expect(railValue(0)).toBe(1);
    expect(railValue(0.2)).toBeCloseTo(0.87, 9);
    expect(snapBar(0.8574)).toBeCloseTo(0.855, 9);
    expect(snapBar(0.8576)).toBeCloseTo(0.86, 9);
  });
});

describe('clampBar and barRange', () => {
  it('keeps a bar between its neighbours in the same ladder', () => {
    expect(clampBar(DEFAULTS, 'complex', 0.95, 0.65)).toBe(0.9);
    expect(clampBar(DEFAULTS, 'complex', 0.7, 0.65)).toBe(0.76);
    expect(clampBar(DEFAULTS, 'complex', 0.855, 0.65)).toBe(0.855);
  });

  it('keeps the lowest bar above the floor and the highest at or below 1', () => {
    expect(clampBar(DEFAULTS, 'simple', 0.5, 0.65)).toBe(0.65);
    expect(clampBar(DEFAULTS, 'critical', 1.2, 0.65)).toBe(1);
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

describe('bandsFor', () => {
  it('draws five bands strictest first and counts members by membership', () => {
    const cands = [cand('a/top', 1, 0.5), cand('a/mid', 0.85, 0.85), cand('b/low', 0.7, 0.95), cand('c/floor', 0.5, 0.4)];
    const bands = bandsFor(DEFAULTS, cands, 'coder');
    expect(bands.map((b) => b.tier)).toEqual(['critical', 'complex', 'moderate', 'simple', null]);
    expect(bands.map((b) => b.count)).toEqual([1, 1, 0, 1, 1]);
    expect(bands[0].top).toBe(1);
    expect(bands[0].bottom).toBe(0.9);
    expect(bands[4].bottom).toBe(0.35);
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
});

describe('panelHasListPrice', () => {
  it('is true when any seat carries the AA list price', () => {
    expect(panelHasListPrice([seat('a/cheap', 2e-6, false), seat('b/pricey', 2e-5, true, 'complex', { price_source: 'aa' })])).toBe(true);
    expect(panelHasListPrice([seat('a/cheap', 2e-6, false)])).toBe(false);
    expect(panelHasListPrice([])).toBe(false);
  });
});
