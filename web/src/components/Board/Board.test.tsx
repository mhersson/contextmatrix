import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, within, waitFor } from '@testing-library/react';
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
// correct sensors prop. We capture it via a spy on DndContext.
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
    useDroppable: () => ({
      setNodeRef: () => {},
      isOver: false,
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
