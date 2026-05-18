import { describe, expect, it } from 'vitest';
import type { DashboardData } from '../../types';
import { aggregateDashboards } from './utils';

function summary(overrides: Partial<DashboardData> = {}): DashboardData {
  return {
    state_counts: {},
    state_counts_parents: {},
    active_agents: [],
    total_cost_usd: 0,
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
    (a as Record<string, unknown>)['state_counts_parents'] = undefined;

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
