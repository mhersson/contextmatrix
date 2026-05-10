import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { KnowledgeBase } from './KnowledgeBase';
import type { KnowledgeBaseSummary, KnowledgeDocResponse } from '../../types';

// --- api mock ---

const mockGetKnowledgeBase = vi.fn();
const mockGetKnowledgeDoc = vi.fn();
const mockPutKnowledgeDoc = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getKnowledgeBase: (...args: unknown[]) => mockGetKnowledgeBase(...args),
    getKnowledgeDoc: (...args: unknown[]) => mockGetKnowledgeDoc(...args),
    putKnowledgeDoc: (...args: unknown[]) => mockPutKnowledgeDoc(...args),
  },
  errorMessage: (err: unknown): string => {
    if (err && typeof err === 'object' && 'error' in err) {
      return String((err as { error: unknown }).error);
    }
    if (err instanceof Error) return err.message;
    return 'Unknown error';
  },
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
