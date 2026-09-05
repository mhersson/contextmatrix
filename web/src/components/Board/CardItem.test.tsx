import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { CardItem } from './CardItem';
import type { Card } from '../../types';

// @dnd-kit requires a DndContext - provide a minimal mock. Passes through the
// `attributes` option the component supplies, defaulting to dnd-kit's real
// default ('sortable') when absent, so an assertion on aria-roledescription
// tests what CardItem passes in rather than a value the mock hardcodes.
vi.mock('@dnd-kit/sortable', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@dnd-kit/sortable')>();
  return {
    ...actual,
    // Every attribute the component can override is passed through, defaulting
    // to dnd-kit's own value - otherwise an assertion on a hardcoded mock value
    // cannot fail when the component stops setting it.
    useSortable: (options: {
      attributes?: { roleDescription?: string; role?: string; tabIndex?: number };
    }) => ({
      attributes: {
        role: options?.attributes?.role ?? 'button',
        tabIndex: options?.attributes?.tabIndex ?? 0,
        'aria-roledescription': options?.attributes?.roleDescription ?? 'sortable',
        // dnd-kit points every draggable at its screen-reader instructions.
        'aria-describedby': 'DndDescribedBy-0',
      },
      listeners: {},
      setNodeRef: () => {},
      transform: null,
      transition: undefined,
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

describe('CardItem - declutter', () => {
  it('shows the priority dot in the header in both views', () => {
    const { rerender } = render(<CardItem card={baseCard} />);
    expect(screen.getByRole('img', { name: 'Priority: medium' })).toBeInTheDocument();
    rerender(<CardItem card={baseCard} isCollapsed />);
    expect(screen.getByRole('img', { name: 'Priority: medium' })).toBeInTheDocument();
  });

  it('collapses the type pill to its initial when the header gets crowded', () => {
    const crowded = {
      ...baseCard,
      type: 'feature',
      autonomous: true,
      mob_participants: 3,
      worker_status: 'running' as const,
      in_playbooks: ['rollout'],
    };
    render(<CardItem card={crowded} />);
    const pill = screen.getByLabelText('Type: feature');
    expect(pill).toHaveTextContent(/^f$/);
    expect(screen.queryByText('feature')).not.toBeInTheDocument();
  });

  it('keeps the full type pill when few signals show', () => {
    render(<CardItem card={{ ...baseCard, type: 'feature', autonomous: true }} />);
    expect(screen.getByText('feature')).toBeInTheDocument();
  });

  it('renders signal icons in the expanded header, none when collapsed', () => {
    const card = { ...baseCard, autonomous: true, mob_participants: 3, worker_status: 'running' as const };
    const { rerender } = render(<CardItem card={card} />);
    expect(screen.getByRole('img', { name: 'Autonomous' })).toBeInTheDocument();
    expect(screen.getByRole('img', { name: 'Mob session - 3 agents' })).toBeInTheDocument();
    expect(screen.getByRole('img', { name: 'Worker running' })).toBeInTheDocument();
    rerender(<CardItem card={card} isCollapsed onToggleCollapse={vi.fn()} />);
    expect(screen.queryByRole('img', { name: 'Autonomous' })).not.toBeInTheDocument();
  });

  it('a claimed card gets the pulse border', () => {
    const card = { ...baseCard, assigned_agent: 'claude-sonnet-worker' };
    render(<CardItem card={card} />);
    const root = screen.getByLabelText(`Card ${card.id}: ${card.title}`);
    expect(root.className).toContain('animate-pulse-border');
  });

  it('a failed worker gets the red status border, beating the claim styling', () => {
    const card = { ...baseCard, assigned_agent: 'claude-sonnet-worker', worker_status: 'failed' as const };
    render(<CardItem card={card} />);
    const root = screen.getByLabelText(`Card ${card.id}: ${card.title}`);
    expect(root.className).toContain('border-l-[var(--red)]');
    expect(root.className).not.toContain('animate-pulse-border');
  });

  it('a parked card gets the yellow status border', () => {
    const card = { ...baseCard, state: 'review', worker_status: 'parked' as const };
    render(<CardItem card={card} />);
    const root = screen.getByLabelText(`Card ${card.id}: ${card.title}`);
    expect(root.className).toContain('border-l-[var(--yellow)]');
    expect(root.className).not.toContain('animate-pulse-border');
  });

  it('failed red beats parked yellow', () => {
    const card = { ...baseCard, state: 'stalled', worker_status: 'parked' as const };
    render(<CardItem card={card} />);
    const root = screen.getByLabelText(`Card ${card.id}: ${card.title}`);
    expect(root.className).toContain('border-l-[var(--red)]');
  });

  it('keeps the failed status border when collapsed', () => {
    const card = { ...baseCard, worker_status: 'failed' as const };
    render(<CardItem card={card} isCollapsed onToggleCollapse={vi.fn()} />);
    const root = screen.getByLabelText(`Card ${card.id}: ${card.title}`);
    expect(root.className).toContain('border-l-[var(--red)]');
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

  it('collapsed parent shows no strip', () => {
    render(<CardItem card={baseCard} subtasks={subs} isCollapsed onToggleCollapse={vi.fn()} />);
    expect(screen.queryByRole('button', { name: /subtask/ })).not.toBeInTheDocument();
  });

  it('flashCardId naming a subtask flashes the parent card and auto-opens the peek', () => {
    window.HTMLElement.prototype.scrollIntoView = vi.fn();
    render(<CardItem card={baseCard} subtasks={subs} flashCardId="TEST-101" />);
    const root = screen.getByLabelText(`Card ${baseCard.id}: ${baseCard.title}`);
    expect(root.className).toContain('animate-card-flash');
    expect(screen.getByTitle('Subtask TEST-101')).toBeInTheDocument();
  });

  it('a subtask flash still surfaces the peek rows on a collapsed parent', () => {
    window.HTMLElement.prototype.scrollIntoView = vi.fn();
    render(
      <CardItem card={baseCard} subtasks={subs} isCollapsed onToggleCollapse={vi.fn()} flashCardId="TEST-101" />,
    );
    expect(screen.getByTitle('Subtask TEST-101')).toBeInTheDocument();
  });
});

describe('CardItem - keyboard activation', () => {
  it('Enter on the card root calls onClick (no keyboard drag sensor to compete with)', () => {
    const onClick = vi.fn();
    render(<CardItem card={baseCard} onClick={onClick} />);
    fireEvent.keyDown(screen.getByLabelText(`Card ${baseCard.id}: ${baseCard.title}`), { key: 'Enter' });
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('does not announce the card as "sortable" - there is no keyboard drag path', () => {
    render(<CardItem card={baseCard} />);
    const root = screen.getByLabelText(`Card ${baseCard.id}: ${baseCard.title}`);
    expect(root).not.toHaveAttribute('aria-roledescription', 'sortable');
    expect(root).toHaveAttribute('aria-roledescription', 'card');
    expect(root).toHaveAttribute('role', 'button');
    expect(root).toHaveAttribute('tabIndex', '0');
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

describe('CardItem - hovercard', () => {
  const signalCard: Card = { ...baseCard, autonomous: true, labels: ['backend'] };
  const cardRoot = () => screen.getByRole('button', { name: 'Card TEST-001: Test card title' });
  // The type pill is in every expanded header, so it anchors the hover zone
  // even on cards with labels but no signal icons.
  const headerGroup = () => screen.getByText('task').parentElement!;

  it('does not open from the card body, however long the pointer rests', () => {
    vi.useFakeTimers();
    try {
      render(<CardItem card={signalCard} />);
      fireEvent.mouseEnter(cardRoot());
      act(() => { vi.advanceTimersByTime(2000); });
      expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it('opens immediately when the header group is hovered', () => {
    render(<CardItem card={{ ...baseCard, labels: ['backend'] }} />);
    fireEvent.mouseEnter(headerGroup());
    expect(screen.getByRole('tooltip')).toHaveTextContent('backend');
  });

  it('opens when a header signal icon is hovered', () => {
    render(<CardItem card={signalCard} />);
    fireEvent.mouseEnter(screen.getByRole('img', { name: 'Autonomous' }));
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
  });

  it('shows a pointer cursor over the header group', () => {
    render(<CardItem card={baseCard} />);
    expect(headerGroup()).toHaveClass('cursor-pointer');
  });

  it('adds the open hovercard to aria-describedby without dropping the drag instructions', () => {
    render(<CardItem card={signalCard} />);
    expect(cardRoot()).toHaveAttribute('aria-describedby', 'DndDescribedBy-0');
    const group = headerGroup();
    fireEvent.mouseEnter(group);
    expect(cardRoot()).toHaveAttribute(
      'aria-describedby',
      `DndDescribedBy-0 ${screen.getByRole('tooltip').id}`,
    );
    fireEvent.mouseLeave(group);
    expect(cardRoot()).toHaveAttribute('aria-describedby', 'DndDescribedBy-0');
  });

  it('closes when the pointer leaves the header group', () => {
    render(<CardItem card={signalCard} />);
    const group = headerGroup();
    fireEvent.mouseEnter(group);
    fireEvent.mouseLeave(group);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('closes on mouse down so a click or drag starts clean', () => {
    render(<CardItem card={signalCard} />);
    fireEvent.mouseEnter(headerGroup());
    fireEvent.mouseDown(cardRoot());
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('opens on keyboard focus of the card itself and closes on blur', () => {
    render(<CardItem card={signalCard} />);
    fireEvent.keyDown(document.body, { key: 'Tab' });
    fireEvent.focus(cardRoot());
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    fireEvent.blur(cardRoot());
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('does not open from the focus a mouse press confers', () => {
    render(<CardItem card={signalCard} />);
    fireEvent.mouseDown(cardRoot());
    fireEvent.focus(cardRoot());
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('does not open from focus restored after a mouse interaction (panel close)', () => {
    render(<CardItem card={signalCard} />);
    // Click opens the panel; the focus trap moves focus away and later
    // hands it back programmatically. Only a pointer was ever used.
    fireEvent.mouseDown(cardRoot());
    fireEvent.mouseUp(cardRoot());
    fireEvent.click(cardRoot());
    fireEvent.blur(cardRoot());
    fireEvent.focus(cardRoot());
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('opens from focus restored after a keyboard interaction', () => {
    render(<CardItem card={signalCard} />);
    fireEvent.mouseDown(cardRoot());
    fireEvent.mouseUp(cardRoot());
    fireEvent.blur(cardRoot());
    fireEvent.keyDown(document.body, { key: 'Escape' });
    fireEvent.focus(cardRoot());
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
  });

  it('still opens on keyboard focus after a press that focused a nested button', () => {
    render(<CardItem card={signalCard} onToggleCollapse={() => {}} />);
    const collapse = screen.getByRole('button', { name: 'Collapse card' });
    fireEvent.mouseDown(collapse);
    fireEvent.focus(collapse);
    fireEvent.mouseUp(collapse);
    fireEvent.keyDown(collapse, { key: 'Tab', shiftKey: true });
    fireEvent.focus(cardRoot());
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
  });

  it('closes on Escape while the card has focus', () => {
    render(<CardItem card={signalCard} />);
    fireEvent.keyDown(document.body, { key: 'Tab' });
    fireEvent.focus(cardRoot());
    fireEvent.keyDown(cardRoot(), { key: 'Escape' });
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('keeps the Escape that closed the hovercard from reaching the board shortcuts', () => {
    const documentKeyDown = vi.fn();
    document.addEventListener('keydown', documentKeyDown);
    try {
      render(<CardItem card={signalCard} />);
      fireEvent.keyDown(document.body, { key: 'Tab' });
      fireEvent.focus(cardRoot());
      fireEvent.keyDown(cardRoot(), { key: 'Escape' });
      expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
      // Tab + the Escape while open: the second must not have bubbled.
      expect(documentKeyDown).toHaveBeenCalledTimes(1);
      fireEvent.keyDown(cardRoot(), { key: 'Escape' });
      expect(documentKeyDown).toHaveBeenCalledTimes(2);
    } finally {
      document.removeEventListener('keydown', documentKeyDown);
    }
  });

  it('closes when any scroll container scrolls', () => {
    render(<CardItem card={signalCard} />);
    fireEvent.mouseEnter(headerGroup());
    fireEvent.scroll(document.body);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('closes when the window resizes', () => {
    render(<CardItem card={signalCard} />);
    fireEvent.mouseEnter(headerGroup());
    fireEvent(window, new Event('resize'));
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('closes when focus moves from the card into a nested button', () => {
    render(<CardItem card={signalCard} onToggleCollapse={() => {}} />);
    fireEvent.keyDown(document.body, { key: 'Tab' });
    fireEvent.focus(cardRoot());
    fireEvent.blur(cardRoot());
    fireEvent.focus(screen.getByRole('button', { name: 'Collapse card' }));
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('opens from the crowded-header type initial too', () => {
    render(
      <CardItem
        card={{
          ...signalCard,
          depends_on: ['TEST-000'],
          mob_participants: 2,
          worker_status: 'queued',
          in_playbooks: ['rollout'],
        }}
      />,
    );
    fireEvent.mouseEnter(screen.getByLabelText('Type: task'));
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
  });

  it('keeps the collapse button outside the hover zone so its own tooltip stands alone', () => {
    render(<CardItem card={signalCard} onToggleCollapse={() => {}} />);
    const collapse = screen.getByRole('button', { name: 'Collapse card' });
    expect(collapse).toHaveAttribute('title', 'Collapse card');
    fireEvent.mouseEnter(collapse);
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('never shows a hovercard on a collapsed row', () => {
    render(<CardItem card={signalCard} isCollapsed onToggleCollapse={() => {}} />);
    fireEvent.mouseEnter(screen.getByLabelText('Type: task'));
    fireEvent.focus(cardRoot());
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('never shows a hovercard on the drag overlay copy', () => {
    render(<CardItem card={signalCard} dragOverlay />);
    fireEvent.mouseEnter(headerGroup());
    fireEvent.focus(cardRoot());
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('drops the slow native title from the priority dot and crowded type pill', () => {
    render(
      <CardItem
        card={{
          ...signalCard,
          depends_on: ['TEST-000'],
          mob_participants: 2,
          worker_status: 'queued',
          in_playbooks: ['rollout'],
        }}
      />,
    );
    expect(screen.getByRole('img', { name: 'Priority: medium' })).not.toHaveAttribute('title');
    expect(screen.getByLabelText('Type: task')).not.toHaveAttribute('title');
  });
});
