import { useState } from 'react';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';

interface DangerSectionProps {
  project: string;
  cardCount: number;
  isDeleting: boolean;
  onDelete: () => Promise<void>;
}

/** Danger tab - the card panel's `.bf-danger-card` idiom around the one
 *  destructive project action. Delete stays blocked while cards exist. */
export function DangerSection({ project, cardCount, isDeleting, onDelete }: DangerSectionProps) {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const blocked = cardCount > 0;

  return (
    <>
      <section className="ps-section">
        <div className="bf-danger-intro">
          <h3 className="section-eyebrow" style={{ color: 'var(--red)' }}>Danger zone</h3>
          <div
            className="font-mono"
            style={{ color: 'var(--grey1)', fontSize: '11.5px', lineHeight: 1.55, marginTop: '4px' }}
          >
            Destructive actions live here. Each one prompts for confirmation.
          </div>
        </div>

        <div className="bf-danger-card">
          <div className="bf-danger-row">
            <div>
              <div className="bf-danger-title">Delete project</div>
              <div className="bf-danger-desc">
                Removes {project} and its board configuration from the boards repository. This cannot be
                undone from the UI; git keeps the deletion commit.
              </div>
              {blocked && (
                <div className="bf-danger-reason">
                  This project has {cardCount} {cardCount === 1 ? 'card' : 'cards'}. Delete every card first.
                </div>
              )}
            </div>
            <button
              type="button"
              className="bf-btn-danger"
              disabled={blocked || isDeleting}
              onClick={() => setConfirmOpen(true)}
            >
              {isDeleting ? 'Deleting…' : 'Delete project'}
            </button>
          </div>
        </div>
      </section>

      <ConfirmModal
        open={confirmOpen}
        title={`Delete project ${project}?`}
        message="This removes the project and its board configuration from the boards repository. It cannot be undone from the UI."
        confirmLabel="Delete"
        variant="danger"
        onConfirm={() => {
          setConfirmOpen(false);
          void onDelete();
        }}
        onCancel={() => setConfirmOpen(false)}
      />
    </>
  );
}
