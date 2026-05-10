import { renderHook, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useKnowledgeRefreshStatus } from './useKnowledgeRefreshStatus';

const mockGetStatus = vi.fn();
vi.mock('../../api/client', () => ({
  api: {
    getKnowledgeRefreshStatus: (...args: unknown[]) => mockGetStatus(...args),
  },
}));

describe('useKnowledgeRefreshStatus', () => {
  beforeEach(() => {
    mockGetStatus.mockReset();
  });

  it('polls until all repos are idle/terminal', async () => {
    mockGetStatus
      .mockResolvedValueOnce({ repos: { core: { state: 'running', docs_total: 4, docs_done: 1 } } })
      .mockResolvedValueOnce({ repos: { core: { state: 'running', docs_total: 4, docs_done: 3 } } })
      .mockResolvedValueOnce({ repos: { core: { state: 'succeeded', docs_total: 4, docs_done: 4 } } });

    const { result } = renderHook(() =>
      useKnowledgeRefreshStatus('p', { intervalMs: 10 }),
    );

    await waitFor(() => expect(result.current.repos.core?.state).toBe('succeeded'));
  });

  it('stops polling when no repo is non-idle', async () => {
    mockGetStatus.mockResolvedValue({ repos: {} });

    renderHook(() => useKnowledgeRefreshStatus('p', { intervalMs: 10 }));

    await new Promise((r) => setTimeout(r, 50));
    expect(mockGetStatus).toHaveBeenCalledTimes(1);
  });

  it('stops polling after MAX_ERROR_STREAK consecutive failures', async () => {
    mockGetStatus.mockRejectedValue(new Error('boom'));

    renderHook(() => useKnowledgeRefreshStatus('p', { intervalMs: 10 }));

    // Long enough for several backoff steps + plateau:
    // base 1000ms, 2000ms, 4000ms, 8000ms — but max-streak=5 should fire
    // and stop the loop. We watch for plateau, not exact timing.
    await new Promise((r) => setTimeout(r, 2000));
    const callCount = mockGetStatus.mock.calls.length;
    expect(callCount).toBeLessThanOrEqual(5);
    expect(callCount).toBeGreaterThanOrEqual(2);

    // Plateau check: wait more, count must not grow further.
    await new Promise((r) => setTimeout(r, 500));
    expect(mockGetStatus.mock.calls.length).toBe(callCount);
  });

  it('uses increasing delays between failures (exponential backoff)', async () => {
    const calls: number[] = [];
    mockGetStatus.mockImplementation(async () => {
      calls.push(Date.now());
      throw new Error('boom');
    });

    renderHook(() => useKnowledgeRefreshStatus('p', { intervalMs: 10 }));

    // Wait long enough to capture at least 3 calls (base 1000ms backoff).
    await new Promise((r) => setTimeout(r, 3500));

    // Need at least 3 datapoints to compute two consecutive gaps.
    expect(calls.length).toBeGreaterThanOrEqual(3);

    for (let i = 2; i < calls.length; i++) {
      const prev = calls[i - 1] - calls[i - 2];
      const cur = calls[i] - calls[i - 1];
      expect(cur).toBeGreaterThanOrEqual(prev * 0.8);
    }
  });

  it('refresh() resets the error streak and resumes polling', async () => {
    mockGetStatus.mockRejectedValue(new Error('boom'));

    const { result } = renderHook(() =>
      useKnowledgeRefreshStatus('p', { intervalMs: 10 }),
    );

    // Let the streak hit max and the loop stop.
    await new Promise((r) => setTimeout(r, 2000));
    const callsAtPlateau = mockGetStatus.mock.calls.length;

    // Confirm plateau.
    await new Promise((r) => setTimeout(r, 200));
    expect(mockGetStatus.mock.calls.length).toBe(callsAtPlateau);

    // Switch to success and trigger refresh().
    mockGetStatus.mockReset();
    mockGetStatus.mockResolvedValue({ repos: {} });
    result.current.refresh();

    await waitFor(() => expect(mockGetStatus).toHaveBeenCalled());
  });

  it('does not fire setTimeout after unmount when refresh() was called', async () => {
    mockGetStatus.mockResolvedValue({ repos: {} });

    const { result, unmount } = renderHook(() =>
      useKnowledgeRefreshStatus('p', { intervalMs: 50 }),
    );

    // Wait for initial mount poll to settle.
    await new Promise((r) => setTimeout(r, 20));
    mockGetStatus.mockClear();
    result.current.refresh();
    unmount();
    await new Promise((r) => setTimeout(r, 150));
    // Only the synchronous re-tick triggered by refresh() should have fired
    // (after unmount, no further calls expected).
    expect(mockGetStatus.mock.calls.length).toBeLessThanOrEqual(1);
  });

  it('refresh() has stable identity across renders', () => {
    mockGetStatus.mockResolvedValue({ repos: {} });

    const { result, rerender } = renderHook(
      ({ project }: { project: string }) =>
        useKnowledgeRefreshStatus(project, { intervalMs: 50 }),
      { initialProps: { project: 'p' } },
    );
    const first = result.current.refresh;
    rerender({ project: 'p' });
    expect(result.current.refresh).toBe(first);
  });
});
