import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { api } from '../../../api/client';
import type {
  Card,
  SentinelAnalysisView,
  SentinelCardView,
  SentinelFindingView,
  SentinelHealView,
  SentinelRunView,
  SentinelStandingPR,
} from '../../../types';
import { MemoryRouter } from 'react-router';
import { cardStatus, findingLabel, stateWord, type CardStatus } from '../../../lib/sentinel';
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
  vi.spyOn(api, 'getCardSentinel').mockResolvedValue({
    heals: [],
    findings: [],
    analyses: [],
    standing_pr: null,
    guidance_revision: null,
    ...view,
  });
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

const finding = (kind: SentinelFindingView['kind'], title: string, over: Partial<SentinelFindingView> = {}): SentinelFindingView => ({
  id: 1,
  state: '',
  reason: '',
  output: '',
  kind,
  title,
  text: `${title} text`,
  evidence: '',
  created_at: '2026-11-13T10:00:00Z',
  ...over,
});

const analysis = (id: number, stage: string, reason: string, spend: number): SentinelAnalysisView => ({
  id,
  run_id: `run-${id}`,
  stage,
  reason,
  spend_usd: spend,
  created_at: '2026-11-13T10:00:00Z',
});

const pr: SentinelStandingPR = {
  url: 'https://github.com/acme/app/pull/12',
  state: 'open',
  file: 'AGENTS.md',
  pushed_at: '2026-11-13T10:30:00Z',
};

const panel = () => screen.getByRole('tabpanel');
const tab = (name: RegExp) => screen.getByRole('tab', { name });
const bodyRows = (table: HTMLElement) => within(table).getAllByRole('row').filter((r) => r.closest('tbody'));

/** The heals table: the one whose rows carry an expand button. */
async function healsTable(): Promise<HTMLElement> {
  await screen.findAllByRole('table');
  const t = within(panel())
    .getAllByRole('table')
    .find((el) => within(el).queryAllByRole('button').length > 0);
  if (!t) throw new Error('no expandable table');
  return t;
}

function rowFor(text: string): HTMLElement {
  const row = screen.getByText(text).closest('tr');
  if (!row) throw new Error(`no row for ${text}`);
  return row;
}

