import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { CardPanel } from './CardPanel';
import type { Card, ProjectConfig } from '../../types';

vi.mock('../../hooks/useTheme', () => ({
  useTheme: () => ({ theme: 'dark', palette: 'everforest', toggleTheme: vi.fn() }),
}));

vi.mock('@uiw/react-md-editor', () => ({
  default: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <textarea
      data-testid="md-editor"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  ),
}));

vi.mock('../../api/client', () => ({
  api: {
    fetchBranches: vi.fn().mockResolvedValue([]),
  },
}));

// CardChat uses EventSource (SSE) which is not available in jsdom
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
  states: ['todo', 'in_progress', 'done'],
  types: ['task'],
  priorities: ['low', 'medium', 'high'],
  transitions: { todo: ['in_progress'], in_progress: ['done'] },
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
    ...overrides,
  };
}

function renderWithTheme(ui: React.ReactElement) {
  return render(ui);
}

describe('CardPanel — collapsible Description section', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('Description editor is visible by default', () => {
    renderWithTheme(<CardPanel {...makeProps()} />);
    expect(screen.getByTestId('md-editor')).toBeInTheDocument();
  });

  it('clicking the Description chevron hides the MDEditor', () => {
    renderWithTheme(<CardPanel {...makeProps()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Collapse description' }));
    expect(screen.queryByTestId('md-editor')).not.toBeInTheDocument();
  });

  it('clicking the Description chevron again shows the MDEditor', () => {
    renderWithTheme(<CardPanel {...makeProps()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Collapse description' }));
    fireEvent.click(screen.getByRole('button', { name: 'Expand description' }));
    expect(screen.getByTestId('md-editor')).toBeInTheDocument();
  });
});

describe('CardPanel — collapsible Automation section', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('Automation checkboxes are visible by default', () => {
    renderWithTheme(<CardPanel {...makeProps()} />);
    expect(screen.getByRole('checkbox', { name: 'Autonomous mode' })).toBeInTheDocument();
  });

  it('clicking the Automation chevron hides the AutomationCheckboxes', () => {
    renderWithTheme(<CardPanel {...makeProps()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Collapse automation' }));
    expect(screen.queryByRole('checkbox', { name: 'Autonomous mode' })).not.toBeInTheDocument();
  });

  it('clicking the Automation chevron again shows the AutomationCheckboxes', () => {
    renderWithTheme(<CardPanel {...makeProps()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Collapse automation' }));
    fireEvent.click(screen.getByRole('button', { name: 'Expand automation' }));
    expect(screen.getByRole('checkbox', { name: 'Autonomous mode' })).toBeInTheDocument();
  });
});

describe('CardPanel — collapsible Labels section', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('Labels input is visible by default', () => {
    renderWithTheme(<CardPanel {...makeProps()} />);
    expect(screen.getByPlaceholderText('Add label...')).toBeInTheDocument();
  });

  it('clicking the Labels chevron hides the labels input', () => {
    renderWithTheme(<CardPanel {...makeProps()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Collapse labels' }));
    expect(screen.queryByPlaceholderText('Add label...')).not.toBeInTheDocument();
  });

  it('clicking the Labels chevron again shows the labels input', () => {
    renderWithTheme(<CardPanel {...makeProps()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Collapse labels' }));
    fireEvent.click(screen.getByRole('button', { name: 'Expand labels' }));
    expect(screen.getByPlaceholderText('Add label...')).toBeInTheDocument();
  });
});

describe('CardPanel — auto-collapse on HITL runner_status transitions', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('transitions to HITL running collapses Description, Automation, and Labels', async () => {
    const { rerender } = renderWithTheme(
      <CardPanel {...makeProps({ card: { ...baseCard, runner_status: undefined, autonomous: false } })} />,
    );
    // All sections visible initially
    expect(screen.getByTestId('md-editor')).toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: 'Autonomous mode' })).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Add label...')).toBeInTheDocument();

    // Transition to HITL running (running AND NOT autonomous)
    await act(async () => {
      rerender(
        <CardPanel {...makeProps({ card: { ...baseCard, runner_status: 'running', autonomous: false } })} />,
      );
    });

    await waitFor(() => {
      expect(screen.queryByTestId('md-editor')).not.toBeInTheDocument();
      expect(screen.queryByRole('checkbox', { name: 'Autonomous mode' })).not.toBeInTheDocument();
      expect(screen.queryByPlaceholderText('Add label...')).not.toBeInTheDocument();
    });
  });

  it('after auto-collapse, clicking a chevron expands and subsequent re-renders while still in HITL running do NOT re-collapse', async () => {
    const { rerender } = renderWithTheme(
      <CardPanel {...makeProps({ card: { ...baseCard, runner_status: undefined, autonomous: false } })} />,
    );

    // Transition to HITL running — auto-collapses all three sections
    await act(async () => {
      rerender(
        <CardPanel {...makeProps({ card: { ...baseCard, runner_status: 'running', autonomous: false } })} />,
      );
    });

    await waitFor(() => {
      expect(screen.queryByTestId('md-editor')).not.toBeInTheDocument();
      expect(screen.queryByPlaceholderText('Add label...')).not.toBeInTheDocument();
    });

    // Manually expand Description
    fireEvent.click(screen.getByRole('button', { name: 'Expand description' }));
    expect(screen.getByTestId('md-editor')).toBeInTheDocument();

    // Manually expand Labels
    fireEvent.click(screen.getByRole('button', { name: 'Expand labels' }));
    expect(screen.getByPlaceholderText('Add label...')).toBeInTheDocument();

    // Another re-render while still HITL running — should NOT re-collapse either
    await act(async () => {
      rerender(
        <CardPanel {...makeProps({ card: { ...baseCard, runner_status: 'running', autonomous: false, title: 'updated title' } })} />,
      );
    });

    expect(screen.getByTestId('md-editor')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Add label...')).toBeInTheDocument();
  });

  it('transitions out of HITL running expands Description, Automation, and Labels', async () => {
    const { rerender } = renderWithTheme(
      <CardPanel {...makeProps({ card: { ...baseCard, runner_status: undefined, autonomous: false } })} />,
    );

    // Transition to HITL running
    await act(async () => {
      rerender(
        <CardPanel {...makeProps({ card: { ...baseCard, runner_status: 'running', autonomous: false } })} />,
      );
    });

    await waitFor(() => {
      expect(screen.queryByTestId('md-editor')).not.toBeInTheDocument();
      expect(screen.queryByPlaceholderText('Add label...')).not.toBeInTheDocument();
    });

    // Transition out of running
    await act(async () => {
      rerender(
        <CardPanel {...makeProps({ card: { ...baseCard, runner_status: 'failed', autonomous: false } })} />,
      );
    });

    await waitFor(() => {
      expect(screen.getByTestId('md-editor')).toBeInTheDocument();
      expect(screen.getByRole('checkbox', { name: 'Autonomous mode' })).toBeInTheDocument();
      expect(screen.getByPlaceholderText('Add label...')).toBeInTheDocument();
    });
  });
});

describe('CardPanel — split layout when HITL runner is active', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('renders split layout when runner_status is "running" and autonomous is false', () => {
    renderWithTheme(
      <CardPanel {...makeProps({ card: { ...baseCard, runner_status: 'running', autonomous: false, state: 'in_progress' } })} />,
    );
    expect(screen.getByTestId('body-split')).toBeInTheDocument();
    expect(screen.getByTestId('body-top-section')).toBeInTheDocument();
    expect(screen.getByTestId('body-chat-region')).toBeInTheDocument();
    // They must be siblings (both children of body-split)
    const split = screen.getByTestId('body-split');
    const top = screen.getByTestId('body-top-section');
    const chat = screen.getByTestId('body-chat-region');
    expect(split.children).toHaveLength(2);
    expect(split.children[0]).toBe(top);
    expect(split.children[1]).toBe(chat);
  });

  it('renders single-scroll layout when runner_status is "running" and autonomous is true', () => {
    renderWithTheme(
      <CardPanel {...makeProps({ card: { ...baseCard, runner_status: 'running', autonomous: true, state: 'in_progress' } })} />,
    );
    expect(screen.getByTestId('body-single')).toBeInTheDocument();
    expect(screen.queryByTestId('body-split')).not.toBeInTheDocument();
    expect(screen.queryByTestId('body-top-section')).not.toBeInTheDocument();
    expect(screen.queryByTestId('body-chat-region')).not.toBeInTheDocument();
  });

  it('renders single-scroll layout when runner_status is not "running"', () => {
    renderWithTheme(<CardPanel {...makeProps()} />);
    expect(screen.getByTestId('body-single')).toBeInTheDocument();
    expect(screen.queryByTestId('body-split')).not.toBeInTheDocument();
    expect(screen.queryByTestId('body-top-section')).not.toBeInTheDocument();
    expect(screen.queryByTestId('body-chat-region')).not.toBeInTheDocument();
  });

  it('chat region contains the CardChat mock when runner_status is "running" and autonomous is false', () => {
    renderWithTheme(
      <CardPanel {...makeProps({ card: { ...baseCard, runner_status: 'running', autonomous: false, state: 'in_progress' } })} />,
    );
    const chatRegion = screen.getByTestId('body-chat-region');
    expect(chatRegion.querySelector('[data-testid="card-chat-mock"]')).not.toBeNull();
  });

  it('chat region is absent when runner_status is "running" and autonomous is true', () => {
    renderWithTheme(
      <CardPanel {...makeProps({ card: { ...baseCard, runner_status: 'running', autonomous: true, state: 'in_progress' } })} />,
    );
    expect(screen.queryByTestId('card-chat-mock')).not.toBeInTheDocument();
  });

  it('chat region is absent when runner_status is not "running"', () => {
    renderWithTheme(<CardPanel {...makeProps()} />);
    expect(screen.queryByTestId('card-chat-mock')).not.toBeInTheDocument();
  });

  it('promoting mid-run (autonomous false→true) switches layout from split to single and expands Description, Automation, and Labels', async () => {
    const { rerender } = renderWithTheme(
      <CardPanel {...makeProps({ card: { ...baseCard, runner_status: 'running', autonomous: false, state: 'in_progress' } })} />,
    );

    // Initially in HITL running — split layout, all three sections collapsed
    expect(screen.getByTestId('body-split')).toBeInTheDocument();
    // Auto-collapsed on mount since it starts in HITL-running state
    expect(screen.queryByTestId('md-editor')).not.toBeInTheDocument();
    expect(screen.queryByRole('checkbox', { name: 'Autonomous mode' })).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText('Add label...')).not.toBeInTheDocument();

    // Promote to autonomous mid-run
    await act(async () => {
      rerender(
        <CardPanel {...makeProps({ card: { ...baseCard, runner_status: 'running', autonomous: true, state: 'in_progress' } })} />,
      );
    });

    await waitFor(() => {
      // Layout switches to single-body
      expect(screen.getByTestId('body-single')).toBeInTheDocument();
      expect(screen.queryByTestId('body-split')).not.toBeInTheDocument();
      // Description, Automation, and Labels are all expanded
      expect(screen.getByTestId('md-editor')).toBeInTheDocument();
      expect(screen.getByRole('checkbox', { name: 'Autonomous mode' })).toBeInTheDocument();
      expect(screen.getByPlaceholderText('Add label...')).toBeInTheDocument();
    });
  });
});

describe('CardPanel — Run Now save-before-run ordering', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('calls onSave before onRunCard when card is dirty', async () => {
    const calls: string[] = [];
    const onSave = vi.fn(async () => { calls.push('save'); });
    const onRunCard = vi.fn(async () => { calls.push('run'); });

    renderWithTheme(<CardPanel {...makeProps({ onSave, onRunCard })} />);

    // Make the card dirty by changing the title
    const titleInput = screen.getByDisplayValue('Test card');
    fireEvent.change(titleInput, { target: { value: 'Dirty title' } });

    // Click Run Now
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run HITL' }));
    });

    expect(calls).toEqual(['save', 'run']);
  });

  it('does NOT call onSave when card is not dirty', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const onRunCard = vi.fn().mockResolvedValue(undefined);

    renderWithTheme(<CardPanel {...makeProps({ onSave, onRunCard })} />);

    // No changes made — card is clean
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Run HITL' }));
    });

    expect(onSave).not.toHaveBeenCalled();
    expect(onRunCard).toHaveBeenCalledOnce();
  });

});
