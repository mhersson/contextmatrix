import { useState, useEffect, useCallback } from 'react';
import { api } from '../../api/client';
import type { DashboardData } from '../../types';
import { SummaryCards } from './SummaryCards';
import { ActiveAgentsFeed } from './ActiveAgentsFeed';
import { CostTable } from './CostTable';

interface DashboardProps {
  project: string;
  epicKey?: string;
}

const REFRESH_INTERVAL = 30000;

export function Dashboard({ project, epicKey }: DashboardProps) {
  const [data, setData] = useState<DashboardData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Reset loading + clear data when project changes (render-time pattern).
  const [prevProject, setPrevProject] = useState(project);
  if (project !== prevProject) {
    setPrevProject(project);
    setLoading(true);
    setData(null);
    setError(null);
  }

  const fetchDashboard = useCallback(async () => {
    try {
      const result = await api.getDashboard(project);
      setData(result);
      setError(null);
    } catch {
      setError('Failed to load dashboard');
    } finally {
      setLoading(false);
    }
  }, [project]);

  useEffect(() => {
    let cancelled = false;
    api.getDashboard(project).then((result) => {
      if (cancelled) return;
      setData(result);
      setError(null);
      setLoading(false);
    }).catch(() => {
      if (cancelled) return;
      setError('Failed to load dashboard');
      setLoading(false);
    });
    const interval = setInterval(fetchDashboard, REFRESH_INTERVAL);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [project, fetchDashboard]);

  if (loading && !data) {
    return (
      <div className="flex items-center justify-center h-full">
        <div style={{ color: 'var(--grey1)' }}>Loading dashboard...</div>
      </div>
    );
  }

  if (error && !data) {
    return (
      <div className="p-4 rounded m-4" style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}>
        {error}
      </div>
    );
  }

  if (!data) return null;

  return (
    <div className="flex flex-col h-full">
      <div className="px-4 py-3 border-b border-[var(--bg3)]">
        <h1 className="text-lg font-medium text-[var(--fg)]">
          {epicKey && <><span className="text-[var(--grey1)]">{epicKey}</span>{' '}</>}
          {project}
        </h1>
        <p className="text-xs text-[var(--grey1)]">Cost overview</p>
      </div>
      <div className="p-6 space-y-6 overflow-y-auto flex-1">
      <SummaryCards
        stateCounts={data.state_counts}
        totalCost={data.total_cost_usd}
        completedToday={data.cards_completed_today}
      />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <ActiveAgentsFeed agents={data.active_agents} />
        <CostTable agentCosts={data.agent_costs} cardCosts={data.card_costs} />
      </div>
      </div>
    </div>
  );
}
