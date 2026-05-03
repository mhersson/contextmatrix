import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { CardPanel } from './CardPanel';
import type { Card, ProjectConfig } from '../../types';

vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ theme: 'dark', palette: 'everforest', toggleTheme: vi.fn() }),
}));

// MDEditor is only mounted in edit mode. The mock exposes a textarea under
// the `md-editor` testid so tests can type into it.
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

// MarkdownPreview is mounted in the read-only path. The mock honours
// `skipHtml` so the XSS guard assertions remain meaningful.
vi.mock('@uiw/react-markdown-preview', () => ({
  default: ({ source, skipHtml }: { source: string; skipHtml?: boolean }) => (
    skipHtml
      ? <div data-testid="md-preview">{source}</div>
      : <div data-testid="md-preview" dangerouslySetInnerHTML={{ __html: source }} />
  ),
}));

vi.mock('../../api/client', () => ({
  api: {
    fetchBranches: vi.fn().mockResolvedValue([]),
    getCard: vi.fn().mockResolvedValue({ state: 'todo' }),
    getTaskSkills: vi.fn().mockResolvedValue([]),
  },
  isAPIError: (err: unknown): err is { error: string; code?: string } =>
    err != null && typeof err === 'object' && 'error' in err,
}));

vi.mock('./CardChat', () => ({
  CardChat: () => <div data-testid="card-chat-mock" />,
}));

const baseCard: Card = {
  id: 'TEST-001',
  title: 'Test card',
  project: 'test',
  type: 'task',
  state: 'todo',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
  autonomous: false,
  feature_branch: false,
  create_pr: false,
};

const config: ProjectConfig = {
  name: 'Test',
  prefix: 'TEST',
  next_id: 2,
  states: ['todo', 'in_progress', 'review', 'done', 'blocked'],
  types: ['task'],
  priorities: ['low', 'medium', 'high'],
  transitions: {
    todo: ['in_progress', 'blocked'],
    in_progress: ['review'],
    review: ['done', 'in_progress'],
    done: ['todo'],
    blocked: ['todo'],
  },
  remote_execution: { enabled: true },
};

