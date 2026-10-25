import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useChatSessions, CHAT_SESSIONS_POLL_MS } from './useChatSessions';
import { api } from '../api/client';
import type { ChatSession } from '../types';

describe('useChatSessions', () => {
  beforeEach(() => {
    vi.spyOn(api, 'listChats').mockResolvedValue([
      { id: 'S1', title: 't', status: 'cold', created_at: '', last_active: '', created_by: 'x' } satisfies ChatSession,
    ]);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('loads sessions on mount', async () => {
    const { result } = renderHook(() => useChatSessions());
    await waitFor(() => expect(result.current.sessions).toHaveLength(1));
    expect(result.current.sessions[0].id).toBe('S1');
  });

  it('refresh re-invokes listChats', async () => {
    const { result } = renderHook(() => useChatSessions());
    await waitFor(() => expect(result.current.sessions).toHaveLength(1));
    expect(api.listChats).toHaveBeenCalledTimes(1);
    result.current.refresh();
    await waitFor(() => expect(api.listChats).toHaveBeenCalledTimes(2));
  });

  it('captures error message on failure', async () => {
    vi.spyOn(api, 'listChats').mockRejectedValueOnce(new Error('boom'));
    const { result } = renderHook(() => useChatSessions());
    await waitFor(() => expect(result.current.error).toBe('boom'));
  });

  it('is loaded once the first fetch settles, whether it succeeds or fails', async () => {
    const ok = renderHook(() => useChatSessions());
    expect(ok.result.current.loaded).toBe(false);
    await waitFor(() => expect(ok.result.current.loaded).toBe(true));
    expect(ok.result.current.sessions).toHaveLength(1);
    ok.unmount();

    vi.spyOn(api, 'listChats').mockRejectedValueOnce(new Error('boom'));
    const failed = renderHook(() => useChatSessions());
    expect(failed.result.current.loaded).toBe(false);
    await waitFor(() => expect(failed.result.current.loaded).toBe(true));
    expect(failed.result.current.error).toBe('boom');
  });

  describe('polling', () => {
    const setVisibility = (state: 'visible' | 'hidden') => {
      Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
    };
    afterEach(() => {
      vi.useRealTimers();
      setVisibility('visible');
    });

    it('refetches every poll interval while visible, skips while hidden, and refetches on becoming visible', async () => {
      vi.useFakeTimers();
      renderHook(() => useChatSessions());
      await act(async () => { await vi.advanceTimersByTimeAsync(0); });
      expect(api.listChats).toHaveBeenCalledTimes(1);

      await act(async () => { await vi.advanceTimersByTimeAsync(CHAT_SESSIONS_POLL_MS); });
      expect(api.listChats).toHaveBeenCalledTimes(2);

      setVisibility('hidden');
      await act(async () => { await vi.advanceTimersByTimeAsync(CHAT_SESSIONS_POLL_MS * 2); });
      expect(api.listChats).toHaveBeenCalledTimes(2);

      setVisibility('visible');
      await act(async () => { document.dispatchEvent(new Event('visibilitychange')); });
      expect(api.listChats).toHaveBeenCalledTimes(3);
    });
  });
});
