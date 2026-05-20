import { describe, expect, test } from 'vitest';
import { deriveMetricsProps } from './metrics';

describe('deriveMetricsProps', () => {
  test('returns a stable shape when cards is empty', () => {
    const result = deriveMetricsProps({
      stateCounts: undefined,
      stateCountsParents: undefined,
      cards: [],
      cardsCompletedToday: 0,
      cardsCompletedTodayParents: undefined,
      cardsCompletedLast7d: undefined,
      cardsCompletedLast7dParents: undefined,
      cardsCompletedPrior7d: undefined,
      cardsCompletedPrior7dParents: undefined,
    });

    expect(result).toBeDefined();

    // All numeric fields must be finite (no NaN).
    for (const [key, v] of Object.entries(result)) {
      if (typeof v === 'number') {
        expect(Number.isFinite(v), `${key} must be finite`).toBe(true);
      }
    }
  });

  test('derives correct counts from stateCounts and stateCountsParents', () => {
    const stateCounts: Record<string, number> = {
      in_progress: 3,
      review: 2,
      stalled: 1,
      todo: 5,
      done: 10,
    };
    const stateCountsParents: Record<string, number> = {
      in_progress: 2,
      review: 1,
      stalled: 0,
      todo: 4,
      done: 8,
    };

    const result = deriveMetricsProps({
      stateCounts,
      stateCountsParents,
      cards: [],
      cardsCompletedToday: 4,
      cardsCompletedTodayParents: 3,
      cardsCompletedLast7d: 20,
      cardsCompletedLast7dParents: 15,
      cardsCompletedPrior7d: 18,
      cardsCompletedPrior7dParents: 14,
    });

    // inFlightTotal = in_progress(3) + review(2) = 5
    // inFlightParents = in_progress(2) + review(1) = 3
    expect(result.inFlightParents).toBe(3);
    expect(result.inFlightSubtasks).toBe(2); // 5 - 3

    // stalledTotal = stalled(1), stalledParents = stalled(0)
    expect(result.stalledParents).toBe(0);
    expect(result.stalledSubtasks).toBe(1); // 1 - 0

    // openCount: sum of stateCountsParents excluding done/not_planned
    // in_progress(2) + review(1) + stalled(0) + todo(4) = 7
    expect(result.openCount).toBe(7);
    expect(result.inReviewCount).toBe(1);

    // shippedToday: cardsCompletedTodayParents = 3
    expect(result.shippedTodayParents).toBe(3);
    expect(result.shippedTodaySubtasks).toBe(1); // 4 - 3

    // shipped7d: cardsCompletedLast7dParents = 15
    expect(result.shippedLast7dParents).toBe(15);
    expect(result.shipped7dSubtasks).toBe(5); // 20 - 15

    expect(result.shippedPrior7dParents).toBe(14);
  });

  test('falls back to cards-derived counts when stateCounts is missing', () => {
    const cards = [
      { id: 'A-001', state: 'in_progress', parent: '', title: 't' } as never,
      { id: 'A-002', state: 'review', parent: 'A-001', title: 't' } as never,
      { id: 'A-003', state: 'stalled', parent: '', title: 't' } as never,
    ];

    const result = deriveMetricsProps({
      stateCounts: undefined,
      stateCountsParents: undefined,
      cards,
      cardsCompletedToday: 2,
      cardsCompletedTodayParents: undefined,
      cardsCompletedLast7d: undefined,
      cardsCompletedLast7dParents: undefined,
      cardsCompletedPrior7d: undefined,
      cardsCompletedPrior7dParents: undefined,
    });

    // cards-derived: in_progress(1) + review(1) = 2
    expect(result.inFlightParents).toBe(2);
    // stateCountsParents missing → subtasks undefined (suffix suppressed)
    expect(result.inFlightSubtasks).toBeUndefined();

    // stalled = 1
    expect(result.stalledParents).toBe(1);
    expect(result.stalledSubtasks).toBeUndefined();

    // shippedToday falls back to cardsCompletedToday when parents missing
    expect(result.shippedTodayParents).toBe(2);
    expect(result.shippedTodaySubtasks).toBeUndefined();
  });
});
