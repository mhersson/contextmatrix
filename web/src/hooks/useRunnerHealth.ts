import { useEffect, useState } from 'react';
import { api } from '../api/client';

export interface RunnerHealthState {
  maxAgents: number | undefined;
  runningContainers: number | undefined;
}

export function useRunnerHealth(intervalMs: number): RunnerHealthState {
  const [maxAgents, setMaxAgents] = useState<number | undefined>(undefined);
  const [runningContainers, setRunningContainers] = useState<number | undefined>(undefined);

  useEffect(() => {
    let cancelled = false;
    let inFlight: AbortController | null = null;
    const fetchRunnerHealth = () => {
      if (cancelled) return;
      if (document.visibilityState !== 'visible') return;
      if (inFlight) inFlight.abort();
      const ctrl = new AbortController();
      inFlight = ctrl;
      api
        .getRunnerHealth(ctrl.signal)
        .then((h) => {
          if (cancelled || ctrl.signal.aborted) return;
          setMaxAgents(h.max_concurrent);
          setRunningContainers(h.running_containers);
        })
        .catch((err) => {
          if (ctrl.signal.aborted) return;
          if (err instanceof DOMException && err.name === 'AbortError') return;
          console.warn('runner health poll failed:', err);
        });
    };
    fetchRunnerHealth();
    const interval = setInterval(fetchRunnerHealth, intervalMs);
    const onVisibilityChange = () => {
      if (document.visibilityState === 'visible') fetchRunnerHealth();
    };
    document.addEventListener('visibilitychange', onVisibilityChange);
    return () => {
      cancelled = true;
      if (inFlight) inFlight.abort();
      clearInterval(interval);
      document.removeEventListener('visibilitychange', onVisibilityChange);
    };
  }, [intervalMs]);

  return { maxAgents, runningContainers };
}
