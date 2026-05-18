import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { BoardFooter } from './BoardFooter';

describe('BoardFooter', () => {
  it('renders sync time + card count', () => {
    render(<BoardFooter lastSyncLabel="git sync · 18s ago" cardCount={32} columnCount={4} />);
    expect(screen.getByText(/git sync · 18s ago/)).toBeInTheDocument();
    expect(screen.getByText(/32 cards · 4 columns$/)).toBeInTheDocument();
  });

  it('renders sync label as a button and invokes onSyncClick', () => {
    const onSyncClick = vi.fn();
    render(<BoardFooter lastSyncLabel="git sync · 18s ago" cardCount={10} columnCount={3} onSyncClick={onSyncClick} />);
    const btn = screen.getByRole('button', { name: /git sync · 18s ago/i });
    expect(btn).toBeInTheDocument();
    fireEvent.click(btn);
    expect(onSyncClick).toHaveBeenCalledTimes(1);
  });

  it('renders sync label as a span when no onSyncClick provided', () => {
    render(<BoardFooter lastSyncLabel="git sync · idle" cardCount={0} columnCount={0} />);
    expect(screen.queryByRole('button', { name: /git sync/i })).not.toBeInTheDocument();
    expect(screen.getByText('git sync · idle')).toBeInTheDocument();
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
