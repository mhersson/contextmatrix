import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useChatFilterPrefs } from './useChatFilterPrefs';

const STORAGE_KEY = 'chat_filter_prefs';

// Node 25 provides a built-in localStorage that lacks .clear(). Override it
// with a real-backing-store mock that supports spy-able methods, matching the
// pattern used in useCollapsedCards.test.ts and useChatLayout.test.tsx.
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
    removeItem: vi.fn((key: string) => { delete store[key]; }),
    clear: vi.fn(() => { store = {}; }),
  };
})();

Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock, configurable: true });

beforeEach(() => {
  localStorageMock.clear();
  vi.clearAllMocks();
});

describe('useChatFilterPrefs', () => {
  it('returns defaults when localStorage is empty', () => {
    const { result } = renderHook(() => useChatFilterPrefs());
    expect(result.current.prefs).toEqual({
      showText: true,
      showToolCalls: false,
      showThinking: false,
    });
  });

  it('restores stored prefs on mount', () => {
    localStorageMock.setItem(STORAGE_KEY, JSON.stringify({
      showText: false, showToolCalls: true, showThinking: true,
    }));
    const { result } = renderHook(() => useChatFilterPrefs());
    expect(result.current.prefs).toEqual({
      showText: false, showToolCalls: true, showThinking: true,
    });
  });

  it('reads localStorage exactly once on mount', () => {
    renderHook(() => useChatFilterPrefs());
    const getItemForKey = localStorageMock.getItem.mock.calls.filter(([k]) => k === STORAGE_KEY);
    expect(getItemForKey).toHaveLength(1);
  });

  it('falls back to defaults when stored JSON is malformed', () => {
    localStorageMock.setItem(STORAGE_KEY, 'not-json{{{');
    const { result } = renderHook(() => useChatFilterPrefs());
    expect(result.current.prefs.showText).toBe(true);
    expect(result.current.prefs.showToolCalls).toBe(false);
  });

  it('falls back per-field when stored fields are not booleans', () => {
    localStorageMock.setItem(STORAGE_KEY, JSON.stringify({
      showText: 'yes', showToolCalls: 1, showThinking: null,
    }));
    const { result } = renderHook(() => useChatFilterPrefs());
    expect(result.current.prefs).toEqual({
      showText: true, showToolCalls: false, showThinking: false,
    });
  });

  it('setPref updates state and persists in a single setItem write', () => {
    const { result } = renderHook(() => useChatFilterPrefs());
    act(() => {
      result.current.setPref('showToolCalls', true);
    });
    expect(result.current.prefs.showToolCalls).toBe(true);
    expect(result.current.prefs.showText).toBe(true);

    const writes = localStorageMock.setItem.mock.calls.filter(([k]) => k === STORAGE_KEY);
    expect(writes).toHaveLength(1);
    const saved = JSON.parse(writes[0][1]);
    expect(saved).toEqual({ showText: true, showToolCalls: true, showThinking: false });
  });

  it('setPref preserves untouched fields across sequential toggles', () => {
    const { result } = renderHook(() => useChatFilterPrefs());
    act(() => { result.current.setPref('showToolCalls', true); });
    act(() => { result.current.setPref('showThinking', true); });
    expect(result.current.prefs).toEqual({
      showText: true, showToolCalls: true, showThinking: true,
    });
  });

  it('state survives re-mount via localStorage', () => {
    const { result, unmount } = renderHook(() => useChatFilterPrefs());
    act(() => { result.current.setPref('showToolCalls', true); });
    unmount();

    const { result: result2 } = renderHook(() => useChatFilterPrefs());
    expect(result2.current.prefs.showToolCalls).toBe(true);
  });

  describe('storage failure paths', () => {
    let warnSpy: ReturnType<typeof vi.spyOn>;
    beforeEach(() => {
      warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    });
    afterEach(() => {
      warnSpy.mockRestore();
    });

    it('tolerates a throwing getItem on mount', () => {
      localStorageMock.getItem.mockImplementationOnce(() => {
        throw new Error('storage blocked');
      });
      const { result } = renderHook(() => useChatFilterPrefs());
      expect(result.current.prefs).toEqual({
        showText: true, showToolCalls: false, showThinking: false,
      });
    });

    it('tolerates a throwing setItem and warns', () => {
      const { result } = renderHook(() => useChatFilterPrefs());
      localStorageMock.setItem.mockImplementationOnce(() => {
        throw new Error('QuotaExceededError');
      });
      expect(() => act(() => result.current.setPref('showToolCalls', true))).not.toThrow();
      expect(result.current.prefs.showToolCalls).toBe(true);
      expect(warnSpy).toHaveBeenCalledTimes(1);
    });
  });
});
