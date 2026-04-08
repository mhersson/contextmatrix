/**
 * Returns true when the primary pointing device is coarse (touch screen).
 * Uses the `pointer: coarse` media query with a fallback to maxTouchPoints
 * for environments where matchMedia is unavailable (e.g. jsdom in tests).
 *
 * On touch devices a TouchSensor with a delay constraint is used instead of
 * PointerSensor so that press-and-hold triggers drag while normal scrolling
 * is unaffected.
 */
export function isTouchDevice(): boolean {
  if (typeof window === 'undefined') return false;
  if (typeof window.matchMedia === 'function') {
    return window.matchMedia('(pointer: coarse)').matches;
  }
  return navigator.maxTouchPoints > 0;
}
