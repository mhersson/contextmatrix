import { useState, useEffect, useCallback, useMemo, useRef, lazy, Suspense } from 'react';
import { useParams, useNavigate, useSearchParams, Routes, Route } from 'react-router-dom';
import { useBoard } from '../../hooks/useBoard';
import { useSync } from '../../hooks/useSync';
import { useAgentId } from '../../hooks/useAgentId';
import { useCardActions } from '../../hooks/useCardActions';
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts';
import { useProjects } from '../../hooks/useProjects';
import { useToast } from '../../hooks/useToast';
import { useRunnerLogs } from '../../hooks/useRunnerLogs';
import { useResizeDivider } from '../../hooks/useResizeDivider';
import { AppHeader } from '../AppHeader';
import { Board } from '../Board';
import { CardPanel } from '../CardPanel';
import { CreateCardPanel } from '../CreateCardPanel';
import { ErrorBoundary } from '../ErrorBoundary';
import { NotFound } from '../NotFound';
import { RunnerConsole } from '../RunnerConsole';
import { api, isAPIError } from '../../api/client';
import type { BoardEvent, Card, CreateCardInput, DashboardData } from '../../types';
import type { ActivityEntry } from '../Board/NowRail';
import { useSSEBus } from '../../hooks/useSSEBus';

// Lazy-load secondary routes — only downloaded when the user navigates to them.
const Dashboard = lazy(() =>
  import('../Dashboard').then((m) => ({ default: m.Dashboard }))
);
const ProjectSettings = lazy(() =>
  import('../ProjectSettings/ProjectSettings').then((m) => ({ default: m.ProjectSettings }))
);
const KnowledgeBase = lazy(() =>
  import('../KnowledgeBase').then((m) => ({ default: m.KnowledgeBase }))
);

function relativeTime(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  return `${h}h ago`;
}

function RouteFallback() {
  return (
    <div className="flex items-center justify-center h-full" style={{ color: 'var(--grey1)' }}>
      <div className="text-sm">Loading...</div>
    </div>
  );
}

