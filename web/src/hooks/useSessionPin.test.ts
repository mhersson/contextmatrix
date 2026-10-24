import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { api } from '../api/client';
import type { ChatSession } from '../types';
import { CHAT_SESSIONS_CHANGED_EVENT } from './useChatSessions';
import { useSessionPin } from './useSessionPin';

const toast = vi.hoisted(() => ({ showToast: vi.fn() }));
vi.mock('./useToast', () => ({ useToast: () => toast }));

let changed = 0;
const onChanged = () => {
  changed += 1;
};

beforeEach(() => {
  changed = 0;
  toast.showToast.mockReset();
  window.addEventListener(CHAT_SESSIONS_CHANGED_EVENT, onChanged);
});

afterEach(() => {
  window.removeEventListener(CHAT_SESSIONS_CHANGED_EVENT, onChanged);
  vi.restoreAllMocks();
});

describe('useSessionPin', () => {
  it('patches each session once and refreshes the list once', async () => {
    const update = vi.spyOn(api, 'updateChat').mockResolvedValue({} as ChatSession);
    const { result } = renderHook(() => useSessionPin());

    await act(() => result.current(['A', 'B'], true));

    expect(update).toHaveBeenCalledTimes(2);
    expect(update).toHaveBeenCalledWith('A', { pinned: true });
    expect(update).toHaveBeenCalledWith('B', { pinned: true });
    expect(changed).toBe(1);
    expect(toast.showToast).not.toHaveBeenCalled();
  });

  it('reports a failed patch in one error toast and still refreshes', async () => {
    vi.spyOn(api, 'updateChat')
      .mockResolvedValueOnce({} as ChatSession)
      .mockRejectedValueOnce(new Error('gone'));
    const { result } = renderHook(() => useSessionPin());

    await act(() => result.current(['A', 'B'], false));

    expect(changed).toBe(1);
    expect(toast.showToast).toHaveBeenCalledTimes(1);
    expect(toast.showToast.mock.calls[0][1]).toBe('error');
  });
});
