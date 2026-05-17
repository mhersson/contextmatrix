import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../../api/client';
import { useProjects } from '../../hooks/useProjects';
import { useProjectSummaries } from '../../hooks/useProjectSummaries';
import { useSSEBus } from '../../hooks/useSSEBus';
import { useToast } from '../../hooks/useToast';
import type { AppConfig, SyncStatus } from '../../types';
import { UtilityBar } from './UtilityBar';
import { PageHeader } from './PageHeader';
import { KpiRow } from './KpiRow';
import { ProjectsTable } from './ProjectsTable';
import { TopCardsPanel } from './TopCardsPanel';
import { CostAgentsPanel } from './CostAgentsPanel';
import { ActivityFeed } from './ActivityFeed';
import { FootStrip } from './FootStrip';
import {
  aggregateDashboards,
  buildPrefixMap,
  openTaskCount,
  summarySentence,
} from './utils';

interface AllProjectsDashboardProps {
  onNewProject?: () => void;
}

export function AllProjectsDashboard({ onNewProject }: AllProjectsDashboardProps) {
  const { projects, refreshProjects } = useProjects();
  const projectNames = useMemo(() => projects.map((p) => p.name), [projects]);
  const { summaries, errors, loading, refresh } = useProjectSummaries(projectNames);
  const { subscribe } = useSSEBus();
  const { showToast } = useToast();

  const [appConfig, setAppConfig] = useState<AppConfig | null>(null);
  const [syncStatus, setSyncStatus] = useState<SyncStatus | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  // Each failure type toasts at most once per dashboard mount so a recurring
  // 30s refresh of a still-broken backend doesn't carpet-bomb the UI.
  const toastedRef = useRef({ appConfig: false, sync: false });
  const lastFailedCountRef = useRef(0);

  useEffect(() => {
    api
      .getAppConfig()
      .then((cfg) => {
        setAppConfig(cfg);
        toastedRef.current.appConfig = false;
      })
      .catch((err) => {
        setAppConfig(null);
        console.warn('getAppConfig failed:', err);
        if (!toastedRef.current.appConfig) {
          toastedRef.current.appConfig = true;
          showToast('Could not load app config', 'error');
        }
      });
  }, [showToast]);

  const fetchSync = useCallback(() => {
    api
      .getSyncStatus()
      .then((s) => {
        setSyncStatus(s);
        toastedRef.current.sync = false;
      })
      .catch((err) => {
        console.warn('getSyncStatus failed:', err);
        if (!toastedRef.current.sync) {
          toastedRef.current.sync = true;
          showToast('Sync status unavailable', 'error');
        }
      });
  }, [showToast]);

  useEffect(() => {
    fetchSync();
  }, [fetchSync]);

  useEffect(() => {
    return subscribe('sync.*', () => fetchSync());
  }, [subscribe, fetchSync]);

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
  const open = openTaskCount(aggregated.state_counts);
  const inProgress = aggregated.state_counts.in_progress ?? 0;
  const stalled = aggregated.state_counts.stalled ?? 0;
  const blockedProjects = useMemo(() => {
    let n = 0;
    for (const name of projectNames) {
      const d = summaries.get(name);
      if (d && (d.state_counts.blocked ?? 0) > 0) n++;
    }
    return n;
  }, [projectNames, summaries]);
  const agentCount = aggregated.active_agents.length;

  const summary = useMemo(
    () => summarySentence(projects.length, totalCards, agentCount, stalled, blockedProjects),
    [projects.length, totalCards, agentCount, stalled, blockedProjects],
  );

  const handleRefresh = useCallback(async () => {
    setRefreshing(true);
    try {
      await Promise.allSettled([
        refreshProjects(),
        refresh(),
        api
          .getSyncStatus()
          .then(setSyncStatus)
          .catch((err) => {
            console.warn('getSyncStatus failed:', err);
          }),
      ]);
    } finally {
      setRefreshing(false);
    }
  }, [refresh, refreshProjects]);

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

  // Keep UtilityBar mounted on the loading splash so the mobile hamburger
  // is reachable before the first dashboard fetch resolves.
  if (loading && summaries.size === 0 && projects.length === 0) {
    return (
      <div className="apd-root" style={rootStyle}>
        <UtilityBar syncStatus={syncStatus} version={appConfig?.version ?? null} />
        <div
          className="flex items-center justify-center"
          style={{ flex: 1, color: 'var(--grey1)' }}
        >
          Loading dashboard…
        </div>
        <FootStrip version={appConfig?.version ?? null} syncStatus={syncStatus} />
      </div>
    );
  }

  return (
    <div className="apd-root" style={rootStyle}>
      <UtilityBar syncStatus={syncStatus} version={appConfig?.version ?? null} />
      <div className="apd-scroll" style={{ flex: 1, minHeight: 0, overflowY: 'auto' }}>
        <PageHeader
          summary={summary}
          projectCount={projects.length}
          onRefresh={handleRefresh}
          onNewProject={handleNewProject}
          refreshing={refreshing}
        />
        <div className="apd-section-pad">
          <KpiRow
            openTasks={open}
            inProgress={inProgress}
            doneToday={aggregated.cards_completed_today}
            totalCostUsd={aggregated.total_cost_usd}
          />
        </div>
        <div className="apd-section-pad apd-grid-asym">
          <ProjectsTable projects={projects} summaries={summaries} errors={errors} />
          <TopCardsPanel cardCosts={aggregated.card_costs} prefixMap={prefixMap} />
        </div>
        <div className="apd-section-pad apd-grid-asym">
          <CostAgentsPanel
            agentCosts={aggregated.agent_costs}
            activeAgents={aggregated.active_agents}
            stalledCount={stalled}
            prefixMap={prefixMap}
          />
          <ActivityFeed prefixMap={prefixMap} />
        </div>
      </div>
      <FootStrip version={appConfig?.version ?? null} syncStatus={syncStatus} />
    </div>
  );
}
