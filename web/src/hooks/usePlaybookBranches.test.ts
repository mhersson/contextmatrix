import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { usePlaybookBranches } from './usePlaybookBranches';
import { api } from '../api/client';

vi.mock('../api/client', () => ({ api: { fetchBranches: vi.fn() } }));

describe('usePlaybookBranches', () => {
  beforeEach(() => { vi.mocked(api.fetchBranches).mockReset(); });

  it('returns the sorted intersection across projects', async () => {
    vi.mocked(api.fetchBranches).mockImplementation(async (project: string) =>
      project === 'alpha' ? ['main', 'develop', 'feature/x'] : ['develop', 'main']);
    const { result } = renderHook(() => usePlaybookBranches(['alpha', 'beta'], true));
    expect(result.current.loading).toBe(true);
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.branches).toEqual(['develop', 'main']);
    expect(result.current.error).toBe(false);
  });

  it('reports an error when any project fails and nothing when disabled or empty', async () => {
    vi.mocked(api.fetchBranches).mockImplementation(async (project: string) => {
      if (project === 'beta') throw new Error('boom');
      return ['main'];
    });
    const { result } = renderHook(() => usePlaybookBranches(['alpha', 'beta'], true));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe(true);

    const empty = renderHook(() => usePlaybookBranches([], true));
    expect(empty.result.current).toEqual({ branches: [], loading: false, error: false });

    const disabled = renderHook(() => usePlaybookBranches(['alpha'], false));
    expect(disabled.result.current.loading).toBe(false);
    expect(api.fetchBranches).toHaveBeenCalledTimes(2);
  });
});
