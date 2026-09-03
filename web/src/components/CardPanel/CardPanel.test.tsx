import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { CardPanel } from './CardPanel';
import type { Card, ProjectConfig } from '../../types';

const theme = vi.hoisted(() => ({
  theme: 'dark',
  palette: 'everforest',
  setTheme: () => {},
  setPalette: () => {},
  taskBackend: 'agent',
  instanceId: '',
  sharedBoards: false,
}));
vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => theme,
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

const useModelCatalogMock = vi.hoisted(() => vi.fn());
vi.mock('../../hooks/useModelCatalog', () => ({
  useModelCatalog: useModelCatalogMock.mockReturnValue({
    source: 'none',
    models: [],
  }),
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
};

function makeProps(overrides?: Partial<Parameters<typeof CardPanel>[0]>) {
  return {
    card: baseCard,
    config,
    cards: [baseCard],
    onClose: vi.fn(),
    onSave: vi.fn().mockResolvedValue(undefined),
    onClaim: vi.fn().mockResolvedValue(undefined),
    onRelease: vi.fn().mockResolvedValue(undefined),
    onSubtaskClick: vi.fn(),
    currentAgentId: 'human:web-test1234',
    onRunCard: vi.fn().mockResolvedValue(undefined),
    onStopCard: vi.fn().mockResolvedValue(undefined),
    onDelete: vi.fn().mockResolvedValue(undefined),
    onForceRelease: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

describe('CardPanel - blacklisted model marker in the pin pickers', () => {
  const catalog = [
    { id: 'vendor/good-model', max_tokens: 200000 },
    { id: 'vendor/bad-model', max_tokens: 100000, blacklisted: true },
  ];

  beforeEach(() => {
    useModelCatalogMock.mockReturnValue({ source: 'none', models: [] });
  });

  it('renders the marker for a flagged pin only', () => {
    useModelCatalogMock.mockReturnValue({ source: 'openrouter', models: catalog });
    render(
      <CardPanel
        {...makeProps({ card: { ...baseCard, model_orchestrator: 'vendor/bad-model' } })}
      />,
    );
    expect(screen.getByTitle(/reported incapable/i)).toBeInTheDocument();
  });

  it('renders no marker for an unflagged pin', () => {
    useModelCatalogMock.mockReturnValue({ source: 'openrouter', models: catalog });
    render(
      <CardPanel
        {...makeProps({ card: { ...baseCard, model_orchestrator: 'vendor/good-model' } })}
      />,
    );
    expect(screen.queryByTitle(/reported incapable/i)).not.toBeInTheDocument();
  });

  it('keeps a blacklisted model selectable via the combobox', () => {
    useModelCatalogMock.mockReturnValue({ source: 'openrouter', models: catalog });
    render(
      <CardPanel
        {...makeProps({ card: { ...baseCard, model_orchestrator: 'vendor/good-model' } })}
      />,
    );
    const orchestrator = screen.getByLabelText('Orchestrator model pin');
    fireEvent.change(orchestrator, { target: { value: 'vendor/bad-model' } });
    fireEvent.blur(orchestrator);
    expect(orchestrator).toHaveValue('vendor/bad-model');
    expect(screen.getByTitle(/reported incapable/i)).toBeInTheDocument();
  });
});

describe('CardPanel - bifold layout', () => {
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
          card: { ...baseCard, state: 'in_progress', worker_status: 'running', autonomous: false },
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
            worker_status: 'running',
            autonomous: false,
          },
        })}
      />,
    );
    expect(screen.queryByRole('tab', { name: /Chat/ })).not.toBeInTheDocument();
  });

  it('default tab is Automation when the worker is not running HITL', () => {
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
    const initial = { ...baseCard, state: 'in_progress', worker_status: 'running' as const, autonomous: false };
    const { rerender } = render(<CardPanel {...makeProps({ card: initial })} />);
    const grid = screen.getByTestId('body-bifold');

    // HITL cards auto-expand the rail on mount - no manual click needed.
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

    // Clear the stored preference so the new card starts with no stored state,
    // which is what this test scenario is testing (no preference → collapsed).
    localStorage.removeItem?.('contextmatrix-rail-expanded');

    // Switching to a different card (new id) is the only path that should
    // collapse the rail.
    const other = { ...baseCard, id: 'TEST-002', title: 'Other card' };
    rerender(<CardPanel {...makeProps({ card: other })} />);

    expect(grid.style.gridTemplateColumns).toContain('340px');
    expect(screen.getByRole('button', { name: 'Expand rail' })).toHaveAttribute('aria-pressed', 'false');
  });
});

