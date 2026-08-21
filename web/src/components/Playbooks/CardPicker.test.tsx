import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CardPicker } from './CardPicker';
import type { Card, ProjectConfig } from '../../types';

const projects: ProjectConfig[] = [
  {
    name: 'alpha',
    prefix: 'ALPHA',
    next_id: 1,
    states: ['todo', 'in_progress', 'done'],
    types: ['task'],
    priorities: ['low', 'medium', 'high'],
    transitions: { todo: ['in_progress'], in_progress: ['done'], done: [] },
    templates: {},
  },
];

function makeCard(id: string, state: string): Card {
  return {
    id,
    title: `Card ${id}`,
    project: 'alpha',
    type: 'task',
    state,
    priority: 'medium',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
  };
}

const cards = [
  makeCard('ALPHA-001', 'todo'),
  makeCard('ALPHA-002', 'in_progress'),
  makeCard('ALPHA-003', 'stalled'),
  makeCard('ALPHA-004', 'done'),
  makeCard('ALPHA-005', 'not_planned'),
];

function renderPicker() {
  render(
    <CardPicker
      projects={projects}
      project="alpha"
      onProjectChange={vi.fn()}
      cards={cards}
      filter="card"
      onFilterChange={vi.fn()}
      selectedCard={null}
      onSelectCard={vi.fn()}
    />,
  );
}

describe('CardPicker', () => {
  it('lists open cards matching the filter, including stalled', () => {
    renderPicker();
    expect(screen.getByText('ALPHA-001')).toBeInTheDocument();
    expect(screen.getByText('ALPHA-002')).toBeInTheDocument();
    expect(screen.getByText('ALPHA-003')).toBeInTheDocument();
  });

  it('excludes cards in terminal states from the results', () => {
    renderPicker();
    expect(screen.queryByText('ALPHA-004')).not.toBeInTheDocument();
    expect(screen.queryByText('ALPHA-005')).not.toBeInTheDocument();
  });
});
