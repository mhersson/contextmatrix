import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { Sidebar } from './Sidebar';
import { PlaybooksProvider } from '../../hooks/usePlaybooks';
import { api } from '../../api/client';
import type { LLMChannelView, PlaybookSummary } from '../../types';
import type { AuthContextValue } from '../../hooks/useAuth';

vi.mock('../../hooks/useProjects', () => ({
  useProjects: vi.fn(),
}));

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({
    subscribe: () => () => {},
    connected: true,
    reconnectEpoch: 0,
  }),
}));

vi.mock('../../hooks/ProjectSummariesProvider', () => ({
  useProjectSummariesContext: vi.fn(() => ({
    summaries: new Map(),
    errors: new Set(),
    loading: false,
    refresh: vi.fn(),
  })),
}));

const themeState = vi.hoisted(() => ({ boardsRepos: [] as { name: string; shared: boolean }[] }));
const syncState = vi.hoisted(() => ({ current: [] as import('../../types').SyncStatus[] }));

vi.mock('../../hooks/useTheme', () => ({
  useTheme: vi.fn(() => ({ theme: 'dark', palette: 'everforest', version: '', boardsRepos: themeState.boardsRepos, setTheme: () => {}, setPalette: () => {} })),
}));

vi.mock('../../hooks/useSync', () => ({
  useSync: () => ({
    syncStatuses: syncState.current,
    syncStatus: syncState.current[0] ?? null,
    statusFor: (repo?: string) => (repo ? syncState.current.find((s) => s.repo === repo) ?? null : syncState.current[0] ?? null),
    triggerSync: async () => {},
  }),
}));

const authState = vi.hoisted(() => ({ current: null as unknown }));
const setAuthState = (v: AuthContextValue | null) => { authState.current = v; };
vi.mock('../../hooks/useAuth', () => ({
  useOptionalAuth: () => authState.current,
}));

vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: () => ({ isOpen: true, toggle: () => {}, close: () => {} }),
}));

vi.mock('./SessionSections', () => ({
  SessionSections: () => null,
}));

import { useProjects } from '../../hooks/useProjects';

const mockUseProjects = vi.mocked(useProjects);

function renderSidebar(props?: { mobileOpen?: boolean; onMobileClose?: () => void }) {
  return render(
    <MemoryRouter>
      <PlaybooksProvider>
        <Sidebar onNewProject={() => {}} onNewSession={() => {}} {...props} />
      </PlaybooksProvider>
    </MemoryRouter>
  );
}

