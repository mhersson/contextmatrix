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

  it('models tab badge shows the full count, not the truncated TOP_MODEL_COSTS', () => {
    const many: ModelCost[] = Array.from({ length: 12 }, (_, i) => ({
      model: `model-${i}`,
      prompt_tokens: 100,
      completion_tokens: 50,
      estimated_cost_usd: 1 + i,
      card_count: 1,
    }));
    render(
      <MemoryRouter>
        <CostAgentsPanel
          modelCosts={many}
          activeAgents={[]}
          stalledCount={0}
          prefixMap={new Map()}
        />
      </MemoryRouter>,
    );
    const modelsTab = document.getElementById('apd-tab-models-btn');
    expect(modelsTab).not.toBeNull();
    const badge = modelsTab!.querySelector('.apd-tab-count');
    expect(badge?.textContent).toBe('12');
  });

  it('uses the renamed "models" tab id and panel id', () => {
    renderPanel();
    expect(document.getElementById('apd-tab-models-btn')).not.toBeNull();
    expect(document.getElementById('apd-tab-models-panel')).not.toBeNull();
    // The old id should not exist anymore.
    expect(document.getElementById('apd-tab-cost-btn')).toBeNull();
    expect(document.getElementById('apd-tab-cost-panel')).toBeNull();
  });

  it('includes the "last model wins" tooltip text on a focusable element', () => {
    renderPanel();
    const tooltipHosts = document.querySelectorAll(
      '[title*="most-recently-used model" i], [aria-label*="most-recently-used model" i]',
    );
    expect(tooltipHosts.length).toBeGreaterThan(0);
    // Must be keyboard-focusable: rendered as <button>, which is natively focusable.
    const focusable = Array.from(tooltipHosts).some(
      (el) => el.tagName === 'BUTTON',
    );
    expect(focusable).toBe(true);
  });
});
