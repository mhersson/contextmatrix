import { describe, expect, it } from 'vitest';
import type { DashboardData } from '../../types';
import { aggregateDashboards } from './utils';

function summary(overrides: Partial<DashboardData> = {}): DashboardData {
  return {
    state_counts: {},
    active_agents: [],
    total_cost_usd: 0,
    cards_completed_today: 0,
    cards_completed_last_7d: 0,
    cards_completed_prior_7d: 0,
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
