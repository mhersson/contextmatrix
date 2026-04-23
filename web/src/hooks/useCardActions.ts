import { useCallback } from 'react';
import { api, isAPIError } from '../api/client';
import type { Card, PatchCardInput, CreateCardInput } from '../types';

interface UseCardActionsParams {
  selectedProject: string;
  selectedCard: Card | null;
  cards: Card[];
  updateCardLocally: (cardId: string, updates: Partial<Card>) => void;
  removeCardLocally: (cardId: string) => void;
  suppressSSE: (cardId: string) => void;
  unsuppressSSE: (cardId: string) => void;
  showToast: (message: string, type: 'success' | 'error') => void;
  onCardDeleted: () => void;
}

export function useCardActions({
  selectedProject,
  selectedCard,
  cards,
  updateCardLocally,
  removeCardLocally,
  suppressSSE,
  unsuppressSSE,
  showToast,
  onCardDeleted,
}: UseCardActionsParams) {
  const handleCardMove = useCallback(
    async (cardId: string, newState: string) => {
      const card = cards.find((c) => c.id === cardId);
      if (!card) return;
      const oldState = card.state;
      suppressSSE(cardId);
      updateCardLocally(cardId, { state: newState });
      try {
        await api.patchCard(selectedProject, cardId, { state: newState });
        showToast(`Moved to ${newState}`, 'success');
      } catch (err) {
        updateCardLocally(cardId, { state: oldState });
        showToast(isAPIError(err) ? err.error : 'Failed to move card', 'error');
      } finally {
        unsuppressSSE(cardId);
      }
    },
    [cards, selectedProject, updateCardLocally, suppressSSE, unsuppressSSE, showToast]
  );

  const handleCardSave = useCallback(
    async (updates: PatchCardInput) => {
      if (!selectedCard) return;
      try {
        await api.patchCard(selectedProject, selectedCard.id, updates);
        showToast('Card saved', 'success');
      } catch (err) {
        showToast(isAPIError(err) ? err.error : 'Failed to save card', 'error');
        throw err;
      }
    },
    [selectedCard, selectedProject, showToast]
  );

  const handleClaim = useCallback(
    async (claimAgentId: string) => {
      if (!selectedCard) return;
      try {
        await api.claimCard(selectedProject, selectedCard.id, claimAgentId);
        showToast('Card claimed', 'success');
      } catch (err) {
        showToast(isAPIError(err) ? err.error : 'Failed to claim card', 'error');
      }
    },
    [selectedCard, selectedProject, showToast]
  );

  const handleRelease = useCallback(
    async (releaseAgentId: string) => {
      if (!selectedCard) return;
      try {
        await api.releaseCard(selectedProject, selectedCard.id, releaseAgentId);
        showToast('Card released', 'success');
      } catch (err) {
        showToast(isAPIError(err) ? err.error : 'Failed to release card', 'error');
      }
    },
    [selectedCard, selectedProject, showToast]
  );

  const handleCreateCard = useCallback(
    async (input: CreateCardInput) => {
      try {
        const card = await api.createCard(selectedProject, input);
        showToast(`Created ${card.id}`, 'success');
        return card;
      } catch (err) {
        showToast(`Failed to create card: ${err instanceof Error ? err.message : 'Unknown error'}`, 'error');
        throw err;
      }
    },
    [selectedProject, showToast]
  );

  const handleRunCard = useCallback(async (interactive: boolean) => {
    if (!selectedCard) return;
    try {
      const updated = await api.runCard(selectedProject, selectedCard.id, { interactive });
      updateCardLocally(selectedCard.id, {
        runner_status: updated.runner_status,
        assigned_agent: updated.assigned_agent,
      });
      showToast('Task queued for runner', 'success');
    } catch (err) {
      showToast(isAPIError(err) ? err.error : 'Failed to trigger runner', 'error');
    }
  }, [selectedCard, selectedProject, updateCardLocally, showToast]);

  const handleStopCard = useCallback(async () => {
    if (!selectedCard) return;
    try {
      const updated = await api.stopCard(selectedProject, selectedCard.id);
      updateCardLocally(selectedCard.id, {
        runner_status: updated.runner_status,
        assigned_agent: updated.assigned_agent,
      });
      showToast('Runner task stopped', 'success');
    } catch (err) {
      showToast(isAPIError(err) ? err.error : 'Failed to stop runner', 'error');
    }
  }, [selectedCard, selectedProject, updateCardLocally, showToast]);

  const handleStopAll = useCallback(async () => {
    try {
      const result = await api.stopAllCards(selectedProject);
      for (const cardId of result.affected_cards) {
        updateCardLocally(cardId, { runner_status: 'killed', assigned_agent: undefined });
      }
      showToast('All runner tasks stopped', 'success');
    } catch (err) {
      showToast(isAPIError(err) ? err.error : 'Failed to stop all', 'error');
    }
  }, [selectedProject, updateCardLocally, showToast]);

  const handleCardDelete = useCallback(async (cardId: string) => {
    try {
      await api.deleteCard(selectedProject, cardId);
      removeCardLocally(cardId);
      onCardDeleted();
    } catch (err) {
      showToast(isAPIError(err) ? err.error : 'Failed to delete card', 'error');
      throw err;
    }
  }, [selectedProject, removeCardLocally, showToast, onCardDeleted]);

  return {
    handleCardMove, handleCardSave, handleClaim, handleRelease, handleCreateCard,
    handleRunCard, handleStopCard, handleStopAll, handleCardDelete,
  };
}
