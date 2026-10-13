import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import { useBackendHealth } from './useBackendHealth';
import { api } from '../api/client';

vi.mock('../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api/client')>()),
  api: { getBackendHealth: vi.fn() },
}));

describe('useBackendHealth', () => {
  beforeEach(() => {
    vi.mocked(api.getBackendHealth).mockReset();
    vi.spyOn(console, 'warn').mockImplementation(() => {});
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('reports counts and reachable after a successful poll', async () => {
    vi.mocked(api.getBackendHealth).mockResolvedValue({ ok: true, running_containers: 3, max_concurrent: 5 });
    const { result } = renderHook(() => useBackendHealth(60_000));
    await waitFor(() => expect(result.current.status).toBe('ok'));
    expect(result.current.maxAgents).toBe(5);
    expect(result.current.runningContainers).toBe(3);
  });

  it('clears the counts and flags unreachable when a later poll fails', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.mocked(api.getBackendHealth)
      .mockResolvedValueOnce({ ok: true, running_containers: 3, max_concurrent: 5 })
      .mockRejectedValue({ error: 'backend health probe failed', code: 'BACKEND_UNAVAILABLE' });
    const { result } = renderHook(() => useBackendHealth(1000));
    await waitFor(() => expect(result.current.status).toBe('ok'));

    await act(async () => { await vi.advanceTimersByTimeAsync(1100); });

    await waitFor(() => expect(result.current.status).toBe('unreachable'));
    expect(result.current.maxAgents).toBeUndefined();
    expect(result.current.runningContainers).toBeUndefined();
  });

  it('flags unreachable when the first poll fails', async () => {
    vi.mocked(api.getBackendHealth).mockRejectedValue(new Error('boom'));
    const { result } = renderHook(() => useBackendHealth(60_000));
    await waitFor(() => expect(result.current.status).toBe('unreachable'));
  });

  it('reports disabled, not unreachable, when no task backend is configured', async () => {
    vi.mocked(api.getBackendHealth).mockRejectedValue({ error: 'no execution backend is configured', code: 'BACKEND_DISABLED' });
    const { result } = renderHook(() => useBackendHealth(60_000));
    await waitFor(() => expect(result.current.status).toBe('disabled'));
    expect(result.current.maxAgents).toBeUndefined();
  });
});
