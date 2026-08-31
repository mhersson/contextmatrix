import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CostByModel } from './CostByModel';
import type { ModelCost } from '../../types';

function mc(model: string, cost: number): ModelCost {
  return {
    model,
    prompt_tokens: 0,
    completion_tokens: 0,
    estimated_cost_usd: cost,
    card_count: 1,
  };
}

describe('CostByModel', () => {
  it('renders one row per model, sorted by cost desc', () => {
    const { container } = render(
      <CostByModel
        modelCosts={[mc('claude-haiku-4-5', 2.5), mc('claude-opus-4-7', 9.1), mc('unknown', 0.4)]}
      />,
    );
    const rowModels = Array.from(container.querySelectorAll('.apd-cost-row')).map(
      (row) => row.querySelector('.apd-model-pill')?.textContent,
    );
    expect(rowModels).toEqual(['claude-opus-4-7', 'claude-haiku-4-5', 'unknown']);
  });

  it('renders every model, not a truncated top-N', () => {
    const many = Array.from({ length: 12 }, (_, i) => mc(`model-${i}`, i + 1));
    const { container } = render(<CostByModel modelCosts={many} />);
    expect(container.querySelectorAll('.apd-cost-row')).toHaveLength(12);
  });

  it('exposes the "last model wins" attribution note to keyboard and AT users', () => {
    render(<CostByModel modelCosts={[mc('claude-opus-4-7', 1)]} />);
    const note = screen.getByLabelText(/most-recently-used model/i);
    expect(note).toHaveAttribute('tabindex', '0');
  });

  it('renders an empty state when no costs are reported', () => {
    render(<CostByModel modelCosts={[]} />);
    expect(screen.getByText(/No cost in the last 30 days/i)).toBeInTheDocument();
  });
});
