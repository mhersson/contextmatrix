import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useState } from 'react';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { api } from '../../api/client';
import { PlaybooksProvider } from '../../hooks/usePlaybooks';
import type { APIError, BoardEvent, PlaybookDetail, PlaybookSummary } from '../../types';
import { samplePreview } from '../JiraImport/previewFixture';
import { EMPTY_PLAYBOOK_FILTER } from './playbookFilter';
import { PlaybooksTab } from './PlaybooksTab';

const toast = vi.hoisted(() => ({ showToast: vi.fn() }));
vi.mock('../../hooks/useToast', () => ({ useToast: () => toast }));

// A stand-in SSE bus that keeps every handler, so a test can deliver an event.
const sse = vi.hoisted(() => ({ handlers: new Map<string, Set<(e: BoardEvent) => void>>() }));
vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({
    subscribe: (pattern: string, fn: (e: BoardEvent) => void) => {
      if (!sse.handlers.has(pattern)) sse.handlers.set(pattern, new Set());
      sse.handlers.get(pattern)!.add(fn);
      return () => sse.handlers.get(pattern)?.delete(fn);
    },
    connected: true,
    reconnectEpoch: 0,
  }),
}));

function emit(type: string, data: Record<string, unknown>) {
  act(() => {
    sse.handlers.get(type)?.forEach((fn) => fn({ type, project: '', card_id: '', timestamp: '', data } as BoardEvent));
  });
}

const themeState = vi.hoisted(() => ({ boardsRepos: [] as { name: string; shared: boolean }[], jiraEnabled: false }));
vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ boardsRepos: themeState.boardsRepos, jiraEnabled: themeState.jiraEnabled }),
}));

