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
  states: ['todo', 'in_progress', 'done'],
  types: ['task'],
  priorities: ['low', 'medium', 'high'],
  transitions: { todo: ['in_progress'], in_progress: ['done'], done: [] },
};

const defaultProps = {
  card: baseCard,
  editedCard: baseCard,
  config: baseConfig,
  isDirty: false,
  isSaving: false,
  isDeleting: false,
  onClose: vi.fn(),
  onSave: vi.fn(),
  onDelete: vi.fn(),
  onTitleChange: vi.fn(),
  onPriorityChange: vi.fn(),
  onStateChange: vi.fn(),
};

// Suppress window.confirm in tests that trigger handleClose
vi.spyOn(window, 'confirm').mockReturnValue(false);

describe('CardPanelHeader — external_url scheme validation', () => {
  it('renders an <a> link for a safe https GitHub URL', () => {
    const card: Card = {
      ...baseCard,
      source: {
        system: 'github',
        external_id: '1',
        external_url: 'https://github.com/foo/bar/issues/1',
      },
    };
    render(<CardPanelHeader {...defaultProps} card={card} editedCard={card} />);
    const link = screen.getByRole('link');
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute('href', 'https://github.com/foo/bar/issues/1');
  });

  it('does not render an <a> link for a javascript: URL', () => {
    const card: Card = {
      ...baseCard,
      source: {
        system: 'github',
        external_id: '1',
        external_url: 'javascript:alert(1)',
      },
    };
    render(<CardPanelHeader {...defaultProps} card={card} editedCard={card} />);
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
  });

  it('does not render an <a> link for a data: URL', () => {
    const card: Card = {
      ...baseCard,
      source: {
        system: 'github',
        external_id: '1',
        external_url: 'data:text/html,<script>alert(1)</script>',
      },
    };
    render(<CardPanelHeader {...defaultProps} card={card} editedCard={card} />);
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
  });

  it('does not render an <a> link for a malformed / relative string', () => {
    const card: Card = {
      ...baseCard,
      source: {
        system: 'github',
        external_id: '1',
        external_url: '/relative/path',
      },
    };
    render(<CardPanelHeader {...defaultProps} card={card} editedCard={card} />);
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
  });
});

describe('CardPanelHeader — Delete button enabled/disabled states', () => {
  beforeEach(() => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);
  });

  it('Delete button is enabled for unclaimed todo card', () => {
    render(<CardPanelHeader {...defaultProps} card={{ ...baseCard, state: 'todo', assigned_agent: undefined }} />);
    expect(screen.getByRole('button', { name: 'Delete card' })).not.toBeDisabled();
  });

  it('Delete button is enabled for unclaimed not_planned card', () => {
    render(<CardPanelHeader {...defaultProps} card={{ ...baseCard, state: 'not_planned', assigned_agent: undefined }} />);
    expect(screen.getByRole('button', { name: 'Delete card' })).not.toBeDisabled();
  });

  it('Delete button is disabled when state is in_progress', () => {
    render(<CardPanelHeader {...defaultProps} card={{ ...baseCard, state: 'in_progress' }} />);
    expect(screen.getByRole('button', { name: 'Delete card' })).toBeDisabled();
  });

  it('Delete button is disabled when state is review', () => {
    render(<CardPanelHeader {...defaultProps} card={{ ...baseCard, state: 'review' }} />);
    expect(screen.getByRole('button', { name: 'Delete card' })).toBeDisabled();
  });

  it('Delete button is disabled when state is done', () => {
    render(<CardPanelHeader {...defaultProps} card={{ ...baseCard, state: 'done' }} />);
    expect(screen.getByRole('button', { name: 'Delete card' })).toBeDisabled();
  });

  it('Delete button is disabled when state is blocked', () => {
    render(<CardPanelHeader {...defaultProps} card={{ ...baseCard, state: 'blocked' }} />);
    expect(screen.getByRole('button', { name: 'Delete card' })).toBeDisabled();
  });

  it('Delete button is disabled when state is stalled', () => {
    render(<CardPanelHeader {...defaultProps} card={{ ...baseCard, state: 'stalled' }} />);
    expect(screen.getByRole('button', { name: 'Delete card' })).toBeDisabled();
  });

  it('Delete button is disabled when assigned_agent is set even if state is todo', () => {
    render(<CardPanelHeader {...defaultProps} card={{ ...baseCard, state: 'todo', assigned_agent: 'some-agent' }} />);
    expect(screen.getByRole('button', { name: 'Delete card' })).toBeDisabled();
  });

  it('disabled Delete button has a tooltip explaining the restriction', () => {
    render(<CardPanelHeader {...defaultProps} card={{ ...baseCard, state: 'in_progress' }} />);
    const btn = screen.getByRole('button', { name: 'Delete card' });
    expect(btn).toHaveAttribute('title');
    expect(btn.getAttribute('title')).toMatch(/unclaimed.*todo.*not_planned|Only unclaimed/i);
  });
});

describe('CardPanelHeader — Delete button click behavior', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('clicking enabled Delete button triggers window.confirm', () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    const onDelete = vi.fn();
    render(
      <CardPanelHeader
        {...defaultProps}
        card={{ ...baseCard, state: 'todo', assigned_agent: undefined }}
        onDelete={onDelete}
      />
    );
    fireEvent.click(screen.getByRole('button', { name: 'Delete card' }));
    expect(confirmSpy).toHaveBeenCalledOnce();
  });

  it('onDelete fires when confirm returns true', () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const onDelete = vi.fn();
    render(
      <CardPanelHeader
        {...defaultProps}
        card={{ ...baseCard, state: 'todo', assigned_agent: undefined }}
        onDelete={onDelete}
      />
    );
    fireEvent.click(screen.getByRole('button', { name: 'Delete card' }));
    expect(onDelete).toHaveBeenCalledOnce();
  });

  it('onDelete does NOT fire when confirm returns false', () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    const onDelete = vi.fn();
    render(
      <CardPanelHeader
        {...defaultProps}
        card={{ ...baseCard, state: 'todo', assigned_agent: undefined }}
        onDelete={onDelete}
      />
    );
    fireEvent.click(screen.getByRole('button', { name: 'Delete card' }));
    expect(onDelete).not.toHaveBeenCalled();
  });
});
