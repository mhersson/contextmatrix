import { useEffect, useState } from 'react';
import { api } from '../api/client';
import type { DashboardData } from '../types';

export function useDashboardPolling(project: string | null | undefined, intervalMs: number): DashboardData | null {
  const [dashboard, setDashboard] = useState<DashboardData | null>(null);

  useEffect(() => {
    if (!project) return;
    let cancelled = false;
    const fetchDashboard = () => {
      api.getDashboard(project).then((data) => {
        if (!cancelled) setDashboard(data);
      }).catch(() => {
        // non-fatal: board renders with empty fallbacks
      });
    };
    fetchDashboard();
    const interval = setInterval(fetchDashboard, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [project, intervalMs]);

  return dashboard;
}
