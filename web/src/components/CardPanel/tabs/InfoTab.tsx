import type { Dispatch, SetStateAction } from 'react';
import type { Card, ProjectConfig } from '../../../types';
import { CardPanelMetadata } from '../CardPanelMetadata';

interface InfoTabProps {
  card: Card;
  editedCard: Card;
  setEditedCard: Dispatch<SetStateAction<Card>>;
  config: ProjectConfig;
  currentAgentId: string | null;
  runnerAttached: boolean;
  onSubtaskClick: (cardId: string) => void;
  onClaim: () => Promise<void>;
  onRelease: () => Promise<void>;
  excludeStateFromPicker: string | null;
}

/**
 * Info rail tab — wraps CardPanelMetadata (which is itself a composition
 * of four peer files under `./metadata/`). Keeping this as a thin
 * adapter isolates the two state-change closures that need to be
 * converted to `setEditedCard` calls.
 */
export function InfoTab({
  card,
  editedCard,
  setEditedCard,
  config,
  currentAgentId,
  runnerAttached,
  onSubtaskClick,
  onClaim,
  onRelease,
  excludeStateFromPicker,
}: InfoTabProps) {
  return (
    <CardPanelMetadata
      card={card}
      editedCard={editedCard}
      config={config}
      currentAgentId={currentAgentId}
      runnerAttached={runnerAttached}
      onStateChange={(state) => setEditedCard((prev) => ({ ...prev, state }))}
      onSubtaskClick={onSubtaskClick}
      onClaim={onClaim}
      onRelease={onRelease}
      editedVetted={editedCard.vetted ?? false}
      onVettedChange={(v) => setEditedCard((prev) => ({ ...prev, vetted: v }))}
      onSkillsChange={(skills) => setEditedCard((prev) => ({ ...prev, skills }))}
      excludeStateFromPicker={excludeStateFromPicker}
    />
  );
}