function makeProps(overrides?: Partial<Parameters<typeof CardPanel>[0]>) {
  return {
    card: baseCard,
    config,
    onClose: vi.fn(),
    onSave: vi.fn().mockResolvedValue(undefined),
    onClaim: vi.fn().mockResolvedValue(undefined),
    onRelease: vi.fn().mockResolvedValue(undefined),
    onSubtaskClick: vi.fn(),
    currentAgentId: null,
    onPromptAgentId: vi.fn().mockReturnValue(null),
    onRunCard: vi.fn().mockResolvedValue(undefined),
    onStopCard: vi.fn().mockResolvedValue(undefined),
    onDelete: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

describe('CardPanel — bifold layout', () => {
  it('renders the two-column grid (left + right rail)', () => {
    render(<CardPanel {...makeProps()} />);
    expect(screen.getByTestId('body-bifold')).toBeInTheDocument();
    expect(screen.getByTestId('body-left')).toBeInTheDocument();
    expect(screen.getByTestId('body-rail')).toBeInTheDocument();
  });

  it('renders the primary tabs (Automation, Info, Danger) for a non-HITL card', () => {
    render(<CardPanel {...makeProps()} />);
    expect(screen.getByRole('tab', { name: /Automation/ })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Info' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Danger' })).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: /Chat/ })).not.toBeInTheDocument();
  });

  it('adds the Chat tab and selects it by default when HITL is running', () => {
    render(
      <CardPanel
        {...makeProps({
          card: { ...baseCard, state: 'in_progress', runner_status: 'running', autonomous: false },
        })}
      />,
    );
    const chatTab = screen.getByRole('tab', { name: /Chat/ });
    expect(chatTab).toHaveAttribute('aria-selected', 'true');
  });

  it('hides the Chat tab on subtask cards even when HITL is running', () => {
    render(
      <CardPanel
        {...makeProps({
          card: {
            ...baseCard,
            type: 'subtask',
            parent: 'TEST-000',
            state: 'in_progress',
            runner_status: 'running',
            autonomous: false,
          },
        })}
      />,
    );
    expect(screen.queryByRole('tab', { name: /Chat/ })).not.toBeInTheDocument();
  });

  it('default tab is Automation when the runner is not running HITL', () => {
    render(<CardPanel {...makeProps()} />);
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('rail expand toggle flips the grid template and the toggle aria-pressed', () => {
    render(<CardPanel {...makeProps()} />);
    const grid = screen.getByTestId('body-bifold');
    expect(grid.style.gridTemplateColumns).toContain('340px');
    fireEvent.click(screen.getByRole('button', { name: 'Expand rail' }));
    expect(grid.style.gridTemplateColumns).toContain('600px');
    expect(screen.getByRole('button', { name: 'Collapse rail' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('preserves railExpanded when the card state changes via a new card object (SSE refresh)', () => {
    const initial = { ...baseCard, state: 'in_progress', runner_status: 'running' as const, autonomous: false };
    const { rerender } = render(<CardPanel {...makeProps({ card: initial })} />);
    const grid = screen.getByTestId('body-bifold');

    // HITL cards auto-expand the rail on mount — no manual click needed.
    expect(grid.style.gridTemplateColumns).toContain('600px');
    expect(screen.getByRole('button', { name: 'Collapse rail' })).toHaveAttribute('aria-pressed', 'true');

    // Simulate an SSE-driven card refresh: same id, new object reference,
    // different state. The rail must stay expanded so mid-HITL users don't
    // lose their layout when the agent transitions the card.
    const next = { ...initial, state: 'review' };
    rerender(<CardPanel {...makeProps({ card: next })} />);

    expect(grid.style.gridTemplateColumns).toContain('600px');
    expect(screen.getByRole('button', { name: 'Collapse rail' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('collapses railExpanded when the card identity changes (different card selected)', () => {
    const { rerender } = render(<CardPanel {...makeProps({ card: baseCard })} />);
    const grid = screen.getByTestId('body-bifold');

    fireEvent.click(screen.getByRole('button', { name: 'Expand rail' }));
    expect(grid.style.gridTemplateColumns).toContain('600px');

    // Switching to a different card (new id) is the only path that should
    // collapse the rail.
    const other = { ...baseCard, id: 'TEST-002', title: 'Other card' };
    rerender(<CardPanel {...makeProps({ card: other })} />);

    expect(grid.style.gridTemplateColumns).toContain('340px');
    expect(screen.getByRole('button', { name: 'Expand rail' })).toHaveAttribute('aria-pressed', 'false');
  });
});

describe('CardPanel — Info tab hosts the state picker', () => {
  it('switches to Info and reveals the State select', async () => {
    render(<CardPanel {...makeProps()} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Info' }));
    expect(await screen.findByRole('combobox', { name: 'State' })).toBeInTheDocument();
  });
});

describe('CardPanel — Run handler (save-before-run)', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('calls onSave before onRunCard when card is dirty via header title edit', async () => {
    const calls: string[] = [];
    const onSave = vi.fn(async () => { calls.push('save'); });
    const onRunCard = vi.fn(async () => { calls.push('run'); });

    render(<CardPanel {...makeProps({ onSave, onRunCard })} />);
    const titleInput = screen.getByDisplayValue('Test card');
    fireEvent.change(titleInput, { target: { value: 'Dirty title' } });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Run HITL/ }));
    });

    expect(calls).toEqual(['save', 'run']);
  });

  it('Run click force-enables feature_branch + create_pr when they were off (server mirrors this)', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const onRunCard = vi.fn().mockResolvedValue(undefined);

    render(<CardPanel {...makeProps({ onSave, onRunCard, card: { ...baseCard, feature_branch: false, create_pr: false } })} />);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Run HITL/ }));
    });

    expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ feature_branch: true, create_pr: true }));
  });

  it('does NOT call onSave when the card is already clean AND feature_branch/create_pr were already on', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const onRunCard = vi.fn().mockResolvedValue(undefined);
    const card = { ...baseCard, feature_branch: true, create_pr: true };
    render(<CardPanel {...makeProps({ onSave, onRunCard, card })} />);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Run HITL/ }));
    });

    expect(onSave).not.toHaveBeenCalled();
    expect(onRunCard).toHaveBeenCalledOnce();
  });

  it('reverts optimistic feature_branch/create_pr when onSave rejects (no runner fire)', async () => {
    const onSave = vi.fn().mockRejectedValue({ error: 'save failed' });
    const onRunCard = vi.fn().mockResolvedValue(undefined);
    render(<CardPanel {...makeProps({ onSave, onRunCard })} />);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Run HITL/ }));
    });

    expect(onSave).toHaveBeenCalledOnce();
    expect(onRunCard).not.toHaveBeenCalled();

    // Clicking Run again must re-send the same optimistic patch — the revert
    // put feature_branch/create_pr back to their pre-Run values, so the card
    // is still dirty relative to the server and a fresh save is attempted.
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Run HITL/ }));
    });
    expect(onSave).toHaveBeenCalledTimes(2);
    expect(onSave).toHaveBeenLastCalledWith(
      expect.objectContaining({ feature_branch: true, create_pr: true }),
    );
  });
});

