import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useWorkingState } from './useWorkingState';
import type { ChatSession } from '../types';

function session(overrides: Partial<ChatSession>): ChatSession {
  return {
    id: 'S1',
    title: 't',
    status: 'active',
    created_at: '2026-07-24T09:00:00Z',
    last_active: '2026-07-24T09:00:00Z',
    created_by: 'human:test',
    ...overrides,
  };
}

describe('useWorkingState', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-24T10:00:00Z'));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('starts idle and arms optimistically with a verb and local since', () => {
    const { result } = renderHook(() => useWorkingState('S1', session({})));
    expect(result.current.working).toBeNull();

    act(() => result.current.armOptimistic());
    expect(result.current.working).not.toBeNull();
    expect(result.current.working?.since).toBe(Date.now());
    expect(result.current.working?.verb.length).toBeGreaterThan(0);
  });

  it('adopts the server timestamp on assistant_working=true and keeps the verb', () => {
    const { result, rerender } = renderHook(({ s }) => useWorkingState('S1', s), {
      initialProps: { s: session({}) },
    });
    act(() => result.current.armOptimistic());
    const verb = result.current.working?.verb;

    rerender({
      s: session({
        assistant_working: true,
        assistant_working_since: '2026-07-24T09:59:30Z',
      }),
    });
    expect(result.current.working?.since).toBe(Date.parse('2026-07-24T09:59:30Z'));
    expect(result.current.working?.verb).toBe(verb);
  });

  it('clears on a fresh assistant_working=false transition', () => {
    const { result, rerender } = renderHook(({ s }) => useWorkingState('S1', s), {
      initialProps: { s: session({ assistant_working: true, assistant_working_since: '2026-07-24T10:00:00Z' }) },
    });
    expect(result.current.working).not.toBeNull();

    rerender({ s: session({ assistant_working: false }) });
    expect(result.current.working).toBeNull();
  });

  it('does not let a stale false cancel a fresh optimistic arm', () => {
    // Production hazard: sending on a warm-idle session arms optimistically
    // while the merged view still holds the previous turn's explicit false;
    // the server's warm-idle -> active status flip then re-fires the effect
    // with that stale false still in place.
    const { result, rerender } = renderHook(({ s }) => useWorkingState('S1', s), {
      initialProps: { s: session({ assistant_working: false, status: 'warm-idle' }) },
    });

    act(() => result.current.armOptimistic());
    expect(result.current.working).not.toBeNull();

    // Status change re-runs the effect; the stale false must not clear.
    rerender({ s: session({ assistant_working: false, status: 'active' }) });
    expect(result.current.working).not.toBeNull();
  });

  it('clears when the session leaves active', () => {
    const { result, rerender } = renderHook(({ s }) => useWorkingState('S1', s), {
      initialProps: { s: session({ assistant_working: true, assistant_working_since: '2026-07-24T10:00:00Z' }) },
    });
    expect(result.current.working).not.toBeNull();

    rerender({ s: session({ status: 'ending' }) });
    expect(result.current.working).toBeNull();
  });

  it('arms from a bootstrap session that is already working', () => {
    const { result } = renderHook(() =>
      useWorkingState('S1', session({ assistant_working: true, assistant_working_since: '2026-07-24T09:58:00Z' })),
    );
    expect(result.current.working?.since).toBe(Date.parse('2026-07-24T09:58:00Z'));
  });

  it('resets on session switch', () => {
    const { result, rerender } = renderHook(({ id, s }) => useWorkingState(id, s), {
      initialProps: { id: 'S1', s: session({ assistant_working: true, assistant_working_since: '2026-07-24T10:00:00Z' }) },
    });
    expect(result.current.working).not.toBeNull();

    rerender({ id: 'S2', s: session({ id: 'S2' }) });
    expect(result.current.working).toBeNull();
  });

  describe('a send racing the turn', () => {
    const turnEnded = (since: string) =>
      session({ assistant_working: false, assistant_working_since: since });

    it('clears when the turn starts and ends before the send resolves', () => {
      const { result, rerender } = renderHook(({ s }) => useWorkingState('S1', s), {
        initialProps: { s: session({ assistant_working: false }) },
      });
      act(() => result.current.armOptimistic());

      rerender({ s: session({ assistant_working: true, assistant_working_since: '2026-07-24T10:00:01Z' }) });
      rerender({ s: turnEnded('2026-07-24T10:00:01Z') });
      expect(result.current.working).toBeNull();
    });

    it('clears when the true and false updates land in one batch', () => {
      const { result, rerender } = renderHook(({ s }) => useWorkingState('S1', s), {
        initialProps: { s: session({ assistant_working: false }) },
      });
      act(() => result.current.armOptimistic());

      // The hook only sees the final false; its new since proves a turn ran.
      rerender({ s: turnEnded('2026-07-24T10:00:01Z') });
      expect(result.current.working).toBeNull();
    });

    it('stays through an async turn until its false arrives', () => {
      const { result, rerender } = renderHook(({ s }) => useWorkingState('S1', s), {
        initialProps: { s: session({ assistant_working: false }) },
      });
      act(() => result.current.armOptimistic());
      expect(result.current.working).not.toBeNull();

      rerender({ s: session({ assistant_working: true, assistant_working_since: '2026-07-24T10:00:05Z' }) });
      expect(result.current.working).not.toBeNull();

      rerender({ s: turnEnded('2026-07-24T10:00:05Z') });
      expect(result.current.working).toBeNull();
    });

    it('keeps the arm when the stale false of the previous turn is re-delivered', () => {
      const prev = turnEnded('2026-07-24T09:00:00Z');
      const { result, rerender } = renderHook(({ s }) => useWorkingState('S1', s), {
        initialProps: { s: prev },
      });
      act(() => result.current.armOptimistic());

      rerender({ s: { ...prev, context_tokens: 10 } });
      rerender({ s: { ...prev, context_tokens: 10, status: 'active' } });
      expect(result.current.working).not.toBeNull();
    });

    it('disarms when the send fails', () => {
      const { result } = renderHook(() => useWorkingState('S1', session({ assistant_working: false })));
      act(() => result.current.armOptimistic());
      expect(result.current.working).not.toBeNull();

      act(() => result.current.disarmOptimistic());
      expect(result.current.working).toBeNull();
    });

    it('does not drop a server-reported turn when a send fails', () => {
      const { result } = renderHook(() =>
        useWorkingState('S1', session({ assistant_working: true, assistant_working_since: '2026-07-24T10:00:00Z' })),
      );
      act(() => result.current.disarmOptimistic());
      expect(result.current.working).not.toBeNull();
    });
  });
});
