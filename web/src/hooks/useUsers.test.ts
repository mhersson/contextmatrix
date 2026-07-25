import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';

const listUsers = vi.fn();
vi.mock('../api/client', () => ({ api: { listUsers: (...a: unknown[]) => listUsers(...a) } }));

describe('useUsers', () => {
  beforeEach(() => {
    vi.resetModules();
    listUsers.mockReset();
  });

  it('does not fetch when disabled', async () => {
    const { useUsers } = await import('./useUsers');
    const { result } = renderHook(() => useUsers(false));
    expect(result.current).toEqual([]);
    expect(listUsers).not.toHaveBeenCalled();
  });

  it('fetches once and caches the result across hook instances when enabled', async () => {
    listUsers.mockResolvedValue([
      { username: 'alice', display_name: 'Alice' },
      { username: 'bob', display_name: 'Bob' },
    ]);
    const { useUsers } = await import('./useUsers');

    const { result: first } = renderHook(() => useUsers(true));
    await waitFor(() =>
      expect(first.current.map((u) => u.username)).toEqual(['alice', 'bob']),
    );

    const { result: second } = renderHook(() => useUsers(true));
    await waitFor(() =>
      expect(second.current.map((u) => u.username)).toEqual(['alice', 'bob']),
    );

    expect(listUsers).toHaveBeenCalledOnce();
  });

  it('swallows errors and returns an empty roster', async () => {
    listUsers.mockRejectedValue(new Error('boom'));
    const { useUsers } = await import('./useUsers');
    const { result } = renderHook(() => useUsers(true));
    await waitFor(() => expect(listUsers).toHaveBeenCalled());
    expect(result.current).toEqual([]);
  });
});
