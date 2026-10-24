interface SessionsBulkBarProps {
  count: number;
  busy: boolean;
  onPin: () => void;
  onUnpin: () => void;
  onDelete: () => void;
}

export function SessionsBulkBar({ count, busy, onPin, onUnpin, onDelete }: SessionsBulkBarProps) {
  return (
    <div className="ssn-bulk" role="toolbar" aria-label="Selected sessions">
      <span>
        <b>{count}</b> selected
      </span>
      <span className="ssn-bulk-acts">
        <button type="button" onClick={onPin} disabled={busy}>Pin</button>
        <button type="button" onClick={onUnpin} disabled={busy}>Unpin</button>
        <button type="button" className="ssn-bulk-del" onClick={onDelete} disabled={busy}>Delete…</button>
      </span>
    </div>
  );
}
