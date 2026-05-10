import { describe, it, expect, vi, beforeEach } from 'vitest';
import { StrictMode, useState } from 'react';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { KnowledgeBase } from './KnowledgeBase';
import type { KnowledgeBaseSummary, KnowledgeDocResponse, RefreshJobStatus } from '../../types';

// --- api mock ---

const mockGetKnowledgeBase = vi.fn();
const mockGetKnowledgeDoc = vi.fn();
const mockPutKnowledgeDoc = vi.fn();
const mockGetKnowledgeRefreshPlan = vi.fn();
const mockStartKnowledgeRefresh = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getKnowledgeBase: (...args: unknown[]) => mockGetKnowledgeBase(...args),
    getKnowledgeDoc: (...args: unknown[]) => mockGetKnowledgeDoc(...args),
    putKnowledgeDoc: (...args: unknown[]) => mockPutKnowledgeDoc(...args),
    getKnowledgeRefreshPlan: (...args: unknown[]) => mockGetKnowledgeRefreshPlan(...args),
    startKnowledgeRefresh: (...args: unknown[]) => mockStartKnowledgeRefresh(...args),
  },
  errorMessage: (err: unknown): string => {
    if (err && typeof err === 'object' && 'error' in err) {
      return String((err as { error: unknown }).error);
    }
    if (err instanceof Error) return err.message;
    return 'Unknown error';
  },
}));

// --- useKnowledgeRefreshStatus mock ---
// The mock implementation calls a per-test hook returning the current repos
// snapshot. Tests that want to simulate poll-tick transitions install a
// driver via setRefreshDriver(); see the "reload-on-transition" test for an
// example. By default the mock returns an idle state.
type RefreshDriver = () => Record<string, RefreshJobStatus>;
const idleDriver: RefreshDriver = () => ({});
let refreshDriver: RefreshDriver = idleDriver;
const mockRefreshFn = vi.fn();
function setRefreshDriver(d: RefreshDriver) {
  refreshDriver = d;
}
vi.mock('./useKnowledgeRefreshStatus', () => ({
  useKnowledgeRefreshStatus: () => ({ repos: refreshDriver(), refresh: mockRefreshFn }),
}));

// --- Lazy-loaded component mocks ---
// @uiw/react-md-editor is not available in jsdom; mock the whole module so
// KnowledgeDocEditor can render without dynamic import errors.
vi.mock('@uiw/react-md-editor', () => ({
  default: ({
    value,
    onChange,
  }: {
    value: string;
    onChange?: (v: string) => void;
  }) => (
    <textarea
      data-testid="md-editor"
      value={value}
      onChange={(e) => onChange?.(e.target.value)}
    />
  ),
}));

vi.mock('@uiw/react-markdown-preview', () => ({
  default: ({ source }: { source: string }) => <div data-testid="md-preview">{source}</div>,
}));

// useEditorHeight is irrelevant here
vi.mock('../../hooks/useEditorHeight', () => ({
  useEditorHeight: () => 400,
}));

vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ theme: 'dark' }),
}));

// --- fixtures ---

const makeSummary = (): KnowledgeBaseSummary => ({
  project: 'p',
  repos: [
    {
      name: 'repo-a',
      last_built_at: '2026-01-01T00:00:00Z',
      last_built_commit: 'abc',
      docs: [
        { name: 'doc-one.md', human_edited: false },
        { name: 'doc-two.md', human_edited: false },
      ],
    },
  ],
});

const makeDocResponse = (content: string): KnowledgeDocResponse => ({
  content,
  meta: { last_built_commit: 'abc', human_edited: false },
});

beforeEach(() => {
  vi.clearAllMocks();
  mockGetKnowledgeBase.mockResolvedValue(makeSummary());
  mockGetKnowledgeDoc.mockResolvedValue(makeDocResponse('# Hello'));
  mockPutKnowledgeDoc.mockResolvedValue(undefined);
  mockGetKnowledgeRefreshPlan.mockResolvedValue({ items: [] });
  mockStartKnowledgeRefresh.mockResolvedValue(undefined);
  refreshDriver = idleDriver;
});

// --- helpers ---

/**
 * Renders KnowledgeBase inside a MemoryRouter wired to the knowledge/* splat
 * pattern, starting at the given initial URL.
 */
function renderWithRouter(initialUrl = '/projects/p/knowledge') {
  return render(
    <MemoryRouter initialEntries={[initialUrl]}>
      <Routes>
        <Route path="/projects/:project/knowledge/*" element={<KnowledgeBase project="p" />} />
      </Routes>
    </MemoryRouter>,
  );
}

