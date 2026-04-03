import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { useParams, useNavigate, Routes, Route } from 'react-router-dom';
import { useBoard } from '../../hooks/useBoard';
import { useSync } from '../../hooks/useSync';
import { useAgentId } from '../../hooks/useAgentId';
import { useCardActions } from '../../hooks/useCardActions';
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts';
import { useLastProject } from '../../hooks/useLastProject';
import { useProjects } from '../../hooks/useProjects';
import { useToast } from '../../hooks/useToast';
import { AppHeader } from '../AppHeader';
import { Board } from '../Board';
import { Dashboard } from '../Dashboard';
import { ProjectSettings } from '../ProjectSettings/ProjectSettings';
import { CardPanel } from '../CardPanel';
import { CreateCardPanel } from '../CreateCardPanel';
import type { Card, CreateCardInput, ProjectConfig } from '../../types';

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
  const flashTimerRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => {
    return () => clearTimeout(flashTimerRef.current);
  }, []);

  useEffect(() => {
    if (project) setLastProject(project);
  }, [project, setLastProject]);

  useEffect(() => {
    setSelectedCard(null);
    setCreatePanelOpen(false);
  }, [project]);

  const { syncStatus, triggerSync, handleSyncEvent } = useSync();
  const { config, cards, loading, error, connected, updateCardLocally } = useBoard(project || '', undefined, handleSyncEvent);

  const {
    handleCardMove, handleCardSave, handleClaim, handleRelease, handleCreateCard,
    handleRunCard, handleStopCard, handleStopAll,
  } = useCardActions({
      selectedProject: project || '',
      selectedCard,
      cards,
      updateCardLocally,
      showToast,
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
    (_updated: ProjectConfig) => {
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

  useKeyboardShortcuts(
    useMemo(
      () => [
        { key: 'n', handler: () => { if (!panelOpen && config) handleOpenCreate(); } },
        { key: 'b', handler: () => { if (!panelOpen) navigate(`/projects/${project}`); } },
        { key: 'd', handler: () => { if (!panelOpen) navigate(`/projects/${project}/dashboard`); } },
        { key: 's', handler: () => { if (!panelOpen) navigate(`/projects/${project}/settings`); } },
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
      />
      <main className="flex-1 overflow-hidden">
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
        </Routes>
      </main>

      {currentSelectedCard && config && (
        <CardPanel
          card={currentSelectedCard} config={config}
          onClose={() => setSelectedCard(null)} onSave={handleCardSave}
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
