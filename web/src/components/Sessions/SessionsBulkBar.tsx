interface SessionsBulkBarProps {
  /** Names the bar for assistive tech, e.g. "Selected sessions". */
  label: string;
  count: number;
  busy: boolean;
  onPin: () => void;
  onUnpin: () => void;
  onDelete: () => void;
}

export function SessionsBulkBar({ label, count, busy, onPin, onUnpin, onDelete }: SessionsBulkBarProps) {
  return (
    <div className="ssn-bulk" role="toolbar" aria-label={label}>
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
