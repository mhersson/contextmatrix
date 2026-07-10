import { useState, useCallback } from 'react';
import type { Card, PatchCardInput } from '../../types';
import { buildCardPatch, isCardDirty } from './utils';

export interface CardEdits {
  editedCard: Card;
  setEditedCard: React.Dispatch<React.SetStateAction<Card>>;
  isDirty: boolean;
  isSaving: boolean;
  forcedFeatureBranch: boolean;
  forcedCreatePR: boolean;
  clearForcedFeatureBranch: () => void;
  clearForcedCreatePR: () => void;
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
  const [forcedFeatureBranch, setForcedFeatureBranch] = useState(false);
  const [forcedCreatePR, setForcedCreatePR] = useState(false);

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
   * Run handler: force-enables feature_branch + create_pr (matching
   * server behavior when running a card), saves if dirty,
   * then fires the worker webhook.
   *
   * On save failure: reverts only the two optimistically-forced fields via
   * a functional update, leaving any concurrent user edits intact.
   *
   * On worker webhook failure after a successful save: clears the
   * "forced on run" badges (they only make sense next to a live claim).
   */
  const handleRun = useCallback(async () => {
    const wasFeatureBranch = editedCard.feature_branch ?? false;
    const wasCreatePR = editedCard.create_pr ?? false;
    setForcedFeatureBranch(!wasFeatureBranch);
    setForcedCreatePR(!wasCreatePR);
    const next: Card = {
      ...editedCard,
      feature_branch: true,
      create_pr: true,
    };
    setEditedCard(next);
    const nextIsDirty = isCardDirty(next, card);
    if (nextIsDirty) {
      setIsSaving(true);
      try {
        await onSave(buildCardPatch(next, card));
      } catch {
        // Revert only the two fields we optimistically forced to `true`. Use
        // a functional update and check that `curr` still holds the optimistic
        // values before reverting — a concurrent user toggle between the
        // optimistic set and the catch would otherwise be silently overwritten.
        setEditedCard((curr) => ({
          ...curr,
          feature_branch: curr.feature_branch === true ? wasFeatureBranch : curr.feature_branch,
          create_pr: curr.create_pr === true ? wasCreatePR : curr.create_pr,
        }));
        setForcedFeatureBranch(false);
        setForcedCreatePR(false);
        return;
      } finally {
        setIsSaving(false);
      }
    }
    try {
      await onRunCard(!(next.autonomous ?? false));
    } catch {
      // Save succeeded but the worker webhook failed. The feature_branch /
      // create_pr values on the server are now real, so don't revert those;
      // clear the "forced on run" badges since they only make sense next
      // to a live worker claim.
      setForcedFeatureBranch(false);
      setForcedCreatePR(false);
    }
  }, [editedCard, card, onSave, onRunCard]);

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
    forcedFeatureBranch,
    forcedCreatePR,
    clearForcedFeatureBranch: () => setForcedFeatureBranch(false),
    clearForcedCreatePR: () => setForcedCreatePR(false),
    handleSave,
    handleRun,
    handleTransitionPrimary,
  };
}