describe('CardPanel - full-width rail', () => {
  it('drops the left column and makes the body a single column', () => {
    render(<CardPanel {...makeProps()} />);
    expect(screen.getByTestId('body-left')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Full width' }));

    expect(screen.queryByTestId('body-left')).not.toBeInTheDocument();
    expect(screen.getByTestId('body-bifold').style.gridTemplateColumns).toBe('1fr');
  });

  // The drawer's own width never changes; the modifier exists so the rail can
  // drop the darker tint it wears when there is a left column beside it.
  it('marks the panel full-width so the rail sheds its two-column tint', () => {
    render(<CardPanel {...makeProps()} />);
    const panel = screen.getByRole('dialog', { name: 'Card details' });
    expect(panel).not.toHaveClass('card-panel-bifold--full');

    fireEvent.click(screen.getByRole('button', { name: 'Full width' }));

    expect(panel).toHaveClass('card-panel-bifold--full');
  });

  it('restores the previous rail width when full width is exited', () => {
    render(<CardPanel {...makeProps()} />);
    const grid = screen.getByTestId('body-bifold');

    fireEvent.click(screen.getByRole('button', { name: 'Expand rail' }));
    expect(grid.style.gridTemplateColumns).toContain('600px');

    fireEvent.click(screen.getByRole('button', { name: 'Full width' }));
    fireEvent.click(screen.getByRole('button', { name: 'Exit full width' }));

    expect(grid.style.gridTemplateColumns).toContain('600px');
    expect(screen.getByTestId('body-left')).toBeInTheDocument();
  });

  it('hides the collapse/expand chevron while full width, since there is no left column', () => {
    render(<CardPanel {...makeProps()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Full width' }));

    expect(screen.queryByRole('button', { name: 'Expand rail' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Collapse rail' })).not.toBeInTheDocument();
  });

  it('does not carry full width over to the next card opened', () => {
    const { rerender } = render(<CardPanel {...makeProps({ card: baseCard })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Full width' }));
    expect(screen.queryByTestId('body-left')).not.toBeInTheDocument();

    const other = { ...baseCard, id: 'TEST-002', title: 'Other card' };
    rerender(<CardPanel {...makeProps({ card: other })} />);

    expect(screen.getByTestId('body-left')).toBeInTheDocument();
    expect(screen.getByTestId('body-bifold').style.gridTemplateColumns).toContain('340px');
  });

  // The flip-on branch expands the rail to give the live chat room. Full width
  // already gives it more than expanded does, so a session going live must not
  // undo a width the user chose deliberately - hitting Run is the moment the
  // big transcript matters most.
  it('keeps full width when an interactive chat session goes live', () => {
    const { rerender } = render(<CardPanel {...makeProps({ card: baseCard })} />);
    fireEvent.click(screen.getByRole('button', { name: 'Full width' }));

    const live = {
      ...baseCard,
      state: 'in_progress',
      worker_status: 'running' as const,
      autonomous: false,
    };
    rerender(<CardPanel {...makeProps({ card: live })} />);

    expect(screen.queryByTestId('body-left')).not.toBeInTheDocument();
    expect(screen.getByTestId('body-bifold').style.gridTemplateColumns).toBe('1fr');
    // The rest of the flip-on behaviour is unchanged: chat still takes focus.
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('still expands a collapsed rail when an interactive chat session goes live', () => {
    const { rerender } = render(<CardPanel {...makeProps({ card: baseCard })} />);
    expect(screen.getByTestId('body-bifold').style.gridTemplateColumns).toContain('340px');

    const live = {
      ...baseCard,
      state: 'in_progress',
      worker_status: 'running' as const,
      autonomous: false,
    };
    rerender(<CardPanel {...makeProps({ card: live })} />);

    expect(screen.getByTestId('body-bifold').style.gridTemplateColumns).toContain('600px');
  });
});

describe('CardPanel - Info tab hosts the state picker', () => {
  it('switches to Info and reveals the State select', async () => {
    render(<CardPanel {...makeProps()} />);
    fireEvent.click(screen.getByRole('tab', { name: 'Info' }));
    expect(await screen.findByRole('combobox', { name: 'State' })).toBeInTheDocument();
  });
});

describe('CardPanel - autonomous toggle leaves base branch alone', () => {
  it('keeps the selected base branch when autonomous is unchecked', async () => {
    const { api } = await import('../../api/client');
    vi.mocked(api.fetchBranches).mockResolvedValueOnce(['develop']);

    render(<CardPanel {...makeProps({
      card: { ...baseCard, autonomous: true, base_branch: 'develop' },
    })} />);

    const select = await screen.findByRole('combobox', { name: 'Base branch' });
    await waitFor(() => expect(select).toHaveValue('develop'));

    fireEvent.click(screen.getByLabelText('Autonomous mode'));

    expect(screen.getByLabelText('Autonomous mode')).not.toBeChecked();
    expect(select).toHaveValue('develop');
  });
});

describe('CardPanel - Run handler (save-before-run)', () => {
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

  it('Run does not patch automation flags - only the user edits are saved', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const onRunCard = vi.fn().mockResolvedValue(undefined);

    render(<CardPanel {...makeProps({ onSave, onRunCard })} />);
    const titleInput = screen.getByDisplayValue('Test card');
    fireEvent.change(titleInput, { target: { value: 'Dirty title' } });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Run HITL/ }));
    });

    expect(onSave).toHaveBeenCalledOnce();
    const patch = onSave.mock.calls[0][0];
    expect(patch).toEqual(expect.objectContaining({ title: 'Dirty title' }));
    expect(patch).not.toHaveProperty('create_pr');
  });

  it('does NOT call onSave when the card is clean', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const onRunCard = vi.fn().mockResolvedValue(undefined);
    render(<CardPanel {...makeProps({ onSave, onRunCard })} />);

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Run HITL/ }));
    });

    expect(onSave).not.toHaveBeenCalled();
    expect(onRunCard).toHaveBeenCalledOnce();
  });

  it('aborts the run when saving pending edits fails', async () => {
    const onSave = vi.fn().mockRejectedValue({ error: 'save failed' });
    const onRunCard = vi.fn().mockResolvedValue(undefined);
    render(<CardPanel {...makeProps({ onSave, onRunCard })} />);
    const titleInput = screen.getByDisplayValue('Test card');
    fireEvent.change(titleInput, { target: { value: 'Dirty title' } });

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Run HITL/ }));
    });

    expect(onSave).toHaveBeenCalledOnce();
    expect(onRunCard).not.toHaveBeenCalled();

    // The card is still dirty relative to the server, so a second Run
    // attempts a fresh save.
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Run HITL/ }));
    });
    expect(onSave).toHaveBeenCalledTimes(2);
  });
});

