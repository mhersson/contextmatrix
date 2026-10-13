import { useEffect, useState } from 'react';
import { api, isAPIError } from '../api/client';

export interface BackendHealthState {
  maxAgents: number | undefined;
  runningContainers: number | undefined;
  status: BackendStatus;
}

/** unknown until the first poll settles; disabled when no task backend is configured. */
export type BackendStatus = 'unknown' | 'ok' | 'unreachable' | 'disabled';

/** With `enabled` false (no task backend configured) nothing is polled and the status is disabled. */
export function useBackendHealth(intervalMs: number, enabled = true): BackendHealthState {
  const [maxAgents, setMaxAgents] = useState<number | undefined>(undefined);
  const [runningContainers, setRunningContainers] = useState<number | undefined>(undefined);
  const [status, setStatus] = useState<BackendStatus>('unknown');

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    let inFlight: AbortController | null = null;
    const fetchBackendHealth = () => {
      if (cancelled) return;
      if (document.visibilityState !== 'visible') return;
      if (inFlight) inFlight.abort();
      const ctrl = new AbortController();
      inFlight = ctrl;
      api
        .getBackendHealth(ctrl.signal)
        .then((h) => {
          if (cancelled || ctrl.signal.aborted) return;
          setMaxAgents(h.max_concurrent);
          setRunningContainers(h.running_containers);
          setStatus('ok');
        })
        .catch((err) => {
          if (ctrl.signal.aborted) return;
          if (err instanceof DOMException && err.name === 'AbortError') return;
          const disabled = isAPIError(err) && err.code === 'BACKEND_DISABLED';
          if (!disabled) console.warn('backend health poll failed:', err);
          // Counts from an earlier poll must not outlive the backend they described.
          setMaxAgents(undefined);
          setRunningContainers(undefined);
          setStatus(disabled ? 'disabled' : 'unreachable');
        });
    };
    fetchBackendHealth();
    const interval = setInterval(fetchBackendHealth, intervalMs);
    const onVisibilityChange = () => {
      if (document.visibilityState === 'visible') fetchBackendHealth();
    };
    document.addEventListener('visibilitychange', onVisibilityChange);
    return () => {
      cancelled = true;
      if (inFlight) inFlight.abort();
      clearInterval(interval);
      document.removeEventListener('visibilitychange', onVisibilityChange);
    };
  }, [intervalMs, enabled]);

  if (!enabled) return { maxAgents: undefined, runningContainers: undefined, status: 'disabled' };
  return { maxAgents, runningContainers, status };
}
