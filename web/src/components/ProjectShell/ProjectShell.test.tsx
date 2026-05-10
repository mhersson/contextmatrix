import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
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

vi.mock('../../hooks/useAgentId', () => ({
  useAgentId: vi.fn(() => ({
    agentId: 'human:web-test1234',
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

vi.mock('../../hooks/useLastProject', () => ({
  useLastProject: vi.fn(() => [null, vi.fn()]),
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
  CardPanel: ({ card }: { card: Card }) => (
    <div data-testid={`card-panel-${card.id}`} />
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
  },
  isAPIError: (err: unknown): err is { error: string; code?: string } =>
    err != null && typeof err === 'object' && 'error' in err,
}));

// context/MobileSidebarContext is used by AppHeader; mock it.
vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: vi.fn(() => ({ isOpen: false, toggle: vi.fn(), close: vi.fn() })),
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

  it('"Just create" still closes the create panel', async () => {
    renderProjectShell();

    act(() => screen.getByTestId('open-create-btn').click());
    expect(screen.getByTestId('create-card-panel')).toBeInTheDocument();

    await act(async () => {
      await capturedOnCreate!({ title: 'New card', type: 'task', priority: 'medium' }, { run: false });
    });

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
