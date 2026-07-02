import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { ProjectSummariesProvider, useProjectSummariesContext } from './ProjectSummariesProvider';
import { useProjects } from './useProjects';
import { api } from '../api/client';
import type { DashboardData, ProjectConfig } from '../types';

vi.mock('../api/client', () => ({
  api: {
    getDashboard: vi.fn(),
  },
}));

vi.mock('./useProjects', () => ({
  useProjects: vi.fn(),
}));

vi.mock('./useSSEBus', () => ({
  useSSEBus: vi.fn(() => ({
    subscribe: vi.fn(() => () => {}),
    connected: true,
    error: null,
  })),
}));

const mockUseProjects = vi.mocked(useProjects);

function makeProject(name: string): ProjectConfig {
  return { name, prefix: name.toUpperCase(), next_id: 1, states: [], types: [], priorities: [], transitions: {} };
}

const dashboardStub: DashboardData = {
  state_counts: {},
  state_counts_parents: {},
  active_agents: [],
  total_cost_usd: 0,
  total_cost_usd_last_30d: 0,
  total_cost_usd_prior_30d: 0,
  cost_series_30d: Array(30).fill(0),
  cards_completed_today: 0,
  cards_completed_today_parents: 0,
  cards_completed_last_7d: 0,
  cards_completed_last_7d_parents: 0,
  cards_completed_prior_7d: 0,
  cards_completed_prior_7d_parents: 0,
  metric_series: {
    active_agents: [],
    in_flight: [],
    stalled: [],
    shipped: [],
    in_flight_parents: [],
    stalled_parents: [],
    shipped_parents: [],
  },
  agent_costs: [],
  card_costs: [],
  model_costs: [],
};

// Two independent consumers of the context, mirroring Sidebar + AllProjectsDashboard
// both mounting under the same provider.
function ConsumerA() {
  const { summaries } = useProjectSummariesContext();
  return <div data-testid="consumer-a">{summaries.size}</div>;
}

function ConsumerB() {
  const { errors, loading, refresh } = useProjectSummariesContext();
  return (
    <div data-testid="consumer-b">
      {loading ? 'loading' : 'loaded'}-{errors.size}-{typeof refresh}
    </div>
  );
}

describe('ProjectSummariesProvider', () => {
  const projectNames = ['alpha', 'beta', 'gamma'];

  beforeEach(() => {
    vi.mocked(api.getDashboard).mockResolvedValue(dashboardStub);
    mockUseProjects.mockReturnValue({
      projects: projectNames.map(makeProject),
      loading: false,
      error: null,
      connected: true,
      refreshProjects: async () => {},
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('fetches each project dashboard exactly once for two mounted consumers', async () => {
    render(
      <ProjectSummariesProvider>
        <ConsumerA />
        <ConsumerB />
      </ProjectSummariesProvider>,
    );

    await waitFor(() => {
      expect(vi.mocked(api.getDashboard)).toHaveBeenCalledTimes(projectNames.length);
    });

    for (const name of projectNames) {
      expect(vi.mocked(api.getDashboard)).toHaveBeenCalledWith(name);
    }
  });

  it('throws when used outside a ProjectSummariesProvider', () => {
    // Swallow the expected React error-boundary console noise.
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => render(<ConsumerA />)).toThrow(
      'useProjectSummariesContext must be used within ProjectSummariesProvider',
    );
    spy.mockRestore();
  });
});
