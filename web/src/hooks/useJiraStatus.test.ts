import { describe, it, expect, vi, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { api } from '../api/client';
import { useJiraStatus } from './useJiraStatus';

describe('useJiraStatus', () => {
  afterEach(() => vi.restoreAllMocks());

  it('drops an earlier status when a refetch fails', async () => {
    const ok = { configured: true, authenticated: true };
    vi.spyOn(api, 'getJiraStatus').mockResolvedValueOnce(ok).mockRejectedValueOnce(new Error('boom'));

    const { result, rerender } = renderHook(({ enabled }) => useJiraStatus(enabled), { initialProps: { enabled: true } });
    await waitFor(() => expect(result.current).toEqual(ok));

    rerender({ enabled: false });
    rerender({ enabled: true });
    await waitFor(() => expect(result.current).toBeNull());
  });
});
