import { useState, useEffect, useCallback, useMemo } from 'react';
import { api, isAPIError } from './api/client';
import { useBoard } from './hooks/useBoard';
import { useAgentId } from './hooks/useAgentId';
import { useCardActions } from './hooks/useCardActions';
import { useKeyboardShortcuts } from './hooks/useKeyboardShortcuts';
import { ToastContext, useToastState } from './hooks/useToast';
import { ErrorBoundary } from './components/ErrorBoundary';
import { AppHeader } from './components/AppHeader';
import type { ViewType } from './components/AppHeader';
import { Board } from './components/Board';
import { Dashboard } from './components/Dashboard';
import { CardPanel } from './components/CardPanel';
import { CreateCardPanel } from './components/CreateCardPanel';
import { ToastContainer } from './components/Toast';
import type { Card, ProjectConfig, CreateCardInput } from './types';

function App() {
  const [projects, setProjects] = useState<ProjectConfig[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>('');
  const [view, setView] = useState<ViewType>('board');
  const [selectedCard, setSelectedCard] = useState<Card | null>(null);
  const [createPanelOpen, setCreatePanelOpen] = useState(false);
  const [flashCardId, setFlashCardId] = useState<string | null>(null);
  const [projectsLoading, setProjectsLoading] = useState(true);
  const [projectsError, setProjectsError] = useState<string | null>(null);
  const toastState = useToastState();
  const { agentId, promptForAgentId } = useAgentId();

  useEffect(() => {
    api
      .getProjects()
      .then((p) => {
        setProjects(p);
        if (p.length > 0 && !selectedProject) setSelectedProject(p[0].name);
      })
      .catch((err) => {
        setProjectsError(isAPIError(err) ? err.error : 'Failed to load projects');
      })
      .finally(() => setProjectsLoading(false));
  }, [selectedProject]);

  const { config, cards, loading, error, connected, updateCardLocally } = useBoard(selectedProject);

  const { handleCardMove, handleCardSave, handleClaim, handleRelease, handleCreateCard } =
    useCardActions({
      selectedProject,
      selectedCard,
      cards,
      updateCardLocally,
      showToast: toastState.showToast,
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
      setTimeout(() => setFlashCardId(null), 2500);
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

  const currentSelectedCard = selectedCard
    ? cards.find((c) => c.id === selectedCard.id) || selectedCard
    : null;
  const panelOpen = !!currentSelectedCard || createPanelOpen;

  useKeyboardShortcuts(
    useMemo(
      () => [
        { key: 'n', handler: () => { if (!panelOpen && config) handleOpenCreate(); } },
        { key: 'b', handler: () => { if (!panelOpen) setView('board'); } },
        { key: 'd', handler: () => { if (!panelOpen) setView('dashboard'); } },
        ...projects.map((_, i) => ({
          key: String(i + 1),
          handler: () => { if (i < projects.length) setSelectedProject(projects[i].name); },
        })),
      ],
      [panelOpen, config, projects, handleOpenCreate]
    )
  );

  return (
    <ToastContext.Provider value={toastState}>
      <div className="min-h-screen flex flex-col" style={{ backgroundColor: 'var(--bg-dim)' }}>
        <AppHeader
          projects={projects}
          selectedProject={selectedProject}
          onSelectProject={setSelectedProject}
          projectsLoading={projectsLoading}
          connected={connected}
          view={view}
          onViewChange={setView}
        />

        <ErrorBoundary>
          <main className="flex-1 overflow-hidden">
            {projectsError && (
              <div className="p-4 rounded m-4" style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}>
                {projectsError}
              </div>
            )}
            {selectedProject && view === 'dashboard' ? (
              <Dashboard project={selectedProject} />
            ) : selectedProject && config ? (
              <Board cards={cards} config={config} loading={loading} error={error}
                onCardClick={handleCardClick} onCardMove={handleCardMove}
                onCreateCard={handleOpenCreate} flashCardId={flashCardId} />
            ) : (
              <div className="flex items-center justify-center h-full">
                <div style={{ color: 'var(--grey1)' }}>
                  {projectsLoading ? 'Loading projects...' : 'Select a project to view the board'}
                </div>
              </div>
            )}
          </main>

          {currentSelectedCard && config && (
            <CardPanel card={currentSelectedCard} config={config}
              onClose={() => setSelectedCard(null)} onSave={handleCardSave}
              onClaim={handleClaim} onRelease={handleRelease}
              onSubtaskClick={handleSubtaskClick} currentAgentId={agentId}
              onPromptAgentId={promptForAgentId} />
          )}
          {createPanelOpen && config && (
            <CreateCardPanel config={config} cards={cards}
              onClose={() => setCreatePanelOpen(false)} onCreate={onCreateCard} />
          )}
        </ErrorBoundary>

        <ToastContainer />
      </div>
    </ToastContext.Provider>
  );
}

export default App;
