import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { CardPanelHeader } from './CardPanelHeader';
import type { Card, ProjectConfig } from '../../types';

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
};

const baseConfig: ProjectConfig = {
  name: 'test',
  prefix: 'TEST',
  next_id: 2,
  states: ['todo', 'in_progress', 'review', 'done', 'blocked', 'stalled'],
  types: ['task'],
  priorities: ['low', 'medium', 'high'],
  transitions: {
    todo: ['in_progress', 'blocked', 'not_planned'],
    in_progress: ['review', 'blocked'],
    review: ['done', 'in_progress'],
    blocked: ['todo'],
    stalled: ['todo'],
    done: ['todo'],
  },
  remote_execution: { enabled: true },
};

const defaultProps = {
  card: baseCard,
  editedCard: baseCard,
  config: baseConfig,
  currentAgentId: null,
  isDirty: false,
  isSaving: false,
  isDeleting: false,
  canRun: true,
  onClose: vi.fn(),
  onSave: vi.fn(),
  onTitleChange: vi.fn(),
  onPriorityChange: vi.fn(),
  onPrimaryAction: vi.fn(),
  onStopCard: vi.fn().mockResolvedValue(undefined),
  onOpenDependency: vi.fn(),
  firstUnfinishedDep: null,
};

// Suppress window.confirm in tests that trigger handleClose
vi.spyOn(window, 'confirm').mockReturnValue(false);

describe('CardPanelHeader — external_url scheme validation', () => {
  it('renders an <a> link for a safe https GitHub URL', () => {
    const card: Card = {
      ...baseCard,
      source: { system: 'github', external_id: '1', external_url: 'https://github.com/foo/bar/issues/1' },
    };
    render(<CardPanelHeader {...defaultProps} card={card} editedCard={card} />);
    const link = screen.getByRole('link');
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute('href', 'https://github.com/foo/bar/issues/1');
  });

  it('does not render an <a> link for a javascript: URL', () => {
    const card: Card = {
      ...baseCard,
      source: { system: 'github', external_id: '1', external_url: 'javascript:alert(1)' },
    };
    render(<CardPanelHeader {...defaultProps} card={card} editedCard={card} />);
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
  });

  it('does not render an <a> link for a data: URL', () => {
    const card: Card = {
      ...baseCard,
      source: { system: 'github', external_id: '1', external_url: 'data:text/html,<script>alert(1)</script>' },
    };
    render(<CardPanelHeader {...defaultProps} card={card} editedCard={card} />);
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
  });
});

describe('CardPanelHeader — primary action by state', () => {
  it('renders Run HITL primary when state=todo and canRun', () => {
    render(<CardPanelHeader {...defaultProps} canRun card={{ ...baseCard, autonomous: false }} editedCard={{ ...baseCard, autonomous: false }} />);
    expect(screen.getByRole('button', { name: /Run HITL/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Run Auto/ })).not.toBeInTheDocument();
  });

  it('renders Run Auto primary when state=todo and autonomous=true', () => {
    const card = { ...baseCard, autonomous: true };
    render(<CardPanelHeader {...defaultProps} canRun card={card} editedCard={card} />);
    expect(screen.getByRole('button', { name: /Run Auto/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Run HITL/ })).not.toBeInTheDocument();
  });

  it('renders Mark done primary when state=review', () => {
    const card = { ...baseCard, state: 'review' };
    render(<CardPanelHeader {...defaultProps} canRun={false} card={card} editedCard={card} />);
    expect(screen.getByRole('button', { name: 'Mark done' })).toBeInTheDocument();
  });

  it('renders Unblock primary when state=blocked', () => {
    const card = { ...baseCard, state: 'blocked' };
    render(<CardPanelHeader {...defaultProps} canRun={false} card={card} editedCard={card} />);
    expect(screen.getByRole('button', { name: 'Unblock' })).toBeInTheDocument();
  });

  it('renders Resume primary when state=stalled', () => {
    const card = { ...baseCard, state: 'stalled' };
    render(<CardPanelHeader {...defaultProps} canRun={false} card={card} editedCard={card} />);
    expect(screen.getByRole('button', { name: 'Resume' })).toBeInTheDocument();
  });

  it('renders Re-open primary when state=done', () => {
    const card = { ...baseCard, state: 'done' };
    render(<CardPanelHeader {...defaultProps} canRun={false} card={card} editedCard={card} />);
    expect(screen.getByRole('button', { name: 'Re-open' })).toBeInTheDocument();
  });

  it('calls onPrimaryAction when primary button is clicked', () => {
    const onPrimaryAction = vi.fn();
    render(<CardPanelHeader {...defaultProps} canRun onPrimaryAction={onPrimaryAction} />);
    fireEvent.click(screen.getByRole('button', { name: /Run HITL/ }));
    expect(onPrimaryAction).toHaveBeenCalledOnce();
  });
});

