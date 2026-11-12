import { useEffect, useState } from 'react';

/**
 * The current time, re-read every `intervalMs`; null stops ticking and keeps
 * the last value. Live durations tick in the browser, with no requests.
 */
export function useNow(intervalMs: number | null): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (intervalMs == null) return;
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(timer);
  }, [intervalMs]);
  return now;
}

// setTimeout's ceiling (~24.8 days); a later deadline re-arms when it fires.
const MAX_TIMEOUT_MS = 2 ** 31 - 1;

/** True once the epoch-ms `deadline` has passed, using one timeout instead of a tick. Null never passes. */
export function useDeadlinePassed(deadline: number | null): boolean {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (deadline == null || deadline <= now) return;
    const wait = Math.min(MAX_TIMEOUT_MS, Math.max(0, deadline - Date.now()));
    const timer = window.setTimeout(() => setNow(Date.now()), wait);
    return () => window.clearTimeout(timer);
  }, [deadline, now]);
  return deadline != null && deadline <= now;
}
