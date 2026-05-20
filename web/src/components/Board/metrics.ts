import type { Card } from '../../types';

export interface DeriveMetricsInput {
  stateCounts?: Record<string, number>;
  stateCountsParents?: Record<string, number>;
  cards: Card[];
  cardsCompletedToday: number;
  cardsCompletedTodayParents?: number;
  cardsCompletedLast7d?: number;
  cardsCompletedLast7dParents?: number;
  cardsCompletedPrior7d?: number;
  cardsCompletedPrior7dParents?: number;
}

export interface DeriveMetricsResult {
  // For BoardBand
  openCount: number;
  inReviewCount: number;
  shippedTodayParents: number;
  shippedLast7dParents: number | undefined;
  shippedPrior7dParents: number | undefined;
  // For MetricsRibbon
  inFlightParents: number;
  inFlightSubtasks: number | undefined;
  stalledParents: number;
  stalledSubtasks: number | undefined;
  shippedTodaySubtasks: number | undefined;
  shipped7dSubtasks: number | undefined;
}

/**
 * Derives all computed props for MetricsRibbon and BoardBand from the raw
 * dashboard counts and card list. Pure function — no React hooks, no side effects.
 *
 * Fallback contracts when source data is partial:
 *   - inFlight/stalled: total falls back to cards-derived count so the headline
 *     is populated during initial mount; *Subtasks is undefined when stateCountsParents
 *     is missing (suffix suppressed until parent data arrives).
 *   - shippedToday: total is provided by the caller; *Subtasks is undefined when
 *     cardsCompletedTodayParents is missing.
 *   - shipped7d: total and *Subtasks are both undefined when cardsCompletedLast7d/
 *     Parents are missing (the tile hides its delta+suffix entirely).
 */
export function deriveMetricsProps({
  stateCounts,
  stateCountsParents,
  cards,
  cardsCompletedToday,
  cardsCompletedTodayParents,
  cardsCompletedLast7d,
  cardsCompletedLast7dParents,
  cardsCompletedPrior7d,
  cardsCompletedPrior7dParents,
}: DeriveMetricsInput): DeriveMetricsResult {
  // inFlightTotal / stalledTotal: prefer server-side stateCounts (unfiltered, so they
  // agree with stateCountsParents); fall back to a cards-derived count so the headline
  // is populated during the initial mount before the dashboard fetch resolves.
  const inFlightTotal = stateCounts
    ? (stateCounts['in_progress'] ?? 0) + (stateCounts['review'] ?? 0)
    : cards.filter((c) => c.state === 'in_progress' || c.state === 'review').length;
  const stalledTotal = stateCounts
    ? (stateCounts['stalled'] ?? 0)
    : cards.filter((c) => c.state === 'stalled').length;

  // openCount + inReviewCount: BoardBand subheader counts delivery units only
  // (parents + standalone cards), so subtasks do not inflate the rolling
  // headline. Prefer server-side stateCountsParents; fall back to filtering
  // cards by !parent. openCount keeps the pre-PR semantics — stalled counts
  // as open (only done/not_planned are excluded).
  const openCount = stateCountsParents !== undefined
    ? Object.entries(stateCountsParents).reduce(
        (sum, [state, n]) =>
          state === 'done' || state === 'not_planned' ? sum : sum + n,
        0,
      )
    : cards.filter(
        (c) => !c.parent && c.state !== 'done' && c.state !== 'not_planned',
      ).length;
  const inReviewCount = stateCountsParents !== undefined
    ? stateCountsParents['review'] ?? 0
    : cards.filter((c) => !c.parent && c.state === 'review').length;

  // Parent-only headline counts for MetricsRibbon. Fall back to totals when
  // stateCountsParents is not yet available (e.g. dashboard not loaded yet).
  const inFlightParents = stateCountsParents !== undefined
    ? (stateCountsParents['in_progress'] ?? 0) + (stateCountsParents['review'] ?? 0)
    : inFlightTotal;
  const stalledParents = stateCountsParents !== undefined
    ? (stateCountsParents['stalled'] ?? 0)
    : stalledTotal;

  // Compute *Subtasks only when stateCountsParents is available (inFlightTotal is
  // always a number now). Otherwise pass undefined — suppresses the muted "+N sub"
  // suffix until parent data is ready.
  const inFlightSubtasks =
    stateCountsParents !== undefined
      ? inFlightTotal - inFlightParents
      : undefined;
  const stalledSubtasks =
    stateCountsParents !== undefined
      ? stalledTotal - stalledParents
      : undefined;

  const shippedTodayParents = cardsCompletedTodayParents ?? cardsCompletedToday;
  const shippedTodaySubtasks =
    cardsCompletedTodayParents !== undefined
      ? cardsCompletedToday - shippedTodayParents
      : undefined;

  const shippedLast7dParents = cardsCompletedLast7dParents ?? cardsCompletedLast7d;
  const shipped7dSubtasks = cardsCompletedLast7d !== undefined && shippedLast7dParents !== undefined
    ? cardsCompletedLast7d - shippedLast7dParents
    : undefined;

  const shippedPrior7dParents = cardsCompletedPrior7dParents ?? cardsCompletedPrior7d;

  return {
    openCount,
    inReviewCount,
    shippedTodayParents,
    shippedLast7dParents,
    shippedPrior7dParents,
    inFlightParents,
    inFlightSubtasks,
    stalledParents,
    stalledSubtasks,
    shippedTodaySubtasks,
    shipped7dSubtasks,
  };
}
