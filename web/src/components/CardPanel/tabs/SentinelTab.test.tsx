import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { api } from '../../../api/client';
import type { Card, SentinelCardView, SentinelFindingView, SentinelHealView, SentinelRunView } from '../../../types';
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

function renderTab(view: Partial<SentinelCardView> & { run: SentinelRunView | null }) {
  vi.spyOn(api, 'getCardSentinel').mockResolvedValue({ heals: [], findings: [], ...view });
  return render(<SentinelTab card={card} />);
}

const heal: SentinelHealView = {
  id: 1,
  kind: 'heal',
  cause: 'gave_up',
  stage: 'healed',
  reason: '',
  diagnosis: 'The criteria were vague.',
  spend_usd: 0.42,
  created_at: '2026-11-13T10:00:00Z',
  updated_at: '2026-11-13T10:05:00Z',
  change: {
    section_before: 'keep\nold line',
    section_after: 'keep\nnew line',
    verify_before: { command: 'make a' },
    verify_after: { command: 'make b' },
    retired: ['T-002'],
    fresh: false,
  },
};

const finding = (kind: SentinelFindingView['kind'], title: string): SentinelFindingView => ({
  kind,
  title,
  text: `${title} text`,
  evidence: '',
  created_at: '2026-11-13T10:00:00Z',
});

describe('SentinelTab', () => {
  afterEach(() => vi.restoreAllMocks());

  it('lists heals with diagnosis, diff and spend', async () => {
    renderTab({ run, heals: [heal] });
    expect(await screen.findByText('The criteria were vague.')).toBeInTheDocument();
    expect(screen.getByText(/gave_up, healed/)).toBeInTheDocument();
    expect(screen.getByText(/\$0\.42/)).toBeInTheDocument();
    expect(screen.getByText('- old line')).toBeInTheDocument();
    expect(screen.getByText('+ new line')).toBeInTheDocument();
    expect(screen.getByText('keep')).toBeInTheDocument();
    expect(screen.getByText('- command: make a')).toBeInTheDocument();
    expect(screen.getByText('+ command: make b')).toBeInTheDocument();
  });

  it('shows retired subtasks', async () => {
    renderTab({ run, heals: [heal] });
    expect(await screen.findByText(/Retired subtasks: T-002/)).toBeInTheDocument();
  });

  it('groups findings and notes', async () => {
    renderTab({
      run,
      findings: [
        finding('guidance', 'Add a hint'),
        finding('hard_card', 'Too big'),
        finding('project_verify', 'Slow verify'),
      ],
    });
    expect(await screen.findByRole('heading', { name: 'Friction' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Notes' })).toBeInTheDocument();
    expect(screen.getByText('Add a hint')).toBeInTheDocument();
    expect(screen.getByText('Too big')).toBeInTheDocument();
    expect(screen.getByText('Slow verify text')).toBeInTheDocument();
  });

  it('renders model text as plain text', async () => {
    renderTab({ run, heals: [{ ...heal, diagnosis: '<img src=x onerror=alert(1)>' }] });
    expect(await screen.findByText('<img src=x onerror=alert(1)>')).toBeInTheDocument();
    expect(document.querySelector('img')).toBeNull();
  });

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

  it('shows a stop that came with no cause', async () => {
    renderTab({ run: { ...run, stopped_at: '2026-11-13T10:30:00Z' } });
    expect(await screen.findByText('Last stop')).toBeInTheDocument();
    expect(screen.getByText(/cause unknown, /)).toBeInTheDocument();
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
