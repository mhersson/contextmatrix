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
    onCreateCard: vi.fn(),
  };

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

  it('shows shipped-7d delta when shipped7d + prior7d are provided', () => {
    render(
      <BoardBand
        projectName="p" displayName="P" activeAgents={1} openCount={1} inReviewCount={0} shippedToday={0}
        onCreateCard={() => {}}
        shippedLast7d={14} shippedPrior7d={11}
      />
    );
    expect(screen.getByText(/14 shipped this week/)).toBeInTheDocument();
    expect(screen.getByText(/\+27%/)).toBeInTheDocument();
  });
});