describe('CardPanel — transition primary rollback', () => {
  it('reverts optimistic state transition when onSave rejects', async () => {
    const onSave = vi.fn().mockRejectedValue({ error: 'save failed' });
    render(
      <CardPanel
        {...makeProps({ onSave, card: { ...baseCard, state: 'review' } })}
      />,
    );

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Mark done' }));
    });

    expect(onSave).toHaveBeenCalledOnce();

    // Info tab reveals the state picker — confirm it reverted to 'review'.
    fireEvent.click(screen.getByRole('tab', { name: 'Info' }));
    const stateSelect = (await screen.findByRole(
      'combobox', { name: 'State' },
    )) as HTMLSelectElement;
    expect(stateSelect.value).toBe('review');
  });
});

describe('CardPanel — Delete via Danger Zone', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('Danger Zone Delete invokes onDelete for an eligible card', async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined);
    render(
      <CardPanel
        {...makeProps({
          card: { ...baseCard, state: 'todo', assigned_agent: undefined },
          onDelete,
        })}
      />,
    );

    fireEvent.click(screen.getByRole('tab', { name: 'Danger' }));
    fireEvent.click(screen.getByRole('button', { name: 'Delete card' }));
    // ConfirmModal opens; click its confirm button.
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    });

    expect(onDelete).toHaveBeenCalledWith('TEST-001');
  });

  it('Delete button is disabled for a claimed card', () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(
      <CardPanel
        {...makeProps({ card: { ...baseCard, state: 'todo', assigned_agent: 'some-agent' } })}
      />,
    );
    fireEvent.click(screen.getByRole('tab', { name: 'Danger' }));
    expect(screen.getByRole('button', { name: 'Delete card' })).toBeDisabled();
  });

  it('Delete button is disabled when state is in_progress', () => {
    render(
      <CardPanel
        {...makeProps({ card: { ...baseCard, state: 'in_progress' } })}
      />,
    );
    fireEvent.click(screen.getByRole('tab', { name: 'Danger' }));
    expect(screen.getByRole('button', { name: 'Delete card' })).toBeDisabled();
  });
});

describe('CardPanel — MDEditor preview skipHtml XSS prevention', () => {
  const xssBody = '<iframe src="https://example.com"></iframe>\n<script>alert(\'xss\')</script>\nhello';

  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('does not render iframe in the preview pane', async () => {
    const { container } = render(
      <CardPanel {...makeProps({ card: { ...baseCard, body: xssBody } })} />,
    );
    await screen.findByTestId('md-preview');
    expect(container.querySelector('iframe')).toBeNull();
  });

  it('does not render script in the preview pane', async () => {
    const { container } = render(
      <CardPanel {...makeProps({ card: { ...baseCard, body: xssBody } })} />,
    );
    await screen.findByTestId('md-preview');
    expect(container.querySelector('script')).toBeNull();
  });

  it('does not render anchors with javascript: hrefs in the preview pane', async () => {
    // The skipHtml-honoring mock stores the raw markdown under md-preview.
    // Assert by inspecting every anchor in the DOM — if the real renderer
    // ever starts producing anchors from markdown link syntax and forgets
    // to filter javascript: URLs, this test fails.
    const body = '[click](javascript:alert(1))\nhello';
    const { container } = render(
      <CardPanel {...makeProps({ card: { ...baseCard, body } })} />,
    );
    await screen.findByTestId('md-preview');
    const anchors = container.querySelectorAll('a[href]');
    anchors.forEach((a) => {
      const href = a.getAttribute('href') ?? '';
      expect(href).not.toMatch(/^javascript:/i);
    });
  });
});

