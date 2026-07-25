import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { FilterChipBar } from './FilterChipBar';

const authState = vi.hoisted(() => ({ current: null as unknown }));
vi.mock('../../hooks/useAuth', () => ({
  useOptionalAuth: () => authState.current,
}));

beforeEach(() => {
  authState.current = null;
});

describe('FilterChipBar', () => {
  const baseProps = {
    filter: {},
    currentAgent: 'human:morten',
    onFilterChange: vi.fn(),
  };

  it('toggles the Mine chip', () => {
    const onFilterChange = vi.fn();
    render(<FilterChipBar {...baseProps} onFilterChange={onFilterChange} />);
    fireEvent.click(screen.getByRole('button', { name: /mine/i }));
    expect(onFilterChange).toHaveBeenCalledWith({ agent: 'human:morten' });
  });

  it('toggles Critical priority', () => {
    const onFilterChange = vi.fn();
    render(<FilterChipBar {...baseProps} onFilterChange={onFilterChange} />);
    fireEvent.click(screen.getByRole('button', { name: /critical/i }));
    expect(onFilterChange).toHaveBeenCalledWith({ priority: 'critical' });
  });

  it('shows Mine as active when filter.agent matches currentAgent', () => {
    render(<FilterChipBar {...baseProps} filter={{ agent: 'human:morten' }} />);
    expect(screen.getByRole('button', { name: /mine/i })).toHaveAttribute('data-active', 'true');
  });

  it('toggles Autonomous (boolean filter)', () => {
    const onFilterChange = vi.fn();
    render(<FilterChipBar {...baseProps} onFilterChange={onFilterChange} />);
    fireEvent.click(screen.getByRole('button', { name: /autonomous/i }));
    expect(onFilterChange).toHaveBeenCalledWith({ autonomous: true });
  });

  it('toggles worker:running', () => {
    const onFilterChange = vi.fn();
    render(<FilterChipBar {...baseProps} onFilterChange={onFilterChange} />);
    fireEvent.click(screen.getByRole('button', { name: /worker:running/i }));
    expect(onFilterChange).toHaveBeenCalledWith({ worker_status: 'running' });
  });

  it('fires onSearchChange when typing in the search input', () => {
    const onSearchChange = vi.fn();
    render(<FilterChipBar {...baseProps} searchQuery="" onSearchChange={onSearchChange} />);
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'auth' } });
    expect(onSearchChange).toHaveBeenCalledWith('auth');
  });

  it('mentions assignee in the search placeholder', () => {
    render(<FilterChipBar {...baseProps} />);
    expect(screen.getByPlaceholderText(/assignee/i)).toBeInTheDocument();
  });

  describe('Assigned to me chip', () => {
    it('is absent without an AuthProvider (useOptionalAuth returns null)', () => {
      render(<FilterChipBar {...baseProps} />);
      expect(screen.queryByRole('button', { name: /assigned to me/i })).not.toBeInTheDocument();
    });

    it('is absent in none mode', () => {
      authState.current = { mode: 'none', user: { username: 'alice' } };
      render(<FilterChipBar {...baseProps} />);
      expect(screen.queryByRole('button', { name: /assigned to me/i })).not.toBeInTheDocument();
    });

    it('is absent in multi mode with no session user', () => {
      authState.current = { mode: 'multi', user: null };
      render(<FilterChipBar {...baseProps} />);
      expect(screen.queryByRole('button', { name: /assigned to me/i })).not.toBeInTheDocument();
    });

    it('renders and toggles filter.assignee in multi mode with a session user', () => {
      authState.current = { mode: 'multi', user: { username: 'alice' } };
      const onFilterChange = vi.fn();
      render(<FilterChipBar {...baseProps} onFilterChange={onFilterChange} />);

      const chip = screen.getByRole('button', { name: /assigned to me/i });
      fireEvent.click(chip);
      expect(onFilterChange).toHaveBeenCalledWith({ assignee: 'alice' });
    });

    it('shows Assigned to me as active when filter.assignee matches the session user', () => {
      authState.current = { mode: 'multi', user: { username: 'alice' } };
      render(<FilterChipBar {...baseProps} filter={{ assignee: 'alice' }} />);
      expect(screen.getByRole('button', { name: /assigned to me/i })).toHaveAttribute('data-active', 'true');
    });

    it('clears filter.assignee when toggled off', () => {
      authState.current = { mode: 'multi', user: { username: 'alice' } };
      const onFilterChange = vi.fn();
      render(<FilterChipBar {...baseProps} filter={{ assignee: 'alice' }} onFilterChange={onFilterChange} />);
      fireEvent.click(screen.getByRole('button', { name: /assigned to me/i }));
      expect(onFilterChange).toHaveBeenCalledWith({});
    });
  });
});