describe('CardPanelHeader — workflow-safety lock', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  it('replaces the Save and primary cluster with a locked badge when runner is running', () => {
    const card = { ...baseCard, state: 'in_progress', runner_status: 'running' as const, assigned_agent: 'agent-1' };
    render(<CardPanelHeader {...defaultProps} card={card} editedCard={card} />);
    expect(screen.getByText(/Agent still owns this card/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Stop runner' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Save/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Run HITL/ })).not.toBeInTheDocument();
  });

  it('replaces the cluster when a non-current agent holds the claim', () => {
    const card = { ...baseCard, assigned_agent: 'other-agent' };
    render(<CardPanelHeader {...defaultProps} card={card} editedCard={card} currentAgentId={null} />);
    expect(screen.getByText(/Agent still owns this card/)).toBeInTheDocument();
  });

  it('does NOT lock when the current human holds the claim', () => {
    const card = { ...baseCard, assigned_agent: 'human:alice' };
    render(
      <CardPanelHeader
        {...defaultProps}
        card={card}
        editedCard={card}
        currentAgentId="human:alice"
      />,
    );
    expect(screen.queryByText(/Agent still owns this card/)).not.toBeInTheDocument();
  });
});

describe('CardPanelHeader — Save button', () => {
  it('is disabled when the panel is clean', () => {
    render(<CardPanelHeader {...defaultProps} isDirty={false} />);
    expect(screen.getByRole('button', { name: /Save/ })).toBeDisabled();
  });

  it('is enabled when the panel is dirty', () => {
    render(<CardPanelHeader {...defaultProps} isDirty={true} />);
    expect(screen.getByRole('button', { name: /Save/ })).not.toBeDisabled();
  });

  it('fires onSave when clicked while dirty', () => {
    const onSave = vi.fn();
    render(<CardPanelHeader {...defaultProps} isDirty onSave={onSave} />);
    fireEvent.click(screen.getByRole('button', { name: /Save/ }));
    expect(onSave).toHaveBeenCalledOnce();
  });
});

describe('CardPanelHeader — Open dependency helper', () => {
  it('renders the Open dependency button on blocked cards with unfinished deps', () => {
    const card = { ...baseCard, state: 'blocked' };
    render(
      <CardPanelHeader
        {...defaultProps}
        card={card}
        editedCard={card}
        firstUnfinishedDep="TEST-999"
      />,
    );
    expect(screen.getByRole('button', { name: 'Open dependency' })).toBeInTheDocument();
  });

  it('calls onOpenDependency with the dep id when clicked', () => {
    const onOpenDependency = vi.fn();
    const card = { ...baseCard, state: 'blocked' };
    render(
      <CardPanelHeader
        {...defaultProps}
        card={card}
        editedCard={card}
        firstUnfinishedDep="TEST-999"
        onOpenDependency={onOpenDependency}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Open dependency' }));
    expect(onOpenDependency).toHaveBeenCalledWith('TEST-999');
  });
});
