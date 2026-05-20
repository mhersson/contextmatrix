import { useCallback, useEffect, useRef } from 'react';

export interface TimeoutRef {
  schedule: (fn: () => void, ms: number) => void;
  cancel: () => void;
}

export function useTimeoutRef(): TimeoutRef {
  const ref = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const cancel = useCallback(() => {
    if (ref.current !== undefined) {
      clearTimeout(ref.current);
      ref.current = undefined;
    }
  }, []);

  const schedule = useCallback((fn: () => void, ms: number) => {
    cancel();
    ref.current = setTimeout(() => {
      ref.current = undefined;
      fn();
    }, ms);
  }, [cancel]);

  useEffect(() => cancel, [cancel]);

  return { schedule, cancel };
}
