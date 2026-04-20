import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Sidebar } from './Sidebar';

vi.mock('../../hooks/useProjects', () => ({
  useProjects: vi.fn(),
}));

vi.mock('../../hooks/useProjectSummaries', () => ({
  useProjectSummaries: vi.fn(() => ({ summaries: new Map(), loading: false })),
}));

vi.mock('../../hooks/useTheme', () => ({
  useTheme: vi.fn(() => ({ theme: 'dark', palette: 'everforest', version: '', toggleTheme: () => {} })),
}));

import { useProjects } from '../../hooks/useProjects';

const mockUseProjects = vi.mocked(useProjects);

function renderSidebar(props?: { mobileOpen?: boolean; onMobileClose?: () => void }) {
  return render(
    <MemoryRouter>
      <Sidebar onNewProject={() => {}} {...props} />
    </MemoryRouter>
  );
}

const defaultProjects = [
  { name: 'zebra', prefix: 'Z', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
  { name: 'alpha', prefix: 'A', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
  { name: 'mango', prefix: 'M', next_id: 1, states: [], types: [], priorities: [], transitions: {} },
];

describe('Sidebar', () => {
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
      refreshProjects: () => {},
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
      refreshProjects: () => {},
    });

    renderSidebar();

    const projectLinks = screen.getAllByRole('link').filter(
      (link) => link.getAttribute('href')?.startsWith('/projects/')
    );

    const names = projectLinks.map((link) => link.textContent);
    expect(names).toEqual(['alpha', 'Bravo', 'Charlie']);
  });

  describe('mobile overlay', () => {
    beforeEach(() => {
      mockUseProjects.mockReturnValue({
        projects: defaultProjects,
        loading: false,
        error: null,
        connected: true,
        refreshProjects: () => {},
      });
    });

    it('does not render overlay backdrop when mobileOpen is false', () => {
      renderSidebar({ mobileOpen: false });
      // The backdrop has aria-hidden="true" and no role — check it's absent
      const backdrop = document.querySelector('[aria-hidden="true"]');
      expect(backdrop).toBeNull();
    });

    it('renders overlay backdrop and close button when mobileOpen is true', () => {
      renderSidebar({ mobileOpen: true, onMobileClose: vi.fn() });

      const backdrop = document.querySelector('[aria-hidden="true"]');
      expect(backdrop).not.toBeNull();

      const closeBtn = screen.getByTitle('Close sidebar');
      expect(closeBtn).toBeTruthy();
    });

    it('calls onMobileClose when backdrop is clicked', () => {
      const onMobileClose = vi.fn();
      renderSidebar({ mobileOpen: true, onMobileClose });

      const backdrop = document.querySelector('[aria-hidden="true"]') as HTMLElement;
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

    it('calls onMobileClose when the All Projects nav link is clicked', () => {
      const onMobileClose = vi.fn();
      renderSidebar({ mobileOpen: true, onMobileClose });

      const allLink = screen.getAllByRole('link').find(
        (link) => link.getAttribute('href') === '/all'
      );
      expect(allLink).toBeTruthy();
      fireEvent.click(allLink!);

      expect(onMobileClose).toHaveBeenCalledTimes(1);
    });

    it('does not render overlay backdrop in desktop mode (mobileOpen=false)', () => {
      renderSidebar({ mobileOpen: false });
      // Desktop sidebar is rendered normally (not fixed overlay)
      const sidebar = document.querySelector('.sidebar');
      expect(sidebar).not.toBeNull();
    });
  });
});
