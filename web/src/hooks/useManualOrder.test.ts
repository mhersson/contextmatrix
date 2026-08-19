import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import React from 'react';
import { useManualOrder } from './useManualOrder';
import type { Card } from '../types';

const PROJECT = 'test-project';
const STORAGE_KEY = `contextmatrix-manual-order-${PROJECT}`;
const STATES = ['todo', 'in_progress', 'review', 'done'];

function card(id: string, state: string): Card {
  return {
    id,
    title: id,
    project: PROJECT,
    type: 'task',
    state,
    priority: 'medium',
    body: '',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
  };
}

// Mock localStorage
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
    removeItem: vi.fn((key: string) => { delete store[key]; }),
    clear: vi.fn(() => { store = {}; }),
  };
})();

Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock });

beforeEach(() => {
  localStorageMock.clear();
  vi.clearAllMocks();
});

describe('useManualOrder', () => {
  it('getOrder is empty for an unset state', () => {
    const cards = [card('A-1', 'todo')];
    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    expect(result.current.getOrder('todo')).toEqual([]);
  });

  it('hasOrder is false then true after setOrder', () => {
    const cards = [card('A-1', 'todo')];
    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    expect(result.current.hasOrder('todo')).toBe(false);

    act(() => {
      result.current.setOrder('todo', ['A-1']);
    });

    expect(result.current.hasOrder('todo')).toBe(true);
  });

  it('setOrder persists and getOrder returns the set value (round-trip)', () => {
    const cards = [card('A-1', 'todo'), card('A-2', 'todo')];
    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    act(() => {
      result.current.setOrder('todo', ['A-2', 'A-1']);
    });

    expect(result.current.getOrder('todo')).toEqual(['A-2', 'A-1']);

    const writes = localStorageMock.setItem.mock.calls.filter(
      ([key]) => key === STORAGE_KEY,
    );
    expect(writes).toHaveLength(1);
    const saved = JSON.parse(writes[0][1] as string) as Record<string, string[]>;
    expect(saved).toEqual({ todo: ['A-2', 'A-1'] });
  });

  it('project change loads the other project record', () => {
    localStorageMock.setItem(
      'contextmatrix-manual-order-other-project',
      JSON.stringify({ todo: ['B-1'] }),
    );

    const cardsA = [card('A-1', 'todo')];
    const cardsB = [card('B-1', 'todo')];

    const { result, rerender } = renderHook(
      ({ project, cards, states }: { project: string; cards: Card[]; states: string[] }) =>
        useManualOrder(project, cards, states),
      { initialProps: { project: PROJECT, cards: cardsA, states: STATES } },
    );

    act(() => {
      result.current.setOrder('todo', ['A-1']);
    });

    rerender({ project: 'other-project', cards: cardsB, states: STATES });

    expect(result.current.getOrder('todo')).toEqual(['B-1']);
  });

  it('a setOrder captured before a project switch writes only its own project', () => {
    localStorageMock.setItem(STORAGE_KEY, JSON.stringify({ todo: ['A-1', 'A-2'] }));
    localStorageMock.setItem(
      'contextmatrix-manual-order-other-project',
      JSON.stringify({ todo: ['B-1'] }),
    );

    const cardsA = [card('A-1', 'todo'), card('A-2', 'todo')];
    const cardsB = [card('B-1', 'todo')];

    const { result, rerender } = renderHook(
      ({ project, cards, states }: { project: string; cards: Card[]; states: string[] }) =>
        useManualOrder(project, cards, states),
      { initialProps: { project: PROJECT, cards: cardsA, states: STATES } },
    );

    // Captured while on the first project, invoked after switching - the shape
    // of a cross-column write deferred until its move's request resolves.
    const staleSetOrder = result.current.setOrder;

    rerender({ project: 'other-project', cards: cardsB, states: STATES });

    act(() => {
      staleSetOrder('review', ['A-1']);
    });

    const savedA = JSON.parse(
      localStorageMock.getItem(STORAGE_KEY) as string,
    ) as Record<string, string[]>;

    // The first project's own hand order survives, and none of the second
    // project's ids leak into its record.
    expect(savedA.todo).toEqual(['A-1', 'A-2']);
    expect(savedA.review).toEqual(['A-1']);
    expect(JSON.stringify(savedA)).not.toContain('B-1');

    const savedB = JSON.parse(
      localStorageMock.getItem('contextmatrix-manual-order-other-project') as string,
    ) as Record<string, string[]>;
    expect(savedB.todo).toEqual(['B-1']);
  });

  it('prune drops an id with no matching card', () => {
    localStorageMock.setItem(STORAGE_KEY, JSON.stringify({ todo: ['A-1', 'A-2'] }));
    const cards = [card('A-1', 'todo')]; // A-2 does not exist

    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    expect(result.current.getOrder('todo')).toEqual(['A-1']);
  });

  it('prune drops an id whose card became done while stored under todo', () => {
    localStorageMock.setItem(STORAGE_KEY, JSON.stringify({ todo: ['A-1'] }));
    const cards = [card('A-1', 'done')];

    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    expect(result.current.getOrder('todo')).toEqual([]);
  });

  it('prune keeps a done card stored under the done key', () => {
    localStorageMock.setItem(STORAGE_KEY, JSON.stringify({ done: ['A-1'] }));
    const cards = [card('A-1', 'done')];

    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    expect(result.current.getOrder('done')).toEqual(['A-1']);
  });

  it('prune keeps an id whose card moved to another non-terminal column', () => {
    localStorageMock.setItem(STORAGE_KEY, JSON.stringify({ todo: ['A-1'] }));
    const cards = [card('A-1', 'in_progress')];

    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    expect(result.current.getOrder('todo')).toEqual(['A-1']);
  });

  it('prune drops unknown state keys', () => {
    localStorageMock.setItem(
      STORAGE_KEY,
      JSON.stringify({ todo: ['A-1'], ghost_state: ['A-1'] }),
    );
    const cards = [card('A-1', 'todo')];

    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    expect(result.current.getOrder('todo')).toEqual(['A-1']);
    expect(result.current.hasOrder('ghost_state')).toBe(false);
  });

  it('ignores a stored value that is a string instead of a record', () => {
    localStorageMock.setItem(STORAGE_KEY, JSON.stringify('not-a-record'));
    const cards = [card('A-1', 'todo')];

    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    expect(result.current.getOrder('todo')).toEqual([]);
  });

  it('ignores a non-array value for a state key', () => {
    localStorageMock.setItem(STORAGE_KEY, JSON.stringify({ todo: 'A-1' }));
    const cards = [card('A-1', 'todo')];

    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    expect(result.current.getOrder('todo')).toEqual([]);
  });

  it('ignores non-string entries within a stored array', () => {
    localStorageMock.setItem(STORAGE_KEY, JSON.stringify({ todo: ['A-1', 42, null, {}] }));
    const cards = [card('A-1', 'todo')];

    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    expect(result.current.getOrder('todo')).toEqual(['A-1']);
  });

  it('collapses duplicate ids keeping the first', () => {
    localStorageMock.setItem(STORAGE_KEY, JSON.stringify({ todo: ['A-1', 'A-2', 'A-1'] }));
    const cards = [card('A-1', 'todo'), card('A-2', 'todo')];

    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    expect(result.current.getOrder('todo')).toEqual(['A-1', 'A-2']);
  });

  it('runs the structural prune but not membership pruning while cards is empty (board still loading)', () => {
    localStorageMock.setItem(
      STORAGE_KEY,
      JSON.stringify({ todo: ['A-1', 'A-2'], ghost_state: ['X-1'] }),
    );

    const { result } = renderHook(() => useManualOrder(PROJECT, [], STATES));

    // todo's ids survive untouched - membership pruning (no matching card)
    // must not run before the board has loaded.
    expect(result.current.getOrder('todo')).toEqual(['A-1', 'A-2']);
    // ghost_state is not a known column, so the structural pass drops it
    // regardless of whether the board has loaded.
    expect(result.current.getOrder('ghost_state')).toEqual([]);
  });

  it('drops and persists structural garbage while cards is empty (board still loading)', () => {
    localStorageMock.setItem(
      STORAGE_KEY,
      JSON.stringify({
        todo: ['A-1', 'A-2', 'A-1', 42, null],
        ghost_state: ['X-1'],
        done: 'not-an-array',
      }),
    );

    const { result } = renderHook(() => useManualOrder(PROJECT, [], STATES));

    expect(result.current.getOrder('todo')).toEqual(['A-1', 'A-2']);

    const writes = localStorageMock.setItem.mock.calls.filter(([key]) => key === STORAGE_KEY);
    const lastWrite = writes[writes.length - 1];
    const saved = JSON.parse(lastWrite[1] as string) as Record<string, string[]>;
    expect(saved).toEqual({ todo: ['A-1', 'A-2'] });
  });

  it('a card stored under todo that becomes done is pruned from localStorage, not only from getOrder', () => {
    const cards = [card('A-1', 'todo'), card('A-2', 'todo')];
    const { result, rerender } = renderHook(
      ({ cards }: { cards: Card[] }) => useManualOrder(PROJECT, cards, STATES),
      { initialProps: { cards } },
    );

    act(() => {
      result.current.setOrder('todo', ['A-1', 'A-2']);
    });

    rerender({ cards: [card('A-1', 'done'), card('A-2', 'todo')] });

    expect(result.current.getOrder('todo')).toEqual(['A-2']);

    const writes = localStorageMock.setItem.mock.calls.filter(([key]) => key === STORAGE_KEY);
    const lastWrite = writes[writes.length - 1];
    const saved = JSON.parse(lastWrite[1] as string) as Record<string, string[]>;
    expect(saved).toEqual({ todo: ['A-2'] });
  });

  it('a card that returns to todo after being pruned as done does not restore to its old slot', () => {
    const cards = [card('A-1', 'todo'), card('A-2', 'todo')];
    const { result, rerender } = renderHook(
      ({ cards }: { cards: Card[] }) => useManualOrder(PROJECT, cards, STATES),
      { initialProps: { cards } },
    );

    act(() => {
      result.current.setOrder('todo', ['A-1', 'A-2']);
    });

    // A-1 finishes, is pruned and persisted, then comes back to todo.
    rerender({ cards: [card('A-1', 'done'), card('A-2', 'todo')] });
    rerender({ cards: [card('A-1', 'todo'), card('A-2', 'todo')] });

    expect(result.current.getOrder('todo')).toEqual(['A-2']);
  });

  it('a deleted card (absent from cards) is pruned from localStorage, not only from getOrder', () => {
    const cards = [card('A-1', 'todo'), card('A-2', 'todo')];
    const { result, rerender } = renderHook(
      ({ cards }: { cards: Card[] }) => useManualOrder(PROJECT, cards, STATES),
      { initialProps: { cards } },
    );

    act(() => {
      result.current.setOrder('todo', ['A-1', 'A-2']);
    });

    rerender({ cards: [card('A-2', 'todo')] }); // A-1 deleted

    expect(result.current.getOrder('todo')).toEqual(['A-2']);

    const writes = localStorageMock.setItem.mock.calls.filter(([key]) => key === STORAGE_KEY);
    const lastWrite = writes[writes.length - 1];
    const saved = JSON.parse(lastWrite[1] as string) as Record<string, string[]>;
    expect(saved).toEqual({ todo: ['A-2'] });
  });

  it('two setOrder calls from the same render, on different columns, both survive (lost update regression)', () => {
    const cards = [card('A-1', 'todo'), card('A-2', 'done')];
    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES));

    // Both calls happen synchronously within the same act(), so they share
    // the exact same setOrder closure - no render runs between them. A
    // merge that reads a closed-over `record` instead of a live ref loses
    // the first write when the second one lands.
    act(() => {
      result.current.setOrder('todo', ['A-1']);
      result.current.setOrder('done', ['A-2']);
    });

    expect(result.current.getOrder('todo')).toEqual(['A-1']);
    expect(result.current.getOrder('done')).toEqual(['A-2']);
  });

  it('a setOrder captured before a stale closure does not revert a later write when invoked after it', () => {
    const cardsV1 = [card('A-1', 'todo'), card('A-2', 'done')];
    const { result, rerender } = renderHook(
      ({ cards }: { cards: Card[] }) => useManualOrder(PROJECT, cards, STATES),
      { initialProps: { cards: cardsV1 } },
    );

    // Capture setOrder now - this simulates a cross-column drop whose write
    // is deferred behind an API round-trip.
    const deferredSetOrder = result.current.setOrder;

    // A cards-identity change (e.g. an SSE update) gives the hook's current
    // setOrder a new closure, so `deferredSetOrder` is now genuinely stale.
    rerender({ cards: [card('A-1', 'todo'), card('A-2', 'done')] });

    // A later write lands first - e.g. a same-column reorder that completes
    // synchronously while the deferred cross-column drop is still in flight.
    act(() => {
      result.current.setOrder('done', ['A-2']);
    });

    expect(result.current.getOrder('done')).toEqual(['A-2']);

    // The deferred write now resolves and lands after the later write. It
    // must merge from the current record, not the one at capture time.
    act(() => {
      deferredSetOrder('todo', ['A-1']);
    });

    expect(result.current.getOrder('todo')).toEqual(['A-1']);
    expect(result.current.getOrder('done')).toEqual(['A-2']);
  });

  it('getOrder returns [] and hasOrder returns false for a state name colliding with an Object.prototype member', () => {
    const cards = [card('A-1', 'todo')];
    const { result } = renderHook(() =>
      useManualOrder(PROJECT, cards, ['todo', 'constructor']),
    );

    expect(result.current.getOrder('constructor')).toEqual([]);
    expect(result.current.hasOrder('constructor')).toBe(false);
  });

  it('a drop out of a terminal state into a manual column keeps the dropped position', () => {
    // Mirrors production timing: Board.tsx's deferred write holds a setOrder
    // closure captured before the move (its own internal `cards` still shows
    // A-1 as 'not_planned'), but by the time that deferred write actually
    // runs, useCardActions' optimistic update has already landed - the
    // hook's current `cards` prop shows A-1 as 'todo'.
    const cardsBeforeMove = [card('A-1', 'not_planned'), card('A-2', 'todo')];
    const { result, rerender } = renderHook(
      ({ cards }: { cards: Card[] }) =>
        useManualOrder(PROJECT, cards, ['todo', 'not_planned', 'done']),
      { initialProps: { cards: cardsBeforeMove } },
    );

    const deferredSetOrder = result.current.setOrder;

    rerender({ cards: [card('A-1', 'todo'), card('A-2', 'todo')] });

    // The deferred write resolves. Its own closure still carries the
    // pre-move `cards` (A-1 as 'not_planned'), so without the override the
    // membership pass would drop A-1 right back out.
    // movedIdState is left off so the `?? column` default is what is under
    // test - that is the form Board.tsx actually calls.
    act(() => {
      deferredSetOrder('todo', ['A-2', 'A-1'], 'A-1');
    });

    expect(result.current.getOrder('todo')).toEqual(['A-2', 'A-1']);
  });

  it('setOrder performs exactly one localStorage.setItem under StrictMode', () => {
    const cards = [card('A-1', 'todo'), card('A-2', 'todo')];
    const { result } = renderHook(() => useManualOrder(PROJECT, cards, STATES), {
      wrapper: ({ children }) => React.createElement(React.StrictMode, null, children),
    });

    localStorageMock.setItem.mockClear();

    act(() => {
      result.current.setOrder('todo', ['A-2', 'A-1']);
    });

    const writes = localStorageMock.setItem.mock.calls.filter(([key]) => key === STORAGE_KEY);
    expect(writes).toHaveLength(1);
  });
});
