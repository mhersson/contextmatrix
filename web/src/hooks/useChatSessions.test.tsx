import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { useChatSessions } from './useChatSessions';
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
});
