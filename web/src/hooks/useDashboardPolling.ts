import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { BoardEvent, DashboardData } from '../types';
import { useSSEBus } from './useSSEBus';

// Module-level helper so the fetch+apply pattern is defined once.
// isCancelled is a thunk so each effect can control its own cancellation flag.
function fetchDashboard(
  project: string,
  setDashboard: (data: DashboardData) => void,
  isCancelled: () => boolean,
): void {
  api.getDashboard(project).then((data) => {
    if (!isCancelled()) setDashboard(data);
  }).catch(() => {
    // non-fatal: board renders with empty fallbacks
  });
}

export function useDashboardPolling(project: string | null | undefined, intervalMs: number): DashboardData | null {
  const [dashboard, setDashboard] = useState<DashboardData | null>(null);
  const { subscribe } = useSSEBus();

  // Polling fallback — keeps the dashboard fresh even when SSE is unavailable.
  useEffect(() => {
    if (!project) return;
    let cancelled = false;
    const isCancelled = () => cancelled;
    fetchDashboard(project, setDashboard, isCancelled);
    const interval = setInterval(() => {
      fetchDashboard(project, setDashboard, isCancelled);
    }, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [project, intervalMs]);

  // SSE subscriptions — trigger an immediate re-fetch on agent state changes
  // so the Now Agents rail updates within ~1 second instead of waiting for
  // the next poll cycle.
  useEffect(() => {
    if (!project) return;
    let cancelled = false;
    const isCancelled = () => cancelled;

    const handler = (event: BoardEvent) => {
      if (event.project === project) {
        fetchDashboard(project, setDashboard, isCancelled);
      }
    };

    const unsubClaimed = subscribe('card.claimed', handler);
    const unsubStateChanged = subscribe('card.state_changed', handler);
    const unsubReleased = subscribe('card.released', handler);

    return () => {
      cancelled = true;
      unsubClaimed();
      unsubStateChanged();
      unsubReleased();
    };
  // subscribe is a stable useCallback (empty deps in useSSEBus) — omitting it
  // is intentional so identity changes never recreate duplicate subscriptions.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project]);

  return dashboard;
}
