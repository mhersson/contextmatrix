import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ParentSearch } from './ParentSearch';
import type { Card } from '../../types';

function makeCard(id: string, state: string): Card {
  return {
    id,
    title: `Card ${id}`,
    project: 'test',
    type: 'task',
    state,
    priority: 'medium',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
  };
}

const cards = [
  makeCard('TEST-001', 'todo'),
  makeCard('TEST-002', 'in_progress'),
  makeCard('TEST-003', 'stalled'),
  makeCard('TEST-004', 'done'),
  makeCard('TEST-005', 'not_planned'),
];

function search(term: string) {
  render(<ParentSearch parent="" setParent={vi.fn()} cards={cards} />);
  fireEvent.change(screen.getByPlaceholderText('Search by ID or title...'), {
    target: { value: term },
  });
}

describe('ParentSearch', () => {
  it('lists open cards matching the search, including stalled', () => {
    search('card');
    expect(screen.getByText('TEST-001')).toBeInTheDocument();
    expect(screen.getByText('TEST-002')).toBeInTheDocument();
    expect(screen.getByText('TEST-003')).toBeInTheDocument();
  });

  it('excludes cards in terminal states from the results', () => {
    search('card');
    expect(screen.queryByText('TEST-004')).not.toBeInTheDocument();
    expect(screen.queryByText('TEST-005')).not.toBeInTheDocument();
  });
});
