import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useNavigate, useSearchParams } from 'react-router-dom';
import { ProjectShell } from './ProjectShell';
import type { Card, CreateCardInput, ProjectConfig } from '../../types';

// ---------------------------------------------------------------------------
// Hook mocks
// ---------------------------------------------------------------------------

vi.mock('../../hooks/useBoard', () => ({
  useBoard: vi.fn(() => ({
    config: {
      name: 'test',
      prefix: 'TEST',
      next_id: 1,
      states: ['todo', 'done'],
      types: ['task'],
      priorities: ['medium'],
      transitions: { todo: ['done'], done: [] },
      remote_execution: { enabled: false },
    } as ProjectConfig,
    cards: [] as Card[],
    loading: false,
    error: null,
    connected: true,
    refresh: vi.fn(),
    updateCardLocally: vi.fn(),
    removeCardLocally: vi.fn(),
    suppressSSE: vi.fn(),
    unsuppressSSE: vi.fn(),
  })),
}));

vi.mock('../../hooks/useSync', () => ({
  useSync: vi.fn(() => ({
    syncStatus: null,
    triggerSync: vi.fn(),
    handleSyncEvent: vi.fn(),
  })),
}));

vi.mock('../../hooks/useIdentity', () => ({
  useIdentity: vi.fn(() => ({
    identity: 'human:web-test1234',
  })),
}));

const mockHandleCreateCard = vi.fn();

vi.mock('../../hooks/useCardActions', () => ({
  useCardActions: vi.fn(() => ({
    handleCardMove: vi.fn(),
    handleCardSave: vi.fn(),
    handleClaim: vi.fn(),
    handleRelease: vi.fn(),
    handleCreateCard: mockHandleCreateCard,
    handleRunCard: vi.fn(),
    handleStopCard: vi.fn(),
    handleStopAll: vi.fn(),
    handleCardDelete: vi.fn(),
  })),
}));

vi.mock('../../hooks/useKeyboardShortcuts', () => ({
  useKeyboardShortcuts: vi.fn(),
}));

vi.mock('../../hooks/useProjects', () => ({
  useProjects: vi.fn(() => ({
    projects: [{ name: 'test', prefix: 'TEST', next_id: 1, states: [], types: [], priorities: [], transitions: {} }],
    loading: false,
    error: null,
    connected: true,
    refreshProjects: vi.fn(),
  })),
}));

vi.mock('../../hooks/useToast', () => ({
  useToast: vi.fn(() => ({
    showToast: vi.fn(),
    toasts: [],
    dismissToast: vi.fn(),
  })),
}));

vi.mock('../../hooks/useRunnerLogs', () => ({
  useRunnerLogs: vi.fn(() => ({
    logs: [],
    connected: false,
    error: null,
    clear: vi.fn(),
  })),
}));

vi.mock('../../hooks/useResizeDivider', () => ({
  useResizeDivider: vi.fn(() => ({
    boardPercent: 60,
    isDragging: false,
    handleProps: {
      onPointerDown: vi.fn(),
      onPointerMove: vi.fn(),
      onPointerUp: vi.fn(),
      style: {},
    },
  })),
}));

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: vi.fn(() => ({
    subscribe: vi.fn(() => () => {}),
    connected: true,
    error: null,
  })),
}));

// ---------------------------------------------------------------------------
// Component mocks
// ---------------------------------------------------------------------------

// Capture the onCreate prop from CreateCardPanel so tests can invoke it.
let capturedOnCreate: ((input: CreateCardInput, opts?: { run?: boolean; interactive?: boolean }) => Promise<void>) | null = null;

vi.mock('../AppHeader', () => ({
  AppHeader: () => <div data-testid="app-header" />,
}));

vi.mock('../Board', () => ({
  Board: ({ onCreateCard }: { onCreateCard?: () => void }) => {
    return (
      <div data-testid="board">
        <button data-testid="open-create-btn" onClick={onCreateCard}>Open Create</button>
      </div>
    );
  },
}));

