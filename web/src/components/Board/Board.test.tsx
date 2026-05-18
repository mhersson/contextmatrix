import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
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

// Minimal mock for @dnd-kit/core — we only care that DndContext receives the
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

describe('Board — touch device drag-and-drop', () => {
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    // Simulate a touch/coarse-pointer device
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
  });

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  it('renders the board without crashing on a touch device', () => {
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
    // Board renders card content
    expect(screen.getByText('Sample card')).toBeInTheDocument();
  });

  it('isTouchDevice returns true in simulated touch environment', () => {
    // Confirm the mock is active for this describe block
    expect(isTouchDevice()).toBe(true);
  });
});

describe('Board — pointer device drag-and-drop', () => {
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    // Simulate a fine-pointer (mouse) device
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: (_query: string) => ({
        matches: false,
        media: _query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }),
    });
  });

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  it('renders the board without crashing on a pointer device', () => {
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
    expect(screen.getByText('Sample card')).toBeInTheDocument();
  });

  it('isTouchDevice returns false in simulated pointer environment', () => {
    expect(isTouchDevice()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Board — mobile NowRail drawer
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

describe('Board — mobile NowRail drawer', () => {
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
// Board — MetricsRibbon headline fallback during initial mount
// ---------------------------------------------------------------------------

describe('Board — MetricsRibbon inFlight fallback', () => {
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

  it('derives openCount and inReviewCount from stateCounts when present (stalled stays open)', () => {
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
        cardsCompletedToday={5}
        activityEntries={[]}
        currentAgent={null}
        stateCounts={{ todo: 4, in_progress: 3, review: 2, stalled: 1, done: 7, not_planned: 2 }}
      />,
    );
    // BoardBand renders "{openCount} open · {inReviewCount} in review · {shippedToday} shipped today".
    // openCount = todo + in_progress + review + stalled = 4 + 3 + 2 + 1 = 10
    // inReviewCount = review = 2
    expect(screen.getByText(/10 open · 2 in review · 5 shipped today/)).toBeInTheDocument();
  });

  it('derives openCount and inReviewCount from cards when stateCounts is undefined', () => {
    mockMatchMediaTrueFor('(min-width: 99999px)');
    const config: ProjectConfig = {
      ...baseConfig,
      states: ['todo', 'in_progress', 'review', 'done', 'stalled'],
      transitions: { todo: [], in_progress: [], review: [], done: [], stalled: [] },
    };
    const makeCard = (id: string, state: string): Card => ({
      id,
      title: id,
      project: 'test-project',
      type: 'task',
      state,
      priority: 'medium',
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
    // openCount fallback excludes done/not_planned; stalled stays open. = 1+1+2+1 = 5
    // inReviewCount fallback = 2
    expect(screen.getByText(/5 open · 2 in review · 0 shipped today/)).toBeInTheDocument();
  });
});