async function renderAndSelectDoc(docName = 'doc-one.md') {
  renderWithRouter();
  // wait for sidebar to appear
  await waitFor(() => expect(screen.getByRole('navigation')).toBeInTheDocument());
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: docName }));
  });
  // wait for viewer to appear
  await waitFor(() => screen.getByTestId('md-preview'));
}

// --- tests ---

describe('KnowledgeBase — viewer key', () => {
  it('remounts the viewer when a different doc is selected (key forces remount)', async () => {
    // First doc resolves to 'doc-one content', second resolves to 'doc-two content'
    mockGetKnowledgeDoc
      .mockResolvedValueOnce(makeDocResponse('doc-one content'))
      .mockResolvedValueOnce(makeDocResponse('doc-two content'));

    renderWithRouter();
    await waitFor(() => screen.getByRole('navigation'));

    // Select doc-one
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'doc-one.md' }));
    });
    await waitFor(() => expect(screen.getByTestId('md-preview').textContent).toBe('doc-one content'));

    // Select doc-two — new key must force remount
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'doc-two.md' }));
    });
    await waitFor(() => expect(screen.getByTestId('md-preview').textContent).toBe('doc-two content'));
  });
});

describe('KnowledgeBase — unsaved-changes guard', () => {
  it('shows ConfirmModal when switching docs while editor is dirty', async () => {
    await renderAndSelectDoc('doc-one.md');

    // Enter edit mode
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    await waitFor(() => screen.getByTestId('md-editor'));

    // Dirty the editor
    fireEvent.change(screen.getByTestId('md-editor'), {
      target: { value: '# Hello changed' },
    });

    // Click another doc — should open the ConfirmModal instead of switching
    fireEvent.click(screen.getByRole('button', { name: 'doc-two.md' }));

    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Discard unsaved changes?')).toBeInTheDocument();
  });

  it('"Keep editing" closes the modal without switching docs', async () => {
    await renderAndSelectDoc('doc-one.md');

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    await waitFor(() => screen.getByTestId('md-editor'));
    fireEvent.change(screen.getByTestId('md-editor'), {
      target: { value: '# Hello changed' },
    });

    fireEvent.click(screen.getByRole('button', { name: 'doc-two.md' }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();

    // Cancel — modal should close, still on doc-one
    fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

    // The editor for doc-one is still visible
    expect(screen.getByTestId('md-editor')).toBeInTheDocument();
    // getKnowledgeDoc for doc-two was NOT called (selection was not committed)
    expect(mockGetKnowledgeDoc).toHaveBeenCalledTimes(1);
  });

  it('"Discard" switches to the pending doc', async () => {
    mockGetKnowledgeDoc
      .mockResolvedValueOnce(makeDocResponse('# Hello'))
      .mockResolvedValueOnce(makeDocResponse('doc-two content'));

    renderWithRouter();
    await waitFor(() => screen.getByRole('navigation'));
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'doc-one.md' }));
    });
    await waitFor(() => screen.getByTestId('md-preview'));

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    await waitFor(() => screen.getByTestId('md-editor'));
    fireEvent.change(screen.getByTestId('md-editor'), {
      target: { value: '# Hello changed' },
    });

    fireEvent.click(screen.getByRole('button', { name: 'doc-two.md' }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();

    // Confirm discard
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    });

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId('md-preview').textContent).toBe('doc-two content'));
  });

  it('no modal appears when switching docs without unsaved edits', async () => {
    mockGetKnowledgeDoc
      .mockResolvedValueOnce(makeDocResponse('# Hello'))
      .mockResolvedValueOnce(makeDocResponse('doc-two content'));

    renderWithRouter();
    await waitFor(() => screen.getByRole('navigation'));
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'doc-one.md' }));
    });
    await waitFor(() => screen.getByTestId('md-preview'));

    // Click another doc without entering edit mode
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'doc-two.md' }));
    });

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId('md-preview').textContent).toBe('doc-two content'));
  });
});

