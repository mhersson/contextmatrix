import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
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
  onClose: vi.fn(),
  onSave: vi.fn(),
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
