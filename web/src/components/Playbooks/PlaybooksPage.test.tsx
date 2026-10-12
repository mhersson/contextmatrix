import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PlaybooksPage } from './PlaybooksPage';
import { api } from '../../api/client';
import type { PlaybookDetail, PlaybookSummary } from '../../types';

vi.mock('../../api/client', () => ({
  api: {
    listPlaybooks: vi.fn().mockResolvedValue([
      { id: 'active-one', title: 'Active one', complete: 1, total: 3, segments: ['complete', 'active', 'pending'], projects: 2, updated_at: '2026-08-20T09:00:00Z' },
      { id: 'done-one', title: 'Done one', complete: 2, total: 2, segments: ['complete', 'complete'], projects: 1, updated_at: '2026-08-19T09:00:00Z' },
    ]),
    createPlaybook: vi.fn(),
    getJiraStatus: vi.fn().mockResolvedValue({ configured: false }),
    previewJiraEpic: vi.fn(() => new Promise(() => {})),
  },
}));

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: () => () => {}, connected: true, error: null, reconnectEpoch: 0 }),
}));

// The import dialog renders the project pickers; useProjects throws outside ProjectsProvider.
vi.mock('../../hooks/useProjects', () => ({
  useProjects: () => ({ projects: [], loading: false, error: null, connected: true, refreshProjects: () => {} }),
}));

vi.mock('../../hooks/useToast', () => {
  // Stable identity: a fresh showToast per render would destabilize the
  // page's fetch callback and trigger duplicate fetches in tests.
  const toast = { showToast: () => {} };
  return { useToast: () => toast };
});

vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: () => ({ isOpen: false, toggle: () => {}, close: () => {} }),
}));

const themeState = vi.hoisted(() => ({ boardsRepos: [] as { name: string; shared: boolean }[] }));

vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ boardsRepos: themeState.boardsRepos }),
}));

const summaryLine = (text: string) => (_: string, el: Element | null) => el?.tagName === 'P' && el.textContent === text;