describe('CardPanel - run gating on global task backend', () => {
  afterEach(() => {
    theme.taskBackend = 'agent';
  });

  it('offers Run HITL when a task backend is configured', () => {
    render(<CardPanel {...makeProps()} />);
    expect(screen.getByRole('button', { name: /Run HITL/ })).toBeInTheDocument();
  });

  it('hides the Run action when no task backend is configured', () => {
    theme.taskBackend = '';
    render(<CardPanel {...makeProps()} />);
    expect(screen.queryByRole('button', { name: /Run HITL/ })).not.toBeInTheDocument();
  });

  // A parked run leaves worker_status "parked" and nothing clears it on the
  // way back to todo; the run gate must keep such a card re-runnable.
  it.each(['parked', 'failed', 'killed'] as const)(
    'offers Run for a todo card whose last run settled as %s',
    (workerStatus) => {
      render(
        <CardPanel
          {...makeProps()}
          card={{ ...baseCard, worker_status: workerStatus }}
        />,
      );
      expect(screen.getByRole('button', { name: /Run HITL/ })).toBeInTheDocument();
    },
  );
});

describe('CardPanel - transition primary rollback', () => {
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

    // Info tab reveals the state picker - confirm it reverted to 'review'.
    fireEvent.click(screen.getByRole('tab', { name: 'Info' }));
    const stateSelect = (await screen.findByRole(
      'combobox', { name: 'State' },
    )) as HTMLSelectElement;
    expect(stateSelect.value).toBe('review');
  });
});

