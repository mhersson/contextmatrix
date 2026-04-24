import { useState, type Dispatch, type SetStateAction } from 'react';
import type { Card } from '../../types';
import { CardPanelEditor } from './CardPanelEditor';
import { LabelsSection } from './CardPanelLabels';

interface CardPanelLeftProps {
  editedCard: Card;
  setEditedCard: Dispatch<SetStateAction<Card>>;
  runnerAttached: boolean;
  editingLocked: boolean;
  lockedReason: string;
  canToggleEditor: boolean;
}

/**
 * Left column of the bifold card panel: labels section + description
 * surface. Both live here because they share the same lock predicates
 * (`editingLocked` / `runnerAttached`) and neither is reused elsewhere
 * in the panel tree.
 *
 * The description is preview-only by default. When `canToggleEditor` is
 * true (todo / done / not_planned without a runner attached) the user can
 * flip into edit mode via the button rendered inside `CardPanelEditor`.
 * Editor state is local and resets on card identity change via a `key`
 * prop from `CardPanel`.
 */
export function CardPanelLeft({
  editedCard,
  setEditedCard,
  runnerAttached,
  editingLocked,
  lockedReason,
  canToggleEditor,
}: CardPanelLeftProps) {
  const [editing, setEditing] = useState(false);

  return (
    <>
      <LabelsSection
        editedLabels={editedCard.labels}
        disabled={editingLocked}
        lockedReason={lockedReason}
        onLabelsChange={(labels) => setEditedCard((prev) => ({ ...prev, labels }))}
      />
      <CardPanelEditor
        body={editedCard.body}
        editable={!runnerAttached}
        editing={editing}
        onToggleEditing={canToggleEditor ? () => setEditing((v) => !v) : undefined}
        onChange={(body) => setEditedCard((prev) => ({ ...prev, body }))}
      />
    </>
  );
}
