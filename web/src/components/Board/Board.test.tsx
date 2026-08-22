import { useState } from 'react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, within, waitFor, act } from '@testing-library/react';
import type { DndContextProps } from '@dnd-kit/core';
import { KeyboardSensor } from '@dnd-kit/core';
import { isTouchDevice } from '../../utils/isTouchDevice';
import { Board } from './Board';
import type { Card, ProjectConfig } from '../../types';

// ---------------------------------------------------------------------------
// isTouchDevice unit tests
// ---------------------------------------------------------------------------

describe('isTouchDevice', () => {
  const originalMatchMedia = window.matchMedia;

  afterEach(() => {
    // Restore original matchMedia after each test
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
    Object.defineProperty(navigator, 'maxTouchPoints', {
      writable: true,
      value: 0,
    });
  });

  it('returns true when matchMedia reports pointer: coarse', () => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: (query: string) => ({
        matches: query === '(pointer: coarse)',
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }),
    });
    expect(isTouchDevice()).toBe(true);
  });

  it('returns false when matchMedia reports pointer: fine', () => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: (query: string) => ({
        matches: query !== '(pointer: coarse)',
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }),
    });
    expect(isTouchDevice()).toBe(false);
  });

  it('falls back to navigator.maxTouchPoints when matchMedia is unavailable', () => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: undefined,
    });
    Object.defineProperty(navigator, 'maxTouchPoints', {
      writable: true,
      value: 5,
    });
    expect(isTouchDevice()).toBe(true);
  });

  it('returns false via maxTouchPoints fallback when no touch points', () => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: undefined,
    });
    Object.defineProperty(navigator, 'maxTouchPoints', {
      writable: true,
      value: 0,
    });
    expect(isTouchDevice()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Board integration: DnD disabled on touch devices
// ---------------------------------------------------------------------------

// Minimal mock for @dnd-kit/core - we only care that DndContext receives the
// correct props (sensors, onDragEnd). DndContext is replaced with a
// pass-through that renders its children and captures its props so
// onDragEnd can be invoked directly from a test.
let capturedDndProps: DndContextProps = {};
vi.mock('@dnd-kit/core', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@dnd-kit/core')>();
  return {
    ...actual,
    useDroppable: () => ({
      setNodeRef: () => {},
      isOver: false,
    }),
    DndContext: (props: DndContextProps) => {
      capturedDndProps = props;
      return props.children;
    },
  };
});

// CardItem uses useSortable (@dnd-kit/sortable); Column's SortableContext
// and verticalListSortingStrategy stay real via importOriginal.
vi.mock('@dnd-kit/sortable', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@dnd-kit/sortable')>();
  return {
    ...actual,
    useSortable: () => ({
      attributes: {},
      listeners: {},
      setNodeRef: () => {},
      transform: null,
      transition: undefined,
      isDragging: false,
    }),
  };
});

const baseConfig: ProjectConfig = {
  name: 'test-project',
  prefix: 'TEST',
  next_id: 1,
  states: ['todo', 'done'],
  transitions: { todo: ['done'], done: [] },
  types: ['task'],
  priorities: ['medium'],
};

const sampleCard: Card = {
  id: 'TEST-001',
  title: 'Sample card',
  project: 'test-project',
  type: 'task',
  state: 'todo',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
};

// ---------------------------------------------------------------------------
// Board - mobile NowRail drawer
// ---------------------------------------------------------------------------

// Helper: build a matchMedia stub that returns true only for the given query.
// Anything else (including `(pointer: coarse)`) returns false. This isolates
// the test from the touch-device sensor path so mobile-layout behaviour can
// only be triggered by the viewport-width query under test.
function mockMatchMediaTrueFor(trueQuery: string) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: query === trueQuery,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }),
  });
}

// ---------------------------------------------------------------------------
// Board - keyboard drag removed
// ---------------------------------------------------------------------------

describe('Board - keyboard drag removed', () => {
  const originalMatchMedia = window.matchMedia;

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  it('registers exactly one sensor and it is not the KeyboardSensor', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    render(
      <Board
        cards={[sampleCard]}
        config={baseConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />
    );
    expect(capturedDndProps.sensors).toHaveLength(1);
    expect(capturedDndProps.sensors!.every((d) => d.sensor !== KeyboardSensor)).toBe(true);
  });

  it('screen-reader instructions describe mouse/touch dragging, not keyboard', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    render(
      <Board
        cards={[sampleCard]}
        config={baseConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />
    );
    const draggable = capturedDndProps.accessibility?.screenReaderInstructions?.draggable;
    expect(draggable).toBeTruthy();
    expect(draggable).not.toMatch(/space bar/i);
    expect(draggable).not.toMatch(/arrow keys/i);
  });
});

