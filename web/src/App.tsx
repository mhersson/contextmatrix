import { useState, useEffect, useCallback, useMemo } from 'react';
import { api } from './api/client';
import { useBoard } from './hooks/useBoard';
import { useAgentId } from './hooks/useAgentId';
import { useKeyboardShortcuts } from './hooks/useKeyboardShortcuts';
import { ToastContext, useToastState } from './hooks/useToast';
import { Board } from './components/Board';
import { CardPanel } from './components/CardPanel';
import { CreateCardPanel } from './components/CreateCardPanel';
import { ToastContainer } from './components/Toast';
import type { Card, ProjectConfig, PatchCardInput, CreateCardInput } from './types';

function App() {
  const [projects, setProjects] = useState<ProjectConfig[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>('');
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
        if (p.length > 0 && !selectedProject) {
          setSelectedProject(p[0].name);
        }
      })
      .catch((err) => {
        setProjectsError(err?.error || 'Failed to load projects');
      })
      .finally(() => {
        setProjectsLoading(false);
      });
  }, [selectedProject]);

  const { config, cards, loading, error, connected, updateCardLocally } = useBoard(selectedProject);

  const handleCardMove = useCallback(
    async (cardId: string, newState: string) => {
      const card = cards.find((c) => c.id === cardId);
      if (!card) return;

      const oldState = card.state;

      // Optimistic update
      updateCardLocally(cardId, { state: newState });

      try {
        await api.patchCard(selectedProject, cardId, { state: newState });
        toastState.showToast(`Moved to ${newState}`, 'success');
      } catch (err) {
        // Rollback on error
        updateCardLocally(cardId, { state: oldState });
        const message =
          err && typeof err === 'object' && 'error' in err
            ? (err as { error: string }).error
            : 'Failed to move card';
        toastState.showToast(message, 'error');
      }
    },
    [cards, selectedProject, updateCardLocally, toastState]
  );

  const handleCardClick = useCallback((card: Card) => {
    setSelectedCard(card);
    setCreatePanelOpen(false);
  }, []);

  const handleOpenCreate = useCallback(() => {
    setCreatePanelOpen(true);
    setSelectedCard(null);
  }, []);

  const handleCreateCard = useCallback(
    async (input: CreateCardInput) => {
      const card = await api.createCard(selectedProject, input);
      toastState.showToast(`Created ${card.id}`, 'success');
      setCreatePanelOpen(false);
      setFlashCardId(card.id);
      setTimeout(() => setFlashCardId(null), 2500);
    },
    [selectedProject, toastState]
  );

  const handlePanelClose = useCallback(() => {
    setSelectedCard(null);
  }, []);

  const handleCardSave = useCallback(
    async (updates: PatchCardInput) => {
      if (!selectedCard) return;

      try {
        await api.patchCard(selectedProject, selectedCard.id, updates);
        toastState.showToast('Card saved', 'success');
        // Card will be updated via SSE
      } catch (err) {
        const message =
          err && typeof err === 'object' && 'error' in err
            ? (err as { error: string }).error
            : 'Failed to save card';
        toastState.showToast(message, 'error');
        throw err;
      }
    },
    [selectedCard, selectedProject, toastState]
  );

  const handleClaim = useCallback(
    async (claimAgentId: string) => {
      if (!selectedCard) return;

      try {
        await api.claimCard(selectedProject, selectedCard.id, claimAgentId);
        toastState.showToast('Card claimed', 'success');
      } catch (err) {
        const message =
          err && typeof err === 'object' && 'error' in err
            ? (err as { error: string }).error
            : 'Failed to claim card';
        toastState.showToast(message, 'error');
      }
    },
    [selectedCard, selectedProject, toastState]
  );

  const handleRelease = useCallback(
    async (releaseAgentId: string) => {
      if (!selectedCard) return;

      try {
        await api.releaseCard(selectedProject, selectedCard.id, releaseAgentId);
        toastState.showToast('Card released', 'success');
      } catch (err) {
        const message =
          err && typeof err === 'object' && 'error' in err
            ? (err as { error: string }).error
            : 'Failed to release card';
        toastState.showToast(message, 'error');
      }
    },
    [selectedCard, selectedProject, toastState]
  );

  const handleSubtaskClick = useCallback(
    (cardId: string) => {
      const card = cards.find((c) => c.id === cardId);
      if (card) {
        setSelectedCard(card);
      }
    },
    [cards]
  );

  // Compute the currently selected card from the cards list
  // This ensures the panel always shows the latest data from SSE updates
  const currentSelectedCard = selectedCard
    ? cards.find((c) => c.id === selectedCard.id) || selectedCard
    : null;

  const panelOpen = !!currentSelectedCard || createPanelOpen;

  useKeyboardShortcuts(
    useMemo(
      () => [
        {
          key: 'n',
          handler: () => {
            if (!panelOpen && config) handleOpenCreate();
          },
        },
        ...projects.map((_, i) => ({
          key: String(i + 1),
          handler: () => {
            if (i < projects.length) setSelectedProject(projects[i].name);
          },
        })),
      ],
      [panelOpen, config, projects, handleOpenCreate]
    )
  );

  return (
    <ToastContext.Provider value={toastState}>
      <div className="min-h-screen flex flex-col" style={{ backgroundColor: 'var(--bg-dim)' }}>
      <header
        className="flex items-center justify-between px-6 py-4 border-b"
        style={{ backgroundColor: 'var(--bg0)', borderColor: 'var(--bg3)' }}
      >
        <h1
          className="text-xl font-semibold"
          style={{ color: 'var(--fg)', fontFamily: 'var(--font-mono)' }}
        >
          ContextMatrix
        </h1>

        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <span
              className={`w-2 h-2 rounded-full ${connected ? 'animate-pulse' : ''}`}
              style={{ backgroundColor: connected ? 'var(--green)' : 'var(--red)' }}
            />
            <span className="text-sm" style={{ color: 'var(--grey1)' }}>
              {connected ? 'Connected' : 'Disconnected'}
            </span>
          </div>

          <select
            value={selectedProject}
            onChange={(e) => setSelectedProject(e.target.value)}
            disabled={projectsLoading || projects.length === 0}
            className="px-3 py-1.5 rounded text-sm border"
            style={{
              backgroundColor: 'var(--bg1)',
              borderColor: 'var(--bg3)',
              color: 'var(--fg)',
            }}
          >
            {projectsLoading && <option>Loading...</option>}
            {!projectsLoading && projects.length === 0 && (
              <option>No projects</option>
            )}
            {projects.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
              </option>
            ))}
          </select>
        </div>
      </header>

      <main className="flex-1 overflow-hidden">
        {projectsError && (
          <div
            className="p-4 rounded m-4"
            style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}
          >
            {projectsError}
          </div>
        )}

        {selectedProject && config ? (
          <Board
            cards={cards}
            config={config}
            loading={loading}
            error={error}
            onCardClick={handleCardClick}
            onCardMove={handleCardMove}
            onCreateCard={handleOpenCreate}
            flashCardId={flashCardId}
          />
        ) : (
          <div className="flex items-center justify-center h-full">
            <div style={{ color: 'var(--grey1)' }}>
              {projectsLoading ? 'Loading projects...' : 'Select a project to view the board'}
            </div>
          </div>
        )}
      </main>

      {currentSelectedCard && config && (
        <CardPanel
          card={currentSelectedCard}
          config={config}
          onClose={handlePanelClose}
          onSave={handleCardSave}
          onClaim={handleClaim}
          onRelease={handleRelease}
          onSubtaskClick={handleSubtaskClick}
          currentAgentId={agentId}
          onPromptAgentId={promptForAgentId}
        />
      )}

      {createPanelOpen && config && (
        <CreateCardPanel
          config={config}
          cards={cards}
          onClose={() => setCreatePanelOpen(false)}
          onCreate={handleCreateCard}
        />
      )}

      <ToastContainer />
      </div>
    </ToastContext.Provider>
  );
}

export default App;
