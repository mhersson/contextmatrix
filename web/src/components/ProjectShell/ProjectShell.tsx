import { useState, useEffect, useCallback, useMemo, useRef, lazy, Suspense } from 'react';
import { useParams, useNavigate, Routes, Route } from 'react-router-dom';
import { useBoard } from '../../hooks/useBoard';
import { useSync } from '../../hooks/useSync';
import { useAgentId } from '../../hooks/useAgentId';
import { useCardActions } from '../../hooks/useCardActions';
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts';
import { useLastProject } from '../../hooks/useLastProject';
import { useProjects } from '../../hooks/useProjects';
import { useToast } from '../../hooks/useToast';
import { useRunnerLogs } from '../../hooks/useRunnerLogs';
import { useResizeDivider } from '../../hooks/useResizeDivider';
import { AppHeader } from '../AppHeader';
import { Board } from '../Board';
import { CardPanel } from '../CardPanel';
import { CreateCardPanel } from '../CreateCardPanel';
import { NotFound } from '../NotFound';
import { RunnerConsole } from '../RunnerConsole';
import type { BoardEvent, Card, CreateCardInput } from '../../types';

// Lazy-load secondary routes — only downloaded when the user navigates to them.
const Dashboard = lazy(() =>
  import('../Dashboard').then((m) => ({ default: m.Dashboard }))
);
const ProjectSettings = lazy(() =>
  import('../ProjectSettings/ProjectSettings').then((m) => ({ default: m.ProjectSettings }))
);

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
  const { agentId, promptForAgentId } = useAgentId();
  const [, setLastProject] = useLastProject();

  const [selectedCard, setSelectedCard] = useState<Card | null>(null);
  const [createPanelOpen, setCreatePanelOpen] = useState(false);
  const [flashCardId, setFlashCardId] = useState<string | null>(null);
  const [consoleOpen, setConsoleOpen] = useState(false);
  const flashTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
  const mainRef = useRef<HTMLDivElement>(null);
  const { boardPercent, isDragging, handleProps: dividerHandleProps } = useResizeDivider({
    containerRef: mainRef,
    enabled: consoleOpen,
  });

  useEffect(() => {
    return () => clearTimeout(flashTimerRef.current);
  }, []);

  useEffect(() => {
    if (project) setLastProject(project);
  }, [project, setLastProject]);

  const [prevProject, setPrevProject] = useState(project);
  if (project !== prevProject) {
    setPrevProject(project);
    setSelectedCard(null);
    setCreatePanelOpen(false);
  }

  const { syncStatus, triggerSync, handleSyncEvent } = useSync();

  const handleCardCreated = useCallback((event: BoardEvent) => {
    if (event.data?.source_system === 'github') {
      const title = (event.data?.title as string) || event.card_id;
      showToast(`New issue from GitHub: ${title}`, 'info');
    }
  }, [showToast]);

  const { config, cards, loading, error, connected, updateCardLocally, removeCardLocally, suppressSSE, unsuppressSSE } = useBoard(project || '', undefined, handleSyncEvent, handleCardCreated);

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
    async (input: CreateCardInput) => {
      const card = await handleCreateCard(input);
      setCreatePanelOpen(false);
      setFlashCardId(card.id);
      flashTimerRef.current = setTimeout(() => setFlashCardId(null), 2500);
    },
    [handleCreateCard]
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
        <CardPanel
          card={currentSelectedCard} config={config}
          cardLogs={selectedCardLogs}
          onClose={() => setSelectedCard(null)} onSave={handleCardSave}
          onDelete={handleCardDelete}
          onClaim={handleClaim} onRelease={handleRelease}
          onSubtaskClick={handleSubtaskClick} currentAgentId={agentId}
          onPromptAgentId={promptForAgentId}
          onRunCard={handleRunCard} onStopCard={handleStopCard}
        />
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
