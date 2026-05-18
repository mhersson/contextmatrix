import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { TopCardsPanel } from './TopCardsPanel';
import type { CardCost } from '../../types';

const cardCosts: CardCost[] = [
  { card_id: 'ALPHA-1', card_title: 'Top card', assigned_agent: 'agent-a', prompt_tokens: 1, completion_tokens: 1, estimated_cost_usd: 9.0 },
  { card_id: 'ALPHA-2', card_title: 'Second',   assigned_agent: undefined, prompt_tokens: 1, completion_tokens: 1, estimated_cost_usd: 1.0 },
  { card_id: 'ZETA-1',  card_title: 'Orphan card', assigned_agent: undefined, prompt_tokens: 1, completion_tokens: 1, estimated_cost_usd: 0.5 },
];

const prefixMap = new Map<string, string>([['ALPHA', 'alpha']]);

function renderPanel() {
  return render(
    <MemoryRouter>
      <TopCardsPanel cardCosts={cardCosts} prefixMap={prefixMap} />
    </MemoryRouter>,
  );
}

describe('TopCardsPanel', () => {
  it('does not render a "Full breakdown" button', () => {
    renderPanel();
    expect(screen.queryByText(/Full breakdown/i)).toBeNull();
  });

  it('row link points at the board with the card ID in the query', () => {
    renderPanel();
    const row = screen.getByText('Top card').closest('a');
    expect(row).not.toBeNull();
    expect(row!.getAttribute('href')).toBe('/projects/alpha?card=ALPHA-1');
  });

  it('does not wrap rows with unmapped prefix in a link', () => {
    renderPanel();
    const orphan = screen.getByText('Orphan card');
    expect(orphan.closest('a')).toBeNull();
  });
});
