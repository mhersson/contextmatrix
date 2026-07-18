import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { createElement } from 'react';
import type { ReactNode } from 'react';
import { useBoard } from './useBoard';
import { SSEProvider } from './useSSEBus';
import { api } from '../api/client';
import type { Card, ProjectConfig } from '../types';

// useSSEBus (exercised here via the real SSEProvider, see `wrapper` below)
// imports SESSION_EXPIRED_EVENT from this module, so the mock factory must
// preserve real exports via importOriginal rather than replacing the module
// wholesale - otherwise the named import resolves to nothing and importing
// useSSEBus throws.
vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>();
  return {
    ...actual,
    api: {
      getProject: vi.fn(),
      getCards: vi.fn(),
      getCard: vi.fn(),
    },
  };
});

// ---- EventSource mock (mirrors useSSEBus.test.tsx) ---------------------------
//
// useBoard resyncs via the real SSEProvider (not a mocked useSSEBus), so a
// real EventSource must exist for the provider to construct against - same
// file-local mock class + globalThis wiring as useSSEBus.test.tsx.

interface MockES {
  url: string;
  readyState: number;
  onopen: ((ev: Event) => void) | null;
  onmessage: ((ev: MessageEvent) => void) | null;
  onerror: ((ev: Event) => void) | null;
  close: () => void;
  _triggerOpen: () => void;
  _triggerError: () => void;
  _closed: boolean;
}

let instances: MockES[] = [];

class MockEventSource implements MockES {
  // Mirrors the real EventSource readyState constants (spec values 0/1/2).
  // useSSEBus's onerror reads es.readyState === EventSource.CLOSED to
  // detect a dead session; this suite only ever models a transient network
  // outage (readyState stays CONNECTING), never a session-death close.
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;

  url: string;
  readyState: number = MockEventSource.CONNECTING;
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  _closed = false;

  constructor(url: string) {
    this.url = url;
    instances.push(this);
  }

  close() {
    this._closed = true;
  }

  _triggerOpen() {
    this.readyState = MockEventSource.OPEN;
    this.onopen?.(new Event('open'));
  }

  _triggerError() {
    this.onerror?.(new Event('error'));
  }
}

Object.defineProperty(globalThis, 'EventSource', {
  value: MockEventSource,
  writable: true,
  configurable: true,
});

function latestInstance(): MockES {
  if (instances.length === 0) throw new Error('No EventSource instances');
  return instances[instances.length - 1];
}

function wrapper({ children }: { children: ReactNode }) {
  return createElement(SSEProvider, null, children);
}

const projectConfig: ProjectConfig = {
  name: 'alpha',
  prefix: 'ALPHA',
  next_id: 1,
  states: [],
  types: [],
  priorities: [],
  transitions: {},
};

const cards: Card[] = [];

describe('useBoard - SSE reconnect resync', () => {
  beforeEach(() => {
    instances = [];
    // shouldAdvanceTime lets testing-library's waitFor polling proceed on
    // real wall-clock ticks while vi.advanceTimersByTime still drives the
    // SSE reconnect backoff deterministically (mirrors
    // AllProjectsDashboard.test.tsx's mount-fetch-count suite).
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(api.getProject).mockResolvedValue(projectConfig);
    vi.mocked(api.getCards).mockResolvedValue(cards);
    vi.mocked(api.getCard).mockRejectedValue(new Error('not used in this test'));
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it('fetches cards exactly once after mount and the initial SSE open', async () => {
    renderHook(() => useBoard('alpha'), { wrapper });

    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(1));

    // Initial connect must not trigger a resync fetch - reconnectEpoch stays
    // 0 through the first open (hasConnectedOnceRef guard in useSSEBus).
    act(() => {
      latestInstance()._triggerOpen();
    });

    expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(1);
  });

  it('refetches cards after an SSE outage and reconnect', async () => {
    renderHook(() => useBoard('alpha'), { wrapper });

    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(1));

    act(() => {
      latestInstance()._triggerOpen();
    });
    expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(1);

    // Outage: error → backoff → reconnect. The reconnect's onopen bumps
    // reconnectEpoch (this is a true reconnect, not the initial connect),
    // and useBoard must resync by calling fetchData again.
    act(() => {
      latestInstance()._triggerError();
    });
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    act(() => {
      latestInstance()._triggerOpen();
    });

    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(2));
  });
});

