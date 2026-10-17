import { describe, it, expect, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { publishSheetButton, registerSheetToggle, toggleSheet, useSheetButton } from './sheetButtonStore';

describe('sheetButtonStore', () => {
  it('shows a published button until it is withdrawn', () => {
    const { result } = renderHook(() => useSheetButton('S1'));
    expect(result.current).toBeUndefined();
    act(() => publishSheetButton('S1', { open: false, hasNew: true }));
    expect(result.current).toEqual({ open: false, hasNew: true });
    act(() => publishSheetButton('S1', null));
    expect(result.current).toBeUndefined();
  });

  it('routes a toggle to the pane that registered it', () => {
    const first = vi.fn();
    const second = vi.fn();
    const unregisterFirst = registerSheetToggle('S1', first);
    const unregisterSecond = registerSheetToggle('S1', second);
    unregisterFirst();
    toggleSheet('S1');
    expect(second).toHaveBeenCalledTimes(1);
    expect(first).not.toHaveBeenCalled();
    unregisterSecond();
    toggleSheet('S1');
    expect(second).toHaveBeenCalledTimes(1);
  });
});
