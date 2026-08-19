import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useColumnSort } from './useColumnSort';
import type { SortMode } from '../types';

const PROJECT = 'test-project';
const STORAGE_KEY = `contextmatrix-column-sort-${PROJECT}`;
const STATES = ['todo', 'in_progress', 'review', 'done'];

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

describe('useColumnSort', () => {
  it('returns "recent" for any unset state', () => {
    const { result } = renderHook(() => useColumnSort(PROJECT, STATES));
    const [getSort] = result.current;

    expect(getSort('todo')).toBe('recent');
    expect(getSort('in_progress')).toBe('recent');
    expect(getSort('review')).toBe('recent');
  });

  it('setSort writes to localStorage and getSort returns the set value', () => {
    const { result } = renderHook(() => useColumnSort(PROJECT, STATES));
    const [, setSort] = result.current;

    act(() => {
      setSort('todo', 'priority');
    });

    const [getSort] = result.current;
    expect(getSort('todo')).toBe('priority');
    expect(getSort('in_progress')).toBe('recent'); // others unchanged

    // Verify localStorage write
    const writes = localStorageMock.setItem.mock.calls.filter(
      ([key]) => key === STORAGE_KEY,
    );
    expect(writes).toHaveLength(1);
    const saved = JSON.parse(writes[0][1]) as Record<string, SortMode>;
    expect(saved).toEqual({ todo: 'priority' });
  });

  it('setSort on multiple states preserves all entries', () => {
    const { result } = renderHook(() => useColumnSort(PROJECT, STATES));
    const [, setSort] = result.current;

    act(() => {
      setSort('todo', 'id-asc');
    });
    act(() => {
      setSort('in_progress', 'priority');
    });

    const [getSort] = result.current;
    expect(getSort('todo')).toBe('id-asc');
    expect(getSort('in_progress')).toBe('priority');
    expect(getSort('review')).toBe('recent');
    expect(getSort('done')).toBe('recent');
  });

  it('project change re-reads from storage', () => {
    // Pre-populate storage for a different project
    localStorageMock.setItem(
      'contextmatrix-column-sort-other-project',
      JSON.stringify({ todo: 'id-desc' }),
    );

    const { result, rerender } = renderHook(
      ({ project, states }: { project: string; states: string[] }) =>
        useColumnSort(project, states),
      { initialProps: { project: PROJECT, states: STATES } },
    );

    // Set a sort on the first project
    act(() => {
      result.current[1]('todo', 'priority');
    });

    // Switch to other project
    rerender({ project: 'other-project', states: STATES });

    const [getSort] = result.current;
    expect(getSort('todo')).toBe('id-desc');
    expect(getSort('in_progress')).toBe('recent');
  });

  it('orphaned states are pruned on load', () => {
    localStorageMock.setItem(
      STORAGE_KEY,
      JSON.stringify({ todo: 'priority', ghost_state: 'id-asc', done: 'type' }),
    );

    const { result } = renderHook(() => useColumnSort(PROJECT, STATES));

    const [getSort] = result.current;
    expect(getSort('todo')).toBe('priority');
    expect(getSort('done')).toBe('type');
    // ghost_state was pruned - no effect
  });

  it('orphaned states are pruned on setSort', () => {
    // Pre-populate with an orphan
    localStorageMock.setItem(
      STORAGE_KEY,
      JSON.stringify({ todo: 'priority', ghost: 'id-desc' }),
    );

    const { result } = renderHook(() => useColumnSort(PROJECT, STATES));

    // setSort triggers a prune
    act(() => {
      result.current[1]('in_progress', 'type');
    });

    // Verify localStorage does not contain ghost
    const writes = localStorageMock.setItem.mock.calls.filter(
      ([key]) => key === STORAGE_KEY,
    );
    const lastWrite = JSON.parse(writes[writes.length - 1][1]) as Record<string, SortMode>;
    expect(lastWrite).not.toHaveProperty('ghost');
    expect(lastWrite).toHaveProperty('todo', 'priority');
    expect(lastWrite).toHaveProperty('in_progress', 'type');
  });

  it('ignores a stored mode that is not a known sort mode', () => {
    localStorageMock.setItem(
      STORAGE_KEY,
      JSON.stringify({ todo: 'bogus', done: 'priority' }),
    );

    const { result } = renderHook(() => useColumnSort(PROJECT, STATES));

    const [getSort] = result.current;
    expect(getSort('todo')).toBe('recent');
    expect(getSort('done')).toBe('priority');
  });

  it('state survives re-mount via localStorage', () => {
    const { result, unmount } = renderHook(() => useColumnSort(PROJECT, STATES));

    act(() => {
      result.current[1]('review', 'id-asc');
    });
    unmount();

    const { result: result2 } = renderHook(() => useColumnSort(PROJECT, STATES));
    const [getSort] = result2.current;
    expect(getSort('review')).toBe('id-asc');
    expect(getSort('todo')).toBe('recent');
  });

  it('setSort with "manual" persists and survives re-mount', () => {
    const { result, unmount } = renderHook(() => useColumnSort(PROJECT, STATES));

    act(() => {
      result.current[1]('todo', 'manual');
    });

    const [getSort] = result.current;
    expect(getSort('todo')).toBe('manual');

    const writes = localStorageMock.setItem.mock.calls.filter(
      ([key]) => key === STORAGE_KEY,
    );
    const lastWrite = JSON.parse(writes[writes.length - 1][1]) as Record<string, SortMode>;
    expect(lastWrite).toEqual({ todo: 'manual' });

    unmount();

    const { result: result2 } = renderHook(() => useColumnSort(PROJECT, STATES));
    const [getSort2] = result2.current;
    expect(getSort2('todo')).toBe('manual');
  });
});
