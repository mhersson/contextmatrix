import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../../api/client';
import type { RefreshJobStatus, RefreshStatusResponse } from '../../types';

interface Options {
  intervalMs?: number;
  enabled?: boolean;
}

const DEFAULT_INTERVAL_MS = 2000;
const MAX_ERROR_STREAK = 5;
const BASE_RETRY_MS = 1000;
const MAX_RETRY_MS = 30_000;

const NON_IDLE = new Set<string>(['planning', 'running']);

export function useKnowledgeRefreshStatus(
  project: string,
  opts: Options = {},
): { repos: Record<string, RefreshJobStatus>; refresh: () => void } {
  const intervalMs = opts.intervalMs ?? DEFAULT_INTERVAL_MS;
  const enabled = opts.enabled !== false;
  const [repos, setRepos] = useState<Record<string, RefreshJobStatus>>({});
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cancelledRef = useRef(false);
  const errorStreakRef = useRef(0);
  // tickRef holds the latest tick closure so refresh() can invoke it after the
  // polling loop has stopped (e.g. after MAX_ERROR_STREAK plateau).
  const tickRef = useRef<(() => Promise<void>) | null>(null);

  useEffect(() => {
    cancelledRef.current = false;
    errorStreakRef.current = 0;

    const tick = async () => {
      if (!enabled || cancelledRef.current) return;

      try {
        const res: RefreshStatusResponse = await api.getKnowledgeRefreshStatus(project);
        if (cancelledRef.current) return;
        errorStreakRef.current = 0;
        setRepos(res.repos);

        const anyActive = Object.values(res.repos).some((j) => NON_IDLE.has(j.state));
        if (anyActive) {
          timerRef.current = setTimeout(tick, intervalMs);
        }
      } catch {
        if (cancelledRef.current) return;
        errorStreakRef.current += 1;
        if (errorStreakRef.current >= MAX_ERROR_STREAK) {
          // Stop polling. Loop resumes on an explicit refresh() call, which
          // resets the streak and re-invokes tick.
          return;
        }
        const backoff = Math.min(
          BASE_RETRY_MS * 2 ** (errorStreakRef.current - 1),
          MAX_RETRY_MS,
        );
        timerRef.current = setTimeout(tick, backoff);
      }
    };

    tickRef.current = tick;
    void tick();

    return () => {
      cancelledRef.current = true;
      tickRef.current = null;
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [project, enabled, intervalMs]);

  // Manual refresh trigger — used by callers after they POST a new job, or
  // to break out of an error-backoff plateau. Wrapped in useCallback with an
  // empty dep array so the returned identity is stable across renders; all
  // mutable state it touches lives in refs.
  const refresh = useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    errorStreakRef.current = 0;
    // Re-enter the polling loop via the latest tick closure. If the effect
    // has been torn down (cancelledRef set), this is a no-op.
    if (tickRef.current && !cancelledRef.current) {
      void tickRef.current();
    }
  }, []);

  return { repos, refresh };
}
