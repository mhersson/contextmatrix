import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useEditorHeight } from './useEditorHeight';

const DEFAULT_DESKTOP = 375;
const MOBILE_ABOVE_EDITOR_PX = 280;
const KEYBOARD_OPEN_RESERVE = 60;

function setInnerWidth(px: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: px });
}

function setInnerHeight(px: number) {
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: px });
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
  setInnerHeight(900);
  Object.defineProperty(window, 'visualViewport', { configurable: true, value: undefined });
});

describe('useEditorHeight — desktop', () => {
  it('returns DEFAULT_EDITOR_HEIGHT on wide viewports', () => {
    setInnerWidth(1440);
    setInnerHeight(900);
    installVisualViewport(undefined);
    const { result } = renderHook(() => useEditorHeight());
    expect(result.current).toBe(DEFAULT_DESKTOP);
  });

  it('stays at DEFAULT_EDITOR_HEIGHT across desktop resizes', () => {
    setInnerWidth(1440);
    setInnerHeight(900);
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

describe('useEditorHeight — mobile, keyboard closed', () => {
  it('uses visualViewport.height minus the above-editor offset when keyboard is closed', () => {
    // Large viewport: vvh equals innerHeight (no keyboard), keyboard-open check passes false.
    setInnerWidth(500);
    setInnerHeight(900);
    installVisualViewport(900);
    const { result } = renderHook(() => useEditorHeight());
    // 900 - 280 = 620; 50% floor = 450 → 620 wins
    expect(result.current).toBe(900 - MOBILE_ABOVE_EDITOR_PX);
  });

  it('applies 50% innerHeight floor when vvh - MOBILE_ABOVE_EDITOR_PX would dip below it', () => {
    // Tall phone (innerHeight=600) but visualViewport also 600 (keyboard closed).
    // 600 - 280 = 320; 50% of 600 = 300 → 320 wins (above floor).
    setInnerWidth(500);
    setInnerHeight(600);
    installVisualViewport(600);
    const { result } = renderHook(() => useEditorHeight());
    expect(result.current).toBe(600 - MOBILE_ABOVE_EDITOR_PX);
  });

  it('50% floor takes effect when even keyboard-closed formula dips below it', () => {
    // Very small screen: innerHeight = 400, vvh = 400 (no keyboard).
    // 400 - 280 = 120; 50% of 400 = 200 → floor wins.
    setInnerWidth(500);
    setInnerHeight(400);
    installVisualViewport(400);
    const { result } = renderHook(() => useEditorHeight());
    expect(result.current).toBe(400 * 0.5);
  });
});

describe('useEditorHeight — mobile, keyboard open', () => {
  it('applies 50% innerHeight floor when vvh - KEYBOARD_OPEN_RESERVE dips below it (floor wins)', () => {
    // iPhone SE: innerHeight=667, keyboard shrinks vvh to 377.
    // keyboard open: 667 - 377 = 290 > 100 → yes.
    // 377 - 60 = 317; 50% of 667 = 333.5 → floor 333.5 wins.
    setInnerWidth(375);
    setInnerHeight(667);
    installVisualViewport(377);
    const { result } = renderHook(() => useEditorHeight());
    // 377 - 60 = 317 < 333.5, so result = Math.max(317, 333.5) = 333.5
    expect(result.current).toBe(Math.max(377 - KEYBOARD_OPEN_RESERVE, 667 * 0.5));
  });

  it('uses vvh - KEYBOARD_OPEN_RESERVE when that value exceeds 50% floor (reserve wins)', () => {
    // Large phone: innerHeight=800, keyboard shrinks vvh to 650.
    // keyboard open: 800 - 650 = 150 > 100 → yes.
    // 650 - 60 = 590; 50% of 800 = 400 → 590 wins.
    setInnerWidth(500);
    setInnerHeight(800);
    installVisualViewport(650);
    const { result } = renderHook(() => useEditorHeight());
    expect(result.current).toBe(650 - KEYBOARD_OPEN_RESERVE);
  });

  it('applies 50% innerHeight floor when vvh - KEYBOARD_OPEN_RESERVE dips below it', () => {
    // innerHeight=667, vvh=200 (extreme keyboard case).
    // 200 - 60 = 140; 50% of 667 = 333.5 → floor wins.
    setInnerWidth(375);
    setInnerHeight(667);
    installVisualViewport(200);
    const { result } = renderHook(() => useEditorHeight());
    expect(result.current).toBe(667 * 0.5);
  });
});

describe('useEditorHeight — mobile keyboard transitions', () => {
  it('reacts to visualViewport resize: keyboard open then keyboard close', () => {
    setInnerWidth(500);
    setInnerHeight(900);
    // Start: keyboard closed
    const vv = installVisualViewport(900);
    const { result } = renderHook(() => useEditorHeight());
    expect(result.current).toBe(900 - MOBILE_ABOVE_EDITOR_PX);

    // Keyboard opens: vvh shrinks to 500 (900 - 500 = 400 > 100 → keyboard open)
    act(() => {
      vv.setHeight?.(500);
      vv.dispatch();
    });
    // 500 - 60 = 440; 50% of 900 = 450 → Math.max(440, 450) = 450
    expect(result.current).toBe(Math.max(500 - KEYBOARD_OPEN_RESERVE, 900 * 0.5));

    // Keyboard closes: vvh returns to 900
    act(() => {
      vv.setHeight?.(900);
      vv.dispatch();
    });
    expect(result.current).toBe(900 - MOBILE_ABOVE_EDITOR_PX);
  });

  it('reacts to a visualViewport resize (soft-keyboard open)', () => {
    setInnerWidth(500);
    setInnerHeight(1000);
    const vv = installVisualViewport(900);
    const { result } = renderHook(() => useEditorHeight());
    // 900 equal to innerHeight(1000)? 1000-900=100, NOT > 100 → keyboard closed
    expect(result.current).toBe(900 - MOBILE_ABOVE_EDITOR_PX);
    act(() => {
      vv.setHeight?.(500);
      vv.dispatch();
    });
    // 1000-500=500>100 → keyboard open; 500-60=440; 50%=500 → Math.max(440,500)=500
    expect(result.current).toBe(Math.max(500 - KEYBOARD_OPEN_RESERVE, 1000 * 0.5));
  });
});

describe('useEditorHeight — listener cleanup', () => {
  it('removes its resize listeners on unmount', () => {
    setInnerWidth(1440);
    setInnerHeight(900);
    installVisualViewport(undefined);
    const windowRemove = vi.spyOn(window, 'removeEventListener');
    const { unmount } = renderHook(() => useEditorHeight());
    unmount();
    expect(
      windowRemove.mock.calls.some(([event]) => event === 'resize'),
    ).toBe(true);
  });

  it('removes its resize listener from visualViewport on unmount', () => {
    setInnerWidth(500);
    setInnerHeight(900);
    // Install a real vv mock with a spy on removeEventListener.
    const removedListeners: EventListenerOrEventListenerObject[] = [];
    const addedListeners = new Set<EventListenerOrEventListenerObject>();
    const vv = {
      height: 900,
      addEventListener: (_: string, l: EventListenerOrEventListenerObject) => addedListeners.add(l),
      removeEventListener: (_: string, l: EventListenerOrEventListenerObject) => removedListeners.push(l),
    } as unknown as VisualViewport;
    Object.defineProperty(window, 'visualViewport', { configurable: true, value: vv });

    const { unmount } = renderHook(() => useEditorHeight());
    expect(addedListeners.size).toBe(1);

    unmount();

    // The same listener that was added must have been removed.
    expect(removedListeners).toHaveLength(1);
    expect(addedListeners.has(removedListeners[0])).toBe(true);
  });
});
