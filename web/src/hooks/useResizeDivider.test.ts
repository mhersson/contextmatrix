import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useResizeDivider } from './useResizeDivider';

function fakePointerEvent() {
  const target = {
    setPointerCapture: () => {},
    releasePointerCapture: () => {},
  };
  return {
    target,
    pointerId: 1,
    clientY: 100,
  } as unknown as React.PointerEvent;
}

describe('useResizeDivider', () => {
  it('clears drag state and body styles on pointercancel', () => {
    const containerRef = { current: document.createElement('div') };
    const { result } = renderHook(() => useResizeDivider({ containerRef, enabled: true }));

    act(() => {
      result.current.handleProps.onPointerDown(fakePointerEvent());
    });
    expect(result.current.isDragging).toBe(true);
    expect(document.body.style.userSelect).toBe('none');

    act(() => {
      result.current.handleProps.onPointerCancel(fakePointerEvent());
    });
    expect(result.current.isDragging).toBe(false);
    expect(document.body.style.userSelect).toBe('');
    expect(document.body.style.cursor).toBe('');
  });
});
