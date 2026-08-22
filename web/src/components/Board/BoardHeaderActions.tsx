import { useState } from 'react';
import { createPortal } from 'react-dom';
import { Link } from 'react-router';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';

interface BoardHeaderActionsProps {
  settingsHref: string;
  taskBackendConfigured?: boolean;
  consoleOpen?: boolean;
  onToggleConsole?: () => void;
  hasActiveWorkers?: boolean;
  onStopAll?: () => void;
}

/**
 * Secondary action cluster rendered left of New Card in both board bands:
 * Stop All (only while workers run), the worker-console toggle (only with a
 * task backend), and a Settings link. Labels go visually hidden (still in the
 * accessibility tree) inside the micro-band and on narrow viewports; titles
 * carry the keyboard hints. The Stop All confirm is portaled to <body>: the
 * micro-band is a sticky stacking context on phones, and a fixed overlay
 * rendered inside it would sit beneath the board footer and rail.
 */
export function BoardHeaderActions({
  settingsHref,
  taskBackendConfigured,
  consoleOpen,
  onToggleConsole,
  hasActiveWorkers,
  onStopAll,
}: BoardHeaderActionsProps) {
  const [confirmStopAllOpen, setConfirmStopAllOpen] = useState(false);

  return (
    <>
      <div className="board-header-actions">
        {hasActiveWorkers && onStopAll && (
          <button
            type="button"
            onClick={() => setConfirmStopAllOpen(true)}
            className="board-header-actions__btn board-header-actions__btn--danger"
            title="Stop all running tasks"
          >
            Stop All
          </button>
        )}
        {taskBackendConfigured && onToggleConsole && (
          <button
            type="button"
            onClick={onToggleConsole}
            aria-pressed={consoleOpen ?? false}
            className="board-header-actions__btn"
            title="Toggle worker console (c)"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <polyline points="4 17 10 11 4 5" />
              <line x1="12" y1="19" x2="20" y2="19" />
            </svg>
            <span className="board-header-actions__label">Console</span>
          </button>
        )}
        <Link
          to={settingsHref}
          className="board-header-actions__btn"
          title="Project settings (s)"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
          </svg>
          <span className="board-header-actions__label">Settings</span>
        </Link>
        <span className="board-header-actions__sep" aria-hidden="true" />
      </div>

      {createPortal(
        <ConfirmModal
          open={confirmStopAllOpen}
          title="Stop all running tasks?"
          message="All active worker containers will be destroyed and uncommitted work discarded."
          confirmLabel="Stop all"
          variant="danger"
          onConfirm={() => {
            setConfirmStopAllOpen(false);
            onStopAll?.();
          }}
          onCancel={() => setConfirmStopAllOpen(false)}
        />,
        document.body,
      )}
    </>
  );
}
