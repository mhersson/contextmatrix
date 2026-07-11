import { useState, useCallback, useMemo, useRef, lazy, Suspense } from 'react';
import { useTimeoutRef } from '../../hooks/useTimeoutRef';
import { useParams, useNavigate, Routes, Route } from 'react-router-dom';
import { useBoard } from '../../hooks/useBoard';
import { useSync } from '../../hooks/useSync';
import { useIdentity } from '../../hooks/useIdentity';
import { useCardActions } from '../../hooks/useCardActions';
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts';
import { useProjects } from '../../hooks/useProjects';
import { useToast } from '../../hooks/useToast';
import { useWorkerLogs } from '../../hooks/useWorkerLogs';
import { useBackendHealth } from '../../hooks/useBackendHealth';
import { useDashboardPolling } from '../../hooks/useDashboardPolling';
import { useActivityFeed } from '../../hooks/useActivityFeed';
import { useResizeDivider } from '../../hooks/useResizeDivider';
import { useConsoleState } from '../../context/ConsoleStateContext';
import { AppHeader } from '../AppHeader';
import { Board } from '../Board';
import { CardPanel } from '../CardPanel';
import { CreateCardPanel } from '../CreateCardPanel';
import { ErrorBoundary } from '../ErrorBoundary';
import { NotFound } from '../NotFound';
import { WorkerConsole } from '../WorkerConsole';
import { api, isAPIError } from '../../api/client';
import type { BoardEvent, Card, CreateCardInput } from '../../types';
import { useDeepLinkCard } from './useDeepLinkCard';

// Lazy-load secondary routes — only downloaded when the user navigates to them.
const ProjectSettings = lazy(() =>
  import('../ProjectSettings/ProjectSettings').then((m) => ({ default: m.ProjectSettings }))
);