describe('PlaybooksPage', () => {
  it('drops the crumb above the title and summarizes progress under it', async () => {
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    await screen.findByText('Active one');
    expect(screen.getByRole('heading', { name: 'Playbooks' })).toBeInTheDocument();
    expect(screen.queryByText('playbooks')).not.toBeInTheDocument();
    expect(screen.getByText(summaryLine('1 in progress, 1 completed'))).toBeInTheDocument();
  });

  it('does not state counts before the list has loaded', async () => {
    let resolveList: (value: PlaybookSummary[]) => void = () => {};
    vi.mocked(api.listPlaybooks).mockReturnValueOnce(new Promise((resolve) => { resolveList = resolve; }));
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    expect(screen.getByText('Loading...')).toBeInTheDocument();
    expect(screen.queryByText(/in progress/)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /new playbook/i }));
    expect(screen.getByLabelText('Playbook title')).toBeInTheDocument();

    resolveList([]);
    await screen.findByText('No playbooks yet');
  });

  it('cancels the ghost row on Escape and clears the draft', async () => {
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    await screen.findByText('Active one');

    fireEvent.click(screen.getByRole('button', { name: /new playbook/i }));
    fireEvent.change(screen.getByLabelText('Playbook title'), { target: { value: 'Draft' } });
    fireEvent.keyDown(screen.getByLabelText('Playbook title'), { key: 'Escape' });
    expect(screen.queryByLabelText('Playbook title')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /new playbook/i }));
    expect(screen.getByLabelText('Playbook title')).toHaveValue('');
  });

  it('shows completed playbooks as receipts under an open section and folds them on click', async () => {
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    expect(await screen.findByText('Active one')).toBeInTheDocument();
    const done = screen.getByRole('link', { name: /done one/i });
    expect(done).toHaveClass('pbl-receipt');

    const toggle = screen.getByRole('button', { name: /completed/i });
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(toggle);
    expect(screen.queryByText('Done one')).not.toBeInTheDocument();
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
  });

  it('links rows to the playbook detail page', async () => {
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    const row = await screen.findByRole('link', { name: /active one/i });
    expect(row.getAttribute('href')).toBe('/playbooks/active-one');
  });

  it('opens a ghost row above the list and disables Create playbook while the request is in flight', async () => {
    let resolveCreate: (value: PlaybookDetail) => void = () => {};
    vi.mocked(api.createPlaybook).mockReturnValue(new Promise((resolve) => { resolveCreate = resolve; }));
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    await screen.findByText('Active one');

    fireEvent.click(screen.getByRole('button', { name: /new playbook/i }));
    const titleInput = screen.getByLabelText('Playbook title');
    const ghost = titleInput.closest('.pbl-ghost');
    expect(ghost).not.toBeNull();
    const firstRow = screen.getByRole('link', { name: /active one/i });
    expect(ghost!.compareDocumentPosition(firstRow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    fireEvent.change(titleInput, { target: { value: 'New rollout' } });
    const createButton = screen.getByRole('button', { name: 'Create playbook' });
    fireEvent.click(createButton);
    expect(createButton).toBeDisabled();

    fireEvent.click(createButton);
    expect(api.createPlaybook).toHaveBeenCalledTimes(1);

    resolveCreate({ id: 'new-rollout', title: 'New rollout', created_at: '', updated_at: '', complete: 0, total: 0, entries: [] });
    await waitFor(() => expect(createButton).not.toBeDisabled());
  });

  it('sends the description only when one is typed', async () => {
    vi.mocked(api.createPlaybook).mockResolvedValue({ id: 'x', title: 'X', created_at: '', updated_at: '', complete: 0, total: 0, entries: [] });
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    await screen.findByText('Active one');

    fireEvent.click(screen.getByRole('button', { name: /new playbook/i }));
    fireEvent.change(screen.getByLabelText('Playbook title'), { target: { value: 'With notes' } });
    fireEvent.change(screen.getByLabelText('Description'), { target: { value: '  Why this route exists  ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create playbook' }));
    await waitFor(() => expect(api.createPlaybook).toHaveBeenCalledWith({ title: 'With notes', description: 'Why this route exists' }));
  });

  it('replaces the header with a centered empty state when there are no playbooks', async () => {
    vi.mocked(api.listPlaybooks).mockResolvedValueOnce([]);
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);

    expect(await screen.findByText('No playbooks yet')).toBeInTheDocument();
    expect(screen.getByText(/ordered route of cards and manual steps/)).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Playbooks' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /new playbook/i })).toBeInTheDocument();
  });

  it('creates a playbook from the empty state via Enter', async () => {
    vi.mocked(api.listPlaybooks).mockResolvedValueOnce([]);
    vi.mocked(api.createPlaybook).mockResolvedValueOnce({ id: 'first-one', title: 'First one', created_at: '', updated_at: '', complete: 0, total: 0, entries: [] });
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    await screen.findByText('No playbooks yet');

    fireEvent.click(screen.getByRole('button', { name: /new playbook/i }));
    const titleInput = screen.getByLabelText('Playbook title');
    expect(titleInput.closest('.pbl-ghost')).not.toBeNull();
    fireEvent.change(titleInput, { target: { value: 'First one' } });
    fireEvent.keyDown(titleInput, { key: 'Enter' });

    await waitFor(() => expect(api.createPlaybook).toHaveBeenCalledWith({ title: 'First one' }));
  });

  it('cancels the empty-state create flow on Escape', async () => {
    vi.mocked(api.listPlaybooks).mockResolvedValueOnce([]);
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    await screen.findByText('No playbooks yet');

    fireEvent.click(screen.getByRole('button', { name: /new playbook/i }));
    const titleInput = screen.getByLabelText('Playbook title');
    fireEvent.keyDown(titleInput, { key: 'Escape' });

    expect(screen.queryByLabelText('Playbook title')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /new playbook/i })).toBeInTheDocument();
  });
});