const defaultProjects = [
  { name: 'zebra', prefix: 'Z', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
  { name: 'alpha', prefix: 'A', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
  { name: 'mango', prefix: 'M', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
];

function playbook(id: string): PlaybookSummary {
  return { id, title: id, complete: 0, total: 2, segments: [], projects: 1, updated_at: '' };
}

function channel(claudeCode: boolean): LLMChannelView {
  const zero = { cards: 0, chats: 0, blueprints: 0 };
  return {
    mode: claudeCode ? 'claude_code' : 'endpoint',
    switchable: claudeCode,
    endpoint: { type: 'openrouter', label: 'OpenRouter' },
    claude_code: claudeCode,
    in_flight: { endpoint: zero, claude_code: zero },
    exceptions: [],
    usage: [],
  };
}

beforeEach(() => {
  vi.spyOn(api, 'getLLMChannel').mockResolvedValue(channel(false));
});

describe('Sidebar', () => {
  beforeEach(() => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([]);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders projects in alphabetical order regardless of input order', () => {
    mockUseProjects.mockReturnValue({
      projects: [
        { name: 'zebra', prefix: 'Z', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
        { name: 'alpha', prefix: 'A', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
        { name: 'mango', prefix: 'M', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
      ],
      loading: false,
      error: null,
      connected: true,
      refreshProjects: async () => {},
    });

    renderSidebar();

    const projectLinks = screen.getAllByRole('link').filter(
      (link) => link.getAttribute('href')?.startsWith('/projects/')
    );

    const names = projectLinks.map((link) => link.textContent);
    expect(names).toEqual(['alpha', 'mango', 'zebra']);
  });

  it('sorts projects case-insensitively', () => {
    mockUseProjects.mockReturnValue({
      projects: [
        { name: 'Bravo', prefix: 'B', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
        { name: 'alpha', prefix: 'A', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
        { name: 'Charlie', prefix: 'C', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
      ],
      loading: false,
      error: null,
      connected: true,
      refreshProjects: async () => {},
    });

    renderSidebar();

    const projectLinks = screen.getAllByRole('link').filter(
      (link) => link.getAttribute('href')?.startsWith('/projects/')
    );

    const names = projectLinks.map((link) => link.textContent);
    expect(names).toEqual(['alpha', 'Bravo', 'Charlie']);
  });

  it('marks a POC project with a POC label that carries a tooltip', () => {
    mockUseProjects.mockReturnValue({
      projects: [
        { name: 'alpha', prefix: 'A', next_id: 1, states: [], types: [], priorities: [], transitions: {}, poc: true },
        { name: 'mango', prefix: 'M', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
      ],
      loading: false,
      error: null,
      connected: true,
      refreshProjects: async () => {},
    });

    renderSidebar();

    const labels = screen.getAllByText('POC');
    expect(labels).toHaveLength(1);
    expect(labels[0].closest('a')).toHaveAttribute('href', '/projects/alpha');
    expect(labels[0]).toHaveAttribute('title', expect.stringMatching(/\S/));
  });

  describe('work flow order', () => {
    beforeEach(() => {
      mockUseProjects.mockReturnValue({
        projects: defaultProjects,
        loading: false,
        error: null,
        connected: true,
        refreshProjects: async () => {},
      });
    });

    const before = (a: Element, b: Element) => Boolean(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);

    it('lists Work, then Boards with the Dashboard under it, and no Workspace group', async () => {
      vi.spyOn(api, 'listPlaybooks').mockResolvedValue([playbook('pb-1')]);
      renderSidebar();
      const playbooks = await screen.findByRole('region', { name: 'Playbooks' });
      const work = screen.getByText('Work');
      const boards = screen.getByText('Boards');
      const dashboard = screen.getByRole('link', { name: 'Dashboard' });
      const firstProject = screen.getByRole('link', { name: 'alpha' });
      expect(before(work, playbooks)).toBe(true);
      expect(before(playbooks, boards)).toBe(true);
      expect(before(boards, dashboard)).toBe(true);
      expect(before(dashboard, firstProject)).toBe(true);
      expect(screen.queryByText('Workspace')).not.toBeInTheDocument();
    });

    it('never jumps straight to a lone playbook from the Playbooks section', async () => {
      vi.spyOn(api, 'listPlaybooks').mockResolvedValue([playbook('pb-1')]);
      renderSidebar();
      const section = await screen.findByRole('region', { name: 'Playbooks' });
      await within(section).findByText('pb-1');
      expect(within(section).getByRole('link', { name: /All playbooks/ })).toHaveAttribute('href', '/playbooks');
    });
  });

  describe('mobile overlay', () => {
    beforeEach(() => {
      mockUseProjects.mockReturnValue({
        projects: defaultProjects,
        loading: false,
        error: null,
        connected: true,
        refreshProjects: async () => {},
      });
    });

    it('does not render overlay backdrop when mobileOpen is false', () => {
      renderSidebar({ mobileOpen: false });
      // The backdrop has aria-hidden="true" and no role - check it's absent
      const backdrop = document.querySelector('div[aria-hidden="true"]');
      expect(backdrop).toBeNull();
    });

    it('renders overlay backdrop and close button when mobileOpen is true', () => {
      renderSidebar({ mobileOpen: true, onMobileClose: vi.fn() });

      const backdrop = document.querySelector('div[aria-hidden="true"]');
      expect(backdrop).not.toBeNull();

      const closeBtn = screen.getByTitle('Close sidebar');
      expect(closeBtn).toBeTruthy();
    });

    it('calls onMobileClose when backdrop is clicked', () => {
      const onMobileClose = vi.fn();
      renderSidebar({ mobileOpen: true, onMobileClose });

      const backdrop = document.querySelector('div[aria-hidden="true"]') as HTMLElement;
      fireEvent.click(backdrop);

      expect(onMobileClose).toHaveBeenCalledTimes(1);
    });

    it('calls onMobileClose when close button is clicked', () => {
      const onMobileClose = vi.fn();
      renderSidebar({ mobileOpen: true, onMobileClose });

      const closeBtn = screen.getByTitle('Close sidebar');
      fireEvent.click(closeBtn);

      expect(onMobileClose).toHaveBeenCalledTimes(1);
    });

    it('calls onMobileClose when a project nav link is clicked', () => {
      const onMobileClose = vi.fn();
      renderSidebar({ mobileOpen: true, onMobileClose });

      const projectLinks = screen.getAllByRole('link').filter(
        (link) => link.getAttribute('href')?.startsWith('/projects/')
      );
      expect(projectLinks.length).toBeGreaterThan(0);
      fireEvent.click(projectLinks[0]);

      expect(onMobileClose).toHaveBeenCalledTimes(1);
    });

    it('calls onMobileClose when the Dashboard nav link is clicked', () => {
      const onMobileClose = vi.fn();
      renderSidebar({ mobileOpen: true, onMobileClose });

      const allLink = screen.getAllByRole('link').find(
        (link) => link.getAttribute('href') === '/all'
      );
      expect(allLink).toBeTruthy();
      fireEvent.click(allLink!);

      expect(onMobileClose).toHaveBeenCalledTimes(1);
    });

  });
});

describe('Sidebar footer appearance slot', () => {
  beforeEach(() => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([]);
    mockUseProjects.mockReturnValue({ projects: defaultProjects, loading: false, error: null, connected: true, refreshProjects: async () => {} });
  });
  afterEach(() => {
    vi.restoreAllMocks();
    authState.current = null;
  });

  it('none mode: shows a Settings chip with the admin pages and appearance, and no user chip', () => {
    setAuthState({ mode: 'none', status: 'authenticated', user: null, version: null, setUser: vi.fn(), logout: vi.fn() });
    renderSidebar();
    const chip = screen.getByRole('button', { name: /settings/i });
    fireEvent.click(chip);
    expect(screen.getByRole('button', { name: 'Model selection' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Chats' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Dark' })).toBeInTheDocument();
    expect(screen.queryByTitle(/^signed in as/i)).toBeNull();
  });

  it('no AuthProvider at all behaves like none mode', () => {
    authState.current = null;
    renderSidebar();
    expect(screen.getByRole('button', { name: /settings/i })).toBeInTheDocument();
  });

  it('multi mode: the user chip owns appearance; no standalone Appearance chip', () => {
    setAuthState({
      mode: 'multi',
      status: 'authenticated',
      user: { username: 'alice', display_name: 'Alice', is_admin: false },
      version: null,
      setUser: vi.fn(),
      logout: vi.fn(),
    });
    renderSidebar();
    expect(screen.queryByRole('button', { name: /^appearance$/i })).toBeNull();
    expect(screen.getByTitle(/^signed in as alice/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /Alice/ }));
    expect(screen.getByRole('button', { name: 'Dark' })).toBeInTheDocument();
  });
});

describe('boards repo sections', () => {
  const projectsByRepo = [
    { name: 'zebra', prefix: 'Z', next_id: 1, states: [], types: [], priorities: [], transitions: {}, boards_repo: 'private' },
    { name: 'alpha', prefix: 'A', next_id: 1, states: [], types: [], priorities: [], transitions: {}, boards_repo: 'team' },
    { name: 'ghost', prefix: 'G', next_id: 1, states: [], types: [], priorities: [], transitions: {}, boards_repo: 'ghost' },
  ];

  beforeEach(() => {
    localStorage.clear();
    themeState.boardsRepos = [{ name: 'team', shared: true }, { name: 'private', shared: false }];
    syncState.current = [
      { repo: 'team', enabled: true, shared: true, syncing: false, last_sync_time: null, remote_reachable: false },
      { repo: 'private', enabled: false, syncing: false, last_sync_time: null },
    ];
    mockUseProjects.mockReturnValue({ projects: projectsByRepo, loading: false, error: null, connected: true, refreshProjects: async () => {} });
  });

  afterEach(() => {
    themeState.boardsRepos = [];
    syncState.current = [];
  });

  it('renders one section per repo holding its projects, with a sync dot', () => {
    renderSidebar();
    const team = screen.getByRole('region', { name: 'Boards repo team' });
    const priv = screen.getByRole('region', { name: 'Boards repo private' });
    expect(within(team).getByRole('link', { name: /alpha/ })).toBeInTheDocument();
    expect(within(team).queryByRole('link', { name: /zebra/ })).toBeNull();
    expect(within(priv).getByRole('link', { name: /zebra/ })).toBeInTheDocument();
    expect(within(team).getByRole('img', { name: /team sync: offline/ })).toBeInTheDocument();
    expect(within(priv).getByRole('img', { name: /private sync: sync disabled/ })).toBeInTheDocument();
    expect(screen.queryByText('Projects')).toBeNull();
  });

  it('collapses a section and remembers it across renders', () => {
    const first = renderSidebar();
    fireEvent.click(screen.getByRole('button', { name: /team/ }));
    expect(screen.queryByRole('link', { name: /alpha/ })).toBeNull();
    expect(localStorage.getItem('contextmatrix-sidebar-repo-collapsed')).toContain('"team":true');
    first.unmount();

    renderSidebar();
    expect(screen.queryByRole('link', { name: /alpha/ })).toBeNull();
    expect(screen.getByRole('link', { name: /zebra/ })).toBeInTheDocument();
  });

  it('keeps the single Projects eyebrow with one repo', () => {
    themeState.boardsRepos = [{ name: 'boards', shared: false }];
    renderSidebar();
    expect(screen.getByText('Projects')).toBeInTheDocument();
    expect(screen.queryByRole('region', { name: /Boards repo/ })).toBeNull();
  });

  it('renders a project with an unknown boards_repo under the first repo', () => {
    renderSidebar();
    const team = screen.getByRole('region', { name: 'Boards repo team' });
    const priv = screen.getByRole('region', { name: 'Boards repo private' });
    expect(within(team).getByRole('link', { name: /ghost/ })).toBeInTheDocument();
    expect(within(priv).queryByRole('link', { name: /ghost/ })).toBeNull();
  });
});

describe('Sidebar LLM channel', () => {
  beforeEach(() => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([]);
    vi.spyOn(api, 'getLLMChannel').mockResolvedValue(channel(true));
    mockUseProjects.mockReturnValue({ projects: defaultProjects, loading: false, error: null, connected: true, refreshProjects: async () => {} });
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('puts the strip under the wordmark and a dot on the collapsed rail', async () => {
    renderSidebar();
    const strip = await screen.findByRole('button', { name: /LLM channel: Claude Code/ });
    const follows = (a: Node, b: Node) => Boolean(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
    expect(follows(screen.getByRole('heading', { level: 1 }), strip)).toBe(true);
    expect(follows(strip, screen.getByRole('navigation', { name: 'Projects' }))).toBe(true);

    fireEvent.click(screen.getByTitle('Collapse sidebar'));
    expect(await screen.findByRole('img', { name: 'LLM channel: Claude Code' })).toBeInTheDocument();
  });

  it('closes only the popover on Escape inside the mobile drawer', async () => {
    const onMobileClose = vi.fn();
    renderSidebar({ mobileOpen: true, onMobileClose });
    const drawer = screen.getByRole('dialog', { name: 'Sidebar navigation' });
    const strip = await within(drawer).findByRole('button', { name: /LLM channel: Claude Code/ });
    fireEvent.click(strip);
    expect(within(drawer).getByRole('group', { name: 'LLM channel' })).toBeInTheDocument();

    fireEvent.keyDown(strip, { key: 'Escape' });
    expect(within(drawer).queryByRole('group', { name: 'LLM channel' })).toBeNull();
    expect(onMobileClose).not.toHaveBeenCalled();

    fireEvent.keyDown(strip, { key: 'Escape' });
    expect(onMobileClose).toHaveBeenCalledTimes(1);
  });
});
