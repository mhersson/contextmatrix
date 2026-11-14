import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { api } from '../../../api/client';
import type { Card, SentinelAnalysisView, SentinelCardView, SentinelFindingView, SentinelHealView, SentinelRunView } from '../../../types';
import { MemoryRouter } from 'react-router';
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
  vi.spyOn(api, 'getCardSentinel').mockResolvedValue({ heals: [], findings: [], analyses: [], standing_pr: null, ...view });
  return render(
    <MemoryRouter>
      <SentinelTab card={card} />
    </MemoryRouter>,
  );
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
  dropped: [],
};

const finding = (kind: SentinelFindingView['kind'], title: string): SentinelFindingView => ({
  id: 1,
  state: '',
  reason: '',
  output: '',
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

  // jsdom has no layout, so only the properties that stop a long line from clipping can be observed.
  it('wraps long lines in both diffs', async () => {
    renderTab({ run, heals: [heal] });
    const section = (await screen.findByText('+ new line')).closest('pre');
    const verify = screen.getByText('+ command: make b').closest('pre');
    for (const pre of [section, verify]) {
      expect(pre).toHaveClass('whitespace-pre-wrap');
      expect(pre).toHaveClass('wrap-anywhere');
    }
  });

  it('shows retired subtasks', async () => {
    renderTab({ run, heals: [heal] });
    expect(await screen.findByText(/Retired subtasks: T-002/)).toBeInTheDocument();
  });

  it('says whether the rerun starts fresh or resumes', async () => {
    const { unmount } = renderTab({ run, heals: [heal] });
    expect(await screen.findByText(/resumes/)).toBeInTheDocument();
    expect(screen.queryByText(/starts fresh/)).not.toBeInTheDocument();
    unmount();

    renderTab({ run, heals: [{ ...heal, change: { ...heal.change!, fresh: true } }] });
    expect(await screen.findByText(/starts fresh/)).toBeInTheDocument();
    expect(screen.queryByText(/resumes/)).not.toBeInTheDocument();
  });

  it.each(['rerun', 'healed', 'rerun_failed', 'closed'])('shows the rerun mode at stage %s', async (stage) => {
    renderTab({ run, heals: [{ ...heal, stage }] });
    expect(await screen.findByText(/resumes/)).toBeInTheDocument();
  });

  it('shows no rerun mode when Sentinel did not rerun the card', async () => {
    renderTab({
      run,
      heals: [
        { ...heal, id: 2, stage: 'parked', change: { ...heal.change!, fresh: true } },
        { ...heal, id: 3, kind: 'forward', stage: 'forwarded', source_card: 'T-009' },
      ],
    });
    expect(await screen.findByText(/Forwarded from T-009/)).toBeInTheDocument();
    expect(screen.queryByText(/starts fresh|resumes/)).not.toBeInTheDocument();
  });

  it('lists why the section was dropped', async () => {
    renderTab({ run, heals: [{ ...heal, dropped: ['introduces gen-tool', 'introduces gen.yaml'] }] });
    expect(await screen.findByRole('heading', { name: /section dropped/i })).toBeInTheDocument();
    const reasons = screen.getAllByRole('listitem').map((li) => li.textContent);
    expect(reasons).toEqual(['introduces gen-tool', 'introduces gen.yaml']);
  });

  it('shows no dropped section when nothing was dropped', async () => {
    renderTab({ run, heals: [heal] });
    expect(await screen.findByText('The criteria were vague.')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: /section dropped/i })).not.toBeInTheDocument();
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

  it("shows each finding's outcome with its link", async () => {
    const f = (id: number, title: string, over: Partial<SentinelFindingView>) => ({ ...finding('guidance', title), id, ...over });
    renderTab({
      run: null,
      standing_pr: { url: 'https://github.com/acme/app/pull/12', state: 'open', file: 'AGENTS.md', pushed_at: '2026-11-13T10:30:00Z' },
      findings: [
        f(1, 'queued one', { state: 'queued', reason: 'over the daily limit' }),
        f(2, 'pending one', { state: 'pending' }),
        f(3, 'proposed one', { state: 'proposed', output: 'https://github.com/acme/app/pull/3' }),
        f(4, 'carded one', { state: 'carded', output: 'T-040' }),
        f(5, 'dropped one', { state: 'dropped', reason: 'no evidence' }),
        f(6, 'duplicate one', { state: 'duplicate', reason: 'same as another' }),
        f(7, 'note one', { state: 'note' }),
      ],
    });
    expect(await screen.findByText('over the daily limit')).toBeInTheDocument();
    expect(screen.getByText('Waiting for the next guidance PR')).toBeInTheDocument();
    const pr = screen.getByRole('link', { name: 'In the standing PR' });
    expect(pr).toHaveAttribute('href', 'https://github.com/acme/app/pull/3');
    expect(pr).toHaveAttribute('target', '_blank');
    expect(pr).toHaveAttribute('rel', 'noopener noreferrer');
    const carded = screen.getByRole('link', { name: 'Friction card T-040' });
    expect(carded).toHaveAttribute('href', '/projects/p?card=T-040');
    expect(carded).not.toHaveAttribute('target');
    expect(screen.getByText('Dropped: no evidence')).toBeInTheDocument();
    expect(screen.getByText('same as another')).toBeInTheDocument();
    expect(screen.getAllByTestId('finding-outcome')).toHaveLength(6);
  });

  it('shows the standing PR changed outside Sentinel', async () => {
    renderTab({
      run: null,
      standing_pr: { url: 'https://github.com/acme/app/pull/12', state: 'changed_outside', file: 'AGENTS.md', pushed_at: '2026-11-13T10:30:00Z' },
    });
    const link = await screen.findByRole('link', { name: /pull\/12/ });
    expect(link).toHaveAttribute('href', 'https://github.com/acme/app/pull/12');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(screen.getByText('changed outside Sentinel')).toBeInTheDocument();
    expect(screen.getByText('AGENTS.md')).toBeInTheDocument();
  });

  it('shows no state before the first GitHub read', async () => {
    renderTab({ run: null, standing_pr: { url: 'https://github.com/acme/app/pull/12', state: '', file: 'AGENTS.md', pushed_at: '2026-11-13T10:30:00Z' } });
    expect(await screen.findByRole('link', { name: /pull\/12/ })).toBeInTheDocument();
    expect(screen.queryByTestId('standing-pr-state')).not.toBeInTheDocument();
  });

  it('lists analyses newest first', async () => {
    const a = (id: number, stage: string, reason: string, spend: number): SentinelAnalysisView => ({
      id,
      run_id: `run-${id}`,
      stage,
      reason,
      spend_usd: spend,
      created_at: '2026-11-13T10:00:00Z',
    });
    renderTab({ run: null, analyses: [a(2, 'analysed', '', 0.31), a(1, 'analyse_skipped', 'not dispatched within 7 days', 0)] });
    expect(await screen.findByText('Analyses')).toBeInTheDocument();
    const items = screen.getAllByTestId('analysis');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('analysed');
    expect(items[0]).toHaveTextContent('$0.31');
    expect(items[1]).toHaveTextContent('analyse_skipped');
    expect(items[1]).toHaveTextContent('not dispatched within 7 days');
  });
});
