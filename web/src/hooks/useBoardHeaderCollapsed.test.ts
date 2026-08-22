import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useBoardHeaderCollapsed } from './useBoardHeaderCollapsed';

const STORAGE_KEY = 'contextmatrix-board-header-collapsed';

const originalMatchMedia = window.matchMedia;

function mockMatchMediaTrueFor(trueQuery: string) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: query === trueQuery,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }),
  });
}

// Stateful variant: keeps registered 'change' listeners so a test can flip
// the match while the hook is mounted, exercising useMediaQuery's live
// subscription instead of only the mount-time snapshot.
function mockStatefulMatchMedia(initialMatches: boolean) {
  let matches = initialMatches;
  const listeners = new Set<() => void>();
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      get matches() {
        return matches;
      },
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: (_type: string, cb: () => void) => listeners.add(cb),
      removeEventListener: (_type: string, cb: () => void) => listeners.delete(cb),
      dispatchEvent: vi.fn(),
    }),
  });
  return {
    setMatches(next: boolean) {
      matches = next;
      listeners.forEach((cb) => cb());
    },
  };
}

describe('useBoardHeaderCollapsed', () => {
  beforeEach(() => {
    localStorage.removeItem(STORAGE_KEY);
  });

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
    localStorage.removeItem(STORAGE_KEY);
  });

  it('defaults to expanded on tall viewports with nothing stored', () => {
    mockMatchMediaTrueFor('(min-width: 0px)');
    const { result } = renderHook(() => useBoardHeaderCollapsed());
    expect(result.current[0]).toBe(false);
  });

  it('auto-collapses on short viewports with nothing stored', () => {
    mockMatchMediaTrueFor('(max-height: 800px)');
    const { result } = renderHook(() => useBoardHeaderCollapsed());
    expect(result.current[0]).toBe(true);
  });

  it('a stored choice wins over the viewport default', () => {
    mockMatchMediaTrueFor('(max-height: 800px)');
    localStorage.setItem(STORAGE_KEY, 'false');
    const { result } = renderHook(() => useBoardHeaderCollapsed());
    expect(result.current[0]).toBe(false);
  });

  it('toggle flips the state and persists it', () => {
    mockMatchMediaTrueFor('(min-width: 0px)');
    const { result } = renderHook(() => useBoardHeaderCollapsed());

    act(() => result.current[1]());
    expect(result.current[0]).toBe(true);
    expect(localStorage.getItem(STORAGE_KEY)).toBe('true');

    act(() => result.current[1]());
    expect(result.current[0]).toBe(false);
    expect(localStorage.getItem(STORAGE_KEY)).toBe('false');
  });

  it('toggling out of the auto-collapsed default persists the expanded choice', () => {
    mockMatchMediaTrueFor('(max-height: 800px)');
    const { result } = renderHook(() => useBoardHeaderCollapsed());
    expect(result.current[0]).toBe(true);

    act(() => result.current[1]());
    expect(result.current[0]).toBe(false);
    expect(localStorage.getItem(STORAGE_KEY)).toBe('false');
  });

  it('follows a live viewport change when nothing is stored', () => {
    const mq = mockStatefulMatchMedia(false);
    const { result } = renderHook(() => useBoardHeaderCollapsed());
    expect(result.current[0]).toBe(false);

    act(() => mq.setMatches(true));
    expect(result.current[0]).toBe(true);
  });

  it('a stored choice pins the state through a viewport change', () => {
    localStorage.setItem(STORAGE_KEY, 'false');
    const mq = mockStatefulMatchMedia(false);
    const { result } = renderHook(() => useBoardHeaderCollapsed());

    act(() => mq.setMatches(true));
    expect(result.current[0]).toBe(false);
  });

  it('the persisted value is restored on a fresh mount', () => {
    mockMatchMediaTrueFor('(min-width: 0px)');
    const first = renderHook(() => useBoardHeaderCollapsed());
    act(() => first.result.current[1]());
    first.unmount();

    const second = renderHook(() => useBoardHeaderCollapsed());
    expect(second.result.current[0]).toBe(true);
  });
});
