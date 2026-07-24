import { useState } from 'react';
import type { Card } from '../../types';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';

interface DangerZoneTabProps {
  card: Card;
  canDelete: boolean;
  deleteTooltip: string;
  isDeleting: boolean;
  onDelete: () => Promise<void>;
  canForceRelease: boolean;
  isForceReleasing: boolean;
  onForceRelease: () => Promise<void>;
}

/**
 * Danger Zone rail tab - mirrors the design mock's `.bf-danger-wrap`
 * (`/tmp/card-panel-explorer.html:2232-2257`). Red-tinted intro followed by
 * a list of action cards. Each card uses the `.bf-danger-card` shell:
 * title (Fraunces 15px) → description (grey2 12.5px) → reason text (mono
 * 11px yellow, conditional) on the left, action button on the right.
 *
 * Currently lists:
 *   1. Delete card - enabled only when state ∈ {todo, not_planned} AND no
 *      worker attached.
 *   2. Force-release agent claim - enabled whenever an agent holds the card;
 *      clears the claim without notifying the agent (crashed-worker recovery).
 */
export function DangerZoneTab({
  card,
  canDelete,
  deleteTooltip,
  isDeleting,
  onDelete,
  canForceRelease,
  isForceReleasing,
  onForceRelease,
}: DangerZoneTabProps) {
  const [confirmDeleteOpen, setConfirmDeleteOpen] = useState(false);
  const [confirmForceReleaseOpen, setConfirmForceReleaseOpen] = useState(false);
  const handleClick = () => {
    if (!canDelete) return;
    setConfirmDeleteOpen(true);
  };
  const handleDeleteConfirm = () => {
    setConfirmDeleteOpen(false);
    void onDelete();
  };
  const handleForceReleaseClick = () => {
    if (!canForceRelease) return;
    setConfirmForceReleaseOpen(true);
  };
  const handleForceReleaseConfirm = () => {
    setConfirmForceReleaseOpen(false);
    void onForceRelease();
  };

  // Reason text mirrors the mock's logic - explain why the action is
  // disabled so the user knows what to fix.
  const deleteReason = canDelete
    ? null
    : card.assigned_agent
      ? 'An agent has an active claim on this card. Release it first.'
      : `Only cards in todo or not_planned can be deleted - current state is ${card.state.replace(/_/g, ' ')}.`;

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

      <div className="bf-danger-card">
        <div className="bf-danger-row">
          <div>
            <div className="bf-danger-title">Force-release agent claim</div>
            <div className="bf-danger-desc">
              Clear the assigned agent&apos;s claim without notifying it. The card&apos;s state and any running container are left untouched. Use when the worker has crashed or is wedged and won&apos;t respond to Stop.
            </div>
            {!canForceRelease && <div className="bf-danger-reason">🔒 No agent claim to release.</div>}
          </div>
          <button
            type="button"
            onClick={handleForceReleaseClick}
            disabled={!canForceRelease || isForceReleasing}
            aria-label="Force-release agent claim"
            className="bf-btn-danger bf-btn-sm"
          >
            {isForceReleasing ? 'Releasing…' : 'Force release'}
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
    <ConfirmModal
      open={confirmForceReleaseOpen}
      title={`Force-release claim on ${card.id}?`}
      message={`This clears the claim held by ${card.assigned_agent ?? 'the assigned agent'}. The agent is not notified - if it is still alive its next write will fail. Card state is unchanged and no container is stopped.`}
      confirmLabel="Force release"
      variant="danger"
      onConfirm={handleForceReleaseConfirm}
      onCancel={() => setConfirmForceReleaseOpen(false)}
    />
    </>
  );
}
