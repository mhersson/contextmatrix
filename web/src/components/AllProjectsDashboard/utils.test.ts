import { describe, expect, it } from 'vitest';
import type { DashboardData } from '../../types';
import { aggregateDashboards } from './utils';

function summary(overrides: Partial<DashboardData> = {}): DashboardData {
  return {
    state_counts: {},
    state_counts_parents: {},
    active_agents: [],
    total_cost_usd: 0,
    total_cost_usd_last_30d: 0,
    total_cost_usd_prior_30d: 0,
    cost_series_30d: Array(30).fill(0),
    cards_completed_today: 0,
    cards_completed_today_parents: 0,
    cards_completed_last_7d: 0,
    cards_completed_last_7d_parents: 0,
    cards_completed_prior_7d: 0,
    cards_completed_prior_7d_parents: 0,
    metric_series: {
      active_agents: [],
      in_flight: [],
      stalled: [],
      shipped: [],
      in_flight_parents: [],
      stalled_parents: [],
      shipped_parents: [],
    },
    agent_costs: [],
    card_costs: [],
    model_costs: [],
    ...overrides,
  };
}

describe('aggregateDashboards model_costs fold', () => {
  it('sums the same model across projects', () => {
    const a = summary({
      model_costs: [
        {
          model: 'claude-opus-4-7',
          prompt_tokens: 100,
          completion_tokens: 50,
          estimated_cost_usd: 1.0,
          card_count: 1,
        },
      ],
    });
    const b = summary({
      model_costs: [
        {
          model: 'claude-opus-4-7',
          prompt_tokens: 200,
          completion_tokens: 60,
          estimated_cost_usd: 2.5,
          card_count: 2,
        },
      ],
    });
    const result = aggregateDashboards(
      new Map([
        ['proj-a', a],
        ['proj-b', b],
      ]),
    );
    expect(result.model_costs).toHaveLength(1);
    const opus = result.model_costs[0];
    expect(opus.model).toBe('claude-opus-4-7');
    expect(opus.prompt_tokens).toBe(300);
    expect(opus.completion_tokens).toBe(110);
    expect(opus.estimated_cost_usd).toBeCloseTo(3.5, 9);
    expect(opus.card_count).toBe(3);
  });

  it('keeps distinct models separate', () => {
    const a = summary({
      model_costs: [
        {
          model: 'claude-opus-4-7',
          prompt_tokens: 1,
          completion_tokens: 1,
          estimated_cost_usd: 0.1,
          card_count: 1,
        },
        {
          model: 'unknown',
          prompt_tokens: 2,
          completion_tokens: 1,
          estimated_cost_usd: 0.2,
          card_count: 1,
        },
      ],
    });
    const result = aggregateDashboards(new Map([['proj-a', a]]));
    const models = result.model_costs.map((m) => m.model).sort();
    expect(models).toEqual(['claude-opus-4-7', 'unknown']);
  });

  it('returns an empty model_costs array for an empty input', () => {
    const result = aggregateDashboards(new Map());
    expect(result.model_costs).toEqual([]);
  });

  it('does not mutate input model_costs entries', () => {
    const input = summary({
      model_costs: [
        {
          model: 'claude-opus-4-7',
          prompt_tokens: 100,
          completion_tokens: 50,
          estimated_cost_usd: 1.0,
          card_count: 1,
        },
      ],
    });
    const snapshot = { ...input.model_costs[0] };
    aggregateDashboards(
      new Map([
        ['proj-a', input],
        ['proj-b', input],
      ]),
    );
    expect(input.model_costs[0]).toEqual(snapshot);
  });
});

