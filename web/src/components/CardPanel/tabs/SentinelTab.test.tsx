import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { api } from '../../../api/client';
import type { Card, SentinelRunView } from '../../../types';
import { SentinelTab } from './SentinelTab';

const bus = vi.hoisted(() => ({ handlers: new Map<string, (e: unknown) => void>() }));
vi.mock('../../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({
    subscribe: (pattern: string, fn: (e: unknown) => void) => {
      bus.handlers.set(pattern, fn);
      return () => bus.handlers.delete(pattern);
    },
    connected: true,
    reconnectEpoch: 0,
  }),
}));

const card = { id: 'T-001', project: 'p' } as Card;

const run: SentinelRunView = {
  origin: 'mcp',
  autonomous: true,
  attempts: 1,
  max_attempts: 2,
  started_at: '2026-11-13T10:00:00Z',
  human_stop: false,
};

function renderTab(view: { run: SentinelRunView | null }) {
  vi.spyOn(api, 'getCardSentinel').mockResolvedValue(view);
  return render(<SentinelTab card={card} />);
}

describe('SentinelTab', () => {
  afterEach(() => vi.restoreAllMocks());

  it('shows origin, autonomy, attempts and the last stop', async () => {
    renderTab({ run: { ...run, stop_cause: 'usage_limit', stopped_at: '2026-11-13T10:30:00Z' } });
    expect(await screen.findByText('MCP')).toBeInTheDocument();
    expect(screen.getByText('Yes')).toBeInTheDocument();
    expect(screen.getByText('1 of 2')).toBeInTheDocument();
    expect(screen.getByText(/usage_limit/)).toBeInTheDocument();
  });

  it('shows the other origins by name', async () => {
    renderTab({ run: { ...run, origin: 'rest', autonomous: false } });
    expect(await screen.findByText('UI or REST')).toBeInTheDocument();
    expect(screen.getByText('No')).toBeInTheDocument();
    expect(screen.queryByText('Last stop')).not.toBeInTheDocument();
  });

  it('says when a human stopped the run', async () => {
    renderTab({ run: { ...run, human_stop: true, stop_cause: 'killed', stopped_at: '2026-11-13T10:30:00Z' } });
    expect(await screen.findByText(/stopped by a human/)).toBeInTheDocument();
    expect(screen.queryByText(/killed/)).not.toBeInTheDocument();
  });

  it('explains an absent run row', async () => {
    renderTab({ run: null });
    await waitFor(() => expect(api.getCardSentinel).toHaveBeenCalledWith('p', 'T-001', expect.anything()));
    expect(await screen.findByText(/acts only on runs this instance launched/)).toBeInTheDocument();
    expect(screen.queryByText('Launched from')).not.toBeInTheDocument();
  });

  it('refetches on a Sentinel event and on events for this card only', async () => {
    renderTab({ run: null });
    const spy = api.getCardSentinel as ReturnType<typeof vi.fn>;
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));

    act(() => bus.handlers.get('card.*')?.({ project: 'p', card_id: 'OTHER-1' }));
    expect(spy).toHaveBeenCalledTimes(1);

    act(() => bus.handlers.get('card.*')?.({ project: 'p', card_id: 'T-001' }));
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(2));

    act(() => bus.handlers.get('sentinel.*')?.({}));
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(3));
  });
});