describe('useBoard - refreshCard (panel-open hydration)', () => {
  beforeEach(() => {
    instances = [];
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it('replaces the selected card in state with the single-card GET result', async () => {
    // Mirrors what ProjectShell does on panel open: the list endpoint omits
    // subtask_cost_usd (a single-card-GET-only field), so opening the panel
    // must fetch and merge the enriched card by id.
    const listCard: Card = {
      id: 'ALPHA-1',
      title: 'Parent card',
      project: 'alpha',
      type: 'task',
      state: 'done',
      priority: 'medium',
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      body: '',
    };
    const enrichedCard: Card = { ...listCard, subtask_cost_usd: 12.34 };

    vi.mocked(api.getProject).mockResolvedValue(projectConfig);
    vi.mocked(api.getCards).mockResolvedValue([listCard]);
    vi.mocked(api.getCard).mockResolvedValue(enrichedCard);

    const { result } = renderHook(() => useBoard('alpha'), { wrapper });

    await waitFor(() => expect(result.current.cards).toHaveLength(1));
    expect(result.current.cards[0].subtask_cost_usd).toBeUndefined();

    await act(async () => {
      await result.current.refreshCard('ALPHA-1');
    });

    expect(api.getCard).toHaveBeenCalledWith('alpha', 'ALPHA-1');
    expect(result.current.cards).toHaveLength(1);
    expect(result.current.cards[0].subtask_cost_usd).toBe(12.34);
  });

  it('logs and leaves state unchanged when the single-card GET fails', async () => {
    const listCard: Card = {
      id: 'ALPHA-1',
      title: 'Parent card',
      project: 'alpha',
      type: 'task',
      state: 'done',
      priority: 'medium',
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      body: '',
    };

    vi.mocked(api.getProject).mockResolvedValue(projectConfig);
    vi.mocked(api.getCards).mockResolvedValue([listCard]);
    vi.mocked(api.getCard).mockRejectedValue(new Error('boom'));
    const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    const { result } = renderHook(() => useBoard('alpha'), { wrapper });

    await waitFor(() => expect(result.current.cards).toHaveLength(1));

    await act(async () => {
      await result.current.refreshCard('ALPHA-1');
    });

    expect(result.current.cards).toEqual([listCard]);
    expect(consoleErrorSpy).toHaveBeenCalledWith(
      'Failed to refresh card:',
      'ALPHA-1',
      expect.any(Error)
    );
    consoleErrorSpy.mockRestore();
  });

  it('skips hydration while a patch is in flight for the card', async () => {
    const listCard: Card = {
      id: 'ALPHA-1',
      title: 'Parent card',
      project: 'alpha',
      type: 'task',
      state: 'done',
      priority: 'medium',
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      body: '',
    };

    vi.mocked(api.getProject).mockResolvedValue(projectConfig);
    vi.mocked(api.getCards).mockResolvedValue([listCard]);
    vi.mocked(api.getCard).mockResolvedValue({ ...listCard, subtask_cost_usd: 1 });

    const { result } = renderHook(() => useBoard('alpha'), { wrapper });

    await waitFor(() => expect(result.current.cards).toHaveLength(1));

    // While a patchCard is in flight (suppressSSE), hydration must not fetch:
    // merging the pre-patch server snapshot would revert the optimistic update.
    act(() => {
      result.current.suppressSSE('ALPHA-1');
    });
    await act(async () => {
      await result.current.refreshCard('ALPHA-1');
    });
    expect(api.getCard).not.toHaveBeenCalled();

    act(() => {
      result.current.unsuppressSSE('ALPHA-1');
    });
    await act(async () => {
      await result.current.refreshCard('ALPHA-1');
    });
    expect(api.getCard).toHaveBeenCalledWith('alpha', 'ALPHA-1');
  });

  it('bumps listEpoch on every wholesale list replace', async () => {
    vi.mocked(api.getProject).mockResolvedValue(projectConfig);
    vi.mocked(api.getCards).mockResolvedValue([]);
    vi.mocked(api.getCard).mockRejectedValue(new Error('not used in this test'));

    const { result } = renderHook(() => useBoard('alpha'), { wrapper });

    // Initial load is the first wholesale replace.
    await waitFor(() => expect(result.current.listEpoch).toBe(1));

    // refresh() reruns fetchData - e.g. what a sync.completed pull triggers.
    await act(async () => {
      await result.current.refresh();
    });
    expect(result.current.listEpoch).toBe(2);
  });
});
