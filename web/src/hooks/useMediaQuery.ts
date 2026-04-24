import { useCallback, useSyncExternalStore } from 'react';

/**
 * SSR-safe boolean hook around `window.matchMedia`. Returns `false` in
 * environments without `matchMedia` (jsdom with no stub, SSR). Uses
 * `useSyncExternalStore` so the subscription, snapshot, and state are
 * managed by React without a sync-set-in-effect that would trip the
 * cascading-renders lint.
 */
export function useMediaQuery(query: string): boolean {
  const subscribe = useCallback(
    (callback: () => void) => {
      if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
        return () => {};
      }
      const mq = window.matchMedia(query);
      mq.addEventListener('change', callback);
      return () => mq.removeEventListener('change', callback);
    },
    [query],
  );

  const getSnapshot = useCallback(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false;
    return window.matchMedia(query).matches;
  }, [query]);

  const getServerSnapshot = () => false;

  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