describe('SentinelTab', () => {
  afterEach(() => vi.restoreAllMocks());

  describe('sub-tabs', () => {
    it('opens on Heals when the card has heals', async () => {
      renderTab({ run, heals: [heal], findings: [finding('guidance', 'Add a hint')] });
      expect(await screen.findByText('gave_up')).toBeInTheDocument();
      expect(tab(/^Heals/)).toHaveAttribute('aria-selected', 'true');
      expect(screen.queryByText('Add a hint')).not.toBeInTheDocument();
    });

    it('opens on Findings when the card has no heals', async () => {
      renderTab({ run, findings: [finding('guidance', 'Add a hint')] });
      expect(await screen.findByText('Add a hint')).toBeInTheDocument();
      expect(tab(/^Findings/)).toHaveAttribute('aria-selected', 'true');
    });

    it('shows the chosen table and only that one', async () => {
      renderTab({
        run,
        heals: [heal],
        findings: [finding('guidance', 'Add a hint')],
        analyses: [analysis(1, 'analysed', 'three findings', 0.31)],
        standing_pr: pr,
      });
      await screen.findByText('gave_up');

      fireEvent.click(tab(/^Findings/));
      expect(screen.getByText('Add a hint')).toBeInTheDocument();
      expect(screen.queryByText('gave_up')).not.toBeInTheDocument();

      fireEvent.click(tab(/^Analyses/));
      expect(screen.getByText('three findings')).toBeInTheDocument();
      expect(screen.queryByText('Add a hint')).not.toBeInTheDocument();

      fireEvent.click(tab(/^PR/));
      expect(screen.getByRole('link', { name: /pull\/12/ })).toBeInTheDocument();
      expect(screen.queryByText('three findings')).not.toBeInTheDocument();

      fireEvent.click(tab(/^Heals/));
      expect(screen.getByText('gave_up')).toBeInTheDocument();
      expect(tab(/^Heals/)).toHaveAttribute('aria-selected', 'true');
      expect(tab(/^PR/)).toHaveAttribute('aria-selected', 'false');
    });

    it('counts heals, friction findings and analyses on the tabs', async () => {
      renderTab({
        run,
        heals: [heal, { ...heal, id: 2 }],
        findings: [finding('guidance', 'a', { id: 1 }), finding('code', 'b', { id: 2 }), finding('card_advice', 'c', { id: 3 })],
        analyses: [analysis(1, 'analysed', '', 0)],
      });
      await screen.findAllByText('gave_up');
      expect(tab(/^Heals/)).toHaveTextContent('2');
      expect(tab(/^Findings/)).toHaveTextContent('2');
      expect(tab(/^Analyses/)).toHaveTextContent('1');
    });

    it.each([/^Heals/, /^Findings/, /^Analyses/, /^PR/])('has an empty state on %s', async (name) => {
      renderTab({ run });
      fireEvent.click(await screen.findByRole('tab', { name }));
      expect(within(panel()).getByTestId('sentinel-empty')).toBeInTheDocument();
      const tables = within(panel()).queryAllByRole('table');
      expect(tables.filter((t) => t.dataset.testid !== 'sentinel-facts')).toHaveLength(0);
    });

    it.each<[RegExp, Partial<SentinelCardView>]>([
      [/^Heals/, { heals: [heal] }],
      [/^Findings/, { findings: [finding('guidance', 'Add a hint')] }],
      [/^Analyses/, { analyses: [analysis(1, 'analysed', '', 0)] }],
      [/^PR/, { standing_pr: pr }],
    ])('has no empty state on %s with data', async (name, data) => {
      renderTab({ run, ...data });
      fireEvent.click(await screen.findByRole('tab', { name }));
      expect(within(panel()).queryByTestId('sentinel-empty')).not.toBeInTheDocument();
    });
  });

  describe('summary', () => {
    it('shows the attempts against the cap, the total spend and the friction count', async () => {
      renderTab({
        run,
        heals: [heal],
        analyses: [analysis(1, 'analysed', '', 0.31)],
        findings: [finding('guidance', 'a', { id: 1 }), finding('hard_card', 'b', { id: 2 })],
      });
      const summary = await screen.findByRole('group');
      expect(within(summary).getByText('1 of 2')).toBeInTheDocument();
      expect(within(summary).getByText('$0.73')).toBeInTheDocument();
      expect(within(summary).getByText('1')).toBeInTheDocument();
    });

    it.each<[CardStatus, Partial<SentinelCardView> & { run: SentinelRunView | null }]>([
      ['healed', { run, heals: [heal] }],
      ['needs_a_human', { run, heals: [{ ...heal, stage: 'parked' }] }],
      ['healing', { run, heals: [{ ...heal, stage: 'investigate' }] }],
      ['analysing', { run, analyses: [analysis(1, 'analyse', '', 0)] }],
      ['watching', { run }],
      ['not_ours', { run: null }],
    ])('names the status %s', async (status, data) => {
      const view = { heals: [], findings: [], analyses: [], standing_pr: null, guidance_revision: null, ...data };
      expect(cardStatus(view)).toBe(status);
      renderTab(data);
      expect(within(await screen.findByRole('group')).getByText(stateWord(status))).toBeInTheDocument();
    });
  });

  describe('heals', () => {
    it('opens the newest heal and leaves the older ones closed', async () => {
      renderTab({ run, heals: [heal, { ...heal, id: 2, cause: 'verify_failed', diagnosis: 'Older diagnosis.' }] });
      expect(await screen.findByText('The criteria were vague.')).toBeInTheDocument();
      expect(screen.queryByText('Older diagnosis.')).not.toBeInTheDocument();
      expect(within(rowFor('gave_up')).getByRole('button')).toHaveAttribute('aria-expanded', 'true');
      expect(within(rowFor('verify_failed')).getByRole('button')).toHaveAttribute('aria-expanded', 'false');
    });

    it('expands and collapses a heal row', async () => {
      renderTab({ run, heals: [heal, { ...heal, id: 2, cause: 'verify_failed', diagnosis: 'Older diagnosis.' }] });
      await screen.findByText('verify_failed');

      fireEvent.click(rowFor('verify_failed'));
      expect(screen.getByText('Older diagnosis.')).toBeInTheDocument();
      expect(within(rowFor('verify_failed')).getByRole('button')).toHaveAttribute('aria-expanded', 'true');

      fireEvent.click(within(rowFor('verify_failed')).getByRole('button'));
      expect(screen.queryByText('Older diagnosis.')).not.toBeInTheDocument();

      fireEvent.click(rowFor('gave_up'));
      expect(screen.queryByText('The criteria were vague.')).not.toBeInTheDocument();
    });

    it('lists one row per heal with its stage', async () => {
      renderTab({ run, heals: [heal, { ...heal, id: 2, stage: 'parked' }, { ...heal, id: 3, kind: 'forward', stage: 'forwarded', source_card: 'T-009' }] });
      const rows = bodyRows(await healsTable()).filter((r) => within(r).queryByRole('button'));
      expect(rows).toHaveLength(3);
      expect(rows[1]).toHaveTextContent('parked');
      expect(rows[2]).toHaveTextContent('T-009');
    });

    it('shows the diagnosis, both diffs and the spend in the detail', async () => {
      renderTab({ run, heals: [heal] });
      const detail = (await screen.findByText('The criteria were vague.')).closest('td')!;
      expect(within(detail).getByText('- old line')).toBeInTheDocument();
      expect(within(detail).getByText('+ new line')).toBeInTheDocument();
      expect(within(detail).getByText('keep')).toBeInTheDocument();
      expect(within(detail).getByText('- command: make a')).toBeInTheDocument();
      expect(within(detail).getByText('+ command: make b')).toBeInTheDocument();
      expect(within(detail).getByText('$0.42')).toBeInTheDocument();
    });

    it('renders each diff line as kept, removed or added, in order', async () => {
      renderTab({ run, heals: [heal] });
      const lines = (marked: string) => Array.from(screen.getByText(marked).closest('pre')!.children).map((l) => l.textContent);
      await screen.findByText('+ new line');
      expect(lines('+ new line')).toEqual(['  keep', '- old line', '+ new line']);
      expect(lines('+ command: make b')).toEqual(['- command: make a', '+ command: make b']);
    });

    it('shows retired subtasks', async () => {
      renderTab({ run, heals: [heal] });
      expect(await screen.findByText(/T-002/)).toBeInTheDocument();
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

    it.each<Partial<SentinelHealView>>([
      { stage: 'parked', change: { ...heal.change!, fresh: true } },
      { kind: 'forward', stage: 'forwarded', source_card: 'T-009' },
    ])('shows no rerun mode when Sentinel did not rerun the card (%j)', async (over) => {
      renderTab({ run, heals: [{ ...heal, ...over }] });
      expect(await screen.findByText('The criteria were vague.')).toBeInTheDocument();
      expect(screen.queryByText(/starts fresh|resumes/)).not.toBeInTheDocument();
    });

    it('lists why the section was dropped', async () => {
      renderTab({ run, heals: [{ ...heal, dropped: ['introduces gen-tool', 'introduces gen.yaml'] }] });
      await screen.findByText('The criteria were vague.');
      const reasons = screen.getAllByRole('listitem').map((li) => li.textContent);
      expect(reasons).toEqual(['introduces gen-tool', 'introduces gen.yaml']);
    });

    it('shows no dropped list when nothing was dropped', async () => {
      renderTab({ run, heals: [heal] });
      await screen.findByText('The criteria were vague.');
      expect(screen.queryByRole('list')).not.toBeInTheDocument();
    });

    it("shows a heal's reason", async () => {
      renderTab({ run, heals: [{ ...heal, stage: 'parked', reason: 'the fix would change what the card asks for' }] });
      expect(await screen.findByText('the fix would change what the card asks for')).toBeInTheDocument();
    });

    it('names the card a forward came from', async () => {
      renderTab({ run, heals: [{ ...heal, kind: 'forward', stage: 'forwarded', source_card: 'T-009' }] });
      expect(await screen.findByText(/T-009/)).toBeInTheDocument();
    });

    it('renders model text as plain text', async () => {
      renderTab({ run, heals: [{ ...heal, diagnosis: '<img src=x onerror=alert(1)>' }] });
      expect(await screen.findByText('<img src=x onerror=alert(1)>')).toBeInTheDocument();
      expect(document.querySelector('img')).toBeNull();
    });
  });

  describe('facts', () => {
    // Facts sit on the Heals sub-tab, which a card without heals does not open on.
    const facts = async () => {
      fireEvent.click(await screen.findByRole('tab', { name: /^Heals/ }));
      return screen.getByTestId('sentinel-facts');
    };

    it('shows the attempts and the last stop', async () => {
      renderTab({ run: { ...run, stop_cause: 'usage_limit', stopped_at: '2026-11-13T10:30:00Z' } });
      const t = await facts();
      expect(within(t).getByText('1 of 2')).toBeInTheDocument();
      expect(within(t).getByText(/usage_limit/)).toBeInTheDocument();
      expect(within(t).getAllByRole('row')).toHaveLength(4);
    });

    it('has no last stop before the run stopped', async () => {
      renderTab({ run });
      expect(within(await facts()).getAllByRole('row')).toHaveLength(3);
    });

    it('shows a stop that came with no cause', async () => {
      renderTab({ run: { ...run, stopped_at: '2026-11-13T10:30:00Z' } });
      expect(within(await facts()).getAllByRole('row')).toHaveLength(4);
    });

    it("hides the stop cause when a human stopped the run", async () => {
      renderTab({ run: { ...run, human_stop: true, stop_cause: 'killed', stopped_at: '2026-11-13T10:30:00Z' } });
      const t = await facts();
      expect(within(t).getAllByRole('row')).toHaveLength(4);
      expect(within(t).queryByText(/killed/)).not.toBeInTheDocument();
    });
  });

  describe('not launched here', () => {
    it('shows the callout and no facts when this instance did not launch the run', async () => {
      renderTab({ run: null, standing_pr: pr });
      await waitFor(() => expect(api.getCardSentinel).toHaveBeenCalledWith('p', 'T-001', expect.anything()));
      expect(await screen.findByRole('note')).toBeInTheDocument();
      fireEvent.click(screen.getByRole('tab', { name: /^Heals/ }));
      expect(screen.queryByTestId('sentinel-facts')).not.toBeInTheDocument();
    });

    it('shows no callout for a run launched here', async () => {
      renderTab({ run });
      await screen.findByRole('group');
      expect(screen.queryByRole('note')).not.toBeInTheDocument();
    });
  });

  describe('findings', () => {
    it('expands and collapses a finding row to its text, evidence and outcome', async () => {
      renderTab({
        run,
        findings: [finding('guidance', 'Add a hint', { evidence: 'go.sum changed twice', state: 'proposed', output: 'https://github.com/acme/app/pull/3' })],
      });
      await screen.findByText('Add a hint');
      expect(screen.queryByText('Add a hint text')).not.toBeInTheDocument();

      fireEvent.click(rowFor('Add a hint'));
      expect(screen.getByText('Add a hint text')).toBeInTheDocument();
      expect(screen.getByText('go.sum changed twice')).toBeInTheDocument();
      expect(within(rowFor('Add a hint')).getByRole('button')).toHaveAttribute('aria-expanded', 'true');

      fireEvent.click(rowFor('Add a hint'));
      expect(screen.queryByText('Add a hint text')).not.toBeInTheDocument();
    });

    it('names the expand button of a finding with no title', async () => {
      const untitled = finding('code', '', { text: 'The build flakes.\nOn a cold cache.' });
      renderTab({ run, findings: [untitled] });
      const button = await screen.findByRole('button', { expanded: false });
      expect(button).toHaveAccessibleName(findingLabel(untitled));
      expect(button).not.toHaveAccessibleName('');
    });

    it("links each finding's outcome", async () => {
      renderTab({
        run: null,
        standing_pr: pr,
        findings: [
          finding('guidance', 'proposed one', { id: 3, state: 'proposed', output: 'https://github.com/acme/app/pull/3' }),
          finding('guidance', 'proposed before the link', { id: 4, state: 'proposed' }),
          finding('code', 'carded one', { id: 5, state: 'carded', output: 'T-040' }),
          finding('code', 'dropped one', { id: 6, state: 'dropped', reason: 'no evidence' }),
        ],
      });
      await screen.findByText('proposed one');
      for (const title of ['proposed one', 'proposed before the link', 'carded one', 'dropped one']) {
        fireEvent.click(rowFor(title));
      }
      const links = within(panel()).getAllByRole('link');
      expect(links.map((a) => a.getAttribute('href'))).toEqual([
        'https://github.com/acme/app/pull/3',
        'https://github.com/acme/app/pull/12',
        '/projects/p?card=T-040',
      ]);
      for (const a of links.slice(0, 2)) {
        expect(a).toHaveAttribute('target', '_blank');
        expect(a).toHaveAttribute('rel', 'noopener noreferrer');
      }
      expect(links[2]).not.toHaveAttribute('target');
      expect(screen.getByText(/no evidence/)).toBeInTheDocument();
    });

    it('lists notes below the findings with their title, text and evidence', async () => {
      renderTab({
        run,
        findings: [
          finding('guidance', 'Add a hint', { id: 1 }),
          finding('hard_card', 'Too big', { id: 2, evidence: 'ten criteria' }),
          finding('card_advice', 'Split it', { id: 3 }),
        ],
      });
      await screen.findByText('Add a hint');
      const [findings, notes] = within(panel()).getAllByRole('table');
      expect(bodyRows(findings)).toHaveLength(1);
      expect(bodyRows(notes)).toHaveLength(2);
      expect(within(notes).getByText('Split it')).toBeInTheDocument();
      expect(within(notes).getByText('Too big text')).toBeInTheDocument();
      expect(within(notes).getByText('ten criteria')).toBeInTheDocument();
    });

    it('marks project-verify notes, the ones the server sends only to admins', async () => {
      renderTab({
        run,
        findings: [finding('project_verify', 'Slow verify', { id: 1 }), finding('card_advice', 'Split it', { id: 2 })],
      });
      await screen.findByText('Slow verify');
      expect(within(rowFor('Slow verify')).getByText('admin')).toBeInTheDocument();
      expect(within(rowFor('Split it')).queryByText('admin')).not.toBeInTheDocument();
    });

    it('shows a notes-only card the notes and no findings rows', async () => {
      renderTab({ run, findings: [finding('card_advice', 'Split it')] });
      await screen.findByText('Split it');
      expect(within(panel()).getAllByRole('table')).toHaveLength(1);
    });
  });

  describe('analyses', () => {
    it('lists analyses newest first with their result, reason and cost', async () => {
      renderTab({ run: null, analyses: [analysis(2, 'analysed', '', 0.31), analysis(1, 'analyse_skipped', 'not dispatched within 7 days', 0)] });
      fireEvent.click(await screen.findByRole('tab', { name: /^Analyses/ }));
      const rows = bodyRows(within(panel()).getByRole('table'));
      expect(rows).toHaveLength(2);
      expect(rows[0]).toHaveTextContent('$0.31');
      expect(rows[1]).toHaveTextContent('not dispatched within 7 days');
    });
  });

  describe('guidance PR', () => {
    const openPR = async () => fireEvent.click(await screen.findByRole('tab', { name: /^PR/ }));

    it('links the standing PR and names its file', async () => {
      renderTab({ run: null, standing_pr: { ...pr, state: 'changed_outside' } });
      await openPR();
      const link = screen.getByRole('link', { name: /pull\/12/ });
      expect(link).toHaveAttribute('href', 'https://github.com/acme/app/pull/12');
      expect(link).toHaveAttribute('target', '_blank');
      expect(link).toHaveAttribute('rel', 'noopener noreferrer');
      expect(screen.getByText('AGENTS.md')).toBeInTheDocument();
    });

    it('has no state row before the first GitHub read', async () => {
      const { unmount } = renderTab({ run: null, standing_pr: pr });
      await openPR();
      const withState = within(panel()).getAllByRole('row').length;
      unmount();

      renderTab({ run: null, standing_pr: { ...pr, state: '' } });
      await openPR();
      expect(within(panel()).getAllByRole('row')).toHaveLength(withState - 1);
    });

    it('shows a revision that did not go out, with its reason, even without a PR', async () => {
      renderTab({
        run: null,
        guidance_revision: { stage: 'revision_failed', reason: 'token mint failed', updated_at: '2026-11-13T10:30:00Z' },
      });
      await openPR();
      expect(screen.getByText('token mint failed')).toBeInTheDocument();
      expect(within(panel()).queryByRole('link')).not.toBeInTheDocument();
    });

    it('shows the PR and the failed revision together', async () => {
      renderTab({
        run: null,
        standing_pr: pr,
        guidance_revision: { stage: 'revision_refused', reason: 'an entry is absent', updated_at: '2026-11-13T10:30:00Z' },
      });
      await openPR();
      expect(screen.getByRole('link', { name: /pull\/12/ })).toBeInTheDocument();
      expect(screen.getByText('an entry is absent')).toBeInTheDocument();
    });
  });

  it('refetches on a Sentinel event and on events for this card only', async () => {
    renderTab({ run: null });
    const spy = api.getCardSentinel as ReturnType<typeof vi.fn>;
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1));

    act(() => bus.handlers.get('card.*')?.({ project: 'p', card_id: 'OTHER-1' }));
    expect(spy).toHaveBeenCalledTimes(1);

    act(() => bus.handlers.get('card.*')?.({ project: 'p', card_id: 'T-001' }));
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(2));

    // Sentinel events are debounced by a second.
    act(() => bus.handlers.get('sentinel.*')?.({ project: 'p', card_id: 'T-001' }));
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(3), { timeout: 3000 });
  });
});
