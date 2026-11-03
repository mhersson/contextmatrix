import type { SelectorTier, TierBars } from '../../types';
import { TIERS_DESC, formatBar } from './ladder';

export const CHART_LAYOUT = {
  height: 440,
  top: 14,
  bottom: 40,
  left: 82,
  right: 92,
  gutter: 28,
  handleW: 44,
  handleH: 16,
} as const;

export const TIER_INITIAL: Record<SelectorTier, string> = {
  simple: 'S',
  moderate: 'M',
  complex: 'C',
  critical: 'X',
};

export interface PriceScale {
  x(perMillion: number): number;
  ticks: number[];
  domain: [number, number];
  /** The "no price" strip, present only when some price is zero. */
  gutter: [number, number] | null;
}

const DEFAULT_DOMAIN: [number, number] = [0.1, 100];

/** Log price axis over dollars per million tokens; prices at or below zero sit in a gutter. */
export function priceScale(pricesPerMillion: number[], x0: number, x1: number): PriceScale {
  const positive = pricesPerMillion.filter((p) => p > 0 && Number.isFinite(p));
  const hasZero = pricesPerMillion.some((p) => !(p > 0));

  let domain: [number, number] = DEFAULT_DOMAIN;
  if (positive.length > 0) {
    let lo = Math.min(...positive) / 1.6;
    let hi = Math.max(...positive) * 1.6;
    if (Math.log10(hi / lo) < 1) {
      const centre = Math.sqrt(lo * hi);
      lo = centre / Math.sqrt(10);
      hi = centre * Math.sqrt(10);
    }
    domain = [lo, hi];
  }

  const gutter: [number, number] | null = hasZero ? [x0, x0 + CHART_LAYOUT.gutter] : null;
  const axisStart = gutter ? gutter[1] : x0;
  const logLo = Math.log10(domain[0]);
  const span = Math.log10(domain[1]) - logLo;
  const gutterCentre = gutter ? (gutter[0] + gutter[1]) / 2 : x0;

  const x = (p: number): number => {
    if (!(p > 0)) return gutterCentre;
    return axisStart + ((Math.log10(p) - logLo) / span) * (x1 - axisStart);
  };

  const ticksFor = (mantissas: number[]): number[] => {
    const out: number[] = [];
    for (let k = Math.floor(logLo) - 1; k <= Math.ceil(logLo + span) + 1; k++) {
      for (const m of mantissas) {
        const t = Number((m * 10 ** k).toPrecision(12));
        if (t >= domain[0] && t <= domain[1]) out.push(t);
      }
    }
    return out;
  };
  // A span near one decade can hold only two 1-3 ticks; 1-2-5 has three in any decade.
  let ticks = ticksFor([1, 3]);
  if (ticks.length < 3) ticks = ticksFor([1, 2, 5]);

  return { x, ticks, domain, gutter };
}

export interface QualityScale {
  y(prior: number): number;
  /** Inverse of y, unclamped. */
  prior(py: number): number;
  lo: number;
  hi: number;
  ticks: number[];
}

export function qualityScale(floor: number, top: number, bottom: number): QualityScale {
  const lo = Math.max(0, Math.min(floor - 0.15, 0.3));
  const hi = 1.02;
  const span = hi - lo;
  const y = (prior: number): number => {
    const c = Math.min(hi, Math.max(lo, prior));
    return top + ((hi - c) / span) * (bottom - top);
  };
  const prior = (py: number): number => hi - ((py - top) / (bottom - top)) * span;

  const ticks: number[] = [];
  const first = Math.ceil(Number((lo * 10).toFixed(6)));
  for (let i = first; i <= 10; i++) ticks.push(Number((i / 10).toFixed(1)));

  return { y, prior, lo, hi, ticks };
}

export interface HandleSlot {
  tier: SelectorTier;
  lineY: number;
  handleY: number;
}

/**
 * Fans handles whose bar lines are closer than handleH apart so each stays
 * grabbable. With bounds (min and max handle centre), a cluster that would
 * leave them shifts back inside, which can merge it into its neighbour.
 */
export function stackHandles(bars: TierBars, y: (prior: number) => number, handleH: number, bounds?: [number, number]): HandleSlot[] {
  const items = TIERS_DESC.map((tier) => ({ tier, lineY: y(bars[tier]) }));
  // Stable sort keeps the strictest tier on top among equal lines.
  const sorted = [...items].sort((a, b) => a.lineY - b.lineY);

  const place = (members: typeof items): number[] => {
    const mean = members.reduce((s, m) => s + m.lineY, 0) / members.length;
    let first = mean - ((members.length - 1) * handleH) / 2;
    if (bounds) {
      const last = first + (members.length - 1) * handleH;
      if (first < bounds[0]) first = bounds[0];
      else if (last > bounds[1]) first -= last - bounds[1];
    }
    return members.map((_, i) => first + i * handleH);
  };

  let clusters = sorted.map((m) => [m]);
  let merged = true;
  while (merged) {
    merged = false;
    for (let i = 0; i < clusters.length - 1; i++) {
      const a = place(clusters[i]);
      const b = place(clusters[i + 1]);
      if (a[a.length - 1] + handleH > b[0] + 1e-9) {
        clusters = [...clusters.slice(0, i), [...clusters[i], ...clusters[i + 1]], ...clusters.slice(i + 2)];
        merged = true;
        break;
      }
    }
  }

  const handleYs = new Map<SelectorTier, number>();
  for (const c of clusters) {
    place(c).forEach((hy, i) => handleYs.set(c[i].tier, hy));
  }
  return items.map((m) => ({ tier: m.tier, lineY: m.lineY, handleY: handleYs.get(m.tier) ?? m.lineY }));
}

/** Label baselines for dots: each starts at y + 4 and lifts by lineH until clear of placed labels. */
export function placeLabels(points: ReadonlyArray<{ x: number; y: number }>, lineH = 12, reach = 120): number[] {
  const placed: { x: number; y: number }[] = [];
  return points.map((p) => {
    let baseline = p.y + 4;
    while (placed.some((q) => Math.abs(q.y - baseline) < lineH && Math.abs(q.x - p.x) < reach)) {
      baseline -= lineH;
    }
    placed.push({ x: p.x, y: baseline });
    return baseline;
  });
}

/** "C .84": tier initial, then the bar without its leading zero. */
export function formatHandle(tier: SelectorTier, bar: number): string {
  return `${TIER_INITIAL[tier]} ${formatBar(bar).replace(/^0\./, '.')}`;
}
