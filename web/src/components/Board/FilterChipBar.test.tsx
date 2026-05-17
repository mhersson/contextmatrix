import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { FilterChipBar } from './FilterChipBar';

describe('FilterChipBar', () => {
  const baseProps = {
    filter: {},
    currentAgent: 'human:morten',
    onFilterChange: vi.fn(),
  };

  it('renders the search input with a placeholder', () => {
    render(<FilterChipBar {...baseProps} />);
    expect(screen.getByPlaceholderText(/search/i)).toBeInTheDocument();
  });

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
});
