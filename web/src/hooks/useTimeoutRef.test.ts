import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { useTimeoutRef } from './useTimeoutRef';

describe('useTimeoutRef', () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  test('schedule fires after the delay', () => {
    const fn = vi.fn();
    const { result } = renderHook(() => useTimeoutRef());
    act(() => { result.current.schedule(fn, 100); });
    expect(fn).not.toHaveBeenCalled();
    act(() => { vi.advanceTimersByTime(100); });
    expect(fn).toHaveBeenCalledTimes(1);
  });

  test('cancel prevents fire', () => {
    const fn = vi.fn();
    const { result } = renderHook(() => useTimeoutRef());
    act(() => { result.current.schedule(fn, 100); });
    act(() => { result.current.cancel(); });
    act(() => { vi.advanceTimersByTime(200); });
    expect(fn).not.toHaveBeenCalled();
  });

  test('schedule replaces a prior pending timer', () => {
    const a = vi.fn();
    const b = vi.fn();
    const { result } = renderHook(() => useTimeoutRef());
    act(() => { result.current.schedule(a, 100); });
    act(() => { result.current.schedule(b, 100); });
    act(() => { vi.advanceTimersByTime(100); });
    expect(a).not.toHaveBeenCalled();
    expect(b).toHaveBeenCalledTimes(1);
  });

  test('unmount cancels pending timer', () => {
    const fn = vi.fn();
    const { result, unmount } = renderHook(() => useTimeoutRef());
    act(() => { result.current.schedule(fn, 100); });
    unmount();
    act(() => { vi.advanceTimersByTime(100); });
    expect(fn).not.toHaveBeenCalled();
  });
});
