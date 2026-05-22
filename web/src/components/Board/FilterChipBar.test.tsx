import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { FilterChipBar } from './FilterChipBar';

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

  it('toggles runner:running', () => {
    const onFilterChange = vi.fn();
    render(<FilterChipBar {...baseProps} onFilterChange={onFilterChange} />);
    fireEvent.click(screen.getByRole('button', { name: /runner:running/i }));
    expect(onFilterChange).toHaveBeenCalledWith({ runner_status: 'running' });
  });

  it('fires onSearchChange when typing in the search input', () => {
    const onSearchChange = vi.fn();
    render(<FilterChipBar {...baseProps} searchQuery="" onSearchChange={onSearchChange} />);
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'auth' } });
    expect(onSearchChange).toHaveBeenCalledWith('auth');
  });

});
