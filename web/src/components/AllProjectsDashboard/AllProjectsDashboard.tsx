import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../../api/client';
import { useOncePerKeyToast } from '../../hooks/useOncePerKeyToast';
import { useOptionalAuth } from '../../hooks/useAuth';
import { useProjects } from '../../hooks/useProjects';
import { useProjectSummariesContext } from '../../hooks/ProjectSummariesProvider';
import { useSync } from '../../hooks/useSync';
import { useToast } from '../../hooks/useToast';
import type { AppConfig } from '../../types';
import { CommandStrip } from './CommandStrip';
import { KpiRow } from './KpiRow';
import { ProjectsTable } from './ProjectsTable';
import { TopCardsPanel } from './TopCardsPanel';
import { AgentsOnDuty } from './AgentsOnDuty';
import { CostByModel } from './CostByModel';
import { ActivityFeed } from './ActivityFeed';
import { FootStrip } from './FootStrip';
import {
  aggregateDashboards,
  buildPrefixMap,
} from './utils';

interface AllProjectsDashboardProps {
  onNewProject?: () => void;
}

export function AllProjectsDashboard({ onNewProject }: AllProjectsDashboardProps) {
  const { projects, refreshProjects } = useProjects();
  const { summaries, errors, loading, refresh } = useProjectSummariesContext();
  const { syncStatuses, refresh: refreshSyncStatuses } = useSync();
  const { showToast } = useToast();

  // UX honesty, not a security boundary - the API 403s a non-admin project
  // create anyway (multi mode is admin-gated). Mirrors Sidebar's gating.
  const auth = useOptionalAuth();
  const canCreateProject = !(auth?.mode === 'multi' && !auth?.user?.is_admin);

  const [appConfig, setAppConfig] = useState<AppConfig | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  // Each failure type toasts at most once per dashboard mount so a recurring
  // 30s refresh of a still-broken backend doesn't carpet-bomb the UI.
  const showOnce = useOncePerKeyToast((msg) => showToast(msg, 'error'));
  const lastFailedCountRef = useRef(0);

  useEffect(() => {
    api
      .getAppConfig()
      .then((cfg) => {
        setAppConfig(cfg);
      })
      .catch((err) => {
        setAppConfig(null);
        console.warn('getAppConfig failed:', err);
        showOnce('appConfig', 'Could not load app config');
      });
  }, [showOnce]);

  // Surface partial-failure toasts when the set of failed project fetches
  // grows. Shrinking (recovery) is silent.
  useEffect(() => {
    const n = errors.size;
    if (n > 0 && n > lastFailedCountRef.current) {
      const label = n === 1 ? '1 project failed to load' : `${n} projects failed to load`;
      showToast(label, 'error');
    }
    lastFailedCountRef.current = n;
  }, [errors, showToast]);

  const aggregated = useMemo(() => aggregateDashboards(summaries), [summaries]);
  const prefixMap = useMemo(() => buildPrefixMap(projects), [projects]);

  const totalCards = useMemo(
    () => Object.values(aggregated.state_counts).reduce((a, b) => a + b, 0),
    [aggregated],
  );
  const stalled = aggregated.state_counts.stalled ?? 0;
  const agentCount = aggregated.active_agents.length;

  const stats = useMemo(
    () => ({
      projectCount: projects.length,
      totalCards,
      agentCount,
      stalledCount: stalled,
    }),
    [projects.length, totalCards, agentCount, stalled],
  );

  const handleRefresh = useCallback(async () => {
    setRefreshing(true);
    try {
      await Promise.allSettled([refreshProjects(), refresh(), refreshSyncStatuses()]);
    } finally {
      setRefreshing(false);
    }
  }, [refresh, refreshProjects, refreshSyncStatuses]);

  const handleNewProject = useCallback(() => {
    if (onNewProject) onNewProject();
  }, [onNewProject]);

  const rootStyle = {
    backgroundColor: 'var(--bg-dim)',
    color: 'var(--fg)',
    fontFamily: 'var(--font-sans)',
    height: '100%',
    display: 'flex',
    flexDirection: 'column',
    minHeight: 0,
  } as const;

  // Keep CommandStrip mounted on the loading splash so the mobile hamburger
  // is reachable before the first dashboard fetch resolves.
  if (loading && summaries.size === 0 && projects.length === 0) {
    return (
      <div className="apd-root" style={rootStyle}>
        <CommandStrip
          stats={null}
          onRefresh={handleRefresh}
          onNewProject={handleNewProject}
          refreshing={refreshing}
          showNewProject={canCreateProject}
        />
        <div
          className="flex items-center justify-center"
          style={{ flex: 1, color: 'var(--grey1)' }}
        >
          Loading dashboard…
        </div>
        <FootStrip version={appConfig?.version ?? null} syncStatuses={syncStatuses} />
      </div>
    );
  }

  return (
    <div className="apd-root" style={rootStyle}>
      <CommandStrip
        stats={stats}
        onRefresh={handleRefresh}
        onNewProject={handleNewProject}
        refreshing={refreshing}
        showNewProject={canCreateProject}
      />
      <div className="apd-body">
        <KpiRow
          costLast30dUsd={aggregated.total_cost_usd_last_30d ?? 0}
          costPrior30dUsd={aggregated.total_cost_usd_prior_30d ?? 0}
          costSeries30d={aggregated.cost_series_30d}
          costHasEstimates={aggregated.total_cost_has_estimates_last_30d}
          stateCountsParents={aggregated.state_counts_parents}
          doneTodayParents={aggregated.cards_completed_today_parents}
          chatCostLast30dUsd={aggregated.chat_cost_usd_last_30d ?? 0}
          chatCostPrior30dUsd={aggregated.chat_cost_usd_prior_30d ?? 0}
          chatCostSeries30d={aggregated.chat_cost_series_30d}
        />
        <div className="apd-deck">
          <ProjectsTable projects={projects} summaries={summaries} boardsRepos={appConfig?.boards_repos ?? []} />
          <AgentsOnDuty
            activeAgents={aggregated.active_agents}
            stalledCount={stalled}
            prefixMap={prefixMap}
          />
          <CostByModel modelCosts={aggregated.model_costs_30d ?? []} />
          <TopCardsPanel cardCosts={aggregated.card_costs_30d ?? []} prefixMap={prefixMap} projects={projects} />
          <ActivityFeed prefixMap={prefixMap} />
        </div>
      </div>
      <FootStrip version={appConfig?.version ?? null} syncStatuses={syncStatuses} />
    </div>
  );
}
