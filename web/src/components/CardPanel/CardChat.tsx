import { useEffect, useRef, useState } from 'react';
import type { Card, LogEntry } from '../../types';
import { api, isAPIError } from '../../api/client';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';
import { ChatPanel } from '../ChatPanel';

interface CardChatProps {
  card: Card;
  cardLogs: readonly LogEntry[];
}

/**
 * Card-bound chat wrapper. Composes the generic ChatPanel primitive with
 * card-specific bits: the promote-to-autonomous confirm modal, the
 * read-only footer text logic (different message for promoted vs ended),
 * and the api.sendCardMessage / api.promoteCardToAutonomous calls.
 *
 * The transcript and filter bar remain visible even when the session is
 * not active (stopped or promoted), with the compose row replaced by a
 * read-only footer.
 */
export function CardChat({ card, cardLogs }: CardChatProps) {
  const [promoting, setPromoting] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [promoteError, setPromoteError] = useState<string | null>(null);

  // Guard against setState after unmount: when HITL→Auto promotion completes,
  // card.autonomous flips to true and this component unmounts while the await
  // is still in flight. aliveRef lets the async handler skip the trailing
  // setState calls after cleanup runs.
  const aliveRef = useRef(true);
  useEffect(() => {
    aliveRef.current = true;
    return () => { aliveRef.current = false; };
  }, []);

  const hitlActive = card.worker_status === 'running' && !card.autonomous;

  const handleSend = async (content: string) => {
    try {
      await api.sendCardMessage(card.project, card.id, content);
    } catch (err) {
      // Rethrow as Error so ChatPanel's internal error display shows the
      // API error message. Preserve the original via `cause` so devtools /
      // future telemetry can still see the underlying APIError.
      const msg = isAPIError(err) ? err.error : 'Failed to send message';
      throw new Error(msg, { cause: err });
    }
  };

  const handlePromoteConfirm = async () => {
    setConfirmOpen(false);
    setPromoting(true);
    setPromoteError(null);
    try {
      await api.promoteCardToAutonomous(card.project, card.id);
      // Successful promotion flips card.autonomous → true via SSE, which causes
      // the parent to unmount this component. Skip setState if that already
      // happened by the time we resume here.
      if (aliveRef.current) setPromoting(false);
    } catch (err) {
      if (!aliveRef.current) return;
      setPromoteError(isAPIError(err) ? err.error : 'Failed to promote session');
      setPromoting(false);
    }
  };

  // Derive readOnlyMessage for non-HITL states. When hitlActive is true this
  // is undefined and the compose row is shown. Every autonomous run — started
  // autonomous (plain or mob session) or promoted mid-session — streams the same
  // read-only transcript, so one caption covers them all while running.
  const readOnlyMessage = hitlActive
    ? undefined
    : card.worker_status === 'running'
      ? 'Autonomous run — read-only'
      : 'Session ended — read-only';

  // Footer renders only when HITL is active: the "Switch to Autonomous"
  // button. Once promoted, card.autonomous flips to true so hitlActive
  // becomes false and the compose row is replaced by readOnlyMessage.
  const footer = hitlActive ? (
    <>
      <button
        type="button"
        onClick={() => setConfirmOpen(true)}
        disabled={promoting}
        className="bf-btn-ghost bf-btn-sm"
        style={{ color: 'var(--orange)', borderColor: 'color-mix(in oklab, var(--orange) 35%, transparent)' }}
      >
        {promoting ? 'Promoting…' : '⇢ Switch to Autonomous'}
      </button>
      {promoteError && (
        <div
          className="text-xs px-2 py-1 rounded font-mono"
          style={{ background: 'var(--bg-red)', color: 'var(--red)' }}
          role="alert"
        >
          {promoteError}
        </div>
      )}
    </>
  ) : undefined;

  return (
    <>
      <ChatPanel
        logs={cardLogs}
        onSend={handleSend}
        sendDisabled={!hitlActive}
        footer={footer}
        readOnlyMessage={readOnlyMessage}
      />
      <ConfirmModal
        open={confirmOpen}
        title="Promote to autonomous?"
        message="The agent will finish the workflow without further input, create a feature branch, and open a PR."
        confirmLabel="Promote"
        cancelLabel="Cancel"
        onConfirm={() => void handlePromoteConfirm()}
        onCancel={() => setConfirmOpen(false)}
      />
    </>
  );
}
