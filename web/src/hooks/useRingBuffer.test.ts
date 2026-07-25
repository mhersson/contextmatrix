import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useRingBuffer } from './useRingBuffer';
import type { LogEntry } from '../types';

function makeEntry(content: string, seq?: number): LogEntry {
  return {
    ts: new Date().toISOString(),
    card_id: 'TEST-001',
    type: 'text',
    content,
    seq,
  };
}

/** Advance past the coalescing window so deferred notifications land. */
function flushRing() {
  act(() => {
    vi.advanceTimersByTime(50);
  });
}

describe('useRingBuffer', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('starts with an empty logs array', () => {
    const { result } = renderHook(() => useRingBuffer(10));
    expect(result.current.logs).toHaveLength(0);
  });

  it('appends entries in order', () => {
    const { result } = renderHook(() => useRingBuffer(10));

    act(() => {
      result.current.append([makeEntry('a'), makeEntry('b'), makeEntry('c')]);
    });
    flushRing();

    expect(result.current.logs).toHaveLength(3);
    expect(result.current.logs[0].content).toBe('a');
    expect(result.current.logs[1].content).toBe('b');
    expect(result.current.logs[2].content).toBe('c');
  });

  it('respects capacity and drops oldest entries when full', () => {
    const { result } = renderHook(() => useRingBuffer(3));

    act(() => {
      result.current.append([makeEntry('a'), makeEntry('b'), makeEntry('c')]);
    });
    flushRing();

    expect(result.current.logs).toHaveLength(3);

    // Appending a fourth entry should drop 'a'
    act(() => {
      result.current.append([makeEntry('d')]);
    });
    flushRing();

    expect(result.current.logs).toHaveLength(3);
    expect(result.current.logs[0].content).toBe('b');
    expect(result.current.logs[1].content).toBe('c');
    expect(result.current.logs[2].content).toBe('d');
  });

  it('handles wrap-around correctly across multiple appends', () => {
    const { result } = renderHook(() => useRingBuffer(3));

    // Fill buffer
    act(() => {
      result.current.append([makeEntry('1'), makeEntry('2'), makeEntry('3')]);
    });

    // Overwrite all three slots
    act(() => {
      result.current.append([makeEntry('4'), makeEntry('5'), makeEntry('6')]);
    });
    flushRing();

    expect(result.current.logs).toHaveLength(3);
    expect(result.current.logs[0].content).toBe('4');
    expect(result.current.logs[1].content).toBe('5');
    expect(result.current.logs[2].content).toBe('6');
  });

  it('clear empties the buffer and triggers a re-render', () => {
    const { result } = renderHook(() => useRingBuffer(10));

    act(() => {
      result.current.append([makeEntry('x'), makeEntry('y')]);
    });
    flushRing();

    expect(result.current.logs).toHaveLength(2);

    act(() => {
      result.current.clear();
    });

    // clear notifies synchronously - no timer advance needed.
    expect(result.current.logs).toHaveLength(0);
  });

  it('appending after clear works correctly', () => {
    const { result } = renderHook(() => useRingBuffer(5));

    act(() => {
      result.current.append([makeEntry('a'), makeEntry('b')]);
    });

    act(() => {
      result.current.clear();
    });

    act(() => {
      result.current.append([makeEntry('c')]);
    });
    flushRing();

    expect(result.current.logs).toHaveLength(1);
    expect(result.current.logs[0].content).toBe('c');
  });

  it('logs snapshot is immutable (readonly array)', () => {
    const { result } = renderHook(() => useRingBuffer(10));

    act(() => {
      result.current.append([makeEntry('a')]);
    });
    flushRing();

    // TypeScript enforces readonly, but we can verify at runtime that the
    // snapshot does not change when subsequent appends happen.
    const snapshot = result.current.logs;

    act(() => {
      result.current.append([makeEntry('b')]);
    });
    flushRing();

    // The captured snapshot reference should not be mutated by later appends.
    expect(snapshot).toHaveLength(1);
    expect(result.current.logs).toHaveLength(2);
  });

  it('batch larger than capacity keeps only last capacity entries', () => {
    const { result } = renderHook(() => useRingBuffer(3));

    act(() => {
      result.current.append([
        makeEntry('a'),
        makeEntry('b'),
        makeEntry('c'),
        makeEntry('d'),
        makeEntry('e'),
      ]);
    });
    flushRing();

    expect(result.current.logs).toHaveLength(3);
    expect(result.current.logs[0].content).toBe('c');
    expect(result.current.logs[1].content).toBe('d');
    expect(result.current.logs[2].content).toBe('e');
  });

  it('appending an empty array does nothing and does not re-render', () => {
    const { result } = renderHook(() => useRingBuffer(10));
    const before = result.current.logs;

    act(() => {
      result.current.append([]);
    });
    flushRing();

    // Same reference - no re-render triggered.
    expect(result.current.logs).toBe(before);
  });

  describe('notification coalescing', () => {
    it('coalesces multiple appends in one window into a single notification', () => {
      let renders = 0;
      const { result } = renderHook(() => {
        renders++;
        return useRingBuffer(10);
      });
      const rendersBefore = renders;
      const snapshotBefore = result.current.logs;

      act(() => {
        result.current.append([makeEntry('a')]);
        result.current.append([makeEntry('b')]);
        result.current.append([makeEntry('c')]);
      });

      // Nothing is published until the window elapses.
      expect(result.current.logs).toBe(snapshotBefore);
      expect(renders).toBe(rendersBefore);

      flushRing();

      // One notification carrying all three entries.
      expect(renders).toBe(rendersBefore + 1);
      expect(result.current.logs).toHaveLength(3);
      expect(result.current.logs.map((e) => e.content)).toEqual(['a', 'b', 'c']);
    });

    it('clear with a pending flush notifies immediately and cancels the timer', () => {
      let renders = 0;
      const { result } = renderHook(() => {
        renders++;
        return useRingBuffer(10);
      });

      act(() => {
        result.current.append([makeEntry('a')]);
      });

      act(() => {
        result.current.clear();
      });
      expect(result.current.logs).toHaveLength(0);
      const rendersAfterClear = renders;

      // The cancelled timer must not fire a second notification.
      flushRing();
      expect(renders).toBe(rendersAfterClear);
    });

    it('does not notify when the window elapses with no pending appends', () => {
      let renders = 0;
      renderHook(() => {
        renders++;
        return useRingBuffer(10);
      });
      const rendersBefore = renders;

      flushRing();

      expect(renders).toBe(rendersBefore);
    });
  });
});
