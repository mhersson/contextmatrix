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

describe('useBoard - dependency state changes', () => {
  beforeEach(() => {
    instances = [];
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(api.getProject).mockResolvedValue(projectConfig);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  const base: Card = {
    id: 'ALPHA-1',
    title: 'Dependency',
    project: 'alpha',
    type: 'task',
    state: 'review',
    priority: 'medium',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
  };
  const dependent: Card = {
    ...base,
    id: 'ALPHA-2',
    title: 'Dependent',
    state: 'todo',
    depends_on: ['ALPHA-1'],
    blocked_by: ['ALPHA-1'],
  };
  const bystander: Card = { ...base, id: 'ALPHA-3', title: 'Bystander', state: 'todo' };

  async function mount() {
    vi.mocked(api.getCards).mockResolvedValueOnce([base, dependent, bystander]);
    const { result } = renderHook(() => useBoard('alpha'), { wrapper });
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => {
      latestInstance()._triggerOpen();
    });
    return result;
  }

  function stateChanged(oldState: string, newState: string) {
    act(() => {
      latestInstance().onmessage?.({
        data: JSON.stringify({
          type: 'card.state_changed',
          card_id: 'ALPHA-1',
          project: 'alpha',
          data: { old_state: oldState, new_state: newState },
        }),
      } as MessageEvent);
    });
  }

  it('refetches dependents when a dependency reaches done so blocked_by stays true', async () => {
    const result = await mount();
    vi.mocked(api.getCard).mockImplementation(async (_project, id) => {
      if (id === 'ALPHA-1') return { ...base, state: 'done' };
      if (id === 'ALPHA-2') return { ...dependent, dependencies_met: true, blocked_by: undefined };
      throw new Error(`unexpected refetch of ${id}`);
    });

    stateChanged('review', 'done');

    await waitFor(() =>
      expect(result.current.cards.find((c) => c.id === 'ALPHA-2')?.dependencies_met).toBe(true),
    );
    expect(result.current.cards.find((c) => c.id === 'ALPHA-2')?.blocked_by).toBeUndefined();
    const ids = vi.mocked(api.getCard).mock.calls.map(([, id]) => id).sort();
    expect(ids).toEqual(['ALPHA-1', 'ALPHA-2']);
  });

  it('leaves dependents alone when the transition does not cross done', async () => {
    await mount();
    vi.mocked(api.getCard).mockImplementation(async (_project, id) => {
      if (id === 'ALPHA-1') return { ...base, state: 'in_progress' };
      throw new Error(`unexpected refetch of ${id}`);
    });

    stateChanged('todo', 'in_progress');

    await waitFor(() => expect(vi.mocked(api.getCard)).toHaveBeenCalledTimes(1));
    expect(vi.mocked(api.getCard).mock.calls.map(([, id]) => id)).toEqual(['ALPHA-1']);
  });
});

describe('useBoard - playbook events', () => {
  beforeEach(() => {
    instances = [];
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(api.getProject).mockResolvedValue(projectConfig);
    vi.mocked(api.getCards).mockResolvedValue(cards);
    vi.mocked(api.getCard).mockRejectedValue(new Error('not used in this test'));
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it('refreshes the cards on a playbook change without showing the loading skeleton', async () => {
    const listCard: Card = {
      id: 'ALPHA-1',
      title: 'Alpha card',
      project: 'alpha',
      type: 'task',
      state: 'todo',
      priority: 'medium',
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      body: '',
    };
    vi.mocked(api.getCards).mockResolvedValueOnce([listCard]);

    const { result } = renderHook(() => useBoard('alpha'), { wrapper });
    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(result.current.loading).toBe(false));

    const refreshed = [{ ...listCard, in_playbooks: ['pb-1'] }];
    vi.mocked(api.getCards).mockResolvedValueOnce(refreshed);

    act(() => {
      latestInstance()._triggerOpen();
    });
    act(() => {
      latestInstance().onmessage?.({
        data: JSON.stringify({ type: 'playbook.updated', card_id: '', project: '' }),
      } as MessageEvent);
    });

    // The refetch is in flight; the board must not have flipped to loading.
    expect(result.current.loading).toBe(false);

    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.cards[0]?.in_playbooks).toEqual(['pb-1']));
    expect(result.current.loading).toBe(false);
  });

  it('drops a playbook refresh for the previous project once a project switch has landed', async () => {
    const alphaCard: Card = {
      id: 'ALPHA-1',
      title: 'Alpha card',
      project: 'alpha',
      type: 'task',
      state: 'todo',
      priority: 'medium',
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      body: '',
    };
    const betaCard: Card = {
      id: 'BETA-1',
      title: 'Beta card',
      project: 'beta',
      type: 'task',
      state: 'todo',
      priority: 'medium',
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      body: '',
    };

    vi.mocked(api.getCards).mockResolvedValueOnce([alphaCard]);

    const { result, rerender } = renderHook(
      ({ project }: { project: string }) => useBoard(project),
      { wrapper, initialProps: { project: 'alpha' } },
    );

    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() => {
      latestInstance()._triggerOpen();
    });

    // The playbook refresh for 'alpha' is issued but held unresolved, so it
    // can land after the 'beta' switch below has already completed.
    let resolveAlphaRefresh: (cards: Card[]) => void = () => {};
    const alphaRefreshPromise = new Promise<Card[]>((resolve) => {
      resolveAlphaRefresh = resolve;
    });
    vi.mocked(api.getCards).mockReturnValueOnce(alphaRefreshPromise);

    act(() => {
      latestInstance().onmessage?.({
        data: JSON.stringify({ type: 'playbook.updated', card_id: '', project: '' }),
      } as MessageEvent);
    });
    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(2));

    vi.mocked(api.getCards).mockResolvedValueOnce([betaCard]);
    rerender({ project: 'beta' });

    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(3));
    await waitFor(() => expect(result.current.loading).toBe(false));
    await waitFor(() => expect(result.current.cards).toEqual([betaCard]));

    // The stale alpha refresh finally resolves - it must not clobber beta's
    // already-landed card list.
    await act(async () => {
      resolveAlphaRefresh([{ ...alphaCard, in_playbooks: ['pb-1'] }]);
      await alphaRefreshPromise;
    });

    expect(result.current.cards).toEqual([betaCard]);
    expect(result.current.loading).toBe(false);
  });

  it('keeps the newest snapshot when two playbook refreshes land out of order', async () => {
    const listCard: Card = {
      id: 'ALPHA-1',
      title: 'Alpha card',
      project: 'alpha',
      type: 'task',
      state: 'todo',
      priority: 'medium',
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      body: '',
    };
    vi.mocked(api.getCards).mockResolvedValueOnce([listCard]);

    const { result } = renderHook(() => useBoard('alpha'), { wrapper });
    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(result.current.loading).toBe(false));

    act(() => {
      latestInstance()._triggerOpen();
    });

    // Two playbook events in quick succession issue two overlapping list
    // fetches, both held so their resolution order can be inverted below.
    let resolveOlder: (cards: Card[]) => void = () => {};
    const olderRefresh = new Promise<Card[]>((resolve) => {
      resolveOlder = resolve;
    });
    let resolveNewer: (cards: Card[]) => void = () => {};
    const newerRefresh = new Promise<Card[]>((resolve) => {
      resolveNewer = resolve;
    });
    vi.mocked(api.getCards)
      .mockReturnValueOnce(olderRefresh)
      .mockReturnValueOnce(newerRefresh);

    act(() => {
      latestInstance().onmessage?.({
        data: JSON.stringify({ type: 'playbook.updated', card_id: '', project: '' }),
      } as MessageEvent);
    });
    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(2));

    act(() => {
      latestInstance().onmessage?.({
        data: JSON.stringify({ type: 'playbook.updated', card_id: '', project: '' }),
      } as MessageEvent);
    });
    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(3));

    // The newer request answers first.
    await act(async () => {
      resolveNewer([{ ...listCard, in_playbooks: ['pb-2'] }]);
      await newerRefresh;
    });
    expect(result.current.cards[0]?.in_playbooks).toEqual(['pb-2']);

    // The older request answers last and must be discarded, not applied.
    await act(async () => {
      resolveOlder([{ ...listCard, in_playbooks: ['pb-1'] }]);
      await olderRefresh;
    });

    expect(result.current.cards[0]?.in_playbooks).toEqual(['pb-2']);
  });

  it('preserves a card whose patch is in flight across a playbook refresh', async () => {
    const listCard: Card = {
      id: 'ALPHA-1',
      title: 'Alpha card',
      project: 'alpha',
      type: 'task',
      state: 'todo',
      priority: 'medium',
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      body: '',
    };
    vi.mocked(api.getCards).mockResolvedValueOnce([listCard]);

    const { result } = renderHook(() => useBoard('alpha'), { wrapper });
    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(result.current.loading).toBe(false));

    // A patchCard is in flight for this card: an optimistic local update is
    // applied and its id is suppressed, same as ProjectShell does around a
    // real PATCH call.
    act(() => {
      result.current.suppressSSE('ALPHA-1');
      result.current.updateCardLocally('ALPHA-1', { title: 'Optimistic title' });
    });

    // The playbook refresh's server snapshot predates the in-flight PATCH.
    vi.mocked(api.getCards).mockResolvedValueOnce([{ ...listCard, in_playbooks: ['pb-1'] }]);

    act(() => {
      latestInstance()._triggerOpen();
    });
    act(() => {
      latestInstance().onmessage?.({
        data: JSON.stringify({ type: 'playbook.updated', card_id: '', project: '' }),
      } as MessageEvent);
    });

    await waitFor(() => expect(vi.mocked(api.getCards)).toHaveBeenCalledTimes(2));
    // Wait for the refresh to have replaced the list (initial load bumped the
    // epoch to 1, the silent refresh to 2) - otherwise the assertion below
    // holds whether or not the refresh ever landed.
    await waitFor(() => expect(result.current.listEpoch).toBe(2));
    // The in-flight card is preserved rather than overwritten by the stale
    // refresh's server snapshot.
    expect(result.current.cards[0]?.title).toBe('Optimistic title');
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
