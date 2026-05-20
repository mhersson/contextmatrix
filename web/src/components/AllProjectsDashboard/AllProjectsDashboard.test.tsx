import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, waitFor, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { AllProjectsDashboard } from './AllProjectsDashboard';
import { ToastContext, useToastState } from '../../hooks/useToast';
import { SSEProvider } from '../../hooks/useSSEBus';
import { ProjectsProvider } from '../../hooks/useProjects';
import { MobileSidebarProvider } from '../../context/MobileSidebarContext';
import { api } from '../../api/client';
import type { ReactNode } from 'react';

vi.mock('../../api/client', () => ({
  api: {
    getAppConfig: vi.fn(),
    getSyncStatus: vi.fn(),
    getProjects: vi.fn(),
    getDashboard: vi.fn(),
  },
  isAPIError: () => false,
}));

class FakeEventSource {
  url: string;
  readyState = 0;
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  constructor(url: string) {
    this.url = url;
    setTimeout(() => {
      this.readyState = 1;
      this.onopen?.(new Event('open'));
    }, 0);
  }
  close() {
    this.readyState = 2;
  }
  addEventListener() {}
  removeEventListener() {}
  dispatchEvent() {
    return true;
  }
}

function ToastWrap({ children }: { children: ReactNode }) {
  const toastState = useToastState();
  return <ToastContext.Provider value={toastState}>{children}</ToastContext.Provider>;
}

describe('AllProjectsDashboard — mount fetch count', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    // @ts-expect-error stub for tests
    globalThis.EventSource = FakeEventSource;
    vi.mocked(api.getAppConfig).mockResolvedValue({ theme: 'everforest', version: 'v1.0.0' });
    vi.mocked(api.getSyncStatus).mockResolvedValue({
      enabled: false,
      syncing: false,
      last_sync_time: null,
    });
    vi.mocked(api.getProjects).mockResolvedValue([]);
    vi.mocked(api.getDashboard).mockResolvedValue({
      state_counts: {},
      state_counts_parents: {},
      active_agents: [],
      total_cost_usd: 0,
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
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it('fetches app config and sync status exactly once on mount (no render loop)', async () => {
    render(
      <MemoryRouter>
        <ToastWrap>
          <MobileSidebarProvider>
            <SSEProvider>
              <ProjectsProvider>
                <AllProjectsDashboard />
              </ProjectsProvider>
            </SSEProvider>
          </MobileSidebarProvider>
        </ToastWrap>
      </MemoryRouter>,
    );

    // Let effects + promise resolution settle.
    await waitFor(() => {
      expect(vi.mocked(api.getAppConfig)).toHaveBeenCalled();
      expect(vi.mocked(api.getSyncStatus)).toHaveBeenCalled();
    });

    // Allow several render-flush cycles to pass.
    for (let i = 0; i < 5; i++) {
      await act(async () => {
        await Promise.resolve();
      });
    }

    expect(vi.mocked(api.getAppConfig)).toHaveBeenCalledTimes(1);
    expect(vi.mocked(api.getSyncStatus)).toHaveBeenCalledTimes(1);
  });
});