describe('aggregateDashboards parent-only fields', () => {
  it('sums state_counts_parents across projects', () => {
    const a = summary({
      state_counts: { todo: 10, in_progress: 3 },
      state_counts_parents: { todo: 4, in_progress: 1 },
      cards_completed_today: 2,
      cards_completed_today_parents: 1,
      cards_completed_last_7d: 8,
      cards_completed_last_7d_parents: 3,
      cards_completed_prior_7d: 12,
      cards_completed_prior_7d_parents: 5,
    });
    const b = summary({
      state_counts: { todo: 5, review: 2 },
      state_counts_parents: { todo: 2, review: 1 },
      cards_completed_today: 1,
      cards_completed_today_parents: 0,
      cards_completed_last_7d: 4,
      cards_completed_last_7d_parents: 2,
      cards_completed_prior_7d: 6,
      cards_completed_prior_7d_parents: 2,
    });

    const result = aggregateDashboards(new Map([['a', a], ['b', b]]));

    // Parent-only state counts summed correctly
    expect(result.state_counts_parents['todo']).toBe(6);
    expect(result.state_counts_parents['in_progress']).toBe(1);
    expect(result.state_counts_parents['review']).toBe(1);

    // Parent-only completion counters summed correctly
    expect(result.cards_completed_today_parents).toBe(1);
    expect(result.cards_completed_last_7d_parents).toBe(5);
    expect(result.cards_completed_prior_7d_parents).toBe(7);
  });

  it('preserves all-cards aggregations alongside parent-only fields', () => {
    const a = summary({
      state_counts: { todo: 10, in_progress: 3 },
      state_counts_parents: { todo: 4, in_progress: 1 },
      cards_completed_today: 2,
      cards_completed_today_parents: 1,
    });
    const b = summary({
      state_counts: { todo: 5, review: 2 },
      state_counts_parents: { todo: 2, review: 1 },
      cards_completed_today: 1,
      cards_completed_today_parents: 0,
    });

    const result = aggregateDashboards(new Map([['a', a], ['b', b]]));

    // All-cards aggregations unchanged
    expect(result.state_counts['todo']).toBe(15);
    expect(result.state_counts['in_progress']).toBe(3);
    expect(result.state_counts['review']).toBe(2);
    expect(result.cards_completed_today).toBe(3);

    // Parent-only totals are lower (subset)
    expect(result.state_counts_parents['todo']).toBe(6);
    expect(result.cards_completed_today_parents).toBe(1);
  });

  it('handles missing state_counts_parents gracefully (treats as empty)', () => {
    // Old-format data that might not have the parents field
    const a = summary({ state_counts: { todo: 5 } });
    // Force the field to be missing to simulate old data
    (a as unknown as Record<string, unknown>)['state_counts_parents'] = undefined;

    const result = aggregateDashboards(new Map([['a', a]]));
    expect(result.state_counts_parents).toEqual({});
    expect(result.cards_completed_today_parents).toBe(0);
  });

  it('returns empty parent counts for empty input', () => {
    const result = aggregateDashboards(new Map());
    expect(result.state_counts_parents).toEqual({});
    expect(result.cards_completed_today_parents).toBe(0);
    expect(result.cards_completed_last_7d_parents).toBe(0);
    expect(result.cards_completed_prior_7d_parents).toBe(0);
  });
});

