import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { CardPanelMetadata } from './CardPanelMetadata';
import type { Card, ProjectConfig } from '../../types';

vi.mock('../../api/client', () => ({
  api: {
    getCard: vi.fn().mockResolvedValue({ state: 'todo' }),
    getTaskSkills: vi.fn().mockResolvedValue([]),
  },
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
};

const subtaskCard: Card = {
  ...baseCard,
  id: 'TEST-002',
  type: 'subtask',
  parent: 'TEST-001',
};

const config: ProjectConfig = {
  name: 'test',
  prefix: 'TEST',
  next_id: 2,
  states: ['todo', 'in_progress', 'done'],
  types: ['task', 'subtask'],
  priorities: ['medium'],
  transitions: { todo: ['in_progress'], in_progress: ['done'], done: [] },
};

const defaultProps = {
  card: baseCard,
  editedCard: baseCard,
  config,
  currentAgentId: null,
  runnerAttached: false,
  onStateChange: vi.fn(),
  onSubtaskClick: vi.fn(),
  onClaim: vi.fn(),
  onRelease: vi.fn(),
  editedVetted: false,
  onVettedChange: vi.fn(),
  onSkillsChange: vi.fn(),
  excludeStateFromPicker: null,
};

describe('CardPanelMetadata — status section', () => {
  it('renders a state <select> with the current state first', () => {
    render(<CardPanelMetadata {...defaultProps} />);
    const select = screen.getByRole('combobox', { name: 'State' }) as HTMLSelectElement;
    expect(select).toBeInTheDocument();
    expect(select.value).toBe('todo');
  });

  it('includes valid transitions as "Move to …" options', () => {
    render(<CardPanelMetadata {...defaultProps} />);
    expect(screen.getByRole('option', { name: /→ in progress/ })).toBeInTheDocument();
  });

  it('excludes the state picked up by the curated primary button', () => {
    render(
      <CardPanelMetadata
        {...defaultProps}
        excludeStateFromPicker="in_progress"
      />,
    );
    expect(screen.queryByRole('option', { name: /→ in progress/ })).not.toBeInTheDocument();
  });

  it('disables the select when a runner is attached', () => {
    render(<CardPanelMetadata {...defaultProps} runnerAttached />);
    expect(screen.getByRole('combobox', { name: 'State' })).toBeDisabled();
  });
});

describe('CardPanelMetadata — agent section', () => {
  it('shows "unassigned" + "runner ready" hint and a Just claim button when no agent holds a todo card', () => {
    render(<CardPanelMetadata {...defaultProps} />);
    expect(screen.getByText('unassigned')).toBeInTheDocument();
    expect(screen.getByText(/runner ready/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Just claim' })).toBeInTheDocument();
  });

  it('calls onClaim when Just claim is clicked', () => {
    const onClaim = vi.fn();
    render(<CardPanelMetadata {...defaultProps} onClaim={onClaim} />);
    fireEvent.click(screen.getByRole('button', { name: 'Just claim' }));
    expect(onClaim).toHaveBeenCalledOnce();
  });

  it('shows the claimer and a Release button when runner is attached and current user is human', () => {
    const card: Card = { ...baseCard, assigned_agent: 'agent-1', last_heartbeat: '2026-01-01T00:00:30Z' };
    render(
      <CardPanelMetadata
        {...defaultProps}
        card={card}
        editedCard={card}
        currentAgentId="human:alice"
        runnerAttached
      />,
    );
    expect(screen.getByText('agent-1')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Release' })).toBeInTheDocument();
  });

  it('does not render the Just claim button when the runner is attached', () => {
    render(<CardPanelMetadata {...defaultProps} runnerAttached />);
    expect(screen.queryByRole('button', { name: 'Just claim' })).not.toBeInTheDocument();
  });

  it('does not render the Just claim button when an agent already holds the card', () => {
    const card: Card = { ...baseCard, assigned_agent: 'agent-1' };
    render(<CardPanelMetadata {...defaultProps} card={card} editedCard={card} />);
    expect(screen.queryByRole('button', { name: 'Just claim' })).not.toBeInTheDocument();
  });

  it('does NOT render the Release button when no currentAgentId is set', () => {
    const card: Card = { ...baseCard, assigned_agent: 'agent-1' };
    render(
      <CardPanelMetadata
        {...defaultProps}
        card={card}
        editedCard={card}
        runnerAttached
        currentAgentId={null}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Release' })).not.toBeInTheDocument();
  });

  it('does NOT render the Release button when currentAgentId is a non-human agent', () => {
    const card: Card = { ...baseCard, assigned_agent: 'agent-1' };
    render(
      <CardPanelMetadata
        {...defaultProps}
        card={card}
        editedCard={card}
        runnerAttached
        currentAgentId="agent:robot"
      />,
    );
    // Release is strictly a human-only override for forcing a claim off.
    expect(screen.queryByRole('button', { name: 'Release' })).not.toBeInTheDocument();
  });

  it('opens a ConfirmModal on Release click (does not fire onRelease yet)', () => {
    const onRelease = vi.fn();
    const card: Card = { ...baseCard, assigned_agent: 'agent-1' };
    render(
      <CardPanelMetadata
        {...defaultProps}
        card={card}
        editedCard={card}
        runnerAttached
        currentAgentId="human:alice"
        onRelease={onRelease}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Release' }));
    expect(onRelease).not.toHaveBeenCalled();
    // Modal appears with the release-specific copy.
    expect(screen.getByText(/Release claim held by agent-1/)).toBeInTheDocument();
  });
});

describe('CardPanelMetadata — parent section', () => {
  it('renders Parent section when card.parent is defined', () => {
    render(<CardPanelMetadata {...defaultProps} card={subtaskCard} editedCard={subtaskCard} />);
    expect(screen.getByText('Parent')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /TEST-001/ })).toBeInTheDocument();
  });

  it('does not render Parent section when card.parent is absent', () => {
    render(<CardPanelMetadata {...defaultProps} />);
    expect(screen.queryByText('Parent')).not.toBeInTheDocument();
  });

  it('calls onSubtaskClick with the parent card ID when the parent button is clicked', () => {
    const onSubtaskClick = vi.fn();
    render(
      <CardPanelMetadata
        {...defaultProps}
        card={subtaskCard}
        editedCard={subtaskCard}
        onSubtaskClick={onSubtaskClick}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /TEST-001/ }));
    expect(onSubtaskClick).toHaveBeenCalledWith('TEST-001');
  });
});

describe('CardPanelMetadata — source section', () => {
  it('renders the vetted checkbox when card.source is set', () => {
    const sourced: Card = {
      ...baseCard,
      source: { system: 'github', external_id: '42', external_url: 'https://example.com' },
    };
    render(<CardPanelMetadata {...defaultProps} card={sourced} editedCard={sourced} />);
    expect(screen.getByLabelText('Content vetted')).toBeInTheDocument();
  });

  it('does not render the vetted checkbox when card.source is absent', () => {
    render(<CardPanelMetadata {...defaultProps} />);
    expect(screen.queryByLabelText('Content vetted')).not.toBeInTheDocument();
  });
});
