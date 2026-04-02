import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Sidebar } from './Sidebar';

vi.mock('../../hooks/useProjects', () => ({
  useProjects: vi.fn(),
}));

vi.mock('../../hooks/useProjectSummaries', () => ({
  useProjectSummaries: vi.fn(() => ({ summaries: new Map(), loading: false })),
}));

import { useProjects } from '../../hooks/useProjects';

const mockUseProjects = vi.mocked(useProjects);

function renderSidebar() {
  return render(
    <MemoryRouter>
      <Sidebar onNewProject={() => {}} />
    </MemoryRouter>
  );
}

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
});