export function ProjectShell() {
  const { project } = useParams<{ project: string }>();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { projects } = useProjects();
  const { showToast } = useToast();
  const { agentId } = useAgentId();

  const [selectedCard, setSelectedCard] = useState<Card | null>(null);
  const [createPanelOpen, setCreatePanelOpen] = useState(false);
  const [flashCardId, setFlashCardId] = useState<string | null>(null);
  const [consoleOpen, setConsoleOpen] = useState(false);
  // Deep-link state markers — declared up here so the in-render reset on
  // project change (below) can clear them. See the explanatory comment
  // alongside the usage block further down for the full design rationale.
  const [deepLinkConsumed, setDeepLinkConsumed] = useState(false);
  const [pendingUrlStrip, setPendingUrlStrip] = useState(false);
  const flashTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const mainRef = useRef<HTMLDivElement>(null);
  const { boardPercent, isDragging, handleProps: dividerHandleProps } = useResizeDivider({
    containerRef: mainRef,
    enabled: consoleOpen,
  });

  useEffect(() => {
    return () => clearTimeout(flashTimerRef.current);
  }, []);

  const { syncStatus, triggerSync, handleSyncEvent } = useSync();

  const [dashboard, setDashboard] = useState<DashboardData | null>(null);
  const [liveActivity, setLiveActivity] = useState<ActivityEntry[]>([]);
  const [backfillLoaded, setBackfillLoaded] = useState(false);
  const [runnerMaxAgents, setRunnerMaxAgents] = useState<number | undefined>(undefined);
  const bus = useSSEBus();

  // In-render reset on project change. This pattern (a `prev*` state marker
  // compared in render) replaces a `useEffect(..., [project])` that called
  // setState — the effect path was flagged by react-hooks/set-state-in-effect
  // because it produced a cascading render after the project switch.
  const [prevProject, setPrevProject] = useState(project);
  if (project !== prevProject) {
    setPrevProject(project);
    setSelectedCard(null);
    setCreatePanelOpen(false);
    setLiveActivity([]);
    setBackfillLoaded(false);
    setDeepLinkConsumed(false);
    setPendingUrlStrip(false);
  }

  // Fetch the runner's global max_concurrent from /api/runner/health.
  // Runner disabled or unreachable → leave undefined (NowRail degrades).
  useEffect(() => {
    let cancelled = false;
    api.getRunnerHealth()
      .then((h) => { if (!cancelled) setRunnerMaxAgents(h.max_concurrent); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, []);

  // Fetch dashboard data for the board route (board reads active_agents +
  // cards_completed_today). Polls at the same cadence as the Dashboard
  // component for parity.
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
    const interval = setInterval(fetchDashboard, 30000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [project]);

  // Subscribe to SSE for the NowRail activity feed.
  // Action vocabulary is normalised to the same set the backfill emits
  // (the raw activity_log `Action` field — `"claimed"`, `"state_changed"`,
  // `"released"`) so the dedup-by-id below is symmetric across both channels.
  // The event id includes the action so the same card_id+timestamp+agent
  // tuple from two different SSE events doesn't collide.
  //
  // `agent` defaults to "system" when missing — matches the activity log
  // convention used by appendStateChangeLog for stall/parent auto-transitions
  // and other server-side state changes. Without this, system-driven SSE
  // events would silently never reach NowRail.
  useEffect(() => {
    if (!project) return;
    const handler = (evt: BoardEvent) => {
      if (evt.project !== project) return;
      const action =
        evt.type === 'card.claimed' ? 'claimed' :
        evt.type === 'card.state_changed' ? 'state_changed' :
        evt.type === 'card.released' ? 'released' :
        null;
      if (!action) return;
      const agent = evt.agent || 'system';
      setLiveActivity((curr) => [
        { id: `${evt.timestamp}-${evt.card_id}-${agent}-${action}`, agent, action, cardId: evt.card_id, ts: evt.timestamp },
        ...curr,
      ].slice(0, 50));
    };
    const unsubs = [
      bus.subscribe('card.claimed', handler),
      bus.subscribe('card.state_changed', handler),
      bus.subscribe('card.released', handler),
    ];
    return () => { unsubs.forEach((u) => u()); };
  }, [bus, project]);

  // One-shot historical activity backfill on mount / project change. SSE
  // handles forward updates; this fills in entries older than the page load.
  // Backfill ids use the same shape as the SSE branch above so the merge
  // dedup is symmetric — an event delivered by both channels collapses to
  // a single entry.
  useEffect(() => {
    if (!project) return;
    let cancelled = false;
    api.getActivity(project, 50)
      .then((resp) => {
        if (cancelled) return;
        const backfill: ActivityEntry[] = resp.items.map((e) => ({
          id: `${e.ts}-${e.card_id}-${e.agent}-${e.action}`,
          agent: e.agent,
          action: e.action,
          cardId: e.card_id,
          ts: e.ts,
        }));
        // Merge with any live entries that arrived before backfill resolved,
        // dedup by id, sort newest-first.
        setLiveActivity((curr) => {
          const seen = new Set<string>();
          const merged = [...curr, ...backfill].filter((e) => {
            if (seen.has(e.id)) return false;
            seen.add(e.id);
            return true;
          });
          merged.sort((a, b) => b.ts.localeCompare(a.ts));
          return merged.slice(0, 50);
        });
        setBackfillLoaded(true);
      })
      .catch(() => {
        // Non-fatal: SSE still populates going forward.
      });
    return () => { cancelled = true; };
  }, [project]);

  const syncLabel = syncStatus?.last_sync_time
    ? `git sync · ${relativeTime(syncStatus.last_sync_time)}`
    : 'git sync · idle';

  const handleCardCreated = useCallback((event: BoardEvent) => {
    if (event.data?.source_system === 'github') {
      const title = (event.data?.title as string) || event.card_id;
      showToast(`New issue from GitHub: ${title}`, 'info');
    }
  }, [showToast]);

  const { config, cards, loading, error, connected, updateCardLocally, removeCardLocally, suppressSSE, unsuppressSSE } = useBoard(project || '', undefined, handleSyncEvent, handleCardCreated);

  // Deep-link handling for ?card=ID. Two transitions, both implemented as
  // in-render state-marker patterns (consistent with the prev-project reset
  // above) rather than reactive effects — `react-hooks/set-state-in-effect`
  // forbids calling setState from useEffect.
  //
  // Inbound: when ?card=ID is in the URL and the matching card has loaded,
  // open the CardPanel. `deepLinkConsumed` tracks that we acted on a URL
  // param so the outbound branch can distinguish a closed deep-linked
  // panel (strip the URL) from the initial mount race where selectedCard
  // is still null (don't strip).
  //
  // Outbound: when a consumed deep-link panel closes, mark the URL for
  // stripping via `pendingUrlStrip`. The actual `setSearchParams` call
  // (router mutation = external-system side-effect) runs in an effect.
  //
  // Click-driven panel opens deliberately do NOT write to the URL — full
  // URL ↔ panel sync is a deferred follow-up.
  //
  // The `deepLinkConsumed` / `pendingUrlStrip` state markers are declared
  // up next to the other useState hooks above so the prev-project reset
  // block can clear them on cross-project SPA navigation (otherwise
  // `deepLinkConsumed` stays stuck at true and silently rejects the new
  // project's ?card= param).
  const urlCardId = searchParams.get('card');
  if (urlCardId && !deepLinkConsumed && selectedCard?.id !== urlCardId) {
    const card = cards.find((c) => c.id === urlCardId);
    if (card) {
      setSelectedCard(card);
      setDeepLinkConsumed(true);
    }
  }
  if (deepLinkConsumed && selectedCard === null && urlCardId && !pendingUrlStrip) {
    setPendingUrlStrip(true);
    setDeepLinkConsumed(false);
  }
  if (pendingUrlStrip && !urlCardId) {
    setPendingUrlStrip(false);
  }

  useEffect(() => {
    if (!pendingUrlStrip) return;
    setSearchParams(
      (p) => {
        p.delete('card');
        return p;
      },
      { replace: true },
    );
  }, [pendingUrlStrip, setSearchParams]);

  const {
    handleCardMove, handleCardSave, handleClaim, handleRelease, handleCreateCard,
    handleRunCard, handleStopCard, handleStopAll, handleCardDelete,
  } = useCardActions({
      selectedProject: project || '',
      selectedCard,
      cards,
      updateCardLocally,
      removeCardLocally,
      suppressSSE,
      unsuppressSSE,
      showToast,
      onCardDeleted: () => setSelectedCard(null),
    });

  const { logs: runnerLogs, connected: consoleConnected, error: consoleError, clear: clearLogs } = useRunnerLogs({
    project: project || '',
    enabled: consoleOpen,
  });

  const handleCardClick = useCallback((card: Card) => {
    setSelectedCard(card);
    setCreatePanelOpen(false);
  }, []);

  const handleOpenCreate = useCallback(() => {
    setCreatePanelOpen(true);
    setSelectedCard(null);
  }, []);

  const onCreateCard = useCallback(
    async (input: CreateCardInput, opts?: { run?: boolean; interactive?: boolean }) => {
      const card = await handleCreateCard(input);
      setCreatePanelOpen(false);
      // Optionally fire the runner immediately (Create & Run flow). Update
      // the local card record on success so the board reflects the new
      // runner_status / assigned_agent without waiting for SSE, then open
      // the card panel so the user sees the running card immediately.
      if (opts?.run) {
        try {
          const updated = await api.runCard(card.project, card.id, { interactive: opts.interactive });
          const updatedCard = { ...card, runner_status: updated.runner_status, assigned_agent: updated.assigned_agent };
          updateCardLocally(card.id, {
            runner_status: updated.runner_status,
            assigned_agent: updated.assigned_agent,
          });
          setSelectedCard(updatedCard);
          showToast('Task queued for runner', 'success');
        } catch (err) {
          showToast(isAPIError(err) ? err.error : 'Failed to trigger runner', 'error');
        }
        return;
      }
      setFlashCardId(card.id);
      flashTimerRef.current = setTimeout(() => setFlashCardId(null), 2500);
    },
    [handleCreateCard, updateCardLocally, showToast]
  );

  const handleSubtaskClick = useCallback(
    (cardId: string) => {
      const card = cards.find((c) => c.id === cardId);
      if (card) setSelectedCard(card);
    },
    [cards]
  );

  const handleProjectUpdated = useCallback(
    () => {
      // ProjectsProvider will pick this up via SSE
    },
    []
  );

  const handleProjectDeleted = useCallback(() => {
    const remaining = projects.filter((p) => p.name !== project);
    if (remaining.length > 0) navigate(`/projects/${remaining[0].name}`);
    else navigate('/');
  }, [project, projects, navigate]);

  const currentSelectedCard = selectedCard
    ? cards.find((c) => c.id === selectedCard.id) || selectedCard
    : null;
  const panelOpen = !!currentSelectedCard || createPanelOpen;
  const hasActiveRunners = useMemo(
    () => cards.some((c) => c.runner_status === 'queued' || c.runner_status === 'running'),
    [cards]
  );

  // Card-scoped log stream for CardChat — enabled only when a HITL session is running.
  // This avoids opening a second EventSource from inside CardChat itself.
  const isHITLCardRunning = useMemo(
    () => currentSelectedCard?.runner_status === 'running' && !(currentSelectedCard?.autonomous ?? false),
    [currentSelectedCard?.runner_status, currentSelectedCard?.autonomous],
  );
  const { logs: selectedCardLogs } = useRunnerLogs({
    project: project || '',
    cardId: currentSelectedCard?.id,
    enabled: !!isHITLCardRunning,
  });

  useKeyboardShortcuts(
    useMemo(
      () => [
        { key: 'n', handler: () => { if (!panelOpen && config) handleOpenCreate(); } },
        { key: 'b', handler: () => { if (!panelOpen) navigate(`/projects/${project}`); } },
        { key: 'd', handler: () => { if (!panelOpen) navigate(`/projects/${project}/dashboard`); } },
        { key: 's', handler: () => { if (!panelOpen) navigate(`/projects/${project}/settings`); } },
        { key: 'k', handler: () => { if (!panelOpen) navigate(`/projects/${project}/knowledge`); } },
        { key: 'c', handler: () => { if (!panelOpen && config?.remote_execution?.enabled) setConsoleOpen((prev) => !prev); } },
        ...projects.map((_, i) => ({
          key: String(i + 1),
          handler: () => { if (i < projects.length) navigate(`/projects/${projects[i].name}`); },
        })),
      ],
      [panelOpen, config, project, projects, handleOpenCreate, navigate]
    )
  );

  return (
    <>
      <AppHeader
        project={project || ''} connected={connected} syncStatus={syncStatus} onSyncClick={triggerSync}
        hasActiveRunners={hasActiveRunners}
        onStopAll={handleStopAll}
        runnerEnabled={!!config?.remote_execution?.enabled}
        consoleOpen={consoleOpen}
        onToggleConsole={() => setConsoleOpen((prev) => !prev)}
      />
      <main ref={mainRef} className="flex-1 overflow-hidden flex flex-col">
        <div
          style={{ flex: consoleOpen ? `0 1 ${boardPercent}%` : '1 1 100%' }}
          className={`overflow-hidden ${isDragging ? '' : 'transition-all duration-300'}`}
        >
          <Suspense fallback={<RouteFallback />}>
            <Routes>
              <Route
                index
                element={
                  project && config ? (
                    <Board
                      cards={cards} config={config} loading={loading} error={error}
                      activeAgents={dashboard?.active_agents ?? []}
                      cardsCompletedToday={dashboard?.cards_completed_today ?? 0}
                      cardsCompletedLast7d={dashboard?.cards_completed_last_7d}
                      cardsCompletedPrior7d={dashboard?.cards_completed_prior_7d}
                      metricSeries={dashboard?.metric_series}
                      runnerMaxAgents={runnerMaxAgents}
                      lastSyncLabel={syncLabel}
                      activityEntries={liveActivity}
                      activityBackfillLoaded={backfillLoaded}
                      currentAgent={agentId}
                      onCardClick={handleCardClick} onCardMove={handleCardMove}
                      onCreateCard={handleOpenCreate} flashCardId={flashCardId}
                      onParentClick={handleSubtaskClick}
                    />
                  ) : (
                    <div className="flex items-center justify-center h-full">
                      <div style={{ color: 'var(--grey1)' }}>
                        {loading ? 'Loading board...' : error || 'Project not found'}
                      </div>
                    </div>
                  )
                }
              />
              <Route path="dashboard" element={<Dashboard project={project || ''} />} />
              <Route
                path="settings"
                element={
                  <ProjectSettings
                    project={project || ''}
                    onUpdated={handleProjectUpdated}
                    onDeleted={handleProjectDeleted}
                    showToast={showToast}
                  />
                }
              />
              <Route path="knowledge/*" element={<KnowledgeBase project={project || ''} />} />
              <Route path="*" element={<NotFound />} />
            </Routes>
          </Suspense>
        </div>
        {consoleOpen && (
          <>
            <div
              className="flex-shrink-0 cursor-row-resize"
              {...dividerHandleProps}
            >
              <div
                className="mx-auto rounded-full transition-colors"
                style={{
                  width: 32,
                  height: 4,
                  marginTop: 2,
                  marginBottom: 2,
                  background: isDragging ? 'var(--bg5)' : 'var(--bg3)',
                }}
              />
            </div>
            <RunnerConsole
              logs={runnerLogs}
              connected={consoleConnected}
              error={consoleError}
              onClose={() => setConsoleOpen(false)}
              onClear={clearLogs}
              flexBasis={`${100 - boardPercent}%`}
            />
          </>
        )}
      </main>

      {currentSelectedCard && config && (
        <ErrorBoundary>
          <CardPanel
            card={currentSelectedCard} config={config}
            cardLogs={selectedCardLogs}
            onClose={() => setSelectedCard(null)} onSave={handleCardSave}
            onDelete={handleCardDelete}
            onClaim={handleClaim} onRelease={handleRelease}
            onSubtaskClick={handleSubtaskClick} currentAgentId={agentId}
            onRunCard={handleRunCard} onStopCard={handleStopCard}
          />
        </ErrorBoundary>
      )}
      {createPanelOpen && config && (
        <CreateCardPanel
          config={config} cards={cards}
          onClose={() => setCreatePanelOpen(false)} onCreate={onCreateCard}
        />
      )}
    </>
  );
}
