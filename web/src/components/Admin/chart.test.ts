import { describe, expect, it } from 'vitest';
import { CHART_LAYOUT, formatHandle, placeLabels, priceScale, qualityScale, stackHandles } from './chart';

describe('priceScale', () => {
  it('maps priced models monotonically inside the domain with no gutter', () => {
    const prices = [0.5, 2, 15, 60];
    const s = priceScale(prices, 82, 800);
    const xs = prices.map((p) => s.x(p));
    xs.forEach((x) => expect(Number.isFinite(x)).toBe(true));
    expect([...xs].sort((a, b) => a - b)).toEqual(xs);
    expect(s.gutter).toBeNull();
    expect(s.domain[0]).toBeLessThanOrEqual(0.5);
    expect(s.domain[1]).toBeGreaterThanOrEqual(60);
    s.ticks.forEach((t) => {
      expect(t).toBeGreaterThanOrEqual(s.domain[0]);
      expect(t).toBeLessThanOrEqual(s.domain[1]);
    });
    expect(s.ticks).toContain(1);
    expect(s.ticks).toContain(3);
  });

  it('puts a zero price in a gutter left of every priced model', () => {
    const s = priceScale([0, 1, 10], 82, 800);
    expect(s.gutter).toEqual([82, 82 + CHART_LAYOUT.gutter]);
    expect(s.x(0)).toBeGreaterThan(s.gutter![0]);
    expect(s.x(0)).toBeLessThan(s.gutter![1]);
    expect(s.x(0)).toBeLessThan(s.x(1));
    expect(s.x(1)).toBeGreaterThanOrEqual(s.gutter![1]);
  });

  it('stays finite when every price is zero', () => {
    const s = priceScale([0, 0], 82, 800);
    expect(s.domain).toEqual([0.1, 100]);
    expect(Number.isFinite(s.x(0))).toBe(true);
    expect(Number.isFinite(s.x(5))).toBe(true);
  });

  it('falls back to 1-2-5 ticks when the 1-3 sequence leaves fewer than three in the domain', () => {
    const s = priceScale([6, 60], 82, 800);
    expect(s.ticks).toEqual([5, 10, 20, 50]);
  });

  it('widens a single price to a decade', () => {
    const s = priceScale([2, 2], 82, 800);
    expect(s.domain[1] / s.domain[0]).toBeGreaterThanOrEqual(10 - 1e-9);
    expect(s.domain[0]).toBeLessThan(2);
    expect(s.domain[1]).toBeGreaterThan(2);
  });
});

describe('qualityScale', () => {
  it('clamps and inverts', () => {
    const q = qualityScale(0.65, 14, 400);
    expect(q.lo).toBeCloseTo(0.3);
    expect(q.hi).toBeCloseTo(1.02);
    expect(q.y(1.02)).toBeCloseTo(14);
    expect(q.y(0.3)).toBeCloseTo(400);
    expect(q.y(-1)).toBeCloseTo(400);
    expect(q.prior(q.y(0.84))).toBeCloseTo(0.84);
    expect(q.ticks[0]).toBe(0.3);
    expect(q.ticks[q.ticks.length - 1]).toBe(1);
  });

  it('keeps room under a low floor', () => {
    expect(qualityScale(0.2, 14, 400).lo).toBeCloseTo(0.05);
  });
});

describe('stackHandles', () => {
  const y = (p: number) => (1 - p) * 1000;

  it('fans equal bars apart, strictest on top', () => {
    const slots = stackHandles({ simple: 0.9, moderate: 0.9, complex: 0.9, critical: 0.92 }, y, 16);
    expect(slots.map((s) => s.tier)).toEqual(['critical', 'complex', 'moderate', 'simple']);
    [71, 87, 103, 119].forEach((want, i) => expect(slots[i].handleY).toBeCloseTo(want, 6));
  });

  it('leaves a spread ladder on its lines', () => {
    const slots = stackHandles({ simple: 0.65, moderate: 0.75, complex: 0.84, critical: 0.94 }, y, 16);
    slots.forEach((s) => expect(s.handleY).toBeCloseTo(s.lineY, 9));
  });

  it('keeps every handle centre inside the bounds, handleH apart', () => {
    for (const bar of [1, 0]) {
      const slots = stackHandles({ simple: bar, moderate: bar, complex: bar, critical: bar }, y, 16, [8, 992]);
      const ys = slots.map((s) => s.handleY);
      ys.forEach((hy) => {
        expect(hy).toBeGreaterThanOrEqual(8 - 1e-9);
        expect(hy).toBeLessThanOrEqual(992 + 1e-9);
      });
      for (let i = 1; i < ys.length; i++) expect(ys[i] - ys[i - 1]).toBeCloseTo(16, 9);
    }
  });

  it('merges only the near pair of a mixed ladder pushed off the top bound', () => {
    // Lines at y 0, 5, 50 and 300: the top pair fans to 8 and 24, clear of the line at 50.
    const slots = stackHandles({ simple: 0.7, moderate: 0.95, complex: 0.995, critical: 1 }, y, 16, [8, 992]);
    const at = Object.fromEntries(slots.map((s) => [s.tier, s]));
    expect(at.critical.handleY).toBeCloseTo(8, 9);
    expect(at.complex.handleY).toBeCloseTo(24, 9);
    expect(at.moderate.handleY).toBeCloseTo(at.moderate.lineY, 9);
    expect(at.simple.handleY).toBeCloseTo(at.simple.lineY, 9);
  });

  it('merges a cluster the bounds push into its neighbour', () => {
    // The pair on y 0 lands on 8 and 24, which covers the line at 30.
    const slots = stackHandles({ simple: 0.5, moderate: 0.97, complex: 1, critical: 1 }, y, 16, [8, 992]);
    [8, 24, 40].forEach((want, i) => expect(slots[i].handleY).toBeCloseTo(want, 9));
    expect(slots[3].handleY).toBeCloseTo(slots[3].lineY, 9);
  });
});

describe('placeLabels', () => {
  it('lifts a label that collides with a neighbour', () => {
    expect(placeLabels([{ x: 100, y: 50 }, { x: 110, y: 50 }, { x: 400, y: 50 }])).toEqual([54, 42, 54]);
  });
});

describe('formatHandle', () => {
  it('prints the initial and the bar', () => {
    expect(formatHandle('complex', 0.84)).toBe('C .84');
    expect(formatHandle('critical', 0.945)).toBe('X .945');
    expect(formatHandle('critical', 1)).toBe('X 1.00');
  });
});
