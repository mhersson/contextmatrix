import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import type { ReactNode } from 'react';
import { AboutProvider, useAbout } from './useAbout';
import { api } from '../api/client';
import { ABOUT_REFRESH_MS, ABOUT_SEEN_KEY } from '../lib/about';
import type { AboutResponse } from '../types';

vi.mock('../api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api/client')>()),
  api: { getAbout: vi.fn() },
}));

const about: AboutResponse = {
  components: [
    {
      name: 'agent',
      status: 'connected',
      build: {
        commit: 'abc',
        changes: [
          { number: 9, kind: 'feat', title: 'nine', merged_at: '2026-10-20T10:00:00Z' },
          { number: 8, kind: 'fix', title: 'eight', merged_at: '2026-10-19T10:00:00Z' },
        ],
      },
    },
  ],
};
const seenNine = { agent: { number: 9, merged_at: '2026-10-20T10:00:00Z' } };

const wrapper = ({ children }: { children: ReactNode }) => <AboutProvider>{children}</AboutProvider>;

function setVisibility(state: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
}

describe('AboutProvider', () => {
  beforeEach(() => {
    vi.mocked(api.getAbout).mockReset();
    vi.mocked(api.getAbout).mockResolvedValue(about);
    localStorage.clear();
    vi.spyOn(console, 'warn').mockImplementation(() => {});
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    setVisibility('visible');
  });

  it('fetches on mount and reports unseen changes', async () => {
    const { result } = renderHook(() => useAbout(), { wrapper });
    await waitFor(() => expect(result.current.hasUnseen).toBe(true));
    expect(result.current.unseen.agent).toBe(2);
    expect(api.getAbout).toHaveBeenCalledTimes(1);
  });

  it('markSeen stores the newest change and clears the dot', async () => {
    const { result } = renderHook(() => useAbout(), { wrapper });
    await waitFor(() => expect(result.current.hasUnseen).toBe(true));
    act(() => result.current.markSeen(about.components));
    expect(result.current.hasUnseen).toBe(false);
    expect(JSON.parse(localStorage.getItem(ABOUT_SEEN_KEY)!)).toEqual(seenNine);
  });

  it('reads seen marks from storage', async () => {
    localStorage.setItem(ABOUT_SEEN_KEY, JSON.stringify(seenNine));
    const { result } = renderHook(() => useAbout(), { wrapper });
    await waitFor(() => expect(result.current.about).not.toBeNull());
    expect(result.current.hasUnseen).toBe(false);
  });

  it('survives storage that throws', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied');
    });
    const { result } = renderHook(() => useAbout(), { wrapper });
    await waitFor(() => expect(result.current.hasUnseen).toBe(true));
    expect(() => act(() => result.current.markSeen(about.components))).not.toThrow();
  });

  it('follows a storage event from another tab', async () => {
    const { result } = renderHook(() => useAbout(), { wrapper });
    await waitFor(() => expect(result.current.hasUnseen).toBe(true));
    act(() => {
      localStorage.setItem(ABOUT_SEEN_KEY, JSON.stringify(seenNine));
      window.dispatchEvent(new StorageEvent('storage', { key: ABOUT_SEEN_KEY }));
    });
    expect(result.current.hasUnseen).toBe(false);
  });

  it('refetches every 15 minutes while visible', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { result } = renderHook(() => useAbout(), { wrapper });
    await waitFor(() => expect(result.current.about).not.toBeNull());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ABOUT_REFRESH_MS + 100);
    });
    expect(api.getAbout).toHaveBeenCalledTimes(2);
  });

  it('skips the refetch while hidden', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { result } = renderHook(() => useAbout(), { wrapper });
    await waitFor(() => expect(result.current.about).not.toBeNull());
    setVisibility('hidden');
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ABOUT_REFRESH_MS + 100);
    });
    expect(api.getAbout).toHaveBeenCalledTimes(1);
  });

  it('refresh returns the fresh answer, or null and the old value on failure', async () => {
    const { result } = renderHook(() => useAbout(), { wrapper });
    await waitFor(() => expect(result.current.about).toEqual(about));
    let got: AboutResponse | null = null;
    await act(async () => {
      got = await result.current.refresh();
    });
    expect(got).toEqual(about);

    vi.mocked(api.getAbout).mockRejectedValue(new Error('boom'));
    await act(async () => {
      got = await result.current.refresh();
    });
    expect(got).toBeNull();
    expect(result.current.about).toEqual(about);
  });
});

describe('useAbout outside a provider', () => {
  it('is inert', () => {
    const { result } = renderHook(() => useAbout());
    expect(result.current.about).toBeNull();
    expect(result.current.hasUnseen).toBe(false);
    expect(() => result.current.markSeen([])).not.toThrow();
  });
});