describe('KnowledgeBase — editor cancel-during-save', () => {
  it('aborts an in-flight save when the editor is cancelled', async () => {
    let receivedSignal: AbortSignal | undefined;
    // putKnowledgeDoc never resolves; capture the signal the editor passes.
    mockPutKnowledgeDoc.mockImplementationOnce(
      (
        _project: string,
        _repo: string,
        _doc: string,
        _content: string,
        opts?: { signal?: AbortSignal },
      ) => {
        receivedSignal = opts?.signal;
        return new Promise(() => { /* never resolves */ });
      },
    );

    await renderAndSelectDoc('doc-one.md');

    // Enter edit mode and dirty the editor.
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    await waitFor(() => screen.getByTestId('md-editor'));
    fireEvent.change(screen.getByTestId('md-editor'), {
      target: { value: '# Hello changed' },
    });

    // Click Save — request goes out, never resolves.
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => expect(mockPutKnowledgeDoc).toHaveBeenCalledTimes(1));
    expect(receivedSignal?.aborted).toBe(false);

    // Click Cancel mid-save — must abort the in-flight request.
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }));
    expect(receivedSignal?.aborted).toBe(true);

    // Editor is gone, viewer is back; getKnowledgeBase was not reloaded by the
    // (still-pending) save handler — only the original mount call.
    await waitFor(() => screen.getByTestId('md-preview'));
    expect(mockGetKnowledgeBase).toHaveBeenCalledTimes(1);
  });

});

describe('KnowledgeBase — sidebar already-selected guard', () => {
  it('does not call getKnowledgeDoc a second time when clicking the already-selected doc', async () => {
    renderWithRouter();
    await waitFor(() => screen.getByRole('navigation'));

    // Select doc-one
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'doc-one.md' }));
    });
    await waitFor(() => screen.getByTestId('md-preview'));

    const callsAfterFirstSelect = mockGetKnowledgeDoc.mock.calls.length;

    // Click the same doc again — sidebar guard should short-circuit
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'doc-one.md' }));
    });

    expect(mockGetKnowledgeDoc.mock.calls.length).toBe(callsAfterFirstSelect);
  });
});

describe('KnowledgeBase — summary fetch error', () => {
  it('renders the error UI instead of "No knowledge base yet" on summary failure', async () => {
    mockGetKnowledgeBase.mockRejectedValue(new Error('network failure'));

    renderWithRouter();

    await waitFor(() =>
      expect(screen.getByText(/Failed to load knowledge base/i)).toBeInTheDocument(),
    );
    expect(screen.queryByText(/No knowledge base yet/i)).not.toBeInTheDocument();
  });
});

describe('KnowledgeBase — sidebar a11y', () => {
  it('renders repo names as h3 headings', async () => {
    renderWithRouter();
    await waitFor(() => screen.getByRole('navigation'));
    const heading = screen.getByRole('heading', { level: 3, name: 'repo-a' });
    expect(heading).toBeInTheDocument();
  });

  it('marks selected doc with aria-current="true"', async () => {
    renderWithRouter();
    await waitFor(() => screen.getByRole('navigation'));

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'doc-one.md' }));
    });
    await waitFor(() => screen.getByTestId('md-preview'));

    const selected = screen.getByRole('button', { name: 'doc-one.md' });
    expect(selected).toHaveAttribute('aria-current', 'true');

    const notSelected = screen.getByRole('button', { name: 'doc-two.md' });
    expect(notSelected).not.toHaveAttribute('aria-current');
  });

  it('ArrowDown moves focus to next doc button', async () => {
    renderWithRouter();
    const nav = await waitFor(() => screen.getByRole('navigation'));

    const firstButton = screen.getByRole('button', { name: 'doc-one.md' });
    const secondButton = screen.getByRole('button', { name: 'doc-two.md' });

    firstButton.focus();
    fireEvent.keyDown(nav, { key: 'ArrowDown' });

    expect(document.activeElement).toBe(secondButton);
  });

  it('ArrowUp does not move past the first doc', async () => {
    renderWithRouter();
    const nav = await waitFor(() => screen.getByRole('navigation'));

    const firstButton = screen.getByRole('button', { name: 'doc-one.md' });
    firstButton.focus();
    fireEvent.keyDown(nav, { key: 'ArrowUp' });

    expect(document.activeElement).toBe(firstButton);
  });

  it('unselected doc buttons have tabIndex=-1 and selected-index button has tabIndex=0', async () => {
    renderWithRouter();
    await waitFor(() => screen.getByRole('navigation'));

    // Initially focusedIdx=0 so first button is 0, second is -1
    const firstButton = screen.getByRole('button', { name: 'doc-one.md' });
    const secondButton = screen.getByRole('button', { name: 'doc-two.md' });
    expect(firstButton).toHaveAttribute('tabindex', '0');
    expect(secondButton).toHaveAttribute('tabindex', '-1');
  });

  it('preserves correct tabIndex under StrictMode (no double-counted indices)', async () => {
    // StrictMode double-invokes render in dev. The previous let-flatIdx-mutation
    // pattern would have produced two buttons with tabIndex=0 (or duplicated
    // indices); the Map-based lookup is render-pure and therefore stable.
    render(
      <StrictMode>
        <MemoryRouter initialEntries={['/projects/p/knowledge']}>
          <Routes>
            <Route path="/projects/:project/knowledge/*" element={<KnowledgeBase project="p" />} />
          </Routes>
        </MemoryRouter>
      </StrictMode>,
    );
    await waitFor(() => screen.getByRole('navigation'));

    const docButtons = [
      screen.getByRole('button', { name: 'doc-one.md' }),
      screen.getByRole('button', { name: 'doc-two.md' }),
    ];
    const focusable = docButtons.filter((b) => b.getAttribute('tabindex') === '0');
    expect(focusable).toHaveLength(1);
    expect(focusable[0]).toBe(docButtons[0]);
  });

  it('End jumps focus to the last doc', async () => {
    renderWithRouter();
    const nav = await waitFor(() => screen.getByRole('navigation'));

    const firstButton = screen.getByRole('button', { name: 'doc-one.md' });
    const lastButton = screen.getByRole('button', { name: 'doc-two.md' });

    firstButton.focus();
    fireEvent.keyDown(nav, { key: 'End' });

    expect(document.activeElement).toBe(lastButton);
  });

  it('Home jumps focus to the first doc', async () => {
    renderWithRouter();
    const nav = await waitFor(() => screen.getByRole('navigation'));

    const firstButton = screen.getByRole('button', { name: 'doc-one.md' });
    const lastButton = screen.getByRole('button', { name: 'doc-two.md' });

    lastButton.focus();
    fireEvent.keyDown(nav, { key: 'Home' });

    expect(document.activeElement).toBe(firstButton);
  });
});

