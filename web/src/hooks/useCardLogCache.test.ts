import { describe, it, expect } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useCardLogCache } from './useCardLogCache';
import type { LogEntry } from '../types';

function entry(cardId: string, seq: number, content: string): LogEntry {
  return {
    ts: '2026-01-01T00:00:00Z',
    card_id: cardId,
    type: 'text',
    content,
    seq,
  };
}

// Server runner emits seq=0 for every event — sequence numbers are not
// reliably monotonic, so dedup must rely on buffer length, not seq.
function flatEntry(cardId: string, content: string): LogEntry {
  return {
    ts: '2026-01-01T00:00:00Z',
    card_id: cardId,
    type: 'text',
    content,
    seq: 0,
  };
}

describe('useCardLogCache', () => {
  it('does not populate cache on the first commit for a new cardId — liveLogs may be stale from a previous card', () => {
    const { result } = renderHook(
      ({ l, id }: { l: readonly LogEntry[]; id: string | null }) =>
        useCardLogCache(l, id),
      { initialProps: { l: [entry('A', 1, 'a-1')] as readonly LogEntry[], id: 'A' as string | null } },
    );
    expect(result.current.cache.has('A')).toBe(false);
  });

  it('appends new entries on subsequent commits with the same cardId', () => {
    const { result, rerender } = renderHook(
      ({ l, id }: { l: readonly LogEntry[]; id: string | null }) =>
        useCardLogCache(l, id),
      { initialProps: { l: [] as readonly LogEntry[], id: 'A' as string | null } },
    );
    rerender({ l: [entry('A', 1, 'a-1'), entry('A', 2, 'a-2')], id: 'A' });
    expect(result.current.cache.get('A')).toEqual([
      entry('A', 1, 'a-1'),
      entry('A', 2, 'a-2'),
    ]);
  });

  it("preserves a card's cache when cardId switches away and back", () => {
    const { result, rerender } = renderHook(
      ({ l, id }: { l: readonly LogEntry[]; id: string | null }) =>
        useCardLogCache(l, id),
      { initialProps: { l: [] as readonly LogEntry[], id: 'A' as string | null } },
    );
    rerender({ l: [entry('A', 1, 'a-1')], id: 'A' });
    expect(result.current.cache.get('A')).toEqual([entry('A', 1, 'a-1')]);

    // Switch to B — A's cache must survive.
    rerender({ l: [] as readonly LogEntry[], id: 'B' });
    rerender({ l: [entry('B', 1, 'b-1')], id: 'B' });
    expect(result.current.cache.get('A')).toEqual([entry('A', 1, 'a-1')]);
    expect(result.current.cache.get('B')).toEqual([entry('B', 1, 'b-1')]);

    // Switch back to A.
    rerender({ l: [] as readonly LogEntry[], id: 'A' });
    expect(result.current.cache.get('A')).toEqual([entry('A', 1, 'a-1')]);
  });

  it('dedups snapshot replay against per-card high-water seq', () => {
    const { result, rerender } = renderHook(
      ({ l, id }: { l: readonly LogEntry[]; id: string | null }) =>
        useCardLogCache(l, id),
      { initialProps: { l: [] as readonly LogEntry[], id: 'A' as string | null } },
    );
    rerender({ l: [entry('A', 1, 'a-1'), entry('A', 2, 'a-2')], id: 'A' });

    // Detour to B and back simulates a real reconnect.
    rerender({ l: [] as readonly LogEntry[], id: 'B' });
    rerender({ l: [] as readonly LogEntry[], id: 'A' });

    // Snapshot replay sends seq 1, 2 again plus a new seq 3.
    rerender({
      l: [
        entry('A', 1, 'a-1'),
        entry('A', 2, 'a-2'),
        entry('A', 3, 'a-3'),
      ],
      id: 'A',
    });

    expect(result.current.cache.get('A')).toEqual([
      entry('A', 1, 'a-1'),
      entry('A', 2, 'a-2'),
      entry('A', 3, 'a-3'),
    ]);
  });

  it('caches client-side gap markers alongside real entries', () => {
    // Gap markers are synthesized by useRunnerLogs to surface delivery
    // holes; they ride in liveLogs alongside server events and are part
    // of the visible stream, so length-based dedup (which is the only
    // dedup that works given seq=0 from the runner) caches them too.
    const gap: LogEntry = {
      ts: '2026-01-01T00:00:00Z',
      card_id: '',
      type: 'gap',
      content: 'seq gap',
    };
    const { result, rerender } = renderHook(
      ({ l, id }: { l: readonly LogEntry[]; id: string | null }) =>
        useCardLogCache(l, id),
      { initialProps: { l: [] as readonly LogEntry[], id: 'A' as string | null } },
    );
    rerender({ l: [gap, entry('A', 1, 'a-1')], id: 'A' });
    expect(result.current.cache.get('A')).toEqual([gap, entry('A', 1, 'a-1')]);
  });

  it("user-reported flow: prompt that arrived during B detour reaches A on reopen via snapshot replay dedup", () => {
    // Bug repro: open A (HITL), close panel, look at B, close, reopen A —
    // the prompt sent to A's session while the user was on B must surface.
    const { result, rerender } = renderHook(
      ({ l, id }: { l: readonly LogEntry[]; id: string | null }) =>
        useCardLogCache(l, id),
      { initialProps: { l: [] as readonly LogEntry[], id: 'A' as string | null } },
    );

    // Initial open of A: snapshot delivers the first message.
    rerender({ l: [entry('A', 1, 'start')], id: 'A' });
    expect(result.current.cache.get('A')).toEqual([entry('A', 1, 'start')]);

    // User opens B. The first commit with id=B sees liveLogs still stale
    // from A (useRunnerLogs hasn't cleared yet) — must be skipped, not
    // appended to cache[B].
    rerender({ l: [entry('A', 1, 'start')], id: 'B' });
    expect(result.current.cache.get('B')).toBeUndefined();

    // Then useRunnerLogs's clear effect notifies; liveLogs is now empty.
    rerender({ l: [] as readonly LogEntry[], id: 'B' });
    // B's snapshot arrives.
    rerender({ l: [entry('B', 1, 'b-start')], id: 'B' });
    expect(result.current.cache.get('B')).toEqual([entry('B', 1, 'b-start')]);
    // A's cache must still hold its earlier entry.
    expect(result.current.cache.get('A')).toEqual([entry('A', 1, 'start')]);

    // User closes B and reopens A. Same stale-then-cleared sequence.
    rerender({ l: [entry('B', 1, 'b-start')], id: 'A' });
    rerender({ l: [] as readonly LogEntry[], id: 'A' });

    // Server replays A's full session — this time the prompt that arrived
    // while we were on B is included as seq=2.
    rerender({
      l: [entry('A', 1, 'start'), entry('A', 2, 'prompt')],
      id: 'A',
    });

    expect(result.current.cache.get('A')).toEqual([
      entry('A', 1, 'start'),
      entry('A', 2, 'prompt'),
    ]);
  });

  it('appends every event when seq is constant 0 (server-side reality)', () => {
    // The runner currently emits seq=0 for every event, so seq-based dedup
    // would treat every event after the first as a duplicate. The cache
    // must dedup by buffer length instead, so each new tail entry is
    // appended.
    const { result, rerender } = renderHook(
      ({ l, id }: { l: readonly LogEntry[]; id: string | null }) =>
        useCardLogCache(l, id),
      { initialProps: { l: [] as readonly LogEntry[], id: 'A' as string | null } },
    );
    rerender({ l: [flatEntry('A', 'start')], id: 'A' });
    rerender({ l: [flatEntry('A', 'start'), flatEntry('A', 'prompt')], id: 'A' });
    rerender({
      l: [flatEntry('A', 'start'), flatEntry('A', 'prompt'), flatEntry('A', 'reply')],
      id: 'A',
    });
    expect(result.current.cache.get('A')).toEqual([
      flatEntry('A', 'start'),
      flatEntry('A', 'prompt'),
      flatEntry('A', 'reply'),
    ]);
  });

  it('user-reported flow with seq=0: prompt arriving during B detour reaches A on reopen', () => {
    const { result, rerender } = renderHook(
      ({ l, id }: { l: readonly LogEntry[]; id: string | null }) =>
        useCardLogCache(l, id),
      { initialProps: { l: [] as readonly LogEntry[], id: 'A' as string | null } },
    );

    // Open A, snapshot replay delivers start.
    rerender({ l: [flatEntry('A', 'start')], id: 'A' });
    expect(result.current.cache.get('A')).toEqual([flatEntry('A', 'start')]);

    // User opens B. liveLogs is briefly stale from A; clear; then B's data.
    rerender({ l: [flatEntry('A', 'start')], id: 'B' });
    rerender({ l: [] as readonly LogEntry[], id: 'B' });
    rerender({ l: [flatEntry('B', 'b-start')], id: 'B' });
    expect(result.current.cache.get('A')).toEqual([flatEntry('A', 'start')]);

    // User reopens A. Snapshot replays full session including the prompt
    // that arrived while the SSE for A was closed.
    rerender({ l: [flatEntry('B', 'b-start')], id: 'A' });
    rerender({ l: [] as readonly LogEntry[], id: 'A' });
    rerender({
      l: [flatEntry('A', 'start'), flatEntry('A', 'prompt')],
      id: 'A',
    });

    expect(result.current.cache.get('A')).toEqual([
      flatEntry('A', 'start'),
      flatEntry('A', 'prompt'),
    ]);
  });

  it('invalidate(cardId) drops a single card slot without affecting other cards', () => {
    const { result, rerender } = renderHook(
      ({ l, id }: { l: readonly LogEntry[]; id: string | null }) =>
        useCardLogCache(l, id),
      { initialProps: { l: [] as readonly LogEntry[], id: 'A' as string | null } },
    );
    rerender({ l: [entry('A', 1, 'a-1')], id: 'A' });

    rerender({ l: [entry('A', 1, 'a-1')], id: 'B' });
    rerender({ l: [] as readonly LogEntry[], id: 'B' });
    rerender({ l: [entry('B', 1, 'b-1')], id: 'B' });

    expect(result.current.cache.get('A')).toEqual([entry('A', 1, 'a-1')]);
    expect(result.current.cache.get('B')).toEqual([entry('B', 1, 'b-1')]);

    act(() => { result.current.invalidate('A'); });

    expect(result.current.cache.has('A')).toBe(false);
    expect(result.current.cache.get('B')).toEqual([entry('B', 1, 'b-1')]);
  });

  it('after invalidate(currentCardId), stale liveLogs do not re-populate the cache', () => {
    // HITL-restart bug repro: a session ends with the cache holding 3
    // entries. The parent invalidates the slot when a fresh run starts,
    // but useRunnerLogs's clear effect has not fired yet, so liveLogs
    // still contains the previous run's entries. The cache must NOT
    // re-cache those stale entries — they belong to the dead session.
    const { result, rerender } = renderHook(
      ({ l, id }: { l: readonly LogEntry[]; id: string | null }) =>
        useCardLogCache(l, id),
      { initialProps: { l: [] as readonly LogEntry[], id: 'A' as string | null } },
    );

    rerender({
      l: [flatEntry('A', 'r1-1'), flatEntry('A', 'r1-2'), flatEntry('A', 'r1-3')],
      id: 'A',
    });
    expect(result.current.cache.get('A')).toHaveLength(3);

    act(() => { result.current.invalidate('A'); });
    expect(result.current.cache.has('A')).toBe(false);

    // Stale render: liveLogs still carries the previous run's entries.
    rerender({
      l: [flatEntry('A', 'r1-1'), flatEntry('A', 'r1-2'), flatEntry('A', 'r1-3')],
      id: 'A',
    });
    expect(result.current.cache.has('A')).toBe(false);

    // Then useRunnerLogs's clear effect fires.
    rerender({ l: [] as readonly LogEntry[], id: 'A' });

    // First entry of run 2 arrives.
    rerender({ l: [flatEntry('A', 'r2-1')], id: 'A' });

    expect(result.current.cache.get('A')).toEqual([flatEntry('A', 'r2-1')]);
  });

  it('reset() clears every cached buffer and forgets high-water marks', () => {
    const { result, rerender } = renderHook(
      ({ l, id }: { l: readonly LogEntry[]; id: string | null }) =>
        useCardLogCache(l, id),
      { initialProps: { l: [] as readonly LogEntry[], id: 'A' as string | null } },
    );
    rerender({ l: [entry('A', 1, 'a-1')], id: 'A' });
    expect(result.current.cache.get('A')).toBeDefined();

    act(() => {
      result.current.reset();
    });
    expect(result.current.cache.size).toBe(0);

    // After reset, the same seqs must be re-cached (no leftover dedup state).
    rerender({ l: [entry('A', 1, 'a-1')] as readonly LogEntry[], id: 'A' });
    rerender({ l: [entry('A', 1, 'a-1'), entry('A', 2, 'a-2')], id: 'A' });
    expect(result.current.cache.get('A')).toEqual([
      entry('A', 1, 'a-1'),
      entry('A', 2, 'a-2'),
    ]);
  });
});
