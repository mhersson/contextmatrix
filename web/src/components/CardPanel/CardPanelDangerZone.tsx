import { useState } from 'react';
import type { Card } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';

interface DangerZoneTabProps {
  card: Card;
  canDelete: boolean;
  deleteTooltip: string;
  isDeleting: boolean;
  onDelete: () => Promise<void>;
}

/**
 * Danger Zone rail tab — mirrors the design mock's `.bf-danger-wrap`
 * (`/tmp/card-panel-explorer.html:2232-2257`). Red-tinted intro followed by
 * a list of action cards. Each card uses the `.bf-danger-card` shell:
 * title (Fraunces 15px) → description (grey2 12.5px) → reason text (mono
 * 11px yellow, conditional) on the left, action button on the right.
 *
 * Currently lists:
 *   1. Delete card — enabled only when state ∈ {todo, not_planned} AND no
 *      runner attached.
 *   2. Force-release agent claim — placeholder, always disabled until the
 *      backend exposes the operation.
 */
export function DangerZoneTab({ card, canDelete, deleteTooltip, isDeleting, onDelete }: DangerZoneTabProps) {
  const [confirmDeleteOpen, setConfirmDeleteOpen] = useState(false);
  const handleClick = () => {
    if (!canDelete) return;
    setConfirmDeleteOpen(true);
  };
  const handleDeleteConfirm = () => {
    setConfirmDeleteOpen(false);
    void onDelete();
  };

  // Reason text mirrors the mock's logic — explain why the action is
  // disabled so the user knows what to fix.
  const deleteReason = canDelete
    ? null
    : card.assigned_agent
      ? 'An agent has an active claim on this card. Release it first.'
      : `Only cards in todo or not_planned can be deleted — current state is ${card.state.replace(/_/g, ' ')}. Move it there first (manually or via the state machine).`;

  return (
    <>
    <div className="bf-danger-wrap">
      <div className="bf-danger-intro">
        <div className="section-eyebrow" style={{ color: 'var(--red)' }}>Danger zone</div>
        <div
          className="font-mono"
          style={{ color: 'var(--grey1)', fontSize: '11.5px', lineHeight: 1.55, marginTop: '4px' }}
        >
          Destructive and irreversible actions live here. Every item prompts for confirmation.
        </div>
      </div>

      <div className="bf-danger-card">
        <div className="bf-danger-row">
          <div>
            <div className="bf-danger-title">Delete card</div>
            <div className="bf-danger-desc">
              Permanently remove this card from the board. The markdown file is deleted and a deletion commit is recorded in the boards repo. Activity log is lost. This cannot be undone from the UI.
            </div>
            {deleteReason && <div className="bf-danger-reason">🔒 {deleteReason}</div>}
          </div>
          <button
            type="button"
            onClick={handleClick}
            disabled={!canDelete || isDeleting}
            title={deleteTooltip}
            aria-label="Delete card"
            className="bf-btn-danger"
          >
            {isDeleting ? 'Deleting…' : canDelete ? 'Delete permanently' : 'Cannot delete'}
          </button>
        </div>
      </div>

      <div className="bf-danger-card" style={{ opacity: 0.6 }}>
        <div className="bf-danger-row">
          <div>
            <div className="bf-danger-title">Force-release agent claim</div>
            <div className="bf-danger-desc">
              Clear the assigned agent without notifying them. Use only if the runner is wedged and won&apos;t respond to Stop.
            </div>
            <div className="bf-danger-reason">Only available when the runner is unresponsive.</div>
          </div>
          <button
            type="button"
            disabled
            className="bf-btn-danger bf-btn-sm"
          >
            Force release
          </button>
        </div>
      </div>
    </div>

    <ConfirmModal
      open={confirmDeleteOpen}
      title={`Delete card ${card.id}?`}
      message="This permanently removes the card file and commits the deletion to git. This cannot be undone."
      confirmLabel="Delete"
      variant="danger"
      onConfirm={handleDeleteConfirm}
      onCancel={() => setConfirmDeleteOpen(false)}
    />
    </>
  );
}
