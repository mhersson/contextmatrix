import { renderHook, act } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import { useOncePerKeyToast } from './useOncePerKeyToast';

describe('useOncePerKeyToast', () => {
  test('shows once per key', () => {
    const toast = vi.fn();
    const { result } = renderHook(() => useOncePerKeyToast(toast));
    act(() => { result.current('k1', 'first'); });
    act(() => { result.current('k1', 'first'); });
    expect(toast).toHaveBeenCalledTimes(1);
  });

  test('distinct keys each fire once', () => {
    const toast = vi.fn();
    const { result } = renderHook(() => useOncePerKeyToast(toast));
    act(() => { result.current('a', 'A'); });
    act(() => { result.current('b', 'B'); });
    expect(toast).toHaveBeenCalledTimes(2);
  });

  test('returned callback is stable across rerenders even when toast identity changes', () => {
    // Regression: callers commonly pass an inline arrow (`(msg) => showToast(msg, 'error')`)
    // which produces a fresh function on every render. If the hook returns a new
    // callback whenever `toast` changes, any effect that depends on the returned
    // callback re-fires on every render, looping fetches indefinitely.
    const { result, rerender } = renderHook(
      ({ toast }: { toast: (m: string) => void }) => useOncePerKeyToast(toast),
      { initialProps: { toast: () => {} } },
    );
    const first = result.current;
    rerender({ toast: () => {} });
    rerender({ toast: () => {} });
    expect(result.current).toBe(first);
  });

  test('uses the latest toast function when invoked', () => {
    const a = vi.fn();
    const b = vi.fn();
    const { result, rerender } = renderHook(
      ({ toast }: { toast: (m: string) => void }) => useOncePerKeyToast(toast),
      { initialProps: { toast: a } },
    );
    rerender({ toast: b });
    act(() => { result.current('k', 'msg'); });
    expect(a).not.toHaveBeenCalled();
    expect(b).toHaveBeenCalledWith('msg');
  });
});
