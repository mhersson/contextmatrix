import type { Dispatch, SetStateAction } from 'react';
import type { Card, ProjectConfig } from '../../../types';
import { CardPanelMetadata } from '../CardPanelMetadata';

interface InfoTabProps {
  card: Card;
  editedCard: Card;
  setEditedCard: Dispatch<SetStateAction<Card>>;
  config: ProjectConfig;
  currentAgentId: string | null;
  workerAttached: boolean;
  cards: Card[];
  onSubtaskClick: (cardId: string) => void;
  onClaim: () => Promise<void>;
  onRelease: () => Promise<void>;
  excludeStateFromPicker: string | null;
  onDependsOnChange: (ids: string[]) => Promise<void>;
}

/**
 * Info rail tab - wraps CardPanelMetadata (which is itself a composition
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
  workerAttached,
  cards,
  onSubtaskClick,
  onClaim,
  onRelease,
  excludeStateFromPicker,
  onDependsOnChange,
}: InfoTabProps) {
  return (
    <CardPanelMetadata
      card={card}
      editedCard={editedCard}
      config={config}
      currentAgentId={currentAgentId}
      workerAttached={workerAttached}
      cards={cards}
      onStateChange={(state) => setEditedCard((prev) => ({ ...prev, state }))}
      onSubtaskClick={onSubtaskClick}
      onClaim={onClaim}
      onRelease={onRelease}
      editedVetted={editedCard.vetted ?? false}
      onVettedChange={(v) => setEditedCard((prev) => ({ ...prev, vetted: v }))}
      onSkillsChange={(skills) => setEditedCard((prev) => ({ ...prev, skills }))}
      excludeStateFromPicker={excludeStateFromPicker}
      assignee={editedCard.assignee}
      onAssigneeChange={(v) => setEditedCard((prev) => ({ ...prev, assignee: v || undefined }))}
      onDependsOnChange={onDependsOnChange}
    />
  );
}