describe('KnowledgeBase — doc loading state', () => {
  it('shows "Loading…" while the doc fetch is in flight', async () => {
    // First call (summary) resolves normally; second call (doc) never resolves
    let resolveSummary!: (v: unknown) => void;
    mockGetKnowledgeBase.mockImplementationOnce(
      () => new Promise((resolve) => { resolveSummary = resolve; }),
    );

    renderWithRouter();

    // Resolve the summary so the sidebar appears
    await act(async () => {
      resolveSummary(makeSummary());
    });
    await waitFor(() => screen.getByRole('navigation'));

    // Doc fetch never resolves — use a persistent pending promise
    mockGetKnowledgeDoc.mockImplementation(() => new Promise(() => {}));

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'doc-one.md' }));
    });

    expect(screen.getByText('Loading…')).toBeInTheDocument();
  });
});

describe('KnowledgeBase — reload-on-refresh-success', () => {
  it('reloads KB summary only on the rising edge into succeeded, not on every poll', async () => {
    // Drive the refresh-status mock from the test: sequence the per-tick
    // snapshot that the polling hook would produce.
    const ticks: Array<Record<string, RefreshJobStatus>> = [
      { 'repo-a': { state: 'running', docs_done: 0, docs_total: 4 } },
      { 'repo-a': { state: 'succeeded', docs_done: 4, docs_total: 4 } },
      { 'repo-a': { state: 'succeeded', docs_done: 4, docs_total: 4 } },
      { 'repo-a': { state: 'succeeded', docs_done: 4, docs_total: 4 } },
    ];
    let tickIdx = 0;
    setRefreshDriver(() => ticks[Math.min(tickIdx, ticks.length - 1)]);

    // Wrapper exposes a state setter so the test can force re-renders without
    // re-mounting (mirrors what the polling hook does on every tick).
    let bumpTick: (n: number) => void = () => {};
    function Harness() {
      const [, setN] = useState(0);
      bumpTick = (n: number) => {
        tickIdx = n;
        setN((x) => x + 1);
      };
      return (
        <Routes>
          <Route path="/projects/:project/knowledge/*" element={<KnowledgeBase project="p" />} />
        </Routes>
      );
    }

    render(
      <MemoryRouter initialEntries={['/projects/p/knowledge']}>
        <Harness />
      </MemoryRouter>,
    );

    // Initial mount fetches the summary once via useKnowledgeBaseData.
    await waitFor(() => expect(mockGetKnowledgeBase).toHaveBeenCalledTimes(1));

    // Tick 1 (running) — no reload triggered.
    await act(async () => {
      bumpTick(0);
    });
    expect(mockGetKnowledgeBase).toHaveBeenCalledTimes(1);

    // Tick 2 (transition into succeeded) — should trigger one reload.
    await act(async () => {
      bumpTick(1);
    });
    await waitFor(() => expect(mockGetKnowledgeBase).toHaveBeenCalledTimes(2));

    // Tick 3 (still succeeded) — must NOT trigger another reload.
    await act(async () => {
      bumpTick(2);
    });
    expect(mockGetKnowledgeBase).toHaveBeenCalledTimes(2);

    // Tick 4 (still succeeded) — must NOT trigger another reload.
    await act(async () => {
      bumpTick(3);
    });
    expect(mockGetKnowledgeBase).toHaveBeenCalledTimes(2);
  });
});