describe('Board - mobile NowRail drawer', () => {
  const originalMatchMedia = window.matchMedia;

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  it('hides the NowRail on initial mount when (max-width: 768px) matches', () => {
    mockMatchMediaTrueFor('(max-width: 768px)');
    const { container } = render(
      <Board
        cards={[sampleCard]}
        config={baseConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />
    );
    expect(container.querySelector('.now-rail')).toBeNull();
    expect(container.querySelector('.now-rail-backdrop')).toBeNull();
  });

  it('shows the NowRail and a backdrop after clicking the rail toggle on mobile', () => {
    mockMatchMediaTrueFor('(max-width: 768px)');
    const { container } = render(
      <Board
        cards={[sampleCard]}
        config={baseConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />
    );
    const toggle = screen.getByRole('button', { name: /show rail/i });
    fireEvent.click(toggle);
    expect(container.querySelector('.now-rail')).not.toBeNull();
    expect(container.querySelector('.now-rail-backdrop')).not.toBeNull();
  });

  it('hides the NowRail on initial mount on desktop and shows no backdrop', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)'); // any query the component does not read
    const { container } = render(
      <Board
        cards={[sampleCard]}
        config={baseConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />
    );
    expect(container.querySelector('.now-rail')).toBeNull();
    expect(container.querySelector('.now-rail-backdrop')).toBeNull();
  });

  it('shows the NowRail without a backdrop after clicking the rail toggle on desktop', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const { container } = render(
      <Board
        cards={[sampleCard]}
        config={baseConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />
    );
    const toggle = screen.getByRole('button', { name: /show rail/i });
    fireEvent.click(toggle);
    expect(container.querySelector('.now-rail')).not.toBeNull();
    expect(container.querySelector('.now-rail-backdrop')).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Board - NowRail open-state persistence
// ---------------------------------------------------------------------------

describe('Board - NowRail persistence to localStorage', () => {
  const originalMatchMedia = window.matchMedia;

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  const boardProps = {
    cards: [sampleCard],
    config: baseConfig,
    loading: false,
    error: null,
    activeAgents: [],
    cardsCompletedToday: 0,
    activityEntries: [],
    currentAgent: null,
  };

  it('opening the rail, unmounting, and remounting restores the open state', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const first = render(<Board {...boardProps} />);
    expect(first.container.querySelector('.now-rail')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: /show rail/i }));
    expect(first.container.querySelector('.now-rail')).not.toBeNull();

    first.unmount();

    const second = render(<Board {...boardProps} />);
    expect(second.container.querySelector('.now-rail')).not.toBeNull();
  });

  it('treats a malformed stored value as no preference (rail stays closed)', () => {
    localStorage.setItem('contextmatrix-now-rail-open', 'maybe');
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const { container } = render(<Board {...boardProps} />);
    expect(container.querySelector('.now-rail')).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Board - MetricsRibbon headline fallback during initial mount
// ---------------------------------------------------------------------------

describe('Board - MetricsRibbon inFlight fallback', () => {
  const originalMatchMedia = window.matchMedia;

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  it('passes cards-derived inFlight count to MetricsRibbon when stateCounts is undefined', () => {
    // Simulate initial mount: stateCounts not yet available, but cards are loaded.
    // Before the fix, inFlightTotal was undefined so inFlightParents fell back to 0.
    // After the fix, inFlightTotal falls back to cards.filter count (3 here).
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const inProgressConfig: ProjectConfig = {
      ...baseConfig,
      states: ['todo', 'in_progress', 'review', 'done'],
      transitions: {
        todo: ['in_progress'],
        in_progress: ['review'],
        review: ['done'],
        done: [],
      },
    };
    const makeCard = (id: string, state: string): Card => ({
      id,
      title: `Card ${id}`,
      project: 'test-project',
      type: 'task',
      state,
      priority: 'medium',
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      body: '',
    });
    const cards = [
      makeCard('TEST-001', 'in_progress'),
      makeCard('TEST-002', 'in_progress'),
      makeCard('TEST-003', 'in_progress'),
      makeCard('TEST-004', 'todo'),
    ];
    render(
      <Board
        cards={cards}
        config={inProgressConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
        // stateCounts and stateCountsParents deliberately omitted (undefined)
        // to simulate the dashboard fetch still in flight.
      />
    );
    // The "In flight" tile should show 3 (cards-derived), not 0.
    const inFlightTile = screen.getByText('In flight').closest('.metric-tile');
    expect(inFlightTile).not.toBeNull();
    const numSpan = inFlightTile!.querySelector('.metric-tile__num');
    expect(numSpan?.textContent).toBe('3');
  });

  it('derives openCount and inReviewCount from stateCountsParents when present (stalled stays open)', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const config: ProjectConfig = {
      ...baseConfig,
      states: ['todo', 'in_progress', 'review', 'done', 'stalled', 'not_planned'],
      transitions: { todo: [], in_progress: [], review: [], done: [], stalled: [], not_planned: [] },
    };
    render(
      <Board
        cards={[]}
        config={config}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={9}
        cardsCompletedTodayParents={5}
        activityEntries={[]}
        currentAgent={null}
        stateCounts={{ todo: 4, in_progress: 3, review: 2, stalled: 1, done: 7, not_planned: 2 }}
        stateCountsParents={{ todo: 2, in_progress: 2, review: 1, stalled: 1, done: 4, not_planned: 1 }}
      />,
    );
    // BoardBand renders "{openCount} open · {inReviewCount} in review · {shippedToday} shipped today".
    // openCount uses stateCountsParents: 2 + 2 + 1 + 1 = 6 (excludes done + not_planned).
    // inReviewCount uses stateCountsParents['review'] = 1.
    // shippedToday uses cardsCompletedTodayParents = 5.
    expect(screen.getByText(/6 open · 1 in review · 5 shipped today/)).toBeInTheDocument();
  });

  it('derives openCount and inReviewCount from cards (parents only) when stateCountsParents is undefined', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const config: ProjectConfig = {
      ...baseConfig,
      states: ['todo', 'in_progress', 'review', 'done', 'stalled'],
      transitions: { todo: [], in_progress: [], review: [], done: [], stalled: [] },
    };
    const makeCard = (id: string, state: string, parent?: string): Card => ({
      id,
      title: id,
      project: 'test-project',
      type: parent ? 'subtask' : 'task',
      state,
      priority: 'medium',
      parent,
      created: '2026-01-01T00:00:00Z',
      updated: '2026-01-01T00:00:00Z',
      body: '',
    });
    const cards = [
      makeCard('A1', 'todo'),
      makeCard('A2', 'in_progress'),
      makeCard('A3', 'review'),
      makeCard('A4', 'review'),
      makeCard('A5', 'stalled'),
      makeCard('A6', 'done'),
      // Subtasks below - should be excluded from open / in review counts.
      makeCard('A7', 'todo', 'A1'),
      makeCard('A8', 'review', 'A1'),
    ];
    render(
      <Board
        cards={cards}
        config={config}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />,
    );
    // openCount fallback excludes done/not_planned + subtasks. = 1+1+2+1 = 5
    // inReviewCount fallback = 2 (subtask A8 excluded)
    expect(screen.getByText(/5 open · 2 in review · 0 shipped today/)).toBeInTheDocument();
  });

  it('uses parent-only shippedLast7d in BoardBand subheader', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const { container } = render(
      <Board
        cards={[]}
        config={baseConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={20}
        cardsCompletedTodayParents={4}
        cardsCompletedLast7d={50}
        cardsCompletedLast7dParents={12}
        cardsCompletedPrior7d={40}
        cardsCompletedPrior7dParents={10}
        stateCounts={{ todo: 0, done: 0 }}
        stateCountsParents={{ todo: 0, done: 0 }}
        activityEntries={[]}
        currentAgent={null}
      />,
    );
    // BoardBand subheader carries parent-only numbers.
    // shippedToday = cardsCompletedTodayParents = 4.
    // shippedLast7d = cardsCompletedLast7dParents = 12.
    expect(screen.getByText(/4 shipped today/)).toBeInTheDocument();
    expect(screen.getByText(/12 shipped this week/)).toBeInTheDocument();
    // The delta is rendered both in BoardBand (parent-only baseline) and in
    // MetricsRibbon; scope to the BoardBand subheader to assert the +20% origin.
    const band = container.querySelector('.board-band__sub');
    expect(band?.textContent).toMatch(/\+20%/);
  });
});

// ---------------------------------------------------------------------------
// Board - subtasks collapse into the parent's phase strip
// ---------------------------------------------------------------------------

describe('Board - subtask phase strip & column membership', () => {
  const originalMatchMedia = window.matchMedia;

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  const makeCard = (id: string, state: string, overrides: Partial<Card> = {}): Card => ({
    id,
    title: `Card ${id}`,
    project: 'test-project',
    type: overrides.parent ? 'subtask' : 'task',
    state,
    priority: 'medium',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
    ...overrides,
  });

  const boardProps = {
    config: baseConfig,
    loading: false,
    error: null,
    activeAgents: [],
    cardsCompletedToday: 0,
    activityEntries: [],
    currentAgent: null,
  };

  it('subtasks whose parent is on the board render as a strip, not as cards', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const cards = [
      makeCard('TEST-001', 'todo'),
      makeCard('TEST-002', 'todo', { parent: 'TEST-001', title: 'Attached subtask' }),
    ];
    render(<Board {...boardProps} cards={cards} />);
    expect(screen.queryByLabelText('Card TEST-002: Attached subtask')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Card TEST-001: Card TEST-001')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '1 subtask' })).toBeInTheDocument();
  });

  it('column count badges count parents only', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const cards = [
      makeCard('TEST-001', 'todo'),
      makeCard('TEST-002', 'todo', { parent: 'TEST-001' }),
      makeCard('TEST-003', 'todo', { parent: 'TEST-001' }),
    ];
    render(<Board {...boardProps} cards={cards} />);
    const heading = screen.getByRole('heading', { name: /Backlog column/ });
    const headerRow = heading.parentElement!.parentElement!;
    expect(within(headerRow).getByText('1')).toBeInTheDocument();
  });

  it('an orphan subtask still renders as a tinted card', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const cards = [makeCard('TEST-004', 'todo', { parent: 'TEST-999', title: 'Orphan subtask' })];
    render(<Board {...boardProps} cards={cards} />);
    const orphan = screen.getByLabelText('Card TEST-004: Orphan subtask');
    expect(orphan.className).toContain('card-orphan-tint');
  });

  it('a stalled parent surfaces in the spotlight with a clickable phase strip', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const onCardClick = vi.fn();
    const cards = [
      makeCard('TEST-001', 'stalled', { title: 'Stalled parent' }),
      makeCard('TEST-002', 'todo', { parent: 'TEST-001', title: 'Live subtask' }),
    ];
    render(<Board {...boardProps} cards={cards} onCardClick={onCardClick} />);
    const strip = screen.getByRole('button', { name: '1 subtask' });
    fireEvent.click(strip);
    fireEvent.click(screen.getByTitle('Live subtask'));
    expect(onCardClick).toHaveBeenCalledTimes(1);
    expect(onCardClick).toHaveBeenCalledWith(expect.objectContaining({ id: 'TEST-002' }));
  });

  it('a flash for a subtask of a stalled parent auto-opens the spotlight peek', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    window.HTMLElement.prototype.scrollIntoView = vi.fn();
    const cards = [
      makeCard('TEST-001', 'stalled', { title: 'Stalled parent' }),
      makeCard('TEST-002', 'todo', { parent: 'TEST-001', title: 'Fresh subtask' }),
    ];
    render(<Board {...boardProps} cards={cards} flashCardId="TEST-002" />);
    expect(screen.getByRole('button', { name: '1 subtask' })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByTitle('Fresh subtask')).toBeInTheDocument();
  });

  it('a parent surfaces when only one of its subtasks matches the search', async () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const cards = [
      makeCard('TEST-001', 'todo', { title: 'Parent card' }),
      makeCard('TEST-002', 'todo', { parent: 'TEST-001', title: 'zebra-unique-token' }),
      makeCard('TEST-005', 'todo', { title: 'Unrelated card' }),
    ];
    render(<Board {...boardProps} cards={cards} />);
    fireEvent.change(screen.getByLabelText('Search cards'), { target: { value: 'zebra-unique' } });
    await waitFor(() => {
      expect(screen.queryByLabelText('Card TEST-005: Unrelated card')).not.toBeInTheDocument();
    });
    expect(screen.getByLabelText('Card TEST-001: Parent card')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Board - column sort integration
// ---------------------------------------------------------------------------

describe('Board - column sort', () => {
  const originalMatchMedia = window.matchMedia;

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  const makeCard = (id: string, state: string, overrides: Partial<Card> = {}): Card => ({
    id,
    title: `Card ${id}`,
    project: 'test-project',
    type: overrides.parent ? 'subtask' : 'task',
    state,
    priority: 'medium',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
    ...overrides,
  });

  const sortConfig: ProjectConfig = {
    name: 'sort-test',
    prefix: 'SORT',
    next_id: 1,
    states: ['todo', 'done'],
    transitions: { todo: ['done'], done: [] },
    types: ['task'],
    priorities: ['low', 'medium', 'high', 'critical'],
  };

  it('defaults to "recent" order (most recent updated first)', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const cards = [
      makeCard('SORT-001', 'todo', { updated: '2026-01-03T00:00:00Z', created: '2026-01-01T00:00:00Z' }),
      makeCard('SORT-002', 'todo', { updated: '2026-01-02T00:00:00Z', created: '2026-01-02T00:00:00Z' }),
      makeCard('SORT-003', 'todo', { updated: '2026-01-01T00:00:00Z', created: '2026-01-03T00:00:00Z' }),
    ];
    render(
      <Board
        cards={cards}
        config={sortConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />,
    );

    // Default "recent" sorts by updated descending: SORT-001 (Jan 3), SORT-002 (Jan 2), SORT-003 (Jan 1)
    const todoColumn = screen.getByRole('heading', { name: /Backlog column/ });
    const column = todoColumn.closest('[data-accent="stripe"]')!;
    const cardItems = column.querySelectorAll('[aria-label^="Card SORT"]');
    expect(cardItems).toHaveLength(3);
    expect(cardItems[0]).toHaveAttribute('aria-label', 'Card SORT-001: Card SORT-001');
    expect(cardItems[1]).toHaveAttribute('aria-label', 'Card SORT-002: Card SORT-002');
    expect(cardItems[2]).toHaveAttribute('aria-label', 'Card SORT-003: Card SORT-003');
  });

  it('switches to id-asc order when selected from sort menu', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const cards = [
      makeCard('SORT-003', 'todo', { updated: '2026-01-03T00:00:00Z' }),
      makeCard('SORT-001', 'todo', { updated: '2026-01-01T00:00:00Z' }),
      makeCard('SORT-002', 'todo', { updated: '2026-01-02T00:00:00Z' }),
    ];
    render(
      <Board
        cards={cards}
        config={sortConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />,
    );

    // Find the sort menu trigger in the first column
    const sortTriggers = screen.getAllByRole('button', { name: /select sort order/i });
    // First column (todo)
    fireEvent.click(sortTriggers[0]);

    // Click "ID ↑" to sort ascending by ID
    fireEvent.click(screen.getByText('ID ↑'));

    // Now cards should be in ID order: SORT-001, SORT-002, SORT-003
    const todoColumn = screen.getByRole('heading', { name: /Backlog column/ });
    const column = todoColumn.closest('[data-accent="stripe"]')!;
    const cardItems = column.querySelectorAll('[aria-label^="Card SORT"]');
    expect(cardItems).toHaveLength(3);
    expect(cardItems[0]).toHaveAttribute('aria-label', 'Card SORT-001: Card SORT-001');
    expect(cardItems[1]).toHaveAttribute('aria-label', 'Card SORT-002: Card SORT-002');
    expect(cardItems[2]).toHaveAttribute('aria-label', 'Card SORT-003: Card SORT-003');
  });

  it('switches to id-desc order when selected from sort menu', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const cards = [
      makeCard('SORT-001', 'todo', { updated: '2026-01-01T00:00:00Z' }),
      makeCard('SORT-002', 'todo', { updated: '2026-01-02T00:00:00Z' }),
      makeCard('SORT-003', 'todo', { updated: '2026-01-03T00:00:00Z' }),
    ];
    render(
      <Board
        cards={cards}
        config={sortConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />,
    );

    const sortTriggers = screen.getAllByRole('button', { name: /select sort order/i });
    fireEvent.click(sortTriggers[0]);

    fireEvent.click(screen.getByText('ID ↓'));

    const todoColumn = screen.getByRole('heading', { name: /Backlog column/ });
    const column = todoColumn.closest('[data-accent="stripe"]')!;
    const cardItems = column.querySelectorAll('[aria-label^="Card SORT"]');
    expect(cardItems).toHaveLength(3);
    expect(cardItems[0]).toHaveAttribute('aria-label', 'Card SORT-003: Card SORT-003');
    expect(cardItems[1]).toHaveAttribute('aria-label', 'Card SORT-002: Card SORT-002');
    expect(cardItems[2]).toHaveAttribute('aria-label', 'Card SORT-001: Card SORT-001');
  });

  it('switches to priority order when selected', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const cards = [
      makeCard('SORT-001', 'todo', { priority: 'low' }),
      makeCard('SORT-002', 'todo', { priority: 'critical' }),
      makeCard('SORT-003', 'todo', { priority: 'high' }),
    ];
    render(
      <Board
        cards={cards}
        config={sortConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />,
    );

    const sortTriggers = screen.getAllByRole('button', { name: /select sort order/i });
    fireEvent.click(sortTriggers[0]);

    fireEvent.click(screen.getByText('Priority'));

    const todoColumn = screen.getByRole('heading', { name: /Backlog column/ });
    const column = todoColumn.closest('[data-accent="stripe"]')!;
    const cardItems = column.querySelectorAll('[aria-label^="Card SORT"]');
    expect(cardItems).toHaveLength(3);
    // Priority order: critical (0), high (1), low (3)
    expect(cardItems[0]).toHaveAttribute('aria-label', 'Card SORT-002: Card SORT-002');
    expect(cardItems[1]).toHaveAttribute('aria-label', 'Card SORT-003: Card SORT-003');
    expect(cardItems[2]).toHaveAttribute('aria-label', 'Card SORT-001: Card SORT-001');
  });

  it('switches to type order when selected', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const cards = [
      makeCard('SORT-001', 'todo', { type: 'feature' }),
      makeCard('SORT-002', 'todo', { type: 'bug' }),
      makeCard('SORT-003', 'todo', { type: 'task' }),
    ];
    render(
      <Board
        cards={cards}
        config={sortConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />,
    );

    const sortTriggers = screen.getAllByRole('button', { name: /select sort order/i });
    fireEvent.click(sortTriggers[0]);

    fireEvent.click(screen.getByText('Type'));

    const todoColumn = screen.getByRole('heading', { name: /Backlog column/ });
    const column = todoColumn.closest('[data-accent="stripe"]')!;
    const cardItems = column.querySelectorAll('[aria-label^="Card SORT"]');
    expect(cardItems).toHaveLength(3);
    // Type order: bug (0), feature (1), task (2)
    expect(cardItems[0]).toHaveAttribute('aria-label', 'Card SORT-002: Card SORT-002');
    expect(cardItems[1]).toHaveAttribute('aria-label', 'Card SORT-001: Card SORT-001');
    expect(cardItems[2]).toHaveAttribute('aria-label', 'Card SORT-003: Card SORT-003');
  });

  it('sort mode is selected per-column; done column has its own sort independent of todo', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const twoStateConfig: ProjectConfig = {
      ...sortConfig,
      states: ['todo', 'done'],
    };
    const cards = [
      makeCard('SORT-001', 'todo', { updated: '2026-01-03T00:00:00Z' }),
      makeCard('SORT-002', 'todo', { updated: '2026-01-01T00:00:00Z' }),
      makeCard('SORT-010', 'done', { updated: '2026-01-02T00:00:00Z' }),
      makeCard('SORT-011', 'done', { updated: '2026-01-01T00:00:00Z' }),
    ];
    render(
      <Board
        cards={cards}
        config={twoStateConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />,
    );

    // Change todo sort to id-asc
    const sortTriggers = screen.getAllByRole('button', { name: /select sort order/i });
    fireEvent.click(sortTriggers[0]);
    fireEvent.click(screen.getByText('ID ↑'));

    // Todo should now be id-asc: SORT-001, SORT-002
    const todoColumn = screen.getByRole('heading', { name: /Backlog column/ });
    const todoCol = todoColumn.closest('[data-accent="stripe"]')!;
    const todoCards = todoCol.querySelectorAll('[aria-label^="Card SORT"]');
    expect(todoCards[0]).toHaveAttribute('aria-label', 'Card SORT-001: Card SORT-001');
    expect(todoCards[1]).toHaveAttribute('aria-label', 'Card SORT-002: Card SORT-002');

    // Done should still be "recent": SORT-010 (Jan 2), SORT-011 (Jan 1)
    const doneColumn = screen.getByRole('heading', { name: /Shipped column/ });
    const doneCol = doneColumn.closest('[data-accent="stripe"]')!;
    const doneCards = doneCol.querySelectorAll('[aria-label^="Card SORT"]');
    expect(doneCards[0]).toHaveAttribute('aria-label', 'Card SORT-010: Card SORT-010');
    expect(doneCards[1]).toHaveAttribute('aria-label', 'Card SORT-011: Card SORT-011');
  });

  it('id sort stays numeric once a project passes 999 cards', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const cards = [
      makeCard('SORT-1000', 'todo'),
      makeCard('SORT-099', 'todo'),
      makeCard('SORT-200', 'todo'),
    ];
    render(
      <Board
        cards={cards}
        config={sortConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />,
    );

    const sortTriggers = screen.getAllByRole('button', { name: /select sort order/i });
    fireEvent.click(sortTriggers[0]);
    fireEvent.click(screen.getByText('ID ↑'));

    const column = screen.getByRole('heading', { name: /Backlog column/ }).closest('[data-accent="stripe"]')!;
    const cardItems = column.querySelectorAll('[aria-label^="Card SORT"]');
    expect(cardItems[0]).toHaveAttribute('aria-label', 'Card SORT-099: Card SORT-099');
    expect(cardItems[1]).toHaveAttribute('aria-label', 'Card SORT-200: Card SORT-200');
    expect(cardItems[2]).toHaveAttribute('aria-label', 'Card SORT-1000: Card SORT-1000');
  });

  it('priority sort follows the board\'s own priority scale', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const customConfig: ProjectConfig = {
      ...sortConfig,
      priorities: ['minor', 'major', 'blocker'],
    };
    const cards = [
      makeCard('SORT-001', 'todo', { priority: 'minor' }),
      makeCard('SORT-002', 'todo', { priority: 'blocker' }),
      makeCard('SORT-003', 'todo', { priority: 'major' }),
    ];
    render(
      <Board
        cards={cards}
        config={customConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />,
    );

    const sortTriggers = screen.getAllByRole('button', { name: /select sort order/i });
    fireEvent.click(sortTriggers[0]);
    fireEvent.click(screen.getByText('Priority'));

    // Most urgent first: the scale is declared least-urgent-first.
    const column = screen.getByRole('heading', { name: /Backlog column/ }).closest('[data-accent="stripe"]')!;
    const cardItems = column.querySelectorAll('[aria-label^="Card SORT"]');
    expect(cardItems[0]).toHaveAttribute('aria-label', 'Card SORT-002: Card SORT-002');
    expect(cardItems[1]).toHaveAttribute('aria-label', 'Card SORT-003: Card SORT-003');
    expect(cardItems[2]).toHaveAttribute('aria-label', 'Card SORT-001: Card SORT-001');
  });

  it('type sort ranks board-specific types after the built-in ones', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const customConfig: ProjectConfig = {
      ...sortConfig,
      types: ['task', 'bug', 'chore'],
    };
    const cards = [
      makeCard('SORT-001', 'todo', { type: 'chore' }),
      makeCard('SORT-002', 'todo', { type: 'task' }),
      makeCard('SORT-003', 'todo', { type: 'bug' }),
    ];
    render(
      <Board
        cards={cards}
        config={customConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
      />,
    );

    const sortTriggers = screen.getAllByRole('button', { name: /select sort order/i });
    fireEvent.click(sortTriggers[0]);
    fireEvent.click(screen.getByText('Type'));

    // bug (built-in 0), task (built-in 2), then chore (board-specific).
    const column = screen.getByRole('heading', { name: /Backlog column/ }).closest('[data-accent="stripe"]')!;
    const cardItems = column.querySelectorAll('[aria-label^="Card SORT"]');
    expect(cardItems[0]).toHaveAttribute('aria-label', 'Card SORT-003: Card SORT-003');
    expect(cardItems[1]).toHaveAttribute('aria-label', 'Card SORT-002: Card SORT-002');
    expect(cardItems[2]).toHaveAttribute('aria-label', 'Card SORT-001: Card SORT-001');
  });
});

// ---------------------------------------------------------------------------
// Board - handleDragEnd: resolve a card-id or column-id drop target
// ---------------------------------------------------------------------------

describe('Board - handleDragEnd drop-target resolution', () => {
  const originalMatchMedia = window.matchMedia;

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  const dragConfig: ProjectConfig = {
    name: 'drag-test',
    prefix: 'DRAG',
    next_id: 1,
    states: ['todo', 'in_progress', 'done'],
    transitions: { todo: ['in_progress', 'done'], in_progress: ['done'], done: [] },
    types: ['task'],
    priorities: ['medium'],
  };

  const makeCard = (id: string, state: string): Card => ({
    id,
    title: `Card ${id}`,
    project: 'drag-test',
    type: 'task',
    state,
    priority: 'medium',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
  });

  type DragEndHandler = NonNullable<DndContextProps['onDragEnd']>;
  function dragEndEvent(activeId: string, activeCard: Card, overId: string | null): Parameters<DragEndHandler>[0] {
    return {
      active: { id: activeId, data: { current: { card: activeCard } } },
      over: overId ? { id: overId } : null,
    } as unknown as Parameters<DragEndHandler>[0];
  }

  function renderDragBoard(cards: Card[]) {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const onCardMove = vi.fn<(id: string, state: string) => Promise<boolean>>().mockResolvedValue(true);
    render(
      <Board
        cards={cards}
        config={dragConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
        onCardMove={onCardMove}
      />,
    );
    return onCardMove;
  }

  it('dropping over a card in another column resolves to that card\'s state', () => {
    const todoCard = makeCard('DRAG-001', 'todo');
    const doneCard = makeCard('DRAG-002', 'done');
    const onCardMove = renderDragBoard([todoCard, doneCard]);
    capturedDndProps.onDragEnd!(dragEndEvent('DRAG-001', todoCard, 'DRAG-002'));
    expect(onCardMove).toHaveBeenCalledWith('DRAG-001', 'done');
  });

  it('dropping over a column id calls onCardMove with that state', () => {
    const todoCard = makeCard('DRAG-001', 'todo');
    const onCardMove = renderDragBoard([todoCard]);
    capturedDndProps.onDragEnd!(dragEndEvent('DRAG-001', todoCard, 'in_progress'));
    expect(onCardMove).toHaveBeenCalledWith('DRAG-001', 'in_progress');
  });

  it('an invalid transition calls nothing', () => {
    const doneCard = makeCard('DRAG-001', 'done');
    const todoCard = makeCard('DRAG-002', 'todo');
    const onCardMove = renderDragBoard([doneCard, todoCard]);
    capturedDndProps.onDragEnd!(dragEndEvent('DRAG-001', doneCard, 'DRAG-002'));
    expect(onCardMove).not.toHaveBeenCalled();
  });

  it('a same-column drop calls nothing', () => {
    const cardA = makeCard('DRAG-001', 'todo');
    const cardB = makeCard('DRAG-002', 'todo');
    const onCardMove = renderDragBoard([cardA, cardB]);
    capturedDndProps.onDragEnd!(dragEndEvent('DRAG-001', cardA, 'DRAG-002'));
    expect(onCardMove).not.toHaveBeenCalled();
  });

  it('an unresolvable over.id calls nothing', () => {
    const todoCard = makeCard('DRAG-001', 'todo');
    const onCardMove = renderDragBoard([todoCard]);
    capturedDndProps.onDragEnd!(dragEndEvent('DRAG-001', todoCard, 'DRAG-999'));
    expect(onCardMove).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Board - manual order: persistence on drop, seeding, and per-column scope
// ---------------------------------------------------------------------------

describe('Board - manual order', () => {
  const originalMatchMedia = window.matchMedia;

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  const manualConfig: ProjectConfig = {
    name: 'manual-test',
    prefix: 'MAN',
    next_id: 1,
    states: ['todo', 'done'],
    transitions: { todo: ['done'], done: [] },
    types: ['task'],
    priorities: ['low', 'medium', 'high', 'critical'],
  };

  const makeCard = (id: string, state: string, overrides: Partial<Card> = {}): Card => ({
    id,
    title: `Card ${id}`,
    project: 'manual-test',
    type: 'task',
    state,
    priority: 'medium',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
    ...overrides,
  });

  // Recent-descending order out of the box: 001 (day 3) before 002 (day 2)
  // before 003 (day 1).
  const cardA = () => makeCard('MAN-001', 'todo', { updated: '2026-01-03T00:00:00Z' });
  const cardB = () => makeCard('MAN-002', 'todo', { updated: '2026-01-02T00:00:00Z' });
  const cardC = () => makeCard('MAN-003', 'todo', { updated: '2026-01-01T00:00:00Z' });

  type DragEndHandler = NonNullable<DndContextProps['onDragEnd']>;
  function dragEndEvent(activeId: string, activeCard: Card, overId: string | null): Parameters<DragEndHandler>[0] {
    return {
      active: { id: activeId, data: { current: { card: activeCard } } },
      over: overId ? { id: overId } : null,
    } as unknown as Parameters<DragEndHandler>[0];
  }

  function renderManualBoard(cards: Card[]) {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const onCardMove = vi.fn<(id: string, state: string) => Promise<boolean>>().mockResolvedValue(true);
    const utils = render(
      <Board
        cards={cards}
        config={manualConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
        onCardMove={onCardMove}
      />,
    );
    return { onCardMove, ...utils };
  }

  // Reads the card ids of one column, in rendered (visual) order.
  function columnCardIds(headingName: RegExp): string[] {
    const heading = screen.getByRole('heading', { name: headingName });
    const column = heading.closest('[data-accent="stripe"]')!;
    return Array.from(column.querySelectorAll('[aria-label^="Card MAN"]')).map(
      (el) => el.getAttribute('aria-label')!.match(/^Card (\S+):/)![1],
    );
  }

  function sortTrigger(columnIndex: number): HTMLElement {
    return screen.getAllByRole('button', { name: /select sort order/i })[columnIndex];
  }

  function isManualChecked(): boolean {
    return screen.getByText('Manual').closest('button')!.getAttribute('aria-checked') === 'true';
  }

  it('a same-column drop reorders the column and flips its sort menu to Manual', () => {
    const cards = [cardA(), cardB(), cardC()];
    renderManualBoard(cards);
    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-001', 'MAN-002', 'MAN-003']);

    act(() => {
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-001', cards[0], 'MAN-003'));
    });

    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-002', 'MAN-003', 'MAN-001']);

    fireEvent.click(sortTrigger(0));
    expect(isManualChecked()).toBe(true);
    fireEvent.click(sortTrigger(0));
  });

  it('the new order survives an unmount/remount', () => {
    const cards = [cardA(), cardB(), cardC()];
    const first = renderManualBoard(cards);

    act(() => {
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-001', cards[0], 'MAN-003'));
    });
    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-002', 'MAN-003', 'MAN-001']);

    first.unmount();
    renderManualBoard(cards);
    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-002', 'MAN-003', 'MAN-001']);
  });

  it('a second drop moves a card back up', () => {
    const cards = [cardA(), cardB(), cardC()];
    renderManualBoard(cards);

    act(() => {
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-001', cards[0], 'MAN-003'));
    });
    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-002', 'MAN-003', 'MAN-001']);

    act(() => {
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-001', cards[0], 'MAN-002'));
    });
    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-001', 'MAN-002', 'MAN-003']);
  });

  it('selecting Manual from the menu with nothing stored leaves the visible order untouched', () => {
    const cards = [cardA(), cardB(), cardC()];
    renderManualBoard(cards);
    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-001', 'MAN-002', 'MAN-003']);

    fireEvent.click(sortTrigger(0));
    fireEvent.click(screen.getByText('Manual'));

    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-001', 'MAN-002', 'MAN-003']);
  });

  it('switching to Priority and back to Manual restores the hand order', () => {
    const cards = [
      cardA(),
      makeCard('MAN-002', 'todo', { updated: '2026-01-02T00:00:00Z', priority: 'critical' }),
      cardC(),
    ];
    renderManualBoard(cards);

    act(() => {
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-001', cards[0], 'MAN-003'));
    });
    const handOrder = columnCardIds(/Backlog column/);
    expect(handOrder).toEqual(['MAN-002', 'MAN-003', 'MAN-001']);

    fireEvent.click(sortTrigger(0));
    fireEvent.click(screen.getByText('Priority'));
    // Most urgent first: critical (MAN-002), then the medium-priority tie
    // broken by original list order (MAN-001, MAN-003).
    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-002', 'MAN-001', 'MAN-003']);

    fireEvent.click(sortTrigger(0));
    fireEvent.click(screen.getByText('Manual'));
    expect(columnCardIds(/Backlog column/)).toEqual(handOrder);
  });

  it('a card not in the stored order renders below the known ones, newest first', () => {
    const a = cardA();
    const b = cardB();
    const { rerender } = renderManualBoard([a, b]);

    act(() => {
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-002', b, 'MAN-001'));
    });
    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-002', 'MAN-001']);

    // Two cards arrive after the hand order was recorded - neither was ever
    // seen by the stored order, so both sort below it, newest first.
    const d = makeCard('MAN-004', 'todo', { updated: '2026-01-05T00:00:00Z' });
    const e = makeCard('MAN-005', 'todo', { updated: '2026-01-04T00:00:00Z' });
    rerender(
      <Board
        cards={[a, b, d, e]}
        config={manualConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
        onCardMove={vi.fn<(id: string, state: string) => Promise<boolean>>().mockResolvedValue(true)}
      />,
    );

    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-002', 'MAN-001', 'MAN-004', 'MAN-005']);
  });

  it('a cross-column drop does not switch the target column to Manual', () => {
    const todoCard = makeCard('MAN-001', 'todo');
    const doneCard = makeCard('MAN-010', 'done');
    const { onCardMove } = renderManualBoard([todoCard, doneCard]);

    act(() => {
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-001', todoCard, 'MAN-010'));
    });
    expect(onCardMove).toHaveBeenCalledWith('MAN-001', 'done');

    fireEvent.click(sortTrigger(1));
    expect(isManualChecked()).toBe(false);
  });

  it('manual order is per column: todo manual while done stays on recent', () => {
    const cards = [
      cardA(),
      cardB(),
      makeCard('MAN-010', 'done', { updated: '2026-01-02T00:00:00Z' }),
      makeCard('MAN-011', 'done', { updated: '2026-01-01T00:00:00Z' }),
    ];
    renderManualBoard(cards);

    act(() => {
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-001', cards[0], 'MAN-002'));
    });

    fireEvent.click(sortTrigger(0));
    expect(isManualChecked()).toBe(true);
    fireEvent.click(sortTrigger(0));

    fireEvent.click(sortTrigger(1));
    expect(isManualChecked()).toBe(false);
    fireEvent.click(sortTrigger(1));

    // Done is still "recent": MAN-010 (Jan 2) before MAN-011 (Jan 1).
    expect(columnCardIds(/Shipped column/)).toEqual(['MAN-010', 'MAN-011']);
  });

  it('a same-column drop on the column body is a no-op', () => {
    const cards = [cardA(), cardB(), cardC()];
    renderManualBoard(cards);
    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-001', 'MAN-002', 'MAN-003']);

    act(() => {
      // over.id is the column itself, not a card - a release over the empty
      // area below the last card or over the header.
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-001', cards[0], 'todo'));
    });

    expect(columnCardIds(/Backlog column/)).toEqual(['MAN-001', 'MAN-002', 'MAN-003']);
    fireEvent.click(sortTrigger(0));
    expect(isManualChecked()).toBe(false);
    fireEvent.click(sortTrigger(0));
    expect(localStorage.getItem('contextmatrix-manual-order-manual-test')).toBeNull();
  });

  it('a cross-column drop into a target column already on manual records the dropped position', async () => {
    localStorage.setItem('contextmatrix-column-sort-manual-test', '{"done":"manual"}');
    const cards = [
      makeCard('MAN-001', 'todo'),
      makeCard('MAN-010', 'done', { updated: '2026-01-02T00:00:00Z' }),
      makeCard('MAN-011', 'done', { updated: '2026-01-01T00:00:00Z' }),
    ];
    const { onCardMove } = renderManualBoard(cards);

    act(() => {
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-001', cards[0], 'MAN-011'));
    });

    await waitFor(() => {
      expect(localStorage.getItem('contextmatrix-manual-order-manual-test')).not.toBeNull();
    });
    expect(onCardMove).toHaveBeenCalledWith('MAN-001', 'done');
    expect(JSON.parse(localStorage.getItem('contextmatrix-manual-order-manual-test')!)).toEqual({
      done: ['MAN-010', 'MAN-001', 'MAN-011'],
    });
  });

  it('an onCardMove resolving false records no order for the target column', async () => {
    localStorage.setItem('contextmatrix-column-sort-manual-test', '{"done":"manual"}');
    const cards = [
      makeCard('MAN-001', 'todo'),
      makeCard('MAN-010', 'done', { updated: '2026-01-02T00:00:00Z' }),
      makeCard('MAN-011', 'done', { updated: '2026-01-01T00:00:00Z' }),
    ];
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const onCardMove = vi.fn<(id: string, state: string) => Promise<boolean>>().mockResolvedValue(false);
    render(
      <Board
        cards={cards}
        config={manualConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
        onCardMove={onCardMove}
      />,
    );

    act(() => {
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-001', cards[0], 'MAN-011'));
    });

    await waitFor(() => {
      expect(onCardMove).toHaveBeenCalledWith('MAN-001', 'done');
    });
    expect(localStorage.getItem('contextmatrix-manual-order-manual-test')).toBeNull();
  });

  it('an onCardMove that throws warns and records no order for the target column', async () => {
    localStorage.setItem('contextmatrix-column-sort-manual-test', '{"done":"manual"}');
    const cards = [
      makeCard('MAN-001', 'todo'),
      makeCard('MAN-010', 'done', { updated: '2026-01-02T00:00:00Z' }),
      makeCard('MAN-011', 'done', { updated: '2026-01-01T00:00:00Z' }),
    ];
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const onCardMove = vi.fn<(id: string, state: string) => Promise<boolean>>().mockRejectedValue(new Error('move failed'));
    render(
      <Board
        cards={cards}
        config={manualConfig}
        loading={false}
        error={null}
        activeAgents={[]}
        cardsCompletedToday={0}
        activityEntries={[]}
        currentAgent={null}
        onCardMove={onCardMove}
      />,
    );

    act(() => {
      capturedDndProps.onDragEnd!(dragEndEvent('MAN-001', cards[0], 'MAN-011'));
    });

    await waitFor(() => {
      expect(warnSpy).toHaveBeenCalled();
    });
    expect(localStorage.getItem('contextmatrix-manual-order-manual-test')).toBeNull();

    warnSpy.mockRestore();
  });
});

// ---------------------------------------------------------------------------
// Board - collapsed header
// ---------------------------------------------------------------------------

describe('Board - collapsed header', () => {
  const baseProps = {
    config: baseConfig,
    loading: false,
    error: null,
    activeAgents: [],
    cardsCompletedToday: 0,
    activityEntries: [],
    currentAgent: null,
  };

  it('replaces the band and ribbon with the micro-band when collapsed', () => {
    render(<Board {...baseProps} cards={[sampleCard]} headerCollapsed />);

    expect(screen.getByRole('heading', { name: 'test-project' })).toBeInTheDocument();
    expect(screen.queryByText('Active agents')).toBeNull();
    expect(screen.queryByText('In flight')).toBeNull();
    expect(screen.getByRole('button', { name: /new card/i })).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/search cards/i)).toBeInTheDocument();
  });

  it('drops the all-clear spotlight strip when collapsed', () => {
    render(<Board {...baseProps} cards={[sampleCard]} headerCollapsed />);
    expect(screen.queryByText('Needs Attention')).toBeNull();
  });

  it('keeps the spotlight strip when a card is stalled', () => {
    const stalledCard: Card = { ...sampleCard, id: 'TEST-002', state: 'stalled' };
    render(<Board {...baseProps} cards={[sampleCard, stalledCard]} headerCollapsed />);
    expect(screen.getByText('Needs Attention')).toBeInTheDocument();
  });

  it('keeps the spotlight strip when a card is blocked', () => {
    const blockedCard: Card = { ...sampleCard, id: 'TEST-003', state: 'blocked' };
    render(<Board {...baseProps} cards={[sampleCard, blockedCard]} headerCollapsed />);
    expect(screen.getByText('Needs Attention')).toBeInTheDocument();
  });

  it('leaves the expanded header unchanged', () => {
    render(<Board {...baseProps} cards={[sampleCard]} headerCollapsed={false} />);
    expect(screen.getByText('Active agents')).toBeInTheDocument();
    expect(screen.getByText('Needs Attention')).toBeInTheDocument();
    expect(screen.getByText(/no stalled or blocked cards/i)).toBeInTheDocument();
  });

  it('wires the toggle into the expanded band', () => {
    const onToggle = vi.fn();
    render(<Board {...baseProps} cards={[sampleCard]} headerCollapsed={false} onToggleHeaderCollapsed={onToggle} />);
    fireEvent.click(screen.getByRole('button', { name: /collapse board header/i }));
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it('wires the toggle into the micro-band', () => {
    const onToggle = vi.fn();
    render(<Board {...baseProps} cards={[sampleCard]} headerCollapsed onToggleHeaderCollapsed={onToggle} />);
    fireEvent.click(screen.getByRole('button', { name: /expand board header/i }));
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it('renders no header toggle without a handler', () => {
    render(<Board {...baseProps} cards={[sampleCard]} headerCollapsed={false} />);
    expect(screen.queryByRole('button', { name: /board header/i })).toBeNull();
  });

  it('hands keyboard focus to the replacement toggle across the band swap', () => {
    function Harness() {
      const [collapsed, setCollapsed] = useState(false);
      return (
        <Board
          {...baseProps}
          cards={[sampleCard]}
          headerCollapsed={collapsed}
          onToggleHeaderCollapsed={() => setCollapsed((c) => !c)}
        />
      );
    }
    render(<Harness />);

    const collapseBtn = screen.getByRole('button', { name: /collapse board header/i });
    collapseBtn.focus();
    fireEvent.click(collapseBtn);

    const expandBtn = screen.getByRole('button', { name: /expand board header/i });
    expect(document.activeElement).toBe(expandBtn);

    fireEvent.click(expandBtn);
    expect(document.activeElement).toBe(screen.getByRole('button', { name: /collapse board header/i }));
  });

  it('leaves focus alone when the toggle was not focused', () => {
    function Harness() {
      const [collapsed, setCollapsed] = useState(false);
      return (
        <Board
          {...baseProps}
          cards={[sampleCard]}
          headerCollapsed={collapsed}
          onToggleHeaderCollapsed={() => setCollapsed((c) => !c)}
        />
      );
    }
    render(<Harness />);

    const search = screen.getByPlaceholderText(/search cards/i);
    search.focus();
    fireEvent.click(screen.getByRole('button', { name: /collapse board header/i }));

    expect(screen.getByRole('button', { name: /expand board header/i })).toBeInTheDocument();
    expect(document.activeElement).toBe(search);
  });
});

// ---------------------------------------------------------------------------
// Board header: actions slot + sidebar opener reach whichever band is showing
// ---------------------------------------------------------------------------
describe('Board header actions slot', () => {
  const originalMatchMedia = window.matchMedia;
  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', { writable: true, value: originalMatchMedia });
  });

  const boardProps = {
    cards: [sampleCard],
    config: baseConfig,
    loading: false,
    error: null,
    activeAgents: [],
    cardsCompletedToday: 0,
    activityEntries: [],
    currentAgent: null,
  };

  it('renders headerActions next to New Card in the expanded band', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const { container } = render(
      <Board {...boardProps} headerCollapsed={false} headerActions={<button type="button">Console</button>} />
    );
    expect(container.querySelector('.board-band__actions')).toContainElement(
      screen.getByRole('button', { name: 'Console' })
    );
  });

  it('renders headerActions in the micro-band when collapsed', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const { container } = render(
      <Board {...boardProps} headerCollapsed headerActions={<button type="button">Console</button>} />
    );
    expect(container.querySelector('.board-microband__actions')).toContainElement(
      screen.getByRole('button', { name: 'Console' })
    );
  });

  it('forwards onOpenSidebar to the band as the menu button', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const onOpenSidebar = vi.fn();
    render(<Board {...boardProps} onOpenSidebar={onOpenSidebar} />);
    fireEvent.click(screen.getByRole('button', { name: /open menu/i }));
    expect(onOpenSidebar).toHaveBeenCalledTimes(1);
  });

  it('keeps the sidebar opener in the error and loading states', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const onOpenSidebar = vi.fn();
    const { rerender } = render(<Board {...boardProps} error="boom" onOpenSidebar={onOpenSidebar} />);
    expect(screen.getByText('boom')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /open menu/i }));
    expect(onOpenSidebar).toHaveBeenCalledTimes(1);

    rerender(<Board {...boardProps} loading onOpenSidebar={onOpenSidebar} />);
    expect(screen.getByRole('button', { name: /open menu/i })).toBeInTheDocument();
  });

  it('names the route project in the band-less crumb even when config is stale', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    render(<Board {...boardProps} projectName="fresh" error="boom" />);
    expect(screen.getByText('fresh')).toBeInTheDocument();
    expect(screen.queryByText(baseConfig.name)).toBeNull();
  });
});
