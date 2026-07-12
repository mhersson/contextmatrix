import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CardChipRow } from './CardChipRow';
import type { Card } from '../../types';

const baseCard: Card = {
  id: 'TEST-001',
  title: 'Chip row card',
  project: 'test',
  type: 'task',
  state: 'todo',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
};

describe('CardChipRow — mob badge', () => {
  it('shows "mob N" when mob_participants >= 2', () => {
    render(<CardChipRow card={{ ...baseCard, mob_participants: 3 }} />);
    expect(screen.getByText('mob 3')).toBeInTheDocument();
  });

  it('hides the badge when mob is off or undefined', () => {
    const { rerender } = render(<CardChipRow card={baseCard} />);
    expect(screen.queryByText(/mob/)).not.toBeInTheDocument();

    rerender(<CardChipRow card={{ ...baseCard, mob_participants: 0 }} />);
    expect(screen.queryByText(/mob/)).not.toBeInTheDocument();
  });
});
