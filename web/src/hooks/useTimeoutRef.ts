import { useCallback, useEffect, useMemo, useRef } from 'react';

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

  // Memoised so callers can list the whole TimeoutRef in effect/useCallback
  // deps without forcing a re-run every render.
  return useMemo(() => ({ schedule, cancel }), [schedule, cancel]);
}
