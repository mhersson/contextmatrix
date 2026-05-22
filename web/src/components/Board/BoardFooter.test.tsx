import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import type { SyncStatus } from '../../types';
import { BoardFooter } from './BoardFooter';

function makeStatus(overrides: Partial<SyncStatus> = {}): SyncStatus {
  return {
    last_sync_time: null,
    last_sync_error: '',
    syncing: false,
    enabled: true,
    ...overrides,
  };
}

describe('BoardFooter', () => {
  it('renders sync label as a button and invokes onSyncClick', () => {
    const onSyncClick = vi.fn();
    const tenSecondsAgo = new Date(Date.now() - 10_000).toISOString();
    render(
      <BoardFooter
        cardCount={10}
        columnCount={3}
        syncStatus={makeStatus({ last_sync_time: tenSecondsAgo })}
        onSyncClick={onSyncClick}
      />,
    );
    const btn = screen.getByRole('button', { name: /git sync · \d+s ago/i });
    expect(btn).toBeInTheDocument();
    fireEvent.click(btn);
    expect(onSyncClick).toHaveBeenCalledTimes(1);
  });

  it('renders sync label as a span when no onSyncClick provided', () => {
    render(<BoardFooter cardCount={0} columnCount={0} />);
    expect(screen.queryByRole('button', { name: /git sync/i })).not.toBeInTheDocument();
    expect(screen.getByText('git sync · idle')).toBeInTheDocument();
  });

  it('shows "Hide rail" when nowRail is open and fires onToggleNowRail', () => {
    const onToggle = vi.fn();
    render(
      <BoardFooter
        cardCount={0} columnCount={0}
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
        cardCount={0} columnCount={0}
        nowRailOpen={false} onToggleNowRail={() => {}}
      />,
    );
    expect(screen.getByRole('button', { name: /show rail/i })).toBeInTheDocument();
  });

  it('shows aria-busy syncing state and is not a clickable button', () => {
    const onSyncClick = vi.fn();
    render(
      <BoardFooter
        cardCount={0}
        columnCount={0}
        syncStatus={makeStatus({ syncing: true })}
        onSyncClick={onSyncClick}
      />,
    );
    // The sync label is a span (with aria-busy), not a button, while syncing.
    expect(screen.queryByRole('button', { name: /sync/i })).not.toBeInTheDocument();
    const label = screen.getByText(/syncing/i);
    expect(label).toHaveAttribute('aria-busy', 'true');
  });

  it('renders error styling and tooltip when last_sync_error is set', () => {
    const onSyncClick = vi.fn();
    render(
      <BoardFooter
        cardCount={0}
        columnCount={0}
        syncStatus={makeStatus({ last_sync_error: 'fetch failed: timeout' })}
        onSyncClick={onSyncClick}
      />,
    );
    const btn = screen.getByRole('button', { name: /git sync/i });
    // The button title carries the error and the retry hint.
    expect(btn).toHaveAttribute('title', expect.stringContaining('fetch failed: timeout'));
    expect(btn).toHaveAttribute('title', expect.stringContaining('Click to retry'));
    // Error styling: the label colour comes from --red via inline style.
    expect(btn).toHaveStyle({ color: 'var(--red)' });
    // Click still triggers the retry path.
    fireEvent.click(btn);
    expect(onSyncClick).toHaveBeenCalledTimes(1);
  });

  it('renders an offline indicator when connected={false}', () => {
    render(
      <BoardFooter
        cardCount={0}
        columnCount={0}
        connected={false}
      />,
    );
    expect(screen.getByText('offline')).toBeInTheDocument();
    expect(screen.queryByText('online')).not.toBeInTheDocument();
  });

  it('renders an online indicator when connected={true}', () => {
    render(
      <BoardFooter
        cardCount={0}
        columnCount={0}
        connected={true}
      />,
    );
    expect(screen.getByText('online')).toBeInTheDocument();
    expect(screen.queryByText('offline')).not.toBeInTheDocument();
  });

  it('omits the connection indicator when connected is undefined', () => {
    render(<BoardFooter cardCount={0} columnCount={0} />);
    expect(screen.queryByText(/online|offline/)).not.toBeInTheDocument();
  });

  it('computes a relative-time label from syncStatus.last_sync_time', () => {
    const tenSecondsAgo = new Date(Date.now() - 10_000).toISOString();
    render(
      <BoardFooter
        cardCount={0}
        columnCount={0}
        syncStatus={makeStatus({ last_sync_time: tenSecondsAgo })}
      />,
    );
    expect(screen.getByText(/git sync · \d+s ago/)).toBeInTheDocument();
  });

  it('falls back to "git sync · idle" when syncStatus has no last_sync_time', () => {
    render(
      <BoardFooter
        cardCount={0}
        columnCount={0}
        syncStatus={makeStatus({ last_sync_time: null })}
      />,
    );
    expect(screen.getByText('git sync · idle')).toBeInTheDocument();
  });
});
