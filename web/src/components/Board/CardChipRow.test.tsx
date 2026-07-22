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

describe('CardChipRow - mob badge', () => {
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

describe('CardChipRow - branch badge gating', () => {
  it('hides the branch chip on a fresh todo card without run activity', () => {
    render(<CardChipRow card={{ ...baseCard, branch_name: 'test-001/chip-row-card' }} />);
    expect(screen.queryByText('chip-row-card')).not.toBeInTheDocument();
  });

  it('shows the branch chip once a worker has touched the card', () => {
    render(<CardChipRow card={{ ...baseCard, branch_name: 'test-001/chip-row-card', worker_status: 'running' }} />);
    expect(screen.getByText('chip-row-card')).toBeInTheDocument();
  });

  it('shows the branch chip when the card has left todo', () => {
    render(<CardChipRow card={{ ...baseCard, branch_name: 'test-001/chip-row-card', state: 'in_progress' }} />);
    expect(screen.getByText('chip-row-card')).toBeInTheDocument();
  });
});

describe('CardChipRow - Best of N vs mob execute', () => {
  it('suppresses the Best of N chip when mob execute is active', () => {
    render(
      <CardChipRow
        card={{ ...baseCard, best_of_n: 3, mob_participants: 3, mob_phases: ['plan', 'execute'] }}
      />,
    );
    expect(screen.queryByText('Best of 3')).not.toBeInTheDocument();
    expect(screen.getByText('mob 3')).toBeInTheDocument();
  });

  it('keeps the Best of N chip when the mob skips execute', () => {
    render(
      <CardChipRow
        card={{ ...baseCard, best_of_n: 3, mob_participants: 3, mob_phases: ['plan', 'review'] }}
      />,
    );
    expect(screen.getByText('Best of 3')).toBeInTheDocument();
  });
});
