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

describe('CardChipRow - declutter: footer omitted unless assigned', () => {
  it('renders nothing for a card whose signals all moved to icons or the panel', () => {
    const { container } = render(
      <CardChipRow
        card={{
          ...baseCard,
          labels: ['model-selection', 'observability'],
          branch_name: 'test-001/chip-row-card',
          worker_status: 'running',
          mob_participants: 3,
          best_of_n: 3,
          autonomous: true,
          assigned_agent: 'claude-sonnet-worker',
          depends_on: ['TEST-009'],
        }}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('never prints the agent name', () => {
    authState.current = { mode: 'multi' };
    render(
      <CardChipRow
        card={{ ...baseCard, assigned_agent: 'claude-sonnet-worker', assignee: 'alice' }}
      />,
    );
    expect(screen.queryByText(/sonnet-worker/)).not.toBeInTheDocument();
  });

  it('never renders label, branch, deps, or best-of-n pills', () => {
    render(
      <CardChipRow
        card={{
          ...baseCard,
          labels: ['observability'],
          branch_name: 'test-001/chip-row-card',
          state: 'in_progress',
          depends_on: ['TEST-009'],
          dependencies_met: true,
          best_of_n: 3,
        }}
      />,
    );
    expect(screen.queryByText('observability')).not.toBeInTheDocument();
    expect(screen.queryByText('chip-row-card')).not.toBeInTheDocument();
    expect(screen.queryByText('deps met')).not.toBeInTheDocument();
    expect(screen.queryByText('Best of 3')).not.toBeInTheDocument();
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

describe('CardChipRow - compact mode', () => {
  it('shows the type initial and no parent badge', () => {
    render(<CardChipRow card={{ ...baseCard, type: 'bug', parent: 'TEST-000' }} compact />);
    expect(screen.getByLabelText('Type: bug')).toBeInTheDocument();
    expect(screen.queryByTitle(/^Parent:/)).not.toBeInTheDocument();
  });
});
