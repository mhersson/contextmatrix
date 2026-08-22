import { useState } from 'react';
import { ConfirmModal } from '../ConfirmModal/ConfirmModal';

export interface BoardHeaderActionsProps {
  onOpenSettings: () => void;
  taskBackendConfigured?: boolean;
  consoleOpen?: boolean;
  onToggleConsole?: () => void;
  hasActiveWorkers?: boolean;
  onStopAll?: () => void;
}

/**
 * Secondary action cluster rendered left of New Card in both board bands:
 * Stop All (only while workers run), the worker-console toggle (only with a
 * task backend), and project Settings. Labels hide via CSS inside the
 * micro-band and on narrow viewports; every button keeps a title for the
 * icon-only case.
 */
export function BoardHeaderActions({
  onOpenSettings,
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
            className="px-2 py-1 rounded text-xs font-medium hover:opacity-90 transition-opacity mr-1"
            style={{ backgroundColor: 'var(--bg-red)', color: 'var(--red)' }}
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
        <button
          type="button"
          onClick={onOpenSettings}
          className="board-header-actions__btn"
          title="Project settings (s)"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
          </svg>
          <span className="board-header-actions__label">Settings</span>
        </button>
        <span className="board-header-actions__sep" aria-hidden="true" />
      </div>

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
      />
    </>
  );
}
