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
import { ProjectSettings } from './components/ProjectSettings/ProjectSettings';
import { NewProjectWizard } from './components/NewProjectWizard/NewProjectWizard';
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
  const [newProjectOpen, setNewProjectOpen] = useState(false);
  const [flashCardId, setFlashCardId] = useState<string | null>(null);
  const [projectsLoading, setProjectsLoading] = useState(true);
  const [projectsError, setProjectsError] = useState<string | null>(null);
  const toastState = useToastState();
  const { agentId, promptForAgentId } = useAgentId();

  const loadProjects = useCallback(() => {
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

  useEffect(() => {
    loadProjects();
  }, [loadProjects]);

  const { config, cards, loading, error, connected, updateCardLocally } = useBoard(selectedProject);

  // Refresh project list when SSE project events arrive
  useEffect(() => {
    if (!connected) return;
    const es = new EventSource('/api/events');
    const handler = (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data);
        if (data.type?.startsWith('project.')) {
          loadProjects();
        }
      } catch { /* ignore parse errors */ }
    };
    es.addEventListener('message', handler);
    return () => { es.close(); };
  }, [connected, loadProjects]);

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

  const handleProjectCreated = useCallback(
    (config: ProjectConfig) => {
      setProjects((prev) => [...prev, config]);
      setSelectedProject(config.name);
      setNewProjectOpen(false);
      setView('board');
      toastState.showToast(`Project "${config.name}" created`, 'success');
    },
    [toastState]
  );

  const handleProjectUpdated = useCallback(
    (updated: ProjectConfig) => {
      setProjects((prev) => prev.map((p) => (p.name === updated.name ? updated : p)));
    },
    []
  );

  const handleProjectDeleted = useCallback(() => {
    setProjects((prev) => prev.filter((p) => p.name !== selectedProject));
    setView('board');
    // Switch to first remaining project
    setProjects((prev) => {
      if (prev.length > 0) setSelectedProject(prev[0].name);
      else setSelectedProject('');
      return prev;
    });
  }, [selectedProject]);

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
        { key: 's', handler: () => { if (!panelOpen) setView('settings'); } },
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
          onNewProject={() => setNewProjectOpen(true)}
        />

        <ErrorBoundary>
          <main className="flex-1 overflow-hidden">
            {projectsError && (
              <div className="p-4 rounded m-4" style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}>
                {projectsError}
              </div>
            )}
            {selectedProject && view === 'settings' ? (
              <ProjectSettings
                project={selectedProject}
                onUpdated={handleProjectUpdated}
                onDeleted={handleProjectDeleted}
                showToast={toastState.showToast}
              />
            ) : selectedProject && view === 'dashboard' ? (
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
          {newProjectOpen && (
            <NewProjectWizard
              onClose={() => setNewProjectOpen(false)}
              onCreated={handleProjectCreated}
            />
          )}
        </ErrorBoundary>

        <ToastContainer />
      </div>
    </ToastContext.Provider>
  );
}

export default App;
