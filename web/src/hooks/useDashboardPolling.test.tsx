import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useDashboardPolling } from './useDashboardPolling';
import { api } from '../api/client';

vi.mock('./useSSEBus', () => ({ useSSEBus: () => ({ subscribe: () => () => {} }) }));

const INTERVAL_MS = 10_000;

const setVisibility = (state: 'visible' | 'hidden') => {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
};

describe('useDashboardPolling', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    setVisibility('visible');
  });

  it('polls while visible, skips while hidden, and refetches on becoming visible', async () => {
    vi.useFakeTimers();
    const spy = vi.spyOn(api, 'getDashboard').mockResolvedValue({} as never);
    renderHook(() => useDashboardPolling('p', INTERVAL_MS));
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(spy).toHaveBeenCalledTimes(1);

    await act(async () => { await vi.advanceTimersByTimeAsync(INTERVAL_MS); });
    expect(spy).toHaveBeenCalledTimes(2);

    setVisibility('hidden');
    await act(async () => { await vi.advanceTimersByTimeAsync(INTERVAL_MS * 3); });
    expect(spy).toHaveBeenCalledTimes(2);

    setVisibility('visible');
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')); });
    expect(spy).toHaveBeenCalledTimes(3);
  });
});
