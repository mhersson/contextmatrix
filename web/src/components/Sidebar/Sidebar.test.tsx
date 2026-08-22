import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { Sidebar } from './Sidebar';
import { api } from '../../api/client';
import type { PlaybookSummary } from '../../types';

vi.mock('../../hooks/useProjects', () => ({
  useProjects: vi.fn(),
}));

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({
    subscribe: () => () => {},
    connected: true,
    error: null,
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

vi.mock('../../hooks/useTheme', () => ({
  useTheme: vi.fn(() => ({ theme: 'dark', palette: 'everforest', version: '', setTheme: () => {}, setPalette: () => {} })),
}));

const authState = vi.hoisted(() => ({ current: null as unknown }));
vi.mock('../../hooks/useAuth', () => ({
  useOptionalAuth: () => authState.current,
}));

vi.mock('./ChatSection', () => ({
  ChatSection: () => null,
}));

import { useProjects } from '../../hooks/useProjects';

const mockUseProjects = vi.mocked(useProjects);

function renderSidebar(props?: { mobileOpen?: boolean; onMobileClose?: () => void }) {
  return render(
    <MemoryRouter>
      <Sidebar onNewProject={() => {}} onNewChat={() => {}} {...props} />
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

  describe('playbooks shortcut', () => {
    beforeEach(() => {
      mockUseProjects.mockReturnValue({
        projects: defaultProjects,
        loading: false,
        error: null,
        connected: true,
        refreshProjects: async () => {},
      });
    });

    const playbooksLink = () =>
      screen.getAllByRole('link').find((link) => link.getAttribute('href')?.startsWith('/playbooks'));

    it('links directly to the single playbook when exactly one exists', async () => {
      vi.spyOn(api, 'listPlaybooks').mockResolvedValue([playbook('pb-1')]);
      renderSidebar();
      await waitFor(() => {
        expect(playbooksLink()?.getAttribute('href')).toBe('/playbooks/pb-1');
      });
    });

    it('links to the list when multiple playbooks exist', async () => {
      vi.spyOn(api, 'listPlaybooks').mockResolvedValue([playbook('pb-1'), playbook('pb-2')]);
      renderSidebar();
      await waitFor(() => {
        expect(api.listPlaybooks).toHaveBeenCalled();
      });
      expect(playbooksLink()?.getAttribute('href')).toBe('/playbooks');
    });

    it('links to the list when the playbook fetch fails', async () => {
      vi.spyOn(api, 'listPlaybooks').mockRejectedValue(new Error('boom'));
      renderSidebar();
      await waitFor(() => {
        expect(api.listPlaybooks).toHaveBeenCalled();
      });
      expect(playbooksLink()?.getAttribute('href')).toBe('/playbooks');
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
    mockUseProjects.mockReturnValue({ projects: defaultProjects, loading: false, error: null, connected: true, refreshProjects: async () => {} });
  });
  afterEach(() => {
    authState.current = null;
  });

  it('none mode: shows a standalone Appearance chip and no user chip', () => {
    authState.current = null;
    renderSidebar();
    const chip = screen.getByRole('button', { name: /appearance/i });
    fireEvent.click(chip);
    expect(screen.getByRole('menuitemradio', { name: 'Dark' })).toBeInTheDocument();
    expect(screen.queryByText(/signed in as/i)).toBeNull();
  });

  it('multi mode: the user chip owns appearance; no standalone Appearance chip', () => {
    authState.current = {
      mode: 'multi',
      user: { username: 'alice', display_name: 'Alice', is_admin: false },
      logout: vi.fn(),
    };
    renderSidebar();
    expect(screen.queryByRole('button', { name: /^appearance$/i })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /Alice/ }));
    expect(screen.getByRole('menuitemradio', { name: 'Dark' })).toBeInTheDocument();
  });
});
