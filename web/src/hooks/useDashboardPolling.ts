import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { BoardEvent, DashboardData } from '../types';
import { useSSEBus } from './useSSEBus';

export function useDashboardPolling(project: string | null | undefined, intervalMs: number): DashboardData | null {
  const [dashboard, setDashboard] = useState<DashboardData | null>(null);
  const { subscribe } = useSSEBus();

  // Polling fallback — keeps the dashboard fresh even when SSE is unavailable.
  useEffect(() => {
    if (!project) return;
    let cancelled = false;
    api.getDashboard(project).then((data) => {
      if (!cancelled) setDashboard(data);
    }).catch(() => {
      // non-fatal: board renders with empty fallbacks
    });
    const interval = setInterval(() => {
      api.getDashboard(project).then((data) => {
        if (!cancelled) setDashboard(data);
      }).catch(() => {
        // non-fatal: board renders with empty fallbacks
      });
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

    const handler = (event: BoardEvent) => {
      if (event.project === project) {
        api.getDashboard(project).then((data) => {
          if (!cancelled) setDashboard(data);
        }).catch(() => {
          // non-fatal: board renders with empty fallbacks
        });
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
  }, [project, subscribe]);

  return dashboard;
}