describe('PlaybooksPage - boards repo', () => {
  it('offers a repo select and sends the choice', async () => {
    themeState.boardsRepos = [{ name: 'team', shared: true }, { name: 'private', shared: false }];
    vi.mocked(api.listPlaybooks).mockResolvedValue([]);
    vi.mocked(api.createPlaybook).mockResolvedValue({ id: 'x', title: 'X', entries: [], created_at: '', updated_at: '' } as never);
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    await screen.findByText('No playbooks yet');
    fireEvent.click(screen.getByRole('button', { name: /new playbook/i }));
    const select = screen.getByRole('combobox', { name: 'Boards repo' });
    expect((select as HTMLSelectElement).value).toBe('team');
    fireEvent.change(select, { target: { value: 'private' } });
    fireEvent.change(screen.getByLabelText('Playbook title'), { target: { value: 'X' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create playbook' }));
    await waitFor(() => expect(vi.mocked(api.createPlaybook)).toHaveBeenCalledWith({ title: 'X', boards_repo: 'private' }));
    themeState.boardsRepos = [];
  });
});

describe('PlaybooksPage - Jira import', () => {
  const SITE = 'https://acme.atlassian.net';

  beforeEach(() => {
    vi.mocked(api.previewJiraEpic).mockClear();
    vi.mocked(api.listPlaybooks).mockResolvedValue([
      { id: 'active-one', title: 'Active one', complete: 1, total: 3, segments: ['complete', 'active', 'pending'], projects: 2, updated_at: '2026-08-20T09:00:00Z' },
    ]);
    vi.mocked(api.getJiraStatus).mockResolvedValue({ configured: true, authenticated: true, site_url: SITE });
  });

  afterEach(() => {
    vi.mocked(api.getJiraStatus).mockResolvedValue({ configured: false });
  });

  async function openGhost() {
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    await screen.findByText('Active one');
    fireEvent.click(screen.getByRole('button', { name: /new playbook/i }));
    return screen.findByRole('button', { name: 'Import from Jira' });
  }

  it('hides the import button when Jira is not configured', async () => {
    vi.mocked(api.getJiraStatus).mockResolvedValue({ configured: false });
    render(<MemoryRouter><PlaybooksPage /></MemoryRouter>);
    await screen.findByText('Active one');
    fireEvent.click(screen.getByRole('button', { name: /new playbook/i }));
    await waitFor(() => expect(api.getJiraStatus).toHaveBeenCalled());
    expect(screen.queryByRole('button', { name: 'Import from Jira' })).not.toBeInTheDocument();
  });

  it('enables the button only for an epic key or a site URL', async () => {
    const button = await openGhost();
    const title = screen.getByLabelText('Playbook title');
    expect(button).toBeDisabled();

    for (const [value, enabled] of [
      ['Q3 rollout', false],
      ['PROJ-12', true],
      [`${SITE}/browse/PROJ-12`, true],
      ['https://evil.example/browse/PROJ-12', false],
    ] as const) {
      fireEvent.change(title, { target: { value } });
      if (enabled) expect(button).toBeEnabled();
      else expect(button).toBeDisabled();
    }
  });

  it('keeps Enter as Create even when the title is a key', async () => {
    vi.mocked(api.createPlaybook).mockResolvedValue({
      id: 'x', title: 'PROJ-12', created_at: '', updated_at: '', complete: 0, total: 0, entries: [],
    } as unknown as PlaybookDetail);
    await openGhost();
    const title = screen.getByLabelText('Playbook title');
    fireEvent.change(title, { target: { value: 'PROJ-12' } });
    fireEvent.keyDown(title, { key: 'Enter' });
    await waitFor(() => expect(api.createPlaybook).toHaveBeenCalledWith({ title: 'PROJ-12' }));
    expect(api.previewJiraEpic).not.toHaveBeenCalled();
  });

  it('opens the import dialog for the parsed key', async () => {
    const button = await openGhost();
    fireEvent.change(screen.getByLabelText('Playbook title'), { target: { value: `${SITE}/browse/PROJ-12` } });
    fireEvent.click(button);
    expect(await screen.findByRole('dialog', { name: /^Import Jira epic PROJ-12/ })).toBeInTheDocument();
    expect(api.previewJiraEpic).toHaveBeenCalledWith('PROJ-12');
  });

  it('accepts a lowercase key and opens the dialog with it upper-cased', async () => {
    const button = await openGhost();
    fireEvent.change(screen.getByLabelText('Playbook title'), { target: { value: 'proj-12' } });
    expect(button).toBeEnabled();
    fireEvent.click(button);
    expect(await screen.findByRole('dialog', { name: /^Import Jira epic PROJ-12/ })).toBeInTheDocument();
    expect(api.previewJiraEpic).toHaveBeenCalledWith('PROJ-12');
  });

  it('explains and disables the import while the Jira token is failing', async () => {
    vi.mocked(api.getJiraStatus).mockResolvedValue({
      configured: true, authenticated: false, auth_error: 'captcha_required', site_url: SITE,
    });
    const button = await openGhost();
    fireEvent.change(screen.getByLabelText('Playbook title'), { target: { value: 'PROJ-12' } });
    expect(button).toBeDisabled();
    const ghost = screen.getByLabelText('Playbook title').closest('.pbl-ghost') as HTMLElement;
    expect(within(ghost).getByRole('status')).toBeInTheDocument();
  });
});
