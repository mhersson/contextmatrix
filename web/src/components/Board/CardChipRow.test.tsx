import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CardChipRow } from './CardChipRow';
import type { Card } from '../../types';

const authState = vi.hoisted(() => ({ current: null as unknown }));
vi.mock('../../hooks/useAuth', () => ({
  useOptionalAuth: () => authState.current,
}));

const usersState = vi.hoisted(() => ({ current: [] as unknown[] }));
vi.mock('../../hooks/useUsers', () => ({
  useUsers: () => usersState.current,
}));

beforeEach(() => {
  authState.current = null;
  usersState.current = [];
});

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

describe('CardChipRow - assignee chip', () => {
  it('shows an initials circle with tooltip in expanded mode (multi mode)', () => {
    authState.current = { mode: 'multi' };
    render(<CardChipRow card={{ ...baseCard, assignee: 'alice' }} />);
    // Empty roster: label and initial fall back to the username.
    const chip = screen.getByTitle('Assignee: alice');
    expect(chip).toBeInTheDocument();
    expect(chip).toHaveTextContent(/^A$/);
  });

  it('uses display-name initials and label when the user is in the roster', () => {
    authState.current = { mode: 'multi' };
    usersState.current = [{ username: 'mohersson', display_name: 'Morten Hersson' }];
    render(<CardChipRow card={{ ...baseCard, assignee: 'mohersson' }} />);
    const chip = screen.getByTitle('Assignee: Morten Hersson');
    expect(chip).toHaveTextContent(/^MH$/);
    expect(chip).toHaveAttribute('aria-label', 'Assignee: Morten Hersson');
  });

  it('falls back to the username initial for a single-word display name', () => {
    authState.current = { mode: 'multi' };
    usersState.current = [{ username: 'alice', display_name: 'Alice' }];
    render(<CardChipRow card={{ ...baseCard, assignee: 'alice' }} />);
    // Label still prefers the display name; the initial comes from the username.
    const chip = screen.getByTitle('Assignee: Alice');
    expect(chip).toHaveTextContent(/^A$/);
  });

  it('falls back to the username when the roster has no entry for the assignee', () => {
    authState.current = { mode: 'multi' };
    usersState.current = [{ username: 'alice', display_name: 'Alice Smith' }];
    render(<CardChipRow card={{ ...baseCard, assignee: 'mohersson' }} />);
    const chip = screen.getByTitle('Assignee: mohersson');
    expect(chip).toHaveTextContent(/^M$/);
  });

  it('hides the assignee chip in compact mode even when assignee is set (multi mode)', () => {
    authState.current = { mode: 'multi' };
    render(<CardChipRow card={{ ...baseCard, assignee: 'alice' }} compact />);
    expect(screen.queryByTitle('Assignee: alice')).not.toBeInTheDocument();
    expect(screen.queryByText('alice')).not.toBeInTheDocument();
  });

  it('hides the assignee chip when unset (multi mode)', () => {
    authState.current = { mode: 'multi' };
    render(<CardChipRow card={baseCard} />);
    expect(screen.queryByTitle(/^Assignee:/)).not.toBeInTheDocument();
  });

  it('hides the assignee chip in none mode even when assignee is set', () => {
    authState.current = { mode: 'none' };
    render(<CardChipRow card={{ ...baseCard, assignee: 'alice' }} />);
    expect(screen.queryByTitle('Assignee: alice')).not.toBeInTheDocument();
    expect(screen.queryByText('alice')).not.toBeInTheDocument();
  });

  it('hides the assignee chip without an AuthProvider (useOptionalAuth returns null)', () => {
    render(<CardChipRow card={{ ...baseCard, assignee: 'alice' }} />);
    expect(screen.queryByTitle('Assignee: alice')).not.toBeInTheDocument();
    expect(screen.queryByText('alice')).not.toBeInTheDocument();
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
