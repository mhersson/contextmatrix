import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, act, fireEvent } from '@testing-library/react';
import type { BoardEvent, SyncStatus } from '../types';
import { api } from '../api/client';
import { SyncProvider, useSync } from './useSync';

const bus = vi.hoisted(() => ({ handler: null as null | ((e: BoardEvent) => void) }));

vi.mock('../api/client', () => ({
  api: { getSyncStatuses: vi.fn(), triggerSync: vi.fn() },
}));

vi.mock('./useSSEBus', () => ({
  useSSEBus: () => ({
    subscribe: (_pattern: string, onEvent: (e: BoardEvent) => void) => {
      bus.handler = onEvent;
      return () => { bus.handler = null; };
    },
    reconnectEpoch: 0,
    connected: true,
    error: null,
  }),
}));

const statuses: SyncStatus[] = [
  { repo: 'team', enabled: true, shared: true, syncing: false, last_sync_time: null, unpushed_commits: 2 },
  { repo: 'private', enabled: false, syncing: false, last_sync_time: null },
];

function Probe() {
  const { syncStatuses, syncStatus, statusFor, refresh } = useSync();
  return (
    <div>
      <span data-testid="count">{syncStatuses.length}</span>
      <span data-testid="first">{syncStatus?.repo ?? 'none'}</span>
      <span data-testid="private">{statusFor('private')?.enabled ? 'on' : 'off'}</span>
      <span data-testid="unpushed">{statusFor('team')?.unpushed_commits ?? 0}</span>
      <span data-testid="missing">{statusFor('nope') === null ? 'null' : 'found'}</span>
      <button onClick={() => { void refresh(); }}>refresh</button>
    </div>
  );
}

describe('useSync', () => {
  beforeEach(() => {
    vi.mocked(api.getSyncStatuses).mockReset();
    vi.mocked(api.getSyncStatuses).mockResolvedValue(statuses);
  });

  it('loads one status per repo and resolves by name', async () => {
    render(<SyncProvider><Probe /></SyncProvider>);
    await waitFor(() => expect(screen.getByTestId('count').textContent).toBe('2'));
    expect(screen.getByTestId('first').textContent).toBe('team');
    expect(screen.getByTestId('private').textContent).toBe('off');
    expect(screen.getByTestId('unpushed').textContent).toBe('2');
    expect(screen.getByTestId('missing').textContent).toBe('null');
  });

  it('refetches on a completed sync event', async () => {
    render(<SyncProvider><Probe /></SyncProvider>);
    await waitFor(() => expect(vi.mocked(api.getSyncStatuses)).toHaveBeenCalledTimes(1));
    vi.mocked(api.getSyncStatuses).mockResolvedValue([{ ...statuses[0], unpushed_commits: 0 }, statuses[1]]);
    await act(async () => {
      bus.handler?.({ type: 'sync.completed', project: '', card_id: '', timestamp: '', data: { repo: 'team' } } as BoardEvent);
    });
    await waitFor(() => expect(screen.getByTestId('unpushed').textContent).toBe('0'));
    expect(vi.mocked(api.getSyncStatuses)).toHaveBeenCalledTimes(2);
  });

  it('refresh() re-fetches and updates syncStatuses', async () => {
    render(<SyncProvider><Probe /></SyncProvider>);
    await waitFor(() => expect(vi.mocked(api.getSyncStatuses)).toHaveBeenCalledTimes(1));
    vi.mocked(api.getSyncStatuses).mockResolvedValue([{ ...statuses[0], unpushed_commits: 5 }, statuses[1]]);
    await act(async () => {
      fireEvent.click(screen.getByText('refresh'));
    });
    await waitFor(() => expect(screen.getByTestId('unpushed').textContent).toBe('5'));
    expect(vi.mocked(api.getSyncStatuses)).toHaveBeenCalledTimes(2);
  });

  it('throws outside the provider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => render(<Probe />)).toThrow(/SyncProvider/);
    spy.mockRestore();
  });
});