const REFRESH_INTERVAL = 30000;

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
  const { projects } = useProjects();
  const { showToast } = useToast();
  const { identity } = useIdentity();

  const [selectedCard, setSelectedCard] = useState<Card | null>(null);
  const [createPanelOpen, setCreatePanelOpen] = useState(false);
  const [flashCardId, setFlashCardId] = useState<string | null>(null);
  const { isOpen: consoleOpen, toggle: toggleConsole, close: closeConsole } = useConsoleState();
  const flashTimer = useTimeoutRef();
  const mainRef = useRef<HTMLDivElement>(null);
  const { boardPercent, isDragging, handleProps: dividerHandleProps } = useResizeDivider({
    containerRef: mainRef,
    enabled: consoleOpen,
  });

  const { syncStatus, triggerSync, handleSyncEvent } = useSync();

  const dashboard = useDashboardPolling(project, REFRESH_INTERVAL);
  const activity = useActivityFeed(project);
  const { maxAgents: maxWorkers, runningContainers } = useBackendHealth(REFRESH_INTERVAL);

  // In-render reset on project change. This pattern (a `prev*` state marker
  // compared in render) replaces a `useEffect(..., [project])` that called
  // setState — the effect path was flagged by react-hooks/set-state-in-effect
  // because it produced a cascading render after the project switch.
  const [prevProject, setPrevProject] = useState(project);
  if (project !== prevProject) {
    setPrevProject(project);
    setSelectedCard(null);
    setCreatePanelOpen(false);
  }

  const handleCardCreated = useCallback((event: BoardEvent) => {
    if (event.data?.source_system === 'github') {
      const title = (event.data?.title as string) || event.card_id;
      showToast(`New issue from GitHub: ${title}`, 'info');
    }
  }, [showToast]);

  const { config, cards, loading, error, connected, updateCardLocally, removeCardLocally, suppressSSE, unsuppressSSE } = useBoard(project || '', undefined, handleSyncEvent, handleCardCreated);

  // Deep-link handling for ?card=ID — see useDeepLinkCard for full rationale.
  // Click-driven panel opens deliberately do NOT write to the URL.
  useDeepLinkCard({ cards, loading, selectedCard, setSelectedCard, project });

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

  const { logs: workerLogs, connected: consoleConnected, error: consoleError, clear: clearLogs } = useWorkerLogs({
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
      // Optionally fire the worker immediately (Create & Run flow). Update
      // the local card record on success so the board reflects the new
      // worker_status / assigned_agent without waiting for SSE, then open
      // the card panel so the user sees the running card immediately.
      if (opts?.run) {
        try {
          const updated = await api.runCard(card.project, card.id, { interactive: opts.interactive });
          const updatedCard = { ...card, worker_status: updated.worker_status, assigned_agent: updated.assigned_agent };
          updateCardLocally(card.id, {
            worker_status: updated.worker_status,
            assigned_agent: updated.assigned_agent,
          });
          setSelectedCard(updatedCard);
          showToast('Task queued for worker', 'success');
        } catch (err) {
          showToast(isAPIError(err) ? err.error : 'Failed to start worker', 'error');
        }
        return;
      }
      setFlashCardId(card.id);
      flashTimer.schedule(() => setFlashCardId(null), 2500);
    },
    [handleCreateCard, updateCardLocally, showToast, flashTimer]
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
  const hasActiveWorkers = useMemo(
    () => cards.some((c) => c.worker_status === 'queued' || c.worker_status === 'running'),
    [cards]
  );

  // Card-scoped log stream for CardChat — enabled when chat is "live": a HITL
  // session is running, or an autonomous run has co-op discussion turned on
  // (coop_participants >= 2). This avoids opening a second EventSource from
  // inside CardChat itself. Mirrors the isChatLive predicate in
  // CardPanel/CardPanel.tsx — keep both in sync if the liveness rule changes.
  const isCardChatLive = useMemo(
    () =>
      currentSelectedCard?.worker_status === 'running' &&
      (!(currentSelectedCard?.autonomous ?? false) ||
        (currentSelectedCard?.coop_participants ?? 0) >= 2),
    [
      currentSelectedCard?.worker_status,
      currentSelectedCard?.autonomous,
      currentSelectedCard?.coop_participants,
    ],
  );
  const { logs: selectedCardLogs } = useWorkerLogs({
    project: project || '',
    cardId: currentSelectedCard?.id,
    enabled: !!isCardChatLive,
  });

  useKeyboardShortcuts(
    useMemo(
      () => [
        { key: 'n', handler: () => { if (!panelOpen && config) handleOpenCreate(); } },
        { key: 'b', handler: () => { if (!panelOpen) navigate(`/projects/${project}`); } },
        { key: 's', handler: () => { if (!panelOpen) navigate(`/projects/${project}/settings`); } },
        { key: 'c', handler: () => { if (!panelOpen && config?.remote_execution?.enabled) toggleConsole(); } },
        ...projects.map((_, i) => ({
          key: String(i + 1),
          handler: () => { if (i < projects.length) navigate(`/projects/${projects[i].name}`); },
        })),
      ],
      [panelOpen, config, project, projects, handleOpenCreate, navigate, toggleConsole]
    )
  );

  return (
    <>
      <AppHeader
        project={project || ''}
        hasActiveWorkers={hasActiveWorkers}
        onStopAll={handleStopAll}
        remoteExecutionEnabled={!!config?.remote_execution?.enabled}
        consoleOpen={consoleOpen}
        onToggleConsole={toggleConsole}
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
                      cardsCompletedTodayParents={dashboard?.cards_completed_today_parents}
                      cardsCompletedLast7d={dashboard?.cards_completed_last_7d}
                      cardsCompletedLast7dParents={dashboard?.cards_completed_last_7d_parents}
                      cardsCompletedPrior7d={dashboard?.cards_completed_prior_7d}
                      cardsCompletedPrior7dParents={dashboard?.cards_completed_prior_7d_parents}
                      stateCounts={dashboard?.state_counts}
                      stateCountsParents={dashboard?.state_counts_parents}
                      metricSeries={dashboard?.metric_series}
                      maxWorkers={maxWorkers}
                      runningContainers={runningContainers}
                      syncStatus={syncStatus}
                      connected={connected}
                      activityEntries={activity.entries}
                      activityBackfillLoaded={activity.backfillLoaded}
                      currentAgent={identity}
                      onCardClick={handleCardClick} onCardMove={handleCardMove}
                      onCreateCard={handleOpenCreate} flashCardId={flashCardId}
                      onParentClick={handleSubtaskClick}
                      onSyncClick={triggerSync}
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
            <WorkerConsole
              logs={workerLogs}
              connected={consoleConnected}
              error={consoleError}
              onClose={closeConsole}
              onClear={clearLogs}
              flexBasis={`${100 - boardPercent}%`}
            />
          </>
        )}
      </main>

      {currentSelectedCard && config && (
        <ErrorBoundary key={currentSelectedCard.id}>
          <CardPanel
            card={currentSelectedCard} config={config}
            cardLogs={selectedCardLogs}
            onClose={() => setSelectedCard(null)} onSave={handleCardSave}
            onDelete={handleCardDelete}
            onClaim={handleClaim} onRelease={handleRelease}
            onSubtaskClick={handleSubtaskClick} currentAgentId={identity}
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
