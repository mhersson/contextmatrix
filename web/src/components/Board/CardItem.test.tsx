import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { CardItem } from './CardItem';
import type { Card } from '../../types';

// @dnd-kit requires a DndContext — provide a minimal mock
vi.mock('@dnd-kit/core', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@dnd-kit/core')>();
  return {
    ...actual,
    useDraggable: () => ({
      attributes: {},
      listeners: {},
      setNodeRef: () => {},
      transform: null,
      isDragging: false,
    }),
  };
});

const baseCard: Card = {
  id: 'TEST-001',
  title: 'Test card title',
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

describe('CardItem — parent ID badge', () => {
  describe('expanded view (isCollapsed=false)', () => {
    it('renders parent badge when card.parent is defined', () => {
      render(<CardItem card={subtaskCard} />);
      expect(screen.getByTitle('Parent: TEST-001')).toBeInTheDocument();
      expect(screen.getByTitle('Parent: TEST-001')).toHaveTextContent('TEST-001');
    });

    it('does not render parent badge when card.parent is absent', () => {
      render(<CardItem card={baseCard} />);
      expect(screen.queryByTitle(/^Parent:/)).not.toBeInTheDocument();
    });

    it('calls onParentClick with the parent ID when badge is clicked', () => {
      const onParentClick = vi.fn();
      render(<CardItem card={subtaskCard} onParentClick={onParentClick} />);
      fireEvent.click(screen.getByTitle('Parent: TEST-001'));
      expect(onParentClick).toHaveBeenCalledOnce();
      expect(onParentClick).toHaveBeenCalledWith('TEST-001');
    });

    it('does not call card onClick when parent badge is clicked', () => {
      const onClick = vi.fn();
      const onParentClick = vi.fn();
      render(<CardItem card={subtaskCard} onClick={onClick} onParentClick={onParentClick} />);
      fireEvent.click(screen.getByTitle('Parent: TEST-001'));
      expect(onClick).not.toHaveBeenCalled();
      expect(onParentClick).toHaveBeenCalledOnce();
    });
  });

  describe('collapsed view (isCollapsed=true)', () => {
    it('renders parent badge when card.parent is defined', () => {
      render(<CardItem card={subtaskCard} isCollapsed />);
      expect(screen.getByTitle('Parent: TEST-001')).toBeInTheDocument();
      expect(screen.getByTitle('Parent: TEST-001')).toHaveTextContent('TEST-001');
    });

    it('does not render parent badge when card.parent is absent', () => {
      render(<CardItem card={baseCard} isCollapsed />);
      expect(screen.queryByTitle(/^Parent:/)).not.toBeInTheDocument();
    });

    it('calls onParentClick with the parent ID when badge is clicked in collapsed view', () => {
      const onParentClick = vi.fn();
      render(<CardItem card={subtaskCard} isCollapsed onParentClick={onParentClick} />);
      fireEvent.click(screen.getByTitle('Parent: TEST-001'));
      expect(onParentClick).toHaveBeenCalledOnce();
      expect(onParentClick).toHaveBeenCalledWith('TEST-001');
    });
  });
});
