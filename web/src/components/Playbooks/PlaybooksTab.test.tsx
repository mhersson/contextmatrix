import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useState } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { api } from '../../api/client';
import { PlaybooksProvider } from '../../hooks/usePlaybooks';
import type { APIError, PlaybookSummary } from '../../types';
import { EMPTY_PLAYBOOK_FILTER } from './playbookFilter';
import { PlaybooksTab } from './PlaybooksTab';

const toast = vi.hoisted(() => ({ showToast: vi.fn() }));
vi.mock('../../hooks/useToast', () => ({ useToast: () => toast }));

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: () => () => {}, connected: true, reconnectEpoch: 0 }),
}));

function pb(o: Partial<PlaybookSummary> & { id: string }): PlaybookSummary {
  return {
    title: o.id,
    complete: 0,
    total: 2,
    segments: ['pending', 'pending'],
    projects: 0,
    project_names: [],
    updated_at: '2026-10-01T10:00:00Z',
    ...o,
  };
}

const TITLES = ['Rollout', 'Docs refresh', 'Cleanup', 'Hardening'];

let store: PlaybookSummary[] = [];

beforeEach(() => {
  toast.showToast.mockReset();
  store = [
    pb({ id: 'rollout', title: 'Rollout', run_status: 'running', project_names: ['api'], updated_at: '2026-10-22T10:00:00Z' }),
    pb({
      id: 'docs',
      title: 'Docs refresh',
      complete: 2,
      segments: ['complete', 'complete'],
      run_status: 'stopped',
      updated_at: '2026-10-20T10:00:00Z',
    }),
    pb({ id: 'cleanup', title: 'Cleanup', run_status: 'stopped', updated_at: '2026-10-23T10:00:00Z' }),
    pb({ id: 'hardening', title: 'Hardening', pinned: true, updated_at: '2026-10-21T10:00:00Z' }),
  ];
  vi.spyOn(api, 'listPlaybooks').mockImplementation(async () => store.map((p) => ({ ...p })));
  vi.spyOn(api, 'pinPlaybook').mockImplementation(async (id, pinned) => {
    store = store.map((p) => (p.id === id ? { ...p, pinned } : p));
  });
  vi.spyOn(api, 'deletePlaybook').mockImplementation(async (id) => {
    store = store.filter((p) => p.id !== id);
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

function Where() {
  const loc = useLocation();
  return <div data-testid="where">{loc.pathname + loc.search}</div>;
}

// The page owns the filter so it outlives the tab; this stands in for it.
function Tab() {
  const [filter, setFilter] = useState(EMPTY_PLAYBOOK_FILTER);
  return <PlaybooksTab filter={filter} onFilterChange={setFilter} />;
}

function renderTab() {
  return render(
    <MemoryRouter initialEntries={['/sessions?tab=playbooks']}>
      <PlaybooksProvider>
        <Routes>
          <Route path="/sessions" element={<Tab />} />
          <Route path="*" element={null} />
        </Routes>
        <Where />
      </PlaybooksProvider>
    </MemoryRouter>,
  );
}

const where = () => screen.getByTestId('where').textContent;
const row = (title: string) => screen.getByText(title).closest('tr') as HTMLElement;
const listedTitles = () =>
  screen
    .queryAllByRole('row')
    .map((r) => TITLES.find((t) => within(r).queryByText(t)))
    .filter(Boolean);
const tick = (title: string) => fireEvent.click(within(row(title)).getByRole('checkbox'));
const bulkBar = () => screen.queryByRole('toolbar');
const ticked = () => screen.getAllByRole('checkbox').filter((c) => (c as HTMLInputElement).checked);

describe('PlaybooksTab', () => {
  it('lists the playbooks newest first and a click opens the detail page', async () => {
    renderTab();
    await screen.findByText('Rollout');

    expect(listedTitles()).toEqual(['Cleanup', 'Rollout', 'Hardening', 'Docs refresh']);

    fireEvent.click(row('Hardening'));
    expect(where()).toBe('/playbooks/hardening');
  });

  it('Status completed finds the finished playbooks, whatever their run says', async () => {
    renderTab();
    await screen.findByText('Rollout');

    fireEvent.change(screen.getByRole('combobox', { name: /status/i }), { target: { value: 'completed' } });
    expect(listedTitles()).toEqual(['Docs refresh']);

    fireEvent.change(screen.getByRole('combobox', { name: /status/i }), { target: { value: 'stopped' } });
    expect(listedTitles()).toEqual(['Cleanup']);
  });

  it('the project filter offers the projects the playbooks span', async () => {
    renderTab();
    await screen.findByText('Rollout');

    fireEvent.change(screen.getByRole('combobox', { name: /project/i }), { target: { value: 'api' } });
    expect(listedTitles()).toEqual(['Rollout']);
  });

  it('the pin toggle pins the playbook without opening it', async () => {
    renderTab();
    await screen.findByText('Rollout');

    fireEvent.click(within(row('Rollout')).getByRole('button', { pressed: false }));

    await waitFor(() => expect(api.pinPlaybook).toHaveBeenCalledWith('rollout', true));
    expect(api.pinPlaybook).toHaveBeenCalledTimes(1);
    expect(where()).toBe('/sessions?tab=playbooks');
    await waitFor(() => expect(within(row('Rollout')).getByRole('button', { pressed: true })).toBeInTheDocument());

    fireEvent.click(screen.getByRole('button', { name: 'Pinned only' }));
    expect(listedTitles()).toEqual(['Rollout', 'Hardening']);
  });

  it('bulk Pin and Unpin act on each ticked row', async () => {
    renderTab();
    await screen.findByText('Rollout');

    tick('Rollout');
    tick('Cleanup');
    fireEvent.click(within(bulkBar()!).getByRole('button', { name: 'Pin' }));
    await waitFor(() => expect(api.pinPlaybook).toHaveBeenCalledTimes(2));
    expect(api.pinPlaybook).toHaveBeenCalledWith('rollout', true);
    expect(api.pinPlaybook).toHaveBeenCalledWith('cleanup', true);

    const unpin = within(bulkBar()!).getByRole('button', { name: 'Unpin' });
    await waitFor(() => expect(unpin).toBeEnabled());
    fireEvent.click(unpin);
    await waitFor(() => expect(api.pinPlaybook).toHaveBeenCalledTimes(4));
    expect(api.pinPlaybook).toHaveBeenCalledWith('rollout', false);
    expect(api.pinPlaybook).toHaveBeenCalledWith('cleanup', false);
  });

  it('the header tick box ticks every row shown', async () => {
    renderTab();
    await screen.findByText('Rollout');

    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'o' } });
    fireEvent.click(screen.getByRole('checkbox', { name: /all/i }));
    expect(within(bulkBar()!).getByText('2')).toBeInTheDocument();

    fireEvent.change(screen.getByRole('searchbox'), { target: { value: '' } });
    expect(within(row('Rollout')).getByRole('checkbox')).toBeChecked();
    expect(within(row('Docs refresh')).getByRole('checkbox')).toBeChecked();
    expect(within(row('Cleanup')).getByRole('checkbox')).not.toBeChecked();
    expect(within(row('Hardening')).getByRole('checkbox')).not.toBeChecked();
  });

  it('bulk delete asks first, naming the count, then deletes each ticked playbook', async () => {
    renderTab();
    await screen.findByText('Rollout');

    tick('Rollout');
    tick('Cleanup');
    fireEvent.click(within(bulkBar()!).getByRole('button', { name: /delete/i }));

    const dialog = screen.getByRole('dialog');
    expect(dialog.textContent).toContain('2');
    expect(api.deletePlaybook).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole('button', { name: /delete/i }));

    await waitFor(() => expect(api.deletePlaybook).toHaveBeenCalledTimes(2));
    expect(api.deletePlaybook).toHaveBeenCalledWith('rollout');
    expect(api.deletePlaybook).toHaveBeenCalledWith('cleanup');
    await waitFor(() => expect(listedTitles()).toEqual(['Hardening', 'Docs refresh']));
    expect(bulkBar()).toBeNull();
    expect(toast.showToast).not.toHaveBeenCalled();
  });

  it('bulk delete reports a refused delete in one toast and keeps it ticked', async () => {
    vi.mocked(api.deletePlaybook).mockImplementation(async (id) => {
      if (id === 'rollout') throw { error: 'run is active', code: 'PLAYBOOK_RUN_ACTIVE' } satisfies APIError;
      store = store.filter((p) => p.id !== id);
    });
    renderTab();
    await screen.findByText('Rollout');

    tick('Rollout');
    tick('Cleanup');
    fireEvent.click(within(bulkBar()!).getByRole('button', { name: /delete/i }));
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /delete/i }));

    await waitFor(() => expect(screen.queryByText('Cleanup')).toBeNull());
    expect(within(row('Rollout')).getByRole('checkbox')).toBeChecked();
    expect(ticked()).toHaveLength(1);
    expect(within(bulkBar()!).getByText('1')).toBeInTheDocument();
    expect(toast.showToast).toHaveBeenCalledTimes(1);
    expect(toast.showToast.mock.calls[0][0]).toContain('1');
    expect(toast.showToast.mock.calls[0][1]).toBe('error');
  });

  it('a tick on a row the filters hide is dropped', async () => {
    renderTab();
    await screen.findByText('Rollout');

    tick('Rollout');
    tick('Cleanup');
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'roll' } });
    expect(within(bulkBar()!).getByText('1')).toBeInTheDocument();

    fireEvent.change(screen.getByRole('searchbox'), { target: { value: '' } });
    expect(within(row('Cleanup')).getByRole('checkbox')).not.toBeChecked();
    expect(within(row('Rollout')).getByRole('checkbox')).toBeChecked();
  });

  it('an empty list and filters with no match each show an empty state', async () => {
    const saved = store;
    store = [];
    const { unmount } = renderTab();
    const empty = await screen.findByRole('status');
    expect(within(empty).queryByRole('button')).toBeNull();
    unmount();

    store = saved;
    renderTab();
    await screen.findByText('Rollout');
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'zzz' } });

    const noMatch = screen.getByRole('status');
    expect(listedTitles()).toHaveLength(0);
    fireEvent.click(within(noMatch).getByRole('button', { name: /clear filters/i }));

    expect(listedTitles()).toHaveLength(4);
    expect(screen.getByRole('searchbox')).toHaveValue('');
  });

  it('a failed load shows an alert and no empty state', async () => {
    vi.mocked(api.listPlaybooks).mockRejectedValueOnce(new Error('boom'));
    renderTab();

    expect(await screen.findByRole('alert')).toBeInTheDocument();
    expect(screen.queryByRole('status')).toBeNull();
  });
});