describe('CardPanel - Delete via Danger Zone', () => {
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

describe('CardPanel - MDEditor preview skipHtml XSS prevention', () => {
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
    // Assert by inspecting every anchor in the DOM - if the real renderer
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

describe('CardPanel - rail default tab follows isChatInteractive', () => {
  it('mounts on Chat when the card arrives already running an HITL session', () => {
    render(
      <CardPanel
        {...makeProps({
          card: { ...baseCard, state: 'in_progress', worker_status: 'running', autonomous: false },
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
          card: { ...baseCard, state: 'in_progress', worker_status: 'running', autonomous: false },
        })}
      />,
    );
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('switches the active tab back to Automation when HITL is promoted to autonomous (two consecutive renders)', () => {
    const runningCard = { ...baseCard, state: 'in_progress', worker_status: 'running' as const, autonomous: false };
    const autonomousCard = { ...baseCard, state: 'in_progress', worker_status: 'running' as const, autonomous: true };
    const { rerender } = render(<CardPanel {...makeProps({ card: runningCard })} />);
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');

    // Flip render (isChatInteractive true→false): counter resets, chat stays selected.
    rerender(<CardPanel {...makeProps({ card: autonomousCard })} />);
    // Two consecutive renders both observing false: counter reaches 2, switch fires.
    rerender(<CardPanel {...makeProps({ card: { ...autonomousCard, updated: '2026-01-02T00:00:00Z' } })} />);
    rerender(<CardPanel {...makeProps({ card: { ...autonomousCard, updated: '2026-01-03T00:00:00Z' } })} />);

    // The chat tab stays in the tab set (the worker is still running, now
    // read-only) but focus moves back to Automation.
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'false');
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('does NOT switch activeTab to Automation on first render after isChatInteractive flips false (counter=1 guard)', () => {
    // Verifies the debounce: a single flip to false does NOT call setActiveTab(defaultTab).
    // After the flip back to true, the chat tab remains selected (no stale automation state).
    const runningCard = { ...baseCard, state: 'in_progress', worker_status: 'running' as const, autonomous: false };
    const { rerender } = render(<CardPanel {...makeProps({ card: runningCard })} />);
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');

    // Render 2: transient HITL=false (SSE lag). Chat stays in the tab set
    // (still running), and activeTab must NOT be set to 'automation' by the sync block.
    const autonomousCard = { ...runningCard, autonomous: true };
    rerender(<CardPanel {...makeProps({ card: autonomousCard })} />);
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');

    // Render 3: HITL flips back to true (SSE corrects), counter reset.
    // Since activeTab was NOT set to 'automation' during render 2, the flip-back to true
    // triggers setActiveTab('chat') cleanly (no stale 'automation' state to overcome).
    rerender(<CardPanel {...makeProps({ card: runningCard })} />);

    // Chat tab is mounted and selected after the bounce-back.
    expect(screen.getByRole('tab', { name: /Chat/ })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('user-initiated tab change resets the stability counter so HITL-off does not re-fire stale switch', () => {
    const runningCard = { ...baseCard, state: 'in_progress', worker_status: 'running' as const, autonomous: false };
    const { rerender } = render(<CardPanel {...makeProps({ card: runningCard })} />);
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');

    // User manually switches to Automation tab while HITL is running.
    // This resets the stability counter to 0.
    fireEvent.click(screen.getByRole('tab', { name: /Automation/ }));
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'true');

    // HITL flips false (counter resets to 0 from manual tab change, so flip sets counter=1).
    const autonomousCard = { ...runningCard, autonomous: true };
    rerender(<CardPanel {...makeProps({ card: autonomousCard })} />);

    // One render with false is not enough - counter=1 is below the threshold.
    // Automation remains the selected tab; the chat tab stays available read-only.
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: /Chat/ })).toBeInTheDocument();
  });
});

describe('CardPanel - autonomous chat visibility', () => {
  it('adds the Chat tab with a live pulse but does NOT select it by default for a running autonomous mob session card', () => {
    render(
      <CardPanel
        {...makeProps({
          card: {
            ...baseCard,
            state: 'in_progress',
            worker_status: 'running',
            autonomous: true,
            mob_participants: 3,
          },
        })}
      />,
    );
    const chatTab = screen.getByRole('tab', { name: /Chat/ });
    expect(chatTab).toHaveAttribute('aria-selected', 'false');
    expect(chatTab.querySelector('.animate-pulse')).not.toBeNull();
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('keeps the Chat tab selected across SSE card refreshes while an autonomous run is live (debounce stays disarmed)', () => {
    const autoCard = {
      ...baseCard,
      state: 'in_progress',
      worker_status: 'running' as const,
      autonomous: true,
    };
    const { rerender } = render(<CardPanel {...makeProps({ card: autoCard })} />);

    // User deliberately opens the read-only chat of the autonomous run.
    fireEvent.click(screen.getByRole('tab', { name: /Chat/ }));
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');

    // SSE churn: every log append / heartbeat hands CardPanel a new card
    // object. isChatInteractive never flipped (always false), so the
    // switch-back debounce must never arm - the user stays on Chat.
    for (const updated of ['2026-01-01T00:01:00Z', '2026-01-01T00:02:00Z', '2026-01-01T00:03:00Z']) {
      rerender(<CardPanel {...makeProps({ card: { ...autoCard, updated } })} />);
    }
    expect(screen.getByRole('tab', { name: /Chat/ })).toHaveAttribute('aria-selected', 'true');
  });

  it('adds the Chat tab with a live pulse, unfocused, for a running autonomous card without mob session discussion', () => {
    render(
      <CardPanel
        {...makeProps({
          card: {
            ...baseCard,
            state: 'in_progress',
            worker_status: 'running',
            autonomous: true,
            mob_participants: 0,
          },
        })}
      />,
    );
    const chatTab = screen.getByRole('tab', { name: /Chat/ });
    expect(chatTab).toHaveAttribute('aria-selected', 'false');
    expect(chatTab.querySelector('.animate-pulse')).not.toBeNull();
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'true');
  });
});

describe('CardPanel - description editability tracks workerAttached', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('starts in preview mode and reveals the editor after clicking "Open in editor" (detached todo)', async () => {
    render(<CardPanel {...makeProps()} />);
    expect(screen.queryByTestId('md-editor')).not.toBeInTheDocument();
    fireEvent.click(await screen.findByRole('button', { name: 'Open in editor' }));
    expect(await screen.findByTestId('md-editor')).toBeInTheDocument();
  });

  it('omits the "Open in editor" toggle when worker is running (HITL)', async () => {
    render(
      <CardPanel
        {...makeProps({ card: { ...baseCard, state: 'in_progress', worker_status: 'running', autonomous: false } })}
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

describe('CardPanel - mobile layout (≤ 768px)', () => {
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
    // On mobile, Card is the default tab, not Automation.
    expect(screen.getByRole('tab', { name: /Automation/ })).toHaveAttribute('aria-selected', 'false');
  });

  it('keeps Chat as the default when an HITL session is running', () => {
    render(
      <CardPanel
        {...makeProps({
          card: { ...baseCard, state: 'in_progress', worker_status: 'running', autonomous: false },
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

  it('hides the full-width toggle, which is meaningless on a single column', () => {
    render(<CardPanel {...makeProps()} />);
    expect(screen.queryByRole('button', { name: 'Full width' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Exit full width' })).not.toBeInTheDocument();
  });
});

describe('CardPanel - keydown listener stability', () => {
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

describe('CardPanel - rail auto-expand behavior', () => {
  it('HITL card mounts with rail expanded and Chat tab selected', () => {
    render(
      <CardPanel
        {...makeProps({
          card: { ...baseCard, state: 'in_progress', worker_status: 'running', autonomous: false },
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
      worker_status: 'running' as const,
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
      worker_status: 'running' as const,
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
      worker_status: 'running' as const,
      autonomous: false,
    };
    const { rerender } = render(<CardPanel {...makeProps({ card: initial })} />);
    const grid = screen.getByTestId('body-bifold');

    // HITL card starts expanded; manually collapse it.
    expect(grid.style.gridTemplateColumns).toContain('600px');
    fireEvent.click(screen.getByRole('button', { name: 'Collapse rail' }));
    expect(grid.style.gridTemplateColumns).toContain('340px');
    expect(screen.getByRole('button', { name: 'Expand rail' })).toHaveAttribute('aria-pressed', 'false');

    // SSE refresh: same id, new object, state change - rail must stay collapsed.
    const refreshed = { ...initial, state: 'review' };
    rerender(<CardPanel {...makeProps({ card: refreshed })} />);

    expect(grid.style.gridTemplateColumns).toContain('340px');
    expect(screen.getByRole('button', { name: 'Expand rail' })).toHaveAttribute('aria-pressed', 'false');
  });
});

describe('CardPanel - automation lock on subtasks', () => {
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

describe('CardPanel - close hardening', () => {
  it('closes on mousedown + mouseup both landing on the backdrop', () => {
    const onClose = vi.fn();
    render(<CardPanel {...makeProps({ onClose })} />);
    const backdrop = screen.getByTestId('card-panel-backdrop');

    fireEvent.mouseDown(backdrop);
    fireEvent.mouseUp(backdrop);

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('does not close on drag-out (mousedown inside dialog, mouseup on backdrop)', () => {
    const onClose = vi.fn();
    render(<CardPanel {...makeProps({ onClose })} />);
    const backdrop = screen.getByTestId('card-panel-backdrop');

    fireEvent.mouseDown(screen.getByRole('dialog'));
    fireEvent.mouseUp(backdrop);

    expect(onClose).not.toHaveBeenCalled();
  });

  it('does not close on an orphan mouseup with no backdrop mousedown', () => {
    const onClose = vi.fn();
    render(<CardPanel {...makeProps({ onClose })} />);

    fireEvent.mouseUp(screen.getByTestId('card-panel-backdrop'));

    expect(onClose).not.toHaveBeenCalled();
  });

  it('an aborted backdrop press does not arm a later drawer-to-backdrop release', () => {
    const onClose = vi.fn();
    render(<CardPanel {...makeProps({ onClose })} />);
    const backdrop = screen.getByTestId('card-panel-backdrop');
    const dialog = screen.getByRole('dialog');

    // Press on backdrop, release over the drawer - abort, no close.
    fireEvent.mouseDown(backdrop);
    fireEvent.mouseUp(dialog);
    expect(onClose).not.toHaveBeenCalled();

    // Text-selection drag: press in the drawer, release over the backdrop.
    // The stale flag from the aborted press must not close the panel.
    fireEvent.mouseDown(dialog);
    fireEvent.mouseUp(backdrop);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('right/middle click on the backdrop does not close the panel', () => {
    const onClose = vi.fn();
    render(<CardPanel {...makeProps({ onClose })} />);
    const backdrop = screen.getByTestId('card-panel-backdrop');

    fireEvent.mouseDown(backdrop, { button: 2 });
    fireEvent.mouseUp(backdrop, { button: 2 });
    fireEvent.mouseDown(backdrop, { button: 1 });
    fireEvent.mouseUp(backdrop, { button: 1 });

    expect(onClose).not.toHaveBeenCalled();
  });

  it('puts initial focus on the dialog surface, not the Close button', () => {
    render(<CardPanel {...makeProps()} />);

    expect(document.activeElement).toBe(screen.getByRole('dialog'));
    expect(screen.getByLabelText('Close panel')).not.toHaveFocus();
  });

  it('keeps Escape-to-close working', () => {
    const onClose = vi.fn();
    render(<CardPanel {...makeProps({ onClose })} />);

    fireEvent.keyDown(document, { key: 'Escape' });

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('Shift+Tab from the focused dialog surface stays inside the dialog', () => {
    render(<CardPanel {...makeProps()} />);
    const dialog = screen.getByRole('dialog');
    expect(document.activeElement).toBe(dialog);

    fireEvent.keyDown(dialog, { key: 'Tab', shiftKey: true });

    expect(dialog.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).not.toBe(document.body);
  });
});
