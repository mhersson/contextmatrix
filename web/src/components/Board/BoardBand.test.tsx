import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { BoardBand } from './BoardBand';

describe('BoardBand', () => {
  const props = {
    projectName: 'contextmatrix',
    displayName: 'ContextMatrix · core',
    activeAgents: 4,
    openCount: 23,
    inReviewCount: 7,
    shippedToday: 3,
    lastUpdated: 'Updated 18s ago',
    onCreateCard: vi.fn(),
  };

  it('renders the project title in the heading', () => {
    render(<BoardBand {...props} />);
    expect(screen.getByRole('heading', { name: /ContextMatrix/ })).toBeInTheDocument();
  });

  it('renders project name in the crumb', () => {
    render(<BoardBand {...props} />);
    expect(screen.getByText('contextmatrix')).toBeInTheDocument();
  });

  it('shows agents-live pulse', () => {
    render(<BoardBand {...props} />);
    expect(screen.getByText('4 agents live')).toBeInTheDocument();
  });

  it('shows open · in-review · shipped-today', () => {
    render(<BoardBand {...props} />);
    expect(screen.getByText(/23 open/)).toBeInTheDocument();
    expect(screen.getByText(/7 in review/)).toBeInTheDocument();
    expect(screen.getByText(/3 shipped today/)).toBeInTheDocument();
  });

  it('invokes onCreateCard when +New Card is clicked', () => {
    const onCreateCard = vi.fn();
    render(<BoardBand {...props} onCreateCard={onCreateCard} />);
    fireEvent.click(screen.getByRole('button', { name: /new card/i }));
    expect(onCreateCard).toHaveBeenCalledTimes(1);
  });
});
