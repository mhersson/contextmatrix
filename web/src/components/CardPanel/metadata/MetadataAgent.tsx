import { useState } from 'react';
import type { Card } from '../../../types';
import { formatRelativeTime } from '../utils';
import { ConfirmModal } from '../../ConfirmModal/ConfirmModal';

interface MetadataAgentProps {
  card: Card;
  currentAgentId: string | null;
  runnerAttached: boolean;
  onClaim: () => void;
  onRelease: () => void;
}

/**
 * Agent section of the Info rail tab. Renders one of three branches:
 *   - Runner attached with agent: show agent ID + heartbeat + optional
 *     Release button (human-only; emits ConfirmModal).
 *   - Unassigned but previously claimed: "released · no active claim" +
 *     last-claimer hint.
 *   - Fresh unassigned todo: "unassigned · runner ready ✓" + Just claim.
 */
export function MetadataAgent({
  card,
  currentAgentId,
  runnerAttached,
  onClaim,
  onRelease,
}: MetadataAgentProps) {
  const [confirmReleaseOpen, setConfirmReleaseOpen] = useState(false);
  const assignedAgent = card.assigned_agent;
  const canClaim = !assignedAgent && !runnerAttached;
  const canRelease =
    !!assignedAgent && !!currentAgentId && currentAgentId.startsWith('human:');

  const handleReleaseConfirm = () => {
    setConfirmReleaseOpen(false);
    onRelease();
  };

  return (
    <>
      <section className="bf-aside-section">
        <h4>Agent</h4>
        {runnerAttached && assignedAgent ? (
          <div className="bf-spread">
            <div className="flex items-center gap-2 min-w-0">
              <span
                className="font-mono"
                style={{ color: 'var(--grey1)', fontSize: '12px', letterSpacing: '0.04em' }}
              >
                {assignedAgent}
              </span>
              {card.last_heartbeat && (
                <span className="font-mono" style={{ color: 'var(--grey0)', fontSize: '11px' }}>
                  · heartbeat {formatRelativeTime(card.last_heartbeat)}
                </span>
              )}
            </div>
            {canRelease && (
              <button
                type="button"
                onClick={() => setConfirmReleaseOpen(true)}
                className="bf-btn-ghost bf-btn-sm"
              >
                Release
              </button>
            )}
          </div>
        ) : (
          <div className="flex flex-col gap-2.5">
            <div className="bf-spread">
              {assignedAgent || card.last_heartbeat ? (
                <>
                  <span className="font-mono" style={{ color: 'var(--grey1)', fontSize: '11.5px' }}>
                    released · no active claim
                  </span>
                  {assignedAgent && (
                    <span className="font-mono" style={{ color: 'var(--grey1)', fontSize: '11.5px' }}>
                      last: {assignedAgent}
                    </span>
                  )}
                </>
              ) : card.state === 'todo' ? (
                <>
                  <span className="font-mono" style={{ color: 'var(--grey1)', fontSize: '11.5px' }}>
                    unassigned
                  </span>
                  <span className="font-mono" style={{ color: 'var(--aqua)', fontSize: '11.5px' }}>
                    runner ready ✓
                  </span>
                </>
              ) : (
                <span className="font-mono" style={{ color: 'var(--grey1)', fontSize: '11.5px' }}>
                  unassigned
                </span>
              )}
            </div>
            {canClaim && (
              <button
                type="button"
                onClick={onClaim}
                className="bf-btn-ghost"
                style={{ width: '100%', justifyContent: 'center' }}
              >
                Just claim
              </button>
            )}
          </div>
        )}
      </section>

      <ConfirmModal
        open={confirmReleaseOpen}
        title={`Release claim held by ${assignedAgent}?`}
        message="Only do this if the agent is no longer running — the current claimant will lose its ability to update the card."
        confirmLabel="Release"
        variant="danger"
        onConfirm={handleReleaseConfirm}
        onCancel={() => setConfirmReleaseOpen(false)}
      />
    </>
  );
}
