import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { ProjectsProvider, useProjects } from './useProjects';
import { api } from '../api/client';
import type { ProjectConfig } from '../types';

vi.mock('../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api/client')>()),
  api: { getProjects: vi.fn() },
}));

vi.mock('./useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: vi.fn(() => () => {}), connected: true }),
}));

const boardsRepos = vi.hoisted(() => ({ value: [] as { name: string; shared: boolean }[] }));
vi.mock('./useTheme', () => ({
  useTheme: () => ({ boardsRepos: boardsRepos.value }),
}));

function p(name: string, boards_repo?: string): ProjectConfig {
  return { name, prefix: name.toUpperCase(), next_id: 1, states: [], types: [], priorities: [], transitions: {}, boards_repo };
}

function Names() {
  const { projects } = useProjects();
  return <div data-testid="names">{projects.map((x) => x.name).join(',')}</div>;
}

describe('ProjectsProvider', () => {
  beforeEach(() => {
    boardsRepos.value = [];
    vi.mocked(api.getProjects).mockReset();
  });

  it('exposes projects in sidebar order regardless of API order', async () => {
    vi.mocked(api.getProjects).mockResolvedValue([p('zeta'), p('alpha'), p('mid')]);
    render(<ProjectsProvider><Names /></ProjectsProvider>);
    await waitFor(() => expect(screen.getByTestId('names')).toHaveTextContent('alpha,mid,zeta'));
  });

  it('orders by repo group first when several boards repos exist', async () => {
    boardsRepos.value = [{ name: 'work', shared: false }, { name: 'home', shared: false }];
    vi.mocked(api.getProjects).mockResolvedValue([p('a', 'home'), p('z', 'work')]);
    render(<ProjectsProvider><Names /></ProjectsProvider>);
    await waitFor(() => expect(screen.getByTestId('names')).toHaveTextContent('z,a'));
  });
});
