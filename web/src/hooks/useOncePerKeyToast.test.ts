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
});
