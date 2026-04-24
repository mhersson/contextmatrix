import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useEditorHeight } from './useEditorHeight';

const DEFAULT_DESKTOP = 375;
const MOBILE_ABOVE_EDITOR_PX = 280;

function setInnerWidth(px: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: px });
}

function installVisualViewport(height: number | undefined) {
  if (height === undefined) {
    Object.defineProperty(window, 'visualViewport', { configurable: true, value: undefined });
    return { dispatch: () => {} };
  }
  const listeners = new Set<EventListenerOrEventListenerObject>();
  const vv = {
    height,
    addEventListener: (_: string, l: EventListenerOrEventListenerObject) => listeners.add(l),
    removeEventListener: (_: string, l: EventListenerOrEventListenerObject) => listeners.delete(l),
  } as unknown as VisualViewport;
  Object.defineProperty(window, 'visualViewport', { configurable: true, value: vv });
  return {
    dispatch: () => listeners.forEach((l) => typeof l === 'function' ? l(new Event('resize')) : l.handleEvent(new Event('resize'))),
    setHeight: (h: number) => { (vv as unknown as { height: number }).height = h; },
  };
}

afterEach(() => {
  vi.restoreAllMocks();
  setInnerWidth(1440);
});

describe('useEditorHeight — desktop', () => {
  it('returns DEFAULT_EDITOR_HEIGHT on wide viewports', () => {
    setInnerWidth(1440);
    installVisualViewport(undefined);
    const { result } = renderHook(() => useEditorHeight());
    expect(result.current).toBe(DEFAULT_DESKTOP);
  });

  it('stays at DEFAULT_EDITOR_HEIGHT across desktop resizes', () => {
    setInnerWidth(1440);
    installVisualViewport(undefined);
    const { result } = renderHook(() => useEditorHeight());
    expect(result.current).toBe(DEFAULT_DESKTOP);
    act(() => {
      setInnerWidth(1200);
      window.dispatchEvent(new Event('resize'));
    });
    expect(result.current).toBe(DEFAULT_DESKTOP);
  });
});

describe('useEditorHeight — mobile', () => {
  it('uses visualViewport.height minus the above-editor offset', () => {
    setInnerWidth(500);
    installVisualViewport(900);
    const { result } = renderHook(() => useEditorHeight());
    expect(result.current).toBe(900 - MOBILE_ABOVE_EDITOR_PX);
  });

  it('clamps to a minimum of 120px when the keyboard shrinks the viewport', () => {
    setInnerWidth(500);
    installVisualViewport(300);
    const { result } = renderHook(() => useEditorHeight());
    // 300 - 280 = 20 → clamps to 120
    expect(result.current).toBe(120);
  });

  it('reacts to a visualViewport resize (soft-keyboard open)', () => {
    setInnerWidth(500);
    const vv = installVisualViewport(900);
    const { result } = renderHook(() => useEditorHeight());
    expect(result.current).toBe(900 - MOBILE_ABOVE_EDITOR_PX);
    act(() => {
      vv.setHeight?.(500);
      vv.dispatch();
    });
    expect(result.current).toBe(500 - MOBILE_ABOVE_EDITOR_PX);
  });
});

describe('useEditorHeight — listener cleanup', () => {
  it('removes its resize listeners on unmount', () => {
    setInnerWidth(1440);
    installVisualViewport(undefined);
    const windowRemove = vi.spyOn(window, 'removeEventListener');
    const { unmount } = renderHook(() => useEditorHeight());
    unmount();
    expect(
      windowRemove.mock.calls.some(([event]) => event === 'resize'),
    ).toBe(true);
  });
});
