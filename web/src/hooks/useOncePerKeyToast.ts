import { useCallback, useEffect, useRef } from 'react';

export type ToastFn = (message: string) => void;

// Returns a callback whose identity is stable across the hook's lifetime, even
// when `toast` is a fresh function every render (e.g. an inline arrow). Callers
// can safely list the returned callback in effect/`useCallback` deps without
// triggering re-fires on every render.
export function useOncePerKeyToast(toast: ToastFn) {
  const seenRef = useRef<Set<string>>(new Set());
  const toastRef = useRef(toast);
  useEffect(() => {
    toastRef.current = toast;
  });
  return useCallback((key: string, message: string) => {
    if (seenRef.current.has(key)) return;
    seenRef.current.add(key);
    toastRef.current(message);
  }, []);
}