vi.mock('../CardPanel', () => ({
  CardPanel: ({ card, onClose }: { card: Card; onClose: () => void }) => (
    <div data-testid={`card-panel-${card.id}`}>
      <span data-testid="card-panel-title">{card.title}</span>
      <button onClick={onClose}>Close</button>
    </div>
  ),
}));

vi.mock('../CreateCardPanel', () => ({
  CreateCardPanel: ({ onCreate, onClose }: {
    onCreate: (input: CreateCardInput, opts?: { run?: boolean; interactive?: boolean }) => Promise<void>;
    onClose: () => void;
  }) => {
    capturedOnCreate = onCreate;
    return (
      <div data-testid="create-card-panel">
        <button data-testid="close-create-btn" onClick={onClose}>Close</button>
      </div>
    );
  },
}));

vi.mock('../ErrorBoundary', () => ({
  ErrorBoundary: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock('../RunnerConsole', () => ({
  RunnerConsole: () => <div data-testid="runner-console" />,
}));

vi.mock('../NotFound', () => ({
  NotFound: () => <div data-testid="not-found" />,
}));

vi.mock('../../api/client', () => ({
  api: {
    runCard: vi.fn(),
    createCard: vi.fn(),
    getDashboard: vi.fn(() => new Promise(() => {})),
    getActivity: vi.fn(() => new Promise(() => {})),
    getRunnerHealth: vi.fn(() => new Promise(() => {})),
  },
  isAPIError: (err: unknown): err is { error: string; code?: string } =>
    err != null && typeof err === 'object' && 'error' in err,
}));

// context/MobileSidebarContext is used by AppHeader; mock it.
vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: vi.fn(() => ({ isOpen: false, toggle: vi.fn(), close: vi.fn() })),
}));