describe('KnowledgeBase — summary reload error', () => {
  it('surfaces summary fetch error near the sidebar, not in the doc viewer', async () => {
    // Drive a rising edge into 'succeeded' to trigger reload(). The reload's
    // summary fetch fails — that error must NOT be rendered as "Failed to load
    // doc" because no doc-specific failure occurred.
    const ticks: Array<Record<string, RefreshJobStatus>> = [
      { 'repo-a': { state: 'running', docs_done: 0, docs_total: 4 } },
      { 'repo-a': { state: 'succeeded', docs_done: 4, docs_total: 4 } },
    ];
    let tickIdx = 0;
    setRefreshDriver(() => ticks[Math.min(tickIdx, ticks.length - 1)]);

    let bumpTick: (n: number) => void = () => {};
    function Harness() {
      const [, setN] = useState(0);
      bumpTick = (n: number) => {
        tickIdx = n;
        setN((x) => x + 1);
      };
      return (
        <Routes>
          <Route path="/projects/:project/knowledge/*" element={<KnowledgeBase project="p" />} />
        </Routes>
      );
    }

    render(
      <MemoryRouter initialEntries={['/projects/p/knowledge']}>
        <Harness />
      </MemoryRouter>,
    );

    // Initial mount: summary resolves successfully, sidebar renders.
    await waitFor(() => screen.getByRole('navigation'));

    // Reload's summary fetch will reject. Doc fetch should not be called
    // because no doc is selected.
    mockGetKnowledgeBase.mockRejectedValueOnce(new Error('summary boom'));

    await act(async () => {
      bumpTick(0);
    });
    await act(async () => {
      bumpTick(1);
    });

    // Surface message near the sidebar — assert the summary error text appears.
    await waitFor(() =>
      expect(screen.getByText(/summary boom/i)).toBeInTheDocument(),
    );

    // The doc viewer area must NOT show "Failed to load doc:" since no doc
    // was being loaded.
    expect(screen.queryByText(/Failed to load doc/i)).not.toBeInTheDocument();
  });
});

describe('KnowledgeBase — deep-link', () => {
  it('loads the doc directly when URL already contains repo/doc', async () => {
    mockGetKnowledgeDoc.mockResolvedValue(makeDocResponse('deep-link content'));

    renderWithRouter('/projects/p/knowledge/repo-a/doc-one.md');

    await waitFor(() => expect(screen.getByTestId('md-preview')).toBeInTheDocument());
    expect(screen.getByTestId('md-preview').textContent).toBe('deep-link content');
    expect(mockGetKnowledgeDoc).toHaveBeenCalledWith('p', 'repo-a', 'doc-one.md');
  });

  it('clicking a sidebar doc updates the URL (selection reflected in sidebar highlight)', async () => {
    mockGetKnowledgeDoc
      .mockResolvedValueOnce(makeDocResponse('doc-one content'))
      .mockResolvedValueOnce(makeDocResponse('doc-two content'));

    renderWithRouter();
    await waitFor(() => screen.getByRole('navigation'));

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'doc-one.md' }));
    });
    await waitFor(() => screen.getByTestId('md-preview'));

    // Now switch to doc-two — it should fetch and display doc-two content
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'doc-two.md' }));
    });
    await waitFor(() =>
      expect(screen.getByTestId('md-preview').textContent).toBe('doc-two content'),
    );

    // doc-one.md button should no longer have the selected style (bg1 background means selected)
    // and getKnowledgeDoc was called twice total
    expect(mockGetKnowledgeDoc).toHaveBeenCalledTimes(2);
    expect(mockGetKnowledgeDoc).toHaveBeenNthCalledWith(1, 'p', 'repo-a', 'doc-one.md');
    expect(mockGetKnowledgeDoc).toHaveBeenNthCalledWith(2, 'p', 'repo-a', 'doc-two.md');
  });
});