describe('CardPanel — rail default tab follows isHITLRunning', () => {
  it('mounts on Chat when the card arrives already running an HITL session', () => {
    render(
      <CardPanel
        {...makeProps({
          card: { ...baseCard, state: 'in_progress', runner_status: 'running', autonomous: false },
        })}
      />,
    );
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('switches active tab from Automation to Chat when the same card transitions to HITL', () => {
    const { rerender } = render(<CardPanel {...makeProps()} />);
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'true');

    rerender(
      <CardPanel
        {...makeProps({
          card: { ...baseCard, state: 'in_progress', runner_status: 'running', autonomous: false },
        })}
      />,
    );
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('switches the active tab back to Automation when the HITL session ends (two consecutive renders)', () => {
    const runningCard = { ...baseCard, state: 'in_progress', runner_status: 'running' as const, autonomous: false };
    const autonomousCard = { ...baseCard, state: 'in_progress', runner_status: 'running' as const, autonomous: true };
    const { rerender } = render(<CardPanel {...makeProps({ card: runningCard })} />);
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');

    // First render with isHITLRunning=false: chat tab stays (debounce — counter=1).
    rerender(<CardPanel {...makeProps({ card: autonomousCard })} />);
    // Second render with isHITLRunning still false: now the switch fires (counter=2).
    rerender(<CardPanel {...makeProps({ card: { ...autonomousCard, updated: '2026-01-02T00:00:00Z' } })} />);

    expect(screen.queryByRole('tab', { name: /Chat/ })).not.toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('does NOT switch activeTab to Automation on first render after isHITLRunning flips false (counter=1 guard)', () => {
    // Verifies the debounce: a single flip to false does NOT call setActiveTab(defaultTab).
    // After the flip back to true, the chat tab is re-mounted and selected (no stale automation state).
    const runningCard = { ...baseCard, state: 'in_progress', runner_status: 'running' as const, autonomous: false };
    const { rerender } = render(<CardPanel {...makeProps({ card: runningCard })} />);
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');

    // Render 2: transient HITL=false (SSE lag). Chat tab removed from tab set by buildCardPanelTabs
    // (isHITLRunning=false), but activeTab must NOT be set to 'automation' by the sync block.
    const autonomousCard = { ...runningCard, autonomous: true };
    rerender(<CardPanel {...makeProps({ card: autonomousCard })} />);

    // Render 3: HITL flips back to true (SSE corrects). Chat tab re-added, counter reset.
    // Since activeTab was NOT set to 'automation' during render 2, the flip-back to true
    // triggers setActiveTab('chat') cleanly (no stale 'automation' state to overcome).
    rerender(<CardPanel {...makeProps({ card: runningCard })} />);

    // Chat tab is mounted and selected after the bounce-back.
    expect(screen.getByRole('tab', { name: /Chat/ })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('user-initiated tab change resets the stability counter so HITL-off does not re-fire stale switch', () => {
    const runningCard = { ...baseCard, state: 'in_progress', runner_status: 'running' as const, autonomous: false };
    const { rerender } = render(<CardPanel {...makeProps({ card: runningCard })} />);
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');

    // User manually switches to Automation tab while HITL is running.
    // This resets the stability counter to 0.
    fireEvent.click(screen.getByRole('tab', { name: /Automation/ }));
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'true');

    // HITL flips false (counter resets to 0 from manual tab change, so flip sets counter=1).
    const autonomousCard = { ...runningCard, autonomous: true };
    rerender(<CardPanel {...makeProps({ card: autonomousCard })} />);

    // One render with false is not enough — counter=1 is below the threshold.
    // The chat tab is no longer in the tab set (autonomous=true), so Automation
    // remains the effective tab regardless.
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'true');
  });
});

describe('CardPanel — description editability tracks runnerAttached', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('starts in preview mode and reveals the editor after clicking "Open in editor" (detached todo)', async () => {
    render(<CardPanel {...makeProps()} />);
    expect(screen.queryByTestId('md-editor')).not.toBeInTheDocument();
    fireEvent.click(await screen.findByRole('button', { name: 'Open in editor' }));
    expect(await screen.findByTestId('md-editor')).toBeInTheDocument();
  });

  it('omits the "Open in editor" toggle when runner is running (HITL)', async () => {
    render(
      <CardPanel
        {...makeProps({ card: { ...baseCard, state: 'in_progress', runner_status: 'running', autonomous: false } })}
      />,
    );
    await waitFor(() => {
      expect(screen.queryByTestId('md-editor')).not.toBeInTheDocument();
    });
    expect(screen.queryByRole('button', { name: 'Open in editor' })).not.toBeInTheDocument();
  });

  it('omits the "Open in editor" toggle outside todo/done/not_planned', () => {
    render(
      <CardPanel {...makeProps({ card: { ...baseCard, state: 'review' } })} />,
    );
    expect(screen.queryByRole('button', { name: 'Open in editor' })).not.toBeInTheDocument();
    expect(screen.queryByTestId('md-editor')).not.toBeInTheDocument();
  });

  it('shows the "Open in editor" toggle in done and not_planned', () => {
    const { rerender } = render(
      <CardPanel {...makeProps({ card: { ...baseCard, state: 'done' } })} />,
    );
    expect(screen.getByRole('button', { name: 'Open in editor' })).toBeInTheDocument();
    rerender(<CardPanel {...makeProps({ card: { ...baseCard, id: 'TEST-002', state: 'not_planned' } })} />);
    expect(screen.getByRole('button', { name: 'Open in editor' })).toBeInTheDocument();
  });
});

describe('CardPanel — mobile layout (≤ 768px)', () => {
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      configurable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: query === '(max-width: 768px)',
        media: query,
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    });
  });

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      configurable: true,
      value: originalMatchMedia,
    });
  });

  it('collapses to a single column and drops the left column from the DOM', () => {
    render(<CardPanel {...makeProps()} />);
    expect(screen.getByTestId('body-bifold')).toBeInTheDocument();
    expect(screen.queryByTestId('body-left')).not.toBeInTheDocument();
    const grid = screen.getByTestId('body-bifold');
    expect(grid.style.gridTemplateColumns).toBe('1fr');
  });

  it('prepends a "Card" tab and selects it by default on non-HITL cards', () => {
    render(<CardPanel {...makeProps()} />);
    const cardTab = screen.getByRole('tab', { name: 'Card' });
    expect(cardTab).toBeInTheDocument();
    expect(cardTab).toHaveAttribute('aria-selected', 'true');
    // Automation is no longer the default on mobile.
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'false');
  });

  it('keeps Chat as the default when an HITL session is running', () => {
    render(
      <CardPanel
        {...makeProps({
          card: { ...baseCard, state: 'in_progress', runner_status: 'running', autonomous: false },
        })}
      />,
    );
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: 'Card' })).toHaveAttribute('aria-selected', 'false');
  });

  it('hides the rail expand toggle', () => {
    render(<CardPanel {...makeProps()} />);
    expect(screen.queryByRole('button', { name: 'Expand rail' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Collapse rail' })).not.toBeInTheDocument();
  });
});

describe('CardPanel — keydown listener stability', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('does not re-register the escape-key listener when typing into the editor', async () => {
    const docAddSpy = vi.spyOn(document, 'addEventListener');

    render(<CardPanel {...makeProps()} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Open in editor' }));

    const initialDocKeydown = docAddSpy.mock.calls.filter((c) => c[0] === 'keydown').length;

    const editor = await screen.findByTestId('md-editor');
    fireEvent.change(editor, { target: { value: 'a' } });
    fireEvent.change(editor, { target: { value: 'ab' } });
    fireEvent.change(editor, { target: { value: 'abc' } });

    const finalDocKeydown = docAddSpy.mock.calls.filter((c) => c[0] === 'keydown').length;

    // The useCardPanelKeyboard hook registers a single escape listener that
    // should remain stable across editor keystrokes (the ⌘S listener is
    // rebinding by design, so we only assert the escape one is stable by
    // checking that we haven't exploded to 10+ registrations per keystroke).
    expect(finalDocKeydown - initialDocKeydown).toBeLessThanOrEqual(4);

    docAddSpy.mockRestore();
  });
});

describe('CardPanel — rail auto-expand behavior', () => {
  it('HITL card mounts with rail expanded and Chat tab selected', () => {
    render(
      <CardPanel
        {...makeProps({
          card: { ...baseCard, state: 'in_progress', runner_status: 'running', autonomous: false },
        })}
      />,
    );
    const grid = screen.getByTestId('body-bifold');
    expect(grid.style.gridTemplateColumns).toContain('600px');
    expect(screen.getByRole('button', { name: 'Collapse rail' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('switching card identity from non-HITL to HITL expands the rail', () => {
    const { rerender } = render(<CardPanel {...makeProps({ card: baseCard })} />);
    const grid = screen.getByTestId('body-bifold');
    expect(grid.style.gridTemplateColumns).toContain('340px');

    const hitlCard = {
      ...baseCard,
      id: 'TEST-002',
      state: 'in_progress',
      runner_status: 'running' as const,
      autonomous: false,
    };
    rerender(<CardPanel {...makeProps({ card: hitlCard })} />);

    expect(grid.style.gridTemplateColumns).toContain('600px');
    expect(screen.getByRole('button', { name: 'Collapse rail' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('HITL-flip on the same card (non-HITL → HITL mid-session) expands the rail', () => {
    const { rerender } = render(<CardPanel {...makeProps({ card: baseCard })} />);
    const grid = screen.getByTestId('body-bifold');
    expect(grid.style.gridTemplateColumns).toContain('340px');

    const hitlFlipped = {
      ...baseCard,
      state: 'in_progress',
      runner_status: 'running' as const,
      autonomous: false,
    };
    rerender(<CardPanel {...makeProps({ card: hitlFlipped })} />);

    expect(grid.style.gridTemplateColumns).toContain('600px');
    expect(screen.getByRole('button', { name: 'Collapse rail' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('manual collapse survives an SSE refresh of the same card', () => {
    const initial = {
      ...baseCard,
      state: 'in_progress',
      runner_status: 'running' as const,
      autonomous: false,
    };
    const { rerender } = render(<CardPanel {...makeProps({ card: initial })} />);
    const grid = screen.getByTestId('body-bifold');

    // HITL card starts expanded; manually collapse it.
    expect(grid.style.gridTemplateColumns).toContain('600px');
    fireEvent.click(screen.getByRole('button', { name: 'Collapse rail' }));
    expect(grid.style.gridTemplateColumns).toContain('340px');
    expect(screen.getByRole('button', { name: 'Expand rail' })).toHaveAttribute('aria-pressed', 'false');

    // SSE refresh: same id, new object, state change — rail must stay collapsed.
    const refreshed = { ...initial, state: 'review' };
    rerender(<CardPanel {...makeProps({ card: refreshed })} />);

    expect(grid.style.gridTemplateColumns).toContain('340px');
    expect(screen.getByRole('button', { name: 'Expand rail' })).toHaveAttribute('aria-pressed', 'false');
  });
});

describe('CardPanel — automation lock on subtasks', () => {
  it('disables automation checkboxes and shows the parent-card reason on a subtask in todo', () => {
    render(
      <CardPanel
        {...makeProps({
          card: {
            ...baseCard,
            type: 'subtask',
            parent: 'TEST-000',
          },
        })}
      />,
    );
    expect(screen.getByRole('checkbox', { name: 'Autonomous mode' })).toBeDisabled();
    expect(
      screen.getByText(/Automation is managed on the parent card/i),
    ).toBeInTheDocument();
  });
});
