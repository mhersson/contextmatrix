import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { BoardFooter } from './BoardFooter';

describe('BoardFooter', () => {
  it('renders sync time + card count', () => {
    render(<BoardFooter lastSyncLabel="git sync · 18s ago" cardCount={32} columnCount={4} />);
    expect(screen.getByText(/git sync · 18s ago/)).toBeInTheDocument();
    expect(screen.getByText(/32 cards · 4 columns/)).toBeInTheDocument();
  });

  it('shows "Hide rail" when nowRail is open and fires onToggleNowRail', () => {
    const onToggle = vi.fn();
    render(
      <BoardFooter
        lastSyncLabel="" cardCount={0} columnCount={0}
        nowRailOpen={true} onToggleNowRail={onToggle}
      />,
    );
    const btn = screen.getByRole('button', { name: /hide rail/i });
    expect(btn).toBeInTheDocument();
    fireEvent.click(btn);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it('shows "Show rail" when nowRail is closed', () => {
    render(
      <BoardFooter
        lastSyncLabel="" cardCount={0} columnCount={0}
        nowRailOpen={false} onToggleNowRail={() => {}}
      />,
    );
    expect(screen.getByRole('button', { name: /show rail/i })).toBeInTheDocument();
  });
});