// The import dialog renders project pickers; useProjects throws outside ProjectsProvider.
vi.mock('../../hooks/useProjects', () => ({
  useProjects: () => ({
    projects: [{ name: 'api', display_name: 'API', prefix: 'API' }, { name: 'web', display_name: 'Web', prefix: 'WEB' }],
    loading: false,
    error: null,
    connected: true,
    refreshProjects: () => {},
  }),
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
  vi.spyOn(api, 'createPlaybook').mockImplementation(async (input) => detail(input.title));
  vi.spyOn(api, 'getJiraStatus').mockResolvedValue({ configured: false });
  vi.spyOn(api, 'previewJiraEpic').mockReturnValue(new Promise(() => {}));
  vi.spyOn(api, 'importJiraEpic').mockResolvedValue({ import_id: 'ji-1' });
  vi.spyOn(api, 'getJiraImport').mockReturnValue(new Promise(() => {}));
});

afterEach(() => {
  vi.restoreAllMocks();
  themeState.boardsRepos = [];
  themeState.jiraEnabled = false;
});

function detail(title: string): PlaybookDetail {
  const id = title.toLowerCase().replace(/\s+/g, '-');
  return { id, title, created_at: '', updated_at: '', complete: 0, total: 0, entries: [] };
}

function Where() {
  const loc = useLocation();
  return <div data-testid="where">{loc.pathname + loc.search}</div>;
}

// The page owns the filter so it outlives the tab; this stands in for it.
function Tab() {
  const [filter, setFilter] = useState(EMPTY_PLAYBOOK_FILTER);
  return <PlaybooksTab filter={filter} onFilterChange={setFilter} />;
}

function renderTab(url = '/sessions?tab=playbooks') {
  return render(
    <MemoryRouter initialEntries={[url]}>
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

const newButton = () => screen.getByRole('button', { name: /new playbook/i });
const titleInput = () => screen.getByLabelText('Playbook title');

describe('PlaybooksTab - create row', () => {
  it('+ New playbook opens the create row above the first playbook', async () => {
    renderTab();
    await screen.findByText('Rollout');
    expect(screen.queryByLabelText('Playbook title')).not.toBeInTheDocument();

    fireEvent.click(newButton());
    const createRow = titleInput().closest('tr')!;
    expect(createRow.compareDocumentPosition(row('Cleanup')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(newButton()).toBeDisabled();
  });

  it('Enter creates the playbook, sending a description only when typed, and opens it', async () => {
    renderTab();
    await screen.findByText('Rollout');

    fireEvent.click(newButton());
    fireEvent.change(titleInput(), { target: { value: 'New rollout' } });
    fireEvent.keyDown(titleInput(), { key: 'Enter' });
    await waitFor(() => expect(where()).toBe('/playbooks/new-rollout'));
    expect(api.createPlaybook).toHaveBeenCalledWith({ title: 'New rollout' });
  });

  it('a typed description is sent trimmed', async () => {
    renderTab();
    await screen.findByText('Rollout');

    fireEvent.click(newButton());
    fireEvent.change(titleInput(), { target: { value: 'With notes' } });
    fireEvent.change(screen.getByLabelText('Description'), { target: { value: '  Why this route exists  ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create playbook' }));
    await waitFor(() =>
      expect(api.createPlaybook).toHaveBeenCalledWith({ title: 'With notes', description: 'Why this route exists' }),
    );
  });

  it('Create playbook sends one request while it is in flight', async () => {
    let resolve: (d: PlaybookDetail) => void = () => {};
    vi.mocked(api.createPlaybook).mockReturnValue(new Promise((r) => { resolve = r; }));
    renderTab();
    await screen.findByText('Rollout');

    fireEvent.click(newButton());
    fireEvent.change(titleInput(), { target: { value: 'New rollout' } });
    const create = screen.getByRole('button', { name: 'Create playbook' });
    fireEvent.click(create);
    expect(create).toBeDisabled();
    fireEvent.click(create);
    fireEvent.keyDown(titleInput(), { key: 'Enter' });
    expect(api.createPlaybook).toHaveBeenCalledTimes(1);

    resolve(detail('New rollout'));
    await waitFor(() => expect(where()).toBe('/playbooks/new-rollout'));
  });

  it('Escape closes the row and drops the draft', async () => {
    renderTab();
    await screen.findByText('Rollout');

    fireEvent.click(newButton());
    fireEvent.change(titleInput(), { target: { value: 'Draft' } });
    fireEvent.keyDown(titleInput(), { key: 'Escape' });
    expect(screen.queryByLabelText('Playbook title')).not.toBeInTheDocument();
    expect(listedTitles()).toHaveLength(4);

    fireEvent.click(newButton());
    expect(titleInput()).toHaveValue('');
  });

  it('?new=1 opens the row once and leaves the URL without it', async () => {
    renderTab('/sessions?tab=playbooks&new=1');
    expect(titleInput()).toBeInTheDocument();
    await waitFor(() => expect(where()).toBe('/sessions?tab=playbooks'));

    fireEvent.keyDown(titleInput(), { key: 'Escape' });
    await screen.findByText('Rollout');
    expect(screen.queryByLabelText('Playbook title')).not.toBeInTheDocument();
  });

  it('opens while the list is still loading, and keeps the draft when the playbooks arrive', async () => {
    let resolve: (list: PlaybookSummary[]) => void = () => {};
    vi.mocked(api.listPlaybooks).mockReturnValue(new Promise((r) => { resolve = r; }));
    renderTab();

    fireEvent.click(newButton());
    fireEvent.change(titleInput(), { target: { value: 'Draft' } });

    resolve(store);
    await screen.findByText('Rollout');
    expect(titleInput()).toHaveValue('Draft');
    expect(titleInput().closest('tr')!.compareDocumentPosition(row('Cleanup')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('opens under a filter that matches nothing, in place of the empty state', async () => {
    renderTab();
    await screen.findByText('Rollout');
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'zzz' } });
    expect(screen.getByRole('status')).toBeInTheDocument();

    fireEvent.click(newButton());
    expect(titleInput()).toBeInTheDocument();
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    expect(listedTitles()).toHaveLength(0);

    fireEvent.keyDown(titleInput(), { key: 'Escape' });
    expect(screen.getByRole('status')).toBeInTheDocument();
  });

  it('opens on an empty list without the empty state', async () => {
    store = [];
    renderTab();
    await screen.findByRole('status');

    fireEvent.click(newButton());
    expect(titleInput()).toBeInTheDocument();
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('sends the chosen boards repo on a multi-repo instance', async () => {
    themeState.boardsRepos = [{ name: 'team', shared: true }, { name: 'private', shared: false }];
    renderTab();
    await screen.findByText('Rollout');

    fireEvent.click(newButton());
    const select = screen.getByRole('combobox', { name: 'Boards repo' });
    expect(select).toHaveValue('team');
    fireEvent.change(select, { target: { value: 'private' } });
    fireEvent.change(titleInput(), { target: { value: 'X' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create playbook' }));
    await waitFor(() => expect(api.createPlaybook).toHaveBeenCalledWith({ title: 'X', boards_repo: 'private' }));
  });
});

describe('PlaybooksTab - Jira import', () => {
  const SITE = 'https://acme.atlassian.net';

  beforeEach(() => {
    themeState.jiraEnabled = true;
    vi.mocked(api.getJiraStatus).mockResolvedValue({ configured: true, authenticated: true, site_url: SITE });
  });

  const createRow = () => titleInput().closest('tr')!;
  const toolbarImport = () => within(screen.getByRole('search').parentElement!).queryByRole('button', { name: 'Import from Jira' });
  const rowImport = () => within(createRow()).findByRole('button', { name: 'Import from Jira' });

  async function openRow() {
    renderTab();
    await screen.findByText('Rollout');
    fireEvent.click(newButton());
    return rowImport();
  }

  it('Import from Jira in the toolbar opens the create row, without asking Jira until then', async () => {
    renderTab();
    await screen.findByText('Rollout');
    expect(api.getJiraStatus).not.toHaveBeenCalled();

    fireEvent.click(toolbarImport()!);
    expect(createRow()).toBeInTheDocument();
    expect(toolbarImport()).toBeDisabled();
    expect(newButton()).toBeDisabled();
    await rowImport();
    expect(api.getJiraStatus).toHaveBeenCalledTimes(1);
  });

  it('offers no import anywhere when Jira is not configured', async () => {
    themeState.jiraEnabled = false;
    vi.mocked(api.getJiraStatus).mockResolvedValue({ configured: false });
    renderTab();
    await screen.findByText('Rollout');
    expect(toolbarImport()).not.toBeInTheDocument();

    fireEvent.click(newButton());
    await waitFor(() => expect(api.getJiraStatus).toHaveBeenCalled());
    expect(screen.queryByRole('button', { name: 'Import from Jira' })).not.toBeInTheDocument();
  });

  it('opens the import dialog for the parsed key', async () => {
    const button = await openRow();
    fireEvent.change(titleInput(), { target: { value: `${SITE}/browse/PROJ-12` } });
    fireEvent.click(button);
    expect(await screen.findByRole('dialog', { name: /^Import Jira epic PROJ-12/ })).toBeInTheDocument();
    expect(api.previewJiraEpic).toHaveBeenCalledWith('PROJ-12');
  });

  it.each(['unauthorized', 'captcha_required'] as const)('disables the import and says why when Jira reports %s', async (auth_error) => {
    vi.mocked(api.getJiraStatus).mockResolvedValue({ configured: true, authenticated: false, auth_error, site_url: SITE });
    const button = await openRow();
    fireEvent.change(titleInput(), { target: { value: 'PROJ-12' } });
    expect(button).toBeDisabled();
    expect(within(createRow()).getByRole('status')).toBeInTheDocument();
  });

  it('keeps the import enabled, with the warning, when only the status probe fails', async () => {
    vi.mocked(api.getJiraStatus).mockResolvedValue({ configured: true, authenticated: false, auth_error: 'unreachable', site_url: SITE });
    const button = await openRow();
    fireEvent.change(titleInput(), { target: { value: 'PROJ-12' } });
    expect(button).toBeEnabled();
    expect(within(createRow()).getByRole('status')).toBeInTheDocument();
  });

  it('keeps the import dialog and its progress when the first import fills an empty list', async () => {
    store = [];
    vi.mocked(api.previewJiraEpic).mockResolvedValueOnce(samplePreview());
    renderTab();
    await screen.findByRole('status');

    fireEvent.click(newButton());
    const button = await rowImport();
    fireEvent.change(titleInput(), { target: { value: 'PROJ-1' } });
    fireEvent.click(button);
    const dialog = await screen.findByRole('dialog', { name: /Checkout revamp/ });
    fireEvent.change(await within(dialog).findByLabelText('Project for PROJ-7'), { target: { value: 'web' } });
    fireEvent.click(within(dialog).getByRole('button', { name: /^Import 3 issues/ }));
    await within(dialog).findByRole('list', { name: 'Import results' });
    // The list can paint before the hook's passive effect subscribes; an event emitted earlier is dropped.
    await waitFor(() => expect(sse.handlers.get('jira_import.progress')?.size).toBeGreaterThan(0));
    emit('jira_import.progress', { import_id: 'ji-1', key: 'PROJ-3', status: 'created', project: 'api', card_id: 'API-001', reason: '' });

    // The import created the epic's playbook, so the list fills mid-run.
    store = [pb({ id: 'checkout', title: 'Checkout revamp' })];
    emit('playbook.*', {});
    await screen.findByRole('button', { name: 'Checkout revamp' }, { timeout: 3000 });

    expect(dialog).toBeInTheDocument();
    expect(within(dialog).getByRole('list', { name: 'Import results' })).toHaveTextContent('API-001');
  });
});
