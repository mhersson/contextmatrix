import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { CardItem } from './CardItem';
import type { Card } from '../../types';

// @dnd-kit requires a DndContext - provide a minimal mock
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

function makeSub(id: string, overrides: Partial<Card> = {}): Card {
  return {
    ...baseCard,
    id,
    title: `Subtask ${id}`,
    type: 'subtask',
    parent: baseCard.id,
    ...overrides,
  };
}

describe('CardItem - parent ID badge', () => {
  describe('expanded view (isCollapsed=false)', () => {
    it('renders parent badge when card.parent is defined', () => {
      render(<CardItem card={subtaskCard} />);
      expect(screen.getByTitle('Parent: TEST-001')).toBeInTheDocument();
      // Board card shows just the numeric suffix; full ID stays in tooltip/aria-label.
      expect(screen.getByTitle('Parent: TEST-001')).toHaveTextContent('001');
      expect(screen.getByTitle('Parent: TEST-001')).not.toHaveTextContent('TEST-001');
    });

    it('parent badge has correct aria-label', () => {
      render(<CardItem card={subtaskCard} />);
      expect(screen.getByRole('button', { name: 'Navigate to parent TEST-001' })).toBeInTheDocument();
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
      // Collapsed view shows just the numeric suffix to save horizontal space;
      // the full ID stays in the tooltip and aria-label.
      expect(screen.getByTitle('Parent: TEST-001')).toHaveTextContent('001');
      expect(screen.getByTitle('Parent: TEST-001')).not.toHaveTextContent('TEST-001');
    });

    it('parent badge has correct aria-label', () => {
      render(<CardItem card={subtaskCard} isCollapsed />);
      expect(screen.getByRole('button', { name: 'Navigate to parent TEST-001' })).toBeInTheDocument();
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

describe('CardItem - subtask phase strip & peek', () => {
  const subs = [makeSub('TEST-101', { state: 'in_progress' }), makeSub('TEST-102', { state: 'done' })];

  it('renders the strip when subtasks are passed and not otherwise', () => {
    const { rerender } = render(<CardItem card={baseCard} subtasks={subs} />);
    expect(screen.getByRole('button', { name: '2 subtasks' })).toBeInTheDocument();
    rerender(<CardItem card={baseCard} />);
    expect(screen.queryByRole('button', { name: /subtask/ })).not.toBeInTheDocument();
  });

  it('strip click toggles the peek list without opening the card', () => {
    const onClick = vi.fn();
    render(<CardItem card={baseCard} subtasks={subs} onClick={onClick} />);
    const strip = screen.getByRole('button', { name: '2 subtasks' });
    fireEvent.click(strip);
    expect(strip).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByTitle('Subtask TEST-101')).toBeInTheDocument();
    fireEvent.click(strip);
    expect(strip).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByTitle('Subtask TEST-101')).not.toBeInTheDocument();
    expect(onClick).not.toHaveBeenCalled();
  });

  it('peek row click opens the subtask via onParentClick, not the card', () => {
    const onClick = vi.fn();
    const onParentClick = vi.fn();
    render(<CardItem card={baseCard} subtasks={subs} onClick={onClick} onParentClick={onParentClick} />);
    fireEvent.click(screen.getByRole('button', { name: '2 subtasks' }));
    fireEvent.click(screen.getByTitle('Subtask TEST-101'));
    expect(onParentClick).toHaveBeenCalledOnce();
    expect(onParentClick).toHaveBeenCalledWith('TEST-101');
    expect(onClick).not.toHaveBeenCalled();
  });

  it('Enter on a peek row does not bubble up and open the card', () => {
    const onClick = vi.fn();
    render(<CardItem card={baseCard} subtasks={subs} onClick={onClick} onParentClick={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: '2 subtasks' }));
    fireEvent.keyDown(screen.getByTitle('Subtask TEST-101'), { key: 'Enter' });
    expect(onClick).not.toHaveBeenCalled();
  });

  it('collapsed parent keeps an interactive mini strip', () => {
    render(<CardItem card={baseCard} subtasks={subs} isCollapsed onToggleCollapse={vi.fn()} />);
    const strip = screen.getByRole('button', { name: '2 subtasks' });
    fireEvent.click(strip);
    expect(screen.getByTitle('Subtask TEST-101')).toBeInTheDocument();
  });

  it('flashCardId naming a subtask flashes the parent card and auto-opens the peek', () => {
    window.HTMLElement.prototype.scrollIntoView = vi.fn();
    render(<CardItem card={baseCard} subtasks={subs} flashCardId="TEST-101" />);
    const root = screen.getByLabelText(`Card ${baseCard.id}: ${baseCard.title}`);
    expect(root.className).toContain('animate-card-flash');
    expect(screen.getByTitle('Subtask TEST-101')).toBeInTheDocument();
  });
});

describe('CardItem - orphan subtask tint', () => {
  it('an orphan subtask card gets the aqua tint class', () => {
    render(<CardItem card={subtaskCard} />);
    const root = screen.getByLabelText(`Card ${subtaskCard.id}: ${subtaskCard.title}`);
    expect(root.className).toContain('card-orphan-tint');
    expect(root.className).not.toContain('card-orphan-tint--dep-blocked');
  });

  it('a dependency-blocked orphan gets the red tint variant', () => {
    const card = { ...subtaskCard, depends_on: ['TEST-009'], dependencies_met: false };
    render(<CardItem card={card} />);
    const root = screen.getByLabelText(`Card ${card.id}: ${card.title}`);
    expect(root.className).toContain('card-orphan-tint--dep-blocked');
  });

  it('cards without a parent get no tint class', () => {
    render(<CardItem card={baseCard} />);
    const root = screen.getByLabelText(`Card ${baseCard.id}: ${baseCard.title}`);
    expect(root.className).not.toContain('card-orphan-tint');
  });

  it('an agent-held orphan keeps the active pulse-border signal', () => {
    const card = { ...subtaskCard, assigned_agent: 'claude-sonnet-worker' };
    render(<CardItem card={card} />);
    const root = screen.getByLabelText(`Card ${card.id}: ${card.title}`);
    expect(root.className).toContain('animate-pulse-border');
  });
});
