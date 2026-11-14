import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { api } from '../../api/client';
import { usePlaybookPin } from './usePlaybookPin';

const toast = vi.hoisted(() => ({ showToast: vi.fn() }));
vi.mock('../../hooks/useToast', () => ({ useToast: () => toast }));

const list = vi.hoisted(() => ({ refresh: vi.fn() }));
vi.mock('../../hooks/usePlaybooks', () => ({
  usePlaybooks: () => ({ playbooks: [], loaded: true, error: null, refresh: list.refresh }),
}));

beforeEach(() => {
  toast.showToast.mockReset();
  list.refresh.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('usePlaybookPin', () => {
  it('pins each playbook once and refreshes the list once', async () => {
    const pin = vi.spyOn(api, 'pinPlaybook').mockResolvedValue(undefined);
    const { result } = renderHook(() => usePlaybookPin());

    await act(() => result.current(['a', 'b'], true));

    expect(pin).toHaveBeenCalledTimes(2);
    expect(pin).toHaveBeenCalledWith('a', true);
    expect(pin).toHaveBeenCalledWith('b', true);
    expect(list.refresh).toHaveBeenCalledTimes(1);
    expect(toast.showToast).not.toHaveBeenCalled();
  });

  it('reports the failures in one error toast with their count and still refreshes', async () => {
    vi.spyOn(api, 'pinPlaybook')
      .mockRejectedValueOnce(new Error('gone'))
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(new Error('gone'));
    const { result } = renderHook(() => usePlaybookPin());

    await act(() => result.current(['a', 'b', 'c'], false));

    expect(api.pinPlaybook).toHaveBeenCalledWith('b', false);
    expect(list.refresh).toHaveBeenCalledTimes(1);
    expect(toast.showToast).toHaveBeenCalledTimes(1);
    expect(toast.showToast.mock.calls[0][0]).toContain('2');
    expect(toast.showToast.mock.calls[0][1]).toBe('error');
  });
});
