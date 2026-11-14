import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { api } from '../api/client';
import type { BoardEvent, EventType } from '../types';
import { useCardSentinel } from './useSentinel';

vi.mock('../api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api/client')>();
  return { ...actual, api: { getCardSentinel: vi.fn() } };
});

const bus = vi.hoisted(() => ({
  handlers: [] as { pattern: string; h: (e: BoardEvent) => void }[],
}));
vi.mock('./useSSEBus', () => ({
  useSSEBus: () => ({
    subscribe: (pattern: string, h: (e: BoardEvent) => void) => {
      const entry = { pattern, h };
      bus.handlers.push(entry);
      return () => {
        bus.handlers = bus.handlers.filter((x) => x !== entry);
      };
    },
    connected: true,
    reconnectEpoch: 0,
  }),
}));

const get = vi.mocked(api.getCardSentinel);

function emit(type: EventType, project: string, card_id: string) {
  const prefix = type.split('.')[0] + '.*';
  act(() => {
    bus.handlers
      .filter((x) => x.pattern === prefix)
      .forEach((x) => x.h({ type, project, card_id, timestamp: '2026-11-14T10:00:00Z' }));
  });
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  get.mockReset();
  get.mockResolvedValue({
    run: null, heals: [], findings: [], analyses: [], standing_pr: null, guidance_revision: null,
  });
  bus.handlers = [];
});

afterEach(() => {
  vi.useRealTimers();
});

describe('useCardSentinel', () => {
  it('refetches once for a burst of the card and project Sentinel events, after the debounce', async () => {
    renderHook(() => useCardSentinel('p', 'P-001'));
    await advance(0);
    expect(get).toHaveBeenCalledTimes(1);

    emit('sentinel.changed', 'p', 'P-001');
    emit('sentinel.changed', 'p', '');
    emit('sentinel.changed', 'p', 'P-001');
    await advance(999);
    expect(get).toHaveBeenCalledTimes(1);

    await advance(1);
    expect(get).toHaveBeenCalledTimes(2);
  });

  it("ignores Sentinel events of another card, another project or Sentinel's status", async () => {
    renderHook(() => useCardSentinel('p', 'P-001'));
    await advance(0);

    emit('sentinel.changed', 'p', 'P-002');
    emit('sentinel.changed', 'q', 'P-001');
    emit('sentinel.changed', '', '');
    await advance(5_000);

    expect(get).toHaveBeenCalledTimes(1);
  });

  it("refetches at once on the card's own card events only", async () => {
    renderHook(() => useCardSentinel('p', 'P-001'));
    await advance(0);

    emit('card.updated', 'p', 'P-002');
    await advance(0);
    expect(get).toHaveBeenCalledTimes(1);

    emit('card.updated', 'p', 'P-001');
    await advance(0);
    expect(get).toHaveBeenCalledTimes(2);
  });

  it('drops a pending refetch when it unmounts', async () => {
    const { unmount } = renderHook(() => useCardSentinel('p', 'P-001'));
    await advance(0);

    emit('sentinel.changed', 'p', 'P-001');
    unmount();
    await advance(5_000);

    expect(get).toHaveBeenCalledTimes(1);
  });
});
