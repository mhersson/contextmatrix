import { useState, useCallback } from 'react';
import type { Card, PatchCardInput } from '../../types';
import { buildCardPatch, isCardDirty } from './utils';

export interface CardEdits {
  editedCard: Card;
  setEditedCard: React.Dispatch<React.SetStateAction<Card>>;
  isDirty: boolean;
  isSaving: boolean;
  handleSave: () => Promise<void>;
  handleRun: () => Promise<void>;
  handleTransitionPrimary: (targetState: string) => Promise<void>;
}

export function useCardEdits(
  card: Card,
  onSave: (updates: PatchCardInput) => Promise<void>,
  onRunCard: (interactive: boolean) => Promise<void>,
): CardEdits {
  const [editedCard, setEditedCard] = useState<Card>(card);
  const [isSaving, setIsSaving] = useState(false);

  const isDirty = isCardDirty(editedCard, card);

  const handleSave = useCallback(async () => {
    if (!isDirty || isSaving) return;
    setIsSaving(true);
    try {
      await onSave(buildCardPatch(editedCard, card));
    } finally {
      setIsSaving(false);
    }
  }, [isDirty, isSaving, editedCard, card, onSave]);

  /**
   * Run handler: saves pending edits first, then fires the worker webhook.
   * A save failure aborts the run so the worker never starts from state the
   * user believes is saved but is not.
   */
  const handleRun = useCallback(async () => {
    if (isDirty) {
      setIsSaving(true);
      try {
        await onSave(buildCardPatch(editedCard, card));
      } catch {
        return; // Save failed - do not fire the worker.
      } finally {
        setIsSaving(false);
      }
    }
    try {
      await onRunCard(!(editedCard.autonomous ?? false));
    } catch {
      // Parent surfaces the error toast; nothing optimistic to revert.
    }
  }, [isDirty, editedCard, card, onSave, onRunCard]);

  const handleTransitionPrimary = useCallback(async (targetState: string) => {
    const prevState = editedCard.state;
    const next: Card = { ...editedCard, state: targetState };
    setEditedCard(next);
    if (isCardDirty(next, card) && !isSaving) {
      setIsSaving(true);
      try {
        await onSave(buildCardPatch(next, card));
      } catch {
        // Revert the optimistic state change; keep any other concurrent edits.
        setEditedCard((curr) => ({ ...curr, state: prevState }));
      } finally {
        setIsSaving(false);
      }
    }
  }, [editedCard, card, onSave, isSaving]);

  return {
    editedCard,
    setEditedCard,
    isDirty,
    isSaving,
    handleSave,
    handleRun,
    handleTransitionPrimary,
  };
}
