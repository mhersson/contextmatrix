import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { filterCardCosts, paginateItems, CostTable } from './CostTable';
import type { AgentCost, CardCost } from '../../types';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function makeCard(id: string, cost = 1.0): CardCost {
  return {
    card_id: id,
    card_title: `Title for ${id}`,
    estimated_cost_usd: cost,
    prompt_tokens: 100,
    completion_tokens: 50,
  };
}

const cards35: CardCost[] = Array.from({ length: 35 }, (_, i) => {
  const n = String(i + 1).padStart(3, '0');
  return makeCard(`PROJ-${n}`, 35 - i); // descending cost so PROJ-001 is most expensive
});

// ---------------------------------------------------------------------------
// filterCardCosts
// ---------------------------------------------------------------------------

describe('filterCardCosts', () => {
  it('returns all cards when search is empty', () => {
    const result = filterCardCosts(cards35, '');
    expect(result).toHaveLength(35);
  });

  it('returns all cards when search is only whitespace', () => {
    const result = filterCardCosts(cards35, '   ');
    expect(result).toHaveLength(35);
  });

  it('matches exact card ID', () => {
    const result = filterCardCosts(cards35, 'PROJ-001');
    expect(result).toHaveLength(1);
    expect(result[0].card_id).toBe('PROJ-001');
  });

  it('matches partial card ID (substring)', () => {
    const result = filterCardCosts(cards35, 'PROJ-01');
    // PROJ-010 through PROJ-019 plus PROJ-010 → cards ending in 01x
    const ids = result.map((c) => c.card_id);
    expect(ids).toContain('PROJ-010');
    expect(ids).toContain('PROJ-011');
    expect(ids).not.toContain('PROJ-001'); // "PROJ-001" does NOT contain "PROJ-01" as a substring
    // Actually "PROJ-01" is in "PROJ-010" etc. and also in "PROJ-010"
    // Let's verify: "PROJ-010".includes("PROJ-01") === true ✓
    // "PROJ-001".includes("PROJ-01") === false ✓
    expect(ids).not.toContain('PROJ-001');
  });

  it('is case-insensitive', () => {
    const result = filterCardCosts(cards35, 'proj-001');
    expect(result).toHaveLength(1);
    expect(result[0].card_id).toBe('PROJ-001');
  });

  it('returns empty array when no match', () => {
    const result = filterCardCosts(cards35, 'NOMATCH');
    expect(result).toHaveLength(0);
  });

  it('does not mutate the input array', () => {
    const input = [makeCard('A-001', 5), makeCard('A-002', 3)];
    const original = [...input];
    filterCardCosts(input, 'A-001');
    expect(input).toEqual(original);
  });
});

// ---------------------------------------------------------------------------
// paginateItems
// ---------------------------------------------------------------------------

describe('paginateItems', () => {
  const items = Array.from({ length: 10 }, (_, i) => makeCard(`X-${i + 1}`));

  it('returns correct slice for page 1', () => {
    const { items: page, totalPages } = paginateItems(items, 1, 3);
    expect(page).toHaveLength(3);
    expect(page[0].card_id).toBe('X-1');
    expect(totalPages).toBe(4); // ceil(10/3)
  });

  it('returns correct slice for last page (partial)', () => {
    const { items: page, totalPages } = paginateItems(items, 4, 3);
    expect(page).toHaveLength(1); // 10 items, page 4 of 3 per page = item 10
    expect(page[0].card_id).toBe('X-10');
    expect(totalPages).toBe(4);
  });

  it('returns all items when pageSize >= length', () => {
    const { items: page, totalPages } = paginateItems(items, 1, 30);
    expect(page).toHaveLength(10);
    expect(totalPages).toBe(1);
  });

  it('returns empty array and totalPages=0 for empty input', () => {
    const { items: page, totalPages } = paginateItems([], 1, 10);
    expect(page).toHaveLength(0);
    expect(totalPages).toBe(0);
  });

  it('returns empty array for out-of-range page', () => {
    const { items: page } = paginateItems(items, 99, 5);
    expect(page).toHaveLength(0);
  });

  it('returns exact pageSize when page is full', () => {
    const { items: page } = paginateItems(items, 2, 5);
    expect(page).toHaveLength(5);
    expect(page[0].card_id).toBe('X-6');
  });
});

// ---------------------------------------------------------------------------
// CostTable component — search and pagination integration
// ---------------------------------------------------------------------------

describe('CostTable — Cost by Card section', () => {
  const agentCosts: AgentCost[] = [];

  it('shows only first 30 cards by default when there are 35', () => {
    render(<CostTable agentCosts={agentCosts} cardCosts={cards35} />);
    // Each card row shows the card_id. With 35 cards, page 1 shows 30.
    expect(screen.getByText('PROJ-001')).toBeInTheDocument();
    expect(screen.getByText('PROJ-030')).toBeInTheDocument();
    expect(screen.queryByText('PROJ-031')).not.toBeInTheDocument();
  });

  it('shows pagination controls when there are more than 30 cards', () => {
    render(<CostTable agentCosts={agentCosts} cardCosts={cards35} />);
    expect(screen.getByText('Page 1 of 2')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /previous/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /next/i })).not.toBeDisabled();
  });

  it('navigates to next page showing remaining cards', () => {
    render(<CostTable agentCosts={agentCosts} cardCosts={cards35} />);
    fireEvent.click(screen.getByRole('button', { name: /next/i }));
    expect(screen.getByText('PROJ-031')).toBeInTheDocument();
    expect(screen.queryByText('PROJ-001')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /next/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /previous/i })).not.toBeDisabled();
  });

  it('shows search input', () => {
    render(<CostTable agentCosts={agentCosts} cardCosts={cards35} />);
    expect(screen.getByPlaceholderText(/search by card id/i)).toBeInTheDocument();
  });

  it('filters cards by search query and resets to page 1', () => {
    render(<CostTable agentCosts={agentCosts} cardCosts={cards35} />);
    const input = screen.getByPlaceholderText(/search by card id/i);
    fireEvent.change(input, { target: { value: 'PROJ-001' } });
    expect(screen.getByText('PROJ-001')).toBeInTheDocument();
    // Other cards should not be visible
    expect(screen.queryByText('PROJ-002')).not.toBeInTheDocument();
  });

  it('shows "No matching cards" when search yields no results', () => {
    render(<CostTable agentCosts={agentCosts} cardCosts={cards35} />);
    const input = screen.getByPlaceholderText(/search by card id/i);
    fireEvent.change(input, { target: { value: 'ZZZNOMATCH' } });
    expect(screen.getByText(/no matching cards/i)).toBeInTheDocument();
  });

  it('resets to page 1 when search text changes', () => {
    render(<CostTable agentCosts={agentCosts} cardCosts={cards35} />);
    // Go to page 2 first
    fireEvent.click(screen.getByRole('button', { name: /next/i }));
    expect(screen.getByText('Page 2 of 2')).toBeInTheDocument();
    // Type in search — should reset to page 1
    const input = screen.getByPlaceholderText(/search by card id/i);
    fireEvent.change(input, { target: { value: 'PROJ' } });
    expect(screen.getByText(/page 1 of/i)).toBeInTheDocument();
  });

  it('hides pagination when all filtered results fit on one page', () => {
    const fewCards = cards35.slice(0, 5);
    render(<CostTable agentCosts={agentCosts} cardCosts={fewCards} />);
    expect(screen.queryByRole('button', { name: /next/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /previous/i })).not.toBeInTheDocument();
  });
});
