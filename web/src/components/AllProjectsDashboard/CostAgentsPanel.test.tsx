import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { CostAgentsPanel } from './CostAgentsPanel';
import type { ModelCost, ActiveAgent } from '../../types';

const modelCosts: ModelCost[] = [
  { model: 'claude-opus-4-7', prompt_tokens: 1000, completion_tokens: 500, estimated_cost_usd: 5.0, card_count: 3 },
  { model: 'claude-haiku-4-5', prompt_tokens: 200, completion_tokens: 100, estimated_cost_usd: 0.10, card_count: 2 },
  { model: 'unknown', prompt_tokens: 50, completion_tokens: 25, estimated_cost_usd: 0.05, card_count: 1 },
];

const activeAgents: ActiveAgent[] = [];

function renderPanel() {
  return render(
    <MemoryRouter>
      <CostAgentsPanel
        modelCosts={modelCosts}
        activeAgents={activeAgents}
        stalledCount={0}
        prefixMap={new Map()}
      />
    </MemoryRouter>,
  );
}

describe('CostAgentsPanel', () => {
  it('panel header references "Cost by model"', () => {
    renderPanel();
    expect(screen.getByText(/Cost by model/i)).toBeInTheDocument();
  });

  it('first tab renders one row per model, sorted by cost desc', () => {
    renderPanel();
    expect(screen.getByText('claude-opus-4-7')).toBeInTheDocument();
    expect(screen.getByText('claude-haiku-4-5')).toBeInTheDocument();
    expect(screen.getByText('unknown')).toBeInTheDocument();

    // Verify rows are rendered in cost-descending order. The first <span>
    // inside each .apd-cost-row carries the model name.
    const rows = document.querySelectorAll('.apd-cost-row');
    const rowModels = Array.from(rows).map(
      (row) => row.querySelector('span')?.textContent ?? '',
    );
    expect(rowModels).toEqual(['claude-opus-4-7', 'claude-haiku-4-5', 'unknown']);
  });

  it('includes the "last model wins" tooltip text', () => {
    renderPanel();
    const tooltipHosts = document.querySelectorAll(
      '[title*="most-recently-used model" i], [aria-label*="most-recently-used model" i]',
    );
    expect(tooltipHosts.length).toBeGreaterThan(0);
  });
});
