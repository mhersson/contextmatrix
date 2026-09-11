import type { SelectorCandidate, SelectorLadders, SelectorRole, SelectorSeat, SelectorTier, TierBars } from '../../types';

/** Tiers from the strictest bar down - the order the rail draws them. */
export const TIERS_DESC: SelectorTier[] = ['critical', 'complex', 'moderate', 'simple'];
/** Tiers from the lowest bar up - the order a ladder must be non-decreasing in. */
export const TIERS_ASC: SelectorTier[] = ['simple', 'moderate', 'complex', 'critical'];
export const ROLES: SelectorRole[] = ['coder', 'reviewer'];

/** Palette token per tier; below-floor uses the neutral border token. */
export const TIER_COLOR: Record<SelectorTier, string> = {
  critical: 'var(--red)',
  complex: 'var(--yellow)',
  moderate: 'var(--aqua)',
  simple: 'var(--green)',
};
export const BELOW_FLOOR_COLOR = 'var(--bg3)';

/** Drag resolution: bars move in steps of 0.005. */
export const BAR_STEP = 0.005;
/** The rail spans this prior range; a candidate below it sits at the bottom edge. */
export const AXIS_MIN = 0.35;
export const AXIS_MAX = 1;

export function priorOf(c: SelectorCandidate, role: SelectorRole): number {
  return role === 'coder' ? c.coder_prior : c.reviewer_prior;
}

export function blendedPrice(c: SelectorCandidate): number {
  return c.prompt_price_per_tok + c.completion_price_per_tok;
}

/** Strictest tier whose bar the prior clears, null when it clears none. */
export function tierOf(bars: TierBars, prior: number): SelectorTier | null {
  for (const t of TIERS_DESC) {
    if (prior >= bars[t]) return t;
  }
  return null;
}

/** Vertical position on the rail as a percentage from the top. */
export function railY(prior: number): number {
  const clamped = Math.min(AXIS_MAX, Math.max(AXIS_MIN, prior));
  return (1 - (clamped - AXIS_MIN) / (AXIS_MAX - AXIS_MIN)) * 100;
}

/** Prior at a fraction of the rail height from the top, snapped to BAR_STEP. */
export function railValue(fraction: number): number {
  return snapBar(AXIS_MAX - fraction * (AXIS_MAX - AXIS_MIN));
}

export function snapBar(v: number): number {
  return round3(Math.round(v / BAR_STEP) * BAR_STEP);
}

/** Guards against 0.855000000001 after a step multiplication. */
export function round3(v: number): number {
  return Math.round(v * 1000) / 1000;
}

/**
 * The range a bar for `tier` may take across every ladder in `roles`: above
 * the lower neighbour (or the floor) and below the upper neighbour (or 1) in
 * each of them. Null when the ranges do not meet, which a linked drag treats
 * as "hold still" rather than breaking one ladder's order.
 */
export function barRange(
  ladders: SelectorLadders,
  roles: SelectorRole[],
  tier: SelectorTier,
  floor: number,
): [number, number] | null {
  const idx = TIERS_ASC.indexOf(tier);
  let lo = floor;
  let hi = AXIS_MAX;
  for (const r of roles) {
    if (idx > 0) lo = Math.max(lo, ladders[r][TIERS_ASC[idx - 1]]);
    if (idx < TIERS_ASC.length - 1) hi = Math.min(hi, ladders[r][TIERS_ASC[idx + 1]]);
  }
  return lo <= hi + 1e-9 ? [round3(lo), round3(hi)] : null;
}

/** Clamp a bar for `tier` between its neighbours in `bars` and above `floor`. */
export function clampBar(bars: TierBars, tier: SelectorTier, value: number, floor: number): number {
  const range = barRange({ coder: bars, reviewer: bars }, ['coder'], tier, floor);
  if (!range) return round3(bars[tier]);
  return round3(Math.max(range[0], Math.min(range[1], value)));
}

export interface Band {
  tier: SelectorTier | null;
  /** Prior at the band's upper edge. */
  top: number;
  /** Prior at the band's lower edge. */
  bottom: number;
  count: number;
}

/** The five bands a column draws under `bars`, strictest first; the last is below floor. */
export function bandsFor(bars: TierBars, candidates: SelectorCandidate[], role: SelectorRole): Band[] {
  const edges = [AXIS_MAX, ...TIERS_DESC.map((t) => bars[t]), AXIS_MIN];
  return edges.slice(0, -1).map((top, i) => {
    const tier = TIERS_DESC[i] ?? null;
    const count = candidates.filter((c) => tierOf(bars, priorOf(c, role)) === tier).length;
    return { tier, top, bottom: edges[i + 1], count };
  });
}

export function laddersEqual(a: SelectorLadders, b: SelectorLadders): boolean {
  return ROLES.every((r) => TIERS_ASC.every((t) => Math.abs(a[r][t] - b[r][t]) < 1e-9));
}

export function isMonotone(bars: TierBars): boolean {
  return TIERS_ASC.every((t, i) => i === 0 || bars[t] >= bars[TIERS_ASC[i - 1]]);
}

export function cloneLadders(l: SelectorLadders): SelectorLadders {
  return { coder: { ...l.coder }, reviewer: { ...l.reviewer } };
}

/** A stable key for a ladder value: same values, same key. */
export function ladderKey(l: SelectorLadders): string {
  return ROLES.map((r) => TIERS_ASC.map((t) => l[r][t].toFixed(3)).join(',')).join('|');
}

/** 0.9 -> "0.90", 0.855 -> "0.855". */
export function formatBar(v: number): string {
  const s = v.toFixed(3);
  return s.endsWith('0') ? s.slice(0, -1) : s;
}

/** Per-token price as dollars per million tokens: 2e-6 -> "$2.0/M", 6.5e-7 -> "$0.65/M". */
export function usdPerMillion(perTok: number): string {
  const m = perTok * 1e6;
  return `$${m.toFixed(m < 1 ? 2 : 1)}/M`;
}

/** The part of a slug after the vendor prefix. */
export function shortSlug(slug: string): string {
  const i = slug.indexOf('/');
  return i >= 0 ? slug.slice(i + 1) : slug;
}

/** Blended per-token price of a whole panel; an unfilled panel costs nothing. */
export function panelPrice(seats: SelectorSeat[]): number {
  return seats.reduce((sum, s) => sum + s.pick.price_per_tok, 0);
}