describe('aggregateDashboards 30-day cost fields', () => {
  it('sums total_cost_usd_last_30d and total_cost_usd_prior_30d across projects', () => {
    const a = summary({ total_cost_usd_last_30d: 1.5, total_cost_usd_prior_30d: 0.8 });
    const b = summary({ total_cost_usd_last_30d: 2.0, total_cost_usd_prior_30d: 1.2 });

    const result = aggregateDashboards(new Map([['a', a], ['b', b]]));

    expect(result.total_cost_usd_last_30d).toBeCloseTo(3.5, 9);
    expect(result.total_cost_usd_prior_30d).toBeCloseTo(2.0, 9);
  });

  it('sums cost_series_30d element-wise across projects', () => {
    const seriesA = Array.from({ length: 30 }, (_, i) => i * 0.1);
    const seriesB = Array.from({ length: 30 }, (_, i) => i * 0.2);
    const a = summary({ cost_series_30d: seriesA });
    const b = summary({ cost_series_30d: seriesB });

    const result = aggregateDashboards(new Map([['a', a], ['b', b]]));

    expect(result.cost_series_30d).toHaveLength(30);
    for (let i = 0; i < 30; i++) {
      expect(result.cost_series_30d![i]).toBeCloseTo(seriesA[i] + seriesB[i], 9);
    }
  });

  it('falls back to 0 / zero-array when fields are missing from one project', () => {
    // Seed a non-trivial series on the project that has data.
    const seededSeries = Array.from({ length: 30 }, (_, i) => (i + 1) * 0.5);
    const withData = summary({
      total_cost_usd_last_30d: 5.0,
      total_cost_usd_prior_30d: 3.0,
      cost_series_30d: seededSeries,
    });
    // Simulate an older server response that omits the new fields entirely.
    const withoutData = summary();
    delete (withoutData as Partial<DashboardData>).total_cost_usd_last_30d;
    delete (withoutData as Partial<DashboardData>).total_cost_usd_prior_30d;
    delete (withoutData as Partial<DashboardData>).cost_series_30d;

    const result = aggregateDashboards(new Map([['a', withData], ['b', withoutData]]));

    expect(result.total_cost_usd_last_30d).toBeCloseTo(5.0, 9);
    expect(result.total_cost_usd_prior_30d).toBeCloseTo(3.0, 9);
    expect(result.cost_series_30d).toHaveLength(30);
    // No NaN values in the output series.
    for (const v of result.cost_series_30d!) {
      expect(Number.isNaN(v)).toBe(false);
    }
    // Seeded values from withData flow through element-wise (withoutData has no series).
    expect(result.cost_series_30d![14]).toBeCloseTo(seededSeries[14], 9);
  });

  it('returns undefined cost_series_30d for empty input (no projects supplied a series)', () => {
    const result = aggregateDashboards(new Map());
    expect(result.cost_series_30d).toBeUndefined();
    expect(result.total_cost_usd_last_30d).toBe(0);
    expect(result.total_cost_usd_prior_30d).toBe(0);
  });
});

describe('aggregateDashboards chat cost picker', () => {
  it('picker_picks_first_numeric_even_when_zero — preserves zero last30d with non-zero prior30d', () => {
    // First response has chat_cost_usd_last_30d: 0 and chat_cost_usd_prior_30d: 50.
    // A truthiness-based picker would drop this and pick nothing (or a wrong source).
    const first = summary({
      chat_cost_usd_last_30d: 0,
      chat_cost_usd_prior_30d: 50,
    });
    const second = summary({
      chat_cost_usd_last_30d: 12.5,
      chat_cost_usd_prior_30d: 10,
    });

    const summaries = new Map([
      ['proj-a', first],
      ['proj-b', second],
    ]);
    const result = aggregateDashboards(summaries);

    // First response wins (it has a numeric last30d, even though it is zero).
    expect(result.chat_cost_usd_last_30d).toBe(0);
    expect(result.chat_cost_usd_prior_30d).toBe(50);
  });

  it('picker_skips_undefined — falls back to second response when first has no chat cost', () => {
    const first = summary({
      // chat_cost_usd_last_30d is absent (undefined)
    });
    const second = summary({
      chat_cost_usd_last_30d: 12.5,
      chat_cost_usd_prior_30d: 10,
    });

    const summaries = new Map([
      ['proj-a', first],
      ['proj-b', second],
    ]);
    const result = aggregateDashboards(summaries);

    // Second response wins because first has no numeric last30d.
    expect(result.chat_cost_usd_last_30d).toBe(12.5);
    expect(result.chat_cost_usd_prior_30d).toBe(10);
  });

  it('returns undefined chat cost fields when no response has them', () => {
    const summaries = new Map([
      ['proj-a', summary()],
      ['proj-b', summary()],
    ]);
    const result = aggregateDashboards(summaries);

    expect(result.chat_cost_usd_last_30d).toBeUndefined();
    expect(result.chat_cost_usd_prior_30d).toBeUndefined();
    expect(result.chat_cost_series_30d).toBeUndefined();
  });
});
