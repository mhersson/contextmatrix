import { useCallback, useRef } from 'react';

export type ToastFn = (message: string) => void;

export function useOncePerKeyToast(toast: ToastFn) {
  const seenRef = useRef<Set<string>>(new Set());
  return useCallback(
    (key: string, message: string) => {
      if (seenRef.current.has(key)) return;
      seenRef.current.add(key);
      toast(message);
    },
    [toast],
  );
}