// context/ConsoleStateContext is used by ProjectShell; mock it.
vi.mock('../../context/ConsoleStateContext', () => ({
  useConsoleState: vi.fn(() => ({ isOpen: false, toggle: vi.fn(), close: vi.fn(), setOpen: vi.fn() })),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

import { api } from '../../api/client';

const mockApi = vi.mocked(api);

const baseCard: Card = {
  id: 'TEST-001',
  title: 'New card',
  project: 'test',
  type: 'task',
  state: 'todo',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
};

function renderProjectShell() {
  return render(
    <MemoryRouter initialEntries={['/projects/test']}>
      <Routes>
        <Route path="/projects/:project/*" element={<ProjectShell />} />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  capturedOnCreate = null;
  vi.clearAllMocks();

  // Default: handleCreateCard resolves to the base card
  mockHandleCreateCard.mockResolvedValue(baseCard);

  // Default: api.runCard resolves with updated card
  mockApi.runCard.mockResolvedValue({
    ...baseCard,
    runner_status: 'queued' as const,
    assigned_agent: 'runner-agent',
  });
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ProjectShell — onCreateCard', () => {
  it('"Create & Run" opens the card panel and does not flash', async () => {
    renderProjectShell();

    // Open the create panel by clicking the Board's "Open Create" button.
    const openBtn = screen.getByTestId('open-create-btn');
    act(() => openBtn.click());

    // CreateCardPanel should now be mounted with its onCreate captured.
    expect(screen.getByTestId('create-card-panel')).toBeInTheDocument();
    expect(capturedOnCreate).not.toBeNull();

    // Invoke onCreate simulating "Create & Run HITL".
    await act(async () => {
      await capturedOnCreate!({ title: 'New card', type: 'task', priority: 'medium' }, { run: true, interactive: true });
    });

    // The card panel should open for the newly created card.
    expect(screen.getByTestId('card-panel-TEST-001')).toBeInTheDocument();

    // The create panel should close.
    expect(screen.queryByTestId('create-card-panel')).not.toBeInTheDocument();

    // api.runCard should have been called.
    expect(mockApi.runCard).toHaveBeenCalledWith('test', 'TEST-001', { interactive: true });
  });

  it('"Create & Run" with autonomous=true opens the card panel', async () => {
    renderProjectShell();

    act(() => screen.getByTestId('open-create-btn').click());
    expect(capturedOnCreate).not.toBeNull();

    await act(async () => {
      await capturedOnCreate!({ title: 'Auto card', type: 'task', priority: 'medium' }, { run: true, interactive: false });
    });

    expect(screen.getByTestId('card-panel-TEST-001')).toBeInTheDocument();
    expect(screen.queryByTestId('create-card-panel')).not.toBeInTheDocument();
    expect(mockApi.runCard).toHaveBeenCalledWith('test', 'TEST-001', { interactive: false });
  });

  it('"Just create" does not open the card panel', async () => {
    renderProjectShell();

    act(() => screen.getByTestId('open-create-btn').click());
    expect(capturedOnCreate).not.toBeNull();

    await act(async () => {
      await capturedOnCreate!({ title: 'New card', type: 'task', priority: 'medium' }, { run: false });
    });

    // The card panel must NOT open (no selectedCard set).
    expect(screen.queryByTestId('card-panel-TEST-001')).not.toBeInTheDocument();

    // api.runCard should NOT have been called.
    expect(mockApi.runCard).not.toHaveBeenCalled();

    // The create panel should close.
    expect(screen.queryByTestId('create-card-panel')).not.toBeInTheDocument();
  });

  it('"Create & Run" with runner error does not open the card panel', async () => {
    mockApi.runCard.mockRejectedValue({ error: 'runner offline', code: 'RUNNER_ERROR' });

    renderProjectShell();

    act(() => screen.getByTestId('open-create-btn').click());
    expect(capturedOnCreate).not.toBeNull();

    await act(async () => {
      await capturedOnCreate!({ title: 'New card', type: 'task', priority: 'medium' }, { run: true, interactive: true });
    });

    // On runner error, the panel should not open.
    expect(screen.queryByTestId('card-panel-TEST-001')).not.toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Deep-link tests — ?card=ID opens the CardPanel; closing strips ?card=
// ---------------------------------------------------------------------------

describe('ProjectShell — ?card= deep-link', () => {
  const deepLinkCard: Card = {
    id: 'TEST-1',
    title: 'Deep-linked card',
    project: 'test',
    type: 'task',
    state: 'todo',
    priority: 'medium',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
  };

  const useBoardReturn = {
    config: {
      name: 'test',
      prefix: 'TEST',
      next_id: 1,
      states: ['todo', 'done'],
      types: ['task'],
      priorities: ['medium'],
      transitions: { todo: ['done'], done: [] },
      remote_execution: { enabled: false },
    } as ProjectConfig,
    cards: [deepLinkCard],
    loading: false,
    error: null,
    connected: true,
    refresh: vi.fn(),
    updateCardLocally: vi.fn(),
    removeCardLocally: vi.fn(),
    suppressSSE: vi.fn(),
    unsuppressSSE: vi.fn(),
  };

  it('opens the CardPanel for ?card=ID when cards are loaded', async () => {
    const { useBoard } = await import('../../hooks/useBoard');
    vi.mocked(useBoard).mockReturnValue(useBoardReturn as unknown as ReturnType<typeof useBoard>);

    render(
      <MemoryRouter initialEntries={['/projects/test?card=TEST-1']}>
        <Routes>
          <Route path="/projects/:project/*" element={<ProjectShell />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByTestId('card-panel-TEST-1')).toBeInTheDocument();
    expect(screen.getByTestId('card-panel-title').textContent).toBe('Deep-linked card');
  });

  it('re-opens the panel after cross-project SPA navigation', async () => {
    // Regression test for the cross-project deep-link bug: when the user
    // navigates from /projects/A?card=A-1 to /projects/B?card=B-2 without
    // remounting ProjectShell (SPA nav reuses the :project route segment),
    // the in-render reset on project change must clear the
    // deep-link-consumed marker. Without the reset, the inbound branch's
    // `!deepLinkConsumed` guard stays false and the panel never opens for
    // the second project's deep link.
    const secondCard: Card = {
      id: 'TEST-2',
      title: 'Second project card',
      project: 'test',
      type: 'task',
      state: 'todo',
      priority: 'medium',
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      body: '',
    };
    const { useBoard } = await import('../../hooks/useBoard');
    vi.mocked(useBoard).mockReturnValue({
      ...useBoardReturn,
      cards: [deepLinkCard, secondCard],
    } as unknown as ReturnType<typeof useBoard>);

    function NavProbe() {
      const navigate = useNavigate();
      const [params] = useSearchParams();
      return (
        <>
          <button
            data-testid="cross-project-nav"
            onClick={() => navigate('/projects/other?card=TEST-2')}
          >
            go
          </button>
          <span data-testid="nav-probe-search">{params.toString()}</span>
        </>
      );
    }

    render(
      <MemoryRouter initialEntries={['/projects/test?card=TEST-1']}>
        <Routes>
          <Route
            path="/projects/:project/*"
            element={
              <>
                <ProjectShell />
                <NavProbe />
              </>
            }
          />
        </Routes>
      </MemoryRouter>,
    );

    // First project's panel opens from the URL.
    expect(await screen.findByTestId('card-panel-TEST-1')).toBeInTheDocument();

    // Simulate sidebar navigation to a new project with a different ?card=.
    // The Routes match the same path pattern so ProjectShell does NOT remount.
    await act(async () => {
      screen.getByTestId('cross-project-nav').click();
    });

    // The deep-link branch must re-fire for the new project: the panel
    // for the second card should be mounted.
    expect(await screen.findByTestId('card-panel-TEST-2')).toBeInTheDocument();
    expect(screen.queryByTestId('card-panel-TEST-1')).not.toBeInTheDocument();

    // The new project's ?card= must survive — without the prev-project
    // reset of deepLinkConsumed/pendingUrlStrip, the outbound branch
    // would trip in the gap between the inbound rejection and the
    // self-healing re-fire, stripping the URL as a side effect.
    expect(screen.getByTestId('nav-probe-search').textContent).toBe('card=TEST-2');
  });

  it('strips ?card= from the URL when the panel closes', async () => {
    const { useBoard } = await import('../../hooks/useBoard');
    vi.mocked(useBoard).mockReturnValue(useBoardReturn as unknown as ReturnType<typeof useBoard>);

    function LocationProbe() {
      const [params] = useSearchParams();
      return <span data-testid="loc-search">{params.toString()}</span>;
    }

    render(
      <MemoryRouter initialEntries={['/projects/test?card=TEST-1']}>
        <Routes>
          <Route
            path="/projects/:project/*"
            element={
              <>
                <ProjectShell />
                <LocationProbe />
              </>
            }
          />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByTestId('card-panel-TEST-1')).toBeInTheDocument();
    expect(screen.getByTestId('loc-search').textContent).toBe('card=TEST-1');

    const close = screen.getByRole('button', { name: /close/i });
    await act(async () => {
      close.click();
    });

    expect(screen.getByTestId('loc-search').textContent).toBe('');
  });

  it('strips ?card= from the URL when the deep-linked card does not exist', async () => {
    // Dead-link: ?card=NONEXISTENT-999 with no matching card. The hook must
    // strip the URL once cards have loaded so the dead link does not stay
    // in the URL forever and interfere with future interactions.
    const { useBoard } = await import('../../hooks/useBoard');
    vi.mocked(useBoard).mockReturnValue(useBoardReturn as unknown as ReturnType<typeof useBoard>);

    function LocationProbe() {
      const [params] = useSearchParams();
      return <span data-testid="loc-search">{params.toString()}</span>;
    }

    render(
      <MemoryRouter initialEntries={['/projects/test?card=NONEXISTENT-999']}>
        <Routes>
          <Route
            path="/projects/:project/*"
            element={
              <>
                <ProjectShell />
                <LocationProbe />
              </>
            }
          />
        </Routes>
      </MemoryRouter>,
    );

    // No card panel should open for the missing id.
    expect(screen.queryByTestId('card-panel-NONEXISTENT-999')).not.toBeInTheDocument();
    // The matching deep-link card panel must also stay closed.
    expect(screen.queryByTestId('card-panel-TEST-1')).not.toBeInTheDocument();

    // The dead `?card=` param must be stripped from the URL.
    await waitFor(() => {
      expect(screen.getByTestId('loc-search').textContent).toBe('');
    });
  });
});
