interface NewChatFooterProps {
  /** Why Create is disabled; null when it is not. */
  reason: string | null;
  error: string | null;
  creating: boolean;
  canCreate: boolean;
  /** Tints the primary button orange: the cards will start running. */
  yolo: boolean;
  label: string;
  onCancel: () => void;
  onCreate: () => void;
}

export function NewChatFooter({ reason, error, creating, canCreate, yolo, label, onCancel, onCreate }: NewChatFooterProps) {
  return (
    <div className="nd-foot">
      {error ? (
        <p className="nd-why nd-why--error" role="alert">
          {error}
        </p>
      ) : (
        <p className="nd-why" aria-live="polite">
          {reason ??
            (creating ? null : (
              <>
                <kbd>Ctrl</kbd>+<kbd>Enter</kbd> to create
              </>
            ))}
        </p>
      )}
      <button type="button" className="nd-btn" onClick={onCancel} disabled={creating}>
        Cancel
      </button>
      <button
        type="button"
        className={`nd-btn nd-btn--primary${yolo ? ' nd-btn--yolo' : ''}`}
        onClick={onCreate}
        disabled={!canCreate}
        aria-busy={creating}
      >
        {creating && <span className="nd-spin" aria-hidden="true" />}
        {label}
      </button>
    </div>
  );
}
